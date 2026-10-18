//go:build windows

// Package capturespec holds ticket28's capture model tests: real
// Windows Graphics Capture on this machine's monitors through the
// public gpui API and the native ABI, real windows driving monitor
// content changes (the renderer's clear-frame presents), the stream
// lifecycle, the bounded pending queue with its drop-oldest counter,
// stop/close races and the release observability of superseded frames.
//
// Environment expectation: this session owns a DWM-composited desktop
// with a D3D11-capable adapter and Windows Graphics Capture available
// (monitor capture needs no user consent dialog). Every test uses real
// captures and real windows and FAILS when the environment cannot
// provide them — it never silently skips. The one thing these tests
// never depend on is the real cursor: this shell's mouse hit tests are
// corrupted, so no gate waits for hover or cursor movement.
//
// Slot discipline: the native capture slot registry reuses vacant
// slots first, and only slot-0 frames fit the scene surface record's
// 32-bit source payload, so every test keeps AT MOST ONE live stream
// and destroys it before finishing (sequential create/destroy stays in
// slot 0).
package capturespec

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// bootApp boots a fresh host with the application attached and Run-ing
// (the rendererspec boot pattern), stopping everything at cleanup.
func bootApp(t *testing.T) (*gpui.App, *gpui.Host) {
	t.Helper()
	before := runtime.NumGoroutine()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v — this session is expected to create real windows", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
		waitFor(t, 5*time.Second, "goroutines to drain back near the baseline", func() bool {
			return runtime.NumGoroutine() <= before+2
		})
	})
	return app, host
}

// openShown opens one shown app window, failing on creation errors.
func openShown(t *testing.T, app *gpui.App, title string, w, h float32) *gpui.Window {
	t.Helper()
	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Activate:  true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow(%q): %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	waitFor(t, 10*time.Second, "the window to be visible", window.Handle().Visible)
	return window
}

// newRenderer creates the pacing renderer and closes it at cleanup.
func newRenderer(t *testing.T) *gpui.Renderer {
	t.Helper()
	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	return renderer
}

