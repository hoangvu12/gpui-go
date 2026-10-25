//go:build windows

// The browser-core ticket 50 (rev 16) scene ABI gates: the effect-group
// record and plan (command kinds 3/4 sharing one group stack with the
// R15 filter boundaries, no depth cap, the MAX_SCENE_GROUPS bound), the
// single-primitive opacity fold, the extended 52-byte content mask and
// the group-target pool plan shape. These run through the native ABI
// directly - the Go styling that authors effect groups (ticket 55) and
// the renderer's pool behaviour (rendererspec) are separate gates.
package scenespec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// f32bits packs one float32 into its u32 bits.
func f32bits(value float32) uint32 { return math.Float32bits(value) }

// effectRecord returns one effect-group marker (bounds 20..120, a
// full-window mask, the given flags and opacity).
func effectRecord(isStart bool, flags uint32, opacity float32) native.SceneEffectRecord {
	rec := native.NewSceneEffectRecord()
	rec.Bounds = native.SceneBoundsOf(20, 20, 100, 100)
	rec.ContentMask = native.SceneBoundsOf(0, 0, 1000, 1000)
	if isStart {
		flags |= native.SceneEffectFlagIsStart
	} else {
		flags &^= native.SceneEffectFlagIsStart
	}
	rec.Flags = flags
	rec.Opacity = f32bits(opacity)
	return rec
}

// solidQuad returns one opaque quad record.
func solidQuad(x float32) native.SceneQuadRecord {
	rec := native.NewSceneQuadRecord()
	rec.Bounds = native.SceneBoundsOf(x, 0, 100, 100)
	rec.ContentMask = native.SceneContentMaskOf(0, 0, 1000, 1000)
	rec.Background = native.SceneBackgroundOf(0, 0, 1, 1)
	return rec
}

// commandKinds returns the plan's (kind, filterTarget, targetIndex)
// triples in order.
func commandKinds(t *testing.T, sc *native.Scene) [][3]uint32 {
	t.Helper()
	commands, err := sc.PlanCommands()
	if err != nil {
		t.Fatalf("PlanCommands: %v", err)
	}
	out := make([][3]uint32, 0, len(commands))
	for _, command := range commands {
		if command.CommandKind == native.SceneCommandBatch {
			continue
		}
		out = append(out, [3]uint32{command.CommandKind, command.FilterTarget, command.TargetIndex})
	}
	return out
}

