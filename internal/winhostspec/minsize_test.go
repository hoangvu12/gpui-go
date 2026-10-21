//go:build windows

// Real-window tests for the window minimum size (consumer gaps ticket
// 04, WindowOptions.MinSize). The Win32 host enforces the floor in
// WM_GETMINMAXINFO (reference pin 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a,
// crates/gpui_windows/src/events.rs handle_get_min_max_info_msg:
// ptMinTrackSize = min_size.scale(scale_factor) + border_offset) and in
// the programmatic sizing paths (SetBounds, initial placement).
//
// Environment expectation: like the rest of internal/winhostspec, these
// tests create and show real Win32 windows and FAIL on creation errors
// (interactive-session expectation). The drag floor is proven by
// querying WM_GETMINMAXINFO through the real window procedure:
// ptMinTrackSize is the value Windows enforces during an interactive
// resize (winuser.h: "an application can process this message to
// override the window's default ... minimum or maximum tracking
// size"), so the message answer IS the drag floor. Physical drag input
// is not injected (the repo never injects synthetic mouse input). The
// per-monitor DPI re-derivation uses a synthetic WM_DPICHANGED — the
// repo's established convention (internal/imespec), since real monitor
// transitions are not automatable in this session.
package winhostspec

import (
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Raw Win32 bindings for the fixtures: the expectations are computed
// from the window's real geometry (scale factor, window and client
// rects), independently of the host's internal state.
var (
	mmModUser32         = syscall.NewLazyDLL("user32.dll")
	mmProcSendMessageW  = mmModUser32.NewProc("SendMessageW")
	mmProcGetWindowRect = mmModUser32.NewProc("GetWindowRect")
	mmProcGetClientRect = mmModUser32.NewProc("GetClientRect")
)

// Win32 message constants (winuser.h).
const (
	mmWMGetMinMaxInfo = 0x0024
	mmWMDPICHanged    = 0x02E0
)

// mmPoint, mmRect and mmMinMaxInfo mirror Win32 POINT, RECT and
// MINMAXINFO.
type mmPoint struct{ x, y int32 }

type mmRect struct{ left, top, right, bottom int32 }

type mmMinMaxInfo struct {
	ptReserved     mmPoint
	ptMaxSize      mmPoint
	ptMaxPosition  mmPoint
	ptMinTrackSize mmPoint
	ptMaxTrackSize mmPoint
}

// mmSendMessage sends one message to a window and returns the window
// procedure's LRESULT.
func mmSendMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) int64 {
	ret, _, _ := mmProcSendMessageW.Call(hwnd, uintptr(message), wparam, lparam)
	return int64(ret)
}

// mmQueryMinMaxInfo sends WM_GETMINMAXINFO through the real window
// procedure (SendMessageW marshals to the host thread) and returns the
// MINMAXINFO as the procedure answered it plus the LRESULT. The buffer
// is pre-filled with sentinels below the floor. The system itself
// pre-fills MINMAXINFO before its real deliveries, and DefWindowProcW
// does not write the struct (verified on this machine: the fall-through
// path leaves the sentinels alone), so surviving sentinels prove the
// message was not answered with a floor.
func mmQueryMinMaxInfo(hwnd uintptr) (mmMinMaxInfo, int64) {
	var mmi mmMinMaxInfo
	mmi.ptMaxSize = mmPoint{x: 4096, y: 2048}
	mmi.ptMaxPosition = mmPoint{x: -7, y: -7}
	mmi.ptMinTrackSize = mmPoint{x: 8, y: 8}
	mmi.ptMaxTrackSize = mmPoint{x: 8192, y: 4096}
	res := mmSendMessage(hwnd, mmWMGetMinMaxInfo, 0, uintptr(unsafe.Pointer(&mmi)))
	return mmi, res
}

