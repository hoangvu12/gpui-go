//go:build windows

// Package devicelossspec holds ticket29's Go-half tests: the device-loss
// detection and recovery controller (gpui/deviceloss.go) over the native
// renderer service's device-loss entries (InjectDeviceLoss, RebuildDevice,
// the ERR_RECOVERING gate, MarkDrawable), driven against REAL windows on
// the real D3D11 device. They exercise the controller from outside the
// runtime package, matching the repo convention (see internal/rendererspec
// and internal/pathspec).
//
// Environment expectation: these tests create and show real Win32 windows
// and need a D3D11-capable adapter; this machine runs them interactively
// and they never silently skip. Every test uses a fresh host and closes
// everything it opens (testing.Cleanup); every wait is a generous poll.
//
// Loss identity: real driver removal cannot be forced deterministically on
// demand, so the loss is the native test injection (InjectDeviceLoss —
// recorded separately from real loss in every state and report record, with
// the DXGI_ERROR_DEVICE_REMOVED marker as the confirmed removal reason).
// The bounded paths behind real loss (poll failure, retire quarantine, the
// rebuild's submission abort, the gate) are all exercised through it.
//
// Mouse note: this machine's mouse hit tests are corrupted post-fullscreen
// (shell state); these tests drive no real cursor input at all — every
// window interaction is a synthetic message or an explicit API call.
package devicelossspec

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// dxgiErrorDeviceRemoved is the injected loss's confirmed removal reason
// (DXGI_ERROR_DEVICE_REMOVED, 0x887A0007, as the i32 the records carry).
const dxgiErrorDeviceRemoved int32 = -2005270523

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

// bootApp boots a fresh application on a started host and stops both at
// cleanup (the rendererspec boot pattern; the cleanup runs LIFO, so the
// renderer's own cleanup — registered after this — stops the pacing
// worker before the host stops).
func bootApp(t *testing.T) *gpui.App {
	t.Helper()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})
	return app
}

// openShown opens one shown app window, failing on creation errors.
func openShown(t *testing.T, app *gpui.App, title string, w, h float32) *gpui.Window {
	t.Helper()
	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:     title,
		Show:      true,
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
	return window
}

