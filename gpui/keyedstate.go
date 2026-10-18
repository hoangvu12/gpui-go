package gpui

import (
	"fmt"
	"runtime"
)

// This file ports Window::use_keyed_state / use_state of the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (reference/ce-source/crates/gpui/src/window.rs):
//
//   - 4090-4114 use_keyed_state: under with_global_id(key) (2933-2945:
//     push the key, snapshot the element id stack), with_element_state
//     keeps an Entity<S> across consecutive frames; the first frame
//     builds it with init and observes it so a notify on the state
//     notifies the view that was rendering (current_view, 5497-5500).
//   - 4116-4132 use_state: the same, keyed by the caller's code
//     location.
//   - 4140-4207 with_element_state + Frame::finish (1225-1231): state of
//     elements that stop drawing is dropped at frame end, which drops the
//     Entity<S> handle and releases the entity.
//
// Go adaptations:
//
//   - Ownership: the retained slot holds one owned lease registered in
//     the window's scope; frame-end eviction (element.go
//     retainElementStates → releaseElementState) and window close release
//     it — the explicit form of the reference's handle drop.
//   - The returned handle is a non-releasable facade over that lease
//     (valid while the entity is live: Read/Update/Downgrade/RetainInto
//     work; Release panics). Retain it with RetainInto to own the entity
//     beyond its element's lifetime — the reference's Entity clone.
//   - The reference's current_view() unwraps (panics) when no view is
//     rendering; the port skips the view-notify observer instead, since a
//     Go window root may be a stateless recipe.

// keyedStateSlot is the retained element state of UseKeyedState: the
// owned lease of the keyed entity (the reference stores Entity<S>).
type keyedStateSlot[S any] struct {
	entity Entity[S]
}

// releaseElementState releases the slot's lease on eviction
// (idempotent).
func (k *keyedStateSlot[S]) releaseElementState() {
	if k.entity.lease != nil {
		leaseRelease(k.entity.lease)
	}
}

// facade returns the non-releasable handle given to callers.
func (k *keyedStateSlot[S]) facade() Entity[S] {
	return Entity[S]{
		id:    k.entity.id,
		app:   k.entity.app,
		lease: &entityLease{kind: leaseBorrow, id: k.entity.id, app: k.entity.app, valid: &borrowValidity{}},
	}
}

// UseKeyedState returns a piece of state that exists as long as an
// element with this key keeps drawing in consecutive frames (window.rs:
// 4090-4114). The first call builds the entity with init and observes it
// so that its notifications notify the view that was rendering; later
// frames return the same entity. When no element with the key draws in a
// completed frame, the state is evicted and the entity released.
//
// It must be called during a render or element draw phase (inside a
// window frame); the key is scoped by the current element id stack.
func (w *Window) UseKeyedState[S any](key ElementID, cx AppContext, init func(w *Window, cx *Context[S]) S) Entity[S] {
	if w == nil {
		panic("gpui: UseKeyedState requires a live window")
	}
	if init == nil {
		panic("gpui: UseKeyedState called with a nil init")
	}
	app := appFrom(cx)
	if app != w.app {
		panic("gpui: UseKeyedState context belongs to a different application")
	}
	currentView, hasView := CurrentView(w)
	return WithID(w, key, func(w *Window) Entity[S] {
		global := &GlobalElementID{Path: currentElementIDs(w)}
		return WithElementState(w, global, func(state *keyedStateSlot[S], w *Window) (Entity[S], keyedStateSlot[S]) {
			if state != nil {
				return state.facade(), *state
			}
			entity := NewEntity(app, w.Scope(), func(s *S, ecx *Context[S]) {
				*s = init(w, ecx)
			})
			if hasView {
				observeNotifyView(app, entity.id, currentView)
			}
			slot := keyedStateSlot[S]{entity: entity}
			return slot.facade(), slot
		})
	})
}

// UseState is UseKeyedState keyed by the caller's source location
// (window.rs:4116-4132 use_state with #[track_caller]). When the location
// cannot identify the state (e.g. list items rendered by one call site),
// use UseKeyedState with an explicit key.
func (w *Window) UseState[S any](cx AppContext, init func(w *Window, cx *Context[S]) S) Entity[S] {
	_, file, line, ok := runtime.Caller(1)
	location := "<unknown>"
	if ok {
		location = fmt.Sprintf("%s:%d", file, line)
	}
	return w.UseKeyedState(codeLocationElementID(location), cx, init)
}

// observeNotifyView registers the detached app-level observer of the
// keyed entity that notifies the rendering view (window.rs:4105-4108
// `cx.observe(&new_state, move |_, cx| cx.notify(current_view)).detach()`).
// The registration activates through a queued defer like every observer
// and is removed with the observed entity.
func observeNotifyView(app *App, observed EntityID, view EntityID) {
	slot := app.entities.slot(observed)
	if slot == nil {
		return
	}
	reg := app.registerSub(slot.ensureObservers(), 0, nil, app.rootScope, func(a *App, _ any) {
		a.notify(view)
	})
	reg.activateViaDefer(app)
	reg.detach()
}
