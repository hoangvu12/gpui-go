package gpui

import "fmt"

// StyleBuilder is a standalone, fluent StyleRefinement: the whole Styled
// vocabulary on a refinement that belongs to no element (ADR 0001). It
// is the Go spelling of the pinned `impl Styled for StyleRefinement`,
// which lets Rust state styles read `.hover(|s| s.bg(color))`:
//
//	gpui.Div().ID("save").
//		Bg(tw.Slate800).
//		Hover(gpui.Styles().Bg(tw.Slate700)).
//		Active(gpui.Styles().Bg(tw.Slate900))
//
// The pinned state builders run their closure once, on an empty
// refinement, when the element is built (div.rs:970-977), so passing a
// prebuilt refinement is equivalent to passing the closure.
type StyleBuilder struct {
	Styled[*StyleBuilder]
}

// Styles returns an empty StyleBuilder.
func Styles() *StyleBuilder { return BindStyled(&StyleBuilder{}) }

// Refinement returns a copy of the refinement built so far.
func (b *StyleBuilder) Refinement() StyleRefinement { return b.StyleMut().Clone() }

// StyleArg is what the state-style builders (Hover, GroupHover, Active,
// GroupActive, Focus, InFocus, FocusVisible) accept: the pinned closure
// form, or a *StyleBuilder from Styles().
type StyleArg interface {
	func(StyleRefinement) StyleRefinement | *StyleBuilder
}

// stateRefinement evaluates a state-style argument to the refinement it
// describes. what names the builder in panics.
func stateRefinement[F StyleArg](f F, what string) *StyleRefinement {
	switch v := any(f).(type) {
	case func(StyleRefinement) StyleRefinement:
		return refinementOf(v, what)
	case *StyleBuilder:
		if v == nil {
			panic(fmt.Sprintf("gpui: %s requires a non-nil style", what))
		}
		style := v.Refinement()
		return &style
	}
	panic(fmt.Sprintf("gpui: %s: unsupported style argument %T", what, f))
}
