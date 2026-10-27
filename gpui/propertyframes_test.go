package gpui

// Tests for browser-core ticket 60's property-only animation frames
// (propertyframes.go): a transform or opacity transition ticks the last
// drawn scene's property nodes in place — ZERO view renders per
// animation frame — the hit test follows the animated transform without
// a rebuild, and the correctness fallbacks (a notified view, a mixed
// non-compositable slot, an unbracketed group) redraw fully. The
// fallback-equals-property contract (the design's open question 4): a
// rebuilt frame at the same clock re-inserts the transition's current
// value, so a full frame and the property-only frames agree.
//
// The GPU-level half (the property tick's damaged present vs a full
// redraw of the same scene, byte-identical) runs in
// internal/rendererspec/r60_property_frame_test.go.

import (
	"testing"
	"time"
)

// pfView is the root view of the property-frame tests: one absolutely
// positioned box whose transform and opacity the test drives through
// the view state, plus a render counter (the acceptance observable:
// zero renders per animation frame).
type pfView struct {
	transformed bool
	translated  bool
	translate   bool
	faded       bool
	wide        bool
	blended     bool
	rules       []TransitionRule
	renders     int
}

func (v *pfView) Render(w *Window, cx *Context[pfView]) AnyElement {
	v.renders++
	// The transform list: a ONE-function list whose from-value is
	// scale(1, 1) and whose target is scale(1.75, 1.75) — matching lists
	// interpolate (none → a list would not — css-transforms-1's list
	// rule), and the from-value keeps the same function kind.
	fns := []TransformFunction{Scale(1, 1)}
	if v.transformed {
		fns = []TransformFunction{Scale(1.75, 1.75)}
	}
	if v.translate {
		// A translate-only list: the from-value translate(0, 0), the
		// target translate(80, 0).
		fns = []TransformFunction{Translate(DefinitePx(0), DefinitePx(0))}
		if v.translated {
			fns = []TransformFunction{Translate(DefinitePx(80), DefinitePx(0))}
		}
	}
	width := float32(40)
	if v.wide {
		width = 80
	}
	d := Div().ID("box").DebugSelector("box").
		Absolute().Left(Px(40)).Top(Px(30)).Size(Px(width), Px(40)).
		Bg(Rgb(0xFF0000)).
		Transition(v.rules...).
		Transform(fns...).
		// A mouse listener keeps the box's hitbox in the rendered frame
		// (the hit-test gate needs one).
		OnClick(func(*ClickEvent, *Window, *App) {})
	if v.blended {
		// A mix-blend-mode element (57): the blend group carries the
		// opacity (styleElementOpacity returns nil for the opacity
		// scope), so an opacity transition brackets no opacity group.
		d = d.MixBlendMode(BlendMultiply)
	}
	if v.faded {
		d = d.Opacity(0)
	}
	return Div().Flex().ID("root").SizeFull().Relative().Child(d).IntoElement()
}

// pfOpen opens a 200x150 test window rendering one pfView.
func pfOpen(t *testing.T, rules []TransitionRule) (*TestApp, *Window, *pfView) {
	t.Helper()
	ta := NewTestApp()
	v := &pfView{rules: rules}
	win := ta.OpenWindowWithOptions[pfView, *pfView](WindowOptions{Bounds: Bounds{Size: Size{Width: 200, Height: 150}}},
		func(state *pfView, _ *Window, _ *Context[pfView]) {
			*state = *v
			v = state
		})
	return ta, win.Window(), v
}

// pfChange applies the state change and notifies the view (the app's
// update shape: the change itself plus cx.Notify()), letting the flush
// draw the change frame — the transition's first frame, whose diff
// starts the run.
func pfChange(t *testing.T, ta *TestApp, w *Window, v *pfView, change func()) {
	t.Helper()
	ta.Update(func(app *App) {
		change()
		app.notify(viewEntityOf(t, w, v))
	})
	ta.RunUntilParked()
}

