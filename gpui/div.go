package gpui

// This file is the port's div element and text element (ticket11): the
// fluent Div()/DivElement builder with the fixture API surface, the text
// style stack types, the text element (the reference Text/SharedString
// element over a measured layout node), and the window text system seam.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/elements/div.rs — Div (the interactivity-driven
//     style, the children small-vec, DivFrameState with the child layout
//     ids), Interactivity::request_layout (compute the style, then the
//     layout request through the window), Interactivity::prepaint (the
//     text style scope, the overflow content mask, the scroll offset,
//     then the children prepaint), Interactivity::paint
//     (with_element_opacity, style.paint — background quad then
//     continuation then border — then the children paint);
//   - crates/gpui/src/style.rs — Style::paint / paint_box (the background
//     quad when non-transparent, the border quad when visible),
//     overflow_mask, TextStyle::default() and TextStyleRefinement;
//   - crates/gpui/src/elements/text.rs — the Text/SharedString element:
//     request_layout shapes through the window text system inside a
//     measured layout node (TextLayout::layout), prepaint commits the
//     parent-relative bounds (TextLayout::prepaint), paint draws the
//     shaped line at the bounds origin (TextLayout::paint).
//   - crates/gpui/src/text_system/line.rs — paint_visual_text: the line
//     bounds layer, the row placement (baseline = (line_height - ascent -
//     descent)/2 + ascent) and the per-glyph paint calls.
//
// Focus routing and key contexts landed with ticket13, hitboxes and the
// mouse listener dispatch with ticket24, inline layout with ticket15.
// Base ticket 06 (interact.go) adds the interactive element state, click
// synthesis, the mouse-down / up-out / hover / drag builders, the state
// styles and the cursor requests; Base ticket 07 (scroll_element.go)
// adds overflow scrolling: the scroll offset resolved at request layout,
// the content size and clamp at prepaint (children prepaint and paint
// under the scroll offset), and the default wheel listener at paint.

import (
	"fmt"
	"reflect"
	"slices"
)

// ---------------------------------------------------------------------------
// The text style stack (style.rs TextStyle / TextStyleRefinement)
// ---------------------------------------------------------------------------

// TextAlign is the text alignment within its element (the reference
// TextAlign; Left is the default).
type TextAlign uint8

const (
	// TextAlignLeft aligns text to the start of the line.
	TextAlignLeft TextAlign = iota
	// TextAlignCenter centers text in the line.
	TextAlignCenter
	// TextAlignRight aligns text to the end of the line.
	TextAlignRight
)

// WhiteSpace controls text wrapping (the reference WhiteSpace; Normal is
// the default).
type WhiteSpace uint8

const (
	// WhiteSpaceNormal wraps text at the wrap width.
	WhiteSpaceNormal WhiteSpace = iota
	// WhiteSpaceNowrap never wraps.
	WhiteSpaceNowrap
)

// TextStyle is the resolved text style of an element (style.rs:590-646
// TextStyle). The text element consumes the color, font, size, line
// height, alignment and white space; the remaining pinned fields
// (background color, underline, strikethrough, truncation, line clamp,
// letter spacing, case transform) are resolved and cascaded here for
// the text tickets that consume them.
type TextStyle struct {
	// Color is the text color.
	Color Hsla
	// FontFamily is the font family (".SystemUIFont" by default).
	FontFamily string
	// FontFeatures are the OpenType feature settings.
	FontFeatures []FontFeature
	// FontFallbacks are the fallback families (nil is the pinned None).
	FontFallbacks []string
	// FontSize is the font size in pixels or rems (1rem by default).
	FontSize AbsoluteLength
	// LineHeight is the line height in pixels, rems or a relative
	// fraction (the golden ratio by default).
	LineHeight DefiniteLength
	// FontWeight is the font weight (400 by default).
	FontWeight float32
	// FontStyle is the font style (Normal by default).
	FontStyle FontStyle
	// BackgroundColor is the text background color (nil = None).
	BackgroundColor *Hsla
	// Underline is the underline style (nil = None).
	Underline *UnderlineStyle
	// Strikethrough is the strikethrough style (nil = None).
	Strikethrough *StrikethroughStyle
	// WhiteSpace is the wrapping mode.
	WhiteSpace WhiteSpace
	// TextOverflow is the truncation behavior (nil = None).
	TextOverflow *TextOverflow
	// TextAlign is the alignment within the element.
	TextAlign TextAlign
	// LineClamp is the number of lines shown before truncating (nil =
	// None).
	LineClamp *int
	// LetterSpacing is the extra spacing between characters in pixels
	// (nil = None).
	LetterSpacing *float32
	// TextTransform is the layout-time case mapping (nil = None).
	TextTransform *TextTransform
}

// DefaultTextStyle returns the reference TextStyle::default()
// (style.rs:648-671): black text, the system UI font at 1rem, the
// golden-ratio line height, normal weight and style, left-aligned
// normal white space, and every optional field None.
func DefaultTextStyle() TextStyle {
	return TextStyle{
		Color:      Hsla{H: 0, S: 0, L: 0, A: 1},
		FontFamily: DefaultFontFamily,
		FontWeight: DefaultFontWeight,
		FontStyle:  FontStyleNormal,
		FontSize:   RemsOf(1),
		LineHeight: DefiniteLength{Kind: DefiniteLengthFraction, Value: goldenRatio},
		TextAlign:  TextAlignLeft,
		WhiteSpace: WhiteSpaceNormal,
	}
}

// TextStyleRefinement is the text style refinement an element cascades
// to its children (the derived TextStyleRefinement of style.rs:591).
// Absent fields leave the previous values unchanged; the pinned Option
// fields of TextStyle refine as Optional of their inner type.
type TextStyleRefinement struct {
	Color           Optional[Hsla]
	FontFamily      Optional[string]
	FontFeatures    Optional[[]FontFeature]
	FontFallbacks   Optional[[]string]
	FontSize        Optional[AbsoluteLength]
	LineHeight      Optional[DefiniteLength]
	FontWeight      Optional[float32]
	FontStyle       Optional[FontStyle]
	BackgroundColor Optional[Hsla]
	Underline       Optional[UnderlineStyle]
	Strikethrough   Optional[StrikethroughStyle]
	WhiteSpace      Optional[WhiteSpace]
	TextOverflow    Optional[TextOverflow]
	TextAlign       Optional[TextAlign]
	LineClamp       Optional[int]
	LetterSpacing   Optional[float32]
	TextTransform   Optional[TextTransform]
}

// Refine applies the explicitly supplied fields onto style (the derived
// Refineable::refine for TextStyle).
func (r *TextStyleRefinement) Refine(style *TextStyle) {
	refineValue(&style.Color, r.Color)
	refineValue(&style.FontFamily, r.FontFamily)
	if v, ok := r.FontFeatures.Get(); ok {
		style.FontFeatures = slices.Clone(v)
	}
	if v, ok := r.FontFallbacks.Get(); ok {
		style.FontFallbacks = slices.Clone(v)
	}
	refineValue(&style.FontSize, r.FontSize)
	refineValue(&style.LineHeight, r.LineHeight)
	refineValue(&style.FontWeight, r.FontWeight)
	refineValue(&style.FontStyle, r.FontStyle)
	refinePointer(&style.BackgroundColor, r.BackgroundColor)
	refinePointer(&style.Underline, r.Underline)
	refinePointer(&style.Strikethrough, r.Strikethrough)
	refineValue(&style.WhiteSpace, r.WhiteSpace)
	refinePointer(&style.TextOverflow, r.TextOverflow)
	refineValue(&style.TextAlign, r.TextAlign)
	refinePointer(&style.LineClamp, r.LineClamp)
	refinePointer(&style.LetterSpacing, r.LetterSpacing)
	refinePointer(&style.TextTransform, r.TextTransform)
}

// Merge applies the present fields of other onto r (the derived
// Refineable::refine for TextStyleRefinement; named Merge because Refine
// applies a refinement onto a TextStyle).
func (r *TextStyleRefinement) Merge(other *TextStyleRefinement) {
	if other == nil {
		return
	}
	mergeOptional(&r.Color, other.Color)
	mergeOptional(&r.FontFamily, other.FontFamily)
	if v, ok := other.FontFeatures.Get(); ok {
		r.FontFeatures = Some(slices.Clone(v))
	}
	if v, ok := other.FontFallbacks.Get(); ok {
		r.FontFallbacks = Some(slices.Clone(v))
	}
	mergeOptional(&r.FontSize, other.FontSize)
	mergeOptional(&r.LineHeight, other.LineHeight)
	mergeOptional(&r.FontWeight, other.FontWeight)
	mergeOptional(&r.FontStyle, other.FontStyle)
	mergeOptional(&r.BackgroundColor, other.BackgroundColor)
	mergeOptional(&r.Underline, other.Underline)
	mergeOptional(&r.Strikethrough, other.Strikethrough)
	mergeOptional(&r.WhiteSpace, other.WhiteSpace)
	mergeOptional(&r.TextOverflow, other.TextOverflow)
	mergeOptional(&r.TextAlign, other.TextAlign)
	mergeOptional(&r.LineClamp, other.LineClamp)
	mergeOptional(&r.LetterSpacing, other.LetterSpacing)
	mergeOptional(&r.TextTransform, other.TextTransform)
}

// IsSome reports whether any field is present (the derived
// Refineable is_some, used by Style::text_style).
func (r *TextStyleRefinement) IsSome() bool {
	return r.Color.present || r.FontFamily.present || r.FontFeatures.present ||
		r.FontFallbacks.present || r.FontSize.present || r.LineHeight.present ||
		r.FontWeight.present || r.FontStyle.present || r.BackgroundColor.present ||
		r.Underline.present || r.Strikethrough.present || r.WhiteSpace.present ||
		r.TextOverflow.present || r.TextAlign.present || r.LineClamp.present ||
		r.LetterSpacing.present || r.TextTransform.present
}

// Clone returns an independent copy (the slice-valued fields copied).
func (r TextStyleRefinement) Clone() TextStyleRefinement {
	out := r
	if v, ok := r.FontFeatures.Get(); ok {
		out.FontFeatures = Some(slices.Clone(v))
	}
	if v, ok := r.FontFallbacks.Get(); ok {
		out.FontFallbacks = Some(slices.Clone(v))
	}
	return out
}

// FontSizePixels resolves the font size against the window rem size
// (AbsoluteLength::to_pixels).
func (t TextStyle) FontSizePixels(rem float32) float32 {
	return t.FontSize.ToPixels(rem)
}

// LineHeightPixels resolves the line height against the font size and
// rem size (DefiniteLength::to_pixels: pixels pass through, rems resolve
// through the rem size, fractions scale the font size).
func (t TextStyle) LineHeightPixels(fontSize, rem float32) float32 {
	switch t.LineHeight.Kind {
	case DefiniteLengthPx:
		return t.LineHeight.Value
	case DefiniteLengthRem:
		return t.LineHeight.Value * rem
	default:
		return fontSize * t.LineHeight.Value
	}
}

// FontDescriptorOf builds the shaping font descriptor of this text
// style (style.rs:708 TextStyle::font / to_run: family, features,
// fallbacks, weight, style).
func (t TextStyle) FontDescriptorOf() FontDescriptor {
	return FontDescriptor{
		Family:    t.FontFamily,
		Weight:    t.FontWeight,
		Style:     t.FontStyle,
		Features:  slices.Clone(t.FontFeatures),
		Fallbacks: slices.Clone(t.FontFallbacks),
	}
}

// ---------------------------------------------------------------------------
// The window text system seam
// ---------------------------------------------------------------------------

// windowTextSystem shapes and paints text for one window (the reference
// Window's Arc<WindowTextSystem>). The test window installs the
// deterministic port of the reference TestTextSystem; real windows use
// the port's real text stack.
type windowTextSystem interface {
	// shape shapes the text at the resolved font size with one style run
	// of the given font, under the optional wrap width and line clamp.
	shape(text string, fontSize float32, font FontDescriptor, wrapWidth *float32, lineClamp *uint32) (shapedText, error)
	// finishFrame ends one frame's shaped-line usage (the reference
	// WindowTextSystem::finish_frame, called by the window draw's tail):
	// the window's line-layout cache rotates — the lines the frame
	// requested survive into the next frame's previous-frame cache, the
	// rest release. A stateless test system has nothing to rotate.
	finishFrame()
	// layoutIndex, reuseLayouts and truncateLayouts are the line-layout
	// index seam of the cached views' range reuse (WindowTextSystem::
	// layout_index / reuse_layouts / truncate_layouts →
	// LineLayoutCache, text_cache.go). A stateless test system has no
	// cache and answers an empty index.
	layoutIndex() int
	reuseLayouts(start, end int)
	truncateLayouts(index int)
	// layoutInline lays out one inline document (text plus atomic
	// element boxes) in one inline formatting context (the reference
	// PlatformTextSystem::layout_inline). Go owns the algorithm on both
	// stacks: the test system ports the reference
	// TestTextSystem::layout_inline exactly, and the real system
	// composes the native shaping seam with the Go greedy row/box
	// algorithm (the native ABI exposes no box-aware shaping).
	layoutInline(request inlineLayoutRequest) (inlineLayout, error)
	// inlineMetrics returns the font metrics of one descriptor at a
	// font size for an inline container's base font (the reference
	// WindowTextSystem::ascent / descent / x_height).
	inlineMetrics(font FontDescriptor, fontSize float32) InlineTextMetrics
	// glyphForChar maps one character to its nominal glyph id in the
	// font a descriptor resolves to (the public TextSystem::GlyphForChar
	// over ResolveFont): the CSS ellipsis fallback ("…" → "...") reads
	// it, Chrome's SimpleFontData::GlyphForCharacter equivalent. ok is
	// false when the character maps to no glyph (the modeled test font
	// maps every character).
	glyphForChar(font FontDescriptor, ch rune) (glyphID uint32, ok bool)
}

