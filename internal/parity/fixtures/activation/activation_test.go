//go:build windows

// The activation fixtures' discrimination tests (ticket 12
// acceptance): a deliberately wrong gpui-go view must make each of the
// three comparisons FAIL with a readable diff, and each wrong view
// must leave the comparisons it does not target passing — so a failure
// always means the thing it says.
package activation_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/activation"
)

// runWindow replays a fixture with the given window-aware view and
// returns the record and the replay.
func runWindow(t *testing.T, fixture parity.Fixture, view func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
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

func TestAWrongParentBoxFailsTheBoxComparison(t *testing.T) {
	record, replay := runWindow(t, activation.ClickAncestorFixture, activation.WrongAncestorBoxSize())
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the 280x120 parent (wrong size) passed the Tier A box comparison")
	}
	if !strings.Contains(err.Error(), "p: recorded") || !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("the box diff is not readable:\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	// The click still fires on the same chain, so the transcript keeps
	// passing (the bigger box also paints more, so Tier C differs with
	// it — the wrong size is a geometric defect, not a click defect).
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("a parent size change must not change the click transcript: %v", err)
	}
}

func TestAMissingAncestorClickFailsTheEventTranscript(t *testing.T) {
	record, replay := runWindow(t, activation.ClickAncestorFixture, activation.WithoutAncestorClick())
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the view without p's click handler passed the Tier B transcript comparison")
	}
	// The readable diff names the missing click on the ancestor.
	if !strings.Contains(err.Error(), `event 7: recorded click:p`) {
		t.Fatalf("the transcript diff is not readable:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	// Discrimination: the boxes are the same, so Tier A passes (the
	// missing handler also loses the background flip, so Tier C fails
	// with it — the click IS the flip; both failures say the same
	// thing here).
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a missing click handler must not move any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, activation.ClickAncestorFixture.Tolerance); err == nil {
		t.Error("the view that never flips p's background passed the Tier C pixel comparison")
	}
}

func TestAUnflippedClickFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := runWindow(t, activation.ClickAncestorFixture, activation.NoClickFlip())
	err := parity.ComparePixels(record, replay, activation.ClickAncestorFixture.Tolerance)
	if err == nil {
		t.Fatal("the view whose click does not flip p's background passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "p: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	// Discrimination: the boxes and the transcript are unchanged.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a missing background flip must not move any box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("a missing background flip must not change the event transcript: %v", err)
	}
}

func TestAButtonWithoutTheActiveStyleFailsTheHeldPixelComparison(t *testing.T) {
	// The Space-held fixture's Tier C gate is the :active colour: a
	// view without the Active style shows the focus colour while the
	// key is held.
	record, replay := runWindow(t, activation.SpaceHeldFixture, activation.ButtonView(activation.NoActiveVariant))
	err := parity.ComparePixels(record, replay, activation.SpaceHeldFixture.Tolerance)
	if err == nil {
		t.Fatal("the view without the button's Active style passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "btn: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	// Discrimination: the transcript (no keyup, no click) and the boxes
	// are unchanged by the style.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a missing active style must not move any box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("a missing active style must not change the event transcript: %v", err)
	}
}

func TestAButtonWithoutTheRootClickFailsTheBubblingTranscript(t *testing.T) {
	// The Enter fixture's Tier B gate is the click's bubbling: without
	// root's click listener, both the mouse click and the keyboard
	// activation click lose click:root.
	record, replay := runWindow(t, activation.EnterFixture, activation.ButtonView(activation.NoRootClickVariant))
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the view without root's click handler passed the Tier B transcript comparison")
	}
	if !strings.Contains(err.Error(), `event 6: recorded click:root`) {
		t.Fatalf("the transcript diff is not readable:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	// Discrimination: the view renders identically, so the other two
	// comparisons pass.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a missing root click handler must not move any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, activation.EnterFixture.Tolerance); err != nil {
		t.Errorf("a missing root click handler must not change any pixel: %v", err)
	}
}

func TestARingOnAClickFocusFailsThePixelComparison(t *testing.T) {
	// The click-focus fixture's Tier C gate: a view that styles the
	// plain Focus state with the ring colour shows a ring on a click
	// focus, which the browser does not.
	record, replay := runWindow(t, activation.FocusVisibleClickFixture, activation.FocusView(activation.OneBox, activation.RingOnFocus))
	err := parity.ComparePixels(record, replay, activation.FocusVisibleClickFixture.Tolerance)
	if err == nil {
		t.Fatal("the view that rings a click focus passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "box: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	// Discrimination: the transcript and the boxes are unchanged.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a ring on the wrong state must not move any box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("a ring on the wrong state must not change the event transcript: %v", err)
	}
}

func TestNoRingOnATabFocusFailsThePixelComparison(t *testing.T) {
	// The tab-focus fixture's Tier C gate: a view without the
	// FocusVisible style shows only the focus colour on the keyboard
	// focus, which the browser rings.
	record, replay := runWindow(t, activation.FocusVisibleTabFixture, activation.FocusView(activation.TwoBoxes, activation.NoRing))
	err := parity.ComparePixels(record, replay, activation.FocusVisibleTabFixture.Tolerance)
	if err == nil {
		t.Fatal("the view without the focus-visible style passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "b: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	// Discrimination: the transcript and the boxes are unchanged.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a missing focus-visible style must not move any box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("a missing focus-visible style must not change the event transcript: %v", err)
	}
}

// The replay comparison of the correct views is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers).
