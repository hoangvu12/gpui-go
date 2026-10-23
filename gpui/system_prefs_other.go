//go:build !windows

package gpui

// The non-Windows system-preferences stubs (browser-core ticket 27):
// the record types, the derivation and the App surface are platform
// neutral (system_prefs.go); only the OS reads and the observer wiring
// are Windows. The target is windows/amd64
// (docs/windows-platform-contract.md).

// SystemPreferences answers the documented default record (a test
// application's answer): the platform surface does not exist here.
func (h *Host) SystemPreferences() SystemPreferences { return defaultSystemPreferences() }

// SystemColors answers the documented default colours.
func (h *Host) SystemColors() SystemColors { return defaultSystemColors() }

// OnSystemPreferencesChanged is a no-op: no platform messages exist.
func (h *Host) OnSystemPreferencesChanged(cb func(SystemPreferences, SystemColors)) {}
