package gpui

// This file ports the text-input seam of the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/input.rs — the InputHandler trait's members the
//     keyboard path consumes (accepts_text_input,
//     replace_text_in_range and dispatch_input) plus UTF16Selection's
//     range shape. The IME-specific members (marked ranges, composition
//     replacement, bounds queries, input configuration) belong to the
//     IME ticket and are not part of this slice.
//   - crates/gpui/src/window.rs — PendingInput (the window's chord
//     state: keystrokes, focus identity, timeout task, needs_timeout)
//     and the pending_modifier lone-modifier tracking.
//
// The handler is registered during paint by focused elements
// (Window::handle_input) and read by the keyboard dispatch
// (prefer-character-input and replay) and by WM_CHAR text delivery.

import (
	"encoding/json"
	"reflect"
)

// ---------------------------------------------------------------------------
// InputHandler (input.rs InputHandler, bounded)
// ---------------------------------------------------------------------------

// UTF16Range is a range of UTF-16 code units in the handler's text
// (input.rs Range<usize> of UTF16Selection). The zero range is empty.
type UTF16Range struct {
	// Start is the first code unit of the range.
	Start int
	// End is one past the last code unit of the range.
	End int
}

// InputHandler receives text input on the focused element (the
// InputHandler members the keyboard path consumes). The IME ticket
// extends this interface with composition and bounds queries.
type InputHandler interface {
	// AcceptsTextInput reports whether the handler currently accepts
	// text input (InputHandler::accepts_text_input). When false (or
	// with no handler), keys prefer bindings and WM_CHAR text is
	// dropped.
	AcceptsTextInput(w *Window, app *App) bool
	// ReplaceTextInRange replaces the text of the given range (nil
	// replaces the selection, or inserts at the caret) with text
	// (InputHandler::replace_text_in_range). WM_CHAR text arrives here.
	ReplaceTextInRange(rng *UTF16Range, text string, w *Window, app *App)
	// DispatchInput delivers a replayed keystroke's character text
	// (window.rs replay_pending_input's input_handler.dispatch_input).
	DispatchInput(input string, w *Window, app *App)
}

// HandleInput registers the focused input handler for the current
// frame (window.rs handle_input): only a handler whose focus handle is
// focused registers; the last focused registration of the frame becomes
// the window's active input handler at frame completion.
func (w *Window) HandleInput(handle FocusHandle, handler InputHandler) {
	if w == nil {
		panic("gpui: HandleInput requires a live window")
	}
	if handler == nil {
		panic("gpui: HandleInput requires a non-nil handler")
	}
	registerInputHandler(w, handle, handler)
}

// SetInputHandler installs the window's active input handler directly
// (the platform seam: tests and the host use it when no element
// registered one). Passing nil clears the handler.
func (w *Window) SetInputHandler(handler InputHandler) {
	if w == nil {
		panic("gpui: SetInputHandler requires a live window")
	}
	focusState(w).inputHandler = handler
}

// ActiveInputHandler returns the window's active input handler, when
// any.
func (w *Window) ActiveInputHandler() InputHandler {
	if w == nil {
		return nil
	}
	return focusState(w).inputHandler
}

// deliverTextInput delivers WM_CHAR text through the active input
// handler (events.rs handle_char_msg: replace_text_in_range(None,
// input)).
func (w *Window) deliverTextInput(text string, app *App) {
	if handler := focusState(w).inputHandler; handler != nil {
		handler.ReplaceTextInRange(nil, text, w, app)
	}
}

// ---------------------------------------------------------------------------
// Pending input (window.rs PendingInput / pending_modifier)
// ---------------------------------------------------------------------------

// pendingInput is the window's chord state (window.rs PendingInput).
type pendingInput struct {
	// keystrokes are the pending chord keystrokes.
	keystrokes []Keystroke
	// focus is the focused handle identity the chord started under (0:
	// none). Input left over from a previous focus never completes a
	// binding.
	focus uint64
	// timer is the chord timeout task, when one is needed.
	timer *Task[struct{}]
	// needsTimeout records that this pending input requires the flush
	// timeout (a binding or text input may follow).
	needsTimeout bool
}

// modifierTracking is the lone-modifier synthesis state (window.rs
// pending_modifier).
type modifierTracking struct {
	// modifiers is the last reported modifier state.
	modifiers Modifiers
	// sawOtherInput reports that non-modifier input arrived since the
	// lone modifier press (suppresses the synthetic keystroke).
	sawOtherInput bool
}

// ---------------------------------------------------------------------------
// Default action payloads (ActionRegistry::build_action_type)
// ---------------------------------------------------------------------------

// defaultPayload constructs the action's default (zero) payload value
// through the descriptor's payload type (the reference's
// build_action_type: default construction; None for types that cannot
// be constructed, which the port reports as ok=false for payload types
// without a default construction — all Go types construct, so this is
// always ok=true for defined descriptors).
func (d *actionDescriptor) defaultPayload() (any, bool) {
	if d == nil || d.payloadType == nil {
		return nil, false
	}
	return reflect.New(d.payloadType).Elem().Interface(), true
}

// ---------------------------------------------------------------------------
// App keymap surface
// ---------------------------------------------------------------------------

// BindKeys adds bindings to the application's keymap (AppContext::
// bind_keys). The keymap feeds every window's dispatch tree on the next
// frame.
func (a *App) BindKeys(bindings ...KeyBinding) {
	if a.keymap == nil {
		a.keymap = NewKeymap(nil)
	}
	a.keymap.AddBindings(bindings)
}

// Keymap returns the application's keymap (creating an empty one on
// first use).
func (a *App) Keymap() *Keymap {
	if a.keymap == nil {
		a.keymap = NewKeymap(nil)
	}
	return a.keymap
}

// LoadKeymapJSON parses keymap JSON and binds its entries through this
// application's action registry (the keymap-file entry point; see
// ParseKeymapJSON for the format).
func (a *App) LoadKeymapJSON(data []byte) error {
	bindings, err := ParseKeymapJSON(data, func(name string, input json.RawMessage) (BoxedAction, error) {
		return a.BuildAction(name, input)
	}, DummyKeyboardMapper{})
	if err != nil {
		return err
	}
	a.BindKeys(bindings...)
	return nil
}
