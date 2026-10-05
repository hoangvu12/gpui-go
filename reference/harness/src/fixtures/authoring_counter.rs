//! Fixture `authoring-counter-v1`: a REAL reference counter app — the
//! typed counter and custom-component authoring path.
//!
//! ## The public oracle boundary
//!
//! The fixture builds a real model entity (`u64` value) shared by two
//! window root views: each window is opened through
//! `TestAppContext::open_window` with a root `Render` view that observes
//! the model and renders a div tree with the count text (the reference
//! div element, the reference text element, the reference window draw
//! path). Increments are simulated through entity updates from the
//! scripted window (never real input — that is the input ticket's
//! slice), and each window is redrawn through the public
//! `AppContext::update_window` + `Window::draw` (the full frame draw).
//!
//! The observable surface, all through public APIs:
//!
//! * `Window::draw` / `AppContext::update_window` — the real frame draw
//!   (request → stretch → compute → prepaint → paint → scene finish).
//! * `Window::rendered_primitive_counts()` — the drawn frame's quad and
//!   glyph sprite counts (public, test-support). NOTE: the reference
//!   test platform's text system (`TestTextSystem`, platform.rs) is a
//!   deterministic stub whose `rasterize_glyph` returns empty bounds, so
//!   text paints NO sprites in a test window: text is observable through
//!   the shaped layout (below) and through the div layout geometry.
//! * `Window::painted_quads()` — the drawn frame's finished quads with
//!   their bounds, masks, colors, radii and orders (public,
//!   test-support). This is the strongest scene record reachable
//!   publicly for a window-drawn frame: the window's `rendered_frame`
//!   scene itself is `pub(crate)`, and the platform render path
//!   (`TestPlatform::with_platform`, `App::new_app`) is not reachable
//!   from this harness crate either (the same boundary the
//!   scene-painting fixture documented).
//! * `VisualTestContext::debug_bounds(selector)` — the div bounds
//!   recorded by `div().debug_selector(...)` (public, test-support).
//! * `Window::text_system()` → `WindowTextSystem::shape_text` — the
//!   public shaping path with the window's own text system, recorded as
//!   the text oracle (width, ascent, descent, visual line count, the
//!   positioned glyphs — the reference stub's glyph ids are the
//!   characters' UTF-16 lengths, so the glyph records carry the text
//!   content).
//! * The fixture's own view and model code record the lifecycle facts:
//!   render invocation counts and observer notification counts (the
//!   reactive-graph observables).
//!
//! ## What each case does
//!
//! 1. One app per case; the model entity is created in window A's
//!    ownership domain and independently retained for window B (the
//!    two-owner shape of the authoring decision round; the reference's
//!    Arc entities need no explicit retention, so the lifetime shape is
//!    exercised through the window closures).
//! 2. Two windows open in case order, each with a root view observing
//!    the model and rendering the counter div tree styled by the case.
//!    Opening a window draws it once (the reference `App::open_window`
//!    draws before returning), so the initial capture reads the drawn
//!    frame without redrawing.
//! 3. The script runs in order: `increment` applies N entity updates
//!    from the given window inside one update (notify coalescing is
//!    observable through the observer counts); the update boundary's
//!    flush then redraws every dirty window — the reference
//!    test-support app's automatic redraw of notified windows (app.rs
//!    flush_effects) — and every open window is captured;
//!    `close-window` removes the window and records the remaining
//!    window count.
//! 4. Every capture records, per open window in opening order:
//!    `render-meta` (the model value and the window's render/notify
//!    counts), `text-shape` + `text-glyph` (the current count text
//!    shaped through the window's text system), `window-drawn` (the
//!    primitive counts), one `painted-quad` per quad and the
//!    `debug-bounds` of the root and label divs.
//!
//! Determinism: everything above is a pure function of the envelope.
//! The reference test window's fixed scale factor (2.0) and default rem
//! size (16) are recorded in `case-begin`. Entity ids and window
//! identities are internal Rust details and are NOT recorded; the
//! observable order is the fixture's own window order.

use crate::envelope::{AuthoringCounterCase, CounterStyleIn, Envelope};
use crate::trace::{FieldValue, TraceRecorder};
use anyhow::{Context as _, Result, bail};
use gpui::{
    AnyWindowHandle, AppContext, Context, DefiniteLength, Entity, Hsla, InteractiveElement,
    IntoElement, ParentElement, Render, Styled, TestAppContext, Window, div, px,
    rgb_to_hsla, rgba,
};
use std::cell::RefCell;
use std::rc::Rc;

/// The count text rendered by the counter view.
fn count_text(value: u64) -> String {
    format!("Count: {value}")
}

