//! Fixture `scene-painting-v1`: reference scene-kernel records for a
//! semantic paint script.
//!
//! ## The public oracle boundary
//!
//! The reference scene kernel is observable through gpui's PUBLIC `Scene`
//! API (`crates/gpui/src/scene.rs`): `insert_primitive`, `push_layer`,
//! `pop_layer`, `raise_order_floor`, `replay`, `finish`,
//! `render_commands`, `render_plan().requirements()`, the public
//! primitive arrays (`quads`, `shadows`, `underlines`, `surfaces`,
//! `backdrop_filters`, `filter_boundaries`) and `surface_opacities()`.
//! This is the highest public boundary that captures the final scene op
//! order POST sort/finish (and the compiled plan), so it is the boundary
//! this fixture records.
//!
//! `TestPlatform::with_platform` (which would allow installing a
//! capturing `PlatformHeadlessRenderer` behind `TestWindow::draw`) is
//! `pub(crate)`; `TestAppContext::build` constructs its own platform
//! without a renderer factory, and `App::new_app` is `pub(crate)` too, so
//! the platform render path is NOT reachable from this harness crate.
//! `Window::painted_quads()` (public, test-support) exposes only the
//! finished quads of a window-drawn frame. The scene's own public API is
//! the more complete boundary and is used instead; the window-level
//! record-construction semantics (`paint_quad`, `paint_drop_shadows`,
//! `paint_inset_shadows`, `paint_underline`, `paint_layer`,
//! `with_filter_layer`, `paint_backdrop_filter`, `with_element_opacity`)
//! are replicated command-by-command from the pinned `window.rs` source,
//! including the snapping helpers, the element-opacity multiplication and
//! the border-only quad split.
//!
//! ## What each case does
//!
//! 1. One window per case (like layout-metrics-v1): the case's style tree
//!    is laid out through `Window::request_layout` under the case's
//!    available space, and every node's bounds are recorded
//!    (`layout-bounds` events, logical pixels, f32 bits) and used as the
//!    paint script's referenced geometry.
//! 2. The paint script runs against a fresh `Scene` through its public
//!    API, mirroring the pinned paint methods:
//!    - `paint-box` builds a `PaintQuad`-shaped record (snapped bounds,
//!      scaled corner radii, stroke-snapped border widths, opacity folded
//!      into background and border colors, corner smoothing clamped) and
//!      inserts it, including the pinned border-only split into strips;
//!    - `paint-shadow` builds the pinned `Shadow` record (drop or inset);
//!    - `paint-underline` builds the pinned `Underline` record;
//!    - `begin-layer`/`end-layer` push/pop a scene layer when the clipped
//!      bounds are non-empty, and multiply an element opacity into the
//!      primitives painted between them (nested layers multiply);
//!    - `raise-floor` raises the deferred-draw order floor;
//!    - `begin-filter-group`/`end-filter-group` insert the pinned
//!      matched `FilterBoundary` pair (one snapshot for both markers);
//!    - `paint-backdrop` inserts the pinned `BackdropFilter` (element
//!      opacity captured at paint time);
//!    - `replay` re-inserts a range of a previous case's operations.
//! 3. After `finish`, the fixture walks `render_commands()` in order and
//!    records `scene-command` events (batch structure, filter-target
//!    plan) and `scene-op` events (one per primitive in final order,
//!    with the assigned draw order, bounds, mask, colors as the stored
//!    HSLA bits, radii, widths and per-kind payload), then a `scene-meta`
//!    event with the scene totals and plan requirements.
//!
//! Determinism: everything above is a pure function of the envelope. The
//! reference test window's fixed scale factor (2.0) is part of the
//! fixture semantics, recorded in `case-begin`.

use crate::envelope::{CornersIn, EdgesIn, Envelope, PaintOp, RectIn, ScenePaintingCase, StyleNode};
use crate::trace::{FieldValue, TraceRecorder};
use anyhow::{Context as _, Result, bail};
use gpui::{
    App, AvailableSpace, Background, Bounds, ColorExt, ContentMask, Corners, Edges, Element,
    Filter, FilterBoundary, Pixels, Point, ScaledPixels, Scene, Shadow, Size, TestAppContext,
    Underline, UnderlineStyle, Window, px, rgba, rgb_to_hsla, transparent_black,
};
use std::cell::RefCell;
use std::collections::BTreeMap;
use std::collections::HashMap;
use std::rc::Rc;

/// Default rem size of a reference window, in logical pixels
/// (`Window`'s `rem_size: px(16.)`, window.rs).
const DEFAULT_REM_SIZE: f64 = 16.0;

// ---------------------------------------------------------------------------
// Pinned window.rs helpers (pub(crate) in the reference; re-implemented
// here from the source formulas so the fixture's records match the paint
// methods exactly)
// ---------------------------------------------------------------------------

/// util.rs `round_half_toward_zero`:
/// `(value.abs() - 0.5).ceil().copysign(value)` in f32.
pub(crate) fn round_half_toward_zero(value: f32) -> f32 {
    let abs = value.abs();
    let shifted = abs - 0.5;
    let ceil = shifted.ceil();
    ceil.copysign(value)
}

pub(crate) fn round_to_device_pixel(logical: f32, scale_factor: f32) -> f32 {
    round_half_toward_zero(logical * scale_factor)
}

pub(crate) fn floor_to_device_pixel(logical: f32, scale_factor: f32) -> f32 {
    (logical * scale_factor).floor()
}

pub(crate) fn ceil_to_device_pixel(logical: f32, scale_factor: f32) -> f32 {
    (logical * scale_factor).ceil()
}

/// util.rs `round_stroke_to_device_pixel`: an exact zero stays zero;
/// anything else is clamped up to one device pixel.
pub(crate) fn round_stroke_to_device_pixel(logical: f32, scale_factor: f32) -> f32 {
    if logical == 0.0 {
        return 0.0;
    }
    let clamped = logical.max(0.0);
    let rounded = round_to_device_pixel(clamped, scale_factor);
    if rounded < 1.0 {
        1.0
    } else {
        rounded
    }
}

/// window.rs `snap_bounds`: each edge rounded to the device grid, far
/// edges clamped to at least the near edges.
pub(crate) fn snap_bounds(bounds: Bounds<Pixels>, scale: f32) -> Bounds<ScaledPixels> {
    let left = round_to_device_pixel(f32::from(bounds.left()), scale);
    let top = round_to_device_pixel(f32::from(bounds.top()), scale);
    let right = round_to_device_pixel(f32::from(bounds.right()), scale).max(left);
    let bottom = round_to_device_pixel(f32::from(bounds.bottom()), scale).max(top);
    Bounds::from_corners(
        gpui::point(ScaledPixels(left), ScaledPixels(top)),
        gpui::point(ScaledPixels(right), ScaledPixels(bottom)),
    )
}

