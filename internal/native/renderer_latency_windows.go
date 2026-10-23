//go:build windows

package native

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

func callRendererSurfaceLatencyHandle(fn uintptr, surface uint64) (handle uint64, code int32, err error) {
	if fn == 0 {
		return 0, 0, nullSlot("renderer_surface_frame_latency_handle")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(surface), uintptr(unsafe.Pointer(&handle)))
	runtime.KeepAlive(&handle)
	return handle, callStatus(r1), nil
}

// waitAndCloseHandle waits on a caller-owned kernel handle, then closes it.
func waitAndCloseHandle(handle uint64, timeout time.Duration) (bool, error) {
	h := syscall.Handle(handle)
	defer syscall.CloseHandle(h)
	ms := uint32(timeout / time.Millisecond)
	event, err := syscall.WaitForSingleObject(h, ms)
	if err != nil {
		return false, err
	}
	return event == syscall.WAIT_OBJECT_0, nil
}
