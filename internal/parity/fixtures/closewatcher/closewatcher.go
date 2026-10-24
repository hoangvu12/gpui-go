//go:build windows

// Package closewatcher is ticket 22's fixture set for close watchers
// and the Escape default action (spec story 69), each recorded in
// headless Chrome and replayed against the gpui-go core:
//
//   - closewatcher-escape: nested popovers opened through their
//     invokers, then Escape twice — the first closes the newest
//     popover while focus sits INSIDE the parent (not inside the
//     newest overlay) and the second closes the last one with focus
//     still outside it, the "regardless of where focus is" rule. The
//     keydown entries carry the focus field, pinning
//     listener-then-default order; the toggle entries pin the close
//     itself.
//   - closewatcher-hint: a tooltip-like popover=hint opened from an
//     invoker, closed by one Escape — hint popovers create close
//     watchers exactly like autos (html_element.cc:2146-2157).
//   - closewatcher-prevented: a keydown listener that calls
//     preventDefault — the default action, and with it the close
//     watcher signal, never runs (the DOM's listener-then-default
//     rule, ticket 09's machinery); both popovers stay open.
//
// The pages follow the popover fixtures' shape (the popovers are
// body-level siblings, the nesting is the invoker link, the popover UA
// stylesheet is reset back to plain fixed boxes) with two additions:
// the invokers are focusable buttons (tabindex, outline: none — the
// port has no default focus ring yet) so the keydown entries' focus
// field agrees, and the anchor is a root-level focusable div. No
// keyup listeners are bound: Chrome
// restores focus when the LAST popover closes (HidePopoverInternal's
// kFocusPreviousElement), which the port does not do yet (ticket 21's
// recorded limitation), and a keyup listener would record the
// disagreement — the keydown entries, which see the pre-default focus,
// agree on both sides.
package closewatcher

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

	parentX = 20.0
	parentY = 60.0
	parentW = 150.0
	parentH = 120.0

	childX = 200.0
	childY = 60.0
	childW = 100.0
	childH = 80.0

	// The anchor: a root-level focusable box at the top-right corner,
	// outside every popover — the Tab default moves focus onto it, so
	// the second Escape runs with focus outside all overlays.
	anchorX = 600.0
	anchorY = 0.0

	// The hint fixtures' geometry: one invoker and one hint popover.
	hintX = 20.0
	hintY = 60.0
	hintW = 120.0
	hintH = 60.0

	// The pump point: an empty corner of every page (no popover, no
	// invoker, no listener) the Move steps travel to — a move carries
	// no transcript entries, it only turns the recorder's task loop
	// (the package comment).
	pumpX = 400.0
	pumpY = 200.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	invokerBlue    = 0x2563eb
	parentAmber    = 0xfde68a
	childGreen     = 0x86efac
	anchorGray     = 0x94a3b8
	hintColor      = 0xfde68a
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

// EscapeFixture is the nested-popover Escape fixture: the newest
// popover closes on the first Escape with focus inside the parent, and
// the last one on the second Escape with focus still outside it. The
// Move steps are task-loop pumps (the package comment).
var EscapeFixture = parity.Fixture{
	Name:   "closewatcher-escape",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		// Open the parent, then the child (b2 sits inside p1 at
		// (30,70)-(70,90); its centre is (50,80)).
		parity.Click(20, 10),
		parity.Click(50, 80),
		// The pump move (no listeners bound): Chrome's task loop turns
		// and delivers the popovers' pending toggle events; the port's
		// end-of-dispatch drain already did.
		parity.Move(pumpX, pumpY),
		// Escape: the child (the newest watcher) closes; focus is on
		// b2, inside p1 and NOT inside p2.
		parity.Key("escape"),
		// The pump move between the two escapes delivers the child's
		// closed toggle before the second press on the Chrome side.
		parity.Move(pumpX, pumpY),
		// Escape: the parent closes — the last watcher.
		parity.Key("escape"),
	},
	ViewWithWindow: escapeView,
	// The popovers are closed at the settle (display: none in Chrome,
	// not rendered in the port), so only the root-level boxes compare;
	// the toggle transcript and the bare-page pixels carry the rest.
	BoxIDs:     []string{"b1", "anchor"},
	EventKinds: []string{"mousedown", "mouseup", "click", "keydown", "toggle"},
	Tolerance:  tolerance,
}

// HintFixture is the hint fixture: a tooltip-like popover=hint closes
// with one Escape. The pump move stands between the opening click and
// the Escape (the package comment).
var HintFixture = parity.Fixture{
	Name:   "closewatcher-hint",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   hintPage,
	Steps: []parity.Step{
		parity.Click(20, 10),
		// The pump move (the package comment).
		parity.Move(pumpX, pumpY),
		parity.Key("escape"),
	},
	ViewWithWindow: hintView,
	BoxIDs:         []string{"b3"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "toggle"},
	Tolerance:      tolerance,
}

