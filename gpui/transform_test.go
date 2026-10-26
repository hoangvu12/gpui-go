package gpui

// Browser-core ticket 56's unit gates (the Chrome-checked twins are the
// parity fixtures of internal/parity/fixtures/transform): the matrix
// model (composition, origin, inverse), the hit test through the
// inverse matrix (hover, click chains and nested transforms), the
// scene's effect-group record for the paint path (the translation-only
// fast path emits no group), the debug bounds' mapped AABB, and the
// transition engine's transform slot.

import (
	"math"
	"testing"
	"time"
)

// approx32 compares two floats at a quarter pixel (the matrix math's
// trig lives in float32).
func approx32(t *testing.T, what string, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 0.25 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// TestTransformMatrixComposition checks the composed window-to-window
// matrix: the functions apply right-to-left (CSS order), the origin
// resolves into the translation, and a pure translation is
// origin-independent.
func TestTransformMatrixComposition(t *testing.T) {
	borderBox := Bounds{Origin: Point{X: 100, Y: 60}, Size: Size{Width: 80, Height: 40}}

	// rotate(90deg) about the default origin (the box centre): the
	// centre stays put and a corner rotates to its 90-degree position.
	m, ok := transformMatrixOf([]TransformFunction{Rotate(90)}, nil, borderBox, 16)
	if !ok {
		t.Fatal("rotate matrix did not resolve")
	}
	centre := applyTransformation(m, Point{X: 140, Y: 80})
	approx32(t, "rotated centre x", centre.X, 140)
	approx32(t, "rotated centre y", centre.Y, 80)
	// The top-right corner (180, 60) maps to the bottom-right corner of
	// the rotated box under a clockwise quarter turn about the centre:
	// the 80x40 box becomes 40x80 spanning (120,40)-(160,120), so the
	// corner lands at (160, 120).
	corner := applyTransformation(m, Point{X: 180, Y: 60})
	approx32(t, "rotated corner x", corner.X, 160)
	approx32(t, "rotated corner y", corner.Y, 120)

	// rotate(90deg) about the top-left origin: the origin stays put.
	topLeft := &TransformOrigin{X: DefinitePx(0), Y: DefinitePx(0)}
	m, _ = transformMatrixOf([]TransformFunction{Rotate(90)}, topLeft, borderBox, 16)
	origin := applyTransformation(m, Point{X: 100, Y: 60})
	approx32(t, "origin-pinned x", origin.X, 100)
	approx32(t, "origin-pinned y", origin.Y, 60)
	corner = applyTransformation(m, Point{X: 180, Y: 60})
	approx32(t, "origin-pinned corner x", corner.X, 100)
	approx32(t, "origin-pinned corner y", corner.Y, 140)

	// translate(10px, 20px) scale(2): the point maps through scale
	// FIRST (the rightmost function), then the translation - CSS
	// `transform: translate(10px, 20px) scale(2)`.
	m, _ = transformMatrixOf(
		[]TransformFunction{Translate(DefinitePx(10), DefinitePx(20)), Scale(2, 2)},
		nil, borderBox, 16)
	// The box centre (the origin) maps to itself translated: the
	// transform-origin keeps the centre, the translate moves it.
	p := applyTransformation(m, Point{X: 140, Y: 80})
	approx32(t, "translate-then-scale x", p.X, 150)
	approx32(t, "translate-then-scale y", p.Y, 100)

	// A translate with rem and fraction arguments resolves against the
	// rem size and the border box.
	m, _ = transformMatrixOf(
		[]TransformFunction{Translate(DefiniteRem(1), Fraction(0.5))},
		nil, borderBox, 16)
	p = applyTransformation(m, Point{X: 0, Y: 0})
	approx32(t, "rem translate x", p.X, 16)
	approx32(t, "fraction translate y", p.Y, 20)

	// scale(0) collapses: no matrix.
	if _, ok := transformMatrixOf([]TransformFunction{Scale(0, 0)}, nil, borderBox, 16); ok {
		t.Error("scale(0) resolved a matrix, want the collapse")
	}
}

// TestTransformationInverseRoundTrip checks the inverse mapping the hit
// test consumes.
func TestTransformationInverseRoundTrip(t *testing.T) {
	m, _ := transformMatrixOf([]TransformFunction{Rotate(30), Translate(DefinitePx(12), DefinitePx(4))},
		nil, Bounds{Origin: Point{X: 40, Y: 30}, Size: Size{Width: 80, Height: 80}}, 16)
	inverse, ok := invertTransformation(m)
	if !ok {
		t.Fatal("inverse did not resolve")
	}
	p := Point{X: 61.3, Y: 22.7}
	round := applyTransformation(inverse, applyTransformation(m, p))
	approx32(t, "round trip x", round.X, p.X)
	approx32(t, "round trip y", round.Y, p.Y)

	// A singular matrix has no inverse: the hit test rejects it.
	if _, ok := invertTransformation(TransformationMatrix{RotationScale: [2][2]float32{{0, 0}, {0, 0}}}); ok {
		t.Error("a singular matrix inverted, want ok=false")
	}
}

// TestMapBoundsThrough checks the mapped AABB (getBoundingClientRect
// geometry: a 90-degree rotation of an 80x40 box reports 40x80).
func TestMapBoundsThrough(t *testing.T) {
	m, _ := transformMatrixOf([]TransformFunction{Rotate(90)}, nil,
		Bounds{Origin: Point{X: 40, Y: 30}, Size: Size{Width: 80, Height: 40}}, 16)
	got := mapBoundsThrough(m, Bounds{Origin: Point{X: 40, Y: 30}, Size: Size{Width: 80, Height: 40}})
	if got.Origin.X != 60 || got.Origin.Y != 10 || got.Size.Width != 40 || got.Size.Height != 80 {
		t.Errorf("mapped bounds = %+v, want (60, 10, 40, 80)", got)
	}
}

// transformView is a minimal render view whose build closure re-runs
// every frame.
type transformView struct {
	build func(w *Window) AnyElement
}

func (v *transformView) Render(w *Window, cx *Context[transformView]) AnyElement {
	return v.build(w)
}

// newTransformView entity builds the window's root view from build.
func newTransformView(app *App, window *Window, build func(w *Window) AnyElement) Entity[transformView] {
	return NewEntity(app, window.Scope(), func(v *transformView, cx *Context[transformView]) {
		v.build = build
	})
}

// transformClickFixture is one test window with a transformed clickable
// element and its child, recording click targets into a transcript.
type transformClickFixture struct {
	t         *testing.T
	app       *App
	window    *Window
	events    *transcript
	transform []TransformFunction
	child     []TransformFunction
}

func newTransformClickFixture(t *testing.T, transform, child []TransformFunction) *transformClickFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 300, Height: 200})
	f := &transformClickFixture{t: t, app: app, window: window, events: &transcript{}, transform: transform, child: child}
	view := newTransformView(app, window, func(w *Window) AnyElement {
		clicked := func(id string) func(*ClickEvent, *Window, *App) {
			return func(e *ClickEvent, w *Window, app *App) { f.events.add("click:%s", id) }
		}
		box := func(id string, fns []TransformFunction, children ...*DivElement) *DivElement {
			d := Div().Flex().ID(id).DebugSelector(id).
				Absolute().Left(Px(40)).Top(Px(30)).Size(Px(80), Px(80)).
				Bg(interactRed)
			if len(fns) > 0 {
				d = d.Transform(fns...)
			}
			d = d.OnClick(clicked(id))
			for _, c := range children {
				d = d.Child(c.IntoElement())
			}
			return d
		}
		inner := box("child", f.child)
		outer := box("box", f.transform, inner)
		root := Div().Flex().ID("root").DebugSelector("root").SizeFull().Relative().
			OnClick(clicked("root")).
			Child(outer)
		return root.IntoElement()
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return f
}

func (f *transformClickFixture) click(x, y float32) {
	f.t.Helper()
	f.window.DispatchInput(&MouseDownEvent{Position: Point{X: x, Y: y}, Button: MouseButtonLeft}, f.app)
	f.window.DispatchInput(&MouseUpEvent{Position: Point{X: x, Y: y}, Button: MouseButtonLeft}, f.app)
}

func (f *transformClickFixture) take() []string { return f.events.take() }

// TestHitTestFollowsRotation: a click at a point inside the ROTATED box
// but outside the layout box hits the element, and a click inside the
// layout box but outside the rotated shape misses it (falls to the
// root) - the two directions of the inverse mapping.
func TestHitTestFollowsRotation(t *testing.T) {
	f := newTransformClickFixture(t, []TransformFunction{Rotate(30)}, nil)
	// The box (40,30)-(120,110), centre (80,70), rotated 30 degrees:
	// the point (126, 56) is inside the rotated shape but right of the
	// layout box's right edge (120).
	f.click(126, 56)
	got := f.take()
	if len(got) != 2 || got[0] != "click:box" || got[1] != "click:root" {
		t.Fatalf("click at the rotated corner: %v, want [click:box click:root]", got)
	}
	// (115, 35) is inside the layout box but outside the rotated shape:
	// only the root sees it.
	f.click(115, 35)
	if got := f.take(); len(got) != 1 || got[0] != "click:root" {
		t.Fatalf("click at the un-rotated corner: %v, want [click:root]", got)
	}
}

// TestHitTestFollowsScale: a click outside the layout box but inside
// the scaled shape hits; a 2x scale about the centre covers (10,-10) to
// (150, 150).
func TestHitTestFollowsScale(t *testing.T) {
	f := newTransformClickFixture(t, []TransformFunction{Scale(2, 2)}, nil)
	// The child box (80,60)-(160,140) sits inside the parent's layout
	// box and inherits the parent's scale: the click at (145, 145) maps
	// back to (112.5, 107.5), inside both - the chain is child, box,
	// root.
	f.click(145, 145)
	if got := f.take(); len(got) != 3 || got[0] != "click:child" || got[1] != "click:box" || got[2] != "click:root" {
		t.Fatalf("click at the scaled corner: %v, want [click:child click:box click:root]", got)
	}
	// (10, 155) is inside the parent's scaled visual but maps outside
	// every layout box: the root alone.
	f.click(10, 155)
	if got := f.take(); len(got) != 1 || got[0] != "click:root" {
		t.Fatalf("click at the scaled bottom edge: %v, want [click:root]", got)
	}
}

// TestHitTestFollowsTranslation: the translation-only fast path (no
// scene group) still moves the hitbox: translate(50, 40) puts the box
// at (90,70)-(170,150).
func TestHitTestFollowsTranslation(t *testing.T) {
	f := newTransformClickFixture(t, []TransformFunction{Translate(DefinitePx(50), DefinitePx(40))}, nil)
	// The child moves with the parent: its box is (130,100)-(210,180);
	// the click at (160,140) lands on the child first.
	f.click(160, 140)
	if got := f.take(); len(got) != 3 || got[0] != "click:child" || got[1] != "click:box" || got[2] != "click:root" {
		t.Fatalf("click at the translated corner: %v, want [click:child click:box click:root]", got)
	}
	// The parent's un-translated centre misses the parent but the
	// translated parent covers it: (100,100) is inside the parent
	// (90,70)-(170,150) and outside the child.
	f.click(100, 100)
	if got := f.take(); len(got) != 2 || got[0] != "click:box" || got[1] != "click:root" {
		t.Fatalf("click at the translated centre: %v, want [click:box click:root]", got)
	}
	// A point outside every translated box: the root alone.
	f.click(30, 30)
	if got := f.take(); len(got) != 1 || got[0] != "click:root" {
		t.Fatalf("click at the un-translated position: %v, want [click:root]", got)
	}
}

// TestHitTestComposesNestedTransforms: a translated parent with a
// rotated child - the click resolves through the composed inverse.
func TestHitTestComposesNestedTransforms(t *testing.T) {
	f := newTransformClickFixture(t,
		[]TransformFunction{Translate(DefinitePx(30), DefinitePx(20))},
		[]TransformFunction{Rotate(-20)})
	// The child's layout box (80,60)-(160,140) moves with the parent to
	// (110,80)-(190,160) and rotates -20 degrees about its centre
	// (150,120). The point (102,97) is inside the rotated child but
	// outside its translated box (x < 110): only the composed inverse
	// reaches it.
	f.click(102, 97)
	got := f.take()
	// The click chain: child, box (parent), root.
	if len(got) != 3 || got[0] != "click:child" || got[1] != "click:box" || got[2] != "click:root" {
		t.Fatalf("click through both transforms: %v, want [click:child click:box click:root]", got)
	}
}

// TestTransformSceneGroupEmission checks the scene side: a rotated div
// brackets its box in one effect-group pair with the matrix (the device
// translation carries the origin resolution), and a translated div
// emits NO group (the offset fast path).
func TestTransformSceneGroupEmission(t *testing.T) {
	record := func(transform []TransformFunction) *Scene {
		ta := NewTestApp()
		app := ta.App()
		window := NewTestWindow(app, Size{Width: 300, Height: 200})
		view := newTransformView(app, window, func(w *Window) AnyElement {
			d := Div().ID("box").DebugSelector("box").
				Absolute().Left(Px(40)).Top(Px(30)).Size(Px(80), Px(80)).
				Bg(interactRed)
			if len(transform) > 0 {
				d = d.Transform(transform...)
			}
			return Div().Flex().ID("root").SizeFull().Relative().Child(d).IntoElement()
		})
		window.SetRootView(ViewOf(view))
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatalf("DrawWindowFrame: %v", err)
		}
		scene := LastDrawnScene(window)
		if scene == nil {
			t.Fatal("no drawn scene")
		}
		return scene
	}

	// A rotation: one start/end pair with the transform flag and the
	// matrix scaled to device space (the test window's scale is 2.0:
	// the logical translation doubles, the 2x2 is unchanged).
	scene := record([]TransformFunction{Rotate(90)})
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatalf("EffectGroups: %v", err)
	}
	if len(groups) != 2 || !groups[0].IsStart || groups[1].IsStart {
		t.Fatalf("rotate scene carries %d groups, want a start/end pair", len(groups))
	}
	g := groups[0]
	if !g.HasTransform || g.PropertyNode || g.HasOpacity || g.Opacity != 1 {
		t.Errorf("group flags: HasTransform=%v PropertyNode=%v HasOpacity=%v Opacity=%v", g.HasTransform, g.PropertyNode, g.HasOpacity, g.Opacity)
	}
	r := g.Transform.RotationScale
	if math.Abs(float64(r[0][0])) > 1e-5 || math.Abs(float64(r[1][0]-1)) > 1e-5 || math.Abs(float64(r[0][1]+1)) > 1e-5 {
		t.Errorf("group matrix 2x2 = %+v, want the 90-degree rotation", r)
	}
	// The rotation about the box centre (80,70): the logical point
	// (140,140) maps to (10,130); in device space (scale 2.0) the matrix
	// maps (280,280) to (20,260) - the 2x2 is scale-invariant, the
	// translation doubles.
	device := applyTransformation(g.Transform, Point{X: 280, Y: 280})
	approx32(t, "device matrix x", device.X, 20)
	approx32(t, "device matrix y", device.Y, 260)

	// A translation: no group at all.
	scene = record([]TransformFunction{Translate(DefinitePx(50), DefinitePx(40))})
	groups, err = scene.EffectGroups()
	if err != nil {
		t.Fatalf("EffectGroups: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("translate scene carries %d groups, want none (the offset fast path)", len(groups))
	}
	// The quads paint at the translated position: the red box's quad
	// bounds origin is (90, 70) logical, (180, 140) device.
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("Quads: %v", err)
	}
	found := false
	for _, q := range quads {
		if q.Background.Solid == interactRed {
			found = true
			approx32(t, "translated quad x", q.Bounds.Origin.X, 180)
			approx32(t, "translated quad y", q.Bounds.Origin.Y, 140)
			approx32(t, "translated quad w", q.Bounds.Size.Width, 160)
		}
	}
	if !found {
		t.Fatal("the red quad is missing from the scene")
	}
}

