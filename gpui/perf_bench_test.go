package gpui_test

// gpui-perf ticket 01: repeatable headless frame-cost benchmarks
// (.scratch/gpui-perf/issues/01-frame-cost-benchmark.md). Each benchmark
// drives whole frames of a hostless test window that draws through the
// REAL text system (native shaping, glyph rasterization and the window's
// sprite atlas), and reports per frame:
//
//   - ns/op, B/op and allocs/op (testing.B, ReportAllocs);
//   - native-calls/frame: calls into the native artifact's service table
//     (internal/native CallCount);
//   - quads/frame and sprites/frame of the drawn scene (a sanity check that
//     the scene did not change between a baseline and a candidate).
//
// Scenes:
//
//   - Divs: 1,000 styled leaf divs in 10 child views; every frame changes
//     state (the root and every pane are notified), so every element is
//     rebuilt.
//   - Text: 300 distinct lines of repeated glyphs; every frame notifies the
//     root (same text, so shaping hits the line-layout cache).
//   - RefreshDivs / RefreshText: the same windows redrawn by Refresh with
//     no state change (the unchanged-frame redraw).
//
// Frames are delivered through the window's frame loop
// (SimulateFrameRequest), like a platform frame request.
//
// Run them with
//
//	CGO_ENABLED=0 go test ./gpui -run '^$' -bench BenchmarkFrame -benchmem
//
// and write the evidence JSON with
//
//	GPUI_PERF_OUT=evidence/perf/ticketNN-x.json GPUI_PERF_LABEL=<commit> \
//	  CGO_ENABLED=0 go test ./gpui -run TestPerfFrameReport -count=1

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

const (
	perfPanes        = 10
	perfDivsPerPane  = 100
	perfTextLines    = 300
	perfTextColumns  = 3
	perfColumnWidthP = 780
)

var perfWindowSize = gpui.Size{Width: 2400, Height: 1800}

// perfDivPane is one child view of the div scene: 100 styled leaf divs.
type perfDivPane struct {
	index int
	tick  int
}

func (p *perfDivPane) Render(w *gpui.Window, cx *gpui.Context[perfDivPane]) gpui.AnyElement {
	children := make([]any, 0, perfDivsPerPane)
	for i := 0; i < perfDivsPerPane; i++ {
		hue := uint32((p.index*perfDivsPerPane + i + p.tick) % 7)
		children = append(children, gpui.Div().
			Size(gpui.Px(18)).
			M1().
			Bg(gpui.Rgb(0x203040+hue*0x101010)).
			Border1().
			BorderColor(gpui.Rgb(0x808080+hue*0x050505)).
			RoundedSm())
	}
	return gpui.Div().Flex().FlexWrap().W(gpui.Px(220)).P1().Children(children...).IntoElement()
}

// perfDivRoot is the div scene's root: 10 panes in a wrapping row.
type perfDivRoot struct {
	panes []gpui.Entity[perfDivPane]
	tick  int
}

func (r *perfDivRoot) Render(w *gpui.Window, cx *gpui.Context[perfDivRoot]) gpui.AnyElement {
	children := make([]any, 0, len(r.panes))
	for _, pane := range r.panes {
		children = append(children, gpui.ViewOf[perfDivPane](pane))
	}
	return gpui.Div().SizeFull().Flex().FlexWrap().Gap1().Children(children...).IntoElement()
}

// perfTextRoot is the text scene: 300 distinct lines in three columns.
type perfTextRoot struct {
	lines []string
	tick  int
}

func (r *perfTextRoot) Render(w *gpui.Window, cx *gpui.Context[perfTextRoot]) gpui.AnyElement {
	perColumn := (len(r.lines) + perfTextColumns - 1) / perfTextColumns
	columns := make([]any, 0, perfTextColumns)
	for c := 0; c < perfTextColumns; c++ {
		rows := make([]any, 0, perColumn)
		for i := c * perColumn; i < (c+1)*perColumn && i < len(r.lines); i++ {
			rows = append(rows, gpui.Div().Child(r.lines[i]))
		}
		columns = append(columns, gpui.Div().Flex().FlexCol().W(gpui.Px(perfColumnWidthP)).TextXs().Children(rows...))
	}
	return gpui.Div().SizeFull().Flex().Children(columns...).IntoElement()
}

func perfTextLinesOf(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%03d The quick brown fox jumps over the lazy dog; pack my box with five dozen jugs.", i)
	}
	return lines
}

// perfWindow is one open benchmark window plus the state change that a
// "changed" frame applies before its frame request.
type perfWindow struct {
	ta     *gpui.TestApp
	window *gpui.Window
	change func(app *gpui.App)
}

