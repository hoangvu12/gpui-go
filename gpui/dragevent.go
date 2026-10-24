package gpui

// This file is browser-core ticket 16 (the full drag-and-drop
// sequence, .scratch/browser-core/issues/16-drag-and-drop-sequence.md,
// spec stories 55/56): the DOM drag events around the pin's drag
// machinery, ported toward the browser (D0: the browser wins):
//
//   - The event order is Chromium's EventHandler::UpdateDragAndDrop
//     (third_party/blink/renderer/core/input/event_handler.cc:1375-
//     1478): a target change fires `drag` on the source, then
//     `dragenter` on the new target, then `dragleave` on the old one
//     ("we must always fire a drag event before firing a dragenter,
//     dragleave, or dragover event", :1415-1446, conforming to the
//     HTML 5 spec's section 7.9.4); the same-target move fires `drag`
//     then `dragover`, except the move right after a target change,
//     which fires `dragover` alone (should_only_fire_drag_over_event_,
//     :1452-1462). The drag start is Chromium's CDP drag controller's
//     DragEnter-then-DragOver pair (content/browser/devtools/protocol/
//     input_handler.cc:1044-1059 UpdateDragging: EnsureDraggingEntered
//     then DragTargetDragOver), so the starting move fires dragstart,
//     drag, dragenter and dragover.
//   - dragstart fires before the drag begins and its cancellation
//     cancels the start (mouse_event_manager.cc:1051
//     TryStartDrag: `if (DispatchDragSrcEvent(kDragstart, ...) !=
//     kNotHandled) return false;`). dragend fires on the source when
//     the drag ends, carrying the final dropEffect
//     (mouse_event_manager.cc:1181-1198 DragSourceEndedAt).
//   - Cancelability: every drag event bubbles; dragleave and dragend
//     are not cancelable, the rest are (mouse_event_manager.cc:1120-
//     1122 DispatchDragEvent's setCancelable).
//   - The drop-effect negotiation: the drag source declares
//     effectAllowed (the allowed-operation mask; the DOM
//     "uninitialized" maps to every — data_transfer.cc
//     ConvertEffectAllowedToDragOperationsMask); dragenter and
//     dragover events carry a default dropEffect derived from it
//     (data_transfer.cc SetDestinationOperationFromEffectAllowed →
//     ConvertEffectAllowedToDropEffect: every→copy, move→move,
//     copy→copy, link→link), a listener may set the event's
//     DropEffect, and the accepted operation is that value masked
//     against the source (drag_controller.cc:918-926 TryDHTMLDrag —
//     an operation outside the source's mask becomes none). A target
//     accepts by cancelling dragenter/dragover; nobody cancelling
//     means the drag is not handled and the operation stays none
//     (drag_controller.cc:870-889 DefaultOperationForDrag's IE-match
//     comment is the fallback when the page cancelled but set no
//     dropEffect).
//   - The drag updates fire per drag-loop update — per mouse move —
//     not on a fixed timer (drag_controller.cc:910 TryDHTMLDrag runs
//     from DragEnteredOrUpdated, which the browser calls per
//     DragTargetDragEnter/DragOver, one per OLE drag-loop update;
//     there is no interval timer in the renderer).
//   - Escape cancels: the drag machinery consumes the Escape key
//     before any listener, binding or default action — the system
//     drag loop owns the keyboard during a drag (the OLE loop's
//     QueryContinueDrag answers DRAGDROP_S_CANCEL on the escape flag;
//     the renderer only sees the aftermath). The cancel fires `drag`
//     on the source, `dragleave` on the current target and `dragend`
//     with dropEffect none (event_handler.cc:1480-1494
//     CancelDragAndDrop, then DragSourceEndedAt; the CDP twin is
//     Input.cancelDragging, input_handler.cc:1670-1683, which sends
//     DragTargetDragLeave + DragSourceEndedAt(kNone)).
//   - Window deactivation cancels the same way (the system drag ends
//     when the window loses activation).
//
// The port rides the pin's drag machinery (app.activeDrag, the OnDrag
// threshold arm, the drop listeners' value delivery): the session is
// a per-window state that observes the moves and ups the pin's
// dispatch already delivers, so the pin's API (OnDrag/OnDropAlt/
// CanDrop) keeps working unchanged, and the DOM events layer on top.
// The drag events dispatch through the rendered frame's mouse
// listeners (capture outside-in, bubble inside-out), each
// registration gating itself the way the mouse listeners do: the
// target events gate on the hit chain (the DOM propagation path),
// the source events on the drag source's element path, and the
// leave/drop events on the session's recorded chains.

