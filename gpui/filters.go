package gpui

import "fmt"

// This file is the port's filter half of ticket16: the pinned
// Window::with_filter_layer / paint_backdrop_filter wrapper layer over
// the scene kernel's filter primitives (the boundary/backdrop records
// of scene.go). The record-level seam (BeginFilterGroup /
// EndFilterGroup / PaintBackdrop) covers the same pinned semantics;
// these wrappers give the pinned closure ergonomics: one snapshot per
// group, matched start/end markers even on early error paths, and the
// documented opacity rule (the group opacity is 1.0 — NOT element
// opacity — because the group's children already carry it; re-applying
// it at composite time would double it, window.rs:4537-4569).

// WithFilterLayer isolates the painting performed by f into a
// content-filter group: the renderer renders everything f paints into
// an offscreen target, blurs it as a single layer, and composites the
// result back into the rounded rectangle described by bounds and radii
// — the CSS `filter` effect (the pinned with_filter_layer,
// window.rs:4526). When the blur radius produces no visible blur (an
// identity filter), f simply runs with no offscreen indirection.
//
// The nested-depth semantics come from the plan, not this wrapper: the
// first two nested groups get dedicated isolation targets
// (MAX_FILTER_GROUP_DEPTH = 2), deeper groups render inline exactly as
// the pinned plan directs (no invented third isolation target).
func (s *Scene) WithFilterLayer(bounds Bounds, blurRadius float32, radii Corners, ctx *PaintContext, f func() error) error {
	return s.WithFilterLayerCornerSmoothing(bounds, blurRadius, radii, 0, ctx, f)
}

// WithFilterLayerCornerSmoothing runs f in a content-filter group
// clipped to smoothed corners (the pinned
// with_filter_layer_with_corner_smoothing).
func (s *Scene) WithFilterLayerCornerSmoothing(bounds Bounds, blurRadius float32, radii Corners, cornerSmoothing float32, ctx *PaintContext, f func() error) error {
	return s.WithFilterLayerChain(bounds, FilterChain{BlurRadius: blurRadius}, radii, cornerSmoothing, ctx, f)
}

// WithFilterLayerChain runs f in one content-filter group carrying a
// whole composed chain (browser-core ticket 41): the renderer renders
// everything f paints into an offscreen target, applies the composed
// gaussian blur and the composed 4x5 colour matrix, and composites the
// result back into the rounded rectangle — the CSS filter effect for
// grayscale(…), sepia(…), brightness(…) and the rest of the shorthand
// colour functions, in one group per chain (the CSS chain order; a
// per-pixel linear map commutes with the uniform-per-channel gaussian,
// so the native draw order is the CSS result for any interleaving —
// see FilterChain). An empty chain runs f inline with no offscreen
// indirection (every filter identity).
func (s *Scene) WithFilterLayerChain(bounds Bounds, chain FilterChain, radii Corners, cornerSmoothing float32, ctx *PaintContext, f func() error) error {
	if ctx == nil {
		return fmt.Errorf("gpui: WithFilterLayerChain: nil paint context")
	}
	if f == nil {
		return fmt.Errorf("gpui: WithFilterLayerChain: nil closure")
	}
	// A chain carrying drop-shadow OPS (the source form,
	// FilterChain.DropShadowOps — browser-core ticket 57) resolves
	// here: the ops insert scene-level (scaled to device pixels) and
	// the boundary pair carries the resolved chain (the op-chain index
	// and count). Chromium builds FEDropShadow for each function
	// (filter_effect_builder.cc:293-301) and draws each shadow beneath
	// the content. A chain carrying the resolved reference only passes
	// through unchanged.
	if len(chain.DropShadowOps) > 0 {
		ops := make([]FilterOp, len(chain.DropShadowOps))
		for i, op := range chain.DropShadowOps {
			ops[i] = FilterOp{
				OffsetX:    op.OffsetX * ctx.ScaleFactor,
				OffsetY:    op.OffsetY * ctx.ScaleFactor,
				BlurRadius: op.BlurRadius * ctx.ScaleFactor,
				Color:      op.Color,
			}
		}
		index, err := s.InsertFilterOpChain(ops)
		if err != nil {
			return fmt.Errorf("gpui: WithFilterLayerChain: drop-shadow ops: %w", err)
		}
		chain.DropShadow = index
		chain.DropShadowCount = len(ops)
		chain.DropShadowOps = nil
	}
	if err := s.BeginFilterGroupChain(bounds, chain, radii, cornerSmoothing, ctx); err != nil {
		return err
	}
	// The pinned code snapshots the boundary once and re-uses it for the
	// end marker; the record-level seam takes the same parameters, so an
	// early error path still closes the group before returning.
	err := f()
	if closeErr := s.EndFilterGroupChain(bounds, chain, radii, cornerSmoothing, ctx); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}

// PaintBackdropFilter paints a backdrop filter into the scene at the
// current z-index (the pinned paint_backdrop_filter,
// window.rs:4477-4524): the renderer blurs the content already painted
// behind bounds and composites the result into the rounded rectangle —
// the CSS `backdrop-filter` effect (frosted glass). Typically the
// element then paints a translucent background quad on top so its
// color tints the blurred backdrop. Does nothing when the blur radius
// produces no visible blur.
func (s *Scene) PaintBackdropFilter(bounds Bounds, blurRadius float32, radii Corners, ctx *PaintContext) error {
	return s.PaintBackdropFilterCornerSmoothing(bounds, blurRadius, radii, 0, ctx)
}

// PaintBackdropFilterCornerSmoothing paints a backdrop filter clipped
// to smoothed corners (the pinned
// paint_backdrop_filter_with_corner_smoothing).
func (s *Scene) PaintBackdropFilterCornerSmoothing(bounds Bounds, blurRadius float32, radii Corners, cornerSmoothing float32, ctx *PaintContext) error {
	return s.PaintBackdrop(bounds, blurRadius, radii, cornerSmoothing, ctx)
}
