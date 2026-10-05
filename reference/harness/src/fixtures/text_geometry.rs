//! Fixture `text-geometry-v1`: reference text geometry through the REAL
//! pinned text stack.
//!
//! ## The public oracle boundary
//!
//! The reference text stack is observable through gpui's PUBLIC text API,
//! exactly as the pinned Windows platform constructs it:
//!
//! * **Construction** — `gpui_ce_parley::ParleyTextSystem::new_with_rasterizer(SystemFonts::Load, "Segoe UI", SwashGlyphRasterizer::default()).with_fallback_families(["Lilex", "IBM Plex Sans", "Arial"])`, wrapped in `gpui::TextSystem::new(Arc<dyn PlatformTextSystem>)` and
//!   `gpui::WindowTextSystem::new(Arc<TextSystem>)`. This mirrors
//!   `crates/gpui_windows/src/platform.rs::WindowsPlatform::new` (lines
//!   117-126 at the pin) with the one deviation the native text service
//!   also records: the rasterizer argument is the portable
//!   `SwashGlyphRasterizer` (the pin's `WindowsGlyphRasterizer` is
//!   `pub(crate)` in `gpui_windows`), and no entry here rasterizes
//!   anything — this fixture is CPU geometry only.
//! * **Shaping** — `WindowTextSystem::shape_text(text, font_size, runs,
//!   wrap_width, line_clamp) -> Result<WrappedLine>` (text_system.rs). No
//!   window or app context is required: the text system is constructed
//!   directly, once, before the first case.
//! * **Lines/runs** — `LineLayout { font_size, width, ascent, descent,
//!   visual_lines, paint_fragments, len, platform_layout }`,
//!   `VisualLine { text_range, fragment_range, advance_width }` and
//!   `PaintFragment { font_id, font_size, glyphs, x_range, .. }`
//!   (line_layout.rs), all public fields reached through `WrappedLine`'s
//!   deref chain.
//! * **Carets/hit tests/selection** — `WrappedLineLayout`'s public query
//!   methods: `closest_caret_for_pixel_point`,
//!   `byte_index_for_pixel_point`, `logical_cluster_before`,
//!   `logical_cluster_after`, `selection_bounds` (line_layout.rs), plus
//!   `PlatformTextLayout::caret_bounds` through the public
//!   `LineLayout::platform_layout` field for the caret's full bounds.
//! * **Fonts** — `PlatformTextSystem::font_id` (the typed resolve;
//!   `TextSystem::resolve_font` panics on failure), `font_metrics`
//!   (the full `gpui::FontMetrics` in font units), `font_generation`,
//!   `all_font_names`, and the pixel-scaled `TextSystem::ascent` /
//!   `descent` / `x_height` accessors.
//!
//! ## API discoveries recorded by this fixture (public vs pub(crate))
//!
//! * **No stretch axis**: the pinned `gpui::Font` descriptor carries
//!   family, features, fallbacks, weight and style — no stretch field
//!   exists, so `font-resolved` events echo weight and style only.
//! * **The selected face is not observable**: resolving a descriptor
//!   yields the canonical `FontId` (`FontStore::intern`:
//!   `FontId(1 << 63 | store index)`, store.rs). Face index, data
//!   identity, variation axes and Fontique's synthesis decision are
//!   `pub(crate)` in `gpui_ce_parley`; the actually-selected weight/style
//!   cannot be read back through the public API. `font-resolved` events
//!   therefore record the FontId handle (process-stable: the store only
//!   interns) plus the echoed descriptor; per-face drift is detected
//!   through the recorded `FontMetrics` instead.
//! * **Run direction is not public**: `LineLayout`/`PaintFragment` carry
//!   no direction. Bidi behavior is observable only through the exposed
//!   caret/hit-test geometry (positions and affinities).
//! * **Fragment byte ranges are not public**: `PaintFragment` exposes a
//!   font id, font size, glyph positions and an `x_range`, but no text
//!   range; only `VisualLine` carries `text_range`. `text-run` events
//!   record exactly the public fragment facts.
//! * **Per-glyph advances are not public**: `ShapedGlyph { id, position,
//!   is_emoji }` has no advance field. The pen advance between glyphs is
//!   the difference of positions; each fragment's `x_range` carries the
//!   run extent. (This fixture records glyph counts only; per-glyph
//!   records belong to the raster ticket.)
//! * **Clusters are graphemes in this pin**: `logical_cluster_before` /
//!   `logical_cluster_after` return grapheme byte ranges
//!   (`paragraphs.rs` builds them from `grapheme_indices(true)`), i.e.
//!   one backend-defined caret step.
//! * **Line height is not a shaping input**: `shape_text` takes no line
//!   height. The caller's line height parameterizes caret/hit-test/
//!   selection geometry and row placement (the `line_height: Pixels`
//!   arguments). Row `i` occupies `[i*line_height, (i+1)*line_height)`
//!   with `baseline = (line_height - ascent - descent)/2 + ascent` from
//!   the row top (`paint_visual_text`, line.rs) — recorded per
//!   `text-line` event exactly as the native text service records it.
//! * **`byte_index_for_pixel_point` vs `closest_caret_for_pixel_point`**:
//!   the byte-index hit test returns the logical start of the cluster
//!   under the point and fails outside the row; the caret hit test
//!   returns the closest caret. Both are public and both are recorded in
//!   `text-hit` events.
//!
//! Determinism: the text system is constructed once per harness process;
//! `SystemFonts::Load` performs Fontique's real DirectWrite-backed system
//! font enumeration, and the `FontStore` interns faces in resolution
//! order, so FontIds are stable for a fixed case order on one machine.
//! Every case records its font identity (descriptor echo, FontId and the
//! full face metrics), and the `text-system-begin` event records the
//! font catalog shape (family count plus the presence of the families
//! this fixture depends on), so machine drift is detectable from the
//! trace alone.

