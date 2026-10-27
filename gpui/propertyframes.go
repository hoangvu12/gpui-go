package gpui

// This file is browser-core ticket 60: property-only animation frames.
// An active transform or opacity transition ticks the LAST DRAWN
// SCENE's property nodes in place — Scene.UpdateEffectMatrix /
// UpdateEffectOpacity (rev 16, ticket 51) — and re-presents through
// the damage entry, skipping render, layout and paint entirely; the
// animation frame stops notifying the whole view. Detection is
// AUTOMATIC from the active transition (the user's 2026-10-24
// decision, design §8.1 Q2): no opt-in style, exactly the properties
// Chromium runs on the compositor.
//
// The borrowed model is Chromium's compositor-only frame:
//
//   - only opacity, transform/translate/rotate/scale, filter and
//     backdrop-filter run without a main-frame update
//     (third_party/blink/renderer/core/animation/compositor_animations.cc:
//     398-426 — the compositable-property switch; every other
//     property falls to the main thread). This port's compositable
//     pair is the transition engine's transform and opacity slots
//     (transition.go); width, height, background and border colour
//     keep the notify-and-rebuild animation the engine always had.
//   - the tick mutates the property node, not the layer tree:
//     ElementAnimations::OnOpacityAnimated / OnTransformAnimated call
//     SetElementOpacityMutated / SetElementTransformMutated
//     (cc/animation/element_animations.cc:487-499, :520-536) from the
//     compositor tick (KeyframeEffect::Tick, cc/animation/
//     keyframe_effect.cc:120-139) — no commit, no rebuild; the same
//     in-place contract Scene.UpdateEffectMatrix/Opacity carries
//     (ticket 51: "no re-sort, no re-plan", the invariant cc relies
//     on when ticking compositor animations).
//   - the tick asks for a redraw, not a rebuild: AnimateInternal's
//     did_animate arm calls SetNeedsRedraw(/*animation_only=*/true)
//     (cc/trees/layer_tree_host_impl.cc:924-949). The port's frame
//     loop wake is the analogue (frameloop.go), and the redraw is the
//     damage-limited present of the mutated scene.
//
// Correctness first — the fallback rule: anything the frame cannot
// prove property-only redraws fully. A notified view, a refresh
// request, a notified-during-draw view, an abandoned build's invalid
// damage state, paint flashing, a value the scene rejects (a singular
// mid-flight scale) and an active-but-unbracketed group (an element
// inside a parent paragraph, an opacity at 1, a backdrop-filter
// element) all fall back; a rebuilt frame re-inserts the transition's
// CURRENT value — the diff samples the same retained run at the
// frame's clock (design §5 risk 4 / open question Q4's contract), so
// a fallback full frame and the property-only frames show the same
// value. A rebuilt frame with an EMITTED transform node damages FULL
// (damage.go): the transform group's visible pixels live at the
// mapped bounds, not at the ops' local bounds the damage rule reads.
//
// Hit testing follows the animated transform: each hitbox under an
// animated transform scope records the scope stack's OWN matrices at
// insertion (transform_paint.go), and the tick recomposes the
// hitbox's matrix by replacing the node's own matrix in the chain —
// Chromium accumulates the transform chain with origins applied and
// maps the location through the accumulated inverse
// (core/paint/hit_testing_transform_state.cc:36-49, ticket 56's
// citation) — so hover, click, capture and drag targets answer from
// the moved geometry without a rebuild.

import (
	"math"
	"reflect"
	"sort"
)

// propertyTransitionKind is which scene property a node drives.
type propertyTransitionKind uint8

const (
	// propertyNodeTransform drives Scene.UpdateEffectMatrix (the
	// element's transform effect group).
	propertyNodeTransform propertyTransitionKind = iota
	// propertyNodeOpacity drives Scene.UpdateEffectOpacity (the
	// element's opacity effect group).
	propertyNodeOpacity
)

// transitionProperty is the transition-engine slot the kind rides.
func (k propertyTransitionKind) transitionProperty() TransitionProperty {
	if k == propertyNodeTransform {
		return TransitionTransform
	}
	return TransitionOpacity
}

