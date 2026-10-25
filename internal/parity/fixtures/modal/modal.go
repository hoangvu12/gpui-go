//go:build windows

// Package modal is ticket 23's fixture set for Window.ShowModal (spec
// story 70), each recorded in headless Chrome and replayed against the
// gpui-go core:
//
//   - modal-focus: the dialog focusing steps with no autofocus — the
//     FIRST keyboard focusable element of the dialog (d1) takes focus
//     when the modal opens; Tab walks the dialog's stops only (the
//     outside is inert: the anchor never receives focus), and Tab past
//     the dialog's last stop passes out to the body (Chrome hands
//     focus to the browser UI at the scope's end, document.cc's
//     TakeFocus path); Escape closes the dialog and restores focus to
//     the invoker (the keyup's focus field proves the restore).
//   - modal-autofocus: the autofocus delegate wins over the earlier
//     first focusable element (d2, marked autofocus, is focused while
//     d1 comes first in tree order).
//   - modal-dialog-focus: a dialog with nothing focusable inside —
//     the focusing steps fall back to the dialog itself (the dialog
//     element is always focusable, html_dialog_element.h:114-116), so
//     the Escape keydown's focus field reads the dialog's id.
//   - modal-dismiss: closedby=any — a click on the page outside the
//     dialog (over the backdrop) requests the dialog's close through
//     its watcher (the cancel event), and the focus restore lands on
//     the invoker: the final Tab's keydown focus field proves it.
//   - modal-closedby-none: closedby=none — neither Escape nor an
//     outside click closes the dialog (the watcher is disabled,
//     SetCloseWatcherEnabledState); the settle leaves it open with its
//     backdrop, so Tier A/C pin the dialog over the scrim.
//
// The pages follow the popover fixtures' shape (a fixed dialog whose
// UA styling is reset back to a plain box, focusable buttons with
// outline: none — the port has the default focus ring of ticket 48, so
// the view's focusable boxes carry .OutlineNone()). The dialog opens
// from a script's d.showModal() — the declarative commandfor path is
// not needed for the behaviour under test. The pump moves between the
// clicks and the key steps deliver Chrome's queued toggle tasks (the
// recorder's key commands do not pump the renderer's task loop; the
// closewatcher fixtures' lesson).
package modal

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by every page and its gpui view (the popover
// fixtures' geometry).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	invokerW = 40.0
	invokerH = 20.0

	// The dialog: fixed at (60,80), 150x120, amber, with a 40% black
	// ::backdrop.
	dialogX = 60.0
	dialogY = 80.0
	dialogW = 150.0
	dialogH = 120.0

	// The dialog's buttons: d1 at (10,10) inside d, d2 at (60,10).
	buttonDX = 10.0
	buttonDY = 10.0
	buttonW  = 40.0
	buttonH  = 20.0

	// The outside focusable anchor at the top-right corner: while the
	// modal is open it is inert (Tab never reaches it).
	anchorX = 600.0
	anchorY = 0.0

	// The pump point: an empty corner of every page (no dialog, no
	// button, no listener) the Move steps travel to — a move carries no
	// transcript entries, it only turns the recorder's task loop (the
	// closewatcher fixtures' lesson).
	pumpX = 400.0
	pumpY = 200.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	invokerBlue    = 0x2563eb
	dialogAmber    = 0xfde68a
	anchorGray     = 0x94a3b8
)

// tolerance is the flat-color pixel gate: text-free solid boxes, so the
// correct view is pixel-exact (the popover fixtures' measured numbers).
// Provisional (research 19's calibration has not run). See
// docs/browser-parity-harness.md.
var tolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.02,
	MaxROIMeanChannelDiff: 0.1,
	MaxHotPixelFraction:   0.002,
}

// escapeSequence is the closing tail of the fixtures that end closed:
// the Escape half-press, the pump move that delivers Chrome's queued
// toggle task, and the release whose keyup entries read the restored
// focus (the port drains the toggle at the keydown dispatch's end, so
// both sides place the toggle entry between the two halves).
func escapeSequence() []parity.Step {
	return []parity.Step{
		parity.KeyDown("escape"),
		parity.Move(pumpX, pumpY),
		parity.KeyUp("escape"),
	}
}

