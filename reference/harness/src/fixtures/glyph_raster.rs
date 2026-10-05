//! Fixture `glyph-raster-v1`: reference glyph rasterization through the
//! pinned public raster API with the pinned Windows DirectWrite
//! rasterizer.
//!
//! ## The public oracle boundary
//!
//! The reference raster surface is observable through gpui's PUBLIC text
//! API exactly as the pinned Windows platform constructs it:
//!
//! * **Construction** — `gpui_ce_parley::ParleyTextSystem::
//!   new_with_rasterizer(SystemFonts::Load, "Segoe UI",
//!   WindowsGlyphRasterizer::new()).with_fallback_families(["Lilex",
//!   "IBM Plex Sans", "Arial"])` wrapped in
//!   `gpui::TextSystem::new(Arc<dyn PlatformTextSystem>)`. This mirrors
//!   `crates/gpui_windows/src/platform.rs::WindowsPlatform::new` (lines
//!   117-126 at the pin) exactly — for this fixture the pinned
//!   rasterizer IS constructible because this harness contains an
//!   independent port of it (below): `WindowsGlyphRasterizer` is
//!   `pub(crate)` in `gpui_windows`, so neither the harness nor the
//!   gpui-go native artifact can link it directly. The port is
//!   line-by-line from `crates/gpui_windows/src/font_rasterizer.rs` at
//!   `254b5dbd…` (the same source the native glyph service ports; this
//!   independent compilation is the conformance contract's independent
//!   oracle).
//! * **Rasterization** — the public `gpui::TextSystem::rasterize_glyph(
//!   &RenderGlyphParams)` (text_system.rs:239): the same call the pinned
//!   window paint path makes (`window.rs::paint_glyph_from_atlas`),
//!   including its `validate()` and the raster-metadata consistency
//!   check.
//! * **Style preparation** — `PlatformTextSystem::prepare_raster_style(
//!   RasterStyleRequest { scene_color, requested_mode })` (the public
//!   trait method the pub(crate) `TextSystem::prepare_raster_style`
//!   forwards to).
//! * **Glyph lookup** — `PlatformTextSystem::glyph_for_char` (the public
//!   trait method; `None` is the pinned missing-glyph outcome).
//! * **Recommendation** — `PlatformTextSystem::recommended_rendering_mode`
//!   (the platform's default mode; never `PlatformDefault`).
//!
//! ## API discoveries recorded by this fixture
//!
//! * **`RasterizedGlyph` has no advance field** — the pinned output is
//!   `bounds`, `size`, `format` and `pixels` (text_system.rs:763-772).
//!   The ticket sketch's "advance" is a shaping concept (pen advances
//!   are the shaped glyph positions of the text-geometry fixture); this
//!   fixture records the raster's own geometry: bounds origin/size,
//!   buffer size and pixel bytes.
//! * **Bounds are baseline-relative device pixels** — origin
//!   `(left, -top)`: y is negative above the baseline (the DirectWrite
//!   alpha texture bounds convention, `font_rasterizer.rs::convert_bounds`
//!   lines 717-738).
//! * **`PreparedRasterStyle`** — the color-independent modes carry the
//!   `Independent` effect; color mode carries `Preblend(Rgba8)` (the
//!   quantized scene color). The record fields mirror both.
//! * **Unsupported color behavior is preserved** — the fixture never
//!   claims COLRv1 support from the raster library: an emoji case
//!   records whatever the pinned fallback routing actually produces
//!   (format, bounds, hash), and the construction record carries the
//!   backend facts (gamma, enhanced contrast, the OS subpixel setting)
//!   so machine drift is detectable.
//!
//! Determinism: the text system is constructed once per harness process
//! with Fontique's real DirectWrite-backed system enumeration; the
//! FontStore interns faces in resolution order, so FontIds are stable
//! for a fixed case order on one machine. DirectWrite rasterization is
//! deterministic for fixed inputs on one machine (three consecutive runs
//! of this fixture must produce byte-identical traces; the recorded
//! pixel hashes pin that).

use crate::envelope::{Envelope, FontDescriptorIn, GlyphRasterCase};
use crate::trace::{FieldValue, TraceRecorder};
use anyhow::{Context as _, Result, bail};
use gpui::{
    Bounds, DevicePixels, Font, FontId, GlyphRenderMode, PlatformTextSystem,
    PreparedRasterStyle, RasterColorEffect, RasterColorExt, RasterStyleRequest, RasterizedGlyph,
    RasterizedGlyphFormat, RenderGlyphParams, Rgba, SUBPIXEL_VARIANTS_X, SUBPIXEL_VARIANTS_Y,
    TextSystem, px, size,
};
use gpui_ce_parley::{ColorGlyphKind, GlyphRasterizer, ParleyTextSystem, RasterFace,
    SwashGlyphRasterizer, SystemFonts};
use std::collections::HashMap;
use std::error::Error;
use std::fmt;
use std::mem::ManuallyDrop;
use std::sync::Arc;
use windows::{
    Win32::{
        Foundation::RECT,
        Graphics::DirectWrite::*,
        UI::WindowsAndMessaging::{
            FE_FONTSMOOTHINGCLEARTYPE, SPI_GETFONTSMOOTHING, SPI_GETFONTSMOOTHINGTYPE,
            SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS, SystemParametersInfoW,
        },
    },
    core::{BOOL, Interface},
};
use windows_numerics::Vector2;

/// The pinned service fallback families.
const SERVICE_FALLBACKS: [&str; 3] = ["Lilex", "IBM Plex Sans", "Arial"];
/// The pinned system font family argument.
const SERVICE_SYSTEM_FONT_FAMILY: &str = "Segoe UI";

