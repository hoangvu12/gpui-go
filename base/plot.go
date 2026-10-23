package base

// Unstyled plotting (U:plot/mod.rs): scales, shapes, axes, grids, labels,
// and the element and hover tracking behind every Plot. Colors are always
// handed in by the caller; a styled layer supplies chart defaults, the
// tooltip overlay, and hover and appear timing through PlotMotion.
//
// Layout: the upstream `plot` module tree lives in the package root, one
// file per Rust module: plot.go (mod.rs), plot_scale*.go (scale.rs,
// scale/*.rs), plot_shape_*.go (shape/*.rs), plot_axis.go, plot_grid.go,
// plot_label.go, plot_path_cache.go, plot_element.go, plot_hover.go and
// plot_appear.go. A base/plot subpackage would import-cycle: Theme holds
// PlotTheme, and the plot needs motion.
//
// Go adaptations (recorded deviations):
//
//   - Names: plot items are module-qualified upstream (base::plot::...).
//     Those that collide, or would collide, with other Base items in the
//     single Go package take a Plot prefix: label.rs's Text is PlotText
//     (U:text/mod.rs:49 has a crate-root Text) and the label constants
//     TEXT_SIZE / TEXT_GAP / TEXT_HEIGHT are PlotTextSize / PlotTextGap /
//     PlotTextHeight. Builder methods sharing a public field's name take
//     the With prefix (PlotText.WithFontSize, AxisText.WithAlign).
//   - The Plot trait has eight defaulted methods next to the required
//     paint. Go interfaces carry no defaults: Plot lists every method and
//     an implementation embeds PlotDefaults for the upstream defaults,
//     overriding what it needs. The IntoElement supertrait is dropped: a
//     plot becomes an element through NewPlotElement.
//   - Option returns are (value, ok) pairs; Option<&T> parameters are
//     pointers (nil for None).
//   - Pixels are float32 in the core; generic Point<T> helpers
//     (origin_point, polygon) take gpui.Point.
//   - Background is solid only (decision 02, map gap 3): every fill and
//     stroke taking `impl Into<Background>` takes gpui.Background, which
//     cannot hold the gpui::linear_gradient upstream mentions in a doc
//     comment (U:plot/shape/bar.rs:141-146).
//   - Paths build through the native tessellator (PathBuilder.Build);
//     shape tests that build paths need the native library loaded.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// PlotMotion is the timing of a plot's motion: how its data marks appear
// when first painted, how its hover progress fades in and out, and the
// spring a pointer follows the hovered datum with (U:plot/mod.rs:37-50).
//
// Base installs no motion of its own: every duration defaults to zero, so
// the hover appears, fades and glides at once. Product timing belongs to
// the styled layer, which projects it through PlotTheme.
type PlotMotion struct {
	pointer Spring
	enter   Transition
	exit    Transition
	appear  Transition
}

// DefaultPlotMotion is the motionless default (U:plot/mod.rs:52-61).
func DefaultPlotMotion() PlotMotion {
	return PlotMotion{
		pointer: NewSpring(0),
		enter:   NewTransition(0),
		exit:    NewTransition(0),
		appear:  NewTransition(0),
	}
}

// WithPointer sets the spring a crosshair, highlight band or hover dot
// follows the hovered datum with (U:plot/mod.rs:64-69).
func (m PlotMotion) WithPointer(pointer Spring) PlotMotion { m.pointer = pointer; return m }

// WithEnter sets how the hover fades in when the cursor lands on a datum
// (U:plot/mod.rs:71-75).
func (m PlotMotion) WithEnter(enter Transition) PlotMotion { m.enter = enter; return m }

// WithExit sets how the hover fades out after the cursor leaves
// (U:plot/mod.rs:77-81).
func (m PlotMotion) WithExit(exit Transition) PlotMotion { m.exit = exit; return m }

// WithAppear sets how a plot's data marks appear the first time it is
// painted; see PlotAppear (U:plot/mod.rs:83-88).
func (m PlotMotion) WithAppear(appear Transition) PlotMotion { m.appear = appear; return m }

// Pointer returns the pointer spring (U:plot/mod.rs:90-92).
func (m PlotMotion) Pointer() Spring { return m.pointer }

// Enter returns the hover enter transition (U:plot/mod.rs:94-96).
func (m PlotMotion) Enter() Transition { return m.enter }

// Exit returns the hover exit transition (U:plot/mod.rs:98-100).
func (m PlotMotion) Exit() Transition { return m.exit }

// Appear returns the appear transition (U:plot/mod.rs:102-104).
func (m PlotMotion) Appear() Transition { return m.appear }

