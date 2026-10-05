//! Versioned fixture envelope format shared by the reference harness and the
//! Go conformance runner.
//!
//! Envelope schema: `gpui-go/conformance/envelope@1`.
//!
//! An envelope is a portable, semantic description of one deterministic test
//! case: it contains stable ids, capability links, the reference profile and
//! fixture-specific semantic inputs. It never contains expected outputs; those
//! live in recorded reference traces.

use anyhow::{Context as _, Result, bail};
use serde::Deserialize;
use std::collections::BTreeMap;

pub const ENVELOPE_SCHEMA: &str = "gpui-go/conformance/envelope@1";

#[derive(Deserialize, Debug, Clone)]
pub struct Envelope {
    pub schema: String,
    pub fixture_id: String,
    pub fixture_kind: String,
    pub title: String,
    pub profile: String,
    pub capabilities: Vec<String>,
    /// Environment dependencies the fixture declares (e.g. which platforms it
    /// can run on). Recorded but not interpreted by the reference harness.
    #[serde(default)]
    pub environment: Vec<String>,
    pub inputs: FixtureInputs,
}

#[derive(Deserialize, Debug, Clone)]
pub struct FixtureInputs {
    /// Available space for the root layout, in logical pixels
    /// (layout-effects-v1).
    ///
    /// `#[serde(default)]` lets the layout-metrics-v1 kind omit the
    /// layout-effects inputs (and vice versa); the fixture kinds read only
    /// their own inputs.
    #[serde(default)]
    pub available_space: SizeInput,
    /// Labeled style tree laid out by the layout section
    /// (layout-effects-v1).
    #[serde(default)]
    pub style_tree: StyleNode,
    /// Ordered effect operations executed by the effects section
    /// (layout-effects-v1).
    #[serde(default)]
    pub effect_script: Vec<EffectOp>,
    /// Per-case layout metric inputs (layout-metrics-v1).
    #[serde(default)]
    pub layout_cases: Option<LayoutMetricsInputs>,
    /// Per-case scene painting inputs (scene-painting-v1).
    #[serde(default)]
    pub scene_cases: Option<ScenePaintingInputs>,
    /// Per-case text geometry inputs (text-geometry-v1).
    #[serde(default)]
    pub text_cases: Option<TextGeometryInputs>,
    /// Per-case glyph raster inputs (glyph-raster-v1).
    #[serde(default)]
    pub glyph_cases: Option<GlyphRasterInputs>,
    /// Per-case authoring counter inputs (authoring-counter-v1).
    #[serde(default)]
    pub counter_cases: Option<AuthoringCounterInputs>,
    /// Per-case path/filter paint inputs (paths-filters-v1, ticket16).
    #[serde(default)]
    pub path_filter_cases: Option<PathFilterInputs>,
}

// ---------------------------------------------------------------------------
// authoring-counter-v1 inputs
// ---------------------------------------------------------------------------

/// Per-case inputs of the `authoring-counter-v1` fixture kind.
#[derive(Deserialize, Debug, Clone)]
pub struct AuthoringCounterInputs {
    /// Cases are executed in order, each with its own app context.
    pub cases: Vec<AuthoringCounterCase>,
}

/// One authoring-counter case: a declarative counter description — the
/// initial value, two windows with their sizes, the counter view's
/// style, an increment script (simulated through entity updates, never
/// real input) and per-capture-point observation of the rendered
/// windows.
#[derive(Deserialize, Debug, Clone)]
pub struct AuthoringCounterCase {
    pub label: String,
    /// The counter model's initial value.
    pub initial_value: u64,
    /// The two windows sharing the model, in opening order.
    pub windows: Vec<CounterWindowIn>,
    /// The counter view's style (padding, font size, justify content,
    /// background).
    pub counter_style: CounterStyleIn,
    /// The ordered script of increments and window closures.
    pub script: Vec<CounterScriptOp>,
}

/// One window of an authoring-counter case.
#[derive(Deserialize, Debug, Clone)]
pub struct CounterWindowIn {
    /// The window's label ("A"/"B"): identifies it in the trace and the
    /// script.
    pub label: String,
    /// The window's client width in logical pixels.
    pub width: f64,
    /// The window's client height in logical pixels.
    pub height: f64,
}

