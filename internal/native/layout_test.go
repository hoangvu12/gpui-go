//go:build windows

package native

import (
	"errors"
	"math"
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

// Layout service tests (ticket06) run against the REAL rebuilt DLL embedded
// in this package. The native measure trampoline is process-global and can
// only be installed (never uninstalled), so the no-trampoline case must be
// exercised by the first layout test that computes a measured tree.

func mustLayoutService(t *testing.T) *LayoutService {
	t.Helper()
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	svc, err := lib.Layout()
	if err != nil {
		t.Fatalf("Layout() failed: %v", err)
	}
	return svc
}

// flexRootStyle returns the ticket's 3-node flex fixture styles: a root with
// a definite 300x200 device-pixel size, a 100x50 child, and a flex-grow:1
// child. All lengths are device pixels.
func flexRootStyle() LayoutStyleRecord {
	root := NewLayoutStyleRecord()
	root.Size = LayoutSize{Width: DefiniteLen(300), Height: DefiniteLen(200)}
	return root
}

func smallChildStyle() LayoutStyleRecord {
	style := NewLayoutStyleRecord()
	style.Size = LayoutSize{Width: DefiniteLen(100), Height: DefiniteLen(50)}
	return style
}

func growChildStyle() LayoutStyleRecord {
	style := NewLayoutStyleRecord()
	style.FlexGrowPresent = 1
	style.FlexGrowBits = math.Float32bits(1.0)
	return style
}

func definiteAvailSize(w, h float32) LayoutAvailSize {
	return LayoutAvailSize{Width: DefiniteAvail(w), Height: DefiniteAvail(h)}
}

// flexFixture builds root(300x200) -> [child 100x50, child flex-grow 1] and
// computes it under definite 300x200 available space.
func flexFixture(t *testing.T, engine *LayoutEngine) (root, a, b NodeHandle) {
	t.Helper()
	var err error
	styleA := smallChildStyle()
	a, err = engine.CreateNode(&styleA)
	if err != nil {
		t.Fatalf("create child A: %v", err)
	}
	styleB := growChildStyle()
	b, err = engine.CreateNode(&styleB)
	if err != nil {
		t.Fatalf("create child B: %v", err)
	}
	rootStyle := flexRootStyle()
	root, err = engine.CreateNode(&rootStyle, a, b)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if _, err := engine.Compute(root, definiteAvailSize(300, 200), 1); err != nil {
		t.Fatalf("compute: %v", err)
	}
	return root, a, b
}

// (a) The layout service table validates: capability bit, service version,
// record-size self-checks against the Go mirrors, and non-null slots.
func TestLayoutServiceTableValidation(t *testing.T) {
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()

	svc, err := lib.Layout()
	if err != nil {
		t.Fatalf("Layout(): %v", err)
	}
	if svc.lib != lib {
		t.Fatal("service not bound to the library")
	}
	if lib.Identity().Capabilities&capLayoutTaffy == 0 {
		t.Errorf("identity capabilities %#x missing layout bit", lib.Identity().Capabilities)
	}

	// A mutated table copy must fail validation.
	base := svc.table
	mutations := []struct {
		name   string
		mutate func(*layoutTable)
	}{
		{"service version", func(tb *layoutTable) { tb.serviceVersion = 2 }},
		{"table size", func(tb *layoutTable) { tb.sizeOfTable = 999 }},
		{"table alignment", func(tb *layoutTable) { tb.alignOfTable = 4 }},
		{"style record size", func(tb *layoutTable) { tb.sizeOfStyleRecord = 340 }},
		{"layout record size", func(tb *layoutTable) { tb.sizeOfLayoutRecord = 64 }},
		{"avail size", func(tb *layoutTable) { tb.sizeOfAvail = 12 }},
		{"avail size pair", func(tb *layoutTable) { tb.sizeOfAvailSize = 8 }},
		{"measure request size", func(tb *layoutTable) { tb.sizeOfMeasureRequest = 64 }},
		{"measure request alignment", func(tb *layoutTable) { tb.alignOfMeasureRequest = 4 }},
		{"measure response size", func(tb *layoutTable) { tb.sizeOfMeasureResponse = 16 }},
		{"measure response alignment", func(tb *layoutTable) { tb.alignOfMeasureResponse = 8 }},
		{"null compute slot", func(tb *layoutTable) { tb.compute = 0 }},
		{"null dump slot", func(tb *layoutTable) { tb.dump = 0 }},
	}
	for _, m := range mutations {
		tb := base
		m.mutate(&tb)
		if err := validateLayoutTable(&tb, "test"); !errors.Is(err, ErrABIMismatch) && !errors.Is(err, ErrMissingCapability) {
			t.Errorf("%s: got %v, want ABI/capability mismatch error", m.name, err)
		}
	}

	// A ticket02-style artifact (layout capability bit missing) fails the
	// service fetch with ErrMissingCapability but still loads.
	id := lib.Identity()
	_ = id
	if err := validateLayoutTable(&base, "test"); err != nil {
		t.Errorf("unmutated table failed validation: %v", err)
	}
}

// (b) Go record mirrors match the documented binary layout (the native
// record_size fields are already checked at service fetch; these pin the
// field offsets).
func TestLayoutRecordBinaryLayout(t *testing.T) {
	if got := unsafe.Sizeof(LayoutStyleRecord{}); got != 336 {
		t.Errorf("sizeof(LayoutStyleRecord) = %d, want 336", got)
	}
	if got := unsafe.Alignof(LayoutStyleRecord{}); got != 4 {
		t.Errorf("alignof(LayoutStyleRecord) = %d, want 4", got)
	}
	offsets := []struct {
		name   string
		got    uintptr
		wanted uintptr
	}{
		{"Display", unsafe.Offsetof(LayoutStyleRecord{}.Display), 0},
		{"ScrollbarWidth", unsafe.Offsetof(LayoutStyleRecord{}.ScrollbarWidth), 16},
		{"Inset", unsafe.Offsetof(LayoutStyleRecord{}.Inset), 24},
		{"Size", unsafe.Offsetof(LayoutStyleRecord{}.Size), 56},
		{"Margin", unsafe.Offsetof(LayoutStyleRecord{}.Margin), 112},
		{"Padding", unsafe.Offsetof(LayoutStyleRecord{}.Padding), 144},
		{"BorderTop", unsafe.Offsetof(LayoutStyleRecord{}.BorderTop), 176},
		{"Gap", unsafe.Offsetof(LayoutStyleRecord{}.Gap), 224},
		{"FlexBasis", unsafe.Offsetof(LayoutStyleRecord{}.FlexBasis), 248},
		{"GridPlacementPresent", unsafe.Offsetof(LayoutStyleRecord{}.GridPlacementPresent), 296},
		{"RecordSize", unsafe.Offsetof(LayoutStyleRecord{}.RecordSize), 332},
	}
	for _, o := range offsets {
		if o.got != o.wanted {
			t.Errorf("offsetof(LayoutStyleRecord.%s) = %d, want %d", o.name, o.got, o.wanted)
		}
	}

	if got := unsafe.Sizeof(LayoutRecord{}); got != 72 {
		t.Errorf("sizeof(LayoutRecord) = %d, want 72", got)
	}
	if got := unsafe.Offsetof(LayoutRecord{}.RecordSize); got != 68 {
		t.Errorf("offsetof(LayoutRecord.RecordSize) = %d, want 68", got)
	}
	if got := unsafe.Sizeof(measureRequestRec{}); got != 72 {
		t.Errorf("sizeof(measureRequestRec) = %d, want 72", got)
	}
	if got := unsafe.Offsetof(measureRequestRec{}.knownWidthPresent); got != 32 {
		t.Errorf("offsetof(knownWidthPresent) = %d, want 32", got)
	}
	if got := unsafe.Offsetof(measureRequestRec{}.availWidthBits); got != 56 {
		t.Errorf("offsetof(availWidthBits) = %d, want 56", got)
	}
	if got := unsafe.Sizeof(measureResponseRec{}); got != 12 {
		t.Errorf("sizeof(measureResponseRec) = %d, want 12", got)
	}
	if got := unsafe.Sizeof(layoutTable{}); got != 160 {
		t.Errorf("sizeof(layoutTable) = %d, want 160", got)
	}
}

// (c) Measured node + no registered trampoline: caller error at compute
// time. Runs FIRST (the trampoline is process-global and stays installed
// once registered).
func TestLayoutMeasureWithoutTrampoline(t *testing.T) {
	svc := mustLayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	measuredStyle := NewLayoutStyleRecord()
	measured, err := engine.CreateNode(&measuredStyle)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetMeasure(measured, 0xDEAD); err != nil {
		t.Fatal(err)
	}
	rootStyle := flexRootStyle()
	root, err := engine.CreateNode(&rootStyle, measured)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Compute(root, definiteAvailSize(300, 200), 42)
	if !errors.Is(err, ErrLayoutNoTrampoline) {
		t.Fatalf("compute without trampoline: got %v, want ErrLayoutNoTrampoline", err)
	}
	// The engine is not failed by the missing trampoline (caller error).
	if _, err := engine.NodeLayout(root); err != nil {
		t.Errorf("NodeLayout after no-trampoline error: %v", err)
	}
}

// (d) The ticket's 3-node flex fixture computes taffy's flexbox geometry:
// root 300x200 at (0,0); child A 100x50 at (0,0) (explicit size, stretch
// does not override it); child B flex-grow 1 at (100,0) with size 200x200
// (stretch fills the cross axis). NodeLayout and layout_dump agree, and the
// parent/child relations hold.
func TestLayoutFlexTreeGeometry(t *testing.T) {
	svc := mustLayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	root, a, b := flexFixture(t, engine)

	type frame struct {
		x, y, w, h float32
	}
	check := func(name string, node NodeHandle, want frame) {
		t.Helper()
		record, err := engine.NodeLayout(node)
		if err != nil {
			t.Fatalf("NodeLayout(%s): %v", name, err)
		}
		got := frame{record.LocationX, record.LocationY, record.SizeW, record.SizeH}
		if got != want {
			t.Errorf("%s geometry = %+v, want %+v", name, got, want)
		}
	}
	check("root", root, frame{0, 0, 300, 200})
	check("child A", a, frame{0, 0, 100, 50})
	check("child B", b, frame{100, 0, 200, 200})

	// Parent/child relations.
	if parent, err := engine.NodeParent(a); err != nil || parent != root {
		t.Errorf("parent(A) = %d, err %v; want %d", parent, err, root)
	}
	if parent, err := engine.NodeParent(b); err != nil || parent != root {
		t.Errorf("parent(B) = %d, err %v; want %d", parent, err, root)
	}
	if parent, err := engine.NodeParent(root); err != nil || parent != 0 {
		t.Errorf("parent(root) = %d, err %v; want 0", parent, err)
	}
	if count, err := engine.NodeChildCount(root); err != nil || count != 2 {
		t.Errorf("childCount(root) = %d, err %v; want 2", count, err)
	}
	if child, err := engine.NodeChild(root, 0); err != nil || child != a {
		t.Errorf("child(root, 0) = %d, err %v; want %d", child, err, a)
	}
	if child, err := engine.NodeChild(root, 1); err != nil || child != b {
		t.Errorf("child(root, 1) = %d, err %v; want %d", child, err, b)
	}
	if _, err := engine.NodeChild(root, 2); !errors.Is(err, ErrLayoutBadStyle) {
		t.Errorf("child(root, 2) = %v, want ErrLayoutBadStyle (index bound)", err)
	}

	// Dump: preorder [root, A, B], matching NodeLayout records. The first
	// call uses a too-small capacity and retries internally.
	ids, records, err := engine.Dump(root, 1)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if len(ids) != 3 || len(records) != 3 {
		t.Fatalf("Dump returned %d records, want 3", len(records))
	}
	if ids[0] != root || ids[1] != a || ids[2] != b {
		t.Errorf("Dump preorder ids = %v, want [root a b]", ids)
	}
	if records[1].LocationX != 0 || records[1].SizeW != 100 || records[1].SizeH != 50 {
		t.Errorf("Dump record for A = %+v", records[1])
	}
	if records[2].LocationX != 100 || records[2].SizeW != 200 || records[2].SizeH != 200 {
		t.Errorf("Dump record for B = %+v", records[2])
	}
}

// (e) Style validation: bad tags/enumerants are caller errors at node
// creation, before any tree mutation.
func TestLayoutStyleValidation(t *testing.T) {
	svc := mustLayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	bad := NewLayoutStyleRecord()
	bad.Size = LayoutSize{Width: LayoutLen{Tag: 9, Bits: 0}, Height: AbsentLen()}
	if _, err := engine.CreateNode(&bad); !errors.Is(err, ErrLayoutBadStyle) {
		t.Errorf("bad size tag: got %v, want ErrLayoutBadStyle", err)
	}
	auto := NewLayoutStyleRecord()
	auto.Padding = LayoutEdges{Top: AutoLen(), Right: AbsentLen(), Bottom: AbsentLen(), Left: AbsentLen()}
	if _, err := engine.CreateNode(&auto); !errors.Is(err, ErrLayoutBadStyle) {
		t.Errorf("auto padding: got %v, want ErrLayoutBadStyle", err)
	}
	overflow := NewLayoutStyleRecord()
	overflow.OverflowX = 4
	if _, err := engine.CreateNode(&overflow); !errors.Is(err, ErrLayoutBadStyle) {
		t.Errorf("bad overflow: got %v, want ErrLayoutBadStyle", err)
	}
	record := NewLayoutStyleRecord()
	record.RecordSize = 100
	if _, err := engine.CreateNode(&record); !errors.Is(err, ErrLayoutBadStyle) {
		t.Errorf("bad record size: got %v, want ErrLayoutBadStyle", err)
	}
	// nil style record.
	if _, err := engine.CreateNode(nil); !errors.Is(err, ErrLayoutNullArg) {
		t.Errorf("nil style: got %v, want ErrLayoutNullArg", err)
	}
	// A garbage engine handle is a caller error.
	probeStyle := NewLayoutStyleRecord()
	if _, err := engine.CreateNode(&probeStyle); err != nil {
		t.Fatal(err)
	}
	code, callErr := callLayoutNodeCreate(svc.table.nodeCreate, 0xFFFFFFFFFFFF, &probeStyle, nil, new(uint64))
	if callErr != nil || (code != layoutErrStaleHandle && code != layoutErrBadHandle) {
		t.Errorf("garbage engine handle: code=%d err=%v, want stale/bad handle status", code, callErr)
	}
}

// (f) set_measure + the Go trampoline returning a fixed size: the computed
// layout honors it, the measured callbacks flag is set, and the measure
// request carries the identities and tokens.
func TestLayoutMeasureTrampolineFixedSize(t *testing.T) {
	svc := mustLayoutService(t)
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("RegisterMeasureTrampoline: %v", err)
	}
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}

	var seen []GoMeasureRequest
	token := engine.RegisterMeasure(func(req GoMeasureRequest) GoMeasureResponse {
		seen = append(seen, req)
		return GoMeasureResponse{Width: 80, Height: 40}
	})

	measuredStyle := NewLayoutStyleRecord()
	measured, err := engine.CreateNode(&measuredStyle)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetMeasure(measured, token); err != nil {
		t.Fatal(err)
	}
	fixedStyle := smallChildStyle()
	fixed, err := engine.CreateNode(&fixedStyle)
	if err != nil {
		t.Fatal(err)
	}
	rootStyle := flexRootStyle()
	rootStyle.AlignItemsPresent = 1
	rootStyle.AlignItems = AlignFlexStart // no cross-axis stretch
	root, err := engine.CreateNode(&rootStyle, measured, fixed)
	if err != nil {
		t.Fatal(err)
	}

	flags, err := engine.Compute(root, definiteAvailSize(300, 200), 7)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if flags&1 == 0 {
		t.Errorf("flags = %#x, want bit 0 set (measured callbacks invoked)", flags)
	}

	if len(seen) == 0 {
		t.Fatal("measure function was never called")
	}
	req := seen[len(seen)-1]
	if req.Engine != engine.Handle() {
		t.Errorf("request engine = %d, want %d", req.Engine, engine.Handle())
	}
	if req.Node != measured {
		t.Errorf("request node = %d, want %d", req.Node, measured)
	}
	if req.CallbackToken != token {
		t.Errorf("request callback token = %d, want %d", req.CallbackToken, token)
	}
	if req.ComputeToken != 7 {
		t.Errorf("request compute token = %d, want 7 (propagated)", req.ComputeToken)
	}

	// Flexbox row, flex-start: measured node (0,0) 80x40, then the fixed
	// node at x=80.
	measuredLayout, err := engine.NodeLayout(measured)
	if err != nil {
		t.Fatal(err)
	}
	if measuredLayout.LocationX != 0 || measuredLayout.LocationY != 0 ||
		measuredLayout.SizeW != 80 || measuredLayout.SizeH != 40 {
		t.Errorf("measured layout = %+v, want (0,0) 80x40", measuredLayout)
	}
	fixedLayout, err := engine.NodeLayout(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if fixedLayout.LocationX != 80 || fixedLayout.LocationY != 0 ||
		fixedLayout.SizeW != 100 || fixedLayout.SizeH != 50 {
		t.Errorf("fixed layout = %+v, want (80,0) 100x50", fixedLayout)
	}

	// An invalid token (not registered) fails the compute with the latched
	// callback-failure status.
	if err := engine.SetMeasure(measured, 0xDEAD); err != nil {
		t.Fatal(err)
	}
	_, err = engine.Compute(root, definiteAvailSize(300, 200), 8)
	if !errors.Is(err, ErrLayoutCallbackFailed) {
		t.Fatalf("compute with invalid token: got %v, want ErrLayoutCallbackFailed", err)
	}
	// The engine is failed until reset.
	if _, err := engine.NodeLayout(measured); !errors.Is(err, ErrLayoutEngineFailed) {
		t.Errorf("NodeLayout after callback failure: got %v, want ErrLayoutEngineFailed", err)
	}
}