// shapedText is one shaped document handle (the reference WrappedLine
// surface the text element consumes: the layout facts plus painting).
type shapedText interface {
	// width is the shaped advance (LineLayout::width).
	width() float32
	// ascent is the line ascent in pixels.
	ascent() float32
	// descent is the line descent in pixels.
	descent() float32
	// fontSize is the document's font size (the shaping request's), the
	// auto decoration geometry derives from (Chrome's used font size in
	// text_decoration_thickness.cc:29-31).
	fontSize() float32
	// lineCount is the visual line count.
	lineCount() int
	// textLen is the shaped text's UTF-8 length.
	textLen() int
	// size is the wrapped size for a line height (WrappedLine::size).
	size(lineHeight float32) Size
	// paint paints the line at the origin with the window's current
	// paint state (WrappedLine::paint → paint_visual_text).
	paint(w *Window, origin Point, lineHeight float32, color Hsla) error
	// paintLine paints one visual line at the given origin and
	// baseline (the inline piece paint path; the ordinary paint
	// derives its baseline from the line height — paint_visual_line).
	paintLine(w *Window, lineIndex int, origin Point, baseline float32, color Hsla) error
	// lineAdvance returns one visual line's shaped advance (the
	// VisualLine::advance_width fact the inline row assembly reads).
	lineAdvance(lineIndex int) float32
	// lineRange returns one visual line's byte range (the
	// VisualLine::text_range fact).
	lineRange(lineIndex int) (start, end int)
	// selectionRects returns the piece-local rectangles covering a
	// byte range (the inline geometry source: the reference
	// platform_layout.selection_bounds feeding inline_geometry).
	selectionRects(start, end int, lineHeight float32) []TextRect
}

// realWindowTextSystem is the real text stack seam: shaping through the
// port's TextSystem and painting through the scene's shaped-text paint
// path (PaintShapedText) with the window's atlas. It is the reference
// WindowTextSystem: one per window, carrying the window's line-layout
// cache (text_cache.go, the reference LineLayoutCache) so shaped lines
// are reused across frames instead of re-shaped every frame.
type realWindowTextSystem struct {
	system *TextSystem
	atlas  *Atlas
	// lines is the window's line-layout cache (the reference
	// WindowTextSystem's line_layout_cache): shaped lines cached across
	// frames, disposed when superseded (ticket 02).
	lines shapedLineCache
}

// windowTextSystemOf returns the real text system seam for one
// window: the process-global TextSystem plus a fresh per-window atlas
// and line-layout cache (the reference WindowTextSystem::new per
// window).
func windowTextSystemOf() (windowTextSystem, error) {
	system, err := DefaultTextSystem()
	if err != nil {
		return nil, err
	}
	atlas, err := NewAtlas()
	if err != nil {
		return nil, err
	}
	return &realWindowTextSystem{system: system, atlas: atlas}, nil
}

// shape implements windowTextSystem.
func (r *realWindowTextSystem) shape(text string, fontSize float32, font FontDescriptor, wrapWidth *float32, lineClamp *uint32) (shapedText, error) {
	request := TextLayoutRequest{
		Text:      text,
		FontSize:  fontSize,
		Runs:      []TextRun{{Len: len(text), Font: font}},
		WrapWidth: wrapWidth,
		LineClamp: lineClamp,
	}
	// The reference shape_text resolves through the window's
	// line-layout cache (WindowTextSystem::shape_text →
	// LineLayoutCache::layout_wrapped_line): the same text, font size,
	// runs, wrap width and line clamp reuse the shaped line across
	// frames, and the superseded lines release at the frame rotation.
	line, err := r.lines.layoutWrappedLine(r.system, request)
	if err != nil {
		return nil, err
	}
	return &realShapedText{line: line, system: r.system, atlas: r.atlas}, nil
}

// finishFrame implements windowTextSystem: rotate the window's
// line-layout cache (WindowTextSystem::finish_frame →
// LineLayoutCache::finish_frame), invoked by the window draw's
// completion (render.go completeFrame, window.rs:3230).
func (r *realWindowTextSystem) finishFrame() { r.lines.finishFrame() }

// layoutIndex, reuseLayouts and truncateLayouts implement
// windowTextSystem through the window's line-layout cache.
func (r *realWindowTextSystem) layoutIndex() int            { return r.lines.layoutIndex() }
func (r *realWindowTextSystem) reuseLayouts(start, end int) { r.lines.reuseLayouts(start, end) }
func (r *realWindowTextSystem) truncateLayouts(index int)   { r.lines.truncateLayouts(index) }

// realShapedText adapts the port's WrappedLine into the shapedText seam.
type realShapedText struct {
	line   *WrappedLine
	system *TextSystem
	atlas  *Atlas
}

// width implements shapedText.
func (s *realShapedText) width() float32 {
	summary, err := s.line.Summary()
	if err != nil {
		return 0
	}
	return summary.Width
}

// ascent implements shapedText.
func (s *realShapedText) ascent() float32 {
	summary, err := s.line.Summary()
	if err != nil {
		return 0
	}
	return summary.Ascent
}

// descent implements shapedText.
func (s *realShapedText) descent() float32 {
	summary, err := s.line.Summary()
	if err != nil {
		return 0
	}
	return summary.Descent
}

// fontSize implements shapedText: the shaping request's font size
// (Chrome's decoration thickness derives from the used font size,
// text_decoration_thickness.cc:29-31).
func (s *realShapedText) fontSize() float32 {
	return s.line.request.FontSize
}

// lineCount implements shapedText.
func (s *realShapedText) lineCount() int {
	summary, err := s.line.Summary()
	if err != nil {
		return 0
	}
	return summary.LineCount
}

// textLen implements shapedText.
func (s *realShapedText) textLen() int {
	summary, err := s.line.Summary()
	if err != nil {
		return 0
	}
	return summary.TextLen
}

// size implements shapedText: min(the wrap constraint, the shaped
// advance) by the line-height-scaled row count.
func (s *realShapedText) size(lineHeight float32) Size {
	summary, err := s.line.Summary()
	if err != nil {
		return Size{}
	}
	width := summary.Width
	if summary.WrapWidth != nil && *summary.WrapWidth < width {
		width = *summary.WrapWidth
	}
	return Size{Width: width, Height: lineHeight * float32(summary.LineCount)}
}

// paint implements shapedText (the shaped glyph loop through the scene's
// paint path with the current window paint state).
func (s *realShapedText) paint(w *Window, origin Point, lineHeight float32, color Hsla) error {
	frame := currentFrame(w)
	return frame.scene.PaintShapedText(s.line, origin, lineHeight, color, s.system, s.atlas, frame.paint,
		PaintTextOptions{Background: frame.paint.Background})
}

// frameTextSystem returns the window's text system seam.
func frameTextSystem(w *Window) windowTextSystem { return currentFrame(w).text }

// ---------------------------------------------------------------------------
// The text element (elements/text.rs Text / SharedString)
// ---------------------------------------------------------------------------

// stringLayoutState is the text element's request-layout state (the
// reference TextLayout): the measured layout node, the resolved line
// height and the cached shaped document (shared with the measure
// closure through a cell, exactly like the reference's Arc-shared
// layout cache).
type stringLayoutState struct {
	// layoutID is the measured layout node.
	layoutID LayoutID
	// lineHeight is the pixel-snapped resolved line height.
	lineHeight float32
	// cell is the shared shaped-document cache written by the measure
	// closure during layout compute.
	cell *shapedCell
	// fontSize is the resolved font size.
	fontSize float32
	// bounds is committed by prepaint (parent-relative).
	bounds *Bounds
}

// shapedCell is the shared shaped-document cache of one text element
// (the reference TextLayoutInner: the document, the wrap width it was
// shaped under, and its measured size).
type shapedCell struct {
	shaped    shapedText
	wrapWidth *float32
	// truncateWidth is the truncation width the document was shaped
	// under (nil: not truncated).
	truncateWidth *float32
	// runs are the style runs covering the SHAPED text (the ambient
	// style's run, cut by truncation): the paint fields (color,
	// background, underline, strikethrough) the element paints with,
	// like the pin's document-carried runs.
	runs []TextRun
	size Size
}

// textElement is the Text element: a string displayed as one shaped,
// measured text block (the reference Text/SharedString element).
type textElement struct {
	// text is the displayed string.
	text string
	// id is the optional element identity.
	id *ElementID
}

// ID implements Element.
func (t *textElement) ID() (ElementID, bool) {
	if t.id == nil {
		return ElementID{}, false
	}
	return *t.id, true
}

// SourceLocation implements Element.
func (t *textElement) SourceLocation() *SourceLocation { return nil }

// A11yRole implements the accessibility role capability (Text::a11y_role
// — a label when identified).
func (t *textElement) A11yRole() (Role, bool) {
	if t.id == nil {
		return 0, false
	}
	// A text element with an id is a Label (elements/text.rs:189-191).
	return RoleLabel, true
}

// WriteA11yInfo implements the property capability (Text::write_a11y_info:
// the node value is the text).
func (t *textElement) WriteA11yInfo(node *Node) {
	if node != nil {
		node.Value = t.text
	}
}

// RequestLayout implements Element: resolve the ambient text style, then
// request a MEASURED layout node whose measure function shapes the text
// through the window's text system (the reference TextLayout::layout,
// including its cached-layout reuse under matching wrap widths), and
// publish the text as this node's inline content so a parent paragraph
// can collect it (the reference layout publishes InlineContent::Text
// after requesting the node).
func (t *textElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, stringLayoutState) {
	textStyle := WindowTextStyle(w)
	rem := RemSize(w)
	fontSize := textStyle.FontSizePixels(rem)
	lineHeight := PixelSnap(w, textStyle.LineHeightPixels(fontSize, rem))

	cell := &shapedCell{}
	state := stringLayoutState{lineHeight: lineHeight, fontSize: fontSize, cell: cell}
	// The pinned TextLayout::layout applies the style's text-transform
	// before shaping (elements/text.rs:895): the shaped document and the
	// published inline content carry the transformed text (byte length
	// preserved, so the covering runs stay valid).
	text := applyTextTransformPreservingByteLen(t.text, textStyle.TextTransform)
	measure := func(req MeasureRequest) Size {
		// The pinned measure closure: wrap at the known width or the
		// definite available width (WhiteSpace::Normal); min/max-content
		// content-based sizing measures unwrapped.
		var wrap float32
		hasWrap := false
		if textStyle.WhiteSpace == WhiteSpaceNowrap {
			hasWrap = false
		} else if req.KnownWidthPresent {
			wrap, hasWrap = req.KnownWidth, true
		} else if req.AvailWidth.Kind == AvailDefinite {
			wrap, hasWrap = req.AvailWidth.Definite, true
		}
		truncation := EvaluateTextOverflow(textStyle, req)
		// The CSS ellipsis marker resolves from the primary font ("…" or
		// "...", text_truncate.go).
		truncation = resolveTruncationAffix(frameTextSystem(w), textStyle.FontDescriptorOf(), truncation)
		// Cached-layout reuse (R:elements/text.rs:931-946): a shaped
		// layout with a size answers when the wrap is unconstrained or
		// matches the cached wrap, and neither this request nor the
		// cached layout truncates.
		if cell.shaped != nil && (!hasWrap || (cell.wrapWidth != nil && wrap == *cell.wrapWidth)) &&
			truncation.Width == nil && cell.truncateWidth == nil {
			return cell.size
		}
		// The wrap width is boxed only when the text shapes (gpui-perf
		// ticket 06: cache hits do not allocate).
		var wrapWidth *float32
		if hasWrap {
			boxed := wrap
			wrapWidth = &boxed
		}
		seam := frameTextSystem(w)
		// The pinned TextLayout::layout shapes the ambient style's run
		// (elements/text.rs:902-915: vec![text_style.to_run(text.len())])
		// so the document paints with its runs — the decorations of a
		// plain text child live here, not only in StyledText.
		runs := []TextRun{textStyle.ToRun(len(text))}
		// A per-invocation copy: the truncation below rewrites it, and
		// the measure closure may run more than once.
		effective := text
		if truncation.Width != nil {
			effective, runs = applyTextTruncation(effective, runs, wrapWidth, textStyle.LineClamp, truncation,
				func(candidate string, candidateRuns []TextRun, wrapWidth *float32) (shapedText, error) {
					return shapeStyledRuns(seam, candidate, fontSize, candidateRuns, wrapWidth, nil)
				})
		}
		shaped, err := shapeStyledRuns(seam, effective, fontSize, runs, wrapWidth, textLineClamp(textStyle))
		if err != nil {
			panic(fmt.Sprintf("gpui: text element shaping failed: %v", err))
		}
		cell.shaped = shaped
		cell.wrapWidth = wrapWidth
		cell.truncateWidth = truncation.Width
		cell.runs = runs
		cell.size = shaped.size(lineHeight)
		return cell.size
	}
	layoutID, err := requestMeasuredLayoutOf(w, DefaultStyle(), measure)
	if err != nil {
		panic(fmt.Sprintf("gpui: text element request_layout: %v", err))
	}
	state.layoutID = layoutID
	// The published runs are the runs the element would shape with: one
	// run of the ambient text style covering the text, paint fields
	// included (TextStyle::to_run, elements/text.rs:903-915), so a nested
	// inline span's own color and decorations survive the paragraph.
	publishInlineContent(w, layoutID, &inlineTextContent{
		text:        text,
		runs:        []TextRun{textStyle.ToRun(len(text))},
		fontSize:    fontSize,
		lineHeight:  lineHeight,
		paintStyled: true,
	})
	return layoutID, state
}

