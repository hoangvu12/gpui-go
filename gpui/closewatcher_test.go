package gpui

// Ticket 22 (browser-core): close watchers and the Escape default
// action — the watcher stack, the user-activation grouping, the
// newest-first signal with the cancel stop, the listener preventDefault
// gate and the TopLayer registration — tested through the public
// CloseWatcher/OverlayHandle API on the deterministic test window. The
// Chrome-parity twins of these tests are
// internal/parity/fixtures/closewatcher.

import (
	"fmt"
	"testing"
)

// watcherLog records the cancel/close deliveries in order.
type watcherLog struct {
	entries []string
}

func (l *watcherLog) add(format string, args ...any) {
	l.entries = append(l.entries, fmt.Sprintf(format, args...))
}

// escapePress dispatches one full Escape press through the platform
// dispatch (down then up, the way a real keyboard does).
func escapePress(w *Window, app *App) {
	stroke := MustParseKeystroke("escape")
	w.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	w.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, app)
}

// clickAt simulates a left click at (x, y) (the overlay fixture's
// helper shape).
func clickAt(w *Window, app *App, x, y float32) {
	w.DispatchInput(&MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, app)
	w.DispatchInput(&MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, app)
}

// newWatcherWindow opens one test app and window for the watcher tests.
func newWatcherWindow(t *testing.T) (*TestApp, *App, *Window) {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	t.Cleanup(func() {
		CloseTestWindow(window)
		releaseAppImageCodec(app)
	})
	return ta, app, window
}

// TestCloseWatcherEscapeSignalsTheNewestWatcher: two watchers created
// after separate user interactions sit in separate groups
// (close_watcher.cc:129-149's grouping rule), so one Escape signals
// only the NEWEST group (WatcherStack::Signal, close_watcher.cc:
// 204-215) — the newest watcher closes, the older one stays.
func TestCloseWatcherEscapeSignalsTheNewestWatcher(t *testing.T) {
	_, app, window := newWatcherWindow(t)
	var log watcherLog
	stroke := MustParseKeystroke("a")

	// Interaction, then the first watcher (its own group).
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, app)
	first := NewCloseWatcher(window)
	first.OnClose(func(*Window, *App) { log.add("close:first") })

	// Interaction, then the second watcher (its own group, newest).
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, app)
	second := NewCloseWatcher(window)
	second.OnClose(func(*Window, *App) { log.add("close:second") })

	escapePress(window, app)

	if !second.IsClosed() {
		t.Fatal("the newest watcher did not close on Escape")
	}
	if first.IsClosed() {
		t.Fatal("one Escape closed the older watcher too (they must sit in separate groups after separate interactions)")
	}
	wantStrings(t, "escape", log.entries, []string{"close:second"})
}

// TestCloseWatcherGroupOfProgrammaticWatchersClosesTogether: watchers
// created with no user interaction between them join ONE group
// (CloseWatcher::WatcherStack::Add, close_watcher.cc:92-107), so one
// Escape walks the newest group newest-first — both close in one
// press.
func TestCloseWatcherGroupOfProgrammaticWatchersClosesTogether(t *testing.T) {
	_, app, window := newWatcherWindow(t)
	var log watcherLog
	first := NewCloseWatcher(window)
	first.OnClose(func(*Window, *App) { log.add("close:first") })
	second := NewCloseWatcher(window)
	second.OnClose(func(*Window, *App) { log.add("close:second") })

	escapePress(window, app)

	if !first.IsClosed() || !second.IsClosed() {
		t.Fatalf("programmatic watchers must share one group and close together, closed: first=%v second=%v", first.IsClosed(), second.IsClosed())
	}
	wantStrings(t, "escape", log.entries, []string{"close:second", "close:first"})
}

