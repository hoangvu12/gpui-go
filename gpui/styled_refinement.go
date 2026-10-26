package gpui

import (
	"math"
	"slices"
)

// This file ports the pinned Refineable derivation of Style (ticket
// base-05; reference crates/gpui/src/style.rs:233-406 with
// #[derive(Refineable)], refineable/derive_refineable). Rust derives a
// StyleRefinement whose every field is optional and whose nested
// #[refineable] fields (Point/Edges/Size/Corners/RingStyle) become nested
// refinements. The port flattens those geometric containers into one
// optional field per leaf, named by the field path (padding.left ->
// PaddingLeft, corner_radii.top_left -> CornerRadiiTopLeft,
// ring.width -> RingWidth); the text refinement stays nested as the
// existing TextStyleRefinement. Option<T> style fields refine as
// Optional[T] exactly like the derive: a present value replaces the
// target, an absent one leaves it.

// Optional distinguishes an absent refinement from an explicit value,
// including zero or false (the derive's Option<T>). The zero value is
// absent.
type Optional[T any] struct {
	value   T
	present bool
}

// Some makes a present value.
func Some[T any](value T) Optional[T] { return Optional[T]{value: value, present: true} }

// Get returns the value and whether it is present.
func (o Optional[T]) Get() (T, bool) { return o.value, o.present }

// IsSome reports whether a value is present.
func (o Optional[T]) IsSome() bool { return o.present }

// UnwrapOr returns the value, or fallback when absent.
func (o Optional[T]) UnwrapOr(fallback T) T {
	if o.present {
		return o.value
	}
	return fallback
}

// mergeOptional applies a present src onto dst (the derive's
// `if src.is_some() { dst = src }`).
func mergeOptional[T any](dst *Optional[T], src Optional[T]) {
	if src.present {
		*dst = src
	}
}

// refineValue writes a present refinement into a required style field.
func refineValue[T any](dst *T, src Optional[T]) {
	if src.present {
		*dst = src.value
	}
}

// refinePointer writes a present refinement into an Option style field
// (a fresh copy, so styles never alias refinements).
func refinePointer[T any](dst **T, src Optional[T]) {
	if src.present {
		v := src.value
		*dst = &v
	}
}

// refineGridTracks writes a present track list into a style field: a
// present list replaces the axis (CSS track lists replace, never merge).
func refineGridTracks(dst *[]GridTrack, src Optional[[]GridTrack]) {
	if src.present {
		*dst = src.value
	}
}

// refineAreaTemplate writes a present area template into the style's
// optional field (a present template replaces any earlier one).
func refineAreaTemplate(dst **GridAreaTemplate, src Optional[*GridAreaTemplate]) {
	if src.present {
		*dst = src.value
	}
}

