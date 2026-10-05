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
// Bounds of this slice (deliberate, separate tickets): hitboxes, mouse
// listeners, focus routing and key contexts are ticket13; scroll handles
// are ticket14; inline layout is ticket15. The div stores what those
// tickets need (the focus handle, the key context, the listeners) without
// dispatching anything.

import (
	"fmt"
	"reflect"

	"gpui-go/authoring"
)

// StyleRefinement is the authoring style refinement the div composes
// with (the shared authoring type identity, avoiding a root/authoring
// dependency cycle).
type StyleRefinement = authoring.StyleRefinement

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

// TextStyle is the resolved text style of an element (the reference
// TextStyle): the fields this slice's text element consumes.
type TextStyle struct {
	// Color is the text color.
	Color Hsla
	// FontFamily is the font family (".SystemUIFont" by default).
	FontFamily string
	// FontWeight is the font weight (400 by default).
	FontWeight float32
	// FontStyle is the font style (Normal by default).
	FontStyle FontStyle
	// FontSize is the font size in pixels or rems (1rem by default).
	FontSize AbsoluteLength
	// LineHeight is the line height in pixels, rems or a relative
	// fraction (the golden ratio by default).
	LineHeight DefiniteLength
	// TextAlign is the alignment within the element.
	TextAlign TextAlign
	// WhiteSpace is the wrapping mode.
	WhiteSpace WhiteSpace
}

// DefaultTextStyle returns the reference TextStyle::default(): black
// text, the system UI font at 1rem, the golden-ratio line height,
// left-aligned normal white space.
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
// to its children (the reference TextStyleRefinement; the fields this
// slice consumes). Absent fields leave the previous values unchanged.
type TextStyleRefinement struct {
	// Color refines the text color.
	Color *Hsla
	// FontFamily refines the font family.
	FontFamily *string
	// FontWeight refines the font weight.
	FontWeight *float32
	// FontStyle refines the font style.
	FontStyle *FontStyle
	// FontSize refines the font size.
	FontSize *AbsoluteLength
	// LineHeight refines the line height.
	LineHeight *DefiniteLength
	// TextAlign refines the alignment.
	TextAlign *TextAlign
	// WhiteSpace refines the wrapping mode.
	WhiteSpace *WhiteSpace
}