/// window.rs `snap_stroke`.
pub(crate) fn snap_stroke(value: Pixels, scale: f32) -> ScaledPixels {
    ScaledPixels(round_stroke_to_device_pixel(f32::from(value), scale))
}

pub(crate) fn snap_border_widths(edges: Edges<Pixels>, scale: f32) -> Edges<ScaledPixels> {
    edges.map(|e| snap_stroke(*e, scale))
}

/// window.rs `cover_bounds`: floor the near edges, ceil the far edges.
pub(crate) fn cover_bounds(bounds: Bounds<Pixels>, scale: f32) -> Bounds<ScaledPixels> {
    let left = floor_to_device_pixel(f32::from(bounds.left()), scale);
    let top = floor_to_device_pixel(f32::from(bounds.top()), scale);
    let right = ceil_to_device_pixel(f32::from(bounds.right()), scale).max(left);
    let bottom = ceil_to_device_pixel(f32::from(bounds.bottom()), scale).max(top);
    Bounds::from_corners(
        gpui::point(ScaledPixels(left), ScaledPixels(top)),
        gpui::point(ScaledPixels(right), ScaledPixels(bottom)),
    )
}

/// window.rs `snapped_content_mask` (the fade_out component is always
/// None in this fixture, so only the bounds are recorded).
pub(crate) fn snapped_content_mask(mask: Bounds<Pixels>, scale: f32) -> ContentMask<ScaledPixels> {
    ContentMask {
        bounds: cover_bounds(mask, scale),
        ..Default::default()
    }
}

/// window.rs `Bounds::intersect` for logical bounds.
pub(crate) fn intersect_logical(a: Bounds<Pixels>, b: Bounds<Pixels>) -> Bounds<Pixels> {
    a.intersect(&b)
}

pub(crate) fn logical_bounds(rect: RectIn) -> Bounds<Pixels> {
    Bounds {
        origin: gpui::point(px(rect.x as f32), px(rect.y as f32)),
        size: Size {
            width: px(rect.width as f32),
            height: px(rect.height as f32),
        },
    }
}

pub(crate) fn logical_point(x: f64, y: f64) -> Point<Pixels> {
    gpui::point(px(x as f32), px(y as f32))
}

/// window.rs `largest_border_interior` (the border-only quad split).
pub(crate) fn largest_border_interior(quad: &gpui::Quad) -> Bounds<ScaledPixels> {
    let radii = &quad.corner_radii;
    let widths = &quad.border_widths;
    let reach_factor = 1.0 + quad.corner_smoothing;
    let edge_reaches = Edges {
        top: radii.top_left.max(radii.top_right) * reach_factor,
        right: radii.top_right.max(radii.bottom_right) * reach_factor,
        bottom: radii.bottom_left.max(radii.bottom_right) * reach_factor,
        left: radii.top_left.max(radii.bottom_left) * reach_factor,
    };

    let antialias_inset = gpui::point(ScaledPixels(1.0), ScaledPixels(1.0));
    let inset_bounds = |top_left_inset, bottom_right_inset| {
        Bounds::from_corners(
            quad.bounds.origin + top_left_inset + antialias_inset,
            quad.bounds.bottom_right() - bottom_right_inset - antialias_inset,
        )
    };

    // Rounded corners need only be excluded on one axis. Either candidate
    // is empty of border pixels, so use the larger interior.
    let horizontal_band = inset_bounds(
        gpui::point(widths.left, widths.top.max(edge_reaches.top)),
        gpui::point(widths.right, widths.bottom.max(edge_reaches.bottom)),
    );
    let vertical_band = inset_bounds(
        gpui::point(widths.left.max(edge_reaches.left), widths.top),
        gpui::point(widths.right.max(edge_reaches.right), widths.bottom),
    );

    let area = |bounds: &Bounds<ScaledPixels>| {
        bounds.size.width.0.max(0.0) * bounds.size.height.0.max(0.0)
    };
    if area(&horizontal_band) >= area(&vertical_band) {
        horizontal_band
    } else {
        vertical_band
    }
}

/// Parse an `RRGGBBAA` hex color (with optional `#` or `0x` prefix) and
/// convert it to the reference's HSL-with-alpha through the pinned
/// public `rgba` + `rgb_to_hsla`.
pub(crate) fn parse_hsla(hex: &str) -> Result<gpui::Hsla> {
    let compact: String = hex
        .chars()
        .filter(|c| !c.is_whitespace())
        .collect();
    let stripped = compact
        .strip_prefix('#')
        .or_else(|| compact.strip_prefix("0x"))
        .unwrap_or(&compact);
    if stripped.len() != 8 || !stripped.chars().all(|c| c.is_ascii_hexdigit()) {
        bail!("parsing color {hex:?}: expected 8 hex digits RRGGBBAA");
    }
    let value = u32::from_str_radix(stripped, 16)
        .with_context(|| format!("parsing color {hex:?}"))?;
    Ok(rgb_to_hsla(rgba(value)))
}

/// The stored solid SceneHsla of a background, derived through the public
/// `as_solid` accessor (hue in positive degrees / 360). This reproduces
/// the stored bits for the trace.
pub(crate) fn solid_hsla(background: &Background) -> gpui::Hsla {
    background.as_solid().unwrap_or_default()
}

pub(crate) fn corners_of(corner_radius: Option<f64>, corner_radii: Option<CornersIn>) -> Corners<Pixels> {
    if let Some(radii) = corner_radii {
        return Corners {
            top_left: px(radii.top_left as f32),
            top_right: px(radii.top_right as f32),
            bottom_right: px(radii.bottom_right as f32),
            bottom_left: px(radii.bottom_left as f32),
        };
    }
    let uniform = corner_radius.unwrap_or(0.0) as f32;
    Corners {
        top_left: px(uniform),
        top_right: px(uniform),
        bottom_right: px(uniform),
        bottom_left: px(uniform),
    }
}

pub(crate) fn edges_of(border_width: Option<f64>, border_widths: Option<EdgesIn>) -> Edges<Pixels> {
    if let Some(widths) = border_widths {
        return Edges {
            top: px(widths.top as f32),
            right: px(widths.right as f32),
            bottom: px(widths.bottom as f32),
            left: px(widths.left as f32),
        };
    }
    let uniform = px(border_width.unwrap_or(0.0) as f32);
    Edges {
        top: uniform,
        right: uniform,
        bottom: uniform,
        left: uniform,
    }
}

