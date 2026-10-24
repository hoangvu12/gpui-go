package gpui

import "time"

// browser-core ticket 15 (.scratch/browser-core/issues/
// 15-wheel-latching-and-keyboard-scroll.md): the wheel's scroll
// latching. One wheel gesture latches to ONE scroller for its whole
// duration, chosen at the gesture's begin as the innermost scroller
// under the wheel that can consume the delta in its direction —
// Chromium's InputHandler::ScrollBegin walks up the scroll tree from
// the hit-tested node and latches the first node whose range can take
// the delta (cc/input/input_handler.cc FindNodeToLatch, :2341-2405);
// the gesture's updates then scroll the latched node only
// (ScrollLatchedScroller, :2195-2330) — an inner scroller that reaches
// its end mid-gesture keeps the gesture and the remaining delta does
// NOT chain to the outer one until the gesture ends.
//
// A gesture's extent follows Chromium's MouseWheelPhaseHandler for
// classic (phaseless) wheel events, which the browser side groups into
// one synthetic-phase transaction (content/browser/renderer_host/input/
// mouse_wheel_phase_handler.cc AddPhaseIfNeededAndScheduleEndEvent):
//
//   - the transaction ends 500ms after the last wheel event
//     (kDefaultMouseWheelLatchingTransaction,
//     mouse_wheel_phase_handler.h:20);
//   - a wheel event with different modifiers starts a new transaction
//     (HasDifferentModifiers, mouse_wheel_phase_handler.cc:203-208);
//   - an event whose phase is Ended or Cancelled ends the gesture
//     immediately (MouseWheelEventManager::HandleWheelEvent clears
//     wheel_target_, mouse_wheel_event_manager.cc:46-54).
//
// The port evaluates the deadline lazily at the next event's arrival —
// nothing needs the gesture-end side effects (scroll snap and fling are
// not ported) — on the application's injectable clock (App.Now: the
// TestApp virtual clock, monotonic wall time in real applications), so
// the tests are deterministic. The position break
// (kWheelLatchingSlopRegion, mouse_wheel_phase_handler.h:25) is
// subsumed by the port's position-gated wheel listeners: a latch whose
// scroller no longer contains the wheel position is cleared at the
// same event's end (finishWheelScrollTransaction's seen check), so a
// moved wheel re-latches on its next event.

// wheelScrollLatchingTransaction is Chromium's
// kDefaultMouseWheelLatchingTransaction (mouse_wheel_phase_handler.h:
// 20): the duration after the last wheel event for which consecutive
// wheel events belong to one scrolling transaction.
const wheelScrollLatchingTransaction = 500 * time.Millisecond

// wheelScrollLatch is the window's latched wheel scroller: the target
// is the scroller's persistent scroll-offset identity (the tracked
// handle's or the element state's offset cell, or the uniform list's
// scroll state), which is stable across the frame rebuilds. A nil
// latch (or a zero target) is no gesture in progress.
type wheelScrollLatch struct {
	// target is the latched scroller's identity, compared by interface
	// equality (pointer identity of the offset cell).
	target any
	// deadline is when the transaction ends: the last event's time plus
	// the 500ms latching transaction.
	deadline time.Duration
	// modifiers is the gesture's modifier state; an event with
	// different modifiers starts a new transaction.
	modifiers Modifiers
	// seen is the per-event flag the latched scroller's listener sets:
	// reset at the event's dispatch start, set when the latched
	// listener runs, and read at the dispatch end to clear a latch
	// whose scroller is gone (or no longer under the wheel — the port's
	// listeners are position-gated, which breaks the gesture the way
	// Chromium's slop region does).
	seen bool
}

// beginWheelScrollTransaction opens the wheel event's transaction
// (dispatchInput's ScrollWheelEvent arm, before the listeners): a
// phase-Ended or phase-Cancelled event ends the gesture; a still-live
// latch expires when its 500ms transaction deadline passed or the
// event's modifiers differ (the application's injectable clock reads
// the time). The per-event seen flag resets so the dispatch's end can
// clear a latch whose scroller did not run.
func (w *Window) beginWheelScrollTransaction(e *ScrollWheelEvent, app *App) {
	is := inputState(w)
	latch := is.wheelLatch
	if latch == nil || latch.target == nil {
		is.wheelLatch = nil
		return
	}
	if e.TouchPhase == TouchPhaseEnded || e.TouchPhase == TouchPhaseCancelled {
		// The gesture completed or was taken over: the latch ends with
		// the phase (mouse_wheel_event_manager.cc:46-54).
		is.wheelLatch = nil
		return
	}
	if app.Now() >= latch.deadline || e.Modifiers != latch.modifiers {
		// The 500ms transaction timed out, or the modifiers changed:
		// this event begins a new gesture (mouse_wheel_phase_handler.
		// cc's timer expiry / HasDifferentModifiers).
		is.wheelLatch = nil
		return
	}
	latch.seen = false
}

// finishWheelScrollTransaction closes the wheel event's dispatch
// (dispatchInput, after the mouse listeners): a latch whose latched
// scroller did not run for this event no longer holds the gesture —
// the scroller left the tree, or the wheel moved off its (position-
// gated) hitbox. The next wheel event re-latches.
func (w *Window) finishWheelScrollTransaction() {
	is := inputState(w)
	latch := is.wheelLatch
	if latch != nil && !latch.seen {
		is.wheelLatch = nil
	}
}

// wheelScrollLatchTake is the scroller's wheel-listener decision
// point, called with the scroller's identity, whether this scroller
// CAN consume the event's delta at its current clamped offset, and
// whether its overscroll-behavior cuts the scroll chain on the delta's
// axis (contain/none). It reports whether this scroller takes the
// event's delta:
//
//   - the latched scroller takes every event of its gesture, even a
//     delta it cannot consume — no chaining mid-gesture
//     (ScrollLatchedScroller scrolls the latched node alone); each
//     event refreshes the transaction's deadline;
//   - a scroller that is not the latched one takes nothing (the outer
//     scrollers' listeners run after the innermost in the bubble
//     order, so the first claim wins the walk);
//   - with no latch, the first scroller that can consume the delta
//     claims the gesture (FindNodeToLatch's CanConsumeDelta arm), and
//     so does a scroller whose overscroll-behavior cuts the chain even
//     while it cannot consume — a fully-scrolled contain scroller
//     latches the gesture so a reversed direction scrolls it again
//     (FindNodeToLatch's CanPropagate arm, input_handler.cc:2362-2378,
//     and scroll_state's is_scroll_chain_cut).
func (w *Window) wheelScrollLatchTake(e *ScrollWheelEvent, target any, canConsume, cutsChain bool, app *App) bool {
	is := inputState(w)
	if latch := is.wheelLatch; latch != nil && latch.target != nil {
		if latch.target != target {
			return false
		}
		// The latched scroller: the gesture's delta is this scroller's,
		// consumed or not, and the transaction extends from now.
		latch.deadline = app.Now() + wheelScrollLatchingTransaction
		latch.seen = true
		return true
	}
	if canConsume || cutsChain {
		is.wheelLatch = &wheelScrollLatch{
			target:    target,
			deadline:  app.Now() + wheelScrollLatchingTransaction,
			modifiers: e.Modifiers,
			seen:      true,
		}
		return true
	}
	return false
}
