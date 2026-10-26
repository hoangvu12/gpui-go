//go:build windows

// The browser-core ticket 52 (rev 16) scene ABI gates: the gradient
// table (insert/dump round-trip, the 64-stop cap, the validation), the
// coverage entries, the drop-shadow op chains, the effect groups' mask
// and blend payload, the fold skip for table-gradient quads and the
// blend groups' forced offscreen route. These run through the native
// ABI directly - the rendererspec gates cover the pixels.
package scenespec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// gradientRecord returns one linear gradient record (the given kind,
// tile and geometry) with the given stops.
func gradientRecord(kind, tile uint32, geometry [8]float32) native.SceneGradientRecord {
	rec := native.NewSceneGradientRecord()
	rec.Kind = kind
	rec.Tile = tile
	for i, value := range geometry {
		rec.Geometry[i] = math.Float32bits(value)
	}
	return rec
}

// rgbaStop packs one straight sRGBA byte quadruple into the stop word.
func rgbaStop(r, g, b, a byte) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16 | uint32(a)<<24
}

// linearStops returns three fixed stops (red, green, blue at 0, .5, 1).
func linearStops() []native.SceneGradientStop {
	return []native.SceneGradientStop{
		{Position: f32bits(0), Rgba: rgbaStop(255, 0, 0, 255)},
		{Position: f32bits(0.5), Rgba: rgbaStop(0, 255, 0, 255)},
		{Position: f32bits(1), Rgba: rgbaStop(0, 0, 255, 255)},
	}
}

