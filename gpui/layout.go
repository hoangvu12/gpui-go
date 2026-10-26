package gpui

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/hoangvu12/gpui-go/internal/native"
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

// clampLineWidth is Chromium's computed border-width rule
// (StyleBuilderConverter::ClampLineWidth, core/css/resolver/
// style_builder_converter.cc:1971-1987): the computed width is an
// integer of CSS px — anything strictly between 0 and 1 becomes 1
// (a sub-pixel hairline keeps one pixel), anything else floors
// (negatives floor away, then clamp up as hairlines). Verified
// against Chrome 155: a 1px border at device scale 1.75 insets the
// content box by exactly 1 CSS px and paints as an antialiased
// 1.75-device-px stroke; a 0.5px hairline at 100% insets 1 CSS px and
// paints 1 device px (browser-core ticket 07).
func clampLineWidth(logical float32) float32 {
	if logical > 0.0 && logical < 1.0 {
		return 1.0
	}
	if logical < 0.0 {
		// A negative width is a floor(-v)-magnitude hairline: floor to
		// zero and keep it at zero (Chromium's ClampLineWidth floors a
		// negative to 0; the pin's Edges<AbsoluteLength> has no negative
		// widths to forward in practice).
		return 0.0
	}
	return floor32(logical)
}

// clampLineWidthToDevice converts one border width to the layout
// record's device value: Chromium's CSS-px floor first (the computed
// border width is an integer of CSS px), then the scale conversion
// with NO further rounding — the layout inset is the floored CSS
// width exactly, like Chrome's LayoutUnit layout at any device scale
// (browser-core ticket 07).
func clampLineWidthToDevice(logical, scaleFactor float32) float32 {
	return clampLineWidth(logical) * scaleFactor
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
// semantics outside Taffy). Browser-core 36 adds the same fold for the
// atomic inline boxes (InlineBlock -> Block, InlineGrid -> Grid: CSS
// lays their CONTENTS out with block flow / grid, the parent's inline
// layout treats them as one box — the InlineFlex pattern), FlowRoot
// onto the rev-15 FlowRoot tag, and Contents onto Block as the
// harmless fallback: a contents element normally requests no node at
// all (div.go splices its children into the parent), so this arm only
// serves a contents Style that reaches a node request through another
// element's seam — contents has no box semantics to preserve there.
func displayToRecord(d Display) uint32 {
	switch d {
	case DisplayBlock, DisplayInline, DisplayInlineBlock, DisplayContents:
		return native.DisplayBlock
	case DisplayFlex, DisplayInlineFlex:
		return native.DisplayFlex
	case DisplayGrid, DisplayInlineGrid:
		return native.DisplayGrid
	case DisplayFlowRoot:
		return native.DisplayFlowRoot
	case DisplayNone:
		return native.DisplayNone
	default:
		return native.DisplayFlex
	}
}

// overflowToRecord maps the gpui Overflow onto the ABI overflow value.
// Browser-core 36's OverflowAuto folds onto Taffy's Hidden: both zero
// the automatic minimum size and contain the scroll region, and
// neither reserves a scrollbar gutter. The auto scrollbar's
// reservation is decided at request time by the div (scrollbar.go
// autoOverflowRecordStyle): once the scrollbar shows (or the gutter is
// stable), the axis maps onto Scroll WITH a scrollbar width, which is
// Taffy's reservation exactly (compute/flexbox.rs:459-470 adds the
// scrollbar width to the content-box inset on the inline-end side,
// like a CSS classic scrollbar).
func overflowToRecord(o Overflow) uint32 {
	switch o {
	case OverflowVisible:
		return native.OverflowVisible
	case OverflowClip:
		return native.OverflowClip
	case OverflowHidden, OverflowAuto:
		return native.OverflowHidden
	case OverflowScroll:
		return native.OverflowScroll
	default:
		return native.OverflowVisible
	}
}

// positionToRecord maps the gpui Position onto the ABI position value
// (browser-core ticket 33, spec D3). Taffy has only Relative and
// Absolute, and the native position_from (reference/native/src/layout.rs
// position_from) rejects any other value, so the new PositionStatic maps
// onto Relative — CSS static lays out in flow exactly like relative —
// while the element's containing-block semantics (static is not a
// container for absolute children) are decided Go-side by the scope
// walk in containingblock.go, not by the Taffy record. Ticket 34's
// PositionSticky maps the same way: CSS sticky is an in-flow positioned
// value, so the layout is the flow layout, and the insets (which Taffy
// would apply as RELATIVE offsets) are zeroed below — they are sticky
// constraints applied at prepaint (sticky.go), never layout offsets.
func positionToRecord(p Position) uint32 {
	switch p {
	case PositionAbsolute:
		return native.PositionAbsolute
	// browser-core 35 (spec L10): fixed maps onto Taffy's Absolute —
	// out of flow with the insets kept (they are the window
	// coordinates); the containing block is decided Go-side
	// (gpui/containingblock.go defers the node into the root scope,
	// the fixed split), never by the engine.
	case PositionFixed:
		return native.PositionAbsolute
	case PositionStatic, PositionRelative, PositionSticky:
		return native.PositionRelative
	default:
		return native.PositionRelative
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
	rec.Position = positionToRecord(s.Position)
	rec.OverflowX = overflowToRecord(s.OverflowX)
	rec.OverflowY = overflowToRecord(s.OverflowY)
	// CSS static ignores the insets (css-position-3: they only apply to
	// positioned boxes), and the record's Relative mapping would apply
	// them as Taffy relative offsets — zero them for static nodes
	// (browser-core 33; deliberate CSS rule, no pin behaviour existed for
	// the value). Sticky zeroes them for the layout's sake too
	// (browser-core 34): its insets are scroll constraints resolved at
	// prepaint (sticky.go), not relative offsets — the box's flow
	// position must be the unshifted one the constraints clamp from.
	if s.Position == PositionStatic || s.Position == PositionSticky {
		rec.Inset = native.LayoutEdges{}
	} else {
		rec.Inset = lengthEdgesToRecord(s.Inset, remSize, scaleFactor)
	}
	// Scrollbar width is an AbsoluteLength forwarded as f32 device pixels
	// (pinned ToTaffy<f32> for AbsoluteLength).
	rec.ScrollbarWidth = native.DefiniteLen(
		roundToDevicePixel(s.ScrollbarWidth.ToPixels(remSize), scaleFactor))

	rec.Size = lengthSizeToRecord(s.Size, remSize, scaleFactor)
	rec.MinSize = lengthSizeToRecord(s.MinSize, remSize, scaleFactor)
	rec.MaxSize = lengthSizeToRecord(s.MaxSize, remSize, scaleFactor)
	if s.AspectRatio != nil {
		rec.AspectRatioPresent = 1
		rec.AspectRatioBits = math.Float32bits(*s.AspectRatio)
	}

	rec.Margin = lengthEdgesToRecord(s.Margin, remSize, scaleFactor)
	rec.Padding = paddingToRecord(s.Padding, s.Size, remSize, scaleFactor)
	// Border widths follow Chromium's computed-style rule (browser-core
	// ticket 07): the computed width is an INTEGER of CSS px — floor,
	// with any width strictly between 0 and 1 clamping up to 1
	// (StyleBuilderConverter::ConvertBorderWidth -> ClampLineWidth,
	// core/css/resolver/style_builder_converter.cc:1971-1987; the layout
	// consumes the integers as-is, core/layout/length_utils.cc:1478-1485
	// ComputeBordersInternal). The device value is the floored CSS
	// width × the scale factor, unrounded — a 1px border at 175% lays
	// out at exactly 1 CSS px (1.75 device), which the measured record
	// confirms (Chrome 155's getBoundingClientRect insets the content
	// by 1, and the painted strip is an antialiased 1.75 device px, not
	// a floored 1). The pin's round_stroke_to_device_pixel rule would
	// have made that border 2 device px of layout inset, which diverges
	// from Chrome at every fractional scale; it survives for the PAINT
	// (paint.go snapBorderWidths), where the hard-edged strip needs a
	// whole device thickness and rounding is the closest approximation
	// of Chrome's antialiased stroke.
	rec.BorderTop = math.Float32bits(clampLineWidthToDevice(s.BorderWidths.Top.ToPixels(remSize), scaleFactor))
	rec.BorderRight = math.Float32bits(clampLineWidthToDevice(s.BorderWidths.Right.ToPixels(remSize), scaleFactor))
	rec.BorderBottom = math.Float32bits(clampLineWidthToDevice(s.BorderWidths.Bottom.ToPixels(remSize), scaleFactor))
	rec.BorderLeft = math.Float32bits(clampLineWidthToDevice(s.BorderWidths.Left.ToPixels(remSize), scaleFactor))

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

	if s.GridColsList == nil {
		gridTemplateToRecord(s.GridCols,
			&rec.GridTemplateColumnsPresent, &rec.GridTemplateColumnsRepeat, &rec.GridTemplateColumnsMinSize)
	} else {
		// The full track list takes the axis over (the native
		// layout_node_set_grid_tracks call applies it after the record;
		// the record's repeat form stays off so a plain SetStyle never
		// resurrects the pinned template over the list).
		gridTemplateToRecord(nil,
			&rec.GridTemplateColumnsPresent, &rec.GridTemplateColumnsRepeat, &rec.GridTemplateColumnsMinSize)
	}
	if s.GridRowsList == nil {
		gridTemplateToRecord(s.GridRows,
			&rec.GridTemplateRowsPresent, &rec.GridTemplateRowsRepeat, &rec.GridTemplateRowsMinSize)
	} else {
		gridTemplateToRecord(nil,
			&rec.GridTemplateRowsPresent, &rec.GridTemplateRowsRepeat, &rec.GridTemplateRowsMinSize)
	}
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
	// nodeMargins records each node's authored margin edges (browser-core
	// ticket 07: the scroll range's trailing-margin extension — Taffy's
	// content size stops at the child border boxes, where Chromium's
	// inflow bounds include the end margins). Filled only while the
	// window requested it (anchor paths below), frame-scoped like the
	// display records.
	nodeMargins map[LayoutID]LengthEdges
	// nodePaths records each node's element id path key (browser-core
	// tickets 07/46: the scroll-anchoring walk identifies the anchor node
	// across frames by its element path). Filled only while the window
	// requested anchoring bookkeeping.
	nodePaths map[LayoutID]string
	// anchorBookkeepingRequested reports whether any scroller of this
	// engine's window saved a scroll anchor or a trailing-margin
	// extension, so the next frame's request walk records node margins
	// and paths (the maps above). It is toggled by the scroll slice
	// (scroll_element.go), not by the engine itself.
	anchorBookkeepingRequested bool
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
		nodeMargins:          make(map[LayoutID]LengthEdges),
		nodePaths:            make(map[LayoutID]string),
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
	// The CSS grid surface the style record cannot carry (browser-core
	// 42: full track lists and the named area template/placement) rides
	// the node's stored style through the dedicated entries after the
	// record.
	if err := applyGridStyleExtras(e, node, &style, ctx); err != nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestLayout: %w", err)
	}
	id := LayoutID{engine: e, node: node, generation: e.generation}
	e.recordInlineStyleState(id, &style)
	return id, nil
}

// applyGridStyleExtras pushes the grid styles the ABI record cannot
// carry (browser-core 42): the full track lists (grid-template-columns/
// rows: `240px 1fr`) through SetGridTracks, and the named area template
// plus the item's named placement through the rev-16 area entries. The
// native calls REPLACE the stored style's axes and placements, so every
// full style write must re-apply them (StretchAutoSizeToFill re-styles
// an existing node; a fresh node applies them right after creation).
func applyGridStyleExtras(e *LayoutEngine, node native.NodeHandle, style *Style, ctx *LayoutContext) error {
	if len(style.GridColsList) > 0 {
		if err := e.native.SetGridTracks(node, native.LayoutGridAxisColumns, trackListToRecords(style.GridColsList, ctx.ScaleFactor())); err != nil {
			return fmt.Errorf("grid template columns: %w", err)
		}
	}
	if len(style.GridRowsList) > 0 {
		if err := e.native.SetGridTracks(node, native.LayoutGridAxisRows, trackListToRecords(style.GridRowsList, ctx.ScaleFactor())); err != nil {
			return fmt.Errorf("grid template rows: %w", err)
		}
	}
	if style.GridAreas != nil {
		records, names := gridAreaRecords(style.GridAreas)
		columnCount := 0
		if len(style.GridAreas.Rows) > 0 {
			columnCount = len(style.GridAreas.Rows[0])
		}
		if err := e.native.SetGridTemplateAreas(node, uint32(len(style.GridAreas.Rows)), uint32(columnCount), records, names); err != nil {
			return fmt.Errorf("grid template areas: %w", err)
		}
	}
	if style.GridAreaName != "" {
		if err := e.native.SetGridArea(node, style.GridAreaName); err != nil {
			return fmt.Errorf("grid area %q: %w", style.GridAreaName, err)
		}
	}
	return nil
}

// trackListToRecords converts one track list to the ABI minmax records,
// converting definite logical lengths to device pixels with the pinned
// definite-length rule (round to the nearest device pixel, ties toward
// zero).
func trackListToRecords(tracks []GridTrack, scaleFactor float32) []native.LayoutTrackRecord {
	records := make([]native.LayoutTrackRecord, len(tracks))
	for i, track := range tracks {
		records[i] = native.LayoutTrackRecord{
			MinTag:  trackSizingTag(track.Min),
			MinBits: trackSizingBits(track.Min, scaleFactor),
			MaxTag:  trackSizingTag(track.Max),
			MaxBits: trackSizingBits(track.Max, scaleFactor),
		}
	}
	return records
}

// trackSizingTag maps one minmax side onto the ABI track tag.
func trackSizingTag(sizing TrackSizing) uint32 {
	switch sizing.Kind {
	case TrackLength:
		return native.TrackMinLength
	case TrackMinContent:
		return native.TrackMinContent
	case TrackMaxContent:
		return native.TrackMaxContent
	case TrackAuto:
		return native.TrackAuto
	default:
		return native.TrackFr
	}
}

// trackSizingBits maps one minmax side's value onto the ABI bits: the
// device-converted length for a length, the fr factor verbatim (the
// factor is unitless), zero otherwise.
func trackSizingBits(sizing TrackSizing, scaleFactor float32) uint32 {
	switch sizing.Kind {
	case TrackLength:
		return math.Float32bits(roundToDevicePixel(sizing.Length, scaleFactor))
	case TrackFr:
		return math.Float32bits(sizing.Length)
	default:
		return 0
	}
}

// gridAreaRecords flattens one area template into the ABI records plus
// the shared names buffer (the records slice the buffer by offset and
// length).
func gridAreaRecords(template *GridAreaTemplate) ([]native.LayoutGridAreaRecord, []byte) {
	names := make([]byte, 0, 16*len(template.rects))
	records := make([]native.LayoutGridAreaRecord, 0, len(template.rects))
	for _, name := range template.AreaNames() {
		rect := template.rects[name]
		records = append(records, native.LayoutGridAreaRecord{
			RowStart:    uint32(rect.rowStart),
			RowEnd:      uint32(rect.rowEnd),
			ColumnStart: uint32(rect.columnStart),
			ColumnEnd:   uint32(rect.columnEnd),
			NameOffset:  uint32(len(names)),
			NameLen:     uint32(len(name)),
		})
		names = append(names, name...)
	}
	return records, names
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
	// browser-core 07: the authored margins ride along (the scroll range's
	// trailing-margin extension reads them at prepaint — Taffy's content
	// size stops at the child border boxes).
	e.nodeMargins[id] = style.Margin
}

// RequestAnchorBookkeeping turns on the node-path records of the
// request walk (browser-core 46: the scroll-anchoring walk identifies
// its anchor node across frames by the element id path, and the paths
// cost one small string per node per frame — so only windows whose
// scrollers actually saved a scroll anchor pay for them). The margins
// are always recorded (a map insert with a struct value, like the
// display records).
func (e *LayoutEngine) RequestAnchorBookkeeping() {
	e.anchorBookkeepingRequested = true
}

// anchorPathOf returns the node's recorded element id path key ("
// when the walk did not record it).
func (e *LayoutEngine) anchorPathOf(id LayoutID) string {
	return e.nodePaths[id]
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
// to the result before it crosses back. The baseline crosses as the
// unrounded device value (logical × scale, the definite-available-space
// rule): CSS baselines are not pixel-snapped, and the pinned snap would
// put a 17.3px baseline at a different offset than the paint layer's
// line placement.
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
		result := measure(logical)
		response := native.GoMeasureResponse{
			Width:  snapMeasuredSize(result.Width, scale),
			Height: snapMeasuredSize(result.Height, scale),
		}
		if result.FirstBaseline != nil {
			// A negative offset would fail the native validation; clamp at
			// zero (the box top) rather than failing the compute.
			baseline := *result.FirstBaseline * scale
			if baseline < 0 {
				baseline = 0
			}
			response.FirstBaseline = &baseline
		}
		return response
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
	// The parent's RAW bounds, without the element offset: the pinned
	// taffy.rs parent_relative_layout_bounds reads layout_bounds(parent)
	// (the engine's ordinary, offset-free computation) and window.rs adds
	// pixel_snap_point(element_offset()) exactly once, AFTER the engine
	// call. Computing the parent through the offset-aware LayoutBounds
	// here and adding the offset again below doubled it for every
	// prepaint-under-offset root (the drag preview's separately prepainted
	// root; consumer gap 08).
	parentBounds, err := e.layoutBounds(parentID, ctx.ScaleFactor())
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
	// The node bookkeeping of the scroll slice (browser-core 07/46) is
	// frame-scoped with the tree: the margins always refill during the
	// request walk, the paths while anchoring bookkeeping is on.
	e.nodeMargins = make(map[LayoutID]LengthEdges)
	e.nodePaths = make(map[LayoutID]string)
}

// ---------------------------------------------------------------------------
// The scroll slice's record-derived facts (browser-core ticket 07)
// ---------------------------------------------------------------------------

// ScrollLayoutFacts is one scroll container node's record-derived facts
// (browser-core ticket 07), in logical pixels:
//
//   - ContentSize: Taffy's Layout::content_size — the extent of the
//     descendants' boxes, their visible overflow and the container's
//     END padding (taffy 0.13 compute/block.rs:701-707 and
//     compute/flexbox.rs:2231-2236 add the end padding to the inflow
//     union, exactly the CSS scrollable-overflow rule Chromium's
//     ScrollableOverflowCalculator::Result applies
//     core/layout/scrollable_overflow_calculator.cc:100-118).
//   - MaxOffset: the scroll range per axis — Taffy's
//     Layout::scroll_width/scroll_height (tree/layout.rs:331-348: the
//     content size less the padding box, plus the scrollbar
//     reservation, floored at zero) extended by the in-flow children's
//     trailing margins (Chromium's inflow bounds carry the end margins
//     into the scrollable overflow, core/layout/box_fragment_builder.
//     cc:321-357 with the negative-end-margin clamp; Taffy's content
//     size stops at the border boxes).
//   - Padding: the node's resolved padding, exactly the values Taffy
//     laid the box out with — every edge's percentage resolved against
//     the CONTAINING BLOCK's width (taffy compute/leaf.rs:19-21,
//     compute/flexbox.rs:446-447; CSS box model), so the Go side stops
//     re-resolving vertical padding percentages against the scroller's
//     own height.
type ScrollLayoutFacts struct {
	ContentSize Size
	MaxOffset   Point
	Padding     Edges
}

// ScrollLayoutFacts reads one node's layout record and computes the
// scroll facts (browser-core ticket 07). The node must have been laid
// out (the frame's compute ran). The trailing-margin extension reads
// the in-flow children's recorded margins: display:none children are
// skipped (they have no layout), absolute children are skipped
// (out-of-flow children never move the inflow bounds — the same rule
// the fragment builder applies), and percentage margins resolve
// against the scroller's content width like Taffy's own resolution.
func (e *LayoutEngine) ScrollLayoutFacts(ctx *LayoutContext, id LayoutID) (ScrollLayoutFacts, error) {
	if err := e.checkBusy("ScrollLayoutFacts"); err != nil {
		return ScrollLayoutFacts{}, err
	}
	if ctx == nil {
		return ScrollLayoutFacts{}, fmt.Errorf("gpui: ScrollLayoutFacts requires a layout context")
	}
	if err := e.checkID(id, "ScrollLayoutFacts"); err != nil {
		return ScrollLayoutFacts{}, err
	}
	record, err := e.native.NodeLayout(id.node)
	if err != nil {
		return ScrollLayoutFacts{}, fmt.Errorf("gpui: ScrollLayoutFacts: %w", err)
	}
	scale := ctx.ScaleFactor()
	// Taffy's Layout::scroll_width/scroll_height, in device pixels:
	// the content size less the padding box (size - borders), plus the
	// scrollbar reservation (an axis's classic scrollbar eats port
	// space), floored at zero.
	maxX := maxZero32(record.ContentW + min32(record.ScrollbarW, record.SizeW) - record.SizeW + record.BorderLeft + record.BorderRight)
	maxY := maxZero32(record.ContentH + min32(record.ScrollbarH, record.SizeH) - record.SizeH + record.BorderTop + record.BorderBottom)
	// The trailing-margin extension: how much the in-flow children's
	// end margins push the farthest border box past the border-box union
	// Taffy measured. Percentage margins resolve against the scroller's
	// content width (Taffy's node_inner_size basis).
	contentWidth := (record.SizeW - record.BorderLeft - record.BorderRight - record.PaddingLeft - record.PaddingRight - record.ScrollbarW) / scale
	marginExtX, marginExtY := e.scrollTrailingMargins(id, contentWidth, ctx)
	maxX += marginExtX
	maxY += marginExtY
	return ScrollLayoutFacts{
		ContentSize: Size{Width: record.ContentW / scale, Height: record.ContentH / scale},
		MaxOffset: Point{
			X: roundToTwoDecimals(maxX / scale),
			Y: roundToTwoDecimals(maxY / scale),
		},
		Padding: Edges{
			Top:    record.PaddingTop / scale,
			Right:  record.PaddingRight / scale,
			Bottom: record.PaddingBottom / scale,
			Left:   record.PaddingLeft / scale,
		},
	}, nil
}

// scrollTrailingMargins measures the in-flow children's end margins'
// extension of the scrollable far edges (device pixels): the difference
// between the far border box (plus its clamped end margin) union and
// the plain union Taffy's content size already carries. Negative end
// margins clamp at zero (Chromium's rule: "for the margin furthest away
// we disallow negative margins", box_fragment_builder.cc:341-353).
func (e *LayoutEngine) scrollTrailingMargins(id LayoutID, contentWidth float32, ctx *LayoutContext) (float32, float32) {
	count, err := e.native.NodeChildCount(id.node)
	if err != nil || count == 0 {
		return 0, 0
	}
	farX, farY := float32(-1.0), float32(-1.0)
	marginedX, marginedY := float32(-1.0), float32(-1.0)
	rem := ctx.RemSize()
	scale := ctx.ScaleFactor()
	for i := uint32(0); i < count; i++ {
		childHandle, err := e.native.NodeChild(id.node, i)
		if err != nil {
			continue
		}
		child := LayoutID{engine: e, node: childHandle, generation: id.generation}
		// Out-of-flow children never move the inflow bounds; hidden
		// children have no layout at all. browser-core 35: a fixed child
		// is out of flow exactly like an absolute one — and once the
		// containing-block walk defers it into the root scope it is not a
		// native child of the scroller at all.
		if record, ok := e.displayPositions[child]; ok {
			if record.display == DisplayNone || record.position == PositionAbsolute || record.position == PositionFixed {
				continue
			}
		}
		childLayout, err := e.native.NodeLayout(childHandle)
		if err != nil {
			continue
		}
		// The child's far border-box edge, relative to the scroller's
		// border box (its own overflow rides along through the child's
		// content size, as in Taffy's contribution).
		childFarX := childLayout.LocationX + max32(childLayout.SizeW, childLayout.ContentW)
		childFarY := childLayout.LocationY + max32(childLayout.SizeH, childLayout.ContentH)
		margins, ok := e.nodeMargins[child]
		if !ok {
			farX = max32(farX, childFarX)
			farY = max32(farY, childFarY)
			continue
		}
		endX := maxZero32(resolveRecordedMargin(margins.Right, contentWidth, rem, scale))
		endY := maxZero32(resolveRecordedMargin(margins.Bottom, contentWidth, rem, scale))
		farX = max32(farX, childFarX)
		farY = max32(farY, childFarY)
		marginedX = max32(marginedX, childFarX+endX)
		marginedY = max32(marginedY, childFarY+endY)
	}
	return maxZero32(marginedX - farX), maxZero32(marginedY - farY)
}

// resolveRecordedMargin resolves one recorded margin edge to device
// pixels: definite pixels and rems through the context, fractions
// against the given basis (the scroller's content width), auto as zero
// (the flex algorithm resolved the start margin into the child's
// location; the end auto margin's share is not observable from the
// record — a documented approximation).
func resolveRecordedMargin(l Length, basis, rem, scale float32) float32 {
	if l.Auto {
		return 0
	}
	switch l.Definite.Kind {
	case DefiniteLengthPx:
		return l.Definite.Value * scale
	case DefiniteLengthRem:
		return l.Definite.Value * rem * scale
	case DefiniteLengthFraction:
		return l.Definite.Value * basis * scale
	default:
		return 0
	}
}

// ResolvedPadding returns one node's layout-record padding, resolved to
// logical pixels (browser-core 07): exactly the values Taffy laid the
// box out with — every edge's percentage against the containing
// block's width (compute/leaf.rs:19-21, compute/flexbox.rs:446-447) —
// so style-side arithmetic matches the layout instead of re-resolving
// vertical padding against the element's own height.
func (e *LayoutEngine) ResolvedPadding(ctx *LayoutContext, id LayoutID) (Edges, error) {
	if err := e.checkBusy("ResolvedPadding"); err != nil {
		return Edges{}, err
	}
	if ctx == nil {
		return Edges{}, fmt.Errorf("gpui: ResolvedPadding requires a layout context")
	}
	if err := e.checkID(id, "ResolvedPadding"); err != nil {
		return Edges{}, err
	}
	record, err := e.native.NodeLayout(id.node)
	if err != nil {
		return Edges{}, fmt.Errorf("gpui: ResolvedPadding: %w", err)
	}
	scale := ctx.ScaleFactor()
	return Edges{
		Top:    record.PaddingTop / scale,
		Right:  record.PaddingRight / scale,
		Bottom: record.PaddingBottom / scale,
		Left:   record.PaddingLeft / scale,
	}, nil
}

// recordPaddingOf reads one node's record-resolved padding (logical
// px), panicking on engine failure like the bounds queries (the
// callers treat it as infallible layout data).
func recordPaddingOf(w *Window, id LayoutID) Edges {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	padding, err := ds.engine.ResolvedPadding(&ctx, id)
	if err != nil {
		panic("gpui: record padding: " + err.Error())
	}
	return padding
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
