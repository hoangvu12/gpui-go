//go:build windows

package tsxtracer_test

// The .tsx twin against the tracer's record (ticket 62's first
// acceptance): the TSX view loads into the headless window through the
// script host and matches the same Chrome page as the Go twin — boxes
// exactly (Tier A), the event transcript exactly (Tier B) and pixels
// under the tracer's tolerance (Tier C).

import (
	"os"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tsxtracer"
)

func TestTheTsxViewMatchesTheTracerRecord(t *testing.T) {
	record, err := parity.LoadRecord("tracer-box-click")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, tsxtracer.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("Tier A (boxes): %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("Tier B (transcript): %v", err)
	}
	if err := parity.ComparePixels(record, replay, tsxtracer.Fixture.Tolerance); err != nil {
		t.Errorf("Tier C (pixels): %v", err)
	}
	// The measured pixel statistics, for the record.
	whole, rois, err := parity.MeasurePixels(record, replay)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("pixels: whole mean %.4f, hot %.5f; box ROI mean %.4f (gates %.2f / %.4f / %.2f)",
		whole.Mean, whole.HotFraction, rois["box"].Mean,
		tsxtracer.Fixture.Tolerance.MaxMeanChannelDiff,
		tsxtracer.Fixture.Tolerance.MaxHotPixelFraction,
		tsxtracer.Fixture.Tolerance.MaxROIMeanChannelDiff)
}

// The discrimination test (the harness's rule that a comparison only
// means something if a real defect fails it): a wrong .tsx view must
// fail the box comparison, so the passing run above proves the .tsx
// path is really under the comparison. The edit is the same one the
// tracer's own discrimination test makes on its Go twin.
func TestAWrongTsxViewFailsTheBoxComparison(t *testing.T) {
	src, err := os.ReadFile("view.tsx")
	if err != nil {
		t.Fatalf("reading view.tsx: %v", err)
	}
	wrong := strings.Replace(string(src), `w-[200px]`, `w-[220px]`, 1)
	fixture := tsxtracer.Fixture
	fixture.View = sourceView(string(wrong))

	record, err := parity.LoadRecord("tracer-box-click")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err == nil {
		t.Fatal("the wrong-width .tsx view passed the Tier A box comparison")
	} else {
		t.Logf("Tier A fails as it should:\n%v", err)
	}
}

// sourceView builds a fixture view over a custom .tsx source, the same
// shape as the fixture's own view (one host per event log, lowered per
// frame).
func sourceView(src string) func(*parity.EventLog) gpui.AnyElement {
	var host *script.Host
	var bound *parity.EventLog
	return func(log *parity.EventLog) gpui.AnyElement {
		if host == nil || bound != log {
			bound = log
			h := script.New()
			h.SetGlobal("logEvent", func(kind, target string) { log.Append(kind, target) })
			bundle, err := compile.String("view.tsx", src)
			if err != nil {
				panic("tsxtracer: compiling view.tsx: " + err.Error())
			}
			if err := h.Load(bundle); err != nil {
				panic("tsxtracer: loading view.tsx: " + err.Error())
			}
			host = h
		}
		return host.Element()
	}
}
