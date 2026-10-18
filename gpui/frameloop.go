package gpui

// This file is the port's window frame loop (Base ticket 03): frame
// demand (dirty windows, pending next-frame callbacks, an unpresented
// frame) wakes the platform's frame source, and a frame request serves
// the demand — next-frame callbacks first, then a draw of a dirty window,
// then the presentation.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (R: = reference/ce-source/crates/gpui/src):
//
//   - WindowInvalidator (R:window.rs:143-292): the dirty flag starts set
//     (:178), invalidate_view marks the window dirty and wakes the frame
//     source only when it BECAME dirty and no draw is running (:189-214),
//     set_dirty wakes on the clean→dirty edge (:220-240), wake_platform
//     (:255-260);
//   - App::notify (R:app.rs:2920-2953): a notification of an entity the
//     window rendered in its last frame invalidates that window;
//     record_entities_accessed (R:app.rs:1189-1216) is the tracking;
//   - App::apply_refresh_effect (R:app.rs:1896-1903) and the flush tail
//     that schedules a frame for every window with demand
//     (R:app.rs:1814-1822);
//   - Window::refresh (R:window.rs:2287-2292), Window::on_next_frame
//     (R:window.rs:2613-2619), Window::request_animation_frame
//     (R:window.rs:2633-2636), Window::simulate_next_frame
//     (R:window.rs:2643-2650), Context::on_next_frame
//     (R:app/context.rs:292-301);
//   - the platform request_frame callback (R:window.rs:1715-1843): skip a
//     re-entrant request (:1733-1737), run the pending next-frame
//     callbacks (:1787-1796), draw+present a dirty window or present an
//     unpresented frame (:1805-1824), re-arm the frame source when demand
//     remains (:1826-1843); Window::draw sets needs_present
//     (R:window.rs:3287) and Window::present clears it (:3338);
//   - the test platform window's frame waker counter and
//     simulate_frame_request (R:platform/test/window.rs:192-206, 377-385).
//
// Deviations, recorded honestly:
//
//   - Entity tracking: the reference records every entity ACCESSED
//     during a draw (reads included). The port records the views rendered
//     in the frame (the root view plus every view whose render ran inside
//     the frame's update boundary, see recordRenderedView). A notify of a
//     model entity that a view merely reads does not invalidate the window
//     unless a rendered view observes it (the usual cx.Observe+Notify
//     pattern does).
//   - A notification of a rendered view while its window is drawing does
//     not dirty the window (the reference's draw-phase guard), but the
//     port still queues the Notify effect so observers run (the reference
//     drops it; the port's existing observers rely on the delivery).
//   - Presentation: the reference presents through the platform window's
//     renderer. The port's renderer is application-owned, so the window's
//     WindowOptions.OnRedraw hook is the present step. A real window
//     without that hook keeps the explicit-draw model: its dirtiness does
//     not wake the frame source (nothing would present the frame), only
//     pending next-frame callbacks do.
//   - The Windows frame source: the reference's VSyncProvider thread
//     invalidates every window on every vblank (gpui_windows platform.rs
//     306-345) and request_frame no-ops on clean windows. The port is
//     demand driven (the frame-waker model of R:window.rs:252-260): a wake
//     arms a vblank-paced invalidation (frameloop_windows.go), so idle
//     windows cost nothing.
//   - Throttling (inactive-window / thermal frame intervals,
//     R:window.rs:1748-1784) is not ported: every request serves its
//     demand.

import (
	"log"
	"sort"
)

// windowFrameState is the per-window frame-loop state (the reference
// WindowInvalidator's dirty flag plus next_frame_callbacks, needs_present
// and the tracked entities of App::record_entities_accessed).
type windowFrameState struct {
	// dirty is the invalidator's dirty flag (R:window.rs:146). The
	// window's draw-state refresh flag (requestRefresh) is the same
	// demand; isDirty reads both.
	dirty bool
	// needsPresent records a drawn frame not presented yet
	// (R:window.rs:3287 / :3338).
	needsPresent bool
	// nextFrameCallbacks are the callbacks scheduled for the next frame
	// (R:window.rs:1288, :2613).
	nextFrameCallbacks []func(*Window, *App)
	// renderedViews are the views rendered by the last completed frame
	// (the tracked entities of R:app.rs:1189-1216).
	renderedViews map[EntityID]struct{}
	// nextRenderedViews collects the views rendered by the frame under
	// construction.
	nextRenderedViews map[EntityID]struct{}
	// present is the window's present hook (WindowOptions.OnRedraw for
	// real windows; nil for test windows, whose present is a no-op).
	present func()
	// drivesFrames reports whether frame demand from dirtiness wakes the
	// frame source: test windows and real windows with a present hook.
	drivesFrames bool
	// armed reports a wake delivered and not yet served by a frame
	// request (coalesces wakes the way an invalid region coalesces
	// InvalidateRect calls).
	armed bool
	// wakeCount counts frame-source wakes (the test window's
	// frame_wake_count, R:platform/test/window.rs:192, :377-385).
	wakeCount uint64
	// requesting reports a frame request in progress for this window;
	// the flush tail leaves its demand to the request's own tail.
	requesting bool
}

