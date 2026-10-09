//go:build windows

package gpui

// Ticket25's Win32 dialog driver: the pure-Go IFileDialog port of
// reference/ce-source/crates/gpui_windows/src/platform.rs:1296-1389
// (file_open_dialog / file_save_dialog) plus the real credential store
// backed by internal/wincred. Every COM call goes through stdlib
// syscalls with CGO_ENABLED=0; the driver runs on the host's OLE-STA
// thread, where IFileDialog::Show's nested modal loop keeps pumping
// messages so the rest of the application stays responsive.
//
// Vtable slots follow the SDK interface layouts (ShObjIdl_core.h):
// IFileDialog (IUnknown 0-2, IModalWindow::Show 3, SetFileTypes 4,
// SetFileTypeIndex 5, GetFileTypeIndex 6, Advise 7, Unadvise 8,
// SetOptions 9, GetOptions 10, SetDefaultFolder 11, SetFolder 12,
// GetFolder 13, GetCurrentSelection 14, SetFileName 15, GetFileName 16,
// SetTitle 17, SetOkButtonLabel 18, SetFileNameLabel 19, GetResult 20,
// AddPlace 21, SetDefaultExtension 22, Close 23, SetClientGuid 24,
// ClearClientData 25, SetFilter 26) and the derived interfaces
// IFileOpenDialog (GetResults 27, GetSelectedItems 28) and
// IShellItem (BindToHandler 3, GetParent 4, GetDisplayName 5,
// GetAttributes 6, Compare 7) / IShellItemArray (GetCount 7,
// GetItemAt 8).

import (
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	wincred "gpui-go/internal/wincred"
)

// ---------------------------------------------------------------------------
// Win32 COM bindings
// ---------------------------------------------------------------------------

var (
	modShell32 = syscall.NewLazyDLL("shell32.dll")
	// user32 GetActiveWindow (already resolved through modUser32 in
	// win32host_windows.go? No: it is not in the host's required set).
	procGetActiveWindow = modUser32.NewProc("GetActiveWindow")
	// ole32 (CoCreateInstance/CoTaskMemFree) and shell32
	// (SHCreateItemFromParsingName).
	procCoCreateInstance            = modOle32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = modOle32.NewProc("CoTaskMemFree")
	procSHCreateItemFromParsingName = modShell32.NewProc("SHCreateItemFromParsingName")
)

// guid is a Win32 GUID/IID/CLSID.
type guid struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

