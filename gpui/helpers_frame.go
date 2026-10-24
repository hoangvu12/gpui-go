package gpui

// finishFrame is the port of the tail of the pinned Window::draw
// (reference/ce-source/crates/gpui/src/window.rs:3233-3274, CE
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a): after Frame::finish carried
// the accessed element states over, the frames swap, the previous
// frame's unaccessed element states drop (releasing keyed state), and the
// focus phase dispatches the window focus listeners.
//
// render.go calls it explicitly when a frame completes (Base ticket 33;
// before, retainElementStates called it): it swaps the completed dispatch
// frame in through endDispatchFrame — the swap at 3236 precedes the
// listeners at 3270, so the focus listeners see the current tree — then
// releases the evicted states and dispatches the focus listeners. It
// reports whether a listener moved focus; render.go then refreshes the
// window AFTER the frame is published as clean (window.rs:3279-3285:
// Window::focus suppresses refresh while a draw is in progress, so the
// draw schedules the follow-up frame itself), so that refresh is not lost.
//
// Releases and listener callbacks run inside one application update so
// their effects (entity release hooks, notifies) flush before the draw
// returns.
func finishFrame(w *Window, evicted []elementStateReleaser) (focusMoved bool) {
	if w == nil || w.app == nil {
		for _, r := range evicted {
			r.releaseElementState()
		}
		return false
	}
	fs := focusState(w)
	if fs.next == nil {
		// No completed dispatch frame to swap: only the evictions apply.
		w.app.Update(func(*App) {
			for _, r := range evicted {
				r.releaseElementState()
			}
		})
		return false
	}

	// window.rs:3234-3240: the previous rendered frame's focus path and
	// activation, then the new frame's (Frame.focus is recorded when the
	// frame completes; window.rs 3198 records window_active).
	var previous []uint64
	previousActive := false // the reference's initial Frame has window_active false
	if fs.rendered != nil {
		previous = fs.rendered.focusPathOf()
		previousActive = fs.rendered.windowActive
	}
	fs.next.focus = fs.focused
	fs.next.windowActive = !fs.inactive
	// The end-of-frame focus fixup (browser-core ticket 14): a focused
	// element that left the completed frame — removed, hidden or inert —
	// blurs to the body equivalent before the frame swaps in, and the
	// sequential focus navigation starting point records where it sat
	// (the next Tab continues from there). The fixup runs pre-swap so
	// the old frame's tab stops are still reachable; the frame's
	// recorded focus follows the blur, so the focus phase's diff
	// delivers the blur listeners and the follow-up refresh
	// (focusMoved) repaints the focus styles (Chromium: the node-remove
	// focus fixup, document.cc:5809-5866, and the deferred
	// ClearFocusedElementIfNeeded, document.cc:3261-3277).
	focusFixed := w.fixupFocusedElement()
	endDispatchFrame(w)
	current := fs.rendered.focusPathOf()
	currentActive := fs.rendered.windowActive

	w.app.Update(func(a *App) {
		for _, r := range evicted {
			r.releaseElementState()
		}
		// The top layer reconciles against the completed frame and
		// drains its queued toggle notifications (browser-core ticket
		// 21, toplayer.go finishOverlayFrame) before the focus phase, so
		// the focus listeners see the settled overlay state — an overlay
		// closed by the show rules or by an app-driven state change
		// drops here and its listeners run before the frame is
		// published. Like a focus move, a delivered toggle may have
		// dirtied state; the refresh after publication catches it.
		overlayChanged := w.finishOverlayFrame(a)
		// browser-core ticket 24: the tooltip records reconcile after
		// the overlay layer — a tooltip whose element stopped drawing
		// hides here (the frame's seen marks).
		tooltipChanged := w.finishTooltipFrame(a)
		focusMoved = dispatchFocusPhase(w, a, previous, previousActive, current, currentActive)
		focusMoved = focusFixed || focusMoved || overlayChanged || tooltipChanged
	})
	return focusMoved
}
