package gpui

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"gpui-go/internal/native"
)

// This file is the Go layout adapter over the native Taffy service
// (internal/native, the ticket06 ABI): the pinned rounding helpers, the
// Style-to-ABI-record translation, the LayoutEngine (node requests,
// compute, bounds) and the LayoutID generation.
//
// The reference semantics are the pinned GPUI-CE sources at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/util.rs — the rounding helpers (round_half_toward_zero
//     and friends), ported bit-for-bit below;
//   - crates/gpui/src/taffy.rs — into_taffy_style (unit conversion),
//     request_layout/request_measured_layout, compute_layout (available
//     space conversion and the measure closure), layout_bounds and
//     parent_relative_layout_bounds (the device round-trip snapping),
//     snap_measured_size_to_device_pixels, and padding_to_taffy's
//     proportional snap;
//   - crates/gpui/src/window.rs — layout_bounds adds the snapped element
//     offset (pixel_snap_point).
//
// Taffy's own rounding is disabled on the native side; every snap happens
// here, in device pixels. Public measurements and bounds are logical
// pixels. All arithmetic stays in f32 with the source's operation order.

// ---------------------------------------------------------------------------
// Pinned rounding helpers (crates/gpui/src/util.rs)
// ---------------------------------------------------------------------------

// roundHalfTowardZero rounds to the nearest integer with 0.5 ties toward
// zero: 1.5 rounds to 1 and -1.5 to -1, not away from zero or to even.
// It is the pinned util.rs round_half_toward_zero,
//
//	(value.abs() - 0.5).ceil().copysign(value)
//
// evaluated in f32. Signed zero is preserved through copysign: -0.5
// rounds to -0.0, which keeps its sign bit.
func roundHalfTowardZero(v float32) float32 {
	abs := float32(math.Abs(float64(v))) // v.abs(): exact for f32
	shifted := abs - 0.5                 // f32 subtract, as in Rust
	ceil := ceil32(shifted)              // f32 ceil
	return float32(math.Copysign(float64(ceil), float64(v)))
}

// roundToDevicePixel converts a logical length to device pixels, rounding
// to the nearest device pixel with midpoint ties toward zero (pinned
// util.rs round_to_device_pixel).
func roundToDevicePixel(logical, scaleFactor float32) float32 {
	return roundHalfTowardZero(logical * scaleFactor)
}

// floorToDevicePixel converts a logical length to device pixels, rounding
// down (pinned util.rs floor_to_device_pixel).
func floorToDevicePixel(logical, scaleFactor float32) float32 {
	return floor32(logical * scaleFactor)
}

// ceilToDevicePixel converts a logical length to device pixels, rounding
// up (pinned util.rs ceil_to_device_pixel).
func ceilToDevicePixel(logical, scaleFactor float32) float32 {
	return ceil32(logical * scaleFactor)
}

// roundStrokeToDevicePixel converts a logical stroke width to device
// pixels with the pinned stroke rule (util.rs
// round_stroke_to_device_pixel): an exact zero stays zero (the stroke
// disappears); anything else is clamped to at least one device pixel so
// thin strokes do not vanish.
func roundStrokeToDevicePixel(logical, scaleFactor float32) float32 {
	if logical == 0.0 {
		return 0.0
	}
	clamped := logical
	if clamped < 0.0 {
		clamped = 0.0
	}
	rounded := roundToDevicePixel(clamped, scaleFactor)
	if rounded < 1.0 {
		return 1.0
	}
	return rounded
}

// ceil32 is an f32 ceil (math.Ceil in float64 is exact for f32 inputs).
func ceil32(v float32) float32 { return float32(math.Ceil(float64(v))) }

// floor32 is an f32 floor.
func floor32(v float32) float32 { return float32(math.Floor(float64(v))) }

// maxZero32 clamps v to at least zero (f32::max(0.0)).
func maxZero32(v float32) float32 {
	if v < 0.0 {
		return 0.0
	}
	return v
}

// snapMeasuredSize snaps one measured dimension to device pixels with the
// pinned measured-size rule (taffy.rs snap_measured_size_to_device_pixels
// -> ceil_to_device_pixel(d.max(0.0), scale)): clamp the raw logical
// value to at least zero, multiply by the scale factor, then round up.
func snapMeasuredSize(d, scaleFactor float32) float32 {
	return ceilToDevicePixel(maxZero32(d), scaleFactor)
}

// pixelSnap snaps a logical coordinate to the device-pixel grid and back
// to logical pixels (window.rs pixel_snap:
// round_to_device_pixel(value, scale) / scale). It is applied to the
// element offset when layout bounds are queried.
func pixelSnap(value, scaleFactor float32) float32 {
	return roundToDevicePixel(value, scaleFactor) / scaleFactor
}

// pixelSnapPoint snaps both components of a logical point to the
// device-pixel grid and back (window.rs pixel_snap_point).
func pixelSnapPoint(p Point, scaleFactor float32) Point {
	return Point{X: pixelSnap(p.X, scaleFactor), Y: pixelSnap(p.Y, scaleFactor)}
}

// ---------------------------------------------------------------------------
// Style translation to the ABI record (taffy.rs into_taffy_style)
// ---------------------------------------------------------------------------

// lengthToLen translates a Length into an ABI GpuiLen for the
// LengthPercentageAuto fields (inset, margin) and the Dimension fields
// (size, min, max, flex basis): Auto -> tag 0, definite absolute ->
// definite device pixels, fraction -> percent.
func lengthToLen(l Length, remSize, scaleFactor float32) native.LayoutLen {
	if l.Auto {
		return native.AutoLen()
	}
	return definiteLengthToLen(l.Definite, remSize, scaleFactor)
}

