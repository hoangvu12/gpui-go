//go:build windows

// The hover fixture's discrimination tests (the tracer pattern): a
// deliberately wrong gpui-go view must make the Tier B transcript
// comparison FAIL with a readable diff, while the comparisons it does
// not target still pass — so a failure always means the thing it says.
package hover_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/hover"
)

// run replays the hover fixture with the given view and returns the
// record and the replay.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := hover.Fixture
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

// TestMissingEnterListenersFailsOnlyTheEventTranscript is the Tier B
// discriminator: a view without the enter listeners loses every
// pointerenter (the boundary sweeps fire nothing on entry), which the
// transcript comparison must catch by name; the view renders
// identically, so the box and pixel comparisons still pass.
func TestMissingEnterListenersFailsOnlyTheEventTranscript(t *testing.T) {
	record, replay := run(t, hover.WithoutEnterListeners)
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the view without enter listeners passed the Tier B transcript comparison")
	}
	// The readable diff names the first missing pointerenter.
	if !strings.Contains(err.Error(), `event 1: recorded pointerenter:root`) {
		t.Fatalf("the transcript diff is not readable:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	// Discrimination: the boxes and the pixels are unchanged.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("dropping enter listeners must not move any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, hover.Fixture.Tolerance); err != nil {
		t.Errorf("dropping enter listeners must not change any pixel: %v", err)
	}
}

// The correct view's full comparison is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers); the tolerance's headroom is pinned by
// TestPixelTolerancesKeepHeadroom in internal/parity/measure_test.go.
