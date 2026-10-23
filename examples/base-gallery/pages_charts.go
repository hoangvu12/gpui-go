package main

import (
	"math"
	"strconv"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var chartPages = []page{
	{"plot-line", "Line chart", "plot/shape/line.rs", (*gallery).linePage},
	{"plot-area", "Area chart", "plot/shape/area.rs", (*gallery).areaPage},
	{"plot-bar", "Bar chart", "plot/shape/bar.rs", (*gallery).barPage},
	{"plot-pie", "Pie & arc", "plot/shape/pie.rs, plot/shape/arc.rs", (*gallery).piePage},
	{"plot-radial", "Radial line", "plot/shape/radial_line.rs", (*gallery).radialPage},
	{"plot-stack", "Stack", "plot/shape/stack.rs", (*gallery).stackPage},
	{"plot-sankey", "Sankey", "plot/shape/sankey.rs", (*gallery).sankeyPage},
}

// sample is one month of chart data.
type sample struct {
	month   string
	desktop float32
	mobile  float32
	tablet  float32
}

var samples = []sample{
	{"Jan", 186, 80, 40}, {"Feb", 305, 200, 60}, {"Mar", 237, 120, 90},
	{"Apr", 73, 190, 50}, {"May", 209, 130, 110}, {"Jun", 214, 140, 70},
}

func months() []string {
	out := make([]string, len(samples))
	for i, s := range samples {
		out[i] = s.month
	}
	return out
}

func fmtNum(v float32) string { return strconv.Itoa(int(v)) }

const gutter float32 = 32

// frame is the shared cartesian setup: a y scale over [0, max], grid
// lines and axis labels. It returns the y scale.
func frame(bounds gpui.Bounds, max float32, ticks []float32, xLabels []base.AxisText, w *gpui.Window, app *gpui.App) base.ScaleLinear[float32] {
	height := bounds.Size.Height
	y := base.NewScaleLinear([]float32{0, max}, [2]float32{height - base.AxisGap, 8})
	var gridY []float32
	var yLabels []base.AxisText
	for _, v := range ticks {
		if t, ok := y.Tick(v); ok {
			gridY = append(gridY, t)
			yLabels = append(yLabels, base.NewAxisText(fmtNum(v), t, pal.muted))
		}
	}
	base.NewGrid().Y(gridY...).Stroke(gpui.SolidBackground(pal.border)).DashArray([]float32{4, 2}).Paint(bounds, w)
	base.NewPlotAxis().
		X(height-base.AxisGap).XLabel(xLabels...).
		Y(gutter).YLabel(yLabels...).
		Stroke(gpui.SolidBackground(pal.border)).
		Paint(bounds, w, app)
	return y
}

func pointLabels(x base.ScalePoint[string]) []base.AxisText {
	var out []base.AxisText
	for _, m := range months() {
		if t, ok := x.Tick(m); ok {
			out = append(out, base.NewAxisText(m, t, pal.muted).WithAlign(gpui.TextAlignCenter))
		}
	}
	return out
}

// plotBox sizes a plot element.
func plotBox(plot base.Plot, width, height float32) any {
	return gpui.Div().W(gpui.Px(width)).H(gpui.Px(height)).Child(base.NewPlotElement(plot))
}

func legend(labels ...string) any {
	r := gpui.Div().Flex().Gap4().TextXs().TextColor(pal.muted)
	for ix, label := range labels {
		r = r.Child(gpui.Div().Flex().ItemsCenter().Gap1().
			Child(gpui.Div().Size(gpui.Px(10)).RoundedSm().Bg(pal.series[ix%len(pal.series)])).
			Child(label))
	}
	return r
}

// ---- Line ----------------------------------------------------------------

type linePlot struct {
	base.PlotDefaults
	id    string
	curve base.Curve
	dots  bool
}

func (p linePlot) ID() (gpui.ElementID, bool)     { return gpui.ElementIDOf(p.id), true }
func (linePlot) AppearGeneration() (uint64, bool) { return 1, true }

func (p linePlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	x := base.NewScalePoint(months(), [2]float32{gutter + 12, bounds.Size.Width - 12})
	y := frame(bounds, 320, []float32{0, 100, 200, 300}, pointLabels(x), w, app)
	for ix, value := range []func(sample) float32{
		func(s sample) float32 { return s.desktop },
		func(s sample) float32 { return s.mobile },
	} {
		line := base.NewLine[sample]().
			Data(samples).
			X(func(s sample) (float32, bool) { return x.Tick(s.month) }).
			Y(func(s sample) (float32, bool) { return y.Tick(value(s)) }).
			Stroke(gpui.SolidBackground(pal.series[ix])).
			StrokeWidth(2).
			Curve(p.curve)
		if p.dots {
			line = line.Dot().DotSize(6).DotFillColor(pal.surface).DotStrokeColor(pal.series[ix])
		}
		line.Paint(bounds, w)
	}
}

func (g *gallery) linePage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Natural curve with dots", col(plotBox(linePlot{id: "line-natural", curve: base.CurveNatural, dots: true}, 520, 220), legend("Desktop", "Mobile"))),
		demo("Linear", plotBox(linePlot{id: "line-linear", curve: base.CurveLinear}, 520, 180)),
		demo("Step after", plotBox(linePlot{id: "line-step", curve: base.CurveStepAfter}, 520, 180)),
	)
}

