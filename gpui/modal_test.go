package gpui

// Browser-core ticket 23 (spec story 70): Window.ShowModal — the modal
// kind's registration, the blocked document (the outside is inert),
// the dialog focusing steps, the focus restore on close, closedby's
// three policies and the dialog cancel forwarding — tested through
// the public TopLayer/OverlayHandle/Window API on the deterministic
// test window. The Chrome-parity twins of these tests are
// internal/parity/fixtures/modal.

import (
	"fmt"
	"testing"
)

// modalView renders the modal fixture tree: an invoker at the root
// (b1) whose click opens the dialog, an outside focusable anchor, and
// the dialog itself in the top layer with two focusable buttons
// inside. The handles and the focus identities live on the view entity
// (mutable state outside the per-frame build, the harness's
// discipline).
type modalView struct {
	dialog                                         OverlayHandle
	focusB1, focusAnchor, focusD, focusD1, focusD2 FocusHandle
	handles                                        bool
	// autofocus marks d2 as the dialog's autofocus delegate.
	autofocus bool
	// noButtons renders the dialog with nothing focusable inside
	// (the focusing steps' dialog-itself fallback).
	noButtons bool
	// closedBy is the entry's registered policy.
	closedBy ClosedBy
	// cancelPrevent makes the dialog's cancel listener prevent the
	// default (the dialog stays open).
	cancelPrevent bool
	// rootKeyDown binds a keydown listener on the root (the dispatch
	// probe: does the key event reach any listener while focus sits on
	// the dialog).
	rootKeyDown bool
	// log records clicks, keydowns, cancels and layer toggles in
	// delivery order.
	log []string
}

func (v *modalView) ensureHandles(w *Window) {
	if v.handles {
		return
	}
	v.dialog = NewOverlayHandle(w)
	v.dialog.OnToggle(func(open bool, _ *Window, _ *App) {
		v.log = append(v.log, fmt.Sprintf("toggle:d %v", open))
	})
	v.dialog.OnCancel(func(e *CloseWatcherEvent, _ *Window, _ *App) {
		v.log = append(v.log, fmt.Sprintf("cancel:d preventable=%v", e.Cancelable()))
		if v.cancelPrevent {
			e.PreventDefault()
		}
	})
	v.focusB1 = NewFocusHandle(w).TabStop(true)
	v.focusAnchor = NewFocusHandle(w).TabStop(true)
	v.focusD = NewFocusHandle(w).TabStop(true)
	v.focusD1 = NewFocusHandle(w).TabStop(true)
	v.focusD2 = NewFocusHandle(w).TabStop(true)
	v.handles = true
}

func (v *modalView) Render(w *Window, cx *Context[modalView]) AnyElement {
	v.ensureHandles(w)
	root := Div().ID("root").SizeFull().Flex().Bg(overlayColors.page)
	if v.rootKeyDown {
		root = root.OnKeyDown(func(_ *KeyDownEvent, _ *Window, _ *App) {
			v.log = append(v.log, "keydown:root")
		})
	}
	// The invoker: its click opens the modal (the popovertarget
	// activation's modal twin — a command=show-modal invoker).
	root.Child(Div().ID("b1").DebugSelector("b1").
		W(Px(40)).H(Px(20)).Bg(overlayColors.invoker).
		TrackFocus(v.focusB1).
		OnClick(func(_ *ClickEvent, w *Window, _ *App) {
			v.log = append(v.log, "click:b1")
			w.ShowModal(v.dialog)
		}))
	// The outside focusable anchor: while the modal is open it is
	// inert — Tab never reaches it and clicks fall through.
	root.Child(Div().ID("anchor").DebugSelector("anchor").
		Absolute().Top(Px(0)).Left(Px(300)).W(Px(40)).H(Px(20)).
		Bg(overlayColors.child).
		TrackFocus(v.focusAnchor))
	if v.dialog.IsOpen() {
		// The dialog's own focus slot: the app tracks focusD on the dialog
		// box div (the DOM dialog element's stand-in — always focusable),
		// and names it on the layer so the focusing steps' fallback can
		// find it and the descendants search can skip it.
		layer := TopLayer(v.dialog).ClosedBy(v.closedBy).DialogFocus(v.focusD)
		d := Div().ID("d").DebugSelector("d").
			Absolute().Top(Px(60)).Left(Px(20)).W(Px(150)).H(Px(120)).
			Bg(overlayColors.popover).Occlude().
			TrackFocus(v.focusD)
		if !v.noButtons {
			b1 := Div().ID("d1").DebugSelector("d1").
				Absolute().Top(Px(10)).Left(Px(10)).W(Px(40)).H(Px(20)).
				Bg(overlayColors.invoker).
				TrackFocus(v.focusD1)
			b2 := Div().ID("d2").DebugSelector("d2").
				Absolute().Top(Px(10)).Left(Px(60)).W(Px(40)).H(Px(20)).
				Bg(overlayColors.invoker).
				TrackFocus(v.focusD2)
			if v.autofocus {
				b2 = b2.AutoFocus()
			}
			d.Child(b1).Child(b2)
		}
		root.Child(layer.Child(d))
	}
	return root.IntoElement()
}

