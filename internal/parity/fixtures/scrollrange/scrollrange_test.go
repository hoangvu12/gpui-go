//go:build windows

// The scroll-range fixtures' discrimination test (the tracer's
// tracer_test.go pattern): the deliberately-wrong view — the padding
// percentage resolved against the scroller's OWN height, exactly the
// pre-ticket-07 pin (30px instead of Chrome's 64px from the containing
// block's width) — must FAIL the Tier A box comparison with a readable
// diff naming every content box (they all sit 34px low), while the root
// and scroller boxes still match. That is what makes the record a real
// gate: a regression back to the own-height basis cannot pass it. The
// range half (the descendant overflow, the trailing margin and the end
// padding in the End-key scroll offset) is gated by the record itself —
// a wrong range lands every scrolled box off — which is why this
// fixture's wrong view only needs to fake the padding.
package scrollrange_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/scrollrange"
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

// TestWrongPaddingBasisFailsTheBoxComparison checks the fixture's
// discrimination: the pre-ticket pin's own-height percentage basis (the
// wrong view) fails Tier A on the content boxes with a readable diff.
func TestWrongPaddingBasisFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, scrollrange.Fixture, scrollrange.WrongPaddingBasisView)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the own-height padding basis (the pin's rule, before ticket 07) passed the Tier A box comparison")
	}
	// Every content box sits far off: the wrong basis (30px) puts the
	// focus row where the step's click misses it, so the End key never
	// scrolls — the boxes report their unscrolled positions, 44px and
	// more away from the record's scrolled ones. The diff names them all.
	for _, id := range []string{"focus", "nested", "tall", "spacer", "last"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	// The root and the scroller's own border box do not depend on the
	// padding basis.
	for _, id := range []string{"root:", "scroller:"} {
		if strings.Contains(err.Error(), id) {
			t.Fatalf("the box diff wrongly reports the unchanged %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
}
