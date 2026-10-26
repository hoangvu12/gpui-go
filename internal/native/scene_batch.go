// Batched scene inserts (gpui-perf ticket 05, scene service version 4).
//
// One native call per primitive was the dominant native-call cost of a
// frame (one SyscallN per quad, sprite, layer push...). The Scene wrapper
// now appends every insert, layer push/pop and order-floor raise to a
// packed word stream and hands the stream to the native insert_batch
// entry in one call, before any operation that reads or finishes the
// scene (Finish, Len, Meta, dumps, Replay, InsertPath) and whenever the
// stream reaches sceneBatchFlushWords. The native side applies the ops
// in order under one scene lock with the exact validation and state
// transitions of the single-op entries, so draw orders, the op log and
// the plan are identical.
//
// Errors: the record checks that need no scene state (record size, zero
// order, color tag, boolean and enum ranges, finite floats, tile kind and
// bounds) run here at insert time, so a malformed insert still fails at
// its call with the same status as before. State errors (capacity
// limits, a failed scene, a stale handle) now surface at the flush, from
// the operation that triggered it; the ops after the failing one are
// dropped.

package native

import (
	"fmt"
	"math"
	"sync"
	"unsafe"
)

// Batch op kinds (mirror batch_op in reference/native/src/scene.rs).
const (
	sceneBatchQuad             uint32 = 0
	sceneBatchShadow           uint32 = 1
	sceneBatchUnderline        uint32 = 2
	sceneBatchBackdrop         uint32 = 3
	sceneBatchBoundary         uint32 = 4
	sceneBatchSurface          uint32 = 5
	sceneBatchMonochromeSprite uint32 = 6
	sceneBatchSubpixelSprite   uint32 = 7
	sceneBatchPolychromeSprite uint32 = 8
	sceneBatchPushLayer        uint32 = 9
	sceneBatchPopLayer         uint32 = 10
	sceneBatchRaiseOrderFloor  uint32 = 11
	// sceneBatchEffectStart is the effect-group start marker (rev 16,
	// ticket 50): the SceneEffectRecord payload with
	// Flags' IsStart bit forced on.
	sceneBatchEffectStart uint32 = 12
	// sceneBatchEffectEnd is the effect-group end marker: the
	// SceneEffectRecord payload with IsStart forced off.
	sceneBatchEffectEnd uint32 = 13
)

var sceneBatchOpNames = [...]string{
	"quad", "shadow", "underline", "backdrop filter", "filter boundary",
	"surface", "monochrome sprite", "subpixel sprite", "polychrome sprite",
	"push layer", "pop layer", "raise order floor", "effect start", "effect end",
}

// sceneBatchFlushWords bounds one batch (64 KiB, about 500 sprite
// records): a stream this long is flushed by the insert that reached it.
// The buffer capacity adds room for the largest op (a 2-word header, a
// 33-word quad record and a surface opacity word), so a buffer never
// grows.
const (
	sceneBatchFlushWords = 1 << 14
	sceneBatchCapWords   = sceneBatchFlushWords + 64
)

// sceneBatchFree is a small free list of stream buffers. A scene holds a
// buffer only between its first buffered op and the next flush, so a
// frame's scene reuses the buffer of the previous one. (A sync.Pool
// would be emptied by every GC cycle and the buffer reallocated.)
var sceneBatchFree struct {
	sync.Mutex
	bufs []*[]uint32
}

func getSceneBatchBuffer() *[]uint32 {
	sceneBatchFree.Lock()
	defer sceneBatchFree.Unlock()
	if n := len(sceneBatchFree.bufs); n > 0 {
		buf := sceneBatchFree.bufs[n-1]
		sceneBatchFree.bufs = sceneBatchFree.bufs[:n-1]
		return buf
	}
	buf := make([]uint32, 0, sceneBatchCapWords)
	return &buf
}

func putSceneBatchBuffer(buf *[]uint32) {
	*buf = (*buf)[:0]
	sceneBatchFree.Lock()
	if len(sceneBatchFree.bufs) < 8 {
		sceneBatchFree.bufs = append(sceneBatchFree.bufs, buf)
	}
	sceneBatchFree.Unlock()
}

