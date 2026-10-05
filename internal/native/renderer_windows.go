//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Renderer service FFI (Windows). Every call goes through the service
// table's function slots with syscall.SyscallN; the pointer-to-uintptr
// conversions sit inside the SyscallN call expressions (the
// documented-safe pattern, mirroring layout_windows.go) and
// runtime.KeepAlive covers every Go-owned record and buffer until the
// foreign call has returned. Status values are reinterpreted through the
// low 32 bits: the native entries return i32 in EAX.

func callRendererSurfaceCreate(
	fn uintptr,
	hwnd uint64,
	mode uint32,
	width, height uint32,
	scaleBits uint32,
	out *uint64,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_create")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(hwnd),
		uintptr(mode),
		uintptr(width),
		uintptr(height),
		uintptr(scaleBits),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callRendererSurfaceResize(fn uintptr, surface uint64, width, height uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_resize")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(surface),
		uintptr(width),
		uintptr(height),
	)
	return callStatus(r1), nil
}

func callRendererSurfaceInfo(fn uintptr, surface uint64, out *SurfaceInfo) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_info")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(surface),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callRendererSurfacePresent(
	fn uintptr,
	surface uint64,
	bits *[4]uint32,
	out *uint64,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_present")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(surface),
		uintptr(unsafe.Pointer(bits)),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(bits)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callRendererSurfaceDestroy(fn uintptr, surface uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_destroy")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(surface))
	return callStatus(r1), nil
}

func callRendererSubmissionPoll(fn uintptr, submission uint64, out *SubmissionStateRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_submission_poll")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(submission),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callRendererSubmissionRetire(fn uintptr, submission uint64, out *RetireRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_submission_retire")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(submission),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callRendererDeviceInfo(
	fn uintptr,
	out *DeviceInfoRecord,
	buf []byte,
	outNeeded *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_device_info")
	}
	var bufPtr uintptr
	if len(buf) > 0 {
		bufPtr = uintptr(unsafe.Pointer(&buf[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(out)),
		bufPtr,
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(outNeeded)),
	)
	runtime.KeepAlive(out)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}

// callRendererSurfaceDrawScene covers the scene draw slot (ticket16):
// render + present + the tracked submission.
func callRendererSurfaceDrawScene(
	fn uintptr,
	surface uint64,
	scene uint64,
	appearance uint32,
	atlas uint64,
	outSubmission *uint64,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_draw_scene")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(surface),
		uintptr(scene),
		uintptr(appearance),
		uintptr(atlas),
		uintptr(unsafe.Pointer(outSubmission)),
	)
	runtime.KeepAlive(outSubmission)
	return callStatus(r1), nil
}

// callRendererSurfaceRenderScene covers the render-only slot
// (ticket16): render without presenting (the readback test path).
func callRendererSurfaceRenderScene(
	fn uintptr,
	surface uint64,
	scene uint64,
	appearance uint32,
	atlas uint64,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_render_scene")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(surface),
		uintptr(scene),
		uintptr(appearance),
		uintptr(atlas),
	)
	return callStatus(r1), nil
}

// callRendererSurfaceReadPixels covers the staging readback slot
// (ticket16): BGRA->RGBA-swapped bytes of the surface's back buffer.
func callRendererSurfaceReadPixels(
	fn uintptr,
	surface uint64,
	buf []byte,
	outNeeded *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_surface_read_pixels")
	}
	var bufPtr uintptr
	if len(buf) > 0 {
		bufPtr = uintptr(unsafe.Pointer(&buf[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(surface),
		bufPtr,
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(outNeeded)),
	)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}
