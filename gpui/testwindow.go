package gpui

// This file is the port's test window (ticket11): a deterministic,
// hostless window that draws through the REAL element phase pipeline
// into a real scene, with the reference test platform's window profile
// (scale factor 2.0, rem size 16 logical pixels) and the deterministic
// test text system (the reference platform/test TestTextSystem, whose
// glyph rasterization is empty so text contributes layout geometry and
// no scene sprites).
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/platform/test/window.rs — TestWindow: the fixed
//     scale_factor 2.0;
//   - crates/gpui/src/platform/test/platform.rs — TestPlatform::new
//     installs Arc<TestTextSystem> as the platform text system, so every
//     reference test window (TestAppContext::add_window_view and
//     cx.open_window) shapes and paints text through the stub;
//   - crates/gpui/src/platform.rs — TestTextSystem: font_id FontId(1),
//     glyph ids are the character's UTF-16 length, the per-glyph advance
//     is em_width * glyph_id with em_width = font_size * 600 / 1000 (the
//     advance of 'm' over 1000 units per em), one visual line, metrics
//     ascent 1025 / descent -275 over 1000 units per em, and
//     rasterize_glyph returns empty bounds (so window.rs
//     paint_glyph_from_atlas inserts no sprite);
//   - crates/gpui/src/app.rs — open_window inserts a logical window into
//     the app registry and draws once before returning.
//
// This is the deterministic fixture path of fx-0006: no real HWND, no
// real font stack — the same observable geometry the reference test
// window produces.

import (
	"fmt"
	"unicode/utf16"
)

// NewTestWindow opens a deterministic headless window on the application:
// a logical window identity in the application's registry, a window-owned
// scope, and an element runtime set up with the reference test window
// profile (scale 2.0, rem 16) and the deterministic test text system.
// The window has no platform host: its methods degrade like any
// hostless lease (Bounds errors, Alive reports false) while the element
// runtime draws real frames.
func NewTestWindow(app *App, size Size) *Window {
	if app == nil {
		panic("gpui: NewTestWindow requires an application")
	}
	var window *Window
	app.Update(func(app *App) {
		app.windowSeq++
		window = &Window{
			app:    app,
			id:     WindowID(app.windowSeq),
			handle: WindowHandle{},
			scope:  app.rootScope.Child(),
		}
		app.windows[window.id] = window
	})
	ds := drawState(window)
	ds.testWindow = true
	ds.viewport = size
	ds.scale = TestScaleFactor
	ds.rem = DefaultRemSize
	ds.text = &testTextSystem{}
	return window
}

// CloseTestWindow closes the test window like a platform window
// destruction: the window is removed from the application's registry,
// its scope closes (retiring window-scoped registrations, tasks and
// entity leases) and its element runtime state is dropped (the
// reference window removal in app.rs update_window_id and the
// removeWindow path of the port's close handling).
func CloseTestWindow(w *Window) {
	if w == nil {
		return
	}
	w.app.Update(func(app *App) {
		app.removeWindow(w.id)
	})
	dropDrawState(w)
}

// TestWindowDrawGeometry returns the test window's deterministic draw
// geometry (viewport size, scale factor, rem size) for diagnostics.
func TestWindowDrawGeometry(w *Window) (Size, float32, float32, error) {
	ds, ok := windowDrawStates[w]
	if !ok || !ds.testWindow {
		return Size{}, 0, 0, fmt.Errorf("gpui: TestWindowDrawGeometry requires a test window")
	}
	return ds.viewport, ds.scale, ds.rem, nil
}

// ---------------------------------------------------------------------------
// The deterministic test text system (platform.rs TestTextSystem)
// ---------------------------------------------------------------------------

// testTextSystem is the port of the reference TestTextSystem: fully
// deterministic shaping (glyph ids are UTF-16 character lengths, the
// per-glyph advance is em_width times the glyph id) and empty glyph
// rasterization (so paint inserts no sprites).
type testTextSystem struct{}

// The reference stub's font metrics (TestTextSystem::font_metrics).
const (
	testTextUnitsPerEm = 1000.0
	testTextAscent     = 1025.0
	testTextDescent    = -275.0
	// testTextAdvanceWidth is the 'm' advance (600 font units) every
	// glyph id scales: advance(FontId(0), GlyphId(n)) = 600 * n.
	testTextAdvanceWidth = 600.0
)