// pfNodeMatrix reads the last drawn scene's transform property-node
// matrix (the mutated record), the tick's observable beside the render
// count.
func pfNodeMatrix(t *testing.T, w *Window) (TransformationMatrix, uint32, bool) {
	t.Helper()
	scene := LastDrawnScene(w)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if !g.IsStart || !g.PropertyNode || !g.HasTransform {
			continue
		}
		return g.Transform, g.NodeID, true
	}
	return TransformationMatrix{}, 0, false
}

// pfNodeOpacity reads the last drawn scene's opacity property node.
func pfNodeOpacity(t *testing.T, w *Window) (float32, uint32, bool) {
	t.Helper()
	scene := LastDrawnScene(w)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if !g.IsStart || !g.PropertyNode || !g.HasOpacity {
			continue
		}
		return g.Opacity, g.NodeID, true
	}
	return 0, 0, false
}

// TestPropertyTransformTransitionTicksZeroViewRenders: the acceptance
// gate — a transform transition runs with ZERO view renders per
// animation frame. The change frame brackets the transform group as a
// property node; each animation frame request samples the retained run
// and mutates the node in place (the scene's group matrix and the
// debug bounds advance), no view renders and no frame is drawn.
func TestPropertyTransformTransitionTicksZeroViewRenders(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	if v.renders == 0 || WindowDrawCount(w) == 0 {
		t.Fatalf("the opening frame rendered nothing (renders %d, draws %d)", v.renders, WindowDrawCount(w))
	}

	// The change frame: the run starts; the group carries the property
	// node at the from-value (the identity scale).
	pfChange(t, ta, w, v, func() { v.transformed = true })
	before := WindowDrawCount(w)
	renders := v.renders
	matrix, nodeID, ok := pfNodeMatrix(t, w)
	if !ok || nodeID == 0 {
		t.Fatalf("the change frame emitted no transform property node (matrix %v, ok %v)", matrix, ok)
	}
	if !matrixEquals(matrix, UnitTransformation()) {
		t.Fatalf("the node's matrix at the change = %+v, want the identity (the from-value)", matrix)
	}
	if !DebugPropertyTickPending(w) {
		t.Fatal("the running transition requested no property frame")
	}

	// 200ms in: the tick samples 1.375. No render, no draw.
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if v.renders != renders {
		t.Fatalf("the animation frame rendered the view (%d -> %d), want zero renders per frame", renders, v.renders)
	}
	if WindowDrawCount(w) != before {
		t.Fatalf("the animation frame drew a window (%d -> %d), want zero draws per frame", before, WindowDrawCount(w))
	}
	matrix, nodeID2, _ := pfNodeMatrix(t, w)
	if nodeID2 != nodeID {
		t.Fatalf("the node id changed mid-transition (%d -> %d)", nodeID, nodeID2)
	}
	if want := float32(1.375); matrix.RotationScale[0][0] != want || matrix.RotationScale[1][1] != want {
		t.Fatalf("the node's matrix at 200ms = %+v, want the %v scale", matrix, want)
	}
	// The debug bounds follow the tick: the 40x40 box at (40,30), centre
	// (60,50), at 1.375 is 55x55 at (32.5, 22.5).
	bounds, ok := WindowDebugBounds(w)["box"]
	if !ok || bounds.Size.Width != 55 || bounds.Origin.X != 32.5 {
		t.Fatalf("the ticked debug bounds = %+v, want (32.5, 22.5, 55, 55)", bounds)
	}
	// The tick's damage is the update's old ∪ new mapped bounds — a
	// partial frame, never a full redraw.
	rects, full := w.LastFrameDamage()
	if full || len(rects) == 0 {
		t.Fatalf("the tick's damage = (%v, full=%v), want partial rects", rects, full)
	}
	if !DebugPropertyTickPending(w) {
		t.Fatal("the still-running transition requested no next property frame")
	}

	// 400ms in: the run finishes at the target 1.75 and requests no
	// further frame.
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if v.renders != renders {
		t.Fatalf("the final animation frame rendered the view (%d -> %d)", renders, v.renders)
	}
	if matrix, _, _ := pfNodeMatrix(t, w); matrix.RotationScale[0][0] != 1.75 {
		t.Fatalf("the node's matrix at 400ms = %+v, want the 1.75 target", matrix)
	}
	if DebugPropertyTickPending(w) {
		t.Fatal("the finished transition still requests property frames")
	}

	// The settled rebuild: the run is gone, the target applies, and the
	// element's group keeps the target matrix (a full frame agreeing
	// with the last ticked one — the node itself is gone with the run,
	// the settled transform needs no updates).
	refreshWindow(ta, w)
	if v.renders <= renders {
		t.Fatal("the settled rebuild did not re-render the view")
	}
	if matrix := pfAnyTransformMatrix(t, w); matrix.RotationScale[0][0] != 1.75 {
		t.Fatalf("the settled frame's matrix = %+v, want the 1.75 target (the full frame agrees with the ticks)", matrix)
	}
}

