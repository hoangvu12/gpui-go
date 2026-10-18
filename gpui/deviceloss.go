//go:build windows

package gpui

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"gpui-go/internal/native"
)

// This file is ticket29's Go half: the device-loss detection and
// recovery controller over the native renderer service's device-loss
// entries (internal/native/renderer_wave4.go, the rev-10 artifact) and
// the recovery orchestration the renderer model drives.
//
// The pinned reference is CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/platform.rs 1416-1475 — check_device_lost
//     (the vsync thread's GetDeviceRemovedReason probe, once per
//     vblank) and handle_gpu_device_lost: the 350ms settle ("if we
//     don't wait, the final drawing result will be blank"), the device
//     recreation, WM_GPUI_GPU_DEVICE_LOST to every window, the 200ms
//     pause, then WM_GPUI_FORCE_UPDATE_WINDOW forcing every window to
//     redraw;
//   - crates/gpui_windows/src/directx_renderer.rs — skip_draws
//     (84/380/522: armed by a completed recovery, the first draw is
//     discarded), handle_device_lost_impl (452-530: tear the old device
//     down, recreate, rebuild each window's swap chain and composition
//     tree in place, rebind the atlas, arm the gate) and mark_drawable
//     (1507);
//   - crates/gpui_windows/src/directx_devices.rs —
//     try_to_recover_from_device_lost: the 5-attempt bounded retry with
//     the 100ms + i*10ms backoff;
//   - crates/gpui_windows/src/events.rs 1287-1296 — the window's
//     WM_GPUI_GPU_DEVICE_LOST handler rebuilds its renderer and arms
//     force_render_pending; draw_window (1329-1334) calls mark_drawable
//     on the first forced draw before requesting the frame.
//
// The port's split: the native service owns the settle, the bounded
// retry, the in-place swap-chain rebuild, the in-flight submission
// abort (the safe-abort release: aborted handles go stale) and the
// recovery gate (ERR_RECOVERING on present/draw/render while armed).
// This controller owns the detection cadence (the pacing worker's
// probe, the vsync-thread check), the recovery sequence (rebuild ->
// atlas invalidation at the device-generation boundary -> per-window
// forced redraw -> mark-drawable), the ledger's abort bookkeeping for
// the natively aborted submissions, and the observability of a skipped
// frame ("skipped, redraw pending" in the ledger — the pin's skip is a
// silent success; the port's is never silent).
//
// Recorded deviations of this slice:
//
//   - The native renderer_status codes ERR_RECOVERING and ERR_SCENE
//     share -12, so a present/draw refusal during the gate is
//     disambiguated by probing DeviceState().Recovering (the native
//     gate check precedes every other -12 source in those entries).
//     The Go client of the native service (internal/native) cannot be
//     extended from this slice, so the disambiguation lives here.
//   - The rev-10 native rebuild's in-place surface recreation fails
//     whenever the old swap chain / composition tree is still attached
//     (FailedSurfaces with a DComp/DXGI error — the pin's
//     handle_device_lost_impl takes resources and direct_composition
//     BEFORE recreating them, directx_renderer.rs 452-530; the native
//     port replaces the fields only on success, so the old DComp
//     target/swap chain is still bound when the new one is created).
//     The controller compensates with the fresh-surface path: when the
//     rebuild reports any failed surface, every live surface is
//     destroyed and recreated on the replacement device (fresh
//     handles, fresh swap chains) before the forced redraw — recorded
//     per window (WindowRedraw.FreshSurface). The rebuild record
//     identifies failures only by count, so one failure recreates every
//     live surface (the conservative path; the healthy in-place case
//     keeps the surface identity the native contract promises).
//   - The pin's per-renderer atlas rebind happens inside the renderer
//     rebuild; the port's atlases are app-owned native handles, so the
//     notify seam is the registry below plus each window's
//     text-system atlas (the element pipeline's per-window atlas),
//     enumerated on the foreground and notified per live handle.
//   - Real driver removal cannot be forced on demand on this hardware;
//     the injected identity (InjectDeviceLoss) is recorded separately
//     from the real-loss identity in every state and report record.
//     The bounded paths behind real loss (poll failures, retire
//     quarantine, the rebuild abort) are exercised through injection.

