//! Fixture `paths-filters-v1` (ticket16): reference scene records for
//! path builder scripts, surface primitives, sprites and nested filter
//! groups beyond depth two.
//!
//! ## The public oracle boundary
//!
//! Exactly the fx-0003 boundary (`scene-painting-v1`): the pinned
//! `Scene` public API plus, for paths, the pinned public `PathBuilder`
//! (`crates/gpui/src/path_builder.rs`) — the same lyon tessellation
//! the native scene service's `path_script` entry drives. The fixture
//! builds records through those public APIs and records the finished
//! scene: op order, per-path vertices (bit-exact tessellation output),
//! batch/plan structure including the filter-target plan (target
//! assignments, group ranges) and the paired surface opacities.
//!
//! Sprites are record-level (tile metadata through the public sprite
//! structs); real atlas-backed glyph drawing is exercised by the
//! real-window composite test of the port.
//!
//! ## What each case does
//!
//! 1. One window per case; the case's style tree is laid out through
//!    `Window::request_layout` and every node's bounds recorded
//!    (`layout-bounds` events) — the fx-0003 layout probe is reused.
//! 2. The paint script runs against a fresh `Scene`:
//!    - `paint-box` (background-only form of the pinned `paint_quad`);
//!    - `paint-path`: `PathBuilder` command script → `build()` → the
//!      pinned `paint_path` record construction (content mask, color
//!      opacity, `path.scale(scale_factor)`);
//!    - `paint-monochrome/subpixel/polychrome-sprite` insert the public
//!      sprite records with explicit atlas tiles;
//!    - `paint-surface` inserts `PaintSurface` through the public
//!      `insert_primitive` (paired opacity 1.0);
//!    - `paint-backdrop`, `begin/end-filter-group`, `raise-floor`,
//!      `replay` mirror the fx-0003 ops.
//! 3. After `finish`, the fixture walks `render_commands()` and records
//!    `scene-command` events (batch structure incl. the paths' vertex
//!    and sprite counts), `scene-op` events (one per primitive in final
//!    order), `path-vertex` events (the exact tessellation output) and
//!    a `scene-meta` event with totals and requirements.
//!
//! Determinism: everything above is a pure function of the envelope
//! and the pinned tessellator; the reference test window's fixed scale
//! factor (2.0) is recorded in `case-begin`.

use crate::envelope::{PathCommandIn, PathFilterCase, PathFilterOp, RectIn, TileIn};
use crate::trace::{FieldValue, TraceRecorder};
use anyhow::{Context as _, Result, bail};
use gpui::{
    Background, Bounds, ColorExt, ContentMask, Filter, FilterBoundary, Path, PathBuilder,
    PathStyle, Pixels, Point, Scene, Size, TestAppContext, px,
};
use lyon::tessellation::{FillOptions, FillRule, LineCap, LineJoin, StrokeOptions};
use std::cell::RefCell;
use std::collections::BTreeMap;
use std::collections::HashMap;
use std::rc::Rc;

use crate::fixtures::scene_painting::{
    SceneLayoutProbe, batch_kind_name, color_fields, common_op_fields, corners_of, effective_mask,
    f32_field, filter_op_fields, filter_boundary_record, parse_hsla, radii_fields, snap_bounds,
    snapped_content_mask, target_fields,
};

/// Default rem size of a reference window (logical px; window.rs).
const DEFAULT_REM_SIZE: f64 = 16.0;

// ---------------------------------------------------------------------------
// Paint script execution
// ---------------------------------------------------------------------------

struct ScriptState<'a> {
    scene: &'a mut Scene,
    scale: f32,
    element_opacity: f32,
    layer_pushed_stack: Vec<bool>,
    layer_push_count: u32,
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

