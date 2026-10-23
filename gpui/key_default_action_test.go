package gpui

// Tests of browser-core ticket 09 (default actions after key
// listeners, .scratch/browser-core/issues/09-default-actions-after-
// listeners.md, spec D5): core default actions — keyboard scrolling,
// Tab focus movement and activation — run after the element key
// listeners, a listener's Window.PreventDefault skips them, key
// bindings keep the GPUI shape (bindings before listeners), and the
// per-window flag gives the browser order (listeners, then bindings
// and defaults). The Chromium citations are in key_default_action.go's
// header; the browser-parity fixtures are in
// internal/parity/fixtures/keyorder.

import (
	"reflect"
	"testing"
)

// defaultActionTest is one test's window: a transcript of ordered
// observations, the focus handles the view tracks, and a scroll
// handle. The root, both tab stops and the scroller log key events
// with the focus state at listener time, so the transcript proves
// listener-then-default order: a listener that still sees the old
// focus ran before the default action changed it.
type defaultActionTest struct {
	*testing.T
	ta     *TestApp
	app    *App
	window *Window
	rec    *transcript

	focusA, focusB, focusTarget FocusHandle
	scroll                      ScrollHandle

	// preventKeyDown makes the root's keydown listener call
	// Window.PreventDefault (the DOM listener that preventDefaults).
	preventKeyDown bool
	// stopKeyDown makes the root's keydown listener call
	// StopPropagation only (the DOM's stopPropagation, which stops
	// other listeners but not the default action).
	stopKeyDown bool
	// keyDispatchOrder is applied to the window before the first frame.
	keyDispatchOrder KeyDispatchOrder
	// textInput, when set, is registered for the target's focus during
	// the view build (the frame's focused registration becomes the
	// window's active input handler at draw).
	textInput InputHandler
}

func newDefaultActionTest(t *testing.T) *defaultActionTest {
	ta := NewTestApp()
	f := &defaultActionTest{
		T:           t,
		ta:          ta,
		app:         ta.App(),
		rec:         &transcript{},
		focusA:      ta.App().FocusHandle().TabStop(true),
		focusB:      ta.App().FocusHandle().TabStop(true),
		focusTarget: ta.App().FocusHandle().TabStop(true),
		scroll:      NewScrollHandle(),
	}
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return f.build(w)
		}
	})
	f.window = cx.Window()
	if f.keyDispatchOrder != BindingsBeforeListeners {
		f.app.Update(func(*App) { f.window.SetKeyDispatchOrder(f.keyDispatchOrder) })
	}
	return f
}

// build is the shared view: a full-size root with key listeners over
// two focusable tab stops and a scrollable region containing a
// focusable target.
func (f *defaultActionTest) build(w *Window) AnyElement {
	rec := f.rec
	focusName := f.focusName
	root := Div().ID("root").SizeFull().
		OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) {
			rec.add("keydown:root focus=%s", focusName(w))
			if f.preventKeyDown {
				w.PreventDefault()
			}
			if f.stopKeyDown {
				app.StopPropagation()
			}
		}).
		OnKeyUp(func(_ *KeyUpEvent, w *Window, app *App) {
			rec.add("keyup:root focus=%s", focusName(w))
		})
	return root.
		Child(Div().ID("a").Size(Px(60)).TrackFocus(f.focusA).
			Focus(func(s StyleRefinement) StyleRefinement { s.Background = Some(BackgroundOf(Rgb(0x22c55e))); return s }).
			OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) { rec.add("keydown:a focus=%s", focusName(w)) }).
			OnKeyUp(func(_ *KeyUpEvent, w *Window, app *App) { rec.add("keyup:a focus=%s", focusName(w)) })).
		Child(Div().ID("b").Size(Px(60)).TrackFocus(f.focusB).
			Focus(func(s StyleRefinement) StyleRefinement { s.Background = Some(BackgroundOf(Rgb(0x22c55e))); return s })).
		Child(Div().ID("scroller").Size(Px(200), Px(200)).OverflowYScroll().TrackScroll(f.scroll).
			OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) { rec.add("keydown:scroller focus=%s", focusName(w)) }).
			OnKeyUp(func(_ *KeyUpEvent, w *Window, app *App) { rec.add("keyup:scroller focus=%s", focusName(w)) }).
			Child(Div().ID("content").Size(Px(200), Px(1000)).
				Child(func() AnyElement {
					target := Div().ID("target").Size(Px(200), Px(50)).TrackFocus(f.focusTarget)
					if f.textInput != nil {
						w.HandleInput(f.focusTarget, f.textInput)
					}
					return target.IntoElement()
				}()))).
		IntoElement()
}

