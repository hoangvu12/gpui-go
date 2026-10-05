package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// layoutMetricsFixturePath is the fx-0002 fixture envelope, and
// layoutMetricsRecordedTracePath is the reference trace recorded from it
// by conformance/cmd/recordreference.
const (
	layoutMetricsFixturePath       = "fixtures/fx-0002-layout-metrics.json"
	layoutMetricsRecordedTracePath = "recorded/fx-0002-layout-metrics/trace.json"
)

// wantLayoutMetricsCaseLabels lists the eight fx-0002 cases in execution
// order.
var wantLayoutMetricsCaseLabels = []string{
	"flex-wrap-row",
	"grid-minmax-columns",
	"absolute-inset-anchors",
	"rem-override-20",
	"measured-fixed-flex",
	"measured-echo-min-content",
	"percent-margin-padding-border",
	"max-height-clamp", "block-display-stacking",
}

// wantLayoutMetricsMeasuredLabels lists the node labels that carry
// measurement specs in fx-0002, keyed by case label.
var wantLayoutMetricsMeasuredLabels = map[string][]string{
	"measured-fixed-flex":       {"m-fixed"},
	"measured-echo-min-content": {"m-echo"},
}

// TestLayoutMetricsEnvelope checks the shipped fx-0002 fixture against the
// layout-metrics wire format: fixture kind detection, eight cases with the
// expected labels, and the per-case inputs the recorded trace depends on.
func TestLayoutMetricsEnvelope(t *testing.T) {
	env, err := LoadEnvelope(layoutMetricsFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", layoutMetricsFixturePath, err)
	}
	if env.FixtureID != "fx-0002-layout-metrics" {
		t.Errorf("fixture_id = %q, want %q", env.FixtureID, "fx-0002-layout-metrics")
	}
	if env.FixtureKind != FixtureKindLayoutMetrics {
		t.Fatalf("fixture_kind = %q, want %q", env.FixtureKind, FixtureKindLayoutMetrics)
	}
	if !env.IsLayoutMetricsKind() {
		t.Errorf("IsLayoutMetricsKind() = false, want true")
	}
	if env.Profile != "test" {
		t.Errorf("profile = %q, want %q", env.Profile, "test")
	}
	if want := []string{"headless", "test-platform"}; len(env.Environment) != 2 || env.Environment[0] != want[0] || env.Environment[1] != want[1] {
		t.Errorf("environment = %q, want %q", env.Environment, want)
	}

	// The layout-metrics inputs live in inputs.layout_cases; the legacy
	// inputs stay zero-valued for this fixture kind.
	if env.Inputs.LayoutCases == nil {
		t.Fatalf("inputs.layout_cases is nil")
	}
	cases := env.Inputs.LayoutCases.Cases
	if len(cases) != 9 {
		t.Fatalf("layout_cases has %d cases, want 9", len(cases))
	}
	for i, want := range wantLayoutMetricsCaseLabels {
		if got := cases[i].Label; got != want {
			t.Errorf("case %d label = %q, want %q", i, got, want)
		}
	}

	// The rem override case: rem_size 20 (the default is 16).
	remCase := cases[3]
	if remCase.Label != "rem-override-20" {
		t.Fatalf("case 3 label = %q, want rem-override-20", remCase.Label)
	}
	if remCase.RemSize == nil || *remCase.RemSize != 20 {
		t.Errorf("rem-override-20 rem_size = %v, want 20", remCase.RemSize)
	}
	for i, c := range cases {
		if i == 3 {
			continue
		}
		if c.RemSize != nil {
			t.Errorf("case %q rem_size = %v, want nil (default 16)", c.Label, c.RemSize)
		}
	}

	// The min-content case: the width axis is offered as min-content.
	echoCase := cases[5]
	if echoCase.Label != "measured-echo-min-content" {
		t.Fatalf("case 5 label = %q, want measured-echo-min-content", echoCase.Label)
	}
	if echoCase.AvailableWidthMode != AvailModeMinContent {
		t.Errorf("measured-echo-min-content available_width_mode = %q, want %q", echoCase.AvailableWidthMode, AvailModeMinContent)
	}
	if echoCase.AvailableHeightMode != "" {
		t.Errorf("measured-echo-min-content available_height_mode = %q, want empty (definite)", echoCase.AvailableHeightMode)
	}
	if want := (SizeInput{Width: 120, Height: 60}); echoCase.AvailableSpace != want {
		t.Errorf("measured-echo-min-content available_space = %+v, want %+v", echoCase.AvailableSpace, want)
	}

	// Every case has a style tree; the measured cases carry specs keyed by
	// node label with the prescribed kinds.
	for i, c := range cases {
		if c.StyleTree == nil {
			t.Fatalf("case %d (%q) has no style_tree", i, c.Label)
		}
		if c.StyleTree.Label == "" {
			t.Errorf("case %q style tree root has empty label", c.Label)
		}
		wantLabels, ok := wantLayoutMetricsMeasuredLabels[c.Label]
		if !ok {
			if len(c.Measured) != 0 {
				t.Errorf("case %q has %d measured specs, want none", c.Label, len(c.Measured))
			}
			continue
		}
		if len(c.Measured) != len(wantLabels) {
			t.Fatalf("case %q has %d measured specs, want %d", c.Label, len(c.Measured), len(wantLabels))
		}
		for _, label := range wantLabels {
			spec, ok := c.Measured[label]
			if !ok {
				t.Fatalf("case %q has no measured spec for node %q", c.Label, label)
			}
			switch c.Label {
			case "measured-fixed-flex":
				if spec.Kind != MeasuredKindFixed || spec.Width != 40 || spec.Height != 25 {
					t.Errorf("m-fixed spec = %+v, want fixed 40x25", spec)
				}
			case "measured-echo-min-content":
				if spec.Kind != MeasuredKindEchoKnown || spec.Width != 50 || spec.Height != 20 {
					t.Errorf("m-echo spec = %+v, want echo-known 50x20", spec)
				}
			}
		}
	}

	// The style trees use the extended layout-metrics key set.
	gridRoot := cases[1].StyleTree.Style
	if got := gridRoot["gridTemplateColumns"]; got != "repeat(2, minmax(min-content, 1fr))" {
		t.Errorf("grid root gridTemplateColumns = %q, want repeat(2, minmax(min-content, 1fr))", got)
	}
	if got := gridRoot["gridTemplateRows"]; got != "repeat(2, minmax(0, 1fr))" {
		t.Errorf("grid root gridTemplateRows = %q, want repeat(2, minmax(0, 1fr))", got)
	}
	if got := cases[2].StyleTree.Style["position"]; got != "Relative" {
		t.Errorf("absolute case root position = %q, want Relative", got)
	}
	if got := cases[2].StyleTree.Children[1].Style["position"]; got != "Absolute" {
		t.Errorf("abs-top-right position = %q, want Absolute", got)
	}
	if got := cases[0].StyleTree.Style["flexWrap"]; got != "Wrap" {
		t.Errorf("wrap root flexWrap = %q, want Wrap", got)
	}
	if got := cases[6].StyleTree.Children[0].Style["borderWidth"]; got != "2px" {
		t.Errorf("p-outer borderWidth = %q, want 2px", got)
	}
}

