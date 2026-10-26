package gpui

// Base ticket 10 (rich text): the window text-system access and the
// legacy single-line text facade Base was written against.
//
// Retargeting fact (ticket 10): Base builds against crates.io gpui-ce
// 0.2.2, which keeps the legacy Zed text API; the core pin 254b5dbd
// removed it in its Parley rewrite. This file ports that legacy surface
// over the port's existing shaping seam (no native change):
//
//   - Window::text_system (R:window.rs:2372-2375) → Window.TextSystem;
//   - WindowTextSystem::shape_line (legacy
//     gpui-ce beb5a6d crates/gpui/src/text_system.rs:430-469, the
//     signature Base calls with a force width; the pin keeps the same
//     name at R:text_system.rs:325-339) → WindowTextSystem.ShapeLine;
//   - TextSystem::layout_width (legacy text_system.rs:745-760: the
//     width of one shaped character) → WindowTextSystem.LayoutWidth;
//   - TextSystem::line_wrapper (legacy text_system.rs:332) → the
//     LineWrapper moved into the gpui-go-base repository with Base
//     (browser-core ticket 75); it measures through LayoutWidth;
//   - ShapedLine len / width / with_len / paint / paint_background
//     (legacy text_system/line.rs:43-130) and the LineLayout geometry
//     index_for_x / closest_index_for_x / x_for_index (legacy
//     text_system/line_layout.rs:56-114).
//
// (R: = gpui-go/reference/ce-source/crates/gpui/src at 254b5dbd; the
// legacy sources are the older gpui-ce checkout
// ~/.cargo/git/checkouts/gpui-ce-6758b1db058c6497/beb5a6d.)
//
// The legacy LineLayout answers its x/index queries from per-glyph
// (byte index, x) pairs. The native ABI carries no glyph byte index
// (internal/native/text.go TextGlyph), so the facade answers the same
// queries from caret stops: one (cluster start, caret x) pair per
// logical cluster, read through WrappedLine.Clusters and
// WrappedLine.Caret on the real stack (so the facade agrees with the
// adapter's own caret geometry by construction) and from the
// deterministic test text system's pen positions on test windows (the
// pinned TestTextSystem, platform.rs). Recorded deviation: inside a
// ligature the legacy glyph table has ONE entry (the ligature glyph's
// start), while the cluster table has one stop per grapheme of the
// ligature at the backend's interpolated caret positions.
//
// Lifetime: a ShapedLine on a real window shares the window's
// line-layout cache entry (text_cache.go, the pinned LineLayoutCache):
// it stays valid for the frame that shaped it and the next one, exactly
// as long as the cache keeps an unrequested entry. The pin hands out an
// Arc that keeps the layout alive indefinitely; Go has no refcount on
// native handles, so re-shape each frame (the pin's own elements do).

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// WindowTextSystem is a window's text system (the pinned
// WindowTextSystem, R:text_system.rs:291-383): it shapes lines through
// the window's text seam — the real Parley stack with the window's
// line-layout cache, or the deterministic test text system on test
// windows.
type WindowTextSystem struct {
	w *Window
}

// TextSystem returns the window's text system (R:window.rs:2372-2375
// Window::text_system). The underlying seam is created lazily on first
// use, like the frame draw does.
func (w *Window) TextSystem() *WindowTextSystem {
	if w == nil {
		panic("gpui: Window.TextSystem requires a live window")
	}
	return &WindowTextSystem{w: w}
}

// seam resolves the window's text seam: the in-flight frame's system
// during a draw, otherwise the window's own (created on demand — the
// same lazy creation DrawWindowFrame and Window.ShapeText use).
func (t *WindowTextSystem) seam() (windowTextSystem, error) {
	ds := drawState(t.w)
	if ds.frame != nil && ds.frame.text != nil {
		return ds.frame.text, nil
	}
	if ds.text == nil {
		text, err := windowTextSystemOf()
		if err != nil {
			return nil, fmt.Errorf("gpui: WindowTextSystem: %w", err)
		}
		ds.text = text
	}
	return ds.text, nil
}