// recordWords views a 4-byte-aligned record as its u32 words.
func recordWords[T any](rec *T) []uint32 {
	return unsafe.Slice((*uint32)(unsafe.Pointer(rec)), unsafe.Sizeof(*rec)/4)
}

// enqueue appends one op to the scene's stream, flushing when it is
// full. extra is appended after the record words (the surface opacity).
func enqueueBatch[T any](sc *Scene, op string, kind uint32, rec *T, extra ...uint32) error {
	if sc.busy.Load() {
		return sceneStatusError(sceneErrBadValue, op+" rejected: scene busy (Go busy guard, before native entry)")
	}
	if sc.batch == nil {
		sc.batch = getSceneBatchBuffer()
	}
	var words []uint32
	if rec != nil {
		words = recordWords(rec)
	}
	buf := append(*sc.batch, kind, uint32(len(words)+len(extra)))
	buf = append(buf, words...)
	buf = append(buf, extra...)
	*sc.batch = buf
	if len(buf) >= sceneBatchFlushWords {
		return sc.flushBatch()
	}
	return nil
}

// flushBatch sends the pending stream to the native insert_batch entry
// and returns the buffer to the free list. It is a no-op without pending
// ops.
func (sc *Scene) flushBatch() error {
	if sc.batch == nil {
		return nil
	}
	buf := sc.batch
	sc.batch = nil
	defer putSceneBatchBuffer(buf)
	words := *buf
	if len(words) == 0 {
		return nil
	}
	var applied uint32
	code, err := sc.withScene("scene_insert_batch", func() (int32, error) {
		return callSceneInsertBatch(sc.svc.table.insertBatch, uint64(sc.handle), words, &applied)
	})
	if err != nil {
		return err
	}
	if code != sceneStatusOK {
		return sceneStatusError(code, batchFailureDetail(words, applied))
	}
	return nil
}

// batchFailureDetail names the op a failed batch stopped at.
func batchFailureDetail(words []uint32, applied uint32) string {
	total, failed := 0, "unknown op"
	for rest := words; len(rest) >= 2 && int(rest[1])+2 <= len(rest); rest = rest[2+rest[1]:] {
		if total == int(applied) && int(rest[0]) < len(sceneBatchOpNames) {
			failed = sceneBatchOpNames[rest[0]]
		}
		total++
	}
	return fmt.Sprintf("scene_insert_batch: op %d of %d (%s)", applied+1, total, failed)
}

// Flush sends every pending insert to the native scene. Operations that
// read or finish the scene flush on their own; Flush is for callers that
// hand the raw scene handle to another native service.
func (sc *Scene) Flush() error { return sc.flushBatch() }

// releaseBatch drops the pending stream (Clear, Dispose) and returns the
// buffer to the free list.
func (sc *Scene) releaseBatch() {
	if sc.batch != nil {
		putSceneBatchBuffer(sc.batch)
		sc.batch = nil
	}
}

// ---------------------------------------------------------------------------
// Stateless record checks (mirror check_* in reference/native/src/scene.rs)
// ---------------------------------------------------------------------------

func badRecord(op, why string) error {
	return sceneStatusError(sceneErrBadValue, op+": "+why)
}

func checkRecordSize(op string, actual uint32, expected uintptr) error {
	if actual != uint32(expected) {
		return badRecord(op, "record_size mismatch")
	}
	return nil
}

func checkCommon(op string, size uint32, expected uintptr, order uint32) error {
	if err := checkRecordSize(op, size, expected); err != nil {
		return err
	}
	if order != 0 {
		return badRecord(op, "order must be 0")
	}
	return nil
}

func checkColor(op string, c SceneColor) error {
	if c.Tag != 0 {
		return badRecord(op, "unsupported color tag")
	}
	return nil
}