// Refine applies the explicitly supplied fields onto style.
func (r *TextStyleRefinement) Refine(style *TextStyle) {
	if r.Color != nil {
		style.Color = *r.Color
	}
	if r.FontFamily != nil {
		style.FontFamily = *r.FontFamily
	}
	if r.FontWeight != nil {
		style.FontWeight = *r.FontWeight
	}
	if r.FontStyle != nil {
		style.FontStyle = *r.FontStyle
	}
	if r.FontSize != nil {
		style.FontSize = *r.FontSize
	}
	if r.LineHeight != nil {
		style.LineHeight = *r.LineHeight
	}
	if r.TextAlign != nil {
		style.TextAlign = *r.TextAlign
	}
	if r.WhiteSpace != nil {
		style.WhiteSpace = *r.WhiteSpace
	}
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
// style (TextStyle::font / to_run).
func (t TextStyle) FontDescriptorOf() FontDescriptor {
	return FontDescriptor{
		Family: t.FontFamily,
		Weight: t.FontWeight,
		Style:  t.FontStyle,
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
	// lineCount is the visual line count.
	lineCount() int
	// textLen is the shaped text's UTF-8 length.
	textLen() int
	// size is the wrapped size for a line height (WrappedLine::size).
	size(lineHeight float32) Size
	// paint paints the line at the origin with the window's current
	// paint state (WrappedLine::paint → paint_visual_text).
	paint(w *Window, origin Point, lineHeight float32, color Hsla) error
}

// realWindowTextSystem is the real text stack seam: shaping through the
// port's TextSystem and painting through the scene's shaped-text paint
// path (PaintShapedText) with the window's atlas.
type realWindowTextSystem struct {
	system *TextSystem
	atlas  *Atlas
}

// windowTextSystemOf returns the real text system seam, constructing the
// shared text system and atlas once per process.
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
	run := TextRun{Len: len(text), Font: font}
	request := TextLayoutRequest{
		Text:      text,
		FontSize:  fontSize,
		Runs:      []TextRun{run},
		WrapWidth: wrapWidth,
		LineClamp: lineClamp,
	}
	line, err := r.system.ShapeText(request)
	if err != nil {
		return nil, err
	}
	return &realShapedText{line: line, system: r.system, atlas: r.atlas}, nil
}

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
	return frame.scene.PaintShapedText(s.line, origin, lineHeight, color, s.system, s.atlas, frame.paint, PaintTextOptions{})
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
	size      Size
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
	return Role(1), true
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
// including its cached-layout reuse under matching wrap widths).
func (t *textElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, stringLayoutState) {
	textStyle := WindowTextStyle(w)
	rem := RemSize(w)
	fontSize := textStyle.FontSizePixels(rem)
	lineHeight := PixelSnap(w, textStyle.LineHeightPixels(fontSize, rem))

	cell := &shapedCell{}
	state := stringLayoutState{lineHeight: lineHeight, fontSize: fontSize, cell: cell}
	measure := func(req MeasureRequest) Size {
		// The pinned measure closure: wrap at the known width or the
		// definite available width (WhiteSpace::Normal); min/max-content
		// content-based sizing measures unwrapped.
		var wrapWidth *float32
		if textStyle.WhiteSpace == WhiteSpaceNowrap {
			wrapWidth = nil
		} else if req.KnownWidthPresent {
			width := req.KnownWidth
			wrapWidth = &width
		} else if req.AvailWidth.Kind == AvailDefinite {
			width := req.AvailWidth.Definite
			wrapWidth = &width
		}
		// Cached-layout reuse: a shaped layout with a size answers when
		// the wrap is unconstrained or matches the cached wrap (the
		// reference's four-condition cache check, truncation aside).
		if cell.shaped != nil && (wrapWidth == nil || (cell.wrapWidth != nil && *wrapWidth == *cell.wrapWidth)) {
			return cell.size
		}
		shaped, err := frameTextSystem(w).shape(t.text, fontSize, textStyle.FontDescriptorOf(), wrapWidth, nil)
		if err != nil {
			panic(fmt.Sprintf("gpui: text element shaping failed: %v", err))
		}
		cell.shaped = shaped
		cell.wrapWidth = wrapWidth
		cell.size = shaped.size(lineHeight)
		return cell.size
	}
	layoutID, err := requestMeasuredLayoutOf(w, DefaultStyle(), measure)
	if err != nil {
		panic(fmt.Sprintf("gpui: text element request_layout: %v", err))
	}
	state.layoutID = layoutID
	return layoutID, state
}

// Prepaint implements Element: commit the parent-relative bounds (the
// reference TextLayout::prepaint uses parent_relative_layout_bounds so
// text placement stays stable when its container moves).
func (t *textElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *stringLayoutState, w *Window, app *App) struct{} {
	textBounds, err := parentRelativeLayoutBoundsOf(w, layout.layoutID)
	if err != nil {
		panic(fmt.Sprintf("gpui: text element prepaint bounds: %v", err))
	}
	layout.bounds = &textBounds
	return struct{}{}
}

// Paint implements Element: draw the shaped line at the committed
// bounds origin (the reference TextLayout::paint with the ambient text
// style).
func (t *textElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *stringLayoutState, prepaint *struct{}, w *Window, app *App) {
	if layout.cell == nil || layout.cell.shaped == nil || layout.bounds == nil {
		panic(fmt.Sprintf("gpui: measurement or prepaint has not been performed on %q", t.text))
	}
	textStyle := WindowTextStyle(w)
	if err := layout.cell.shaped.paint(w, layout.bounds.Origin, layout.lineHeight, textStyle.Color); err != nil {
		panic(fmt.Sprintf("gpui: text element paint: %v", err))
	}
}

// LayoutNodeStyle implements the root-stretch report (the text node's
// style is the default style).
func (t *textElement) LayoutNodeStyle() (Style, bool) { return DefaultStyle(), true }

// ---------------------------------------------------------------------------
// Div (elements/div.rs Div / Interactivity, bounded to this slice)
// ---------------------------------------------------------------------------

// ClickEvent is the click event payload of OnClick listeners (the
// reference ClickEvent; the payload fields arrive with the input ticket).
type ClickEvent struct {
	// Button is the mouse button (0 primary).
	Button uint8
}

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
// DivFrameState): the children's layout ids.
type divFrameState struct {
	// childLayoutIDs are the requested layout nodes of the children.
	childLayoutIDs []LayoutID
}

// DivElement is the concrete div element builder (the fixture API
// spelling: the Div() constructor returns *DivElement because a type and
// a function cannot share the package-level identifier Div).
type DivElement struct {
	// elementID is the optional element identity (div.rs
	// Interactivity::element_id).
	elementID *string
	// focusHandle is the tracked focus handle (ticket13 routes it).
	focusHandle *FocusHandle
	// keyContext is the key context name (ticket13's keymap layer).
	keyContext *string
	// style is the layout style (the interactivity base style refined by
	// the fluent methods).
	style Style
	// textStyle is the text style refinement cascaded to children.
	textStyle TextStyleRefinement
	// background is the box background (painted when non-transparent).
	background *Background
	// opacity is the element opacity applied at paint.
	opacity *float32
	// children are the converted child elements.
	children []AnyElement
	// clickListeners are the registered click listeners (ticket13
	// dispatch).
	clickListeners []func(*ClickEvent, *Window, *App)
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
	// tabIndex is the interactivity tab index (div.rs tab_index).
	tabIndex *int64
	// tabGroup marks a tab group (div.rs tab_group).
	tabGroup bool
	// tabStop records an interactivity tab-stop request (div.rs
	// tab_stop; explicit track_focus handles carry their own).
	tabStop bool
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
	return &DivElement{style: DefaultStyle()}
}

// ID assigns this element a string identity (DivElement::id): identity
// participates in the global element id path and keys retained element
// state.
func (d *DivElement) ID(value string) *DivElement {
	d.elementID = &value
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

// Flex sets the display type to flex (Styled::flex).
func (d *DivElement) Flex() *DivElement {
	d.style.Display = DisplayFlex
	return d
}

// Gap2 sets the gap to 0.5rem (Styled::gap_2).
func (d *DivElement) Gap2() *DivElement {
	d.style.Gap = DefiniteLengthSize{Width: DefiniteRem(0.5), Height: DefiniteRem(0.5)}
	return d
}

// Gap sets the gap between children.
func (d *DivElement) Gap(width, height DefiniteLength) *DivElement {
	d.style.Gap = DefiniteLengthSize{Width: width, Height: height}
	return d
}

// P sets the padding on all four edges (Styled::p).
func (d *DivElement) P(padding DefiniteLength) *DivElement {
	d.style.Padding = DefiniteLengthEdges{Top: padding, Right: padding, Bottom: padding, Left: padding}
	return d
}

// SizeFull sets the size to 100% of the parent (Styled::size_full).
func (d *DivElement) SizeFull() *DivElement {
	d.style.Size = LengthSize{Width: DefiniteLengthOf(Fraction(1.0)), Height: DefiniteLengthOf(Fraction(1.0))}
	return d
}

// Justify sets the main-axis content distribution (Styled::justify).
func (d *DivElement) Justify(justify AlignContent) *DivElement {
	d.style.JustifyContent = &justify
	return d
}

// AlignItems sets the cross-axis item alignment.
func (d *DivElement) AlignItems(align AlignItems) *DivElement {
	d.style.AlignItems = &align
	return d
}

// Bg sets the background color of the box (Styled::bg).
func (d *DivElement) Bg(color Hsla) *DivElement {
	d.background = &[]Background{SolidBackground(color)}[0]
	return d
}

// Opacity sets the element opacity applied at paint (Styled::opacity).
func (d *DivElement) Opacity(opacity float32) *DivElement {
	d.opacity = &opacity
	return d
}

// TextSize sets the text font size cascading to children
// (Styled::text_size).
func (d *DivElement) TextSize(size AbsoluteLength) *DivElement {
	d.textStyle.FontSize = &size
	return d
}

// TextColor sets the text color cascading to children
// (Styled::text_color).
func (d *DivElement) TextColor(color Hsla) *DivElement {
	d.textStyle.Color = &color
	return d
}

// Refine applies the authoring style refinement onto this element (the
// fixture Refine method; the authoring Styled helper's snapshot composes
// through it).
func (d *DivElement) Refine(value StyleRefinement) *DivElement {
	if display, ok := value.Display.Get(); ok {
		if display == authoring.DisplayFlex {
			d.style.Display = DisplayFlex
		} else {
			d.style.Display = DisplayBlock
		}
	}
	if padding, ok := value.PaddingLeft.Get(); ok {
		d.style.Padding.Left = authoringLengthToDefinite(padding)
	}
	if padding, ok := value.PaddingRight.Get(); ok {
		d.style.Padding.Right = authoringLengthToDefinite(padding)
	}
	if gap, ok := value.Gap.Get(); ok {
		converted := authoringLengthToDefinite(gap)
		d.style.Gap = DefiniteLengthSize{Width: converted, Height: converted}
	}
	if opacity, ok := value.Opacity.Get(); ok {
		opacity := opacity
		d.opacity = &opacity
	}
	return d
}

// authoringLengthToDefinite converts an authoring length into a port
// definite length.
func authoringLengthToDefinite(length authoring.Length) DefiniteLength {
	if length.Unit == authoring.Rems {
		return DefiniteRem(length.Value)
	}
	return DefinitePx(length.Value)
}

// DebugSelector sets the test-support bounds key of this element (the
// reference debug_selector: the element's bounds are recorded per frame
// and queryable through the test window).
func (d *DivElement) DebugSelector(selector string) *DivElement {
	d.debugSelector = selector
	return d
}

// OnClick binds a click listener to this element (the fluent spelling
// of Interactivity::on_click; delivery arrives with the input ticket).
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
	return NameElementID(*e.builder.elementID), true
}

// SourceLocation implements Element.
func (e *divElement) SourceLocation() *SourceLocation { return nil }

// A11yRole implements the accessibility role capability: the div carries
// no role (the reference filters GenericContainer).
func (e *divElement) A11yRole() (Role, bool) { return 0, false }

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

// ChildCount reports the destination's committed child count (the
// test-support observable of the checked child-insertion contract: a
// failed batch preserves it).
func (d *DivElement) ChildCount() int { return len(d.children) }

// IntoElement implements IntoElement: the div builder converts into its
// element (the reference IntoElement for Div wraps the builder).
func (d *DivElement) IntoElement() AnyElement {
	return CustomElement[divFrameState, divPrepaintState](&divElement{builder: d})
}

// divPrepaintState is the div's prepaint state (the reference
// Option<Hitbox>; hitboxes arrive with the input ticket, so this slice
// records whether the frame recorded the element bounds).
type divPrepaintState struct {
	// debugBounds records whether the debug selector was recorded.
	debugBounds bool
}

// computeStyle returns the div's complete layout style (the reference
// compute_style: Style::default().refine(base_style)).
func (d *DivElement) computeStyle() Style {
	return d.style
}

// textStyleSheet builds the div's text style refinement scope (the
// reference style.text_style()).
func (d *DivElement) textStyleSheet() *TextStyleRefinement {
	return &d.textStyle
}

// overflowMask computes the div's content-mask refinement for its
// overflow style (the reference Style::overflow_mask): visible overflow
// returns nil.
func (d *DivElement) overflowMask(bounds Bounds, rem float32) *Bounds {
	if d.style.OverflowX == OverflowVisible && d.style.OverflowY == OverflowVisible {
		return nil
	}
	// Clip/hidden/scroll overflow intersects the mask with the bounds.
	mask := bounds
	if d.style.OverflowX == OverflowVisible {
		mask.Origin.X = bounds.Origin.X - 1e9
		mask.Size.Width = 2e9
	}
	if d.style.OverflowY == OverflowVisible {
		mask.Origin.Y = bounds.Origin.Y - 1e9
		mask.Size.Height = 2e9
	}
	return &mask
}

// RequestLayout implements Element (the div.rs Div::request_layout +
// Interactivity::request_layout path, bounded): compute the style, scope
// the text style, request the children, then request this element's
// layout node with the children.
func (d *DivElement) requestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, divFrameState) {
	style := d.computeStyle()
	d.lastStyle = style
	state := divFrameState{}
	var layoutID LayoutID
	WithTextStyleVoid(w, d.textStyleSheet(), func(w *Window) {
		childLayoutIDs := make([]LayoutID, 0, len(d.children))
		for i := range d.children {
			childLayoutIDs = append(childLayoutIDs, d.children[i].RequestLayout(w, app))
		}
		state.childLayoutIDs = childLayoutIDs
		id, err := requestLayoutOf(w, style, childLayoutIDs...)
		if err != nil {
			panic(fmt.Sprintf("gpui: DivElement request_layout: %v", err))
		}
		layoutID = id
	})
	return layoutID, state
}

