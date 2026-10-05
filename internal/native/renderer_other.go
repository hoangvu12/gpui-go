//go:build !windows

package native

// Renderer service FFI stubs for non-Windows hosts. The Windows AMD64
// artifact cannot load there (Load fails with ErrUnsupportedTarget), so
// every renderer call helper surfaces the same typed error instead of a
// build break; the package still compiles everywhere for module tooling
// (mirrors layout_other.go).

func callRendererSurfaceCreate(
	fn uintptr,
	hwnd uint64,
	mode uint32,
	width, height uint32,
	scaleBits uint32,
	out *uint64,
) (int32, error) {
	return 0, hostSupported()
}

func callRendererSurfaceResize(fn uintptr, surface uint64, width, height uint32) (int32, error) {
	return 0, hostSupported()
}

func callRendererSurfaceInfo(fn uintptr, surface uint64, out *SurfaceInfo) (int32, error) {
	return 0, hostSupported()
}

func callRendererSurfacePresent(
	fn uintptr,
	surface uint64,
	bits *[4]uint32,
	out *uint64,
) (int32, error) {
	return 0, hostSupported()
}

func callRendererSurfaceDestroy(fn uintptr, surface uint64) (int32, error) {
	return 0, hostSupported()
}

func callRendererSubmissionPoll(fn uintptr, submission uint64, out *SubmissionStateRecord) (int32, error) {
	return 0, hostSupported()
}

func callRendererSubmissionRetire(fn uintptr, submission uint64, out *RetireRecord) (int32, error) {
	return 0, hostSupported()
}

func callRendererDeviceInfo(
	fn uintptr,
	out *DeviceInfoRecord,
	buf []byte,
	outNeeded *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callRendererSurfaceDrawScene(
	fn uintptr,
	surface uint64,
	scene uint64,
	appearance uint32,
	atlas uint64,
	outSubmission *uint64,
) (int32, error) {
	return 0, hostSupported()
}

func callRendererSurfaceRenderScene(
	fn uintptr,
	surface uint64,
	scene uint64,
	appearance uint32,
	atlas uint64,
) (int32, error) {
	return 0, hostSupported()
}

func callRendererSurfaceReadPixels(
	fn uintptr,
	surface uint64,
	buf []byte,
	outNeeded *uint32,
) (int32, error) {
	return 0, hostSupported()
}