// NormalLineHeight returns the CSS `normal` line height of a
// descriptor at a font size (browser-parity spec D1): the resolved
// font's line spacing — rounded ascent, descent and line gap scaled to
// the size (FontMetrics.LineSpacingPx), Chromium's
// ComputedStyle::ComputedLineHeight value for `line-height: normal`
// (computed_style.cc:2425-2436). Like the rest of this handle it uses
// the in-flight frame's text system during a draw and the window's own
// otherwise.
func (t *WindowTextSystem) NormalLineHeight(font FontDescriptor, fontSize float32) (float32, error) {
	seam, err := t.seam()
	if err != nil {
		return 0, fmt.Errorf("gpui: WindowTextSystem.NormalLineHeight: %w", err)
	}
	return seam.normalLineHeight(font, fontSize), nil
}

// ShapeLine shapes one line of text at a font size with the given
// style runs for painting (legacy text_system.rs:430-469 shape_line):
// the runs must exactly cover the text on UTF-8 character boundaries,
// and the text must not contain a newline (the pin's debug assertion;
// the port reports it as a value error). Adjacent runs with the same
// paint style merge into one decoration run, like the legacy
// DecorationRun list.
//
// forceWidth is the legacy fixed-advance override (legacy
// line_layout.rs:849-872 apply_force_width_to_layout). Its glyph
// repositioning has no native counterpart — the native paint path
// places the shaped glyphs — so a non-nil force width is rejected with
// ErrTextValue (recorded deviation; every Base call but one passes
// None).
func (t *WindowTextSystem) ShapeLine(text string, fontSize float32, runs []TextRun, forceWidth *float32) (*ShapedLine, error) {
	if forceWidth != nil {
		return nil, fmt.Errorf("gpui: ShapeLine: %w: force_width is not supported by the native shaping seam", ErrTextValue)
	}
	if strings.ContainsRune(text, '\n') {
		return nil, fmt.Errorf("gpui: ShapeLine: %w: text argument should not contain newlines", ErrTextValue)
	}
	seam, err := t.seam()
	if err != nil {
		return nil, err
	}
	layout, err := shapeStyledRuns(seam, text, fontSize, runs, nil, nil, nil, TextAlignLeft, false)
	if err != nil {
		return nil, fmt.Errorf("gpui: ShapeLine: %w", err)
	}
	return &ShapedLine{
		Text:     text,
		layout:   layout,
		fontSize: fontSize,
		len:      layout.textLen(),
		runs:     mergeDecorationRuns(runs),
	}, nil
}

// LayoutWidth returns the advance of one character shaped alone in the
// font at the font size (legacy text_system.rs:745-760 layout_width).
// The legacy call takes a resolved FontId; the port's shaping seam
// resolves descriptors itself, so it takes the descriptor. Like the
// legacy infallible call, a native shaping failure panics.
func (t *WindowTextSystem) LayoutWidth(font FontDescriptor, fontSize float32, ch rune) float32 {
	seam, err := t.seam()
	if err != nil {
		panic(err.Error())
	}
	width, err := layoutWidthOf(seam, font, fontSize, ch)
	if err != nil {
		panic(fmt.Sprintf("gpui: LayoutWidth: %v", err))
	}
	return width
}

// layoutWidthOf shapes one character alone and reads its advance. The
// real stack shapes outside the frame cache and disposes the handle at
// once (the wrapper caches the widths); the test stub answers its
// em-width advance.
func layoutWidthOf(seam windowTextSystem, font FontDescriptor, fontSize float32, ch rune) (float32, error) {
	text := string(ch)
	if real, ok := seam.(*realWindowTextSystem); ok {
		line, err := real.system.ShapeText(TextLayoutRequest{
			Text:     text,
			FontSize: fontSize,
			Runs:     []TextRun{{Len: len(text), Font: font}},
		})
		if err != nil {
			return 0, err
		}
		defer func() { _ = line.Dispose() }()
		summary, err := line.Summary()
		if err != nil {
			return 0, err
		}
		return summary.Width, nil
	}
	shaped, err := seam.shape(text, fontSize, font, nil, nil)
	if err != nil {
		return 0, err
	}
	return shaped.width(), nil
}

