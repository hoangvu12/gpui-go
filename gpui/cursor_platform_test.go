package gpui

// Base ticket 39: the hovered element's cursor reaches the platform
// boundary of reset_cursor_style (window.rs:5684-5693) on a test window.
// The window-owns-cursor and set-cursor seams (interact.go) are swapped
// for recorders, so no real window, input or focus is involved; the
// host half (Host.SetCursorStyle -> WM_GPUI_CURSOR_STYLE_CHANGED ->
// WM_SETCURSOR) is the real-window corpus's.

import (
	"slices"
	"testing"
)

// cursorPlatformRecorder replaces the platform seams for one test.
type cursorPlatformRecorder struct {
	hovered bool
	sent    []CursorStyle
}

func recordPlatformCursor(t *testing.T, hovered bool) *cursorPlatformRecorder {
	t.Helper()
	r := &cursorPlatformRecorder{hovered: hovered}
	prevOwns, prevSet := windowOwnsCursor, setPlatformCursorStyle
	windowOwnsCursor = func(*Window) bool { return r.hovered }
	setPlatformCursorStyle = func(_ *App, style CursorStyle) { r.sent = append(r.sent, style) }
	t.Cleanup(func() { windowOwnsCursor, setPlatformCursorStyle = prevOwns, prevSet })
	return r
}

func (r *cursorPlatformRecorder) last() (CursorStyle, bool) {
	if len(r.sent) == 0 {
		return CursorArrow, false
	}
	return r.sent[len(r.sent)-1], true
}

// cursorBoard lays out one element per cursor builder the Base
// components use: pointer (U:text/inline_flow.rs:274), text
// (U:input/base/state.rs:4487), column and row resize
// (U:resizable/resize_handle.rs:264-300), and the default arrow over a
// pointer parent (U:input/base/element.rs:1435).
func cursorBoard(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
	return Div().Relative().SizeFull().
		Child(Div().ID("pointer").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).CursorPointer()).
		Child(Div().ID("text").Absolute().Left(Px(100)).Top(Px(0)).Size(Px(50)).CursorText()).
		Child(Div().ID("col").Absolute().Left(Px(200)).Top(Px(0)).Size(Px(50)).CursorColResize()).
		Child(Div().ID("row").Absolute().Left(Px(300)).Top(Px(0)).Size(Px(50)).CursorRowResize()).
		Child(Div().ID("token-host").Absolute().Left(Px(0)).Top(Px(100)).Size(Px(100)).CursorPointer().
			Child(Div().ID("token").Size(Px(40)).CursorDefault())).
		IntoElement()
}

// TestCursorHoveredElementReachesThePlatform: on a window that owns the
// mouse cursor, every hit-test change hands the hovered element's cursor
// to the platform, and leaving every request restores the arrow.
func TestCursorHoveredElementReachesThePlatform(t *testing.T) {
	rec := recordPlatformCursor(t, true)
	f := newInteractFixture(t, cursorBoard)
	cases := []struct {
		name string
		x, y float32
		want CursorStyle
	}{
		{"pointer", 10, 10, CursorPointingHand},
		{"text", 110, 10, CursorIBeam},
		{"col-resize", 210, 10, CursorResizeColumn},
		{"row-resize", 310, 10, CursorResizeRow},
		{"default over a pointer parent", 10, 110, CursorArrow},
		{"pointer parent", 70, 170, CursorPointingHand},
		{"nothing", 380, 280, CursorArrow},
	}
	for _, tc := range cases {
		f.move(tc.x, tc.y)
		got, ok := rec.last()
		if !ok || got != tc.want {
			t.Fatalf("%s: platform cursor = %v (sent %v), want %v", tc.name, got, ok, tc.want)
		}
		if resolved, _ := WindowCursorStyle(f.window); resolved != tc.want {
			t.Fatalf("%s: WindowCursorStyle = %v, want %v", tc.name, resolved, tc.want)
		}
	}
}

// TestCursorAFrameHandsItsCursorToThePlatform: the end of a frame resets
// the cursor too (window.rs:3277), so a hovered element whose cursor
// changes without a mouse move reaches the platform on the next frame.
func TestCursorAFrameHandsItsCursorToThePlatform(t *testing.T) {
	rec := recordPlatformCursor(t, true)
	text := false
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		d := Div().ID("swap").Size(Px(100))
		if text {
			d.CursorText()
		} else {
			d.CursorPointer()
		}
		return Div().SizeFull().Child(d).IntoElement()
	})
	f.move(10, 10)
	if got, _ := rec.last(); got != CursorPointingHand {
		t.Fatalf("cursor after the move = %v, want PointingHand", got)
	}
	text = true
	f.draw()
	if got, _ := rec.last(); got != CursorIBeam {
		t.Fatalf("cursor after the frame = %v, want IBeam (sent %v)", got, rec.sent)
	}
}

// TestCursorAWindowThatDoesNotOwnTheCursorLeavesThePlatformAlone: the
// pin sets the cursor only from the hovered window (window.rs:5685-5686).
func TestCursorAWindowThatDoesNotOwnTheCursorLeavesThePlatformAlone(t *testing.T) {
	rec := recordPlatformCursor(t, false)
	f := newInteractFixture(t, cursorBoard)
	f.move(10, 10)
	f.move(110, 10)
	f.draw()
	if len(rec.sent) != 0 {
		t.Fatalf("an un-hovered window sent %v to the platform", rec.sent)
	}
	if got, _ := WindowCursorStyle(f.window); got != CursorIBeam {
		t.Fatalf("WindowCursorStyle = %v, want the resolved IBeam", got)
	}
}

// TestCursorAMoveInsideOneElementSendsNothingNew: the reset runs when
// the hit test changes (window.rs:6083-6086), not on every move.
func TestCursorAMoveInsideOneElementSendsNothingNew(t *testing.T) {
	rec := recordPlatformCursor(t, true)
	f := newInteractFixture(t, cursorBoard)
	f.move(10, 10)
	n := len(rec.sent)
	f.move(20, 20)
	f.move(30, 30)
	if len(rec.sent) != n {
		t.Fatalf("moves inside one hitbox sent %v", rec.sent[n:])
	}
	if !slices.Contains(rec.sent, CursorPointingHand) {
		t.Fatalf("sent %v, want the pointer", rec.sent)
	}
}