// PreventedFixture is the listener-gate fixture: a keydown listener
// that calls preventDefault keeps the default action from running, so
// Escape closes nothing and both popovers stay open at the settle. The
// pump move stands between the opening click and the Escape.
var PreventedFixture = parity.Fixture{
	Name:   "closewatcher-prevented",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   preventedPage,
	Steps: []parity.Step{
		parity.Click(20, 10),
		parity.Click(50, 80),
		// The pump move (the package comment).
		parity.Move(pumpX, pumpY),
		// The document's capturing keydown listener preventDefaults
		// every key: the close watcher never signals.
		parity.Key("escape"),
	},
	ViewWithWindow: preventedView,
	BoxIDs:         []string{"b1", "b2", "p1", "p2"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "toggle"},
	Tolerance:      tolerance,
}

func init() {
	parity.Register(EscapeFixture)
	parity.Register(HintFixture)
	parity.Register(PreventedFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// page is the escape fixture's Chrome twin: two popover divs fixed in
// the viewport, two popovertarget invokers (b2 nested inside p1's
// content — the invoker link that makes p1 the popover ancestor of
// p2), and the root-level focusable anchor.
const page = `<!doctype html>
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
	/* The UA stylesheet centers popovers (position: fixed; inset: 0;
	   margin: auto; width/height: fit-content; border; padding), so the
	   pages reset all of it back to plain fixed boxes at explicit
	   coordinates (html.css:1966-1977). Popovers without autofocus keep
	   focus where it was (SetPopoverFocusOnShow's "focus should remain
	   on the currently active element", html_element.cc:2851-2855). */
	.popover {
		position: fixed;
		inset: auto;
		margin: 0;
		border: 0;
		padding: 0;
		overflow: visible;
	}
	#p1 {
		left: 20px;
		top: 60px;
		width: 150px;
		height: 120px;
		background: #fde68a;
	}
	#b2 {
		left: 10px;
		top: 10px;
	}
	#p2 {
		left: 200px;
		top: 60px;
		width: 100px;
		height: 80px;
		background: #86efac;
	}
	#anchor {
		position: absolute;
		left: 600px;
		top: 0px;
		width: 40px;
		height: 20px;
		background: #94a3b8;
		outline: none;
	}
</style>
</head>
<body data-id="root" data-events="keydown">
	<button id="b1" popovertarget="p1" data-id="b1"></button>
	<div id="p1" class="popover" popover data-id="p1">
		<button id="b2" popovertarget="p2" data-id="b2"></button>
	</div>
	<div id="p2" class="popover" popover data-id="p2"></div>
	<div id="anchor" data-id="anchor" tabindex="0"></div>
</body>
</html>
`

// preventedPage is the prevented fixture's page: the shared page plus
// a capturing document keydown listener that calls preventDefault on
// every key (the keyorder prevented fixtures' pattern — the listener
// is bound on the document, not a [data-id] element, so the
// transcript's entries come from the instrumented element listeners
// and report the final default-prevented state).
const preventedPage = `<!doctype html>
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
	.popover {
		position: fixed;
		inset: auto;
		margin: 0;
		border: 0;
		padding: 0;
		overflow: visible;
	}
	#p1 {
		left: 20px;
		top: 60px;
		width: 150px;
		height: 120px;
		background: #fde68a;
	}
	#b2 {
		left: 10px;
		top: 10px;
	}
	#p2 {
		left: 200px;
		top: 60px;
		width: 100px;
		height: 80px;
		background: #86efac;
	}
	#anchor {
		position: absolute;
		left: 600px;
		top: 0px;
		width: 40px;
		height: 20px;
		background: #94a3b8;
		outline: none;
	}
</style>
</head>
<body data-id="root" data-events="keydown">
	<button id="b1" popovertarget="p1" data-id="b1"></button>
	<div id="p1" class="popover" popover data-id="p1">
		<button id="b2" popovertarget="p2" data-id="b2"></button>
	</div>
	<div id="p2" class="popover" popover data-id="p2"></div>
	<div id="anchor" data-id="anchor" tabindex="0"></div>
	<script>
		document.addEventListener('keydown', e => e.preventDefault(), true);
	</script>
</body>
</html>
`

// hintPage is the hint fixture's page: one invoker and one
// popover=hint (the tooltip family — light-dismissable, and its show
// creates a close watcher like an auto's).
const hintPage = `<!doctype html>
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
	#b3 {
		left: 0px;
		top: 0px;
	}
	#hint {
		position: fixed;
		inset: auto;
		margin: 0;
		border: 0;
		padding: 0;
		overflow: visible;
		left: 20px;
		top: 60px;
		width: 120px;
		height: 60px;
		background: #fde68a;
	}
</style>
</head>
<body data-id="root" data-events="keydown">
	<button id="b3" popovertarget="hint" data-id="b3"></button>
	<div id="hint" popover="hint" data-id="hint"></div>
</body>
</html>
`

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// cwState is the fixture's per-window mutable state (the harness's
// discipline: the view function re-runs every frame, the handles must
// not): the overlay handles and the focusable elements' focus handles.
type cwState struct {
	parent, child gpui.OverlayHandle
	hint          gpui.OverlayHandle
	focusB1       gpui.FocusHandle
	focusB2       gpui.FocusHandle
	focusB3       gpui.FocusHandle
	focusAnchor   gpui.FocusHandle
}

// cwStates keeps one state per replayed window: Replays can run the
// same fixture twice in one process with fresh windows.
var cwStates = map[*gpui.Window]*cwState{}

// cwStateOf returns (creating when absent) the window's state with the
// layer's toggle listeners wired to the log.
func cwStateOf(log *parity.EventLog, w *gpui.Window) *cwState {
	if state, ok := cwStates[w]; ok {
		return state
	}
	state := &cwState{
		parent:      gpui.NewOverlayHandle(w),
		child:       gpui.NewOverlayHandle(w),
		hint:        gpui.NewOverlayHandle(w),
		focusB1:     gpui.NewFocusHandle(w).TabStop(true),
		focusB2:     gpui.NewFocusHandle(w).TabStop(true),
		focusB3:     gpui.NewFocusHandle(w).TabStop(true),
		focusAnchor: gpui.NewFocusHandle(w).TabStop(true),
	}
	state.parent.OnToggle(log.Toggle("p1"))
	state.child.OnToggle(log.Toggle("p2"))
	state.hint.OnToggle(log.Toggle("hint"))
	log.TrackFocus(state.focusB1, "b1")
	log.TrackFocus(state.focusB2, "b2")
	log.TrackFocus(state.focusB3, "b3")
	log.TrackFocus(state.focusAnchor, "anchor")
	cwStates[w] = state
	return state
}

// escapeView is the gpui-go twin of page: the invokers, the two
// top-layer popovers nested through the invoker link, and the
// root-level anchor. Escape closes the newest overlay through the
// close watcher the TopLayer registered at show — no per-overlay key
// listener anywhere.
func escapeView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := cwStateOf(log, w)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Flex().Bg(gpui.Rgb(pageBackground)).
		Child(invokerBox(log, "b1", 0, 0, state.focusB1, state.parent)).
		Child(anchorBox(log, state)).
		OnKeyDown(log.KeyDown("root"))
	if state.parent.IsOpen() {
		p1 := popoverBox(log, "p1", parentX, parentY, parentW, parentH, parentAmber)
		p1.Child(invokerBox(log, "b2", 10, 10, state.focusB2, state.child).Absolute().Top(gpui.Px(10)).Left(gpui.Px(10)))
		root.Child(gpui.TopLayer(state.parent).Invoker("b1").Child(p1))
	}
	if state.child.IsOpen() {
		root.Child(gpui.TopLayer(state.child).Invoker("b2").
			Child(popoverBox(log, "p2", childX, childY, childW, childH, childGreen)))
	}
	return root.IntoElement()
}

// preventedView is the prevented fixture's twin: the root's keydown
// listener logs and preventDefaults (the page's capturing document
// listener).
func preventedView(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	state := cwStateOf(log, w)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Flex().Bg(gpui.Rgb(pageBackground)).
		Child(invokerBox(log, "b1", 0, 0, state.focusB1, state.parent)).
		Child(anchorBox(log, state)).
		OnKeyDown(log.KeyDownPreventDefault("root"))
	if state.parent.IsOpen() {
		p1 := popoverBox(log, "p1", parentX, parentY, parentW, parentH, parentAmber)
		p1.Child(invokerBox(log, "b2", 10, 10, state.focusB2, state.child).Absolute().Top(gpui.Px(10)).Left(gpui.Px(10)))
		root.Child(gpui.TopLayer(state.parent).Invoker("b1").Child(p1))
	}
	if state.child.IsOpen() {
		root.Child(gpui.TopLayer(state.child).Invoker("b2").
			Child(popoverBox(log, "p2", childX, childY, childW, childH, childGreen)))
	}
	return root.IntoElement()
}

// hintView is the hint fixture's twin: one invoker and one hint-kind
// top-layer entry.
func hintView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := cwStateOf(log, w)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Flex().Bg(gpui.Rgb(pageBackground)).
		Child(invokerBox(log, "b3", 0, 0, state.focusB3, state.hint)).
		OnKeyDown(log.KeyDown("root"))
	if state.hint.IsOpen() {
		root.Child(gpui.TopLayer(state.hint).Kind(gpui.OverlayHint).
			Child(popoverBox(log, "hint", hintX, hintY, hintW, hintH, hintColor)))
	}
	return root.IntoElement()
}

