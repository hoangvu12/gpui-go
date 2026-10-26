// Vendored copy of `gpui_ce_parley`'s `src/text_system.rs` (the pinned
// checkout at 254b5dbd…, lines 1-1541 — the implementation; the pinned
// crate's own test module is dropped, see text_vendored/mod.rs). This file
// is a byte-copy of the pinned adapter's text system with only the import
// paths rewritten for the vendored module tree (`crate::…` → `super::…`,
// the pinned crate's public re-exports stay `gpui_ce_parley::…`) plus the
// recorded rev-16 divergences marked `REV16` below. Do not edit anything
// else without recording it in text_vendored/mod.rs.

use super::catalog::{CatalogState, FaceFamily, FaceRequest, FontCatalog};
use super::paragraphs::{
    ParagraphLayout, ParleyDocumentLayout, local_range, paragraph_ranges,
};
use super::store::{FontStore, LoadedFont};
use anyhow::{Context as _, Result};
use gpui::{
    Bounds, CaretAffinity, CaretMovement, CaretPosition, Font, FontId, FontMetrics, GlyphId,
    InlineBoxRequest, InlineLayout, InlineLayoutRequest, InlineRangeGeometry, InlineTextMetrics,
    InlineTextStyle, InlineVisualLine, LineLayout, PaintFragment, PaintStyle, Pixels,
    PlatformTextLayout, PlatformTextSystem, PositionedInlineBox, PreparedRasterStyle,
    RasterStyleRequest, RasterizedGlyph, RenderGlyphParams, ShapedGlyph, Size, TextAlign,
    TextBoundary as Boundary, TextDirection as Direction, TextLayoutRequest, TextMovement,
    TextRenderingMode, TextRun, TextSelectionKind, VisualDirection, VisualLine, align_inline_boxes,
    is_paragraph_separator, point, px, size,
};
use gpui_ce_parley::{GlyphRasterizer, SwashGlyphRasterizer, SystemFonts};
use parking_lot::{Mutex, RwLock};
use parley::setting::Tag;
use parley::{
    Affinity, Alignment, AlignmentOptions, CHROMIUM_LINE_BREAK_OVERRIDE, Cluster, Cursor,
    FontContext, FontFamily, FontFamilyName, FontFeature, FontFeatures, FontStyle, FontWeight,
    GenericFamily, InlineBox, InlineBoxKind, Layout, LayoutContext, LineHeight,
    PositionedLayoutItem, Selection, StyleProperty,
};
use skrifa::instance::NormalizedCoord;
use std::{borrow::Cow, ops::Range};
use unicode_segmentation::UnicodeSegmentation as _;

struct ParleyState {
    fonts: FontContext,
    layout: LayoutContext<PaintStyle>,
}

struct ParleyLayoutParams<'a> {
    text: TextLayoutRequest<'a>,
    inline: Option<ParleyInlineLayoutParams<'a>>,
    // REV16 (browser-core 43): the side-channel the rev-15 run fields
    // reach the layout through without touching the pinned gpui types
    // (see the module docs, divergence 4). The default (no fields)
    // reproduces the pinned behavior exactly.
    browser: BrowserLayoutParams<'a>,
}

/// REV16: one run's browser text fields (the rev-15 ABI run-record
/// tags), parallel to `TextLayoutRequest::runs` by index. Both stay at
/// their defaults when the caller carries no fields.
#[derive(Clone, Copy, Default)]
pub(crate) struct BrowserRunFields {
    /// CSS `word-break` (the run-record tag: normal / break-all /
    /// keep-all).
    pub word_break: parley::WordBreak,
    /// CSS `overflow-wrap` (the run-record tag: normal / anywhere /
    /// break-word).
    pub overflow_wrap: parley::OverflowWrap,
}

/// REV16: the document-level text-align tag (the run-record tag 0
/// none / 1 start / 2 end / 3 justify; document-level by the
/// rev-15.1 any-run rule, like text-wrap: nowrap).
#[derive(Clone, Copy, Default, PartialEq, Eq, Debug)]
pub(crate) enum BrowserTextAlign {
    /// No browser alignment: the pinned behavior (Go paint-time
    /// alignment, or the inline params' `TextAlign`).
    #[default]
    None,
    /// CSS `text-align: start` — Parley's `Alignment::Start` (against
    /// the layout's base direction).
    Start,
    /// CSS `text-align: end` — Parley's `Alignment::End`.
    End,
    /// CSS `text-align: justify` — Parley's `Alignment::Justify`
    /// (stretches the whitespace clusters; the last line of every
    /// paragraph stays start-aligned).
    Justify,
}

/// REV16: the browser text fields side-channel (module docs,
/// divergence 4).
pub(crate) struct BrowserLayoutParams<'a> {
    /// The per-run fields, parallel to `TextLayoutRequest::runs`. Fewer
    /// entries than runs are treated as defaults for the missing tail.
    pub runs: &'a [BrowserRunFields],
    /// The document-level alignment tag.
    pub align: BrowserTextAlign,
}

impl<'a> Default for BrowserLayoutParams<'a> {
    fn default() -> Self {
        Self { runs: &[], align: BrowserTextAlign::None }
    }
}

struct ParleyInlineLayoutParams<'a> {
    boxes: &'a [InlineBoxRequest],
    text_styles: &'a [InlineTextStyle],
    line_height: Pixels,
    text_metrics: InlineTextMetrics,
    text_align: TextAlign,
}

struct ParleyLayoutResult {
    layout: LineLayout,
    inline_lines: Vec<InlineVisualLine>,
    inline_boxes: Vec<PositionedInlineBox>,
    size: Size<Pixels>,
    is_rtl: bool,
}

/// REV16: the `layout_text_with_browser` result — the `LineLayout`
/// plus the document's base direction (module docs, divergence 5).
pub(crate) struct BrowserLayoutResult {
    pub layout: LineLayout,
    pub is_rtl: bool,
}

fn inline_alignment_offset(text_align: TextAlign, lines: &[InlineVisualLine]) -> Pixels {
    let Some(line) = lines.first() else {
        return Pixels::ZERO;
    };

    match text_align {
        TextAlign::Left => Pixels::ZERO,
        TextAlign::Center => line.origin.x + line.size.width / 2.,
        TextAlign::Right => line.origin.x + line.size.width,
    }
}

#[derive(Clone, Debug)]
struct ParleyLayout {
    layout: Layout<PaintStyle>,
    inline_lines: Vec<InlineVisualLine>,
    text_len: usize,
    caret_stops: Vec<ParleyCaretStop>,
    graphemes: Vec<std::ops::Range<usize>>,
}

#[derive(Clone, Copy, Debug)]
struct ParleyCaretStop {
    cursor: Cursor,
    block: f64,
    inline: f64,
}

trait IntoAffinity<T> {
    fn into_affinity(self) -> T;
}

impl IntoAffinity<Affinity> for CaretAffinity {
    fn into_affinity(self) -> Affinity {
        match self {
            Self::Downstream => Affinity::Downstream,
            Self::Upstream => Affinity::Upstream,
        }
    }
}

impl IntoAffinity<CaretAffinity> for Affinity {
    fn into_affinity(self) -> CaretAffinity {
        match self {
            Self::Downstream => CaretAffinity::Downstream,
            Self::Upstream => CaretAffinity::Upstream,
        }
    }
}

