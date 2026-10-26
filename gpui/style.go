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
	// DefiniteLengthNormal is the CSS `normal` keyword, valid only as a
	// line height (browser-parity spec D1): not a length at all but the
	// font-derived value the text system computes (the rounded ascent,
	// descent and line gap of the resolved font, Chromium's
	// FontMetrics::LineSpacing). The reference DefiniteLength has no such
	// variant — the pin's default line height is the golden-ratio
	// fraction, and the port kept that until D1. It is constructed only
	// through NormalLineHeight; every length property (width, padding,
	// gap, …) keeps taking Px/Rem/Fraction values, and resolving a
	// normal length as a plain length fails loudly (LineHeightPixels
	// panics, DefiniteLength.Absolute panics).
	DefiniteLengthNormal
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
// variant) and for the CSS `normal` line-height keyword (which is no
// length).
func (d DefiniteLength) Absolute() AbsoluteLength {
	if d.Kind == DefiniteLengthFraction {
		panic("gpui: DefiniteLength.Absolute on a relative length")
	}
	if d.Kind == DefiniteLengthNormal {
		panic("gpui: DefiniteLength.Absolute on the CSS normal line height")
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
// ("42px", "1.5rem", "50%", "normal").
func (d DefiniteLength) String() string {
	switch d.Kind {
	case DefiniteLengthFraction:
		return trimFloat(d.Value*100) + "%"
	case DefiniteLengthRem:
		return trimFloat(d.Value) + "rem"
	case DefiniteLengthNormal:
		return "normal"
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
	// DisplayBlock arranges children with the block layout algorithm
	// (the default — browser-core ticket 71, spec D6: a plain div stacks
	// its children and an auto width fills the containing block, like
	// HTML; a divergence toward CSS from the pin, whose default is
	// Flex).
	DisplayBlock Display = iota
	// DisplayFlex arranges children with the flexbox algorithm (the
	// pin's default; state it explicitly since browser-core 71).
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
	// DisplayContents is browser-core ticket 36 (spec story 12): the
	// element generates no box of its own; its children splice into the
	// parent's layout as if they were the parent's direct children
	// (css-display-3 §3.1: contents "the element itself does not
	// generate a box, but its children... are rendered as if they were
	// children of the element's parent"). Chromium skips the layout
	// object entirely (LayoutObject::CreateObject's switch returns
	// nullptr for EDisplay::kContents, core/layout/layout_object.cc:
	// 430-432) and splices the children through the layout-tree
	// builder's sibling walk (NextLayoutSiblingInternal, core/dom/
	// layout_tree_builder_traversal.cc:604-634). The port splices at
	// request layout (contents.go).
	DisplayContents
	// DisplayInlineBlock is browser-core ticket 36 (spec story 12): an
	// atomic inline-level box whose contents use block flow
	// (css-display-3: display: block flow, inline-level; Chromium
	// creates a LayoutBlockFlow for it like a block — core/layout/
	// layout_object.cc:437-442 — and it participates in the parent's
	// inline layout as an atomic box). Like the pin's InlineFlex, Taffy
	// lays its contents out with Block layout while the port's inline
	// collector measures it as one atomic box (inline.go).
	DisplayInlineBlock
	// DisplayInlineGrid is browser-core ticket 36 (spec story 12): an
	// atomic inline-level box whose children use grid (Chromium's
	// EDisplay::kInlineGrid creates a LayoutGrid, layout_object.cc:
	// 470-472). The InlineBlock rule with grid contents.
	DisplayInlineGrid
	// DisplayFlowRoot is browser-core ticket 36 (spec story 12): the
	// element establishes an independent block formatting context while
	// remaining a block-level box (css-display-3). The native ABI
	// already carries Taffy's Display::FlowRoot (rev 15, LAYOUT_ABI.md
	// display tag 4); ticket 39 landed the record field, this exposes
	// it on the Style.
	DisplayFlowRoot
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
	case DisplayContents:
		return "Contents"
	case DisplayInlineBlock:
		return "InlineBlock"
	case DisplayInlineGrid:
		return "InlineGrid"
	case DisplayFlowRoot:
		return "FlowRoot"
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
	case "Contents":
		return DisplayContents, nil
	case "InlineBlock":
		return DisplayInlineBlock, nil
	case "InlineGrid":
		return DisplayInlineGrid, nil
	case "FlowRoot":
		return DisplayFlowRoot, nil
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
	// PositionFixed is placed relative to the window root (browser-core
	// 35, spec L10): the containing block of a fixed element is the
	// viewport — LayoutObject::ContainerForFixedPosition walks PAST
	// every positioned ancestor to the LayoutView
	// (core/layout/layout_object.cc:1885-1892), because nothing but a
	// transform, filter or containment makes an ancestor a fixed
	// container (ComputeIsFixedContainer, layout_object.cc:2009-2067,
	// and the port has no transforms). The record translation maps it
	// onto Taffy's Absolute (out of flow, insets kept); the containing
	// block is decided Go-side (gpui/containingblock.go defers the node
	// into the root scope) and the draw defers above the whole in-flow
	// scene (gpui/fixed.go).
	PositionFixed
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
	if p == PositionFixed {
		return "Fixed"
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
	case "Fixed":
		return PositionFixed, nil
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
	// OverflowAuto is browser-core ticket 36 (spec story 14): CSS
	// overflow: auto — a scroll container whose scrollbar (and the
	// layout space it reserves) appears on an axis only while that
	// axis' content overflows. Chromium decides existence after layout
	// (PaintLayerScrollableArea::ComputeScrollbarExistence's kAuto arm:
	// needs_vertical_scrollbar = HasVerticalOverflow() && ..., core/
	// paint/paint_layer_scrollable_area.cc:1852-1861) and relayouts when
	// an auto scrollbar appears or disappears (UpdateAfterOverflowRecalc
	// -> SetNeedsLayoutAndFullPaintInvalidation, :1480-1500). The port
	// resolves the reservation one frame after the overflow changes
	// (scrollbar.go).
	OverflowAuto
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
	case OverflowAuto:
		return "Auto"
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
	case "Auto":
		return OverflowAuto, nil
	default:
		return 0, fmt.Errorf("invalid Overflow %q", s)
	}
}

// ScrollbarGutter selects the scrollbar-gutter policy of a scroll
// container (CSS overflow-5 §scrollbar-gutter; browser-core ticket 36,
// spec story 14): the default auto reserves space only while a
// scrollbar is shown, so content shifts when one appears; stable
// always reserves the gutter on the inline-end side so nothing shifts.
// Chromium: LayoutBox::HasScrollbarGutters (core/layout/layout_box.cc:
// 1477-1503 — the property applies when overflow is auto, scroll or
// hidden and scrollbars are non-overlay) and ComputeScrollbarsInternal
// (:1505-1560 — the stable gutter reserves the hypothetical scrollbar
// thickness on the inline-end side).
type ScrollbarGutter uint8

const (
	// ScrollbarGutterAuto reserves nothing while no scrollbar shows.
	ScrollbarGutterAuto ScrollbarGutter = iota
	// ScrollbarGutterStable always reserves the scrollbar gutter.
	ScrollbarGutterStable
)

// String returns the CSS keyword.
func (g ScrollbarGutter) String() string {
	if g == ScrollbarGutterStable {
		return "Stable"
	}
	return "Auto"
}

// ParseScrollbarGutter parses a variant name.
func ParseScrollbarGutter(s string) (ScrollbarGutter, error) {
	switch s {
	case "Auto":
		return ScrollbarGutterAuto, nil
	case "Stable":
		return ScrollbarGutterStable, nil
	default:
		return 0, fmt.Errorf("invalid ScrollbarGutter %q", s)
	}
}

// OverscrollBehavior selects a scroll container's overscroll-behavior
// on one axis (CSS overscroll-behavior, browser-core ticket 15, spec
// story 15): auto is the default — a scroller that reaches its end
// lets the remaining scroll chain to its ancestor scrollers (and
// allows the user agent's overscroll affordance); contain stops the
// chaining at this scroller (the affordance may still show); none
// stops the chaining AND the affordance. The port has no overscroll
// glow, so contain and none behave identically here; the values stay
// distinct because the class layer spells both.
//
// Chromium: cc/input/overscroll_behavior.h PropagatesX/YScroll (auto
// propagates, contain/none do not) and InputHandler::FindNodeToLatch's
// CanPropagate arm (cc/input/input_handler.cc:2336-2338, :2362-2378 —
// the walk up the scroll tree stops at the first non-propagating
// scroller, latching to it); the keyboard chain is cut the same way
// (core/input/scroll_manager.cc RecomputeScrollChain :166-213, behind
// the stable RespectOverscrollBehaviorForScrollBubbling feature).
type OverscrollBehavior uint8

const (
	// OverscrollBehaviorAuto lets the scroll chain continue past this
	// scroller (the CSS default).
	OverscrollBehaviorAuto OverscrollBehavior = iota
	// OverscrollBehaviorContain stops the scroll chain at this
	// scroller.
	OverscrollBehaviorContain
	// OverscrollBehaviorNone stops the scroll chain and the local
	// overscroll affordance.
	OverscrollBehaviorNone
)

// String returns the CSS keyword.
func (b OverscrollBehavior) String() string {
	switch b {
	case OverscrollBehaviorContain:
		return "Contain"
	case OverscrollBehaviorNone:
		return "None"
	}
	return "Auto"
}

// ParseOverscrollBehavior parses a variant name.
func ParseOverscrollBehavior(s string) (OverscrollBehavior, error) {
	switch s {
	case "Auto":
		return OverscrollBehaviorAuto, nil
	case "Contain":
		return OverscrollBehaviorContain, nil
	case "None":
		return OverscrollBehaviorNone, nil
	default:
		return 0, fmt.Errorf("invalid OverscrollBehavior %q", s)
	}
}

// propagatesScroll reports whether this axis' overscroll-behavior
// lets an unconsumed scroll chain to the ancestor scrollers
// (cc/input/overscroll_behavior.h PropagatesXScroll/PropagatesYScroll:
// auto propagates; contain and none do not).
func (b OverscrollBehavior) propagatesScroll() bool {
	return b == OverscrollBehaviorAuto
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
	// nodes (reference: AbsoluteLength, default px(0)). For
	// OverflowAuto it overrides the classic scrollbar width the auto
	// policy would use (0 keeps the classic width; an auto container's
	// scrollbar can only be hidden by removing the overflow, like the
	// pin's Scroll containers).
	ScrollbarWidth AbsoluteLength

	// ScrollbarGutter is the scrollbar-gutter policy of a scroll
	// container (browser-core 36; default Auto).
	ScrollbarGutter ScrollbarGutter

	// OverscrollBehaviorX and OverscrollBehaviorY are the scroller's
	// overscroll-behavior per axis (browser-core 15): whether an
	// unconsumed scroll chains to the ancestor scrollers. Default Auto.
	OverscrollBehaviorX OverscrollBehavior
	OverscrollBehaviorY OverscrollBehavior

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

	// GridColsList is the full column track list (browser-core 42,
	// CSS grid-template-columns beyond the pinned repeat form: fixed
	// lengths, fr fractions, auto and the content keywords, per track).
	// When set, it takes the place of GridCols — the pinned form stays
	// for compatibility with the reference surface.
	GridColsList []GridTrack
	// GridRowsList is the full row track list (CSS
	// grid-template-rows). When set, it takes the place of GridRows.
	GridRowsList []GridTrack
	// GridAreas is the named area template (CSS
	// grid-template-areas), parsed by ParseGridTemplateAreas.
	GridAreas *GridAreaTemplate
	// GridAreaName places the item by area name (CSS grid-area:
	// <name>); it takes precedence over GridLocation when set.
	GridAreaName string

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
	// Outline is the element's outline — CSS css-ui-4 §outline
	// (browser-core ticket 48): a frame painted ABOVE the content
	// (Chromium's outline phase runs after the foreground,
	// paint_phase.h:46-47), unlike the pin's rings, which paint with
	// the background. The zero value is the CSS initial outline-style:
	// auto, which the :focus-visible UA rule materializes as Chrome's
	// default focus ring (see interact.go's computeStyleInternal);
	// outline-style: none opts out.
	Outline OutlineStyle
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
	// Transform is the element's CSS transform property (browser-core
	// ticket 56): a list of transform functions composed left to right
	// about TransformOrigin against the border box at prepaint/paint.
	// Transforms never affect layout; the paint brackets the element's
	// whole box in a scene effect group under the composed matrix (a
	// translation-only transform shifts the element offset instead),
	// and hit testing maps the pointer through the inverse. Nil (the
	// default) is `none`. A refinement replaces the whole list.
	Transform []TransformFunction
	// TransformOrigin is the CSS transform-origin (browser-core ticket
	// 56), resolved against the border box; nil is the CSS initial
	// 50% 50% (the centre). A percentage origin is a fraction of the
	// border box.
	TransformOrigin *TransformOrigin
	// Debug draws a red outline around the element (style.rs:401; the
	// pinned field is cfg(debug_assertions), the port always carries it).
	Debug bool
	// DebugBelow draws the outline on this element and every conforming
	// descendant (style.rs:405).
	DebugBelow bool
}

// DefaultStyle returns the reference Style::default() for the forwarded
// subset: Block display (browser-core 71, spec D6: the browser's div
// default; the pin's default is Flex — the recorded divergence), Static
// position (browser-core 76, spec D3: the CSS default; the pin's
// default is Relative — the second recorded divergence — so an
// absolute child resolves against the nearest positioned ancestor,
// and insets on a default-position element are ignored), Visible
// overflow, zero scrollbar width, auto insets/sizes/
// min/max/flex-basis, zero margins, padding and borders, no alignment
// overrides, zero gap, Row direction, NoWrap, grow 0, shrink 1, and no grid
// properties.
func DefaultStyle() Style {
	zeroAbs := AbsoluteLength{Kind: AbsoluteLengthPx, Value: 0}
	zeroDefinite := DefiniteLength{Kind: DefiniteLengthPx, Value: 0}
	zeroLen := Length{Definite: zeroDefinite}
	return Style{
		Display:             DisplayBlock,
		Position:            PositionStatic,
		OverflowX:           OverflowVisible,
		OverflowY:           OverflowVisible,
		ScrollbarWidth:      zeroAbs,
		ScrollbarGutter:     ScrollbarGutterAuto,
		OverscrollBehaviorX: OverscrollBehaviorAuto,
		OverscrollBehaviorY: OverscrollBehaviorAuto,
		Inset:               LengthEdges{Top: AutoLength(), Right: AutoLength(), Bottom: AutoLength(), Left: AutoLength()},
		Size:                LengthSize{Width: AutoLength(), Height: AutoLength()},
		MinSize:             LengthSize{Width: AutoLength(), Height: AutoLength()},
		MaxSize:             LengthSize{Width: AutoLength(), Height: AutoLength()},
		Margin:              LengthEdges{Top: zeroLen, Right: zeroLen, Bottom: zeroLen, Left: zeroLen},
		Padding:             DefiniteLengthEdges{Top: zeroDefinite, Right: zeroDefinite, Bottom: zeroDefinite, Left: zeroDefinite},
		BorderWidths:        AbsoluteLengthEdges{Top: zeroAbs, Right: zeroAbs, Bottom: zeroAbs, Left: zeroAbs},
		Gap:                 DefiniteLengthSize{Width: zeroDefinite, Height: zeroDefinite},
		FlexDirection:       FlexDirectionRow,
		FlexWrap:            FlexWrapNoWrap,
		FlexBasis:           AutoLength(),
		FlexGrow:            0.0,
		FlexShrink:          1.0,
		VerticalAlign:       VerticalAlignBaseline,
		// The paint defaults (style.rs:1028-1081): visible, zero overflow
		// fade, no background or border color, solid border with the
		// scene's default dash pattern, zero radii, no shadows, rings or
		// filters, an empty text refinement, no cursor and no opacity.
		// The outline default is the CSS initial value pair —
		// outline-style: auto with the medium width (css-ui-4: auto is
		// the style's initial value, and only the :focus-visible UA rule
		// materializes it, interact.go's computeStyleInternal).
		Visibility:         VisibilityVisible,
		OverflowFade:       AbsoluteLengthEdges{Top: zeroAbs, Right: zeroAbs, Bottom: zeroAbs, Left: zeroAbs},
		BorderStyle:        BorderStyleSolid,
		BorderDashedLength: DefaultBorderDashedLength,
		BorderDashedGap:    DefaultBorderDashedGap,
		CornerRadii:        AbsoluteLengthCorners{TopLeft: zeroAbs, TopRight: zeroAbs, BottomRight: zeroAbs, BottomLeft: zeroAbs},
		Outline:            OutlineStyle{Kind: OutlineAuto, Width: DefaultOutlineWidth},
	}
}

// textNodeStyle is the measured text node's style: the default style
// with the display pinned back to Flex. The Text and StyledText nodes
// are measured leaves (no Taffy children), so their display cannot
// change their own layout — a parent paragraph classifies them by their
// published inline content, and Flex and Block behave identically in
// the non-atomic arms — but pinning keeps the nodes exactly at the
// pin's Style::default() while the div default is block (browser-core
// 71, spec D6; the same reasoning pins Svg, List, UniformList, the
// selection layer and the capture surface).
func textNodeStyle() Style {
	style := DefaultStyle()
	style.Display = DisplayFlex
	return style
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

// ---------------------------------------------------------------------------
// The outline (CSS css-ui-4 §outline; browser-core ticket 48)
// ---------------------------------------------------------------------------

// OutlineStyleKind is the outline style (css-ui-4 §outline-style). The
// port carries the kinds its paint path covers: the native scene's
// border records are solid and dashed only (the pinned scene.rs:
// "0 = solid, 1 = dashed"), so dotted, double, groove, ridge, inset
// and outset have no spelling — a recorded limitation (see the
// ticket's Answer). Auto is the focus ring.
type OutlineStyleKind uint8

const (
	// OutlineAuto is outline-style: auto — the CSS initial value
	// (css-ui-4): "not authored". It never paints by itself; the div's
	// style computation materializes it as the focus ring on a focused
	// :focus-visible element — the UA rule :focus-visible { outline:
	// auto 1px -webkit-focus-ring-color } (html.css:1701-1703) — and
	// drops it to none everywhere else (interact.go's
	// computeStyleInternal). Elements that paint a Style without that
	// computation (img, surface, svg) carry Auto untouched and draw
	// nothing.
	OutlineAuto OutlineStyleKind = iota
	// OutlineNone is outline-style: none: no outline ever — the
	// outline-none opt-out of the default focus ring.
	OutlineNone
	// OutlineSolid is outline-style: solid.
	OutlineSolid
	// OutlineDashed is outline-style: dashed.
	OutlineDashed
	// OutlineFocusRing is the materialized default focus ring: the
	// UA rule's auto outline on a focused, focus-visible element,
	// set only by the div's computeStyleInternal. The ring ignores
	// the style's width (like Chromium's hardcoded
	// FocusRingStrokeWidth).
	OutlineFocusRing
)

// String returns the CSS spelling.
func (k OutlineStyleKind) String() string {
	switch k {
	case OutlineAuto:
		return "auto"
	case OutlineNone:
		return "none"
	case OutlineSolid:
		return "solid"
	case OutlineDashed:
		return "dashed"
	case OutlineFocusRing:
		return "auto (focus ring)"
	default:
		return "unknown"
	}
}

// DefaultOutlineWidth is outline-width's medium: 3 logical pixels
// (the medium border width).
const DefaultOutlineWidth float32 = 3.0

// The default focus ring's numbers (Chromium outline_painter.cc @
// 0d4dcd4f, Windows):
//
//   - focusRingStroke 3 (FocusRingStrokeWidth:40-47, zoom 1), the
//     corner-radius floor; the painted stroke is the outer third, 2px
//     (FocusRingOuterStrokeWidth:51-53) — the width the recorded
//     stable Chrome 155 draws (one 2px stroke centred on the border
//     box, 1px inside and 1px outside);
//   - the default colour #101010: -webkit-focus-ring-color as the
//     recorded Chrome 155 resolves it (the browser's
//     renderer-preference focus_ring_color, web_view_impl.cc:3836 →
//     LayoutTheme::SetCustomFocusRingColor; the in-tree default is
//     0xFFE59700, renderer_preferences.h:45 — the amber of the
//     newer two-tone ring, recorded as the port's divergence toward
//     upstream main, not ported while the recorded stable draws the
//     single stroke).
const (
	focusRingStroke float32 = 3.0
	focusRingWidth  float32 = 2.0
	// defaultFocusRingColor is the recorded Chrome's resolved
	// -webkit-focus-ring-color.
	defaultFocusRingColor = 0x101010
)

// OutlineStyle is the CSS outline (css-ui-4 §outline): the width,
// style, colour and offset of a frame painted above the element's
// content. Logical pixels, like the pin's rings.
type OutlineStyle struct {
	// Kind is the outline style (outline-style).
	Kind OutlineStyleKind
	// Width in logical pixels (outline-width); the auto focus ring
	// ignores it.
	Width float32
	// Color is the outline colour (outline-color): currentColor when
	// not explicit — except on the auto focus ring, whose default is
	// the theme's focus ring colour (#101010, the recorded Chrome's
	// -webkit-focus-ring-color).
	Color RingColor
	// Offset in logical pixels (outline-offset): the gap between the
	// border edge and the outline; negative draws it inside the
	// border box (clamped per direction at half the size, css-ui-4
	// §outline-offset, Chromium AdjustedOutlineOffset).
	Offset float32
}

// paintQuads returns the outline's paint: the solid or dashed frame
// quad, the materialized focus ring quad, or nil — the unmaterialized
// auto kind (the CSS initial value, "not authored") paints nothing.
// radii are the element's resolved corner radii, current is the
// current colour, and borderWidths the element's resolved border
// widths (the focus ring insets itself onto a border).
func (o OutlineStyle) paintQuads(bounds Bounds, radii Corners, current Hsla, borderWidths Edges) []PaintQuad {
	switch o.Kind {
	case OutlineSolid, OutlineDashed:
		quad, ok := o.frameQuad(bounds, radii, current)
		if !ok {
			return nil
		}
		return []PaintQuad{quad}
	case OutlineFocusRing:
		return o.focusRingQuads(bounds, radii, borderWidths)
	default:
		return nil
	}
}

// frameQuad is the solid or dashed outline: one border-frame quad
// whose ink is the CSS band [offset, offset+width] around the border
// box. Quad borders paint inward, so the quad is the border box
// outset by offset+width with the radii grown by the same — the
// outerQuad geometry (ComplexOutlinePainter fills between the outer
// path outset offset+width and the inner path outset offset,
// outline_painter.cc:449-524, the radii from ComputeCornerRadii:
// 208-215). A negative offset is clamped per direction at half the
// box so the shape never collapses (AdjustedOutlineOffset:
// 75-79). The radii follow the element's corners grown by
// offset+width, with a zero corner kept sharp (FloatRoundedRect::
// Radii::Outset, float_rounded_rect.cc: "Zero widths and heights of
// radii are kept zero so that sharp corners are still sharp") — a
// square element's outline is square, unlike the focus ring's 3px
// default rounding.
func (o OutlineStyle) frameQuad(bounds Bounds, radii Corners, current Hsla) (PaintQuad, bool) {
	if !(o.Width > 0) {
		return PaintQuad{}, false
	}
	offV, offH := adjustedOutlineOffsets(bounds.Size, o.Offset)
	style := BorderStyleSolid
	if o.Kind == OutlineDashed {
		style = BorderStyleDashed
	}
	grown := addCorners(radii, o.Offset+o.Width)
	grown = Corners{
		TopLeft:     sharpAware(grown.TopLeft, radii.TopLeft),
		TopRight:    sharpAware(grown.TopRight, radii.TopRight),
		BottomRight: sharpAware(grown.BottomRight, radii.BottomRight),
		BottomLeft:  sharpAware(grown.BottomLeft, radii.BottomLeft),
	}
	quad := outlineFrameQuad(bounds, offV+o.Width, offH+o.Width, o.Width,
		grown, o.Color.resolve(current), style)
	quad.CornerRadii = quad.CornerRadii.ConstrainRadiiForQuadSize(quad.Bounds.Size)
	return quad, true
}

// sharpAware clamps a grown corner radius at zero when the element's
// own corner is sharp (FloatRoundedRect::Radii::Outset's zero
// preservation).
func sharpAware(grown, own float32) float32 {
	if own <= 0 || grown < 0 {
		return 0
	}
	return grown
}

// focusRingQuads is the materialized default focus ring (Chromium
// PaintFocusRing, outline_painter.cc): one width-2 border frame —
// the outer third of the 3px focus ring stroke — centred on the
// border box outset by the offset, so the band is [offset-1,
// offset+1]: 1px inside the border edge and 1px outside. The offset
// is the outline's own minus 1 when the element has a border at
// least 1px wide, so the ring hugs it (FocusRingOffset:60-73); the
// corner radii follow the element's radius plus the offset with a
// 3px floor (GetFocusRingCornerRadii:764-781,
// DefaultFocusRingCornerRadius:759-761 — square elements still get
// the 3px rounding, unlike solid outlines).
//
// Divergence, recorded toward the record: Chromium main (the
// research checkout, 158) paints TWO strokes there — the outline
// colour over a scheme-dependent outer colour (the two-tone refresh,
// PaintFocusRing:878-916, light #E59700 under white) — but the
// recorded stable Chrome 155 draws the single stroke, so the port
// does too; re-record when the recorder's Chrome updates past the
// refresh. The colour is the outline colour (the UA rule's
// -webkit-focus-ring-color — #101010 as this Chrome resolves it,
// web_view_impl.cc:3836's renderer-preference override) unless the
// author set one.
func (o OutlineStyle) focusRingQuads(bounds Bounds, radii Corners, borderWidths Edges) []PaintQuad {
	// FocusRingOffset: the outline offset pulled 1px onto a border
	// every side has at least 1px of.
	offset := o.Offset
	if borderWidths.Top >= 1 && borderWidths.Right >= 1 && borderWidths.Bottom >= 1 && borderWidths.Left >= 1 {
		offset -= 1
	}
	// The ring radii: the element's radii grown by the RAW outline
	// offset, each floored at the 3px stroke.
	ringRadii := Corners{
		TopLeft:     max32(radii.TopLeft+o.Offset, focusRingStroke),
		TopRight:    max32(radii.TopRight+o.Offset, focusRingStroke),
		BottomRight: max32(radii.BottomRight+o.Offset, focusRingStroke),
		BottomLeft:  max32(radii.BottomLeft+o.Offset, focusRingStroke),
	}
	color := SolidBackground(Rgb(defaultFocusRingColor))
	if o.Color.Explicit {
		color = o.Color.Color
	}
	// The stroke centres on the border box outset by the offset; quad
	// borders paint inward, so the quad is dilated by half the 2px
	// stroke and its radii grow with it.
	quad := outlineFrameQuad(bounds, offset+focusRingWidth/2, offset+focusRingWidth/2,
		focusRingWidth, addCorners(ringRadii, focusRingWidth/2), color, BorderStyleSolid)
	quad.CornerRadii = quad.CornerRadii.ConstrainRadiiForQuadSize(quad.Bounds.Size)
	return []PaintQuad{quad}
}

// inkOutset is the outline's outside extent (OutlineOutsetExtent,
// outline_painter.cc:1073-1082) — the focus ring's border-adjusted
// offset plus the half stroke outside it, a solid or dashed
// outline's offset plus width floored at zero — which feeds the
// filter group's ink bounds like the ring's width does (the outline
// extent grows VisualOverflowRect, layout_box.cc:3485-3492). The
// unmaterialized auto kind paints nothing and contributes nothing.
func (o OutlineStyle) inkOutset(borderWidths Edges) float32 {
	switch o.Kind {
	case OutlineFocusRing:
		offset := o.Offset
		if borderWidths.Top >= 1 && borderWidths.Right >= 1 && borderWidths.Bottom >= 1 && borderWidths.Left >= 1 {
			offset -= 1
		}
		return max32(offset+focusRingWidth/2, 0)
	case OutlineSolid, OutlineDashed:
		return max32(o.Offset+o.Width, 0)
	default:
		return 0
	}
}

// adjustedOutlineOffsets clamps a negative outline offset per
// direction at half the box (Chromium AdjustedOutlineOffset,
// outline_painter.cc:75-79: max(offset, -height/2) and
// max(offset, -width/2), css-ui-4 §outline-offset's "a negative
// outline-offset should not cause the rendered outline shape to
// become smaller than…" rule).
func adjustedOutlineOffsets(size Size, offset float32) (vertical, horizontal float32) {
	vertical = max32(offset, -size.Height/2)
	horizontal = max32(offset, -size.Width/2)
	return vertical, horizontal
}

// outlineFrameQuad builds one outline frame: a border quad of the
// given width painting inward from bounds outset by the per-axis
// dilations, with the given (already computed) corner radii — the
// RingStyle.outerQuad geometry.
func outlineFrameQuad(bounds Bounds, dilationY, dilationX, width float32, radii Corners, border Background, style BorderStyle) PaintQuad {
	return PaintQuad{
		Bounds: Bounds{
			Origin: Point{X: bounds.Origin.X - dilationX, Y: bounds.Origin.Y - dilationY},
			Size:   Size{Width: bounds.Size.Width + 2*dilationX, Height: bounds.Size.Height + 2*dilationY},
		},
		CornerRadii:        radii,
		Background:         SolidBackground(TransparentBlack()),
		BorderWidths:       Edges{Top: width, Right: width, Bottom: width, Left: width},
		BorderColor:        border,
		BorderStyle:        style,
		BorderDashedLength: DefaultBorderDashedLength,
		BorderDashedGap:    DefaultBorderDashedGap,
	}
}

// addCorners adds v to every corner radius.
func addCorners(c Corners, v float32) Corners {
	return Corners{
		TopLeft:     c.TopLeft + v,
		TopRight:    c.TopRight + v,
		BottomRight: c.BottomRight + v,
		BottomLeft:  c.BottomLeft + v,
	}
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
// borderEdges returns the style's border widths clamp-lined to
// Chromium's computed rule (browser-core ticket 07: the width is an
// integer of CSS px, with sub-pixel hairlines kept at one —
// StyleBuilderConverter::ClampLineWidth, style_builder_converter.cc:
// 1971-1987). This is the border geometry the layout laid the box out
// with, so every style-side arithmetic that must agree with the layout
// (the overflow mask, the content box, the sticky scrollport, the
// scrollbar tracks) resolves through it instead of the raw authored
// widths.
func (s *Style) borderEdges(remSize float32) Edges {
	return Edges{
		Top:    clampLineWidth(s.BorderWidths.Top.ToPixels(remSize)),
		Right:  clampLineWidth(s.BorderWidths.Right.ToPixels(remSize)),
		Bottom: clampLineWidth(s.BorderWidths.Bottom.ToPixels(remSize)),
		Left:   clampLineWidth(s.BorderWidths.Left.ToPixels(remSize)),
	}
}

// pinned fade distances are not carried (the port's content mask has
// no fade component; the fade styles are ticket 57).
func (s *Style) OverflowMask(bounds Bounds, remSize float32) *ClipMask {
	xVisible := s.OverflowX == OverflowVisible
	yVisible := s.OverflowY == OverflowVisible
	if xVisible && yVisible {
		return nil
	}
	minX, minY := bounds.Origin.X, bounds.Origin.Y
	maxX, maxY := bounds.Right(), bounds.Bottom()
	borders := s.borderEdges(remSize)
	minX += borders.Left
	maxX -= borders.Right
	minY += borders.Top
	maxY -= borders.Bottom
	var rect Bounds
	switch {
	case xVisible && !yVisible:
		rect = fromCorners(Point{X: minX, Y: bounds.Origin.Y}, Point{X: maxX, Y: bounds.Bottom()})
	case !xVisible && yVisible:
		rect = fromCorners(Point{X: bounds.Origin.X, Y: minY}, Point{X: bounds.Right(), Y: maxY})
	default:
		rect = fromCorners(Point{X: minX, Y: minY}, Point{X: maxX, Y: maxY})
	}
	mask := ClipMask{Bounds: rect}
	// The rounded corner clip (rev 16, browser-core ticket 55): when
	// BOTH axes clip, the mask carries the INNER border radii - the
	// padding-box corner rounding, Chromium's overflow clip (the
	// ContouredInnerBorder of the border rrect: the outer radii minus
	// the border widths per corner, clamped at zero,
	// core/paint/contoured_border_geometry.cc:209-219, applied through
	// UpdateInnerBorderRadiusClip,
	// core/paint/paint_property_tree_builder.cc:3257-3293). When one
	// axis stays visible the clip region is NOT rounded (css-overflow
	// §corner-clipping: "when one of overflow-x or overflow-y computes
	// to clip and the other computes to visible, the clipping region
	// is not rounded" - NeedsInnerBorderRadiusClip,
	// paint_property_tree_builder.cc:3108-3112). The paint path turns a
	// rounded mask into a rounded_clip_only effect group
	// (group_styles.go); a zero-radii mask stays the plain rectangular
	// clip of every pre-R16 scene.
	if !xVisible && !yVisible {
		radii := s.CornerRadii.ToPixels(remSize).ConstrainRadiiForQuadSize(bounds.Size)
		mask.CornerRadii = Corners{
			TopLeft:     max32(radii.TopLeft-max32(borders.Top, borders.Left), 0),
			TopRight:    max32(radii.TopRight-max32(borders.Top, borders.Right), 0),
			BottomRight: max32(radii.BottomRight-max32(borders.Bottom, borders.Right), 0),
			BottomLeft:  max32(radii.BottomLeft-max32(borders.Bottom, borders.Left), 0),
		}.ConstrainRadiiForQuadSize(rect.Size)
		if s.CornerSmoothing != nil {
			mask.CornerSmoothing = *s.CornerSmoothing
		}
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
	borders := s.borderEdges(remSize)
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