// Prepaint implements Element: commit the parent-relative bounds (the
// reference TextLayout::prepaint uses parent_relative_layout_bounds so
// text placement stays stable when its container moves). Inside a
// parent paragraph the element's prepaint is a no-op — the paragraph
// owns its geometry (window.current_inline_fragments is Some).
func (t *textElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *stringLayoutState, w *Window, app *App) struct{} {
	if currentFrame(w).currentInlineFragments != nil {
		return struct{}{}
	}
	textBounds, err := parentRelativeLayoutBoundsOf(w, layout.layoutID)
	if err != nil {
		panic(fmt.Sprintf("gpui: text element prepaint bounds: %v", err))
	}
	layout.bounds = &textBounds
	return struct{}{}
}

// Paint implements Element: draw the shaped line at the committed
// bounds origin (the reference TextLayout::paint with the ambient text
// style, elements/text.rs:1021-1060: paint_background then paint, each
// aligned within the element bounds). Inside a parent paragraph the
// paragraph paints the text, so the element's paint is a no-op.
func (t *textElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *stringLayoutState, prepaint *struct{}, w *Window, app *App) {
	if currentFrame(w).currentInlineFragments != nil {
		return
	}
	if layout.cell == nil || layout.cell.shaped == nil || layout.bounds == nil {
		panic(fmt.Sprintf("gpui: measurement or prepaint has not been performed on %q", t.text))
	}
	textStyle := WindowTextStyle(w)
	// The runs the document was shaped with (the ambient style's run,
	// cut by truncation): the paint fields — color, background,
	// underline, strikethrough — paint from them, like the pin's
	// document-carried runs. A cell shaped before this field existed
	// (the same frame) falls back to the ambient run over the shaped
	// text.
	runs := layout.cell.runs
	if len(runs) == 0 {
		runs = []TextRun{textStyle.ToRun(layout.cell.shaped.textLen())}
	}
	align := textStyle.TextAlign
	alignWidth := layout.bounds.Size.Width
	if err := paintStyledDocument(w, layout.cell.shaped, runs, layout.bounds.Origin, layout.lineHeight, align, &alignWidth, textPaintBackground); err != nil {
		panic(fmt.Sprintf("gpui: text element paint background: %v", err))
	}
	if err := paintStyledDocument(w, layout.cell.shaped, runs, layout.bounds.Origin, layout.lineHeight, align, &alignWidth, textPaintForeground); err != nil {
		panic(fmt.Sprintf("gpui: text element paint: %v", err))
	}
}

// LayoutNodeStyle implements the root-stretch report (the text node's
// style is the default style).
func (t *textElement) LayoutNodeStyle() (Style, bool) { return DefaultStyle(), true }

// ---------------------------------------------------------------------------
// Div (elements/div.rs Div / Interactivity, bounded to this slice)
// ---------------------------------------------------------------------------

// typedKeyListener is one registered key listener: the raw callback
// and its phase (capture or bubble).
type typedKeyListener struct {
	// capture routes the listener on the capture phase (bubble default).
	capture bool
	// f is the plain listener shape (func(*KeyDownEvent, *Window, *App)
	// or func(*KeyUpEvent, *Window, *App)).
	f any
}

// typedActionListener is one registered action listener (OnAction /
// CaptureAction / OnBoxedAction): the action descriptor, the phase and
// the plain handler shape. Dispatch routes them through the dispatch
// tree since ticket13.
type typedActionListener struct {
	// capture routes the handler on the capture phase (bubble default).
	capture bool
	// action is the action descriptor (Action[A] or BoxedAction) the
	// listener binds to.
	action any
	// handler is the plain listener shape (func(*A, *Window, *App) or
	// func(BoxedAction, *Window, *App)).
	handler any
}

// divFrameState is the div's request-layout state (the reference
// DivFrameState): the children's layout ids, the inline frame state of
// a Block/Inline div, and whether the div's contents participate in a
// parent paragraph (committed at prepaint).
type divFrameState struct {
	// childLayoutIDs are the requested layout nodes of the children.
	childLayoutIDs []LayoutID
	// layoutID is this div's own layout node (browser-core ticket 34:
	// the sticky prepaint resolves the div's flow containing block —
	// its native layout parent — from it).
	layoutID LayoutID
	// inline is the inline frame state (Block/Inline display only).
	inline *inlineDivFrameState
	// contents records a display:contents div (browser-core 36): no
	// node of its own — childLayoutIDs holds the spliced children and
	// layoutID the first child's node (or an inert none node when the
	// div has no children).
	contents bool
	// contentsInParentParagraph records that this div's own layout node
	// carries placed fragments, so its contents belong to the parent's
	// paragraph and its own paragraphs stay unplaced (the reference
	// contents_in_parent_paragraph).
	contentsInParentParagraph bool
}

// standaloneInline returns the div's own inline frame state when it
// should drive this div's layout (not participating in a parent
// paragraph).
func (s *divFrameState) standaloneInline() *inlineDivFrameState {
	if s.contentsInParentParagraph {
		return nil
	}
	return s.inline
}

// DivElement is the concrete div element builder (the fixture API
// spelling: the Div() constructor returns *DivElement because a type and
// a function cannot share the package-level identifier Div).
type DivElement struct {
	// Styled is the div's base style refinement and the whole pinned
	// Styled vocabulary (div.rs Interactivity::base_style, refined by
	// every Styled method; ticket base-05).
	Styled[*DivElement]
	// a11y is the accessibility authoring state, nil until an a11y
	// builder runs (div_a11y.go).
	a11y *divA11yState
	// styleBoxes lets the div's per-phase style computations share their
	// boxed style values (styled_refinement.go).
	styleBoxes styleBoxes
	// elementID is the optional element identity (div.rs
	// Interactivity::element_id).
	elementID *ElementID
	// focusHandle is the tracked focus handle (ticket13 routes it).
	focusHandle *FocusHandle
	// keyContext is the key context name (ticket13's keymap layer).
	keyContext *string
	// children are the converted child elements.
	children []AnyElement
	// clickListeners are the registered click listeners (div.rs:2615
	// click_listeners; delivered by the mouse-up pairing and the
	// keyboard activation, interact.go).
	clickListeners []func(*ClickEvent, *Window, *App)
	// auxClickListeners are the non-primary click listeners (div.rs:2616
	// aux_click_listeners).
	auxClickListeners []func(*ClickEvent, *Window, *App)
	// mouseDownListeners are the registered mouse-down listeners
	// (div.rs:2602 mouse_down_listeners).
	mouseDownListeners []typedMouseListener[MouseDownEvent]
	// dragListener starts a drag once the mouse moves past the drag
	// threshold (div.rs:2617 drag_listener).
	dragListener func(cursorOffset Point, cursorStyle *CursorStyle, w *Window, app *App) AnyDrag
	// hoverListeners observe hover transitions (div.rs:2618
	// hover_listeners).
	hoverListeners []func(*bool, *Window, *App)
	// pointerEnterListeners observe the DOM pointerenter boundary event
	// (browser-core ticket 10; fired outermost-first from the hover
	// boundary sweeps, hoverboundary.go).
	pointerEnterListeners []func(*PointerEnterEvent, *Window, *App)
	// pointerLeaveListeners observe the DOM pointerleave boundary event
	// (browser-core ticket 10; fired innermost-first).
	pointerLeaveListeners []func(*PointerLeaveEvent, *Window, *App)
	// gotPointerCaptureListeners observe the DOM gotpointercapture event
	// (browser-core ticket 11; fired target-first from the pointer
	// capture sweeps, pointercapture.go).
	gotPointerCaptureListeners []func(*GotPointerCaptureEvent, *Window, *App)
	// lostPointerCaptureListeners observe the DOM lostpointercapture
	// event (browser-core ticket 11; fired target-first).
	lostPointerCaptureListeners []func(*LostPointerCaptureEvent, *Window, *App)
	// group names this element's hitbox for group styles (div.rs:2586).
	group *string
	// hoverStyle, activeStyle, focusStyle, inFocusStyle and
	// focusVisibleStyle are the state style refinements (div.rs:2591-2596).
	hoverStyle        *StyleRefinement
	activeStyle       *StyleRefinement
	focusStyle        *StyleRefinement
	inFocusStyle      *StyleRefinement
	focusVisibleStyle *StyleRefinement
	// groupHoverStyle and groupActiveStyle refine this element while its
	// named group is hovered / pressed (div.rs:2595, 2597).
	groupHoverStyle  *GroupStyle
	groupActiveStyle *GroupStyle
	// actionListeners are the registered action listeners (dispatch
	// routed by the dispatch tree since ticket13).
	actionListeners []typedActionListener
	// keyDownListeners are the registered key-down listeners
	// (div.rs key_down_listeners).
	keyDownListeners []typedKeyListener
	// keyUpListeners are the registered key-up listeners
	// (div.rs key_up_listeners).
	keyUpListeners []typedKeyListener
	// modifiersChangedListeners are the registered modifiers-changed
	// listeners (div.rs modifiers_changed_listeners).
	modifiersChangedListeners []func(*ModifiersChangedEvent, *Window, *App)
	// mouseMoveListeners are the registered mouse-move listeners
	// (ticket24; div.rs mouse_move_listeners).
	mouseMoveListeners []typedMouseListener[MouseMoveEvent]
	// mouseUpListeners are the registered mouse-up listeners (div.rs
	// mouse_up_listeners).
	mouseUpListeners []typedMouseListener[MouseUpEvent]
	// scrollWheelListeners are the registered scroll listeners (div.rs
	// scroll_wheel_listeners).
	scrollWheelListeners []typedMouseListener[ScrollWheelEvent]
	// pinchListeners are the registered pinch listeners (div.rs
	// pinch_listeners).
	pinchListeners []typedMouseListener[PinchEvent]
	// dropListeners are the type-keyed drop listeners (div.rs
	// drop_listeners: (TypeId, DropListener<dyn Any>)).
	dropListeners []typedDropListener
	// canDropPredicate is the element-level drop predicate (div.rs
	// can_drop_predicate; CanDrop): hovered and the predicate.
	canDropPredicate func(value any, hitbox *Hitbox, w *Window, app *App) bool
	// dragOverStyle is the type-agnostic drag-over style (div.rs
	// drag_over_style; OnDragOverAny).
	dragOverStyle func(value any, w *Window, app *App) StyleRefinement
	// groupDragOverStyles are the type-keyed group drag-over styles
	// (div.rs group_drag_over_styles; GroupDragOver).
	groupDragOverStyles []typedGroupStyle
	// hitboxBehavior is this element's hitbox occlusion behavior (div.rs
	// Interactivity hitbox behavior; Normal by default).
	hitboxBehavior HitboxBehavior
	// windowControl marks the element as a window control area (div.rs
	// Interactivity window_control); nil when unset.
	windowControl *WindowControlArea
	// tabIndex is the interactivity tab index (div.rs tab_index).
	tabIndex *int64
	// tabGroup marks a tab group (div.rs tab_group).
	tabGroup bool
	// tabStop records an interactivity tab-stop request (div.rs
	// tab_stop; explicit track_focus handles carry their own).
	tabStop bool
	// trackedScrollHandle is the tracked scroll handle (div.rs:2582
	// tracked_scroll_handle; Base ticket 07, scroll_element.go).
	trackedScrollHandle *ScrollHandle
	// scroll is this frame's resolved scroll slice (div.rs:2584-2585
	// scroll_offset / ongoing_scroll, assigned at request layout,
	// div.rs:2735-2755); nil when the element does not scroll.
	scroll *elementScroll
	// scrollStateful records a StatefulInteractiveElement scroll builder
	// (OverflowScroll / OverflowXScroll / OverflowYScroll /
	// RestrictScrollToAxis / TrackScroll), which requires an element ID.
	scrollStateful bool
	// debugSelector is the test-support bounds key.
	debugSelector string
	// converting guards reentrant child mutation/conversion (the
	// conversion gate of the authoring contract).
	converting bool
	// lastStyle is the computed style of the last layout request (the
	// root-stretch report).
	lastStyle Style
}

// Div creates a new div element (the fixture constructor spelling).
func Div() *DivElement {
	return BindStyled(&DivElement{})
}

// IntoElementID is the set of values convertible to an element identity
// (the pinned `impl Into<ElementId>`): a name string (ElementId::Name) or
// any ElementID (named integers, named children, code locations, ...).
type IntoElementID interface {
	string | ElementID
}

// ElementIDOf converts a string or ElementID into an ElementID (a string
// is NameElementID).
func ElementIDOf[I IntoElementID](value I) ElementID {
	switch v := any(value).(type) {
	case string:
		return NameElementID(v)
	case ElementID:
		return v
	}
	panic("gpui: unreachable IntoElementID")
}