impl ParleyLayout {
    fn new(layout: Layout<PaintStyle>, text: &str, inline_lines: Vec<InlineVisualLine>) -> Self {
        let graphemes = text
            .grapheme_indices(true)
            .map(|(start, grapheme)| start..start + grapheme.len())
            .collect::<Vec<_>>();
        let caret_stops = Self::collect_caret_stops(&layout, text, &graphemes);
        Self {
            layout,
            inline_lines,
            text_len: text.len(),
            caret_stops,
            graphemes,
        }
    }

    fn caret_position(cursor: Cursor) -> CaretPosition {
        let gpui_affinity = cursor.affinity().into_affinity();
        CaretPosition {
            index: cursor.index(),
            affinity: gpui_affinity,
        }
    }

    fn cursor(&self, caret: CaretPosition) -> Cursor {
        let parley_affinity = caret.affinity.into_affinity();
        Cursor::from_byte_index(&self.layout, caret.index, parley_affinity)
    }

    fn cursor_position(layout: &Layout<PaintStyle>, cursor: Cursor) -> (f64, f64) {
        let geometry = cursor.geometry(layout, 0.0);
        (geometry.y0, geometry.x0)
    }

    fn collect_caret_stops(
        layout: &Layout<PaintStyle>,
        text: &str,
        graphemes: &[std::ops::Range<usize>],
    ) -> Vec<ParleyCaretStop> {
        let boundaries = std::iter::once(0)
            .chain(graphemes.iter().map(|range| range.end))
            .chain(std::iter::once(text.len()));

        let mut stops = Vec::with_capacity((graphemes.len() + 2) * 2);
        for index in boundaries {
            for affinity in [Affinity::Downstream, Affinity::Upstream] {
                let cursor = Cursor::from_byte_index(layout, index, affinity);
                let (block, inline) = Self::cursor_position(layout, cursor);
                stops.push(ParleyCaretStop {
                    cursor,
                    block,
                    inline,
                });
            }
        }

        stops.sort_by(|left, right| {
            left.block
                .total_cmp(&right.block)
                .then_with(|| left.inline.total_cmp(&right.inline))
        });

        let mut unique_stops: Vec<ParleyCaretStop> = Vec::with_capacity(stops.len());
        for mut stop in stops {
            if let Some(previous) = unique_stops.last_mut()
                && previous.block == stop.block
                && previous.inline == stop.inline
            {
                let geometry = stop.cursor.geometry(layout, 0.0);
                let hit = Cursor::from_point(
                    layout,
                    stop.inline as f32,
                    ((geometry.y0 + geometry.y1) * 0.5) as f32,
                );

                if Self::cursor_position(layout, hit) == (stop.block, stop.inline) {
                    previous.cursor = hit;
                }

                continue;
            }

            let geometry = stop.cursor.geometry(layout, 0.0);
            let hit = Cursor::from_point(
                layout,
                stop.inline as f32,
                ((geometry.y0 + geometry.y1) * 0.5) as f32,
            );

            if Self::cursor_position(layout, hit) == (stop.block, stop.inline) {
                stop.cursor = hit;
            }

            unique_stops.push(stop);
        }

        unique_stops
    }

    fn caret_stop_index(&self, cursor: Cursor) -> Option<usize> {
        let (block, inline) = Self::cursor_position(&self.layout, cursor);
        self.caret_stops
            .binary_search_by(|stop| {
                stop.block
                    .total_cmp(&block)
                    .then_with(|| stop.inline.total_cmp(&inline))
            })
            .ok()
    }

    fn adjacent_caret_stop(
        &self,
        cursor: Cursor,
        direction: VisualDirection,
    ) -> Option<ParleyCaretStop> {
        let index = self.caret_stop_index(cursor)?;
        match direction {
            VisualDirection::Left => index
                .checked_sub(1)
                .and_then(|index| self.caret_stops.get(index)),
            VisualDirection::Right => self.caret_stops.get(index + 1),
        }
        .copied()
    }

    fn direct_visual_move(&self, cursor: Cursor, direction: VisualDirection) -> Cursor {
        match direction {
            VisualDirection::Left => cursor.previous_visual(&self.layout),
            VisualDirection::Right => cursor.next_visual(&self.layout),
        }
    }

    fn opposite(direction: VisualDirection) -> VisualDirection {
        match direction {
            VisualDirection::Left => VisualDirection::Right,
            VisualDirection::Right => VisualDirection::Left,
        }
    }

    fn native_y_for_line(&self, line_index: usize) -> f32 {
        self.layout
            .get(line_index)
            .map(|line| {
                let metrics = line.metrics();
                (metrics.block_min_coord + metrics.block_max_coord) * 0.5
            })
            .unwrap_or_else(|| self.layout.height())
    }
}

impl PlatformTextLayout for ParleyLayout {
    fn len(&self) -> usize {
        self.text_len
    }

    fn line_count(&self) -> usize {
        self.layout.len()
    }

    fn size(&self) -> Size<Pixels> {
        let width = if self.text_len == 0 && self.layout.inline_boxes().is_empty() {
            Pixels::ZERO
        } else {
            px(self.layout.width())
        };

        size(width, px(self.layout.height()))
    }

    fn byte_index_from_pixel_point(
        &self,
        pixel_point: gpui::Point<Pixels>,
        line_height: Pixels,
    ) -> std::result::Result<usize, usize> {
        let closest = self
            .caret_from_pixel_point(pixel_point, line_height)
            .unwrap_or_else(|caret| caret)
            .index;
        if self.text_len == 0 || pixel_point.y < Pixels::ZERO || line_height <= Pixels::ZERO {
            return Err(closest);
        }
        let line_index = (pixel_point.y / line_height) as usize;
        let Some(line) = self.layout.get(line_index) else {
            return Err(closest);
        };

        let metrics = line.metrics();
        let left = metrics.inline_min_coord + metrics.offset;
        let right = left + metrics.advance;
        if f32::from(pixel_point.x) < left || f32::from(pixel_point.x) >= right {
            return Err(closest);
        }

        Cluster::from_point(
            &self.layout,
            pixel_point.x.into(),
            self.native_y_for_line(line_index),
        )
        .map(|(cluster, _)| cluster.text_range().start)
        .ok_or(closest)
    }

    fn caret_from_pixel_point(
        &self,
        pixel_point: gpui::Point<Pixels>,
        line_height: Pixels,
    ) -> std::result::Result<CaretPosition, CaretPosition> {
        let line_index = if line_height > px(0.0) && pixel_point.y >= Pixels::ZERO {
            (pixel_point.y / line_height) as usize
        } else {
            0
        };

        let caret = Self::caret_position(Cursor::from_point(
            &self.layout,
            pixel_point.x.into(),
            self.native_y_for_line(line_index),
        ));
        if self.text_len == 0 {
            return Err(caret);
        }

        let Some(line) = self.layout.get(line_index) else {
            return Err(caret);
        };

        let metrics = line.metrics();
        let left = metrics.inline_min_coord + metrics.offset;
        let right = left + metrics.advance;
        if pixel_point.y >= Pixels::ZERO
            && line_height > Pixels::ZERO
            && f32::from(pixel_point.x) >= left
            && f32::from(pixel_point.x) < right
        {
            Ok(caret)
        } else {
            Err(caret)
        }
    }