// ---------------------------------------------------------------------------
// The skip sentinel
// ---------------------------------------------------------------------------

// ErrRendererRecovering reports the recovery-gate refusal: a present,
// draw, render or render-to-image was skipped because the renderer is
// recovering from device loss and the replacement device's resources
// are not yet rebuilt (the pin's skip_draws gate — the render-to-image
// seam of ticket32 refuses through the same sentinel). On the frame
// submission paths the skip is recorded in the submission ledger
// ("skipped, redraw pending") — never a silent success. The gate is
// cleared on the first forced draw after the rebuild
// ([Renderer.MarkDrawable], the pin's mark_drawable).
var ErrRendererRecovering = fmt.Errorf("gpui: frame skipped: the renderer is recovering from device loss; a forced redraw is pending: %w", native.ErrRendererRecovering)

// recoveryGateStatus is the native ERR_RECOVERING status code (-13
// since the rev-11 fix-up; the rev-10 value -12 collided with
// ERR_SCENE, which this probe used to disambiguate). The gate probe
// stays as a belt-and-braces confirmation.
const recoveryGateStatus = -13

// isRecoveryRefusal reports whether a native renderer error is the
// recovery-gate refusal rather than the colliding scene rejection.
func (r *Renderer) isRecoveryRefusal(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *native.RendererStatusError
	if !errors.As(err, &statusErr) || statusErr.Code != recoveryGateStatus {
		return false
	}
	state, probeErr := r.svc.DeviceState()
	if probeErr != nil {
		return false
	}
	return state.Recovering != 0
}

// ---------------------------------------------------------------------------
// State and report records
// ---------------------------------------------------------------------------

// DeviceLossState is the live device-loss state: the native probe (real
// and injected loss recorded separately, the recovery gate, the removal
// reason, the device generation) plus the controller's sequence
// bookkeeping.
type DeviceLossState struct {
	// Lost reports a confirmed device loss (real driver removal or the
	// test injection).
	Lost bool
	// Injected reports the test-injected identity of the loss (real
	// loss is recorded separately; the injection reports the
	// DXGI_ERROR_DEVICE_REMOVED marker).
	Injected bool
	// Recovering reports the native recovery gate armed: present, draw
	// and render refuse with [ErrRendererRecovering] until the first
	// forced draw clears it.
	Recovering bool
	// RemovedReason is the confirmed removal HRESULT (0 = alive).
	RemovedReason int32
	// DeviceGeneration counts completed rebuilds (the generation
	// boundary that invalidates GPU caches).
	DeviceGeneration uint32
	// RecoveringNow reports a recovery sequence in flight (Go-side).
	RecoveringNow bool
	// Recoveries counts completed recovery sequences.
	Recoveries int
	// FailedRecoveries counts consecutive failed sequences (a rebuild
	// that exhausted its attempts).
	FailedRecoveries int
	// TerminalFailure reports the automatic-recovery bound was hit:
	// the pacing worker stops triggering recoveries; explicit
	// [Renderer.RecoverDeviceLoss] calls still run. The pin aborts the
	// process on a failed handle_gpu_device_lost; the port reports the
	// terminal failure instead.
	TerminalFailure bool
}

// DeviceLossReport is the observable record of one recovery sequence.
type DeviceLossReport struct {
	// Injected reports the test-injected identity of the detected loss
	// (real loss is recorded separately).
	Injected bool
	// RemovedReason is the confirmed removal reason at detection.
	RemovedReason int32
	// StartedAt and CompletedAt bracket the sequence.
	StartedAt, CompletedAt time.Time
	// AlreadyHealthy: the probe found no loss (or a concurrent sequence
	// had already recovered it); nothing was rebuilt.
	AlreadyHealthy bool
	// Rebuild is the native rebuild record: attempts used, surfaces
	// rebuilt in place, surfaces that failed to rebuild, submissions
	// aborted by the rebuild, capture surface views dropped, and the
	// new device generation.
	Rebuild native.RebuildRecord
	// AbortedSubmissions lists the ledger identities closed through the
	// rebuild's loss-abort terminal (their native handles are stale
	// afterwards).
	AbortedSubmissions []uint64
	// Atlases records the per-atlas invalidation at the generation
	// boundary.
	Atlases []AtlasInvalidation
	// Windows records the per-window forced redraw.
	Windows []WindowRedraw
	// SkippedFrames counts the ledger skip events observed during the
	// sequence (gate refusals of the forced redraw probes).
	SkippedFrames int
	// RecreatedSurfaces counts surfaces destroyed and recreated on the
	// replacement device (the fresh-surface path taken when the
	// rebuild's in-place surface recreation failed for at least one
	// surface; the old handles are stale afterwards).
	RecreatedSurfaces int
	// MarkedDrawable reports that the recovery gate was cleared for the
	// first forced draw (the pin's mark_drawable).
	MarkedDrawable bool
	// Err is the sequence's terminal failure (nil on success).
	Err error
}