use crate::envelope::{Envelope, FeatureIn, FontDescriptorIn, FontOverrideIn, TextGeometryCase};
use crate::trace::{FieldValue, TraceRecorder};
use anyhow::{Result, bail};
use gpui::{
    CaretAffinity, CaretPosition, Font, FontFallbacks, FontFeatures, FontStyle, FontWeight,
    LineLayout, Pixels, PlatformTextSystem, Point, TextRun, TextSystem, WindowTextSystem, px,
};
use gpui_ce_parley::{ParleyTextSystem, SwashGlyphRasterizer, SystemFonts};
use std::ops::Range;
use std::sync::Arc;

/// Caret query modes (see `TextGeometryCase::caret_mode`).
const CARET_MODE_EXPLICIT: &str = "explicit";
const CARET_MODE_ALL_CLUSTERS: &str = "all-clusters";
const CARET_MODE_ALL_CLUSTERS_BOTH: &str = "all-clusters-both";

/// Caret affinity names, matching the `CaretAffinity` variants.
const AFFINITY_DOWNSTREAM: &str = "downstream";
const AFFINITY_UPSTREAM: &str = "upstream";

/// Style names, matching the `FontStyle` variants.
const STYLE_NORMAL: &str = "Normal";
const STYLE_ITALIC: &str = "Italic";
const STYLE_OBLIQUE: &str = "Oblique";

/// Font roles of `font-resolved` events.
const ROLE_DEFAULT: &str = "default";
const ROLE_CASE: &str = "case";
const ROLE_RUN: &str = "run";

/// Font metric sources of `font-metrics` events.
const METRIC_SOURCE_CASE: &str = "case";
const METRIC_SOURCE_FRAGMENT: &str = "fragment";

/// Families whose catalog presence is recorded for machine-drift
/// detection: the system UI font, its emoji companion, the last service
/// fallback, the common CJK fallbacks and the two named service
/// fallbacks.
const CATALOG_PROBE_FAMILIES: [&str; 7] = [
    "Segoe UI",
    "Segoe UI Emoji",
    "Arial",
    "Microsoft YaHei",
    "Microsoft YaHei UI",
    "Lilex",
    "IBM Plex Sans",
];

/// The pinned Windows platform's fallback families
/// (gpui_windows/src/platform.rs).
const SERVICE_FALLBACKS: [&str; 3] = ["Lilex", "IBM Plex Sans", "Arial"];

/// The pinned platform's system font family argument.
const SERVICE_SYSTEM_FONT_FAMILY: &str = "Segoe UI";