// shapeStyledRuns shapes a document with explicit style runs through a
// window text seam: the real stack shapes every run's font through the
// window's line-layout cache (inline_real.go shapeRuns); the
// deterministic test system ignores fonts like the pinned
// TestTextSystem (one FontId for every run, platform.rs). tabSizeSpaces
// carries the browser's tab-size stops (browser-core 38; nil keeps the
// pin's DirectWrite stops). align and balance carry the browser text
// alignment and the balance mode (browser-core 43): the real stack
// stamps them on the shaping request (align start/end/justify reaches
// the native layout; balance bisects the shaped line), while the
// deterministic stub ignores them.
func shapeStyledRuns(seam windowTextSystem, text string, fontSize float32, runs []TextRun, wrapWidth *float32, lineClamp *uint32, tabSizeSpaces *uint32, align TextAlign, balance bool) (shapedText, error) {
	if err := validateRuns(text, runs); err != nil {
		return nil, err
	}
	if real, ok := seam.(*realWindowTextSystem); ok {
		return real.shapeRuns(text, fontSize, runs, wrapWidth, lineClamp, tabSizeSpaces, align, balance)
	}
	font := DefaultFontDescriptor()
	if len(runs) > 0 {
		font = runs[0].Font
	}
	return seam.shape(text, fontSize, font, wrapWidth, lineClamp)
}

// validateRuns checks the pinned run contract (R:elements/text.rs:540-553
// with_runs; R:text_system.rs:341-349): the runs exactly cover the text
// and end on UTF-8 character boundaries.
func validateRuns(text string, runs []TextRun) error {
	if len(runs) == 0 {
		return fmt.Errorf("%w: at least one run is required", ErrTextValue)
	}
	offset := 0
	for i, run := range runs {
		if run.Len < 0 {
			return fmt.Errorf("%w: run %d has a negative length", ErrTextValue, i)
		}
		offset += run.Len
		if offset > len(text) {
			return fmt.Errorf("%w: run %d ends at byte %d past the text length %d", ErrTextValue, i, offset, len(text))
		}
		if offset < len(text) && !utf8.RuneStart(text[offset]) {
			return fmt.Errorf("%w: run %d ends inside a UTF-8 character at byte %d", ErrTextValue, i, offset)
		}
	}
	if offset != len(text) {
		return fmt.Errorf("%w: runs cover %d of %d bytes", ErrTextValue, offset, len(text))
	}
	return nil
}

// mergeDecorationRuns merges adjacent runs with equal paint styles
// (legacy text_system.rs:442-460: color, underline, strikethrough and
// background equal → one DecorationRun).
func mergeDecorationRuns(runs []TextRun) []TextRun {
	var out []TextRun
	for _, run := range runs {
		if n := len(out); n > 0 && samePaintStyle(out[n-1], run) {
			out[n-1].Len += run.Len
			continue
		}
		out = append(out, run)
	}
	return out
}

// samePaintStyle reports whether two runs paint identically (the
// legacy DecorationRun equality; the pinned PaintStyle PartialEq,
// R:text_system/line_layout.rs:405-416).
func samePaintStyle(a, b TextRun) bool {
	return a.Color == b.Color &&
		equalPtr(a.BackgroundColor, b.BackgroundColor) &&
		equalUnderline(a.Underline, b.Underline) &&
		equalStrikethrough(a.Strikethrough, b.Strikethrough)
}

// ---------------------------------------------------------------------------
// ShapedLine (legacy text_system/line.rs + line_layout.rs)
// ---------------------------------------------------------------------------

// ShapedLine is a line of text that has been shaped and decorated (the
// legacy ShapedLine, legacy text_system/line.rs:41-130, with the
// LineLayout geometry it dereferences to, legacy line_layout.rs:14-114).
type ShapedLine struct {
	// Text is the text that was shaped for this line.
	Text string

	layout   shapedText
	fontSize float32
	// len is the line's length in UTF-8 bytes (WithLen overrides it).
	len int
	// runs are the merged decoration runs (legacy decoration_runs).
	runs []TextRun
	// stops caches the caret-stop table (lazily built).
	stops []caretStop
}

// Len returns the length of the line in UTF-8 bytes (legacy line.rs:55).
func (l *ShapedLine) Len() int { return l.len }

// Width returns the shaped advance width of the line (legacy line.rs:63).
func (l *ShapedLine) Width() float32 { return l.layout.width() }

// FontSize returns the line's font size (LineLayout::font_size).
func (l *ShapedLine) FontSize() float32 { return l.fontSize }

// Ascent returns the line's ascent (LineLayout::ascent).
func (l *ShapedLine) Ascent() float32 { return l.layout.ascent() }

// Descent returns the line's descent (LineLayout::descent).
func (l *ShapedLine) Descent() float32 { return l.layout.descent() }

