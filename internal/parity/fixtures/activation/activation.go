//go:build windows

// Package activation is ticket 12's fixture set
// (.scratch/browser-core/issues/12-click-activation-focus-visible.md,
// spec D12): the click, keyboard-activation and focus-visible rules
// recorded in headless Chrome and replayed against the gpui-go core.
//
// The eight Chrome fixtures:
//
//   - activation-click-ancestor: a press on a, a release on b (two
//     children of p) — the click goes to the common ancestor p (and
//     bubbles to root; a and b's click listeners stay silent), after
//     every mouseup listener, and p's background flips so Tier C sees
//     the click too (Chromium mouse_event_manager.cc:358-367).
//   - activation-enter / activation-space-held / activation-space-
//     release: Enter activates between the keydown and keyup (the DOM
//     keypress, html_element.cc:4318-4324 — the recorder types "\r" so
//     the keypress exists) and its click bubbles to root; Space keeps
//     the button :active while held (the held fixture records the key
//     still down at the settle, a StepKeyDown half-press) and clicks
//     after the keyup listeners; the mousedown that focuses the button
//     is unprevented on both sides (the mouse-focus rule).
//   - focus-visible-click / focus-visible-tab / focus-visible-ctrl-s /
//     focus-visible-tab-move: the focus origin decides the
//     :focus-visible style (selector_checker.cc:3888-3901): a click
//     focus shows the focus colour but not the focus-visible colour, a
//     Tab focus shows it, a modified key (Ctrl+S) after a click does
//     not count as keyboard focus, and the ring survives a later mouse
//     move (Chromium's LastFocusType is a focus property, not an
//     input modality).
package activation

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' shared numbers. The pin's defaults the pages pin back:
// the display is flex (the pin's default) and the line height 1.618.
const (
	windowWidth  = 480.0
	windowHeight = 360.0

	btnWidth  = 120.0
	btnHeight = 60.0

	parentWidth  = 260.0
	parentHeight = 120.0
	childWidth   = 100.0
	childHeight  = 80.0

	pageBackground = 0xffffff
	boxBackground  = 0xe2e8f0
	childAColor    = 0xfca5a5
	childBColor    = 0xfda4af
	parentClicked  = 0x86efac
	focusColor     = 0x22c55e
	activeColor    = 0x93c5fd
	ringColor      = 0xf97316
)

// The input positions (logical pixels): the button and the boxes sit
// at the top-left corner of the flex column; the ancestor fixture
// presses the middle of a and releases over the middle of b.
const (
	btnClickX = 60.0
	btnClickY = 30.0

	pressAX   = 50.0
	pressAY   = 40.0
	releaseBX = 150.0
	releaseBY = 40.0

	// The move of focus-visible-tab-move: over the root's empty space,
	// off both boxes, with no button held.
	emptyMoveX = 400.0
	emptyMoveY = 300.0
)

// flatTolerance is the pixel gate for the flat-colour pages (the
// keyorder rule): the measured correct-view statistics are 0.0 mean /
// 0.0 hot (the pages are solid rectangles; outline:none removes
// Chrome's default ring), so the gates sit far below any real defect
// — a wrong focus colour, a missed flip or a stray ring measures
// 0.1-3.5 whole-image.
var flatTolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.05,
	MaxROIMeanChannelDiff: 0.25,
	MaxHotPixelFraction:   0.003,
}

// ClickAncestorFixture is the common-ancestor click: press on a,
// release on b (siblings inside p). The click's target chain is
// p → root — a and b bind click listeners too and must stay silent —
// and it fires after every mouseup listener. p's click flips its
// background, so the settled frame carries the click as pixels.
var ClickAncestorFixture = parity.Fixture{
	Name:   "activation-click-ancestor",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   ancestorPage(),
	Steps: []parity.Step{
		parity.Move(pressAX, pressAY),
		parity.Down(pressAX, pressAY, parity.ButtonLeft),
		parity.Move(releaseBX, releaseBY),
		parity.Up(releaseBX, releaseBY, parity.ButtonLeft),
	},
	BoxIDs:         []string{"root", "p", "a", "b"},
	EventKinds:     []string{"mousedown", "mouseup", "click"},
	ViewWithWindow: AncestorView(ancestorCorrect),
	Tolerance:      flatTolerance,
}