// definiteLengthToLen translates a DefiniteLength into an ABI GpuiLen:
// absolute lengths become definite device pixels (pinned ToTaffy for
// AbsoluteLength: round_to_device_pixel(to_pixels(rem))), fractions stay
// percent fractions (no preconversion against a guessed parent).
func definiteLengthToLen(d DefiniteLength, remSize, scaleFactor float32) native.LayoutLen {
	switch d.Kind {
	case DefiniteLengthPx, DefiniteLengthRem:
		px := d.Absolute().ToPixels(remSize)
		return native.DefiniteLen(roundToDevicePixel(px, scaleFactor))
	case DefiniteLengthFraction:
		return native.PercentLen(d.Value)
	default:
		return native.AbsentLen()
	}
}

// lengthEdgesToRecord translates Edges<Length> into the ABI edges
// (top/right/bottom/left), used for inset and margin.
func lengthEdgesToRecord(edges LengthEdges, remSize, scaleFactor float32) native.LayoutEdges {
	return native.LayoutEdges{
		Top:    lengthToLen(edges.Top, remSize, scaleFactor),
		Right:  lengthToLen(edges.Right, remSize, scaleFactor),
		Bottom: lengthToLen(edges.Bottom, remSize, scaleFactor),
		Left:   lengthToLen(edges.Left, remSize, scaleFactor),
	}
}

// lengthSizeToRecord translates Size<Length> (size, min, max).
func lengthSizeToRecord(size LengthSize, remSize, scaleFactor float32) native.LayoutSize {
	return native.LayoutSize{
		Width:  lengthToLen(size.Width, remSize, scaleFactor),
		Height: lengthToLen(size.Height, remSize, scaleFactor),
	}
}

// definiteSizeToRecord translates Size<DefiniteLength> (gap): absolute
// lengths become definite device pixels, fractions stay percent.
func definiteSizeToRecord(size DefiniteLengthSize, remSize, scaleFactor float32) native.LayoutSize {
	return native.LayoutSize{
		Width:  definiteLengthToLen(size.Width, remSize, scaleFactor),
		Height: definiteLengthToLen(size.Height, remSize, scaleFactor),
	}
}

// paddingPair converts one padding edge pair, snapping proportionally on
// auto-sized axes. It is the port of taffy.rs padding_to_taffy's
// convert_pair + snap_absolute_pair: when the parent axis is auto-sized
// and both edges are absolute, the combined length is snapped and
// redistributed in the original ratio; explicit axes, percentage edges
// and the negative/zero-total fallback cases use the ordinary per-edge
// conversion.
func paddingPair(first, second DefiniteLength, remSize, scaleFactor float32, proportional bool) (native.LayoutLen, native.LayoutLen) {
	if proportional && first.Kind != DefiniteLengthFraction && second.Kind != DefiniteLengthFraction {
		// Both edges are absolute (DefiniteLength::Absolute).
		f := first.Absolute().ToPixels(remSize)
		s := second.Absolute().ToPixels(remSize)
		total := f + s
		if f < 0.0 || s < 0.0 || total == 0.0 {
			// Ordinary conversion for the fallback cases.
			return native.DefiniteLen(roundToDevicePixel(f, scaleFactor)),
				native.DefiniteLen(roundToDevicePixel(s, scaleFactor))
		}
		// Snap the combined length, then divide it by the authored ratio.
		snappedTotal := roundToDevicePixel(total, scaleFactor)
		snappedFirst := snappedTotal * (f / total)
		return native.DefiniteLen(snappedFirst), native.DefiniteLen(snappedTotal - snappedFirst)
	}
	return definiteLengthToLen(first, remSize, scaleFactor),
		definiteLengthToLen(second, remSize, scaleFactor)
}

// paddingToRecord translates the padding edges: the left/right pair snaps
// proportionally when the width axis is auto-sized, the top/bottom pair
// when the height axis is auto-sized (pinned padding_to_taffy passes
// proportional_horizontal for left/right and proportional_vertical for
// top/bottom).
func paddingToRecord(padding DefiniteLengthEdges, size LengthSize, remSize, scaleFactor float32) native.LayoutEdges {
	proportionalHorizontal := size.Width.Auto
	proportionalVertical := size.Height.Auto
	left, right := paddingPair(padding.Left, padding.Right, remSize, scaleFactor, proportionalHorizontal)
	top, bottom := paddingPair(padding.Top, padding.Bottom, remSize, scaleFactor, proportionalVertical)
	return native.LayoutEdges{Top: top, Right: right, Bottom: bottom, Left: left}
}

// gridTemplateToRecord fills one Option<GridTemplate> slot of the record
// (present, repeat, min size). The native side builds exactly
// repeat(<repeat>, minmax(<min>, 1fr)) the way the pinned to_grid_repeat
// does.
func gridTemplateToRecord(t *GridTemplate, present *uint32, repeat *uint32, minSize *uint32) {
	if t == nil {
		*present = 0
		*repeat = 0
		*minSize = uint32(GridTemplateMinZero)
		return
	}
	*present = 1
	*repeat = uint32(t.Repeat)
	*minSize = uint32(t.MinSize)
}

// gridPlacementToRecord fills one GridPlacement slot (kind + value; line
// values are the i16 bit pattern, span values are u16).
func gridPlacementToRecord(p GridPlacement, kind *uint32, value *uint32) {
	*kind = uint32(p.Kind)
	*value = p.Value
}

// displayToRecord maps the gpui Display onto the ABI display value,
// folding Inline to Block and InlineFlex to Flex exactly like the pinned
// Display -> taffy::style::Display conversion (GPUI keeps their inline
// semantics outside Taffy).
func displayToRecord(d Display) uint32 {
	switch d {
	case DisplayBlock, DisplayInline:
		return native.DisplayBlock
	case DisplayFlex, DisplayInlineFlex:
		return native.DisplayFlex
	case DisplayGrid:
		return native.DisplayGrid
	case DisplayNone:
		return native.DisplayNone
	default:
		return native.DisplayFlex
	}
}

