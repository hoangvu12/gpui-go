//! Fixture `layout-metrics-v1`: per-case reference layout metrics.
//!
//! Observable surface exercised (all through public GPUI APIs):
//! - One window per case (`TestAppContext::add_window_view`), each drawn
//!   through `VisualTestContext::draw` with the case's available space,
//!   which may be definite, min-content or max-content per axis.
//! - The reference test window's fixed scale factor (2.0, see
//!   `platform/test/window.rs::scale_factor`); the scale is part of the
//!   fixture semantics and is recorded in each `case-begin` event.
//! - An optional per-case rem override applied through
//!   `Window::with_rem_size` around the probe's own layout requests, the
//!   same way the deferred-draw path scopes rem overrides around element
//!   prepaint (window.rs). The default rem size is 16px (`Window`'s
//!   `rem_size: px(16.)`).
//! - A labeled style tree with an extended style key set (grid templates,
//!   position/inset, overflow, flex wrap, align-content, borders, display),
//!   laid out through `Window::request_layout` and, for measured nodes,
//!   `Window::request_measured_layout` with a deterministic measure
//!   function.
//! - Every measure callback query and result, in the exact order taffy
//!   invokes them (taffy may query a node repeatedly), and every node's
//!   computed bounds as exact f32 bit patterns in logical pixels.
//!
//! The fixture records what the reference actually does; the Go port must
//! reproduce the trace exactly (see docs/conformance-contract.md).

use crate::envelope::{Envelope, LayoutMetricsCase, MeasuredSpec, StyleNode};
use crate::trace::{FieldValue, TraceRecorder};
use anyhow::{Context as _, Result, bail};
use gpui::{
    AbsoluteLength, AlignContent, AlignItems, App, AvailableSpace, DefiniteLength, Display, Edges,
    Element, FlexDirection, FlexWrap, GridTemplate, GridTemplateMinSize, Length, Pixels, Point,
    Position, Size, TestAppContext, Window,
};
use std::collections::BTreeMap;

/// Default rem size of a reference window, in logical pixels
/// (`Window`'s `rem_size: px(16.)`, window.rs).
const DEFAULT_REM_SIZE: f64 = 16.0;

/// Available-space mode names, matching the `availWidthTag`/`availHeightTag`
/// values recorded in `measure-query` events.
const AVAIL_DEFINITE: &str = "definite";
const AVAIL_MIN_CONTENT: &str = "min-content";
const AVAIL_MAX_CONTENT: &str = "max-content";

/// Measured spec kinds accepted by this fixture.
const MEASURED_FIXED: &str = "fixed";
const MEASURED_ECHO_KNOWN: &str = "echo-known";
const MEASURED_MINMAX: &str = "minmax";

// ---------------------------------------------------------------------------
// Style parsing
// ---------------------------------------------------------------------------

pub(crate) struct FlatNode {
    pub(crate) label: String,
    pub(crate) style: gpui::Style,
    pub(crate) children: Vec<usize>,
}

pub(crate) fn flatten_tree(node: &StyleNode, out: &mut Vec<FlatNode>) -> Result<usize> {
    let idx = out.len();
    out.push(FlatNode {
        label: node.label.clone(),
        style: build_style(&node.style)?,
        children: Vec::new(),
    });
    for child in &node.children {
        let child_idx = flatten_tree(child, out)?;
        out[idx].children.push(child_idx);
    }
    Ok(idx)
}

fn parse_length(value: &str) -> Result<Length> {
    Length::try_from(value).map_err(|e| anyhow::anyhow!("parsing Length {value:?}: {e:#}"))
}

fn parse_definite_length(value: &str) -> Result<DefiniteLength> {
    DefiniteLength::try_from(value)
        .map_err(|e| anyhow::anyhow!("parsing DefiniteLength {value:?}: {e:#}"))
}

fn parse_absolute_length(value: &str) -> Result<AbsoluteLength> {
    AbsoluteLength::try_from(value)
        .map_err(|e| anyhow::anyhow!("parsing AbsoluteLength {value:?}: {e:#}"))
}

fn parse_f32(value: &str) -> Result<f32> {
    value
        .parse::<f32>()
        .with_context(|| format!("parsing f32 {value:?}"))
}

fn parse_enum<T: for<'de> serde::Deserialize<'de>>(value: &str) -> Result<T> {
    serde_json::from_value(serde_json::Value::String(value.to_string()))
        .with_context(|| format!("parsing enum variant {value:?}"))
}

