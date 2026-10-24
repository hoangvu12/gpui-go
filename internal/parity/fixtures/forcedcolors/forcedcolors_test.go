//go:build windows

// The forcedcolors fixture's discrimination tests (the tracer's
// tracer_test.go pattern): the deliberately-wrong replays must make the
// Tier C pixel comparison FAIL with a readable diff while leaving the
// Tier A box comparison passing, so a failure always means the thing it
// says. The wrong views are the pass-off replay (the acceptance's
// second half: with no forced colours and no override the view renders
// its authored colours against a record that was forced) and the
// wrong-palette replay (the mapping must read the palette it is given,
// not a hardcoded one).
package forcedcolors_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/forcedcolors"
)

// run replays the forcedcolors fixture with the given configuration
// override and returns the record and the replay. A non-nil configure
// replaces the fixture's own; configureForcedOff is the no-op that
// leaves the hostless app with no override at all (the pass off).
func run(t *testing.T, configure func(app *gpui.App) error) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := forcedcolors.Fixture
	if configure != nil {
		fixture.Configure = configure
	}
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

// configureForcedOff configures nothing: the hostless app keeps its
// default preference record (no forced colours, no override), which is
// the acceptance's pass-off state.
func configureForcedOff(*gpui.App) error { return nil }

// TestPassOffFailsOnlyThePixelComparison checks the discrimination of
// the acceptance's second half: with the pass off (no override, a
// hostless app reporting no forced colours) the authored colours and
// the shadow render against a record whose colours were forced, so
// Tier C fails while the boxes (Tier A) are unchanged.
func TestPassOffFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, configureForcedOff)
	err := parity.ComparePixels(record, replay, forcedcolors.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the pass-off view (authored colours against a forced record) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("turning the pass off must not move any box: %v", err)
	}
}

// TestWrongPaletteFailsOnlyThePixelComparison checks that the mapping
// reads the palette it is given: the recorded palette with Canvas and
// CanvasText swapped fails Tier C (every mapped colour renders
// inverted), so a pass that hardcoded the classic white-canvas/
// black-text pair could not pass this either.
func TestWrongPaletteFailsOnlyThePixelComparison(t *testing.T) {
	palette, err := recordedPalette()
	if err != nil {
		t.Fatal(err)
	}
	palette.Canvas, palette.CanvasText = palette.CanvasText, palette.Canvas
	record, replay := run(t, forcedcolors.ConfigureWithPalette(palette))
	err = parity.ComparePixels(record, replay, forcedcolors.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the swapped-palette view (Canvas and CanvasText inverted) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("swapping the palette must not move any box: %v", err)
	}
}

// recordedPalette reads the palette the record was rendered with
// (through the fixture's own Configure machinery).
func recordedPalette() (gpui.SystemColors, error) {
	record, err := parity.LoadRecord(forcedcolors.Fixture.Name)
	if err != nil {
		return gpui.SystemColors{}, err
	}
	return forcedcolors.SystemColorsOfRecord(record)
}

// The correct view's end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers), and its tolerance headroom is TestPixelTolerancesKeepHeadroom
// in internal/parity/measure_test.go.
