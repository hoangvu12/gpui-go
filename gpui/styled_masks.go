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

// The Tailwind mix-blend-* methods (browser-core ticket 57): each
// zero-argument method is its class (mix-blend-multiply →
// MixBlendMultiply), the naming rule of docs/tailwind.md.

// MixBlendNormal sets mix-blend-mode: normal (source-over, the default).
func (s *Styled[Self]) MixBlendNormal[_ lazy]() Self { return s.MixBlendMode(BlendNormal) }

// MixBlendMultiply sets mix-blend-mode: multiply.
func (s *Styled[Self]) MixBlendMultiply[_ lazy]() Self { return s.MixBlendMode(BlendMultiply) }

// MixBlendScreen sets mix-blend-mode: screen.
func (s *Styled[Self]) MixBlendScreen[_ lazy]() Self { return s.MixBlendMode(BlendScreen) }

// MixBlendOverlay sets mix-blend-mode: overlay.
func (s *Styled[Self]) MixBlendOverlay[_ lazy]() Self { return s.MixBlendMode(BlendOverlay) }

// MixBlendDarken sets mix-blend-mode: darken.
func (s *Styled[Self]) MixBlendDarken[_ lazy]() Self { return s.MixBlendMode(BlendDarken) }

// MixBlendLighten sets mix-blend-mode: lighten.
func (s *Styled[Self]) MixBlendLighten[_ lazy]() Self { return s.MixBlendMode(BlendLighten) }

// MixBlendColorDodge sets mix-blend-mode: color-dodge.
func (s *Styled[Self]) MixBlendColorDodge[_ lazy]() Self { return s.MixBlendMode(BlendColorDodge) }

// MixBlendColorBurn sets mix-blend-mode: color-burn.
func (s *Styled[Self]) MixBlendColorBurn[_ lazy]() Self { return s.MixBlendMode(BlendColorBurn) }

// MixBlendHardLight sets mix-blend-mode: hard-light.
func (s *Styled[Self]) MixBlendHardLight[_ lazy]() Self { return s.MixBlendMode(BlendHardLight) }

// MixBlendSoftLight sets mix-blend-mode: soft-light.
func (s *Styled[Self]) MixBlendSoftLight[_ lazy]() Self { return s.MixBlendMode(BlendSoftLight) }

// MixBlendDifference sets mix-blend-mode: difference.
func (s *Styled[Self]) MixBlendDifference[_ lazy]() Self { return s.MixBlendMode(BlendDifference) }

// MixBlendExclusion sets mix-blend-mode: exclusion.
func (s *Styled[Self]) MixBlendExclusion[_ lazy]() Self { return s.MixBlendMode(BlendExclusion) }

// MixBlendHue sets mix-blend-mode: hue.
func (s *Styled[Self]) MixBlendHue[_ lazy]() Self { return s.MixBlendMode(BlendHue) }

// MixBlendSaturation sets mix-blend-mode: saturation.
func (s *Styled[Self]) MixBlendSaturation[_ lazy]() Self { return s.MixBlendMode(BlendSaturation) }

// MixBlendColor sets mix-blend-mode: color.
func (s *Styled[Self]) MixBlendColor[_ lazy]() Self { return s.MixBlendMode(BlendColor) }

// MixBlendLuminosity sets mix-blend-mode: luminosity.
func (s *Styled[Self]) MixBlendLuminosity[_ lazy]() Self { return s.MixBlendMode(BlendLuminosity) }

// MixBlendPlusLighter sets mix-blend-mode: plus-lighter.
func (s *Styled[Self]) MixBlendPlusLighter[_ lazy]() Self { return s.MixBlendMode(BlendPlusLighter) }