fn set_edges_definite(edges: &mut Edges<DefiniteLength>, value: DefiniteLength) {
    edges.top = value;
    edges.right = value;
    edges.bottom = value;
    edges.left = value;
}

fn set_edges_length(edges: &mut Edges<Length>, value: Length) {
    edges.top = value;
    edges.right = value;
    edges.bottom = value;
    edges.left = value;
}

fn set_edges_absolute(edges: &mut Edges<AbsoluteLength>, value: AbsoluteLength) {
    edges.top = value;
    edges.right = value;
    edges.bottom = value;
    edges.left = value;
}

/// Parse a grid template value. The pinned reference's `GridTemplate`
/// expresses exactly the forms its taffy adapter spells in
/// `crates/gpui/src/taffy.rs::to_grid_repeat`:
///
/// - `repeat(<n>, minmax(0, 1fr))` -> `GridTemplateMinSize::Zero`
/// - `repeat(<n>, minmax(min-content, 1fr))` -> `GridTemplateMinSize::MinContent`
/// - `repeat(<n>, minmax(0, max-content))` -> `GridTemplateMinSize::MaxContent`
///
/// The zero minimum may be written `0` or `0px`; whitespace is ignored.
/// Any other minimum (for example a definite pixel floor like
/// `minmax(10px, 1fr)`) fails the run: `Style` has no field that could
/// carry it, so accepting it would invent broader CSS than the reference
/// supports.
fn parse_grid_template(value: &str) -> Result<GridTemplate> {
    let compact: String = value.chars().filter(|c| !c.is_whitespace()).collect();
    let body = compact
        .strip_prefix("repeat(")
        .and_then(|rest| rest.strip_suffix(")"))
        .with_context(|| format!("parsing grid template {value:?}: expected repeat(<n>, <minmax>)"))?;
    let (count, minmax) = body
        .split_once(',')
        .with_context(|| format!("parsing grid template {value:?}: expected a count and a minmax()"))?;
    let repeat: u16 = count
        .parse()
        .with_context(|| format!("parsing grid template repeat count {count:?} in {value:?}"))?;
    let inner = minmax
        .strip_prefix("minmax(")
        .and_then(|rest| rest.strip_suffix(")"))
        .with_context(|| format!("parsing grid template {value:?}: expected minmax(<min>, <max>)"))?;
    let (min, max) = inner
        .split_once(',')
        .with_context(|| format!("parsing grid template {value:?}: expected a min and a max"))?;
    let min_size = match (min, max) {
        ("0" | "0px", "1fr") => GridTemplateMinSize::Zero,
        ("min-content", "1fr") => GridTemplateMinSize::MinContent,
        ("0" | "0px", "max-content") => GridTemplateMinSize::MaxContent,
        _ => bail!(
            "unsupported grid template {value:?}: the reference GridTemplate supports \
             repeat(<n>, minmax(0, 1fr)), repeat(<n>, minmax(min-content, 1fr)) and \
             repeat(<n>, minmax(0, max-content))"
        ),
    };
    Ok(GridTemplate { repeat, min_size })
}

