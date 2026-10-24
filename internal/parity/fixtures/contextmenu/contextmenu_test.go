//go:build windows

// The contextmenu fixtures' discrimination test (the keyorder_test.go
// pattern): the record must FAIL its comparison when the behaviour it
// claims is absent, with a readable diff. Ticket 17's one
// deliberately-wrong view is the prevented fixture's: a plain logging
// contextmenu listener (no preventDefault) against the prevented
// record, which must fail Tier B on the default-prevented state — the
// acceptance's "PreventDefault on contextmenu" pin.
package contextmenu_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/contextmenu"
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

// TestPreventedFailsWhenTheListenerDoesNotPrevent: replaying the
// prevented record against a view whose contextmenu listener only
// logs must fail Tier B (the entries lose their default-prevented
// state) while the boxes are unchanged and Tier C still passes — the
// Tab lands on b either way, so the transcript is the gate.
func TestPreventedFailsWhenTheListenerDoesNotPrevent(t *testing.T) {
	// PreventedMenuView with prevent=false: the contextmenu-only view
	// (the record binds only that kind) whose listener logs without
	// preventing — the wrong view.
	record, replay := runWithView(t, contextmenu.PreventedFixture, contextmenu.PreventedMenuView("a", 2, true, false))
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the non-preventing view passed Tier B against the prevented record")
	}
	if !strings.Contains(err.Error(), "contextmenu:a (defaultPrevented)") {
		t.Fatalf("the transcript diff does not name the prevented state:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a prevented contextmenu must not change any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, contextmenu.PreventedFixture.Tolerance); err != nil {
		t.Errorf("the Tab must still move focus to b (a prevented contextmenu suppresses nothing else): %v", err)
	}
}