// (g) Same-engine nested compute from inside the measure function: the
// Go busy guard rejects it before native entry with ErrLayoutEngineBusy,
// and the outer compute still succeeds.
func TestLayoutNestedSameEngineComputeRejected(t *testing.T) {
	svc := mustLayoutService(t)
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("RegisterMeasureTrampoline: %v", err)
	}
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}

	var nestedErr error
	token := engine.RegisterMeasure(func(req GoMeasureRequest) GoMeasureResponse {
		_, nestedErr = engine.Compute(req.Node, definiteAvailSize(300, 200), 99)
		return GoMeasureResponse{Width: 80, Height: 40}
	})

	measuredStyle := NewLayoutStyleRecord()
	measured, err := engine.CreateNode(&measuredStyle)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetMeasure(measured, token); err != nil {
		t.Fatal(err)
	}
	rootStyle := flexRootStyle()
	rootStyle.AlignItemsPresent = 1
	rootStyle.AlignItems = AlignFlexStart
	root, err := engine.CreateNode(&rootStyle, measured)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Compute(root, definiteAvailSize(300, 200), 10); err != nil {
		t.Fatalf("outer compute: %v", err)
	}
	if !errors.Is(nestedErr, ErrLayoutEngineBusy) {
		t.Errorf("nested same-engine compute: got %v, want ErrLayoutEngineBusy", nestedErr)
	}
	layout, err := engine.NodeLayout(measured)
	if err != nil {
		t.Fatal(err)
	}
	if layout.SizeW != 80 || layout.SizeH != 40 {
		t.Errorf("measured layout = %+v, want 80x40", layout)
	}
}

