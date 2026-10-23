package gpui

// This file is Base ticket 08's deferred drawing: the pinned
// crates/gpui/src/elements/deferred.rs (the Deferred element,
// DeferredPriority and the DeferredPriorityStackCache) and the window
// half in crates/gpui/src/window.rs (GPUI-CE 254b5dbd, R: below):
// Window::defer_draw R:4302-4332, the DeferredDraw record, and
// draw_roots' prepaint_deferred_draws R:3580-3659 / paint_deferred_draws
// R:3661-3703 / deferred_draw_traversal_order R:3705-3710, called from
// render.go drawRoots at the pinned points (R:3436, R:3470).
//
// Go adaptations, recorded:
//
//   - The port's drawables re-resolve their layout bounds at paint
//     (element.go drawable.paint) where the pin reuses the prepainted
//     bounds, so a deferred draw's paint restores the absolute element
//     offset and the text style stack it prepainted under (the pin
//     restores them at prepaint only).
//   - The pinned prepaint/paint range reuse of a cached subtree's
//     deferred draws (gpui-perf ticket 04, view_cache.go): a deferred
//     draw carried over by a cached view's reuse_prepaint has no element
//     and replays the ranges it recorded last frame (R:3635-3652,
//     3685-3699).

import (
	"fmt"
	"sort"
)

// deferredDraw is one deferred draw record (the pinned DeferredDraw):
// the element plus the ambient state captured when it was deferred.
type deferredDraw struct {
	// currentView is the view rendering when the draw was deferred
	// (window.current_view()); hasView is false outside any view.
	currentView EntityID
	hasView     bool
	// parentNode is the dispatch node active when the draw was deferred.
	parentNode    DispatchNodeID
	hasParentNode bool
	// elementIDs and textStyles are the captured element id and text
	// style stacks.
	elementIDs []ElementID
	textStyles []TextStyleRefinement
	// contentMask clips the deferred paint when set.
	contentMask *Bounds
	// rem is the rem size captured at defer time.
	rem float32
	// priority orders the deferred draws (higher paints on top).
	priority int
	// element is the deferred element; empty for a draw carried over by
	// a cached view's reuse_prepaint (the pin's element: None).
	element AnyElement
	// absoluteOffset is the element offset the element prepaints under.
	absoluteOffset Point
	// prepaintStart/End and paintStart/End are the ranges the draw's
	// prepaint and paint produced in the frame that recorded them (the
	// pinned DeferredDraw prepaint_range / paint_range).
	prepaintStart, prepaintEnd prepaintStateIndex
	paintStart, paintEnd       paintIndex
}

// DeferDraw defers the drawing of the element, scheduling it to be
// painted on top of the currently drawn tree (R:window.rs:4310
// defer_draw): priority orders it relative to the other deferred draws
// (higher values draw on top); a non-nil contentMask clips its paint.
// Call it only during prepaint.
func (w *Window) DeferDraw(element AnyElement, absoluteOffset Point, priority int, contentMask *Bounds) {
	frame := currentFrame(w)
	if element.obj == nil {
		panic("gpui: DeferDraw requires an element")
	}
	draw := &deferredDraw{
		elementIDs:     append([]ElementID(nil), frame.elementIDs...),
		textStyles:     append([]TextStyleRefinement(nil), frame.textStyles...),
		rem:            frame.rem,
		priority:       priority,
		element:        element,
		absoluteOffset: absoluteOffset,
	}
	if contentMask != nil {
		mask := *contentMask
		draw.contentMask = &mask
	}
	if view, ok := CurrentView(w); ok {
		draw.currentView, draw.hasView = view, true
	}
	if node, ok := currentDispatchTree(w).ActiveNodeID(); ok {
		draw.parentNode, draw.hasParentNode = node, true
	}
	frame.deferredDraws = append(frame.deferredDraws, draw)
}

// withDeferredDrawScope runs f under a deferred draw's captured state:
// its rendered view, its element id and text style stacks, its rem size
// and its absolute element offset.
func withDeferredDrawScope(w *Window, draw *deferredDraw, f func(*Window)) {
	restoreDeferredDrawStacks(w, draw)
	run := func(w *Window) {
		WithRemSize(w, draw.rem, func(w *Window) struct{} {
			WithAbsoluteElementOffsetVoid(w, draw.absoluteOffset, f)
			return struct{}{}
		})
	}
	if draw.hasView {
		withRenderedViewVoid(w, draw.currentView, run)
		return
	}
	run(w)
}

