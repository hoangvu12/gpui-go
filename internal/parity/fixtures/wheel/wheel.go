//go:build windows

// Package wheel is browser-core ticket 15's fixture set for the wheel
// scroll latching, overscroll-behavior containment and the keyboard
// scroll's start node: nested scrollers where one wheel gesture
// latches to the innermost scroller that can scroll — the latched
// scroller keeps the gesture at its end (no chaining mid-gesture), a
// new gesture after the 500ms latching transaction chains to the outer
// scroller, overscroll-behavior: contain stops the chaining, and the
// keyboard arrows scroll the focused chain or, with nothing focused,
// the mouse-press node's chain.
//
// The pages are text-free geometry (the boxpaint discipline), so Tier C
// carries no font rasterization noise; the scroll offsets are visible
// through the child boxes (Tier A, exact).
//
// Chromium's rules these fixtures pin:
//
//   - the latched scroller for a gesture is the innermost one under the
//     wheel that can consume the delta (cc/input/input_handler.cc
//     FindNodeToLatch, :2341-2405), and the gesture's updates scroll the
//     latched node only (ScrollLatchedScroller, :2195-2330) — an inner
//     scroller at its end keeps the gesture;
//   - classic (phaseless) wheel events form one transaction for 500ms
//     after the last event (kDefaultMouseWheelLatchingTransaction,
//     content/browser/renderer_host/input/mouse_wheel_phase_handler.h:
//     20) — a Wait(600) between wheel steps starts a new gesture;
//   - overscroll-behavior: contain stops the chaining at the scroller
//     that cannot consume (FindNodeToLatch's CanPropagate arm,
//     input_handler.cc:2362-2378; the keyboard chain is cut the same
//     way, core/input/scroll_manager.cc RecomputeScrollChain :192-213);
//   - the keyboard scroll starts at the focused element's chain, else
//     the mouse-press node's, else the root scroller (LogicalScroll,
//     scroll_manager.cc:216-232).
package wheel

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

	outerW = 220.0
	outerH = 200.0
	outerX = 10.0
	outerY = 10.0

	innerW = 180.0
	innerH = 120.0
	innerM = 20.0 // the inner scroller's margin inside the outer

	rowH   = 60.0
	rowCnt = 5 // 5 rows of 60px = 300px of content, inner max 180

	spacerH = 300.0 // the outer's spacer: outer content 440, max 240

	// wheelDelta is one wheel notch: 60 logical px down.
	wheelDelta = -60.0
	// latchingWait is beyond Chromium's 500ms wheel latching
	// transaction, so the wheel steps on either side of it are separate
	// gestures on both sides (the recorder's wait sleeps real time; the
	// replay advances the virtual clock).
	latchingWait = 600
)

// The keyboard-target fixture's numbers.
const (
	ktScrollerW = 140.0
	ktScrollerH = 200.0
	ktLeftX     = 10.0
	ktRightX    = 170.0
	ktScrollerY = 10.0
	ktRowH      = 40.0
	ktRowCount  = 10 // 10 rows of 40px = 400px of content, max 200
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	outerBg        = 0xf1f5f9
	innerBg        = 0x93c5fd
	rowBg          = 0xcbd5e1
	spacerBg       = 0xe2e8f0
	lastRowBg      = 0x16a34a
	focusBg        = 0xfca5a5
)

// latchBoxIDs are the wheel-latch fixture's compared boxes.
var latchBoxIDs = []string{
	"outer", "inner", "irow2", "irow3", "irow4", "ospacer",
}

// Fixture is the wheel-latching fixture: four wheel notches over the
// inner scroller (range 180) in ONE gesture scroll it to its end — the
// fourth notch cannot chain to the outer (the gesture is latched) —
// and a fifth notch after the 500ms latching transaction ends starts a
// new gesture that chains to the outer scroller.
var Fixture = parity.Fixture{
	Name:   "wheel-latch",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   wheelPage(""),
	Steps: []parity.Step{
		// One gesture of 240px over the inner scroller (range 180): the
		// innermost scroller that can consume latches the gesture, takes
		// the whole delta (clamped to its end) and the outer does not
		// move — the pre-ticket port scrolled BOTH (the ticket's spec
		// risk).
		parity.Wheel(100, 100, 0, -240),
		// The transaction ends (the recorder's wait sleeps past the
		// 500ms latching transaction; the replay advances the virtual
		// clock). A new gesture: the inner scroller is exhausted, so the
		// walk continues and the outer latches.
		parity.Wait(latchingWait),
		parity.Wheel(100, 100, 0, wheelDelta),
	},
	BoxIDs:         latchBoxIDs,
	ViewWithWindow: WheelLatchView,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), measured
		// from the correct view: flat-colour geometry, pixel-exact at
		// the 2.0 device scale. Verified by wheel_test.go.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// ContainFixture is the overscroll-behavior: contain fixture: the same
