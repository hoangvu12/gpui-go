package portfixture

// This file implements the glyph-raster-v1 fixture runner: the fx-0005
// envelope executed against the port's real raster stack (gpui's
// TextSystem over the native glyph raster service — the pinned Windows
// DirectWrite rasterizer port running inside the shared Parley/Fontique
// text stack), recording the same trace events the reference harness
// records (reference/harness/src/fixtures/glyph_raster.rs, run_case op
// for op): the construction record (the pinned fallback chain, the
// recommended rendering mode, the subpixel variant counts, the catalog
// generation), then per case the case-begin event, and per (character,
// size, scale, subpixel variant) query one glyph-raster event with the
// pinned outcome: the missing-glyph record (hasGlyph 0) or the raster's
// format, baseline-relative bounds, buffer size, pixel byte count and
// pixel-bytes SHA-256.
//
// The gate (glyph_raster_test.go) compares the port trace against the
// recorded reference trace with CompareTraces: every event must match
// exactly — every format tag, every bounds coordinate, every pixel byte
// count and every pixel hash (bit-exact raster bytes through the whole
// DirectWrite → ABI → Go path).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// The render-mode spellings (the fixture input names).
const (
	glyphModeGrayscale = "grayscale"
	glyphModeSubpixel  = "subpixel"
	glyphModeColor     = "color"
)

// RunGlyphRaster executes the envelope's glyph cases against the real
// gpui raster stack and returns the port-side trace (with the port
// harness identity and no envelope hash: run metadata belongs to the
// caller, which pins the envelope bytes it executed).
func RunGlyphRaster(envelope *conformance.Envelope) (*conformance.Trace, error) {
	if envelope == nil {
		return nil, fmt.Errorf("portfixture: nil envelope")
	}
	if envelope.FixtureKind != conformance.FixtureKindGlyphRaster {
		return nil, fmt.Errorf("portfixture: unsupported fixture kind %q", envelope.FixtureKind)
	}
	inputs := envelope.Inputs.GlyphCases
	if inputs == nil {
		return nil, fmt.Errorf("portfixture: fixture kind glyph-raster-v1 requires inputs.glyph_cases")
	}

	system, err := gpui.DefaultTextSystem()
	if err != nil {
		return nil, fmt.Errorf("portfixture: text system: %w", err)
	}
	rec := &recorder{}
	if err := runRasterSystemBegin(system, inputs.Cases, rec); err != nil {
		return nil, fmt.Errorf("glyph-raster fixture construction failed: %w", err)
	}

	for i := range inputs.Cases {
		if err := runGlyphRasterCase(system, &inputs.Cases[i], rec); err != nil {
			return nil, fmt.Errorf("glyph-raster case %q failed: %w", inputs.Cases[i].Label, err)
		}
	}

	trace := &conformance.Trace{
		Schema:      conformance.TraceSchema,
		FixtureID:   envelope.FixtureID,
		FixtureKind: envelope.FixtureKind,
		Harness: conformance.HarnessInfo{
			HarnessVersion: "0.1.0",
			HarnessCrate:   "gpui-go-portfixture",
			GpuiCrate:      "gpui-ce 0.2.2",
			GpuiCommit:     pinnedGpuiCommit,
			Profile:        "test",
		},
		Events: rec.snapshot(),
	}
	return trace, nil
}