// openPerfWindow opens a hostless test window on the real text system
// with the given root and draws its first frame.
func openPerfWindow(tb testing.TB, root func(app *gpui.App, w *gpui.Window) (gpui.View, func(*gpui.App))) *perfWindow {
	tb.Helper()
	ta := gpui.NewTestApp()
	window := gpui.NewTestWindow(ta.App(), perfWindowSize)
	tb.Cleanup(func() {
		gpui.CloseTestWindow(window)
		ta.App().Shutdown()
	})
	if err := gpui.UseRealTextSystem(window); err != nil {
		tb.Skipf("real text system unavailable: %v", err)
	}
	p := &perfWindow{ta: ta, window: window}
	ta.Update(func(app *gpui.App) {
		var view gpui.View
		view, p.change = root(app, window)
		window.SetRootView(view)
	})
	window.Refresh()
	p.frame(tb)
	return p
}

func openPerfDivs(tb testing.TB) *perfWindow {
	return openPerfWindow(tb, func(app *gpui.App, w *gpui.Window) (gpui.View, func(*gpui.App)) {
		root := gpui.NewEntity(app, w.Scope(), func(r *perfDivRoot, cx *gpui.Context[perfDivRoot]) {
			for i := 0; i < perfPanes; i++ {
				index := i
				r.panes = append(r.panes, gpui.NewEntity(app, w.Scope(), func(p *perfDivPane, _ *gpui.Context[perfDivPane]) {
					p.index = index
				}))
			}
		})
		change := func(app *gpui.App) {
			root.Update(app, func(r *perfDivRoot, cx *gpui.Context[perfDivRoot]) {
				r.tick++
				for _, pane := range r.panes {
					pane.Update(app, func(p *perfDivPane, cx *gpui.Context[perfDivPane]) {
						p.tick++
						cx.Notify()
					})
				}
				cx.Notify()
			})
		}
		return gpui.ViewOf[perfDivRoot](root), change
	})
}

func openPerfText(tb testing.TB) *perfWindow {
	return openPerfWindow(tb, func(app *gpui.App, w *gpui.Window) (gpui.View, func(*gpui.App)) {
		root := gpui.NewEntity(app, w.Scope(), func(r *perfTextRoot, _ *gpui.Context[perfTextRoot]) {
			r.lines = perfTextLinesOf(perfTextLines)
		})
		change := func(app *gpui.App) {
			root.Update(app, func(r *perfTextRoot, cx *gpui.Context[perfTextRoot]) {
				r.tick++
				cx.Notify()
			})
		}
		return gpui.ViewOf[perfTextRoot](root), change
	})
}

// frame delivers one frame request and checks that exactly one frame was
// drawn.
func (p *perfWindow) frame(tb testing.TB) {
	before := gpui.WindowDrawCount(p.window)
	p.window.SimulateFrameRequest()
	if after := gpui.WindowDrawCount(p.window); after != before+1 {
		tb.Fatalf("frame request drew %d frames, want 1", after-before)
	}
}

// changedFrame applies the scene's state change, then draws. The change
// runs inside an update whose flush would draw the dirty test window
// itself, so the frame request below is what draws when the flush does
// not; either way exactly one frame is drawn per call.
func (p *perfWindow) changedFrame(tb testing.TB) {
	before := gpui.WindowDrawCount(p.window)
	p.ta.Update(p.change)
	if gpui.WindowDrawCount(p.window) == before {
		p.window.SimulateFrameRequest()
	}
	if after := gpui.WindowDrawCount(p.window); after != before+1 {
		tb.Fatalf("changed frame drew %d frames, want 1", after-before)
	}
}

// refreshFrame is the unchanged-frame redraw: Refresh, then one frame
// request.
func (p *perfWindow) refreshFrame(tb testing.TB) {
	p.window.Refresh()
	p.frame(tb)
}

// sceneCounts reports the last drawn scene's quads and sprites.
func (p *perfWindow) sceneCounts(tb testing.TB) (quads, sprites int) {
	q, mono, sub, poly, err := gpui.WindowPrimitiveCounts(p.window)
	if err != nil {
		tb.Fatalf("primitive counts: %v", err)
	}
	return q, mono + sub + poly
}

func runFrameBenchmark(b *testing.B, open func(testing.TB) *perfWindow, refresh bool) {
	p := open(b)
	step := p.changedFrame
	if refresh {
		step = p.refreshFrame
	}
	// One warm frame of the measured kind (caches settle).
	step(b)
	quads, sprites := p.sceneCounts(b)
	b.ReportAllocs()
	runtime.GC()
	calls := native.CallCount()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		step(b)
	}
	b.StopTimer()
	b.ReportMetric(float64(native.CallCount()-calls)/float64(b.N), "native-calls/frame")
	b.ReportMetric(float64(quads), "quads/frame")
	b.ReportMetric(float64(sprites), "sprites/frame")
}

// BenchmarkFrameDivs: (a) 1,000 styled divs, every element rebuilt.
func BenchmarkFrameDivs(b *testing.B) { runFrameBenchmark(b, openPerfDivs, false) }

// BenchmarkFrameText: (b) 300 distinct lines of repeated glyphs.
func BenchmarkFrameText(b *testing.B) { runFrameBenchmark(b, openPerfText, false) }

