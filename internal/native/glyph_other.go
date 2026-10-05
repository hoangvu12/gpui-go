//go:build !windows

package native

// Glyph service FFI stubs for non-Windows hosts. The Windows AMD64
// artifact cannot load there (Load fails with ErrUnsupportedTarget), so
// every glyph call helper surfaces the same typed error instead of a
// build break; the package still compiles everywhere for module tooling
// (mirrors text_other.go).

func callGlyphPrepareStyle(
	fn uintptr,
	colorRBits, colorGBits, colorBBits, colorABits, mode uint32,
	out *RasterStyleRecord,
) (int32, error) {
	return 0, hostSupported()
}

func callGlyphForChar(fn uintptr, fontID uint64, charCode uint32, outGlyphID *uint32) (int32, error) {
	return 0, hostSupported()
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
	return 0, hostSupported()
}

func callGlyphRecommendedMode(fn uintptr, outMode *uint32) (int32, error) {
	return 0, hostSupported()
}

func callGlyphBackendInfo(fn uintptr, out *RasterBackendRecord) (int32, error) {
	return 0, hostSupported()
}

func callGlyphPanicProbe(fn uintptr) (int32, error) {
	return 0, hostSupported()
}
