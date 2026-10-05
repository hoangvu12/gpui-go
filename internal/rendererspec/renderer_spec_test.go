//go:build windows

// Package rendererspec holds ticket07's renderer tests: real windows, a
// real D3D11 device and real clear-frame submissions through the public
// gpui API and the native ABI. They exercise the surfaces from outside
// the runtime package, matching the repo convention (see
// internal/winhostspec and internal/layoutspec).
//
// Environment expectation: these tests create and show real Win32
// windows and need a D3D11-capable adapter; this machine runs them
// interactively and they never silently skip. Every test uses a fresh
// host and closes everything it opens (testing.Cleanup); every wait is a
// generous poll.
//
// Device REMOVAL/loss cannot be forced deterministically on real
// hardware: the native quarantine/abort paths are implemented and
// unit-validated in Rust (record validation, status-code
// distinctness), but no test here injects a fake failed state — the
// shipped service has no fake happy paths.
package rendererspec

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"gpui-go/gpui"
	"gpui-go/internal/native"
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

// newHost boots a fresh host and stops it at cleanup (the winhostspec
// boot pattern), checking the thread join and goroutine baseline.
func newHost(t *testing.T) *gpui.Host {
	t.Helper()
	before := runtime.NumGoroutine()
	h := gpui.NewHost()
	if err := h.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
		waitFor(t, 5*time.Second, "goroutines to drain back near the baseline", func() bool {
			return runtime.NumGoroutine() <= before+2
		})
	})
	return h
}

// openShown opens one shown host window, failing on creation errors.
func openShown(t *testing.T, h *gpui.Host, title string, w, hgt float32) gpui.WindowHandle {
	t.Helper()
	handle, err := h.OpenWindow(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: hgt}},
	})
	if err != nil {
		t.Fatalf("OpenWindow(%q) failed: %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if handle.Alive() {
			handle.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !handle.Alive() })
		}
	})
	return handle
}

// mustRendererService loads the native renderer service of the embedded
// artifact.
func mustRendererService(t *testing.T) *native.RendererService {
	t.Helper()
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	svc, err := lib.Renderer()
	if err != nil {
		t.Fatalf("lib.Renderer(): %v", err)
	}
	return svc
}

// TestNativeRendererDCompClearFrame is the ABI-level flow: create a
// DComp surface on a real HWND, present a red clear, poll until
// completion (S_OK; S_FALSE observations recorded but not required),
// retire, and check the exactly-once typed errors.
func TestNativeRendererDCompClearFrame(t *testing.T) {
	svc := mustRendererService(t)
	h := newHost(t)
	handle := openShown(t, h, "rendererspec native dcomp", 300, 200)
	waitFor(t, 10*time.Second, "the window to be visible", handle.Visible)

	surface, err := svc.CreateSurface(handle.Hwnd(), native.SurfaceModeDCompPremultiplied, 300, 200, 1.0)
	if err != nil {
		t.Fatalf("CreateSurface: %v", err)
	}
	t.Cleanup(func() { svc.DestroySurface(surface) })

	// The pinned swap-chain facts surface in the info record.
	info, err := svc.Info(surface)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Mode != uint32(native.SurfaceModeDCompPremultiplied) ||
		info.Format != native.RenderTargetFormatValue ||
		info.BufferCount != native.RenderBufferCount {
		t.Fatalf("surface info = %+v, want DComp/BGRA8/triple-buffer", info)
	}
	t.Logf("surface info: %+v", info)

	sub, err := svc.PresentClear(surface, native.RGBA{R: 1, G: 0, B: 0, A: 1})
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}

	// The immediate poll: record an S_FALSE observation if one happens.
	state, err := svc.Poll(sub)
	if err != nil {
		t.Fatalf("immediate Poll: %v", err)
	}
	pendingSeen := state.State == native.SubmissionPending
	t.Logf("immediate poll: state=%d data_hr=%#x present_hr=%#x (S_FALSE=1)", state.State, state.DataHR, state.PresentHR)

	// Poll until S_OK + completion.
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, err = svc.Poll(sub)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if state.State == native.SubmissionCompleted {
			break
		}
		if state.State == native.SubmissionFailed {
			t.Fatalf("submission failed: %+v", state)
		}
		if time.Now().After(deadline) {
			t.Fatalf("submission did not complete within 10s: %+v", state)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !pendingSeen {
		t.Logf("no S_FALSE observation captured (completion preceded the first poll)")
	}

	record, err := svc.Retire(sub)
	if err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if record.Terminal != native.RetireCompleted {
		t.Fatalf("terminal = %d, want completed", record.Terminal)
	}

	// Exactly-once: the second retire is the typed stale-handle error.
	if _, err := svc.Retire(sub); !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("second Retire = %v, want the stale-handle error", err)
	}
	// Polling a retired submission is a typed error.
	if _, err := svc.Poll(sub); !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("Poll of a retired submission = %v, want the stale-handle error", err)
	}
}