/// The counter view's style inputs.
#[derive(Deserialize, Debug, Clone)]
pub struct CounterStyleIn {
    /// The root div's padding (all edges), e.g. "0.75rem".
    pub padding: String,
    /// The cascaded text font size, e.g. "1.25rem".
    pub font_size: String,
    /// The root's justify content, a reference variant name ("Center").
    pub justify: String,
    /// The root's background color, 8 hex digits RRGGBBAA.
    pub background: String,
}

/// One authoring-counter script operation.
#[derive(Deserialize, Debug, Clone)]
#[serde(tag = "op", rename_all = "kebab-case")]
pub enum CounterScriptOp {
    /// Increment the model from the given window, `clicks` times, inside
    /// one update (entity updates + notify; real input arrives with the
    /// input ticket).
    Increment {
        /// The window the simulated clicks originate from.
        window: String,
        /// How many clicks to apply.
        clicks: u32,
    },
    /// Close the given window (its scope and root view retire; the model
    /// survives through the other window's lease).
    CloseWindow {
        /// The window to close.
        window: String,
    },
}

#[derive(Deserialize, Debug, Clone, Default)]
pub struct SizeInput {
    pub width: f64,
    pub height: f64,
}

#[derive(Deserialize, Debug, Clone, Default)]
pub struct StyleNode {
    pub label: String,
    /// CSS-like style values parsed by the reference's own `TryFrom<&str>`
    /// implementations, or Rust enum variant names for non-length properties.
    /// Only a documented subset is supported by each fixture kind;
    /// unsupported keys fail the run instead of being ignored.
    pub style: BTreeMap<String, String>,
    #[serde(default)]
    pub children: Vec<StyleNode>,
}

/// Per-case inputs of the `layout-metrics-v1` fixture kind.
#[derive(Deserialize, Debug, Clone)]
pub struct LayoutMetricsInputs {
    /// Cases are executed in order, each in its own window.
    pub cases: Vec<LayoutMetricsCase>,
}

/// One layout-metrics case: a labeled style tree drawn under its own
/// available space, rem size and optional measurement functions.
#[derive(Deserialize, Debug, Clone)]
pub struct LayoutMetricsCase {
    pub label: String,
    /// Rem size override for this case, in logical pixels. `None` keeps the
    /// reference window default (16px, `Window`'s `rem_size: px(16.)`).
    #[serde(default)]
    pub rem_size: Option<f64>,
    /// Definite available space in logical pixels; the per-axis mode fields
    /// below select how each axis is offered to the layout.
    pub available_space: SizeInput,
    /// How the width axis is offered to the root layout: `"definite"`
    /// (default, uses `available_space.width`), `"min-content"` or
    /// `"max-content"`.
    #[serde(default)]
    pub available_width_mode: Option<String>,
    /// How the height axis is offered to the root layout; same values as
    /// `available_width_mode`, defaulting to `"definite"`.
    #[serde(default)]
    pub available_height_mode: Option<String>,
    /// Labeled style tree laid out as the root element of the case.
    pub style_tree: StyleNode,
    /// Measurement specs keyed by node label. A node whose label appears
    /// here is laid out through `Window::request_measured_layout` with the
    /// spec's deterministic measure function instead of children.
    #[serde(default)]
    pub measured: BTreeMap<String, MeasuredSpec>,
}

/// Deterministic measurement spec for one measured node.
#[derive(Deserialize, Debug, Clone)]
pub struct MeasuredSpec {
    /// `"fixed"`, `"echo-known"` or `"minmax"`.
    pub kind: String,
    /// Fixed size (`fixed`, `echo-known` defaults) or per-axis floor
    /// (`minmax`), in logical pixels.
    pub width: f64,
    pub height: f64,
    /// Per-axis maximum for `minmax`, in logical pixels. `None` is
    /// unbounded.
    #[serde(default)]
    pub max_width: Option<f64>,
    /// Per-axis maximum for `minmax`, in logical pixels. `None` is
    /// unbounded.
    #[serde(default)]
    pub max_height: Option<f64>,
}