/// Render-mode names, matching the fixture input spelling.
const MODE_GRAYSCALE: &str = "grayscale";
const MODE_SUBPIXEL: &str = "subpixel";
const MODE_COLOR: &str = "color";

// ---------------------------------------------------------------------------
// The pinned Windows rasterizer, ported from
// crates/gpui_windows/src/font_rasterizer.rs at 254b5dbd… (the independent
// oracle copy; the native glyph service ports the same source).
// ---------------------------------------------------------------------------

/// Uses DirectWrite where its required interfaces are available and
/// retains the old OS range through the portable rasterizer otherwise
/// (font_rasterizer.rs:28-42).
pub struct WindowsGlyphRasterizer {
    backend: WindowsRasterBackend,
    system_subpixel_rendering: bool,
}

enum WindowsRasterBackend {
    DirectWrite {
        rasterizer: DirectWriteGlyphRasterizer,
        fallback: SwashGlyphRasterizer,
    },
    Swash(SwashGlyphRasterizer),
}

/// The typed unsupported cases (font_rasterizer.rs:44-65).
#[derive(Debug)]
enum NativeRasterUnsupported {
    VariableAxesOnLegacyDirectWrite,
    BitmapColorGlyph,
    ColrV1Glyph,
}

impl fmt::Display for NativeRasterUnsupported {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::VariableAxesOnLegacyDirectWrite => {
                formatter.write_str("this DirectWrite version cannot instantiate variable axes")
            }
            Self::BitmapColorGlyph => formatter
                .write_str("the DirectWrite layer rasterizer does not handle bitmap glyphs"),
            Self::ColrV1Glyph => {
                formatter.write_str("the DirectWrite layer rasterizer does not handle COLRv1")
            }
        }
    }
}

impl Error for NativeRasterUnsupported {}

impl WindowsGlyphRasterizer {
    /// The pinned constructor (font_rasterizer.rs:67-85).
    pub fn new() -> Self {
        let backend = match DirectWriteGlyphRasterizer::new() {
            Ok(rasterizer) => WindowsRasterBackend::DirectWrite {
                rasterizer,
                fallback: SwashGlyphRasterizer::default(),
            },
            Err(_error) => WindowsRasterBackend::Swash(SwashGlyphRasterizer::default()),
        };

        Self {
            backend,
            system_subpixel_rendering: get_system_subpixel_rendering(),
        }
    }
}

impl Default for WindowsGlyphRasterizer {
    fn default() -> Self {
        Self::new()
    }
}

impl GlyphRasterizer for WindowsGlyphRasterizer {
    fn supports_color_glyph(&self, kind: ColorGlyphKind) -> bool {
        matches!(kind, ColorGlyphKind::ColrV0 | ColorGlyphKind::Bitmap)
    }

    fn prepare_style(&self, request: RasterStyleRequest) -> PreparedRasterStyle {
        match &self.backend {
            WindowsRasterBackend::DirectWrite { rasterizer, .. } => {
                rasterizer.prepare_style(request)
            }
            WindowsRasterBackend::Swash(rasterizer) => rasterizer.prepare_style(request),
        }
    }

    fn rasterize(
        &mut self,
        face: RasterFace<'_>,
        params: &RenderGlyphParams,
    ) -> anyhow::Result<RasterizedGlyph> {
        match &mut self.backend {
            WindowsRasterBackend::DirectWrite {
                rasterizer,
                fallback,
            } => match rasterizer.rasterize(face, params) {
                Ok(glyph) => Ok(glyph),
                Err(error) if error.downcast_ref::<NativeRasterUnsupported>().is_some() => {
                    fallback.rasterize(face, params)
                }
                Err(error) => Err(error),
            },
            WindowsRasterBackend::Swash(rasterizer) => rasterizer.rasterize(face, params),
        }
    }

    fn recommended_mode(&self) -> gpui::TextRenderingMode {
        if self.system_subpixel_rendering {
            gpui::TextRenderingMode::Subpixel
        } else {
            gpui::TextRenderingMode::Grayscale
        }
    }
}

/// DirectWrite rasterization for the exact face and instance selected by
/// Parley (font_rasterizer.rs:130-157).
pub struct DirectWriteGlyphRasterizer {
    factory: IDWriteFactory5,
    variable_factory: Option<IDWriteFactory6>,
    in_memory_loader: IDWriteInMemoryFontFileLoader,
    rendering_params: IDWriteRenderingParams,
    faces: HashMap<gpui::FontId, NativeFace>,
    color_rendering: ColorRenderingParams,
    system_subpixel_rendering: bool,
}

struct NativeFace {
    face: IDWriteFontFace3,
    _data: Box<[u8]>,
}

struct GlyphAnalysis {
    analysis: IDWriteGlyphRunAnalysis,
    bounds: RECT,
    texture_type: DWRITE_TEXTURE_TYPE,
}

struct ColorRenderingParams {
    gamma_ratios: [f32; 4],
    grayscale_enhanced_contrast: f32,
}

#[derive(Clone, Copy)]
struct LayerColor {
    red: f32,
    green: f32,
    blue: f32,
    alpha: f32,
}

