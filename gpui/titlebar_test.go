package gpui

import "testing"

// TestWindowControlHitTestCodeMapping is events.rs:978-988: each window
// control area's Win32 hit-test code under the movable / resizable /
// minimizable flags.
func TestWindowControlHitTestCodeMapping(t *testing.T) {
	const none = ^uintptr(0) // no answer: DefWindowProc decides
	cases := []struct {
		area                            WindowControlArea
		movable, resizable, minimizable bool
		want                            uintptr
	}{
		{WindowControlAreaDrag, true, true, true, htCaption},
		{WindowControlAreaDrag, false, true, true, none},
		{WindowControlAreaClose, true, true, true, htClose},
		{WindowControlAreaClose, false, false, false, htClose},
		{WindowControlAreaMax, true, true, true, htMaxButton},
		{WindowControlAreaMax, false, true, false, htMaxButton},
		{WindowControlAreaMax, true, false, true, htCaption},
		{WindowControlAreaMax, false, false, true, htNowhere},
		{WindowControlAreaMin, true, true, true, htMinButton},
		{WindowControlAreaMin, false, false, true, htMinButton},
		{WindowControlAreaMin, true, true, false, htCaption},
		{WindowControlAreaMin, false, true, false, htNowhere},
	}
	for _, c := range cases {
		code, ok := windowControlHitTestCode(c.area, c.movable, c.resizable, c.minimizable)
		got := code
		if !ok {
			got = none
		}
		if got != c.want {
			t.Errorf("%v movable=%v resizable=%v minimizable=%v: code %d, want %d",
				c.area, c.movable, c.resizable, c.minimizable, int64(got), int64(c.want))
		}
	}
}

// TestTitlebarOptionsHideOnlyWhenTransparent checks the option plumbing:
// a nil Titlebar keeps the system titlebar (the port's default; the pin
// hides it for None), and only AppearsTransparent hides it.
func TestTitlebarOptionsHideOnlyWhenTransparent(t *testing.T) {
	var opts WindowOptions
	if opts.Titlebar.hidesTitleBar() {
		t.Fatal("nil Titlebar hides the system titlebar")
	}
	opts.Titlebar = &TitlebarOptions{}
	if opts.Titlebar.hidesTitleBar() {
		t.Fatal("AppearsTransparent false hides the system titlebar")
	}
	opts.Titlebar.AppearsTransparent = true
	if !opts.Titlebar.hidesTitleBar() {
		t.Fatal("AppearsTransparent true keeps the system titlebar")
	}
}

// titlebarButton is a component using the Interactive mixin's
// WindowControlArea forwarding.
type titlebarButton struct {
	Component[*titlebarButton]
}

func (b *titlebarButton) IntoElement() AnyElement {
	return b.Interactivity().Refine(b.Style()).Extend(b.TakeChildren()).IntoElement()
}

// TestWindowControlAreasInTheRenderedFrame draws a client titlebar: a
// drag strip whose caption buttons occlude it (as Zed's caption buttons
// do), plus a nested area that does not occlude. The rendered frame's
// window-control hit test (window.rs:1928-1945) answers the button under
// the point, the drag strip elsewhere on it, nothing off the titlebar,
// and the outer (first inserted) area for a non-occluding nested one.
func TestWindowControlAreasInTheRenderedFrame(t *testing.T) {
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		button := func(id string, left float32, area WindowControlArea) AnyElement {
			return Div().ID(id).Absolute().Left(Px(left)).Top0().W(Px(40)).H(Px(32)).
				Occlude().WindowControlArea(area).IntoElement()
		}
		mixin := BindComponent(&titlebarButton{}, Div().ID("mixin")).
			Absolute().Left(Px(100)).Top0().W(Px(40)).H(Px(32)).
			Occlude().WindowControlArea(WindowControlAreaMin)
		return Div().Relative().SizeFull().Child(
			Div().ID("titlebar").Absolute().Left0().Top0().W(Px(400)).H(Px(32)).
				WindowControlArea(WindowControlAreaDrag).
				Child(button("max", 280, WindowControlAreaMax)).
				Child(button("close", 320, WindowControlAreaClose)).
				Child(mixin).
				// Not occluding: the drag strip stays in the hit test.
				Child(Div().ID("nested").Absolute().Left(Px(200)).Top0().W(Px(40)).H(Px(32)).
					WindowControlArea(WindowControlAreaClose)),
		).IntoElement()
	})

	cases := []struct {
		x, y float32
		want WindowControlArea
		ok   bool
	}{
		{10, 10, WindowControlAreaDrag, true},
		{110, 10, WindowControlAreaMin, true},
		{290, 10, WindowControlAreaMax, true},
		{330, 10, WindowControlAreaClose, true},
		{210, 10, WindowControlAreaDrag, true},
		{10, 100, 0, false},
	}
	for _, c := range cases {
		area, ok := f.window.windowControlAreaAt(Point{X: c.x, Y: c.y})
		if ok != c.ok || (ok && area != c.want) {
			t.Errorf("windowControlAreaAt(%v,%v) = %v, %v; want %v, %v", c.x, c.y, area, ok, c.want, c.ok)
		}
		if a, k := f.window.WindowControlAreaAt(Point{X: c.x, Y: c.y}); a != area || k != ok {
			t.Errorf("WindowControlAreaAt(%v,%v) = %v, %v; differs from the host's %v, %v", c.x, c.y, a, k, area, ok)
		}
	}

	// The next frame replaces the areas: an empty view has none.
	f.view.Update(f.app, func(v *interactView, cx *Context[interactView]) {
		v.build = func(*interactView, *Window, *Context[interactView]) AnyElement {
			return Div().SizeFull().IntoElement()
		}
		cx.Notify()
	})
	f.draw()
	if area, ok := f.window.windowControlAreaAt(Point{X: 10, Y: 10}); ok {
		t.Fatalf("after redraw without control areas: %v, true", area)
	}
}
