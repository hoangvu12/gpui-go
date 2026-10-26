// Vendored copy of `gpui_ce_parley`'s `src/store.rs` (the pinned checkout
// at 254b5dbd…): the FontStore / LoadedFont / ColorGlyphClassifier
// interning machinery the vendored text system needs. The pinned file's
// PUBLIC types (`FontVariation`, `FontSynthesis`, `RasterFace`,
// `ColorGlyphKind`, the `GlyphRasterizer` trait, `SwashGlyphRasterizer`)
// and its test module are NOT vendored — they remain the pinned crate's
// own (crate::glyph implements that pinned trait), so this copy keeps
// only the private store and imports the public types from
// `gpui_ce_parley`. No other change; record any further edit in
// text_vendored/mod.rs.

use anyhow::{Context as _, Result, bail};
use fontique::{Blob, Synthesis};
use gpui::{Bounds, FontId, FontMetrics, GlyphId, Size, point, size};
use gpui_ce_parley::{ColorGlyphKind, FontSynthesis, FontVariation, RasterFace};
use skrifa::{
    FontRef, MetadataProvider as _, Tag,
    instance::{Location, NormalizedCoord, Size as SkrifaSize},
    raw::TableProvider as _,
};
use std::collections::HashMap;

const CANONICAL_FONT_ID_BIT: usize = 1 << (usize::BITS - 1);

/// The identity assigned to one immutable font-data blob.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
struct SourceIdentity(u64);

impl From<&Blob<u8>> for SourceIdentity {
    /// Returns the stable identity carried by a Fontique blob.
    fn from(data: &Blob<u8>) -> Self {
        Self(data.id())
    }
}

/// Synthetic outline changes which affect raster output.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
struct SynthesisKey {
    embolden: bool,
    skew_bits: Option<u32>,
}

impl From<Synthesis> for SynthesisKey {
    fn from(synthesis: Synthesis) -> Self {
        Self {
            embolden: synthesis.embolden(),
            skew_bits: synthesis.skew().map(f32::to_bits),
        }
    }
}

/// Full identity of a selected font instance.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
struct FontKey {
    source_identity: SourceIdentity,
    face_index: u32,
    normalized_coords: Vec<NormalizedCoord>,
    synthesis: SynthesisKey,
}

/// A concrete font face and variation instance selected for GPUI.
#[derive(Clone, Debug)]
pub(crate) struct LoadedFont {
    /// Immutable bytes containing the selected face.
    pub(crate) data: Blob<u8>,
    /// Face index within the font collection.
    pub(crate) index: u32,
    /// Normalized variation coordinates in axis order.
    pub(crate) normalized_coords: Vec<NormalizedCoord>,
    /// Design-space coordinates for native font APIs.
    pub(crate) variations: Vec<FontVariation>,
    /// Synthetic styling requested for the selected face.
    pub(crate) synthesis: Synthesis,
    /// Whether this face advertises color glyph data.
    pub(crate) has_color_glyphs: bool,
    source_identity: SourceIdentity,
}

/// Color glyph data parsed once for all glyphs in a shaped run.
pub(crate) struct ColorGlyphClassifier<'a> {
    colr: skrifa::color::ColorGlyphCollection<'a>,
    bitmap_strikes: skrifa::bitmap::BitmapStrikes<'a>,
    svg_ranges: Vec<(u32, u32)>,
}

impl ColorGlyphClassifier<'_> {
    fn new(font: FontRef<'_>) -> ColorGlyphClassifier<'_> {
        let svg_ranges = font
            .svg()
            .ok()
            .and_then(|svg| svg.svg_document_list().ok())
            .map(|documents| {
                documents
                    .document_records()
                    .iter()
                    .map(|record| {
                        (
                            record.start_glyph_id().to_u32(),
                            record.end_glyph_id().to_u32(),
                        )
                    })
                    .collect()
            })
            .unwrap_or_default();
        ColorGlyphClassifier {
            colr: font.color_glyphs(),
            bitmap_strikes: font.bitmap_strikes(),
            svg_ranges,
        }
    }

    /// Returns the artwork format used by this glyph.
    pub(crate) fn kind(&self, glyph_id: GlyphId) -> Option<ColorGlyphKind> {
        let skrifa_id = skrifa::GlyphId::new(glyph_id.0);

        if let Some(glyph) = self.colr.get(skrifa_id) {
            return Some(match glyph.format() {
                skrifa::color::ColorGlyphFormat::ColrV0 => ColorGlyphKind::ColrV0,
                skrifa::color::ColorGlyphFormat::ColrV1 => ColorGlyphKind::ColrV1,
            });
        }

        let has_color_bitmap = matches!(
            self.bitmap_strikes.format(),
            Some(skrifa::bitmap::BitmapFormat::Sbix | skrifa::bitmap::BitmapFormat::Cbdt)
        ) && self
            .bitmap_strikes
            .iter()
            .any(|strike| strike.get(skrifa_id).is_some());

        if has_color_bitmap {
            return Some(ColorGlyphKind::Bitmap);
        }

        self.svg_ranges
            .iter()
            .any(|&(start, end)| (start..=end).contains(&glyph_id.0))
            .then_some(ColorGlyphKind::Svg)
    }
}

