package gpui

import (
	"fmt"
	"strconv"
	"strings"
)

// This file ports the layout-relevant style surface of the pinned GPUI-CE
// source (254b5dbd47cbb5acbcc5bbdcbb322a339276c88a): the Length/
// DefiniteLength/AbsoluteLength units, the enums Style forwards to Taffy,
// and the Style struct itself (crates/gpui/src/geometry.rs and
// crates/gpui/src/style.rs). Only the fields the pinned adapter forwards
// (crates/gpui/src/taffy.rs::into_taffy_style) are carried; visual style
// (background, borders as paint, text, shadows) stays out of this slice.
//
// Units are LOGICAL pixels at the style layer, exactly like the reference:
// authored values convert to device pixels once, inside the layout
// adapter, with the pinned rounding rules (see layout.go). Percentages
// stay fractions of the parent; Auto stays auto. The style record sent
// across the native ABI distinguishes absent from explicit zero, but a
// port Style is always complete (the reference Style is a struct with
// defaults, not a refinement): every field is forwarded, mirroring
// into_taffy_style.

// ---------------------------------------------------------------------------
// Length types
// ---------------------------------------------------------------------------

// AbsoluteLengthKind selects the unit of an AbsoluteLength.
type AbsoluteLengthKind uint8

const (
	// AbsoluteLengthPx is a length in logical pixels (reference AbsoluteLength::Pixels).
	AbsoluteLengthPx AbsoluteLengthKind = iota
	// AbsoluteLengthRem is a length in rems (reference AbsoluteLength::Rems).
	AbsoluteLengthRem
)

// AbsoluteLength is an absolute length in logical pixels or rems, the
// reference crate::AbsoluteLength (geometry.rs). It cannot be a
// percentage or auto.
type AbsoluteLength struct {
	// Kind selects px or rem.
	Kind AbsoluteLengthKind
	// Value is the amount: logical pixels, or the rem count.
	Value float32
}

// Px returns an absolute length in logical pixels.
func Px(v float32) AbsoluteLength { return AbsoluteLength{Kind: AbsoluteLengthPx, Value: v} }

// RemsOf returns an absolute length in rems.
func RemsOf(v float32) AbsoluteLength { return AbsoluteLength{Kind: AbsoluteLengthRem, Value: v} }

// IsZero reports whether the length is exactly zero in its own unit,
// mirroring AbsoluteLength::is_zero.
func (l AbsoluteLength) IsZero() bool { return l.Value == 0 }

// ToPixels converts the length to logical pixels against the given rem
// size, mirroring AbsoluteLength::to_pixels (Rems::to_pixels is
// rems * rem_size, an f32 multiply).
func (l AbsoluteLength) ToPixels(remSize float32) float32 {
	if l.Kind == AbsoluteLengthRem {
		return l.Value * remSize
	}
	return l.Value
}

// String renders the length like the reference Display impl
// ("42px", "1.5rem").
func (l AbsoluteLength) String() string {
	if l.Kind == AbsoluteLengthRem {
		return trimFloat(l.Value) + "rem"
	}
	return trimFloat(l.Value) + "px"
}

// DefiniteLengthKind selects the unit of a DefiniteLength.
type DefiniteLengthKind uint8

const (
	// DefiniteLengthPx is a definite length in logical pixels.
	DefiniteLengthPx DefiniteLengthKind = iota
	// DefiniteLengthRem is a definite length in rems.
	DefiniteLengthRem
	// DefiniteLengthFraction is a length relative to the parent: the reference
	// DefiniteLength::Relative(Relative), a fraction where 0.5 is 50%.
	DefiniteLengthFraction
)

// DefiniteLength is a non-auto length: logical pixels, rems, or a
// fraction of the parent, the reference crate::DefiniteLength.
type DefiniteLength struct {
	// Kind selects the unit.
	Kind DefiniteLengthKind
	// Value is the amount: logical pixels, the rem count, or the fraction
	// (0.5 = 50% of the parent).
	Value float32
}

// Absolute returns the DefiniteLength as an AbsoluteLength; it panics for
// a fraction (mirroring the reference enum, where Relative is a distinct
// variant).
func (d DefiniteLength) Absolute() AbsoluteLength {
	if d.Kind == DefiniteLengthFraction {
		panic("gpui: DefiniteLength.Absolute on a relative length")
	}
	return AbsoluteLength{Kind: AbsoluteLengthKind(d.Kind), Value: d.Value}
}

