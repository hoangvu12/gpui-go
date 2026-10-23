package base

// The bar shape (U:plot/shape/bar.rs).
//
// Go adaptations (recorded deviations):
//
//   - Accessors are funcs of the datum; `Option<f32>` returns are (value,
//     ok).
//   - The fill closure returns a solid gpui.Background (decision 02): the
//     upstream doc invites a gpui::linear_gradient
//     (U:plot/shape/bar.rs:141-146), which the frozen native scene cannot
//     paint (scene color tag 0 only). The frame is a gpui.Bounds (Pixels
//     and f32 are both float32).
//   - `label: Option<Box<dyn Fn>>` is a nil-able func.

import "github.com/hoangvu12/gpui-go/gpui"

// BarAlignment is the alignment of bars within a Bar shape, controlling
// both the orientation and the side the baseline lives on
// (U:plot/shape/bar.rs:10-23). Bottom is the default.
type BarAlignment uint8

const (
	// BarAlignmentBottom: vertical bars with the baseline at the bottom;
	// bars grow upward (the default).
	BarAlignmentBottom BarAlignment = iota
	// BarAlignmentTop: vertical bars with the baseline at the top; bars
	// grow downward.
	BarAlignmentTop
	// BarAlignmentLeft: horizontal bars with the baseline at the left;
	// bars grow rightward.
	BarAlignmentLeft
	// BarAlignmentRight: horizontal bars with the baseline at the right;
	// bars grow leftward.
	BarAlignmentRight
)

// IsHorizontal reports Left or Right (U:plot/shape/bar.rs:26-28).
func (a BarAlignment) IsHorizontal() bool {
	return a == BarAlignmentLeft || a == BarAlignmentRight
}

// IsVertical reports Bottom or Top (U:plot/shape/bar.rs:30-32).
func (a BarAlignment) IsVertical() bool { return !a.IsHorizontal() }

// GradientAngle is the linear-gradient angle, in degrees, that runs from
// the bar's base to its tip: 0° points upward and angles increase
// clockwise (U:plot/shape/bar.rs:34-46).
func (a BarAlignment) GradientAngle() float32 {
	switch a {
	case BarAlignmentTop:
		return 180
	case BarAlignmentLeft:
		return 90
	case BarAlignmentRight:
		return 270
	}
	return 0
}

// Bar paints one rectangle per datum (U:plot/shape/bar.rs:49-60).
type Bar[T any] struct {
	data        []T
	alignment   BarAlignment
	cross       func(datum T) (float32, bool)
	bandWidth   float32
	base        func(datum T) float32
	value       func(datum T) (float32, bool)
	fill        func(datum T, frame gpui.Bounds, alignment BarAlignment) gpui.Background
	label       func(datum T, origin gpui.Point) []PlotText
	cornerRadii gpui.Corners
}

// NewBar returns an empty bottom-aligned bar shape filling black
// (U:plot/shape/bar.rs:62-81).
func NewBar[T any]() Bar[T] {
	return Bar[T]{
		cross: plotNone[T],
		base:  func(T) float32 { return 0 },
		value: plotNone[T],
		fill: func(T, gpui.Bounds, BarAlignment) gpui.Background {
			return gpui.SolidBackground(gpui.Black())
		},
	}
}

// Data sets the data (U:plot/shape/bar.rs:83-90).
func (b Bar[T]) Data(data []T) Bar[T] { b.data = append([]T(nil), data...); return b }

// Alignment sets the alignment; Bottom by default
// (U:plot/shape/bar.rs:92-98).
func (b Bar[T]) Alignment(alignment BarAlignment) Bar[T] { b.alignment = alignment; return b }

// Cross sets the cross-axis position of each bar, in pixels: the X
// coordinate for vertical alignments, the Y coordinate for horizontal
// ones (U:plot/shape/bar.rs:100-110).
func (b Bar[T]) Cross(cross func(datum T) (float32, bool)) Bar[T] { b.cross = cross; return b }

// BandWidth sets the bar thickness along the cross axis
// (U:plot/shape/bar.rs:112-116).
func (b Bar[T]) BandWidth(bandWidth float32) Bar[T] { b.bandWidth = bandWidth; return b }

// Base sets the baseline position of each bar, in pixels along the value
// axis (U:plot/shape/bar.rs:118-125).
func (b Bar[T]) Base(base func(datum T) float32) Bar[T] { b.base = base; return b }

// Value sets the value-end position of each bar, in pixels along the
// value axis (U:plot/shape/bar.rs:127-134).
func (b Bar[T]) Value(value func(datum T) (float32, bool)) Bar[T] { b.value = value; return b }

// Fill sets the fill of each bar (U:plot/shape/bar.rs:136-154). The func
// receives the datum, the bar's painted frame relative to the plot bounds
// origin, and the bar's alignment.
func (b Bar[T]) Fill(fill func(datum T, frame gpui.Bounds, alignment BarAlignment) gpui.Background) Bar[T] {
	b.fill = fill
	return b
}

