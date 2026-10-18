package gpui

//go:generate go run ../cmd/stylegen -out styled_generated.go

import "slices"

// This file ports the pinned Styled trait (ticket base-05; reference
// crates/gpui/src/styled.rs at 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a)
// as a generic mixin. Rust supplies the trait's default methods to any
// type implementing `fn style(&mut self) -> &mut StyleRefinement`; Go
// supplies them through a value-embedded Styled[Self] bound once to its
// owner (the authoring design of research/09-fluent-authoring-methods.md),
// so every method returns the owner's concrete type.
//
// The methods written by hand here are styled.rs's own; the families the
// pinned gpui_macros/src/styles.rs expands (style_helpers!, margin,
// padding, position, overflow, cursor, border, box-shadow and visibility
// methods) are emitted into styled_generated.go by cmd/stylegen, a
// maintainer-run generator whose tables mirror styles.rs.
//
// Naming: snake_case becomes CamelCase; numeric segments are kept
// ("w_4" -> W4, "w_0p5" -> W0p5, "w_neg_4" -> WNeg4), and two adjacent
// numeric segments keep their underscore ("w_1_2" -> W1_2). Parameters
// typed `impl Into<Length>` / `impl Into<DefiniteLength>` /
// `impl Into<Background>` take the IntoLength / IntoDefiniteLength /
// IntoBackground constraints; `impl Into<Pixels>` takes float32 logical
// pixels; `impl Into<AbsoluteLength>` takes an AbsoluteLength (Px or
// RemsOf); `impl IntoColor<Hsla>` takes an Hsla.

// Styled supplies the pinned Styled vocabulary as fluent methods that
// return the owner's concrete type. Embed Styled[*YourComponent] by value
// and bind it once with BindStyled (own storage, the common Base shape
// `impl Styled for X { fn style(&mut self) { &mut self.style } }`) or
// BindStyledTo (delegating storage, the `self.base.style()` shape).
//
// A bound component is mutable and must be used through its original
// pointer: pointer aliases share mutations; copying the component by
// value is unsupported and shared style operations on a copy panic
// before changing state. The zero value is unbound. Components are not
// safe for concurrent mutation.
type Styled[Self any] struct {
	self   Self
	anchor *Styled[Self]
	// own is the storage used by BindStyled.
	own StyleRefinement
	// target is the bound storage: &own, or the delegate's refinement.
	target *StyleRefinement
}

// StyleBase is the binding hook promoted through value embedding.
// Components using that embedding must not override this method.
func (s *Styled[Self]) StyleBase() *Styled[Self] { return s }

// BindStyled binds a newly constructed component to its embedded helper
// with its own style storage and returns that same component pointer.
// Both type parameters are inferred. It panics on a nil owner, a missing
// helper, or an already-bound (or copied bound) helper.
func BindStyled[Value any, Self interface {
	*Value
	StyleBase() *Styled[Self]
}](self Self) Self {
	s := styledBindTarget[Value](self)
	s.bind(self, &s.own)
	return self
}

// BindStyledTo binds a newly constructed component to its embedded
// helper with delegated storage: every style method writes into storage
// (the pinned delegation `fn style(&mut self) -> &mut StyleRefinement {
// self.base.style() }`, e.g. a component styling its inner div through
// div.StyleMut()). It panics like BindStyled, and on nil storage.
func BindStyledTo[Value any, Self interface {
	*Value
	StyleBase() *Styled[Self]
}](self Self, storage *StyleRefinement) Self {
	if storage == nil {
		panic("gpui: BindStyledTo requires non-nil style storage")
	}
	s := styledBindTarget[Value](self)
	s.bind(self, storage)
	return self
}

// styledBindTarget validates the owner and returns its helper.
func styledBindTarget[Value any, Self interface {
	*Value
	StyleBase() *Styled[Self]
}](self Self) *Styled[Self] {
	if self == nil {
		panic("gpui: cannot bind a nil component")
	}
	s := self.StyleBase()
	if s == nil {
		panic("gpui: component has no Styled helper")
	}
	if s.anchor != nil {
		panic("gpui: Styled is already bound; create a new component instead of rebinding or copying")
	}
	return s
}

// bind records the owner and the storage.
func (s *Styled[Self]) bind(self Self, storage *StyleRefinement) {
	s.self, s.anchor, s.target = self, s, storage
}

// check rejects unbound and copied helpers.
func (s *Styled[Self]) check() {
	if s == nil || s.anchor == nil {
		panic("gpui: unbound Styled; construct the component with BindStyled")
	}
	if s.anchor != s {
		panic("gpui: copied bound component; use its original pointer")
	}
}