// TestGradientTableInsertAndDumpRoundTrips: the gradient insert
// assigns stop_row (the table index, 1:1) and the dump returns the
// records and the concatenated stops; the meta record carries the
// table counts.
func TestGradientTableInsertAndDumpRoundTrips(t *testing.T) {
	sc := nativeScene(t)
	stops := linearStops()
	rec := gradientRecord(native.SceneGradientKindLinear, native.SceneGradientTilePad, [8]float32{0, 0, 100, 80})
	rec.StopCount = uint32(len(stops))
	first, err := sc.InsertGradient(&rec, stops)
	if err != nil {
		t.Fatalf("InsertGradient: %v", err)
	}
	if first != 0 {
		t.Fatalf("the first gradient's index is %d, want 0", first)
	}
	radial := gradientRecord(native.SceneGradientKindRadial, native.SceneGradientTileRepeat, [8]float32{60, 40, 0, 50, 1})
	radial.Geometry[2] = f32bits(0)
	radial.Geometry[3] = f32bits(50)
	radial.Geometry[4] = f32bits(1)
	radialStops := []native.SceneGradientStop{
		{Position: f32bits(0), Rgba: rgbaStop(255, 255, 255, 255)},
		{Position: f32bits(1), Rgba: rgbaStop(0, 0, 0, 0)},
	}
	radial.StopCount = 2
	second, err := sc.InsertGradient(&radial, radialStops)
	if err != nil {
		t.Fatalf("InsertGradient (radial): %v", err)
	}
	if second != 1 {
		t.Fatalf("the second gradient's index is %d, want 1", second)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	dumped, dumpedStops, err := sc.GradientDump()
	if err != nil {
		t.Fatalf("GradientDump: %v", err)
	}
	if len(dumped) != 2 {
		t.Fatalf("dumped %d gradients, want 2", len(dumped))
	}
	if dumped[0].StopRow != 0 || dumped[1].StopRow != 1 {
		t.Fatalf("stop rows (%d, %d), want (0, 1)", dumped[0].StopRow, dumped[1].StopRow)
	}
	if dumped[0].Kind != native.SceneGradientKindLinear || dumped[0].Tile != native.SceneGradientTilePad {
		t.Fatalf("the linear record round-tripped as kind %d tile %d", dumped[0].Kind, dumped[0].Tile)
	}
	if math.Float32frombits(dumped[0].Geometry[2]) != 100 || math.Float32frombits(dumped[0].Geometry[3]) != 80 {
		t.Fatalf("the linear geometry round-tripped wrong: %v", dumped[0].Geometry)
	}
	if len(dumpedStops) != 5 {
		t.Fatalf("dumped %d stops, want 5", len(dumpedStops))
	}
	if dumpedStops[0].Rgba != rgbaStop(255, 0, 0, 255) || dumpedStops[4].Rgba != rgbaStop(0, 0, 0, 0) {
		t.Fatalf("the stop words round-tripped wrong: %v %v", dumpedStops[0], dumpedStops[4])
	}
	meta, err := sc.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.GradientCount != 2 {
		t.Fatalf("meta gradient count %d, want 2", meta.GradientCount)
	}
}

// TestGradientStopCapRejectsAbove64: the native service rejects a stop
// array over the 64-stop cap (the GO layer resamples; the native cap is
// the hard bound). 64 stops insert fine.
func TestGradientStopCapRejectsAbove64(t *testing.T) {
	sc := nativeScene(t)
	stops := make([]native.SceneGradientStop, 64)
	for i := range stops {
		stops[i] = native.SceneGradientStop{Position: f32bits(float32(i) / 63), Rgba: rgbaStop(255, 0, 0, 255)}
	}
	rec := gradientRecord(native.SceneGradientKindLinear, native.SceneGradientTilePad, [8]float32{0, 0, 10, 10})
	rec.StopCount = 64
	if _, err := sc.InsertGradient(&rec, stops); err != nil {
		t.Fatalf("64 stops must insert: %v", err)
	}
	over := make([]native.SceneGradientStop, 65)
	for i := range over {
		over[i] = stops[i%64]
	}
	rec2 := gradientRecord(native.SceneGradientKindLinear, native.SceneGradientTilePad, [8]float32{0, 0, 10, 10})
	rec2.StopCount = 65
	if _, err := sc.InsertGradient(&rec2, over); err == nil {
		t.Fatal("65 stops must be rejected (the GO layer resamples above 64)")
	}
}

// TestGradientValidationRejectsMalformedRecords: kind, tile, the sRGB
// space, the kind-specific degeneracy rules, stop_row, the stop count
// agreement and the stop ordering.
func TestGradientValidationRejectsMalformedRecords(t *testing.T) {
	sc := nativeScene(t)
	stops := linearStops()
	mk := func() (native.SceneGradientRecord, []native.SceneGradientStop) {
		rec := gradientRecord(native.SceneGradientKindLinear, native.SceneGradientTilePad, [8]float32{0, 0, 100, 80})
		rec.StopCount = 3
		return rec, linearStops()
	}
	rejects := func(name string, rec native.SceneGradientRecord, stops []native.SceneGradientStop) {
		t.Helper()
		if _, err := sc.InsertGradient(&rec, stops); err == nil {
			t.Fatalf("%s: the malformed gradient must be rejected", name)
		}
	}
	rec, _ := mk()
	rec.Kind = 3
	rejects("kind out of range", rec, stops)
	rec, _ = mk()
	rec.Tile = 3
	rejects("tile out of range", rec, stops)
	rec, _ = mk()
	rec.ColorSpace = 1
	rejects("oklab reserved", rec, stops)
	rec, _ = mk()
	rec.Geometry = [8]uint32{f32bits(0), f32bits(0), f32bits(0), f32bits(0)}
	rejects("degenerate line", rec, stops)
	rec, _ = mk()
	rec.Kind = native.SceneGradientKindRadial
	rec.Geometry = [8]uint32{f32bits(50), f32bits(50), f32bits(10), f32bits(10), f32bits(1)}
	rejects("degenerate radii", rec, stops)
	rec.Kind = native.SceneGradientKindRadial
	rec.Geometry = [8]uint32{f32bits(50), f32bits(50), f32bits(0), f32bits(10), f32bits(0)}
	rejects("zero aspect", rec, stops)
	rec = gradientRecord(native.SceneGradientKindConic, native.SceneGradientTilePad, [8]float32{50, 50, 0, 0})
	rec.StopCount = 3
	rejects("zero conic sweep", rec, stops)
	rec, _ = mk()
	rec.StopRow = 1
	rejects("stop row set on insert", rec, stops)
	rec, _ = mk()
	rec.StopCount = 2
	rejects("stop count disagreement", rec, stops)
	reversed := []native.SceneGradientStop{
		{Position: f32bits(1), Rgba: rgbaStop(255, 0, 0, 255)},
		{Position: f32bits(0.5), Rgba: rgbaStop(0, 255, 0, 255)},
		{Position: f32bits(0), Rgba: rgbaStop(0, 0, 255, 255)},
	}
	rec, _ = mk()
	rejects("descending stops", rec, reversed)
	rec, _ = mk()
	outOfRange := linearStops()
	outOfRange[1].Position = f32bits(1.5)
	rejects("stop position out of range", rec, outOfRange)
}

// TestCoverageEntriesInsertAndCount: the coverage insert validates the
// bounds and the meta record carries the count.
func TestCoverageEntriesInsertAndCount(t *testing.T) {
	sc := nativeScene(t)
	rec := native.NewSceneCoverageRecord()
	rec.PathIndex = 0
	rec.Bounds = native.SceneBoundsOf(10, 10, 80, 60)
	index, err := sc.InsertCoverage(&rec)
	if err != nil {
		t.Fatalf("InsertCoverage: %v", err)
	}
	if index != 0 {
		t.Fatalf("the coverage index is %d, want 0", index)
	}
	bad := native.NewSceneCoverageRecord()
	bad.Bounds = native.SceneBoundsOf(float32(math.NaN()), 0, 10, 10)
	if _, err := sc.InsertCoverage(&bad); err == nil {
		t.Fatal("a non-finite bound must be rejected")
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	meta, err := sc.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.CoverageCount != 1 {
		t.Fatalf("meta coverage count %d, want 1", meta.CoverageCount)
	}
}

// TestFilterOpChainsInsertAndReference: the drop-shadow op chain
// inserts (validated), the meta record carries the count and the
// filter boundary's appended op-chain words round-trip through the
// dump; 0/0 stays the R15 shape.
func TestFilterOpChainsInsertAndReference(t *testing.T) {
	sc := nativeScene(t)
	op := native.NewSceneFilterOpRecord()
	op.Offset[0] = f32bits(4)
	op.Offset[1] = f32bits(6)
	op.BlurRadius = f32bits(8)
	op.Color = native.SceneColorOf(0, 0, 0, 0.5)
	chain, err := sc.InsertFilterOpChain([]native.SceneFilterOpRecord{op})
	if err != nil {
		t.Fatalf("InsertFilterOpChain: %v", err)
	}
	if chain != 0 {
		t.Fatalf("the chain index is %d, want 0", chain)
	}
	// The R15 boundary with the chain reference.
	boundary := native.NewSceneFilterRecord()
	boundary.Bounds = native.SceneBoundsOf(10, 10, 80, 60)
	boundary.ContentMask = native.SceneContentMaskOf(0, 0, 1000, 1000)
	boundary.BlurRadii[0] = f32bits(2)
	boundary.FilterTags[0] = 0
	boundary.FilterCount = 1
	boundary.Opacity = f32bits(1)
	boundary.IsStart = 1
	boundary.OpChainIndex = chain
	boundary.OpChainCount = 1
	if err := sc.InsertFilterBoundary(&boundary); err != nil {
		t.Fatalf("InsertFilterBoundary: %v", err)
	}
	// The malformed shapes: a count over the bound, a count without
	// the R15 zero index staying legal (index 0 IS chain 0 - the 0/0
	// pair alone means the R15 legacy shape), a non-drop-shadow op
	// kind.
	bad := boundary
	bad.OpChainCount = 9
	if err := sc.InsertFilterBoundary(&bad); err == nil {
		t.Fatal("an op-chain count over the bound must be rejected")
	}
	bad = boundary
	bad.OpChainIndex = 0
	bad.OpChainCount = 0
	if err := sc.InsertFilterBoundary(&bad); err != nil {
		t.Fatalf("the 0/0 pair is the R15 legacy shape and must validate: %v", err)
	}
	badOp := native.NewSceneFilterOpRecord()
	badOp.Kind = native.SceneFilterOpBlur
	if _, err := sc.InsertFilterOpChain([]native.SceneFilterOpRecord{badOp}); err == nil {
		t.Fatal("a blur-kind op must be rejected (the R15 tags carry the chain's blurs)")
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	boundaries, err := sc.DumpBoundaries()
	if err != nil {
		t.Fatalf("DumpBoundaries: %v", err)
	}
	if len(boundaries) != 2 {
		t.Fatalf("dumped %d boundaries, want 2 (the chain reference and the 0/0 legacy)", len(boundaries))
	}
	if boundaries[0].OpChainIndex != 0 || boundaries[0].OpChainCount != 1 {
		t.Fatalf("the boundary's op-chain reference round-tripped wrong: %+v", boundaries[0])
	}
	if boundaries[1].OpChainIndex != 0 || boundaries[1].OpChainCount != 0 {
		t.Fatalf("the 0/0 legacy boundary round-tripped wrong: %+v", boundaries[1])
	}
	meta, err := sc.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.FilterChainCount != 1 {
		t.Fatalf("meta filter chain count %d, want 1", meta.FilterChainCount)
	}
}

// TestEffectMaskAndBlendValidate: the effect record's mask kinds 1-3
// and the blend modes validate (the composite resolves them at render);
// the none-kind mask still demands zero reference and mode.
func TestEffectMaskAndBlendValidate(t *testing.T) {
	sc := nativeScene(t)
	inserts := func(rec native.SceneEffectRecord) error {
		return sc.InsertEffect(&rec)
	}
	masked := effectRecord(true, native.SceneEffectFlagHasOpacity|native.SceneEffectFlagHasMask, 1)
	masked.MaskKind = 1
	masked.MaskReference = 0
	if err := inserts(masked); err != nil {
		t.Fatalf("a gradient mask group must validate: %v", err)
	}
	end := masked
	end.Flags = native.SceneEffectFlagHasOpacity
	if err := inserts(end); err != nil {
		t.Fatalf("end: %v", err)
	}
	coverageMasked := effectRecord(true, native.SceneEffectFlagHasMask, 1)
	coverageMasked.MaskKind = 3
	if err := inserts(coverageMasked); err != nil {
		t.Fatalf("a coverage mask group must validate: %v", err)
	}
	endCoverage := effectRecord(false, native.SceneEffectFlagHasMask, 1)
	endCoverage.MaskKind = 3
	if err := inserts(endCoverage); err != nil {
		t.Fatalf("end coverage: %v", err)
	}
	noneBad := effectRecord(true, native.SceneEffectFlagHasOpacity, 1)
	noneBad.MaskKind = 0
	noneBad.MaskReference = 3
	if err := inserts(noneBad); err == nil {
		t.Fatal("a none-kind mask with a reference must be rejected")
	}
	modeBad := effectRecord(true, native.SceneEffectFlagHasMask, 1)
	modeBad.MaskKind = 1
	modeBad.MaskMode = 2
	if err := inserts(modeBad); err == nil {
		t.Fatal("a mask mode out of range must be rejected")
	}
	blendBad := effectRecord(true, native.SceneEffectFlagHasBlend, 1)
	blendBad.BlendMode = 17
	if err := inserts(blendBad); err == nil {
		t.Fatal("a blend mode out of range must be rejected")
	}
}

// TestBlendGroupForcesTheOffscreenRoute: a matched blend group's plan
// sets uses_offscreen_target (the composite samples a backdrop COPY of
// the parent, so the base target must be a readable texture); a plain
// opacity group does not.
func TestBlendGroupForcesTheOffscreenRoute(t *testing.T) {
	sc := nativeScene(t)
	quad := solidQuadAt(0)
	group := effectRecord(true, native.SceneEffectFlagHasBlend|native.SceneEffectFlagHasOpacity, 1)
	group.BlendMode = 1 // multiply
	if err := sc.InsertEffect(&group); err != nil {
		t.Fatalf("InsertEffect: %v", err)
	}
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	end := effectRecord(false, native.SceneEffectFlagHasBlend|native.SceneEffectFlagHasOpacity, 1)
	end.BlendMode = 1
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("end effect: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	requirements, err := sc.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if requirements.UsesOffscreenTarget == 0 {
		t.Fatal("a blend group must force the offscreen route (uses_offscreen_target)")
	}
}

// TestTableGradientQuadDoesNotFold: a pure-opacity group whose single
// primitive carries a TABLE-GRADIENT background (tag 4) does NOT fold
// (the fold multiplies the record's stored alpha payload, which the
// table arm carries none of) - the plan keeps the isolated group.
func TestTableGradientQuadDoesNotFold(t *testing.T) {
	sc := nativeScene(t)
	stops := linearStops()
	rec := gradientRecord(native.SceneGradientKindLinear, native.SceneGradientTilePad, [8]float32{0, 0, 100, 100})
	rec.StopCount = uint32(len(stops))
	index, err := sc.InsertGradient(&rec, stops)
	if err != nil {
		t.Fatalf("InsertGradient: %v", err)
	}
	quad := solidQuadAt(0)
	quad.Background = native.SceneGradientTableBackground(index)
	group := effectRecord(true, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&group); err != nil {
		t.Fatalf("InsertEffect: %v", err)
	}
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	end := effectRecord(false, native.SceneEffectFlagHasOpacity, 0.5)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("end effect: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	kinds := commandKinds(t, sc)
	if len(kinds) != 2 {
		t.Fatalf("got %d marker commands, want 2 (begin + end)", len(kinds))
	}
	if kinds[0][1] != 1 {
		t.Fatalf("the table-gradient group must stay ISOLATED (filter_target %d, want 1)", kinds[0][1])
	}
}

// TestQuadTableGradientBackgroundValidates: the tag-4 background arm
// validates the index (finite, integral, under the table bound) and
// rejects payload in the other arms' words.
func TestQuadTableGradientBackgroundValidates(t *testing.T) {
	sc := nativeScene(t)
	quad := solidQuadAt(0)
	quad.Background = native.SceneGradientTableBackground(0)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("a table-gradient background with index 0 must validate: %v", err)
	}
	bad := solidQuadAt(0)
	bad.Background = native.SceneGradientTableBackground(300)
	if err := sc.InsertQuad(&bad); err == nil {
		t.Fatal("a table index over the table bound must be rejected")
	}
	fractional := solidQuadAt(0)
	fractional.Background = native.SceneGradientTableBackground(0)
	fractional.Background.Angle = f32bits(0.5)
	if err := sc.InsertQuad(&fractional); err == nil {
		t.Fatal("a fractional table index must be rejected")
	}
}

// solidQuadAt returns one opaque white quad at the given x (the shared
// helper shape of this file family).
func solidQuadAt(x float32) native.SceneQuadRecord {
	rec := native.NewSceneQuadRecord()
	rec.Bounds = native.SceneBoundsOf(x, 0, 100, 100)
	rec.ContentMask = native.SceneContentMaskOf(0, 0, 1000, 1000)
	rec.Background = native.SceneBackgroundOf(0, 0, 1, 1)
	return rec
}
