//go:build windows

// The box-and-pixel fixtures of browser-core ticket 56 (transform.go's
// package comment): one Tier-A-exact box+pixel fixture per transform
// kind, the transform-origin pair, and the mid-flight transition
// sample. Each element's geometry is chosen so the TRANSFORMED box is
// representable in float32 (the Tier A comparison is exact): integer
// translations, a dyadic scale factor, a quarter-turn rotation and a
// 45-degree skew whose tangent is exactly 1.
package transform

import (
	"sync"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The box fixtures' shared geometry: a 80x40 box at (40, 30) on the
// white window (windowWidth x windowHeight), one per page, with the
// page carrying one labelled element under test plus the root.
const (
	boxW = 80.0
	boxH = 40.0
	boxX = 40.0
	boxY = 30.0

	boxBg = 0x00aa00
)

// TranslateFixture: translate(24px, 12px) — the offset fast path (no
// scene group; the element's own bounds and children shift). The
// translated box (64, 42, 80, 40) is integer-exact.
var TranslateFixture = parity.Fixture{
	Name:   "transform-translate",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   translatePage,
	Steps:  nil,
	BoxIDs: []string{"root", "box"},
	View:   TranslateView,
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.002,
	},
}

// translatePage is the HTML twin of TranslateView.
const translatePage = `<!doctype html>
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
		width: 320px;
		height: 240px;
	}
	#box {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 30px;
		width: 80px;
		height: 40px;
		background: #00aa00;
		transform: translate(24px, 12px);
	}
</style>
</head>
<body data-id="root">
	<div id="box" data-id="box"></div>
</body>
</html>
`

// TranslateView is the gpui-go twin of translatePage.
func TranslateView(_ *parity.EventLog) gpui.AnyElement {
	return boxView(func(d *gpui.DivElement) *gpui.DivElement {
		return d.TranslatePx(24, 12)
	})
}

// ScaleFixture: scale(1.75) about the default centre — the 80x40 box
// (centre (80, 50)) becomes 140x70 at (10, 15) (all dyadic, exact in
// float32).
var ScaleFixture = parity.Fixture{
	Name:   "transform-scale",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   scalePage,
	Steps:  nil,
	BoxIDs: []string{"root", "box"},
	View:   ScaleView,
	Tolerance: parity.PixelTolerance{
		// Provisional, measured from the correct view: whole mean
		// 0.1165 hot 0.00272; the box ROI mean 0.9134 hot 0.02133 — the
		// upscale's bilinear edge against Chrome's rasterization (the
		// interior is solid and exact; the translate and rotate fixtures
		// measure 0.0000, so the residue is the resampling edge alone).
		// The gates sit at 2x those.
		MaxMeanChannelDiff:    0.25,
		MaxROIMeanChannelDiff: 1.9,
		MaxHotPixelFraction:   0.045,
	},
}

// scalePage is the HTML twin of ScaleView.
const scalePage = `<!doctype html>
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
		width: 320px;
		height: 240px;
	}
	#box {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 30px;
		width: 80px;
		height: 40px;
		background: #00aa00;
		transform: scale(1.75);
	}
</style>
</head>
<body data-id="root">
	<div id="box" data-id="box"></div>
</body>
`

// ScaleView is the gpui-go twin of scalePage.
func ScaleView(_ *parity.EventLog) gpui.AnyElement {
	return boxView(func(d *gpui.DivElement) *gpui.DivElement {
		return d.Scale(1.75, 1.75)
	})
}

// RotateFixture: rotate(90deg) about the default centre — the 80x40
// box becomes 40x80 at (60, 10). A quarter turn is Tier-A exact (the
// trig residues are ~1e-16, far below the float32 ULP).
var RotateFixture = parity.Fixture{
	Name:   "transform-rotate",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   rotatePage,
	Steps:  nil,
	BoxIDs: []string{"root", "box"},
	View:   RotateView,
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.002,
	},
}

// rotatePage is the HTML twin of RotateView.
const rotatePage = `<!doctype html>
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
		width: 320px;
		height: 240px;
	}
	#box {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 30px;
		width: 80px;
		height: 40px;
		background: #00aa00;
		transform: rotate(90deg);
	}
</style>
</head>
<body data-id="root">
	<div id="box" data-id="box"></div>
</body>
`

