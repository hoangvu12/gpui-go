//go:build windows

// Package keyorder is ticket 09's fixture set (default actions after
// key listeners, .scratch/browser-core/issues/09-default-actions-after-
// listeners.md, spec D5): key events recorded in headless Chrome and
// replayed against the gpui-go core's default actions.
//
// The six Chrome fixtures:
//
//   - keyorder-tab / keyorder-tab-prevented: Tab moves focus as the
//     default action, after the key listeners (the root's listener
//     still sees the old focus), and a prevented keydown keeps focus
//     where it was.
//   - keyorder-scroll / keyorder-scroll-prevented: ArrowDown scrolls
//     the scroller containing focus by Chromium's 40 px line step, and
//     a prevented keydown leaves it untouched.
//   - keyorder-activate / keyorder-activate-prevented: Space clicks
//     the focused button after every keyup listener, and a prevented
//     keydown suppresses the click.
//   - keyorder-binding-order: with the window's key dispatch order set
//     to the browser order, the keydown listeners run before a bound
//     action (the action fires between the keydown and keyup entries
//     without adding a transcript entry, so the record still matches;
//     the discrimination test asserts the order).
//
// The page-side details that make the two sides agree are commented
// where they appear; docs/browser-parity-harness.md documents the
// harness extension (keydown/keyup kinds, the key and focus fields,
// data-events, the key step's down+up pair). The fixtures bind no
// mousedown listeners — a choice the pages made while the pin's
// mouse-down focus transfer still reported defaultPrevented
// (browser-core ticket 12 fixed that: the transfer's innermost-wins
// signal is window-internal now and the mousedown stays unprevented;
// ticket 12's activation fixtures instrument mousedown to pin it).
// The records stay valid either way: nothing here keys on the
// mousedown's default-prevented state.
package keyorder

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' shared numbers. Where a gpui-go default differs from a
// browser today, the page pins the gpui-go value explicitly so both
// sides draw the same thing (the tracer's rule): the line height is
// 1.618 (the pin's default, before D1 lands) and the display is flex.
const (
	windowWidth  = 480.0
	windowHeight = 360.0

	tabBoxWidth  = 120.0
	tabBoxHeight = 60.0

	scrollerWidth  = 200.0
	scrollerHeight = 200.0
	contentHeight  = 1000.0
	targetHeight   = 50.0

	pageBackground = 0xffffff
	boxBackground  = 0xe2e8f0
	targetColor    = 0xfca5a5
	fillerColor    = 0x94a3b8
	focusColor     = 0x22c55e
)

// The click positions (logical pixels): the tab boxes and the button
// sit at the top-left corner of the flex column, and the scroll
// fixture's target is the first child inside the scroller.
const (
	tabClickX = 60.0
	tabClickY = 30.0
)

// keyOrderAction is the binding-order fixture's bound action. It has
// no transcript side effect on purpose: Chrome has no key bindings, so
// the record can only carry listener-observable facts; the
// discrimination tests assert the action fired (and when) through the
// bindingOrderActions counter.
var keyOrderAction = gpui.DefineUnitAction[keyOrderPayload]("keyorder::Test")

type keyOrderPayload struct{}

// bindingOrderActions counts the binding-order action's dispatches
// (the fixture's own replays and the discrimination tests read it;
// BindingOrderActionsFired resets and reports it).
var bindingOrderActions int

// BindingOrderActionsFired reports how many times the binding-order
// fixture's bound action has dispatched since the last call (the
// counter resets).
func BindingOrderActionsFired() int {
	fired := bindingOrderActions
	bindingOrderActions = 0
	return fired
}

// Fixture is the tab fixture: click a, then Tab. The keydown entries
// see focus a (the listeners ran first); the keyup entries see focus b
// (the default action moved it between the two), and the final frame
// shows b focused.
var Fixture = parity.Fixture{
	Name:   "keyorder-tab",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   tabPage(false),
	Steps: []parity.Step{
		parity.Click(tabClickX, tabClickY),
		parity.Key("tab"),
	},
	BoxIDs:         []string{"a", "b", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: TabView(false),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run); set from
		// the measured correct-view statistics, ~2x headroom (see
		// keyorder_test.go).
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.006,
	},
}

