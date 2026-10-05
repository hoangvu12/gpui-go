//go:build !windows

package native

// Atlas service FFI stubs for non-Windows hosts. The Windows AMD64
// artifact cannot load there (Load fails with ErrUnsupportedTarget), so
// every atlas call helper surfaces the same typed error instead of a
// build break; the package still compiles everywhere for module tooling
// (mirrors text_other.go).

func callAtlasCreate(fn uintptr, outHandle *uint64) (int32, error) {
	return 0, hostSupported()
}

func callAtlasDispose(fn uintptr, handle uint64) (int32, error) {
	return 0, hostSupported()
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
	return 0, hostSupported()
}

func callAtlasRemove(fn uintptr, handle uint64, key *AtlasKeyRecord) (int32, error) {
	return 0, hostSupported()
}

func callAtlasQuery(fn uintptr, handle uint64, key *AtlasKeyRecord, outTile *AtlasTileRecord) (int32, error) {
	return 0, hostSupported()
}

func callAtlasTextureCount(fn uintptr, handle uint64, kind uint32, outCount *uint32) (int32, error) {
	return 0, hostSupported()
}

func callAtlasTileCount(fn uintptr, handle uint64, outCount *uint32) (int32, error) {
	return 0, hostSupported()
}

func callAtlasGeneration(fn uintptr, handle uint64, outGeneration *uint32) (int32, error) {
	return 0, hostSupported()
}

func callAtlasNotifyDeviceLost(fn uintptr, handle uint64) (int32, error) {
	return 0, hostSupported()
}

func callAtlasPanicProbe(fn uintptr) (int32, error) {
	return 0, hostSupported()
}
