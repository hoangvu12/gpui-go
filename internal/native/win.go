//go:build windows

package native

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// All Windows interop goes through the stdlib syscall package and explicit
// kernel32 procedure addresses — no cgo, no third-party modules
// (CGO_ENABLED=0 by design).
//
// Loading policy (distribution contract, restricted absolute load): the DLL
// is loaded by LoadLibraryExW with its absolute verified path and ONLY
//
//	LOAD_LIBRARY_SEARCH_SYSTEM32 (0x00000800)
//
// The application directory, CWD and PATH are never searched; dependencies
// can resolve only from System32. LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR
// (0x00000100) is deliberately NOT set: the contract reserves it for future
// artifacts that ship private companion DLLs — "if future artifacts need
// them, revise the manifest and use a verified artifact directory plus
// LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR; do not add global DLL search paths".
// The ticket02 artifact imports only OS system components, so no companion
// resolution is needed.
// The kernel32 procedure table. The ticket names syscall.NewLazySystemDLL;
// Go 1.27 removed that constructor. The equivalent in this toolchain is
// syscall.NewLazyDLL("kernel32.dll"): the stdlib syscall package registers
// kernel32.dll in its internal system-DLL registry during its own package
// init (zsyscall_windows.go: modkernel32 = NewLazyDLL(sysdll.Add("kernel32.dll"))),
// so LoadDLL resolves it through LoadLibraryExW with
// LOAD_LIBRARY_SEARCH_SYSTEM32 — System32 only, no preloading search. The
// procs are resolved explicitly (Proc.Find) instead of letting LazyProc.Addr
// panic, so missing exports surface as typed errors.
var (
	modkernel32        = syscall.NewLazyDLL("kernel32.dll")
	procLoadLibraryExW = modkernel32.NewProc("LoadLibraryExW")
	procGetProcAddress = modkernel32.NewProc("GetProcAddress")
	procCreateFileW    = modkernel32.NewProc("CreateFileW")
	procReadFile       = modkernel32.NewProc("ReadFile")
	procLockFileEx     = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx   = modkernel32.NewProc("UnlockFileEx")
)

// resolveKernel32 finds every kernel32 procedure used by this package once;
// later calls return the cached result.
var (
	kernel32Once sync.Once
	kernel32Err  error
)

func resolveKernel32() error {
	kernel32Once.Do(func() {
		for _, p := range []*syscall.LazyProc{
			procLoadLibraryExW,
			procGetProcAddress,
			procCreateFileW,
			procReadFile,
			procLockFileEx,
			procUnlockFileEx,
		} {
			if err := p.Find(); err != nil {
				kernel32Err = &OSLoadError{Path: "kernel32.dll", Err: err}
				return
			}
		}
	})
	return kernel32Err
}

// LOAD_LIBRARY_EX flags (libloaderapi.h). loadLibrarySearchDLLLoadDir is
// intentionally unused in this slice: the distribution contract reserves it
// for future verified private companion DLLs (see the package comment).
const (
	loadLibrarySearchDLLLoadDir = 0x00000100
	loadLibrarySearchSystem32   = 0x00000800
)

// CreateFileW constants (winbase.h / winnt.h).
const (
	genericRead    = 0x80000000
	genericWrite   = 0x40000000
	fileShareRead  = 0x00000001
	fileShareWrite = 0x00000002
	openExisting   = 3
	openAlways     = 4

	fileAttributeNormal  = 0x00000080
	fileAttributeReparse = 0x00000400

	invalidHandleValue = ^uintptr(0)
)

// LockFileEx flags (winbase.h).
const (
	lockfileExclusiveLock = 0x00000002
)

// loadLibraryExRestricted loads an absolute-path DLL with the restricted
// search flag above (LOAD_LIBRARY_SEARCH_SYSTEM32 only; see the package
// comment for why DLL_LOAD_DIR stays reserved). A zero return wraps the raw
// syscall errno (for example ERROR_MOD_NOT_FOUND 126 or ERROR_ACCESS_DENIED
// 5) in an OSLoadError.
func loadLibraryExRestricted(path string) (uintptr, error) {
	if err := resolveKernel32(); err != nil {
		return 0, err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, &OSLoadError{Path: path, Err: err}
	}
	h, _, callErr := syscall.SyscallN(
		procLoadLibraryExW.Addr(),
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(loadLibrarySearchSystem32),
	)
	if h == 0 {
		return 0, &OSLoadError{Path: path, Err: callErr}
	}
	return h, nil
}

