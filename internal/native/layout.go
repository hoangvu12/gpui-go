// Layout service access (ticket06).
//
// This file is the Go mirror of the native layout service documented in
// reference/native/LAYOUT_ABI.md and implemented in
// reference/native/src/layout.rs (Taffy 0.13.0 behind the pinned gpui-CE
// adapter semantics). The service table lives in reserved slot 1 of the
// bootstrap ABI table and is advertised by capability bit 1
// ("layout-taffy-0-13-0").
//
// Units: the native side computes in device pixels. Every definite length in
// a style record was converted by the Go adapter with the pinned rounding
// rules (docs/layout-contract.md); measure requests and responses also carry
// device pixels. All logical-unit conversion and snapping belongs to the
// higher-level Go adapter built on this package.
//
// Records are plain fixed-width Go structs whose layout is asserted against
// the native self-check fields at service-fetch time (validateLayoutTable).
// No Go pointer ever crosses the boundary: the measure trampoline receives
// native-owned records and dispatches through a token registry.

package native

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Status codes and errors
// ---------------------------------------------------------------------------

// Native layout service status codes (mirror layout_status in
// reference/native/src/layout.rs). 0 is success; negative values are
// caller/argument errors; 100+ are internal failures.
const (
	layoutStatusOK          int32 = 0
	layoutErrStaleHandle    int32 = -1
	layoutErrBadHandle      int32 = -2
	layoutErrNullArg        int32 = -3
	layoutErrBadValue       int32 = -4
	layoutErrNodeAttached   int32 = -5
	layoutErrEngineBusy     int32 = -6
	layoutErrNoTrampoline   int32 = -7
	layoutErrCapacity       int32 = -8
	layoutErrEngineFailed   int32 = 100
	layoutErrPanic          int32 = 101
	layoutErrCallbackFailed int32 = 102
)

// layoutServiceVersion mirrors GPUI_GO_LAYOUT_SERVICE_VERSION.
const layoutServiceVersion uint32 = 1

func layoutStatusName(code int32) string {
	switch code {
	case layoutStatusOK:
		return "ok"
	case layoutErrStaleHandle:
		return "stale handle"
	case layoutErrBadHandle:
		return "bad handle"
	case layoutErrNullArg:
		return "null argument"
	case layoutErrBadValue:
		return "bad value (invalid tag/enumerant/bound)"
	case layoutErrNodeAttached:
		return "child node already attached"
	case layoutErrEngineBusy:
		return "engine busy (nested same-engine operation)"
	case layoutErrNoTrampoline:
		return "measured node without a registered trampoline"
	case layoutErrCapacity:
		return "dump capacity too small"
	case layoutErrEngineFailed:
		return "engine failed (reset or dispose required)"
	case layoutErrPanic:
		return "native panic contained"
	case layoutErrCallbackFailed:
		return "measure callback failed (latched)"
	default:
		return "unknown layout status"
	}
}

// Layout service sentinel errors, distinguishable with errors.Is.
var (
	// ErrLayoutStaleHandle: the handle encoded a past generation (engine
	// disposed, engine reset since, or node removed).
	ErrLayoutStaleHandle = errors.New("native: layout handle is stale")
	// ErrLayoutBadHandle: the handle never existed or is malformed.
	ErrLayoutBadHandle = errors.New("native: layout handle is invalid")
	// ErrLayoutNullArg: a required pointer argument was null.
	ErrLayoutNullArg = errors.New("native: layout argument was null")
	// ErrLayoutBadStyle: a record field held an invalid tag, enumerant or
	// bound.
	ErrLayoutBadStyle = errors.New("native: layout style/value rejected")
	// ErrLayoutNodeAttached: node creation was given an attached child.
	ErrLayoutNodeAttached = errors.New("native: layout child already attached")
	// ErrLayoutEngineBusy: the engine is computing; the operation was
	// rejected (Go-side guard fires before native entry for nested
	// same-engine calls; the native busy flag is defense in depth).
	ErrLayoutEngineBusy = errors.New("native: layout engine busy")
	// ErrLayoutNoTrampoline: a computed tree has a measured node but no
	// registered trampoline.
	ErrLayoutNoTrampoline = errors.New("native: layout trampoline not registered")
	// ErrLayoutCapacity: dump capacity was smaller than the subtree.
	ErrLayoutCapacity = errors.New("native: layout dump capacity too small")
	// ErrLayoutEngineFailed: the engine is marked failed (panic or callback
	// failure); reset or dispose is required.
	ErrLayoutEngineFailed = errors.New("native: layout engine failed")
	// ErrLayoutPanic: a native panic was contained by catch_unwind.
	ErrLayoutPanic = errors.New("native: layout panic contained")
	// ErrLayoutCallbackFailed: the measure trampoline failed during a
	// compute; the failure was latched and the engine is now failed.
	ErrLayoutCallbackFailed = errors.New("native: layout measure callback failed")
)

// LayoutStatusError reports a non-zero layout service status code. For
// layoutErrCallbackFailed, Panic carries the value recovered from the Go
// measure function (if any), so ordinary Go code can inspect — and choose to
// re-raise — the original panic after the native call has returned and
// engine bookkeeping was restored.
type LayoutStatusError struct {
	Code   int32
	Name   string
	Detail string
	Panic  any // recovered panic value, only for ErrLayoutCallbackFailed
}

