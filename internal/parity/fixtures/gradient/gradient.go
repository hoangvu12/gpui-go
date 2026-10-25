//go:build windows

// Package gradient is ticket 40's fixture set for linear gradient
// backgrounds (the rev-15 CSS-semantics gradient record), each recorded
// in headless Chrome and replayed against the gpui-go view through the
// browser-parity harness:
//
//   - gradient-angles: six non-square 160x100 boxes at the CSS angles
//     0, 45, 90, 135, 180 and 37 degrees (red-500 to blue-500, stops far
//     apart so a geometry error moves clear pixels). The diagonal boxes
//     carry the ticket's named risk: the CSS gradient-line length
//     |W·sin θ| + |H·cos θ| (css-images-3 §linear-gradients, Chromium
//     EndPointsFromAngle, css_gradient_value.cc:1284-1350) must reach
//     the corners exactly, where the pinned shader squashed the
//     direction to the dominant axis and normalized by it.
//   - gradient-transparent: three gradients with transparent and
//     semi-transparent stops (premultiplied interpolation,
//     css-images-4 §coloring-gradient-line; Chromium premultiplies,
//     platform/graphics/gradient.cc:287-291): a color fading to
//     transparent must keep its hue — no grey fringe — and two stops
//     with different alphas mix premultiplied.
//   - gradient-classes: the class-built twin — the same gradients
//     authored through the Tailwind layer (bg-linear-to-r, the angle
//     form bg-linear-45, the arbitrary angle bg-linear-[117deg], and
//     the corner keyword bg-linear-to-tr, whose magic-corner angle
//     resolves against the box) against pages of the equivalent CSS.
//
// The pages are text-free, so Tier C carries no font rasterization
// noise; the residual diff is the two sides' gradient dither (the
// shader's ±2/255 triangular noise vs Skia's own dithering).
package gradient

import (
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by every page and its gpui view (the tracer's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxWidth  = 160.0
	boxHeight = 100.0
	gap       = 24.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	red500         = 0xef4444
	blue500        = 0x3b82f6
	amber400       = 0xfbbf24
	fuchsia600     = 0xc026d3
	emerald500     = 0x10b981
	cyan500        = 0x06b6d4
)

// gradientStop is one stop: a hex color at a position on the gradient
// line (0 = start, 1 = end).
func stop(hex uint32, at float32) gpui.GradientStop {
	return gpui.GradientStop{Color: gpui.Rgb(hex), Position: at}
}

// transparentStop is CSS transparent (rgba(0,0,0,0)) at a position: a
// stop the premultiplied mix fades toward without going grey.
func transparentStop(at float32) gpui.GradientStop {
	return gpui.GradientStop{Color: gpui.Rgb(0).Alpha(0), Position: at}
}

// gradientBox builds one gradient box of the fixture: the id maps the
// page's data-id, and the background is the gradient under test.
func gradientBox(id string, background gpui.Background) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(boxWidth)).
		H(gpui.Px(boxHeight)).
		Flex().
		Bg(background)
}

// fixtureRoot builds a centered white root with the given rows of
// boxes (flex column of flex rows, gap 24).
func fixtureRoot(rows ...[]*gpui.DivElement) gpui.AnyElement {
	root := gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(gap)).
		Bg(gpui.Rgb(pageBackground))
	for _, row := range rows {
		rowDiv := gpui.Div().
			Flex().
			Justify(gpui.AlignContentCenter).
			AlignItems(gpui.AlignItemsCenter).
			Gap(gpui.Px(gap))
		for _, box := range row {
			rowDiv = rowDiv.Child(box)
		}
		root = root.Child(rowDiv)
	}
	return root.IntoElement()
}

// ---------------------------------------------------------------------------
// The angles fixture
// ---------------------------------------------------------------------------

// angles is the angles fixture's six CSS angles, in box order.
var angles = []float32{0, 45, 90, 135, 180, 37}

// angleIDs are the compared box ids, in the same order as angles.
var angleIDs = []string{"grad-0", "grad-45", "grad-90", "grad-135", "grad-180", "grad-37"}

// AnglesFixture is the angle fixture: six non-square boxes, one per
// CSS angle.
var AnglesFixture = parity.Fixture{
	Name:   "gradient-angles",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   anglesPage,
	View:   AnglesView,
	BoxIDs: angleIDs,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.2131 mean image-wide /
		// 0.000 hot, per box up to 0.5750 mean (the two sides' gradient
		// dither). The deliberately-wrong view (the pin's squashed,
		// dominant-axis diagonal) measures 1.6111 image-wide and 8.1150
		// in the 45deg box's ROI, with the axis boxes (0, 90, 180deg)
		// identical to the correct view — exactly the pin's behavior.
		// Verified by gradient_test.go.
		MaxMeanChannelDiff:    0.5,
		MaxROIMeanChannelDiff: 2.5,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(AnglesFixture)
	parity.Register(TransparentFixture)
	parity.Register(ClassesFixture)
}