/// Default line height of a reference text style: the golden ratio times
/// the font size (`TextStyle::default().line_height = phi()`, style.rs;
/// `phi()` is `relative(f32::consts::GOLDEN_RATIO)`, geometry.rs).
fn default_line_height(font_size: f64) -> Pixels {
    px((font_size as f32) * std::f32::consts::GOLDEN_RATIO)
}

/// One constructed reference text system plus its typed platform handle.
struct ReferenceTextSystem {
    /// The `WindowTextSystem` the shaping entry lives on.
    window_text_system: WindowTextSystem,
    /// The platform text system, for the typed trait methods
    /// (`font_id`, `font_metrics`, `font_generation`, `all_font_names`)
    /// that the gpui wrapper either panics on or does not expose.
    platform: Arc<dyn PlatformTextSystem>,
    /// The public gpui wrapper, for the pixel-scaled metric accessors.
    text_system: Arc<TextSystem>,
}

impl ReferenceTextSystem {
    /// Construct the reference text system exactly as the pinned Windows
    /// platform does (with the portable rasterizer argument; see the
    /// module docs).
    fn new() -> Self {
        let parley = Arc::new(
            ParleyTextSystem::new_with_rasterizer(
                SystemFonts::Load,
                SERVICE_SYSTEM_FONT_FAMILY,
                SwashGlyphRasterizer::default(),
            )
            .with_fallback_families(SERVICE_FALLBACKS),
        );
        let platform: Arc<dyn PlatformTextSystem> = parley;
        let text_system = Arc::new(TextSystem::new(platform.clone()));
        Self {
            window_text_system: WindowTextSystem::new(text_system.clone()),
            platform,
            text_system,
        }
    }
}

// ---------------------------------------------------------------------------
// Input conversion
// ---------------------------------------------------------------------------

fn parse_style(value: Option<&str>) -> Result<FontStyle> {
    match value {
        None | Some(STYLE_NORMAL) => Ok(FontStyle::Normal),
        Some(STYLE_ITALIC) => Ok(FontStyle::Italic),
        Some(STYLE_OBLIQUE) => Ok(FontStyle::Oblique),
        Some(other) => bail!(
            "unsupported font style {other:?}: expected {STYLE_NORMAL}, {STYLE_ITALIC} or {STYLE_OBLIQUE}"
        ),
    }
}

fn build_features(features: &[FeatureIn]) -> FontFeatures {
    FontFeatures(Arc::new(
        features
            .iter()
            .map(|feature| (feature.tag.clone(), feature.value))
            .collect::<Vec<_>>(),
    ))
}

/// Build the public `gpui::Font` descriptor from the fixture's input
/// shape. Weight defaults to `FontWeight::default()` (400), style to
/// `Normal`, and absent fallbacks to the descriptor's own fallback list
/// only (the service fallbacks are appended by the platform itself).
fn build_font(descriptor: &FontDescriptorIn) -> Result<Font> {
    Ok(Font {
        family: descriptor.family.clone().into(),
        weight: descriptor
            .weight
            .map(|weight| FontWeight(weight as f32))
            .unwrap_or_default(),
        style: parse_style(descriptor.style.as_deref())?,
        features: build_features(&descriptor.features),
        fallbacks: (!descriptor.fallbacks.is_empty())
            .then(|| FontFallbacks::from_fonts(descriptor.fallbacks.clone())),
    })
}

/// Merge one run's font overrides over the case's base descriptor.
fn merge_descriptor(
    base: &FontDescriptorIn,
    overrides: Option<&FontOverrideIn>,
) -> FontDescriptorIn {
    match overrides {
        None => base.clone(),
        Some(overrides) => FontDescriptorIn {
            family: overrides
                .family
                .clone()
                .unwrap_or_else(|| base.family.clone()),
            weight: overrides.weight.or(base.weight),
            style: overrides.style.clone().or_else(|| base.style.clone()),
            features: overrides
                .features
                .clone()
                .unwrap_or_else(|| base.features.clone()),
            fallbacks: overrides
                .fallbacks
                .clone()
                .unwrap_or_else(|| base.fallbacks.clone()),
        },
    }
}