func (e *LayoutStatusError) Error() string {
	detail := e.Detail
	if detail != "" {
		detail = ": " + detail
	}
	if e.Panic != nil {
		detail += fmt.Sprintf(" (recovered panic: %v)", e.Panic)
	}
	return ErrBadStatus.Error() + ": layout status " + fmt.Sprint(e.Code) + " (" + e.Name + ")" + detail
}

func (e *LayoutStatusError) Unwrap() error {
	switch e.Code {
	case layoutErrStaleHandle:
		return ErrLayoutStaleHandle
	case layoutErrBadHandle:
		return ErrLayoutBadHandle
	case layoutErrNullArg:
		return ErrLayoutNullArg
	case layoutErrBadValue:
		return ErrLayoutBadStyle
	case layoutErrNodeAttached:
		return ErrLayoutNodeAttached
	case layoutErrEngineBusy:
		return ErrLayoutEngineBusy
	case layoutErrNoTrampoline:
		return ErrLayoutNoTrampoline
	case layoutErrCapacity:
		return ErrLayoutCapacity
	case layoutErrEngineFailed:
		return ErrLayoutEngineFailed
	case layoutErrPanic:
		return ErrLayoutPanic
	case layoutErrCallbackFailed:
		return ErrLayoutCallbackFailed
	default:
		return ErrBadStatus
	}
}

func layoutStatusError(code int32, detail string) *LayoutStatusError {
	return &LayoutStatusError{Code: code, Name: layoutStatusName(code), Detail: detail}
}

// ---------------------------------------------------------------------------
// Record mirrors
// ---------------------------------------------------------------------------

// Length tags (LayoutLen.Tag).
const (
	// LenTagAuto: automatic length (bits ignored).
	LenTagAuto uint32 = 0
	// LenTagDefinite: definite device pixels (Go already applied the pinned
	// rounding rules).
	LenTagDefinite uint32 = 1
	// LenTagPercent: percentage, a fraction of the parent.
	LenTagPercent uint32 = 2
	// LenTagAbsent: field omitted; the native side keeps the Taffy default.
	// Absent and an explicit definite zero are distinct.
	LenTagAbsent uint32 = 3
)

// LayoutLen mirrors the native GpuiLen (8 bytes, align 4): tag + f32 bits.
type LayoutLen struct {
	Tag  uint32
	Bits uint32
}

// AutoLen returns an automatic length.
func AutoLen() LayoutLen { return LayoutLen{Tag: LenTagAuto} }

// DefiniteLen returns a definite device-pixel length.
func DefiniteLen(px float32) LayoutLen {
	return LayoutLen{Tag: LenTagDefinite, Bits: math.Float32bits(px)}
}

// PercentLen returns a percentage length (fraction of the parent).
func PercentLen(fraction float32) LayoutLen {
	return LayoutLen{Tag: LenTagPercent, Bits: math.Float32bits(fraction)}
}

// AbsentLen returns an omitted length (native keeps the Taffy default).
func AbsentLen() LayoutLen { return LayoutLen{Tag: LenTagAbsent} }

// Float decodes the length's value (meaningful for definite/percent tags).
func (l LayoutLen) Float() float32 { return math.Float32frombits(l.Bits) }

// LayoutEdges mirrors GpuiEdges: four lengths, top/right/bottom/left
// (32 bytes, align 4).
type LayoutEdges struct {
	Top    LayoutLen
	Right  LayoutLen
	Bottom LayoutLen
	Left   LayoutLen
}

// LayoutSize mirrors GpuiSizeL: width and height lengths (16 bytes, align 4).
type LayoutSize struct {
	Width  LayoutLen
	Height LayoutLen
}

// Available-space tags (LayoutAvail.Tag).
const (
	// AvailTagDefinite: a definite device-pixel amount.
	AvailTagDefinite uint32 = 0
	// AvailTagMinContent: indefinite, min-content constraint.
	AvailTagMinContent uint32 = 1
	// AvailTagMaxContent: indefinite, max-content constraint.
	AvailTagMaxContent uint32 = 2
)

// LayoutAvail mirrors GpuiAvail: available space on one axis (8 bytes).
type LayoutAvail struct {
	Tag  uint32
	Bits uint32
}

// DefiniteAvail returns definite available space in device pixels.
func DefiniteAvail(px float32) LayoutAvail {
	return LayoutAvail{Tag: AvailTagDefinite, Bits: math.Float32bits(px)}
}

// MinContentAvail returns a min-content available-space mode.
func MinContentAvail() LayoutAvail { return LayoutAvail{Tag: AvailTagMinContent} }

// MaxContentAvail returns a max-content available-space mode.
func MaxContentAvail() LayoutAvail { return LayoutAvail{Tag: AvailTagMaxContent} }

// LayoutAvailSize mirrors GpuiAvailSize: available space on both axes
// (16 bytes, align 4).
type LayoutAvailSize struct {
	Width  LayoutAvail
	Height LayoutAvail
}

// Display values (LayoutStyleRecord.Display). The Go adapter maps gpui's
// Inline to DisplayBlock and InlineFlex to DisplayFlex before the record.
const (
	DisplayFlex  uint32 = 0
	DisplayBlock uint32 = 1
	DisplayGrid  uint32 = 2
	DisplayNone  uint32 = 3
)

// Position values (LayoutStyleRecord.Position).
const (
	PositionRelative uint32 = 0
	PositionAbsolute uint32 = 1
)