impl DirectWriteGlyphRasterizer {
    /// The pinned constructor (font_rasterizer.rs:167-197).
    pub fn new() -> anyhow::Result<Self> {
        let factory: IDWriteFactory5 = unsafe { DWriteCreateFactory(DWRITE_FACTORY_TYPE_SHARED) }
            .context("creating the DirectWrite factory")?;
        let variable_factory = factory.cast().ok();
        let in_memory_loader = unsafe { factory.CreateInMemoryFontFileLoader() }
            .context("creating the DirectWrite in-memory font loader")?;
        unsafe { factory.RegisterFontFileLoader(&in_memory_loader) }
            .context("registering the DirectWrite in-memory font loader")?;
        let rendering_params = unsafe { factory.CreateRenderingParams() }
            .context("reading DirectWrite rendering parameters")?;
        let grayscale_rendering_params: IDWriteRenderingParams1 = rendering_params
            .cast()
            .context("reading DirectWrite grayscale rendering parameters")?;
        let color_rendering = ColorRenderingParams {
            gamma_ratios: gpui::get_gamma_correction_ratios(unsafe {
                grayscale_rendering_params.GetGamma()
            }),
            grayscale_enhanced_contrast: unsafe {
                grayscale_rendering_params.GetGrayscaleEnhancedContrast()
            },
        };

        Ok(Self {
            factory,
            variable_factory,
            in_memory_loader,
            rendering_params,
            faces: HashMap::default(),
            color_rendering,
            system_subpixel_rendering: get_system_subpixel_rendering(),
        })
    }

    /// The pinned face cache (font_rasterizer.rs:200-224).
    fn native_face(&mut self, face: &RasterFace<'_>) -> anyhow::Result<IDWriteFontFace3> {
        if !face.variations.is_empty() && self.variable_factory.is_none() {
            return Err(NativeRasterUnsupported::VariableAxesOnLegacyDirectWrite.into());
        }

        match self.faces.entry(face.font_id) {
            std::collections::hash_map::Entry::Occupied(entry) => Ok(entry.get().face.clone()),
            std::collections::hash_map::Entry::Vacant(entry) => {
                let native = NativeFace::new(
                    &self.factory,
                    self.variable_factory.as_ref(),
                    &self.in_memory_loader,
                    face,
                )
                .with_context(|| {
                    format!(
                        "DirectWrite could not create FontId {:?}, face index {}, variations {:?}",
                        face.font_id, face.face_index, face.variations
                    )
                })?;

                Ok(entry.insert(native).face.clone())
            }
        }
    }

    /// The pinned glyph-run analysis (font_rasterizer.rs:226-303).
    fn create_glyph_analysis(
        &self,
        font_face: &IDWriteFontFace3,
        params: &RenderGlyphParams,
        mode: GlyphRenderMode,
    ) -> anyhow::Result<GlyphAnalysis> {
        let glyph_id =
            [u16::try_from(params.glyph_id.0).context("DirectWrite glyph IDs are 16-bit")?];
        let advances = [0.0];
        let offsets = [DWRITE_GLYPH_OFFSET::default()];
        let base_face: IDWriteFontFace = font_face.cast()?;
        let glyph_run = DWRITE_GLYPH_RUN {
            fontFace: ManuallyDrop::new(Some(unsafe { std::ptr::read(&base_face) })),
            fontEmSize: f32::from(params.font_size),
            glyphCount: 1,
            glyphIndices: glyph_id.as_ptr(),
            glyphAdvances: advances.as_ptr(),
            glyphOffsets: offsets.as_ptr(),
            isSideways: BOOL(0),
            bidiLevel: 0,
        };

        let transform = raster_transform(params.scale_factor);
        let baseline = baseline_origin(params);
        let mut rendering_mode = DWRITE_RENDERING_MODE1::default();
        let mut grid_fit_mode = DWRITE_GRID_FIT_MODE::default();
        unsafe {
            font_face.GetRecommendedRenderingMode(
                f32::from(params.font_size),
                96.0,
                96.0,
                Some(&transform),
                false,
                DWRITE_OUTLINE_THRESHOLD_ANTIALIASED,
                DWRITE_MEASURING_MODE_NATURAL,
                &self.rendering_params,
                &mut rendering_mode,
                &mut grid_fit_mode,
            )?;
        }

        if rendering_mode == DWRITE_RENDERING_MODE1_OUTLINE {
            rendering_mode = DWRITE_RENDERING_MODE1_NATURAL_SYMMETRIC;
        }

        let (antialias_mode, texture_type) = if mode == GlyphRenderMode::Subpixel {
            (
                DWRITE_TEXT_ANTIALIAS_MODE_CLEARTYPE,
                DWRITE_TEXTURE_CLEARTYPE_3x1,
            )
        } else {
            (
                DWRITE_TEXT_ANTIALIAS_MODE_GRAYSCALE,
                DWRITE_TEXTURE_ALIASED_1x1,
            )
        };

        let analysis = unsafe {
            self.factory.CreateGlyphRunAnalysis(
                &glyph_run,
                Some(&transform),
                rendering_mode,
                DWRITE_MEASURING_MODE_NATURAL,
                grid_fit_mode,
                antialias_mode,
                baseline.X,
                baseline.Y,
            )
        }?;

        let bounds = unsafe { analysis.GetAlphaTextureBounds(texture_type) }?;

        Ok(GlyphAnalysis {
            analysis,
            bounds,
            texture_type,
        })
    }

    /// The pinned mask rasterization (font_rasterizer.rs:305-357).
    fn rasterize_mask(
        &self,
        font_face: &IDWriteFontFace3,
        params: &RenderGlyphParams,
        mode: GlyphRenderMode,
    ) -> anyhow::Result<RasterizedGlyph> {
        let glyph = self.create_glyph_analysis(font_face, params, mode)?;
        let Some((bounds, width, height)) = convert_bounds(glyph.bounds)? else {
            return Ok(RasterizedGlyph::empty(mode.rasterized_format()));
        };

        let pixel_count = width as usize * height as usize;

        if mode != GlyphRenderMode::Subpixel {
            let mut pixels = vec![0; pixel_count];
            unsafe {
                glyph.analysis.CreateAlphaTexture(
                    DWRITE_TEXTURE_ALIASED_1x1,
                    &glyph.bounds,
                    &mut pixels,
                )?;
            }

            return Ok(RasterizedGlyph {
                bounds,
                size: size(DevicePixels(width), DevicePixels(height)),
                format: RasterizedGlyphFormat::AlphaMask,
                pixels,
            });
        }

        let mut pixels = vec![0; pixel_count * 4];
        unsafe {
            glyph.analysis.CreateAlphaTexture(
                glyph.texture_type,
                &glyph.bounds,
                &mut pixels[..pixel_count * 3],
            )?;
        }

        for pixel_index in (0..pixel_count).rev() {
            let source = pixel_index * 3;
            let target = pixel_index * 4;
            let red = pixels[source];
            let green = pixels[source + 1];
            let blue = pixels[source + 2];
            pixels[target..target + 4].copy_from_slice(&[blue, green, red, 0]);
        }

        Ok(RasterizedGlyph {
            bounds,
            size: size(DevicePixels(width), DevicePixels(height)),
            format: RasterizedGlyphFormat::BgraSubpixelMask,
            pixels,
        })
    }