// StyleMut returns the mutable style refinement this element's style
// methods write into (styled.rs:27 Styled::style).
func (s *Styled[Self]) StyleMut() *StyleRefinement {
	s.check()
	return s.target
}

// Style returns an independent value snapshot of the refinement (slice
// fields copied); mutating it does not reach the component.
func (s *Styled[Self]) Style() StyleRefinement {
	s.check()
	return s.target.Clone()
}

// storage returns the bound storage without the copy check (the
// framework's own phase code, which always runs on the original).
func (s *Styled[Self]) storage() *StyleRefinement {
	if s.target == nil {
		return &s.own
	}
	return s.target
}

// Refine applies the given refinement to this element's style
// (styled.rs:30 Styled::refine_style): only present fields change.
func (s *Styled[Self]) Refine(refinement StyleRefinement) Self {
	s.StyleMut().Refine(&refinement)
	return s.self
}

// TextStyleMut returns the mutable text refinement of this element
// (styled.rs:671 Styled::text_style).
func (s *Styled[Self]) TextStyleMut() *TextStyleRefinement {
	return &s.StyleMut().Text
}

// ---- Filters (styled.rs:55-91) ----

// Blur blurs this element's own content and children (CSS filter:
// blur), appending to the filter chain (styled.rs:55).
func (s *Styled[Self]) Blur(radius float32) Self {
	st := s.StyleMut()
	chain, _ := st.Filter.Get()
	st.Filter = Some(append(slices.Clip(slices.Clone(chain)), Blur(radius)))
	return s.self
}

// Filter replaces the full filter chain of this element's own content
// (styled.rs:66).
func (s *Styled[Self]) Filter(filters []Filter) Self {
	s.StyleMut().Filter = Some(slices.Clone(filters))
	return s.self
}

// BackdropBlur blurs the content rendered behind this element (CSS
// backdrop-filter: blur), appending to the backdrop chain
// (styled.rs:77).
func (s *Styled[Self]) BackdropBlur(radius float32) Self {
	st := s.StyleMut()
	chain, _ := st.BackdropFilter.Get()
	st.BackdropFilter = Some(append(slices.Clip(slices.Clone(chain)), Blur(radius)))
	return s.self
}

// BackdropFilter replaces the full backdrop filter chain (styled.rs:88).
func (s *Styled[Self]) BackdropFilter(filters []Filter) Self {
	s.StyleMut().BackdropFilter = Some(slices.Clone(filters))
	return s.self
}

// ---- Display and inline alignment (styled.rs:94-160) ----

// Display sets the display type (styled.rs:94).
func (s *Styled[Self]) Display(display Display) Self {
	s.StyleMut().Display = Some(display)
	return s.self
}

// Block sets the display type to block (styled.rs:101).
func (s *Styled[Self]) Block() Self { return s.Display(DisplayBlock) }

// Flex sets the display type to flex (styled.rs:107).
func (s *Styled[Self]) Flex() Self { return s.Display(DisplayFlex) }

// Grid sets the display type to grid (styled.rs:113).
func (s *Styled[Self]) Grid() Self { return s.Display(DisplayGrid) }

// Inline contributes this element's contents to a block parent's
// paragraph (styled.rs:119).
func (s *Styled[Self]) Inline() Self { return s.Display(DisplayInline) }

// InlineFlex places this element in a paragraph as one atomic box with
// flex layout inside (styled.rs:124).
func (s *Styled[Self]) InlineFlex() Self { return s.Display(DisplayInlineFlex) }

// VerticalAlign sets the vertical alignment in an inline layout
// (styled.rs:129).
func (s *Styled[Self]) VerticalAlign(align VerticalAlign) Self {
	s.StyleMut().VerticalAlign = Some(align)
	return s.self
}

// AlignBaseline aligns this inline box with the text baseline
// (styled.rs:135).
func (s *Styled[Self]) AlignBaseline() Self { return s.VerticalAlign(VerticalAlignBaseline) }

// AlignMiddle aligns this inline box's midpoint with the parent baseline
// plus half the x-height (styled.rs:142).
func (s *Styled[Self]) AlignMiddle() Self { return s.VerticalAlign(VerticalAlignMiddle) }

// AlignTop aligns this inline box with the top of its line
// (styled.rs:147).
func (s *Styled[Self]) AlignTop() Self { return s.VerticalAlign(VerticalAlignTop) }

// AlignBottom aligns this inline box with the bottom of its line
// (styled.rs:152).
func (s *Styled[Self]) AlignBottom() Self { return s.VerticalAlign(VerticalAlignBottom) }

// Hidden sets the display type to none (styled.rs:158).
func (s *Styled[Self]) Hidden() Self { return s.Display(DisplayNone) }

// ---- Scrollbar and overflow fade (styled.rs:166-196) ----

