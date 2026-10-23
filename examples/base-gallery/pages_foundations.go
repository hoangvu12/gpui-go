package main

import (
	"fmt"
	"math"
	"time"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var foundationPages = []page{
	{"motion", "Motion", "motion.rs, motion/keyframes.rs", (*gallery).motionPage},
	{"theme", "Theme tokens", "theme.rs, theme_tokens.rs", (*gallery).themePage},
}

type motionState struct {
	springRight   bool
	bouncyRight   bool
	keyframeRun   uint64
	transitionBig bool
	present       bool
}

func (g *gallery) initMotion(_ *gpui.Window, _ *gpui.Context[gallery]) {
	g.present = true
}

func (g *gallery) motionPage(w *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()

	// Spring: a selector indicator that keeps its velocity when retargeted.
	indicator := base.MotionSpring(base.TransitionIDWithChannel("spring-indicator", "x"),
		map[bool]float32{false: 0, true: 120}[g.springRight],
		base.NewSpring(420*time.Millisecond).WithDamping(0.68), w, app)
	selector := gpui.Div().Relative().W(gpui.Px(240)).H9().
		Border1().BorderColor(pal.border).RoundedLg().OverflowHidden().
		Child(gpui.Div().Absolute().Top0().Left(gpui.Px(indicator)).W(gpui.Px(118)).HFull().Bg(pal.primary)).
		Child(gpui.Div().Relative().HFull().Flex())
	options := gpui.Div().Absolute().Inset0().Flex()
	for _, opt := range []struct {
		label string
		right bool
	}{{"Focus", false}, {"Flow", true}} {
		color := pal.muted
		if g.springRight == opt.right {
			color = pal.primaryFg
		}
		right := opt.right
		options = options.Child(base.NewButton("spring-" + opt.label).CursorPointer().
			W(gpui.Px(119)).HFull().Flex().ItemsCenter().JustifyCenter().TextSm().TextColor(color).
			OnClick(g.update(cx, func(g *gallery) { g.springRight = right })).
			Child(opt.label))
	}
	selector = selector.Child(options)

	// A loosely damped spring overshoots before settling.
	bouncy := base.MotionSpring(base.TransitionIDWithChannel("spring-bouncy", "x"),
		map[bool]float32{false: 0, true: 220}[g.bouncyRight],
		base.NewSpring(700*time.Millisecond).WithDamping(0.3), w, app)
	track := gpui.Div().Relative().W(gpui.Px(260)).H8().
		Child(gpui.Div().Absolute().Top(gpui.Px(15)).Left0().WFull().H(gpui.Px(2)).Bg(pal.border)).
		Child(gpui.Div().Absolute().Top0().Left(gpui.Px(bouncy)).Size8().RoundedFull().Bg(pal.accent))

	// Keyframes: seven bars on one track with offset delays, looping.
	frames, err := base.KeyframesTryNew(
		base.NewKeyframe[float32](0, 0),
		base.NewKeyframe[float32](0.35, 1).Ease(base.EasingEaseOut()),
		base.NewKeyframe[float32](0.7, 0),
		base.NewKeyframe[float32](1, 0),
	)
	var bars any
	if err != nil {
		bars = missing("keyframes rejected: " + err.Error())
	} else {
		row := gpui.Div().H(gpui.Px(64)).Flex().ItemsEnd().JustifyCenter().Gap2()
		for ix := range 7 {
			id := base.TransitionIDWithChannel(gpui.NamedIntegerElementID("keyframe-bar", g.keyframeRun*100+uint64(ix)), "height")
			value := base.AnimateKeyframes(id, frames,
				base.NewTiming(1200*time.Millisecond).
					Delay(base.SignedDurationPositive(time.Duration(ix)*80*time.Millisecond)).
					Iterations(base.IterationCountInfinite()),
				w, app).Value
			row = row.Child(gpui.Div().W5().H(gpui.Px(18 + 38*value)).RoundedSm().Bg(pal.primary).Opacity(0.35 + 0.65*value))
		}
		bars = row
	}

	// Transition: a CSS-like tween between two widths.
	width := base.MotionTransition(base.TransitionIDWithChannel("transition-bar", "width"),
		map[bool]float32{false: 80, true: 300}[g.transitionBig],
		base.NewTransition(500*time.Millisecond).Easing(base.EasingEaseInOut()), w, app)

	// Presence: a card that fades out before it unmounts.
	sample := base.NewPresence("presence-card", g.present).
		Transition(base.NewTransition(360*time.Millisecond).Easing(base.EasingEaseInOut())).
		Sample(w, app)
	card := gpui.Div().H(gpui.Px(56))
	if sample.ShouldRender() {
		card = card.Child(gpui.Div().W(gpui.Px(260)).P3().TextSm().
			Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface).
			Opacity(sample.Progress).Mt(gpui.Px(8 * (1 - sample.Progress))).
			Child("Notification card"))
	}

	return col(
		demo("Spring: retarget mid-flight and it turns around smoothly", selector),
		demo("Spring with damping 0.3 (overshoots)", row(track,
			btn("bouncy-toggle", "Move", outline, sm).OnClick(g.update(cx, func(g *gallery) { g.bouncyRight = !g.bouncyRight })))),
		demo("Keyframes: one track, seven offset playbacks, infinite", col(bars,
			row(btn("keyframes-restart", "Restart", outline, sm).OnClick(g.update(cx, func(g *gallery) { g.keyframeRun++ }))))),
		demo("Transition (500 ms ease-in-out)", row(
			gpui.Div().W(gpui.Px(300)).Child(gpui.Div().W(gpui.Px(width)).H3().RoundedFull().Bg(pal.series[1])),
			btn("transition-toggle", "Toggle width", outline, sm).OnClick(g.update(cx, func(g *gallery) { g.transitionBig = !g.transitionBig })))),
		demo("Presence (fade out, then unmount)", row(card,
			btn("presence-toggle", map[bool]string{true: "Dismiss", false: "Show"}[g.present], outline, sm).
				OnClick(g.update(cx, func(g *gallery) { g.present = !g.present })))),
		note(fmt.Sprintf("Reduced motion: %v (Base settles every animation at once when the system asks for reduced motion).", app.ReduceMotion())),
	)
}

