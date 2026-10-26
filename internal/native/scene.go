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
// and the pinned-PathBuilder tessellation entry; version 4 (gpui-perf
// ticket 05) appended the insert_batch entry (scene_batch.go);
// version 5 (rev 15.1, browser-core ticket 77) grew the filter record
// to 780 bytes (8 filters per chain, one matrix slot per colour
// function with the per-function clamp — the native side's scene.rs
// doc has the full note); version 6 (rev 16, browser-core ticket 50)
// added the effect-group primitive (SceneEffectRecord,
// InsertEffect/EffectDump, the batch ops 12/13 and the plan command
// kinds 3/4), grew every content mask from 16 to 52 bytes
// (SceneContentMask: bounds + per-edge fade_out (LEFT, TOP, RIGHT,
// BOTTOM) + corner radii + smoothing; a zero-radii, zero-fade mask
// behaves exactly as the old bounds-only mask), removed the filter
// group depth cap in favour of the pool-index plan with the
// MAX_SCENE_GROUPS = 1024 sanity bound, and grew the requirements
// record (isolated_effect_count) and the meta record (effect_count).
// The design doc's "scene service v5" became this version 6 because
// ticket 77 had already taken 5. Version 7 (rev 16, browser-core
// ticket 51) appended the update_property entry (the design's
// appendix A.7: the finished-scene property-node mutation with the
// old/new mapped bounds) - the only addition, no records or scalars
// changed. Version 9 (rev 16, browser-core ticket 53) appended
// tess_cache_stats (the path tessellation LRU cache's hits, misses,
// evictions and entry count).
const sceneServiceVersion uint32 = 9

// Property kinds of Scene.UpdateProperty (rev 16, ticket 51; the
// native scene_property_kind mirror).
const (
	// ScenePropertyMatrix updates the node's 2x3 transform (six f32
	// values, row-major [m00, m01, m10, m11, tx, ty]; the records must
	// carry SceneEffectFlagHasTransform).
	ScenePropertyMatrix uint32 = 0
	// ScenePropertyOpacity updates the node's group opacity (one f32
	// value in [0, 1]; the records must carry SceneEffectFlagHasOpacity).
	ScenePropertyOpacity uint32 = 1
)

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

// SceneContentMask mirrors GpuiGoSceneContentMask (rev 16, ticket 50):
// the pin's shader-side content mask finally carried by the ABI - the
// clip bounds, the linear per-edge fade ramp, and the rounded-corner
// clip (radii + smoothing; the innermost radii apply INLINE in the
// vendored quad/shadow pipelines, ancestors' rounded clips become
// effect groups). 56 bytes, alignment 4 (52 bytes of fields plus the
// one-word layout pad the HLSL structured-buffer layout requires -
// the mask's bounds member forces 8-byte alignment, so the struct's
// size rounds up; the pad must be zero); replaces the 16-byte
// bounds-only mask in every record that has one. A zero-radii,
// zero-fade mask behaves exactly as the pre-R16 bounds-only mask.
type SceneContentMask struct {
	// Bounds is the clip bounds (the rect intersection).
	Bounds SceneBounds
	// FadeOut is the per-edge fade distances: LEFT, TOP, RIGHT, BOTTOM
	// (f32 bits; a zero distance leaves that edge fully opaque).
	FadeOut [4]uint32
	// CornerRadii is the rounded-clip radii: top-left, top-right,
	// bottom-right, bottom-left (f32 bits).
	CornerRadii [4]uint32
	// CornerSmoothing is the corner smoothing in [0, 1] (f32 bits).
	CornerSmoothing uint32
	// MaskPadding is the layout pad the HLSL structured-buffer layout
	// requires (the mask's bounds member forces 8-byte alignment, so
	// the struct's size rounds up); must be zero.
	MaskPadding uint32
}

// SceneContentMaskOf packs a rect-only (zero fade, zero radii) mask -
// the exact pre-R16 behaviour, the encoding every Go paint path that
// does not carry the rounded clip uses.
func SceneContentMaskOf(x, y, w, h float32) SceneContentMask {
	return SceneContentMask{Bounds: SceneBoundsOf(x, y, w, h)}
}

// SceneContentMaskRounded packs a content mask with the rounded-corner
// clip (rev 16, browser-core ticket 55): the rect plus the corner radii
// (top-left, top-right, bottom-right, bottom-left) and the smoothing.
// The fade edges stay zero - the Go styling authors no fades yet
// (ticket 57). Zero radii pack exactly the rect-only mask.
func SceneContentMaskRounded(x, y, w, h float32, radii [4]float32, smoothing float32) SceneContentMask {
	return SceneContentMask{
		Bounds: SceneBoundsOf(x, y, w, h),
		CornerRadii: [4]uint32{
			math.Float32bits(radii[0]),
			math.Float32bits(radii[1]),
			math.Float32bits(radii[2]),
			math.Float32bits(radii[3]),
		},
		CornerSmoothing: math.Float32bits(smoothing),
	}
}