// runRasterSystemBegin records the construction event
// (raster-system-begin), mirroring the reference fixture's run()
// prologue: the pinned fallback chain, the recommended rendering mode,
// the subpixel variant counts and the catalog generation. The reference
// reads font_generation() before resolving anything; the port reads the
// generation through the FIRST case font's identity record (pre-resolved
// here, which also fixes the FontStore interning order to the fixture's
// case order — the same determinism rule the text-geometry runner
// documents). The generation is a registration-batch counter no resolve
// changes, so the recorded value is the pre-resolution generation the
// reference reads.
func runRasterSystemBegin(system *gpui.TextSystem, cases []conformance.GlyphRasterCase, rec *recorder) error {
	recommended, err := system.RecommendedRenderingMode()
	if err != nil {
		return fmt.Errorf("recommended rendering mode: %w", err)
	}
	recommendedName := "PlatformDefault"
	switch recommended {
	case gpui.TextRenderingSubpixel:
		recommendedName = "Subpixel"
	case gpui.TextRenderingGrayscale:
		recommendedName = "Grayscale"
	}
	if len(cases) == 0 {
		return fmt.Errorf("glyph-raster fixture has no cases")
	}
	identity, err := system.ResolveFont(buildRasterFont(cases[0].Font))
	if err != nil {
		return fmt.Errorf("resolving the first case font: %w", err)
	}
	rec.eventWith("raster-system-begin", "raster-system",
		field{"systemFontFamily", textServiceSystemFontFamily},
		field{"fallbackFamilies", textServiceFallbackFamilies},
		field{"recommendedMode", recommendedName},
		field{"subpixelVariantsX", uint64(4)},
		field{"subpixelVariantsY", uint64(1)},
		field{"fontGeneration", uint64(identity.Generation)},
	)
	return nil
}

// renderModeOf decodes the fixture's mode spelling.
func renderModeOf(mode string) (gpui.GlyphRenderMode, error) {
	switch mode {
	case glyphModeGrayscale:
		return gpui.GlyphRenderGrayscale, nil
	case glyphModeSubpixel:
		return gpui.GlyphRenderSubpixel, nil
	case glyphModeColor:
		return gpui.GlyphRenderColor, nil
	default:
		return 0, fmt.Errorf("unknown render mode %q", mode)
	}
}

// buildRasterFont converts the fixture's font input into the port's
// descriptor (family, optional weight and style; the text-geometry
// fixture's shape: absent weight keeps the pinned default 400).
func buildRasterFont(font conformance.TextFontIn) gpui.FontDescriptor {
	descriptor := gpui.FontDescriptor{Family: font.Family, Weight: gpui.DefaultFontWeight}
	if font.Weight != nil {
		descriptor.Weight = float32(*font.Weight)
	}
	if font.Style != "" {
		switch font.Style {
		case "Italic":
			descriptor.Style = gpui.FontStyleItalic
		case "Oblique":
			descriptor.Style = gpui.FontStyleOblique
		}
	}
	return descriptor
}

