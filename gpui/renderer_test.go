//go:build windows

package gpui

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"gpui-go/internal/native"
)

// Renderer tests (ticket07): real windows through the real Win32 host,
// real GPU submissions through the embedded native D3D11 service. The
// deterministic flows use NewRendererManual (no pacing worker) so the
// test itself polls and retires; the pacing-worker path is exercised by
// TestRendererPacingWorkerRetiresAndDispatches. Environment expectation:
// this machine runs interactively with a D3D11-capable adapter — window
// creation failures fail the tests instead of skipping.

// newAppWithHost boots a real application with a started host, driving
// Run in a goroutine (the winhostspec boot pattern). The cleanup stops
// the host and checks the goroutine baseline.
func newAppWithHost(t *testing.T) (*App, *Host) {
	t.Helper()
	before := runtime.NumGoroutine()
	app := NewApp()
	h := NewHost()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(h) }()
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
		waitForGoroutines(t, before)
	})
	return app, h
}

// waitForGoroutines waits for the goroutine count to fall back near the
// baseline (the host thread joined; the renderer pacing worker joined).
func waitForGoroutines(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutines did not drain back near the baseline: before=%d now=%d", before, runtime.NumGoroutine())
}

// openAppWindow opens one shown app window, failing on creation errors.
func openAppWindow(t *testing.T, app *App, title string, w, h float32, opts ...func(*WindowOptions)) *Window {
	t.Helper()
	options := WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    Bounds{Size: Size{Width: w, Height: h}},
	}
	for _, opt := range opts {
		opt(&options)
	}
	window, err := app.OpenWindow(options)
	if err != nil {
		t.Fatalf("app.OpenWindow(%q): %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return window
}

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

// newManualRenderer creates a renderer without the pacing worker and
// closes it at cleanup (surfaces must have drained by then).
func newManualRenderer(t *testing.T) *Renderer {
	t.Helper()
	renderer, err := NewRendererManual()
	if err != nil {
		t.Fatalf("NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	return renderer
}

// logDeviceIdentity records the environment identity of the shared device
// (evidence for the ticket).
func logDeviceIdentity(t *testing.T, renderer *Renderer) *native.DeviceInfo {
	t.Helper()
	info, err := renderer.DeviceInfo()
	if err != nil {
		t.Fatalf("DeviceInfo: %v", err)
	}
	t.Logf("device identity: adapter=%q vendor=0x%x device=0x%x feature-level=0x%x software=%v vram=%d driver=%q version=%q multithread=%v",
		info.AdapterDescription, info.VendorID, info.DeviceID, info.FeatureLevel, info.Software,
		info.DedicatedVideoMemory, info.DriverName, info.DriverVersion, info.MultithreadProtected)
	if info.AdapterDescription == "" || info.DriverName == "" || info.DriverVersion == "" {
		t.Fatalf("device identity incomplete: %+v", info)
	}
	if info.FeatureLevel != 0xb000 && info.FeatureLevel != 0xb100 {
		t.Errorf("unexpected feature level %#x (the pin restricts creation to 11.0/11.1)", info.FeatureLevel)
	}
	if !info.MultithreadProtected {
		t.Errorf("immediate-context multithread protection not enabled")
	}
	return info
}

// presentAndWait submits a clear frame and drives it to completion with
// bounded polls, returning the final state record. pendingSeen reports
// whether at least one S_FALSE observation happened (timing-dependent,
// never required).
func presentAndWait(t *testing.T, renderer *Renderer, window *Window, color Color, timeout time.Duration) (sub *Submission, final native.SubmissionStateRecord, pendingSeen bool) {
	t.Helper()
	sub, err := renderer.PresentClear(window, color)
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}
	// One immediate poll (the delayed-completion probe): likely S_FALSE.
	state, err := sub.Poll()
	if err != nil {
		t.Fatalf("immediate poll: %v", err)
	}
	if state.State == native.SubmissionPending {
		pendingSeen = true
	}
	final, err = sub.WaitForCompletion(timeout)
	if err != nil {
		t.Fatalf("WaitForCompletion: %v (last state %d)", err, final.State)
	}
	if final.State != native.SubmissionCompleted {
		t.Fatalf("submission ended in state %d, want completed", final.State)
	}
	return sub, final, pendingSeen
}

// TestRendererDCompClearFramePresentAndRetire is the ticket's flow 1: a
// DComp-premultiplied surface on a real window presents a red clear
// frame, the poll observes completion (S_OK; S_FALSE observations are
// recorded but not required), the retire produces the terminal record,
// the second retire is a typed error, and polling a retired submission
// is a typed error.
func TestRendererDCompClearFramePresentAndRetire(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer dcomp", 320, 200)
	renderer := newManualRenderer(t)
	logDeviceIdentity(t, renderer)

	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface info: %v", err)
	}
	t.Logf("surface info: mode=%d format=%d buffers=%d %dx%d feature-level=0x%x resize-gen=%d",
		info.Mode, info.Format, info.BufferCount, info.Width, info.Height, info.FeatureLevel, info.ResizeGeneration)
	// The pinned swap-chain facts.
	if info.Mode != uint32(native.SurfaceModeDCompPremultiplied) {
		t.Errorf("surface mode = %d, want DComp premultiplied", info.Mode)
	}
	if info.Format != native.RenderTargetFormatValue {
		t.Errorf("surface format = %d, want %d (DXGI_FORMAT_B8G8R8A8_UNORM)", info.Format, native.RenderTargetFormatValue)
	}
	if info.BufferCount != native.RenderBufferCount {
		t.Errorf("buffer count = %d, want %d (the pin's flip-sequential triple buffering)", info.BufferCount, native.RenderBufferCount)
	}
	if info.Width == 0 || info.Height == 0 {
		t.Fatalf("implausible surface extent %dx%d", info.Width, info.Height)
	}

	sub, _, pendingSeen := presentAndWait(t, renderer, window, Color{R: 1, G: 0, B: 0, A: 1}, 10*time.Second)
	if pendingSeen {
		t.Logf("observed at least one S_FALSE pending poll before completion")
	} else {
		t.Logf("no S_FALSE observation captured (the GPU finished the clear before the first poll)")
	}

	record, err := sub.Retire()
	if err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if record.Terminal != native.RetireCompleted {
		t.Fatalf("retire terminal = %d, want completed", record.Terminal)
	}

	// Exactly-once retire: the second call is the typed stale-handle
	// error.
	_, err = sub.Retire()
	if !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("second retire error = %v, want the native stale-handle error", err)
	}

	// Polling a retired submission is a typed error.
	_, err = sub.Poll()
	if !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("poll of a retired submission = %v, want the native stale-handle error", err)
	}

	// Ledger invariants for this flow.
	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 1 || retired != 1 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 1/1/0/0", accepted, retired, quarantined, inFlight)
	}
	var completedBeforeRetire bool
	for _, event := range events {
		if event.Kind == LedgerRetired {
			break
		}
		if event.Kind == LedgerPollCompleted {
			completedBeforeRetire = true
		}
	}
	if !completedBeforeRetire {
		t.Fatalf("the retire event was not preceded by a completed poll event: %+v", events)
	}

	// Surface destroy succeeds once drained.
	if err := surface.Destroy(); err != nil {
		t.Fatalf("surface destroy: %v", err)
	}
}

