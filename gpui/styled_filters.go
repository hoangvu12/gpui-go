package gpui

import "slices"

// The CSS filter functions and replaced-content options as fluent
// Styled methods (browser-core ticket 41). The pinned vocabulary has
// Blur/Filter/BackdropBlur/BackdropFilter (styled.rs:55-91); these add
// the Filter Effects shorthand colour functions — each appends to the
// element's filter chain exactly like Blur does, so
// .Grayscale(1).Blur(4) is the CSS filter: grayscale(1) blur(4) — plus
// the object-position and image-rendering style properties the img
// element consumes (object-fit keeps its pinned element option;
// the style property overrides it when set).

// Grayscale appends grayscale(amount) to the filter chain (CSS filter:
// grayscale; see the Grayscale constructor for the amount's range).
func (s *Styled[Self]) Grayscale[_ lazy](amount float32) Self {
	return s.appendFilter(Grayscale(amount))
}

// Sepia appends sepia(amount) to the filter chain (CSS filter: sepia).
func (s *Styled[Self]) Sepia[_ lazy](amount float32) Self {
	return s.appendFilter(Sepia(amount))
}

// Saturate appends saturate(amount) to the filter chain (CSS filter:
// saturate).
func (s *Styled[Self]) Saturate[_ lazy](amount float32) Self {
	return s.appendFilter(Saturate(amount))
}

// HueRotate appends hue-rotate(degrees) to the filter chain (CSS
// filter: hue-rotate).
func (s *Styled[Self]) HueRotate[_ lazy](degrees float32) Self {
	return s.appendFilter(HueRotate(degrees))
}

// Invert appends invert(amount) to the filter chain (CSS filter:
// invert).
func (s *Styled[Self]) Invert[_ lazy](amount float32) Self {
	return s.appendFilter(Invert(amount))
}

// OpacityFilter appends the filter function opacity(amount) to the
// filter chain (CSS filter: opacity — the function inside filter, not
// the opacity property that Styled.Opacity sets).
func (s *Styled[Self]) OpacityFilter[_ lazy](amount float32) Self {
	return s.appendFilter(OpacityFilter(amount))
}

// Brightness appends brightness(amount) to the filter chain (CSS
// filter: brightness).
func (s *Styled[Self]) Brightness[_ lazy](amount float32) Self {
	return s.appendFilter(Brightness(amount))
}

// Contrast appends contrast(amount) to the filter chain (CSS filter:
// contrast).
func (s *Styled[Self]) Contrast[_ lazy](amount float32) Self {
	return s.appendFilter(Contrast(amount))
}

// appendFilter appends one filter to the element's filter chain
// (Styled.Blur's shape: a clone, never an alias).
func (s *Styled[Self]) appendFilter(f Filter) Self {
	st := s.StyleMut()
	chain, _ := st.Filter.Get()
	st.Filter = Some(append(slices.Clip(slices.Clone(chain)), f))
	return s.self
}

// ObjectPosition sets how the fitted content of a replaced element (an
// img) is positioned inside its content box (CSS object-position, the
// two-value form: an x and a y axis — keywords, percentages or lengths
// through ObjectLeft/ObjectPercent/ObjectPx and friends; the default is
// 50% 50%). Inert on elements that are not replaced, as in CSS.
func (s *Styled[Self]) ObjectPosition[_ lazy](x, y ObjectPositionAxis) Self {
	s.StyleMut().ObjectPosition = Some(ObjectPosition{X: x, Y: y})
	return s.self
}

// ImageRendering sets the image sampling quality (CSS
// image-rendering: auto, pixelated or crisp-edges — pixelated and
// crisp-edges draw through the point sampler). Inert on elements that
// are not replaced, as in CSS. The object-fit style property (CSS
// object-fit) has no general method: its whole vocabulary is the five
// Tailwind-named methods ObjectFill/ObjectContain/ObjectCover/
// ObjectNone/ObjectScaleDown (styled_tailwind.go), which override a
// replaced element's own fit option when set; the pinned img element
// keeps its ObjectFit element option.
func (s *Styled[Self]) ImageRendering[_ lazy](rendering ImageRendering) Self {
	s.StyleMut().ImageRendering = Some(rendering)
	return s.self
}