    /// The pinned COLRv0 rasterization (font_rasterizer.rs:362-537).
    fn rasterize_colr(
        &self,
        font_face: &IDWriteFontFace3,
        params: &RenderGlyphParams,
    ) -> anyhow::Result<RasterizedGlyph> {
        let current_color = prepared_color(params.raster_style)?;
        let glyph_id = [u16::try_from(params.glyph_id.0)?];
        let advances = [0.0];
        let offsets = [DWRITE_GLYPH_OFFSET::default()];
        let base_face: IDWriteFontFace = font_face.cast()?;
        let glyph_run = DWRITE_GLYPH_RUN {
            fontFace: ManuallyDrop::new(Some(unsafe { std::ptr::read(&base_face) })),
            fontEmSize: f32::from(params.font_size),
            glyphCount: 1,
            glyphIndices: glyph_id.as_ptr(),
            glyphAdvances: advances.as_ptr(),
            glyphOffsets: offsets.as_ptr(),
            isSideways: BOOL(0),
            bidiLevel: 0,
        };

        let transform = raster_transform(params.scale_factor);
        let baseline = baseline_origin(params);
        let enumerate = || unsafe {
            self.factory.TranslateColorGlyphRun(
                baseline,
                &glyph_run,
                None,
                DWRITE_GLYPH_IMAGE_FORMATS_COLR,
                DWRITE_MEASURING_MODE_NATURAL,
                Some(&transform),
                0,
            )
        };

        let enumerator = enumerate()?;
        let mut raster_bounds: Option<RECT> = None;
        while unsafe { enumerator.MoveNext() }?.as_bool() {
            let run = unsafe { &*enumerator.GetCurrentRun()? };

            if run.glyphImageFormat & DWRITE_GLYPH_IMAGE_FORMATS_COLR
                == DWRITE_GLYPH_IMAGE_FORMATS_NONE
            {
                continue;
            }

            let analysis = unsafe {
                self.factory.CreateGlyphRunAnalysis(
                    &run.Base.glyphRun,
                    Some(&transform),
                    DWRITE_RENDERING_MODE1_NATURAL_SYMMETRIC,
                    run.measuringMode,
                    DWRITE_GRID_FIT_MODE_DEFAULT,
                    DWRITE_TEXT_ANTIALIAS_MODE_GRAYSCALE,
                    run.Base.baselineOriginX,
                    run.Base.baselineOriginY,
                )
            }?;

            let layer_bounds =
                unsafe { analysis.GetAlphaTextureBounds(DWRITE_TEXTURE_ALIASED_1x1) }?;

            if convert_bounds(layer_bounds)?.is_none() {
                continue;
            }

            raster_bounds = Some(match raster_bounds {
                Some(bounds) => RECT {
                    left: bounds.left.min(layer_bounds.left),
                    top: bounds.top.min(layer_bounds.top),
                    right: bounds.right.max(layer_bounds.right),
                    bottom: bounds.bottom.max(layer_bounds.bottom),
                },
                None => layer_bounds,
            });
        }

        let Some(raster_bounds) = raster_bounds else {
            return Ok(RasterizedGlyph::empty(RasterizedGlyphFormat::BgraColor));
        };

        let Some((bounds, width, height)) = convert_bounds(raster_bounds)? else {
            unreachable!("color layer bounds were validated above");
        };

        let mut premultiplied = vec![[0.0f32; 4]; width as usize * height as usize];
        let enumerator = enumerate()?;
        while unsafe { enumerator.MoveNext() }?.as_bool() {
            let run = unsafe { &*enumerator.GetCurrentRun()? };

            if run.glyphImageFormat & DWRITE_GLYPH_IMAGE_FORMATS_COLR
                == DWRITE_GLYPH_IMAGE_FORMATS_NONE
            {
                continue;
            }

            let layer_analysis = unsafe {
                self.factory.CreateGlyphRunAnalysis(
                    &run.Base.glyphRun,
                    Some(&transform),
                    DWRITE_RENDERING_MODE1_NATURAL_SYMMETRIC,
                    run.measuringMode,
                    DWRITE_GRID_FIT_MODE_DEFAULT,
                    DWRITE_TEXT_ANTIALIAS_MODE_GRAYSCALE,
                    run.Base.baselineOriginX,
                    run.Base.baselineOriginY,
                )
            }?;

            let layer_bounds =
                unsafe { layer_analysis.GetAlphaTextureBounds(DWRITE_TEXTURE_ALIASED_1x1) }?;

            let Some((_, layer_width, layer_height)) = convert_bounds(layer_bounds)? else {
                continue;
            };

            let mut coverage = vec![0; layer_width as usize * layer_height as usize];
            unsafe {
                layer_analysis.CreateAlphaTexture(
                    DWRITE_TEXTURE_ALIASED_1x1,
                    &layer_bounds,
                    &mut coverage,
                )?;
            }

            let color = layer_color(run, current_color);
            for layer_y in 0..layer_height {
                let target_y = layer_bounds.top - raster_bounds.top + layer_y;

                if !(0..height).contains(&target_y) {
                    continue;
                }

                for layer_x in 0..layer_width {
                    let target_x = layer_bounds.left - raster_bounds.left + layer_x;

                    if !(0..width).contains(&target_x) {
                        continue;
                    }

                    let source_index = (layer_y as usize * layer_width as usize) + layer_x as usize;
                    let target_index = target_y as usize * width as usize + target_x as usize;
                    let corrected = corrected_coverage(
                        f32::from(coverage[source_index]) / 255.0,
                        color,
                        &self.color_rendering,
                    );
                    composite_color(&mut premultiplied[target_index], color, corrected);
                }
            }
        }

        let mut pixels = Vec::with_capacity(premultiplied.len() * 4);
        for pixel in premultiplied {
            let alpha = pixel[3].clamp(0.0, 1.0);

            if alpha == 0.0 {
                pixels.extend_from_slice(&[0, 0, 0, 0]);
                continue;
            }

            pixels.extend_from_slice(&[
                float_channel(pixel[2] / alpha),
                float_channel(pixel[1] / alpha),
                float_channel(pixel[0] / alpha),
                float_channel(alpha),
            ]);
        }

        Ok(RasterizedGlyph {
            bounds,
            size: size(DevicePixels(width), DevicePixels(height)),
            format: RasterizedGlyphFormat::BgraColor,
            pixels,
        })
    }

