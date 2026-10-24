//go:build windows

// The tabnav fixtures' discrimination test (the keyorder_test.go
// pattern): the record must FAIL its comparison when the behaviour it
// claims is absent, with a readable diff — so a passing fixture means
// the thing it says. Ticket 14's one deliberately-wrong view is the
// order fixture's: every tabindex flattened to zero, the pre-ticket
// pin's DOM order, which the D4-ranked record must reject.
package tabnav_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tabnav"
)

// viewFn is the fixture view signature (parity.Fixture.ViewWithWindow).
type viewFn = func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement

// runWithView replays one fixture's record against a variant view.
func runWithView(t *testing.T, fixture parity.Fixture, view viewFn) (*parity.Record, *parity.ReplayResult) {
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

// TestOrderFailsWhenTabIndexesAreFlattened: replaying the order record
// against the DOM-order view (every tabindex zero — the pin's order)
// must fail Tier B at the very first Tab (the record's first keyup
// sees p1, the lowest positive tabindex; the flattened view's sees
// z2, the first element in DOM order) while the boxes are unchanged.
// Tier C cannot discriminate this wrong view — both walks end
// blurred (the flattened cycle's backward pass-out lands on the body
// just like the ranked one's) — so the transcript is the gate, and
// the box comparison proves the failure is not a layout accident.
func TestOrderFailsWhenTabIndexesAreFlattened(t *testing.T) {
	record, replay := runWithView(t, tabnav.OrderFixture, tabnav.BoxesView(tabnav.OrderBoxesForTest(), false))
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the DOM-order view passed Tier B against the D4-ranked record (the tab order must follow the tabindex ranking)")
	}
	// The diff line spells each entry with its fields (key, code,
	// focus, isComposing since ticket 18); match the two targets only.
	if msg := err.Error(); !strings.Contains(msg, "recorded keyup:p1 ") || !strings.Contains(msg, "replayed keyup:z2 ") {
		t.Fatalf("the transcript diff does not name the first ranked move:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the tab order must not change any box: %v", err)
	}
}
