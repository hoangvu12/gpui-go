package base

// ElementExt (U:element_ext.rs) extends a parent element with post-layout
// prepaint observation.
//
// Go adaptation: the trait is blanket-implemented for every ParentElement
// (U:element_ext.rs:31). ElementExtOf accepts a *gpui.DivElement or any
// component embedding gpui.Parent (or gpui.Component), the two parent
// shapes of the core; any other value panics.
//
// Deviation: text_selection_scope (U:element_ext.rs:7-13) needs
// TextSelectionScopeId from text_selection.rs and joins with that ticket
// (29).

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ElementExt is the ElementExt trait over one parent element.
type ElementExt[S any] struct {
	element S
	extend  func([]gpui.AnyElement)
}

// ElementExtOf wraps a parent element (a *gpui.DivElement or a component
// embedding gpui.Parent) for the ElementExt methods.
func ElementExtOf[S any](element S) ElementExt[S] {
	switch e := any(element).(type) {
	case *gpui.DivElement:
		return ElementExt[S]{element: element, extend: func(c []gpui.AnyElement) { e.Extend(c) }}
	case interface{ Extend([]gpui.AnyElement) }:
		return ElementExt[S]{element: element, extend: e.Extend}
	}
	panic(fmt.Sprintf("base: ElementExtOf requires a *gpui.DivElement or a gpui.Parent component, got %T", element))
}

// OnPrepaint invokes callback during prepaint with this element's
// resolved bounds (U:element_ext.rs:15-28): an absolutely positioned,
// full-size canvas child observes the bounds and paints nothing.
func (e ElementExt[S]) OnPrepaint(callback func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App)) S {
	canvas := gpui.Canvas(
		func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} {
			callback(bounds, w, app)
			return struct{}{}
		},
		func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {},
	).Absolute().SizeFull()
	e.extend([]gpui.AnyElement{canvas.IntoElement()})
	return e.element
}
