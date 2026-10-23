//go:build windows

package gpui

// The Windows frame source of the frame loop (Base ticket 03): a woken
// real window gets its client area invalidated at the next vblank, and
// the resulting WM_PAINT delivers the window's frame request (the host's
// paint hook → Window.requestFrame).
//
// The pinned reference (CE 254b5dbd, crates/gpui_windows/src):
//
//   - platform.rs:306-345 begin_vsync_thread: a "VSyncProvider" thread
//     waits for vblank and RedrawWindow(RDW_INVALIDATE)s every window;
//   - vsync.rs:40-55 wait_for_vsync: DwmFlush, falling back to sleeping
//     one interval when the wait returns in under 1ms (monitor asleep or
//     occluded desktop); DEFAULT_VSYNC_INTERVAL = 16.666ms (vsync.rs:23);
//   - events.rs:1303-1346 draw_window: WM_PAINT runs request_frame then
//     ValidateRect.
//
// Deviations: the pacer is demand driven — it runs only while some
// window has an armed wake and exits one vblank after the demand ends
// (the reference thread invalidates every window on every vblank and the
// clean frames no-op); a minimized or DWM-cloaked window is never
// invalidated — its demand stays pending until it is visible again
// (browser-core ticket 03; Chromium's display scheduler draws only for
// visible windows, display_scheduler.cc:602-607); a window can ask for
// half rate (browser-core ticket 47, the port of viz's half-rate
// preferred interval, external_begin_frame_source_win.cc:53-62,
// :96-110) — the source-level skip of every other vsync becomes a
// per-window every-other-slot rule below; the interval fallback
// uses the reference default
// instead of querying DwmGetCompositionTimingInfo (vsync.rs:58-75), with
// the pacer measuring its own slot cadence for the half-rate threshold.
// The invalidation itself is posted to the host thread so it lands after
// the current WM_PAINT's ValidateRect and only for a window still alive.

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hoangvu12/gpui-go/internal/native"
)

var (
	// procFrameInvalidateRect is user32 InvalidateRect (the frame
	// source's invalidation; RedrawWindow RDW_INVALIDATE equivalent).
	procFrameInvalidateRect = modUser32.NewProc("InvalidateRect")
	// procFrameDwmFlush is dwmapi DwmFlush (the vblank wait).
	procFrameDwmFlush = modDwmapi.NewProc("DwmFlush")
)

const (
	// frameVsyncIntervalThreshold is VSYNC_INTERVAL_THRESHOLD
	// (vsync.rs:22).
	frameVsyncIntervalThreshold = time.Millisecond
	// frameDefaultVsyncInterval is DEFAULT_VSYNC_INTERVAL (vsync.rs:23).
	frameDefaultVsyncInterval = 16666 * time.Microsecond
	// preferredFrameIntervalTolerance is the half-millisecond slack of
	// Chromium's half-rate decision (SetPreferredInterval,
	// external_begin_frame_source_win.cc:97-101: a preferred interval
	// greater than twice the vsync interval minus this much runs at half
	// rate). The same tolerance separates two granted slots of a
	// half-rate window: a slot is due once that much time has passed
	// since the window's last granted slot, which on regular vblanks is
	// every other vblank.
	preferredFrameIntervalTolerance = 500 * time.Microsecond
	// preferredFrameIntervalMeasureFloor and Cap bound the vblank
	// interval samples the pacer accepts: shorter gaps are a back-to-
	// back slot pair (the no-wait-after-idle path), longer ones cross a
	// demand gap, and neither is a vblank.
	preferredFrameIntervalMeasureFloor = time.Millisecond
	preferredFrameIntervalMeasureCap   = 60 * time.Millisecond
)

// framePacer is one host's demand-driven vblank pacer.
type framePacer struct {
	host    *Host
	mu      sync.Mutex
	pending map[*Window]struct{}
	running bool
	// lastSlot is when the previous frame slot was granted (read and
	// written by the single running loop goroutine only).
	lastSlot time.Time
	// slotObserved is when the loop last observed a frame slot; with
	// lastSlot it measures the pacer's own slot cadence. Loop goroutine
	// only.
	slotObserved time.Time
	// slotCount counts the frame slots the pacer has served (monotonic
	// across demand bursts; loop goroutine only). The half-rate rule
	// compares it with each window's last granted slot.
	slotCount int64
	// vsyncEstimate is the median gap between recent frame slots, in
	// nanoseconds (0 until measured): the pacer's stand-in for the vsync
	// interval the reference's VSyncProvider carries (vsync.rs:19).
	// Atomic — the loop goroutine writes it, the host thread's posted
	// invalidation reads it for the per-window half-rate decision.
	vsyncEstimate atomic.Int64
	// slotGaps is the ring of recent inter-slot gaps the estimate is
	// computed from. Loop goroutine only.
	slotGaps     [8]time.Duration
	slotGapCount int
}

