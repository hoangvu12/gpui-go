//go:build windows

// Package tooltip is ticket 24's fixture set for the tooltip hook
// (spec story 71), each recorded in headless Chrome and replayed
// against the gpui-go core. The recordable Chrome mechanism is the
// INTEREST INVOKER (interestfor) — the renderer-side twin of the title
// tooltip: the page's invoker carries interestfor="tip", and the
// popover=hint target opens 250ms after the hover and closes 150ms
// after the de-hover (the delay defaults, element.h:306-308). The
// NATIVE title tooltip is browser UI (the browser process paints it,
// ui/views — not in this checkout, not reachable from a headless
// record), so it serves only as the delay reference the port's
// constants adopt; the interest invoker is what both sides run.
//
//   - tooltip-hover: the timed hover story with key markers between
//     the waits (the transcript is order-only, so the markers pin when
//     the toggles land): nothing opens before the 250ms show delay
//     (the marker after a 100ms wait), the toggle lands after the
//     remaining 200ms, moving from the element onto the tooltip's own
//     surface keeps it open past the 150ms hide delay (the grace
//     period — the upstream cancel of the pending interest loss), and
//     the closed toggle lands only after leaving the tooltip and
//     another 150ms.
//   - tooltip-focus: the keyboard trigger — a Tab onto the invoker
//     opens the tooltip after the same 250ms (the keyup's focus field
//     proves the focusin source), and the Tab off closes it after
//     150ms.
//   - tooltip-look: the plain default look — the text in a
//     tooltip-yellow box below the element, a 4px gap away; the settle
//     leaves the tooltip open, so Tier A compares its box and Tier C
//     the pixels. The page's hint CSS pins the port's default look
//     (the two sides agree by construction, the port defining the
//     look).
//   - tooltip-escape: Escape closes it — the interest loss of
//     LoseInterestInAllElements (the default Escape handler's first
//     arm, before the close-watcher signal), recorded as the closed
//     toggle between the Escape's two halves (the pump move delivers
//     Chrome's queued toggle task).
//
// The pages follow the popover fixtures' shape (a fixed popover=hint
// whose UA styling is reset back to a plain box). The tooltips that
// need instrumented content (tooltip-hover's grace proof) use
// TooltipView; the others use the plain Tooltip(text) default look.
package tooltip

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

	// The tooltip: fixed at (0,24) — below the invoker at (0,0), a 4px
	// gap away (the port's default look's placement).
	tipX = 0.0
	tipY = 24.0
	tipW = 120.0
	tipH = 60.0

	// The pump point: an empty corner of every page (no invoker, no
	// tooltip, no listener) the Move steps travel to.
	pumpX = 400.0
	pumpY = 200.0

	// The default look's numbers (the port's plain default, pinned by
	// the look fixture's page): 16px text (the pin's default rem), the
	// 1.618 line height, black on #ffffe1, 4px padding.
	tipTextSize = 16.0
	tipPadding  = 4.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	invokerBlue    = 0x2563eb
	tipAmber       = 0xfde68a
	tipBackground  = 0xffffe1
	tipTextColor   = 0x000000
)

// tolerance is the flat-color pixel gate (the popover fixtures'
// measured numbers); the look fixture (text over a flat box) calibrates
// its own from the measured statistics. Provisional (research 19's
// calibration has not run). See docs/browser-parity-harness.md.
var tolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.02,
	MaxROIMeanChannelDiff: 0.1,
	MaxHotPixelFraction:   0.002,
}

// lookTolerance is the look fixture's gate, calibrated like the text
// fixtures' (the default look is one line of text over a flat box):
// the measured correct-view statistics are whole-image mean 0.011 /
// hot 0.0002, worst ROI (the tooltip's own box) mean 2.47 / hot 0.030 —
// the "Tip" glyphs' ~0.33px advance drift between the two text systems
// (Chrome shapes 21.67px where the port's DirectWrite path rounds to
// 22), the text-heavy fixtures' known raster noise. The gates sit at
// 2x those; the wrong-text discrimination view measures far above
// (tooltip_test.go). Provisional (research 19's calibration has not
// run). See docs/browser-parity-harness.md.
var lookTolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.05,
	MaxROIMeanChannelDiff: 5.0,
	MaxHotPixelFraction:   0.06,
}

