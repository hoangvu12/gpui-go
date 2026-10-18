package gpui

// This file ports the focus-related context helpers of the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (reference/ce-source/crates/gpui/src):
//
//   - app.rs:2794-2798 — App::focus_handle: a focus handle allocated
//     without a window (the reference keeps one app-global focus map,
//     app.rs focus_handles; window.rs FocusHandle::new).
//   - app/context.rs:547-642 — Context::on_focus / on_focus_in /
//     on_blur / on_focus_out: window focus listeners keyed on a focus
//     id, activated through a deferred activate (self.defer(|_|
//     activate())), delivering through a weak entity update whose
//     failure drops the listener.
//   - window.rs:327-346 — WindowFocusEvent (is_focus_in/is_focus_out)
//     and FocusOutEvent{blurred: WeakFocusHandle}.
//   - window.rs:3233-3274 — the draw's focus phase: compare the
//     previous and current frame's focus path and window activation and
//     dispatch the focus listeners (delivered from helpers_frame.go).
//   - app/context.rs:444-458 + window.rs:1887-1902 — observe_window_
//     activation: activation observers run on the platform's active
//     status change, then the window refreshes.
//
// Go adaptation: window-allocated handles stay per-window (focus.go's
// header); app-level handles take identities from a disjoint space (the
// high bit) and live in a per-application registry, so they are valid in
// every window of the application exactly like the reference's
// app-global handles.

// appFocusIDBit marks app-level focus identities: window registries
// allocate from 1 upward, so the spaces never collide.
const appFocusIDBit uint64 = 1 << 63

// isAppFocusID reports whether a focus identity is app-level.
func isAppFocusID(id uint64) bool { return id&appFocusIDBit != 0 }

// appFocusRegistry is one application's app-level focus records.
type appFocusRegistry struct {
	seq  uint64
	refs map[uint64]*focusRef
}

// appFocusRegistries holds the app-level focus registry per application
// (App's struct is owned by app.go; the registry lives beside it, the
// same package-registry pattern as windowDrawStates). Foreground only.
var appFocusRegistries = map[*App]*appFocusRegistry{}

// appFocusState returns (creating when absent) the app registry.
func appFocusState(a *App) *appFocusRegistry {
	r, ok := appFocusRegistries[a]
	if !ok {
		r = &appFocusRegistry{refs: make(map[uint64]*focusRef)}
		appFocusRegistries[a] = r
	}
	return r
}

// appFocusRefOf returns the live app-level record, or nil when absent or
// released.
func appFocusRefOf(a *App, id uint64) *focusRef {
	if a == nil {
		return nil
	}
	r, ok := appFocusRegistries[a]
	if !ok {
		return nil
	}
	ref, ok := r.refs[id]
	if !ok || ref.released {
		return nil
	}
	return ref
}

// FocusHandle allocates a focus handle without a window (app.rs:2794-2798
// App::focus_handle → FocusHandle::new over the app focus map, default
// tab properties). The handle is valid in every window of this
// application: it can be tracked by elements, focused, tab-navigated and
// released like a window handle (Release clears the focus of any window
// holding it).
func (a *App) FocusHandle() FocusHandle {
	if a == nil {
		panic("gpui: App.FocusHandle requires an application")
	}
	r := appFocusState(a)
	r.seq++
	id := appFocusIDBit | r.seq
	r.refs[id] = &focusRef{tabIndex: 0, tabStop: false}
	return FocusHandle{id: id, app: a}
}

// handleFor builds the handle value for an identity rendered in this
// window (carrying the app for app-level identities).
func (w *Window) handleFor(id uint64) FocusHandle {
	h := FocusHandle{id: id, window: w}
	if isAppFocusID(id) {
		h.app = w.app
	}
	return h
}

// ---------------------------------------------------------------------------
// Focus listeners (app/context.rs:547-642)
// ---------------------------------------------------------------------------

// windowFocusEvent is the reference WindowFocusEvent (window.rs:327-340).
type windowFocusEvent struct {
	previous []uint64
	current  []uint64
}

func containsFocusID(path []uint64, id uint64) bool {
	for _, candidate := range path {
		if candidate == id {
			return true
		}
	}
	return false
}