// mmWindowRect reads the outer window rect (GetWindowRect).
func mmWindowRect(hwnd uintptr) mmRect {
	var r mmRect
	mmProcGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// mmClientRect reads the client rect (GetClientRect).
func mmClientRect(hwnd uintptr) mmRect {
	var r mmRect
	mmProcGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// mmFrameExtents returns the outer-window minus client extent in
// device pixels: the border offset the minimum-size floor adds to a
// client-area minimum.
func mmFrameExtents(hwnd uintptr) (int32, int32) {
	wr, cr := mmWindowRect(hwnd), mmClientRect(hwnd)
	return (wr.right - wr.left) - (cr.right - cr.left), (wr.bottom - wr.top) - (cr.bottom - cr.top)
}

// mmNear reports whether got is within eps of want.
func mmNear(got, want float32, eps float32) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= eps
}

// TestWindowMinSizeClampsSetBounds proves the programmatic clamp:
// SetBounds below the floor clamps up to it (both dimensions and
// partially), and a window whose requested initial bounds are below
// the floor opens at the floor. The window starts above the floor so
// the clamped results cannot be confused with the creation size.
func TestWindowMinSizeClampsSetBounds(t *testing.T) {
	h := newHost(t)
	w := openShown(t, h, gpui.WindowOptions{
		Title:     "winhostspec min setbounds",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 700, Height: 720}},
		MinSize:   gpui.Size{Width: 600, Height: 640},
	})
	waitFor(t, 10*time.Second, "window to become visible", w.Visible)

	// Below the floor in both dimensions: clamps to the floor.
	w.SetBounds(gpui.Bounds{Size: gpui.Size{Width: 200, Height: 200}})
	waitFor(t, 10*time.Second, "the below-floor SetBounds to clamp", func() bool {
		b, err := w.Bounds()
		return err == nil && b.Size.Width >= 598 && b.Size.Height >= 638
	})
	b, err := w.Bounds()
	if err != nil {
		t.Fatalf("bounds after the clamped SetBounds: %v", err)
	}
	if !mmNear(b.Size.Width, 600, 2) || !mmNear(b.Size.Height, 640, 2) {
		t.Fatalf("below-floor SetBounds produced %fx%f logical, want the 600x640 floor (±2 rounding)", b.Size.Width, b.Size.Height)
	}
	t.Logf("SetBounds(200x200) clamped to %fx%f logical", b.Size.Width, b.Size.Height)

	// Partially below the floor: only the width clamps, the height
	// keeps the requested size.
	w.SetBounds(gpui.Bounds{Size: gpui.Size{Width: 500, Height: 700}})
	waitFor(t, 10*time.Second, "the partial SetBounds to clamp", func() bool {
		b, err := w.Bounds()
		return err == nil && mmNear(b.Size.Width, 600, 2)
	})
	b, err = w.Bounds()
	if err != nil {
		t.Fatalf("bounds after the partial SetBounds: %v", err)
	}
	if !mmNear(b.Size.Width, 600, 2) {
		t.Fatalf("partial SetBounds width = %f, want the 600 floor (±2 rounding)", b.Size.Width)
	}
	if !mmNear(b.Size.Height, 700, 2) {
		t.Fatalf("partial SetBounds height = %f, want the requested 700 (the height is above the floor)", b.Size.Height)
	}

	// Above the floor: unchanged (the clamp never lowers a size).
	w.SetBounds(gpui.Bounds{Size: gpui.Size{Width: 650, Height: 680}})
	waitFor(t, 10*time.Second, "the above-floor SetBounds to apply", func() bool {
		b, err := w.Bounds()
		return err == nil && mmNear(b.Size.Width, 650, 2) && mmNear(b.Size.Height, 680, 2)
	})

	// The initial placement clamps too: a window opened below its own
	// floor opens at the floor.
	w2 := openShown(t, h, gpui.WindowOptions{
		Title:     "winhostspec min initial",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 300, Height: 300}},
		MinSize:   gpui.Size{Width: 600, Height: 640},
	})
	waitFor(t, 10*time.Second, "the second window to become visible", w2.Visible)
	b2, err := w2.Bounds()
	if err != nil {
		t.Fatalf("bounds of the initially-clamped window: %v", err)
	}
	if !mmNear(b2.Size.Width, 600, 2) || !mmNear(b2.Size.Height, 640, 2) {
		t.Fatalf("initial bounds 300x300 with MinSize opened %fx%f, want the 600x640 floor (±2 rounding)", b2.Size.Width, b2.Size.Height)
	}
	t.Logf("initial bounds 300x300 with MinSize opened at %fx%f logical", b2.Size.Width, b2.Size.Height)

	w2.Close()
	w.Close()
	waitFor(t, 10*time.Second, "the windows to be destroyed", func() bool { return !w.Alive() && !w2.Alive() })
}