// DefinitePx returns a definite length in logical pixels.
func DefinitePx(v float32) DefiniteLength { return DefiniteLength{Kind: DefiniteLengthPx, Value: v} }

// DefiniteRem returns a definite length in rems.
func DefiniteRem(v float32) DefiniteLength { return DefiniteLength{Kind: DefiniteLengthRem, Value: v} }

// Fraction returns a definite length relative to the parent (0.5 = 50%).
func Fraction(v float32) DefiniteLength {
	return DefiniteLength{Kind: DefiniteLengthFraction, Value: v}
}

// String renders the length like the reference Display impl
// ("42px", "1.5rem", "50%").
func (d DefiniteLength) String() string {
	switch d.Kind {
	case DefiniteLengthFraction:
		return trimFloat(d.Value*100) + "%"
	case DefiniteLengthRem:
		return trimFloat(d.Value) + "rem"
	default:
		return trimFloat(d.Value) + "px"
	}
}

// Length is a definite length or the automatic length, the reference
// crate::Length (Definite(DefiniteLength) | Auto).
type Length struct {
	// Auto selects the automatic length (reference Length::Auto).
	Auto bool
	// Definite carries the definite length when Auto is false.
	Definite DefiniteLength
}

// AutoLength returns the automatic length.
func AutoLength() Length { return Length{Auto: true} }

// DefiniteLengthOf returns a definite Length.
func DefiniteLengthOf(d DefiniteLength) Length { return Length{Definite: d} }

// PxLength returns a Length in logical pixels.
func PxLength(v float32) Length { return Length{Definite: DefinitePx(v)} }

// RemsLength returns a Length in rems.
func RemsLength(v float32) Length { return Length{Definite: DefiniteRem(v)} }

// String renders the length like the reference Display impl ("42px",
// "1.5rem", "50%", "auto").
func (l Length) String() string {
	if l.Auto {
		return "auto"
	}
	return l.Definite.String()
}

// trimFloat renders a float the way Rust's default float Display does
// (shortest representation, no trailing ".0") for String methods.
func trimFloat(v float32) string {
	s := strconv.FormatFloat(float64(v), 'f', -1, 32)
	return s
}

// ---------------------------------------------------------------------------
// Length parsing (reference TryFrom<&str> impls in geometry.rs)
// ---------------------------------------------------------------------------

// ParseAbsoluteLength parses "42px" or "1.5rem" (reference
// AbsoluteLength::try_from: a px suffix is tried first, then rem; the
// number is parsed as f32).
func ParseAbsoluteLength(s string) (AbsoluteLength, error) {
	if v, ok := strings.CutSuffix(s, "px"); ok {
		f, err := parseFloat32(v)
		if err != nil {
			return AbsoluteLength{}, fmt.Errorf("invalid AbsoluteLength %q, expected number with 'px' or 'rem' suffix: %v", s, err)
		}
		return Px(f), nil
	}
	if v, ok := strings.CutSuffix(s, "rem"); ok {
		f, err := parseFloat32(v)
		if err != nil {
			return AbsoluteLength{}, fmt.Errorf("invalid AbsoluteLength %q, expected number with 'px' or 'rem' suffix: %v", s, err)
		}
		return RemsOf(f), nil
	}
	return AbsoluteLength{}, fmt.Errorf("invalid AbsoluteLength %q, expected number with 'px' or 'rem' suffix", s)
}

// ParseDefiniteLength parses "42px", "1.5rem" or "50%" (reference
// DefiniteLength::try_from: a '%' suffix is a Relative with the number
// divided by 100; anything else is an AbsoluteLength).
func ParseDefiniteLength(s string) (DefiniteLength, error) {
	if v, ok := strings.CutSuffix(s, "%"); ok {
		f, err := parseFloat32(v)
		if err != nil {
			return DefiniteLength{}, fmt.Errorf("invalid DefiniteLength %q, expected number with 'px', 'rem', or '%%' suffix: %v", s, err)
		}
		return Fraction(f / 100), nil
	}
	abs, err := ParseAbsoluteLength(s)
	if err != nil {
		return DefiniteLength{}, fmt.Errorf("invalid DefiniteLength %q, expected number with 'px', 'rem', or '%%' suffix", s)
	}
	return DefiniteLength{Kind: DefiniteLengthKind(abs.Kind), Value: abs.Value}, nil
}

