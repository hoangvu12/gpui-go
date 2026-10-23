package base

// The arc generator (U:plot/shape/arc.rs, after d3-shape's arc).
//
// Go adaptations (recorded deviations):
//
//   - ArcData borrows its datum (`&'a T`); the Go field is a pointer.
//   - The centroid is `Point<f32>`; Pixels are float32, so it is a
//     gpui.Point.
//   - f32 trigonometry (cos, sin, atan2, hypot) is evaluated in float64
//     and rounded to float32; it can differ from the platform's f32 libm
//     in the last place.
//   - fill takes a solid gpui.Background (decision 02).

import (
	"fmt"
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
)

const (
	plotArcEpsilon float32 = 1e-12
	plotHalfPi     float32 = math.Pi / 2
)

// ArcData is one slice of a Pie: its datum and the angles it spans, in
// radians with 0 at 12 o'clock and positive angles proceeding clockwise
// (U:plot/shape/arc.rs:12-22).
type ArcData[T any] struct {
	Data       *T
	Index      int
	Value      float32
	StartAngle float32
	EndAngle   float32
	PadAngle   float32
}

// NewArcData returns a slice of data from startAngle to endAngle, with no
// padding (U:plot/shape/arc.rs:24-37).
func NewArcData[T any](data *T, index int, value, startAngle, endAngle float32) ArcData[T] {
	return ArcData[T]{Data: data, Index: index, Value: value, StartAngle: startAngle, EndAngle: endAngle}
}

// String renders the manual Debug impl (U:plot/shape/arc.rs:39-47).
func (a ArcData[T]) String() string {
	return fmt.Sprintf("ArcData { index: %d, value: %v, start_angle: %v, end_angle: %v, pad_angle: %v }",
		a.Index, a.Value, a.StartAngle, a.EndAngle, a.PadAngle)
}

// Arc paints pie slices between an inner and an outer radius
// (U:plot/shape/arc.rs:49-61).
type Arc struct {
	innerRadius float32
	outerRadius float32
}

// NewArc returns an arc with zero radii (U:plot/shape/arc.rs:64-66).
func NewArc() Arc { return Arc{} }

// InnerRadius sets the inner radius (U:plot/shape/arc.rs:68-72).
func (a Arc) InnerRadius(innerRadius float32) Arc { a.innerRadius = innerRadius; return a }

// OuterRadius sets the outer radius (U:plot/shape/arc.rs:74-78).
func (a Arc) OuterRadius(outerRadius float32) Arc { a.outerRadius = outerRadius; return a }

func cos32(v float32) float32 { return float32(math.Cos(float64(v))) }
func sin32(v float32) float32 { return float32(math.Sin(float64(v))) }

// Centroid returns the centroid of the slice, relative to the center
// (U:plot/shape/arc.rs:80-88).
func (a Arc) Centroid[T any](arc ArcData[T]) gpui.Point {
	startAngle := arc.StartAngle - plotHalfPi
	endAngle := arc.EndAngle - plotHalfPi
	r := (a.innerRadius + a.outerRadius) / 2
	angle := (startAngle + endAngle) / 2
	return gpui.Point{X: r * cos32(angle), Y: r * sin32(angle)}
}

