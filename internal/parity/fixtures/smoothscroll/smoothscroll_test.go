//go:build windows

// The smooth-scroll fixtures' discrimination tests. Two gates:
//
//   - the record comparison's wrong view: a scroller that never
//     scrolls (the rows at their unscrolled positions) must FAIL the
//     keyboard record's Tier A box comparison on exactly the moved
//     boxes — the record is a real gate on the smooth path's
//     convergence, not a rubber stamp.
//   - the mid-animation replay: the port's smooth scrolling must
//     actually animate. The record's end state cannot tell a smooth
//     scroll from an instant one (they settle identically — the reason
//     Chrome's compositor-side animations, which the recorder cannot
//     seek, are pinned by the port-side tests), so this test replays
//     the keyboard fixture with a wait at exactly half the 150ms
//     keyboard animation and reads the scroller's offset: the
//     ease-in-out curve's symmetric midpoint is exactly 20px of the
//     40px step — an instant scroll would sit at 40, and a wrong curve
//     or duration sits measurably elsewhere.
package smoothscroll_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/smoothscroll"
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

// TestWrongUnscrolledViewFailsTheBoxComparison checks the keyboard
// record's discrimination: a view whose scroller never scrolls (no
// overflow, no track — the rows keep their unscrolled positions) fails
// Tier A on every row box while the scroller's own box still matches.
func TestWrongUnscrolledViewFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, smoothscroll.Fixture, smoothscroll.WrongView)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the unscrolled view passed the Tier A box comparison (the record is not a gate)")
	}
	for _, id := range []string{"rrow0:", "rrow1:", "rrow2:", "rrow4:"} {
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	if strings.Contains(err.Error(), "scroller:") {
		t.Fatalf("the box diff wrongly reports the unchanged scroller box:\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
}

// TestKeyboardScrollAnimatesThroughTheMidpoint pins the port's smooth
// keyboard scroll inside the harness: the same fixture with the settle
// wait replaced by a wait at exactly half the 150ms keyboard animation
// reads the scroller's offset at the ease-in-out midpoint — 20px of
// the 40px step, not the instant 40 and not 0.
func TestKeyboardScrollAnimatesThroughTheMidpoint(t *testing.T) {
	fixture := smoothscroll.Fixture
	fixture.Steps = []parity.Step{
		parity.Click(110, 30),
		parity.Key("arrowdown"),
		parity.Wait(75), // half of the keyboard's constant 150ms
	}
	var handle gpui.ScrollHandle
	fixture.ViewWithWindow = func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		handle = smoothscroll.HandleOfTest(w)
		return smoothscroll.KeyboardView(log, w, app)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	_ = replay
	if got := handle.Offset(); got != (gpui.Point{Y: -20}) {
		t.Fatalf("the mid-animation offset = %v, want (0,-20) exactly (the ease-in-out midpoint of the 40px step at half the 150ms animation; an instant scroll reads -40)", got)
	}
}
