package gpui

// The measurement harness of browser-core ticket 59 (R59_MEASURE_OUT,
// the R58_MEASURE_OUT pattern): the frame-draw CPU cost of the
// automatic view reuse against the per-view opt-out baseline, on one
// test window of 192 fixed-cell panels (each a Flex+SizeFull view with
// a hoverable button, a label, a focusable field and a model read) —
// a partial-update frame (one panel notified; the 191 others reuse or
// re-render) and a root re-render (the root notified; every panel
// reuses or re-renders). The two sides are measured INTERLEAVED (one
// pair per step: the reuse window's frame, then the opt-out window's
// frame of the same kind), so every pair sees the same machine load
// and the paired savings mean is load-immune.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// vr59Timing is one scenario's timing sample.
type vr59Timing struct {
	N      int     `json:"n"`
	MeanMS float64 `json:"mean_ms"`
	MinMS  float64 `json:"min_ms"`
}

// vr59Scenario pairs the reuse run with its opt-out baseline.
type vr59Scenario struct {
	Desc       string     `json:"desc"`
	Reuse      vr59Timing `json:"reuse"`
	Baseline   vr59Timing `json:"baseline_opt_out"`
	SavingsMS  float64    `json:"paired_draw_savings_ms_mean"`
	SavingsMin float64    `json:"paired_draw_savings_ms_min"`
	SavingsPct float64    `json:"draw_savings_percent"`
	// EndToEnd is the notify-plus-flush-draw pair (the frame loop's
	// whole path).
	EndToEnd           vr59Timing `json:"end_to_end_notify_and_flush"`
	EndToEndBaseline   vr59Timing `json:"end_to_end_baseline_opt_out"`
	EndToEndSavingsMS  float64    `json:"end_to_end_savings_ms_mean"`
	EndToEndSavingsPct float64    `json:"end_to_end_savings_percent"`
	ReuseHits          int        `json:"reuse_hits"`
	Renders            int        `json:"fresh_renders"`
}

// vr59Doc is the whole measurement record.
type vr59Doc struct {
	Schema  string         `json:"schema"`
	Date    string         `json:"date"`
	Window  [2]int         `json:"window_px"`
	Note    string         `json:"note"`
	Measure map[string]any `json:"measurements"`
}

