//go:build windows

// The block default fixture's discrimination test (the display
// fixture's pattern): the deliberately-wrong view — every plain div
// left at the pre-71 default display (flex) — must FAIL the Tier A box
// comparison with a readable diff naming the moved boxes (the stack's
// children sit in a row instead of stacking, the auto-width children
// stop filling their containing blocks, the root's blocks sit in a row
// and the paragraph moves, while b1 — an explicit width and an inline
// margin — keeps its own box and the explicit-flex sibling and its
// children are unchanged). That is what makes the record a real gate:
// a regression back to the pin's flex default cannot pass it.
package displayblock_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/displayblock"
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

// TestWrongFlexDefaultsFailTheBoxComparison checks the fixture's
// discrimination: the pre-71 view (every plain div at the flex
// default) fails Tier A on the unstacked and unstretched boxes, with
// a readable diff.
func TestWrongFlexDefaultsFailTheBoxComparison(t *testing.T) {
	record, replay := run(t, displayblock.BlockFixture, displayblock.WrongFlexView)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the flex-default view (the pin's rule, before browser-core 71) passed the Tier A box comparison")
	}
	// The stack's unstretched children, the nested pair and the
	// paragraph (now a row sibling, its text a flex item) move; the
	// stack's own box (explicit size) and b1 (explicit width, inline
	// margin) keep their recorded boxes under flex too, so they may
	// appear in the diff only through the moved parent chain — the
	// named ones below are the ones the diff must report.
	for _, id := range []string{"b2", "nested", "inner", "para", "flex-sib", "fs1", "fs2"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
}

// The correct view's end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (all three tiers).
