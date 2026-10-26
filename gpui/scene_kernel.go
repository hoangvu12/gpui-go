package gpui

import (
	"fmt"
	"math"
	"sync"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// This file is the kernel seam of the scene slice (ticket08):
// SceneKernel wraps one native scene handle (internal/native, reserved
// slot 3, capability "scene-kernel-v1") and translates the port's typed
// scene records (scene.go) into the fixed ABI records
// (internal/native/scene.go mirrors reference/native/src/scene.rs).
//
// The process-wide native scene service is loaded once (like the layout
// service); scenes are created from it and disposed explicitly. The
// native side owns the kernel semantics: bounds-tree draw orders,
// layers, order floors, finish/sort, batching, filter-target planning,
// paired surface opacity and replay. The busy guard and
// generation-stamped handles live in the native wrapper
// (internal/native.Scene), which this type delegates to; nothing here
// retains Go pointers across calls.

// sceneServiceState holds the process-wide native scene service: the
// embedded artifact is loaded once and stays loaded for the process
// lifetime (the distribution contract: no FreeLibrary, no hot unload).
var (
	sceneServiceOnce sync.Once
	sceneServiceVal  *native.SceneService
	sceneServiceErr  error
)

func sceneService() (*native.SceneService, error) {
	sceneServiceOnce.Do(func() {
		lib, err := native.Load(native.Options{})
		if err != nil {
			sceneServiceErr = fmt.Errorf("gpui: loading the native scene kernel artifact: %w", err)
			return
		}
		svc, err := lib.Scene()
		if err != nil {
			sceneServiceErr = fmt.Errorf("gpui: native scene kernel service: %w", err)
			return
		}
		sceneServiceVal = svc
	})
	return sceneServiceVal, sceneServiceErr
}

// SceneKernel is the native-backed scene kernel handle: Insert /
// BeginLayer / EndLayer / Finish / Plan / Dump / Replay over one native
// scene. It is the seam the port's Scene (scene.go) drives; the typed
// records and semantic paint API live there.
//
// A SceneKernel is not safe for concurrent use; scene work runs on the
// owning foreground thread. Same-scene re-entry is rejected by the busy
// guard before native entry (the service pattern).
type SceneKernel struct {
	scene *native.Scene
	// damageLog mirrors the native paint-op log's INDEX space (the
	// replay address space Len reports) with each op's clipped device
	// bounds — browser-core ticket 58's damage computation reads ranges
	// of it (damage.go). One entry per op, appended at every tracked
	// insert (the native insertion core's drop rule replicated), so the
	// log and the native count stay aligned; damageLogDrifted is the
	// gate that falls back to a full redraw when they do not.
	damageLog []sceneOpDamage
}

// newSceneKernel creates a fresh native scene for the process-wide
// service.
func newSceneKernel() (*SceneKernel, error) {
	svc, err := sceneService()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSceneUnavailable, err)
	}
	scene, err := svc.CreateScene()
	if err != nil {
		return nil, fmt.Errorf("gpui: creating the native scene: %w", err)
	}
	return &SceneKernel{scene: scene}, nil
}

// nativeScene exposes the wrapped native scene (identity comparison for
// Replay's self-check).
func (k *SceneKernel) nativeScene() *native.Scene { return k.scene }

// Dispose frees the native scene. The kernel is unusable afterwards.
func (k *SceneKernel) Dispose() error {
	if k.scene == nil {
		return ErrSceneClosed
	}
	err := k.scene.Dispose()
	k.scene = nil
	if err != nil {
		return fmt.Errorf("gpui: scene dispose: %w", err)
	}
	return nil
}

func (k *SceneKernel) checkLive(op string) error {
	if k.scene == nil {
		return fmt.Errorf("gpui: %s: %w", op, ErrSceneClosed)
	}
	return nil
}

// Len returns the scene's paint-operation count.
func (k *SceneKernel) Len() (uint32, error) {
	if err := k.checkLive("Len"); err != nil {
		return 0, err
	}
	length, err := k.scene.Len()
	if err != nil {
		return 0, fmt.Errorf("gpui: scene len: %w", err)
	}
	return length, nil
}

// BeginLayer pushes a paint layer with device-pixel bounds.
func (k *SceneKernel) BeginLayer(bounds Bounds) error {
	if err := k.checkLive("BeginLayer"); err != nil {
		return err
	}
	if err := k.scene.PushLayer(sceneBoundsRecord(bounds)); err != nil {
		return fmt.Errorf("gpui: scene push layer: %w", err)
	}
	// A layer push counts in the op space but paints nowhere (the
	// layer's ops carry their own bounds).
	k.recordDamageOpless()
	return nil
}

// EndLayer pops the innermost layer.
func (k *SceneKernel) EndLayer() error {
	if err := k.checkLive("EndLayer"); err != nil {
		return err
	}
	if err := k.scene.PopLayer(); err != nil {
		return fmt.Errorf("gpui: scene pop layer: %w", err)
	}
	k.recordDamageOpless()
	return nil
}

// RaiseOrderFloor raises the deferred-draw order floor.
func (k *SceneKernel) RaiseOrderFloor() error {
	if err := k.checkLive("RaiseOrderFloor"); err != nil {
		return err
	}
	if err := k.scene.RaiseOrderFloor(); err != nil {
		return fmt.Errorf("gpui: scene raise order floor: %w", err)
	}
	return nil
}