// StyleRefinement is the refinement of Style (the derived
// StyleRefinement of style.rs:233): every field is optional and Refine
// applies only the present ones. See the file comment for the flattened
// field naming.
type StyleRefinement struct {
	Display    Optional[Display]
	Visibility Optional[Visibility]

	OverflowX Optional[Overflow]
	OverflowY Optional[Overflow]

	OverflowFadeTop    Optional[AbsoluteLength]
	OverflowFadeRight  Optional[AbsoluteLength]
	OverflowFadeBottom Optional[AbsoluteLength]
	OverflowFadeLeft   Optional[AbsoluteLength]

	ScrollbarWidth        Optional[AbsoluteLength]
	ScrollbarGutter       Optional[ScrollbarGutter]
	AllowConcurrentScroll Optional[bool]
	RestrictScrollToAxis  Optional[bool]

	OverscrollBehaviorX Optional[OverscrollBehavior]
	OverscrollBehaviorY Optional[OverscrollBehavior]

	Position Optional[Position]

	InsetTop    Optional[Length]
	InsetRight  Optional[Length]
	InsetBottom Optional[Length]
	InsetLeft   Optional[Length]

	SizeWidth     Optional[Length]
	SizeHeight    Optional[Length]
	MinSizeWidth  Optional[Length]
	MinSizeHeight Optional[Length]
	MaxSizeWidth  Optional[Length]
	MaxSizeHeight Optional[Length]

	AspectRatio Optional[float32]

	MarginTop    Optional[Length]
	MarginRight  Optional[Length]
	MarginBottom Optional[Length]
	MarginLeft   Optional[Length]

	PaddingTop    Optional[DefiniteLength]
	PaddingRight  Optional[DefiniteLength]
	PaddingBottom Optional[DefiniteLength]
	PaddingLeft   Optional[DefiniteLength]

	BorderWidthsTop    Optional[AbsoluteLength]
	BorderWidthsRight  Optional[AbsoluteLength]
	BorderWidthsBottom Optional[AbsoluteLength]
	BorderWidthsLeft   Optional[AbsoluteLength]

	AlignItems     Optional[AlignItems]
	AlignSelf      Optional[AlignItems]
	VerticalAlign  Optional[VerticalAlign]
	AlignContent   Optional[AlignContent]
	JustifyContent Optional[AlignContent]

	GapWidth  Optional[DefiniteLength]
	GapHeight Optional[DefiniteLength]

	FlexDirection Optional[FlexDirection]
	FlexWrap      Optional[FlexWrap]
	FlexBasis     Optional[Length]
	FlexGrow      Optional[float32]
	FlexShrink    Optional[float32]

	Background         Optional[Background]
	BorderColor        Optional[Background]
	BorderStyle        Optional[BorderStyle]
	BorderDashedLength Optional[float32]
	BorderDashedGap    Optional[float32]

	CornerRadiiTopLeft     Optional[AbsoluteLength]
	CornerRadiiTopRight    Optional[AbsoluteLength]
	CornerRadiiBottomRight Optional[AbsoluteLength]
	CornerRadiiBottomLeft  Optional[AbsoluteLength]
	CornerSmoothing        Optional[float32]

	BoxShadow Optional[[]BoxShadow]

	RingWidth      Optional[float32]
	RingColor      Optional[RingColor]
	InsetRingWidth Optional[float32]
	InsetRingColor Optional[RingColor]

	// The outline fields (CSS css-ui-4 §outline, browser-core ticket
	// 48), flattened outline.width/.color/.style/.offset like the
	// ring's.
	OutlineWidth  Optional[float32]
	OutlineColor  Optional[RingColor]
	OutlineStyle  Optional[OutlineStyleKind]
	OutlineOffset Optional[float32]

	Filter         Optional[[]Filter]
	BackdropFilter Optional[[]Filter]

	// The replaced-content options (CSS object-fit, object-position,
	// image-rendering; browser-core ticket 41): consumed by the img
	// element's paint, inert on anything else — like CSS, where they
	// only affect replaced elements.
	ObjectFit      Optional[ObjectFit]
	ObjectPosition Optional[ObjectPosition]
	ImageRendering Optional[ImageRendering]

	Text TextStyleRefinement

	// ForcedColorAdjust is the forced-color-adjust opt-out of the
	// forced-colours pass (browser-core ticket 29): None keeps the
	// element's authored colours under forced colours; Auto (absent) is
	// the CSS default.
	ForcedColorAdjust Optional[ForcedColorAdjust]

	MouseCursor Optional[CursorStyle]
	Opacity     Optional[float32]

	// Transition is the transition style property's refinement (CSS
	// transition, browser-core ticket 44): a present list replaces the
	// whole target list (the property is not inherited and CSS
	// cascade-level lists replace, not merge).
	Transition Optional[[]TransitionRule]

	// Transform is the CSS transform property's refinement (browser-
	// core ticket 56): a present list replaces the whole target list
	// (CSS transform lists replace, like the transition rules).
	Transform Optional[[]TransformFunction]
	// TransformOrigin is the CSS transform-origin's refinement; a
	// present value replaces the target (nil target = the CSS initial
	// 50% 50%).
	TransformOrigin Optional[TransformOrigin]

	GridCols     Optional[GridTemplate]
	GridRows     Optional[GridTemplate]
	GridLocation Optional[GridLocation]

	// GridColsList is the full column track list's refinement (CSS
	// grid-template-columns: 240px 1fr — browser-core 42): present
	// replaces the axis list (and takes precedence over the pinned
	// GridCols repeat form).
	GridColsList Optional[[]GridTrack]
	// GridRowsList is the full row track list's refinement (CSS
	// grid-template-rows).
	GridRowsList Optional[[]GridTrack]
	// GridAreas is the named area template's refinement (CSS
	// grid-template-areas).
	GridAreas Optional[*GridAreaTemplate]
	// GridAreaName is the item's named placement's refinement (CSS
	// grid-area: <name>); it takes precedence over GridLocation.
	GridAreaName Optional[string]

	Debug      Optional[bool]
	DebugBelow Optional[bool]
}

