package gpui

import (
	"fmt"
	"math"
	"sync"

	"gpui-go/internal/native"
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
	rec.ContentMask = sceneBoundsRecord(q.ContentMask)
	rec.Background = sceneColorRecord(q.Background.Solid)
	rec.BorderColor = sceneColorRecord(q.BorderColor.Solid)
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
	rec.ContentMask = sceneBoundsRecord(sh.ContentMask)
	rec.Color = sceneColorRecord(sh.Color.Solid)
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
	rec.ContentMask = sceneBoundsRecord(u.ContentMask)
	rec.Color = sceneColorRecord(u.Color)
	rec.Thickness = math.Float32bits(u.Thickness)
	if u.Wavy {
		rec.Wavy = 1
	}
	if err := k.scene.InsertUnderline(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert underline: %w", err)
	}
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
	rec := filterRecordOf(f.Order, f.Bounds, f.ContentMask, f.CornerRadii, f.CornerSmoothing, f.BlurRadius, f.Opacity, false)
	if err := k.scene.InsertBackdropFilter(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert backdrop filter: %w", err)
	}
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
	rec := filterRecordOf(b.Order, b.Bounds, b.ContentMask, b.CornerRadii, b.CornerSmoothing, b.BlurRadius, b.Opacity, b.IsStart)
	if err := k.scene.InsertFilterBoundary(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert filter boundary: %w", err)
	}
	return nil
}

// InsertSurface inserts a surface record with its paired opacity.
func (k *SceneKernel) InsertSurface(bounds, contentMask Bounds, source SurfaceSourceTag, opacity float32) error {
	if err := k.checkLive("InsertSurface"); err != nil {
		return err
	}
	rec := native.NewSceneSurfaceRecord()
	rec.Bounds = sceneBoundsRecord(bounds)
	rec.ContentMask = sceneBoundsRecord(contentMask)
	rec.SourceTag = uint32(source)
	if err := k.scene.InsertSurface(&rec, opacity); err != nil {
		return fmt.Errorf("gpui: scene insert surface: %w", err)
	}
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
			ContentMask:     sceneBoundsValue(rec.ContentMask),
			Background:      Background{Solid: sceneColorValue(rec.Background)},
			BorderColor:     Background{Solid: sceneColorValue(rec.BorderColor)},
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
			ContentMask:        sceneBoundsValue(rec.ContentMask),
			Color:              Background{Solid: sceneColorValue(rec.Color)},
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
			ContentMask: sceneBoundsValue(rec.ContentMask),
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
			ContentMask: sceneBoundsValue(rec.ContentMask),
			Source:      SurfaceSourceTag(rec.SourceTag),
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
		IsFinished:            rec.IsFinished == 1,
	}, nil
}

// ---------------------------------------------------------------------------
// ABI record translation helpers
// ---------------------------------------------------------------------------

func sceneBoundsRecord(b Bounds) native.SceneBounds {
	return native.SceneBoundsOf(b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height)
}

func sceneBoundsValue(rec native.SceneBounds) Bounds {
	x, y, w, h := rec.Rect()
	return Bounds{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
}

func sceneColorRecord(c Hsla) native.SceneColor {
	return native.SceneColorOf(c.H, c.S, c.L, c.A)
}

func sceneColorValue(rec native.SceneColor) Hsla {
	h, s, l, a := rec.HSLA()
	return Hsla{H: h, S: s, L: l, A: a}
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

func filterRecordOf(order uint32, bounds, mask Bounds, radii Corners, smoothing, blur, opacity float32, isStart bool) native.SceneFilterRecord {
	rec := native.NewSceneFilterRecord()
	rec.Order = order
	rec.Bounds = sceneBoundsRecord(bounds)
	rec.ContentMask = sceneBoundsRecord(mask)
	for i, radius := range [4]float32{radii.TopLeft, radii.TopRight, radii.BottomRight, radii.BottomLeft} {
		rec.CornerRadii[i] = math.Float32bits(radius)
	}
	rec.CornerSmoothing = math.Float32bits(smoothing)
	rec.BlurRadii[0] = math.Float32bits(blur)
	rec.FilterCount = 1
	rec.Opacity = math.Float32bits(opacity)
	if isStart {
		rec.IsStart = 1
	}
	return rec
}

func backdropValue(rec native.SceneFilterRecord) BackdropFilter {
	return BackdropFilter{
		Order:           rec.Order,
		Bounds:          sceneBoundsValue(rec.Bounds),
		ContentMask:     sceneBoundsValue(rec.ContentMask),
		CornerRadii:     cornersValue(rec.CornerRadii),
		CornerSmoothing: math.Float32frombits(rec.CornerSmoothing),
		BlurRadius:      math.Float32frombits(rec.BlurRadii[0]),
		Opacity:         math.Float32frombits(rec.Opacity),
	}
}

func boundaryValue(rec native.SceneFilterRecord) FilterBoundary {
	return FilterBoundary{
		Order:           rec.Order,
		Bounds:          sceneBoundsValue(rec.Bounds),
		ContentMask:     sceneBoundsValue(rec.ContentMask),
		CornerRadii:     cornersValue(rec.CornerRadii),
		CornerSmoothing: math.Float32frombits(rec.CornerSmoothing),
		BlurRadius:      math.Float32frombits(rec.BlurRadii[0]),
		Opacity:         math.Float32frombits(rec.Opacity),
		IsStart:         rec.IsStart == 1,
	}
}
