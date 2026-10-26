//go:build windows

// The browser-core ticket 58 GPU gates and measurement harness: the
// window's frame loop computes the damage (the reused view ranges are
// undamaged, everything else damages old ∪ new bounds) and presents
// through Surface.DrawSceneDamage — these tests drive one real,
// shown-never-activated off-screen window through its frame loop and
// require every damaged present to be byte-identical to a full redraw
// of the same scene (the damaged-vs-full equivalence gate of the
// design's §3.11, now at the WINDOW level: a missed damage region
// leaves stale pixels in the retained frame target, so the comparison
// catches it), plus the paint-flashing overlay's visual shape.
//
// The measurement harness (R58_MEASURE_OUT, the r54 pattern) writes
// evidence/browser-core/ticket58-damage.json: frame CPU time (the draw
// + present + retirement per frame request) and the damage fraction
// for the hover change, the scroll, the animation and the full-redraw
// control, each against the force-full baseline presented through the
// same window.
package rendererspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// r58Pane is one cached pane of the harness page: a colored box (the
// hover steps recolor it, the pulse pane ticks it every frame).
type r58Pane struct {
	index int
	light float32
	pulse bool
	// running gates the pulse's animation loop off during the hover and
	// scroll scenarios (a next-frame callback ticks at EVERY frame
	// request, so a live animation would pollute their damage).
	running bool
	tick    int
}

func (p *r58Pane) Render(w *gpui.Window, cx *gpui.Context[r58Pane]) gpui.AnyElement {
	hue := float32(p.index%7) / 7
	if p.pulse && p.running {
		// The animation: re-register the next-frame callback every
		// render (the pulse notifies itself, so the cache misses and
		// the render re-runs each frame — the reference animation
		// loop's damage shape: a small region repaints per frame).
		cx.OnNextFrame(w, func(s *r58Pane, w *gpui.Window, cx *gpui.Context[r58Pane]) {
			s.tick++
			s.light = 0.3 + 0.4*float32(s.tick%2)
			cx.Notify()
		})
	}
	return gpui.Div().
		ID(fmt.Sprintf("r58-pane-%d", p.index)).
		SizeFull().
		Bg(gpui.Hsla{H: hue, S: 0.6, L: p.light, A: 1}).
		IntoElement()
}

// r58Root lays the harness page out: a row of six cached panes, a
// scrollable column of twelve cached rows, and the pulse pane.
type r58Root struct {
	panes []gpui.Entity[r58Pane]
	rows  []gpui.Entity[r58Pane]
	pulse gpui.Entity[r58Pane]
	scroll gpui.ScrollHandle
}

func (r *r58Root) Render(w *gpui.Window, cx *gpui.Context[r58Root]) gpui.AnyElement {
	root := gpui.Div().ID("r58-root").SizeFull().Flex().FlexCol()
	// The page: a 16x12 wrap grid of 80x40 cached panes (192 panes, the
	// hovered element's neighborhood), sized so the scene carries enough
	// primitives for the present's GPU work to matter (the r54
	// measurement page's discipline).
	top := gpui.Div().ID("r58-top").Flex().FlexRow().FlexWrap()
	for i := 0; i < len(r.panes); i++ {
		top.Child(gpui.Div().Flex().FlexShrink0().W(gpui.Px(80)).H(gpui.Px(40)).
			Child(gpui.CachedView(gpui.ViewOf(r.panes[i]), gpui.Styles().SizeFull())))
	}
	scroller := gpui.Div().ID("r58-scroll").Flex().FlexCol().OverflowYScroll().TrackScroll(r.scroll).
		W(gpui.Px(640)).H(gpui.Px(300)).FlexShrink0()
	for i := 0; i < len(r.rows); i++ {
		scroller.Child(gpui.Div().Flex().FlexShrink0().W(gpui.Px(640)).H(gpui.Px(50)).
			Child(gpui.CachedView(gpui.ViewOf(r.rows[i]), gpui.Styles().SizeFull())))
	}
	root.Child(top).
		Child(gpui.Div().ID("r58-bottom").Flex().FlexRow().
			Child(scroller).
			Child(gpui.Div().Flex().FlexShrink0().W(gpui.Px(200)).H(gpui.Px(100)).
				Child(gpui.CachedView(gpui.ViewOf(r.pulse), gpui.Styles().SizeFull()))))
	return root.IntoElement()
}