// EscapePerOverlay is the deliberately-wrong view for the
// discrimination test: the pre-ticket shape every component had to
// wire (the spec's problem statement: "Escape and click-outside only
// work if each component wires them up itself") — the entries are
// MANUAL kind (no close watcher, nothing the core closes) and each
// popover handles Escape through a key listener on its own content. A
// key listener only runs while focus sits INSIDE the overlay, so the
// record's first Escape (focus on b2, inside p1 and not inside p2)
// closes the PARENT instead of the newest child, and the second closes
// nothing at all (focus left the tree with p1, so no listener runs):
// the newest-overlay rule is exactly what the per-overlay wiring
// cannot express.
func EscapePerOverlay(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := cwStateOf(log, w)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Flex().Bg(gpui.Rgb(pageBackground)).
		Child(invokerBox(log, "b1", 0, 0, state.focusB1, state.parent)).
		Child(anchorBox(log, state)).
		OnKeyDown(log.KeyDown("root"))
	escapeHide := func(hide gpui.OverlayHandle) func(*gpui.KeyDownEvent, *gpui.Window, *gpui.App) {
		return func(e *gpui.KeyDownEvent, _ *gpui.Window, _ *gpui.App) {
			if e.Keystroke.Key == "escape" {
				hide.Hide()
			}
		}
	}
	if state.parent.IsOpen() {
		p1 := popoverBox(log, "p1", parentX, parentY, parentW, parentH, parentAmber)
		p1.Child(invokerBox(log, "b2", 10, 10, state.focusB2, state.child).Absolute().Top(gpui.Px(10)).Left(gpui.Px(10)))
		p1.OnKeyDown(escapeHide(state.parent))
		root.Child(gpui.TopLayer(state.parent).Kind(gpui.OverlayPopoverManual).Invoker("b1").Child(p1))
	}
	if state.child.IsOpen() {
		p2 := popoverBox(log, "p2", childX, childY, childW, childH, childGreen)
		p2.OnKeyDown(escapeHide(state.child))
		root.Child(gpui.TopLayer(state.child).Kind(gpui.OverlayPopoverManual).Invoker("b2").Child(p2))
	}
	return root.IntoElement()
}