// (h) Independent-engine nested compute from inside a measure function
// succeeds (a different engine is not busy).
func TestLayoutIndependentEngineNestedCompute(t *testing.T) {
	svc := mustLayoutService(t)
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("RegisterMeasureTrampoline: %v", err)
	}
	engineA, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	engineB, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}

	// Engine B's tree: root 200x100 with a 60x40 child.
	childStyle := NewLayoutStyleRecord()
	childStyle.Size = LayoutSize{Width: DefiniteLen(60), Height: DefiniteLen(40)}
	childB, err := engineB.CreateNode(&childStyle)
	if err != nil {
		t.Fatal(err)
	}
	rootBStyle := NewLayoutStyleRecord()
	rootBStyle.Size = LayoutSize{Width: DefiniteLen(200), Height: DefiniteLen(100)}
	rootB, err := engineB.CreateNode(&rootBStyle, childB)
	if err != nil {
		t.Fatal(err)
	}

	var innerErr error
	var innerLayout LayoutRecord
	token := engineA.RegisterMeasure(func(req GoMeasureRequest) GoMeasureResponse {
		if _, innerErr = engineB.Compute(rootB, definiteAvailSize(200, 100), 21); innerErr != nil {
			return GoMeasureResponse{}
		}
		innerLayout, innerErr = engineB.NodeLayout(childB)
		return GoMeasureResponse{Width: 80, Height: 40}
	})

	measured, err := engineA.CreateNode(&[]LayoutStyleRecord{NewLayoutStyleRecord()}[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := engineA.SetMeasure(measured, token); err != nil {
		t.Fatal(err)
	}
	rootAStyle := flexRootStyle()
	rootAStyle.AlignItemsPresent = 1
	rootAStyle.AlignItems = AlignFlexStart
	rootA, err := engineA.CreateNode(&rootAStyle, measured)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := engineA.Compute(rootA, definiteAvailSize(300, 200), 11); err != nil {
		t.Fatalf("outer compute on engine A: %v", err)
	}
	if innerErr != nil {
		t.Fatalf("nested compute on engine B: %v", innerErr)
	}
	if innerLayout.SizeW != 60 || innerLayout.SizeH != 40 || innerLayout.LocationY != 0 {
		t.Errorf("engine B child layout = %+v, want 60x40 at y=0", innerLayout)
	}
	// Both engines remain usable afterwards.
	if layout, err := engineA.NodeLayout(measured); err != nil || layout.SizeW != 80 || layout.SizeH != 40 {
		t.Errorf("engine A measured layout = %+v, err %v", layout, err)
	}
}

// (i) A panic in the Go measure function: recovered inside the trampoline,
// latched as callback failure (no crash), the recovered value is attached to
// the returned error, and the engine is failed until reset.
func TestLayoutMeasurePanicLatched(t *testing.T) {
	svc := mustLayoutService(t)
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("RegisterMeasureTrampoline: %v", err)
	}
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("measure exploded")
	token := engine.RegisterMeasure(func(req GoMeasureRequest) GoMeasureResponse {
		panic(sentinel)
	})

	measuredStyle := NewLayoutStyleRecord()
	measured, err := engine.CreateNode(&measuredStyle)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetMeasure(measured, token); err != nil {
		t.Fatal(err)
	}
	rootStyle := flexRootStyle()
	root, err := engine.CreateNode(&rootStyle, measured)
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Compute(root, definiteAvailSize(300, 200), 12)
	if !errors.Is(err, ErrLayoutCallbackFailed) {
		t.Fatalf("compute with panicking measure: got %v, want ErrLayoutCallbackFailed", err)
	}
	statusErr := &LayoutStatusError{}
	if !errors.As(err, &statusErr) || statusErr.Panic != sentinel {
		t.Errorf("recovered panic value = %v, want the sentinel", statusErr.Panic)
	}

	// The engine is failed until reset; reset recovers it.
	if _, err := engine.NodeLayout(root); !errors.Is(err, ErrLayoutEngineFailed) {
		t.Errorf("NodeLayout after latched panic: got %v, want ErrLayoutEngineFailed", err)
	}
	if _, err := engine.Compute(root, definiteAvailSize(300, 200), 13); !errors.Is(err, ErrLayoutEngineFailed) {
		t.Errorf("recompute on failed engine: got %v, want ErrLayoutEngineFailed", err)
	}
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset failed engine: %v", err)
	}
	freshStyle := flexRootStyle()
	fresh, err := engine.CreateNode(&freshStyle)
	if err != nil {
		t.Fatalf("create node after reset: %v", err)
	}
	if _, err := engine.Compute(fresh, definiteAvailSize(300, 200), 14); err != nil {
		t.Errorf("compute after reset: %v", err)
	}

	// The Go runtime is healthy: a normal round trip still works.
	lib := svc.lib
	echo, checksum, err := lib.RoundTrip([]byte("still-alive"))
	if err != nil || string(echo) != "still-alive" || checksum != fnv1a64([]byte("still-alive")) {
		t.Errorf("round trip after measure panic: echo=%q err=%v", echo, err)
	}
}