// styleToRecord translates a Style into the ABI style record under the
// given layout context, mirroring the pinned into_taffy_style field for
// field: definite absolute lengths become device pixels (rounded with the
// pinned helpers), percentages stay fractions, Auto stays auto, borders
// use the stroke rule, and padding on auto-sized axes snaps
// proportionally. The record starts from the default record, but a
// complete Style overwrites every forwarded field, so absent tags only
// survive where the reference forwards an Option that is None
// (aspect ratio, alignments, grid properties).
func styleToRecord(s *Style, ctx *LayoutContext) native.LayoutStyleRecord {
	remSize := ctx.RemSize()
	scaleFactor := ctx.ScaleFactor()

	rec := native.NewLayoutStyleRecord()
	rec.Display = displayToRecord(s.Display)
	rec.Position = uint32(s.Position)
	rec.OverflowX = uint32(s.OverflowX)
	rec.OverflowY = uint32(s.OverflowY)
	// Scrollbar width is an AbsoluteLength forwarded as f32 device pixels
	// (pinned ToTaffy<f32> for AbsoluteLength).
	rec.ScrollbarWidth = native.DefiniteLen(
		roundToDevicePixel(s.ScrollbarWidth.ToPixels(remSize), scaleFactor))

	rec.Inset = lengthEdgesToRecord(s.Inset, remSize, scaleFactor)
	rec.Size = lengthSizeToRecord(s.Size, remSize, scaleFactor)
	rec.MinSize = lengthSizeToRecord(s.MinSize, remSize, scaleFactor)
	rec.MaxSize = lengthSizeToRecord(s.MaxSize, remSize, scaleFactor)
	if s.AspectRatio != nil {
		rec.AspectRatioPresent = 1
		rec.AspectRatioBits = math.Float32bits(*s.AspectRatio)
	}

	rec.Margin = lengthEdgesToRecord(s.Margin, remSize, scaleFactor)
	rec.Padding = paddingToRecord(s.Padding, s.Size, remSize, scaleFactor)
	// Border widths are always present and always stroke-snapped (the
	// reference's Edges<AbsoluteLength> defaults to zero, which the
	// stroke rule keeps zero).
	rec.BorderTop = math.Float32bits(roundStrokeToDevicePixel(s.BorderWidths.Top.ToPixels(remSize), scaleFactor))
	rec.BorderRight = math.Float32bits(roundStrokeToDevicePixel(s.BorderWidths.Right.ToPixels(remSize), scaleFactor))
	rec.BorderBottom = math.Float32bits(roundStrokeToDevicePixel(s.BorderWidths.Bottom.ToPixels(remSize), scaleFactor))
	rec.BorderLeft = math.Float32bits(roundStrokeToDevicePixel(s.BorderWidths.Left.ToPixels(remSize), scaleFactor))

	if s.AlignItems != nil {
		rec.AlignItemsPresent = 1
		rec.AlignItems = uint32(*s.AlignItems)
	}
	if s.AlignSelf != nil {
		rec.AlignSelfPresent = 1
		rec.AlignSelf = uint32(*s.AlignSelf)
	}
	if s.AlignContent != nil {
		rec.AlignContentPresent = 1
		rec.AlignContent = uint32(*s.AlignContent)
	}
	if s.JustifyContent != nil {
		rec.JustifyContentPresent = 1
		rec.JustifyContent = uint32(*s.JustifyContent)
	}
	rec.Gap = definiteSizeToRecord(s.Gap, remSize, scaleFactor)

	rec.FlexDirection = uint32(s.FlexDirection)
	rec.FlexWrap = uint32(s.FlexWrap)
	rec.FlexBasis = lengthToLen(s.FlexBasis, remSize, scaleFactor)
	// flex_grow and flex_shrink are plain f32 fields in the reference
	// Style and are always forwarded; an explicit zero shrink is a real
	// override, not an omitted default.
	rec.FlexGrowPresent = 1
	rec.FlexGrowBits = math.Float32bits(s.FlexGrow)
	rec.FlexShrinkPresent = 1
	rec.FlexShrinkBits = math.Float32bits(s.FlexShrink)

	gridTemplateToRecord(s.GridRows,
		&rec.GridTemplateRowsPresent, &rec.GridTemplateRowsRepeat, &rec.GridTemplateRowsMinSize)
	gridTemplateToRecord(s.GridCols,
		&rec.GridTemplateColumnsPresent, &rec.GridTemplateColumnsRepeat, &rec.GridTemplateColumnsMinSize)
	if s.GridLocation != nil {
		rec.GridPlacementPresent = 1
		gridPlacementToRecord(s.GridLocation.Row.Start, &rec.GridRowStartKind, &rec.GridRowStartValue)
		gridPlacementToRecord(s.GridLocation.Row.End, &rec.GridRowEndKind, &rec.GridRowEndValue)
		gridPlacementToRecord(s.GridLocation.Column.Start, &rec.GridColumnStartKind, &rec.GridColumnStartValue)
		gridPlacementToRecord(s.GridLocation.Column.End, &rec.GridColumnEndKind, &rec.GridColumnEndValue)
	}
	return rec
}

// ---------------------------------------------------------------------------
// LayoutID
// ---------------------------------------------------------------------------

// LayoutID identifies one layout node. It carries the owning engine's
// identity, the native node handle and the engine generation it was
// created in: after the engine is reset, native node handles become stale
// and the port-side generation makes the old ids fail fast with
// ErrLayoutIDStale. Layout ids are frame-scoped identities, NOT
// persistent element-state keys (layout contract: "Layout IDs include
// engine identity and generation and become unusable on engine reset").
type LayoutID struct {
	engine     *LayoutEngine
	node       native.NodeHandle
	generation uint64
}

// Engine returns the layout engine the id belongs to.
func (id LayoutID) Engine() *LayoutEngine { return id.engine }

