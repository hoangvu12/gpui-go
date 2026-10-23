// Frame-latency waitable surfaces (gpui-perf ticket 05, renderer service
// version 4). Every swap chain is created with
// DXGI_SWAP_CHAIN_FLAG_FRAME_LATENCY_WAITABLE_OBJECT and a maximum frame
// latency of 1; the waitable object is signaled when the swap chain can
// accept another frame. The FFI wrapper lives in
// renderer_latency_windows.go (stubbed off Windows in renderer_other.go).

package native

import (
	"fmt"
	"time"
)

// FrameLatencyWait is the outcome of WaitFrameLatency.
type FrameLatencyWait uint8

const (
	// FrameLatencySignaled: the swap chain can accept a new frame.
	FrameLatencySignaled FrameLatencyWait = iota
	// FrameLatencyTimeout: the timeout passed first.
	FrameLatencyTimeout
	// FrameLatencyUnavailable: the surface has no waitable object (its
	// swap chain did not expose IDXGISwapChain2); nothing was waited on.
	FrameLatencyUnavailable
)

// WaitFrameLatency blocks until the surface's swap chain can accept a new
// frame or the timeout passes.
//
// The native entry hands out a duplicate of the waitable handle, so the
// wait runs outside every renderer lock and cannot race a concurrent
// destroy or device rebuild (those close the surface's own handle, never
// the duplicate, which this call closes).
func (s *RendererService) WaitFrameLatency(surface SurfaceHandle, timeout time.Duration) (FrameLatencyWait, error) {
	if s == nil {
		return FrameLatencyUnavailable, fmt.Errorf("native: no renderer service")
	}
	if s.lib.released.Load() {
		return FrameLatencyUnavailable, ErrClosed
	}
	handle, code, err := callRendererSurfaceLatencyHandle(s.table.surfaceLatencyHandle, uint64(surface))
	if err != nil {
		return FrameLatencyUnavailable, err
	}
	if code != rendererStatusOK {
		return FrameLatencyUnavailable, rendererStatusError(code, "renderer_surface_frame_latency_handle")
	}
	if handle == 0 {
		return FrameLatencyUnavailable, nil
	}
	signaled, err := waitAndCloseHandle(handle, timeout)
	if err != nil {
		return FrameLatencyUnavailable, err
	}
	if !signaled {
		return FrameLatencyTimeout, nil
	}
	return FrameLatencySignaled, nil
}