// TestRendererHwndAlphaIgnoreMode is the ticket's flow 2: the same
// present/poll/retire path on an HWND alpha-ignore surface (the pin's
// DirectComposition-disabled mode).
func TestRendererHwndAlphaIgnoreMode(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer hwnd", 280, 180)
	renderer := newManualRenderer(t)

	surface, err := renderer.GetOrCreateSurfaceMode(window, native.SurfaceModeHwndAlphaIgnore)
	if err != nil {
		t.Fatalf("GetOrCreateSurfaceMode(HWND): %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface info: %v", err)
	}
	if info.Mode != uint32(native.SurfaceModeHwndAlphaIgnore) {
		t.Fatalf("surface mode = %d, want HWND alpha-ignore", info.Mode)
	}
	if info.Format != native.RenderTargetFormatValue || info.BufferCount != native.RenderBufferCount {
		t.Fatalf("HWND surface format/buffers = %d/%d, want the pinned values", info.Format, info.BufferCount)
	}

	sub, _, _ := presentAndWait(t, renderer, window, Color{R: 0, G: 0, B: 1, A: 1}, 10*time.Second)
	record, err := sub.Retire()
	if err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if record.Terminal != native.RetireCompleted {
		t.Fatalf("retire terminal = %d, want completed", record.Terminal)
	}
	if _, err := sub.Retire(); !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("second retire = %v, want stale-handle", err)
	}
	if err := surface.Destroy(); err != nil {
		t.Fatalf("surface destroy: %v", err)
	}
}

