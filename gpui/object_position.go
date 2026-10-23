package gpui

// CSS object-position and image-rendering for replaced elements
// (browser-core ticket 41). These are style properties the img element
// consumes at paint time (Style.ObjectPosition, Style.ImageRendering):
// CSS applies them to replaced elements only, so on any other element
// they refine the style and change nothing.

// ObjectPosition is a CSS object-position: where the fitted content
// sits inside the element's content box (css-images-3 §the-object-
// position). The default (and the zero of a missing style value) is
// 50% 50%, centered — Chromium's initial value
// LengthPoint(Length::Percent(50.0), Length::Percent(50.0))
// (core/css/css_properties.json5:4693).
//
// Each axis resolves against the FREE space — the content box minus the
// object's fitted size — exactly as Chromium's
// ComputeObjectFitAndPositionRect places it:
// MinimumValueForLength(ObjectPosition().X(), base.Width() -
// object_size.Width) (core/layout/layout_replaced.cc:469-472): a
// fraction times the free space plus a pixel offset.
type ObjectPosition struct {
	// X positions the content horizontally.
	X ObjectPositionAxis
	// Y positions the content vertically.
	Y ObjectPositionAxis
}

// ObjectPositionAxis is one axis of an ObjectPosition: a fraction of
// the free space (the keywords and percentages) plus a logical-pixel
// offset (the lengths). "right 10px" is Fraction 1 with OffsetPx -10.
type ObjectPositionAxis struct {
	// Fraction is the fraction of (content box - object size) the
	// object's leading edge sits at: 0 left/top, 0.5 center, 1
	// right/bottom.
	Fraction float32
	// OffsetPx is a logical-pixel offset added after the fraction.
	OffsetPx float32
}

// ObjectLeft is the left keyword (0%).
func ObjectLeft() ObjectPositionAxis { return ObjectPositionAxis{Fraction: 0} }

// ObjectCenter is the center keyword (50%).
func ObjectCenter() ObjectPositionAxis { return ObjectPositionAxis{Fraction: 0.5} }

// ObjectRight is the right keyword (100%).
func ObjectRight() ObjectPositionAxis { return ObjectPositionAxis{Fraction: 1} }

// ObjectTop is the top keyword (0%).
func ObjectTop() ObjectPositionAxis { return ObjectPositionAxis{Fraction: 0} }

// ObjectBottom is the bottom keyword (100%).
func ObjectBottom() ObjectPositionAxis { return ObjectPositionAxis{Fraction: 1} }

// ObjectPercent is a percentage axis: percent 75 is 75% of the free
// space from the leading edge.
func ObjectPercent(percent float32) ObjectPositionAxis {
	return ObjectPositionAxis{Fraction: percent / 100}
}

// ObjectPx is a length axis: a fixed logical-pixel offset from the
// leading edge.
func ObjectPx(px float32) ObjectPositionAxis { return ObjectPositionAxis{OffsetPx: px} }

// ObjectPositionCenter is the CSS default position, 50% 50%.
func ObjectPositionCenter() ObjectPosition {
	return ObjectPosition{X: ObjectCenter(), Y: ObjectCenter()}
}

// resolve places one axis: base is the content box's extent, object the
// fitted content's, and the return the offset from the box's leading
// edge — Fraction·(base-object) + OffsetPx (Chromium
// MinimumValueForLength against base - object_size,
// layout_replaced.cc:469-472).
func (a ObjectPositionAxis) resolve(base, object float32) float32 {
	return a.Fraction*(base-object) + a.OffsetPx
}

// place returns the fitted content's bounds inside the content box
// bounds: the size every ObjectFit mode computes, moved by the
// position's resolved offsets.
func (p ObjectPosition) place(bounds Bounds, width, height float32) Bounds {
	return Bounds{
		Origin: Point{
			X: bounds.Origin.X + p.X.resolve(bounds.Size.Width, width),
			Y: bounds.Origin.Y + p.Y.resolve(bounds.Size.Height, height),
		},
		Size: Size{Width: width, Height: height},
	}
}