// Layout id errors.
var (
	// ErrLayoutIDStale reports a layout id from before the engine's last
	// reset (or after its disposal): node handles do not survive engine
	// reset.
	ErrLayoutIDStale = errors.New("gpui: layout id is stale (engine was reset or disposed)")
	// ErrLayoutIDForeign reports a layout id that belongs to a different
	// layout engine.
	ErrLayoutIDForeign = errors.New("gpui: layout id belongs to a different layout engine")
)

// checkID validates that id belongs to this engine and its current
// generation, before any native call.
func (e *LayoutEngine) checkID(id LayoutID, op string) error {
	if id.engine != e {
		return fmt.Errorf("gpui: %s: %w", op, ErrLayoutIDForeign)
	}
	if id.generation != e.generation {
		return fmt.Errorf("gpui: %s: %w", op, ErrLayoutIDStale)
	}
	return nil
}

// ---------------------------------------------------------------------------
// The layout engine adapter
// ---------------------------------------------------------------------------

// layoutServiceState holds the process-wide native layout service: the
// embedded artifact is loaded once and stays loaded for the process
// lifetime (the distribution contract: no FreeLibrary, no hot unload),
// and the process-global measure trampoline is installed exactly once
// before any measured compute.
var (
	layoutServiceOnce sync.Once
	layoutServiceVal  *native.LayoutService
	layoutServiceErr  error
)

func layoutService() (*native.LayoutService, error) {
	layoutServiceOnce.Do(func() {
		lib, err := native.Load(native.Options{})
		if err != nil {
			layoutServiceErr = fmt.Errorf("gpui: loading the native layout artifact: %w", err)
			return
		}
		svc, err := lib.Layout()
		if err != nil {
			layoutServiceErr = fmt.Errorf("gpui: native layout service: %w", err)
			return
		}
		if err := svc.RegisterMeasureTrampoline(); err != nil {
			layoutServiceErr = fmt.Errorf("gpui: registering the measure trampoline: %w", err)
			return
		}
		layoutServiceVal = svc
	})
	return layoutServiceVal, layoutServiceErr
}

// devicePoint is an unrounded device-pixel coordinate.
type devicePoint struct {
	X float32
	Y float32
}

// LayoutEngine is the port's Taffy layout engine: one native engine plus
// the Go-side absolute-position caches the pinned adapter keeps
// (absolute_layout_bounds, absolute_outer_origins, computed_layouts).
//
// A LayoutEngine runs on its owning foreground thread and is not safe for
// concurrent use. While ComputeLayout runs, every other operation on the
// SAME engine fails with the engine-busy error before native entry (the
// same-engine re-entry guard of the layout contract); nesting a compute
// on a DIFFERENT engine from inside a measure function is allowed.
//
// Create one with NewLayoutEngine; the underlying native artifact is
// loaded once per process and shared.
type LayoutEngine struct {
	// native is the wrapped native engine (itself bound to the layout
	// service that created it).
	native *native.LayoutEngine

	// busy is the Go-side same-engine re-entry guard: it is set for the
	// whole duration of a ComputeLayout (including the measure
	// callbacks running inside it), so every other operation on THIS
	// engine fails with the engine-busy error before native entry — even
	// a cached bounds read, which the reference makes impossible by
	// taking the engine out of its window during compute. Independent
	// engines keep their own flags, so nesting a compute on a different
	// engine from inside a measure function stays allowed.
	busy atomic.Bool

	// generation stamps every LayoutID; bumped by Reset and Dispose so
	// stale ids fail the port-side generation check before native entry.
	generation uint64
	// computeSeq hands out unique compute tokens (panic attribution).
	computeSeq uint64
	// activeScale is the compute-time scale factor, read by the measure
	// wrappers while this engine is computing (the pinned measure closure
	// uses the window scale captured at compute time, not request time).
	activeScale float32

	// Cached snapped logical bounds per node (absolute_layout_bounds).
	absoluteLayoutBounds map[LayoutID]Bounds
	// Unrounded absolute border-box top-left per node in device pixels
	// (absolute_outer_origins).
	absoluteOuterOrigins map[LayoutID]devicePoint
	// Roots already computed (computed_layouts): recomputing a root
	// invalidates its subtree's cached bounds before layout runs.
	computedRoots map[LayoutID]bool

	// inlineContent is the inline content published per node during
	// request_layout (the reference TaffyLayoutEngine::inline_content;
	// GPUI-owned inline orchestration state, never forwarded to Taffy).
	inlineContent map[LayoutID]inlineContentRecord
	// inlineFragments is the fragment geometry placed per node during
	// prepaint (the reference TaffyLayoutEngine::inline_fragments).
	inlineFragments map[LayoutID][]Bounds
	// verticalAligns records non-Baseline vertical alignments per node
	// (the reference TaffyLayoutEngine::vertical_alignments).
	verticalAligns map[LayoutID]VerticalAlign
	// displayPositions records the display/position pair per node (the
	// reference TaffyLayoutEngine::display_and_position) for the inline
	// collector's classification.
	displayPositions map[LayoutID]displayPositionRecord
}

// NewLayoutEngine creates a fresh layout engine backed by the embedded
// native Taffy artifact (loaded and verified once per process; the
// process-global measure trampoline is registered before any measured
// compute). The reference creates one engine per window; the port's
// fixture creates one per window-equivalent scope.
func NewLayoutEngine() (*LayoutEngine, error) {
	svc, err := layoutService()
	if err != nil {
		return nil, err
	}
	eng, err := svc.CreateEngine()
	if err != nil {
		return nil, fmt.Errorf("gpui: creating the native layout engine: %w", err)
	}
	return &LayoutEngine{
		native:               eng,
		generation:           1,
		absoluteLayoutBounds: make(map[LayoutID]Bounds),
		absoluteOuterOrigins: make(map[LayoutID]devicePoint),
		computedRoots:        make(map[LayoutID]bool),
		inlineContent:        make(map[LayoutID]inlineContentRecord),
		inlineFragments:      make(map[LayoutID][]Bounds),
		verticalAligns:       make(map[LayoutID]VerticalAlign),
		displayPositions:     make(map[LayoutID]displayPositionRecord),
	}, nil
}