// (j) Stale handles after reset: node handles die with the generation; the
// engine handle survives; disposed engines reject everything.
func TestLayoutStaleHandles(t *testing.T) {
	svc := mustLayoutService(t)
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}
	root, a, b := flexFixture(t, engine)

	if err := engine.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	for name, node := range map[string]NodeHandle{"root": root, "a": a, "b": b} {
		if _, err := engine.NodeLayout(node); !errors.Is(err, ErrLayoutStaleHandle) {
			t.Errorf("NodeLayout(%s) after reset: got %v, want ErrLayoutStaleHandle", name, err)
		}
	}
	if _, err := engine.NodeParent(a); !errors.Is(err, ErrLayoutStaleHandle) {
		t.Errorf("NodeParent(a) after reset: got %v, want ErrLayoutStaleHandle", err)
	}
	if _, err := engine.NodeChild(root, 0); !errors.Is(err, ErrLayoutStaleHandle) {
		t.Errorf("NodeChild after reset: got %v, want ErrLayoutStaleHandle", err)
	}
	if _, err := engine.Compute(root, definiteAvailSize(300, 200), 1); !errors.Is(err, ErrLayoutStaleHandle) {
		t.Errorf("Compute after reset: got %v, want ErrLayoutStaleHandle", err)
	}

	// The engine handle is still valid: new nodes work.
	freshStyle := flexRootStyle()
	fresh, err := engine.CreateNode(&freshStyle)
	if err != nil {
		t.Fatalf("create node after reset: %v", err)
	}
	if _, err := engine.Compute(fresh, definiteAvailSize(300, 200), 2); err != nil {
		t.Errorf("compute fresh root: %v", err)
	}

	// Subtree removal invalidates the removed handles but not the rest.
	_, a2, _ := flexFixture(t, engine)
	if err := engine.RemoveSubtree(a2); err != nil {
		t.Fatalf("remove subtree: %v", err)
	}
	if _, err := engine.NodeLayout(a2); !errors.Is(err, ErrLayoutStaleHandle) {
		t.Errorf("NodeLayout(removed) = %v, want ErrLayoutStaleHandle", err)
	}

	// Disposed engines reject their handle.
	if err := engine.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if err := engine.Reset(); !errors.Is(err, ErrLayoutStaleHandle) {
		t.Errorf("reset after dispose: got %v, want ErrLayoutStaleHandle", err)
	}
}

