//go:build windows

// Package imecompose is ticket 18's fixture (browser-core,
// .scratch/browser-core/issues/18-ime-composition-events.md): a
// scripted IME composition recorded in headless Chrome (CDP
// Input.imeSetComposition / Input.insertText, which drive the page's
// real IME adapter — content/browser/devtools/protocol/
// input_handler.cc:1316 ImeSetComposition → ImeAdapter::SetComposition,
// :1290 InsertText → ImeCommitText) and replayed against the ported
// composition driver's simulated seam (gpui.Window.
// SimulateIMEComposition over the WM_IME_COMPOSITION routing).
//
// The Tier B transcript pins, in Chrome's order
// (core/editing/ime/input_method_controller.cc, verified by the
// record):
//
//   - a composition session: compositionstart (data: the selection it
//     replaces — "" here) then compositionupdate (the string) per
//     update;
//   - keydowns and keyups DURING the composition are delivered, flagged
//     isComposing=true (keyboard_event.cc:122 HasCurrentComposition) —
//     not dropped, the spec's inferred risk;
//   - the commit (Input.insertText): compositionupdate (the committed
//     text) then compositionend (the committed text), and the session
//     is over — the keys after it are flagged isComposing=false;
//   - a second session cancelled by an empty composition string:
//     compositionupdate("") then compositionend("").
//
// The composed text itself is not pixel-compared: the page's input
// hides its value (color/caret transparent) and the view renders no
// text, because the port has no editor element yet (the editor model
// is gpui's Editor; the committed text is observed through the
// transcript's composition data and the model's own tests). The
// field's flat boxes still compare (Tier A) and the flat pixels (Tier
// C).
package imecompose

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers (the keyorder fixtures' conventions: the pin's
// flex default and 1.618 line height pinned on both sides; the field
// is a 200x40 box at the top-left).
const (
	windowWidth  = 480.0
	windowHeight = 360.0

	fieldWidth  = 200.0
	fieldHeight = 40.0

	fieldClickX = 100.0
	fieldClickY = 20.0

	pageBackground  = 0xffffff
	fieldBackground = 0xe2e8f0
)

// Fixture is ticket 18's composition fixture: click the field (focus),
// compose "n" → "ni", press Enter mid-composition, commit "nihao",
// press "a" after the commit, then compose "x" and cancel it with an
// empty composition. The keydown/keyup entries carry isComposing
// (true during the session, false after the commit); the composition
// entries carry the event data.
var Fixture = parity.Fixture{
	Name:   "imecompose",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Click(fieldClickX, fieldClickY),
		parity.ImeSet("n", 1, 1),
		parity.ImeSet("ni", 2, 2),
		parity.Key("enter"),
		parity.ImeCommit("nihao"),
		parity.Key("a"),
		parity.ImeSet("x", 1, 1),
		parity.ImeSet("", 0, 0),
	},
	BoxIDs: []string{"field", "root"},
	EventKinds: []string{
		"mousedown", "mouseup",
		"keydown", "keyup",
		"compositionstart", "compositionupdate", "compositionend",
	},
	ViewWithWindow: View(),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). The
		// pages are flat colors (the input hides its text and caret),
		// like the keyorder fixtures: measured correct-view statistics
		// 0.0 mean / 0.0 hot.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

func init() {
	parity.Register(Fixture)
}

// page is the fixture's HTML: a flex-column body over one text input
// (the editable element the IME composition targets; a plain div
// would not receive composition events). The input is styled to a
// plain 200x40 box whose value and caret are invisible — the fixture
// compares the event transcript, and the port has no editor element
// to render the composed text yet (see the package comment). The root
// carries data-events="keydown,keyup": key events bubble to it on
// both sides, but the mouse and composition kinds stay on the field.
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
	#field {
		appearance: none;
		display: block;
		box-sizing: border-box;
		width: ` + f2s(fieldWidth) + `px;
		height: ` + f2s(fieldHeight) + `px;
		padding: 0;
		border: none;
		margin: 0;
		background: #e2e8f0;
		color: transparent;
		caret-color: transparent;
		outline: none;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
