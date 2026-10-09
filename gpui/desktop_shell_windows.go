//go:build windows

package gpui

// Ticket26's shell-side desktop operations, ported from the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/platform.rs — open_url /
//     open_with_system / reveal_path (open_target, open_target_in_explorer),
//     restart (the deferred PowerShell launch and
//     encode_restart_arguments), app_path.
//   - crates/gpui_ce_util/src/lib.rs — get_powershell and new_std_command
//     (CREATE_NO_WINDOW).
//
// External launches are real OS actions: nothing in the test corpus
// launches a URL, opens a folder or restarts the app. The tests verify
// the call sites and argument marshaling through seams whose default is
// the real syscall; what was and was not really executed is recorded in
// the ticket.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Shell launch bindings
// ---------------------------------------------------------------------------

var (
	procShellExecuteW              = modShell32.NewProc("ShellExecuteW")
	procSHGetDesktopFolder         = modShell32.NewProc("SHGetDesktopFolder")
	procSHOpenFolderAndSelectItems = modShell32.NewProc("SHOpenFolderAndSelectItems")
	procCoTaskMemFreeShell         = modOle32.NewProc("CoTaskMemFree")

	// shellExecute is the ShellExecuteW seam: the tests swap it for a
	// recorder, the default is the real call.
	shellExecute = realShellExecute
)

// realShellExecute calls ShellExecuteW(NULL, verb, target, NULL, NULL,
// SW_SHOWDEFAULT) — platform.rs open_target. A result <= 32 is the
// documented failure range and reports the last OS error.
func realShellExecute(verb, target string) error {
	verb16, err := syscall.UTF16PtrFromString(verb)
	if err != nil {
		return fmt.Errorf("gpui: shell verb %q: %v", verb, err)
	}
	target16, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("gpui: shell target %q: %v", target, err)
	}
	r, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb16)),
		uintptr(unsafe.Pointer(target16)),
		0, 0, uintptr(swShowDefault))
	if r <= 32 {
		return fmt.Errorf("gpui: unable to open target %q: %v", target, callErr)
	}
	return nil
}

// OpenURL opens a URL with the system handler (platform.rs open_url:
// empty URLs return immediately; the launch is fire-and-forget with the
// error logged).
//
// Deviation: the reference spawns ShellExecuteW on the background
// executor; this port marshals it onto the host thread, whose OLE
// apartment ShellExecuteW requires, and which runs it between updates
// (the platform contract's rule that potentially pumping operations
// start after the current update unwinds — the same reason the
// reference defers restart to the foreground executor).
func (h *Host) OpenURL(url string) {
	if url == "" {
		return
	}
	target := url
	h.runForegroundAsync(func() {
		if err := shellExecute("open", target); err != nil {
			h.recordFault(fmt.Sprintf("opening url %q: %v", target, err))
		}
	})
}

// OpenWithSystem opens a path with the system handler (platform.rs
// open_with_system).
func (h *Host) OpenWithSystem(path string) {
	if path == "" {
		return
	}
	target := path
	h.runForegroundAsync(func() {
		if err := shellExecute("open", target); err != nil {
			h.recordFault(fmt.Sprintf("opening %q with system: %v", target, err))
		}
	})
}

// RevealPath reveals a path in the shell (platform.rs reveal_path):
// open the parent folder with the item selected
// (open_target_in_explorer), falling back to opening the parent folder
// itself when SHOpenFolderAndSelectItems reports the documented
// ERROR_FILE_NOT_FOUND quirk. Empty paths return immediately.
func (h *Host) RevealPath(path string) {
	if path == "" {
		return
	}
	target := path
	h.runForegroundAsync(func() {
		if err := revealInExplorer(target); err != nil {
			h.recordFault(fmt.Sprintf("revealing path %q in explorer: %v", target, err))
		}
	})
}

// revealInExplorer is the open_target_in_explorer port: resolve the
// parent folder and the target to ITEMIDLISTs through the shell's
// desktop folder, then SHOpenFolderAndSelectItems with the item
// highlighted. A missing parent is an error (the reference's "No parent
// folder found" context). The shell call sits behind the shellReveal
// seam so tests verify the marshaling without opening Explorer.
func revealInExplorer(path string) error {
	dir, ok := filepathParent(path)
	if !ok {
		return fmt.Errorf("gpui: no parent folder found for %q", path)
	}
	return shellReveal(dir, path)
}