// HoverFixture is the timed hover fixture: the show delay, the grace
// period and the hide delay, with key markers between the waits.
var HoverFixture = parity.Fixture{
	Name:   "tooltip-hover",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   hoverPage,
	Steps: []parity.Step{
		// The hover starts the interest timer (interest-delay-start).
		parity.Move(20, 10), // pointerenter:b1
		parity.Wait(100),
		// The marker: nothing has opened yet — Chrome's headless
		// renderer coalesces its timers (the hidden-page wake-up
		// alignment), so the OPEN lands anywhere from the 250ms
		// interest delay up to ~1s of coalescing later, but never
		// before the marker; the replay's virtual clock fires the
		// port's 250ms timer during the next wait — the same position,
		// between the marker and the leave. The exact constants are
		// pinned by the unit tests (gpui/tooltip_test.go); this fixture
		// pins the SHAPE: the delayed open, the grace, the delayed
		// close.
		parity.Key("x"),
		// Long enough that Chrome's coalesced timer is ready whichever
		// way it aligned.
		parity.Wait(2000),
		// From the invoker onto the tooltip's own surface: the leave of
		// b1 schedules the loss (interest-delay-end), the tip's enter
		// cancels it (the grace period's upstream arm).
		parity.Move(60, 54), // toggle:tip open, pointerleave:b1, pointerenter:tip
		// Past the hide delay: the grace keeps the tooltip open.
		parity.Wait(2000),
		// The marker: still open.
		parity.Key("y"),
		// Away from both: the tip's leave schedules the loss.
		parity.Move(pumpX, pumpY), // pointerleave:tip
		parity.Wait(100),
		// The marker: still open.
		parity.Key("z"),
		// The loss timer is ready; the pump move delivers the closed
		// toggle on Chrome (the replay fired it during the wait — the
		// same position, after the marker).
		parity.Wait(2000),
		parity.Move(pumpX, pumpY-10), // toggle:tip closed
	},
	ViewWithWindow: hoverView,
	// The tooltip is closed at the settle, so only the invoker's box
	// compares; the timed transcript carries the behaviour.
	BoxIDs:     []string{"b1"},
	EventKinds: []string{"pointerenter", "pointerleave", "toggle", "keydown", "keyup"},
	Tolerance:  tolerance,
}

// FocusFixture is the keyboard-trigger fixture: a Tab onto the invoker
// opens the tooltip after the show delay, a Tab off closes it after
// the hide delay.
var FocusFixture = parity.Fixture{
	Name:   "tooltip-focus",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   focusPage,
	Steps: []parity.Step{
		// Onto the invoker, the page's first tab stop (the keydown
		// listeners see the body, the keyup the invoker).
		parity.Key("tab"),
		// The wait makes the interest timer ready (the long wait
		// absorbs Chrome's hidden-page timer coalescing, like
		// HoverFixture's); the pump move delivers the toggle on Chrome
		// while the replay's virtual clock fires it during the wait —
		// the same transcript position.
		parity.Wait(2000),
		parity.Move(pumpX, pumpY), // toggle:tip open
		// Onto the anchor: the invoker's focusout schedules the loss.
		parity.Key("tab"),
		// 200ms: the interest is lost.
		parity.Wait(2000),
		parity.Move(pumpX, pumpY-10), // toggle:tip closed
	},
	ViewWithWindow: focusView,
	BoxIDs:         []string{"b1", "anchor"},
	EventKinds:     []string{"keydown", "keyup", "toggle"},
	Tolerance:      tolerance,
}