</style>
</head>
<body data-id="root" data-events="keydown,keyup">
	<input id="field" data-id="field">
</body>
</html>
`
}

// fieldHandler is the fixture's input handler: the port's Editor model
// (the IME text mutations land in it through the composition driver)
// wrapped with ticket 18's composition callbacks, which append to the
// Tier B transcript — the app pattern for a handler that wants the
// events.
type fieldHandler struct {
	*gpui.Editor
	log *parity.EventLog
}

func (h *fieldHandler) CompositionStart(data string, w *gpui.Window, app *gpui.App) {
	h.log.AppendComposition("compositionstart", "field", data)
}

func (h *fieldHandler) CompositionUpdate(data string, w *gpui.Window, app *gpui.App) {
	h.log.AppendComposition("compositionupdate", "field", data)
}

func (h *fieldHandler) CompositionEnd(data string, w *gpui.Window, app *gpui.App) {
	h.log.AppendComposition("compositionend", "field", data)
}

// View returns the fixture's view builder: a focusable 200x40 field
// (the input's box) whose key and mouse listeners log with the focus
// and isComposing fields, and whose input handler registers through
// the paint-time seam (Window.HandleInput for the field's focus
// handle — the focused element's registration the frame completion
// installs, so the handler survives every redraw exactly like an
// app's).
func View() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handle and handler live in the closure, keyed on the window
	// (the keyorder fixtures' note): the builder re-runs every frame,
	// and the editor's model state must persist. A Replay of the same
	// fixture in a new window builds a fresh pair.
	var focus gpui.FocusHandle
	var handler *fieldHandler
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focus = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focus, "field")
			handler = &fieldHandler{
				Editor: gpui.NewEditor("", 0, gpui.EditorOptions{}),
				log:    log,
			}
		}
		w.HandleInput(focus, handler)
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
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root"))
		field := gpui.Div().Flex().
			ID("field").
			DebugSelector("field").
			Size(gpui.Px(fieldWidth), gpui.Px(fieldHeight)).
			Bg(gpui.Rgb(fieldBackground)).
			// The page pins outline: none on the editable; the
			// port has a default focus ring now (ticket 48), so the
			// view carries the pin's twin.
			OutlineNone().
			TrackFocus(focus).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("field")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("field")).
			OnKeyDown(log.KeyDown("field")).
			OnKeyUp(log.KeyUp("field"))
		return root.Child(field).IntoElement()
	}
}

// PlainView is the discrimination view: the same tree with a handler
// that does NOT implement the composition callbacks (the plain Editor
// — the base InputHandler contract, which still receives the text
// mutations). Replaying the record against it must fail Tier B with
// the missing composition entries (the callbacks are the only
// transcript source for them).
func PlainView() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handle and plain handler live in the closure, keyed on the
	// window (View's note).
	var focus gpui.FocusHandle
	var handler *gpui.Editor
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focus = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focus, "field")
			handler = gpui.NewEditor("", 0, gpui.EditorOptions{})
		}
		w.HandleInput(focus, handler)
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
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root"))
		field := gpui.Div().Flex().
			ID("field").
			DebugSelector("field").
			Size(gpui.Px(fieldWidth), gpui.Px(fieldHeight)).
			Bg(gpui.Rgb(fieldBackground)).
			// The page pins outline: none on the editable; the
			// port has a default focus ring now (ticket 48), so the
			// view carries the pin's twin.
			OutlineNone().
			TrackFocus(focus).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("field")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("field")).
			OnKeyDown(log.KeyDown("field")).
			OnKeyUp(log.KeyUp("field"))
		return root.Child(field).IntoElement()
	}
}

// f2s renders a shared float constant for the page's CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