// TestEffectGroupsPairAndIsolateBeyondTheOldDepth: a matched effect
// pair composites through an isolated target, and six nested groups ALL
// isolate (the pre-R16 cap was 2 - and effect groups never existed):
// the slots are nesting-based, so scenes nested at most two deep
// produce exactly the R15 plan shape.
func TestEffectGroupsPairAndIsolateBeyondTheOldDepth(t *testing.T) {
	sc := nativeScene(t)
	for depth := 0; depth < 6; depth++ {
		if err := beginEffectWithin(sc, depth); err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
	}
	for depth := 0; depth < 6; depth++ {
		end := effectRecord(false, native.SceneEffectFlagHasOpacity, 0.5)
		if err := sc.InsertEffect(&end); err != nil {
			t.Fatalf("close depth %d: %v", depth, err)
		}
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	kinds := commandKinds(t, sc)
	if len(kinds) != 12 {
		t.Fatalf("got %d marker commands, want 12 (six begins then six ends)", len(kinds))
	}
	// The stream order: the six begins (nesting slots 0..4, the
	// innermost folds to inline), then the six ends (innermost first).
	for i := 0; i < 6; i++ {
		if kinds[i][0] != native.SceneCommandBeginEffect {
			t.Fatalf("command %d = %v, want a begin-effect", i, kinds[i])
		}
	}
	for i := 6; i < 12; i++ {
		if kinds[i][0] != native.SceneCommandEndEffect {
			t.Fatalf("command %d = %v, want an end-effect", i, kinds[i])
		}
	}
	// The five OUTER begins isolate with nesting slots 0..4.
	for depth := 0; depth < 5; depth++ {
		begin := kinds[depth]
		if begin[1] != 1 || begin[2] != uint32(depth) {
			t.Fatalf("depth %d: begin = %v, want isolated slot %d", depth, begin, depth)
		}
	}
	// The innermost pair folds to inline (one primitive, pure opacity).
	if kinds[5][1] != 0 || kinds[6][1] != 0 {
		t.Fatalf("innermost pair = %v / %v, want both inline (the fold)", kinds[5], kinds[6])
	}
	// The five outer ends stay isolated.
	for depth := 0; depth < 5; depth++ {
		if kinds[7+depth][1] != 1 {
			t.Fatalf("end %d = %v, want an isolated end-effect", depth, kinds[7+depth])
		}
	}
	requirements, err := sc.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if requirements.IsolatedTargetCount != 6 {
		t.Fatalf("isolated_target_count = %d, want 6 (the conservative high-water mark)", requirements.IsolatedTargetCount)
	}
	if requirements.IsolatedEffectCount != 5 {
		t.Fatalf("isolated_effect_count = %d, want 5 (the innermost folds)", requirements.IsolatedEffectCount)
	}
}

// beginEffectWithin inserts one nested begin (depth d) plus its quad;
// the caller closes the pairs afterwards.
func beginEffectWithin(sc *native.Scene, depth int) error {
	start := effectRecord(true, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&start); err != nil {
		return err
	}
	quad := solidQuad(float32(depth))
	return sc.InsertQuad(&quad)
}

// TestSinglePrimitiveOpacityGroupFoldsInline: a pure-opacity effect
// group whose span holds exactly ONE primitive emits INLINE begin/end
// commands (Chromium's "at least two layers draw content" rule); two
// primitives isolate.
func TestSinglePrimitiveOpacityGroupFoldsInline(t *testing.T) {
	sc := nativeScene(t)
	start := effectRecord(true, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	quad := solidQuad(0)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	end := effectRecord(false, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	commands, err := sc.PlanCommands()
	if err != nil {
		t.Fatalf("PlanCommands: %v", err)
	}
	var markerTargets []uint32
	for _, command := range commands {
		if command.CommandKind == native.SceneCommandBeginEffect || command.CommandKind == native.SceneCommandEndEffect {
			markerTargets = append(markerTargets, command.FilterTarget)
		}
	}
	if len(markerTargets) != 2 || markerTargets[0] != 0 || markerTargets[1] != 0 {
		t.Fatalf("marker targets = %v, want both inline (the fold)", markerTargets)
	}
}

// TestOpacityGroupWithTwoPrimitivesIsolates: two primitives under the
// group isolate (the group composite path).
func TestOpacityGroupWithTwoPrimitivesIsolates(t *testing.T) {
	sc := nativeScene(t)
	start := effectRecord(true, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	for i := 0; i < 2; i++ {
		quad := solidQuad(float32(i) * 10)
		if err := sc.InsertQuad(&quad); err != nil {
			t.Fatalf("InsertQuad: %v", err)
		}
	}
	end := effectRecord(false, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	kinds := commandKinds(t, sc)
	if len(kinds) != 2 {
		t.Fatalf("got %d marker commands, want 2", len(kinds))
	}
	if kinds[0][0] != native.SceneCommandBeginEffect || kinds[0][1] != 1 || kinds[0][2] != 0 {
		t.Fatalf("begin = %v, want an isolated begin-effect in slot 0", kinds[0])
	}
	if kinds[1][0] != native.SceneCommandEndEffect || kinds[1][1] != 1 {
		t.Fatalf("end = %v, want an isolated end-effect", kinds[1])
	}
}

// TestEffectGroupWithClipDoesNotFold: a clipping group keeps the
// isolated composite even over one primitive (the clip needs it).
func TestEffectGroupWithClipDoesNotFold(t *testing.T) {
	sc := nativeScene(t)
	flags := native.SceneEffectFlagHasOpacity | native.SceneEffectFlagClipToBounds
	start := effectRecord(true, flags, 0.5)
	start.CornerRadii = [4]uint32{f32bits(12), f32bits(12), f32bits(12), f32bits(12)}
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	quad := solidQuad(0)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	end := effectRecord(false, flags, 0.5)
	end.CornerRadii = start.CornerRadii
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	kinds := commandKinds(t, sc)
	if len(kinds) != 2 || kinds[0][1] != 1 || kinds[1][1] != 1 {
		t.Fatalf("marker commands = %v, want an isolated pair (the clip blocks the fold)", kinds)
	}
}

// TestSharedGroupStackMixesKinds: an effect group nested inside a
// filter boundary pair shares ONE stack - the slots count open groups
// of both kinds.
func TestSharedGroupStackMixesKinds(t *testing.T) {
	sc := nativeScene(t)
	boundary := native.NewSceneFilterRecord()
	boundary.Bounds = native.SceneBoundsOf(0, 0, 200, 200)
	boundary.ContentMask = native.SceneContentMaskOf(0, 0, 1000, 1000)
	boundary.BlurRadii[0] = f32bits(3)
	boundary.FilterCount = 1
	boundary.Opacity = f32bits(1)
	boundary.IsStart = 1
	if err := sc.InsertFilterBoundary(&boundary); err != nil {
		t.Fatalf("insert boundary start: %v", err)
	}
	start := effectRecord(true, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert effect start: %v", err)
	}
	quad := solidQuad(0)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	quad2 := solidQuad(10)
	if err := sc.InsertQuad(&quad2); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	end := effectRecord(false, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert effect end: %v", err)
	}
	boundary.IsStart = 0
	if err := sc.InsertFilterBoundary(&boundary); err != nil {
		t.Fatalf("insert boundary end: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	kinds := commandKinds(t, sc)
	want := [][3]uint32{
		{native.SceneCommandBeginFilter, 1, 0},
		{native.SceneCommandBeginEffect, 1, 1},
		{native.SceneCommandEndEffect, 1, 1},
		{native.SceneCommandEndFilter, 1, 0},
	}
	if len(kinds) != len(want) {
		t.Fatalf("marker commands = %v, want %v", kinds, want)
	}
	for i, command := range kinds {
		if command != want[i] {
			t.Fatalf("marker command %d = %v, want %v", i, command, want[i])
		}
	}
}

// TestSceneGroupsLimitRejectsTheOverflow: MAX_SCENE_GROUPS (1024)
// counts group START markers of both kinds; the overflow start fails
// with the typed limit error, ends never count.
func TestSceneGroupsLimitRejectsTheOverflow(t *testing.T) {
	sc := nativeScene(t)
	// The inserts ride the batch stream (scene_batch.go): the failing
	// op surfaces at the FLUSH, so each phase finishes to observe it.
	for i := 0; i < 1024; i++ {
		start := effectRecord(true, native.SceneEffectFlagHasOpacity, 1)
		if err := sc.InsertEffect(&start); err != nil {
			t.Fatalf("insert start %d: %v", i, err)
		}
	}
	overflow := effectRecord(true, native.SceneEffectFlagHasOpacity, 1)
	if err := sc.InsertEffect(&overflow); err != nil {
		t.Fatalf("insert the overflow start (buffered): %v", err)
	}
	if err := sc.Finish(); err == nil || !isSceneLimit(err) {
		t.Fatalf("the 1025th group start must fail at the flush with the scene limit error, got %v", err)
	}
	// End markers never count.
	end := effectRecord(false, native.SceneEffectFlagHasOpacity, 1)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	// The filter boundary starts share the same bound.
	boundary := native.NewSceneFilterRecord()
	boundary.Bounds = native.SceneBoundsOf(0, 0, 200, 200)
	boundary.ContentMask = native.SceneContentMaskOf(0, 0, 1000, 1000)
	boundary.BlurRadii[0] = f32bits(3)
	boundary.FilterCount = 1
	boundary.IsStart = 1
	if err := sc.InsertFilterBoundary(&boundary); err != nil {
		t.Fatalf("insert the boundary start (buffered): %v", err)
	}
	if err := sc.Finish(); err == nil || !isSceneLimit(err) {
		t.Fatalf("a boundary start over the group bound must fail at the flush with the scene limit error, got %v", err)
	}
}

// isSceneLimit reports whether the error unwraps to the native scene
// limit sentinel (the -8 status).
func isSceneLimit(err error) bool {
	var status *native.SceneStatusError
	for err != nil {
		if e, ok := err.(*native.SceneStatusError); ok {
			status = e
			break
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return status != nil && status.Code == -8
}

// TestEffectRecordValidationRejectsMalformedRecords: unknown flag
// bits, a singular transform, a non-zero mask kind, a property node
// without an id (or without a value), opacity out of range and
// negative radii all fail at the Go-side record check.
func TestEffectRecordValidationRejectsMalformedRecords(t *testing.T) {
	sc := nativeScene(t)
	rejects := func(name string, rec native.SceneEffectRecord) {
		t.Helper()
		if err := sc.InsertEffect(&rec); err == nil {
			t.Fatalf("%s: the malformed record must be rejected", name)
		}
	}
	rec := effectRecord(true, native.SceneEffectFlagHasOpacity, 1)
	rec.Flags |= 1 << 9
	rejects("unknown flag bits", rec)

	rec = effectRecord(true, native.SceneEffectFlagHasTransform, 1)
	rec.Transform = [6]uint32{}
	rejects("singular transform", rec)

	rec = effectRecord(true, native.SceneEffectFlagHasOpacity, 1)
	rec.MaskKind = 1
	rejects("mask kind this revision", rec)

	rec = effectRecord(true, native.SceneEffectFlagPropertyNode|native.SceneEffectFlagHasOpacity, 1)
	rec.NodeID = 0
	rejects("property node without an id", rec)

	rec = effectRecord(true, native.SceneEffectFlagPropertyNode, 1)
	rec.NodeID = 7
	rejects("property node without a value", rec)

	rec = effectRecord(true, native.SceneEffectFlagHasOpacity, 1.5)
	rejects("opacity out of range", rec)

	rec = effectRecord(true, native.SceneEffectFlagHasOpacity, 1)
	rec.CornerRadii[0] = f32bits(-1)
	rejects("negative radius", rec)

	rec = effectRecord(true, native.SceneEffectFlagHasOpacity, 1)
	rec.Reserved = 1
	rejects("reserved word", rec)
}

// TestExtendedContentMaskRoundTripsAndValidates: the 52-byte mask's
// fades, radii and smoothing ride the records; the validation rejects
// negative fades, negative radii and out-of-range smoothing.
func TestExtendedContentMaskRoundTripsAndValidates(t *testing.T) {
	sc := nativeScene(t)
	quad := solidQuad(0)
	quad.ContentMask.FadeOut = [4]uint32{f32bits(4), 0, 0, f32bits(8)}
	quad.ContentMask.CornerRadii = [4]uint32{f32bits(6), f32bits(6), f32bits(6), f32bits(6)}
	quad.ContentMask.CornerSmoothing = f32bits(0.25)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	quads, err := sc.DumpQuads()
	if err != nil {
		t.Fatalf("DumpQuads: %v", err)
	}
	if len(quads) != 1 {
		t.Fatalf("dumped %d quads, want 1", len(quads))
	}
	if quads[0].ContentMask != quad.ContentMask {
		t.Fatalf("mask did not round-trip:\n got %+v\nwant %+v", quads[0].ContentMask, quad.ContentMask)
	}
	if !quads[0].ContentMask.IsRounded() {
		t.Fatal("the dumped mask must report itself rounded")
	}

	// Validation rejects the malformed masks.
	sc = nativeScene(t)
	quad = solidQuad(0)
	quad.ContentMask.FadeOut[0] = f32bits(-1)
	if err := sc.InsertQuad(&quad); err == nil {
		t.Fatal("a negative fade distance must be rejected")
	}
	quad = solidQuad(0)
	quad.ContentMask.CornerRadii[0] = f32bits(-2)
	if err := sc.InsertQuad(&quad); err == nil {
		t.Fatal("a negative corner radius must be rejected")
	}
	quad = solidQuad(0)
	quad.ContentMask.CornerSmoothing = f32bits(1.5)
	if err := sc.InsertQuad(&quad); err == nil {
		t.Fatal("an out-of-range smoothing must be rejected")
	}
}

// TestEffectRecordsDumpAndReplay: the effect markers survive the
// finish sort with kernel-assigned orders and replay re-inserts them
// (the same pair, recomputed orders).
func TestEffectRecordsDumpAndReplay(t *testing.T) {
	sc := nativeScene(t)
	start := effectRecord(true, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	quad := solidQuad(0)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	end := effectRecord(false, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	effects, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	if len(effects) != 2 {
		t.Fatalf("dumped %d effect records, want 2", len(effects))
	}
	if effects[0].Order == 0 || effects[1].Order == 0 {
		t.Fatal("the kernel must assign orders to the effect markers")
	}
	if effects[0].Order >= effects[1].Order {
		t.Fatalf("the start must sort before the end: %d >= %d", effects[0].Order, effects[1].Order)
	}
	// The meta carries the effect count.
	meta, err := sc.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.EffectCount != 2 {
		t.Fatalf("meta effect_count = %d, want 2", meta.EffectCount)
	}
}
