//go:build windows

// Package outline is ticket 48's fixture set
// (.scratch/browser-core/issues/48-outline-and-focus-ring.md):
// the CSS outline (width, offset, colour, style) painted above the
// content, and Chrome's default auto focus ring on focus-visible
// elements, recorded in headless Chrome and replayed against the
// gpui-go core.
//
// The five Chrome fixtures:
//
//   - outline-offset: five boxes with solid outlines — a positive
//     offset (the ring 6-9px outside the border box), a zero offset
//     (flush with the border edge), a negative offset (the band
//     3-6px INSIDE the border box, css-ui-4 §outline-offset), a 1px
//     thin ring, a 6px wide ring, and a ring over a rounded border
//     (the outline corners follow the border radius grown by the
//     offset, Chromium ComputeCornerRadii).
//   - outline-dashed: a dashed outline — the port lowers it to the
//     native scene's dashed border quad, whose dash pattern is the
//     pin's own (Chromium strokes the outline's center path with
//     Skia dashes); the fixture calibrates what that divergence
//     costs in pixels.
//   - outline-focusring-tab: a Tab focus draws Chrome's default
//     auto ring — one 2px #101010 stroke centred on the border box
//     (1px inside, 1px outside) with 3px-rounded corners, the look
//     the recorded stable Chrome 155 paints (the record's
//     ComputedScript pins the resolved -webkit-focus-ring-color;
//     Chromium main has since refreshed it to a two-tone ring, a
//     recorded divergence toward the record).
//   - outline-focusring-click: a click focus draws NO ring (the
//     :focus-visible rule, ticket 12's focus origin) — the box
//     focuses (the :focus background flips) but stays clean.
//   - outline-focusring-none: outline-none opts the Tab-focused box
//     out of the ring.
package outline

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' shared numbers. The pin's defaults the pages pin back:
// the display is flex (the pin's default) and the line height 1.618.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	ringWindowWidth  = 480.0
	ringWindowHeight = 360.0

	// The outline fixture's boxes: two flex columns of three 140x70
	// boxes, 24px page padding and 16px gaps — every outline (at most
	// 9px of ink outside the border box) stays clear of its
	// neighbours.
	boxWidth  = 140.0
	boxHeight = 70.0

	// The focus fixtures' focusable box.
	focusBoxWidth  = 160.0
	focusBoxHeight = 80.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	boxBackground  = 0xe2e8f0
	focusColor     = 0x22c55e

	outlineGreen  = 0x22c55e
	outlineBlue   = 0x0ea5e9
	outlineOrange = 0xf97316
	outlineRed    = 0xdc2626
	outlineViolet = 0x7c3aed
	outlineTeal   = 0x0f766e
	outlineSlate  = 0x334155
)

// The outline geometry each box carries, as one table both sides are
// built from (the tracer's discipline): width, offset and the
// outline's CSS colour.
type outlineBox struct {
	id     string
	width  float32
	offset float32
	color  uint32
	// rounded is the box's border radius (0: square corners — the
	// outline keeps them sharp, FloatRoundedRect::Radii::Outset's
	// zero preservation).
	rounded float32
}

var offsetBoxes = []outlineBox{
	{"pos", 3, 6, outlineGreen, 0},
	{"zero", 2, 0, outlineBlue, 0},
	{"neg", 3, -6, outlineOrange, 0},
	{"thin", 1, 4, outlineRed, 0},
	{"wide", 6, 2, outlineViolet, 0},
	{"rounded", 3, 4, outlineTeal, 16},
}

// OffsetFixture is the outline geometry fixture: positive, zero and
// negative offsets, 1px/2px/3px/6px widths, distinct colours and a
// ring over a rounded border. The outline never affects layout (Tier
// A compares the boxes exactly), and it paints above the content.
var OffsetFixture = parity.Fixture{
	Name:   "outline-offset",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   offsetPage(),
	View:   OffsetView,
	BoxIDs: []string{"root", "pos", "zero", "neg", "thin", "wide", "rounded"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set
		// from the measured correct-view statistics: whole mean
		// 0.0032 / hot 0.0 (the pages are flat rectangles whose
		// outline bands land on the same device pixels), the rounded
		// box's corners 0.0175 from the antialiased arc edges. The
		// gates sit above 2x those; a wrong offset, width or colour
		// moves a whole band and fails by orders of magnitude.
		MaxMeanChannelDiff:    0.01,
		MaxROIMeanChannelDiff: 0.05,
		MaxHotPixelFraction:   0.003,
	},
}

