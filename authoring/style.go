package authoring

import "github.com/hoangvu12/gpui-go/gpui"

// Since ticket base-05 the fluent styling helper lives in package gpui
// (gpui.Styled, with the whole pinned Styled vocabulary, and the
// Parent/Interactive/Component mixins). This package keeps the original
// authoring spellings as aliases of those types so existing components
// keep compiling unchanged; new code can use either spelling.

// Optional distinguishes an absent refinement from an explicit zero value
// (gpui.Optional). Its zero value is absent.
type Optional[T any] = gpui.Optional[T]

// Some makes an explicit refinement, including zero or false (gpui.Some).
func Some[T any](value T) Optional[T] { return gpui.Some(value) }

// Display is the layout display (gpui.Display).
type Display = gpui.Display

// The displays of the original authoring slice (gpui.DisplayBlock,
// gpui.DisplayFlex).
const (
	DisplayBlock = gpui.DisplayBlock
	DisplayFlex  = gpui.DisplayFlex
)

// Length is an unresolved definite length that keeps logical pixels and
// rems distinct until layout resolves them (gpui.DefiniteLength).
type Length = gpui.DefiniteLength

// Unit is the unit of a Length (gpui.DefiniteLengthKind).
type Unit = gpui.DefiniteLengthKind

// The units of the original authoring slice.
const (
	Pixels = gpui.DefiniteLengthPx
	Rems   = gpui.DefiniteLengthRem
)

// Px constructs a logical-pixel length (gpui.DefinitePx).
func Px(value float32) Length { return gpui.DefinitePx(value) }

// Rem constructs a root-font-relative length (gpui.DefiniteRem).
func Rem(value float32) Length { return gpui.DefiniteRem(value) }

// StyleRefinement is the style refinement (gpui.StyleRefinement). Absent
// fields leave previous values unchanged when applied with Refine.
type StyleRefinement = gpui.StyleRefinement

// Styled supplies the shared fluent styling methods that return the
// owner's concrete type (gpui.Styled). Embed Styled[*YourComponent] by
// value and initialize it with BindStyled. The zero value is unbound;
// only StyleBase may be called before binding.
type Styled[Self any] = gpui.Styled[Self]

// BindStyled binds a newly constructed component to its embedded helper
// and returns that same component pointer (gpui.BindStyled). Both type
// parameters are inferred. It panics on a nil owner, missing helper, or
// an already-bound/copied helper. The pointer constraint rejects value
// owners at compile time.
func BindStyled[Value any, Self interface {
	*Value
	StyleBase() *Styled[Self]
}](self Self) Self {
	return gpui.BindStyled[Value, Self](self)
}