// Overflow values (LayoutStyleRecord.OverflowX/Y), in the pinned gpui enum
// source order: Visible, Clip, Hidden, Scroll.
const (
	OverflowVisible uint32 = 0
	OverflowClip    uint32 = 1
	OverflowHidden  uint32 = 2
	OverflowScroll  uint32 = 3
)

// FlexDirection values, in the pinned gpui enum source order.
const (
	FlexDirectionRow           uint32 = 0
	FlexDirectionColumn        uint32 = 1
	FlexDirectionRowReverse    uint32 = 2
	FlexDirectionColumnReverse uint32 = 3
)

// FlexWrap values, in the pinned gpui enum source order.
const (
	FlexWrapNoWrap      uint32 = 0
	FlexWrapWrap        uint32 = 1
	FlexWrapWrapReverse uint32 = 2
)

// AlignItems/AlignSelf values (gpui AlignItems source order).
const (
	AlignStart     uint32 = 0
	AlignEnd       uint32 = 1
	AlignFlexStart uint32 = 2
	AlignFlexEnd   uint32 = 3
	AlignCenter    uint32 = 4
	AlignBaseline  uint32 = 5
	AlignStretch   uint32 = 6
)

// AlignContent/JustifyContent values (gpui AlignContent source order).
const (
	ContentStart        uint32 = 0
	ContentEnd          uint32 = 1
	ContentFlexStart    uint32 = 2
	ContentFlexEnd      uint32 = 3
	ContentCenter       uint32 = 4
	ContentStretch      uint32 = 5
	ContentSpaceBetween uint32 = 6
	ContentSpaceEvenly  uint32 = 7
	ContentSpaceAround  uint32 = 8
)

// Grid template minimum sizes (LayoutStyleRecord grid template min size).
const (
	TemplateMinZero       uint32 = 0
	TemplateMinMinContent uint32 = 1
	TemplateMinMaxContent uint32 = 2
)

// Grid placement kinds.
const (
	GridPlacementAuto uint32 = 0
	GridPlacementLine uint32 = 1
	GridPlacementSpan uint32 = 2
)

// LayoutStyleRecord mirrors GpuiGoLayoutStyleRecord exactly: 84 u32 fields,
// 336 bytes, alignment 4. Field order and offsets are part of the ABI; the
// size is validated against the service table's size_of_style_record before
// any call. Every optional field uses a presence flag or the LenTagAbsent
// length tag; absent fields keep the Taffy default.
type LayoutStyleRecord struct {
	// --- display / position / overflow ------------------------------
	Display   uint32
	Position  uint32
	OverflowX uint32
	OverflowY uint32
	// Scrollbar width: LenTagDefinite or LenTagAbsent.
	ScrollbarWidth LayoutLen

	// --- insets and sizing -------------------------------------------
	Inset              LayoutEdges
	Size               LayoutSize
	MinSize            LayoutSize
	MaxSize            LayoutSize
	AspectRatioPresent uint32
	AspectRatioBits    uint32

	// --- spacing -------------------------------------------------------
	Margin  LayoutEdges
	Padding LayoutEdges
	// Border widths in device pixels (already stroke-snapped by the Go
	// adapter with the pinned round_stroke_to_device_pixel rule).
	BorderTop    uint32
	BorderRight  uint32
	BorderBottom uint32
	BorderLeft   uint32

	// --- alignment -----------------------------------------------------
	AlignItemsPresent     uint32
	AlignItems            uint32
	AlignSelfPresent      uint32
	AlignSelf             uint32
	AlignContentPresent   uint32
	AlignContent          uint32
	JustifyContentPresent uint32
	JustifyContent        uint32
	Gap                   LayoutSize

	// --- flexbox ---------------------------------------------------------
	FlexDirection     uint32
	FlexWrap          uint32
	FlexBasis         LayoutLen
	FlexGrowPresent   uint32
	FlexGrowBits      uint32
	FlexShrinkPresent uint32
	FlexShrinkBits    uint32

	// --- grid --------------------------------------------------------
	GridTemplateRowsPresent    uint32
	GridTemplateRowsRepeat     uint32
	GridTemplateRowsMinSize    uint32
	GridTemplateColumnsPresent uint32
	GridTemplateColumnsRepeat  uint32
	GridTemplateColumnsMinSize uint32
	GridPlacementPresent       uint32
	GridRowStartKind           uint32
	GridRowStartValue          uint32
	GridRowEndKind             uint32
	GridRowEndValue            uint32
	GridColumnStartKind        uint32
	GridColumnStartValue       uint32
	GridColumnEndKind          uint32
	GridColumnEndValue         uint32

	// Record-size self-check; must stay 336.
	RecordSize uint32
}