// InsertQuad inserts a quad record. The record's Order must be zero (the
// kernel assigns it); a non-zero order is a caller error rejected before
// native entry, mirroring the ABI's record validation.
func (k *SceneKernel) InsertQuad(q Quad) error {
	if err := k.checkLive("InsertQuad"); err != nil {
		return err
	}
	if q.Order != 0 {
		return fmt.Errorf("gpui: scene insert quad: record order must be zero, got %d (the kernel assigns the draw order)", q.Order)
	}
	rec := native.NewSceneQuadRecord()
	rec.Bounds = sceneBoundsRecord(q.Bounds)
	rec.ContentMask = sceneContentMaskRecord(q.ContentMask)
	rec.Background = sceneBackgroundRecord(q.Background, q.Bounds)
	rec.BorderColor = sceneBackgroundRecord(q.BorderColor, q.Bounds)
	for i, radius := range [4]float32{q.CornerRadii.TopLeft, q.CornerRadii.TopRight, q.CornerRadii.BottomRight, q.CornerRadii.BottomLeft} {
		rec.CornerRadii[i] = math.Float32bits(radius)
	}
	for i, width := range [4]float32{q.BorderWidths.Top, q.BorderWidths.Right, q.BorderWidths.Bottom, q.BorderWidths.Left} {
		rec.BorderWidths[i] = math.Float32bits(width)
	}
	rec.BorderStyle = uint32(q.BorderStyle)
	rec.BorderDashedLength = math.Float32bits(q.DashedLength)
	rec.BorderDashedGap = math.Float32bits(q.DashedGap)
	rec.CornerSmoothing = math.Float32bits(q.CornerSmoothing)
	rec.Padding = q.Padding
	if err := k.scene.InsertQuad(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert quad: %w", err)
	}
	k.recordDamageRect(q.Bounds, q.ContentMask, false)
	return nil
}

// InsertShadow inserts a shadow record (Order must be zero; the kernel
// assigns it).
func (k *SceneKernel) InsertShadow(sh Shadow) error {
	if err := k.checkLive("InsertShadow"); err != nil {
		return err
	}
	if sh.Order != 0 {
		return fmt.Errorf("gpui: scene insert shadow: record order must be zero, got %d", sh.Order)
	}
	rec := native.NewSceneShadowRecord()
	rec.BlurRadius = math.Float32bits(sh.BlurRadius)
	rec.Bounds = sceneBoundsRecord(sh.Bounds)
	rec.ContentMask = sceneContentMaskRecord(sh.ContentMask)
	rec.Color = sceneBackgroundRecord(sh.Color, sh.Bounds)
	rec.ElementBounds = sceneBoundsRecord(sh.ElementBounds)
	for i, radius := range [4]float32{sh.CornerRadii.TopLeft, sh.CornerRadii.TopRight, sh.CornerRadii.BottomRight, sh.CornerRadii.BottomLeft} {
		rec.CornerRadii[i] = math.Float32bits(radius)
	}
	for i, radius := range [4]float32{sh.ElementCornerRadii.TopLeft, sh.ElementCornerRadii.TopRight, sh.ElementCornerRadii.BottomRight, sh.ElementCornerRadii.BottomLeft} {
		rec.ElementCornerRadii[i] = math.Float32bits(radius)
	}
	if sh.Inset {
		rec.Inset = 1
	}
	rec.CornerSmoothing = math.Float32bits(sh.CornerSmoothing)
	if err := k.scene.InsertShadow(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert shadow: %w", err)
	}
	k.recordDamageRect(sh.Bounds, sh.ContentMask, false)
	return nil
}

// InsertUnderline inserts an underline record (Order must be zero; the
// kernel assigns it).
func (k *SceneKernel) InsertUnderline(u Underline) error {
	if err := k.checkLive("InsertUnderline"); err != nil {
		return err
	}
	if u.Order != 0 {
		return fmt.Errorf("gpui: scene insert underline: record order must be zero, got %d", u.Order)
	}
	rec := native.NewSceneUnderlineRecord()
	rec.Padding = u.Padding
	rec.Bounds = sceneBoundsRecord(u.Bounds)
	rec.ContentMask = sceneContentMaskRecord(u.ContentMask)
	rec.Color = sceneColorRecord(u.Color)
	rec.Thickness = math.Float32bits(u.Thickness)
	if u.Wavy {
		rec.Wavy = 1
	}
	if err := k.scene.InsertUnderline(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert underline: %w", err)
	}
	k.recordDamageRect(u.Bounds, u.ContentMask, false)
	return nil
}

// InsertBackdropFilter inserts a backdrop-filter record (Order must be
// zero; the kernel assigns it).
func (k *SceneKernel) InsertBackdropFilter(f BackdropFilter) error {
	if err := k.checkLive("InsertBackdropFilter"); err != nil {
		return err
	}
	if f.Order != 0 {
		return fmt.Errorf("gpui: scene insert backdrop filter: record order must be zero, got %d", f.Order)
	}
	rec := filterRecordOf(f.Order, f.Bounds, f.ContentMask, f.CornerRadii, f.CornerSmoothing, f.BlurRadius, f.ColorMatrix, f.ColorMatrixCount, f.Opacity, false)
	if err := k.scene.InsertBackdropFilter(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert backdrop filter: %w", err)
	}
	k.recordDamageRect(f.Bounds, f.ContentMask, false)
	return nil
}

// InsertFilterBoundary inserts a content-filter boundary marker (Order
// must be zero; the kernel assigns it).
func (k *SceneKernel) InsertFilterBoundary(b FilterBoundary) error {
	if err := k.checkLive("InsertFilterBoundary"); err != nil {
		return err
	}
	if b.Order != 0 {
		return fmt.Errorf("gpui: scene insert filter boundary: record order must be zero, got %d", b.Order)
	}
	rec := filterRecordOf(b.Order, b.Bounds, b.ContentMask, b.CornerRadii, b.CornerSmoothing, b.BlurRadius, b.ColorMatrix, b.ColorMatrixCount, b.Opacity, b.IsStart)
	rec.OpChainIndex = b.OpChainIndex
	rec.OpChainCount = b.OpChainCount
	if err := k.scene.InsertFilterBoundary(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert filter boundary: %w", err)
	}
	// A boundary marker never drops, but paints nowhere itself — its
	// group's pixels come from the children it brackets (the native
	// damage walk expands the rects across the group; ticket 53).
	k.recordDamageOpless()
	return nil
}