/// Build the case's style runs. An empty run list means one run covering
/// the whole text with the case's font. Present runs must cover the text
/// exactly and end on UTF-8 character boundaries — the pin assumes
/// gpui-validated input, so the fixture validates it loudly instead.
fn build_runs(case: &TextGeometryCase) -> Result<Vec<TextRun>> {
    if case.runs.is_empty() {
        return Ok(vec![TextRun {
            len: case.text.len(),
            font: build_font(&case.font)?,
            ..Default::default()
        }]);
    }

    let mut runs = Vec::with_capacity(case.runs.len());
    let mut covered = 0usize;
    for run_in in &case.runs {
        let descriptor = merge_descriptor(&case.font, run_in.font.as_ref());
        runs.push(TextRun {
            len: run_in.len,
            font: build_font(&descriptor)?,
            letter_spacing: run_in.letter_spacing.map(|spacing| px(spacing as f32)),
            ..Default::default()
        });
        covered += run_in.len;
        if !case.text.is_char_boundary(covered) {
            bail!(
                "text-geometry case {:?}: runs cover {} bytes, which is not a UTF-8 character boundary",
                case.label,
                covered
            );
        }
    }
    if covered != case.text.len() {
        bail!(
            "text-geometry case {:?}: runs cover {} bytes but the text is {} bytes",
            case.label,
            covered,
            case.text.len()
        );
    }
    Ok(runs)
}

fn parse_affinity(value: Option<&str>) -> Result<CaretAffinity> {
    match value {
        None | Some(AFFINITY_DOWNSTREAM) => Ok(CaretAffinity::Downstream),
        Some(AFFINITY_UPSTREAM) => Ok(CaretAffinity::Upstream),
        Some(other) => bail!(
            "unsupported caret affinity {other:?}: expected {AFFINITY_DOWNSTREAM} or {AFFINITY_UPSTREAM}"
        ),
    }
}

fn affinity_name(affinity: CaretAffinity) -> &'static str {
    match affinity {
        CaretAffinity::Downstream => AFFINITY_DOWNSTREAM,
        CaretAffinity::Upstream => AFFINITY_UPSTREAM,
    }
}

/// A caret query resolved to its input position and affinity.
struct CaretQuery {
    index: usize,
    affinity: CaretAffinity,
}

/// Expand the case's caret mode into the concrete query list.
/// `all-clusters`/`all-clusters-both` derive the cluster boundaries
/// through the public cluster API itself (one query per boundary, and
/// both affinities for the `-both` mode so wrap-boundary affinity is
/// observable).
fn caret_queries(case: &TextGeometryCase, clusters: &[Range<usize>]) -> Result<Vec<CaretQuery>> {
    let mode = case.caret_mode.as_deref().unwrap_or(CARET_MODE_EXPLICIT);
    match mode {
        CARET_MODE_EXPLICIT => case
            .caret_queries
            .iter()
            .map(|query| {
                Ok(CaretQuery {
                    index: query.index,
                    affinity: parse_affinity(query.affinity.as_deref())?,
                })
            })
            .collect(),
        CARET_MODE_ALL_CLUSTERS | CARET_MODE_ALL_CLUSTERS_BOTH => {
            let mut boundaries = vec![0usize];
            for cluster in clusters {
                if boundaries.last().is_some_and(|&last| last < cluster.end) {
                    boundaries.push(cluster.end);
                }
            }
            let mut queries = Vec::with_capacity(boundaries.len() * 2);
            for index in boundaries {
                queries.push(CaretQuery {
                    index,
                    affinity: CaretAffinity::Downstream,
                });
                if mode == CARET_MODE_ALL_CLUSTERS_BOTH {
                    queries.push(CaretQuery {
                        index,
                        affinity: CaretAffinity::Upstream,
                    });
                }
            }
            Ok(queries)
        }
        other => bail!(
            "unsupported caret mode {other:?}: expected {CARET_MODE_EXPLICIT}, {CARET_MODE_ALL_CLUSTERS} or {CARET_MODE_ALL_CLUSTERS_BOTH}"
        ),
    }
}

// ---------------------------------------------------------------------------
// Trace event helpers
// ---------------------------------------------------------------------------

fn field_str(name: &str, value: &str) -> (String, FieldValue) {
    (name.to_string(), FieldValue::str(value))
}