// runGlyphRasterCase runs one case: resolves the font, then rasters
// every (character, size, scale, subpixel variant) query through the
// public API and records the pinned outcome fields.
func runGlyphRasterCase(system *gpui.TextSystem, c *conformance.GlyphRasterCase, rec *recorder) error {
	mode, err := renderModeOf(c.Mode)
	if err != nil {
		return err
	}

	identity, err := system.ResolveFont(buildRasterFont(c.Font))
	if err != nil {
		return fmt.Errorf("resolving font %q: %w", c.Font.Family, err)
	}
	fontID := identity.FontID

	rec.eventWith("case-begin", c.Label,
		field{"family", c.Font.Family},
		field{"fontId", traceFontID(fontID)},
		field{"mode", c.Mode},
	)

	var sceneColor gpui.Rgba
	if c.SceneColor != nil {
		sceneColor = gpui.Rgba{
			R: float32((*c.SceneColor)[0]),
			G: float32((*c.SceneColor)[1]),
			B: float32((*c.SceneColor)[2]),
			A: float32((*c.SceneColor)[3]),
		}
	} else {
		sceneColor = gpui.Rgba{R: 0, G: 0, B: 0, A: 1}
	}

	// The prepared style fields (recorded once per case, exactly as the
	// reference records them).
	style, err := system.PrepareRasterStyle(sceneColor, mode)
	if err != nil {
		return fmt.Errorf("preparing the raster style: %w", err)
	}
	var styleFields []field
	switch style.Effect {
	case gpui.RasterEffectPreblend:
		styleFields = append(styleFields,
			field{"effectTag", "preblend"},
			field{"effectR", uint64(style.PreblendColor.R)},
			field{"effectG", uint64(style.PreblendColor.G)},
			field{"effectB", uint64(style.PreblendColor.B)},
			field{"effectA", uint64(style.PreblendColor.A)},
		)
	default:
		styleFields = append(styleFields, field{"effectTag", "independent"})
	}

	variants := c.SubpixelVariants
	if len(variants) == 0 {
		variants = [][2]uint64{{0, 0}}
	}

	for _, character := range c.Chars {
		runes := []rune(character)
		if len(runes) != 1 {
			return fmt.Errorf("one character per entry, got %q", character)
		}
		ch := runes[0]

		glyphID, hasGlyph, err := system.GlyphForChar(fontID, ch)
		if err != nil {
			return fmt.Errorf("glyph for char: %w", err)
		}
		if !hasGlyph {
			// The pinned missing-glyph outcome: the reference records
			// hasGlyph 0 and nothing else.
			rec.eventWith("glyph-raster", c.Label,
				field{"char", escapeUnicode(ch)},
				field{"hasGlyph", uint64(0)},
			)
			continue
		}

		for _, size := range c.Sizes {
			for _, scale := range c.Scales {
				for _, variant := range variants {
					subpixelX := uint8(min64(variant[0], 3))
					subpixelY := uint8(min64(variant[1], 0))
					params := gpui.RasterGlyphParams{
						FontID:      fontID,
						GlyphID:     glyphID,
						FontSize:    float32(size),
						SubpixelX:   subpixelX,
						SubpixelY:   subpixelY,
						ScaleFactor: float32(scale),
						RasterStyle: style,
					}

					fields := append([]field{
						{"char", escapeUnicode(ch)},
						{"hasGlyph", uint64(1)},
						{"glyphId", uint64(glyphID)},
						{"fontSize", conformance.F32Bits(float32(size))},
						{"subpixelX", uint64(variant[0])},
						{"subpixelY", uint64(variant[1])},
						{"scale", conformance.F32Bits(float32(scale))},
					}, styleFields...)

					raster, err := system.RasterizeGlyph(params)
					if err != nil {
						// A raster failure is an honest recorded outcome,
						// never a silent skip (the reference records the
						// error kind the same way).
						fields = append(fields,
							field{"outcome", "error"},
							field{"errorKind", "raster-error"},
						)
						rec.eventWith("glyph-raster", c.Label, fields...)
						continue
					}

					fields = append(fields,
						field{"outcome", "ok"},
						field{"format", formatName(raster.Format)},
						field{"boundsX", float64(raster.BoundsX)},
						field{"boundsY", float64(raster.BoundsY)},
						field{"boundsW", float64(raster.BoundsW)},
						field{"boundsH", float64(raster.BoundsH)},
						field{"width", float64(raster.Width)},
						field{"height", float64(raster.Height)},
						field{"pixelBytes", uint64(len(raster.Pixels))},
						field{"pixelsSHA256", sha256Hex(raster.Pixels)},
					)
					rec.eventWith("glyph-raster", c.Label, fields...)
				}
			}
		}
	}

	rec.event("case-end", c.Label)
	return nil
}

// formatName maps the raster format to the recorded spelling.
func formatName(format gpui.RasterFormat) string {
	switch format {
	case gpui.RasterFormatAlphaMask:
		return "alpha-mask"
	case gpui.RasterFormatBgraSubpixelMask:
		return "bgra-subpixel"
	case gpui.RasterFormatBgraColor:
		return "bgra-color"
	default:
		return fmt.Sprintf("unknown-%d", uint32(format))
	}
}

// escapeUnicode mirrors Rust's char::escape_unicode for single scalars
// (\u{XX} with lowercase hex).
func escapeUnicode(ch rune) string {
	return fmt.Sprintf("\\u{%x}", ch)
}

// sha256Hex is the pixel-hash encoding of the trace schema.
func sha256Hex(bytes []byte) string {
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

// min64 is a tiny uint64 min (used for subpixel clamps).
func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