// ParseLength parses "auto", "42px", "1.5rem" or "50%" (reference
// Length::try_from).
func ParseLength(s string) (Length, error) {
	if s == "auto" {
		return AutoLength(), nil
	}
	d, err := ParseDefiniteLength(s)
	if err != nil {
		return Length{}, fmt.Errorf("invalid Length %q, expected 'auto' or number with 'px', 'rem', or '%%' suffix", s)
	}
	return DefiniteLengthOf(d), nil
}

// parseFloat32 parses an f32 like Rust's str::parse::<f32>.
func parseFloat32(s string) (float32, error) {
	f, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}

// ---------------------------------------------------------------------------
// Edges and sizes of length types
// ---------------------------------------------------------------------------

// LengthEdges is Edges<Length>: four Length values in the reference
// top/right/bottom/left order.
type LengthEdges struct {
	Top    Length
	Right  Length
	Bottom Length
	Left   Length
}

// DefiniteLengthEdges is Edges<DefiniteLength>.
type DefiniteLengthEdges struct {
	Top    DefiniteLength
	Right  DefiniteLength
	Bottom DefiniteLength
	Left   DefiniteLength
}

// AbsoluteLengthEdges is Edges<AbsoluteLength>.
type AbsoluteLengthEdges struct {
	Top    AbsoluteLength
	Right  AbsoluteLength
	Bottom AbsoluteLength
	Left   AbsoluteLength
}

// LengthSize is Size<Length>.
type LengthSize struct {
	Width  Length
	Height Length
}

// DefiniteLengthSize is Size<DefiniteLength>.
type DefiniteLengthSize struct {
	Width  DefiniteLength
	Height DefiniteLength
}

// ---------------------------------------------------------------------------
// Enums (reference source order; the ABI records use the same order
// except Display, which the adapter maps)
// ---------------------------------------------------------------------------

// Display selects the layout strategy (reference style.rs Display; the
// variant order is the reference source order).
type Display uint8

const (
	// DisplayBlock arranges children with the block layout algorithm.
	DisplayBlock Display = iota
	// DisplayFlex arranges children with the flexbox algorithm (default).
	DisplayFlex
	// DisplayGrid arranges children with the CSS Grid algorithm.
	DisplayGrid
	// DisplayInline contributes to the surrounding paragraph; as a root
	// or flex/grid item it lays out its contents with block flow.
	DisplayInline
	// DisplayInlineFlex is an atomic inline box whose children use flex.
	DisplayInlineFlex
	// DisplayNone hides the element and its children (no space).
	DisplayNone
)

// String returns the reference variant name.
func (d Display) String() string {
	switch d {
	case DisplayBlock:
		return "Block"
	case DisplayFlex:
		return "Flex"
	case DisplayGrid:
		return "Grid"
	case DisplayInline:
		return "Inline"
	case DisplayInlineFlex:
		return "InlineFlex"
	case DisplayNone:
		return "None"
	default:
		return fmt.Sprintf("Display(%d)", uint8(d))
	}
}

// ParseDisplay parses a reference variant name.
func ParseDisplay(s string) (Display, error) {
	switch s {
	case "Block":
		return DisplayBlock, nil
	case "Flex":
		return DisplayFlex, nil
	case "Grid":
		return DisplayGrid, nil
	case "Inline":
		return DisplayInline, nil
	case "InlineFlex":
		return DisplayInlineFlex, nil
	case "None":
		return DisplayNone, nil
	default:
		return 0, fmt.Errorf("invalid Display %q", s)
	}
}

// Position is the positioning strategy (reference style.rs Position;
// Relative is the default, unlike CSS).
type Position uint8

const (
	// PositionRelative offsets from the final layout position (default).
	PositionRelative Position = iota
	// PositionAbsolute offsets from the closest positioned ancestor.
	PositionAbsolute
)

// String returns the reference variant name.
func (p Position) String() string {
	if p == PositionAbsolute {
		return "Absolute"
	}
	if p == PositionRelative {
		return "Relative"
	}
	return fmt.Sprintf("Position(%d)", uint8(p))
}

// ParsePosition parses a reference variant name.
func ParsePosition(s string) (Position, error) {
	switch s {
	case "Relative":
		return PositionRelative, nil
	case "Absolute":
		return PositionAbsolute, nil
	default:
		return 0, fmt.Errorf("invalid Position %q", s)
	}
}