/// Build a gpui `Style` from the fixture's documented subset: every key
/// supported by the layout-effects fixture plus the extended
/// layout-metrics keys (display, grid templates, position/inset, overflow,
/// flex wrap, align-content, border widths). Unknown keys fail the run
/// instead of being ignored.
pub(crate) fn build_style(map: &BTreeMap<String, String>) -> Result<gpui::Style> {
    let mut style = gpui::Style::default();
    for (key, value) in map {
        match key.as_str() {
            // Base set shared with the layout-effects-v1 fixture.
            "width" => style.size.width = parse_length(value)?,
            "height" => style.size.height = parse_length(value)?,
            "minWidth" => style.min_size.width = parse_length(value)?,
            "minHeight" => style.min_size.height = parse_length(value)?,
            "maxWidth" => style.max_size.width = parse_length(value)?,
            "maxHeight" => style.max_size.height = parse_length(value)?,
            "flexGrow" => style.flex_grow = parse_f32(value)?,
            "flexShrink" => style.flex_shrink = parse_f32(value)?,
            "flexBasis" => style.flex_basis = parse_length(value)?,
            "flexDirection" => style.flex_direction = parse_enum::<FlexDirection>(value)?,
            "justifyContent" => style.justify_content = Some(parse_enum::<AlignContent>(value)?),
            "alignItems" => style.align_items = Some(parse_enum::<AlignItems>(value)?),
            "alignSelf" => style.align_self = Some(parse_enum::<AlignItems>(value)?),
            "gap" => {
                let length = parse_definite_length(value)?;
                style.gap.width = length;
                style.gap.height = length;
            }
            "padding" => set_edges_definite(&mut style.padding, parse_definite_length(value)?),
            "margin" => set_edges_length(&mut style.margin, parse_length(value)?),
            "aspectRatio" => style.aspect_ratio = Some(parse_f32(value)?),
            // Extended set for layout-metrics-v1.
            "display" => style.display = parse_enum::<Display>(value)?,
            "position" => style.position = parse_enum::<Position>(value)?,
            "top" => style.inset.top = parse_length(value)?,
            "right" => style.inset.right = parse_length(value)?,
            "bottom" => style.inset.bottom = parse_length(value)?,
            "left" => style.inset.left = parse_length(value)?,
            "overflow" => {
                let overflow = parse_enum::<gpui::Overflow>(value)?;
                style.overflow.x = overflow;
                style.overflow.y = overflow;
            }
            "flexWrap" => style.flex_wrap = parse_enum::<FlexWrap>(value)?,
            "alignContent" => style.align_content = Some(parse_enum::<AlignContent>(value)?),
            "borderWidth" => {
                set_edges_absolute(&mut style.border_widths, parse_absolute_length(value)?)
            }
            "gridTemplateColumns" => style.grid_cols = Some(parse_grid_template(value)?),
            "gridTemplateRows" => style.grid_rows = Some(parse_grid_template(value)?),
            other => bail!("unsupported style key {other:?} in layout-metrics fixture"),
        }
    }
    Ok(style)
}

/// Map an available-space mode string plus a definite fallback value to the
/// `AvailableSpace` offered on that axis.
fn axis_available(mode: Option<&str>, definite: f64) -> Result<AvailableSpace> {
    match mode.unwrap_or(AVAIL_DEFINITE) {
        AVAIL_DEFINITE => Ok(AvailableSpace::Definite(Pixels::from(definite as f32))),
        AVAIL_MIN_CONTENT => Ok(AvailableSpace::MinContent),
        AVAIL_MAX_CONTENT => Ok(AvailableSpace::MaxContent),
        other => bail!("unsupported available space mode {other:?}"),
    }
}

// ---------------------------------------------------------------------------
// Measured nodes
// ---------------------------------------------------------------------------

/// Build the deterministic measure function for one measured node. The
/// closure records a `measure-query` event with everything taffy passed
/// (known dimensions, per-axis available space) and a `measure-result`
/// event with the size it returned, in logical pixels. It never snaps:
/// the framework's `snap_measured_size_to_device_pixels`
/// (taffy.rs) applies the clamp-and-ceil, and the trace must show the raw
/// value the fixture returned.
fn measure_fn(
    label: String,
    spec: MeasuredSpec,
    recorder: TraceRecorder,
) -> impl Fn(Size<Option<Pixels>>, Size<AvailableSpace>, &mut Window, &mut App) -> Size<Pixels> + 'static
{
    move |known, available, _window, _cx| {
        let avail_axis = |space: AvailableSpace| -> (String, Option<f32>) {
            match space {
                AvailableSpace::Definite(pixels) => {
                    (AVAIL_DEFINITE.to_string(), Some(f32::from(pixels)))
                }
                AvailableSpace::MinContent => (AVAIL_MIN_CONTENT.to_string(), None),
                AvailableSpace::MaxContent => (AVAIL_MAX_CONTENT.to_string(), None),
            }
        };
        let (avail_width_tag, avail_width) = avail_axis(available.width);
        let (avail_height_tag, avail_height) = avail_axis(available.height);

        let mut fields: Vec<(String, FieldValue)> = vec![
            ("label".into(), FieldValue::str(label.clone())),
            (
                "knownWidthPresent".into(),
                FieldValue::Uint(known.width.is_some() as u64),
            ),
            (
                "knownHeightPresent".into(),
                FieldValue::Uint(known.height.is_some() as u64),
            ),
            ("availWidthTag".into(), FieldValue::str(avail_width_tag)),
            ("availHeightTag".into(), FieldValue::str(avail_height_tag)),
        ];
        if let Some(width) = known.width {
            fields.push(("knownWidth".into(), FieldValue::f32(f32::from(width))));
        }
        if let Some(height) = known.height {
            fields.push(("knownHeight".into(), FieldValue::f32(f32::from(height))));
        }
        if let Some(width) = avail_width {
            fields.push(("availWidth".into(), FieldValue::f32(width)));
        }
        if let Some(height) = avail_height {
            fields.push(("availHeight".into(), FieldValue::f32(height)));
        }
        recorder.event_with("measure-query", Some(&label), fields);

        let measured = compute_measured_size(&spec, known);
        recorder.event_with(
            "measure-result",
            Some(&label),
            [
                ("label".into(), FieldValue::str(label.clone())),
                ("width".into(), FieldValue::f32(f32::from(measured.width))),
                ("height".into(), FieldValue::f32(f32::from(measured.height))),
            ],
        );
        measured
    }
}

