//go:build windows

// Package tabnav is ticket 14's fixture set
// (.scratch/browser-core/issues/14-core-tab-navigation-and-inert.md,
// spec D4): the HTML sequential tab order, the empty-press blur, the
// end-of-frame focus fixup and the Inert style, recorded in headless
// Chrome and replayed against the gpui-go core.
//
// The five Chrome fixtures:
//
//   - tabnav-order: the D4 order itself — positive tabindex values
//     first in ascending order, then the zero values in DOM order, and
//     the negative value never reached (the page's DOM order is
//     [z2:0, p2:2, z1:0, p1:1, n:-1], so the cycle is p1, p2, z2, z1).
//     Five Tabs walk it with the wrap; two Shift+Tabs walk it back.
//   - tabnav-negative: the excluded element — a click focuses the
//     tabindex=-1 box (mouse focus; a negative index is focusable but
//     not a tab stop), and Tab/Shift+Tab from it continue in TREE
//     order, not cycle order (Chromium's NextFocusableElement negative
//     arm, focus_controller.cc:1354-1366).
//   - tabnav-inert: an inert subtree is skipped by Tab, and a click on
//     it passes through to the element behind (pointer-events: none)
//     while it still paints.
//   - tabnav-removed: clicking a box removes it; the focused element
//     that disappeared at the end of the frame is blurred (the node
//     remove focus fixup) — the next Tab's keydown listener sees the
//     body, and the keyup sees the first stop.
//   - tabnav-blur: a press on empty space blurs (the mouse-focus
//     default with no focusable element under the press) — the next
//     Tab's keydown listener sees the body.
//
// The pages pin outline: none on the focusable boxes (the port has no
// default focus ring yet, ticket 48) and line-height 1.618 (the pin's
// default until D1 lands), the harness's two standard pins.
package tabnav

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' shared numbers. Where a gpui-go default differs from a
// browser today, the page pins the gpui-go value explicitly (the
// tracer's rule): the line height 1.618 and the flex column.
const (
	windowWidth  = 480.0
	windowHeight = 360.0

	boxWidth  = 120.0
	boxHeight = 60.0

	pageBackground = 0xffffff
	boxBackground  = 0xe2e8f0
	focusColor     = 0x22c55e
)

// boxCenter returns the click position of the row-th box (the flex
// column stacks 60px rows; the boxes sit at the column's left edge).
func boxCenter(row int) (float32, float32) {
	return 60.0, float32(row)*boxHeight + 30.0
}

// The empty-space click position of the blur fixture: below the last
// box, inside the window, over nothing but the root.
const (
	emptyX = 400.0
	emptyY = 300.0
)

// flatTolerance is the pixel gate for the flat-colour pages (the
// keyorder rule): the measured correct-view statistics are 0.0 mean /
// 0.0 hot (solid rectangles; outline:none removes Chrome's ring), so
// the gates sit far below any real defect — a wrong focus colour or a
// missing box measures whole-image.
var flatTolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.05,
	MaxROIMeanChannelDiff: 0.25,
	MaxHotPixelFraction:   0.003,
}

// keySteps returns n full Tab (or Shift+Tab) presses.
func keySteps(n int, shift bool) []parity.Step {
	key := "tab"
	if shift {
		key = "shift-tab"
	}
	steps := make([]parity.Step, n)
	for i := range steps {
		steps[i] = parity.Key(key)
	}
	return steps
}

// ---------------------------------------------------------------------------
// tabnav-order: the D4 order
// ---------------------------------------------------------------------------

// orderBox is one box of the order pages: id and tabindex.
type orderBox struct {
	id       string
	tabIndex int
}

// The DOM order [z2:0, p2:2, z1:0, p1:1, n:-1]: the cycle the Tab key
// must walk is p1(1), p2(2), z2(0), z1(0) — positives ascending first,
// then the zeros in DOM order — and n(-1) never.
var orderBoxes = []orderBox{
	{"z2", 0},
	{"p2", 2},
	{"z1", 0},
	{"p1", 1},
	{"n", -1},
}