// shape implements windowTextSystem: the reference TestTextSystem
// layout_text — one visual line, glyphs positioned by cumulative
// em-width advances, width = the accumulated advance plus run tracking.
func (t *testTextSystem) shape(text string, fontSize float32, font FontDescriptor, wrapWidth *float32, lineClamp *uint32) (shapedText, error) {
	// em_width = font_size * advance('m') / units_per_em, in f32 with the
	// reference's operation order.
	emWidth := fontSize * testTextAdvanceWidth / testTextUnitsPerEm
	position := float32(0)
	glyphs := make([]ShapedGlyph, 0, len(text))
	for _, ch := range text {
		glyphID := uint32(utf16.RuneLen(ch))
		glyphs = append(glyphs, ShapedGlyph{
			ID:      glyphID,
			X:       position,
			Y:       0,
			IsEmoji: glyphID == 2,
		})
		position += emWidth * float32(glyphID)
	}
	// This slice's text styles carry no letter spacing, so run tracking is
	// zero (the reference accumulates run letter spacing otherwise).
	tracking := float32(0)
	return &testShapedText{
		text:     text,
		fontSize: fontSize,
		advance:  position + tracking,
		wrap:     wrapWidth,
		glyphs:   glyphs,
	}, nil
}

// layoutInline implements windowTextSystem: the reference
// TestTextSystem::layout_inline (platform.rs:1716), ported operation
// for operation — the whole document is one visual line; the baseline
// grows to the tallest box; boxes sit after the text preceding their
// index, shifted by every preceding box; glyph x positions shift by
// the widths of boxes at or before their char; the platform geometry
// (caret stops) stays UN-shifted, so byte-range geometry excludes box
// advances exactly like the pin's test platform; and the vertical
// alignment runs the shared align_inline_boxes pass with the request's
// container metrics and no per-row text bounds.
func (t *testTextSystem) layoutInline(request inlineLayoutRequest) (inlineLayout, error) {
	shaped, err := t.shape(request.text, request.fontSize, FontDescriptor{}, request.wrapWidth, request.lineClamp)
	if err != nil {
		return inlineLayout{}, err
	}
	document := shaped.(*testShapedText)

	emWidth := request.fontSize * testTextAdvanceWidth / testTextUnitsPerEm
	baseline := request.lineHeight
	for _, box := range request.boxes {
		baseline = max32(baseline, box.Size.Height)
	}

	// position_test_inline_boxes: each box sits after the text width
	// preceding its index plus every preceding box width, bottom on the
	// pre-alignment baseline.
	precedingWidth := float32(0)
	positionedBoxes := make([]PositionedInlineBox, 0, len(request.boxes))
	for _, box := range request.boxes {
		textWidth := float32(0)
		for _, ch := range request.text[:box.Index] {
			textWidth += emWidth * float32(utf16.RuneLen(ch))
		}
		positioned := PositionedInlineBox{
			ID:        box.ID,
			LineIndex: 0,
			Bounds: Bounds{
				Origin: Point{X: textWidth + precedingWidth, Y: baseline - box.Size.Height},
				Size:   box.Size,
			},
		}
		precedingWidth += box.Size.Width
		positionedBoxes = append(positionedBoxes, positioned)
	}

	// add_test_inline_box_advances: every glyph shifts right by the
	// widths of boxes inserted at or before its char index (glyphs are
	// in char order, zipped with the text's char indices); the fragment
	// extents, layout width and line advance gain the total box width.
	glyphs := make([]ShapedGlyph, len(document.glyphs))
	copy(glyphs, document.glyphs)
	i := 0
	for byteIndex, _ := range request.text {
		if i >= len(glyphs) {
			break
		}
		shift := float32(0)
		for _, box := range request.boxes {
			if box.Index <= byteIndex {
				shift += box.Size.Width
			}
		}
		glyphs[i].X += shift
		i++
	}
	totalBoxWidth := float32(0)
	for _, box := range request.boxes {
		totalBoxWidth += box.Size.Width
	}
	inline := &testInlineDocument{
		text:     request.text,
		fontSize: request.fontSize,
		glyphs:   glyphs,
		advance:  document.advance + totalBoxWidth,
		wrap:     request.wrapWidth,
		stops:    testCaretStops(request.text, emWidth),
	}

	lineWidth := document.advance + totalBoxWidth
	if request.wrapWidth != nil && *request.wrapWidth < lineWidth {
		lineWidth = *request.wrapWidth
	}
	lines := []InlineVisualLine{{
		Origin:   Point{},
		Size:     Size{Width: lineWidth, Height: baseline},
		Baseline: baseline,
	}}
	size := Size{Width: document.advance + totalBoxWidth, Height: baseline}

	layout := inlineLayout{
		lines:           lines,
		boxes:           positionedBoxes,
		alignmentOffset: 0,
		size:            size,
		pieces: []inlinePaintPiece{{
			row:        0,
			x:          0,
			advance:    document.advance + totalBoxWidth,
			byteStart:  0,
			byteEnd:    len(request.text),
			shaped:     inline,
			lineIndex:  0,
			lineHeight: request.lineHeight,
		}},
	}
	layout.geometry = testInlineGeometry(inline, request.fontSize)

	alignInlineBoxes(layout.lines, layout.boxes, &layout.size, request.boxes,
		[]InlineTextMetrics{request.textMetrics}, nil, request.textMetrics, request.lineHeight)
	return layout, nil
}