// TestR59MeasureViewReuse writes the ticket's frame-cost measurements
// to the evidence JSON named by R59_MEASURE_OUT. Skipped when the env
// is unset.
func TestR59MeasureViewReuse(t *testing.T) {
	out := os.Getenv("R59_MEASURE_OUT")
	if out == "" {
		t.Skip("set R59_MEASURE_OUT to write the ticket 59 view-reuse measurements")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join("..", out)
	}

	ta := NewTestApp()
	app := ta.App()
	reuse := newVRMeasureWindow(t, ta, app, false)
	optOut := newVRMeasureWindow(t, ta, app, true)

	// One pair, measured twice: the direct frame draw (a notify without
	// parking, then DrawWindowFrame of the DIRTY window — which keeps
	// the reuse; only a clean forced draw re-renders — isolating the
	// frame build) and the end-to-end frame request (the notify plus
	// the flush draw, the frame loop's whole path).
	pair := func(step func()) (directReuse, directBase, e2eReuse, e2eBase float64) {
		step()
		d := vr59DrawTime(func() {
			if _, err := DrawWindowFrame(reuse.window); err != nil {
				t.Fatal(err)
			}
		})
		f := vr59DrawTime(func() {
			if _, err := DrawWindowFrame(optOut.window); err != nil {
				t.Fatal(err)
			}
		})
		ed := vr59DrawTime(func() { reuse.notify(ta) })
		ef := vr59DrawTime(func() { optOut.notify(ta) })
		return d, f, ed, ef
	}

	measure := map[string]any{}
	const warmup, n = 10, 60
	scenarios := []struct {
		desc string
		step func()
	}{
		{
			desc: "partial update (one panel notified, the rest reuse)",
			step: func() {
				revision := time.Now().UnixNano()
				reuse.panels[95].Update(app, func(p *vrPanel, cx *Context[vrPanel]) {
					p.label = fmt.Sprintf("panel 95 rev %d", revision)
					cx.Notify()
				})
				optOut.panels[95].Update(app, func(p *vrPanel, cx *Context[vrPanel]) {
					p.label = fmt.Sprintf("panel 95 rev %d", revision)
					cx.Notify()
				})
			},
		},
		{
			desc: "root re-render (the root notified, every panel unchanged)",
			step: func() {
				reuse.root.Update(app, func(r *vrRoot, cx *Context[vrRoot]) { cx.Notify() })
				optOut.root.Update(app, func(r *vrRoot, cx *Context[vrRoot]) { cx.Notify() })
			},
		},
	}
	for _, scenario := range scenarios {
		// Warmup pairs.
		for i := 0; i < warmup; i++ {
			pair(scenario.step)
		}
		var dBest, dTotal, fBest, fTotal, deltaBest, deltaTotal = 1e9, 0.0, 1e9, 0.0, 1e9, 0.0
		var edBest, edTotal, efBest, efTotal, edeltaBest, edeltaTotal = 1e9, 0.0, 1e9, 0.0, 1e9, 0.0
		for i := 0; i < n; i++ {
			var d, f, ed, ef float64
			if i%2 == 0 {
				d, f, ed, ef = pair(scenario.step)
			} else {
				// The pair's order alternates so neither side always
				// runs second (the warmed caches of a just-drawn frame).
				f, d, ef, ed = pair(scenario.step)
			}
			dBest, dTotal = min(dBest, d), dTotal+d
			fBest, fTotal = min(fBest, f), fTotal+f
			delta := f - d
			deltaBest, deltaTotal = min(deltaBest, delta), deltaTotal+delta
			edBest, edTotal = min(edBest, ed), edTotal+ed
			efBest, efTotal = min(efBest, ef), efTotal+ef
			edelta := ef - ed
			edeltaBest, edeltaTotal = min(edeltaBest, edelta), edeltaTotal+edelta
		}
		savings := deltaTotal / float64(n)
		pct := 0.0
		if fTotal > 0 {
			pct = savings / (fTotal / float64(n)) * 100
		}
		e2eSavings := edeltaTotal / float64(n)
		e2ePct := 0.0
		if efTotal > 0 {
			e2ePct = e2eSavings / (efTotal / float64(n)) * 100
		}
		measure[scenario.desc] = vr59Scenario{
			Desc:       scenario.desc,
			Reuse:      vr59Timing{N: n, MeanMS: dTotal / float64(n), MinMS: dBest},
			Baseline:   vr59Timing{N: n, MeanMS: fTotal / float64(n), MinMS: fBest},
			SavingsMS:  savings,
			SavingsMin: deltaBest,
			SavingsPct: pct,
			EndToEnd: vr59Timing{
				N:      n,
				MeanMS: edTotal / float64(n),
				MinMS:  edBest,
			},
			EndToEndBaseline: vr59Timing{
				N:      n,
				MeanMS: efTotal / float64(n),
				MinMS:  efBest,
			},
			EndToEndSavingsMS:  e2eSavings,
			EndToEndSavingsPct: e2ePct,
			ReuseHits:          int(drawState(reuse.window).viewReuseHits),
			Renders:            int(drawState(reuse.window).viewReuseMisses),
		}
	}

	doc := vr59Doc{
		Schema:  "browser-core/ticket59-view-reuse",
		Date:    time.Now().UTC().Format(time.RFC3339),
		Window:  [2]int{800, 600},
		Note:    "One test window of 192 fixed-cell panel views (a hoverable button, a label, a focusable field, a model read per panel), the reuse side against the per-view opt-out baseline, interleaved pairs. The draw is the frame loop's flush draw of the notified window. The machine is heavily shared; the paired mean is the comparable statistic.",
		Measure: measure,
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("creating the evidence directory: %v", err)
	}
	if err := os.WriteFile(out, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", out, err)
	}
}

// vr59DrawTime times one frame draw (the flush draw of the notified
// window) in milliseconds.
func vr59DrawTime(draw func()) float64 {
	start := time.Now()
	draw()
	return float64(time.Since(start)) / float64(time.Millisecond)
}

// vrMeasureWindow is the measurement twin of vrWindow: the panels'
// labels rotate per step so the partial updates change real content.
type vrMeasureWindow struct {
	window *Window
	root   Entity[vrRoot]
	panels []Entity[vrPanel]
	ta     *TestApp
}

func newVRMeasureWindow(t *testing.T, ta *TestApp, app *App, optOut bool) *vrMeasureWindow {
	t.Helper()
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	side := &vrMeasureWindow{window: window, ta: ta}
	counter := NewEntity(app, window.Scope(), func(c *vrCounter, _ *Context[vrCounter]) {
		c.light = 0.4
	})
	for i := 0; i < 192; i++ {
		focus := app.FocusHandle()
		side.panels = append(side.panels, NewEntity(app, window.Scope(), func(p *vrPanel, _ *Context[vrPanel]) {
			p.index, p.label, p.focus, p.counter = i, fmt.Sprintf("panel %d", i), focus, counter
		}))
	}
	side.root = NewEntity(app, window.Scope(), func(r *vrRoot, _ *Context[vrRoot]) {
		r.panels, r.counter, r.optOut = side.panels, counter, optOut
	})
	window.SetRootView(ViewOf(side.root))
	ta.RunUntilParked()
	return side
}

// notify dirties the window (the root) and waits for the flush draw.
func (s *vrMeasureWindow) notify(ta *TestApp) {
	s.root.Update(ta.App(), func(r *vrRoot, cx *Context[vrRoot]) { cx.Notify() })
	ta.RunUntilParked()
}