// OrderFixture walks the whole cycle and both of its boundaries,
// each exactly once: five Tabs from nothing (p1, p2, z2, z1, then the
// forward pass-out — focus leaves the page to the browser UI,
// AdvanceFocusInDocumentOrder's CanTakeFocus/TakeFocus arm,
// focus_controller.cc:2216-2231, which headless records as the body
// and the port records as no handle), then five Shift+Tabs back from
// the body (z1, z2, p2, p1, then the backward pass-out). The keyup
// entries' focus field is the whole pin: every move is the DOM's
// sequential order and its wrap-free boundary, not the pin's wrapping
// one. (Each boundary is stepped once deliberately: a second forward
// pass-out from the same last element lands nondeterministically in
// headless Chrome — the DevTools input path returns the focus
// somewhere in between — so the fixture pins one pass-out per
// direction.)
var OrderFixture = parity.Fixture{
	Name:   "tabnav-order",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   boxesPage(orderBoxes),
	Steps: append(
		keySteps(5, false),
		keySteps(5, true)...,
	),
	BoxIDs:         []string{"root", "z2", "p2", "z1", "p1", "n"},
	EventKinds:     []string{"keydown", "keyup"},
	ViewWithWindow: BoxesView(orderBoxes, true),
	Tolerance:      flatTolerance,
}

// ---------------------------------------------------------------------------
// tabnav-negative: the excluded element's tree order
// ---------------------------------------------------------------------------

// negativeBoxes is the DOM order [p:1, n:-1, a:0]: from a focused n
// (a click focuses it), Tab's tree-order scan finds a (not p — the
// cycle's successor of nothing; the rule is Chromium's negative arm),
// and Shift+Tab's backward scan finds p.
var negativeBoxes = []orderBox{
	{"p", 1},
	{"n", -1},
	{"a", 0},
}

// NegativeFixture clicks the tabindex=-1 box (a mouse focus; a
// negative index is focusable), Tabs to a (the tree-order successor),
// re-focuses n and Shift+Tabs to p (the tree-order predecessor).
var NegativeFixture = parity.Fixture{
	Name:   "tabnav-negative",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   boxesPage(negativeBoxes),
	Steps: []parity.Step{
		parity.Click(boxCenter(1)),
		parity.Key("tab"),
		parity.Click(boxCenter(1)),
		parity.Key("shift-tab"),
	},
	BoxIDs:         []string{"root", "p", "n", "a"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: BoxesView(negativeBoxes, true),
	Tolerance:      flatTolerance,
}

// ---------------------------------------------------------------------------
// tabnav-inert: the inert subtree
// ---------------------------------------------------------------------------

// InertFixture walks the cycle [a, c] (the inert cover's box b is
// skipped by Tab; the third Tab passes out to the body, c being the
// last stop), then clicks b: the hit passes through the inert subtree
// to the root, whose listeners alone answer — the final frame shows
// no focus and every box painted (inertness is not visibility).
var InertFixture = parity.Fixture{
	Name:   "tabnav-inert",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   inertPage(),
	Steps: append(
		keySteps(3, false),
		parity.Click(60, 75), // b's centre: the cover row's inner half
	),
	BoxIDs:         []string{"root", "a", "cover", "b", "c"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: InertView(),
	Tolerance:      flatTolerance,
}

// InertView is the inert fixture's view factory: a, cover (Inert,
// containing b) and c as tab stops — the cover and b are inert, so
// Tab never reaches them and the click on b lands on the root (the
// hit test skips the inert hitboxes).
func InertView() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var focusA, focusCover, focusB, focusC gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focusA = gpui.NewFocusHandle(w).TabStop(true)
			focusCover = gpui.NewFocusHandle(w).TabStop(true)
			focusB = gpui.NewFocusHandle(w).TabStop(true)
			focusC = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focusA, "a")
			log.TrackFocus(focusCover, "cover")
			log.TrackFocus(focusB, "b")
			log.TrackFocus(focusC, "c")
		}
		root := instrumentedRoot(log)
		box := func(id string, handle gpui.FocusHandle) *gpui.DivElement {
			return focusBox(id, handle, log).
				OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
				OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(id)).
				OnClick(log.Click(id))
		}
		// b is the page's half-height inner box (its own colour), the
		// click target that must pass through the inert cover to the root.
		b := gpui.Div().Flex().
			ID("b").
			DebugSelector("b").
			Size(gpui.Px(boxWidth), gpui.Px(boxHeight/2)).
			Bg(gpui.Rgb(0x94a3b8)).
			TrackFocus(focusB).
			// The pages pin outline: none on the focusable boxes; the
			// port has a default focus ring now (ticket 48), so the
			// view carries the pin's twin.
			OutlineNone().
			Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
				s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
				return s
			}).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("b")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("b")).
			OnClick(log.Click("b")).
			OnKeyDown(log.KeyDown("b")).
			OnKeyUp(log.KeyUp("b"))
		// The cover is inert: its own and b's registrations are
		// suppressed and the hit test skips their hitboxes, so Tab
		// never reaches them and the click lands on the root.
		cover := box("cover", focusCover).Inert()
		return root.
			Child(box("a", focusA)).
			Child(cover.
				Child(b)).
			Child(box("c", focusC)).
			IntoElement()
	}
}

