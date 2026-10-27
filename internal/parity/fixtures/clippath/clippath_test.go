// The clippath fixture's discrimination tests (the tracer's
// pattern): the deliberately-wrong view must make the comparisons
// FAIL with readable diffs. The wrong view drops the clip-paths -
// the full squares - so Tier C fails (the corners the shapes cut
// away) and the record's event transcript discriminates the hit
// test separately (the fixture's own pass pins it).

package clippath_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/clippath"
)

// run replays the fixture with the given view.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := clippath.Fixture
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

// TestUnclippedViewFailsPixelsAndEvents checks the
// discrimination: the mask dropped (the fully opaque box) fails Tier C
// — the record's lower half fades to white, the wrong view's stays
// blue — while the boxes (Tier A) are unchanged.
func TestUnclippedViewFailsPixelsAndEvents(t *testing.T) {
	record, replay := run(t, clippath.WrongUnclippedView)
	err := parity.ComparePixels(record, replay, clippath.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the wrong view passed the Tier C pixel comparison - the fixture's feature is not being discriminated")
	}
	if !strings.Contains(err.Error(), "clip-circle: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("dropping the mask must not move any box: %v", err)
	}
}

// TestClicksOutsideTheShapesMissTheBoxes pins the event-log half of
// the record directly: the four clicks (two inside the shapes, two
// inside the boxes' bounds but OUTSIDE the shapes) produce exactly the
// record's transcript — the outside clicks reach the root alone, the
// clipped elements' hit tests answer point-in-shape (css-masking-1:
// the clip-path shapes pointer events).
func TestClicksOutsideTheShapesMissTheBoxes(t *testing.T) {
	record, replay := run(t, clippath.View)
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("the clipped view's transcript must match the record exactly:\n%v", err)
	}
	// The record's own shape: the inside clicks bubble (box then
	// root), the outside clicks hit the root alone.
	var targets []string
	for _, event := range replay.Events {
		if event.Kind == "click" {
			targets = append(targets, event.Target)
		}
	}
	want := []string{"clip-circle", "root", "root", "clip-poly", "root", "root"}
	if len(targets) != len(want) {
		t.Fatalf("the click transcript = %v, want %v", targets, want)
	}
	for i, target := range targets {
		if target != want[i] {
			t.Fatalf("the click transcript = %v, want %v (a click outside the shape must miss the element)", targets, want)
		}
	}
}