// CLSID/IID values from ShObjIdl_core.h of the installed Windows SDK
// (10.0.26100.0) — the same constants the reference resolves through the
// windows crate's CoCreateInstance/FileOpenDialog bindings. Verified
// against the machine's registry (the dialog CLSIDs) and SDK headers
// (the interface IIDs).
var (
	clsidFileOpenDialog = guid{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	clsidFileSaveDialog = guid{0xC0B4E2F3, 0xBA21, 0x4773, [8]byte{0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B}}
	iidIFileOpenDialog  = guid{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIFileSaveDialog  = guid{0x84BCCD23, 0x5FDE, 0x4CDB, [8]byte{0xAE, 0xA4, 0xAF, 0x64, 0xB8, 0x3D, 0x78, 0xAB}}
	iidIShellItem       = guid{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

// FILEOPENDIALOGOPTIONS bits (ShObjIdl_core.h).
const (
	fosPickFolders      = 0x00000020
	fosAllowMultiSelect = 0x00000200
	fosFileMustExist    = 0x00001000
)

// SIGDN_FILESYSPATH (ShObjIdl_core.h).
const sigdnFileSysPath = uintptr(0x80058000)

// CLSCTX_ALL (CLSCTX_INPROC_SERVER|CLSCTX_INPROC_HANDLER|CLSCTX_LOCAL_SERVER|CLSCTX_REMOTE_SERVER).
const clsctxAll = uintptr(0x1 | 0x2 | 0x4 | 0x10)

// hresultFromWin32(ERROR_CANCELLED) — the dismissal code passed to
// IFileDialog::Close so Show reports cancellation.
const hresultCancelled = uintptr(0x800704C5)

// wmGPUIDialogDismiss is this port's private dismissal message
// (WM_APP-family slot 0x0400+9, following the reference's private
// platform-window messages like WM_GPUI_DOCK_MENU_ACTION). Posted to a
// per-dialog message-only window so the dismissal is dispatched by the
// dialog's own nested modal loop: the wake protocol defers posted
// foreground closures until the modal loop ends (same as the reference's
// dispatcher during dialog pumping), so a direct closure would never
// reach a live dialog.
const wmGPUIDialogDismiss = wmUser + 9

// dialogDismissClassName is the message-only window class carrying the
// dismissal trampoline.
const dialogDismissClassName = "gpui-go::DialogDismissal"

var (
	dialogClassOnce    sync.Once
	dialogClassErr     error
	dialogClassNamePtr *uint16
	dialogWndProcPtr   uintptr

	// dismissRegistry maps the per-dialog dismissal token (a random id)
	// to its live closer. The token rides the posted message's lparam.
	dismissRegistryMu sync.Mutex
	dismissRegistry   = map[uintptr]*comDialogCloser{}
)

// registerDialogDismissClass registers the dismissal window class once
// per process (reusing the host's WNDCLASSW bindings).
func registerDialogDismissClass() error {
	dialogClassOnce.Do(func() {
		var err error
		if dialogClassNamePtr, err = syscall.UTF16PtrFromString(dialogDismissClassName); err != nil {
			dialogClassErr = err
			return
		}
		dialogWndProcPtr = syscall.NewCallback(dialogDismissWndProc)
		inst, _, _ := procGetModuleHandleW.Call(0)
		wc := wndClass{
			lpfnWndProc:   dialogWndProcPtr,
			hInstance:     inst,
			lpszClassName: dialogClassNamePtr,
		}
		if a, _, _ := procRegisterClassW.Call(uintptr(unsafe.Pointer(&wc))); a == 0 {
			dialogClassErr = fmt.Errorf("gpui: RegisterClassW(%s) failed", dialogDismissClassName)
		}
	})
	return dialogClassErr
}

// dialogDismissWndProc handles the dismissal message on the dialog's
// thread: it resolves the token's closer and closes the dialog. The
// recover trampoline keeps a Go panic out of the Win32 dispatch frame
// (native ABI rule: panics are caught at trampolines). Everything else
// falls to DefWindowProcW.
func dialogDismissWndProc(hwnd uintptr, message uint32, wparam, lparam uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = 0
		}
	}()
	if message == wmGPUIDialogDismiss {
		dismissRegistryMu.Lock()
		closer := dismissRegistry[lparam]
		dismissRegistryMu.Unlock()
		if closer != nil {
			closer.dismiss()
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}

// createDismissalWindow creates the per-dialog message-only window and
// registers the dismissal token. Host (STA) thread only; the dialog's
// nested modal loop dispatches the posted dismissal through it.
func createDismissalWindow(closer *comDialogCloser) (hwnd, token uintptr, err error) {
	if err := registerDialogDismissClass(); err != nil {
		return 0, 0, err
	}
	var b [8]byte
	if _, cryptoErr := crand.Read(b[:]); cryptoErr != nil {
		return 0, 0, cryptoErr
	}
	token = uintptr(binary.LittleEndian.Uint64(b[:]))
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(dialogClassNamePtr)),
		0, 0,
		0, 0, 0, 0,
		hwndMessage,
		0,
		0,
		0,
	)
	if hwnd == 0 {
		if callErr == nil {
			callErr = fmt.Errorf("CreateWindowExW returned NULL")
		}
		return 0, 0, callErr
	}
	dismissRegistryMu.Lock()
	dismissRegistry[token] = closer
	dismissRegistryMu.Unlock()
	return hwnd, token, nil
}

// destroyDismissalWindow retires the token and destroys the per-dialog
// message-only window. Host (STA) thread only.
func destroyDismissalWindow(hwnd, token uintptr) {
	if token != 0 {
		dismissRegistryMu.Lock()
		delete(dismissRegistry, token)
		dismissRegistryMu.Unlock()
	}
	if hwnd != 0 {
		procDestroyWindow.Call(hwnd)
	}
}

// dismissalFunc returns the DialogDismissal for one in-flight dialog: a
// posted message that the dialog's nested modal loop dispatches on the
// dialog's own thread. Callable from any goroutine (PostMessageW is
// thread-safe); posting after the dialog finished is a harmless no-op
// (the window is destroyed and the token retired).
func dismissalFunc(hwnd, token uintptr) DialogDismissal {
	return func() {
		procPostMessageW.Call(hwnd, uintptr(wmGPUIDialogDismiss), 0, token)
	}
}

// comIFace is the generic COM interface-pointer carrier: the first field
// of every COM object is its vtable pointer. Keeping the object pointer
// as a typed Go pointer (rather than a uintptr) keeps every conversion
// vet-provable; the vtable array is sized to the largest interface used
// here (IFileSaveDialog has 32 slots).
type comIFace struct {
	vtbl *[32]uintptr
}

// comCall invokes one vtable slot (index) with args. The returned error
// wraps a failed HRESULT (negative int32).
func comCall(obj *comIFace, slot int, args ...uintptr) (uintptr, error) {
	if obj == nil || obj.vtbl == nil {
		return 0, fmt.Errorf("gpui: COM call on a nil interface")
	}
	fn := obj.vtbl[slot]
	callArgs := append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)
	r1, _, _ := syscall.SyscallN(fn, callArgs...)
	if int32(r1) < 0 {
		return r1, fmt.Errorf("gpui: COM method failed with HRESULT 0x%08x", uint32(r1))
	}
	return r1, nil
}

// comRelease releases the interface (IUnknown::Release).
func comRelease(obj *comIFace) {
	if obj == nil {
		return
	}
	_, _, _ = syscall.SyscallN(obj.vtbl[2], uintptr(unsafe.Pointer(obj)))
}

// coCreateInstance is CoCreateInstance(&clsid, nil, CLSCTX_ALL, &iid, &obj).
func coCreateInstance(clsid, iid *guid) (*comIFace, error) {
	var obj *comIFace
	r1, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsid)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&obj)),
	)
	if int32(r1) < 0 {
		return nil, fmt.Errorf("gpui: CoCreateInstance failed with HRESULT 0x%08x", uint32(r1))
	}
	if obj == nil {
		return nil, fmt.Errorf("gpui: CoCreateInstance returned a null interface")
	}
	return obj, nil
}

