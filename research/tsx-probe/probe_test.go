package tsxprobe

// Measurements for research/browser-parity/09. Run with
//
//	CGO_ENABLED=0 go test -run TestReport -count=1 -v .
//
// Everything is headless: hostless test windows on the real text system,
// no visible window.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
	"github.com/hoangvu12/gpui-go/gpui"
)

//go:embed view.tsx
var viewSrc string

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

func timeN(n int, f func()) (first, med time.Duration) {
	ds := make([]time.Duration, n)
	for i := range ds {
		start := Now()
		f()
		ds[i] = Since(start)
	}
	return ds[0], median(ds)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func mustLoad(t testing.TB, h *Host, src string) {
	t.Helper()
	if err := h.Load(src, "view.tsx"); err != nil {
		t.Fatal(err)
	}
}

// scriptView renders the host's tree; nativeView the plain-Go tree.
type scriptView struct{ h *Host }

func (v *scriptView) Render(w *gpui.Window, cx *gpui.Context[scriptView]) gpui.AnyElement {
	return v.h.Lower().IntoElement()
}

type nativeView struct{ s *Native }

func (v *nativeView) Render(w *gpui.Window, cx *gpui.Context[nativeView]) gpui.AnyElement {
	return v.s.Build().IntoElement()
}

// openFrames opens a hostless window on the real text system and returns
// a step that draws one changed frame (root notified, every element
// rebuilt, laid out and painted) and returns its time.
func openFrames[S any, PS interface {
	*S
	gpui.Render[S]
}](t *testing.T, build func(*S)) (step func() time.Duration, counts func() [2]int) {
	ta := gpui.NewTestApp()
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 1200, Height: 900})
	t.Cleanup(func() {
		gpui.CloseTestWindow(window)
		ta.App().Shutdown()
	})
	if err := gpui.UseRealTextSystem(window); err != nil {
		t.Skipf("real text system unavailable: %v", err)
	}
	var root gpui.Entity[S]
	ta.Update(func(app *gpui.App) {
		root = gpui.NewEntity(app, window.Scope(), func(s *S, _ *gpui.Context[S]) { build(s) })
		window.SetRootView(gpui.ViewOf[S, PS](root))
	})
	window.Refresh()
	window.SimulateFrameRequest()
	counts = func() [2]int {
		q, mono, sub, poly, err := gpui.WindowPrimitiveCounts(window)
		if err != nil {
			t.Fatal(err)
		}
		return [2]int{q, mono + sub + poly}
	}
	return func() time.Duration {
		// The update's flush draws the dirty test window itself; the
		// frame request draws when it does not. One frame either way.
		before := gpui.WindowDrawCount(window)
		start := Now()
		ta.Update(func(app *gpui.App) {
			root.Update(app, func(_ *S, cx *gpui.Context[S]) { cx.Notify() })
		})
		if gpui.WindowDrawCount(window) == before {
			window.SimulateFrameRequest()
		}
		d := Since(start)
		if gpui.WindowDrawCount(window) != before+1 {
			t.Fatal("frame not drawn")
		}
		return d
	}, counts
}

// paired alternates a and b n times (so load on the PC hits both alike)
// and returns each one's median and the median of the paired differences.
func paired(n int, a, b func() time.Duration) (ma, mb, diff time.Duration) {
	as, bs, ds := make([]time.Duration, n), make([]time.Duration, n), make([]time.Duration, n)
	for i := range n {
		as[i], bs[i] = a(), b()
		ds[i] = as[i] - bs[i]
	}
	return median(as), median(bs), median(ds)
}

func timed(f func()) func() time.Duration {
	return func() time.Duration {
		start := Now()
		f()
		return Since(start)
	}
}