// ---------------------------------------------------------------------------
// The layout probe (mirrors the layout-metrics probe, collecting bounds)
// ---------------------------------------------------------------------------

/// A custom element that lays out the case's style tree and collects the
/// computed bounds of every labeled node (also recording layout-bounds
/// events like the layout-metrics fixture).
pub struct SceneLayoutProbe {
    pub tree: StyleNode,
    pub recorder: TraceRecorder,
    /// Filled by prepaint: label -> window-local logical bounds. Shared
    /// with the runner so the collected bounds escape `vcx.draw` (which
    /// consumes the element).
    pub bounds: Rc<RefCell<BTreeMap<String, Bounds<Pixels>>>>,
}

impl Element for SceneLayoutProbe {
    type RequestLayoutState = Vec<(String, gpui::LayoutId)>;
    type PrepaintState = ();

    fn request_layout(
        &mut self,
        _id: Option<&gpui::GlobalElementId>,
        _inspector_id: Option<&gpui::InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (gpui::LayoutId, Self::RequestLayoutState) {
        let mut flat: Vec<crate::fixtures::layout_metrics::FlatNode> = Vec::new();
        let root = crate::fixtures::layout_metrics::flatten_tree(&self.tree, &mut flat)
            .expect("style tree parsed during construction");
        let mut laid_out: Vec<(String, Option<gpui::LayoutId>)> = Vec::new();
        let layout_id = request_subtree(&flat, root, window, cx, &mut laid_out)
            .expect("layout tree built");
        let state = laid_out
            .into_iter()
            .map(|(label, id)| (label, id.expect("layout id filled")))
            .collect();
        (layout_id, state)
    }

    fn prepaint(
        &mut self,
        _id: Option<&gpui::GlobalElementId>,
        _inspector_id: Option<&gpui::InspectorElementId>,
        _bounds: gpui::Bounds<Pixels>,
        request_layout: &mut Self::RequestLayoutState,
        window: &mut Window,
        _cx: &mut App,
    ) -> Self::PrepaintState {
        let mut bounds = self.bounds.borrow_mut();
        for (label, layout_id) in request_layout.iter() {
            let node_bounds = window.layout_bounds(*layout_id);
            bounds.insert(label.clone(), node_bounds);
            self.recorder.event_with(
                "layout-bounds",
                Some(label),
                [
                    ("x".into(), FieldValue::f32_raw_pixels(node_bounds.origin.x)),
                    ("y".into(), FieldValue::f32_raw_pixels(node_bounds.origin.y)),
                    ("width".into(), FieldValue::f32_raw_pixels(node_bounds.size.width)),
                    ("height".into(), FieldValue::f32_raw_pixels(node_bounds.size.height)),
                ],
            );
        }
    }

    fn paint(
        &mut self,
        _id: Option<&gpui::GlobalElementId>,
        _inspector_id: Option<&gpui::InspectorElementId>,
        _bounds: gpui::Bounds<Pixels>,
        _request_layout: &mut Self::RequestLayoutState,
        _prepaint: &mut Self::PrepaintState,
        _window: &mut Window,
        _cx: &mut App,
    ) {
    }

    fn id(&self) -> Option<gpui::ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static std::panic::Location<'static>> {
        None
    }
}

impl gpui::IntoElement for SceneLayoutProbe {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

/// Request the layout tree bottom-up (the layout-metrics build_layout
/// without the measured-node support; the scene fixture lays out plain
/// style trees).
fn request_subtree(
    flat: &[crate::fixtures::layout_metrics::FlatNode],
    idx: usize,
    window: &mut Window,
    cx: &mut App,
    out: &mut Vec<(String, Option<gpui::LayoutId>)>,
) -> Result<gpui::LayoutId> {
    let slot = out.len();
    out.push((flat[idx].label.clone(), None));
    let node = &flat[idx];
    let child_ids: Vec<gpui::LayoutId> = node
        .children
        .iter()
        .map(|&child| request_subtree(flat, child, window, cx, out))
        .collect::<Result<Vec<_>>>()?;
    let layout_id = window.request_layout(node.style.clone(), child_ids, cx);
    out[slot].1 = Some(layout_id);
    Ok(layout_id)
}

// ---------------------------------------------------------------------------
// Paint script execution
// ---------------------------------------------------------------------------

/// The case's execution state.
struct ScriptState<'a> {
    scene: &'a mut Scene,
    scale: f32,
    element_opacity: f32,
    opacity_stack: Vec<f32>,
    layer_pushed_stack: Vec<bool>,
    layer_push_count: u32,
    /// Open content-filter groups; None marks an identity group that
    /// runs inline (no markers).
    pending_boundaries: Vec<Option<FilterBoundary>>,
}

fn node_bounds(
    bounds_by_label: &BTreeMap<String, Bounds<Pixels>>,
    label: &str,
) -> Result<Bounds<Pixels>> {
    bounds_by_label
        .get(label)
        .copied()
        .with_context(|| format!("paint script references unknown node label {label:?}"))
}

pub(crate) fn case_mask(case: &ScenePaintingCase) -> Bounds<Pixels> {
    Bounds {
        origin: gpui::point(px(0.), px(0.)),
        size: Size {
            width: px(case.available_space.width as f32),
            height: px(case.available_space.height as f32),
        },
    }
}

pub(crate) fn effective_mask(op_mask: Option<RectIn>, default_mask: Bounds<Pixels>) -> Bounds<Pixels> {
    op_mask.map(logical_bounds).unwrap_or(default_mask)
}