// path builds the filled slice around the bounds center
// (U:plot/shape/arc.rs:90-187).
func (a Arc) path[T any](arc ArcData[T], bounds gpui.Bounds) (*gpui.Path, bool) {
	startAngle := arc.StartAngle - plotHalfPi
	endAngle := arc.EndAngle - plotHalfPi
	da := endAngle - startAngle
	padAngle := arc.PadAngle
	if da >= math.Pi {
		// Leave some pad angle for a full circle, so the path start and
		// end are not the same point.
		padAngle = 0.0001
	}
	r0 := rustMaxF32(a.innerRadius, 0)
	r1 := rustMaxF32(a.outerRadius, 0)

	centerX := bounds.Origin.X + bounds.Size.Width/2
	centerY := bounds.Origin.Y + bounds.Size.Height/2

	if r1 < plotArcEpsilon || abs32(da) < plotArcEpsilon {
		return nil, false
	}

	var a0Outer, a1Outer, a0Inner, a1Inner float32
	if r0 > plotArcEpsilon && padAngle > 0 {
		padWidth := r1 * padAngle
		padAngleOuter := padWidth / r1
		padAngleInner := padWidth / r0
		if maxInnerPad := da * 0.8; padAngleInner > maxInnerPad {
			padAngleInner = maxInnerPad
		}
		a0Outer = startAngle + padAngleOuter*0.5
		a1Outer = endAngle - padAngleOuter*0.5
		a0Inner = startAngle + padAngleInner*0.5
		a1Inner = endAngle - padAngleInner*0.5
	} else {
		pad := padAngle * 0.5
		a0Outer, a1Outer = startAngle+pad, endAngle-pad
		a0Inner, a1Inner = startAngle+pad, endAngle-pad
	}

	if a1Outer-a0Outer <= 0 {
		return nil, false
	}

	x01 := centerX + r1*cos32(a0Outer)
	y01 := centerY + r1*sin32(a0Outer)
	x11 := centerX + r1*cos32(a1Outer)
	y11 := centerY + r1*sin32(a1Outer)

	b := gpui.FillPathBuilder()
	b.MoveTo(x01, y01)
	largeArc := abs32(a1Outer-a0Outer) > math.Pi
	b.ArcTo(r1, r1, 0, largeArc, true, x11, y11)

	if r0 > plotArcEpsilon {
		x10 := centerX + r0*cos32(a1Inner)
		y10 := centerY + r0*sin32(a1Inner)
		b.LineTo(x10, y10)

		x00 := centerX + r0*cos32(a0Inner)
		y00 := centerY + r0*sin32(a0Inner)
		largeArcInner := abs32(a1Inner-a0Inner) > math.Pi
		b.ArcTo(r0, r0, 0, largeArcInner, false, x00, y00)
	} else {
		// No inner radius: draw a line to the center.
		b.LineTo(centerX, centerY)
	}
	return plotBuild(b)
}

// Contains reports whether the cursor at position (relative to the bounds
// origin) is on this arc's slice: within its angles and between this
// arc's radii (U:plot/shape/arc.rs:189-209).
func (a Arc) Contains[T any](arc ArcData[T], position gpui.Point, bounds gpui.Bounds) bool {
	dx := position.X - bounds.Size.Width/2
	dy := position.Y - bounds.Size.Height/2
	radius := float32(math.Hypot(float64(dx), float64(dy)))
	r0 := rustMaxF32(a.innerRadius, 0)
	r1 := rustMaxF32(a.outerRadius, 0)
	if radius < r0 || radius > r1 {
		return false
	}
	// Screen angle -> pie angle (0 at 12 o'clock, clockwise), in [0, TAU).
	angle := plotRemEuclid(float32(math.Atan2(float64(dy), float64(dx)))+plotHalfPi, 2*math.Pi)
	return arc.StartAngle <= angle && angle < arc.EndAngle
}

// plotRemEuclid is f32::rem_euclid.
func plotRemEuclid(a, b float32) float32 {
	r := float32(math.Mod(float64(a), float64(b)))
	if r < 0 {
		return r + abs32(b)
	}
	return r
}

// PaintCached paints the slice, reusing the path tessellated by an
// earlier paint while its angles, radii and the bounds size are unchanged
// (U:plot/shape/arc.rs:211-240). To paint one slice at other radii (e.g.
// lifted on hover), build another Arc with those radii.
func (a Arc) PaintCached[T any](arc ArcData[T], fill gpui.Background, bounds gpui.Bounds, cache *PathCache, w *gpui.Window) {
	type sizeBits struct{ w, h uint32 }
	key := NewShapeKey(sizeBits{math.Float32bits(bounds.Size.Width), math.Float32bits(bounds.Size.Height)}).
		F32(arc.StartAngle).
		F32(arc.EndAngle).
		F32(arc.PadAngle).
		F32(a.innerRadius).
		F32(a.outerRadius).
		Finish()
	local := gpui.Bounds{Size: bounds.Size}
	if path, ok := cache.Get(key, bounds.Origin, func() (*gpui.Path, bool) { return a.path(arc, local) }); ok {
		w.PaintPath(path, fill)
	}
}

// Paint paints the slice (U:plot/shape/arc.rs:242-253).
func (a Arc) Paint[T any](arc ArcData[T], fill gpui.Background, bounds gpui.Bounds, w *gpui.Window) {
	if path, ok := a.path(arc, bounds); ok {
		w.PaintPath(path, fill)
	}
}
