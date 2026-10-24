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
// crates/gpui/src/style.rs). The layout fields are the ones the pinned
// adapter forwards (crates/gpui/src/taffy.rs::into_taffy_style); ticket
// base-05 adds the paint and interaction fields of the pinned Style
// (style.rs:233-406: visibility, overflow fade, scroll axis flags,
// background, border color/style/dash pattern, corner radii and
// smoothing, box shadows, rings, filters, the text refinement, the
// mouse cursor, opacity and the debug flags). The adapter still forwards
// only the layout subset; the paint fields are consumed by the div's
// Style.paint port (div.go).
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

// Position is the positioning strategy. Relative is the default (the
// reference style.rs Position; unlike CSS, whose default is static).
// PositionStatic is the CSS opt-in of browser-core ticket 33 (spec
// D3): a static element is laid out in flow exactly like a relative
// one (the record translation maps it onto Taffy's Relative, which
// Taffy has no static value), it ignores the insets (CSS static never
// offsets), and — the point of the value — it is NOT a containing
// block: an absolutely positioned child skips it and resolves
// against the nearest positioned ancestor, the way
// LayoutObject::ContainerForAbsolutePosition walks the parent chain
// in Chromium (core/layout/layout_object.cc:1877-1883, the research
// checkout). The default itself flips to static in ticket 76; until
// then existing behaviour is bit-for-bit unchanged unless a view says
// Static().
//
// PositionSticky is browser-core ticket 34 (spec L3): a sticky element
// lays out in flow exactly like a relative one WITHOUT the insets as
// relative offsets (the record translation zeroes them; they are
// scroll constraints, decided at prepaint by gpui/sticky.go), shifts
// at prepaint so it stays inside its scroll container's
// inset-adjusted scrollport, never leaves its flow containing block,
// and — like every positioned value except static — IS a containing
// block for absolute descendants (Chromium's ComputeIsAbsolute-
// Container, position != kStatic).
type Position uint8

const (
	// PositionRelative offsets from the final layout position (default).
	PositionRelative Position = iota
	// PositionAbsolute offsets from the closest positioned ancestor.
	PositionAbsolute
	// PositionStatic lays out in flow and is not a containing block for
	// absolute children (browser-core 33, spec D3; the CSS default,
	// introduced opt-in — the port's default stays Relative until ticket
	// 76 flips it).
	PositionStatic
	// PositionSticky lays out in flow and sticks inside its scroll
	// container at prepaint (browser-core 34, spec L3).
	PositionSticky
)