// mustNativeRenderer loads the native renderer service of the embedded
// artifact (the test injection and rebuild seam; the same process-global
// native device the gpui renderer drives).
func mustNativeRenderer(t *testing.T) *native.RendererService {
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

// injectLoss arms the native test-injected device loss.
func injectLoss(t *testing.T, svc *native.RendererService) {
	t.Helper()
	if err := svc.InjectDeviceLoss(); err != nil {
		t.Fatalf("InjectDeviceLoss: %v", err)
	}
}

// drain polls the submission to completion and retires it.
func drain(t *testing.T, sub *gpui.Submission, timeout time.Duration) {
	t.Helper()
	if sub == nil {
		t.Fatalf("drain: nil submission")
	}
	if _, err := sub.WaitForCompletion(timeout); err != nil {
		t.Fatalf("submission %d did not complete: %v", sub.ID(), err)
	}
	if _, err := sub.Retire(); err != nil {
		t.Fatalf("retire submission %d: %v", sub.ID(), err)
	}
}

// pixelAt reads one RGBA pixel from the readback buffer.
func pixelAt(pixels []byte, width, x, y int) (r, g, b byte) {
	offset := (y*width + x) * 4
	return pixels[offset], pixels[offset+1], pixels[offset+2]
}

// closeColor compares a readback channel with tolerance.
func closeColor(actual, wanted byte) bool {
	diff := int(actual) - int(wanted)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 8
}

// quadScene builds a finished one-quad scene (the full-bounds background
// fill) for the gate-refusal and pixel-verification draws.
func quadScene(t *testing.T, color gpui.Hsla, size gpui.Size, scale float32) *gpui.Scene {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	mask := gpui.Bounds{Origin: gpui.Point{}, Size: size}
	ctx := gpui.NewPaintContext(scale, mask, 1.0)
	if err := scene.PaintQuad(gpui.PaintQuad{Bounds: mask, Background: gpui.SolidBackground(color)}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("scene finish: %v", err)
	}
	return scene
}

// fillRootView is the root view of a window filled with one color: every
// frame draw reads the current fill, so a fresh frame after recovery
// carries the app's latest state (the pin's force-render bypass of the
// retained-frame cache).
type fillRootView struct {
	fill *gpui.Hsla
}

// EntityID implements gpui.View: stateless recipe.
func (v *fillRootView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *fillRootView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.Div().SizeFull().Bg(*v.fill).IntoElement()
}

// openFilledWindow opens a shown window whose root view fills with fill
// (the OpenWindowView first-frame draw included).
func openFilledWindow(t *testing.T, app *gpui.App, title string, w, h float32, fill *gpui.Hsla) *gpui.Window {
	t.Helper()
	window, err := app.OpenWindowView(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		return &fillRootView{fill: fill}
	})
	if err != nil {
		t.Fatalf("app.OpenWindowView(%q): %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return window
}

// syntheticGlyphKey is an atlas key guaranteed outside the real font id
// space (bit 63 set, the glyphspec pattern).
func syntheticGlyphKey(index uint32) gpui.RasterGlyphParams {
	return gpui.RasterGlyphParams{
		FontID:      gpui.FontID(0x8000000000000000 | uint64(index)),
		GlyphID:     index,
		FontSize:    24,
		ScaleFactor: 1,
		RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
	}
}

// insertSyntheticTile uploads one synthetic monochrome tile into the
// atlas (the tile whose invalidation the recovery proves).
func insertSyntheticTile(t *testing.T, atlas *gpui.Atlas, index uint32) gpui.AtlasTile {
	t.Helper()
	raster := &gpui.RasterizedGlyph{
		Width:  32,
		Height: 32,
		Format: gpui.RasterFormatAlphaMask,
		Pixels: make([]byte, 32*32),
	}
	for i := range raster.Pixels {
		raster.Pixels[i] = byte(i % 251)
	}
	tile, err := atlas.InsertGlyph(raster, syntheticGlyphKey(index))
	if err != nil {
		t.Fatalf("InsertGlyph: %v", err)
	}
	return tile
}

// ---------------------------------------------------------------------------
// Identity: real and injected loss recorded separately
// ---------------------------------------------------------------------------

// TestDeviceLossIdentityRecordedSeparately probes the live state through
// the gpui seam: healthy, the injected identity (lost + injected + the
// removal marker, separately from the real-loss identity), and the
// recovered state (gate cleared, generation bumped).
func TestDeviceLossIdentityRecordedSeparately(t *testing.T) {
	app := bootApp(t)
	window := openShown(t, app, "devicelossspec identity", 240, 160)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	svc := mustNativeRenderer(t)
	if _, err := renderer.GetOrCreateSurface(window); err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}

	state, err := renderer.DeviceLossState()
	if err != nil {
		t.Fatalf("DeviceLossState (healthy): %v", err)
	}
	if state.Lost || state.Injected || state.Recovering || state.RemovedReason != 0 {
		t.Fatalf("healthy state = %+v, want no loss and no gate", state)
	}

	// The injected identity: lost with the injected marker recorded
	// separately from the real-loss identity.
	injectLoss(t, svc)
	state, err = renderer.DeviceLossState()
	if err != nil {
		t.Fatalf("DeviceLossState (injected): %v", err)
	}
	if !state.Lost || !state.Injected || state.Recovering {
		t.Fatalf("injected state = %+v, want lost+injected, no gate yet", state)
	}
	if state.RemovedReason != dxgiErrorDeviceRemoved {
		t.Fatalf("injected removal reason = %#x, want the DXGI_ERROR_DEVICE_REMOVED marker %#x", state.RemovedReason, dxgiErrorDeviceRemoved)
	}

	// The recovery records the injected identity in its report and
	// leaves a healthy device with a bumped generation (the generation
	// is process-global: earlier recoveries in this binary already
	// bumped it, so the assertion is on the bump).
	baseline := state.DeviceGeneration
	report, err := renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("RecoverDeviceLoss: %v", err)
	}
	if !report.Injected {
		t.Fatalf("the report did not record the injected identity")
	}
	if report.RemovedReason != dxgiErrorDeviceRemoved {
		t.Fatalf("report removal reason = %#x, want the injected marker", report.RemovedReason)
	}
	if report.AlreadyHealthy || report.Rebuild.Attempts != 1 || report.Rebuild.DeviceGeneration != baseline+1 {
		t.Fatalf("report = %+v, want one successful rebuild at generation %d", report, baseline+1)
	}
	if report.Rebuild.RebuiltSurfaces+report.Rebuild.FailedSurfaces < 1 {
		t.Fatalf("rebuild record = %+v, want the window's surface attempted (in place or fresh)", report.Rebuild)
	}
	if report.MarkedDrawable != true {
		t.Fatalf("the recovery did not clear the gate")
	}
	if report.Windows[0].Submission != nil {
		drain(t, report.Windows[0].Submission, 15*time.Second)
	}
	state, err = renderer.DeviceLossState()
	if err != nil {
		t.Fatalf("DeviceLossState (recovered): %v", err)
	}
	if state.Lost || state.Injected || state.Recovering || state.DeviceGeneration != baseline+1 || state.Recoveries != 1 {
		t.Fatalf("recovered state = %+v, want healthy at generation %d with one recovery", state, baseline+1)
	}

	// A second sequence on the healthy device is the honest no-op.
	report, err = renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("second RecoverDeviceLoss: %v", err)
	}
	if !report.AlreadyHealthy {
		t.Fatalf("second report = %+v, want AlreadyHealthy", report)
	}
}

// ---------------------------------------------------------------------------
// The gate: present/draw refusals are observable, never silent
// ---------------------------------------------------------------------------