// modalFixture is one test app, window and modalView.
type modalFixture struct {
	t      *testing.T
	ta     *TestApp
	app    *App
	window *Window
	view   Entity[modalView]
}

func newModalFixture(t *testing.T, configure func(v *modalView)) *modalFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *modalView, cx *Context[modalView]) {
		if configure != nil {
			configure(v)
		}
	})
	f := &modalFixture{t: t, ta: ta, app: app, window: window, view: view}
	window.SetRootView(ViewOf(view))
	t.Cleanup(func() {
		CloseTestWindow(window)
		releaseAppImageCodec(app)
	})
	f.draw()
	return f
}

func (f *modalFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (f *modalFixture) settle() {
	f.t.Helper()
	f.ta.RunUntilParked()
	f.draw()
}

func (f *modalFixture) click(x, y float32) {
	f.t.Helper()
	f.window.DispatchInput(&MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
	f.window.DispatchInput(&MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
}

// pressKey dispatches one full key press through the platform
// dispatch (the listeners, then the default actions).
func (f *modalFixture) pressKey(key string) {
	f.t.Helper()
	keystroke, err := ParseKeystroke(key)
	if err != nil {
		f.t.Fatalf("ParseKeystroke(%q): %v", key, err)
	}
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: keystroke}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: keystroke}, f.app)
}

func (f *modalFixture) log() []string {
	return f.view.Read(f.app, func(v *modalView, _ *App) []string { return v.log })
}

func (f *modalFixture) takeLog() []string {
	entries := f.log()
	f.view.Update(f.app, func(v *modalView, _ *Context[modalView]) { v.log = nil })
	return entries
}

// focusedID returns the focused handle's window id, 0 when nothing is
// focused.
func (f *modalFixture) focusedID() uint64 {
	if focused := f.window.Focused(); focused.Ok {
		return focused.Handle.ID()
	}
	return 0
}

// openModal clicks the invoker (the mouse focus lands on b1, the
// dialog's previously focused element) and settles.
func (f *modalFixture) openModal() {
	f.click(20, 10)
	f.settle()
}

// TestShowModalFocusingStepsPickTheFirstFocusable: with no autofocus,
// the steps focus the first keyboard focusable element of the dialog
// in tree order (GetFocusDelegate's walk).
func TestShowModalFocusingStepsPickTheFirstFocusable(t *testing.T) {
	f := newModalFixture(t, nil)
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	if got := f.focusedID(); got != v.focusD1.ID() {
		t.Fatalf("focused after ShowModal = handle %d, want d1 (%d)", got, v.focusD1.ID())
	}
	wantLog(t, "the modal's open", f.takeLog(), []string{"click:b1", "toggle:d true"})
}

// TestShowModalFocusingStepsPreferAutofocus: the autofocus delegate
// wins over the earlier first focusable element.
func TestShowModalFocusingStepsPreferAutofocus(t *testing.T) {
	f := newModalFixture(t, func(v *modalView) { v.autofocus = true })
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	if got := f.focusedID(); got != v.focusD2.ID() {
		t.Fatalf("focused after ShowModal = handle %d, want the autofocus d2 (%d)", got, v.focusD2.ID())
	}
}

// TestShowModalFocusingStepsFallBackToTheDialogItself: a dialog with
// nothing focusable inside focuses its own slot (the dialog element is
// always focusable, html_dialog_element.h:114-116).
func TestShowModalFocusingStepsFallBackToTheDialogItself(t *testing.T) {
	f := newModalFixture(t, func(v *modalView) { v.noButtons = true })
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	if got := f.focusedID(); got != v.focusD.ID() {
		t.Fatalf("focused after ShowModal = handle %d, want the dialog itself (%d)", got, v.focusD.ID())
	}
}

