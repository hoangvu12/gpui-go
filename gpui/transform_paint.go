package gpui

import (
	"fmt"
)

// The element-transform scope of the div's prepaint and paint
// (browser-core ticket 56; the model lives in transform.go's file
// comment).
//
// PREPAINT resolves the style's transform list into the window-to-
// window matrix and opens one of two scopes around everything from the
// debug-bounds record to the children's prepaint:
//
//   - a translation-only matrix (the design §3.4 fast path) shifts the
//     element's own bounds and the children's element offset — the
//     sticky-shift mechanism, the pin's existing offset economy, no
//     scene group at all (the children paint at their translated
//     positions);
//   - anything else pushes the matrix onto the frame's transform scope
//     (hitboxes inserted from here down compose it into their hit
//     test) and leaves the bounds alone — PAINT brackets the subtree
//     in a scene effect group under the matrix.
//
// PAINT mirrors the same decision from the matrix the prepaint
// resolved (stored in the div prepaint state, so the two phases agree
// even when a hover style flips between them): the shift re-applies to
// the bounds and the offset; the matrix opens the effect group around
// the element's whole box — background, borders, shadows, outline and
// children — as one transformed unit.
//
// Elements participating in a parent paragraph get no transform: CSS
// applies `transform` to transformable elements only (css-transforms-1
// §transform-property; a plain inline is not one), and the port's
// fragment geometry has no matrix seam — a recorded limitation for
// atomic inlines, which CSS would transform.

// pushTransformScope pushes the element's own matrix onto the frame's
// transform scope (composed with the parent's: entry = parent·own, so
// the stack top maps this subtree's layout space to the window).
func pushTransformScope(w *Window, own TransformationMatrix) {
	frame := currentFrame(w)
	parent := UnitTransformation()
	if len(frame.transformScopes) > 0 {
		parent = frame.transformScopes[len(frame.transformScopes)-1]
	}
	frame.transformScopes = append(frame.transformScopes, mulTransformation(parent, own))
}

// popTransformScope pops the innermost transform scope (balanced with
// pushTransformScope).
func popTransformScope(w *Window) {
	frame := currentFrame(w)
	frame.transformScopes = frame.transformScopes[:len(frame.transformScopes)-1]
}

// currentTransformScope returns the composed matrix a hitbox inserted
// now carries, or nil when no transform is open.
func currentTransformScope(w *Window) *TransformationMatrix {
	frame := currentFrame(w)
	if len(frame.transformScopes) == 0 {
		return nil
	}
	m := frame.transformScopes[len(frame.transformScopes)-1]
	return &m
}

// elementTransformState classifies a style's resolved transform.
type elementTransformState uint8

const (
	// transformNone: the style carries no functions (or the element
	// participates in a parent paragraph) — no scope.
	transformNone elementTransformState = iota
	// transformMatrix: the functions resolve to an invertible matrix.
	transformMatrix
	// transformCollapse: the composed 2x2 is singular (scale 0) — the
	// element paints nothing and hit tests nothing, like CSS's
	// collapsed transform.
	transformCollapse
)

// elementTransform resolves the style's transform into the
// window-to-window matrix for the element's border box. The state
// classifies the answer (transformNone, transformMatrix,
// transformCollapse).
func elementTransform(style *Style, bounds Bounds, inParentParagraph bool, rem float32) (TransformationMatrix, elementTransformState) {
	if inParentParagraph || len(style.Transform) == 0 {
		return TransformationMatrix{}, transformNone
	}
	m, ok := transformMatrixOf(style.Transform, style.TransformOrigin, bounds, rem)
	if !ok {
		return TransformationMatrix{}, transformCollapse
	}
	return m, transformMatrix
}

// transformScopeSplit is the prepaint's transform decision: the
// translation-only fast path's shift (the sticky mechanism), and the
// matrix scope when the transform needs the group. At most one is
// non-zero: a translation-only matrix yields the shift and no matrix
// scope; anything else yields the matrix. A collapsed transform
// yields a singular matrix the hit test always rejects.
type transformScopeSplit struct {
	// shift is the translation-only transform's window-space shift,
	// applied to the element's own bounds and the children's element
	// offset (Point{} when zero or matrix-scoped).
	shift Point
	// matrix is the composed window-to-window matrix the hitboxes and
	// paint carry (nil when the transform took the shift path or there
	// is none).
	matrix *TransformationMatrix
}

// splitTransform decides the scope for the resolved matrix: a
// translation-only matrix (with the origin resolved into it, which a
// pure translation is origin-independent of) takes the offset fast
// path; anything else takes the matrix scope; a collapsed transform
// carries a singular matrix so every hit test below it fails.
func splitTransform(m TransformationMatrix, state elementTransformState) transformScopeSplit {
	switch state {
	case transformNone:
		return transformScopeSplit{}
	case transformCollapse:
		// A singular matrix: Contains fails, LocalPosition degrades, the
		// paint skips the subtree.
		singular := TransformationMatrix{RotationScale: [2][2]float32{{0, 0}, {0, 0}}}
		return transformScopeSplit{matrix: &singular}
	}
	if tx, ty, translation := transformationIsTranslation(m); translation {
		return transformScopeSplit{shift: Point{X: tx, Y: ty}}
	}
	return transformScopeSplit{matrix: &m}
}

// transformCollapsed reports whether the resolved matrix is the
// singular collapse marker (scale 0): the paint skips the subtree and
// every hit test below it fails.
func transformCollapsed(m *TransformationMatrix) bool {
	return m != nil && transformationDeterminant(*m) == 0
}

// withElementTransformPaint brackets the element's whole painted box
// in one scene effect group under the resolved matrix (browser-core
// ticket 56 riding ticket 51's ABI record): the subtree's records stay
// at their untransformed window coordinates — the record's LOCAL frame
// is the untransformed window frame — and the composite maps them
// through the device-space matrix, sampling the bucket with linear
// filtering. The group's own opacity is 1 (transform-only groups never
// fade; the ABI's composite always multiplies by the record's opacity
// field). A nil matrix runs f unchanged.
//
// The group's content mask is the current content mask in device
// pixels (the filter-boundary convention): the composite's local-space
// clip never cuts surviving content, because the subtree's records are
// already clipped by the same mask at insert time.
func withElementTransformPaint(w *Window, matrix *TransformationMatrix, bounds Bounds, f func(*Window)) {
	if matrix == nil {
		f(w)
		return
	}
	frame := currentFrame(w)
	scale := frame.scale
	group := EffectGroup{
		HasTransform: true,
		Opacity:      1,
		Transform:    deviceTransform(*matrix, scale),
		Bounds:       snapBounds(bounds, scale),
		ContentMask:  snappedContentMask(frame.contentMask, scale),
	}
	if err := frame.scene.BeginEffectGroup(group); err != nil {
		panic(fmt.Sprintf("gpui: DivElement paint: begin transform group: %v", err))
	}
	f(w)
	end := group
	end.IsStart = false
	if err := frame.scene.EndEffectGroup(end); err != nil {
		panic(fmt.Sprintf("gpui: DivElement paint: end transform group: %v", err))
	}
}
