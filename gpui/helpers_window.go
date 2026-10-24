package gpui

import "math"

// This file ports the small window/app/context helpers of the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (reference/ce-source/crates/gpui/src), placed beside the owners'
// files:
//
//   - app.rs:2920 — App::notify(entity_id) (the exported form of
//     the package's notify effect).
//   - window.rs:2510-2515 — Window::defer: run at the end of the
//     current effect cycle with the window, skipped when the window is
//     gone (handle.update(...).ok()).
//   - window.rs:3127-3129 — Window::modifiers.
//   - window.rs:2983-2985 — Window::line_height: the current text
//     style's line height in pixels, rounded.
//   - window.rs:6868-6877 — Window::listener_for: a weak-entity event
//     adapter.
//   - app/context.rs:305-317 — Context::defer_in.
//   - app/context.rs:676-683 + window.rs:2656-2666 — Context::spawn_in
//     over Window::spawn: a foreground task with a weak entity and an
//     AsyncWindowContext (app/async_context.rs:319-341).

// windowIsOpen reports whether the window is still registered with its
// application (the port of a window handle update succeeding).
func windowIsOpen(w *Window) bool {
	if w == nil || w.app == nil {
		return false
	}
	registered, ok := w.app.windows[w.id]
	return ok && registered == w
}

// Notify notifies the observers of the entity with the given identity at
// the next effect flush, coalesced while pending (app.rs:2920 App::notify
// via AppContext). Notifying a released identity is a no-op.
func (a *App) Notify(id EntityID) {
	a.notify(id)
}

// Defer schedules f to run at the end of the current effect cycle with
// this window (window.rs:2510-2515 Window::defer). The callback is
// skipped when the window was closed in the meantime (the reference's
// failed window-handle update).
func (w *Window) Defer(cx AppContext, f func(w *Window, app *App)) {
	if w == nil {
		panic("gpui: Window.Defer requires a live window")
	}
	if f == nil {
		panic("gpui: Window.Defer called with a nil callback")
	}
	app := appFrom(cx)
	app.Defer(func(a *App) {
		if windowIsOpen(w) {
			f(w, a)
		}
	})
}

// Modifiers returns the window's current keyboard modifier state
// (window.rs:3127-3129 Window::modifiers), as tracked from dispatched
// input events.
func (w *Window) Modifiers() Modifiers {
	if w == nil {
		return Modifiers{}
	}
	return inputState(w).modifiers
}

// LineHeight returns the line height of the current text style in
// pixels (window.rs:2983-2985: text_style().line_height_in_pixels(
// rem_size()), style.rs:719-721 rounds the result). Outside a frame the
// text style stack is empty, so the default text style and the window's
// rem size apply, like the reference between draws.
//
// The CSS `normal` keyword — the default text style's value since
// browser-parity D1 — resolves through the window's text system: the
// resolved font's line spacing, the rounded ascent + rounded descent +
// rounded line gap (FontMetrics.LineSpacingPx, Chromium's
// ComputedStyle::ComputedLineHeight for normal, computed_style.cc:
// 2425-2436), already a whole pixel value. A window without its text
// system yet (outside a frame, before any draw) answers the pin's
// golden-ratio default, the reference behavior.
func (w *Window) LineHeight() float32 {
	if w == nil {
		panic("gpui: LineHeight requires a live window")
	}
	ds := drawState(w)
	style := DefaultTextStyle()
	rem := ds.rem
	if ds.frame != nil {
		style = WindowTextStyle(w)
		rem = ds.frame.rem
	}
	fontSize := style.FontSizePixels(rem)
	if style.LineHeight.Kind == DefiniteLengthNormal {
		lineHeight, err := w.TextSystem().NormalLineHeight(style.FontDescriptorOf(), fontSize)
		if err != nil {
			return DefaultLineHeight(fontSize)
		}
		return lineHeight
	}
	return float32(math.Round(float64(style.LineHeightPixels(fontSize, rem))))
}

// ListenerFor returns an event listener that invokes f with view's state
// and a fresh context (window.rs:6868-6877 Window::listener_for). The
// adapter holds a weak identity: it is a no-op once the entity releases.
func (w *Window) ListenerFor[T, E any](view Entity[T], f func(*T, *E, *Window, *Context[T])) func(*E, *Window, *App) {
	if f == nil {
		panic("gpui: ListenerFor called with a nil callback")
	}
	weak := view.Downgrade()
	return func(e *E, window *Window, app *App) {
		if app == nil {
			panic("gpui: ListenerFor adapter invoked without an application")
		}
		if app != weak.app {
			panic("gpui: ListenerFor adapter invoked with a foreign application")
		}
		withEntityUpdate[T](app, weak.id, func(state *T, lcx *Context[T]) {
			f(state, e, window, lcx)
		})
	}
}

// DeferIn schedules f to run at the end of the current effect cycle with
// this entity and the window (app/context.rs:305-317 defer_in). The
// callback is skipped when the entity released or the window closed.
func (cx *Context[S]) DeferIn(w *Window, f func(*S, *Window, *Context[S])) {
	if w == nil {
		panic("gpui: DeferIn requires a live window")
	}
	if f == nil {
		panic("gpui: DeferIn called with a nil callback")
	}
	entity := cx.entity
	cx.app.Defer(func(a *App) {
		if !windowIsOpen(w) {
			return
		}
		withEntityUpdate[S](a, entity, func(state *S, dcx *Context[S]) {
			f(state, w, dcx)
		})
	})
}

// AsyncWindowContext is the context handed to SpawnIn bodies: an
// AsyncApp bound to one window (app/async_context.rs:319-341).
type AsyncWindowContext struct {
	*AsyncApp
	window *Window
}

// Window returns the window this context is bound to (the reference
// window_handle).
func (cx *AsyncWindowContext) Window() *Window { return cx.window }

// Update posts one application update with the window and waits for it
// (app/async_context.rs:337-340 update → App::update_window). It reports
// ErrWindowClosed, without running f, when the window is gone.
func (cx *AsyncWindowContext) Update(f func(w *Window, app *App)) error {
	if f == nil {
		panic("gpui: AsyncWindowContext.Update called with a nil callback")
	}
	var err error
	cx.AsyncApp.Update(func(a *App) {
		if !windowIsOpen(cx.window) {
			err = ErrWindowClosed
			return
		}
		f(cx.window, a)
	})
	return err
}

// SpawnIn schedules body on the foreground executor with a weak handle
// to this entity and an AsyncWindowContext for the window
// (app/context.rs:676-683 spawn_in over window.rs:2656-2666
// Window::spawn). Like App.Spawn, the task defaults to the app root scope
// for cancellation.
func (cx *Context[S]) SpawnIn[R any](w *Window, body func(entity WeakEntity[S], cx *AsyncWindowContext) R) Task[R] {
	if w == nil {
		panic("gpui: SpawnIn requires a live window")
	}
	if body == nil {
		panic("gpui: SpawnIn called with a nil body")
	}
	weak := WeakEntity[S]{id: cx.entity, app: cx.app}
	return cx.app.Spawn(func(acx *AsyncApp) R {
		return body(weak, &AsyncWindowContext{AsyncApp: acx, window: w})
	})
}