var (
	framePacersMu sync.Mutex
	framePacers   = map[*Host]*framePacer{}
)

// scheduleHostFrame arms a vblank-paced invalidation of a real window.
func scheduleHostFrame(w *Window) {
	h := w.handle.host
	if h == nil {
		return
	}
	framePacersMu.Lock()
	p := framePacers[h]
	if p == nil {
		p = &framePacer{host: h, pending: make(map[*Window]struct{})}
		framePacers[h] = p
	}
	framePacersMu.Unlock()
	p.request(w)
}

// request queues a window and starts the pacer goroutine when idle.
func (p *framePacer) request(w *Window) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending[w] = struct{}{}
	if !p.running {
		p.running = true
		go p.loop()
	}
}

// loop waits for the next frame slot (waitForSlot), then posts the
// invalidation of every pending window to the host thread; it exits as
// soon as no demand is pending (before waiting, so an exiting pacer never
// consumes a frame-latency signal no frame will pay back) or the host
// stopped accepting work. A hidden window (minimized or DWM-cloaked,
// browser-core ticket 03) is not invalidated: its demand stays pending,
// so the pacer keeps checking every frame slot and serves the demand
// when the window becomes visible again.
func (p *framePacer) loop() {
	for {
		p.mu.Lock()
		if len(p.pending) == 0 {
			p.running = false
			p.mu.Unlock()
			return
		}
		var latency *Surface
		for w := range p.pending {
			if latency = frameLatencySurfaceOf(w); latency != nil {
				break
			}
		}
		p.mu.Unlock()
		p.waitForSlot(latency)
		p.observeSlot(p.lastSlot)
		p.mu.Lock()
		batch := make([]*Window, 0, len(p.pending))
		for w := range p.pending {
			batch = append(batch, w)
		}
		p.pending = make(map[*Window]struct{})
		p.mu.Unlock()
		p.slotCount++
		slot := p.slotCount

		accepted := p.host.Post(func() {
			for _, w := range batch {
				hw := w.handle.record()
				if hw == nil {
					continue
				}
				// A hidden window cannot present: a minimized or DWM-cloaked
				// one keeps its demand pending instead of being invalidated
				// (browser-core ticket 03; the reference's vsync thread
				// invalidates unconditionally, gpui_windows platform.rs
				// 306-345). The pacer re-checks at every frame slot, so a
				// hidden window costs one IsIconic/DWM query per vblank and
				// its demand is served — the pending wake, the dirty flag or
				// the next-frame callbacks — as soon as it is visible again.
				if !hw.frameVisible() {
					p.request(w)
					continue
				}
				// A half-rate window skips this vblank (browser-core ticket
				// 47): its demand stays pending exactly like a hidden one, so
				// the pacer re-checks at every slot and the next animation
				// frame is served on the vblank the interval allows.
				if !p.grantFrameSlot(w, slot) {
					p.request(w)
					continue
				}
				// InvalidateRect(hwnd, NULL, FALSE): the whole client area,
				// no background erase; WM_PAINT follows when the thread's
				// queue is otherwise idle.
				procFrameInvalidateRect.Call(hw.hwnd, 0, 0)
			}
		})
		if !accepted {
			p.mu.Lock()
			p.running = false
			p.pending = make(map[*Window]struct{})
			p.mu.Unlock()
			framePacersMu.Lock()
			if framePacers[p.host] == p {
				delete(framePacers, p.host)
			}
			framePacersMu.Unlock()
			return
		}
	}
}

