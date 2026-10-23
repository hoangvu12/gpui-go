//go:build windows

package parity_test

import (
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/boxpaint"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/textpaint"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tracer"
)

// TestFixtures replays every registered fixture and runs the three
// comparisons against the checked-in records: boxes exactly (Tier A),
// the event transcript exactly (Tier B) and pixels under the fixture's
// provisional tolerance (Tier C). Registering a fixture in a package
// imported here adds it to this run.
func TestFixtures(t *testing.T) {
	fixtures := parity.All()
	if len(fixtures) == 0 {
		t.Fatal("no registered fixtures (import the fixture packages)")
	}
	for _, f := range fixtures {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			record, err := parity.LoadRecord(f.Name)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := parity.Replay(t, f)
			if err != nil {
				t.Fatal(err)
			}
			if err := parity.CompareBoxes(record, replay); err != nil {
				t.Errorf("%v", err)
			}
			if err := parity.CompareEvents(record, replay); err != nil {
				t.Errorf("%v", err)
			}
			if err := parity.ComparePixels(record, replay, f.Tolerance); err != nil {
				t.Errorf("%v", err)
			}
		})
	}
}

// The tracer's own discriminator: the deliberately-wrong views are in
// tracer_test.go (package tracer_test), next to the fixture.
var _ = tracer.Fixture.Name
