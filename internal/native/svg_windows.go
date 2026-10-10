//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// SVG service FFI (Windows). Every call goes through the service
// table's function slots with syscall.SyscallN (the established
// pattern of image_windows.go); runtime.KeepAlive covers every
// Go-owned record and buffer until the foreign call has returned.
// Status values are reinterpreted through the low 32 bits: the native
// entries return i32 in EAX.

func callSvgFontAssetCount(fn uintptr) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("svg_font_asset_count")
	}
	r1, _, _ := syscall.SyscallN(fn)
	return callStatus(r1), nil
}

func callSvgFontAssetPath(fn uintptr, index uint32) (string, error) {
	if fn == 0 {
		return "", nullSlot("svg_font_asset_path")
	}
	var ptr *byte
	var length uint32
	r1, _, _ := syscall.SyscallN(fn,
		uintptr(index), uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&length)))
	runtime.KeepAlive(&ptr)
	runtime.KeepAlive(&length)
	if code := callStatus(r1); code != svgStatusOK {
		return "", svgError(code)
	}
	if ptr == nil && length != 0 {
		return "", svgError(svgStatusBadValue)
	}
	if length == 0 {
		return "", nil
	}
	// The native static lives for the process lifetime; copy it out.
	return unsafe.String(ptr, length), nil
}

func callSvgHasFontAsset(fn uintptr, path string) (bool, error) {
	if fn == 0 {
		return false, nullSlot("svg_has_font_asset")
	}
	var bytePtr *byte
	if len(path) > 0 {
		bytePtr = &[]byte(path)[0]
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(unsafe.Pointer(bytePtr)), uintptr(len(path)))
	runtime.KeepAlive(path)
	code := callStatus(r1)
	if code < 0 {
		return false, svgError(code)
	}
	return code == 1, nil
}

func callSvgAddFont(fn uintptr, path string, bytes []byte) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("svg_add_font")
	}
	var pathPtr *byte
	if len(path) > 0 {
		pathPtr = &[]byte(path)[0]
	}
	var bytesPtr *byte
	if len(bytes) > 0 {
		bytesPtr = &bytes[0]
	}
	r1, _, _ := syscall.SyscallN(fn,
		uintptr(unsafe.Pointer(pathPtr)), uintptr(len(path)),
		uintptr(unsafe.Pointer(bytesPtr)), uintptr(len(bytes)))
	runtime.KeepAlive(path)
	runtime.KeepAlive(bytes)
	return callStatus(r1), nil
}

func callSvgParse(fn uintptr, bytes []byte) (SvgHandle, int32, error) {
	if fn == 0 {
		return 0, 0, nullSlot("svg_parse")
	}
	var bytesPtr *byte
	if len(bytes) > 0 {
		bytesPtr = &bytes[0]
	}
	var handle uint64
	r1, _, _ := syscall.SyscallN(fn,
		uintptr(unsafe.Pointer(bytesPtr)), uintptr(len(bytes)), uintptr(unsafe.Pointer(&handle)))
	runtime.KeepAlive(bytes)
	runtime.KeepAlive(&handle)
	return SvgHandle(handle), callStatus(r1), nil
}

func callSvgDispose(fn uintptr, handle SvgHandle) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("svg_dispose")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle))
	return callStatus(r1), nil
}

// renderSvg runs the render entry with the capacity protocol: the
// first call with a nil buffer queries the required size, the second
// passes it.
func renderSvg(fn uintptr, handle SvgHandle, mode uint32, width, height uint32, scale float32) (pixels []byte, info SvgRenderInfo, err error) {
	if fn == 0 {
		return nil, info, nullSlot("svg_render")
	}
	var infoRecord SvgRenderInfo
	var needed uint32
	r1, _, _ := syscall.SyscallN(fn,
		uintptr(handle), uintptr(mode), uintptr(width), uintptr(height),
		uintptr(mathFloat32bits(scale)), 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&infoRecord)))
	runtime.KeepAlive(&needed)
	runtime.KeepAlive(&infoRecord)
	code := callStatus(r1)
	if code != svgStatusOK && code != svgStatusCapacity {
		return nil, info, svgError(code)
	}
	if code == svgStatusCapacity {
		if needed == 0 {
			return nil, info, svgError(svgStatusBadValue)
		}
	}
	// Second call with the exact capacity.
	buf := make([]byte, needed)
	var needed2 uint32
	var info2 SvgRenderInfo
	r1, _, _ = syscall.SyscallN(fn,
		uintptr(handle), uintptr(mode), uintptr(width), uintptr(height),
		uintptr(mathFloat32bits(scale)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&needed2)), uintptr(unsafe.Pointer(&info2)))
	runtime.KeepAlive(buf)
	runtime.KeepAlive(&needed2)
	runtime.KeepAlive(&info2)
	code = callStatus(r1)
	if code != svgStatusOK {
		return nil, info2, svgError(code)
	}
	return buf, info2, nil
}

// renderSvgAlphaMask runs the alpha-mask entry with the same capacity
// protocol.
func renderSvgAlphaMask(fn uintptr, bytes []byte, width, height uint32) (mask []byte, info SvgRenderInfo, err error) {
	if fn == 0 {
		return nil, info, nullSlot("svg_render_alpha_mask")
	}
	var bytesPtr *byte
	if len(bytes) > 0 {
		bytesPtr = &bytes[0]
	}
	var infoRecord SvgRenderInfo
	var needed uint32
	r1, _, _ := syscall.SyscallN(fn,
		uintptr(unsafe.Pointer(bytesPtr)), uintptr(len(bytes)),
		uintptr(width), uintptr(height), 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&infoRecord)))
	runtime.KeepAlive(bytes)
	runtime.KeepAlive(&needed)
	runtime.KeepAlive(&infoRecord)
	code := callStatus(r1)
	if code != svgStatusOK && code != svgStatusCapacity {
		return nil, info, svgError(code)
	}
	if code == svgStatusCapacity && needed == 0 {
		return nil, info, svgError(svgStatusBadValue)
	}
	buf := make([]byte, needed)
	var needed2 uint32
	var info2 SvgRenderInfo
	r1, _, _ = syscall.SyscallN(fn,
		uintptr(unsafe.Pointer(bytesPtr)), uintptr(len(bytes)),
		uintptr(width), uintptr(height),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&needed2)), uintptr(unsafe.Pointer(&info2)))
	runtime.KeepAlive(bytes)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(&needed2)
	runtime.KeepAlive(&info2)
	code = callStatus(r1)
	if code != svgStatusOK {
		return nil, info2, svgError(code)
	}
	return buf, info2, nil
}

// mathFloat32bits reinterprets a float32 as its IEEE 754 bits (the
// ABI passes the scale factor by value in a register slot).
func mathFloat32bits(f float32) uint32 {
	return *(*uint32)(unsafe.Pointer(&f))
}