// Plot is a custom plot painted by a PlotElement (U:plot/mod.rs:107-225).
// Embed PlotDefaults for the upstream default of every method but Paint.
type Plot interface {
	// Prepaint lays out and places the child elements this plot hosts
	// (e.g. element labels). Called during the element's prepaint phase,
	// before TooltipState and Tooltip; the returned elements are painted
	// right after Paint, below the tooltip overlay
	// (U:plot/mod.rs:108-126). The default returns no children.
	Prepaint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) []gpui.AnyElement

	// Paint draws the plot (U:plot/mod.rs:128).
	Paint(bounds gpui.Bounds, w *gpui.Window, app *gpui.App)

	// ID is a stable element id that keeps this plot's state across
	// frames (U:plot/mod.rs:130-142). Returning one opts in to appear
	// motion and, unless Interactive says otherwise, tooltips and hover
	// motion. The default returns none.
	ID() (gpui.ElementID, bool)

	// Interactive reports whether a plot with an ID tracks hover and shows
	// its tooltip (U:plot/mod.rs:144-151). The default is true.
	Interactive() bool

	// Appear receives how far the plot's data marks have appeared this
	// frame, before Hover and Paint run (U:plot/mod.rs:153-159). The
	// default ignores it.
	Appear(appear PlotAppear, w *gpui.Window, app *gpui.App)

	// AppearGeneration opts in to appear motion: a value draws the plot in
	// the first time its id is painted, and again whenever the value
	// changes (U:plot/mod.rs:161-169). The default tracks no appear.
	AppearGeneration() (uint64, bool)

	// TooltipState maps the cursor (relative to the plot origin) to the
	// tooltip state to display, or false to show nothing
	// (U:plot/mod.rs:171-186). The default shows nothing.
	TooltipState(position gpui.Point, bounds gpui.Bounds, app *gpui.App) (TooltipState, bool)

	// Hover receives the hovered datum this frame (nil when nothing is
	// hovered or fading), before Tooltip and Paint run
	// (U:plot/mod.rs:188-201). The default ignores it.
	Hover(hover *PlotHover, w *gpui.Window, app *gpui.App)

	// Tooltip renders the tooltip overlay for the active state; cursor is
	// the live (or, while fading, last) cursor relative to the plot origin
	// (U:plot/mod.rs:203-224). The default returns none.
	Tooltip(state *TooltipState, cursor gpui.Point, bounds gpui.Bounds, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool)
}

// PlotDefaults supplies the default bodies of the Plot trait
// (U:plot/mod.rs:107-225) to a plot that embeds it.
type PlotDefaults struct{}

// Prepaint returns no children (U:plot/mod.rs:108-126).
func (PlotDefaults) Prepaint(gpui.Bounds, *gpui.Window, *gpui.App) []gpui.AnyElement { return nil }

// ID returns none (U:plot/mod.rs:130-142).
func (PlotDefaults) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// Interactive returns true (U:plot/mod.rs:144-151).
func (PlotDefaults) Interactive() bool { return true }

// Appear ignores the appear (U:plot/mod.rs:159).
func (PlotDefaults) Appear(PlotAppear, *gpui.Window, *gpui.App) {}

// AppearGeneration tracks no appear (U:plot/mod.rs:161-169).
func (PlotDefaults) AppearGeneration() (uint64, bool) { return 0, false }

// TooltipState shows nothing (U:plot/mod.rs:171-186).
func (PlotDefaults) TooltipState(gpui.Point, gpui.Bounds, *gpui.App) (TooltipState, bool) {
	return TooltipState{}, false
}

// Hover ignores the hover (U:plot/mod.rs:201).
func (PlotDefaults) Hover(*PlotHover, *gpui.Window, *gpui.App) {}

// Tooltip returns none (U:plot/mod.rs:203-224).
func (PlotDefaults) Tooltip(*TooltipState, gpui.Point, gpui.Bounds, *gpui.Window, *gpui.App) (gpui.AnyElement, bool) {
	return gpui.AnyElement{}, false
}

// Curve is how a Line or Area connects its points, like d3's curve
// factories (U:plot/mod.rs:227-238). Natural is the default.
type Curve uint8

const (
	// CurveNatural is a smooth curve through every point
	// (d3.curveNatural).
	CurveNatural Curve = iota
	// CurveLinear is straight segments between points (d3.curveLinear).
	CurveLinear
	// CurveStepAfter is a step that holds each value until the next point
	// (d3.curveStepAfter).
	CurveStepAfter
)

// String renders the variant name like the derived Debug.
func (c Curve) String() string {
	switch c {
	case CurveLinear:
		return "Linear"
	case CurveStepAfter:
		return "StepAfter"
	}
	return "Natural"
}

// OriginPoint is the point (x, y) offset by origin (U:plot/mod.rs:240-245).
func OriginPoint(x, y float32, origin gpui.Point) gpui.Point {
	return gpui.Point{X: x + origin.X, Y: y + origin.Y}
}

// Polygon strokes the open polygon through points (offset by the bounds
// origin) at 1px; false when the path does not build
// (U:plot/mod.rs:247-263).
func Polygon(points []gpui.Point, bounds gpui.Bounds) (*gpui.Path, bool) {
	moved := make([]gpui.Point, len(points))
	for i, p := range points {
		moved[i] = gpui.Point{X: p.X + bounds.Origin.X, Y: p.Y + bounds.Origin.Y}
	}
	b := gpui.StrokePathBuilder(1)
	b.AddPolygon(moved, false)
	return plotBuild(b)
}

// plotBuild is `builder.build().ok()`.
func plotBuild(b *gpui.PathBuilder) (*gpui.Path, bool) {
	path, err := b.Build()
	if err != nil {
		return nil, false
	}
	return path, true
}