fn field_u64(name: &str, value: u64) -> (String, FieldValue) {
    (name.to_string(), FieldValue::Uint(value))
}

fn field_f32(name: &str, value: f32) -> (String, FieldValue) {
    (name.to_string(), FieldValue::f32(value))
}

/// The canonical FontId as the u64 the trace records (bit 63 set, store
/// index below — `FontStore::intern`, store.rs).
fn font_id_u64(font_id: gpui::FontId) -> u64 {
    font_id.0 as u64
}

/// Record one `font-resolved` event: the descriptor echo plus the
/// resolved canonical FontId handle.
fn record_font_resolved(
    recorder: &TraceRecorder,
    label: &str,
    role: &str,
    run_index: Option<usize>,
    descriptor: &FontDescriptorIn,
    font_id: gpui::FontId,
) {
    let mut fields = vec![
        field_str("family", &descriptor.family),
        field_u64("fontId", font_id_u64(font_id)),
        field_f32(
            "weight",
            descriptor
                .weight
                .map(|weight| weight as f32)
                .unwrap_or(FontWeight::default().0),
        ),
        field_str("style", descriptor.style.as_deref().unwrap_or(STYLE_NORMAL)),
        field_u64("featureCount", descriptor.features.len() as u64),
        field_u64("fallbackCount", descriptor.fallbacks.len() as u64),
        field_str("role", role),
    ];
    if let Some(run_index) = run_index {
        fields.push(field_u64("runIndex", run_index as u64));
    }
    recorder.event_with("font-resolved", Some(label), fields);
}

/// Record one `font-metrics` event: the full `gpui::FontMetrics` in font
/// units (the same record the native text service exposes) plus the
/// pixel-scaled values through the public `TextSystem` accessors at the
/// case's font size.
fn record_font_metrics(
    system: &ReferenceTextSystem,
    recorder: &TraceRecorder,
    label: &str,
    font_id: gpui::FontId,
    source: &str,
    font_size: Pixels,
) {
    let metrics = system.platform.font_metrics(font_id);
    recorder.event_with(
        "font-metrics",
        Some(label),
        vec![
            field_u64("fontId", font_id_u64(font_id)),
            field_str("source", source),
            field_u64("unitsPerEm", metrics.units_per_em as u64),
            field_f32("ascent", metrics.ascent),
            field_f32("descent", metrics.descent),
            field_f32("lineGap", metrics.line_gap),
            field_f32("underlinePosition", metrics.underline_position),
            field_f32("underlineThickness", metrics.underline_thickness),
            field_f32("capHeight", metrics.cap_height),
            field_f32("xHeight", metrics.x_height),
            field_f32("bboxX", metrics.bounding_box.origin.x),
            field_f32("bboxY", metrics.bounding_box.origin.y),
            field_f32("bboxWidth", metrics.bounding_box.size.width),
            field_f32("bboxHeight", metrics.bounding_box.size.height),
            field_f32(
                "ascentPx",
                f32::from(system.text_system.ascent(font_id, font_size)),
            ),
            field_f32(
                "descentPx",
                f32::from(system.text_system.descent(font_id, font_size)),
            ),
            field_f32(
                "xHeightPx",
                f32::from(system.text_system.x_height(font_id, font_size)),
            ),
        ],
    );
}

/// Push a byte-range cluster field pair (`present`, then `start`/`end`
/// when present) onto a field list.
fn push_cluster_fields(
    fields: &mut Vec<(String, FieldValue)>,
    prefix: &str,
    range: Option<Range<usize>>,
) {
    match range {
        Some(range) => {
            fields.push(field_u64(&format!("{prefix}Present"), 1));
            fields.push(field_u64(&format!("{prefix}Start"), range.start as u64));
            fields.push(field_u64(&format!("{prefix}End"), range.end as u64));
        }
        None => fields.push(field_u64(&format!("{prefix}Present"), 0)),
    }
}

// ---------------------------------------------------------------------------
// Case execution
// ---------------------------------------------------------------------------

