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

// lazy is the type-parameter marker on every fluent method of the
// mixins (Styled, Parent, Interactive): `func (s *Styled[Self])
// Flex[_ lazy]() Self`. Its only type is lazyT, so callers never write
// the argument (d.Flex() infers it).
//
// It exists for build speed. Go compiles every ordinary method of an
// instantiated generic type in each package that references the
// instantiation, so with plain methods a package that merely mentioned
// *DivElement (which embeds Styled[*DivElement]) recompiled the whole
// ~3,100-method vocabulary: a 109-line view compiled to a 30 MB object
// and rebuilt in ~14 s, and each Component[Self] component added ~30 MB
// to every package using it. A generic method is not part of the method
// set; it is compiled only where it is called, and only the methods a
// package actually calls (Rust's Styled trait has the same
// pay-for-what-you-use property). internal/buildcostspec guards it.
//
// The one cost: a fluent method cannot be used as a method value
// (f := d.Flex) without instantiation, which callers cannot spell; wrap
// it in a closure instead.
type lazy interface{ lazyT }

// lazyT is lazy's only type.
type lazyT struct{}

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
func (s *Styled[Self]) Refine[_ lazy](refinement StyleRefinement) Self {
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
func (s *Styled[Self]) Blur[_ lazy](radius float32) Self {
	st := s.StyleMut()
	chain, _ := st.Filter.Get()
	st.Filter = Some(append(slices.Clip(slices.Clone(chain)), Blur(radius)))
	return s.self
}

// Filter replaces the full filter chain of this element's own content
// (styled.rs:66).
func (s *Styled[Self]) Filter[_ lazy](filters []Filter) Self {
	s.StyleMut().Filter = Some(slices.Clone(filters))
	return s.self
}

// BackdropBlur blurs the content rendered behind this element (CSS
// backdrop-filter: blur), appending to the backdrop chain
// (styled.rs:77).
func (s *Styled[Self]) BackdropBlur[_ lazy](radius float32) Self {
	st := s.StyleMut()
	chain, _ := st.BackdropFilter.Get()
	st.BackdropFilter = Some(append(slices.Clip(slices.Clone(chain)), Blur(radius)))
	return s.self
}

// BackdropFilter replaces the full backdrop filter chain (styled.rs:88).
func (s *Styled[Self]) BackdropFilter[_ lazy](filters []Filter) Self {
	s.StyleMut().BackdropFilter = Some(slices.Clone(filters))
	return s.self
}

// ---- Display and inline alignment (styled.rs:94-160) ----

// Display sets the display type (styled.rs:94).
func (s *Styled[Self]) Display[_ lazy](display Display) Self {
	s.StyleMut().Display = Some(display)
	return s.self
}

// Block sets the display type to block (styled.rs:101).
func (s *Styled[Self]) Block[_ lazy]() Self { return s.Display(DisplayBlock) }

// Flex sets the display type to flex (styled.rs:107).
func (s *Styled[Self]) Flex[_ lazy]() Self { return s.Display(DisplayFlex) }

// Grid sets the display type to grid (styled.rs:113).
func (s *Styled[Self]) Grid[_ lazy]() Self { return s.Display(DisplayGrid) }

// Inline contributes this element's contents to a block parent's
// paragraph (styled.rs:119).
func (s *Styled[Self]) Inline[_ lazy]() Self { return s.Display(DisplayInline) }

// InlineFlex places this element in a paragraph as one atomic box with
// flex layout inside (styled.rs:124).
func (s *Styled[Self]) InlineFlex[_ lazy]() Self { return s.Display(DisplayInlineFlex) }

// InlineBlock places this element in a paragraph as one atomic box
// whose contents use block flow (browser-core 36; CSS
// display:inline-block). Atomic like InlineFlex, with block layout
// inside.
func (s *Styled[Self]) InlineBlock[_ lazy]() Self { return s.Display(DisplayInlineBlock) }

// InlineGrid places this element in a paragraph as one atomic box
// whose children use grid (browser-core 36; CSS display:inline-grid).
// Atomic like InlineFlex, with grid layout inside.
func (s *Styled[Self]) InlineGrid[_ lazy]() Self { return s.Display(DisplayInlineGrid) }

// Contents splices this element's children into its parent's layout —
// the element itself generates no box (browser-core 36; CSS
// display:contents; contents.go).
func (s *Styled[Self]) Contents[_ lazy]() Self { return s.Display(DisplayContents) }

// FlowRoot makes this element a block box that establishes an
// independent block formatting context (browser-core 36; CSS
// display:flow-root; the native rev-15 FlowRoot tag).
func (s *Styled[Self]) FlowRoot[_ lazy]() Self { return s.Display(DisplayFlowRoot) }

// ScrollbarGutterStable opts this scroll container into reserving its
// scrollbar gutter even while no scrollbar shows, so content does not
// shift when one appears (browser-core 36; CSS
// scrollbar-gutter: stable).
func (s *Styled[Self]) ScrollbarGutterStable[_ lazy]() Self {
	s.StyleMut().ScrollbarGutter = Some(ScrollbarGutterStable)
	return s.self
}

// OverscrollContain sets overscroll-behavior: contain on both axes
// (browser-core 15): a scroller that cannot consume a scroll (it
// reached its end, or its axis cannot scroll) stops the scroll chain —
// the remaining delta never reaches the ancestor scrollers. The
// Tailwind classes overscroll-contain / overscroll-x-contain /
// overscroll-y-contain land here.
func (s *Styled[Self]) OverscrollContain[_ lazy]() Self {
	style := s.StyleMut()
	style.OverscrollBehaviorX = Some(OverscrollBehaviorContain)
	style.OverscrollBehaviorY = Some(OverscrollBehaviorContain)
	return s.self
}

// OverscrollNone sets overscroll-behavior: none on both axes
// (browser-core 15): like contain, the scroll chain stops at this
// scroller; none additionally suppresses the user agent's overscroll
// affordance (the port has none either way).
func (s *Styled[Self]) OverscrollNone[_ lazy]() Self {
	style := s.StyleMut()
	style.OverscrollBehaviorX = Some(OverscrollBehaviorNone)
	style.OverscrollBehaviorY = Some(OverscrollBehaviorNone)
	return s.self
}

// OverscrollAuto resets overscroll-behavior: auto on both axes
// (browser-core 15): the scroller lets an unconsumed scroll chain to
// its ancestors (the CSS default).
func (s *Styled[Self]) OverscrollAuto[_ lazy]() Self {
	style := s.StyleMut()
	style.OverscrollBehaviorX = Some(OverscrollBehaviorAuto)
	style.OverscrollBehaviorY = Some(OverscrollBehaviorAuto)
	return s.self
}

// OverscrollXContain sets overscroll-behavior-x: contain.
func (s *Styled[Self]) OverscrollXContain[_ lazy]() Self {
	s.StyleMut().OverscrollBehaviorX = Some(OverscrollBehaviorContain)
	return s.self
}

// OverscrollXNone sets overscroll-behavior-x: none.
func (s *Styled[Self]) OverscrollXNone[_ lazy]() Self {
	s.StyleMut().OverscrollBehaviorX = Some(OverscrollBehaviorNone)
	return s.self
}

// OverscrollXAuto resets overscroll-behavior-x: auto.
func (s *Styled[Self]) OverscrollXAuto[_ lazy]() Self {
	s.StyleMut().OverscrollBehaviorX = Some(OverscrollBehaviorAuto)
	return s.self
}

// OverscrollYContain sets overscroll-behavior-y: contain.
func (s *Styled[Self]) OverscrollYContain[_ lazy]() Self {
	s.StyleMut().OverscrollBehaviorY = Some(OverscrollBehaviorContain)
	return s.self
}

// OverscrollYNone sets overscroll-behavior-y: none.
func (s *Styled[Self]) OverscrollYNone[_ lazy]() Self {
	s.StyleMut().OverscrollBehaviorY = Some(OverscrollBehaviorNone)
	return s.self
}

// OverscrollYAuto resets overscroll-behavior-y: auto.
func (s *Styled[Self]) OverscrollYAuto[_ lazy]() Self {
	s.StyleMut().OverscrollBehaviorY = Some(OverscrollBehaviorAuto)
	return s.self
}

// VerticalAlign sets the vertical alignment in an inline layout
// (styled.rs:129).
func (s *Styled[Self]) VerticalAlign[_ lazy](align VerticalAlign) Self {
	s.StyleMut().VerticalAlign = Some(align)
	return s.self
}

// AlignBaseline aligns this inline box with the text baseline
// (styled.rs:135).
func (s *Styled[Self]) AlignBaseline[_ lazy]() Self { return s.VerticalAlign(VerticalAlignBaseline) }

// AlignMiddle aligns this inline box's midpoint with the parent baseline
// plus half the x-height (styled.rs:142).
func (s *Styled[Self]) AlignMiddle[_ lazy]() Self { return s.VerticalAlign(VerticalAlignMiddle) }

// AlignTop aligns this inline box with the top of its line
// (styled.rs:147).
func (s *Styled[Self]) AlignTop[_ lazy]() Self { return s.VerticalAlign(VerticalAlignTop) }

// AlignBottom aligns this inline box with the bottom of its line
// (styled.rs:152).
func (s *Styled[Self]) AlignBottom[_ lazy]() Self { return s.VerticalAlign(VerticalAlignBottom) }

// Hidden sets the display type to none (styled.rs:158).
func (s *Styled[Self]) Hidden[_ lazy]() Self { return s.Display(DisplayNone) }

// ---- Position (browser-core 33, spec D3) ----

// Static sets the position to static: the element stays in flow but is
// not a containing block, so absolutely positioned children skip it
// and resolve against the nearest positioned ancestor like in a
// browser (css-position-3 §2.1; Chromium decides containership from
// position != static, LayoutObject::ComputeIsAbsoluteContainer,
// core/layout/layout_object.cc:2069-2081). Insets are ignored on a
// static element, as in CSS. This is the opt-in form of ticket 33: the
// port's default position stays Relative until ticket 76 flips it, so
// nothing changes for views that never call Static. Not generated by
// cmd/stylegen (the pinned styles.rs has no static variant).
func (s *Styled[Self]) Static[_ lazy]() Self {
	s.StyleMut().Position = Some(PositionStatic)
	return s.self
}

// ---- Position: sticky (browser-core 34, spec L3) ----

// Sticky sets the position to sticky: the element stays in flow (its
// insets are NOT relative offsets — the record translation zeroes
// them) and sticks inside its scroll container at prepaint — shifted
// so it stays within the inset-adjusted scrollport (top/left/
// bottom/right insets against the scroller's content box), never
// leaving its flow containing block (css-position-3 §3.4; Chromium's
// constraint math in core/page/scrolling/
// sticky_position_scrolling_constraints.cc:65-144, verified in the
// research checkout). The offset updates on scroll without re-layout,
// hitboxes and debug bounds carry it, and the element IS a containing
// block for absolute children (position != static, like Chromium).
// Not generated by cmd/stylegen (the pinned styles.rs has no sticky
// variant); the Tailwind `sticky` class maps onto it through the
// generated class switch. Not for divs participating in a parent
// paragraph (inline flow) — a documented limitation.
func (s *Styled[Self]) Sticky[_ lazy]() Self {
	s.StyleMut().Position = Some(PositionSticky)
	return s.self
}

// ---- Forced color adjust (browser-core 29) ----

// ForcedColorAdjustNone opts this element out of the forced-colours
// pass: while the pass is active (Windows high contrast, or an
// App.SetForcedColors pin), the element keeps its authored colours,
// gradients and shadows. This is CSS forced-color-adjust: none
// (css-color-adjust-1 §forced-color-adjust; Chromium's adjuster
// returns before any mapping, style_adjuster.cc:1036-1038 @ 0d4dcd4f).
// Not generated by cmd/stylegen (the pinned styles.rs has no such
// variant), like Static; the Tailwind class forced-color-adjust-none
// maps onto it through the generated class switch.
func (s *Styled[Self]) ForcedColorAdjustNone[_ lazy]() Self {
	s.StyleMut().ForcedColorAdjust = Some(ForcedColorAdjustNone)
	return s.self
}

// ---- Scrollbar and overflow fade (styled.rs:166-196) ----

// ScrollbarWidth sets the space reserved for the scrollbar
// (styled.rs:166).
func (s *Styled[Self]) ScrollbarWidth[_ lazy](width AbsoluteLength) Self {
	s.StyleMut().ScrollbarWidth = Some(width)
	return s.self
}

// OverflowFade sets per-edge fade distances for clipped overflow
// (styled.rs:176).
func (s *Styled[Self]) OverflowFade[_ lazy](fade AbsoluteLengthEdges) Self {
	st := s.StyleMut()
	st.OverflowFadeTop = Some(fade.Top)
	st.OverflowFadeRight = Some(fade.Right)
	st.OverflowFadeBottom = Some(fade.Bottom)
	st.OverflowFadeLeft = Some(fade.Left)
	return s.self
}

// OverflowFadeX sets equal left/right fades and clears top/bottom
// (styled.rs:188, Edges::horizontal).
func (s *Styled[Self]) OverflowFadeX[_ lazy](fade AbsoluteLength) Self {
	return s.OverflowFade(AbsoluteLengthEdges{Left: fade, Right: fade})
}

// OverflowFadeY sets equal top/bottom fades and clears left/right
// (styled.rs:194, Edges::vertical).
func (s *Styled[Self]) OverflowFadeY[_ lazy](fade AbsoluteLength) Self {
	return s.OverflowFade(AbsoluteLengthEdges{Top: fade, Bottom: fade})
}

// ---- Whitespace, truncation and text alignment (styled.rs:200-293) ----

// Whitespace sets the white space handling (styled.rs:200).
func (s *Styled[Self]) Whitespace[_ lazy](whiteSpace WhiteSpace) Self {
	s.TextStyleMut().WhiteSpace = Some(whiteSpace)
	return s.self
}

// WhitespaceNormal sets white space to normal (styled.rs:207).
func (s *Styled[Self]) WhitespaceNormal[_ lazy]() Self { return s.Whitespace(WhiteSpaceNormal) }

// WhitespaceNowrap sets white space to nowrap (styled.rs:214).
func (s *Styled[Self]) WhitespaceNowrap[_ lazy]() Self { return s.Whitespace(WhiteSpaceNowrap) }

// WhitespacePre sets white space to pre (browser-core 38, CSS
// white-space: pre: spaces, tabs and newlines preserved, no wrapping,
// tabs at 8-space stops).
func (s *Styled[Self]) WhitespacePre[_ lazy]() Self { return s.Whitespace(WhiteSpacePre) }

// WhitespacePreLine sets white space to pre-line (browser-core 38,
// CSS white-space: pre-line: spaces collapse, newlines break).
func (s *Styled[Self]) WhitespacePreLine[_ lazy]() Self { return s.Whitespace(WhiteSpacePreLine) }

// WhitespacePreWrap sets white space to pre-wrap (browser-core 38,
// CSS white-space: pre-wrap: spaces, tabs and newlines preserved,
// wrapping on).
func (s *Styled[Self]) WhitespacePreWrap[_ lazy]() Self { return s.Whitespace(WhiteSpacePreWrap) }

// TextEllipsis truncates overflowing text with an ellipsis at the end
// (styled.rs:221).
func (s *Styled[Self]) TextEllipsis[_ lazy]() Self {
	return s.TextOverflow(TextOverflow{Kind: TextOverflowTruncate, Ellipsis: Ellipsis})
}

// TextEllipsisStart truncates with an ellipsis at the start
// (styled.rs:229).
func (s *Styled[Self]) TextEllipsisStart[_ lazy]() Self {
	return s.TextOverflow(TextOverflow{Kind: TextOverflowTruncateStart, Ellipsis: Ellipsis})
}

// TextEllipsisMiddle truncates with an ellipsis in the middle
// (styled.rs:237).
func (s *Styled[Self]) TextEllipsisMiddle[_ lazy]() Self {
	return s.TextOverflow(TextOverflow{Kind: TextOverflowTruncateMiddle, Ellipsis: Ellipsis})
}

// TextOverflow sets the text overflow behavior (styled.rs:243).
func (s *Styled[Self]) TextOverflow[_ lazy](overflow TextOverflow) Self {
	s.TextStyleMut().TextOverflow = Some(overflow)
	return s.self
}

// TextAlign sets the text alignment (styled.rs:249).
func (s *Styled[Self]) TextAlign[_ lazy](align TextAlign) Self {
	s.TextStyleMut().TextAlign = Some(align)
	return s.self
}

// TextLeft aligns text left (styled.rs:255).
func (s *Styled[Self]) TextLeft[_ lazy]() Self { return s.TextAlign(TextAlignLeft) }

// TextCenter centers text (styled.rs:260).
func (s *Styled[Self]) TextCenter[_ lazy]() Self { return s.TextAlign(TextAlignCenter) }

// TextRight aligns text right (styled.rs:265).
func (s *Styled[Self]) TextRight[_ lazy]() Self { return s.TextAlign(TextAlignRight) }

// LetterSpacing sets the letter spacing in logical pixels
// (styled.rs:270).
func (s *Styled[Self]) LetterSpacing[_ lazy](spacing float32) Self {
	s.TextStyleMut().LetterSpacing = Some(spacing)
	return s.self
}

// LetterSpacingEm sets the letter spacing in ems of the element's own
// font size (browser-core 38, CSS letter-spacing: <length> in em —
// Tailwind's named tracking-* values; resolved against the font size
// at run-build time, ToRunAt).
func (s *Styled[Self]) LetterSpacingEm[_ lazy](em float32) Self {
	s.TextStyleMut().LetterSpacingEm = Some(em)
	return s.self
}

// TextTransform sets the case transformation (styled.rs:276).
func (s *Styled[Self]) TextTransform[_ lazy](transform TextTransform) Self {
	s.TextStyleMut().TextTransform = Some(transform)
	return s.self
}

// Truncate hides overflow, prevents wrapping and truncates with an
// ellipsis (styled.rs:283). Text truncates to the width its layout node
// is given (text_truncate.go): a FlexCol box stretches its text child to
// the box width, while in a flex row a text child's automatic minimum
// width is its whole unwrapped width (Taffy's min-size: auto), so it
// overflows and clips instead.
func (s *Styled[Self]) Truncate[_ lazy]() Self {
	s.OverflowHidden()
	s.WhitespaceNowrap()
	return s.TextEllipsis()
}

// LineClamp sets the number of lines shown before truncating, and hides
// overflow (styled.rs:289).
func (s *Styled[Self]) LineClamp[_ lazy](lines int) Self {
	s.TextStyleMut().LineClamp = Some(lines)
	return s.OverflowHidden()
}

// ---- Flex (styled.rs:297-434) ----

// FlexDirection sets the flex direction (styled.rs:297).
func (s *Styled[Self]) FlexDirection[_ lazy](direction FlexDirection) Self {
	s.StyleMut().FlexDirection = Some(direction)
	return s.self
}

// FlexCol sets the flex direction to column (styled.rs:304).
func (s *Styled[Self]) FlexCol[_ lazy]() Self { return s.FlexDirection(FlexDirectionColumn) }

// FlexColReverse sets the flex direction to column-reverse
// (styled.rs:311).
func (s *Styled[Self]) FlexColReverse[_ lazy]() Self {
	return s.FlexDirection(FlexDirectionColumnReverse)
}

// FlexRow sets the flex direction to row (styled.rs:318).
func (s *Styled[Self]) FlexRow[_ lazy]() Self { return s.FlexDirection(FlexDirectionRow) }

// FlexRowReverse sets the flex direction to row-reverse (styled.rs:325).
func (s *Styled[Self]) FlexRowReverse[_ lazy]() Self { return s.FlexDirection(FlexDirectionRowReverse) }

// setFlex writes grow, shrink and basis together.
func (s *Styled[Self]) setFlex[_ lazy](grow, shrink float32, basis Length) Self {
	st := s.StyleMut()
	st.FlexGrow = Some(grow)
	st.FlexShrink = Some(shrink)
	st.FlexBasis = Some(basis)
	return s.self
}

// Flex1 lets the item grow and shrink, ignoring its initial size
// (styled.rs:332: grow 1, shrink 1, basis relative(0)).
func (s *Styled[Self]) Flex1[_ lazy]() Self { return s.setFlex(1, 1, DefiniteLengthOf(Fraction(0))) }

// FlexAuto lets the item grow and shrink from its initial size
// (styled.rs:341).
func (s *Styled[Self]) FlexAuto[_ lazy]() Self { return s.setFlex(1, 1, AutoLength()) }

// FlexInitial lets the item shrink but not grow (styled.rs:350).
func (s *Styled[Self]) FlexInitial[_ lazy]() Self { return s.setFlex(0, 1, AutoLength()) }

// FlexNone prevents the item from growing or shrinking (styled.rs:359).
func (s *Styled[Self]) FlexNone[_ lazy]() Self { return s.setFlex(0, 0, AutoLength()) }

// FlexBasis sets the initial main-axis size (styled.rs:368).
func (s *Styled[Self]) FlexBasis[L IntoLength](basis L) Self {
	s.StyleMut().FlexBasis = Some(LengthOf(basis))
	return s.self
}

// FlexGrow sets the grow factor (styled.rs:375).
func (s *Styled[Self]) FlexGrow[_ lazy](grow float32) Self {
	s.StyleMut().FlexGrow = Some(grow)
	return s.self
}

// FlexGrow0 disables growth (styled.rs:382).
func (s *Styled[Self]) FlexGrow0[_ lazy]() Self { return s.FlexGrow(0) }

// FlexGrow1 enables growth (styled.rs:389).
func (s *Styled[Self]) FlexGrow1[_ lazy]() Self { return s.FlexGrow(1) }

// FlexShrink sets the shrink factor (styled.rs:396).
func (s *Styled[Self]) FlexShrink[_ lazy](shrink float32) Self {
	s.StyleMut().FlexShrink = Some(shrink)
	return s.self
}

// FlexShrink0 disables shrinking (styled.rs:403).
func (s *Styled[Self]) FlexShrink0[_ lazy]() Self { return s.FlexShrink(0) }

// FlexShrink1 enables shrinking (styled.rs:410).
func (s *Styled[Self]) FlexShrink1[_ lazy]() Self { return s.FlexShrink(1) }

// FlexWrap lets flex items wrap (styled.rs:417).
func (s *Styled[Self]) FlexWrap[_ lazy]() Self {
	s.StyleMut().FlexWrap = Some(FlexWrapWrap)
	return s.self
}

// FlexWrapReverse wraps flex items in reverse (styled.rs:424).
func (s *Styled[Self]) FlexWrapReverse[_ lazy]() Self {
	s.StyleMut().FlexWrap = Some(FlexWrapWrapReverse)
	return s.self
}

// FlexNowrap prevents flex items from wrapping (styled.rs:431).
func (s *Styled[Self]) FlexNowrap[_ lazy]() Self {
	s.StyleMut().FlexWrap = Some(FlexWrapNoWrap)
	return s.self
}

// ---- Alignment (styled.rs:438-624) ----

// alignItems writes align_items.
func (s *Styled[Self]) alignItems[_ lazy](value AlignItems) Self {
	s.StyleMut().AlignItems = Some(value)
	return s.self
}

// ItemsStart aligns items to the cross-axis start (styled.rs:438:
// FlexStart).
func (s *Styled[Self]) ItemsStart[_ lazy]() Self { return s.alignItems(AlignItemsFlexStart) }

// ItemsEnd aligns items to the cross-axis end (styled.rs:445: FlexEnd).
func (s *Styled[Self]) ItemsEnd[_ lazy]() Self { return s.alignItems(AlignItemsFlexEnd) }

// ItemsCenter centers items on the cross axis (styled.rs:452).
func (s *Styled[Self]) ItemsCenter[_ lazy]() Self { return s.alignItems(AlignItemsCenter) }

// ItemsBaseline aligns items on their baselines (styled.rs:459).
func (s *Styled[Self]) ItemsBaseline[_ lazy]() Self { return s.alignItems(AlignItemsBaseline) }

// ItemsStretch stretches items on the cross axis (styled.rs:466).
func (s *Styled[Self]) ItemsStretch[_ lazy]() Self { return s.alignItems(AlignItemsStretch) }

// alignSelf writes align_self.
func (s *Styled[Self]) alignSelf[_ lazy](value AlignItems) Self {
	s.StyleMut().AlignSelf = Some(value)
	return s.self
}

// SelfStart aligns this item to the cross-axis start (styled.rs:473:
// Start).
func (s *Styled[Self]) SelfStart[_ lazy]() Self { return s.alignSelf(AlignItemsStart) }

// SelfEnd aligns this item to the cross-axis end (styled.rs:480: End).
func (s *Styled[Self]) SelfEnd[_ lazy]() Self { return s.alignSelf(AlignItemsEnd) }

// SelfFlexStart aligns this item to the flex start (styled.rs:487).
func (s *Styled[Self]) SelfFlexStart[_ lazy]() Self { return s.alignSelf(AlignItemsFlexStart) }

// SelfFlexEnd aligns this item to the flex end (styled.rs:494).
func (s *Styled[Self]) SelfFlexEnd[_ lazy]() Self { return s.alignSelf(AlignItemsFlexEnd) }

// SelfCenter centers this item on the cross axis (styled.rs:501).
func (s *Styled[Self]) SelfCenter[_ lazy]() Self { return s.alignSelf(AlignItemsCenter) }

// SelfBaseline aligns this item on the baseline (styled.rs:508).
func (s *Styled[Self]) SelfBaseline[_ lazy]() Self { return s.alignSelf(AlignItemsBaseline) }

// SelfStretch stretches this item on the cross axis (styled.rs:515).
func (s *Styled[Self]) SelfStretch[_ lazy]() Self { return s.alignSelf(AlignItemsStretch) }

// justify writes justify_content.
func (s *Styled[Self]) justify[_ lazy](value AlignContent) Self {
	s.StyleMut().JustifyContent = Some(value)
	return s.self
}

// JustifyStart packs items at the main-axis start (styled.rs:522).
func (s *Styled[Self]) JustifyStart[_ lazy]() Self { return s.justify(AlignContentStart) }

// JustifyEnd packs items at the main-axis end (styled.rs:529).
func (s *Styled[Self]) JustifyEnd[_ lazy]() Self { return s.justify(AlignContentEnd) }

// JustifyCenter centers items on the main axis (styled.rs:536).
func (s *Styled[Self]) JustifyCenter[_ lazy]() Self { return s.justify(AlignContentCenter) }

// JustifyBetween distributes space between items (styled.rs:544).
func (s *Styled[Self]) JustifyBetween[_ lazy]() Self { return s.justify(AlignContentSpaceBetween) }

// JustifyAround distributes space around items (styled.rs:552).
func (s *Styled[Self]) JustifyAround[_ lazy]() Self { return s.justify(AlignContentSpaceAround) }

// JustifyEvenly distributes space evenly (styled.rs:562).
func (s *Styled[Self]) JustifyEvenly[_ lazy]() Self { return s.justify(AlignContentSpaceEvenly) }

// alignContent writes align_content.
func (s *Styled[Self]) alignContent[_ lazy](value AlignContent) Self {
	s.StyleMut().AlignContent = Some(value)
	return s.self
}

// ContentNormal clears align-content (styled.rs:569: align_content =
// None, the refinement absent).
func (s *Styled[Self]) ContentNormal[_ lazy]() Self {
	s.StyleMut().AlignContent = Optional[AlignContent]{}
	return s.self
}

// ContentCenter packs content in the cross-axis center (styled.rs:576).
func (s *Styled[Self]) ContentCenter[_ lazy]() Self { return s.alignContent(AlignContentCenter) }

// ContentStart packs content at the cross-axis start (styled.rs:583:
// FlexStart).
func (s *Styled[Self]) ContentStart[_ lazy]() Self { return s.alignContent(AlignContentFlexStart) }

// ContentEnd packs content at the cross-axis end (styled.rs:590:
// FlexEnd).
func (s *Styled[Self]) ContentEnd[_ lazy]() Self { return s.alignContent(AlignContentFlexEnd) }

// ContentBetween distributes content with space between
// (styled.rs:598).
func (s *Styled[Self]) ContentBetween[_ lazy]() Self { return s.alignContent(AlignContentSpaceBetween) }

// ContentAround distributes content with space around (styled.rs:606).
func (s *Styled[Self]) ContentAround[_ lazy]() Self { return s.alignContent(AlignContentSpaceAround) }

// ContentEvenly distributes content evenly (styled.rs:614).
func (s *Styled[Self]) ContentEvenly[_ lazy]() Self { return s.alignContent(AlignContentSpaceEvenly) }

// ContentStretch stretches content on the cross axis (styled.rs:621).
func (s *Styled[Self]) ContentStretch[_ lazy]() Self { return s.alignContent(AlignContentStretch) }

// ---- Aspect ratio, background and border pattern (styled.rs:628-668) ----

// AspectRatio sets the width/height ratio (styled.rs:628).
func (s *Styled[Self]) AspectRatio[_ lazy](ratio float32) Self {
	s.StyleMut().AspectRatio = Some(ratio)
	return s.self
}

// AspectSquare sets the aspect ratio to 1 (styled.rs:635).
func (s *Styled[Self]) AspectSquare[_ lazy]() Self { return s.AspectRatio(1) }

// Bg sets the background (styled.rs:641; the pinned Fill's one variant
// is a Background).
func (s *Styled[Self]) Bg[F IntoBackground](fill F) Self {
	s.StyleMut().Background = Some(BackgroundOf(fill))
	return s.self
}

// BorderDashed sets the border style to dashed (styled.rs:651).
func (s *Styled[Self]) BorderDashed[_ lazy]() Self {
	s.StyleMut().BorderStyle = Some(BorderStyleDashed)
	return s.self
}

// BorderDashedLength sets the dash length as a multiple of the border
// width, clamped at zero (styled.rs:657).
func (s *Styled[Self]) BorderDashedLength[_ lazy](lengthPerBorderWidth float32) Self {
	s.StyleMut().BorderDashedLength = Some(max32(lengthPerBorderWidth, 0))
	return s.self
}

// BorderDashedGap sets the dash gap as a multiple of the border width,
// clamped at zero (styled.rs:664).
func (s *Styled[Self]) BorderDashedGap[_ lazy](gapPerBorderWidth float32) Self {
	s.StyleMut().BorderDashedGap = Some(max32(gapPerBorderWidth, 0))
	return s.self
}

// ---- Outline (CSS css-ui-4 §outline; browser-core ticket 48) ----

// Outline sets the outline width with a solid style (outline: <width>
// solid): a frame painted above the element's content, outset by the
// offset (OutlineOffset).
func (s *Styled[Self]) Outline[_ lazy](width float32) Self {
	st := s.StyleMut()
	st.OutlineWidth = Some(width)
	st.OutlineStyle = Some(OutlineSolid)
	return s.self
}

// OutlineColor sets the outline colour (outline-color; currentColor
// when left out).
func (s *Styled[Self]) OutlineColor[F IntoBackground](fill F) Self {
	s.StyleMut().OutlineColor = Some(RingColorOf(BackgroundOf(fill)))
	return s.self
}

// OutlineOffset sets the outline offset (outline-offset): the gap
// between the border edge and the outline; a negative value draws the
// outline inside the border box.
func (s *Styled[Self]) OutlineOffset[_ lazy](offset float32) Self {
	s.StyleMut().OutlineOffset = Some(offset)
	return s.self
}

// OutlineDashed sets the outline style to dashed (outline-style:
// dashed; the native scene's dashed border pattern).
func (s *Styled[Self]) OutlineDashed[_ lazy]() Self {
	s.StyleMut().OutlineStyle = Some(OutlineDashed)
	return s.self
}

// OutlineNone sets the outline style to none (outline-style: none) —
// the default focus ring's per-element opt-out: a focused
// focus-visible element draws no ring with it, like Chrome's
// outline-none.
func (s *Styled[Self]) OutlineNone[_ lazy]() Self {
	s.StyleMut().OutlineStyle = Some(OutlineNone)
	return s.self
}

// ---- Text (styled.rs:679-908) ----

// TextColor sets the text color, cascading to children (styled.rs:679).
func (s *Styled[Self]) TextColor[_ lazy](color Hsla) Self {
	s.TextStyleMut().Color = Some(color)
	return s.self
}

// FontWeight sets the font weight, cascading to children
// (styled.rs:687; FontWeight is the port's float32 weight).
func (s *Styled[Self]) FontWeight[_ lazy](weight float32) Self {
	s.TextStyleMut().FontWeight = Some(weight)
	return s.self
}

// TextBg sets the text background color, cascading to children
// (styled.rs:695).
func (s *Styled[Self]) TextBg[_ lazy](bg Hsla) Self {
	s.TextStyleMut().BackgroundColor = Some(bg)
	return s.self
}

// TextSize sets the font size, cascading to children (styled.rs:703).
func (s *Styled[Self]) TextSize[_ lazy](size AbsoluteLength) Self {
	s.TextStyleMut().FontSize = Some(size)
	return s.self
}

// TextXs sets the font size to 0.75rem (styled.rs:710).
func (s *Styled[Self]) TextXs[_ lazy]() Self { return s.TextSize(RemsOf(0.75)) }

// TextSm sets the font size to 0.875rem (styled.rs:717).
func (s *Styled[Self]) TextSm[_ lazy]() Self { return s.TextSize(RemsOf(0.875)) }

// TextBase sets the font size to 1rem (styled.rs:724).
func (s *Styled[Self]) TextBase[_ lazy]() Self { return s.TextSize(RemsOf(1.0)) }

// TextLg sets the font size to 1.125rem (styled.rs:731).
func (s *Styled[Self]) TextLg[_ lazy]() Self { return s.TextSize(RemsOf(1.125)) }

// TextXl sets the font size to 1.25rem (styled.rs:738).
func (s *Styled[Self]) TextXl[_ lazy]() Self { return s.TextSize(RemsOf(1.25)) }

// Text2xl sets the font size to 1.5rem (styled.rs:745).
func (s *Styled[Self]) Text2xl[_ lazy]() Self { return s.TextSize(RemsOf(1.5)) }

// Text3xl sets the font size to 1.875rem (styled.rs:752).
func (s *Styled[Self]) Text3xl[_ lazy]() Self { return s.TextSize(RemsOf(1.875)) }

// Italic sets the font style to italic (styled.rs:759).
func (s *Styled[Self]) Italic[_ lazy]() Self {
	s.TextStyleMut().FontStyle = Some(FontStyleItalic)
	return s.self
}

// NotItalic sets the font style to normal (styled.rs:766).
func (s *Styled[Self]) NotItalic[_ lazy]() Self {
	s.TextStyleMut().FontStyle = Some(FontStyleNormal)
	return s.self
}

// Underline sets an underline with the browser's auto geometry
// (styled.rs:773 sets 1px; the ticket-06 browser rule replaces the
// thickness and the offset with Chrome's — see UnderlineStyle.
// AutoThickness and paintRunDecorations).
func (s *Styled[Self]) Underline[_ lazy]() Self {
	s.TextStyleMut().Underline = Some(UnderlineStyle{AutoThickness: true})
	return s.self
}

// LineThrough sets a 1px strikethrough (styled.rs:784).
func (s *Styled[Self]) LineThrough[_ lazy]() Self {
	s.TextStyleMut().Strikethrough = Some(StrikethroughStyle{Thickness: 1})
	return s.self
}

// TextDecorationNone removes the underline refinement (styled.rs:796:
// underline = None).
func (s *Styled[Self]) TextDecorationNone[_ lazy]() Self {
	s.TextStyleMut().Underline = Optional[UnderlineStyle]{}
	return s.self
}

// underlineWith edits the underline, inserting the default first
// (`underline.get_or_insert_with(Default::default)`). The color pointer
// is replaced, never written through, so snapshots stay independent.
func (s *Styled[Self]) underlineWith[_ lazy](edit func(*UnderlineStyle)) Self {
	text := s.TextStyleMut()
	underline, _ := text.Underline.Get()
	edit(&underline)
	text.Underline = Some(underline)
	return s.self
}

// TextDecorationColor sets the underline color (styled.rs:802).
func (s *Styled[Self]) TextDecorationColor[_ lazy](color Hsla) Self {
	return s.underlineWith(func(u *UnderlineStyle) { c := color; u.Color = &c })
}

// TextDecorationSolid sets a solid underline (styled.rs:811).
func (s *Styled[Self]) TextDecorationSolid[_ lazy]() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Wavy = false })
}