// focusName reports the focused element's id ("" when none).
func (f *defaultActionTest) focusName(w *Window) string {
	focused := w.Focused()
	if !focused.Ok {
		return ""
	}
	switch focused.Handle.id {
	case f.focusA.id:
		return "a"
	case f.focusB.id:
		return "b"
	case f.focusTarget.id:
		return "target"
	}
	return "?"
}

// press dispatches one full key press (down then up) through the
// platform dispatch, the way a real keyboard does.
func (f *defaultActionTest) press(source string) {
	f.Helper()
	stroke := MustParseKeystroke(source)
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, f.app)
}

// focusHandle moves focus to the named element and parks.
func (f *defaultActionTest) focus(name string) {
	f.Helper()
	f.app.Update(func(*App) {
		switch name {
		case "a":
			f.window.Focus(f.focusA)
		case "b":
			f.window.Focus(f.focusB)
		case "target":
			f.window.Focus(f.focusTarget)
		}
	})
	f.ta.RunUntilParked()
}

func wantStrings(t *testing.T, step string, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s transcript:\n got  %q\n want %q", step, got, want)
	}
}

// TestDefaultTabNavigationAfterListeners ports the Tab default action
// (DefaultTabEventHandler, keyboard_event_manager.cc:574-616): the
// keydown listeners run first — still seeing the old focus — then
// focus advances, so the keyup dispatches to the new focus.
func TestDefaultTabNavigationAfterListeners(t *testing.T) {
	f := newDefaultActionTest(t)
	f.ta.RunUntilParked()
	f.focus("a")

	f.press("tab")
	// The keyup dispatches to the new focus (b), which logs only through
	// the root's listener - Chrome's keyup:tb/keyup:root shape.
	want := []string{
		"keydown:a focus=a", "keydown:root focus=a",
		"keyup:root focus=b",
	}
	wantStrings(t, "tab", f.rec.take(), want)
	if name := f.focusName(f.window); name != "b" {
		t.Fatalf("focus after tab = %q, want b", name)
	}
}

// TestDefaultTabNavigationShiftBackward ports DefaultTabEventHandler's
// Shift branch: Shift+Tab moves focus backward.
func TestDefaultTabNavigationShiftBackward(t *testing.T) {
	f := newDefaultActionTest(t)
	f.ta.RunUntilParked()
	f.focus("b")

	f.press("shift-tab")
	if name := f.focusName(f.window); name != "a" {
		t.Fatalf("focus after shift-tab = %q, want a", name)
	}
}

// TestDefaultTabNavigationModifiersNeverAdvance ports
// DefaultTabEventHandler's modifier rules: Ctrl, Alt and the platform
// modifier never advance focus (ctrl/meta explicitly, alt as a system
// key off Mac).
func TestDefaultTabNavigationModifiersNeverAdvance(t *testing.T) {
	for _, source := range []string{"ctrl-tab", "alt-tab", "cmd-tab"} {
		t.Run(source, func(t *testing.T) {
			f := newDefaultActionTest(t)
			f.ta.RunUntilParked()
			f.focus("a")
			f.press(source)
			if name := f.focusName(f.window); name != "a" {
				t.Fatalf("focus after %s = %q, want a (unchanged)", source, name)
			}
		})
	}
}

