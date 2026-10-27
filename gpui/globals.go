package gpui

import "reflect"

// This file ports the optional global accessors of the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (reference/ce-source/crates/gpui/src/app.rs):
//
//   - 2106-2108 has_global: whether a global of the type is assigned.
//   - 2120-2124 try_global: the global when assigned.
//   - 2139-2146 default_global: the global, assigned its default first
//     when absent; it queues the NotifyGlobalObservers effect like
//     global_mut (the caller is assumed to mutate through the returned
//     reference).
//
// Globals are keyed by their declared Go type, exactly like SetGlobal
// and Global (app.go).

// HasGlobal reports whether a global of type T is assigned (app.rs:
// 2106-2108 App::has_global).
func HasGlobal[T any](a *App) bool {
	if a == nil {
		panic("gpui: HasGlobal requires an application")
	}
	_, ok := a.globals[reflect.TypeFor[T]()]
	return ok
}

// TryGlobal returns the global of type T when one is assigned (app.rs:
// 2120-2124 App::try_global).
func TryGlobal[T any](a *App) (T, bool) {
	if a == nil {
		panic("gpui: TryGlobal requires an application")
	}
	t := reflect.TypeFor[T]()
	// browser-core 59 (view_reuse.go): a global read while a window's
	// frame runs is recorded, so a notify of the type refreshes the
	// windows that render from it.
	recordViewGlobalRead(a, t)
	v, ok := a.globals[t]
	if !ok {
		var zero T
		return zero, false
	}
	return v.(T), true
}

// DefaultGlobal returns the mutable global of type *G, assigning
// new(G) (G's zero value, the Go form of Default) when no *G global is
// assigned yet (app.rs:2139-2146 App::default_global). Like the
// reference it queues a global notification for *G (coalesced while
// pending), because the caller may mutate through the returned pointer.
//
// Go adaptation: the reference returns &mut G; Go globals are stored by
// value, so the mutable form is a pointer global — the global's declared
// type is *G (read it back with Global[*G], TryGlobal[*G] or
// HasGlobal[*G], observe it with ObserveGlobal[*G]).
func DefaultGlobal[G any](a *App) *G {
	if a == nil {
		panic("gpui: DefaultGlobal requires an application")
	}
	t := reflect.TypeFor[*G]()
	a.pushEffect(effect{kind: effNotifyGlobal, globalType: t})
	v, ok := a.globals[t]
	if !ok {
		created := new(G)
		a.globals[t] = created
		return created
	}
	return v.(*G)
}