// ---------------------------------------------------------------------------
// tabnav-removed: the focus fixup
// ---------------------------------------------------------------------------

// RemovedFixture clicks b — the click focuses it (the mousedown) and
// removes it (the listener) — so the frame that drops b blurs (the
// node remove focus fixup) and the next Tab's keydown listener sees
// the body. The Tab itself starts from the removed position (the
// sequential focus navigation starting point): nothing follows b in
// tree order, so the forward search passes out and the keyup sees
// the body too; the following Shift+Tab — the starting point was
// nulled by the pass-out — restarts backward from the cycle's end,
// a, the only remaining stop, and its keyup sees a.
var RemovedFixture = parity.Fixture{
	Name:   "tabnav-removed",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   boxesPage([]orderBox{{id: "a", tabIndex: 0}, {id: "b", tabIndex: 0}}),
	Steps: []parity.Step{
		parity.Click(boxCenter(1)),
		parity.Key("tab"),
		parity.Key("shift-tab"),
	},
	BoxIDs:         []string{"root", "a"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: RemovedView(),
	Tolerance:      flatTolerance,
}

// RemovedView is the removed fixture's view factory: a and b as tab
// stops; b's click listener (after logging) drops b from the next
// frame. The removal state lives in the factory's closure, keyed on
// the window (the view function re-runs every frame, and a fresh
// replay window resets it).
func RemovedView() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var focusA, focusB gpui.FocusHandle
	var removed bool
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			removed = false
			focusA = gpui.NewFocusHandle(w).TabStop(true)
			focusB = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focusA, "a")
			log.TrackFocus(focusB, "b")
		}
		root := instrumentedRoot(log)
		a := focusBox("a", focusA, log).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("a")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("a")).
			OnClick(log.Click("a"))
		out := root.Child(a)
		if !removed {
			b := focusBox("b", focusB, log).
				OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("b")).
				OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("b")).
				OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
					log.Click("b")(e, w, app)
					removed = true
				})
			out = out.Child(b)
		}
		return out.IntoElement()
	}
}

// ---------------------------------------------------------------------------
// tabnav-blur: the press on empty space
// ---------------------------------------------------------------------------