fn run_case(
    system: &ReferenceTextSystem,
    case: &TextGeometryCase,
    recorder: &TraceRecorder,
) -> Result<()> {
    let runs = build_runs(case)?;
    let font_size = px(case.font_size as f32);
    let line_height = case
        .line_height
        .map(|height| px(height as f32))
        .unwrap_or_else(|| default_line_height(case.font_size));
    let wrap_width = case.wrap_width.map(|width| px(width as f32));

    recorder.event_with("case-begin", Some(&case.label), {
        let mut fields = vec![
            field_str("label", &case.label),
            field_u64("textLen", case.text.len() as u64),
            field_f32("fontSize", f32::from(font_size)),
            field_f32("lineHeight", f32::from(line_height)),
            field_u64("runCount", runs.len() as u64),
        ];
        match wrap_width {
            Some(width) => {
                fields.push(field_u64("wrapPresent", 1));
                fields.push(field_f32("wrapWidth", f32::from(width)));
            }
            None => fields.push(field_u64("wrapPresent", 0)),
        }
        match case.line_clamp {
            Some(clamp) => {
                fields.push(field_u64("clampPresent", 1));
                fields.push(field_u64("lineClamp", clamp as u64));
            }
            None => fields.push(field_u64("clampPresent", 0)),
        }
        fields
    });

    // Resolve the case's own descriptor, then every distinct run
    // descriptor (run fonts that differ from an already-resolved
    // descriptor resolve separately — the store may map different
    // descriptors to the same face).
    let case_font = build_font(&case.font)?;
    let case_font_id = system.platform.font_id(&case_font).map_err(|error| {
        anyhow::anyhow!(
            "text-geometry case {:?}: resolving font {:?} failed: {error:#}",
            case.label,
            case.font.family
        )
    })?;
    record_font_resolved(
        recorder,
        &case.label,
        ROLE_CASE,
        None,
        &case.font,
        case_font_id,
    );

    let mut resolved_descriptors: Vec<Font> = vec![case_font];
    for (run_index, run) in runs.iter().enumerate() {
        // Skip runs whose effective font is identical to an already
        // resolved descriptor: the descriptor echo would repeat.
        if resolved_descriptors.contains(&run.font) {
            continue;
        }
        let font_id = system.platform.font_id(&run.font).map_err(|error| {
            anyhow::anyhow!(
                "text-geometry case {:?}: resolving run {} font failed: {error:#}",
                case.label,
                run_index
            )
        })?;
        record_font_resolved(
            recorder,
            &case.label,
            ROLE_RUN,
            Some(run_index),
            &merge_descriptor(&case.font, case.runs[run_index].font.as_ref()),
            font_id,
        );
        resolved_descriptors.push(run.font.clone());
    }

    // Shape through the reference's public API. No window, app or
    // platform context is involved.
    let wrapped = system
        .window_text_system
        .shape_text(
            case.text.clone(),
            font_size,
            &runs,
            wrap_width,
            case.line_clamp,
        )
        .map_err(|error| {
            anyhow::anyhow!(
                "text-geometry case {:?}: shaping failed: {error:#}",
                case.label
            )
        })?;
    let layout: &LineLayout = &wrapped;

    // Enumerate logical clusters through the public cluster API
    // (graphemes in this pin).
    let mut clusters: Vec<Range<usize>> = Vec::new();
    let mut caret = CaretPosition::attached_to_next_cluster(0);
    while let Some(cluster) = wrapped.logical_cluster_after(caret) {
        if cluster.end <= caret.index {
            bail!(
                "text-geometry case {:?}: logical cluster {:?} does not advance caret {}",
                case.label,
                cluster,
                caret.index
            );
        }
        clusters.push(cluster.clone());
        caret = CaretPosition::attached_to_next_cluster(cluster.end);
    }

    let glyph_count: usize = layout
        .paint_fragments
        .iter()
        .map(|fragment| fragment.glyphs.len())
        .sum();

    recorder.event_with(
        "text-shape-meta",
        Some(&case.label),
        vec![
            field_u64("len", layout.len as u64),
            field_f32("fontSize", f32::from(layout.font_size)),
            field_f32("lineHeight", f32::from(line_height)),
            field_f32("width", f32::from(layout.width)),
            field_f32("ascent", f32::from(layout.ascent)),
            field_f32("descent", f32::from(layout.descent)),
            field_u64("lineCount", layout.visual_lines.len() as u64),
            field_u64("fragmentCount", layout.paint_fragments.len() as u64),
            field_u64("glyphCount", glyph_count as u64),
            field_u64("clusterCount", clusters.len() as u64),
        ],
    );

    // Font metrics: the case's resolved font first, then every distinct
    // font Parley selected while shaping (character-level fallback can
    // pick faces the caller never resolved).
    let mut metric_fonts: Vec<gpui::FontId> = vec![case_font_id];
    for fragment in &layout.paint_fragments {
        if !metric_fonts.contains(&fragment.font_id) {
            metric_fonts.push(fragment.font_id);
        }
    }
    for font_id in &metric_fonts {
        let source = if *font_id == case_font_id {
            METRIC_SOURCE_CASE
        } else {
            METRIC_SOURCE_FRAGMENT
        };
        record_font_metrics(system, recorder, &case.label, *font_id, source, font_size);
    }

    // Visual lines, with the row placement the pinned paint path derives
    // (line.rs `paint_visual_text`): row i occupies
    // [i*line_height, (i+1)*line_height), baseline =
    // (line_height - ascent - descent)/2 + ascent from the row top.
    let baseline = (line_height - layout.ascent - layout.descent) / 2. + layout.ascent;
    for (line_index, line) in layout.visual_lines.iter().enumerate() {
        recorder.event_with(
            "text-line",
            Some(&case.label),
            vec![
                field_u64("lineIndex", line_index as u64),
                field_u64("textStart", line.text_range.start as u64),
                field_u64("textEnd", line.text_range.end as u64),
                field_u64("fragmentStart", line.fragment_range.start as u64),
                field_u64("fragmentEnd", line.fragment_range.end as u64),
                field_f32("advanceWidth", f32::from(line.advance_width)),
                field_f32("lineHeight", f32::from(line_height)),
                field_f32("baseline", f32::from(baseline)),
            ],
        );
    }

    // Paint fragments (the public "run" surface) in visual order.
    for (line_index, line) in layout.visual_lines.iter().enumerate() {
        for fragment in &layout.paint_fragments[line.fragment_range.clone()] {
            recorder.event_with(
                "text-run",
                Some(&case.label),
                vec![
                    field_u64("lineIndex", line_index as u64),
                    field_u64("fontId", font_id_u64(fragment.font_id)),
                    field_f32("fontSize", f32::from(fragment.font_size)),
                    field_f32("xStart", f32::from(fragment.x_range.start)),
                    field_f32("xEnd", f32::from(fragment.x_range.end)),
                    field_u64("glyphCount", fragment.glyphs.len() as u64),
                ],
            );
        }
    }

    // Logical clusters in document order.
    for (cluster_index, cluster) in clusters.iter().enumerate() {
        recorder.event_with(
            "text-cluster",
            Some(&case.label),
            vec![
                field_u64("clusterIndex", cluster_index as u64),
                field_u64("start", cluster.start as u64),
                field_u64("end", cluster.end as u64),
            ],
        );
    }

    // Caret queries: full bounds through the public
    // `LineLayout::platform_layout` field, plus the adjacent logical
    // clusters.
    for query in caret_queries(case, &clusters)? {
        let caret = CaretPosition {
            index: query.index,
            affinity: query.affinity,
        };
        let bounds = layout.platform_layout.caret_bounds(caret, line_height);
        let mut fields = vec![
            field_u64("index", query.index as u64),
            field_str("affinity", affinity_name(query.affinity)),
        ];
        match bounds {
            Some(bounds) => {
                fields.push(field_u64("present", 1));
                fields.push(field_f32("x", f32::from(bounds.origin.x)));
                fields.push(field_f32("y", f32::from(bounds.origin.y)));
                fields.push(field_f32("width", f32::from(bounds.size.width)));
                fields.push(field_f32("height", f32::from(bounds.size.height)));
            }
            None => fields.push(field_u64("present", 0)),
        }
        push_cluster_fields(
            &mut fields,
            "clusterBefore",
            wrapped.logical_cluster_before(caret),
        );
        push_cluster_fields(
            &mut fields,
            "clusterAfter",
            wrapped.logical_cluster_after(caret),
        );
        recorder.event_with("text-caret", Some(&case.label), fields);
    }

    // Hit tests: the caret hit test (Ok = inside a visual row, Err = the
    // edge caret) and the byte-index hit test (the logical start of the
    // cluster under the point).
    for point_in in &case.hit_points {
        let point = Point::new(px(point_in.x as f32), px(point_in.y as f32));
        let (inside, caret) = match wrapped.closest_caret_for_pixel_point(point, line_height) {
            Ok(caret) => (1, caret),
            Err(caret) => (0, caret),
        };
        let (byte_index_present, byte_index) =
            match wrapped.byte_index_for_pixel_point(point, line_height) {
                Ok(index) => (1, index),
                Err(index) => (0, index),
            };
        recorder.event_with(
            "text-hit",
            Some(&case.label),
            vec![
                field_f32("x", f32::from(point.x)),
                field_f32("y", f32::from(point.y)),
                field_u64("inside", inside),
                field_u64("index", caret.index as u64),
                field_str("affinity", affinity_name(caret.affinity)),
                field_u64("byteIndexPresent", byte_index_present),
                field_u64("byteIndex", byte_index as u64),
            ],
        );
    }

    // Selection ranges: the visual-order rectangles.
    for selection in &case.selections {
        let rects = wrapped.selection_bounds(selection.start..selection.end, line_height);
        if rects.is_empty() {
            recorder.event_with(
                "text-selection",
                Some(&case.label),
                vec![
                    field_u64("start", selection.start as u64),
                    field_u64("end", selection.end as u64),
                    field_u64("rectIndex", 0),
                    field_u64("rectPresent", 0),
                ],
            );
        } else {
            for (rect_index, rect) in rects.iter().enumerate() {
                recorder.event_with(
                    "text-selection",
                    Some(&case.label),
                    vec![
                        field_u64("start", selection.start as u64),
                        field_u64("end", selection.end as u64),
                        field_u64("rectIndex", rect_index as u64),
                        field_u64("rectPresent", 1),
                        field_f32("x", f32::from(rect.origin.x)),
                        field_f32("y", f32::from(rect.origin.y)),
                        field_f32("width", f32::from(rect.size.width)),
                        field_f32("height", f32::from(rect.size.height)),
                    ],
                );
            }
        }
    }

    recorder.event("case-end", Some(&case.label));
    Ok(())
}

