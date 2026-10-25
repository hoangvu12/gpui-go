//go:build windows

// Package capture is browser-core ticket 11's fixture: pointer capture
// retargeting, recorded in headless Chrome and replayed against the
// gpui view. A handler on cap calls setPointerCapture on pointerdown
// (the view's OnMouseDownAll calls Window.CapturePointer), then the
// pointer drags outside cap (over the sibling) and back before the
// release outside: while captured, the moves target cap and its
// ancestors alone — the sibling under the pointer gets nothing and no
// boundary events fire — gotpointercapture fires when the pending
// capture processes at the first move (before the move itself), and
// the release fires lostpointercapture after the ups with the boundary
// events settling after it (leave cap, enter other). The exact rules
// are Chromium's (third_party/blink/renderer/core/input/
// pointer_event_manager.cc: the pending capture processes before every
// event at :1353-1365, the effective target is the capture target at
// :232-239, the up releases at :1244-1246 with the boundary events at
// :1300-1313; gotpointercapture/lostpointercapture bubble —
// core/events/pointer_event_factory.cc:287-296).
//
// The click kind is deliberately NOT instrumented: the pin fires its
// click inside the up's listener traversal (click before mouseup)
// where the browser delivers pointerup, lostpointercapture, click — a
// known divergence owned by the input bundle (D12), documented in
// docs/browser-parity-harness.md.
package capture

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the tracer
// pattern): body 640x400 flex-centering p (260x120, 20px padding,
// border-box) whose row of children cap and other (100x80 each) places
// cap at (210,160) and other at (310,160).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	pWidth  = 260.0
	pHeight = 120.0
	pPad    = 20.0

	childWidth  = 100.0
	childHeight = 80.0

	pageBackground  = 0xffffff
	pBackground     = 0xe2e8f0
	capBackground   = 0xfde68a
	otherBackground = 0xbbf7d0

	// The scripted pointer positions: the root's background (arming the
	// enter events), cap's centre, the sibling other's centre (outside
	// cap, inside p — the drag-outside step), back over cap, and the
	// release over other.
	outsideX = 20.0
	outsideY = 20.0
	capX     = 260.0
	capY     = 200.0
	otherX   = 360.0
	otherY   = 200.0
)

// Fixture is the pointer-capture fixture.
var Fixture = parity.Fixture{
	Name:   "capture-drag-outside",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		parity.Move(outsideX, outsideY),
		parity.Move(capX, capY),
		parity.Down(capX, capY, parity.ButtonLeft),
		parity.Move(otherX, otherY),
		parity.Move(capX, capY),
		parity.Up(otherX, otherY, parity.ButtonLeft),
	},
	BoxIDs: []string{"root", "p", "cap", "other"},
	EventKinds: []string{
		"pointerenter", "pointerleave",
		"pointerdown", "pointermove", "pointerup",
		"gotpointercapture", "lostpointercapture",
	},
	View: View,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run): the page
		// is four solid rectangles with no text, so the measured
		// correct-view statistics are 0.0000 across the board (verified
		// by TestPixelTolerancesKeepHeadroom); the gates keep the tracer's
		// house values with full headroom. See
		// docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.006,
	},
}

func init() { parity.Register(Fixture) }

// page is the HTML/CSS twin of the gpui view below. cap's pointerdown
// handler requests the pointer capture — the page's half of the
// fixture's one behaviour.
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
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.p {
		box-sizing: border-box;
		width: 260px;
		height: 120px;
		padding: 20px;
		display: flex;
		justify-content: flex-start;
		background: #e2e8f0;
	}
	.cap {
		box-sizing: border-box;
		width: 100px;
		height: 80px;
		background: #fde68a;
	}
	.other {
		box-sizing: border-box;
		width: 100px;
		height: 80px;
		background: #bbf7d0;
	}
</style>
</head>
<body data-id="root">
	<div class="p" data-id="p">
		<div class="cap" data-id="cap"></div>
		<div class="other" data-id="other"></div>
	</div>
</body>
<script>
	document.querySelector('[data-id="cap"]').addEventListener(
		'pointerdown', e => e.currentTarget.setPointerCapture(e.pointerId));
</script>
</body>
</html>
`

// View is the gpui-go twin of the page: a full-window root that
// flex-centers p, whose row of children holds cap and other. Every
// element logs the pointer enter/leave, down/move/up and capture
// events; cap's press requests the pointer capture through the
// window's hitbox (the port's idiom for the page's setPointerCapture).
func View(log *parity.EventLog) gpui.AnyElement {
	return tree(log, true)
}

// WithoutCapture is the deliberately-wrong view for the Tier B
// comparison: the press never requests the capture, so the transcript
// loses every got/lostpointercapture and the moves outside cap target
// the hit-test chain instead of the captured one. The view renders
// identically, so only Tier B fails.
func WithoutCapture(log *parity.EventLog) gpui.AnyElement {
	return tree(log, false)
}

// tree builds the fixture's view; withCapture selects whether cap's
// press requests the pointer capture.
func tree(log *parity.EventLog, withCapture bool) gpui.AnyElement {
	instrument := func(d *gpui.DivElement, id string) *gpui.DivElement {
		return d.
			OnPointerEnter(log.PointerEnter(id)).
			OnPointerLeave(log.PointerLeave(id)).
			OnMouseDown(gpui.MouseButtonLeft, log.PointerDown(id)).
			OnMouseMove(log.PointerMove(id)).
			OnMouseUp(gpui.MouseButtonLeft, log.PointerUp(id)).
			OnGotPointerCapture(log.GotPointerCapture(id)).
			OnLostPointerCapture(log.LostPointerCapture(id))
	}
	capEl := instrument(gpui.Div().
		ID("cap").
		DebugSelector("cap").
		Size(gpui.Px(childWidth), gpui.Px(childHeight)).
		Bg(gpui.Rgb(capBackground)), "cap")
	if withCapture {
		// The capture request: a pressed, hovered cap captures the
		// pointer (the page's pointerdown handler's twin).
		capEl = capEl.OnMouseDownAll(func(e *gpui.MouseDownEvent, phase gpui.DispatchPhase, hitbox *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
			if phase == gpui.DispatchBubble && e.Button == gpui.MouseButtonLeft && hitbox.IsHovered(w) {
				w.CapturePointer(hitbox.ID())
			}
		})
	}
	other := instrument(gpui.Div().
		ID("other").
		DebugSelector("other").
		Size(gpui.Px(childWidth), gpui.Px(childHeight)).
		Bg(gpui.Rgb(otherBackground)), "other")
	p := instrument(gpui.Div().
		ID("p").
		DebugSelector("p").
		Size(gpui.Px(pWidth), gpui.Px(pHeight)).
		P(gpui.Px(pPad)).
		Flex().
		Bg(gpui.Rgb(pBackground)), "p").
		Child(capEl.IntoElement()).
		Child(other.IntoElement())
	return instrument(gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)), "root").
		Child(p).
		IntoElement()
}