// BlurFixture focuses a (a click), then presses the empty space below
// the boxes: nothing focusable is under the pointer, so the press
// blurs — the next Tab's keydown listener sees the body, and the keyup
// sees a again (the only stop).
var BlurFixture = parity.Fixture{
	Name:   "tabnav-blur",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   boxesPage([]orderBox{{id: "a", tabIndex: 0}}),
	Steps: []parity.Step{
		parity.Click(boxCenter(0)),
		parity.Click(emptyX, emptyY),
		parity.Key("tab"),
	},
	BoxIDs:         []string{"root", "a"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup"},
	ViewWithWindow: BoxesView([]orderBox{{id: "a", tabIndex: 0}}, true),
	Tolerance:      flatTolerance,
}

func init() {
	parity.Register(OrderFixture)
	parity.Register(NegativeFixture)
	parity.Register(InertFixture)
	parity.Register(RemovedFixture)
	parity.Register(BlurFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// boxesPage is the plain fixture pages: one flex-column body with one
// fixed box per entry, in DOM order, carrying the entry's tabindex.
// A box named b binds the removal script (tabnav-removed): b's click
// listener removes it from the DOM, exactly what the view's closure
// drops from the tree.
func boxesPage(boxes []orderBox) string {
	var rows, script string
	for _, box := range boxes {
		rows += fmt.Sprintf("\t<div id=%q data-id=%q tabindex=%d></div>\n", box.id, box.id, box.tabIndex)
		if box.id == "b" {
			script = `<script>
	document.getElementById("b").addEventListener("click", function () {
		this.remove();
	});
</script>
`
		}
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
	div[id] {
		box-sizing: border-box;
		width: ` + f2s(boxWidth) + `px;
		height: ` + f2s(boxHeight) + `px;
		background: #e2e8f0;
	}
	div[id]:focus {
		background: #22c55e;
		outline: none;
	}
</style>
</head>
<body data-id="root">
` + rows + script + `</body>
</html>
`
}

// inertPage is the inert fixture's page: a, c and the inert cover
// (tabindex=0, the inert attribute) containing b (tabindex=0, half
// height so the click position is unambiguous). Tab's cycle is a, c —
// the cover's own tabindex is neutralized by the inert attribute
// (inert elements are not focusable, so not tab stops) — and the
// click on b passes through to the body.
func inertPage() string {
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
	#a, #cover, #c {
		box-sizing: border-box;
		width: ` + f2s(boxWidth) + `px;
		height: ` + f2s(boxHeight) + `px;
		background: #e2e8f0;
	}
	#b {
		box-sizing: border-box;
		width: ` + f2s(boxWidth) + `px;
		height: ` + f2s(boxHeight/2) + `px;
		background: #94a3b8;
	}
	#a:focus, #cover:focus, #b:focus, #c:focus {
		background: #22c55e;
		outline: none;
	}
</style>
</head>
<body data-id="root">
	<div id="a" data-id="a" tabindex="0"></div>
	<div id="cover" data-id="cover" tabindex="0" inert>
		<div id="b" data-id="b" tabindex="0"></div>
	</div>
	<div id="c" data-id="c" tabindex="0"></div>
</body>
</html>
`
}

// f2s renders a shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// ---------------------------------------------------------------------------
// The shared view bodies
// ---------------------------------------------------------------------------

// BoxesView is the plain boxes pages' view factory: one tracked
// handle per box with the page's tabindex, the mouse and key
// instrumentation, and the :focus styling. htmlRank=false is the
// deliberately wrong view of the order fixture: every tabindex
// flattened to zero — the DOM order the pre-ticket pin walked.
// OrderBoxesForTest exposes the order page's box set to the
// discrimination test.
func BoxesView(boxes []orderBox, htmlRank bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return boxesView(boxes, htmlRank)
}

// OrderBoxesForTest returns the order fixture's boxes (the
// discrimination test's own copy).
func OrderBoxesForTest() []orderBox {
	return append([]orderBox(nil), orderBoxes...)
}

func boxesView(boxes []orderBox, htmlRank bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	// The handles live outside the returned closure, keyed on the
	// window (the keyorder fixtures' rule: the view function re-runs
	// every frame, and new handles would break the focus identity).
	var handles map[string]gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			handles = make(map[string]gpui.FocusHandle)
			for _, box := range boxes {
				index := int64(box.tabIndex)
				if !htmlRank {
					index = 0
				}
				handle := gpui.NewFocusHandle(w).TabStop(true).TabIndex(index)
				handles[box.id] = handle
				log.TrackFocus(handle, box.id)
			}
		}
		root := instrumentedRoot(log)
		out := root
		for _, box := range boxes {
			boxID, handle := box.id, handles[box.id]
			out = out.Child(focusBox(boxID, handle, log).
				OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(boxID)).
				OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(boxID)).
				OnClick(log.Click(boxID)))
		}
		return out.IntoElement()
	}
}

// instrumentedRoot is the mouse-and-key instrumented root column.
func instrumentedRoot(log *parity.EventLog) *gpui.DivElement {
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Bg(gpui.Rgb(pageBackground)).
		// The pages pin the reference's 1.618 line height (body font
		// 16px/1.618); the port's default is CSS normal since
		// browser-parity D1, so the golden-ratio opt-in keeps the
		// recorded text placement.
		LineHeight(gpui.PhiLineHeight()).
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("root")).
		OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("root")).
		OnClick(log.Click("root")).
		OnKeyDown(log.KeyDown("root")).
		OnKeyUp(log.KeyUp("root"))
}

// focusBox builds one focusable box: fixed size, the focus style, the
// tracked handle and the key logging.
func focusBox(id string, handle gpui.FocusHandle, log *parity.EventLog) *gpui.DivElement {
	return gpui.Div().Flex().
		ID(id).
		DebugSelector(id).
		Size(gpui.Px(boxWidth), gpui.Px(boxHeight)).
		Bg(gpui.Rgb(boxBackground)).
		TrackFocus(handle).
		// The pages pin outline: none on the focusable boxes; the
		// port has a default focus ring now (ticket 48), so the view
		// carries the pin's twin.
		OutlineNone().
		Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
			s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusColor)))
			return s
		}).
		OnKeyDown(log.KeyDown(id)).
		OnKeyUp(log.KeyUp(id))
}