// Label sets the label of each bar (U:plot/shape/bar.rs:156-163).
func (b Bar[T]) Label(label func(datum T, origin gpui.Point) []PlotText) Bar[T] {
	b.label = label
	return b
}

// CornerRadii sets the corner radii applied to every bar rectangle
// (U:plot/shape/bar.rs:165-172).
func (b Bar[T]) CornerRadii(cornerRadii gpui.Corners) Bar[T] { b.cornerRadii = cornerRadii; return b }

// path resolves the bar quads and labels (U:plot/shape/bar.rs:174-229).
func (b Bar[T]) path(bounds gpui.Bounds) ([]gpui.PaintQuad, PlotLabel) {
	origin := bounds.Origin
	var quads []gpui.PaintQuad
	var labels []PlotText
	for _, v := range b.data {
		cross, ok := b.cross(v)
		if !ok {
			continue
		}
		value, ok := b.value(v)
		if !ok {
			continue
		}
		base := b.base(v)
		bw := b.bandWidth

		var frame gpui.Bounds
		var p1, p2 gpui.Point
		if b.alignment.IsVertical() {
			x0, x1 := cross, cross+bw
			yMin, yMax := rustMinF32(value, base), rustMaxF32(value, base)
			frame = gpui.Bounds{Origin: gpui.Point{X: x0, Y: yMin}, Size: gpui.Size{Width: x1 - x0, Height: yMax - yMin}}
			p1, p2 = OriginPoint(x0, yMin, origin), OriginPoint(x1, yMax, origin)
		} else {
			y0, y1 := cross, cross+bw
			xMin, xMax := rustMinF32(value, base), rustMaxF32(value, base)
			frame = gpui.Bounds{Origin: gpui.Point{X: xMin, Y: y0}, Size: gpui.Size{Width: xMax - xMin, Height: y1 - y0}}
			p1, p2 = OriginPoint(xMin, y0, origin), OriginPoint(xMax, y1, origin)
		}

		bg := b.fill(v, frame, b.alignment)
		quad := gpui.Fill(plotBoundsFromCorners(p1, p2), bg)
		quad.CornerRadii = b.cornerRadii
		quads = append(quads, quad)

		if b.label != nil {
			labels = append(labels, b.label(v, plotBarLabelOrigin(b.alignment, cross, base, value, bw))...)
		}
	}
	return quads, NewPlotLabel(labels)
}

// plotBoundsFromCorners is Bounds::from_corners (R:geometry.rs:849): the
// bounds from the top-left to the bottom-right corner.
func plotBoundsFromCorners(topLeft, bottomRight gpui.Point) gpui.Bounds {
	return gpui.Bounds{
		Origin: topLeft,
		Size:   gpui.Size{Width: bottomRight.X - topLeft.X, Height: bottomRight.Y - topLeft.Y},
	}
}

// Paint paints the bars and their labels (U:plot/shape/bar.rs:231-238).
func (b Bar[T]) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	quads, labels := b.path(bounds)
	for _, q := range quads {
		w.PaintQuad(q)
	}
	labels.Paint(bounds, w, app)
}

// plotBarLabelOrigin is the origin of a bar label, outside the bar at the
// value end (U:plot/shape/bar.rs:241-291). The caller chooses the
// alignment of the text.
func plotBarLabelOrigin(alignment BarAlignment, cross, base, value, bandWidth float32) gpui.Point {
	switch alignment {
	case BarAlignmentTop:
		cx := cross + bandWidth/2
		// Normal: value > base (bar grows down). Label below the bar end.
		if value >= base {
			return gpui.Point{X: cx, Y: value + PlotTextGap}
		}
		return gpui.Point{X: cx, Y: value - PlotTextHeight}
	case BarAlignmentLeft:
		// Vertical centering: the text origin is the top of the glyph cell.
		cy := cross + bandWidth/2 - PlotTextSize/2
		// Normal: value > base (bar grows right). Label right of the end.
		if value >= base {
			return gpui.Point{X: value + PlotTextGap, Y: cy}
		}
		return gpui.Point{X: value - PlotTextGap, Y: cy}
	case BarAlignmentRight:
		cy := cross + bandWidth/2 - PlotTextSize/2
		// Normal: value < base (bar grows left). Label left of the end.
		if value <= base {
			return gpui.Point{X: value - PlotTextGap, Y: cy}
		}
		return gpui.Point{X: value + PlotTextGap, Y: cy}
	}
	cx := cross + bandWidth/2
	// Normal: value < base (bar grows up). Label above the bar end.
	if value <= base {
		return gpui.Point{X: cx, Y: value - PlotTextHeight}
	}
	return gpui.Point{X: cx, Y: value + PlotTextGap}
}
