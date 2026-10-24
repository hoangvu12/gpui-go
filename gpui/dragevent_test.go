package gpui

// Browser-core ticket 16's drag-sequence unit tests: the DOM drag
// events' order and effect negotiation against Chromium's rules (the
// parity fixtures pin the same sequence against a headless-Chrome
// record; these pin the parts the fixtures cannot reach — the
// prevented dragstart, the effect mask and the deactivation cancel).
//
//   - The event order: event_handler.cc:1375-1478 (UpdateDragAndDrop)
//     — drag before every dragenter/dragleave/dragover, dragenter on
//     the new target then dragleave on the old, dragover alone on the
//     move after a target change, and the drag start's enter+over
//     pair.
//   - The drop effect: drag_controller.cc:918-926 (TryDHTMLDrag) — a
//     cancelled dragover accepts, the dropEffect is masked against
//     the source's allowed effects.
//   - The cancels: event_handler.cc:1480-1494 (CancelDragAndDrop) and
//     mouse_event_manager.cc:1181-1198 (DragSourceEndedAt) — drag,
//     dragleave, dragend with none, no drop; Escape is consumed by
//     the drag machinery before any listener (the OLE drag loop owns
//     the keyboard).

import (
	"testing"
)

// dragLog is one drag-sequence observation: the event kind and
// target, plus the drop effect of the kinds that carry one.
type dragLog struct {
	transcript
}

func (l *dragLog) dragEntry(kind, target string, effect DragEffect, hasEffect bool) {
	if hasEffect {
		l.add("%s:%s:%s", kind, target, effect)
		return
	}
	l.add("%s:%s", kind, target)
}

// dragSequenceView builds the drag-sequence fixture's view: a drag
// source at the top-left and three absolute targets stacked in a
// column (t1, t2, t3 — the fixture window is 400x300, so each target
// is exactly 100 tall). accept1 and accept3 make the target accept
// the dragover (t3 with the move effect); t2 never accepts.
// preventStart makes the source's dragstart listener cancel the drag.
func dragSequenceView(rec *dragLog, accept1, accept3, preventStart bool) func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
	return func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		logDragEvents := func(d *DivElement, id string) *DivElement {
			return d.
				OnDragStart(func(e *DragStartEvent, w *Window, app *App) { rec.dragEntry("dragstart", id, 0, false) }).
				OnDragEvent(func(e *DragEvent, w *Window, app *App) { rec.dragEntry("drag", id, 0, false) }).
				// The enter logs the listener-time effect: during the dispatch it
				// reads the default from the allowed mask; a dragleave in the
				// same update resets the shared transfer afterwards, which only
				// the final-state read (the parity fixtures' recorder twin)
				// observes.
				OnDragEnter(func(e *DragEnterEvent, w *Window, app *App) { rec.dragEntry("dragenter", id, e.DropEffect, true) }).
				OnDragOverEvent(func(e *DragOverEvent, w *Window, app *App) { rec.dragEntry("dragover", id, e.DropEffect, true) }).
				OnDragLeave(func(e *DragLeaveEvent, w *Window, app *App) { rec.dragEntry("dragleave", id, 0, false) }).
				OnDropEvent(func(e *DropEvent, w *Window, app *App) { rec.dragEntry("drop", id, e.DropEffect, true) }).
				OnDragEnd(func(e *DragEndEvent, w *Window, app *App) { rec.dragEntry("dragend", id, e.DropEffect, true) })
		}
		root := logDragEvents(Div().ID("root").DebugSelector("root").Relative().SizeFull(), "root").
			OnKeyDown(func(e *KeyDownEvent, w *Window, app *App) { rec.add("keydown") })
		source := logDragEvents(Div().ID("source").DebugSelector("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)), "source").
			OnDrag(dragPayload{name: "chip"}, func(value *dragPayload, offset Point, w *Window, app *App) View {
				return ViewOf(NewEntity(app, w.Scope(), func(*dragPreview, *Context[dragPreview]) {}))
			})
		if preventStart {
			source = source.OnDragStart(func(e *DragStartEvent, w *Window, app *App) {
				w.PreventDefault()
			})
		}
		t1 := logDragEvents(Div().ID("t1").DebugSelector("t1").Absolute().Left(Px(100)).Top(Px(0)).Size(Px(100)), "t1")
		if accept1 {
			t1 = t1.OnDragOverEvent(func(e *DragOverEvent, w *Window, app *App) {
				w.PreventDefault()
			})
		}
		t2 := logDragEvents(Div().ID("t2").DebugSelector("t2").Absolute().Left(Px(100)).Top(Px(100)).Size(Px(100)), "t2")
		t3 := Div().ID("t3").DebugSelector("t3").Absolute().Left(Px(100)).Top(Px(200)).Size(Px(100))
		if accept3 {
			// The accept listener registers before the logging listener so
			// the log reads the effect it set (the Chrome record reads the
			// event's final state after the whole dispatch).
			t3 = t3.OnDragOverEvent(func(e *DragOverEvent, w *Window, app *App) {
				e.DropEffect = DragEffectMove
				w.PreventDefault()
			})
		}
		t3 = logDragEvents(t3, "t3")
		return root.
			Child(source).
			Child(t1).
			Child(t2).
			Child(t3).
			IntoElement()
	}
}

