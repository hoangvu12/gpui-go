//go:build !windows

package gpui

import "fmt"

// The Win32 host stub for non-windows builds: the same public surface
// as win32host_windows.go with typed errors instead of windows, so the
// platform-neutral window layer (window.go) compiles everywhere. Real
// window hosting exists only on Windows in this slice (see
// docs/windows-platform-contract.md); the target is windows/amd64.

// Host is the platform host. On non-windows builds Start fails with
// ErrHostUnsupportedPlatform and every operation reports the same.
type Host struct {
	startedOnce bool
	stopped     bool
}

// NewHost creates a host. On non-windows builds it cannot start.
func NewHost() *Host { return &Host{} }

// Start reports that this platform has no Win32 host.
func (h *Host) Start() error { return ErrHostUnsupportedPlatform }

// Post panics: there is no foreground loop to drain work on this
// platform. It reports acceptance like the windows implementation.
func (h *Host) Post(f func()) bool {
	panic("gpui: no platform host on this build (the Win32 host requires windows)")
}

// Stop reports that there is no host loop to stop.
func (h *Host) Stop() error { return ErrHostUnsupportedPlatform }

// Wait reports that there is no host loop to wait for.
func (h *Host) Wait() error { return ErrHostUnsupportedPlatform }

// ThreadID reports 0: no foreground thread exists here.
func (h *Host) ThreadID() uint32 { return 0 }

// IsForegroundThread reports false: the Win32 foreground thread does
// not exist here.
func (h *Host) IsForegroundThread() bool { return false }

// DPIAwareness reports the unsupported record.
func (h *Host) DPIAwareness() string { return "unsupported-on-this-platform" }

// OpenWindow reports the unsupported platform.
func (h *Host) OpenWindow(opts WindowOptions) (WindowHandle, error) {
	return WindowHandle{}, ErrHostUnsupportedPlatform
}

// attach rejects application wiring on non-windows builds.
func (h *Host) attach(a *App) error {
	return fmt.Errorf("gpui: attaching a real window host requires a windows build")
}

// runForegroundSync has no foreground thread to marshal to; it only
// runs after a failed attach, so reaching it is a programming error.
// It reports acceptance like the windows implementation.
func (h *Host) runForegroundSync(f func()) bool {
	f()
	return true
}

// WindowHandle is the leased platform window handle. On non-windows
// builds handles are inert: there are no windows to lease.
type WindowHandle struct{}

// Hwnd reports 0: no HWND exists on this platform.
func (wh WindowHandle) Hwnd() uintptr { return 0 }

// ID reports 0: no host window identity exists here.
func (wh WindowHandle) ID() uint64 { return 0 }

// Alive reports false.
func (wh WindowHandle) Alive() bool { return false }

// Visible reports false.
func (wh WindowHandle) Visible() bool { return false }

// Title reports the unsupported platform.
func (wh WindowHandle) Title() (string, error) { return "", ErrHostUnsupportedPlatform }

// SetTitle is a no-op.
func (wh WindowHandle) SetTitle(title string) {}

// Show is a no-op.
func (wh WindowHandle) Show() {}

// Hide is a no-op.
func (wh WindowHandle) Hide() {}

// Activate is a no-op.
func (wh WindowHandle) Activate() {}

// Close is a no-op.
func (wh WindowHandle) Close() {}

// Bounds reports the unsupported platform.
func (wh WindowHandle) Bounds() (Bounds, error) { return Bounds{}, ErrHostUnsupportedPlatform }

// SetBounds is a no-op.
func (wh WindowHandle) SetBounds(bounds Bounds) {}

// ScaleFactor reports the unsupported platform.
func (wh WindowHandle) ScaleFactor() (float32, error) { return 0, ErrHostUnsupportedPlatform }