// Overflow controls how overflowing children affect layout (reference
// style.rs Overflow; source order Visible, Clip, Hidden, Scroll).
type Overflow uint8

const (
	// OverflowVisible lets overflow contribute to the parent scroll region.
	OverflowVisible Overflow = iota
	// OverflowClip keeps overflow out of the parent scroll region.
	OverflowClip
	// OverflowHidden zeroes the automatic minimum size and clips.
	OverflowHidden
	// OverflowScroll reserves scrollbar space as well.
	OverflowScroll
)

// String returns the reference variant name.
func (o Overflow) String() string {
	switch o {
	case OverflowVisible:
		return "Visible"
	case OverflowClip:
		return "Clip"
	case OverflowHidden:
		return "Hidden"
	case OverflowScroll:
		return "Scroll"
	default:
		return fmt.Sprintf("Overflow(%d)", uint8(o))
	}
}

// ParseOverflow parses a reference variant name.
func ParseOverflow(s string) (Overflow, error) {
	switch s {
	case "Visible":
		return OverflowVisible, nil
	case "Clip":
		return OverflowClip, nil
	case "Hidden":
		return OverflowHidden, nil
	case "Scroll":
		return OverflowScroll, nil
	default:
		return 0, fmt.Errorf("invalid Overflow %q", s)
	}
}

// FlexDirection is the flexbox main axis (reference style.rs; source
// order Row, Column, RowReverse, ColumnReverse; Row is the default).
type FlexDirection uint8

const (
	FlexDirectionRow FlexDirection = iota
	FlexDirectionColumn
	FlexDirectionRowReverse
	FlexDirectionColumnReverse
)

// String returns the reference variant name.
func (d FlexDirection) String() string {
	switch d {
	case FlexDirectionRow:
		return "Row"
	case FlexDirectionColumn:
		return "Column"
	case FlexDirectionRowReverse:
		return "RowReverse"
	case FlexDirectionColumnReverse:
		return "ColumnReverse"
	default:
		return fmt.Sprintf("FlexDirection(%d)", uint8(d))
	}
}

// ParseFlexDirection parses a reference variant name.
func ParseFlexDirection(s string) (FlexDirection, error) {
	switch s {
	case "Row":
		return FlexDirectionRow, nil
	case "Column":
		return FlexDirectionColumn, nil
	case "RowReverse":
		return FlexDirectionRowReverse, nil
	case "ColumnReverse":
		return FlexDirectionColumnReverse, nil
	default:
		return 0, fmt.Errorf("invalid FlexDirection %q", s)
	}
}

// FlexWrap controls wrapping (reference style.rs; source order NoWrap,
// Wrap, WrapReverse; NoWrap is the default).
type FlexWrap uint8

const (
	FlexWrapNoWrap FlexWrap = iota
	FlexWrapWrap
	FlexWrapWrapReverse
)

// String returns the reference variant name.
func (w FlexWrap) String() string {
	switch w {
	case FlexWrapNoWrap:
		return "NoWrap"
	case FlexWrapWrap:
		return "Wrap"
	case FlexWrapWrapReverse:
		return "WrapReverse"
	default:
		return fmt.Sprintf("FlexWrap(%d)", uint8(w))
	}
}

// ParseFlexWrap parses a reference variant name.
func ParseFlexWrap(s string) (FlexWrap, error) {
	switch s {
	case "NoWrap":
		return FlexWrapNoWrap, nil
	case "Wrap":
		return FlexWrapWrap, nil
	case "WrapReverse":
		return FlexWrapWrapReverse, nil
	default:
		return 0, fmt.Errorf("invalid FlexWrap %q", s)
	}
}

// AlignItems aligns children in the cross axis (reference style.rs
// AlignItems; AlignSelf and JustifyItems are aliases of the same type).
// Source order: Start, End, FlexStart, FlexEnd, Center, Baseline, Stretch.
type AlignItems uint8

const (
	AlignItemsStart AlignItems = iota
	AlignItemsEnd
	AlignItemsFlexStart
	AlignItemsFlexEnd
	AlignItemsCenter
	AlignItemsBaseline
	AlignItemsStretch
)