// nesting with contain on the inner scroller — the first gesture still
// scrolls the inner to its end, and a new gesture after the latching
// transaction cannot chain: the walk latches to the contain scroller
// without consuming, so nothing scrolls.
var ContainFixture = parity.Fixture{
	Name:   "wheel-contain",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   wheelPage("overscroll-behavior: contain;"),
	Steps: []parity.Step{
		// One gesture of 240px over the inner scroller: contain does not
		// stop the scroller's OWN scrolling — the inner latches the
		// gesture and scrolls to its end (clamped, 180).
		parity.Wheel(100, 100, 0, -240),
		parity.Wait(latchingWait),
		// A new gesture after the transaction: the inner cannot consume,
		// and its overscroll-behavior: contain cuts the chaining — the
		// walk latches to the contain scroller without consuming, so
		// nothing scrolls and the outer never moves.
		parity.Wheel(100, 100, 0, wheelDelta),
	},
	BoxIDs:         latchBoxIDs,
	ViewWithWindow: WheelContainView,
	Tolerance: parity.PixelTolerance{
		// Provisional, measured from the correct view: flat-colour
		// geometry, pixel-exact at the 2.0 device scale.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// KeyboardTargetFixture is the keyboard-scroll start-node fixture: two
// scrollers side by side. A click on the focusable row inside the left
// one focuses it — the arrows scroll the LEFT scroller (the focused
// chain); a click on the plain content of the right one blurs focus —
// the arrows scroll the RIGHT scroller (the mouse-press node's chain,
// LogicalScroll's fallback, scroll_manager.cc:216-232).
var KeyboardTargetFixture = parity.Fixture{
	Name:   "keyboard-scroll-target",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   keyboardTargetPage,
	Steps: []parity.Step{
		// Focus the left scroller's row, then arrows: the left scrolls.
		parity.Click(80, 30),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		// Blur onto the right scroller's plain content, then arrows: the
		// press node's chain scrolls the right scroller.
		parity.Click(240, 30),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
	},
	BoxIDs:         []string{"left", "lrow0", "lrow1", "right", "rrow0", "rrow2"},
	ViewWithWindow: KeyboardTargetView,
	Tolerance: parity.PixelTolerance{
		// Provisional, measured from the correct view: flat-colour
		// geometry, pixel-exact at the 2.0 device scale.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// KeyboardContainFixture is the keyboard half of the contain rule: the
// nested scrollers with contain, the inner scrolled to its end with the
// wheel, then a click focuses its last row — the arrows cannot scroll
// the exhausted inner scroller, and the contain cut stops the walk
// before the outer, so nothing scrolls.
var KeyboardContainFixture = parity.Fixture{
	Name:   "keyboard-contain",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   keyboardContainPage,
	Steps: []parity.Step{
		// Scroll the inner scroller to its end with one gesture.
		parity.Wheel(100, 100, 0, -180),
		parity.Wait(latchingWait),
		// Focus the inner's last row, then arrows: nothing scrolls.
		parity.Click(100, 120),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
	},
	BoxIDs:         []string{"inner", "irow2", "irow3", "irow4", "ospacer"},
	ViewWithWindow: KeyboardContainView,
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
	parity.Register(ContainFixture)
	parity.Register(KeyboardTargetFixture)
	parity.Register(KeyboardContainFixture)
}

// f2s renders one shared float constant for the pages' CSS.
func f2s(v float64) string { return fmt.Sprintf("%.0f", v) }

// innerRows builds the inner scroller's rows (irow0..irow4) as gpui
// elements; focusHandle (when non-nil) tracks the last row's focus.
func innerRows(w *gpui.Window, focusHandle *gpui.FocusHandle) []gpui.AnyElement {
	rows := make([]gpui.AnyElement, 0, rowCnt)
	for i := 0; i < rowCnt; i++ {
		id := fmt.Sprintf("irow%d", i)
		row := gpui.Div().
			ID(id).
			DebugSelector(id).
			Size(gpui.Px(innerW), gpui.Px(rowH)).
			FlexShrink0().
			Bg(gpui.Rgb(rowBg))
		if i == rowCnt-1 {
			row = row.Bg(gpui.Rgb(lastRowBg))
			if focusHandle != nil {
				row = row.TrackFocus(*focusHandle).OutlineNone()
			}
		}
		rows = append(rows, row.IntoElement())
	}
	return rows
}

// wheelPage is one wheel fixture's page: the nested scrollers. The
// inner's extra CSS is the contain rule of the contain variant.
func wheelPage(innerCSS string) string {
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
		align-items: flex-start;
	}
	#outer {
		box-sizing: border-box;
		width: ` + f2s(outerW) + `px;
		height: ` + f2s(outerH) + `px;
		margin-left: ` + f2s(outerX) + `px;
		margin-top: ` + f2s(outerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		/* The port draws no scrollbars and reserves no gutter. */
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#outer > div {
		flex-shrink: 0;
	}
	#inner {
		box-sizing: border-box;
		width: ` + f2s(innerW) + `px;
		height: ` + f2s(innerH) + `px;
		margin-left: ` + f2s(innerM) + `px;
		margin-top: ` + f2s(innerM) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		scrollbar-width: none;
		` + innerCSS + `
		background: #93c5fd;
	}
	#inner > div {
		flex-shrink: 0;
		box-sizing: border-box;
		width: ` + f2s(innerW) + `px;
		height: ` + f2s(rowH) + `px;
		background: #cbd5e1;
	}
	#inner > div:last-child {
		background: #16a34a;
	}
	#ospacer {
		box-sizing: border-box;
		width: ` + f2s(outerW) + `px;
		height: ` + f2s(spacerH) + `px;
		background: #e2e8f0;
	}