// AtlasInvalidation records one atlas handle's device-loss notify: the
// pools and tiles cleared and the generation bumped (the rebind seam of
// the pin's atlas handle_device_lost). A stale handle (a disposed
// atlas) is recorded with Err — it was not live, nothing was
// invalidated.
type AtlasInvalidation struct {
	// Atlas is the native atlas handle.
	Atlas native.AtlasHandle
	// GenerationBefore and GenerationAfter bracket the notify.
	GenerationBefore, GenerationAfter uint32
	// TilesBefore and TilesAfter report the live tile keys (zero after
	// the notify: every tile is generation-invalidated).
	TilesBefore, TilesAfter int
	// Err is the notify error, if any.
	Err error
}

// WindowRedraw records one window's forced redraw during recovery.
type WindowRedraw struct {
	// Window is the window identity.
	Window WindowID
	// Skipped: the gate refused the window's probe present and the
	// ledger recorded the skipped frame ("skipped, redraw pending").
	Skipped bool
	// FreshSurface: the surface was destroyed and recreated on the
	// replacement device (the in-place rebuild had failed; the old
	// handle is stale, the redraw went through the fresh swap chain).
	FreshSurface bool
	// Submission is the forced-redraw submission when the controller
	// produced one (nil when the app's callback owned the redraw, or
	// the redraw failed and Err is set). ID() is its ledger identity.
	Submission *Submission
	// Err is the per-window redraw failure (the window closed, the
	// host unavailable, a frame-draw or submit error).
	Err error
}

// ForcedRedrawFunc is the app's per-window forced-redraw seam: the
// recovery sequence drives it once per window after the rebuild (the
// pin's request_frame(force_render: true) after mark_drawable, which
// bypasses the retained-frame cache so stale atlas tiles re-insert).
// Returning nil reports the app redrew the window itself; the error is
// recorded per window in the recovery report. The callback runs on the
// recovery goroutine — foreground work must be marshaled by the
// callback.
type ForcedRedrawFunc func(w *Window, surface *Surface) error

// ---------------------------------------------------------------------------
// The controller
// ---------------------------------------------------------------------------

// Detection and retry bounds.
const (
	// deviceLossPollTicks spaces the pacing worker's device-state
	// probes: one probe per 8 rounds (16ms at the 2ms poll cadence —
	// the pin's one-check-per-vblank cadence of its vsync thread).
	deviceLossPollTicks = 8
	// maxConsecutiveFailedRecoveries bounds the automatic recovery
	// attempts after repeated terminal rebuild failures (see
	// DeviceLossState.TerminalFailure).
	maxConsecutiveFailedRecoveries = 3
)

// The gate probe and the rootless default redraw's clear colors. The
// probe's color only reaches a swap chain when the gate was already
// cleared elsewhere (the refusal precedes any GPU work); the rootless
// redraw re-establishes the rebuilt swap chain with the pin's opaque
// background clear.
var (
	recoveryProbeClear  = Color{R: 0, G: 0, B: 0, A: 0}
	recoveryRedrawClear = Color{R: 1, G: 1, B: 1, A: 1}
)

// deviceLoss is the detection/recovery controller of one renderer. The
// sequence itself is serialized by seqMu; mu guards the bookkeeping
// fields read by DeviceLossState. The pacing worker calls pollDetection
// from its own goroutine (it is the only tick writer).
type deviceLoss struct {
	renderer *Renderer

	mu         sync.Mutex
	registered []*Atlas
	redrawFn   ForcedRedrawFunc
	running    bool
	recoveries int
	failures   int
	terminal   bool
	lastReport *DeviceLossReport

	// seqMu serializes recovery sequences: the pacing worker's
	// auto-trigger and explicit RecoverDeviceLoss calls queue behind
	// the running one (the re-probe turns a stale trigger into an
	// AlreadyHealthy record).
	seqMu sync.Mutex

	// tickCount spaces the detection probes (pacing-worker only).
	tickCount int
}

