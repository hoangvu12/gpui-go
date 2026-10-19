//go:build windows

package gpui

// Unit tests for the window minimum size (consumer gaps ticket 04,
// WindowOptions.MinSize): the WM_GETMINMAXINFO floor derivation at
// several scale factors, the zero-minimum fall-through and the
// per-dimension policy, plus the programmatic client-size clamp.
//
// These exercise the derivation math on fabricated hostWindow state;
// the real-window behavior (message delivery through the real window
// procedure, SetBounds clamping, DPI re-derivation) is proven in
// internal/winhostspec/minsize_test.go.
//
// Reference: crates/gpui_windows/src/events.rs
// handle_get_min_max_info_msg (pin 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a):
// ptMinTrackSize = min_size.scale(scale_factor) + border_offset per
// axis, Some(0) when a minimum exists, None (default processing)
// otherwise.

import (
	"testing"
	"unsafe"
)

// minMaxSentinel pre-fills a MINMAXINFO the way the system does before
// sending WM_GETMINMAXINFO: values the handler must not lower or
// disturb except where a minimum raises them.
func minMaxSentinel() minMaxInfo {
	info := minMaxInfo{
		ptMaxSize:      point{x: 1200, y: 800},
		ptMaxPosition:  point{x: -8, y: -8},
		ptMinTrackSize: point{x: 136, y: 39},
		ptMaxTrackSize: point{x: 1600, y: 1200},
	}
	return info
}

func TestHandleGetMinMaxInfoMsgDerivesFloorPerScale(t *testing.T) {
	// floor = int32(min * scale) + border, the reference's
	// `min_size.scale(scale_factor).as_f32() as i32 + border_offset`
	// per axis.
	cases := []struct {
		scale             float32
		borderW, borderH  int32
		wantTrackX, wantY int32
	}{
		{1, 8, 10, 600 + 8, 640 + 10},
		{1.25, 16, 20, 750 + 16, 800 + 20},
		{1.5, 24, 30, 900 + 24, 960 + 30},
		{1.75, 28, 35, 1050 + 28, 1120 + 35},
		{2, 32, 40, 1200 + 32, 1280 + 40},
	}
	for _, c := range cases {
		w := &hostWindow{
			scale:   c.scale,
			borderW: c.borderW,
			borderH: c.borderH,
			minSize: Size{Width: 600, Height: 640},
		}
		info := minMaxSentinel()
		result, handled := w.handleGetMinMaxInfoMsg(unsafe.Pointer(&info))
		if !handled {
			t.Fatalf("scale %v: WM_GETMINMAXINFO with a minimum must be handled", c.scale)
		}
		if result != 0 {
			t.Errorf("scale %v: result = %d, want 0 (Some(0))", c.scale, result)
		}
		if gotX, gotY := info.ptMinTrackSize.x, info.ptMinTrackSize.y; gotX != c.wantTrackX || gotY != c.wantY {
			t.Errorf("scale %v: ptMinTrackSize = (%d, %d), want (%d, %d)",
				c.scale, gotX, gotY, c.wantTrackX, c.wantY)
		}
		// The untouched fields keep the system's pre-filled values.
		if info.ptMaxSize != (point{x: 1200, y: 800}) ||
			info.ptMaxPosition != (point{x: -8, y: -8}) ||
			info.ptMaxTrackSize != (point{x: 1600, y: 1200}) ||
			info.ptReserved != (point{}) {
			t.Errorf("scale %v: handler disturbed fields it must not write: %+v", c.scale, info)
		}
	}
}

func TestHandleGetMinMaxInfoMsgZeroMinimumFallsThrough(t *testing.T) {
	w := &hostWindow{scale: 1.5, borderW: 24, borderH: 30}
	info := minMaxSentinel()
	_, handled := w.handleGetMinMaxInfoMsg(unsafe.Pointer(&info))
	if handled {
		t.Fatal("a window without a minimum must leave WM_GETMINMAXINFO to default processing (today's behavior)")
	}
	if info.ptMinTrackSize != (point{x: 136, y: 39}) {
		t.Fatalf("zero minimum disturbed ptMinTrackSize: %+v", info.ptMinTrackSize)
	}
}

func TestHandleGetMinMaxInfoMsgPerDimension(t *testing.T) {
	// A width-only minimum raises the width floor and leaves the
	// height's pre-filled value alone (upstream carries Option<Size>;
	// the Go zero-value option is per-dimension).
	w := &hostWindow{scale: 2, borderW: 8, borderH: 10, minSize: Size{Width: 600}}
	info := minMaxSentinel()
	_, handled := w.handleGetMinMaxInfoMsg(unsafe.Pointer(&info))
	if !handled {
		t.Fatal("a width-only minimum must still be handled")
	}
	if want := int32(600*2) + 8; info.ptMinTrackSize.x != want {
		t.Errorf("ptMinTrackSize.x = %d, want %d", info.ptMinTrackSize.x, want)
	}
	if info.ptMinTrackSize.y != 39 {
		t.Errorf("ptMinTrackSize.y = %d, want the pre-filled 39", info.ptMinTrackSize.y)
	}

	w = &hostWindow{scale: 2, borderW: 8, borderH: 10, minSize: Size{Height: 640}}
	info = minMaxSentinel()
	_, handled = w.handleGetMinMaxInfoMsg(unsafe.Pointer(&info))
	if !handled {
		t.Fatal("a height-only minimum must still be handled")
	}
	if info.ptMinTrackSize.x != 136 {
		t.Errorf("ptMinTrackSize.x = %d, want the pre-filled 136", info.ptMinTrackSize.x)
	}
	if want := int32(640*2) + 10; info.ptMinTrackSize.y != want {
		t.Errorf("ptMinTrackSize.y = %d, want %d", info.ptMinTrackSize.y, want)
	}
}

func TestClampClientDeviceSize(t *testing.T) {
	w := &hostWindow{scale: 1.5, minSize: Size{Width: 600, Height: 640}}
	// 600x640 logical at 1.5x is a 900x960 device-pixel floor.
	cases := []struct {
		cx, cy       int32
		wantX, wantY int32
	}{
		{900, 960, 900, 960},     // at the floor
		{1200, 1024, 1200, 1024}, // above the floor
		{500, 1000, 900, 1000},   // width below the floor
		{1000, 500, 1000, 960},   // height below the floor
		{200, 200, 900, 960},     // both below the floor
	}
	for _, c := range cases {
		gotX, gotY := w.clampClientDeviceSize(c.cx, c.cy)
		if gotX != c.wantX || gotY != c.wantY {
			t.Errorf("clampClientDeviceSize(%d, %d) = (%d, %d), want (%d, %d)",
				c.cx, c.cy, gotX, gotY, c.wantX, c.wantY)
		}
	}

	// A window without a minimum: the clamp is the identity (today's
	// behavior; SetBounds(200x200) stays 200x200).
	plain := &hostWindow{scale: 1.5}
	if gotX, gotY := plain.clampClientDeviceSize(200, 200); gotX != 200 || gotY != 200 {
		t.Errorf("zero-minimum clamp(200, 200) = (%d, %d), want (200, 200)", gotX, gotY)
	}

	// Per-dimension minimum: the zero dimension is unconstrained.
	partial := &hostWindow{scale: 2, minSize: Size{Width: 600}}
	if gotX, gotY := partial.clampClientDeviceSize(200, 100); gotX != 1200 || gotY != 100 {
		t.Errorf("width-only clamp(200, 100) = (%d, %d), want (1200, 100)", gotX, gotY)
	}
}