// ScrollbarWidth sets the space reserved for the scrollbar
// (styled.rs:166).
func (s *Styled[Self]) ScrollbarWidth(width AbsoluteLength) Self {
	s.StyleMut().ScrollbarWidth = Some(width)
	return s.self
}

// OverflowFade sets per-edge fade distances for clipped overflow
// (styled.rs:176).
func (s *Styled[Self]) OverflowFade(fade AbsoluteLengthEdges) Self {
	st := s.StyleMut()
	st.OverflowFadeTop = Some(fade.Top)
	st.OverflowFadeRight = Some(fade.Right)
	st.OverflowFadeBottom = Some(fade.Bottom)
	st.OverflowFadeLeft = Some(fade.Left)
	return s.self
}

// OverflowFadeX sets equal left/right fades and clears top/bottom
// (styled.rs:188, Edges::horizontal).
func (s *Styled[Self]) OverflowFadeX(fade AbsoluteLength) Self {
	return s.OverflowFade(AbsoluteLengthEdges{Left: fade, Right: fade})
}

// OverflowFadeY sets equal top/bottom fades and clears left/right
// (styled.rs:194, Edges::vertical).
func (s *Styled[Self]) OverflowFadeY(fade AbsoluteLength) Self {
	return s.OverflowFade(AbsoluteLengthEdges{Top: fade, Bottom: fade})
}

// ---- Whitespace, truncation and text alignment (styled.rs:200-293) ----

// Whitespace sets the white space handling (styled.rs:200).
func (s *Styled[Self]) Whitespace(whiteSpace WhiteSpace) Self {
	s.TextStyleMut().WhiteSpace = Some(whiteSpace)
	return s.self
}

// WhitespaceNormal sets white space to normal (styled.rs:207).
func (s *Styled[Self]) WhitespaceNormal() Self { return s.Whitespace(WhiteSpaceNormal) }

// WhitespaceNowrap sets white space to nowrap (styled.rs:214).
func (s *Styled[Self]) WhitespaceNowrap() Self { return s.Whitespace(WhiteSpaceNowrap) }

// TextEllipsis truncates overflowing text with an ellipsis at the end
// (styled.rs:221).
func (s *Styled[Self]) TextEllipsis() Self {
	return s.TextOverflow(TextOverflow{Kind: TextOverflowTruncate, Ellipsis: Ellipsis})
}

// TextEllipsisStart truncates with an ellipsis at the start
// (styled.rs:229).
func (s *Styled[Self]) TextEllipsisStart() Self {
	return s.TextOverflow(TextOverflow{Kind: TextOverflowTruncateStart, Ellipsis: Ellipsis})
}

// TextEllipsisMiddle truncates with an ellipsis in the middle
// (styled.rs:237).
func (s *Styled[Self]) TextEllipsisMiddle() Self {
	return s.TextOverflow(TextOverflow{Kind: TextOverflowTruncateMiddle, Ellipsis: Ellipsis})
}

// TextOverflow sets the text overflow behavior (styled.rs:243).
func (s *Styled[Self]) TextOverflow(overflow TextOverflow) Self {
	s.TextStyleMut().TextOverflow = Some(overflow)
	return s.self
}

// TextAlign sets the text alignment (styled.rs:249).
func (s *Styled[Self]) TextAlign(align TextAlign) Self {
	s.TextStyleMut().TextAlign = Some(align)
	return s.self
}

// TextLeft aligns text left (styled.rs:255).
func (s *Styled[Self]) TextLeft() Self { return s.TextAlign(TextAlignLeft) }

// TextCenter centers text (styled.rs:260).
func (s *Styled[Self]) TextCenter() Self { return s.TextAlign(TextAlignCenter) }

// TextRight aligns text right (styled.rs:265).
func (s *Styled[Self]) TextRight() Self { return s.TextAlign(TextAlignRight) }

// LetterSpacing sets the letter spacing in logical pixels
// (styled.rs:270).
func (s *Styled[Self]) LetterSpacing(spacing float32) Self {
	s.TextStyleMut().LetterSpacing = Some(spacing)
	return s.self
}

// TextTransform sets the case transformation (styled.rs:276).
func (s *Styled[Self]) TextTransform(transform TextTransform) Self {
	s.TextStyleMut().TextTransform = Some(transform)
	return s.self
}

// Truncate hides overflow, prevents wrapping and truncates with an
// ellipsis (styled.rs:283).
func (s *Styled[Self]) Truncate() Self {
	s.OverflowHidden()
	s.WhitespaceNowrap()
	return s.TextEllipsis()
}

// LineClamp sets the number of lines shown before truncating, and hides
// overflow (styled.rs:289).
func (s *Styled[Self]) LineClamp(lines int) Self {
	s.TextStyleMut().LineClamp = Some(lines)
	return s.OverflowHidden()
}

