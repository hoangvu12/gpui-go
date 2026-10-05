package gpui

import (
	"errors"
	"fmt"

	"gpui-go/internal/native"
)

// This file is the port's scene half of ticket08: the scene record types
// field-for-field with the pinned GPUI-CE structs
// (crates/gpui/src/scene.rs at 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a)
// and the public Scene type over the native scene kernel (reserved slot
// 3, capability "scene-kernel-v1"; see internal/native/scene.go and
// reference/native/SCENE_ABI.md).
//
// The scene is DEVICE-PIXEL space (the pinned ScaledPixels): every
// record's Bounds and ContentMask are device pixels, produced by the
// paint-method ports in paint.go (snap_bounds / cover_bounds / stroke
// snapping under the window scale factor). The semantic paint commands
// (logical pixels, element opacity) are the paint API below
// (PaintQuad, PaintShadow, PaintUnderline, PaintBackdrop,
// BeginLayer/EndLayer, BeginFilterGroup/EndFilterGroup,
// RaiseOrderFloor, Replay), mirroring the pinned Window paint methods
// one for one; the kernel computes the draw orders (bounds tree), sorts
// at finish and compiles the render plan (batches + filter targets),
// exactly as the pinned Scene does.
//
// The draw order fields on the records mirror the pinned DrawOrder: the
// caller leaves them zero; Insert assigns them.

// ---------------------------------------------------------------------------
// Scene records (the pinned scene.rs structs)
// ---------------------------------------------------------------------------

// Quad is a colored rectangular region with an optional background,
// border and corner radii (the pinned Quad). All geometry is device
// pixels; Order is kernel-assigned.
type Quad struct {
	// Order is the draw order (kernel-assigned on insert).
	Order uint32
	// Bounds of the quad, device pixels.
	Bounds Bounds
	// ContentMask is the clip bounds, device pixels.
	ContentMask Bounds
	// Background color of the quad.
	Background Background
	// BorderColor is the background painted into the quad's borders.
	BorderColor Background
	// CornerRadii, device pixels.
	CornerRadii Corners
	// BorderWidths, device pixels (stroke-snapped by the paint path).
	BorderWidths Edges
	// Style of the quad's borders.
	BorderStyle BorderStyle
	// DashedLength / DashedGap: the dashed border parameters (multiples
	// of the border width).
	DashedLength float32
	DashedGap    float32
	// CornerSmoothing in [0, 1].
	CornerSmoothing float32
	// Padding (payload; the pinned field, 0 from the paint path).
	Padding uint32
}

// Shadow is a box shadow record (the pinned Shadow). Geometry is device
// pixels; Order is kernel-assigned.
type Shadow struct {
	Order uint32
	// BlurRadius, device pixels.
	BlurRadius float32
	// Bounds of the (dilated) shadow region.
	Bounds Bounds
	// CornerRadii of the shadow shape.
	CornerRadii Corners
	// ContentMask is the clip bounds.
	ContentMask Bounds
	// Color of the shadow.
	Color Background
	// ElementBounds is the shadow's un-dilated frame.
	ElementBounds Bounds
	// ElementCornerRadii of the element.
	ElementCornerRadii Corners
	// Inset draws the shadow inside the element's bounds.
	Inset bool
	// CornerSmoothing in [0, 1].
	CornerSmoothing float32
}

// Underline is an underline record (the pinned Underline). Geometry is
// device pixels; Order is kernel-assigned.
type Underline struct {
	Order uint32
	// Padding (payload; 0 from the paint path).
	Padding uint32
	// Bounds of the underline.
	Bounds Bounds
	// ContentMask is the clip bounds.
	ContentMask Bounds
	// Color (the pinned SceneHsla).
	Color Hsla
	// Thickness of the stroke, device pixels.
	Thickness float32
	// Wavy draws the wavy underline (height = 3x thickness).
	Wavy bool
}

