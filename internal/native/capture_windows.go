//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Capture service FFI (Windows). Same conventions as the other
// service FFI files.

func callCaptureSupported(fn uintptr) (uint32, int32, error) {
	if fn == 0 {
		return 0, 0, nullSlot("capture_supported")
	}
	var supported uint32
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(&supported)),
	)
	runtime.KeepAlive(&supported)
	return supported, callStatus(r1), nil
}

func callCaptureSourcesEnumerate(
	fn uintptr,
	records []CaptureSourceRecord,
	stringsBuf []byte,
) (count uint32, stringsNeeded uint32, code int32, err error) {
	if fn == 0 {
		return 0, 0, 0, nullSlot("capture_sources_enumerate")
	}
	var recordsPtr *CaptureSourceRecord
	if len(records) > 0 {
		recordsPtr = &records[0]
	}
	var stringsPtr *byte
	if len(stringsBuf) > 0 {
		stringsPtr = &stringsBuf[0]
	}
	var outCount, outNeeded uint32
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(recordsPtr)),
		uintptr(len(records)),
		uintptr(unsafe.Pointer(stringsPtr)),
		uintptr(len(stringsBuf)),
		uintptr(unsafe.Pointer(&outCount)),
		uintptr(unsafe.Pointer(&outNeeded)),
	)
	runtime.KeepAlive(&outCount)
	runtime.KeepAlive(&outNeeded)
	runtime.KeepAlive(records)
	runtime.KeepAlive(stringsBuf)
	return outCount, outNeeded, callStatus(r1), nil
}

func callCaptureStreamCreate(
	fn uintptr,
	hmonitor uint64,
) (handle CaptureStreamHandle, width int32, height int32, code int32, err error) {
	if fn == 0 {
		return 0, 0, 0, 0, nullSlot("capture_stream_create")
	}
	var outStream uint64
	var outWidth, outHeight int32
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(hmonitor),
		uintptr(unsafe.Pointer(&outStream)),
		uintptr(unsafe.Pointer(&outWidth)),
		uintptr(unsafe.Pointer(&outHeight)),
	)
	runtime.KeepAlive(&outStream)
	runtime.KeepAlive(&outWidth)
	runtime.KeepAlive(&outHeight)
	return CaptureStreamHandle(outStream), outWidth, outHeight, callStatus(r1), nil
}

func callCaptureStreamPoll(
	fn uintptr,
	stream CaptureStreamHandle,
	frames []CaptureFrameRecord,
) (count uint32, code int32, err error) {
	if fn == 0 {
		return 0, 0, nullSlot("capture_stream_poll")
	}
	var framesPtr *CaptureFrameRecord
	if len(frames) > 0 {
		framesPtr = &frames[0]
	}
	var outCount uint32
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(stream),
		uintptr(unsafe.Pointer(framesPtr)),
		uintptr(len(frames)),
		uintptr(unsafe.Pointer(&outCount)),
	)
	runtime.KeepAlive(&outCount)
	runtime.KeepAlive(frames)
	return outCount, callStatus(r1), nil
}

func callCaptureStreamStop(fn uintptr, stream CaptureStreamHandle) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("capture_stream_stop")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(stream))
	return callStatus(r1), nil
}

func callCaptureStreamDestroy(fn uintptr, stream CaptureStreamHandle) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("capture_stream_destroy")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(stream))
	return callStatus(r1), nil
}

func callCaptureStreamStats(fn uintptr, stream CaptureStreamHandle, out *CaptureStreamStats) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("capture_stream_stats")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(stream),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callCaptureFrameRelease(fn uintptr, frame CaptureFrameHandle) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("capture_frame_release")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(frame))
	return callStatus(r1), nil
}