// String returns the reference variant name.
func (p Position) String() string {
	if p == PositionAbsolute {
		return "Absolute"
	}
	if p == PositionRelative {
		return "Relative"
	}
	if p == PositionStatic {
		return "Static"
	}
	if p == PositionSticky {
		return "Sticky"
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
	case "Static":
		return PositionStatic, nil
	case "Sticky":
		return PositionSticky, nil
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

	// ---- Paint and interaction fields (style.rs:239-405) ----

	// Visibility controls whether the element paints (style.rs:240;
	// Hidden keeps the layout space but skips paint, div.rs:3021).
	Visibility Visibility
	// OverflowFade is the per-edge fade distance of overflow masking
	// (style.rs:248). Stored and refined; the port's content mask has no
	// fade component yet, so it is not applied at paint.
	OverflowFade AbsoluteLengthEdges
	// AllowConcurrentScroll lets both axes scroll at once (style.rs:252).
	AllowConcurrentScroll bool
	// RestrictScrollToAxis locks scrolling to the gesture's axis
	// (style.rs:278).
	RestrictScrollToAxis bool
	// Background is the fill (style.rs:339 Option<Fill>; Fill has the one
	// variant Color(Background), so the port stores the Background).
	Background *Background
	// BorderColor is the background painted into the border
	// (style.rs:342).
	BorderColor *Background
	// BorderStyle is the border style (style.rs:345).
	BorderStyle BorderStyle
	// BorderDashedLength is the dash length as a multiple of the border
	// width (style.rs:348).
	BorderDashedLength float32
	// BorderDashedGap is the dash gap as a multiple of the border width
	// (style.rs:351).
	BorderDashedGap float32
	// CornerRadii are the corner radii (style.rs:355 Corners<AbsoluteLength>).
	CornerRadii AbsoluteLengthCorners
	// CornerSmoothing is the corner smoothing in [0, 1] (style.rs:359).
	CornerSmoothing *float32
	// BoxShadow are the element's shadows (style.rs:362).
	BoxShadow []BoxShadow
	// Ring is the solid ring painted outside the element (style.rs:366).
	Ring RingStyle
	// InsetRing is the solid ring painted inside the element
	// (style.rs:370).
	InsetRing RingStyle
	// Filter applies to the element's own content (style.rs:373).
	Filter []Filter
	// BackdropFilter applies to the content behind the element
	// (style.rs:376).
	BackdropFilter []Filter
	// ObjectFit, when set, overrides a replaced element's (img, surface)
	// own fit option — CSS object-fit (browser-core ticket 41); nil keeps
	// the element's option (img defaults to contain).
	ObjectFit *ObjectFit
	// ObjectPosition positions the fitted content inside the content
	// box — CSS object-position (browser-core ticket 41); nil is the CSS
	// default 50% 50% (centered).
	ObjectPosition *ObjectPosition
	// ImageRendering selects the image sampling quality — CSS
	// image-rendering (browser-core ticket 41); nil is auto (bilinear,
	// the pinned sampler).
	ImageRendering *ImageRendering
	// Text is the text style refinement the element cascades
	// (style.rs:380).
	Text TextStyleRefinement
	// ForcedColorAdjust is the forced-color-adjust property (CSS Color
	// Adjustment 1): Auto (the default) maps the element's colours onto
	// the system palette while the forced-colours pass is active,
	// None opts the element out (browser-core ticket 29; see
	// forced_colors.go).
	ForcedColorAdjust ForcedColorAdjust
	// MouseCursor is the cursor shown over the element (style.rs:383).
	MouseCursor *CursorStyle
	// Opacity is the element opacity (style.rs:386).
	Opacity *float32
	// Transition is the element's transition rules — CSS transition
	// (browser-core ticket 44): when a covered style property's resolved
	// value changes between frames, the change animates over the rule's
	// duration and easing with CSS's reversing rule, driven by the
	// per-element style diff (transition.go). Nil (the default) animates
	// nothing and costs nothing. The rules are NOT inherited and a
	// refinement replaces the whole list.
	Transition []TransitionRule
	// Debug draws a red outline around the element (style.rs:401; the
	// pinned field is cfg(debug_assertions), the port always carries it).
	Debug bool
	// DebugBelow draws the outline on this element and every conforming
	// descendant (style.rs:405).
	DebugBelow bool
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
		// The paint defaults (style.rs:1028-1081): visible, zero overflow
		// fade, no background or border color, solid border with the
		// scene's default dash pattern, zero radii, no shadows, rings or
		// filters, an empty text refinement, no cursor and no opacity.
		Visibility:         VisibilityVisible,
		OverflowFade:       AbsoluteLengthEdges{Top: zeroAbs, Right: zeroAbs, Bottom: zeroAbs, Left: zeroAbs},
		BorderStyle:        BorderStyleSolid,
		BorderDashedLength: DefaultBorderDashedLength,
		BorderDashedGap:    DefaultBorderDashedGap,
		CornerRadii:        AbsoluteLengthCorners{TopLeft: zeroAbs, TopRight: zeroAbs, BottomRight: zeroAbs, BottomLeft: zeroAbs},
	}
}

// ---------------------------------------------------------------------------
// Paint-side style types (style.rs)
// ---------------------------------------------------------------------------

// Visibility is the value of the visibility property (style.rs:421-429;
// Visible is the default).
type Visibility uint8

const (
	// VisibilityVisible draws the element as normal.
	VisibilityVisible Visibility = iota
	// VisibilityHidden skips drawing but keeps the layout space.
	VisibilityHidden
)

// String returns the reference variant name.
func (v Visibility) String() string {
	switch v {
	case VisibilityVisible:
		return "Visible"
	case VisibilityHidden:
		return "Hidden"
	default:
		return fmt.Sprintf("Visibility(%d)", uint8(v))
	}
}

