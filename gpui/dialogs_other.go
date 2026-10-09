//go:build !windows

package gpui

import "fmt"

// Non-windows stubs for ticket25's platform seams: the same public
// surface as dialogs_windows.go with typed errors instead of Win32 COM
// (the selected platform contract targets windows; see
// docs/windows-platform-contract.md).

type stubDialogDriver struct{}

// PromptForPaths reports the unsupported platform.
func (stubDialogDriver) PromptForPaths(owner uintptr, options PathPromptOptions, dismiss func(d DialogDismissal)) PathsOutcome {
	return PathsOutcome{Err: fmt.Errorf("gpui: native path prompts require a windows build")}
}

// PromptForNewPath reports the unsupported platform.
func (stubDialogDriver) PromptForNewPath(owner uintptr, directory, suggestedName string, dismiss func(d DialogDismissal)) PathOutcome {
	return PathOutcome{Err: fmt.Errorf("gpui: native path prompts require a windows build")}
}

// CanSelectMixedFilesAndDirs reports false (no platform dialogs here).
func (stubDialogDriver) CanSelectMixedFilesAndDirs() bool { return false }

// defaultDialogDriver returns the stub driver.
func defaultDialogDriver() DialogDriver { return stubDialogDriver{} }

type stubCredentialStore struct{}

// Write reports the unsupported platform.
func (stubCredentialStore) Write(url, username string, password []byte) error {
	return fmt.Errorf("gpui: the platform credential store requires a windows build")
}

// Read reports the unsupported platform.
func (stubCredentialStore) Read(url string) (string, []byte, bool, error) {
	return "", nil, false, fmt.Errorf("gpui: the platform credential store requires a windows build")
}

// Delete reports the unsupported platform.
func (stubCredentialStore) Delete(url string) error {
	return fmt.Errorf("gpui: the platform credential store requires a windows build")
}

// defaultCredentialStore returns the stub store.
func defaultCredentialStore() CredentialStore { return stubCredentialStore{} }

// getActiveWindowHWND reports 0: no windows exist on this platform.
func getActiveWindowHWND() uintptr { return 0 }

// hostLoopExited reports true: there is no live host loop here.
func hostLoopExited(h *Host) bool { return h.stopped }

// DialogProbeReport on non-windows builds records the unsupported
// platform instead of probing COM (same shape as the windows file).
type DialogProbeReport struct {
	// Steps lists each verified step in execution order.
	Steps []string
	// Err reports the first failure, if any.
	Err error
	// OptionsRoundTrip records the FILEOPENDIALOGOPTIONS value Windows
	// reported back after SetOptions (always 0 here).
	OptionsRoundTrip uint32
}

// ProbeFileDialogs reports the unsupported platform; it never touches a
// host on non-windows builds.
func ProbeFileDialogs(h *Host, directory string) DialogProbeReport {
	return DialogProbeReport{
		Steps: []string{"probe unsupported on this platform"},
		Err:   fmt.Errorf("gpui: the dialog COM probe requires a windows build"),
	}
}