    fn caret_bounds(&self, caret: CaretPosition, line_height: Pixels) -> Option<Bounds<Pixels>> {
        if caret.index > self.len() {
            return None;
        }

        if self.text_len == 0 && self.layout.inline_boxes().is_empty() {
            return Some(Bounds::new(
                point(self.inline_lines[0].origin.x, Pixels::ZERO),
                size(Pixels::ZERO, line_height),
            ));
        }

        let cursor = self.cursor(caret);
        let geometry = cursor.geometry(&self.layout, 0.0);
        let line_index = self
            .layout
            .lines()
            .position(|line| {
                let metrics = line.metrics();
                geometry.y0 as f32 >= metrics.block_min_coord
                    && (geometry.y0 as f32) < metrics.block_max_coord
            })
            .unwrap_or_else(|| self.layout.len().saturating_sub(1));
        Some(Bounds::from_corners(
            point(px(geometry.x0 as f32), line_height * line_index),
            point(px(geometry.x1 as f32), line_height * (line_index + 1)),
        ))
    }

    fn normalized_caret(&self, caret: CaretPosition) -> CaretPosition {
        Self::caret_position(self.cursor(caret))
    }

    fn adjacent_visual_caret(
        &self,
        caret: CaretPosition,
        direction: VisualDirection,
    ) -> Option<CaretPosition> {
        if self.text_len == 0 && self.layout.inline_boxes().is_empty() {
            return None;
        }

        let cursor = self.cursor(caret);
        let adjacent = self.adjacent_caret_stop(cursor, direction)?;
        let moved = self.direct_visual_move(cursor, direction);
        let moved_position = Self::cursor_position(&self.layout, moved);
        let inverse = self.direct_visual_move(moved, Self::opposite(direction));
        let inverse_position = Self::cursor_position(&self.layout, inverse);
        let current_position = Self::cursor_position(&self.layout, cursor);

        let result = if moved_position == (adjacent.block, adjacent.inline)
            && inverse_position == current_position
        {
            moved
        } else {
            adjacent.cursor
        };

        Some(Self::caret_position(result))
    }

    fn selection_bounds(
        &self,
        byte_range: std::ops::Range<usize>,
        line_height: Pixels,
    ) -> Vec<Bounds<Pixels>> {
        let anchor = Cursor::from_byte_index(&self.layout, byte_range.start, Affinity::Downstream);
        let focus = Cursor::from_byte_index(&self.layout, byte_range.end, Affinity::Upstream);
        Selection::new(anchor, focus)
            .geometry(&self.layout)
            .into_iter()
            .map(|(geometry, line_index)| {
                Bounds::from_corners(
                    point(px(geometry.x0 as f32), line_height * line_index),
                    point(px(geometry.x1 as f32), line_height * (line_index + 1)),
                )
            })
            .collect()
    }

    fn inline_geometry(&self, range: std::ops::Range<usize>) -> Option<Vec<InlineRangeGeometry>> {
        if range.is_empty() {
            return None;
        }

        let cursor_at = |index, affinity| Cursor::from_byte_index(&self.layout, index, affinity);
        let selection = Selection::new(
            cursor_at(range.start, Affinity::Downstream),
            cursor_at(range.end, Affinity::Upstream),
        );
        let mut regions = Vec::new();

        selection.geometry_with(&self.layout, |selection_bounds, line_index| {
            let Some(line) = self.layout.get(line_index) else {
                return;
            };

            let metrics = line.metrics();

            // Parley adds a selection-only extension after explicit newlines.
            let clamped_right = (selection_bounds.x1 as f32)
                .min(metrics.inline_min_coord + metrics.offset + metrics.advance);

            if clamped_right > selection_bounds.x0 as f32 {
                // Inline boxes and styled leading can change the row after Parley positions it.
                let inline_line = self.inline_lines[line_index];
                regions.push(InlineRangeGeometry {
                    bounds: Bounds::from_corners(
                        point(px(selection_bounds.x0 as f32), inline_line.origin.y),
                        point(
                            px(clamped_right),
                            inline_line.origin.y + inline_line.size.height,
                        ),
                    ),
                    visual_line_index: line_index,
                });
            }
        });

        Some(regions)
    }

    fn logical_cluster_before(&self, caret: CaretPosition) -> Option<std::ops::Range<usize>> {
        self.graphemes
            .iter()
            .rev()
            .find(|range| range.start < caret.index)
            .cloned()
    }

    fn logical_cluster_after(&self, caret: CaretPosition) -> Option<std::ops::Range<usize>> {
        self.graphemes
            .iter()
            .find(|range| range.end > caret.index)
            .cloned()
    }

    fn caret_movement(
        &self,
        caret: CaretPosition,
        movement: TextMovement,
        vertical_navigation_x: Option<Pixels>,
    ) -> CaretMovement {
        let cursor = self.cursor(caret);
        let moved = match (movement.direction, movement.boundary) {
            (Direction::Left, Boundary::Cluster) => {
                return CaretMovement {
                    result: self
                        .adjacent_visual_caret(caret, VisualDirection::Left)
                        .unwrap_or(caret),
                    vertical_navigation_x: None,
                };
            }
            (Direction::Right, Boundary::Cluster) => {
                return CaretMovement {
                    result: self
                        .adjacent_visual_caret(caret, VisualDirection::Right)
                        .unwrap_or(caret),
                    vertical_navigation_x: None,
                };
            }
            (Direction::Left, Boundary::Word) => cursor.previous_visual_word(&self.layout),
            (Direction::Right, Boundary::Word) => cursor.next_visual_word(&self.layout),
            (Direction::Start, Boundary::VisualLine) => Selection::from(cursor)
                .line_start(&self.layout, false)
                .focus(),
            (Direction::End, Boundary::VisualLine) => Selection::from(cursor)
                .line_end(&self.layout, false)
                .focus(),
            (Direction::Start, Boundary::HardLine) => Selection::from(cursor)
                .hard_line_start(&self.layout, false)
                .focus(),
            (Direction::End, Boundary::HardLine) => Selection::from(cursor)
                .hard_line_end(&self.layout, false)
                .focus(),
            (Direction::Up | Direction::Down, Boundary::VisualLine) => {
                let delta = if movement.direction == Direction::Up {
                    -1
                } else {
                    1
                };

                let geometry = cursor.geometry(&self.layout, 0.0);
                let line_index = self
                    .layout
                    .lines()
                    .position(|line| {
                        let metrics = line.metrics();
                        geometry.y0 as f32 >= metrics.block_min_coord
                            && (geometry.y0 as f32) < metrics.block_max_coord
                    })
                    .unwrap_or_else(|| self.layout.len().saturating_sub(1));
                let target_ix = line_index
                    .checked_add_signed(delta)
                    .filter(|&target_ix| self.layout.get(target_ix).is_some());
                let Some(target_ix) = target_ix else {
                    let selection = Selection::from(cursor);
                    let moved = if delta < 0 {
                        selection.previous_line(&self.layout, false)
                    } else {
                        selection.next_line(&self.layout, false)
                    };
                    return CaretMovement {
                        result: Self::caret_position(moved.focus()),
                        vertical_navigation_x,
                    };
                };

                let x = vertical_navigation_x
                    .map_or_else(|| cursor.geometry(&self.layout, 0.0).x0 as f32, f32::from);
                let moved = Cursor::from_point(&self.layout, x, self.native_y_for_line(target_ix));
                return CaretMovement {
                    result: Self::caret_position(moved),
                    vertical_navigation_x: Some(px(x)),
                };
            }
            _ => cursor,
        };
        CaretMovement {
            result: Self::caret_position(moved),
            vertical_navigation_x: None,
        }
    }