// shellReveal is the reveal seam: the default performs the real COM
// reveal; tests record the marshaled arguments instead.
var shellReveal = realRevealInExplorer

// realRevealInExplorer performs the real COM reveal (SHGetDesktopFolder
// -> IShellFolder::ParseDisplayName x2 -> SHOpenFolderAndSelectItems,
// with open_target(parent) as the ERROR_FILE_NOT_FOUND fallback).
func realRevealInExplorer(dir, target string) error {
	folder, err := shGetDesktopFolder()
	if err != nil {
		return err
	}
	defer folder.release()

	dirItem, err := folder.parseDisplayName(dir)
	if err != nil {
		return err
	}
	defer procCoTaskMemFreeShell.Call(dirItem)

	fileItem, err := folder.parseDisplayName(target)
	if err != nil {
		return err
	}
	defer procCoTaskMemFreeShell.Call(fileItem)

	hr, _, callErr := procSHOpenFolderAndSelectItems.Call(
		dirItem, 1, uintptr(unsafe.Pointer(&fileItem)), 0)
	return revealSelectResult(hr, callErr, dir)
}

// revealSelectResult maps SHOpenFolderAndSelectItems' result: success is
// nil; the documented ERROR_FILE_NOT_FOUND quirk falls back to opening
// the parent folder (which cannot select, but shows the folder); other
// failures are errors.
func revealSelectResult(hr uintptr, callErr error, dir string) error {
	if int32(hr) >= 0 {
		return nil
	}
	if uintptr(uint32(hr)) == hresultFileNotFoundWin32 {
		// On some systems the select call fails with "file not found"
		// although the file exists; ShellExecute on the parent folder
		// works then (it just does not select).
		if err := shellExecute("open", dir); err != nil {
			return fmt.Errorf("gpui: opening target parent folder: %w", err)
		}
		return nil
	}
	return fmt.Errorf("gpui: cannot open target path: 0x%08x %v", uint32(hr), callErr)
}

// shellFolder is the IShellFolder COM carrier for the desktop folder.
// The out parameter of SHGetDesktopFolder is the interface pointer
// itself; the vtable pointer is the object's first word, so the carrier
// keeps the object as an unsafe.Pointer (vet-provable) and derives the
// vtable from it. The array is sized for IShellFolder's 13 slots.
type shellFolder struct {
	obj  unsafe.Pointer
	vtbl *[16]uintptr
}

// shGetDesktopFolder returns the shell desktop folder
// (SHGetDesktopFolder).
func shGetDesktopFolder() (*shellFolder, error) {
	folder := &shellFolder{}
	r, _, callErr := procSHGetDesktopFolder.Call(uintptr(unsafe.Pointer(&folder.obj)))
	if int32(r) < 0 {
		return nil, fmt.Errorf("gpui: SHGetDesktopFolder failed: 0x%08x %v", uint32(r), callErr)
	}
	if folder.obj == nil {
		return nil, fmt.Errorf("gpui: SHGetDesktopFolder returned a null interface")
	}
	folder.vtbl = *(**[16]uintptr)(folder.obj)
	return folder, nil
}

// parseDisplayName resolves one display name to an ITEMIDLIST
// (IShellFolder::ParseDisplayName, vtable slot 3: hwnd, bind context,
// name, eaten, pidl out, flags out).
func (f *shellFolder) parseDisplayName(name string) (uintptr, error) {
	name16, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("gpui: shell path %q: %v", name, err)
	}
	var pidl uintptr
	var eaten uint32
	var flagsOut uint32
	r, _, callErr := syscall.SyscallN(f.vtbl[3],
		uintptr(f.obj), 0, 0,
		uintptr(unsafe.Pointer(name16)),
		uintptr(unsafe.Pointer(&eaten)),
		uintptr(unsafe.Pointer(&pidl)),
		uintptr(unsafe.Pointer(&flagsOut)))
	if int32(r) < 0 {
		return 0, fmt.Errorf("gpui: ParseDisplayName(%q) failed: 0x%08x %v", name, uint32(r), callErr)
	}
	return pidl, nil
}

// release drops the interface (IUnknown::Release, slot 2).
func (f *shellFolder) release() {
	if f != nil && f.vtbl != nil {
		syscall.SyscallN(f.vtbl[2], uintptr(f.obj))
	}
}

