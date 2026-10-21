//go:build !windows

package gpui

// scheduleHostFrame has no platform frame source on non-windows builds
// (no real windows exist there; see win32host_other.go).
func scheduleHostFrame(w *Window) {}