// TextDecorationWavy sets a wavy underline (styled.rs:820).
func (s *Styled[Self]) TextDecorationWavy[_ lazy]() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Wavy = true })
}

// TextDecoration0 sets a 0px underline thickness (styled.rs:829).
func (s *Styled[Self]) TextDecoration0[_ lazy]() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 0 })
}

// TextDecoration1 sets a 1px underline thickness (styled.rs:838).
func (s *Styled[Self]) TextDecoration1[_ lazy]() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 1 })
}

// TextDecoration2 sets a 2px underline thickness (styled.rs:847).
func (s *Styled[Self]) TextDecoration2[_ lazy]() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 2 })
}

// TextDecoration4 sets a 4px underline thickness (styled.rs:856).
func (s *Styled[Self]) TextDecoration4[_ lazy]() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 4 })
}

// TextDecoration8 sets an 8px underline thickness (styled.rs:865).
func (s *Styled[Self]) TextDecoration8[_ lazy]() Self {
	return s.underlineWith(func(u *UnderlineStyle) { u.Thickness = 8 })
}

// FontFamily sets the font family (styled.rs:873). The CSS generic
// families ("sans-serif", "serif", "monospace", case-insensitive)
// resolve to Chrome's Windows defaults at the native boundary — Arial,
// Times New Roman and Consolas (text.go resolveGenericFontFamily).
func (s *Styled[Self]) FontFamily[_ lazy](familyName string) Self {
	s.TextStyleMut().FontFamily = Some(familyName)
	return s.self
}