// checkBusy rejects operations while a ComputeLayout on this engine is
// running, before native entry (the contract's "reject same-engine
// re-entry before native entry").
func (e *LayoutEngine) checkBusy(op string) error {
	if e.busy.Load() {
		return fmt.Errorf("gpui: %s: %w (a compute is in progress)", op, native.ErrLayoutEngineBusy)
	}
	return nil
}

// RequestLayout adds a node with the given style and children to the
// layout tree and returns its layout id, mirroring the pinned
// TaffyLayoutEngine::request_layout: the style is translated under the
// context's rem scope and scale factor at request time (rems resolve
// here), children are attached in order (they must be unattached ids of
// this engine), and a node without children becomes a leaf.
func (e *LayoutEngine) RequestLayout(ctx *LayoutContext, style Style, children ...LayoutID) (LayoutID, error) {
	if err := e.checkBusy("RequestLayout"); err != nil {
		return LayoutID{}, err
	}
	if ctx == nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestLayout requires a layout context")
	}
	record := styleToRecord(&style, ctx)
	childHandles := make([]native.NodeHandle, len(children))
	for i, child := range children {
		if err := e.checkID(child, "RequestLayout"); err != nil {
			return LayoutID{}, err
		}
		childHandles[i] = child.node
	}
	node, err := e.native.CreateNode(&record, childHandles...)
	if err != nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestLayout: %w", err)
	}
	id := LayoutID{engine: e, node: node, generation: e.generation}
	e.recordInlineStyleState(id, &style)
	return id, nil
}

// recordInlineStyleState records the inline-orchestration style facts
// of a freshly requested node (the reference request_layout /
// request_measured_layout record vertical_align and
// display_and_position before returning the id).
func (e *LayoutEngine) recordInlineStyleState(id LayoutID, style *Style) {
	if style.VerticalAlign != VerticalAlignBaseline {
		e.verticalAligns[id] = style.VerticalAlign
	}
	e.displayPositions[id] = displayPositionRecord{display: style.Display, position: style.Position}
}

// displayPositionRecord is the display/position pair of one node
// (the reference display_and_position map entry).
type displayPositionRecord struct {
	display  Display
	position Position
}

// RequestMeasuredLayout adds a MEASURED LEAF node: a node whose size is
// determined by the given measure function instead of by children,
// mirroring the pinned request_measured_layout (new_leaf_with_context).
//
// Measured nodes are leaves by construction: this method takes no
// children, and a node created here can never gain any. The measure
// function runs synchronously on the calling thread from inside
// ComputeLayout, in logical pixels (the adapter converts the native
// device-pixel request and applies the pinned measured-size snapping to
// the result); see MeasureFunc for the re-entry and panic rules.
func (e *LayoutEngine) RequestMeasuredLayout(ctx *LayoutContext, style Style, measure MeasureFunc) (LayoutID, error) {
	if err := e.checkBusy("RequestMeasuredLayout"); err != nil {
		return LayoutID{}, err
	}
	if ctx == nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestMeasuredLayout requires a layout context")
	}
	if measure == nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestMeasuredLayout requires a non-nil measure function")
	}
	record := styleToRecord(&style, ctx)
	node, err := e.native.CreateNode(&record)
	if err != nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestMeasuredLayout: %w", err)
	}
	// The wrapper captures the engine (not a scale): the pinned measure
	// closure divides by the window scale captured at COMPUTE time, so
	// the wrapper reads the engine's active compute scale.
	token := e.native.RegisterMeasure(e.wrapMeasure(measure))
	if err := e.native.SetMeasure(node, token); err != nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestMeasuredLayout: %w", err)
	}
	id := LayoutID{engine: e, node: node, generation: e.generation}
	e.recordInlineStyleState(id, &style)
	return id, nil
}

// wrapMeasure adapts a logical-space MeasureFunc to the native
// device-pixel MeasureFunc, porting the pinned measure closure in
// taffy.rs::compute_layout: known dimensions and definite available space
// are divided by the (compute-time) scale factor to logical pixels, the
// user function returns raw logical pixels, and the pinned
// measured-size snapping (clamp >= 0, multiply by scale, ceil) is applied
// to the result before it crosses back.
func (e *LayoutEngine) wrapMeasure(measure MeasureFunc) native.MeasureFunc {
	return func(req native.GoMeasureRequest) native.GoMeasureResponse {
		scale := e.activeScale
		logical := MeasureRequest{
			KnownWidth:         req.KnownWidth / scale,
			KnownHeight:        req.KnownHeight / scale,
			KnownWidthPresent:  req.KnownWidthPresent,
			KnownHeightPresent: req.KnownHeightPresent,
			AvailWidth:         availToLogical(req.AvailWidth, scale),
			AvailHeight:        availToLogical(req.AvailHeight, scale),
		}
		size := measure(logical)
		return native.GoMeasureResponse{
			Width:  snapMeasuredSize(size.Width, scale),
			Height: snapMeasuredSize(size.Height, scale),
		}
	}
}

// availToLogical converts one native device-pixel available-space axis
// back to logical pixels (Definite(pixels) / scale).
func availToLogical(a native.AvailSpace, scale float32) AvailableSpace {
	switch a.Mode {
	case native.AvailModeDefinite:
		return AvailableSpace{Kind: AvailDefinite, Definite: a.Definite / scale}
	case native.AvailModeMinContent:
		return MinContentAvailableSpace()
	default:
		return MaxContentAvailableSpace()
	}
}