// TestNativeRendererHwndAlphaIgnoreMode is the ABI-level HWND-mode flow
// (the pin's DirectComposition-disabled path).
func TestNativeRendererHwndAlphaIgnoreMode(t *testing.T) {
	svc := mustRendererService(t)
	h := newHost(t)
	handle := openShown(t, h, "rendererspec native hwnd", 280, 180)

	surface, err := svc.CreateSurface(handle.Hwnd(), native.SurfaceModeHwndAlphaIgnore, 280, 180, 1.0)
	if err != nil {
		t.Fatalf("CreateSurface(HWND): %v", err)
	}
	t.Cleanup(func() { svc.DestroySurface(surface) })
	info, err := svc.Info(surface)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Mode != uint32(native.SurfaceModeHwndAlphaIgnore) {
		t.Fatalf("mode = %d, want HWND alpha-ignore", info.Mode)
	}

	sub, err := svc.PresentClear(surface, native.RGBA{R: 0, G: 1, B: 0, A: 1})
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, err := svc.Poll(sub)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if state.State == native.SubmissionCompleted {
			break
		}
		if state.State == native.SubmissionFailed {
			t.Fatalf("submission failed: %+v", state)
		}
		if time.Now().After(deadline) {
			t.Fatalf("submission did not complete within 10s: %+v", state)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := svc.Retire(sub); err != nil {
		t.Fatalf("Retire: %v", err)
	}
}

// TestNativeRendererHandleErrorPaths pins the ABI's typed error paths
// with real handles: a malformed surface handle, a destroyed surface, a
// retire of a pending submission and a destroy with outstanding
// submissions.
func TestNativeRendererHandleErrorPaths(t *testing.T) {
	svc := mustRendererService(t)
	h := newHost(t)
	handle := openShown(t, h, "rendererspec native errors", 240, 160)

	// A malformed surface handle (out-of-range slot): typed bad-handle.
	if _, err := svc.Info(0xFFFF_FFFF_0000_0001); !errors.Is(err, native.ErrRendererBadHandle) {
		t.Fatalf("Info(malformed) = %v, want the bad-handle error", err)
	}
	if err := svc.DestroySurface(0xFFFF_FFFF_0000_0001); !errors.Is(err, native.ErrRendererBadHandle) {
		t.Fatalf("DestroySurface(malformed) = %v, want the bad-handle error", err)
	}

	surface, err := svc.CreateSurface(handle.Hwnd(), native.SurfaceModeDCompPremultiplied, 240, 160, 1.0)
	if err != nil {
		t.Fatalf("CreateSurface: %v", err)
	}

	// Retire of a pending submission: typed pending error (acceptance is
	// not completion).
	sub, err := svc.PresentClear(surface, native.RGBA{R: 0.1, G: 0.2, B: 0.3, A: 1})
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}
	if _, err := svc.Retire(sub); !errors.Is(err, native.ErrRendererPending) {
		t.Fatalf("Retire(pending) = %v, want the pending error", err)
	}
	// Destroy with the submission outstanding is refused.
	if err := svc.DestroySurface(surface); !errors.Is(err, native.ErrRendererPending) {
		t.Fatalf("DestroySurface(outstanding) = %v, want the pending error", err)
	}

	// Drain, then destroy: the handle becomes stale.
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, err := svc.Poll(sub)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if state.State == native.SubmissionCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("submission did not complete: %+v", state)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := svc.Retire(sub); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if err := svc.DestroySurface(surface); err != nil {
		t.Fatalf("DestroySurface: %v", err)
	}
	if _, err := svc.Info(surface); !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("Info(destroyed) = %v, want the stale-handle error", err)
	}
	if _, err := svc.PresentClear(surface, native.RGBA{R: 1, G: 1, B: 1, A: 1}); !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("PresentClear(destroyed) = %v, want the stale-handle error", err)
	}
}

