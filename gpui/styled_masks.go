package gpui

// The CSS mask, clip-path, blend-mode and isolation fluent setters
// (browser-core ticket 57) — each writes the style property its
// effect-group scope consumes at paint (mask_styles.go).

// MaskImage sets the element's mask (CSS mask-image: a gradient mask
// composites the element's whole rendered image dst_in against the
// gradient's alpha or luminance). Nil (the method's zero spelling)
// masks nothing.
func (s *Styled[Self]) MaskImage[_ lazy](mask *MaskImage) Self {
	s.StyleMut().MaskImage = Some(mask)
	return s.self
}

// ClipPath sets the element's clip-path (CSS clip-path: a basic shape
// in element-local coordinates — circle, ellipse, inset, polygon or a
// tessellated path; the element's rendering and its hit testing clip
// to the shape).
func (s *Styled[Self]) ClipPath[_ lazy](clip ClipPath) Self {
	s.StyleMut().ClipPath = Some(&clip)
	return s.self
}

// MixBlendMode sets the element's compositing blend mode (CSS
// mix-blend-mode: the element's rendered image composites with the mode
// against its backdrop). BlendMultiply, BlendPlusLighter and the rest
// of the W3C compositing-1 modes.
func (s *Styled[Self]) MixBlendMode[_ lazy](mode BlendMode) Self {
	s.StyleMut().MixBlendMode = Some(mode)
	return s.self
}

// Isolate sets isolation: isolate (CSS isolation): the element's
// subtree becomes an isolated group — descendant blend modes composite
// within it, never past it.
func (s *Styled[Self]) Isolate[_ lazy]() Self {
	s.StyleMut().Isolation = Some(true)
	return s.self
}

// NoIsolate resets isolation to auto (CSS isolation: auto, the
// default).
func (s *Styled[Self]) NoIsolate[_ lazy]() Self {
	s.StyleMut().Isolation = Some(false)
	return s.self
}
