package gpui

// This file is browser-core ticket 17
// (.scratch/browser-core/issues/17-context-menu-event.md, spec story
// 60): the contextmenu event — the DOM's menu event, delivered from
// the right-button release, the Menu key (on key-up, on Windows) and
// Shift+F10 (on key-down), with a keyboard-invoked position at the
// focused element.
//
// The Chromium reference (0d4dcd4f, %TEMP%\research-src\chromium):
//
//   - The mouse trigger: WebFrameWidgetImpl::HandleMouseUp dispatches
//     the contextmenu event AFTER the whole up handling — "Dispatch
//     the contextmenu event regardless of if the click was swallowed"
//     (web_frame_widget_impl.cc:1253-1260, the
//     context_menu_on_mouse_up setting: web_preferences.h:64 defaults
//     it to BUILDFLAG(IS_WIN)) — through MouseContextMenu
//     (web_frame_widget_impl.cc:1215-1245) → SendContextMenuEvent,
//     which hit-tests at the RELEASE position and dispatches a
//     bubbling contextmenu MouseEvent whose target is the element
//     under the pointer (event_handler.cc:2183-2228). Mac's mousedown
//     arm is the non-Windows shape (web_frame_widget_impl.cc:
//     1192-1204) and is not ported: this port is Windows-only.
//   - The keyboard trigger: WebFrameWidgetImpl::HandleKeyEvent runs
//     the DOM key dispatch first (KeyEvent → the keydown/keyup
//     listeners, keyboard_event_manager.cc:226-419) and only when it
//     returned NotHandled — no listener canceled the event — do the
//     menu keys fire (web_frame_widget_impl.cc:1112-1141): on Windows
//     the unmodified Menu key (VKEY_APPS) triggers on KEY UP
//     (is_context_menu_key_triggering_event, :1115-1118) and
//     Shift+F10 — Shift alone held — triggers on KEY DOWN
//     (is_shift_f10, :1125-1131). Both arms return kHandledSystem:
//     the key is consumed (the recorder's keyup still dispatches its
//     DOM event; only the menu decision is consumed).
//   - The keyboard position: ShowNonLocatedContextMenu
//     (event_handler.cc:2245-2316) places the menu at the FOCUSED
//     element's centre — GetFocusedElementRectForNonLocatedContextMenu
//     — or, with nothing focused, at the viewport origin plus
//     kContextMenuMargin (1 px, :2256-2257, :2320-2323); the event
//     targets the focused element's chain (the no-focus case hits
//     whatever sits at that corner, the body for a plain page).
//   - The default action — showing the browser's menu — is the
//     embedder's; preventDefault on the event suppresses nothing else
//     in the core (the port has no menu, so the event is purely
//     informational: the acceptance's "suppresses nothing else").
//
// Divergences toward CSS, recorded per D0: the port's synthesized
// follow-up shares the triggering platform dispatch's
// default-prevented flag (the mouse-up's or the key-up's result
// reports a contextmenu listener's PreventDefault) — the same
// coarseness the activation click already carries
// (mouseevent.go dispatchMouseClick); a fixture that needs the
// contextmenu's own prevented state binds only the contextmenu kind
// (the fixtures/contextmenu prevented record is the shape).

// ContextMenuEventKind names the contextmenu event's trigger.
type ContextMenuEventKind uint8

const (
	// ContextMenuEventMouse is the right-button release trigger.
	ContextMenuEventMouse ContextMenuEventKind = iota
	// ContextMenuEventKeyboard is the Menu key or Shift+F10 trigger.
	ContextMenuEventKeyboard
)

// String renders the kind for diagnostics.
func (k ContextMenuEventKind) String() string {
	switch k {
	case ContextMenuEventMouse:
		return "mouse"
	case ContextMenuEventKeyboard:
		return "keyboard"
	}
	return "unknown"
}

// ContextMenuEvent is the contextmenu event (the DOM's MouseEvent of
// the same name): the right-button release, the Menu key or Shift+F10
// delivered it, Position is where a menu should appear (the mouse
// position or the focused element's centre) and Modifiers were held at
// the trigger. A listener calls Window.PreventDefault to claim the
// event — the core itself does nothing with the flag (the acceptance:
// preventDefault on contextmenu suppresses nothing else).
type ContextMenuEvent struct {
	// Kind names the trigger.
	Kind ContextMenuEventKind
	// Position is the menu position in window-local logical pixels:
	// the release position for the mouse trigger, the focused
	// element's centre (or the viewport corner plus 1 px with nothing
	// focused) for the keyboard one.
	Position Point
	// Modifiers were held at the trigger (Shift for Shift+F10).
	Modifiers Modifiers
}