// insertGradient inserts one gradient table entry (rev 16, ticket 52)
// and returns the assigned table index. The stop list above the cap is
// resampled down to 64 evenly-spaced stops with a debug-build warning
// (the design's §8.1 answer 4); the native service bakes the
// premultiplied-sRGB ramp row.
func (k *SceneKernel) insertGradient(g Gradient) (uint32, error) {
	if err := k.checkLive("InsertGradient"); err != nil {
		return 0, err
	}
	if len(g.Stops) == 0 {
		return 0, fmt.Errorf("gpui: InsertGradient: at least one stop is required")
	}
	stops := g.Stops
	if len(stops) > MaxTableGradientStops {
		GradientDebugWarnf("gpui: InsertGradient: %d stops over the %d-stop cap; resampling down to %d (the ramp-path approximation)", len(stops), MaxTableGradientStops, MaxTableGradientStops)
		stops = ResampleTableGradientStops(stops)
	}
	rec := native.NewSceneGradientRecord()
	rec.Kind = uint32(g.Kind)
	rec.Tile = uint32(g.Tile)
	for i, value := range g.Geometry {
		rec.Geometry[i] = math.Float32bits(value)
	}
	rec.StopCount = uint32(len(stops))
	packed := make([]native.SceneGradientStop, len(stops))
	for i, stop := range stops {
		packed[i] = native.SceneGradientStop{
			Position: math.Float32bits(stop.Position),
			Rgba:     rgbaPacked(stop.Color),
		}
	}
	index, err := k.scene.InsertGradient(&rec, packed)
	if err != nil {
		return 0, fmt.Errorf("gpui: scene insert gradient: %w", err)
	}
	// A table row: it counts in the op space but paints nowhere.
	k.recordDamageOpless()
	return index, nil
}

// rgbaPacked packs one straight sRGBA f32 color into the stop record's
// one-byte-per-lane word (r | g<<8 | b<<16 | a<<24).
func rgbaPacked(c Rgba) uint32 {
	clampLane := func(value float32) uint32 {
		if value < 0 {
			return 0
		}
		if value > 1 {
			return 255
		}
		return uint32(value*255.0 + 0.5)
	}
	return clampLane(c.R) | clampLane(c.G)<<8 | clampLane(c.B)<<16 | clampLane(c.A)<<24
}

// insertCoverage inserts one clip-path coverage entry (rev 16, ticket
// 52) and returns the assigned coverage index.
func (k *SceneKernel) insertCoverage(pathIndex uint32, bounds Bounds) (uint32, error) {
	if err := k.checkLive("InsertCoverage"); err != nil {
		return 0, err
	}
	rec := native.NewSceneCoverageRecord()
	rec.PathIndex = pathIndex
	rec.Bounds = sceneBoundsRecord(bounds)
	index, err := k.scene.InsertCoverage(&rec)
	if err != nil {
		return 0, fmt.Errorf("gpui: scene insert coverage: %w", err)
	}
	// A table row: it counts in the op space but paints nowhere.
	k.recordDamageOpless()
	return index, nil
}

// insertFilterOpChain inserts one drop-shadow op chain (rev 16, ticket
// 52) and returns the assigned chain index.
func (k *SceneKernel) insertFilterOpChain(ops []FilterOp) (uint32, error) {
	if err := k.checkLive("InsertFilterOpChain"); err != nil {
		return 0, err
	}
	if len(ops) == 0 {
		return 0, fmt.Errorf("gpui: InsertFilterOpChain: at least one op is required")
	}
	records := make([]native.SceneFilterOpRecord, len(ops))
	for i, op := range ops {
		records[i] = native.NewSceneFilterOpRecord()
		records[i].Offset[0] = math.Float32bits(op.OffsetX)
		records[i].Offset[1] = math.Float32bits(op.OffsetY)
		records[i].BlurRadius = math.Float32bits(op.BlurRadius)
		records[i].Color = rgbaSceneColor(op.Color)
	}
	index, err := k.scene.InsertFilterOpChain(records)
	if err != nil {
		return 0, fmt.Errorf("gpui: scene insert filter op chain: %w", err)
	}
	// A table row: it counts in the op space but paints nowhere.
	k.recordDamageOpless()
	return index, nil
}

// rgbaSceneColor converts one straight sRGBA f32 color into the ABI
// scene color record (the HSL-with-alpha storage the native side
// converts back through its own pinned conversion).
func rgbaSceneColor(c Rgba) native.SceneColor {
	hsla := RgbaToHsla32(c)
	return native.SceneColorOf(hsla.H, hsla.S, hsla.L, hsla.A)
}

// RgbaToHsla32 converts a straight sRGBA f32 color into the palette's
// HSL representation (the pinned Rgba::into_color path). The hex
// round trip is in RgbaFromHex's 0xRRGGBBAA order (rgbaPacked's
// one-byte-per-lane packing is the STOP RECORD's ABI order, the
// reverse).
func RgbaToHsla32(c Rgba) Hsla {
	r := clampLaneByte(c.R)
	g := clampLaneByte(c.G)
	b := clampLaneByte(c.B)
	a := clampLaneByte(c.A)
	return RgbaToHsla(r<<24 | g<<16 | b<<8 | a)
}

// clampLaneByte rounds one straight color lane into its 8-bit bucket.
func clampLaneByte(value float32) uint32 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 255
	}
	return uint32(value*255.0 + 0.5)
}

