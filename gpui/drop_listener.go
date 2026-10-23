package gpui

// zterm ticket 12: drop predicates and drag-over styles (R:elements/
// div.rs:598-668 on_drop / on_drop_alt / can_drop / on_drag_over_any,
// 1334-1430 the fluent drag_over / on_drag_over / group_drag_over /
// can_drop, 1991-2110 DropListener; R: = gpui-go/reference/ce-source/
// crates/gpui/src). The style side runs in computeStyleInternal and the
// drop side in paintMouseListeners (interact.go).
//
// The pin has no drag-leave callback. A target that must react to a drag
// leaving it styles itself with OnDragOver (the style drops as soon as
// the drag no longer hovers it) or tracks inside/outside itself from
// OnDragMove's Event.Position against Bounds, which fires on every move
// of a matching drag wherever the pointer is.
//
// Go adaptation: typed callbacks receive the drag value as *T (a copy of
// the stored value, like OnDrop), the pin's downcast_ref.

import "reflect"

// DropListener is the behavior of a drop target for drags of type T
// (div.rs:1997-2110): an optional can-drop predicate, an optional
// drag-over style and an optional drop callback. Build one with its
// methods from the zero value and attach it with OnDropAlt.
type DropListener[T any] struct {
	canDrop    func(value *T, hitbox *Hitbox, w *Window, app *App) bool
	onDragOver func(value *T, w *Window, app *App) StyleRefinement
	onDrop     func(value *T, event *MouseUpEvent, w *Window, app *App)
}

// CanDropWithin decides whether drops reach this listener, while the
// element is hovered (div.rs:2022-2030 can_drop_within).
func (l DropListener[T]) CanDropWithin(predicate func(value *T, w *Window, app *App) bool) DropListener[T] {
	requireListener("CanDropWithin", predicate == nil)
	return l.CanDropAnywhere(func(value *T, hitbox *Hitbox, w *Window, app *App) bool {
		return hitbox.IsHovered(w) && predicate(value, w, app)
	})
}

// CanDropAnywhere decides whether drops reach this listener, given the
// element's hitbox; it replaces the default hovered gate, so a target
// may accept a drop released outside it (div.rs:2032-2039
// can_drop_anywhere).
func (l DropListener[T]) CanDropAnywhere(predicate func(value *T, hitbox *Hitbox, w *Window, app *App) bool) DropListener[T] {
	requireListener("CanDropAnywhere", predicate == nil)
	l.canDrop = predicate
	return l
}

// OnDragOver refines the element's style while a drag of type T that
// can drop here hovers it (div.rs:2043-2052 on_drag_over); f receives
// the default refinement.
func (l DropListener[T]) OnDragOver(f func(style StyleRefinement, value *T, w *Window, app *App) StyleRefinement) DropListener[T] {
	requireListener("OnDragOver", f == nil)
	l.onDragOver = func(value *T, w *Window, app *App) StyleRefinement {
		return f(StyleRefinement{}, value, w, app)
	}
	return l
}

// OnDrop processes a drop of type T on the element (div.rs:2056-2062
// on_drop).
func (l DropListener[T]) OnDrop(f func(value *T, event *MouseUpEvent, w *Window, app *App)) DropListener[T] {
	requireListener("OnDrop", f == nil)
	l.onDrop = f
	return l
}

// intoAny is the pinned into_any (div.rs:2064-2105): the typed
// callbacks wrapped to take the untyped drag value.
func (l DropListener[T]) intoAny() typedDropListener {
	out := typedDropListener{payloadType: reflect.TypeFor[T]()}
	if canDrop := l.canDrop; canDrop != nil {
		out.canDrop = func(value any, hitbox *Hitbox, w *Window, app *App) bool {
			typed, ok := value.(T)
			if !ok {
				return false
			}
			return canDrop(&typed, hitbox, w, app)
		}
	}
	if onDragOver := l.onDragOver; onDragOver != nil {
		out.onDragOver = func(value any, w *Window, app *App) StyleRefinement {
			typed := value.(T)
			return onDragOver(&typed, w, app)
		}
	}
	if onDrop := l.onDrop; onDrop != nil {
		out.onDrop = dropListenerInvoke[T](onDrop)
	}
	return out
}

// dropListenerFor returns the element's listener for a payload type,
// or nil (div.rs:632-638 get_mut_drop_listener).
func (d *DivElement) dropListenerFor(payloadType reflect.Type) *typedDropListener {
	for i := range d.dropListeners {
		if d.dropListeners[i].payloadType == payloadType {
			return &d.dropListeners[i]
		}
	}
	return nil
}

// OnDropAlt attaches a DropListener for drags of type T (div.rs:627-630,
// 1415 on_drop_alt). It is appended even when a listener for T exists;
// the first eligible listener of the type takes the drop.
func (d *DivElement) OnDropAlt[T any](listener DropListener[T]) *DivElement {
	d.dropListeners = append(d.dropListeners, listener.intoAny())
	return d
}