// ---------------------------------------------------------------------------
// Fixture entry point
// ---------------------------------------------------------------------------

pub fn run(envelope: &Envelope) -> Vec<crate::trace::TraceEvent> {
    let recorder = TraceRecorder::new();
    let inputs = envelope
        .inputs
        .text_cases
        .as_ref()
        .unwrap_or_else(|| panic!("fixture kind text-geometry-v1 requires inputs.text_cases"));

    let system = ReferenceTextSystem::new();

    // Construction record: the path, the catalog shape and the resolved
    // default font (`.SystemUIFont`, the reference `Font::default()`).
    let font_names = system.platform.all_font_names();
    let has_family = |family: &str| font_names.iter().any(|name| name == family);
    let mut fields = vec![
        field_str("systemFontFamily", SERVICE_SYSTEM_FONT_FAMILY),
        field_str("fallbackFamilies", &SERVICE_FALLBACKS.join(", ")),
        field_u64("fontGeneration", system.platform.font_generation()),
        field_u64("fontFamilyCount", font_names.len() as u64),
    ];
    for family in CATALOG_PROBE_FAMILIES {
        fields.push(field_u64(
            &format!("has{}", family.replace(' ', "")),
            has_family(family) as u64,
        ));
    }
    recorder.event_with("text-system-begin", Some("text-system"), fields);

    let default_font = Font::default();
    let default_font_id = system
        .platform
        .font_id(&default_font)
        .expect("resolving the default system font");
    record_font_resolved(
        &recorder,
        "default-font",
        ROLE_DEFAULT,
        None,
        &FontDescriptorIn {
            family: default_font.family.to_string(),
            weight: None,
            style: None,
            features: Vec::new(),
            fallbacks: Vec::new(),
        },
        default_font_id,
    );

    for case in &inputs.cases {
        run_case(&system, case, &recorder)
            .unwrap_or_else(|e| panic!("text-geometry case {:?} failed: {e:#}", case.label));
    }
    recorder.into_events()
}