// pfAnyTransformMatrix reads the last drawn scene's first transform
// group's matrix (with or without a property node).
func pfAnyTransformMatrix(t *testing.T, w *Window) TransformationMatrix {
	t.Helper()
	scene := LastDrawnScene(w)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.IsStart && g.HasTransform {
			return g.Transform
		}
	}
	return TransformationMatrix{}
}

// TestPropertyOpacityTransitionTicksZeroViewRenders: an opacity
// transition likewise runs with zero view renders per frame — the
// group's opacity record mutates in place.
func TestPropertyOpacityTransitionTicksZeroViewRenders(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionOpacity, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	// The change frame: the run starts at opacity 1, which brackets no
	// group (WithElementOpacity is a no-op at 1) — the registry entry is
	// unemitted and the first tick falls back to a notify (a full frame,
	// exactly the pre-60 behaviour), whose sample at 1ms is below 1 and
	// brackets the node.
	pfChange(t, ta, w, v, func() { v.faded = true })
	ta.AdvanceClock(1)
	w.SimulateNextFrame(ta.App())
	if _, nodeID, ok := pfNodeOpacity(t, w); !ok || nodeID == 0 {
		t.Fatal("the opacity transition bracketed no property node after its first visible frame")
	}
	renders := v.renders
	before := WindowDrawCount(w)

	// 201ms in (200ms past the fallback frame): the sample is 0.4975.
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if v.renders != renders {
		t.Fatalf("the animation frame rendered the view (%d -> %d), want zero renders per frame", renders, v.renders)
	}
	if WindowDrawCount(w) != before {
		t.Fatalf("the animation frame drew a window (%d -> %d), want zero draws per frame", before, WindowDrawCount(w))
	}
	if opacity, _, _ := pfNodeOpacity(t, w); opacity < 0.49 || opacity > 0.505 {
		t.Fatalf("the node's opacity at 201ms = %v, want ~0.4975", opacity)
	}
	rects, full := w.LastFrameDamage()
	if full || len(rects) == 0 {
		t.Fatalf("the tick's damage = (%v, full=%v), want partial rects", rects, full)
	}

	// The end: the target 0.
	ta.AdvanceClock(199)
	w.SimulateNextFrame(ta.App())
	if v.renders != renders {
		t.Fatalf("the final animation frame rendered the view (%d -> %d)", renders, v.renders)
	}
	if opacity, _, _ := pfNodeOpacity(t, w); opacity != 0 {
		t.Fatalf("the node's opacity at 400ms = %v, want 0", opacity)
	}
	if DebugPropertyTickPending(w) {
		t.Fatal("the finished transition still requests property frames")
	}
}