import (
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// The drop-effect vocabulary (drag_operation.h / data_transfer.cc)
// ---------------------------------------------------------------------------

// DragEffect is the drag-and-drop drop effect (the DOM
// dataTransfer.dropEffect / Blink's DragOperation).
type DragEffect uint8

// The drop effects.
const (
	// DragEffectNone is no effect (a target that does not accept).
	DragEffectNone DragEffect = iota
	// DragEffectCopy copies the drag data.
	DragEffectCopy
	// DragEffectMove moves the drag data.
	DragEffectMove
	// DragEffectLink links to the drag data.
	DragEffectLink
)

// String renders the effect as the DOM dropEffect string ("none",
// "copy", "move", "link").
func (e DragEffect) String() string {
	switch e {
	case DragEffectCopy:
		return "copy"
	case DragEffectMove:
		return "move"
	case DragEffectLink:
		return "link"
	default:
		return "none"
	}
}

// DragEffects is the allowed-operations mask of a drag (the DOM
// effectAllowed; Blink's DragOperationsMask — drag_operation.h:43-47,
// matching NSDragOperation: copy 1, link 2, move 16). The zero value
// means every effect is allowed, the DOM "uninitialized" default
// (data_transfer.cc ConvertEffectAllowedToDragOperationsMask maps
// kUninitialized to kDragOperationEvery).
type DragEffects uint32

// The allowed-effect bits.
const (
	// DragAllowsCopy allows the copy operation.
	DragAllowsCopy DragEffects = 1
	// DragAllowsLink allows the link operation.
	DragAllowsLink DragEffects = 2
	// DragAllowsMove allows the move operation.
	DragAllowsMove DragEffects = 16
	// DragAllowsAll is the "all"/"uninitialized" mask (every effect).
	DragAllowsAll DragEffects = DragAllowsCopy | DragAllowsLink | DragAllowsMove
)

// effectAllowedMask resolves a drag's allowed mask: zero (the DOM
// "uninitialized") reads as every operation allowed, like
// ConvertEffectAllowedToDragOperationsMask.
func effectAllowedMask(allowed DragEffects) DragEffects {
	if allowed == 0 {
		return DragAllowsAll
	}
	return allowed
}

// defaultDropEffect is the dropEffect a dragenter or dragover event
// starts with, derived from the source's allowed mask
// (data_transfer.cc ConvertEffectAllowedToDropEffect, whose ordering
// matches DragController::DefaultOperationForDrag: every→copy, then
// move, then copy, then link).
func defaultDropEffect(allowed DragEffects) DragEffect {
	mask := effectAllowedMask(allowed)
	if mask == DragAllowsAll {
		return DragEffectCopy
	}
	if mask&DragAllowsMove != 0 {
		return DragEffectMove
	}
	if mask&DragAllowsCopy != 0 {
		return DragEffectCopy
	}
	if mask&DragAllowsLink != 0 {
		return DragEffectLink
	}
	return DragEffectNone
}

// allowedContains reports whether the mask allows the effect (Blink
// composes single-operation masks; a zero mask means every).
func allowedContains(allowed DragEffects, effect DragEffect) bool {
	mask := effectAllowedMask(allowed)
	switch effect {
	case DragEffectCopy:
		return mask&DragAllowsCopy != 0
	case DragEffectMove:
		return mask&DragAllowsMove != 0
	case DragEffectLink:
		return mask&DragAllowsLink != 0
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// The DOM drag events (mouse_event_manager.cc DispatchDragEvent)
// ---------------------------------------------------------------------------

// dragEventDefaults is the shared per-event dispatch state: the
// event's default-prevented flag, snapshotted onto the event after
// its dispatch so a listener (or the harness) can read the final
// state the way the DOM's event.defaultPrevented reads after the
// dispatch completes.
type dragEventDefaults struct {
	dragPrevented bool
}

// DefaultPrevented reports the event's final default-prevented state
// (the DOM Event.defaultPrevented of the drag event).
func (d *dragEventDefaults) DefaultPrevented() bool { return d.dragPrevented }

// setDragDefaultPrevented records the dispatch outcome.
func (d *dragEventDefaults) setDragDefaultPrevented(v bool) { d.dragPrevented = v }

// dragPreventedEvent is the internal marker of the drag events.
type dragPreventedEvent interface {
	setDragDefaultPrevented(bool)
}

// DragStartEvent is the DOM dragstart event: it fires on the drag
// source (bubbling) when the drag starts, before the drag begins. Its
// cancellation cancels the drag start (mouse_event_manager.cc:1051:
// a prevented dragstart means no drag). The listener may set
// EffectAllowed to narrow the drag's allowed operations, the DOM's
// dragstart-only effectAllowed assignment.
type DragStartEvent struct {
	// Position is the pointer's window-local position.
	Position Point
	// Modifiers were held when the drag started.
	Modifiers Modifiers
	// EffectAllowed is the drag's allowed-operation mask. It starts as
	// the AnyDrag's mask and the listener may narrow it (the DOM
	// effectAllowed assignment of dragstart).
	EffectAllowed DragEffects
	dragEventDefaults
}

// DragEvent is the DOM drag event: it fires on the drag source
// (bubbling) on every drag-loop update — per mouse move — while the
// drag is over a target (event_handler.cc:1375-1478 fires it before
// every dragenter, dragleave and dragover). It is not the pin's
// OnDrag (the drag-source builder) nor OnDragMove (the typed
// per-element move listener); OnDragEvent is this event's builder.
type DragEvent struct {
	// Position is the pointer's window-local position.
	Position Point
	// Modifiers were held at the update.
	Modifiers Modifiers
	dragEventDefaults
}

// DragEnterEvent is the DOM dragenter event: it fires (bubbling) on
// the elements under the pointer when the drag target changes. A
// listener accepts the drag by calling Window.PreventDefault; the
// event's DropEffect starts as the default derived from the drag's
// allowed effects and may be set.
type DragEnterEvent struct {
	// Position is the pointer's window-local position.
	Position Point
	// Modifiers were held at the transition.
	Modifiers Modifiers
	// DropEffect is the event's drop effect: the default from the
	// drag's allowed effects unless a listener sets it.
	DropEffect DragEffect
	dragEventDefaults
}

// DragOverEvent is the DOM dragover event: it fires (bubbling) on the
// current target on every drag-loop update while the target is
// unchanged (the move right after a target change fires it alone, the
// one after that fires it with the drag event). A listener accepts
// the drag by calling Window.PreventDefault and may set the event's
// DropEffect — the drop lands only on an accepting target, and the
// final effect is the dropEffect masked against the source's allowed
// effects (drag_controller.cc:918-926).
type DragOverEvent struct {
	// Position is the pointer's window-local position.
	Position Point
	// Modifiers were held at the update.
	Modifiers Modifiers
	// DropEffect is the event's drop effect: the default from the
	// drag's allowed effects unless a listener sets it.
	DropEffect DragEffect
	dragEventDefaults
}

// DragLeaveEvent is the DOM dragleave event: it fires (bubbling) on
// the previous target's chain when the drag target changes, and on
// the current target when the drag is cancelled. It is not
// cancelable and its drop effect is none (mouse_event_manager.cc:
// 1132-1140).
type DragLeaveEvent struct {
	// Position is the pointer's window-local position at the change.
	Position Point
	// Modifiers were held at the change.
	Modifiers Modifiers
	// DropEffect is none: the drag is leaving (mouse_event_manager.cc:
	// 1136-1138 forces none for dragleave).
	DropEffect DragEffect
	dragEventDefaults
}

// DropEvent is the DOM drop event: it fires (bubbling) on the drag
// target when the button is released over an accepting target, before
// dragend. A drop over a non-accepting target fires no drop event.
type DropEvent struct {
	// Position is the pointer's window-local position at the release.
	Position Point
	// Modifiers were held at the release.
	Modifiers Modifiers
	// DropEffect is the negotiated operation the drop performs.
	DropEffect DragEffect
	dragEventDefaults
}

// DragEndEvent is the DOM dragend event: it fires (bubbling) on the
// drag source when the drag ends — after a drop, after a release over
// a non-accepting target, or after a cancel — carrying the final
// dropEffect (mouse_event_manager.cc:1181-1198 DragSourceEndedAt
// sets the operation before dispatching). It is not cancelable.
type DragEndEvent struct {
	// Position is the pointer's window-local position at the end.
	Position Point
	// Modifiers were held at the end.
	Modifiers Modifiers
	// DropEffect is the drag's final operation (none when nothing
	// accepted it or the drag was cancelled).
	DropEffect DragEffect
	dragEventDefaults
}

// ---------------------------------------------------------------------------
// The drag session (the per-window drag state)
// ---------------------------------------------------------------------------

// dragSession is the in-progress drag's per-window state (the DOM
// drag-and-drop processing state of event_handler.cc: drag_target_,
// should_only_fire_drag_over_event_ and the DragState's drag_src_).
// One session serves both an element drag (a source path, from
// DivElement.OnDrag) and a platform file drag (no source — the OLE
// drop target's Entered translation begins it).
type dragSession struct {
	// active reports whether the session is in progress.
	active bool
	// sourcePath is the drag source's global element path ("" for a
	// platform file drag, whose source is another application).
	sourcePath string
	// sourceChain is the drag source's ancestor chain, innermost
	// first (the drag/dragstart/dragend dispatch path).
	sourceChain []string
	// allowed is the source's allowed-effect mask (AnyDrag.
	// EffectAllowed; a dragstart listener may narrow it).
	allowed DragEffects
	// targetChain is the current drag target's chain (innermost
	// first), nil when the pointer is over no element.
	targetChain []string
	// onlyDragOverNext suppresses the drag event of the move right
	// after a target change (event_handler.cc's
	// should_only_fire_drag_over_event_).
	onlyDragOverNext bool
	// freshStart reports that the next update is the drag's first: it
	// fires the dragover the CDP drag controller's DragEnter-then-
	// DragOver pair produces at the drag start.
	freshStart bool
	// accepted reports whether the current target accepts the drop
	// (a cancelled dragenter/dragover, or the pin's drop-listener
	// query marking it during the event dispatch).
	accepted bool
	// effect is the current negotiated operation (none until a target
	// accepts).
	effect DragEffect
	// leaveChain is the old target chain while the dragleave dispatch
	// runs (the leave listeners' membership gate).
	leaveChain []string
	// dropChain is the target chain of the drop event while it
	// dispatches.
	dropChain []string
	// finalEffect is the session's settled operation after it ended
	// (the OLE layer's answer for the Drop call).
	finalEffect DragEffect
}

// dragSessionStates is the per-window registry (the
// interactWindowStates pattern). Closed windows are pruned when a new
// entry is created.
var dragSessionStates = map[*Window]*dragSession{}

// dragSessionOf returns (creating when absent) the window's session.
func dragSessionOf(w *Window) *dragSession {
	if s, ok := dragSessionStates[w]; ok {
		return s
	}
	for other := range dragSessionStates {
		if !windowIsOpen(other) {
			delete(dragSessionStates, other)
		}
	}
	s := &dragSession{}
	dragSessionStates[w] = s
	return s
}

// chainContains reports whether the path is in the chain.
func chainContains(chain []string, path string) bool {
	return path != "" && slices.Contains(chain, path)
}

// ancestorPathsOf expands a global element path key into its
// ancestor chain, innermost first ("root.p.c" → ["root.p.c",
// "root.p", "root"]).
func ancestorPathsOf(path string) []string {
	if path == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	out := make([]string, 0, len(parts))
	for i := len(parts); i > 0; i-- {
		out = append(out, strings.Join(parts[:i], "."))
	}
	return out
}

// ---------------------------------------------------------------------------
// The drag-event dispatch
// ---------------------------------------------------------------------------

// dispatchDragEvent dispatches one drag event through the rendered
// frame's mouse listeners — capture outside-in, bubble inside-out,
// stopping at propagation breaks — with its own prevent-default and
// propagation state (the drag events are nested dispatches inside the
// platform event that drove them, so the enclosing dispatch's state
// is saved and restored). The event's DefaultPrevented state is
// snapshotted onto the event after the traversal, the recorder-readable
// final state. It reports the prevented state.
func (w *Window) dispatchDragEvent(event any, app *App) bool {
	fs := focusState(w)
	if fs.rendered == nil {
		return false
	}
	is := inputState(w)
	savedPrevented, savedPropagate := is.defaultPrevented, app.propagateEvent
	is.defaultPrevented = false
	app.propagateEvent = true
	listeners := append([]mouseListener(nil), fs.rendered.mouseListeners...)
	for _, listener := range listeners {
		listener(event, DispatchCapture, w, app)
		if !app.propagateEvent {
			break
		}
	}
	if app.propagateEvent {
		for i := len(listeners) - 1; i >= 0; i-- {
			listeners[i](event, DispatchBubble, w, app)
			if !app.propagateEvent {
				break
			}
		}
	}
	prevented := is.defaultPrevented
	is.defaultPrevented = savedPrevented
	app.propagateEvent = savedPropagate
	if marker, ok := event.(dragPreventedEvent); ok {
		marker.setDragDefaultPrevented(prevented)
	}
	return prevented
}

// beginDragSession starts the DOM side of an element drag: dispatch
// dragstart on the source's chain and, unless a listener prevented
// it, keep the seeded session. It reports whether the drag should
// start (a prevented dragstart cancels it, Chromium's TryStartDrag).
func (w *Window) beginDragSession(drag *AnyDrag, sourcePath string, app *App) bool {
	s := dragSessionOf(w)
	// Seed the session before the dispatch: the dragstart listeners'
	// source-chain gate reads it (the session is rolled back when the
	// dragstart is prevented).
	s.active = true
	s.sourcePath = sourcePath
	s.sourceChain = ancestorPathsOf(sourcePath)
	s.allowed = effectAllowedMask(drag.EffectAllowed)
	s.targetChain = nil
	s.onlyDragOverNext = false
	s.freshStart = true
	s.accepted = false
	s.effect = DragEffectNone
	s.finalEffect = DragEffectNone
	e := &DragStartEvent{
		Position:      inputState(w).mousePosition,
		Modifiers:     inputState(w).modifiers,
		EffectAllowed: s.allowed,
	}
	prevented := w.dispatchDragEvent(e, app)
	if prevented {
		s.active = false
		s.sourcePath = ""
		s.sourceChain = nil
		s.targetChain = nil
		s.freshStart = false
		return false
	}
	// A dragstart listener may have narrowed the allowed effects (the
	// DOM's dragstart-only effectAllowed assignment).
	s.allowed = e.EffectAllowed
	return true
}

// beginExternalDragSession starts the session for a platform file
// drag (the OLE drop target's Entered translation): no source
// (dragstart/drag/dragend never fire — the source is another
// application), the target events track the file drag's position.
func (w *Window) beginExternalDragSession(app *App) {
	s := dragSessionOf(w)
	s.active = true
	s.sourcePath = ""
	s.sourceChain = nil
	s.allowed = 0
	s.targetChain = nil
	s.onlyDragOverNext = false
	s.freshStart = true
	s.accepted = false
	s.effect = DragEffectNone
	s.finalEffect = DragEffectNone
}

// updateDragSession runs one drag-loop update for a mouse move while
// a drag is active (EventHandler::UpdateDragAndDrop): diff the target
// chain and fire the DOM events in Chromium's order, then negotiate
// the drop effect.
func (w *Window) updateDragSession(move *MouseMoveEvent, app *App) {
	s := dragSessionOf(w)
	if !s.active {
		return
	}
	fs := focusState(w)
	var chain []string
	if fs.rendered != nil {
		chain = chainPathsOf(fs.rendered, inputState(w).mouseHitTest)
	}
	// enterEvent is this update's dragenter, whose DropEffect the
	// dragleave of the same update resets (the shared DataTransfer).
	var enterEvent *DragEnterEvent
	changed := !slices.Equal(chain, s.targetChain)
	if changed {
		// The target change: drag on the source, dragenter on the new
		// target, dragleave on the old one (event_handler.cc:1415-
		// 1446; the drag fires only over a target — :1422-1430).
		if len(chain) > 0 {
			w.fireDragSourceEvent(move, app)
			e := &DragEnterEvent{
				Position:   move.Position,
				Modifiers:  move.Modifiers,
				DropEffect: defaultDropEffect(s.allowed),
			}
			enterEvent = e
			// The acceptance re-negotiates this round: reset before the
			// dispatch (the pin's drop-listener arm may mark it during
			// the traversal), then fold in the event's own cancellation.
			s.accepted = false
			enterPrevented := w.dispatchDragEvent(e, app)
			s.accepted = s.accepted || enterPrevented
			s.effect = negotiatedEffect(s, e.DropEffect, s.accepted)
		} else {
			s.accepted = false
			s.effect = DragEffectNone
		}
		if len(s.targetChain) > 0 {
			w.fireDragLeave(move.Position, move.Modifiers, s.targetChain, app)
			if enterEvent != nil {
				// The enter and the leave of one update share the drag's
				// DataTransfer (TryDHTMLDrag creates one per update), and
				// the leave resets its dropEffect (mouse_event_manager.cc:
				// 1176 resetDropEffect — the uninitialized state reads
				// "none", data_transfer.h:84-86), so a dragenter that fired
				// with a dragleave reads none after the dispatch — the
				// DOM-observable value the page's event object reports.
				enterEvent.DropEffect = DragEffectNone
			}
		}
		if len(chain) > 0 {
			s.onlyDragOverNext = true
		}
		s.targetChain = chain
	} else if len(chain) > 0 {
		// The same target: drag (unless this is the move right after
		// the change) then dragover (event_handler.cc:1452-1462).
		if !s.onlyDragOverNext {
			w.fireDragSourceEvent(move, app)
		}
		s.onlyDragOverNext = false
		w.fireDragOver(move, app)
	} else {
		// Over nothing, unchanged: no events (event_handler.cc's
		// null-target arms).
	}
	// The drag start's DragEnter-then-DragOver pair (the CDP drag
	// controller's UpdateDragging runs both at the start): the first
	// update fires the dragover too.
	if s.freshStart && changed && len(chain) > 0 {
		s.freshStart = false
		s.onlyDragOverNext = false
		w.fireDragOver(move, app)
	}
}

// fireDragSourceEvent dispatches the DOM drag event on the source
// chain (DispatchDragSrcEvent).
func (w *Window) fireDragSourceEvent(move *MouseMoveEvent, app *App) {
	s := dragSessionOf(w)
	if len(s.sourceChain) == 0 {
		return
	}
	e := &DragEvent{Position: move.Position, Modifiers: move.Modifiers}
	w.dispatchDragEvent(e, app)
}

// fireDragOver dispatches the dragover event on the current target
// and negotiates the effect (TryDHTMLDrag): acceptance is a
// cancellation of the event or the pin's drop-listener query (marked
// during the dispatch by interact.go's drop arm), and the effect is
// the event's dropEffect masked against the source's allowed effects.
func (w *Window) fireDragOver(move *MouseMoveEvent, app *App) {
	s := dragSessionOf(w)
	e := &DragOverEvent{
		Position:   move.Position,
		Modifiers:  move.Modifiers,
		DropEffect: defaultDropEffect(s.allowed),
	}
	s.accepted = false
	prevented := w.dispatchDragEvent(e, app)
	s.accepted = s.accepted || prevented
	s.effect = negotiatedEffect(s, e.DropEffect, s.accepted)
}

// fireDragLeave dispatches the dragleave event on the given (old)
// chain.
func (w *Window) fireDragLeave(position Point, modifiers Modifiers, oldChain []string, app *App) {
	if len(oldChain) == 0 {
		return
	}
	s := dragSessionOf(w)
	s.leaveChain = oldChain
	defer func() { s.leaveChain = nil }()
	e := &DragLeaveEvent{Position: position, Modifiers: modifiers, DropEffect: DragEffectNone}
	w.dispatchDragEvent(e, app)
}

// negotiatedEffect resolves the operation of one negotiation round
// (drag_controller.cc:918-926 TryDHTMLDrag): a non-accepted target
// keeps none; an accepted target takes the event's dropEffect masked
// against the source's allowed effects (an operation the source does
// not allow becomes none).
func negotiatedEffect(s *dragSession, dropEffect DragEffect, accepted bool) DragEffect {
	if !accepted {
		return DragEffectNone
	}
	if !allowedContains(s.allowed, dropEffect) {
		return DragEffectNone
	}
	return dropEffect
}

// endDragSessionFromUp ends the drag at a mouse up (the up's drag
// tail): an accepting current target gets the drop event, then
// dragend fires on the source with the final effect (PerformDrop then
// DragSourceEndedAt). A release over a non-accepting target is the
// cancel sequence — drag on the source, dragleave on the current
// target, dragend with none (the drag exits the target: Chromium's
// DragTargetDragLeave → CancelDragAndDrop → DragSourceEndedAt path
// the refused drop takes; the dnd-drop-refused record is the
// evidence), never a drop event.
func (w *Window) endDragSessionFromUp(up *MouseUpEvent, app *App) {
	s := dragSessionOf(w)
	if !s.active {
		return
	}
	s.active = false
	if s.accepted && len(s.targetChain) > 0 {
		s.dropChain = s.targetChain
		e := &DropEvent{
			Position:   up.Position,
			Modifiers:  up.Modifiers,
			DropEffect: s.effect,
		}
		w.dispatchDragEvent(e, app)
		s.dropChain = nil
		s.finalEffect = s.effect
		w.fireDragEnd(up.Position, up.Modifiers, s.finalEffect, app)
		return
	}
	s.finalEffect = DragEffectNone
	s.accepted = false
	s.effect = DragEffectNone
	if len(s.sourceChain) > 0 {
		e := &DragEvent{Position: up.Position, Modifiers: up.Modifiers}
		w.dispatchDragEvent(e, app)
	}
	w.fireDragLeave(up.Position, up.Modifiers, s.targetChain, app)
	s.targetChain = nil
	w.fireDragEnd(up.Position, up.Modifiers, DragEffectNone, app)
}

// fireDragEnd dispatches dragend on the source chain with the final
// effect (DragSourceEndedAt: the operation is set on the transfer
// before the event; not cancelable).
func (w *Window) fireDragEnd(position Point, modifiers Modifiers, effect DragEffect, app *App) {
	s := dragSessionOf(w)
	if len(s.sourceChain) == 0 {
		return
	}
	e := &DragEndEvent{Position: position, Modifiers: modifiers, DropEffect: effect}
	w.dispatchDragEvent(e, app)
}

// cancelDragSession cancels the drag (Escape, window deactivation, a
// pointer cancel — event_handler.cc:1480-1494 CancelDragAndDrop, then
// DragSourceEndedAt with none): drag on the source, dragleave on the
// current target, dragend with dropEffect none, no drop. It reports
// whether a session was active.
func (w *Window) cancelDragSession(app *App) bool {
	s := dragSessionOf(w)
	if !s.active {
		return false
	}
	s.active = false
	s.accepted = false
	s.effect = DragEffectNone
	s.finalEffect = DragEffectNone
	position := inputState(w).mousePosition
	modifiers := inputState(w).modifiers
	if len(s.sourceChain) > 0 {
		e := &DragEvent{Position: position, Modifiers: modifiers}
		w.dispatchDragEvent(e, app)
	}
	w.fireDragLeave(position, modifiers, s.targetChain, app)
	s.targetChain = nil
	w.fireDragEnd(position, modifiers, DragEffectNone, app)
	return true
}

// endDragSessionQuietly ends the session with no events (the OLE
// Ended translation: the platform drag session's end after the
// leave/drop already ran).
func (w *Window) endDragSessionQuietly() {
	s := dragSessionOf(w)
	s.active = false
	s.targetChain = nil
	s.leaveChain = nil
	s.dropChain = nil
}

// consumeDragCancelKey reports whether the key event is the drag
// cancel key (Escape) consumed by an active drag. The system drag
// loop owns the keyboard during a drag: the OLE loop's
// QueryContinueDrag answers DRAGDROP_S_CANCEL on the escape flag and
// the renderer never sees the key — the port's drag machinery
// consumes it before any listener, binding or default action, so it
// composes with the close-watcher Escape default action (ticket 22):
// an active drag wins, no default action runs.
func consumeDragCancelKey(w *Window, event any, app *App) bool {
	down, ok := event.(*KeyDownEvent)
	if !ok {
		return false
	}
	if down.Keystroke.Key != "escape" {
		return false
	}
	if !w.cancelDragSession(app) {
		return false
	}
	app.cancelActiveDrag()
	return true
}

// ---------------------------------------------------------------------------
// The element builders
// ---------------------------------------------------------------------------

// dragEventListeners is one element's DOM drag-event listener set
// (DivElement's registration surface of this ticket).
type dragEventListeners struct {
	dragStart []func(*DragStartEvent, *Window, *App)
	drag      []func(*DragEvent, *Window, *App)
	dragEnter []func(*DragEnterEvent, *Window, *App)
	dragOver  []func(*DragOverEvent, *Window, *App)
	dragLeave []func(*DragLeaveEvent, *Window, *App)
	drop      []func(*DropEvent, *Window, *App)
	dragEnd   []func(*DragEndEvent, *Window, *App)
}

// hasDragEventListeners reports whether any DOM drag-event listener
// is registered.
func (d *DivElement) hasDragEventListeners() bool {
	l := &d.dragEvents
	return len(l.dragStart) > 0 || len(l.drag) > 0 || len(l.dragEnter) > 0 ||
		len(l.dragOver) > 0 || len(l.dragLeave) > 0 || len(l.drop) > 0 || len(l.dragEnd) > 0
}

// OnDragStart binds a listener to the DOM dragstart event of this
// element (browser-core ticket 16): it fires on the drag source's
// chain when the drag starts, before the drag begins, and its
// cancellation (Window.PreventDefault) cancels the drag. The listener
// may narrow the drag's allowed effects through the event's
// EffectAllowed. Requires an element ID (the source chain keys on
// it).
func (d *DivElement) OnDragStart(f func(*DragStartEvent, *Window, *App)) *DivElement {
	requireListener("OnDragStart", f == nil)
	d.dragEvents.dragStart = append(d.dragEvents.dragStart, f)
	return d
}

// OnDragEvent binds a listener to the DOM drag event of this element
// (browser-core ticket 16): it fires on the drag source's chain on
// every drag-loop update — per mouse move — while the drag is over a
// target. (The pin's OnDrag is the drag-source builder and OnDragMove
// the typed per-element move listener; this is the DOM event.)
// Requires an element ID.
func (d *DivElement) OnDragEvent(f func(*DragEvent, *Window, *App)) *DivElement {
	requireListener("OnDragEvent", f == nil)
	d.dragEvents.drag = append(d.dragEvents.drag, f)
	return d
}

// OnDragEnter binds a listener to the DOM dragenter event of this
// element (browser-core ticket 16): it fires on the elements under
// the pointer (bubbling) when the drag target changes to them. A
// listener accepts the drag by calling Window.PreventDefault.
func (d *DivElement) OnDragEnter(f func(*DragEnterEvent, *Window, *App)) *DivElement {
	requireListener("OnDragEnter", f == nil)
	d.dragEvents.dragEnter = append(d.dragEvents.dragEnter, f)
	return d
}

// OnDragOverEvent binds a listener to the DOM dragover event of this
// element (browser-core ticket 16): it fires (bubbling) on the
// current drag target on every drag-loop update. A listener accepts
// the drag by calling Window.PreventDefault and may set the event's
// DropEffect; the drop lands only on an accepting target. (The name
// carries the Event suffix because the pin's OnDragOver is the
// drag-over style refinement.)
func (d *DivElement) OnDragOverEvent(f func(*DragOverEvent, *Window, *App)) *DivElement {
	requireListener("OnDragOverEvent", f == nil)
	d.dragEvents.dragOver = append(d.dragEvents.dragOver, f)
	return d
}

// OnDragLeave binds a listener to the DOM dragleave event of this
// element (browser-core ticket 16): it fires (bubbling) on the old
// target's chain when the drag target changes away, and on the
// current target when the drag is cancelled. Requires an element ID
// (the leave chain keys on it).
func (d *DivElement) OnDragLeave(f func(*DragLeaveEvent, *Window, *App)) *DivElement {
	requireListener("OnDragLeave", f == nil)
	d.dragEvents.dragLeave = append(d.dragEvents.dragLeave, f)
	return d
}

// OnDropEvent binds a listener to the DOM drop event of this element
// (browser-core ticket 16): it fires (bubbling) on the drag target
// when the button is released over an accepting target, before
// dragend. (The name carries the Event suffix because the pin's
// OnDrop is the typed value delivery.) Requires an element ID.
func (d *DivElement) OnDropEvent(f func(*DropEvent, *Window, *App)) *DivElement {
	requireListener("OnDropEvent", f == nil)
	d.dragEvents.drop = append(d.dragEvents.drop, f)
	return d
}

// OnDragEnd binds a listener to the DOM dragend event of this element
// (browser-core ticket 16): it fires on the drag source's chain when
// the drag ends — after a drop, a release over a non-accepting
// target, or a cancel — carrying the final DropEffect. Requires an
// element ID.
func (d *DivElement) OnDragEnd(f func(*DragEndEvent, *Window, *App)) *DivElement {
	requireListener("OnDragEnd", f == nil)
	d.dragEvents.dragEnd = append(d.dragEvents.dragEnd, f)
	return d
}

// paintDragEventListeners registers the element's DOM drag-event
// listeners on the frame under construction (called from
// paintMouseListeners): one mouse-listener registration that reacts
// to the drag events the drag session dispatches, gating each kind
// the way the DOM propagation path does — the target events on the
// hit chain (IsHovered), the source events on the session's source
// chain, the leave event on the old target chain and the drop event
// on the session's drop chain.
func (d *DivElement) paintDragEventListeners(hitbox Hitbox, path string, w *Window, app *App) {
	if !d.hasDragEventListeners() {
		return
	}
	listeners := d.dragEvents
	startListeners := slices.Clone(listeners.dragStart)
	dragListeners := slices.Clone(listeners.drag)
	enterListeners := slices.Clone(listeners.dragEnter)
	overListeners := slices.Clone(listeners.dragOver)
	leaveListeners := slices.Clone(listeners.dragLeave)
	dropListeners := slices.Clone(listeners.drop)
	endListeners := slices.Clone(listeners.dragEnd)
	box := hitbox
	myPath := path
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		if phase != DispatchBubble {
			return
		}
		switch e := event.(type) {
		case *DragStartEvent:
			s := dragSessionOf(w)
			if !chainContains(s.sourceChain, myPath) {
				return
			}
			for _, listener := range startListeners {
				listener(e, w, app)
			}
		case *DragEvent:
			s := dragSessionOf(w)
			if !chainContains(s.sourceChain, myPath) {
				return
			}
			for _, listener := range dragListeners {
				listener(e, w, app)
			}
		case *DragEndEvent:
			s := dragSessionOf(w)
			if !chainContains(s.sourceChain, myPath) {
				return
			}
			for _, listener := range endListeners {
				listener(e, w, app)
			}
		case *DragEnterEvent:
			if !box.IsHovered(w) {
				return
			}
			for _, listener := range enterListeners {
				listener(e, w, app)
			}
		case *DragOverEvent:
			if !box.IsHovered(w) {
				return
			}
			for _, listener := range overListeners {
				listener(e, w, app)
			}
		case *DragLeaveEvent:
			s := dragSessionOf(w)
			if !chainContains(s.leaveChain, myPath) {
				return
			}
			for _, listener := range leaveListeners {
				listener(e, w, app)
			}
		case *DropEvent:
			s := dragSessionOf(w)
			if !chainContains(s.dropChain, myPath) {
				return
			}
			for _, listener := range dropListeners {
				listener(e, w, app)
			}
		}
	})
}
