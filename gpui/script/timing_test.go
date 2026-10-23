//go:build windows

package script_test

// The probe's thresholds, as tests (ticket 62, spec seam 3, probe 09):
//
//	reload with state kept   under 300 ms
//	a bound-value update     under 2 ms
//	frame overhead           under 1 ms at the probe's size (a zterm-like
//	                         sidebar of ~1,050 elements)
//
// The probe measured with QueryPerformanceCounter because time.Now is
// quantized to about 1 ms on this PC; the same clock is used here
// (clock_windows.go in this package). The frame overhead is the median
// of paired differences — a script frame and a hand-written Go frame
// drawn alternately — because the machine is shared.

import (
	_ "embed"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
)

//go:embed testdata/sidebar.tsx
var sidebarSrc string

// The thresholds (probe 09, spec "TSX scripting (ADR 0003)").
const (
	reloadThreshold    = 300 * time.Millisecond
	boundThreshold     = 2 * time.Millisecond
	frameThreshold     = 1 * time.Millisecond
	pairedFrameSamples = 200
	reloadSamples      = 30
	boundSamples       = 1000
)

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

// mustSidebar loads the probe-sized sidebar into a host.
func mustSidebar(t testing.TB) (*script.Host, *srcView) {
	t.Helper()
	s := &srcView{src: sidebarSrc}
	h := script.New(script.WithSource(s))
	if err := h.Reload(); err != nil {
		t.Fatalf("load sidebar.tsx: %v", err)
	}
	return h, s
}

func TestReloadWithStateKeptIsUnder300ms(t *testing.T) {
	h, s := mustSidebar(t)
	// Move state first, so the reload has something to keep.
	if err := h.Click("counter"); err != nil {
		t.Fatal(err)
	}
	ds := make([]time.Duration, reloadSamples)
	for i := range ds {
		edited := strings.Replace(sidebarSrc, ">Agents<", fmt.Sprintf(">Agents (edit %d)<", i+1), 1)
		s.Set(edited)
		start := qpcNow()
		if err := h.Reload(); err != nil {
			t.Fatalf("reload %d: %v", i, err)
		}
		ds[i] = qpcSince(start)
	}
	med := median(ds)
	t.Logf("reload median = %.3f ms over %d reloads (threshold %.0f ms); first = %.3f ms",
		msOf(med), reloadSamples, reloadThreshold.Seconds()*1000, msOf(ds[0]))
	if med >= reloadThreshold {
		t.Fatalf("reload median = %.3f ms, want under %.0f ms", msOf(med), reloadThreshold.Seconds()*1000)
	}
	// State was kept across the timing loop (the threshold's scenario).
	if got, _ := h.Text("counter"); got != "Clicked 1 times" {
		t.Fatalf("counter text = %q (keyed state lost)", got)
	}
}

func TestABoundUpdateIsUnder2ms(t *testing.T) {
	h, _ := mustSidebar(t)
	ds := make([]time.Duration, boundSamples)
	for i := range ds {
		start := qpcNow()
		if err := h.Click("counter"); err != nil {
			t.Fatalf("click %d: %v", i, err)
		}
		ds[i] = qpcSince(start)
	}
	med := median(ds)
	t.Logf("bound update median = %.4f ms over %d clicks (threshold %.0f ms); first = %.4f ms",
		msOf(med), boundSamples, boundThreshold.Seconds()*1000, msOf(ds[0]))
	if med >= boundThreshold {
		t.Fatalf("bound update median = %.4f ms, want under %.0f ms", msOf(med), boundThreshold.Seconds()*1000)
	}
	if got, _ := h.Text("counter"); got != fmt.Sprintf("Clicked %d times", boundSamples) {
		t.Fatalf("counter text = %q", got)
	}
	// And the fast path really is the fast path: no re-describes.
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1", h.DescribeCount())
	}
}