// IsRounded reports whether the mask clips with rounded corners.
func (m SceneContentMask) IsRounded() bool {
	return m.CornerRadii[0] != 0 || m.CornerRadii[1] != 0 || m.CornerRadii[2] != 0 || m.CornerRadii[3] != 0
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

// SceneBackground mirrors GpuiGoSceneBackground (browser-core ticket
// 39, rev 15): the full pinned Background shape — a solid color or a
// two-stop linear gradient with its interpolation color space. The
// quad's background and border, the shadow's color and the path's color
// carry it; gradient records evaluate with CSS semantics in the
// renderer (the gradient-line length reaches the corners, the stops mix
// premultiplied in sRGB, the default). 72 bytes, alignment 4.
type SceneBackground struct {
	// Tag is 0 (solid) or 1 (linear gradient), the pinned
	// BackgroundTag.
	Tag uint32
	// ColorSpace is 0 (sRGB, the ABI default and CSS's legacy
	// interpolation space) or 1 (Oklab), the pinned ColorSpace.
	ColorSpace uint32
	// H is the solid hue in [0, 1] (degrees / 360), f32 bits.
	H uint32
	// S is the solid saturation in [0, 1], f32 bits.
	S uint32
	// L is the solid lightness in [0, 1], f32 bits.
	L uint32
	// A is the solid alpha in [0, 1], f32 bits.
	A uint32
	// Angle is the gradient angle in CSS degrees (f32 bits; 0 points
	// up, growing clockwise, 90 is to the right; any finite value,
	// taken modulo 360).
	Angle uint32
	// Stop0H..Stop0A are stop 0's HSLA (f32 bits).
	Stop0H uint32
	Stop0S uint32
	Stop0L uint32
	Stop0A uint32
	// Stop0Percentage is stop 0's position along the gradient line (f32
	// bits; 0 = the line's start, 1 = its end; a descending pair
	// reverses the gradient).
	Stop0Percentage uint32
	// Stop1H..Stop1A are stop 1's HSLA (f32 bits).
	Stop1H uint32
	Stop1S uint32
	Stop1L uint32
	Stop1A uint32
	// Stop1Percentage is stop 1's position; must differ from stop 0's.
	Stop1Percentage uint32
	// Reserved must be zero.
	Reserved uint32
}

// SceneBackgroundOf packs a solid HSLA color (h in degrees/360) into the
// background record.
func SceneBackgroundOf(h, s, l, a float32) SceneBackground {
	return SceneBackground{H: math.Float32bits(h), S: math.Float32bits(s), L: math.Float32bits(l), A: math.Float32bits(a)}
}

// HslaF is one HSLA color with the hue as a [0, 1] fraction of the
// circle (the ABI's storage form).
type HslaF struct {
	H, S, L, A float32
}

// SceneGradientBackground packs a two-stop linear gradient: the CSS
// angle in degrees (0 = up, clockwise) and both stops (the hue as a
// [0, 1] fraction with the position along the gradient line; the
// positions must differ). The color space defaults to sRGB (CSS's
// legacy interpolation).
// SceneGradientTableBackground builds the R16 gradient-table arm of the
// background record (rev 16, ticket 52): the table index rides the
// ANGLE field as f32 bits; every other payload word is zero.
func SceneGradientTableBackground(index uint32) SceneBackground {
	return SceneBackground{
		Tag:   4,
		Angle: math.Float32bits(float32(index)),
	}
}

func SceneGradientBackground(angle float32, from HslaF, fromPos float32, to HslaF, toPos float32) SceneBackground {
	return SceneBackground{
		Tag:             1,
		Angle:           math.Float32bits(angle),
		Stop0H:          math.Float32bits(from.H),
		Stop0S:          math.Float32bits(from.S),
		Stop0L:          math.Float32bits(from.L),
		Stop0A:          math.Float32bits(from.A),
		Stop0Percentage: math.Float32bits(fromPos),
		Stop1H:          math.Float32bits(to.H),
		Stop1S:          math.Float32bits(to.S),
		Stop1L:          math.Float32bits(to.L),
		Stop1A:          math.Float32bits(to.A),
		Stop1Percentage: math.Float32bits(toPos),
	}
}

// Solid unpacks the solid arm (valid for tag 0).
func (b SceneBackground) Solid() (h, s, l, a float32) {
	return math.Float32frombits(b.H), math.Float32frombits(b.S), math.Float32frombits(b.L), math.Float32frombits(b.A)
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
// record (order must be 0 on insert). 156 bytes, alignment 4 (rev 15:
// the color is a full background record; rev 16: the extended content
// mask). The vertices travel as the
// parallel bulk array of InsertPath and PathDump.
type ScenePathRecord struct {
	Order       uint32
	Bounds      SceneBounds
	ContentMask SceneContentMask
	Color       SceneBackground
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

// SceneQuadRecord mirrors GpuiGoSceneQuadRecord: 272 bytes, alignment 4
// (rev 15: the background and border are full background records; rev
// 16: the extended content mask).
// Order must be 0 on insert; the kernel assigns it (mirrored in dumps).
type SceneQuadRecord struct {
	Order              uint32
	BorderStyle        uint32 // 0 solid, 1 dashed
	BorderDashedLength uint32
	BorderDashedGap    uint32
	Bounds             SceneBounds
	ContentMask        SceneContentMask
	Background         SceneBackground
	BorderColor        SceneBackground
	CornerRadii        [4]uint32 // top-left, top-right, bottom-right, bottom-left
	BorderWidths       [4]uint32 // top, right, bottom, left
	CornerSmoothing    uint32
	Padding            uint32
	// LayoutPadding is the trailing layout pad (the record's HLSL
	// structured-buffer size rounds up to its 8-byte alignment); must
	// be zero. The field order mirrors the pinned struct: the record IS
	// the vendored quad pipeline's instance layout, and the
	// vec2f-bearing fields (bounds, the mask's bounds) must sit on
	// 8-byte-aligned offsets.
	LayoutPadding uint32
	RecordSize    uint32
}

// NewSceneQuadRecord returns a validated empty quad record (every field
// zeroed except the record-size self-check).
func NewSceneQuadRecord() SceneQuadRecord {
	return SceneQuadRecord{RecordSize: uint32(unsafe.Sizeof(SceneQuadRecord{}))}
}

// SceneShadowRecord mirrors GpuiGoSceneShadowRecord: 208 bytes,
// alignment 4 (rev 15: the color is a full background record and drop
// shadows clip out of the element's rounded bounds; rev 16: the
// extended content mask).
type SceneShadowRecord struct {
	Order              uint32
	BlurRadius         uint32
	Bounds             SceneBounds
	CornerRadii        [4]uint32
	ContentMask        SceneContentMask
	Color              SceneBackground
	ElementBounds      SceneBounds
	ElementCornerRadii [4]uint32
	Inset              uint32 // 0 drop, 1 inset
	CornerSmoothing    uint32
	// LayoutPadding is the trailing layout pad (the record's HLSL
	// structured-buffer size rounds up to its 8-byte alignment); must
	// be zero. The record IS the vendored shadow pipeline's instance
	// layout.
	LayoutPadding uint32
	RecordSize    uint32
}

// NewSceneShadowRecord returns a validated empty shadow record.
func NewSceneShadowRecord() SceneShadowRecord {
	return SceneShadowRecord{RecordSize: uint32(unsafe.Sizeof(SceneShadowRecord{}))}
}

// SceneUnderlineRecord mirrors GpuiGoSceneUnderlineRecord: 112 bytes,
// alignment 4 (rev 16: the extended content mask).
type SceneUnderlineRecord struct {
	Order       uint32
	Padding     uint32
	Bounds      SceneBounds
	ContentMask SceneContentMask
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
// filter boundaries): 820 bytes, alignment 4 (rev 15: per-filter kind
// tags and one composed colour-matrix payload; rev 15.1: one matrix
// slot per colour function; rev 16: the extended content mask).
type SceneFilterRecord struct {
	Order           uint32
	Bounds          SceneBounds
	ContentMask     SceneContentMask
	CornerRadii     [4]uint32
	CornerSmoothing uint32
	BlurRadii       [8]uint32
	FilterCount     uint32
	Opacity         uint32
	IsStart         uint32 // boundary: 1 start, 0 end; backdrops pass 0
	// FilterTags carries the per-filter kind for the first FilterCount
	// entries: 0 = blur, 1 = colour matrix. Entries at and beyond
	// FilterCount must be zero.
	FilterTags [8]uint32
	// ColorMatrix carries the chain's colour matrices, one 20-word slot
	// per matrix filter in chain order (rev 15.1, browser-core ticket
	// 77): slot i is ColorMatrix[20*i : 20*i+20], a row-major 4x5 matrix
	// (f32 bits; row 0 in [0..5], the fifth column the additive offset)
	// — the matrix of the i-th colour function itself, NOT a composed
	// product: Chrome clamps each function's output to [0,1] before the
	// next applies, which one linear map cannot express. Slots at and
	// beyond ColorMatrixCount must be zero.
	ColorMatrix [160]uint32
	// ColorMatrixCount is how many of the chain's filters are colour
	// matrices (must equal the tag-1 entry count).
	ColorMatrixCount uint32
	RecordSize       uint32
	// OpChainIndex is the appended drop-shadow op-chain reference (rev
	// 16, ticket 52, the design's appendix A.5): the scene-level chain
	// the group's drop-shadow ops live in. 0/0 (the default) is the R15
	// legacy chain - bit-exact.
	OpChainIndex uint32
	// OpChainCount is the op count (0 = R15 legacy).
	OpChainCount uint32
}

// ColorMatrixRows returns the i-th matrix slot as four rows of five
// floats — row r is ColorMatrix[20*i+5*r : 20*i+5*r+5] with the last
// entry the additive offset.
func (r SceneFilterRecord) ColorMatrixRows(i int) [4][5]float32 {
	var rows [4][5]float32
	for row := 0; row < 4; row++ {
		for col := 0; col < 5; col++ {
			rows[row][col] = math.Float32frombits(r.ColorMatrix[20*i+5*row+col])
		}
	}
	return rows
}

// NewSceneFilterRecord returns a validated empty filter record.
func NewSceneFilterRecord() SceneFilterRecord {
	return SceneFilterRecord{RecordSize: uint32(unsafe.Sizeof(SceneFilterRecord{}))}
}

// SceneSurfaceRecord mirrors GpuiGoSceneSurfaceRecord: 96 bytes,
// alignment 4 (rev 16: the extended content mask).
type SceneSurfaceRecord struct {
	Order          uint32
	Bounds         SceneBounds
	ContentMask    SceneContentMask
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

// MaxGradientStops is the gradient table's stop cap (rev 16, ticket
// 52; the user's 2026-10-24 decision - the design text's 32 was raised
// to 64: zgui's ramp cap). Above it the GO layer resamples down to 64
// and warns in debug builds; the native service rejects the insert
// with ErrSceneLimit.
const MaxGradientStops = 64

// SceneSpriteRecord mirrors GpuiGoSceneSpriteRecord (the pinned
// MonochromeSprite/SubpixelSprite field set): 164 bytes, alignment 4
// (rev 15: the pixelated sampling flag; rev 16: the extended content
// mask).
type SceneSpriteRecord struct {
	Order          uint32
	Padding        uint32
	Bounds         SceneBounds
	ContentMask    SceneContentMask
	Color          SceneColor
	Tile           SceneTileRecord
	Transformation [6]uint32 // rotation-scale 2x2 then translation xy, f32 bits
	// Pixelated is 0 (bilinear, the pinned default) or 1 (point
	// sampling, CSS image-rendering: pixelated).
	Pixelated  uint32
	RecordSize uint32
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
// (the pinned PolychromeSprite): 144 bytes, alignment 4 (rev 15: the
// pixelated sampling flag; rev 16: the extended content mask).
type ScenePolychromeSpriteRecord struct {
	Order           uint32
	Grayscale       uint32
	Opacity         uint32
	CornerSmoothing uint32
	Bounds          SceneBounds
	ContentMask     SceneContentMask
	CornerRadii     [4]uint32
	Tile            SceneTileRecord
	// Pixelated is 0 (bilinear, the pinned default) or 1 (point
	// sampling, CSS image-rendering: pixelated).
	Pixelated  uint32
	RecordSize uint32
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
	// SceneCommandBeginEffect begins an effect group (rev 16, ticket
	// 50): the command carries the effect record index in BoundaryIndex
	// and the pool target slot in TargetIndex (FilterTarget 1).
	SceneCommandBeginEffect uint32 = 3
	// SceneCommandEndEffect finishes an effect group: BoundaryIndex is
	// the opening record's index, ClosingBoundaryIndex the closing
	// marker's.
	SceneCommandEndEffect uint32 = 4
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

	// SceneBatchEffectBoundary is the effect-group marker tag (rev 16;
	// never a batch command - markers compile to begin/end effect
	// commands).
	SceneBatchEffectBoundary uint32 = 10
)

// SceneCommandRecord mirrors GpuiGoSceneCommandRecord: 56 bytes,
// alignment 4 (rev 15: the sprite batches' pixelated flag).
type SceneCommandRecord struct {
	CommandKind              uint32
	PrimitiveKind            uint32
	RangeStart               uint32
	RangeEnd                 uint32
	Smoothed                 uint32
	Pixelated                uint32 // rev 15: sprite batches' point-sampler flag
	TextureIndex             uint32
	RasterizationVertexCount uint32
	SpriteCount              uint32
	BoundaryIndex            uint32
	ClosingBoundaryIndex     uint32
	FilterTarget             uint32 // 0 inline, 1 isolated
	TargetIndex              uint32
	RecordSize               uint32
}

// SceneRequirementsRecord mirrors GpuiGoSceneRequirementsRecord: 48
// bytes, alignment 4 (rev 16 appended isolated_effect_count; the
// isolated_target_count is the pool high-water mark over BOTH group
// kinds).
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
	// IsolatedEffectCount is the isolated effect groups composited
	// (rev 16, ticket 50).
	IsolatedEffectCount uint32
}

// Effect-group flag bits of SceneEffectRecord::Flags (rev 16, ticket
// 50; bits 8 and up are reserved zero).
const (
	// SceneEffectFlagIsStart opens the group (a start marker).
	SceneEffectFlagIsStart uint32 = 1
	// SceneEffectFlagHasTransform carries a 2x3 transform (ticket 51
	// wires the matrix composite).
	SceneEffectFlagHasTransform uint32 = 2
	// SceneEffectFlagHasOpacity composites the subtree once at Opacity
	// (group opacity - the R16 core case).
	SceneEffectFlagHasOpacity uint32 = 4
	// SceneEffectFlagHasBlend composites with BlendMode (ticket 52).
	SceneEffectFlagHasBlend uint32 = 8
	// SceneEffectFlagHasMask composites dst_in against a mask source
	// (ticket 52).
	SceneEffectFlagHasMask uint32 = 16
	// SceneEffectFlagPropertyNode marks a property node (ticket 51).
	SceneEffectFlagPropertyNode uint32 = 32
	// SceneEffectFlagRoundedClipOnly clips the subtree to the rounded
	// bounds (a nested rounded clip).
	SceneEffectFlagRoundedClipOnly uint32 = 64
	// SceneEffectFlagClipToBounds clips the composite to the group's
	// rounded bounds (0 lets content bleed, like content filters).
	SceneEffectFlagClipToBounds uint32 = 128
)

// SceneEffectRecord mirrors GpuiGoSceneEffectRecord (rev 16, ticket
// 50; primitive class 10): the effect-group marker carrying the whole
// Chromium effect-node payload - group opacity, the 2x3 transform,
// blend mode, mask reference, corner radii and the property-node id.
// Start and end records pair through the SAME boundary machinery as
// filter boundaries, and both kinds share one group stack in the plan
// (command kinds 3/4). This revision wires opacity and the rounded
// clip; the transform/blend/mask/property-node fields are carried and
// validated (MaskKind must be 0) and wired by tickets 51/52.
// 116 bytes, alignment 4.
type SceneEffectRecord struct {
	Order       uint32
	Flags       uint32
	Bounds      SceneBounds
	ContentMask SceneBounds
	// Transform is the row-major 2x3 [m00, m01, m10, m11, tx, ty] (f32
	// bits; requires an invertible 2x2 when HasTransform is set).
	Transform [6]uint32
	// Opacity is the group opacity in [0, 1] (f32 bits).
	Opacity uint32
	// BlendMode is 0 (src-over) .. 16 (plus-lighter).
	BlendMode uint32
	// MaskKind is 0 (none) this revision; 1 gradient, 2 atlas tile, 3
	// coverage arrive with ticket 52.
	MaskKind uint32
	// MaskReference is the mask source reference (0 when MaskKind is 0).
	MaskReference uint32
	// MaskMode is 0 (alpha) or 1 (luminance).
	MaskMode uint32
	// NodeID is the property-node id (0 = none; non-zero when
	// PropertyNode is set).
	NodeID uint32
	// CornerRadii is the group's rounded-clip radii (TL, TR, BR, BL,
	// f32 bits).
	CornerRadii [4]uint32
	// CornerSmoothing is in [0, 1] (f32 bits).
	CornerSmoothing uint32
	Reserved        uint32
	RecordSize      uint32
}

// SceneGradientStop mirrors GpuiGoSceneGradientStop (rev 16, ticket
// 52; the design's appendix A.3): one gradient stop - the position in
// [0, 1] along the gradient's parameter and an RGBA8 straight sRGB
// color packed one byte per lane, r | g<<8 | b<<16 | a<<24. The CSS
// color-stop fixup is the CALLER's; the service interpolates the fixed
// stops. 8 bytes, alignment 4.
type SceneGradientStop struct {
	// Position is the stop's position along the gradient parameter
	// (f32 bits).
	Position uint32
	// Rgba is the stop's straight sRGBA color, one byte per lane.
	Rgba uint32
}

// Gradient kind tags of SceneGradientRecord::Kind (rev 16, ticket 52).
const (
	// SceneGradientKindLinear is a linear gradient along the record's
	// [x0, y0, x1, y1] line.
	SceneGradientKindLinear uint32 = 0
	// SceneGradientKindRadial is a radial gradient (circle or ellipse)
	// around [cx, cy].
	SceneGradientKindRadial uint32 = 1
	// SceneGradientKindConic is a conic gradient sweeping from
	// start_angle.
	SceneGradientKindConic uint32 = 2
)

// Gradient tile-mode tags of SceneGradientRecord::Tile (rev 16, ticket
// 52): the wrap rule applied to the parameter before the ramp is
// sampled.
const (
	// SceneGradientTilePad clamps at the ramp's own end colors (the
	// default).
	SceneGradientTilePad uint32 = 0
	// SceneGradientTileRepeat wraps with fract(t) - CSS repeating-*.
	SceneGradientTileRepeat uint32 = 1
	// SceneGradientTileReflect mirrors around the ends (a triangle
	// wave).
	SceneGradientTileReflect uint32 = 2
)

// SceneGradientRecord mirrors GpuiGoSceneGradientRecord (rev 16,
// ticket 52; the design's appendix A.3): one scene-level gradient
// table entry - the kind, tile mode, geometry and stop count; the
// service bakes the stops into a 1024x1 premultiplied-sRGB ramp row
// (row index = the gradient's own table index, assigned at insert).
// The stops travel as the parallel bulk array of InsertGradient. 56
// bytes, alignment 4.
type SceneGradientRecord struct {
	// Kind is a gradient kind tag.
	Kind uint32
	// Tile is a gradient tile-mode tag.
	Tile uint32
	// ColorSpace is 0 (sRGB, this revision's only space; 1 Oklab is
	// reserved and rejected).
	ColorSpace uint32
	// Geometry is the kind-specific geometry, f32 bits, device pixels:
	// linear [x0, y0, x1, y1], radial [cx, cy, r0, r1, aspect], conic
	// [cx, cy, start_angle, rotation]; the rest zero.
	Geometry [8]uint32
	// StopCount is the stop count (1..=64).
	StopCount uint32
	// StopRow is the ramp row index (0 on insert; the service assigns
	// the gradient's table index, 1:1).
	StopRow    uint32
	RecordSize uint32
}

// NewSceneGradientRecord returns a validated empty linear gradient
// record (stop_row 0; the service assigns it).
func NewSceneGradientRecord() SceneGradientRecord {
	return SceneGradientRecord{
		Kind:       SceneGradientKindLinear,
		Tile:       SceneGradientTilePad,
		RecordSize: uint32(unsafe.Sizeof(SceneGradientRecord{})),
	}
}

// SceneCoverageRecord mirrors GpuiGoSceneCoverageRecord (rev 16,
// ticket 52; the design's appendix A.4): one clip-path coverage entry -
// a reference to one of the scene's path records plus its device
// bounds. The renderer rasterizes the referenced path into the
// per-surface coverage target when the frame's effect groups reference
// the entry (mask kind 3). 24 bytes, alignment 4.
type SceneCoverageRecord struct {
	// PathIndex is the referenced path record's index in the scene's
	// path array.
	PathIndex uint32
	// Bounds is the path's device bounds.
	Bounds     SceneBounds
	RecordSize uint32
}

// NewSceneCoverageRecord returns a validated empty coverage entry.
func NewSceneCoverageRecord() SceneCoverageRecord {
	return SceneCoverageRecord{
		RecordSize: uint32(unsafe.Sizeof(SceneCoverageRecord{})),
	}
}

// Filter-op kinds of SceneFilterOpRecord::Kind (rev 16, ticket 52; the
// design's appendix A.5).
const (
	// SceneFilterOpBlur is R15's chain blur (re-stated for reference -
	// not insertable; the R15 filter tags carry the chain's blurs).
	SceneFilterOpBlur uint32 = 0
	// SceneFilterOpColorMatrix is R15's chain matrix (re-stated for
	// reference - not insertable).
	SceneFilterOpColorMatrix uint32 = 1
	// SceneFilterOpDropShadow is a CSS drop-shadow(): the only
	// insertable kind this revision.
	SceneFilterOpDropShadow uint32 = 2
)

// SceneFilterOpRecord mirrors GpuiGoSceneFilterOpRecord (rev 16,
// ticket 52; the design's appendix A.5): one drop-shadow op - offset,
// the CSS blur radius (the renderer derives sigma = radius/2) and the
// shadow color. 40 bytes, alignment 4.
type SceneFilterOpRecord struct {
	// Kind is a filter-op kind tag (only DropShadow is insertable).
	Kind uint32
	// Offset is the shadow offset dx, dy (device pixels, f32 bits).
	Offset [2]uint32
	// BlurRadius is the CSS blur radius (device pixels, f32 bits).
	BlurRadius uint32
	// Color is the shadow color (a solid SceneColor).
	Color      SceneColor
	RecordSize uint32
}

// NewSceneFilterOpRecord returns a validated empty drop-shadow op.
func NewSceneFilterOpRecord() SceneFilterOpRecord {
	return SceneFilterOpRecord{
		Kind:       SceneFilterOpDropShadow,
		RecordSize: uint32(unsafe.Sizeof(SceneFilterOpRecord{})),
	}
}

// Batch op kinds (rev 16, ticket 52): the packed batch stream's
// gradient, coverage and op-chain entries.
const (
	// SceneBatchOpGradient is one SceneGradientRecord (14 words)
	// followed by its stop rows (2 words each).
	SceneBatchOpGradient uint32 = 14
	// SceneBatchOpCoverage is one SceneCoverageRecord (6 words).
	SceneBatchOpCoverage uint32 = 15
	// SceneBatchOpFilterOpChain is one op-count word followed by that
	// many SceneFilterOpRecord rows (10 words each).
	SceneBatchOpFilterOpChain uint32 = 16
)

// NewSceneEffectRecord returns a validated empty effect record (the
// pinned unit transform and full opacity).
func NewSceneEffectRecord() SceneEffectRecord {
	return SceneEffectRecord{
		Transform: [6]uint32{
			unitF32Bits, 0, 0, unitF32Bits, 0, 0,
		},
		Opacity:    oneF32Bits,
		RecordSize: uint32(unsafe.Sizeof(SceneEffectRecord{})),
	}
}

// SceneMetaRecord mirrors GpuiGoSceneMetaRecord: 60 bytes, alignment 4
// (ticket10 added the three sprite counts before IsFinished; rev 16
// appended effect_count before IsFinished).
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
	PathCount uint32
	// EffectCount is the effect-group marker count (rev 16, ticket 50).
	EffectCount uint32
	IsFinished  uint32
	RecordSize  uint32
	// GradientCount is the gradient table entry count (rev 16, ticket
	// 52; appended after RecordSize).
	GradientCount uint32
	// CoverageCount is the coverage-mask entry count (rev 16, ticket
	// 52).
	CoverageCount uint32
	// FilterChainCount is the filter-op chain count (rev 16, ticket
	// 52: the drop-shadow chains).
	FilterChainCount uint32
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

// sceneTable mirrors the native GpuiGoSceneTable: 30 function pointers
// then the self-check scalars; 320 bytes, alignment 8 (ticket16 added
// the three path entries and the three path record sizes; version 4
// appended insert_batch; version 6 appended insert_effect/effect_dump
// and the effect record scalars; version 7 appended update_property).
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
	insertBatch              uintptr
	insertEffect             uintptr
	effectDump               uintptr
	updateProperty           uintptr
	insertGradient           uintptr
	gradientDump             uintptr
	insertCoverage           uintptr
	insertFilterOpChain      uintptr
	tessCacheStats           uintptr
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
	sizeOfEffectRecord       uint32
	sizeOfContentMask        uint32
	maxSceneGroups           uint32
	sizeOfGradientRecord     uint32
	sizeOfGradientStop       uint32
	maxGradientStops         uint32
	sizeOfCoverageRecord     uint32
	sizeOfFilterOpRecord     uint32
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
	if t.sizeOfEffectRecord != uint32(unsafe.Sizeof(SceneEffectRecord{})) {
		return abi("scene size_of_effect_record", fmt.Sprint(unsafe.Sizeof(SceneEffectRecord{})), fmt.Sprint(t.sizeOfEffectRecord))
	}
	if t.sizeOfContentMask != uint32(unsafe.Sizeof(SceneContentMask{})) {
		return abi("scene size_of_content_mask", fmt.Sprint(unsafe.Sizeof(SceneContentMask{})), fmt.Sprint(t.sizeOfContentMask))
	}
	if t.maxSceneGroups == 0 {
		return &CapabilityError{Path: path, Detail: "scene max_scene_groups is zero"}
	}
	if t.sizeOfGradientRecord != uint32(unsafe.Sizeof(SceneGradientRecord{})) {
		return abi("scene size_of_gradient_record", fmt.Sprint(unsafe.Sizeof(SceneGradientRecord{})), fmt.Sprint(t.sizeOfGradientRecord))
	}
	if t.sizeOfGradientStop != uint32(unsafe.Sizeof(SceneGradientStop{})) {
		return abi("scene size_of_gradient_stop", fmt.Sprint(unsafe.Sizeof(SceneGradientStop{})), fmt.Sprint(t.sizeOfGradientStop))
	}
	if t.maxGradientStops != MaxGradientStops {
		return abi("scene max_gradient_stops", fmt.Sprint(MaxGradientStops), fmt.Sprint(t.maxGradientStops))
	}
	if t.sizeOfCoverageRecord != uint32(unsafe.Sizeof(SceneCoverageRecord{})) {
		return abi("scene size_of_coverage_record", fmt.Sprint(unsafe.Sizeof(SceneCoverageRecord{})), fmt.Sprint(t.sizeOfCoverageRecord))
	}
	if t.sizeOfFilterOpRecord != uint32(unsafe.Sizeof(SceneFilterOpRecord{})) {
		return abi("scene size_of_filter_op_record", fmt.Sprint(unsafe.Sizeof(SceneFilterOpRecord{})), fmt.Sprint(t.sizeOfFilterOpRecord))
	}
	slots := [...]uintptr{
		t.create, t.clear, t.dispose, t.pushLayer, t.popLayer, t.raiseOrderFloor,
		t.insertQuad, t.insertShadow, t.insertUnderline, t.insertBackdrop,
		t.insertBoundary, t.insertSurface, t.insertMonochromeSprite,
		t.insertSubpixelSprite, t.insertPolychromeSprite, t.replay, t.finish,
		t.length, t.meta, t.dump, t.planDump, t.requirements, t.panicProbe,
		t.insertPath, t.pathDump, t.pathScript, t.insertBatch, t.insertEffect,
		t.effectDump, t.updateProperty, t.insertGradient, t.gradientDump,
		t.insertCoverage, t.insertFilterOpChain, t.tessCacheStats,
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
		"scene_insert_batch", "scene_insert_effect", "scene_effect_dump",
		"scene_update_property", "scene_insert_gradient", "scene_gradient_dump",
		"scene_insert_coverage", "scene_insert_filter_op_chain", "scene_tess_cache_stats",
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
//
// Inserts, layer pushes/pops and order-floor raises are buffered and sent
// in one native call (scene_batch.go); every other operation flushes
// them first.
type Scene struct {
	svc    *SceneService
	handle SceneHandle
	busy   atomic.Bool
	// batch is the pending op stream (nil while nothing is pending).
	batch *[]uint32
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
	sc.releaseBatch()
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
	sc.releaseBatch()
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
	return enqueueBatch(sc, "scene_push_layer", sceneBatchPushLayer, &bounds)
}

// PopLayer pops the innermost layer (the pinned Scene::pop_layer).
func (sc *Scene) PopLayer() error {
	return enqueueBatch[uint32](sc, "scene_pop_layer", sceneBatchPopLayer, nil)
}

// RaiseOrderFloor raises the deferred-draw order floor so every
// primitive inserted afterwards sorts above everything inserted before
// (the pinned Scene::raise_order_floor).
func (sc *Scene) RaiseOrderFloor() error {
	return enqueueBatch[uint32](sc, "scene_raise_order_floor", sceneBatchRaiseOrderFloor, nil)
}

// InsertQuad inserts a quad record (order must be 0; the kernel assigns
// the draw order).
func (sc *Scene) InsertQuad(rec *SceneQuadRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_quad")
	}
	if err := checkQuadRecord(rec); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_quad", sceneBatchQuad, rec)
}

// InsertShadow inserts a shadow record.
func (sc *Scene) InsertShadow(rec *SceneShadowRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_shadow")
	}
	if err := checkShadowRecord(rec); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_shadow", sceneBatchShadow, rec)
}

// InsertUnderline inserts an underline record.
func (sc *Scene) InsertUnderline(rec *SceneUnderlineRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_underline")
	}
	if err := checkUnderlineRecord(rec); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_underline", sceneBatchUnderline, rec)
}

// InsertBackdropFilter inserts a backdrop-filter record.
func (sc *Scene) InsertBackdropFilter(rec *SceneFilterRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_backdrop_filter")
	}
	if err := checkFilterRecord("scene_insert_backdrop_filter", rec, sc.svc.table.maxFilters); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_backdrop_filter", sceneBatchBackdrop, rec)
}

// InsertFilterBoundary inserts a content-filter boundary marker (start
// or end). A scene cannot close an unopened group; unmatched markers are
// handled by the plan (an unmatched start renders inline).
func (sc *Scene) InsertFilterBoundary(rec *SceneFilterRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_filter_boundary")
	}
	if err := checkFilterRecord("scene_insert_filter_boundary", rec, sc.svc.table.maxFilters); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_filter_boundary", sceneBatchBoundary, rec)
}

// InsertEffect inserts an effect-group marker (rev 16, ticket 50):
// Flags' IsStart bit decides start/end. Start and end must carry the
// same snapshot (the pinned with_filter_layer rule); unmatched starts
// render inline. The group start that would exceed MAX_SCENE_GROUPS
// (1024, shared with the filter boundaries) fails with ErrSceneLimit
// before native entry is attempted at the flush.
func (sc *Scene) InsertEffect(rec *SceneEffectRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_effect")
	}
	if err := checkEffectRecord("scene_insert_effect", rec); err != nil {
		return err
	}
	kind := sceneBatchEffectEnd
	if rec.Flags&SceneEffectFlagIsStart != 0 {
		kind = sceneBatchEffectStart
	}
	return enqueueBatch(sc, "scene_insert_effect", kind, rec)
}