// dragSequenceFixture is the drag-sequence test fixture.
func dragSequenceFixture(t *testing.T, accept1, accept3, preventStart bool) (*interactFixture, *dragLog) {
	t.Helper()
	rec := &dragLog{}
	f := newInteractFixture(t, dragSequenceView(rec, accept1, accept3, preventStart))
	return f, rec
}

// The fixture's geometry: the source at the top-left, the targets in
// a column to its right.
const (
	dragSourceX, dragSourceY = 10, 10
	dragT1X, dragT1Y         = 150, 50
	dragT1bX, dragT1bY       = 160, 60
	dragT2X, dragT2Y         = 150, 150
	dragT2bX, dragT2bY       = 160, 160
	dragT3X, dragT3Y         = 150, 250
	dragT3bX, dragT3bY       = 160, 260
)

// TestDragEventSequenceMatchesChromiumOrder drives an internal drag
// across three targets (one accepting, one not, one with a different
// effect) and drops on the different-effect one: the transcript is
// Chromium's order — dragstart, drag, dragenter, dragover at the
// start; drag + dragover on the same target; drag, dragenter,
// dragleave on a change; dragover alone on the move after it; drop
// and dragend (with the final effect) at the release.
func TestDragEventSequenceMatchesChromiumOrder(t *testing.T) {
	f, rec := dragSequenceFixture(t, true, true, false)
	f.down(MouseButtonLeft, dragSourceX, dragSourceY)
	f.draw()
	f.dragMove(dragT1X, dragT1Y)
	wantTranscript(t, "the drag's start (the enter+over pair)", rec.take(), []string{
		"dragstart:source", "dragstart:root",
		"drag:source", "drag:root",
		"dragenter:t1:copy", "dragenter:root:copy",
		"dragover:t1:copy", "dragover:root:copy",
	})
	if !f.app.HasActiveDrag() {
		t.Fatal("the drag did not start")
	}
	f.draw()
	f.dragMove(dragT1bX, dragT1bY)
	wantTranscript(t, "the same target", rec.take(), []string{
		"drag:source", "drag:root",
		"dragover:t1:copy", "dragover:root:copy",
	})
	f.dragMove(dragT2X, dragT2Y)
	wantTranscript(t, "the target change (no dragover on the changing move)", rec.take(), []string{
		"drag:source", "drag:root",
		"dragenter:t2:copy", "dragenter:root:copy",
		"dragleave:t1", "dragleave:root",
	})
	f.dragMove(dragT2bX, dragT2bY)
	wantTranscript(t, "the move after the change (dragover alone)", rec.take(), []string{
		"dragover:t2:copy", "dragover:root:copy",
	})
	f.dragMove(dragT3X, dragT3Y)
	wantTranscript(t, "the second target change", rec.take(), []string{
		"drag:source", "drag:root",
		"dragenter:t3:copy", "dragenter:root:copy",
		"dragleave:t2", "dragleave:root",
	})
	f.dragMove(dragT3bX, dragT3bY)
	wantTranscript(t, "the move-effect target accepts", rec.take(), []string{
		"dragover:t3:move", "dragover:root:move",
	})
	s := dragSessionOf(f.window)
	if !s.accepted || s.effect != DragEffectMove {
		t.Fatalf("negotiated effect = %v (accepted %v), want move", s.effect, s.accepted)
	}
	f.up(MouseButtonLeft, dragT3bX, dragT3bY)
	wantTranscript(t, "the drop", rec.take(), []string{
		"drop:t3:move", "drop:root:move",
		"dragend:source:move", "dragend:root:move",
	})
	if f.app.HasActiveDrag() {
		t.Fatal("the drop ended the drag")
	}
}

