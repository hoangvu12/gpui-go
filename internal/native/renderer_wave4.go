// Renderer wave-4 client surface (tickets 29/32, the rev-10 artifact):
// the device-loss state records, the bounded-retry rebuild, the
// recovery gate and the pinned test-support render-to-image entry. The
// FFI wrappers live in renderer_wave4_windows.go (stubbed off Windows
// in renderer_other.go, the established pattern).

package native

import (
	"fmt"
	"unsafe"
)

// DeviceStateRecord mirrors GpuiGoDeviceStateRecord (24 bytes,
// alignment 4): the live device-loss state.
type DeviceStateRecord struct {
	// Lost is 1 when the device is lost (real or injected).
	Lost uint32
	// Injected is 1 when the loss is the test injection.
	Injected uint32
	// Recovering is 1 while a rebuild completed but the first forced
	// draw has not yet cleared the gate (the pin's skip_draws).
	Recovering uint32
	// RemovedReason is the removal HRESULT (0 = alive; the injected
	// marker is DXGI_ERROR_DEVICE_REMOVED).
	RemovedReason int32
	// DeviceGeneration counts completed rebuilds.
	DeviceGeneration uint32
	// RecordSize self-check.
	RecordSize uint32
}

// RebuildRecord mirrors GpuiGoRebuildRecord (32 bytes, alignment 4):
// the bounded-retry device rebuild outcome.
type RebuildRecord struct {
	// Attempts used (the pin's 5-attempt bound).
	Attempts uint32
	// RebuiltSurfaces rebuilt in place.
	RebuiltSurfaces uint32
	// FailedSurfaces whose recreation failed.
	FailedSurfaces uint32
	// AbortedSubmissions released by the rebuild (their handles are
	// stale afterwards — the safe-abort release path).
	AbortedSubmissions uint32
	// ClearedViews dropped from the capture surface-view cache.
	ClearedViews uint32
	// DeviceGeneration is the new generation (GPU caches must
	// invalidate below it).
	DeviceGeneration uint32
	// FailReasonHR is the HRESULT that failed the rebuild (0 = ok).
	FailReasonHR int32
	// RecordSize self-check.
	RecordSize uint32
}

// rendererStatusRecovering aliases rendererErrRecovering
// (renderer_status::ERR_RECOVERING, -13 since the rev-11 fix-up: the
// rev-10 value -12 collided with rendererErrScene).
const rendererStatusRecovering = rendererErrRecovering

// ErrRendererRecovering reports the recovery-gate refusal (the pin's
// skip_draws: draw, render, present and render-to-image skip while the
// replacement device's resources are not yet rebuilt; clear it with
// MarkDrawable).
var ErrRendererRecovering = fmt.Errorf("native: renderer is recovering from device loss (the frame was skipped)")

// DeviceState returns the live device-loss state (ticket29): real and
// injected loss recorded separately, the recovery gate, the removal
// reason and the device generation.
func (s *RendererService) DeviceState() (DeviceStateRecord, error) {
	if s == nil {
		return DeviceStateRecord{}, fmt.Errorf("native: no renderer service")
	}
	var out DeviceStateRecord
	code, err := callRendererDeviceStateCheck(s.table.deviceStateCheck, &out)
	if err != nil {
		return DeviceStateRecord{}, err
	}
	if code != rendererStatusOK {
		return DeviceStateRecord{}, rendererStatusError(code, "renderer_device_state_check")
	}
	if out.RecordSize != uint32(unsafe.Sizeof(DeviceStateRecord{})) {
		return DeviceStateRecord{}, fmt.Errorf("native: device-state record size %d does not match the mirror", out.RecordSize)
	}
	return out, nil
}

// InjectDeviceLoss arms the test-injected loss flag (test-only; the
// state record reports it separately from real loss).
func (s *RendererService) InjectDeviceLoss() error {
	if s == nil {
		return fmt.Errorf("native: no renderer service")
	}
	code, err := callRendererDeviceInjectLoss(s.table.deviceInjectLoss)
	if err != nil {
		return err
	}
	if code != rendererStatusOK {
		return rendererStatusError(code, "renderer_device_inject_loss")
	}
	return nil
}

// RebuildDevice performs the pin's bounded-retry device recovery: the
// device is recreated, every surface's swap chain is rebuilt in place
// (surface identity and handles persist), in-flight submissions are
// aborted (their handles become stale — the safe-abort release path),
// the draw globals and the capture surface-view cache are dropped for
// lazy rebuild, and the recovery gate is armed until MarkDrawable. The
// Go side must follow the rebuild with the atlas NotifyDeviceLost call
// per live atlas handle (the device generation boundary invalidates
// every GPU cache) and a forced redraw.
func (s *RendererService) RebuildDevice() (RebuildRecord, error) {
	if s == nil {
		return RebuildRecord{}, fmt.Errorf("native: no renderer service")
	}
	var out RebuildRecord
	code, err := callRendererDeviceRebuild(s.table.deviceRebuild, &out)
	if err != nil {
		return RebuildRecord{}, err
	}
	if code != rendererStatusOK {
		return RebuildRecord{}, rendererStatusError(code, "renderer_device_rebuild")
	}
	if out.RecordSize != uint32(unsafe.Sizeof(RebuildRecord{})) {
		return RebuildRecord{}, fmt.Errorf("native: rebuild record size %d does not match the mirror", out.RecordSize)
	}
	return out, nil
}

// MarkDrawable clears the recovery gate (the pin's mark_drawable,
// called on the first forced draw after recovery).
func (s *RendererService) MarkDrawable() error {
	if s == nil {
		return fmt.Errorf("native: no renderer service")
	}
	code, err := callRendererDeviceMarkDrawable(s.table.deviceMarkDrawable)
	if err != nil {
		return err
	}
	if code != rendererStatusOK {
		return rendererStatusError(code, "renderer_device_mark_drawable")
	}
	return nil
}

// RenderToImage is the pinned test-support path (ticket32): render the
// scene without presenting, then stage-copy the render target into
// owned RGBA bytes (the staging copy, the row-pitch handling and the
// BGRA->RGBA swap are native). The capacity protocol: a nil or
// too-small buffer returns the needed size with a nil error; retry
// with it. The returned bytes are owned (a fresh copy).
func (s *RendererService) RenderToImage(
	surface SurfaceHandle,
	scene SceneHandle,
	appearance BackgroundAppearance,
	atlas AtlasHandle,
	out []byte,
) (pixels []byte, needed uint32, err error) {
	if s == nil {
		return nil, 0, fmt.Errorf("native: no renderer service")
	}
	needed, code, err := callRendererSurfaceRenderToImage(
		s.table.surfaceRenderToImage,
		uint64(surface),
		uint64(scene),
		uint32(appearance),
		uint64(atlas),
		out,
	)
	if err != nil {
		return nil, 0, err
	}
	switch code {
	case rendererStatusOK:
		return out[:needed], needed, nil
	case rendererErrCapacity:
		// The retry contract: report the need, no error.
		return nil, needed, nil
	case rendererStatusRecovering:
		return nil, 0, ErrRendererRecovering
	default:
		return nil, 0, rendererStatusError(code, "renderer_surface_render_to_image")
	}
}
