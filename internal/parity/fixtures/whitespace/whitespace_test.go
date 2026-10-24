//go:build windows

// The whitespace fixtures' discrimination test (the tracer/position
// pattern, fixtures/tracer/tracer_test.go): the deliberately-wrong view
// must FAIL the comparison its difference lives in — Tier A, the boxes:
// every row's height is its line count, and the wrong view's line
// counts are the port's pre-ticket ones — with a readable diff naming
// the rows, while Tier C fails with it. That is what makes the records
// real gates: a regression back to the no-collapse rule cannot pass
// them.
package whitespace_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/whitespace"
)

// TestWrongPreserveFailsTheBoxComparison checks the collapse fixture's
// discrimination: the pre view (the port's rule, before ticket 38 —
// nothing collapses, every newline renders its own row) fails Tier A on
// every collapse row with a readable diff.
func TestWrongPreserveFailsTheBoxComparison(t *testing.T) {
	fixture := whitespace.CollapseFixture
	fixture.View = whitespace.WrongPreserveView
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
		t.Fatal("the pre view (the port's rule, before ticket 38) passed the Tier A box comparison")
	}
	// The rows whose line counts the collapse owns: the three
	// single-line paths over "Alpha   beta\n\ngamma" (three rows under
	// pre), and the emptied paragraph (two rows under pre).
	for _, id := range []string{"row-normal", "span-normal", "row-leaf", "span-leaf", "row-styled", "span-styled", "row-empty", "span-empty"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)

	// The un-collapsed spaces are visible: the pixel comparison fails
	// too (the preline row keeps its height under pre, but its spaces
	// render).
	if err := parity.ComparePixels(record, replay, whitespace.CollapseFixture.Tolerance); err == nil {
		t.Fatal("the wrong view passed the Tier C pixel comparison: the un-collapsed spaces are hidden by the tolerance")
	} else {
		t.Logf("Tier C fails as it should:\n%v", err)
	}
}

// TestWrongPreserveViewKeepsTheEventTranscript pins the other tier: the
// wrong view changes no events (the fixture has none), so a Tier B pass
// under the wrong view means the Tier A/C failures are the boxes and
// pixels alone.
func TestWrongPreserveViewKeepsTheEventTranscript(t *testing.T) {
	fixture := whitespace.CollapseFixture
	fixture.View = whitespace.WrongPreserveView
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the wrong view must not change the event transcript: %v", err)
	}
}
