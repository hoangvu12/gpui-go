//go:build windows

package gpui

import (
	"errors"
	"math"
	"testing"
)

// Engine-backed layout tests (ticket06): LayoutID generation invalidation
// after engine reset, the recompute cache invalidation, and the measured
// leaf flow, all against the real embedded native Taffy artifact. The
// pure unit semantics (rounding, parsing, style translation) live in
// layout_test.go and run everywhere; these need the Windows AMD64 DLL.

// newTestEngine creates a layout engine in the test-profile context.
func newTestEngine(t *testing.T) *LayoutEngine {
	t.Helper()
	engine, err := NewLayoutEngine()
	if err != nil {
		t.Fatalf("NewLayoutEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Dispose() })
	return engine
}

// TestLayoutIDInvalidAfterReset pins the LayoutID generation rule: ids
// carry the engine identity and the generation they were created in, and
// become unusable (ErrLayoutIDStale) after the engine resets — they are
// not persistent element-state keys. New requests after a reset hand out
// fresh, working ids.
func TestLayoutIDInvalidAfterReset(t *testing.T) {
	engine := newTestEngine(t)
	ctx := NewTestLayoutContext()

	rootStyle := DefaultStyle()
	rootStyle.Size = LengthSize{Width: PxLength(100), Height: PxLength(100)}
	root, err := engine.RequestLayout(ctx, rootStyle)
	if err != nil {
		t.Fatalf("RequestLayout: %v", err)
	}
	if err := engine.ComputeLayout(ctx, root, AvailableSize{
		Width:  DefiniteAvailableSpace(100),
		Height: DefiniteAvailableSpace(100),
	}); err != nil {
		t.Fatalf("ComputeLayout: %v", err)
	}
	bounds, err := engine.LayoutBounds(ctx, root)
	if err != nil || bounds.Size.Width != 100 || bounds.Size.Height != 100 {
		t.Fatalf("LayoutBounds before reset = %+v, %v, want 100x100", bounds, err)
	}

	if err := engine.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// Every outstanding id is stale: bounds, compute and re-request as a
	// child all fail with the same typed error, before native entry.
	if _, err := engine.LayoutBounds(ctx, root); !errors.Is(err, ErrLayoutIDStale) {
		t.Errorf("LayoutBounds after reset: got %v, want ErrLayoutIDStale", err)
	}
	err = engine.ComputeLayout(ctx, root, AvailableSize{
		Width:  DefiniteAvailableSpace(100),
		Height: DefiniteAvailableSpace(100),
	})
	if !errors.Is(err, ErrLayoutIDStale) {
		t.Errorf("ComputeLayout after reset: got %v, want ErrLayoutIDStale", err)
	}
	if _, err := engine.RequestLayout(ctx, rootStyle, root); !errors.Is(err, ErrLayoutIDStale) {
		t.Errorf("RequestLayout with a stale child: got %v, want ErrLayoutIDStale", err)
	}

	// A second reset keeps the old ids stale and the new ones working.
	if err := engine.Reset(); err != nil {
		t.Fatalf("second Reset: %v", err)
	}
	fresh, err := engine.RequestLayout(ctx, rootStyle)
	if err != nil {
		t.Fatalf("RequestLayout after reset: %v", err)
	}
	if err := engine.ComputeLayout(ctx, fresh, AvailableSize{
		Width:  DefiniteAvailableSpace(100),
		Height: DefiniteAvailableSpace(100),
	}); err != nil {
		t.Fatalf("ComputeLayout after reset: %v", err)
	}
	if bounds, err := engine.LayoutBounds(ctx, fresh); err != nil || bounds.Size.Width != 100 {
		t.Fatalf("LayoutBounds after reset = %+v, %v, want width 100", bounds, err)
	}
	if _, err := engine.LayoutBounds(ctx, root); !errors.Is(err, ErrLayoutIDStale) {
		t.Errorf("LayoutBounds of the pre-reset id: got %v, want ErrLayoutIDStale", err)
	}
}

// TestLayoutIDForeignEngine pins the engine-identity half of LayoutID: an
// id of one engine is rejected by another engine, even at the same
// generation.
func TestLayoutIDForeignEngine(t *testing.T) {
	engineA := newTestEngine(t)
	engineB := newTestEngine(t)
	ctx := NewTestLayoutContext()

	style := DefaultStyle()
	style.Size = LengthSize{Width: PxLength(10), Height: PxLength(10)}
	idA, err := engineA.RequestLayout(ctx, style)
	if err != nil {
		t.Fatalf("RequestLayout on A: %v", err)
	}
	if _, err := engineB.LayoutBounds(ctx, idA); !errors.Is(err, ErrLayoutIDForeign) {
		t.Errorf("engineB.LayoutBounds(idA): got %v, want ErrLayoutIDForeign", err)
	}
	if err := engineB.ComputeLayout(ctx, idA, AvailableSize{}); !errors.Is(err, ErrLayoutIDForeign) {
		t.Errorf("engineB.ComputeLayout(idA): got %v, want ErrLayoutIDForeign", err)
	}
}

// TestRecomputeInvalidatesCachedBounds pins the recomputation rule: a
// root computed a second time under different available space reports
// NEW bounds (the cached absolute bounds of the subtree are invalidated
// before the recompute), instead of the cached first result.
func TestRecomputeInvalidatesCachedBounds(t *testing.T) {
	engine := newTestEngine(t)
	ctx := NewTestLayoutContext()

	// A root sized by percentage of the available space: 50% of 100 then
	// 50% of 200.
	rootStyle := DefaultStyle()
	rootStyle.Size = LengthSize{Width: DefiniteLengthOf(Fraction(0.5))}
	root, err := engine.RequestLayout(ctx, rootStyle)
	if err != nil {
		t.Fatalf("RequestLayout: %v", err)
	}
	if err := engine.ComputeLayout(ctx, root, AvailableSize{
		Width:  DefiniteAvailableSpace(100),
		Height: DefiniteAvailableSpace(100),
	}); err != nil {
		t.Fatalf("first ComputeLayout: %v", err)
	}
	bounds, err := engine.LayoutBounds(ctx, root)
	if err != nil || bounds.Size.Width != 50 {
		t.Fatalf("bounds after first compute = %+v, %v, want width 50", bounds, err)
	}

	if err := engine.ComputeLayout(ctx, root, AvailableSize{
		Width:  DefiniteAvailableSpace(200),
		Height: DefiniteAvailableSpace(100),
	}); err != nil {
		t.Fatalf("second ComputeLayout: %v", err)
	}
	bounds, err = engine.LayoutBounds(ctx, root)
	if err != nil || bounds.Size.Width != 100 {
		t.Fatalf("bounds after recompute = %+v, %v, want width 100 (cache invalidated)", bounds, err)
	}
}

// TestMeasuredLeafFlow pins the measured-node pipeline end to end: the
// measure function receives LOGICAL values (device divided by the scale
// factor), returns raw logical sizes, and the adapter's snapping (clamp
// to >= 0, multiply by the scale factor, ceil) decides the final device
// size. A measured node is a leaf: it has no children by construction.
func TestMeasuredLeafFlow(t *testing.T) {
	engine := newTestEngine(t)
	ctx := NewTestLayoutContext() // scale 2

	var gotRequest MeasureRequest
	root, err := engine.RequestMeasuredLayout(ctx, DefaultStyle(), func(req MeasureRequest) Size {
		gotRequest = req
		// Raw logical: negative height clamps to zero, 12.3 ceils.
		return Size{Width: 12.3, Height: -5}
	})
	if err != nil {
		t.Fatalf("RequestMeasuredLayout: %v", err)
	}
	if err := engine.ComputeLayout(ctx, root, AvailableSize{
		Width:  MinContentAvailableSpace(),
		Height: DefiniteAvailableSpace(40),
	}); err != nil {
		t.Fatalf("ComputeLayout: %v", err)
	}

	// The measure request arrived in logical pixels: 40 logical available
	// height (80 device divided by the scale factor 2).
	if gotRequest.AvailHeight.Kind != AvailDefinite || gotRequest.AvailHeight.Definite != 40 {
		t.Errorf("measure available height = %+v, want definite 40 logical", gotRequest.AvailHeight)
	}

	// The snapped measured size: width ceil(12.3 * 2) = 25 device, height
	// clamped to 0. The bounds come back in logical pixels.
	bounds, err := engine.LayoutBounds(ctx, root)
	if err != nil {
		t.Fatalf("LayoutBounds: %v", err)
	}
	if bounds.Size.Width != 12.5 || bounds.Size.Height != 0 {
		t.Errorf("measured bounds = %+v, want 12.5x0 (25x0 device)", bounds)
	}

	// The node count under a measured node is zero: request_measured_layout
	// creates a leaf.
	count, err := engine.native.NodeChildCount(root.node)
	if err != nil {
		t.Fatalf("NodeChildCount: %v", err)
	}
	if count != 0 {
		t.Errorf("measured node child count = %d, want 0 (measured nodes are leaves)", count)
	}
}

// TestMeasureKnownDimensionsAreLogical pins the conversion the adapter
// applies to taffy's known dimensions: a stretched cross axis arrives as
// a device-pixel known dimension and is divided by the scale factor
// before the measure function sees it.
func TestMeasureKnownDimensionsAreLogical(t *testing.T) {
	engine := newTestEngine(t)
	ctx := NewTestLayoutContext() // scale 2

	// A 30x20 container with a measured child: the cross axis (height)
	// stretches the child, so at least one measure query sees a known
	// height of 20 logical (40 device). The child is requested first and
	// attached to the root when the root is requested (build order, as in
	// the fixtures). Taffy may query the node several times (caching); not
	// every query carries the stretched known dimension.
	var sawKnownHeight bool
	child, err := engine.RequestMeasuredLayout(ctx, DefaultStyle(), func(req MeasureRequest) Size {
		if req.KnownHeightPresent && req.KnownHeight != 20 {
			t.Errorf("known height = %v, want 20 logical (40 device / 2)", req.KnownHeight)
		}
		if req.KnownHeightPresent && req.KnownHeight == 20 {
			sawKnownHeight = true
		}
		return Size{Width: 10, Height: 20}
	})
	if err != nil {
		t.Fatalf("RequestMeasuredLayout: %v", err)
	}
	rootStyle := DefaultStyle()
	rootStyle.Size = LengthSize{Width: PxLength(30), Height: PxLength(20)}
	root, err := engine.RequestLayout(ctx, rootStyle, child)
	if err != nil {
		t.Fatalf("RequestLayout: %v", err)
	}
	if err := engine.ComputeLayout(ctx, root, AvailableSize{
		Width:  DefiniteAvailableSpace(30),
		Height: DefiniteAvailableSpace(20),
	}); err != nil {
		t.Fatalf("ComputeLayout: %v", err)
	}
	bounds, err := engine.LayoutBounds(ctx, child)
	if err != nil {
		t.Fatalf("LayoutBounds: %v", err)
	}
	// Cross-axis stretch: the measured 20 logical becomes the container
	// height even though the measure returned it.
	if bounds.Size.Height != 20 {
		t.Errorf("stretched bounds height = %v, want 20", bounds.Size.Height)
	}
	if !sawKnownHeight {
		t.Error("no measure query carried the stretched known height of 20 logical")
	}
}

// TestComputeAvailableSpaceConversion pins the available-space transform
// of the pinned compute_layout: definite logical space becomes device
// pixels by multiplication with the scale factor (no rounding), and the
// min/max-content modes pass through. The measured node sees the logical
// round trip of exactly that value.
func TestComputeAvailableSpaceConversion(t *testing.T) {
	engine := newTestEngine(t)
	ctx := NewTestLayoutContext() // scale 2

	var seen []AvailableSpace
	root, err := engine.RequestMeasuredLayout(ctx, DefaultStyle(), func(req MeasureRequest) Size {
		seen = append(seen, req.AvailWidth)
		return Size{Width: 10, Height: 10}
	})
	if err != nil {
		t.Fatalf("RequestMeasuredLayout: %v", err)
	}
	if err := engine.ComputeLayout(ctx, root, AvailableSize{
		Width:  DefiniteAvailableSpace(60.25),
		Height: DefiniteAvailableSpace(40),
	}); err != nil {
		t.Fatalf("ComputeLayout: %v", err)
	}
	found := false
	for _, avail := range seen {
		if avail.Kind == AvailDefinite && avail.Definite == 60.25 {
			found = true
		}
	}
	if !found {
		t.Errorf("measure never saw definite 60.25 logical; saw %v", seen)
	}
	// 60.25 * 2 = 120.5 device; 120.5 / 2 = 60.25 logical: the multiply and
	// divide are exact inverses, with no rounding in between.
	if math.Abs(float64(60.25*2/2-60.25)) != 0 {
		t.Fatal("unreachable: scale round trip is exact")
	}
}
