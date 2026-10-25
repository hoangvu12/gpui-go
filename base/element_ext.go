package base

// ElementExt (U:element_ext.rs) extends a parent element with post-layout
// prepaint observation and text-selection scoping.
//
// Go adaptations:
//
//   - The trait is blanket-implemented for every ParentElement
//     (U:element_ext.rs:31). ElementExtOf accepts a *gpui.DivElement or any
//     component embedding gpui.Parent (or gpui.Component), the two parent
//     shapes of the core; any other value panics.
//   - text_selection_scope's `Self: IntoElement` bound (U:element_ext.rs:8-10)
//     is checked at run time: the element converts through
//     gpui.TryIntoElement, and a value that is not an element panics.

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
	).Flex().Absolute().SizeFull()
	e.extend([]gpui.AnyElement{canvas.IntoElement()})
	return e.element
}

// TextSelectionScope marks this element's subtree as belonging to a
// text-selection scope (U:element_ext.rs:7-13): the element runs every
// phase with scope pushed (textSelectionScope, U:text_selection.rs:
// 2422-2430).
func (e ElementExt[S]) TextSelectionScope(scope TextSelectionScopeID) gpui.AnyElement {
	element, err := gpui.TryIntoElement(e.element)
	if err != nil {
		panic(fmt.Sprintf("base: ElementExt.TextSelectionScope: %v", err))
	}
	return gpui.TextSelectionScope(scope, element)
}