/// window.rs `paint_quad_with_corner_smoothing`, including the
/// border-only split.
fn paint_box(state: &mut ScriptState, bounds: Bounds<Pixels>, op: &PaintOpDetails) -> Result<()> {
    let PaintOpDetails {
        background,
        corner_radii,
        border_widths,
        border_color,
        border_style,
        corner_smoothing,
        mask,
    } = op;

    let scale = state.scale;
    let opacity = state.element_opacity;
    let snapped_bounds = snap_bounds(bounds, scale);
    let snapped_border_widths = snap_border_widths((*border_widths).clone(), scale);
    let quad = gpui::Quad {
        order: 0,
        bounds: snapped_bounds,
        content_mask: snapped_content_mask((*mask).clone(), scale),
        background: background.opacity(opacity),
        border_color: border_color.opacity(opacity),
        corner_radii: corner_radii.scale(scale),
        border_widths: snapped_border_widths,
        border_style: **border_style,
        // The pinned Quad::default values (scene.rs
        // DEFAULT_BORDER_DASHED_LENGTH / GAP, pub(crate) in the
        // reference): 2.0 and 1.0.
        border_dashed_length: 2.0,
        border_dashed_gap: 1.0,
        corner_smoothing: (**corner_smoothing as f32).clamp(0.0, 1.0),
        padding: 0,
    };

    if !quad.background.is_transparent() {
        state.scene.insert_primitive(quad);
        return Ok(());
    }

    // Splitting a border-only quad around its empty interior avoids
    // shading every transparent pixel inside large outlines.
    let outer_bounds = quad.bounds;
    let inner_bounds = largest_border_interior(&quad);

    if inner_bounds.is_empty() {
        state.scene.insert_primitive(quad);
        return Ok(());
    }

    let strips = [
        // Top
        Bounds::from_corners(
            outer_bounds.origin,
            gpui::point(outer_bounds.right(), inner_bounds.top()),
        ),
        // Bottom
        Bounds::from_corners(
            gpui::point(outer_bounds.left(), inner_bounds.bottom()),
            outer_bounds.bottom_right(),
        ),
        // Left
        Bounds::from_corners(
            gpui::point(outer_bounds.left(), inner_bounds.top()),
            inner_bounds.bottom_left(),
        ),
        // Right
        Bounds::from_corners(
            inner_bounds.top_right(),
            gpui::point(outer_bounds.right(), inner_bounds.bottom()),
        ),
    ];

    for strip in strips {
        let content_mask_bounds = quad.content_mask.bounds.intersect(&strip);
        if !content_mask_bounds.is_empty() {
            state.scene.insert_primitive(gpui::Quad {
                content_mask: ContentMask {
                    bounds: content_mask_bounds,
                    ..Default::default()
                },
                ..quad
            });
        }
    }
    Ok(())
}

/// window.rs `paint_drop_shadows_with_corner_smoothing` and
/// `paint_inset_shadows_with_corner_smoothing` (one shadow per op).
fn paint_shadow(
    state: &mut ScriptState,
    bounds: Bounds<Pixels>,
    offset: Point<Pixels>,
    blur_radius: Pixels,
    spread_radius: Pixels,
    color: Background,
    inset: bool,
    corner_radii: Corners<Pixels>,
    corner_smoothing: f64,
    mask: Bounds<Pixels>,
) {
    let scale = state.scale;
    let content_mask = snapped_content_mask(mask, scale);
    let opacity = state.element_opacity;
    let element_bounds = cover_bounds(bounds, scale);
    let element_corner_radii = corner_radii.scale(scale);
    let corner_smoothing = (corner_smoothing as f32).clamp(0.0, 1.0);

    if !inset {
        // Drop shadow: the dilated region.
        let shadow_bounds = dilate(offset_bounds(bounds, offset), spread_radius);
        state.scene.insert_primitive(Shadow {
            order: 0,
            blur_radius: blur_radius.scale(scale),
            bounds: cover_bounds(shadow_bounds, scale),
            content_mask,
            corner_radii: corner_radii.scale(scale),
            color: color.opacity(opacity),
            element_bounds,
            element_corner_radii,
            inset: false.into(),
            corner_smoothing,
        });
    } else {
        // Inset shadow: the hole (bounds + offset dilated by -spread).
        let hole = dilate(offset_bounds(bounds, offset), px(-f32::from(spread_radius)));
        // Clamp each hole corner radius at zero so a large spread can't
        // produce negative radii.
        let zero = Pixels::ZERO;
        let hole_corner_radii = Corners {
            top_left: (corner_radii.top_left - spread_radius).max(zero),
            top_right: (corner_radii.top_right - spread_radius).max(zero),
            bottom_right: (corner_radii.bottom_right - spread_radius).max(zero),
            bottom_left: (corner_radii.bottom_left - spread_radius).max(zero),
        };
        state.scene.insert_primitive(Shadow {
            order: 0,
            blur_radius: blur_radius.scale(scale),
            bounds: cover_bounds(hole, scale),
            content_mask,
            corner_radii: hole_corner_radii.scale(scale),
            color: color.opacity(opacity),
            element_bounds,
            element_corner_radii,
            inset: true.into(),
            corner_smoothing,
        });
    }
}

/// geometry.rs `Bounds + Point`: offset the origin.
pub(crate) fn offset_bounds(bounds: Bounds<Pixels>, offset: Point<Pixels>) -> Bounds<Pixels> {
    Bounds {
        origin: bounds.origin + offset,
        size: bounds.size,
    }
}

/// geometry.rs `Bounds::dilate`: origin -= (amount, amount); size += (2a, 2a).
pub(crate) fn dilate(bounds: Bounds<Pixels>, amount: Pixels) -> Bounds<Pixels> {
    let double_amount = px(f32::from(amount) + f32::from(amount));
    Bounds {
        origin: bounds.origin - gpui::point(amount, amount),
        size: Size {
            width: bounds.size.width + double_amount,
            height: bounds.size.height + double_amount,
        },
    }
}

/// window.rs `paint_underline`.
fn paint_underline(
    state: &mut ScriptState,
    origin: Point<Pixels>,
    width: Pixels,
    style: &UnderlineStyle,
    mask: Bounds<Pixels>,
) {
    let scale = state.scale;
    let thickness = snap_stroke(style.thickness, scale);
    let height = if style.wavy {
        ScaledPixels(thickness.0 * 3.0)
    } else {
        thickness
    };
    let bounds = Bounds {
        origin: origin.map(|c| ScaledPixels(round_to_device_pixel(f32::from(c), scale))),
        size: Size {
            width: snap_stroke(width, scale),
            height,
        },
    };
    let element_opacity = state.element_opacity;
    state.scene.insert_primitive(Underline {
        order: 0,
        padding: 0,
        bounds,
        content_mask: snapped_content_mask(mask, scale),
        color: style.color.unwrap_or_default().opacity(element_opacity).into(),
        thickness,
        wavy: style.wavy.into(),
    });
}

