//go:build windows

package native

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Layout service FFI (Windows). Every call goes through the service table's
// function slots with syscall.SyscallN; the pointer-to-uintptr conversions
// sit inside the SyscallN call expressions (the documented-safe pattern) and
// runtime.KeepAlive covers every Go-owned record and buffer until the
// foreign call has returned. Status values are reinterpreted through the
// low 32 bits: the native entries return i32 in EAX.

// callStatus narrows a SyscallN return value to the native i32 status.
func callStatus(r1 uintptr) int32 {
	return int32(uint32(r1))
}

func nullSlot(name string) error {
	return &ABIError{Field: name, Expected: "non-null", Actual: "null"}
}

func callLayoutEngineCreate(fn uintptr, out *uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("engine_create")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(unsafe.Pointer(out)))
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

// callLayoutEngineOp invokes a (engine) -> i32 entry (reset/dispose).
func callLayoutEngineOp(fn uintptr, engine uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("engine_reset/dispose")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(engine))
	return callStatus(r1), nil
}

func callLayoutNodeCreate(
	fn uintptr,
	engine uint64,
	style *LayoutStyleRecord,
	children []NodeHandle,
	out *uint64,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_create")
	}
	var childPtr uintptr
	if len(children) > 0 {
		childPtr = uintptr(unsafe.Pointer(&children[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(unsafe.Pointer(style)),
		childPtr,
		uintptr(len(children)),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(style)
	runtime.KeepAlive(children)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callLayoutNodeSetStyle(fn uintptr, engine, node uint64, style *LayoutStyleRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_set_style")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(node),
		uintptr(unsafe.Pointer(style)),
	)
	runtime.KeepAlive(style)
	return callStatus(r1), nil
}

func callLayoutNodeSetMeasure(fn uintptr, engine, node, token uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_set_measure")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(engine), uintptr(node), uintptr(token))
	return callStatus(r1), nil
}

func callLayoutNodeRemoveSubtree(fn uintptr, engine, node uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_remove_subtree")
	}
	r1, _, _ := syscall.SyscallN(fn, uintptr(engine), uintptr(node))
	return callStatus(r1), nil
}

func callLayoutCompute(
	fn uintptr,
	engine, root uint64,
	available *LayoutAvailSize,
	computeToken uint64,
	outFlags *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("compute")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(root),
		uintptr(unsafe.Pointer(available)),
		uintptr(computeToken),
		uintptr(unsafe.Pointer(outFlags)),
	)
	runtime.KeepAlive(available)
	runtime.KeepAlive(outFlags)
	return callStatus(r1), nil
}

func callLayoutNodeLayout(fn uintptr, engine, node uint64, out *LayoutRecord) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_layout")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(node),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callLayoutNodeParent(fn uintptr, engine, node uint64, out *uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_parent")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(node),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callLayoutNodeChildCount(fn uintptr, engine, node uint64, out *uint32) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_child_count")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(node),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callLayoutNodeChild(fn uintptr, engine, node uint64, index uint32, out *uint64) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("node_child")
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(node),
		uintptr(index),
		uintptr(unsafe.Pointer(out)),
	)
	runtime.KeepAlive(out)
	return callStatus(r1), nil
}

func callLayoutDump(
	fn uintptr,
	engine, root uint64,
	nodeIDs []NodeHandle,
	records []LayoutRecord,
	capacity uint32,
	outCount *uint32,
) (int32, error) {
	if fn == 0 {
		return 0, nullSlot("dump")
	}
	var idPtr, recPtr uintptr
	if capacity > 0 {
		if len(nodeIDs) == 0 || len(records) == 0 {
			return 0, fmt.Errorf("%w: dump capacity %d with empty buffers", ErrBadStatus, capacity)
		}
		idPtr = uintptr(unsafe.Pointer(&nodeIDs[0]))
		recPtr = uintptr(unsafe.Pointer(&records[0]))
	}
	r1, _, _ := syscall.SyscallN(
		fn,
		uintptr(engine),
		uintptr(root),
		idPtr,
		recPtr,
		uintptr(capacity),
		uintptr(unsafe.Pointer(outCount)),
	)
	runtime.KeepAlive(nodeIDs)
	runtime.KeepAlive(records)
	runtime.KeepAlive(outCount)
	return callStatus(r1), nil
}

// ---------------------------------------------------------------------------
// The process-global measure trampoline
// ---------------------------------------------------------------------------

var (
	layoutTrampolineOnce sync.Once
	layoutTrampolineAddr uintptr
	layoutTrampolineErr  error
)

