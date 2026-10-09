//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Image service FFI (Windows). Every call goes through the service
// table's function slots with syscall.SyscallN (the established
// pattern of text_windows.go/atlas_windows.go); runtime.KeepAlive
// covers every Go-owned record and buffer until the foreign call has
// returned. Status values are reinterpreted through the low 32 bits:
// the native entries return i32 in EAX.

func callImageDecodeResource(fn uintptr, bytes *byte, length uint32, outHandle *uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_decode_resource")
	}
	var bytePtr uintptr
	if bytes != nil {
		bytePtr = uintptr(unsafe.Pointer(bytes))
	}
	r1, _, _ := syscall.SyscallN(fn, bytePtr, uintptr(length), uintptr(unsafe.Pointer(outHandle)))
	runtime.KeepAlive(bytes)
	runtime.KeepAlive(outHandle)
	return callStatus(r1), nil
}

func callImageDecodeClipboard(fn uintptr, bytes *byte, length uint32, format uint32, outHandle *uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_decode_clipboard")
	}
	var bytePtr uintptr
	if bytes != nil {
		bytePtr = uintptr(unsafe.Pointer(bytes))
	}
	r1, _, _ := syscall.SyscallN(fn, bytePtr, uintptr(length), uintptr(format), uintptr(unsafe.Pointer(outHandle)))
	runtime.KeepAlive(bytes)
	runtime.KeepAlive(outHandle)
	return callStatus(r1), nil
}

func callImageFormatProbe(fn uintptr, bytes *byte, length uint32, outFormat *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_format_probe")
	}
	var bytePtr uintptr
	if bytes != nil {
		bytePtr = uintptr(unsafe.Pointer(bytes))
	}
	r1, _, _ := syscall.SyscallN(fn, bytePtr, uintptr(length), uintptr(unsafe.Pointer(outFormat)))
	runtime.KeepAlive(bytes)
	runtime.KeepAlive(outFormat)
	return callStatus(r1), nil
}

func callImageFrameCount(fn uintptr, handle uint64, outCount *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_frame_count")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle), uintptr(unsafe.Pointer(outCount)))
	runtime.KeepAlive(outCount)
	return callStatus(r1), nil
}

func callImageFrameInfo(fn uintptr, handle uint64, frameIndex uint32, out *imageFrameRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_frame_info")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle), uintptr(frameIndex), uintptr(unsafe.Pointer(out)))
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callImageFramePixels(fn uintptr, handle uint64, frameIndex uint32, buf *byte, capacity uint32, outNeeded *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_frame_pixels")
	}
	var bufPtr uintptr
	if buf != nil {
		bufPtr = uintptr(unsafe.Pointer(buf))
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle), uintptr(frameIndex), bufPtr, uintptr(capacity), uintptr(unsafe.Pointer(outNeeded)))
	runtime.KeepAlive(buf)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}

func callImageDispose(fn uintptr, handle uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_dispose")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle))
	return callStatus(r1), nil
}

func callImageCodecGraph(fn uintptr, records *imageCodecRecord, capacity uint32, outCount *uint32, outNeeded *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("image_codec_graph")
	}
	var recordPtr uintptr
	if records != nil {
		recordPtr = uintptr(unsafe.Pointer(records))
	}
	r1, _, _ := syscall.SyscallN(fn, recordPtr, uintptr(capacity), uintptr(unsafe.Pointer(outCount)), uintptr(unsafe.Pointer(outNeeded)))
	runtime.KeepAlive(records)
	runtime.KeepAlive(outCount)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}