// ---------------------------------------------------------------------------
// Restart (the deferred-launch path)
// ---------------------------------------------------------------------------

// restartScript is the reference's PowerShell restart script verbatim
// (platform.rs restart): it waits for the old pid to exit, then
// launches the executable with the recorded argument list.
const restartScript = `
$pidToWaitFor = $env:ZED_RESTART_PID
$exePath = $env:ZED_RESTART_EXECUTABLE
$argumentList = $env:ZED_RESTART_ARGUMENTS

[Environment]::SetEnvironmentVariable("ZED_RESTART_PID", $null)
[Environment]::SetEnvironmentVariable("ZED_RESTART_EXECUTABLE", $null)
[Environment]::SetEnvironmentVariable("ZED_RESTART_ARGUMENTS", $null)

while ($true) {
    $process = Get-Process -Id $pidToWaitFor -ErrorAction SilentlyContinue
    if (-not $process) {
        if ([string]::IsNullOrEmpty($argumentList)) {
            Start-Process -FilePath $exePath
        } else {
            Start-Process -FilePath $exePath -ArgumentList $argumentList
        }
        break
    }
    Start-Sleep -Seconds 0.1
}
`

// restartLauncher is the restart launch seam: the default really spawns
// the PowerShell script; tests swap in a recorder so no process is
// launched and no restart happens.
var restartLauncher = realRestartLauncher

// realRestartLauncher spawns the restart script through PowerShell with
// CREATE_NO_WINDOW (gpui_ce_util new_std_command) and the
// ZED_RESTART_* environment records.
func realRestartLauncher(powershell string, env []string) error {
	cmd := exec.Command(powershell, "-command", restartScript)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	return cmd.Start()
}

// Restart relaunches the application after this process exits
// (platform.rs restart). The launch is DEFERRED to the foreground
// queue — the reference defers it to the foreground executor so
// CreateProcessW's message pumping cannot re-enter a live AppCell
// borrow (the same reentrancy the Go update boundary protects); on a
// successful spawn the message loop quits, a failed spawn records the
// fault and the app keeps running.
func (h *Host) Restart(binaryPath string, arguments []string) error {
	appPath := binaryPath
	if appPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("gpui: restart could not resolve the app path: %w", err)
		}
		appPath = exe
	}
	powershell, err := findPowerShell()
	if err != nil {
		h.recordFault(fmt.Sprintf("failed to restart: %v", err))
		return err
	}
	pid := os.Getpid()
	encoded := encodeRestartArguments(arguments)
	env := []string{
		fmt.Sprintf("ZED_RESTART_PID=%d", pid),
		"ZED_RESTART_EXECUTABLE=" + appPath,
		"ZED_RESTART_ARGUMENTS=" + encoded,
	}
	shell := powershell
	posted := h.Post(func() {
		if err := restartLauncher(shell, env); err != nil {
			h.recordFault(fmt.Sprintf("failed to spawn restart script: %v", err))
			return
		}
		procPostQuitMessage.Call(0)
	})
	if !posted {
		return ErrHostStopped
	}
	return nil
}

// encodeRestartArguments quotes each argument according to the Windows
// argv parsing rules so Start-Process receives the intended list
// (platform.rs encode_restart_arguments): backslashes double before
// quotes (and at the tail), and quotes escape.
func encodeRestartArguments(arguments []string) string {
	var encoded []uint16
	for index, argument := range arguments {
		if index > 0 {
			encoded = append(encoded, ' ')
		}
		encoded = append(encoded, '"')
		backslashCount := 0
		for _, unit := range utf16Units(argument) {
			if unit == '\\' {
				backslashCount++
				continue
			}
			if unit == '"' {
				encoded = append(encoded, repeatUnits('\\', backslashCount*2+1)...)
			} else {
				encoded = append(encoded, repeatUnits('\\', backslashCount)...)
			}
			backslashCount = 0
			encoded = append(encoded, unit)
		}
		encoded = append(encoded, repeatUnits('\\', backslashCount*2)...)
		encoded = append(encoded, '"')
	}
	return utf16ToString(encoded)
}

// utf16Units converts a UTF-8 string to its UTF-16 units.
func utf16Units(s string) []uint16 {
	units, _ := syscall.UTF16FromString(s)
	if n := len(units); n > 0 && units[n-1] == 0 {
		units = units[:n-1]
	}
	return units
}

