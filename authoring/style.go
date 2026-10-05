package authoring

// Optional distinguishes an absent refinement from an explicit zero value.
// Its zero value is absent.
type Optional[T any] struct {
	value   T
	present bool
}

// Some makes an explicit refinement, including zero or false.
func Some[T any](value T) Optional[T] { return Optional[T]{value: value, present: true} }

// Get returns the value and whether it was explicitly supplied.
func (o Optional[T]) Get() (T, bool) { return o.value, o.present }

// Display describes the layout display requested by this authoring slice.
type Display uint8

const (
	DisplayBlock Display = iota
	DisplayFlex
)

// Unit keeps logical pixels and rem values distinct until layout resolves them.
type Unit uint8

const (
	Pixels Unit = iota
	Rems
)

// Length is an unresolved length. A rem is relative to the root font size.
type Length struct {
	Value float32
	Unit  Unit
}

// Px constructs a logical-pixel length.
func Px(value float32) Length { return Length{Value: value, Unit: Pixels} }

// Rem constructs a root-font-relative length.
func Rem(value float32) Length { return Length{Value: value, Unit: Rems} }

// StyleRefinement is a value snapshot of the fields supported by this slice.
// Absent fields leave previous values unchanged when applied with Refine.
// These fields contain no shared mutable references.
type StyleRefinement struct {
	Display      Optional[Display]
	PaddingLeft  Optional[Length]
	PaddingRight Optional[Length]
	Gap          Optional[Length]
	Opacity      Optional[float32]
}

func merge[T any](dst *Optional[T], src Optional[T]) {
	if src.present {
		*dst = src
	}
}

// Styled supplies shared fluent methods that return the owner's concrete type.
// Embed Styled[*YourComponent] by value and initialize it with BindStyled.
// The zero value is unbound; only StyleBase may be called before binding.
type Styled[Self any] struct {
	self   Self
	anchor *Styled[Self]
	style  StyleRefinement
}

// StyleBase is the binding hook automatically promoted through value embedding.
// Components using that embedding must not override this method.
func (s *Styled[Self]) StyleBase() *Styled[Self] { return s }

// BindStyled binds a newly constructed component to its embedded helper and
// returns that same component pointer. Both type parameters are inferred.
// It panics on a nil owner, missing helper, or an already-bound/copied helper.
// The pointer constraint rejects value owners at compile time.
func BindStyled[Value any, Self interface {
	*Value
	StyleBase() *Styled[Self]
}](self Self) Self {
	if self == nil {
		panic("gpui-go/authoring: cannot bind a nil component")
	}
	s := self.StyleBase()
	if s == nil {
		panic("gpui-go/authoring: component has no Styled helper")
	}
	if s.anchor != nil {
		panic("gpui-go/authoring: Styled is already bound; create a new component instead of rebinding or copying")
	}
	s.self, s.anchor = self, s
	return self
}

func (s *Styled[Self]) check() {
	if s == nil || s.anchor == nil {
		panic("gpui-go/authoring: unbound Styled; construct the component with BindStyled")
	}
	if s.anchor != s {
		panic("gpui-go/authoring: copied bound component; use its original pointer")
	}
}

// Style returns an independent value snapshot. It does not expose mutable state.
func (s *Styled[Self]) Style() StyleRefinement {
	s.check()
	return s.style
}

// Refine applies only explicitly supplied fields and returns the component.
func (s *Styled[Self]) Refine(style StyleRefinement) Self {
	s.check()
	merge(&s.style.Display, style.Display)
	merge(&s.style.PaddingLeft, style.PaddingLeft)
	merge(&s.style.PaddingRight, style.PaddingRight)
	merge(&s.style.Gap, style.Gap)
	merge(&s.style.Opacity, style.Opacity)
	return s.self
}

// Flex requests flex layout.
func (s *Styled[Self]) Flex() Self {
	return s.Refine(StyleRefinement{Display: Some(DisplayFlex)})
}

// PaddingX sets the left and right padding, preserving the supplied unit.
func (s *Styled[Self]) PaddingX(length Length) Self {
	return s.Refine(StyleRefinement{PaddingLeft: Some(length), PaddingRight: Some(length)})
}

// Px3 sets horizontal padding to 0.75rem, matching GPUI's spacing scale.
func (s *Styled[Self]) Px3() Self { return s.PaddingX(Rem(0.75)) }

// Gap sets the gap between children, preserving the supplied unit.
func (s *Styled[Self]) Gap(length Length) Self {
	return s.Refine(StyleRefinement{Gap: Some(length)})
}

// Gap2 sets the gap to 0.5rem, matching GPUI's spacing scale.
func (s *Styled[Self]) Gap2() Self { return s.Gap(Rem(0.5)) }

// Opacity records an explicit opacity refinement, including zero.
func (s *Styled[Self]) Opacity(opacity float32) Self {
	return s.Refine(StyleRefinement{Opacity: Some(opacity)})
}
