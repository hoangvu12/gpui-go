//go:build windows

// The mincontent fixture's discrimination test (the tracer/position
// pattern): the deliberately-wrong view — white-space: nowrap on the
// whole tree, the port's exact pre-ticket behaviour, where min-content
// equals max-content so nothing shrinks and nothing wraps — must FAIL
// the Tier A box comparison (every wrap row is one line tall instead of
// wrapped, and every text span overflows) with a readable diff naming
// the rows, and Tier C with it. That is what makes the record a real
// gate: a regression back to the unwrapped min-content cannot pass it.
package mincontent_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/mincontent"
)

// run replays the fixture with the given view and returns the record
// and the replay.
func run(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture.View = view
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

// TestWrongNowrapFailsTheBoxComparison checks the fixture's
// discrimination: the nowrap view (the port's rule, before ticket 32)
// fails Tier A on the wrap rows and text spans, with a readable diff.
func TestWrongNowrapFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, mincontent.Fixture, mincontent.WrongNowwrapView)

	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the nowrap view (the pin's rule, before ticket 32) passed the Tier A box comparison")
	}
	for _, id := range []string{"row-block", "wrap-block", "row-leaf", "wrap-leaf", "row-styled", "wrap-styled", "row-shrink", "shrink-text"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)

	// The overflow is visible: the pixel comparison fails too.
	if err := parity.ComparePixels(record, replay, mincontent.Fixture.Tolerance); err == nil {
		t.Fatal("the nowrap view passed the Tier C pixel comparison: the overflow is hidden by the tolerance")
	} else {
		t.Logf("Tier C fails as it should:\n%v", err)
	}

	// The fixture binds no events, so Tier B must keep passing: the
	// failure always means the boxes, not a transcript difference.
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the wrong view must not change the event transcript: %v", err)
	}
}