// ---- Flex (styled.rs:297-434) ----

// FlexDirection sets the flex direction (styled.rs:297).
func (s *Styled[Self]) FlexDirection(direction FlexDirection) Self {
	s.StyleMut().FlexDirection = Some(direction)
	return s.self
}

// FlexCol sets the flex direction to column (styled.rs:304).
func (s *Styled[Self]) FlexCol() Self { return s.FlexDirection(FlexDirectionColumn) }

// FlexColReverse sets the flex direction to column-reverse
// (styled.rs:311).
func (s *Styled[Self]) FlexColReverse() Self { return s.FlexDirection(FlexDirectionColumnReverse) }

// FlexRow sets the flex direction to row (styled.rs:318).
func (s *Styled[Self]) FlexRow() Self { return s.FlexDirection(FlexDirectionRow) }

// FlexRowReverse sets the flex direction to row-reverse (styled.rs:325).
func (s *Styled[Self]) FlexRowReverse() Self { return s.FlexDirection(FlexDirectionRowReverse) }

// setFlex writes grow, shrink and basis together.
func (s *Styled[Self]) setFlex(grow, shrink float32, basis Length) Self {
	st := s.StyleMut()
	st.FlexGrow = Some(grow)
	st.FlexShrink = Some(shrink)
	st.FlexBasis = Some(basis)
	return s.self
}

// Flex1 lets the item grow and shrink, ignoring its initial size
// (styled.rs:332: grow 1, shrink 1, basis relative(0)).
func (s *Styled[Self]) Flex1() Self { return s.setFlex(1, 1, DefiniteLengthOf(Fraction(0))) }

// FlexAuto lets the item grow and shrink from its initial size
// (styled.rs:341).
func (s *Styled[Self]) FlexAuto() Self { return s.setFlex(1, 1, AutoLength()) }

// FlexInitial lets the item shrink but not grow (styled.rs:350).
func (s *Styled[Self]) FlexInitial() Self { return s.setFlex(0, 1, AutoLength()) }

// FlexNone prevents the item from growing or shrinking (styled.rs:359).
func (s *Styled[Self]) FlexNone() Self { return s.setFlex(0, 0, AutoLength()) }

// FlexBasis sets the initial main-axis size (styled.rs:368).
func (s *Styled[Self]) FlexBasis[L IntoLength](basis L) Self {
	s.StyleMut().FlexBasis = Some(LengthOf(basis))
	return s.self
}

// FlexGrow sets the grow factor (styled.rs:375).
func (s *Styled[Self]) FlexGrow(grow float32) Self {
	s.StyleMut().FlexGrow = Some(grow)
	return s.self
}

// FlexGrow0 disables growth (styled.rs:382).
func (s *Styled[Self]) FlexGrow0() Self { return s.FlexGrow(0) }

// FlexGrow1 enables growth (styled.rs:389).
func (s *Styled[Self]) FlexGrow1() Self { return s.FlexGrow(1) }

// FlexShrink sets the shrink factor (styled.rs:396).
func (s *Styled[Self]) FlexShrink(shrink float32) Self {
	s.StyleMut().FlexShrink = Some(shrink)
	return s.self
}

// FlexShrink0 disables shrinking (styled.rs:403).
func (s *Styled[Self]) FlexShrink0() Self { return s.FlexShrink(0) }

// FlexShrink1 enables shrinking (styled.rs:410).
func (s *Styled[Self]) FlexShrink1() Self { return s.FlexShrink(1) }

// FlexWrap lets flex items wrap (styled.rs:417).
func (s *Styled[Self]) FlexWrap() Self {
	s.StyleMut().FlexWrap = Some(FlexWrapWrap)
	return s.self
}

// FlexWrapReverse wraps flex items in reverse (styled.rs:424).
func (s *Styled[Self]) FlexWrapReverse() Self {
	s.StyleMut().FlexWrap = Some(FlexWrapWrapReverse)
	return s.self
}

// FlexNowrap prevents flex items from wrapping (styled.rs:431).
func (s *Styled[Self]) FlexNowrap() Self {
	s.StyleMut().FlexWrap = Some(FlexWrapNoWrap)
	return s.self
}

// ---- Alignment (styled.rs:438-624) ----

// alignItems writes align_items.
func (s *Styled[Self]) alignItems(value AlignItems) Self {
	s.StyleMut().AlignItems = Some(value)
	return s.self
}

// ItemsStart aligns items to the cross-axis start (styled.rs:438:
// FlexStart).
func (s *Styled[Self]) ItemsStart() Self { return s.alignItems(AlignItemsFlexStart) }