impl LoadedFont {
    fn skrifa_ref(&self) -> Result<FontRef<'_>> {
        FontRef::from_index(self.data.as_ref(), self.index)
            .context("Skrifa could not parse the stored font face")
    }

    fn location(&self) -> Location {
        let mut location = Location::new(self.normalized_coords.len());
        location
            .coords_mut()
            .copy_from_slice(&self.normalized_coords);
        location
    }

    /// Builds a glyph-level view of the face's color artwork.
    pub(crate) fn color_glyphs(&self) -> Result<ColorGlyphClassifier<'_>> {
        let font = self.skrifa_ref()?;
        Ok(ColorGlyphClassifier::new(font))
    }

    /// Reads global metrics in font units from the canonical bytes.
    pub(crate) fn metrics(&self) -> Result<FontMetrics> {
        let font = self.skrifa_ref()?;
        let location = self.location();
        let metrics = font.metrics(SkrifaSize::unscaled(), &location);
        Ok(FontMetrics {
            units_per_em: metrics.units_per_em.into(),
            ascent: metrics.ascent,
            descent: -metrics.descent,
            line_gap: metrics.leading,
            underline_position: metrics.underline.map_or(0.0, |underline| underline.offset),
            underline_thickness: metrics
                .underline
                .map_or(0.0, |underline| underline.thickness),
            cap_height: metrics.cap_height.unwrap_or(metrics.ascent),
            x_height: metrics.x_height.unwrap_or(metrics.ascent),
            bounding_box: Bounds {
                origin: point(0.0, 0.0),
                size: size(
                    metrics.max_width.unwrap_or(0.0),
                    metrics.ascent - metrics.descent,
                ),
            },
        })
    }

    /// Maps a Unicode scalar to a nominal glyph using the canonical bytes.
    pub(crate) fn glyph_for_char(&self, character: char) -> Result<Option<GlyphId>> {
        Ok(self
            .skrifa_ref()?
            .charmap()
            .map(character)
            .map(|glyph| GlyphId(glyph.to_u32())))
    }

    /// Returns the unscaled advance for a glyph.
    pub(crate) fn advance(&self, glyph_id: GlyphId) -> Result<Size<f32>> {
        let font = self.skrifa_ref()?;
        let location = self.location();
        let metrics = font.glyph_metrics(SkrifaSize::unscaled(), &location);
        let glyph_id = skrifa::GlyphId::new(glyph_id.0);
        Ok(size(metrics.advance_width(glyph_id).unwrap_or(0.0), 0.0))
    }

    pub(crate) fn raster_face(&self, font_id: FontId) -> RasterFace<'_> {
        RasterFace {
            font_id,
            data: self.data.as_ref(),
            face_index: self.index,
            variations: &self.variations,
            // The pinned crate's `impl From<Synthesis> for FontSynthesis`
            // cannot be vendored (FontSynthesis is the pinned crate's type
            // and Synthesis is fontique's — the orphan rule); the same
            // field mapping is spelled out instead.
            synthesis: FontSynthesis {
                embolden: self.synthesis.embolden(),
                skew_degrees: self.synthesis.skew(),
            },
            has_color_glyphs: self.has_color_glyphs,
        }
    }

    pub(crate) fn data_identity(&self) -> u64 {
        self.source_identity.0
    }

    /// Returns the glyph's control bounds in font units.
    pub(crate) fn glyph_bounds(&self, glyph_id: GlyphId) -> Result<Bounds<f32>> {
        let font = self.skrifa_ref()?;
        let location = self.location();
        let bounds = font
            .glyph_metrics(SkrifaSize::unscaled(), &location)
            .bounds(skrifa::GlyphId::new(glyph_id.0));
        let Some(bounds) = bounds else {
            return Ok(Bounds::default());
        };

        Ok(Bounds {
            origin: point(bounds.x_min, bounds.y_min),
            size: size(bounds.x_max - bounds.x_min, bounds.y_max - bounds.y_min),
        })
    }
}