    fn selection_from_pixel_point(
        &self,
        pixel_point: gpui::Point<Pixels>,
        line_height: Pixels,
        kind: TextSelectionKind,
    ) -> std::ops::Range<usize> {
        let line_index = if line_height > Pixels::ZERO && pixel_point.y >= Pixels::ZERO {
            (pixel_point.y / line_height) as usize
        } else {
            0
        };

        let y = self.native_y_for_line(line_index);
        match kind {
            TextSelectionKind::Word => {
                Selection::word_from_point(&self.layout, pixel_point.x.into(), y)
            }
            TextSelectionKind::VisualLine => {
                Selection::line_from_point(&self.layout, pixel_point.x.into(), y)
            }
            TextSelectionKind::HardLine => {
                Selection::hard_line_from_point(&self.layout, pixel_point.x.into(), y)
            }
        }
        .text_range()
    }
}

impl ParleyState {
    fn new(system_fonts: SystemFonts) -> (Self, FontCatalog) {
        let collection = fontique::Collection::new(fontique::CollectionOptions {
            shared: true,
            system_fonts: system_fonts == SystemFonts::Load,
        });

        let source_cache = fontique::SourceCache::new_shared();
        let catalog = FontCatalog::from_shared(collection.clone(), source_cache.clone());
        (
            Self {
                fonts: FontContext {
                    collection,
                    source_cache,
                },
                layout: LayoutContext::new(),
            },
            catalog,
        )
    }
}

/// Shapes with Parley and paints exact font instances with an injected glyph rasterizer.
pub struct ParleyTextSystem {
    catalog: FontCatalog,
    fonts: RwLock<FontStore>,
    rasterizer: Mutex<Box<dyn GlyphRasterizer>>,
    parley: Mutex<ParleyState>,
    system_font_fallback: String,
    additional_fallbacks: Vec<String>,
}

impl ParleyTextSystem {
    /// Creates a text system using GPUI's default system-font family.
    pub fn new(system_fonts: SystemFonts) -> Self {
        Self::new_with_system_font(system_fonts, ".SystemUIFont")
    }

    /// Creates a text system with the concrete family used for GPUI's system-font alias.
    pub fn new_with_system_font(
        system_fonts: SystemFonts,
        system_font_fallback: impl Into<String>,
    ) -> Self {
        Self::new_with_rasterizer(
            system_fonts,
            system_font_fallback,
            SwashGlyphRasterizer::default(),
        )
    }

    /// Creates a text system which delegates only glyph rasterization to `rasterizer`.
    pub fn new_with_rasterizer(
        system_fonts: SystemFonts,
        system_font_fallback: impl Into<String>,
        rasterizer: impl GlyphRasterizer + 'static,
    ) -> Self {
        let (parley, catalog) = ParleyState::new(system_fonts);
        Self {
            catalog,
            fonts: RwLock::new(FontStore::default()),
            rasterizer: Mutex::new(Box::new(rasterizer)),
            parley: Mutex::new(parley),
            system_font_fallback: system_font_fallback.into(),
            additional_fallbacks: Vec::new(),
        }
    }

    /// Creates a deterministic text system without operating-system fonts.
    pub fn without_system_fonts() -> Self {
        Self::new_with_system_font(SystemFonts::Skip, "IBM Plex Sans")
    }

    /// Adds fallback families after those supplied by each text style.
    pub fn with_fallback_families(
        mut self,
        families: impl IntoIterator<Item = impl Into<String>>,
    ) -> Self {
        self.additional_fallbacks = families.into_iter().map(Into::into).collect();
        self
    }

    /// REV16 (browser-core 43): lays out a document with the rev-15
    /// browser text fields — the side-channel the pinned gpui types
    /// cannot carry (module docs, divergence 4). The per-run
    /// word-break / overflow-wrap tags become Parley range styles and
    /// the document-level alignment tag drives Parley's `Alignment`;
    /// the pinned `TextLayoutRequest` stays untouched. Returns the
    /// `LineLayout` plus the document's base direction (`is_rtl`, the
    /// `paragraphs[0]` rule) so the caller can resolve start/end
    /// alignment at paint time. Fails exactly where the pinned
    /// `layout_text` panics (its `expect`); the ABI turns the failure
    /// into a typed status before this call.
    pub fn layout_text_with_browser(
        &self,
        request: TextLayoutRequest<'_>,
        browser: BrowserLayoutParams<'_>,
    ) -> Result<BrowserLayoutResult> {
        let result = self.parley_layout(ParleyLayoutParams {
            text: request,
            inline: None,
            browser,
        })?;
        Ok(BrowserLayoutResult { layout: result.layout, is_rtl: result.is_rtl })
    }

    fn resolve_canonical_font(&self, descriptor: &Font) -> Result<FontId> {
        let mut families = Vec::with_capacity(
            1 + descriptor
                .fallbacks
                .as_ref()
                .map_or(0, |fallbacks| fallbacks.fallback_list().len()),
        );
        push_face_families(
            &mut families,
            descriptor.family.as_ref(),
            &self.system_font_fallback,
        );

        if let Some(fallbacks) = &descriptor.fallbacks {
            for family in fallbacks.fallback_list() {
                push_face_families(&mut families, family, &self.system_font_fallback);
            }
        }

        for family in &self.additional_fallbacks {
            push_face_families(&mut families, family, &self.system_font_fallback);
        }

        let resolved = self
            .catalog
            .resolve(&FaceRequest {
                families: &families,
                weight: descriptor.weight.0,
                style: descriptor.style,
                character: None,
            })
            .with_context(|| format!("Fontique could not resolve '{}'", descriptor.family))?;
        let font_id = self.fonts.write().intern_synthesized(
            resolved.data,
            resolved.index,
            resolved.synthesis,
        )?;
        Ok(font_id)
    }