// TestRendererTwoWindowsInterleavedConcurrent is the ticket's flow 3: two
// windows with surfaces, interleaved presents from two goroutines, all
// retired — immediate-context serialization is verified by both
// concurrent present/retire paths succeeding in order (the native device
// mutex serializes; nothing deadlocks or corrupts).
func TestRendererTwoWindowsInterleavedConcurrent(t *testing.T) {
	app, _ := newAppWithHost(t)
	window1 := openAppWindow(t, app, "gpui renderer two 1", 300, 200)
	window2 := openAppWindow(t, app, "gpui renderer two 2", 260, 160)
	renderer := newManualRenderer(t)

	surface1, err := renderer.GetOrCreateSurface(window1)
	if err != nil {
		t.Fatalf("surface one: %v", err)
	}
	surface2, err := renderer.GetOrCreateSurfaceMode(window2, native.SurfaceModeHwndAlphaIgnore)
	if err != nil {
		t.Fatalf("surface two: %v", err)
	}
	if surface1.Hwnd() == surface2.Hwnd() {
		t.Fatalf("two surfaces share an HWND")
	}

	// Concurrent presents from two goroutines, interleaved rounds: both
	// must succeed (serialized by the native device mutex).
	const rounds = 4
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)
	for round := 0; round < rounds; round++ {
		for _, surface := range []*Surface{surface1, surface2} {
			surface := surface
			wg.Add(1)
			go func() {
				defer wg.Done()
				color := Color{R: float32(round+1) / rounds, G: 0.5, B: 0.2, A: 1}
				sub, err := surface.PresentClear(color)
				if err != nil {
					errs <- err
					return
				}
				if _, err := sub.WaitForCompletion(10 * time.Second); err != nil {
					errs <- err
					return
				}
				if _, err := sub.Retire(); err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
	}
	close(errs)
	for err := range errs {
		t.Errorf("concurrent present/retire: %v", err)
	}

	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != rounds*2 || retired != rounds*2 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want %d/%d/0/0",
			accepted, retired, quarantined, inFlight, rounds*2, rounds*2)
	}
	if len(events) < accepted+retired {
		t.Fatalf("ledger trace too short: %d events for %d accepts + %d retires", len(events), accepted, retired)
	}
	t.Logf("two-window trace: %d events, %d accepted, %d retired", len(events), accepted, retired)
}

// TestRendererResizeExercisesSwapChainResize is the ticket's flow 4:
// present, resize the window, present again at the new extent (the
// auto-resize at the safe point runs the native ResizeBuffers path), and
// retire both submissions.
func TestRendererResizeExercisesSwapChainResize(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer resize", 320, 200)
	renderer := newManualRenderer(t)

	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	infoBefore, err := surface.Info()
	if err != nil {
		t.Fatalf("info before: %v", err)
	}

	sub1, _, _ := presentAndWait(t, renderer, window, Color{R: 1, G: 1, B: 0, A: 1}, 10*time.Second)
	if _, err := sub1.Retire(); err != nil {
		t.Fatalf("retire before resize: %v", err)
	}

	// Resize the window through the real path, then present again: the
	// auto-resize runs at the present boundary (the safe point), and the
	// surface reports the new device-pixel extent and a bumped resize
	// generation afterwards.
	window.SetBounds(Bounds{Size: Size{Width: 480, Height: 320}})
	waitFor(t, 10*time.Second, "the window to report the resized bounds", func() bool {
		bounds, err := window.Bounds()
		return err == nil && bounds.Size.Width > 400 && bounds.Size.Height > 280
	})

	// Present at the new extent and retire; the present's safe point ran
	// the native ResizeBuffers path.
	sub2, _, _ := presentAndWait(t, renderer, window, Color{R: 0, G: 1, B: 1, A: 1}, 10*time.Second)
	if _, err := sub2.Retire(); err != nil {
		t.Fatalf("retire after resize: %v", err)
	}
	infoAfter, err := surface.Info()
	if err != nil {
		t.Fatalf("info after resize: %v", err)
	}
	t.Logf("resize: %dx%d (gen %d) -> %dx%d (gen %d)", infoBefore.Width, infoBefore.Height,
		infoBefore.ResizeGeneration, infoAfter.Width, infoAfter.Height, infoAfter.ResizeGeneration)
	if infoAfter.Width <= infoBefore.Width || infoAfter.Height <= infoBefore.Height {
		t.Fatalf("surface extent did not grow after the window resize: %dx%d -> %dx%d",
			infoBefore.Width, infoBefore.Height, infoAfter.Width, infoAfter.Height)
	}
	if infoAfter.ResizeGeneration <= infoBefore.ResizeGeneration {
		t.Fatalf("resize generation did not bump: %d -> %d", infoBefore.ResizeGeneration, infoAfter.ResizeGeneration)
	}
}