// propertyNodeDraft is one element's active transform/opacity
// transition as the frame under construction sees it, keyed by the
// element's global id path (resolveStyleTransitions records it; the
// div prepaint fills the geometry; the paint's group emissions mark
// the nodes). A draft exists only while a run is delayed or running —
// a finished or absent run leaves no draft, so the settled frame's
// group carries no node at all.
type propertyNodeDraft struct {
	// elementKey is the element's global id path — the element-state
	// key the tick samples the retained run through.
	elementKey string
	// view is the view whose render contains the element (the
	// fallback notify's target when the frame cannot bracket a group).
	view EntityID
	// transformNodeID / opacityNodeID are the scene property-node ids
	// (0 until the run first becomes active). They are assigned ONCE
	// per element from the window's counter and persisted in the
	// element's transition state, so every frame of the transition
	// carries the same id.
	transformNodeID uint32
	opacityNodeID   uint32
	// transformActive / opacityActive: the slot's run sampled active
	// this frame (delayed or running).
	transformActive bool
	opacityActive   bool
	// transformEmitted / opacityEmitted: the paint actually bracketed
	// the group carrying the node (an element inside a parent
	// paragraph, an opacity at 1 or a backdrop-filter element emits
	// none — the tick's notify fallback).
	transformEmitted bool
	opacityEmitted   bool
	// The transform node's geometry: the border box, transform-origin,
	// rem and device scale the prepaint resolved the matrix against —
	// the tick re-resolves the sampled function list with exactly
	// these, so a ticked matrix equals what a rebuild would emit.
	bounds Bounds
	origin TransformOrigin
	rem    float32
	scale  float32
	// debugSelector is the element's debug selector ("" when it has
	// none): the tick re-records the mapped AABB so Tier A probes
	// follow the animation without a rebuild.
	debugSelector string
	// emittedMatrix / emittedOpacity are the device-space values this
	// frame left on the nodes — the tick's skip-equal baseline (a
	// delayed run's held sample presents nothing).
	emittedMatrix  TransformationMatrix
	emittedOpacity float32
}

// propertyAnimation is one published property node of the last
// completed frame — the registry the frame loop's tick consumes. It
// carries the draft's identity and geometry plus the applied value
// (updated by each tick, so consecutive ticks compare against the
// current node, not the frame's emission).
type propertyAnimation struct {
	elementKey string
	view       EntityID
	nodeID     uint32
	kind       propertyTransitionKind
	// emitted reports whether the frame bracketed the group carrying
	// the node. An active run whose frame did not (paragraph content,
	// opacity 1, a backdrop-filter element) falls back to a full frame
	// per tick — the notify path the engine used before this ticket.
	emitted bool
	// The transform node's geometry (kind = transform).
	bounds Bounds
	origin TransformOrigin
	rem    float32
	scale  float32
	// debugSelector: the element's debug selector ("" when none).
	debugSelector string
	// appliedMatrix / appliedOpacity: the value the last frame (built
	// or ticked) left on the node.
	appliedMatrix  TransformationMatrix
	appliedOpacity float32
}

// framePropertyDraft returns the frame's property-node draft for the
// element, or nil (no draft: no active transform/opacity transition).
// One map-nil check for transition-free frames — the element key is
// only built when some element's transition is active.
func framePropertyDraftOf(w *Window, global *GlobalElementID) *propertyNodeDraft {
	if global == nil {
		return nil
	}
	ds, ok := windowDrawStates[w]
	if !ok || ds.frame == nil || len(ds.frame.propertyNodeDrafts) == 0 {
		return nil
	}
	return ds.frame.propertyNodeDrafts[global.key()]
}

// framePropertyDraft is framePropertyDraftOf with a pre-built key (the
// tick's element-state reads).
func framePropertyDraft(w *Window, key string) *propertyNodeDraft {
	ds, ok := windowDrawStates[w]
	if !ok || ds.frame == nil || len(ds.frame.propertyNodeDrafts) == 0 {
		return nil
	}
	return ds.frame.propertyNodeDrafts[key]
}

