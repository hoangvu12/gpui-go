//go:build !windows

package native

// Text service FFI stubs for non-Windows hosts. The Windows AMD64
// artifact cannot load there (Load fails with ErrUnsupportedTarget), so
// every text call helper surfaces the same typed error instead of a
// build break; the package still compiles everywhere for module tooling
// (mirrors scene_other.go).

func callTextFontNames(
	fn uintptr,
	buf []byte,
	capacity uint32,
	outCount *uint32,
	outNeeded *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callTextFontResolve(
	fn uintptr,
	request *textFontRequest,
	family []byte,
	features []TextFeatureRecord,
	fallbacks []byte,
	out *FontRecord,
) (int32, error) {
	return 0, hostSupported()
}

func callTextFontMetrics(fn uintptr, fontID uint64, out *FontMetricsRecord) (int32, error) {
	return 0, hostSupported()
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
	return 0, hostSupported()
}

func callTextLayout(
	fn uintptr,
	handle uint64,
	wrapBits, wrapPresent uint32,
	lineClamp, clampPresent uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callTextDispose(fn uintptr, handle uint64) (int32, error) {
	return 0, hostSupported()
}

func callTextLayoutInfo(fn uintptr, handle uint64, out *TextLayoutRecord) (int32, error) {
	return 0, hostSupported()
}

func callTextLineCount(fn uintptr, handle uint64, outCount *uint32) (int32, error) {
	return 0, hostSupported()
}

func callTextLine(fn uintptr, handle uint64, index, lineHeightBits uint32, out *TextLineRecord) (int32, error) {
	return 0, hostSupported()
}

func callTextCaret(
	fn uintptr,
	handle uint64,
	index, affinity, lineHeightBits uint32,
	out *TextCaretRecord,
) (int32, error) {
	return 0, hostSupported()
}

func callTextHitTest(
	fn uintptr,
	handle uint64,
	xBits, yBits, lineHeightBits uint32,
	out *TextHitRecord,
) (int32, error) {
	return 0, hostSupported()
}

func callTextSelectionRects(
	fn uintptr,
	handle uint64,
	start, end, lineHeightBits uint32,
	rects []TextSelectionRecord,
	capacity uint32,
	outNeeded *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callTextCluster(
	fn uintptr,
	handle uint64,
	index, affinity, side uint32,
	out *TextClusterRecord,
) (int32, error) {
	return 0, hostSupported()
}

func callTextFragments(
	fn uintptr,
	handle uint64,
	records []TextFragmentRecord,
	capacity uint32,
	outNeeded *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callTextGlyphs(
	fn uintptr,
	handle uint64,
	records []TextGlyphRecord,
	capacity uint32,
	outNeeded *uint32,
) (int32, error) {
	return 0, hostSupported()
}