#[derive(Deserialize, Debug, Clone)]
#[serde(tag = "op", rename_all = "kebab-case")]
pub enum EffectOp {
    /// Create an entity with a starting counter value.
    CreateEntity { label: String, value: u64 },
    /// Drop the entity handle; release must be observed on the next effect
    /// cycle, before remaining pending effects run.
    DropEntity { label: String },
    /// Register an on-release callback for the entity.
    ObserveRelease { label: String },
    /// Create an observer entity that subscribes to `source`'s `tick` events
    /// and records every delivery.
    Subscribe {
        observer: String,
        source: String,
        event_type: String,
    },
    /// Schedule `count` notifications on the entity within a single outermost
    /// update, so coalescing behavior is observable.
    Notify { entity: String, count: u32 },
    /// Emit typed events from the entity, in the given order, within a single
    /// outermost update, so FIFO delivery is observable.
    Emit { entity: String, values: Vec<u32> },
    /// Schedule a deferred callback; a defer may schedule another defer
    /// (deferred registration activation is observable).
    Defer { label: String, schedule: Option<String> },
    /// Spawn a background task that waits for a virtual-clock delay and then
    /// produces a value, with foreground delivery of the result.
    SpawnTask {
        label: String,
        result: i64,
        delay_ms: u64,
    },
    /// Advance the virtual clock, letting delayed background work complete.
    AdvanceClock { ms: u64 },
    /// Run all foreground and background scheduler work until parked.
    RunTasks,
    /// Run one app update cycle: the outermost update boundary flushes the
    /// pending effect queue (release, notify, events, defers) in order.
    RunEffects,
}


// ---------------------------------------------------------------------------
// scene-painting-v1 inputs
// ---------------------------------------------------------------------------

/// Per-case inputs of the `scene-painting-v1` fixture kind.
#[derive(Deserialize, Debug, Clone)]
pub struct ScenePaintingInputs {
    /// Cases are executed in order; a later case may replay a range of an
    /// earlier case's scene (the source scenes stay pinned for the run).
    pub cases: Vec<ScenePaintingCase>,
}

/// One scene-painting case: a labeled style tree laid out under the
/// case's available space, then a paint script executed against a fresh
/// reference `Scene` through its public API, mirroring the pinned
/// `Window` paint methods' record construction.
#[derive(Deserialize, Debug, Clone)]
pub struct ScenePaintingCase {
    pub label: String,
    /// Definite available space for the root layout, in logical pixels.
    pub available_space: SizeInput,
    /// The labeled style tree providing node bounds for the paint script.
    pub style_tree: StyleNode,
    /// The ordered paint script. Each command references node labels
    /// (bounds) plus explicit paint parameters.
    pub paint_script: Vec<PaintOp>,
}

/// Corner radii in logical pixels (top-left, top-right, bottom-right,
/// bottom-left).
#[derive(Deserialize, Debug, Clone, Copy)]
pub struct CornersIn {
    pub top_left: f64,
    pub top_right: f64,
    pub bottom_right: f64,
    pub bottom_left: f64,
}

/// Edge widths in logical pixels (top, right, bottom, left).
#[derive(Deserialize, Debug, Clone, Copy)]
pub struct EdgesIn {
    pub top: f64,
    pub right: f64,
    pub bottom: f64,
    pub left: f64,
}