// newWindowFrameState builds a window's frame state. The window starts
// dirty, like the reference invalidator (R:window.rs:178).
func newWindowFrameState(present func(), drivesFrames bool) *windowFrameState {
	return &windowFrameState{dirty: true, present: present, drivesFrames: drivesFrames}
}

// frameStateOf returns the window's frame state, creating a test-window
// style state for windows registered before the frame loop existed.
func frameStateOf(w *Window) *windowFrameState {
	if w.frames == nil {
		w.frames = newWindowFrameState(nil, true)
	}
	return w.frames
}

// isDrawing reports whether this window's frame is being constructed
// (the invalidator's draw phase is not None).
func (w *Window) isDrawing() bool {
	ds, ok := windowDrawStates[w]
	return ok && ds.frame != nil
}

// isFrameDirty reports the invalidator's dirty flag: the frame-loop flag
// or the draw state's refresh request (both are the same demand).
func (w *Window) isFrameDirty() bool {
	if frameStateOf(w).dirty {
		return true
	}
	ds, ok := windowDrawStates[w]
	return ok && ds.refreshRequested
}

// clearFrameDirty clears both halves of the dirty flag.
func (w *Window) clearFrameDirty() {
	frameStateOf(w).dirty = false
	if ds, ok := windowDrawStates[w]; ok {
		ds.refreshRequested = false
	}
}

// markFrameDirty is WindowInvalidator::set_dirty(true)
// (R:window.rs:220-240): the window becomes dirty and the frame source
// wakes on the clean→dirty edge.
func (w *Window) markFrameDirty() {
	became := !w.isFrameDirty()
	frameStateOf(w).dirty = true
	if ds, ok := windowDrawStates[w]; ok {
		ds.refreshRequested = true
	}
	if became && frameStateOf(w).drivesFrames {
		w.wakeFrameSource()
	}
}

// wakeFrameSource is WindowInvalidator::wake_platform
// (R:window.rs:255-260) through the window's frame waker: a test window
// counts the wake (R:platform/test/window.rs:377-385); a real window
// arms a vblank-paced invalidation of its client area. Wakes coalesce
// until a frame request serves them; a closed window never wakes.
func (w *Window) wakeFrameSource() {
	if w == nil || w.app == nil {
		return
	}
	if _, open := w.app.windows[w.id]; !open {
		return
	}
	fs := frameStateOf(w)
	if fs.armed {
		return
	}
	fs.armed = true
	fs.wakeCount++
	if w.handle.Hwnd() != 0 {
		scheduleHostFrame(w)
	}
}

// hasFrameDemand reports whether the window needs a frame (the flush
// tail's condition, R:app.rs:1816-1818).
func (w *Window) hasFrameDemand() bool {
	fs := frameStateOf(w)
	if len(fs.nextFrameCallbacks) > 0 {
		return true
	}
	if !fs.drivesFrames {
		return false
	}
	return w.isFrameDirty() || fs.needsPresent
}

// Refresh marks the window dirty, scheduling it to be redrawn on the
// next frame (Window::refresh, R:window.rs:2287-2292). It has no effect
// while the window's frame is being drawn.
func (w *Window) Refresh() {
	if w == nil || w.isDrawing() {
		return
	}
	w.markFrameDirty()
}

// OnNextFrame schedules f to run directly after the current frame is
// rendered — at the window's next frame request (Window::on_next_frame,
// R:window.rs:2613-2619). Next-frame callbacks create frame demand
// without dirtying the window, so the frame source is woken explicitly.
func (w *Window) OnNextFrame(f func(w *Window, app *App)) {
	if w == nil {
		panic("gpui: OnNextFrame requires a window")
	}
	if f == nil {
		panic("gpui: OnNextFrame called with a nil callback")
	}
	fs := frameStateOf(w)
	fs.nextFrameCallbacks = append(fs.nextFrameCallbacks, f)
	w.wakeFrameSource()
}