// ID assigns this element an identity (div.rs InteractiveElement::id,
// `impl Into<ElementId>`): a string names it; any ElementID is used as
// is. Identity participates in the global element id path and keys
// retained element state.
func (d *DivElement) ID[I IntoElementID](value I) *DivElement {
	id := ElementIDOf(value)
	d.elementID = &id
	return d
}

// TrackFocus registers a focus handle with this element (the reference
// InteractiveElement::track_focus; routing arrives with ticket13).
func (d *DivElement) TrackFocus(value FocusHandle) *DivElement {
	d.focusHandle = &value
	return d
}

// KeyContext sets the key context of this element (ticket13's keymap
// layer reads it during dispatch).
func (d *DivElement) KeyContext(value string) *DivElement {
	d.keyContext = &value
	return d
}

// The Styled vocabulary is promoted from the embedded Styled[*DivElement]
// (styled.go, styled_generated.go). The methods below are the port's own
// div builders that predate it: Size and Gap keep their legacy
// two-argument form next to the pinned one-argument form, and Justify,
// AlignItems, GridTemplateCols, GridTemplateRows and GridAt are the
// port's direct setters of the corresponding style fields.

// Size sets the width and height (styles.rs:1348 `size`): one argument
// sets both, as pinned; two arguments set width then height (the port's
// original Size(width, height) form). Any other count panics.
func (d *DivElement) Size[L IntoLength](lengths ...L) *DivElement {
	style := d.StyleMut()
	switch len(lengths) {
	case 1:
		v := LengthOf(lengths[0])
		style.SizeWidth, style.SizeHeight = Some(v), Some(v)
	case 2:
		style.SizeWidth, style.SizeHeight = Some(LengthOf(lengths[0])), Some(LengthOf(lengths[1]))
	default:
		panic(fmt.Sprintf("gpui: DivElement.Size takes one or two lengths, got %d", len(lengths)))
	}
	return d
}

// Gap sets the gap between children (styles.rs:1396 `gap`): one argument
// sets both axes, as pinned; two arguments set the column (width) then
// row (height) gap (the port's original Gap(width, height) form). Any
// other count panics.
func (d *DivElement) Gap[L IntoDefiniteLength](lengths ...L) *DivElement {
	style := d.StyleMut()
	switch len(lengths) {
	case 1:
		v := DefiniteLengthFrom(lengths[0])
		style.GapWidth, style.GapHeight = Some(v), Some(v)
	case 2:
		style.GapWidth, style.GapHeight = Some(DefiniteLengthFrom(lengths[0])), Some(DefiniteLengthFrom(lengths[1]))
	default:
		panic(fmt.Sprintf("gpui: DivElement.Gap takes one or two lengths, got %d", len(lengths)))
	}
	return d
}

// GridTemplateCols sets the column track template
// (grid-template-columns: repeat(n, minmax(min, 1fr)) in the pinned
// simplified form; the pinned builders are GridCols and its variants).
func (d *DivElement) GridTemplateCols(template GridTemplate) *DivElement {
	d.StyleMut().GridCols = Some(template)
	return d
}

// GridTemplateRows sets the row track template (grid-template-rows).
func (d *DivElement) GridTemplateRows(template GridTemplate) *DivElement {
	d.StyleMut().GridRows = Some(template)
	return d
}

// GridAt places this item in its parent grid (the reference
// col_row/row_col placement; the pinned builders are ColStart, RowSpan
// and their variants).
func (d *DivElement) GridAt(location GridLocation) *DivElement {
	d.StyleMut().GridLocation = Some(location)
	return d
}

// Justify sets the main-axis content distribution (the justify_content
// field; the pinned builders are the Justify* methods).
func (d *DivElement) Justify(justify AlignContent) *DivElement {
	d.StyleMut().JustifyContent = Some(justify)
	return d
}

// AlignItems sets the cross-axis item alignment (the align_items field;
// the pinned builders are the Items* methods).
func (d *DivElement) AlignItems(align AlignItems) *DivElement {
	d.StyleMut().AlignItems = Some(align)
	return d
}

// Extend appends already-converted children (element.rs:230
// ParentElement::extend), e.g. a component's collected Parent children.
func (d *DivElement) Extend(elements []AnyElement) *DivElement {
	if len(elements) == 0 {
		return d
	}
	d.commitChildren("DivElement.Extend", len(elements), elements)
	return d
}

// DebugSelector sets the test-support bounds key of this element (the
// reference debug_selector: the element's bounds are recorded per frame
// and queryable through the test window).
func (d *DivElement) DebugSelector(selector string) *DivElement {
	d.debugSelector = selector
	return d
}

// OnClick binds a click listener to this element (div.rs:1814
// StatefulInteractiveElement::on_click, Interactivity::on_click at
// div.rs:674). A left mouse press and release over the element fires a
// ClickEvent of kind Mouse (div.rs:3467-3626); Enter/Space pressed and
// released while the element's tracked focus handle is focused fires a
// Keyboard click (div.rs:3512-3577). The pin restricts on_click to
// stateful elements: the element needs an ID (an ID-less div keeps no
// interactive state, so its click listeners never fire).
func (d *DivElement) OnClick(f func(*ClickEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnClick requires a non-nil listener")
	}
	d.clickListeners = append(d.clickListeners, f)
	return d
}

// OnAction binds an action listener to this element (the reference
// OnAction; capture/bubble routing arrives with the focus ticket).
func (d *DivElement) OnAction[A any](a Action[A], f func(*A, *Window, *App)) *DivElement {
	return d.appendActionListener(false, a, f)
}

// CaptureAction binds a capture-phase action listener.
func (d *DivElement) CaptureAction[A any](a Action[A], f func(*A, *Window, *App)) *DivElement {
	return d.appendActionListener(true, a, f)
}

// appendActionListener stores one action registration.
func (d *DivElement) appendActionListener(capture bool, a any, handler any) *DivElement {
	if a == nil || isNilReflect(a) {
		panic("gpui: action listeners require a non-nil action descriptor")
	}
	if handler == nil {
		panic("gpui: action listeners require a non-nil handler")
	}
	d.actionListeners = append(d.actionListeners, typedActionListener{capture: capture, action: a, handler: handler})
	return d
}

// OnBoxedAction binds a boxed-action listener (the pinned
// OnBoxedAction: routing by payload type with a cloned instance at
// delivery — the listener receives the binding's captured action,
// cloned per dispatch, like on_boxed_action's captured clone).
func (d *DivElement) OnBoxedAction(a BoxedAction, f func(BoxedAction, *Window, *App)) *DivElement {
	return d.appendActionListener(false, a, f)
}

// OnKeyDown binds a key-down listener to the bubble phase
// (InteractiveElement::on_key_down). The listener is stored and routed
// by the dispatch tree during prepaint.
func (d *DivElement) OnKeyDown(f func(*KeyDownEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnKeyDown requires a non-nil listener")
	}
	d.keyDownListeners = append(d.keyDownListeners, typedKeyListener{f: f})
	return d
}

// CaptureKeyDown binds a key-down listener to the capture phase
// (InteractiveElement::capture_key_down).
func (d *DivElement) CaptureKeyDown(f func(*KeyDownEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: CaptureKeyDown requires a non-nil listener")
	}
	d.keyDownListeners = append(d.keyDownListeners, typedKeyListener{capture: true, f: f})
	return d
}

// OnKeyUp binds a key-up listener to the bubble phase
// (InteractiveElement::on_key_up).
func (d *DivElement) OnKeyUp(f func(*KeyUpEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnKeyUp requires a non-nil listener")
	}
	d.keyUpListeners = append(d.keyUpListeners, typedKeyListener{f: f})
	return d
}

// CaptureKeyUp binds a key-up listener to the capture phase
// (InteractiveElement::capture_key_up).
func (d *DivElement) CaptureKeyUp(f func(*KeyUpEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: CaptureKeyUp requires a non-nil listener")
	}
	d.keyUpListeners = append(d.keyUpListeners, typedKeyListener{capture: true, f: f})
	return d
}

// OnModifiersChanged binds a modifiers-changed listener
// (InteractiveElement::on_modifiers_changed).
func (d *DivElement) OnModifiersChanged(f func(*ModifiersChangedEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnModifiersChanged requires a non-nil listener")
	}
	d.modifiersChangedListeners = append(d.modifiersChangedListeners, f)
	return d
}

// ---------------------------------------------------------------------------
// Mouse, scroll and drop listeners (div.rs paint_mouse_listeners,
// ticket24)
// ---------------------------------------------------------------------------

// typedMouseListener is one mouse-family listener registration: the
// plain listener shape (func(*E, DispatchPhase, *Hitbox, *Window,
// *App)) with its phase (capture or bubble; the reference's mouse
// listeners all take the hitbox and phase).
type typedMouseListener[E any] struct {
	// capture routes the listener on the capture phase (bubble
	// default).
	capture bool
	// f is the listener.
	f func(*E, DispatchPhase, *Hitbox, *Window, *App)
}

// typedDropListener is one type-keyed drop listener (div.rs
// (TypeId, DropListener<dyn Any>)): the payload type plus the
// untyped can-drop predicate, drag-over style and on-drop callback,
// each optional (dragdrop.go DropListener).
type typedDropListener struct {
	// payloadType is the drag value type the listener receives
	// (reflect.TypeOf of the payload type).
	payloadType reflect.Type
	// canDrop, when set, decides whether a drag of the matching type
	// may drop here (replacing the default hovered gate).
	canDrop func(value any, hitbox *Hitbox, w *Window, app *App) bool
	// onDragOver, when set, refines the element's style while a drag
	// of the matching type that can drop hovers it.
	onDragOver func(value any, w *Window, app *App) StyleRefinement
	// onDrop receives the drag value and the mouse-up event when a
	// drag of the matching type lands on this element.
	onDrop func(value any, event *MouseUpEvent, w *Window, app *App)
}

// typedGroupStyle is a group style keyed by the drag payload type
// (div.rs (TypeId, GroupStyle)).
type typedGroupStyle struct {
	payloadType reflect.Type
	GroupStyle
}

// OnMouseMove binds a listener to the mouse moves over this element, in
// the bubble phase (div.rs:342-352 Interactivity::on_mouse_move, fluent
// div.rs:1155-1161). OnMouseMoveAll receives every move with the phase and
// hitbox.
func (d *DivElement) OnMouseMove(f func(*MouseMoveEvent, *Window, *App)) *DivElement {
	requireListener("OnMouseMove", f == nil)
	d.mouseMoveListeners = append(d.mouseMoveListeners, typedMouseListener[MouseMoveEvent]{f: func(e *MouseMoveEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
		if phase == DispatchBubble && hitbox.IsHovered(w) {
			f(e, w, app)
		}
	}})
	return d
}

// OnMouseUp binds a listener to the mouse up of the given button over
// this element, in the bubble phase (div.rs:223-236
// Interactivity::on_mouse_up, fluent div.rs:1051-1058). OnMouseUpAll
// receives every mouse up with the phase and hitbox.
func (d *DivElement) OnMouseUp(button MouseButton, f func(*MouseUpEvent, *Window, *App)) *DivElement {
	requireListener("OnMouseUp", f == nil)
	return d.appendMouseUp(func(e *MouseUpEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
		if phase == DispatchBubble && e.Button == button && hitbox.IsHovered(w) {
			f(e, w, app)
		}
	})
}

// OnScrollWheel binds a scroll listener
// (InteractiveElement::on_scroll_wheel). Precision-touchpad pans
// arrive here as pixel-delta ScrollWheelEvents with gesture phases.
func (d *DivElement) OnScrollWheel(f func(*ScrollWheelEvent, DispatchPhase, *Hitbox, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnScrollWheel requires a non-nil listener")
	}
	d.scrollWheelListeners = append(d.scrollWheelListeners, typedMouseListener[ScrollWheelEvent]{f: f})
	return d
}

// OnPinch binds a pinch listener (InteractiveElement::on_pinch).
func (d *DivElement) OnPinch(f func(*PinchEvent, DispatchPhase, *Hitbox, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnPinch requires a non-nil listener")
	}
	d.pinchListeners = append(d.pinchListeners, typedMouseListener[PinchEvent]{f: f})
	return d
}

// OnDrop binds a drop listener for payloads of type T, whether or
// not the drag started on this element (Interactivity::on_drop): on
// a mouse up with an active drag whose value has type T and this
// element's hitbox hovered, the drag stops and the listener receives
// the value (div.rs paint_mouse_listeners's drop arm). A platform
// file drag delivers an ExternalPaths value here.
func OnDrop[T any](d *DivElement, f func(*T, *MouseUpEvent, *Window, *App)) *DivElement {
	if f == nil {
		panic("gpui: OnDrop requires a non-nil listener")
	}
	payloadType := reflect.TypeOf((*T)(nil)).Elem()
	// A pre-existing listener for the same type is replaced, matching
	// the pin's get_mut_drop_listener match.
	for i, existing := range d.dropListeners {
		if existing.payloadType == payloadType {
			d.dropListeners[i].onDrop = dropListenerInvoke[T](f)
			return d
		}
	}
	d.dropListeners = append(d.dropListeners, typedDropListener{
		payloadType: payloadType,
		onDrop:      dropListenerInvoke[T](f),
	})
	return d
}

