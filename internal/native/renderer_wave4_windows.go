//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// The wave-4 renderer FFI additions (tickets 29/32): the device-loss
// state check, the test-only loss injection, the bounded-retry device
// rebuild, the recovery-gate clear (mark_drawable) and the pinned
// test-support render-to-image path. Same conventions as the entries
// above: SyscallN through the table slots, KeepAlive on every Go-owned
// record and buffer.

func callRendererDeviceStateCheck(fn uintptr, out *DeviceStateRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_device_state_check")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callRendererDeviceInjectLoss(fn uintptr) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_device_inject_loss")
	}
	r1, _, _ := syscall.SyscallN(fn)
	return callStatus(r1), nil
}

func callRendererDeviceRebuild(fn uintptr, out *RebuildRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_device_rebuild")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callRendererDeviceMarkDrawable(fn uintptr) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("renderer_device_mark_drawable")
	}
	r1, _, _ := syscall.SyscallN(fn)
	return callStatus(r1), nil
}

func callRendererSurfaceRenderToImage(
	fn uintptr,
	surface uint64,
	scene uint64,
	appearance uint32,
	atlas uint64,
	outBytes []byte,
) (needed uint32, code int32, err error) {
	if fn == 0 {
		return 0, 0, nullSlot("renderer_surface_render_to_image")
	}
	var outNeeded uint32
	var ptr, capArg uintptr
	if len(outBytes) > 0 {
		ptr = uintptr(unsafe.Pointer(&outBytes[0]))
		capArg = uintptr(len(outBytes))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(surface),
		uintptr(scene),
		uintptr(appearance),
		uintptr(atlas),
		ptr,
		capArg,
		uintptr(unsafe.Pointer(&outNeeded)),
	)
	runtime.KeepAlive(&outNeeded)
	runtime.KeepAlive(outBytes)
	return outNeeded, callStatus(r1), nil
}