// sidebarNative is the plain-Go twin of sidebar.tsx: the frame-cost
// baseline (the probe's native.go, on the host's public API). Its
// state mirrors the script's initial keyed state so both sides draw
// the same scene.
type sidebarNative struct {
	Current  string
	Selected int
	Clicks   int
	notify   func()
}

var nativeAgents = []struct{ pane, name, where, status string }{
	{"p1", "Refactor the layout pass", "gpui-go", "working"},
	{"p2", "Port the IME tests", "zterm", "blocked"},
	{"p3", "Fix shadow blur sigma", "gpui", "error"},
	{"p4", "Write the release notes", "docs", "done"},
	{"p5", "Probe goja reload", "research", "idle"},
	{"p6", "Tidy the class mapper", "gpui", "working"},
}

func (s *sidebarNative) build() *gpui.DivElement {
	agents := gpui.Div().Flex().FlexCol().Gap(gpui.Px(1)).Py(gpui.Px(2)).Px(gpui.Px(8))
	for _, a := range nativeAgents {
		agents = agents.Child(s.agentRow(a))
	}
	list := gpui.Div().ID("sessions").Flex().FlexCol().Flex1().MinH(gpui.Px(0)).OverflowYScroll().Py(gpui.Px(0)).Px(gpui.Px(8))
	for i := range 200 {
		list = list.Child(s.sessionRow(i))
	}
	return gpui.Div().Flex().FlexCol().W(gpui.Px(240)).HFull().Bg(c111827).
		Child(gpui.Div().Class("flex items-center gap-1.5 px-4 pt-1 pb-1.5").TextSize(gpui.Px(14)).
			Child(gpui.Div().FontWeight(600).TextColor(cd1d5db).Child("Agents")).
			Child(gpui.Div().TextColor(c6b7280).Child(strconv.Itoa(len(nativeAgents))))).
		Child(agents).
		Child(gpui.Div().ID("counter").Class("mx-2 my-2 px-3 py-1.5 rounded-md bg-gray-700").
			TextColor(cf9fafb).TextSize(gpui.Px(13)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { s.Clicks++; s.fire() }).
			Child(gpui.Div().Child(fmt.Sprintf("Clicked %d times", s.Clicks)))).
		Child(list)
}

func (s *sidebarNative) agentRow(a struct{ pane, name, where, status string }) *gpui.DivElement {
	class := "rounded-md"
	if a.pane == s.Current {
		class = "rounded-md bg-white/10"
	}
	pane := a.pane
	return gpui.Div().ID("agent-" + a.pane).Class(class).Flex().ItemsCenter().Gap(gpui.Px(8)).Py(gpui.Px(6)).Px(gpui.Px(8)).
		OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { s.Current = pane; s.fire() }).
		Child(gpui.Div().W(gpui.Px(8)).H(gpui.Px(8)).Rounded(gpui.Px(4)).Bg(nativeMark[a.status])).
		Child(gpui.Div().Flex().FlexCol().Flex1().
			Child(gpui.Div().TextColor(ce5e7eb).TextSize(gpui.Px(13)).Child(a.name)).
			Child(gpui.Div().TextColor(c9ca3af).TextSize(gpui.Px(11)).Child(a.status + " · " + a.where)))
}

func (s *sidebarNative) sessionRow(i int) *gpui.DivElement {
	class := "px-2 py-1 rounded"
	return gpui.Div().ID("row-" + strconv.Itoa(i)).Class(class).Flex().JustifyBetween().
		TextColor(cd1d5db).TextSize(gpui.Px(12)).
		OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { s.fire() }).
		Child(gpui.Div().Child("session " + strconv.Itoa(i))).
		Child(gpui.Div().TextColor(c6b7280).Child(strconv.Itoa((i*7)%60) + "m ago"))
}

func (s *sidebarNative) fire() {
	if s.notify != nil {
		s.notify()
	}
}

var nativeMark = map[string]gpui.Hsla{
	"working": gpui.Hex("#60a5fa"),
	"blocked": gpui.Hex("#f59e0b"),
	"error":   gpui.Hex("#ef4444"),
	"done":    gpui.Hex("#22c55e"),
	"idle":    gpui.Hex("#6b7280"),
}

