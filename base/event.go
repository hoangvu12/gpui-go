package base

// Interaction extensions (U:event.rs).
//
// The wasm32 no-op branches (U:event.rs:6-10, 19-20, 33-36) do not apply:
// the port has no wasm target.

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// OngoingScrollLockAxis locks a wheel delta to the axis its gesture
// started on (U:event.rs:11-24 OngoingScrollExt::lock_axis, which calls
// OngoingScroll::filter). The core's Filter reads the gesture clock from
// the application, so the Go form takes it.
func OngoingScrollLockAxis(scroll *gpui.OngoingScroll, delta *gpui.Point, touchPhase gpui.TouchPhase, app *gpui.App) {
	scroll.Filter(delta, touchPhase, app)
}

// InteractiveElementExt is the InteractiveElementExt trait
// (U:event.rs:26-60) over one interactive element.
//
// Go adaptation: the trait is implemented for Stateful<E:
// InteractiveElement> (U:event.rs:60). InteractiveElementExtOf accepts a
// *gpui.DivElement or any component embedding gpui.Interactive (or
// gpui.Component), whose inner div carries the interactivity; any other
// value panics. The element needs an ID like the pinned Stateful.
type InteractiveElementExt[S any] struct {
	element S
	div     *gpui.DivElement
}

// InteractiveElementExtOf wraps an interactive element for the
// InteractiveElementExt methods.
func InteractiveElementExtOf[S any](element S) InteractiveElementExt[S] {
	switch e := any(element).(type) {
	case *gpui.DivElement:
		return InteractiveElementExt[S]{element: element, div: e}
	case interface{ Interactivity() *gpui.DivElement }:
		return InteractiveElementExt[S]{element: element, div: e.Interactivity()}
	}
	panic(fmt.Sprintf("base: InteractiveElementExtOf requires a *gpui.DivElement or a gpui.Interactive component, got %T", element))
}

// LockScrollAxis locks scrolling to the gesture's dominant axis
// (U:event.rs:27-41: restrict_scroll_to_axis).
func (e InteractiveElementExt[S]) LockScrollAxis() S {
	e.div.RestrictScrollToAxis()
	return e.element
}

// OnDoubleClick sets a listener for a double click: a click whose click
// count is two (U:event.rs:43-57).
func (e InteractiveElementExt[S]) OnDoubleClick(listener func(*gpui.ClickEvent, *gpui.Window, *gpui.App)) S {
	e.div.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
		if event.ClickCount() == 2 {
			listener(event, w, app)
		}
	})
	return e.element
}