// observeSlot records one frame slot and keeps the pacer's vblank
// interval estimate at the MEDIAN gap between recent slots (the
// stand-in for the interval the reference's VSyncProvider reports,
// vsync.rs:19-37; the pacer cannot query the compositor's timing
// without diverging from its documented default-interval deviation, so
// it measures its own cadence). Gaps outside the healthy window are
// slot pairs with no wait between them or demand gaps, not vblanks,
// and a stray early DwmFlush return — the sub-millisecond fallback of
// frameWaitForVsync only catches returns under 1 ms — cannot move the
// median the way it would poison a running minimum. Loop goroutine
// only.
func (p *framePacer) observeSlot(slot time.Time) {
	prev := p.slotObserved
	p.slotObserved = slot
	if prev.IsZero() {
		return
	}
	gap := slot.Sub(prev)
	if gap < preferredFrameIntervalMeasureFloor || gap > preferredFrameIntervalMeasureCap {
		return
	}
	n := copy(p.slotGaps[:], p.slotGaps[1:])
	p.slotGaps[n] = gap
	if p.slotGapCount < len(p.slotGaps) {
		p.slotGapCount++
	}
	samples := p.slotGapCount
	if samples < 1 {
		return
	}
	sorted := append([]time.Duration(nil), p.slotGaps[:samples]...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p.vsyncEstimate.Store(int64(sorted[len(sorted)/2]))
}

// vsyncInterval returns the pacer's current vblank interval estimate,
// falling back to the reference default (vsync.rs:23) until slots have
// been measured. Any goroutine.
func (p *framePacer) vsyncInterval() time.Duration {
	if ns := p.vsyncEstimate.Load(); ns > 0 {
		return time.Duration(ns)
	}
	return frameDefaultVsyncInterval
}

// grantFrameSlot decides whether frame slot number `slot` serves w
// (browser-core ticket 47, the port of viz's half-rate preferred
// interval, components/viz/service/frame_sinks/
// external_begin_frame_source_win.cc). A window without a preferred
// interval, or one asking for no more than half rate's threshold, is
// always granted. A preferred interval greater than twice the vblank
// interval minus half a millisecond selects half rate
// (SetPreferredInterval, :97-101); a half-rate window is granted every
// OTHER slot — the per-window form of the source's skip_next_vsync_
// alternation (:53-62) — counting slots rather than time so vblank
// jitter cannot break the cadence. The rule self-corrects where
// Chromium resets the flag: a window whose demand starts after idle
// (a stale last slot) is served at once like the source's missed begin
// frame (:70-92), and a window that missed a slot mid-flight (its
// WM_PAINT still running) gets the next one instead of falling behind.
// Skipped demand stays pending in the pacer, so no frame is lost. Runs
// on the host thread inside the pacer's posted invalidation.
func (p *framePacer) grantFrameSlot(w *Window, slot int64) bool {
	fs := frameStateOf(w)
	preferred := time.Duration(fs.preferredFrameInterval.Load())
	if preferred <= 0 {
		return true
	}
	if preferred <= 2*p.vsyncInterval()-preferredFrameIntervalTolerance {
		// Full rate: only full and half rate exist
		// (external_begin_frame_source_win.cc:96-110 maps any interval
		// to one of the two).
		return true
	}
	// Half rate: every other vblank. The first grant of a session (a
	// zero last slot) is immediate.
	if fs.lastGrantedSlot != 0 && slot-fs.lastGrantedSlot < 2 {
		return false
	}
	fs.lastGrantedSlot = slot
	return true
}

// waitForSlot waits until a frame should start (gpui-perf ticket 05).
//
// With a pending window's surface it first waits on the swap chain's
// frame-latency waitable object (maximum latency 1), so a frame never
// starts while the previous one is still queued for the GPU. That wait
// is bounded by one vsync interval: a frame request that did not present
// leaves the object unsignaled, and the timeout keeps the loop moving.
//
// The waitable does not follow the display (the swap chain presents with
// sync interval 0), so it cannot pace an animation alone. A frame that
// follows the previous slot within one interval (an animation or other
// continuous demand) also waits for the vblank as before. The first
// frame after idle skips that wait and starts at once: its present is
// composed at the next vblank instead of one vblank later.
//
// Without a surface, or when the surface has no waitable, it is the
// reference DwmFlush wait.
func (p *framePacer) waitForSlot(latency *Surface) {
	defer func() { p.lastSlot = time.Now() }()
	if latency != nil {
		wait, err := latency.waitFrameLatency(frameDefaultVsyncInterval)
		if err == nil && wait != native.FrameLatencyUnavailable {
			if time.Since(p.lastSlot) >= frameDefaultVsyncInterval {
				return
			}
		}
	}
	frameWaitForVsync()
}

// frameWaitForVsync is VSyncProvider::wait_for_vsync (vsync.rs:40-55).
func frameWaitForVsync() {
	start := time.Now()
	hr, _, _ := procFrameDwmFlush.Call()
	if int32(hr) < 0 || time.Since(start) < frameVsyncIntervalThreshold {
		time.Sleep(frameDefaultVsyncInterval)
	}
}