// Refine applies the present fields of r onto s (the derived
// Refineable::refine for Style).
func (s *Style) Refine(r *StyleRefinement) {
	s.refineBoxed(r, nil)
}

// styleBoxes are the boxed values of an element's last refined Style
// pointer fields (gpui-perf ticket 06). An element computes its style in
// request_layout, prepaint and paint from the same refinements; the box
// cache lets the later computations point at the first one's boxes when
// the value is bit-identical instead of allocating fresh copies. Style
// pointer fields are read-only after refinement (nothing writes through
// them), so sharing a box between Style values is safe, and boxes never
// alias the refinement itself.
type styleBoxes struct {
	alignItems, alignSelf        *AlignItems
	alignContent, justifyContent *AlignContent
	background, borderColor      *Background
	mouseCursor                  *CursorStyle
}

// refineBoxed writes a present refinement into an Option style field,
// reusing *box when it holds an equal value (see styleBoxes).
func refineBoxed[T any](dst **T, src Optional[T], box **T, equal func(a, b T) bool) {
	if !src.present {
		return
	}
	if *box != nil && equal(**box, src.value) {
		*dst = *box
		return
	}
	v := src.value
	*dst = &v
	*box = &v
}

func equalComparable[T comparable](a, b T) bool { return a == b }

// equalBackground compares backgrounds bit for bit (so -0 and +0 or NaN
// payloads never share a box): the arm, the solid's channels, and the
// gradient's direction, angle and stops.
func equalBackground(a, b Background) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == BackgroundKindLinearGradient {
		return a.Gradient.Direction == b.Gradient.Direction &&
			math.Float32bits(a.Gradient.Angle) == math.Float32bits(b.Gradient.Angle) &&
			equalGradientStop(a.Gradient.From, b.Gradient.From) &&
			equalGradientStop(a.Gradient.To, b.Gradient.To)
	}
	return math.Float32bits(a.Solid.H) == math.Float32bits(b.Solid.H) &&
		math.Float32bits(a.Solid.S) == math.Float32bits(b.Solid.S) &&
		math.Float32bits(a.Solid.L) == math.Float32bits(b.Solid.L) &&
		math.Float32bits(a.Solid.A) == math.Float32bits(b.Solid.A)
}

// equalGradientStop compares two gradient stops bit for bit.
func equalGradientStop(a, b GradientStop) bool {
	return math.Float32bits(a.Color.H) == math.Float32bits(b.Color.H) &&
		math.Float32bits(a.Color.S) == math.Float32bits(b.Color.S) &&
		math.Float32bits(a.Color.L) == math.Float32bits(b.Color.L) &&
		math.Float32bits(a.Color.A) == math.Float32bits(b.Color.A) &&
		math.Float32bits(a.Position) == math.Float32bits(b.Position)
}

