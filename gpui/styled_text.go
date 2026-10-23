package gpui

// Base ticket 10 (rich text): the StyledText element and its TextLayout
// handle (R:elements/text.rs:386-1161 at 254b5dbd, R: =
// gpui-go/reference/ce-source/crates/gpui/src).
//
// StyledText renders text with runs of different styles; its TextLayout
// maps byte indices to pixels and back. The layout follows the port's
// own Text element (div.go textElement) step for step — the measured
// leaf node, the cached-layout reuse under a matching wrap width, the
// inline-content publication and the parent-relative prepaint bounds —
// with the runs carried through, and paints through the per-run painter
// (rich_paint.go) in the pinned two passes: paint_background, then
// paint (R:elements/text.rs:1021-1060).
//
// Recorded deviations:
//
//   - text_transform (R:elements/text.rs:636-695) is not applied, like
//     the port's Text element (div.go); the text-overflow truncation
//     (R:elements/text.rs:764-885, 1164-1328) is (text_truncate.go);
//   - line_layout_for_index / line_layouts return the pin's
//     WrappedLineLayout, whose port is the native *WrappedLine on real
//     windows only (test windows shape through the deterministic stub);
//     they are not exposed — TextLayout answers the geometry queries
//     (PositionForIndex, IndexForPosition, WrappedText) itself;
//   - inside a parent inline paragraph the paragraph owns the text's
//     paint (R:elements/text.rs:1002-1024 current_inline_fragments); the
//     published runs keep their paint styles and the paragraph painter
//     (inline.go paintInlineLayout) paints each piece through the same
//     per-run painter with the runs cut to the piece (Base ticket 35).

import (
	"fmt"
	"strings"
)

// FontFamilyOverride overrides the font family of one byte range (the
// pinned (Range<usize>, SharedString) pair of with_font_family_overrides).
type FontFamilyOverride struct {
	// Range is the overridden UTF-8 byte range.
	Range TextRange
	// Family is the font family applied to runs inside the range.
	Family string
}

// StyledText renders text with runs of different styles
// (R:elements/text.rs:386-397). Callers are responsible for setting the
// correct style for each run.
type StyledText struct {
	text                       string
	runs                       []TextRun
	hasRuns                    bool
	delayedHighlights          []HighlightRange
	hasDelayedHighlights       bool
	delayedFontFamilyOverrides []FontFamilyOverride
	hasFontFamilyOverrides     bool
	layout                     *TextLayout
}

// NewStyledText constructs a styled text element from a string
// (R:elements/text.rs:400-409 StyledText::new).
func NewStyledText(text string) *StyledText {
	return &StyledText{text: text, layout: &TextLayout{state: &textLayoutState{}}}
}

// Layout returns the element's layout handle, to map indices to pixels
// and back (R:elements/text.rs:411-414).
func (s *StyledText) Layout() *TextLayout { return s.layout }

// WithDefaultHighlights sets the runs from a default style plus the
// highlighted ranges (R:elements/text.rs:416-429). The highlights must
// be sorted, non-overlapping and on character boundaries.
func (s *StyledText) WithDefaultHighlights(defaultStyle TextStyle, highlights []HighlightRange) *StyledText {
	if s.hasDelayedHighlights {
		panic("gpui: StyledText: can't use WithDefaultHighlights and WithHighlights")
	}
	return s.WithRuns(computeStyledRuns(s.text, defaultStyle, highlights))
}

// WithHighlights sets highlighted ranges resolved against the window's
// text style at layout time (R:elements/text.rs:431-450).
func (s *StyledText) WithHighlights(highlights []HighlightRange) *StyledText {
	if s.hasRuns {
		panic("gpui: StyledText: can't use WithHighlights and WithDefaultHighlights")
	}
	for _, highlight := range highlights {
		if !containsRange(s.text, highlight.Range) {
			panic(fmt.Sprintf("gpui: StyledText: highlight range %d..%d is not a character range of %q", highlight.Range.Start, highlight.Range.End, s.text))
		}
	}
	s.delayedHighlights = append([]HighlightRange(nil), highlights...)
	s.hasDelayedHighlights = true
	return s
}

// WithFontFamilyOverrides overrides the font family of byte ranges,
// resolved lazily at layout time on top of the inherited text style
// (R:elements/text.rs:479-500). The overrides must be sorted by start,
// non-overlapping and on character boundaries.
func (s *StyledText) WithFontFamilyOverrides(overrides []FontFamilyOverride) *StyledText {
	for _, override := range overrides {
		if !containsRange(s.text, override.Range) {
			panic(fmt.Sprintf("gpui: StyledText: font family override %d..%d is not a character range of %q", override.Range.Start, override.Range.End, s.text))
		}
	}
	s.delayedFontFamilyOverrides = append([]FontFamilyOverride(nil), overrides...)
	s.hasFontFamilyOverrides = true
	return s
}