// TestNativeRendererBoundedPendingBusy pins the native bounded
// pending-submission rule directly: 64 tracked (un-retired) submissions
// are accepted, the 65th is rejected with the typed busy error before any
// GPU work, and draining restores acceptance.
func TestNativeRendererBoundedPendingBusy(t *testing.T) {
	svc := mustRendererService(t)
	h := newHost(t)
	handle := openShown(t, h, "rendererspec native busy", 240, 160)

	surface, err := svc.CreateSurface(handle.Hwnd(), native.SurfaceModeDCompPremultiplied, 240, 160, 1.0)
	if err != nil {
		t.Fatalf("CreateSurface: %v", err)
	}
	t.Cleanup(func() { svc.DestroySurface(surface) })

	bound := svc.MaxPendingSubmissions()
	if bound != 64 {
		t.Fatalf("MaxPendingSubmissions = %d, want 64", bound)
	}
	subs := make([]native.SubmissionHandle, 0, bound)
	for i := 0; i < bound; i++ {
		sub, err := svc.PresentClear(surface, native.RGBA{R: 0.05, G: 0.05, B: 0.05, A: 1})
		if err != nil {
			t.Fatalf("PresentClear %d within the bound: %v", i, err)
		}
		subs = append(subs, sub)
	}
	// The next submission is backpressure, typed, before GPU work.
	if _, err := svc.PresentClear(surface, native.RGBA{R: 1, G: 1, B: 1, A: 1}); !errors.Is(err, native.ErrRendererBusy) {
		t.Fatalf("PresentClear beyond the bound = %v, want the busy error", err)
	}
	// Draining (poll to completion, retire) restores acceptance.
	for _, sub := range subs {
		deadline := time.Now().Add(10 * time.Second)
		for {
			state, err := svc.Poll(sub)
			if err != nil {
				t.Fatalf("Poll: %v", err)
			}
			if state.State == native.SubmissionCompleted {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("drain did not complete: %+v", state)
			}
			time.Sleep(time.Millisecond)
		}
		if _, err := svc.Retire(sub); err != nil {
			t.Fatalf("Retire: %v", err)
		}
	}
	if _, err := svc.PresentClear(surface, native.RGBA{R: 1, G: 0, B: 1, A: 1}); err != nil {
		t.Fatalf("PresentClear after drain: %v", err)
	}
	// Destroy is refused until the last submission retires.
	if err := svc.DestroySurface(surface); !errors.Is(err, native.ErrRendererPending) {
		t.Fatalf("destroy with the last submission outstanding = %v, want pending", err)
	}
}

// TestNativeRendererDeviceInfoIdentity records the environment identity
// of the shared device (adapter description, feature level, driver
// identity) — the ticket's evidence record.
func TestNativeRendererDeviceInfoIdentity(t *testing.T) {
	svc := mustRendererService(t)
	info, err := svc.DeviceInfo()
	if err != nil {
		t.Fatalf("DeviceInfo: %v", err)
	}
	t.Logf("device identity: adapter=%q vendor=0x%x device=0x%x feature-level=0x%x software=%v vram=%d driver=%q version=%q multithread=%v",
		info.AdapterDescription, info.VendorID, info.DeviceID, info.FeatureLevel, info.Software,
		info.DedicatedVideoMemory, info.DriverName, info.DriverVersion, info.MultithreadProtected)
	if info.AdapterDescription == "" {
		t.Fatal("empty adapter description")
	}
	if info.DriverName == "" || info.DriverVersion == "" {
		t.Fatalf("empty driver identity: %q / %q", info.DriverName, info.DriverVersion)
	}
	if info.FeatureLevel != 0xb000 && info.FeatureLevel != 0xb100 {
		t.Errorf("feature level %#x is not 11.0/11.1", info.FeatureLevel)
	}
	if !info.MultithreadProtected {
		t.Error("multithread protection is not enabled on the immediate context")
	}
}