// hex renders a colour as #rrggbb (plus alpha when translucent).
func hex(c gpui.Hsla) string {
	h, s, l := float64(c.H), float64(c.S), float64(c.L)
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	channel := func(t float64) int {
		t -= math.Floor(t)
		var v float64
		switch {
		case t < 1.0/6:
			v = p + (q-p)*6*t
		case t < 0.5:
			v = q
		case t < 2.0/3:
			v = p + (q-p)*(2.0/3-t)*6
		default:
			v = p
		}
		return int(math.Round(v * 255))
	}
	r, g, b := channel(h+1.0/3), channel(h), channel(h-1.0/3)
	if s == 0 {
		r, g, b = int(math.Round(l*255)), int(math.Round(l*255)), int(math.Round(l*255))
	}
	out := fmt.Sprintf("#%02x%02x%02x", r, g, b)
	if c.A < 1 {
		out += fmt.Sprintf(" · %.0f%%", c.A*100)
	}
	return out
}

func (g *gallery) themePage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	theme := base.ThemeGlobal(cx.App())
	c := theme.Tokens.Colors
	tokens := []struct {
		name  string
		color gpui.Hsla
	}{
		{"background", c.Background}, {"foreground", c.Foreground},
		{"surface", c.Surface}, {"surface_foreground", c.SurfaceForeground},
		{"primary", c.Primary}, {"primary_foreground", c.PrimaryForeground},
		{"secondary", c.Secondary}, {"secondary_foreground", c.SecondaryForeground},
		{"muted", c.Muted}, {"muted_foreground", c.MutedForeground},
		{"accent", c.Accent}, {"accent_foreground", c.AccentForeground},
		{"destructive", c.Destructive}, {"destructive_foreground", c.DestructiveForeground},
		{"border", c.Border}, {"input", c.Input},
		{"ring", c.Ring}, {"selection", c.Selection},
	}
	grid := gpui.Div().Grid().GridCols(3).Gap3().W(gpui.Px(660))
	for _, t := range tokens {
		grid = grid.Child(gpui.Div().Flex().ItemsCenter().Gap3().P2().
			Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface).
			Child(gpui.Div().Size(gpui.Px(40)).FlexNone().RoundedMd().Bg(t.color).Border1().BorderColor(pal.border)).
			Child(gpui.Div().MinW(gpui.Px(0)).Flex().FlexCol().
				Child(gpui.Div().TextXs().FontWeight(600).Child(t.name)).
				Child(gpui.Div().TextXs().TextColor(pal.muted).Child(hex(t.color)))))
	}
	r := theme.Tokens.Radius
	radii := row()
	for _, rad := range []struct {
		name string
		v    float32
	}{{"none", r.None}, {"sm", r.Sm}, {"md", r.Md}, {"lg", r.Lg}, {"xl", r.Xl}, {"full", r.Full}} {
		radii = radii.Child(gpui.Div().Flex().FlexCol().ItemsCenter().Gap1().
			Child(gpui.Div().Size(gpui.Px(44)).Bg(pal.elevated).Border1().BorderColor(pal.border).Rounded(gpui.Px(min(rad.v, 22)))).
			Child(note(rad.name)))
	}
	ty := theme.Tokens.Typography
	types := col()
	for _, t := range []struct {
		name  string
		token base.TextStyleToken
	}{{"xs", ty.Xs}, {"sm", ty.Sm}, {"md", ty.Md}, {"lg", ty.Lg}, {"xl", ty.Xl}} {
		types = types.Child(gpui.Div().Flex().ItemsCenter().Gap3().
			Child(gpui.Div().W(gpui.Px(32)).TextXs().TextColor(pal.muted).Child(t.name)).
			Child(gpui.Div().TextSize(gpui.Px(t.token.Size)).LineHeight(gpui.Px(t.token.LineHeight)).
				Child(fmt.Sprintf("The quick brown fox · %.0f/%.0f", t.token.Size, t.token.LineHeight))))
	}
	return col(
		demo(fmt.Sprintf("Colour tokens (%s appearance; switch in the header)", theme.Appearance), grid),
		demo("Radius tokens", radii),
		demo("Typography tokens (sans "+ty.Sans+", mono "+ty.Mono+")", types),
	)
}