/// Canonical storage for concrete font faces and variation instances.
#[derive(Default)]
pub(crate) struct FontStore {
    fonts: Vec<LoadedFont>,
    ids_by_key: HashMap<FontKey, FontId>,
}

impl FontStore {
    /// Interns a face using Fontique's selected variation and synthesis settings.
    pub(crate) fn intern_synthesized(
        &mut self,
        data: Blob<u8>,
        index: u32,
        synthesis: Synthesis,
    ) -> Result<FontId> {
        let normalized_coords = {
            let font = FontRef::from_index(data.as_ref(), index)
                .context("cannot intern a font face Skrifa cannot parse")?;
            font.axes()
                .location(synthesis.variation_settings().iter().copied())
                .coords()
                .to_vec()
        };

        self.intern(data, index, &normalized_coords, synthesis)
    }

    /// Interns a selected font instance and returns its canonical GPUI ID.
    pub(crate) fn intern(
        &mut self,
        data: Blob<u8>,
        index: u32,
        normalized_coords: &[NormalizedCoord],
        synthesis: Synthesis,
    ) -> Result<FontId> {
        let font = FontRef::from_index(data.as_ref(), index)
            .context("cannot intern a font face Skrifa cannot parse")?;
        let key = FontKey {
            source_identity: SourceIdentity::from(&data),
            face_index: index,
            normalized_coords: normalized_coords.to_vec(),
            synthesis: synthesis.into(),
        };

        if let Some(id) = self.ids_by_key.get(&key) {
            return Ok(*id);
        }

        if self.fonts.len() >= CANONICAL_FONT_ID_BIT {
            bail!("canonical font store exhausted its FontId namespace");
        }

        let id = FontId(CANONICAL_FONT_ID_BIT | self.fonts.len());
        let variations = design_variations(&font, normalized_coords);
        let has_color_glyphs = [*b"CBDT", *b"sbix", *b"COLR", *b"SVG "]
            .into_iter()
            .any(|tag| font.table_data(Tag::new(&tag)).is_some());
        let source_identity = SourceIdentity::from(&data);
        self.fonts.push(LoadedFont {
            data,
            index,
            normalized_coords: normalized_coords.to_vec(),
            variations,
            synthesis,
            has_color_glyphs,
            source_identity,
        });

        self.ids_by_key.insert(key, id);
        Ok(id)
    }

    /// Returns the stored font for a canonical ID.
    pub(crate) fn get(&self, id: FontId) -> Option<&LoadedFont> {
        canonical_index(id).and_then(|index| self.fonts.get(index))
    }
}

/// Converts the shaped normalized location back to the design-space values expected by native
/// APIs. Skrifa performs the forward conversion, including `avar`, during the bounded search.
fn design_variations(
    font: &FontRef<'_>,
    normalized_coords: &[NormalizedCoord],
) -> Vec<FontVariation> {
    let axes = font.axes();
    let axis_records = axes.iter().collect::<Vec<_>>();
    let mut values = axis_records
        .iter()
        .map(|axis| axis.default_value())
        .collect::<Vec<_>>();

    // Revisit every axis so version 2 `avar` mappings which couple axes converge as well as the
    // ordinary per-axis segment maps. Native APIs will apply the same mapping to these values.
    for _ in 0..4 {
        for (axis_index, axis) in axis_records.iter().enumerate() {
            let target = normalized_coords
                .get(axis_index)
                .copied()
                .unwrap_or_default()
                .to_f32();
            let mut low = axis.min_value();
            let mut high = axis.max_value();
            for _ in 0..24 {
                values[axis_index] = (low + high) * 0.5;
                let normalized = axes
                    .location(
                        axis_records
                            .iter()
                            .zip(&values)
                            .map(|(axis, value)| (axis.tag(), *value)),
                    )
                    .coords()
                    .get(axis_index)
                    .copied()
                    .unwrap_or_default()
                    .to_f32();

                if normalized < target {
                    low = values[axis_index];
                } else {
                    high = values[axis_index];
                }
            }

            values[axis_index] = (low + high) * 0.5;
        }
    }

    axis_records
        .into_iter()
        .zip(values)
        .map(|(axis, value)| FontVariation {
            tag: axis.tag(),
            value,
        })
        .collect()
}

fn canonical_index(id: FontId) -> Option<usize> {
    (id.0 & CANONICAL_FONT_ID_BIT != 0).then_some(id.0 & !CANONICAL_FONT_ID_BIT)
}
