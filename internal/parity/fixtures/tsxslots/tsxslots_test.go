//go:build windows

package tsxslots_test

// The plugin fixture's replay (ticket 67): the app view's slot, filled
// by a plugin through the script host's plugin registry, must match
// the recorded Chrome page — boxes exactly (Tier A), the event
// transcript exactly (Tier B, the plugin's box logging through the
// granted action) and pixels under the tracer's tolerance (Tier C).
// The discrimination test proves a real defect fails the comparison,
// and a second test proves the slot with no plugin falls back (the
// registry's healthy empty path through a real window).

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tsxslots"
)

func TestTheSlotFilledByAPluginMatchesTheRecordedPage(t *testing.T) {
	record, err := parity.LoadRecord("plugin-slot")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, tsxslots.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("Tier A (boxes): %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("Tier B (transcript): %v", err)
	}
	if err := parity.ComparePixels(record, replay, tsxslots.Fixture.Tolerance); err != nil {
		t.Errorf("Tier C (pixels): %v", err)
	}
	// The measured pixel statistics, for the record.
	whole, rois, err := parity.MeasurePixels(record, replay)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("pixels: whole mean %.4f, hot %.5f; slot ROI mean %.4f (gates %.2f / %.4f / %.2f)",
		whole.Mean, whole.HotFraction, rois["slot"].Mean,
		tsxslots.Fixture.Tolerance.MaxMeanChannelDiff,
		tsxslots.Fixture.Tolerance.MaxHotPixelFraction,
		tsxslots.Fixture.Tolerance.MaxROIMeanChannelDiff)
}

// The discrimination test (the harness's rule that a comparison only
// means something if a real defect fails it): a wrong plugin
// contribution — the box 20px wider, the edit the tracer's own
// discrimination test makes — must fail the box comparison, so the
// passing run above proves the plugin's tree is really under the
// comparison.
func TestAWrongPluginContributionFailsTheBoxComparison(t *testing.T) {
	record, err := parity.LoadRecord("plugin-slot")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, parity.Fixture{
		Name:       tsxslots.Fixture.Name,
		Window:     tsxslots.Fixture.Window,
		Steps:      tsxslots.Fixture.Steps,
		BoxIDs:     tsxslots.Fixture.BoxIDs,
		EventKinds: tsxslots.Fixture.EventKinds,
		View:       sourceView(`w-[220px]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err == nil {
		t.Fatal("the wrong plugin contribution passed the Tier A box comparison")
	} else {
		t.Logf("Tier A fails as it should:\n%v", err)
	}
}

// sourceView builds a fixture view whose plugin contribution is edited
// (the wrong-width discrimination and any future planted plugin
// defect) — the same shape as the fixture's own view: one host per
// event log, the plugin registered from the edited source.
func sourceView(wrongWidth string) func(*parity.EventLog) gpui.AnyElement {
	src := strings.Replace(inlinePluginSrc, `w-[200px]`, wrongWidth, 1)
	var host *script.Host
	var bound *parity.EventLog
	return func(log *parity.EventLog) gpui.AnyElement {
		if host == nil || bound != log {
			bound = log
			h := script.New()
			h.SetGlobal("logEvent", func(kind, target string) { log.Append(kind, target) })
			h.Action("log", func(args []any) (any, error) {
				log.Append(args[0].(string), args[1].(string))
				return nil, nil
			})
			plugin, err := compile.String("plugin.tsx", src)
			if err != nil {
				panic("tsxslots: compiling the edited plugin: " + err.Error())
			}
			if err := h.Slots().Add("greeting", plugin,
				script.WithPluginGrants(script.Grants{Actions: []string{"log"}})); err != nil {
				panic("tsxslots: adding the plugin: " + err.Error())
			}
			app, err := compile.String("view.tsx", inlineAppSrc)
			if err != nil {
				panic("tsxslots: compiling view.tsx: " + err.Error())
			}
			if err := h.Load(app); err != nil {
				panic("tsxslots: loading view.tsx: " + err.Error())
			}
			host = h
		}
		return host.Element()
	}
}

// inlineAppSrc and inlinePluginSrc are the fixture's own sources, read
// back from the embedded files (the test edits the plugin's copy; the
// app's is identical).
var inlineAppSrc, inlinePluginSrc = tsxslots.AppSource(), tsxslots.PluginSource()

// A slot with no plugin falls back to its own JSX children (the
// registry's healthy empty path): the fallback view runs through a
// real test window and its slot box sits where the plugin-filled
// record's does (the fallback child keeps the plugin box's size, so
// only the content differs).
func TestAnEmptySlotRendersItsFallbackInAWindow(t *testing.T) {
	app, err := compile.String("view.tsx", `
export function App() {
	return (
		<div
			id="root"
			className="flex items-center justify-center"
			style={{ width: 640, height: 400, background: "#ffffff" }}
		>
			<slot id="slot" name="main" style={{ display: "block", padding: 16, background: "#f1f5f9" }}>
				<div id="fallback" className="w-[200px] h-[80px]">no plugin</div>
			</slot>
		</div>
	);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	host := script.New()
	if err := host.Load(app); err != nil {
		t.Fatal(err)
	}
	if names := host.Slots().Names(); len(names) != 0 {
		t.Fatalf("an empty registry reports plugins: %v", names)
	}
	if got, ok := host.Text("fallback"); !ok || got != "no plugin" {
		t.Fatalf("the fallback = %q, ok=%v, want the slot's own children", got, ok)
	}
	// Windowed: the slot renders where the plugin-filled view's does
	// (the fallback child has the plugin box's size; the box's id is
	// the fallback's, so only the slot's box is compared to the
	// record — the geometry the empty registry must preserve).
	record, err := parity.LoadRecord("plugin-slot")
	if err != nil {
		t.Fatal(err)
	}
	windowed := parity.Fixture{
		Name:   tsxslots.Fixture.Name,
		Window: tsxslots.Fixture.Window,
		View: func(*parity.EventLog) gpui.AnyElement {
			return host.Element()
		},
		BoxIDs: []string{"slot", "root"},
	}
	replay, err := parity.Replay(t, windowed)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"slot", "root"} {
		if replay.Boxes[id] != record.Boxes[id] {
			t.Errorf("the %s box moved: got %v, want %v", id, replay.Boxes[id], record.Boxes[id])
		}
	}
}