// TestRecoveryGateRefusesPresentObservably drives the raw gate semantics
// with the native entries (inject during present/draw — the refuse path):
// while the gate is armed after a rebuild, PresentClear, DrawScene and
// RenderScene refuse with the typed sentinel and every refused frame
// lands in the ledger as a skipped frame ("skipped, redraw pending");
// MarkDrawable clears the gate and submissions flow again.
func TestRecoveryGateRefusesPresentObservably(t *testing.T) {
	app := bootApp(t)
	window := openShown(t, app, "devicelossspec gate refuse", 260, 180)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	svc := mustNativeRenderer(t)
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}

	// A present while the device is lost but NOT yet rebuilt is still
	// accepted: the injection only arms the retire-time confirmation,
	// the device itself is alive (the honest injected-loss semantics —
	// real removal is not forceable on demand on this hardware).
	injectLoss(t, svc)
	sub, err := surface.PresentClear(gpui.Color{R: 0.3, G: 0.3, B: 0.3, A: 1})
	if err != nil {
		t.Fatalf("PresentClear while lost (pre-rebuild): %v", err)
	}
	drain(t, sub, 15*time.Second)

	// The rebuild arms the gate: every frame path refuses.
	if _, err := svc.RebuildDevice(); err != nil {
		t.Fatalf("native RebuildDevice: %v", err)
	}
	state, _ := renderer.DeviceLossState()
	if !state.Recovering {
		t.Fatalf("state = %+v, want the recovery gate armed after the rebuild", state)
	}

	scene := quadScene(t, gpui.RgbaToHsla(0x334155ff), gpui.Size{Width: 260, Height: 180}, 1)
	skipsBefore := renderer.SkippedFrames()
	if _, err := surface.PresentClear(gpui.Color{R: 1, G: 0, B: 0, A: 1}); !errors.Is(err, gpui.ErrRendererRecovering) {
		t.Fatalf("PresentClear while gated = %v, want ErrRendererRecovering", err)
	}
	if _, err := surface.DrawScene(scene, nil, gpui.BackgroundOpaque); !errors.Is(err, gpui.ErrRendererRecovering) {
		t.Fatalf("DrawScene while gated = %v, want ErrRendererRecovering", err)
	}
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); !errors.Is(err, gpui.ErrRendererRecovering) {
		t.Fatalf("RenderScene while gated = %v, want ErrRendererRecovering", err)
	}

	// The refusals are observable in the ledger: skipped frames for the
	// window, never silent successes, never accepted submissions.
	skips := renderer.SkippedFrames() - skipsBefore
	if skips != 2 {
		t.Fatalf("skipped frames = %d, want 2 (the present and draw paths; the render path carries no submission identity)", skips)
	}
	events, accepted, _, _, _ := renderer.Ledger()
	if accepted != 1 {
		t.Fatalf("ledger accepted = %d, want 1 (the pre-rebuild submission only)", accepted)
	}
	skipEvents := 0
	for _, event := range events {
		if event.Kind == gpui.LedgerSkipped {
			skipEvents++
			if event.Submission != 0 {
				t.Fatalf("skip event carries submission %d, want 0 (the refusal precedes acceptance)", event.Submission)
			}
			if event.Window != window.ID() {
				t.Fatalf("skip event window = %d, want %d", event.Window, window.ID())
			}
		}
	}
	if skipEvents != 2 {
		t.Fatalf("ledger skip events = %d, want 2", skipEvents)
	}

	// MarkDrawable clears the gate; submissions flow again.
	if err := renderer.MarkDrawable(); err != nil {
		t.Fatalf("renderer.MarkDrawable: %v", err)
	}
	state, _ = renderer.DeviceLossState()
	if state.Recovering {
		t.Fatalf("state = %+v, want the gate cleared by MarkDrawable", state)
	}
	// The in-place surface rebuild failed on this artifact (the old
	// composition tree stays attached): the raw consumer re-attaches a
	// fresh surface on the replacement device exactly like the
	// controller's fresh-surface path, and the gate-cleared present
	// flows through it.
	if state, _ := renderer.DeviceLossState(); state.Lost {
		t.Fatalf("state = %+v, want a healthy replacement device", state)
	}
	if err := surface.Destroy(); err != nil {
		t.Fatalf("destroying the stale surface: %v", err)
	}
	fresh, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("re-attaching a fresh surface: %v", err)
	}
	sub, err = fresh.PresentClear(gpui.Color{R: 0, G: 1, B: 0, A: 1})
	if err != nil {
		t.Fatalf("PresentClear after MarkDrawable: %v", err)
	}
	drain(t, sub, 15*time.Second)
	// The device stays healthy through the fresh surface (presenting
	// through the stale old-device chain would poison the replacement
	// device — the fresh-surface path avoids it).
	state, _ = renderer.DeviceLossState()
	if state.Lost {
		t.Fatalf("state = %+v, want the replacement device healthy after the gate-cleared present", state)
	}
}

// ---------------------------------------------------------------------------
// The controller's full recovery sequence
// ---------------------------------------------------------------------------