// PreventedFixture is the tab fixture with a prevented keydown: the
// root's keydown listener (the page's capturing body listener, the
// view's KeyDownPreventDefault) calls preventDefault, focus stays on a
// and the final frame shows a focused.
var PreventedFixture = parity.Fixture{
	Name:   "keyorder-tab-prevented",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   tabPage(true),
	Steps: []parity.Step{
		parity.Click(tabClickX, tabClickY),
		parity.Key("tab"),
	},
	BoxIDs:         []string{"a", "b", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: TabView(true),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). Measured
		// correct-view statistics: 0.0 mean / 0.0 hot everywhere (the
		// pages are flat colors; the outline:none pin removes Chrome's
		// focus ring, ticket 48 owns the port's default ring). The gates
		// sit far below any real defect: a wrong focus color, a missed
		// scroll or a stray ring measures 0.1-3.5 whole-image.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// ScrollFixture is the keyboard-scroll fixture: click the target
// inside the scroller (focus), then two ArrowDowns. Each arrow scrolls
// Chromium's 40 px line step (cc/input/scroll_utils.h:20), so the
// content ends at -80 and the final frame shows the filler row.
var ScrollFixture = parity.Fixture{
	Name:   "keyorder-scroll",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   scrollPage(false),
	Steps: []parity.Step{
		parity.Click(100, 25),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
	},
	BoxIDs:         []string{"scroller", "content", "target", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: ScrollView(false),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). Measured
		// correct-view statistics: 0.0 mean / 0.0 hot everywhere (the
		// pages are flat colors; the outline:none pin removes Chrome's
		// focus ring, ticket 48 owns the port's default ring). The gates
		// sit far below any real defect: a wrong focus color, a missed
		// scroll or a stray ring measures 0.1-3.5 whole-image.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// ScrollPreventedFixture is the scroll fixture with a prevented
// keydown: no scroll, the content stays at 0.
var ScrollPreventedFixture = parity.Fixture{
	Name:   "keyorder-scroll-prevented",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   scrollPage(true),
	Steps: []parity.Step{
		parity.Click(100, 25),
		parity.Key("arrowdown"),
	},
	BoxIDs:         []string{"scroller", "content", "target", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: ScrollView(true),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). Measured
		// correct-view statistics: 0.0 mean / 0.0 hot everywhere (the
		// pages are flat colors; the outline:none pin removes Chrome's
		// focus ring, ticket 48 owns the port's default ring). The gates
		// sit far below any real defect: a wrong focus color, a missed
		// scroll or a stray ring measures 0.1-3.5 whole-image.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// ActivateFixture is the activation fixture: click the button (focus),
// then Space. The click fires after every keyup listener — the DOM's
// keyup default action — and the final frame shows the button focused.
var ActivateFixture = parity.Fixture{
	Name:   "keyorder-activate",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   activatePage(false),
	Steps: []parity.Step{
		parity.Click(60, 30),
		parity.Key("space"),
	},
	BoxIDs:         []string{"btn", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: ActivateView(false),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). Measured
		// correct-view statistics: 0.0 mean / 0.0 hot everywhere (the
		// pages are flat colors; the outline:none pin removes Chrome's
		// focus ring, ticket 48 owns the port's default ring). The gates
		// sit far below any real defect: a wrong focus color, a missed
		// scroll or a stray ring measures 0.1-3.5 whole-image.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// ActivatePreventedFixture is the activation fixture with a prevented
// keydown: the pending activation never arms, so the keyup clicks
// nothing.
var ActivatePreventedFixture = parity.Fixture{
	Name:   "keyorder-activate-prevented",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   activatePage(true),
	Steps: []parity.Step{
		parity.Click(60, 30),
		parity.Key("space"),
	},
	BoxIDs:         []string{"btn", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: ActivateView(true),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). Measured
		// correct-view statistics: 0.0 mean / 0.0 hot everywhere (the
		// pages are flat colors; the outline:none pin removes Chrome's
		// focus ring, ticket 48 owns the port's default ring). The gates
		// sit far below any real defect: a wrong focus color, a missed
		// scroll or a stray ring measures 0.1-3.5 whole-image.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// BindingOrderFixture is the browser-order flag fixture: the window's
// key dispatch order is the browser order and a key binding is bound
// to "j". The keydown listeners still see the key (they ran before the
// binding; under the GPUI shape the binding would consume it and the
// transcript would lose the keydown entries — the discrimination test
// asserts exactly that).
var BindingOrderFixture = parity.Fixture{
	Name:   "keyorder-binding-order",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   bindingOrderPage(),
	Steps: []parity.Step{
		parity.Click(tabClickX, tabClickY),
		parity.Key("j"),
	},
	BoxIDs:         []string{"a", "root"},
	EventKinds:     []string{"click", "keydown", "keyup"},
	ViewWithWindow: BindingOrderView(true),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). Measured
		// correct-view statistics: 0.0 mean / 0.0 hot everywhere (the
		// pages are flat colors; the outline:none pin removes Chrome's
		// focus ring, ticket 48 owns the port's default ring). The gates
		// sit far below any real defect: a wrong focus color, a missed
		// scroll or a stray ring measures 0.1-3.5 whole-image.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

func init() {
	parity.Register(Fixture)
	parity.Register(PreventedFixture)
	parity.Register(ScrollFixture)
	parity.Register(ScrollPreventedFixture)
	parity.Register(ActivateFixture)
	parity.Register(ActivatePreventedFixture)
	parity.Register(BindingOrderFixture)
}

// ---------------------------------------------------------------------------
// The tab fixtures
// ---------------------------------------------------------------------------

// tabPage is the tab fixtures' page: a flex-column body (the pin's
// flex default) with two tabindex=0 boxes whose :focus background is
// the focus probe. The prevented variant adds a capturing keydown
// listener that calls preventDefault — the DOM listener whose default
// suppression the fixture checks.
func tabPage(prevent bool) string {
	preventScript := ""
	if prevent {
		preventScript = "\n\tdocument.addEventListener('keydown', e => e.preventDefault(), true);"
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
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
		flex-direction: column;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	.box {
		box-sizing: border-box;
		width: ` + f2s(tabBoxWidth) + `px;
		height: ` + f2s(tabBoxHeight) + `px;
		background: #e2e8f0;
	}
	.box:focus {
		background: #22c55e;
		outline: none;
	}
</style>
</head>
<body data-id="root">
	<div class="box" data-id="a" tabindex="0"></div>
	<div class="box" data-id="b" tabindex="0"></div>
	<script>` + preventScript + `
	</script>
</body>
</html>
`
}

// tabView is the tab fixtures' view: two focusable, tab-stop boxes
// tracked by window focus handles, with mousedown/click/keydown/keyup
// logging (the root keydown logs and, in the prevented variant,
// preventDefaults). The handles are created once in the closure; the
// rebuild uses them again.
func TabView(prevent bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handles live outside the returned closure: the view function
	// re-runs every frame, and new handles every frame would break the
	// focus identity (the click's focus, the tab stops and the recorded
	// focus field all key on it). They are keyed on the window: a
	// Replay of the same fixture in the same process (TestFixtures and
	// the tolerance headroom check both do) opens a fresh window, and
	// the old window-scoped handles would be foreign to it.
	var focusA, focusB gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focusA = gpui.NewFocusHandle(w).TabStop(true)
			focusB = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focusA, "a")
			log.TrackFocus(focusB, "b")
		}
		rootKeyDown := log.KeyDown("root")
		if prevent {
			rootKeyDown = log.KeyDownPreventDefault("root")
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			OnClick(log.Click("root")).
			OnKeyDown(rootKeyDown).
			OnKeyUp(log.KeyUp("root"))
		box := func(id string, handle gpui.FocusHandle) *gpui.DivElement {
			return gpui.Div().
				ID(id).
				DebugSelector(id).
				Size(gpui.Px(tabBoxWidth), gpui.Px(tabBoxHeight)).
				Bg(gpui.Rgb(boxBackground)).
				TrackFocus(handle).
				Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
					s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
					return s
				}).
				OnClick(log.Click(id)).
				OnKeyDown(log.KeyDown(id)).
				OnKeyUp(log.KeyUp(id))
		}
		return root.
			Child(box("a", focusA)).
			Child(box("b", focusB)).
			IntoElement()
	}
}

// ---------------------------------------------------------------------------
// The scroll fixtures
// ---------------------------------------------------------------------------

// scrollPage is the scroll fixtures' page: a 200x200 scroller over a
// 1000px content whose first 50px row (the click target) is red and
// whose second row is the filler the scrolled frame shows. The page
// pins scrollbar-width: none — the port draws no scrollbars and its
// overflow does not reserve a gutter (browser-core ticket 36 owns
// scrollbar gutter) — and the recorder disables Chrome's smooth
// scrolling (the port scrolls instantly; ticket 45 owns smoothness).
func scrollPage(prevent bool) string {
	preventScript := ""
	if prevent {
		preventScript = "\n\tdocument.addEventListener('keydown', e => e.preventDefault(), true);"
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
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
		flex-direction: column;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	#scroller {
		box-sizing: border-box;
		width: ` + f2s(scrollerWidth) + `px;
		height: ` + f2s(scrollerHeight) + `px;
		overflow-y: scroll;
		scrollbar-width: none;
		background: #e2e8f0;
	}
	#content {
		box-sizing: border-box;
		width: ` + f2s(scrollerWidth) + `px;
		height: ` + f2s(contentHeight) + `px;
		display: flex;
		flex-direction: column;
	}
	#target {
		width: ` + f2s(scrollerWidth) + `px;
		height: ` + f2s(targetHeight) + `px;
		background: #fca5a5;
		outline: none;
	}
	#filler {
		width: ` + f2s(scrollerWidth) + `px;
		height: ` + f2s(targetHeight) + `px;
		background: #94a3b8;
	}
</style>
</head>
<body data-id="root">
	<div id="scroller" data-id="scroller">
		<div id="content" data-id="content">
			<div id="target" data-id="target" tabindex="0"></div>
			<div id="filler"></div>
		</div>
	</div>
	<script>` + preventScript + `
	</script>
</body>
</html>
`
}

// scrollView is the scroll fixtures' view: the scroller tracks a
// scroll handle (the keyboard-scroll default action moves its offset
// cell) and the target is the focusable child the click focuses.
func ScrollView(prevent bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handle lives outside the returned closure, keyed on the
	// window (TabView's note).
	var focusTarget gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focusTarget = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focusTarget, "target")
		}
		rootKeyDown := log.KeyDown("root")
		if prevent {
			rootKeyDown = log.KeyDownPreventDefault("root")
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			OnClick(log.Click("root")).
			OnKeyDown(rootKeyDown).
			OnKeyUp(log.KeyUp("root"))
		scroller := gpui.Div().
			ID("scroller").
			DebugSelector("scroller").
			Size(gpui.Px(scrollerWidth), gpui.Px(scrollerHeight)).
			Bg(gpui.Rgb(boxBackground)).
			OverflowYScroll().
			OnClick(log.Click("scroller")).
			OnKeyDown(log.KeyDown("scroller")).
			OnKeyUp(log.KeyUp("scroller"))
		content := gpui.Div().
			ID("content").
			DebugSelector("content").
			Size(gpui.Px(scrollerWidth), gpui.Px(contentHeight)).
			FlexCol().
			OnClick(log.Click("content")).
			OnKeyDown(log.KeyDown("content")).
			OnKeyUp(log.KeyUp("content"))
		target := gpui.Div().
			ID("target").
			DebugSelector("target").
			Size(gpui.Px(scrollerWidth), gpui.Px(targetHeight)).
			Bg(gpui.Rgb(targetColor)).
			TrackFocus(focusTarget).
			OnClick(log.Click("target")).
			OnKeyDown(log.KeyDown("target")).
			OnKeyUp(log.KeyUp("target"))
		filler := gpui.Div().
			Size(gpui.Px(scrollerWidth), gpui.Px(targetHeight)).
			Bg(gpui.Rgb(fillerColor))
		return root.
			Child(scroller.
				Child(content.
					Child(target).
					Child(filler))).
			IntoElement()
	}
}

// ---------------------------------------------------------------------------
// The activation fixtures
// ---------------------------------------------------------------------------

// activatePage is the activation fixtures' page: a real button (the
// DOM element with activation behavior; a plain div would not activate
// on Space) styled to match an empty gpui div. The root carries
// data-events="keydown,keyup": the page's shape predates browser-core
// ticket 12 (the keyboard activation click did not bubble then, so the
// root's click was not instrumented); the click now bubbles on both
// sides, and ticket 12's activation fixtures instrument it — this
// record stays as recorded, and the shape keeps matching because the
// view also binds no root click listener.
func activatePage(prevent bool) string {
	preventScript := ""
	if prevent {
		preventScript = "\n\tdocument.addEventListener('keydown', e => e.preventDefault(), true);"
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
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
		flex-direction: column;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	#btn {
		appearance: none;
		display: block;
		box-sizing: border-box;
		width: 120px;
		height: 60px;
		padding: 0;
		border: none;
		margin: 0;
		background: #fda4af;
	}
	#btn:focus {
		background: #22c55e;
		outline: none;
	}
</style>
</head>
<body data-id="root" data-events="keydown,keyup">
	<button id="btn" data-id="btn"></button>
	<script>` + preventScript + `
	</script>
</body>
</html>
`
}

// activateView is the activation fixtures' view: a focusable div with
// a click listener (the port's activation is the focused element's
// OnClick), a keyup listener on the root that must run before the
// click, and the mousedown/click/keydown/keyup logging.
func ActivateView(prevent bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handle lives outside the returned closure, keyed on the
	// window (TabView's note).
	var focus gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focus = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focus, "btn")
		}
		rootKeyDown := log.KeyDown("root")
		if prevent {
			rootKeyDown = log.KeyDownPreventDefault("root")
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			OnKeyDown(rootKeyDown).
			OnKeyUp(log.KeyUp("root"))
		button := gpui.Div().
			ID("btn").
			DebugSelector("btn").
			Size(gpui.Px(120), gpui.Px(60)).
			Bg(gpui.Rgb(0xfda4af)).
			TrackFocus(focus).
			Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
				s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
				return s
			}).
			OnClick(log.Click("btn")).
			OnKeyDown(log.KeyDown("btn")).
			OnKeyUp(log.KeyUp("btn"))
		return root.Child(button).IntoElement()
	}
}

// ---------------------------------------------------------------------------
// The binding-order fixture
// ---------------------------------------------------------------------------

// bindingOrderPage is the binding-order fixture's page: one focusable
// box; "j" has no default action in Chrome (nothing but keydown and
// keyup listeners see it).
func bindingOrderPage() string {
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
		width: ` + f2s(tabBoxWidth) + `px;
		height: ` + f2s(tabBoxHeight) + `px;
		background: #e2e8f0;
	}
	#box:focus {
		background: #22c55e;
		outline: none;
	}
</style>
</head>
<body data-id="root">
	<div id="box" data-id="a" tabindex="0"></div>
</body>
</html>
`
}

// bindingOrderView is the binding-order fixture's view: the window's
// key dispatch order is set to the browser order (once, before the
// first key dispatch) and "j" is bound to keyOrderAction whose action
// listener does nothing (the transcript must stay Chrome-shaped; the
// discrimination tests assert the action fired and when).
func BindingOrderView(browserOrder bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handle, the order flag and the binding live outside the
	// returned closure, keyed on the window (TabView's note): the flag
	// and the keymap entry are set per replay window, before the first
	// key dispatch. browserOrder false is the discrimination view: the
	// GPUI shape, where the binding consumes the key before the
	// listeners.
	var focusA gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focusA = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focusA, "a")
			if browserOrder {
				if w.KeyDispatchOrder() != gpui.BindingsBeforeListeners {
					panic("keyorder: the binding-order fixture expects the default GPUI order before the first build")
				}
				w.SetKeyDispatchOrder(gpui.ListenersBeforeBindings)
			}
			app.BindKeys(gpui.NewKeyBinding("j", keyOrderAction.Box(keyOrderPayload{}), nil))
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			OnClick(log.Click("root")).
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root")).
			OnAction(keyOrderAction, func(*keyOrderPayload, *gpui.Window, *gpui.App) { bindingOrderActions++ })
		box := gpui.Div().
			ID("a").
			DebugSelector("a").
			Size(gpui.Px(tabBoxWidth), gpui.Px(tabBoxHeight)).
			Bg(gpui.Rgb(boxBackground)).
			TrackFocus(focusA).
			Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
				s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
				return s
			}).
			OnClick(log.Click("a")).
			OnKeyDown(log.KeyDown("a")).
			OnKeyUp(log.KeyUp("a"))
		return root.Child(box).IntoElement()
	}
}

// f2s renders a shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