// WithRuns sets the text runs (R:elements/text.rs:539-553); runs that do
// not exactly cover the text on character boundaries panic, like the
// pin's "invalid text run" assertion.
func (s *StyledText) WithRuns(runs []TextRun) *StyledText {
	if err := validateRuns(s.text, runs); err != nil {
		panic(fmt.Sprintf("gpui: StyledText: invalid text run: %v", err))
	}
	s.runs = append([]TextRun(nil), runs...)
	s.hasRuns = true
	return s
}

// computeStyledRuns is the pinned compute_runs (R:elements/text.rs:
// 452-477): default-style runs fill the gaps between highlights, and
// each highlight runs with the default style highlighted.
func computeStyledRuns(text string, defaultStyle TextStyle, highlights []HighlightRange) []TextRun {
	var runs []TextRun
	ix := 0
	for _, highlight := range highlights {
		if ix < highlight.Range.Start {
			runs = append(runs, defaultStyle.ToRun(highlight.Range.Start-ix))
		}
		runs = append(runs, defaultStyle.Highlight(highlight.Style).ToRun(highlight.Range.End-highlight.Range.Start))
		ix = highlight.Range.End
	}
	if ix < len(text) {
		runs = append(runs, defaultStyle.ToRun(len(text)-ix))
	}
	return runs
}

// applyFontFamilyOverrides is the pinned apply_font_family_overrides
// (R:elements/text.rs:502-521): a run entirely inside the current
// override takes its family.
func applyFontFamilyOverrides(runs []TextRun, overrides []FontFamilyOverride) {
	byteOffset := 0
	overrideIdx := 0
	for i := range runs {
		runEnd := byteOffset + runs[i].Len
		for overrideIdx < len(overrides) && overrides[overrideIdx].Range.End <= byteOffset {
			overrideIdx++
		}
		if overrideIdx < len(overrides) {
			override := overrides[overrideIdx]
			if byteOffset >= override.Range.Start && runEnd <= override.Range.End {
				runs[i].Font.Family = override.Family
			}
		}
		byteOffset = runEnd
	}
}

// takeRuns is the pinned take_runs (R:elements/text.rs:523-537): the
// explicit runs, else the delayed highlights against the default style,
// else one default run; then the font family overrides.
func (s *StyledText) takeRuns(defaultStyle TextStyle) []TextRun {
	var runs []TextRun
	switch {
	case s.hasRuns:
		runs = append([]TextRun(nil), s.runs...)
	case s.hasDelayedHighlights:
		runs = computeStyledRuns(s.text, defaultStyle, s.delayedHighlights)
	}
	if s.hasFontFamilyOverrides {
		if runs == nil {
			runs = []TextRun{defaultStyle.ToRun(len(s.text))}
		}
		applyFontFamilyOverrides(runs, s.delayedFontFamilyOverrides)
	}
	if runs == nil {
		runs = []TextRun{defaultStyle.ToRun(len(s.text))}
	}
	return runs
}

// containsRange reports whether a range is a valid character range of
// the text (the pinned str::contains_range debug assertion).
func containsRange(text string, r TextRange) bool {
	if r.Start < 0 || r.End < r.Start || r.End > len(text) {
		return false
	}
	return isCharBoundary(text, r.Start) && isCharBoundary(text, r.End)
}

// isCharBoundary is str::is_char_boundary.
func isCharBoundary(text string, index int) bool {
	if index == 0 || index == len(text) {
		return true
	}
	return index > 0 && index < len(text) && (text[index]&0xC0) != 0x80
}

// IntoElement implements IntoElement (R:elements/text.rs:608-614).
func (s *StyledText) IntoElement() AnyElement {
	return CustomElement[struct{}, struct{}](s)
}

// ID implements Element: a styled text has no identity
// (R:elements/text.rs:560-562).
func (s *StyledText) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (s *StyledText) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (R:elements/text.rs:568-580): take
// the runs against the window's text style and lay the text out.
func (s *StyledText) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	runs := s.takeRuns(WindowTextStyle(w))
	return s.layout.layout(s.text, runs, w), struct{}{}
}

// Prepaint implements Element (R:elements/text.rs:582-592).
func (s *StyledText) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	s.layout.prepaint(bounds, s.text, w)
	return struct{}{}
}

// Paint implements Element (R:elements/text.rs:594-605).
func (s *StyledText) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
	s.layout.paint(s.text, w)
}

// LayoutNodeStyle implements the root-stretch report (the text node's
// style is the default style, like the Text element).
func (s *StyledText) LayoutNodeStyle() (Style, bool) { return DefaultStyle(), true }