// EffectDump bulk-dumps the finished scene's effect records with their
// kernel-assigned orders (rev 16, ticket 50). Requires Finish; a
// too-small capacity retries once with the reported count.
func (sc *Scene) EffectDump() ([]SceneEffectRecord, error) {
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
	want := uint32(16)
	for attempt := 0; attempt < 2; attempt++ {
		out := make([]SceneEffectRecord, want)
		var count uint32
		var recPtr uintptr
		if want > 0 {
			recPtr = uintptr(unsafe.Pointer(&out[0]))
		}
		code, err := sc.withScene("scene_effect_dump", func() (int32, error) {
			return callSceneEffectDump(sc.svc.table.effectDump, uint64(sc.handle), recPtr, want, &count)
		})
		if err != nil {
			return nil, err
		}
		if code == sceneErrCapacity {
			want = count
			continue
		}
		if code != sceneStatusOK {
			return nil, sceneStatusError(code, "scene_effect_dump")
		}
		return out[:count], nil
	}
	return nil, sceneStatusError(sceneErrCapacity, "scene_effect_dump capacity retry failed")
}

// InsertGradient inserts one gradient table entry with its bulk stop
// array (rev 16, ticket 52): the service bakes the 1024x1
// premultiplied-sRGB ramp row and assigns StopRow (the gradient's own
// table index, returned). The gradient table is a scene-level table
// the quads' tag-4 backgrounds and the effect groups' gradient masks
// reference by index.
func (sc *Scene) InsertGradient(rec *SceneGradientRecord, stops []SceneGradientStop) (uint32, error) {
	if rec == nil {
		return 0, sceneStatusError(sceneErrNullArg, "scene_insert_gradient")
	}
	if err := checkGradientRecord("scene_insert_gradient", rec, stops); err != nil {
		return 0, err
	}
	if err := sc.flushBatch(); err != nil {
		return 0, err
	}
	var stopsPtr *SceneGradientStop
	if len(stops) > 0 {
		stopsPtr = &stops[0]
	}
	var index uint32
	code, err := sc.withScene("scene_insert_gradient", func() (int32, error) {
		return callSceneInsertGradient(sc.svc.table.insertGradient, uint64(sc.handle), rec, stopsPtr, uint32(len(stops)), &index)
	})
	if err != nil {
		return 0, err
	}
	if code != sceneStatusOK {
		return 0, sceneStatusError(code, "scene_insert_gradient")
	}
	return index, nil
}