// newDeviceLoss creates the controller for a renderer.
func newDeviceLoss(r *Renderer) *deviceLoss {
	return &deviceLoss{renderer: r}
}

// ---------------------------------------------------------------------------
// Detection (the pacing worker's probe)
// ---------------------------------------------------------------------------

// pollDetection is the pacing worker's loss probe (the pin's vsync
// thread check_device_lost): one device-state probe every
// deviceLossPollTicks rounds. A lost device (real or injected) triggers
// the recovery sequence on a dedicated goroutine — never the foreground
// thread and never this worker inline (the pin's vsync thread runs the
// recovery inline; the port keeps the pacing worker's poll rounds
// responsive and serializes the sequences instead).
func (c *deviceLoss) pollDetection() {
	c.tickCount++
	if c.tickCount%deviceLossPollTicks != 0 {
		return
	}
	state, err := c.renderer.svc.DeviceState()
	if err != nil || state.Lost == 0 {
		return
	}
	c.trigger()
}

// trigger launches the recovery sequence once (the running flag and the
// terminal bound are check-and-set under mu).
func (c *deviceLoss) trigger() {
	c.mu.Lock()
	if c.running || c.terminal {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
		}()
		c.seqMu.Lock()
		defer c.seqMu.Unlock()
		_, _ = c.sequence()
	}()
}

// ---------------------------------------------------------------------------
// The recovery sequence
// ---------------------------------------------------------------------------

// sequence drives one recovery (the pin's handle_gpu_device_lost): the
// native bounded-retry rebuild, the ledger's abort bookkeeping for the
// natively aborted submissions, the atlas invalidation at the
// generation boundary, the per-window forced redraws, and the
// mark-drawable on the first forced draw. Callers serialize through
// seqMu; the controller's counters are updated under mu.
func (c *deviceLoss) sequence() (*DeviceLossReport, error) {
	r := c.renderer
	report := &DeviceLossReport{StartedAt: time.Now()}
	finish := func(err error) (*DeviceLossReport, error) {
		report.CompletedAt = time.Now()
		report.Err = err
		c.recordOutcome(report, err != nil)
		return report, err
	}

	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return finish(errors.New("gpui: the renderer is closed"))
	}

	// The detection identity: real and injected loss are recorded
	// separately (the injected marker reports the
	// DXGI_ERROR_DEVICE_REMOVED reason).
	state, err := r.svc.DeviceState()
	if err != nil {
		return finish(fmt.Errorf("gpui: probing the device-loss state: %w", err))
	}
	report.Injected = state.Injected != 0
	report.RemovedReason = state.RemovedReason
	if state.Lost == 0 {
		// Not lost — or a concurrent sequence already recovered it.
		report.AlreadyHealthy = true
		return finish(nil)
	}
	skipsBefore := r.ledger.skippedCount()

	// 1. The bounded-retry rebuild (the pin's 350ms settle and the
	//    5-attempt try_to_recover_from_device_lost live in the native
	//    entry): the device is recreated, every surface's swap chain is
	//    rebuilt in place (handles persist), the in-flight submissions
	//    are aborted and the recovery gate is armed.
	record, err := r.svc.RebuildDevice()
	report.Rebuild = record
	if err != nil {
		return finish(fmt.Errorf("gpui: the device rebuild failed after %d attempt(s) (hr %#x): %w", record.Attempts, record.FailReasonHR, err))
	}

	// 2. Abort bookkeeping: the rebuild aborted every tracked native
	//    submission (the backend's no-further-use guarantee; the
	//    handles are stale afterwards). Close the ledger records with
	//    the documented loss-abort terminal so no aborted dependency
	//    stays silently pending — and release the Go-side in-flight
	//    bound.
	for _, handle := range r.ledger.pending() {
		if id, ok := r.ledger.retireAborted(handle, state.RemovedReason, false); ok {
			report.AbortedSubmissions = append(report.AbortedSubmissions, id)
			r.finishInFlight(handle)
		}
	}

	// 3. Invalidate every live atlas handle at the generation boundary
	//    (the pin's atlas rebind: pools and tiles cleared, generation
	//    bumped, tiles from the old generation are invalid).
	c.invalidateAtlases(report)

	// 4. Redraw every window with fresh surfaces: the gate refuses the
	//    first forced draw of each window (the observable skip), then
	//    mark_drawable clears the gate for the real redraws.
	c.redrawWindows(report)

	report.SkippedFrames = r.ledger.skippedCount() - skipsBefore
	return finish(nil)
}