// TestControllerRecoversInjectedLossEndToEnd is the ticket's core flow:
// inject loss during a live window with an in-flight submission whose
// retirement was refused (the quarantine-side entry), then the
// controller's sequence — rebuild (aborting the submission: its handle
// goes stale), atlas invalidation (tiles stale, re-insert works), the
// gate probe (the observable skip), MarkDrawable, the fresh forced
// redraw (pixel change through the rebuilt device) — and the ledger
// accounting.
func TestControllerRecoversInjectedLossEndToEnd(t *testing.T) {
	app := bootApp(t)
	svc := mustNativeRenderer(t)
	fill := gpui.RgbaToHsla(0xdc2626ff) // red
	window := openFilledWindow(t, app, "devicelossspec end to end", 320, 220, &fill)

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	width, height := int(info.Width), int(info.Height)

	// The app's atlas: one live tile the recovery must invalidate.
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("gpui.NewAtlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })
	if err := renderer.RegisterAtlas(atlas); err != nil {
		t.Fatalf("renderer.RegisterAtlas: %v", err)
	}
	tile := insertSyntheticTile(t, atlas, 1)
	if tile.Generation != 1 {
		t.Fatalf("tile generation = %d, want 1", tile.Generation)
	}

	// The pre-loss frame's pixels (the initial draw's scene, red fill).
	preScene := gpui.LastDrawnScene(window)
	if preScene == nil {
		t.Fatalf("the window has no initial drawn scene")
	}
	if err := surface.RenderScene(preScene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("pre-loss RenderScene: %v", err)
	}
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("pre-loss ReadPixels: %v", err)
	}
	r, g, b := pixelAt(pixels, width, width/2, height/2)
	if !closeColor(r, 0xdc) || !closeColor(g, 0x26) || !closeColor(b, 0x26) {
		t.Fatalf("pre-loss center pixel = (%d,%d,%d), want the red fill", r, g, b)
	}

	// The in-flight dependency: a submission whose retirement is
	// refused while pending (acceptance is not completion — the
	// quarantined-side entry the rebuild must abort).
	pending, err := surface.PresentClear(gpui.Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
	if err != nil {
		t.Fatalf("PresentClear (the in-flight dependency): %v", err)
	}
	pendingHandle := pending.Handle()
	if _, err := pending.Retire(); !errors.Is(err, gpui.ErrRendererRetireNotCompleted) {
		t.Fatalf("retire while pending = %v, want ErrRendererRetireNotCompleted", err)
	}

	// The app mutates its state: the post-recovery forced redraw must
	// carry the fresh frame, not a replay of the lost one.
	fill = gpui.RgbaToHsla(0x2563ebff) // blue

	lossState, err := renderer.DeviceLossState()
	if err != nil {
		t.Fatalf("DeviceLossState (baseline): %v", err)
	}
	baselineGeneration := lossState.DeviceGeneration

	injectLoss(t, svc)
	report, err := renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("RecoverDeviceLoss: %v", err)
	}

	// The rebuild: one attempt, the surface attempted, the generation
	// bumped, the gate cleared by the first forced draw.
	if report.Rebuild.Attempts != 1 || report.Rebuild.DeviceGeneration != baselineGeneration+1 {
		t.Fatalf("rebuild record = %+v, want one attempt at generation %d", report.Rebuild, baselineGeneration+1)
	}
	if report.Rebuild.RebuiltSurfaces+report.Rebuild.FailedSurfaces < 1 {
		t.Fatalf("rebuild record = %+v, want the window's surface attempted", report.Rebuild)
	}
	if report.Rebuild.FailedSurfaces > 0 && report.RecreatedSurfaces != 1 {
		t.Fatalf("fresh-surface path: failed surfaces = %d, recreated = %d, want the window's surface recreated", report.Rebuild.FailedSurfaces, report.RecreatedSurfaces)
	}
	if !report.MarkedDrawable {
		t.Fatalf("the recovery did not clear the gate (MarkDrawable)")
	}
	if !report.Injected {
		t.Fatalf("the report did not record the injected identity")
	}

	// The aborted submission: closed through the loss-abort terminal,
	// its native handle stale afterwards (the safe-abort release).
	if len(report.AbortedSubmissions) != 1 || report.AbortedSubmissions[0] != pending.ID() {
		t.Fatalf("aborted submissions = %v, want [%d]", report.AbortedSubmissions, pending.ID())
	}
	entry, ok := renderer.LedgerEntry(pending.ID())
	if !ok || entry.State != "retired" || entry.Terminal != native.RetireAbortedLoss {
		t.Fatalf("aborted ledger entry = %+v, want retired/aborted-loss", entry)
	}
	if entry.DeviceRemovedReason != dxgiErrorDeviceRemoved {
		t.Fatalf("aborted entry removal reason = %#x, want the injected marker", entry.DeviceRemovedReason)
	}
	if _, err := svc.Poll(pendingHandle); !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Fatalf("poll of the aborted handle = %v, want the stale-handle error", err)
	}

	// The atlas invalidation: the generation bumped, every tile stale,
	// re-insert works on the replacement device.
	var atlasEntry *gpui.AtlasInvalidation
	for i := range report.Atlases {
		if report.Atlases[i].TilesBefore == 1 {
			atlasEntry = &report.Atlases[i]
		}
	}
	if atlasEntry == nil {
		t.Fatalf("the registered atlas (one live tile) is missing from the invalidation report: %+v", report.Atlases)
	}
	if atlasEntry.Err != nil {
		t.Fatalf("atlas notify error: %v", atlasEntry.Err)
	}
	if atlasEntry.GenerationAfter != atlasEntry.GenerationBefore+1 || atlasEntry.TilesAfter != 0 {
		t.Fatalf("atlas invalidation = %+v, want a generation bump and zero live tiles", *atlasEntry)
	}
	if _, found, err := atlas.GlyphTile(syntheticGlyphKey(1), gpui.RasterFormatAlphaMask); err != nil || found {
		t.Fatalf("tile query after the notify = (found %v, err %v), want a miss (stale)", found, err)
	}
	retile := insertSyntheticTile(t, atlas, 1)
	if retile.Generation != atlasEntry.GenerationAfter {
		t.Fatalf("re-inserted tile generation = %d, want the post-notify generation %d", retile.Generation, atlasEntry.GenerationAfter)
	}
	if _, found, err := atlas.GlyphTile(syntheticGlyphKey(1), gpui.RasterFormatAlphaMask); err != nil || !found {
		t.Fatalf("tile query after re-insert = (found %v, err %v), want a hit", found, err)
	}

	// The redraw: the gate refused the probe (the observable skip) and
	// the fresh forced redraw was submitted.
	if len(report.Windows) != 1 || report.Windows[0].Window != window.ID() {
		t.Fatalf("redraw report = %+v, want the window's entry", report.Windows)
	}
	redraw := report.Windows[0]
	if !redraw.Skipped {
		t.Fatalf("the forced-redraw probe was not recorded as skipped: %+v", redraw)
	}
	if redraw.Err != nil {
		t.Fatalf("the forced redraw failed: %v", redraw.Err)
	}
	if redraw.Submission == nil {
		t.Fatalf("the forced redraw produced no submission: %+v", redraw)
	}
	if report.SkippedFrames < 1 || renderer.SkippedFrames() < 1 {
		t.Fatalf("skipped frames = report %d / ledger %d, want at least the probe's skip", report.SkippedFrames, renderer.SkippedFrames())
	}
	redrawEntry, ok := renderer.LedgerEntry(redraw.Submission.ID())
	if !ok {
		t.Fatalf("the forced-redraw submission %d is missing from the ledger", redraw.Submission.ID())
	}
	if redrawEntry.PresentFailed {
		t.Fatalf("the forced-redraw submission partially failed: %+v", redrawEntry)
	}

	// The pixel gate: the fresh frame carries the app's mutated state
	// through the rebuilt device (red -> blue at the window center).
	postScene := gpui.LastDrawnScene(window)
	if postScene == nil || postScene == preScene {
		t.Fatalf("the recovery did not draw a fresh frame (the last scene is unchanged)")
	}
	if err := surface.RenderScene(postScene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("post-recovery RenderScene: %v", err)
	}
	pixels, err = surface.ReadPixels()
	if err != nil {
		t.Fatalf("post-recovery ReadPixels: %v", err)
	}
	r, g, b = pixelAt(pixels, width, width/2, height/2)
	if !closeColor(r, 0x25) || !closeColor(g, 0x63) || !closeColor(b, 0xeb) {
		t.Fatalf("post-recovery center pixel = (%d,%d,%d), want the blue fill (the fresh frame)", r, g, b)
	}

	// The redraw submission retires through the ledger (the manual
	// renderer drives it here) and the device serves later frames.
	drain(t, redraw.Submission, 15*time.Second)
	sub, err := surface.PresentClear(gpui.Color{R: 0.9, G: 0.9, B: 0.9, A: 1})
	if err != nil {
		t.Fatalf("post-recovery PresentClear: %v", err)
	}
	drain(t, sub, 15*time.Second)
	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 3 || retired != 3 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 3/3/0/0 (the aborted dependency, the forced redraw, the later frame)",
			accepted, retired, quarantined, inFlight)
	}
	// The trace tells the true story: the aborted dependency's retire is
	// the loss-abort terminal (no completed poll precedes it — the
	// rebuild released it, acceptance was still not completion).
	abortedRetire := false
	for _, event := range events {
		if event.Kind == gpui.LedgerRetired && event.Submission == pending.ID() {
			if event.Terminal == native.RetireAbortedLoss && event.DeviceRemovedReason == dxgiErrorDeviceRemoved {
				abortedRetire = true
			}
		}
	}
	if !abortedRetire {
		t.Fatalf("the ledger trace is missing the aborted-loss retire of submission %d", pending.ID())
	}
}