// FontFeatures sets the font features (styled.rs:879).
func (s *Styled[Self]) FontFeatures[_ lazy](features []FontFeature) Self {
	s.TextStyleMut().FontFeatures = Some(slices.Clone(features))
	return s.self
}

// Font sets family, features, weight and style, and the fallbacks
// (styled.rs:885: fallbacks are assigned as-is, so an empty list makes
// the refinement absent).
func (s *Styled[Self]) Font[_ lazy](font FontDescriptor) Self {
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
func (s *Styled[Self]) Opacity[_ lazy](opacity float32) Self {
	s.StyleMut().Opacity = Some(opacity)
	return s.self
}

// Transition sets the element's transition rules — CSS transition
// (browser-core ticket 44; see transition.go). A covered style property's
// resolved change animates over the rule's duration, delay and easing;
// the last matching rule for a property wins, and a TransitionAll rule
// covers every interpolable property. Build rules with
// NewTransitionRule (the CSS shorthand defaults: the `ease` timing
// function, no delay).
func (s *Styled[Self]) Transition[_ lazy](rules ...TransitionRule) Self {
	s.StyleMut().Transition = Some(rules)
	return s.self
}

// ---- Grid (styled.rs:917-1054) ----

// GridCols sets the column template (styled.rs:917: min size Zero).
func (s *Styled[Self]) GridCols[_ lazy](cols uint16) Self {
	s.StyleMut().GridCols = Some(GridTemplate{Repeat: cols, MinSize: GridTemplateMinZero})
	return s.self
}

// GridColsMinContent sets min-content column minimums (styled.rs:927).
func (s *Styled[Self]) GridColsMinContent[_ lazy](cols uint16) Self {
	s.StyleMut().GridCols = Some(GridTemplate{Repeat: cols, MinSize: GridTemplateMinMinContent})
	return s.self
}

// GridColsMaxContent sets max-content column sizing (styled.rs:936).
func (s *Styled[Self]) GridColsMaxContent[_ lazy](cols uint16) Self {
	s.StyleMut().GridCols = Some(GridTemplate{Repeat: cols, MinSize: GridTemplateMinMaxContent})
	return s.self
}

// GridRows sets the row template (styled.rs:945).
func (s *Styled[Self]) GridRows[_ lazy](rows uint16) Self {
	s.StyleMut().GridRows = Some(GridTemplate{Repeat: rows, MinSize: GridTemplateMinZero})
	return s.self
}

// GridRowsMinContent sets min-content row minimums (styled.rs:955).
func (s *Styled[Self]) GridRowsMinContent[_ lazy](rows uint16) Self {
	s.StyleMut().GridRows = Some(GridTemplate{Repeat: rows, MinSize: GridTemplateMinMinContent})
	return s.self
}

// GridRowsMaxContent sets max-content row sizing (styled.rs:964).
func (s *Styled[Self]) GridRowsMaxContent[_ lazy](rows uint16) Self {
	s.StyleMut().GridRows = Some(GridTemplate{Repeat: rows, MinSize: GridTemplateMinMaxContent})
	return s.self
}

// gridLocation edits the grid location, inserting the default first
// (style.rs:416 grid_location_mut: get_or_insert_default).
func (s *Styled[Self]) gridLocation[_ lazy](edit func(*GridLocation)) Self {
	st := s.StyleMut()
	location, _ := st.GridLocation.Get()
	edit(&location)
	st.GridLocation = Some(location)
	return s.self
}

// ColStart sets the column start line (styled.rs:973).
func (s *Styled[Self]) ColStart[_ lazy](start int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.Start = LineGridPlacement(start) })
}