// ItemsEnd aligns items to the cross-axis end (styled.rs:445: FlexEnd).
func (s *Styled[Self]) ItemsEnd() Self { return s.alignItems(AlignItemsFlexEnd) }

// ItemsCenter centers items on the cross axis (styled.rs:452).
func (s *Styled[Self]) ItemsCenter() Self { return s.alignItems(AlignItemsCenter) }

// ItemsBaseline aligns items on their baselines (styled.rs:459).
func (s *Styled[Self]) ItemsBaseline() Self { return s.alignItems(AlignItemsBaseline) }

// ItemsStretch stretches items on the cross axis (styled.rs:466).
func (s *Styled[Self]) ItemsStretch() Self { return s.alignItems(AlignItemsStretch) }

// alignSelf writes align_self.
func (s *Styled[Self]) alignSelf(value AlignItems) Self {
	s.StyleMut().AlignSelf = Some(value)
	return s.self
}

// SelfStart aligns this item to the cross-axis start (styled.rs:473:
// Start).
func (s *Styled[Self]) SelfStart() Self { return s.alignSelf(AlignItemsStart) }

// SelfEnd aligns this item to the cross-axis end (styled.rs:480: End).
func (s *Styled[Self]) SelfEnd() Self { return s.alignSelf(AlignItemsEnd) }

// SelfFlexStart aligns this item to the flex start (styled.rs:487).
func (s *Styled[Self]) SelfFlexStart() Self { return s.alignSelf(AlignItemsFlexStart) }

// SelfFlexEnd aligns this item to the flex end (styled.rs:494).
func (s *Styled[Self]) SelfFlexEnd() Self { return s.alignSelf(AlignItemsFlexEnd) }

// SelfCenter centers this item on the cross axis (styled.rs:501).
func (s *Styled[Self]) SelfCenter() Self { return s.alignSelf(AlignItemsCenter) }

// SelfBaseline aligns this item on the baseline (styled.rs:508).
func (s *Styled[Self]) SelfBaseline() Self { return s.alignSelf(AlignItemsBaseline) }

// SelfStretch stretches this item on the cross axis (styled.rs:515).
func (s *Styled[Self]) SelfStretch() Self { return s.alignSelf(AlignItemsStretch) }

// justify writes justify_content.
func (s *Styled[Self]) justify(value AlignContent) Self {
	s.StyleMut().JustifyContent = Some(value)
	return s.self
}

// JustifyStart packs items at the main-axis start (styled.rs:522).
func (s *Styled[Self]) JustifyStart() Self { return s.justify(AlignContentStart) }

// JustifyEnd packs items at the main-axis end (styled.rs:529).
func (s *Styled[Self]) JustifyEnd() Self { return s.justify(AlignContentEnd) }

// JustifyCenter centers items on the main axis (styled.rs:536).
func (s *Styled[Self]) JustifyCenter() Self { return s.justify(AlignContentCenter) }

// JustifyBetween distributes space between items (styled.rs:544).
func (s *Styled[Self]) JustifyBetween() Self { return s.justify(AlignContentSpaceBetween) }

// JustifyAround distributes space around items (styled.rs:552).
func (s *Styled[Self]) JustifyAround() Self { return s.justify(AlignContentSpaceAround) }

// JustifyEvenly distributes space evenly (styled.rs:562).
func (s *Styled[Self]) JustifyEvenly() Self { return s.justify(AlignContentSpaceEvenly) }

// alignContent writes align_content.
func (s *Styled[Self]) alignContent(value AlignContent) Self {
	s.StyleMut().AlignContent = Some(value)
	return s.self
}

// ContentNormal clears align-content (styled.rs:569: align_content =
// None, the refinement absent).
func (s *Styled[Self]) ContentNormal() Self {
	s.StyleMut().AlignContent = Optional[AlignContent]{}
	return s.self
}

// ContentCenter packs content in the cross-axis center (styled.rs:576).
func (s *Styled[Self]) ContentCenter() Self { return s.alignContent(AlignContentCenter) }

// ContentStart packs content at the cross-axis start (styled.rs:583:
// FlexStart).
func (s *Styled[Self]) ContentStart() Self { return s.alignContent(AlignContentFlexStart) }

// ContentEnd packs content at the cross-axis end (styled.rs:590:
// FlexEnd).
func (s *Styled[Self]) ContentEnd() Self { return s.alignContent(AlignContentFlexEnd) }

// ContentBetween distributes content with space between
// (styled.rs:598).
func (s *Styled[Self]) ContentBetween() Self { return s.alignContent(AlignContentSpaceBetween) }

// ContentAround distributes content with space around (styled.rs:606).
func (s *Styled[Self]) ContentAround() Self { return s.alignContent(AlignContentSpaceAround) }