// TestPropertyRebuildReinsertsTheCurrentValue: the design's open
// question 4 contract — a rebuilt frame at the SAME clock re-inserts
// the transition's current value, so a fallback full frame shows
// exactly what the property-only frames showed. The rebuilt scene's
// property node carries the tick's matrix.
func TestPropertyRebuildReinsertsTheCurrentValue(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	pfChange(t, ta, w, v, func() { v.transformed = true })
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	ticked, _, _ := pfNodeMatrix(t, w)
	if ticked.RotationScale[0][0] != 1.375 {
		t.Fatalf("the tick's matrix = %+v, want 1.375", ticked)
	}
	// A full rebuild at the same clock (the virtual clock holds): the
	// diff samples the same retained run at the same now.
	refreshWindow(ta, w)
	rebuilt, _, _ := pfNodeMatrix(t, w)
	if !matrixEquals(rebuilt, ticked) {
		t.Fatalf("the rebuilt frame's node matrix = %+v, want the tick's %+v (a fallback full frame must match the property-only frames)", rebuilt, ticked)
	}
}

// TestPropertyTickFollowsHitTesting: the hit test follows the animated
// transform on property-only frames — a pointer inside the element's
// visual span but outside its layout span hits it mid-flight (the
// hitbox matrix was recomposed without a rebuild).
func TestPropertyTickFollowsHitTesting(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	pfChange(t, ta, w, v, func() { v.transformed = true })

	// A translate transition (the visual box slides right by 40px over
	// 400ms): swap the target mid-flight by retargeting from the
	// element's perspective is complex; drive the scale case instead —
	// the 1.375 scale at 200ms puts the box at (32.5, 22.5)-(87.5, 77.5)
	// while its LAYOUT box is (40, 30)-(80, 70): a point inside the
	// visual span but outside the layout span (86, 74) must hit, and a
	// point inside the layout span but outside the visual one (41, 31)
	// must miss.
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if v.renders == 0 {
		t.Fatal("the view never rendered")
	}
	fs := focusState(w)
	frame := fs.rendered
	if frame == nil {
		t.Fatal("no rendered frame")
	}
	var box *Hitbox
	for _, record := range frame.hitboxes {
		if record.ownerPath == "root/box" || record.ownerPath == "box" {
			box = &record.box
			break
		}
	}
	if box == nil {
		// The id-path spelling differs; find the 40x40 box at (40, 30).
		for _, record := range frame.hitboxes {
			if record.box.Bounds.Origin == (Point{X: 40, Y: 30}) && record.box.Bounds.Size == (Size{Width: 40, Height: 40}) {
				box = &record.box
				break
			}
		}
	}
	if box == nil {
		t.Fatal("the box's hitbox was not found in the rendered frame")
	}
	if !box.Contains(Point{X: 86, Y: 74}) {
		t.Fatal("a point in the scaled element's visual span does not hit it mid-flight (the hitbox matrix did not follow the tick)")
	}
	// A point outside BOTH spans must miss (the matrix did not grow the
	// box unboundedly — the composition is exactly the tick's).
	if box.Contains(Point{X: 100, Y: 74}) {
		t.Fatal("a point outside the scaled element's visual span hits it mid-flight (the hitbox matrix did not follow the tick)")
	}
}

// TestPropertyNotifiedViewFallsBackToAFullFrame: a view notified in the
// same frame (the app changed other state) falls back to a full frame —
// the notify's rebuild re-samples the transition, the property tick is
// absorbed.
func TestPropertyNotifiedViewFallsBackToAFullFrame(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	pfChange(t, ta, w, v, func() { v.transformed = true })
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	renders := v.renders

	// Notify the view (an app-driven content change) and serve the frame
	// request: the rebuild wins, the view re-renders and the transition
	// re-samples inside it.
	ta.Update(func(app *App) {
		app.notify(viewEntityOf(t, w, v))
	})
	w.SimulateFrameRequest()
	if v.renders <= renders {
		t.Fatal("the notified frame did not re-render the view (the fallback must be a full frame)")
	}
	if matrix, _, _ := pfNodeMatrix(t, w); matrix.RotationScale[0][0] != 1.375 {
		t.Fatalf("the fallback frame's matrix = %+v, want the 200ms sample 1.375 (the rebuild re-inserts the current value)", matrix)
	}
}