// ---------------------------------------------------------------------------
// Multiple windows redraw during recovery
// ---------------------------------------------------------------------------

// TestControllerRedrawsMultipleWindows proves the forced redraw covers
// every window: two windows, each with its probe skip recorded in the
// ledger and its forced redraw submitted and retired.
func TestControllerRedrawsMultipleWindows(t *testing.T) {
	app := bootApp(t)
	svc := mustNativeRenderer(t)
	window1 := openShown(t, app, "devicelossspec multi 1", 300, 200)
	window2 := openShown(t, app, "devicelossspec multi 2", 260, 180)

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	for _, window := range []*gpui.Window{window1, window2} {
		if _, err := renderer.GetOrCreateSurface(window); err != nil {
			t.Fatalf("GetOrCreateSurface: %v", err)
		}
	}

	injectLoss(t, svc)
	report, err := renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("RecoverDeviceLoss: %v", err)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("redraw entries = %d, want 2", len(report.Windows))
	}
	seen := map[gpui.WindowID]bool{}
	for _, redraw := range report.Windows {
		if seen[redraw.Window] {
			t.Fatalf("window %d appears twice in the redraw report", redraw.Window)
		}
		seen[redraw.Window] = true
		if !redraw.Skipped {
			t.Errorf("window %d's probe was not skipped: %+v", redraw.Window, redraw)
		}
		if redraw.Err != nil {
			t.Errorf("window %d's redraw failed: %v", redraw.Window, redraw.Err)
			continue
		}
		if redraw.Submission == nil {
			t.Errorf("window %d's redraw produced no submission", redraw.Window)
			continue
		}
		entry, ok := renderer.LedgerEntry(redraw.Submission.ID())
		if !ok || entry.Window != redraw.Window {
			t.Errorf("window %d's redraw submission is missing or misattributed: %+v", redraw.Window, entry)
			continue
		}
	}
	if !seen[window1.ID()] || !seen[window2.ID()] {
		t.Fatalf("the redraw report missed a window: seen = %v", seen)
	}
	// Both windows' skip events are in the ledger.
	events, _, _, _, _ := renderer.Ledger()
	windowSkips := map[gpui.WindowID]int{}
	for _, event := range events {
		if event.Kind == gpui.LedgerSkipped {
			windowSkips[event.Window]++
		}
	}
	for _, window := range []*gpui.Window{window1, window2} {
		if windowSkips[window.ID()] != 1 {
			t.Errorf("window %d skip events = %d, want 1", window.ID(), windowSkips[window.ID()])
		}
	}
	// Both forced redraws drain and retire (the manual renderer drives
	// the polls here).
	for _, redraw := range report.Windows {
		if redraw.Submission == nil {
			continue
		}
		drain(t, redraw.Submission, 15*time.Second)
	}
	_, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 2 || retired != 2 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 2/2/0/0 (both forced redraws)",
			accepted, retired, quarantined, inFlight)
	}
}

// ---------------------------------------------------------------------------
// The pacing worker's detection
// ---------------------------------------------------------------------------