/// Compute the measured size for one query per the spec kind. All values
/// are raw logical pixels; no clamping or snapping happens here.
///
/// - `fixed`: the spec size, verbatim.
/// - `echo-known`: the known dimension when present, else the spec size.
/// - `minmax`: the known dimension clamped to `[floor, max]` per axis
///   (a missing max is unbounded), else the floor.
fn compute_measured_size(spec: &MeasuredSpec, known: Size<Option<Pixels>>) -> Size<Pixels> {
    fn minmax_axis(known: Option<Pixels>, floor: f64, max: Option<f64>) -> Pixels {
        match known {
            Some(value) => {
                let value = f32::from(value).max(floor as f32);
                match max {
                    Some(max) => Pixels::from(value.min(max as f32)),
                    None => Pixels::from(value),
                }
            }
            None => Pixels::from(floor as f32),
        }
    }
    match spec.kind.as_str() {
        MEASURED_FIXED => Size {
            width: Pixels::from(spec.width as f32),
            height: Pixels::from(spec.height as f32),
        },
        MEASURED_ECHO_KNOWN => Size {
            width: known
                .width
                .unwrap_or(Pixels::from(spec.width as f32)),
            height: known
                .height
                .unwrap_or(Pixels::from(spec.height as f32)),
        },
        MEASURED_MINMAX => Size {
            width: minmax_axis(known.width, spec.width, spec.max_width),
            height: minmax_axis(known.height, spec.height, spec.max_height),
        },
        // Validated before the window opens; unreachable here.
        other => panic!("unsupported measured spec kind {other:?}"),
    }
}

// ---------------------------------------------------------------------------
// Probe element
// ---------------------------------------------------------------------------

/// A custom element that lays out the case's style tree and records the
/// computed bounds of every labeled node.
pub struct LayoutMetricsProbe {
    pub tree: StyleNode,
    pub measured: BTreeMap<String, MeasuredSpec>,
    pub rem_size: Option<f64>,
    pub recorder: TraceRecorder,
}

fn build_layout(
    flat: &[FlatNode],
    idx: usize,
    measured: &BTreeMap<String, MeasuredSpec>,
    recorder: &TraceRecorder,
    window: &mut Window,
    cx: &mut App,
    out: &mut Vec<(String, Option<gpui::LayoutId>)>,
) -> Result<gpui::LayoutId> {
    let slot = out.len();
    out.push((flat[idx].label.clone(), None));
    let node = &flat[idx];
    let layout_id = if let Some(spec) = measured.get(&node.label) {
        if !node.children.is_empty() {
            bail!(
                "measured node {:?} must not have children: request_measured_layout creates a leaf",
                node.label
            );
        }
        window.request_measured_layout(
            node.style.clone(),
            measure_fn(node.label.clone(), spec.clone(), recorder.clone()),
        )
    } else {
        let child_ids: Vec<gpui::LayoutId> = flat[idx]
            .children
            .iter()
            .map(|&child| build_layout(flat, child, measured, recorder, window, cx, out))
            .collect::<Result<Vec<_>>>()?;
        window.request_layout(node.style.clone(), child_ids, cx)
    };
    out[slot].1 = Some(layout_id);
    Ok(layout_id)
}

impl Element for LayoutMetricsProbe {
    type RequestLayoutState = Vec<(String, gpui::LayoutId)>;
    type PrepaintState = ();