// ---- Area ----------------------------------------------------------------

type areaPlot struct {
	base.PlotDefaults
	id      string
	curve   base.Curve
	overlay bool
}

func (p areaPlot) ID() (gpui.ElementID, bool)     { return gpui.ElementIDOf(p.id), true }
func (areaPlot) AppearGeneration() (uint64, bool) { return 1, true }

func (p areaPlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	x := base.NewScalePoint(months(), [2]float32{gutter, bounds.Size.Width})
	y := frame(bounds, 320, []float32{0, 100, 200, 300}, pointLabels(x), w, app)
	baseline, _ := y.Tick(0)
	series := []func(sample) float32{func(s sample) float32 { return s.desktop }}
	if p.overlay {
		series = append(series, func(s sample) float32 { return s.mobile })
	}
	for ix, value := range series {
		base.NewArea[sample]().
			Data(samples).
			X(func(s sample) (float32, bool) { return x.Tick(s.month) }).
			Y0(baseline).
			Y1(func(s sample) (float32, bool) { return y.Tick(value(s)) }).
			Fill(gpui.SolidBackground(pal.series[ix].Opacity(0.25))).
			Stroke(gpui.SolidBackground(pal.series[ix])).
			Curve(p.curve).
			Paint(bounds, w)
	}
}

func (g *gallery) areaPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Single series, natural curve", plotBox(areaPlot{id: "area-single", curve: base.CurveNatural}, 520, 220)),
		demo("Overlapping series, linear", col(plotBox(areaPlot{id: "area-overlay", curve: base.CurveLinear, overlay: true}, 520, 220), legend("Desktop", "Mobile"))),
	)
}

// ---- Bar -----------------------------------------------------------------

type barPlot struct {
	base.PlotDefaults
	id         string
	horizontal bool
}

func (p barPlot) ID() (gpui.ElementID, bool)     { return gpui.ElementIDOf(p.id), true }
func (barPlot) AppearGeneration() (uint64, bool) { return 1, true }

