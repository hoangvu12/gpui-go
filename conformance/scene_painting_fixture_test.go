package conformance

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// scenePaintingFixturePath is the fx-0003 fixture envelope, and
// scenePaintingRecordedTracePath is the reference trace recorded from it
// by conformance/cmd/recordreference.
const (
	scenePaintingFixturePath       = "fixtures/fx-0003-scene-painting.json"
	scenePaintingRecordedTracePath = "recorded/fx-0003-scene-painting/trace.json"
)

// wantScenePaintingCaseLabels lists the six fx-0003 cases in execution
// order.
var wantScenePaintingCaseLabels = []string{
	"styled-boxes-opacity",
	"deferred-overlay-floor",
	"nested-filter-groups",
	"replay-range",
	"clip-cull-and-splits",
	"order-reuse-ties",
}

// TestScenePaintingEnvelope checks the shipped fx-0003 fixture against
// the scene-painting wire format: fixture kind detection, six cases with
// the expected labels, and the paint-script shapes the recorded trace
// depends on (box/shadow/underline paint commands, layers with opacity,
// the deferred order floor, filter groups with blur radii, backdrops and
// the replay range).
func TestScenePaintingEnvelope(t *testing.T) {
	env, err := LoadEnvelope(scenePaintingFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", scenePaintingFixturePath, err)
	}
	if env.FixtureID != "fx-0003-scene-painting" {
		t.Errorf("fixture_id = %q, want %q", env.FixtureID, "fx-0003-scene-painting")
	}
	if env.FixtureKind != FixtureKindScenePainting {
		t.Fatalf("fixture_kind = %q, want %q", env.FixtureKind, FixtureKindScenePainting)
	}
	if !env.IsScenePaintingKind() {
		t.Errorf("IsScenePaintingKind() = false, want true")
	}
	if env.Profile != "test" {
		t.Errorf("profile = %q, want %q", env.Profile, "test")
	}
	if want := []string{"headless", "test-platform"}; len(env.Environment) != 2 || env.Environment[0] != want[0] || env.Environment[1] != want[1] {
		t.Errorf("environment = %q, want %q", env.Environment, want)
	}

	if env.Inputs.SceneCases == nil {
		t.Fatalf("inputs.scene_cases is nil")
	}
	cases := env.Inputs.SceneCases.Cases
	if len(cases) != 6 {
		t.Fatalf("scene_cases has %d cases, want 6", len(cases))
	}
	for i, want := range wantScenePaintingCaseLabels {
		if got := cases[i].Label; got != want {
			t.Errorf("case %d label = %q, want %q", i, got, want)
		}
	}

	// Every case has a style tree and a definite available space.
	for i, c := range cases {
		if c.StyleTree == nil {
			t.Fatalf("case %d (%q) has no style_tree", i, c.Label)
		}
		if c.StyleTree.Label == "" {
			t.Errorf("case %q style tree root has empty label", c.Label)
		}
		if c.AvailableSpace.Width <= 0 || c.AvailableSpace.Height <= 0 {
			t.Errorf("case %q available_space = %+v, want positive definite space", c.Label, c.AvailableSpace)
		}
	}

	// The script shapes: layers with opacity, a raised floor, filter
	// groups, backdrops, an inset shadow, a border-only split, explicit
	// masks and a replay range.
	var paintBoxes, paintShadows, paintUnderlines, beginLayers, endLayers, raiseFloors int
	var beginGroups, endGroups, backdrops, replays int
	var opacityLayers, insetShadows, borderOnly, explicitMasks int
	for _, c := range cases {
		for _, op := range c.PaintScript {
			switch op.Op {
			case OpPaintBox:
				paintBoxes++
				if op.Background == "" && (op.BorderWidth != nil || op.BorderWidths != nil) {
					borderOnly++
				}
				if op.Mask != nil {
					explicitMasks++
				}
			case OpPaintShadow:
				paintShadows++
				if op.Inset != nil && *op.Inset {
					insetShadows++
				}
			case OpPaintUnderline:
				paintUnderlines++
			case OpBeginLayer:
				beginLayers++
				if op.Opacity != nil {
					opacityLayers++
				}
			case OpEndLayer:
				endLayers++
			case OpRaiseFloor:
				raiseFloors++
			case OpBeginFilterGroup:
				beginGroups++
			case OpEndFilterGroup:
				endGroups++
			case OpPaintBackdrop:
				backdrops++
			case OpReplay:
				replays++
			default:
				t.Errorf("case %q has unknown paint op %q", c.Label, op.Op)
			}
		}
	}
	if paintBoxes < 6 || paintShadows < 2 || paintUnderlines < 3 {
		t.Errorf("paint command counts = boxes %d, shadows %d, underlines %d; want at least 6/2/3", paintBoxes, paintShadows, paintUnderlines)
	}
	if beginLayers != endLayers {
		t.Errorf("layer begin/end counts = %d/%d, want equal", beginLayers, endLayers)
	}
	if beginGroups != endGroups {
		t.Errorf("filter group begin/end counts = %d/%d, want equal", beginGroups, endGroups)
	}
	if raiseFloors == 0 || opacityLayers < 2 || insetShadows == 0 || borderOnly == 0 || explicitMasks == 0 || replays == 0 || backdrops == 0 {
		t.Errorf("missing script coverage: floors=%d opacityLayers=%d insetShadows=%d borderOnly=%d masks=%d replays=%d backdrops=%d",
			raiseFloors, opacityLayers, insetShadows, borderOnly, explicitMasks, replays, backdrops)
	}

	// The replay references the first case's scene.
	var replaySource string
	for _, c := range cases {
		for _, op := range c.PaintScript {
			if op.Op == OpReplay {
				replaySource = op.Source
			}
		}
	}
	if replaySource != "styled-boxes-opacity" {
		t.Errorf("replay source = %q, want styled-boxes-opacity (an earlier case)", replaySource)
	}

	// The style trees use the extended layout-metrics key set.
	if got := cases[0].StyleTree.Style["flexDirection"]; got != "Column" {
		t.Errorf("styled-boxes root flexDirection = %q, want Column", got)
	}
	if got := cases[5].StyleTree.Style["flexDirection"]; got != "Row" {
		t.Errorf("order-reuse-ties root flexDirection = %q, want Row", got)
	}
}