// ---------------------------------------------------------------------------
// TextLayout (R:elements/text.rs:616-634, 808-1161)
// ---------------------------------------------------------------------------

// TextLayout is the layout handle of a text element: it maps indices to
// pixels and back (R:elements/text.rs:616-618). Copies share the state
// (the pinned Rc).
type TextLayout struct {
	state *textLayoutState
}

// textLayoutState is the pinned TextLayoutState (R:elements/text.rs:
// 620-624).
type textLayoutState struct {
	layoutID *LayoutID
	inner    *textLayoutInner
}

// textLayoutInner is the pinned TextLayoutInner (R:elements/text.rs:
// 626-634) plus the runs the document paints with.
type textLayoutInner struct {
	len        int
	text       string
	document   shapedText
	runs       []TextRun
	lineHeight float32
	wrapWidth  *float32
	// truncateWidth is the truncation width the document was shaped
	// under (nil: not truncated).
	truncateWidth *float32
	size          *Size
	bounds        *Bounds
}

// layout requests the measured node (R:elements/text.rs:887-1000).
func (l *TextLayout) layout(text string, runs []TextRun, w *Window) LayoutID {
	textStyle := WindowTextStyle(w)
	rem := RemSize(w)
	fontSize := textStyle.FontSizePixels(rem)
	lineHeight := PixelSnap(w, textStyle.LineHeightPixels(fontSize, rem))
	lineClamp := textLineClamp(textStyle)
	// The pinned TextLayout::layout applies the style's text-transform
	// before shaping (elements/text.rs:895): the shaped document and the
	// published inline content carry the transformed text. The mapping
	// preserves byte length, so the covering runs stay valid.
	text = applyTextTransformPreservingByteLen(text, textStyle.TextTransform)

	state := l.state
	measure := func(req MeasureRequest) Size {
		// evaluate_wrap_width (R:elements/text.rs:809-827).
		var wrapWidth *float32
		if textStyle.WhiteSpace != WhiteSpaceNowrap {
			if req.KnownWidthPresent {
				width := req.KnownWidth
				wrapWidth = &width
			} else if req.AvailWidth.Kind == AvailDefinite {
				width := req.AvailWidth.Definite
				wrapWidth = &width
			}
		}
		truncation := EvaluateTextOverflow(textStyle, req)
		// The CSS ellipsis marker resolves from the primary font ("…" or
		// "...", text_truncate.go).
		truncation = resolveTruncationAffix(frameTextSystem(w), textStyle.FontDescriptorOf(), truncation)
		// The cached-layout reuse (R:elements/text.rs:931-946): neither
		// this request nor the cached layout may truncate.
		if inner := state.inner; inner != nil && inner.size != nil &&
			(wrapWidth == nil || (inner.wrapWidth != nil && *wrapWidth == *inner.wrapWidth)) &&
			truncation.Width == nil && inner.truncateWidth == nil {
			return *inner.size
		}
		seam := frameTextSystem(w)
		// A per-invocation copy: the truncation below rewrites it, and
		// the measure closure may run more than once.
		effective := text
		effective, runs := applyTextTruncation(effective, runs, wrapWidth, textStyle.LineClamp, truncation,
			func(candidate string, candidateRuns []TextRun, wrapWidth *float32) (shapedText, error) {
				return shapeStyledRuns(seam, candidate, fontSize, candidateRuns, wrapWidth, nil)
			})
		document, err := shapeStyledRuns(seam, effective, fontSize, runs, wrapWidth, lineClamp)
		if err != nil {
			panic(fmt.Sprintf("gpui: StyledText shaping failed: %v", err))
		}
		size := document.size(lineHeight)
		state.inner = &textLayoutInner{
			len:           document.textLen(),
			text:          effective,
			document:      document,
			runs:          mergeDecorationRuns(runs),
			lineHeight:    lineHeight,
			wrapWidth:     wrapWidth,
			truncateWidth: truncation.Width,
			size:          &size,
		}
		return size
	}
	layoutID, err := requestMeasuredLayoutOf(w, DefaultStyle(), measure)
	if err != nil {
		panic(fmt.Sprintf("gpui: StyledText request_layout: %v", err))
	}
	publishInlineContent(w, layoutID, &inlineTextContent{
		text:       text,
		runs:       runs,
		fontSize:   fontSize,
		lineHeight: lineHeight,
		// The runs carry their paint styles: a parent paragraph paints
		// them per run (Base ticket 35, inline.go paintInlineLayout).
		paintStyled: true,
	})
	id := layoutID
	state.layoutID = &id
	return layoutID
}