// ContentEvenly distributes content evenly (styled.rs:614).
func (s *Styled[Self]) ContentEvenly() Self { return s.alignContent(AlignContentSpaceEvenly) }

// ContentStretch stretches content on the cross axis (styled.rs:621).
func (s *Styled[Self]) ContentStretch() Self { return s.alignContent(AlignContentStretch) }

// ---- Aspect ratio, background and border pattern (styled.rs:628-668) ----

// AspectRatio sets the width/height ratio (styled.rs:628).
func (s *Styled[Self]) AspectRatio(ratio float32) Self {
	s.StyleMut().AspectRatio = Some(ratio)
	return s.self
}

// AspectSquare sets the aspect ratio to 1 (styled.rs:635).
func (s *Styled[Self]) AspectSquare() Self { return s.AspectRatio(1) }

// Bg sets the background (styled.rs:641; the pinned Fill's one variant
// is a Background).
func (s *Styled[Self]) Bg[F IntoBackground](fill F) Self {
	s.StyleMut().Background = Some(BackgroundOf(fill))
	return s.self
}

// BorderDashed sets the border style to dashed (styled.rs:651).
func (s *Styled[Self]) BorderDashed() Self {
	s.StyleMut().BorderStyle = Some(BorderStyleDashed)
	return s.self
}

// BorderDashedLength sets the dash length as a multiple of the border
// width, clamped at zero (styled.rs:657).
func (s *Styled[Self]) BorderDashedLength(lengthPerBorderWidth float32) Self {
	s.StyleMut().BorderDashedLength = Some(max32(lengthPerBorderWidth, 0))
	return s.self
}

// BorderDashedGap sets the dash gap as a multiple of the border width,
// clamped at zero (styled.rs:664).
func (s *Styled[Self]) BorderDashedGap(gapPerBorderWidth float32) Self {
	s.StyleMut().BorderDashedGap = Some(max32(gapPerBorderWidth, 0))
	return s.self
}

// ---- Text (styled.rs:679-908) ----

// TextColor sets the text color, cascading to children (styled.rs:679).
func (s *Styled[Self]) TextColor(color Hsla) Self {
	s.TextStyleMut().Color = Some(color)
	return s.self
}

// FontWeight sets the font weight, cascading to children
// (styled.rs:687; FontWeight is the port's float32 weight).
func (s *Styled[Self]) FontWeight(weight float32) Self {
	s.TextStyleMut().FontWeight = Some(weight)
	return s.self
}

// TextBg sets the text background color, cascading to children
// (styled.rs:695).
func (s *Styled[Self]) TextBg(bg Hsla) Self {
	s.TextStyleMut().BackgroundColor = Some(bg)
	return s.self
}

// TextSize sets the font size, cascading to children (styled.rs:703).
func (s *Styled[Self]) TextSize(size AbsoluteLength) Self {
	s.TextStyleMut().FontSize = Some(size)
	return s.self
}

// TextXs sets the font size to 0.75rem (styled.rs:710).
func (s *Styled[Self]) TextXs() Self { return s.TextSize(RemsOf(0.75)) }

// TextSm sets the font size to 0.875rem (styled.rs:717).
func (s *Styled[Self]) TextSm() Self { return s.TextSize(RemsOf(0.875)) }

// TextBase sets the font size to 1rem (styled.rs:724).
func (s *Styled[Self]) TextBase() Self { return s.TextSize(RemsOf(1.0)) }

// TextLg sets the font size to 1.125rem (styled.rs:731).
func (s *Styled[Self]) TextLg() Self { return s.TextSize(RemsOf(1.125)) }

// TextXl sets the font size to 1.25rem (styled.rs:738).
func (s *Styled[Self]) TextXl() Self { return s.TextSize(RemsOf(1.25)) }

// Text2xl sets the font size to 1.5rem (styled.rs:745).
func (s *Styled[Self]) Text2xl() Self { return s.TextSize(RemsOf(1.5)) }

// Text3xl sets the font size to 1.875rem (styled.rs:752).
func (s *Styled[Self]) Text3xl() Self { return s.TextSize(RemsOf(1.875)) }

// Italic sets the font style to italic (styled.rs:759).
func (s *Styled[Self]) Italic() Self {
	s.TextStyleMut().FontStyle = Some(FontStyleItalic)
	return s.self
}

// NotItalic sets the font style to normal (styled.rs:766).
func (s *Styled[Self]) NotItalic() Self {
	s.TextStyleMut().FontStyle = Some(FontStyleNormal)
	return s.self
}

// Underline sets a 1px underline (styled.rs:773).
func (s *Styled[Self]) Underline() Self {
	s.TextStyleMut().Underline = Some(UnderlineStyle{Thickness: 1})
	return s.self
}

