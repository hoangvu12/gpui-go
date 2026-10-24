//go:build windows

// The popover fixtures' discrimination test (the tracer's
// tracer_test.go pattern): the deliberately-wrong view drops the
// parent's invoker link, so a click on b1 (outside every popover)
// relates to no popover — light dismiss hides the whole layer, closing
// the parent that the click's own activation then reopens. The
// transcript's tail gains a toggle:p1 open entry after the closed one,
// and the settle leaves the parent painting over the bare page the
// record captured: a regression in the invoker rule surfaces as a
// failing Tier B (and Tier A/C), not as a mysteriously passing
// fixture.
package popover_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/popover"
)

// TestDismissNoInvokerLinkFailsTheEventTranscript checks the dismissal
// fixture's discrimination: without the parent's invoker link the b1
// click's light dismiss closes the parent before the activation
// reopens it, so Tier B fails with the extra toggle:p1 open entry.
func TestDismissNoInvokerLinkFailsTheEventTranscript(t *testing.T) {
	fixture := popover.DismissFixture
	fixture.ViewWithWindow = popover.DismissNoInvokerLink
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
		t.Fatal("the no-invoker-link view must fail the Tier B transcript comparison")
	}
	if !strings.Contains(err.Error(), "toggle:p1 open") {
		t.Fatalf("the Tier B diff must name the reopened parent's toggle entry, got: %v", err)
	}
}

// TestDismissNoInvokerLinkFailsThePixelComparison: the wrong view's
// settle leaves the parent painting over the bare page the record
// captured, so Tier C fails with the popover's hot pixels.
func TestDismissNoInvokerLinkFailsThePixelComparison(t *testing.T) {
	fixture := popover.DismissFixture
	fixture.ViewWithWindow = popover.DismissNoInvokerLink
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
		t.Fatal("the no-invoker-link view must fail the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "hot pixel") {
		t.Fatalf("the Tier C diff must report the hot pixels of the reopened popover, got: %v", err)
	}
}
