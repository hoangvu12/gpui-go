package gpui

// Browser-core ticket 24 (spec story 71): the tooltip hook — the hover
// and keyboard-focus triggers with Chrome's interest delays, the grace
// period between the element and the tooltip, the hint-layer entry,
// the default look's placement and the Escape close — tested through
// the public Tooltip/TooltipView API on the deterministic test window.
// The Chrome-parity twins of these tests are
// internal/parity/fixtures/tooltip.

import (
	"testing"
	"time"
)

// tooltipView renders the tooltip fixture tree: one tooltip-carrying
// box (b1) with the default or custom look, and a second focusable box
// (anchor) for the focus-trigger's blur target.
type tooltipView struct {
	b1, anchor    FocusHandle
	focusHandles  bool
	customContent bool
	// dropB1 stops rendering b1 (the element-gone reconciliation's
	// test).
	dropB1 bool
	log    []string
}

func (v *tooltipView) ensureFocusHandles(w *Window) {
	if v.focusHandles {
		return
	}
	v.b1 = NewFocusHandle(w).TabStop(true)
	v.anchor = NewFocusHandle(w).TabStop(true)
	v.focusHandles = true
}

func (v *tooltipView) Render(w *Window, cx *Context[tooltipView]) AnyElement {
	v.ensureFocusHandles(w)
	root := Div().ID("root").SizeFull().Flex().Bg(overlayColors.page)
	b1 := Div().ID("b1").DebugSelector("b1").
		Absolute().Top(Px(0)).Left(Px(0)).W(Px(40)).H(Px(20)).
		Bg(overlayColors.invoker).
		TrackFocus(v.b1).
		OnKeyDown(func(e *KeyDownEvent, _ *Window, _ *App) {
			v.log = append(v.log, "keydown:b1 "+e.Keystroke.Key)
		})
	if v.customContent {
		b1 = b1.TooltipView(func(w *Window, app *App) AnyElement {
			return Div().ID("tip").DebugSelector("tip").
				Absolute().Top(Px(24)).Left(Px(0)).W(Px(120)).H(Px(60)).
				Bg(overlayColors.popover).Occlude().
				OnPointerEnter(func(*PointerEnterEvent, *Window, *App) {
					v.log = append(v.log, "pointerenter:tip")
				}).
				OnPointerLeave(func(*PointerLeaveEvent, *Window, *App) {
					v.log = append(v.log, "pointerleave:tip")
				}).IntoElement()
		}).OnTooltipToggle(func(open bool, _ *Window, _ *App) {
			state := "closed"
			if open {
				state = "open"
			}
			v.log = append(v.log, "toggle:tip "+state)
		})
	} else {
		b1 = b1.Tooltip("Tip text").OnTooltipToggle(func(open bool, _ *Window, _ *App) {
			state := "closed"
			if open {
				state = "open"
			}
			v.log = append(v.log, "toggle:tip "+state)
		})
	}
	if !v.dropB1 {
		root.Child(b1)
	}
	root.Child(Div().ID("anchor").DebugSelector("anchor").
		Absolute().Top(Px(0)).Left(Px(300)).W(Px(40)).H(Px(20)).
		Bg(overlayColors.child).
		TrackFocus(v.anchor))
	return root.IntoElement()
}

// tooltipFixture is one test app, window and tooltipView.
type tooltipFixture struct {
	t      *testing.T
	ta     *TestApp
	app    *App
	window *Window
	view   Entity[tooltipView]
}

func newTooltipFixture(t *testing.T, customContent bool) *tooltipFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *tooltipView, cx *Context[tooltipView]) {
		v.customContent = customContent
	})
	f := &tooltipFixture{t: t, ta: ta, app: app, window: window, view: view}
	window.SetRootView(ViewOf(view))
	t.Cleanup(func() {
		CloseTestWindow(window)
		releaseAppImageCodec(app)
	})
	f.draw()
	return f
}

func (f *tooltipFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (f *tooltipFixture) settle() {
	f.t.Helper()
	f.ta.RunUntilParked()
	f.draw()
}

// wait advances the clock and settles (the replay's Wait step's twin).
func (f *tooltipFixture) wait(ms int64) {
	f.t.Helper()
	f.ta.AdvanceClock(ms)
	f.draw()
}

func (f *tooltipFixture) move(x, y float32) {
	f.t.Helper()
	f.window.DispatchInput(&MouseMoveEvent{Position: Point{X: x, Y: y}, Modifiers: Modifiers{}}, f.app)
}

func (f *tooltipFixture) pressKey(key string) {
	f.t.Helper()
	keystroke, err := ParseKeystroke(key)
	if err != nil {
		f.t.Fatalf("ParseKeystroke(%q): %v", key, err)
	}
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: keystroke}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: keystroke}, f.app)
}