// TestRendererHiddenWindowMarkerStillRetires is the ticket's flow 5: with
// the window hidden, a present still submits its marker (the present may
// be composited differently or not at all, but the query+flush path ran)
// and retirement happens only on a real completed poll. The observed
// behavior is recorded honestly.
func TestRendererHiddenWindowMarkerStillRetires(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer hidden", 240, 160)
	renderer := newManualRenderer(t)

	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}

	window.Hide()
	waitFor(t, 10*time.Second, "the window to hide", func() bool { return !window.Handle().Visible() })

	sub, err := surface.PresentClear(Color{R: 0.2, G: 0.4, B: 0.6, A: 1})
	if err != nil {
		t.Fatalf("PresentClear on the hidden window: %v", err)
	}
	// The honest observation: poll until completion or timeout; a hidden
	// window must never be falsely retired (retire requires the completed
	// poll).
	state, err := sub.WaitForCompletion(10 * time.Second)
	if err != nil {
		// Pending forever would be recorded as such; retirement is then
		// correctly refused (never a false retire).
		t.Logf("hidden-window submission did not complete: %v (state %d) — not retired, as required", err, state.State)
		if _, err := sub.Retire(); !errors.Is(err, ErrRendererRetireNotCompleted) {
			t.Fatalf("retire of an unfinished hidden-window submission = %v, want the not-completed typed error", err)
		}
		return
	}
	t.Logf("hidden-window submission completed (state %d, data_hr %#x) — the marker was submitted and retired via a real poll", state.State, state.DataHR)
	record, err := sub.Retire()
	if err != nil {
		t.Fatalf("retire after hidden-window completion: %v", err)
	}
	if record.Terminal != native.RetireCompleted {
		t.Fatalf("terminal = %d, want completed", record.Terminal)
	}
}

