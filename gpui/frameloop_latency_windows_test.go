//go:build windows

package gpui

import (
	"encoding/json"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// TestFrameLatencyWaitableRealWindow is the gpui-perf ticket 05
// real-window check of the low-latency present: a REAL window whose
// present hook draws its last scene through a Renderer surface gets a
// frame-latency waitable swap chain (maximum latency 1), the frame
// source paces on it, a notify is presented sooner than through the
// DwmFlush wait, animation frames keep flowing, and an idle window still
// draws and presents nothing.
//
// With GPUI_PERF05_OUT set, the measured latencies are written there as
// JSON (evidence/perf/ticket05-*.json).
func TestFrameLatencyWaitableRealWindow(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	var (
		windowRef  atomic.Pointer[Window]
		presents   atomic.Int64
		lastStamp  atomic.Int64
		presentErr atomic.Pointer[error]
	)
	var entity Entity[frameLoopView]
	window, err := app.OpenWindowView(WindowOptions{
		Title:    "gpui frame latency",
		Bounds:   Bounds{Origin: Point{X: 80, Y: 80}, Size: Size{Width: 320, Height: 200}},
		Show:     true,
		Activate: false,
		OnRedraw: func() {
			w := windowRef.Load()
			if w == nil {
				return
			}
			scene := LastDrawnScene(w)
			if scene == nil {
				return
			}
			surface, err := renderer.GetOrCreateSurface(w)
			if err == nil {
				var atlas *Atlas
				if atlas, err = w.GlyphAtlas(); err == nil {
					_, err = surface.DrawScene(scene, atlas, BackgroundOpaque)
				}
			}
			if err != nil {
				presentErr.Store(&err)
				return
			}
			lastStamp.Store(time.Now().UnixNano())
			presents.Add(1)
		},
	}, func(w *Window, app *App) View {
		entity = NewEntity(app, w.Scope(), func(*frameLoopView, *Context[frameLoopView]) {})
		return ViewOf(entity)
	})
	if err != nil {
		t.Fatalf("OpenWindowView failed: %v — this session is expected to create real windows", err)
	}
	windowRef.Store(window)
	t.Cleanup(func() {
		h.runForegroundSync(func() { renderer.Close() })
		if window.Alive() {
			window.Close()
			desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	demand := func() bool {
		var d bool
		h.runForegroundSync(func() { d = window.hasFrameDemand() })
		return d
	}
	// The opening frame was drawn before the hook could find the window;
	// a refresh presents through the surface.
	h.runForegroundSync(func() { window.Refresh() })
	desktopWaitFor(t, "the first surface present", func() bool { return presents.Load() >= 1 })
	if errp := presentErr.Load(); errp != nil {
		t.Fatalf("present hook: %v", *errp)
	}
	desktopWaitFor(t, "the window to go idle", func() bool { return !demand() })

	surface := frameLatencySurfaceOf(window)
	if surface == nil {
		t.Fatal("the window's surface is not registered with the frame source")
	}

	// notifyLatency measures one notify -> present round trip from idle.
	notifyLatency := func() time.Duration {
		before := presents.Load()
		time.Sleep(60 * time.Millisecond) // idle, so the swap chain drained
		start := time.Now()
		h.runForegroundSync(func() {
			entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
		})
		desktopWaitFor(t, "the notified frame to be presented", func() bool { return presents.Load() > before })
		return time.Duration(lastStamp.Load() - start.UnixNano())
	}
	measure := func(n int) []time.Duration {
		out := make([]time.Duration, n)
		for i := range out {
			out[i] = notifyLatency()
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	const samples = 25
	waitable := measure(samples)

	// The same window paced by DwmFlush (the pre-ticket frame source):
	// unregister the surface for the comparison, then restore it.
	unregisterFrameLatencySurface(surface)
	dwmFlush := measure(samples)
	registerFrameLatencySurface(window, surface)

	// Animation: 60 requested frames are drawn and presented, paced by
	// the waitable.
	ledger := newFrameWakeLedger()
	h.runForegroundSync(func() { frameStateOf(window).onWake = ledger.record })
	t.Cleanup(func() { h.runForegroundSync(func() { frameStateOf(window).onWake = nil }) })
	startPresents := presents.Load()
	animStart := time.Now()
	h.runForegroundSync(func() {
		entity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
			v.animate = 60
			cx.Notify()
		})
	})
	desktopWaitFor(t, "the animation frames to be presented", func() bool { return presents.Load() >= startPresents+61 })
	animElapsed := time.Duration(lastStamp.Load() - animStart.UnixNano())
	desktopWaitFor(t, "the animation loop to go idle", func() bool { return !demand() })

	// Idle means idle: nothing is drawn or presented unless the platform
	// caused it.
	settled := presents.Load()
	idleMark := ledger.mark()
	time.Sleep(300 * time.Millisecond)
	idleExternal, idleOther := ledger.since(idleMark)
	if len(idleOther) != 0 {
		t.Fatalf("an idle window was woken by a non-platform origin: %v", idleOther)
	}
	if idle := presents.Load() - settled; idle > int64(idleExternal) {
		t.Fatalf("an idle window presented %d frames, only %d attributable to platform events", idle, idleExternal)
	}
	if errp := presentErr.Load(); errp != nil {
		t.Fatalf("present hook: %v", *errp)
	}

	// The surface's swap chain really is waitable (checked last: a wait
	// consumes one frame-latency signal).
	var wait native.FrameLatencyWait
	wait, err = surface.waitFrameLatency(100 * time.Millisecond)
	if err != nil || wait == native.FrameLatencyUnavailable {
		t.Fatalf("frame-latency wait = %v, %v; want a waitable swap chain", wait, err)
	}

	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	median := func(ds []time.Duration) float64 { return ms(ds[len(ds)/2]) }
	p90 := func(ds []time.Duration) float64 { return ms(ds[len(ds)*9/10]) }
	t.Logf("notify->present from idle: waitable median %.2f ms (p90 %.2f), DwmFlush median %.2f ms (p90 %.2f); 60-frame animation %.1f ms (%.2f ms/frame)",
		median(waitable), p90(waitable), median(dwmFlush), p90(dwmFlush), ms(animElapsed), ms(animElapsed)/61)
	// Continuous frames stay vblank paced (an unpaced loop measured 0.23
	// ms/frame here; one vblank is at least 2 ms on any display up to
	// 500 Hz).
	if perFrame := ms(animElapsed) / 61; perFrame < 1.5 {
		t.Errorf("animation frames ran at %.2f ms/frame: the frame source no longer waits for the vblank between continuous frames", perFrame)
	}
	if median(waitable) > median(dwmFlush) {
		t.Errorf("the waitable frame source is slower from idle (median %.2f ms) than DwmFlush (%.2f ms)", median(waitable), median(dwmFlush))
	}

	if out := os.Getenv("GPUI_PERF05_OUT"); out != "" {
		list := func(ds []time.Duration) []float64 {
			v := make([]float64, len(ds))
			for i, d := range ds {
				v[i] = ms(d)
			}
			return v
		}
		record := map[string]any{
			"test":                          "TestFrameLatencyWaitableRealWindow",
			"samples":                       samples,
			"notify_to_present_ms_waitable": map[string]any{"median": median(waitable), "p90": p90(waitable), "sorted": list(waitable)},
			"notify_to_present_ms_dwmflush": map[string]any{"median": median(dwmFlush), "p90": p90(dwmFlush), "sorted": list(dwmFlush)},
			"animation_frames":              61,
			"animation_ms":                  ms(animElapsed),
			"animation_ms_per_frame":        ms(animElapsed) / 61,
			"idle_presents_in_300ms":        presents.Load() - settled,
			"idle_platform_wakes":           idleExternal,
			"final_wait_result":             int(wait),
			"method":                        "real window 320x200, present hook draws LastDrawnScene through Renderer.DrawScene; notify from idle (60 ms gap) to the present hook's return; DwmFlush = the same window with its surface unregistered from the frame source",
		}
		data, _ := json.MarshalIndent(record, "", "  ")
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatalf("writing %s: %v", out, err)
		}
	}
}
