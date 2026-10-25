//go:build windows

// Package transition is browser-core ticket 44's fixture set for the
// Transition style property and the per-element style diff: pages whose
// boxes change style on a click, recorded in headless Chrome at a fixed
// mid-transition time through the recorder's virtual-time budget, and
// replayed against the gpui-go view whose transitions the wait step
// samples at the same clock.
//
//   - transition: the sample curves. A click toggles five boxes —
//     width linear (Tier A: exactly halfway at 200 of 400ms), width
//     steps(4) over 800ms (Tier A: one quarter of the way), width ease
//     (Tier C only: the bezier's midpoint is not exact in float32),
//     opacity 1→0 linear and background red→blue linear (both Tier C:
//     the interpolated paint). The two Tier A boxes pin the timing and
//     the step boundary; the pixel tier pins the eased width, the
//     opacity midpoint and the sRGB colour midpoint.
//   - transition-reverse: the reversing-shortening rule. The click
//     starts a 400ms linear width transition, 200ms in (halfway, eased
//     progress 0.5) a second click reverses it — the return run starts
//     from the current value shortened by the factor 0.5 to 200ms — and
//     100ms into that (Tier A: exactly halfway back) the record reads
//     the box.
//
// Deterministic time: the recorder advances Chrome's virtual clock by
// the wait steps' budgets (Emulation.setVirtualTimePolicy) and freezes
// the page's animations at the sample; the replay advances the test
// application's virtual clock and delivers one animation frame. Both
// sides therefore sample mid-transition state at the same instants, so
// a wrong curve, duration or reversing factor moves Tier A and Tier C
// measurably.
package transition

import (
	"sync"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by both sides (the tracer's discipline: one set
// of constants builds the page and the view).
const (
	windowWidth  = 320.0
	windowHeight = 240.0

	goWidth   = 40.0
	goHeight  = 20.0
	gap       = 10.0
	boxHeight = 30.0

	widthOff = 100.0
	widthOn  = 200.0

	// The sample instant: 200ms after the click.
	sampleMs = 200

	// linearDuration: the linear/ease/opacity/colour rules' duration.
	linearDuration = 400 * time.Millisecond
	// stepsDuration: the steps rule runs 800ms so its quarter step
	// lands exactly at the 200ms sample.
	stepsDuration = 800 * time.Millisecond
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	goGray         = 0xcccccc
	widthGreen     = 0x00ff00
	opacityBlue    = 0x0000ff
	bgRed          = 0xff0000
	bgBlue         = 0x0000ff
)

// state is the fixture's per-run toggle state: the click handler flips
// it and the view reads it. It is keyed on the run's event log, so the
// second Replay of the same fixture (the tolerance headroom check)
// starts from off again.
type state struct {
	on bool
}

var (
	stateMu    sync.Mutex
	stateByLog = map[*parity.EventLog]*state{}
)

// stateOf returns the run's state, creating it off.
func stateOf(log *parity.EventLog) *state {
	stateMu.Lock()
	defer stateMu.Unlock()
	s, ok := stateByLog[log]
	if !ok {
		s = &state{}
		stateByLog[log] = s
	}
	return s
}

// Fixture is the sample-curves fixture: one click, one 200ms sample.
var Fixture = parity.Fixture{
	Name:   "transition",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		parity.Click(goWidth/2, goHeight/2),
		parity.Wait(sampleMs),
	},
	EventKinds: []string{"click"},
	BoxIDs:     []string{"width-linear", "width-steps"},
	View:       View,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), measured
		// from the correct view at the 200ms sample: whole-image mean
		// 0.0260, hot 0 (the fixture is text-free geometry and colour;
		// the residue is the colour round trip's 1/255 and the eased
		// box's float32 bezier at a fraction of a device pixel — its
		// 180.16px width snaps to the same 180 device pixels). The
		// wrong-duration view measures far beyond (a quarter of the
		// linear box's green is missing). Verified by transition_test.go.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 1.0,
		MaxHotPixelFraction:   0.004,
	},
}