// TestRendererDelayedCompletionLedgerOrdering is the ticket's flow 6:
// poll immediately after present (likely S_FALSE — pending, not
// retired), wait, poll again (completed). The ledger must show
// pending-then-completed, never completed-before-poll, and retiring
// before completion is refused with the typed error.
func TestRendererDelayedCompletionLedgerOrdering(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer delayed", 260, 180)
	renderer := newManualRenderer(t)

	sub, err := renderer.PresentClear(window, Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}

	// Retiring before any completed poll is refused (acceptance is not
	// completion).
	if _, err := sub.Retire(); !errors.Is(err, ErrRendererRetireNotCompleted) {
		t.Fatalf("retire before completion = %v, want ErrRendererRetireNotCompleted", err)
	}

	// The immediate poll: the first observation.
	first, err := sub.Poll()
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	t.Logf("immediate poll: state=%d data_hr=%#x (1 = S_FALSE pending)", first.State, first.DataHR)

	if _, err := sub.WaitForCompletion(10 * time.Second); err != nil {
		t.Fatalf("WaitForCompletion: %v", err)
	}
	entry, ok := renderer.LedgerEntry(sub.ID())
	if !ok {
		t.Fatalf("ledger entry missing for submission %d", sub.ID())
	}
	// The entry reached "completed" through the observed poll, never
	// before it.
	if entry.State != "completed" {
		t.Fatalf("ledger state = %q, want completed", entry.State)
	}
	if entry.PollCount < 1 || entry.CompletedAt.IsZero() {
		t.Fatalf("ledger entry has no observed completion: %+v", entry)
	}
	if entry.CompletedAt.Before(entry.AcceptedAt) {
		t.Fatalf("completed before accepted: %+v", entry)
	}
	if first.State == native.SubmissionPending && !entry.PendingObserved {
		t.Fatalf("the ledger missed the observed pending state: %+v", entry)
	}

	if _, err := sub.Retire(); err != nil {
		t.Fatalf("retire after completion: %v", err)
	}
	// The trace is ordered: accepted, (poll events), retired.
	events, accepted, retired, _, inFlight := renderer.Ledger()
	if accepted != 1 || retired != 1 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d in-flight=%d, want 1/1/0", accepted, retired, inFlight)
	}
	kinds := make([]LedgerEventKind, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	t.Logf("ledger trace: %v", kinds)
	if len(kinds) < 3 || kinds[0] != LedgerAccepted || kinds[len(kinds)-1] != LedgerRetired {
		t.Fatalf("ledger trace is not ordered: %v", kinds)
	}
}

// TestRendererPacingWorkerRetiresAndDispatches exercises the shipped
// default: a pacing Renderer whose background worker polls pending
// submissions with bounded nonblocking polls, retires completed ones, and
// dispatches the retirement records to the foreground via App.Update
// (the ledger events carry Foreground=true).
func TestRendererPacingWorkerRetiresAndDispatches(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer pacing", 300, 200)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})

	const count = 6
	subs := make([]*Submission, 0, count)
	for i := 0; i < count; i++ {
		sub, err := renderer.PresentClear(window, Color{R: float32(i) / count, G: 0.3, B: 0.7, A: 1})
		if err != nil {
			t.Fatalf("PresentClear %d: %v", i, err)
		}
		subs = append(subs, sub)
	}
	// The pacing worker owns polling/retirement; wait for every
	// submission to reach a terminal state.
	for _, sub := range subs {
		if err := sub.WaitForRetirement(15 * time.Second); err != nil {
			t.Fatalf("pacing worker did not retire submission %d: %v", sub.ID(), err)
		}
	}
	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != count || retired != count || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want %d/%d/0/0",
			accepted, retired, quarantined, inFlight, count, count)
	}
	retireEvents := 0
	for _, event := range events {
		if event.Kind != LedgerRetired {
			continue
		}
		retireEvents++
		if !event.Foreground {
			t.Errorf("retirement event for submission %d was not dispatched to the foreground", event.Submission)
		}
	}
	if retireEvents != count {
		t.Fatalf("foreground retirement events = %d, want %d", retireEvents, count)
	}
	// Every retire event was preceded by its completed poll event.
	completed := make(map[uint64]bool)
	for _, event := range events {
		switch event.Kind {
		case LedgerPollCompleted:
			completed[event.Submission] = true
		case LedgerRetired:
			if !completed[event.Submission] {
				t.Errorf("submission %d retired without a preceding completed poll event", event.Submission)
			}
		}
	}
}

