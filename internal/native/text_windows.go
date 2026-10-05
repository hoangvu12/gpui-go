//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Text service FFI (Windows). Every call goes through the service
// table's function slots with syscall.SyscallN; the pointer-to-uintptr
// conversions sit inside the SyscallN call expressions (the
// documented-safe pattern, mirroring layout_windows.go) and
// runtime.KeepAlive covers every Go-owned record and buffer until the
// foreign call has returned. Status values are reinterpreted through the
// low 32 bits: the native entries return i32 in EAX.

func callTextFontNames(
	fn uintptr,
	buf []byte,
	capacity uint32,
	outCount *uint32,
	outNeeded *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_font_names")
	}
	var bufPtr uintptr
	if len(buf) > 0 {
		bufPtr = uintptr(unsafe.Pointer(&buf[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		bufPtr,
		uintptr(capacity),
		uintptr(unsafe.Pointer(outCount)),
		uintptr(unsafe.Pointer(outNeeded)),
	)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(outCount)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}

func callTextFontResolve(
	fn uintptr,
	request *textFontRequest,
	family []byte,
	features []TextFeatureRecord,
	fallbacks []byte,
	out *FontRecord,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_font_resolve")
	}
	var familyPtr uintptr
	if len(family) > 0 {
		familyPtr = uintptr(unsafe.Pointer(&family[0]))
	}
	var featurePtr uintptr
	if len(features) > 0 {
		featurePtr = uintptr(unsafe.Pointer(&features[0]))
	}
	var fallbackPtr uintptr
	if len(fallbacks) > 0 {
		fallbackPtr = uintptr(unsafe.Pointer(&fallbacks[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(request)),
		familyPtr,
		uintptr(len(family)),
		featurePtr,
		uintptr(len(features)),
		fallbackPtr,
		uintptr(len(fallbacks)),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(request)
	runtime.KeepAlive(family)
	runtime.KeepAlive(features)
	runtime.KeepAlive(fallbacks)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callTextFontMetrics(fn uintptr, fontID uint64, out *FontMetricsRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_font_metrics")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(fontID),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callTextShape(
	fn uintptr,
	text []byte,
	runs []TextRunRecord,
	features []TextFeatureRecord,
	strings []byte,
	fontSizeBits uint32,
	wrapBits, wrapPresent uint32,
	lineClamp, clampPresent uint32,
	outHandle *uint64,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_shape")
	}
	var textPtr uintptr
	if len(text) > 0 {
		textPtr = uintptr(unsafe.Pointer(&text[0]))
	}
	var runPtr uintptr
	if len(runs) > 0 {
		runPtr = uintptr(unsafe.Pointer(&runs[0]))
	}
	var featurePtr uintptr
	if len(features) > 0 {
		featurePtr = uintptr(unsafe.Pointer(&features[0]))
	}
	var stringsPtr uintptr
	if len(strings) > 0 {
		stringsPtr = uintptr(unsafe.Pointer(&strings[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		textPtr,
		uintptr(len(text)),
		runPtr,
		uintptr(len(runs)),
		featurePtr,
		uintptr(len(features)),
		stringsPtr,
		uintptr(len(strings)),
		uintptr(fontSizeBits),
		uintptr(wrapBits),
		uintptr(wrapPresent),
		uintptr(lineClamp),
		uintptr(clampPresent),
		uintptr(unsafe.Pointer(outHandle)),
	)
	runtime.KeepAlive(text)
	runtime.KeepAlive(runs)
	runtime.KeepAlive(features)
	runtime.KeepAlive(strings)
	runtime.KeepAlive(outHandle)
	return callStatus(r1), nil
}

func callTextLayout(
	fn uintptr,
	handle uint64,
	wrapBits, wrapPresent uint32,
	lineClamp, clampPresent uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_layout")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(wrapBits),
		uintptr(wrapPresent),
		uintptr(lineClamp),
		uintptr(clampPresent),
	)
	return callStatus(r1), nil
}

func callTextDispose(fn uintptr, handle uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_dispose")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(handle))
	return callStatus(r1), nil
}

func callTextLayoutInfo(fn uintptr, handle uint64, out *TextLayoutRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_layout_info")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callTextLineCount(fn uintptr, handle uint64, outCount *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_line_count")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(unsafe.Pointer(outCount)),
	)
	runtime.KeepAlive(outCount)
	return callStatus(r1), nil
}

func callTextLine(fn uintptr, handle uint64, index, lineHeightBits uint32, out *TextLineRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_line")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(index),
		uintptr(lineHeightBits),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callTextCaret(
	fn uintptr,
	handle uint64,
	index, affinity, lineHeightBits uint32,
	out *TextCaretRecord,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_caret")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(index),
		uintptr(affinity),
		uintptr(lineHeightBits),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callTextHitTest(
	fn uintptr,
	handle uint64,
	xBits, yBits, lineHeightBits uint32,
	out *TextHitRecord,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_hit_test")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(xBits),
		uintptr(yBits),
		uintptr(lineHeightBits),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callTextSelectionRects(
	fn uintptr,
	handle uint64,
	start, end, lineHeightBits uint32,
	rects []TextSelectionRecord,
	capacity uint32,
	outNeeded *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_selection_rects")
	}
	var rectPtr uintptr
	if len(rects) > 0 {
		rectPtr = uintptr(unsafe.Pointer(&rects[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(start),
		uintptr(end),
		uintptr(lineHeightBits),
		rectPtr,
		uintptr(capacity),
		uintptr(unsafe.Pointer(outNeeded)),
	)
	runtime.KeepAlive(rects)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}

func callTextCluster(
	fn uintptr,
	handle uint64,
	index, affinity, side uint32,
	out *TextClusterRecord,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_cluster")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		uintptr(index),
		uintptr(affinity),
		uintptr(side),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callTextFragments(
	fn uintptr,
	handle uint64,
	records []TextFragmentRecord,
	capacity uint32,
	outNeeded *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_fragments")
	}
	var recordPtr uintptr
	if len(records) > 0 {
		recordPtr = uintptr(unsafe.Pointer(&records[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		recordPtr,
		uintptr(capacity),
		uintptr(unsafe.Pointer(outNeeded)),
	)
	runtime.KeepAlive(records)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}

func callTextGlyphs(
	fn uintptr,
	handle uint64,
	records []TextGlyphRecord,
	capacity uint32,
	outNeeded *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("text_glyphs")
	}
	var recordPtr uintptr
	if len(records) > 0 {
		recordPtr = uintptr(unsafe.Pointer(&records[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(handle),
		recordPtr,
		uintptr(capacity),
		uintptr(unsafe.Pointer(outNeeded)),
	)
	runtime.KeepAlive(records)
	runtime.KeepAlive(outNeeded)
	return callStatus(r1), nil
}