// TestPacingWorkerDetectsAndRecovers proves the detection loop: the
// renderer's pacing worker probes the device state (the pin's
// vsync-thread check), detects the injected loss, and the recovery
// sequence runs to completion with the forced redraw retired by the
// pacing worker itself.
func TestPacingWorkerDetectsAndRecovers(t *testing.T) {
	app := bootApp(t)
	svc := mustNativeRenderer(t)
	window := openShown(t, app, "devicelossspec pacing", 300, 200)

	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	// The renderer must close before the host stops (the pacing worker
	// dispatches retire records through the foreground).
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	if _, err := renderer.GetOrCreateSurface(window); err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}

	// A registered atlas with one live tile: the paced recovery
	// invalidates it.
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("gpui.NewAtlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })
	if err := renderer.RegisterAtlas(atlas); err != nil {
		t.Fatalf("renderer.RegisterAtlas: %v", err)
	}
	insertSyntheticTile(t, atlas, 1)

	// Baseline: one present the pacing worker retires.
	sub, err := renderer.PresentClear(window, gpui.Color{R: 0.2, G: 0.4, B: 0.6, A: 1})
	if err != nil {
		t.Fatalf("baseline PresentClear: %v", err)
	}
	if err := sub.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("baseline submission did not retire: %v", err)
	}

	// Inject: the pacing worker's probe detects the loss (the 350ms
	// native settle is inside the sequence) and the recovery completes.
	baselineState, err := renderer.DeviceLossState()
	if err != nil {
		t.Fatalf("DeviceLossState (baseline): %v", err)
	}
	injectLoss(t, svc)
	waitFor(t, 30*time.Second, "the pacing worker to detect and recover the loss", func() bool {
		state, err := renderer.DeviceLossState()
		return err == nil && state.Recoveries >= 1 && !state.RecoveringNow
	})

	state, err := renderer.DeviceLossState()
	if err != nil {
		t.Fatalf("DeviceLossState: %v", err)
	}
	if state.Lost || state.Recovering || state.DeviceGeneration != baselineState.DeviceGeneration+1 {
		t.Fatalf("state after the paced recovery = %+v, want a healthy device at generation %d", state, baselineState.DeviceGeneration+1)
	}
	report := renderer.DeviceLossReport()
	if report == nil || report.Err != nil || !report.Injected || !report.MarkedDrawable {
		t.Fatalf("paced recovery report = %+v, want a completed injected recovery", report)
	}
	if len(report.Windows) != 1 || !report.Windows[0].Skipped || report.Windows[0].Submission == nil {
		t.Fatalf("paced redraw report = %+v, want the probe skip and the redraw submission", report.Windows)
	}

	// The atlas was invalidated at the generation boundary.
	found := false
	for i := range report.Atlases {
		if report.Atlases[i].TilesBefore == 1 && report.Atlases[i].TilesAfter == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the registered atlas is missing from the paced recovery's invalidation report: %+v", report.Atlases)
	}

	// The pacing worker retired the forced redraw itself.
	if err := report.Windows[0].Submission.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the paced redraw submission did not retire: %v", err)
	}

	// The device serves later frames through the pacing worker.
	sub, err = renderer.PresentClear(window, gpui.Color{R: 0.8, G: 0.2, B: 0.2, A: 1})
	if err != nil {
		t.Fatalf("post-recovery PresentClear: %v", err)
	}
	if err := sub.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("post-recovery submission did not retire: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bounded submission pressure across the recovery
// ---------------------------------------------------------------------------

// TestBoundedSubmissionPressureRecovered pins the bounded in-flight
// pressure across device loss: the per-surface cap holds before the
// loss, the rebuild aborts every tracked submission (freeing the bound —
// no wedge), and the ledger closes all of them through the loss-abort
// terminal.
func TestBoundedSubmissionPressureRecovered(t *testing.T) {
	app := bootApp(t)
	svc := mustNativeRenderer(t)
	window := openShown(t, app, "devicelossspec pressure", 240, 160)

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}

	// Fill the bound with tracked (un-retired) submissions.
	bound := renderer.MaxPendingSubmissions()
	if bound != 64 {
		t.Fatalf("MaxPendingSubmissions = %d, want 64", bound)
	}
	subs := make([]*gpui.Submission, 0, bound)
	for i := 0; i < bound; i++ {
		sub, err := surface.PresentClear(gpui.Color{R: 0.05, G: 0.05, B: 0.05, A: 1})
		if err != nil {
			t.Fatalf("PresentClear %d within the bound: %v", i, err)
		}
		subs = append(subs, sub)
	}
	if got := renderer.InFlight(); got != bound {
		t.Fatalf("in-flight = %d, want the bound %d", got, bound)
	}
	// The bound is typed backpressure before native entry.
	if _, err := surface.PresentClear(gpui.Color{R: 1, G: 1, B: 1, A: 1}); !errors.Is(err, gpui.ErrRendererInFlight) {
		t.Fatalf("PresentClear beyond the bound = %v, want ErrRendererInFlight", err)
	}

	// The recovery aborts all of them and frees the bound.
	injectLoss(t, svc)
	report, err := renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("RecoverDeviceLoss: %v", err)
	}
	if len(report.AbortedSubmissions) != bound {
		t.Fatalf("aborted submissions = %d, want the full bound %d", len(report.AbortedSubmissions), bound)
	}
	if report.Windows[0].Submission != nil {
		drain(t, report.Windows[0].Submission, 15*time.Second)
	}
	if got := renderer.InFlight(); got != 0 {
		t.Fatalf("in-flight after the recovery = %d, want 0 (the bound is freed)", got)
	}
	aborted := 0
	for _, sub := range subs {
		entry, ok := renderer.LedgerEntry(sub.ID())
		if !ok {
			t.Errorf("submission %d has no ledger entry", sub.ID())
			continue
		}
		if entry.State != "retired" || entry.Terminal != native.RetireAbortedLoss {
			t.Errorf("submission %d state = %q terminal = %d, want retired/aborted-loss", sub.ID(), entry.State, entry.Terminal)
			continue
		}
		aborted++
	}
	if aborted != bound {
		t.Fatalf("aborted ledger closures = %d, want %d", aborted, bound)
	}

	// Submissions flow again on the replacement device.
	sub, err := surface.PresentClear(gpui.Color{R: 1, G: 0, B: 1, A: 1})
	if err != nil {
		t.Fatalf("PresentClear after the recovery: %v", err)
	}
	drain(t, sub, 15*time.Second)
}