// RotateView is the gpui-go twin of rotatePage.
func RotateView(_ *parity.EventLog) gpui.AnyElement {
	return boxView(func(d *gpui.DivElement) *gpui.DivElement {
		return d.Rotate(90)
	})
}

// SkewFixture: skewX(45deg) — tan(45deg) is exactly 1 in the engine
// and rounds to 1 through Chrome's double, so the parallelogram's AABB
// (40, 30, 120, 40) is exact.
var SkewFixture = parity.Fixture{
	Name:   "transform-skew",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   skewPage,
	Steps:  nil,
	BoxIDs: []string{"root", "box"},
	View:   SkewView,
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.002,
	},
}

// skewPage is the HTML twin of SkewView.
const skewPage = `<!doctype html>
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
		width: 320px;
		height: 240px;
	}
	#box {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 30px;
		width: 80px;
		height: 40px;
		background: #00aa00;
		transform: skewX(45deg);
	}
</style>
</head>
<body data-id="root">
	<div id="box" data-id="box"></div>
</body>
`

// SkewView is the gpui-go twin of skewPage.
func SkewView(_ *parity.EventLog) gpui.AnyElement {
	return boxView(func(d *gpui.DivElement) *gpui.DivElement {
		return d.SkewX(45)
	})
}

// OriginFixture: two 80x40 rotations of the same layout box, one about
// the CSS initial centre origin (the 40x80 box at (60, 10)) and one
// about the top-left corner (transform-origin: 0 0; the 40x80 box at
// (40, 30)) — the pair pins the origin resolution against Chrome.
var OriginFixture = parity.Fixture{
	Name:   "transform-origin",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   originPage,
	Steps:  nil,
	BoxIDs: []string{"root", "centre", "corner"},
	View:   OriginView,
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.002,
	},
}

// originPage is the HTML twin of OriginView: two quarter-turned boxes.
const originPage = `<!doctype html>
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
		width: 320px;
		height: 240px;
	}
	.centre, .corner {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 30px;
		width: 80px;
		height: 40px;
	}
	.centre {
		background: #00aa00;
		transform: rotate(90deg);
	}
	.corner {
		background: #0066ff;
		transform: rotate(90deg);
		transform-origin: 0 0;
	}
</style>
</head>
<body data-id="root">
	<div class="centre" data-id="centre"></div>
	<div class="corner" data-id="corner"></div>
</body>
</html>
`

// OriginView is the gpui-go twin of originPage.
func OriginView(_ *parity.EventLog) gpui.AnyElement {
	centre := gpui.Div().
		ID("centre").
		DebugSelector("centre").
		Absolute().Left(gpui.Px(boxX)).Top(gpui.Px(boxY)).
		Size(gpui.Px(boxW), gpui.Px(boxH)).
		Bg(gpui.Rgb(boxBg)).
		Rotate(90)
	corner := gpui.Div().
		ID("corner").
		DebugSelector("corner").
		Absolute().Left(gpui.Px(boxX)).Top(gpui.Px(boxY)).
		Size(gpui.Px(boxW), gpui.Px(boxH)).
		Bg(gpui.Rgb(0x0066ff)).
		Rotate(90).
		TransformOrigin(gpui.DefinitePx(0), gpui.DefinitePx(0))
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Bg(gpui.Rgb(pageBackground)).
		Child(centre.IntoElement()).
		Child(corner.IntoElement()).
		IntoElement()
}

// boxView builds the single-element box fixtures' root: the labelled
// 80x40 box at (40, 30) with the transform the fixture applies.
func boxView(apply func(*gpui.DivElement) *gpui.DivElement) gpui.AnyElement {
	box := apply(gpui.Div().
		ID("box").
		DebugSelector("box").
		Absolute().Left(gpui.Px(boxX)).Top(gpui.Px(boxY)).
		Size(gpui.Px(boxW), gpui.Px(boxH)).
		Bg(gpui.Rgb(boxBg)))
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Bg(gpui.Rgb(pageBackground)).
		Child(box.IntoElement()).
		IntoElement()
}

