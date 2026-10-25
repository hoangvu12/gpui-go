package gpui_test

// gpui-perf ticket 04: frame cost with cached views
// (.scratch/gpui-perf/issues/04-view-caching.md), on ticket 01's harness
// (perf_bench_test.go). The scenes are ticket 01's 1,000 styled divs and
// 300 text lines split into pane views laid out in fixed cells, so each
// pane can be embedded cached (gpui.CachedView, the pinned
// AnyView::cached) or plain:
//
//   - RootNotify: only the root view is notified; the panes are unchanged
//     (an unchanged-content redraw: cached panes are reused).
//   - Partial: the root and one pane (rotating) change; the other panes
//     are reused.
//   - Refresh: Window.Refresh with no state change (ticket 01's scene (c));
//     a refresh bypasses every view cache (R:window.rs:2287-2292), so
//     caching does not change it.
//
// Run them with
//
//	CGO_ENABLED=0 go test ./gpui -run '^$' -bench BenchmarkViewCache -benchmem
//
// and write the evidence JSON with
//
//	GPUI_PERF_OUT=evidence/perf/x.json GPUI_PERF_LABEL=<commit> \
//	  CGO_ENABLED=0 go test ./gpui -run TestPerfViewCacheReport -count=1

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

const (
	perfTextPanes        = 15
	perfTextLinesPerPane = perfTextLines / perfTextPanes
)

// perfPaneChild embeds one pane view, cached or not, in a fixed cell.
func perfPaneChild(view gpui.View, cached bool, width, height float32) any {
	var child any = view
	if cached {
		child = gpui.CachedView(view, gpui.Styles().SizeFull())
	}
	return gpui.Div().Flex().W(gpui.Px(width)).H(gpui.Px(height)).Child(child)
}

// perfCachedDivRoot is ticket 01's div root with each pane in a fixed cell.
type perfCachedDivRoot struct {
	panes  []gpui.Entity[perfDivPane]
	cached bool
	tick   int
}

func (r *perfCachedDivRoot) Render(w *gpui.Window, cx *gpui.Context[perfCachedDivRoot]) gpui.AnyElement {
	children := make([]any, 0, len(r.panes))
	for _, pane := range r.panes {
		children = append(children, perfPaneChild(gpui.ViewOf[perfDivPane](pane), r.cached, 220, 350))
	}
	return gpui.Div().SizeFull().Flex().FlexWrap().Gap1().Children(children...).IntoElement()
}

// perfTextPane is 20 of ticket 01's 300 text lines.
type perfTextPane struct {
	lines []string
	tick  int
}

func (p *perfTextPane) Render(w *gpui.Window, cx *gpui.Context[perfTextPane]) gpui.AnyElement {
	rows := make([]any, 0, len(p.lines))
	for _, line := range p.lines {
		rows = append(rows, gpui.Div().Flex().Child(line))
	}
	return gpui.Div().Flex().FlexCol().W(gpui.Px(perfColumnWidthP)).TextXs().Children(rows...).IntoElement()
}

// perfCachedTextRoot lays the text panes out three per row.
type perfCachedTextRoot struct {
	panes  []gpui.Entity[perfTextPane]
	cached bool
	tick   int
}

func (r *perfCachedTextRoot) Render(w *gpui.Window, cx *gpui.Context[perfCachedTextRoot]) gpui.AnyElement {
	children := make([]any, 0, len(r.panes))
	for _, pane := range r.panes {
		children = append(children, perfPaneChild(gpui.ViewOf[perfTextPane](pane), r.cached, perfColumnWidthP, 340))
	}
	return gpui.Div().SizeFull().Flex().FlexWrap().Children(children...).IntoElement()
}

// perfChange selects what a changed frame notifies.
type perfChange int

const (
	perfChangeRootOnly perfChange = iota
	perfChangePartial
)

func openPerfCachedDivs(cached bool, change perfChange) func(testing.TB) *perfWindow {
	return func(tb testing.TB) *perfWindow {
		return openPerfWindow(tb, func(app *gpui.App, w *gpui.Window) (gpui.View, func(*gpui.App)) {
			root := gpui.NewEntity(app, w.Scope(), func(r *perfCachedDivRoot, _ *gpui.Context[perfCachedDivRoot]) {
				r.cached = cached
				for i := 0; i < perfPanes; i++ {
					index := i
					r.panes = append(r.panes, gpui.NewEntity(app, w.Scope(), func(p *perfDivPane, _ *gpui.Context[perfDivPane]) {
						p.index = index
					}))
				}
			})
			return gpui.ViewOf[perfCachedDivRoot](root), func(app *gpui.App) {
				root.Update(app, func(r *perfCachedDivRoot, cx *gpui.Context[perfCachedDivRoot]) {
					r.tick++
					if change == perfChangePartial {
						r.panes[r.tick%len(r.panes)].Update(app, func(p *perfDivPane, pcx *gpui.Context[perfDivPane]) {
							p.tick++
							pcx.Notify()
						})
					}
					cx.Notify()
				})
			}
		})
	}
}

