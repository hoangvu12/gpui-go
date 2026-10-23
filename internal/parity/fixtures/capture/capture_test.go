//go:build windows

// The capture fixture's discrimination tests (the tracer pattern): a
// deliberately wrong gpui-go view must make the Tier B transcript
// comparison FAIL with a readable diff, while the comparisons it does
// not target still pass — so a failure always means the thing it says.
package capture_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/capture"
)

// run replays the capture fixture with the given view and returns the
// record and the replay.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := capture.Fixture
	fixture.View = view
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	return record, replay
}

// TestWithoutCaptureFailsOnlyTheEventTranscript is the Tier B
// discriminator: a view whose press never requests the capture loses
// every got/lostpointercapture, and the drag-outside moves target the
// hit-test chain (other, p, root) instead of the captured one — the
// transcript comparison must catch the first divergence by name; the
// view renders and lays out identically, so the box and pixel
// comparisons still pass.
func TestWithoutCaptureFailsOnlyTheEventTranscript(t *testing.T) {
	record, replay := run(t, capture.WithoutCapture)
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the view without the capture request passed the Tier B transcript comparison")
	}
	// The readable diff names the first divergence: event 11, where
	// Chrome recorded the gotpointercapture (the pending capture
	// processing at the move) and the no-capture view delivered the
	// un-retargeted boundary events and move instead.
	if !strings.Contains(err.Error(), "event 11") {
		t.Fatalf("the transcript diff is not readable:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	// Discrimination: the boxes and the pixels are unchanged.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("dropping the capture request must not move any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, capture.Fixture.Tolerance); err != nil {
		t.Errorf("dropping the capture request must not change any pixel: %v", err)
	}
}

// The correct view's full comparison is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers); the tolerance's headroom is pinned by
// TestPixelTolerancesKeepHeadroom in internal/parity/measure_test.go.
