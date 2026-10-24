package gpui

import (
	"flag"
	"fmt"
	"os"
	"sync"
)

// Off-screen test windows.
//
// Under `go test`, every real window opens parked far off the desktop
// so a test run never puts windows in front of the user. The window is
// still created, shown and composited normally (DWM keeps presenting an
// off-screen window, and it is neither minimized nor cloaked, so the
// frame loop keeps drawing it); only its position moves. Positions are
// shifted, not replaced, so a test that opens several windows keeps
// their relative layout and sizes.
//
// Two switches turn it off:
//   - GPUI_REAL_INPUT_TESTS=1 (internal/realinput): those tests drive the
//     real cursor and keyboard and need windows where the input lands.
//   - GPUI_TEST_ONSCREEN=1: shows test windows where they asked to be,
//     for debugging a test by eye.
//
// Outside a test binary the placement is untouched.

// offscreenTestOrigin is the device-pixel offset added to a test
// window's position: far beyond any monitor (the convention the
// browser-core real-window tests used per test before this default).
const offscreenTestOrigin = 20000

var (
	offscreenTestWindowsOnce sync.Once
	offscreenTestWindowsOn   bool
)

// offscreenTestPosition shifts a test window's requested device
// position off the desktop. A position that is already far outside any
// monitor (a test that parks its own window) is kept: shifting it again
// would overflow the signed 16-bit coordinates WM_MOVE carries.
func offscreenTestPosition(x, y int32) (int32, int32) {
	const alreadyOff = offscreenTestOrigin / 2
	if x >= alreadyOff || x <= -alreadyOff || y >= alreadyOff || y <= -alreadyOff {
		return x, y
	}
	return x + offscreenTestOrigin, y + offscreenTestOrigin
}

// reportOnscreenTestWindow is the diagnostic behind
// GPUI_TEST_REPORT_ONSCREEN=1: inside a test binary it logs every
// window that is shown, or moved while shown, onto a monitor, so a test
// that still puts a window in front of the user can be found. shown is
// true when the window is known to be becoming visible (WM_SHOWWINDOW).
func reportOnscreenTestWindow(w *hostWindow, shown bool) {
	if !reportOnscreenOn() {
		return
	}
	if !shown {
		if visible, _, _ := procIsWindowVisible.Call(w.hwnd); visible == 0 {
			return
		}
	}
	if monitor, _, _ := procMonitorFromWindow.Call(w.hwnd, uintptr(monitorDefaultToNull)); monitor == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "gpui: test window %q is on-screen at (%v, %v)\n", w.title, w.originX, w.originY)
}

var (
	reportOnscreenOnce sync.Once
	reportOnscreenFlag bool
)

func reportOnscreenOn() bool {
	reportOnscreenOnce.Do(func() {
		reportOnscreenFlag = flag.Lookup("test.v") != nil && os.Getenv("GPUI_TEST_REPORT_ONSCREEN") == "1"
	})
	return reportOnscreenFlag
}

// offscreenTestWindows reports whether real windows open off-screen:
// true inside a `go test` binary unless one of the switches is set.
func offscreenTestWindows() bool {
	offscreenTestWindowsOnce.Do(func() {
		// The testing package registers its flags before any test runs;
		// gpui does not import testing, so it looks the flag up instead.
		inTest := flag.Lookup("test.v") != nil
		offscreenTestWindowsOn = inTest &&
			os.Getenv("GPUI_REAL_INPUT_TESTS") != "1" &&
			os.Getenv("GPUI_TEST_ONSCREEN") != "1"
	})
	return offscreenTestWindowsOn
}