</style>
</head>
<body data-id="root">
	<div id="outer" data-id="outer">
		<div id="inner" data-id="inner">
			<div data-id="irow0"></div>
			<div data-id="irow1"></div>
			<div data-id="irow2"></div>
			<div data-id="irow3"></div>
			<div data-id="irow4"></div>
		</div>
		<div id="ospacer" data-id="ospacer"></div>
	</div>
</body>
</html>`
}

// WheelLatchView is the wheel-latch fixture's gpui twin (the
// window-aware form the fixtures and the discrimination test use).
func WheelLatchView(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return wheelView("")(log)
}

// WheelContainView is the wheel-contain fixture's gpui twin.
func WheelContainView(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return wheelView("contain")(log)
}

// WheelView builds one wheel fixture's twin with the inner scroller's
// overscroll-behavior selected by contain ("" or "contain") — the
// discrimination test's entry point for the wrong view.
func WheelView(log *parity.EventLog, w *gpui.Window, app *gpui.App, contain string) gpui.AnyElement {
	return wheelView(contain)(log)
}

// wheelView is one wheel fixture's gpui twin. contain selects the
// inner scroller's overscroll-behavior.
func wheelView(contain string) func(log *parity.EventLog) gpui.AnyElement {
	return func(log *parity.EventLog) gpui.AnyElement {
		inner := gpui.Div().Flex().
			ID("inner").
			DebugSelector("inner").
			Size(gpui.Px(innerW), gpui.Px(innerH)).
			Ml(gpui.Px(innerM)).
			Mt(gpui.Px(innerM)).
			FlexCol().
			OverflowYScroll().
			FlexShrink0().
			Bg(gpui.Rgb(innerBg))
		if contain == "contain" {
			inner = inner.OverscrollContain()
		}
		for _, row := range innerRows(nil, nil) {
			inner = inner.Child(row)
		}
		outer := gpui.Div().Flex().
			ID("outer").
			DebugSelector("outer").
			Size(gpui.Px(outerW), gpui.Px(outerH)).
			Ml(gpui.Px(outerX)).
			Mt(gpui.Px(outerY)).
			FlexCol().
			OverflowYScroll().
			Bg(gpui.Rgb(outerBg)).
			Child(inner).
			Child(gpui.Div().
				ID("ospacer").
				DebugSelector("ospacer").
				Size(gpui.Px(outerW), gpui.Px(spacerH)).
				FlexShrink0().
				Bg(gpui.Rgb(spacerBg)))
		return gpui.Div().
			ID("root").
			SizeFull().
			Flex().
			AlignItems(gpui.AlignItemsFlexStart).
			Bg(gpui.Rgb(pageBackground)).
			Child(outer).
			IntoElement()
	}
}

// keyboardTargetPage is the keyboard-target fixture's page: two
// scrollers side by side, the left one's first row focusable, ten rows
// of 40px each (400px of content, max 200).
var keyboardTargetPage = `<!doctype html>
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
		align-items: flex-start;
	}
	#left, #right {
		box-sizing: border-box;
		width: ` + f2s(ktScrollerW) + `px;
		height: ` + f2s(ktScrollerH) + `px;
		margin-top: ` + f2s(ktScrollerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#left {
		margin-left: ` + f2s(ktLeftX) + `px;
	}
	#right {
		margin-left: ` + f2s(ktRightX-ktLeftX-ktScrollerW) + `px;
	}
	#left > div, #right > div {
		flex-shrink: 0;
		box-sizing: border-box;
		width: ` + f2s(ktScrollerW) + `px;
		height: ` + f2s(ktRowH) + `px;
		background: #cbd5e1;
	}
	#lrow0 {
		background: #fca5a5;
		outline: none;
	}
</style>
</head>
<body data-id="root">
	<div id="left" data-id="left">` + ktRowsHTML("l") + `
	</div>
	<div id="right" data-id="right">` + ktRowsHTML("r") + `
	</div>
</body>
</html>`