// OnNextFrame schedules f to run with this context's entity on the
// window's next frame (Context::on_next_frame, R:app/context.rs:292-301).
// The callback is skipped when the entity was released meanwhile (the
// port holds the entity identity, not a strong handle).
func (cx *Context[S]) OnNextFrame(w *Window, f func(state *S, w *Window, cx *Context[S])) {
	if f == nil {
		panic("gpui: Context.OnNextFrame called with a nil callback")
	}
	entity := cx.entity
	w.OnNextFrame(func(w *Window, app *App) {
		withEntityUpdate[S](app, entity, func(state *S, cx *Context[S]) {
			f(state, w, cx)
		})
	})
}

// SimulateNextFrame runs every callback scheduled through OnNextFrame
// and returns how many ran (Window::simulate_next_frame,
// R:window.rs:2643-2650). Tests have no platform frame loop, so this
// simulates the delivery of the next frame's callbacks.
func (w *Window) SimulateNextFrame(app *App) int {
	fs := frameStateOf(w)
	callbacks := fs.nextFrameCallbacks
	fs.nextFrameCallbacks = nil
	if len(callbacks) == 0 {
		return 0
	}
	app.Update(func(app *App) {
		for _, callback := range callbacks {
			callback(w, app)
		}
	})
	return len(callbacks)
}

// SimulateFrameRequest delivers one frame request to the window, as the
// platform's frame source would (the test window's
// simulate_frame_request, R:platform/test/window.rs:196-206, running the
// window's request_frame callback, R:window.rs:1715-1843).
func (w *Window) SimulateFrameRequest() {
	w.requestFrame()
}

// FrameWakeCount reports how many times the window's frame source was
// woken (the test window's frame_wake_count,
// R:platform/test/window.rs:192-194). Wakes coalesce until a frame
// request serves them.
func (w *Window) FrameWakeCount() uint64 {
	if w == nil {
		return 0
	}
	return frameStateOf(w).wakeCount
}

// requestFrame is the window's request_frame callback
// (R:window.rs:1715-1843, without the throttling of :1748-1784): run the
// pending next-frame callbacks, draw a dirty window, present the drawn
// frame, then re-arm the frame source when demand remains.
func (w *Window) requestFrame() {
	if w == nil || w.app == nil {
		return
	}
	fs := frameStateOf(w)
	fs.armed = false
	if activeFrame != nil {
		// A request that arrived re-entrantly while a draw is on this
		// thread's stack is skipped (R:window.rs:1733-1737); the frame
		// source is re-armed so the frame is still drawn.
		w.wakeFrameSource()
		return
	}
	if _, open := w.app.windows[w.id]; !open {
		return
	}
	fs.requesting = true
	defer func() { fs.requesting = false }()
	app := w.app

	// The pending next-frame callbacks (R:window.rs:1787-1796).
	if callbacks := fs.nextFrameCallbacks; len(callbacks) > 0 {
		fs.nextFrameCallbacks = nil
		app.Update(func(app *App) {
			for _, callback := range callbacks {
				callback(w, app)
			}
		})
	}

	// Draw a dirty window, then present (R:window.rs:1805-1824).
	if fs.drivesFrames && w.isFrameDirty() {
		ds, ok := windowDrawStates[w]
		if ok && ds.root != nil && !ds.closed {
			if _, err := DrawWindowFrame(w); err != nil {
				// The reference logs the failed frame (log_err); the port
				// clears the demand so a failing draw does not spin.
				log.Printf("gpui: window %d frame request: %v", uint64(w.id), err)
				w.clearFrameDirty()
			}
		} else {
			// No root view: nothing to draw, the present hook still runs
			// for the requested frame.
			w.clearFrameDirty()
			fs.needsPresent = true
		}
	}
	if fs.drivesFrames && fs.needsPresent {
		fs.needsPresent = false
		if present := fs.present; present != nil {
			app.Update(func(*App) { present() })
		}
		// The present hook may draw again itself (the pre-frame-loop
		// OnRedraw shape); that frame was presented by the same hook.
		fs.needsPresent = false
	}

	// Re-arm the frame source when demand remains: the window was
	// re-invalidated mid-frame or the frame scheduled next-frame
	// callbacks (R:window.rs:1826-1843).
	if (fs.drivesFrames && w.isFrameDirty()) || len(fs.nextFrameCallbacks) > 0 {
		w.wakeFrameSource()
	}
}

// ---------------------------------------------------------------------------
// App integration
// ---------------------------------------------------------------------------

// sortedWindows returns the application's open windows in opening order.
func (a *App) sortedWindows() []*Window {
	windows := make([]*Window, 0, len(a.windows))
	for _, w := range a.windows {
		windows = append(windows, w)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].id < windows[j].id })
	return windows
}