// IsKeyboard reports whether the event was keyboard-invoked (the Menu
// key or Shift+F10).
func (e *ContextMenuEvent) IsKeyboard() bool { return e.Kind == ContextMenuEventKeyboard }

// ---------------------------------------------------------------------------
// The listener registration
// ---------------------------------------------------------------------------

// OnContextMenu binds a contextmenu listener (browser-core ticket 17):
// the listener runs when the right button is released over this
// element (the event bubbles from the element under the release), or
// when the Menu key or Shift+F10 fires while this element (or a
// descendant) holds focus. The element needs an ID (the registration
// keys on it, like OnClick).
func (d *DivElement) OnContextMenu(f func(*ContextMenuEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnContextMenu requires a non-nil listener")
	}
	d.contextMenuListeners = append(d.contextMenuListeners, f)
	return d
}

// nodeContextMenu is one node's contextmenu registration
// (browser-core ticket 17): the keyboard trigger's dispatch reads it
// from the FOCUSED node's path — the listeners fire focused-first
// down the path, the DOM's bubbling — and the bounds place the
// keyboard-invoked menu at the focused element's centre.
type nodeContextMenu struct {
	// listeners are the element's OnContextMenu handlers.
	listeners []func(*ContextMenuEvent, *Window, *App)
	// bounds are the element's prepaint hitbox bounds (the keyboard
	// position, ShowNonLocatedContextMenu's GetFocusedElementRect...).
	bounds Bounds
}

// contextMenuRegistration is one element's paint-side registration on
// the frame (browser-core ticket 17): the mouse trigger's dispatch
// resolves the release hit-test chain's element paths against it,
// innermost first.
type contextMenuRegistration struct {
	// path is the owning element's global id key (the hitbox's
	// ownerPath).
	path string
	// listeners are the element's OnContextMenu handlers.
	listeners []func(*ContextMenuEvent, *Window, *App)
}

// registerDivContextMenu registers the element's contextmenu listeners
// on its dispatch node (browser-core ticket 17): called from the div's
// prepaint, after the keyboard click (div.rs's paint_mouse_listeners
// before paint_keyboard_listeners order), so the keyboard trigger's
// default action can find them from the focused node's path.
// Suppressed subtrees (hidden, inert — div.go) register nothing: an
// inert element cannot hold focus and the hidden are not dispatched
// to.
func registerDivContextMenu(w *Window, d *DivElement, hitbox *Hitbox) {
	if len(d.contextMenuListeners) == 0 || hitbox == nil {
		return
	}
	tree := currentDispatchTree(w)
	registration := nodeContextMenu{
		listeners: append([]func(*ContextMenuEvent, *Window, *App){}, d.contextMenuListeners...),
		bounds:    hitbox.Bounds,
	}
	tree.SetContextMenu(registration)
}

// appendContextMenuRegistration registers the element's contextmenu
// listeners on the frame under construction (browser-core ticket 17):
// the mouse trigger's dispatch resolves the release chain by these
// path keys. Registered during paint, so the completed frame carries
// them.
func appendContextMenuRegistration(w *Window, registration contextMenuRegistration) {
	fs := focusState(w)
	if fs.next == nil {
		return
	}
	fs.next.contextMenuListeners = append(fs.next.contextMenuListeners, registration)
}

// ---------------------------------------------------------------------------
// The dispatch
// ---------------------------------------------------------------------------

// dispatchContextMenu fires one contextmenu event at the given chain
// (browser-core ticket 17): the mouse trigger's chain is the release
// hit-test's element paths, the keyboard trigger's the focused node's
// dispatch path — both innermost-first, the DOM's bubbling. All
// listeners share one event; a listener's Window.PreventDefault sets
// the window flag (the dispatch's result reports it), and nothing else
// in the core reads it (the menu itself is the app's).
func (w *Window) dispatchContextMenu(event *ContextMenuEvent, app *App, chain []contextMenuTarget) {
	if len(chain) == 0 {
		return
	}
	for _, target := range chain {
		for _, listener := range target.listeners {
			listener(event, w, app)
		}
	}
	w.requestRefresh()
}

// contextMenuTarget is one chain entry of a contextmenu dispatch: a
// node's registration (the keyboard path) or a frame registration (the
// mouse path), flattened.
type contextMenuTarget struct {
	listeners []func(*ContextMenuEvent, *Window, *App)
}