// TestCloseWatcherPreventedCancelStopsTheGroupWalk: a watcher whose
// cancel listener prevents the default stops the group walk
// (RequestClose's defaultPrevented branch, close_watcher.cc:295-302)
// and consumes the history user activation, so the watcher below it in
// the group never closes.
func TestCloseWatcherPreventedCancelStopsTheGroupWalk(t *testing.T) {
	_, app, window := newWatcherWindow(t)
	var log watcherLog
	first := NewCloseWatcher(window)
	first.OnClose(func(*Window, *App) { log.add("close:first") })
	second := NewCloseWatcher(window)
	second.OnCancel(func(e *CloseWatcherEvent, _ *Window, _ *App) {
		log.add("cancel:second cancelable=%v", e.Cancelable())
		e.PreventDefault()
	})
	second.OnClose(func(*Window, *App) { log.add("close:second") })

	escapePress(window, app)

	if second.IsClosed() {
		t.Fatal("the prevented cancel still closed the watcher")
	}
	if first.IsClosed() {
		t.Fatal("the prevented cancel did not stop the group walk (the older watcher closed)")
	}
	wantStrings(t, "escape", log.entries, []string{"cancel:second cancelable=true"})

	// The activation was consumed (ConsumeHistoryUserActivation,
	// close_watcher.cc:296-299). A signal with NO fresh interaction
	// behind it (the port's signal runs from the Escape default action,
	// whose keydown just granted activation — so this drives the stack
	// directly, the OS-close-button shape Chromium's mojo Signal has)
	// then finds the next watcher's cancel NOT cancelable
	// (CancelEventCanBeCancelable, close_watcher.cc:151-155): the close
	// cannot be blocked anymore.
	third := NewCloseWatcher(window)
	third.OnCancel(func(e *CloseWatcherEvent, _ *Window, _ *App) {
		log.add("cancel:third cancelable=%v", e.Cancelable())
		e.PreventDefault()
	})
	third.OnClose(func(*Window, *App) { log.add("close:third") })
	window.signalCloseWatchers(app)
	if !third.IsClosed() {
		t.Fatal("the consumed activation must leave the cancel non-cancelable, so the watcher closes")
	}
	if !second.IsClosed() || !first.IsClosed() {
		t.Fatal("the non-cancelable group must close entirely: only a PREVENTED cancel stops the walk")
	}
	wantStrings(t, "direct signal", log.entries, []string{
		"cancel:second cancelable=true",
		"cancel:third cancelable=false",
		"close:third",
		"cancel:second cancelable=false",
		"close:second",
		"close:first",
	})
}

// TestCloseWatcherRequestCloseAndCloseAndDestroy: the public surface —
// RequestClose fires the cancel then the close, a plain Close fires
// only the close, and Destroy removes silently.
func TestCloseWatcherRequestCloseAndCloseAndDestroy(t *testing.T) {
	_, app, window := newWatcherWindow(t)
	var log watcherLog
	watcher := NewCloseWatcher(window)
	watcher.OnCancel(func(*CloseWatcherEvent, *Window, *App) { log.add("cancel") })
	watcher.OnClose(func(*Window, *App) { log.add("close") })
	if watcher.RequestClose() {
		log.add("requestclose=true")
	} else {
		t.Fatal("RequestClose with an unprevented cancel reports the close")
	}
	wantStrings(t, "requestClose", log.entries, []string{"cancel", "close", "requestclose=true"})

	log.entries = nil
	watcher = NewCloseWatcher(window)
	watcher.OnCancel(func(*CloseWatcherEvent, *Window, *App) { log.add("cancel") })
	watcher.OnClose(func(*Window, *App) { log.add("close") })
	watcher.Close()
	wantStrings(t, "close", log.entries, []string{"close"})

	log.entries = nil
	watcher = NewCloseWatcher(window)
	watcher.OnCancel(func(*CloseWatcherEvent, *Window, *App) { log.add("cancel") })
	watcher.OnClose(func(*Window, *App) { log.add("close") })
	watcher.Destroy()
	wantStrings(t, "destroy", log.entries, nil)
	if !watcher.IsClosed() {
		t.Fatal("Destroy leaves the watcher closed")
	}
	// A closed watcher no longer signals: an Escape closes nothing.
	escapePress(window, app)
	wantStrings(t, "escape after destroy", log.entries, nil)
}

