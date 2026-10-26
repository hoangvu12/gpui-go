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
	// CornerRadii of the rounded clip, logical pixels (zero = a plain
	// rectangular clip).
	CornerRadii Corners
	// CornerSmoothing of the rounded clip in [0, 1].
	CornerSmoothing float32
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