// getProcAddr resolves an export by name (used for gpui_go_abi and the panic
// probe). A zero return means the export is missing.
func getProcAddr(module uintptr, name string) (uintptr, error) {
	if err := resolveKernel32(); err != nil {
		return 0, err
	}
	n, err := syscall.BytePtrFromString(name)
	if err != nil {
		return 0, err
	}
	addr, _, callErr := syscall.SyscallN(
		procGetProcAddress.Addr(),
		module,
		uintptr(unsafe.Pointer(n)),
	)
	if addr == 0 {
		return 0, fmt.Errorf("GetProcAddress(%s): %w", name, callErr)
	}
	return addr, nil
}

// openVerifiedFile opens path for read with FILE_SHARE_READ ONLY —
// sharing that DENIES write and delete to every other opener while the
// handle is retained (distribution contract: "Open the selected DLL for read
// with sharing that denies write/delete, hash that open file, and retain the
// verified-file handle across loading; retain the protection for the
// module's process lifetime"). The caller hashes the file through this same
// handle (readAll) and hands it to the Library, which retains it until
// release/shutdown.
func openVerifiedFile(path string) (*verifiedFile, error) {
	if err := resolveKernel32(); err != nil {
		return nil, err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, _, callErr := syscall.SyscallN(
		procCreateFileW.Addr(),
		uintptr(unsafe.Pointer(p)),
		uintptr(genericRead),
		uintptr(fileShareRead), // no FILE_SHARE_WRITE, no FILE_SHARE_DELETE
		0,                      // no security attributes
		uintptr(openExisting),
		uintptr(fileAttributeNormal),
		0,
	)
	if h == invalidHandleValue || h == 0 {
		return nil, fmt.Errorf("CreateFileW: %w", callErr)
	}
	return &verifiedFile{path: path, handle: h}, nil
}

// readAll reads the whole file through the open verified handle. The handle
// stays open (and keeps denying write/delete) across the read and afterwards.
func (v *verifiedFile) readAll() ([]byte, error) {
	if err := resolveKernel32(); err != nil {
		return nil, err
	}
	var out []byte
	buf := make([]byte, 64*1024)
	for {
		var n uint32
		r1, _, callErr := syscall.SyscallN(
			procReadFile.Addr(),
			v.handle,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&n)),
			0, // synchronous handle: no OVERLAPPED
		)
		if r1 == 0 {
			return nil, fmt.Errorf("ReadFile: %w", callErr)
		}
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		// ReadFile reports completion of a synchronous read via n < len(buf)
		// (no ERROR_MORE_DATA looping for a plain file).
		if n < uint32(len(buf)) {
			break
		}
	}
	return out, nil
}

// close releases the verified handle (and with it the write/delete denial).
func (v *verifiedFile) close() {
	if v == nil || v.handle == 0 {
		return
	}
	syscall.CloseHandle(syscall.Handle(v.handle))
	v.handle = 0
}

// acquirePublicationLock opens (or creates) the lock file at path and takes
// a blocking, exclusive LockFileEx byte-range lock over [0,1). Every
// framework loader — in this or any other process — holds this lock while
// inspecting, publishing or cleaning a cache artifact directory.
func acquirePublicationLock(path string) (*publicationLock, error) {
	if err := resolveKernel32(); err != nil {
		return nil, err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, _, callErr := syscall.SyscallN(
		procCreateFileW.Addr(),
		uintptr(unsafe.Pointer(p)),
		uintptr(genericRead|genericWrite),
		uintptr(fileShareRead|fileShareWrite), // other processes may open/lock it too
		0,
		uintptr(openAlways),
		uintptr(fileAttributeNormal),
		0,
	)
	if h == invalidHandleValue || h == 0 {
		return nil, fmt.Errorf("CreateFileW(lock): %w", callErr)
	}
	var ov syscall.Overlapped // Offset 0: lock range [0,1)
	r1, _, callErr := syscall.SyscallN(
		procLockFileEx.Addr(),
		h,
		uintptr(lockfileExclusiveLock),
		0,
		1, // nNumberOfBytesToLockLow
		0, // nNumberOfBytesToLockHigh
		uintptr(unsafe.Pointer(&ov)),
	)
	if r1 == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("LockFileEx: %w", callErr)
	}
	return &publicationLock{handle: h}, nil
}