// ReverseFixture is the reversing-shortening-rule fixture: start, wait
// 200ms (halfway), reverse, wait 100ms (halfway back), read.
var ReverseFixture = parity.Fixture{
	Name:   "transition-reverse",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   reversePage,
	Steps: []parity.Step{
		parity.Click(goWidth/2, goHeight/2),
		parity.Wait(sampleMs),
		parity.Click(goWidth/2, goHeight/2),
		parity.Wait(100),
	},
	EventKinds: []string{"click"},
	BoxIDs:     []string{"width"},
	View:       ReverseView,
	Tolerance: parity.PixelTolerance{
		// Provisional, like Fixture's: the measured correct-view
		// statistics are 0.0000 mean and 0 hot everywhere (one green box
		// mid-reversal on white).
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(Fixture)
	parity.Register(ReverseFixture)
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
		width: 320px;
		height: 240px;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
	}
	#go {
		width: 40px;
		height: 20px;
		background: #cccccc;
	}
	.box {
		height: 30px;
	}
	#width-linear {
		width: 100px;
		background: #00ff00;
		transition: width 400ms linear;
	}
	#width-steps {
		width: 100px;
		background: #00ff00;
		transition: width 800ms steps(4, jump-end);
	}
	#width-ease {
		width: 100px;
		background: #00ff00;
		transition: width 400ms ease;
	}
	#opacity {
		width: 100px;
		background: #0000ff;
		opacity: 1;
		transition: opacity 400ms linear;
	}
	#bg {
		width: 100px;
		background: #ff0000;
		transition: background-color 400ms linear;
	}
	body.on #width-linear { width: 200px; }
	body.on #width-steps  { width: 200px; }
	body.on #width-ease   { width: 200px; }
	body.on #opacity      { opacity: 0; }
	body.on #bg           { background: #0000ff; }
</style>
</head>
<body>
	<div id="go" data-id="go"></div>
	<div class="box" id="width-linear" data-id="width-linear"></div>
	<div class="box" id="width-steps" data-id="width-steps"></div>
	<div class="box" id="width-ease" data-id="width-ease"></div>
	<div class="box" id="opacity" data-id="opacity"></div>
	<div class="box" id="bg" data-id="bg"></div>
	<script>
		document.getElementById("go").addEventListener("click", () => {
			document.body.classList.toggle("on");
		});
	</script>
</body>
</html>
`

// reversePage is the HTML twin of ReverseView: one box.
const reversePage = `<!doctype html>
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
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
	}
	#go {
		width: 40px;
		height: 20px;
		background: #cccccc;
	}
	#width {
		width: 100px;
		height: 30px;
		background: #00ff00;
		transition: width 400ms linear;
	}
	body.on #width { width: 200px; }
</style>
</head>
<body>
	<div id="go" data-id="go"></div>
	<div id="width" data-id="width"></div>
	<script>
		document.getElementById("go").addEventListener("click", () => {
			document.body.classList.toggle("on");
		});
	</script>
