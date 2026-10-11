//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// A11y service FFI (Windows). Same conventions as the other service
// FFI files: SyscallN through the table slots, the pointer-to-uintptr
// conversions inside the call expressions, KeepAlive on every Go-owned
// record and buffer.

func callA11yAdapterCreate(fn uintptr, hwnd uintptr, isFocused bool, label string) (A11yAdapterHandle, int32, error) {
	if fn == 0 {
		return 0, 0, nullSlot("a11y_adapter_create")
	}
	var labelPtr *byte
	var labelLen uintptr
	if len(label) > 0 {
		labelPtr = unsafe.StringData(label)
		labelLen = uintptr(len(label))
	}
	var out uint64
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(hwnd),
		boolWord(isFocused),
		uintptr(unsafe.Pointer(labelPtr)),
		labelLen,
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(&out)
	runtime.KeepAlive(label)
	return A11yAdapterHandle(out), callStatus(r1), nil
}

func callA11yAdapterDestroy(fn uintptr, handle A11yAdapterHandle) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("a11y_adapter_destroy")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle))
	return callStatus(r1), nil
}

func callA11yPublish(
	fn uintptr,
	handle A11yAdapterHandle,
	nodes []A11yNodeRecord,
	children []uint64,
	strings []byte,
	rootID uint64,
	focusID uint64,
) (A11yPublishRecord, int32, error) {
	if fn == 0 {
		return A11yPublishRecord{}, 0, nullSlot("a11y_publish")
	}
	var nodesPtr *A11yNodeRecord
	if len(nodes) > 0 {
		nodesPtr = &nodes[0]
	}
	var childrenPtr *uint64
	if len(children) > 0 {
		childrenPtr = &children[0]
	}
	var stringsPtr *byte
	if len(strings) > 0 {
		stringsPtr = &strings[0]
	}
	var out A11yPublishRecord
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(nodesPtr)),
		uintptr(len(nodes)),
		uintptr(unsafe.Pointer(childrenPtr)),
		uintptr(len(children)),
		uintptr(unsafe.Pointer(stringsPtr)),
		uintptr(len(strings)),
		uintptr(rootID),
		uintptr(focusID),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(&out)
	runtime.KeepAlive(nodes)
	runtime.KeepAlive(children)
	runtime.KeepAlive(strings)
	return out, callStatus(r1), nil
}

func callA11yHandleWmGetObject(
	fn uintptr,
	handle A11yAdapterHandle,
	wparam uintptr,
	lparam uintptr,
) (A11yWmGetObjectResult, int32, error) {
	if fn == 0 {
		return A11yWmGetObjectResult{}, 0, nullSlot("a11y_handle_wm_getobject")
	}
	var result A11yWmGetObjectResult
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(wparam),
		intptr(lparam),
		uintptr(unsafe.Pointer(&result.Handled)),
		uintptr(unsafe.Pointer(&result.LResult)),
	)
	runtime.KeepAlive(&result)
	return result, callStatus(r1), nil
}

func callA11yPollActions(
	fn uintptr,
	handle A11yAdapterHandle,
	records []A11yActionRecord,
	stringsBuf []byte,
) (count uint32, stringsNeeded uint32, code int32, err error) {
	if fn == 0 {
		return 0, 0, 0, nullSlot("a11y_poll_actions")
	}
	var recordsPtr *A11yActionRecord
	if len(records) > 0 {
		recordsPtr = &records[0]
	}
	var stringsPtr *byte
	if len(stringsBuf) > 0 {
		stringsPtr = &stringsBuf[0]
	}
	var outCount, outNeeded uint32
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(recordsPtr)),
		uintptr(len(records)),
		uintptr(unsafe.Pointer(stringsPtr)),
		uintptr(len(stringsBuf)),
		uintptr(unsafe.Pointer(&outCount)),
		uintptr(unsafe.Pointer(&outNeeded)),
	)
	runtime.KeepAlive(&outCount)
	runtime.KeepAlive(&outNeeded)
	runtime.KeepAlive(records)
	runtime.KeepAlive(stringsBuf)
	return outCount, outNeeded, callStatus(r1), nil
}

func callA11yIsActive(fn uintptr, handle A11yAdapterHandle) (uint32, int32, error) {
	if fn == 0 {
		return 0, 0, nullSlot("a11y_is_active")
	}
	var active uint32
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(&active)),
	)
	runtime.KeepAlive(&active)
	return active, callStatus(r1), nil
}

func callA11yPendingActions(fn uintptr, handle A11yAdapterHandle) (uint32, int32, error) {
	if fn == 0 {
		return 0, 0, nullSlot("a11y_pending_actions")
	}
	var count uint32
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(&count)),
	)
	runtime.KeepAlive(&count)
	return count, callStatus(r1), nil
}

// boolWord encodes a bool as the u32 the ABI takes.
func boolWord(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// intptr reinterprets a uintptr as the signed LPARAM the ABI takes.
func intptr(v uintptr) uintptr {
	return uintptr(int64(int64(v)))
}