// String returns the reference variant name.
func (a AlignItems) String() string {
	switch a {
	case AlignItemsStart:
		return "Start"
	case AlignItemsEnd:
		return "End"
	case AlignItemsFlexStart:
		return "FlexStart"
	case AlignItemsFlexEnd:
		return "FlexEnd"
	case AlignItemsCenter:
		return "Center"
	case AlignItemsBaseline:
		return "Baseline"
	case AlignItemsStretch:
		return "Stretch"
	default:
		return fmt.Sprintf("AlignItems(%d)", uint8(a))
	}
}

// ParseAlignItems parses a reference variant name.
func ParseAlignItems(s string) (AlignItems, error) {
	switch s {
	case "Start":
		return AlignItemsStart, nil
	case "End":
		return AlignItemsEnd, nil
	case "FlexStart":
		return AlignItemsFlexStart, nil
	case "FlexEnd":
		return AlignItemsFlexEnd, nil
	case "Center":
		return AlignItemsCenter, nil
	case "Baseline":
		return AlignItemsBaseline, nil
	case "Stretch":
		return AlignItemsStretch, nil
	default:
		return 0, fmt.Errorf("invalid AlignItems %q", s)
	}
}

// AlignContent distributes space between and around content items
// (reference style.rs AlignContent; JustifyContent is the same type).
// Source order: Start, End, FlexStart, FlexEnd, Center, Stretch,
// SpaceBetween, SpaceEvenly, SpaceAround.
type AlignContent uint8

const (
	AlignContentStart AlignContent = iota
	AlignContentEnd
	AlignContentFlexStart
	AlignContentFlexEnd
	AlignContentCenter
	AlignContentStretch
	AlignContentSpaceBetween
	AlignContentSpaceEvenly
	AlignContentSpaceAround
)

// String returns the reference variant name.
func (a AlignContent) String() string {
	switch a {
	case AlignContentStart:
		return "Start"
	case AlignContentEnd:
		return "End"
	case AlignContentFlexStart:
		return "FlexStart"
	case AlignContentFlexEnd:
		return "FlexEnd"
	case AlignContentCenter:
		return "Center"
	case AlignContentStretch:
		return "Stretch"
	case AlignContentSpaceBetween:
		return "SpaceBetween"
	case AlignContentSpaceEvenly:
		return "SpaceEvenly"
	case AlignContentSpaceAround:
		return "SpaceAround"
	default:
		return fmt.Sprintf("AlignContent(%d)", uint8(a))
	}
}

// ParseAlignContent parses a reference variant name.
func ParseAlignContent(s string) (AlignContent, error) {
	switch s {
	case "Start":
		return AlignContentStart, nil
	case "End":
		return AlignContentEnd, nil
	case "FlexStart":
		return AlignContentFlexStart, nil
	case "FlexEnd":
		return AlignContentFlexEnd, nil
	case "Center":
		return AlignContentCenter, nil
	case "Stretch":
		return AlignContentStretch, nil
	case "SpaceBetween":
		return AlignContentSpaceBetween, nil
	case "SpaceEvenly":
		return AlignContentSpaceEvenly, nil
	case "SpaceAround":
		return AlignContentSpaceAround, nil
	default:
		return 0, fmt.Errorf("invalid AlignContent %q", s)
	}
}

// ---------------------------------------------------------------------------
// Grid templates and placement (reference style.rs / geometry.rs)
// ---------------------------------------------------------------------------

// GridTemplateMinSize is the minimum size of a grid row or column
// (reference GridTemplateMinSize; source order Zero, MinContent,
// MaxContent).
type GridTemplateMinSize uint8

const (
	// GridTemplateMinZero allows the track size to shrink to 0.
	GridTemplateMinZero GridTemplateMinSize = iota
	// GridTemplateMinMinContent sizes the track by min-content.
	GridTemplateMinMinContent
	// GridTemplateMinMaxContent sizes the track by max-content.
	GridTemplateMinMaxContent
)

// GridTemplate is the simplified grid-template-* value (reference
// GridTemplate): repeat(<repeat>, minmax(<min>, 1fr)).
type GridTemplate struct {
	// Repeat is how many times the template directive repeats.
	Repeat uint16
	// MinSize is the minimum in the minmax equation.
	MinSize GridTemplateMinSize
}

// GridPlacementKind selects a grid placement form (reference GridPlacement:
// Line, Span, Auto).
type GridPlacementKind uint8

