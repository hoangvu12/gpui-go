package conformance

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// authoringCounterFixturePath is the fx-0006 fixture envelope, and
// authoringCounterRecordedTracePath is the reference trace recorded
// from it by conformance/cmd/recordreference.
const (
	authoringCounterFixturePath       = "fixtures/fx-0006-authoring-counter.json"
	authoringCounterRecordedTracePath = "recorded/fx-0006-authoring-counter/trace.json"
)

// wantAuthoringCounterCaseLabels lists the fx-0006 cases in execution
// order.
var wantAuthoringCounterCaseLabels = []string{
	"shared-counter-two-windows",
}

// TestAuthoringCounterEnvelope checks the shipped fx-0006 fixture
// against the authoring-counter wire format: fixture kind detection,
// one case with two windows sharing the model, the counter style
// inputs the recorded trace depends on (padding, font size, justify,
// background), and the increment/close script with its wire tags.
func TestAuthoringCounterEnvelope(t *testing.T) {
	env, err := LoadEnvelope(authoringCounterFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", authoringCounterFixturePath, err)
	}
	if env.FixtureID != "fx-0006-authoring-counter" {
		t.Errorf("fixture_id = %q, want %q", env.FixtureID, "fx-0006-authoring-counter")
	}
	if env.FixtureKind != FixtureKindAuthoringCounter {
		t.Fatalf("fixture_kind = %q, want %q", env.FixtureKind, FixtureKindAuthoringCounter)
	}
	if !env.IsAuthoringCounterKind() {
		t.Errorf("IsAuthoringCounterKind() = false, want true")
	}
	if env.Profile != "test" {
		t.Errorf("profile = %q, want %q", env.Profile, "test")
	}
	if want := []string{"headless", "test-platform"}; len(env.Environment) != 2 || env.Environment[0] != want[0] || env.Environment[1] != want[1] {
		t.Errorf("environment = %q, want %q", env.Environment, want)
	}

	if env.Inputs.CounterCases == nil {
		t.Fatalf("inputs.counter_cases is nil")
	}
	cases := env.Inputs.CounterCases.Cases
	if len(cases) != len(wantAuthoringCounterCaseLabels) {
		t.Fatalf("counter_cases has %d cases, want %d", len(cases), len(wantAuthoringCounterCaseLabels))
	}
	for i, want := range wantAuthoringCounterCaseLabels {
		if got := cases[i].Label; got != want {
			t.Errorf("case %d label = %q, want %q", i, got, want)
		}
	}

	c := cases[0]
	if c.InitialValue != 8 {
		t.Errorf("initial_value = %d, want 8", c.InitialValue)
	}
	if len(c.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(c.Windows))
	}
	if c.Windows[0].Label != "A" || c.Windows[1].Label != "B" {
		t.Errorf("window labels = %q, want [A B]", []string{c.Windows[0].Label, c.Windows[1].Label})
	}
	if c.Windows[0].Width <= c.Windows[1].Width {
		t.Errorf("window A width %f must differ from window B's %f", c.Windows[0].Width, c.Windows[1].Width)
	}

	// The counter style inputs the recorded trace depends on.
	style := c.CounterStyle
	if style.Padding == "" || style.FontSize == "" || style.Justify == "" || style.Background == "" {
		t.Fatalf("counter_style incomplete: %+v", style)
	}
	if style.Justify != "Center" {
		t.Errorf("justify = %q, want Center (the centered label placement the trace records)", style.Justify)
	}
	if len(style.Background) != 8 {
		t.Errorf("background = %q, want 8 hex digits RRGGBBAA", style.Background)
	}

	// The script: increments from both windows and a closure, with the
	// wire tags round-tripping.
	if len(c.Script) != 4 {
		t.Fatalf("script has %d ops, want 4", len(c.Script))
	}
	if c.Script[0].Op != OpIncrement || c.Script[0].Window != "A" || c.Script[0].Clicks != 2 {
		t.Errorf("script[0] = %+v, want increment from A with 2 clicks", c.Script[0])
	}
	if c.Script[2].Op != OpCloseWindow || c.Script[2].Window != "A" {
		t.Errorf("script[2] = %+v, want close-window of A", c.Script[2])
	}
	var increments, closes int
	for _, op := range c.Script {
		switch op.Op {
		case OpIncrement:
			increments++
		case OpCloseWindow:
			closes++
		default:
			t.Errorf("unknown script op %q", op.Op)
		}
	}
	if increments != 3 || closes != 1 {
		t.Errorf("script op counts = increments %d, closes %d; want 3/1", increments, closes)
	}
}