// FocusFixture is the focusing-steps fixture: the first focusable
// element (d1), the modal Tab scope, the pass-out blur and the restore.
var FocusFixture = parity.Fixture{
	Name:   "modal-focus",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(false, ""),
	Steps: append([]parity.Step{
		// Open the modal from the invoker: the click's mouse focus lands
		// on b1 (the previously focused element), and the modal's
		// focusing steps move focus to d1 at the modal's first frame.
		parity.Click(20, 10),
		// The pump move delivers the modal's open toggle.
		parity.Move(pumpX, pumpY),
		// Tab: d1 -> d2 (the keyup's focus field proves the move).
		parity.Key("tab"),
		// Tab past the dialog's last stop: the document scope's end —
		// the outside is inert, so nothing is focusable after d2 and
		// focus passes out (the body).
		parity.Key("tab"),
	}, escapeSequence()...),
	// The dialog is closed at the settle (display: none in Chrome, not
	// rendered in the port), so only the always-rendered boxes compare;
	// the focus fields of the key entries carry the focusing steps, the
	// Tab scope and the restore.
	ViewWithWindow: focusView,
	BoxIDs:         []string{"b1", "anchor"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup", "toggle"},
	Tolerance:      tolerance,
}

// AutofocusFixture is the autofocus fixture: d2 (autofocus) wins over
// d1 (first in tree order), and Escape closes with the restore.
var AutofocusFixture = parity.Fixture{
	Name:   "modal-autofocus",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   autofocusPage(),
	Steps: append([]parity.Step{
		parity.Click(20, 10),
		parity.Move(pumpX, pumpY),
	}, escapeSequence()...),
	// The Escape keydown's focus field reads d2 (the autofocus
	// delegate), not d1 — that entry is the fixture's assertion.
	ViewWithWindow: autofocusView,
	BoxIDs:         []string{"b1", "anchor"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup", "toggle"},
	Tolerance:      tolerance,
}

// DialogFocusFixture is the dialog-itself fallback: nothing inside the
// dialog is focusable, so the focusing steps focus the dialog element
// (the keydown's focus field reads "d").
var DialogFocusFixture = parity.Fixture{
	Name:   "modal-dialog-focus",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   dialogOnlyPage,
	Steps: append([]parity.Step{
		parity.Click(20, 10),
		parity.Move(pumpX, pumpY),
	}, escapeSequence()...),
	ViewWithWindow: dialogOnlyView,
	BoxIDs:         []string{"b1", "anchor"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup", "toggle"},
	Tolerance:      tolerance,
}

// DismissFixture is the closedby=any fixture: a click on the page
// outside the dialog (the backdrop region) light-dismisses the dialog
// through its watcher's requestClose, and the restored focus is the
// invoker (the last Tab's keydown focus field).
var DismissFixture = parity.Fixture{
	Name:   "modal-dismiss",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   dismissPage(),
	Steps: []parity.Step{
		parity.Click(20, 10),
		parity.Move(pumpX, pumpY),
		// A click in the backdrop region — the open page under the
		// scrim, outside the dialog box: the press and the release both
		// fall on the inert document (no transcript entries on either
		// side), and the light dismiss requests the dialog's close.
		parity.Click(400, 200),
		// The pump move delivers the closed toggle.
		parity.Move(pumpX, pumpY),
		// The restore proof: the Tab's keydown listeners see the
		// restored invoker focused, and its default action moves focus
		// to the outside anchor — interactive again now the dialog
		// closed (the keyup's focus field).
		parity.Key("tab"),
	},
	ViewWithWindow: dismissView,
	BoxIDs:         []string{"b1", "anchor"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup", "toggle"},
	Tolerance:      tolerance,
}

// ClosedByNoneFixture is the closedby=none fixture: neither Escape nor
// an outside click closes the dialog; the settle leaves it open with
// its backdrop, so Tier A compares the dialog box over the scrim and
// Tier C the pixels.
var ClosedByNoneFixture = parity.Fixture{
	Name:   "modal-closedby-none",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   closedByNonePage(),
	Steps: []parity.Step{
		parity.Click(20, 10),
		parity.Move(pumpX, pumpY),
		// Escape: the dialog's watcher is disabled (closedby=none), so
		// nothing closes and the keydown entries read the dialog's
		// focus. A full press is safe: no toggle task is ever queued.
		parity.Key("escape"),
		// An outside click: the light dismiss skips a closedby=none
		// dialog, so nothing happens (and the press's mouse-focus
		// default blurs to the body — the DOM's HandleMouseFocus with
		// no focusable press target).
		parity.Click(400, 200),
		// The pump move lets Chrome deliver anything the steps queued
		// before the settle (the open toggle already landed).
		parity.Move(pumpX, pumpY),
	},
	ViewWithWindow: closedByNoneView,
	BoxIDs:         []string{"b1", "d", "anchor"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup", "toggle"},
	Tolerance:      tolerance,
}

func init() {
	parity.Register(FocusFixture)
	parity.Register(AutofocusFixture)
	parity.Register(DialogFocusFixture)
	parity.Register(DismissFixture)
	parity.Register(ClosedByNoneFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// page is the shared Chrome twin: an invoker that opens the dialog, an
// outside focusable anchor, and the dialog with two focusable buttons.
// withAutofocus marks d2 as the dialog's autofocus delegate; closedBy is
// the dialog element's closedby attribute ("" for the auto default).
func page(withAutofocus bool, closedBy string) string {
	autofocus := ""
	if withAutofocus {
		autofocus = " autofocus"
	}
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
		width: 640px;
		height: 400px;
		position: relative;
	}
	button {
		appearance: none;
		border: 0;
		margin: 0;
		padding: 0;
		display: block;
		position: absolute;
		width: 40px;
		height: 20px;
		background: #2563eb;
		outline: none;
	}
	#b1 {
		left: 0px;
		top: 0px;
	}
	#anchor {
		left: 600px;
		top: 0px;
		background: #94a3b8;
	}
	/* The UA stylesheet centers dialogs (position: absolute; inset: 0;
	   margin: auto; border; padding; color: canvastext), so the page
	   resets all of it back to a plain fixed box at an explicit
	   coordinate. The ::backdrop is the UA dialog's rgba(0,0,0,0.1)
	   scrim (html.css:2007-2010) — the page styles its own 40% black
	   one, the port's TopLayer backdrop twin. */
	dialog {
		position: fixed;
		inset: auto;
		margin: 0;
		border: 0;
		padding: 0;
		overflow: visible;
		color: #000000;
	}
	#d {
		left: 60px;
		top: 80px;
		width: 150px;
		height: 120px;
		background: #fde68a;
	}
	#d::backdrop {
		background: rgba(0, 0, 0, 0.4);
	}
	#d1 {
		left: 10px;
		top: 10px;
	}
	#d2 {
		left: 60px;
		top: 10px;
	}
</style>
</head>
<body data-id="root" data-events="keydown,keyup">
	<button id="b1" data-id="b1"></button>
	<button id="anchor" data-id="anchor"></button>
	<!-- The dialog binds no mouse listeners (data-events): Chrome's
	     ::backdrop pseudo propagates its pointer events onto the dialog
	     element, where the port's plain backdrop div (a sibling of the
	     content, no hitbox) delivers nothing — the narrowing keeps the
	     transcripts equal while the light dismiss itself (the toggle
	     entries and the restored focus) carries the behaviour. -->
	<dialog id="d" data-id="d" data-events="keydown,keyup,toggle"` + closedBy + `>
		<button id="d1" data-id="d1" tabindex="0"></button>
		<button id="d2" data-id="d2" tabindex="0"` + autofocus + `></button>
	</dialog>
	<script>
		document.getElementById("b1").addEventListener("click", () => {
			document.getElementById("d").showModal();
		});
	</script>