// checkBackground mirrors the native check_background (rev 15): a known
// tag and color space, finite gradient scalars, distinct stop
// positions, and a zeroed unused arm.
func checkBackground(op string, b SceneBackground) error {
	if b.Reserved != 0 {
		return badRecord(op, "background reserved word must be zero")
	}
	switch b.Tag {
	case 0:
		if b.ColorSpace > 1 {
			return badRecord(op, "unsupported background color space")
		}
		if b.Angle != 0 || b.Stop0H != 0 || b.Stop0S != 0 || b.Stop0L != 0 || b.Stop0A != 0 ||
			b.Stop0Percentage != 0 || b.Stop1H != 0 || b.Stop1S != 0 || b.Stop1L != 0 ||
			b.Stop1A != 0 || b.Stop1Percentage != 0 {
			return badRecord(op, "solid background carries gradient payload")
		}
		return nil
	case 1:
		if b.ColorSpace > 1 {
			return badRecord(op, "unsupported background color space")
		}
		for _, bits := range [11]uint32{
			b.Angle, b.Stop0H, b.Stop0S, b.Stop0L, b.Stop0A, b.Stop0Percentage,
			b.Stop1H, b.Stop1S, b.Stop1L, b.Stop1A, b.Stop1Percentage,
		} {
			if !finiteBits(bits) {
				return badRecord(op, "non-finite gradient scalar")
			}
		}
		if math.Float32frombits(b.Stop0Percentage) == math.Float32frombits(b.Stop1Percentage) {
			return badRecord(op, "gradient stop positions must differ")
		}
		if b.H != 0 || b.S != 0 || b.L != 0 || b.A != 0 {
			return badRecord(op, "gradient background carries solid payload")
		}
		return nil
	case 4:
		// rev 16, ticket 52: the gradient-table arm - the index rides the
		// ANGLE field as f32 bits (finite, non-negative, integral, under
		// the table bound; the RENDERER resolves the reference).
		if b.ColorSpace != 0 {
			return badRecord(op, "gradient-table background requires the sRGB space")
		}
		index := float64(math.Float32frombits(b.Angle))
		if math.IsNaN(index) || math.IsInf(index, 0) || index < 0 ||
			index != math.Trunc(index) || index >= 256 {
			return badRecord(op, "gradient-table background index out of range")
		}
		if b.H != 0 || b.S != 0 || b.L != 0 || b.A != 0 ||
			b.Stop0H != 0 || b.Stop0S != 0 || b.Stop0L != 0 || b.Stop0A != 0 || b.Stop0Percentage != 0 ||
			b.Stop1H != 0 || b.Stop1S != 0 || b.Stop1L != 0 || b.Stop1A != 0 || b.Stop1Percentage != 0 {
			return badRecord(op, "gradient-table background carries payload")
		}
		return nil
	default:
		return badRecord(op, "unsupported background tag")
	}
}

func checkBool01(op string, v uint32) error {
	if v > 1 {
		return badRecord(op, "boolean field out of range")
	}
	return nil
}

func finiteBits(bits uint32) bool {
	f := float64(math.Float32frombits(bits))
	return !math.IsInf(f, 0) && !math.IsNaN(f)
}

// checkContentMask validates the extended content mask (rev 16,
// ticket 50): non-negative finite fades and radii, smoothing in
// [0, 1]. A zero-everything mask (the pre-R16 shape) always passes.
func checkContentMask(op string, mask SceneContentMask) error {
	for _, bits := range mask.FadeOut {
		f := float64(math.Float32frombits(bits))
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return badRecord(op, "content mask fade distance negative or non-finite")
		}
	}
	for _, bits := range mask.CornerRadii {
		f := float64(math.Float32frombits(bits))
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return badRecord(op, "content mask corner radius negative or non-finite")
		}
	}
	smoothing := float64(math.Float32frombits(mask.CornerSmoothing))
	if math.IsNaN(smoothing) || math.IsInf(smoothing, 0) || smoothing < 0 || smoothing > 1 {
		return badRecord(op, "content mask corner smoothing out of range")
	}
	return nil
}