/// window.rs `with_filter_layer_with_corner_smoothing`'s boundary snapshot
/// (one snapshot shared by the start and end markers; opacity is 1.0 —
/// NOT element_opacity, per the pinned comment).
pub(crate) fn filter_boundary_record(
    bounds: Bounds<Pixels>,
    blur_radius: f64,
    corner_radii: Corners<Pixels>,
    corner_smoothing: f64,
    mask: Bounds<Pixels>,
    scale: f32,
) -> FilterBoundary {
    FilterBoundary {
        order: 0,
        bounds: snap_bounds(bounds, scale),
        content_mask: snapped_content_mask(mask, scale),
        corner_radii: corner_radii.scale(scale),
        corner_smoothing: (corner_smoothing as f32).clamp(0.0, 1.0),
        filters: vec![Filter::Blur(px(blur_radius as f32))]
            .into_iter()
            .filter(|filter| !filter.is_identity())
            .map(|filter| filter.scale(scale))
            .collect(),
        opacity: 1.0,
        is_start: true,
    }
}

/// window.rs `paint_backdrop_filter_with_corner_smoothing` (the element
/// opacity captured at paint time is multiplied into the result).
fn paint_backdrop(
    state: &mut ScriptState,
    bounds: Bounds<Pixels>,
    blur_radius: f64,
    corner_radii: Corners<Pixels>,
    corner_smoothing: f64,
    mask: Bounds<Pixels>,
) {
    let scale = state.scale;
    let filters: Vec<_> = vec![Filter::Blur(px(blur_radius as f32))]
        .into_iter()
        .filter(|filter| !filter.is_identity())
        .map(|filter| filter.scale(scale))
        .collect();
    if filters.is_empty() {
        return;
    }
    state.scene.insert_primitive(gpui::BackdropFilter {
        order: 0,
        bounds: snap_bounds(bounds, scale),
        content_mask: snapped_content_mask(mask, scale),
        corner_radii: corner_radii.scale(scale),
        corner_smoothing: (corner_smoothing as f32).clamp(0.0, 1.0),
        filters: filters.into(),
        opacity: state.element_opacity,
    });
}

/// The flattened paint parameters of the script's box op.
struct PaintOpDetails<'a> {
    background: &'a Background,
    corner_radii: &'a Corners<Pixels>,
    border_widths: &'a Edges<Pixels>,
    border_color: &'a Background,
    border_style: &'a gpui::BorderStyle,
    corner_smoothing: &'a f64,
    mask: &'a Bounds<Pixels>,
}

// ---------------------------------------------------------------------------
// Trace emission
// ---------------------------------------------------------------------------

pub(crate) fn f32_field(value: ScaledPixels) -> FieldValue {
    FieldValue::f32(value.0)
}

pub(crate) fn color_fields(prefix: &str, color: &gpui::Hsla) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}H"), FieldValue::f32(color.hue.into_positive_degrees() / 360.0)),
        (format!("{prefix}S"), FieldValue::f32(color.saturation)),
        (format!("{prefix}L"), FieldValue::f32(color.lightness)),
        (format!("{prefix}A"), FieldValue::f32(color.alpha)),
    ]
}

pub(crate) fn bounds_fields(prefix: &str, bounds: &Bounds<ScaledPixels>) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}X"), f32_field(bounds.origin.x)),
        (format!("{prefix}Y"), f32_field(bounds.origin.y)),
        (format!("{prefix}W"), f32_field(bounds.size.width)),
        (format!("{prefix}H"), f32_field(bounds.size.height)),
    ]
}

pub(crate) fn radii_fields(prefix: &str, radii: &Corners<ScaledPixels>) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}TopLeft"), f32_field(radii.top_left)),
        (format!("{prefix}TopRight"), f32_field(radii.top_right)),
        (format!("{prefix}BottomRight"), f32_field(radii.bottom_right)),
        (format!("{prefix}BottomLeft"), f32_field(radii.bottom_left)),
    ]
}

pub(crate) fn widths_fields(prefix: &str, widths: &Edges<ScaledPixels>) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}Top"), f32_field(widths.top)),
        (format!("{prefix}Right"), f32_field(widths.right)),
        (format!("{prefix}Bottom"), f32_field(widths.bottom)),
        (format!("{prefix}Left"), f32_field(widths.left)),
    ]
}

pub(crate) fn common_op_fields(
    kind: &str,
    order: u32,
    batch_index: usize,
    bounds: &Bounds<ScaledPixels>,
    mask: &Bounds<ScaledPixels>,
) -> Vec<(String, FieldValue)> {
    let mut fields = vec![
        ("kind".into(), FieldValue::str(kind)),
        ("order".into(), FieldValue::Uint(order as u64)),
        ("batchIndex".into(), FieldValue::Uint(batch_index as u64)),
    ];
    fields.extend(bounds_fields("bounds", bounds));
    fields.extend(bounds_fields("mask", mask));
    fields
}

pub(crate) fn batch_kind_name(batch: &gpui::PrimitiveBatch) -> &'static str {
    match batch {
        gpui::PrimitiveBatch::Shadows { .. } => "shadows",
        gpui::PrimitiveBatch::Quads { .. } => "quads",
        gpui::PrimitiveBatch::Paths { .. } => "paths",
        gpui::PrimitiveBatch::Underlines(_) => "underlines",
        gpui::PrimitiveBatch::MonochromeSprites { .. } => "monochrome-sprites",
        gpui::PrimitiveBatch::SubpixelSprites { .. } => "subpixel-sprites",
        gpui::PrimitiveBatch::PolychromeSprites { .. } => "polychrome-sprites",
        gpui::PrimitiveBatch::Surfaces(_) => "surfaces",
        gpui::PrimitiveBatch::BackdropFilters(_) => "backdrop-filters",
        gpui::PrimitiveBatch::FilterBoundary(_) => "filter-boundary",
    }
}

pub(crate) fn target_fields(target: &gpui::FilterRenderTarget) -> Vec<(String, FieldValue)> {
    let mut fields = Vec::new();
    match target {
        gpui::FilterRenderTarget::Inline => {
            fields.push(("target".into(), FieldValue::str("inline")));
        }
        gpui::FilterRenderTarget::Isolated(index) => {
            fields.push(("target".into(), FieldValue::str("isolated")));
            fields.push(("targetIndex".into(), FieldValue::Uint(index.as_usize() as u64)));
        }
    }
    fields
}

