//go:build !windows

package native

// Layout service FFI stubs for non-Windows hosts. The Windows AMD64 artifact
// cannot load there (Load fails with ErrUnsupportedTarget), so every layout
// call helper surfaces the same typed error instead of a build break; the
// package still compiles everywhere for module tooling.

func callLayoutEngineCreate(fn uintptr, out *uint64) (int32, error) {
	return 0, hostSupported()
}

func callLayoutEngineOp(fn uintptr, engine uint64) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeCreate(
	fn uintptr,
	engine uint64,
	style *LayoutStyleRecord,
	children []NodeHandle,
	out *uint64,
) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeSetStyle(fn uintptr, engine, node uint64, style *LayoutStyleRecord) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeSetMeasure(fn uintptr, engine, node, token uint64) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeRemoveSubtree(fn uintptr, engine, node uint64) (int32, error) {
	return 0, hostSupported()
}

func callLayoutCompute(
	fn uintptr,
	engine, root uint64,
	available *LayoutAvailSize,
	computeToken uint64,
	outFlags *uint32,
) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeLayout(fn uintptr, engine, node uint64, out *LayoutRecord) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeParent(fn uintptr, engine, node uint64, out *uint64) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeChildCount(fn uintptr, engine, node uint64, out *uint32) (int32, error) {
	return 0, hostSupported()
}

func callLayoutNodeChild(fn uintptr, engine, node uint64, index uint32, out *uint64) (int32, error) {
	return 0, hostSupported()
}

func callLayoutDump(
	fn uintptr,
	engine, root uint64,
	nodeIDs []NodeHandle,
	records []LayoutRecord,
	capacity uint32,
	outCount *uint32,
) (int32, error) {
	return 0, hostSupported()
}

// layoutInstallTrampoline requires syscall.NewCallback (Windows only).
func layoutInstallTrampoline(setFn uintptr) error {
	return hostSupported()
}
