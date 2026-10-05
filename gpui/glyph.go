package gpui

// This file is the port's glyph raster half of ticket10: the typed seam
// over the native glyph raster service (reserved slot 5, capability
// "glyph-raster-dwrite-v1"; see internal/native/glyph.go and
// reference/native/GLYPH_ABI.md). The native service is the pinned
// Windows DirectWrite rasterizer ported from
// crates/gpui_windows/src/font_rasterizer.rs at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a, running inside the shared
// Parley/Fontique text stack this package's TextSystem (ticket09) owns,
// so font ids are the same canonical ids shaping produces.
//
// The public surface mirrors the pinned gpui text API the window paint
// path uses: PlatformTextSystem::glyph_for_char, the public
// PlatformTextSystem::prepare_raster_style (the TextSystem wrapper's
// pub(crate) prepare_raster_style forwards to the same call),
// TextSystem::rasterize_glyph (including validate and the raster
// metadata consistency check) and recommended_rendering_mode.

import (
	"fmt"
	"math"

	"gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// Render modes, formats and the scene color type
// ---------------------------------------------------------------------------

// GlyphRenderMode is the kind of glyph image requested from the
// rasterizer (the pinned GlyphRenderMode).
type GlyphRenderMode uint32

const (
	// GlyphRenderGrayscale requests a color-independent, one-channel
	// coverage mask.
	GlyphRenderGrayscale GlyphRenderMode = 0
	// GlyphRenderSubpixel requests a color-independent, three-channel
	// subpixel coverage mask.
	GlyphRenderSubpixel GlyphRenderMode = 1
	// GlyphRenderColor requests a color glyph whose pixels include their
	// final RGB values.
	GlyphRenderColor GlyphRenderMode = 2
)

// RasterFormat is the byte layout supplied by a glyph rasterizer (the
// pinned RasterizedGlyphFormat).
type RasterFormat uint32

const (
	// RasterFormatAlphaMask is one byte of coverage per pixel.
	RasterFormatAlphaMask RasterFormat = 0
	// RasterFormatBgraSubpixelMask is four bytes per pixel in blue,
	// green, red, unused order.
	RasterFormatBgraSubpixelMask RasterFormat = 1
	// RasterFormatBgraColor is four bytes per pixel in blue, green, red,
	// straight-alpha order.
	RasterFormatBgraColor RasterFormat = 2
)

// TextRenderingMode selects how ordinary text is rendered (the pinned
// TextRenderingMode).
type TextRenderingMode uint32

const (
	// TextRenderingPlatformDefault defers to the platform default.
	TextRenderingPlatformDefault TextRenderingMode = 0
	// TextRenderingSubpixel uses subpixel (ClearType-style) rendering.
	TextRenderingSubpixel TextRenderingMode = 1
	// TextRenderingGrayscale uses grayscale rendering.
	TextRenderingGrayscale TextRenderingMode = 2
)

// RasterColorEffectKind is the part of the requested color which changes
// rasterized pixels (the pinned RasterColorEffect).
type RasterColorEffectKind uint32

const (
	// RasterEffectIndependent: pixels do not depend on the scene color.
	RasterEffectIndependent RasterColorEffectKind = 0
	// RasterEffectPreblend: a quantized color baked into coverage or
	// color pixels.
	RasterEffectPreblend RasterColorEffectKind = 1
)

// Rgba is the pinned gpui Rgba: sRGB-encoded f32 channels plus alpha
// (palette's Srgba). It is the RasterStyleRequest scene color.
type Rgba struct {
	R, G, B, A float32
}

// Rgba8 is the eight-bit sRGB color of a raster cache key (the pinned
// Rgba8).
type Rgba8 struct {
	R, G, B, A uint8
}

// PreparedRasterStyle is the cache-relevant raster setting the platform
// rasterizer chose (the pinned PreparedRasterStyle: a mode plus the
// normalized color effect).
type PreparedRasterStyle struct {
	// Mode is the normalized render mode.
	Mode GlyphRenderMode
	// Effect is the normalized color effect.
	Effect RasterColorEffectKind
	// PreblendColor is the quantized color of a Preblend effect (zero
	// otherwise).
	PreblendColor Rgba8
}

// RasterGlyphParams are the pinned RenderGlyphParams: the exact font
// instance, glyph, size, subpixel positioning, scale factor and the
// prepared raster style. They key the glyph cache (and, with the format,
// the atlas).
type RasterGlyphParams struct {
	FontID      FontID
	GlyphID     uint32
	FontSize    float32
	SubpixelX   uint8
	SubpixelY   uint8
	ScaleFactor float32
	RasterStyle PreparedRasterStyle
}

// RasterizedGlyph is a glyph raster ready for insertion into a renderer
// atlas (the pinned RasterizedGlyph: baseline-relative bounds, buffer
// size, format and pixel bytes; the pin carries no advance field — pen
// advances are the shaping positions).
type RasterizedGlyph struct {
	// BoundsX/BoundsY is the placement relative to the glyph's baseline
	// origin in device pixels (the pinned left and -top).
	BoundsX, BoundsY int32
	// BoundsW/BoundsH is the bounds size in device pixels.
	BoundsW, BoundsH int32
	// Width/Height are the pixel buffer dimensions (equal to the bounds
	// size in the pin).
	Width, Height int32
	// Format is the byte layout of Pixels.
	Format RasterFormat
	// Pixels are the raster bytes (1 or 4 per pixel per the format).
	Pixels []byte
}

// RasterBackend is the rasterizer backend selection (the pinned
// WindowsRasterBackend).
type RasterBackend uint32

const (
	// RasterBackendDirectWrite: DirectWrite with the retained Swash
	// per-glyph fallback.
	RasterBackendDirectWrite RasterBackend = 0
	// RasterBackendSwash: Swash only (DirectWrite failed to construct).
	RasterBackendSwash RasterBackend = 1
)

// RasterBackendInfo reports the rasterizer's construction facts: which
// backend was selected (DirectWrite with the Swash per-glyph fallback,
// or Swash only), whether variable axes are supported through
// IDWriteFactory6, the DirectWrite gamma and grayscale enhanced
// contrast, and whether the OS font smoothing is ClearType.
type RasterBackendInfo struct {
	// Backend is the selected backend.
	Backend RasterBackend
	// VariableFactory is true when IDWriteFactory6 was available.
	VariableFactory bool
	// Gamma is the DirectWrite rendering gamma (0 for Swash).
	Gamma float32
	// GrayscaleEnhancedContrast is the DirectWrite contrast (0 for
	// Swash).
	GrayscaleEnhancedContrast float32
	// SystemSubpixelRendering is true when the OS uses ClearType.
	SystemSubpixelRendering bool
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrGlyphService reports a failure to load/validate the native glyph
	// service.
	ErrGlyphService = ErrTextService
	// ErrGlyphValue reports a raster request the adapter rejected.
	ErrGlyphValue = ErrTextValue
	// ErrGlyphRaster reports a pinned rasterization failure.
	ErrGlyphRaster = errGlyphRaster
)

var errGlyphRaster = fmt.Errorf("gpui: glyph rasterization failed")

// wrapGlyphError maps a native glyph error to the gpui seam's context.
func wrapGlyphError(op string, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case isNativeError(err, native.ErrGlyphBadHandle):
		return fmt.Errorf("gpui: %s: %w", op, ErrTextBadHandle)
	case isNativeError(err, native.ErrGlyphBadValue):
		return fmt.Errorf("gpui: %s: %w", op, ErrGlyphValue)
	case isNativeError(err, native.ErrGlyphRaster):
		return fmt.Errorf("gpui: %s: %w", op, ErrGlyphRaster)
	case isNativeError(err, native.ErrGlyphCapacity):
		return fmt.Errorf("gpui: %s: internal capacity retry failed: %w", op, err)
	default:
		return fmt.Errorf("gpui: %s: %w", op, err)
	}
}