    /// The pinned monochrome currentColor rasterization
    /// (font_rasterizer.rs:539-570).
    fn rasterize_native_monochrome_color(
        &self,
        glyph: GlyphAnalysis,
        bounds: Bounds<DevicePixels>,
        width: i32,
        height: i32,
        color: gpui::Rgba8,
    ) -> anyhow::Result<RasterizedGlyph> {
        let pixel_count = width as usize * height as usize;
        let mut coverage = vec![0; pixel_count];
        unsafe {
            glyph.analysis.CreateAlphaTexture(
                DWRITE_TEXTURE_ALIASED_1x1,
                &glyph.bounds,
                &mut coverage,
            )?;
        }

        let mut pixels = Vec::with_capacity(pixel_count * 4);
        for alpha in coverage {
            let alpha = multiply_u8(alpha, color.alpha);
            pixels.extend_from_slice(&[color.blue, color.green, color.red, alpha]);
        }

        Ok(RasterizedGlyph {
            bounds,
            size: size(DevicePixels(width), DevicePixels(height)),
            format: RasterizedGlyphFormat::BgraColor,
            pixels,
        })
    }
}

impl Drop for DirectWriteGlyphRasterizer {
    fn drop(&mut self) {
        self.faces.clear();
        unsafe {
            let _ = self
                .factory
                .UnregisterFontFileLoader(&self.in_memory_loader);
        }
    }
}

impl GlyphRasterizer for DirectWriteGlyphRasterizer {
    fn supports_color_glyph(&self, kind: ColorGlyphKind) -> bool {
        kind == ColorGlyphKind::ColrV0
    }

    fn prepare_style(&self, request: RasterStyleRequest) -> PreparedRasterStyle {
        if request.requested_mode == GlyphRenderMode::Color {
            PreparedRasterStyle {
                mode: GlyphRenderMode::Color,
                color_effect: RasterColorEffect::Preblend(
                    request.scene_color.quantize_raster_color(),
                ),
            }
        } else {
            PreparedRasterStyle::independent(request.requested_mode)
        }
    }

    fn rasterize(
        &mut self,
        face: RasterFace<'_>,
        params: &RenderGlyphParams,
    ) -> anyhow::Result<RasterizedGlyph> {
        anyhow::ensure!(
            params.scale_factor.is_finite() && params.scale_factor > 0.0,
            "invalid raster scale factor"
        );
        let color_kind = if params.raster_style.mode == GlyphRenderMode::Color {
            face.color_glyph_kind(params.glyph_id)?
        } else {
            None
        };

        if color_kind == Some(ColorGlyphKind::Bitmap) {
            return Err(NativeRasterUnsupported::BitmapColorGlyph.into());
        }

        if color_kind == Some(ColorGlyphKind::ColrV1) {
            return Err(NativeRasterUnsupported::ColrV1Glyph.into());
        }

        let font_face = self.native_face(&face)?;
        match color_kind {
            Some(ColorGlyphKind::ColrV0) => self.rasterize_colr(&font_face, params),
            Some(ColorGlyphKind::Svg) | None
                if params.raster_style.mode == GlyphRenderMode::Color =>
            {
                let color = prepared_color(params.raster_style)?;
                let glyph =
                    self.create_glyph_analysis(&font_face, params, GlyphRenderMode::Grayscale)?;
                let Some((bounds, width, height)) = convert_bounds(glyph.bounds)? else {
                    return Ok(RasterizedGlyph::empty(RasterizedGlyphFormat::BgraColor));
                };

                self.rasterize_native_monochrome_color(glyph, bounds, width, height, color)
            }
            _ => self.rasterize_mask(&font_face, params, params.raster_style.mode),
        }
    }

    fn recommended_mode(&self) -> gpui::TextRenderingMode {
        if self.system_subpixel_rendering {
            gpui::TextRenderingMode::Subpixel
        } else {
            gpui::TextRenderingMode::Grayscale
        }
    }
}