// dropListenerInvoke adapts the typed listener to the untyped drop
// delivery: the drag value (an ExternalPaths for a platform file drag)
// is delivered as a *T, matching the pin's downcast_ref.
func dropListenerInvoke[T any](f func(*T, *MouseUpEvent, *Window, *App)) func(any, *MouseUpEvent, *Window, *App) {
	return func(value any, event *MouseUpEvent, w *Window, app *App) {
		if typed, ok := value.(T); ok {
			f(&typed, event, w, app)
		}
	}
}

// Occlude sets the hitbox behavior to BlockMouse
// (InteractiveElement::occlude): hitboxes behind this element's
// hitbox stop reporting hover and scroll eligibility.
func (d *DivElement) Occlude() *DivElement {
	d.hitboxBehavior = HitboxBlockMouse
	return d
}

// WindowControlArea marks the div's bounds as a control area of the
// platform window (InteractiveElement::window_control_area, div.rs:1443):
// the div gets a hitbox, and while that hitbox is in the hit test under
// the cursor the Windows host answers WM_NCHITTEST with the area's code
// (Drag → HTCAPTION, Min → HTMINBUTTON, Max → HTMAXBUTTON, Close →
// HTCLOSE). Use it to draw a custom titlebar on a window opened with
// Titlebar: &TitlebarOptions{AppearsTransparent: true}. When areas nest
// (caption buttons inside a drag strip), the outer one wins unless the
// inner element also calls Occlude, as Zed's caption buttons do.
func (d *DivElement) WindowControlArea(area WindowControlArea) *DivElement {
	a := area
	d.windowControl = &a
	return d
}

// TabIndex sets the div's tab index and makes it a tab stop
// (InteractiveElement::tab_index): the index participates in tab-group
// ordering; a tracked focus handle's own TabIndex/TabStop control its
// focusability.
func (d *DivElement) TabIndex(index int64) *DivElement {
	i := index
	d.tabIndex = &i
	d.tabStop = true
	return d
}

// TabGroup designates this div as a tab group
// (InteractiveElement::tab_group): children's tab order restarts inside
// the group's position.
func (d *DivElement) TabGroup() *DivElement {
	d.tabGroup = true
	if d.tabIndex == nil {
		i := int64(0)
		d.tabIndex = &i
	}
	return d
}

// Child adds one child element, converting it with the selected
// precedence and panicking on invalid input (DivElement::child).
func (d *DivElement) Child(value any) *DivElement {
	d.appendChildren("DivElement.Child", 1, func() []AnyElement {
		return []AnyElement{intoChild("DivElement", 0, value)}
	})
	return d
}

// Children adds multiple child elements (DivElement::children).
func (d *DivElement) Children(values ...any) *DivElement {
	if len(values) == 0 {
		return d
	}
	d.appendChildren("DivElement.Children", len(values), func() []AnyElement {
		out := make([]AnyElement, 0, len(values))
		for i, value := range values {
			out = append(out, intoChild("DivElement", i, value))
		}
		return out
	})
	return d
}

// TryChildren adds multiple child elements with the checked conversion:
// every candidate is converted before any attachment, a failure
// preserves the destination's child list, and reentrant child mutation
// of the destination is rejected (the selected contract).
func (d *DivElement) TryChildren(values ...any) (*DivElement, error) {
	if len(values) == 0 {
		return d, nil
	}
	var converted []AnyElement
	for i, value := range values {
		elem, err := TryIntoElement(value)
		if err != nil {
			return d, fmt.Errorf("gpui: DivElement child %d: %w", i, err)
		}
		converted = append(converted, elem)
	}
	d.commitChildren("DivElement.TryChildren", len(values), converted)
	return d, nil
}

// TryChildrenSlice validates and attaches a typed slice of child
// candidates.
func (d *DivElement) TryChildrenSlice[T any](values []T) (*DivElement, error) {
	if len(values) == 0 {
		return d, nil
	}
	converted := make([]AnyElement, 0, len(values))
	for i, value := range values {
		elem, err := TryIntoElement(value)
		if err != nil {
			return d, fmt.Errorf("gpui: DivElement child %d: %w", i, err)
		}
		converted = append(converted, elem)
	}
	d.commitChildren("DivElement.TryChildrenSlice", len(values), converted)
	return d, nil
}

// appendChildren runs one parent child-mutation operation: the
// conversion gate, the conversion, the token validation/reservation,
// then the commit without further application callbacks (the selected
// contract).
func (d *DivElement) appendChildren(parent string, count int, convert func() []AnyElement) {
	if d.converting {
		panic(fmt.Sprintf("gpui: reentrant child mutation of %s during child conversion", parent))
	}
	d.converting = true
	var converted []AnyElement
	func() {
		defer func() {
			if r := recover(); r != nil {
				d.converting = false
				panic(r)
			}
		}()
		converted = convert()
	}()
	d.converting = false
	reserveAndCommitRecipes(converted)
	d.children = append(d.children, converted...)
}

// commitChildren attaches pre-converted children under the conversion
// gate with the same token validation and reservation.
func (d *DivElement) commitChildren(parent string, count int, converted []AnyElement) {
	if d.converting {
		panic(fmt.Sprintf("gpui: reentrant child mutation of %s during child conversion", parent))
	}
	d.converting = true
	defer func() { d.converting = false }()
	reserveAndCommitRecipes(converted)
	d.children = append(d.children, converted...)
}

// divElement is the ELEMENT side of the div: the adapter implementing
// the typed Element phases over the builder (the reference Div implements
// both the builder traits and Element; Go cannot overload ID by arity, so
// the fluent ID(value string) stays on DivElement and the phase
// ID() (ElementID, bool) lives here).
type divElement struct {
	builder *DivElement
}

// ID implements Element.
func (e *divElement) ID() (ElementID, bool) {
	if e.builder.elementID == nil {
		return ElementID{}, false
	}
	return *e.builder.elementID, true
}

// SourceLocation implements Element.
func (e *divElement) SourceLocation() *SourceLocation { return nil }

// A11yRole implements the accessibility role capability: the div carries
// no role (the reference filters GenericContainer).
func (e *divElement) A11yRole() (Role, bool) { return e.builder.A11yRole() }

// RequestLayout implements Element (div.rs Div::request_layout +
// Interactivity::request_layout, bounded).
func (e *divElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, divFrameState) {
	return e.builder.requestLayout(global, inspector, w, app)
}

// Prepaint implements Element.
func (e *divElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, w *Window, app *App) divPrepaintState {
	return e.builder.prepaint(global, inspector, bounds, layout, w, app)
}

// Paint implements Element.
func (e *divElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, prepaint *divPrepaintState, w *Window, app *App) {
	e.builder.paint(global, inspector, bounds, layout, prepaint, w, app)
}

// LayoutNodeStyle implements the root-stretch report (the div's
// computed style of its last request).
func (e *divElement) LayoutNodeStyle() (Style, bool) {
	return e.builder.lastStyle, true
}

// PaintsAfterInFlow reports the sticky paint-order rule (browser-core
// ticket 34): a sticky element paints — and inserts its hitbox — after
// its in-flow non-positioned siblings (css2.1 Appendix E step 8: a
// positioned element, which sticky is, paints above in-flow content,
// so a pinned header rides over the content scrolling under it). The
// parent's child walk consults this through paintsAfterInFlow
// (sticky.go).
func (e *divElement) PaintsAfterInFlow() bool {
	return e.builder.lastStyle.Position == PositionSticky
}

// ChildCount reports the destination's committed child count (the
// test-support observable of the checked child-insertion contract: a
// failed batch preserves it).
func (d *DivElement) ChildCount() int { return len(d.children) }

// IntoElement implements IntoElement: the div builder converts into its
// element (the reference IntoElement for Div wraps the builder).
func (d *DivElement) IntoElement() AnyElement {
	// An a11y-authored div joins the semantic tree through its companion
	// (div.rs:2261-2269, 4065-4162; Base ticket 11).
	if d.hasA11yAuthoring() {
		return divIntoA11yElement(d)
	}
	return CustomElement[divFrameState, divPrepaintState](&divElement{builder: d})
}

// divPrepaintState is the div's prepaint state (div.rs:2362 the
// reference Option<Hitbox>): the hitbox inserted for this frame, when
// the element needs one (div.rs:2887-2911 should_insert_hitbox), plus
// whether the frame recorded the debug-selector bounds.
type divPrepaintState struct {
	// hitbox is this frame's hitbox, or nil.
	hitbox *Hitbox
	// hitboxRecord is this frame's hitbox record (browser-core ticket
	// 11): the children prepaint inside withHitboxSubtree on it, so their
	// hitboxes record it as their parent (the capture chain's ancestry).
	hitboxRecord *hitboxRecord
	// debugBounds records whether the debug selector was recorded.
	debugBounds bool
	// stickyOffset is the sticky positioning offset this frame's
	// prepaint shifted the div by (browser-core ticket 34, sticky.go):
	// paint re-applies it to the bounds and the children's element
	// offset, exactly like scrollOffset below — the port's drawables
	// re-resolve their bounds at paint.
	stickyOffset Point
	// scrollOffset is the clamped scroll offset the children prepainted
	// under (div.rs:2876-2878, 2460); paint re-applies it because the
	// port's drawables re-resolve their bounds at paint.
	scrollOffset Point
	// autoScrollbar is this frame's shown overflow: auto scrollbar pair
	// (browser-core 36, scrollbar.go): the paint geometry computed at
	// prepaint from the clamp's results. Nil while no scrollbar shows.
	autoScrollbar *autoScrollbarFacts
}

// computeStyle returns the div's base style (Style::default() refined by
// the base style, the first step of div.rs:3904 compute_style_internal).
func (d *DivElement) computeStyle() Style {
	style := DefaultStyle()
	style.refineBoxed(d.Styled.storage(), &d.styleBoxes)
	return style
}

// interactiveStateOf resolves the element's retained interactive state
// inside WithOptionalElementState (the pinned
// `element_state.map(|s| s.unwrap_or_default())`): an element with an id
// gets its retained state or a fresh default; an id-less element has
// none.
func interactiveStateOf(global *GlobalElementID, state *InteractiveElementState) *InteractiveElementState {
	if global == nil {
		return nil
	}
	if state == nil {
		return &InteractiveElementState{}
	}
	return state
}

// RequestLayout implements Element (div.rs:2288-2351 Div::request_layout
// + div.rs:2672-2763 Interactivity::request_layout, bounded): inside the
// element's interactive state, a drag in progress clears the pending
// mouse down and the pressed state (div.rs:2703-2712), the style is
// computed with the state styles (compute_style_internal without a
// hitbox, div.rs:2757-2758), then the text style is scoped, the children
// are requested and this element's layout node is requested with them —
// a Block/Inline div routes through the inline frame state (paragraph
// collection) — and the node's container inline content is published
// (the reference publishes Container for every div).
func (d *DivElement) requestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, divFrameState) {
	// The scroll offset and gesture cells (div.rs:2735-2755).
	d.scroll = resolveElementScroll(d.trackedScrollHandle, d.Styled.storage(), global, w)
	style := WithOptionalElementState(w, global, func(slot *InteractiveElementState, w *Window) (Style, *InteractiveElementState) {
		es := interactiveStateOf(global, slot)
		if es != nil && app.HasActiveDrag() {
			if es.pendingMouseDown != nil {
				es.pendingMouseDown.event = nil
			}
			if es.clickedState != nil {
				*es.clickedState = ElementClickedState{}
			}
		}
		return resolveStyleTransitions(global, d.computeStyleInternal(nil, es, w, app), w, app), es
	})
	d.lastStyle = style
	state := divFrameState{}
	var layoutID LayoutID
	WithTextStyleVoid(w, style.TextStyle(), func(w *Window) {
		// browser-core 36 (spec story 12): display: contents — the element
		// generates no box. Its children splice into the enclosing node
		// request (contents.go), its box styles are inert (no containing-
		// block scope, no sticky flag, no scroll state: a contents element
		// is not a scroll container), and the text style above still
		// scopes the children (inherited properties pass through).
		if style.Display == DisplayContents {
			childLayoutIDs := make([]LayoutID, 0, len(d.children))
			for i := range d.children {
				childLayoutIDs = append(childLayoutIDs, d.children[i].RequestLayout(w, app))
			}
			state.childLayoutIDs = childLayoutIDs
			state.contents = true
			layoutID = requestContentsLayout(w, &state, app)
			state.layoutID = layoutID
			return
		}
		// browser-core 33 (spec D3): open this element's containing-block
		// scope around the child walk and the node request — a positioned
		// div absorbs the absolutes deferred by static descendants below
		// it, and a static div defers its own absolute children into the
		// enclosing scope instead of attaching them to itself
		// (containingblock.go).
		scope := openContainingBlock(w, &style)
		defer closeContainingBlock(w, scope)
		// browser-core 34 (spec L3): flag the frame as sticky-carrying so
		// the prepaints fill the flow-box registry (sticky.go); the cost
		// for non-sticky views is this one check per div.
		if style.Position == PositionSticky {
			currentFrame(w).stickyRequested = true
		}
		childLayoutIDs := make([]LayoutID, 0, len(d.children))
		for i := range d.children {
			childLayoutIDs = append(childLayoutIDs, d.children[i].RequestLayout(w, app))
		}
		// browser-core 36: expand display:contents splices before the two
		// non-attaching consumers of this list (the inline paragraph
		// collector below and the published inline content); the attach
		// itself expands again (requestLayoutNode, idempotently).
		childLayoutIDs = expandContentsChildren(w, childLayoutIDs)
		state.childLayoutIDs = childLayoutIDs
		// browser-core 36 (spec story 14): the overflow: auto axes fold
		// onto the record values their reservation needs — a shown (or
		// gutter-stable) axis becomes Scroll WITH the scrollbar width, so
		// Taffy reserves the gutter, and an unshown axis becomes Hidden
		// (scrollbar.go autoOverflowRecordStyle). The flags come from the
		// element state (what the previous frame's prepaint saw
		// overflowing); the first frame settles one draw later, like
		// Chromium's post-layout relayout.
		nodeStyle := style
		if d.scroll != nil && d.scroll.auto != nil {
			if adjusted, ok := autoOverflowRecordStyle(&style, d.scroll.auto, RemSize(w)); ok {
				nodeStyle = adjusted
			}
		}
		if style.Display == DisplayBlock || style.Display == DisplayInline {
			id, inlineState := requestInlineDivFrame(w, nodeStyle, scope, childLayoutIDs, app)
			state.inline = inlineState
			layoutID = id
		} else {
			id, err := requestLayoutNode(w, scope, nodeStyle, childLayoutIDs)
			if err != nil {
				panic(fmt.Sprintf("gpui: DivElement request_layout: %v", err))
			}
			layoutID = id
		}
		// browser-core 34: the prepaint resolves this div's sticky
		// containing block (its native layout parent) from its own node.
		state.layoutID = layoutID
		if len(childLayoutIDs) == 0 {
			// Leaf divs share one read-only empty record (gpui-perf
			// ticket 06: no allocation per leaf).
			publishInlineContent(w, layoutID, emptyInlineContainer)
		} else {
			publishInlineContent(w, layoutID, &inlineContainerContent{children: childLayoutIDs})
		}
	})
	return layoutID, state
}