// LookFixture is the default-look fixture: the plain Tooltip(text)
// renders the text in a tooltip-yellow box below the element; the
// settle leaves it open, so the box and the pixels compare. A pure
// painting fixture besides the invoker's pointerenter.
var LookFixture = parity.Fixture{
	Name:   "tooltip-look",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   lookPage,
	Steps: []parity.Step{
		parity.Move(20, 10), // pointerenter:b1
		parity.Wait(2000),   // the interest timer fires; no toggle is logged
	},
	ViewWithWindow: lookView,
	BoxIDs:         []string{"b1", "tooltip"},
	EventKinds:     []string{"pointerenter"},
	Tolerance:      lookTolerance,
}

// EscapeFixture is the Escape fixture: the default Escape handler
// drops all interest before the close-watcher signal, closing the
// tooltip immediately.
var EscapeFixture = parity.Fixture{
	Name:   "tooltip-escape",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   hoverPage,
	Steps: []parity.Step{
		parity.Move(20, 10), // pointerenter:b1
		parity.Wait(2000),   // toggle:tip open
		// The Escape half-press: the interest loss closes the tooltip
		// during the keydown's default action.
		parity.KeyDown("escape"), // keydown:root
		// The pump move delivers Chrome's queued toggle task (the
		// port's end-of-dispatch drain already delivered it).
		parity.Move(pumpX, pumpY), // toggle:tip closed
		parity.KeyUp("escape"),    // keyup:root
	},
	ViewWithWindow: hoverView,
	BoxIDs:         []string{"b1"},
	EventKinds:     []string{"pointerenter", "pointerleave", "keydown", "keyup", "toggle"},
	Tolerance:      tolerance,
}