func openPerfCachedText(cached bool, change perfChange) func(testing.TB) *perfWindow {
	return func(tb testing.TB) *perfWindow {
		return openPerfWindow(tb, func(app *gpui.App, w *gpui.Window) (gpui.View, func(*gpui.App)) {
			lines := perfTextLinesOf(perfTextLines)
			root := gpui.NewEntity(app, w.Scope(), func(r *perfCachedTextRoot, _ *gpui.Context[perfCachedTextRoot]) {
				r.cached = cached
				for i := 0; i < perfTextPanes; i++ {
					paneLines := lines[i*perfTextLinesPerPane : (i+1)*perfTextLinesPerPane]
					r.panes = append(r.panes, gpui.NewEntity(app, w.Scope(), func(p *perfTextPane, _ *gpui.Context[perfTextPane]) {
						p.lines = paneLines
					}))
				}
			})
			return gpui.ViewOf[perfCachedTextRoot](root), func(app *gpui.App) {
				root.Update(app, func(r *perfCachedTextRoot, cx *gpui.Context[perfCachedTextRoot]) {
					r.tick++
					if change == perfChangePartial {
						r.panes[r.tick%len(r.panes)].Update(app, func(p *perfTextPane, pcx *gpui.Context[perfTextPane]) {
							p.tick++
							pcx.Notify()
						})
					}
					cx.Notify()
				})
			}
		})
	}
}

// perfViewCacheBenchmarks are ticket 04's scenes, each plain and cached.
var perfViewCacheBenchmarks = []struct {
	name    string
	open    func(testing.TB) *perfWindow
	refresh bool
}{
	{"DivsRootNotify/plain", openPerfCachedDivs(false, perfChangeRootOnly), false},
	{"DivsRootNotify/cached", openPerfCachedDivs(true, perfChangeRootOnly), false},
	{"DivsPartial/plain", openPerfCachedDivs(false, perfChangePartial), false},
	{"DivsPartial/cached", openPerfCachedDivs(true, perfChangePartial), false},
	{"DivsRefresh/plain", openPerfCachedDivs(false, perfChangeRootOnly), true},
	{"DivsRefresh/cached", openPerfCachedDivs(true, perfChangeRootOnly), true},
	{"TextRootNotify/plain", openPerfCachedText(false, perfChangeRootOnly), false},
	{"TextRootNotify/cached", openPerfCachedText(true, perfChangeRootOnly), false},
	{"TextPartial/plain", openPerfCachedText(false, perfChangePartial), false},
	{"TextPartial/cached", openPerfCachedText(true, perfChangePartial), false},
	{"TextRefresh/plain", openPerfCachedText(false, perfChangeRootOnly), true},
	{"TextRefresh/cached", openPerfCachedText(true, perfChangeRootOnly), true},
}

func BenchmarkViewCache(b *testing.B) {
	for _, bm := range perfViewCacheBenchmarks {
		b.Run(bm.name, func(b *testing.B) { runFrameBenchmark(b, bm.open, bm.refresh) })
	}
}

// TestPerfViewCacheScenesMatch keeps the benchmark scenes honest in the
// ordinary test run: every cached scene paints exactly the primitives of
// its plain twin after a changed frame and after a refresh.
func TestPerfViewCacheScenesMatch(t *testing.T) {
	for i := 0; i < len(perfViewCacheBenchmarks); i += 2 {
		plain, cached := perfViewCacheBenchmarks[i], perfViewCacheBenchmarks[i+1]
		// One subtest per pair: its windows (and their native atlas slots,
		// bounded per process) close when it ends.
		t.Run(cached.name, func(t *testing.T) {
			p, c := plain.open(t), cached.open(t)
			for _, step := range []func(*perfWindow){
				func(w *perfWindow) { w.changedFrame(t) },
				func(w *perfWindow) { w.changedFrame(t) },
				func(w *perfWindow) { w.refreshFrame(t) },
			} {
				step(p)
				step(c)
				pq, ps := p.sceneCounts(t)
				cq, cs := c.sceneCounts(t)
				if pq != cq || ps != cs {
					t.Fatalf("cached scene paints %d quads %d sprites, plain %d and %d", cq, cs, pq, ps)
				}
			}
		})
	}
}

// TestPerfViewCacheReport runs every ticket 04 benchmark (best of
// GPUI_PERF_RUNS, default 3, by ns/frame) and writes the evidence JSON
// named by GPUI_PERF_OUT. It is skipped when GPUI_PERF_OUT is unset.
func TestPerfViewCacheReport(t *testing.T) {
	out := os.Getenv("GPUI_PERF_OUT")
	if out == "" {
		t.Skip("set GPUI_PERF_OUT to write the view-cache frame-cost evidence")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join("..", out)
	}
	runs := 3
	if v := os.Getenv("GPUI_PERF_RUNS"); v != "" {
		fmt.Sscan(v, &runs)
	}
	benchmarks := perfViewCacheBenchmarks
	extra := []struct {
		name    string
		open    func(testing.TB) *perfWindow
		refresh bool
	}{
		{"FrameRefreshDivs", openPerfDivs, true},
		{"FrameRefreshText", openPerfText, true},
	}
	benchmarks = append(benchmarks, extra...)
	var results []perfResult
	for _, bm := range benchmarks {
		var best *perfResult
		for r := 0; r < runs; r++ {
			res := testing.Benchmark(func(b *testing.B) { runFrameBenchmark(b, bm.open, bm.refresh) })
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
		t.Logf("%-22s %9.3f ms/frame %8d allocs %10d B %8.0f native calls  quads %v sprites %v",
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
		"method":    "best of runs by ns/frame; testing.Benchmark over hostless test windows on the real text system; frames via SimulateFrameRequest (gpui/perf_viewcache_bench_test.go on ticket 01's harness)",
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