// availToNative converts one logical available-space axis to the native
// device-pixel form. Definite space is multiplied by the scale factor
// WITHOUT rounding, exactly like the pinned compute_layout transform
// (`AvailableSpace::Definite(Pixels(pixels.0 * scale_factor))`); the
// min/max-content modes pass through as tags.
func availToNative(a AvailableSpace, scale float32) native.LayoutAvail {
	switch a.Kind {
	case AvailDefinite:
		return native.DefiniteAvail(a.Definite * scale)
	case AvailMinContent:
		return native.MinContentAvail()
	default:
		return native.MaxContentAvail()
	}
}

// ComputeLayout lays the subtree rooted at root out under the given
// available space (logical pixels, per axis), mirroring the pinned
// compute_layout:
//
//   - recomputing an already-computed root invalidates the cached
//     absolute bounds and origins of its whole subtree first;
//   - definite available space becomes device pixels by multiplication
//     with the scale factor;
//   - measured leaves are queried synchronously through their measure
//     functions (the engine is busy for the duration, so same-engine
//     re-entry fails with the engine-busy error).
//
// After a successful compute, query bounds with LayoutBounds or
// ParentRelativeLayoutBounds.
func (e *LayoutEngine) ComputeLayout(ctx *LayoutContext, root LayoutID, available AvailableSize) error {
	if ctx == nil {
		return fmt.Errorf("gpui: ComputeLayout requires a layout context")
	}
	if err := e.checkID(root, "ComputeLayout"); err != nil {
		return err
	}
	// The same-engine re-entry guard: this engine is busy from here until
	// the compute (including every measure callback inside it) returns.
	// Nested computes on a different engine are unaffected.
	if !e.busy.CompareAndSwap(false, true) {
		return fmt.Errorf("gpui: ComputeLayout: %w (nested same-engine compute)", native.ErrLayoutEngineBusy)
	}
	defer e.busy.Store(false)
	if e.computedRoots[root] {
		// Already computed once: invalidate the cached absolute-position
		// state of the subtree before recomputing (pinned
		// compute_layout's computed_layouts bookkeeping).
		if err := e.invalidateSubtree(root); err != nil {
			return err
		}
	} else {
		e.computedRoots[root] = true
	}
	e.activeScale = ctx.ScaleFactor()
	avail := native.LayoutAvailSize{
		Width:  availToNative(available.Width, ctx.ScaleFactor()),
		Height: availToNative(available.Height, ctx.ScaleFactor()),
	}
	e.computeSeq++
	if _, err := e.native.Compute(root.node, avail, e.computeSeq); err != nil {
		return fmt.Errorf("gpui: ComputeLayout: %w", err)
	}
	return nil
}

// invalidateSubtree clears the cached bounds and origins of id's subtree,
// walking the native tree preorder.
func (e *LayoutEngine) invalidateSubtree(root LayoutID) error {
	stack := []LayoutID{root}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		delete(e.absoluteLayoutBounds, id)
		delete(e.absoluteOuterOrigins, id)
		count, err := e.native.NodeChildCount(id.node)
		if err != nil {
			return fmt.Errorf("gpui: invalidating layout caches: %w", err)
		}
		for i := uint32(0); i < count; i++ {
			child, err := e.native.NodeChild(id.node, i)
			if err != nil {
				return fmt.Errorf("gpui: invalidating layout caches: %w", err)
			}
			stack = append(stack, LayoutID{engine: e, node: child, generation: e.generation})
		}
	}
	return nil
}

// LayoutBounds returns the node's computed bounds in logical pixels,
// relative to the window (the ordinary outer bounds), applying the full
// pinned snapping pipeline:
//
//   - the native layout's unrounded device-pixel location (relative to
//     the parent) and size are read;
//   - absolute device origins are accumulated unrounded down the tree
//     (parents are resolved first, caching their origins);
//   - the near (min) and far (max) edges are rounded INDEPENDENTLY with
//     the midpoint-toward-zero helper, and the size is the difference of
//     the rounded edges;
//   - the result is divided by the scale factor back to logical pixels;
//   - the separately snapped element offset of the context is added to
//     the origin (window.rs layout_bounds adds
//     pixel_snap_point(element_offset())).
//
// Results are cached per node until the subtree is recomputed or the
// engine reset.
func (e *LayoutEngine) LayoutBounds(ctx *LayoutContext, id LayoutID) (Bounds, error) {
	if err := e.checkBusy("LayoutBounds"); err != nil {
		return Bounds{}, err
	}
	if ctx == nil {
		return Bounds{}, fmt.Errorf("gpui: LayoutBounds requires a layout context")
	}
	if err := e.checkID(id, "LayoutBounds"); err != nil {
		return Bounds{}, err
	}
	bounds, err := e.layoutBounds(id, ctx.ScaleFactor())
	if err != nil {
		return Bounds{}, err
	}
	// window.rs layout_bounds: bounds.origin += pixel_snap_point(element_offset()).
	offset := ctx.ElementOffset()
	bounds.Origin.X += pixelSnap(offset.X, ctx.ScaleFactor())
	bounds.Origin.Y += pixelSnap(offset.Y, ctx.ScaleFactor())
	return bounds, nil
}