// recordOutcome folds one finished sequence into the controller state.
func (c *deviceLoss) recordOutcome(report *DeviceLossReport, failed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastReport = report
	if failed {
		c.failures++
		if c.failures >= maxConsecutiveFailedRecoveries {
			c.terminal = true
		}
		return
	}
	if !report.AlreadyHealthy {
		c.recoveries++
		c.failures = 0
		c.terminal = false
	}
}

// invalidateAtlases notifies every live atlas handle of the device loss
// (the pin's atlas.handle_device_lost): the app's registered atlases
// plus each window's text-system atlas (the element pipeline's
// per-window atlas, enumerated on the foreground — the draw-state
// registry is foreground-only).
func (c *deviceLoss) invalidateAtlases(report *DeviceLossReport) {
	r := c.renderer
	c.mu.Lock()
	registered := append([]*Atlas(nil), c.registered...)
	c.mu.Unlock()

	// Snapshot the live surfaces; enumerate their windows' text atlases
	// on the foreground (SendMessageW semantics: no foreground, no
	// window atlases).
	r.mu.Lock()
	closed := r.closed
	surfaces := make([]*Surface, 0, len(r.surfaces))
	for _, surface := range r.surfaces {
		surfaces = append(surfaces, surface)
	}
	r.mu.Unlock()
	var windowAtlases []*Atlas
	if !closed {
		c.foreground(func() {
			for _, surface := range surfaces {
				if surface.window == nil {
					continue
				}
				if ds, ok := windowDrawStates[surface.window]; ok {
					if rts, ok := ds.text.(*realWindowTextSystem); ok && rts.atlas != nil {
						windowAtlases = append(windowAtlases, rts.atlas)
					}
				}
			}
		})
	}

	seen := make(map[native.AtlasHandle]struct{})
	for _, atlas := range append(registered, windowAtlases...) {
		if atlas == nil || atlas.handle == 0 {
			continue
		}
		if _, dup := seen[atlas.handle]; dup {
			continue
		}
		seen[atlas.handle] = struct{}{}
		entry := AtlasInvalidation{Atlas: atlas.handle}
		if generation, err := atlas.Generation(); err == nil {
			entry.GenerationBefore = generation
		}
		if tiles, err := atlas.TileCount(); err == nil {
			entry.TilesBefore = tiles
		}
		if err := atlas.NotifyDeviceLost(); err != nil {
			// Not live (disposed) or the notify failed: recorded, not
			// fatal — the recovery must proceed for the live handles.
			entry.Err = fmt.Errorf("gpui: notifying the atlas of the device loss: %w", err)
		} else {
			if generation, err := atlas.Generation(); err == nil {
				entry.GenerationAfter = generation
			}
			if tiles, err := atlas.TileCount(); err == nil {
				entry.TilesAfter = tiles
			}
		}
		report.Atlases = append(report.Atlases, entry)
	}
}

