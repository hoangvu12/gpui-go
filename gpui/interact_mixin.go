package gpui

// The Interactive mixin's forwarding of Base ticket 06's div builders
// (fluent.go Interactive; the Base shape `fn interactivity(&mut self) ->
// &mut Interactivity { self.base.interactivity() }`, upstream
// base/src/button.rs:199-203): every InteractiveElement /
// StatefulInteractiveElement builder interact.go adds to *DivElement is
// reachable from a component that embeds Interactive or Component, and
// returns the component.

// Group assigns the inner div to a group (DivElement.Group, div.rs:901).
func (i *Interactive[Self]) Group[_ lazy](group string) Self {
	i.Interactivity().Group(group)
	return i.self
}

// Hover applies a style while the inner div is hovered (DivElement.Hover,
// div.rs:970). It takes the pinned closure or a StyleBuilder:
// .Hover(gpui.Styles().Bg(tw.Slate700)).
func (i *Interactive[Self]) Hover[F StyleArg](f F) Self {
	i.Interactivity().Hover(f)
	return i.self
}

// GroupHover applies a style while the named group is hovered
// (DivElement.GroupHover, div.rs:980).
func (i *Interactive[Self]) GroupHover[F StyleArg](group string, f F) Self {
	i.Interactivity().GroupHover(group, f)
	return i.self
}

// Active applies a style while the inner div is pressed
// (DivElement.Active, div.rs:1786).
func (i *Interactive[Self]) Active[F StyleArg](f F) Self {
	i.Interactivity().Active(f)
	return i.self
}

// GroupActive applies a style while the named group is pressed
// (DivElement.GroupActive, div.rs:1795).
func (i *Interactive[Self]) GroupActive[F StyleArg](group string, f F) Self {
	i.Interactivity().GroupActive(group, f)
	return i.self
}

// Focus applies a style while the inner div's tracked handle is focused
// (DivElement.Focus, div.rs:1459).
func (i *Interactive[Self]) Focus[F StyleArg](f F) Self {
	i.Interactivity().Focus(f)
	return i.self
}

// InFocus applies a style while focus is within the inner div's tracked
// handle (DivElement.InFocus, div.rs:1469).
func (i *Interactive[Self]) InFocus[F StyleArg](f F) Self {
	i.Interactivity().InFocus(f)
	return i.self
}

// FocusVisible applies a style while the inner div is keyboard-focused
// (DivElement.FocusVisible, div.rs:1481).
func (i *Interactive[Self]) FocusVisible[F StyleArg](f F) Self {
	i.Interactivity().FocusVisible(f)
	return i.self
}

// WindowControlArea marks the inner div as a control area of the
// platform window (DivElement.WindowControlArea, div.rs:1443).
func (i *Interactive[Self]) WindowControlArea[_ lazy](area WindowControlArea) Self {
	i.Interactivity().WindowControlArea(area)
	return i.self
}

// BlockMouseExceptScroll blocks non-scroll mouse interactions behind the
// inner div (DivElement.BlockMouseExceptScroll, div.rs:1452).
func (i *Interactive[Self]) BlockMouseExceptScroll[_ lazy]() Self {
	i.Interactivity().BlockMouseExceptScroll()
	return i.self
}

// OnMouseDown binds a bubble mouse-down listener for one button
// (DivElement.OnMouseDown, div.rs:996).
func (i *Interactive[Self]) OnMouseDown[_ lazy](button MouseButton, f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseDown(button, f)
	return i.self
}

// CaptureAnyMouseDown binds a capture mouse-down listener
// (DivElement.CaptureAnyMouseDown, div.rs:1027).
func (i *Interactive[Self]) CaptureAnyMouseDown[_ lazy](f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().CaptureAnyMouseDown(f)
	return i.self
}

// OnAnyMouseDown binds a bubble mouse-down listener for any button
// (DivElement.OnAnyMouseDown, div.rs:1039).
func (i *Interactive[Self]) OnAnyMouseDown[_ lazy](f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().OnAnyMouseDown(f)
	return i.self
}

