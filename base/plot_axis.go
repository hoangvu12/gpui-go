package base

// Axis lines and their tick labels (U:plot/axis.rs). A line is a 1px
// stroked path (gpui.Window.PaintPath, R:window.rs:4715); labels paint
// through PlotLabel.
//
// Go adaptations: AxisText's builders sharing a field name take the With
// prefix; `Option<Pixels>` positions are a value plus a flag; the label
// iterators are slices.

import "github.com/hoangvu12/gpui-go/gpui"

// AxisGap is the x-axis gutter for labels at the default PlotTextSize
// (U:plot/axis.rs:11-16).
//
// Deprecated: since 0.7.0; use AxisGutter with the label font size the
// chart draws.
const AxisGap float32 = 18

// AxisGutter is the space below (or above) an x-axis line that tick labels
// of fontSize need: the gap PlotAxis leaves between the line and the
// labels, the labels themselves, and a trailing gap
// (U:plot/axis.rs:18-27). With the default PlotTextSize it is 18px.
func AxisGutter(fontSize float32) float32 {
	return fontSize + PlotTextGap*4
}

// AxisLabelSide is which side of an axis line the tick labels render on
// (U:plot/axis.rs:29-37). End is the default.
type AxisLabelSide uint8

const (
	// AxisLabelSideEnd: x-axis labels below the line, y-axis labels right
	// of the line (the default).
	AxisLabelSideEnd AxisLabelSide = iota
	// AxisLabelSideStart: x-axis labels above the line, y-axis labels left
	// of the line.
	AxisLabelSideStart
)

// AxisLabelPlacement is where a chart draws the tick labels of its value
// axis (U:plot/axis.rs:39-48). Outside is the default.
type AxisLabelPlacement uint8

const (
	// AxisLabelPlacementOutside: in a gutter beside the plot, which the
	// plot shrinks to make room for (the default).
	AxisLabelPlacementOutside AxisLabelPlacement = iota
	// AxisLabelPlacementInside: over the plot's edge, beside the grid line
	// each label reads, so the plot keeps its full size.
	AxisLabelPlacementInside
)

// AxisText is a tick label on a PlotAxis: its text, where along the axis
// it sits and how it is drawn (U:plot/axis.rs:50-59).
type AxisText struct {
	Text     string
	Tick     float32
	Color    gpui.Hsla
	FontSize float32
	Align    gpui.TextAlign
}

// NewAxisText returns a left-aligned label at PlotTextSize
// (U:plot/axis.rs:62-70).
func NewAxisText(text string, tick float32, color gpui.Hsla) AxisText {
	return AxisText{Text: text, Tick: tick, Color: color, FontSize: PlotTextSize, Align: gpui.TextAlignLeft}
}

// WithFontSize sets the font size (U:plot/axis.rs:72-75).
func (t AxisText) WithFontSize(fontSize float32) AxisText { t.FontSize = fontSize; return t }

// WithAlign sets the alignment (U:plot/axis.rs:77-80).
func (t AxisText) WithAlign(align gpui.TextAlign) AxisText { t.Align = align; return t }

// PlotAxis is axis lines and their tick labels (U:plot/axis.rs:83-98).
// The builders only record values, combined when the axis paints, so they
// can be set in any order.
type PlotAxis struct {
	x          float32
	hasX       bool
	xLabels    []AxisText
	xAxis      bool
	xLabelSide AxisLabelSide
	y          float32
	hasY       bool
	yLabels    []AxisText
	yAxis      bool
	yLabelSide AxisLabelSide
	stroke     gpui.Background
}

// NewPlotAxis returns an axis with the x line shown and the y line hidden,
// a transparent stroke and no positions (U:plot/axis.rs:100-119).
func NewPlotAxis() PlotAxis {
	return PlotAxis{xAxis: true, stroke: gpui.SolidBackground(gpui.TransparentBlack())}
}

// X places the x-axis line at position from the top of the plot. Without
// it the x-axis draws neither its line nor its labels
// (U:plot/axis.rs:121-126).
func (a PlotAxis) X(position float32) PlotAxis { a.x, a.hasX = position, true; return a }

// XAxis shows or hides the x-axis line; its labels are drawn either way.
// Default is true (U:plot/axis.rs:128-134).
func (a PlotAxis) XAxis(xAxis bool) PlotAxis { a.xAxis = xAxis; return a }