impl NativeFace {
    /// The pinned face creation (font_rasterizer.rs:652-714).
    fn new(
        factory: &IDWriteFactory5,
        variable_factory: Option<&IDWriteFactory6>,
        loader: &IDWriteInMemoryFontFileLoader,
        face: &RasterFace<'_>,
    ) -> anyhow::Result<Self> {
        let data: Box<[u8]> = face.data.into();
        let data_len = u32::try_from(data.len()).context("font data exceeds DirectWrite limits")?;
        let file = unsafe {
            loader.CreateInMemoryFontFileReference(
                factory,
                data.as_ptr().cast(),
                data_len,
                None::<&windows::core::IUnknown>,
            )
        }?;

        let mut simulations = DWRITE_FONT_SIMULATIONS_NONE;

        if face.synthesis.embolden {
            simulations |= DWRITE_FONT_SIMULATIONS_BOLD;
        }

        if face.synthesis.skew_degrees.is_some() {
            simulations |= DWRITE_FONT_SIMULATIONS_OBLIQUE;
        }

        let native_face = if face.variations.is_empty() {
            let reference =
                unsafe { factory.CreateFontFaceReference(&file, face.face_index, simulations) }?;

            unsafe { reference.CreateFontFace() }?
        } else {
            let variable_factory =
                variable_factory.ok_or(NativeRasterUnsupported::VariableAxesOnLegacyDirectWrite)?;
            let variations = face
                .variations
                .iter()
                .map(|variation| DWRITE_FONT_AXIS_VALUE {
                    axisTag: DWRITE_FONT_AXIS_TAG(u32::from_le_bytes(variation.tag.to_be_bytes())),
                    value: variation.value,
                })
                .collect::<Vec<_>>();
            let reference = unsafe {
                variable_factory.CreateFontFaceReference(
                    &file,
                    face.face_index,
                    simulations,
                    &variations,
                )
            }?;

            let variable_face = unsafe { reference.CreateFontFace() }?;

            variable_face.cast()?
        };

        Ok(Self {
            face: native_face,
            _data: data,
        })
    }
}

fn convert_bounds(bounds: RECT) -> anyhow::Result<Option<(Bounds<DevicePixels>, i32, i32)>> {
    if bounds.right <= bounds.left || bounds.bottom <= bounds.top {
        return Ok(None);
    }

    let width = bounds
        .right
        .checked_sub(bounds.left)
        .context("DirectWrite glyph width overflow")?;
    let height = bounds
        .bottom
        .checked_sub(bounds.top)
        .context("DirectWrite glyph height overflow")?;
    Ok(Some((
        Bounds {
            origin: gpui::point(DevicePixels(bounds.left), DevicePixels(bounds.top)),
            size: size(DevicePixels(width), DevicePixels(height)),
        },
        width,
        height,
    )))
}

fn raster_transform(scale_factor: f32) -> DWRITE_MATRIX {
    DWRITE_MATRIX {
        m11: scale_factor,
        m12: 0.0,
        m21: 0.0,
        m22: scale_factor,
        dx: 0.0,
        dy: 0.0,
    }
}

fn baseline_origin(params: &RenderGlyphParams) -> Vector2 {
    Vector2::new(
        f32::from(params.subpixel_variant.x) / SUBPIXEL_VARIANTS_X as f32 / params.scale_factor,
        f32::from(params.subpixel_variant.y) / SUBPIXEL_VARIANTS_Y as f32 / params.scale_factor,
    )
}

fn prepared_color(style: PreparedRasterStyle) -> anyhow::Result<gpui::Rgba8> {
    match style.color_effect {
        RasterColorEffect::Preblend(color) => Ok(color),
        _ => bail!("color glyph rasterization requires a prepared currentColor value"),
    }
}

fn layer_color(run: &DWRITE_COLOR_GLYPH_RUN1, current_color: gpui::Rgba8) -> LayerColor {
    if u32::from(run.Base.paletteIndex) == DWRITE_NO_PALETTE_INDEX {
        LayerColor {
            red: f32::from(current_color.red) / 255.0,
            green: f32::from(current_color.green) / 255.0,
            blue: f32::from(current_color.blue) / 255.0,
            alpha: f32::from(current_color.alpha) / 255.0,
        }
    } else {
        let color = run.Base.runColor;
        LayerColor {
            red: color.r,
            green: color.g,
            blue: color.b,
            alpha: color.a,
        }
    }
}

fn corrected_coverage(sample: f32, color: LayerColor, rendering: &ColorRenderingParams) -> f32 {
    let brightness = 0.30 * color.red + 0.59 * color.green + 0.11 * color.blue;
    let light_on_dark = (4.0 * (0.75 - brightness)).clamp(0.0, 1.0);
    let contrast = rendering.grayscale_enhanced_contrast * light_on_dark;
    let contrasted = sample * (contrast + 1.0) / (sample * contrast + 1.0);
    let ratios = rendering.gamma_ratios;
    let brightness_adjustment = ratios[0] * brightness + ratios[1];
    let correction = brightness_adjustment * contrasted + ratios[2] * brightness + ratios[3];
    (contrasted + contrasted * (1.0 - contrasted) * correction).clamp(0.0, 1.0)
}

fn composite_color(destination: &mut [f32; 4], color: LayerColor, coverage: f32) {
    let source_alpha = (coverage * color.alpha).clamp(0.0, 1.0);
    let inverse_alpha = 1.0 - source_alpha;
    destination[0] = color.red * source_alpha + destination[0] * inverse_alpha;
    destination[1] = color.green * source_alpha + destination[1] * inverse_alpha;
    destination[2] = color.blue * source_alpha + destination[2] * inverse_alpha;
    destination[3] = source_alpha + destination[3] * inverse_alpha;
}

fn float_channel(value: f32) -> u8 {
    (value * 255.0).round().clamp(0.0, 255.0) as u8
}

fn multiply_u8(left: u8, right: u8) -> u8 {
    ((u16::from(left) * u16::from(right) + 127) / 255) as u8
}