func lastFocusID(path []uint64) (uint64, bool) {
	if len(path) == 0 {
		return 0, false
	}
	return path[len(path)-1], true
}

// isFocusIn is WindowFocusEvent::is_focus_in (window.rs:333-335).
func (e *windowFocusEvent) isFocusIn(id uint64) bool {
	return !containsFocusID(e.previous, id) && containsFocusID(e.current, id)
}

// isFocusOut is WindowFocusEvent::is_focus_out (window.rs:337-339).
func (e *windowFocusEvent) isFocusOut(id uint64) bool {
	return containsFocusID(e.previous, id) && !containsFocusID(e.current, id)
}

// FocusOutEvent is provided to Context.OnFocusOut listeners (window.rs:
// 342-346): a weak handle to the element that was blurred.
type FocusOutEvent struct {
	// Blurred is a weak handle to the blurred element.
	Blurred WeakFocusHandle
}

// focusDelivery is the payload delivered to focus listeners: the event
// plus the dispatching window.
type focusDelivery struct {
	event  *windowFocusEvent
	window *Window
}

// newFocusListener registers one window focus listener for the context's
// entity (window.rs:2090-2095 new_focus_listener). Delivery upgrades the
// entity weakly — a released entity drops the listener, like the
// reference's `.is_ok()` — and activation is deferred (the reference
// `self.defer(|_| activate())`).
func newFocusListener[S any](cx *Context[S], w *Window, op string, deliver func(state *S, e *windowFocusEvent, w *Window, cx *Context[S])) Subscription {
	if w == nil {
		panic("gpui: " + op + " requires a live window")
	}
	fs := focusState(w)
	if fs.focusListeners == nil {
		fs.focusListeners = newSubscriberSet()
	}
	observer := cx.entity
	reg := cx.app.registerSub(fs.focusListeners, observer, nil, cx.dependentScope(), func(a *App, payload any) {
		d := payload.(focusDelivery)
		withEntityUpdate[S](a, observer, func(state *S, ocx *Context[S]) {
			deliver(state, d.event, d.window, ocx)
		})
	})
	reg.activateViaDefer(cx.app)
	return Subscription{app: cx.app, reg: reg}
}

// OnFocus registers listener to run when handle receives focus: the
// previous focus path did not end at it and the current one does
// (app/context.rs:547-570 on_focus). The subscription activates at the
// end of the current effect cycle and persists until cancelled or the
// entity releases.
func (cx *Context[S]) OnFocus(handle FocusHandle, w *Window, listener func(*S, *Window, *Context[S])) Subscription {
	if listener == nil {
		panic("gpui: OnFocus called with a nil listener")
	}
	focusID := handle.id
	return newFocusListener(cx, w, "OnFocus", func(state *S, e *windowFocusEvent, w *Window, cx *Context[S]) {
		prev, prevOK := lastFocusID(e.previous)
		cur, curOK := lastFocusID(e.current)
		if !(prevOK && prev == focusID) && (curOK && cur == focusID) {
			listener(state, w, cx)
		}
	})
}

// OnFocusIn registers listener to run when handle or one of its
// descendants receives focus; it does not fire when the handle's subtree
// already held focus (app/context.rs:573-594 on_focus_in).
func (cx *Context[S]) OnFocusIn(handle FocusHandle, w *Window, listener func(*S, *Window, *Context[S])) Subscription {
	if listener == nil {
		panic("gpui: OnFocusIn called with a nil listener")
	}
	focusID := handle.id
	return newFocusListener(cx, w, "OnFocusIn", func(state *S, e *windowFocusEvent, w *Window, cx *Context[S]) {
		if e.isFocusIn(focusID) {
			listener(state, w, cx)
		}
	})
}

// OnBlur registers listener to run when handle loses focus: the previous
// focus path ended at it and the current one does not (app/context.rs:
// 596-618 on_blur).
func (cx *Context[S]) OnBlur(handle FocusHandle, w *Window, listener func(*S, *Window, *Context[S])) Subscription {
	if listener == nil {
		panic("gpui: OnBlur called with a nil listener")
	}
	focusID := handle.id
	return newFocusListener(cx, w, "OnBlur", func(state *S, e *windowFocusEvent, w *Window, cx *Context[S]) {
		prev, prevOK := lastFocusID(e.previous)
		cur, curOK := lastFocusID(e.current)
		if (prevOK && prev == focusID) && !(curOK && cur == focusID) {
			listener(state, w, cx)
		}
	})
}

