package base

// A measured, clipped vertical reveal (U:motion/reveal.rs).
//
// Core seams: retained element state is gpui.WithElementState
// (R:window.rs with_element_state), the layout node is
// gpui.RequestElementLayout (R:window.rs request_layout), the child is
// measured with AnyElement.LayoutAsRoot and placed with AnyElement.PrepaintAt
// (R:element.rs layout_as_root / prepaint_at), and the clip is
// gpui.WithContentMask (R:window.rs with_content_mask).

import "github.com/hoangvu12/gpui-go/gpui"

// MotionReveal reveals its child vertically by normalized progress: it is
// as tall as the child's measured height times the progress and clips the
// child to that height (U:motion/reveal.rs:6-11).
type MotionReveal struct {
	id       gpui.ElementID
	progress float32
	child    gpui.AnyElement
}

// revealState is the child's last measured height (U:motion/reveal.rs:13-16).
type revealState struct {
	height    float32
	hasHeight bool
}

// NewMotionReveal clamps progress to [0, 1] (U:motion/reveal.rs:18-26).
func NewMotionReveal[I gpui.IntoElementID](id I, progress float32, child gpui.AnyElement) *MotionReveal {
	return &MotionReveal{id: gpui.ElementIDOf(id), progress: clamp32(progress, 0, 1), child: child}
}

// IntoElement implements gpui.IntoElement (U:motion/reveal.rs:28-34).
func (r *MotionReveal) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, struct{}](r)
}

// ID implements gpui.Element (U:motion/reveal.rs:40-42).
func (r *MotionReveal) ID() (gpui.ElementID, bool) { return r.id, true }

// SourceLocation implements gpui.Element (U:motion/reveal.rs:44-46).
func (r *MotionReveal) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements gpui.Element (U:motion/reveal.rs:48-70): full
// width; the last measured height times the progress, or — before the
// child was measured — no height while revealing and zero while hidden.
func (r *MotionReveal) RequestLayout(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	if global == nil {
		panic("MotionReveal must have an id")
	}
	measured := gpui.WithElementState(w, global, func(state *revealState, _ *gpui.Window) (revealState, revealState) {
		var s revealState
		if state != nil {
			s = *state
		}
		return s, s
	})
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.DefiniteLengthOf(gpui.Fraction(1))
	switch {
	case !measured.hasHeight && r.progress > 0:
	case !measured.hasHeight:
		style.Size.Height = gpui.DefiniteLengthOf(gpui.DefinitePx(0))
	default:
		style.Size.Height = gpui.DefiniteLengthOf(gpui.DefinitePx(measured.height * r.progress))
	}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic("base: MotionReveal request_layout: " + err.Error())
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element (U:motion/reveal.rs:72-104): the child
// is measured at the reveal's width with a min-content height; a changed
// measurement requests another frame so the reveal's height follows; the
// child is prepainted at the reveal's origin under its clip.
func (r *MotionReveal) Prepaint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	if global == nil {
		panic("MotionReveal must have an id")
	}
	measured := r.child.LayoutAsRoot(gpui.AvailableSize{
		Width:  gpui.DefiniteAvailableSpace(bounds.Size.Width),
		Height: gpui.MinContentAvailableSpace(),
	}, w, app)
	changed := gpui.WithElementState(w, global, func(state *revealState, _ *gpui.Window) (bool, revealState) {
		var s revealState
		if state != nil {
			s = *state
		}
		changed := !s.hasHeight || s.height != measured.Height
		s.height, s.hasHeight = measured.Height, true
		return changed, s
	})
	if changed {
		gpui.RequestAnimationFrame(w)
	}
	gpui.WithContentMask(w, &bounds, func(w *gpui.Window) struct{} {
		r.child.PrepaintAt(bounds.Origin, w, app)
		return struct{}{}
	})
	return struct{}{}
}

// Paint implements gpui.Element (U:motion/reveal.rs:106-119).
func (r *MotionReveal) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, _ *struct{}, w *gpui.Window, app *gpui.App) {
	gpui.WithContentMask(w, &bounds, func(w *gpui.Window) struct{} {
		r.child.Paint(w, app)
		return struct{}{}
	})
}