// TestDragDropRefusedFiresNoDrop releases over the non-accepting
// target: no drop event, and the dragend carries the none effect.
func TestDragDropRefusedFiresNoDrop(t *testing.T) {
	f, rec := dragSequenceFixture(t, true, true, false)
	f.down(MouseButtonLeft, dragSourceX, dragSourceY)
	f.draw()
	f.dragMove(dragT1X, dragT1Y)
	f.dragMove(dragT1bX, dragT1bY)
	rec.take()
	f.dragMove(dragT2X, dragT2Y)
	rec.take()
	f.dragMove(dragT2bX, dragT2bY)
	rec.take()
	f.up(MouseButtonLeft, dragT2bX, dragT2bY)
	wantTranscript(t, "the refused drop (the cancel sequence)", rec.take(), []string{
		"drag:source", "drag:root",
		"dragleave:t2", "dragleave:root",
		"dragend:source:none", "dragend:root:none",
	})
	if f.app.HasActiveDrag() {
		t.Fatal("the release ended the drag")
	}
}

// TestDragEscapeCancelsLeaveAndEndNoDrop presses Escape mid-drag: the
// drag machinery consumes the key before any listener (the OLE drag
// loop owns the keyboard), and the cancel fires drag, dragleave and
// dragend with no drop.
func TestDragEscapeCancelsLeaveAndEndNoDrop(t *testing.T) {
	f, rec := dragSequenceFixture(t, true, true, false)
	f.down(MouseButtonLeft, dragSourceX, dragSourceY)
	f.draw()
	f.dragMove(dragT1X, dragT1Y)
	f.dragMove(dragT1bX, dragT1bY)
	rec.take()
	f.key(true, "escape")
	wantTranscript(t, "the Escape cancel", rec.take(), []string{
		"drag:source", "drag:root",
		"dragleave:t1", "dragleave:root",
		"dragend:source:none", "dragend:root:none",
	})
	if f.app.HasActiveDrag() {
		t.Fatal("the Escape ended the drag")
	}
	if s := dragSessionOf(f.window); s.active {
		t.Fatal("the Escape ended the drag session")
	}
	// The consumed Escape never reaches the key listeners (the
	// transcript has no keydown entry; a normal key still does).
	f.key(true, "a")
	wantTranscript(t, "a key after the drag", rec.take(), []string{"keydown"})
	f.key(false, "a")
	rec.take()
	// The drag is gone: a move produces no drag events.
	f.dragMove(dragT2X, dragT2Y)
	wantTranscript(t, "a move after the cancel", rec.take(), nil)
}

// TestDragStartPreventedCancelsTheDragStart cancels the dragstart: no
// drag starts (Chromium's TryStartDrag returns false), the dragstart
// fires once (the pending press is spent, like Chromium's one attempt
// per press), and no drag events follow.
func TestDragStartPreventedCancelsTheDragStart(t *testing.T) {
	f, rec := dragSequenceFixture(t, true, true, true)
	f.down(MouseButtonLeft, dragSourceX, dragSourceY)
	f.draw()
	f.dragMove(dragT1X, dragT1Y)
	wantTranscript(t, "the prevented dragstart", rec.take(), []string{
		"dragstart:source", "dragstart:root",
	})
	if f.app.HasActiveDrag() {
		t.Fatal("the prevented dragstart still started a drag")
	}
	if s := dragSessionOf(f.window); s.active {
		t.Fatal("the prevented dragstart still started a session")
	}
	f.dragMove(dragT1bX, dragT1bY)
	wantTranscript(t, "the moves after the prevented start", rec.take(), nil)
	f.up(MouseButtonLeft, dragT1bX, dragT1bY)
	wantTranscript(t, "the release after the prevented start", rec.take(), nil)
}