// OnFocusOut registers listener to run when handle or one of its
// descendants loses focus; the event carries a weak handle to the
// previously focused element (app/context.rs:642-668 on_focus_out).
func (cx *Context[S]) OnFocusOut(handle FocusHandle, w *Window, listener func(*S, FocusOutEvent, *Window, *Context[S])) Subscription {
	if listener == nil {
		panic("gpui: OnFocusOut called with a nil listener")
	}
	focusID := handle.id
	return newFocusListener(cx, w, "OnFocusOut", func(state *S, e *windowFocusEvent, w *Window, cx *Context[S]) {
		blurred, ok := lastFocusID(e.previous)
		if ok && e.isFocusOut(focusID) {
			listener(state, FocusOutEvent{Blurred: WeakFocusHandle{handle: w.handleFor(blurred)}}, w, cx)
		}
	})
}

// dispatchFocusPhase is the draw's focus phase (window.rs:3233-3274),
// run after the completed frame swapped in: when the focus path or the
// window activation changed, deliver the WindowFocusEvent (paths blanked
// for an inactive side) to the focus listeners in registration order.
// When a listener moved focus, the window is refreshed (window.rs:
// 3262-3264).
func dispatchFocusPhase(w *Window, app *App, previous []uint64, previousActive bool, current []uint64, currentActive bool) {
	if equalFocusPaths(previous, current) && previousActive == currentActive {
		return
	}
	fs := focusState(w)
	focusBefore := fs.focused
	event := &windowFocusEvent{}
	if previousActive {
		event.previous = previous
	}
	if currentActive {
		event.current = current
	}
	if fs.focusListeners != nil {
		app.deliverSubs(fs.focusListeners, nil, 0, focusDelivery{event: event, window: w})
	}
	if fs.focused != focusBefore {
		w.requestRefresh()
	}
}

func equalFocusPaths(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Window activation observers (app/context.rs:444-458)
// ---------------------------------------------------------------------------

// ObserveWindowActivation registers callback to run when the window is
// activated or deactivated (app/context.rs:444-458
// observe_window_activation). The registration is active immediately
// (the reference calls activate() inline) and is dropped when the
// entity releases.
func (cx *Context[S]) ObserveWindowActivation(w *Window, callback func(*S, *Window, *Context[S])) Subscription {
	if w == nil {
		panic("gpui: ObserveWindowActivation requires a live window")
	}
	if callback == nil {
		panic("gpui: ObserveWindowActivation called with a nil callback")
	}
	fs := focusState(w)
	if fs.activationObservers == nil {
		fs.activationObservers = newSubscriberSet()
	}
	observer := cx.entity
	reg := cx.app.registerSub(fs.activationObservers, observer, nil, cx.dependentScope(), func(a *App, payload any) {
		window := payload.(*Window)
		withEntityUpdate[S](a, observer, func(state *S, ocx *Context[S]) {
			callback(state, window, ocx)
		})
	})
	reg.active = true
	return Subscription{app: cx.app, reg: reg}
}

// isWindowActiveCached reports the window activation last reported to the
// helper runtime (window.rs Window::is_window_active over the cached
// active flag). Windows start active until a deactivation is reported.
func (w *Window) isWindowActiveCached() bool {
	if w == nil {
		return false
	}
	return !focusState(w).inactive
}

// activationChanged is the platform active-status change path
// (window.rs:1887-1902): record the activation, run the activation
// observers in registration order, then refresh the window. The platform
// seam that calls it is window.go's OnActivate wiring (see the ticket 09
// report: OpenWindow must forward activation here).
func (w *Window) activationChanged(active bool, app *App) {
	fs := focusState(w)
	fs.inactive = !active
	app.Update(func(a *App) {
		if fs.activationObservers != nil {
			a.deliverSubs(fs.activationObservers, nil, 0, w)
		}
	})
	w.requestRefresh()
}
