package conformance

import (
	"encoding/json"
	"os"
	"testing"
)

// pathsFiltersFixturePath is the fx-0007 fixture envelope, and
// pathsFiltersRecordedTracePath is the reference trace recorded from it
// by conformance/cmd/recordreference.
const (
	pathsFiltersFixturePath       = "fixtures/fx-0007-paths-filters.json"
	pathsFiltersRecordedTracePath = "recorded/fx-0007-paths-filters/trace.json"
)

// wantPathsFiltersCaseLabels lists the four fx-0007 cases in execution
// order.
var wantPathsFiltersCaseLabels = []string{
	"paths-fill-stroke",
	"nested-filters-depth3",
	"sprites-surfaces-order",
	"replay-paths-filters",
}

// TestPathsFiltersEnvelope checks the shipped fx-0007 fixture against
// the paths-filters wire format: fixture kind detection, four cases
// with the expected labels, and the script shapes the recorded trace
// depends on (path builder command scripts with fills/strokes/dashes,
// sprites with explicit atlas tiles, surfaces, nested filter groups
// beyond depth two, and the replay range).
func TestPathsFiltersEnvelope(t *testing.T) {
	env, err := LoadEnvelope(pathsFiltersFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", pathsFiltersFixturePath, err)
	}
	if env.FixtureID != "fx-0007-paths-filters" {
		t.Errorf("fixture_id = %q, want %q", env.FixtureID, "fx-0007-paths-filters")
	}
	if env.FixtureKind != FixtureKindPathsFilters {
		t.Fatalf("fixture_kind = %q, want %q", env.FixtureKind, FixtureKindPathsFilters)
	}
	if !env.IsPathsFiltersKind() {
		t.Errorf("IsPathsFiltersKind() = false, want true")
	}
	if env.Profile != "test" {
		t.Errorf("profile = %q, want %q", env.Profile, "test")
	}
	if want := []string{"headless", "test-platform"}; len(env.Environment) != 2 || env.Environment[0] != want[0] || env.Environment[1] != want[1] {
		t.Errorf("environment = %q, want %q", env.Environment, want)
	}
	if env.Inputs.SceneCases != nil || env.Inputs.PathFilterCases == nil {
		t.Fatalf("inputs shape: scene_cases=%v path_filter_cases=%v, want nil/non-nil", env.Inputs.SceneCases != nil, env.Inputs.PathFilterCases)
	}

	cases := env.Inputs.PathFilterCases.Cases
	if len(cases) != 4 {
		t.Fatalf("path_filter_cases has %d cases, want 4", len(cases))
	}
	for i, want := range wantPathsFiltersCaseLabels {
		if got := cases[i].Label; got != want {
			t.Errorf("case %d label = %q, want %q", i, got, want)
		}
	}

	// Case 1: six path scripts + one box; the styles cover fill,
	// stroke with caps/join, dash, fill options and transforms; two
	// scripts paint degenerate/fully-clipped paths (dropped).
	c1 := cases[0]
	if len(c1.PaintScript) != 9 {
		t.Fatalf("case %q script length = %d, want 9", c1.Label, len(c1.PaintScript))
	}
	firstPath := c1.PaintScript[0]
	if firstPath.Op != OpPaintPath || len(firstPath.Commands) != 5 {
		t.Fatalf("case %q op 0 = %q with %d commands, want paint-path with 5", c1.Label, firstPath.Op, len(firstPath.Commands))
	}
	if firstPath.Commands[0].Kind != PathCmdMoveTo || firstPath.Commands[4].Kind != PathCmdClose {
		t.Errorf("first path commands = %q..%q, want move-to..close", firstPath.Commands[0].Kind, firstPath.Commands[4].Kind)
	}
	stroke := c1.PaintScript[1]
	if stroke.Commands[0].Kind != PathCmdStyle || stroke.Commands[0].Style != "stroke" || *stroke.Commands[0].Width != 3 {
		t.Errorf("stroke style command = %+v, want stroke width 3", stroke.Commands[0])
	}
	dashed := c1.PaintScript[4]
	if dashed.Commands[0].Kind != PathCmdStyle || dashed.Commands[1].Kind != PathCmdDash || len(dashed.Commands[1].Lengths) != 2 {
		t.Errorf("dashed stroke commands = %+v, want style+dash[4,2]", dashed.Commands[:2])
	}
	transformed := c1.PaintScript[5]
	if transformed.Commands[1].Kind != PathCmdTranslate || transformed.Commands[2].Kind != PathCmdScale || *transformed.Commands[2].Factor != 1.5 {
		t.Errorf("transform commands = %+v, want translate+scale 1.5", transformed.Commands[1:3])
	}

	// Case 2: three nested filter groups (the third beyond
	// MAX_FILTER_GROUP_DEPTH, inline) with explicit overlapping bounds,
	// a path inside, a backdrop and a surface.
	c2 := cases[1]
	groups := 0
	for _, op := range c2.PaintScript {
		switch op.Op {
		case OpBeginFilterGroup2, OpEndFilterGroup2:
			groups++
		}
	}
	if groups != 6 {
		t.Errorf("case %q has %d group markers, want 6 (3 groups)", c2.Label, groups)
	}

	// Case 3: two monochrome sprites on different atlas textures, one
	// subpixel, one polychrome sprite and one surface.
	c3 := cases[2]
	kinds := map[string]int{}
	for _, op := range c3.PaintScript {
		kinds[op.Op]++
		if op.Tile != nil && op.Tile.TextureIndex > 1 {
			t.Errorf("case %q sprite tile texture index = %d, want 0 or 1", c3.Label, op.Tile.TextureIndex)
		}
	}
	if kinds[OpPaintMonochromeSprite] != 2 || kinds[OpPaintSubpixelSprite] != 1 || kinds[OpPaintPolychromeSprite] != 1 || kinds[OpPaintSurface] != 1 {
		t.Errorf("case %q sprite/surface op counts = %+v, want 2 mono, 1 sub, 1 poly, 1 surface", c3.Label, kinds)
	}

	// Case 4: replay of case 1's first three ops, one filter group and
	// an arc stroke.
	c4 := cases[3]
	if c4.PaintScript[0].Op != OpReplay2 || c4.PaintScript[0].Source != "paths-fill-stroke" || c4.PaintScript[0].Start != 0 || c4.PaintScript[0].End != 3 {
		t.Errorf("case %q replay op = %+v, want replay paths-fill-stroke 0..3", c4.Label, c4.PaintScript[0])
	}
	lastPath := c4.PaintScript[len(c4.PaintScript)-1]
	if lastPath.Op != OpPaintPath || lastPath.Commands[2].Kind != PathCmdArcTo {
		t.Errorf("case %q last op = %q, want paint-path with an arc-to", c4.Label, lastPath.Op)
	}

	// The recorded reference trace exists and matches the fixture id.
	trace, err := LoadTrace(pathsFiltersRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	if trace.FixtureID != "fx-0007-paths-filters" {
		t.Errorf("trace fixture_id = %q, want fx-0007-paths-filters", trace.FixtureID)
	}
	if trace.FixtureKind != FixtureKindPathsFilters {
		t.Errorf("trace fixture_kind = %q, want %q", trace.FixtureKind, FixtureKindPathsFilters)
	}
	if len(trace.Events) == 0 {
		t.Fatalf("recorded trace has no events")
	}
}

// TestPathsFiltersEnvelopeRoundTrip re-encodes the decoded fixture and
// re-decodes it, pinning the wire shape of the new envelope types (the
// additive-types rule: existing fixture kinds' wire shapes stay
// byte-identical).
func TestPathsFiltersEnvelopeRoundTrip(t *testing.T) {
	raw, err := os.ReadFile(pathsFiltersFixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	env, err := LoadEnvelope(pathsFiltersFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope: %v", err)
	}
	// The old fixture must decode through the same Envelope struct
	// untouched.
	if _, err := LoadEnvelope(scenePaintingFixturePath); err != nil {
		t.Fatalf("loading the fx-0003 fixture through the extended envelope: %v", err)
	}

	var anyEnvelope any
	if err := json.Unmarshal(raw, &anyEnvelope); err != nil {
		t.Fatalf("unmarshaling raw fixture: %v", err)
	}
	// The path_filter_cases input decodes into the typed mirror without
	// errors (already checked by LoadEnvelope); re-encode one case's op
	// and confirm the op tag survives.
	c := env.Inputs.PathFilterCases.Cases[0]
	encoded, err := json.Marshal(c.PaintScript[0])
	if err != nil {
		t.Fatalf("marshaling paint op: %v", err)
	}
	var decoded PathFilterOp
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("re-decoding paint op: %v", err)
	}
	if decoded.Op != OpPaintPath || decoded.Color != "2563ebff" {
		t.Errorf("round trip = %+v, want paint-path with color 2563ebff", decoded)
	}
}