// ColStartAuto sets the column start to auto (styled.rs:980).
func (s *Styled[Self]) ColStartAuto[_ lazy]() Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.Start = AutoGridPlacement() })
}

// ColEnd sets the column end line (styled.rs:987).
func (s *Styled[Self]) ColEnd[_ lazy](end int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.End = LineGridPlacement(end) })
}

// ColEndAuto sets the column end to auto (styled.rs:994).
func (s *Styled[Self]) ColEndAuto[_ lazy]() Self {
	return s.gridLocation(func(l *GridLocation) { l.Column.End = AutoGridPlacement() })
}

// ColSpan spans the given number of columns (styled.rs:1001).
func (s *Styled[Self]) ColSpan[_ lazy](span uint16) Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Column = GridPlacementRange{Start: SpanGridPlacement(span), End: SpanGridPlacement(span)}
	})
}

// ColSpanFull spans every column (styled.rs:1008: Line(1)..Line(-1)).
func (s *Styled[Self]) ColSpanFull[_ lazy]() Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Column = GridPlacementRange{Start: LineGridPlacement(1), End: LineGridPlacement(-1)}
	})
}

// RowStart sets the row start line (styled.rs:1015).
func (s *Styled[Self]) RowStart[_ lazy](start int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.Start = LineGridPlacement(start) })
}