// prepaintContents is prepaint's display:contents arm (browser-core
// 36): no box, so no sticky shift, hitbox, scroll clamp, content mask
// or debug bounds — only the text style (inherited properties pass
// through css-display-3 §3.1) and the dispatch node, so key listeners
// and actions keep their place between the children and the parent in
// the bubbling chain (a DOM element with no box). The children
// prepaint in order inside whatever scopes the parent opened around
// this element's walk (its content mask, its hitbox subtree, its
// scroll offset): the spliced children behave exactly like the
// parent's direct children.
func (d *DivElement) prepaintContents(layout *divFrameState, state *divPrepaintState, w *Window, app *App) {
	WithTextStyleVoid(w, d.lastStyle.TextStyle(), func(w *Window) {
		tree := currentDispatchTree(w)
		tree.PushNode()
		defer tree.PopNode()
		registerDivElementKeyDispatch(tree, d)
		for _, child := range stickyChildrenLast(d.children) {
			child.Prepaint(w, app)
		}
	})
}

// paintContents is paint's display:contents arm (browser-core 36): the
// element paints nothing of its own (no box), its children paint in
// order (sticky children last, like the in-flow path) under the text
// style, inside whatever scopes the parent opened around this walk.
func (d *DivElement) paintContents(w *Window, app *App) {
	WithTextStyleVoid(w, d.lastStyle.TextStyle(), func(w *Window) {
		for _, child := range stickyChildrenLast(d.children) {
			child.Paint(w, app)
		}
	})
}

// Prepaint implements Element (div.rs:2354-2482 Div::prepaint +
// div.rs:2766-2885 Interactivity::prepaint, bounded): a standalone
// inline div first places its paragraphs' boxes and span fragments
// (prepare_layout, outside the style/mask scopes — the reference
// computes the content size before the interactivity prepaint); then,
// inside the element's interactive state, the style is computed, the
// hover state is initialized for hover styles (div.rs:2846-2850), and
// inside the text style and overflow content mask the hitbox is
// inserted when needed (div.rs:2865-2874), the dispatch state is
// registered and the children are prepainted (through the inline frame
// state when it owns them). The div's dispatch node is popped and its
// tab group closed after the children (the pinned Drawable::prepaint
// pop_node, element.rs:505, and the with_tab_group scope, div.rs:3036).
//
// The port registers the dispatch-tree state (key listeners, actions,
// tab stops, the keyboard click) during prepaint so the completed frame
// carries the tree (key_dispatch.go's header); the pin registers them in
// paint after its Visibility::Hidden early return (div.rs:3021-3023), so
// a hidden div — and everything inside it, whose paint never runs —
// registers none of them here either (interact.go hiddenPrepaintDepth).
// The mouse listeners and cursor requests register in paint, like the
// pin (div.rs:3048-3082).
func (d *DivElement) prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, w *Window, app *App) divPrepaintState {
	state := divPrepaintState{}
	// browser-core 36: a display:contents div has no box — no sticky
	// shift, no hitbox, no scroll, no mask. It opens only the text style
	// (inherited properties pass through) and the dispatch node (key
	// listeners and actions keep their place in the bubbling chain, like
	// a DOM element without a box), then prepaints its children in order
	// under whatever scopes the parent opened around this walk.
	if layout.contents {
		d.prepaintContents(layout, &state, w, app)
		return state
	}
	// Contents participating in a parent paragraph are detected here:
	// the drawable scoped this element's placed fragments around this
	// call (the reference commits contents_in_parent_paragraph at
	// prepaint from window.current_inline_fragments).
	layout.contentsInParentParagraph = currentFrame(w).currentInlineFragments != nil
	// browser-core 34 (spec L3): a standalone sticky div shifts at
	// prepaint — laid out in flow (the record zeroes its insets) and
	// moved so it stays inside its scroll container's inset-adjusted
	// scrollport, never leaving its flow containing block (sticky.go,
	// Chromium's constraint math). The shift moves the element's own
	// bounds — hitbox, debug bounds, anchors, the painted box — and
	// opens as the children's element offset below, so everything
	// resolves consistently; a scroll changes only the offset cell and
	// this recomputation, never the layout. A div participating in a
	// parent paragraph does not stick (a documented limitation:
	// sticky.go's header).
	stickyOffset := Point{}
	if d.lastStyle.Position == PositionSticky && !layout.contentsInParentParagraph {
		stickyOffset = stickyDivOffset(w, &d.lastStyle, bounds, layout.layoutID)
	}
	state.stickyOffset = stickyOffset
	if stickyOffset.X != 0 || stickyOffset.Y != 0 {
		bounds = Bounds{
			Origin: Point{X: bounds.Origin.X + stickyOffset.X, Y: bounds.Origin.Y + stickyOffset.Y},
			Size:   bounds.Size,
		}
	}
	withElementOffsetVoid(w, stickyOffset, func(w *Window) {
		d.prepaintInFlow(global, inspector, bounds, layout, &state, w, app)
	})
	return state
}

// prepaintInFlow is prepaint's body under the sticky shift (browser-
// core 34): the content-size and inline placement phase, the debug and
// anchor records, and the interactivity prepaint — the pinned
// div.rs:2354-2482 Div::prepaint + div.rs:2766-2885
// Interactivity::prepaint, with the frame's sticky scroller scope and
// flow-box record opened around the child walk (sticky.go).
func (d *DivElement) prepaintInFlow(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, state *divPrepaintState, w *Window, app *App) {
	// The scroll facts the clamp, the sticky scope and the auto
	// scrollbar resolve against (browser-core 07): the div's own node's
	// record-derived content size (the descendants' boxes, their visible
	// overflow and the END padding), scroll range (the padding-box basis
	// plus the in-flow children's trailing margins, minus the scrollbar
	// reservation) and resolved padding (percentages against the
	// CONTAINING BLOCK's width, exactly what Taffy laid out) — replacing
	// the pin's element-tree union, which measured only the direct
	// children's border boxes and resolved vertical padding percentages
	// against the scroller's own height. The inline path keeps its own
	// content extent (the paragraph geometry the inline collector
	// places, which the layout record does not carry) with the record's
	// padding; the flow path takes the record's facts wholesale. The
	// tracked handle's child bounds (div.rs:2383-2413) ride along.
	var contentSize Size
	var scrollMax Point
	var scrollerPadding Edges
	needsFacts := d.scroll != nil || d.trackedScrollHandle != nil ||
		(currentFrame(w).stickyRequested && styleIsScrollContainer(&d.lastStyle))
	if standalone := layout.standaloneInline(); standalone != nil {
		contentSize = standalone.prepareLayout(bounds, layout.childLayoutIDs, w)
		d.trackInlineChildBounds(layout.childLayoutIDs, w)
		if needsFacts {
			scrollerPadding = d.scrollLayoutFactsOf(layout, w).Padding
			scrollMax = inlineScrollMaxOf(contentSize, scrollerPadding, bounds)
		}
	} else if needsFacts {
		// browser-core 36: a hidden scroller with no scroll state still
		// measures its content while the frame requested a sticky element
		// — its scrollable extent is the sticky containing block of its
		// direct children (the scroller arm below).
		facts := d.scrollLayoutFactsOf(layout, w)
		contentSize = facts.ContentSize
		scrollMax = facts.MaxOffset
		scrollerPadding = facts.Padding
		d.trackChildBounds(layout.childLayoutIDs, w)
	}
	if d.trackedScrollHandle != nil {
		d.trackedScrollHandle.scrollToActiveItem()
	}
	if d.debugSelector != "" {
		recordDebugBounds(w, d.debugSelector, bounds)
		state.debugBounds = true
	}
	// Ticket 25: a named element records its prepaint bounds (and the
	// content mask it prepaints under — its clipping scroller's visible
	// rectangle) into the frame's anchor registry for Anchored().
	// AnchorTo lookups. The registry fills only while this window's
	// Anchored elements ask for it (requestAnchorTracking at request
	// layout), so plain frames pay nothing. A later same-named element
	// wins, matching prepaint order.
	if d.elementID != nil && d.elementID.Kind == ElementIDName && windowTracksAnchors(w) {
		recordAnchorBounds(w, d.elementID.Name, bounds, ContentMask(w))
	}
	if global == nil && d.requiresElementState() {
		panic("gpui: OnClick, OnAuxClick, OnDrag, OnHover, Active and GroupActive require an element ID (the pinned StatefulInteractiveElement methods, div.rs:1495-1899)")
	}
	if global == nil && d.scrollStateful {
		panic("gpui: OverflowScroll, OverflowXScroll, OverflowYScroll, OverflowAuto, OverflowXAuto, OverflowYAuto, RestrictScrollToAxis and TrackScroll require an element ID (the pinned StatefulInteractiveElement methods, div.rs:1746-1777)")
	}
	WithOptionalElementState(w, global, func(slot *InteractiveElementState, w *Window) (struct{}, *InteractiveElementState) {
		es := interactiveStateOf(global, slot)
		style := resolveStyleTransitions(global, d.computeStyleInternal(nil, es, w, app), w, app)
		if es != nil && (d.hoverStyle != nil || d.groupHoverStyle != nil) && es.hoverState == nil {
			es.hoverState = &ElementHoverState{}
		}
		hidden := style.Visibility == VisibilityHidden
		suppressed := hidden || hiddenPrepaintDepth(w) > 0
		WithTextStyleVoid(w, style.TextStyle(), func(w *Window) {
			WithContentMaskVoid(w, style.OverflowMask(bounds, RemSize(w)), func(w *Window) {
				// should_insert_hitbox's scroll_offset.is_some() arm
				// (div.rs:2892) joins interact.go's predicate here.
				if d.shouldInsertHitbox(&style, w) || d.scroll != nil {
					state.hitboxRecord = w.insertHitboxRecord(bounds, d.hitboxBehavior)
					box := state.hitboxRecord.box
					state.hitbox = &box
					// insert_inspector_hitbox (div.rs:3100-3111, the
					// ticket27 integration): while the window's inspector
					// is picking, register this hitbox as selectable for
					// its element's inspector id.
					w.InsertInspectorHitbox(box, inspector)
				}
				// clamp_scroll_position (div.rs:2876-2877), on the
				// record-derived range (browser-core 07).
				scrollOffset := d.scroll.clamp(bounds, &style, scrollMax, w)
				state.scrollOffset = scrollOffset
				// browser-core 36 (spec story 14): the auto-scrollbar
				// detection — Chromium's ComputeScrollbarExistence kAuto
				// arm (an axis shows its scrollbar exactly while it
				// overflows) with UpdateAfterOverflowRecalc's relayout as a
				// notify: the flags persist in the element state, the NEXT
				// frame's request walk reserves from them (the one-frame
				// settle, scrollbar.go), and the shown pair's paint
				// geometry is resolved here for the paint below.
				if d.scroll != nil && d.scroll.auto != nil {
					if d.scroll.auto.update(d.scroll.maxOffset) {
						currentViewNotifier(w)(w, app)
						drawState(w).autoScrollbarAppearanceChanged = true
					}
					state.autoScrollbar = autoScrollbarPaint(&style, bounds, RemSize(w),
						d.scroll.maxOffset, scrollOffset, contentSize, d.scroll.auto)
					recordAutoScrollbarFacts(w, state.autoScrollbar)
				}
				// browser-core 34 (spec L3) + 36: open this scroller's sticky
				// scope around the child walk (sticky.go) — sticky
				// descendants resolve their constraints against the nearest
				// scroll container, this one while its children prepaint.
				// Per CSS (ComputedStyle::IsScrollContainer: every overflow
				// value but visible and clip), an overflow: hidden or auto box
				// is a scroll container too — ticket 34's recorded
				// limitation, closed here (Chromium:
				// PaintLayer::ContainingScrollContainerLayer walks for
				// IsScrollContainer, paint_layer.cc:275-292, and the
				// constraint axes follow ScrollableAxes,
				// paint_layer_scrollable_area.cc:2520-2542). A scroller without
				// scroll state (hidden) contributes a zero offset and its
				// content extent as the scroll range. The scroller records its
				// flow rectangle (the scrollable extent) for its sticky direct
				// children; every other div records its content box when the
				// frame requested a sticky element.
				if d.scroll != nil || (currentFrame(w).stickyRequested && styleIsScrollContainer(&style)) {
					maxOffset, scrollerOffset := Point{}, Point{}
					if d.scroll != nil {
						maxOffset, scrollerOffset = d.scroll.maxOffset, scrollOffset
					} else {
						// A hidden scroller never scrolls, but its sticky
						// children's containing block is still its scrollable
						// extent — the record-derived range (browser-core 07:
						// the content size, the end margins and the padding
						// box, not the old style-resolved padding arithmetic).
						maxOffset = scrollMax
					}
					ctx := stickyScrollContextOf(w, &style, bounds, scrollerPadding, maxOffset, scrollerOffset, layout.layoutID.node)
					frame := currentFrame(w)
					frame.stickyScrollers = append(frame.stickyScrollers, ctx)
					defer func() {
						frame.stickyScrollers = frame.stickyScrollers[:len(frame.stickyScrollers)-1]
					}()
					recordStickyFlowBox(w, layout.layoutID.node, ctx.selfCB)
				} else if currentFrame(w).stickyRequested {
					recordStickyFlowBox(w, layout.layoutID.node, style.ContentBox(bounds, RemSize(w)))
				}
				tree := currentDispatchTree(w)
				endGroup := registerDivElementDispatch(w, d, suppressed, state.hitbox, es)
				defer func() {
					if endGroup {
						currentTabStops(w).EndGroup()
					}
					tree.PopNode()
				}()
				if style.Display == DisplayNone {
					// skip children (div.rs:2428-2430)
					return
				}
				// browser-core 09: the scroller registers on its dispatch node
				// (still active here) so the keyboard-scroll default action can
				// find it from the focused node's ancestor path.
				if d.scroll != nil && !suppressed {
					registerKeyScrollContainer(w, d.scroll, &style)
				}
				withHiddenPrepaint(w, hidden, func() {
					// The children prepaint under the scroll offset
					// (div.rs:2460; inline.rs:454 for the inline path), inside
					// this element's hitbox subtree (browser-core ticket 11:
					// their hitboxes record this hitbox as their parent, the
					// capture chain's ancestry). Sticky children prepaint
					// after the in-flow ones (sticky.go's paint-order rule —
					// their hitboxes land on top, where they paint).
					withElementOffsetVoid(w, scrollOffset, func(w *Window) {
						w.withHitboxSubtree(state.hitboxRecord, func() {
							if standalone := layout.standaloneInline(); standalone != nil {
								standalone.prepaintChildren(stickyChildrenLast(d.children), w, app)
								return
							}
							for _, child := range stickyChildrenLast(d.children) {
								child.Prepaint(w, app)
							}
						})
					})
				})
			})
		})
		return struct{}{}, es
	})
}