// BackdropFilter blurs the content already rendered behind its bounds
// (the frosted-glass effect; the pinned BackdropFilter). Geometry is
// device pixels; Order is kernel-assigned.
type BackdropFilter struct {
	Order uint32
	// Bounds of the filtered region.
	Bounds Bounds
	// ContentMask is the clip bounds.
	ContentMask Bounds
	// CornerRadii of the rounded composite.
	CornerRadii Corners
	// CornerSmoothing in [0, 1].
	CornerSmoothing float32
	// BlurRadius is the gaussian blur radius, device pixels (this slice
	// carries a single-filter chain, the pinned SmallVec's one entry).
	BlurRadius float32
	// Opacity is the element opacity captured at paint time, multiplied
	// into the composited result.
	Opacity float32
}

// FilterBoundary is the start or end marker of a content-filter
// isolation group (the pinned FilterBoundary): the element's subtree is
// painted between a matched start/end pair; the renderer redirects that
// span into an offscreen target, filters it and composites it back.
type FilterBoundary struct {
	Order uint32
	// Bounds of the group.
	Bounds Bounds
	// ContentMask is the clip bounds.
	ContentMask Bounds
	// CornerRadii of the rounded composite.
	CornerRadii Corners
	// CornerSmoothing in [0, 1].
	CornerSmoothing float32
	// BlurRadius is the gaussian blur radius, device pixels.
	BlurRadius float32
	// Opacity of the group (the pinned snapshot uses 1.0).
	Opacity float32
	// IsStart opens the group; false closes it.
	IsStart bool
}

// ---------------------------------------------------------------------------
// Plan types (the pinned plan.rs)
// ---------------------------------------------------------------------------

// SceneCommandKind is one compiled render-plan command kind.
type SceneCommandKind uint8

const (
	// SceneCommandKindBatch is a normal primitive batch.
	SceneCommandKindBatch SceneCommandKind = 0
	// SceneCommandKindBeginFilter begins a content-filter group.
	SceneCommandKindBeginFilter SceneCommandKind = 1
	// SceneCommandKindEndFilter finishes and composites a group.
	SceneCommandKindEndFilter SceneCommandKind = 2
)

// SceneBatchKind is the primitive class of a batch command (the reserved
// path/sprite kinds are never emitted by this revision).
type SceneBatchKind uint8

const (
	SceneBatchShadows         SceneBatchKind = 0
	SceneBatchQuads           SceneBatchKind = 1
	SceneBatchPaths           SceneBatchKind = 2 // reserved
	SceneBatchUnderlines      SceneBatchKind = 3
	SceneBatchMonochrome      SceneBatchKind = 4 // reserved
	SceneBatchSubpixel        SceneBatchKind = 5 // reserved
	SceneBatchPolychrome      SceneBatchKind = 6 // reserved
	SceneBatchSurfaces        SceneBatchKind = 7
	SceneBatchBackdropFilters SceneBatchKind = 8
	SceneBatchFilterBoundary  SceneBatchKind = 9
)

func (k SceneBatchKind) String() string {
	switch k {
	case SceneBatchShadows:
		return "shadows"
	case SceneBatchQuads:
		return "quads"
	case SceneBatchPaths:
		return "paths"
	case SceneBatchUnderlines:
		return "underlines"
	case SceneBatchMonochrome:
		return "monochrome-sprites"
	case SceneBatchSubpixel:
		return "subpixel-sprites"
	case SceneBatchPolychrome:
		return "polychrome-sprites"
	case SceneBatchSurfaces:
		return "surfaces"
	case SceneBatchBackdropFilters:
		return "backdrop-filters"
	case SceneBatchFilterBoundary:
		return "filter-boundary"
	default:
		return "unknown"
	}
}

// FilterTarget describes where a filter group renders.
type FilterTarget struct {
	// Isolated is true when the group renders into a dedicated offscreen
	// target (the first two nested groups; deeper groups render inline).
	Isolated bool
	// Index is the offscreen target pool index when isolated.
	Index uint32
}