func (f *tooltipFixture) log() []string {
	return f.view.Read(f.app, func(v *tooltipView, _ *App) []string { return v.log })
}

func (f *tooltipFixture) takeLog() []string {
	entries := f.log()
	f.view.Update(f.app, func(v *tooltipView, _ *Context[tooltipView]) { v.log = nil })
	return entries
}

// tooltipOpen reports whether the b1 tooltip's overlay is open.
func (f *tooltipFixture) tooltipOpen() bool {
	ts, ok := tooltipWindowStates[f.window]
	if !ok {
		return false
	}
	for _, rec := range ts.records {
		if rec.handle.IsOpen() {
			return true
		}
	}
	return false
}

// TestTooltipOpensOnHoverAfterTheShowDelay: the hover schedules the
// interest at interest-delay-start (250ms) — nothing before, the
// toggle open after.
func TestTooltipOpensOnHoverAfterTheShowDelay(t *testing.T) {
	f := newTooltipFixture(t, false)
	f.move(20, 10)
	f.wait(100)
	if f.tooltipOpen() {
		t.Fatal("the tooltip opened before the 250ms show delay")
	}
	if log := f.takeLog(); len(log) != 0 {
		t.Fatalf("the log before the delay: %v", log)
	}
	f.wait(200)
	if !f.tooltipOpen() {
		t.Fatal("the tooltip did not open 300ms after the hover")
	}
	if log := f.takeLog(); len(log) != 1 || log[0] != "toggle:tip open" {
		t.Fatalf("the open log = %v, want [toggle:tip open]", log)
	}
}

// TestTooltipClosesAfterTheHideDelayOnUnhover: the de-hover schedules
// the interest loss at interest-delay-end (150ms).
func TestTooltipClosesAfterTheHideDelayOnUnhover(t *testing.T) {
	f := newTooltipFixture(t, false)
	f.move(20, 10)
	f.wait(300)
	if !f.tooltipOpen() {
		t.Fatal("the setup failed: the tooltip did not open")
	}
	f.takeLog()
	f.move(300, 150)
	f.wait(100)
	if !f.tooltipOpen() {
		t.Fatal("the tooltip closed before the 150ms hide delay")
	}
	f.wait(100)
	if f.tooltipOpen() {
		t.Fatal("the tooltip did not close 200ms after the unhover")
	}
	if log := f.takeLog(); len(log) != 1 || log[0] != "toggle:tip closed" {
		t.Fatalf("the close log = %v, want [toggle:tip closed]", log)
	}
}

// TestTooltipGracePeriodMovingOntoTheTooltip: moving from the element
// onto the tooltip's own surface cancels the pending interest loss
// (the upstream arm) — the tooltip stays open past the hide delay and
// closes only after the pointer leaves the tooltip.
func TestTooltipGracePeriodMovingOntoTheTooltip(t *testing.T) {
	f := newTooltipFixture(t, true)
	f.move(20, 10)
	f.wait(300)
	if !f.tooltipOpen() {
		t.Fatal("the setup failed: the tooltip did not open")
	}
	f.takeLog()
	// Onto the tooltip (at (0,24) 120x60): the leave of b1 schedules the
	// loss, the enter of the tip cancels it.
	f.move(60, 54)
	f.wait(300)
	if !f.tooltipOpen() {
		t.Fatal("the grace period did not keep the tooltip open over its own surface")
	}
	if log := f.log(); len(log) != 1 || log[0] != "pointerenter:tip" {
		t.Fatalf("the grace log = %v, want [pointerenter:tip] — nothing closed", log)
	}
	// Away from both: the tooltip's leave schedules the loss.
	f.move(300, 150)
	f.wait(100)
	if !f.tooltipOpen() {
		t.Fatal("the tooltip closed before the 150ms hide delay after leaving the tip")
	}
	f.wait(100)
	if f.tooltipOpen() {
		t.Fatal("the tooltip did not close 200ms after leaving the tip")
	}
	log := f.log()
	want := []string{"pointerenter:tip", "pointerleave:tip", "toggle:tip closed"}
	if len(log) != len(want) {
		t.Fatalf("the full log = %v, want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("the full log = %v, want %v", log, want)
		}
	}
}