// restoreDeferredDrawStacks restores a deferred draw's captured element id
// and text style stacks and makes its dispatch parent the active node.
func restoreDeferredDrawStacks(w *Window, draw *deferredDraw) {
	frame := currentFrame(w)
	frame.elementIDs = append(frame.elementIDs[:0:0], draw.elementIDs...)
	frame.textStyles = append(frame.textStyles[:0:0], draw.textStyles...)
	if draw.hasParentNode {
		currentDispatchTree(w).SetActiveNode(draw.parentNode)
	}
}

// deferredDrawTraversalOrder sorts indices [start, end) by priority,
// stable for equal priorities (the pinned sort_by_key, R:window.rs:3605
// and 3705-3710).
func deferredDrawTraversalOrder(draws []*deferredDraw, start, end int) []int {
	order := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		order = append(order, i)
	}
	sort.SliceStable(order, func(a, b int) bool { return draws[order[a]].priority < draws[order[b]].priority })
	return order
}

// prepaintDeferredDraws is R:window.rs:3580-3659 prepaint_deferred_draws:
// the deferred draws prepaint in rounds so a deferred element may defer
// more (each round sorts the draws it found by priority and prepaints
// them with their priority pushed on the priority stack); more than ten
// nested rounds panic.
func prepaintDeferredDraws(w *Window, app *App) {
	frame := currentFrame(w)
	if len(frame.elementIDs) != 0 {
		panic("gpui: prepaint_deferred_draws: the element id stack is not empty")
	}
	roundStart := 0
	depth := 0
	for {
		roundEnd := len(frame.deferredDraws)
		if roundStart == roundEnd {
			break
		}
		if depth >= 10 {
			panic("gpui: Exceeded maximum (10) deferred depth")
		}
		depth++
		for _, ix := range deferredDrawTraversalOrder(frame.deferredDraws, roundStart, roundEnd) {
			draw := frame.deferredDraws[ix]
			prepaintStart := w.prepaintIndex()
			if draw.element.obj != nil {
				withDeferredDrawScope(w, draw, func(w *Window) {
					frame.deferredPriorities = append(frame.deferredPriorities, draw.priority)
					defer func() { frame.deferredPriorities = frame.deferredPriorities[:len(frame.deferredPriorities)-1] }()
					draw.element.Prepaint(w, app)
				})
			} else {
				// R:3618-3633, 3647-3649: the stacks and the active node are
				// restored, then the recorded range is reused.
				restoreDeferredDrawStacks(w, draw)
				w.reusePrepaint(draw.prepaintStart, draw.prepaintEnd)
			}
			draw.prepaintStart, draw.prepaintEnd = prepaintStart, w.prepaintIndex()
		}
		frame.elementIDs = nil
		frame.textStyles = nil
		roundStart = roundEnd
	}
}

// paintDeferredDraws is R:window.rs:3661-3703 paint_deferred_draws: the
// scene's order floor is raised so every deferred draw sorts above the
// whole main scene, then the draws paint in priority order under their
// content masks.
func paintDeferredDraws(w *Window, app *App) {
	frame := currentFrame(w)
	if len(frame.elementIDs) != 0 {
		panic("gpui: paint_deferred_draws: the element id stack is not empty")
	}
	if len(frame.deferredDraws) == 0 {
		return
	}
	if err := frame.scene.RaiseOrderFloor(); err != nil {
		panic(fmt.Sprintf("gpui: paint_deferred_draws: raise order floor: %v", err))
	}
	for _, ix := range deferredDrawTraversalOrder(frame.deferredDraws, 0, len(frame.deferredDraws)) {
		draw := frame.deferredDraws[ix]
		paintStart := w.paintIndex()
		if draw.element.obj != nil {
			withDeferredDrawScope(w, draw, func(w *Window) {
				WithContentMaskVoid(w, draw.contentMask, func(w *Window) {
					draw.element.Paint(w, app)
				})
			})
		} else {
			// R:3679-3683, 3695-3697.
			restoreDeferredDrawStacks(w, draw)
			w.reusePaint(draw.paintStart, draw.paintEnd)
		}
		draw.paintStart, draw.paintEnd = paintStart, w.paintIndex()
	}
	frame.elementIDs = nil
	frame.textStyles = nil
}

// deferredPrioritiesOf returns the priorities of the frame's deferred draws
// in push order (the completed frame's observable, render.go).
func deferredPrioritiesOf(frame *frameDrawState) []int {
	if len(frame.deferredDraws) == 0 {
		return nil
	}
	out := make([]int, len(frame.deferredDraws))
	for i, draw := range frame.deferredDraws {
		out[i] = draw.priority
	}
	return out
}

