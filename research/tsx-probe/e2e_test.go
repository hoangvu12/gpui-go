package tsxprobe

// End to end: a simulated click goes through gpui's hit testing to the
// script handler, and the next frame shows the change.
//
//	CGO_ENABLED=0 go test -run TestEndToEndClick -count=1 -v .

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

func TestEndToEndClick(t *testing.T) {
	h := NewHost()
	h.Debug = true
	mustLoad(t, h, viewSrc)
	ta := gpui.NewTestApp()
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 1200, Height: 900})
	t.Cleanup(func() {
		gpui.CloseTestWindow(window)
		ta.App().Shutdown()
	})
	if err := gpui.UseRealTextSystem(window); err != nil {
		t.Skipf("real text system unavailable: %v", err)
	}
	h.Notify = window.Refresh
	ta.Update(func(app *gpui.App) {
		root := gpui.NewEntity(app, window.Scope(), func(v *scriptView, _ *gpui.Context[scriptView]) { v.h = h })
		window.SetRootView(gpui.ViewOf[scriptView](root))
	})
	window.Refresh()
	window.SimulateFrameRequest()
	cx := gpui.NewVisualTestContext(window, ta)

	center := func(sel string) gpui.Point {
		b, ok := gpui.WindowDebugBound(window, sel)
		if !ok {
			t.Fatalf("no bounds for %s", sel)
		}
		return gpui.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
	}
	// click simulates a click and draws the frame it asked for; it
	// returns the time from the press to the drawn frame.
	frames := 0
	click := func(sel string) time.Duration {
		p := center(sel)
		before := gpui.WindowDrawCount(window)
		start := Now()
		cx.SimulateClick(p, gpui.NoModifiers())
		if gpui.WindowDrawCount(window) == before {
			window.SimulateFrameRequest()
		}
		d := Since(start)
		if gpui.WindowDrawCount(window) == before {
			t.Fatalf("click on %s drew no frame", sel)
		}
		frames = int(gpui.WindowDrawCount(window) - before)
		return d
	}

	bound := make([]time.Duration, 100)
	for i := range bound {
		bound[i] = click("counter")
	}
	if got := h.Find("counter").Children[0].Children[0].Text; got != "Clicked 100 times" {
		t.Fatalf("counter text = %q", got)
	}
	re := make([]time.Duration, 50)
	for i := range re {
		re[i] = click("row-" + []string{"3", "4"}[i%2])
	}
	if h.Get("selected") != int64(4) {
		t.Fatalf("selected = %v", h.Get("selected"))
	}
	if !strings.Contains(h.Find("row-4").Class, "bg-blue-500/30") {
		t.Fatal("row 4 not highlighted after re-describe")
	}
	out, _ := json.MarshalIndent(map[string]any{
		"click_to_frame_bound_median_ms":      ms(median(bound)),
		"click_to_frame_redescribe_median_ms": ms(median(re)),
		"frames_drawn_per_click":              frames,
	}, "", "  ")
	t.Logf("%s", out)
	if path := os.Getenv("TSX_E2E_OUT"); path != "" {
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