// GradientDump bulk-dumps the finished scene's gradient records and
// the concatenated stop array (rev 16, ticket 52; the
// path_dump capacity protocol - both capacities retry once with the
// reported counts).
func (sc *Scene) GradientDump() ([]SceneGradientRecord, []SceneGradientStop, error) {
	if err := sc.flushBatch(); err != nil {
		return nil, nil, err
	}
	wantRecords, wantStops := uint32(16), uint32(64)
	for attempt := 0; attempt < 2; attempt++ {
		records := make([]SceneGradientRecord, wantRecords)
		stops := make([]SceneGradientStop, wantStops)
		var recordCount, stopCount uint32
		var recPtr, stopPtr uintptr
		if wantRecords > 0 {
			recPtr = uintptr(unsafe.Pointer(&records[0]))
		}
		if wantStops > 0 {
			stopPtr = uintptr(unsafe.Pointer(&stops[0]))
		}
		code, err := sc.withScene("scene_gradient_dump", func() (int32, error) {
			return callSceneGradientDump(sc.svc.table.gradientDump, uint64(sc.handle), recPtr, stopPtr, wantRecords, wantStops, &recordCount, &stopCount)
		})
		if err != nil {
			return nil, nil, err
		}
		if code == sceneErrCapacity {
			wantRecords, wantStops = recordCount, stopCount
			continue
		}
		if code != sceneStatusOK {
			return nil, nil, sceneStatusError(code, "scene_gradient_dump")
		}
		return records[:recordCount], stops[:stopCount], nil
	}
	return nil, nil, sceneStatusError(sceneErrCapacity, "scene_gradient_dump capacity retry failed")
}