// checkEffectRecord validates the effect-group record (rev 16, ticket
// 50; the design appendix A.1 rules): known flag bits only, an
// invertible 2x2 when HasTransform is set, the none-kind mask this
// revision, the property-node id non-zero and paired with a transform
// or opacity, opacity in [0, 1], finite radii and smoothing in [0, 1].
func checkEffectRecord(op string, rec *SceneEffectRecord) error {
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	known := SceneEffectFlagIsStart | SceneEffectFlagHasTransform |
		SceneEffectFlagHasOpacity | SceneEffectFlagHasBlend |
		SceneEffectFlagHasMask | SceneEffectFlagPropertyNode |
		SceneEffectFlagRoundedClipOnly | SceneEffectFlagClipToBounds
	if rec.Flags&^known != 0 {
		return badRecord(op, "effect flags carry unknown bits")
	}
	if rec.Flags&SceneEffectFlagHasTransform != 0 {
		m00 := float64(math.Float32frombits(rec.Transform[0]))
		m01 := float64(math.Float32frombits(rec.Transform[1]))
		m10 := float64(math.Float32frombits(rec.Transform[2]))
		m11 := float64(math.Float32frombits(rec.Transform[3]))
		for _, value := range []float64{m00, m01, m10, m11} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return badRecord(op, "effect transform is non-finite")
			}
		}
		determinant := m00*m11 - m01*m10
		if math.IsNaN(determinant) || math.IsInf(determinant, 0) || determinant == 0 {
			return badRecord(op, "effect transform 2x2 is singular")
		}
	}
	if rec.BlendMode > 16 {
		return badRecord(op, "effect blend mode out of range")
	}
	// rev 16, ticket 52: the mask kinds 1-3 are wired - the composite
	// resolves the reference at RENDER time (an unresolvable mask hides
	// the element), so only the kind/mode tags validate here.
	if rec.MaskKind > 3 {
		return badRecord(op, "effect mask kind out of range")
	}
	if rec.MaskKind == 0 && (rec.MaskReference != 0 || rec.MaskMode != 0) {
		return badRecord(op, "effect none-kind mask must carry zero reference and mode")
	}
	if rec.MaskKind != 0 && rec.MaskMode > 1 {
		return badRecord(op, "effect mask mode out of range")
	}
	if rec.Flags&SceneEffectFlagPropertyNode != 0 {
		if rec.NodeID == 0 {
			return badRecord(op, "effect property node requires a non-zero node id")
		}
		if rec.Flags&(SceneEffectFlagHasTransform|SceneEffectFlagHasOpacity) == 0 {
			return badRecord(op, "effect property node requires a transform or an opacity")
		}
	}
	opacity := float64(math.Float32frombits(rec.Opacity))
	if math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
		return badRecord(op, "effect opacity out of range")
	}
	for _, bits := range rec.CornerRadii {
		f := float64(math.Float32frombits(bits))
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return badRecord(op, "effect corner radius negative or non-finite")
		}
	}
	smoothing := float64(math.Float32frombits(rec.CornerSmoothing))
	if math.IsNaN(smoothing) || math.IsInf(smoothing, 0) || smoothing < 0 || smoothing > 1 {
		return badRecord(op, "effect corner smoothing out of range")
	}
	if rec.Reserved != 0 {
		return badRecord(op, "effect reserved word must be zero")
	}
	return nil
}

func checkQuadRecord(rec *SceneQuadRecord) error {
	const op = "scene_insert_quad"
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	if rec.BorderStyle > 1 {
		return badRecord(op, "border style out of range")
	}
	if err := checkBackground(op, rec.Background); err != nil {
		return err
	}
	if err := checkBackground(op, rec.BorderColor); err != nil {
		return err
	}
	return checkContentMask(op, rec.ContentMask)
}

func checkShadowRecord(rec *SceneShadowRecord) error {
	const op = "scene_insert_shadow"
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	if err := checkBool01(op, rec.Inset); err != nil {
		return err
	}
	if err := checkBackground(op, rec.Color); err != nil {
		return err
	}
	return checkContentMask(op, rec.ContentMask)
}

func checkUnderlineRecord(rec *SceneUnderlineRecord) error {
	const op = "scene_insert_underline"
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	if err := checkBool01(op, rec.Wavy); err != nil {
		return err
	}
	if err := checkColor(op, rec.Color); err != nil {
		return err
	}
	return checkContentMask(op, rec.ContentMask)
}