/// The counter model: a shared `u64` value.
struct CounterModel {
    value: u64,
}

/// The per-window view counts shared with the fixture's recorder.
#[derive(Clone, Default)]
struct ViewCounts {
    renders: Rc<RefCell<u64>>,
    notified: Rc<RefCell<u64>>,
}

/// The counter root view: one per window, observing the shared model.
struct CounterView {
    model: Entity<CounterModel>,
    counts: ViewCounts,
    style: ResolvedStyle,
}

/// The case's justify-content variant (the named Styled methods the
/// reference exposes).
#[derive(Clone, Copy)]
enum CaseJustify {
    Center,
    Start,
    End,
    SpaceBetween,
}

/// The resolved case style applied to the counter div tree.
#[derive(Clone)]
struct ResolvedStyle {
    padding: DefiniteLength,
    font_size: gpui::AbsoluteLength,
    justify: CaseJustify,
    background: Hsla,
}

impl Render for CounterView {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        *self.counts.renders.borrow_mut() += 1;
        let value = self.model.read(cx).value;
        let root = div()
            .flex()
            .gap_2()
            .p(self.style.padding)
            .bg(self.style.background)
            .size_full()
            .text_size(self.style.font_size);
        let root = match self.style.justify {
            CaseJustify::Center => root.justify_center(),
            CaseJustify::Start => root.justify_start(),
            CaseJustify::End => root.justify_end(),
            CaseJustify::SpaceBetween => root.justify_between(),
        };
        root.id("counter-root")
            .debug_selector(|| "root".to_string())
            .child(
                div()
                    .flex()
                    .id("counter-label")
                    .debug_selector(|| "label".to_string())
                    .child(count_text(value)),
            )
    }
}

// ---------------------------------------------------------------------------
// Input conversion
// ---------------------------------------------------------------------------