// ---------------------------------------------------------------------------
// Cancellation: close and reopen windows during recovery
// ---------------------------------------------------------------------------

// TestCloseAndReopenDuringRecovery proves the cancellation and reopen
// interactions: a window closed before the forced redraw records the
// closed refusal (no probe, no redraw), and a window opened during the
// recovery's forced redraw gets a surface on the rebuilt device and
// presents afterwards.
func TestCloseAndReopenDuringRecovery(t *testing.T) {
	app := bootApp(t)
	svc := mustNativeRenderer(t)
	windowA := openShown(t, app, "devicelossspec close A", 260, 180)
	windowB := openShown(t, app, "devicelossspec close B", 240, 160)

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	if _, err := renderer.GetOrCreateSurface(windowA); err != nil {
		t.Fatalf("GetOrCreateSurface A: %v", err)
	}
	if _, err := renderer.GetOrCreateSurface(windowB); err != nil {
		t.Fatalf("GetOrCreateSurface B: %v", err)
	}

	// The cancellation: window A closes before the recovery sequence
	// runs (its forced redraw must observe the closed lease).
	windowA.Close()
	waitFor(t, 10*time.Second, "window A to be destroyed", func() bool { return !windowA.Alive() })

	// The reopen happens during the recovery: window B's forced-redraw
	// callback (the app's request_frame seam) opens a fresh window and
	// attaches its surface on the rebuilt device.
	var windowC *gpui.Window
	callbackErr := make(chan error, 2)
	renderer.SetForcedRedraw(func(w *gpui.Window, surface *gpui.Surface) error {
		if w.ID() == windowB.ID() {
			var err error
			windowC, err = app.OpenWindow(gpui.WindowOptions{
				Title:  "devicelossspec reopen C",
				Show:   true,
				Bounds: gpui.Bounds{Size: gpui.Size{Width: 220, Height: 150}},
			})
			if err != nil {
				callbackErr <- err
				return err
			}
			if _, err := renderer.GetOrCreateSurface(windowC); err != nil {
				callbackErr <- err
				return err
			}
			callbackErr <- nil
			return nil
		}
		// Window A: the renderer's own closed-window refusal is the
		// honest per-window record.
		_, err := surface.PresentClear(gpui.Color{R: 0.1, G: 0.7, B: 0.3, A: 1})
		return err
	})

	injectLoss(t, svc)
	report, err := renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("RecoverDeviceLoss: %v", err)
	}
	select {
	case err := <-callbackErr:
		if err != nil {
			t.Fatalf("the forced-redraw callback failed: %v", err)
		}
	default:
		t.Fatalf("the forced-redraw callback never ran")
	}

	// Window A's entry: the closed-window refusal, no probe, no
	// submission. Window B: the app-handled redraw without error.
	if len(report.Windows) != 2 {
		t.Fatalf("redraw entries = %d, want 2", len(report.Windows))
	}
	for _, redraw := range report.Windows {
		switch redraw.Window {
		case windowA.ID():
			if redraw.Err == nil || !strings.Contains(redraw.Err.Error(), "closed") {
				t.Fatalf("window A's redraw entry = %+v, want the closed-window refusal", redraw)
			}
			if redraw.Skipped || redraw.Submission != nil {
				t.Fatalf("window A's redraw entry = %+v, want no probe and no submission for a closed window", redraw)
			}
		case windowB.ID():
			if redraw.Err != nil {
				t.Fatalf("window B's redraw entry = %+v, want the app-handled redraw", redraw)
			}
		default:
			t.Fatalf("unexpected window %d in the redraw report", redraw.Window)
		}
	}
	t.Logf("rebuild record: %+v (a closed window's surface may fail its in-place rebuild)", report.Rebuild)

	// The reopened window works on the rebuilt device.
	if windowC == nil {
		t.Fatalf("the callback did not reopen a window")
	}
	t.Cleanup(func() {
		if windowC.Alive() {
			windowC.Close()
			waitFor(t, 10*time.Second, "window C to be destroyed", func() bool { return !windowC.Alive() })
		}
	})
	surfaceC, err := renderer.GetOrCreateSurface(windowC)
	if err != nil {
		t.Fatalf("GetOrCreateSurface(window C): %v", err)
	}
	sub, err := surfaceC.PresentClear(gpui.Color{R: 0.9, G: 0.5, B: 0.1, A: 1})
	if err != nil {
		t.Fatalf("PresentClear on the reopened window: %v", err)
	}
	drain(t, sub, 15*time.Second)
}

// ---------------------------------------------------------------------------
// Resize during recovery
// ---------------------------------------------------------------------------