// InsertCoverage inserts one clip-path coverage entry (rev 16, ticket
// 52): a path reference plus its device bounds. The renderer
// rasterizes the referenced path into the per-surface coverage target
// when the frame's effect groups reference the entry (mask kind 3).
func (sc *Scene) InsertCoverage(rec *SceneCoverageRecord) (uint32, error) {
	if rec == nil {
		return 0, sceneStatusError(sceneErrNullArg, "scene_insert_coverage")
	}
	if err := checkCoverageRecord("scene_insert_coverage", rec); err != nil {
		return 0, err
	}
	if err := sc.flushBatch(); err != nil {
		return 0, err
	}
	var index uint32
	code, err := sc.withScene("scene_insert_coverage", func() (int32, error) {
		return callSceneInsertCoverage(sc.svc.table.insertCoverage, uint64(sc.handle), rec, &index)
	})
	if err != nil {
		return 0, err
	}
	if code != sceneStatusOK {
		return 0, sceneStatusError(code, "scene_insert_coverage")
	}
	return index, nil
}

// InsertFilterOpChain inserts one drop-shadow op chain (rev 16,
// ticket 52; the design's appendix A.5) and returns the chain index
// the R15 filter records reference through their appended
// OpChainIndex/OpChainCount (0/0 = the R15 legacy chain, bit-exact).
func (sc *Scene) InsertFilterOpChain(ops []SceneFilterOpRecord) (uint32, error) {
	if len(ops) == 0 {
		return 0, sceneStatusError(sceneErrNullArg, "scene_insert_filter_op_chain: empty chain")
	}
	for i := range ops {
		if err := checkFilterOpRecord("scene_insert_filter_op_chain", &ops[i]); err != nil {
			return 0, err
		}
	}
	if err := sc.flushBatch(); err != nil {
		return 0, err
	}
	var index uint32
	code, err := sc.withScene("scene_insert_filter_op_chain", func() (int32, error) {
		return callSceneInsertFilterOpChain(sc.svc.table.insertFilterOpChain, uint64(sc.handle), &ops[0], uint32(len(ops)), &index)
	})
	if err != nil {
		return 0, err
	}
	if code != sceneStatusOK {
		return 0, sceneStatusError(code, "scene_insert_filter_op_chain")
	}
	return index, nil
}

