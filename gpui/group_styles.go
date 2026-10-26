package gpui

import "fmt"

// This file is the R16 group-styling half of browser-core ticket 55:
// the style-level content mask with the rounded-corner clip
// (Style.OverflowMask's radii), the rounded_clip_only effect group the
// paint path brackets an overflowing rounded box's children in, and the
// element-opacity group condition (D9). The scene encoding it rides is
// ticket 50's EffectGroup record (scene.go); the native plan isolates
// every matched group, and the single-primitive opacity fold is native
// too (Chromium's "at least two layers in subtree draw content" rule,
// cc/trees/property_tree_builder.cc:395-403 @ 0d4dcd4f).

// ClipMask is the style-level content mask: the clip rectangle plus
// the rounded-corner clip (rev 16, browser-core ticket 55). The
// rectangle is the overflow clip - the border box inset by the border
// widths, CSS's padding box (Chromium's OverflowClipRect contracts the
// border box by the border outsets unconditionally,
// core/layout/layout_box.cc:2288-2302); the radii are the INNER border
// radii (the padding-box corner rounding: Chromium clips an overflowing
// box's descendants to ContouredBorderGeometry::ContouredInnerBorder -
// the border rounded rect inset by the border widths,
// core/paint/contoured_border_geometry.cc:209-219 - through
// UpdateInnerBorderRadiusClip, core/paint/paint_property_tree_builder.
// cc:3257-3293).
//
// A mask with zero radii is a plain rectangular clip - exactly the
// pre-R16 behaviour every existing scene encodes.
type ClipMask struct {
	// Bounds is the clip rectangle, logical pixels.
	Bounds Bounds
	// Fade is the clip's per-edge fade distance, logical pixels (rev
	// 16, browser-core ticket 57): content within the distance of an
	// edge fades out linearly toward it (the pin's ContentMask
	// fade_out, Style.OverflowFade's paint). A zero distance leaves
	// the edge opaque.
	Fade Edges
	// CornerRadii of the rounded clip, logical pixels (zero = a plain
	// rectangular clip).
	CornerRadii Corners
	// CornerSmoothing of the rounded clip in [0, 1].
	CornerSmoothing float32
}

// HasFade reports whether the mask fades any edge.
func (m ClipMask) HasFade() bool {
	return m.Fade.Top != 0 || m.Fade.Right != 0 || m.Fade.Bottom != 0 || m.Fade.Left != 0
}

// intersectMaskFade intersects two content masks' per-edge fades with
// the pin's rule (window.rs ContentMask::intersect, window.rs
// 2128-2170): the mask whose edge actually clips (the closer
// coordinate) contributes its fade, and when both clip at the same
// coordinate the stronger (larger) fade wins. aMask pairs with aFade,
// bMask with bFade.
func intersectMaskFade(aMask Bounds, aFade Edges, bMask Bounds, bFade Edges) Edges {
	return Edges{
		Top:    edgeFade(aMask.Top(), bMask.Top(), aFade.Top, bFade.Top, func(a, b float32) bool { return a > b }),
		Right:  edgeFade(aMask.Right(), bMask.Right(), aFade.Right, bFade.Right, func(a, b float32) bool { return a < b }),
		Bottom: edgeFade(aMask.Bottom(), bMask.Bottom(), aFade.Bottom, bFade.Bottom, func(a, b float32) bool { return a < b }),
		Left:   edgeFade(aMask.Left(), bMask.Left(), aFade.Left, bFade.Left, func(a, b float32) bool { return a > b }),
	}
}

// edgeFade is intersectMaskFade's per-edge rule.
func edgeFade(a, b, aFade, bFade float32, aClipsCloser func(a, b float32) bool) float32 {
	if aClipsCloser(a, b) {
		return aFade
	}
	if aClipsCloser(b, a) {
		return bFade
	}
	return max32(aFade, bFade)
}

// IsRounded reports whether the mask clips with rounded corners.
func (m ClipMask) IsRounded() bool {
	return m.CornerRadii.TopLeft != 0 || m.CornerRadii.TopRight != 0 ||
		m.CornerRadii.BottomRight != 0 || m.CornerRadii.BottomLeft != 0
}