func heap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestReport(t *testing.T) {
	r := map[string]any{}
	r["go"] = runtime.Version()
	r["cpu_count"] = runtime.NumCPU()

	// esbuild transform.
	first, med := timeN(30, func() {
		if _, err := Transform(viewSrc, "view.tsx", api.ES2017); err != nil {
			t.Fatal(err)
		}
	})
	r["transform_first_ms"], r["transform_median_ms"] = ms(first), ms(med)

	// VM startup (goja.New + host globals).
	h := NewHost()
	first, med = timeN(30, func() { h.install(goja.New()) })
	r["vm_start_first_ms"], r["vm_start_median_ms"] = ms(first), ms(med)

	// A bare VM with the host globals: retained heap.
	b0 := heap()
	vms := make([]*goja.Runtime, 10)
	for i := range vms {
		vms[i] = goja.New()
		h.install(vms[i])
	}
	r["heap_per_bare_vm_kib"] = (heap() - b0) / 1024 / 10
	runtime.KeepAlive(vms)

	// First evaluation: a cold host's first Load.
	before := heap()
	h = NewHost()
	start := Now()
	mustLoad(t, h, viewSrc)
	r["first_load_ms"] = ms(Since(start))
	r["first_load_parts_ms"] = map[string]float64{
		"transform": ms(h.Last.Transform), "vm_start": ms(h.Last.VMStart),
		"compile": ms(h.Last.Compile), "run": ms(h.Last.Run), "describe": ms(h.Last.Describe),
	}
	r["nodes"] = h.Count()
	r["heap_after_load_kib"] = (heap() - before) / 1024

	// Events. The bound path: the counter's click recomputes one binding.
	counter := h.Find("counter")
	first, med = timeN(1000, func() {
		if err := h.Dispatch(counter.OnClick); err != nil {
			t.Fatal(err)
		}
	})
	r["event_bound_first_ms"], r["event_bound_median_ms"] = ms(first), ms(med)
	if got := h.Get("clicks"); fmt.Sprint(got) != "1000" {
		t.Fatalf("clicks = %v", got)
	}
	if txt := counter.Children[0].Children[0].Text; txt != "Clicked 1000 times" {
		t.Fatalf("bound text = %q", txt)
	}
	// The re-describe path: a session row's click sets a useState key, App
	// runs again and the whole tree is described again.
	i := 0
	first, med = timeN(200, func() {
		i++
		row := h.Find(fmt.Sprintf("row-%d", i%200))
		if err := h.Dispatch(row.OnClick); err != nil {
			t.Fatal(err)
		}
	})
	r["event_redescribe_first_ms"], r["event_redescribe_median_ms"] = ms(first), ms(med)
	if got := h.Get("selected"); fmt.Sprint(got) != fmt.Sprint(i%200) {
		t.Fatalf("selected = %v, want %d", got, i%200)
	}

	// Reload after a simulated file change; state must survive.
	h.Find("agent-p3")
	if err := h.Dispatch(h.Find("agent-p3").OnClick); err != nil {
		t.Fatal(err)
	}
	var loads []Timings
	n := 0
	first, med = timeN(30, func() {
		n++
		edited := strings.Replace(viewSrc, `>Agents<`, fmt.Sprintf(">Agents (edit %d)<", n), 1)
		mustLoad(t, h, edited)
		loads = append(loads, h.Last)
	})
	r["reload_first_ms"], r["reload_median_ms"] = ms(first), ms(med)
	mid := loads[len(loads)/2]
	r["reload_parts_sample_ms"] = map[string]float64{
		"transform": ms(mid.Transform), "vm_start": ms(mid.VMStart),
		"compile": ms(mid.Compile), "run": ms(mid.Run), "describe": ms(mid.Describe),
	}
	if h.Get("current") != "p3" || fmt.Sprint(h.Get("clicks")) != "1000" {
		t.Fatalf("state lost on reload: current=%v clicks=%v", h.Get("current"), h.Get("clicks"))
	}
	if txt := h.Find("counter").Children[0].Children[0].Text; txt != "Clicked 1000 times" {
		t.Fatalf("bound text after reload = %q", txt)
	}
	r["state_kept_on_reload"] = true
	r["heap_after_30_reloads_kib"] = (heap() - before) / 1024

	// A broken edit keeps the last good tree.
	if err := h.Load(strings.Replace(viewSrc, "<text>{`session", "<text>{`session ${", 1), "view.tsx"); err == nil {
		t.Fatal("syntax error not reported")
	} else {
		r["syntax_error_message"] = err.Error()
	}
	if h.Find("counter") == nil {
		t.Fatal("last good tree lost")
	}

	// Lowering only (no layout/paint): building gpui elements per frame.
	mustLoad(t, h, viewSrc) // back to the unedited view
	nat := &Native{Current: "p3", Selected: int(h.Get("selected").(int64)), Clicks: 1000}
	lowMed, natMed, buildDiff := paired(2000, timed(func() { _ = h.Lower() }), timed(func() { _ = nat.Build() }))
	r["build_elements_script_median_ms"], r["build_elements_native_median_ms"] = ms(lowMed), ms(natMed)
	r["build_elements_overhead_ms"] = ms(buildDiff)

	// Whole frames (build + layout + prepaint + paint), changed every frame.
	scriptFrame, scriptCounts := openFrames[scriptView](t, func(v *scriptView) { v.h = h })
	nativeFrame, nativeCounts := openFrames[nativeView](t, func(v *nativeView) { v.s = nat })
	fs, fn, frameDiff := paired(300, scriptFrame, nativeFrame)
	// The same scene both ways: quads and glyph sprites.
	if sc, nc := scriptCounts(), nativeCounts(); sc != nc {
		t.Fatalf("scenes differ: script quads/sprites %v, native %v", sc, nc)
	} else {
		r["scene_quads_sprites"] = sc
	}
	r["frame_script_median_ms"], r["frame_native_median_ms"] = ms(fs), ms(fn)
	r["frame_overhead_ms"] = ms(frameDiff)

	out, _ := json.MarshalIndent(r, "", "  ")
	t.Logf("%s", out)
	if path := os.Getenv("TSX_PROBE_OUT"); path != "" {
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