// TestRendererBoundedInFlightBackpressure pins the typed backpressure:
// the per-surface in-flight bound (the native cap mirrored Go-side)
// rejects PresentClear before native entry while submissions are pending.
func TestRendererBoundedInFlightBackpressure(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer backpressure", 240, 160)
	renderer := newManualRenderer(t)

	subs := make([]*Submission, 0, renderer.MaxPendingSubmissions())
	for {
		sub, err := renderer.PresentClear(window, Color{R: 0.1, G: 0.2, B: 0.3, A: 1})
		if err != nil {
			if !errors.Is(err, ErrRendererInFlight) {
				t.Fatalf("PresentClear beyond the bound failed with %v, want ErrRendererInFlight", err)
			}
			break
		}
		subs = append(subs, sub)
		if len(subs) > renderer.MaxPendingSubmissions() {
			t.Fatalf("accepted %d submissions above the bound %d", len(subs), renderer.MaxPendingSubmissions())
		}
	}
	if len(subs) == 0 {
		t.Fatal("the first present was rejected; the bound check is broken")
	}
	t.Logf("backpressure after %d in-flight submissions (bound %d)", len(subs), renderer.MaxPendingSubmissions())
	// Draining restores acceptance.
	for _, sub := range subs {
		if _, err := sub.WaitForCompletion(10 * time.Second); err != nil {
			t.Fatalf("drain: %v", err)
		}
		if _, err := sub.Retire(); err != nil {
			t.Fatalf("retire during drain: %v", err)
		}
	}
	sub, err := renderer.PresentClear(window, Color{R: 1, G: 1, B: 1, A: 1})
	if err != nil {
		t.Fatalf("present after drain: %v", err)
	}
	if _, err := sub.WaitForCompletion(10 * time.Second); err != nil {
		t.Fatalf("completion after drain: %v", err)
	}
	if _, err := sub.Retire(); err != nil {
		t.Fatalf("retire after drain: %v", err)
	}
}

// TestRendererForcedGCDuringPresentAndPoll is the ticket's flow 9: forced
// GC cycles during present/poll — the ABI passes only value records, so
// no retained Go pointer can dangle.
func TestRendererForcedGCDuringPresentAndPoll(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer gc", 240, 160)
	renderer := newManualRenderer(t)

	for i := 0; i < 6; i++ {
		sub, err := renderer.PresentClear(window, Color{R: float32(i) / 6, G: 0.9, B: 0.1, A: 1})
		if err != nil {
			t.Fatalf("PresentClear %d: %v", i, err)
		}
		runtime.GC()
		runtime.GC()
		if _, err := sub.WaitForCompletion(10 * time.Second); err != nil {
			t.Fatalf("completion %d: %v", i, err)
		}
		runtime.GC()
		if _, err := sub.Retire(); err != nil {
			t.Fatalf("retire %d: %v", i, err)
		}
		runtime.GC()
	}
	_, accepted, retired, _, inFlight := renderer.Ledger()
	if accepted != 6 || retired != 6 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d in-flight=%d, want 6/6/0", accepted, retired, inFlight)
	}
}

// TestRendererSurfaceErrorPaths pins the ticket's flow 10 at the gpui
// level: a closed window refuses new surfaces, and a destroyed surface is
// rejected by the native service (the typed stale-handle error).
func TestRendererSurfaceErrorPaths(t *testing.T) {
	app, _ := newAppWithHost(t)
	window := openAppWindow(t, app, "gpui renderer errors", 240, 160)
	renderer := newManualRenderer(t)

	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	// Destroy with pending submissions is refused.
	sub, err := renderer.PresentClear(window, Color{R: 0.3, G: 0.6, B: 0.9, A: 1})
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}
	if err := surface.Destroy(); !errors.Is(err, native.ErrRendererPending) {
		t.Fatalf("destroy with a pending submission = %v, want ErrRendererPending", err)
	}
	if _, err := sub.WaitForCompletion(10 * time.Second); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if _, err := sub.Retire(); err != nil {
		t.Fatalf("retire: %v", err)
	}
	// Drained destroy succeeds.
	if err := surface.Destroy(); err != nil {
		t.Fatalf("drained destroy: %v", err)
	}
	// A present through the renderer on the destroyed surface is a
	// stale-handle typed error (the surface table entry is gone, so the
	// window gets a fresh surface — the old handle is unusable through
	// the public API; probe the stale handle directly).
	_, err = renderer.svc.PresentClear(surface.handle, Color{R: 1, G: 0, B: 1, A: 1}.toNative())
	if !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("present on the destroyed surface = %v, want the stale-handle error", err)
	}

	// A closed window lease refuses new surfaces.
	window.Close()
	waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
	_, err = renderer.GetOrCreateSurface(window)
	if !errors.Is(err, ErrRendererWindowClosed) {
		t.Fatalf("GetOrCreateSurface on a closed window = %v, want ErrRendererWindowClosed", err)
	}
}