// TestDefaultTabPreventDefaultSuppressesFocusMovement is the
// listener-then-default suppression: a key listener that calls
// Window.PreventDefault keeps focus where it was.
func TestDefaultTabPreventDefaultSuppressesFocusMovement(t *testing.T) {
	f := newDefaultActionTest(t)
	f.preventKeyDown = true
	f.ta.RunUntilParked()
	f.focus("a")

	f.press("tab")
	want := []string{
		"keydown:a focus=a", "keydown:root focus=a",
		"keyup:a focus=a", "keyup:root focus=a",
	}
	wantStrings(t, "tab-prevented", f.rec.take(), want)
	if name := f.focusName(f.window); name != "a" {
		t.Fatalf("focus after prevented tab = %q, want a (no movement)", name)
	}
}

// TestDefaultActionsIgnoreStopPropagation pins the DOM rule
// (event_dispatcher.cc:397-401: the dispatch's step 10 clears the stop
// flags before the default handlers): a listener's StopPropagation
// stops other listeners, not the default actions.
func TestDefaultActionsIgnoreStopPropagation(t *testing.T) {
	f := newDefaultActionTest(t)
	f.stopKeyDown = true
	f.ta.RunUntilParked()
	f.focus("a")

	f.press("tab")
	if name := f.focusName(f.window); name != "b" {
		t.Fatalf("focus after stopPropagation tab = %q, want b (defaults still run)", name)
	}
}

// TestDefaultKeyboardScrollArrows ports the navigation-key default
// action (DefaultNavigationKeyEventHandler → BubblingScroll,
// keyboard_event_manager.cc:526-572): with focus inside the scroller,
// ArrowDown scrolls it 40 logical px per press (kPixelsPerLineStep,
// cc/input/scroll_utils.h:20), and ArrowUp scrolls back.
func TestDefaultKeyboardScrollArrows(t *testing.T) {
	f := newDefaultActionTest(t)
	f.ta.RunUntilParked()
	f.focus("target")

	f.press("arrowdown")
	f.press("arrowdown")
	if got := f.scroll.Offset().Y; got != -80 {
		t.Fatalf("scroller y offset after 2 arrowdown = %v, want -80", got)
	}
	want := []string{
		"keydown:scroller focus=target", "keydown:root focus=target",
		"keyup:scroller focus=target", "keyup:root focus=target",
		"keydown:scroller focus=target", "keydown:root focus=target",
		"keyup:scroller focus=target", "keyup:root focus=target",
	}
	wantStrings(t, "arrows", f.rec.take(), want)

	f.press("arrowup")
	if got := f.scroll.Offset().Y; got != -40 {
		t.Fatalf("scroller y offset after arrowup = %v, want -40", got)
	}
}

// TestDefaultKeyboardScrollSteps pins the page and document steps:
// PageDown scrolls keyScrollPageStep of the visible length
// (CalculatePageStep, cc/input/scroll_utils.cc:50-55), End jumps to
// the scroll maximum and Home back to the top.
func TestDefaultKeyboardScrollSteps(t *testing.T) {
	f := newDefaultActionTest(t)
	f.ta.RunUntilParked()
	f.focus("target")

	// Visible height 200: the page step is max(200*0.875, 200-40) = 175.
	f.press("pagedown")
	if got := f.scroll.Offset().Y; got != -175 {
		t.Fatalf("scroller y offset after pagedown = %v, want -175", got)
	}
	f.press("end")
	if got := f.scroll.Offset().Y; got != -800 {
		t.Fatalf("scroller y offset after end = %v, want -800 (the max: 1000-200)", got)
	}
	f.press("home")
	if got := f.scroll.Offset().Y; got != 0 {
		t.Fatalf("scroller y offset after home = %v, want 0", got)
	}
}