// utf16ToString converts UTF-16 units to a UTF-8 string.
func utf16ToString(units []uint16) string {
	return syscall.UTF16ToString(units)
}

// repeatUnits builds a run of one unit.
func repeatUnits(unit uint16, count int) []uint16 {
	out := make([]uint16, count)
	for i := range out {
		out[i] = unit
	}
	return out
}

// findPowerShell locates a PowerShell executable (gpui_ce_util
// get_powershell) in the reference's location order: pwsh in
// ProgramFiles (stable), pwsh in the alternate ProgramFiles(x86)
// (stable), the MSIX app alias, ProgramFiles preview, the MSIX preview
// alias, the alternate preview, scoop, dotnet tools, PATH lookup for
// pwsh then powershell, and finally Windows PowerShell.
func findPowerShell() (string, error) {
	// amd64 target: the alternate install root is ProgramFiles(x86)
	// (find_pwsh_in_programfiles picks the env var by pointer width).
	primary := ""
	if v := os.Getenv("ProgramFiles"); v != "" {
		primary = filepath.Join(v, "PowerShell")
	}
	alternate := ""
	if v := os.Getenv("ProgramFiles(x86)"); v != "" {
		alternate = filepath.Join(v, "PowerShell")
	}
	msix := ""
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		msix = filepath.Join(v, "Microsoft", "WindowsApps")
	}

	if primary != "" {
		if path, ok := newestPwshInDir(primary, false); ok {
			return path, nil
		}
	}
	if alternate != "" {
		if path, ok := newestPwshInDir(alternate, false); ok {
			return path, nil
		}
	}
	if msix != "" {
		if path := filepath.Join(msix, "Microsoft.PowerShell_8wekyb3d8bbwe", "pwsh.exe"); fileExists(path) {
			return path, nil
		}
	}
	if primary != "" {
		if path, ok := newestPwshInDir(primary, true); ok {
			return path, nil
		}
	}
	if msix != "" {
		if path := filepath.Join(msix, "Microsoft.PowerShellPreview_8wekyb3d8bbwe", "pwsh.exe"); fileExists(path) {
			return path, nil
		}
	}
	if alternate != "" {
		if path, ok := newestPwshInDir(alternate, true); ok {
			return path, nil
		}
	}
	if user := os.Getenv("USERPROFILE"); user != "" {
		if path := filepath.Join(user, "scoop", "shims", "pwsh.exe"); fileExists(path) {
			return path, nil
		}
		if path := filepath.Join(user, ".dotnet", "tools", "pwsh.exe"); fileExists(path) {
			return path, nil
		}
	}
	if path, ok := lookPath("pwsh.exe"); ok {
		return path, nil
	}
	if path, ok := lookPath("powershell.exe"); ok {
		return path, nil
	}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		if path := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"); fileExists(path) {
			return path, nil
		}
	}
	return "", ErrPowerShellUnavailable
}

// newestPwshInDir picks the highest-versioned pwsh.exe under one
// PowerShell install dir (find_pwsh_in_programfiles: version-numbered
// directories, optionally the *-preview spelling).
func newestPwshInDir(base string, preview bool) (string, bool) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", false
	}
	type versioned struct {
		version uint32
		path    string
	}
	var found []versioned
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		versionPart := name
		if strings.Contains(name, "-") {
			if !preview {
				continue
			}
			dash := strings.Index(name, "-")
			if name[dash+1:] != "preview" {
				continue
			}
			versionPart = name[:dash]
		} else if preview {
			continue
		}
		var version uint32
		if _, err := fmt.Sscanf(versionPart, "%d", &version); err != nil {
			continue
		}
		path := filepath.Join(base, name, "pwsh.exe")
		if fileExists(path) {
			found = append(found, versioned{version: version, path: path})
		}
	}
	if len(found) == 0 {
		return "", false
	}
	sort.Slice(found, func(i, j int) bool { return found[i].version > found[j].version })
	return found[0].path, true
}

// fileExists reports a regular-file existence check.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// lookPath searches PATH (the reference uses the which crate).
func lookPath(name string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return path, true
}

// AppPath returns the current executable's path (platform.rs
// app_path: std::env::current_exe).
func (h *Host) AppPath() (string, error) {
	return os.Executable()
}