func (p barPlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	width, height := bounds.Size.Width, bounds.Size.Height
	if p.horizontal {
		y := base.NewScaleBand(months(), [2]float32{0, height}).PaddingInner(0.3).PaddingOuter(0.1)
		x := base.NewScaleLinear([]float32{0, 320}, [2]float32{gutter, width - 40})
		var labels []base.AxisText
		for _, m := range months() {
			if t, ok := y.Tick(m); ok {
				labels = append(labels, base.NewAxisText(m, t+y.BandWidth()/2, pal.muted))
			}
		}
		base.NewPlotAxis().Y(gutter).YLabel(labels...).Stroke(gpui.SolidBackground(pal.border)).Paint(bounds, w, app)
		start, _ := x.Tick(0)
		base.NewBar[sample]().
			Data(samples).
			Alignment(base.BarAlignmentLeft).
			BandWidth(y.BandWidth()).
			Cross(func(s sample) (float32, bool) { return y.Tick(s.month) }).
			Base(func(sample) float32 { return start }).
			Value(func(s sample) (float32, bool) { return x.Tick(s.desktop) }).
			Fill(func(sample, gpui.Bounds, base.BarAlignment) gpui.Background {
				return gpui.SolidBackground(pal.series[1])
			}).
			Label(func(s sample, origin gpui.Point) []base.PlotText {
				return []base.PlotText{base.NewPlotText(fmtNum(s.desktop), gpui.Point{X: origin.X + 4, Y: origin.Y}, pal.muted).
					WithAlign(gpui.TextAlignLeft)}
			}).
			CornerRadii(gpui.Corners{TopRight: 3, BottomRight: 3}).
			Paint(bounds, w, app)
		return
	}
	x := base.NewScaleBand(months(), [2]float32{gutter, width}).PaddingInner(0.3).PaddingOuter(0.2)
	var labels []base.AxisText
	for _, m := range months() {
		if t, ok := x.Tick(m); ok {
			labels = append(labels, base.NewAxisText(m, t+x.BandWidth()/2, pal.muted).WithAlign(gpui.TextAlignCenter))
		}
	}
	y := frame(bounds, 320, []float32{0, 100, 200, 300}, labels, w, app)
	baseline, _ := y.Tick(0)
	base.NewBar[sample]().
		Data(samples).
		BandWidth(x.BandWidth()).
		Cross(func(s sample) (float32, bool) { return x.Tick(s.month) }).
		Base(func(sample) float32 { return baseline }).
		Value(func(s sample) (float32, bool) { return y.Tick(s.desktop) }).
		Fill(func(s sample, _ gpui.Bounds, _ base.BarAlignment) gpui.Background {
			if s.desktop > 250 {
				return gpui.SolidBackground(pal.series[0])
			}
			return gpui.SolidBackground(pal.series[0].Opacity(0.6))
		}).
		CornerRadii(gpui.Corners{TopLeft: 4, TopRight: 4}).
		Paint(bounds, w, app)
}

func (g *gallery) barPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Vertical bars (values over 250 emphasized)", plotBox(barPlot{id: "bar-vertical"}, 520, 220)),
		demo("Horizontal bars with value labels", plotBox(barPlot{id: "bar-horizontal", horizontal: true}, 520, 220)),
	)
}

// ---- Pie & arc -----------------------------------------------------------

type share struct {
	label string
	value float32
}

var shares = []share{{"Chrome", 275}, {"Safari", 200}, {"Firefox", 187}, {"Edge", 173}, {"Other", 90}}

type piePlot struct {
	base.PlotDefaults
	id    string
	inner float32
	pad   float32
	half  bool
}

func (p piePlot) ID() (gpui.ElementID, bool)     { return gpui.ElementIDOf(p.id), true }
func (piePlot) AppearGeneration() (uint64, bool) { return 1, true }

func (p piePlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	radius := min(bounds.Size.Width, bounds.Size.Height)/2 - 4
	pie := base.NewPie[share]().Value(func(s share) (float32, bool) { return s.value, true }).PadAngle(p.pad)
	if p.half {
		pie = pie.StartAngle(-math.Pi / 2).EndAngle(math.Pi / 2)
		radius = min(bounds.Size.Width/2, bounds.Size.Height) - 8
	}
	arc := base.NewArc().InnerRadius(radius * p.inner).OuterRadius(radius)
	var labels []base.PlotText
	cx, cy := bounds.Size.Width/2, bounds.Size.Height/2
	if p.half {
		// Arcs center on their bounds: doubling the height puts the
		// center on the bottom edge, under the upper half circle.
		bounds.Size.Height *= 2
		cy = bounds.Size.Height / 2
	}
	for _, a := range pie.Arcs(shares) {
		arc.Paint(a, gpui.SolidBackground(pal.series[a.Index%len(pal.series)]), bounds, w)
		c := arc.Centroid(a)
		labels = append(labels, base.NewPlotText(fmtNum(a.Value), gpui.Point{X: cx + c.X, Y: cy + c.Y - base.PlotTextSize/2}, gpui.White()))
	}
	if p.inner > 0 && !p.half {
		labels = append(labels, base.NewPlotText("925 visitors", gpui.Point{X: cx, Y: cy - base.PlotTextSize/2}, pal.fg))
	}
	base.NewPlotLabel(labels).Paint(bounds, w, app)
}