// TestTransformDebugBoundsAreMapped: the debug bounds (the parity Tier
// A probe) report the mapped AABB, matching getBoundingClientRect.
func TestTransformDebugBoundsAreMapped(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 300, Height: 200})
	view := newTransformView(app, window, func(w *Window) AnyElement {
		d := Div().ID("box").DebugSelector("box").
			Absolute().Left(Px(40)).Top(Px(30)).Size(Px(80), Px(40)).
			Bg(interactRed).
			Transform(Rotate(90))
		return Div().Flex().ID("root").SizeFull().Relative().Child(d).IntoElement()
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	bounds, ok := WindowDebugBounds(window)["box"]
	if !ok {
		t.Fatal("no debug bounds for box")
	}
	// The 80x40 box at (40,30) rotated 90 degrees about its centre
	// (80,50): the mapped AABB is (60,10) 40x80.
	if bounds.Origin.X != 60 || bounds.Origin.Y != 10 || bounds.Size.Width != 40 || bounds.Size.Height != 80 {
		t.Errorf("mapped debug bounds = %+v, want (60, 10, 40, 80)", bounds)
	}
}

// TestScaleZeroHidesTheElement: a collapsed transform paints nothing
// and hit tests nothing.
func TestScaleZeroHidesTheElement(t *testing.T) {
	f := newTransformClickFixture(t, []TransformFunction{Scale(0, 0)}, nil)
	// The centre of the box: a click there falls through to the root.
	f.click(80, 70)
	if got := f.take(); len(got) != 1 || got[0] != "click:root" {
		t.Fatalf("click on a scale(0) box: %v, want [click:root]", got)
	}
	quads := func() int {
		scene := LastDrawnScene(f.window)
		if scene == nil {
			f.t.Fatal("no scene")
		}
		list, err := scene.Quads()
		if err != nil {
			f.t.Fatalf("Quads: %v", err)
		}
		n := 0
		for _, q := range list {
			if q.Background.Solid == interactRed {
				n++
			}
		}
		return n
	}
	if got := quads(); got != 0 {
		t.Errorf("a scale(0) box painted %d red quads, want none", got)
	}
}

// TestTransformTransitionInterpolates: the transition engine's
// transform slot interpolates a matching list (the mid value is exact
// for dyadic factors).
func TestTransformTransitionInterpolates(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 200, Height: 150})
	var on bool
	view := newTransformView(app, window, func(w *Window) AnyElement {
		d := Div().ID("box").DebugSelector("box").
			Absolute().Left(Px(40)).Top(Px(30)).Size(Px(40), Px(40)).
			Bg(interactRed).
			Transition(NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()))
		if !on {
			d = d.Scale(1, 1)
		} else {
			d = d.Scale(1.75, 1.75)
		}
		return Div().Flex().ID("root").SizeFull().Relative().Child(d).IntoElement()
	})
	window.SetRootView(ViewOf(view))
	draw := func() {
		t.Helper()
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatalf("DrawWindowFrame: %v", err)
		}
	}
	draw()

	// Flip the target: the diff starts the run; 200ms in, the sample is
	// the midpoint 1.375.
	on = true
	window.Refresh()
	draw()
	ta.AdvanceClock(200)
	window.Refresh()
	draw()
	bounds, ok := WindowDebugBounds(window)["box"]
	if !ok {
		t.Fatal("no debug bounds")
	}
	// The 40x40 box at (40,30), centre (60,50): at 1.375 the box is
	// 55x55 at (32.5, 22.5).
	if math.Abs(float64(bounds.Origin.X-32.5)) > 0.01 || math.Abs(float64(bounds.Origin.Y-22.5)) > 0.01 ||
		math.Abs(float64(bounds.Size.Width-55)) > 0.01 || math.Abs(float64(bounds.Size.Height-55)) > 0.01 {
		t.Errorf("mid-transition bounds = %+v, want (32.5, 22.5, 55, 55)", bounds)
	}
}
