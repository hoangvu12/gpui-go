package main

import (
	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

type sample struct {
	month string
	value float32
}

var samples = []sample{
	{"Jan", 186}, {"Feb", 305}, {"Mar", 237}, {"Apr", 73}, {"May", 209}, {"Jun", 214},
}

// revenuePlot is a bar chart with a line overlay, drawn with Base's plot
// scales and shapes (U:plot/*). It embeds base.PlotDefaults for the
// optional Plot methods and opts in to the appear motion with an id.
type revenuePlot struct {
	base.PlotDefaults
}

func (revenuePlot) ID() (gpui.ElementID, bool) { return gpui.ElementIDOf("revenue-plot"), true }

func (revenuePlot) AppearGeneration() (uint64, bool) { return 1, true }

const plotGutter float32 = 28

func (revenuePlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	width, height := bounds.Size.Width, bounds.Size.Height
	months := make([]string, len(samples))
	for i, s := range samples {
		months[i] = s.month
	}
	x := base.NewScaleBand(months, [2]float32{plotGutter, width}).PaddingInner(0.3).PaddingOuter(0.2)
	y := base.NewScaleLinear([]float32{0, 320}, [2]float32{height - base.AxisGap, 8})

	var gridY []float32
	var yLabels []base.AxisText
	for _, v := range []float32{0, 100, 200, 300} {
		if t, ok := y.Tick(v); ok {
			gridY = append(gridY, t)
			yLabels = append(yLabels, base.NewAxisText(fmtInt(v), t, colMuted))
		}
	}
	base.NewGrid().Y(gridY...).Stroke(gpui.SolidBackground(colBorder)).DashArray([]float32{4, 2}).Paint(bounds, w)

	var xLabels []base.AxisText
	for _, m := range months {
		if t, ok := x.Tick(m); ok {
			xLabels = append(xLabels, base.NewAxisText(m, t+x.BandWidth()/2, colMuted).WithAlign(gpui.TextAlignCenter))
		}
	}
	base.NewPlotAxis().
		X(height-base.AxisGap).XLabel(xLabels...).
		Y(plotGutter).YLabel(yLabels...).
		Stroke(gpui.SolidBackground(colBorder)).
		Paint(bounds, w, app)

	baseline, _ := y.Tick(0)
	base.NewBar[sample]().
		Data(samples).
		BandWidth(x.BandWidth()).
		Cross(func(s sample) (float32, bool) { return x.Tick(s.month) }).
		Base(func(sample) float32 { return baseline }).
		Value(func(s sample) (float32, bool) { return y.Tick(s.value) }).
		Fill(func(sample, gpui.Bounds, base.BarAlignment) gpui.Background {
			return gpui.SolidBackground(colAccent.Opacity(0.8))
		}).
		CornerRadii(gpui.Corners{TopLeft: 3, TopRight: 3}).
		Paint(bounds, w, app)

	base.NewLine[sample]().
		Data(samples).
		X(func(s sample) (float32, bool) {
			t, ok := x.Tick(s.month)
			return t + x.BandWidth()/2, ok
		}).
		Y(func(s sample) (float32, bool) { return y.Tick(s.value) }).
		Stroke(gpui.SolidBackground(colForeground)).
		StrokeWidth(1.5).
		Curve(base.CurveNatural).
		Dot().DotFillColor(colSurface).DotStrokeColor(colForeground).
		Paint(bounds, w)
}

func fmtInt(v float32) string {
	n := int(v)
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func plotExample() any {
	return gpui.Div().W(gpui.Px(360)).H(gpui.Px(200)).Child(base.NewPlotElement(revenuePlot{}))
}
