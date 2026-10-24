//go:build windows

// Package keycode is ticket 13's fixture set (key code and location,
// with Shift kept; .scratch/browser-core/issues/13-key-code-and-shift.md,
// spec story 53): key events carry the physical code and location the
// way Chrome reports them on a US layout, and a binding written in
// either form fires for the same keystroke.
//
// The three Chrome fixtures:
//
//   - keycode-shifted: US-layout shifted punctuation (Shift+], Shift+1,
//     Shift+;), a plain letter (a) and a numpad key (Numpad1), logged
//     as keydown/keyup entries whose key, code and location fields must
//     match Chrome's e.key/e.code/e.location: the shifted character
//     with Shift kept, the physical code, and the numpad location.
//   - keycode-binding-code / keycode-binding-char: Ctrl+Shift+] with a
//     binding written ctrl-shift-] (the code form) or ctrl-} (the key
//     form) under the browser dispatch order — the same platform
//     keystroke must fire either binding (keycode_test.go replays both
//     and asserts each action fired exactly once).
//
// The two binding fixtures are the same page and steps; only the
// binding's spelling differs, which is the ticket's acceptance: "a
// binding written as ctrl-shift-] and one written as ctrl-} both fire
// for the same keystroke".
//
// The fixtures click the box first (the keyorder-tab shape): key
// dispatch targets the focused element's path, and with nothing
// focused the port's dispatch tree delivers keys to its synthetic root
// node — a path the page's root listener is not on — where Chrome
// would target the body. A focused box puts both sides on the same
// listener path (box then root), and the entries' focus field carries
// it.
package keycode

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' shared numbers (the keyorder fixtures' shape: the pin's
// flex default and 1.618 line height pinned explicitly so both sides
// agree, flat colors so Tier C sees any real defect).
const (
	windowWidth  = 480.0
	windowHeight = 360.0

	boxWidth  = 120.0
	boxHeight = 60.0

	pageBackground = 0xffffff
	boxBackground  = 0xe2e8f0
	focusColor     = 0x22c55e
)

// The click position (logical pixels): the box sits at the top-left
// corner of the flex column.
const (
	clickX = 60.0
	clickY = 30.0
)

// keyFiredAction is the binding fixtures' bound action. It has no
// transcript side effect on purpose (Chrome has no key bindings): the
// fixtures' tests assert it fired through the firedActions counter.
var keyFiredAction = gpui.DefineUnitAction[keyFiredPayload]("keycode::Fired")

type keyFiredPayload struct{}

// firedActions counts the binding fixtures' action dispatches (the
// package's tests read it; FiredActions resets and reports it).
var firedActions int

// FiredActions reports how many times the binding fixtures' action has
// dispatched since the last call (the counter resets).
func FiredActions() int {
	fired := firedActions
	firedActions = 0
	return fired
}

// ShiftedFixture is the key/code/location fixture: a click focuses the
// box, then five US-layout key presses whose keydown/keyup entries
// carry the DOM's key, code and location. Shift+]/Shift+1/Shift+; prove
// the shifted character with Shift kept (key "}", "!", ":"), a proves
// the plain letter (KeyA), and the numpad step proves the location
// (Numpad1 at 3) with the digit key.
var ShiftedFixture = parity.Fixture{
	Name:   "keycode-shifted",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Click(clickX, clickY),
		parity.Key("shift-]"),
		parity.Key("shift-1"),
		parity.Key("shift-;"),
		parity.Key("a"),
		parity.KeyCode("1", "Numpad1"),
	},
	BoxIDs:         []string{"box", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: ShiftedView(),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run); the
		// keyorder fixtures' flat-page numbers (0.0 measured mean/hot
		// everywhere) with the same headroom.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// BindingCodeFixture is the code-form binding fixture: a click focuses