// NewLayoutStyleRecord returns a style record with every optional field
// absent and every defaulted field carrying the gpui default (the record the
// pinned gpui Style::default() forwards). It translates to exactly the
// Taffy default style.
func NewLayoutStyleRecord() LayoutStyleRecord {
	return LayoutStyleRecord{
		Display:             DisplayFlex,
		Position:            PositionRelative,
		OverflowX:           OverflowVisible,
		OverflowY:           OverflowVisible,
		ScrollbarWidth:      AbsentLen(),
		Inset:               LayoutEdges{Top: AbsentLen(), Right: AbsentLen(), Bottom: AbsentLen(), Left: AbsentLen()},
		Size:                LayoutSize{Width: AbsentLen(), Height: AbsentLen()},
		MinSize:             LayoutSize{Width: AbsentLen(), Height: AbsentLen()},
		MaxSize:             LayoutSize{Width: AbsentLen(), Height: AbsentLen()},
		Margin:              LayoutEdges{Top: AbsentLen(), Right: AbsentLen(), Bottom: AbsentLen(), Left: AbsentLen()},
		Padding:             LayoutEdges{Top: AbsentLen(), Right: AbsentLen(), Bottom: AbsentLen(), Left: AbsentLen()},
		Gap:                 LayoutSize{Width: AbsentLen(), Height: AbsentLen()},
		FlexDirection:       FlexDirectionRow,
		FlexWrap:            FlexWrapNoWrap,
		FlexBasis:           AbsentLen(),
		GridRowStartKind:    GridPlacementAuto,
		GridRowEndKind:      GridPlacementAuto,
		GridColumnStartKind: GridPlacementAuto,
		GridColumnEndKind:   GridPlacementAuto,
		RecordSize:          uint32(unsafe.Sizeof(LayoutStyleRecord{})),
	}
}

// LayoutRecord mirrors GpuiGoLayoutRecord: order, location, size, content
// size, scrollbar size, border and padding — all unrounded device pixels
// (Taffy rounding is disabled; the pinned snapping runs on the Go side).
// 72 bytes, alignment 4.
type LayoutRecord struct {
	Order         int32
	LocationX     float32
	LocationY     float32
	SizeW         float32
	SizeH         float32
	ContentW      float32
	ContentH      float32
	ScrollbarW    float32
	ScrollbarH    float32
	BorderTop     float32
	BorderRight   float32
	BorderBottom  float32
	BorderLeft    float32
	PaddingTop    float32
	PaddingRight  float32
	PaddingBottom float32
	PaddingLeft   float32
	RecordSize    uint32
}

// ---------------------------------------------------------------------------
// Service table mirror and validation
// ---------------------------------------------------------------------------

// layoutTable mirrors the native GpuiGoLayoutTable: 14 function pointers
// then 11 self-check scalars; 160 bytes, alignment 8.
type layoutTable struct {
	engineCreate           uintptr
	engineReset            uintptr
	engineDispose          uintptr
	nodeCreate             uintptr
	nodeSetStyle           uintptr
	nodeSetMeasure         uintptr
	nodeRemoveSubtree      uintptr
	compute                uintptr
	nodeLayout             uintptr
	nodeParent             uintptr
	nodeChildCount         uintptr
	nodeChild              uintptr
	dump                   uintptr
	setTrampoline          uintptr
	serviceVersion         uint32
	sizeOfTable            uint32
	alignOfTable           uint32
	sizeOfStyleRecord      uint32
	sizeOfLayoutRecord     uint32
	sizeOfAvail            uint32
	sizeOfAvailSize        uint32
	sizeOfMeasureRequest   uint32
	alignOfMeasureRequest  uint32
	sizeOfMeasureResponse  uint32
	alignOfMeasureResponse uint32
}