// anglesPage is the HTML twin of AnglesView.
const anglesPage = `<!doctype html>
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
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 24px;
	}
	.row {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 24px;
	}
	/* Every box is non-square: the diagonal angles are where the CSS
	   gradient-line length and the pin's dominant-axis normalization
	   differ. */
	.box {
		box-sizing: border-box;
		width: 160px;
		height: 100px;
		display: flex;
		/* The CSS the class layer draws: the legacy sRGB interpolation
		   (no "in oklab") Chrome applies to these stops. */
	}
</style>
</head>
<body>
	<div class="row">
		<div class="box" data-id="grad-0" style="background: linear-gradient(0deg, #ef4444 0%, #3b82f6 100%)"></div>
		<div class="box" data-id="grad-45" style="background: linear-gradient(45deg, #ef4444 0%, #3b82f6 100%)"></div>
		<div class="box" data-id="grad-90" style="background: linear-gradient(90deg, #ef4444 0%, #3b82f6 100%)"></div>
	</div>
	<div class="row">
		<div class="box" data-id="grad-135" style="background: linear-gradient(135deg, #ef4444 0%, #3b82f6 100%)"></div>
		<div class="box" data-id="grad-180" style="background: linear-gradient(180deg, #ef4444 0%, #3b82f6 100%)"></div>
		<div class="box" data-id="grad-37" style="background: linear-gradient(37deg, #ef4444 0%, #3b82f6 100%)"></div>
	</div>
</body>
</html>
`

// AnglesView is the gpui-go twin of anglesPage: six 160x100 boxes with
// the two-stop gradient at each CSS angle.
func AnglesView(_ *parity.EventLog) gpui.AnyElement {
	return anglesRoot(func(angle float32) gpui.Background {
		return gpui.LinearGradientBackground(angle, stop(red500, 0), stop(blue500, 1))
	})
}

// AnglesPinnedDiagonal is the deliberately-wrong view for the Tier C
// comparison: the PIN's diagonal geometry (rev 14 and earlier), emulated
// exactly as a gradient. The pinned linear_gradient_ratio
// (crates/gpui_render/src/shaders/common.rs:620-645 @ 254b5dbd) squashes
// the direction by the aspect ratio and normalizes the projection by the
// DOMINANT axis, so a 45deg gradient on a 160x100 box points at the
// effective bearing atan2, and the ramp saturates before the corners.
// Both are expressible as one gradient: the effective bearing as the
// angle, and the dominant-axis normalization as stop positions
// ±dominant / (2·lineLength), so the wrong view reproduces the pin's
// ramp bit for bit where the axis boxes (0, 90, 180) stay identical.
func AnglesPinnedDiagonal(_ *parity.EventLog) gpui.AnyElement {
	return anglesRoot(func(angle float32) gpui.Background {
		bearing, from, to := pinnedDiagonal(angle, boxWidth, boxHeight)
		return gpui.LinearGradientBackground(bearing, stop(red500, from), stop(blue500, to))
	})
}

// pinnedDiagonal computes the pin's effective ramp for a CSS angle on a
// w x h box: the squashed direction's bearing (CSS degrees) and the two
// stop positions that reproduce its dominant-axis normalization.
func pinnedDiagonal(angle, w, h float32) (bearing, from, to float32) {
	// The pin's direction, in screen coordinates (y down): radians
	// (angle - 90deg).
	radians := (angle - 90) * math.Pi / 180
	dirX, dirY := math.Cos(float64(radians)), math.Sin(float64(radians))
	if w > h {
		dirY *= float64(h) / float64(w)
	} else {
		dirX *= float64(w) / float64(h)
	}
	length := math.Hypot(dirX, dirY)
	dX, dY := dirX/length, dirY/length
	dominant := float64(w)
	if math.Abs(dirX) <= math.Abs(dirY) {
		dominant = float64(h)
	}
	// The effective bearing (0 up, clockwise): the CSS angle whose unit
	// direction IS (dX, dY) in screen coordinates.
	bearing = float32(math.Atan2(dX, -dY) * 180 / math.Pi)
	// The CSS line length at that bearing, and the stop positions whose
	// ramp matches (dot + dominant/2) / dominant: solving
	// (t - p0) / (p1 - p0) = dot/dominant + 0.5 with t = dot/line + 0.5
	// gives p1 - p0 = dominant/line and p0 = (line - dominant) / (2·line)
	// — the axis angles (dominant = line) keep stops 0 and 1, identical
	// to the CSS ramp, exactly as the pin drew them.
	line := math.Abs(float64(w)*math.Sin(float64(bearing)*math.Pi/180)) +
		math.Abs(float64(h)*math.Cos(float64(bearing)*math.Pi/180))
	spread := dominant / line
	start := 0.5 * (1 - spread)
	return bearing, float32(start), float32(start + spread)
}