// SceneCommand is one compiled render-plan command: a primitive batch
// (with its array range and smoothing split) or a filter begin/end (with
// the boundary indices and the isolation target).
type SceneCommand struct {
	// Kind of the command.
	Kind SceneCommandKind
	// BatchKind is the primitive class for batch commands.
	BatchKind SceneBatchKind
	// Range is the [start, end) primitive-array range for batch commands.
	Range [2]uint32
	// Smoothed marks corner-smoothed batches (quads and shadows).
	Smoothed bool
	// TextureIndex is the sprite batch's atlas texture (the pinned
	// PrimitiveBatch texture_id; the kind pool is implied by the batch
	// kind).
	TextureIndex uint32
	// RasterizationVertexCount is the path batch's summed vertex count
	// (the pinned PrimitiveBatch::Paths field).
	RasterizationVertexCount uint32
	// SpriteCount is the path batch's sprite count (the pinned
	// PrimitiveBatch::Paths field).
	SpriteCount uint32
	// BoundaryIndex is the opening boundary index for filter commands
	// (end commands carry the MATCHED start's index).
	BoundaryIndex uint32
	// ClosingBoundaryIndex is the closing marker's index for end
	// commands (begin commands repeat their own index).
	ClosingBoundaryIndex uint32
	// Target of the filter group.
	Target FilterTarget
}

// SceneRequirements are the resource totals computed while a scene's
// render plan is compiled (the pinned ScenePlanRequirements).
type SceneRequirements struct {
	CommandCount                 uint32
	InstanceBatchCount           uint32
	PathRasterizationVertexCount uint32
	PathSpriteCount              uint32
	SurfaceCount                 uint32
	BackdropFilterCount          uint32
	IsolatedFilterCount          uint32
	IsolatedTargetCount          uint32
	UsesPathTarget               bool
	UsesOffscreenTarget          bool
}

// SceneMeta is the scene totals observable (op count, layer pushes,
// per-class primitive counts, finished flag).
type SceneMeta struct {
	OpCount        uint32
	LayerPushCount uint32
	QuadCount      uint32
	ShadowCount    uint32
	UnderlineCount uint32
	BackdropCount  uint32
	BoundaryCount  uint32
	SurfaceCount   uint32
	// Sprite counts (ticket10's ABI; surfaced in the meta since
	// ticket16's paths-filters fixture records them).
	MonochromeSpriteCount uint32
	SubpixelSpriteCount   uint32
	PolychromeSpriteCount uint32
	// PathCount is the path primitive count (ticket16).
	PathCount  uint32
	IsFinished bool
}

// ---------------------------------------------------------------------------
// Scene errors
// ---------------------------------------------------------------------------

// Scene errors, distinguishable with errors.Is. They wrap the native
// scene service's status errors (internal/native) with the gpui seam's
// context.
var (
	// ErrSceneUnavailable reports a failure to load/validate the native
	// scene kernel service.
	ErrSceneUnavailable = errors.New("gpui: native scene kernel service unavailable")
	// ErrSceneClosed reports use of a disposed scene.
	ErrSceneClosed = errors.New("gpui: scene is closed")
)

// ---------------------------------------------------------------------------
// The scene
// ---------------------------------------------------------------------------

// Scene is the port's scene: ordered semantic paint commands (this Go
// type) backed by the native scene kernel (bounds-tree draw orders,
// layers, order floors, finish/sort, batching, filter-target planning,
// paired surface opacity, replay). Create one with NewScene; the
// underlying native artifact is loaded once per process and shared.
//
// A Scene runs on its owning foreground thread and is not safe for
// concurrent use; same-scene re-entry is rejected by the busy guard
// before native entry.
type Scene struct {
	kernel *SceneKernel
}

// NewScene creates a fresh empty scene backed by the native scene kernel.
func NewScene() (*Scene, error) {
	kernel, err := newSceneKernel()
	if err != nil {
		return nil, err
	}
	return &Scene{kernel: kernel}, nil
}

// Dispose frees the scene's native state; the scene is unusable
// afterwards. In-flight native calls hold their own references, so
// actual destruction is deferred until they return.
func (s *Scene) Dispose() error {
	if s.kernel == nil {
		return ErrSceneClosed
	}
	return s.kernel.Dispose()
}

// Len returns the scene's paint-operation count (the replay address
// space; the pinned Scene::len).
func (s *Scene) Len() (uint32, error) { return s.kernel.Len() }

