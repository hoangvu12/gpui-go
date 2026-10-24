package gpui

// Tailwind CSS extensions to the pinned Styled vocabulary (ADR 0001).
//
// The pinned GPUI vocabulary (styled.go, styled_generated.go) already
// follows Tailwind's names and scale for layout, spacing, sizing,
// borders, radii, shadows and cursors. These methods add the common
// Tailwind utilities it lacks, so a Tailwind class always has a fluent
// method of the same name: font-bold → FontBold, leading-tight →
// LeadingTight, opacity-50 → Opacity50, uppercase → Uppercase. They are
// gpui-go additions with no counterpart at the pin; each sets the same
// fields its pinned setter (FontWeight, LineHeight, Opacity,
// TextTransform, TextSize, FlexGrow, FlexShrink) would.
//
// Values follow Tailwind CSS v3.4: font weights 100–900, line heights as
// multiples of the font size, text sizes in rems.

// FontThin sets the font weight to 100 (font-thin).
func (s *Styled[Self]) FontThin[_ lazy]() Self { return s.FontWeight(100) }

// FontExtralight sets the font weight to 200 (font-extralight).
func (s *Styled[Self]) FontExtralight[_ lazy]() Self { return s.FontWeight(200) }

// FontLight sets the font weight to 300 (font-light).
func (s *Styled[Self]) FontLight[_ lazy]() Self { return s.FontWeight(300) }

// FontNormal sets the font weight to 400 (font-normal).
func (s *Styled[Self]) FontNormal[_ lazy]() Self { return s.FontWeight(400) }

// FontMedium sets the font weight to 500 (font-medium).
func (s *Styled[Self]) FontMedium[_ lazy]() Self { return s.FontWeight(500) }

// FontSemibold sets the font weight to 600 (font-semibold).
func (s *Styled[Self]) FontSemibold[_ lazy]() Self { return s.FontWeight(600) }

// FontBold sets the font weight to 700 (font-bold).
func (s *Styled[Self]) FontBold[_ lazy]() Self { return s.FontWeight(700) }

// FontExtrabold sets the font weight to 800 (font-extrabold).
func (s *Styled[Self]) FontExtrabold[_ lazy]() Self { return s.FontWeight(800) }

// FontBlack sets the font weight to 900 (font-black).
func (s *Styled[Self]) FontBlack[_ lazy]() Self { return s.FontWeight(900) }

// Uppercase renders text in upper case (uppercase).
func (s *Styled[Self]) Uppercase[_ lazy]() Self { return s.TextTransform(TextTransformUppercase) }

// Lowercase renders text in lower case (lowercase).
func (s *Styled[Self]) Lowercase[_ lazy]() Self { return s.TextTransform(TextTransformLowercase) }

// Capitalize upper-cases the first letter of each word (capitalize).
func (s *Styled[Self]) Capitalize[_ lazy]() Self { return s.TextTransform(TextTransformCapitalize) }

// NormalCase renders text as written (normal-case).
func (s *Styled[Self]) NormalCase[_ lazy]() Self { return s.TextTransform(TextTransformNone) }

// LeadingNone sets the line height to the font size (leading-none).
func (s *Styled[Self]) LeadingNone[_ lazy]() Self { return s.LineHeight(Fraction(1)) }

// LeadingTight sets the line height to 1.25× the font size
// (leading-tight).
func (s *Styled[Self]) LeadingTight[_ lazy]() Self { return s.LineHeight(Fraction(1.25)) }

// LeadingSnug sets the line height to 1.375× the font size
// (leading-snug).
func (s *Styled[Self]) LeadingSnug[_ lazy]() Self { return s.LineHeight(Fraction(1.375)) }

// LeadingNormal sets the line height to 1.5× the font size
// (leading-normal) — TAILWIND's leading-normal, which is 1.5, NOT the
// CSS `normal` keyword (browser-parity D1: the CSS keyword is the
// port's default text style; the Tailwind arbitrary class
// leading-[normal] and NormalLineHeight opt into it).
func (s *Styled[Self]) LeadingNormal[_ lazy]() Self { return s.LineHeight(Fraction(1.5)) }

// LeadingRelaxed sets the line height to 1.625× the font size
// (leading-relaxed).
func (s *Styled[Self]) LeadingRelaxed[_ lazy]() Self { return s.LineHeight(Fraction(1.625)) }

// LeadingLoose sets the line height to twice the font size
// (leading-loose).
func (s *Styled[Self]) LeadingLoose[_ lazy]() Self { return s.LineHeight(Fraction(2)) }

// Text4xl sets the font size to 2.25rem (text-4xl). Like the pinned
// TextXs…Text3xl, it sets only the size.
func (s *Styled[Self]) Text4xl[_ lazy]() Self { return s.TextSize(RemsOf(2.25)) }