// anglesRoot builds the angles fixture's root: the six boxes, their
// backgrounds built by background.
func anglesRoot(background func(angle float32) gpui.Background) gpui.AnyElement {
	var rows [2][]*gpui.DivElement
	for i, angle := range angles {
		box := gradientBox(angleIDs[i], background(angle))
		rows[i/3] = append(rows[i/3], box)
	}
	return fixtureRoot(rows[0], rows[1])
}

// ---------------------------------------------------------------------------
// The transparent-stops fixture
// ---------------------------------------------------------------------------

// TransparentFixture is the premultiplied-interpolation fixture:
// gradients fading to transparent keep their hue (no grey fringe), and
// stops with different alphas mix premultiplied.
var TransparentFixture = parity.Fixture{
	Name:   "gradient-transparent",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   transparentPage,
	View:   TransparentView,
	BoxIDs: []string{"fade-down", "fade-right", "fade-alpha"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.0946 mean image-wide /
		// 0.000 hot, per box up to 0.5360 mean. The deliberately-wrong
		// view (the fade going through black) measures 4.4479
		// image-wide / 35.3017 in the fade-down ROI / 0.4917 hot.
		// Verified by gradient_test.go.
		MaxMeanChannelDiff:    0.25,
		MaxROIMeanChannelDiff: 1.5,
		MaxHotPixelFraction:   0.002,
	},
}

// transparentPage is the HTML twin of TransparentView.
const transparentPage = `<!doctype html>
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
		gap: 24px;
	}
	.box {
		box-sizing: border-box;
		width: 160px;
		height: 100px;
		display: flex;
	}
</style>
</head>
<body>
	<!-- A color fading to transparent must keep its hue on the white
	     page (premultiplied interpolation): the naive mix would take it
	     through the transparent stop's rgb (black). -->
	<div class="box" data-id="fade-down" style="background: linear-gradient(180deg, #ef4444 0%, transparent 100%)"></div>
	<div class="box" data-id="fade-right" style="background: linear-gradient(90deg, transparent 0%, #ef4444 100%)"></div>
	<!-- Two stops with different alphas: the premultiplied mix weights
	     the semi-transparent blue's channels by its alpha on the way. -->
	<div class="box" data-id="fade-alpha" style="background: linear-gradient(90deg, rgba(59, 130, 246, 0.6) 0%, #ef4444 100%)"></div>
</body>
</html>
`

// TransparentView is the gpui-go twin of transparentPage.
func TransparentView(_ *parity.EventLog) gpui.AnyElement {
	return fixtureRoot([]*gpui.DivElement{
		gradientBox("fade-down", gpui.LinearGradientBackground(180, stop(red500, 0), transparentStop(1))),
		gradientBox("fade-right", gpui.LinearGradientBackground(90, transparentStop(0), stop(red500, 1))),
		gradientBox("fade-alpha", gpui.LinearGradientBackground(90,
			gpui.GradientStop{Color: gpui.Rgb(blue500).Alpha(0.6), Position: 0},
			stop(red500, 1))),
	})
}

// TransparentThroughBlack is the deliberately-wrong view for the Tier C
// comparison: the fades to transparent end at a half-alpha black stop,
// the visible signature of the naive non-premultiplied mix (the color
// darkening toward the transparent end, which lerps toward
// transparent's rgb of black). The naive curve itself is NOT
// expressible as a two-stop premultiplied gradient — its premultiplied
// form is quadratic in t, a straight line in t only when the alphas are
// equal — so this view pins the signature instead of the curve; the
// fixture itself catches a shader regression either way (the correct
// view's pixels change).
func TransparentThroughBlack(_ *parity.EventLog) gpui.AnyElement {
	dark := gpui.GradientStop{Color: gpui.Rgb(0).Alpha(0.5), Position: 1}
	return fixtureRoot([]*gpui.DivElement{
		gradientBox("fade-down", gpui.LinearGradientBackground(180, stop(red500, 0), dark)),
		gradientBox("fade-right", gpui.LinearGradientBackground(90, gpui.GradientStop{Color: gpui.Rgb(0).Alpha(0.5), Position: 0}, stop(red500, 1))),
		gradientBox("fade-alpha", gpui.LinearGradientBackground(90,
			gpui.GradientStop{Color: gpui.Rgb(blue500).Alpha(0.6), Position: 0},
			stop(red500, 1))),
	})
}