// LineThrough sets a 1px strikethrough (styled.rs:784).
func (s *Styled[Self]) LineThrough() Self {
	s.TextStyleMut().Strikethrough = Some(StrikethroughStyle{Thickness: 1})
	return s.self
}

// TextDecorationNone removes the underline refinement (styled.rs:796:
// underline = None).
func (s *Styled[Self]) TextDecorationNone() Self {
	s.TextStyleMut().Underline = Optional[UnderlineStyle]{}
	return s.self
}

// underlineWith edits the underline, inserting the default first
// (`underline.get_or_insert_with(Default::default)`). The color pointer
// is replaced, never written through, so snapshots stay independent.
func (s *Styled[Self]) underlineWith(edit func(*UnderlineStyle)) Self {
	text := s.TextStyleMut()
	underline, _ := text.Underline.Get()
	edit(&underline)
	text.Underline = Some(underline)
	return s.self
}

// TextDecorationColor sets the underline color (styled.rs:802).
func (s *Styled[Self]) TextDecorationColor(color Hsla) Self {
	return s.underlineWith(func(u *UnderlineStyle) { c := color; u.Color = &c })
}

// TextDecorationSolid sets a solid underline (styled.rs:811).
func (s *Styled[Self]) TextDecorationSolid() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Wavy = false })
}

// TextDecorationWavy sets a wavy underline (styled.rs:820).
func (s *Styled[Self]) TextDecorationWavy() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Wavy = true })
}

// TextDecoration0 sets a 0px underline thickness (styled.rs:829).
func (s *Styled[Self]) TextDecoration0() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 0 })
}

// TextDecoration1 sets a 1px underline thickness (styled.rs:838).
func (s *Styled[Self]) TextDecoration1() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 1 })
}

// TextDecoration2 sets a 2px underline thickness (styled.rs:847).
func (s *Styled[Self]) TextDecoration2() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 2 })
}

// TextDecoration4 sets a 4px underline thickness (styled.rs:856).
func (s *Styled[Self]) TextDecoration4() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 4 })
}

// TextDecoration8 sets an 8px underline thickness (styled.rs:865).
func (s *Styled[Self]) TextDecoration8() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 8 })
}

// FontFamily sets the font family (styled.rs:873).
func (s *Styled[Self]) FontFamily(familyName string) Self {
	s.TextStyleMut().FontFamily = Some(familyName)
	return s.self
}

// FontFeatures sets the font features (styled.rs:879).
func (s *Styled[Self]) FontFeatures(features []FontFeature) Self {
	s.TextStyleMut().FontFeatures = Some(slices.Clone(features))
	return s.self
}

// Font sets family, features, weight and style, and the fallbacks
// (styled.rs:885: fallbacks are assigned as-is, so an empty list makes
// the refinement absent).
func (s *Styled[Self]) Font(font FontDescriptor) Self {
	text := s.TextStyleMut()
	text.FontFamily = Some(font.Family)
	text.FontFeatures = Some(slices.Clone(font.Features))
	text.FontWeight = Some(font.Weight)
	text.FontStyle = Some(font.Style)
	if len(font.Fallbacks) == 0 {
		text.FontFallbacks = Optional[[]string]{}
	} else {
		text.FontFallbacks = Some(slices.Clone(font.Fallbacks))
	}
	return s.self
}

// LineHeight sets the line height (styled.rs:905).
func (s *Styled[Self]) LineHeight[L IntoDefiniteLength](lineHeight L) Self {
	s.TextStyleMut().LineHeight = Some(DefiniteLengthFrom(lineHeight))
	return s.self
}

// Opacity sets the opacity of this element and its children
// (styled.rs:911).
func (s *Styled[Self]) Opacity(opacity float32) Self {
	s.StyleMut().Opacity = Some(opacity)
	return s.self
}

// ---- Grid (styled.rs:917-1054) ----

// GridCols sets the column template (styled.rs:917: min size Zero).
func (s *Styled[Self]) GridCols(cols uint16) Self {
	s.StyleMut().GridCols = Some(GridTemplate{Repeat: cols, MinSize: GridTemplateMinZero})
	return s.self
}

// GridColsMinContent sets min-content column minimums (styled.rs:927).
func (s *Styled[Self]) GridColsMinContent(cols uint16) Self {
	s.StyleMut().GridCols = Some(GridTemplate{Repeat: cols, MinSize: GridTemplateMinMinContent})
	return s.self
}

// GridColsMaxContent sets max-content column sizing (styled.rs:936).
func (s *Styled[Self]) GridColsMaxContent(cols uint16) Self {
	s.StyleMut().GridCols = Some(GridTemplate{Repeat: cols, MinSize: GridTemplateMinMaxContent})
	return s.self
}

