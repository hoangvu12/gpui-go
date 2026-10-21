package gpui

// This file is Base ticket 08's canvas element: the pinned
// crates/gpui/src/elements/canvas.rs (GPUI-CE 254b5dbd, R: below) — a
// styled leaf whose prepaint and paint run caller closures, the low-level
// paint API without a whole custom element.

import "fmt"

// CanvasElement is a canvas element (R:elements/canvas.rs:23 Canvas<T>):
// the prepaint closure's result T is handed to the paint closure. Build
// one with Canvas; it carries the full Styled vocabulary.
type CanvasElement[T any] struct {
	// Styled is the canvas's style refinement (R:canvas.rs:91-95 impl
	// Styled for Canvas).
	Styled[*CanvasElement[T]]
	prepaint func(bounds Bounds, w *Window, app *App) T
	paint    func(bounds Bounds, state T, w *Window, app *App)
	// lastStyle is the style of the last layout request (the root-stretch
	// report).
	lastStyle Style
}

// Canvas constructs a canvas element with the given prepaint and paint
// callbacks (R:elements/canvas.rs:10 canvas): useful for short-term custom
// drawing in a view through the Window paint methods (paintapi.go).
func Canvas[T any](prepaint func(bounds Bounds, w *Window, app *App) T, paint func(bounds Bounds, state T, w *Window, app *App)) *CanvasElement[T] {
	if prepaint == nil || paint == nil {
		panic("gpui: Canvas requires non-nil prepaint and paint callbacks")
	}
	return BindStyled(&CanvasElement[T]{prepaint: prepaint, paint: paint})
}

// canvasPrepaintState is the pinned PrepaintState Option<T>.
type canvasPrepaintState[T any] struct {
	value T
	set   bool
}

// canvasDrawable adapts the builder into the Element phases (the div's
// builder/drawable split).
type canvasDrawable[T any] struct {
	canvas *CanvasElement[T]
	used   bool
}

// IntoElement implements IntoElement (R:canvas.rs:29-35).
func (c *CanvasElement[T]) IntoElement() AnyElement {
	return CustomElement[Style, canvasPrepaintState[T]](&canvasDrawable[T]{canvas: c})
}

// ID implements Element: a canvas has no identity (R:canvas.rs:41).
func (d *canvasDrawable[T]) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element (R:canvas.rs:45).
func (d *canvasDrawable[T]) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (R:canvas.rs:49-60): Style::default()
// refined by the canvas's style, one childless layout node; the style is
// the request-layout state.
func (d *canvasDrawable[T]) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, Style) {
	style := DefaultStyle()
	style.Refine(d.canvas.Styled.storage())
	d.canvas.lastStyle = style
	id, err := requestLayoutOf(w, style)
	if err != nil {
		panic(fmt.Sprintf("gpui: Canvas request_layout: %v", err))
	}
	return id, style
}

// Prepaint implements Element (R:canvas.rs:62-72): the prepaint closure
// runs once with the committed bounds.
func (d *canvasDrawable[T]) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, style *Style, w *Window, app *App) canvasPrepaintState[T] {
	if d.used {
		panic("gpui: Canvas prepaint called twice (the pinned FnOnce take().unwrap())")
	}
	return canvasPrepaintState[T]{value: d.canvas.prepaint(bounds, w, app), set: true}
}

// Paint implements Element (R:canvas.rs:74-88): the style paints its box
// around the paint closure, which receives the prepaint result.
func (d *canvasDrawable[T]) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, style *Style, prepaint *canvasPrepaintState[T], w *Window, app *App) {
	if !prepaint.set || d.used {
		panic("gpui: Canvas paint without a prepaint result (the pinned prepaint.take().unwrap())")
	}
	d.used = true
	state := prepaint.value
	var zero T
	prepaint.value, prepaint.set = zero, false
	style.paint(bounds, w, app, func(w *Window) {
		d.canvas.paint(bounds, state, w, app)
	})
}

// LayoutNodeStyle implements the root-stretch report.
func (d *canvasDrawable[T]) LayoutNodeStyle() (Style, bool) { return d.canvas.lastStyle, true }
