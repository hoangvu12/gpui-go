package portfixture

import (
	"runtime"
	"testing"

	"gpui-go/conformance"
)

const (
	// fixturePath is the fx-0001 envelope this gate executes.
	fixturePath = "../../conformance/fixtures/fx-0001-layout-effects.json"
	// recordedTracePath is the oracle recorded from the pinned reference
	// harness by ticket01.
	recordedTracePath = "../../conformance/recorded/fx-0001-layout-effects/trace.json"
)

// TestFx0001FullTraceMatchesRecordedReference is the ticket06 gate: the
// port fixture must reproduce the FULL recorded reference trace (all 54
// events: the effect section AND the layout section, every event, field
// and value, in order) through the real gpui runtime and the real gpui
// layout adapter (the Taffy engine behind internal/native).
func TestFx0001FullTraceMatchesRecordedReference(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native layout artifact is Windows AMD64; the layout section cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(fixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, pending, err := Run(envelope)
	if err != nil {
		t.Fatalf("running port fixture: %v", err)
	}

	// The layout capabilities are implemented by the layout adapter; no
	// capability of this fixture kind stays pending.
	if len(pending) != 0 {
		t.Fatalf("pending capabilities = %v, want none", pending)
	}

	// Run metadata: pin the envelope bytes this run executed. Run receives
	// the parsed envelope only, so the caller supplies the file identity.
	sha, err := conformance.SHA256Envelope(fixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha

	reference, err := conformance.LoadTrace(recordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}

	// The full trace: the effect section and the layout section (the
	// recorded reference carries all 54 events).
	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("full trace mismatch:\n%s", result.Diff)
	}
	if result.Diff != "" {
		t.Fatalf("equal comparison must report an empty diff, got %q", result.Diff)
	}
	if got, want := len(trace.Events), len(reference.Events); got != want {
		t.Errorf("event count = %d, want %d", got, want)
	}
}

// TestFx0001EffectSectionBehavioralFacts pins the observable effect
// behaviors directly from the port run, independent of the file-based
// comparison: pending-only notify coalescing, FIFO event delivery,
// deferred registration of a defer scheduled from inside a running defer,
// task completion during clock advance, and release-before-effects.
func TestFx0001EffectSectionBehavioralFacts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native layout artifact is Windows AMD64; the layout section cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(fixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, _, err := Run(envelope)
	if err != nil {
		t.Fatalf("running port fixture: %v", err)
	}

	var names []string
	eventsByName := make(map[string][]conformance.TraceEvent)
	for _, event := range trace.Events {
		names = append(names, event.Name)
		eventsByName[event.Name] = append(eventsByName[event.Name], event)
	}

	// Two notifies within one outermost update coalesce into exactly one
	// observer-notified, delivered after update-returned.
	if got := len(eventsByName["observer-notified"]); got != 1 {
		t.Errorf("observer-notified count = %d, want 1 (coalescing)", got)
	}
	// Three emits in one update deliver three events in FIFO order.
	if got := len(eventsByName["event-delivered"]); got != 3 {
		t.Errorf("event-delivered count = %d, want 3 (FIFO)", got)
	} else {
		for i, want := range []uint64{1, 2, 3} {
			value, ok := eventsByName["event-delivered"][i].Fields["value"]
			if !ok || value.(uint64) != want {
				t.Errorf("event-delivered[%d].value = %v, want %d", i, value, want)
			}
		}
	}
	// A defer scheduled from inside a running defer runs in the same
	// flush loop, before the update returns.
	if got := len(eventsByName["defer-ran"]); got != 2 {
		t.Errorf("defer-ran count = %d, want 2 (d1 and d1b)", got)
	}
	// The release callback fires at the next flush start, before other
	// effects, after the drop.
	if got := len(eventsByName["entity-released"]); got != 1 {
		t.Errorf("entity-released count = %d, want 1", got)
	} else if value, ok := eventsByName["entity-released"][0].Fields["value"]; !ok || value.(uint64) != 2 {
		t.Errorf("entity-released value = %v, want 2", value)
	}
	// The 50ms task completes during the 100ms clock advance, before the
	// clock-advanced event.
	if got := len(eventsByName["task-started"]); got != 1 {
		t.Errorf("task-started count = %d, want 1", got)
	}
	if got := len(eventsByName["task-completed"]); got != 1 {
		t.Errorf("task-completed count = %d, want 1", got)
	}
	if got := len(eventsByName["task-result-observed"]); got != 1 {
		t.Errorf("task-result-observed count = %d, want 1", got)
	}
	clockIndex := indexOf(names, "clock-advanced")
	for _, name := range []string{"task-started", "task-completed", "task-result-observed"} {
		if indexOf(names, name) > clockIndex {
			t.Errorf("%s must fire before clock-advanced", name)
		}
	}
}

func indexOf(names []string, name string) int {
	for i, candidate := range names {
		if candidate == name {
			return i
		}
	}
	return -1
}
