package portfixture

import (
	"runtime"
	"testing"

	"gpui-go/conformance"
)

const (
	// authoringCounterFixturePath is the fx-0006 envelope this gate
	// executes.
	authoringCounterFixturePath = "../../conformance/fixtures/fx-0006-authoring-counter.json"
	// authoringCounterRecordedTracePath is the oracle recorded from the
	// pinned reference harness.
	authoringCounterRecordedTracePath = "../../conformance/recorded/fx-0006-authoring-counter/trace.json"
)

// TestFx0006AuthoringCounterMatchesRecordedReference is the ticket11
// gate for the authoring-counter fixture: the port runner must
// reproduce the FULL recorded reference trace (all 108 events: the
// case lifecycle, the model entity, the two windows with their view
// lifecycles, the increment script with its coalesced notify
// deliveries, the per-capture text-shape and text-glyph facts of the
// count text, the window-drawn primitive counts, the painted quads with
// the exact f32 bit patterns of the bounds, masks and colors, the
// kernel-assigned draw order, and the root/label debug bounds) through
// the real gpui authoring runtime: the element phases, the div element,
// the view root render, the shared model entity, the layout engine and
// the scene kernel behind internal/native.
func TestFx0006AuthoringCounterMatchesRecordedReference(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native layout/scene artifacts are Windows AMD64; the authoring counter cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(authoringCounterFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunAuthoringCounter(envelope)
	if err != nil {
		t.Fatalf("running port authoring counter: %v", err)
	}

	// Run metadata: pin the envelope bytes this run executed.
	sha, err := conformance.SHA256Envelope(authoringCounterFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha

	reference, err := conformance.LoadTrace(authoringCounterRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}

	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("authoring counter trace mismatch:\n%s", result.Diff)
	}
	if result.Diff != "" {
		t.Fatalf("equal comparison must report an empty diff, got %q", result.Diff)
	}
	if got, want := len(trace.Events), len(reference.Events); got != want {
		t.Errorf("event count = %d, want %d", got, want)
	}
}

// TestFx0006AuthoringCounterBehavioralFacts pins the observable counter
// behaviors directly from the port run, independent of the file-based
// comparison: the shared model drives both windows' views (both
// observers fire per increment, both windows redraw), the count text
// grows with the value (the glyph count and the label bounds), the
// drawn scene carries the background quad, and closing one window
// leaves the other drawing.
func TestFx0006AuthoringCounterBehavioralFacts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native layout/scene artifacts are Windows AMD64; the authoring counter cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(authoringCounterFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunAuthoringCounter(envelope)
	if err != nil {
		t.Fatalf("running port authoring counter: %v", err)
	}

	var caseBegins, caseEnds, textShapes, glyphs, quads, debug int
	windowDrawn := map[string]int{}
	lastTextLen := map[string]uint64{}
	for _, event := range trace.Events {
		switch event.Name {
		case "case-begin":
			caseBegins++
			if got, _ := event.Fields["scale"].(string); got != conformance.F32Bits(2.0) {
				t.Errorf("case-begin %q scale = %q, want the reference test window's 2.0", event.Label, got)
			}
		case "case-end":
			caseEnds++
		case "text-shape":
			textShapes++
			lastTextLen[event.Label] = fieldUint(event.Fields["textLen"])
		case "text-glyph":
			glyphs++
		case "painted-quad":
			quads++
		case "debug-bounds":
			debug++
		case "window-drawn":
			windowDrawn[event.Label]++
			// The text content follows the model value: the glyph count
			// of the count text grows 8 -> 9 with the value 8 -> 10.
			if event.Label == "A" {
				if value := fieldUint(event.Fields["value"]); value == 10 {
					if lastTextLen["A"] != 9 {
						t.Errorf("text length at value 10 = %d, want 9 (\"Count: 10\")", lastTextLen["A"])
					}
				}
			}
		case "window-closed":
			if count := fieldUint(event.Fields["windowCount"]); count != 1 {
				t.Errorf("window-closed windowCount = %v, want 1 (the other window stays open)", event.Fields["windowCount"])
			}
		}
	}
	if caseBegins != 1 || caseEnds != 1 {
		t.Errorf("case begin/end = %d/%d, want 1/1", caseBegins, caseEnds)
	}
	if textShapes == 0 || glyphs == 0 || quads == 0 || debug == 0 {
		t.Fatalf("capture facts = textShapes %d, glyphs %d, quads %d, debug %d; want all non-zero", textShapes, glyphs, quads, debug)
	}
	// Both windows drew at every capture (the shared model notifies both
	// views), and window B kept drawing after window A closed.
	if windowDrawn["A"] == 0 || windowDrawn["B"] == 0 {
		t.Fatalf("window draws = %v, want both windows drawn", windowDrawn)
	}
	if windowDrawn["B"] <= windowDrawn["A"] {
		t.Errorf("window B draws (%d) must outlive window A's (%d) after the close", windowDrawn["B"], windowDrawn["A"])
	}
}
