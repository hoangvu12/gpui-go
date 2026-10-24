//go:build windows

package parity_test

import (
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/activation"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/anchor"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/anchoring"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/boxpaint"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/capture"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/contextmenu"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/display"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/editkeys"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/filters"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/forcedcolors"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/gradient"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/hover"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/imecompose"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/keycode"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/keyorder"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/mincontent"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/outline"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/popover"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/position"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/scrollrange"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/selection"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/shadow"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/sticky"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/tabnav"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/textpaint"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/textstyle"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tracer"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/transition"
	_ "github.com/hoangvu12/gpui-go/internal/parity/fixtures/whitespace"
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
			if f.SkipPixels != "" {
				t.Logf("Tier C skipped (a documented blocker): %s", f.SkipPixels)
				return
			}
			if err := parity.ComparePixels(record, replay, f.Tolerance); err != nil {
				t.Errorf("%v", err)
			}
		})
	}
}

// The tracer's own discriminator: the deliberately-wrong views are in
// tracer_test.go (package tracer_test), next to the fixture; the
// keyorder fixtures' discriminators are in keyorder_test.go.
var _ = tracer.Fixture.Name
var _ = keyorder.Fixture.Name

// The fixture packages register through the imports above (tracer is
// ticket 01's, textstyle is ticket 06's); the wrong-view discrimination
// tests live beside them (fixtures/tracer/tracer_test.go,
// fixtures/textstyle/textstyle_test.go).
var (
	_ = tracer.Fixture.Name
	_ = textstyle.TransformFixture.Name
)
