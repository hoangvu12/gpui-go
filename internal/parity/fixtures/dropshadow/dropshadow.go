//go:build windows

// Package dropshadow is ticket 57's drop-shadow fixture: a rounded
// translucent box carrying filter: drop-shadow() — the blurred alpha
// silhouette, tinted, offset, composited beneath the content (rev 16's
// filter-op chain, ticket 52) — recorded in headless Chrome and
// replayed against the gpui-go view through the browser-parity
// harness. Chromium builds FEDropShadow (filter_effect_builder.cc:
// 293-301) with sigma = blur/2, the CSS shadow rule.
//
// A NATIVE BUG the fixture works around (reported, not fixed — no
// native changes in this ticket): the group-pool lease is acquired
// with needs_half computed from the RECORD's content blur only
// (reference/native/src/renderer/draw.rs:3506), so a filter group
// whose content chain carries no blur but whose drop-shadow ops do
// gets a bucket without the half-resolution scratch, and the
// composite's needs_blur_chain check (draw.rs:5301-5316) then fails
// with ERR_DEVICE at lease.half_a_rtv.is_none(). A sub-pixel 0.1px
// content blur on both sides makes the lease carry the scratch; the
// workaround clears when the native lease accounts for the ops' blur.
//
// The page is text-free. The wrong view drops the shadow and must fail
// Tier C.
package dropshadow

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxWidth  = 140.0
	boxHeight = 90.0
	boxRadius = 16.0

	shadowOffsetX = 0.0
	shadowOffsetY = 6.0
	shadowBlur    = 8.0
	// The shadow's colour (a 40% black), the same on both sides.
	shadowAlpha = 0.4

	// The native lease workaround: a filter group whose content chain
	// carries NO blur but whose drop-shadow ops do takes its pooled
	// bucket without the half-resolution scratch and the composite
	// fails with ERR_DEVICE (the recorded native bug — see the package
	// comment). A sub-pixel content blur (0.1px) makes the lease carry
	// the scratch, so the blurred shadow composites; both sides draw
	// the same 0.1px blur, so the parity is unaffected. The class
	// clears when the native lease accounts for the ops' blur (the
	// blocker below) — then drop the blur from both sides.
	contentBlur = 0.1
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	boxFill        = 0x0ea5e9
	boxAlpha       = 0.8
)

// Fixture is the drop-shadow fixture.
var Fixture = parity.Fixture{
	Name:   "dropshadow",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{"shadowed"},
	Tolerance: parity.PixelTolerance{
		// Set from the measured correct-view statistics (the headroom
		// check's rule: 2x the measured): whole mean 0.284 / the
		// shadowed ROI 1.27 / hot 0.0001 — the blurred shadow's
		// gaussian approximation (Chrome's three box-blur passes
		// against the port's; both approximate the same gaussian, the
		// tails differ in the low bits) plus the translucent box's
		// rounding. The deliberately-wrong view (no shadow) measures
		// far past it. Verified by dropshadow_test.go.
		MaxMeanChannelDiff:    0.6,
		MaxROIMeanChannelDiff: 3.0,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(Fixture)
}

// page is the HTML twin of View.
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
	.shadowed {
		box-sizing: border-box;
		width: 140px;
		height: 90px;
		border-radius: 16px;
		background: rgba(14, 165, 233, 0.8);
		/* The drop shadow: the box's rounded alpha silhouette, blurred
		   at sigma = 8/2, offset down, 40% black, under the content.
		   The 0.1px content blur is the native lease workaround the
		   view mirrors (see the fixture's comment). */
		filter: blur(0.1px) drop-shadow(0 6px 8px rgba(0, 0, 0, 0.4));
	}
</style>
</head>
<body>
	<div class="shadowed" data-id="shadowed"></div>
</body>
</html>
`

// View is the gpui-go twin of page: the rounded translucent box with
// the drop-shadow filter.
func View(_ *parity.EventLog) gpui.AnyElement {
	return root(true)
}

// WrongNoShadowView is the deliberately-wrong view for the Tier C
// comparison: the shadow dropped.
func WrongNoShadowView(_ *parity.EventLog) gpui.AnyElement {
	return root(false)
}

// root builds the fixture's root: the shadowed (or not) box, centered.
func root(shadow bool) gpui.AnyElement {
	box := gpui.Div().
		ID("shadowed").
		DebugSelector("shadowed").
		W(gpui.Px(boxWidth)).
		H(gpui.Px(boxHeight)).
		Rounded(gpui.Px(boxRadius)).
		Bg(gpui.Rgb(boxFill).Opacity(boxAlpha)).
		// The native lease workaround (see the page's twin comment).
		Blur(contentBlur)
	if shadow {
		box = box.DropShadow(shadowOffsetX, shadowOffsetY, shadowBlur,
			gpui.Rgb(0x000000).Opacity(shadowAlpha))
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		Child(box.IntoElement()).IntoElement()
}