// ---------------------------------------------------------------------------
// transform-transition: the mid-flight sample
// ---------------------------------------------------------------------------

// The transition fixture's geometry: two boxes, one scaling (1 ->
// 1.75, Tier A: the 200ms sample is 1.375, dyadic-exact) and one
// rotating (0 -> 90deg, Tier C only: cos(45deg) is not float32-exact).
const (
	transW = 40.0
	transH = 40.0
	transX = 40.0
	transY = 30.0

	rotBoxX = 160.0

	transDuration   = 400 * time.Millisecond
	transSampleMs   = 200
	transScaleFrom  = 1.0
	transScaleTo    = 1.75
	transRotateFrom = 0.0
	transRotateTo   = 90.0

	// The go button's absolute position.
	goX = 40.0
	goY = 120.0
)

// TransitionFixture: a click toggles both boxes' transform targets;
// the 200ms Wait samples the mid-transition state (scale 1.375 and
// rotate 45deg), the transition fixture's paused-animation-seek
// pattern.
var TransitionFixture = parity.Fixture{
	Name:   "transform-transition",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   transitionPage,
	Steps: []parity.Step{
		parity.Click(goX+transW/2, goY+transH/2),
		parity.Wait(transSampleMs),
	},
	EventKinds: []string{"click"},
	BoxIDs:     []string{"root", "scale"},
	View:       TransitionView,
	Tolerance: parity.PixelTolerance{
		// Provisional, measured from the correct view at the 200ms
		// sample: whole mean 0.0589 hot 0.00001; the scale ROI (the
		// mid-flight 1.375 upscale) mean 0.9926 hot 0.00033 — the same
		// resampling edge as the scale fixture. The gates sit at 2x.
		MaxMeanChannelDiff:    0.13,
		MaxROIMeanChannelDiff: 2.0,
		MaxHotPixelFraction:   0.002,
	},
}

// transitionPage is the HTML twin of TransitionView.
const transitionPage = `<!doctype html>
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
		width: 320px;
		height: 240px;
	}
	#go {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 120px;
		width: 40px;
		height: 40px;
		background: #cccccc;
	}
	#scale, #rotate {
		box-sizing: border-box;
		position: absolute;
		top: 30px;
		width: 40px;
		height: 40px;
	}
	#scale {
		left: 40px;
		background: #00aa00;
		transform: scale(1);
		transition: transform 400ms linear;
	}
	#rotate {
		left: 160px;
		background: #0066ff;
		transform: rotate(0deg);
		transition: transform 400ms linear;
	}
	body.on #scale { transform: scale(1.75); }
	body.on #rotate { transform: rotate(90deg); }
</style>
</head>
<body data-id="root">
	<div id="scale" data-id="scale"></div>
	<div id="rotate" data-id="rotate"></div>
	<div id="go" data-id="go"></div>
	<script>
		document.getElementById("go").addEventListener("click", () => {
			document.body.classList.toggle("on");
		});
	</script>
</body>
</html>
`

// transitionState is the fixture's per-run toggle, keyed on the run's
// event log so the second Replay starts off again (the transition
// fixture's pattern).
type transitionState struct {
	on bool
}

var (
	transMu    sync.Mutex
	transByLog = map[*parity.EventLog]*transitionState{}
)