// dispatchContextMenuFromMouse is the right-button release's trigger
// (browser-core ticket 17; Chromium WebFrameWidgetImpl::HandleMouseUp
// → MouseContextMenu → SendContextMenuEvent, web_frame_widget_impl.cc:
// 1253-1260/:1215-1245): after the up's whole tail — the listeners,
// the capture release, the click/auxclick and the boundary settling —
// and regardless of the click having been swallowed (no
// default-prevented gate), the contextmenu event targets the element
// under the RELEASE position and bubbles to the root.
func (w *Window) dispatchContextMenuFromMouse(up *MouseUpEvent, app *App) {
	if w == nil || up.Button != MouseButtonRight {
		return
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return
	}
	is := inputState(w)
	paths := chainPathsOf(fs.rendered, is.mouseHitTest)
	if len(paths) == 0 {
		return
	}
	registrations := fs.rendered.contextMenuListeners
	var chain []contextMenuTarget
	for _, path := range paths {
		for i := range registrations {
			if registrations[i].path == path {
				chain = append(chain, contextMenuTarget{listeners: registrations[i].listeners})
			}
		}
	}
	event := &ContextMenuEvent{
		Kind:      ContextMenuEventMouse,
		Position:  up.Position,
		Modifiers: up.Modifiers,
	}
	w.dispatchContextMenu(event, app, chain)
}

// contextMenuKeyMargin is kContextMenuMargin (event_handler.cc:2256):
// the keyboard menu's fallback position — the viewport origin plus 1
// logical pixel — when nothing holds focus.
const contextMenuKeyMargin float32 = 1

// dispatchContextMenuFromKey is the keyboard trigger (browser-core
// ticket 17; Chromium WebFrameWidgetImpl::HandleKeyEvent's menu arms,
// web_frame_widget_impl.cc:1112-1141): the unmodified Menu key at KEY
// UP (Windows: is_context_menu_key_triggering_event, :1115-1118) and
// Shift+F10 — Shift alone — at KEY DOWN (:1125-1131), both after the
// DOM key dispatch found the event unhandled (runKeyDefaultActions'
// default-prevented gate is that NotHandled). The event targets the
// FOCUSED element's chain (focused-first, bubbling) and the position
// is the focused element's centre — ShowNonLocatedContextMenu's
// GetFocusedElementRectForNonLocatedContextMenu, event_handler.cc:
// 2245-2316 — or the viewport corner plus 1 px with nothing focused
// (:2320-2323). It reports whether the event fired (the key is
// consumed — kHandledSystem).
func (w *Window) dispatchContextMenuFromKey(event any, app *App) bool {
	if w == nil {
		return false
	}
	var stroke Keystroke
	switch e := event.(type) {
	case *KeyDownEvent:
		// Shift+F10 fires on key down, Shift alone held
		// (web_frame_widget_impl.cc:1125-1131).
		stroke = e.Keystroke
		if stroke.Key != "f10" {
			return false
		}
		m := stroke.Modifiers
		if !m.Shift || m.Control || m.Alt || m.Platform {
			return false
		}
	case *KeyUpEvent:
		// The unmodified Menu key fires on key up on Windows
		// (web_frame_widget_impl.cc:1115-1118, :1120-1123).
		stroke = e.Keystroke
		if stroke.Key != "menu" {
			return false
		}
		if stroke.Modifiers.Modified() {
			return false
		}
	default:
		return false
	}
	fs := focusState(w)
	tree := fs.renderedDispatchTree()
	path, nodeID := w.focusDispatchPath()
	position := Point{X: contextMenuKeyMargin, Y: contextMenuKeyMargin}
	if fs.focused != 0 {
		if node := tree.Node(nodeID); node != nil && node.contextMenu != nil {
			bounds := node.contextMenu.bounds
			position = Point{
				X: bounds.Origin.X + bounds.Size.Width/2,
				Y: bounds.Origin.Y + bounds.Size.Height/2,
			}
		}
	}
	// The focused node's path, focused-first (the DOM's bubbling); a
	// node with no registration still passes its position through —
	// only the focused element's bounds place the menu.
	var chain []contextMenuTarget
	for i := len(path) - 1; i >= 0; i-- {
		if node := tree.Node(path[i]); node != nil && node.contextMenu != nil {
			chain = append(chain, contextMenuTarget{listeners: node.contextMenu.listeners})
		}
	}
	if len(chain) == 0 {
		// Nothing on the chain listens: the key is still consumed (the
		// menu would have opened at the focused element).
		return true
	}
	w.dispatchContextMenu(&ContextMenuEvent{
		Kind:      ContextMenuEventKeyboard,
		Position:  position,
		Modifiers: stroke.Modifiers,
	}, app, chain)
	return true
}