// inlineMetrics implements windowTextSystem: the stub's font metrics
// at a font size (ascent 1025, descent -275, x_height 516 over 1000
// units per em).
func (t *testTextSystem) inlineMetrics(font FontDescriptor, fontSize float32) InlineTextMetrics {
	return InlineTextMetrics{
		Ascent:  fontSize * (testTextAscent / testTextUnitsPerEm),
		Descent: fontSize * (testTextDescent / testTextUnitsPerEm),
		XHeight: fontSize * (testTextXHeight / testTextUnitsPerEm),
	}
}

// testTextXHeight is the stub's x_height (516 font units).
const testTextXHeight = 516.0

// testInlineDocument is the stub's inline document: the shifted glyph
// line plus the un-shifted caret stops (the platform layout).
type testInlineDocument struct {
	text     string
	fontSize float32
	// glyphs are the box-shifted positioned glyphs.
	glyphs []ShapedGlyph
	// advance is the document width including box advances.
	advance float32
	// wrap is the wrap constraint (echo).
	wrap *float32
	// stops are the un-shifted (byte index, pen x) caret stops.
	stops [][2]float32
}

// width implements shapedText.
func (t *testInlineDocument) width() float32 { return t.advance }

// ascent implements shapedText.
func (t *testInlineDocument) ascent() float32 {
	return t.fontSize * (testTextAscent / testTextUnitsPerEm)
}

// descent implements shapedText.
func (t *testInlineDocument) descent() float32 {
	return t.fontSize * (testTextDescent / testTextUnitsPerEm)
}

// lineCount implements shapedText: one line.
func (t *testInlineDocument) lineCount() int { return 1 }

// textLen implements shapedText.
func (t *testInlineDocument) textLen() int { return len(t.text) }

// size implements shapedText.
func (t *testInlineDocument) size(lineHeight float32) Size {
	width := t.advance
	if t.wrap != nil && *t.wrap < width {
		width = *t.wrap
	}
	return Size{Width: width, Height: lineHeight}
}

// paint implements shapedText: the stub's empty-raster paint (no
// sprites; the paragraph layer carries the observable bounds).
func (t *testInlineDocument) paint(w *Window, origin Point, lineHeight float32, color Hsla) error {
	return nil
}

// paintLine implements shapedText: no observable output (empty
// rasters).
func (t *testInlineDocument) paintLine(w *Window, lineIndex int, origin Point, baseline float32, color Hsla) error {
	return nil
}

// lineAdvance implements shapedText.
func (t *testInlineDocument) lineAdvance(lineIndex int) float32 { return t.advance }

// lineRange implements shapedText.
func (t *testInlineDocument) lineRange(lineIndex int) (int, int) { return 0, len(t.text) }

// selectionRects implements shapedText: the caret-stop geometry of a
// byte range (the test platform's selection_bounds).
func (t *testInlineDocument) selectionRects(start, end int, lineHeight float32) []TextRect {
	if start >= end {
		return nil
	}
	from := t.caretStop(start)
	to := t.caretStop(end)
	low, high := min32(from, to), max32(from, to)
	return []TextRect{{X: low, Y: 0, W: high - low, H: lineHeight}}
}

// caretStop returns the pen x of the stop at or before a byte index
// (the platform's caret_stop partition).
func (t *testInlineDocument) caretStop(byteOffset int) float32 {
	index := 0
	for i, stop := range t.stops {
		if int(stop[0]) <= byteOffset {
			index = i
		} else {
			break
		}
	}
	return t.stops[index][1]
}