// UpdateProperty mutates a finished scene's property-node records in
// place (rev 16, ticket 51; the design's appendix A.7): every effect
// record carrying NodeID takes the new value - the 2x3 matrix
// (ScenePropertyMatrix, six f32 bits row-major [m00, m01, m10, m11,
// tx, ty]; the records must carry SceneEffectFlagHasTransform) or the
// group opacity (ScenePropertyOpacity, one f32 bit word; the records
// must carry SceneEffectFlagHasOpacity). No re-sort, no re-plan: the
// render walk consults the mutated records, so the same scene handle
// re-presents with the updated value. Returns the group's old and new
// mapped device bounds - the damage union the caller re-presents
// (mapped through the old/new matrix; the record bounds when the node
// has no transform).
//
// Requires Finish (ErrSceneNotFinished before); a stale node id, an
// unknown kind, a wrong value count or a value the insert validation
// would reject answers ErrSceneBadValue.
func (sc *Scene) UpdateProperty(nodeID uint32, kind uint32, values []uint32) (oldBounds, newBounds SceneBounds, err error) {
	if nodeID == 0 {
		return SceneBounds{}, SceneBounds{}, sceneStatusError(sceneErrBadValue, "scene_update_property: node id is zero")
	}
	want := 0
	switch kind {
	case ScenePropertyMatrix:
		want = 6
	case ScenePropertyOpacity:
		want = 1
	default:
		return SceneBounds{}, SceneBounds{}, sceneStatusError(sceneErrBadValue, fmt.Sprintf("scene_update_property: unknown kind %d", kind))
	}
	if len(values) != want {
		return SceneBounds{}, SceneBounds{}, sceneStatusError(sceneErrBadValue, fmt.Sprintf("scene_update_property: kind %d needs %d values, got %d", kind, want, len(values)))
	}
	// The value validation mirrors the insert rules (the native side
	// validates again before mutating).
	if kind == ScenePropertyMatrix {
		m00 := float64(math.Float32frombits(values[0]))
		m01 := float64(math.Float32frombits(values[1]))
		m10 := float64(math.Float32frombits(values[2]))
		m11 := float64(math.Float32frombits(values[3]))
		tx := float64(math.Float32frombits(values[4]))
		ty := float64(math.Float32frombits(values[5]))
		for _, value := range []float64{m00, m01, m10, m11, tx, ty} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return SceneBounds{}, SceneBounds{}, badRecord("scene_update_property", "matrix value non-finite")
			}
		}
		determinant := m00*m11 - m01*m10
		if math.IsNaN(determinant) || math.IsInf(determinant, 0) || determinant == 0 {
			return SceneBounds{}, SceneBounds{}, badRecord("scene_update_property", "matrix 2x2 is singular")
		}
	} else {
		opacity := float64(math.Float32frombits(values[0]))
		if math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
			return SceneBounds{}, SceneBounds{}, badRecord("scene_update_property", "opacity out of range")
		}
	}
	if err := sc.flushBatch(); err != nil {
		return SceneBounds{}, SceneBounds{}, err
	}
	code, err := sc.withScene("scene_update_property", func() (int32, error) {
		return callSceneUpdateProperty(
			sc.svc.table.updateProperty,
			uint64(sc.handle),
			nodeID,
			kind,
			uintptr(unsafe.Pointer(&values[0])),
			uint32(len(values)),
			&oldBounds,
			&newBounds,
		)
	})
	if err != nil {
		return SceneBounds{}, SceneBounds{}, err
	}
	if code != sceneStatusOK {
		return SceneBounds{}, SceneBounds{}, sceneStatusError(code, "scene_update_property")
	}
	return oldBounds, newBounds, nil
}