const (
	// GridPlacementAuto places the item automatically (Span(1)).
	GridPlacementAuto GridPlacementKind = iota
	// GridPlacementLine places the item at a grid line index (i16).
	GridPlacementLine
	// GridPlacementSpan spans the given number of lines (u16).
	GridPlacementSpan
)

// GridPlacement is one end of a grid axis placement (reference
// GridPlacement: Line(i16) | Span(u16) | Auto).
type GridPlacement struct {
	// Kind selects auto, line or span.
	Kind GridPlacementKind
	// Value is the line index (GridPlacementLine) or the span
	// (GridPlacementSpan); ignored for auto.
	Value uint32
}

// AutoGridPlacement returns the automatic placement.
func AutoGridPlacement() GridPlacement { return GridPlacement{Kind: GridPlacementAuto} }

// LineGridPlacement returns a line placement from a 1-based CSS line
// index (negative counts from the end), mirroring the reference
// GridPlacement::Line usage.
func LineGridPlacement(line int16) GridPlacement {
	return GridPlacement{Kind: GridPlacementLine, Value: uint32(uint16(line))}
}

// SpanGridPlacement returns a span placement (span must be >= 1 and fit
// u16).
func SpanGridPlacement(span uint16) GridPlacement {
	return GridPlacement{Kind: GridPlacementSpan, Value: uint32(span)}
}

// GridPlacementRange is one axis of a grid location (reference
// Range<GridPlacement>: start and end).
type GridPlacementRange struct {
	Start GridPlacement
	End   GridPlacement
}

// GridLocation places an item in a grid (reference GridLocation: row and
// column ranges).
type GridLocation struct {
	Row    GridPlacementRange
	Column GridPlacementRange
}

// VerticalAlign is the vertical alignment of an inline-level box
// within its line (reference style.rs VerticalAlign; Baseline is the
// default).
type VerticalAlign uint8

const (
	// VerticalAlignBaseline aligns the box's bottom with the text
	// baseline.
	VerticalAlignBaseline VerticalAlign = iota
	// VerticalAlignTop aligns the box's top with the line's top.
	VerticalAlignTop
	// VerticalAlignBottom aligns the box's bottom with the line's
	// bottom.
	VerticalAlignBottom
	// VerticalAlignMiddle centers the box on the text's x-height
	// midpoint.
	VerticalAlignMiddle
)

// String renders the alignment like the reference Debug impl.
func (v VerticalAlign) String() string {
	switch v {
	case VerticalAlignBaseline:
		return "Baseline"
	case VerticalAlignTop:
		return "Top"
	case VerticalAlignBottom:
		return "Bottom"
	case VerticalAlignMiddle:
		return "Middle"
	default:
		return fmt.Sprintf("VerticalAlign(%d)", uint8(v))
	}
}

// ParseVerticalAlign parses a reference variant name.
func ParseVerticalAlign(s string) (VerticalAlign, error) {
	switch s {
	case "Baseline":
		return VerticalAlignBaseline, nil
	case "Top":
		return VerticalAlignTop, nil
	case "Bottom":
		return VerticalAlignBottom, nil
	case "Middle":
		return VerticalAlignMiddle, nil
	default:
		return 0, fmt.Errorf("invalid VerticalAlign %q", s)
	}
}

// ---------------------------------------------------------------------------
// Style
// ---------------------------------------------------------------------------