// TestEscapeWithNestedOverlaysClosesNewestFirst: the ticket's core
// acceptance — nested popovers opened through their invokers each
// registered a close watcher (the popover show steps,
// html_element.cc:2146-2157), so Escape closes the newest popover
// first and the next Escape closes the next, whatever holds focus.
func TestEscapeWithNestedOverlaysClosesNewestFirst(t *testing.T) {
	ta, app, window := newWatcherWindow(t)
	view := NewEntity(app, window.Scope(), func(v *overlayView, cx *Context[overlayView]) {})
	window.SetRootView(ViewOf(view))
	f := &overlayFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()

	f.openBoth()
	if !f.hasBounds("p1") || !f.hasBounds("p2") {
		t.Fatal("the popovers did not open")
	}

	// Escape #1: the child (p2, the newest watcher) closes; the parent
	// stays.
	escapePress(window, app)
	f.settle()
	if f.hasBounds("p2") {
		t.Fatal("the first Escape left the child popover open")
	}
	if !f.hasBounds("p1") {
		t.Fatal("the first Escape closed the parent popover too")
	}

	// Escape #2: the parent closes.
	escapePress(window, app)
	f.settle()
	if f.hasBounds("p1") {
		t.Fatal("the second Escape left the parent popover open")
	}
	wantLog(t, "toggles", f.takeLog(), []string{
		"click:b1", "toggle:p1 true",
		"click:b2", "click:p1", "toggle:p2 true",
		"toggle:p2 false", "toggle:p1 false",
	})
}

// TestEscapeWithFocusElsewhereClosesTheNewest: focus sits on an
// element outside the newest overlay — the close-watcher stack is
// window-global (EscapeKeyHandler runs from the keydown default action
// regardless of the dispatch path, keyboard_event_manager.cc:618-630)
// — so Escape still closes the newest overlay.
func TestEscapeWithFocusElsewhereClosesTheNewest(t *testing.T) {
	ta, app, window := newWatcherWindow(t)
	view := NewEntity(app, window.Scope(), func(v *overlayView, cx *Context[overlayView]) {
		v.anchor = true
	})
	window.SetRootView(ViewOf(view))
	f := &overlayFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()
	f.openBoth()
	// Move focus to the anchor, outside every popover.
	anchor := f.view.Read(app, func(v *overlayView, _ *App) FocusHandle { return v.anchorFocus })
	app.Update(func(*App) { window.Focus(anchor) })
	ta.RunUntilParked()

	escapePress(window, app)
	f.settle()
	if f.hasBounds("p2") {
		t.Fatal("Escape with focus outside the newest overlay left the child popover open")
	}
	if !f.hasBounds("p1") {
		t.Fatal("Escape with focus outside the newest overlay closed the parent")
	}
}

// TestEscapeHandledByAListenerClosesNothing: a key listener that calls
// PreventDefault suppresses the default action (the DOM's rule the
// port's default-action phase implements, ticket 09), so the close
// watcher never signals.
func TestEscapeHandledByAListenerClosesNothing(t *testing.T) {
	ta, app, window := newWatcherWindow(t)
	view := NewEntity(app, window.Scope(), func(v *overlayView, cx *Context[overlayView]) {
		v.escapePrevented = true
	})
	window.SetRootView(ViewOf(view))
	f := &overlayFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()

	f.openBoth()
	escapePress(window, app)
	f.settle()
	if !f.hasBounds("p1") || !f.hasBounds("p2") {
		t.Fatal("a preventDefault Escape closed a popover")
	}
	wantLog(t, "toggles", f.takeLog(), []string{
		"click:b1", "toggle:p1 true",
		"click:b2", "click:p1", "toggle:p2 true",
	})
}