    fn parley_layout(&self, params: ParleyLayoutParams<'_>) -> Result<ParleyLayoutResult> {
        let TextLayoutRequest {
            text,
            font_size,
            runs,
            line_clamp,
            ..
        } = params.text;
        let (inline_boxes, text_styles) =
            params.inline.as_ref().map_or((&[][..], &[][..]), |inline| {
                (inline.boxes, inline.text_styles)
            });

        // Parley 0.11 resolves one base direction per layout, including across newlines.
        if !text.chars().any(is_paragraph_separator) {
            return self.parley_paragraph_layout(params);
        }

        let run_ranges = run_ranges(runs);

        let mut paragraphs = Vec::new();
        let mut visual_lines = Vec::new();
        let mut paint_fragments = Vec::new();
        let mut inline_lines = Vec::new();
        let mut positioned_inline_boxes = Vec::new();

        let mut document_size = Size::<Pixels>::default();
        let mut width = Pixels::ZERO;
        let mut ascent = Pixels::ZERO;
        let mut descent = Pixels::ZERO;

        for source in paragraph_ranges(text) {
            let first_run = run_ranges.partition_point(|range| range.end <= source.content.start);
            let mut paragraph_runs = run_ranges
                .iter()
                .enumerate()
                .skip(first_run)
                .take_while(|(_idx, range)| range.start < source.content.end)
                .filter_map(|(idx, range)| {
                    let local = local_range(range, &source.content)?;

                    Some(TextRun {
                        len: local.len(),
                        ..runs[idx].clone()
                    })
                })
                .collect::<Vec<_>>();
            let mut paragraph_styles = text_styles
                .iter()
                .filter_map(|style| {
                    let range = local_range(&style.range, &source.content)?;

                    Some(InlineTextStyle {
                        range,
                        ..style.clone()
                    })
                })
                .collect::<Vec<_>>();

            if source.content.is_empty() {
                if let Some(run) = runs.get(first_run.min(runs.len().saturating_sub(1))) {
                    paragraph_runs.push(TextRun {
                        len: 0,
                        ..run.clone()
                    });
                }

                let style_idx = source.content.start.min(text.len().saturating_sub(1));
                paragraph_styles.extend(
                    text_styles
                        .iter()
                        .filter(|style| style.range.contains(&style_idx))
                        .map(|style| InlineTextStyle {
                            range: 0..0,
                            ..style.clone()
                        }),
                );
            }

            let paragraph_boxes = inline_boxes
                .iter()
                .filter(|inline_box| {
                    inline_box.index >= source.content.start
                        && (inline_box.index < source.separator.end
                            || source.separator.is_empty() && inline_box.index == text.len())
                })
                .map(|inline_box| InlineBoxRequest {
                    index: inline_box.index.min(source.content.end) - source.content.start,
                    ..*inline_box
                })
                .collect::<Vec<_>>();

            // REV16: the paragraph's slice of the browser run fields —
            // the same skip(first_run)/take_while window the paragraph
            // runs were collected through, so the per-run word-break /
            // overflow-wrap tags stay on their own text. (A run the
            // filter_map dropped for lying outside the paragraph keeps
            // its slot here; the paragraph layout indexes the fields by
            // the PARAGRAPH run list, and a dropped run shifts the
            // window — the same offset the runs themselves took.)
            let paragraph_browser_runs = params
                .browser
                .runs
                .iter()
                .enumerate()
                .skip(first_run)
                .take_while(|(idx, _fields)| {
                    run_ranges
                        .get(*idx)
                        .is_some_and(|range| range.start < source.content.end)
                })
                .filter_map(|(idx, fields)| {
                    let range = run_ranges.get(idx)?;
                    local_range(range, &source.content)?;
                    Some(*fields)
                })
                .collect::<Vec<_>>();
            let mut result = self.parley_paragraph_layout(ParleyLayoutParams {
                text: TextLayoutRequest {
                    text: &text[source.content.clone()],
                    runs: &paragraph_runs,
                    // Clamping stops soft wrapping after the budget, but preserves hard breaks.
                    line_clamp: line_clamp.map(|count| count.saturating_sub(visual_lines.len())),
                    ..params.text
                },
                inline: params
                    .inline
                    .as_ref()
                    .map(|inline| ParleyInlineLayoutParams {
                        boxes: &paragraph_boxes,
                        text_styles: &paragraph_styles,
                        ..*inline
                    }),
                // REV16: the sliced run fields; the alignment tag is
                // document-level and passes through unchanged.
                browser: BrowserLayoutParams {
                    runs: &paragraph_browser_runs,
                    align: params.browser.align,
                },
            })?;

            let first_line = visual_lines.len();
            let first_fragment = paint_fragments.len();
            let block_offset = document_size.height;

            for line in &mut result.layout.visual_lines {
                line.text_range.start += source.content.start;
                line.text_range.end += source.content.start;
                line.fragment_range.start += first_fragment;
                line.fragment_range.end += first_fragment;
            }

            if let Some(line) = result.layout.visual_lines.last_mut() {
                // Keep separators in document indices without adding native trailing rows.
                line.text_range.end = source.separator.end;
            }

            for line in &mut result.inline_lines {
                line.origin.y += block_offset;
            }

            for inline_box in &mut result.inline_boxes {
                inline_box.line_index += first_line;
                inline_box.bounds.origin.y += block_offset;
            }

            width = width.max(result.layout.width);
            ascent = ascent.max(result.layout.ascent);
            descent = descent.max(result.layout.descent);
            document_size.width = document_size.width.max(result.size.width);
            document_size.height += result.size.height;

            let last_line = result.inline_lines.last().unwrap();
            let newline_width = (result.layout.ascent + result.layout.descent) * 0.25;
            let newline = if result.is_rtl {
                last_line.origin.x - newline_width..last_line.origin.x
            } else {
                let newline_x = last_line.origin.x + last_line.size.width;

                newline_x..newline_x + newline_width
            };

            paragraphs.push(ParagraphLayout {
                source,
                first_line,
                block_offset,
                native: result.layout.platform_layout,
                newline,
                is_rtl: result.is_rtl,
            });
            visual_lines.extend(result.layout.visual_lines);
            paint_fragments.extend(result.layout.paint_fragments);
            inline_lines.extend(result.inline_lines);
            positioned_inline_boxes.extend(result.inline_boxes);
        }

        let is_rtl = paragraphs[0].is_rtl;
        let platform_layout = ParleyDocumentLayout::new(paragraphs, text, document_size);

        Ok(ParleyLayoutResult {
            layout: LineLayout {
                font_size,
                width,
                ascent,
                descent,
                visual_lines: visual_lines.into_iter().collect(),
                paint_fragments,
                len: text.len(),
                platform_layout: std::sync::Arc::new(platform_layout),
            },
            inline_lines,
            inline_boxes: positioned_inline_boxes,
            size: document_size,
            is_rtl,
        })
    }