fn get_system_subpixel_rendering() -> bool {
    let mut smoothing_enabled = BOOL::default();
    let enabled_result = unsafe {
        SystemParametersInfoW(
            SPI_GETFONTSMOOTHING,
            0,
            Some((&mut smoothing_enabled as *mut BOOL).cast::<std::ffi::c_void>()),
            SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS::default(),
        )
    };

    let mut smoothing_type = std::ffi::c_uint::default();
    let type_result = unsafe {
        SystemParametersInfoW(
            SPI_GETFONTSMOOTHINGTYPE,
            0,
            Some((&mut smoothing_type as *mut std::ffi::c_uint).cast::<std::ffi::c_void>()),
            SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS::default(),
        )
    };

    enabled_result.is_ok()
        && type_result.is_ok()
        && smoothing_enabled.as_bool()
        && smoothing_type == FE_FONTSMOOTHINGCLEARTYPE
}

// ---------------------------------------------------------------------------
// The fixture
// ---------------------------------------------------------------------------

fn field_str(name: &str, value: &str) -> (String, FieldValue) {
    (name.to_string(), FieldValue::str(value))
}

fn field_u64(name: &str, value: u64) -> (String, FieldValue) {
    (name.to_string(), FieldValue::Uint(value))
}

fn field_i64(name: &str, value: i64) -> (String, FieldValue) {
    (name.to_string(), FieldValue::Int(value))
}

fn field_f32(name: &str, value: f32) -> (String, FieldValue) {
    (name.to_string(), FieldValue::f32(value))
}

fn sha256_hex(bytes: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    let digest = Sha256::digest(bytes);
    let mut out = String::with_capacity(64);
    for byte in digest {
        out.push_str(&format!("{:02x}", byte));
    }
    out
}

/// Builds the descriptor input shape into the pinned `Font` (the
/// text-geometry fixture's build_font).
fn build_font(descriptor: &FontDescriptorIn) -> Font {
    Font {
        family: descriptor.family.clone().into(),
        weight: descriptor
            .weight
            .map(|weight| gpui::FontWeight(weight as f32))
            .unwrap_or_default(),
        style: descriptor
            .style
            .as_deref()
            .and_then(|style| match style {
                "Italic" => Some(gpui::FontStyle::Italic),
                "Oblique" => Some(gpui::FontStyle::Oblique),
                _ => Some(gpui::FontStyle::Normal),
            })
            .unwrap_or_default(),
        features: build_features(&descriptor.features),
        fallbacks: (!descriptor.fallbacks.is_empty())
            .then(|| gpui::FontFallbacks::from_fonts(descriptor.fallbacks.clone())),
    }
}

fn build_features(features: &[crate::envelope::FeatureIn]) -> gpui::FontFeatures {
    gpui::FontFeatures(std::sync::Arc::new(
        features
            .iter()
            .map(|feature| (feature.tag.clone(), feature.value))
            .collect::<Vec<_>>(),
    ))
}

fn render_mode_of(name: &str) -> GlyphRenderMode {
    match name {
        MODE_GRAYSCALE => GlyphRenderMode::Grayscale,
        MODE_SUBPIXEL => GlyphRenderMode::Subpixel,
        MODE_COLOR => GlyphRenderMode::Color,
        other => panic!("unknown render mode {other:?}"),
    }
}

/// Runs the fixture: constructs the reference text system with the
/// ported DirectWrite rasterizer, records the construction facts, then
/// rasters every (font, char, size, scale, subpixel, mode) query of
/// every case through the public API.
pub fn run(envelope: &Envelope) -> Vec<crate::trace::TraceEvent> {
    let recorder = TraceRecorder::new();
    let inputs = envelope
        .inputs
        .glyph_cases
        .as_ref()
        .unwrap_or_else(|| panic!("fixture kind glyph-raster-v1 requires inputs.glyph_cases"));

    let parley = Arc::new(
        ParleyTextSystem::new_with_rasterizer(
            SystemFonts::Load,
            SERVICE_SYSTEM_FONT_FAMILY,
            WindowsGlyphRasterizer::new(),
        )
        .with_fallback_families(SERVICE_FALLBACKS),
    );
    let system = TextSystem::new(parley.clone());
    let platform: Arc<ParleyTextSystem> = parley.clone();

    // Construction record: the pinned fallback chain, the recommended
    // rendering mode and the DirectWrite facts (the rasterizer's private
    // ColorRenderingParams in the pin; recorded here so machine drift in
    // the OS text settings is detectable from the trace alone).
    let recommended = platform.recommended_rendering_mode(FontId(0), px(16.0));
    let recommended_name = match recommended {
        gpui::TextRenderingMode::Subpixel => "Subpixel",
        gpui::TextRenderingMode::Grayscale => "Grayscale",
        gpui::TextRenderingMode::PlatformDefault => "PlatformDefault",
    };
    recorder.event_with(
        "raster-system-begin",
        Some("raster-system"),
        vec![
            field_str("systemFontFamily", SERVICE_SYSTEM_FONT_FAMILY),
            field_str("fallbackFamilies", &SERVICE_FALLBACKS.join(", ")),
            field_str("recommendedMode", recommended_name),
            field_u64("subpixelVariantsX", SUBPIXEL_VARIANTS_X as u64),
            field_u64("subpixelVariantsY", SUBPIXEL_VARIANTS_Y as u64),
            field_u64("fontGeneration", platform.font_generation()),
        ],
    );

    for case in &inputs.cases {
        run_case(&system, &platform, case, &recorder)
            .unwrap_or_else(|e| panic!("glyph-raster case {:?} failed: {e:#}", case.label));
    }
    recorder.into_events()
}