// AbsoluteLengthCorners is Corners<AbsoluteLength> (the Style corner
// radii, style.rs:355).
type AbsoluteLengthCorners struct {
	TopLeft     AbsoluteLength
	TopRight    AbsoluteLength
	BottomRight AbsoluteLength
	BottomLeft  AbsoluteLength
}

// ToPixels resolves the radii against the rem size
// (Corners<AbsoluteLength>::to_pixels).
func (c AbsoluteLengthCorners) ToPixels(remSize float32) Corners {
	return Corners{
		TopLeft:     c.TopLeft.ToPixels(remSize),
		TopRight:    c.TopRight.ToPixels(remSize),
		BottomRight: c.BottomRight.ToPixels(remSize),
		BottomLeft:  c.BottomLeft.ToPixels(remSize),
	}
}

// ToPixels resolves the edges against the rem size
// (Edges<AbsoluteLength>::to_pixels).
func (e AbsoluteLengthEdges) ToPixels(remSize float32) Edges {
	return Edges{
		Top:    e.Top.ToPixels(remSize),
		Right:  e.Right.ToPixels(remSize),
		Bottom: e.Bottom.ToPixels(remSize),
		Left:   e.Left.ToPixels(remSize),
	}
}

// ConstrainRadiiForQuadSize scales all corner radii by one common
// factor when adjacent radii overflow the quad: CSS Backgrounds 3
// §corner-overlap computes f = min(Li/Si) over the four sides, where
// Si is the sum of the radii at the ends of side i (the top side's
// top-left + top-right and the bottom side's bottom-left +
// bottom-right bound the horizontal sums; the left and right sides'
// vertical pairs bound the vertical sums), and multiplies every radius
// by f when f < 1. Chromium: FloatRoundedRect::ConstrainRadii
// (third_party/blink/renderer/platform/geometry/float_rounded_rect.cc:
// 289-305).
//
// Recorded divergence toward CSS: the pin clamps each corner
// independently to half the shortest side
// (geometry.rs:2511-2523 clamp_radii_for_quad_size), which is
// equivalent only when the overflowing radii are equal — on a 40px
// tall box a single 30px radius stays 30 in CSS and becomes 20 under
// the clamp, and a 60/20 pair scales to 40/13.33 in CSS and
// 20/13.33 under the clamp.
func (c Corners) ConstrainRadiiForQuadSize(size Size) Corners {
	factor := float32(1)
	horizontalSum := max32(c.TopLeft+c.TopRight, c.BottomLeft+c.BottomRight)
	if horizontalSum > size.Width {
		factor = min32(size.Width/horizontalSum, factor)
	}
	verticalSum := max32(c.TopLeft+c.BottomLeft, c.TopRight+c.BottomRight)
	if verticalSum > size.Height {
		factor = min32(size.Height/verticalSum, factor)
	}
	if factor == 1 {
		return c
	}
	return Corners{
		TopLeft:     c.TopLeft * factor,
		TopRight:    c.TopRight * factor,
		BottomRight: c.BottomRight * factor,
		BottomLeft:  c.BottomLeft * factor,
	}
}