// PushLayer pushes a paint layer with device-pixel bounds (the pinned
// Scene::push_layer): the layer's order comes from the bounds tree and
// is shared by every primitive painted inside the layer.
func (s *Scene) PushLayer(bounds Bounds) error { return s.kernel.BeginLayer(bounds) }

// PopLayer pops the innermost layer.
func (s *Scene) PopLayer() error { return s.kernel.EndLayer() }

// RaiseOrderFloor raises the deferred-draw order floor: every primitive
// inserted afterwards sorts above everything inserted before (the
// pinned Scene::raise_order_floor; overlays and their backdrops).
func (s *Scene) RaiseOrderFloor() error { return s.kernel.RaiseOrderFloor() }

// InsertQuad inserts a quad record (the pinned
// Scene::insert_primitive(Quad)). Order must be zero; the kernel
// assigns it. A quad whose clipped bounds are empty is dropped.
func (s *Scene) InsertQuad(q Quad) error { return s.kernel.InsertQuad(q) }

// InsertShadow inserts a shadow record.
func (s *Scene) InsertShadow(sh Shadow) error { return s.kernel.InsertShadow(sh) }

// InsertUnderline inserts an underline record.
func (s *Scene) InsertUnderline(u Underline) error { return s.kernel.InsertUnderline(u) }

// InsertBackdropFilter inserts a backdrop-filter record.
func (s *Scene) InsertBackdropFilter(f BackdropFilter) error {
	return s.kernel.InsertBackdropFilter(f)
}

// InsertFilterBoundary inserts a content-filter boundary marker. The
// start and end of a group must carry the same snapshot (the pinned
// with_filter_layer snapshots once); unmatched starts render inline.
func (s *Scene) InsertFilterBoundary(b FilterBoundary) error {
	return s.kernel.InsertFilterBoundary(b)
}

// SurfaceSourceTag tags a painted surface's source kind (payload; 0 is
// the none/unsupported stand-in of this slice).
type SurfaceSourceTag uint32

// SurfaceSourceNone is the unsupported/none source tag.
const SurfaceSourceNone SurfaceSourceTag = 0

// InsertSurface inserts a surface record with its paired opacity (the
// pinned insert_surface): the opacity stays paired with the surface
// through the finish sort and replay.
func (s *Scene) InsertSurface(bounds, contentMask Bounds, source SurfaceSourceTag, opacity float32) error {
	return s.kernel.InsertSurface(bounds, contentMask, source, opacity)
}

// Replay re-inserts the range [start, end) of prev's paint operations
// into this scene (the pinned Scene::replay): the re-inserted records
// keep their parameters but their orders are recomputed against THIS
// scene's bounds tree and layer state. The caller keeps prev pinned
// while ranges reference it; a scene cannot replay itself.
func (s *Scene) Replay(start, end uint32, prev *Scene) error {
	if prev == nil {
		return fmt.Errorf("gpui: Replay: nil source scene")
	}
	if prev.kernel == s.kernel || (prev.kernel != nil && s.kernel != nil && prev.kernel.nativeScene() == s.kernel.nativeScene()) {
		return fmt.Errorf("gpui: Replay: a scene cannot replay itself")
	}
	return s.kernel.Replay(start, end, prev.kernel)
}

// Finish sorts the primitive arrays and compiles the render plan (the
// pinned Scene::finish). Plan/requirements/dump reads require it.
func (s *Scene) Finish() error { return s.kernel.Finish() }

// Quads returns the finished scene's quads in draw order.
func (s *Scene) Quads() ([]Quad, error) { return s.kernel.Quads() }

// Shadows returns the finished scene's shadows in draw order.
func (s *Scene) Shadows() ([]Shadow, error) { return s.kernel.Shadows() }

// Underlines returns the finished scene's underlines in draw order.
func (s *Scene) Underlines() ([]Underline, error) { return s.kernel.Underlines() }

// BackdropFilters returns the finished scene's backdrop filters in draw
// order.
func (s *Scene) BackdropFilters() ([]BackdropFilter, error) { return s.kernel.BackdropFilters() }

// FilterBoundaries returns the finished scene's filter boundaries in
// draw order (start before end at equal orders).
func (s *Scene) FilterBoundaries() ([]FilterBoundary, error) { return s.kernel.FilterBoundaries() }