func (g *gallery) piePage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	names := make([]string, len(shares))
	for i, s := range shares {
		names[i] = s.label
	}
	return col(
		demo("Pie with value labels", row(plotBox(piePlot{id: "pie-full"}, 220, 220), legend(names...))),
		demo("Donut with pad angle", row(plotBox(piePlot{id: "pie-donut", inner: 0.6, pad: 0.03}, 220, 220))),
		demo("Half donut (start and end angles)", plotBox(piePlot{id: "pie-half", inner: 0.55, pad: 0.02, half: true}, 300, 160)),
	)
}

// ---- Radial line ---------------------------------------------------------

type skill struct {
	name string
	a, b float32
}

var skills = []skill{{"Speed", 80, 55}, {"Power", 65, 85}, {"Range", 90, 60}, {"Focus", 50, 70}, {"Agility", 75, 40}, {"Craft", 60, 90}}

type radialPlot struct {
	base.PlotDefaults
	id string
}

func (p radialPlot) ID() (gpui.ElementID, bool) { return gpui.ElementIDOf(p.id), true }

func (p radialPlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	radius := min(bounds.Size.Width, bounds.Size.Height)/2 - 24
	step := float32(2*math.Pi) / float32(len(skills))
	angle := func(_ skill, ix int) (float32, bool) { return float32(ix) * step, true }
	// Rings and spokes.
	for _, f := range []float32{0.25, 0.5, 0.75, 1} {
		base.NewRadialLine[skill]().Data(skills).Angle(angle).
			Radius(func(skill, int) (float32, bool) { return radius * f, true }).
			Closed().Stroke(gpui.SolidBackground(pal.border)).Paint(bounds, w)
	}
	scale := func(v float32) float32 { return radius * v / 100 }
	for ix, value := range []func(skill) float32{func(s skill) float32 { return s.a }, func(s skill) float32 { return s.b }} {
		base.NewRadialLine[skill]().Data(skills).Angle(angle).
			Radius(func(s skill, _ int) (float32, bool) { return scale(value(s)), true }).
			Closed().
			Fill(gpui.SolidBackground(pal.series[ix].Opacity(0.2))).
			Stroke(gpui.SolidBackground(pal.series[ix])).StrokeWidth(2).
			Dot().DotSize(6).DotFillColor(pal.series[ix]).
			Paint(bounds, w)
	}
	cx, cy := bounds.Size.Width/2, bounds.Size.Height/2
	var labels []base.PlotText
	for ix, s := range skills {
		a := float32(ix)*step - math.Pi/2
		r := radius + 14
		labels = append(labels, base.NewPlotText(s.name,
			gpui.Point{X: cx + r*float32(math.Cos(float64(a))), Y: cy + r*float32(math.Sin(float64(a))) - base.PlotTextSize/2}, pal.muted))
	}
	base.NewPlotLabel(labels).Paint(bounds, w, app)
}

func (g *gallery) radialPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Radar chart: closed, filled radial lines with dots", col(plotBox(radialPlot{id: "radar"}, 320, 300), legend("Player A", "Player B"))),
	)
}

// ---- Stack ---------------------------------------------------------------

type stackPlot struct {
	base.PlotDefaults
	id string
}

func (p stackPlot) ID() (gpui.ElementID, bool)     { return gpui.ElementIDOf(p.id), true }
func (stackPlot) AppearGeneration() (uint64, bool) { return 1, true }