// release unlocks and closes the lock file. Locks are also released by the OS
// when the handle or process goes away; explicit release keeps the critical
// section short for other processes.
func (l *publicationLock) release() {
	if l == nil || l.handle == 0 {
		return
	}
	var ov syscall.Overlapped
	syscall.SyscallN(
		procUnlockFileEx.Addr(),
		l.handle,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ov)),
	)
	syscall.CloseHandle(syscall.Handle(l.handle))
	l.handle = 0
}

// hostSupported reports whether the running host can load the artifact
// (windows/amd64 only in the initial target).
func hostSupported() error {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("%w: running on %s/%s", ErrUnsupportedTarget, runtime.GOOS, runtime.GOARCH)
	}
	return nil
}

// loadTableFromModule resolves gpui_go_abi, calls it and copies the returned
// bootstrap table into Go-owned memory. The pointer itself is a Rust static
// valid for the process lifetime; the copy makes the loader independent of
// any aliasing and lets validateAbiTable run against plain Go memory.
func loadTableFromModule(module uintptr, path string) (*abiTable, error) {
	addr, err := getProcAddr(module, "gpui_go_abi")
	if err != nil {
		return nil, &ExportError{Path: path, Name: "gpui_go_abi", Err: err}
	}
	r1, _, callErr := syscall.SyscallN(addr)
	if r1 == 0 {
		return nil, &ABIError{Path: path, Field: "gpui_go_abi()", Expected: "non-null table pointer", Actual: fmt.Sprintf("null (%v)", callErr)}
	}
	// r1 is a pointer value returned by the foreign export. Converting a
	// syscall result uintptr to unsafe.Pointer has no vet-sanctioned form, so
	// reinterpret the bits through the local variable below (see
	// pointerFromRaw).
	t := (*abiTable)(pointerFromRaw(r1))
	tbl := *t // copy out of DLL memory
	return &tbl, nil
}

// pointerFromRaw converts a pointer value returned by a foreign function
// into an unsafe.Pointer. go vet's unsafeptr checker has no sanctioned form
// for this FFI direction (a direct unsafe.Pointer(uintptr) conversion is
// flagged), so the conversion reinterprets the bits through the address of
// the local variable instead. The value is a live Rust static for the
// process lifetime; it is never dereferenced after the copy below.
func pointerFromRaw(r uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&r))
}

// callRoundTripRaw invokes the table's round-trip function pointer with the
// given — possibly null — record pointers. The pointer-to-uintptr
// conversions sit inside the syscall.SyscallN call expressions (the
// documented-safe pattern), and runtime.KeepAlive covers the records for the
// duration of the call. This is the low-level hook the package's tests use to
// exercise the DLL's own status-code paths with crafted records.
func callRoundTripRaw(fn uintptr, req *bufferRequest, res *bufferResponse) (int32, error) {
	if fn == 0 {
		return 0, &ABIError{Field: "buffer_round_trip", Expected: "non-null", Actual: "null"}
	}
	var r1 uintptr
	switch {
	case req == nil && res == nil:
		r1, _, _ = syscall.SyscallN(fn, 0, 0)
	case req == nil:
		r1, _, _ = syscall.SyscallN(fn, 0, uintptr(unsafe.Pointer(res)))
		runtime.KeepAlive(res)
	case res == nil:
		r1, _, _ = syscall.SyscallN(fn, uintptr(unsafe.Pointer(req)), 0)
		runtime.KeepAlive(req)
	default:
		r1, _, _ = syscall.SyscallN(fn, uintptr(unsafe.Pointer(req)), uintptr(unsafe.Pointer(res)))
		runtime.KeepAlive(req)
		runtime.KeepAlive(res)
	}
	return int32(r1), nil
}

// callNoArgExport invokes a no-argument diagnostic export (the panic probe).
func callNoArgExport(addr uintptr) (int32, error) {
	r1, _, callErr := syscall.SyscallN(addr)
	if r1 == 0 {
		return 0, fmt.Errorf("export returned 0: %w", callErr)
	}
	return int32(r1), nil
}
