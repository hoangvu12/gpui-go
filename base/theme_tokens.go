package base

// Semantic design tokens shared by application-owned components
// (U:theme_tokens.rs). These tokens describe visual roles and scales; they
// intentionally do not contain component names such as button, table or
// sidebar.
//
// Go adaptations: Pixels are float32 logical pixels, FontWeight is the
// core's float32 weight, SharedString is string. serde becomes
// encoding/json struct tags with the Rust field names; schemars (JSON
// Schema) is dropped (dependency decision, ticket 01). Hsla, Pixels and
// BoxShadow encode with the core's Go JSON shapes, not the upstream serde
// shapes.

import (
	"encoding/json"
	"runtime"

	"github.com/hoangvu12/gpui-go/gpui"
)

// fontWeightNormal is FontWeight::NORMAL (R:text_system.rs:426).
const fontWeightNormal float32 = 400

// SemanticThemeTokens groups the token scales (U:theme_tokens.rs:12-19).
type SemanticThemeTokens struct {
	Colors     ColorTokens      `json:"colors"`
	Radius     RadiusTokens     `json:"radius"`
	Spacing    SpacingTokens    `json:"spacing"`
	Typography TypographyTokens `json:"typography"`
	Shadow     ShadowTokens     `json:"shadow"`
}

// DefaultSemanticThemeTokens returns the default tokens: the light palette
// and the default scales (U:theme_tokens.rs:12 derive(Default)).
func DefaultSemanticThemeTokens() SemanticThemeTokens {
	return SemanticThemeTokens{
		Colors:     DefaultColorTokens(),
		Radius:     DefaultRadiusTokens(),
		Spacing:    DefaultSpacingTokens(),
		Typography: DefaultTypographyTokens(),
		Shadow:     ShadowTokens{},
	}
}

// Clone returns an independent copy (the derived Clone deep-copies the
// shadow vectors).
func (t SemanticThemeTokens) Clone() SemanticThemeTokens {
	t.Shadow = t.Shadow.Clone()
	return t
}

// ColorTokens are the semantic colors (U:theme_tokens.rs:21-65).
type ColorTokens struct {
	Background            gpui.Hsla `json:"background"`
	Foreground            gpui.Hsla `json:"foreground"`
	Surface               gpui.Hsla `json:"surface"`
	SurfaceForeground     gpui.Hsla `json:"surface_foreground"`
	Primary               gpui.Hsla `json:"primary"`
	PrimaryForeground     gpui.Hsla `json:"primary_foreground"`
	Secondary             gpui.Hsla `json:"secondary"`
	SecondaryForeground   gpui.Hsla `json:"secondary_foreground"`
	Muted                 gpui.Hsla `json:"muted"`
	MutedForeground       gpui.Hsla `json:"muted_foreground"`
	Accent                gpui.Hsla `json:"accent"`
	AccentForeground      gpui.Hsla `json:"accent_foreground"`
	Destructive           gpui.Hsla `json:"destructive"`
	DestructiveForeground gpui.Hsla `json:"destructive_foreground"`
	Border                gpui.Hsla `json:"border"`
	Input                 gpui.Hsla `json:"input"`
	Ring                  gpui.Hsla `json:"ring"`
	// Selection is the background painted behind selected text
	// (U:theme_tokens.rs:57-64): a translucent wash under the glyphs. A
	// palette written before the token existed loads with the default
	// (serde default = ColorTokens::default_selection).
	Selection gpui.Hsla `json:"selection"`
}

// DefaultColorTokens is the light palette (U:theme_tokens.rs:67-71).
func DefaultColorTokens() ColorTokens { return LightColorTokens() }

// hsla is the pinned hsla(h, s, l, a) constructor (hue in [0, 1]).
func hsla(h, s, l, a float32) gpui.Hsla { return gpui.Hsla{H: h, S: s, L: l, A: a} }