// redrawWindows drives the per-window forced redraw (the pin's
// WM_GPUI_FORCE_UPDATE_WINDOW pass): every window's gate probe first —
// the refusals land in the ledger as skipped frames — then
// mark_drawable (the first forced draw), then the real redraws.
func (c *deviceLoss) redrawWindows(report *DeviceLossReport) {
	r := c.renderer
	r.mu.Lock()
	closed := r.closed
	surfaces := make([]*Surface, 0, len(r.surfaces))
	for _, surface := range r.surfaces {
		surfaces = append(surfaces, surface)
	}
	r.mu.Unlock()
	if closed {
		return
	}
	sort.Slice(surfaces, func(i, j int) bool {
		return surfaces[i].windowID < surfaces[j].windowID
	})

	// The gate probes: one draw attempt per window while the gate is
	// armed. The refusal happens before any GPU work and is recorded in
	// the ledger ("skipped, redraw pending") — never a silent success.
	// A window whose in-place rebuild failed gets a fresh surface on
	// the replacement device first (the old handle is stale).
	freshNeeded := report.Rebuild.FailedSurfaces > 0
	entries := make([]WindowRedraw, len(surfaces))
	for i, surface := range surfaces {
		entries[i] = WindowRedraw{Window: surface.windowID}
		if surface.window != nil && !surface.window.Alive() {
			entries[i].Err = fmt.Errorf("gpui: window %d closed before its forced redraw: %w", surface.windowID, ErrRendererWindowClosed)
			continue
		}
		if freshNeeded {
			if err := c.recreateSurface(surface); err != nil {
				entries[i].Err = err
				continue
			}
			entries[i].FreshSurface = true
			report.RecreatedSurfaces++
		}
		if _, err := surface.PresentClear(recoveryProbeClear); err != nil {
			if errors.Is(err, ErrRendererRecovering) {
				entries[i].Skipped = true
			} else if !errors.Is(err, ErrRendererWindowClosed) {
				entries[i].Err = fmt.Errorf("gpui: the forced-redraw probe of window %d: %w", surface.windowID, err)
			}
		}
	}

	// Mark drawable: the pin's mark_drawable on the first forced draw
	// (events.rs draw_window) — the gate clears for the real redraws.
	gateErr := r.svc.MarkDrawable()
	report.MarkedDrawable = gateErr == nil
	if gateErr != nil {
		gateErr = fmt.Errorf("gpui: clearing the recovery gate: %w", gateErr)
	}

	for i, surface := range surfaces {
		if entries[i].Err != nil {
			continue
		}
		if gateErr != nil {
			entries[i].Err = gateErr
			continue
		}
		c.redrawWindow(surface, &entries[i])
	}
	report.Windows = entries
}

// redrawWindow performs one window's real forced redraw: the app's
// callback (the pin's request_frame seam) when installed, otherwise the
// renderer default — a fresh frame through the element pipeline when
// the window has a root view (the pin's forced render, which bypasses
// the retained-frame cache so stale atlas tiles re-insert), or one
// clear-frame present re-establishing the rebuilt swap chain.
func (c *deviceLoss) redrawWindow(surface *Surface, entry *WindowRedraw) {
	if surface.window != nil && !surface.window.Alive() {
		entry.Err = fmt.Errorf("gpui: window %d closed before its forced redraw: %w", surface.windowID, ErrRendererWindowClosed)
		return
	}
	if fn := c.redrawFunc(); fn != nil {
		if err := fn(surface.window, surface); err != nil {
			entry.Err = fmt.Errorf("gpui: the forced redraw of window %d: %w", surface.windowID, err)
		}
		return
	}

	// The default redraw. The element pipeline runs on the foreground
	// (the draw-state registry and the app update boundary): the frame
	// construction is marshaled there like the pin's window message.
	type frameOutcome struct {
		hasRoot bool
		scene   *Scene
		atlas   *Atlas
		err     error
	}
	var frame frameOutcome
	c.foreground(func() {
		if surface.window == nil || !surface.window.Alive() {
			frame.err = fmt.Errorf("gpui: window %d closed before its forced redraw: %w", surface.windowID, ErrRendererWindowClosed)
			return
		}
		ds, ok := windowDrawStates[surface.window]
		if !ok || ds.root == nil {
			// No root view: no app frame path — the clear present
			// fallback below re-establishes the rebuilt swap chain.
			return
		}
		if rts, ok := ds.text.(*realWindowTextSystem); ok {
			frame.atlas = rts.atlas
		}
		scene, err := DrawWindowFrame(surface.window)
		if err != nil {
			frame.err = fmt.Errorf("gpui: the forced frame draw of window %d: %w", surface.windowID, err)
			return
		}
		frame.hasRoot = true
		frame.scene = scene
	})
	if frame.err != nil {
		entry.Err = frame.err
		return
	}
	if !frame.hasRoot {
		// No root view, or no foreground available (no attached host,
		// or the host stopped): a root-view window cannot exist without
		// its host lease, so this is the rootless clear-present path.
		c.presentClearRedraw(surface, entry)
		return
	}

	// The fresh frame's sprite textures: the window's text atlas (the
	// frame's paint re-inserted its tiles after the notify), or the
	// first registered atlas. A sprite frame without any live atlas is
	// an honest per-window failure.
	atlas := frame.atlas
	if atlas == nil {
		atlas = c.firstRegisteredAtlas()
	}
	if atlas == nil {
		needs, err := sceneNeedsSpriteAtlas(frame.scene)
		if err != nil {
			entry.Err = fmt.Errorf("gpui: the forced redraw of window %d: %w", surface.windowID, err)
			return
		}
		if needs {
			entry.Err = fmt.Errorf("gpui: the forced redraw of window %d needs sprite textures but no atlas is live", surface.windowID)
			return
		}
	}
	sub, err := surface.DrawScene(frame.scene, atlas, BackgroundOpaque)
	if err != nil {
		entry.Err = fmt.Errorf("gpui: the forced redraw present of window %d: %w", surface.windowID, err)
		return
	}
	entry.Submission = sub
}

