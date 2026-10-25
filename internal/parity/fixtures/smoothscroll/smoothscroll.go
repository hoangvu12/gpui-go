//go:build windows

// Package smoothscroll is browser-core ticket 45's fixture set for
// smooth scrolling (spec D13): pages whose scrollers scroll through
// Chrome's curve, recorded in headless Chrome with smooth scrolling ON
// (the recorder's --enable-smooth-scrolling, which wins over the
// harness's own --disable-smooth-scrolling — the flag pair's OR,
// content/browser/web_contents/web_contents_impl.cc:4042-4046) and
// replayed with the port's smooth scrolling on (the fixture's
// SmoothScrolling flag opts the TestApp in).
//
//   - smoothscroll-keyboard: the keyboard path. A click focuses the
//     scroller's first row, two ArrowDown presses scroll 40px each
//     (Chromium's kPixelsPerLineStep) — the second press retargets the
//     running animation (blink's UserScroll accumulates the target,
//     scroll_animator.cc:88-140) — and the record's settle reads the
//     scroller at its settled end state. The 150ms animation (the
//     keyboard's constant duration) is over long before the settle, so
//     Tier A compares the END state exactly: the port's animated scroll
//     must converge to Chrome's target.
//   - smoothscroll-programmatic: the CSSOM path. A page script scrolls
//     with behavior "smooth" and the replay's Script step calls the
//     handle's ScrollTo(ScrollBehaviorSmooth) — Chromium's programmatic
//     curve (the tweaked bezier, sqrt-of-delta duration) — and the
//     settle reads the end state.
//
// What the records can and cannot pin, stated plainly: Chrome's smooth
// scroll animations run on the compositor as impl-only cc animations —
// they are not document.getAnimations() Web Animations, so the
// recorder's Wait step cannot seek them, and CDP-dispatched wheels are
// kScrollByPrecisePixel (input_handler.cc:484), which never animates —
// so no timed mid-animation sample can be recorded from Chrome. The
// records therefore pin the smooth paths' end states (the convergence
// and the step sizes), and the mid-flight curve is pinned two ways on
// the port side: the fixture package's own mid-animation replay (the
// offset at exactly half the keyboard animation is the ease-in-out
// midpoint, 20px of the 40px step — not the instant 40) and the
// gpui unit tests that fit Chromium's curve formulas
// (gpui/scroll_smooth_test.go).
package smoothscroll

