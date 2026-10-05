package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordedTracePath points at the reference trace recorded for
// fx-0001-layout-effects by conformance/cmd/recordreference. The recording is
// an executed artifact of ticket01: the trace was produced by the pinned CE
// reference harness and is byte-deterministic across runs.
const recordedTracePath = "recorded/fx-0001-layout-effects/trace.json"

func loadRecordedTrace(t *testing.T) *Trace {
	t.Helper()
	raw, err := os.ReadFile(recordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	var trace Trace
	if err := json.Unmarshal(raw, &trace); err != nil {
		t.Fatalf("parsing recorded trace: %v", err)
	}
	return &trace
}

// TestRecordedReferenceTraceIdentity checks the identity fields of the
// recorded reference trace for fx-0001-layout-effects.
func TestRecordedReferenceTraceIdentity(t *testing.T) {
	trace := loadRecordedTrace(t)
	if trace.Schema != TraceSchema {
		t.Fatalf("schema = %q, want %q", trace.Schema, TraceSchema)
	}
	if trace.FixtureID != "fx-0001-layout-effects" {
		t.Fatalf("fixture id = %q", trace.FixtureID)
	}
	if trace.Harness.GpuiCommit != "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a" {
		t.Fatalf("gpui commit = %q", trace.Harness.GpuiCommit)
	}
	if trace.EnvelopeSHA256 == "" {
		t.Fatal("empty envelope hash")
	}

	// The recorded envelope hash must match the envelope file in the repo.
	envHash, err := SHA256Envelope(filepath.Join("fixtures", "fx-0001-layout-effects.json"))
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	if trace.EnvelopeSHA256 != envHash {
		t.Fatalf("recorded trace envelope hash %s does not match current envelope %s; the fixture changed after the recording", trace.EnvelopeSHA256, envHash)
	}

	// Reference behavior sanity: the effect section shows notification
	// coalescing (one observer-notified for a count of two notifies), FIFO
	// event delivery, and release-before-effects; the layout section records
	// bounds for every labeled node with exact f32 bit patterns.
	var notified, delivered, released, bounds int
	for _, event := range trace.Events {
		switch event.Name {
		case "observer-notified":
			notified++
		case "event-delivered":
			delivered++
		case "entity-released":
			released++
		case "layout-bounds":
			bounds++
			for _, field := range []string{"x", "y", "width", "height"} {
				value, ok := event.Fields[field]
				if !ok {
					t.Fatalf("layout-bounds event %d missing field %q", event.Seq, field)
				}
				text, ok := value.(string)
				if !ok {
					t.Fatalf("layout-bounds field %q is not an f32 string: %v", field, value)
				}
				if _, err := ParseF32Bits(text); err != nil {
					t.Fatalf("layout-bounds field %q is not a valid f32 bit pattern: %v", field, err)
				}
			}
		}
	}
	if notified != 1 {
		t.Errorf("observer-notified count = %d, want 1 (two notifies in one update must coalesce)", notified)
	}
	if delivered != 3 {
		t.Errorf("event-delivered count = %d, want 3 (FIFO)", delivered)
	}
	if released != 1 {
		t.Errorf("entity-released count = %d, want 1", released)
	}
	if bounds != 6 {
		t.Errorf("layout-bounds count = %d, want 6 (root, a, b, c, c1, d)", bounds)
	}
}

// TestRecordedReferenceTraceMismatchDemonstration is the intentional mismatch
// demonstration against the real recorded reference trace. It mutates the
// recorded trace in five material ways and requires the comparator to catch
// each one at a precise location. A comparator that cannot fail these checks
// cannot validate the port.
func TestRecordedReferenceTraceMismatchDemonstration(t *testing.T) {
	trace := loadRecordedTrace(t)

	// Self comparison must be equal.
	if result := CompareTraces(trace, trace); !result.Equal {
		t.Fatalf("self comparison failed: %s", result.Diff)
	}

	// Locate one layout-bounds event with a non-zero f32 field to mutate.
	target := -1
	field := ""
	for i, event := range trace.Events {
		if event.Name == "layout-bounds" {
			for name, value := range event.Fields {
				if text, ok := value.(string); ok && text != "f32:00000000" {
					target, field = i, name
					break
				}
			}
			if target >= 0 {
				break
			}
		}
	}
	if target < 0 {
		t.Fatal("no layout-bounds event with a non-zero f32 field found")
	}

	// Mutation 1: flip the lowest bit of one f32 value.
	bitFlip := deepCopyTrace(trace)
	value := bitFlip.Events[target].Fields[field].(string)
	bits, err := ParseF32Bits(value)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	flipped := F32Bits(bits + 1) // a one-bit change in the mantissa
	if flipped == value {
		t.Fatalf("bit flip produced identical string for %q", value)
	}
	bitFlip.Events[target].Fields[field] = flipped
	result := CompareTraces(trace, bitFlip)
	if result.Equal {
		t.Fatal("one-bit f32 change was not detected")
	}
	if !containsAll(result.Diff, trace.Events[target].Name, field, value, flipped) {
		t.Errorf("diff does not name the exact event/field: %s", result.Diff)
	}

	// Mutation 2: swap two consecutive events with different names.
	swap := deepCopyTrace(trace)
	for i := 0; i+1 < len(swap.Events); i++ {
		if swap.Events[i].Name != swap.Events[i+1].Name {
			swap.Events[i], swap.Events[i+1] = swap.Events[i+1], swap.Events[i]
			result := CompareTraces(trace, swap)
			if result.Equal {
				t.Fatal("event order change was not detected")
			}
			// The comparator reports the first differing event position and seq.
			if !containsAll(result.Diff, "event[", "seq") {
				t.Errorf("diff does not name the swapped event: %s", result.Diff)
			}
			break
		}
	}

	// Mutation 3: remove one event.
	remove := deepCopyTrace(trace)
	removedSeq := remove.Events[target].Seq
	remove.Events = append(remove.Events[:target], remove.Events[target+1:]...)
	_ = removedSeq
	result = CompareTraces(trace, remove)
	if result.Equal {
		t.Fatal("event removal was not detected")
	}
	if !containsAll(result.Diff, "count", fmt.Sprintf("%d", len(trace.Events)), fmt.Sprintf("%d", len(remove.Events))) {
		t.Errorf("diff does not report the count mismatch: %s", result.Diff)
	}

	// Mutation 4: change one integer field (value in an entity event).
	intChange := deepCopyTrace(trace)
	changed := false
	for i, event := range intChange.Events {
		if event.Name == "event-delivered" {
			for name, value := range event.Fields {
				if number, ok := value.(float64); ok {
					intChange.Events[i].Fields[name] = number + 1
					changed = true
					result := CompareTraces(trace, intChange)
					if result.Equal {
						t.Fatal("integer field change was not detected")
					}
					if !containsAll(result.Diff, event.Name, name) {
						t.Errorf("diff does not name the changed field: %s", result.Diff)
					}
					break
				}
			}
			if changed {
				break
			}
		}
	}
	if !changed {
		t.Fatal("no integer field found to mutate")
	}

	// Mutation 5: change the recorded fixture id.
	relabel := deepCopyTrace(trace)
	relabel.FixtureID = "fx-9999-other"
	result = CompareTraces(trace, relabel)
	if result.Equal {
		t.Fatal("fixture id change was not detected")
	}
	if !containsAll(result.Diff, relabel.FixtureID) {
		t.Errorf("diff does not name the fixture id: %s", result.Diff)
	}
}

func deepCopyTrace(trace *Trace) *Trace {
	raw, err := json.Marshal(trace)
	if err != nil {
		panic(err)
	}
	var copy Trace
	if err := json.Unmarshal(raw, &copy); err != nil {
		panic(err)
	}
	return &copy
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