/// Emit the `scene-command` and `scene-op` events for the finished scene,
/// in plan order.
fn record_scene(scene: &Scene, recorder: &TraceRecorder, layer_count: u32) {
    let commands = scene.render_commands();
    for (batch_index, command) in commands.iter().enumerate() {
        match command {
            gpui::RenderCommand::Batch(batch) => {
                let mut fields = vec![
                    ("command".into(), FieldValue::str("batch")),
                    ("batchKind".into(), FieldValue::str(batch_kind_name(batch))),
                ];
                match batch {
                    gpui::PrimitiveBatch::Shadows { range, smoothed }
                    | gpui::PrimitiveBatch::Quads { range, smoothed } => {
                        fields.push(("rangeStart".into(), FieldValue::Uint(range.start as u64)));
                        fields.push(("rangeEnd".into(), FieldValue::Uint(range.end as u64)));
                        fields.push(("smoothed".into(), FieldValue::Uint(u64::from(*smoothed))));
                    }
                    gpui::PrimitiveBatch::Underlines(range)
                    | gpui::PrimitiveBatch::Surfaces(range)
                    | gpui::PrimitiveBatch::BackdropFilters(range) => {
                        fields.push(("rangeStart".into(), FieldValue::Uint(range.start as u64)));
                        fields.push(("rangeEnd".into(), FieldValue::Uint(range.end as u64)));
                    }
                    _ => {}
                }
                recorder.event_with("scene-command", None, fields);

                match batch {
                    gpui::PrimitiveBatch::Quads { range, .. } => {
                        for quad in &scene.quads[range.clone()] {
                            let mut fields = common_op_fields(
                                "quad",
                                quad.order,
                                batch_index,
                                &quad.bounds,
                                &quad.content_mask.bounds,
                            );
                            fields.extend(color_fields("background", &solid_hsla(&quad.background)));
                            fields.extend(color_fields("borderColor", &solid_hsla(&quad.border_color)));
                            fields.extend(radii_fields("radius", &quad.corner_radii));
                            fields.extend(widths_fields("border", &quad.border_widths));
                            fields.push((
                                "borderStyle".into(),
                                FieldValue::str(if quad.border_style == gpui::BorderStyle::Dashed {
                                    "dashed"
                                } else {
                                    "solid"
                                }),
                            ));
                            fields.push(("borderDashedLength".into(), FieldValue::f32(quad.border_dashed_length)));
                            fields.push(("borderDashedGap".into(), FieldValue::f32(quad.border_dashed_gap)));
                            fields.push(("cornerSmoothing".into(), FieldValue::f32(quad.corner_smoothing)));
                            fields.push(("padding".into(), FieldValue::Uint(quad.padding as u64)));
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    gpui::PrimitiveBatch::Shadows { range, .. } => {
                        for shadow in &scene.shadows[range.clone()] {
                            let mut fields = common_op_fields(
                                "shadow",
                                shadow.order,
                                batch_index,
                                &shadow.bounds,
                                &shadow.content_mask.bounds,
                            );
                            fields.push(("blurRadius".into(), f32_field(shadow.blur_radius)));
                            fields.extend(color_fields("color", &solid_hsla(&shadow.color)));
                            fields.extend(bounds_fields("elementBounds", &shadow.element_bounds));
                            fields.extend(radii_fields("elementRadius", &shadow.element_corner_radii));
                            fields.extend(radii_fields("radius", &shadow.corner_radii));
                            fields.push(("inset".into(), FieldValue::Uint(u64::from(shadow.inset.is_enabled()))));
                            fields.push(("cornerSmoothing".into(), FieldValue::f32(shadow.corner_smoothing)));
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    gpui::PrimitiveBatch::Underlines(range) => {
                        for underline in &scene.underlines[range.clone()] {
                            let mut fields = common_op_fields(
                                "underline",
                                underline.order,
                                batch_index,
                                &underline.bounds,
                                &underline.content_mask.bounds,
                            );
                            fields.push(("thickness".into(), f32_field(underline.thickness)));
                            fields.push(("wavy".into(), FieldValue::Uint(u64::from(underline.wavy.is_enabled()))));
                            // The underline record stores a raw
                            // SceneHsla; convert through the public
                            // Into<palette::Hsla> and normalize the hue
                            // (the same derivation color_fields uses,
                            // reproducing the stored bits).
                            let color: gpui::Hsla = underline.color.into();
                            fields.extend(color_fields("color", &color));
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    gpui::PrimitiveBatch::Surfaces(range) => {
                        let opacities = scene.surface_opacities();
                        for (index, surface) in scene.surfaces[range.clone()].iter().enumerate() {
                            let mut fields = common_op_fields(
                                "surface",
                                surface.order,
                                batch_index,
                                &surface.bounds,
                                &surface.content_mask.bounds,
                            );
                            fields.push(("sourceTag".into(), FieldValue::Uint(0)));
                            fields.push((
                                "opacity".into(),
                                FieldValue::f32(opacities[range.start + index]),
                            ));
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    gpui::PrimitiveBatch::BackdropFilters(range) => {
                        for filter in &scene.backdrop_filters[range.clone()] {
                            let mut fields = filter_op_fields(
                                "backdrop-filter",
                                filter.order,
                                batch_index,
                                filter,
                            );
                            fields.pop(); // drop isStart; backdrops have no marker
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    _ => {}
                }
            }
            gpui::RenderCommand::BeginFilter { boundary_index, target } => {
                let mut fields = vec![
                    ("command".into(), FieldValue::str("begin-filter")),
                    ("boundaryIndex".into(), FieldValue::Uint(*boundary_index as u64)),
                ];
                fields.extend(target_fields(target));
                recorder.event_with("scene-command", None, fields);
                let boundary = &scene.filter_boundaries[*boundary_index];
                let fields = filter_op_fields("filter-boundary", boundary.order, batch_index, boundary);
                recorder.event_with("scene-op", None, fields);
            }
            gpui::RenderCommand::EndFilter {
                boundary_index,
                closing_boundary_index,
                target,
            } => {
                let mut fields = vec![
                    ("command".into(), FieldValue::str("end-filter")),
                    ("boundaryIndex".into(), FieldValue::Uint(*boundary_index as u64)),
                    (
                        "closingBoundaryIndex".into(),
                        FieldValue::Uint(*closing_boundary_index as u64),
                    ),
                ];
                fields.extend(target_fields(target));
                recorder.event_with("scene-command", None, fields);
                let boundary = &scene.filter_boundaries[*closing_boundary_index];
                let fields = filter_op_fields("filter-boundary", boundary.order, batch_index, boundary);
                recorder.event_with("scene-op", None, fields);
            }
        }
    }

    // The scene totals and plan requirements.
    let requirements = scene.render_plan().requirements();
    let fields = vec![
        ("opCount".into(), FieldValue::Uint(scene.len() as u64)),
        ("layerCount".into(), FieldValue::Uint(layer_count as u64)),
        ("quadCount".into(), FieldValue::Uint(scene.quads.len() as u64)),
        ("shadowCount".into(), FieldValue::Uint(scene.shadows.len() as u64)),
        ("underlineCount".into(), FieldValue::Uint(scene.underlines.len() as u64)),
        ("backdropCount".into(), FieldValue::Uint(scene.backdrop_filters.len() as u64)),
        ("boundaryCount".into(), FieldValue::Uint(scene.filter_boundaries.len() as u64)),
        ("surfaceCount".into(), FieldValue::Uint(scene.surfaces.len() as u64)),
        ("commandCount".into(), FieldValue::Uint(requirements.command_count as u64)),
        ("instanceBatchCount".into(), FieldValue::Uint(requirements.instance_batch_count as u64)),
        (
            "pathRasterizationVertexCount".into(),
            FieldValue::Uint(requirements.path_rasterization_vertex_count as u64),
        ),
        ("pathSpriteCount".into(), FieldValue::Uint(requirements.path_sprite_count as u64)),
        (
            "isolatedFilterCount".into(),
            FieldValue::Uint(requirements.isolated_filter_count as u64),
        ),
        (
            "isolatedTargetCount".into(),
            FieldValue::Uint(requirements.isolated_target_count as u64),
        ),
        (
            "usesOffscreenTarget".into(),
            FieldValue::Uint(u64::from(requirements.uses_offscreen_target)),
        ),
        ("usesPathTarget".into(), FieldValue::Uint(u64::from(requirements.uses_path_target))),
    ];
    recorder.event_with("scene-meta", None, fields);
}

pub(crate) fn filter_op_fields(
    kind: &str,
    order: u32,
    batch_index: usize,
    filter: &impl FilterRecordLike,
) -> Vec<(String, FieldValue)> {
    let mut fields = common_op_fields(kind, order, batch_index, filter.bounds(), filter.mask());
    fields.extend(radii_fields("radius", filter.radii()));
    fields.push(("cornerSmoothing".into(), FieldValue::f32(filter.smoothing())));
    fields.push(("blurRadius".into(), FieldValue::f32(filter.blur())));
    fields.push(("opacity".into(), FieldValue::f32(filter.opacity())));
    fields.push(("isStart".into(), FieldValue::Uint(u64::from(filter.is_start()))));
    fields
}

/// A view over the backdrop-filter/filter-boundary record shapes used by
/// filter_op_fields.
pub(crate) trait FilterRecordLike {
    fn bounds(&self) -> &Bounds<ScaledPixels>;
    fn mask(&self) -> &Bounds<ScaledPixels>;
    fn radii(&self) -> &Corners<ScaledPixels>;
    fn smoothing(&self) -> f32;
    fn blur(&self) -> f32;
    fn opacity(&self) -> f32;
    fn is_start(&self) -> bool;
}

impl FilterRecordLike for gpui::BackdropFilter {
    fn bounds(&self) -> &Bounds<ScaledPixels> {
        &self.bounds
    }
    fn mask(&self) -> &Bounds<ScaledPixels> {
        &self.content_mask.bounds
    }
    fn radii(&self) -> &Corners<ScaledPixels> {
        &self.corner_radii
    }
    fn smoothing(&self) -> f32 {
        self.corner_smoothing
    }
    fn blur(&self) -> f32 {
        self.max_blur_radius()
    }
    fn opacity(&self) -> f32 {
        self.opacity
    }
    fn is_start(&self) -> bool {
        false
    }
}

impl FilterRecordLike for FilterBoundary {
    fn bounds(&self) -> &Bounds<ScaledPixels> {
        &self.bounds
    }
    fn mask(&self) -> &Bounds<ScaledPixels> {
        &self.content_mask.bounds
    }
    fn radii(&self) -> &Corners<ScaledPixels> {
        &self.corner_radii
    }
    fn smoothing(&self) -> f32 {
        self.corner_smoothing
    }
    fn blur(&self) -> f32 {
        self.max_blur_radius()
    }
    fn opacity(&self) -> f32 {
        self.opacity
    }
    fn is_start(&self) -> bool {
        self.is_start
    }
}

// ---------------------------------------------------------------------------
// Case execution
// ---------------------------------------------------------------------------

fn run_case(
    cx: &mut TestAppContext,
    case: &ScenePaintingCase,
    recorder: &TraceRecorder,
    scenes: &mut HashMap<String, Scene>,
) -> Result<()> {
    let (_, vcx) = cx.add_window_view(|_, _| gpui::Empty);

    let scale = vcx.update(|window, _cx| window.scale_factor());
    let rem_size = DEFAULT_REM_SIZE;
    recorder.event_with(
        "case-begin",
        Some(&case.label),
        [
            ("label".into(), FieldValue::str(case.label.clone())),
            ("remSize".into(), FieldValue::f32(rem_size as f32)),
            ("scale".into(), FieldValue::f32(scale)),
        ],
    );

    // Layout section: the probe lays out the style tree and collects the
    // node bounds (recording layout-bounds events like layout-metrics).
    let bounds_by_label = Rc::new(RefCell::new(BTreeMap::new()));
    let probe = SceneLayoutProbe {
        tree: case.style_tree.clone(),
        recorder: recorder.clone(),
        bounds: bounds_by_label.clone(),
    };
    let available = Size {
        width: AvailableSpace::Definite(px(case.available_space.width as f32)),
        height: AvailableSpace::Definite(px(case.available_space.height as f32)),
    };
    vcx.draw(Point::default(), available, |_window, _cx| probe);
    let bounds_by_label = bounds_by_label.borrow().clone();

    // Scene section: build the scene through the public API.
    let mut scene = Scene::default();
    let default_mask = case_mask(case);
    let mut state = ScriptState {
        scene: &mut scene,
        scale,
        element_opacity: 1.0,
        opacity_stack: Vec::new(),
        layer_pushed_stack: Vec::new(),
        layer_push_count: 0,
        pending_boundaries: Vec::new(),
    };

    for op in &case.paint_script {
        match op {
            PaintOp::PaintBox {
                label,
                background,
                corner_radius,
                corner_radii,
                border_width,
                border_widths,
                border_color,
                border_style,
                corner_smoothing,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let bg = match background {
                    Some(hex) => Background::from(parse_hsla(hex)?),
                    None => transparent_black().into(),
                };
                let bc = match border_color {
                    Some(hex) => Background::from(parse_hsla(hex)?),
                    None => transparent_black().into(),
                };
                let style = match border_style.as_deref() {
                    None | Some("Solid") => gpui::BorderStyle::Solid,
                    Some("Dashed") => gpui::BorderStyle::Dashed,
                    Some(other) => bail!("unsupported border style {other:?}"),
                };
                let details = PaintOpDetails {
                    background: &bg,
                    corner_radii: &corners_of(*corner_radius, *corner_radii),
                    border_widths: &edges_of(*border_width, *border_widths),
                    border_color: &bc,
                    border_style: &style,
                    corner_smoothing: corner_smoothing.as_ref().unwrap_or(&0.0),
                    mask: &effective_mask(*mask, default_mask),
                };
                paint_box(&mut state, bounds, &details)?;
            }
            PaintOp::PaintShadow {
                label,
                offset_x,
                offset_y,
                blur_radius,
                spread_radius,
                color,
                inset,
                corner_radius,
                corner_radii,
                corner_smoothing,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                paint_shadow(
                    &mut state,
                    bounds,
                    logical_point(offset_x.unwrap_or(0.0), offset_y.unwrap_or(0.0)),
                    px(blur_radius.unwrap_or(0.0) as f32),
                    px(spread_radius.unwrap_or(0.0) as f32),
                    Background::from(parse_hsla(color)?),
                    inset.unwrap_or(false),
                    corners_of(*corner_radius, *corner_radii),
                    corner_smoothing.unwrap_or(0.0),
                    effective_mask(*mask, default_mask),
                );
            }
            PaintOp::PaintUnderline {
                label,
                width,
                thickness,
                wavy,
                color,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let underline_width = match width {
                    Some(width) => px(*width as f32),
                    None => bounds.size.width,
                };
                let style = UnderlineStyle {
                    thickness: px(*thickness as f32),
                    color: Some(parse_hsla(color)?),
                    wavy: wavy.unwrap_or(false),
                };
                paint_underline(&mut state, bounds.origin, underline_width, &style, effective_mask(*mask, default_mask));
            }
            PaintOp::BeginLayer {
                label,
                bounds,
                opacity,
                mask,
            } => {
                let layer_bounds = match (label, bounds) {
                    (Some(label), _) => node_bounds(&bounds_by_label, label)?,
                    (None, Some(rect)) => logical_bounds(*rect),
                    (None, None) => bail!("begin-layer requires a label or explicit bounds"),
                };
                let mask = effective_mask(*mask, default_mask);
                // window.rs paint_layer: push only when the clipped
                // bounds are non-empty; with_element_opacity applies
                // regardless (it wraps the whole layer).
                let clipped = intersect_logical(layer_bounds, mask);
                let pushed = !clipped.is_empty();
                if pushed {
                    state.scene.push_layer(cover_bounds(clipped, state.scale));
                    state.layer_push_count += 1;
                }
                state.layer_pushed_stack.push(pushed);
                state.opacity_stack.push(state.element_opacity);
                if let Some(opacity) = opacity {
                    state.element_opacity *= *opacity as f32;
                }
            }
            PaintOp::EndLayer => {
                if let Some(pushed) = state.layer_pushed_stack.pop() {
                    if pushed {
                        state.scene.pop_layer();
                    }
                } else {
                    bail!("end-layer without a matching begin-layer");
                }
                if let Some(previous) = state.opacity_stack.pop() {
                    state.element_opacity = previous;
                }
            }
            PaintOp::RaiseFloor => {
                state.scene.raise_order_floor();
            }
            PaintOp::BeginFilterGroup {
                label,
                blur_radius,
                corner_radius,
                corner_radii,
                corner_smoothing,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let boundary = filter_boundary_record(
                    bounds,
                    *blur_radius,
                    corners_of(*corner_radius, *corner_radii),
                    corner_smoothing.unwrap_or(0.0),
                    effective_mask(*mask, default_mask),
                    state.scale,
                );
                if boundary.filters.is_empty() {
                    // Identity filters run inline (no markers); the group's
                    // contents are simply painted normally, and the
                    // matching end-filter-group is a no-op.
                    state.pending_boundaries.push(None);
                } else {
                    state.scene.insert_primitive(boundary.clone());
                    state.pending_boundaries.push(Some(boundary));
                }
            }
            PaintOp::EndFilterGroup => {
                match state
                    .pending_boundaries
                    .pop()
                    .with_context(|| "end-filter-group without a matching begin")?
                {
                    Some(boundary) => {
                        state.scene.insert_primitive(FilterBoundary {
                            is_start: false,
                            ..boundary
                        });
                    }
                    None => {
                        // Identity group: already ran inline.
                    }
                }
            }
            PaintOp::PaintBackdrop {
                label,
                blur_radius,
                corner_radius,
                corner_radii,
                corner_smoothing,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                paint_backdrop(
                    &mut state,
                    bounds,
                    *blur_radius,
                    corners_of(*corner_radius, *corner_radii),
                    corner_smoothing.unwrap_or(0.0),
                    effective_mask(*mask, default_mask),
                );
            }
            PaintOp::Replay { source, start, end } => {
                let prev = scenes
                    .get(source)
                    .with_context(|| format!("replay references unknown source case {source:?}"))?;
                state
                    .scene
                    .replay((*start as usize)..(*end as usize), prev);
            }
        }
    }

    if !state.pending_boundaries.is_empty() {
        bail!("unclosed content-filter group(s) at the end of the paint script");
    }
    if !state.layer_pushed_stack.is_empty() {
        bail!("unbalanced layer(s) at the end of the paint script");
    }
    let layer_count = state.layer_push_count;
    state.scene.finish();

    record_scene(&scene, recorder, layer_count);

    // The finished scene stays pinned for later replay ranges.
    scenes.insert(case.label.clone(), scene);
    recorder.event("case-end", Some(&case.label));
    Ok(())
}

pub fn run(envelope: &Envelope) -> Vec<crate::trace::TraceEvent> {
    let recorder = TraceRecorder::new();
    let inputs = envelope
        .inputs
        .scene_cases
        .as_ref()
        .unwrap_or_else(|| panic!("fixture kind scene-painting-v1 requires inputs.scene_cases"));
    let mut cx = TestAppContext::single();
    let mut scenes: HashMap<String, Scene> = HashMap::new();
    for case in &inputs.cases {
        run_case(&mut cx, case, &recorder, &mut scenes)
            .unwrap_or_else(|e| panic!("scene-painting case {:?} failed: {e:#}", case.label));
    }
    recorder.into_events()
}