// TestPropertyMixedSlotFallsBackToFullFrames: an element whose
// transition also animates a non-compositable property (width) keeps
// the notify-driven rebuild animation — the width needs a layout pass
// every frame (Chromium's main-thread property set,
// compositor_animations.cc:398-426's default arm).
func TestPropertyMixedSlotFallsBackToFullFrames(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()),
		NewTransitionRule(TransitionWidth, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	// Both runs start together: the transform's node still rides the
	// group, but the width run forces the notify path — every animation
	// frame is a full rebuild.
	pfChange(t, ta, w, v, func() { v.transformed = true; v.wide = true })
	if DebugPropertyTickPending(w) {
		t.Fatal("a mixed-slot transition (transform + width) requests property frames; the width run needs rebuilds")
	}
	renders := v.renders
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if v.renders <= renders {
		t.Fatal("the mixed-slot animation frame did not re-render the view (the width run needs a layout pass)")
	}
	// The rebuild re-sampled the transform at 200ms — the fallback
	// agrees with the property path.
	if matrix, _, _ := pfNodeMatrix(t, w); matrix.RotationScale[0][0] != 1.375 {
		t.Fatalf("the mixed frame's matrix = %+v, want the 200ms sample 1.375", matrix)
	}
	// The width transitioned too (the layout took the animated width:
	// the mapped AABB is 60 × 1.375 = 82.5 wide).
	if bounds, ok := WindowDebugBounds(w)["box"]; !ok || bounds.Size.Width != 82.5 {
		t.Fatalf("the mixed frame's mapped box = %v, want 82.5 wide (the 60px layout width at the 1.375 sample)", bounds)
	}
}

// TestPropertyRebuildWithActiveTransformDamagesFull: a rebuilt frame
// with an emitted transform property node presents full damage (the
// mapped composite the op-bounds rule cannot see).
func TestPropertyRebuildWithActiveTransformDamagesFull(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	pfChange(t, ta, w, v, func() { v.transformed = true })
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	// A rebuild while the run is active: the damage is full.
	refreshWindow(ta, w)
	if _, full := w.LastFrameDamage(); !full {
		t.Fatal("a rebuilt frame with an active transform transition must present full damage")
	}
}

// TestPropertyTranslateTransitionForcesTheGroup: a translation-only
// transform transition forces the matrix scope while it runs — the
// property node needs the group (the offset fast path emits none) — so
// a translate transition is property-only too (the native composites
// translation-only groups through the matrix, a lossless blit at
// integral offsets; ticket 51). The settled element returns to the
// offset fast path.
func TestPropertyTranslateTransitionForcesTheGroup(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionTransform, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	v.translate = true
	// The settle frame: the from-list must be a translate for the diff
	// to interpolate (a scale → translate change is discrete). This
	// frame renders [translate(0, 0)] and records it as the
	// before-change value.
	pfChange(t, ta, w, v, func() {})
	pfChange(t, ta, w, v, func() { v.translated = true })
	renders := v.renders
	before := WindowDrawCount(w)
	// The change frame's from-value is translate(0, 0) — the identity:
	// the forced scope brackets the group with the node anyway.
	if matrix, nodeID, ok := pfNodeMatrix(t, w); !ok || nodeID == 0 || matrix.Translation != [2]float32{0, 0} {
		t.Fatalf("the change frame emitted no translate property node (matrix %+v, ok %v)", matrix, ok)
	}
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if v.renders != renders || WindowDrawCount(w) != before {
		t.Fatalf("the translate tick rendered or drew (renders %d->%d, draws %d->%d)", renders, v.renders, before, WindowDrawCount(w))
	}
	// The node's matrix is DEVICE space (the test window's scale factor
	// is 2): the 40 logical px halfway sample is 80 device px.
	if matrix, _, _ := pfNodeMatrix(t, w); matrix.Translation != [2]float32{80, 0} || matrix.RotationScale != UnitTransformation().RotationScale {
		t.Fatalf("the node's matrix at 200ms = %+v, want the device translate(80, 0)", matrix)
	}
	// The debug bounds follow: the halfway translate is 40 logical px,
	// so the box (Left 40) shows at x=80.
	if bounds, ok := WindowDebugBounds(w)["box"]; !ok || bounds.Origin.X != 80 || bounds.Origin.Y != 30 {
		t.Fatalf("the ticked debug bounds = %+v, want (80, 30, 40, 40)", bounds)
	}
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if matrix, _, _ := pfNodeMatrix(t, w); matrix.Translation != [2]float32{160, 0} {
		t.Fatalf("the node's matrix at 400ms = %+v, want the device translate(160, 0)", matrix)
	}
	// The settled frame: the run is gone and the (translation-only)
	// target returns to the offset fast path — no group, the ops carry
	// the translated positions.
	refreshWindow(ta, w)
	if _, _, ok := pfNodeMatrix(t, w); ok {
		t.Fatal("the settled translate frame still carries a property node (the offset fast path should take over)")
	}
	if bounds, ok := WindowDebugBounds(w)["box"]; !ok || bounds.Origin.X != 120 || bounds.Origin.Y != 30 {
		t.Fatalf("the settled debug bounds = %+v, want (120, 30, 40, 40)", bounds)
	}
}