// ---------------------------------------------------------------------------
// The class-built fixture
// ---------------------------------------------------------------------------

// ClassesFixture is the Tailwind-layer twin: the same gradients the
// angle fixture draws, authored with the classes against pages of the
// equivalent CSS.
var ClassesFixture = parity.Fixture{
	Name:   "gradient-classes",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   classesPage,
	View:   ClassesView,
	BoxIDs: []string{"class-to-r", "class-45", "class-117", "class-corner"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.1450 mean image-wide /
		// 0.000 hot, per box up to 0.6014 mean. The deliberately-wrong
		// view (the corner keyword drawn as the square's 45deg instead
		// of the magic corner) measures 0.4479 image-wide / 5.4079 in
		// the corner box's ROI. Verified by gradient_test.go.
		MaxMeanChannelDiff:    0.35,
		MaxROIMeanChannelDiff: 2.5,
		MaxHotPixelFraction:   0.002,
	},
}

// classesPage is the HTML twin of ClassesView: the CSS each class draws
// (the legacy sRGB interpolation Chrome applies to these stops — for
// the named and numeric forms this is the documented divergence from
// Tailwind v4, which emits "in oklab"; the arbitrary angle form
// matches Tailwind exactly).
const classesPage = `<!doctype html>
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
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 24px;
	}
	.row {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 24px;
	}
	.box {
		box-sizing: border-box;
		width: 160px;
		height: 100px;
		display: flex;
	}
</style>
</head>
<body>
	<div class="row">
		<!-- bg-linear-to-r from-red-500 to-blue-500 -->
		<div class="box" data-id="class-to-r" style="background: linear-gradient(to right, #ef4444 0%, #3b82f6 100%)"></div>
		<!-- bg-linear-45 from-amber-400 to-fuchsia-600 -->
		<div class="box" data-id="class-45" style="background: linear-gradient(45deg, #fbbf24 0%, #c026d3 100%)"></div>
	</div>
	<div class="row">
		<!-- bg-linear-[117deg] from-emerald-500 to-cyan-500 -->
		<div class="box" data-id="class-117" style="background: linear-gradient(117deg, #10b981 0%, #06b6d4 100%)"></div>
		<!-- bg-linear-to-tr from-red-500 to-blue-500: the corner keyword
		     resolves the magic-corner angle against the box (about
		     32deg for 160x100), not the square's 45deg. -->
		<div class="box" data-id="class-corner" style="background: linear-gradient(to top right, #ef4444 0%, #3b82f6 100%)"></div>
	</div>
</body>
</html>
`

// ClassesView is the gpui-go twin of classesPage: the four gradients
// authored through the Tailwind class layer.
func ClassesView(_ *parity.EventLog) gpui.AnyElement {
	return classesRoot(func(box *gpui.DivElement) *gpui.DivElement { return box })
}

// ClassesCornerAsDiagonal is the deliberately-wrong view for the Tier C
// comparison: the corner keyword drawn as the square's 45deg — the
// naive reading of "to top right" that ignores the magic-corner rule —
// on a non-square box. Only the corner box's ROI fails.
func ClassesCornerAsDiagonal(_ *parity.EventLog) gpui.AnyElement {
	return classesRoot(func(box *gpui.DivElement) *gpui.DivElement {
		if box == nil {
			return box
		}
		return box.Bg(gpui.LinearGradientBackground(45, stop(red500, 0), stop(blue500, 1)))
	})
}

// classesRoot builds the class fixture's root: the four class-built
// boxes, with the corner box replaced by corner when non-nil.
func classesRoot(corner func(*gpui.DivElement) *gpui.DivElement) gpui.AnyElement {
	classed := func(id, classes string) *gpui.DivElement {
		return gpui.Div().
			ID(id).
			DebugSelector(id).
			W(gpui.Px(boxWidth)).
			H(gpui.Px(boxHeight)).
			Flex().
			Class(classes)
	}
	cornerBox := classed("class-corner", "bg-linear-to-tr from-red-500 to-blue-500")
	if corner != nil {
		cornerBox = corner(cornerBox)
	}
	return fixtureRoot(
		[]*gpui.DivElement{
			classed("class-to-r", "bg-linear-to-r from-red-500 to-blue-500"),
			classed("class-45", "bg-linear-45 from-amber-400 to-fuchsia-600"),
		},
		[]*gpui.DivElement{
			classed("class-117", "bg-linear-[117deg] from-emerald-500 to-cyan-500"),
			cornerBox,
		},
	)
}