/// Build the pinned `PathBuilder` from a command script (the public API
/// shapes: `fill()`/`stroke(width)`/`with_style`, `dash_array`, the
/// geometry commands, the transform mutators).
fn build_path(commands: &[PathCommandIn]) -> Result<Path<Pixels>> {
    let mut builder = PathBuilder::default();
    let point = |x: f64, y: f64| gpui::point(px(x as f32), px(y as f32));
    for command in commands {
        match command {
            PathCommandIn::MoveTo { x, y } => builder.move_to(point(*x, *y)),
            PathCommandIn::LineTo { x, y } => builder.line_to(point(*x, *y)),
            PathCommandIn::CurveTo { to_x, to_y, ctrl_x, ctrl_y } => {
                builder.curve_to(point(*to_x, *to_y), point(*ctrl_x, *ctrl_y));
            }
            PathCommandIn::CubicTo { to_x, to_y, a_x, a_y, b_x, b_y } => {
                builder.cubic_bezier_to(point(*to_x, *to_y), point(*a_x, *a_y), point(*b_x, *b_y));
            }
            PathCommandIn::ArcTo {
                radius_x,
                radius_y,
                x_rotation,
                large_arc,
                sweep,
                x,
                y,
            } => {
                builder.arc_to(
                    point(*radius_x, *radius_y),
                    px(*x_rotation as f32),
                    *large_arc,
                    *sweep,
                    point(*x, *y),
                );
            }
            PathCommandIn::Polygon { points, closed } => {
                let points: Vec<Point<Pixels>> = points
                    .iter()
                    .map(|(x, y)| point(*x, *y))
                    .collect();
                builder.add_polygon(&points, *closed);
            }
            PathCommandIn::Close => builder.close(),
            PathCommandIn::Style {
                style,
                tolerance,
                fill_rule,
                sweep_orientation,
                handle_intersections,
                width,
                start_cap,
                end_cap,
                line_join,
                miter_limit,
            } => {
                let style = match style.as_str() {
                    "fill" => {
                        let mut options = FillOptions::default();
                        if let Some(tolerance) = tolerance {
                            options.tolerance = *tolerance as f32;
                        }
                        if let Some(rule) = fill_rule {
                            options.fill_rule = match rule.as_str() {
                                "even-odd" => FillRule::EvenOdd,
                                "non-zero" => FillRule::NonZero,
                                other => bail!("unsupported fill rule {other:?}"),
                            };
                        }
                        if let Some(sweep) = sweep_orientation {
                            options.sweep_orientation = match sweep.as_str() {
                                "vertical" => lyon::tessellation::Orientation::Vertical,
                                "horizontal" => lyon::tessellation::Orientation::Horizontal,
                                other => bail!("unsupported sweep orientation {other:?}"),
                            };
                        }
                        if let Some(handle) = handle_intersections {
                            options.handle_intersections = *handle;
                        }
                        PathStyle::Fill(options)
                    }
                    "stroke" => {
                        let mut options = StrokeOptions::default();
                        if let Some(width) = width {
                            options.line_width = *width as f32;
                        }
                        if let Some(cap) = start_cap {
                            options.start_cap = line_cap_of(cap)?;
                        }
                        if let Some(cap) = end_cap {
                            options.end_cap = line_cap_of(cap)?;
                        }
                        if let Some(join) = line_join {
                            options.line_join = match join.as_str() {
                                "miter" => LineJoin::Miter,
                                "round" => LineJoin::Round,
                                "bevel" => LineJoin::Bevel,
                                other => bail!("unsupported line join {other:?}"),
                            };
                        }
                        if let Some(limit) = miter_limit {
                            options.miter_limit = *limit as f32;
                        }
                        PathStyle::Stroke(options)
                    }
                    other => bail!("unsupported path style {other:?}"),
                };
                builder = builder.with_style(style);
            }
            PathCommandIn::Dash { lengths } => {
                let lengths: Vec<Pixels> = lengths.iter().map(|l| px(*l as f32)).collect();
                builder = builder.dash_array(&lengths);
            }
            PathCommandIn::Translate { x, y } => builder.translate(point(*x, *y)),
            PathCommandIn::Scale { factor } => builder.scale(*factor as f32),
            PathCommandIn::Rotate { degrees } => builder.rotate(*degrees as f32),
        }
    }
    builder
        .build()
        .map_err(|e| anyhow::anyhow!("tessellating path: {e}"))
}

fn line_cap_of(cap: &str) -> Result<LineCap> {
    match cap {
        "butt" => Ok(LineCap::Butt),
        "square" => Ok(LineCap::Square),
        "round" => Ok(LineCap::Round),
        other => bail!("unsupported line cap {other:?}"),
    }
}