// LightColorTokens is the default light palette, aligned with
// gpui-component's Default Light theme (U:theme_tokens.rs:74-96).
func LightColorTokens() ColorTokens {
	return ColorTokens{
		Background:            hsla(0, 0, 1, 1),
		Foreground:            hsla(0, 0, 0.039, 1),
		Surface:               hsla(0, 0, 1, 1),
		SurfaceForeground:     hsla(0, 0, 0.039, 1),
		Primary:               hsla(0, 0, 0.09, 1),
		PrimaryForeground:     hsla(0, 0, 0.98, 1),
		Secondary:             hsla(0, 0, 0.898, 1),
		SecondaryForeground:   hsla(0, 0, 0.09, 1),
		Muted:                 hsla(0, 0, 0.961, 1),
		MutedForeground:       hsla(0, 0, 0.451, 1),
		Accent:                hsla(0, 0, 0.961, 1),
		AccentForeground:      hsla(0, 0, 0.09, 1),
		Destructive:           hsla(0, 0.842, 0.602, 1),
		DestructiveForeground: hsla(0, 0, 0.98, 1),
		Border:                hsla(0, 0, 0.898, 1),
		Input:                 hsla(0, 0, 0.898, 1),
		Ring:                  hsla(0, 0, 0.639, 1),
		// rgb_to_hsla(rgb(0x55a0fc)).with_alpha(0.3)
		Selection: gpui.Rgb(0x55a0fc).Alpha(0.3),
	}
}

// DarkColorTokens is the default dark palette, aligned with
// gpui-component's Default Dark theme (U:theme_tokens.rs:98-120).
func DarkColorTokens() ColorTokens {
	return ColorTokens{
		Background:            hsla(0, 0, 0.039, 1),
		Foreground:            hsla(0, 0, 0.98, 1),
		Surface:               hsla(0, 0, 0.039, 1),
		SurfaceForeground:     hsla(0, 0, 0.98, 1),
		Primary:               hsla(0, 0, 0.98, 1),
		PrimaryForeground:     hsla(0, 0, 0.09, 1),
		Secondary:             hsla(0, 0, 0.149, 1),
		SecondaryForeground:   hsla(0, 0, 0.98, 1),
		Muted:                 hsla(0, 0, 0.149, 1),
		MutedForeground:       hsla(0, 0, 0.639, 1),
		Accent:                hsla(0, 0, 0.149, 1),
		AccentForeground:      hsla(0, 0, 0.98, 1),
		Destructive:           hsla(0, 0.906, 0.708, 1),
		DestructiveForeground: hsla(0, 0.722, 0.506, 1),
		Border:                hsla(0, 0, 0.149, 1),
		Input:                 hsla(0, 0, 47.0/255.0, 1),
		Ring:                  hsla(0, 0, 0.451, 1),
		// rgb_to_hsla(rgb(0x1d4ed8)).with_alpha(0.3)
		Selection: gpui.Rgb(0x1d4ed8).Alpha(0.3),
	}
}

// colorTokensDefaultSelection is the selection color a palette falls back
// to when it predates the token (U:theme_tokens.rs:122-125).
func colorTokensDefaultSelection() gpui.Hsla { return LightColorTokens().Selection }

// UnmarshalJSON decodes the palette, defaulting a missing selection to
// the light palette's (U:theme_tokens.rs:62 #[serde(default =
// "ColorTokens::default_selection")]). Other missing fields decode as the
// zero color (serde reports them as missing; not ported).
func (c *ColorTokens) UnmarshalJSON(data []byte) error {
	type plain ColorTokens
	decoded := plain{Selection: colorTokensDefaultSelection()}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = ColorTokens(decoded)
	return nil
}

// RadiusTokens is the corner radius scale (U:theme_tokens.rs:128-149).
type RadiusTokens struct {
	None float32 `json:"none"`
	Sm   float32 `json:"sm"`
	Md   float32 `json:"md"`
	Lg   float32 `json:"lg"`
	Xl   float32 `json:"xl"`
	Full float32 `json:"full"`
}

// DefaultRadiusTokens returns the default radius scale
// (U:theme_tokens.rs:138-149).
func DefaultRadiusTokens() RadiusTokens {
	return RadiusTokens{None: 0, Sm: 3, Md: 6, Lg: 8, Xl: 12, Full: 9999}
}