// XLabel sets the tick labels of the x-axis (U:plot/axis.rs:136-140).
func (a PlotAxis) XLabel(labels ...AxisText) PlotAxis {
	a.xLabels = append([]AxisText(nil), labels...)
	return a
}

// XLabelSide sets which side of the x-axis line tick labels render on
// (U:plot/axis.rs:142-146).
func (a PlotAxis) XLabelSide(side AxisLabelSide) PlotAxis { a.xLabelSide = side; return a }

// Y places the y-axis line at position from the left of the plot. Without
// it the y-axis draws neither its line nor its labels
// (U:plot/axis.rs:148-153).
func (a PlotAxis) Y(position float32) PlotAxis { a.y, a.hasY = position, true; return a }

// YAxis shows or hides the y-axis line; its labels are drawn either way.
// Default is false (U:plot/axis.rs:155-161).
func (a PlotAxis) YAxis(yAxis bool) PlotAxis { a.yAxis = yAxis; return a }

// YLabel sets the tick labels of the y-axis (U:plot/axis.rs:163-167).
func (a PlotAxis) YLabel(labels ...AxisText) PlotAxis {
	a.yLabels = append([]AxisText(nil), labels...)
	return a
}

// YLabelSide sets which side of the y-axis line tick labels render on
// (U:plot/axis.rs:169-173).
func (a PlotAxis) YLabelSide(side AxisLabelSide) PlotAxis { a.yLabelSide = side; return a }

// Stroke sets the stroke of the axis lines (U:plot/axis.rs:175-179).
func (a PlotAxis) Stroke(stroke gpui.Background) PlotAxis { a.stroke = stroke; return a }

// xTexts are the x-axis labels placed against the line at x
// (U:plot/axis.rs:181-193).
func (a PlotAxis) xTexts(x float32) []PlotText {
	out := make([]PlotText, 0, len(a.xLabels))
	for _, t := range a.xLabels {
		var y float32
		switch a.xLabelSide {
		case AxisLabelSideStart:
			y = x - (PlotTextGap + PlotTextHeight)
		default:
			y = x + PlotTextGap*3
		}
		out = append(out, plotAxisText(t, gpui.Point{X: t.Tick, Y: y}))
	}
	return out
}

// yTexts are the y-axis labels placed against the line at y
// (U:plot/axis.rs:195-207).
func (a PlotAxis) yTexts(y float32) []PlotText {
	out := make([]PlotText, 0, len(a.yLabels))
	for _, t := range a.yLabels {
		var x float32
		switch a.yLabelSide {
		case AxisLabelSideStart:
			x = y - PlotTextGap
		default:
			x = y + PlotTextGap
		}
		out = append(out, plotAxisText(t, gpui.Point{X: x, Y: t.Tick - PlotTextSize/2}))
	}
	return out
}

// drawAxis strokes one axis line at 1px (U:plot/axis.rs:209-216).
func (a PlotAxis) drawAxis(start, end gpui.Point, w *gpui.Window) {
	b := gpui.StrokePathBuilder(1)
	b.MoveTo(start.X, start.Y)
	b.LineTo(end.X, end.Y)
	if path, ok := plotBuild(b); ok {
		w.PaintPath(path, a.stroke)
	}
}

// Paint paints the axis (U:plot/axis.rs:218-243).
func (a PlotAxis) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	origin := bounds.Origin
	if a.hasX {
		if a.xAxis {
			a.drawAxis(OriginPoint(0, a.x, origin), OriginPoint(bounds.Size.Width, a.x, origin), w)
		}
		NewPlotLabel(a.xTexts(a.x)).Paint(bounds, w, app)
	}
	if a.hasY {
		if a.yAxis {
			a.drawAxis(OriginPoint(a.y, 0, origin), OriginPoint(a.y, bounds.Size.Height, origin), w)
		}
		NewPlotLabel(a.yTexts(a.y)).Paint(bounds, w, app)
	}
}

// plotAxisText is label as the text PlotLabel draws at origin
// (U:plot/axis.rs:246-252).
func plotAxisText(label AxisText, origin gpui.Point) PlotText {
	return NewPlotText(label.Text, origin, label.Color).
		WithFontSize(label.FontSize).
		WithFontWeight(gpui.DefaultFontWeight).
		WithAlign(label.Align)
}
