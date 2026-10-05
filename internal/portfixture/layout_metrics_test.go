package portfixture

import (
	"runtime"
	"testing"

	"gpui-go/conformance"
)

const (
	// layoutMetricsFixturePath is the fx-0002 envelope this gate executes.
	layoutMetricsFixturePath = "../../conformance/fixtures/fx-0002-layout-metrics.json"
	// layoutMetricsRecordedTracePath is the oracle recorded from the
	// pinned reference harness.
	layoutMetricsRecordedTracePath = "../../conformance/recorded/fx-0002-layout-metrics/trace.json"
)

// TestFx0002LayoutMetricsMatchesRecordedReference is the ticket06 gate for
// the layout-metrics fixture: the port runner must reproduce the FULL
// recorded reference trace (all 63 events: every case-begin, every
// measure-query/measure-result pair with the exact logical values the
// reference passes, every layout-bounds event and every case-end, byte
// for byte in the f32 bit patterns) through the real gpui layout adapter.
func TestFx0002LayoutMetricsMatchesRecordedReference(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native layout artifact is Windows AMD64; the layout metrics cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(layoutMetricsFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunLayoutMetrics(envelope)
	if err != nil {
		t.Fatalf("running port layout metrics: %v", err)
	}

	// Run metadata: pin the envelope bytes this run executed.
	sha, err := conformance.SHA256Envelope(layoutMetricsFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha

	reference, err := conformance.LoadTrace(layoutMetricsRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}

	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("layout metrics trace mismatch:\n%s", result.Diff)
	}
	if result.Diff != "" {
		t.Fatalf("equal comparison must report an empty diff, got %q", result.Diff)
	}
	if got, want := len(trace.Events), len(reference.Events); got != want {
		t.Errorf("event count = %d, want %d", got, want)
	}
}

// TestFx0002LayoutMetricsBehavioralFacts pins the observable layout
// behaviors directly from the port run, independent of the file-based
// comparison: the test-profile scale 2.0 and default rem 16 in every
// case-begin, the rem override in its case, the raw (unsnapped) measured
// sizes, and the cross-axis stretch of measured flex children.
func TestFx0002LayoutMetricsBehavioralFacts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native layout artifact is Windows AMD64; the layout metrics cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(layoutMetricsFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunLayoutMetrics(envelope)
	if err != nil {
		t.Fatalf("running port layout metrics: %v", err)
	}

	var caseBegins, bounds, queries, results int
	for _, event := range trace.Events {
		switch event.Name {
		case "case-begin":
			caseBegins++
			if got, _ := event.Fields["scale"].(string); got != conformance.F32Bits(2.0) {
				t.Errorf("case-begin %q scale = %q, want the reference test window's 2.0", event.Label, got)
			}
		case "layout-bounds":
			bounds++
		case "measure-query":
			queries++
		case "measure-result":
			results++
		}
	}
	if caseBegins != 9 {
		t.Errorf("case-begin count = %d, want 9", caseBegins)
	}
	if bounds != 33 {
		t.Errorf("layout-bounds count = %d, want 33 (every node of every case)", bounds)
	}
	// m-fixed and m-echo: 4 and 2 queries in the recorded reference.
	if queries != 6 || results != 6 {
		t.Errorf("measure events = %d queries / %d results, want 6 / 6", queries, results)
	}

	// The measured fixed node returns its spec size exactly (40x25 raw
	// logical pixels): the fixture must not pre-snap measured sizes.
	var fixedWidth, fixedHeight, fixedBoundsHeight any
	var remOverrideRem any
	for _, event := range trace.Events {
		if event.Label == "m-fixed" && event.Name == "measure-result" {
			fixedWidth = event.Fields["width"]
			fixedHeight = event.Fields["height"]
		}
		if event.Label == "m-fixed" && event.Name == "layout-bounds" {
			fixedBoundsHeight = event.Fields["height"]
		}
		if event.Label == "rem-override-20" && event.Name == "case-begin" {
			remOverrideRem = event.Fields["remSize"]
		}
	}
	if fixedWidth != conformance.F32Bits(40) || fixedHeight != conformance.F32Bits(25) {
		t.Errorf("m-fixed measure-result = %v x %v, want raw 40x25 logical", fixedWidth, fixedHeight)
	}
	// Taffy stretches flex children on the cross axis: the measured node
	// reported 25 logical but its final box is the container height 60.
	if fixedBoundsHeight != conformance.F32Bits(60) {
		t.Errorf("m-fixed bounds height = %v, want 60 (cross-axis stretch)", fixedBoundsHeight)
	}
	// The rem override reaches the request-time rem scope.
	if remOverrideRem != conformance.F32Bits(20) {
		t.Errorf("rem-override-20 remSize = %v, want 20", remOverrideRem)
	}
}