</body>
</html>
`
}

// autofocusPage is the autofocus fixture's page: d2 carries autofocus.
func autofocusPage() string { return page(true, "") }

// dismissPage is the closedby=any fixture's page.
func dismissPage() string { return page(false, ` closedby="any"`) }

// closedByNonePage is the closedby=none fixture's page.
func closedByNonePage() string { return page(false, ` closedby="none"`) }

// dialogOnlyPage is the dialog-itself fallback's page: the dialog has
// nothing focusable inside (no buttons, no autofocus).
const dialogOnlyPage = `<!doctype html>
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
		width: 640px;
		height: 400px;
		position: relative;
	}
	button {
		appearance: none;
		border: 0;
		margin: 0;
		padding: 0;
		display: block;
		position: absolute;
		width: 40px;
		height: 20px;
		background: #2563eb;
		outline: none;
	}
	#b1 {
		left: 0px;
		top: 0px;
	}
	#anchor {
		left: 600px;
		top: 0px;
		background: #94a3b8;
	}
	dialog {
		position: fixed;
		inset: auto;
		margin: 0;
		border: 0;
		padding: 0;
		overflow: visible;
		color: #000000;
	}
	#d {
		left: 60px;
		top: 80px;
		width: 150px;
		height: 120px;
		background: #fde68a;
	}
	#d::backdrop {
		background: rgba(0, 0, 0, 0.4);
	}