func init() {
	parity.Register(HoverFixture)
	parity.Register(FocusFixture)
	parity.Register(LookFixture)
	parity.Register(EscapeFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// invokerCSS is the shared invoker rule: a plain 40x20 blue BUTTON —
// the interest invoker element types are the button family
// (html_button_element's IsValidInterestInvoker override; a plain
// div's base implementation refuses), so every invoker is a button
// (and a native tab stop; the hover fixtures never focus it).
const invokerCSS = `	button {
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
`

// hintCSS is the hint popover's UA reset (the popover fixtures' rule):
// a plain fixed box at an explicit coordinate.
const hintCSS = `	/* The UA stylesheet centers popovers (position: fixed; inset: 0;
	   margin: auto; border; padding), so the pages reset all of it back
	   to plain fixed boxes (html.css:1966-1977). */
	.hint {
		position: fixed;
		inset: auto;
		margin: 0;
		border: 0;
		padding: 0;
		overflow: visible;
	}
`

// hoverPage is the hover/escape fixtures' page: an interest invoker and
// its popover=hint target, both instrumented for the boundary events.
const hoverPage = `<!doctype html>
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
` + invokerCSS + hintCSS + `	#tip {
		left: 0px;
		top: 24px;
		width: 120px;
		height: 60px;
		background: #fde68a;
	}
</style>
</head>
<body data-id="root" data-events="keydown,keyup">
	<button id="b1" interestfor="tip" data-id="b1"></button>
	<div id="tip" class="hint" popover="hint" data-id="tip"></div>
	<script>
		window.__t = {};
		window.__t.page = performance.now();
		document.addEventListener("pointerover", () => {
			if (!window.__t.over) window.__t.over = performance.now();
		}, true);
		document.getElementById("tip").addEventListener("toggle", (e) => {
			if (e.newState === "open") window.__t.open = performance.now();
			else window.__t.close = performance.now();
		});
	</script>
</body>
</html>
`

// focusPage is the focus fixture's page: the invoker and a second tab
// stop (the anchor), both focusable.
const focusPage = `<!doctype html>
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
` + invokerCSS + hintCSS + `	#anchor {
		left: 600px;
		top: 0px;
		background: #94a3b8;
	}
	#tip {
		left: 0px;
		top: 24px;
		width: 120px;
		height: 60px;
		background: #fde68a;
	}
</style>
</head>
<body data-id="root" data-events="keydown,keyup">
	<button id="b1" interestfor="tip" data-id="b1" tabindex="0"></button>
	<button id="anchor" data-id="anchor" tabindex="0"></button>
	<div id="tip" class="hint" popover="hint" data-id="tip"></div>
</body>
</html>
`

// lookPage is the default-look fixture's page: the hint's CSS pins the
// port's plain default look (the port defines the look, the page
// copies it — 16px text, the 1.618 line height, black on #ffffe1, 4px
// padding, below the element with a 4px gap, fit-content size).
const lookPage = `<!doctype html>
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
` + invokerCSS + hintCSS + `	/* The box is pinned to the port's measured size (the port defines
	   the look, the page copies it): the default look's box is
	   text-driven, whose float32-exact width the two text systems shape
	   0.33px apart ("Tip" at 16px), so the fixture pins the geometry and
	   lets Tier C carry the look. */
	/* The box is pinned to the port's measured size (the port defines
	   the look, the page copies it): the default look's box is
	   text-driven, whose float32-exact width the two text systems shape
	   0.33px apart ("Tip" at 16px), so the fixture pins the geometry and
	   lets Tier C carry the look. The CSS box is content-box: the
	   numbers are the port's border-box size minus the 8px padding.
	   Divergence toward CSS (browser-parity D1): the line-height pin
	   (1.618, the reference default) is dropped — the default line
	   height is CSS normal on both sides now, so the record carries
	   Chrome's normal text placement in the pinned box. */
	#tip {
		left: 0px;
		top: 24px;
		width: 22px;
		height: 21px;
		padding: 4px;
		background: #ffffe1;
		color: #000000;
		font: 16px "Segoe UI";
	}
</style>
</head>
<body data-id="root">
	<button id="b1" interestfor="tip" data-id="b1"></button>
	<div id="tip" class="hint" popover="hint" data-id="tooltip">Tip</div>
</body>
</html>
`

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// tooltipState is the fixture's per-window mutable state (the harness's
// discipline: the view function re-runs every frame, the handles must
// not).
type tooltipState struct {
	focusB1, focusAnchor gpui.FocusHandle
}

// tooltipStates keeps one state per replayed window: Replays can run
// the same fixture twice in one process with fresh windows.
var tooltipStates = map[*gpui.Window]*tooltipState{}

// tooltipStateOf returns (creating when absent) the window's state.
func tooltipStateOf(w *gpui.Window) *tooltipState {
	if state, ok := tooltipStates[w]; ok {
		return state
	}
	state := &tooltipState{
		focusB1:     gpui.NewFocusHandle(w).TabStop(true),
		focusAnchor: gpui.NewFocusHandle(w).TabStop(true),
	}
	tooltipStates[w] = state
	return state
}

// trackFocus wires the focus identities into the log (the key entries'
// focus field).
func (s *tooltipState) trackFocus(log *parity.EventLog) {
	log.TrackFocus(s.focusB1, "b1")
	log.TrackFocus(s.focusAnchor, "anchor")
}

// hoverView is the gpui-go twin of hoverPage: the invoker with the
// custom tooltip content (the instrumented tip div — the grace
// period's pointerenter/leave proof) and the toggle listener.
func hoverView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := tooltipStateOf(w)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Bg(gpui.Rgb(pageBackground)).
		OnKeyDown(log.KeyDown("root")).
		OnKeyUp(log.KeyUp("root")).
		Child(invokerBox(log, state, true))
	return root.IntoElement()
}

// focusView is the gpui-go twin of focusPage: the focusable invoker
// with the plain default tooltip, the anchor, and the toggle listener.
func focusView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := tooltipStateOf(w)
	state.trackFocus(log)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Bg(gpui.Rgb(pageBackground)).
		OnKeyDown(log.KeyDown("root")).
		OnKeyUp(log.KeyUp("root")).
		Child(invokerBox(log, state, false)).
		Child(anchorBox(log, state))
	return root.IntoElement()
}

// lookView is the gpui-go twin of lookPage: the invoker with the plain
// default look and no toggle listener (the fixture's transcript is the
// invoker's pointerenter alone).
func lookView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := tooltipStateOf(w)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Bg(gpui.Rgb(pageBackground)).
		OnPointerEnter(log.PointerEnter("root")).
		Child(lookInvokerBox(log, state))
	return root.IntoElement()
}

