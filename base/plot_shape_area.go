package base

// The area generator (U:plot/shape/area.rs, after d3-shape's area).
//
// Go adaptations: accessors are funcs of the datum returning (value,
// ok); `y0: Option<f32>` is a value plus a flag; fill and stroke are
// solid gpui.Backgrounds (decision 02).

import (
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Area is a filled area under projected data points, with a 1px top line
// (U:plot/shape/area.rs:7-16).
type Area[T any] struct {
	data   []T
	x      func(datum T) (float32, bool)
	y0     float32
	hasY0  bool
	y1     func(datum T) (float32, bool)
	fill   gpui.Background
	stroke gpui.Background
	curve  Curve
}

// NewArea returns an empty natural-curve area (U:plot/shape/area.rs:18-35).
func NewArea[T any]() Area[T] {
	return Area[T]{x: plotNone[T], y1: plotNone[T]}
}

// Data sets the data (U:plot/shape/area.rs:37-44).
func (a Area[T]) Data(data []T) Area[T] { a.data = append([]T(nil), data...); return a }

// X sets the x accessor (U:plot/shape/area.rs:46-53).
func (a Area[T]) X(x func(datum T) (float32, bool)) Area[T] { a.x = x; return a }

// Y0 sets the baseline (U:plot/shape/area.rs:55-59).
func (a Area[T]) Y0(y0 float32) Area[T] { a.y0, a.hasY0 = y0, true; return a }

// Y1 sets the top accessor (U:plot/shape/area.rs:61-68).
func (a Area[T]) Y1(y1 func(datum T) (float32, bool)) Area[T] { a.y1 = y1; return a }

// Fill sets the fill (U:plot/shape/area.rs:70-74).
func (a Area[T]) Fill(fill gpui.Background) Area[T] { a.fill = fill; return a }

// Stroke sets the stroke of the top line (U:plot/shape/area.rs:76-80).
func (a Area[T]) Stroke(stroke gpui.Background) Area[T] { a.stroke = stroke; return a }

// Curve sets how the top line connects its points; Natural by default
// (U:plot/shape/area.rs:82-87).
func (a Area[T]) Curve(curve Curve) Area[T] { a.curve = curve; return a }

// path builds the fill and the top line (U:plot/shape/area.rs:89-179).
func (a Area[T]) path(bounds gpui.Bounds) (fill *gpui.Path, fillOK bool, line *gpui.Path, lineOK bool) {
	origin := bounds.Origin
	area := gpui.FillPathBuilder()
	stroke := gpui.StrokePathBuilder(1)

	var points []gpui.Point
	var first, last float32
	var hasFirst, hasLast bool
	for index, v := range a.data {
		if index == 0 {
			first, hasFirst = a.x(v)
		}
		if index == len(a.data)-1 {
			last, hasLast = a.x(v)
		}
		x, okX := a.x(v)
		y, okY := a.y1(v)
		if okX && okY {
			points = append(points, OriginPoint(x, y, origin))
		}
	}

	if len(points) == 0 {
		return nil, false, nil, false
	}
	if len(points) == 1 {
		area.MoveTo(points[0].X, points[0].Y)
		stroke.MoveTo(points[0].X, points[0].Y)
		fill, fillOK = plotBuild(area)
		line, lineOK = plotBuild(stroke)
		return
	}

	plotCurveSegments(a.curve, points, area, stroke)

	// Close the path.
	if hasFirst && hasLast && a.hasY0 {
		p := OriginPoint(last, a.y0, origin)
		area.LineTo(p.X, p.Y)
		p = OriginPoint(first, a.y0, origin)
		area.LineTo(p.X, p.Y)
		area.Close()
	}

	fill, fillOK = plotBuild(area)
	line, lineOK = plotBuild(stroke)
	return
}

// PaintCached paints the area, reusing the fill and stroke tessellated by
// an earlier paint while the projected points, baseline and curve style
// are unchanged (U:plot/shape/area.rs:181-217). fill and line are the two
// caches this area keeps together (PathCaches.SlotPair).
func (a Area[T]) PaintCached(bounds gpui.Bounds, fill, line *PathCache, w *gpui.Window) {
	type extra struct {
		curve Curve
		y0    uint32
		hasY0 bool
	}
	k := extra{curve: a.curve, hasY0: a.hasY0}
	if a.hasY0 {
		k.y0 = math.Float32bits(a.y0)
	}
	key := NewShapeKey(k)
	for _, v := range a.data {
		x, okX := a.x(v)
		y, okY := a.y1(v)
		if okX && okY {
			key.F32(x).F32(y)
		}
	}
	sum := key.Finish()
	local := gpui.Bounds{Size: bounds.Size}
	// One miss builds both paths; the second cache takes the stroke from
	// the stash instead of building again.
	var stashed *gpui.Path
	var stashedOK, hasStash bool
	fillPath, fillOK := fill.Get(sum, bounds.Origin, func() (*gpui.Path, bool) {
		area, areaOK, stroke, strokeOK := a.path(local)
		stashed, stashedOK, hasStash = stroke, strokeOK, true
		return area, areaOK
	})
	linePath, lineOK := line.Get(sum, bounds.Origin, func() (*gpui.Path, bool) {
		if hasStash {
			hasStash = false
			return stashed, stashedOK
		}
		_, _, stroke, strokeOK := a.path(local)
		return stroke, strokeOK
	})
	if fillOK {
		w.PaintPath(fillPath, a.fill)
	}
	if lineOK {
		w.PaintPath(linePath, a.stroke)
	}
}

// Paint paints the area (U:plot/shape/area.rs:219-229).
func (a Area[T]) Paint(bounds gpui.Bounds, w *gpui.Window) {
	area, areaOK, line, lineOK := a.path(bounds)
	if areaOK {
		w.PaintPath(area, a.fill)
	}
	if lineOK {
		w.PaintPath(line, a.stroke)
	}
}