/// The pinned `Window::paint_path` record construction (window.rs:4715):
/// the unsnapped content mask, the color with element opacity, and
/// `path.scale(scale_factor)` at insertion.
fn paint_path(
    state: &mut ScriptState,
    commands: &[PathCommandIn],
    color: Background,
    mask: Bounds<Pixels>,
) -> Result<()> {
    let mut path = build_path(commands)?;
    path.content_mask = ContentMask {
        bounds: mask,
        ..Default::default()
    };
    path.color = color.opacity(state.element_opacity);
    state
        .scene
        .insert_primitive(path.scale(state.scale));
    Ok(())
}

fn tile_of(tile: TileIn) -> gpui::AtlasTile {
    gpui::AtlasTile {
        texture_id: gpui::AtlasTextureId {
            index: tile.texture_index,
            kind: match tile.texture_kind {
                0 => gpui::AtlasTextureKind::Monochrome,
                1 => gpui::AtlasTextureKind::Polychrome,
                _ => gpui::AtlasTextureKind::Subpixel,
            },
        },
        tile_id: gpui::TileId(tile.tile_id),
        padding: tile.padding,
        bounds: Bounds {
            origin: gpui::point(
                gpui::DevicePixels(tile.bounds.x as i32),
                gpui::DevicePixels(tile.bounds.y as i32),
            ),
            size: Size {
                width: gpui::DevicePixels(tile.bounds.width as i32),
                height: gpui::DevicePixels(tile.bounds.height as i32),
            },
        },
    }
}

fn sprite_bounds(bounds: Bounds<Pixels>, state: &ScriptState) -> Bounds<gpui::ScaledPixels> {
    snap_bounds(bounds, state.scale)
}

// ---------------------------------------------------------------------------
// Trace emission
// ---------------------------------------------------------------------------

fn tile_fields(prefix: &str, tile: &gpui::AtlasTile) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}TextureIndex"), FieldValue::Uint(tile.texture_id.index as u64)),
        (
            format!("{prefix}TextureKind"),
            FieldValue::Uint(match tile.texture_id.kind {
                gpui::AtlasTextureKind::Monochrome => 0,
                gpui::AtlasTextureKind::Polychrome => 1,
                gpui::AtlasTextureKind::Subpixel => 2,
            }),
        ),
        (format!("{prefix}TileId"), FieldValue::Uint(tile.tile_id.0 as u64)),
        (format!("{prefix}Padding"), FieldValue::Uint(tile.padding as u64)),
        (
            format!("{prefix}X"),
            f32_field(gpui::ScaledPixels(tile.bounds.origin.x.0 as f32)),
        ),
        (
            format!("{prefix}Y"),
            f32_field(gpui::ScaledPixels(tile.bounds.origin.y.0 as f32)),
        ),
        (
            format!("{prefix}W"),
            f32_field(gpui::ScaledPixels(tile.bounds.size.width.0 as f32)),
        ),
        (
            format!("{prefix}H"),
            f32_field(gpui::ScaledPixels(tile.bounds.size.height.0 as f32)),
        ),
    ]
}

fn transformation_fields(
    prefix: &str,
    transformation: &gpui::TransformationMatrix,
) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}00"), FieldValue::f32(transformation.rotation_scale[0][0])),
        (format!("{prefix}01"), FieldValue::f32(transformation.rotation_scale[0][1])),
        (format!("{prefix}10"), FieldValue::f32(transformation.rotation_scale[1][0])),
        (format!("{prefix}11"), FieldValue::f32(transformation.rotation_scale[1][1])),
        (format!("{prefix}TX"), FieldValue::f32(transformation.translation[0])),
        (format!("{prefix}TY"), FieldValue::f32(transformation.translation[1])),
    ]
}

