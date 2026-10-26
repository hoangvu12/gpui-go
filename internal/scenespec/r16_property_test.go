//go:build windows

// The browser-core ticket 51 (rev 16) scene ABI gates for
// scene_update_property (the design's appendix A.7): the finished
// scene's property-node records mutate in place (no re-sort, no
// re-plan), the old/new mapped device bounds come back for damage, and
// the stale ids / pre-finish calls / invalid values answer typed
// errors.
package scenespec

import (
	"errors"
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// propertyGroup builds one property-node effect group marker carrying
// the given flags and matrix (a matched start/end pair shares the
// snapshot).
func propertyGroup(isStart bool, flags uint32, matrix [6]float32, opacity float32, nodeID uint32) native.SceneEffectRecord {
	rec := effectRecord(isStart, flags, opacity)
	rec.Transform = [6]uint32{
		f32bits(matrix[0]), f32bits(matrix[1]), f32bits(matrix[2]),
		f32bits(matrix[3]), f32bits(matrix[4]), f32bits(matrix[5]),
	}
	rec.NodeID = nodeID
	return rec
}

// mappedBounds maps the record's bounds corners through the row-major
// 2x3 (the native map_bounds_corners mirror).
func mappedBounds(bounds native.SceneBounds, matrix [6]float32) (float32, float32, float32, float32) {
	bx, by, bw, bh := bounds.Rect()
	var minX, minY float32 = math.MaxFloat32, math.MaxFloat32
	var maxX, maxY float32 = -math.MaxFloat32, -math.MaxFloat32
	for _, corner := range [][2]float32{
		{bx, by}, {bx + bw, by}, {bx, by + bh}, {bx + bw, by + bh},
	} {
		dx := matrix[0]*corner[0] + matrix[1]*corner[1] + matrix[4]
		dy := matrix[2]*corner[0] + matrix[3]*corner[1] + matrix[5]
		minX = min(minX, dx)
		minY = min(minY, dy)
		maxX = max(maxX, dx)
		maxY = max(maxY, dy)
	}
	return minX, minY, maxX - minX, maxY - minY
}

// buildPropertyScene inserts one property-node transform group (node
// id 7, the identity matrix about the record bounds) around two quads
// and finishes it.
func buildPropertyScene(t *testing.T) *native.Scene {
	t.Helper()
	sc := nativeScene(t)
	const flags = native.SceneEffectFlagHasTransform | native.SceneEffectFlagPropertyNode
	start := propertyGroup(true, flags, [6]float32{1, 0, 0, 1, 0, 0}, 1, 7)
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	for _, x := range []float32{20, 40} {
		quad := solidQuad(x)
		if err := sc.InsertQuad(&quad); err != nil {
			t.Fatalf("insert quad: %v", err)
		}
	}
	end := propertyGroup(false, flags, [6]float32{1, 0, 0, 1, 0, 0}, 1, 7)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return sc
}

// TestUpdatePropertyMatrixMutatesRecordsAndReturnsBounds: the matrix
// update rewrites every record carrying the node id (both markers of
// the pair), returns the bounds mapped by the old and the new matrix,
// and leaves the compiled plan byte-identical (no re-sort, no
// re-plan).
func TestUpdatePropertyMatrixMutatesRecordsAndReturnsBounds(t *testing.T) {
	sc := buildPropertyScene(t)

	commandsBefore, err := sc.PlanCommands()
	if err != nil {
		t.Fatalf("PlanCommands: %v", err)
	}
	recordsBefore, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}

	oldMatrix := [6]float32{1, 0, 0, 1, 0, 0} // the identity
	newMatrix := [6]float32{0, -1, 1, 0, 300, 200}
	values := make([]uint32, 6)
	for i, value := range newMatrix {
		values[i] = f32bits(value)
	}
	oldBounds, newBounds, err := sc.UpdateProperty(7, native.ScenePropertyMatrix, values)
	if err != nil {
		t.Fatalf("UpdateProperty: %v", err)
	}

	// The records: both markers carry the new matrix.
	recordsAfter, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	if len(recordsAfter) != len(recordsBefore) {
		t.Fatalf("record count changed: %d -> %d", len(recordsBefore), len(recordsAfter))
	}
	for i, record := range recordsAfter {
		if record.NodeID != 7 {
			continue
		}
		for j, bits := range record.Transform {
			if bits != values[j] {
				t.Fatalf("record %d: transform word %d = %#x, want the updated %#x", i, j, bits, values[j])
			}
		}
		if record.Order != recordsBefore[i].Order {
			t.Fatalf("record %d: order changed (%d -> %d) - the update must not re-sort", i, recordsBefore[i].Order, record.Order)
		}
	}

	// The bounds: the record bounds (20, 20, 100, 100) mapped by the
	// old and the new matrix.
	for _, want := range []struct {
		name   string
		got    native.SceneBounds
		matrix [6]float32
	}{
		{"old", oldBounds, oldMatrix},
		{"new", newBounds, newMatrix},
	} {
		wx, wy, ww, wh := mappedBounds(recordsBefore[0].Bounds, want.matrix)
		gx, gy, gw, gh := want.got.Rect()
		if gx != wx || gy != wy || gw != ww || gh != wh {
			t.Fatalf("%s bounds = (%v,%v,%v,%v), want the mapped (%v,%v,%v,%v)", want.name, gx, gy, gw, gh, wx, wy, ww, wh)
		}
	}

	// The plan: byte-identical (no re-sort, no re-plan).
	commandsAfter, err := sc.PlanCommands()
	if err != nil {
		t.Fatalf("PlanCommands: %v", err)
	}
	if len(commandsAfter) != len(commandsBefore) {
		t.Fatalf("command count changed: %d -> %d", len(commandsBefore), len(commandsAfter))
	}
	for i := range commandsBefore {
		if commandsBefore[i] != commandsAfter[i] {
			t.Fatalf("command %d changed by the update: %+v -> %+v", i, commandsBefore[i], commandsAfter[i])
		}
	}
}

