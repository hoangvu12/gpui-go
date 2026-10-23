package gpui

// This file is Base ticket 12's input simulation core: the window's
// keystroke entry point and the shared "simulate a platform input on a
// test window" path the test contexts (testapp.go TestApp,
// visualtest.go VisualTestContext and TestAppWindow) drive. It runs over
// the port's real dispatch entries (Window.dispatchInput for the mouse
// family, Window.dispatchKeyEvent for the keyboard family) inside one
// application update, whose flush draws the dirty test windows (Base
// ticket 33, R:app.rs:1800-1812), followed by the scheduler park
// (TestApp.RunUntilParked).
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (R: = reference/ce-source/crates/gpui/src):
//
//   - R:window.rs:5697-5720 Window::dispatch_keystroke — with_simulated_ime,
//     a KeyDown through dispatch_event, then the key_char delivered to the
//     window's input handler when the key down propagated;
//   - R:window.rs:1919-1927 — the platform input callback: dispatch_event
//     inside the window's update (handle.update);
//   - R:platform/test/window.rs:208-217 TestWindow::simulate_input — the
//     test window hands the event to that callback;
//   - R:app/test_context.rs:503-550 — TestAppContext::dispatch_action,
//     simulate_keystrokes (split on ' ', Keystroke::parse(..).unwrap()),
//     simulate_input (split on "") and dispatch_keystroke;
//   - R:elements/div.rs:3882-3901 Interactivity::compute_style.

import "fmt"

// testDisplaySize is the test platform's display size: TestDisplay::new
// bounds the display at (0,0)-(1920,1080) (R:platform/test/display.rs:16)
// and add_window_view / add_window / add_empty_window / TestApp::open_window
// open their windows at Bounds::maximized(None, cx), the primary
// display's bounds (R:geometry.rs:785-796, R:app/test_context.rs:262,297,322,
// R:app/test_app.rs:161).
var testDisplaySize = Size{Width: 1920, Height: 1080}

// ---------------------------------------------------------------------------
// Window::dispatch_keystroke (R:window.rs:5697-5720)
// ---------------------------------------------------------------------------

// DispatchKeystroke dispatches one keystroke the way a test or an
// automation types it (R:window.rs:5697-5720 Window::dispatch_keystroke):
// the keystroke's key_char is simulated (Keystroke.WithSimulatedIME, so
// "space" types " " and "a" types "a"), a KeyDown event runs through the
// window's keyboard dispatch, and when the key down propagated (no
// binding or listener stopped it) the key_char is delivered to the
// window's active input handler (InputHandler.DispatchInput). It reports
// whether the keystroke was handled: stopped by dispatch, or delivered as
// text. The dispatch runs inside one application update.
func (w *Window) DispatchKeystroke(keystroke Keystroke, cx AppContext) bool {
	if w == nil {
		panic("gpui: DispatchKeystroke requires a live window")
	}
	app := appFrom(cx)
	var handled bool
	app.Update(func(a *App) {
		handled = w.dispatchKeystroke(keystroke, a)
	})
	return handled
}