</body>
</html>
`

// View is the gpui-go twin of page: the go button whose click toggles
// the state, then the five transition boxes.
func View(log *parity.EventLog) gpui.AnyElement {
	return root(log, []box{
		{"width-linear", widthRules(), widthGreen, width},
		{"width-steps", stepsRules(), widthGreen, width},
		{"width-ease", easeRules(), widthGreen, width},
		{"opacity", opacityRules(), opacityBlue, opacity},
		{"bg", backgroundRules(), bgRed, background},
	})
}

// ReverseView is the gpui-go twin of reversePage: one linear width box.
func ReverseView(log *parity.EventLog) gpui.AnyElement {
	return root(log, []box{
		{"width", widthRules(), widthGreen, width},
	})
}

// WrongDurationView is the deliberately-wrong view of Fixture for the
// discrimination test: the linear box's rule runs 800ms instead of
// 400ms, so at the 200ms sample the box is a quarter of the way (125px)
// where the record holds the halfway value (150px) — a timing bug the
// Tier A box comparison catches exactly.
func WrongDurationView(log *parity.EventLog) gpui.AnyElement {
	wrong := []gpui.TransitionRule{
		gpui.NewTransitionRule(gpui.TransitionWidth, 800*time.Millisecond).WithEasing(gpui.EasingLinear()),
	}
	return root(log, []box{
		{"width-linear", wrong, widthGreen, width},
		{"width-steps", stepsRules(), widthGreen, width},
		{"width-ease", easeRules(), widthGreen, width},
		{"opacity", opacityRules(), opacityBlue, width},
		{"bg", backgroundRules(), bgRed, width},
	})
}

// box is one transition box of the view: its id, its transition rules,
// its background, and the target the toggle changes — the width boxes
// change width (the page's body.on rules), the paint boxes stay 100px
// wide and change opacity or colour.
type box struct {
	id    string
	rules []gpui.TransitionRule
	color uint32
	// paint applies the toggle's target to the element.
	paint func(el *gpui.DivElement, on bool) *gpui.DivElement
}

// width is the width-transition box's target: 100px off, 200px on.
func width(el *gpui.DivElement, on bool) *gpui.DivElement {
	if on {
		return el.W(gpui.Px(widthOn))
	}
	return el.W(gpui.Px(widthOff))
}

// opacity is the opacity-transition box's target: 1 off, 0 on, over a
// 100px box that never changes width.
func opacity(el *gpui.DivElement, on bool) *gpui.DivElement {
	if on {
		return el.W(gpui.Px(widthOff)).Opacity(0)
	}
	return el.W(gpui.Px(widthOff)).Opacity(1)
}

// background is the background-colour transition's target: red off,
// blue on, over a 100px box that never changes width.
func background(el *gpui.DivElement, on bool) *gpui.DivElement {
	if on {
		return el.W(gpui.Px(widthOff)).Bg(gpui.Rgb(bgBlue))
	}
	return el.W(gpui.Px(widthOff)).Bg(gpui.Rgb(bgRed))
}

// widthRules is the linear width rule.
func widthRules() []gpui.TransitionRule {
	return []gpui.TransitionRule{
		gpui.NewTransitionRule(gpui.TransitionWidth, linearDuration).WithEasing(gpui.EasingLinear()),
	}
}

// stepsRules is the steps(4, jump-end) width rule over 800ms.
func stepsRules() []gpui.TransitionRule {
	steps, err := gpui.EasingSteps(4, gpui.StepPositionJumpEnd)
	if err != nil {
		panic(err)
	}
	return []gpui.TransitionRule{
		gpui.NewTransitionRule(gpui.TransitionWidth, stepsDuration).WithEasing(steps),
	}
}

// easeRules is the default-ease width rule (the CSS shorthand's `ease`
// timing function).
func easeRules() []gpui.TransitionRule {
	return []gpui.TransitionRule{
		gpui.NewTransitionRule(gpui.TransitionWidth, linearDuration),
	}
}

// opacityRules animates opacity 1 → 0 linearly.
func opacityRules() []gpui.TransitionRule {
	return []gpui.TransitionRule{
		gpui.NewTransitionRule(gpui.TransitionOpacity, linearDuration).WithEasing(gpui.EasingLinear()),
	}
}

// backgroundRules animates the background colour red → blue linearly.
func backgroundRules() []gpui.TransitionRule {
	return []gpui.TransitionRule{
		gpui.NewTransitionRule(gpui.TransitionBackground, linearDuration).WithEasing(gpui.EasingLinear()),
	}
}

// root builds the fixture's root: the go button (its click toggles the
// run's state and logs the click) over the boxes, in a 10px-gap flex
// column like the page's body.
func root(log *parity.EventLog, boxes []box) gpui.AnyElement {
	s := stateOf(log)
	goDiv := gpui.Div().Flex().
		ID("go").
		W(gpui.Px(goWidth)).
		H(gpui.Px(goHeight)).
		Bg(gpui.Rgb(goGray)).
		OnClick(func(e *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			s.on = !s.on
			log.Append("click", "go")
			w.Refresh()
		})
	root := gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		AlignItems(gpui.AlignItemsStart).
		Gap(gpui.Px(gap)).
		Bg(gpui.Rgb(pageBackground)).
		Child(goDiv)
	for _, b := range boxes {
		el := gpui.Div().Flex().
			ID(b.id).
			DebugSelector(b.id).
			H(gpui.Px(boxHeight)).
			Bg(gpui.Rgb(b.color)).
			Transition(b.rules...)
		el = b.paint(el, s.on)
		root = root.Child(el)
	}
	return root.IntoElement()
}