// testCaretStops builds the (byte index, pen x) stops of a text at an
// em width (the stub's layout_text stop accumulation: stop 0 at pen
// 0, then every char boundary at its pen position).
func testCaretStops(text string, emWidth float32) [][2]float32 {
	stops := make([][2]float32, 0, len(text)+1)
	position := float32(0)
	stops = append(stops, [2]float32{0, 0})
	byteIndex := 0
	for _, ch := range text {
		position += emWidth * float32(utf16.RuneLen(ch))
		byteIndex += len(string(ch))
		stops = append(stops, [2]float32{float32(byteIndex), position})
	}
	return stops
}

// testInlineGeometry builds the byte-range geometry source of the
// stub's inline layout (the platform's default inline_geometry over
// selection_bounds: line_height = the platform size height / line
// count = font_size; one region per range at row 0).
func testInlineGeometry(document *testInlineDocument, fontSize float32) func(start, end int) []inlineRangeGeometry {
	return func(start, end int) []inlineRangeGeometry {
		if start >= end {
			return nil
		}
		rects := document.selectionRects(start, end, fontSize)
		regions := make([]inlineRangeGeometry, 0, len(rects))
		for _, rect := range rects {
			regions = append(regions, inlineRangeGeometry{
				bounds:          Bounds{Origin: Point{X: rect.X, Y: rect.Y}, Size: Size{Width: rect.W, Height: rect.H}},
				visualLineIndex: int(rect.Y / fontSize),
			})
		}
		return regions
	}
}

// testShapedText is the stub's shaped document (the reference
// TestPlatformTextLayout/LineLayout facts).
type testShapedText struct {
	text     string
	fontSize float32
	// advance is the accumulated glyph advance (LineLayout::width).
	advance float32
	// wrap is the wrap constraint the document was shaped under.
	wrap *float32
	// glyphs are the positioned glyphs (paint order).
	glyphs []ShapedGlyph
}

// width implements shapedText.
func (t *testShapedText) width() float32 { return t.advance }

// ascent implements shapedText: font_size * (ascent / units_per_em).
func (t *testShapedText) ascent() float32 {
	return t.fontSize * (testTextAscent / testTextUnitsPerEm)
}

// descent implements shapedText: font_size * (descent / units_per_em).
func (t *testShapedText) descent() float32 {
	return t.fontSize * (testTextDescent / testTextUnitsPerEm)
}

// lineCount implements shapedText: the stub always reports its single
// visual line.
func (t *testShapedText) lineCount() int { return 1 }

// textLen implements shapedText.
func (t *testShapedText) textLen() int { return len(t.text) }

// size implements shapedText: min(the wrap constraint, the advance) by
// the line-height-scaled row count (WrappedLine::size).
func (t *testShapedText) size(lineHeight float32) Size {
	width := t.advance
	if t.wrap != nil && *t.wrap < width {
		width = *t.wrap
	}
	return Size{Width: width, Height: lineHeight * float32(t.lineCount())}
}

// paint implements shapedText: the reference paint_visual_text — the
// line bounds layer (pushed when the mask-clipped bounds are non-empty)
// and the per-glyph paint calls, whose rasters are empty in the test
// text system so no sprite is inserted.
func (t *testShapedText) paint(w *Window, origin Point, lineHeight float32, color Hsla) error {
	frame := currentFrame(w)
	paintWidth := t.advance
	lineBounds := Bounds{
		Origin: origin,
		Size:   Size{Width: paintWidth, Height: lineHeight * float32(t.lineCount())},
	}
	clipped := intersectLogical(lineBounds, frame.paint.Mask)
	pushed := !isEmptyBounds(clipped)
	if pushed {
		if err := frame.scene.BeginLayer(lineBounds, frame.paint); err != nil {
			return err
		}
	}
	// The glyph loop: every glyph's raster is empty in the test text
	// system (rasterize_glyph returns default bounds), so
	// paint_glyph_from_atlas returns before inserting a sprite. The
	// sprite counts of a test-window frame therefore carry no text.
	if pushed {
		if err := frame.scene.EndLayer(); err != nil {
			return err
		}
	}
	return nil
}

// paintLine implements shapedText: the inline piece path has no
// observable output in the stub (empty rasters; the paragraph layer
// carries the observable bounds).
func (t *testShapedText) paintLine(w *Window, lineIndex int, origin Point, baseline float32, color Hsla) error {
	return nil
}

// lineAdvance implements shapedText: the stub's single line advance.
func (t *testShapedText) lineAdvance(lineIndex int) float32 { return t.advance }