// Prepaint implements Element (the Interactivity::prepaint path,
// bounded): scope the text style and the overflow content mask, record
// the debug-selector bounds, then prepaint the children in order.
func (d *DivElement) prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, w *Window, app *App) divPrepaintState {
	state := divPrepaintState{}
	if d.debugSelector != "" {
		recordDebugBounds(w, d.debugSelector, bounds)
		state.debugBounds = true
	}
	WithTextStyleVoid(w, d.textStyleSheet(), func(w *Window) {
		WithContentMaskVoid(w, d.overflowMask(bounds, RemSize(w)), func(w *Window) {
			registerDivElementDispatch(w, d)
			for i := range d.children {
				d.children[i].Prepaint(w, app)
			}
		})
	})
	return state
}

// registerDivElementDispatch pushes the div's dispatch node and
// registers its interactive state on the frame under construction
// (div.rs Interactivity paint: with_tab_group + tab_stops.insert + the
// dispatch-tree registrations; the port registers during prepaint so
// the completed frame carries the tree — see key_dispatch.go's
// header).
func registerDivElementDispatch(w *Window, d *DivElement) {
	tree := currentDispatchTree(w)
	tree.PushNode()
	if d.keyContext != nil {
		if context, err := ParseKeyContext(*d.keyContext); err == nil {
			tree.SetKeyContext(context)
		}
	}
	if d.focusHandle != nil {
		tree.SetFocusID(d.focusHandle.id)
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

	// The tab group opens before the children (their paths carry the
	// group index); the container's own handle is inserted inside the
	// group (div.rs: the container sorts with its children). The group
	// is closed and the node popped by prepaint after the children.
	if d.focusHandle != nil {
		if d.tabGroup && d.tabIndex != nil {
			currentTabStops(w).BeginGroup(*d.tabIndex)
		}
		registerFocusStop(w, *d.focusHandle)
	}
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

// Paint implements Element (the Interactivity::paint + Style::paint
// path, bounded): scope the element opacity, paint the background quad
// when non-transparent, paint the children, then paint the border when
// visible.
func (d *DivElement) paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *divFrameState, prepaint *divPrepaintState, w *Window, app *App) {
	WithElementOpacityVoid(w, d.opacity, func(w *Window) {
		paintDivElementBox(d, bounds, w, func(w *Window) {
			WithTextStyleVoid(w, d.textStyleSheet(), func(w *Window) {
				WithContentMaskVoid(w, d.overflowMask(bounds, RemSize(w)), func(w *Window) {
					for i := range d.children {
						d.children[i].Paint(w, app)
					}
				})
			})
		})
	})
}

// paintDivElementBox paints the div's own box: the background quad
// before the continuation and the border quad after it (the reference
// Style::paint_box, bounded to background and border).
func paintDivElementBox(d *DivElement, bounds Bounds, w *Window, continuation func(*Window)) {
	frame := currentFrame(w)
	if d.background != nil && !d.background.IsTransparent() {
		background := *d.background
		borderColor := background.Opacity(0)
		pq := PaintQuad{
			Bounds:             bounds,
			Background:         background,
			BorderColor:        borderColor,
			CornerRadii:        Corners{},
			BorderWidths:       Edges{},
			BorderStyle:        BorderStyleSolid,
			BorderDashedLength: DefaultBorderDashedLength,
			BorderDashedGap:    DefaultBorderDashedGap,
		}
		if err := frame.scene.PaintQuad(pq, 0, frame.paint); err != nil {
			panic(fmt.Sprintf("gpui: DivElement background paint: %v", err))
		}
	}
	continuation(w)
}