// Text5xl sets the font size to 3rem (text-5xl).
func (s *Styled[Self]) Text5xl[_ lazy]() Self { return s.TextSize(RemsOf(3)) }

// Text6xl sets the font size to 3.75rem (text-6xl).
func (s *Styled[Self]) Text6xl[_ lazy]() Self { return s.TextSize(RemsOf(3.75)) }

// Text7xl sets the font size to 4.5rem (text-7xl).
func (s *Styled[Self]) Text7xl[_ lazy]() Self { return s.TextSize(RemsOf(4.5)) }

// Text8xl sets the font size to 6rem (text-8xl).
func (s *Styled[Self]) Text8xl[_ lazy]() Self { return s.TextSize(RemsOf(6)) }

// Text9xl sets the font size to 8rem (text-9xl).
func (s *Styled[Self]) Text9xl[_ lazy]() Self { return s.TextSize(RemsOf(8)) }

// Grow lets the element grow to fill free space (grow; the pinned
// FlexGrow takes the factor).
func (s *Styled[Self]) Grow[_ lazy]() Self { return s.FlexGrow(1) }

// Grow0 stops the element from growing (grow-0).
func (s *Styled[Self]) Grow0[_ lazy]() Self { return s.FlexGrow(0) }

// Shrink lets the element shrink when space runs out (shrink).
func (s *Styled[Self]) Shrink[_ lazy]() Self { return s.FlexShrink(1) }

// Shrink0 stops the element from shrinking (shrink-0; the pinned
// spelling is FlexShrink0).
func (s *Styled[Self]) Shrink0[_ lazy]() Self { return s.FlexShrink(0) }

// Opacity0 makes the element fully transparent (opacity-0).
func (s *Styled[Self]) Opacity0[_ lazy]() Self { return s.Opacity(0) }

// Opacity5 sets the opacity to 5% (opacity-5).
func (s *Styled[Self]) Opacity5[_ lazy]() Self { return s.Opacity(0.05) }

// Opacity10 sets the opacity to 10% (opacity-10).
func (s *Styled[Self]) Opacity10[_ lazy]() Self { return s.Opacity(0.1) }

// Opacity15 sets the opacity to 15% (opacity-15).
func (s *Styled[Self]) Opacity15[_ lazy]() Self { return s.Opacity(0.15) }

// Opacity20 sets the opacity to 20% (opacity-20).
func (s *Styled[Self]) Opacity20[_ lazy]() Self { return s.Opacity(0.2) }

// Opacity25 sets the opacity to 25% (opacity-25).
func (s *Styled[Self]) Opacity25[_ lazy]() Self { return s.Opacity(0.25) }

// Opacity30 sets the opacity to 30% (opacity-30).
func (s *Styled[Self]) Opacity30[_ lazy]() Self { return s.Opacity(0.3) }

// Opacity35 sets the opacity to 35% (opacity-35).
func (s *Styled[Self]) Opacity35[_ lazy]() Self { return s.Opacity(0.35) }

// Opacity40 sets the opacity to 40% (opacity-40).
func (s *Styled[Self]) Opacity40[_ lazy]() Self { return s.Opacity(0.4) }

// Opacity45 sets the opacity to 45% (opacity-45).
func (s *Styled[Self]) Opacity45[_ lazy]() Self { return s.Opacity(0.45) }

// Opacity50 sets the opacity to 50% (opacity-50).
func (s *Styled[Self]) Opacity50[_ lazy]() Self { return s.Opacity(0.5) }

// Opacity55 sets the opacity to 55% (opacity-55).
func (s *Styled[Self]) Opacity55[_ lazy]() Self { return s.Opacity(0.55) }

// Opacity60 sets the opacity to 60% (opacity-60).
func (s *Styled[Self]) Opacity60[_ lazy]() Self { return s.Opacity(0.6) }

// Opacity65 sets the opacity to 65% (opacity-65).
func (s *Styled[Self]) Opacity65[_ lazy]() Self { return s.Opacity(0.65) }

// Opacity70 sets the opacity to 70% (opacity-70).
func (s *Styled[Self]) Opacity70[_ lazy]() Self { return s.Opacity(0.7) }

// Opacity75 sets the opacity to 75% (opacity-75).
func (s *Styled[Self]) Opacity75[_ lazy]() Self { return s.Opacity(0.75) }

// Opacity80 sets the opacity to 80% (opacity-80).
func (s *Styled[Self]) Opacity80[_ lazy]() Self { return s.Opacity(0.8) }

// Opacity85 sets the opacity to 85% (opacity-85).
func (s *Styled[Self]) Opacity85[_ lazy]() Self { return s.Opacity(0.85) }