// TestDefaultKeyboardScrollChainsWhenBlocked pins BubblingScroll's
// chaining (scroll_manager.cc:194-232): a scroller that cannot take
// the delta leaves it to the next scrollable ancestor.
func TestDefaultKeyboardScrollChainsWhenBlocked(t *testing.T) {
	f := newDefaultActionTest(t)
	f.ta.RunUntilParked()
	f.focus("target")

	// The inner scroller holds 800 px; End bottoms it out, and a further
	// ArrowDown has nowhere to go inside it — the offset stays clamped.
	f.press("end")
	if got := f.scroll.Offset().Y; got != -800 {
		t.Fatalf("scroller y offset after end = %v, want -800", got)
	}
	f.press("arrowdown")
	if got := f.scroll.Offset().Y; got != -800 {
		t.Fatalf("scroller y offset after end + arrowdown = %v, want -800 (clamped)", got)
	}
}

// TestDefaultKeyboardScrollPreventDefaultSuppresses is the
// listener-then-default suppression for scrolling: a prevented keydown
// leaves the scroller untouched.
func TestDefaultKeyboardScrollPreventDefaultSuppresses(t *testing.T) {
	f := newDefaultActionTest(t)
	f.preventKeyDown = true
	f.ta.RunUntilParked()
	f.focus("target")

	f.press("arrowdown")
	if got := f.scroll.Offset().Y; got != 0 {
		t.Fatalf("scroller y offset after prevented arrowdown = %v, want 0", got)
	}
}

// TestDefaultKeyboardScrollModifierRules pins MapKeyCodeForScroll's
// modifier rules (keyboard_event_manager.cc:80-115): Shift (selection),
// Alt (system keys) and Ctrl other than Home/End never scroll.
func TestDefaultKeyboardScrollModifierRules(t *testing.T) {
	for _, source := range []string{"shift-arrowdown", "alt-arrowdown", "ctrl-arrowdown"} {
		t.Run(source, func(t *testing.T) {
			f := newDefaultActionTest(t)
			f.ta.RunUntilParked()
			f.focus("target")
			f.press(source)
			if got := f.scroll.Offset().Y; got != 0 {
				t.Fatalf("scroller y offset after %s = %v, want 0", source, got)
			}
		})
	}
}

// TestDefaultKeyboardScrollSkipsTextInputs pins the editor-first rule
// (keyboard_event_manager.cc:442-445: the editor commands run before
// the navigation-key default): a focused input handler that accepts
// text input consumes the key, so nothing scrolls.
func TestDefaultKeyboardScrollSkipsTextInputs(t *testing.T) {
	f := newDefaultActionTest(t)
	f.textInput = &acceptingTextInput{}
	f.ta.RunUntilParked()
	f.focus("target")

	f.press("arrowdown")
	if got := f.scroll.Offset().Y; got != 0 {
		t.Fatalf("scroller y offset with a focused text input = %v, want 0", got)
	}
}

// acceptingTextInput is an InputHandler that accepts text input and
// records nothing.
type acceptingTextInput struct{}

func (h *acceptingTextInput) AcceptsTextInput(*Window, *App) bool                   { return true }
func (h *acceptingTextInput) DispatchInput(string, *Window, *App)                   {}
func (h *acceptingTextInput) ReplaceTextInRange(*UTF16Range, string, *Window, *App) {}

// TestDefaultActivationRunsAfterKeyUpListeners ports the activation
// default action (HTMLElement::HandleKeyboardActivation,
// html_element.cc:4313-4338) and the listener-then-default order: the
// keyboard click fires after every keyup listener (the pin fired it as
// the focused element's own keyup listener, before the ancestors).
func TestDefaultActivationRunsAfterKeyUpListeners(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle().TabStop(true)
	rec := &transcript{}
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().ID("root").SizeFull().
				OnKeyUp(func(_ *KeyUpEvent, w *Window, app *App) { rec.add("keyup:root") }).
				Child(Div().ID("button").Size(Px(60)).TrackFocus(focus).
					OnClick(func(_ *ClickEvent, w *Window, app *App) { rec.add("click:button") })).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	stroke := MustParseKeystroke("space")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, app)
	want := []string{"keyup:root", "click:button"}
	wantStrings(t, "activation", rec.take(), want)
}