var (
	c111827 = gpui.Hex("#111827")
	c6b7280 = gpui.Hex("#6b7280")
	c9ca3af = gpui.Hex("#9ca3af")
	cd1d5db = gpui.Hex("#d1d5db")
	ce5e7eb = gpui.Hex("#e5e7eb")
	cf9fafb = gpui.Hex("#f9fafb")
)

// scriptRenderView renders a host's tree; nativeRenderView the Go twin.
type scriptRenderView struct{ h *script.Host }

func (v *scriptRenderView) Render(w *gpui.Window, cx *gpui.Context[scriptRenderView]) gpui.AnyElement {
	return v.h.Element()
}

type nativeRenderView struct{ s *sidebarNative }

func (v *nativeRenderView) Render(w *gpui.Window, cx *gpui.Context[nativeRenderView]) gpui.AnyElement {
	return v.s.build().IntoElement()
}

// openFrames opens a hostless window on the real text system and
// returns a step that draws one changed frame and its time, plus the
// scene's primitive counts (the probe's openFrames pattern).
func openFrames[S any, PS interface {
	*S
	gpui.Render[S]
}](t *testing.T, build func(*S)) (step func() time.Duration, counts func() [2]int) {
	t.Helper()
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
		before := gpui.WindowDrawCount(window)
		start := qpcNow()
		ta.Update(func(app *gpui.App) {
			root.Update(app, func(_ *S, cx *gpui.Context[S]) { cx.Notify() })
		})
		if gpui.WindowDrawCount(window) == before {
			window.SimulateFrameRequest()
		}
		d := qpcSince(start)
		if gpui.WindowDrawCount(window) != before+1 {
			t.Fatal("frame not drawn")
		}
		return d
	}, counts
}

// paired alternates a and b so machine load hits both alike, and
// returns each median and the median of the paired differences.
func paired(n int, a, b func() time.Duration) (ma, mb, diff time.Duration) {
	as, bs, ds := make([]time.Duration, n), make([]time.Duration, n), make([]time.Duration, n)
	for i := range n {
		as[i], bs[i] = a(), b()
		ds[i] = as[i] - bs[i]
	}
	return median(as), median(bs), median(ds)
}

func TestFrameOverheadIsUnder1msAtTheProbeSize(t *testing.T) {
	h, _ := mustSidebar(t)
	// The probe's tree: 1,053 nodes (elements and text runs).
	if n := h.NodeCount(); n != 1053 {
		t.Fatalf("the sidebar view describes %d nodes, want the probe's 1053 (the threshold's size premise)", n)
	}
	// The script's initial state: current p1, nothing selected, 0 clicks.
	nat := &sidebarNative{Current: "p1", Selected: -1}
	scriptFrame, scriptCounts := openFrames[scriptRenderView](t, func(v *scriptRenderView) { v.h = h })
	nativeFrame, nativeCounts := openFrames[nativeRenderView](t, func(v *nativeRenderView) { v.s = nat })

	// The same scene both ways: quads and glyph sprites.
	if sc, nc := scriptCounts(), nativeCounts(); sc != nc {
		t.Fatalf("the scenes differ: script quads/sprites %v, native %v", sc, nc)
	} else {
		t.Logf("scene: %d quads, %d glyph sprites", sc[0], sc[1])
	}

	fs, fn, diff := paired(pairedFrameSamples, scriptFrame, nativeFrame)
	t.Logf("frames over %d pairs: script %.3f ms, native %.3f ms, paired overhead %.3f ms (threshold %.0f ms)",
		pairedFrameSamples, msOf(fs), msOf(fn), msOf(diff), frameThreshold.Seconds()*1000)
	if diff >= frameThreshold {
		t.Fatalf("frame overhead = %.3f ms, want under %.0f ms", msOf(diff), frameThreshold.Seconds()*1000)
	}
}
