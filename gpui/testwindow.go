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