// TestPropertyBlendElementOpacityFallsBack: a mix-blend-mode element's
// opacity is carried by the blend group (ticket 57's styleElementOpacity
// — one effect node holds blend and opacity), which brackets no opacity
// property node, so an opacity transition on a blended element falls
// back to a full frame per tick (the notify path, the pre-60 animation).
func TestPropertyBlendElementOpacityFallsBack(t *testing.T) {
	ta, w, v := pfOpen(t, []TransitionRule{
		NewTransitionRule(TransitionOpacity, 400*time.Millisecond).WithEasing(EasingLinear()),
	})
	// The settle frame: the blended box adopts its blend style (the
	// first frame after the open re-renders with it).
	pfChange(t, ta, w, v, func() { v.blended = true })
	if _, _, ok := pfNodeOpacity(t, w); ok {
		t.Fatal("a blend element's opacity group carries a property node (the blend group should carry the opacity instead)")
	}
	pfChange(t, ta, w, v, func() { v.faded = true })
	// Advance past the first fallback frame so the diff has sampled
	// below 1, then gate: every animation frame re-renders (the notify
	// fallback), the transition still animates.
	ta.AdvanceClock(2)
	w.SimulateNextFrame(ta.App())
	renders := v.renders
	for i := 0; i < 3; i++ {
		ta.AdvanceClock(50)
		w.SimulateNextFrame(ta.App())
		if v.renders <= renders {
			t.Fatal("a blend element's opacity transition did not re-render (the blend group carries the opacity; the tick must fall back to full frames)")
		}
		renders = v.renders
	}
	// The blend group's opacity still animated through the rebuilds (the
	// scene's blend group carries the sampled opacity).
	scene := LastDrawnScene(w)
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range groups {
		if g.IsStart && g.HasBlend && g.HasOpacity {
			found = true
			// 400ms linear from 1 to 0: the three 50ms steps land at
			// 152ms past the change — the sample is 0.62.
			if abs32(g.Opacity-0.62) > 0.01 {
				t.Fatalf("the blend group's opacity at 152ms = %v, want the sampled 0.62 (the transition animates through the full frames)", g.Opacity)
			}
		}
	}
	if !found {
		t.Fatal("the blended box's scene carries no blend group with opacity")
	}
}

// viewEntityOf resolves the root view's entity id (the notify target).
func viewEntityOf(t *testing.T, w *Window, v *pfView) EntityID {
	t.Helper()
	ds, ok := windowDrawStates[w]
	if !ok || ds.root == nil {
		t.Fatal("the window has no root view")
	}
	id, ok := ds.root.EntityID()
	if !ok {
		t.Fatal("the root view has no entity id")
	}
	return id
}

// matrixEquals compares two matrices field for field.
func matrixEquals(a, b TransformationMatrix) bool {
	return a.RotationScale == b.RotationScale && a.Translation == b.Translation
}
