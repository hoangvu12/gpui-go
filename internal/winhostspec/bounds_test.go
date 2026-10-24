//go:build windows

// Real-window tests for the window bounds' round trip: Bounds and
// SetBounds must agree exactly — SetBounds(Bounds()) is a no-op up to
// device rounding — because a saved-and-restored position must not
// drift. The dev command's restart handoff (browser-core ticket 66)
// saves Window.Bounds and restores with Window.SetBounds, so every
// disagreement moves the window once per restart.
//
// The placement math (windowRectFromClient) once split the frame
// extent symmetrically (the reference's calculate_window_rect), while
// a caption window's client area sits one caption lower than that
// split: every set landed the client that much below the request, and
// every Bounds()/SetBounds() round trip drifted by it (~12 logical px
// in Y on this frame). The placement now uses the measured
// client-offsets, so the expectations here are exact (±1 device
// rounding).
//
// Environment expectation: like the rest of internal/winhostspec,
// these tests create and show real Win32 windows and fail on creation
// errors; under `go test` they open far off the desktop
// (testwindows_windows.go), and the positions they request are already
// off-screen so the test-window shift leaves them alone. No input is
// injected.
package winhostspec

import (
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/hoangvu12/gpui-go/gpui"
)

var (
	btModUser32          = syscall.NewLazyDLL("user32.dll")
	btProcClientToScreen = btModUser32.NewProc("ClientToScreen")
)

// btNear reports whether got is within eps of want.
func btNear(got, want float32, eps float32) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= eps
}

// btClientOrigin returns the client area's screen position in device
// pixels, straight from the OS (independent of the host's state).
func btClientOrigin(hwnd uintptr) (int32, int32) {
	pt := mmPoint{0, 0}
	btProcClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	return pt.x, pt.y
}

// roundTripsOnce sets the given bounds, then reads the window back —
// the save/restore pair the dev state performs.
func roundTripsOnce(t *testing.T, w gpui.WindowHandle, want gpui.Bounds, what string) gpui.Bounds {
	t.Helper()
	w.SetBounds(want)
	waitFor(t, 10*time.Second, what, func() bool {
		got, err := w.Bounds()
		return err == nil && btNear(got.Origin.X, want.Origin.X, 1) &&
			btNear(got.Origin.Y, want.Origin.Y, 1) &&
			btNear(got.Size.Width, want.Size.Width, 1) && btNear(got.Size.Height, want.Size.Height, 1)
	})
	got, err := w.Bounds()
	if err != nil {
		t.Fatalf("%s: bounds: %v", what, err)
	}
	return got
}

// TestWindowBoundsRoundTripIsExact: a window lands on the position it
// asks for (creation and SetBounds, verified against the OS's own
// client-origin answer), and repeated Bounds()→SetBounds() round trips
// do not move it — the drift the dev command's restart handoff must
// not accumulate.
func TestWindowBoundsRoundTripIsExact(t *testing.T) {
	h := newHost(t)
	// Already off-screen, so the go-test window shift leaves the
	// requested position alone.
	requested := gpui.Bounds{
		Origin: gpui.Point{X: 20220, Y: 20180},
		Size:   gpui.Size{Width: 500, Height: 400},
	}
	w := openShown(t, h, gpui.WindowOptions{
		Title:     "winhostspec bounds roundtrip",
		Show:      true,
		Resizable: true,
		Bounds:    requested,
	})
	waitFor(t, 10*time.Second, "window to become visible", w.Visible)

	// Creation: the client origin is exactly the requested one, as the
	// OS itself reports it (device pixels, at the window's scale).
	scale, err := w.ScaleFactor()
	if err != nil {
		t.Fatalf("scale factor: %v", err)
	}
	waitFor(t, 10*time.Second, "the creation position to settle", func() bool {
		b, err := w.Bounds()
		return err == nil && btNear(b.Origin.X, requested.Origin.X, 1) && btNear(b.Origin.Y, requested.Origin.Y, 1)
	})
	if cx, cy := btClientOrigin(w.Hwnd()); err == nil {
		if wantX, wantY := int32(requested.Origin.X*scale), int32(requested.Origin.Y*scale); cx != wantX || cy != wantY {
			t.Fatalf("the OS reports the client origin at (%d, %d), want (%d, %d) — the requested position", cx, cy, wantX, wantY)
		}
	}

	// A save/restore pair, from the read bounds.
	first := roundTripsOnce(t, w, requested, "the first set to land")

	// Three consecutive round trips — a restart per cycle in dev use:
	// the position after the last is the position after the first (the
	// symmetric-split frame math this guards against moved the window
	// one caption per cycle).
	current := first
	for i := 0; i < 3; i++ {
		current = roundTripsOnce(t, w, current, "the round trip to be a no-op")
	}
	if !btNear(current.Origin.Y, first.Origin.Y, 1) || !btNear(current.Origin.X, first.Origin.X, 1) {
		t.Fatalf("round trips drifted the window: %v -> %v", first, current)
	}
	t.Logf("bounds round-tripped exactly: %v (scale %v)", current, scale)
}