// TestShowModalEscapeClosesAndRestoresFocus: an unhandled Escape
// signals the dialog's watcher, whose close hides the entry and
// restores the previously focused element (close's
// previously_focused_element->Focus, html_dialog_element.cc:206-223).
func TestShowModalEscapeClosesAndRestoresFocus(t *testing.T) {
	f := newModalFixture(t, nil)
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	f.pressKey("escape")
	f.settle()
	if got := f.focusedID(); got != v.focusB1.ID() {
		t.Fatalf("focused after Escape = handle %d, want the invoker b1 restored (%d)", got, v.focusB1.ID())
	}
	if v.dialog.IsOpen() {
		t.Fatal("the modal is still open after Escape")
	}
	// The outside is interactive again once the dialog closed: a Tab
	// from the restored invoker reaches the outside anchor.
	f.pressKey("tab")
	f.settle()
	if got := f.focusedID(); got != v.focusAnchor.ID() {
		t.Fatalf("focused after Tab past the closed modal = handle %d, want the anchor (%d)", got, v.focusAnchor.ID())
	}
}

// TestShowModalTabNeverLeavesTheDialog: while the modal is open, Tab
// cycles inside the dialog only — the outside is inert, so its tab
// stops never register (the suppressed registrations of
// browser-core 14). Tab past the dialog's last stop passes out (the
// port's document-scope end: focus blurs), never onto the anchor.
func TestShowModalTabNeverLeavesTheDialog(t *testing.T) {
	f := newModalFixture(t, nil)
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	f.pressKey("tab") // d1 -> d2
	if got := f.focusedID(); got != v.focusD2.ID() {
		t.Fatalf("focused after the first Tab = handle %d, want d2", got)
	}
	f.pressKey("tab") // past the dialog's last stop: out of the document scope
	if got := f.focusedID(); got != 0 {
		t.Fatalf("focused after the second Tab = handle %d, want the pass-out blur (0); the anchor is %d",
			got, v.focusAnchor.ID())
	}
}

// TestShowModalOutsideClickIsInert: while the modal is open, a click
// on the page does not reach the outside element (the hit test skips
// the inert hitboxes) and — with the default closedby (closerequest) —
// does not close the dialog either.
func TestShowModalOutsideClickIsInert(t *testing.T) {
	f := newModalFixture(t, nil)
	f.openModal()
	f.takeLog()
	f.click(350, 10) // the anchor's box, outside the dialog
	f.settle()
	if log := f.takeLog(); len(log) != 0 {
		t.Fatalf("the outside click reached something: %v", log)
	}
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	if !v.dialog.IsOpen() {
		t.Fatal("the default closedby (closerequest) let an outside click close the modal")
	}
}

// TestShowModalClosedByAnyLightDismisses: with closedby=any, a click
// on the inert page requests the dialog's close (the cancel event, then
// the hide) and the focus restores.
func TestShowModalClosedByAnyLightDismisses(t *testing.T) {
	f := newModalFixture(t, func(v *modalView) { v.closedBy = ClosedByAny })
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	f.click(350, 10) // the anchor's box: outside the dialog, inside the blocked document
	f.settle()
	if v.dialog.IsOpen() {
		t.Fatal("closedby=any did not close the modal on the outside click")
	}
	if got := f.focusedID(); got != v.focusB1.ID() {
		t.Fatalf("focused after the light dismiss = handle %d, want the restored invoker (%d)", got, v.focusB1.ID())
	}
	wantLog(t, "the light dismiss", f.takeLog(),
		[]string{"click:b1", "toggle:d true", "cancel:d preventable=true", "toggle:d false"})
}

// TestShowModalClosedByAnyClickInsideKeepsItOpen: the press and the
// release inside the dialog relate to the topmost dialog itself, so
// nothing dismisses.
func TestShowModalClosedByAnyClickInsideKeepsItOpen(t *testing.T) {
	f := newModalFixture(t, func(v *modalView) { v.closedBy = ClosedByAny })
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	f.click(30, 70) // inside d1's box
	f.settle()
	if !v.dialog.IsOpen() {
		t.Fatal("a click inside the dialog light-dismissed it")
	}
}