fn run_case(
    system: &TextSystem,
    platform: &Arc<ParleyTextSystem>,
    case: &GlyphRasterCase,
    recorder: &TraceRecorder,
) -> Result<()> {
    let mode = render_mode_of(&case.mode);
    let scene_color = case.scene_color.map(|components| {
        Rgba::new(
            components[0] as f32,
            components[1] as f32,
            components[2] as f32,
            components[3] as f32,
        )
    });

    let font = build_font(&case.font);
    let font_id = platform
        .font_id(&font)
        .with_context(|| format!("resolving font {:?}", case.font.family))?;

    recorder.event_with(
        "case-begin",
        Some(&case.label),
        vec![
            field_str("family", &case.font.family),
            field_u64("fontId", font_id.0 as u64),
            field_str("mode", &case.mode),
        ],
    );

    let mut variants = case.subpixel_variants.clone();
    if variants.is_empty() {
        variants.push([0, 0]);
    }

    for character in &case.chars {
        let mut chars = character.chars();
        let ch: char = match (chars.next(), chars.next()) {
            (Some(ch), None) => ch,
            _ => anyhow::bail!("case {:?}: one character per entry", case.label),
        };

        // The pinned glyph lookup: `None` is the missing-glyph outcome.
        let glyph_id = match platform.glyph_for_char(font_id, ch) {
            Some(glyph_id) => glyph_id,
            None => {
                recorder.event_with(
                    "glyph-raster",
                    Some(&case.label),
                    vec![
                        field_str("char", &character.escape_unicode().collect::<String>()),
                        field_u64("hasGlyph", 0),
                    ],
                );
                continue;
            }
        };

        let mut style_fields: Vec<(String, FieldValue)> = Vec::new();
        if let Some(color) = scene_color {
            // The public style preparation (the window's paint path).
            let style = platform.prepare_raster_style(RasterStyleRequest {
                scene_color: color,
                requested_mode: mode,
            });
            let bytes: [u8; 4] = match style.color_effect {
                RasterColorEffect::Independent => [0, 0, 0, 0],
                RasterColorEffect::Preblend(color) => color.into(),
                RasterColorEffect::Dilation(_) => [0, 0, 0, 0],
            };
            style_fields.push(field_str("effectTag", "preblend"));
            style_fields.push(field_u64("effectR", bytes[0] as u64));
            style_fields.push(field_u64("effectG", bytes[1] as u64));
            style_fields.push(field_u64("effectB", bytes[2] as u64));
            style_fields.push(field_u64("effectA", bytes[3] as u64));
        } else {
            let style = platform.prepare_raster_style(RasterStyleRequest {
                scene_color: Rgba::new(0.0, 0.0, 0.0, 1.0),
                requested_mode: mode,
            });
            style_fields.push(field_str("effectTag", match style.color_effect {
                RasterColorEffect::Independent => "independent",
                _ => "preblend",
            }));
        }

        for &size_value in &case.sizes {
            for &scale_value in &case.scales {
                for &variant in &variants {
                    let style = platform.prepare_raster_style(RasterStyleRequest {
                        scene_color: scene_color
                            .unwrap_or(Rgba::new(0.0, 0.0, 0.0, 1.0)),
                        requested_mode: mode,
                    });
                    let params = RenderGlyphParams {
                        font_id,
                        glyph_id,
                        font_size: px(size_value as f32),
                        subpixel_variant: gpui::point(
                            variant[0].min((SUBPIXEL_VARIANTS_X - 1) as u64) as u8,
                            variant[1].min((SUBPIXEL_VARIANTS_Y - 1) as u64) as u8,
                        ),
                        scale_factor: scale_value as f32,
                        raster_style: style,
                    };

                    // The pinned public call, including validate and the
                    // metadata consistency check.
                    let mut fields = vec![
                        field_str("char", &character.escape_unicode().collect::<String>()),
                        field_u64("hasGlyph", 1),
                        field_u64("glyphId", glyph_id.0 as u64),
                        field_f32("fontSize", size_value as f32),
                        field_u64("subpixelX", variant[0]),
                        field_u64("subpixelY", variant[1]),
                        field_f32("scale", scale_value as f32),
                    ];
                    fields.extend(style_fields.iter().cloned());

                    match system.rasterize_glyph(&params) {
                        Ok(raster) => {
                            fields.push(field_str("outcome", "ok"));
                            fields.push(field_str(
                                "format",
                                match raster.format {
                                    RasterizedGlyphFormat::AlphaMask => "alpha-mask",
                                    RasterizedGlyphFormat::BgraSubpixelMask => "bgra-subpixel",
                                    RasterizedGlyphFormat::BgraColor => "bgra-color",
                                },
                            ));
                            fields.push(field_i64(
                                "boundsX",
                                i64::from(raster.bounds.origin.x.0),
                            ));
                            fields.push(field_i64(
                                "boundsY",
                                i64::from(raster.bounds.origin.y.0),
                            ));
                            fields.push(field_i64(
                                "boundsW",
                                i64::from(raster.bounds.size.width.0),
                            ));
                            fields.push(field_i64(
                                "boundsH",
                                i64::from(raster.bounds.size.height.0),
                            ));
                            fields.push(field_i64("width", i64::from(raster.size.width.0)));
                            fields.push(field_i64("height", i64::from(raster.size.height.0)));
                            fields.push(field_u64("pixelBytes", raster.pixels.len() as u64));
                            fields.push(field_str("pixelsSHA256", &sha256_hex(&raster.pixels)));
                        }
                        Err(error) => {
                            // A raster failure is an honest recorded
                            // outcome, never a silent skip.
                            fields.push(field_str("outcome", "error"));
                            fields.push(field_str("errorKind", "raster-error"));
                            let _ = error;
                        }
                    }

                    recorder.event_with("glyph-raster", Some(&case.label), fields);
                }
            }
        }
    }

    recorder.event("case-end", Some(&case.label));
    Ok(())
}