fn parse_hsla(hex: &str) -> Result<Hsla> {
    let compact: String = hex.chars().filter(|c| !c.is_whitespace()).collect();
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

fn resolve_style(style: &CounterStyleIn) -> Result<ResolvedStyle> {
    let padding = DefiniteLength::try_from(style.padding.as_str())
        .with_context(|| format!("parsing padding {:?}", style.padding))?;
    let font_size = gpui::AbsoluteLength::try_from(style.font_size.as_str())
        .with_context(|| format!("parsing font size {:?}", style.font_size))?;
    let justify = match style.justify.as_str() {
        "Center" => CaseJustify::Center,
        "Start" => CaseJustify::Start,
        "End" => CaseJustify::End,
        "SpaceBetween" => CaseJustify::SpaceBetween,
        other => bail!("unsupported justify {other:?}: expected Center, Start, End or SpaceBetween"),
    };
    let background = parse_hsla(&style.background)?;
    Ok(ResolvedStyle {
        padding,
        font_size,
        justify,
        background,
    })
}

// ---------------------------------------------------------------------------
// Case execution
// ---------------------------------------------------------------------------

/// One open window of a case: its label, handle and view counts.
#[derive(Clone)]
struct CaseWindow {
    label: String,
    handle: AnyWindowHandle,
    counts: ViewCounts,
}

/// The case state: the app, the model, the open windows and the text
/// style facts derived once (the ambient text style of the label text:
/// the default text style with the case's font size).
struct CaseState {
    windows: Vec<CaseWindow>,
    model: Entity<CounterModel>,
    /// The text style the label text renders with (the root div's
    /// cascaded font size over the default text style).
    text_style: gpui::TextStyle,
}

/// Open one window with the counter root view observing the model.
fn open_counter_window(
    cx: &mut TestAppContext,
    model: Entity<CounterModel>,
    style: ResolvedStyle,
    width: f64,
    height: f64,
    label: &str,
) -> Result<CaseWindow> {
    let counts = ViewCounts::default();
    let view_counts = counts.clone();
    let handle = cx.update(|cx| {
        let bounds = gpui::Bounds {
            origin: gpui::Point::default(),
            size: gpui::Size {
                width: px(width as f32),
                height: px(height as f32),
            },
        };
        cx.open_window(
            gpui::WindowOptions::new()
                .window_bounds(Some(gpui::WindowBounds::Windowed(bounds))),
            move |_window, cx| {
                let model = model.clone();
                let counts = view_counts.clone();
                cx.new(move |cx| {
                    let model_for_observe = model.clone();
                    let counts_for_observe = counts.clone();
                    cx.observe(&model_for_observe, move |_, _, cx| {
                        *counts_for_observe.notified.borrow_mut() += 1;
                        cx.notify();
                    })
                    .detach();
                    CounterView {
                        model,
                        counts,
                        style: style.clone(),
                    }
                })
            },
        )
    })?;
    let handle: AnyWindowHandle = handle.into();
    cx.run_until_parked();
    Ok(CaseWindow {
        label: label.to_string(),
        handle,
        counts,
    })
}

/// Capture one window's current drawn frame and lifecycle facts.
fn capture_window(
    cx: &mut TestAppContext,
    state: &CaseState,
    window: &CaseWindow,
    recorder: &TraceRecorder,
    text: &str,
) -> Result<()> {
    let value = cx.read(|cx| state.model.read(cx).value);
    let renders = *window.counts.renders.borrow();
    let notified = *window.counts.notified.borrow();

    // The text oracle: shape the current count text through this
    // window's text system with the ambient text style of the label.
    let (text_layout, font_size, line_height) = cx
        .update_window(window.handle, |_, window, _cx| {
            let text_style = &state.text_style;
            let rem_size = window.rem_size();
            let font_size = text_style.font_size.to_pixels(rem_size);
            let line_height = window.pixel_snap(
                text_style
                    .line_height
                    .to_pixels(font_size.into(), rem_size),
            );
            let runs = vec![text_style.to_run(text.len())];
            let shaped = window
                .text_system()
                .shape_text(text, font_size, &runs, None, None)
                .expect("shaping the count text through the window text system");
            (shaped, font_size, line_height)
        })
        .context("shaping through the window")?;

    recorder.event_with(
        "text-shape",
        Some(&window.label),
        vec![
            ("textLen".into(), FieldValue::Uint(text.len() as u64)),
            ("fontSize".into(), FieldValue::f32(f32::from(font_size))),
            ("lineHeight".into(), FieldValue::f32(f32::from(line_height))),
            ("width".into(), FieldValue::f32(f32::from(text_layout.width))),
            ("ascent".into(), FieldValue::f32(f32::from(text_layout.ascent))),
            ("descent".into(), FieldValue::f32(f32::from(text_layout.descent))),
            ("lineCount".into(), FieldValue::Uint(text_layout.visual_lines.len() as u64)),
            (
                "glyphCount".into(),
                FieldValue::Uint(
                    text_layout
                        .paint_fragments
                        .iter()
                        .map(|fragment| fragment.glyphs.len())
                        .sum::<usize>() as u64,
                ),
            ),
        ],
    );
    for (index, glyph) in text_layout
        .paint_fragments
        .iter()
        .flat_map(|fragment| fragment.glyphs.iter())
        .enumerate()
    {
        recorder.event_with(
            "text-glyph",
            Some(&window.label),
            vec![
                ("index".into(), FieldValue::Uint(index as u64)),
                ("id".into(), FieldValue::Uint(glyph.id.0 as u64)),
                ("x".into(), FieldValue::f32(f32::from(glyph.position.x))),
                ("y".into(), FieldValue::f32(f32::from(glyph.position.y))),
                ("isEmoji".into(), FieldValue::Uint(u64::from(glyph.is_emoji))),
            ],
        );
    }

    // The primitive counts and the painted quads of the drawn frame.
    let (quads, mono, subpixel, polychrome) = cx
        .update_window(window.handle, |_, window, _cx| {
            window.rendered_primitive_counts()
        })
        .context("reading primitive counts")?;
    recorder.event_with(
        "window-drawn",
        Some(&window.label),
        vec![
            ("value".into(), FieldValue::Uint(value)),
            ("renders".into(), FieldValue::Uint(renders)),
            ("notified".into(), FieldValue::Uint(notified)),
            ("quads".into(), FieldValue::Uint(quads as u64)),
            ("monoSprites".into(), FieldValue::Uint(mono as u64)),
            ("subpixelSprites".into(), FieldValue::Uint(subpixel as u64)),
            ("polychromeSprites".into(), FieldValue::Uint(polychrome as u64)),
        ],
    );
    let painted = cx
        .update_window(window.handle, |_, window, _cx| window.painted_quads())
        .context("reading painted quads")?;
    for quad in &painted {
        let mut fields = vec![
            ("order".into(), FieldValue::Uint(quad.order as u64)),
            ("boundsX".into(), f32_field(quad.bounds.origin.x.0)),
            ("boundsY".into(), f32_field(quad.bounds.origin.y.0)),
            ("boundsW".into(), f32_field(quad.bounds.size.width.0)),
            ("boundsH".into(), f32_field(quad.bounds.size.height.0)),
            ("maskX".into(), f32_field(quad.content_mask.bounds.origin.x.0)),
            ("maskY".into(), f32_field(quad.content_mask.bounds.origin.y.0)),
            ("maskW".into(), f32_field(quad.content_mask.bounds.size.width.0)),
            ("maskH".into(), f32_field(quad.content_mask.bounds.size.height.0)),
        ];
        let background = quad.background.as_solid().unwrap_or_default();
        let border_color = quad.border_color.as_solid().unwrap_or_default();
        fields.extend(color_fields("background", &background));
        fields.extend(color_fields("borderColor", &border_color));
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
        recorder.event_with("painted-quad", Some(&window.label), fields);
    }

    // The layout facts: the debug-selector bounds of the root and label
    // divs (public through the VisualTestContext).
    for selector in ["root", "label"] {
        let mut vcx = gpui::VisualTestContext::from_window(window.handle, cx);
        let bounds = vcx.debug_bounds(selector);
        let mut fields = Vec::new();
        match bounds {
            Some(bounds) => {
                fields.push(("present".into(), FieldValue::Uint(1)));
                fields.push(("x".into(), FieldValue::f32(f32::from(bounds.origin.x))));
                fields.push(("y".into(), FieldValue::f32(f32::from(bounds.origin.y))));
                fields.push(("width".into(), FieldValue::f32(f32::from(bounds.size.width))));
                fields.push(("height".into(), FieldValue::f32(f32::from(bounds.size.height))));
            }
            None => fields.push(("present".into(), FieldValue::Uint(0))),
        }
        recorder.event_with("debug-bounds", Some(selector), fields);
    }
    Ok(())
}

fn f32_field(value: f32) -> FieldValue {
    FieldValue::f32(value)
}

fn color_fields(prefix: &str, color: &gpui::Hsla) -> Vec<(String, FieldValue)> {
    vec![
        (
            format!("{prefix}H"),
            FieldValue::f32(color.hue.into_positive_degrees() / 360.0),
        ),
        (format!("{prefix}S"), FieldValue::f32(color.saturation)),
        (format!("{prefix}L"), FieldValue::f32(color.lightness)),
        (format!("{prefix}A"), FieldValue::f32(color.alpha)),
    ]
}

fn radii_fields(prefix: &str, radii: &gpui::Corners<gpui::ScaledPixels>) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}TopLeft"), f32_field(radii.top_left.0)),
        (format!("{prefix}TopRight"), f32_field(radii.top_right.0)),
        (format!("{prefix}BottomRight"), f32_field(radii.bottom_right.0)),
        (format!("{prefix}BottomLeft"), f32_field(radii.bottom_left.0)),
    ]
}