func checkFilterRecord(op string, rec *SceneFilterRecord, maxFilters uint32) error {
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	if err := checkBool01(op, rec.IsStart); err != nil {
		return err
	}
	if rec.FilterCount > maxFilters {
		return badRecord(op, "filter count over the maximum")
	}
	// rev 15 / rev 15.1: the per-filter tags and the per-function colour
	// matrices (one 20-word slot per matrix filter in chain order).
	matrixEntries := uint32(0)
	for index, tag := range rec.FilterTags {
		if uint32(index) < rec.FilterCount {
			switch tag {
			case 0:
				radius := math.Float32frombits(rec.BlurRadii[index])
				if math.IsInf(float64(radius), 0) || math.IsNaN(float64(radius)) || radius < 0 {
					return badRecord(op, "blur radius out of range")
				}
			case 1:
				matrixEntries++
				if rec.BlurRadii[index] != 0 {
					return badRecord(op, "matrix filter carries a blur radius")
				}
			default:
				return badRecord(op, "unknown filter kind tag")
			}
		} else if tag != 0 {
			return badRecord(op, "unused filter kind tag must be zero")
		}
	}
	if rec.ColorMatrixCount != matrixEntries {
		return badRecord(op, "matrix count does not match the tag-1 entries")
	}
	if rec.ColorMatrixCount == 0 {
		if rec.ColorMatrix != [160]uint32{} {
			return badRecord(op, "unused matrix payload must be zero")
		}
	} else {
		carried := int(rec.ColorMatrixCount) * 20
		for _, bits := range rec.ColorMatrix[:carried] {
			if !finiteBits(bits) {
				return badRecord(op, "non-finite matrix entry")
			}
		}
		// Slots at and beyond the count must be zeroed.
		for _, bits := range rec.ColorMatrix[carried:] {
			if bits != 0 {
				return badRecord(op, "matrix slot beyond the count must be zero")
			}
		}
	}
	// rev 16, ticket 52: the appended drop-shadow op-chain reference -
	// 0/0 is the R15 legacy shape; a count demands an index (the chain
	// resolves at SNAPSHOT time: an out-of-range index reads as empty,
	// the R15 semantics).
	if rec.OpChainCount == 0 && rec.OpChainIndex != 0 {
		return badRecord(op, "filter op-chain count zero with a non-zero index")
	}
	if rec.OpChainCount > 8 {
		return badRecord(op, "filter op-chain count over the maximum")
	}
	return nil
}

// Surface source tags (mirror surface_source_kind).
const (
	sceneSurfaceUnsupported  uint32 = 0
	sceneSurfaceCaptureFrame uint32 = 1
)

func checkSurfaceRecord(rec *SceneSurfaceRecord) error {
	const op = "scene_insert_surface"
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	switch rec.SourceTag {
	case sceneSurfaceUnsupported:
		if rec.SourceReserved != [3]uint32{} {
			return badRecord(op, "reserved source words must be zero")
		}
	case sceneSurfaceCaptureFrame:
		if rec.SourceReserved[0] == 0 || rec.SourceReserved[2] != 0 {
			return badRecord(op, "malformed capture frame payload")
		}
	default:
		return badRecord(op, "unknown source tag")
	}
	return nil
}

func checkTile(op string, tile *SceneTileRecord, expectedKind uint32) error {
	if tile.TextureKind != expectedKind {
		return badRecord(op, "tile texture kind does not match the sprite class")
	}
	if tile.BoundsW <= 0 || tile.BoundsH <= 0 || tile.BoundsX < 0 || tile.BoundsY < 0 {
		return badRecord(op, "tile bounds out of range")
	}
	return nil
}

func checkMaskSpriteRecord(op string, rec *SceneSpriteRecord, expectedKind uint32) error {
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	if err := checkColor(op, rec.Color); err != nil {
		return err
	}
	if err := checkBool01(op, rec.Pixelated); err != nil {
		return err
	}
	for _, bits := range rec.Transformation {
		if !finiteBits(bits) {
			return badRecord(op, "non-finite transformation")
		}
	}
	return checkTile(op, &rec.Tile, expectedKind)
}

func checkPolychromeSpriteRecord(rec *ScenePolychromeSpriteRecord) error {
	const op = "scene_insert_polychrome_sprite"
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), rec.Order); err != nil {
		return err
	}
	if rec.Grayscale > 1 {
		return badRecord(op, "grayscale flag out of range")
	}
	if !finiteBits(rec.Opacity) || !finiteBits(rec.CornerSmoothing) {
		return badRecord(op, "non-finite opacity or corner smoothing")
	}
	for _, bits := range rec.CornerRadii {
		if !finiteBits(bits) {
			return badRecord(op, "non-finite corner radius")
		}
	}
	return checkTile(op, &rec.Tile, SceneSpriteTexturePolychrome)
}