// OutsetWithCornerCorrection returns the corner radii of this box's
// shape after the box is dilated ("outset") by the given amount,
// following CSS Backgrounds 3 §corner-shaping, the "outset-adjusted
// border radius" (Chromium FloatRoundedRect::Radii::
// OutsetWithCornerCorrection driving ComputeOutsetAdjustedBorderRadius
// and AdjustedRadiusDimension, which quote the spec,
// platform/geometry/float_rounded_rect.cc:131-186 @ 0d4dcd4f, applied
// to box-shadow spread by ApplySpreadToShadowShape,
// core/paint/box_painter_base.cc:90-95). Recorded divergence toward
// CSS (spec decision D8): the pin kept the element's radii unchanged
// under a shadow's spread, so a `0 0 0 4px` halo on an r=8 box had
// 8px corners where CSS gives 12px.
//
// Each radius grows by the outset, EXCEPT when it is smaller than the
// outset: then it grows by the cubic ratio rule instead, so a
// near-square corner does not suddenly become fully rounded, and a
// zero radius stays zero (a square corner stays square). The size is
// the ORIGINAL box's size — the coverage ratio is taken before the
// outset (Chromium captures rect size before outsetting). A negative
// outset (an inset) simply shrinks the radius by its magnitude,
// clamped at zero. Pair the result with ConstrainRadiiForQuadSize on
// the dilated box (Chromium's ApplySpreadToShadowShape calls
// ConstrainRadii right after the outset).
func (c Corners) OutsetWithCornerCorrection(size Size, outset float32) Corners {
	if outset == 0 || size.Width <= 0 || size.Height <= 0 {
		return c
	}
	// coverage = 2 * min(r / edge_width, r / edge_height), the corner's
	// share of the box (float_rounded_rect.cc:157-159).
	adjust := func(radius float32) float32 {
		if radius == 0 {
			return 0
		}
		coverage := 2 * min32(radius/size.Width, radius/size.Height)
		// AdjustedRadiusDimension (float_rounded_rect.cc:138-152):
		// 1. if radius > outset or coverage > 1: radius + outset;
		// 2. ratio = radius / outset;
		// 3. radius + outset * (1 - (1-ratio)^3 * (1-coverage^3)).
		if radius > outset || coverage > 1 {
			return max32(radius+outset, 0)
		}
		ratio := radius / outset
		return max32(radius+outset*(1-(1-ratio)*(1-ratio)*(1-ratio)*(1-coverage*coverage*coverage)), 0)
	}
	return Corners{
		TopLeft:     adjust(c.TopLeft),
		TopRight:    adjust(c.TopRight),
		BottomRight: adjust(c.BottomRight),
		BottomLeft:  adjust(c.BottomLeft),
	}
}

// Filter is a graphical filter applied to an element's own content
// (Styled.Filter, CSS filter) or to the content behind it
// (Styled.BackdropFilter, CSS backdrop-filter): the pinned enum's one
// variant Blur(Pixels) (style.rs:479-486) plus ticket 41's CSS colour
// functions (FilterFunction) as one Filter value each — see
// filter_colors.go for their Chromium-sourced matrices.
type Filter struct {
	// BlurRadius is the gaussian blur radius in logical pixels
	// (FilterFunctionBlur).
	BlurRadius float32
	// Function selects the CSS filter function (FilterFunctionBlur for
	// the pinned blur, the zero value).
	Function FilterFunction
	// Amount is the colour function's argument: the [0,1] fraction for
	// grayscale/sepia/invert/opacity, the unclamped multiplier for
	// saturate/brightness/contrast, or the degrees for hue-rotate.
	// Zero for blur.
	Amount float32
}

// Blur returns the gaussian blur filter (Filter::Blur).
func Blur(radius float32) Filter { return Filter{BlurRadius: radius} }

// IsIdentity reports whether the filter has no visible effect
// (style.rs:495 Filter::is_identity: radius <= 0 for blur; the CSS
// colour functions' no-op amounts: 0 for grayscale/sepia/invert/
// hue-rotate, 1 for saturate/opacity/brightness/contrast).
func (f Filter) IsIdentity() bool {
	switch f.Function {
	case FilterFunctionSaturate, FilterFunctionOpacity, FilterFunctionBrightness, FilterFunctionContrast:
		return f.Amount == 1
	case FilterFunctionGrayscale, FilterFunctionSepia, FilterFunctionInvert, FilterFunctionHueRotate:
		return f.Amount == 0
	default:
		return blurRadiusIsIdentity(f.BlurRadius)
	}
}

// RingColor is a ring's color (style.rs:173-190): CurrentColor (the
// default, the element's effective text color like CSS currentColor) or
// an explicit background.
type RingColor struct {
	// Explicit selects Color; false is CurrentColor.
	Explicit bool
	// Color is the explicit color when Explicit is set.
	Color Background
}

// RingColorOf returns an explicit ring color (RingColor::Color).
func RingColorOf(color Background) RingColor { return RingColor{Explicit: true, Color: color} }

// resolve is RingColor::resolve (style.rs:184).
func (c RingColor) resolve(current Hsla) Background {
	if c.Explicit {
		return c.Color
	}
	return SolidBackground(current)
}