// TestUpdatePropertyOpacityMutatesRecords: the opacity update rewrites
// the records' opacity; the opacity never moves the group, so the old
// and new bounds are the (transform-mapped) record bounds, equal.
func TestUpdatePropertyOpacityMutatesRecords(t *testing.T) {
	sc := nativeScene(t)
	const flags = native.SceneEffectFlagHasOpacity | native.SceneEffectFlagPropertyNode
	start := propertyGroup(true, flags, [6]float32{1, 0, 0, 1, 0, 0}, 0.75, 9)
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	quad := solidQuad(20)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("insert quad: %v", err)
	}
	quad2 := solidQuad(40)
	if err := sc.InsertQuad(&quad2); err != nil {
		t.Fatalf("insert quad 2: %v", err)
	}
	end := propertyGroup(false, flags, [6]float32{1, 0, 0, 1, 0, 0}, 0.75, 9)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	oldBounds, newBounds, err := sc.UpdateProperty(9, native.ScenePropertyOpacity, []uint32{f32bits(0.25)})
	if err != nil {
		t.Fatalf("UpdateProperty: %v", err)
	}
	if oldBounds != newBounds {
		ox, oy, ow, oh := oldBounds.Rect()
		nx, ny, nw, nh := newBounds.Rect()
		t.Fatalf("opacity update bounds differ: (%v,%v,%v,%v) vs (%v,%v,%v,%v) - the opacity never moves the group", ox, oy, ow, oh, nx, ny, nw, nh)
	}
	// The opacity-only node maps through the identity: the raw record
	// bounds.
	records, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	wantX, wantY, wantW, wantH := records[0].Bounds.Rect()
	gotX, gotY, gotW, gotH := newBounds.Rect()
	if gotX != wantX || gotY != wantY || gotW != wantW || gotH != wantH {
		t.Fatalf("bounds = (%v,%v,%v,%v), want the record bounds (%v,%v,%v,%v)", gotX, gotY, gotW, gotH, wantX, wantY, wantW, wantH)
	}
	for i, record := range records {
		if record.NodeID != 9 {
			continue
		}
		if record.Opacity != f32bits(0.25) {
			t.Fatalf("record %d: opacity = %v, want 0.25", i, math.Float32frombits(record.Opacity))
		}
	}
}

// TestUpdatePropertyRejectsTheStaleNode: an id no record carries is a
// typed error, and the scene is untouched.
func TestUpdatePropertyRejectsTheStaleNode(t *testing.T) {
	sc := buildPropertyScene(t)
	recordsBefore, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	_, _, err = sc.UpdateProperty(1234, native.ScenePropertyOpacity, []uint32{f32bits(0.5)})
	if err == nil {
		t.Fatalf("a stale node id updated without error")
	}
	if !errors.Is(err, native.ErrSceneBadValue) {
		t.Fatalf("stale id error = %v, want ErrSceneBadValue", err)
	}
	recordsAfter, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	for i := range recordsBefore {
		if recordsBefore[i] != recordsAfter[i] {
			t.Fatalf("the stale update mutated record %d", i)
		}
	}
}

// TestUpdatePropertyRequiresFinish: before finish the update answers
// the not-finished error.
func TestUpdatePropertyRequiresFinish(t *testing.T) {
	sc := nativeScene(t)
	const flags = native.SceneEffectFlagHasOpacity | native.SceneEffectFlagPropertyNode
	start := propertyGroup(true, flags, [6]float32{1, 0, 0, 1, 0, 0}, 1, 3)
	if err := sc.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	quad := solidQuad(20)
	if err := sc.InsertQuad(&quad); err != nil {
		t.Fatalf("insert quad: %v", err)
	}
	end := propertyGroup(false, flags, [6]float32{1, 0, 0, 1, 0, 0}, 1, 3)
	if err := sc.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	_, _, err := sc.UpdateProperty(3, native.ScenePropertyOpacity, []uint32{f32bits(0.5)})
	if err == nil {
		t.Fatalf("the pre-finish update succeeded")
	}
	var status *native.SceneStatusError
	if !errors.As(err, &status) || !errors.Is(err, native.ErrSceneNotFinished) {
		t.Fatalf("pre-finish error = %v, want ErrSceneNotFinished", err)
	}
}