// refineBoxed is Refine with a box cache; a nil cache boxes every value
// fresh, exactly like Refine.
func (s *Style) refineBoxed(r *StyleRefinement, boxes *styleBoxes) {
	if r == nil {
		return
	}
	if boxes == nil {
		boxes = &styleBoxes{}
	}
	refineValue(&s.Display, r.Display)
	refineValue(&s.Visibility, r.Visibility)
	refineValue(&s.OverflowX, r.OverflowX)
	refineValue(&s.OverflowY, r.OverflowY)
	refineValue(&s.OverflowFade.Top, r.OverflowFadeTop)
	refineValue(&s.OverflowFade.Right, r.OverflowFadeRight)
	refineValue(&s.OverflowFade.Bottom, r.OverflowFadeBottom)
	refineValue(&s.OverflowFade.Left, r.OverflowFadeLeft)
	refineValue(&s.ScrollbarWidth, r.ScrollbarWidth)
	refineValue(&s.ScrollbarGutter, r.ScrollbarGutter)
	refineValue(&s.OverscrollBehaviorX, r.OverscrollBehaviorX)
	refineValue(&s.OverscrollBehaviorY, r.OverscrollBehaviorY)
	refineValue(&s.AllowConcurrentScroll, r.AllowConcurrentScroll)
	refineValue(&s.RestrictScrollToAxis, r.RestrictScrollToAxis)
	refineValue(&s.Position, r.Position)
	refineValue(&s.Inset.Top, r.InsetTop)
	refineValue(&s.Inset.Right, r.InsetRight)
	refineValue(&s.Inset.Bottom, r.InsetBottom)
	refineValue(&s.Inset.Left, r.InsetLeft)
	refineValue(&s.Size.Width, r.SizeWidth)
	refineValue(&s.Size.Height, r.SizeHeight)
	refineValue(&s.MinSize.Width, r.MinSizeWidth)
	refineValue(&s.MinSize.Height, r.MinSizeHeight)
	refineValue(&s.MaxSize.Width, r.MaxSizeWidth)
	refineValue(&s.MaxSize.Height, r.MaxSizeHeight)
	refinePointer(&s.AspectRatio, r.AspectRatio)
	refineValue(&s.Margin.Top, r.MarginTop)
	refineValue(&s.Margin.Right, r.MarginRight)
	refineValue(&s.Margin.Bottom, r.MarginBottom)
	refineValue(&s.Margin.Left, r.MarginLeft)
	refineValue(&s.Padding.Top, r.PaddingTop)
	refineValue(&s.Padding.Right, r.PaddingRight)
	refineValue(&s.Padding.Bottom, r.PaddingBottom)
	refineValue(&s.Padding.Left, r.PaddingLeft)
	refineValue(&s.BorderWidths.Top, r.BorderWidthsTop)
	refineValue(&s.BorderWidths.Right, r.BorderWidthsRight)
	refineValue(&s.BorderWidths.Bottom, r.BorderWidthsBottom)
	refineValue(&s.BorderWidths.Left, r.BorderWidthsLeft)
	refineBoxed(&s.AlignItems, r.AlignItems, &boxes.alignItems, equalComparable[AlignItems])
	refineBoxed(&s.AlignSelf, r.AlignSelf, &boxes.alignSelf, equalComparable[AlignItems])
	refineValue(&s.VerticalAlign, r.VerticalAlign)
	refineBoxed(&s.AlignContent, r.AlignContent, &boxes.alignContent, equalComparable[AlignContent])
	refineBoxed(&s.JustifyContent, r.JustifyContent, &boxes.justifyContent, equalComparable[AlignContent])
	refineValue(&s.Gap.Width, r.GapWidth)
	refineValue(&s.Gap.Height, r.GapHeight)
	refineValue(&s.FlexDirection, r.FlexDirection)
	refineValue(&s.FlexWrap, r.FlexWrap)
	refineValue(&s.FlexBasis, r.FlexBasis)
	refineValue(&s.FlexGrow, r.FlexGrow)
	refineValue(&s.FlexShrink, r.FlexShrink)
	refineBoxed(&s.Background, r.Background, &boxes.background, equalBackground)
	refineBoxed(&s.BorderColor, r.BorderColor, &boxes.borderColor, equalBackground)
	refineValue(&s.BorderStyle, r.BorderStyle)
	refineValue(&s.BorderDashedLength, r.BorderDashedLength)
	refineValue(&s.BorderDashedGap, r.BorderDashedGap)
	refineValue(&s.CornerRadii.TopLeft, r.CornerRadiiTopLeft)
	refineValue(&s.CornerRadii.TopRight, r.CornerRadiiTopRight)
	refineValue(&s.CornerRadii.BottomRight, r.CornerRadiiBottomRight)
	refineValue(&s.CornerRadii.BottomLeft, r.CornerRadiiBottomLeft)
	refinePointer(&s.CornerSmoothing, r.CornerSmoothing)
	if shadows, ok := r.BoxShadow.Get(); ok {
		s.BoxShadow = slices.Clone(shadows)
	}
	refineValue(&s.Ring.Width, r.RingWidth)
	refineValue(&s.Ring.Color, r.RingColor)
	refineValue(&s.InsetRing.Width, r.InsetRingWidth)
	refineValue(&s.InsetRing.Color, r.InsetRingColor)
	refineValue(&s.Outline.Width, r.OutlineWidth)
	refineValue(&s.Outline.Color, r.OutlineColor)
	refineValue(&s.Outline.Kind, r.OutlineStyle)
	refineValue(&s.Outline.Offset, r.OutlineOffset)
	if filters, ok := r.Filter.Get(); ok {
		s.Filter = slices.Clone(filters)
	}
	if filters, ok := r.BackdropFilter.Get(); ok {
		s.BackdropFilter = slices.Clone(filters)
	}
	refinePointer(&s.ObjectFit, r.ObjectFit)
	refinePointer(&s.ObjectPosition, r.ObjectPosition)
	refinePointer(&s.ImageRendering, r.ImageRendering)
	s.Text.Merge(&r.Text)
	refineValue(&s.ForcedColorAdjust, r.ForcedColorAdjust)
	refineBoxed(&s.MouseCursor, r.MouseCursor, &boxes.mouseCursor, equalComparable[CursorStyle])
	refinePointer(&s.Opacity, r.Opacity)
	if rules, ok := r.Transition.Get(); ok {
		s.Transition = slices.Clone(rules)
	}
	if functions, ok := r.Transform.Get(); ok {
		s.Transform = slices.Clone(functions)
	}
	refinePointer(&s.TransformOrigin, r.TransformOrigin)
	refinePointer(&s.GridCols, r.GridCols)
	refinePointer(&s.GridRows, r.GridRows)
	refinePointer(&s.GridLocation, r.GridLocation)
	refineGridTracks(&s.GridColsList, r.GridColsList)
	refineGridTracks(&s.GridRowsList, r.GridRowsList)
	refineAreaTemplate(&s.GridAreas, r.GridAreas)
	if name, ok := r.GridAreaName.Get(); ok {
		s.GridAreaName = name
	}
	refineValue(&s.Debug, r.Debug)
	refineValue(&s.DebugBelow, r.DebugBelow)
}