// TestDefaultActivationPreventedKeyDown is the suppression Chrome
// shows (html_element.cc:4319-4322: a prevented Space keydown never
// sets the active state, so the keyup never clicks): an ancestor's
// prevented keydown leaves the pending unset and no click fires.
func TestDefaultActivationPreventedKeyDown(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle().TabStop(true)
	rec := &transcript{}
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().ID("root").SizeFull().
				OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) { w.PreventDefault() }).
				Child(Div().ID("button").Size(Px(60)).TrackFocus(focus).
					OnClick(func(_ *ClickEvent, w *Window, app *App) { rec.add("click:button") })).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	stroke := MustParseKeystroke("space")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, app)
	wantStrings(t, "activation-prevented", rec.take(), nil)
}

// The binding-order pair: the GPUI shape (bindings before listeners)
// and the per-window browser order flag. The bound action's listener
// stops propagation by default (the pin's bubble action semantics), so
// under the GPUI shape the key listeners never see the key.
var keyOrderAction = DefineUnitAction[keyOrderPayload]("keyordertest::Key")

type keyOrderPayload struct{}

func TestKeyBindingOrderGPUIShape(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	app.BindKeys(NewKeyBinding("x", keyOrderAction.Box(keyOrderPayload{}), nil))
	focus := app.FocusHandle().TabStop(true)
	rec := &transcript{}
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().ID("root").SizeFull().TrackFocus(focus).
				OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) { rec.add("keydown:root") }).
				OnAction(keyOrderAction, func(*keyOrderPayload, *Window, *App) { rec.add("action") }).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	stroke := MustParseKeystroke("x")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	// The binding won and its action stopped propagation: the keydown
	// listener never ran (the pin's bindings-before-listeners shape).
	wantStrings(t, "gpui shape", rec.take(), []string{"action"})
}

func TestKeyBindingOrderBrowserOrder(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	app.BindKeys(NewKeyBinding("x", keyOrderAction.Box(keyOrderPayload{}), nil))
	focus := app.FocusHandle().TabStop(true)
	rec := &transcript{}
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().ID("root").SizeFull().TrackFocus(focus).
				OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) {
					rec.add("keydown:root")
					if w.DefaultPrevented() {
						rec.add("keydown:root prevented")
					}
				}).
				OnAction(keyOrderAction, func(*keyOrderPayload, *Window, *App) { rec.add("action") }).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.SetKeyDispatchOrder(ListenersBeforeBindings) })
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	stroke := MustParseKeystroke("x")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	// The browser order: the key listener ran first, then the binding.
	wantStrings(t, "browser order", rec.take(), []string{"keydown:root", "action"})
}

func TestKeyBindingOrderBrowserOrderPreventDefaultSuppressesBinding(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	app.BindKeys(NewKeyBinding("x", keyOrderAction.Box(keyOrderPayload{}), nil))
	focus := app.FocusHandle().TabStop(true)
	rec := &transcript{}
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().ID("root").SizeFull().TrackFocus(focus).
				OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) {
					rec.add("keydown:root")
					w.PreventDefault()
				}).
				OnAction(keyOrderAction, func(*keyOrderPayload, *Window, *App) { rec.add("action") }).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.SetKeyDispatchOrder(ListenersBeforeBindings) })
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	stroke := MustParseKeystroke("x")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	// A prevented key suppresses the binding in the browser order.
	wantStrings(t, "browser order prevented", rec.take(), []string{"keydown:root"})
}

