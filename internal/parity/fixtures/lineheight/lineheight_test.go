//go:build windows

// The line-height fixtures' discrimination test (the tracer/whitespace
// pattern): the deliberately-wrong view — the golden-ratio line
// height, the reference's default and the port's pre-D1 default — must
// FAIL the Tier A box comparison its difference lives in: every row's
// height is the font-metrics line spacing, and the wrong view's rows
// are the fraction's. That is what makes the records real gates: a
// regression back to the golden-ratio default cannot pass them.
package lineheight_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/lineheight"
)

// TestWrongPhiViewFailsTheBoxComparison checks the Segoe fixture's
// discrimination: the golden-ratio view fails Tier A on every row with
// a readable diff (the fraction's heights: 12px → 19.5, 16px → 26,
// 24px → 39 — against the record's 16, 21, 32).
func TestWrongPhiViewFailsTheBoxComparison(t *testing.T) {
	fixture := lineheight.Fixture
	fixture.View = lineheight.WrongPhiView
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
		t.Fatal("the golden-ratio view (the port's rule, before D1) passed the Tier A box comparison")
	}
	for _, id := range []string{"row-12", "span-12", "row-16", "span-16", "row-24", "span-24", "row-48", "span-48", "row-wrap", "span-wrap", "row-mixed", "span-mixed", "row-ratio", "span-ratio"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)

	// The taller golden-ratio lines are visible: the pixel comparison
	// fails too.
	if err := parity.ComparePixels(record, replay, lineheight.Fixture.Tolerance); err == nil {
		t.Fatal("the wrong view passed the Tier C pixel comparison: the golden-ratio lines are hidden by the tolerance")
	} else {
		t.Logf("Tier C fails as it should:\n%v", err)
	}
}

// TestWrongPhiViewKeepsTheEventTranscript pins the other tier: the
// wrong view changes no events (the fixture has none), so a Tier B pass
// under the wrong view means the Tier A/C failures are the boxes and
// pixels alone.
func TestWrongPhiViewKeepsTheEventTranscript(t *testing.T) {
	fixture := lineheight.Fixture
	fixture.View = lineheight.WrongPhiView
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