// Style is the layout style of an element: exactly the fields the pinned
// adapter forwards to Taffy (crates/gpui/src/taffy.rs::into_taffy_style),
// with the reference's field semantics. See DefaultStyle for the
// reference defaults.
type Style struct {
	// Display selects the layout algorithm. Inline maps to Block and
	// InlineFlex to Flex at the adapter (the reference preserves their
	// inline semantics outside Taffy).
	Display Display

	// Position selects the positioning strategy.
	Position Position

	// OverflowX and OverflowY control how overflowing children affect
	// layout on each axis.
	OverflowX Overflow
	OverflowY Overflow

	// ScrollbarWidth reserves space for scrollbars of Overflow::Scroll
	// nodes (reference: AbsoluteLength, default px(0)).
	ScrollbarWidth AbsoluteLength

	// Inset tweaks the position relative to the layout-defined position
	// (reference: Edges<Length>, default auto on every edge).
	Inset LengthEdges

	// Size, MinSize and MaxSize control the item's size
	// (reference: Size<Length>, default auto).
	Size    LengthSize
	MinSize LengthSize
	MaxSize LengthSize

	// AspectRatio is the preferred width/height ratio (default absent).
	AspectRatio *float32

	// Margin is Edges<Length> (percent allowed, default zero).
	Margin LengthEdges
	// Padding is Edges<DefiniteLength> (percent allowed, default zero).
	Padding DefiniteLengthEdges
	// BorderWidths is Edges<AbsoluteLength> (default zero). Converted
	// with the pinned stroke rule: exact zero stays zero, otherwise at
	// least one device pixel.
	BorderWidths AbsoluteLengthEdges

	// AlignItems aligns children in the cross axis (default absent).
	AlignItems *AlignItems
	// AlignSelf overrides the parent's AlignItems (default absent).
	AlignSelf *AlignItems
	// AlignContent distributes content in the cross axis (default absent).
	AlignContent *AlignContent
	// JustifyContent distributes content in the main axis (default
	// absent).
	JustifyContent *AlignContent

	// Gap is the space between items (reference: Size<DefiniteLength>,
	// default zero).
	Gap DefiniteLengthSize

	// FlexDirection is the main axis (default Row).
	FlexDirection FlexDirection
	// FlexWrap is the wrapping mode (default NoWrap).
	FlexWrap FlexWrap
	// FlexBasis is the initial main-axis size (default auto).
	FlexBasis Length
	// FlexGrow is the relative growth rate (default 0; always forwarded,
	// an explicit 0 is not "absent").
	FlexGrow float32
	// FlexShrink is the relative shrink rate (default 1; always
	// forwarded, an explicit 0 is a real override).
	FlexShrink float32

	// GridCols is the column template (grid-template-columns).
	GridCols *GridTemplate
	// GridRows is the row template (grid-template-rows).
	GridRows *GridTemplate
	// GridLocation places the item in its parent grid.
	GridLocation *GridLocation

	// VerticalAlign is the vertical alignment of this box when it
	// participates in an inline line (reference Style::vertical_align;
	// the adapter records it per node and the inline layout consumes
	// it. Baseline by default; NOT forwarded to Taffy).
	VerticalAlign VerticalAlign
}

// DefaultStyle returns the reference Style::default() for the forwarded
// subset: Flex display, Relative position, Visible overflow, zero
// scrollbar width, auto insets/sizes/min/max/flex-basis, zero margins,
// padding and borders, no alignment overrides, zero gap, Row direction,
// NoWrap, grow 0, shrink 1, and no grid properties.
func DefaultStyle() Style {
	zeroAbs := AbsoluteLength{Kind: AbsoluteLengthPx, Value: 0}
	zeroDefinite := DefiniteLength{Kind: DefiniteLengthPx, Value: 0}
	zeroLen := Length{Definite: zeroDefinite}
	return Style{
		Display:        DisplayFlex,
		Position:       PositionRelative,
		OverflowX:      OverflowVisible,
		OverflowY:      OverflowVisible,
		ScrollbarWidth: zeroAbs,
		Inset:          LengthEdges{Top: AutoLength(), Right: AutoLength(), Bottom: AutoLength(), Left: AutoLength()},
		Size:           LengthSize{Width: AutoLength(), Height: AutoLength()},
		MinSize:        LengthSize{Width: AutoLength(), Height: AutoLength()},
		MaxSize:        LengthSize{Width: AutoLength(), Height: AutoLength()},
		Margin:         LengthEdges{Top: zeroLen, Right: zeroLen, Bottom: zeroLen, Left: zeroLen},
		Padding:        DefiniteLengthEdges{Top: zeroDefinite, Right: zeroDefinite, Bottom: zeroDefinite, Left: zeroDefinite},
		BorderWidths:   AbsoluteLengthEdges{Top: zeroAbs, Right: zeroAbs, Bottom: zeroAbs, Left: zeroAbs},
		Gap:            DefiniteLengthSize{Width: zeroDefinite, Height: zeroDefinite},
		FlexDirection:  FlexDirectionRow,
		FlexWrap:       FlexWrapNoWrap,
		FlexBasis:      AutoLength(),
		FlexGrow:       0.0,
		FlexShrink:     1.0,
		VerticalAlign:  VerticalAlignBaseline,
	}
}
