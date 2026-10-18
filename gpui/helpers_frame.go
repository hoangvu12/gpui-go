package gpui

// finishFrame is the port of the tail of the pinned Window::draw
// (reference/ce-source/crates/gpui/src/window.rs:3233-3274, CE
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a): after Frame::finish carried
// the accessed element states over, the frames swap, the previous
// frame's unaccessed element states drop (releasing keyed state), and the
// focus phase dispatches the window focus listeners.
//
// It is called from retainElementStates (element.go), which render.go
// invokes when a frame completes, BEFORE render.go's own
// endDispatchFrame. To deliver the focus listeners against the swapped
// (current) dispatch tree like the reference (the swap at 3236 precedes
// the listeners at 3270), finishFrame performs the swap itself through
// endDispatchFrame; render.go's following call is then a no-op
// (endDispatchFrame returns when no frame is pending).
//
// Releases and listener callbacks run inside one application update so
// their effects (entity release hooks, notifies) flush before the draw
// returns.
func finishFrame(w *Window, evicted []elementStateReleaser) {
	if w == nil || w.app == nil {
		for _, r := range evicted {
			r.releaseElementState()
		}
		return
	}
	fs := focusState(w)
	if fs.next == nil {
		// No completed dispatch frame to swap: only the evictions apply.
		w.app.Update(func(*App) {
			for _, r := range evicted {
				r.releaseElementState()
			}
		})
		return
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
	endDispatchFrame(w)
	current := fs.rendered.focusPathOf()
	currentActive := fs.rendered.windowActive

	w.app.Update(func(a *App) {
		for _, r := range evicted {
			r.releaseElementState()
		}
		dispatchFocusPhase(w, a, previous, previousActive, current, currentActive)
	})
}