// withRoundedClipScope brackets f's painting in the rounded_clip_only
// effect group of a rounded overflow mask (rev 16, ticket 55): the
// group's subtree renders into a pooled target and composites clipped
// to the mask's rounded rectangle, so every primitive kind clips - the
// vendored quad/shadow pipelines clip an inline mask's radii, but the
// pinned underline/sprite/path pipelines consume bounds+fade only (the
// recorded native divergence 4, reference/native/shaders/mod.rs), and
// the group composite is the one anti-aliased rrect clip (carrying the
// radii on the records too would double the corner coverage - the
// composite's clip times the inline clip - and halve the corner
// anti-aliasing band).
//
// The group's Bounds are the mask's own rectangle so the radii sit at
// ITS corners; the record's ContentMask is the intersected rectangle so
// the ancestors' clips still apply through the composite. Nested
// rounded clips nest their groups: each composite clips its own subtree,
// and the parent group's composite clips the child group's result -
// the "innermost radii win, ancestors' rounded clips become groups"
// rule of the design's §3.3.
func withRoundedClipScope[R any](w *Window, mask ClipMask, intersected Bounds, f func(*Window) R) R {
	frame := currentFrame(w)
	scale := frame.paint.ScaleFactor
	group := EffectGroup{
		ClipToBounds:    true,
		RoundedClipOnly: true,
		Opacity:         1,
		Bounds:          snapBounds(mask.Bounds, scale),
		ContentMask:     snappedContentMask(intersected, scale),
		CornerRadii:     scaleCorners(mask.CornerRadii, scale),
		CornerSmoothing: clamp01(mask.CornerSmoothing),
	}
	if err := frame.scene.BeginEffectGroup(group); err != nil {
		panic(fmt.Sprintf("gpui: WithRoundedContentMask: rounded clip group: %v", err))
	}
	defer func() {
		if err := frame.scene.EndEffectGroup(group); err != nil {
			panic(fmt.Sprintf("gpui: WithRoundedContentMask: rounded clip group end: %v", err))
		}
	}()
	return f(w)
}

// InkBounds is the element's ink bounds (the border box dilated by the
// drop shadows' and the outer ring's extent, and the outline's outset —
// Chromium's filter reference box, the layer's visual overflow:
// PaintLayer::LocalBoundingBox → VisualOverflowRect,
// paint_layer.cc:2199-2205, fed by ComputedStyle::BoxDecorationOutsets
// and the outline extent, outline_painter.cc:1073-1082): the group
// bounds of the element's filter and blend groups (the composite must
// cover the shadow ink; the pin passed the element bounds and cut the
// blurred shadow at the border).
func (s *Style) InkBounds(bounds Bounds, rem float32) Bounds {
	var ink Edges
	for _, shadow := range s.BoxShadow {
		if shadow.Inset {
			continue
		}
		o := shadow.inkOutsets()
		ink.Top = max32(ink.Top, o.Top)
		ink.Right = max32(ink.Right, o.Right)
		ink.Bottom = max32(ink.Bottom, o.Bottom)
		ink.Left = max32(ink.Left, o.Left)
	}
	if s.Ring.Width > 0 {
		ink.Top = max32(ink.Top, s.Ring.Width)
		ink.Right = max32(ink.Right, s.Ring.Width)
		ink.Bottom = max32(ink.Bottom, s.Ring.Width)
		ink.Left = max32(ink.Left, s.Ring.Width)
	}
	if outline := s.Outline.inkOutset(s.BorderWidths.ToPixels(rem)); outline > 0 {
		ink.Top = max32(ink.Top, outline)
		ink.Right = max32(ink.Right, outline)
		ink.Bottom = max32(ink.Bottom, outline)
		ink.Left = max32(ink.Left, outline)
	}
	return Bounds{
		Origin: Point{X: bounds.Origin.X - ink.Left, Y: bounds.Origin.Y - ink.Top},
		Size:   Size{Width: bounds.Size.Width + ink.Left + ink.Right, Height: bounds.Size.Height + ink.Top + ink.Bottom},
	}
}

// opacityCompositesAsGroup reports whether an element with the given
// style and a non-identity opacity composites as an R16 opacity group
// (D9, rev 16, ticket 55). Every style qualifies but one: an element
// with a backdrop-filter chain keeps the per-primitive opacity
// multiply, because the native backdrop-filter pass samples the
// CURRENT target - inside a group's bucket it would blur the empty
// bucket (the recorded native gap; the group composite is the correct
// CSS shape once the backdrop pass can sample the parent target).
func opacityCompositesAsGroup(style *Style) bool {
	return composeFilterChain(style.BackdropFilter).IsEmpty()
}