// TestShowModalClosedByNoneIgnoresEscapeAndClicks: closedby=none drops
// the watcher (the disabled watcher of SetCloseWatcherEnabledState),
// so Escape closes nothing and neither does a closedby-style click.
func TestShowModalClosedByNoneIgnoresEscapeAndClicks(t *testing.T) {
	f := newModalFixture(t, func(v *modalView) { v.closedBy = ClosedByNone })
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	f.pressKey("escape")
	f.settle()
	if got := f.focusedID(); got != v.focusD1.ID() {
		t.Fatalf("focus moved under closedby=none: handle %d, want d1 still focused", got)
	}
	if !v.dialog.IsOpen() {
		t.Fatal("closedby=none let Escape close the modal")
	}
	f.click(350, 10)
	f.settle()
	if !v.dialog.IsOpen() {
		t.Fatal("closedby=none let an outside click close the modal")
	}
	// The outside press still blurs (the mouse-focus default with no
	// focusable element under the press — the inert hit test): focus
	// is empty, never on the anchor.
	if got, want := f.focusedID(), uint64(0); got != want {
		t.Fatalf("focused after the inert outside click = handle %d, want the empty-press blur (0)", got)
	}
}

// TestShowModalCancelForwardingIsPreventable: the dialog's cancel
// listener (the DOM dialog's oncancel event the watcher's cancel
// forwards onto) can prevent the close.
func TestShowModalCancelForwardingIsPreventable(t *testing.T) {
	f := newModalFixture(t, func(v *modalView) { v.cancelPrevent = true })
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	f.pressKey("escape")
	f.settle()
	if !v.dialog.IsOpen() {
		t.Fatal("the prevented cancel still closed the modal")
	}
	wantLog(t, "the prevented cancel", f.takeLog(), []string{"click:b1", "toggle:d true", "cancel:d preventable=true"})
}

// TestShowModalHidesByRequest: the app's own Hide closes the modal and
// restores focus too (close()'s restore is the dialog's close path,
// whatever triggered it).
func TestShowModalHidesByRequest(t *testing.T) {
	f := newModalFixture(t, nil)
	v := f.view.Read(f.app, func(v *modalView, _ *App) *modalView { return v })
	f.openModal()
	v.dialog.Hide()
	f.settle()
	if got := f.focusedID(); got != v.focusB1.ID() {
		t.Fatalf("focused after Hide = handle %d, want the restored invoker (%d)", got, v.focusB1.ID())
	}
}

// TestA11yModalBlocksOutsideSubtree: while the modal is open, the
// outside publishes hidden accessibility nodes (the blocked document's
// a11y half, Chromium's ShouldIgnoreForHiddenOrInert) and the dialog's
// nodes stay visible.
func TestA11yModalBlocksOutsideSubtree(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 600})
	SetA11yWindowTitle(window, "modal a11y")
	dialogHandle := NewOverlayHandle(window)
	dialogFocus := NewFocusHandle(window)
	open := false
	view := NewEntity(app, window.Scope(), func(v *a11yDivView, cx *Context[a11yDivView]) {
		v.build = func(w *Window, cx *Context[a11yDivView]) AnyElement {
			root := Div().ID("col").FlexCol().
				Child(ax(leaf("live").Role(RoleButton))).
				Child(ax(leaf("frozen").Role(RoleLink)))
			if open {
				box := Div().ID("d").AccessibilityID("d").Role(RoleDialog).
					Child(ax(leaf("inner").Role(RoleCheckBox))).
					Absolute().Top(Px(60)).Left(Px(20)).W(Px(150)).H(Px(120))
				root.Child(TopLayer(dialogHandle).DialogFocus(dialogFocus).Child(box))
			}
			return root.IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	ActivateWindowA11y(window)
	snapshot := func() *A11ySnapshot {
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatalf("DrawWindowFrame: %v", err)
		}
		snapshot, err := PublishWindowA11y(window)
		if err != nil {
			t.Fatalf("PublishWindowA11y: %v", err)
		}
		return snapshot
	}
	node := func(s *A11ySnapshot, id string) A11ySnapshotNode {
		for _, n := range s.Nodes() {
			if n.AuthorID == id {
				return n
			}
		}
		t.Fatalf("no node with author id %q in\n%s", id, s.Trace())
		return A11ySnapshotNode{}
	}
	// Before: the outside publishes visible.
	if n := node(snapshot(), "frozen"); n.Hidden {
		t.Fatalf("the outside is hidden without a modal:\n%s", snapshot().Trace())
	}
	// Open the modal and redraw: the outside hides, the dialog does not.
	window.ShowModal(dialogHandle)
	open = true
	s := snapshot()
	if n := node(s, "frozen"); !n.Hidden {
		t.Fatalf("the outside is not hidden while the modal is open:\n%s", s.Trace())
	}
	if n := node(s, "d"); n.Hidden {
		t.Fatalf("the dialog's node is hidden while the modal is open:\n%s", s.Trace())
	}
	if n := node(s, "inner"); n.Hidden {
		t.Fatalf("the dialog's child is hidden while the modal is open:\n%s", s.Trace())
	}
}