// ktRowsHTML builds one scroller's ten row divs; the left prefix's
// first row carries the tabindex (the focusable target).
func ktRowsHTML(prefix string) string {
	s := ""
	for i := 0; i < ktRowCount; i++ {
		attrs := ""
		if prefix == "l" && i == 0 {
			attrs = ` tabindex="0"`
		}
		s += fmt.Sprintf("\n\t\t<div id=\"%srow%d\" data-id=\"%srow%d\"%s></div>", prefix, i, prefix, i, attrs)
	}
	return s
}

// keyboardContainPage is the keyboard-contain fixture's page: the
// nested scrollers with contain, the inner's last row focusable.
var keyboardContainPage = `<!doctype html>
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
		align-items: flex-start;
	}
	#outer {
		box-sizing: border-box;
		width: ` + f2s(outerW) + `px;
		height: ` + f2s(outerH) + `px;
		margin-left: ` + f2s(outerX) + `px;
		margin-top: ` + f2s(outerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#outer > div {
		flex-shrink: 0;
	}
	#inner {
		box-sizing: border-box;
		width: ` + f2s(innerW) + `px;
		height: ` + f2s(innerH) + `px;
		margin-left: ` + f2s(innerM) + `px;
		margin-top: ` + f2s(innerM) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		scrollbar-width: none;
		overscroll-behavior: contain;
		background: #93c5fd;
	}
	#inner > div {
		flex-shrink: 0;
		box-sizing: border-box;
		width: ` + f2s(innerW) + `px;
		height: ` + f2s(rowH) + `px;
		background: #cbd5e1;
	}
	#inner > div:last-child {
		background: #16a34a;
		outline: none;
	}
	#ospacer {
		box-sizing: border-box;
		width: ` + f2s(outerW) + `px;
		height: ` + f2s(spacerH) + `px;
		background: #e2e8f0;
	}
</style>
</head>
<body data-id="root">
	<div id="outer" data-id="outer">
		<div id="inner" data-id="inner">
			<div data-id="irow0"></div>
			<div data-id="irow1"></div>
			<div data-id="irow2"></div>
			<div data-id="irow3"></div>
			<div data-id="irow4" tabindex="0"></div>
		</div>
		<div id="ospacer" data-id="ospacer"></div>
	</div>
</body>
</html>`