// ensureLayoutTrampoline creates the ONE syscall.NewCallback for the
// process. NewCallback allocates from a bounded pool, so the trampoline is
// created exactly once and shared by every library/service instance.
func ensureLayoutTrampoline() (uintptr, error) {
	layoutTrampolineOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				layoutTrampolineErr = fmt.Errorf("native: creating layout measure trampoline: %v", r)
			}
		}()
		layoutTrampolineAddr = syscall.NewCallback(layoutTrampolineFn)
	})
	return layoutTrampolineAddr, layoutTrampolineErr
}

// layoutTrampolineFn is the fixed Win64 callback installed with
// layout_set_trampoline. The native measure closure calls it synchronously
// during layout_compute, on the same OS thread, with pointers to
// native-owned request/response records that are valid for the duration of
// the call.
//
// It never blocks, never retains any pointer, and never lets a panic cross
// the boundary: panics from the registered Go measure function are
// recovered here, latched in measurePanics (keyed by compute token) for the
// owning Compute call, and reported as callback failure (status 1). The
// registry lookup happens without a held lock (sync.Map), and the token is
// generation-checked against the requesting engine (status 2).
//
// The result is uintptr because syscall.NewCallback only accepts
// word-sized results; the native side declares an i32 return, which the
// Win64 ABI reads from EAX — the low 32 bits of the word written here. The
// status values (0, 1, 2) fit either way.
func layoutTrampolineFn(req, resp uintptr) uintptr {
	// Copy the native-owned request record into Go-owned memory first; the
	// copy stays valid even while the deferred recover runs.
	request := *(*measureRequestRec)(*(*unsafe.Pointer)(unsafe.Pointer(&req)))

	defer func() {
		if r := recover(); r != nil {
			// Latch the recovered value for the owning Compute call, then
			// report callback failure (latched natively for this compute).
			measurePanics.Store(request.computeToken, r)
			writeMeasureFailure(resp)
		}
	}()

	value, ok := measureRegistry.Load(request.callbackToken)
	if !ok {
		writeMeasureStatus(resp, 2)
		return 2
	}
	entry, ok := value.(measureEntry)
	if !ok || entry.engine != EngineHandle(request.engine) {
		// Generation check: the token belongs to a different engine (or a
		// purged generation).
		writeMeasureStatus(resp, 2)
		return 2
	}

	measured := entry.fn(GoMeasureRequest{
		Engine:             EngineHandle(request.engine),
		Node:               NodeHandle(request.node),
		CallbackToken:      request.callbackToken,
		ComputeToken:       request.computeToken,
		KnownWidth:         math.Float32frombits(request.knownWidthBits),
		KnownHeight:        math.Float32frombits(request.knownHeightBits),
		KnownWidthPresent:  request.knownWidthPresent == 1,
		KnownHeightPresent: request.knownHeightPresent == 1,
		AvailWidth:         availFromTagBits(request.availWidthTag, request.availWidthBits),
		AvailHeight:        availFromTagBits(request.availHeightTag, request.availHeightBits),
	})

	// Non-finite sizes would poison the layout; treat them as callback
	// failures (the native side rejects them as well).
	if !isFinite32(measured.Width) || !isFinite32(measured.Height) {
		writeMeasureStatus(resp, 1)
		return 1
	}

	record := measureResponseRec{
		status:     0,
		widthBits:  math.Float32bits(measured.Width),
		heightBits: math.Float32bits(measured.Height),
	}
	*(*measureResponseRec)(*(*unsafe.Pointer)(unsafe.Pointer(&resp))) = record
	return 0
}

// isFinite32 reports whether v is neither NaN nor an infinity.
func isFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// writeMeasureFailure reports a recovered panic as callback failure.
func writeMeasureFailure(resp uintptr) {
	writeMeasureStatus(resp, 1)
}

// writeMeasureStatus writes a status-only response record in place.
func writeMeasureStatus(resp uintptr, status int32) {
	record := measureResponseRec{status: status, widthBits: 0, heightBits: 0}
	*(*measureResponseRec)(*(*unsafe.Pointer)(unsafe.Pointer(&resp))) = record
}

func availFromTagBits(tag, bits uint32) AvailSpace {
	return AvailSpace{Mode: AvailMode(tag), Definite: math.Float32frombits(bits)}
}

// layoutInstallTrampoline registers the fixed callback with the native
// library (layout_set_trampoline).
func layoutInstallTrampoline(setFn uintptr) error {
	if setFn == 0 {
		return nullSlot("set_trampoline")
	}
	addr, err := ensureLayoutTrampoline()
	if err != nil {
		return err
	}
	r1, _, _ := syscall.SyscallN(setFn, addr)
	if code := callStatus(r1); code != layoutStatusOK {
		return layoutStatusError(code, "layout_set_trampoline")
	}
	return nil
}