// RowStartAuto sets the row start to auto (styled.rs:1022).
func (s *Styled[Self]) RowStartAuto[_ lazy]() Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.Start = AutoGridPlacement() })
}

// RowEnd sets the row end line (styled.rs:1029).
func (s *Styled[Self]) RowEnd[_ lazy](end int16) Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.End = LineGridPlacement(end) })
}

// RowEndAuto sets the row end to auto (styled.rs:1036).
func (s *Styled[Self]) RowEndAuto[_ lazy]() Self {
	return s.gridLocation(func(l *GridLocation) { l.Row.End = AutoGridPlacement() })
}

// RowSpan spans the given number of rows (styled.rs:1043).
func (s *Styled[Self]) RowSpan[_ lazy](span uint16) Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Row = GridPlacementRange{Start: SpanGridPlacement(span), End: SpanGridPlacement(span)}
	})
}

// RowSpanFull spans every row (styled.rs:1050).
func (s *Styled[Self]) RowSpanFull[_ lazy]() Self {
	return s.gridLocation(func(l *GridLocation) {
		l.Row = GridPlacementRange{Start: LineGridPlacement(1), End: LineGridPlacement(-1)}
	})
}

// ---- Debug and corner smoothing (styled.rs:1058-1085) ----

// Debug draws a red debug outline around this element (styled.rs:1058;
// cfg(debug_assertions) in the pin, always available in the port).
func (s *Styled[Self]) Debug[_ lazy]() Self {
	s.StyleMut().Debug = Some(true)
	return s.self
}

// DebugBelow draws the debug outline on this element and every
// conforming descendant (styled.rs:1065).
func (s *Styled[Self]) DebugBelow[_ lazy]() Self {
	s.StyleMut().DebugBelow = Some(true)
	return s.self
}

// RoundedSmoothing sets the corner smoothing, 0 (circular) to 1 (maximum)
// (styled.rs:1072; the pinned debug_assert on the range is not ported —
// paint clamps to [0, 1]).
func (s *Styled[Self]) RoundedSmoothing[_ lazy](amount float32) Self {
	s.StyleMut().CornerSmoothing = Some(amount)
	return s.self
}

// RoundedSmoothingIos sets corner smoothing to 0.6 (styled.rs:1082).
func (s *Styled[Self]) RoundedSmoothingIos[_ lazy]() Self { return s.RoundedSmoothing(0.6) }