// InsertEffect inserts an effect-group marker record (rev 16,
// browser-core ticket 50). Order must be zero; the kernel assigns it.
// The record's flags decide start/end; a group start over the native
// MAX_SCENE_GROUPS bound (1024, shared with the filter boundaries)
// fails with the typed limit error.
func (k *SceneKernel) InsertEffect(e EffectGroup) error {
	if err := k.checkLive("InsertEffect"); err != nil {
		return err
	}
	if e.Order != 0 {
		return fmt.Errorf("gpui: scene insert effect: record order must be zero, got %d (the kernel assigns the draw order)", e.Order)
	}
	rec := effectRecordOf(e)
	if err := k.scene.InsertEffect(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert effect: %w", err)
	}
	// A group marker never drops, but paints nowhere itself — its
	// group's pixels come from the children it brackets (the native
	// damage walk expands the rects across the group; ticket 53).
	k.recordDamageOpless()
	return nil
}

// EffectBoundaries returns the finished scene's effect-group markers in
// draw order (start before end at equal orders; rev 16).
func (k *SceneKernel) EffectBoundaries() ([]EffectGroup, error) {
	if err := k.checkLive("EffectBoundaries"); err != nil {
		return nil, err
	}
	records, err := k.scene.EffectDump()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene effect dump: %w", err)
	}
	out := make([]EffectGroup, 0, len(records))
	for _, rec := range records {
		out = append(out, effectGroupOf(rec))
	}
	return out, nil
}

// effectPropertyValues packs a property-node update value into the
// ABI word stream (f32 bits, the record layout's order).
func effectPropertyValues(kind uint32, matrix TransformationMatrix, opacity float32) []uint32 {
	if kind == native.ScenePropertyMatrix {
		return []uint32{
			math.Float32bits(matrix.RotationScale[0][0]),
			math.Float32bits(matrix.RotationScale[0][1]),
			math.Float32bits(matrix.RotationScale[1][0]),
			math.Float32bits(matrix.RotationScale[1][1]),
			math.Float32bits(matrix.Translation[0]),
			math.Float32bits(matrix.Translation[1]),
		}
	}
	return []uint32{math.Float32bits(opacity)}
}

// UpdateEffectMatrix mutates a finished scene's property-node records
// carrying nodeID in place (rev 16, browser-core ticket 51; the
// design's appendix A.7): every effect record of the node takes the
// new 2x3 matrix - no re-sort, no re-plan, the same scene handle
// re-presents with the updated transform. The node's records must
// carry the transform flag (insert the group with HasTransform and
// PropertyNode). Returns the group's old and new mapped device bounds
// (the record bounds mapped through the old and the new matrix), the
// damage union the caller re-presents.
func (k *SceneKernel) UpdateEffectMatrix(nodeID uint32, matrix TransformationMatrix) (oldBounds, newBounds Bounds, err error) {
	if err := k.checkLive("UpdateEffectMatrix"); err != nil {
		return Bounds{}, Bounds{}, err
	}
	old, new, err := k.scene.UpdateProperty(
		nodeID,
		native.ScenePropertyMatrix,
		effectPropertyValues(native.ScenePropertyMatrix, matrix, 0),
	)
	if err != nil {
		return Bounds{}, Bounds{}, fmt.Errorf("gpui: scene update effect matrix: %w", err)
	}
	return sceneBoundsValue(old), sceneBoundsValue(new), nil
}

// UpdateEffectOpacity mutates a finished scene's property-node records
// carrying nodeID in place (rev 16, browser-core ticket 51): every
// effect record of the node takes the new group opacity - no re-sort,
// no re-plan. The node's records must carry the opacity flag. The
// opacity never moves the group, so both returned bounds are the
// group's mapped device bounds (unchanged by the update).
func (k *SceneKernel) UpdateEffectOpacity(nodeID uint32, opacity float32) (oldBounds, newBounds Bounds, err error) {
	if err := k.checkLive("UpdateEffectOpacity"); err != nil {
		return Bounds{}, Bounds{}, err
	}
	old, new, err := k.scene.UpdateProperty(
		nodeID,
		native.ScenePropertyOpacity,
		effectPropertyValues(native.ScenePropertyOpacity, UnitTransformation(), opacity),
	)
	if err != nil {
		return Bounds{}, Bounds{}, fmt.Errorf("gpui: scene update effect opacity: %w", err)
	}
	return sceneBoundsValue(old), sceneBoundsValue(new), nil
}

// InsertSurface inserts a surface record with its paired opacity.
func (k *SceneKernel) InsertSurface(bounds, contentMask Bounds, source SurfaceSourceTag, opacity float32) error {
	if err := k.checkLive("InsertSurface"); err != nil {
		return err
	}
	rec := native.NewSceneSurfaceRecord()
	rec.Bounds = sceneBoundsRecord(bounds)
	rec.ContentMask = sceneContentMaskRecord(contentMask)
	rec.SourceTag = uint32(source)
	if err := k.scene.InsertSurface(&rec, opacity); err != nil {
		return fmt.Errorf("gpui: scene insert surface: %w", err)
	}
	k.recordDamageRect(bounds, contentMask, uint32(source) == uint32(SurfaceSourceCaptureFrame))
	return nil
}

// Replay re-inserts a range of prev's operations into this scene.
func (k *SceneKernel) Replay(start, end uint32, prev *SceneKernel) error {
	if err := k.checkLive("Replay"); err != nil {
		return err
	}
	if prev == nil || prev.scene == nil {
		return fmt.Errorf("gpui: Replay: nil source scene")
	}
	if err := k.scene.Replay(start, end, prev.scene); err != nil {
		return fmt.Errorf("gpui: scene replay: %w", err)
	}
	// The copied ops keep their parameters, so their damage-log
	// entries copy too (damage.go's rule reads them as the undamaged
	// region of the next frame).
	k.recordDamageReplay(start, end, prev)
	return nil
}