// lookInvokerBox is the look fixture's invoker: the default look with
// NO toggle listener (the fixture's page binds no toggle listener
// either — a pure painting fixture; the timed story is the hover
// fixture's).
func lookInvokerBox(log *parity.EventLog, state *tooltipState) *gpui.DivElement {
	return gpui.Div().ID("b1").DebugSelector("b1").
		Absolute().Top(gpui.Px(0)).Left(gpui.Px(0)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(invokerBlue)).
		TrackFocus(state.focusB1).OutlineNone().
		OnPointerEnter(log.PointerEnter("b1")).
		OnKeyDown(log.KeyDown("b1")).
		OnKeyUp(log.KeyUp("b1")).
		Tooltip("Tip")
}

// WrongLookView is the deliberately-wrong view for the discrimination
// test: the default look renders a different text, so the box's width
// (Tier A) and the pixels (Tier C) differ from the record — the
// discrimination that makes the look fixture's gates mean something.
func WrongLookView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := tooltipStateOf(w)
	root := gpui.Div().ID("root").DebugSelector("root").
		SizeFull().Bg(gpui.Rgb(pageBackground)).
		Child(wrongInvokerBox(state))
	return root.IntoElement()
}

// invokerBox builds the invoker: the pointerenter/leave listeners the
// page's data-id instrumentation binds, the focus handle for the focus
// fixture, and the tooltip. withCustomTip switches between the
// instrumented custom content (the grace proof) and the plain default
// look.
func invokerBox(log *parity.EventLog, state *tooltipState, withCustomTip bool) *gpui.DivElement {
	box := gpui.Div().ID("b1").DebugSelector("b1").
		Absolute().Top(gpui.Px(0)).Left(gpui.Px(0)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(invokerBlue)).
		OnPointerEnter(log.PointerEnter("b1")).
		OnPointerLeave(log.PointerLeave("b1")).
		OnKeyDown(log.KeyDown("b1")).
		OnKeyUp(log.KeyUp("b1")).
		TrackFocus(state.focusB1).OutlineNone()
	if withCustomTip {
		return box.TooltipView(func(w *gpui.Window, app *gpui.App) gpui.AnyElement {
			return gpui.Div().ID("tip").DebugSelector("tip").
				Absolute().Top(gpui.Px(tipY)).Left(gpui.Px(tipX)).
				W(gpui.Px(tipW)).H(gpui.Px(tipH)).
				Bg(gpui.Rgb(tipAmber)).Occlude().
				OnPointerEnter(log.PointerEnter("tip")).
				OnPointerLeave(log.PointerLeave("tip")).
				IntoElement()
		}).OnTooltipToggle(log.Toggle("tip"))
	}
	return box.Tooltip("Tip").OnTooltipToggle(log.Toggle("tip"))
}

// wrongInvokerBox is the wrong view's invoker: the default look with a
// different text.
func wrongInvokerBox(state *tooltipState) *gpui.DivElement {
	return gpui.Div().ID("b1").DebugSelector("b1").
		Absolute().Top(gpui.Px(0)).Left(gpui.Px(0)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(invokerBlue)).
		TrackFocus(state.focusB1).OutlineNone().
		OnPointerEnter(func(*gpui.PointerEnterEvent, *gpui.Window, *gpui.App) {}).
		Tooltip("A different tooltip")
}

// anchorBox builds the focus fixture's second tab stop.
func anchorBox(log *parity.EventLog, state *tooltipState) *gpui.DivElement {
	return gpui.Div().ID("anchor").DebugSelector("anchor").
		Absolute().Top(gpui.Px(0)).Left(gpui.Px(600)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(0x94a3b8)).
		TrackFocus(state.focusAnchor).OutlineNone().
		OnKeyDown(log.KeyDown("anchor")).
		OnKeyUp(log.KeyUp("anchor"))
}