// TestManualPopoverRegistersNoCloseWatcher: popover=manual never joins
// the popover stacks, so its entry registers no close watcher
// (the watcher creation sits inside the stack-append branch,
// html_element.cc:2140-2157) — Escape closes nothing while only a
// manual overlay is open.
func TestManualPopoverRegistersNoCloseWatcher(t *testing.T) {
	ta, app, window := newWatcherWindow(t)
	view := NewEntity(app, window.Scope(), func(v *overlayView, cx *Context[overlayView]) {
		v.ensureHandles(window)
	})
	window.SetRootView(ViewOf(view))
	f := &overlayFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.view.Update(app, func(v *overlayView, _ *Context[overlayView]) { v.manual = true })
	f.draw()

	f.click(20, 10) // b1: show the manual parent
	f.settle()
	if !f.hasBounds("p1") {
		t.Fatal("the manual popover did not open")
	}

	escapePress(window, app)
	f.settle()
	if !f.hasBounds("p1") {
		t.Fatal("Escape closed a popover=manual entry (manual popovers register no close watcher)")
	}
}

// TestOverlayHandleShowCreatesAndHideDestroysTheWatcher: the watcher
// lifecycle rides the overlay's: Show creates one (the show steps),
// Hide destroys it (the hide steps, html_element.cc:2834-2836), and a
// re-show creates a fresh one.
func TestOverlayHandleShowCreatesAndHideDestroysTheWatcher(t *testing.T) {
	_, _, window := newWatcherWindow(t)
	handle := NewOverlayHandle(window)
	if handle.Show() != true {
		t.Fatal("Show reported no transition")
	}
	os := window.overlays
	rec := os.handles[handle.ID()]
	if rec.watcher == (CloseWatcher{}) {
		t.Fatal("Show created no close watcher for the popover entry")
	}
	if rec.watcher.IsClosed() {
		t.Fatal("the entry's watcher should be live while open")
	}
	firstWatcher := rec.watcher
	handle.Hide()
	if !firstWatcher.IsClosed() {
		t.Fatal("Hide left the entry's close watcher in the stack")
	}
	if handle.Show() != true {
		t.Fatal("re-Show reported no transition")
	}
	if rec = os.handles[handle.ID()]; rec.watcher == (CloseWatcher{}) {
		t.Fatal("the re-show created no close watcher")
	}
	if rec.watcher.ID() == firstWatcher.ID() {
		t.Fatal("the re-show reused the destroyed watcher's identity")
	}
}

// TestModalOverlayRegistersACloseWatcher: the dialog slot registers
// one too (HTMLDialogElement::CreateCloseWatcher,
// html_dialog_element.cc:536-545) — the modal entry's watcher closes
// it on Escape, while the light dismiss never does.
func TestModalOverlayRegistersACloseWatcher(t *testing.T) {
	ta, app, window := newWatcherWindow(t)
	handle := NewOverlayHandle(window)
	view := NewEntity(app, window.Scope(), func(v *modalRootView, cx *Context[modalRootView]) {
		v.handle = handle
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	handle.Show()
	ta.RunUntilParked()
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	rec := window.overlays.handles[handle.ID()]
	if rec.watcher == (CloseWatcher{}) {
		t.Fatal("the modal entry registered no close watcher")
	}
	escapePress(window, app)
	ta.RunUntilParked()
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	if handle.IsOpen() {
		t.Fatal("Escape left the modal entry open")
	}
}

// modalRootView renders one modal overlay entry for the modal watcher
// test.
type modalRootView struct {
	handle OverlayHandle
}

// Render implements Render.
func (v *modalRootView) Render(w *Window, cx *Context[modalRootView]) AnyElement {
	root := Div().ID("root").SizeFull().Flex().Bg(overlayColors.page)
	if v.handle.IsOpen() {
		root.Child(TopLayer(v.handle).Kind(OverlayModal).
			Child(Div().ID("m1").DebugSelector("m1").
				Absolute().Top(Px(60)).Left(Px(20)).W(Px(150)).H(Px(120)).
				Bg(overlayColors.popover).Occlude()))
	}
	return root.IntoElement()
}
