//go:build windows

// The gridlayout fixtures' discrimination tests (the tracer/textwrap
// pattern): the deliberately-wrong views must FAIL the Tier A box
// comparison with readable diffs naming the moved boxes — the pinned
// repeat form (equal fr columns instead of 240px 1fr), the dropped
// named placements (auto placement) and the dropped baseline alignment
// (the offsets vanish).
package gridlayout_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/gridlayout"
)

// wrongViewFailsBoxes checks that the wrong view fails the Tier A box
// comparison naming the given ids.
func wrongViewFailsBoxes(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement, ids []string) {
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
	err = parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the wrong view passed the Tier A box comparison")
	}
	for _, id := range ids {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
}

// TestWrongTrackListFailsTheBoxComparison: the pinned repeat form —
// every column an equal fr instead of the fixed 240px track.
func TestWrongTrackListFailsTheBoxComparison(t *testing.T) {
	wrongViewFailsBoxes(t, gridlayout.TracksFixture, gridlayout.WrongTracksView, []string{
		// The second column of each grid starts at 230 instead of 240.
		"a2", "a4", "b2",
	})
}

// TestWrongAreasFailsTheBoxComparison: the named placements dropped —
// the items land in auto-placement order.
func TestWrongAreasFailsTheBoxComparison(t *testing.T) {
	wrongViewFailsBoxes(t, gridlayout.AreasFixture, gridlayout.WrongAreasView, []string{
		// head narrows to the first column, side moves to row 1's
		// second column, main to row 2's first.
		"head", "side", "main",
	})
}

// TestWrongBaselineFailsTheBoxComparison: the baseline alignment
// dropped — the 5px and 3.5px offsets vanish from both rows.
func TestWrongBaselineFailsTheBoxComparison(t *testing.T) {
	wrongViewFailsBoxes(t, gridlayout.BaselineFixture, gridlayout.WrongBaselineView, []string{
		// The 12px and 16px items sit at the row top instead of 5px
		// and 3.5px below it; the rows shrink by the 5px shim.
		"flex-small", "flex-medium", "flexrow",
		"grid-small", "grid-medium", "gridrow",
	})
}