// TestScenePaintingPaintOpRoundTrip checks that the paint ops of the
// fx-0003 fixture marshal back into the exact Rust wire shape (op tag
// plus the variant's fields, optional fields only when set), the same
// way the fx-0001/fx-0002 round-trip tests do for their input types.
func TestScenePaintingPaintOpRoundTrip(t *testing.T) {
	env, err := LoadEnvelope(scenePaintingFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope: %v", err)
	}
	for _, c := range env.Inputs.SceneCases.Cases {
		for _, op := range c.PaintScript {
			data, err := json.Marshal(op)
			if err != nil {
				t.Fatalf("marshaling op %s: %v", op.Op, err)
			}
			var again PaintOp
			if err := json.Unmarshal(data, &again); err != nil {
				t.Fatalf("unmarshaling op %s: %v", op.Op, err)
			}
			remarshal, err := json.Marshal(again)
			if err != nil {
				t.Fatalf("re-marshaling op %s: %v", op.Op, err)
			}
			if string(data) != string(remarshal) {
				t.Errorf("op %s does not round-trip:\ngot  %s\nwant %s", op.Op, remarshal, data)
			}
		}
	}
}

// TestScenePaintingPaintOpValidation checks that malformed paint ops are
// rejected during unmarshaling the way the Rust enum deserializer would:
// unknown tags, missing required fields.
func TestScenePaintingPaintOpValidation(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"missing tag", `{"label": "b1"}`, "missing \"op\" tag"},
		{"unknown tag", `{"op": "paint-circle", "label": "b1"}`, "unknown op tag"},
		{"box without label", `{"op": "paint-box"}`, "missing required field \"label\""},
		{"shadow without color", `{"op": "paint-shadow", "label": "b1"}`, "missing required field \"color\""},
		{"underline without thickness", `{"op": "paint-underline", "label": "b1", "color": "000000ff"}`, "missing required field \"thickness\""},
		{"group without blur", `{"op": "begin-filter-group", "label": "g1"}`, "missing required field \"blur_radius\""},
		{"replay without source", `{"op": "replay", "start": 0, "end": 2}`, "missing required field \"source\""},
	}
	for _, tc := range cases {
		var op PaintOp
		err := json.Unmarshal([]byte(tc.json), &op)
		if err == nil {
			t.Errorf("%s: unmarshal unexpectedly succeeded", tc.name)
			continue
		}
		if want := tc.want; !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q, want it to contain %q", tc.name, err.Error(), want)
		}
	}
}

