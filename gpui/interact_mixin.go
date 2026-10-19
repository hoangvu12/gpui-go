package gpui

// The Interactive mixin's forwarding of Base ticket 06's div builders
// (fluent.go Interactive; the Base shape `fn interactivity(&mut self) ->
// &mut Interactivity { self.base.interactivity() }`, upstream
// base/src/button.rs:199-203): every InteractiveElement /
// StatefulInteractiveElement builder interact.go adds to *DivElement is
// reachable from a component that embeds Interactive or Component, and
// returns the component.

// Group assigns the inner div to a group (DivElement.Group, div.rs:901).
func (i *Interactive[Self]) Group(group string) Self {
	i.Interactivity().Group(group)
	return i.self
}

// Hover applies a style while the inner div is hovered (DivElement.Hover,
// div.rs:970).
func (i *Interactive[Self]) Hover(f func(StyleRefinement) StyleRefinement) Self {
	i.Interactivity().Hover(f)
	return i.self
}

// GroupHover applies a style while the named group is hovered
// (DivElement.GroupHover, div.rs:980).
func (i *Interactive[Self]) GroupHover(group string, f func(StyleRefinement) StyleRefinement) Self {
	i.Interactivity().GroupHover(group, f)
	return i.self
}

// Active applies a style while the inner div is pressed
// (DivElement.Active, div.rs:1786).
func (i *Interactive[Self]) Active(f func(StyleRefinement) StyleRefinement) Self {
	i.Interactivity().Active(f)
	return i.self
}

// GroupActive applies a style while the named group is pressed
// (DivElement.GroupActive, div.rs:1795).
func (i *Interactive[Self]) GroupActive(group string, f func(StyleRefinement) StyleRefinement) Self {
	i.Interactivity().GroupActive(group, f)
	return i.self
}

// Focus applies a style while the inner div's tracked handle is focused
// (DivElement.Focus, div.rs:1459).
func (i *Interactive[Self]) Focus(f func(StyleRefinement) StyleRefinement) Self {
	i.Interactivity().Focus(f)
	return i.self
}

// InFocus applies a style while focus is within the inner div's tracked
// handle (DivElement.InFocus, div.rs:1469).
func (i *Interactive[Self]) InFocus(f func(StyleRefinement) StyleRefinement) Self {
	i.Interactivity().InFocus(f)
	return i.self
}

// FocusVisible applies a style while the inner div is keyboard-focused
// (DivElement.FocusVisible, div.rs:1481).
func (i *Interactive[Self]) FocusVisible(f func(StyleRefinement) StyleRefinement) Self {
	i.Interactivity().FocusVisible(f)
	return i.self
}

// BlockMouseExceptScroll blocks non-scroll mouse interactions behind the
// inner div (DivElement.BlockMouseExceptScroll, div.rs:1452).
func (i *Interactive[Self]) BlockMouseExceptScroll() Self {
	i.Interactivity().BlockMouseExceptScroll()
	return i.self
}

// OnMouseDown binds a bubble mouse-down listener for one button
// (DivElement.OnMouseDown, div.rs:996).
func (i *Interactive[Self]) OnMouseDown(button MouseButton, f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseDown(button, f)
	return i.self
}

// CaptureAnyMouseDown binds a capture mouse-down listener
// (DivElement.CaptureAnyMouseDown, div.rs:1027).
func (i *Interactive[Self]) CaptureAnyMouseDown(f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().CaptureAnyMouseDown(f)
	return i.self
}

// OnAnyMouseDown binds a bubble mouse-down listener for any button
// (DivElement.OnAnyMouseDown, div.rs:1039).
func (i *Interactive[Self]) OnAnyMouseDown(f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().OnAnyMouseDown(f)
	return i.self
}

// OnMouseDownOut binds a listener to mouse downs outside the inner div
// (DivElement.OnMouseDownOut, div.rs:1101).
func (i *Interactive[Self]) OnMouseDownOut(f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseDownOut(f)
	return i.self
}

// OnMouseDownAll binds a listener to every mouse down
// (DivElement.OnMouseDownAll, div.rs:1115).
func (i *Interactive[Self]) OnMouseDownAll(f func(*MouseDownEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnMouseDownAll(f)
	return i.self
}

// OnMouseUpOut binds a listener to mouse ups of one button while the
// inner div is not hovered (DivElement.OnMouseUpOut, div.rs:1128).
func (i *Interactive[Self]) OnMouseUpOut(button MouseButton, f func(*MouseUpEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseUpOut(button, f)
	return i.self
}

// CaptureAnyMouseUp binds a capture mouse-up listener
// (DivElement.CaptureAnyMouseUp, div.rs:1064).
func (i *Interactive[Self]) CaptureAnyMouseUp(f func(*MouseUpEvent, *Window, *App)) Self {
	i.Interactivity().CaptureAnyMouseUp(f)
	return i.self
}

// OnAnyMouseUp binds a bubble mouse-up listener for any button
// (DivElement.OnAnyMouseUp, div.rs:259).
func (i *Interactive[Self]) OnAnyMouseUp(f func(*MouseUpEvent, *Window, *App)) Self {
	i.Interactivity().OnAnyMouseUp(f)
	return i.self
}

// OnMouseUpAll binds a listener to every mouse up
// (DivElement.OnMouseUpAll, div.rs:1143).
func (i *Interactive[Self]) OnMouseUpAll(f func(*MouseUpEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnMouseUpAll(f)
	return i.self
}

// OnMouseMoveAll binds a listener to every mouse move
// (DivElement.OnMouseMoveAll, div.rs:1167).
func (i *Interactive[Self]) OnMouseMoveAll(f func(*MouseMoveEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnMouseMoveAll(f)
	return i.self
}

// OnDragMove binds a listener to moves during a drag of type T
// (DivElement.OnDragMove, div.rs:1194).
func (i *Interactive[Self]) OnDragMove[T any](f func(*DragMoveEvent[T], *Window, *App)) Self {
	i.Interactivity().OnDragMove(f)
	return i.self
}

// OnAuxClick binds a non-primary click listener (DivElement.OnAuxClick,
// div.rs:1826).
func (i *Interactive[Self]) OnAuxClick(f func(*ClickEvent, *Window, *App)) Self {
	i.Interactivity().OnAuxClick(f)
	return i.self
}

// OnHover binds a hover transition listener (DivElement.OnHover,
// div.rs:1899).
func (i *Interactive[Self]) OnHover(f func(*bool, *Window, *App)) Self {
	i.Interactivity().OnHover(f)
	return i.self
}

// OnDrag makes the inner div a drag source (DivElement.OnDrag,
// div.rs:1844).
func (i *Interactive[Self]) OnDrag[T any](value T, constructor func(*T, Point, *Window, *App) View) Self {
	i.Interactivity().OnDrag(value, constructor)
	return i.self
}

// OnDragAlt makes the inner div a drag source with a custom AnyDrag
// (DivElement.OnDragAlt, div.rs:1867).
func (i *Interactive[Self]) OnDragAlt(f func(cursorOffset Point, cursorStyle *CursorStyle, w *Window, app *App) AnyDrag) Self {
	i.Interactivity().OnDragAlt(f)
	return i.self
}