// checkGradientRecord validates a gradient table entry (rev 16,
// ticket 52): known kind and tile tags, the sRGB space, finite
// geometry with the kind's non-degeneracy rule, the stop count in
// 1..=64, and stops that are finite, ordered non-decreasing within
// [0, 1].
func checkGradientRecord(op string, rec *SceneGradientRecord, stops []SceneGradientStop) error {
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), 0); err != nil {
		return err
	}
	if rec.Kind > SceneGradientKindConic {
		return badRecord(op, "gradient kind out of range")
	}
	if rec.Tile > SceneGradientTileReflect {
		return badRecord(op, "gradient tile mode out of range")
	}
	if rec.ColorSpace != 0 {
		return badRecord(op, "gradient color space must be sRGB this revision")
	}
	for _, bits := range rec.Geometry {
		if !finiteBits(bits) {
			return badRecord(op, "gradient geometry is non-finite")
		}
	}
	switch rec.Kind {
	case SceneGradientKindLinear:
		x0 := math.Float32frombits(rec.Geometry[0])
		y0 := math.Float32frombits(rec.Geometry[1])
		x1 := math.Float32frombits(rec.Geometry[2])
		y1 := math.Float32frombits(rec.Geometry[3])
		if x1 == x0 && y1 == y0 {
			return badRecord(op, "linear gradient line is degenerate")
		}
	case SceneGradientKindRadial:
		r0 := math.Float32frombits(rec.Geometry[2])
		r1 := math.Float32frombits(rec.Geometry[3])
		aspect := math.Float32frombits(rec.Geometry[4])
		if r1 == r0 || aspect <= 0 {
			return badRecord(op, "radial gradient radii or aspect are degenerate")
		}
	case SceneGradientKindConic:
		rotation := math.Float32frombits(rec.Geometry[3])
		if rotation <= 0 {
			return badRecord(op, "conic gradient sweep must be positive")
		}
	}
	if rec.StopRow != 0 {
		return badRecord(op, "gradient stop row must be zero on insert")
	}
	if len(stops) == 0 || len(stops) > int(MaxGradientStops) {
		return badRecord(op, "gradient stop count out of range (1..=64)")
	}
	if rec.StopCount != uint32(len(stops)) {
		return badRecord(op, "gradient stop count disagrees with the stop array")
	}
	previous := math.Inf(-1)
	for _, stop := range stops {
		position := float64(math.Float32frombits(stop.Position))
		if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 || position > 1 {
			return badRecord(op, "gradient stop position out of range")
		}
		if position < previous {
			return badRecord(op, "gradient stop positions must be non-decreasing")
		}
		previous = position
	}
	return nil
}

// checkCoverageRecord validates a coverage entry (rev 16, ticket 52):
// finite bounds (the path index resolves at render time).
func checkCoverageRecord(op string, rec *SceneCoverageRecord) error {
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), 0); err != nil {
		return err
	}
	for _, bits := range []uint32{rec.Bounds.X, rec.Bounds.Y, rec.Bounds.W, rec.Bounds.H} {
		if !finiteBits(bits) {
			return badRecord(op, "coverage bounds are non-finite")
		}
	}
	return nil
}

// checkFilterOpRecord validates one drop-shadow op (rev 16, ticket 52):
// the drop-shadow kind only, finite offset and radius, a solid color.
func checkFilterOpRecord(op string, rec *SceneFilterOpRecord) error {
	if err := checkCommon(op, rec.RecordSize, unsafe.Sizeof(*rec), 0); err != nil {
		return err
	}
	if rec.Kind != SceneFilterOpDropShadow {
		return badRecord(op, "filter op kind must be drop-shadow")
	}
	for _, bits := range rec.Offset {
		if !finiteBits(bits) {
			return badRecord(op, "filter op offset is non-finite")
		}
	}
	radius := float64(math.Float32frombits(rec.BlurRadius))
	if math.IsNaN(radius) || math.IsInf(radius, 0) || radius < 0 {
		return badRecord(op, "filter op blur radius out of range")
	}
	return checkColor(op, rec.Color)
}