// prepaint commits the parent-relative bounds (R:elements/text.rs:
// 1002-1019); a no-op inside a parent paragraph.
func (l *TextLayout) prepaint(bounds Bounds, text string, w *Window) {
	if currentFrame(w).currentInlineFragments != nil {
		return
	}
	if l.state.layoutID != nil {
		relative, err := parentRelativeLayoutBoundsOf(w, *l.state.layoutID)
		if err != nil {
			panic(fmt.Sprintf("gpui: StyledText prepaint bounds: %v", err))
		}
		bounds = relative
	}
	if l.state.inner == nil {
		panic(fmt.Sprintf("gpui: measurement has not been performed on %s", text))
	}
	l.state.inner.bounds = &bounds
}

// paint paints the backgrounds, then the glyphs and decorations
// (R:elements/text.rs:1021-1060), aligned within the element bounds.
func (l *TextLayout) paint(text string, w *Window) {
	if currentFrame(w).currentInlineFragments != nil {
		return
	}
	inner := l.state.inner
	if inner == nil {
		panic(fmt.Sprintf("gpui: measurement has not been performed on %s", text))
	}
	if inner.bounds == nil {
		panic(fmt.Sprintf("gpui: prepaint has not been performed on %s", text))
	}
	if inner.document == nil {
		return
	}
	align := WindowTextStyle(w).TextAlign
	alignWidth := inner.bounds.Size.Width
	if err := paintStyledDocument(w, inner.document, inner.runs, inner.bounds.Origin, inner.lineHeight, align, &alignWidth, textPaintBackground); err != nil {
		panic(fmt.Sprintf("gpui: StyledText paint background: %v", err))
	}
	if err := paintStyledDocument(w, inner.document, inner.runs, inner.bounds.Origin, inner.lineHeight, align, &alignWidth, textPaintForeground); err != nil {
		panic(fmt.Sprintf("gpui: StyledText paint: %v", err))
	}
}

// measured returns the measured state, panicking like the pin's expect.
func (l *TextLayout) measured() *textLayoutInner {
	if l == nil || l.state == nil || l.state.inner == nil {
		panic("gpui: TextLayout: measurement has not been performed")
	}
	return l.state.inner
}

// prepainted returns the measured state with its bounds.
func (l *TextLayout) prepainted() (*textLayoutInner, Bounds) {
	inner := l.measured()
	if inner.bounds == nil {
		panic("gpui: TextLayout: prepaint has not been performed")
	}
	return inner, *inner.bounds
}

// IndexForPosition returns the byte index for a window position
// (R:elements/text.rs:1062-1081): ok is the pinned Ok (the position hit
// a visual row); a position above the bounds answers (0, false).
func (l *TextLayout) IndexForPosition(position Point) (int, bool) {
	inner, bounds := l.prepainted()
	if position.Y < bounds.Top() {
		return 0, false
	}
	geometry, ok := inner.document.(styledTextGeometry)
	if !ok {
		return 0, false
	}
	return geometry.byteIndexForPixelPoint(Point{X: position.X - bounds.Origin.X, Y: position.Y - bounds.Origin.Y}, inner.lineHeight)
}

// PositionForIndex returns the window position of a byte index
// (R:elements/text.rs:1083-1096): false past the text end.
func (l *TextLayout) PositionForIndex(index int) (Point, bool) {
	inner, bounds := l.prepainted()
	geometry, ok := inner.document.(styledTextGeometry)
	if !ok {
		return Point{}, false
	}
	position, ok := geometry.visualPositionForByteIndex(index, inner.lineHeight)
	if !ok {
		return Point{}, false
	}
	return Point{X: bounds.Origin.X + position.X, Y: bounds.Origin.Y + position.Y}, true
}

// Bounds returns the bounds of this layout (R:elements/text.rs:1121-1124).
func (l *TextLayout) Bounds() Bounds {
	_, bounds := l.prepainted()
	return bounds
}

// LineHeight returns the line height of this layout
// (R:elements/text.rs:1126-1129).
func (l *TextLayout) LineHeight() float32 { return l.measured().lineHeight }

// Len returns the UTF-8 length of the underlying text
// (R:elements/text.rs:1131-1134).
func (l *TextLayout) Len() int { return l.measured().len }

// Text returns the laid-out text (R:elements/text.rs:1136-1146).
func (l *TextLayout) Text() string {
	inner := l.measured()
	if inner.document == nil {
		return ""
	}
	return inner.text
}

// WrappedText returns the text with soft wraps as newlines
// (R:elements/text.rs:1148-1161): every visual line's text range joined
// by newlines.
func (l *TextLayout) WrappedText() string {
	inner := l.measured()
	if inner.document == nil {
		return ""
	}
	var b strings.Builder
	for i := 0; i < inner.document.lineCount(); i++ {
		start, end := inner.document.lineRange(i)
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(inner.text[start:end])
	}
	return b.String()
}