// Refine applies the present fields of other onto r (the derived
// Refineable::refine for StyleRefinement: later refinements win
// field-by-field).
func (r *StyleRefinement) Refine(other *StyleRefinement) {
	if other == nil {
		return
	}
	mergeOptional(&r.Display, other.Display)
	mergeOptional(&r.Visibility, other.Visibility)
	mergeOptional(&r.OverflowX, other.OverflowX)
	mergeOptional(&r.OverflowY, other.OverflowY)
	mergeOptional(&r.OverflowFadeTop, other.OverflowFadeTop)
	mergeOptional(&r.OverflowFadeRight, other.OverflowFadeRight)
	mergeOptional(&r.OverflowFadeBottom, other.OverflowFadeBottom)
	mergeOptional(&r.OverflowFadeLeft, other.OverflowFadeLeft)
	mergeOptional(&r.ScrollbarWidth, other.ScrollbarWidth)
	mergeOptional(&r.ScrollbarGutter, other.ScrollbarGutter)
	mergeOptional(&r.OverscrollBehaviorX, other.OverscrollBehaviorX)
	mergeOptional(&r.OverscrollBehaviorY, other.OverscrollBehaviorY)
	mergeOptional(&r.AllowConcurrentScroll, other.AllowConcurrentScroll)
	mergeOptional(&r.RestrictScrollToAxis, other.RestrictScrollToAxis)
	mergeOptional(&r.Position, other.Position)
	mergeOptional(&r.InsetTop, other.InsetTop)
	mergeOptional(&r.InsetRight, other.InsetRight)
	mergeOptional(&r.InsetBottom, other.InsetBottom)
	mergeOptional(&r.InsetLeft, other.InsetLeft)
	mergeOptional(&r.SizeWidth, other.SizeWidth)
	mergeOptional(&r.SizeHeight, other.SizeHeight)
	mergeOptional(&r.MinSizeWidth, other.MinSizeWidth)
	mergeOptional(&r.MinSizeHeight, other.MinSizeHeight)
	mergeOptional(&r.MaxSizeWidth, other.MaxSizeWidth)
	mergeOptional(&r.MaxSizeHeight, other.MaxSizeHeight)
	mergeOptional(&r.AspectRatio, other.AspectRatio)
	mergeOptional(&r.MarginTop, other.MarginTop)
	mergeOptional(&r.MarginRight, other.MarginRight)
	mergeOptional(&r.MarginBottom, other.MarginBottom)
	mergeOptional(&r.MarginLeft, other.MarginLeft)
	mergeOptional(&r.PaddingTop, other.PaddingTop)
	mergeOptional(&r.PaddingRight, other.PaddingRight)
	mergeOptional(&r.PaddingBottom, other.PaddingBottom)
	mergeOptional(&r.PaddingLeft, other.PaddingLeft)
	mergeOptional(&r.BorderWidthsTop, other.BorderWidthsTop)
	mergeOptional(&r.BorderWidthsRight, other.BorderWidthsRight)
	mergeOptional(&r.BorderWidthsBottom, other.BorderWidthsBottom)
	mergeOptional(&r.BorderWidthsLeft, other.BorderWidthsLeft)
	mergeOptional(&r.AlignItems, other.AlignItems)
	mergeOptional(&r.AlignSelf, other.AlignSelf)
	mergeOptional(&r.VerticalAlign, other.VerticalAlign)
	mergeOptional(&r.AlignContent, other.AlignContent)
	mergeOptional(&r.JustifyContent, other.JustifyContent)
	mergeOptional(&r.GapWidth, other.GapWidth)
	mergeOptional(&r.GapHeight, other.GapHeight)
	mergeOptional(&r.FlexDirection, other.FlexDirection)
	mergeOptional(&r.FlexWrap, other.FlexWrap)
	mergeOptional(&r.FlexBasis, other.FlexBasis)
	mergeOptional(&r.FlexGrow, other.FlexGrow)
	mergeOptional(&r.FlexShrink, other.FlexShrink)
	mergeOptional(&r.Background, other.Background)
	mergeOptional(&r.BorderColor, other.BorderColor)
	mergeOptional(&r.BorderStyle, other.BorderStyle)
	mergeOptional(&r.BorderDashedLength, other.BorderDashedLength)
	mergeOptional(&r.BorderDashedGap, other.BorderDashedGap)
	mergeOptional(&r.CornerRadiiTopLeft, other.CornerRadiiTopLeft)
	mergeOptional(&r.CornerRadiiTopRight, other.CornerRadiiTopRight)
	mergeOptional(&r.CornerRadiiBottomRight, other.CornerRadiiBottomRight)
	mergeOptional(&r.CornerRadiiBottomLeft, other.CornerRadiiBottomLeft)
	mergeOptional(&r.CornerSmoothing, other.CornerSmoothing)
	if shadows, ok := other.BoxShadow.Get(); ok {
		r.BoxShadow = Some(slices.Clone(shadows))
	}
	mergeOptional(&r.RingWidth, other.RingWidth)
	mergeOptional(&r.RingColor, other.RingColor)
	mergeOptional(&r.InsetRingWidth, other.InsetRingWidth)
	mergeOptional(&r.InsetRingColor, other.InsetRingColor)
	mergeOptional(&r.OutlineWidth, other.OutlineWidth)
	mergeOptional(&r.OutlineColor, other.OutlineColor)
	mergeOptional(&r.OutlineStyle, other.OutlineStyle)
	mergeOptional(&r.OutlineOffset, other.OutlineOffset)
	if filters, ok := other.Filter.Get(); ok {
		r.Filter = Some(slices.Clone(filters))
	}
	if filters, ok := other.BackdropFilter.Get(); ok {
		r.BackdropFilter = Some(slices.Clone(filters))
	}
	mergeOptional(&r.ObjectFit, other.ObjectFit)
	mergeOptional(&r.ObjectPosition, other.ObjectPosition)
	mergeOptional(&r.ImageRendering, other.ImageRendering)
	r.Text.Merge(&other.Text)
	mergeOptional(&r.ForcedColorAdjust, other.ForcedColorAdjust)
	mergeOptional(&r.MouseCursor, other.MouseCursor)
	mergeOptional(&r.Opacity, other.Opacity)
	if rules, ok := other.Transition.Get(); ok {
		r.Transition = Some(slices.Clone(rules))
	}
	if functions, ok := other.Transform.Get(); ok {
		r.Transform = Some(slices.Clone(functions))
	}
	mergeOptional(&r.TransformOrigin, other.TransformOrigin)
	mergeOptional(&r.GridCols, other.GridCols)
	mergeOptional(&r.GridRows, other.GridRows)
	mergeOptional(&r.GridLocation, other.GridLocation)
	mergeOptional(&r.GridColsList, other.GridColsList)
	mergeOptional(&r.GridRowsList, other.GridRowsList)
	mergeOptional(&r.GridAreas, other.GridAreas)
	mergeOptional(&r.GridAreaName, other.GridAreaName)
	mergeOptional(&r.Debug, other.Debug)
	mergeOptional(&r.DebugBelow, other.DebugBelow)
}