// layoutTableFromSlot reinterprets a reserved-slot word (the address of the
// native static service table) as a table pointer. The conversion goes
// through the reinterpretation used by pointerFromRaw: go vet has no
// sanctioned form for uintptr-to-unsafe.Pointer in this direction. The
// pointee is a Rust static valid for the process lifetime; it is copied out
// immediately and never retained.
func layoutTableFromSlot(slot uintptr) *layoutTable {
	return (*layoutTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

// validateLayoutTable checks the layout service table copy against the Go
// mirrors before any function slot is trusted: service version, table
// size/alignment, and every record size/alignment. Mirrors
// layoutSelfChecks in abi.go.
func validateLayoutTable(t *layoutTable, path string) error {
	abi := func(field, expected, actual string) error {
		return &ABIError{Path: path, Field: field, Expected: expected, Actual: actual}
	}
	if t.serviceVersion != layoutServiceVersion {
		return abi("layout service_version", fmt.Sprint(layoutServiceVersion), fmt.Sprint(t.serviceVersion))
	}
	if t.sizeOfTable != uint32(unsafe.Sizeof(layoutTable{})) {
		return abi("layout size_of_table", fmt.Sprint(unsafe.Sizeof(layoutTable{})), fmt.Sprint(t.sizeOfTable))
	}
	if t.alignOfTable != uint32(unsafe.Alignof(layoutTable{})) {
		return abi("layout align_of_table", fmt.Sprint(unsafe.Alignof(layoutTable{})), fmt.Sprint(t.alignOfTable))
	}
	if t.sizeOfStyleRecord != uint32(unsafe.Sizeof(LayoutStyleRecord{})) {
		return abi("layout size_of_style_record", fmt.Sprint(unsafe.Sizeof(LayoutStyleRecord{})), fmt.Sprint(t.sizeOfStyleRecord))
	}
	if t.sizeOfLayoutRecord != uint32(unsafe.Sizeof(LayoutRecord{})) {
		return abi("layout size_of_layout_record", fmt.Sprint(unsafe.Sizeof(LayoutRecord{})), fmt.Sprint(t.sizeOfLayoutRecord))
	}
	if t.sizeOfAvail != uint32(unsafe.Sizeof(LayoutAvail{})) {
		return abi("layout size_of_avail", fmt.Sprint(unsafe.Sizeof(LayoutAvail{})), fmt.Sprint(t.sizeOfAvail))
	}
	if t.sizeOfAvailSize != uint32(unsafe.Sizeof(LayoutAvailSize{})) {
		return abi("layout size_of_avail_size", fmt.Sprint(unsafe.Sizeof(LayoutAvailSize{})), fmt.Sprint(t.sizeOfAvailSize))
	}
	if t.sizeOfMeasureRequest != uint32(unsafe.Sizeof(measureRequestRec{})) {
		return abi("layout size_of_measure_request", fmt.Sprint(unsafe.Sizeof(measureRequestRec{})), fmt.Sprint(t.sizeOfMeasureRequest))
	}
	if t.alignOfMeasureRequest != uint32(unsafe.Alignof(measureRequestRec{})) {
		return abi("layout align_of_measure_request", fmt.Sprint(unsafe.Alignof(measureRequestRec{})), fmt.Sprint(t.alignOfMeasureRequest))
	}
	if t.sizeOfMeasureResponse != uint32(unsafe.Sizeof(measureResponseRec{})) {
		return abi("layout size_of_measure_response", fmt.Sprint(unsafe.Sizeof(measureResponseRec{})), fmt.Sprint(t.sizeOfMeasureResponse))
	}
	if t.alignOfMeasureResponse != uint32(unsafe.Alignof(measureResponseRec{})) {
		return abi("layout align_of_measure_response", fmt.Sprint(unsafe.Alignof(measureResponseRec{})), fmt.Sprint(t.alignOfMeasureResponse))
	}
	slots := [...]uintptr{
		t.engineCreate, t.engineReset, t.engineDispose, t.nodeCreate,
		t.nodeSetStyle, t.nodeSetMeasure, t.nodeRemoveSubtree, t.compute,
		t.nodeLayout, t.nodeParent, t.nodeChildCount, t.nodeChild, t.dump,
		t.setTrampoline,
	}
	names := [...]string{
		"engine_create", "engine_reset", "engine_dispose", "node_create",
		"node_set_style", "node_set_measure", "node_remove_subtree", "compute",
		"node_layout", "node_parent", "node_child_count", "node_child", "dump",
		"set_trampoline",
	}
	for i, slot := range slots {
		if slot == 0 {
			return &CapabilityError{Path: path, Detail: "layout table slot " + names[i] + " is null"}
		}
	}
	return nil
}

// measureRequestRec mirrors GpuiMeasureRequest (native-owned during the
// trampoline call): engine @0, node @8, callbackToken @16, computeToken @24,
// knownWidthPresent @32, knownHeightPresent @36, availWidthTag @40,
// availHeightTag @44, knownWidthBits @48, knownHeightBits @52,
// availWidthBits @56, availHeightBits @60, reserved @64; size 72, align 8.
type measureRequestRec struct {
	engine             uint64
	node               uint64
	callbackToken      uint64
	computeToken       uint64
	knownWidthPresent  uint32
	knownHeightPresent uint32
	availWidthTag      uint32
	availHeightTag     uint32
	knownWidthBits     uint32
	knownHeightBits    uint32
	availWidthBits     uint32
	availHeightBits    uint32
	reserved0          uint32
	reserved1          uint32
}

// measureResponseRec mirrors GpuiMeasureResponse: status @0, widthBits @4,
// heightBits @8; size 12, align 4.
type measureResponseRec struct {
	status     int32
	widthBits  uint32
	heightBits uint32
}

// ---------------------------------------------------------------------------
// Layout service and engines
// ---------------------------------------------------------------------------

// LayoutService is the typed accessor for the native layout service of a
// loaded Library. It holds a Go-owned copy of the validated service table.
// It is safe for concurrent use.
type LayoutService struct {
	lib   *Library
	table layoutTable
}

// Layout returns the layout service of the loaded artifact. The capability
// bit ("layout-taffy-0-13-0") must be present and the service table in
// reserved slot 1 must validate (service version and record sizes) before
// any call is made. An artifact without the layout capability (a ticket02
// artifact) fails with ErrMissingCapability here, not at Load.
func (l *Library) Layout() (*LayoutService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capLayoutTaffy == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capLayoutTaffy,
			Actual:   l.identity.Capabilities,
			Detail:   "layout-taffy-0-13-0 capability bit missing",
		}
	}
	slot := l.table.reserved[1]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "layout service table (reserved slot 1) is null"}
	}
	table := *layoutTableFromSlot(slot) // copy out of DLL memory
	if err := validateLayoutTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &LayoutService{lib: l, table: table}, nil
}

// EngineHandle is a validated opaque native engine handle. It embeds the
// registry slot and slot generation; it stays valid across engine resets and
// becomes stale after dispose (or slot reuse).
type EngineHandle uint64

// NodeHandle is a validated opaque native node handle. It embeds the owning
// engine's node generation; it becomes stale after engine reset or subtree
// removal.
type NodeHandle uint64

// LayoutEngine is a Go-side wrapper around one native layout engine with a
// busy guard: while a Compute runs, every other operation on the same engine
// is rejected with ErrLayoutEngineBusy before native entry (the contract's
// "reject same-engine re-entry before native entry"). LayoutEngine is not
// safe for concurrent use; layout runs on the owning foreground thread.
type LayoutEngine struct {
	svc    *LayoutService
	handle EngineHandle
	busy   atomic.Bool
}