import (
	"fmt"
	"sync"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui views.
const (
	windowWidth  = 320.0
	windowHeight = 240.0

	scrollerW = 200.0
	scrollerH = 200.0
	scrollerX = 10.0
	scrollerY = 10.0

	rowH   = 40.0
	rowCnt = 10 // 10 rows of 40px = 400px of content, max 200

	// settleWait outlasts every animation the fixture runs (the
	// keyboard's 150ms; the programmatic sqrt(200)/60 = 236ms), so both
	// sides settle at their targets when the record is read.
	settleWait = 600

	// programmaticTop is the smooth programmatic scroll's target.
	programmaticTop = 200.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	scrollerBg     = 0xf1f5f9
	rowBg          = 0xcbd5e1
	focusBg        = 0xfca5a5
)

// boxIDs are the compared boxes: the scroller and the rows the settled
// offsets move.
var boxIDs = []string{"scroller", "rrow0", "rrow1", "rrow2", "rrow4"}

// Fixture is the keyboard smooth-scroll fixture: click to focus, two
// ArrowDown presses (one 40px step each), settle past the animation.
var Fixture = parity.Fixture{
	Name:   "smoothscroll-keyboard",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(true),
	Steps: []parity.Step{
		// Focus the first row: the mouse-press node's chain is the
		// keyboard scroll's start node (LogicalScroll,
		// scroll_manager.cc:216-232) — the wheel fixture's
		// keyboard-target pattern.
		parity.Click(110, 30),
		// Two steps of 40px: the first starts the 150ms animation, the
		// second retargets it to 80px (blink's accumulated target).
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		// The settle: past the animation, both sides at 80px.
		parity.Wait(settleWait),
	},
	BoxIDs:         boxIDs,
	ViewWithWindow: keyboardView,
	// Chrome records with smooth scrolling on; the replay opts the
	// port's smooth scrolling in (the TestApp default is the recorder's
	// --disable-smooth-scrolling shape).
	SmoothScrolling: true,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), measured
		// from the correct view: flat-colour geometry, pixel-exact at
		// the 2.0 device scale. Verified by smoothscroll_test.go.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// ProgrammaticFixture is the CSSOM smooth-scroll fixture: a page script
// scrolls with behavior "smooth", the replay's Script half calls the
// handle's ScrollTo(ScrollBehaviorSmooth), and the settle reads the end
// state.
var ProgrammaticFixture = parity.Fixture{
	Name:   "smoothscroll-programmatic",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(false),
	Steps: []parity.Step{
		// The smooth programmatic scroll: the page's half is
		// scrollTo({behavior: "smooth"}) and the view's half the
		// handle's ScrollTo with ScrollBehaviorSmooth — Chromium's
		// programmatic curve (the tweaked bezier, the sqrt(200)/60 =
		// 236ms duration).
		parity.Script(
			"document.getElementById('scroller').scrollTo({top: "+fmt.Sprintf("%.0f", programmaticTop)+", behavior: 'smooth'})",
			func(log *parity.EventLog, w *gpui.Window, app *gpui.App) {
				handleOf(w).ScrollTo(gpui.Point{Y: -programmaticTop}, gpui.ScrollBehaviorSmooth, w, app)
			},
		),
		parity.Wait(settleWait),
	},
	BoxIDs:          boxIDs,
	ViewWithWindow:  plainView,
	SmoothScrolling: true,
	Tolerance: parity.PixelTolerance{
		// Provisional, measured from the correct view: flat-colour
		// geometry, pixel-exact at the 2.0 device scale.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(Fixture)
	parity.Register(ProgrammaticFixture)
}

// page is one fixture's page: a single 200x200 scroller over ten 40px
// rows (400px of content, max 200). focusable marks the first row
// focusable for the keyboard fixture.
func page(focusable bool) string {
	firstAttrs := ""
	if focusable {
		firstAttrs = ` tabindex="0"`
	}
	rows := ""
	for i := 0; i < rowCnt; i++ {
		attrs := ""
		style := ""
		if i == 0 && focusable {
			attrs = firstAttrs
			style = "\n\t\t\tbackground: #fca5a5;\n\t\t\toutline: none;"
		}
		rows += fmt.Sprintf("\n\t\t<div id=\"rrow%d\" data-id=\"rrow%d\"%s style=\"width: %.0fpx; height: %.0fpx;%s\"></div>",
			i, i, attrs, scrollerW, rowH, style)
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
		width: ` + fmt.Sprintf("%.0f", windowWidth) + `px;
		height: ` + fmt.Sprintf("%.0f", windowHeight) + `px;
		display: flex;
		align-items: flex-start;
	}
	#scroller {
		box-sizing: border-box;
		width: ` + fmt.Sprintf("%.0f", scrollerW) + `px;
		height: ` + fmt.Sprintf("%.0f", scrollerH) + `px;
		margin-left: ` + fmt.Sprintf("%.0f", scrollerX) + `px;
		margin-top: ` + fmt.Sprintf("%.0f", scrollerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		/* The port draws no scrollbars and reserves no gutter. */
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#scroller > div {
		flex-shrink: 0;
		box-sizing: border-box;
		background: #cbd5e1;
	}
</style>
</head>
<body data-id="root">
	<div id="scroller" data-id="scroller">` + rows + `
	</div>
</body>
</html>`
}

// The per-window state (the scroll handle must be fresh for a fresh
// window: Replay runs the same fixture twice — TestFixtures and the
// tolerance headroom check — and a shared handle would carry the first
// run's offset into the second's record comparison). The keyboard
// fixture's focus handle is per-window for the same reason.
var (
	stateMu     sync.Mutex
	handleByWin = map[*gpui.Window]gpui.ScrollHandle{}
	focusByWin  = map[*gpui.Window]gpui.FocusHandle{}
)

// handleOf resolves (creating on first use) the window's scroll handle.
func handleOf(w *gpui.Window) gpui.ScrollHandle {
	stateMu.Lock()
	defer stateMu.Unlock()
	if handle, ok := handleByWin[w]; ok {
		return handle
	}
	handle := gpui.NewScrollHandle()
	handleByWin[w] = handle
	return handle
}

// focusOf resolves (creating on first use) the window's focus handle.
func focusOf(w *gpui.Window) gpui.FocusHandle {
	stateMu.Lock()
	defer stateMu.Unlock()
	if focus, ok := focusByWin[w]; ok {
		return focus
	}
	focus := gpui.NewFocusHandle(w).TabStop(true)
	focusByWin[w] = focus
	return focus
}

// scrollerView builds the page's twin: the scroller over ten rows. When
// focusable, the first row tracks the focus handle (the page's
// tabindex row, with the outline: none pin's .OutlineNone() twin).
func scrollerView(w *gpui.Window, focusable bool) gpui.AnyElement {
	handle := handleOf(w)
	scroller := gpui.Div().Flex().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(scrollerW), gpui.Px(scrollerH)).
		Ml(gpui.Px(scrollerX)).
		Mt(gpui.Px(scrollerY)).
		FlexCol().
		OverflowYScroll().
		TrackScroll(handle).
		Bg(gpui.Rgb(scrollerBg))
	for i := 0; i < rowCnt; i++ {
		id := fmt.Sprintf("rrow%d", i)
		row := gpui.Div().Flex().
			ID(id).
			DebugSelector(id).
			Size(gpui.Px(scrollerW), gpui.Px(rowH)).
			FlexShrink0().
			Bg(gpui.Rgb(rowBg))
		if i == 0 && focusable {
			// The page's focusable row pins outline: none; the port
			// draws the default focus ring on focused, focus-visible
			// elements (ticket 48), so the view carries the twin.
			row = row.TrackFocus(focusOf(w)).OutlineNone().Bg(gpui.Rgb(focusBg))
		}
		scroller = scroller.Child(row.IntoElement())
	}
	return gpui.Div().
		ID("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground)).
		Child(scroller).
		IntoElement()
}

// keyboardView is the keyboard fixture's gpui twin.
func keyboardView(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return scrollerView(w, true)
}

// KeyboardView is the exported form of the keyboard fixture's twin
// (the discrimination tests' entry point).
func KeyboardView(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return scrollerView(w, true)
}

// HandleOfTest resolves the window's scroll handle — the test-side
// accessor for reading a mid-animation offset (smoothscroll_test.go).
func HandleOfTest(w *gpui.Window) gpui.ScrollHandle {
	return handleOf(w)
}

// plainView is the programmatic fixture's gpui twin.
func plainView(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return scrollerView(w, false)
}

// WrongView is the deliberately-wrong view of Fixture for the
// discrimination test: the scroller never scrolls — the view's rows
// keep their unscrolled positions — so the Tier A comparison of the
// settled record (the rows moved by 80px) fails on exactly the moved
// boxes, while the scroller's own box still passes.
func WrongView(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	handle := handleOf(w)
	scroller := gpui.Div().Flex().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(scrollerW), gpui.Px(scrollerH)).
		Ml(gpui.Px(scrollerX)).
		Mt(gpui.Px(scrollerY)).
		FlexCol().
		Bg(gpui.Rgb(scrollerBg))
	for i := 0; i < rowCnt; i++ {
		id := fmt.Sprintf("rrow%d", i)
		row := gpui.Div().Flex().
			ID(id).
			DebugSelector(id).
			Size(gpui.Px(scrollerW), gpui.Px(rowH)).
			FlexShrink0().
			Bg(gpui.Rgb(rowBg))
		if i == 0 {
			row = row.TrackFocus(focusOf(w)).OutlineNone().Bg(gpui.Rgb(focusBg))
		}
		scroller = scroller.Child(row.IntoElement())
	}
	_ = handle
	return gpui.Div().
		ID("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground)).
		Child(scroller).
		IntoElement()
}
