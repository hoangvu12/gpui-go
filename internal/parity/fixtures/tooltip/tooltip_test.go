//go:build windows

// The tooltip fixtures' discrimination test (the tracer's tracer_test.go
// pattern): the deliberately-wrong view renders the default look with a
// different text, so the box's width (Tier A) and the glyphs (Tier C)
// differ from the record — the discrimination that makes the look
// fixture's gates mean something. The timed story (the delays, the
// grace, Escape) is discriminated by the records themselves: the key
// markers between the waits make any timing regression fail Tier B, and
// the constants are pinned by the unit tests (gpui/tooltip_test.go).
package tooltip_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tooltip"
)

// TestLookWrongTextFailsTheBoxes checks the discrimination: a different
// default-look text changes the box's width, failing Tier A exactly.
func TestLookWrongTextFailsTheBoxes(t *testing.T) {
	fixture := tooltip.LookFixture
	fixture.ViewWithWindow = tooltip.WrongLookView
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	err = parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the wrong-text view must fail the Tier A box comparison")
	}
	if !strings.Contains(err.Error(), "tooltip") {
		t.Fatalf("the Tier A diff must name the tooltip's box, got: %v", err)
	}
}

// TestLookWrongTextFailsThePixelComparison: the wrong text's glyphs
// fail the Tier C gate (the look's discrimination).
func TestLookWrongTextFailsThePixelComparison(t *testing.T) {
	fixture := tooltip.LookFixture
	fixture.ViewWithWindow = tooltip.WrongLookView
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
		t.Fatal("the wrong-text view must fail the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "tooltip") {
		t.Fatalf("the Tier C diff must name the tooltip's box, got: %v", err)
	}
}
