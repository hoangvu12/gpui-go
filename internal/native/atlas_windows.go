//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Atlas service FFI (Windows). Every call goes through the service
// table's function slots with syscall.SyscallN (the established pattern
// of text_windows.go); runtime.KeepAlive covers every Go-owned record
// and buffer until the foreign call has returned. Status values are
// reinterpreted through the low 32 bits: the native entries return i32
// in EAX.

func callAtlasCreate(fn uintptr, outHandle *uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_create")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(outHandle)),
	)
	runtime.KeepAlive(outHandle)
	return callStatus(r1), nil
}

func callAtlasDispose(fn uintptr, handle uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_dispose")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle))
	return callStatus(r1), nil
}

func callAtlasInsert(
	fn uintptr,
	handle uint64,
	key *AtlasKeyRecord,
	width, height int32,
	bytes []byte,
	byteCount uint32,
	outTile *AtlasTileRecord,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_insert")
	}
	var bytePtr uintptr
	if len(bytes) > 0 {
		bytePtr = uintptr(unsafe.Pointer(&bytes[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(key)),
		uintptr(width),
		uintptr(height),
		bytePtr,
		uintptr(byteCount),
		uintptr(unsafe.Pointer(outTile)),
	)
	runtime.KeepAlive(key)
	runtime.KeepAlive(bytes)
	runtime.KeepAlive(outTile)
	return callStatus(r1), nil
}

func callAtlasRemove(fn uintptr, handle uint64, key *AtlasKeyRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_remove")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(key)),
	)
	runtime.KeepAlive(key)
	return callStatus(r1), nil
}

func callAtlasQuery(fn uintptr, handle uint64, key *AtlasKeyRecord, outTile *AtlasTileRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_query")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(key)),
		uintptr(unsafe.Pointer(outTile)),
	)
	runtime.KeepAlive(key)
	runtime.KeepAlive(outTile)
	return callStatus(r1), nil
}

func callAtlasTextureCount(fn uintptr, handle uint64, kind uint32, outCount *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_texture_count")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(kind),
		uintptr(unsafe.Pointer(outCount)),
	)
	runtime.KeepAlive(outCount)
	return callStatus(r1), nil
}

func callAtlasTileCount(fn uintptr, handle uint64, outCount *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_tile_count")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(outCount)),
	)
	runtime.KeepAlive(outCount)
	return callStatus(r1), nil
}

func callAtlasGeneration(fn uintptr, handle uint64, outGeneration *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_generation")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(outGeneration)),
	)
	runtime.KeepAlive(outGeneration)
	return callStatus(r1), nil
}

func callAtlasNotifyDeviceLost(fn uintptr, handle uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_notify_device_lost")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle))
	return callStatus(r1), nil
}

func callAtlasPanicProbe(fn uintptr) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("atlas_panic_probe")
	}
	r1, _, _ := syscall.SyscallN(fn)
	return callStatus(r1), nil
}
