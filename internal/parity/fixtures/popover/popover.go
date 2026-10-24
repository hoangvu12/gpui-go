//go:build windows

// Package popover is ticket 21's fixture set for the top layer and
// click-based light dismiss, each recorded in headless Chrome and
// replayed against the gpui-go TopLayer view through the browser-parity
// harness:
//
//   - popover-nested: a parent popover opened from an invoker at the
//     root, a child popover opened from an invoker INSIDE the parent
//     (the DOM's popover ancestor link), and a click inside the child
//     — the spec's dismissal-fault case. HTML popovers keep BOTH open
//     (the click relates to the child, and nothing sits above it), and
//     the port must too; Base's old per-popover mousedown-out rule
//     dismissed the parent here (research/browser-parity 06 §1). The
//     settle leaves both open, so Tier A pins the popover boxes and
//     Tier B the absence of toggle(closed) entries.
//   - popover-dismiss: the dismissal matrix — a click inside the
//     parent closes the child above it, a click on the parent's
//     invoker light-dismisses the child (not the parent, which the
//     click's own activation then toggles closed), leaving everything
//     closed. Tier B carries the toggle transcript; the closed settle
//     leaves the bare page for Tier C.
//   - popover-backdrop: the parent's ::backdrop slot — a scrim under
//     the entry's content and over everything below — with a nested
//     child above it, plus a click in the backdrop region pinning the
//     ::backdrop's own hit-test behaviour.
//
// The pages are text-free (Tier C carries no font rasterization noise)
// and the geometry is integral (exact Tier A at the 2.0 device scale).
// Both popovers are body-level siblings and the nesting is the invoker
// link — the DOM shape Chromium's popover ancestor rules describe
// (html_element.cc:3000-3010's rule 3).
package popover

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by every page and its gpui view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The invokers: 40x20 blue buttons. b1 at the root's top-left, b2
	// inside p1's content (10,10).
	invokerW = 40.0
	invokerH = 20.0

	// The parent popover: fixed at (20,60), 150x120, amber.
	parentX = 20.0
	parentY = 60.0
	parentW = 150.0
	parentH = 120.0

	// The child popover: fixed at (200,60), 100x80, green — beside the
	// parent, not inside its box (the deferred-overlay geometry whose
	// press used to dismiss the parent through Base's old rule).
	childX = 200.0
	childY = 60.0
	childW = 100.0
	childH = 80.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	invokerBlue    = 0x2563eb
	parentAmber    = 0xfde68a
	childGreen     = 0x86efac
	backdropDim    = 0x000000
)