// recordPropertyTransitionDraft records the element's active
// transform/opacity runs in the frame under construction and assigns
// the property-node ids (persisted in the element's transition state,
// so the id survives every frame of the transition). Called by
// resolveStyleTransitions at each of the frame's style resolutions;
// the paint's group emission consumes the ids.
func recordPropertyTransitionDraft(w *Window, global *GlobalElementID, state *transitionElementState, style *Style, transformActive, opacityActive bool) {
	if global == nil || (!transformActive && !opacityActive) {
		return
	}
	frame := currentFrame(w)
	if frame == nil {
		return
	}
	if frame.propertyNodeDrafts == nil {
		frame.propertyNodeDrafts = make(map[string]*propertyNodeDraft)
	}
	key := global.key()
	draft := frame.propertyNodeDrafts[key]
	if draft == nil {
		view, _ := currentViewOf(w)
		draft = &propertyNodeDraft{elementKey: key, view: view}
		frame.propertyNodeDrafts[key] = draft
	}
	ds := drawState(w)
	if transformActive {
		if state.transformNodeID == 0 {
			ds.nextPropertyNodeID++
			state.transformNodeID = ds.nextPropertyNodeID
		}
		draft.transformNodeID = state.transformNodeID
		draft.transformActive = true
	}
	if opacityActive {
		if state.opacityNodeID == 0 {
			ds.nextPropertyNodeID++
			state.opacityNodeID = ds.nextPropertyNodeID
		}
		draft.opacityNodeID = state.opacityNodeID
		draft.opacityActive = true
		// The sampled opacity is the value the paint's group carries;
		// the tick compares its own sample against it.
		if style.Opacity != nil {
			draft.emittedOpacity = *style.Opacity
		}
	}
}

// fillPropertyDraftGeometry records the transform node's geometry at
// the div prepaint: the border box, origin, rem and scale the matrix
// resolves against, and the debug selector the tick re-records.
func fillPropertyDraftGeometry(draft *propertyNodeDraft, bounds Bounds, origin *TransformOrigin, rem, scale float32, selector string) {
	draft.bounds = bounds
	draft.origin = OriginFrom(origin)
	draft.rem = rem
	draft.scale = scale
	draft.debugSelector = selector
}

// publishPropertyAnimations builds the registry of the completed
// frame's property nodes (deterministic key order): one entry per
// active transform or opacity run, emitted or not — the not-emitted
// entries are the tick's notify fallback.
func publishPropertyAnimations(frame *frameDrawState) []*propertyAnimation {
	if len(frame.propertyNodeDrafts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(frame.propertyNodeDrafts))
	for key := range frame.propertyNodeDrafts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []*propertyAnimation
	for _, key := range keys {
		draft := frame.propertyNodeDrafts[key]
		if draft.transformActive && draft.transformNodeID != 0 {
			out = append(out, &propertyAnimation{
				elementKey:    key,
				view:          draft.view,
				nodeID:        draft.transformNodeID,
				kind:          propertyNodeTransform,
				emitted:       draft.transformEmitted,
				bounds:        draft.bounds,
				origin:        draft.origin,
				rem:           draft.rem,
				scale:         draft.scale,
				debugSelector: draft.debugSelector,
				appliedMatrix: draft.emittedMatrix,
			})
		}
		if draft.opacityActive && draft.opacityNodeID != 0 {
			out = append(out, &propertyAnimation{
				elementKey:     key,
				view:           draft.view,
				nodeID:         draft.opacityNodeID,
				kind:           propertyNodeOpacity,
				emitted:        draft.opacityEmitted,
				debugSelector:  draft.debugSelector,
				appliedOpacity: draft.emittedOpacity,
			})
		}
	}
	return out
}

// requestPropertyAnimationFrame schedules the window's next frame as
// a property tick: the frame request samples the active transform and
// opacity transition runs and updates the last drawn scene's nodes —
// NO view notification, so nothing re-renders. The tick's fallback
// (a run the frame could not bracket, any invalid state) redraws the
// window fully, which is the behaviour RequestAnimationFrame's notify
// always had.
func requestPropertyAnimationFrame(w *Window) {
	if w == nil {
		return
	}
	fs := frameStateOf(w)
	fs.propertyTickPending = true
	w.wakeFrameSource()
}

// ---------------------------------------------------------------------------
// The tick
// ---------------------------------------------------------------------------