// TestDragWindowDeactivationCancels deactivates the window mid-drag:
// the drag cancels (dragleave + dragend, no drop), like the system
// drag ending when the window loses activation.
func TestDragWindowDeactivationCancels(t *testing.T) {
	f, rec := dragSequenceFixture(t, true, true, false)
	f.down(MouseButtonLeft, dragSourceX, dragSourceY)
	f.draw()
	f.dragMove(dragT1X, dragT1Y)
	f.dragMove(dragT1bX, dragT1bY)
	rec.take()
	f.app.Update(func(a *App) {
		f.window.activationChanged(false, a)
	})
	wantTranscript(t, "the deactivation cancel", rec.take(), []string{
		"drag:source", "drag:root",
		"dragleave:t1", "dragleave:root",
		"dragend:source:none", "dragend:root:none",
	})
	if f.app.HasActiveDrag() {
		t.Fatal("the deactivation ended the drag")
	}
}

// TestDragOverEffectMaskedAgainstAllowed sets a dropEffect the source
// does not allow: the negotiated operation becomes none
// (drag_controller.cc:918-926 — an operation outside the source's
// mask is none), so the release fires no drop.
func TestDragOverEffectMaskedAgainstAllowed(t *testing.T) {
	rec := &dragLog{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		// A copy-only drag whose target asks for move.
		source := Div().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).
			OnDragAlt(func(cursorOffset Point, cursorStyle *CursorStyle, w *Window, app *App) AnyDrag {
				return AnyDrag{
					Value:         dragPayload{name: "chip"},
					CursorOffset:  cursorOffset,
					EffectAllowed: DragAllowsCopy,
				}
			})
		target := Div().ID("t1").Absolute().Left(Px(100)).Top(Px(0)).Size(Px(100)).
			OnDragOverEvent(func(e *DragOverEvent, w *Window, app *App) {
				e.DropEffect = DragEffectMove
				w.PreventDefault()
				rec.add("dragover:t1:%s", e.DropEffect)
			}).
			OnDropEvent(func(e *DropEvent, w *Window, app *App) { rec.add("drop:%s", e.DropEffect) }).
			OnDragEnd(func(e *DragEndEvent, w *Window, app *App) { rec.add("dragend:%s", e.DropEffect) })
		return Div().ID("root").Relative().SizeFull().Child(source).Child(target).IntoElement()
	})
	f.down(MouseButtonLeft, dragSourceX, dragSourceY)
	f.draw()
	f.dragMove(dragT1X, dragT1Y)
	f.dragMove(dragT1bX, dragT1bY)
	wantTranscript(t, "the masked effect", rec.take(), []string{
		"dragover:t1:move", "dragover:t1:move",
	})
	f.up(MouseButtonLeft, dragT1bX, dragT1bY)
	wantTranscript(t, "the masked release", rec.take(), []string{
		// Chromium fires the drop when the dragover was cancelled — the
		// operation is none (the masked effect), and the drop event
		// carries it (drag_controller.cc:489-505: a cancelled dragover
		// handles the drag even when the negotiated operation is none;
		// PerformDrop still dispatches).
		"drop:none",
	})
}

// TestDragStartEffectAllowedNarrowing narrows the allowed effects in
// the dragstart listener (the DOM's dragstart-only effectAllowed
// assignment): the dragover default follows the narrowed mask.
func TestDragStartEffectAllowedNarrowing(t *testing.T) {
	rec := &dragLog{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		source := Div().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).
			OnDrag(dragPayload{name: "chip"}, func(value *dragPayload, offset Point, w *Window, app *App) View {
				return ViewOf(NewEntity(app, w.Scope(), func(*dragPreview, *Context[dragPreview]) {}))
			}).
			OnDragStart(func(e *DragStartEvent, w *Window, app *App) {
				e.EffectAllowed = DragAllowsMove
			})
		target := Div().ID("t1").Absolute().Left(Px(100)).Top(Px(0)).Size(Px(100)).
			OnDragOverEvent(func(e *DragOverEvent, w *Window, app *App) {
				rec.add("dragover:%s", e.DropEffect)
				w.PreventDefault()
			}).
			OnDropEvent(func(e *DropEvent, w *Window, app *App) { rec.add("drop:%s", e.DropEffect) })
		return Div().ID("root").Relative().SizeFull().Child(source).Child(target).IntoElement()
	})
	f.down(MouseButtonLeft, dragSourceX, dragSourceY)
	f.draw()
	f.dragMove(dragT1X, dragT1Y)
	f.dragMove(dragT1bX, dragT1bY)
	wantTranscript(t, "the narrowed default effect", rec.take(), []string{"dragover:move", "dragover:move"})
	f.up(MouseButtonLeft, dragT1bX, dragT1bY)
	wantTranscript(t, "the narrowed drop", rec.take(), []string{"drop:move"})
}