// GridRows sets the row template (styled.rs:945).
func (s *Styled[Self]) GridRows(rows uint16) Self {
	s.StyleMut().GridRows = Some(GridTemplate{Repeat: rows, MinSize: GridTemplateMinZero})
	return s.self
}

// GridRowsMinContent sets min-content row minimums (styled.rs:955).
func (s *Styled[Self]) GridRowsMinContent(rows uint16) Self {
	s.StyleMut().GridRows = Some(GridTemplate{Repeat: rows, MinSize: GridTemplateMinMinContent})
	return s.self
}

// GridRowsMaxContent sets max-content row sizing (styled.rs:964).
func (s *Styled[Self]) GridRowsMaxContent(rows uint16) Self {
	s.StyleMut().GridRows = Some(GridTemplate{Repeat: rows, MinSize: GridTemplateMinMaxContent})
	return s.self
}

// gridLocation edits the grid location, inserting the default first
// (style.rs:416 grid_location_mut: get_or_insert_default).
func (s *Styled[Self]) gridLocation(edit func(*GridLocation)) Self {
	st := s.StyleMut()
	location, _ := st.GridLocation.Get()
	edit(&location)
	st.GridLocation = Some(location)
	return s.self
}

// ColStart sets the column start line (styled.rs:973).
func (s *Styled[Self]) ColStart(start int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.Start = LineGridPlacement(start) })
}

// ColStartAuto sets the column start to auto (styled.rs:980).
func (s *Styled[Self]) ColStartAuto() Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.Start = AutoGridPlacement() })
}

// ColEnd sets the column end line (styled.rs:987).
func (s *Styled[Self]) ColEnd(end int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.End = LineGridPlacement(end) })
}

// ColEndAuto sets the column end to auto (styled.rs:994).
func (s *Styled[Self]) ColEndAuto() Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.End = AutoGridPlacement() })
}

// ColSpan spans the given number of columns (styled.rs:1001).
func (s *Styled[Self]) ColSpan(span uint16) Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Column = GridPlacementRange{Start: SpanGridPlacement(span), End: SpanGridPlacement(span)}
	})
}

// ColSpanFull spans every column (styled.rs:1008: Line(1)..Line(-1)).
func (s *Styled[Self]) ColSpanFull() Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Column = GridPlacementRange{Start: LineGridPlacement(1), End: LineGridPlacement(-1)}
	})
}

// RowStart sets the row start line (styled.rs:1015).
func (s *Styled[Self]) RowStart(start int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.Start = LineGridPlacement(start) })
}

// RowStartAuto sets the row start to auto (styled.rs:1022).
func (s *Styled[Self]) RowStartAuto() Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.Start = AutoGridPlacement() })
}

// RowEnd sets the row end line (styled.rs:1029).
func (s *Styled[Self]) RowEnd(end int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.End = LineGridPlacement(end) })
}

// RowEndAuto sets the row end to auto (styled.rs:1036).
func (s *Styled[Self]) RowEndAuto() Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.End = AutoGridPlacement() })
}

// RowSpan spans the given number of rows (styled.rs:1043).
func (s *Styled[Self]) RowSpan(span uint16) Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Row = GridPlacementRange{Start: SpanGridPlacement(span), End: SpanGridPlacement(span)}
	})
}

// RowSpanFull spans every row (styled.rs:1050).
func (s *Styled[Self]) RowSpanFull() Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Row = GridPlacementRange{Start: LineGridPlacement(1), End: LineGridPlacement(-1)}
	})
}

// ---- Debug and corner smoothing (styled.rs:1058-1085) ----

// Debug draws a red debug outline around this element (styled.rs:1058;
// cfg(debug_assertions) in the pin, always available in the port).
func (s *Styled[Self]) Debug() Self {
	s.StyleMut().Debug = Some(true)
	return s.self
}

// DebugBelow draws the debug outline on this element and every
// conforming descendant (styled.rs:1065).
func (s *Styled[Self]) DebugBelow() Self {
	s.StyleMut().DebugBelow = Some(true)
	return s.self
}

// RoundedSmoothing sets the corner smoothing, 0 (circular) to 1 (maximum)
// (styled.rs:1072; the pinned debug_assert on the range is not ported —
// paint clamps to [0, 1]).
func (s *Styled[Self]) RoundedSmoothing(amount float32) Self {
	s.StyleMut().CornerSmoothing = Some(amount)
	return s.self
}

// RoundedSmoothingIos sets corner smoothing to 0.6 (styled.rs:1082).
func (s *Styled[Self]) RoundedSmoothingIos() Self { return s.RoundedSmoothing(0.6) }