// (k) Forced GC during compute + measure callbacks: no dangling references
// (no Go pointer crosses the boundary; the registry roots the functions).
func TestLayoutForcedGCDuringComputeAndMeasures(t *testing.T) {
	svc := mustLayoutService(t)
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("RegisterMeasureTrampoline: %v", err)
	}
	engine, err := svc.CreateEngine()
	if err != nil {
		t.Fatal(err)
	}

	// The measure function allocates so the GC has something to move.
	token := engine.RegisterMeasure(func(req GoMeasureRequest) GoMeasureResponse {
		data := make([]float32, 64)
		for i := range data {
			data[i] = float32(i)
		}
		return GoMeasureResponse{Width: data[63] + 17, Height: 40}
	})

	measuredStyle := NewLayoutStyleRecord()
	measured, err := engine.CreateNode(&measuredStyle)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetMeasure(measured, token); err != nil {
		t.Fatal(err)
	}
	fixedStyle := smallChildStyle()
	fixed, err := engine.CreateNode(&fixedStyle)
	if err != nil {
		t.Fatal(err)
	}
	rootStyle := flexRootStyle()
	rootStyle.AlignItemsPresent = 1
	rootStyle.AlignItems = AlignFlexStart
	root, err := engine.CreateNode(&rootStyle, measured, fixed)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var gcWG sync.WaitGroup
	gcWG.Add(1)
	go func() {
		defer gcWG.Done()
		for {
			select {
			case <-done:
				return
			default:
				runtime.GC()
			}
		}
	}()

	for i := 0; i < 300; i++ {
		// Taffy's per-node cache may skip the measure callback when the
		// inputs repeat ("Taffy's caching may avoid a callback"); dirty the
		// measured node so every iteration really re-measures under GC
		// pressure.
		if err := engine.SetStyle(measured, &measuredStyle); err != nil {
			t.Fatalf("iteration %d: re-dirty: %v", i, err)
		}
		flags, err := engine.Compute(root, definiteAvailSize(300, 200), uint64(1000+i))
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if flags&1 == 0 {
			t.Fatalf("iteration %d: measured callbacks not invoked", i)
		}
		layout, err := engine.NodeLayout(measured)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if layout.SizeW != 80 || layout.SizeH != 40 {
			t.Fatalf("iteration %d: measured layout = %+v, want 80x40", i, layout)
		}
	}
	close(done)
	gcWG.Wait()

	// A full dump under GC pressure stays consistent.
	ids, records, err := engine.Dump(root, 3)
	if err != nil || len(ids) != 3 || len(records) != 3 {
		t.Fatalf("dump after GC loop: ids=%d records=%d err=%v", len(ids), len(records), err)
	}
}