    fn request_layout(
        &mut self,
        _id: Option<&gpui::GlobalElementId>,
        _inspector_id: Option<&gpui::InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (gpui::LayoutId, Self::RequestLayoutState) {
        // The rem override must be active while the tree's layouts are
        // requested: the layout engine resolves rem-based lengths from
        // window.rem_size() at request time (window.rs). Scoping
        // with_rem_size around the draw closure would pop the override
        // before layout_as_root runs, so the probe scopes it around its
        // own tree construction, exactly how the deferred-draw path
        // applies rem overrides around element prepaint.
        let tree = self.tree.clone();
        let measured = self.measured.clone();
        let recorder = self.recorder.clone();
        let rem_override = self.rem_size;
        let state: Vec<(String, gpui::LayoutId)> = window.with_rem_size(rem_override, |window| {
            let mut flat = Vec::new();
            let root = flatten_tree(&tree, &mut flat)
                .expect("style tree parsed during construction");
            let mut laid_out: Vec<(String, Option<gpui::LayoutId>)> = Vec::new();
            build_layout(&flat, root, &measured, &recorder, window, cx, &mut laid_out)
                .expect("layout tree built");
            laid_out
                .into_iter()
                .map(|(label, id)| (label, id.expect("layout id filled")))
                .collect()
        });
        (root_layout_id(state.as_slice()), state)
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
        for (label, layout_id) in request_layout.iter() {
            let bounds = window.layout_bounds(*layout_id);
            self.recorder.event_with(
                "layout-bounds",
                Some(label),
                [
                    ("x".into(), FieldValue::f32_raw_pixels(bounds.origin.x)),
                    ("y".into(), FieldValue::f32_raw_pixels(bounds.origin.y)),
                    ("width".into(), FieldValue::f32_raw_pixels(bounds.size.width)),
                    ("height".into(), FieldValue::f32_raw_pixels(bounds.size.height)),
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

impl gpui::IntoElement for LayoutMetricsProbe {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

fn root_layout_id(state: &[(String, gpui::LayoutId)]) -> gpui::LayoutId {
    state.first().expect("root layout id").1
}

// ---------------------------------------------------------------------------
// Fixture entry point
// ---------------------------------------------------------------------------

/// Validate the case's measurement specs before any window opens so
/// malformed specs fail loudly even if layout never queries the node.
fn validate_case(case: &LayoutMetricsCase) -> Result<()> {
    let mut labels = Vec::new();
    collect_labels(&case.style_tree, &mut labels);
    for (label, spec) in &case.measured {
        match spec.kind.as_str() {
            MEASURED_FIXED | MEASURED_ECHO_KNOWN | MEASURED_MINMAX => {}
            other => bail!("measured spec {label:?} has unsupported kind {other:?}"),
        }
        if !labels.contains(label) {
            bail!("measured spec {label:?} has no node with that label in the style tree");
        }
    }
    Ok(())
}

fn collect_labels(node: &StyleNode, out: &mut Vec<String>) {
    out.push(node.label.clone());
    for child in &node.children {
        collect_labels(child, out);
    }
}

fn run_case(
    cx: &mut TestAppContext,
    case: &LayoutMetricsCase,
    recorder: &TraceRecorder,
) -> Result<()> {
    validate_case(case)?;

    let (_, vcx) = cx.add_window_view(|_, _| gpui::Empty);

    // The scale is part of the fixture semantics: the reference test
    // window's fixed scale factor, read from the actual window.
    let scale = vcx.update(|window, _cx| window.scale_factor());
    let rem_size = case.rem_size.unwrap_or(DEFAULT_REM_SIZE);
    recorder.event_with(
        "case-begin",
        Some(&case.label),
        [
            ("label".into(), FieldValue::str(case.label.clone())),
            ("remSize".into(), FieldValue::f32(rem_size as f32)),
            ("scale".into(), FieldValue::f32(scale)),
        ],
    );

    let available = Size {
        width: axis_available(
            case.available_width_mode.as_deref(),
            case.available_space.width,
        )?,
        height: axis_available(
            case.available_height_mode.as_deref(),
            case.available_space.height,
        )?,
    };
    let probe = LayoutMetricsProbe {
        tree: case.style_tree.clone(),
        measured: case.measured.clone(),
        rem_size: case.rem_size,
        recorder: recorder.clone(),
    };
    vcx.draw(Point::default(), available, |_window, _cx| probe);

    recorder.event("case-end", Some(&case.label));
    Ok(())
}

pub fn run(envelope: &Envelope) -> Vec<crate::trace::TraceEvent> {
    let recorder = TraceRecorder::new();
    let inputs = envelope
        .inputs
        .layout_cases
        .as_ref()
        .unwrap_or_else(|| panic!("fixture kind layout-metrics-v1 requires inputs.layout_cases"));
    let mut cx = TestAppContext::single();
    for case in &inputs.cases {
        run_case(&mut cx, case, &recorder)
            .unwrap_or_else(|e| panic!("layout-metrics case {:?} failed: {e:#}", case.label));
    }
    recorder.into_events()
}
