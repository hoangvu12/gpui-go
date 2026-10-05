//go:build windows

// Package layoutspec holds ticket06's layout adapter tests: real layout
// computed through the public gpui API against the embedded native Taffy
// artifact. They exercise the engine from outside the runtime package,
// matching the repo convention (see internal/taskspec and
// internal/winhostspec).
//
// Environment expectation: the native layout artifact is Windows AMD64
// (loaded from the embedded bytes, materialized into the per-user cache
// by internal/native); this machine is expected to run these tests
// interactively. The conformance gates themselves live in
// internal/portfixture (fx-0001 and fx-0002); this package pins the
// engine behaviors the fixtures do not reach.
package layoutspec

import (
	"errors"
	"testing"

	"gpui-go/gpui"
	"gpui-go/internal/native"
)

// newEngine creates a layout engine, disposed at cleanup.
func newEngine(t *testing.T) *gpui.LayoutEngine {
	t.Helper()
	engine, err := gpui.NewLayoutEngine()
	if err != nil {
		t.Fatalf("gpui.NewLayoutEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Dispose() })
	return engine
}

// computeDefinite computes root under a definite available size.
func computeDefinite(t *testing.T, engine *gpui.LayoutEngine, ctx *gpui.LayoutContext, root gpui.LayoutID, w, h float32) {
	t.Helper()
	err := engine.ComputeLayout(ctx, root, gpui.AvailableSize{
		Width:  gpui.DefiniteAvailableSpace(w),
		Height: gpui.DefiniteAvailableSpace(h),
	})
	if err != nil {
		t.Fatalf("ComputeLayout: %v", err)
	}
}

// sizedBox returns a Style with a definite px size.
func sizedBox(w, h float32) gpui.Style {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(w), Height: gpui.PxLength(h)}
	return style
}

// TestSameEngineReentryRejectedFromMeasure pins the same-engine re-entry
// guard: while a compute runs (including inside its measure callbacks),
// every other operation on the SAME engine fails with the engine-busy
// error before native entry — a nested compute, a bounds query (even a
// cached one) and a new node request. The measure function still returns
// normally and the outer compute succeeds.
func TestSameEngineReentryRejectedFromMeasure(t *testing.T) {
	engine := newEngine(t)
	ctx := gpui.NewTestLayoutContext()

	// A first, ordinary tree in the same engine: its bounds get cached, so
	// a bounds read during the second compute exercises the cached path of
	// the guard too (the reference makes any query impossible by taking
	// the engine out of its window during compute).
	firstRoot, err := engine.RequestLayout(ctx, sizedBox(40, 40))
	if err != nil {
		t.Fatalf("RequestLayout: %v", err)
	}
	computeDefinite(t, engine, ctx, firstRoot, 40, 40)
	if _, err := engine.LayoutBounds(ctx, firstRoot); err != nil {
		t.Fatalf("LayoutBounds(firstRoot) before: %v", err)
	}

	var nestedComputeErr, cachedBoundsErr, freshBoundsErr, requestErr error
	var measured gpui.LayoutID
	measured, err = engine.RequestMeasuredLayout(ctx, sizedBox(10, 10), func(req gpui.MeasureRequest) gpui.Size {
		nestedComputeErr = engine.ComputeLayout(ctx, firstRoot, gpui.AvailableSize{
			Width:  gpui.DefiniteAvailableSpace(40),
			Height: gpui.DefiniteAvailableSpace(40),
		})
		_, cachedBoundsErr = engine.LayoutBounds(ctx, firstRoot)
		_, freshBoundsErr = engine.LayoutBounds(ctx, measured)
		_, requestErr = engine.RequestLayout(ctx, sizedBox(1, 1))
		return gpui.Size{Width: 10, Height: 10}
	})
	if err != nil {
		t.Fatalf("RequestMeasuredLayout: %v", err)
	}
	// The measured node is the root of the second tree.
	computeDefinite(t, engine, ctx, measured, 10, 10)

	for _, c := range []struct {
		name string
		err  error
	}{
		{"nested compute", nestedComputeErr},
		{"cached bounds", cachedBoundsErr},
		{"fresh bounds", freshBoundsErr},
		{"node request", requestErr},
	} {
		if c.err == nil {
			t.Errorf("%s during a measure callback: unexpectedly succeeded", c.name)
			continue
		}
		if !errors.Is(c.err, native.ErrLayoutEngineBusy) {
			t.Errorf("%s during a measure callback: got %v, want the engine-busy error", c.name, c.err)
		}
	}

	// The outer compute succeeded and the measured node's bounds reflect
	// the returned size (snapped: 10x10 logical = 20x20 device).
	bounds, err := engine.LayoutBounds(ctx, measured)
	if err != nil {
		t.Fatalf("LayoutBounds(measured): %v", err)
	}
	if bounds.Size.Width != 10 || bounds.Size.Height != 10 {
		t.Errorf("measured bounds = %+v, want 10x10", bounds)
	}

	// After the compute returned, the engine accepts operations again.
	after, err := engine.RequestLayout(ctx, sizedBox(5, 5))
	if err != nil {
		t.Fatalf("RequestLayout after compute: %v", err)
	}
	computeDefinite(t, engine, ctx, after, 5, 5)
}

// TestIndependentEngineNestingAllowedFromMeasure pins the cross-engine
// rule: a measure function of one engine may compute and query a
// DIFFERENT engine (each engine keeps its own busy state), and the outer
// compute still finishes with the measured size in place.
func TestIndependentEngineNestingAllowedFromMeasure(t *testing.T) {

	// After the compute returned, the engine accepts operations again.
	engineA := newEngine(t)
	engineB := newEngine(t)
	ctx := gpui.NewTestLayoutContext()

	// Engine B holds a small definite tree.
	bRoot, err := engineB.RequestLayout(ctx, sizedBox(30, 20))
	if err != nil {
		t.Fatalf("engineB.RequestLayout: %v", err)
	}
	bChild, err := engineB.RequestLayout(ctx, sizedBox(10, 10))
	if err != nil {
		t.Fatalf("engineB.RequestLayout(child): %v", err)
	}
	bTree, err := engineB.RequestLayout(ctx, sizedBox(40, 20), bRoot, bChild)
	if err != nil {
		t.Fatalf("engineB.RequestLayout(root): %v", err)
	}
	_ = bTree

	var nestedErr error
	var nestedBounds gpui.Bounds
	measured, err := engineA.RequestMeasuredLayout(ctx, sizedBox(10, 10), func(req gpui.MeasureRequest) gpui.Size {
		// Compute engine B's tree from inside engine A's measure callback.
		nestedErr = engineB.ComputeLayout(ctx, bTree, gpui.AvailableSize{
			Width:  gpui.DefiniteAvailableSpace(40),
			Height: gpui.DefiniteAvailableSpace(20),
		})
		if nestedErr == nil {
			nestedBounds, nestedErr = engineB.LayoutBounds(ctx, bChild)
		}
		return gpui.Size{Width: 10, Height: 10}
	})
	if err != nil {
		t.Fatalf("engineA.RequestMeasuredLayout: %v", err)
	}
	if err := engineA.ComputeLayout(ctx, measured, gpui.AvailableSize{
		Width:  gpui.DefiniteAvailableSpace(10),
		Height: gpui.DefiniteAvailableSpace(10),
	}); err != nil {
		t.Fatalf("engineA.ComputeLayout: %v", err)
	}
	if nestedErr != nil {
		t.Fatalf("nested engineB compute from engineA's measure: %v", nestedErr)
	}
	// Engine B's child is the second flex item: it follows the 30px
	// sibling at x 30 with its own 10x10 snapped size.
	if nestedBounds.Origin.X != 30 || nestedBounds.Origin.Y != 0 ||
		nestedBounds.Size.Width != 10 || nestedBounds.Size.Height != 10 {
		t.Errorf("engineB nested bounds = %+v, want origin (30,0) size 10x10", nestedBounds)
	}
	// The outer measure result is still the measured node's size.
	bounds, err := engineA.LayoutBounds(ctx, measured)
	if err != nil {
		t.Fatalf("engineA.LayoutBounds: %v", err)
	}
	if bounds.Size.Width != 10 || bounds.Size.Height != 10 {
		t.Errorf("engineA measured bounds = %+v, want 10x10", bounds)
	}
}

// TestParentRelativeLayoutBoundsSnapsInParentSpace pins the dedicated
// parent-relative path (the pinned parent_relative_layout_bounds): the
// node's local device origin and size are each rounded with the
// midpoint-toward-zero helper INSIDE the parent's coordinate space, then
// divided by the scale factor — which differs from the absolute edge
// snapping of LayoutBounds when a shared boundary rounds differently.
//
// The case: a 49.5px root (99 device) with two 50% children (49.5 device
// each). The second child's local origin 49.5 device rounds to 49 (ties
// toward zero), so its parent-relative size is 49 device (24.5 logical),
// while its absolute edges are 49.5 -> 49 and 99 -> 99, a 50 device
// (25 logical) span.
func TestParentRelativeLayoutBoundsSnapsInParentSpace(t *testing.T) {
	engine := newEngine(t)
	ctx := gpui.NewTestLayoutContext() // scale 2

	rootStyle := gpui.DefaultStyle()
	rootStyle.Size = gpui.LengthSize{Width: gpui.PxLength(49.5), Height: gpui.PxLength(10)}
	childStyle := gpui.DefaultStyle()
	childStyle.Size.Width = gpui.DefiniteLengthOf(gpui.Fraction(0.5))

	// Children are requested first and attached to the parent.
	childA, err := engine.RequestLayout(ctx, childStyle)
	if err != nil {
		t.Fatalf("RequestLayout(childA): %v", err)
	}
	childB, err := engine.RequestLayout(ctx, childStyle)
	if err != nil {
		t.Fatalf("RequestLayout(childB): %v", err)
	}
	root, err := engine.RequestLayout(ctx, rootStyle, childA, childB)
	if err != nil {
		t.Fatalf("RequestLayout(root): %v", err)
	}
	computeDefinite(t, engine, ctx, root, 49.5, 10)

	// Absolute edge snapping (the ordinary bounds).
	absA, err := engine.LayoutBounds(ctx, childA)
	if err != nil {
		t.Fatalf("LayoutBounds(childA): %v", err)
	}
	absB, err := engine.LayoutBounds(ctx, childB)
	if err != nil {
		t.Fatalf("LayoutBounds(childB): %v", err)
	}
	if absA.Origin.X != 0 || absA.Size.Width != 24.5 || absA.Size.Height != 10 {
		t.Errorf("absolute bounds A = %+v, want origin.x 0, size 24.5x10", absA)
	}
	if absB.Origin.X != 24.5 || absB.Size.Width != 25 || absB.Size.Height != 10 {
		t.Errorf("absolute bounds B = %+v, want origin.x 24.5, size 25x10", absB)
	}

	// Parent-relative snapping: the local origin and size are rounded
	// independently of the parent's position.
	relA, err := engine.ParentRelativeLayoutBounds(ctx, childA)
	if err != nil {
		t.Fatalf("ParentRelativeLayoutBounds(childA): %v", err)
	}
	relB, err := engine.ParentRelativeLayoutBounds(ctx, childB)
	if err != nil {
		t.Fatalf("ParentRelativeLayoutBounds(childB): %v", err)
	}
	if relA.Origin.X != 0 || relA.Size.Width != 24.5 || relA.Size.Height != 10 {
		t.Errorf("parent-relative bounds A = %+v, want origin.x 0, size 24.5x10", relA)
	}
	if relB.Origin.X != 24.5 || relB.Size.Width != 24.5 || relB.Size.Height != 10 {
		t.Errorf("parent-relative bounds B = %+v, want origin.x 24.5, size 24.5x10 (round(49.5) = 49 device)", relB)
	}

	// The parentless root falls back to its ordinary layout bounds.
	relRoot, err := engine.ParentRelativeLayoutBounds(ctx, root)
	if err != nil {
		t.Fatalf("ParentRelativeLayoutBounds(root): %v", err)
	}
	absRoot, err := engine.LayoutBounds(ctx, root)
	if err != nil {
		t.Fatalf("LayoutBounds(root): %v", err)
	}
	if relRoot != absRoot {
		t.Errorf("parentless node parent-relative = %+v, want the ordinary bounds %+v", relRoot, absRoot)
	}
}

// TestLayoutBoundsElementOffset pins the snapped element offset: the
// context's offset is snapped (round to device, divide back) and added
// to the origin of every queried bounds, exactly like the reference's
// window.layout_bounds adding pixel_snap_point(element_offset()).
func TestLayoutBoundsElementOffset(t *testing.T) {
	engine := newEngine(t)
	ctx := gpui.NewTestLayoutContext()

	root, err := engine.RequestLayout(ctx, sizedBox(10, 10))
	if err != nil {
		t.Fatalf("RequestLayout: %v", err)
	}
	computeDefinite(t, engine, ctx, root, 10, 10)

	plain, err := engine.LayoutBounds(ctx, root)
	if err != nil {
		t.Fatalf("LayoutBounds: %v", err)
	}
	if plain.Origin.X != 0 || plain.Origin.Y != 0 {
		t.Fatalf("bounds without offset = %+v, want origin (0,0)", plain)
	}

	// An offset of 0.25 logical at scale 2 is 0.5 device, which snaps
	// toward zero: the origin stays 0. An offset of 0.3 logical is 0.6
	// device, snapping to 1 device = 0.5 logical.
	quarter := ctx.WithElementOffset(gpui.Point{X: 0.25, Y: 0.25})
	snappedTowardZero, err := engine.LayoutBounds(quarter, root)
	if err != nil {
		t.Fatalf("LayoutBounds(quarter): %v", err)
	}
	if snappedTowardZero.Origin.X != 0 || snappedTowardZero.Origin.Y != 0 {
		t.Errorf("bounds with a half-toward-zero offset = %+v, want origin (0,0)", snappedTowardZero)
	}
	pointThree := ctx.WithElementOffset(gpui.Point{X: 0.3, Y: 0.3})
	snappedUp, err := engine.LayoutBounds(pointThree, root)
	if err != nil {
		t.Fatalf("LayoutBounds(pointThree): %v", err)
	}
	if snappedUp.Origin.X != 0.5 || snappedUp.Origin.Y != 0.5 {
		t.Errorf("bounds with a snapped offset = %+v, want origin (0.5,0.5)", snappedUp)
	}
}