// TestGpuiRendererPresentsAndRetiresThroughPublicAPI drives the whole
// public gpui flow from outside the runtime package: app + host, a real
// window, GetOrCreateSurface, PresentClear, the pacing worker's
// retirement and the ledger invariants.
func TestGpuiRendererPresentsAndRetiresThroughPublicAPI(t *testing.T) {
	app := gpui.NewApp()
	h := gpui.NewHost()
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
	})

	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})

	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:     "rendererspec gpui pacing",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 320, Height: 220}},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow: %v", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	surface, err := renderer.GetOrCreateSurfaceMode(window, native.SurfaceModeDCompPremultiplied)
	if err != nil {
		t.Fatalf("GetOrCreateSurfaceMode: %v", err)
	}
	if surface.Hwnd() != window.Handle().Hwnd() {
		t.Fatalf("surface HWND %#x does not match the window lease %#x", surface.Hwnd(), window.Handle().Hwnd())
	}

	// Several submissions; the pacing worker polls, retires and
	// dispatches the records to the foreground.
	subs := make([]*gpui.Submission, 0, 8)
	for i := 0; i < 8; i++ {
		sub, err := renderer.PresentClear(window, gpui.Color{R: float32(i) / 8, G: 0.4, B: 0.6, A: 1})
		if err != nil {
			t.Fatalf("PresentClear %d: %v", i, err)
		}
		subs = append(subs, sub)
	}
	for _, sub := range subs {
		if err := sub.WaitForRetirement(15 * time.Second); err != nil {
			t.Fatalf("submission %d did not retire: %v", sub.ID(), err)
		}
	}

	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 8 || retired != 8 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 8/8/0/0",
			accepted, retired, quarantined, inFlight)
	}
	if accepted < retired {
		t.Fatalf("invariant violated: accepted %d < retired %d", accepted, retired)
	}
	// No retire without a preceding completed poll.
	completed := make(map[uint64]bool)
	for _, event := range events {
		switch event.Kind {
		case gpui.LedgerPollCompleted:
			completed[event.Submission] = true
		case gpui.LedgerRetired:
			if !completed[event.Submission] {
				t.Fatalf("submission %d retired without a completed poll", event.Submission)
			}
		}
	}
	t.Logf("ledger trace: %d events (%d accepted, %d retired)", len(events), accepted, retired)
}

// TestGpuiRendererTwoWindowsSerializesAcrossSurfaces proves the
// two-window serialization through the public API: interleaved presents
// from two goroutines both succeed and retire (the native device mutex
// plus the Go-side surface table serialize everything).
func TestGpuiRendererTwoWindowsSerializesAcrossSurfaces(t *testing.T) {
	app := gpui.NewApp()
	h := gpui.NewHost()
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
	})

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})

	window1, err := app.OpenWindow(gpui.WindowOptions{
		Title:  "rendererspec gpui two 1",
		Show:   true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 300, Height: 200}},
	})
	if err != nil {
		t.Fatalf("window one: %v", err)
	}
	window2, err := app.OpenWindow(gpui.WindowOptions{
		Title:  "rendererspec gpui two 2",
		Show:   true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 260, Height: 180}},
	})
	if err != nil {
		t.Fatalf("window two: %v", err)
	}
	t.Cleanup(func() {
		for _, w := range []*gpui.Window{window1, window2} {
			if w.Alive() {
				w.Close()
			}
		}
		waitFor(t, 10*time.Second, "both windows to be destroyed", func() bool {
			return !window1.Alive() && !window2.Alive()
		})
	})

	const rounds = 3
	type result struct {
		err error
	}
	results := make(chan result, rounds*2)
	present := func(w *gpui.Window, i int) {
		sub, err := renderer.PresentClear(w, gpui.Color{R: float32(i) / rounds, G: 0.7, B: 0.2, A: 1})
		if err != nil {
			results <- result{err}
			return
		}
		if _, err := sub.WaitForCompletion(10 * time.Second); err != nil {
			results <- result{err}
			return
		}
		if _, err := sub.Retire(); err != nil {
			results <- result{err}
			return
		}
		results <- result{}
	}
	for round := 0; round < rounds; round++ {
		go present(window1, round)
		go present(window2, round)
	}
	for i := 0; i < rounds*2; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Errorf("concurrent present/retire: %v", r.err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("timed out waiting for a concurrent present/retire to finish")
		}
	}
	_, accepted, retired, _, inFlight := renderer.Ledger()
	if accepted != rounds*2 || retired != rounds*2 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d in-flight=%d, want %d/%d/0",
			accepted, retired, inFlight, rounds*2, rounds*2)
	}
}