// DashedFixture is the dashed outline: the port lowers
// outline-style: dashed to the native dashed border quad.
var DashedFixture = parity.Fixture{
	Name:   "outline-dashed",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   dashedPage(),
	View:   DashedView,
	BoxIDs: []string{"root", "dashed"},
	Tolerance: parity.PixelTolerance{
		// The port's dashed border pattern is the pin's own dash
		// rhythm (DefaultBorderDashedLength 2x the width, gap 1x),
		// where Chromium strokes the outline's center path with
		// Skia's dashes; the measured whole-image diff 0.0704 / hot
		// 0.00058 is that divergence (the dashed band sits outside
		// the border box, so the box ROI is flat) — the dashes land
		// within a pixel of Chrome's, with the corners differing in
		// phase. The gates sit above 2x the measurement.
		MaxMeanChannelDiff:    0.15,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.002,
	},
}

// The focus fixtures' shared position: a point inside the box (its
// upper-left quadrant — the box sits at the flex column's 24px
// padding, so the click lands well inside its border box).
const (
	focusClickX = focusBoxWidth / 2
	focusClickY = focusBoxHeight / 2
)

// FocusRingTabFixture is the default ring: one Tab press from body
// focus moves focus to the box as the keydown's default action, and
// the focused :focus-visible box draws Chrome's auto ring — the
// page pins nothing about the outline, so the UA rule is the whole
// record.
var FocusRingTabFixture = parity.Fixture{
	Name:   "outline-focusring-tab",
	Window: parity.Size{Width: ringWindowWidth, Height: ringWindowHeight},
	HTML:   focusPage(false),
	Steps:  []parity.Step{parity.Key("tab")},
	BoxIDs: []string{"root", "box"},
	EventKinds: []string{
		"keydown", "keyup",
	},
	ViewWithWindow: FocusView(false),
	// The recorder's headless Chrome prefers the dark scheme by
	// default (the record's ComputedScript pins it), and the ring's
	// recorded look is the browser's, so the record emulates the light
	// scheme — a real light-mode browser page, matching the replay's
	// plain test application.
	EmulatedMedia: []parity.EmulatedMediaFeature{
		{Name: "prefers-color-scheme", Value: "light"},
	},
	// ComputedScript pins the record's focus-ring provenance: the
	// media and used colour schemes (matchMedia and the light-dark()
	// probe — the ring's look is the browser's own, so the scheme it
	// rendered under is part of the record's identity), and the UA
	// rule's resolved outline on the focused box (-webkit-focus-ring-
	// color → rgb(16,16,16) in this Chrome).
	ComputedScript: `JSON.stringify({
		scheme: matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light",
		colorScheme: getComputedStyle(document.documentElement).colorScheme,
		lightDark: getComputedStyle(document.getElementById("ld")).backgroundColor,
		outline: getComputedStyle(document.getElementById("box")).outlineStyle + " " + getComputedStyle(document.getElementById("box")).outlineWidth + " " + getComputedStyle(document.getElementById("box")).outlineColor,
	})`,
	Tolerance: parity.PixelTolerance{
		// Calibrated from the measured correct view: whole mean
		// 0.0003 / hot 0.00001, the focused box's ROI 0.0044 /
		// 0.00008 (the ring's antialiased inner edge overlaps the
		// box, and the rounded corners' arcs differ by fractions of a
		// device pixel). The gates sit above 2x those; a missing or
		// mis-coloured ring fails by orders of magnitude (the
		// discrimination test proves it).
		MaxMeanChannelDiff:    0.002,
		MaxROIMeanChannelDiff: 0.02,
		MaxHotPixelFraction:   0.0005,
	},
}

