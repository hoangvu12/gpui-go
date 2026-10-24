//go:build windows

// The fixed fixture's discrimination test (the tracer's tracer_test.go
// pattern): the deliberately-wrong view leaves the fixed boxes at the
// port's pre-ticket shape — plain absolute, resolving against their DOM
// ancestors — so all three move (fixed-a scrolls with the content and
// is clipped by the scroller, fixed-b lands on the ancestor's padding
// box, fixed-c falls off the window) and the later sibling paints over
// fixed-c's region. Tier A fails on exactly the three moved boxes with
// readable diffs, Tier C on their pixels, and the in-flow boxes still
// match: a regression in the fixed rule surfaces as those failures, not
// as a mysteriously passing fixture.
package fixed_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/fixed"
)

// TestWrongAbsoluteFailsTheBoxes: the three fixed boxes resolve against
// their DOM ancestors instead of the window, so Tier A fails on
// exactly them.
func TestWrongAbsoluteFailsTheBoxes(t *testing.T) {
	fixture := fixed.Fixture
	fixture.ViewWithWindow = fixed.WrongAbsolute
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
		t.Fatal("the absolute view must fail the Tier A box comparison")
	}
	for _, id := range []string{"fixed-a", "fixed-b", "fixed-c"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the Tier A diff must name the moved box %q, got: %v", id, err)
		}
	}
	// The in-flow boxes the wrong view still places correctly keep
	// matching: the failure names exactly the three fixed boxes.
	for _, id := range []string{"scroller:", "content:", "row1:", "ancestor:", "stack:"} {
		if strings.Contains(err.Error(), id) {
			t.Fatalf("the Tier A diff must not move the in-flow box %q, got: %v", id, err)
		}
	}
}

// TestWrongAbsoluteFailsThePixels: the moved boxes paint elsewhere
// (fixed-a scrolled under the scroller's clip, fixed-b on the ancestor,
// fixed-c off the window), so Tier C fails with hot pixels.
func TestWrongAbsoluteFailsThePixels(t *testing.T) {
	fixture := fixed.Fixture
	fixture.ViewWithWindow = fixed.WrongAbsolute
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
		t.Fatal("the absolute view must fail the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "hot pixel") {
		t.Fatalf("the Tier C diff must report the moved boxes' hot pixels, got: %v", err)
	}
}