// lineRange implements shapedText: the stub's single line byte range.
func (t *testShapedText) lineRange(lineIndex int) (int, int) { return 0, len(t.text) }

// selectionRects implements shapedText: the stub's caret-stop geometry
// (the test platform's selection_bounds over pen positions).
func (t *testShapedText) selectionRects(start, end int, lineHeight float32) []TextRect {
	if start >= end {
		return nil
	}
	emWidth := t.fontSize * testTextAdvanceWidth / testTextUnitsPerEm
	from := testPenAt(t.text, emWidth, start)
	to := testPenAt(t.text, emWidth, end)
	low, high := min32(from, to), max32(from, to)
	return []TextRect{{X: low, Y: 0, W: high - low, H: lineHeight}}
}

// testPenAt returns the stub's pen x at a byte boundary (the
// accumulated em-width advance of the preceding chars).
func testPenAt(text string, emWidth float32, byteOffset int) float32 {
	position := float32(0)
	for _, ch := range text[:byteOffset] {
		position += emWidth * float32(utf16.RuneLen(ch))
	}
	return position
}

// ---------------------------------------------------------------------------
// Public shaped-text facts (the reference Window::text_system() →
// WindowTextSystem::shape_text → WrappedLine public fields)
// ---------------------------------------------------------------------------

// ShapedGlyph is one positioned glyph of a shaped text (the reference
// ShapedGlyph: id, position, is_emoji).
type ShapedGlyph struct {
	// ID is the glyph id.
	ID uint32
	// X is the glyph's line-local x position.
	X float32
	// Y is the glyph's baseline-relative y position.
	Y float32
	// IsEmoji reports color artwork.
	IsEmoji bool
}

// ShapedText is the observable shaped layout of one text through a
// window's text system (the reference WrappedLine's public layout
// facts: the advance width, ascent, descent, visual line count, the
// UTF-8 length and the positioned glyphs).
type ShapedText struct {
	// TextLen is the shaped text's UTF-8 length.
	TextLen int
	// FontSize is the document's font size.
	FontSize float32
	// Width is the shaped advance (LineLayout::width).
	Width float32
	// Ascent is the line ascent in pixels.
	Ascent float32
	// Descent is the line descent in pixels.
	Descent float32
	// LineCount is the visual line count.
	LineCount int
	// Glyphs are the positioned glyphs in paint order.
	Glyphs []ShapedGlyph
}

// ShapeText shapes the text through this window's text system under the
// given resolved font size and text style, returning the public shaped
// facts (the reference fixture observation path:
// window.text_system().shape_text(...)). The wrap width is optional.
func (w *Window) ShapeText(text string, style TextStyle, wrapWidth *float32) (ShapedText, error) {
	ds, ok := windowDrawStates[w]
	if !ok {
		return ShapedText{}, fmt.Errorf("gpui: ShapeText: the window has no text system")
	}
	if ds.text == nil {
		text, err := windowTextSystemOf()
		if err != nil {
			return ShapedText{}, fmt.Errorf("gpui: ShapeText: text system: %w", err)
		}
		ds.text = text
	}
	shaped, err := ds.text.shape(text, style.FontSizePixels(ds.rem), style.FontDescriptorOf(), wrapWidth, nil)
	if err != nil {
		return ShapedText{}, err
	}
	return ShapedText{
		TextLen:   shaped.textLen(),
		FontSize:  style.FontSizePixels(ds.rem),
		Width:     shaped.width(),
		Ascent:    shaped.ascent(),
		Descent:   shaped.descent(),
		LineCount: shaped.lineCount(),
		Glyphs:    shaped.(interface{ shapedGlyphs() []ShapedGlyph }).shapedGlyphs(),
	}, nil
}

// shapedGlyphs exposes the stub's glyphs for the public facts.
func (t *testShapedText) shapedGlyphs() []ShapedGlyph { return t.glyphs }

// shapedGlyphs exposes the real line's glyphs for the public facts.
func (s *realShapedText) shapedGlyphs() []ShapedGlyph {
	glyphs, err := s.line.Glyphs()
	if err != nil {
		return nil
	}
	out := make([]ShapedGlyph, 0, len(glyphs))
	for _, glyph := range glyphs {
		out = append(out, ShapedGlyph{ID: glyph.ID, X: glyph.X, Y: glyph.Y, IsEmoji: glyph.IsEmoji})
	}
	return out
}