// TestWindowMinSizeDragFloor proves the interactive-resize floor: the
// real window procedure answers WM_GETMINMAXINFO with a ptMinTrackSize
// of exactly the scaled minimum plus the frame offset — the value
// Windows enforces while the user drags the resize frame. The rest of
// the struct keeps the system's pre-filled values (the reference
// returns Some(0) without calling DefWindowProc).
func TestWindowMinSizeDragFloor(t *testing.T) {
	h := newHost(t)
	w := openShown(t, h, gpui.WindowOptions{
		Title:     "winhostspec min drag floor",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 700, Height: 720}},
		MinSize:   gpui.Size{Width: 600, Height: 640},
	})
	waitFor(t, 10*time.Second, "window to become visible", w.Visible)

	scale, err := w.ScaleFactor()
	if err != nil {
		t.Fatalf("scale factor: %v", err)
	}
	borderW, borderH := mmFrameExtents(w.Hwnd())
	wantX := int32(600*scale) + borderW
	wantY := int32(640*scale) + borderH

	mmi, res := mmQueryMinMaxInfo(w.Hwnd())
	if res != 0 {
		t.Fatalf("the handled WM_GETMINMAXINFO returned %d, want 0 (the reference's Some(0))", res)
	}
	if mmi.ptMinTrackSize.x != wantX || mmi.ptMinTrackSize.y != wantY {
		t.Fatalf("WM_GETMINMAXINFO ptMinTrackSize = (%d, %d), want (%d, %d) = 600x640 logical × scale %v + frame (%d, %d)",
			mmi.ptMinTrackSize.x, mmi.ptMinTrackSize.y, wantX, wantY, scale, borderW, borderH)
	}
	t.Logf("drag floor at scale %v: ptMinTrackSize=(%d, %d) = 600x640 logical + frame (%d, %d)",
		scale, wantX, wantY, borderW, borderH)

	// The handled message returns 0 and leaves the rest of the struct
	// as the system pre-filled it (the reference's Some(0); only
	// ptMinTrackSize is overridden).
	if mmi.ptMaxSize != (mmPoint{x: 4096, y: 2048}) ||
		mmi.ptMaxPosition != (mmPoint{x: -7, y: -7}) ||
		mmi.ptMaxTrackSize != (mmPoint{x: 8192, y: 4096}) {
		t.Fatalf("the minimum-size handler disturbed fields it must not write: %+v", mmi)
	}

	w.Close()
	waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !w.Alive() })
}