// TestScenePaintingRecordedTrace checks the recorded reference trace for
// fx-0003: identity, envelope hash agreement, the per-case event shape
// (case-begin/case-end for all six cases, layout-bounds for every node,
// the scene-command/scene-op/scene-meta event stream) and exact
// self-comparison.
func TestScenePaintingRecordedTrace(t *testing.T) {
	raw, err := os.ReadFile(scenePaintingRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	var trace Trace
	if err := json.Unmarshal(raw, &trace); err != nil {
		t.Fatalf("parsing recorded trace: %v", err)
	}

	if trace.Schema != TraceSchema {
		t.Fatalf("schema = %q, want %q", trace.Schema, TraceSchema)
	}
	if trace.FixtureID != "fx-0003-scene-painting" {
		t.Fatalf("fixture id = %q", trace.FixtureID)
	}
	if trace.FixtureKind != FixtureKindScenePainting {
		t.Fatalf("fixture kind = %q, want %q", trace.FixtureKind, FixtureKindScenePainting)
	}
	if trace.Harness.GpuiCommit != "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a" {
		t.Fatalf("gpui commit = %q", trace.Harness.GpuiCommit)
	}

	// The recorded envelope hash must match the fixture file in the repo.
	envHash, err := SHA256Envelope(scenePaintingFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	if trace.EnvelopeSHA256 != envHash {
		t.Fatalf("recorded trace envelope hash %s does not match current envelope %s; the fixture changed after the recording", trace.EnvelopeSHA256, envHash)
	}

	// Event shape per case: case-begin (label, remSize, scale f32 fields),
	// layout-bounds for every node, the scene event stream, scene-meta,
	// case-end. Sequences are dense and ordered.
	caseBeginOrder := []string{}
	caseEnds, bounds, commands, ops, metas := 0, 0, 0, 0, 0
	opKinds := map[string]int{}
	commandKinds := map[string]int{}
	for i, event := range trace.Events {
		if want := uint64(i + 1); event.Seq != want {
			t.Fatalf("event %d seq = %d, want %d (dense ordered seq)", i, event.Seq, want)
		}
		switch event.Name {
		case "case-begin":
			caseBeginOrder = append(caseBeginOrder, event.Label)
			for _, field := range []string{"label", "remSize", "scale"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("case-begin %q missing field %q", event.Label, field)
				}
			}
			if got, _ := event.Fields["scale"].(string); got != F32Bits(2.0) {
				t.Errorf("case-begin %q scale = %q, want f32:40000000 (2.0, the reference test window scale)", event.Label, got)
			}
		case "case-end":
			caseEnds++
		case "layout-bounds":
			bounds++
			for _, field := range []string{"x", "y", "width", "height"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("layout-bounds %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
				}
			}
		case "scene-command":
			commands++
			command, _ := event.Fields["command"].(string)
			commandKinds[command]++
			switch command {
			case "batch":
				if _, ok := event.Fields["batchKind"]; !ok {
					t.Fatalf("batch scene-command missing batchKind")
				}
			case "begin-filter", "end-filter":
				if _, ok := event.Fields["boundaryIndex"]; !ok {
					t.Fatalf("%s scene-command missing boundaryIndex", command)
				}
				if _, ok := event.Fields["target"]; !ok {
					t.Fatalf("%s scene-command missing target", command)
				}
			default:
				t.Fatalf("unexpected scene-command command %q at seq %d", command, event.Seq)
			}
		case "scene-op":
			ops++
			kind, _ := event.Fields["kind"].(string)
			opKinds[kind]++
			for _, field := range []string{"order", "batchIndex", "boundsX", "boundsY", "boundsW", "boundsH", "maskX", "maskY", "maskW", "maskH"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("scene-op %q missing field %q", kind, field)
				}
			}
			for _, field := range []string{"boundsX", "boundsY", "boundsW", "boundsH", "maskX", "maskY", "maskW", "maskH"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("scene-op %q field %q is not a valid f32 bit pattern: %v", kind, field, err)
				}
			}
		case "scene-meta":
			metas++
			for _, field := range []string{"opCount", "layerCount", "quadCount", "shadowCount", "underlineCount", "backdropCount", "boundaryCount", "surfaceCount", "commandCount", "instanceBatchCount", "usesOffscreenTarget"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("scene-meta missing field %q", field)
				}
			}
		default:
			t.Fatalf("unexpected event name %q at seq %d", event.Name, event.Seq)
		}
	}

	// All six cases begin and end, in fixture order.
	if len(caseBeginOrder) != 6 {
		t.Errorf("case-begin count = %d, want 6", len(caseBeginOrder))
	}
	for i, want := range wantScenePaintingCaseLabels {
		if i < len(caseBeginOrder) && caseBeginOrder[i] != want {
			t.Errorf("case-begin %d = %q, want %q", i, caseBeginOrder[i], want)
		}
	}
	if caseEnds != 6 {
		t.Errorf("case-end count = %d, want 6", caseEnds)
	}

	// Every node of every case reports bounds: 25 nodes across the six
	// cases (4+4+4+4+4+5).
	if bounds != 25 {
		t.Errorf("layout-bounds count = %d, want 25", bounds)
	}
	if metas != 6 {
		t.Errorf("scene-meta count = %d, want 6", metas)
	}
	if commands == 0 || ops == 0 {
		t.Fatalf("scene-command/scene-op counts = %d/%d, want non-zero", commands, ops)
	}

	// The trace carries every primitive class the fixture paints
	// (surfaces are absent: the reference's insert_surface is
	// crate-private, so the paired-opacity path is validated by the
	// kernel tests instead).
	for _, kind := range []string{"quad", "shadow", "underline", "backdrop-filter", "filter-boundary"} {
		if opKinds[kind] == 0 {
			t.Errorf("no scene-op events of kind %q", kind)
		}
	}
	if opKinds["surface"] != 0 {
		t.Errorf("scene-op surface count = %d, want 0 in this fixture", opKinds["surface"])
	}

	// The plan carries batch, begin-filter and end-filter commands.
	for _, command := range []string{"batch", "begin-filter", "end-filter"} {
		if commandKinds[command] == 0 {
			t.Errorf("no scene-command events of command %q", command)
		}
	}

	// Exact self-comparison must hold.
	if result := CompareTraces(&trace, &trace); !result.Equal {
		t.Fatalf("self comparison failed: %s", result.Diff)
	}
}