    fn parley_paragraph_layout(
        &self,
        params: ParleyLayoutParams<'_>,
    ) -> Result<ParleyLayoutResult> {
        let TextLayoutRequest {
            text,
            font_size,
            runs,
            wrap_width,
            line_clamp,
        } = params.text;

        let wrap = (wrap_width.is_some() || line_clamp.is_some())
            .then_some((wrap_width.unwrap_or(Pixels::MAX), line_clamp));

        let (inline_boxes, text_styles, line_height, inline_text_metrics, text_align) =
            match params.inline.as_ref() {
                Some(inline) => (
                    inline.boxes,
                    inline.text_styles,
                    Some(inline.line_height),
                    Some(inline.text_metrics),
                    Some(inline.text_align),
                ),
                None => (&[][..], &[][..], None, None, None),
            };

        let run_ranges = run_ranges(runs);
        let line_height = if text.is_empty() {
            text_styles
                .last()
                .map(|style| style.line_height)
                .or(line_height)
        } else {
            line_height
        };

        let family_lists = runs
            .iter()
            .map(|run| {
                let descriptor = &run.font;
                let mut families = Vec::new();
                push_parley_families(
                    &mut families,
                    descriptor.family.as_ref(),
                    &self.system_font_fallback,
                );

                if let Some(fallbacks) = &descriptor.fallbacks {
                    for family in fallbacks.fallback_list() {
                        push_parley_families(&mut families, family, &self.system_font_fallback);
                    }
                }

                families.extend(
                    self.additional_fallbacks
                        .iter()
                        .map(|family| FontFamilyName::Named(Cow::Borrowed(family.as_str()))),
                );
                families
            })
            .collect::<Vec<_>>();
        let feature_lists = runs
            .iter()
            .map(|run| {
                let descriptor = &run.font;
                descriptor
                    .features
                    .tag_value_list()
                    .iter()
                    .map(|(tag, value)| {
                        let tag = Tag::parse(tag)
                            .with_context(|| format!("invalid OpenType feature tag '{tag}'"))?;
                        let value = (*value).try_into().with_context(|| {
                            format!("OpenType feature '{tag}' value is larger than u16")
                        })?;

                        Ok(FontFeature::new(tag, value))
                    })
                    .collect::<Result<Vec<_>>>()
            })
            .collect::<Result<Vec<_>>>()?;

        let mut state = self.parley.lock();
        let ParleyState { fonts, layout } = &mut *state;
        let mut builder = layout.ranged_builder(fonts, text, 1.0, false);
        // REV16 divergence (browser-core 43, recorded in mod.rs): the
        // Chromium ASCII pair override suppresses the BreakAll
        // segmenter's every-cluster boundaries — its table sets every
        // printable-ASCII pair to no-break (`with_pairs(ALL, ALL,
        // false)`), and parley applies the override AFTER the segmenter
        // with no style scoping, so `word-break: break-all` could never
        // break ASCII text while the override is pinned. Chrome's own
        // break-all path consults its fast pair table first but then
        // OVERRIDES it with the break-all line-break class table, which
        // allows AL×AL breaks (`ShouldBreakAfterBreakAll`,
        // third_party/blink/renderer/platform/text/text_break_iterator.cc:125-149
        // and :328-336 at 0d4dcd4f) — so a document carrying a BreakAll
        // run drops the override (the segmenter's own BreakAll rules
        // stand, exactly Chrome's class-table outcome) while every other
        // document keeps the pinned override. A mixed normal+break-all
        // document also loses the override's punctuation rules for its
        // normal runs — the recorded cost of parley's un-scoped
        // override hook.
        let break_all_document = params
            .browser
            .runs
            .iter()
            .any(|fields| fields.word_break == parley::WordBreak::BreakAll);
        if break_all_document {
            builder.set_line_break_override(None);
        } else {
            builder.set_line_break_override(Some(CHROMIUM_LINE_BREAK_OVERRIDE));
        }
        builder.push_default(StyleProperty::FontSize(f32::from(font_size)));

        if let Some(line_height) = line_height {
            builder.push_default(StyleProperty::LineHeight(LineHeight::Absolute(f32::from(
                line_height,
            ))));
        }

        let mut push_style = |property, range| {
            if text.is_empty() {
                builder.push_default(property);
            } else {
                builder.push(property, range);
            }
        };

        for (run_index, run) in runs.iter().enumerate() {
            let descriptor = &run.font;
            let range = run_ranges[run_index].clone();
            push_style(
                StyleProperty::FontFamily(FontFamily::from(family_lists[run_index].as_slice())),
                range.clone(),
            );
            push_style(
                StyleProperty::FontWeight(FontWeight::new(descriptor.weight.0)),
                range.clone(),
            );
            push_style(
                StyleProperty::FontStyle(match descriptor.style {
                    gpui::FontStyle::Normal => FontStyle::Normal,
                    gpui::FontStyle::Italic => FontStyle::Italic,
                    gpui::FontStyle::Oblique => FontStyle::Oblique(None),
                }),
                range.clone(),
            );

            if !feature_lists[run_index].is_empty() {
                push_style(
                    StyleProperty::FontFeatures(FontFeatures::from(
                        feature_lists[run_index].as_slice(),
                    )),
                    range.clone(),
                );
            }

            if let Some(letter_spacing) = run.letter_spacing {
                push_style(
                    StyleProperty::LetterSpacing(f32::from(letter_spacing)),
                    range.clone(),
                );
            }

            // REV16 (browser-core 43): the side-channel run fields become
            // Parley range styles — Parley 0.11's own `WordBreak`/
            // `OverflowWrap` properties (parlance's CSS text enums), pushed
            // ONLY when the tag differs from the default so the pinned
            // no-fields path stays byte-identical. The Chromium line-break
            // class override stays pinned above (set_line_break_override);
            // these range styles select Parley's word-segmentation and
            // emergency-breaking behavior on top of it.
            if let Some(fields) = params.browser.runs.get(run_index) {
                if fields.word_break != parley::WordBreak::Normal {
                    push_style(StyleProperty::WordBreak(fields.word_break), range.clone());
                }
                if fields.overflow_wrap != parley::OverflowWrap::Normal {
                    push_style(
                        StyleProperty::OverflowWrap(fields.overflow_wrap),
                        range.clone(),
                    );
                }
            }

            let paint_style = PaintStyle::from(run);
            push_style(StyleProperty::Brush(paint_style.clone()), range.clone());

            if let Some(underline) = run.underline {
                push_style(StyleProperty::Underline(true), range.clone());
                push_style(
                    StyleProperty::UnderlineSize(Some(underline.thickness.into())),
                    range.clone(),
                );
                push_style(
                    StyleProperty::UnderlineBrush(Some(paint_style.clone())),
                    range.clone(),
                );
            }

            if let Some(strikethrough) = run.strikethrough {
                push_style(StyleProperty::Strikethrough(true), range.clone());
                push_style(
                    StyleProperty::StrikethroughSize(Some(strikethrough.thickness.into())),
                    range.clone(),
                );
                push_style(
                    StyleProperty::StrikethroughBrush(Some(paint_style)),
                    range.clone(),
                );
            }
        }

        for style in text_styles {
            push_style(
                StyleProperty::FontSize(f32::from(style.font_size)),
                style.range.clone(),
            );

            push_style(
                StyleProperty::LineHeight(LineHeight::Absolute(f32::from(style.line_height))),
                style.range.clone(),
            );
        }

        for inline_box in inline_boxes {
            if inline_box.index > text.len() || !text.is_char_boundary(inline_box.index) {
                anyhow::bail!("inline box index does not align with the input text");
            }

            builder.push_inline_box(InlineBox {
                id: inline_box.id,
                kind: InlineBoxKind::InFlow,
                index: inline_box.index,
                width: f32::from(inline_box.size.width),
                height: f32::from(inline_box.size.height),
            });
        }

        let mut layout = builder.build(text);

        if let Some((wrap_width, max_lines)) = wrap {
            if let Some(max_lines) = max_lines {
                let mut breaker = layout.break_lines();
                breaker.state_mut().set_layout_max_advance(f32::MAX);
                breaker
                    .state_mut()
                    .set_line_max_advance(f32::from(wrap_width));

                for _ in 0..max_lines.saturating_sub(1) {
                    if breaker.break_next().is_none() {
                        break;
                    }
                }

                breaker.break_remaining(f32::MAX);
            } else {
                layout.break_all_lines(Some(f32::from(wrap_width)));
            }
        } else {
            layout.break_all_lines(None);
        }

        // Parley uses an unbounded line width for empty layouts. Align their empty
        // row here so centered and right-aligned carets stay inside the container.
        let empty_alignment = (text.is_empty() && inline_boxes.is_empty()).then(|| {
            let width = wrap
                .map(|(width, _max_lines)| width)
                .filter(|width| *width < Pixels::MAX)
                .unwrap_or_default();

            match text_align {
                Some(TextAlign::Right) => width,
                Some(TextAlign::Center) => width / 2.,
                _ => match params.browser.align {
                    // REV16: the side-channel tag's empty-row rule (an
                    // empty layout carries no direction content, so end
                    // anchors at the wrap width like Right, start and
                    // justify at zero like Left).
                    BrowserTextAlign::End => width,
                    BrowserTextAlign::Start | BrowserTextAlign::Justify => Pixels::ZERO,
                    BrowserTextAlign::None => Pixels::ZERO,
                },
            }
        });

        // REV16: the effective alignment — the side-channel tag first
        // (the ABI entry this vendored system serves for browser-core
        // 43), else the inline params' pinned `TextAlign`. `Justify`
        // stretches the whitespace clusters inside the layout (the
        // last line of the paragraph — BreakReason::None/Explicit —
        // stays start-aligned); `Start`/`End` resolve against the
        // layout's base direction (Parley's own rule).
        let browser_alignment = match params.browser.align {
            BrowserTextAlign::None => None,
            BrowserTextAlign::Start => Some(Alignment::Start),
            BrowserTextAlign::End => Some(Alignment::End),
            BrowserTextAlign::Justify => Some(Alignment::Justify),
        };
        let inline_alignment = text_align.map(|text_align| match text_align {
            TextAlign::Left => Alignment::Left,
            TextAlign::Center => Alignment::Center,
            TextAlign::Right => Alignment::Right,
        });

        if let Some(alignment) = browser_alignment.or(inline_alignment)
            && empty_alignment.is_none()
        {
            layout.align(alignment, AlignmentOptions::default());
        }

        let mut visual_lines = Vec::new();
        let mut paint_fragments = Vec::new();

        let mut positioned_inline_boxes = Vec::new();
        let mut inline_lines = Vec::new();
        let mut inline_line_metrics = Vec::new();
        let mut inline_text_bounds = Vec::new();

        let mut width = px(0.0);
        let mut ascent = px(0.0);
        let mut descent = px(0.0);

        let mut saw_line = false;

        for (line_index, line) in layout.lines().enumerate() {
            saw_line = true;

            let fragment_start = paint_fragments.len();
            let metrics = *line.metrics();
            let line_x =
                empty_alignment.unwrap_or_else(|| px(metrics.inline_min_coord + metrics.offset));
            let line_advance = if empty_alignment.is_some() {
                Pixels::ZERO
            } else {
                px(metrics.advance)
            };

            let mut text_metrics = inline_text_metrics.unwrap_or_default();
            let leading =
                ((line_height.unwrap_or_default() - text_metrics.ascent - text_metrics.descent)
                    / 2.)
                    .max(Pixels::ZERO);

            let mut text_top = -text_metrics.ascent - leading;
            let mut text_bottom = text_metrics.descent + leading;

            for item in line.items() {
                let PositionedLayoutItem::GlyphRun(glyph_run) = item else {
                    let PositionedLayoutItem::InlineBox(inline_box) = item else {
                        unreachable!();
                    };

                    positioned_inline_boxes.push(PositionedInlineBox {
                        id: inline_box.id,
                        line_index,
                        bounds: Bounds::new(
                            point(px(inline_box.x), px(inline_box.y)),
                            size(px(inline_box.width), px(inline_box.height)),
                        ),
                    });

                    continue;
                };

                let run = glyph_run.run();
                let normalized_coords = run
                    .normalized_coords()
                    .iter()
                    .copied()
                    .map(NormalizedCoord::from_bits)
                    .collect::<Vec<_>>();
                let font_id = self.fonts.write().intern(
                    run.font().data.clone(),
                    run.font().index,
                    &normalized_coords,
                    run.synthesis(),
                )?;

                let baseline = glyph_run.baseline();
                let run_metrics = glyph_run.run().metrics();

                // Resolve leading from the input ranges. Parley 0.11 can report the
                // following style's line height in a shaped run's cached metrics.
                let range = run.text_range();

                let run_line_height = text_styles
                    .iter()
                    .filter(|style| style.range.start < range.end && style.range.end > range.start)
                    .map(|style| f32::from(style.line_height))
                    .reduce(f32::max)
                    .unwrap_or_else(|| {
                        line_height
                            .map(f32::from)
                            .unwrap_or(run_metrics.line_height)
                    });

                let half_leading =
                    ((run_line_height - run_metrics.ascent - run_metrics.descent) / 2.).max(0.);

                text_metrics.ascent = text_metrics.ascent.max(px(run_metrics.ascent));
                text_metrics.descent = text_metrics.descent.max(px(run_metrics.descent));
                text_top = text_top.min(px(-run_metrics.ascent - half_leading));
                text_bottom = text_bottom.max(px(run_metrics.descent + half_leading));

                if text.is_empty() {
                    continue;
                }

                let parley_style = glyph_run.style();
                let mut paint_style = parley_style.brush.clone();
                let underline_offset = parley_style.underline.as_ref().map(|decoration| {
                    let mut underline = decoration.brush.underline.unwrap_or_default();
                    underline.thickness = px(decoration.size.unwrap_or(run_metrics.underline_size));
                    paint_style.underline = Some(underline);
                    px(decoration.offset.unwrap_or(run_metrics.underline_offset))
                });

                let strikethrough_offset = parley_style.strikethrough.as_ref().map(|decoration| {
                    let mut strikethrough = decoration.brush.strikethrough.unwrap_or_default();
                    strikethrough.thickness =
                        px(decoration.size.unwrap_or(run_metrics.strikethrough_size));
                    paint_style.strikethrough = Some(strikethrough);
                    px(decoration
                        .offset
                        .unwrap_or(run_metrics.strikethrough_offset))
                });

                let glyphs = {
                    let fonts = self.fonts.read();
                    let color_glyphs = fonts
                        .get(font_id)
                        .context("canonical font missing after interning")?
                        .color_glyphs()?;
                    let rasterizer = self.rasterizer.lock();
                    glyph_run
                        .positioned_glyphs()
                        .map(|glyph| {
                            let id = GlyphId(glyph.id);
                            ShapedGlyph {
                                id,
                                position: point(px(glyph.x) - line_x, px(glyph.y - baseline)),
                                is_emoji: color_glyphs
                                    .kind(id)
                                    .is_some_and(|kind| rasterizer.supports_color_glyph(kind)),
                            }
                        })
                        .collect()
                };

                let start = px(glyph_run.offset()) - line_x;
                paint_fragments.push(PaintFragment {
                    font_id,
                    font_size: px(run.font_size()),
                    glyphs,
                    x_range: start..start + px(glyph_run.advance()),
                    style: paint_style,
                    underline_offset,
                    strikethrough_offset,
                });
            }

            let parley_text_range = line.text_range();
            let text_range =
                parley_text_range.start.min(text.len())..parley_text_range.end.min(text.len());

            visual_lines.push(VisualLine {
                text_range,
                fragment_range: fragment_start..paint_fragments.len(),
                advance_width: line_advance,
            });

            inline_lines.push(InlineVisualLine {
                origin: point(line_x, px(metrics.block_min_coord)),
                size: size(
                    line_advance,
                    px(metrics.block_max_coord - metrics.block_min_coord),
                ),
                baseline: px(metrics.baseline - metrics.block_min_coord),
            });

            inline_line_metrics.push(text_metrics);
            inline_text_bounds.push((text_top, text_bottom));

            width = width.max(line_advance);
            ascent = ascent.max(px(metrics.ascent));
            descent = descent.max(px(metrics.descent));
        }

        if !saw_line {
            anyhow::bail!("Parley produced no line");
        }

        let mut size = size(px(layout.width()), px(layout.height()));

        if empty_alignment.is_some() {
            size.width = Pixels::ZERO;
        }

        if let (Some(text_metrics), Some(line_height)) = (inline_text_metrics, line_height) {
            align_inline_boxes(
                &mut inline_lines,
                &mut positioned_inline_boxes,
                &mut size,
                inline_boxes,
                &inline_line_metrics,
                &inline_text_bounds,
                text_metrics,
                line_height,
            );
        }

        let is_rtl = layout.is_rtl();
        let platform_layout = ParleyLayout::new(layout, text, inline_lines.clone());
        let line_layout = LineLayout {
            font_size,
            width,
            ascent,
            descent,
            visual_lines: visual_lines.iter().cloned().collect(),
            paint_fragments,
            len: text.len(),
            platform_layout: std::sync::Arc::new(platform_layout),
        };

        Ok(ParleyLayoutResult {
            layout: line_layout,
            inline_lines,
            inline_boxes: positioned_inline_boxes,
            size,
            is_rtl,
        })
    }
}

