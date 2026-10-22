package base

// Axis-aligned grid lines (U:plot/grid.rs). Each line, or each dash of
// one, paints as a 1px quad (gpui.Window.PaintQuad, R:window.rs:4634),
// not a stroked path.
//
// Go adaptation: `dash_array: Option<Vec<Pixels>>` is a nil-or-set slice
// plus a flag, so an empty array set explicitly behaves as upstream's
// `Some(&[])`.

import (
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Grid is axis-aligned grid lines across a plot, at the given x and y
// positions (U:plot/grid.rs:5-12).
type Grid struct {
	x, y      []float32
	stroke    gpui.Background
	dashArray []float32
	hasDash   bool
}

// NewGrid returns an empty grid (U:plot/grid.rs:15-17).
func NewGrid() Grid { return Grid{} }

// X sets the positions of the vertical lines, from the left of the plot
// (U:plot/grid.rs:19-23).
func (g Grid) X(x ...float32) Grid { g.x = append([]float32(nil), x...); return g }

// Y sets the positions of the horizontal lines, from the top of the plot
// (U:plot/grid.rs:25-29).
func (g Grid) Y(y ...float32) Grid { g.y = append([]float32(nil), y...); return g }

// Stroke sets the stroke of the grid lines (U:plot/grid.rs:31-35).
func (g Grid) Stroke(stroke gpui.Background) Grid { g.stroke = stroke; return g }

// DashArray sets the dash array of the grid lines (U:plot/grid.rs:37-41).
func (g Grid) DashArray(dashArray []float32) Grid {
	g.dashArray = append([]float32(nil), dashArray...)
	g.hasDash = true
	return g
}

type plotSegment struct{ start, end gpui.Point }

// points are the lines in window coordinates (U:plot/grid.rs:43-71).
func (g Grid) points(bounds gpui.Bounds) []plotSegment {
	size, origin := bounds.Size, bounds.Origin
	out := make([]plotSegment, 0, len(g.x)+len(g.y))
	for _, x := range g.x {
		out = append(out, plotSegment{OriginPoint(x, 0, origin), OriginPoint(x, size.Height, origin)})
	}
	for _, y := range g.y {
		out = append(out, plotSegment{OriginPoint(0, y, origin), OriginPoint(size.Width, y, origin)})
	}
	return out
}

// Paint paints the grid, each line or dash a 1px quad
// (U:plot/grid.rs:73-86).
func (g Grid) Paint(bounds gpui.Bounds, w *gpui.Window) {
	var dashes []float32
	if g.hasDash {
		dashes = g.dashArray
	}
	for _, line := range g.points(bounds) {
		for _, seg := range plotDashSegments(line.start, line.end, dashes, g.hasDash) {
			w.PaintQuad(gpui.Fill(plotLineBounds(seg.start, seg.end), g.stroke))
		}
	}
}

// plotLineBounds is the box a 1px stroke of the axis-aligned line
// start–end covers, centred on the coordinate (U:plot/grid.rs:89-106).
func plotLineBounds(start, end gpui.Point) gpui.Bounds {
	const half float32 = 0.5
	if start.X == end.X {
		top := rustMinF32(start.Y, end.Y)
		return gpui.Bounds{
			Origin: gpui.Point{X: start.X - half, Y: top},
			Size:   gpui.Size{Width: 1, Height: rustMaxF32(start.Y, end.Y) - top},
		}
	}
	left := rustMinF32(start.X, end.X)
	return gpui.Bounds{
		Origin: gpui.Point{X: left, Y: start.Y - half},
		Size:   gpui.Size{Width: rustMaxF32(start.X, end.X) - left, Height: 1},
	}
}

// plotDashSegments splits the line start–end into the dashes of
// dashArray, walked from start with the SVG stroke-dasharray rules:
// values alternate dash and gap, and an odd-length array repeats to an
// even one. Without a (non-empty) dash array the whole line is one
// segment (U:plot/grid.rs:108-149).
func plotDashSegments(start, end gpui.Point, dashArray []float32, hasDash bool) []plotSegment {
	if !hasDash || len(dashArray) == 0 {
		return []plotSegment{{start, end}}
	}
	dx, dy := end.X-start.X, end.Y-start.Y
	length := float32(math.Sqrt(float64(powiF32(dx, 2) + powiF32(dy, 2))))
	allNonPositive := true
	for _, d := range dashArray {
		if d > 0 {
			allNonPositive = false
		}
	}
	if length <= 0 || allNonPositive {
		return []plotSegment{{start, end}}
	}
	at := func(distance float32) gpui.Point {
		t := distance / length
		return gpui.Point{X: start.X + (end.X-start.X)*t, Y: start.Y + (end.Y-start.Y)*t}
	}
	patternLen := len(dashArray)
	if patternLen%2 == 1 {
		patternLen *= 2
	}
	var segments []plotSegment
	var position float32
	index := 0
	for position < length {
		dash := rustMaxF32(dashArray[index%len(dashArray)], 0)
		next := rustMinF32(position+dash, length)
		if index%2 == 0 && next > position {
			segments = append(segments, plotSegment{at(position), at(next)})
		}
		position = next
		index = (index + 1) % patternLen
	}
	return segments
}
