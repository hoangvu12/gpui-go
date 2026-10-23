package base

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The shape tests of U:plot/shape/{stack,pie,arc,line,radial_line}.rs
// (1 + 2 + 4 + 1 + 2 = 10/10). The line and radial-line path tests build
// paths through the native tessellator (PathBuilder.Build), so they need
// the native library and fail without it.

// U:plot/shape/stack.rs:151-192
func TestStackBasic(t *testing.T) {
	type sales struct {
		date                      string
		apples, bananas, cherries float32
	}
	data := []sales{{"Jan", 10, 20, 30}, {"Feb", 15, 25, 35}}
	series := NewStack[sales]().
		Data(data).
		Keys("apples", "bananas", "cherries").
		Value(func(d sales, key string) (float32, bool) {
			switch key {
			case "apples":
				return d.apples, true
			case "bananas":
				return d.bananas, true
			case "cherries":
				return d.cherries, true
			}
			return 0, false
		}).
		Series()

	if len(series) != 3 {
		t.Fatalf("len = %d", len(series))
	}
	for i, want := range []struct {
		key    string
		y0, y1 float32
	}{{"apples", 0, 10}, {"bananas", 10, 30}, {"cherries", 30, 60}} {
		s := series[i]
		if s.Key != want.key || s.Points[0].Y0 != want.y0 || s.Points[0].Y1 != want.y1 {
			t.Fatalf("series %d = %s %v..%v, want %+v", i, s.Key, s.Points[0].Y0, s.Points[0].Y1, want)
		}
	}
}

func identityValue(v float32) (float32, bool) { return v, true }

// U:plot/shape/pie.rs:103-120
func TestPie(t *testing.T) {
	arcs := NewPie[float32]().Value(identityValue).Arcs([]float32{1, 1, 1})
	if len(arcs) != 3 {
		t.Fatalf("len = %d", len(arcs))
	}
	for _, a := range arcs {
		if a.Value != 1 {
			t.Fatalf("value = %v", a.Value)
		}
	}
	if arcs[0].StartAngle != 0 || arcs[0].EndAngle != arcs[1].StartAngle || arcs[1].EndAngle != arcs[2].StartAngle {
		t.Fatalf("arcs are not contiguous: %v", arcs)
	}
	if arcs[2].EndAngle != float32(2*math.Pi) {
		t.Fatalf("end = %v, want TAU", arcs[2].EndAngle)
	}
}

// U:plot/shape/pie.rs:122-133
func TestPieZeroValues(t *testing.T) {
	arcs := NewPie[float32]().Value(identityValue).Arcs([]float32{0, 1, 0, 2})
	if len(arcs) != 2 || arcs[0].Value != 1 || arcs[1].Value != 2 || arcs[0].Index != 1 || arcs[1].Index != 3 {
		t.Fatalf("arcs = %v", arcs)
	}
}

// U:plot/shape/arc.rs:260-265
func TestArcDefault(t *testing.T) {
	a := NewArc()
	if a.innerRadius != 0 || a.outerRadius != 0 {
		t.Fatalf("arc = %+v", a)
	}
}

// U:plot/shape/arc.rs:267-273
func TestArcBuilder(t *testing.T) {
	a := NewArc().InnerRadius(10).OuterRadius(20)
	if a.innerRadius != 10 || a.outerRadius != 20 {
		t.Fatalf("arc = %+v", a)
	}
}

// U:plot/shape/arc.rs:275-294
func TestArcCentroid(t *testing.T) {
	a := NewArc().InnerRadius(10).OuterRadius(20)
	unit := struct{}{}
	data := ArcData[struct{}]{Data: &unit, Value: 1, StartAngle: 0, EndAngle: math.Pi}
	c := a.Centroid(data)
	expectedRadius := float32(10+20) / 2
	var pi float32 = math.Pi
	expectedAngle := (0 + pi - 2*plotHalfPi) / 2
	if c.X != expectedRadius*cos32(expectedAngle) || c.Y != expectedRadius*sin32(expectedAngle) {
		t.Fatalf("centroid = %v", c)
	}
}