// RingStyle is a ring around or inside an element (style.rs:192-200): a
// zero width disables it.
type RingStyle struct {
	// Width in logical pixels.
	Width float32
	// Color is the ring color.
	Color RingColor
}

// outerQuad is RingStyle::outer_quad (style.rs:203-221): quad borders
// paint inward, so the quad is dilated by the width and its radii grow
// by the width, leaving its transparent interior on the element bounds.
func (r RingStyle) outerQuad(bounds Bounds, radii Corners, current Hsla) (PaintQuad, bool) {
	if !(r.Width > 0) {
		return PaintQuad{}, false
	}
	return PaintQuad{
		Bounds: dilate(bounds, r.Width),
		CornerRadii: Corners{
			TopLeft:     radii.TopLeft + r.Width,
			TopRight:    radii.TopRight + r.Width,
			BottomRight: radii.BottomRight + r.Width,
			BottomLeft:  radii.BottomLeft + r.Width,
		},
		Background:         SolidBackground(TransparentBlack()),
		BorderWidths:       Edges{Top: r.Width, Right: r.Width, Bottom: r.Width, Left: r.Width},
		BorderColor:        r.Color.resolve(current),
		BorderStyle:        BorderStyleSolid,
		BorderDashedLength: DefaultBorderDashedLength,
		BorderDashedGap:    DefaultBorderDashedGap,
	}, true
}

// insetShadow is RingStyle::inset_shadow (style.rs:223-229): a zero
// offset, zero blur inset shadow spread by the width.
func (r RingStyle) insetShadow(current Hsla) (BoxShadow, bool) {
	if !(r.Width > 0) {
		return BoxShadow{}, false
	}
	return NewBoxShadow(0, 0, r.Color.resolve(current)).WithSpreadRadius(r.Width).WithInset(), true
}

// TextOverflowKind selects the truncation position (style.rs:534-547).
type TextOverflowKind uint8

const (
	// TextOverflowTruncate truncates at the end ("very long te…").
	TextOverflowTruncate TextOverflowKind = iota
	// TextOverflowTruncateStart truncates at the start ("…ong text here").
	TextOverflowTruncateStart
	// TextOverflowTruncateMiddle truncates in the middle ("long fi…name.rs").
	TextOverflowTruncateMiddle
)

// TextOverflow is how overflowing text truncates (style.rs:534-547): the
// position plus the string that represents the truncation.
type TextOverflow struct {
	// Kind is the truncation position.
	Kind TextOverflowKind
	// Ellipsis is the truncation marker.
	Ellipsis string
}

// Ellipsis is the pinned truncation marker (styled.rs:16 ELLIPSIS).
const Ellipsis = "…"

// TextTransform is the layout-time case mapping (style.rs:576-588; None
// is the default).
type TextTransform uint8

const (
	// TextTransformNone does not transform text.
	TextTransformNone TextTransform = iota
	// TextTransformUppercase uppercases text.
	TextTransformUppercase
	// TextTransformLowercase lowercases text.
	TextTransformLowercase
	// TextTransformCapitalize uppercases the first letter of each word.
	TextTransformCapitalize
)

// StrikethroughStyle is the style of a strikethrough (style.rs:1099-1107):
// logical pixels; a nil color is the reference None.
type StrikethroughStyle struct {
	// Thickness of the strikethrough.
	Thickness float32
	// Color of the strikethrough.
	Color *Hsla
}

// HasOpaqueBackground reports whether the background is a
// non-transparent color (style.rs:776 Style::has_opaque_background).
func (s *Style) HasOpaqueBackground() bool {
	return s.Background != nil && !s.Background.IsTransparent()
}

// TextStyle returns the element's text refinement, or nil when it
// refines nothing (style.rs:783 Style::text_style).
func (s *Style) TextStyle() *TextStyleRefinement {
	if s.Text.IsSome() {
		return &s.Text
	}
	return nil
}