// registerDivElementDispatch pushes the div's dispatch node and
// registers its dispatch-tree state on the frame under construction
// (the pinned Drawable::prepaint push_node, element.rs:468; the focus
// identity of Interactivity::prepaint's set_focus_handle, div.rs:2800;
// then Interactivity::paint's with_tab_group + tab_stops.insert,
// div.rs:3025-3047, the keyboard click of paint_mouse_listeners,
// div.rs:3512-3577, and paint_keyboard_listeners, div.rs:3766-3796, in
// that order). The port registers during prepaint so the completed
// frame carries the tree (key_dispatch.go's header). suppressed is the
// pin's Visibility::Hidden early return (div.rs:3021-3023) for this div
// or a hidden ancestor: only the node and its focus identity register.
// It reports whether a tab group was opened; the caller closes it and
// pops the node after the children.
func registerDivElementDispatch(w *Window, d *DivElement, suppressed bool, hitbox *Hitbox, es *InteractiveElementState) (endGroup bool) {
	tree := currentDispatchTree(w)
	tree.PushNode()
	if d.focusHandle != nil {
		tree.SetFocusID(d.focusHandle.id)
	}
	registerDivElementKeyDispatch(tree, d)
	if suppressed {
		return false
	}

	// with_tab_group (div.rs:3025-3036): the group opens before the
	// children (their paths carry the group index); the container's own
	// handle is inserted inside the group (div.rs:3037-3047).
	if d.tabGroup && d.tabIndex != nil {
		currentTabStops(w).BeginGroup(*d.tabIndex)
		endGroup = true
	}
	if d.focusHandle != nil {
		registerFocusStop(w, *d.focusHandle)
	}

	// The keyboard click (div.rs:3512-3577) registers before the
	// element's own key listeners, as paint_mouse_listeners runs before
	// paint_keyboard_listeners (div.rs:3075, 3084).
	if hitbox != nil {
		registerDivKeyboardClick(w, d, *hitbox, es)
	}

	return endGroup
}

// registerDivElementKeyDispatch registers a div's key context, key
// listeners and action listeners on its dispatch node — the pieces that
// do not need a box (browser-core 36's display:contents arm reuses
// them: a DOM element without a box still sits in the bubbling chain;
// the mouse/keyboard-click/focus registrations do need a box and stay
// in registerDivElementDispatch).
func registerDivElementKeyDispatch(tree *DispatchTree, d *DivElement) {
	if d.keyContext != nil {
		if context, err := ParseKeyContext(*d.keyContext); err == nil {
			tree.SetKeyContext(context)
		}
	}

	// Key listeners (phase-routed by the tree).
	for _, listener := range d.keyDownListeners {
		tree.OnKeyEvent(true, listener.capture, listener.f)
	}
	for _, listener := range d.keyUpListeners {
		tree.OnKeyEvent(false, listener.capture, listener.f)
	}
	for _, listener := range d.modifiersChangedListeners {
		tree.OnModifiersChanged(listener)
	}

	// Action listeners: resolve the canonical descriptor and wrap the
	// typed handler (the reference's downcast adapters; capture
	// listeners keep propagation in the bubble phase).
	for _, registration := range d.actionListeners {
		descriptor := anyActionDescriptor(registration.action)
		if descriptor == nil {
			continue
		}
		listener := registration
		tree.OnAction(descriptor, listener.capture, func(action any, phase DispatchPhase, w *Window, app *App) {
			invokeTypedActionListener(listener, action, phase, w, app)
		})
	}
}

// registerMouseListener wraps one typed mouse-family listener as a
// frame mouse listener gated on its event type (div.rs's per-kind
// registration: `window.on_mouse_event(move |event: &E, phase,
// window, cx| listener(event, phase, &hitbox, window, cx))`).
func registerMouseListener[E any](w *Window, listener typedMouseListener[E], box Hitbox) {
	hitbox := box
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		if e, ok := event.(*E); ok {
			listener.f(e, phase, &hitbox, w, app)
		}
	})
}

// invokeTypedActionListener adapts a div action registration to a
// dispatch callback: typed handlers receive the cloned payload; boxed
// handlers receive the binding's captured action, cloned per dispatch
// (on_boxed_action's captured clone); capture-only handlers re-enable
// propagation in the bubble phase (capture_action's cx.propagate()).
func invokeTypedActionListener(listener typedActionListener, action any, phase DispatchPhase, w *Window, app *App) {
	if boxed, isBoxed := listener.handler.(func(BoxedAction, *Window, *App)); isBoxed {
		if captured, ok := listener.action.(BoxedAction); ok && phase == DispatchBubble {
			boxed(captured.Clone(), w, app)
		}
		return
	}
	if listener.capture && phase != DispatchCapture {
		// capture_action: continue propagation when the bubble phase
		// reaches this registration.
		app.propagateEvent = true
		return
	}
	if !listener.capture && phase != DispatchBubble {
		return
	}
	handler := reflect.ValueOf(listener.handler)
	handlerType := handler.Type()
	if handlerType.NumIn() != 3 || handlerType.In(0).Kind() != reflect.Pointer {
		return
	}
	payload := reflect.New(handlerType.In(0).Elem())
	if action != nil {
		value := reflect.ValueOf(action)
		if value.Type().AssignableTo(payload.Elem().Type()) {
			payload.Elem().Set(value)
		} else {
			return
		}
	}
	handler.Call([]reflect.Value{payload, reflect.ValueOf(w), reflect.ValueOf(app)})
}

// Paint implements Element (div.rs:2485-2525 Div::paint + div.rs:2986-3125
// Interactivity::paint + style.rs:874-1015 Style::paint, bounded): inside
// the element's interactive state, compute the style with this frame's
// hitbox (hover, active, focus and group styles; div.rs:3003-3009) and
// register the group-hover refresh handler (div.rs:3019); a hidden
// element paints nothing more (div.rs:3021); otherwise scope the
// element opacity and paint the style's box around the text-style and
// overflow-mask scoped content: with a hitbox, the cursor request
// (div.rs:3054-3062), the group hitbox push (div.rs:3064-3066) and the
// mouse listeners (div.rs:3075-3080, interact.go); then the children
// (through the inline frame state — the children first, then the
// paragraphs — when it owns them; none when Display is None,
// div.rs:2510-2512) and the group pop (div.rs:3111-3113). The sticky
// shift from the prepaint re-applies here (browser-core 34, sticky.go)
// before the body runs in paintInFlow.
func (d *DivElement) paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, prepaint *divPrepaintState, w *Window, app *App) {
	ensurePaintHitTest(w)
	hitbox := prepaint.hitbox
	// browser-core 36: display:contents paints no box — the children
	// paint in order under the text style (paintContents), inside the
	// parent's scopes.
	if layout.contents {
		d.paintContents(w, app)
		return
	}
	// browser-core 34 (spec L3): re-apply the prepaint's sticky shift —
	// the port's drawables re-resolve their bounds at paint, so the
	// element's own box paints at the shifted bounds (the pin reuses
	// the prepainted bounds; the scroll offset below re-applies the
	// same way) and the children resolve under the shift, exactly as
	// they prepainted (sticky.go).
	if prepaint.stickyOffset.X != 0 || prepaint.stickyOffset.Y != 0 {
		bounds = Bounds{
			Origin: Point{X: bounds.Origin.X + prepaint.stickyOffset.X, Y: bounds.Origin.Y + prepaint.stickyOffset.Y},
			Size:   bounds.Size,
		}
	}
	withElementOffsetVoid(w, prepaint.stickyOffset, func(w *Window) {
		d.paintInFlow(global, inspector, bounds, layout, prepaint, hitbox, w, app)
	})
}

// paintInFlow is paint's body under the sticky shift (browser-core
// 34): the pinned div.rs:2485-2525 Div::paint + div.rs:2986-3125
// Interactivity::paint + style.rs:874-1015 Style::paint.
func (d *DivElement) paintInFlow(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, prepaint *divPrepaintState, hitbox *Hitbox, w *Window, app *App) {
	WithOptionalElementState(w, global, func(slot *InteractiveElementState, w *Window) (struct{}, *InteractiveElementState) {
		es := interactiveStateOf(global, slot)
		style := resolveStyleTransitions(global, d.computeStyleInternal(hitbox, es, w, app), w, app)
		d.paintHoverGroupHandler(w, app)
		if style.Visibility == VisibilityHidden {
			return struct{}{}, es
		}
		WithElementOpacityVoid(w, style.Opacity, func(w *Window) {
			style.paint(bounds, w, app, func(w *Window) {
				WithTextStyleVoid(w, style.TextStyle(), func(w *Window) {
					WithContentMaskVoid(w, style.OverflowMask(bounds, RemSize(w)), func(w *Window) {
						if hitbox != nil {
							if drag := app.ActiveDrag(); drag != nil {
								if drag.CursorStyle != nil {
									w.SetWindowCursorStyle(*drag.CursorStyle)
								}
							} else if style.MouseCursor != nil {
								w.SetCursorStyle(*style.MouseCursor, hitbox)
							}
							if d.group != nil {
								groupHitboxPush(*d.group, hitbox.ID(), app)
								defer groupHitboxPop(*d.group, app)
							}
							// insert_window_control_hitbox (div.rs:3068-3073).
							if d.windowControl != nil {
								w.InsertWindowControlHitbox(*d.windowControl, *hitbox)
							}
							// The click registration's path key (browser-core
							// ticket 12): the global id path — the hitbox's
							// ownerPath, the identity the up's click dispatch
							// resolves its target chain by.
							var clickPath string
							if global != nil {
								clickPath = global.key()
							}
							d.paintMouseListeners(*hitbox, clickPath, es, w, app)
							// paint_scroll_listener (div.rs:3081).
							d.scroll.paintListener(*hitbox, &style, w)
						}
						if style.Display == DisplayNone {
							return
						}
						// The children paint under the offset they
						// prepainted under (the port's drawables re-resolve
						// their bounds at paint; the pin reuses the
						// prepainted bounds), in the sticky paint order
						// (sticky.go: sticky children after the in-flow ones,
						// where their hitboxes already prepainted).
						withElementOffsetVoid(w, prepaint.scrollOffset, func(w *Window) {
							if standalone := layout.standaloneInline(); standalone != nil {
								standalone.paintChildren(stickyChildrenLast(d.children), w, app)
								return
							}
							for _, child := range stickyChildrenLast(d.children) {
								child.Paint(w, app)
							}
						})
						// browser-core 36: the overflow: auto scrollbar paints
						// after the children, above the content, inside this
						// content mask (scrollbar.go).
						paintAutoScrollbar(w, prepaint.autoScrollbar)
					})
				})
			})
		})
		return struct{}{}, es
	})
}