// ---------------------------------------------------------------------------
// The Deferred element (elements/deferred.rs)
// ---------------------------------------------------------------------------

// DeferredPriority describes how the priority of a Deferred element is
// calculated (R:elements/deferred.rs:111-118). The zero value is the
// explicit priority 0 (the pinned deferred() default).
type DeferredPriority struct {
	// Auto selects DeferredPriority::Auto: 1 for a deferred element caused
	// by non-deferred elements, the causing deferred element's priority
	// plus one otherwise.
	Auto bool
	// Value is the explicit priority (DeferredPriority::Value) when Auto
	// is false.
	Value int
}

// evaluate is R:elements/deferred.rs:127-135 DeferredPriority::evaluate
// over the frame's priority stack (DeferredPriorityStackCache).
func (p DeferredPriority) evaluate(frame *frameDrawState) int {
	if !p.Auto {
		return p.Value
	}
	if n := len(frame.deferredPriorities); n > 0 {
		// saturating_add(1)
		if previous := frame.deferredPriorities[n-1]; previous < int(^uint(0)>>1) {
			return previous + 1
		}
		return frame.deferredPriorities[n-1]
	}
	// "NOTE: functionality change. If using auto, we start at a priority
	// of 1 instead of 0."
	return 1
}

// DeferredElement delays the painting of its child until after all of
// its ancestors, while keeping its layout as part of the current element
// tree (R:elements/deferred.rs:15-25 Deferred). Build one with Deferred.
type DeferredElement struct {
	child       AnyElement
	priority    DeferredPriority
	contentMask *Bounds
	taken       bool
}

// Deferred builds a Deferred element, which delays the layout and paint
// of its child (R:elements/deferred.rs:7 deferred): child is any child
// value (an element, IntoElement, string, view or recipe).
func Deferred(child any) *DeferredElement {
	return &DeferredElement{child: intoChild("Deferred", 0, child)}
}

// Priority sets an explicit priority for the element: a higher priority
// paints on top of deferred draws with a lower priority
// (R:elements/deferred.rs:89 priority).
func (d *DeferredElement) Priority(priority int) *DeferredElement {
	d.priority = DeferredPriority{Value: priority}
	return d
}

// WithPriority sets the priority description (R:elements/deferred.rs:89
// priority with an `impl Into<DeferredPriority>`).
func (d *DeferredElement) WithPriority(priority DeferredPriority) *DeferredElement {
	d.priority = priority
	return d
}

// PriorityAuto configures the priority to be one greater than the
// deferred element that caused this one, or 1 for a deferred element
// caused by non-deferred elements (R:elements/deferred.rs:97
// priority_auto).
func (d *DeferredElement) PriorityAuto() *DeferredElement {
	d.priority = DeferredPriority{Auto: true}
	return d
}

// ContentMask clips the deferred element to the mask
// (R:elements/deferred.rs:104 content_mask).
func (d *DeferredElement) ContentMask(mask Bounds) *DeferredElement {
	m := mask
	d.contentMask = &m
	return d
}

// IntoElement implements IntoElement (R:deferred.rs:78-84).
func (d *DeferredElement) IntoElement() AnyElement {
	return CustomElement[struct{}, struct{}](&deferredDrawable{deferred: d})
}

// deferredDrawable adapts the builder into the Element phases.
type deferredDrawable struct {
	deferred *DeferredElement
}

// ID implements Element (R:deferred.rs:31).
func (d *deferredDrawable) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element (R:deferred.rs:35).
func (d *deferredDrawable) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (R:deferred.rs:39-48): the child's
// layout is the deferred element's.
func (d *deferredDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	if d.deferred.taken {
		panic("gpui: Deferred request_layout after its child was deferred")
	}
	return d.deferred.child.RequestLayout(w, app), struct{}{}
}

// Prepaint implements Element (R:deferred.rs:50-63): the child is taken
// and deferred at the current element offset with the evaluated
// priority and the content mask.
func (d *deferredDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	if d.deferred.taken {
		panic("gpui: Deferred prepaint called twice (the pinned child.take().unwrap())")
	}
	d.deferred.taken = true
	frame := currentFrame(w)
	priority := d.deferred.priority.evaluate(frame)
	w.DeferDraw(d.deferred.child, ElementOffset(w), priority, d.deferred.contentMask)
	return struct{}{}
}

// Paint implements Element: the deferred element itself paints nothing
// (R:deferred.rs:65-75).
func (d *deferredDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
}