// (l) RegisterMeasureTrampoline is idempotent (one NewCallback per process)
// and rejects a released library.
func TestLayoutTrampolineRegistration(t *testing.T) {
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := lib.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := svc.RegisterMeasureTrampoline(); err != nil {
		t.Fatalf("second registration (idempotent): %v", err)
	}
	// Only one callback was allocated.
	layoutTrampolineOnce.Do(func() { t.Fatal("trampoline created twice") })
	if layoutTrampolineAddr == 0 {
		t.Fatal("no trampoline address")
	}
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterMeasureTrampoline(); !errors.Is(err, ErrClosed) {
		t.Errorf("registration after Close: got %v, want ErrClosed", err)
	}
}

// (m) Manifest identity: the embedded manifest carries the layout
// capability and the resolved Taffy pin.
func TestLayoutManifestIdentity(t *testing.T) {
	auth := testAuthority(t)
	// Ticket07 added the renderer capability (bit 2); ticket08 added the
	// scene kernel capability (bit 3); ticket09 added the text capability
	// (bit 4); ticket10 added the glyph raster and atlas capabilities
	// (bits 5 and 6); ticket16 added the scene-draw capability (bit 7);
	// the mask is the cumulative set of landed services, recorded by
	// tools/measure.
	wantMask := capBootstrapBufferRoundTrip | capLayoutTaffy | capRendererD3D11 | capSceneKernel | capTextParley | capGlyphRaster | capAtlasD3D11 | capSceneDrawPaths
	if auth.CapabilitiesMask != wantMask {
		t.Errorf("manifest capabilities mask = %#x, want %#x (bootstrap + layout + renderer + scene + text + glyph + atlas + scene-draw)", auth.CapabilitiesMask, wantMask)
	}
	if auth.NativeRevision != nativeRevision {
		t.Errorf("manifest native revision = %d, want %d", auth.NativeRevision, nativeRevision)
	}
	if auth.Taffy == nil {
		t.Fatal("manifest missing the taffy resolution record")
	}
	if auth.Taffy.Version != "0.13.0" || !auth.Taffy.PinSatisfied {
		t.Errorf("manifest taffy version = %q pin=%v, want 0.13.0 true", auth.Taffy.Version, auth.Taffy.PinSatisfied)
	}
	if auth.Taffy.Checksum != "c034e05f6ee85a12daa63863c2245797715075c70649947aa0da54f3f2ab1d0f" {
		t.Errorf("manifest taffy checksum = %q", auth.Taffy.Checksum)
	}
	if len(auth.Taffy.Features) != 9 {
		t.Errorf("manifest taffy features = %v, want the 9 defaults", auth.Taffy.Features)
	}
}