// SurfaceRecord is a painted surface record (the pinned PaintSurface
// shape, reduced to the ABI payload): device-pixel bounds, the clip and
// the opaque source tag.
type SurfaceRecord struct {
	Order       uint32
	Bounds      Bounds
	ContentMask Bounds
	Source      SurfaceSourceTag
}

// Surfaces returns the finished scene's surfaces in draw order together
// with their paired opacities (the pairing survives the sort and
// replay).
func (s *Scene) Surfaces() ([]SurfaceRecord, []float32, error) {
	return s.kernel.Surfaces()
}

// Commands returns the compiled render plan: the backend-neutral work
// order (batches with primitive ranges, and paired filter begin/end
// commands with their isolation targets).
// Handle returns the scene's native handle (renderer-facing; ticket16's
// draw entries resolve scenes through it).
func (s *Scene) Handle() native.SceneHandle {
	if s.kernel == nil || s.kernel.scene == nil {
		return 0
	}
	return native.SceneHandle(s.kernel.scene.Handle())
}

// FinishIfNeeded finishes the scene unless it is already finished (the
// draw entries require the compiled plan).
func (s *Scene) FinishIfNeeded() error {
	if s.kernel == nil {
		return ErrSceneClosed
	}
	meta, err := s.Meta()
	if err != nil {
		return err
	}
	if meta.IsFinished {
		return nil
	}
	return s.Finish()
}

func (s *Scene) Commands() ([]SceneCommand, error) { return s.kernel.Plan() }

// Paths returns the finished scene's path records with their
// kernel-assigned orders and the vertices concatenated per record
// (the pinned scene.paths). Requires Finish.
func (s *Scene) Paths() ([]PathRecord, [][]PathVertex, error) {
	return s.kernel.Paths()
}

// Requirements returns the compiled plan's resource totals.
func (s *Scene) Requirements() (SceneRequirements, error) { return s.kernel.Requirements() }

// Meta returns the scene totals (op count, layer pushes, primitive
// counts, finished flag).
func (s *Scene) Meta() (SceneMeta, error) { return s.kernel.Meta() }

// ---------------------------------------------------------------------------
// The paint API (semantic commands; the pinned Window paint methods)
// ---------------------------------------------------------------------------

// PaintContext carries the state the paint methods need beyond their
// explicit parameters: the window scale factor, the current content
// mask and the current element opacity (the pinned Window's
// element_opacity, multiplied by nested BeginLayer/with_element_opacity
// scopes). The zero value paints at scale 1 with an unbounded mask and
// full opacity.
type PaintContext struct {
	// ScaleFactor converts logical to device pixels.
	ScaleFactor float32
	// Mask is the current content mask in logical pixels.
	Mask Bounds
	// ElementOpacity is the accumulated element opacity.
	ElementOpacity float32
}

// NewPaintContext builds a paint context (scale, logical mask, element
// opacity).
func NewPaintContext(scale float32, mask Bounds, elementOpacity float32) *PaintContext {
	return &PaintContext{ScaleFactor: scale, Mask: mask, ElementOpacity: elementOpacity}
}

// PaintQuad paints a styled box (the pinned paint_quad, including the
// border-only split into strips around the empty interior).
func (s *Scene) PaintQuad(pq PaintQuad, cornerSmoothing float32, ctx *PaintContext) error {
	record := paintQuadFor(pq, ctx.Mask, ctx.ScaleFactor, ctx.ElementOpacity, cornerSmoothing)
	if !record.Background.IsTransparent() {
		return s.InsertQuad(record)
	}
	inner := largestBorderInterior(record)
	if isEmptyBounds(inner) {
		return s.InsertQuad(record)
	}
	for _, strip := range paintQuadStrips(record) {
		if err := s.InsertQuad(strip); err != nil {
			return err
		}
	}
	return nil
}

