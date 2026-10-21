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
// clean frames no-op); the interval fallback uses the reference default
// instead of querying DwmGetCompositionTimingInfo (vsync.rs:58-75). The
// invalidation itself is posted to the host thread so it lands after the
// current WM_PAINT's ValidateRect and only for a window still alive.

import (
	"sync"
	"time"
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
)

// framePacer is one host's demand-driven vblank pacer.
type framePacer struct {
	host    *Host
	mu      sync.Mutex
	pending map[*Window]struct{}
	running bool
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

// loop waits for vblank, then posts the invalidation of every pending
// window to the host thread; it exits once a vblank passes with no
// demand or the host stopped accepting work.
func (p *framePacer) loop() {
	for {
		frameWaitForVsync()
		p.mu.Lock()
		if len(p.pending) == 0 {
			p.running = false
			p.mu.Unlock()
			return
		}
		batch := make([]*Window, 0, len(p.pending))
		for w := range p.pending {
			batch = append(batch, w)
		}
		p.pending = make(map[*Window]struct{})
		p.mu.Unlock()

		accepted := p.host.Post(func() {
			for _, w := range batch {
				if w.handle.record() == nil {
					continue
				}
				// InvalidateRect(hwnd, NULL, FALSE): the whole client area,
				// no background erase; WM_PAINT follows when the thread's
				// queue is otherwise idle.
				procFrameInvalidateRect.Call(w.handle.hwnd, 0, 0)
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

// frameWaitForVsync is VSyncProvider::wait_for_vsync (vsync.rs:40-55).
func frameWaitForVsync() {
	start := time.Now()
	hr, _, _ := procFrameDwmFlush.Call()
	if int32(hr) < 0 || time.Since(start) < frameVsyncIntervalThreshold {
		time.Sleep(frameDefaultVsyncInterval)
	}
}
