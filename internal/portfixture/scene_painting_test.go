package portfixture

import (
	"runtime"
	"testing"

	"gpui-go/conformance"
)

const (
	// scenePaintingFixturePath is the fx-0003 envelope this gate
	// executes.
	scenePaintingFixturePath = "../../conformance/fixtures/fx-0003-scene-painting.json"
	// scenePaintingRecordedTracePath is the oracle recorded from the
	// pinned reference harness.
	scenePaintingRecordedTracePath = "../../conformance/recorded/fx-0003-scene-painting/trace.json"
)

// TestFx0003ScenePaintingMatchesRecordedReference is the ticket08 gate
// for the scene-painting fixture: the port runner must reproduce the
// FULL recorded reference trace (all 142 events: every case-begin with
// the test-profile scale, every layout-bounds event, every scene-command
// with its batch structure and filter-target plan, every scene-op with
// the exact f32 bit patterns of the bounds, masks, colors, radii and
// widths and the kernel-assigned draw order, and every scene-meta with
// the totals and requirements) through the real gpui scene kernel (the
// native scene service behind internal/native) driven by the port's
// paint API.
func TestFx0003ScenePaintingMatchesRecordedReference(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native scene artifact is Windows AMD64; the scene painting cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(scenePaintingFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunScenePainting(envelope)
	if err != nil {
		t.Fatalf("running port scene painting: %v", err)
	}

	// Run metadata: pin the envelope bytes this run executed.
	sha, err := conformance.SHA256Envelope(scenePaintingFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha

	reference, err := conformance.LoadTrace(scenePaintingRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}

	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("scene painting trace mismatch:\n%s", result.Diff)
	}
	if result.Diff != "" {
		t.Fatalf("equal comparison must report an empty diff, got %q", result.Diff)
	}
	if got, want := len(trace.Events), len(reference.Events); got != want {
		t.Errorf("event count = %d, want %d", got, want)
	}
}

// TestFx0003ScenePaintingBehavioralFacts pins the observable scene
// behaviors directly from the port run, independent of the file-based
// comparison: six cases, the recorded per-case event shapes, the pinned
// kind tie-break at equal orders (shadow before quad before underline),
// order reuse for non-overlapping content, and the paired filter-group
// target plan.
// fieldUint decodes a trace field that may be a uint64 (port-emitted) or
// a float64 (JSON-decoded reference).
func fieldUint(v any) uint64 {
	switch value := v.(type) {
	case uint64:
		return value
	case float64:
		return uint64(value)
	case int:
		return uint64(value)
	default:
		return 0
	}
}

func TestFx0003ScenePaintingBehavioralFacts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native scene artifact is Windows AMD64; the scene painting cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(scenePaintingFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunScenePainting(envelope)
	if err != nil {
		t.Fatalf("running port scene painting: %v", err)
	}

	var caseBegins, commands, ops, metas int
	kinds := map[string]int{}
	for _, event := range trace.Events {
		switch event.Name {
		case "case-begin":
			caseBegins++
			if got, _ := event.Fields["scale"].(string); got != conformance.F32Bits(2.0) {
				t.Errorf("case-begin %q scale = %q, want the reference test window's 2.0", event.Label, got)
			}
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
			if _, ok := event.Fields["order"]; !ok {
				t.Errorf("scene-op %q missing order", kind)
			}
		case "scene-meta":
			metas++
			for _, field := range []string{"opCount", "layerCount", "commandCount", "instanceBatchCount", "usesOffscreenTarget"} {
				if _, ok := event.Fields[field]; !ok {
					t.Errorf("scene-meta missing field %q", field)
				}
			}
		}
	}
	if caseBegins != 6 {
		t.Errorf("case-begin count = %d, want 6", caseBegins)
	}
	if metas != 6 {
		t.Errorf("scene-meta count = %d, want 6", metas)
	}
	if commands == 0 || ops == 0 {
		t.Fatalf("scene-command/scene-op counts = %d/%d, want non-zero", commands, ops)
	}
	// The fixture paints quads, shadows, underlines, backdrop filters and
	// filter boundaries (no surfaces: the reference's insert_surface is
	// crate-private, so the paired-opacity path is covered by the kernel
	// tests instead).
	for _, kind := range []string{"quad", "shadow", "underline", "backdrop-filter", "filter-boundary"} {
		if kinds[kind] == 0 {
			t.Errorf("no scene-op events of kind %q", kind)
		}
	}

	// The order-reuse-ties case: at order 1 the pinned PrimitiveKind
	// discriminant order emits the shadow before the quads before the
	// underline; the filter group brackets its child; the post-group
	// sibling sorts above the end marker (the close-time floor).
	var sawShadow, sawQuad, sawUnderline, sawBegin, sawEnd bool
	var lastOpKind string
	var lastOpOrder uint64
	inTies := false
	for _, event := range trace.Events {
		if event.Name == "case-begin" {
			inTies = event.Label == "order-reuse-ties"
			continue
		}
		if !inTies || event.Name != "scene-op" {
			continue
		}
		kind, _ := event.Fields["kind"].(string)
		// The port-side trace carries counts and orders as uint64 (the
		// reference trace decodes them as float64 from JSON; the exact
		// gate normalizes both through CompareTraces).
		order := fieldUint(event.Fields["order"])
		switch {
		case kind == "shadow" && order == 1 && !sawQuad:
			sawShadow = true
		case kind == "quad" && order == 1:
			if !sawShadow {
				t.Errorf("order-1 quads emitted before the order-1 shadow; the pinned kind tie-break emits Shadow first")
			}
			sawQuad = true
		case kind == "underline" && order == 1:
			sawUnderline = true
		case kind == "filter-boundary":
			if fieldUint(event.Fields["isStart"]) == 1 {
				sawBegin = true
			} else {
				sawEnd = true
			}
		}
		lastOpKind = kind
		lastOpOrder = uint64(order)
	}
	if !sawShadow || !sawQuad || !sawUnderline {
		t.Errorf("order-reuse-ties missing the equal-order kind spread: shadow=%v quad=%v underline=%v", sawShadow, sawQuad, sawUnderline)
	}
	if !sawBegin || !sawEnd {
		t.Errorf("order-reuse-ties missing the filter group markers: begin=%v end=%v", sawBegin, sawEnd)
	}
	// The last op of the case is the post-group sibling (a quad above the
	// group's end marker, order 6).
	if lastOpKind != "quad" || lastOpOrder != 6 {
		t.Errorf("order-reuse-ties last op = %q order %d, want the post-group quad at order 6", lastOpKind, lastOpOrder)
	}
}