// BenchmarkFrameRefreshDivs: (c) the 1,000-div window redrawn by Refresh
// with no state change.
func BenchmarkFrameRefreshDivs(b *testing.B) { runFrameBenchmark(b, openPerfDivs, true) }

// BenchmarkFrameRefreshText: (c) the text window redrawn by Refresh with
// no state change.
func BenchmarkFrameRefreshText(b *testing.B) { runFrameBenchmark(b, openPerfText, true) }

// TestPerfScenesDraw keeps the benchmark scenes honest in the ordinary
// test run: both draw, the div scene paints its 1,000 leaf quads and the
// text scene paints glyph sprites.
func TestPerfScenesDraw(t *testing.T) {
	divs := openPerfDivs(t)
	divs.changedFrame(t)
	divs.refreshFrame(t)
	if quads, _ := divs.sceneCounts(t); quads < perfPanes*perfDivsPerPane {
		t.Errorf("div scene painted %d quads, want at least %d", quads, perfPanes*perfDivsPerPane)
	}
	text := openPerfText(t)
	text.changedFrame(t)
	text.refreshFrame(t)
	if _, sprites := text.sceneCounts(t); sprites < perfTextLines*20 {
		t.Errorf("text scene painted %d glyph sprites, want at least %d", sprites, perfTextLines*20)
	}
}

// perfResult is one benchmark's evidence row.
type perfResult struct {
	Name             string  `json:"name"`
	N                int     `json:"n"`
	NsPerFrame       float64 `json:"ns_per_frame"`
	MsPerFrame       float64 `json:"ms_per_frame"`
	AllocsPerFrame   int64   `json:"allocs_per_frame"`
	BytesPerFrame    int64   `json:"bytes_per_frame"`
	NativeCallsFrame float64 `json:"native_calls_per_frame"`
	QuadsPerFrame    float64 `json:"quads_per_frame"`
	SpritesPerFrame  float64 `json:"sprites_per_frame"`
}

// TestPerfFrameReport runs every frame benchmark (best of GPUI_PERF_RUNS,
// default 3, by ns/frame) and writes the evidence JSON named by
// GPUI_PERF_OUT. It is skipped when GPUI_PERF_OUT is unset.
func TestPerfFrameReport(t *testing.T) {
	out := os.Getenv("GPUI_PERF_OUT")
	if out == "" {
		t.Skip("set GPUI_PERF_OUT to write the frame-cost evidence")
	}
	if !filepath.IsAbs(out) {
		// Relative to the module root (go test runs in ./gpui).
		out = filepath.Join("..", out)
	}
	runs := 3
	if v := os.Getenv("GPUI_PERF_RUNS"); v != "" {
		fmt.Sscan(v, &runs)
	}
	benchmarks := []struct {
		name string
		f    func(*testing.B)
	}{
		{"FrameDivs", BenchmarkFrameDivs},
		{"FrameText", BenchmarkFrameText},
		{"FrameRefreshDivs", BenchmarkFrameRefreshDivs},
		{"FrameRefreshText", BenchmarkFrameRefreshText},
	}
	var results []perfResult
	for _, bm := range benchmarks {
		var best *perfResult
		for r := 0; r < runs; r++ {
			res := testing.Benchmark(bm.f)
			if res.N == 0 {
				t.Fatalf("%s did not run", bm.name)
			}
			row := perfResult{
				Name:             bm.name,
				N:                res.N,
				NsPerFrame:       float64(res.T.Nanoseconds()) / float64(res.N),
				AllocsPerFrame:   res.AllocsPerOp(),
				BytesPerFrame:    res.AllocedBytesPerOp(),
				NativeCallsFrame: res.Extra["native-calls/frame"],
				QuadsPerFrame:    res.Extra["quads/frame"],
				SpritesPerFrame:  res.Extra["sprites/frame"],
			}
			row.MsPerFrame = row.NsPerFrame / 1e6
			if best == nil || row.NsPerFrame < best.NsPerFrame {
				best = &row
			}
		}
		t.Logf("%-18s %8.3f ms/frame %8d allocs %10d B %8.0f native calls  quads %v sprites %v",
			best.Name, best.MsPerFrame, best.AllocsPerFrame, best.BytesPerFrame, best.NativeCallsFrame, best.QuadsPerFrame, best.SpritesPerFrame)
		results = append(results, *best)
	}
	doc := map[string]any{
		"label":     os.Getenv("GPUI_PERF_LABEL"),
		"date":      time.Now().UTC().Format(time.RFC3339),
		"go":        runtime.Version(),
		"goos":      runtime.GOOS,
		"goarch":    runtime.GOARCH,
		"cpus":      runtime.NumCPU(),
		"runs":      runs,
		"method":    "best of runs by ns/frame; testing.Benchmark over hostless test windows on the real text system; frames via SimulateFrameRequest (gpui/perf_bench_test.go). allocs, bytes and native calls are deterministic; ns/frame is wall time and noisy when other work shares the machine",
		"benchmark": results,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