// Clone returns an independent copy: the slice-valued fields (shadows,
// filters, font features and fallbacks) are copied so mutating the copy
// cannot reach the original.
func (r StyleRefinement) Clone() StyleRefinement {
	out := r
	if v, ok := r.BoxShadow.Get(); ok {
		out.BoxShadow = Some(slices.Clone(v))
	}
	if v, ok := r.Filter.Get(); ok {
		out.Filter = Some(slices.Clone(v))
	}
	if v, ok := r.BackdropFilter.Get(); ok {
		out.BackdropFilter = Some(slices.Clone(v))
	}
	if v, ok := r.Transition.Get(); ok {
		out.Transition = Some(slices.Clone(v))
	}
	if v, ok := r.Transform.Get(); ok {
		out.Transform = Some(slices.Clone(v))
	}
	out.Text = r.Text.Clone()
	return out
}

// ---------------------------------------------------------------------------
// Into conversions (the pinned `impl Into<...>` parameters)
// ---------------------------------------------------------------------------

// IntoLength is the set of values convertible to a Length (the pinned
// `impl Into<Length>`: a Length, a DefiniteLength or an AbsoluteLength).
type IntoLength interface {
	Length | DefiniteLength | AbsoluteLength
}

// IntoDefiniteLength is the set of values convertible to a
// DefiniteLength (the pinned `impl Into<DefiniteLength>`).
type IntoDefiniteLength interface {
	DefiniteLength | AbsoluteLength
}

