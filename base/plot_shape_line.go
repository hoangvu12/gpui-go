package base

// The line generator (U:plot/shape/line.rs, after d3-shape's line).
//
// Go adaptations (recorded deviations):
//
//   - The boxed `Fn(&T) -> Option<f32>` accessors are funcs of the datum
//     returning (value, ok).
//   - Stroke and dot fill are solid gpui.Backgrounds (decision 02), so
//     the dot border's `dot_fill.as_solid()` fallback always applies.
//   - The deprecated dot_fill_color / dot_stroke_color aliases are
//     DotFillColor / DotStrokeColor.

import (
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Line is a line through projected data points
// (U:plot/shape/line.rs:10-22).
type Line[T any] struct {
	data        []T
	x, y        func(datum T) (float32, bool)
	stroke      gpui.Background
	strokeWidth float32
	curve       Curve
	dot         bool
	dotSize     float32
	dotFill     gpui.Background
	dotStroke   gpui.Hsla
	hasDotColor bool
}

func plotNone[T any](T) (float32, bool) { return 0, false }

// NewLine returns an empty natural-curve line with a 1px stroke and 4px
// transparent dots (U:plot/shape/line.rs:24-44).
func NewLine[T any]() Line[T] {
	return Line[T]{
		x:           plotNone[T],
		y:           plotNone[T],
		strokeWidth: 1,
		dotSize:     4,
		dotFill:     gpui.SolidBackground(gpui.TransparentBlack()),
	}
}

// Data sets the data (U:plot/shape/line.rs:46-53).
func (l Line[T]) Data(data []T) Line[T] { l.data = append([]T(nil), data...); return l }

// X sets the x accessor (U:plot/shape/line.rs:55-62).
func (l Line[T]) X(x func(datum T) (float32, bool)) Line[T] { l.x = x; return l }

// Y sets the y accessor (U:plot/shape/line.rs:64-71).
func (l Line[T]) Y(y func(datum T) (float32, bool)) Line[T] { l.y = y; return l }

// Stroke sets the stroke (U:plot/shape/line.rs:73-77).
func (l Line[T]) Stroke(stroke gpui.Background) Line[T] { l.stroke = stroke; return l }

// StrokeWidth sets the stroke width (U:plot/shape/line.rs:79-83).
func (l Line[T]) StrokeWidth(strokeWidth float32) Line[T] { l.strokeWidth = strokeWidth; return l }

// Curve sets how the line connects its points; Natural by default
// (U:plot/shape/line.rs:85-89).
func (l Line[T]) Curve(curve Curve) Line[T] { l.curve = curve; return l }

// Dot draws a dot on every point (U:plot/shape/line.rs:91-95).
func (l Line[T]) Dot() Line[T] { l.dot = true; return l }

// DotSize sets the size of the dots (U:plot/shape/line.rs:97-101).
func (l Line[T]) DotSize(dotSize float32) Line[T] { l.dotSize = dotSize; return l }

// DotFill sets the fill of the dots (U:plot/shape/line.rs:103-107).
func (l Line[T]) DotFill(fill gpui.Background) Line[T] { l.dotFill = fill; return l }

// DotStroke sets the 1px border color of the dots; it defaults to the dot
// fill (U:plot/shape/line.rs:109-114).
func (l Line[T]) DotStroke(stroke gpui.Hsla) Line[T] {
	l.dotStroke, l.hasDotColor = stroke, true
	return l
}

// DotFillColor is DotFill with a color (U:plot/shape/line.rs:116-119).
//
// Deprecated: since 0.7.0; use DotFill.
func (l Line[T]) DotFillColor(color gpui.Hsla) Line[T] { return l.DotFill(gpui.SolidBackground(color)) }

// DotStrokeColor is DotStroke (U:plot/shape/line.rs:121-124).
//
// Deprecated: since 0.7.0; use DotStroke.
func (l Line[T]) DotStrokeColor(color gpui.Hsla) Line[T] { return l.DotStroke(color) }

// plotDotQuad is the round dot quad shared by Line and RadialLine
// (U:plot/shape/line.rs:126-138, the pinned quad(), R:window.rs:7891).
func plotDotQuad(origin gpui.Point, dotSize float32, fill gpui.Background, stroke gpui.Hsla, hasStroke bool) gpui.PaintQuad {
	border := fill.AsSolid()
	if hasStroke {
		border = stroke
	}
	r := dotSize / 2
	return gpui.PaintQuad{
		Bounds:             gpui.Bounds{Origin: origin, Size: gpui.Size{Width: dotSize, Height: dotSize}},
		CornerRadii:        gpui.Corners{TopLeft: r, TopRight: r, BottomRight: r, BottomLeft: r},
		Background:         fill,
		BorderWidths:       gpui.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
		BorderColor:        gpui.SolidBackground(border),
		BorderStyle:        gpui.BorderStyleSolid,
		BorderDashedLength: gpui.DefaultBorderDashedLength,
		BorderDashedGap:    gpui.DefaultBorderDashedGap,
	}
}

// dots are the projected points relative to origin, and the dot quads to
// paint (U:plot/shape/line.rs:140-163).
func (l Line[T]) dots(origin gpui.Point) ([]gpui.Point, []gpui.PaintQuad) {
	var points []gpui.Point
	var quads []gpui.PaintQuad
	for _, v := range l.data {
		x, okX := l.x(v)
		y, okY := l.y(v)
		if !okX || !okY {
			continue
		}
		if l.dot {
			radius := l.dotSize / 2
			quads = append(quads, plotDotQuad(OriginPoint(x-radius, y-radius, origin), l.dotSize, l.dotFill, l.dotStroke, l.hasDotColor))
		}
		points = append(points, OriginPoint(x, y, origin))
	}
	return points, quads
}

// plotCurveSegments adds the segments of curve through points to the
// builders after a move to the first point (the Natural / Linear /
// StepAfter arms shared by U:plot/shape/line.rs:178-211 and
// U:plot/shape/area.rs:125-169). points has at least two entries.
func plotCurveSegments(curve Curve, points []gpui.Point, builders ...*gpui.PathBuilder) {
	for _, b := range builders {
		b.MoveTo(points[0].X, points[0].Y)
	}
	n := len(points)
	switch curve {
	case CurveLinear:
		for _, p := range points[1:] {
			for _, b := range builders {
				b.LineTo(p.X, p.Y)
			}
		}
	case CurveStepAfter:
		for i := 0; i+1 < n; i++ {
			p0, p1 := points[i], points[i+1]
			for _, b := range builders {
				b.LineTo(p1.X, p0.Y)
			}
			// Don't draw the vertical line for the last point.
			if i < n-2 {
				for _, b := range builders {
					b.LineTo(p1.X, p1.Y)
				}
			}
		}
	default:
		for i := 0; i < n-1; i++ {
			p0 := points[0]
			if i > 0 {
				p0 = points[i-1]
			}
			p1, p2 := points[i], points[i+1]
			p3 := points[n-1]
			if i+2 < n {
				p3 = points[i+2]
			}
			// Catmull-Rom to Bezier.
			c1x, c1y := p1.X+(p2.X-p0.X)/6.0, p1.Y+(p2.Y-p0.Y)/6.0
			c2x, c2y := p2.X-(p3.X-p1.X)/6.0, p2.Y-(p3.Y-p1.Y)/6.0
			for _, b := range builders {
				b.CubicBezierTo(p2.X, p2.Y, c1x, c1y, c2x, c2y)
			}
		}
	}
}

// buildPath is the stroke through dots (U:plot/shape/line.rs:165-214).
func (l Line[T]) buildPath(dots []gpui.Point) (*gpui.Path, bool) {
	b := gpui.StrokePathBuilder(l.strokeWidth)
	if len(dots) == 0 {
		return nil, false
	}
	if len(dots) == 1 {
		b.MoveTo(dots[0].X, dots[0].Y)
		return plotBuild(b)
	}
	plotCurveSegments(l.curve, dots, b)
	return plotBuild(b)
}

// path is the stroke and the dot quads at the bounds origin
// (U:plot/shape/line.rs:216-219).
func (l Line[T]) path(bounds gpui.Bounds) (*gpui.Path, bool, []gpui.PaintQuad) {
	dots, quads := l.dots(bounds.Origin)
	path, ok := l.buildPath(dots)
	return path, ok, quads
}

// PaintCached paints the line, reusing the stroke tessellated by an
// earlier paint while the projected points, stroke width and curve style
// are unchanged (U:plot/shape/line.rs:221-247). Use it from a Plot that
// keeps a PathCache per line.
func (l Line[T]) PaintCached(bounds gpui.Bounds, cache *PathCache, w *gpui.Window) {
	dots, quads := l.dots(gpui.Point{})
	type extra struct {
		curve Curve
		width uint32
	}
	key := NewShapeKey(extra{l.curve, math.Float32bits(l.strokeWidth)})
	for _, d := range dots {
		key.Point(d)
	}
	if path, ok := cache.Get(key.Finish(), bounds.Origin, func() (*gpui.Path, bool) { return l.buildPath(dots) }); ok {
		w.PaintPath(path, l.stroke)
	}
	// Dots are quads: cheap, and positioned at this frame's origin.
	for _, q := range quads {
		q.Bounds.Origin.X += bounds.Origin.X
		q.Bounds.Origin.Y += bounds.Origin.Y
		w.PaintQuad(q)
	}
}

// Paint paints the line (U:plot/shape/line.rs:249-258).
func (l Line[T]) Paint(bounds gpui.Bounds, w *gpui.Window) {
	path, ok, quads := l.path(bounds)
	if ok {
		w.PaintPath(path, l.stroke)
	}
	for _, q := range quads {
		w.PaintQuad(q)
	}
}