// sync drives f on the foreground (host) thread and waits for it (the
// a11yspec fixture pattern; app-state mutation belongs there).
func sync(t *testing.T, host *gpui.Host, run func()) {
	t.Helper()
	done := make(chan struct{})
	if !host.Post(func() {
		defer close(done)
		run()
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	<-done
}

// presentChanges drives count distinct clear-frame presents on the
// window (each one changes the monitor composition, so the capture
// producer delivers frames) and waits for the pacing worker to retire
// them all.
func presentChanges(t *testing.T, renderer *gpui.Renderer, window *gpui.Window, count int, interval time.Duration) {
	t.Helper()
	for i := 0; i < count; i++ {
		color := gpui.Color{
			R: float32(i%8) / 8.0,
			G: 0.25 + float32(i%3)/8.0,
			B: 0.5 + float32(i%5)/8.0,
			A: 1,
		}
		if _, err := renderer.PresentClear(window, color); err != nil {
			t.Fatalf("present %d: %v", i, err)
		}
		time.Sleep(interval)
	}
	waitFor(t, 15*time.Second, "the presents to retire", func() bool { return renderer.InFlight() == 0 })
}

// mainSource resolves the primary monitor's capture source through the
// public app API (on the foreground thread).
func mainSource(t *testing.T, app *gpui.App, host *gpui.Host) gpui.ScreenCaptureSource {
	t.Helper()
	var sources []gpui.ScreenCaptureSource
	var err error
	sync(t, host, func() {
		sources, err = app.ScreenCaptureSources()
	})
	if err != nil {
		t.Fatalf("app.ScreenCaptureSources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no capturable monitor sources — this machine is expected to enumerate at least the primary monitor")
	}
	for _, source := range sources {
		if source.IsMain {
			return source
		}
	}
	t.Fatalf("none of the %d enumerated sources is the main monitor", len(sources))
	return gpui.ScreenCaptureSource{}
}

// createMainStream opens the capture stream on the primary monitor
// (on the foreground thread) and destroys it at cleanup. Callers keep
// at most one live stream (the slot-0 payload discipline).
func createMainStream(t *testing.T, app *gpui.App, host *gpui.Host) *gpui.ScreenCaptureStream {
	t.Helper()
	source := mainSource(t, app, host)
	var stream *gpui.ScreenCaptureStream
	var err error
	sync(t, host, func() {
		stream, err = app.ScreenCaptureStream(source)
	})
	if err != nil {
		t.Fatalf("app.ScreenCaptureStream(main): %v", err)
	}
	t.Cleanup(func() {
		if dErr := stream.Destroy(); dErr != nil && !errors.Is(dErr, gpui.ErrCaptureStaleHandle) {
			t.Errorf("stream destroy: %v", dErr)
		}
	})
	return stream
}

// isTypedPollError reports whether err is one of the stream's legal
// poll outcomes (nothing pending, the stopped record, the stale record
// of a destroyed stream, or the capacity retry record).
func isTypedPollError(err error) bool {
	return err == nil ||
		errors.Is(err, gpui.ErrCaptureStopped) ||
		errors.Is(err, gpui.ErrCaptureStaleHandle) ||
		errors.Is(err, gpui.ErrCaptureCapacity) ||
		errors.Is(err, gpui.ErrCaptureBadHandle)
}

// ---------------------------------------------------------------------------
// Consent, availability and source discovery
// ---------------------------------------------------------------------------

// TestScreenCaptureSupportedAndSourceEnumeration records the capture
// consent/availability honestly and enumerates the monitor sources
// with the ABI's capacity protocol: the native probe answers
// Supported, a zero-capacity enumeration reports the typed capacity
// error with the needs (nothing enumerated), and the app-level
// enumeration resolves the pinned SourceMetadata (resolution, label,
// is-main, 1-based id).
func TestScreenCaptureSupportedAndSourceEnumeration(t *testing.T) {
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	svc, err := lib.Capture()
	if err != nil {
		t.Fatalf("lib.Capture(): %v — the rev-10 artifact must carry the capture service in slot 10", err)
	}
	identity := lib.Identity()
	t.Logf("capture artifact: %s rev %d sha %s capabilities %#x (%v)",
		identity.Name, identity.NativeRevision, identity.SHA256, identity.Capabilities, identity.CapabilityNames)

	// The honest availability record (a false here is a blocked
	// environment, which FAILS the suite — never a skip).
	supported, err := svc.Supported()
	if err != nil {
		t.Fatalf("capture Supported: %v", err)
	}
	if !supported {
		t.Fatal("Windows Graphics Capture is unavailable on this machine (Supported = false): the blocked-environment record, not passed capture behavior — this session is expected to capture")
	}
	t.Log("capture consent/availability: Supported() = true (monitor items resolve; no consent dialog required for monitor capture)")

	// The ABI capacity protocol: a zero-capacity enumeration reports
	// the typed capacity error with the record and string needs and
	// enumerates nothing.
	_, count, stringsNeeded, err := svc.Sources(nil, nil)
	if !errors.Is(err, native.ErrCaptureCapacity) {
		t.Fatalf("Sources(nil, nil) = (%d, %d, %v), want the typed capacity error", count, stringsNeeded, err)
	}
	if count == 0 {
		t.Fatal("the capacity probe reported zero monitor records")
	}
	t.Logf("capacity probe: %d monitors, %d label bytes needed", count, stringsNeeded)

	// The app-level enumeration resolves the pinned metadata.
	app, host := bootApp(t)
	var sources []gpui.ScreenCaptureSource
	var appErr error
	sync(t, host, func() {
		sources, appErr = app.ScreenCaptureSources()
	})
	if appErr != nil {
		t.Fatalf("app.ScreenCaptureSources: %v", appErr)
	}
	if len(sources) == 0 {
		t.Fatal("the app-level enumeration found no monitor sources")
	}
	mains := 0
	for i, source := range sources {
		if source.HMonitor == 0 || source.Width <= 0 || source.Height <= 0 {
			t.Fatalf("source %d = %+v: a monitor source must carry a handle and a positive resolution", i, source)
		}
		if source.ID != uint64(i+1) {
			t.Errorf("source %d has id %d, want the 1-based %d", i, source.ID, i+1)
		}
		if source.IsMain {
			mains++
		}
		t.Logf("capture source %d: hmonitor=%#x %dx%d main=%v label=%q",
			source.ID, source.HMonitor, source.Width, source.Height, source.IsMain, source.Label)
	}
	if mains != 1 {
		t.Errorf("enumerated %d main monitors, want exactly one (MONITORINFOF_PRIMARY)", mains)
	}

	// A zero monitor handle is the typed bad-value record before any
	// native work.
	var streamErr error
	sync(t, host, func() {
		_, streamErr = app.ScreenCaptureStream(gpui.ScreenCaptureSource{})
	})
	if !errors.Is(streamErr, gpui.ErrCaptureBadValue) {
		t.Fatalf("ScreenCaptureStream(zero monitor) = %v, want the typed bad-value error", streamErr)
	}
}

// ---------------------------------------------------------------------------
// The stream lifecycle: frames, the bounded published set, release
// ---------------------------------------------------------------------------

// TestCaptureStreamLifecycleFramesAndRelease drives the whole stream
// lifecycle on the primary monitor: the first frame's record (the pool
// size, the content-size crop, the timestamp), the bounded published
// set of one, the release of the superseded frame (the observable
// frame-release when the scene stops referencing it), the stop record,
// the destroy teardown and the slot reuse.
func TestCaptureStreamLifecycleFramesAndRelease(t *testing.T) {
	app, host := bootApp(t)
	window := openShown(t, app, "capturespec lifecycle", 300, 200)
	renderer := newRenderer(t)
	stream := createMainStream(t, app, host)

	// The capture item's initial size (the pool is created at it).
	initialWidth, initialHeight := stream.InitialSize()
	if initialWidth <= 0 || initialHeight <= 0 {
		t.Fatalf("the stream's initial item size = %dx%d, want positive", initialWidth, initialHeight)
	}

	// The first frame: drive one monitor change (a clear-frame present
	// on a real window) so a frame arrives deterministically, then poll.
	presentChanges(t, renderer, window, 3, 30*time.Millisecond)
	frame, err := stream.PollForFrame(0, 10*time.Second)
	if err != nil {
		t.Fatalf("PollForFrame(first): %v", err)
	}
	if frame.Width == 0 || frame.Height == 0 {
		t.Fatalf("the first frame's pool size = %dx%d, want the monitor resolution", frame.Width, frame.Height)
	}
	if frame.ContentWidth <= 0 || frame.ContentHeight <= 0 {
		t.Fatalf("the first frame's content size = %dx%d, want positive (the caller's crop)", frame.ContentWidth, frame.ContentHeight)
	}
	t.Logf("first frame: handle=%#x pool=%dx%d content=%dx%d timestamp=%d",
		frame.Handle, frame.Width, frame.Height, frame.ContentWidth, frame.ContentHeight, frame.Timestamp)

	stats, err := stream.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Published != 1 {
		t.Errorf("after the first poll the published count = %d, want 1 (the bounded published set)", stats.Published)
	}
	if stats.Pending > 8 {
		t.Errorf("the pending queue depth = %d, want at most the native bound of 8", stats.Pending)
	}
	if stats.PoolWidth != frame.ContentWidth || stats.PoolHeight != frame.ContentHeight {
		t.Logf("pool %dx%d tracks content %dx%d (equal in the steady state; a difference is the transient recreate window)",
			stats.PoolWidth, stats.PoolHeight, frame.ContentWidth, frame.ContentHeight)
	}

	// The release observable: drive new frames; the poll that installs
	// the newer current frame releases the superseded one.
	presentChanges(t, renderer, window, 4, 30*time.Millisecond)
	next, err := stream.PollForFrame(frame.Handle, 10*time.Second)
	if err != nil {
		t.Fatalf("PollForFrame(second): %v", err)
	}
	if next.Handle == frame.Handle {
		t.Fatalf("the second frame handle %#x equals the first; the stream did not advance", next.Handle)
	}
	stats, err = stream.Stats()
	if err != nil {
		t.Fatalf("Stats after advance: %v", err)
	}
	if stats.Published != 1 {
		t.Errorf("after the advance the published count = %d, want 1 (only the current frame stays published)", stats.Published)
	}
	if stats.FramesReleased < 1 {
		t.Errorf("the released-frame counter = %d, want at least the superseded first frame", stats.FramesReleased)
	}
	t.Logf("advanced to frame %#x; released=%d dropped=%d size-changes=%d",
		next.Handle, stats.FramesReleased, stats.Dropped, stats.SizeChanges)

	// The superseded frame's handle is genuinely released: a direct
	// native release of it is the typed bad-handle record.
	svc, err := app.CaptureService()
	if err != nil {
		t.Fatalf("app.CaptureService: %v", err)
	}
	if err := svc.FrameRelease(native.CaptureFrameHandle(frame.Handle)); !errors.Is(err, native.ErrCaptureBadHandle) {
		t.Fatalf("FrameRelease(the superseded frame) = %v, want the typed bad-handle error (already released)", err)
	}

	// Stop: the session stops; nothing new arrives; the typed stopped
	// record surfaces on the drained poll.
	if err := stream.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := stream.Poll(); !errors.Is(err, gpui.ErrCaptureStopped) {
		t.Fatalf("Poll after stop = %v, want the typed stopped error", err)
	}
	if err := stream.Stop(); err != nil {
		t.Fatalf("the second Stop (idempotent) = %v, want nil", err)
	}

	// Destroy: the full teardown; every later call is the typed stale
	// record.
	if err := stream.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := stream.Poll(); !errors.Is(err, gpui.ErrCaptureStaleHandle) {
		t.Errorf("Poll after destroy = %v, want the typed stale-handle error", err)
	}
	if _, err := stream.Stats(); !errors.Is(err, gpui.ErrCaptureStaleHandle) {
		t.Errorf("Stats after destroy = %v, want the typed stale-handle error", err)
	}
	if err := stream.Stop(); !errors.Is(err, gpui.ErrCaptureStaleHandle) {
		t.Errorf("Stop after destroy = %v, want the typed stale-handle error", err)
	}
	if _, ok := stream.CurrentFrame(); ok {
		t.Error("CurrentFrame after destroy still reports a frame; the native destroy released its texture")
	}

	// Slot registry record: a fresh stream after a destroy is a NEW slot
	// (the registry appends slots until sixteen exist; vacancy-first
	// recycling starts only after that), and only slot-0 handles fit the
	// scene record's 32-bit payload — the observable the paint guard
	// records.
	var stream2 *gpui.ScreenCaptureStream
	var err2 error
	sync(t, host, func() {
		stream2, err2 = app.ScreenCaptureStream(stream.Source())
	})
	if err2 != nil {
		t.Fatalf("the follow-up stream: %v", err2)
	}
	frame2, err := stream2.PollForFrame(0, 10*time.Second)
	if err != nil {
		t.Fatalf("the follow-up stream's first frame: %v", err)
	}
	if frame2.Handle == 0 {
		t.Fatal("the follow-up stream published a zero handle")
	}
	paintable := frame2.Handle <= 0xFFFFFFFF
	t.Logf("slot registry: the follow-up stream's first frame handle=%#x (slot %d — a fresh slot, since the registry appends until sixteen exist; paintable through the 32-bit payload: %v)",
		frame2.Handle, frame2.Handle>>44, paintable)
	if err := stream2.Destroy(); err != nil {
		t.Fatalf("the follow-up stream destroy: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bounded pending frames: the drop-oldest counter
// ---------------------------------------------------------------------------

// TestCaptureBoundedPendingDropsOldest pins the bounded pending queue:
// while the app does not poll, monitor changes keep arriving, the
// queue holds at most the native bound (8) and the drop-oldest
// overflow is counted; the next poll drains a bounded batch and leaves
// exactly one published frame.
func TestCaptureBoundedPendingDropsOldest(t *testing.T) {
	app, host := bootApp(t)
	window := openShown(t, app, "capturespec drop counter", 300, 200)
	renderer := newRenderer(t)
	stream := createMainStream(t, app, host)

	// Drive many monitor changes WITHOUT polling: 60 distinct
	// clear-frame presents at 20ms each.
	presentChanges(t, renderer, window, 60, 20*time.Millisecond)

	stats, err := stream.Stats()
	if err != nil {
		t.Fatalf("Stats before the drain: %v", err)
	}
	if stats.Pending > 8 {
		t.Fatalf("the pending queue depth = %d, want at most the native bound of 8", stats.Pending)
	}
	if stats.Dropped < 1 {
		t.Fatalf("the drop counter = %d after 60 un-polled monitor changes, want at least one dropped overflow (the drop-oldest rule)", stats.Dropped)
	}
	t.Logf("bounded pending: pending=%d dropped=%d (60 changes while not polling)", stats.Pending, stats.Dropped)

	// The drain: a bounded batch, the newest frame published.
	frames, err := stream.Poll()
	if err != nil {
		t.Fatalf("Poll after the overflow: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the drain returned no frames; the queue held pending frames")
	}
	if len(frames) > 8 {
		t.Fatalf("the drain returned %d frames, want at most the native bound of 8", len(frames))
	}
	stats, err = stream.Stats()
	if err != nil {
		t.Fatalf("Stats after the drain: %v", err)
	}
	if stats.Published != 1 {
		t.Errorf("after the drain the published count = %d, want 1 (the newest frame only)", stats.Published)
	}
	if current, ok := stream.CurrentFrame(); !ok || current.Handle != frames[len(frames)-1].Handle {
		t.Errorf("the current frame = (%#x, %v), want the newest drained frame %#x", current.Handle, ok, frames[len(frames)-1].Handle)
	}
	t.Logf("drained %d frames; the current frame is %#x, released=%d", len(frames), frames[len(frames)-1].Handle, stats.FramesReleased)
}

// ---------------------------------------------------------------------------
// Stop/close races
// ---------------------------------------------------------------------------

// TestCaptureStopCloseRaces exercises the stop/close race surface: a
// stop while frames are pending keeps them drainable (the final
// drain), the stopped record surfaces once drained, and a concurrent
// poll/stop/destroy hammer never panics or hangs — every outcome is a
// typed record.
func TestCaptureStopCloseRaces(t *testing.T) {
	app, host := bootApp(t)
	window := openShown(t, app, "capturespec races", 300, 200)
	renderer := newRenderer(t)
	stream := createMainStream(t, app, host)

	// Fill the queue without polling, then stop: the pending frames
	// must remain drainable (the pin's queue keeps what was already
	// enqueued).
	presentChanges(t, renderer, window, 12, 20*time.Millisecond)
	if err := stream.Stop(); err != nil {
		t.Fatalf("Stop with pending frames: %v", err)
	}
	frames, err := stream.Poll()
	if err != nil {
		t.Fatalf("the final drain after stop: %v (pending frames must stay drainable)", err)
	}
	if len(frames) == 0 {
		t.Fatal("the final drain after stop returned no frames; the queue held pending frames")
	}
	t.Logf("the stopped stream drained %d pending frames", len(frames))
	if _, err := stream.Poll(); !errors.Is(err, gpui.ErrCaptureStopped) {
		t.Fatalf("the drained stopped poll = %v, want the typed stopped record", err)
	}

	// The concurrent hammer: one goroutine polls, one stops, one
	// destroys — serialized through the stream's guard, every outcome
	// typed, no panic, no hang.
	const pollers = 3
	results := make(chan error, pollers+2)
	for p := 0; p < pollers; p++ {
		go func() {
			for i := 0; i < 20; i++ {
				if _, err := stream.Poll(); err != nil && !isTypedPollError(err) {
					results <- fmt.Errorf("unexpected poll outcome: %w", err)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			results <- nil
		}()
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		if err := stream.Stop(); err != nil && !errors.Is(err, gpui.ErrCaptureStaleHandle) {
			results <- fmt.Errorf("concurrent stop: %w", err)
			return
		}
		results <- nil
	}()
	go func() {
		time.Sleep(80 * time.Millisecond)
		if err := stream.Destroy(); err != nil {
			results <- fmt.Errorf("concurrent destroy: %w", err)
			return
		}
		results <- nil
	}()
	for i := 0; i < pollers+2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("the concurrent hammer hung")
		}
	}

	// After the destroy every entry is the typed stale record.
	if _, err := stream.Stats(); !errors.Is(err, gpui.ErrCaptureStaleHandle) {
		t.Errorf("Stats after the race = %v, want the typed stale-handle error", err)
	}
}

// ---------------------------------------------------------------------------
// Occlusion-agnostic monitor capture
// ---------------------------------------------------------------------------

// TestCaptureOcclusionAgnosticMonitorCapture proves the monitor stream
// keeps delivering frames while the application's own window is fully
// occluded by another window: the capture is of the MONITOR, not of a
// window, so occluding one of the app's windows neither pauses nor
// stalls the session (the visible sibling's composition changes drive
// the arrivals; whether DWM also composes the occluded window's
// presents is DWM's choice, never the stream's).
func TestCaptureOcclusionAgnosticMonitorCapture(t *testing.T) {
	app, host := bootApp(t)
	bounds := gpui.Bounds{Origin: gpui.Point{X: 60, Y: 60}, Size: gpui.Size{Width: 260, Height: 180}}
	occluded, err := app.OpenWindow(gpui.WindowOptions{
		Title:    "capturespec occluded",
		Show:     true,
		Activate: true,
		Bounds:   bounds,
	})
	if err != nil {
		t.Fatalf("open the occluded window: %v", err)
	}
	occluder, err := app.OpenWindow(gpui.WindowOptions{
		Title:  "capturespec occluder",
		Show:   true,
		Bounds: bounds, // the same bounds, opened later: fully on top
	})
	if err != nil {
		t.Fatalf("open the occluder: %v", err)
	}
	t.Cleanup(func() {
		for _, window := range []*gpui.Window{occluder, occluded} {
			if window.Alive() {
				window.Close()
			}
		}
		waitFor(t, 10*time.Second, "both windows to be destroyed", func() bool {
			return !occluder.Alive() && !occluded.Alive()
		})
	})
	waitFor(t, 10*time.Second, "both windows to be visible", func() bool {
		return occluded.Handle().Visible() && occluder.Handle().Visible()
	})

	renderer := newRenderer(t)
	// A static dark base on the occluder, then the OCCLUDED window
	// presents underneath it.
	if _, err := renderer.PresentClear(occluder, gpui.Color{R: 0.1, G: 0.1, B: 0.12, A: 1}); err != nil {
		t.Fatalf("the occluder's base present: %v", err)
	}
	stream := createMainStream(t, app, host)

	// Present changes from the OCCLUDED window (under the occluder) while
	// polling the monitor stream; the driver stops before the test ends.
	driver := make(chan struct{})
	go func() {
		defer close(driver)
		presentChangesQuiet(renderer, occluded, 30, 40*time.Millisecond)
	}()
	first, err := stream.PollForFrame(0, 10*time.Second)
	if err != nil {
		t.Fatalf("the first frame with the occluded window presenting: %v", err)
	}
	next, err := stream.PollForFrame(first.Handle, 10*time.Second)
	if err != nil {
		t.Fatalf("no further frame arrived while the app window was occluded: %v", err)
	}
	select {
	case <-driver:
	case <-time.After(15 * time.Second):
		t.Fatal("the occlusion driver did not finish")
	}
	stats, statsErr := stream.Stats()
	if statsErr != nil {
		t.Fatalf("Stats: %v", statsErr)
	}
	t.Logf("occlusion-agnostic: frames %#x → %#x while the app window was occluded (timestamps %d → %d, dropped=%d)",
		first.Handle, next.Handle, first.Timestamp, next.Timestamp, stats.Dropped)
	if next.Timestamp != 0 && next.Timestamp <= first.Timestamp && next.Handle != first.Handle {
		t.Errorf("the new frame's timestamp %d did not advance past %d", next.Timestamp, first.Timestamp)
	}
}

// presentChangesQuiet is presentChanges without the test-fatal
// assertions (for background driver goroutines); failures surface
// through the stream's own gate.
func presentChangesQuiet(renderer *gpui.Renderer, window *gpui.Window, count int, interval time.Duration) {
	for i := 0; i < count; i++ {
		_, _ = renderer.PresentClear(window, gpui.Color{
			R: float32(i%8) / 8.0,
			G: 0.3,
			B: 0.6,
			A: 1,
		})
		time.Sleep(interval)
	}
}