// TestTooltipOpensOnKeyboardFocus: the focusin schedules the interest
// with the same show delay, and the focusout the loss.
func TestTooltipOpensOnKeyboardFocus(t *testing.T) {
	f := newTooltipFixture(t, false)
	v := f.view.Read(f.app, func(v *tooltipView, _ *App) *tooltipView { return v })
	if got := f.window.Focused(); got.Ok && got.Handle.ID() != 0 {
		t.Fatal("the setup failed: something is focused")
	}
	f.pressKey("tab") // onto b1, the first tab stop
	if focused := f.window.Focused(); !focused.Ok || focused.Handle.ID() != v.b1.ID() {
		t.Fatal("the setup failed: the Tab did not focus b1")
	}
	f.settle()
	f.wait(100)
	if f.tooltipOpen() {
		t.Fatal("the tooltip opened before the 250ms show delay")
	}
	f.wait(200)
	if !f.tooltipOpen() {
		t.Fatal("the tooltip did not open 300ms after the focusin")
	}
	f.pressKey("tab") // onto the anchor: the focusout
	f.settle()
	f.wait(100)
	if !f.tooltipOpen() {
		t.Fatal("the tooltip closed before the 150ms hide delay")
	}
	f.wait(100)
	if f.tooltipOpen() {
		t.Fatal("the tooltip did not close 200ms after the focusout")
	}
}

// TestTooltipEscapeClosesIt: an unhandled Escape drops all interest
// BEFORE the close-watcher signal (keyboard_event_manager.cc:628), so
// the tooltip closes immediately, not after the hide delay.
func TestTooltipEscapeClosesIt(t *testing.T) {
	f := newTooltipFixture(t, false)
	f.move(20, 10)
	f.wait(300)
	if !f.tooltipOpen() {
		t.Fatal("the setup failed: the tooltip did not open")
	}
	f.takeLog()
	f.pressKey("escape")
	f.settle()
	if f.tooltipOpen() {
		t.Fatal("the Escape did not close the tooltip")
	}
	if log := f.takeLog(); len(log) != 1 || log[0] != "toggle:tip closed" {
		t.Fatalf("the Escape log = %v, want [toggle:tip closed]", log)
	}
}

// TestTooltipDefaultLookAnchorsBelowTheElement: the plain default look
// renders the text in a tooltip-yellow box below the element, a 4px
// gap away, and the hover continues over it (the grace period's
// anchor).
func TestTooltipDefaultLookAnchorsBelowTheElement(t *testing.T) {
	f := newTooltipFixture(t, false)
	f.move(20, 10)
	f.wait(300)
	f.settle()
	bounds, ok := WindowDebugBound(f.window, "tooltip")
	if !ok {
		f.window.requestRefresh()
		_, _ = DrawWindowFrame(f.window)
		bounds, ok = WindowDebugBound(f.window, "tooltip")
	}
	if !ok {
		t.Fatal("the default look's box did not render")
	}
	if bounds.Origin.X != 0 || bounds.Origin.Y != 24 {
		t.Fatalf("the default look's box origin = %v, want (0, 24) — below the element, a 4px gap", bounds.Origin)
	}
	if bounds.Size.Width <= 0 || bounds.Size.Height <= 0 {
		t.Fatalf("the default look's box size = %v, want a sized text box", bounds.Size)
	}
	// The interest constants are the port's contract with the record
	// (element.h:306-308).
	if interestDelayStart != 250*time.Millisecond || interestDelayEnd != 150*time.Millisecond {
		t.Fatalf("the interest delays = %v / %v, want 250ms / 150ms", interestDelayStart, interestDelayEnd)
	}
}

// TestTooltipClosesWhenItsElementDisappears: the frame-end
// reconciliation drops the record of an element that stopped drawing
// (the invoker left the document).
func TestTooltipClosesWhenItsElementDisappears(t *testing.T) {
	f := newTooltipFixture(t, false)
	f.move(20, 10)
	f.wait(300)
	if !f.tooltipOpen() {
		t.Fatal("the setup failed: the tooltip did not open")
	}
	// Drop b1 from the tree: the record leaves at the next frame's end.
	f.view.Update(f.app, func(v *tooltipView, _ *Context[tooltipView]) { v.dropB1 = true })
	f.settle()
	if f.tooltipOpen() {
		t.Fatal("the tooltip stayed open after its element left the tree")
	}
}
