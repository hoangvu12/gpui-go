//go:build windows

// The textwrap fixtures' discrimination tests (the tracer/position
// pattern, fixtures/tracer/tracer_test.go): the deliberately-wrong
// views must FAIL the comparison their difference lives in — the
// breaking-class fixtures at Tier A (the rows' heights are their line
// counts, and the wrong views keep the pre-ticket counts), the balance
// and justify fixtures at Tier C (their line counts stay, so only the
// pixels move) — with readable diffs naming the rows.
package textwrap_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/textwrap"
)

// wrongViewFailsBoxes checks that the wrong view fails the Tier A box
// comparison naming the given rows.
func wrongViewFailsBoxes(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement, rows []string) {
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
	err = parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the wrong view passed the Tier A box comparison")
	}
	for _, id := range rows {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
}

// wrongViewFailsPixels checks that the wrong view fails the Tier C
// pixel comparison (the fixture's own tolerance).
func wrongViewFailsPixels(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement) {
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
	if err := parity.ComparePixels(record, replay, fixture.Tolerance); err == nil {
		t.Fatal("the wrong view passed the Tier C pixel comparison")
	}
	t.Logf("Tier C fails as it should:\n%v", err)
}

// TestWrongURLClassesFailTheBoxComparison: the breaking classes dropped
// — every emergency/break-all row keeps the one overflow line.
func TestWrongURLClassesFailTheBoxComparison(t *testing.T) {
	wrongViewFailsBoxes(t, textwrap.URLFixture, textwrap.WrongURLView, []string{
		"row-url-anywhere", "span-url-anywhere",
		"row-url-break-word", "span-url-break-word",
	})
	// The break-all row's height matches normal (both fill two rows);
	// its discrimination is Tier C — the filled rows' glyphs.
	wrongViewFailsPixels(t, textwrap.URLFixture, textwrap.WrongURLView)
}

// TestWrongCJKKeepAllFailsTheBoxComparison: keep-all dropped — the CJK
// run wraps at the ideograph boundaries.
func TestWrongCJKKeepAllFailsTheBoxComparison(t *testing.T) {
	wrongViewFailsBoxes(t, textwrap.CJKFixture, textwrap.WrongCJKView, []string{
		"row-cjk-keepall", "span-cjk-keepall",
	})
}

// TestWrongBalanceFailsThePixelComparison: text-wrap: balance dropped —
// the greedy rows.
func TestWrongBalanceFailsThePixelComparison(t *testing.T) {
	wrongViewFailsPixels(t, textwrap.BalanceFixture, textwrap.WrongBalanceView)
}

// TestWrongJustifyFailsThePixelComparison: justify dropped — the
// unstretched word spacing.
func TestWrongJustifyFailsThePixelComparison(t *testing.T) {
	wrongViewFailsPixels(t, textwrap.JustifyFixture, textwrap.WrongJustifyView)
}
