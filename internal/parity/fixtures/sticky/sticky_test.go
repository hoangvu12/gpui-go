//go:build windows

// The sticky fixtures' discrimination test (the tracer's tracer_test.go
// pattern): the deliberately-wrong view — every sticky element left at
// the port's default relative, the pre-ticket state, so the elements
// sit at their natural flow positions and never stick — must FAIL the
// Tier A box comparison on exactly the boxes the sticky offsets moved
// (the pinned header, its bar child and the pinned footer), while the
// scroller and every natural element still match. That is what makes
// the records a real gate: a regression that drops the sticky pass
// cannot pass them.
package sticky_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/sticky"
)

// run replays the scrolled fixture with the given view and returns the
// record and the replay.
func run(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog, *gpui.Window, *gpui.App) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture.View = nil
	fixture.ViewWithWindow = view
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

// TestWrongRelativeViewFailsTheBoxComparison checks the fixture's
// discrimination on the scrolled record (scroll 120): the relative
// view fails Tier A on the three moved boxes — the pinned header
// (natural −60 instead of 32), its bar child, and the pinned footer —
// with a readable diff, while the scroller, the focus row, the section
// and the spacer (all at their natural positions) still match.
func TestWrongRelativeViewFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, sticky.TopScrolledFixture, sticky.WrongRelativeScrolledView)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the relative view (the pre-ticket state, no sticking) passed the Tier A box comparison")
	}
	for _, id := range []string{"header-a", "bar", "footer"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report the moved %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	for _, id := range []string{"root", "scroller", "focus", "section-a", "filler-a", "spacer", "tail"} {
		if strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff wrongly reports the unchanged %q:\n%v", id, err)
		}
	}
}

// TestWrongRelativeViewFailsThePixelComparison checks that the wrong
// view also fails Tier C: the pinned boxes paint in the wrong places.
func TestWrongRelativeViewFailsThePixelComparison(t *testing.T) {
	record, replay := run(t, sticky.TopScrolledFixture, sticky.WrongRelativeScrolledView)
	err := parity.ComparePixels(record, replay, sticky.TopScrolledFixture.Tolerance)
	if err == nil {
		t.Fatal("the relative view (the pre-ticket state, no sticking) passed the Tier C pixel comparison")
	}
	t.Logf("Tier C fails as it should:\n%v", err)
}

// The correct views' end-to-end replays are TestFixtures in
// internal/parity/replay_test.go (all three tiers), and the tolerance
// headroom is TestPixelTolerancesKeepHeadroom in
// internal/parity/measure_test.go.