// TestUpdatePropertyValidatesValues: singular matrices, non-finite
// values, out-of-range opacity, wrong value counts, unknown kinds and
// records without the required flag are all typed errors that leave
// the scene untouched.
func TestUpdatePropertyValidatesValues(t *testing.T) {
	sc := buildPropertyScene(t)
	recordsBefore, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	matrixWords := func(matrix [6]float32) []uint32 {
		out := make([]uint32, 6)
		for i, value := range matrix {
			out[i] = f32bits(value)
		}
		return out
	}
	cases := []struct {
		name   string
		kind   uint32
		values []uint32
	}{
		{"singular matrix", native.ScenePropertyMatrix, matrixWords([6]float32{1, 0, 0, 0, 0, 0})},
		{"non-finite matrix", native.ScenePropertyMatrix, matrixWords([6]float32{float32(math.Inf(1)), 0, 0, 1, 0, 0})},
		{"opacity out of range", native.ScenePropertyOpacity, []uint32{f32bits(1.5)}},
		{"negative opacity", native.ScenePropertyOpacity, []uint32{f32bits(-0.25)}},
		{"wrong count for matrix", native.ScenePropertyMatrix, []uint32{1, 2, 3}},
		{"wrong count for opacity", native.ScenePropertyOpacity, []uint32{1, 2}},
		{"unknown kind", 99, []uint32{1}},
		{"zero node id", native.ScenePropertyOpacity, []uint32{f32bits(0.5)}},
	}
	for _, testCase := range cases {
		nodeID := uint32(7)
		if testCase.name == "zero node id" {
			nodeID = 0
		}
		_, _, err := sc.UpdateProperty(nodeID, testCase.kind, testCase.values)
		if err == nil {
			t.Fatalf("%s: updated without error", testCase.name)
		}
		if !errors.Is(err, native.ErrSceneBadValue) {
			t.Fatalf("%s: error = %v, want ErrSceneBadValue", testCase.name, err)
		}
	}
	// A matrix update on an opacity-only node requires the transform
	// flag: build one and try.
	sc2 := nativeScene(t)
	const flags = native.SceneEffectFlagHasOpacity | native.SceneEffectFlagPropertyNode
	start := propertyGroup(true, flags, [6]float32{1, 0, 0, 1, 0, 0}, 1, 5)
	if err := sc2.InsertEffect(&start); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	quad := solidQuad(20)
	if err := sc2.InsertQuad(&quad); err != nil {
		t.Fatalf("insert quad: %v", err)
	}
	quad2 := solidQuad(40)
	if err := sc2.InsertQuad(&quad2); err != nil {
		t.Fatalf("insert quad 2: %v", err)
	}
	end := propertyGroup(false, flags, [6]float32{1, 0, 0, 1, 0, 0}, 1, 5)
	if err := sc2.InsertEffect(&end); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if err := sc2.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, _, err := sc2.UpdateProperty(5, native.ScenePropertyMatrix, matrixWords([6]float32{1, 0, 0, 1, 10, 10})); err == nil {
		t.Fatalf("a matrix update on an opacity-only node succeeded")
	} else if !errors.Is(err, native.ErrSceneBadValue) {
		t.Fatalf("missing-flag error = %v, want ErrSceneBadValue", err)
	}

	// The scene is untouched by every rejection.
	recordsAfter, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	for i := range recordsBefore {
		if recordsBefore[i] != recordsAfter[i] {
			t.Fatalf("a rejected update mutated record %d", i)
		}
	}
}

// TestUpdatePropertyDrivesTheNextRender: the mutated record reaches the
// renderer's snapshot - a scene rendered once, updated, and rendered
// again draws the new matrix (the rendererspec package gates the
// pixels; here the dump proves the records the snapshot copies).
func TestUpdatePropertyDrivesTheNextRender(t *testing.T) {
	sc := buildPropertyScene(t)
	newMatrix := [6]float32{1.5, 0, 0, 1.5, 40, 30}
	values := make([]uint32, 6)
	for i, value := range newMatrix {
		values[i] = f32bits(value)
	}
	if _, _, err := sc.UpdateProperty(7, native.ScenePropertyMatrix, values); err != nil {
		t.Fatalf("UpdateProperty: %v", err)
	}
	records, err := sc.EffectDump()
	if err != nil {
		t.Fatalf("EffectDump: %v", err)
	}
	found := false
	for _, record := range records {
		if record.NodeID != 7 {
			continue
		}
		found = true
		if record.Transform != [6]uint32(values) {
			t.Fatalf("the dumped transform = %v, want %v", record.Transform, values)
		}
	}
	if !found {
		t.Fatalf("no record carries the node id after the update")
	}
}