// the box, then Ctrl+Shift+] with "ctrl-shift-]" bound (the spelling
// that never fired before ticket 13 — the pin folded the keystroke to
// ctrl-} and cleared Shift).
var BindingCodeFixture = parity.Fixture{
	Name:   "keycode-binding-code",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Click(clickX, clickY),
		parity.Key("ctrl-shift-]"),
	},
	BoxIDs:         []string{"box", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: BindingView("ctrl-shift-]"),
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// BindingCharFixture is the key-form binding fixture: the same
// Ctrl+Shift+] keystroke with "ctrl-}" bound (the shifted character
// without shift — the form the pin's folded keystroke matched).
var BindingCharFixture = parity.Fixture{
	Name:   "keycode-binding-char",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Click(clickX, clickY),
		parity.Key("ctrl-shift-]"),
	},
	BoxIDs:         []string{"box", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: BindingView("ctrl-}"),
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

func init() {
	parity.Register(ShiftedFixture)
	parity.Register(BindingCodeFixture)
	parity.Register(BindingCharFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// page is the fixtures' page: a flex-column body (the pin's flex
// default) with one focusable, tab-stop box whose :focus background is
// the focus probe (the keyorder tab page's shape, one box).
func page() string {
	return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
    html, body {
        margin: 0;
        padding: 0;
        background: #ffffff;
    }
    body {
        width: ` + f2s(windowWidth) + `px;
        height: ` + f2s(windowHeight) + `px;
        display: flex;
        flex-direction: column;
        font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
    }
    #box {
        box-sizing: border-box;
        width: ` + f2s(boxWidth) + `px;
        height: ` + f2s(boxHeight) + `px;
        background: #e2e8f0;
    }
    #box:focus {
        background: #22c55e;
        outline: none;
    }
</style>
</head>
<body data-id="root">
    <div id="box" data-id="box" tabindex="0"></div>
</body>
</html>
`
}

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// ShiftedView is the shifted fixture's view: a focusable box tracked
// by a window focus handle (the click focuses it; the keys then target
// its listener path) and the root's and box's click/keydown/keyup
// logging. The listeners see the keystroke the platform reports (Key
// folded to the shifted character with Shift kept, Code and Location
// filled — the parity replay's StepKey normalization produces it).
func ShiftedView() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handle lives outside the returned closure, keyed on the
	// window (the keyorder fixtures' note: the view function re-runs
	// every frame, and a Replay of the same fixture in the same
	// process opens a fresh window whose handles must be fresh too).
	var focus gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focus = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focus, "box")
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			// The page pins the reference's 1.618 line height (body font
			// 16px/1.618); the port's default is CSS normal since
			// browser-parity D1, so the golden-ratio opt-in keeps the
			// recorded text placement.
			LineHeight(gpui.PhiLineHeight()).
			OnClick(log.Click("root")).
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root"))
		box := gpui.Div().
			ID("box").
			DebugSelector("box").
			Size(gpui.Px(boxWidth), gpui.Px(boxHeight)).
			Bg(gpui.Rgb(boxBackground)).
			// The page pins outline: none on the focusable box; the
			// port has a default focus ring now (ticket 48), so the
			// view carries the pin's twin.
			OutlineNone().
			TrackFocus(focus).
			Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
				s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
				return s
			}).
			OnClick(log.Click("box")).
			OnKeyDown(log.KeyDown("box")).
			OnKeyUp(log.KeyUp("box"))
		return root.Child(box).IntoElement()
	}
}

// BindingView is the binding fixtures' view: the shifted fixture's
// shape plus the window's key dispatch order set to the browser order
// (once, before the first key dispatch — the listeners must run before
// the binding consumes the key, as in the keyorder binding-order
// fixture) and the given spelling bound to keyFiredAction, whose
// action listener counts and does nothing else (the transcript stays
// Chrome-shaped).
func BindingView(spelling string) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handle, the order flag and the binding live outside the
	// returned closure, keyed on the window (the keyorder fixtures'
	// note): the flag and the keymap entry are set per replay window,
	// before the first key dispatch.
	var focus gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focus = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focus, "box")
			if w.KeyDispatchOrder() != gpui.BindingsBeforeListeners {
				panic("keycode: the binding fixtures expect the default GPUI order before the first build")
			}
			w.SetKeyDispatchOrder(gpui.ListenersBeforeBindings)
			app.BindKeys(gpui.NewKeyBinding(spelling, keyFiredAction.Box(keyFiredPayload{}), nil))
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			// The page's 1.618 line-height pin, mirrored (D1: the port's
			// default is CSS normal; PhiLineHeight is the opt-in).
			LineHeight(gpui.PhiLineHeight()).
			OnClick(log.Click("root")).
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root")).
			OnAction(keyFiredAction, func(*keyFiredPayload, *gpui.Window, *gpui.App) { firedActions++ })
		box := gpui.Div().
			ID("box").
			DebugSelector("box").
			Size(gpui.Px(boxWidth), gpui.Px(boxHeight)).
			Bg(gpui.Rgb(boxBackground)).
			// The page pins outline: none on the focusable box; the
			// port has a default focus ring now (ticket 48), so the
			// view carries the pin's twin.
			OutlineNone().
			TrackFocus(focus).
			Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
				s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
				return s
			}).
			OnClick(log.Click("box")).
			OnKeyDown(log.KeyDown("box")).
			OnKeyUp(log.KeyUp("box"))
		return root.Child(box).IntoElement()
	}
}

// f2s renders a shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