// SpacingTokens is the spacing scale (U:theme_tokens.rs:151-174).
type SpacingTokens struct {
	Xxs float32 `json:"xxs"`
	Xs  float32 `json:"xs"`
	Sm  float32 `json:"sm"`
	Md  float32 `json:"md"`
	Lg  float32 `json:"lg"`
	Xl  float32 `json:"xl"`
	Xxl float32 `json:"xxl"`
}

// DefaultSpacingTokens returns the default spacing scale
// (U:theme_tokens.rs:162-174).
func DefaultSpacingTokens() SpacingTokens {
	return SpacingTokens{Xxs: 2, Xs: 4, Sm: 8, Md: 12, Lg: 16, Xl: 24, Xxl: 32}
}

// TextStyleToken is one text style: size, line height and weight
// (U:theme_tokens.rs:176-181).
type TextStyleToken struct {
	Size       float32 `json:"size"`
	LineHeight float32 `json:"line_height"`
	Weight     float32 `json:"weight"`
}

// TypographyTokens are the font families and text styles
// (U:theme_tokens.rs:183-208).
type TypographyTokens struct {
	Sans   string         `json:"sans"`
	Mono   string         `json:"mono"`
	Xs     TextStyleToken `json:"xs"`
	Sm     TextStyleToken `json:"sm"`
	Md     TextStyleToken `json:"md"`
	Lg     TextStyleToken `json:"lg"`
	Xl     TextStyleToken `json:"xl"`
	MonoMd TextStyleToken `json:"mono_md"`
}

// DefaultTypographyTokens returns the default typography
// (U:theme_tokens.rs:195-208).
func DefaultTypographyTokens() TypographyTokens {
	return TypographyTokens{
		Sans:   ".SystemUIFont",
		Mono:   defaultMonoFontFamily(),
		Xs:     textStyle(12, 16),
		Sm:     textStyle(14, 20),
		Md:     textStyle(16, 24),
		Lg:     textStyle(18, 28),
		Xl:     textStyle(20, 28),
		MonoMd: textStyle(13, 20),
	}
}

// ShadowTokens are the elevation shadows (U:theme_tokens.rs:210-218).
type ShadowTokens struct {
	Sm []gpui.BoxShadow `json:"sm"`
	Md []gpui.BoxShadow `json:"md"`
	Lg []gpui.BoxShadow `json:"lg"`
}

// ShadowTokensElevations returns the three elevations in the given color
// (U:theme_tokens.rs:220-228 ShadowTokens::elevations).
func ShadowTokensElevations(color gpui.Hsla) ShadowTokens {
	return ShadowTokens{
		Sm: []gpui.BoxShadow{BoxShadow(0, 1, 2, 0, color)},
		Md: []gpui.BoxShadow{BoxShadow(0, 4, 8, -2, color)},
		Lg: []gpui.BoxShadow{BoxShadow(0, 12, 24, -4, color)},
	}
}

// Clone returns an independent copy of the shadow vectors.
func (s ShadowTokens) Clone() ShadowTokens {
	return ShadowTokens{Sm: cloneShadows(s.Sm), Md: cloneShadows(s.Md), Lg: cloneShadows(s.Lg)}
}

func cloneShadows(s []gpui.BoxShadow) []gpui.BoxShadow {
	if s == nil {
		return nil
	}
	return append([]gpui.BoxShadow(nil), s...)
}

// textStyle builds a normal-weight text style (U:theme_tokens.rs:230-236).
func textStyle(size, lineHeight float32) TextStyleToken {
	return TextStyleToken{Size: size, LineHeight: lineHeight, Weight: fontWeightNormal}
}

// defaultMonoFontFamily picks the platform monospace family
// (U:theme_tokens.rs:238-246).
func defaultMonoFontFamily() string {
	switch runtime.GOOS {
	case "darwin":
		return "Menlo"
	case "windows":
		return "Consolas"
	default:
		return "DejaVu Sans Mono"
	}
}