// Finish sorts and compiles the plan.
func (k *SceneKernel) Finish() error {
	if err := k.checkLive("Finish"); err != nil {
		return err
	}
	if err := k.scene.Finish(); err != nil {
		return fmt.Errorf("gpui: scene finish: %w", err)
	}
	return nil
}

// Quads dumps the finished scene's quads in draw order.
func (k *SceneKernel) Quads() ([]Quad, error) {
	if err := k.checkLive("Quads"); err != nil {
		return nil, err
	}
	records, err := k.scene.DumpQuads()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump quads: %w", err)
	}
	out := make([]Quad, len(records))
	for i, rec := range records {
		out[i] = Quad{
			Order:           rec.Order,
			Bounds:          sceneBoundsValue(rec.Bounds),
			ContentMask:     sceneContentMaskValue(rec.ContentMask),
			Background:      sceneBackgroundValue(rec.Background),
			BorderColor:     sceneBackgroundValue(rec.BorderColor),
			BorderStyle:     BorderStyle(rec.BorderStyle),
			DashedLength:    math.Float32frombits(rec.BorderDashedLength),
			DashedGap:       math.Float32frombits(rec.BorderDashedGap),
			CornerSmoothing: math.Float32frombits(rec.CornerSmoothing),
			Padding:         rec.Padding,
		}
		out[i].CornerRadii = cornersValue(rec.CornerRadii)
		out[i].BorderWidths = edgesValue(rec.BorderWidths)
	}
	return out, nil
}

// Shadows dumps the finished scene's shadows in draw order.
func (k *SceneKernel) Shadows() ([]Shadow, error) {
	if err := k.checkLive("Shadows"); err != nil {
		return nil, err
	}
	records, err := k.scene.DumpShadows()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump shadows: %w", err)
	}
	out := make([]Shadow, len(records))
	for i, rec := range records {
		out[i] = Shadow{
			Order:              rec.Order,
			BlurRadius:         math.Float32frombits(rec.BlurRadius),
			Bounds:             sceneBoundsValue(rec.Bounds),
			ContentMask:        sceneContentMaskValue(rec.ContentMask),
			Color:              sceneBackgroundValue(rec.Color),
			ElementBounds:      sceneBoundsValue(rec.ElementBounds),
			ElementCornerRadii: cornersValue(rec.ElementCornerRadii),
			Inset:              rec.Inset == 1,
			CornerSmoothing:    math.Float32frombits(rec.CornerSmoothing),
		}
		out[i].CornerRadii = cornersValue(rec.CornerRadii)
	}
	return out, nil
}

// Underlines dumps the finished scene's underlines in draw order.
func (k *SceneKernel) Underlines() ([]Underline, error) {
	if err := k.checkLive("Underlines"); err != nil {
		return nil, err
	}
	records, err := k.scene.DumpUnderlines()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump underlines: %w", err)
	}
	out := make([]Underline, len(records))
	for i, rec := range records {
		out[i] = Underline{
			Order:       rec.Order,
			Padding:     rec.Padding,
			Bounds:      sceneBoundsValue(rec.Bounds),
			ContentMask: sceneContentMaskValue(rec.ContentMask),
			Color:       sceneColorValue(rec.Color),
			Thickness:   math.Float32frombits(rec.Thickness),
			Wavy:        rec.Wavy == 1,
		}
	}
	return out, nil
}

// BackdropFilters dumps the finished scene's backdrop filters in draw
// order.
func (k *SceneKernel) BackdropFilters() ([]BackdropFilter, error) {
	if err := k.checkLive("BackdropFilters"); err != nil {
		return nil, err
	}
	records, err := k.scene.DumpBackdrops()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump backdrops: %w", err)
	}
	out := make([]BackdropFilter, len(records))
	for i, rec := range records {
		out[i] = backdropValue(rec)
	}
	return out, nil
}

// FilterBoundaries dumps the finished scene's filter boundaries in draw
// order.
func (k *SceneKernel) FilterBoundaries() ([]FilterBoundary, error) {
	if err := k.checkLive("FilterBoundaries"); err != nil {
		return nil, err
	}
	records, err := k.scene.DumpBoundaries()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump boundaries: %w", err)
	}
	out := make([]FilterBoundary, len(records))
	for i, rec := range records {
		out[i] = boundaryValue(rec)
	}
	return out, nil
}

// Surfaces dumps the finished scene's surfaces in draw order with their
// paired opacities.
func (k *SceneKernel) Surfaces() ([]SurfaceRecord, []float32, error) {
	if err := k.checkLive("Surfaces"); err != nil {
		return nil, nil, err
	}
	records, opacities, err := k.scene.DumpSurfaces()
	if err != nil {
		return nil, nil, fmt.Errorf("gpui: scene dump surfaces: %w", err)
	}
	out := make([]SurfaceRecord, len(records))
	for i, rec := range records {
		out[i] = SurfaceRecord{
			Order:       rec.Order,
			Bounds:      sceneBoundsValue(rec.Bounds),
			ContentMask: sceneContentMaskValue(rec.ContentMask),
			Source:      SurfaceSourceTag(rec.SourceTag),
		}
		if rec.SourceTag == uint32(SurfaceSourceCaptureFrame) {
			out[i].SourceHandle = uint64(rec.SourceReserved[0]) | uint64(rec.SourceReserved[1])<<32
		}
	}
	return out, opacities, nil
}