// The per-window focus handles of the keyboard fixtures (the
// scrollrange pattern: the handle lives outside the view function so
// its identity survives the per-frame rebuilds, and a fresh window
// gets a fresh handle).
var (
	focusMu      sync.Mutex
	targetByWin  = map[*gpui.Window]gpui.FocusHandle{}
	containByWin = map[*gpui.Window]gpui.FocusHandle{}
)

// targetFocusOf resolves (creating on first use) the keyboard-target
// fixture's focus row handle.
func targetFocusOf(w *gpui.Window) gpui.FocusHandle {
	focusMu.Lock()
	defer focusMu.Unlock()
	if focus, ok := targetByWin[w]; ok {
		return focus
	}
	focus := gpui.NewFocusHandle(w).TabStop(true)
	targetByWin[w] = focus
	return focus
}

// containFocusOf resolves (creating on first use) the keyboard-contain
// fixture's focus row handle.
func containFocusOf(w *gpui.Window) gpui.FocusHandle {
	focusMu.Lock()
	defer focusMu.Unlock()
	if focus, ok := containByWin[w]; ok {
		return focus
	}
	focus := gpui.NewFocusHandle(w).TabStop(true)
	containByWin[w] = focus
	return focus
}

// KeyboardTargetView is the keyboard-target fixture's gpui twin: the
// left scroller's first row tracks the focus handle.
func KeyboardTargetView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	focus := targetFocusOf(w)
	rows := func(prefix string) []gpui.AnyElement {
		out := make([]gpui.AnyElement, 0, ktRowCount)
		for i := 0; i < ktRowCount; i++ {
			id := fmt.Sprintf("%srow%d", prefix, i)
			row := gpui.Div().
				ID(id).
				DebugSelector(id).
				Size(gpui.Px(ktScrollerW), gpui.Px(ktRowH)).
				FlexShrink0().
				Bg(gpui.Rgb(rowBg))
			if prefix == "l" && i == 0 {
				row = row.TrackFocus(focus).OutlineNone().Bg(gpui.Rgb(focusBg))
			}
			out = append(out, row.IntoElement())
		}
		return out
	}
	scroller := func(id string, x float32, children []gpui.AnyElement) *gpui.DivElement {
		d := gpui.Div().Flex().
			ID(id).
			DebugSelector(id).
			Size(gpui.Px(ktScrollerW), gpui.Px(ktScrollerH)).
			Ml(gpui.Px(x)).
			Mt(gpui.Px(ktScrollerY)).
			FlexCol().
			OverflowYScroll().
			Bg(gpui.Rgb(outerBg))
		for _, child := range children {
			d = d.Child(child)
		}
		return d
	}
	return gpui.Div().
		ID("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground)).
		Child(scroller("left", ktLeftX, rows("l"))).
		Child(scroller("right", ktRightX-ktLeftX-ktScrollerW, rows("r"))).
		IntoElement()
}

// KeyboardContainView is the keyboard-contain fixture's gpui twin: the
// nested scrollers with contain, the inner's last row tracking the
// focus handle.
func KeyboardContainView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	focus := containFocusOf(w)
	inner := gpui.Div().Flex().
		ID("inner").
		DebugSelector("inner").
		Size(gpui.Px(innerW), gpui.Px(innerH)).
		Ml(gpui.Px(innerM)).
		Mt(gpui.Px(innerM)).
		FlexCol().
		OverflowYScroll().
		FlexShrink0().
		OverscrollContain().
		Bg(gpui.Rgb(innerBg))
	for _, row := range innerRows(w, &focus) {
		inner = inner.Child(row)
	}
	outer := gpui.Div().Flex().
		ID("outer").
		DebugSelector("outer").
		Size(gpui.Px(outerW), gpui.Px(outerH)).
		Ml(gpui.Px(outerX)).
		Mt(gpui.Px(outerY)).
		FlexCol().
		OverflowYScroll().
		Bg(gpui.Rgb(outerBg)).
		Child(inner).
		Child(gpui.Div().
			ID("ospacer").
			DebugSelector("ospacer").
			Size(gpui.Px(outerW), gpui.Px(spacerH)).
			FlexShrink0().
			Bg(gpui.Rgb(spacerBg)))
	return gpui.Div().
		ID("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground)).
		Child(outer).
		IntoElement()
}