// isNativeError reports whether err wraps the sentinel through the
// native status error chain.
func isNativeError(err error, sentinel error) bool {
	for err != nil {
		if err == sentinel {
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}

// glyphService lazily loads the process-global glyph service handle.
var (
	glyphServiceOnce func() (*native.GlyphService, error)
)

func init() {
	loaded := false
	var svc *native.GlyphService
	var loadErr error
	glyphServiceOnce = func() (*native.GlyphService, error) {
		if !loaded {
			loaded = true
			lib, err := native.Load(native.Options{})
			if err != nil {
				loadErr = fmt.Errorf("gpui: %w: loading the native artifact: %w", ErrGlyphService, err)
				return nil, loadErr
			}
			svc, err = lib.Glyph()
			if err != nil {
				loadErr = fmt.Errorf("gpui: %w: %w", ErrGlyphService, err)
				return nil, loadErr
			}
		}
		return svc, loadErr
	}
}

// ---------------------------------------------------------------------------
// TextSystem raster methods (ticket10)
// ---------------------------------------------------------------------------

// GlyphForChar maps a character to its nominal glyph id in the resolved
// face (the pinned PlatformTextSystem::glyph_for_char). The second
// result is false when the face has no mapping (the missing-glyph
// outcome).
func (ts *TextSystem) GlyphForChar(fontID FontID, ch rune) (uint32, bool, error) {
	glyph, err := glyphServiceOnce()
	if err != nil {
		return 0, false, err
	}
	glyphID, ok, err := glyph.GlyphForChar(native.FontID(fontID), ch)
	if err != nil {
		return 0, false, wrapGlyphError("GlyphForChar", err)
	}
	return glyphID, ok, nil
}

// PrepareRasterStyle normalizes a scene color and render mode into the
// cache-relevant raster settings (the pinned
// PlatformTextSystem::prepare_raster_style: color mode quantizes the
// scene color into a Preblend effect; other modes stay
// color-independent).
func (ts *TextSystem) PrepareRasterStyle(color Rgba, mode GlyphRenderMode) (PreparedRasterStyle, error) {
	glyph, err := glyphServiceOnce()
	if err != nil {
		return PreparedRasterStyle{}, err
	}
	record, err := glyph.PrepareRasterStyle(color.R, color.G, color.B, color.A, native.GlyphRenderMode(mode))
	if err != nil {
		return PreparedRasterStyle{}, wrapGlyphError("PrepareRasterStyle", err)
	}
	style := PreparedRasterStyle{
		Mode:   GlyphRenderMode(record.Mode),
		Effect: RasterColorEffectKind(record.EffectTag),
	}
	if record.EffectTag == uint32(RasterEffectPreblend) {
		style.PreblendColor = Rgba8{
			R: uint8(record.ColorR),
			G: uint8(record.ColorG),
			B: uint8(record.ColorB),
			A: uint8(record.ColorA),
		}
	}
	return style, nil
}

// RasterizeGlyph rasters one glyph with the pinned parameters (the
// public TextSystem::rasterize_glyph call the window paint path makes,
// including validate and the raster metadata consistency check). The
// result carries the baseline-relative bounds, the buffer size, the
// format and the pixel bytes.
func (ts *TextSystem) RasterizeGlyph(params RasterGlyphParams) (*RasterizedGlyph, error) {
	if params.FontSize < 0 || !finiteFloat32(params.FontSize) {
		return nil, fmt.Errorf("gpui: RasterizeGlyph: %w: font size must be finite and >= 0", ErrGlyphValue)
	}
	if params.ScaleFactor <= 0 || !finiteFloat32(params.ScaleFactor) {
		return nil, fmt.Errorf("gpui: RasterizeGlyph: %w: scale must be finite and > 0 (the pin's own validation)", ErrGlyphValue)
	}
	glyph, err := glyphServiceOnce()
	if err != nil {
		return nil, err
	}
	style := native.NewRasterStyleRecord()
	style.Mode = uint32(params.RasterStyle.Mode)
	style.EffectTag = uint32(params.RasterStyle.Effect)
	style.ColorR = uint32(params.RasterStyle.PreblendColor.R)
	style.ColorG = uint32(params.RasterStyle.PreblendColor.G)
	style.ColorB = uint32(params.RasterStyle.PreblendColor.B)
	style.ColorA = uint32(params.RasterStyle.PreblendColor.A)
	record, pixels, err := glyph.RasterizeGlyph(native.RasterParams{
		FontID:    native.FontID(params.FontID),
		GlyphID:   params.GlyphID,
		FontSize:  params.FontSize,
		SubpixelX: uint32(params.SubpixelX),
		SubpixelY: uint32(params.SubpixelY),
		Scale:     params.ScaleFactor,
		Style:     style,
	})
	if err != nil {
		return nil, wrapGlyphError("RasterizeGlyph", err)
	}
	return &RasterizedGlyph{
		BoundsX: record.BoundsX,
		BoundsY: record.BoundsY,
		BoundsW: record.BoundsW,
		BoundsH: record.BoundsH,
		Width:   record.Width,
		Height:  record.Height,
		Format:  RasterFormat(record.Format),
		Pixels:  pixels,
	}, nil
}

// RecommendedRenderingMode returns the platform's recommended mode for
// ordinary text (the pinned recommended_rendering_mode; never
// PlatformDefault on Windows: ClearType on means Subpixel, else
// Grayscale).
func (ts *TextSystem) RecommendedRenderingMode() (TextRenderingMode, error) {
	glyph, err := glyphServiceOnce()
	if err != nil {
		return 0, err
	}
	mode, err := glyph.RecommendedRenderingMode()
	if err != nil {
		return 0, wrapGlyphError("RecommendedRenderingMode", err)
	}
	return TextRenderingMode(mode), nil
}

// RasterBackend reports the native rasterizer's construction facts (the
// backend selection, the DirectWrite gamma/contrast parameters and the
// OS subpixel setting). The port records them the way the pin keeps them
// privately in ColorRenderingParams.
func (ts *TextSystem) RasterBackend() (RasterBackendInfo, error) {
	glyph, err := glyphServiceOnce()
	if err != nil {
		return RasterBackendInfo{}, err
	}
	info, err := glyph.BackendInfo()
	if err != nil {
		return RasterBackendInfo{}, wrapGlyphError("RasterBackend", err)
	}
	return RasterBackendInfo{
		Backend:                   RasterBackend(info.Backend),
		VariableFactory:           info.VariableFactory != 0,
		Gamma:                     math.Float32frombits(info.GammaBits),
		GrayscaleEnhancedContrast: math.Float32frombits(info.ContrastBits),
		SystemSubpixelRendering:   info.SystemSubpixel != 0,
	}, nil
}