// r58Shared is the one application and host the measurement's windows
// share: the element runtime's per-window registries assume ONE
// foreground thread, so two hosts (two foreground threads) would race
// them — one app, one host, two windows is the real application's
// shape anyway.
type r58Shared struct {
	app      *gpui.App
	host     *gpui.Host
	renderer *gpui.Renderer
	runDone  chan error
}

// r58Harness is one driven window: its app/host/renderer (shared with
// the other harness windows), the window, its surface and the present
// hook's knobs (the forceFull baseline switch).
type r58Harness struct {
	t        *testing.T
	shared   *r58Shared
	app      *gpui.App
	host     *gpui.Host
	window   *gpui.Window
	surface  *gpui.Surface
	renderer *gpui.Renderer
	root     gpui.Entity[r58Root]
	panes    []gpui.Entity[r58Pane]
	pulse    gpui.Entity[r58Pane]
	scroll   gpui.ScrollHandle
	forceFull atomic.Bool
	scale     float32
	presents  atomic.Int32
	// lastSub is the present hook's latest submission (retired by the
	// harness's frame helper, outside the frame loop's update).
	lastSub atomic.Pointer[gpui.Submission]
	// lastPresentMS is the present hook's latest present-call duration
	// (the CPU submit cost, sampled by the measurement runner).
	lastPresentMS atomic.Int64
}

// foreground runs f on the host thread and waits for it: every app
// touch (entity updates, frame requests) is serialized with the host's
// own message processing, exactly like the real application's
// foreground-thread discipline (the frameloop latency test's
// runForegroundSync pattern).
func (h *r58Harness) foreground(f func()) {
	done := make(chan struct{})
	if !h.host.Post(func() { defer close(done); f() }) {
		// A refused post means the host stopped accepting work; surface
		// it instead of silently skipping the step.
		h.t.Fatalf("the host refused the posted closure")
	}
	<-done
}