// OverflowMask returns the content mask of this style for the given
// bounds, or nil when the element does not clip its overflow
// (style.rs:793-850 Style::overflow_mask). The clipped region is
// always inset by the border widths: CSS clips an element's overflow
// at the padding box whatever the border color (Chromium's
// LayoutBox::OverflowClipRect contracts the border box by
// BorderOutsets unconditionally,
// third_party/blink/renderer/core/layout/layout_box.cc:2288-2302), so
// a transparent border that reserves space insets the clip too.
// Recorded divergence toward CSS: the pin insets only when the border
// color is set and opaque (style.rs:812-820).
//
// The pinned axis selection is ported verbatim: with x visible and y
// clipped the mask spans the border-inset x range and the full bounds
// height, and vice versa (the CSS overflow-value computation that
// turns that pair into auto is spec ticket V2's, not this one's). The
// pinned fade distances are not carried (the port's content mask has
// no fade component).
func (s *Style) OverflowMask(bounds Bounds, remSize float32) *Bounds {
	xVisible := s.OverflowX == OverflowVisible
	yVisible := s.OverflowY == OverflowVisible
	if xVisible && yVisible {
		return nil
	}
	minX, minY := bounds.Origin.X, bounds.Origin.Y
	maxX, maxY := bounds.Right(), bounds.Bottom()
	minX += s.BorderWidths.Left.ToPixels(remSize)
	maxX -= s.BorderWidths.Right.ToPixels(remSize)
	minY += s.BorderWidths.Top.ToPixels(remSize)
	maxY -= s.BorderWidths.Bottom.ToPixels(remSize)
	var mask Bounds
	switch {
	case xVisible && !yVisible:
		mask = fromCorners(Point{X: minX, Y: bounds.Origin.Y}, Point{X: maxX, Y: bounds.Bottom()})
	case !xVisible && yVisible:
		mask = fromCorners(Point{X: bounds.Origin.X, Y: minY}, Point{X: bounds.Right(), Y: maxY})
	default:
		mask = fromCorners(Point{X: minX, Y: minY}, Point{X: maxX, Y: maxY})
	}
	return &mask
}

// ContentBox returns the element's content box: the bounds inset by
// the border widths and the padding, clamped at zero size (CSS: the
// content box; Chromium's PhysicalContentBoxRect contracts the border
// box by borders and padding,
// third_party/blink/renderer/core/layout/layout_box.cc:3848-3856).
// Padding fractions resolve against the element's own width (CSS's
// all-edges-against-width rule, but against the element's box instead
// of the containing block — the paint-time approximation, exact for
// the px and rem padding an object-fit style uses; the layout engine
// resolves layout-time percentages itself).
func (s *Style) ContentBox(bounds Bounds, remSize float32) Bounds {
	borders := s.BorderWidths.ToPixels(remSize)
	insets := Edges{
		Top:    borders.Top + definiteEdgePixels(s.Padding.Top, bounds.Size.Width, remSize),
		Right:  borders.Right + definiteEdgePixels(s.Padding.Right, bounds.Size.Width, remSize),
		Bottom: borders.Bottom + definiteEdgePixels(s.Padding.Bottom, bounds.Size.Width, remSize),
		Left:   borders.Left + definiteEdgePixels(s.Padding.Left, bounds.Size.Width, remSize),
	}
	width := bounds.Size.Width - insets.Left - insets.Right
	height := bounds.Size.Height - insets.Top - insets.Bottom
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	return Bounds{
		Origin: Point{X: bounds.Origin.X + insets.Left, Y: bounds.Origin.Y + insets.Top},
		Size:   Size{Width: width, Height: height},
	}
}

// definiteEdgePixels resolves one definite padding edge against the
// element's own width (the paint-time convention; see ContentBox).
func definiteEdgePixels(length DefiniteLength, elementWidth, remSize float32) float32 {
	switch length.Kind {
	case DefiniteLengthPx:
		return length.Value
	case DefiniteLengthRem:
		return length.Value * remSize
	default: // DefiniteLengthFraction
		return length.Value * max32(elementWidth, 0)
	}
}

// isBorderVisible is style.rs:1017 Style::is_border_visible.
func (s *Style) isBorderVisible() bool {
	if s.BorderColor == nil || s.BorderColor.IsTransparent() {
		return false
	}
	w := s.BorderWidths
	return !w.Top.IsZero() || !w.Right.IsZero() || !w.Bottom.IsZero() || !w.Left.IsZero()
}