// PaintShadow paints one box shadow (the pinned paint_drop_shadows for
// non-inset shadows, paint_inset_shadows for inset ones), with the
// element's corner radii.
func (s *Scene) PaintShadow(elementBounds Bounds, shadow BoxShadow, elementRadii Corners, cornerSmoothing float32, ctx *PaintContext) error {
	var record Shadow
	if shadow.Inset {
		record = insetShadowFor(elementBounds, shadow, ctx.Mask, ctx.ScaleFactor, ctx.ElementOpacity, elementRadii, cornerSmoothing)
	} else {
		record = dropShadowFor(elementBounds, shadow, ctx.Mask, ctx.ScaleFactor, ctx.ElementOpacity, elementRadii, cornerSmoothing)
	}
	return s.InsertShadow(record)
}

// PaintUnderline paints an underline (the pinned paint_underline) at the
// logical origin with the given logical width.
func (s *Scene) PaintUnderline(origin Point, width float32, style UnderlineStyle, ctx *PaintContext) error {
	return s.InsertUnderline(underlineFor(origin, width, style, ctx.Mask, ctx.ScaleFactor, ctx.ElementOpacity))
}

// BeginLayer begins a paint layer (the pinned paint_layer): the clipped
// (bounds ∩ mask) layer is pushed with cover bounds when non-empty. The
// element-opacity scope (the pinned with_element_opacity) is the
// caller's: BeginLayer does not change it; use WithElementOpacity.
func (s *Scene) BeginLayer(bounds Bounds, ctx *PaintContext) error {
	clipped := intersectLogical(bounds, ctx.Mask)
	if isEmptyBounds(clipped) {
		return nil
	}
	return s.PushLayer(coverBounds(clipped, ctx.ScaleFactor))
}

// EndLayer ends the innermost paint layer; the caller must only call it
// for a layer whose begin actually pushed (the clipped bounds were
// non-empty), mirroring the pinned paint_layer's symmetric guards.
func (s *Scene) EndLayer() error { return s.PopLayer() }

// WithElementOpacity scopes an element opacity (the pinned
// with_element_opacity): the paint methods called with the returned
// context multiply their colors by the product of the nested opacities
// (nested scopes multiply; they never merge).
func (ctx *PaintContext) WithElementOpacity(opacity float32) *PaintContext {
	next := *ctx
	next.ElementOpacity = ctx.ElementOpacity * opacity
	return &next
}

// BeginFilterGroup inserts the start marker of a content-filter group
// (the pinned with_filter_layer: snapped bounds, scaled radii, the blur
// scaled to device pixels, opacity 1.0 — NOT element opacity). Identity
// blur radii (<= 0) run inline: no marker is inserted and
// EndFilterGroup must still be called (it is a no-op).
func (s *Scene) BeginFilterGroup(bounds Bounds, blurRadius float32, radii Corners, cornerSmoothing float32, ctx *PaintContext) error {
	boundary, ok := filterBoundaryFor(bounds, blurRadius, radii, ctx.Mask, ctx.ScaleFactor, cornerSmoothing)
	if !ok {
		return nil
	}
	return s.InsertFilterBoundary(boundary)
}

// EndFilterGroup closes the innermost content-filter group with the SAME
// snapshot as its begin (the caller re-supplies the parameters; the
// pinned with_filter_layer snapshots once — pass the identical values).
func (s *Scene) EndFilterGroup(bounds Bounds, blurRadius float32, radii Corners, cornerSmoothing float32, ctx *PaintContext) error {
	boundary, ok := filterBoundaryFor(bounds, blurRadius, radii, ctx.Mask, ctx.ScaleFactor, cornerSmoothing)
	if !ok {
		return nil
	}
	boundary.IsStart = false
	return s.InsertFilterBoundary(boundary)
}

// PaintBackdrop paints a backdrop filter (the pinned
// paint_backdrop_filter: the element opacity captured at paint time is
// stored on the record). Identity blur radii paint nothing.
func (s *Scene) PaintBackdrop(bounds Bounds, blurRadius float32, radii Corners, cornerSmoothing float32, ctx *PaintContext) error {
	filter, ok := backdropFilterFor(bounds, blurRadius, radii, ctx.Mask, ctx.ScaleFactor, ctx.ElementOpacity, cornerSmoothing)
	if !ok {
		return nil
	}
	return s.InsertBackdropFilter(filter)
}