// InsertSurface inserts a surface record with its paired opacity (the
// pinned insert_surface path: the opacity stays paired with the surface
// through the finish sort and replay).
func (sc *Scene) InsertSurface(rec *SceneSurfaceRecord, opacity float32) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_surface")
	}
	if err := checkSurfaceRecord(rec); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_surface", sceneBatchSurface, rec, math.Float32bits(opacity))
}

// InsertMonochromeSprite inserts a monochrome sprite record (an R8 atlas
// tile drawn with the mask color). The tile's texture kind must be the
// monochrome pool.
func (sc *Scene) InsertMonochromeSprite(rec *SceneSpriteRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_monochrome_sprite")
	}
	if err := checkMaskSpriteRecord("scene_insert_monochrome_sprite", rec, SceneSpriteTextureMonochrome); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_monochrome_sprite", sceneBatchMonochromeSprite, rec)
}

// InsertSubpixelSprite inserts a subpixel sprite record (a BGRA LCD
// atlas tile blended with the mask color).
func (sc *Scene) InsertSubpixelSprite(rec *SceneSpriteRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_subpixel_sprite")
	}
	if err := checkMaskSpriteRecord("scene_insert_subpixel_sprite", rec, SceneSpriteTextureSubpixel); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_subpixel_sprite", sceneBatchSubpixelSprite, rec)
}