// TransitionView is the gpui-go twin of transitionPage.
func TransitionView(log *parity.EventLog) gpui.AnyElement {
	s := transitionStateOf(log)
	scaleBox := gpui.Div().
		ID("scale").
		DebugSelector("scale").
		Absolute().Left(gpui.Px(transX)).Top(gpui.Px(transY)).
		Size(gpui.Px(transW), gpui.Px(transH)).
		Bg(gpui.Rgb(boxBg)).
		Transition(gpui.NewTransitionRule(gpui.TransitionTransform, transDuration).WithEasing(gpui.EasingLinear()))
	if s.on {
		scaleBox = scaleBox.Scale(transScaleTo, transScaleTo)
	} else {
		scaleBox = scaleBox.Scale(transScaleFrom, transScaleFrom)
	}
	rotateBox := gpui.Div().
		ID("rotate").
		DebugSelector("rotate").
		Absolute().Left(gpui.Px(rotBoxX)).Top(gpui.Px(transY)).
		Size(gpui.Px(transW), gpui.Px(transH)).
		Bg(gpui.Rgb(0x0066ff)).
		Transition(gpui.NewTransitionRule(gpui.TransitionTransform, transDuration).WithEasing(gpui.EasingLinear()))
	if s.on {
		rotateBox = rotateBox.Rotate(transRotateTo)
	} else {
		rotateBox = rotateBox.Rotate(transRotateFrom)
	}
	goDiv := gpui.Div().
		ID("go").
		DebugSelector("go").
		Absolute().Left(gpui.Px(goX)).Top(gpui.Px(goY)).
		Size(gpui.Px(transW), gpui.Px(transH)).
		Bg(gpui.Rgb(0xcccccc)).
		OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			s.on = !s.on
			log.Append("click", "go")
			w.Refresh()
		})
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Bg(gpui.Rgb(pageBackground)).
		OnClick(log.Click("root")).
		Child(scaleBox.IntoElement()).
		Child(rotateBox.IntoElement()).
		Child(goDiv.IntoElement()).
		IntoElement()
}

// transitionStateOf returns the run's state, creating it off.
func transitionStateOf(log *parity.EventLog) *transitionState {
	transMu.Lock()
	defer transMu.Unlock()
	s, ok := transByLog[log]
	if !ok {
		s = &transitionState{}
		transByLog[log] = s
	}
	return s
}

// WrongTransitionDurationView is the deliberately-wrong view for the
// discrimination test: the scale rule runs 800ms instead of 400ms, so
// at the 200ms sample the box is a quarter of the way (1.1875: 47.5px
// wide) where the record holds the halfway value (1.375: 55px).
func WrongTransitionDurationView(log *parity.EventLog) gpui.AnyElement {
	s := transitionStateOf(log)
	scaleBox := gpui.Div().
		ID("scale").
		DebugSelector("scale").
		Absolute().Left(gpui.Px(transX)).Top(gpui.Px(transY)).
		Size(gpui.Px(transW), gpui.Px(transH)).
		Bg(gpui.Rgb(boxBg)).
		Transition(gpui.NewTransitionRule(gpui.TransitionTransform, 2*transDuration).WithEasing(gpui.EasingLinear()))
	if s.on {
		scaleBox = scaleBox.Scale(transScaleTo, transScaleTo)
	} else {
		scaleBox = scaleBox.Scale(transScaleFrom, transScaleFrom)
	}
	rotateBox := gpui.Div().
		ID("rotate").
		DebugSelector("rotate").
		Absolute().Left(gpui.Px(rotBoxX)).Top(gpui.Px(transY)).
		Size(gpui.Px(transW), gpui.Px(transH)).
		Bg(gpui.Rgb(0x0066ff)).
		Transition(gpui.NewTransitionRule(gpui.TransitionTransform, transDuration).WithEasing(gpui.EasingLinear()))
	if s.on {
		rotateBox = rotateBox.Rotate(transRotateTo)
	} else {
		rotateBox = rotateBox.Rotate(transRotateFrom)
	}
	goDiv := gpui.Div().
		ID("go").
		DebugSelector("go").
		Absolute().Left(gpui.Px(goX)).Top(gpui.Px(goY)).
		Size(gpui.Px(transW), gpui.Px(transH)).
		Bg(gpui.Rgb(0xcccccc)).
		OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			s.on = !s.on
			log.Append("click", "go")
			w.Refresh()
		})
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Bg(gpui.Rgb(pageBackground)).
		OnClick(log.Click("root")).
		Child(scaleBox.IntoElement()).
		Child(rotateBox.IntoElement()).
		Child(goDiv.IntoElement()).
		IntoElement()
}

func init() {
	parity.Register(TranslateFixture)
	parity.Register(ScaleFixture)
	parity.Register(RotateFixture)
	parity.Register(SkewFixture)
	parity.Register(OriginFixture)
	parity.Register(TransitionFixture)
}
