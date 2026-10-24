//go:build windows

// The display modes fixture's discrimination test (the tracer's
// tracer_test.go pattern): the deliberately-wrong view — every contents
// element left at the port's default display (flex), the exact state
// before ticket 36 — must FAIL the Tier A box comparison with a
// readable diff naming the moved boxes (the contents element's inert
// box styles start taking space, so the spliced children and the tail
// of the row shift, and the nested column's children shift past the
// space its boxes now occupy), while the row's plain children and the
// inline boxes of the block section still match. That is what makes the
// record a real gate: a regression back to the pin's "contents takes a
// box" cannot pass it.
package display_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/display"
)

// run replays the fixture with the given view and returns the record
// and the replay.
func run(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
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

// TestWrongContentsAsFlexFailsTheBoxComparison checks the fixture's
// discrimination: the pre-ticket pin (contents as a flex box) fails
// Tier A on the shifted boxes, with a readable diff.
func TestWrongContentsAsFlexFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, display.ModesFixture, display.WrongModesView)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the contents-as-flex view (the pin's rule, before ticket 36) passed the Tier A box comparison")
	}
	// The row's spliced children (b, c) and the tail (d) move — the
	// contents element's box takes space — and the nested column's
	// children (nb, nc, nd) shift under the space its boxes occupy.
	for _, id := range []string{"b", "c", "d", "nb", "nc", "nd", "nest"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	// The plain children of the row, the block section's atomic inline
	// boxes and the row containers still match.
	for _, id := range []string{"root", "contents-row", "a", "block", "ib1", "ib2", "ig", "g1", "g2"} {
		if strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff wrongly reports the unchanged %q:\n%v", id, err)
		}
	}
}

// The correct view's end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (all three tiers), and its tolerance
// headroom is TestPixelTolerancesKeepHeadroom in
// internal/parity/measure_test.go.
