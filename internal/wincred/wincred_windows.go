//go:build windows

package wincred

// The Windows Credential Manager calls (advapi32), pure stdlib syscall
// bindings with CGO_ENABLED=0. Struct layout and error mapping follow
// reference/ce-source/crates/gpui_windows/src/platform.rs:829-926; see
// wincred.go for the package contract.

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32                 = syscall.NewLazyDLL("advapi32.dll")
	modKernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procCredWriteW              = modAdvapi32.NewProc("CredWriteW")
	procCredReadW               = modAdvapi32.NewProc("CredReadW")
	procCredDeleteW             = modAdvapi32.NewProc("CredDeleteW")
	procCredFree                = modAdvapi32.NewProc("CredFree")
	procGetSystemTimeAsFileTime = modKernel32.NewProc("GetSystemTimeAsFileTime")
)

// Windows constants (wincred.h).
const (
	credTypeGeneric         = 1    // CRED_TYPE_GENERIC
	credPersistLocalMachine = 2    // CRED_PERSIST_LOCAL_MACHINE
	errorNotFound           = 1168 // ERROR_NOT_FOUND
)

// credentialW is Win32 CREDENTIALW (80 bytes on amd64). Field order and
// the interior padding follow the SDK layout the pinned reference fills
// (platform.rs:846-856).
type credentialW struct {
	flags          uint32
	typ            uint32
	targetName     *uint16
	comment        *uint16
	lastWritten    int64 // FILETIME
	blobSize       uint32
	_pad           uint32
	blob           *byte
	persist        uint32
	attributeCount uint32
	// No padding here: AttributeCount ends at offset 56, which is
	// already pointer-aligned, matching the SDK layout exactly.
	attributes  uintptr
	targetAlias *uint16
	userName    *uint16
}

// windowsCredentialStore is the platform store behind gpui's default
// real-app credential seam.
type windowsCredentialStore struct{}

// Store returns the process-wide Windows Credential Manager store.
func Store() *windowsCredentialStore { return &windowsCredentialStore{} }

// Write stores url/username/password as a CRED_TYPE_GENERIC credential
// under the "zed:url={url}" target, mirroring write_credentials
// (platform.rs:829-867): the blob-size precheck, LastWritten from
// GetSystemTimeAsFileTime, CRED_PERSIST_LOCAL_MACHINE and the
// CredWriteW error message.
func (windowsCredentialStore) Write(url, username string, password []byte) error {
	if len(password) > MaxCredentialBlobSize {
		return &BlobSizeError{URL: url, Size: len(password)}
	}
	target16, err := syscall.UTF16FromString(TargetName(url))
	if err != nil {
		return fmt.Errorf("credential target for %q is not representable: %w", url, err)
	}
	user16, err := syscall.UTF16FromString(username)
	if err != nil {
		return fmt.Errorf("credential username for %q is not representable: %w", url, err)
	}
	var lastWritten int64
	procGetSystemTimeAsFileTime.Call(uintptr(unsafe.Pointer(&lastWritten)))
	// The blob may be empty: CredWriteW accepts CredentialBlobSize 0 with
	// a NULL blob (the reference stores empty passwords the same way).
	var blobPtr *byte
	if len(password) > 0 {
		blobPtr = &password[0]
	}
	cred := credentialW{
		typ:         credTypeGeneric,
		targetName:  &target16[0],
		lastWritten: lastWritten,
		blobSize:    uint32(len(password)),
		blob:        blobPtr,
		persist:     credPersistLocalMachine,
		userName:    &user16[0],
	}
	if r, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0); r == 0 {
		if callErr == nil {
			callErr = errors.New("CredWriteW failed")
		}
		return fmt.Errorf("failed to write credentials to Windows Credential Manager: %w", callErr)
	}
	return nil
}

// Read returns the credential stored for url. found=false with a nil
// error reports absence (CredReadW's ERROR_NOT_FOUND), mirroring
// read_credentials (platform.rs:869-909): "ERROR_NOT_FOUND means the
// credential doesn't exist. Return Ok(None) to match macOS and Linux
// behavior." The CredReadW allocation is released with CredFree after
// the username and blob are copied.
func (windowsCredentialStore) Read(url string) (entry Entry, found bool, err error) {
	target16, err := syscall.UTF16FromString(TargetName(url))
	if err != nil {
		return Entry{}, false, fmt.Errorf("credential target for %q is not representable: %w", url, err)
	}
	var cred *credentialW
	if r, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(&target16[0])),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&cred)),
	); r == 0 {
		if errors.Is(callErr, syscall.Errno(errorNotFound)) {
			// Absence: the reference's Ok(None).
			return Entry{}, false, nil
		}
		if callErr == nil {
			callErr = errors.New("CredReadW failed")
		}
		return Entry{}, false, fmt.Errorf("failed to read credentials from Windows Credential Manager: %w", callErr)
	}
	if cred == nil {
		return Entry{}, false, nil
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	entry.Username = utf16PtrToString(cred.userName)
	if cred.blobSize > 0 && cred.blob != nil {
		blob := make([]byte, cred.blobSize)
		copy(blob, unsafe.Slice(cred.blob, cred.blobSize))
		entry.Password = blob
	} else {
		entry.Password = []byte{}
	}
	return entry, true, nil
}

// Delete removes the credential stored for url, propagating CredDeleteW
// failures (deleting an absent credential is an error), mirroring
// delete_credentials (platform.rs:911-926).
func (windowsCredentialStore) Delete(url string) error {
	target16, err := syscall.UTF16FromString(TargetName(url))
	if err != nil {
		return fmt.Errorf("credential target for %q is not representable: %w", url, err)
	}
	if r, _, callErr := procCredDeleteW.Call(
		uintptr(unsafe.Pointer(&target16[0])),
		credTypeGeneric,
		0,
	); r == 0 {
		if callErr == nil {
			callErr = errors.New("CredDeleteW failed")
		}
		return fmt.Errorf("failed to delete credentials from Windows Credential Manager: %w", callErr)
	}
	return nil
}

// utf16PtrToString decodes a NUL-terminated UTF-16 string (the UserName
// field is NUL-terminated per the CREDENTIALW contract).
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	// Find the NUL terminator.
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; n++ {
		ptr = unsafe.Pointer(uintptr(ptr) + 2)
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
