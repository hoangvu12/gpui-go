package base

// Plot labels (U:plot/label.rs): text a plot draws at points relative to
// its origin, and the measuring helpers charts reserve space with.
//
// Core seams: shape_line is the legacy facade
// gpui.Window.TextSystem().ShapeLine (ticket 10, gpui/shaped_line.go; the
// 4-argument legacy signature Base calls, force width None), its width is
// ShapedLine.Width, and painting is ShapedLine.Paint (legacy
// text_system/line.rs paint). The run's font is the window text style's
// (`window.text_style().font()`, R:window.rs:2378 text_style;
// gpui.WindowTextStyle + TextStyle.FontDescriptorOf).
//
// Go adaptations (recorded deviations):
//
//   - Names: Text is PlotText and TEXT_SIZE / TEXT_GAP / TEXT_HEIGHT are
//     PlotTextSize / PlotTextGap / PlotTextHeight (see plot.go); the
//     builders sharing a field name take the With prefix.
//   - `Text::new` is generic over `Point<T: Into<Pixels>>`; Pixels are
//     float32 in the core, so NewPlotText takes a gpui.Point.
//   - `From<Iterator<Item = Text>> for PlotLabel` is NewPlotLabel over a
//     slice.
//   - A shaping or paint error is skipped like upstream's ignored paint
//     result (`let _ = line.paint(..)`); a failed measurement counts as
//     zero width.

import (
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// PlotTextSize is the default label font size, in pixels, of PlotText and
// AxisText (U:plot/label.rs:10-16). A default, not a rule: a styled layer
// passes its own size and reserves axis space with AxisGutter for it.
const PlotTextSize float32 = 10

// PlotTextGap is the default gap, in pixels, between a label and what it
// labels (U:plot/label.rs:17-18).
const PlotTextGap float32 = 2

// PlotTextHeight is the default height, in pixels, a label line takes:
// PlotTextSize plus PlotTextGap (U:plot/label.rs:19-21).
const PlotTextHeight float32 = PlotTextSize + PlotTextGap

// plotShapeLabel shapes a label line in the window's text style
// (U:plot/label.rs:23-41).
func plotShapeLabel(text string, fontSize float32, color gpui.Hsla, w *gpui.Window) (*gpui.ShapedLine, error) {
	run := gpui.TextRun{
		Len:   len(text),
		Font:  gpui.WindowTextStyle(w).FontDescriptorOf(),
		Color: color,
	}
	return w.TextSystem().ShapeLine(text, fontSize, []gpui.TextRun{run}, nil)
}

// MeasureTextWidth returns the rendered width of text at fontSize in the
// window's current text style (U:plot/label.rs:43-53). Used for layout
// calculations that need to reserve space for labels before the scale
// ranges are fixed.
func MeasureTextWidth(text string, fontSize float32, w *gpui.Window) float32 {
	if text == "" {
		return 0
	}
	line, err := plotShapeLabel(text, fontSize, gpui.Black(), w)
	if err != nil {
		return 0
	}
	return line.Width()
}

// TruncateTextToWidth truncates text with a trailing ellipsis so it fits
// within maxWidth at fontSize (U:plot/label.rs:55-93). It returns text
// unchanged when it already fits, or when maxWidth <= 0 (no budget to
// reason about); it returns just the ellipsis when not even one character
// fits.
func TruncateTextToWidth(text string, fontSize, maxWidth float32, w *gpui.Window) string {
	if maxWidth <= 0 || MeasureTextWidth(text, fontSize, w) <= maxWidth {
		return text
	}
	const ellipsis = "…"
	// Byte offsets of every char boundary after the first char; cutting at
	// cuts[k] keeps the first k+1 chars.
	var cuts []int
	for i := range text {
		if i > 0 {
			cuts = append(cuts, i)
		}
	}

	// Binary search the largest prefix whose text + ellipsis still fits.
	best := -1
	lo, hi := 0, len(cuts)
	for lo < hi {
		mid := (lo + hi) / 2
		if MeasureTextWidth(text[:cuts[mid]]+ellipsis, fontSize, w) <= maxWidth {
			best = mid
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if best < 0 {
		return ellipsis
	}
	var b strings.Builder
	b.WriteString(text[:cuts[best]])
	b.WriteString(ellipsis)
	return b.String()
}

// PlotText is a label PlotLabel draws: its text, where it sits relative to
// the plot origin and how it is drawn (U:plot/label.rs:95-105; upstream
// `Text`, renamed, see plot.go).
type PlotText struct {
	Text       string
	Origin     gpui.Point
	Color      gpui.Hsla
	FontSize   float32
	FontWeight float32
	Align      gpui.TextAlign
}

// NewPlotText returns a left-aligned normal-weight label at PlotTextSize
// (U:plot/label.rs:108-122).
func NewPlotText(text string, origin gpui.Point, color gpui.Hsla) PlotText {
	return PlotText{
		Text:       text,
		Origin:     origin,
		Color:      color,
		FontSize:   PlotTextSize,
		FontWeight: gpui.DefaultFontWeight,
		Align:      gpui.TextAlignLeft,
	}
}

// WithFontSize sets the font size (U:plot/label.rs:124-128).
func (t PlotText) WithFontSize(fontSize float32) PlotText { t.FontSize = fontSize; return t }

// WithFontWeight sets the font weight (U:plot/label.rs:130-134).
func (t PlotText) WithFontWeight(fontWeight float32) PlotText {
	t.FontWeight = fontWeight
	return t
}

// WithAlign sets the alignment (U:plot/label.rs:136-140).
func (t PlotText) WithAlign(align gpui.TextAlign) PlotText { t.Align = align; return t }

// PlotLabel is a set of labels painted relative to a plot's origin
// (U:plot/label.rs:152-153).
type PlotLabel struct {
	items []PlotText
}

// NewPlotLabel returns a label over items (U:plot/label.rs:156-158).
func NewPlotLabel(items []PlotText) PlotLabel { return PlotLabel{items: items} }

// Paint paints the labels (U:plot/label.rs:160-181): each is shaped in the
// window's text style and aligned on its origin; the font weight is not
// applied, as upstream.
func (l PlotLabel) Paint(bounds gpui.Bounds, w *gpui.Window, _ *gpui.App) {
	for _, t := range l.items {
		origin := OriginPoint(t.Origin.X, t.Origin.Y, bounds.Origin)
		line, err := plotShapeLabel(t.Text, t.FontSize, t.Color, w)
		if err != nil {
			continue
		}
		switch t.Align {
		case gpui.TextAlignLeft:
		case gpui.TextAlignRight:
			origin.X -= line.Width()
		default:
			origin.X -= line.Width() / 2
		}
		_ = line.Paint(origin, t.FontSize, t.Align, nil, w)
	}
}