/// Emit the `scene-command`, `scene-op`, `path-vertex` and `scene-meta`
/// events for the finished scene, in plan order.
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
                    gpui::PrimitiveBatch::Paths {
                        range,
                        rasterization_vertex_count,
                        sprite_count,
                    } => {
                        fields.push(("rangeStart".into(), FieldValue::Uint(range.start as u64)));
                        fields.push(("rangeEnd".into(), FieldValue::Uint(range.end as u64)));
                        fields.push((
                            "rasterizationVertexCount".into(),
                            FieldValue::Uint(*rasterization_vertex_count as u64),
                        ));
                        fields.push(("spriteCount".into(), FieldValue::Uint(*sprite_count as u64)));
                    }
                    gpui::PrimitiveBatch::MonochromeSprites { texture_id, range }
                    | gpui::PrimitiveBatch::SubpixelSprites { texture_id, range }
                    | gpui::PrimitiveBatch::PolychromeSprites { texture_id, range, .. } => {
                        fields.push(("rangeStart".into(), FieldValue::Uint(range.start as u64)));
                        fields.push(("rangeEnd".into(), FieldValue::Uint(range.end as u64)));
                        fields.push(("textureIndex".into(), FieldValue::Uint(texture_id.index as u64)));
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
                            fields.extend(color_fields("background", &quad.background.as_solid().unwrap_or_default()));
                            fields.extend(radii_fields("radius", &quad.corner_radii));
                            fields.push(("cornerSmoothing".into(), FieldValue::f32(quad.corner_smoothing)));
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    gpui::PrimitiveBatch::Paths { range, .. } => {
                        for path in &scene.paths[range.clone()] {
                            let mut fields = common_op_fields(
                                "path",
                                path.order,
                                batch_index,
                                &path.bounds,
                                &path.content_mask.bounds,
                            );
                            fields.extend(color_fields("color", &path.color.as_solid().unwrap_or_default()));
                            fields.push(("vertexCount".into(), FieldValue::Uint(path.vertices.len() as u64)));
                            fields.push(("pathId".into(), FieldValue::Uint(path.id.0 as u64)));
                            recorder.event_with("scene-op", None, fields);
                            for vertex in &path.vertices {
                                recorder.event_with(
                                    "path-vertex",
                                    None,
                                    vec![
                                        ("x".into(), FieldValue::f32(vertex.xy_position.x.0)),
                                        ("y".into(), FieldValue::f32(vertex.xy_position.y.0)),
                                        ("s".into(), FieldValue::f32(vertex.st_position.x)),
                                        ("t".into(), FieldValue::f32(vertex.st_position.y)),
                                    ],
                                );
                            }
                        }
                    }
                    gpui::PrimitiveBatch::MonochromeSprites { range, .. } => {
                        for sprite in &scene.monochrome_sprites[range.clone()] {
                            let mut fields = common_op_fields(
                                "monochrome-sprite",
                                sprite.order,
                                batch_index,
                                &sprite.bounds,
                                &sprite.content_mask.bounds,
                            );
                            let color: gpui::Hsla = sprite.color.into();
                            fields.extend(color_fields("color", &color));
                            fields.extend(tile_fields("tile", &sprite.tile));
                            fields.extend(transformation_fields("transform", &sprite.transformation));
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    gpui::PrimitiveBatch::SubpixelSprites { range, .. } => {
                        for sprite in &scene.subpixel_sprites[range.clone()] {
                            let mut fields = common_op_fields(
                                "subpixel-sprite",
                                sprite.order,
                                batch_index,
                                &sprite.bounds,
                                &sprite.content_mask.bounds,
                            );
                            let color: gpui::Hsla = sprite.color.into();
                            fields.extend(color_fields("color", &color));
                            fields.extend(tile_fields("tile", &sprite.tile));
                            fields.extend(transformation_fields("transform", &sprite.transformation));
                            recorder.event_with("scene-op", None, fields);
                        }
                    }
                    gpui::PrimitiveBatch::PolychromeSprites { range, .. } => {
                        for sprite in &scene.polychrome_sprites[range.clone()] {
                            let mut fields = common_op_fields(
                                "polychrome-sprite",
                                sprite.order,
                                batch_index,
                                &sprite.bounds,
                                &sprite.content_mask.bounds,
                            );
                            fields.extend(radii_fields("radius", &sprite.corner_radii));
                            fields.push(("grayscale".into(), FieldValue::Uint(u64::from(sprite.grayscale.is_enabled()))));
                            fields.push(("opacity".into(), FieldValue::f32(sprite.opacity)));
                            fields.extend(tile_fields("tile", &sprite.tile));
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
        ("pathCount".into(), FieldValue::Uint(scene.paths.len() as u64)),
        ("backdropCount".into(), FieldValue::Uint(scene.backdrop_filters.len() as u64)),
        ("boundaryCount".into(), FieldValue::Uint(scene.filter_boundaries.len() as u64)),
        ("surfaceCount".into(), FieldValue::Uint(scene.surfaces.len() as u64)),
        (
            "monochromeSpriteCount".into(),
            FieldValue::Uint(scene.monochrome_sprites.len() as u64),
        ),
        (
            "subpixelSpriteCount".into(),
            FieldValue::Uint(scene.subpixel_sprites.len() as u64),
        ),
        (
            "polychromeSpriteCount".into(),
            FieldValue::Uint(scene.polychrome_sprites.len() as u64),
        ),
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

// ---------------------------------------------------------------------------
// Case execution
// ---------------------------------------------------------------------------

fn run_case(
    cx: &mut TestAppContext,
    case: &PathFilterCase,
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

    // Layout section: the fx-0003 layout probe (bounds collection).
    let bounds_by_label = Rc::new(RefCell::new(BTreeMap::new()));
    let probe = SceneLayoutProbe {
        tree: case.style_tree.clone(),
        recorder: recorder.clone(),
        bounds: bounds_by_label.clone(),
    };
    let available = Size {
        width: gpui::AvailableSpace::Definite(px(case.available_space.width as f32)),
        height: gpui::AvailableSpace::Definite(px(case.available_space.height as f32)),
    };
    vcx.draw(gpui::Point::default(), available, |_window, _cx| probe);
    let bounds_by_label = bounds_by_label.borrow().clone();

    // Scene section: build the scene through the public API.
    let mut scene = Scene::default();
    let default_mask = case_mask_from(case);
    let mut state = ScriptState {
        scene: &mut scene,
        scale,
        element_opacity: 1.0,
        layer_pushed_stack: Vec::new(),
        layer_push_count: 0,
        pending_boundaries: Vec::new(),
    };

    for op in &case.paint_script {
        match op {
            PathFilterOp::PaintBox {
                label,
                background,
                corner_radius,
                corner_radii,
                corner_smoothing,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let bg = Background::from(parse_hsla(background.as_deref().unwrap_or("00000000"))?);
                let quad = gpui::Quad {
                    order: 0,
                    bounds: snap_bounds(bounds, state.scale),
                    content_mask: snapped_content_mask(
                        effective_mask(*mask, default_mask),
                        state.scale,
                    ),
                    background: bg.opacity(state.element_opacity),
                    border_color: Background::from(parse_hsla("00000000")?),
                    corner_radii: corners_of(*corner_radius, *corner_radii).scale(state.scale),
                    border_widths: Default::default(),
                    border_style: gpui::BorderStyle::Solid,
                    border_dashed_length: 2.0,
                    border_dashed_gap: 1.0,
                    corner_smoothing: corner_smoothing.unwrap_or(0.0) as f32,
                    padding: 0,
                };
                if !quad.background.is_transparent() {
                    state.scene.insert_primitive(quad);
                }
            }
            PathFilterOp::PaintPath { commands, color, mask } => {
                paint_path(
                    &mut state,
                    commands,
                    Background::from(parse_hsla(color)?),
                    effective_mask(*mask, default_mask),
                )?;
            }
            PathFilterOp::PaintMonochromeSprite { label, color, tile, mask } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let sprite = gpui::MonochromeSprite {
                    order: 0,
                    padding: tile.padding,
                    bounds: sprite_bounds(bounds, &state),
                    content_mask: snapped_content_mask(
                        effective_mask(*mask, default_mask),
                        state.scale,
                    ),
                    color: parse_hsla(color)?.opacity(state.element_opacity).into(),
                    tile: tile_of(*tile),
                    transformation: Default::default(),
                };
                state.scene.insert_primitive(sprite);
            }
            PathFilterOp::PaintSubpixelSprite { label, color, tile, mask } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let sprite = gpui::SubpixelSprite {
                    order: 0,
                    padding: tile.padding,
                    bounds: sprite_bounds(bounds, &state),
                    content_mask: snapped_content_mask(
                        effective_mask(*mask, default_mask),
                        state.scale,
                    ),
                    color: parse_hsla(color)?.opacity(state.element_opacity).into(),
                    tile: tile_of(*tile),
                    transformation: Default::default(),
                };
                state.scene.insert_primitive(sprite);
            }
            PathFilterOp::PaintPolychromeSprite {
                label,
                opacity,
                corner_radius,
                corner_smoothing,
                tile,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let sprite = gpui::PolychromeSprite {
                    order: 0,
                    grayscale: gpui::ShaderBool::Disabled,
                    opacity: opacity.unwrap_or(1.0) as f32 * state.element_opacity,
                    corner_smoothing: corner_smoothing.unwrap_or(0.0) as f32,
                    bounds: sprite_bounds(bounds, &state),
                    content_mask: snapped_content_mask(
                        effective_mask(*mask, default_mask),
                        state.scale,
                    ),
                    corner_radii: corners_of(*corner_radius, None).scale(state.scale),
                    tile: tile_of(*tile),
                };
                state.scene.insert_primitive(sprite);
            }
            PathFilterOp::PaintSurface { label, mask } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                // The public insertion path: `insert_primitive` defaults
                // the paired opacity to 1.0 (the crate-private
                // `insert_surface` carries the element opacity; the
                // paired-opacity sort itself is exercised by the kernel
                // tests and recorded through `surface_opacities()`).
                state.scene.insert_primitive(gpui::PaintSurface {
                    order: 0,
                    bounds: snap_bounds(bounds, state.scale),
                    content_mask: snapped_content_mask(
                        effective_mask(*mask, default_mask),
                        state.scale,
                    ),
                    source: gpui::SurfaceSource::Unsupported(Default::default()),
                });
            }
            PathFilterOp::PaintBackdrop {
                label,
                blur_radius,
                corner_radius,
                corner_smoothing,
                mask,
            } => {
                let bounds = node_bounds(&bounds_by_label, label)?;
                let filters: Vec<_> = vec![Filter::Blur(px(*blur_radius as f32))]
                    .into_iter()
                    .filter(|filter| !filter.is_identity())
                    .map(|filter| filter.scale(state.scale))
                    .collect();
                if filters.is_empty() {
                    continue;
                }
                state.scene.insert_primitive(gpui::BackdropFilter {
                    order: 0,
                    bounds: snap_bounds(bounds, state.scale),
                    content_mask: snapped_content_mask(
                        effective_mask(*mask, default_mask),
                        state.scale,
                    ),
                    corner_radii: corners_of(*corner_radius, None).scale(state.scale),
                    corner_smoothing: corner_smoothing.unwrap_or(0.0) as f32,
                    filters: filters.into(),
                    opacity: state.element_opacity,
                });
            }
            PathFilterOp::BeginFilterGroup {
                label,
                bounds,
                blur_radius,
                corner_radius,
                corner_smoothing,
                mask,
            } => {
                let bounds = match (label, bounds) {
                    (Some(label), _) => node_bounds(&bounds_by_label, label)?,
                    (None, Some(rect)) => logical_bounds_of(*rect),
                    (None, None) => {
                        bail!("begin-filter-group requires a label or explicit bounds")
                    }
                };
                let boundary = filter_boundary_record(
                    bounds,
                    *blur_radius,
                    corners_of(*corner_radius, None),
                    corner_smoothing.unwrap_or(0.0),
                    effective_mask(*mask, default_mask),
                    state.scale,
                );
                if boundary.filters.is_empty() {
                    state.pending_boundaries.push(None);
                } else {
                    state.scene.insert_primitive(boundary.clone());
                    state.pending_boundaries.push(Some(boundary));
                }
            }
            PathFilterOp::EndFilterGroup => {
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
                    None => {}
                }
            }
            PathFilterOp::RaiseFloor => {
                state.scene.raise_order_floor();
            }
            PathFilterOp::Replay { source, start, end } => {
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

fn case_mask_from(case: &PathFilterCase) -> Bounds<Pixels> {
    Bounds {
        origin: gpui::point(px(0.), px(0.)),
        size: Size {
            width: px(case.available_space.width as f32),
            height: px(case.available_space.height as f32),
        },
    }
}

fn logical_bounds_of(rect: RectIn) -> Bounds<Pixels> {
    Bounds {
        origin: gpui::point(px(rect.x as f32), px(rect.y as f32)),
        size: Size {
            width: px(rect.width as f32),
            height: px(rect.height as f32),
        },
    }
}

pub fn run(envelope: &crate::envelope::Envelope) -> Vec<crate::trace::TraceEvent> {
    let recorder = TraceRecorder::new();
    let inputs = envelope
        .inputs
        .path_filter_cases
        .as_ref()
        .unwrap_or_else(|| panic!("fixture kind paths-filters-v1 requires inputs.path_filter_cases"));
    let mut cx = TestAppContext::single();
    let mut scenes: HashMap<String, Scene> = HashMap::new();
    for case in &inputs.cases {
        run_case(&mut cx, case, &recorder, &mut scenes)
            .unwrap_or_else(|e| panic!("paths-filters case {:?} failed: {e:#}", case.label));
    }
    recorder.into_events()
}
