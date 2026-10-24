//go:build windows

// The closewatcher fixtures' discrimination test (the tracer's
// tracer_test.go pattern): the deliberately-wrong view wires each
// popover's own Escape listener (the pre-ticket shape, manual entries
// with no close watcher), where a key listener only runs while focus
// sits inside its overlay — the record's first Escape (focus on b2,
// inside p1 and not inside the newest popover) closes the PARENT
// instead of the child, and the second closes nothing. A regression in
// the close-watcher signal surfaces as a failing Tier B (and Tier C),
// not as a mysteriously passing fixture.
package closewatcher_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/closewatcher"
)

// TestEscapePerOverlayFailsTheEventTranscript: without the core's
// close watcher the per-overlay listener closes the parent at the
// first Escape (focus is inside it), so Tier B fails on the wrong
// toggle entry — the record's toggle:p2 closed never happens and a
// toggle:p1 closed appears instead.
func TestEscapePerOverlayFailsTheEventTranscript(t *testing.T) {
	fixture := closewatcher.EscapeFixture
	fixture.ViewWithWindow = closewatcher.EscapePerOverlay
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
		t.Fatal("the per-overlay Escape view must fail the Tier B transcript comparison")
	}
	if !strings.Contains(err.Error(), "toggle:p1 closed") {
		t.Fatalf("the Tier B diff must name the wrongly closed parent's toggle entry, got: %v", err)
	}
}

// TestEscapePerOverlayFailsThePixelComparison: the wrong view leaves
// the child popover painting over the bare page the record captured
// (and the parent's listener closed the parent), so Tier C fails with
// the popover's hot pixels.
func TestEscapePerOverlayFailsThePixelComparison(t *testing.T) {
	fixture := closewatcher.EscapeFixture
	fixture.ViewWithWindow = closewatcher.EscapePerOverlay
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
		t.Fatal("the per-overlay Escape view must fail the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "hot pixel") {
		t.Fatalf("the Tier C diff must report the popover's hot pixels, got: %v", err)
	}
}

// TestEscapePerOverlayStillMatchesTheBoxes: the wrong view only
// changes which overlays close — the root-level boxes it still draws
// match, so Tier A passing while B and C fail pins the failure to the
// behaviour, not the geometry.
func TestEscapePerOverlayStillMatchesTheBoxes(t *testing.T) {
	fixture := closewatcher.EscapeFixture
	fixture.ViewWithWindow = closewatcher.EscapePerOverlay
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Fatalf("the per-overlay Escape view must keep the Tier A boxes: %v", err)
	}
}