// Handle returns the native engine handle.
func (e *LayoutEngine) Handle() EngineHandle { return e.handle }

// CreateEngine creates a fresh empty native layout engine (generation 1).
func (s *LayoutService) CreateEngine() (*LayoutEngine, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	var handle uint64
	code, err := callLayoutEngineCreate(s.table.engineCreate, &handle)
	if err != nil {
		return nil, err
	}
	if code != layoutStatusOK {
		return nil, layoutStatusError(code, "layout_engine_create")
	}
	return &LayoutEngine{svc: s, handle: EngineHandle(handle)}, nil
}

// checkIdle rejects operations while a Compute on this engine is running
// (before native entry).
func (e *LayoutEngine) checkIdle(op string) error {
	if e.busy.Load() {
		return layoutStatusError(layoutErrEngineBusy, op+" rejected: engine busy")
	}
	return nil
}

// Reset clears the engine's tree and bumps its node generation: every node
// handle of the engine becomes stale, the engine handle stays valid, and the
// failed flag clears. Measure tokens registered for the old generation are
// purged from the Go registry.
func (e *LayoutEngine) Reset() error {
	if err := e.checkIdle("layout_engine_reset"); err != nil {
		return err
	}
	code, err := callLayoutEngineOp(e.svc.table.engineReset, uint64(e.handle))
	if err != nil {
		return err
	}
	if code != layoutStatusOK {
		return layoutStatusError(code, "layout_engine_reset")
	}
	purgeMeasureEntries(e.handle)
	return nil
}

// Dispose frees the engine; its handle becomes stale. In-flight native
// calls hold their own references, so actual destruction is deferred until
// they return (the contract's deferred native destruction).
func (e *LayoutEngine) Dispose() error {
	if err := e.checkIdle("layout_engine_dispose"); err != nil {
		return err
	}
	code, err := callLayoutEngineOp(e.svc.table.engineDispose, uint64(e.handle))
	if err != nil {
		return err
	}
	if code != layoutStatusOK {
		return layoutStatusError(code, "layout_engine_dispose")
	}
	purgeMeasureEntries(e.handle)
	return nil
}

// CreateNode creates a node with the given style, attaching children (which
// must be unattached nodes of this engine). All definite lengths in style
// must already be device pixels; invalid tags/enumerants are rejected by the
// native side with ErrLayoutBadStyle.
func (e *LayoutEngine) CreateNode(style *LayoutStyleRecord, children ...NodeHandle) (NodeHandle, error) {
	if err := e.checkIdle("layout_node_create"); err != nil {
		return 0, err
	}
	if style == nil {
		return 0, layoutStatusError(layoutErrNullArg, "layout_node_create: nil style record")
	}
	var handle uint64
	code, err := callLayoutNodeCreate(
		e.svc.table.nodeCreate, uint64(e.handle), style, children, &handle)
	if err != nil {
		return 0, err
	}
	if code != layoutStatusOK {
		return 0, layoutStatusError(code, "layout_node_create")
	}
	return NodeHandle(handle), nil
}

// SetStyle replaces a node's style (validated and translated natively).
func (e *LayoutEngine) SetStyle(node NodeHandle, style *LayoutStyleRecord) error {
	if err := e.checkIdle("layout_node_set_style"); err != nil {
		return err
	}
	if style == nil {
		return layoutStatusError(layoutErrNullArg, "layout_node_set_style: nil style record")
	}
	code, err := callLayoutNodeSetStyle(e.svc.table.nodeSetStyle, uint64(e.handle), uint64(node), style)
	if err != nil {
		return err
	}
	if code != layoutStatusOK {
		return layoutStatusError(code, "layout_node_set_style")
	}
	return nil
}

// SetMeasure marks a node as measured: during Compute, the native measure
// closure builds a request and calls the process-global Go trampoline, which
// resolves callbackToken through the Go registry (RegisterMeasure).
func (e *LayoutEngine) SetMeasure(node NodeHandle, callbackToken uint64) error {
	if err := e.checkIdle("layout_node_set_measure"); err != nil {
		return err
	}
	code, err := callLayoutNodeSetMeasure(e.svc.table.nodeSetMeasure, uint64(e.handle), uint64(node), callbackToken)
	if err != nil {
		return err
	}
	if code != layoutStatusOK {
		return layoutStatusError(code, "layout_node_set_measure")
	}
	return nil
}

// RemoveSubtree removes a node and all of its descendants; their handles
// become stale.
func (e *LayoutEngine) RemoveSubtree(node NodeHandle) error {
	if err := e.checkIdle("layout_node_remove_subtree"); err != nil {
		return err
	}
	code, err := callLayoutNodeRemoveSubtree(e.svc.table.nodeRemoveSubtree, uint64(e.handle), uint64(node))
	if err != nil {
		return err
	}
	if code != layoutStatusOK {
		return layoutStatusError(code, "layout_node_remove_subtree")
	}
	return nil
}