// paint is style.rs:874 Style::paint: inside a parent paragraph every
// placed fragment gets the box (with an empty continuation) before the
// continuation runs once; otherwise the element bounds get the box.
func (s *Style) paint(bounds Bounds, w *Window, app *App, continuation func(*Window)) {
	if fragments := currentFrame(w).currentInlineFragments; fragments != nil {
		for _, fragment := range fragments {
			s.paintBox(fragment, w, app, func(*Window) {})
		}
		continuation(w)
		return
	}
	s.paintBox(bounds, w, app, continuation)
}

// debugBelowDepth counts the DebugBelow scopes open per application
// (the pinned DebugBelow global set and removed around paint_box,
// style.rs:901-909 and 1011-1014).
var debugBelowDepth = map[*App]int{}

// paintBox is style.rs:894-1015 Style::paint_box: the debug outline,
// the drop shadows and the outer ring, the backdrop filter, then — inside
// the content filter group when the style has filters — the background
// quad, the inset ring and inset shadows, the continuation, and the
// border quad.
func (s *Style) paintBox(bounds Bounds, w *Window, app *App, continuation func(*Window)) {
	frame := currentFrame(w)
	mustPaint := func(err error, what string) {
		if err != nil {
			panic(fmt.Sprintf("gpui: DivElement %s paint: %v", what, err))
		}
	}
	if s.DebugBelow {
		debugBelowDepth[app]++
		defer func() {
			if debugBelowDepth[app]--; debugBelowDepth[app] <= 0 {
				delete(debugBelowDepth, app)
			}
		}()
	}
	if s.Debug || debugBelowDepth[app] > 0 {
		mustPaint(frame.scene.PaintQuad(Outline(bounds, SolidBackground(Red()), BorderStyleSolid), 0, frame.paint), "debug outline")
	}

	rem := RemSize(w)
	// CSS Backgrounds 3 §corner-overlap: overflowing radii scale by one
	// common factor (the pin clamped each corner to half the shortest
	// side; see Corners.ConstrainRadiiForQuadSize).
	radii := s.CornerRadii.ToPixels(rem).ConstrainRadiiForQuadSize(bounds.Size)
	var smoothing float32
	if s.CornerSmoothing != nil {
		smoothing = *s.CornerSmoothing
	}
	currentColor := WindowTextStyle(w).Color
	if color, ok := s.Text.Color.Get(); ok {
		currentColor = color
	}

	// paint_backdrop_filter_with_corner_smoothing (window.rs:4487): the
	// identity filters drop; rev 15's record carries a whole composed
	// chain (the blurs composed by quadrature, the colour functions as
	// one 4x5 matrix), so a chain paints one record (browser-core
	// ticket 41; the port once painted one record per filter). The
	// backdrop-filter node paints before the element's own filter group,
	// as in Chromium ("the backdrop-filter node comes first",
	// core/paint/paint_layer.cc:2629-2635 @ 0d4dcd4f).
	//
	// The CSS backdrop-filter blur length is the σ (filter-effects-2;
	// Chromium FEGaussianBlur takes the raw length,
	// filter_effect_builder.cc:284-291), and the pin's record already
	// lands there: the kernel computes σ = 0.5 × the record radius in
	// TEXELS of the half-resolution blur texture
	// (gpui_render/src/blur.rs:17 RADIUS_TO_STANDARD_DEVIATION, the
	// passes' viewport and texture size are downsampled_dimension, dx
	// blur passes in renderer/draw.rs), so the effective device-pixel σ
	// IS the record radius — CSS radius × scale, exactly the CSS value.
	// (The spec's D8 note "the pin uses σ = radius/2" read the texel
	// conversion without the half-resolution factor; the shadow-filter
	// Chrome fixture measured the pin value at 0.42 mean channel diff
	// vs 9.73 for the doubled radius.) No divergence: the pin's value
	// passes through.
	if chain := composeFilterChain(s.BackdropFilter); !chain.IsEmpty() {
		mustPaint(frame.scene.PaintBackdropFilterChain(bounds, chain, radii, smoothing, frame.paint), "backdrop filter")
	}

	paintUnit := func(w *Window) {
		frame := currentFrame(w)
		// paint_drop_shadows_with_corner_smoothing (window.rs:4375): the
		// non-inset shadows, before the background.
		//
		// Recorded divergences toward CSS (D8), all at this Style paint
		// path — the pin's Scene.PaintShadow keeps the pinned record for
		// the pin-conformance fixtures:
		//   - the shadows paint INSIDE the filter group, so `filter:`
		//     blurs the element's own shadow (filter-effects-1
		//     §FilterProperty: the filter applies to the whole rendered
		//     element, including its box-shadow and border; Chromium
		//     paints the decorations inside the filter effect node, whose
		//     reference box is the layer's visual overflow including the
		//     shadow ink — PaintLayer::LocalBoundingBox →
		//     VisualOverflowRect, paint_layer.cc:2199-2205). The pin
		//     painted the shadows and the ring above/outside
		//     with_filter_layer (style.rs:918-930), so its blur never
		//     touched them.
		//   - the list paints in REVERSE order so the FIRST shadow paints
		//     on top (css-backgrounds-3 §box-shadow; Chromium "the
		//     first-specified shadow is painted on top", shadow_list.h:
		//     46-47 — box_painter_base.cc:295 iterates i--). The pin
		//     painted them in list order, so its LAST shadow was on top.
		//   - PaintBoxShadow: CSS geometry (σ = blur/2, spread-grown
		//     corner radii).
		for i := len(s.BoxShadow) - 1; i >= 0; i-- {
			shadow := s.BoxShadow[i]
			if !shadow.Inset {
				mustPaint(frame.scene.PaintBoxShadow(bounds, shadow, radii, smoothing, frame.paint), "drop shadow")
			}
		}
		if ring, ok := s.Ring.outerQuad(bounds, radii, currentColor); ok {
			mustPaint(frame.scene.PaintQuad(ring, smoothing, frame.paint), "ring")
		}
		if s.Background != nil && !s.Background.IsTransparent() {
			background := *s.Background
			mustPaint(frame.scene.PaintQuad(PaintQuad{
				Bounds:             bounds,
				CornerRadii:        radii,
				Background:         background,
				BorderColor:        background.Opacity(0),
				BorderStyle:        s.BorderStyle,
				BorderDashedLength: DefaultBorderDashedLength,
				BorderDashedGap:    DefaultBorderDashedGap,
			}, smoothing, frame.paint), "background")
		}
		// paint_inset_shadows (window.rs:4424) — cast from the PADDING
		// edge, CSS's model ("an inner box-shadow casts a shadow as if
		// everything outside the padding edge were opaque"): the hole and
		// the clip region derive from the element bounds inset by the
		// border widths, with the radii reduced by the border (clamped at
		// zero). Recorded deviation from the pin (consumer gap 09): the
		// reference casts inner shadows from the border box, where the
		// border quad — painted after — covers any band narrower than the
		// border, so a hard `inset 0 1px 0` line on a 1px-bordered panel
		// never shows; the padding edge is where CSS (and a 1:1 HTML
		// port) draws it. A borderless element's padding box IS its border
		// box, so its records keep the pinned geometry exactly.
		insetBounds := bounds
		insetRadii := radii
		if widths := s.borderEdges(rem); widths.Top != 0 || widths.Right != 0 || widths.Bottom != 0 || widths.Left != 0 {
			width := bounds.Size.Width - widths.Left - widths.Right
			height := bounds.Size.Height - widths.Top - widths.Bottom
			if width < 0 {
				width = 0
			}
			if height < 0 {
				height = 0
			}
			insetBounds = Bounds{
				Origin: Point{X: bounds.Origin.X + widths.Left, Y: bounds.Origin.Y + widths.Top},
				Size:   Size{Width: width, Height: height},
			}
			insetRadii = Corners{
				TopLeft:     max32(radii.TopLeft-max32(widths.Top, widths.Left), 0),
				TopRight:    max32(radii.TopRight-max32(widths.Top, widths.Right), 0),
				BottomRight: max32(radii.BottomRight-max32(widths.Bottom, widths.Right), 0),
				BottomLeft:  max32(radii.BottomLeft-max32(widths.Bottom, widths.Left), 0),
			}
		}
		if ring, ok := s.InsetRing.insetShadow(currentColor); ok {
			mustPaint(frame.scene.PaintBoxShadow(insetBounds, ring, insetRadii, smoothing, frame.paint), "inset ring")
		}
		// paint_inset_shadows_with_corner_smoothing (window.rs:4424) —
		// reversed so the FIRST shadow paints on top (Chromium
		// box_painter_base.cc:416 iterates i--, shadow_list.h:46-47),
		// with PaintBoxShadow's CSS blur (σ = blur/2).
		for i := len(s.BoxShadow) - 1; i >= 0; i-- {
			shadow := s.BoxShadow[i]
			if shadow.Inset {
				mustPaint(frame.scene.PaintBoxShadow(insetBounds, shadow, insetRadii, smoothing, frame.paint), "inset shadow")
			}
		}

		continuation(w)

		if s.isBorderVisible() {
			borderColor := *s.BorderColor
			mustPaint(currentFrame(w).scene.PaintQuad(PaintQuad{
				Bounds:             bounds,
				CornerRadii:        radii,
				Background:         borderColor.Opacity(0),
				BorderWidths:       s.BorderWidths.ToPixels(rem),
				BorderColor:        borderColor,
				BorderStyle:        s.BorderStyle,
				BorderDashedLength: s.BorderDashedLength,
				BorderDashedGap:    s.BorderDashedGap,
			}, smoothing, currentFrame(w).paint), "border")
		}
	}

	if len(s.Filter) == 0 {
		paintUnit(w)
		return
	}
	chain := composeFilterChain(s.Filter)
	if chain.IsEmpty() {
		paintUnit(w)
		return
	}
	// with_filter_layer_with_corner_smoothing (window.rs:4537), rev 15:
	// one group per chain carrying the whole composed payload — the
	// blurs as one quadrature-composed radius and the colour functions
	// as one 4x5 matrix in CSS chain order (browser-core ticket 41; the
	// port once nested one group per filter, which the depth cap of 2
	// silently degraded for chains of three or more). An identity chain
	// runs inline with no group.
	//
	// Recorded divergence toward CSS (D8) at this Style paint path, and
	// one verified non-divergence:
	//   - the group's bounds are the element's INK bounds — the border box
	//     dilated by the drop shadows' and the outer ring's extent — so
	//     the group's composite covers the blurred shadow instead of
	//     cutting it at the element bounds (+3σ). Chromium's filter
	//     reference box is the layer's visual overflow, which the
	//     box-shadow ink feeds (PaintLayer::LocalBoundingBox →
	//     VisualOverflowRect, paint_layer.cc:2199-2205, from
	//     ComputedStyle::BoxDecorationOutsets, computed_style.cc:
	//     2697-2709). The pin passed the element bounds.
	//   - the blur's σ is the CSS length (filter-effects-1 §funcdef-
	//     filter-blur: the length is the standard deviation; Chromium
	//     FEGaussianBlur(std_deviation = the raw length),
	//     filter_effect_builder.cc:284-291). The pin's kernel computes
	//     σ = 0.5 × the record radius in half-resolution TEXELS
	//     (blur.rs:17; the blur passes run on the downsampled ping/pong
	//     textures, draw.rs), so the effective device-pixel σ IS the
	//     record radius — CSS radius × scale, already the CSS value.
	//     No divergence: the pin's radius passes through (the spec's
	//     D8 "the pin uses σ = radius/2" read the texel conversion
	//     without the half-resolution factor; the shadow-filter Chrome
	//     fixture measured the pin value at 0.42 mean channel diff vs
	//     9.73 for the doubled radius).
	var ink Edges
	for _, shadow := range s.BoxShadow {
		if shadow.Inset {
			continue
		}
		o := shadow.inkOutsets()
		ink.Top = max32(ink.Top, o.Top)
		ink.Right = max32(ink.Right, o.Right)
		ink.Bottom = max32(ink.Bottom, o.Bottom)
		ink.Left = max32(ink.Left, o.Left)
	}
	if s.Ring.Width > 0 {
		ink.Top = max32(ink.Top, s.Ring.Width)
		ink.Right = max32(ink.Right, s.Ring.Width)
		ink.Bottom = max32(ink.Bottom, s.Ring.Width)
		ink.Left = max32(ink.Left, s.Ring.Width)
	}
	groupBounds := Bounds{
		Origin: Point{X: bounds.Origin.X - ink.Left, Y: bounds.Origin.Y - ink.Top},
		Size:   Size{Width: bounds.Size.Width + ink.Left + ink.Right, Height: bounds.Size.Height + ink.Top + ink.Bottom},
	}
	frame2 := currentFrame(w)
	if err := frame2.scene.WithFilterLayerChain(groupBounds, chain, radii, smoothing, frame2.paint, func() error {
		paintUnit(w)
		return nil
	}); err != nil {
		panic(fmt.Sprintf("gpui: DivElement filter paint: %v", err))
	}
}