func (p stackPlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	x := base.NewScaleBand(months(), [2]float32{gutter, bounds.Size.Width}).PaddingInner(0.35).PaddingOuter(0.2)
	var labels []base.AxisText
	for _, m := range months() {
		if t, ok := x.Tick(m); ok {
			labels = append(labels, base.NewAxisText(m, t+x.BandWidth()/2, pal.muted).WithAlign(gpui.TextAlignCenter))
		}
	}
	y := frame(bounds, 560, []float32{0, 200, 400}, labels, w, app)
	series := base.NewStack[sample]().
		Data(samples).
		Keys("desktop", "mobile", "tablet").
		Value(func(s sample, key string) (float32, bool) {
			switch key {
			case "desktop":
				return s.desktop, true
			case "mobile":
				return s.mobile, true
			case "tablet":
				return s.tablet, true
			}
			return 0, false
		}).
		Series()
	for _, s := range series {
		color := pal.series[s.Index%len(pal.series)]
		top := s.Index == len(series)-1
		bar := base.NewBar[base.StackPoint[sample]]().
			Data(s.Points).
			BandWidth(x.BandWidth()).
			Cross(func(pt base.StackPoint[sample]) (float32, bool) { return x.Tick(pt.Data.month) }).
			Base(func(pt base.StackPoint[sample]) float32 { v, _ := y.Tick(pt.Y0); return v }).
			Value(func(pt base.StackPoint[sample]) (float32, bool) { return y.Tick(pt.Y1) }).
			Fill(func(base.StackPoint[sample], gpui.Bounds, base.BarAlignment) gpui.Background {
				return gpui.SolidBackground(color)
			})
		if top {
			bar = bar.CornerRadii(gpui.Corners{TopLeft: 4, TopRight: 4})
		}
		bar.Paint(bounds, w, app)
	}
}

func (g *gallery) stackPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Stacked bars from Stack series (desktop + mobile + tablet)", col(plotBox(stackPlot{id: "stacked"}, 520, 240), legend("Desktop", "Mobile", "Tablet"))),
	)
}

// ---- Sankey --------------------------------------------------------------

var sankeyNodes = []string{"Visits", "Search", "Direct", "Social", "Signup", "Bounce", "Paid", "Free"}

var sankeyLinks = []base.SankeyLink{
	base.NewSankeyLink(0, 1, 50), base.NewSankeyLink(0, 2, 30), base.NewSankeyLink(0, 3, 20),
	base.NewSankeyLink(1, 4, 30), base.NewSankeyLink(1, 5, 20),
	base.NewSankeyLink(2, 4, 20), base.NewSankeyLink(2, 5, 10),
	base.NewSankeyLink(3, 4, 5), base.NewSankeyLink(3, 5, 15),
	base.NewSankeyLink(4, 6, 15), base.NewSankeyLink(4, 7, 40),
}

type sankeyPlot struct {
	base.PlotDefaults
	align base.SankeyAlign
}

func (p sankeyPlot) Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	graph, err := base.NewSankey().
		NodeWidth(12).NodePadding(14).NodeAlign(p.align).
		Extent(0, 0, bounds.Size.Width-70, bounds.Size.Height).
		Layout(len(sankeyNodes), sankeyLinks)
	if err != nil {
		base.NewPlotLabel([]base.PlotText{base.NewPlotText(err.Error(), gpui.Point{X: 8, Y: 8}, pal.danger).WithAlign(gpui.TextAlignLeft)}).Paint(bounds, w, app)
		return
	}
	for i := range graph.Links {
		link := &graph.Links[i]
		color := pal.series[link.Source%len(pal.series)].Opacity(0.35)
		if path, ok := base.SankeyLinkPath(&graph.Nodes[link.Source], &graph.Nodes[link.Target], link, 1, bounds.Origin); ok {
			w.PaintPath(path, gpui.SolidBackground(color))
		}
	}
	var labels []base.PlotText
	for i := range graph.Nodes {
		n := &graph.Nodes[i]
		rect := gpui.Bounds{
			Origin: gpui.Point{X: bounds.Origin.X + n.X0, Y: bounds.Origin.Y + n.Y0},
			Size:   gpui.Size{Width: n.X1 - n.X0, Height: n.Y1 - n.Y0},
		}
		w.PaintQuad(gpui.Fill(rect, gpui.SolidBackground(pal.series[n.Index%len(pal.series)])))
		labels = append(labels, base.NewPlotText(sankeyNodes[n.Index]+" "+fmtNum(float32(n.Value)),
			gpui.Point{X: n.X1 + 4, Y: (n.Y0+n.Y1)/2 - base.PlotTextSize/2}, pal.fg).WithAlign(gpui.TextAlignLeft))
	}
	base.NewPlotLabel(labels).Paint(bounds, w, app)
}

func (g *gallery) sankeyPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Justified nodes", plotBox(sankeyPlot{align: base.SankeyAlignJustify}, 560, 260)),
		demo("Left-aligned nodes", plotBox(sankeyPlot{align: base.SankeyAlignLeft}, 560, 260)),
	)
}
