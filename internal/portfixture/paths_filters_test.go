package portfixture

import (
	"runtime"
	"testing"

	"gpui-go/conformance"
)

const (
	// pathsFiltersFixturePath is the fx-0007 envelope this gate
	// executes.
	pathsFiltersFixturePath = "../../conformance/fixtures/fx-0007-paths-filters.json"
	// pathsFiltersRecordedTracePath is the oracle recorded from the
	// pinned reference harness.
	pathsFiltersRecordedTracePath = "../../conformance/recorded/fx-0007-paths-filters/trace.json"
)

// TestFx0007PathsFiltersMatchesRecordedReference is the ticket16 gate
// for the paths-filters fixture: the port runner must reproduce the
// FULL recorded reference trace (all 428 events: every case-begin with
// the test-profile scale, every layout-bounds event, every
// scene-command with its batch structure — path vertex and sprite
// counts, sprite texture indices, the filter-target plan with the
// depth-3 inline group — every scene-op with the exact f32 bit
// patterns of the bounds, masks, colors, radii and tiles, every
// path-vertex with the exact tessellation output of the pinned
// PathBuilder, and every scene-meta with the totals and requirements)
// through the real gpui scene kernel (the native scene service behind
// internal/native, including the native pinned PathBuilder
// tessellation) driven by the port's paint API.
func TestFx0007PathsFiltersMatchesRecordedReference(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native scene artifact is Windows AMD64; the paths-filters fixture cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(pathsFiltersFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunPathsFilters(envelope)
	if err != nil {
		t.Fatalf("running port paths-filters fixture: %v", err)
	}

	// Run metadata: pin the envelope bytes this run executed.
	sha, err := conformance.SHA256Envelope(pathsFiltersFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha

	reference, err := conformance.LoadTrace(pathsFiltersRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}

	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("paths-filters trace mismatch:\n%s", result.Diff)
	}
	if result.Diff != "" {
		t.Fatalf("equal comparison must report an empty diff, got %q", result.Diff)
	}
	if got, want := len(trace.Events), len(reference.Events); got != want {
		t.Errorf("event count = %d, want %d", got, want)
	}
}

// TestFx0007PathsFiltersBehavioralFacts pins the observable
// paths-filters behaviors directly from the port run, independent of
// the file-based comparison: four cases, path tessellation vertex
// counts, the depth-3 filter-target plan (two isolated targets then
// inline), sprite batch structure with texture splits, and the paired
// surface opacity.
func TestFx0007PathsFiltersBehavioralFacts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native scene artifact is Windows AMD64; the paths-filters fixture cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(pathsFiltersFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunPathsFilters(envelope)
	if err != nil {
		t.Fatalf("running port paths-filters fixture: %v", err)
	}

	var caseBegins, commands, ops, vertices, metas int
	kinds := map[string]int{}
	pathVertexCounts := map[string]uint64{}
	lastMeta := map[string]map[string]uint64{}
	inCase := ""
	for _, event := range trace.Events {
		switch event.Name {
		case "case-begin":
			caseBegins++
			inCase = event.Label
		case "case-end":
			inCase = ""
		case "scene-command":
			commands++
			command, _ := event.Fields["command"].(string)
			if command == "" {
				t.Errorf("scene-command missing the command field")
			}
		case "scene-op":
			ops++
			kind, _ := event.Fields["kind"].(string)
			kinds[kind]++
			if kind == "path" {
				pathVertexCounts[inCase] += fieldUint(event.Fields["vertexCount"])
			}
		case "path-vertex":
			vertices++
		case "scene-meta":
			metas++
			meta := map[string]uint64{}
			for _, key := range []string{"pathCount", "commandCount", "instanceBatchCount", "pathRasterizationVertexCount", "pathSpriteCount", "usesPathTarget", "surfaceCount", "monochromeSpriteCount", "subpixelSpriteCount", "polychromeSpriteCount", "boundaryCount", "backdropCount", "isolatedTargetCount", "usesOffscreenTarget"} {
				meta[key] = fieldUint(event.Fields[key])
			}
			lastMeta[inCase] = meta
		}
	}
	if caseBegins != 4 {
		t.Errorf("case-begin count = %d, want 4", caseBegins)
	}
	if metas != 4 {
		t.Errorf("scene-meta count = %d, want 4", metas)
	}
	if commands == 0 || ops == 0 || vertices == 0 {
		t.Fatalf("scene-command/scene-op/path-vertex counts = %d/%d/%d, want non-zero", commands, ops, vertices)
	}
	for _, kind := range []string{"path", "monochrome-sprite", "subpixel-sprite", "polychrome-sprite", "surface", "backdrop-filter", "filter-boundary", "quad"} {
		if kinds[kind] == 0 {
			t.Errorf("no scene-op events of kind %q", kind)
		}
	}

	// Case 1: six paths (two scripted paths drop — the empty
	// tessellation and the fully-clipped one), every vertex bit-exact
	// from the pinned tessellator.
	if got := lastMeta["paths-fill-stroke"]["pathCount"]; got != 6 {
		t.Errorf("paths-fill-stroke pathCount = %d, want 6 (two scripted paths drop)", got)
	}
	if got := lastMeta["paths-fill-stroke"]["usesPathTarget"]; got != 1 {
		t.Errorf("paths-fill-stroke usesPathTarget = %d, want 1", got)
	}

	// Case 2: the nested filter plan — two isolated targets, the third
	// group inline (MAX_FILTER_GROUP_DEPTH = 2), one backdrop, one
	// surface.
	if got := lastMeta["nested-filters-depth3"]["isolatedTargetCount"]; got != 2 {
		t.Errorf("nested-filters-depth3 isolatedTargetCount = %d, want 2 (depth-3 group renders inline)", got)
	}
	if got := lastMeta["nested-filters-depth3"]["boundaryCount"]; got != 6 {
		t.Errorf("nested-filters-depth3 boundaryCount = %d, want 6", got)
	}
	if got := lastMeta["nested-filters-depth3"]["usesOffscreenTarget"]; got != 1 {
		t.Errorf("nested-filters-depth3 usesOffscreenTarget = %d, want 1", got)
	}

	// Case 3: sprite batch structure — the two monochrome sprites split
	// on the texture change.
	if got := lastMeta["sprites-surfaces-order"]["monochromeSpriteCount"]; got != 2 {
		t.Errorf("sprites-surfaces-order monochromeSpriteCount = %d, want 2", got)
	}
	if got := lastMeta["sprites-surfaces-order"]["surfaceCount"]; got != 1 {
		t.Errorf("sprites-surfaces-order surfaceCount = %d, want 1", got)
	}

	// Case 4: replayed paths keep their vertex payloads (the replay
	// address space carries the vertex arrays).
	if got := lastMeta["replay-paths-filters"]["pathCount"]; got != 4 {
		t.Errorf("replay-paths-filters pathCount = %d, want 4 (three replayed + one arc stroke)", got)
	}
	if got := pathVertexCounts["replay-paths-filters"]; got != lastMeta["replay-paths-filters"]["pathRasterizationVertexCount"] {
		t.Errorf("replay-paths-filters summed path vertexCount = %d, want %d (the meta's rasterization vertex count)", got, lastMeta["replay-paths-filters"]["pathRasterizationVertexCount"])
	}
}