// EnterFixture is the Enter activation: click the button (focus), then
// Enter. The activation click lands between the keydown and keyup
// entries — the DOM's keypress timing — and bubbles to root.
var EnterFixture = parity.Fixture{
	Name:   "activation-enter",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   buttonPage(),
	Steps: []parity.Step{
		parity.Click(btnClickX, btnClickY),
		parity.Key("enter"),
	},
	BoxIDs:         []string{"root", "btn"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: ButtonView(EnterVariant),
	Tolerance:      flatTolerance,
}

// SpaceHeldFixture is the Space hold: click the button (focus), then
// press Space and keep it down (the StepKeyDown half-press). The
// settled frame shows the button :active — the held state — and the
// transcript ends at the keydown: no keyup, no click.
var SpaceHeldFixture = parity.Fixture{
	Name:   "activation-space-held",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   buttonPage(),
	Steps: []parity.Step{
		parity.Click(btnClickX, btnClickY),
		parity.KeyDown("space"),
	},
	BoxIDs:         []string{"root", "btn"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: ButtonView(ButtonCorrect),
	Tolerance:      flatTolerance,
}

// SpaceReleaseFixture is the Space release: the held key from
// SpaceHeldFixture released. The activation click fires after the
// keyup listeners (the keyup's default action), bubbles to root, and
// the settled frame shows the button focused but no longer active.
var SpaceReleaseFixture = parity.Fixture{
	Name:   "activation-space-release",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   buttonPage(),
	Steps: []parity.Step{
		parity.Click(btnClickX, btnClickY),
		parity.KeyDown("space"),
		parity.KeyUp("space"),
	},
	BoxIDs:         []string{"root", "btn"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: ButtonView(ButtonCorrect),
	Tolerance:      flatTolerance,
}

// FocusVisibleClickFixture is the click focus: a click focuses the box
// and shows the :focus colour, but not the :focus-visible colour —
// the focus origin is the mouse. The mousedown entries are
// unprevented on both sides (Chrome's mouse-focus rule).
var FocusVisibleClickFixture = parity.Fixture{
	Name:   "focus-visible-click",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   focusPage(false),
	Steps: []parity.Step{
		parity.Click(btnClickX, btnClickY),
	},
	BoxIDs:         []string{"root", "box"},
	EventKinds:     []string{"mousedown", "mouseup", "click"},
	ViewWithWindow: FocusView(OneBox, CorrectRing),
	Tolerance:      flatTolerance,
}

// FocusVisibleTabFixture is the keyboard focus: click a (a mouse
// focus, no ring), then Tab moves focus to b as the keydown's default
// action — the keyup entries already see b focused — and b shows the
// :focus-visible colour.
var FocusVisibleTabFixture = parity.Fixture{
	Name:   "focus-visible-tab",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   focusPage(true),
	Steps: []parity.Step{
		parity.Click(btnClickX, btnClickY),
		parity.Key("tab"),
	},
	BoxIDs:         []string{"root", "a", "b"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: FocusView(TwoBoxes, CorrectRing),
	Tolerance:      flatTolerance,
}

// FocusVisibleCtrlSFixture is the modified key: click a (a mouse
// focus), then Ctrl+S. The keydown carries the Control modifier, so it
// does not count as keyboard focus (keyboard_event_manager.cc:354-357
// skips the ctrl/alt/meta keys) — the box keeps the plain focus colour.
var FocusVisibleCtrlSFixture = parity.Fixture{
	Name:   "focus-visible-ctrl-s",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   focusPage(false),
	Steps: []parity.Step{
		parity.Click(btnClickX, btnClickY),
		parity.Key("ctrl-s"),
	},
	BoxIDs:         []string{"root", "box"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: FocusView(OneBox, CorrectRing),
	Tolerance:      flatTolerance,
}

// FocusVisibleTabMoveFixture is the ring's survival: the Tab focus of
// focus-visible-tab, then a mouse move over empty space. The
// :focus-visible colour stays — the focus origin is a focus property
// in Chromium (LastFocusType, document.cc:6068-6070), not an input
// modality a mouse move flips.
var FocusVisibleTabMoveFixture = parity.Fixture{
	Name:   "focus-visible-tab-move",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   focusPage(true),
	Steps: []parity.Step{
		parity.Click(btnClickX, btnClickY),
		parity.Key("tab"),
		parity.Move(emptyMoveX, emptyMoveY),
	},
	BoxIDs:         []string{"root", "a", "b"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: FocusView(TwoBoxes, CorrectRing),
	Tolerance:      flatTolerance,
}

func init() {
	parity.Register(ClickAncestorFixture)
	parity.Register(EnterFixture)
	parity.Register(SpaceHeldFixture)
	parity.Register(SpaceReleaseFixture)
	parity.Register(FocusVisibleClickFixture)
	parity.Register(FocusVisibleTabFixture)
	parity.Register(FocusVisibleCtrlSFixture)
	parity.Register(FocusVisibleTabMoveFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// ancestorPage is the common-ancestor click's page: two plain children
// (not focusable — the focus rules have their own fixtures) inside a
// parent whose click handler flips its background, inside the root.
func ancestorPage() string {
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
	#p {
		box-sizing: border-box;
		width: ` + f2s(parentWidth) + `px;
		height: ` + f2s(parentHeight) + `px;
		display: flex;
		background: #e2e8f0;
	}
	#p.clicked {
		background: #86efac;
	}
	#a, #b {
		box-sizing: border-box;
		width: ` + f2s(childWidth) + `px;
		height: ` + f2s(childHeight) + `px;
	}
	#a {
		background: #fca5a5;
	}
	#b {
		background: #fda4af;
	}
</style>
</head>
<body data-id="root">
	<div id="p" data-id="p">
		<div id="a" data-id="a"></div>
		<div id="b" data-id="b"></div>
	</div>
	<script>
		document.getElementById("p").addEventListener("click", function () {
			this.classList.add("clicked");
		});
	</script>
</body>
</html>
`
}

// buttonPage is the activation fixtures' page: a real button (the DOM
// element with activation behaviour) styled to match an empty gpui
// div, with :focus and :active backgrounds — the activation's settled
// states. outline:none on :focus suppresses Chrome's default ring
// (the port has none yet, ticket 48); the focus fixtures' own pages
// carry the focus-visible signal in their backgrounds.
func buttonPage() string {
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
		width: ` + f2s(btnWidth) + `px;
		height: ` + f2s(btnHeight) + `px;
		padding: 0;
		border: none;
		margin: 0;
		background: #fda4af;
	}
	#btn:focus {
		background: #22c55e;
		outline: none;
	}
	#btn:active {
		background: #93c5fd;
	}
</style>
</head>
<body data-id="root">
	<button id="btn" data-id="btn"></button>
</body>
</html>
`
}

// focusPage is the focus-visible fixtures' page: one focusable box
// (or two, for the Tab fixtures) whose :focus and :focus-visible
// backgrounds differ — the plain focus colour proves the box is
// focused, the focus-visible colour proves the ring. The
// :focus-visible rule comes after :focus so both matching shows the
// ring colour (source order, like the port's style refinement order).
func focusPage(twoBoxes bool) string {
	boxes := `	<div id="box" data-id="box" tabindex="0"></div>
`
	if twoBoxes {
		boxes = `	<div id="a" data-id="a" tabindex="0"></div>
	<div id="b" data-id="b" tabindex="0"></div>
`
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
	#box, #a, #b {
		box-sizing: border-box;
		width: ` + f2s(btnWidth) + `px;
		height: ` + f2s(btnHeight) + `px;
		background: #e2e8f0;
	}
	#box:focus, #a:focus, #b:focus {
		background: #22c55e;
		outline: none;
	}
	#box:focus-visible, #a:focus-visible, #b:focus-visible {
		background: #f97316;
	}
</style>
</head>
<body data-id="root">
` + boxes + `</body>
</html>
`
}

// f2s renders a shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// ---------------------------------------------------------------------------
// The ancestor view
// ---------------------------------------------------------------------------

// ancestorShape parameterizes the common-ancestor view: the parent's
// width (Tier A), whether p binds a click listener at all (Tier B)
// and whether that listener flips p's background (Tier C). The
// correct view is ancestorCorrect.
type ancestorShape struct {
	parentWidth float32
	pClick      bool
	flip        bool
}

var ancestorCorrect = ancestorShape{parentWidth: parentWidth, pClick: true, flip: true}

// AncestorView is the common-ancestor click's view: a, b and p all
// bind mousedown/mouseup/click (a and b's clicks must stay silent),
// and p's click flips a shared background — the click's pixel half.
// The flip state and the window key live in the closure (the keyorder
// fixtures' rule: the view function re-runs every frame, so the state
// must survive the rebuilds, and a fresh replay window resets it).
func AncestorView(shape ancestorShape) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var pClicked bool
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			pClicked = false
		}
		var background uint32 = boxBackground
		if pClicked {
			background = parentClicked
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			// The page pins the reference's 1.618 line height (body
			// font 16px/1.618); the port's default is CSS normal since
			// browser-parity D1, so the golden-ratio opt-in keeps the
			// recorded text placement.
			LineHeight(gpui.PhiLineHeight()).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("root")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("root")).
			OnClick(log.Click("root"))
		p := gpui.Div().
			ID("p").
			DebugSelector("p").
			Size(gpui.Px(shape.parentWidth), gpui.Px(parentHeight)).
			Flex().
			Bg(gpui.Rgb(background)).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("p")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("p"))
		if shape.pClick {
			if shape.flip {
				p = p.OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
					log.Click("p")(e, w, app)
					pClicked = true
				})
			} else {
				p = p.OnClick(log.Click("p"))
			}
		}
		child := func(id string, color uint32) *gpui.DivElement {
			return gpui.Div().Flex().
				ID(id).
				DebugSelector(id).
				Size(gpui.Px(childWidth), gpui.Px(childHeight)).
				Bg(gpui.Rgb(color)).
				OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
				OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(id)).
				OnClick(log.Click(id))
		}
		return root.
			Child(p.
				Child(child("a", childAColor)).
				Child(child("b", childBColor))).
			IntoElement()
	}
}

// WithoutAncestorClick is the deliberately-wrong view for the Tier B
// comparison: p has no click listener, so the transcript loses
// click:p — and the flip never happens, so Tier C also differs (the
// click is what flips the background; the discrimination test
// documents both).
func WithoutAncestorClick() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return AncestorView(ancestorShape{parentWidth: parentWidth, pClick: false})
}

// NoClickFlip is the deliberately-wrong view for the Tier C
// comparison: p's click listener logs but never flips the background,
// so the settled frame keeps the plain colour. The transcript and the
// boxes are unchanged.
func NoClickFlip() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return AncestorView(ancestorShape{parentWidth: parentWidth, pClick: true, flip: false})
}

// WrongAncestorBoxSize is the deliberately-wrong view for the Tier A
// comparison: p is 280x120 instead of 260x120, so its recorded box
// differs. The click still fires on the same chain, so Tier B keeps
// passing (the bigger box also paints, so Tier C differs with it).
func WrongAncestorBoxSize() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return AncestorView(ancestorShape{parentWidth: parentWidth + 20, pClick: true, flip: true})
}

// ---------------------------------------------------------------------------
// The activation views
// ---------------------------------------------------------------------------

// ButtonVariant selects which activation behaviour a button view pins
// (the wrong views of the discrimination tests).
type ButtonVariant uint8

const (
	// ButtonCorrect is the correct button view: focus, active and
	// click listeners all present. The enter and space fixtures share
	// it (Enter never touches the active style; Space does).
	ButtonCorrect ButtonVariant = iota
	// NoActiveVariant drops the Active style: the Space-held frame
	// shows the focus colour instead of the active colour (Tier C).
	NoActiveVariant
	// NoRootClickVariant drops root's click listener: the mouse and
	// keyboard activation clicks no longer bubble to root (Tier B).
	NoRootClickVariant
)

// EnterVariant is ButtonCorrect under the Enter fixture's name.
const EnterVariant ButtonVariant = ButtonCorrect

// ButtonView is the activation fixtures' view: a focusable div with
// focus, active and click styles. The handle lives outside the
// returned closure, keyed on the window (the keyorder fixtures'
// rule: the view function re-runs every frame, so new handles would
// break the focus identity).
func ButtonView(variant ButtonVariant) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var focus gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focus = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focus, "btn")
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			// The page pins the reference's 1.618 line height (body
			// font 16px/1.618); the port's default is CSS normal since
			// browser-parity D1, so the golden-ratio opt-in keeps the
			// recorded text placement.
			LineHeight(gpui.PhiLineHeight()).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("root")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("root")).
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root"))
		if variant != NoRootClickVariant {
			root = root.OnClick(log.Click("root"))
		}
		btn := gpui.Div().Flex().
			ID("btn").
			DebugSelector("btn").
			Size(gpui.Px(btnWidth), gpui.Px(btnHeight)).
			Bg(gpui.Rgb(childBColor)).
			TrackFocus(focus).
			// The page pins outline: none on :focus (buttonPage); the
			// port has a default focus ring now (ticket 48), so the
			// view carries the pin's twin.
			OutlineNone().
			Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
				s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
				return s
			})
		if variant != NoActiveVariant {
			btn = btn.Active(func(s gpui.StyleRefinement) gpui.StyleRefinement {
				s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(activeColor)))
				return s
			})
		}
		btn = btn.
			OnClick(log.Click("btn")).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("btn")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("btn")).
			OnKeyDown(log.KeyDown("btn")).
			OnKeyUp(log.KeyUp("btn"))
		return root.Child(btn).IntoElement()
	}
}

// ---------------------------------------------------------------------------
// The focus-visible views
// ---------------------------------------------------------------------------

// FocusBoxCount selects the focus-visible page shape: one box or two
// (the Tab fixtures).
type FocusBoxCount uint8

const (
	OneBox FocusBoxCount = iota
	TwoBoxes
)

// FocusRingMode selects whether the view styles the box's
// focus-visible state correctly or wrong (the discrimination tests).
type FocusRingMode uint8

const (
	// CorrectRing styles Focus with the focus colour and FocusVisible
	// with the ring colour — the port's twin of the page's rules.
	CorrectRing FocusRingMode = iota
	// RingOnFocus styles Focus with the ring colour: a click focus
	// shows the ring the browser does not (Tier C).
	RingOnFocus
	// NoRing drops the FocusVisible style: a Tab focus shows only the
	// focus colour (Tier C).
	NoRing
)

// FocusView is the focus-visible fixtures' view: focusable boxes with
// focus and focus-visible styles, tracked by window focus handles
// (the keyorder fixtures' rule for the handles).
func FocusView(count FocusBoxCount, mode FocusRingMode) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var focusA, focusB gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focusA = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focusA, boxIDOf(count, 0))
			if count == TwoBoxes {
				focusB = gpui.NewFocusHandle(w).TabStop(true)
				log.TrackFocus(focusB, boxIDOf(count, 1))
			}
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			// The page pins the reference's 1.618 line height (body
			// font 16px/1.618); the port's default is CSS normal since
			// browser-parity D1, so the golden-ratio opt-in keeps the
			// recorded text placement.
			LineHeight(gpui.PhiLineHeight()).
			OnClick(log.Click("root")).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("root")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("root")).
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root"))
		box := func(id string, handle gpui.FocusHandle) *gpui.DivElement {
			focusBackground, ringBackground := focusBackgroundsOf(mode)
			box := gpui.Div().Flex().
				ID(id).
				DebugSelector(id).
				Size(gpui.Px(btnWidth), gpui.Px(btnHeight)).
				Bg(gpui.Rgb(boxBackground)).
				TrackFocus(handle).
				// focusPage pins outline: none on :focus; the port has
				// a default focus ring now (ticket 48), so the view
				// carries the pin's twin.
				OutlineNone().
				Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
					s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusBackground)))
					return s
				})
			if mode != NoRing {
				box = box.FocusVisible(func(s gpui.StyleRefinement) gpui.StyleRefinement {
					s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(ringBackground)))
					return s
				})
			}
			return box.
				OnClick(log.Click(id)).
				OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
				OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(id)).
				OnKeyDown(log.KeyDown(id)).
				OnKeyUp(log.KeyUp(id))
		}
		if count == TwoBoxes {
			return root.
				Child(box("a", focusA)).
				Child(box("b", focusB)).
				IntoElement()
		}
		return root.Child(box("box", focusA)).IntoElement()
	}
}

// boxIDOf maps a box index to its element id for the focus map.
func boxIDOf(count FocusBoxCount, index int) string {
	if count == TwoBoxes {
		if index == 1 {
			return "b"
		}
		return "a"
	}
	return "box"
}

// focusBackgroundsOf returns the focus and focus-visible backgrounds
// of a ring mode: the correct pair, the ring colour in both slots
// (RingOnFocus) or the focus colour and an unused value (NoRing
// styles no FocusVisible at all).
func focusBackgroundsOf(mode FocusRingMode) (uint32, uint32) {
	switch mode {
	case RingOnFocus:
		return ringColor, ringColor
	default:
		return focusColor, ringColor
	}
}