/// An explicit bounds/mask rectangle in logical pixels.
#[derive(Deserialize, Debug, Clone, Copy)]
pub struct RectIn {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

/// One paint-script operation. The tags are kebab-case, mirroring the
/// effect-script op encoding. Parameters follow the pinned paint API's
/// shapes: colors are 8-hex-digit `RRGGBBAA` strings (parsed with the
/// reference's own `rgba`/`rgb_to_hsla`), lengths are logical pixels
/// converted with the pinned window helpers, and each command may carry
/// an explicit `mask` (the default mask is the case's available space at
/// the origin).
#[derive(Deserialize, Debug, Clone)]
#[serde(tag = "op", rename_all = "kebab-case")]
pub enum PaintOp {
    /// Paint a styled box through the pinned `paint_quad` path (snapped
    /// bounds, scaled radii, stroke-snapped border widths, opacity into
    /// the colors, and the border-only split into strips).
    PaintBox {
        /// Node label providing the bounds.
        label: String,
        /// Background color (`RRGGBBAA`); default transparent black.
        #[serde(default)]
        background: Option<String>,
        /// Uniform corner radius in logical pixels; default 0.
        #[serde(default)]
        corner_radius: Option<f64>,
        /// Per-corner radii; overrides `corner_radius` when present.
        #[serde(default)]
        corner_radii: Option<CornersIn>,
        /// Uniform border width; default 0.
        #[serde(default)]
        border_width: Option<f64>,
        /// Per-edge border widths; overrides `border_width`.
        #[serde(default)]
        border_widths: Option<EdgesIn>,
        /// Border color; default transparent black.
        #[serde(default)]
        border_color: Option<String>,
        /// `"Solid"` (default) or `"Dashed"`.
        #[serde(default)]
        border_style: Option<String>,
        /// Corner smoothing, 0..1; default 0.
        #[serde(default)]
        corner_smoothing: Option<f64>,
        /// Explicit content mask; default the case mask.
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Paint a box shadow through the pinned `paint_drop_shadows` (or
    /// `paint_inset_shadows` when `inset`) path.
    PaintShadow {
        /// Node label providing the element bounds.
        label: String,
        /// Shadow offset x in logical pixels; default 0.
        #[serde(default)]
        offset_x: Option<f64>,
        /// Shadow offset y in logical pixels; default 0.
        #[serde(default)]
        offset_y: Option<f64>,
        /// Blur radius in logical pixels; default 0.
        #[serde(default)]
        blur_radius: Option<f64>,
        /// Spread radius in logical pixels; default 0.
        #[serde(default)]
        spread_radius: Option<f64>,
        /// Shadow color (`RRGGBBAA`).
        color: String,
        /// Inset shadow (painted inside the element bounds).
        #[serde(default)]
        inset: Option<bool>,
        /// The element's uniform corner radius; default 0.
        #[serde(default)]
        corner_radius: Option<f64>,
        /// The element's per-corner radii; overrides `corner_radius`.
        #[serde(default)]
        corner_radii: Option<CornersIn>,
        /// Corner smoothing, 0..1; default 0.
        #[serde(default)]
        corner_smoothing: Option<f64>,
        /// Explicit content mask; default the case mask.
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Paint an underline through the pinned `paint_underline` path.
    PaintUnderline {
        /// Node label providing the underline origin (its top-left).
        label: String,
        /// Underline width in logical pixels; default the node width.
        #[serde(default)]
        width: Option<f64>,
        /// Stroke thickness in logical pixels.
        thickness: f64,
        /// Wavy underline (height = 3x thickness).
        #[serde(default)]
        wavy: Option<bool>,
        /// Underline color (`RRGGBBAA`).
        color: String,
        /// Explicit content mask; default the case mask.
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Begin a paint layer: the pinned `paint_layer` pushes a scene layer
    /// with cover bounds of the (mask-clipped) bounds when non-empty, and
    /// the pinned `with_element_opacity` multiplies the element opacity
    /// applied to every primitive painted until the matching end.
    BeginLayer {
        /// Node label providing the layer bounds; optional when
        /// `bounds` is explicit.
        #[serde(default)]
        label: Option<String>,
        /// Explicit layer bounds in logical pixels.
        #[serde(default)]
        bounds: Option<RectIn>,
        /// Element opacity applied until `end-layer` (nested layers
        /// multiply, exactly like nested `with_element_opacity`).
        #[serde(default)]
        opacity: Option<f64>,
        /// Explicit content mask for the clip test; default the case mask.
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// End the innermost layer.
    EndLayer,
    /// Raise the deferred-draw order floor (`Scene::raise_order_floor`).
    RaiseFloor,
    /// Begin a content-filter group through the pinned `with_filter_layer`
    /// path: a start boundary marker with the given blur; the matching
    /// `end-filter-group` closes it with the same snapshot.
    BeginFilterGroup {
        /// Node label providing the group bounds.
        label: String,
        /// Gaussian blur radius in logical pixels; 0 or negative is an
        /// identity filter and the group runs inline (no markers).
        blur_radius: f64,
        /// Group corner radius; default 0.
        #[serde(default)]
        corner_radius: Option<f64>,
        /// Per-corner radii; overrides `corner_radius`.
        #[serde(default)]
        corner_radii: Option<CornersIn>,
        /// Corner smoothing, 0..1; default 0.
        #[serde(default)]
        corner_smoothing: Option<f64>,
        /// Explicit content mask; default the case mask.
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// End the innermost content-filter group (same snapshot as its
    /// begin).
    EndFilterGroup,
    /// Paint a backdrop filter through the pinned `paint_backdrop_filter`
    /// path (the frosted-glass effect; the element opacity is captured at
    /// paint time).
    PaintBackdrop {
        /// Node label providing the filter bounds.
        label: String,
        /// Gaussian blur radius in logical pixels; 0 or negative is an
        /// identity filter and paints nothing.
        blur_radius: f64,
        /// Corner radius; default 0.
        #[serde(default)]
        corner_radius: Option<f64>,
        /// Per-corner radii; overrides `corner_radius`.
        #[serde(default)]
        corner_radii: Option<CornersIn>,
        /// Corner smoothing, 0..1; default 0.
        #[serde(default)]
        corner_smoothing: Option<f64>,
        /// Explicit content mask; default the case mask.
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Replay a range of a previous case's paint operations into this
    /// scene (`Scene::replay`; orders are recomputed against THIS scene's
    /// bounds tree and layer state).
    Replay {
        /// The source case's label (an earlier case in this fixture).
        source: String,
        /// Start of the operation range (inclusive).
        start: u32,
        /// End of the operation range (exclusive).
        end: u32,
    },
}

// ---------------------------------------------------------------------------
// text-geometry-v1 inputs
// ---------------------------------------------------------------------------

/// Per-case inputs of the `text-geometry-v1` fixture kind.
#[derive(Deserialize, Debug, Clone)]
pub struct TextGeometryInputs {
    /// Cases are executed in order against one process-global text system.
    pub cases: Vec<TextGeometryCase>,
}

/// One text-geometry case: a font descriptor, text and font size shaped
/// through the reference's public shaping API, then queried for line, run,
/// cluster, caret, hit-test and selection geometry.
#[derive(Deserialize, Debug, Clone)]
pub struct TextGeometryCase {
    pub label: String,
    /// Base font descriptor resolved for this case; run descriptors inherit
    /// every field this descriptor carries.
    pub font: FontDescriptorIn,
    /// UTF-8 source text. Byte indices everywhere in this fixture are UTF-8
    /// byte offsets (the pin's layout coordinate system).
    pub text: String,
    /// Font size in logical pixels, shared by all runs (the pin's
    /// `TextLayoutRequest::font_size`).
    pub font_size: f64,
    /// Soft-wrap width in logical pixels. `None` disables wrapping.
    #[serde(default)]
    pub wrap_width: Option<f64>,
    /// Maximum number of visual rows (`TextLayoutRequest::line_clamp`).
    #[serde(default)]
    pub line_clamp: Option<usize>,
    /// Line height in logical pixels used for every caret/hit-test/selection
    /// query and the derived row placement. `None` keeps the reference text
    /// style default (`phi()` — the golden ratio — times the font size;
    /// `TextStyle::default().line_height`, style.rs).
    #[serde(default)]
    pub line_height: Option<f64>,
    /// Style runs. Empty means one run covering the whole text with the
    /// case's font. When present, the runs must cover the text exactly and
    /// end on UTF-8 character boundaries (the pin's validated input).
    #[serde(default)]
    pub runs: Vec<TextRunIn>,
    /// Caret query mode: `"explicit"` (default) uses `caret_queries`,
    /// `"all-clusters"` queries every cluster boundary with downstream
    /// affinity, and `"all-clusters-both"` queries every boundary with both
    /// affinities (so wrap-boundary affinity behavior is observable).
    #[serde(default)]
    pub caret_mode: Option<String>,
    /// Explicit caret queries: UTF-8 byte offsets with an affinity
    /// (`"downstream"` default, `"upstream"`).
    #[serde(default)]
    pub caret_queries: Vec<CaretQueryIn>,
    /// Hit-test points in logical pixels.
    #[serde(default)]
    pub hit_points: Vec<PointIn>,
    /// Selection ranges (UTF-8 byte offsets).
    #[serde(default)]
    pub selections: Vec<ByteRangeIn>,
}

/// A font descriptor: the public `gpui::Font` input shape. There is no
/// stretch axis in the pinned descriptor (API discovery recorded with the
/// fixture).
#[derive(Deserialize, Debug, Clone)]
pub struct FontDescriptorIn {
    /// Family name. The special name `.SystemUIFont` identifies the system
    /// UI font.
    pub family: String,
    /// Font weight, 100..=900. `None` keeps `FontWeight::default()` (400).
    #[serde(default)]
    pub weight: Option<f64>,
    /// `"Normal"` (default), `"Italic"` or `"Oblique"`.
    #[serde(default)]
    pub style: Option<String>,
    /// OpenType features (4 printable ASCII tag characters plus a value).
    #[serde(default)]
    pub features: Vec<FeatureIn>,
    /// Additional fallback family names tried after the main family.
    #[serde(default)]
    pub fallbacks: Vec<String>,
}

/// One OpenType feature setting of a font descriptor.
#[derive(Deserialize, Debug, Clone)]
pub struct FeatureIn {
    /// Four-character feature tag (e.g. `"calt"`).
    pub tag: String,
    /// Feature value; 0 disables the feature and 1 enables it.
    pub value: u32,
}

/// One style run: a byte length plus optional font and letter-spacing
/// overrides over the case's descriptor.
#[derive(Deserialize, Debug, Clone)]
pub struct TextRunIn {
    /// Number of UTF-8 bytes this run styles.
    pub len: usize,
    /// Font overrides merged over the case's descriptor; absent fields
    /// inherit the case's.
    #[serde(default)]
    pub font: Option<FontOverrideIn>,
    /// Letter spacing applied between glyphs of this run, in logical pixels
    /// (`TextRun::letter_spacing`).
    #[serde(default)]
    pub letter_spacing: Option<f64>,
}

/// Per-field font overrides of one style run.
#[derive(Deserialize, Debug, Clone, Default)]
pub struct FontOverrideIn {
    #[serde(default)]
    pub family: Option<String>,
    #[serde(default)]
    pub weight: Option<f64>,
    #[serde(default)]
    pub style: Option<String>,
    #[serde(default)]
    pub features: Option<Vec<FeatureIn>>,
    #[serde(default)]
    pub fallbacks: Option<Vec<String>>,
}

/// One caret query: a UTF-8 byte offset plus affinity.
#[derive(Deserialize, Debug, Clone)]
pub struct CaretQueryIn {
    pub index: usize,
    /// `"downstream"` (default) or `"upstream"`.
    #[serde(default)]
    pub affinity: Option<String>,
}

/// A point input in logical pixels.
#[derive(Deserialize, Debug, Clone, Copy)]
pub struct PointIn {
    pub x: f64,
    pub y: f64,
}

/// Per-case inputs of the `glyph-raster-v1` fixture kind.
#[derive(Deserialize, Debug, Clone)]
pub struct GlyphRasterInputs {
    /// Cases are executed in order against one process-global reference
    /// text system constructed with the ported DirectWrite rasterizer.
    pub cases: Vec<GlyphRasterCase>,
}

/// One glyph-raster case: a font, characters, sizes, scale factors,
/// subpixel variants and a render mode rasterized through the pinned
/// public raster API.
#[derive(Deserialize, Debug, Clone)]
pub struct GlyphRasterCase {
    pub label: String,
    /// Font descriptor resolved for this case.
    pub font: FontDescriptorIn,
    /// Characters mapped to glyphs before rasterization. A character
    /// with no glyph in the resolved face records the pinned `None`
    /// outcome (the missing-glyph case).
    pub chars: Vec<String>,
    /// Font sizes in logical pixels.
    pub sizes: Vec<f64>,
    /// Scale factors (the `RenderGlyphParams::scale_factor`).
    pub scales: Vec<f64>,
    /// Subpixel variants as [x, y] pairs.
    pub subpixel_variants: Vec<[u64; 2]>,
    /// Render mode: `"grayscale"`, `"subpixel"` or `"color"`.
    pub mode: String,
    /// Scene color as [red, green, blue, alpha] in 0..1 (the
    /// `RasterStyleRequest::scene_color`; required for color mode).
    #[serde(default)]
    pub scene_color: Option<[f64; 4]>,
}

/// A UTF-8 byte range input.
#[derive(Deserialize, Debug, Clone, Copy)]
pub struct ByteRangeIn {
    pub start: usize,
    pub end: usize,
}

impl Envelope {
    pub fn load(path: &std::path::Path) -> Result<Self> {
        let raw = std::fs::read_to_string(path)
            .with_context(|| format!("reading envelope {}", path.display()))?;
        let env: Envelope =
            serde_json::from_str(&raw).with_context(|| format!("parsing {}", path.display()))?;
        if env.schema != ENVELOPE_SCHEMA {
            bail!(
                "unsupported envelope schema {} (expected {})",
                env.schema,
                ENVELOPE_SCHEMA
            );
        }
        Ok(env)
    }
}

// ---------------------------------------------------------------------------
// paths-filters-v1 inputs (ticket16)
// ---------------------------------------------------------------------------

/// Per-case inputs of the `paths-filters-v1` fixture kind: path builder
/// scripts, surface primitives, sprites and nested filter groups beyond
/// depth two, painted through the pinned `Scene`/`PathBuilder` public
/// APIs.
#[derive(Deserialize, Debug, Clone)]
pub struct PathFilterInputs {
    pub cases: Vec<PathFilterCase>,
}

/// One paths-filters case: a labeled style tree laid out under the
/// case's available space (node bounds for boxes and filter groups),
/// then a paint script against a fresh reference `Scene`.
#[derive(Deserialize, Debug, Clone)]
pub struct PathFilterCase {
    pub label: String,
    /// Definite available space for the root layout, in logical pixels.
    pub available_space: SizeInput,
    /// The labeled style tree providing node bounds for the paint script.
    pub style_tree: StyleNode,
    /// The ordered paint script.
    pub paint_script: Vec<PathFilterOp>,
}

/// One atlas tile reference of a sprite op (the pinned `AtlasTile`).
#[derive(Deserialize, Debug, Clone, Copy)]
pub struct TileIn {
    pub texture_index: u32,
    /// 0 monochrome, 1 polychrome, 2 subpixel.
    pub texture_kind: u32,
    pub tile_id: u32,
    pub padding: u32,
    /// The tile bounds inside the atlas texture, device pixels.
    pub bounds: RectIn,
}

/// One path builder command (the pinned `PathBuilder` public API's
/// shapes; coordinates in logical pixels).
#[derive(Deserialize, Debug, Clone)]
#[serde(tag = "kind", rename_all = "kebab-case")]
pub enum PathCommandIn {
    MoveTo { x: f64, y: f64 },
    LineTo { x: f64, y: f64 },
    /// The pinned quadratic `curve_to(to, ctrl)`.
    CurveTo { to_x: f64, to_y: f64, ctrl_x: f64, ctrl_y: f64 },
    CubicTo { to_x: f64, to_y: f64, a_x: f64, a_y: f64, b_x: f64, b_y: f64 },
    ArcTo {
        radius_x: f64,
        radius_y: f64,
        /// Rotation in degrees.
        x_rotation: f64,
        large_arc: bool,
        sweep: bool,
        x: f64,
        y: f64,
    },
    Polygon { points: Vec<(f64, f64)>, closed: bool },
    Close,
    /// The builder style (the pinned `with_style`). `style` is
    /// `"fill"` or `"stroke"`.
    Style {
        style: String,
        /// Fill: the flattening tolerance; default lyon 0.1.
        #[serde(default)]
        tolerance: Option<f64>,
        /// Fill: `"even-odd"` (default) or `"non-zero"`.
        #[serde(default)]
        fill_rule: Option<String>,
        /// Fill: `"vertical"` (default) or `"horizontal"`.
        #[serde(default)]
        sweep_orientation: Option<String>,
        /// Fill: handle self-intersections (default true).
        #[serde(default)]
        handle_intersections: Option<bool>,
        /// Stroke: the line width.
        #[serde(default)]
        width: Option<f64>,
        /// Stroke: `"butt"` (default), `"square"` or `"round"`.
        #[serde(default)]
        start_cap: Option<String>,
        /// Stroke: like `start_cap`.
        #[serde(default)]
        end_cap: Option<String>,
        /// Stroke: `"miter"` (default), `"round"` or `"bevel"`.
        #[serde(default)]
        line_join: Option<String>,
        /// Stroke: the miter limit; default 4.
        #[serde(default)]
        miter_limit: Option<f64>,
    },
    /// The stroke dash array (the pinned `dash_array`).
    Dash { lengths: Vec<f64> },
    Translate { x: f64, y: f64 },
    Scale { factor: f64 },
    /// Rotation in degrees.
    Rotate { degrees: f64 },
}

/// One paths-filters paint-script operation. The subset of
/// scene-painting ops relevant to the composite (boxes, backdrops,
/// filter groups, floors, replay) plus the new path, sprite and surface
/// ops.
#[derive(Deserialize, Debug, Clone)]
#[serde(tag = "op", rename_all = "kebab-case")]
pub enum PathFilterOp {
    /// Paint a filled box (the pinned `paint_quad` path, background
    /// only in this fixture family).
    PaintBox {
        label: String,
        #[serde(default)]
        background: Option<String>,
        #[serde(default)]
        corner_radius: Option<f64>,
        #[serde(default)]
        corner_radii: Option<CornersIn>,
        #[serde(default)]
        corner_smoothing: Option<f64>,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Paint a tessellated path (the pinned `paint_path` record
    /// construction: content mask, opacity-into-color, scale).
    PaintPath {
        commands: Vec<PathCommandIn>,
        color: String,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Insert a monochrome sprite (the pinned `MonochromeSprite`).
    PaintMonochromeSprite {
        label: String,
        color: String,
        tile: TileIn,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Insert a subpixel sprite (the pinned `SubpixelSprite`).
    PaintSubpixelSprite {
        label: String,
        color: String,
        tile: TileIn,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Insert a polychrome sprite (the pinned `PolychromeSprite`).
    PaintPolychromeSprite {
        label: String,
        #[serde(default)]
        opacity: Option<f64>,
        #[serde(default)]
        corner_radius: Option<f64>,
        #[serde(default)]
        corner_smoothing: Option<f64>,
        tile: TileIn,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Insert a surface primitive (the public `insert_primitive`
    /// path: the pinned `SurfaceSource::Unsupported` stand-in, paired
    /// opacity 1.0).
    PaintSurface {
        label: String,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Paint a backdrop filter (the pinned record construction).
    PaintBackdrop {
        label: String,
        blur_radius: f64,
        #[serde(default)]
        corner_radius: Option<f64>,
        #[serde(default)]
        corner_smoothing: Option<f64>,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    /// Begin a content-filter group (the pinned `with_filter_layer`
    /// boundary snapshot; identity filters run inline). Bounds come
    /// from a layout label or an explicit rectangle (overlapping
    /// rectangles make nested groups interleave their orders, like the
    /// pinned nested-group test).
    BeginFilterGroup {
        #[serde(default)]
        label: Option<String>,
        #[serde(default)]
        bounds: Option<RectIn>,
        blur_radius: f64,
        #[serde(default)]
        corner_radius: Option<f64>,
        #[serde(default)]
        corner_smoothing: Option<f64>,
        #[serde(default)]
        mask: Option<RectIn>,
    },
    EndFilterGroup,
    /// Raise the deferred-draw order floor.
    RaiseFloor,
    /// Replay a range of a previous case's scene.
    Replay { source: String, start: u32, end: u32 },
}