// NestedFixture is the nested-popover light-dismiss fixture: both
// popovers stay open through a click inside the child.
var NestedFixture = parity.Fixture{
	Name:   "popover-nested",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(false),
	Steps: []parity.Step{
		// Open the parent, then the child (b2 sits inside p1 at
		// (30,70)-(70,90); its centre is (50,80)).
		parity.Click(20, 10),
		parity.Click(50, 80),
		// Click inside the child, outside the parent's box: the child
		// popover's own surface (centre (250,100)).
		parity.Click(250, 100),
	},
	ViewWithWindow: nestedView,
	BoxIDs:         []string{"b1", "b2", "p1", "p2"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "toggle"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). Text-free
		// solid boxes at integral logical coordinates: the correct view
		// is pixel-exact (measured 0.0 mean, 0.0 hot everywhere). The
		// wrong view (the invoker link dropped) closes the parent at the
		// b2 click, failing Tier A (the p1 box) and Tier B (a
		// toggle:p1 closed entry) first.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// DismissFixture is the dismissal-matrix fixture: a click inside the
// parent closes the child above it; a click on the parent's invoker
// closes the child by light dismiss and the parent by the invoker's
// own activation.
var DismissFixture = parity.Fixture{
	Name:   "popover-dismiss",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(false),
	Steps: []parity.Step{
		// Open both popovers.
		parity.Click(20, 10),
		parity.Click(50, 80),
		// Click inside the parent's content, away from b2: the child
		// hides (everything above the parent), the parent stays.
		parity.Click(120, 150),
		// Click the parent's invoker: light dismiss hides everything
		// above the parent (the child is already closed), and the
		// popovertarget activation toggles the parent closed.
		parity.Click(20, 10),
	},
	ViewWithWindow: nestedView,
	// The popovers are closed at the settle (display: none in Chrome,
	// not rendered in the port), so only the invoker's box compares;
	// the toggle transcript and the bare-page pixels carry the rest.
	BoxIDs:     []string{"b1"},
	EventKinds: []string{"mousedown", "mouseup", "click", "toggle"},
	Tolerance: parity.PixelTolerance{
		// Provisional. The settle paints the bare page (the popovers
		// closed), text-free and pixel-exact when correct.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// BackdropFixture is the backdrop-slot fixture: the parent's backdrop
// (a 40% black scrim over the page) paints under the entry's content
// and over everything below it, with the child above; a click in the
// backdrop region pins the ::backdrop's hit-test behaviour.
var BackdropFixture = parity.Fixture{
	Name:   "popover-backdrop",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   backdropPage,
	Steps: []parity.Step{
		parity.Click(20, 10),
		parity.Click(50, 80),
		// A click inside the child keeps both open (the popover-nested
		// case over a backdrop).
		parity.Click(250, 100),
		// A click in the backdrop region — the open page under the
		// scrim, outside every popover box — closes everything: the UA
		// makes a popover's ::backdrop pointer-events: none
		// (html.css:2012-2015), so the click falls through to the page
		// and light dismiss hides the whole layer.
		parity.Click(400, 200),
		// Reopen the parent: the settle leaves it open over the scrim,
		// so Tier C pins the backdrop's paint order (under the entry's
		// content, over the page).
		parity.Click(20, 10),
	},
	ViewWithWindow: backdropView,
	// p2 is closed at the settle (not rendered in the port, display:
	// none in Chrome), so it carries no comparable box.
	BoxIDs:     []string{"b1", "b2", "p1"},
	EventKinds: []string{"mousedown", "mouseup", "click", "toggle"},
	Tolerance: parity.PixelTolerance{
		// Provisional. Solid boxes and a flat scrim: pixel-exact when
		// correct (measured).
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(NestedFixture)
	parity.Register(DismissFixture)
	parity.Register(BackdropFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// page is the shared Chrome twin (withBackdrop adds the ::backdrop
// rule): two popover divs fixed in the viewport and two popovertarget
// invokers, b2 nested inside p1's content.
func page(withBackdrop bool) string {
	backdrop := ""
	if withBackdrop {
		backdrop = "\n\t#p1::backdrop {\n\t\tbackground: rgba(0, 0, 0, 0.4);\n\t}"
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
	}
	#b1 {
		left: 0px;
		top: 0px;
	}
	/* The UA stylesheet centers popovers (position: fixed; inset: 0;
	   margin: auto; width/height: fit-content; border; padding), so the
	   pages reset all of it back to plain fixed boxes at explicit
	   coordinates (html.css:1966-1977). */
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
	}` + backdrop + `
</style>
</head>
<body>
	<button id="b1" popovertarget="p1" data-id="b1"></button>
	<div id="p1" class="popover" popover data-id="p1">
		<button id="b2" popovertarget="p2" data-id="b2"></button>
	</div>
	<div id="p2" class="popover" popover data-id="p2"></div>
</body>
</html>
`
}

// backdropPage is the backdrop fixture's page: the parent popover's
// ::backdrop carries a 40% black scrim.
const backdropPage = `<!doctype html>
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
	}
	#b1 {
		left: 0px;
		top: 0px;
	}
	/* The shared page's popover UA reset, plus the parent's ::backdrop
	   scrim: the UA makes a popover's ::backdrop pointer-events: none
	   and transparent (html.css:2012-2015), so the styled background
	   shows while clicks fall through to the page — the port's plain
	   backdrop div does exactly that. */
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
	#p1::backdrop {
		background: rgba(0, 0, 0, 0.4);
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
</style>
</head>
<body>
	<button id="b1" popovertarget="p1" data-id="b1"></button>
	<div id="p1" class="popover" popover data-id="p1">
		<button id="b2" popovertarget="p2" data-id="b2"></button>
	</div>
	<div id="p2" class="popover" popover data-id="p2"></div>
</body>
</html>
`

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// popoverState is the fixture's per-window mutable state (the
// harness's discipline: the view function re-runs every frame, the
// handles must not).
type popoverState struct {
	parent, child gpui.OverlayHandle
	// noInvokerLink drops the parent's invoker link (the
	// discrimination view).
	noInvokerLink bool
}

// popoverStates keeps one state per replayed window: Replays can run
// the same fixture twice in one process with fresh windows.
var popoverStates = map[*gpui.Window]*popoverState{}

// popoverStateOf returns (creating when absent) the window's popover
// state with the layer's toggle listeners wired to the log.
func popoverStateOf(log *parity.EventLog, w *gpui.Window) *popoverState {
	if state, ok := popoverStates[w]; ok {
		return state
	}
	state := &popoverState{
		parent: gpui.NewOverlayHandle(w),
		child:  gpui.NewOverlayHandle(w),
	}
	state.parent.OnToggle(log.Toggle("p1"))
	state.child.OnToggle(log.Toggle("p2"))
	popoverStates[w] = state
	return state
}

// nestedView is the gpui-go twin of page: the invokers and the two
// top-layer popovers, nested through the invoker link (b2 inside p1's
// content).
func nestedView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return popoverRoot(log, w, popoverStateOf(log, w), false)
}

// backdropView is the gpui-go twin of backdropPage: the parent's entry
// carries the scrim backdrop.
func backdropView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return popoverRoot(log, w, popoverStateOf(log, w), true)
}

// DismissNoInvokerLink is the deliberately-wrong view for the
// discrimination test: the parent's entry does not name its invoker, so
// a click on b1 (outside every popover) relates to no popover — light
// dismiss hides the whole layer, closing the parent that the click's
// own activation then reopens. The transcript's tail gains a
// toggle:p1 open entry after the closed one, and the settle leaves the
// parent painting over the bare page the record captured.
func DismissNoInvokerLink(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := popoverStateOf(log, w)
	state.noInvokerLink = true
	return popoverRoot(log, w, state, false)
}

// popoverRoot builds the fixture's tree: the root with b1, and the
// two popovers in the top layer while their handles are open.
func popoverRoot(log *parity.EventLog, w *gpui.Window, state *popoverState, backdrop bool) gpui.AnyElement {
	root := gpui.Div().ID("root").SizeFull().Flex().Bg(gpui.Rgb(pageBackground)).
		Child(invokerBox(log, "b1", 0, 0, state.parent))
	parentLayer := gpui.TopLayer(state.parent)
	if !state.noInvokerLink {
		parentLayer = parentLayer.Invoker("b1")
	}
	if backdrop {
		parentLayer = parentLayer.Backdrop(gpui.Div().
			SizeFull().
			Bg(gpui.RgbaToHsla(0x00000066)))
	}
	if state.parent.IsOpen() {
		p1 := popoverBox(log, "p1", parentX, parentY, parentW, parentH, parentAmber)
		p1.Child(invokerBox(log, "b2", 10, 10, state.child).Absolute().Top(gpui.Px(10)).Left(gpui.Px(10)))
		root.Child(parentLayer.Child(p1))
	}
	if state.child.IsOpen() {
		root.Child(gpui.TopLayer(state.child).Invoker("b2").
			Child(popoverBox(log, "p2", childX, childY, childW, childH, childGreen)))
	}
	return root.IntoElement()
}

// invokerBox builds one 40x20 invoker: the click logs and toggles the
// overlay (the popovertarget activation; a zero handle does nothing —
// the discrimination view's unlinked invoker).
func invokerBox(log *parity.EventLog, id string, x, y float32, overlay gpui.OverlayHandle) *gpui.DivElement {
	box := gpui.Div().ID(id).DebugSelector(id).
		Absolute().Top(gpui.Px(y)).Left(gpui.Px(x)).
		W(gpui.Px(invokerW)).H(gpui.Px(invokerH)).
		Bg(gpui.Rgb(invokerBlue)).
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
		OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(id))
	if overlay != (gpui.OverlayHandle{}) {
		box.OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			log.Click(id)(e, w, app)
			overlay.Toggle()
		})
	} else {
		box.OnClick(log.Click(id))
	}
	return box
}

// popoverBox builds one popover surface: the mousedown, mouseup and
// click listeners the page's data-id instrumentation binds (and the
// hitbox they insert — the light-dismiss chain's containment).
func popoverBox(log *parity.EventLog, id string, x, y, w, h float32, background uint32) *gpui.DivElement {
	return gpui.Div().ID(id).DebugSelector(id).
		Absolute().Top(gpui.Px(y)).Left(gpui.Px(x)).
		W(gpui.Px(w)).H(gpui.Px(h)).
		Bg(gpui.Rgb(background)).
		Occlude().
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
		OnMouseUp(gpui.MouseButtonLeft, log.MouseUp(id)).
		OnClick(log.Click(id))
}
