//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Glyph service FFI (Windows). Every call goes through the service
// table's function slots with syscall.SyscallN (the established pattern
// of text_windows.go); runtime.KeepAlive covers every Go-owned record
// and buffer until the foreign call has returned. Status values are
// reinterpreted through the low 32 bits: the native entries return i32
// in EAX.

func callGlyphPrepareStyle(
	fn uintptr,
	colorRBits, colorGBits, colorBBits, colorABits, mode uint32,
	out *RasterStyleRecord,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("glyph_prepare_style")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(colorRBits),
		uintptr(colorGBits),
		uintptr(colorBBits),
		uintptr(colorABits),
		uintptr(mode),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callGlyphForChar(fn uintptr, fontID uint64, charCode uint32, outGlyphID *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("glyph_for_char")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(fontID),
		uintptr(charCode),
		uintptr(unsafe.Pointer(outGlyphID)),
	)
	runtime.KeepAlive(outGlyphID)
	return callStatus(r1), nil
}

func callGlyphRasterize(
	fn uintptr,
	fontID uint64,
	glyphID uint32,
	fontSizeBits, subpixelX, subpixelY, scaleBits uint32,
	style *RasterStyleRecord,
	out *RasterRecord,
	pixels []byte,
	pixelCapacity uint32,
	outNeeded *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("glyph_rasterize")
	}
	var pixelPtr uintptr
	if len(pixels) > 0 {
		pixelPtr = uintptr(unsafe.Pointer(&pixels[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(fontID),
		uintptr(glyphID),
		uintptr(fontSizeBits),
		uintptr(subpixelX),
		uintptr(subpixelY),
		uintptr(scaleBits),
		uintptr(unsafe.Pointer(style)),
		uintptr(unsafe.Pointer(out)),
		pixelPtr,
		uintptr(pixelCapacity),
		uintptr(unsafe.Pointer(outNeeded)),
	)
	runtime.KeepAlive(style)
	runtime.KeepAlive(out)
	runtime.KeepAlive(pixels)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}

func callGlyphRecommendedMode(fn uintptr, outMode *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("glyph_recommended_mode")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(outMode)),
	)
	runtime.KeepAlive(outMode)
	return callStatus(r1), nil
}

func callGlyphBackendInfo(fn uintptr, out *RasterBackendRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("glyph_backend_info")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callGlyphPanicProbe(fn uintptr) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("glyph_panic_probe")
	}
	r1, _, _ := syscall.SyscallN(fn)
	return callStatus(r1), nil
}