// TestLayoutMetricsEnvelopeRoundTrip checks that the fx-0002 envelope
// marshals back into the same wire shape (layout_cases wrapper,
// snake_case keys, pointer optionals), the same way the fx-0001 round-trip
// test does for the effects inputs.
func TestLayoutMetricsEnvelopeRoundTrip(t *testing.T) {
	env, err := LoadEnvelope(layoutMetricsFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope: %v", err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshaling envelope: %v", err)
	}
	var again Envelope
	if err := json.Unmarshal(data, &again); err != nil {
		t.Fatalf("unmarshaling envelope: %v", err)
	}
	remarshal, err := json.Marshal(again)
	if err != nil {
		t.Fatalf("re-marshaling envelope: %v", err)
	}
	if string(data) != string(remarshal) {
		t.Errorf("envelope does not round-trip:\ngot  %s\nwant %s", remarshal, data)
	}
	if again.Inputs.LayoutCases == nil || len(again.Inputs.LayoutCases.Cases) != 9 {
		t.Fatalf("round-tripped layout_cases = %+v, want 9 cases", again.Inputs.LayoutCases)
	}
}

// TestLayoutMetricsRecordedTrace checks the recorded reference trace for
// fx-0002: identity, envelope hash agreement, the per-case event shape
// (case-begin/case-end for all eight cases, measure-query/measure-result
// pairs for every measured node, layout-bounds for every node) and exact
// self-comparison.
func TestLayoutMetricsRecordedTrace(t *testing.T) {
	raw, err := os.ReadFile(layoutMetricsRecordedTracePath)
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
	if trace.FixtureID != "fx-0002-layout-metrics" {
		t.Fatalf("fixture id = %q", trace.FixtureID)
	}
	if trace.FixtureKind != FixtureKindLayoutMetrics {
		t.Fatalf("fixture kind = %q, want %q", trace.FixtureKind, FixtureKindLayoutMetrics)
	}
	if trace.Harness.GpuiCommit != "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a" {
		t.Fatalf("gpui commit = %q", trace.Harness.GpuiCommit)
	}

	// The recorded envelope hash must match the fixture file in the repo.
	envHash, err := SHA256Envelope(layoutMetricsFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	if trace.EnvelopeSHA256 != envHash {
		t.Fatalf("recorded trace envelope hash %s does not match current envelope %s; the fixture changed after the recording", trace.EnvelopeSHA256, envHash)
	}

	// Event shape per case: case-begin (with label, remSize and scale f32
	// fields), measure-query/measure-result pairs for the measured nodes,
	// layout-bounds for every node, case-end. Sequences are dense and
	// ordered, and the case-begin events appear in fixture order.
	caseBeginOrder := []string{}
	caseEnds := 0
	queries := map[string]int{}
	results := map[string]int{}
	bounds := 0
	for i, event := range trace.Events {
		if want := uint64(i + 1); event.Seq != want {
			t.Fatalf("event %d seq = %d, want %d (dense ordered seq)", i, event.Seq, want)
		}
		switch event.Name {
		case "case-begin":
			caseBeginOrder = append(caseBeginOrder, event.Label)
			for _, field := range []string{"label", "remSize", "scale"} {
				value, ok := event.Fields[field]
				if !ok {
					t.Fatalf("case-begin %q missing field %q", event.Label, field)
				}
				if field != "label" {
					if _, err := asF32Bits(value); err != nil {
						t.Fatalf("case-begin %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
					}
				}
			}
			if got, _ := event.Fields["label"].(string); got != event.Label {
				t.Errorf("case-begin %q label field = %q, want %q", event.Label, got, event.Label)
			}
			if got, err := asF32Bits(event.Fields["scale"]); err != nil || got != "f32:40000000" {
				t.Errorf("case-begin %q scale = %q (%v), want f32:40000000 (2.0, the reference test window scale)", event.Label, got, err)
			}
			if _, err := asF32Bits(event.Fields["remSize"]); err != nil {
				t.Errorf("case-begin %q remSize = %v, want valid f32 bit pattern", event.Label, event.Fields["remSize"])
			}
		case "case-end":
			caseEnds++
		case "measure-query":
			queries[event.Label]++
			for _, field := range []string{"label", "knownWidthPresent", "knownHeightPresent", "availWidthTag", "availHeightTag"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("measure-query %q missing field %q", event.Label, field)
				}
			}
			present, _ := event.Fields["knownWidthPresent"].(float64)
			if _, has := event.Fields["knownWidth"]; has != (present == 1) {
				t.Errorf("measure-query %q knownWidth present = %v but flag = %v", event.Label, has, present)
			}
			present, _ = event.Fields["knownHeightPresent"].(float64)
			if _, has := event.Fields["knownHeight"]; has != (present == 1) {
				t.Errorf("measure-query %q knownHeight present = %v but flag = %v", event.Label, has, present)
			}
			tag, _ := event.Fields["availWidthTag"].(string)
			if _, has := event.Fields["availWidth"]; has != (tag == AvailModeDefinite) {
				t.Errorf("measure-query %q availWidth present = %v but tag = %q", event.Label, has, tag)
			}
			tag, _ = event.Fields["availHeightTag"].(string)
			if _, has := event.Fields["availHeight"]; has != (tag == AvailModeDefinite) {
				t.Errorf("measure-query %q availHeight present = %v but tag = %q", event.Label, has, tag)
			}
		case "measure-result":
			results[event.Label]++
			for _, field := range []string{"width", "height"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("measure-result %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
				}
			}
		case "layout-bounds":
			bounds++
			for _, field := range []string{"x", "y", "width", "height"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("layout-bounds %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
				}
			}
		default:
			t.Fatalf("unexpected event name %q at seq %d", event.Name, event.Seq)
		}
	}

	// All eight cases begin and end, in fixture order.
	if len(caseBeginOrder) != 9 {
		t.Errorf("case-begin count = %d, want 9", len(caseBeginOrder))
	}
	for i, want := range wantLayoutMetricsCaseLabels {
		if i < len(caseBeginOrder) && caseBeginOrder[i] != want {
			t.Errorf("case-begin %d = %q, want %q", i, caseBeginOrder[i], want)
		}
	}
	if caseEnds != 9 {
		t.Errorf("case-end count = %d, want 9", caseEnds)
	}

	// Every measured node was queried at least once, and every query
	// produced exactly one result.
	for caseLabel, labels := range wantLayoutMetricsMeasuredLabels {
		for _, label := range labels {
			if queries[label] == 0 {
				t.Errorf("case %q: node %q was never measured", caseLabel, label)
			}
			if queries[label] != results[label] {
				t.Errorf("node %q: %d measure-query events but %d measure-result events", label, queries[label], results[label])
			}
		}
	}
	// No other node was measured.
	if len(queries) != len(wantLayoutMetricsMeasuredLabels) {
		t.Errorf("measured node labels = %v, want exactly m-fixed and m-echo", queries)
	}

	// Every node of every case reports bounds: 33 nodes across the nine
	// cases (5+4+4+3+4+2+3+3+5).
	if bounds != 33 {
		t.Errorf("layout-bounds count = %d, want 33", bounds)
	}

	// The rem override shows up as remSize 20 in its case-begin; every
	// other case records the 16px default.
	for label, want := range map[string]string{
		"rem-override-20":     F32Bits(20),
		"flex-wrap-row":       F32Bits(16),
		"grid-minmax-columns": F32Bits(16),
	} {
		event, ok := caseBeginEvent(&trace, label)
		if !ok {
			t.Fatalf("missing case-begin for %q", label)
		}
		if got, _ := asF32Bits(event.Fields["remSize"]); got != want {
			t.Errorf("case %q remSize = %q, want %q", label, got, want)
		}
	}

	// The measured fixed node returns its spec size exactly (40x25
	// logical pixels): the fixture must not pre-snap measured sizes.
	fixedResult, ok := firstEvent(&trace, "measure-result", "m-fixed")
	if !ok {
		t.Fatalf("missing measure-result for m-fixed")
	}
	if got, _ := asF32Bits(fixedResult.Fields["width"]); got != F32Bits(40) {
		t.Errorf("m-fixed measure-result width = %q, want %q", got, F32Bits(40))
	}
	if got, _ := asF32Bits(fixedResult.Fields["height"]); got != F32Bits(25) {
		t.Errorf("m-fixed measure-result height = %q, want %q", got, F32Bits(25))
	}

	// Exact self-comparison must hold.
	if result := CompareTraces(&trace, &trace); !result.Equal {
		t.Fatalf("self comparison failed: %s", result.Diff)
	}
}

// caseBeginEvent returns the case-begin event with the given case label.
func caseBeginEvent(trace *Trace, label string) (TraceEvent, bool) {
	for _, event := range trace.Events {
		if event.Name == "case-begin" && event.Label == label {
			return event, true
		}
	}
	return TraceEvent{}, false
}

// firstEvent returns the first event with the given name and label.
func firstEvent(trace *Trace, name, label string) (TraceEvent, bool) {
	for _, event := range trace.Events {
		if event.Name == name && event.Label == label {
			return event, true
		}
	}
	return TraceEvent{}, false
}

// asF32Bits asserts that v is an f32 bit-pattern string and returns its
// canonical form.
func asF32Bits(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("value %v is not a string", v)
	}
	f, err := ParseF32Bits(s)
	if err != nil {
		return "", err
	}
	return F32Bits(f), nil
}