// U:plot/shape/arc.rs:296-325
func TestArcContains(t *testing.T) {
	// A 100x100 plot: center (50, 50). The right half, 12 to 6 o'clock.
	a := NewArc().InnerRadius(10).OuterRadius(40)
	unit := struct{}{}
	rightHalf := ArcData[struct{}]{Data: &unit, Value: 1, StartAngle: 0, EndAngle: math.Pi}
	bounds := gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}
	p := func(x, y float32) gpui.Point { return gpui.Point{X: x, Y: y} }

	// 3 o'clock, between the radii.
	if !a.Contains(rightHalf, p(80, 50), bounds) {
		t.Fatal("3 o'clock not contained")
	}
	// 9 o'clock is the other half.
	if a.Contains(rightHalf, p(20, 50), bounds) {
		t.Fatal("9 o'clock contained")
	}
	// Inside the hole and past the rim.
	if a.Contains(rightHalf, p(55, 50), bounds) || a.Contains(rightHalf, p(95, 50), bounds) {
		t.Fatal("hole or past-the-rim contained")
	}
	// A wider arc reaches the same point.
	if !NewArc().InnerRadius(10).OuterRadius(50).Contains(rightHalf, p(95, 50), bounds) {
		t.Fatal("wider arc misses the rim point")
	}
	// 12 o'clock is the start of this arc, 6 o'clock the start of the next.
	if !a.Contains(rightHalf, p(50, 20), bounds) || a.Contains(rightHalf, p(50, 80), bounds) {
		t.Fatal("12/6 o'clock edges wrong")
	}
}

// U:plot/shape/line.rs:267-289 (native tessellator).
func TestLinePath(t *testing.T) {
	data := []float32{1, 2, 3}
	line := NewLine[float32]().
		Data(data).
		X(identityValue).
		Y(func(v float32) (float32, bool) { return v * 2, true })
	bounds := gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}
	path, ok, dots := line.path(bounds)
	if !ok || path == nil {
		t.Fatal("line path is None")
	}
	if len(dots) != 0 {
		t.Fatalf("dots = %d, want 0", len(dots))
	}
	if _, _, dots := line.Dot().path(bounds); len(dots) != 3 {
		t.Fatalf("dots = %d, want 3", len(dots))
	}
}

// U:plot/shape/radial_line.rs:257-275
func TestRadialLinePoints(t *testing.T) {
	tau := float32(2 * math.Pi)
	line := NewRadialLine[float32]().
		Data([]float32{1, 1, 1, 1}).
		Angle(func(_ float32, i int) (float32, bool) { return float32(i) * tau / 4, true }).
		Radius(func(v float32, _ int) (float32, bool) { return v * 10, true })
	points := line.points(gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}})
	// 4 points around the center (50, 50), at 12, 3, 6 and 9 o'clock.
	if len(points) != 4 {
		t.Fatalf("len = %d", len(points))
	}
	for i, want := range [][2]float32{{50, 40}, {60, 50}, {50, 60}, {40, 50}} {
		if abs32(points[i].X-want[0]) >= 1e-4 || abs32(points[i].Y-want[1]) >= 1e-4 {
			t.Fatalf("point %d = %v, want %v", i, points[i], want)
		}
	}
}

// U:plot/shape/radial_line.rs:277-304 (native tessellator).
func TestRadialLinePath(t *testing.T) {
	tau := float32(2 * math.Pi)
	bounds := gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}
	base := func() RadialLine[float32] {
		return NewRadialLine[float32]().
			Data([]float32{1, 2, 3}).
			Angle(func(_ float32, i int) (float32, bool) { return float32(i) * tau / 3, true }).
			Radius(func(v float32, _ int) (float32, bool) { return v * 10, true })
	}
	p := base().path(bounds)
	if p.fill != nil || p.stroke == nil || len(p.dots) != 0 {
		t.Fatalf("open line = fill %v stroke %v dots %d", p.fill != nil, p.stroke != nil, len(p.dots))
	}
	p = base().Closed().Fill(gpui.SolidBackground(gpui.Black())).Dot().path(bounds)
	if p.fill == nil || p.stroke == nil || len(p.dots) != 3 {
		t.Fatalf("closed filled line = fill %v stroke %v dots %d", p.fill != nil, p.stroke != nil, len(p.dots))
	}
}
