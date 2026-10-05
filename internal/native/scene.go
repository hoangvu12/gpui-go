// Scene kernel service access (ticket08).
//
// This file is the Go mirror of the native scene kernel service documented
// in reference/native/SCENE_ABI.md and implemented in
// reference/native/src/scene.rs: the pinned gpui-CE scene kernel (bounds
// tree draw orders, layers, order floors, finish/sort, batching,
// filter-target planning, paired surface opacity, replay) behind
// `#[repr(C)]` records and an `extern "system"` function table. The
// service table lives in reserved slot 3 of the bootstrap ABI table and
// is advertised with capability bit 3 ("scene-kernel-v1").
//
// Units: the scene is device-pixel space (ScaledPixels). Every f32 field
// of every record crosses the ABI as IEEE-754 bits; use the Bit()
// helpers (or math.Float32bits) when filling records and the Float()
// accessors when reading them back. Logical-pixel conversion, snapping
// and color conversion belong to the higher-level Go adapter (gpui).
//
// Records are plain fixed-width Go structs whose layout is asserted
// against the native self-check fields at service-fetch time
// (validateSceneTable). No Go pointer is retained across calls; the
// native side copies every record at insert and back into caller buffers
// at dump.

package native

import (
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Status codes and errors
// ---------------------------------------------------------------------------

// Native scene service status codes (mirror scene_status in
// reference/native/src/scene.rs). 0 is success; negative values are
// caller/argument errors; 100+ are internal failures.
const (
	sceneStatusOK       int32 = 0
	sceneErrStaleHandle int32 = -1
	sceneErrBadHandle   int32 = -2
	sceneErrNullArg     int32 = -3
	sceneErrBadValue    int32 = -4
	sceneErrNotFinished int32 = -5
	sceneErrCapacity    int32 = -6
	sceneErrSceneFailed int32 = -7
	sceneErrLimit       int32 = -8
	sceneErrPanic       int32 = 101
)

// sceneServiceVersion mirrors GPUI_GO_SCENE_SERVICE_VERSION.
// Version 2 added the three sprite primitive classes (ticket10);
// version 3 (ticket16) added the path primitive class, the path dump
// and the pinned-PathBuilder tessellation entry.
const sceneServiceVersion uint32 = 3

func sceneStatusName(code int32) string {
	switch code {
	case sceneStatusOK:
		return "ok"
	case sceneErrStaleHandle:
		return "stale handle"
	case sceneErrBadHandle:
		return "bad handle"
	case sceneErrNullArg:
		return "null argument"
	case sceneErrBadValue:
		return "bad value (record validation, unknown kind, bad range)"
	case sceneErrNotFinished:
		return "scene not finished"
	case sceneErrCapacity:
		return "dump capacity too small"
	case sceneErrSceneFailed:
		return "scene failed (panic contained; clear or dispose required)"
	case sceneErrLimit:
		return "scene capacity limit exceeded"
	case sceneErrPanic:
		return "native panic contained"
	default:
		return "unknown scene status"
	}
}

// Scene service sentinel errors, distinguishable with errors.Is.
var (
	// ErrSceneStaleHandle: the handle encoded a past generation (scene
	// disposed or slot reused).
	ErrSceneStaleHandle = errors.New("native: scene handle is stale")
	// ErrSceneBadHandle: the handle never existed or is malformed.
	ErrSceneBadHandle = errors.New("native: scene handle is invalid")
	// ErrSceneNullArg: a required pointer argument was null.
	ErrSceneNullArg = errors.New("native: scene argument was null")
	// ErrSceneBadValue: a record failed validation, or a value was
	// malformed (unknown dump kind, bad replay range, self-replay).
	ErrSceneBadValue = errors.New("native: scene value rejected")
	// ErrSceneNotFinished: a plan/requirements/dump read before finish.
	ErrSceneNotFinished = errors.New("native: scene not finished")
	// ErrSceneCapacity: dump capacity was smaller than the content.
	ErrSceneCapacity = errors.New("native: scene dump capacity too small")
	// ErrSceneFailed: the scene is marked failed after a contained
	// native panic; clear or dispose is required.
	ErrSceneFailed = errors.New("native: scene failed")
	// ErrSceneLimit: an insert exceeded a scene capacity bound.
	ErrSceneLimit = errors.New("native: scene capacity limit exceeded")
	// ErrScenePanic: a native panic was contained by catch_unwind.
	ErrScenePanic = errors.New("native: scene panic contained")
)

// SceneStatusError reports a non-zero scene service status code.
type SceneStatusError struct {
	Code   int32
	Name   string
	Detail string
}

func (e *SceneStatusError) Error() string {
	detail := e.Detail
	if detail != "" {
		detail = ": " + detail
	}
	return ErrBadStatus.Error() + ": scene status " + fmt.Sprint(e.Code) + " (" + e.Name + ")" + detail
}

func (e *SceneStatusError) Unwrap() error {
	switch e.Code {
	case sceneErrStaleHandle:
		return ErrSceneStaleHandle
	case sceneErrBadHandle:
		return ErrSceneBadHandle
	case sceneErrNullArg:
		return ErrSceneNullArg
	case sceneErrBadValue:
		return ErrSceneBadValue
	case sceneErrNotFinished:
		return ErrSceneNotFinished
	case sceneErrCapacity:
		return ErrSceneCapacity
	case sceneErrSceneFailed:
		return ErrSceneFailed
	case sceneErrLimit:
		return ErrSceneLimit
	case sceneErrPanic:
		return ErrScenePanic
	default:
		return ErrBadStatus
	}
}

func sceneStatusError(code int32, detail string) *SceneStatusError {
	return &SceneStatusError{Code: code, Name: sceneStatusName(code), Detail: detail}
}

// ---------------------------------------------------------------------------
// Record mirrors
// ---------------------------------------------------------------------------

// SceneBounds mirrors GpuiGoSceneBounds: x, y, w, h as f32 bits (device
// pixels). 16 bytes, alignment 4.
type SceneBounds struct {
	X uint32
	Y uint32
	W uint32
	H uint32
}

// SceneBoundsOf packs a device-pixel rectangle into the ABI record.
func SceneBoundsOf(x, y, w, h float32) SceneBounds {
	return SceneBounds{X: math.Float32bits(x), Y: math.Float32bits(y), W: math.Float32bits(w), H: math.Float32bits(h)}
}

// Rect unpacks the record into its components.
func (b SceneBounds) Rect() (x, y, w, h float32) {
	return math.Float32frombits(b.X), math.Float32frombits(b.Y), math.Float32frombits(b.W), math.Float32frombits(b.H)
}

// SceneColor mirrors GpuiGoSceneColor: tag plus the HSL-with-alpha
// components as f32 bits. Tag 0 is the only supported value (the pinned
// Background's solid form). 20 bytes, alignment 4.
type SceneColor struct {
	// Tag is 0 (solid).
	Tag uint32
	// H is the hue in [0, 1] (degrees / 360), f32 bits.
	H uint32
	// S is the saturation in [0, 1], f32 bits.
	S uint32
	// L is the lightness in [0, 1], f32 bits.
	L uint32
	// A is the alpha in [0, 1], f32 bits.
	A uint32
}

// SceneColorOf packs an HSLA color (h in degrees/360) into the ABI record.
func SceneColorOf(h, s, l, a float32) SceneColor {
	return SceneColor{Tag: 0, H: math.Float32bits(h), S: math.Float32bits(s), L: math.Float32bits(l), A: math.Float32bits(a)}
}

// HSLA unpacks the record into its components.
func (c SceneColor) HSLA() (h, s, l, a float32) {
	return math.Float32frombits(c.H), math.Float32frombits(c.S), math.Float32frombits(c.L), math.Float32frombits(c.A)
}

// Path command kind tags of the tessellation entry (the pinned
// PathBuilder public API surface; ticket16).
const (
	ScenePathCmdMoveTo        uint32 = 0
	ScenePathCmdLineTo        uint32 = 1
	ScenePathCmdCurveTo       uint32 = 2
	ScenePathCmdCubicBezierTo uint32 = 3
	ScenePathCmdArcTo         uint32 = 4
	ScenePathCmdRelativeArcTo uint32 = 5
	ScenePathCmdPolygon       uint32 = 6
	ScenePathCmdClose         uint32 = 7
	ScenePathCmdStyle         uint32 = 8
	ScenePathCmdDash          uint32 = 9
	ScenePathCmdTranslate     uint32 = 10
	ScenePathCmdScale         uint32 = 11
	ScenePathCmdRotate        uint32 = 12
	ScenePathCmdTransform     uint32 = 13
)

// Path style tags of the STYLE command's first data word (the pinned
// PathStyle variants).
const (
	ScenePathStyleFill   uint32 = 0
	ScenePathStyleStroke uint32 = 1
)

// PathCommandRecord mirrors GpuiGoPathCommandRecord: one path-builder
// command — a kind tag, a 12-word data area (floats as f32 bits, small
// enums as tags) and a record-size self-check. 56 bytes, alignment 4.
//
// Data areas by kind (word indices): MOVE_TO/LINE_TO/TRANSLATE
// data[0..2] = x,y; CURVE_TO data[0..2] = to, data[2..4] = ctrl;
// CUBIC_BEZIER_TO data[0..6] = to, control_a, control_b;
// ARC_TO/RELATIVE_ARC_TO data[0..2] = radii, data[2] = x rotation in
// DEGREES, data[3] = large_arc (0/1), data[4] = sweep (0/1),
// data[5..7] = to; POLYGON data[0] = first point index into the call's
// point pool, data[1] = point count, data[2] = closed (0/1); CLOSE all
// zero; STYLE fill data[0] = 0, data[1] = tolerance, data[2] = fill
// rule (0 even-odd, 1 non-zero), data[3] = sweep orientation (0
// vertical, 1 horizontal), data[4] = handle intersections (0/1);
// STYLE stroke data[0] = 1, data[1] = line width, data[2] = start cap
// (0 butt, 1 square, 2 round), data[3] = end cap, data[4] = line join
// (0 miter, 1 round, 2 bevel), data[5] = miter limit; DASH data[0] =
// first word index of the dash lengths, data[1] = word count;
// SCALE/ROTATE data[0] = factor/degrees; TRANSFORM data[0..6] = the
// row-major 2x2 matrix then translation [m00, m01, m10, m11, tx, ty].
type PathCommandRecord struct {
	Kind       uint32
	Data       [12]uint32
	RecordSize uint32
}

// NewPathCommandRecord returns a zeroed command with the self-check
// field set.
func NewPathCommandRecord(kind uint32) PathCommandRecord {
	return PathCommandRecord{
		Kind:       kind,
		RecordSize: uint32(unsafe.Sizeof(PathCommandRecord{})),
	}
}

// SetXY stores an (x, y) pair (f32 bits) at data[from..from+2].
func (cmd *PathCommandRecord) SetXY(from int, x, y float32) {
	cmd.Data[from] = math.Float32bits(x)
	cmd.Data[from+1] = math.Float32bits(y)
}

// SetFloat stores one f32-bits word at data[index].
func (cmd *PathCommandRecord) SetFloat(index int, value float32) {
	cmd.Data[index] = math.Float32bits(value)
}

// ScenePathVertexRecord mirrors GpuiGoScenePathVertexRecord: one
// tessellated path vertex (the pinned PathVertex's xy and st
// positions). 16 bytes, alignment 4.
type ScenePathVertexRecord struct {
	XYX uint32
	XYY uint32
	STU uint32
	STV uint32
}

// ScenePathVertexOf packs one vertex into the ABI record.
func ScenePathVertexOf(x, y, s, t float32) ScenePathVertexRecord {
	return ScenePathVertexRecord{
		XYX: math.Float32bits(x),
		XYY: math.Float32bits(y),
		STU: math.Float32bits(s),
		STV: math.Float32bits(t),
	}
}

// ScenePathRecord mirrors GpuiGoScenePathRecord: the path primitive
// record (order must be 0 on insert). 64 bytes, alignment 4. The
// vertices travel as the parallel bulk array of InsertPath and
// PathDump.
type ScenePathRecord struct {
	Order       uint32
	Bounds      SceneBounds
	ContentMask SceneBounds
	Color       SceneColor
	VertexCount uint32
	RecordSize  uint32
}

// NewScenePathRecord returns a zeroed record with the self-check field
// set.
func NewScenePathRecord() ScenePathRecord {
	return ScenePathRecord{
		RecordSize: uint32(unsafe.Sizeof(ScenePathRecord{})),
	}
}

// SceneQuadRecord mirrors GpuiGoSceneQuadRecord: 132 bytes, alignment 4.
// Order must be 0 on insert; the kernel assigns it (mirrored in dumps).
type SceneQuadRecord struct {
	Order              uint32
	Bounds             SceneBounds
	ContentMask        SceneBounds
	Background         SceneColor
	BorderColor        SceneColor
	CornerRadii        [4]uint32 // top-left, top-right, bottom-right, bottom-left
	BorderWidths       [4]uint32 // top, right, bottom, left
	BorderStyle        uint32    // 0 solid, 1 dashed
	BorderDashedLength uint32
	BorderDashedGap    uint32
	CornerSmoothing    uint32
	Padding            uint32
	RecordSize         uint32
}

// NewSceneQuadRecord returns a validated empty quad record (every field
// zeroed except the record-size self-check).
func NewSceneQuadRecord() SceneQuadRecord {
	return SceneQuadRecord{RecordSize: uint32(unsafe.Sizeof(SceneQuadRecord{}))}
}

// SceneShadowRecord mirrors GpuiGoSceneShadowRecord: 120 bytes,
// alignment 4.
type SceneShadowRecord struct {
	Order              uint32
	BlurRadius         uint32
	Bounds             SceneBounds
	CornerRadii        [4]uint32
	ContentMask        SceneBounds
	Color              SceneColor
	ElementBounds      SceneBounds
	ElementCornerRadii [4]uint32
	Inset              uint32 // 0 drop, 1 inset
	CornerSmoothing    uint32
	RecordSize         uint32
}

// NewSceneShadowRecord returns a validated empty shadow record.
func NewSceneShadowRecord() SceneShadowRecord {
	return SceneShadowRecord{RecordSize: uint32(unsafe.Sizeof(SceneShadowRecord{}))}
}

// SceneUnderlineRecord mirrors GpuiGoSceneUnderlineRecord: 72 bytes,
// alignment 4.
type SceneUnderlineRecord struct {
	Order       uint32
	Padding     uint32
	Bounds      SceneBounds
	ContentMask SceneBounds
	Color       SceneColor
	Thickness   uint32
	Wavy        uint32 // 0 straight, 1 wavy
	RecordSize  uint32
}

// NewSceneUnderlineRecord returns a validated empty underline record.
func NewSceneUnderlineRecord() SceneUnderlineRecord {
	return SceneUnderlineRecord{RecordSize: uint32(unsafe.Sizeof(SceneUnderlineRecord{}))}
}

// SceneFilterRecord mirrors GpuiGoSceneFilterRecord (backdrop filters and
// filter boundaries): 88 bytes, alignment 4.
type SceneFilterRecord struct {
	Order           uint32
	Bounds          SceneBounds
	ContentMask     SceneBounds
	CornerRadii     [4]uint32
	CornerSmoothing uint32
	BlurRadii       [4]uint32
	FilterCount     uint32
	Opacity         uint32
	IsStart         uint32 // boundary: 1 start, 0 end; backdrops pass 0
	RecordSize      uint32
}

// NewSceneFilterRecord returns a validated empty filter record.
func NewSceneFilterRecord() SceneFilterRecord {
	return SceneFilterRecord{RecordSize: uint32(unsafe.Sizeof(SceneFilterRecord{}))}
}

// SceneSurfaceRecord mirrors GpuiGoSceneSurfaceRecord: 56 bytes,
// alignment 4.
type SceneSurfaceRecord struct {
	Order          uint32
	Bounds         SceneBounds
	ContentMask    SceneBounds
	SourceTag      uint32
	SourceReserved [3]uint32
	RecordSize     uint32
}

// NewSceneSurfaceRecord returns a validated empty surface record.
func NewSceneSurfaceRecord() SceneSurfaceRecord {
	return SceneSurfaceRecord{RecordSize: uint32(unsafe.Sizeof(SceneSurfaceRecord{}))}
}

// SceneTileRecord mirrors GpuiGoSceneTileRecord (the pinned AtlasTile
// sub-record of the sprite records): 32 bytes, alignment 4.
type SceneTileRecord struct {
	TextureIndex uint32
	TextureKind  uint32 // 0 monochrome, 1 polychrome, 2 subpixel
	TileID       uint32
	Padding      uint32
	BoundsX      int32
	BoundsY      int32
	BoundsW      int32
	BoundsH      int32
}

// Scene sprite texture-kind tags (the pinned AtlasTextureKind mapping of
// the sprite classes).
const (
	SceneSpriteTextureMonochrome uint32 = 0
	SceneSpriteTexturePolychrome uint32 = 1
	SceneSpriteTextureSubpixel   uint32 = 2
)

// SceneSpriteRecord mirrors GpuiGoSceneSpriteRecord (the pinned
// MonochromeSprite/SubpixelSprite field set): 120 bytes, alignment 4.
type SceneSpriteRecord struct {
	Order          uint32
	Padding        uint32
	Bounds         SceneBounds
	ContentMask    SceneBounds
	Color          SceneColor
	Tile           SceneTileRecord
	Transformation [6]uint32 // rotation-scale 2x2 then translation xy, f32 bits
	RecordSize     uint32
}

// NewSceneSpriteRecord returns a validated empty sprite record with the
// pinned unit transformation.
func NewSceneSpriteRecord() SceneSpriteRecord {
	return SceneSpriteRecord{
		Transformation: [6]uint32{
			unitF32Bits, 0, 0, unitF32Bits, 0, 0,
		},
		RecordSize: uint32(unsafe.Sizeof(SceneSpriteRecord{})),
	}
}

// ScenePolychromeSpriteRecord mirrors GpuiGoScenePolychromeSpriteRecord
// (the pinned PolychromeSprite): 100 bytes, alignment 4.
type ScenePolychromeSpriteRecord struct {
	Order           uint32
	Grayscale       uint32 // 0 color sampling, 1 grayscale
	Opacity         uint32 // f32 bits
	CornerSmoothing uint32 // f32 bits
	Bounds          SceneBounds
	ContentMask     SceneBounds
	CornerRadii     [4]uint32
	Tile            SceneTileRecord
	RecordSize      uint32
}

// NewScenePolychromeSpriteRecord returns a validated empty polychrome
// sprite record (opaque).
func NewScenePolychromeSpriteRecord() ScenePolychromeSpriteRecord {
	return ScenePolychromeSpriteRecord{
		Opacity:    oneF32Bits,
		RecordSize: uint32(unsafe.Sizeof(ScenePolychromeSpriteRecord{})),
	}
}

// unitF32Bits and oneF32Bits are the f32 bit patterns of 1.0.
const (
	unitF32Bits uint32 = 0x3F800000
	oneF32Bits  uint32 = 0x3F800000
)

// Scene command kinds.
const (
	SceneCommandBatch       uint32 = 0
	SceneCommandBeginFilter uint32 = 1
	SceneCommandEndFilter   uint32 = 2
)

// Scene batch primitive kinds (the reserved path kind is never emitted
// by this revision; the sprite kinds are emitted since ticket10).
const (
	SceneBatchShadows         uint32 = 0
	SceneBatchQuads           uint32 = 1
	SceneBatchPaths           uint32 = 2
	SceneBatchUnderlines      uint32 = 3
	SceneBatchMonochrome      uint32 = 4
	SceneBatchSubpixel        uint32 = 5
	SceneBatchPolychrome      uint32 = 6
	SceneBatchSurfaces        uint32 = 7
	SceneBatchBackdropFilters uint32 = 8
	SceneBatchFilterBoundary  uint32 = 9
)

// SceneCommandRecord mirrors GpuiGoSceneCommandRecord: 52 bytes,
// alignment 4.
type SceneCommandRecord struct {
	CommandKind              uint32
	PrimitiveKind            uint32
	RangeStart               uint32
	RangeEnd                 uint32
	Smoothed                 uint32
	TextureIndex             uint32
	RasterizationVertexCount uint32
	SpriteCount              uint32
	BoundaryIndex            uint32
	ClosingBoundaryIndex     uint32
	FilterTarget             uint32 // 0 inline, 1 isolated
	TargetIndex              uint32
	RecordSize               uint32
}

// SceneRequirementsRecord mirrors GpuiGoSceneRequirementsRecord: 44
// bytes, alignment 4.
type SceneRequirementsRecord struct {
	CommandCount                 uint32
	InstanceBatchCount           uint32
	PathRasterizationVertexCount uint32
	PathSpriteCount              uint32
	SurfaceCount                 uint32
	BackdropFilterCount          uint32
	IsolatedFilterCount          uint32
	IsolatedTargetCount          uint32
	UsesPathTarget               uint32
	UsesOffscreenTarget          uint32
	RecordSize                   uint32
}

// SceneMetaRecord mirrors GpuiGoSceneMetaRecord: 52 bytes, alignment 4
// (ticket10 added the three sprite counts before IsFinished).
type SceneMetaRecord struct {
	OpCount               uint32
	LayerPushCount        uint32
	QuadCount             uint32
	ShadowCount           uint32
	UnderlineCount        uint32
	BackdropCount         uint32
	BoundaryCount         uint32
	SurfaceCount          uint32
	MonochromeSpriteCount uint32
	SubpixelSpriteCount   uint32
	PolychromeSpriteCount uint32
	// PathCount is the path primitive count (ticket16).
	PathCount  uint32
	IsFinished uint32
	RecordSize uint32
}

// Scene dump kinds.
const (
	SceneDumpQuads             uint32 = 0
	SceneDumpShadows           uint32 = 1
	SceneDumpUnderlines        uint32 = 2
	SceneDumpBackdrops         uint32 = 3
	SceneDumpBoundaries        uint32 = 4
	SceneDumpSurfaces          uint32 = 5
	SceneDumpMonochromeSprites uint32 = 6
	SceneDumpSubpixelSprites   uint32 = 7
	SceneDumpPolychromeSprites uint32 = 8
)

// ---------------------------------------------------------------------------
// Service table mirror and validation
// ---------------------------------------------------------------------------

// sceneTable mirrors the native GpuiGoSceneTable: 26 function pointers
// then 17 self-check scalars; 280 bytes, alignment 8 (ticket16 added
// the three path entries and the three path record sizes).
type sceneTable struct {
	create                   uintptr
	clear                    uintptr
	dispose                  uintptr
	pushLayer                uintptr
	popLayer                 uintptr
	raiseOrderFloor          uintptr
	insertQuad               uintptr
	insertShadow             uintptr
	insertUnderline          uintptr
	insertBackdrop           uintptr
	insertBoundary           uintptr
	insertSurface            uintptr
	insertMonochromeSprite   uintptr
	insertSubpixelSprite     uintptr
	insertPolychromeSprite   uintptr
	replay                   uintptr
	finish                   uintptr
	length                   uintptr
	meta                     uintptr
	dump                     uintptr
	planDump                 uintptr
	requirements             uintptr
	panicProbe               uintptr
	insertPath               uintptr
	pathDump                 uintptr
	pathScript               uintptr
	serviceVersion           uint32
	sizeOfTable              uint32
	alignOfTable             uint32
	sizeOfQuadRecord         uint32
	sizeOfShadowRecord       uint32
	sizeOfUnderlineRecord    uint32
	sizeOfFilterRecord       uint32
	sizeOfSurfaceRecord      uint32
	sizeOfMonoSpriteRecord   uint32
	sizeOfPolySpriteRecord   uint32
	sizeOfCommandRecord      uint32
	sizeOfRequirementsRecord uint32
	sizeOfMetaRecord         uint32
	sizeOfPathRecord         uint32
	sizeOfPathVertexRecord   uint32
	sizeOfPathCommandRecord  uint32
	maxFilters               uint32
}

func sceneTableFromSlot(slot uintptr) *sceneTable {
	return (*sceneTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

func validateSceneTable(t *sceneTable, path string) error {
	abi := func(field, expected, actual string) error {
		return &ABIError{Path: path, Field: field, Expected: expected, Actual: actual}
	}
	if t.serviceVersion != sceneServiceVersion {
		return abi("scene service_version", fmt.Sprint(sceneServiceVersion), fmt.Sprint(t.serviceVersion))
	}
	if t.sizeOfTable != uint32(unsafe.Sizeof(sceneTable{})) {
		return abi("scene size_of_table", fmt.Sprint(unsafe.Sizeof(sceneTable{})), fmt.Sprint(t.sizeOfTable))
	}
	if t.alignOfTable != uint32(unsafe.Alignof(sceneTable{})) {
		return abi("scene align_of_table", fmt.Sprint(unsafe.Alignof(sceneTable{})), fmt.Sprint(t.alignOfTable))
	}
	if t.sizeOfQuadRecord != uint32(unsafe.Sizeof(SceneQuadRecord{})) {
		return abi("scene size_of_quad_record", fmt.Sprint(unsafe.Sizeof(SceneQuadRecord{})), fmt.Sprint(t.sizeOfQuadRecord))
	}
	if t.sizeOfShadowRecord != uint32(unsafe.Sizeof(SceneShadowRecord{})) {
		return abi("scene size_of_shadow_record", fmt.Sprint(unsafe.Sizeof(SceneShadowRecord{})), fmt.Sprint(t.sizeOfShadowRecord))
	}
	if t.sizeOfUnderlineRecord != uint32(unsafe.Sizeof(SceneUnderlineRecord{})) {
		return abi("scene size_of_underline_record", fmt.Sprint(unsafe.Sizeof(SceneUnderlineRecord{})), fmt.Sprint(t.sizeOfUnderlineRecord))
	}
	if t.sizeOfFilterRecord != uint32(unsafe.Sizeof(SceneFilterRecord{})) {
		return abi("scene size_of_filter_record", fmt.Sprint(unsafe.Sizeof(SceneFilterRecord{})), fmt.Sprint(t.sizeOfFilterRecord))
	}
	if t.sizeOfSurfaceRecord != uint32(unsafe.Sizeof(SceneSurfaceRecord{})) {
		return abi("scene size_of_surface_record", fmt.Sprint(unsafe.Sizeof(SceneSurfaceRecord{})), fmt.Sprint(t.sizeOfSurfaceRecord))
	}
	if t.sizeOfMonoSpriteRecord != uint32(unsafe.Sizeof(SceneSpriteRecord{})) {
		return abi("scene size_of_monochrome_sprite_record", fmt.Sprint(unsafe.Sizeof(SceneSpriteRecord{})), fmt.Sprint(t.sizeOfMonoSpriteRecord))
	}
	if t.sizeOfPolySpriteRecord != uint32(unsafe.Sizeof(ScenePolychromeSpriteRecord{})) {
		return abi("scene size_of_polychrome_sprite_record", fmt.Sprint(unsafe.Sizeof(ScenePolychromeSpriteRecord{})), fmt.Sprint(t.sizeOfPolySpriteRecord))
	}
	if t.sizeOfCommandRecord != uint32(unsafe.Sizeof(SceneCommandRecord{})) {
		return abi("scene size_of_command_record", fmt.Sprint(unsafe.Sizeof(SceneCommandRecord{})), fmt.Sprint(t.sizeOfCommandRecord))
	}
	if t.sizeOfRequirementsRecord != uint32(unsafe.Sizeof(SceneRequirementsRecord{})) {
		return abi("scene size_of_requirements_record", fmt.Sprint(unsafe.Sizeof(SceneRequirementsRecord{})), fmt.Sprint(t.sizeOfRequirementsRecord))
	}
	if t.sizeOfMetaRecord != uint32(unsafe.Sizeof(SceneMetaRecord{})) {
		return abi("scene size_of_meta_record", fmt.Sprint(unsafe.Sizeof(SceneMetaRecord{})), fmt.Sprint(t.sizeOfMetaRecord))
	}
	if t.sizeOfPathRecord != uint32(unsafe.Sizeof(ScenePathRecord{})) {
		return abi("scene size_of_path_record", fmt.Sprint(unsafe.Sizeof(ScenePathRecord{})), fmt.Sprint(t.sizeOfPathRecord))
	}
	if t.sizeOfPathVertexRecord != uint32(unsafe.Sizeof(ScenePathVertexRecord{})) {
		return abi("scene size_of_path_vertex_record", fmt.Sprint(unsafe.Sizeof(ScenePathVertexRecord{})), fmt.Sprint(t.sizeOfPathVertexRecord))
	}
	if t.sizeOfPathCommandRecord != uint32(unsafe.Sizeof(PathCommandRecord{})) {
		return abi("scene size_of_path_command_record", fmt.Sprint(unsafe.Sizeof(PathCommandRecord{})), fmt.Sprint(t.sizeOfPathCommandRecord))
	}
	slots := [...]uintptr{
		t.create, t.clear, t.dispose, t.pushLayer, t.popLayer, t.raiseOrderFloor,
		t.insertQuad, t.insertShadow, t.insertUnderline, t.insertBackdrop,
		t.insertBoundary, t.insertSurface, t.insertMonochromeSprite,
		t.insertSubpixelSprite, t.insertPolychromeSprite, t.replay, t.finish,
		t.length, t.meta, t.dump, t.planDump, t.requirements, t.panicProbe,
		t.insertPath, t.pathDump, t.pathScript,
	}
	names := [...]string{
		"scene_create", "scene_clear", "scene_dispose", "scene_push_layer",
		"scene_pop_layer", "scene_raise_order_floor", "scene_insert_quad",
		"scene_insert_shadow", "scene_insert_underline", "scene_insert_backdrop_filter",
		"scene_insert_filter_boundary", "scene_insert_surface",
		"scene_insert_monochrome_sprite", "scene_insert_subpixel_sprite",
		"scene_insert_polychrome_sprite", "scene_replay", "scene_finish",
		"scene_len", "scene_meta", "scene_dump", "scene_plan_dump",
		"scene_requirements", "scene_panic_probe",
		"scene_insert_path", "scene_path_dump", "scene_path_script",
	}
	for i, slot := range slots {
		if slot == 0 {
			return &CapabilityError{Path: path, Detail: "scene table slot " + names[i] + " is null"}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scene service and scenes
// ---------------------------------------------------------------------------

// SceneService is the typed accessor for the native scene kernel service
// of a loaded Library. It holds a Go-owned copy of the validated service
// table. It is safe for concurrent use; individual scenes are not (scene
// work runs on the owning foreground thread, like layout).
type SceneService struct {
	lib   *Library
	table sceneTable
}

// SceneHandle is a validated opaque native scene handle. It embeds the
// registry slot and slot generation; it becomes stale after dispose (or
// slot reuse).
type SceneHandle uint64

// Scene is a Go-side wrapper around one native scene with a busy guard:
// while any native scene call is in flight, every other operation on the
// same scene is rejected with ErrSceneBadValue before native entry (the
// service pattern's re-entry guard; scene calls have no callbacks, so
// the guard is defense in depth plus serialization).
type Scene struct {
	svc    *SceneService
	handle SceneHandle
	busy   atomic.Bool
}

// Scene returns the scene kernel service of the loaded artifact. The
// capability bit ("scene-kernel-v1") must be present and the service
// table in reserved slot 3 must validate (service version and record
// sizes) before any call is made. An artifact without the scene
// capability fails with ErrMissingCapability here, not at Load.
func (l *Library) Scene() (*SceneService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capSceneKernel == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capSceneKernel,
			Actual:   l.identity.Capabilities,
			Detail:   "scene-kernel-v1 capability bit missing",
		}
	}
	slot := l.table.reserved[3]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "scene service table (reserved slot 3) is null"}
	}
	table := *sceneTableFromSlot(slot) // copy out of DLL memory
	if err := validateSceneTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &SceneService{lib: l, table: table}, nil
}

// CreateScene creates a fresh empty native scene (generation 1).
func (s *SceneService) CreateScene() (*Scene, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	var handle uint64
	code, err := callSceneCreate(s.table.create, &handle)
	if err != nil {
		return nil, err
	}
	if code != sceneStatusOK {
		return nil, sceneStatusError(code, "scene_create")
	}
	return &Scene{svc: s, handle: SceneHandle(handle)}, nil
}

// Handle returns the native scene handle.
func (sc *Scene) Handle() SceneHandle { return sc.handle }

// withScene runs f while the scene's busy guard is held. The guard
// rejects same-scene re-entry before native entry.
func (sc *Scene) withScene(op string, f func() (int32, error)) (int32, error) {
	if sc.busy.CompareAndSwap(false, true) {
		defer sc.busy.Store(false)
		return f()
	}
	return 0, sceneStatusError(sceneErrBadValue, op+" rejected: scene busy (Go busy guard, before native entry)")
}

// Clear resets the scene to a fresh empty state (the pinned
// Scene::clear): every primitive array, the bounds tree, the layer
// stack, the plan and the failed flag.
func (sc *Scene) Clear() error {
	code, err := sc.withScene("scene_clear", func() (int32, error) {
		return callSceneOp(sc.svc.table.clear, uint64(sc.handle))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_clear")
	}
	return nil
}

// Dispose frees the scene; its handle becomes stale. In-flight native
// calls hold their own references, so actual destruction is deferred
// until they return (deferred native destruction).
func (sc *Scene) Dispose() error {
	code, err := sc.withScene("scene_dispose", func() (int32, error) {
		return callSceneOp(sc.svc.table.dispose, uint64(sc.handle))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_dispose")
	}
	return nil
}

// PushLayer pushes a paint layer with the given device-pixel bounds
// (the pinned Scene::push_layer: the layer's order comes from the bounds
// tree and is shared by every primitive painted inside the layer).
func (sc *Scene) PushLayer(bounds SceneBounds) error {
	code, err := sc.withScene("scene_push_layer", func() (int32, error) {
		return callScenePushLayer(sc.svc.table.pushLayer, uint64(sc.handle), &bounds)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_push_layer")
	}
	return nil
}

// PopLayer pops the innermost layer (the pinned Scene::pop_layer).
func (sc *Scene) PopLayer() error {
	code, err := sc.withScene("scene_pop_layer", func() (int32, error) {
		return callSceneOp(sc.svc.table.popLayer, uint64(sc.handle))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_pop_layer")
	}
	return nil
}

// RaiseOrderFloor raises the deferred-draw order floor so every
// primitive inserted afterwards sorts above everything inserted before
// (the pinned Scene::raise_order_floor).
func (sc *Scene) RaiseOrderFloor() error {
	code, err := sc.withScene("scene_raise_order_floor", func() (int32, error) {
		return callSceneOp(sc.svc.table.raiseOrderFloor, uint64(sc.handle))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_raise_order_floor")
	}
	return nil
}

// InsertQuad inserts a quad record (order must be 0; the kernel assigns
// the draw order).
func (sc *Scene) InsertQuad(rec *SceneQuadRecord) error {
	code, err := sc.withScene("scene_insert_quad", func() (int32, error) {
		return callSceneInsertQuad(sc.svc.table.insertQuad, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_quad")
	}
	return nil
}

// InsertShadow inserts a shadow record.
func (sc *Scene) InsertShadow(rec *SceneShadowRecord) error {
	code, err := sc.withScene("scene_insert_shadow", func() (int32, error) {
		return callSceneInsertShadow(sc.svc.table.insertShadow, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_shadow")
	}
	return nil
}

// InsertUnderline inserts an underline record.
func (sc *Scene) InsertUnderline(rec *SceneUnderlineRecord) error {
	code, err := sc.withScene("scene_insert_underline", func() (int32, error) {
		return callSceneInsertUnderline(sc.svc.table.insertUnderline, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_underline")
	}
	return nil
}

// InsertBackdropFilter inserts a backdrop-filter record.
func (sc *Scene) InsertBackdropFilter(rec *SceneFilterRecord) error {
	code, err := sc.withScene("scene_insert_backdrop_filter", func() (int32, error) {
		return callSceneInsertFilter(sc.svc.table.insertBackdrop, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_backdrop_filter")
	}
	return nil
}

// InsertFilterBoundary inserts a content-filter boundary marker (start
// or end). A scene cannot close an unopened group; unmatched markers are
// handled by the plan (an unmatched start renders inline).
func (sc *Scene) InsertFilterBoundary(rec *SceneFilterRecord) error {
	code, err := sc.withScene("scene_insert_filter_boundary", func() (int32, error) {
		return callSceneInsertFilter(sc.svc.table.insertBoundary, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_filter_boundary")
	}
	return nil
}

// InsertSurface inserts a surface record with its paired opacity (the
// pinned insert_surface path: the opacity stays paired with the surface
// through the finish sort and replay).
func (sc *Scene) InsertSurface(rec *SceneSurfaceRecord, opacity float32) error {
	code, err := sc.withScene("scene_insert_surface", func() (int32, error) {
		return callSceneInsertSurface(sc.svc.table.insertSurface, uint64(sc.handle), rec, math.Float32bits(opacity))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_surface")
	}
	return nil
}

// InsertMonochromeSprite inserts a monochrome sprite record (an R8 atlas
// tile drawn with the mask color). The tile's texture kind must be the
// monochrome pool.
func (sc *Scene) InsertMonochromeSprite(rec *SceneSpriteRecord) error {
	code, err := sc.withScene("scene_insert_monochrome_sprite", func() (int32, error) {
		return callSceneInsertSprite(sc.svc.table.insertMonochromeSprite, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_monochrome_sprite")
	}
	return nil
}

// InsertSubpixelSprite inserts a subpixel sprite record (a BGRA LCD
// atlas tile blended with the mask color).
func (sc *Scene) InsertSubpixelSprite(rec *SceneSpriteRecord) error {
	code, err := sc.withScene("scene_insert_subpixel_sprite", func() (int32, error) {
		return callSceneInsertSprite(sc.svc.table.insertSubpixelSprite, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_subpixel_sprite")
	}
	return nil
}

// InsertPolychromeSprite inserts a polychrome sprite record (a BGRA
// color atlas tile drawn with its own opacity and corner smoothing).
func (sc *Scene) InsertPolychromeSprite(rec *ScenePolychromeSpriteRecord) error {
	code, err := sc.withScene("scene_insert_polychrome_sprite", func() (int32, error) {
		return callSceneInsertPolySprite(sc.svc.table.insertPolychromeSprite, uint64(sc.handle), rec)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_polychrome_sprite")
	}
	return nil
}

// Replay re-inserts the range [start, end) of prev's paint operations
// into this scene (the pinned Scene::replay: orders are recomputed
// against THIS scene's bounds tree and layer state; surfaces keep their
// paired opacities). The caller keeps prev pinned while ranges reference
// it; a scene cannot replay itself.
func (sc *Scene) Replay(start, end uint32, prev *Scene) error {
	if prev == nil {
		return sceneStatusError(sceneErrNullArg, "scene_replay: nil source scene")
	}
	if prev.handle == sc.handle {
		return sceneStatusError(sceneErrBadValue, "scene_replay: a scene cannot replay itself")
	}
	code, err := sc.withScene("scene_replay", func() (int32, error) {
		return callSceneReplay(sc.svc.table.replay, uint64(sc.handle), start, end, uint64(prev.handle))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_replay")
	}
	return nil
}

// InsertPath inserts one path primitive with its bulk vertex array
// (the pinned Scene::insert_primitive(Primitive::Path)); order must be
// 0 — the kernel assigns it.
func (sc *Scene) InsertPath(rec *ScenePathRecord, vertices []ScenePathVertexRecord) error {
	if rec.VertexCount != uint32(len(vertices)) {
		return sceneStatusError(sceneErrBadValue, "scene_insert_path: record vertex_count disagrees with the vertex array")
	}
	var vertexPtr *ScenePathVertexRecord
	if len(vertices) > 0 {
		vertexPtr = &vertices[0]
	}
	code, err := sc.withScene("scene_insert_path", func() (int32, error) {
		return callSceneInsertPath(sc.svc.table.insertPath, uint64(sc.handle), rec, vertexPtr, uint32(len(vertices)))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_insert_path")
	}
	return nil
}

// PathDump bulk-dumps the finished scene's path records (with the
// assigned orders and per-path vertex counts) and the concatenated
// vertex array. Requires Finish; a too-small capacity retries with the
// required counts.
func (sc *Scene) PathDump() ([]ScenePathRecord, []ScenePathVertexRecord, error) {
	var outRecords uint32
	var outVertices uint32
	// First call: report the required counts (record capacity 0).
	if _, err := sc.withScene("scene_path_dump", func() (int32, error) {
		return callScenePathDump(sc.svc.table.pathDump, uint64(sc.handle), nil, nil, 0, 0, &outRecords, &outVertices)
	}); err != nil {
		return nil, nil, err
	}
	if outRecords > 1<<24 || outVertices > 1<<28 {
		return nil, nil, sceneStatusError(sceneErrBadValue, "scene_path_dump: implausible required counts")
	}
	records := make([]ScenePathRecord, outRecords)
	vertices := make([]ScenePathVertexRecord, outVertices)
	var recPtr *ScenePathRecord
	var vertexPtr *ScenePathVertexRecord
	if outRecords > 0 {
		recPtr = &records[0]
	}
	if outVertices > 0 {
		vertexPtr = &vertices[0]
	}
	code, err := sc.withScene("scene_path_dump", func() (int32, error) {
		return callScenePathDump(sc.svc.table.pathDump, uint64(sc.handle), recPtr, vertexPtr, outRecords, outVertices, &outRecords, &outVertices)
	})
	if err != nil {
		return nil, nil, err
	}
	if code != sceneStatusOK {
		return nil, nil, sceneStatusError(code, "scene_path_dump")
	}
	return records[:outRecords], vertices[:outVertices], nil
}

// TessellatePath runs the pinned PathBuilder tessellation (the same
// lyon code the reference oracle drives) over one command script and
// returns the tessellated vertices (logical pixels) plus the bounds of
// the tessellation: the union of the triangles, or (0,0,0,0) for an
// empty tessellation (the pinned build_path fallback). The words slice
// carries the call's point pool (point pairs and dash lengths).
func (s *SceneService) TessellatePath(commands []PathCommandRecord, words []uint32) ([]ScenePathVertexRecord, SceneBounds, error) {
	var outCount uint32
	var outBounds SceneBounds
	var cmdPtr *PathCommandRecord
	var wordPtr *uint32
	if len(commands) > 0 {
		cmdPtr = &commands[0]
	}
	if len(words) > 0 {
		wordPtr = &words[0]
	}
	// First call: report the required vertex count (capacity 0).
	if _, err := callScenePathScript(s.table.pathScript, cmdPtr, uint32(len(commands)), wordPtr, uint32(len(words)), nil, 0, &outCount, &outBounds); err != nil {
		return nil, SceneBounds{}, err
	}
	if outCount > 1<<28 {
		return nil, SceneBounds{}, sceneStatusError(sceneErrBadValue, "scene_path_script: implausible vertex count")
	}
	vertices := make([]ScenePathVertexRecord, outCount)
	var vertexPtr *ScenePathVertexRecord
	if outCount > 0 {
		vertexPtr = &vertices[0]
	}
	code, err := callScenePathScript(s.table.pathScript, cmdPtr, uint32(len(commands)), wordPtr, uint32(len(words)), vertexPtr, outCount, &outCount, &outBounds)
	if err != nil {
		return nil, SceneBounds{}, err
	}
	if code != sceneStatusOK {
		return nil, SceneBounds{}, sceneStatusError(code, "scene_path_script")
	}
	return vertices, outBounds, nil
}

// Finish sorts the primitive arrays and compiles the render plan (the
// pinned Scene::finish). Plan/requirements/dump reads require it.
func (sc *Scene) Finish() error {
	code, err := sc.withScene("scene_finish", func() (int32, error) {
		return callSceneOp(sc.svc.table.finish, uint64(sc.handle))
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, "scene_finish")
	}
	return nil
}

// Len returns the scene's paint-operation count (the pinned Scene::len;
// the replay address space).
func (sc *Scene) Len() (uint32, error) {
	var length uint32
	code, err := sc.withScene("scene_len", func() (int32, error) {
		return callSceneLen(sc.svc.table.length, uint64(sc.handle), &length)
	})
	if err != nil {
		return 0, err
	}
	if code != sceneStatusOK {
		return 0, sceneStatusError(code, "scene_len")
	}
	return length, nil
}

// Meta returns the scene's totals (op count, layer pushes, per-class
// primitive counts, finished flag).
func (sc *Scene) Meta() (SceneMetaRecord, error) {
	var meta SceneMetaRecord
	code, err := sc.withScene("scene_meta", func() (int32, error) {
		return callSceneMeta(sc.svc.table.meta, uint64(sc.handle), &meta)
	})
	if err != nil {
		return SceneMetaRecord{}, err
	}
	if code != sceneStatusOK {
		return SceneMetaRecord{}, sceneStatusError(code, "scene_meta")
	}
	if meta.RecordSize != uint32(unsafe.Sizeof(SceneMetaRecord{})) {
		return SceneMetaRecord{}, &ABIError{
			Field:    "scene meta record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(SceneMetaRecord{})),
			Actual:   fmt.Sprint(meta.RecordSize),
		}
	}
	return meta, nil
}

// Requirements returns the compiled plan's requirements. Requires Finish.
func (sc *Scene) Requirements() (SceneRequirementsRecord, error) {
	var requirements SceneRequirementsRecord
	code, err := sc.withScene("scene_requirements", func() (int32, error) {
		return callSceneRequirements(sc.svc.table.requirements, uint64(sc.handle), &requirements)
	})
	if err != nil {
		return SceneRequirementsRecord{}, err
	}
	if code != sceneStatusOK {
		return SceneRequirementsRecord{}, sceneStatusError(code, "scene_requirements")
	}
	if requirements.RecordSize != uint32(unsafe.Sizeof(SceneRequirementsRecord{})) {
		return SceneRequirementsRecord{}, &ABIError{
			Field:    "scene requirements record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(SceneRequirementsRecord{})),
			Actual:   fmt.Sprint(requirements.RecordSize),
		}
	}
	return requirements, nil
}

// PlanCommands bulk-dumps the compiled render-plan commands (the
// backend-neutral work order: batches with primitive ranges, and paired
// filter begin/end commands with their isolation targets). Requires
// Finish. A too-small capacity is retried once with the reported count.
func (sc *Scene) PlanCommands() ([]SceneCommandRecord, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		commands := make([]SceneCommandRecord, want)
		var count uint32
		code, err := sc.withScene("scene_plan_dump", func() (int32, error) {
			var cmdPtr uintptr
			if want > 0 {
				cmdPtr = uintptr(unsafe.Pointer(&commands[0]))
			}
			return callScenePlanDump(sc.svc.table.planDump, uint64(sc.handle), cmdPtr, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_plan_dump")
		}
		return commands[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_plan_dump capacity retry failed")
}

// DumpQuads bulk-dumps the finished scene's quads (sorted by draw
// order). Requires Finish. A too-small capacity is retried once with the
// reported count.
func (sc *Scene) DumpQuads() ([]SceneQuadRecord, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]SceneQuadRecord, want)
		var count uint32
		var recPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
		}
		code, err := sc.withScene("scene_dump quads", func() (int32, error) {
			return callSceneDump(sc.svc.table.dump, uint64(sc.handle), SceneDumpQuads, recPtr, 0, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_dump quads")
		}
		return out[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_dump quads capacity retry failed")
}

// DumpShadows bulk-dumps the finished scene's shadows (sorted by draw
// order). Requires Finish. A too-small capacity is retried once with the
// reported count.
func (sc *Scene) DumpShadows() ([]SceneShadowRecord, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]SceneShadowRecord, want)
		var count uint32
		var recPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
		}
		code, err := sc.withScene("scene_dump shadows", func() (int32, error) {
			return callSceneDump(sc.svc.table.dump, uint64(sc.handle), SceneDumpShadows, recPtr, 0, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_dump shadows")
		}
		return out[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_dump shadows capacity retry failed")
}

// DumpUnderlines bulk-dumps the finished scene's underlines (sorted by draw
// order). Requires Finish. A too-small capacity is retried once with the
// reported count.
func (sc *Scene) DumpUnderlines() ([]SceneUnderlineRecord, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]SceneUnderlineRecord, want)
		var count uint32
		var recPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
		}
		code, err := sc.withScene("scene_dump underlines", func() (int32, error) {
			return callSceneDump(sc.svc.table.dump, uint64(sc.handle), SceneDumpUnderlines, recPtr, 0, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_dump underlines")
		}
		return out[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_dump underlines capacity retry failed")
}

// DumpBackdrops bulk-dumps the finished scene's backdrops (sorted by draw order;
// order). Requires Finish. A too-small capacity is retried once with the
// reported count.
func (sc *Scene) DumpBackdrops() ([]SceneFilterRecord, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]SceneFilterRecord, want)
		var count uint32
		var recPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
		}
		code, err := sc.withScene("scene_dump backdrops", func() (int32, error) {
			return callSceneDump(sc.svc.table.dump, uint64(sc.handle), SceneDumpBackdrops, recPtr, 0, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_dump backdrops")
		}
		return out[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_dump backdrops capacity retry failed")
}

// DumpBoundaries bulk-dumps the finished scene's boundaries (sorted by draw order, then start before end;
// order). Requires Finish. A too-small capacity is retried once with the
// reported count.
func (sc *Scene) DumpBoundaries() ([]SceneFilterRecord, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]SceneFilterRecord, want)
		var count uint32
		var recPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
		}
		code, err := sc.withScene("scene_dump boundaries", func() (int32, error) {
			return callSceneDump(sc.svc.table.dump, uint64(sc.handle), SceneDumpBoundaries, recPtr, 0, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_dump boundaries")
		}
		return out[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_dump boundaries capacity retry failed")
}

// DumpSurfaces bulk-dumps the finished scene's surfaces together with
// their paired opacities (the pairing survives the finish sort and
// replay). Requires Finish. A too-small capacity is retried once with
// the reported count.
func (sc *Scene) DumpSurfaces() ([]SceneSurfaceRecord, []float32, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]SceneSurfaceRecord, want)
		opacities := make([]float32, want)
		var count uint32
		var recPtr, opPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
			opPtr = uintptr(unsafe.Pointer(&opacities[0]))
		}
		code, err := sc.withScene("scene_dump surfaces", func() (int32, error) {
			return callSceneDump(sc.svc.table.dump, uint64(sc.handle), SceneDumpSurfaces, recPtr, opPtr, want, &count)
		})
		if err != nil {
			return nil, nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, nil, sceneStatusError(code, "scene_dump surfaces")
		}
		return out[:count], opacities[:count], nil
	}
	return nil, nil, sceneStatusError(sceneErrCapacity, "scene_dump surfaces capacity retry failed")
}

// DumpMonochromeSprites dumps the finished scene's monochrome sprite
// records (the capacity-retry protocol of the other dumps).
func (sc *Scene) DumpMonochromeSprites() ([]SceneSpriteRecord, error) {
	return dumpSprites[SceneSpriteRecord](sc, SceneDumpMonochromeSprites, "monochrome")
}

// DumpSubpixelSprites dumps the finished scene's subpixel sprite records.
func (sc *Scene) DumpSubpixelSprites() ([]SceneSpriteRecord, error) {
	return dumpSprites[SceneSpriteRecord](sc, SceneDumpSubpixelSprites, "subpixel")
}

// dumpSprites implements the sprite dump capacity-retry protocol.
func dumpSprites[T any](sc *Scene, kind uint32, class string) ([]T, error) {
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]T, want)
		var recPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
		}
		var count uint32
		code, err := sc.withScene("scene_dump "+class+" sprites", func() (int32, error) {
			return callSceneDump(sc.svc.table.dump, uint64(sc.handle), kind, recPtr, 0, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_dump "+class+" sprites")
		}
		return out[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_dump "+class+" sprites capacity retry failed")
}

// DumpPolychromeSprites dumps the finished scene's polychrome sprite
// records.
func (sc *Scene) DumpPolychromeSprites() ([]ScenePolychromeSpriteRecord, error) {
	return dumpSprites[ScenePolychromeSpriteRecord](sc, SceneDumpPolychromeSprites, "polychrome")
}

// PanicProbe invokes the scene service's deliberate panic probe and
// verifies the panic was contained (status 101) before returning across
// the ABI. It exists so conformance tests can observe real panic
// containment through this service's table; application code never needs
// it.
func (s *SceneService) PanicProbe() error {
	if s.lib.released.Load() {
		return ErrClosed
	}
	code, err := callScenePanicProbe(s.table.panicProbe)
	if err != nil {
		return err
	}
	if code != sceneErrPanic {
		return &SceneStatusError{Code: code, Name: sceneStatusName(code), Detail: "panic probe must report containment (status 101)"}
	}
	return nil
}