</style>
</head>
<body data-id="root" data-events="keydown,keyup">
	<button id="b1" data-id="b1"></button>
	<button id="anchor" data-id="anchor"></button>
	<dialog id="d" data-id="d" data-events="keydown,keyup,toggle"></dialog>
	<script>
		document.getElementById("b1").addEventListener("click", () => {
			document.getElementById("d").showModal();
		});
	</script>
</body>
</html>
`

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// modalState is the fixture's per-window mutable state (the harness's
// discipline: the view function re-runs every frame, the handles must
// not): the dialog's overlay handle and the focus handles.
type modalState struct {
	dialog                   gpui.OverlayHandle
	focusB1, focusAnchor     gpui.FocusHandle
	focusD, focusD1, focusD2 gpui.FocusHandle
	// noModal renders the dialog as a plain popover-family overlay
	// opened with Show (the deliberately-wrong view).
	noModal bool
}

// modalStates keeps one state per replayed window: Replays can run the
// same fixture twice in one process with fresh windows.
var modalStates = map[*gpui.Window]*modalState{}

// modalStateOf returns (creating when absent) the window's state with
// the dialog's toggle listener wired to the log.
func modalStateOf(log *parity.EventLog, w *gpui.Window) *modalState {
	if state, ok := modalStates[w]; ok {
		return state
	}
	state := &modalState{
		dialog:      gpui.NewOverlayHandle(w),
		focusB1:     gpui.NewFocusHandle(w).TabStop(true),
		focusAnchor: gpui.NewFocusHandle(w).TabStop(true),
		// The dialog element's own slot: focusable but NOT a tab stop
		// (the DOM dialog without tabindex — SupportsFocus without
		// IsKeyboardFocusableSlow, html_dialog_element.h:114-119).
		focusD:  gpui.NewFocusHandle(w),
		focusD1: gpui.NewFocusHandle(w).TabStop(true),
		focusD2: gpui.NewFocusHandle(w).TabStop(true),
	}
	state.dialog.OnToggle(log.Toggle("d"))
	log.TrackFocus(state.focusB1, "b1")
	log.TrackFocus(state.focusAnchor, "anchor")
	log.TrackFocus(state.focusD, "d")
	log.TrackFocus(state.focusD1, "d1")
	log.TrackFocus(state.focusD2, "d2")
	modalStates[w] = state
	return state
}

// focusView is the gpui-go twin of page: the invoker (click ->
// ShowModal), the outside anchor, and the dialog in the top layer with
// two focusable buttons. d2 carries no autofocus here (the first
// focusable is d1).
func focusView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return modalRoot(log, w, modalStateOf(log, w), variantButtons, gpui.ClosedByAuto)
}

// autofocusView is the autofocus page's twin: d2 carries AutoFocus.
func autofocusView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return modalRoot(log, w, modalStateOf(log, w), variantAutofocus, gpui.ClosedByAuto)
}

// dialogOnlyView is the dialog-itself page's twin: the dialog box has
// no buttons, and the entry's DialogFocus is the focusing steps' (only)
// target.
func dialogOnlyView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return modalRoot(log, w, modalStateOf(log, w), variantDialogOnly, gpui.ClosedByAuto)
}

// dismissView is the closedby=any fixture's twin.
func dismissView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return modalRoot(log, w, modalStateOf(log, w), variantButtons, gpui.ClosedByAny)
}

// closedByNoneView is the closedby=none page's twin.
func closedByNoneView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return modalRoot(log, w, modalStateOf(log, w), variantButtons, gpui.ClosedByNone)
}

// DismissWithoutModal is the deliberately-wrong view for the
// discrimination test: the pre-ticket modal — a plain popover-family
// overlay the app opens with Show (no Window.ShowModal, the spec's
// problem statement: "Modal dialogs are Base focus traps plus a
// backdrop hitbox"). The core never runs the focusing steps (the first
// Tab's keydown focus field still reads b1), the outside is never
// inert (Tab leaves the dialog onto the anchor), and the entry's kind
// is a manual overlay whose Escape does nothing — no toggle closed
// entry ever lands.
func DismissWithoutModal(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := modalStateOf(log, w)
	state.noModal = true
	return modalRoot(log, w, state, variantButtons, gpui.ClosedByAny)
}

// modalVariant selects the dialog's content shape: the two buttons
// (with or without d2's autofocus) or nothing focusable inside (the
// dialog-itself fallback).
type modalVariant uint8

const (
	variantButtons modalVariant = iota
	variantAutofocus
	variantDialogOnly
)

// modalRoot builds the fixture's tree: the root with the invoker and
// the anchor, and the dialog in the top layer while its handle is
// open. The buttons render in the page's tree order (d1 then d2) so
// the focusing steps' tree-order walk agrees on both sides.
func modalRoot(log *parity.EventLog, w *gpui.Window, state *modalState, variant modalVariant, closedBy gpui.ClosedBy) gpui.AnyElement {
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Bg(gpui.Rgb(pageBackground)).
		OnKeyDown(log.KeyDown("root")).
		OnKeyUp(log.KeyUp("root")).
		Child(invokerBox(log, state)).
		Child(anchorBox(log, state))
	if state.dialog.IsOpen() {
		layer := gpui.TopLayer(state.dialog).
			DialogFocus(state.focusD).
			ClosedBy(closedBy).
			Backdrop(gpui.Div().SizeFull().Bg(gpui.RgbaToHsla(0x00000066)))
		if state.noModal {
			layer = layer.Kind(gpui.OverlayPopoverManual)
		}
		dialog := dialogBox(state, log)
		if variant != variantDialogOnly {
			dialog.Child(buttonBox(state, log, "d1", buttonDX, buttonDY, state.focusD1, false))
			dialog.Child(buttonBox(state, log, "d2", buttonDX+50, buttonDY, state.focusD2, variant == variantAutofocus))
		}
		root.Child(layer.Child(dialog))
	}
	return root.IntoElement()
}

// invokerBox builds the invoker: the click logs and opens the modal
// (the page's showModal script).
func invokerBox(log *parity.EventLog, state *modalState) *gpui.DivElement {
	box := gpui.Div().ID("b1").DebugSelector("b1").
		Absolute().Top(gpui.Px(0)).Left(gpui.Px(0)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(invokerBlue)).
		TrackFocus(state.focusB1).OutlineNone().
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("b1")).
		OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("b1")).
		OnKeyDown(log.KeyDown("b1")).
		OnKeyUp(log.KeyUp("b1"))
	if state.noModal {
		box.OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			log.Click("b1")(e, w, app)
			state.dialog.Show()
		})
	} else {
		box.OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			log.Click("b1")(e, w, app)
			w.ShowModal(state.dialog)
		})
	}
	return box
}

// anchorBox builds the outside focusable anchor (the key listeners the
// page's data-id instrumentation binds: the keyup after the restore's
// Tab reads its focus field).
func anchorBox(log *parity.EventLog, state *modalState) *gpui.DivElement {
	return gpui.Div().ID("anchor").DebugSelector("anchor").
		Absolute().Top(gpui.Px(anchorY)).Left(gpui.Px(anchorX)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(anchorGray)).
		TrackFocus(state.focusAnchor).OutlineNone().
		OnKeyDown(log.KeyDown("anchor")).
		OnKeyUp(log.KeyUp("anchor"))
}

// dialogBox builds the dialog surface: the dialog element's stand-in,
// tracking the entry's dialog focus slot (focusable, not a tab stop),
// with the key listeners the page's data-id instrumentation binds.
func dialogBox(state *modalState, log *parity.EventLog) *gpui.DivElement {
	return gpui.Div().ID("d").DebugSelector("d").
		Absolute().Top(gpui.Px(dialogY)).Left(gpui.Px(dialogX)).
		W(gpui.Px(dialogW)).H(gpui.Px(dialogH)).
		Bg(gpui.Rgb(dialogAmber)).
		Occlude().
		TrackFocus(state.focusD).
		OnKeyDown(log.KeyDown("d")).
		OnKeyUp(log.KeyUp("d"))
}

// buttonBox builds one focusable dialog button, optionally carrying
// the autofocus marking.
func buttonBox(state *modalState, log *parity.EventLog, id string, x, y float32, focus gpui.FocusHandle, autofocus bool) *gpui.DivElement {
	box := gpui.Div().ID(id).DebugSelector(id).
		Absolute().Top(gpui.Px(buttonDY)).Left(gpui.Px(x)).
		W(gpui.Px(buttonW)).H(gpui.Px(buttonH)).
		Bg(gpui.Rgb(invokerBlue)).
		TrackFocus(focus).OutlineNone().
		OnKeyDown(log.KeyDown(id)).
		OnKeyUp(log.KeyUp(id))
	if autofocus {
		box = box.AutoFocus()
	}
	return box
}
