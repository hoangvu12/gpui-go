//go:build windows

// The modal fixtures' discrimination test (the tracer's tracer_test.go
// pattern): the deliberately-wrong view is the pre-ticket modal — a
// plain popover-family overlay the app opens with Show, no
// Window.ShowModal. The core never runs the focusing steps (the first
// Tab's keydown focus field still reads the invoker), the outside is
// never inert (the pass-out Tab walks onto the outside anchor), and a
// manual-kind entry has no close watcher, so Escape closes nothing —
// the record's restore and close never happen, and the failure surfaces
// as a readable Tier B diff instead of a mysteriously passing fixture.
package modal_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/modal"
)

// TestDismissWithoutModalFailsTheEventTranscript checks the
// discrimination: without ShowModal the transcript loses the modal's
// behaviour — the toggle never closes on the light dismiss, and the
// Tab entries' focus fields disagree with the record (the focusing
// steps never ran and the outside is interactive).
func TestDismissWithoutModalFailsTheEventTranscript(t *testing.T) {
	fixture := modal.DismissFixture
	fixture.ViewWithWindow = modal.DismissWithoutModal
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	err = parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the without-ShowModal view must fail the Tier B transcript comparison")
	}
	if !strings.Contains(err.Error(), "focus") {
		t.Fatalf("the Tier B diff must name the focus-field disagreement, got: %v", err)
	}
}

// TestDismissWithoutModalFailsTheBoxes: the wrong view never closes
// the overlay, so the settle leaves the dialog (and its scrim) painting
// over the bare page the record captured — Tier A's anchor box fails
// under the scrim? No: the boxes still compare (geometry does not
// move), so this test pins the PIXEL tier: the scrim darkens every
// compared box.
func TestDismissWithoutModalFailsThePixelComparison(t *testing.T) {
	fixture := modal.DismissFixture
	fixture.ViewWithWindow = modal.DismissWithoutModal
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	err = parity.ComparePixels(record, replay, fixture.Tolerance)
	if err == nil {
		t.Fatal("the without-ShowModal view must fail the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "hot pixel") {
		t.Fatalf("the Tier C diff must report the hot pixels of the never-closed modal, got: %v", err)
	}
}