// presentClearRedraw submits the rootless default redraw: one
// clear-frame present with the pin's opaque background clear,
// re-establishing the rebuilt swap chain's content.
func (c *deviceLoss) presentClearRedraw(surface *Surface, entry *WindowRedraw) {
	sub, err := surface.PresentClear(recoveryRedrawClear)
	if err != nil {
		entry.Err = fmt.Errorf("gpui: the forced redraw present of window %d: %w", surface.windowID, err)
		return
	}
	entry.Submission = sub
}

// recreateSurface destroys the surface and creates a fresh one on the
// replacement device (the fresh-surface path of the recovery: the
// in-place rebuild failed, so the old swap chain and composition tree
// are still bound to the lost device; the fresh chain carries the new
// device). The Go-side surface object persists with its new handle; the
// old handle is stale. The new surface starts at the last synced
// extent — the present boundary's resize-at-a-safe-point re-syncs it.
func (c *deviceLoss) recreateSurface(surface *Surface) error {
	r := c.renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("gpui: the renderer is closed")
	}
	// The rebuild aborted every in-flight submission, so the destroy is
	// not refused for outstanding work.
	if err := r.svc.DestroySurface(surface.handle); err != nil {
		return fmt.Errorf("gpui: destroying the stale surface of window %d: %w", surface.windowID, err)
	}
	handle, err := r.svc.CreateSurface(surface.hwnd, surface.mode, surface.width, surface.height, surface.scale)
	if err != nil {
		// The surface is gone; the window needs a fresh attach through
		// GetOrCreateSurface to draw again.
		delete(r.surfaces, surface.windowID)
		return fmt.Errorf("gpui: recreating the surface of window %d on the replacement device: %w", surface.windowID, err)
	}
	surface.handle = handle
	return nil
}

// sceneNeedsSpriteAtlas reports whether the finished scene carries
// sprite batches that need an atlas handle at the draw seam.
func sceneNeedsSpriteAtlas(scene *Scene) (bool, error) {
	meta, err := scene.Meta()
	if err != nil {
		return false, fmt.Errorf("scene meta: %w", err)
	}
	return meta.MonochromeSpriteCount+meta.SubpixelSpriteCount+meta.PolychromeSpriteCount > 0, nil
}

// foreground runs f on the foreground (host) thread and waits for it —
// the pin's SendMessageW semantics for the per-window recovery
// messages. It reports false when no foreground is available (no
// attached application/host, or the host stopped accepting work).
func (c *deviceLoss) foreground(f func()) bool {
	app := c.renderer.app.Load()
	if app == nil {
		return false
	}
	host := app.Host()
	if host == nil {
		return false
	}
	return host.runForegroundSync(f)
}

// redrawFunc returns the installed forced-redraw callback.
func (c *deviceLoss) redrawFunc() ForcedRedrawFunc {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.redrawFn
}