// OnMouseDownOut binds a listener to mouse downs outside the inner div
// (DivElement.OnMouseDownOut, div.rs:1101).
func (i *Interactive[Self]) OnMouseDownOut[_ lazy](f func(*MouseDownEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseDownOut(f)
	return i.self
}

// OnMouseDownAll binds a listener to every mouse down
// (DivElement.OnMouseDownAll, div.rs:1115).
func (i *Interactive[Self]) OnMouseDownAll[_ lazy](f func(*MouseDownEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnMouseDownAll(f)
	return i.self
}

// OnMouseUpOut binds a listener to mouse ups of one button while the
// inner div is not hovered (DivElement.OnMouseUpOut, div.rs:1128).
func (i *Interactive[Self]) OnMouseUpOut[_ lazy](button MouseButton, f func(*MouseUpEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseUpOut(button, f)
	return i.self
}

// CaptureAnyMouseUp binds a capture mouse-up listener
// (DivElement.CaptureAnyMouseUp, div.rs:1064).
func (i *Interactive[Self]) CaptureAnyMouseUp[_ lazy](f func(*MouseUpEvent, *Window, *App)) Self {
	i.Interactivity().CaptureAnyMouseUp(f)
	return i.self
}

// OnAnyMouseUp binds a bubble mouse-up listener for any button
// (DivElement.OnAnyMouseUp, div.rs:259).
func (i *Interactive[Self]) OnAnyMouseUp[_ lazy](f func(*MouseUpEvent, *Window, *App)) Self {
	i.Interactivity().OnAnyMouseUp(f)
	return i.self
}

// OnMouseUpAll binds a listener to every mouse up
// (DivElement.OnMouseUpAll, div.rs:1143).
func (i *Interactive[Self]) OnMouseUpAll[_ lazy](f func(*MouseUpEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnMouseUpAll(f)
	return i.self
}

// OnMouseMoveAll binds a listener to every mouse move
// (DivElement.OnMouseMoveAll, div.rs:1167).
func (i *Interactive[Self]) OnMouseMoveAll[_ lazy](f func(*MouseMoveEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnMouseMoveAll(f)
	return i.self
}

// OnDragMove binds a listener to moves during a drag of type T
// (DivElement.OnDragMove, div.rs:1194).
func (i *Interactive[Self]) OnDragMove[T any](f func(*DragMoveEvent[T], *Window, *App)) Self {
	i.Interactivity().OnDragMove(f)
	return i.self
}

// OnDropAlt attaches a DropListener for drags of type T to the inner div
// (DivElement.OnDropAlt, div.rs:1415).
func (i *Interactive[Self]) OnDropAlt[T any](listener DropListener[T]) Self {
	i.Interactivity().OnDropAlt(listener)
	return i.self
}

// CanDrop gates drops and drag-over styles on the inner div
// (DivElement.CanDrop, div.rs:1425).
func (i *Interactive[Self]) CanDrop[_ lazy](predicate func(value any, w *Window, app *App) bool) Self {
	i.Interactivity().CanDrop(predicate)
	return i.self
}

// OnDragOver styles the inner div while a drag of type S hovers it
// (DivElement.OnDragOver, div.rs:1344).
func (i *Interactive[Self]) OnDragOver[S any](f func(style StyleRefinement, value *S, w *Window, app *App) StyleRefinement) Self {
	i.Interactivity().OnDragOver(f)
	return i.self
}

// DragOver is OnDragOver (DivElement.DragOver, div.rs:1336).
func (i *Interactive[Self]) DragOver[S any](f func(style StyleRefinement, value *S, w *Window, app *App) StyleRefinement) Self {
	i.Interactivity().DragOver(f)
	return i.self
}

// OnDragOverAny styles the inner div while any droppable drag hovers it
// (DivElement.OnDragOverAny, div.rs:1375).
func (i *Interactive[Self]) OnDragOverAny[_ lazy](f func(style StyleRefinement, value any, w *Window, app *App) StyleRefinement) Self {
	i.Interactivity().OnDragOverAny(f)
	return i.self
}

// GroupDragOver styles the inner div while a drag of type S hovers the
// named group (DivElement.GroupDragOver, div.rs:1384).
func (i *Interactive[Self]) GroupDragOver[S any, F StyleArg](group string, f F) Self {
	i.Interactivity().GroupDragOver[S](group, f)
	return i.self
}

// OnAuxClick binds a non-primary click listener (DivElement.OnAuxClick,
// div.rs:1826).
func (i *Interactive[Self]) OnAuxClick[_ lazy](f func(*ClickEvent, *Window, *App)) Self {
	i.Interactivity().OnAuxClick(f)
	return i.self
}

// OnHover binds a hover transition listener (DivElement.OnHover,
// div.rs:1899).
func (i *Interactive[Self]) OnHover[_ lazy](f func(*bool, *Window, *App)) Self {
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
func (i *Interactive[Self]) OnDragAlt[_ lazy](f func(cursorOffset Point, cursorStyle *CursorStyle, w *Window, app *App) AnyDrag) Self {
	i.Interactivity().OnDragAlt(f)
	return i.self
}

// OverflowScroll scrolls the inner div on both axes
// (DivElement.OverflowScroll, div.rs:1746-1751; Base ticket 07's builder,
// forwarded by Base ticket 34). Requires an ID on the inner div.
func (i *Interactive[Self]) OverflowScroll[_ lazy]() Self {
	i.Interactivity().OverflowScroll()
	return i.self
}

// OverflowXScroll scrolls the inner div horizontally
// (DivElement.OverflowXScroll, div.rs:1753-1757).
func (i *Interactive[Self]) OverflowXScroll[_ lazy]() Self {
	i.Interactivity().OverflowXScroll()
	return i.self
}

// OverflowYScroll scrolls the inner div vertically
// (DivElement.OverflowYScroll, div.rs:1759-1763).
func (i *Interactive[Self]) OverflowYScroll[_ lazy]() Self {
	i.Interactivity().OverflowYScroll()
	return i.self
}

// OverflowAuto makes the inner div an overflow: auto scroll container
// (DivElement.OverflowAuto, browser-core 36). Requires an ID on the
// inner div.
func (i *Interactive[Self]) OverflowAuto[_ lazy]() Self {
	i.Interactivity().OverflowAuto()
	return i.self
}

// OverflowXAuto makes the inner div's horizontal overflow auto
// (DivElement.OverflowXAuto, browser-core 36).
func (i *Interactive[Self]) OverflowXAuto[_ lazy]() Self {
	i.Interactivity().OverflowXAuto()
	return i.self
}

// OverflowYAuto makes the inner div's vertical overflow auto
// (DivElement.OverflowYAuto, browser-core 36).
func (i *Interactive[Self]) OverflowYAuto[_ lazy]() Self {
	i.Interactivity().OverflowYAuto()
	return i.self
}

// RestrictScrollToAxis restricts the inner div's scrolling to the
// gesture's axis (DivElement.RestrictScrollToAxis, div.rs:1765-1771).
func (i *Interactive[Self]) RestrictScrollToAxis[_ lazy]() Self {
	i.Interactivity().RestrictScrollToAxis()
	return i.self
}

// TrackScroll tracks the inner div's scroll state with the handle
// (DivElement.TrackScroll, div.rs:1773-1777).
func (i *Interactive[Self]) TrackScroll[_ lazy](handle ScrollHandle) Self {
	i.Interactivity().TrackScroll(handle)
	return i.self
}