// invokerBox builds one 40x20 invoker: the click logs and toggles the
// overlay (the popovertarget activation). focus is the invoker's
// focus handle (the page's buttons are focusable, and the keydown
// entries' focus field reads it).
func invokerBox(log *parity.EventLog, id string, x, y float32, focus gpui.FocusHandle, overlay gpui.OverlayHandle) *gpui.DivElement {
	return gpui.Div().ID(id).DebugSelector(id).
		Absolute().Top(gpui.Px(y)).Left(gpui.Px(x)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(invokerBlue)).
		TrackFocus(focus).
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
		OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(id)).
		OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			log.Click(id)(e, w, app)
			overlay.Toggle()
		}).
		OnKeyDown(log.KeyDown(id))
}

// anchorBox builds the root-level focusable anchor the Tab default
// moves focus onto.
func anchorBox(log *parity.EventLog, state *cwState) *gpui.DivElement {
	return gpui.Div().ID("anchor").DebugSelector("anchor").
		Absolute().Top(gpui.Px(anchorY)).Left(gpui.Px(anchorX)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(anchorGray)).
		TrackFocus(state.focusAnchor).
		OnKeyDown(log.KeyDown("anchor"))
}

// popoverBox builds one popover surface: the mousedown, mouseup, click
// and keydown listeners the page's data-id instrumentation binds (and
// the hitbox they insert — the light-dismiss chain's containment).
func popoverBox(log *parity.EventLog, id string, x, y, w, h float32, background uint32) *gpui.DivElement {
	return gpui.Div().ID(id).DebugSelector(id).
		Absolute().Top(gpui.Px(y)).Left(gpui.Px(x)).
		W(gpui.Px(w)).H(gpui.Px(h)).
		Bg(gpui.Rgb(background)).
		Occlude().
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
		OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(id)).
		OnClick(log.Click(id)).
		OnKeyDown(log.KeyDown(id))
}