// TestAuthoringCounterScriptOpRoundTrip checks that the script ops of
// the fx-0006 fixture marshal back into the exact Rust wire shape (op
// tag plus the variant's fields), the same way the fx-0001/fx-0003
// round-trip tests do for their input types.
func TestAuthoringCounterScriptOpRoundTrip(t *testing.T) {
	env, err := LoadEnvelope(authoringCounterFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope: %v", err)
	}
	for _, op := range env.Inputs.CounterCases.Cases[0].Script {
		data, err := json.Marshal(op)
		if err != nil {
			t.Fatalf("marshaling op %s: %v", op.Op, err)
		}
		var again CounterScriptOp
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

// TestAuthoringCounterScriptOpValidation checks that malformed counter
// script ops are rejected during unmarshaling the way the Rust enum
// deserializer would: unknown tags, missing required fields.
func TestAuthoringCounterScriptOpValidation(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"missing tag", `{"window": "A"}`, `missing "op" tag`},
		{"unknown tag", `{"op": "resize-window", "window": "A"}`, "unknown op tag"},
		{"increment without clicks", `{"op": "increment", "window": "A"}`, `missing required field "clicks"`},
		{"close without window", `{"op": "close-window"}`, `missing required field "window"`},
	}
	for _, tc := range cases {
		var op CounterScriptOp
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

// TestAuthoringCounterRecordedTrace checks the recorded reference trace
// for fx-0006: identity, envelope hash agreement, the per-case event
// shape (the case lifecycle, the model entity, the two windows' view
// lifecycles, the per-capture text-shape/text-glyph/window-drawn/
// painted-quad/debug-bounds stream, the script ops and the window
// closure) and exact self-comparison.
func TestAuthoringCounterRecordedTrace(t *testing.T) {
	raw, err := os.ReadFile(authoringCounterRecordedTracePath)
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
	if trace.FixtureID != "fx-0006-authoring-counter" {
		t.Fatalf("fixture id = %q", trace.FixtureID)
	}
	if trace.FixtureKind != FixtureKindAuthoringCounter {
		t.Fatalf("fixture kind = %q", trace.FixtureKind)
	}
	if trace.Harness.GpuiCommit != "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a" {
		t.Fatalf("gpui commit = %q", trace.Harness.GpuiCommit)
	}

	// The recorded envelope hash must match the fixture file in the repo.
	envHash, err := SHA256Envelope(authoringCounterFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	if trace.EnvelopeSHA256 != envHash {
		t.Fatalf("recorded trace envelope hash %s does not match current envelope %s; the fixture changed after the recording", trace.EnvelopeSHA256, envHash)
	}

	// Event shape: dense ordered sequences and the fx-0006 event
	// vocabulary.
	caseBeginOrder := []string{}
	counts := map[string]int{}
	for i, event := range trace.Events {
		if want := uint64(i + 1); event.Seq != want {
			t.Fatalf("event %d seq = %d, want %d (dense ordered seq)", i, event.Seq, want)
		}
		counts[event.Name]++
		switch event.Name {
		case "case-begin":
			caseBeginOrder = append(caseBeginOrder, event.Label)
			for _, field := range []string{"label", "value", "remSize", "scale", "windowCount"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("case-begin %q missing field %q", event.Label, field)
				}
			}
			if got, _ := event.Fields["scale"].(string); got != F32Bits(2.0) {
				t.Errorf("case-begin %q scale = %q, want f32:40000000 (2.0, the reference test window scale)", event.Label, got)
			}
			if got, _ := event.Fields["remSize"].(string); got != F32Bits(16.0) {
				t.Errorf("case-begin %q remSize = %q, want f32:41800000 (16.0, the reference window default rem)", event.Label, got)
			}
		case "entity-created":
			if event.Label != "model" {
				t.Errorf("entity-created label = %q, want model", event.Label)
			}
		case "window-opened", "view-created":
			if event.Label != "A" && event.Label != "B" {
				t.Errorf("%s label = %q, want a window label", event.Name, event.Label)
			}
		case "text-shape":
			for _, field := range []string{"textLen", "fontSize", "lineHeight", "width", "ascent", "descent", "lineCount", "glyphCount"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-shape %q missing field %q", event.Label, field)
				}
			}
		case "text-glyph":
			for _, field := range []string{"index", "id", "x", "y", "isEmoji"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-glyph %q missing field %q", event.Label, field)
				}
			}
		case "window-drawn":
			for _, field := range []string{"value", "renders", "notified", "quads", "monoSprites", "subpixelSprites", "polychromeSprites"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("window-drawn %q missing field %q", event.Label, field)
				}
			}
		case "painted-quad":
			for _, field := range []string{"order", "boundsX", "boundsY", "boundsW", "boundsH", "maskX", "maskY", "maskW", "maskH", "backgroundH", "backgroundS", "backgroundL", "backgroundA", "borderStyle"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("painted-quad %q missing field %q", event.Label, field)
				}
			}
		case "debug-bounds":
			if _, ok := event.Fields["present"]; !ok {
				t.Fatalf("debug-bounds %q missing present", event.Label)
			}
		case "op-begin":
			if _, ok := event.Fields["op"]; !ok {
				t.Fatalf("op-begin %q missing op", event.Label)
			}
		case "window-closed":
			if _, ok := event.Fields["windowCount"]; !ok {
				t.Fatalf("window-closed %q missing windowCount", event.Label)
			}
		case "case-end":
		default:
			t.Fatalf("unexpected event name %q at seq %d", event.Name, event.Seq)
		}
	}

	// All cases begin and end, in fixture order.
	if len(caseBeginOrder) != len(wantAuthoringCounterCaseLabels) {
		t.Errorf("case-begin count = %d, want %d", len(caseBeginOrder), len(wantAuthoringCounterCaseLabels))
	}
	for i, want := range wantAuthoringCounterCaseLabels {
		if i < len(caseBeginOrder) && caseBeginOrder[i] != want {
			t.Errorf("case-begin %d = %q, want %q", i, caseBeginOrder[i], want)
		}
	}
	if counts["case-begin"] != 1 || counts["case-end"] != 1 || counts["entity-created"] != 1 {
		t.Errorf("lifecycle counts = case %d/%d, entity %d; want 1/1/1", counts["case-begin"], counts["case-end"], counts["entity-created"])
	}
	if counts["window-opened"] != 2 || counts["view-created"] != 2 {
		t.Errorf("window/view counts = %d/%d, want 2/2", counts["window-opened"], counts["view-created"])
	}
	if counts["window-closed"] != 1 {
		t.Errorf("window-closed count = %d, want 1", counts["window-closed"])
	}
	// Captures: the initial capture plus one per increment op, two
	// windows each (until the closure).
	if want := 7; counts["window-drawn"] != want {
		t.Errorf("window-drawn count = %d, want %d (2 + 2 + 2 + 1)", counts["window-drawn"], want)
	}
	if counts["window-drawn"] != counts["text-shape"] {
		t.Errorf("text-shape count = %d, want %d (one per captured window)", counts["text-shape"], counts["window-drawn"])
	}
	if counts["painted-quad"] != counts["window-drawn"] {
		t.Errorf("painted-quad count = %d, want %d (one background quad per captured window)", counts["painted-quad"], counts["window-drawn"])
	}
	if want := 2 * counts["window-drawn"]; counts["debug-bounds"] != want {
		t.Errorf("debug-bounds count = %d, want %d (root and label per captured window)", counts["debug-bounds"], want)
	}

	// Exact self-comparison must hold.
	if result := CompareTraces(&trace, &trace); !result.Equal {
		t.Fatalf("self comparison failed: %s", result.Diff)
	}
}
