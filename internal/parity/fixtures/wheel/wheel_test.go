//go:build windows

// The wheel fixtures' discrimination test (the scrollrange pattern):
// the deliberately-wrong view drops the inner scroller's
// overscroll-behavior: contain — the pre-ticket pin, which let an
// unconsumed wheel chain straight to the outer scroller — and must
// FAIL the wheel-contain record's Tier A box comparison with a
// readable diff naming the outer-dependent boxes, while the
// inner-scroller boxes still match. That is what makes the record a
// real gate: a regression back to unconditional chaining cannot pass
// it. The latching itself is gated by the same records (the wheel-latch
// record's outer sits at its unscrolled position after the inner's
// clamped gesture) and by the port's own red/green unit tests
// (gpui/wheel_scroll_test.go, verified failing against the pre-fix
// code where nested scrollers moved together).
package wheel_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/wheel"
)

// run replays the fixture with the given view and returns the record
// and the replay.
func run(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog, *gpui.Window, *gpui.App) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
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

// TestWrongNoContainFailsTheBoxComparison checks the contain fixture's
// discrimination: the view without overscroll-behavior: contain (the
// pin's unconditional chaining, before ticket 15) fails Tier A on the
// boxes that move with the outer scroller.
func TestWrongNoContainFailsTheBoxComparison(t *testing.T) {
	// The wrong view: the same twin with the contain rule dropped, so
	// the last wheel step's unconsumed delta chains to the outer
	// scroller exactly the way the pre-ticket pin scrolled it.
	record, replay := run(t, wheel.ContainFixture, func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		return wheel.WheelView(log, w, app, "")
	})
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the view without overscroll-behavior: contain (the pin's unconditional chaining, before ticket 15) passed the Tier A box comparison")
	}
	// The boxes that ride the outer scroller's offset — the inner
	// scroller's box, its rows and the outer spacer — all sit 60px low
	// in the wrong view (the record keeps the outer at zero through the
	// contain cut; the wrong view chains the last wheel step's delta to
	// it). The diff names every one of them.
	for _, id := range []string{"inner:", "ospacer:", "irow2:", "irow3:", "irow4:"} {
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	// The outer scroller's own box does not depend on the chaining rule
	// (its offset moves its content, not itself).
	if strings.Contains(err.Error(), "outer:") {
		t.Fatalf("the box diff wrongly reports the unchanged outer scroller:\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
}
