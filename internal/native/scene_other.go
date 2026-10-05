//go:build !windows

package native

import "unsafe"

// Scene service FFI stubs for non-Windows hosts. The Windows AMD64
// artifact cannot load there (Load fails with ErrUnsupportedTarget), so
// every scene call helper surfaces the same typed error instead of a
// build break; the package still compiles everywhere for module tooling.

func callSceneCreate(fn uintptr, outHandle *uint64) (int32, error) {
	return 0, hostSupported()
}

func callSceneOp(fn uintptr, scene uint64) (int32, error) {
	return 0, hostSupported()
}

func callScenePushLayer(fn uintptr, scene uint64, bounds *SceneBounds) (int32, error) {
	return 0, hostSupported()
}

func callSceneInsertQuad(fn uintptr, scene uint64, rec *SceneQuadRecord) (int32, error) {
	return 0, hostSupported()
}

func callSceneInsertShadow(fn uintptr, scene uint64, rec *SceneShadowRecord) (int32, error) {
	return 0, hostSupported()
}

func callSceneInsertUnderline(fn uintptr, scene uint64, rec *SceneUnderlineRecord) (int32, error) {
	return 0, hostSupported()
}

func callSceneInsertFilter(fn uintptr, scene uint64, rec *SceneFilterRecord) (int32, error) {
	return 0, hostSupported()
}

func callSceneInsertSurface(fn uintptr, scene uint64, rec *SceneSurfaceRecord, opacityBits uint32) (int32, error) {
	return 0, hostSupported()
}

func callSceneInsertSprite(fn uintptr, scene uint64, rec *SceneSpriteRecord) (int32, error) {
	return 0, hostSupported()
}

func callSceneInsertPolySprite(fn uintptr, scene uint64, rec *ScenePolychromeSpriteRecord) (int32, error) {
	return 0, hostSupported()
}

func callSceneReplay(fn uintptr, scene uint64, start, end uint32, prev uint64) (int32, error) {
	return 0, hostSupported()
}

func callSceneLen(fn uintptr, scene uint64, outLen *uint32) (int32, error) {
	return 0, hostSupported()
}

func callSceneMeta(fn uintptr, scene uint64, outMeta *SceneMetaRecord) (int32, error) {
	return 0, hostSupported()
}

func callSceneDump(
	fn uintptr,
	scene uint64,
	kind uint32,
	records uintptr,
	opacityBits uintptr,
	capacity uint32,
	outCount *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callScenePlanDump(
	fn uintptr,
	scene uint64,
	commands uintptr,
	capacity uint32,
	outCount *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callSceneRequirements(fn uintptr, scene uint64, out *SceneRequirementsRecord) (int32, error) {
	return 0, hostSupported()
}

func callScenePanicProbe(fn uintptr) (int32, error) {
	return 0, hostSupported()
}

// Keep unsafe referenced for parity with the Windows build (the record
// pointers above are typed).
var _ = unsafe.Pointer(nil)

func callSceneInsertPath(fn uintptr, scene uint64, rec *ScenePathRecord, vertices *ScenePathVertexRecord, vertexCount uint32) (int32, error) {
	return 0, hostSupported()
}

func callScenePathDump(
	fn uintptr,
	scene uint64,
	records *ScenePathRecord,
	vertices *ScenePathVertexRecord,
	recordCapacity uint32,
	vertexCapacity uint32,
	outRecordCount *uint32,
	outVertexCount *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callScenePathScript(
	fn uintptr,
	commands *PathCommandRecord,
	commandCount uint32,
	words *uint32,
	wordCount uint32,
	outVertices *ScenePathVertexRecord,
	vertexCapacity uint32,
	outVertexCount *uint32,
	outBounds *SceneBounds,
) (int32, error) {
	return 0, hostSupported()
}