// TestWindowWithoutMinSizeKeepsTodayBehavior is the regression guard
// for a window without MinSize: SetBounds(200x200) produces exactly
// 200x200 (the ticket's verified gap fact), and the WM_GETMINMAXINFO
// answer is DefWindowProc's own default — the exact call the procedure
// made before MinSize existed, so no floor is introduced.
func TestWindowWithoutMinSizeKeepsTodayBehavior(t *testing.T) {
	h := newHost(t)
	w := openShown(t, h, gpui.WindowOptions{
		Title:     "winhostspec no minimum",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 640, Height: 820}},
	})
	waitFor(t, 10*time.Second, "window to become visible", w.Visible)

	// The verified gap fact: a programmatic resize below any plausible
	// default floor applies exactly (nothing clamps it).
	w.SetBounds(gpui.Bounds{Size: gpui.Size{Width: 200, Height: 200}})
	waitFor(t, 10*time.Second, "the small SetBounds to apply", func() bool {
		b, err := w.Bounds()
		return err == nil && mmNear(b.Size.Width, 200, 2) && mmNear(b.Size.Height, 200, 2)
	})
	b, err := w.Bounds()
	if err != nil {
		t.Fatalf("bounds after the small SetBounds: %v", err)
	}
	if !mmNear(b.Size.Width, 200, 2) || !mmNear(b.Size.Height, 200, 2) {
		t.Fatalf("SetBounds(200x200) without MinSize produced %fx%f, want exactly 200x200 (±2 rounding)", b.Size.Width, b.Size.Height)
	}
	t.Logf("SetBounds(200x200) without MinSize produced exactly %fx%f logical", b.Size.Width, b.Size.Height)

	// The WM_GETMINMAXINFO answer is untouched: the zero-MinSize
	// fall-through goes straight to DefWindowProcW, which does not
	// write the struct (the system, not DefWindowProc, pre-fills
	// MINMAXINFO before its real deliveries), so the pre-filled
	// sentinels survive exactly as they did before MinSize existed.
	// No floor is introduced.
	mmi, res := mmQueryMinMaxInfo(w.Hwnd())
	if mmi.ptMinTrackSize != (mmPoint{x: 8, y: 8}) {
		t.Fatalf("WM_GETMINMAXINFO without MinSize answered ptMinTrackSize = (%d, %d), want the untouched pre-filled sentinel (8, 8) (result %d)",
			mmi.ptMinTrackSize.x, mmi.ptMinTrackSize.y, res)
	}
	if mmi.ptMaxSize != (mmPoint{x: 4096, y: 2048}) ||
		mmi.ptMaxPosition != (mmPoint{x: -7, y: -7}) ||
		mmi.ptMaxTrackSize != (mmPoint{x: 8192, y: 4096}) {
		t.Fatalf("a window without MinSize disturbed fields it must not write: %+v", mmi)
	}
	scale, _ := w.ScaleFactor()
	borderW, borderH := mmFrameExtents(w.Hwnd())
	if int32(8) >= int32(600*scale)+borderW || int32(8) >= int32(640*scale)+borderH {
		t.Fatalf("implausible frame extents (%d, %d) or scale %v for the sentinel check", borderW, borderH, scale)
	}
	t.Logf("no-MinSize WM_GETMINMAXINFO left the sentinels untouched (result %d): no floor at scale %v",
		res, scale)

	w.Close()
	waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !w.Alive() })
}