// TestResizeDuringRecovery proves the resize interaction: a window
// resized during the recovery sequence keeps drawing afterwards — the
// present boundary's resize-at-a-safe-point syncs the surface extent on
// the rebuilt swap chain.
func TestResizeDuringRecovery(t *testing.T) {
	app := bootApp(t)
	svc := mustNativeRenderer(t)
	window := openShown(t, app, "devicelossspec resize", 260, 180)

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	oldWidth, oldHeight := surface.Size()
	if oldWidth == 0 || oldHeight == 0 {
		t.Fatalf("surface extent = %dx%d, want non-zero", oldWidth, oldHeight)
	}

	// The resize lands inside the recovery sequence (the 350ms native
	// settle brackets it deterministically), driven by the
	// forced-redraw callback.
	const newW, newH = 380, 260
	resized := make(chan struct{})
	renderer.SetForcedRedraw(func(w *gpui.Window, surface *gpui.Surface) error {
		w.SetBounds(gpui.Bounds{Origin: gpui.Point{X: 40, Y: 40}, Size: gpui.Size{Width: newW, Height: newH}})
		waitFor(t, 10*time.Second, "the window bounds to report the resize", func() bool {
			bounds, err := w.Bounds()
			return err == nil && bounds.Size.Width == newW && bounds.Size.Height == newH
		})
		close(resized)
		return nil
	})

	injectLoss(t, svc)
	report, err := renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("RecoverDeviceLoss: %v", err)
	}
	select {
	case <-resized:
	default:
		t.Fatalf("the forced-redraw callback did not run (the resize never landed)")
	}
	if len(report.Windows) != 1 || report.Windows[0].Err != nil {
		t.Fatalf("redraw report = %+v, want the app-handled redraw without error", report.Windows)
	}

	// The next present syncs the surface extent with the resized window
	// on the rebuilt swap chain.
	sub, err := renderer.PresentClear(window, gpui.Color{R: 0.3, G: 0.6, B: 0.9, A: 1})
	if err != nil {
		t.Fatalf("PresentClear after the resize: %v", err)
	}
	newWidth, newHeight := surface.Size()
	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("window.ScaleFactor: %v", err)
	}
	wantWidth := uint32(float32(newW) * scale)
	wantHeight := uint32(float32(newH) * scale)
	if newWidth != wantWidth || newHeight != wantHeight {
		t.Fatalf("surface extent after the post-recovery present = %dx%d, want %dx%d (the resized device extent)",
			newWidth, newHeight, wantWidth, wantHeight)
	}
	if newWidth == oldWidth && newHeight == oldHeight {
		t.Fatalf("the surface extent did not change across the resize")
	}
	drain(t, sub, 15*time.Second)
}

// ---------------------------------------------------------------------------
// Shutdown: the renderer closes during recovery
// ---------------------------------------------------------------------------

// TestShutdownDuringRecovery proves the shutdown interaction: closing
// the renderer from inside the recovery's forced redraw is safe — the
// remaining windows' redraws observe the closed renderer, the sequence
// completes, and later recovery attempts refuse.
func TestShutdownDuringRecovery(t *testing.T) {
	app := bootApp(t)
	svc := mustNativeRenderer(t)
	windowA := openShown(t, app, "devicelossspec shutdown A", 260, 180)
	windowB := openShown(t, app, "devicelossspec shutdown B", 240, 160)

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	if _, err := renderer.GetOrCreateSurface(windowA); err != nil {
		t.Fatalf("GetOrCreateSurface A: %v", err)
	}
	if _, err := renderer.GetOrCreateSurface(windowB); err != nil {
		t.Fatalf("GetOrCreateSurface B: %v", err)
	}

	renderer.SetForcedRedraw(func(w *gpui.Window, surface *gpui.Surface) error {
		if w.ID() == windowA.ID() {
			// The shutdown: close the renderer mid-recovery (its
			// surfaces are destroyed; the recovery observes it).
			return renderer.Close()
		}
		// Window B's app redraw observes the closed renderer.
		_, err := renderer.PresentClear(w, gpui.Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
		return err
	})

	injectLoss(t, svc)
	report, err := renderer.RecoverDeviceLoss()
	if err != nil {
		t.Fatalf("RecoverDeviceLoss: %v", err)
	}
	if report.Windows[0].Err != nil {
		t.Fatalf("window A's redraw entry = %+v, want the callback-handled close", report.Windows[0])
	}
	if report.Windows[1].Err == nil || !strings.Contains(report.Windows[1].Err.Error(), "renderer is closed") {
		t.Fatalf("window B's redraw entry = %+v, want the closed-renderer refusal", report.Windows[1])
	}

	// The renderer is closed; later recovery attempts refuse.
	report, err = renderer.RecoverDeviceLoss()
	if err == nil || report == nil || report.Err == nil {
		t.Fatalf("RecoverDeviceLoss on a closed renderer = (%v, %v), want the closed refusal in both", report, err)
	}
}

// ---------------------------------------------------------------------------
// The closed renderer refuses the sequence outright
// ---------------------------------------------------------------------------

// TestRecoveryRefusedOnClosedRenderer pins the entry guard: a closed
// renderer refuses the recovery sequence before any native call.
func TestRecoveryRefusedOnClosedRenderer(t *testing.T) {
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatalf("renderer close: %v", err)
	}
	report, err := renderer.RecoverDeviceLoss()
	if err == nil {
		t.Fatalf("RecoverDeviceLoss on a closed renderer = nil error")
	}
	if report == nil || report.Err == nil || report.AlreadyHealthy {
		t.Fatalf("report = %+v, want the closed-renderer failure", report)
	}
}