fn widths_fields(prefix: &str, widths: &gpui::Edges<gpui::ScaledPixels>) -> Vec<(String, FieldValue)> {
    vec![
        (format!("{prefix}Top"), f32_field(widths.top.0)),
        (format!("{prefix}Right"), f32_field(widths.right.0)),
        (format!("{prefix}Bottom"), f32_field(widths.bottom.0)),
        (format!("{prefix}Left"), f32_field(widths.left.0)),
    ]
}

/// Capture every open window (in opening order).
///
/// No explicit draws happen here: the reference test-support application
/// redraws every dirty window at each update boundary (app.rs
/// flush_effects, cfg any(test, feature "test-support")), so the
/// increment update's flush has already redrawn the notified windows —
/// exactly one draw per dirty window. The port fixture mirrors this
/// boundary: it draws every window whose root view was notified since
/// the window's last draw.
fn capture_all(cx: &mut TestAppContext, state: &mut CaseState, recorder: &TraceRecorder) -> Result<()> {
    for window in state.windows.clone() {
        let value = cx.read(|cx| state.model.read(cx).value);
        let text = count_text(value);
        capture_window(cx, state, &window, recorder, &text)?;
    }
    Ok(())
}

fn run_case(cx: &mut TestAppContext, case: &AuthoringCounterCase, recorder: &TraceRecorder) -> Result<()> {
    if case.windows.len() != 2 {
        bail!(
            "authoring-counter case {:?}: expected exactly 2 windows, got {}",
            case.label,
            case.windows.len()
        );
    }
    let style = resolve_style(&case.counter_style)?;

    // The model entity is created first (both windows' views observe it;
    // the reference's Arc entities need no explicit retention).
    let model = cx.update(|cx| {
        cx.new(|_| CounterModel {
            value: case.initial_value,
        })
    });

    // Open the two windows (the model is shared; window A opens first
    // and owns the model's lifetime together with window B). Each window
    // draws once at open.
    let mut windows = Vec::new();
    for window_in in &case.windows {
        let window = open_counter_window(
            cx,
            model.clone(),
            style.clone(),
            window_in.width,
            window_in.height,
            &window_in.label,
        )?;
        windows.push(window);
    }

    // The scale/rem facts are part of the case semantics: the reference
    // test window's fixed scale factor and the default rem size, read
    // from the first opened window.
    let (scale, rem_size) = {
        let mut vcx = gpui::VisualTestContext::from_window(windows[0].handle, cx);
        vcx.update(|window, _| (window.scale_factor(), f32::from(window.rem_size())))
    };
    recorder.event_with(
        "case-begin",
        Some(&case.label),
        vec![
            ("label".into(), FieldValue::str(case.label.clone())),
            ("value".into(), FieldValue::Uint(case.initial_value)),
            ("remSize".into(), FieldValue::f32(rem_size)),
            ("scale".into(), FieldValue::f32(scale)),
            ("windowCount".into(), FieldValue::Uint(0)),
        ],
    );
    recorder.event_with(
        "entity-created",
        Some("model"),
        vec![("value".into(), FieldValue::Uint(case.initial_value))],
    );
    for (index, window) in windows.iter().enumerate() {
        let window_in = &case.windows[index];
        recorder.event_with(
            "window-opened",
            Some(&window.label),
            vec![
                ("width".into(), FieldValue::f32(window_in.width as f32)),
                ("height".into(), FieldValue::f32(window_in.height as f32)),
                ("windowCount".into(), FieldValue::Uint(index as u64 + 1)),
            ],
        );
        recorder.event_with(
            "view-created",
            Some(&window.label),
            vec![
                ("value".into(), FieldValue::Uint(case.initial_value)),
                ("renders".into(), FieldValue::Uint(0)),
            ],
        );
    }

    // The ambient text style of the label text: the default text style
    // with the case's font size (the root div cascades it).
    let mut text_style = gpui::TextStyle::default();
    text_style.font_size = style.font_size;

    let mut state = CaseState {
        windows,
        model,
        text_style,
    };

    // The initial capture: the windows drew at open.
    for window in state.windows.clone() {
        capture_window(cx, &state, &window, recorder, &count_text(case.initial_value))?;
    }

    // The script.
    for op in &case.script {
        match op {
            crate::envelope::CounterScriptOp::Increment { window, clicks } => {
                recorder.event_with(
                    "op-begin",
                    Some(window),
                    vec![
                        ("op".into(), FieldValue::str("increment")),
                        ("clicks".into(), FieldValue::Uint(*clicks as u64)),
                    ],
                );
                let handle = state
                    .windows
                    .iter()
                    .find(|w| &w.label == window)
                    .map(|w| w.handle)
                    .with_context(|| format!("increment references unknown window {window:?}"))?;
                let model = state.model.clone();
                cx.update_window(handle, |_, _window, cx| {
                    for _ in 0..*clicks {
                        model.update(cx, |model, cx| {
                            model.value += 1;
                            cx.notify();
                        });
                    }
                })
                .context("applying the increment")?;
                // The update boundary's flush redrew the notified windows
                // (the test-support automatic redraw); capture them.
                capture_all(cx, &mut state, recorder)?;
            }
            crate::envelope::CounterScriptOp::CloseWindow { window } => {
                recorder.event_with(
                    "op-begin",
                    Some(window),
                    vec![("op".into(), FieldValue::str("close-window"))],
                );
                let index = state
                    .windows
                    .iter()
                    .position(|w| &w.label == window)
                    .with_context(|| format!("close references unknown window {window:?}"))?;
                let handle = state.windows[index].handle;
                cx.update_window(handle, |_, window, _| window.remove_window())
                    .context("closing the window")?;
                state.windows.remove(index);
                let count = cx.windows().len() as u64;
                recorder.event_with(
                    "window-closed",
                    Some(window),
                    vec![("windowCount".into(), FieldValue::Uint(count))],
                );
            }
        }
    }

    recorder.event("case-end", Some(&case.label));
    Ok(())
}

pub fn run(envelope: &Envelope) -> Vec<crate::trace::TraceEvent> {
    let recorder = TraceRecorder::new();
    let inputs = envelope
        .inputs
        .counter_cases
        .as_ref()
        .unwrap_or_else(|| panic!("fixture kind authoring-counter-v1 requires inputs.counter_cases"));
    for case in &inputs.cases {
        let mut cx = TestAppContext::single();
        run_case(&mut cx, case, &recorder)
            .unwrap_or_else(|e| panic!("authoring-counter case {:?} failed: {e:#}", case.label));
    }
    recorder.into_events()
}