// Plan dumps the compiled render-plan commands.
func (k *SceneKernel) Plan() ([]SceneCommand, error) {
	if err := k.checkLive("Plan"); err != nil {
		return nil, err
	}
	records, err := k.scene.PlanCommands()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene plan dump: %w", err)
	}
	out := make([]SceneCommand, len(records))
	for i, rec := range records {
		command := SceneCommand{
			Kind:                     SceneCommandKind(rec.CommandKind),
			BatchKind:                SceneBatchKind(rec.PrimitiveKind),
			Range:                    [2]uint32{rec.RangeStart, rec.RangeEnd},
			Smoothed:                 rec.Smoothed == 1,
			Pixelated:                rec.Pixelated == 1,
			TextureIndex:             rec.TextureIndex,
			RasterizationVertexCount: rec.RasterizationVertexCount,
			SpriteCount:              rec.SpriteCount,
			BoundaryIndex:            rec.BoundaryIndex,
			ClosingBoundaryIndex:     rec.ClosingBoundaryIndex,
			Target: FilterTarget{
				Isolated: rec.FilterTarget == 1,
				Index:    rec.TargetIndex,
			},
		}
		out[i] = command
	}
	return out, nil
}

// Requirements returns the compiled plan's resource totals.
func (k *SceneKernel) Requirements() (SceneRequirements, error) {
	if err := k.checkLive("Requirements"); err != nil {
		return SceneRequirements{}, err
	}
	rec, err := k.scene.Requirements()
	if err != nil {
		return SceneRequirements{}, fmt.Errorf("gpui: scene requirements: %w", err)
	}
	return SceneRequirements{
		CommandCount:                 rec.CommandCount,
		InstanceBatchCount:           rec.InstanceBatchCount,
		PathRasterizationVertexCount: rec.PathRasterizationVertexCount,
		PathSpriteCount:              rec.PathSpriteCount,
		SurfaceCount:                 rec.SurfaceCount,
		BackdropFilterCount:          rec.BackdropFilterCount,
		IsolatedFilterCount:          rec.IsolatedFilterCount,
		IsolatedTargetCount:          rec.IsolatedTargetCount,
		UsesPathTarget:               rec.UsesPathTarget == 1,
		UsesOffscreenTarget:          rec.UsesOffscreenTarget == 1,
	}, nil
}

// Meta returns the scene totals.
func (k *SceneKernel) Meta() (SceneMeta, error) {
	if err := k.checkLive("Meta"); err != nil {
		return SceneMeta{}, err
	}
	rec, err := k.scene.Meta()
	if err != nil {
		return SceneMeta{}, fmt.Errorf("gpui: scene meta: %w", err)
	}
	return SceneMeta{
		OpCount:               rec.OpCount,
		LayerPushCount:        rec.LayerPushCount,
		QuadCount:             rec.QuadCount,
		ShadowCount:           rec.ShadowCount,
		UnderlineCount:        rec.UnderlineCount,
		BackdropCount:         rec.BackdropCount,
		BoundaryCount:         rec.BoundaryCount,
		SurfaceCount:          rec.SurfaceCount,
		MonochromeSpriteCount: rec.MonochromeSpriteCount,
		SubpixelSpriteCount:   rec.SubpixelSpriteCount,
		PolychromeSpriteCount: rec.PolychromeSpriteCount,
		PathCount:             rec.PathCount,
		EffectCount:           rec.EffectCount,
		GradientCount:         rec.GradientCount,
		CoverageCount:         rec.CoverageCount,
		FilterChainCount:      rec.FilterChainCount,
		IsFinished:            rec.IsFinished == 1,
	}, nil
}

// ---------------------------------------------------------------------------
// ABI record translation helpers
// ---------------------------------------------------------------------------

func sceneBoundsRecord(b Bounds) native.SceneBounds {
	return native.SceneBoundsOf(b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height)
}

// sceneContentMaskRecord packs a rect-only (zero fade, zero radii)
// content mask: the exact pre-R16 behaviour, the encoding every
// existing Go paint path uses. The Go styling that authors mask radii
// and fades (Style.OverflowMask, the mask-intersection rules) is
// browser-core ticket 55.
func sceneContentMaskRecord(b Bounds) native.SceneContentMask {
	return native.SceneContentMaskOf(b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height)
}

// sceneContentMaskValue unpacks a rect-only mask (the radii and fades
// are zero from every Go-authored record this revision).
func sceneContentMaskValue(rec native.SceneContentMask) Bounds {
	return sceneBoundsValue(rec.Bounds)
}

