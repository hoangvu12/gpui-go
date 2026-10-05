//go:build windows

package native

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Scene service FFI (Windows). Every call goes through the service
// table's function slots with syscall.SyscallN; the pointer-to-uintptr
// conversions sit inside the SyscallN call expressions (the
// documented-safe pattern) and runtime.KeepAlive covers every Go-owned
// record until the foreign call has returned. Status values are
// reinterpreted through the low 32 bits: the native entries return i32
// in EAX.

func callSceneCreate(fn uintptr, outHandle *uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_create")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(unsafe.Pointer(outHandle)))
	runtime.KeepAlive(outHandle)
	return callStatus(r1), nil
}

// callSceneOp invokes a (scene) -> i32 entry (clear/dispose/pop-layer/
// raise-order-floor/finish).
func callSceneOp(fn uintptr, scene uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene op")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene))
	return callStatus(r1), nil
}

func callScenePushLayer(fn uintptr, scene uint64, bounds *SceneBounds) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_push_layer")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(bounds)))
	runtime.KeepAlive(bounds)
	return callStatus(r1), nil
}

func callSceneInsertQuad(fn uintptr, scene uint64, rec *SceneQuadRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_quad")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(rec)))
	runtime.KeepAlive(rec)
	return callStatus(r1), nil
}

func callSceneInsertShadow(fn uintptr, scene uint64, rec *SceneShadowRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_shadow")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(rec)))
	runtime.KeepAlive(rec)
	return callStatus(r1), nil
}

func callSceneInsertUnderline(fn uintptr, scene uint64, rec *SceneUnderlineRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_underline")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(rec)))
	runtime.KeepAlive(rec)
	return callStatus(r1), nil
}

// callSceneInsertFilter covers the backdrop-filter and boundary slots
// (same record shape).
func callSceneInsertFilter(fn uintptr, scene uint64, rec *SceneFilterRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_filter")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(rec)))
	runtime.KeepAlive(rec)
	return callStatus(r1), nil
}

func callSceneInsertSurface(fn uintptr, scene uint64, rec *SceneSurfaceRecord, opacityBits uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_surface")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(rec)), uintptr(opacityBits))
	runtime.KeepAlive(rec)
	return callStatus(r1), nil
}

// callSceneInsertSprite covers the monochrome and subpixel sprite slots
// (same record shape).
func callSceneInsertSprite(fn uintptr, scene uint64, rec *SceneSpriteRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_sprite")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(rec)))
	runtime.KeepAlive(rec)
	return callStatus(r1), nil
}

func callSceneInsertPolySprite(fn uintptr, scene uint64, rec *ScenePolychromeSpriteRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_polychrome_sprite")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(rec)))
	runtime.KeepAlive(rec)
	return callStatus(r1), nil
}

func callSceneReplay(fn uintptr, scene uint64, start, end uint32, prev uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_replay")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(start), uintptr(end), uintptr(prev))
	return callStatus(r1), nil
}

func callSceneLen(fn uintptr, scene uint64, outLen *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_len")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(outLen)))
	runtime.KeepAlive(outLen)
	return callStatus(r1), nil
}

func callSceneMeta(fn uintptr, scene uint64, outMeta *SceneMetaRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_meta")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(outMeta)))
	runtime.KeepAlive(outMeta)
	return callStatus(r1), nil
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
	if fn == 0 {
		return 0, nullSlot("scene_dump")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(scene),
		uintptr(kind),
		records,
		opacityBits,
		uintptr(capacity),
		uintptr(unsafe.Pointer(outCount)),
	)
	runtime.KeepAlive(outCount)
	return callStatus(r1), nil
}

func callScenePlanDump(
	fn uintptr,
	scene uint64,
	commands uintptr,
	capacity uint32,
	outCount *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_plan_dump")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(scene),
		commands,
		uintptr(capacity),
		uintptr(unsafe.Pointer(outCount)),
	)
	runtime.KeepAlive(outCount)
	return callStatus(r1), nil
}

func callSceneRequirements(fn uintptr, scene uint64, out *SceneRequirementsRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_requirements")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(scene), uintptr(unsafe.Pointer(out)))
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callScenePanicProbe(fn uintptr) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_panic_probe")
	}
	r1, _, _ := syscall.SyscallN(fn)
	return callStatus(r1), nil
}

// callSceneInsertPath covers the path insert slot (ticket16).
func callSceneInsertPath(fn uintptr, scene uint64, rec *ScenePathRecord, vertices *ScenePathVertexRecord, vertexCount uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("scene_insert_path")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(scene),
		uintptr(unsafe.Pointer(rec)),
		uintptr(unsafe.Pointer(vertices)),
		uintptr(vertexCount),
	)
	runtime.KeepAlive(rec)
	runtime.KeepAlive(vertices)
	return callStatus(r1), nil
}

// callScenePathDump covers the path dump slot (ticket16).
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
	if fn == 0 {
		return 0, nullSlot("scene_path_dump")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(scene),
		uintptr(unsafe.Pointer(records)),
		uintptr(unsafe.Pointer(vertices)),
		uintptr(recordCapacity),
		uintptr(vertexCapacity),
		uintptr(unsafe.Pointer(outRecordCount)),
		uintptr(unsafe.Pointer(outVertexCount)),
	)
	runtime.KeepAlive(records)
	runtime.KeepAlive(vertices)
	runtime.KeepAlive(outRecordCount)
	runtime.KeepAlive(outVertexCount)
	return callStatus(r1), nil
}

// callScenePathScript covers the pinned-PathBuilder tessellation slot
// (ticket16).
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
	if fn == 0 {
		return 0, nullSlot("scene_path_script")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(unsafe.Pointer(commands)),
		uintptr(commandCount),
		uintptr(unsafe.Pointer(words)),
		uintptr(wordCount),
		uintptr(unsafe.Pointer(outVertices)),
		uintptr(vertexCapacity),
		uintptr(unsafe.Pointer(outVertexCount)),
		uintptr(unsafe.Pointer(outBounds)),
	)
	runtime.KeepAlive(commands)
	runtime.KeepAlive(words)
	runtime.KeepAlive(outVertices)
	runtime.KeepAlive(outVertexCount)
	runtime.KeepAlive(outBounds)
	return callStatus(r1), nil
}
