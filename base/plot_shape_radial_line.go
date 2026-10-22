package base

// The radial line generator (U:plot/shape/radial_line.rs, after
// d3.lineRadial).
//
// Go adaptations: accessors are funcs of the datum and its index
// returning (value, ok); `fill: Option<Background>` is a value plus a
// flag; colors are solid (decision 02).

import "github.com/hoangvu12/gpui-go/gpui"

// RadialLine places points around the center of the plot bounds
// (U:plot/shape/radial_line.rs:12-39): the angle accessor returns radians
// with 0 at 12 o'clock and positive angles proceeding clockwise, the
// radius accessor the distance in pixels from the center. Closed connects
// the last point back to the first and Fill fills the enclosed polygon,
// e.g. for radar charts.
type RadialLine[T any] struct {
	data        []T
	angle       func(datum T, index int) (float32, bool)
	radius      func(datum T, index int) (float32, bool)
	closed      bool
	fill        gpui.Background
	hasFill     bool
	stroke      gpui.Background
	strokeWidth float32
	dot         bool
	dotSize     float32
	dotFill     gpui.Background
	dotStroke   gpui.Hsla
	hasDotColor bool
}

func plotNoneIndexed[T any](T, int) (float32, bool) { return 0, false }

// NewRadialLine returns an empty open radial line with a 1px stroke and
// 4px transparent dots (U:plot/shape/radial_line.rs:41-62).
func NewRadialLine[T any]() RadialLine[T] {
	return RadialLine[T]{
		angle:       plotNoneIndexed[T],
		radius:      plotNoneIndexed[T],
		strokeWidth: 1,
		dotSize:     4,
		dotFill:     gpui.SolidBackground(gpui.TransparentBlack()),
	}
}

// Data sets the data (U:plot/shape/radial_line.rs:64-71).
func (r RadialLine[T]) Data(data []T) RadialLine[T] { r.data = append([]T(nil), data...); return r }

// Angle sets the angle accessor (U:plot/shape/radial_line.rs:73-84).
func (r RadialLine[T]) Angle(angle func(datum T, index int) (float32, bool)) RadialLine[T] {
	r.angle = angle
	return r
}

// Radius sets the radius accessor (U:plot/shape/radial_line.rs:86-96).
func (r RadialLine[T]) Radius(radius func(datum T, index int) (float32, bool)) RadialLine[T] {
	r.radius = radius
	return r
}

// Closed connects the last point back to the first, like
// d3.curveLinearClosed (U:plot/shape/radial_line.rs:98-102).
func (r RadialLine[T]) Closed() RadialLine[T] { r.closed = true; return r }

// Fill sets the fill of the enclosed polygon, which is always closed
// (U:plot/shape/radial_line.rs:104-110).
func (r RadialLine[T]) Fill(fill gpui.Background) RadialLine[T] {
	r.fill, r.hasFill = fill, true
	return r
}

// Stroke sets the stroke (U:plot/shape/radial_line.rs:112-116).
func (r RadialLine[T]) Stroke(stroke gpui.Background) RadialLine[T] { r.stroke = stroke; return r }

// StrokeWidth sets the stroke width (U:plot/shape/radial_line.rs:118-122).
func (r RadialLine[T]) StrokeWidth(strokeWidth float32) RadialLine[T] {
	r.strokeWidth = strokeWidth
	return r
}

// Dot draws a dot on every point (U:plot/shape/radial_line.rs:124-128).
func (r RadialLine[T]) Dot() RadialLine[T] { r.dot = true; return r }

// DotSize sets the size of the dots (U:plot/shape/radial_line.rs:130-134).
func (r RadialLine[T]) DotSize(dotSize float32) RadialLine[T] { r.dotSize = dotSize; return r }

// DotFill sets the fill of the dots (U:plot/shape/radial_line.rs:136-140).
func (r RadialLine[T]) DotFill(fill gpui.Background) RadialLine[T] { r.dotFill = fill; return r }

// DotStroke sets the 1px border color of the dots; it defaults to the dot
// fill (U:plot/shape/radial_line.rs:142-147).
func (r RadialLine[T]) DotStroke(stroke gpui.Hsla) RadialLine[T] {
	r.dotStroke, r.hasDotColor = stroke, true
	return r
}

// DotFillColor is DotFill with a color
// (U:plot/shape/radial_line.rs:149-152).
//
// Deprecated: since 0.7.0; use DotFill.
func (r RadialLine[T]) DotFillColor(color gpui.Hsla) RadialLine[T] {
	return r.DotFill(gpui.SolidBackground(color))
}

// DotStrokeColor is DotStroke (U:plot/shape/radial_line.rs:154-157).
//
// Deprecated: since 0.7.0; use DotStroke.
func (r RadialLine[T]) DotStrokeColor(color gpui.Hsla) RadialLine[T] { return r.DotStroke(color) }

// points resolves the data to points around the center of the bounds
// (U:plot/shape/radial_line.rs:173-191).
func (r RadialLine[T]) points(bounds gpui.Bounds) []gpui.Point {
	centerX := bounds.Origin.X + bounds.Size.Width/2
	centerY := bounds.Origin.Y + bounds.Size.Height/2
	var out []gpui.Point
	for i, v := range r.data {
		angle, ok := r.angle(v, i)
		if !ok {
			continue
		}
		angle -= plotHalfPi
		radius, ok := r.radius(v, i)
		if !ok {
			continue
		}
		out = append(out, gpui.Point{X: centerX + radius*cos32(angle), Y: centerY + radius*sin32(angle)})
	}
	return out
}

// radialPaths are the fill path, the stroke path and the dot quads.
type radialPaths struct {
	fill   *gpui.Path
	stroke *gpui.Path
	dots   []gpui.PaintQuad
}

// path builds the fill, stroke and dots (U:plot/shape/radial_line.rs:193-231).
func (r RadialLine[T]) path(bounds gpui.Bounds) radialPaths {
	points := r.points(bounds)
	var out radialPaths
	if r.dot {
		radius := r.dotSize / 2
		for _, p := range points {
			out.dots = append(out.dots, plotDotQuad(gpui.Point{X: p.X - radius, Y: p.Y - radius}, r.dotSize, r.dotFill, r.dotStroke, r.hasDotColor))
		}
	}
	if len(points) == 0 {
		return out
	}
	if r.hasFill && len(points) >= 3 {
		b := gpui.FillPathBuilder()
		b.AddPolygon(points, true)
		out.fill, _ = plotBuild(b)
	}
	b := gpui.StrokePathBuilder(r.strokeWidth)
	b.MoveTo(points[0].X, points[0].Y)
	for _, p := range points[1:] {
		b.LineTo(p.X, p.Y)
	}
	if r.closed && len(points) > 2 {
		b.Close()
	}
	out.stroke, _ = plotBuild(b)
	return out
}

// Paint paints the radial line (U:plot/shape/radial_line.rs:233-246).
func (r RadialLine[T]) Paint(bounds gpui.Bounds, w *gpui.Window) {
	paths := r.path(bounds)
	if paths.fill != nil && r.hasFill {
		w.PaintPath(paths.fill, r.fill)
	}
	if paths.stroke != nil {
		w.PaintPath(paths.stroke, r.stroke)
	}
	for _, q := range paths.dots {
		w.PaintQuad(q)
	}
}