// TestWindowMinSizeFloorTracksDPIChange proves the clamp tracks the
// scale factor: a synthetic WM_DPICHANGED (the repo's convention —
// real monitor transitions are not automatable in this session)
// updates the scale, and the next WM_GETMINMAXINFO answer re-derives
// the device-pixel floor at the new scale.
func TestWindowMinSizeFloorTracksDPIChange(t *testing.T) {
	h := newHost(t)
	w := openShown(t, h, gpui.WindowOptions{
		Title:     "winhostspec min dpi track",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 700, Height: 720}},
		MinSize:   gpui.Size{Width: 600, Height: 640},
	})
	waitFor(t, 10*time.Second, "window to become visible", w.Visible)

	scale0, err := w.ScaleFactor()
	if err != nil {
		t.Fatalf("scale factor: %v", err)
	}
	// Pick a target scale different from the current one (144 DPI is
	// the repo's synthetic convention; fall back to 96 when the machine
	// already runs 1.5x).
	newScale := float32(144) / 96
	if scale0 == newScale {
		newScale = 1
	}
	targetDPI := uintptr(int32(newScale * 96))

	borderW0, borderH0 := mmFrameExtents(w.Hwnd())
	floor0, _ := mmQueryMinMaxInfo(w.Hwnd())
	want0X, want0Y := int32(600*scale0)+borderW0, int32(640*scale0)+borderH0
	if floor0.ptMinTrackSize.x != want0X || floor0.ptMinTrackSize.y != want0Y {
		t.Fatalf("floor before the DPI change = (%d, %d), want (%d, %d) at scale %v",
			floor0.ptMinTrackSize.x, floor0.ptMinTrackSize.y, want0X, want0Y, scale0)
	}

	// Synthetic WM_DPICHANGED: wparam low word = new DPI, lParam =
	// suggested RECT (the current outer rect scaled by the ratio, the
	// way the system proposes it).
	ratio := newScale / scale0
	wr := mmWindowRect(w.Hwnd())
	newW := int32(float32(wr.right-wr.left) * ratio)
	newH := int32(float32(wr.bottom-wr.top) * ratio)
	suggested := mmRect{left: wr.left, top: wr.top, right: wr.left + newW, bottom: wr.top + newH}
	mmSendMessage(w.Hwnd(), mmWMDPICHanged, targetDPI, uintptr(unsafe.Pointer(&suggested)))

	scale1, err := w.ScaleFactor()
	if err != nil || scale1 != newScale {
		t.Fatalf("scale after WM_DPICHANGED = %v (err %v), want %v", scale1, err, newScale)
	}

	// The next WM_GETMINMAXINFO re-derives the floor at the new scale.
	borderW1, borderH1 := mmFrameExtents(w.Hwnd())
	floor1, _ := mmQueryMinMaxInfo(w.Hwnd())
	want1X, want1Y := int32(600*newScale)+borderW1, int32(640*newScale)+borderH1
	if floor1.ptMinTrackSize.x != want1X || floor1.ptMinTrackSize.y != want1Y {
		t.Fatalf("floor after the DPI change = (%d, %d), want (%d, %d) at scale %v",
			floor1.ptMinTrackSize.x, floor1.ptMinTrackSize.y, want1X, want1Y, newScale)
	}
	if floor1.ptMinTrackSize.x == floor0.ptMinTrackSize.x && floor1.ptMinTrackSize.y == floor0.ptMinTrackSize.y {
		t.Fatalf("the floor did not change across the DPI change (%v -> %v): the re-derivation did not happen", scale0, newScale)
	}
	t.Logf("DPI case: scale %.2f -> %.2f, floor (%d, %d) -> (%d, %d) (synthetic WM_DPICHANGED; real monitor transitions are not automatable in this session)",
		scale0, newScale, floor0.ptMinTrackSize.x, floor0.ptMinTrackSize.y, floor1.ptMinTrackSize.x, floor1.ptMinTrackSize.y)

	// The programmatic clamp follows the new scale too: a below-floor
	// SetBounds clamps to the new device-pixel floor, which is the same
	// 600x640 logical minimum.
	w.SetBounds(gpui.Bounds{Size: gpui.Size{Width: 200, Height: 200}})
	waitFor(t, 10*time.Second, "the below-floor SetBounds to clamp at the new scale", func() bool {
		b, err := w.Bounds()
		return err == nil && b.Size.Width >= 598 && b.Size.Height >= 638
	})
	b, err := w.Bounds()
	if err != nil {
		t.Fatalf("bounds after the clamped SetBounds: %v", err)
	}
	if !mmNear(b.Size.Width, 600, 2) || !mmNear(b.Size.Height, 640, 2) {
		t.Fatalf("below-floor SetBounds after the DPI change produced %fx%f logical, want the 600x640 floor (±2 rounding)", b.Size.Width, b.Size.Height)
	}

	w.Close()
	waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !w.Alive() })
}