// FocusRingClickFixture is the mouse-focus rule: the click focuses
// the box (the :focus background flips), but the focus origin is the
// mouse, so :focus-visible does not match and NO ring draws.
var FocusRingClickFixture = parity.Fixture{
	Name:   "outline-focusring-click",
	Window: parity.Size{Width: ringWindowWidth, Height: ringWindowHeight},
	HTML:   focusPage(false),
	Steps: []parity.Step{
		parity.Click(focusClickX, focusClickY),
	},
	BoxIDs: []string{"root", "box"},
	EventKinds: []string{
		"mousedown", "mouseup", "click",
	},
	ViewWithWindow: FocusView(false),
	Tolerance: parity.PixelTolerance{
		// Flat rectangles: the correct view sits at 0.0/0.0 — any
		// stray ring fails the gate.
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

// FocusRingNoneFixture is the opt-out: the box's outline: none beats
// the UA rule, so the Tab focus draws no ring (the :focus background
// still flips).
var FocusRingNoneFixture = parity.Fixture{
	Name:   "outline-focusring-none",
	Window: parity.Size{Width: ringWindowWidth, Height: ringWindowHeight},
	HTML:   focusPage(true),
	Steps:  []parity.Step{parity.Key("tab")},
	BoxIDs: []string{"root", "box"},
	EventKinds: []string{
		"keydown", "keyup",
	},
	ViewWithWindow: FocusView(true),
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.003,
	},
}

func init() {
	parity.Register(OffsetFixture)
	parity.Register(DashedFixture)
	parity.Register(FocusRingTabFixture)
	parity.Register(FocusRingClickFixture)
	parity.Register(FocusRingNoneFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// offsetPage is the outline geometry page: two flex columns of three
// outlined boxes. The pages pin no outline on the root: the root has
// no outline.
func offsetPage() string {
	css := ""
	boxes := map[string]string{}
	for _, b := range offsetBoxes {
		extra := ""
		if b.rounded > 0 {
			extra = fmt.Sprintf("\n\t\tborder-radius: %spx;\n", f2s(float64(b.rounded)))
		}
		boxes[b.id] = fmt.Sprintf("\t<div id=%q data-id=%q></div>\n", b.id, b.id)
		css += fmt.Sprintf("\t#%s {\n\t\tbox-sizing: border-box;\n\t\twidth: %spx;\n\t\theight: %spx;\n\t\tbackground: #%06x;\n\t\toutline: %spx solid #%06x;\n\t\toutline-offset: %spx;%s\t}\n",
			b.id, f2s(boxWidth), f2s(boxHeight), boxBackground,
			f2s(float64(b.width)), b.color, f2s(float64(b.offset)), extra)
	}
	column := func(first, second, third string) string {
		return "\t<div class=\"col\">\n" + boxes[first] + boxes[second] + boxes[third] + "\t</div>\n"
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
		flex-direction: row;
		padding: 24px;
		box-sizing: border-box;
		gap: 16px;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	.col {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
` + css + `</style>
</head>
<body data-id="root">
` + column("pos", "zero", "neg") + column("thin", "wide", "rounded") + `</body>
</html>
`
}

// dashedPage is the dashed outline's page: one 200x100 box with
// outline: 2px dashed, offset 4px.
func dashedPage() string {
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
		padding: 24px;
		box-sizing: border-box;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	#dashed {
		box-sizing: border-box;
		width: 200px;
		height: 100px;
		background: #` + h6s(boxBackground) + `;
		outline: 2px dashed #` + h6s(outlineSlate) + `;
		outline-offset: 4px;
	}
</style>
</head>
<body data-id="root">
	<div id="dashed" data-id="dashed"></div>
</body>
</html>
`
}

// focusPage is the focus fixtures' page: one focusable box whose
// :focus background flip proves the focus landed (the activation
// fixtures' pattern). The page pins no outline (the UA
// :focus-visible ring is the record), or opts the box out with
// outline: none for the opt-out fixture.
func focusPage(outlineNone bool) string {
	none := ""
	if outlineNone {
		none = "\n\t\toutline: none;"
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
		width: ` + f2s(ringWindowWidth) + `px;
		height: ` + f2s(ringWindowHeight) + `px;
		display: flex;
		flex-direction: column;
		padding: 24px;
		box-sizing: border-box;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	#box {
		box-sizing: border-box;
		width: ` + f2s(focusBoxWidth) + `px;
		height: ` + f2s(focusBoxHeight) + `px;
		background: #` + h6s(boxBackground) + `;` + none + `
	}
	#box:focus {
		background: #` + h6s(focusColor) + `;
	}
</style>
</head>
<body data-id="root">
	<div id="box" data-id="box" tabindex="0"></div>
	<div id="ld" style="width: 1px; height: 1px; visibility: hidden; background: light-dark(#ff0000, #0000ff)"></div>
</body>
</html>
`
}

// f2s renders a shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// h6s renders a palette constant as a 6-digit hex colour.
func h6s(v uint32) string {
	return fmt.Sprintf("%06x", v)
}

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// OffsetView is the outline geometry fixture's view: the six boxes
// with their outline styles. The outline paints above the content and
// never affects layout, so the boxes compare exactly (Tier A) and the
// bands are the pixels (Tier C).
func OffsetView(log *parity.EventLog) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexRow().
		P(gpui.Px(24)).
		Gap(gpui.Px(16)).
		Bg(gpui.Rgb(pageBackground)).
		// The page pins the reference's 1.618 line height (body font
		// 16px/1.618); the port's default is CSS normal since
		// browser-parity D1, so the golden-ratio opt-in keeps the
		// recorded text placement.
		LineHeight(gpui.PhiLineHeight())
	box := func(b outlineBox) *gpui.DivElement {
		d := gpui.Div().Flex().
			ID(b.id).
			DebugSelector(b.id).
			Size(gpui.Px(boxWidth), gpui.Px(boxHeight)).
			Bg(gpui.Rgb(boxBackground)).
			Outline(b.width).
			OutlineOffset(b.offset).
			OutlineColor(gpui.Rgb(b.color))
		if b.rounded > 0 {
			d = d.Rounded(gpui.Px(b.rounded))
		}
		return d
	}
	column := func(ids ...string) *gpui.DivElement {
		col := gpui.Div().Flex().FlexCol().Gap(gpui.Px(16))
		for _, id := range ids {
			for _, b := range offsetBoxes {
				if b.id == id {
					col = col.Child(box(b))
				}
			}
		}
		return col
	}
	return root.
		Child(column("pos", "zero", "neg")).
		Child(column("thin", "wide", "rounded")).
		IntoElement()
}

// DashedView is the dashed outline's view.
func DashedView(log *parity.EventLog) gpui.AnyElement {
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		P(gpui.Px(24)).
		Bg(gpui.Rgb(pageBackground)).
		// The page's 1.618 line-height pin, mirrored (D1: the port's
		// default is CSS normal; PhiLineHeight is the opt-in).
		LineHeight(gpui.PhiLineHeight()).
		Child(gpui.Div().Flex().
			ID("dashed").
			DebugSelector("dashed").
			Size(gpui.Px(200), gpui.Px(100)).
			Bg(gpui.Rgb(boxBackground)).
			Outline(2).
			OutlineOffset(4).
			OutlineColor(gpui.Rgb(outlineSlate)).
			OutlineDashed()).
		IntoElement()
}

// FocusView is the focus fixtures' view: a focusable box with the
// :focus background flip, tracked by a window focus handle (the
// keyorder fixtures' rule: the view function re-runs every frame, so
// the handle lives in the closure keyed on the window — new handles
// would break the focus identity, and a fresh replay window resets
// it). outlineNone adds the opt-out; the default ring is the style
// system's own (:focus-visible materializes it — no explicit outline
// anywhere).
func FocusView(outlineNone bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var focus gpui.FocusHandle
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			focus = gpui.NewFocusHandle(w).TabStop(true)
			log.TrackFocus(focus, "box")
		}
		box := gpui.Div().Flex().
			ID("box").
			DebugSelector("box").
			Size(gpui.Px(focusBoxWidth), gpui.Px(focusBoxHeight)).
			Bg(gpui.Rgb(boxBackground)).
			TrackFocus(focus).
			Focus(gpui.Styles().Bg(gpui.Rgb(focusColor))).
			OnKeyDown(log.KeyDown("box")).
			OnKeyUp(log.KeyUp("box")).
			OnClick(log.Click("box")).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("box")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("box"))
		if outlineNone {
			box = box.OutlineNone()
		}
		return gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			P(gpui.Px(24)).
			Bg(gpui.Rgb(pageBackground)).
			// The page's 1.618 line-height pin, mirrored (D1: the port's
			// default is CSS normal; PhiLineHeight is the opt-in).
			LineHeight(gpui.PhiLineHeight()).
			OnKeyDown(log.KeyDown("root")).
			OnKeyUp(log.KeyUp("root")).
			OnClick(log.Click("root")).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("root")).
			OnMouseUp(gpui.MouseButtonLeft, log.MouseUp("root")).
			Child(box).
			IntoElement()
	}
}