// layoutBounds is the pinned TaffyLayoutEngine::layout_bounds: cached
// bounds short-circuit; otherwise the node's unrounded parent-relative
// device location is accumulated onto the parent's cached absolute outer
// origin, the near and far edges are rounded independently, and the
// snapped bounds are divided by the scale factor.
func (e *LayoutEngine) layoutBounds(id LayoutID, scaleFactor float32) (Bounds, error) {
	if cached, ok := e.absoluteLayoutBounds[id]; ok {
		return cached, nil
	}
	record, err := e.native.NodeLayout(id.node)
	if err != nil {
		return Bounds{}, fmt.Errorf("gpui: LayoutBounds: %w", err)
	}
	parent, err := e.native.NodeParent(id.node)
	if err != nil {
		return Bounds{}, fmt.Errorf("gpui: LayoutBounds: %w", err)
	}
	var origin devicePoint
	if parent != 0 {
		parentID := LayoutID{engine: e, node: parent, generation: id.generation}
		if _, err := e.layoutBounds(parentID, scaleFactor); err != nil {
			return Bounds{}, err
		}
		parentOrigin, ok := e.absoluteOuterOrigins[parentID]
		if !ok {
			return Bounds{}, fmt.Errorf("gpui: LayoutBounds: parent absolute outer origin missing after resolve")
		}
		origin = devicePoint{
			X: parentOrigin.X + record.LocationX,
			Y: parentOrigin.Y + record.LocationY,
		}
	} else {
		origin = devicePoint{X: record.LocationX, Y: record.LocationY}
	}
	e.absoluteOuterOrigins[id] = origin

	// Round the near and far edges independently; the size is the
	// difference of the rounded edges; divide by the scale factor.
	far := devicePoint{X: origin.X + record.SizeW, Y: origin.Y + record.SizeH}
	nearX := roundHalfTowardZero(origin.X)
	nearY := roundHalfTowardZero(origin.Y)
	farX := roundHalfTowardZero(far.X)
	farY := roundHalfTowardZero(far.Y)
	bounds := Bounds{
		Origin: Point{X: nearX / scaleFactor, Y: nearY / scaleFactor},
		Size: Size{
			Width:  (farX - nearX) / scaleFactor,
			Height: (farY - nearY) / scaleFactor,
		},
	}
	e.absoluteLayoutBounds[id] = bounds
	return bounds, nil
}

// ParentRelativeLayoutBounds returns bounds whose origin and size are
// snapped in the coordinate space of the PARENT (the pinned
// parent_relative_layout_bounds): the node's local device origin and
// size are each rounded with the midpoint-toward-zero helper
// independently of the parent's position, then divided by the scale
// factor, and the origin is placed inside the parent's (ordinary) layout
// bounds plus the separately snapped element offset.
//
// Text uses this placement so its offset within a containing element
// stays stable when the element moves across the device-pixel grid;
// other boxes keep the absolute edge snapping of LayoutBounds. A node
// without a parent falls back to its ordinary layout bounds (the
// reference also falls back for inline fragments, which this slice does
// not have).
func (e *LayoutEngine) ParentRelativeLayoutBounds(ctx *LayoutContext, id LayoutID) (Bounds, error) {
	if err := e.checkBusy("ParentRelativeLayoutBounds"); err != nil {
		return Bounds{}, err
	}
	if ctx == nil {
		return Bounds{}, fmt.Errorf("gpui: ParentRelativeLayoutBounds requires a layout context")
	}
	if err := e.checkID(id, "ParentRelativeLayoutBounds"); err != nil {
		return Bounds{}, err
	}
	scaleFactor := ctx.ScaleFactor()
	// Nodes with placed inline fragment geometry use the ordinary
	// absolute-edge path (the pinned parent_relative_layout_bounds
	// returns layout_bounds for fragment-bearing nodes).
	if _, placed := e.inlineFragments[id]; placed {
		return e.LayoutBounds(ctx, id)
	}
	parent, err := e.native.NodeParent(id.node)
	if err != nil {
		return Bounds{}, fmt.Errorf("gpui: ParentRelativeLayoutBounds: %w", err)
	}
	if parent == 0 {
		// Parentless nodes use the ordinary path.
		return e.LayoutBounds(ctx, id)
	}
	parentID := LayoutID{engine: e, node: parent, generation: id.generation}
	parentBounds, err := e.LayoutBounds(ctx, parentID)
	if err != nil {
		return Bounds{}, err
	}
	record, err := e.native.NodeLayout(id.node)
	if err != nil {
		return Bounds{}, fmt.Errorf("gpui: ParentRelativeLayoutBounds: %w", err)
	}
	// Snap the local origin and size independently, inside the parent's
	// coordinate space.
	localOriginX := roundHalfTowardZero(record.LocationX)
	localOriginY := roundHalfTowardZero(record.LocationY)
	localWidth := roundHalfTowardZero(record.SizeW)
	localHeight := roundHalfTowardZero(record.SizeH)
	bounds := Bounds{
		Origin: Point{
			X: parentBounds.Origin.X + localOriginX/scaleFactor,
			Y: parentBounds.Origin.Y + localOriginY/scaleFactor,
		},
		Size: Size{
			Width:  localWidth / scaleFactor,
			Height: localHeight / scaleFactor,
		},
	}
	// window.rs parent_relative_layout_bounds adds the snapped element
	// offset as well.
	offset := ctx.ElementOffset()
	bounds.Origin.X += pixelSnap(offset.X, scaleFactor)
	bounds.Origin.Y += pixelSnap(offset.Y, scaleFactor)
	return bounds, nil
}

// Reset clears the engine's tree and starts a new generation: every
// outstanding LayoutID becomes unusable (ErrLayoutIDStale), the cached
// bounds are dropped, the failed flag clears, and measure tokens of the
// old generation are purged. The engine handle itself stays valid.
func (e *LayoutEngine) Reset() error {
	if err := e.checkBusy("Reset"); err != nil {
		return err
	}
	if err := e.native.Reset(); err != nil {
		return fmt.Errorf("gpui: Reset: %w", err)
	}
	e.generation++
	e.absoluteLayoutBounds = make(map[LayoutID]Bounds)
	e.absoluteOuterOrigins = make(map[LayoutID]devicePoint)
	e.computedRoots = make(map[LayoutID]bool)
	e.resetInlineState()
	return nil
}

// Dispose releases the engine's native state. In-flight native calls hold
// their own references, so actual destruction is deferred until they
// return; after disposal every LayoutID of the engine is stale.
func (e *LayoutEngine) Dispose() error {
	if err := e.checkBusy("Dispose"); err != nil {
		return err
	}
	if err := e.native.Dispose(); err != nil {
		return fmt.Errorf("gpui: Dispose: %w", err)
	}
	e.generation++
	e.absoluteLayoutBounds = make(map[LayoutID]Bounds)
	e.absoluteOuterOrigins = make(map[LayoutID]devicePoint)
	e.computedRoots = make(map[LayoutID]bool)
	e.resetInlineState()
	return nil
}