// tickPropertyAnimations serves one property-only animation frame:
// sample the registered nodes' retained runs at the app clock, update
// the last drawn scene's nodes in place, publish the damage (the
// update's old ∪ new mapped bounds, dilated ~2 px for the composite's
// bilinear support — the r53/r54 tests' rule), recompose the hitboxes
// under the animated transform and re-record the debug bounds. It
// reports success; a false answer (or an early notify) means the
// caller falls back to a full frame — the same content, rebuilt.
//
// The tick mutates only the scene's records and the window's
// published state: the transition runs are sampled read-only (the
// diff at the next rebuilt frame re-samples the same retained runs at
// that frame's clock — the fallback-equals-property contract), no
// view renders, no layout, no paint, and WindowDrawCount stands.
func (w *Window) tickPropertyAnimations(app *App) bool {
	ds, ok := windowDrawStates[w]
	if !ok || ds.closed || ds.lastScene == nil || ds.lastScene.kernel == nil {
		return false
	}
	// The correctness gate: the last frame's damage state must be
	// valid (an abandoned build's next frame is a full redraw), and
	// the paint-flashing overlay's tints must not go stale (the tick
	// inserts nothing into the scene, so the overlay would freeze).
	if !ds.lastDamageValid || ds.paintFlashing {
		return false
	}
	scene := ds.lastScene
	fs := frameStateOf(w)
	now := app.Now()
	clipW := int32(math.Ceil(float64(ds.lastDamageViewport.Width) * float64(ds.lastDamageScale)))
	clipH := int32(math.Ceil(float64(ds.lastDamageViewport.Height) * float64(ds.lastDamageScale)))
	var raw []DamageRect
	changed := false
	anyActive := false
	for _, anim := range ds.lastPropertyAnimations {
		state := readTransitionElementState(ds, anim.elementKey)
		if state == nil {
			// The element's state was evicted (it stopped drawing — a
			// CachedView freeze): the run is gone; stop ticking it (the
			// frozen value stays until the view re-renders, the motion
			// engine's recorded limitation).
			continue
		}
		slot, _ := anim.kind.transitionProperty().slot()
		run := state.runs[slot]
		if run == nil {
			continue
		}
		value, active := run.sample(satSubDuration(now, run.startedAt))
		if !anim.emitted {
			// An active run whose last frame bracketed no group (an
			// element inside a parent paragraph, an opacity at 1, a
			// backdrop-filter element): the transition needs a rebuild —
			// notify the view exactly as RequestAnimationFrame did and
			// let the caller draw the full frame.
			if active {
				notifyPropertyFallback(app, w, anim.view)
				return false
			}
			continue
		}
		if active {
			anyActive = true
		}
		switch anim.kind {
		case propertyNodeTransform:
			if !value.hasFns {
				continue
			}
			logical, okM := transformMatrixOf(value.fns, &anim.origin, anim.bounds, anim.rem)
			if !okM {
				// A singular mid-flight sample (a scale crossing zero):
				// the scene rejects the matrix — a full frame (the
				// collapse path paints nothing).
				return false
			}
			device := deviceTransform(logical, anim.scale)
			if device == anim.appliedMatrix {
				// The held sample (a delayed run): nothing changed on
				// the node, nothing to present.
				continue
			}
			oldBounds, newBounds, err := scene.UpdateEffectMatrix(anim.nodeID, device)
			if err != nil {
				return false
			}
			anim.appliedMatrix = device
			raw = appendPropertyDamage(raw, oldBounds, newBounds)
			updateHitboxesForNode(w, anim.nodeID, logical)
			updateDebugBoundsForNode(w, anim, logical)
			changed = true
		case propertyNodeOpacity:
			if value.num == anim.appliedOpacity {
				continue
			}
			oldBounds, newBounds, err := scene.UpdateEffectOpacity(anim.nodeID, value.num)
			if err != nil {
				return false
			}
			anim.appliedOpacity = value.num
			raw = appendPropertyDamage(raw, oldBounds, newBounds)
			changed = true
		}
	}
	// Publish the tick's damage (the present reads it through
	// LastFrameDamage / PresentFrame) and request the present.
	if changed {
		ds.lastDamageRects = mergeDamageRects(raw, clipW, clipH)
		ds.lastDamageFull = false
		fs.needsPresent = true
	}
	if anyActive {
		// The runs still ticking: schedule the next property frame.
		requestPropertyAnimationFrame(w)
	}
	return true
}

// notifyPropertyFallback is the tick's full-frame fallback: notify the
// view whose render contains the element (RequestAnimationFrame's own
// callback), or refresh the whole window when no view was recorded.
func notifyPropertyFallback(app *App, w *Window, view EntityID) {
	if view != 0 {
		app.notify(view)
		return
	}
	w.requestRefresh()
}