fn run_ranges(runs: &[TextRun]) -> Vec<Range<usize>> {
    let mut start = 0;

    runs.iter()
        .map(|run| {
            let range = start..start + run.len;
            start = range.end;

            range
        })
        .collect()
}

fn push_face_families<'a>(
    families: &mut Vec<FaceFamily<'a>>,
    name: &'a str,
    system_font_fallback: &'a str,
) {
    if name == ".SystemUIFont" {
        families.push(FaceFamily::SystemUi);
        families.push(FaceFamily::Named(system_font_fallback));
    } else {
        families.push(FaceFamily::Named(name));
    }
}

fn push_parley_families<'a>(
    families: &mut Vec<FontFamilyName<'a>>,
    name: &'a str,
    system_font_fallback: &str,
) {
    if name == ".SystemUIFont" {
        families.push(FontFamilyName::Generic(GenericFamily::SystemUi));
        families.push(FontFamilyName::Named(Cow::Owned(
            system_font_fallback.to_string(),
        )));
    } else {
        families.push(FontFamilyName::Named(Cow::Owned(name.to_string())));
    }
}

impl PlatformTextSystem for ParleyTextSystem {
    fn add_fonts(&self, fonts: Vec<Cow<'static, [u8]>>) -> Result<()> {
        let blobs = fonts
            .iter()
            .map(|bytes| fontique::Blob::from(bytes.as_ref().to_vec()))
            .collect::<Vec<_>>();
        let mut state = self.catalog.state.write();
        let mut next: CatalogState = state.clone();
        next.register_blobs(&blobs)?;
        *state = next;
        Ok(())
    }