// InsertPolychromeSprite inserts a polychrome sprite record (a BGRA
// color atlas tile drawn with its own opacity and corner smoothing).
func (sc *Scene) InsertPolychromeSprite(rec *ScenePolychromeSpriteRecord) error {
	if rec == nil {
		return sceneStatusError(sceneErrNullArg, "scene_insert_polychrome_sprite")
	}
	if err := checkPolychromeSpriteRecord(rec); err != nil {
		return err
	}
	return enqueueBatch(sc, "scene_insert_polychrome_sprite", sceneBatchPolychromeSprite, rec)
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
	if err := prev.flushBatch(); err != nil {
		return err
	}
	if err := sc.flushBatch(); err != nil {
		return err
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
	if err := sc.flushBatch(); err != nil {
		return err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, nil, err
	}
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

// TessCacheStats reports the path tessellation LRU cache's counters
// (rev 16, browser-core ticket 53): the cumulative hits, misses and
// evictions plus the live entry count. The cache is process-global
// (path_script is stateless), so the counters are cumulative across
// every scene and the Go PathBuilder.Build callers. Note the two-call
// capacity protocol of TessellatePath: one Build() through this client
// is one miss (the capacity probe tessellates and caches) followed by
// one hit (the data call).
type TessCacheStats struct {
	Hits      uint32
	Misses    uint32
	Evictions uint32
	Entries   uint32
}

// TessCacheStats reads the tessellation cache's counters through the
// scene service's stateless stats entry.
func (s *SceneService) TessCacheStats() (TessCacheStats, error) {
	if s.lib.released.Load() {
		return TessCacheStats{}, ErrClosed
	}
	var stats TessCacheStats
	code, err := callSceneTessCacheStats(s.table.tessCacheStats, &stats.Hits, &stats.Misses, &stats.Evictions, &stats.Entries)
	if err != nil {
		return TessCacheStats{}, err
	}
	if code != sceneStatusOK {
		return TessCacheStats{}, sceneStatusError(code, "scene_tess_cache_stats")
	}
	return stats, nil
}

// Finish sorts the primitive arrays and compiles the render plan (the
// pinned Scene::finish). Plan/requirements/dump reads require it.
func (sc *Scene) Finish() error {
	if err := sc.flushBatch(); err != nil {
		return err
	}
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
	if err := sc.flushBatch(); err != nil {
		return 0, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return SceneMetaRecord{}, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return SceneRequirementsRecord{}, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, nil, err
	}
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
	if err := sc.flushBatch(); err != nil {
		return nil, err
	}
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