// invalidateViewWindows is the window half of App::notify
// (R:app.rs:2920-2953 with invalidate_view, R:window.rs:189-214): every
// window whose last frame rendered the entity becomes dirty, unless it is
// drawing right now.
func (a *App) invalidateViewWindows(id EntityID) {
	if len(a.windows) == 0 {
		return
	}
	for _, w := range a.sortedWindows() {
		fs := w.frames
		if fs == nil {
			continue
		}
		if _, rendered := fs.renderedViews[id]; !rendered {
			continue
		}
		if w.isDrawing() {
			continue
		}
		w.markFrameDirty()
	}
}

// refreshAllWindows is App::apply_refresh_effect (R:app.rs:1896-1903):
// every window becomes dirty.
func (a *App) refreshAllWindows() {
	for _, w := range a.sortedWindows() {
		w.markFrameDirty()
	}
}

// scheduleWindowFrames is the flush tail's frame scheduling
// (R:app.rs:1814-1822): every window with frame demand gets its frame
// source woken. Windows inside a frame request leave the decision to the
// request's tail. This also catches dirtiness raised through the draw
// state's refresh seam (requestRefresh), which does not wake by itself.
func (a *App) scheduleWindowFrames() {
	if len(a.windows) == 0 {
		return
	}
	for _, w := range a.sortedWindows() {
		if fs := frameStateOf(w); fs.requesting || fs.armed {
			continue
		}
		if w.isDrawing() {
			continue
		}
		if w.hasFrameDemand() {
			w.wakeFrameSource()
		}
	}
}

// recordRenderedView records the view rendering in the active frame (the
// top of the rendered-view stack) as tracked by its window — the port's
// form of the accessed-entity tracking (R:window.rs:3302-3314,
// R:app.rs:1189-1216). It runs at every update boundary entered during a
// frame draw: a view's render is an entity update.
func recordRenderedView(w *Window) {
	ds, ok := windowDrawStates[w]
	if !ok || ds.frame == nil {
		return
	}
	views := ds.frame.renderedViews
	if len(views) == 0 {
		return
	}
	fs := frameStateOf(w)
	if fs.nextRenderedViews == nil {
		fs.nextRenderedViews = make(map[EntityID]struct{})
	}
	fs.nextRenderedViews[views[len(views)-1]] = struct{}{}
}

// beginFrameTracking starts the frame's rendered-view collection.
func beginFrameTracking(w *Window) {
	frameStateOf(w).nextRenderedViews = make(map[EntityID]struct{})
}

// completeFrameTracking publishes a completed frame to the frame loop:
// the rendered views (plus the root view) become the window's tracked
// entities, the window is clean and its frame needs presenting
// (R:window.rs:3164, :3276, :3287).
func completeFrameTracking(w *Window, root View) {
	fs := frameStateOf(w)
	rendered := fs.nextRenderedViews
	if rendered == nil {
		rendered = make(map[EntityID]struct{})
	}
	if root != nil {
		if id, ok := root.EntityID(); ok {
			rendered[id] = struct{}{}
		}
	}
	fs.renderedViews = rendered
	fs.nextRenderedViews = nil
	fs.dirty = false
	fs.needsPresent = true
}

// abandonFrameTracking drops an abandoned frame's collection.
func abandonFrameTracking(w *Window) {
	frameStateOf(w).nextRenderedViews = nil
}

// drawDirtyTestWindows is the test-support flush branch that draws every
// dirty window (R:app.rs:1800-1812), served from TestApp's
// RunUntilParked/AdvanceClock when the frame loop is enabled. Drawing can
// queue more work (tasks, notifications); the loop runs until no window
// is dirty. Next-frame callbacks are not run here (an animation loop
// would never park); SimulateNextFrame/SimulateFrameRequest deliver them.
func drawDirtyTestWindows(a *App, runScheduler func()) {
	for pass := 0; ; pass++ {
		if pass > 1000 {
			panic("gpui: the test frame loop did not settle: windows stayed dirty after 1000 frames")
		}
		drew := false
		for _, w := range a.sortedWindows() {
			ds, ok := windowDrawStates[w]
			if !ok || ds.root == nil || ds.closed || !w.isFrameDirty() {
				continue
			}
			if _, err := DrawWindowFrame(w); err != nil {
				panic("gpui: test frame loop draw: " + err.Error())
			}
			fs := frameStateOf(w)
			fs.needsPresent = false
			fs.armed = false
			drew = true
		}
		if !drew {
			return
		}
		runScheduler()
	}
}