// comFilterSpec is COMDLG_FILTERSPEC.
type comFilterSpec struct {
	name *uint16
	spec *uint16
}

// allFilesFilter mirrors the reference's single filter:
// {"All files", "*.*"} (platform.rs:1366-1370).
func allFilesFilter() comFilterSpec {
	name, _ := syscall.UTF16PtrFromString("All files")
	spec, _ := syscall.UTF16PtrFromString("*.*")
	return comFilterSpec{name: name, spec: spec}
}

// getActiveWindowHWND returns the calling thread's active window
// (GetActiveWindow) — the host thread's active window when called from
// the host thread, mirroring find_current_active_window
// (crates/gpui_windows/src/platform.rs:294, 561-565, 606).
func getActiveWindowHWND() uintptr {
	hwnd, _, _ := procGetActiveWindow.Call()
	return hwnd
}

// hostLoopExited reports whether the host's message loop already exited
// (the dialog closure must never show a dialog then — the shutdown drain
// can run posted closures after the loop is gone).
func hostLoopExited(h *Host) bool { return h.loopDone.Load() }

// simplifyWindowsPath strips the \\?\ verbatim prefix the way
// dunce::simplified does for the canonicalized save-dialog directory
// (crates/gpui_windows/src/platform.rs:1348-1350): UNC paths become
// \\server\share, drive paths lose the prefix.
func simplifyWindowsPath(p string) string {
	if strings.HasPrefix(p, `\\?\UNC\`) {
		// \\?\UNC\server\share -> \\server\share (drop the 8-char
		// verbatim-UNC prefix, keep two leading backslashes).
		return `\\` + p[8:]
	}
	if strings.HasPrefix(p, `\\?\`) {
		return p[4:]
	}
	return p
}

// comDialogCloser is the dismissal seam for one shown dialog: vtable
// slot 23 (IFileDialog::Close) on the foreground thread. finish()
// invalidates it when the dialog returns so a late dismiss never touches
// a released object.
type comDialogCloser struct {
	mu       sync.Mutex
	dialog   *comIFace
	finished bool
}

func (c *comDialogCloser) dismiss() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished || c.dialog == nil {
		return
	}
	_, _, _ = syscall.SyscallN(c.dialog.vtbl[23], uintptr(unsafe.Pointer(c.dialog)), hresultCancelled)
}

func (c *comDialogCloser) finish() {
	c.mu.Lock()
	c.finished = true
	c.dialog = nil
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// The dialog port (crates/gpui_windows/src/platform.rs:1296-1389)
// ---------------------------------------------------------------------------

// openDialogOptions maps PathPromptOptions to FILEOPENDIALOGOPTIONS
// exactly like file_open_dialog (platform.rs:1304-1311): the base is
// always FOS_FILEMUSTEXIST (options.files is not consulted on Windows —
// the flag toggles nothing here), multiple adds
// FOS_ALLOWMULTISELECT, directories adds FOS_PICKFOLDERS.
func openDialogOptions(options PathPromptOptions) uintptr {
	dialogOptions := uintptr(fosFileMustExist)
	if options.Multiple {
		dialogOptions |= fosAllowMultiSelect
	}
	if options.Directories {
		dialogOptions |= fosPickFolders
	}
	return dialogOptions
}

// win32FileOpenDialog is the file_open_dialog port
// (platform.rs:1296-1339): CoCreateInstance(FileOpenDialog), SetOptions,
// SetOkButtonLabel for the prompt label, Show (any Show error is the
// user-cancelled Ok(None) result, exactly like the reference's
// `if folder_dialog.Show(window).is_err() { return Ok(None) }`),
// GetResults/GetCount (zero results = cancelled), then per-item
// GetDisplayName(SIGDN_FILESYSPATH).
//
// Deviation from the reference: it frees the GetDisplayName string only
// in the save dialog (platform.rs:1381-1384) and leaks the open dialog's
// per-item names (item.GetDisplayName(...).to_string() drops the PWSTR);
// this port frees both with CoTaskMemFree — the correct allocation
// discipline, cited here as the deliberate fix.
func win32FileOpenDialog(owner uintptr, options PathPromptOptions, dismiss func(d DialogDismissal)) PathsOutcome {
	dialog, err := coCreateInstance(&clsidFileOpenDialog, &iidIFileOpenDialog)
	if err != nil {
		return PathsOutcome{Err: err}
	}
	defer comRelease(dialog)

	if _, err := comCall(dialog, 9 /* SetOptions */, openDialogOptions(options)); err != nil {
		return PathsOutcome{Err: err}
	}
	if options.Prompt != "" {
		label, err := syscall.UTF16PtrFromString(options.Prompt)
		if err != nil {
			return PathsOutcome{Err: fmt.Errorf("gpui: dialog prompt label is not representable: %w", err)}
		}
		if _, err := comCall(dialog, 18 /* SetOkButtonLabel */, uintptr(unsafe.Pointer(label))); err != nil {
			return PathsOutcome{Err: err}
		}
	}

	// Register the dismissal before Show: a Cancel racing the dialog
	// posts the private dismissal message, which the dialog's own modal
	// loop dispatches on this (STA) thread. The window and token retire
	// with the dialog.
	closer := &comDialogCloser{dialog: dialog}
	dismissHwnd, dismissToken, err := createDismissalWindow(closer)
	if err != nil {
		return PathsOutcome{Err: err}
	}
	defer destroyDismissalWindow(dismissHwnd, dismissToken)
	defer closer.finish()
	if dismiss != nil {
		dismiss(dismissalFunc(dismissHwnd, dismissToken))
	}

	if _, err := comCall(dialog, 3 /* Show */, owner); err != nil {
		// User cancelled (the reference maps ANY Show error to Ok(None)).
		return PathsOutcome{Cancelled: true}
	}

	var results *comIFace
	if _, err := comCall(dialog, 27 /* GetResults */, uintptr(unsafe.Pointer(&results))); err != nil {
		return PathsOutcome{Err: err}
	}
	defer comRelease(results)

	var count uintptr
	if _, err := comCall(results, 7 /* GetCount */, uintptr(unsafe.Pointer(&count))); err != nil {
		return PathsOutcome{Err: err}
	}
	if count == 0 {
		// No results: the reference returns Ok(None).
		return PathsOutcome{Cancelled: true}
	}
	paths := make([]string, 0, count)
	for i := uintptr(0); i < count; i++ {
		var item *comIFace
		if _, err := comCall(results, 8 /* GetItemAt */, i, uintptr(unsafe.Pointer(&item))); err != nil {
			return PathsOutcome{Err: err}
		}
		var name *uint16
		if _, err := comCall(item, 5 /* GetDisplayName */, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); err != nil {
			comRelease(item)
			return PathsOutcome{Err: err}
		}
		path := ""
		if name != nil {
			path = syscall.UTF16ToString(unsafe.Slice(name, utf16Len(name)))
			procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
		}
		comRelease(item)
		paths = append(paths, path)
	}
	return PathsOutcome{Paths: paths}
}

// utf16Len counts the code units before the NUL terminator of a
// PWSTR the Win32 heap allocated.
func utf16Len(p *uint16) int {
	n := 0
	for unsafe.Pointer(p) != nil && *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return n
}

// win32FileSaveDialog is the file_save_dialog port
// (platform.rs:1341-1389): canonicalize the directory (simplified), set
// the folder through SHCreateItemFromParsingName + SetFolder (failures
// logged and skipped, like the reference's .log_err()), SetFileName for
// the suggested name (failures skipped), SetFileTypes with the single
// All-files filter (fatal), Show (any error = cancelled), then
// GetResult + GetDisplayName(SIGDN_FILESYSPATH) + CoTaskMemFree.
func win32FileSaveDialog(owner uintptr, directory, suggestedName string, dismiss func(d DialogDismissal)) PathOutcome {
	dialog, err := coCreateInstance(&clsidFileSaveDialog, &iidIFileSaveDialog)
	if err != nil {
		return PathOutcome{Err: err}
	}
	defer comRelease(dialog)

	if directory != "" {
		if full, err := canonicalizeDirectory(directory); err == nil {
			path16, err := syscall.UTF16PtrFromString(full)
			if err == nil {
				var item *comIFace
				if hr, _, _ := procSHCreateItemFromParsingName.Call(
					uintptr(unsafe.Pointer(path16)),
					0,
					uintptr(unsafe.Pointer(&iidIShellItem)),
					uintptr(unsafe.Pointer(&item)),
				); int32(hr) >= 0 && item != nil {
					if _, err := comCall(dialog, 12 /* SetFolder */, uintptr(unsafe.Pointer(item))); err != nil {
						// The reference logs SetFolder failures and
						// continues (platform.rs:1359-1363 .log_err()).
						_ = err
					}
					comRelease(item)
				}
			}
		}
	}
	if suggestedName != "" {
		name16, err := syscall.UTF16PtrFromString(suggestedName)
		if err == nil {
			if _, err := comCall(dialog, 15 /* SetFileName */, uintptr(unsafe.Pointer(name16))); err != nil {
				// The reference logs SetFileName failures and continues
				// (platform.rs:1367-1373 .log_err()).
				_ = err
			}
		}
	}
	filter := allFilesFilter()
	if _, err := comCall(dialog, 4 /* SetFileTypes */, 1, uintptr(unsafe.Pointer(&filter))); err != nil {
		return PathOutcome{Err: err}
	}

	closer := &comDialogCloser{dialog: dialog}
	dismissHwnd, dismissToken, err := createDismissalWindow(closer)
	if err != nil {
		return PathOutcome{Err: err}
	}
	defer destroyDismissalWindow(dismissHwnd, dismissToken)
	defer closer.finish()
	if dismiss != nil {
		dismiss(dismissalFunc(dismissHwnd, dismissToken))
	}

	if _, err := comCall(dialog, 3 /* Show */, owner); err != nil {
		return PathOutcome{Cancelled: true}
	}

	var item *comIFace
	if _, err := comCall(dialog, 20 /* GetResult */, uintptr(unsafe.Pointer(&item))); err != nil {
		return PathOutcome{Err: err}
	}
	defer comRelease(item)
	var name *uint16
	if _, err := comCall(item, 5 /* GetDisplayName */, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); err != nil {
		return PathOutcome{Err: err}
	}
	path := ""
	if name != nil {
		path = syscall.UTF16ToString(unsafe.Slice(name, utf16Len(name)))
		procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
	}
	return PathOutcome{Path: path}
}

// canonicalizeDirectory mirrors the save dialog's directory handling
// (platform.rs:1345-1350): fs::canonicalize then dunce::simplified. A
// canonicalization failure is logged and skipped by the caller, like the
// reference's .log_err().
func canonicalizeDirectory(directory string) (string, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return simplifyWindowsPath(resolved), nil
}

// win32DialogDriver is the real platform dialog driver.
type win32DialogDriver struct{}

// PromptForPaths shows the Win32 open dialog.
func (win32DialogDriver) PromptForPaths(owner uintptr, options PathPromptOptions, dismiss func(d DialogDismissal)) PathsOutcome {
	return win32FileOpenDialog(owner, options, dismiss)
}

// PromptForNewPath shows the Win32 save dialog.
func (win32DialogDriver) PromptForNewPath(owner uintptr, directory, suggestedName string, dismiss func(d DialogDismissal)) PathOutcome {
	return win32FileSaveDialog(owner, directory, suggestedName, dismiss)
}

// CanSelectMixedFilesAndDirs is the Windows answer with the reference's
// comment: "The FOS_PICKFOLDERS flag toggles between only files and only
// folders" (platform.rs:634-637).
func (win32DialogDriver) CanSelectMixedFilesAndDirs() bool { return false }

// defaultDialogDriver returns the real Win32 driver.
func defaultDialogDriver() DialogDriver { return win32DialogDriver{} }

// win32CredentialStore adapts internal/wincred to the CredentialStore
// seam (the platform.rs:829-926 port lives there).
type win32CredentialStore struct{}

func (win32CredentialStore) Write(url, username string, password []byte) error {
	return wincred.Store().Write(url, username, password)
}

func (win32CredentialStore) Read(url string) (string, []byte, bool, error) {
	entry, found, err := wincred.Store().Read(url)
	return entry.Username, entry.Password, found, err
}

func (win32CredentialStore) Delete(url string) error {
	return wincred.Store().Delete(url)
}

// defaultCredentialStore returns the real Windows Credential Manager
// store.
func defaultCredentialStore() CredentialStore { return win32CredentialStore{} }

// ---------------------------------------------------------------------------
// The no-interaction COM probe
// ---------------------------------------------------------------------------

// DialogProbeReport records the verified steps of the no-interaction
// dialog COM probe: every COM object creation, option round trip,
// folder/file-name configuration and allocation release performed
// WITHOUT showing any dialog.
type DialogProbeReport struct {
	// Steps lists each verified step in execution order.
	Steps []string
	// Err reports the first failure, if any.
	Err error
	// OptionsRoundTrip records the FILEOPENDIALOGOPTIONS value Windows
	// reported back after SetOptions.
	OptionsRoundTrip uint32
}

// ProbeFileDialogs exercises the pinned IFileDialog COM surface on the
// host's STA thread without showing any dialog: CoCreateInstance for
// both dialog classes, the SetOptions/GetOptions round trip, the
// SetOkButtonLabel/SetFolder/SetFileName/SetFileTypes configuration,
// an SHCreateItemFromParsingName allocation whose GetDisplayName result
// is freed with CoTaskMemFree, and explicit Releases. It returns the
// step report; the host's clean Stop (balanced OleUninitialize)
// afterwards is the apartment-release evidence. Runs on the host thread
// through runForegroundSync.
func ProbeFileDialogs(h *Host, directory string) DialogProbeReport {
	var report DialogProbeReport
	if !h.runForegroundSync(func() {
		probeFileDialogsOnHostThread(directory, &report)
	}) {
		report.Err = ErrHostStopped
	}
	return report
}

// probeFileDialogsOnHostThread is the probe body; it must run on the
// host thread (the STA owner).
func probeFileDialogsOnHostThread(directory string, report *DialogProbeReport) {
	step := func(format string, args ...any) {
		report.Steps = append(report.Steps, fmt.Sprintf(format, args...))
	}
	fail := func(err error) {
		if report.Err == nil {
			report.Err = err
		}
	}

	// Open dialog: creation, option round trip, OK label, release.
	dialog, err := coCreateInstance(&clsidFileOpenDialog, &iidIFileOpenDialog)
	if err != nil {
		fail(err)
		return
	}
	step("cocreate FileOpenDialog")
	defer comRelease(dialog)
	defer step("release FileOpenDialog")

	options := uintptr(fosFileMustExist | fosAllowMultiSelect | fosPickFolders)
	if _, err := comCall(dialog, 9 /* SetOptions */, options); err != nil {
		fail(err)
		return
	}
	step("set-options 0x%x", uint32(options))
	var got uintptr
	if _, err := comCall(dialog, 10 /* GetOptions */, uintptr(unsafe.Pointer(&got))); err != nil {
		fail(err)
		return
	}
	report.OptionsRoundTrip = uint32(got)
	if got != options {
		fail(fmt.Errorf("gpui: dialog option round trip: SetOptions(0x%x) then GetOptions() = 0x%x", uint32(options), uint32(got)))
		return
	}
	step("get-options 0x%x (round trip verified)", uint32(got))

	label, _ := syscall.UTF16PtrFromString("probe")
	if _, err := comCall(dialog, 18 /* SetOkButtonLabel */, uintptr(unsafe.Pointer(label))); err != nil {
		fail(err)
		return
	}
	step("set-ok-button-label")

	// Save dialog: creation, folder item, file name, filter, display
	// name with CoTaskMemFree, releases.
	save, err := coCreateInstance(&clsidFileSaveDialog, &iidIFileSaveDialog)
	if err != nil {
		fail(err)
		return
	}
	step("cocreate FileSaveDialog")
	defer comRelease(save)
	defer step("release FileSaveDialog")

	var item *comIFace
	if directory != "" {
		path16, err := syscall.UTF16PtrFromString(simplifyWindowsPath(directory))
		if err != nil {
			fail(err)
			return
		}
		if hr, _, _ := procSHCreateItemFromParsingName.Call(
			uintptr(unsafe.Pointer(path16)),
			0,
			uintptr(unsafe.Pointer(&iidIShellItem)),
			uintptr(unsafe.Pointer(&item)),
		); int32(hr) < 0 || item == nil {
			fail(fmt.Errorf("gpui: SHCreateItemFromParsingName(%s) failed with HRESULT 0x%08x", directory, uint32(hr)))
			return
		}
		step("shcreateitem-from-parsing-name")
		defer comRelease(item)
		defer step("release IShellItem")
		if _, err := comCall(save, 12 /* SetFolder */, uintptr(unsafe.Pointer(item))); err != nil {
			fail(err)
			return
		}
		step("set-folder")
	}

	name16, _ := syscall.UTF16PtrFromString("probe.txt")
	if _, err := comCall(save, 15 /* SetFileName */, uintptr(unsafe.Pointer(name16))); err != nil {
		fail(err)
		return
	}
	step("set-file-name")

	filter := allFilesFilter()
	if _, err := comCall(save, 4 /* SetFileTypes */, 1, uintptr(unsafe.Pointer(&filter))); err != nil {
		fail(err)
		return
	}
	step("set-file-types All files *.*")

	if item != nil {
		var name *uint16
		if _, err := comCall(item, 5 /* GetDisplayName */, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); err != nil {
			fail(err)
			return
		}
		displayed := ""
		if name != nil {
			displayed = syscall.UTF16ToString(unsafe.Slice(name, utf16Len(name)))
			procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
		}
		step("get-display-name %q (freed with CoTaskMemFree)", displayed)
		if !strings.EqualFold(displayed, strings.TrimSuffix(simplifyWindowsPath(directory), `\`)) {
			fail(fmt.Errorf("gpui: display name %q does not round trip the probe directory", displayed))
			return
		}
	}
}