// IntoBackground is the set of values convertible to a Background (the
// pinned `impl Into<Background>` / `impl Into<Fill>`: a Background or a
// color).
type IntoBackground interface {
	Background | Hsla
}

// LengthOf converts a length-like value into a Length (the pinned From
// impls: DefiniteLength -> Length::Definite, AbsoluteLength ->
// Length::Definite(DefiniteLength::Absolute)).
func LengthOf[L IntoLength](value L) Length {
	switch v := any(value).(type) {
	case Length:
		return v
	case DefiniteLength:
		return DefiniteLengthOf(v)
	case AbsoluteLength:
		return DefiniteLengthOf(definiteFromAbsolute(v))
	}
	panic("gpui: unreachable IntoLength")
}

// DefiniteLengthFrom converts a definite-length-like value into a
// DefiniteLength (AbsoluteLength -> DefiniteLength::Absolute).
func DefiniteLengthFrom[L IntoDefiniteLength](value L) DefiniteLength {
	switch v := any(value).(type) {
	case DefiniteLength:
		return v
	case AbsoluteLength:
		return definiteFromAbsolute(v)
	}
	panic("gpui: unreachable IntoDefiniteLength")
}

// BackgroundOf converts a color or background into a Background (the
// pinned From<Hsla> for Background: a solid background).
func BackgroundOf[B IntoBackground](value B) Background {
	switch v := any(value).(type) {
	case Background:
		return v
	case Hsla:
		return SolidBackground(v)
	}
	panic("gpui: unreachable IntoBackground")
}

// definiteFromAbsolute is DefiniteLength::Absolute.
func definiteFromAbsolute(v AbsoluteLength) DefiniteLength {
	return DefiniteLength{Kind: DefiniteLengthKind(v.Kind), Value: v.Value}
}

// negativeZero is IEEE-754 negative zero, the value of the pinned
// negated zero-length helpers (`-px(0.)`, styles.rs:1138).
var negativeZero = float32(math.Copysign(0, -1))