// resetInlineState drops the GPUI-owned inline orchestration records
// (the reference TaffyLayoutEngine::clear clears the vertical
// alignments, inline content, fragments and display records with the
// tree; frame-scoped in the port since the window resets the engine
// per frame).
func (e *LayoutEngine) resetInlineState() {
	e.inlineContent = make(map[LayoutID]inlineContentRecord)
	e.inlineFragments = make(map[LayoutID][]Bounds)
	e.verticalAligns = make(map[LayoutID]VerticalAlign)
	e.displayPositions = make(map[LayoutID]displayPositionRecord)
}

// ---------------------------------------------------------------------------
// GPUI-owned inline orchestration state (taffy.rs inline_content /
// inline_fragments / place_inline, window.rs publish_inline_content /
// inline_fragments / place_inline)
// ---------------------------------------------------------------------------

// PublishInlineContent records the inline content of a requested node
// (the reference window.publish_inline_content → engine.inline_content
// .insert). The record lives until the engine resets (frame-scoped).
func (e *LayoutEngine) PublishInlineContent(id LayoutID, record inlineContentRecord) error {
	if err := e.checkBusy("PublishInlineContent"); err != nil {
		return err
	}
	if err := e.checkID(id, "PublishInlineContent"); err != nil {
		return err
	}
	e.inlineContent[id] = record
	return nil
}

// InlineContent returns the node's published inline content, when any.
func (e *LayoutEngine) InlineContent(id LayoutID) (inlineContentRecord, bool) {
	if err := e.checkID(id, "InlineContent"); err != nil {
		return nil, false
	}
	record, ok := e.inlineContent[id]
	return record, ok
}

// InlineFragments returns the node's placed fragment geometry in
// window-local logical pixels — the raw stored regions shifted by the
// pixel-snapped element offset (the reference window.inline_fragments:
// each region's origin gains pixel_snap_point(element_offset())).
func (e *LayoutEngine) InlineFragments(id LayoutID, scaleFactor float32, offset Point) ([]Bounds, bool) {
	if err := e.checkID(id, "InlineFragments"); err != nil {
		return nil, false
	}
	fragments, ok := e.inlineFragments[id]
	if !ok {
		return nil, false
	}
	snapped := pixelSnapPoint(offset, scaleFactor)
	out := make([]Bounds, len(fragments))
	for i, region := range fragments {
		out[i] = Bounds{Origin: Point{X: region.Origin.X + snapped.X, Y: region.Origin.Y + snapped.Y}, Size: region.Size}
	}
	return out, true
}

// DisplayPositionOf returns the node's recorded display and position
// (the reference window.layout_display_and_position).
func (e *LayoutEngine) DisplayPositionOf(id LayoutID) (Display, Position, bool) {
	if err := e.checkID(id, "DisplayPositionOf"); err != nil {
		return DisplayFlex, PositionRelative, false
	}
	record, ok := e.displayPositions[id]
	if !ok {
		return DisplayFlex, PositionRelative, false
	}
	return record.display, record.position, true
}

// VerticalAlignOf returns the node's recorded vertical alignment (the
// reference window.layout_vertical_align; Baseline when unrecorded).
func (e *LayoutEngine) VerticalAlignOf(id LayoutID) (VerticalAlign, bool) {
	if err := e.checkID(id, "VerticalAlignOf"); err != nil {
		return VerticalAlignBaseline, false
	}
	align, ok := e.verticalAligns[id]
	if !ok {
		return VerticalAlignBaseline, true
	}
	return align, true
}

// PlaceInline places a detached inline box (or a text span's union
// bounds with its fragment regions), invalidating the cached absolute
// bounds and origins of its descendants first (the pinned
// TaffyLayoutEngine::place_inline): the given logical-pixel bounds are
// cached as the node's absolute layout bounds and their origin becomes
// the unrounded absolute outer origin in device pixels, so later
// LayoutBounds/ParentRelativeLayoutBounds queries of the placed node
// read the placement instead of a Taffy-computed position. The
// optional fragments are stored as the node's inline fragment
// geometry (the reference stores them unshifted; the query shifts them
// by the snapped element offset).
func (e *LayoutEngine) PlaceInline(id LayoutID, bounds Bounds, fragments []Bounds, scaleFactor float32) error {
	if err := e.checkBusy("PlaceInline"); err != nil {
		return err
	}
	if err := e.checkID(id, "PlaceInline"); err != nil {
		return err
	}
	// Invalidate the cached positions of the placed subtree, walking the
	// native tree preorder (the pinned place_inline stack walk).
	stack := []LayoutID{id}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		delete(e.absoluteLayoutBounds, node)
		delete(e.absoluteOuterOrigins, node)
		count, err := e.native.NodeChildCount(node.node)
		if err != nil {
			return fmt.Errorf("gpui: PlaceInline: invalidating layout caches: %w", err)
		}
		for i := uint32(0); i < count; i++ {
			child, err := e.native.NodeChild(node.node, i)
			if err != nil {
				return fmt.Errorf("gpui: PlaceInline: invalidating layout caches: %w", err)
			}
			stack = append(stack, LayoutID{engine: e, node: child, generation: e.generation})
		}
	}
	e.absoluteLayoutBounds[id] = bounds
	e.absoluteOuterOrigins[id] = devicePoint{X: bounds.Origin.X * scaleFactor, Y: bounds.Origin.Y * scaleFactor}
	if fragments != nil {
		stored := make([]Bounds, len(fragments))
		copy(stored, fragments)
		e.inlineFragments[id] = stored
	} else {
		delete(e.inlineFragments, id)
	}
	return nil
}