// Compute lays the subtree rooted at root out under the given available
// space (definite values are device pixels). computeToken is an opaque
// caller token propagated to every measure request of this compute; it must
// be unique among concurrent computes so recovered panic values can be
// attributed (see LayoutStatusError.Panic). The returned flags carry bit 0
// when measured callbacks were invoked.
//
// While Compute runs, the engine is busy: nested operations on the same
// engine (including from inside a measure function) fail with
// ErrLayoutEngineBusy. Nested computes on a different engine are allowed.
func (e *LayoutEngine) Compute(root NodeHandle, available LayoutAvailSize, computeToken uint64) (uint32, error) {
	if e.svc.lib.released.Load() {
		return 0, ErrClosed
	}
	if !e.busy.CompareAndSwap(false, true) {
		return 0, layoutStatusError(layoutErrEngineBusy,
			"layout_compute rejected: nested same-engine compute (Go busy guard, before native entry)")
	}
	defer e.busy.Store(false)
	// The out-flags record comes from a pool: native code writes it AFTER
	// measure trampolines have run, and a callback can move the goroutine
	// stack (stack growth/shrink while Go code runs in the trampoline),
	// which would invalidate a stack-addressed out pointer. Pooled objects
	// are heap allocated and Go's heap is non-moving.
	flags := computeFlagsPool.Get().(*uint32)
	defer computeFlagsPool.Put(flags)
	code, err := callLayoutCompute(
		e.svc.table.compute, uint64(e.handle), uint64(root), &available, computeToken, flags)
	if err != nil {
		return 0, err
	}
	if code != layoutStatusOK {
		statusErr := layoutStatusError(code, "layout_compute")
		if code == layoutErrCallbackFailed {
			// Attach the recovered Go panic value (if any) so ordinary Go
			// code can inspect or re-raise it after the native call
			// returned and engine bookkeeping was restored.
			if v, ok := takeMeasurePanic(computeToken); ok {
				statusErr.Panic = v
			}
		}
		return 0, statusErr
	}
	return *flags, nil
}