// dispatchKeystroke is dispatch_keystroke's body; it runs inside an
// update on the foreground thread.
func (w *Window) dispatchKeystroke(keystroke Keystroke, app *App) bool {
	keystroke = keystroke.WithSimulatedIME()
	propagate := w.dispatchKeyEvent(&KeyDownEvent{
		Keystroke:            keystroke,
		IsHeld:               false,
		PreferCharacterInput: false,
	}, app)
	if !propagate {
		return true
	}
	// The pin takes the platform input handler for the call and puts it
	// back afterwards (take_input_handler / set_input_handler); the
	// port's handler is the window's active input handler (input.go).
	if keystroke.KeyChar != "" {
		if handler := focusState(w).inputHandler; handler != nil {
			handler.DispatchInput(keystroke.KeyChar, w, app)
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Simulated platform input (R:platform/test/window.rs:208-217 →
// R:window.rs:1919-1927 → Window::dispatch_event)
// ---------------------------------------------------------------------------

// simulatedInput normalizes a simulated event to the pointer form the
// port's dispatch entries consume (the pin's InputEvent::to_platform_input
// wraps any of these values) and panics on a type no dispatch entry
// accepts.
func simulatedInput(event any) any {
	switch e := event.(type) {
	case MouseMoveEvent:
		return &e
	case MouseDownEvent:
		return &e
	case MouseUpEvent:
		return &e
	case MouseExitEvent:
		// Browser-core ticket 10: the window-exit event simulates the
		// pointer leaving the window (WM_MOUSELEAVE's dispatch).
		return &e
	case ScrollWheelEvent:
		return &e
	case PinchEvent:
		return &e
	case FileDropEvent:
		return &e
	case KeyDownEvent:
		return &e
	case KeyUpEvent:
		return &e
	case ModifiersChangedEvent:
		return &e
	case *MouseMoveEvent, *MouseDownEvent, *MouseUpEvent, *MouseExitEvent,
		*ScrollWheelEvent, *PinchEvent, *FileDropEvent, *KeyDownEvent, *KeyUpEvent, *ModifiersChangedEvent:
		if isNilReflect(event) {
			panic("gpui: simulated input event is a nil pointer")
		}
		return event
	case nil:
		panic("gpui: simulated input requires a non-nil event")
	}
	panic(fmt.Sprintf("gpui: %T is not a platform input event", event))
}

// dispatchPlatformInput is Window::dispatch_event over the port's split
// entries: keyboard events route to the keyboard dispatch
// (key_dispatch.go), every other platform input to the mouse/file-drop
// dispatch (filedrop.go). Runs inside an update.
func (w *Window) dispatchPlatformInput(event any, app *App) DispatchEventResult {
	switch event.(type) {
	case *KeyDownEvent, *KeyUpEvent, *ModifiersChangedEvent:
		propagate := w.dispatchKeyEvent(event, app)
		return DispatchEventResult{Propagate: propagate, DefaultPrevented: w.DefaultPrevented()}
	}
	return w.dispatchInput(event, app)
}

// requireOpenTestWindow is update_window's window lookup, unwrapped like
// the pinned helpers (R:app/test_context.rs:546-549 `.unwrap()`): a
// closed or foreign window panics.
func requireOpenTestWindow(ta *TestApp, w *Window) {
	if w == nil {
		panic("gpui: simulated input requires a window")
	}
	if w.app != ta.app {
		panic("gpui: the window belongs to a different application")
	}
	if open, ok := ta.app.windows[w.id]; !ok || open != w {
		panic("gpui: window not found (it was closed)")
	}
}

// simulateEvent is the shared body of VisualTestContext::simulate_event
// (R:app/test_context.rs:947-951) and TestAppWindow::simulate_event
// (R:app/test_app.rs:459-470): the event is dispatched inside the
// window's update (whose flush draws the dirty test windows), then the
// scheduler runs until parked. It returns the dispatch result
// (TestWindow::simulate_input reports !propagate; the port keeps the
// whole result).
func simulateEvent(ta *TestApp, w *Window, event any) DispatchEventResult {
	requireOpenTestWindow(ta, w)
	input := simulatedInput(event)
	var result DispatchEventResult
	ta.app.Update(func(app *App) {
		result = w.dispatchPlatformInput(input, app)
	})
	ta.RunUntilParked()
	return result
}

// parseSimulatedKeystrokes is simulate_keystrokes' parse
// (R:app/test_context.rs:521-524): the source splits on single spaces and
// every component parses, a parse failure panicking like the pinned
// unwrap.
func parseSimulatedKeystrokes(keystrokes string) []Keystroke {
	var out []Keystroke
	for _, component := range splitOnSpace(keystrokes) {
		out = append(out, MustParseKeystroke(component))
	}
	return out
}

// splitOnSpace is Rust's str::split(' '): every space separates, so
// consecutive spaces yield empty components (which parse as the empty
// key, Keystroke::parse("")).
func splitOnSpace(source string) []string {
	parts := []string{}
	start := 0
	for i := 0; i < len(source); i++ {
		if source[i] == ' ' {
			parts = append(parts, source[start:i])
			start = i + 1
		}
	}
	return append(parts, source[start:])
}

// splitOnEmpty is Rust's str::split(""): an empty leading component,
// one component per char, and an empty trailing component (simulate_input,
// R:app/test_context.rs:537, parses each, so the empty components
// dispatch empty-key KeyDowns that type nothing).
func splitOnEmpty(source string) []string {
	parts := []string{""}
	for _, ch := range source {
		parts = append(parts, string(ch))
	}
	return append(parts, "")
}

// ---------------------------------------------------------------------------
// Interactivity::compute_style (R:elements/div.rs:3882-3901)
// ---------------------------------------------------------------------------

// ComputeStyle computes the visual style of this div for its current
// state (R:elements/div.rs:3882-3901 Interactivity::compute_style): the
// base style refined by the focus, hover, drag-cursor and active styles,
// read through the element's retained interactive state under global
// (WithOptionalElementState; an id-less element has none). hitbox is the
// element's painted hitbox when the caller has one (hover then comes
// from the live hit test) or nil (hover comes from the retained hover
// state). Base's test-support observer calls it at paint with the
// element's own global id and hitbox (U:test_support.rs:355-358).
//
// The port's Div carries its Interactivity inline, so the method lives
// on DivElement; it belongs beside computeStyleInternal in interact.go
// and lives here until that file's owner moves it.
func (d *DivElement) ComputeStyle(global *GlobalElementID, hitbox *Hitbox, w *Window, app *App) Style {
	if d == nil {
		panic("gpui: ComputeStyle requires a div")
	}
	return WithOptionalElementState(w, global, func(slot *InteractiveElementState, w *Window) (Style, *InteractiveElementState) {
		es := interactiveStateOf(global, slot)
		return d.computeStyleInternal(hitbox, es, w, app), es
	})
}