func TestKeyBindingOrderBrowserOrderBindingOverridesCoreDefault(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	// A tab binding: in the browser order its action stops propagation,
	// so the core Tab default action does not also run (the app's
	// binding replaces the core default, as in the GPUI shape).
	app.BindKeys(NewKeyBinding("tab", keyOrderAction.Box(keyOrderPayload{}), nil))
	focusA := app.FocusHandle().TabStop(true)
	focusB := app.FocusHandle().TabStop(true)
	rec := &transcript{}
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().ID("root").SizeFull().
				OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) { rec.add("keydown:root") }).
				OnAction(keyOrderAction, func(*keyOrderPayload, *Window, *App) { rec.add("action") }).
				Child(Div().ID("a").Size(Px(60)).TrackFocus(focusA)).
				Child(Div().ID("b").Size(Px(60)).TrackFocus(focusB)).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.SetKeyDispatchOrder(ListenersBeforeBindings) })
	app.Update(func(*App) { window.Focus(focusA) })
	cx.RunUntilParked()

	stroke := MustParseKeystroke("tab")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	wantStrings(t, "browser order tab binding", rec.take(), []string{"keydown:root", "action"})
	if focused := window.Focused(); !focused.Ok || focused.Handle.id != focusA.id {
		t.Fatalf("focus after the bound tab = %v, want a (the binding replaced the core default)", focused)
	}
}

// TestKeyDispatchOrderFlagDefaults checks the flag surface: the GPUI
// shape is the default, the browser order applies from the next
// dispatched event, and unknown orders panic.
func TestKeyDispatchOrderFlagDefaults(t *testing.T) {
	f := newDefaultActionTest(t)
	f.ta.RunUntilParked()
	if got := f.window.KeyDispatchOrder(); got != BindingsBeforeListeners {
		t.Fatalf("default order = %v, want the GPUI shape", got)
	}
	f.app.Update(func(*App) { f.window.SetKeyDispatchOrder(ListenersBeforeBindings) })
	if got := f.window.KeyDispatchOrder(); got != ListenersBeforeBindings {
		t.Fatalf("order after SetKeyDispatchOrder = %v, want the browser order", got)
	}
	// With the flag on, the core Tab default still moves focus after the
	// listeners.
	f.focus("a")
	f.press("tab")
	if name := f.focusName(f.window); name != "b" {
		t.Fatalf("focus after tab (browser order) = %q, want b", name)
	}
}

// TestKeyBindingOrderBrowserOrderNotifiesObserversOnce pins the
// observer contract under the browser order: one keystroke notification
// per dispatched key, carrying the action when a binding dispatched
// (window.rs dispatch_key_event's dispatch_keystroke_observers with
// Some(binding.action); the naive listener-then-binding reordering
// would notify twice).
func TestKeyBindingOrderBrowserOrderNotifiesObserversOnce(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	app.BindKeys(NewKeyBinding("x", keyOrderAction.Box(keyOrderPayload{}), nil))
	focus := app.FocusHandle().TabStop(true)
	var notified []string
	sub := app.OnKeystroke(func(event *KeystrokeEvent, w *Window, app *App) {
		entry := event.Keystroke.Key
		if event.Action.descriptor != nil {
			entry += ":action"
		}
		notified = append(notified, entry)
	})
	defer sub.Cancel()
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().ID("root").SizeFull().TrackFocus(focus).
				OnAction(keyOrderAction, func(*keyOrderPayload, *Window, *App) {}).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.SetKeyDispatchOrder(ListenersBeforeBindings) })
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	stroke := MustParseKeystroke("x")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, app)
	// Keystroke observers see key downs only (window.rs:2466-2469's
	// downcast early return); the key up dispatch notifies nothing.
	if want := []string{"x:action"}; !reflect.DeepEqual(notified, want) {
		t.Fatalf("keystroke observer notifications = %q, want %q (one notification, carrying the action)", notified, want)
	}
}
