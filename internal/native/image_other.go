//go:build !windows

package native

// Image service FFI stubs for non-Windows hosts. The Windows AMD64
// artifact cannot load there (Load fails with ErrUnsupportedTarget),
// so every image call helper surfaces the same typed error instead of
// a build break; the package still compiles everywhere for module
// tooling (mirrors text_other.go/atlas_other.go).

func callImageDecodeResource(fn uintptr, bytes *byte, length uint32, outHandle *uint64) (int32, error) {
	return 0, hostSupported()
}

func callImageDecodeClipboard(fn uintptr, bytes *byte, length uint32, format uint32, outHandle *uint64) (int32, error) {
	return 0, hostSupported()
}

func callImageFormatProbe(fn uintptr, bytes *byte, length uint32, outFormat *uint32) (int32, error) {
	return 0, hostSupported()
}

func callImageFrameCount(fn uintptr, handle uint64, outCount *uint32) (int32, error) {
	return 0, hostSupported()
}

func callImageFrameInfo(fn uintptr, handle uint64, frameIndex uint32, out *imageFrameRecord) (int32, error) {
	return 0, hostSupported()
}

func callImageFramePixels(fn uintptr, handle uint64, frameIndex uint32, buf *byte, capacity uint32, outNeeded *uint32) (int32, error) {
	return 0, hostSupported()
}

func callImageDispose(fn uintptr, handle uint64) (int32, error) {
	return 0, hostSupported()
}

func callImageCodecGraph(fn uintptr, records *imageCodecRecord, capacity uint32, outCount *uint32, outNeeded *uint32) (int32, error) {
	return 0, hostSupported()
}
