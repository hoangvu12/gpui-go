//go:build windows

// Package editkeys is ticket 26's fixture set (browser-core,
// .scratch/browser-core/issues/26-edit-actions-and-html-clipboard.md):
// the editing keys of an input, recorded in headless Chrome and
// replayed against the core's edit actions (gpui/edit_action.go).
//
// The Tier B transcript pins, in Chrome's order
// (core/editing/commands/editing_behavior.cc's key table, dispatched by
// editor_key_bindings.cc Editor::HandleEditingKeyboardEvent — the first
// arm of KeyboardEventManager::DefaultKeyboardEventHandler,
// keyboard_event_manager.cc:442-445):
//
//   - editkeys: a copy with NO selection is disabled (the keydown and
//     keyup fire, the command does nothing — EditorCommand::IsEnabled's
//     CanCopy); Ctrl+A fires select (the SelectAll command's selection
//     change, between the halves of the press); Ctrl+Insert fires copy
//     and Shift+Delete fires cut then beforeinput (deleteByCut, the cut
//     command's mutation preview).
//   - editkeys-paste: Shift+Insert fires paste then beforeinput
//     (insertFromPaste) — the paste command's event pair.
//
// The Ctrl+C/X/V letter bindings are not recorded: headless Chrome
// consumes them in the browser process before the renderer (the Edit
// menu accelerators — no keydown reaches the page at all, verified by
// the recording), so the recorder cannot drive them. The port still
// binds them (a real Chrome delivers them to the page: they are the
// same editing_behavior.cc:174-178 rows); the port's key-to-command
// mapping for all seven bindings is pinned by gpui's
// TestEditCommandKeyMapping instead.
//
// The paste's clipboard data is nondeterministic at replay (the real
// machine clipboard), so the record deliberately carries none of it:
// Chrome's beforeinput for a paste has data null (the DataTransfer
// rides the event, not the transcript), the input event is not
// instrumented, and the input hides its value — the port's hostless
// replay (whose clipboard read fails) fires the same event pair. The
// pasted text itself is pinned by internal/clipspec's real-clipboard
// paste test.
//
// The composed text itself is not pixel-compared (the port's field is
// a model, not an editor element — the imespec fixture's shape): the
// input hides its value and the view renders no text, so Tier C sees
// the flat boxes.
package editkeys

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers (the imespec fixture's conventions: the pin's
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

// fieldText is the input's initial value on both sides: the editor
// model starts with it, the page's input carries it as the value
// attribute. The click focuses the field with a collapsed caret (no
// selection), Ctrl+A selects it all.
const fieldText = "hello world"

// Fixture is the command fixture: a disabled copy, select-all, copy
// and cut through the Windows bindings the recorder can drive
// (Ctrl+Insert, Shift+Delete — editing_behavior.cc:168-170).
var Fixture = parity.Fixture{
	Name:   "editkeys",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Click(fieldClickX, fieldClickY),
		// Ctrl+Insert with no selection: the copy command is disabled
		// (CanCopy), so only the key pair lands.
		parity.Key("ctrl-insert"),
		parity.Key("ctrl-a"),
		parity.Key("ctrl-insert"),
		parity.Key("shift-delete"),
	},
	BoxIDs: []string{"field", "root"},
	EventKinds: []string{
		"mousedown", "mouseup",
		"keydown", "keyup",
		"select", "copy", "cut", "beforeinput",
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

// PasteFixture is the paste fixture: Shift+Insert fires the paste
// command's event pair (editing_behavior.cc:169) over whatever the
// machine's clipboard holds (the transcript carries no data — see the
// package comment).
var PasteFixture = parity.Fixture{
	Name:   "editkeys-paste",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Click(fieldClickX, fieldClickY),
		parity.Key("shift-insert"),
	},
	BoxIDs: []string{"field", "root"},
	EventKinds: []string{
		"mousedown", "mouseup",
		"keydown", "keyup",
		"select", "copy", "cut", "paste", "beforeinput",
	},
	ViewWithWindow: View(),
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

func init() {
	parity.Register(Fixture)
	parity.Register(PasteFixture)
}

// page is the fixture's HTML: a flex-column body over one text input
// carrying fieldText as its value. The input is styled to a plain
// 200x40 box whose value and caret are invisible — the fixture
// compares the event transcript, and the port's field is a model
// wrapped by a plain div (the imespec fixture's shape). The root
// carries data-events="keydown,keyup": key events bubble to it on both
// sides, while the mouse, clipboard and beforeinput kinds stay on the
// field.
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
	<input id="field" data-id="field" value="` + fieldText + `">
</body>
</html>
`
}

// fieldHandler is the fixture's input handler: the port's Editor model
// (the text mutations land in it through the core's edit actions) with
// the field's selection carried here, wrapped with ticket 26's
// edit-action callbacks, which append to the Tier B transcript — the
// app pattern for a handler that wants the events. The Editor's own
// selection members stay the pin's no-op shape, so the field's
// selection lives in this handler (an input's selection model).
type fieldHandler struct {
	*gpui.Editor
	log *parity.EventLog
	// sel is the field's selection in UTF-16 units (the collapsed
	// caret after the click; the whole text after the SelectAll
	// command).
	sel gpui.UTF16Range
}

// SelectedTextRange overrides the Editor's promoted member: the field's
// selection is this handler's state (the Editor's own member stays the
// pin's no-op contract for system-driven moves).
func (h *fieldHandler) SelectedTextRange(ignoreDisabledInput bool, w *gpui.Window, app *gpui.App) (gpui.UTF16Selection, bool) {
	return gpui.UTF16Selection{Range: h.sel}, true
}

// Copy fires the DOM copy event; the log entry is the transcript's
// copy record (the entry's default-prevented state rides the keydown
// dispatch's mark, like the activation click's coarseness).
func (h *fieldHandler) Copy(w *gpui.Window, app *gpui.App) bool {
	h.log.AppendEditEvent("copy", "field")
	return false
}

// Cut fires the DOM cut event.
func (h *fieldHandler) Cut(w *gpui.Window, app *gpui.App) bool {
	h.log.AppendEditEvent("cut", "field")
	return false
}

// Paste fires the DOM paste event carrying the clipboard item that was
// read for it (the entry's data is the item's text — the hostless
// replay reads nothing, so the entry is empty there; the record's
// beforeinput carries no data on either side, so the transcripts
// match).
func (h *fieldHandler) Paste(item *gpui.ClipboardItem, w *gpui.Window, app *gpui.App) bool {
	text := ""
	if item != nil {
		if t, ok := item.Text(); ok {
			text = t
		}
	}
	h.log.AppendEditEventWithData("paste", "field", text)
	return false
}

// BeforeInput fires the DOM beforeinput event: the entry carries the
// input type (deleteByCut for the cut command's mutation,
// insertFromPaste for a paste) and the event's data.
func (h *fieldHandler) BeforeInput(inputType, data string, w *gpui.Window, app *gpui.App) bool {
	h.log.AppendBeforeInput("field", inputType, data)
	return false
}

// Select fires the DOM select event (the selection change the SelectAll
// command made).
func (h *fieldHandler) Select(w *gpui.Window, app *gpui.App) {
	h.log.AppendEditEvent("select", "field")
}

// SelectAll selects the whole text (the SelectAll editing command's
// action): the field's selection becomes the whole text.
func (h *fieldHandler) SelectAll(w *gpui.Window, app *gpui.App) {
	h.sel = gpui.UTF16Range{Start: 0, End: utf16Len(fieldText)}
}

// utf16Len is the UTF-16 length of s (the ASCII fixture text's rune
// count).
func utf16Len(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// View returns the fixture's view builder: a focusable 200x40 field
// (the input's box) whose key and mouse listeners log with the focus
// field, and whose input handler registers through the paint-time seam
// (Window.HandleInput for the field's focus handle — the focused
// element's registration the frame completion installs, so the handler
// survives every redraw exactly like an app's).
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
				Editor: gpui.NewEditor(fieldText, len(fieldText), gpui.EditorOptions{}),
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
		field := gpui.Div().
			ID("field").
			DebugSelector("field").
			Size(gpui.Px(fieldWidth), gpui.Px(fieldHeight)).
			Bg(gpui.Rgb(fieldBackground)).
			TrackFocus(focus).
			// The page pins outline: none on the focused element; the port has a
			// default focus ring since ticket 48, so the view carries the pin's
			// twin.
			OutlineNone().
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("field")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("field")).
			OnKeyDown(log.KeyDown("field")).
			OnKeyUp(log.KeyUp("field"))
		return root.Child(field).IntoElement()
	}
}

// PlainView is the discrimination view: the same tree with a handler
// that does NOT implement the edit-action callbacks (the plain Editor
// — the base InputHandler contract, which still receives the text
// mutations). Replaying the record against it must fail Tier B with
// the missing select/copy/cut/beforeinput entries (the callbacks are
// the only transcript source for them).
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
			handler = gpui.NewEditor(fieldText, len(fieldText), gpui.EditorOptions{})
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
		field := gpui.Div().
			ID("field").
			DebugSelector("field").
			Size(gpui.Px(fieldWidth), gpui.Px(fieldHeight)).
			Bg(gpui.Rgb(fieldBackground)).
			TrackFocus(focus).
			// The page pins outline: none on the focused element; the port has a
			// default focus ring since ticket 48, so the view carries the pin's
			// twin.
			OutlineNone().
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
