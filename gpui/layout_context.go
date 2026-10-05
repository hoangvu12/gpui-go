package gpui

// This file carries the logical-space layout scope and the measurement
// contract of the port's Taffy adapter: the LayoutContext (rem scope,
// scale factor, element offset), the per-axis AvailableSpace and the
// user-facing MeasureFunc, mirroring the pinned reference's
// crates/gpui/src/window.rs (Window::rem_size, scale_factor,
// with_rem_size, request_measured_layout) and crates/gpui/src/taffy.rs
// (AvailableSpace, the measure closure that converts device pixels to
// logical pixels around the user's function).
//
// Units: everything on this surface is LOGICAL pixels. The adapter
// converts to and from device pixels (multiply/divide by the scale
// factor) exactly once at the native boundary, applying the pinned
// rounding rules (layout.go).

// DefaultRemSize is the reference window's default rem size in logical
// pixels (Window's `rem_size: px(16.)`, window.rs).
const DefaultRemSize float32 = 16.0

// TestScaleFactor is the reference test window's hardcoded scale factor
// (crates/gpui/src/platform/test/window.rs::TestWindow::scale_factor
// returns 2.0); the port's test-profile layout context uses it.
const TestScaleFactor float32 = 2.0

// LayoutContext is the layout scope of one window: the active rem size,
// the scale factor and the absolute element offset. It is threaded
// through the layout adapter's request/compute/bounds calls the same way
// the reference threads Window state:
//
//   - rem values resolve through RemSize at node REQUEST time (the
//     reference resolves rems from window.rem_size() inside
//     request_layout/request_measured_layout; an override is scoped with
//     Window::with_rem_size around tree construction);
//   - the scale factor converts logical to device pixels at request,
//     compute and bounds time (window.scale_factor());
//   - the element offset is applied (separately snapped) to layout
//     bounds (window.rs layout_bounds adds
//     pixel_snap_point(element_offset())).
type LayoutContext struct {
	remSize       float32
	scaleFactor   float32
	elementOffset Point
}

// NewLayoutContext returns a layout context with the given rem size (in
// logical pixels) and scale factor (device pixels per logical pixel) and
// a zero element offset.
func NewLayoutContext(remSize, scaleFactor float32) *LayoutContext {
	return &LayoutContext{remSize: remSize, scaleFactor: scaleFactor}
}

// NewTestLayoutContext returns the test-profile layout context: the
// reference test window's hardcoded scale factor 2.0 and the default rem
// size 16 logical pixels, with a zero element offset.
func NewTestLayoutContext() *LayoutContext {
	return NewLayoutContext(DefaultRemSize, TestScaleFactor)
}

// RemSize returns the active rem size in logical pixels.
func (c *LayoutContext) RemSize() float32 { return c.remSize }

// ScaleFactor returns the scale factor (device pixels per logical pixel).
func (c *LayoutContext) ScaleFactor() float32 { return c.scaleFactor }

// ElementOffset returns the absolute element offset in logical pixels.
func (c *LayoutContext) ElementOffset() Point { return c.elementOffset }

// WithRemSize returns a copy of the context with the rem override
// applied, mirroring Window::with_rem_size's rem scoping (an override
// active at request time resolves that tree's rem lengths).
func (c *LayoutContext) WithRemSize(remSize float32) *LayoutContext {
	out := *c
	out.remSize = remSize
	return &out
}

// WithElementOffset returns a copy of the context with the given
// absolute element offset in logical pixels (mirrors
// Window::with_absolute_element_offset: the offset is added, snapped, to
// queried bounds).
func (c *LayoutContext) WithElementOffset(offset Point) *LayoutContext {
	out := *c
	out.elementOffset = offset
	return &out
}

// ---------------------------------------------------------------------------
// Available space
// ---------------------------------------------------------------------------

// AvailableSpaceKind is the mode of one axis of available space.
type AvailableSpaceKind uint8

const (
	// AvailDefinite is a definite amount of logical pixels.
	AvailDefinite AvailableSpaceKind = iota
	// AvailMinContent is an indefinite min-content constraint.
	AvailMinContent
	// AvailMaxContent is an indefinite max-content constraint.
	AvailMaxContent
)

// AvailableSpace is the space available to lay an element out in, on one
// axis (reference crates/gpui/src/taffy.rs::AvailableSpace; note the
// reference default is MinContent, not Definite). Values are logical
// pixels.
type AvailableSpace struct {
	// Kind selects definite, min-content or max-content.
	Kind AvailableSpaceKind
	// Definite is the definite amount in logical pixels; valid only when
	// Kind is AvailDefinite.
	Definite float32
}

// DefiniteAvailableSpace returns definite available space in logical
// pixels.
func DefiniteAvailableSpace(px float32) AvailableSpace {
	return AvailableSpace{Kind: AvailDefinite, Definite: px}
}

// MinContentAvailableSpace returns a min-content constraint.
func MinContentAvailableSpace() AvailableSpace {
	return AvailableSpace{Kind: AvailMinContent}
}

// MaxContentAvailableSpace returns a max-content constraint.
func MaxContentAvailableSpace() AvailableSpace {
	return AvailableSpace{Kind: AvailMaxContent}
}

// String renders the mode the conformance measure-query events use
// ("definite", "min-content", "max-content").
func (a AvailableSpace) String() string {
	switch a.Kind {
	case AvailDefinite:
		return "definite"
	case AvailMinContent:
		return "min-content"
	case AvailMaxContent:
		return "max-content"
	default:
		return "unknown"
	}
}

// AvailableSize is the per-axis available space for one layout
// (reference Size<AvailableSpace>): width and height.
type AvailableSize struct {
	Width  AvailableSpace
	Height AvailableSpace
}

// ---------------------------------------------------------------------------
// Measurement
// ---------------------------------------------------------------------------

// MeasureRequest is the logical-space measurement request handed to a
// MeasureFunc: the optional known dimensions and the per-axis available
// space, all in LOGICAL pixels.
//
// The native layout engine measures in device pixels; the adapter
// divides the definite values by the scale factor before calling the
// measure function, exactly like the pinned measure closure in
// crates/gpui/src/taffy.rs::compute_layout (known dimensions and definite
// available space are mapped through `e / scale_factor`).
type MeasureRequest struct {
	// KnownWidth and KnownHeight are the known (fixed) dimensions in
	// logical pixels; the Present flags say whether each is known. The
	// axes are independent: a stretched cross axis can be known while the
	// main axis is not.
	KnownWidth         float32
	KnownHeight        float32
	KnownWidthPresent  bool
	KnownHeightPresent bool
	// AvailWidth and AvailHeight are the available space per axis.
	AvailWidth  AvailableSpace
	AvailHeight AvailableSpace
}

// MeasureFunc measures a node synchronously, from inside ComputeLayout,
// and returns its size in RAW logical pixels.
//
// The returned value is NOT clamped or snapped here: the adapter applies
// the pinned measured-size snapping (each dimension clamped to at least
// zero, multiplied by the scale factor, then rounded up to a whole device
// pixel) before handing the size back to the layout engine, mirroring
// snap_measured_size_to_device_pixels (taffy.rs).
//
// A measure function must not block and must not call back into the same
// layout engine: same-engine re-entry (requesting layout, computing or
// querying bounds of the engine being computed) is rejected with the
// engine-busy error before native entry. Computing a different engine is
// allowed. A panic propagates to the trampoline's recover and fails the
// compute; there is no error return on the happy path.
type MeasureFunc func(req MeasureRequest) Size