    fn all_font_names(&self) -> Vec<String> {
        let mut names = self.catalog.family_names();
        names.push(".SystemUIFont".to_owned());
        names.sort_unstable();
        names.dedup();
        names
    }

    fn font_generation(&self) -> u64 {
        self.catalog.generation()
    }

    fn font_id(&self, descriptor: &Font) -> Result<FontId> {
        self.resolve_canonical_font(descriptor)
    }

    fn font_metrics(&self, font_id: FontId) -> FontMetrics {
        self.fonts
            .read()
            .get(font_id)
            .expect("Parley FontId missing from its store")
            .metrics()
            .expect("stored font failed Skrifa metrics")
    }

    fn typographic_bounds(&self, font_id: FontId, glyph_id: GlyphId) -> Result<Bounds<f32>> {
        self.fonts
            .read()
            .get(font_id)
            .context("Parley FontId missing from its store")?
            .glyph_bounds(glyph_id)
    }

    fn advance(&self, font_id: FontId, glyph_id: GlyphId) -> Result<Size<f32>> {
        self.fonts
            .read()
            .get(font_id)
            .context("Parley FontId missing from its store")?
            .advance(glyph_id)
    }

    fn glyph_for_char(&self, font_id: FontId, ch: char) -> Option<GlyphId> {
        self.fonts.read().get(font_id)?.glyph_for_char(ch).ok()?
    }

    fn rasterize_glyph(&self, params: &RenderGlyphParams) -> Result<RasterizedGlyph> {
        let fonts = self.fonts.read();
        let font = fonts
            .get(params.font_id)
            .context("Parley FontId missing from its store")?;
        let data_identity = font.data_identity();
        let face_index = font.index;
        let variations = font.variations.clone();
        self.rasterizer
            .lock()
            .rasterize(font.raster_face(params.font_id), params)
            .with_context(|| {
                format!(
                    "native rasterization failed for FontId {:?}, data identity {data_identity}, face index {face_index}, variations {variations:?}",
                    params.font_id
                )
            })
    }

    fn prepare_raster_style(&self, request: RasterStyleRequest) -> PreparedRasterStyle {
        self.rasterizer.lock().prepare_style(request)
    }

    fn recommended_rendering_mode(
        &self,
        _font_id: FontId,
        _font_size: Pixels,
    ) -> TextRenderingMode {
        self.rasterizer.lock().recommended_mode()
    }

    fn layout_text(&self, request: TextLayoutRequest<'_>) -> LineLayout {
        self.parley_layout(ParleyLayoutParams {
            text: request,
            inline: None,
            // REV16: the trait entry keeps the pinned, field-free
            // behavior (the browser fields reach the layout through
            // layout_text_with_browser).
            browser: BrowserLayoutParams::default(),
        })
        .expect("Parley failed to lay out a validated GPUI document")
        .layout
    }

    fn layout_inline(&self, request: InlineLayoutRequest<'_>) -> InlineLayout {
        let result = self
            .parley_layout(ParleyLayoutParams {
                text: TextLayoutRequest {
                    text: request.text,
                    font_size: request.font_size,
                    runs: request.runs,
                    wrap_width: request.wrap_width,
                    line_clamp: request.line_clamp,
                },
                inline: Some(ParleyInlineLayoutParams {
                    boxes: request.boxes,
                    text_styles: request.text_styles,
                    line_height: request.line_height,
                    text_metrics: request.text_metrics,
                    text_align: request.text_align,
                }),
                // REV16: the pinned, field-free behavior for the trait
                // entry.
                browser: BrowserLayoutParams::default(),
            })
            .expect("Parley failed to lay out a validated GPUI inline document");
        InlineLayout {
            layout: std::sync::Arc::new(result.layout),
            alignment_offset: inline_alignment_offset(request.text_align, &result.inline_lines),
            lines: result.inline_lines,
            boxes: result.inline_boxes,
            size: result.size,
        }
    }
}