// WithLen returns a copy of the line reporting a different length, for
// rendering text a as text b (legacy line.rs:67-80).
func (l *ShapedLine) WithLen(length int) *ShapedLine {
	out := *l
	out.len = length
	return &out
}

// IndexForX returns the byte index of the character at an x coordinate
// (legacy line_layout.rs:57-71 index_for_x): false at or past the line
// width; otherwise the last stop at or before x, or 0.
func (l *ShapedLine) IndexForX(x float32) (int, bool) {
	if x >= l.Width() {
		return 0, false
	}
	stops := l.caretStops()
	for i := len(stops) - 1; i >= 0; i-- {
		if stops[i].x <= x {
			return stops[i].index, true
		}
	}
	return 0, true
}

// ClosestIndexForX returns the character boundary closest to an x
// coordinate (legacy line_layout.rs:73-102 closest_index_for_x): the
// nearer of the first stop at or after x and the stop before it; the
// line length past the last stop (a one-byte line splits at half its
// width).
func (l *ShapedLine) ClosestIndexForX(x float32) int {
	prevIndex := 0
	prevX := float32(0)
	for _, stop := range l.caretStops() {
		if stop.x >= x {
			if stop.x-x < x-prevX {
				return stop.index
			}
			return prevIndex
		}
		prevIndex = stop.index
		prevX = stop.x
	}
	if l.len == 1 {
		if x > l.Width()/2 {
			return 1
		}
		return 0
	}
	return l.len
}

// XForIndex returns the x position of the character at a byte index
// (legacy line_layout.rs:104-114 x_for_index): the first stop at or
// after the index, else the line width.
func (l *ShapedLine) XForIndex(index int) float32 {
	for _, stop := range l.caretStops() {
		if stop.index >= index {
			return stop.x
		}
	}
	return l.Width()
}

// Paint paints the line's glyphs and foreground decorations (legacy
// line.rs:82-105 / R:text_system/line.rs:33-55 ShapedLine::paint): each
// run paints in its own color, clipped to its selection rectangles
// (ticket 02's per-run paint decision, rich_paint.go). alignWidth nil
// aligns within the line's own advance.
func (l *ShapedLine) Paint(origin Point, lineHeight float32, align TextAlign, alignWidth *float32, w *Window) error {
	return paintStyledDocument(w, l.layout, l.runs, origin, lineHeight, align, alignWidth, textPaintForeground)
}

// PaintBackground paints the run backgrounds of the line (legacy
// line.rs:107-130 / R:text_system/line.rs:57-79 paint_background).
func (l *ShapedLine) PaintBackground(origin Point, lineHeight float32, align TextAlign, alignWidth *float32, w *Window) error {
	return paintStyledDocument(w, l.layout, l.runs, origin, lineHeight, align, alignWidth, textPaintBackground)
}

// caretStops returns the line's (cluster start, x) table, built once.
func (l *ShapedLine) caretStops() []caretStop {
	if l.stops == nil {
		geometry, ok := l.layout.(styledTextGeometry)
		if !ok {
			l.stops = []caretStop{}
			return l.stops
		}
		stops, err := geometry.caretStops()
		if err != nil {
			panic(fmt.Sprintf("gpui: ShapedLine geometry: %v", err))
		}
		l.stops = stops
	}
	return l.stops
}

// ---------------------------------------------------------------------------
// Geometry adapters over the shaped-text seam
// ---------------------------------------------------------------------------

// caretStop is one (logical cluster start, caret x) pair: the facade's
// replacement for the legacy (glyph.index, glyph.position.x) pair.
type caretStop struct {
	index int
	x     float32
}

// styledTextGeometry is the hit/caret geometry a shaped document
// answers for the rich-text facade (the pinned WrappedLineLayout
// visual_position_for_byte_index / byte_index_for_pixel_point,
// R:text_system/line_layout.rs:698-758, plus the caret-stop table).
type styledTextGeometry interface {
	// caretStops returns the cluster-start stops of visual row 0 in
	// logical order (the single-line facade's table).
	caretStops() ([]caretStop, error)
	// visualPositionForByteIndex returns the caret origin of a byte
	// index (false past the text end or without caret geometry).
	visualPositionForByteIndex(index int, lineHeight float32) (Point, bool)
	// byteIndexForPixelPoint returns the byte index for a layout point
	// (true = the pinned Ok, inside a visual row).
	byteIndexForPixelPoint(point Point, lineHeight float32) (int, bool)
}