// readTransitionElementState reads the element's retained transition
// state (the runs the tick samples) without the WithElementState
// take/put dance — the tick runs between frames, so nothing else can
// hold the slot.
func readTransitionElementState(ds *windowDrawState, key string) *transitionElementState {
	byType, ok := ds.elementStates[key]
	if !ok {
		return nil
	}
	value, ok := byType[reflect.TypeFor[transitionElementState]()]
	if !ok {
		return nil
	}
	state, ok := value.(*transitionElementState)
	if !ok {
		return nil
	}
	return state
}

// appendPropertyDamage adds one update's old and new mapped bounds to
// the raw damage list, each dilated ~2 px outward — the composite's
// bilinear support (the r53/r54 property-tick tests' rule: the damage
// is the update's old ∪ new mapped bounds, inflated by the half-texel
// neighbourhood).
func appendPropertyDamage(rects []DamageRect, oldBounds, newBounds Bounds) []DamageRect {
	for _, b := range [2]Bounds{oldBounds, newBounds} {
		if b.Size.Width <= 0 || b.Size.Height <= 0 {
			continue
		}
		x := int32(math.Floor(float64(b.Origin.X))) - 2
		y := int32(math.Floor(float64(b.Origin.Y))) - 2
		w := int32(math.Ceil(float64(b.Origin.X+b.Size.Width))) - x + 4
		h := int32(math.Ceil(float64(b.Origin.Y+b.Size.Height))) - y + 4
		rects = append(rects, DamageRect{X: x, Y: y, W: w, H: h})
	}
	return rects
}

// updateHitboxesForNode recomposes every hitbox whose transform chain
// carries the node: the chain holds the open scopes' OWN matrices at
// insertion, so replacing the node's entry with the new own matrix
// and folding left reproduces exactly the composed matrix a rebuild
// would capture (pushTransformScope composes parent·own). The hitbox
// bounds and content mask are unchanged — the matrix carries the
// mapping, Chromium's hit-test model.
func updateHitboxesForNode(w *Window, nodeID uint32, own TransformationMatrix) {
	fs := focusState(w)
	frame := fs.rendered
	if frame == nil {
		return
	}
	for _, record := range frame.hitboxes {
		nodes := record.box.transformChainNodes
		if len(nodes) == 0 || len(record.box.transformChain) != len(nodes) {
			continue
		}
		index := -1
		for i, id := range nodes {
			if id == nodeID {
				index = i
				break
			}
		}
		if index < 0 || record.box.transform == nil {
			continue
		}
		composed := UnitTransformation()
		for i, entry := range record.box.transformChain {
			if i == index {
				composed = mulTransformation(composed, own)
			} else {
				composed = mulTransformation(composed, entry)
			}
		}
		*record.box.transform = composed
	}
}

// updateDebugBoundsForNode re-records the element's debug-selector
// bounds at the ticked matrix (the mapped AABB the prepaint records —
// getBoundingClientRect geometry), so Tier A probes follow the
// animation without a rebuild.
func updateDebugBoundsForNode(w *Window, anim *propertyAnimation, logical TransformationMatrix) {
	if anim.debugSelector == "" {
		return
	}
	ds, ok := windowDrawStates[w]
	if !ok || ds.lastDebugBounds == nil {
		return
	}
	ds.lastDebugBounds[anim.debugSelector] = mapBoundsThrough(logical, anim.bounds)
}

// propertyScopeChain snapshots the open transform scopes' own
// matrices and node ids when any open scope is a property node (an
// animated transform): the property tick recomposes the hitbox's
// matrix by replacing the node's own matrix in the chain. Hitboxes
// outside any animated transform record nothing (the common case —
// one nil check).
func propertyScopeChain(frame *frameDrawState) (chain []TransformationMatrix, nodes []uint32, ok bool) {
	if frame == nil || len(frame.transformNodeStack) == 0 {
		return nil, nil, false
	}
	any := false
	for _, id := range frame.transformNodeStack {
		if id != 0 {
			any = true
			break
		}
	}
	if !any {
		return nil, nil, false
	}
	chain = append([]TransformationMatrix(nil), frame.transformOwnStack...)
	nodes = append([]uint32(nil), frame.transformNodeStack...)
	return chain, nodes, true
}

// DebugPropertyTickPending reports whether the window has a property
// animation tick pending (test support: the property-only frame's
// observable beside WindowDrawCount).
func DebugPropertyTickPending(w *Window) bool {
	if w == nil {
		return false
	}
	return frameStateOf(w).propertyTickPending
}