// GetPositionedBounds returns the bounds of the content within the
// given content box under this object fit, positioned by position (CSS
// object-fit + object-position): the sizes are ObjectFit.GetBounds's,
// and the position resolves against each axis's free space (base -
// object size), so keywords, percentages and lengths all move the
// content the CSS way — fill ignores percentage positions (no free
// space) but still shifts by lengths, exactly as Chromium's
// ComputeObjectFitAndPositionRect does when the position is not the
// default (layout_replaced.cc:410-477: the fill + default-position fast
// path returns the base rect; any other position resolves against a
// zero-size free space).
func (f ObjectFit) GetPositionedBounds(bounds Bounds, contentWidth, contentHeight int32, position ObjectPosition) Bounds {
	if contentWidth <= 0 || contentHeight <= 0 || bounds.Size.Width <= 0 || bounds.Size.Height <= 0 {
		return Bounds{Origin: bounds.Origin}
	}
	imageWidth := float32(contentWidth)
	imageHeight := float32(contentHeight)
	imageRatio := imageWidth / imageHeight
	boundsRatio := bounds.Size.Width / bounds.Size.Height

	containSize := func() (float32, float32) {
		if boundsRatio > imageRatio {
			return imageWidth * (bounds.Size.Height / imageHeight), bounds.Size.Height
		}
		return bounds.Size.Width, imageHeight * (bounds.Size.Width / imageWidth)
	}

	switch f {
	case ObjectFitFill:
		return position.place(bounds, bounds.Size.Width, bounds.Size.Height)
	case ObjectFitContain:
		width, height := containSize()
		return position.place(bounds, width, height)
	case ObjectFitScaleDown:
		// Scale down only: an image already fitting keeps its original
		// size, positioned.
		if imageWidth > bounds.Size.Width || imageHeight > bounds.Size.Height {
			width, height := containSize()
			return position.place(bounds, width, height)
		}
		return position.place(bounds, imageWidth, imageHeight)
	case ObjectFitCover:
		var width, height float32
		if boundsRatio > imageRatio {
			width = bounds.Size.Width
			height = imageHeight * (bounds.Size.Width / imageWidth)
		} else {
			width = imageWidth * (bounds.Size.Height / imageHeight)
			height = bounds.Size.Height
		}
		return position.place(bounds, width, height)
	default: // ObjectFitNone
		// The intrinsic size, positioned (object-position defaults to
		// 50% 50%, so the default placement is centered — the recorded
		// divergence toward CSS ticket 05 made; a general position moves
		// it like Chromium's ComputeObjectFitAndPositionRect, the none
		// arm at layout_replaced.cc:459-463).
		return position.place(bounds, imageWidth, imageHeight)
	}
}

// GetBounds returns the content's bounds under this object fit with the
// CSS default position (50% 50%, centered): GetPositionedBounds with
// ObjectPositionCenter.
func (f ObjectFit) GetBounds(bounds Bounds, contentWidth, contentHeight int32) Bounds {
	return f.GetPositionedBounds(bounds, contentWidth, contentHeight, ObjectPositionCenter())
}

// ImageRendering is the CSS image-rendering property: how images are
// sampled when scaled (css-images-3 §the-image-rendering).
type ImageRendering uint8

const (
	// ImageRenderingAuto lets the renderer choose (the pinned bilinear
	// sampler); the zero value.
	ImageRenderingAuto ImageRendering = iota
	// ImageRenderingPixelated scales with nearest-neighbour sampling —
	// hard edges on upscaled pixel art (the native rev-15 point
	// sampler).
	ImageRenderingPixelated
	// ImageRenderingCrispEdges emphasizes contrast and edges; Chromium
	// maps it to the same no-interpolation path as pixelated
	// (core/style/computed_style.cc:1326-1337 GetInterpolationQuality:
	// pixelated and crisp-edges both return kInterpolationNone), so the
	// two share the point sampler here.
	ImageRenderingCrispEdges
)

// PointSampled reports whether the rendering mode draws through the
// point sampler (pixelated and crisp-edges; Chromium
// GetInterpolationQuality, computed_style.cc:1326-1337).
func (r ImageRendering) PointSampled() bool {
	return r == ImageRenderingPixelated || r == ImageRenderingCrispEdges
}
