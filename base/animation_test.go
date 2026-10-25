package base

import (
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// animation.rs tests (4/4) plus Go-own EffectTransition coverage.

// U:animation.rs:263-311
func TestCubicBezierMatchesEnginePublishedValues(t *testing.T) {
	// Reference values sampled from the CSS `ease` curve.
	ease := CubicBezier(0.25, 0.1, 0.25, 1)
	for _, c := range [][2]float32{{0, 0}, {0.2, 0.295}, {0.5, 0.802}, {0.8, 0.976}, {1, 1}} {
		if got := ease(c[0]); abs32(got-c[1]) >= 1e-3 {
			t.Fatalf("ease(%v) = %v, expected %v", c[0], got, c[1])
		}
	}

	// Chromium's CubicBezier(0.25, 0, 0.75, 1) expectations, from
	// ui/gfx/geometry/cubic_bezier_unittest.cc (epsilon widened for f32).
	curve := CubicBezier(0.25, 0, 0.75, 1)
	for _, c := range [][2]float32{
		{0.05, 0.01136}, {0.1, 0.03978}, {0.15, 0.07978}, {0.2, 0.12803}, {0.25, 0.18235},
		{0.3, 0.24115}, {0.35, 0.30323}, {0.4, 0.36761}, {0.45, 0.43345}, {0.5, 0.5},
		{0.6, 0.63238}, {0.65, 0.69676}, {0.7, 0.75884}, {0.75, 0.81764}, {0.8, 0.87196},
		{0.85, 0.92021}, {0.9, 0.96021}, {0.95, 0.98863},
	} {
		if got := curve(c[0]); abs32(got-c[1]) >= 3e-4 {
			t.Fatalf("curve(%v) = %v, Chromium says %v", c[0], got, c[1])
		}
	}
}

// U:animation.rs:313-329
func TestCubicBezierWithThirdsXMapsTimeIdentically(t *testing.T) {
	ease := CubicBezier(1.0/3, 0.72, 2.0/3, 1)
	for step := 0; step <= 100; step++ {
		x := float32(step) / 100
		u := 1 - x
		expected := 3*0.72*u*u*x + 3*u*x*x + x*x*x
		if got := ease(x); abs32(got-expected) >= 1e-4 {
			t.Fatalf("ease(%v) = %v, expected %v", x, got, expected)
		}
	}
}

// U:animation.rs:331-382
func TestCubicBezierMatchesTheCSSDefinition(t *testing.T) {
	// Reference solver straight off the CSS Easing Functions definition:
	// solve x(s) = t by bisection in f64, then sample y(s).
	cssReference := func(x1, y1, x2, y2, t float64) float64 {
		sample := func(p1, p2, s float64) float64 {
			return 3*p1*(1-s)*(1-s)*s + 3*p2*(1-s)*s*s + s*s*s
		}
		low, high := 0.0, 1.0
		for range 64 {
			mid := (low + high) / 2
			if sample(x1, x2, mid) < t {
				low = mid
			} else {
				high = mid
			}
		}
		return sample(y1, y2, (low+high)/2)
	}
	curves := [][4]float32{
		{0.25, 0.1, 0.25, 1},
		{1.0 / 3, 0.72, 2.0 / 3, 1},
		{0.42, 0, 1, 1},
		{0, 0, 0.58, 1},
		{0.42, 0, 0.58, 1},
		{0, 0, 1, 1},
		// Degenerate x slopes forcing the bisection fallback.
		{1, 0, 0, 1},
		{0, 0, 0, 1},
		{1, 0.5, 1, 0.5},
	}
	for _, c := range curves {
		ease := CubicBezier(c[0], c[1], c[2], c[3])
		for step := 0; step <= 1000; step++ {
			x := float32(step) / 1000
			y := ease(x)
			expected := float32(cssReference(float64(c[0]), float64(c[1]), float64(c[2]), float64(c[3]), float64(x)))
			if abs32(y-expected) >= 5e-4 {
				t.Fatalf("cubic_bezier%v(%v) = %v, CSS = %v", c, x, y, expected)
			}
			if y < 0 || y > 1 {
				t.Fatalf("ease(%v) = %v out of range", x, y)
			}
		}
	}
}

// U:animation.rs:384-396
func TestCubicBezierIsMonotonicAndClamped(t *testing.T) {
	ease := CubicBezier(0.32, 0.72, 0, 1)
	if ease(-1) != 0 || ease(2) != 1 {
		t.Fatalf("ease(-1) = %v, ease(2) = %v", ease(-1), ease(2))
	}
	previous := float32(0)
	for step := 0; step <= 100; step++ {
		current := ease(float32(step) / 100)
		if current < previous-1e-4 {
			t.Fatalf("not monotonic at %d", step)
		}
		previous = current
	}
}

// Go-own: the easing presets and Lerp over the core's types
// (U:animation.rs:67-132).
func TestEasingPresetsAndLerp(t *testing.T) {
	if EaseOutCubic(0.5) != 0.875 || EaseInCubic(0.5) != 0.125 || EaseInOutCubic(0.25) != 0.0625 || EaseInOutCubic(0.75) != 0.9375 {
		t.Fatal("cubic presets")
	}
	if EaseOutCubic(2) != 1 || EaseInCubic(-1) != 0 {
		t.Fatal("presets clamp their input")
	}
	if got := LerpValue(gpui.Point{X: 0, Y: 10}, gpui.Point{X: 10, Y: 30}, 0.5); got != (gpui.Point{X: 5, Y: 20}) {
		t.Fatalf("point lerp = %v", got)
	}
	got := LerpValue(gpui.Hsla{H: 0.2, S: 0, L: 0.2, A: 1}, gpui.Hsla{H: 0.4, S: 0, L: 0.8, A: 0}, 0.5)
	if abs32(got.H-0.3) > 1e-6 || abs32(got.L-0.5) > 1e-6 || got.A != 0.5 {
		t.Fatalf("hsla lerp = %v", got)
	}
}

// Go-own: EffectTransition drives the element's style through the core
// animation element and lands on its targets (U:animation.rs:213-247).
func TestEffectTransitionAnimatesStyleOverItsDuration(t *testing.T) {
	ta := gpui.NewTestApp()
	m := openMotionWindow(t, ta, func(*gpui.Window, *gpui.App) gpui.AnyElement {
		box := gpui.Div().Flex().Absolute().DebugSelector("box").W(gpui.Px(10)).H(gpui.Px(10))
		return gpui.Div().Flex().Size(gpui.Px(200)).Child(
			NewEffectTransition(100*time.Millisecond).
				Ease(gpui.Linear).
				SlideY(-40, 0).
				Width(20, 60).
				Fade(0, 1).
				Apply(box, "enter-anim"),
		).IntoElement()
	})
	bounds := func() gpui.Bounds {
		b, ok := m.visual().DebugBounds("box")
		if !ok {
			t.Fatal("box not drawn")
		}
		return b
	}
	if b := bounds(); b.Origin.Y != -40 || b.Size.Width != 20 {
		t.Fatalf("start bounds = %+v", b)
	}
	ta.AdvanceClock(50)
	if m.pendingFrame() != 1 {
		t.Fatal("the running animation requested no frame")
	}
	ta.RunUntilParked()
	if b := bounds(); math.Abs(float64(b.Origin.Y+20)) > 1e-3 || math.Abs(float64(b.Size.Width-40)) > 1e-3 {
		t.Fatalf("midway bounds = %+v", b)
	}
	ta.AdvanceClock(50)
	m.pendingFrame()
	ta.RunUntilParked()
	if b := bounds(); b.Origin.Y != 0 || b.Size.Width != 60 {
		t.Fatalf("end bounds = %+v", b)
	}
	m.pendingFrame()
	ta.RunUntilParked()
	if n := m.pendingFrame(); n != 0 {
		t.Fatalf("a finished transition still requests frames (%d)", n)
	}
}