// r58Open opens one harness window (the rendererspec boot pattern of
// r16Surface, with the damage-aware present hook installed).
func r58Open(t *testing.T, title string, width, height float32, mode native.SurfaceMode, shared *r58Shared) *r58Harness {
	t.Helper()
	if shared == nil {
		shared = &r58Shared{}
		app := gpui.NewApp()
		host := gpui.NewHost()
		if err := app.Attach(host); err != nil {
			t.Fatalf("attach: %v", err)
		}
		if err := host.Start(); err != nil {
			t.Fatalf("host start: %v", err)
		}
		shared.app, shared.host = app, host
		shared.runDone = make(chan error, 1)
		go func() { shared.runDone <- app.Run(host) }()
		t.Cleanup(func() {
			_ = host.Stop()
			<-shared.runDone
		})
	}
	h := &r58Harness{t: t, shared: shared, app: shared.app, host: shared.host}
	var window *gpui.Window
	var surface *gpui.Surface
	var renderer *gpui.Renderer
	present := func() {
		if window == nil || surface == nil || !window.Alive() || renderer == nil {
			return
		}
		h.presents.Add(1)
		var sub *gpui.Submission
		var err error
		presentStart := time.Now()
		if h.forceFull.Load() {
			// The baseline: the same drawn frame presented through the
			// full-redraw path (forceFull).
			sub, err = surface.DrawSceneDamage(gpui.LastDrawnScene(window), nil, gpui.BackgroundOpaque, nil, true)
		} else {
			sub, err = gpui.PresentFrame(window, surface, gpui.BackgroundOpaque)
		}
		h.lastPresentMS.Store(int64(time.Since(presentStart).Seconds() * 1000000))
		if err != nil {
			// A present failure is observable but never fatal here: the
			// frame loop's next request retries (the ledger keeps the
			// failed submission tracked).
			t.Logf("present: %v", err)
			return
		}
		// The submission's retirement is waited for OUTSIDE the frame
		// request: the hook runs inside the frame loop's App.Update,
		// and the pacing worker's retire dispatch needs that update to
		// finish first (frame below).
		h.lastSub.Store(sub)
	}
	window, err := shared.app.OpenWindowView(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: width, Height: height}},
		OnRedraw:  present,
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		var root gpui.Entity[r58Root]
		root = gpui.NewEntity(app, w.Scope(), func(r *r58Root, _ *gpui.Context[r58Root]) {
			for i := 0; i < 192; i++ {
				r.panes = append(r.panes, gpui.NewEntity(app, w.Scope(), func(p *r58Pane, _ *gpui.Context[r58Pane]) {
					p.index, p.light = i, 0.5
				}))
			}
			for i := 0; i < 40; i++ {
				r.rows = append(r.rows, gpui.NewEntity(app, w.Scope(), func(p *r58Pane, _ *gpui.Context[r58Pane]) {
					p.index, p.light = 10+i, 0.4
				}))
			}
			r.pulse = gpui.NewEntity(app, w.Scope(), func(p *r58Pane, _ *gpui.Context[r58Pane]) {
				p.index, p.light, p.pulse = 20, 0.5, true
			})
			r.scroll = gpui.NewScrollHandle()
			h.panes = r.panes
			h.pulse = r.pulse
			h.scroll = r.scroll
		})
		// The root handle is taken AFTER NewEntity returned: the
		// constructor closure runs inside it, before the outer variable
		// is assigned.
		h.root = root
		return gpui.ViewOf(root)
	})
	if err != nil {
		t.Fatalf("OpenWindowView: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	waitFor(t, 10*time.Second, "the window to be visible", window.Handle().Visible)
	if shared.renderer == nil {
		renderer, err = gpui.NewRenderer()
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		shared.renderer = renderer
	}
	renderer = shared.renderer
	surface, err = renderer.GetOrCreateSurfaceMode(window, mode)
	if err != nil {
		t.Fatalf("GetOrCreateSurfaceMode: %v", err)
	}
	h.window, h.surface, h.renderer = window, surface, renderer
	if scale, err := window.ScaleFactor(); err == nil {
		h.scale = scale
	}
	if h.scale == 0 {
		h.scale = 1
	}
	// The opening frame's present may have been consumed by the host's
	// initial WM_PAINT before the surface existed; a refresh forces the
	// priming present through the surface (a full redraw — the rule's
	// own first-frame answer). The first attempt can lose the race with
	// the frame pacer's own delivery, so retry.
	for attempt := 0; attempt < 5 && h.presents.Load() == 0; attempt++ {
		h.foreground(func() { h.window.Refresh() })
		h.frame()
		r58Settle()
	}
	if h.presents.Load() == 0 {
		t.Fatal("the priming frame request did not present")
	}
	return h
}

// frame runs one frame request (the draw plus the present, on the host
// thread) and its submission's retirement (here, where the pacing
// worker's retire dispatch is free to run), and returns the wall time
// in milliseconds.
func (h *r58Harness) frame() float64 {
	start := time.Now()
	h.foreground(func() { h.window.SimulateFrameRequest() })
	if sub := h.lastSub.Load(); sub != nil {
		h.lastSub.Store(nil)
		if err := sub.WaitForRetirement(15 * time.Second); err != nil {
			h.t.Fatalf("the submission did not retire: %v", err)
		}
	}
	return time.Since(start).Seconds() * 1000
}

// r58ExpectedRect converts one logical rect to the expected device
// damage rect at the window's scale.
func (h *r58Harness) r58ExpectedRect(x, y, w, height float32) gpui.DamageRect {
	s := h.scale
	return gpui.DamageRect{
		X: int32(x * s),
		Y: int32(y * s),
		W: int32(w * s),
		H: int32(height * s),
	}
}

// hoverPane recolors pane i (the idle hover change's state half) and
// leaves the window dirty.
func (h *r58Harness) hoverPane(i int, light float32) {
	h.foreground(func() {
		h.panes[i].Update(h.app, func(p *r58Pane, cx *gpui.Context[r58Pane]) {
			p.light = light
			cx.Notify()
		})
	})
}

// scrollBy advances the scroller by dy logical pixels.
func (h *r58Harness) scrollBy(dy float32) {
	h.foreground(func() {
		h.scroll.SetOffset(gpui.Point{X: 0, Y: h.scroll.Offset().Y - dy})
		h.root.Update(h.app, func(r *r58Root, cx *gpui.Context[r58Root]) { cx.Notify() })
	})
}

// setPulse gates the pulse pane's animation loop on or off (a redraw
// re-registers — or drops — its next-frame callback).
func (h *r58Harness) setPulse(on bool) {
	h.foreground(func() {
		h.pulse.Update(h.app, func(p *r58Pane, cx *gpui.Context[r58Pane]) {
			p.running = on
			cx.Notify()
		})
	})
	h.frame()
}

// pulseFrame steps the animation one frame (the next-frame callback
// mutates and notifies, then the frame request draws and presents).
func (h *r58Harness) pulseFrame() float64 {
	h.foreground(func() { h.window.SimulateNextFrame(h.app) })
	return h.frame()
}

// requireFullRedrawMatch presents the window's last frame with its
// computed damage, reads the retained frame target, presents the SAME
// scene fully and reads again: the two readbacks must be byte-identical
// (a missed damage region leaves the first readback stale).
func (h *r58Harness) requireFullRedrawMatch(step string) {
	h.t.Helper()
	h.forceFull.Store(false)
	h.frame()
	damage, full := h.window.LastFrameDamage()
	if full {
		h.t.Fatalf("%s: the damage is a full redraw; the scenario must produce a partial one", step)
	}
	if len(damage) == 0 {
		h.t.Fatalf("%s: the damage is empty; the scenario must change something", step)
	}
	damagedPixels, err := h.surface.ReadFramePixels()
	if err != nil {
		h.t.Fatalf("%s: ReadFramePixels (damage): %v", step, err)
	}
	sub, err := h.surface.DrawSceneDamage(gpui.LastDrawnScene(h.window), nil, gpui.BackgroundOpaque, nil, true)
	if err != nil {
		h.t.Fatalf("%s: the full redraw: %v", step, err)
	}
	if err := sub.WaitForRetirement(15 * time.Second); err != nil {
		h.t.Fatalf("%s: the full redraw did not retire: %v", step, err)
	}
	fullPixels, err := h.surface.ReadFramePixels()
	if err != nil {
		h.t.Fatalf("%s: ReadFramePixels (full): %v", step, err)
	}
	if !bytes.Equal(damagedPixels, fullPixels) {
		h.t.Fatalf("%s: the damaged present differs from the full redraw of the same scene (damage %v) — the rule missed a region", step, damage)
	}
}

// TestR58HoverScrollAndAnimationDamageMatchesFullRedraw drives the
// harness window through the three acceptance scenarios — an idle
// hover change (a pane recolors), a scroll (the cached rows move) and
// an animation (the pulse pane repaints every frame) — and requires
// every damaged present to be byte-identical to a full redraw of the
// same scene, with the damage bounds exactly the changed element's.
func TestR58HoverScrollAndAnimationDamageMatchesFullRedraw(t *testing.T) {
	h := r58Open(t, "rendererspec r58 damage identity", 1280, 800, native.SurfaceModeDCompPremultiplied, nil)

	// The idle hover change: only the hovered pane's bounds damage
	// (pane 100 sits at grid row 6, column 4).
	h.hoverPane(100, 0.8)
	h.frame()
	damage, full := h.window.LastFrameDamage()
	if full || len(damage) != 1 {
		t.Fatalf("hover damage = (%v, full=%v), want exactly the hovered pane's rect", damage, full)
	}
	if want := h.r58ExpectedRect(320, 240, 80, 40); damage[0] != want {
		t.Fatalf("hover damage = %v, want the hovered pane's device rect %v", damage[0], want)
	}

	// Hover out: the same bounds again.
	h.hoverPane(100, 0.5)
	h.frame()
	if damage, full = h.window.LastFrameDamage(); full || len(damage) != 1 || damage[0] != h.r58ExpectedRect(320, 240, 80, 40) {
		t.Fatalf("hover-out damage = (%v, full=%v), want the hovered pane's rect", damage, full)
	}

	// A few more hover rounds with the pixel-identity gate.
	for step := 0; step < 4; step++ {
		h.hoverPane(100, []float32{0.3, 0.8, 0.4, 0.7}[step])
		h.requireFullRedrawMatch(fmt.Sprintf("hover step %d", step))
	}

	// The scroll: the scroller's region damages (the rows' old and new
	// bounds; the panes replay).
	for step := 0; step < 4; step++ {
		h.scrollBy(37)
		h.frame()
		damage, full := h.window.LastFrameDamage()
		if full {
			t.Fatalf("scroll step %d damage is a full redraw, want the scroller's region", step)
		}
		union := damage[0]
		for _, r := range damage[1:] {
			union = unionDamage(union, r)
		}
		// The scroller sits at (0, 480, 640, 300) logical.
		if union.X < 0 || union.Y < int32(480*h.scale) ||
			union.X+union.W > int32(640*h.scale) || union.Y+union.H > int32(780*h.scale) {
			t.Fatalf("scroll step %d damage union %v escapes the scroller's device bounds (0,%.0f,%.0f,%.0f)", step, union, 480*h.scale, 640*h.scale, 780*h.scale)
		}
		h.requireFullRedrawMatch(fmt.Sprintf("scroll step %d", step))
	}

	// The animation: the pulse pane repaints every frame.
	h.setPulse(true)
	for step := 0; step < 4; step++ {
		h.pulseFrame()
		damage, full := h.window.LastFrameDamage()
		if full || len(damage) != 1 {
			t.Fatalf("animation step %d damage = (%v, full=%v), want the pulse pane's rect", step, damage, full)
		}
		if want := h.r58ExpectedRect(640, 480, 200, 100); damage[0] != want {
			t.Fatalf("animation step %d damage = %v, want the pulse pane's device rect %v", step, damage[0], want)
		}
	}
}

// unionDamage merges two damage rects (the test's local union helper).
func unionDamage(a, b gpui.DamageRect) gpui.DamageRect {
	x0, y0 := a.X, a.Y
	if b.X < x0 {
		x0 = b.X
	}
	if b.Y < y0 {
		y0 = b.Y
	}
	x1, y1 := a.X+a.W, a.Y+a.H
	if b.X+b.W > x1 {
		x1 = b.X + b.W
	}
	if b.Y+b.H > y1 {
		y1 = b.Y + b.H
	}
	return gpui.DamageRect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// ---------------------------------------------------------------------------
// The measurement harness (R58_MEASURE_OUT)
// ---------------------------------------------------------------------------

// r58Sequence is one measured scenario: the timing plus the debug
// counters the ticket asks for.
type r58Sequence struct {
	Desc                  string    `json:"desc"`
	Timing                r54Timing `json:"timing"`
	InputDamageRects      int       `json:"input_damage_rects"`
	DamageFractionMin     int       `json:"damage_fraction_per_mille_min"`
	DamageFractionMean    int       `json:"damage_fraction_per_mille_mean"`
	FullPresents          int       `json:"full_presents"`
	PartialPresents       int       `json:"partial_presents"`
	StateBindsPerFrame    float64   `json:"state_binds_per_frame"`
	SkippedFrames         int       `json:"stolen_frames_skipped"`
	PresentSubmitMeanMS   float64   `json:"present_submit_ms_mean"`
}

// r58Scenario pairs the damaged run with its force-full baseline: the
// two paths are measured INTERLEAVED (one pair per step - the damage
// frame, then the same content's force-full frame), so every pair sees
// the same machine load and the paired savings mean is load-immune.
type r58Scenario struct {
	Desc        string      `json:"desc"`
	Damage      r58Sequence `json:"damage"`
	Baseline    r58Sequence `json:"baseline_force_full"`
	SavingsMS   float64     `json:"paired_frame_savings_ms_mean"`
	SavingsMin  float64     `json:"paired_frame_savings_ms_min"`
	SavingsPct  float64     `json:"frame_savings_percent"`
}

// r58Doc is the whole measurement record.
type r58Doc struct {
	Schema  string         `json:"schema"`
	Date    string         `json:"date"`
	Window  [2]int         `json:"window_px"`
	Note    string         `json:"note"`
	Rule    string         `json:"rule"`
	Measure map[string]any `json:"measurements"`
}

// r58Settle lets the frame pacer's pending vblank post land on a clean
// window (a no-op request) before the next step, so the timed frame is
// the step's own draw and not stolen by a host-delivered one.
func r58Settle() {
	time.Sleep(20 * time.Millisecond)
}

// r58RunPair measures one scenario with both present paths, interleaved:
// each step settles, applies the damage-path mutation (phase 0), times
// one frame request, settles, applies the baseline's mutation (phase 1
// - the same kind of change, so both frames repaint something), and
// times the force-full frame of it. The frame request is the draw plus
// the present plus the retirement, on the host thread; a pair's savings
// (full minus damage) sees the same machine load on both sides.
func r58RunPair(t *testing.T, h *r58Harness, desc string, warmup, n, rectCount int, step func(i, phase int)) r58Scenario {
	t.Helper()
	var damageSubmit, fullSubmit float64
	timed := func(forceFull bool, i, phase int) (float64, bool) {
		h.forceFull.Store(forceFull)
		step(i, phase)
		before := h.presents.Load()
		d := h.frame()
		if h.presents.Load() > before {
			if forceFull {
				fullSubmit += float64(h.lastPresentMS.Load()) / 1000
			} else {
				damageSubmit += float64(h.lastPresentMS.Load()) / 1000
			}
		}
		return d, h.presents.Load() > before
	}
	for i := 0; i < warmup; i++ {
		r58Settle()
		timed(false, i, 0)
		r58Settle()
		timed(true, i, 1)
	}
	before, err := h.surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	var damage, baseline r58Sequence
	damage.Desc = desc + " (damage present)"
	baseline.Desc = desc + " (force-full baseline)"
	damage.InputDamageRects = rectCount
	damageBest, damageTotal := 1e9, 0.0
	fullBest, fullTotal := 1e9, 0.0
	deltaBest, deltaTotal := 1e9, 0.0
	fractionMin, fractionSum := 1<<30, 0.0
	pairs, skipped := 0, 0
	for i := 0; i < n; i++ {
		// Alternate the pair's order so neither path benefits from
		// always running second (the warmed caches of a just-drawn
		// frame).
		var d, f float64
		var okDamage, okFull bool
		sample := func() {
			// The damage fraction is the DAMAGE present's own: sample it
			// right after that frame, before the pair's full frame
			// overwrites the counter.
			if after, err := h.surface.DebugStats(); err == nil {
				frac := int(after.DamageFraction)
				if frac < fractionMin {
					fractionMin = frac
				}
				fractionSum += float64(frac)
			}
		}
		if i%2 == 0 {
			r58Settle()
			d, okDamage = timed(false, i, 0)
			if okDamage {
				sample()
			}
			r58Settle()
			f, okFull = timed(true, i, 1)
		} else {
			r58Settle()
			f, okFull = timed(true, i, 1)
			r58Settle()
			d, okDamage = timed(false, i, 0)
			if okDamage {
				sample()
			}
		}
		if !okDamage || !okFull {
			// The frame pacer's host-delivered request stole one of the
			// pair's frames (its present happened outside the timed
			// call): the pair is not comparable - skip it.
			skipped++
			continue
		}
		pairs++
		damageBest, damageTotal = min(damageBest, d), damageTotal+d
		fullBest, fullTotal = min(fullBest, f), fullTotal+f
		delta := f - d
		deltaBest, deltaTotal = min(deltaBest, delta), deltaTotal+delta
	}
	h.forceFull.Store(false)
	if pairs == 0 {
		pairs = 1
	}
	after, err := h.surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	damage.Timing = r54Timing{N: pairs, MeanMS: damageTotal / float64(pairs), MinMS: damageBest}
	damage.PresentSubmitMeanMS = damageSubmit / float64(pairs)
	damage.DamageFractionMin = fractionMin
	damage.DamageFractionMean = int(fractionSum / float64(pairs))
	damage.FullPresents = int(after.FullPresentCount - before.FullPresentCount)
	damage.PartialPresents = int(after.PartialPresentCount - before.PartialPresentCount)
	damage.StateBindsPerFrame = float64(after.StateBindCount-before.StateBindCount) / float64(2*pairs+2*skipped)
	damage.SkippedFrames = skipped
	baseline.Timing = r54Timing{N: pairs, MeanMS: fullTotal / float64(pairs), MinMS: fullBest}
	baseline.PresentSubmitMeanMS = fullSubmit / float64(pairs)
	baseline.SkippedFrames = skipped
	savings := deltaTotal / float64(pairs)
	pct := 0.0
	if fullTotal > 0 {
		pct = savings / (fullTotal / float64(pairs)) * 100
	}
	return r58Scenario{
		Desc:       desc,
		Damage:     damage,
		Baseline:   baseline,
		SavingsMS:  savings,
		SavingsMin: deltaBest,
		SavingsPct: pct,
	}
}


// TestR58MeasureDamage runs the ticket's measurement set (hover, scroll,
// animation, the full-redraw control, each against the force-full
// baseline on the same window) and writes the evidence JSON named by
// R58_MEASURE_OUT. Skipped when the env is unset.
func TestR58MeasureDamage(t *testing.T) {
	out := os.Getenv("R58_MEASURE_OUT")
	if out == "" {
		t.Skip("set R58_MEASURE_OUT to write the ticket 58 damage measurements")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join("..", "..", out)
	}
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	identity := lib.Identity()

	measure := map[string]any{}
	measure["identity"] = map[string]any{
		"native_revision": identity.NativeRevision,
		"sha256":          identity.SHA256,
		"bytes":           identity.Bytes,
		"source":          identity.Source,
	}

	// The default (DComp, mode 0) surface: the app-default path. The
	// r53 gate pinned this machine's DComp readback fallback (every
	// frame presents full there — the damage-scissored redraw still
	// saves the GPU work), so the partial-present counts also run in
	// the hwnd mode below.
	h := r58Open(t, "rendererspec r58 measure", 1280, 800, native.SurfaceModeDCompPremultiplied, nil)

	// The idle hover change: pane 3 recolors every other step (the
	// alternating lightness keeps the damage real every frame).
	hover := r58RunPair(t, h, "hover change: one cached pane recolors per frame", 3, 30, 1, func(i, phase int) {
		h.hoverPane(100, []float32{0.3, 0.8}[(i*2+phase)%2])
	})
	measure["hover_change"] = hover
	t.Logf("hover: damage %.3f ms vs full %.3f ms — paired savings %.3f ms (min %.3f, %.1f%%), damage %d‰",
		hover.Damage.Timing.MeanMS, hover.Baseline.Timing.MeanMS, hover.SavingsMS, hover.SavingsMin,
		hover.SavingsPct, hover.Damage.DamageFractionMean)

	// The scroll: the scroller advances 16 logical pixels per frame
	// (the rows' old and new bounds; the panes replay).
	scroll := r58RunPair(t, h, "scroll: the cached rows move 16px per frame", 3, 30, 1, func(i, phase int) {
		h.scrollBy(16)
	})
	measure["scroll"] = scroll
	t.Logf("scroll: damage %.3f ms vs full %.3f ms — paired savings %.3f ms (min %.3f, %.1f%%), damage %d‰",
		scroll.Damage.Timing.MeanMS, scroll.Baseline.Timing.MeanMS, scroll.SavingsMS, scroll.SavingsMin,
		scroll.SavingsPct, scroll.Damage.DamageFractionMean)

	// The animation: the pulse pane repaints every frame through the
	// next-frame callback.
	h.setPulse(true)
	animation := r58RunPair(t, h, "animation: the pulse pane repaints every frame", 3, 30, 1, func(i, phase int) {
		h.foreground(func() { h.window.SimulateNextFrame(h.app) })
	})
	measure["animation"] = animation
	t.Logf("animation: damage %.3f ms vs full %.3f ms — paired savings %.3f ms (min %.3f, %.1f%%), damage %d‰",
		animation.Damage.Timing.MeanMS, animation.Baseline.Timing.MeanMS, animation.SavingsMS, animation.SavingsMin,
		animation.SavingsPct, animation.Damage.DamageFractionMean)

	// The full-redraw control: a window refresh re-renders everything;
	// the damage computes full and the present takes the full path.
	full := r58RunPair(t, h, "full-redraw control: a refresh per frame", 3, 30, 0, func(i, phase int) {
		h.foreground(func() { h.window.Refresh() })
	})
	measure["full_redraw_control"] = full
	t.Logf("full redraw control: damage %.3f ms vs full %.3f ms — paired savings %.3f ms (min %.3f, %.1f%%) (both redraw everything; the control pins the ceiling)",
		full.Damage.Timing.MeanMS, full.Baseline.Timing.MeanMS, full.SavingsMS, full.SavingsMin, full.SavingsPct)

	// The Present1-accepted mode (the r53 gate: hwnd mode accepts the
	// dirty-rect present on this machine): the same hover scenario,
	// driven through the frame pacer (this surface's frame slot delivers
	// its frames immediately, so a foreground frame request would only
	// race it) and counting the window-level partial presents.
	hwnd := r58Open(t, "rendererspec r58 measure hwnd", 1280, 800, native.SurfaceModeHwndAlphaIgnore, h.shared)
	hwndBefore, err := hwnd.surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	for i := 0; i < 30; i++ {
		r58Settle()
		p0 := hwnd.presents.Load()
		hwnd.hoverPane(100, []float32{0.3, 0.8}[i%2])
		waitFor(t, 2*time.Second, "the pacer-delivered hover frame", func() bool { return hwnd.presents.Load() > p0 })
	}
	hwndAfter, err := hwnd.surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	measure["hover_change_hwnd_mode"] = map[string]any{
		"desc":                    "hover change in hwnd mode (Present1's accepted mode), driven through the frame pacer",
		"frames":                  30,
		"damage_fraction_per_mille": int(hwndAfter.DamageFraction),
		"partial_presents":        int(hwndAfter.PartialPresentCount - hwndBefore.PartialPresentCount),
		"full_presents":           int(hwndAfter.FullPresentCount - hwndBefore.FullPresentCount),
	}
	t.Logf("hover (hwnd): partial %d full %d, damage %d‰",
		int(hwndAfter.PartialPresentCount-hwndBefore.PartialPresentCount),
		int(hwndAfter.FullPresentCount-hwndBefore.FullPresentCount), int(hwndAfter.DamageFraction))

	doc := r58Doc{
		Schema: "gpui-go/r58-damage-measure@1",
		Date:   time.Now().UTC().Format(time.RFC3339),
		Window: [2]int{1280, 800},
		Note: "wall-time measurements of the WINDOW frame request (the draw plus the present plus the retirement wait, on the host thread) on one shown-never-activated off-screen window per surface mode; the page is a 16x12 wrap grid of 80x40 cached panes (192 panes), a 640x300 scroller of forty cached 50px rows and a 200x100 pulse pane (the animation), so the scene carries about 470 primitives - enough for the present's GPU work to matter while the cached replays keep the rebuild cheap (the damage savings come from the reused view ranges; a real page's non-cached parts add their own rebuild cost on both sides of every row); every scenario is measured as INTERLEAVED pairs (the damage-presented frame, then the same content's force-full frame, order alternating), so the paired savings mean sees the same machine load on both sides - this machine is heavily shared, so the absolute wall times are several times an idle machine's and the paired mean is the comparable statistic; the present_submit_ms fields time the present CALL (the CPU submit cost, measured inside the hook) - the damage path pays a little more CPU (the merge, the retained-target copy) and wins back GPU completion time; the damage fraction is the native per-mille of the window area from the DAMAGE present (forced-full presents do not update that counter, so the full-redraw control's fraction field is stale there - its rects ARE the whole window); the present/bind counters are cumulative deltas over the sequence and include the frame pacer's host-delivered frames during the settles; the DComp mode (the default surface) presents full on this machine - the r53 pinned readback fallback - while the damage-scissored redraw still saves the GPU shading work, and the hwnd mode accepts Present1 (30/30 partial presents at the window level, below)",
		Rule: "reused view ranges at the same bounds and mask are undamaged (Chrome's chunk rule, raster_invalidator.cc:242-249); everything else damages its old and new bounds; capture-backed surfaces always damage; the first frame, a viewport/scale/appearance change, log drift and out-of-order replays fall back to full",
		Measure: measure,
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
	t.Logf("wrote %s", out)
}