// Opacity90 sets the opacity to 90% (opacity-90).
func (s *Styled[Self]) Opacity90[_ lazy]() Self { return s.Opacity(0.9) }

// Opacity95 sets the opacity to 95% (opacity-95).
func (s *Styled[Self]) Opacity95[_ lazy]() Self { return s.Opacity(0.95) }

// Opacity100 makes the element fully opaque (opacity-100).
func (s *Styled[Self]) Opacity100[_ lazy]() Self { return s.Opacity(1) }

// ---------------------------------------------------------------------------
// Filters, replaced-content options (browser-core ticket 41)
// ---------------------------------------------------------------------------

// ObjectFill stretches a replaced element's content to fill its content
// box (object-fill).
func (s *Styled[Self]) ObjectFill[_ lazy]() Self {
	s.StyleMut().ObjectFit = Some(ObjectFitFill)
	return s.self
}

// ObjectContain scales a replaced element's content to fit inside its
// content box (object-contain).
func (s *Styled[Self]) ObjectContain[_ lazy]() Self {
	s.StyleMut().ObjectFit = Some(ObjectFitContain)
	return s.self
}

// ObjectCover scales a replaced element's content to cover its content
// box, cropping the overflow (object-cover).
func (s *Styled[Self]) ObjectCover[_ lazy]() Self {
	s.StyleMut().ObjectFit = Some(ObjectFitCover)
	return s.self
}

// ObjectNone keeps a replaced element's content at its intrinsic size,
// positioned by the object position (object-none).
func (s *Styled[Self]) ObjectNone[_ lazy]() Self {
	s.StyleMut().ObjectFit = Some(ObjectFitNone)
	return s.self
}

// ObjectScaleDown scales a replaced element's content down to fit,
// never up (object-scale-down).
func (s *Styled[Self]) ObjectScaleDown[_ lazy]() Self {
	s.StyleMut().ObjectFit = Some(ObjectFitScaleDown)
	return s.self
}

// ObjectBottom positions a replaced element's content at the horizontal
// center, bottom edge (object-bottom).
func (s *Styled[Self]) ObjectBottom[_ lazy]() Self {
	return s.ObjectPosition(ObjectCenter(), ObjectBottom())
}

// ObjectCenter positions a replaced element's content at the center
// (object-center, the CSS default).
func (s *Styled[Self]) ObjectCenter[_ lazy]() Self {
	return s.ObjectPosition(ObjectCenter(), ObjectCenter())
}

// ObjectLeft positions a replaced element's content at the left edge,
// vertical center (object-left).
func (s *Styled[Self]) ObjectLeft[_ lazy]() Self {
	return s.ObjectPosition(ObjectLeft(), ObjectCenter())
}

// ObjectLeftBottom positions a replaced element's content at the bottom
// left corner (object-left-bottom).
func (s *Styled[Self]) ObjectLeftBottom[_ lazy]() Self {
	return s.ObjectPosition(ObjectLeft(), ObjectBottom())
}

// ObjectLeftTop positions a replaced element's content at the top left
// corner (object-left-top).
func (s *Styled[Self]) ObjectLeftTop[_ lazy]() Self {
	return s.ObjectPosition(ObjectLeft(), ObjectTop())
}

// ObjectRight positions a replaced element's content at the right edge,
// vertical center (object-right).
func (s *Styled[Self]) ObjectRight[_ lazy]() Self {
	return s.ObjectPosition(ObjectRight(), ObjectCenter())
}

// ObjectRightBottom positions a replaced element's content at the
// bottom right corner (object-right-bottom).
func (s *Styled[Self]) ObjectRightBottom[_ lazy]() Self {
	return s.ObjectPosition(ObjectRight(), ObjectBottom())
}

// ObjectRightTop positions a replaced element's content at the top
// right corner (object-right-top).
func (s *Styled[Self]) ObjectRightTop[_ lazy]() Self {
	return s.ObjectPosition(ObjectRight(), ObjectTop())
}

// ObjectTop positions a replaced element's content at the horizontal
// center, top edge (object-top).
func (s *Styled[Self]) ObjectTop[_ lazy]() Self {
	return s.ObjectPosition(ObjectCenter(), ObjectTop())
}

// RenderingPixelated draws images through the point sampler — hard
// edges on upscaled pixel art (rendering-pixelated, CSS
// image-rendering: pixelated).
func (s *Styled[Self]) RenderingPixelated[_ lazy]() Self {
	return s.ImageRendering(ImageRenderingPixelated)
}

// RenderingAuto lets the renderer choose the image sampling (the
// bilinear default; rendering-auto, CSS image-rendering: auto).
func (s *Styled[Self]) RenderingAuto[_ lazy]() Self {
	return s.ImageRendering(ImageRenderingAuto)
}