// CanDrop decides, for every drag type, whether drops and drag-over
// styles reach this element: a drop needs the element hovered and the
// predicate true (div.rs:645-653, 1425 can_drop). A rejected drop
// continues to the enclosing drop targets, so an outer target can take
// what an inner one refuses.
func (d *DivElement) CanDrop(predicate func(value any, w *Window, app *App) bool) *DivElement {
	requireListener("CanDrop", predicate == nil)
	d.canDropPredicate = func(value any, hitbox *Hitbox, w *Window, app *App) bool {
		return hitbox.IsHovered(w) && predicate(value, w, app)
	}
	return d
}

// OnDragOver refines this element's style while a drag of type S that
// can drop here hovers it (div.rs:1344-1370 on_drag_over); f receives
// the default refinement and the dragged value. It makes the element a
// drop target for S, so drags of S stop here.
func (d *DivElement) OnDragOver[S any](f func(style StyleRefinement, value *S, w *Window, app *App) StyleRefinement) *DivElement {
	requireListener("OnDragOver", f == nil)
	onDragOver := DropListener[S]{}.OnDragOver(f).intoAny().onDragOver
	if existing := d.dropListenerFor(reflect.TypeFor[S]()); existing != nil {
		existing.onDragOver = onDragOver
		return d
	}
	d.dropListeners = append(d.dropListeners, typedDropListener{payloadType: reflect.TypeFor[S](), onDragOver: onDragOver})
	return d
}

// DragOver is OnDragOver (div.rs:1334-1342; the pin marks it
// deprecated in favor of on_drag_over).
func (d *DivElement) DragOver[S any](f func(style StyleRefinement, value *S, w *Window, app *App) StyleRefinement) *DivElement {
	return d.OnDragOver(f)
}

// OnDragOverAny refines this element's style while any drag hovers it
// that one of its drop listeners can take (div.rs:660-668, 1375
// on_drag_over_any); f receives the default refinement and the untyped
// dragged value.
func (d *DivElement) OnDragOverAny(f func(style StyleRefinement, value any, w *Window, app *App) StyleRefinement) *DivElement {
	requireListener("OnDragOverAny", f == nil)
	d.dragOverStyle = func(value any, w *Window, app *App) StyleRefinement {
		return f(StyleRefinement{}, value, w, app)
	}
	return d
}

// GroupDragOver applies the style while a drag of type S that one of
// this element's drop listeners can take hovers the named group
// (div.rs:1384-1398 group_drag_over). It takes the pinned closure or a
// StyleBuilder, like GroupHover.
func (d *DivElement) GroupDragOver[S any, F StyleArg](group string, f F) *DivElement {
	d.groupDragOverStyles = append(d.groupDragOverStyles, typedGroupStyle{
		payloadType: reflect.TypeFor[S](),
		GroupStyle:  GroupStyle{Group: group, Style: *stateRefinement(f, "GroupDragOver")},
	})
	return d
}

// refineDragOver is the drag-over arm of compute_style_internal
// (div.rs:3973-4021): with a drag active, the first drop listener of
// the drag's type whose predicates pass makes the group drag-over
// styles of the type apply while their group is hovered, and the
// drag-over styles while this element is hovered. The callbacks run
// with the drag taken out of the application, like the pin.
func (d *DivElement) refineDragOver(style *Style, hitbox *Hitbox, drag *AnyDrag, w *Window, app *App) {
	if len(d.dropListeners) == 0 {
		return
	}
	app.activeDrag = nil
	defer func() { app.activeDrag = drag }()
	if d.canDropPredicate != nil && !d.canDropPredicate(drag.Value, hitbox, w, app) {
		return
	}
	var relevant *typedDropListener
	for i := range d.dropListeners {
		listener := &d.dropListeners[i]
		if !dropListenerPayloadMatches(drag.Value, listener.payloadType) {
			continue
		}
		if listener.canDrop == nil || listener.canDrop(drag.Value, hitbox, w, app) {
			relevant = listener
			break
		}
	}
	if relevant == nil {
		return
	}
	for _, group := range d.groupDragOverStyles {
		if id, ok := groupHitboxGet(group.Group, app); ok &&
			dropListenerPayloadMatches(drag.Value, group.payloadType) && id.IsHovered(w) {
			style.Refine(&group.Style)
		}
	}
	if hitbox.IsHovered(w) {
		if d.dragOverStyle != nil {
			refinement := d.dragOverStyle(drag.Value, w, app)
			style.Refine(&refinement)
		}
		if relevant.onDragOver != nil {
			refinement := relevant.onDragOver(drag.Value, w, app)
			style.Refine(&refinement)
		}
	}
}