func sceneBoundsValue(rec native.SceneBounds) Bounds {
	x, y, w, h := rec.Rect()
	return Bounds{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
}

// effectRecordOf packs an EffectGroup into the ABI record.
func effectRecordOf(e EffectGroup) native.SceneEffectRecord {
	rec := native.NewSceneEffectRecord()
	rec.Order = e.Order
	rec.Bounds = sceneBoundsRecord(e.Bounds)
	rec.ContentMask = sceneBoundsRecord(e.ContentMask)
	rec.Opacity = math.Float32bits(e.Opacity)
	rec.Transform = [6]uint32{
		math.Float32bits(e.Transform.RotationScale[0][0]),
		math.Float32bits(e.Transform.RotationScale[0][1]),
		math.Float32bits(e.Transform.RotationScale[1][0]),
		math.Float32bits(e.Transform.RotationScale[1][1]),
		math.Float32bits(e.Transform.Translation[0]),
		math.Float32bits(e.Transform.Translation[1]),
	}
	rec.NodeID = e.NodeID
	rec.BlendMode = uint32(e.BlendMode)
	rec.MaskKind = uint32(e.MaskKind)
	rec.MaskReference = e.MaskReference
	rec.MaskMode = uint32(e.MaskMode)
	for i, radius := range [4]float32{e.CornerRadii.TopLeft, e.CornerRadii.TopRight, e.CornerRadii.BottomRight, e.CornerRadii.BottomLeft} {
		rec.CornerRadii[i] = math.Float32bits(radius)
	}
	rec.CornerSmoothing = math.Float32bits(e.CornerSmoothing)
	flags := uint32(0)
	if e.IsStart {
		flags |= native.SceneEffectFlagIsStart
	}
	if e.HasOpacity {
		flags |= native.SceneEffectFlagHasOpacity
	}
	if e.HasTransform {
		flags |= native.SceneEffectFlagHasTransform
	}
	if e.PropertyNode {
		flags |= native.SceneEffectFlagPropertyNode
	}
	if e.ClipToBounds {
		flags |= native.SceneEffectFlagClipToBounds
	}
	if e.RoundedClipOnly {
		flags |= native.SceneEffectFlagRoundedClipOnly
	}
	if e.HasBlend {
		flags |= native.SceneEffectFlagHasBlend
	}
	if e.HasMask {
		flags |= native.SceneEffectFlagHasMask
	}
	rec.Flags = flags
	return rec
}

// effectGroupOf unpacks an ABI effect record (the Go-authored fields
// this revision: order, bounds, mask, radii, smoothing, opacity, the
// transform, the property node and the wired flags).
func effectGroupOf(rec native.SceneEffectRecord) EffectGroup {
	flags := rec.Flags
	return EffectGroup{
		Order:           rec.Order,
		IsStart:         flags&native.SceneEffectFlagIsStart != 0,
		HasOpacity:      flags&native.SceneEffectFlagHasOpacity != 0,
		HasTransform:    flags&native.SceneEffectFlagHasTransform != 0,
		PropertyNode:    flags&native.SceneEffectFlagPropertyNode != 0,
		NodeID:          rec.NodeID,
		ClipToBounds:    flags&native.SceneEffectFlagClipToBounds != 0,
		RoundedClipOnly: flags&native.SceneEffectFlagRoundedClipOnly != 0,
		HasBlend:        flags&native.SceneEffectFlagHasBlend != 0,
		BlendMode:       BlendMode(rec.BlendMode),
		HasMask:         flags&native.SceneEffectFlagHasMask != 0,
		MaskKind:        MaskKind(rec.MaskKind),
		MaskReference:   rec.MaskReference,
		MaskMode:        MaskMode(rec.MaskMode),
		Bounds:          sceneBoundsValue(rec.Bounds),
		ContentMask:     sceneBoundsValue(rec.ContentMask),
		Opacity:         math.Float32frombits(rec.Opacity),
		Transform: TransformationMatrix{
			RotationScale: [2][2]float32{
				{math.Float32frombits(rec.Transform[0]), math.Float32frombits(rec.Transform[1])},
				{math.Float32frombits(rec.Transform[2]), math.Float32frombits(rec.Transform[3])},
			},
			Translation: [2]float32{
				math.Float32frombits(rec.Transform[4]),
				math.Float32frombits(rec.Transform[5]),
			},
		},
		CornerRadii: Corners{
			TopLeft:     math.Float32frombits(rec.CornerRadii[0]),
			TopRight:    math.Float32frombits(rec.CornerRadii[1]),
			BottomRight: math.Float32frombits(rec.CornerRadii[2]),
			BottomLeft:  math.Float32frombits(rec.CornerRadii[3]),
		},
		CornerSmoothing: math.Float32frombits(rec.CornerSmoothing),
	}
}

func sceneColorRecord(c Hsla) native.SceneColor {
	return native.SceneColorOf(c.H, c.S, c.L, c.A)
}

func sceneColorValue(rec native.SceneColor) Hsla {
	h, s, l, a := rec.HSLA()
	return Hsla{H: h, S: s, L: l, A: a}
}

// sceneBackgroundRecord packs a Go Background into the ABI record
// (browser-core ticket 40): the solid arm as-is, the gradient arm as
// the two-stop record with its CSS angle resolved against bounds (the
// corner keywords' magic-corner angle is bounds-dependent). The sRGB
// color space is CSS's legacy default and the only arm this
// revision draws exactly, so it is the only one the Go Background
// carries.
func sceneBackgroundRecord(b Background, bounds Bounds) native.SceneBackground {
	if b.Kind == BackgroundKindGradientTable {
		// rev 16, ticket 52: the table-gradient arm - the table index
		// rides the ANGLE field as f32 bits (the native validation
		// checks it).
		return native.SceneGradientTableBackground(b.GradientTable)
	}
	if b.Kind == BackgroundKindLinearGradient {
		angle := resolveGradientAngle(&b.Gradient, bounds.Size.Width, bounds.Size.Height)
		return native.SceneGradientBackground(
			angle,
			native.HslaF{H: b.Gradient.From.Color.H, S: b.Gradient.From.Color.S, L: b.Gradient.From.Color.L, A: b.Gradient.From.Color.A},
			b.Gradient.From.Position,
			native.HslaF{H: b.Gradient.To.Color.H, S: b.Gradient.To.Color.S, L: b.Gradient.To.Color.L, A: b.Gradient.To.Color.A},
			b.Gradient.To.Position,
		)
	}
	return native.SceneBackgroundOf(b.Solid.H, b.Solid.S, b.Solid.L, b.Solid.A)
}

// sceneBackgroundValue reads an ABI background record into the Go
// Background: the solid arm as-is, the gradient arm with the stored
// (already resolved) angle. A tag this Go surface does not carry (the
// record's reserved pattern tags) reads as a transparent solid, the
// kernel's never-inserted state.
func sceneBackgroundValue(rec native.SceneBackground) Background {
	if rec.Tag == 4 {
		// rev 16, ticket 52: the table-gradient arm (the index stored in
		// the angle field).
		index := uint32(math.Float32frombits(rec.Angle))
		return Background{Kind: BackgroundKindGradientTable, GradientTable: index}
	}
	if rec.Tag == 1 {
		from := native.HslaF{
			H: math.Float32frombits(rec.Stop0H),
			S: math.Float32frombits(rec.Stop0S),
			L: math.Float32frombits(rec.Stop0L),
			A: math.Float32frombits(rec.Stop0A),
		}
		to := native.HslaF{
			H: math.Float32frombits(rec.Stop1H),
			S: math.Float32frombits(rec.Stop1S),
			L: math.Float32frombits(rec.Stop1L),
			A: math.Float32frombits(rec.Stop1A),
		}
		return Background{
			Kind: BackgroundKindLinearGradient,
			Gradient: LinearGradient{
				Direction: GradientDirectionAngle,
				Angle:     math.Float32frombits(rec.Angle),
				From:      GradientStop{Color: Hsla{H: from.H, S: from.S, L: from.L, A: from.A}, Position: math.Float32frombits(rec.Stop0Percentage)},
				To:        GradientStop{Color: Hsla{H: to.H, S: to.S, L: to.L, A: to.A}, Position: math.Float32frombits(rec.Stop1Percentage)},
			},
		}
	}
	h, s, l, a := rec.Solid()
	return Background{Kind: BackgroundKindSolid, Solid: Hsla{H: h, S: s, L: l, A: a}}
}

func cornersValue(bits [4]uint32) Corners {
	return Corners{
		TopLeft:     math.Float32frombits(bits[0]),
		TopRight:    math.Float32frombits(bits[1]),
		BottomRight: math.Float32frombits(bits[2]),
		BottomLeft:  math.Float32frombits(bits[3]),
	}
}

func edgesValue(bits [4]uint32) Edges {
	return Edges{
		Top:    math.Float32frombits(bits[0]),
		Right:  math.Float32frombits(bits[1]),
		Bottom: math.Float32frombits(bits[2]),
		Left:   math.Float32frombits(bits[3]),
	}
}

func filterRecordOf(order uint32, bounds, mask Bounds, radii Corners, smoothing, blur float32, matrix [160]float32, matrixCount uint32, opacity float32, isStart bool) native.SceneFilterRecord {
	rec := native.NewSceneFilterRecord()
	rec.Order = order
	rec.Bounds = sceneBoundsRecord(bounds)
	rec.ContentMask = sceneContentMaskRecord(mask)
	for i, radius := range [4]float32{radii.TopLeft, radii.TopRight, radii.BottomRight, radii.BottomLeft} {
		rec.CornerRadii[i] = math.Float32bits(radius)
	}
	rec.CornerSmoothing = math.Float32bits(smoothing)
	rec.Opacity = math.Float32bits(opacity)
	if isStart {
		rec.IsStart = 1
	}
	// The chain's payload (rev 15.1): one blur entry and one matrix
	// entry PER colour function, in chain order. The blur goes first
	// (the draw path blurs, then the composite applies the matrices
	// with the per-function clamps), and a matrix entry's blur slot
	// stays zero — the record's validation demands that.
	count := 0
	if blur > 0 {
		rec.BlurRadii[0] = math.Float32bits(blur)
		rec.FilterTags[0] = 0
		count = 1
	}
	for slot := 0; slot < int(matrixCount); slot++ {
		rec.FilterTags[count] = 1
		count++
		copy(rec.ColorMatrix[20*slot:20*(slot+1)], bitsOf(matrix[20*slot:20*(slot+1)]))
	}
	rec.ColorMatrixCount = matrixCount
	rec.FilterCount = uint32(count)
	return rec
}

// bitsOf packs one matrix slot's floats as f32 bits.
func bitsOf(values []float32) []uint32 {
	bits := make([]uint32, len(values))
	for i, value := range values {
		bits[i] = math.Float32bits(value)
	}
	return bits
}

func backdropValue(rec native.SceneFilterRecord) BackdropFilter {
	return BackdropFilter{
		Order:            rec.Order,
		Bounds:           sceneBoundsValue(rec.Bounds),
		ContentMask:      sceneContentMaskValue(rec.ContentMask),
		CornerRadii:      cornersValue(rec.CornerRadii),
		CornerSmoothing:  math.Float32frombits(rec.CornerSmoothing),
		BlurRadius:       math.Float32frombits(rec.BlurRadii[0]),
		ColorMatrix:      colorMatrixValue(rec),
		ColorMatrixCount: rec.ColorMatrixCount,
		Opacity:          math.Float32frombits(rec.Opacity),
	}
}

func boundaryValue(rec native.SceneFilterRecord) FilterBoundary {
	return FilterBoundary{
		Order:            rec.Order,
		Bounds:           sceneBoundsValue(rec.Bounds),
		ContentMask:      sceneContentMaskValue(rec.ContentMask),
		CornerRadii:      cornersValue(rec.CornerRadii),
		CornerSmoothing:  math.Float32frombits(rec.CornerSmoothing),
		BlurRadius:       math.Float32frombits(rec.BlurRadii[0]),
		ColorMatrix:      colorMatrixValue(rec),
		ColorMatrixCount: rec.ColorMatrixCount,
		OpChainIndex:     rec.OpChainIndex,
		OpChainCount:     rec.OpChainCount,
		Opacity:          math.Float32frombits(rec.Opacity),
		IsStart:          rec.IsStart == 1,
	}
}

// colorMatrixValue unpacks the record's per-function matrix slots into
// floats (all zeros when the count is zero).
func colorMatrixValue(rec native.SceneFilterRecord) [160]float32 {
	var matrix [160]float32
	for i, bits := range rec.ColorMatrix {
		matrix[i] = math.Float32frombits(bits)
	}
	return matrix
}