// NodeLayout returns a node's computed layout (device pixels, unrounded).
func (e *LayoutEngine) NodeLayout(node NodeHandle) (LayoutRecord, error) {
	if err := e.checkIdle("layout_node_layout"); err != nil {
		return LayoutRecord{}, err
	}
	var record LayoutRecord
	code, err := callLayoutNodeLayout(e.svc.table.nodeLayout, uint64(e.handle), uint64(node), &record)
	if err != nil {
		return LayoutRecord{}, err
	}
	if code != layoutStatusOK {
		return LayoutRecord{}, layoutStatusError(code, "layout_node_layout")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(LayoutRecord{})) {
		return LayoutRecord{}, &ABIError{
			Field:    "layout record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(LayoutRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return record, nil
}

// NodeParent returns the node's parent, or 0 when the node is the root.
func (e *LayoutEngine) NodeParent(node NodeHandle) (NodeHandle, error) {
	if err := e.checkIdle("layout_node_parent"); err != nil {
		return 0, err
	}
	var parent uint64
	code, err := callLayoutNodeParent(e.svc.table.nodeParent, uint64(e.handle), uint64(node), &parent)
	if err != nil {
		return 0, err
	}
	if code != layoutStatusOK {
		return 0, layoutStatusError(code, "layout_node_parent")
	}
	return NodeHandle(parent), nil
}

// NodeChildCount returns the number of the node's children.
func (e *LayoutEngine) NodeChildCount(node NodeHandle) (uint32, error) {
	if err := e.checkIdle("layout_node_child_count"); err != nil {
		return 0, err
	}
	var count uint32
	code, err := callLayoutNodeChildCount(e.svc.table.nodeChildCount, uint64(e.handle), uint64(node), &count)
	if err != nil {
		return 0, err
	}
	if code != layoutStatusOK {
		return 0, layoutStatusError(code, "layout_node_child_count")
	}
	return count, nil
}

// NodeChild returns the node's index'th child.
func (e *LayoutEngine) NodeChild(node NodeHandle, index uint32) (NodeHandle, error) {
	if err := e.checkIdle("layout_node_child"); err != nil {
		return 0, err
	}
	var child uint64
	code, err := callLayoutNodeChild(e.svc.table.nodeChild, uint64(e.handle), uint64(node), index, &child)
	if err != nil {
		return 0, err
	}
	if code != layoutStatusOK {
		return 0, layoutStatusError(code, "layout_node_child")
	}
	return NodeHandle(child), nil
}

// Dump copies the preorder walk of the subtree rooted at root (root first,
// then children in order) into Go-owned storage. capacity is the number of
// records the caller is prepared to receive; when it is too small, the
// required size is fetched and the call retried once. Returns the node
// handles and layout records in preorder order.
func (e *LayoutEngine) Dump(root NodeHandle, capacity uint32) ([]NodeHandle, []LayoutRecord, error) {
	if err := e.checkIdle("layout_dump"); err != nil {
		return nil, nil, err
	}
	want := capacity
	ids := make([]NodeHandle, want)
	recs := make([]LayoutRecord, want)
	var count uint32
	code, err := callLayoutDump(e.svc.table.dump, uint64(e.handle), uint64(root), ids, recs, want, &count)
	if err != nil {
		return nil, nil, err
	}
	if code == layoutErrCapacity {
		// The native side reported the required count; retry once with it.
		want = count
		ids = make([]NodeHandle, want)
		recs = make([]LayoutRecord, want)
		code, err = callLayoutDump(e.svc.table.dump, uint64(e.handle), uint64(root), ids, recs, want, &count)
		if err != nil {
			return nil, nil, err
		}
	}
	if code != layoutStatusOK {
		return nil, nil, layoutStatusError(code, "layout_dump")
	}
	for i := uint32(0); i < count; i++ {
		if recs[i].RecordSize != uint32(unsafe.Sizeof(LayoutRecord{})) {
			return nil, nil, &ABIError{
				Field:    "layout record_size",
				Expected: fmt.Sprint(unsafe.Sizeof(LayoutRecord{})),
				Actual:   fmt.Sprint(recs[i].RecordSize),
			}
		}
	}
	return ids[:count], recs[:count], nil
}

// ---------------------------------------------------------------------------
// Measure registry and trampoline
// ---------------------------------------------------------------------------

// AvailMode is the available-space mode of one axis in a measure request.
type AvailMode uint32

// Available-space modes (mirror of the GpuiAvail tags).
const (
	AvailModeDefinite   AvailMode = 0
	AvailModeMinContent AvailMode = 1
	AvailModeMaxContent AvailMode = 2
)

// AvailSpace is one axis of the available space handed to a measure
// function: a definite device-pixel amount or a min/max-content mode.
type AvailSpace struct {
	Mode     AvailMode
	Definite float32
}

// GoMeasureRequest is the value form of a native measure request (device
// pixels throughout). Known dimensions are optional per axis and independent
// of the available-space modes.
type GoMeasureRequest struct {
	// Engine and node identity, and the caller tokens involved.
	Engine        EngineHandle
	Node          NodeHandle
	CallbackToken uint64
	ComputeToken  uint64
	// Known (fixed) dimensions; valid only when the Present flag is set.
	KnownWidth         float32
	KnownHeight        float32
	KnownWidthPresent  bool
	KnownHeightPresent bool
	// Available space per axis.
	AvailWidth  AvailSpace
	AvailHeight AvailSpace
}

// GoMeasureResponse is the measured size of a measured node, in device
// pixels. The higher-level adapter applies the pinned measurement snapping
// (clamp logical >= 0, multiply by scale, ceil) before returning; a failure
// is signalled by panicking inside the measure function (the trampoline
// recovers it and the native side latches the failure).
type GoMeasureResponse struct {
	Width  float32
	Height float32
}

// MeasureFunc is the Go measurement function invoked synchronously, on the
// calling thread, from inside layout_compute. It must not block and must
// not call back into the same engine (same-engine re-entry is rejected with
// ErrLayoutEngineBusy before native entry). Panics propagate to the
// trampoline's recover and latch the compute's failure.
type MeasureFunc func(req GoMeasureRequest) GoMeasureResponse

// measureEntry is one registered measure function, bound to the engine
// identity (slot + slot generation) it was registered for. Requests whose
// engine handle differs fail the trampoline's generation check (status 2).
type measureEntry struct {
	engine EngineHandle
	fn     MeasureFunc
}

// measureRegistry maps callback tokens to measure entries. sync.Map: the
// trampoline looks up without holding a lock across the callback body (the
// contract's "no mutable global borrow held across callback execution").
var measureRegistry sync.Map // map[uint64]measureEntry

// measureTokenSeq hands out unique callback tokens.
var measureTokenSeq atomic.Uint64

// measurePanics carries recovered panic values keyed by compute token, from
// the trampoline to the owning Compute call.
var measurePanics sync.Map // map[uint64]any

// computeFlagsPool hands out the out-flags records of layout_compute. A
// pooled allocation is guaranteed to live on the (non-moving) Go heap:
// native code writes the flags AFTER measure trampolines have run, and a
// callback can move the goroutine stack (stack growth or shrink while Go
// code runs inside the trampoline), which would silently invalidate a
// stack-addressed out pointer. The local reference keeps the record rooted
// for the whole native call.
var computeFlagsPool = sync.Pool{New: func() any { return new(uint32) }}

func takeMeasurePanic(computeToken uint64) (any, bool) {
	if v, ok := measurePanics.LoadAndDelete(computeToken); ok {
		return v, true
	}
	return nil, false
}

// RegisterMeasure registers fn and returns an opaque callback token for
// SetMeasure. Tokens are generation-checked: the registry entry remembers
// the engine identity (slot + slot generation), and a token used against a
// different engine (or after the entry was purged by Reset/Dispose) fails
// with the trampoline's invalid-token status.
func (e *LayoutEngine) RegisterMeasure(fn MeasureFunc) uint64 {
	token := measureTokenSeq.Add(1)
	measureRegistry.Store(token, measureEntry{engine: e.handle, fn: fn})
	return token
}

// purgeMeasureEntries removes every registry entry bound to the given
// engine identity (after reset bumped the node generation, or dispose).
// Entries are only removed here — never while a native call is running
// (Reset/Dispose are rejected while the engine is busy).
func purgeMeasureEntries(engine EngineHandle) {
	measureRegistry.Range(func(key, value any) bool {
		if entry, ok := value.(measureEntry); ok && entry.engine == engine {
			measureRegistry.Delete(key)
		}
		return true
	})
}

// RegisterMeasureTrampoline installs the process-global Go measure
// trampoline into the native library (layout_set_trampoline). The Windows
// callback is created with syscall.NewCallback exactly once per process;
// the call is idempotent. It must run before any measured compute. On
// non-Windows hosts it returns ErrUnsupportedTarget.
func (s *LayoutService) RegisterMeasureTrampoline() error {
	if s.lib.released.Load() {
		return ErrClosed
	}
	return layoutInstallTrampoline(s.table.setTrampoline)
}