// caretStops implements styledTextGeometry for the real stack: one stop
// per logical cluster at its downstream caret x (WrappedLine.Clusters +
// WrappedLine.Caret).
func (s *realShapedText) caretStops() ([]caretStop, error) {
	clusters, err := s.line.Clusters()
	if err != nil {
		return nil, err
	}
	stops := make([]caretStop, 0, len(clusters))
	for _, cluster := range clusters {
		caret, err := s.line.Caret(cluster.Start, CaretAffinityDownstream, 1)
		if err != nil {
			return nil, err
		}
		stops = append(stops, caretStop{index: cluster.Start, x: caret.Bounds.X})
	}
	return stops, nil
}

// visualPositionForByteIndex implements styledTextGeometry
// (R:text_system/line_layout.rs:745-770: None past the end, else the
// downstream caret's bounds origin).
func (s *realShapedText) visualPositionForByteIndex(index int, lineHeight float32) (Point, bool) {
	if index < 0 || index > s.textLen() {
		return Point{}, false
	}
	caret, err := s.line.Caret(index, CaretAffinityDownstream, lineHeight)
	if err != nil || !caret.Present {
		return Point{}, false
	}
	return Point{X: caret.Bounds.X, Y: caret.Bounds.Y}, true
}

// byteIndexForPixelPoint implements styledTextGeometry
// (WrappedLine.ByteIndexForPixelPoint, the pinned
// byte_index_for_pixel_point).
func (s *realShapedText) byteIndexForPixelPoint(point Point, lineHeight float32) (int, bool) {
	index, inside, err := s.line.ByteIndexForPixelPoint(point.X, point.Y, lineHeight)
	if err != nil {
		return 0, false
	}
	return index, inside
}

// testStops returns the stub's caret stops including the end stop (the
// pinned TestPlatformTextLayout stops, platform.rs:1248-1255).
func (t *testShapedText) testStops() []caretStop {
	emWidth := t.textSize * testTextAdvanceWidth / testTextUnitsPerEm
	stops := []caretStop{{index: 0, x: 0}}
	position := float32(0)
	index := 0
	for _, ch := range t.text {
		position += emWidth * float32(utf16.RuneLen(ch))
		index += utf8.RuneLen(ch)
		stops = append(stops, caretStop{index: index, x: position})
	}
	return stops
}

// caretStops implements styledTextGeometry for the deterministic test
// system: one stop per character at its pen position (the stub's glyph
// table — one glyph per char, platform.rs TestTextSystem).
func (t *testShapedText) caretStops() ([]caretStop, error) {
	stops := t.testStops()
	return stops[:len(stops)-1], nil
}

// testCaretStop is the pinned TestPlatformTextLayout::caret_stop
// (platform.rs:1258-1265): the last stop at or before the offset.
func (t *testShapedText) testCaretStop(byteOffset int) caretStop {
	stops := t.testStops()
	index := 0
	for i, stop := range stops {
		if stop.index <= byteOffset {
			index = i
		}
	}
	return stops[index]
}

// visualPositionForByteIndex implements styledTextGeometry: the pinned
// test layout's caret_bounds origin (platform.rs:1334-1346) — the stop
// x on row 0 — and None past the end (line_layout.rs:750-752).
func (t *testShapedText) visualPositionForByteIndex(index int, lineHeight float32) (Point, bool) {
	if index < 0 || index > len(t.text) {
		return Point{}, false
	}
	return Point{X: t.testCaretStop(index).x, Y: 0}, true
}

// byteIndexForPixelPoint implements styledTextGeometry: the pinned test
// layout's byte_index_from_pixel_point (platform.rs:1282-1302) — the
// start of the stop pair whose x span holds the point, else the closest
// caret (the first minimal stop distance, caret_from_pixel_point
// platform.rs:1304-1332).
func (t *testShapedText) byteIndexForPixelPoint(point Point, lineHeight float32) (int, bool) {
	stops := t.testStops()
	for i := 0; i+1 < len(stops); i++ {
		if point.X >= stops[i].x && point.X < stops[i+1].x {
			return stops[i].index, true
		}
	}
	closest := 0
	best := float32(-1)
	for _, stop := range stops {
		distance := abs32(stop.x - point.X)
		if best < 0 || distance < best {
			best = distance
			closest = stop.index
		}
	}
	return t.testCaretStop(closest).index, false
}