// firstRegisteredAtlas returns the first registered live atlas (the
// fallback sprite-texture handle for the default redraw).
func (c *deviceLoss) firstRegisteredAtlas() *Atlas {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, atlas := range c.registered {
		if atlas != nil && atlas.handle != 0 {
			return atlas
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Public seams (Renderer methods)
// ---------------------------------------------------------------------------

// DeviceLossState returns the live device-loss state: the native probe
// (loss, gate, removal reason, device generation — real and injected
// identities recorded separately) and the controller's sequence
// bookkeeping.
func (r *Renderer) DeviceLossState() (DeviceLossState, error) {
	var state DeviceLossState
	nativeState, err := r.svc.DeviceState()
	if err != nil {
		return state, fmt.Errorf("gpui: probing the device-loss state: %w", err)
	}
	state.Lost = nativeState.Lost != 0
	state.Injected = nativeState.Injected != 0
	state.Recovering = nativeState.Recovering != 0
	state.RemovedReason = nativeState.RemovedReason
	state.DeviceGeneration = nativeState.DeviceGeneration
	c := r.loss
	c.mu.Lock()
	state.RecoveringNow = c.running
	state.Recoveries = c.recoveries
	state.FailedRecoveries = c.failures
	state.TerminalFailure = c.terminal
	c.mu.Unlock()
	return state, nil
}

// DeviceLossReport returns the last finished recovery sequence's record
// (nil before the first).
func (r *Renderer) DeviceLossReport() *DeviceLossReport {
	c := r.loss
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastReport
}

// RecoverDeviceLoss drives the recovery sequence synchronously on the
// calling goroutine (the automatic path runs it on a dedicated
// goroutine from the pacing worker's detection probe): the
// bounded-retry rebuild, the abort bookkeeping, the atlas
// invalidation, the per-window forced redraws and the mark-drawable.
// Concurrent sequences serialize; a re-probe that finds the device
// healthy again returns an AlreadyHealthy report. Do not call this from
// the app's draw path (the sequence marshals frame draws to the
// foreground and waits for them).
func (r *Renderer) RecoverDeviceLoss() (*DeviceLossReport, error) {
	c := r.loss
	c.seqMu.Lock()
	defer c.seqMu.Unlock()
	return c.sequence()
}

// RegisterAtlas registers one of the application's atlases with the
// renderer's device-loss recovery: every registered live handle is
// notified (generation bump, pools cleared) at the rebuild boundary.
// Registration is idempotent.
func (r *Renderer) RegisterAtlas(atlas *Atlas) error {
	if atlas == nil || atlas.handle == 0 {
		return errors.New("gpui: RegisterAtlas requires a live atlas")
	}
	c := r.loss
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, existing := range c.registered {
		if existing == atlas {
			return nil
		}
	}
	c.registered = append(c.registered, atlas)
	return nil
}

// UnregisterAtlas removes a registered atlas from the recovery notify
// set (the atlas is being disposed or handed off).
func (r *Renderer) UnregisterAtlas(atlas *Atlas) {
	if atlas == nil {
		return
	}
	c := r.loss
	c.mu.Lock()
	defer c.mu.Unlock()
	registered := make([]*Atlas, 0, len(c.registered))
	for _, existing := range c.registered {
		if existing != atlas {
			registered = append(registered, existing)
		}
	}
	c.registered = registered
}

// SetForcedRedraw installs the app's per-window forced-redraw callback
// (the pin's request_frame seam driven by the recovery's forced update
// pass). A nil callback restores the renderer default (a fresh frame
// through the element pipeline for root-view windows, a clear present
// otherwise).
func (r *Renderer) SetForcedRedraw(fn ForcedRedrawFunc) {
	c := r.loss
	c.mu.Lock()
	defer c.mu.Unlock()
	c.redrawFn = fn
}

// MarkDrawable clears the native recovery gate (the pin's
// mark_drawable, called on the first forced draw after recovery). The
// recovery sequence calls it automatically; the seam exists for
// app-driven recovery flows.
func (r *Renderer) MarkDrawable() error {
	if err := r.svc.MarkDrawable(); err != nil {
		return fmt.Errorf("gpui: clearing the recovery gate: %w", err)
	}
	return nil
}

// SkippedFrames returns the number of frames the ledger recorded as
// skipped while the renderer was recovering from device loss (the pin's
// skip_draws frames — observable, never silent).
func (r *Renderer) SkippedFrames() int {
	return r.ledger.skippedCount()
}
