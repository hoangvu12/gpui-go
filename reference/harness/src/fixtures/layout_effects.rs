//! Fixture `layout-effects-v1`: a small deterministic layout and effect case.
//!
//! Observable surface exercised (all through public GPUI APIs):
//! - Entity creation, typed event subscription, notification coalescing,
//!   FIFO event delivery, deferred callbacks (including a defer scheduled
//!   from inside a running defer), entity release observation, background
//!   task completion on the virtual clock with foreground result delivery.
//! - A labeled style tree laid out through `Window::request_layout` /
//!   `Window::layout_bounds` with every node's computed bounds recorded as
//!   exact f32 bit patterns.
//!
//! The fixture records what the reference actually does; the Go port must
//! reproduce the trace exactly (see docs/conformance-contract.md).

use crate::envelope::{EffectOp, Envelope, StyleNode};
use crate::trace::{FieldValue, TraceRecorder};
use anyhow::{Context as _, Result, bail};
use gpui::{
    AlignContent, AlignItems, App, AppContext, AsyncApp, AvailableSpace, DefiniteLength, Edges,
    Element, Entity, EventEmitter, FlexDirection, Length, Pixels, Point, Size, TestAppContext,
    Window,
};
use std::collections::BTreeMap;
use std::rc::Rc;
use std::time::Duration;

/// Typed event payload emitted by `Counter`.
pub struct TickEvent {
    pub value: u32,
}

/// Entity used by the effects section.
pub struct Counter {
    pub value: u64,
}

impl EventEmitter<TickEvent> for Counter {}

/// Entity that subscribes to `Counter` events and observations.
pub struct Observer {
    pub received_events: u32,
    pub notified: u32,
}

// ---------------------------------------------------------------------------
// Effects section
// ---------------------------------------------------------------------------

struct EffectFixture {
    counters: BTreeMap<String, Entity<Counter>>,
    observers: BTreeMap<String, Entity<Observer>>,
}

impl EffectFixture {
    fn run_op(
        &mut self,
        cx: &mut TestAppContext,
        op: &EffectOp,
        recorder: &TraceRecorder,
    ) -> Result<()> {
        match op {
            EffectOp::CreateEntity { label, value } => {
                let entity = cx.new(|_| Counter { value: *value });
                self.counters.insert(label.clone(), entity);
                recorder.event_with(
                    "entity-created",
                    Some(label),
                    [("value".into(), FieldValue::Uint(*value))],
                );
            }
            EffectOp::ObserveRelease { label } => {
                let entity = self
                    .counters
                    .get(label)
                    .with_context(|| format!("unknown entity {label}"))?
                    .clone();
                let rec = recorder.clone();
                let label_owned = label.clone();
                entity.update(cx, |_, cx| {
                    cx.on_release(move |counter, _| {
                        rec.event_with(
                            "entity-released",
                            Some(&label_owned),
                            [("value".into(), FieldValue::Uint(counter.value))],
                        );
                    })
                    .detach();
                });
                recorder.event("release-observed", Some(label));
            }
            EffectOp::DropEntity { label } => {
                let entity = self
                    .counters
                    .remove(label)
                    .with_context(|| format!("unknown entity {label} to drop"))?;
                recorder.event("entity-dropped", Some(label));
                drop(entity);
            }
            EffectOp::Subscribe {
                observer,
                source,
                event_type,
            } => {
                if event_type != "tick" {
                    bail!("fixture supports event_type \"tick\", got {event_type}");
                }
                let counter = self
                    .counters
                    .get(source)
                    .with_context(|| format!("unknown source entity {source}"))?
                    .clone();
                let rec = recorder.clone();
                let rec2 = recorder.clone();
                let observer_label = observer.clone();
                let observer_label2 = observer.clone();
                let observer_entity = cx.new(|cx| {
                    cx.subscribe(&counter, move |obs: &mut Observer, _source, event: &TickEvent, _cx| {
                        obs.received_events += 1;
                        rec.event_with(
                            "event-delivered",
                            Some(&observer_label),
                            [
                                ("value".into(), FieldValue::Uint(event.value as u64)),
                                (
                                    "receivedEvents".into(),
                                    FieldValue::Uint(obs.received_events as u64),
                                ),
                            ],
                        );
                    })
                    .detach();
                    cx.observe(&counter, move |obs: &mut Observer, _entity, _cx| {
                        obs.notified += 1;
                        rec2.event_with(
                            "observer-notified",
                            Some(&observer_label2),
                            [
                                (
                                    "notified".into(),
                                    FieldValue::Uint(obs.notified as u64),
                                ),
                                (
                                    "receivedEvents".into(),
                                    FieldValue::Uint(obs.received_events as u64),
                                ),
                            ],
                        );
                    })
                    .detach();
                    Observer {
                        received_events: 0,
                        notified: 0,
                    }
                });
                self.observers.insert(observer.clone(), observer_entity);
                recorder.event_with(
                    "subscription-registered",
                    Some(observer),
                    [("source".into(), FieldValue::str(source.clone()))],
                );
            }
            EffectOp::Notify { entity, count } => {
                let entity_handle = self
                    .counters
                    .get(entity)
                    .with_context(|| format!("unknown entity {entity}"))?
                    .clone();
                let rec = recorder.clone();
                let entity_label = entity.clone();
                // All notify calls happen within one outermost update so
                // coalescing behavior is observable.
                cx.update(|app: &mut App| {
                    for _ in 0..*count {
                        entity_handle.update(app, |counter, cx| {
                            counter.value += 1;
                            cx.notify();
                        });
                    }
                    rec.event_with(
                        "update-returned",
                        Some(&entity_label),
                        [
                            ("op".into(), FieldValue::str("notify")),
                            ("count".into(), FieldValue::Uint(*count as u64)),
                        ],
                    );
                });
            }
            EffectOp::Emit { entity, values } => {
                let entity_handle = self
                    .counters
                    .get(entity)
                    .with_context(|| format!("unknown entity {entity}"))?
                    .clone();
                let rec = recorder.clone();
                let entity_label = entity.clone();
                // All emits happen within one outermost update, in the given
                // order, so FIFO delivery is observable.
                cx.update(|app: &mut App| {
                    for value in values {
                        entity_handle.update(app, |_, cx| {
                            cx.emit(TickEvent { value: *value });
                        });
                    }
                    rec.event_with(
                        "update-returned",
                        Some(&entity_label),
                        [
                            ("op".into(), FieldValue::str("emit")),
                            ("count".into(), FieldValue::Uint(values.len() as u64)),
                        ],
                    );
                });
            }
            EffectOp::Defer { label, schedule } => {
                let rec = recorder.clone();
                let defer_label = label.clone();
                let schedule_label = schedule.clone();
                cx.update(|app: &mut App| {
                    app.defer(move |app: &mut App| {
                        rec.event("defer-ran", Some(&defer_label));
                        if let Some(next) = schedule_label {
                            let rec = rec.clone();
                            let next = next.clone();
                            app.defer(move |_app| {
                                rec.event("defer-ran", Some(&next));
                            });
                        }
                    });
                });
            }
            EffectOp::SpawnTask {
                label,
                result,
                delay_ms,
            } => {
                let rec = recorder.clone();
                let task_label = label.clone();
                let executor = cx.executor();
                let delay = Duration::from_millis(*delay_ms);
                let result_value = *result;
                let delay_ms_value = *delay_ms;
                let task = cx.background_spawn(async move {
                    rec.event_with(
                        "task-started",
                        Some(&task_label),
                        [
                            ("delayMs".into(), FieldValue::Uint(delay_ms_value)),
                            ("result".into(), FieldValue::Int(result_value)),
                        ],
                    );
                    executor.timer(delay).await;
                    rec.event_with(
                        "task-completed",
                        Some(&task_label),
                        [("result".into(), FieldValue::Int(result_value))],
                    );
                    result_value
                });
                let rec_delivery = recorder.clone();
                let delivery_label = label.clone();
                cx.update(|app: &mut App| {
                    app.spawn(async move |cx: &mut AsyncApp| {
                        let result = task.await;
                        cx.update(|_cx: &mut App| {
                            rec_delivery.event_with(
                                "task-result-observed",
                                Some(&delivery_label),
                                [("result".into(), FieldValue::Int(result))],
                            );
                        });
                    })
                    .detach();
                });
            }
            EffectOp::AdvanceClock { ms } => {
                cx.executor().advance_clock(Duration::from_millis(*ms));
                recorder.event_with(
                    "clock-advanced",
                    None,
                    [("ms".into(), FieldValue::Uint(*ms as u64))],
                );
            }
            EffectOp::RunTasks => {
                cx.run_until_parked();
                recorder.event("tasks-run", None);
            }
            EffectOp::RunEffects => {
                cx.update(|_app: &mut App| {});
                recorder.event("effects-run", None);
            }
        }
        Ok(())
    }
}

// ---------------------------------------------------------------------------
// Layout section
// ---------------------------------------------------------------------------

/// A custom element that lays out the envelope's style tree and records the
/// computed bounds of every labeled node.
pub struct StyleTreeProbe {
    pub tree: StyleNode,
    pub recorder: TraceRecorder,
}

struct FlatNode {
    label: String,
    style: gpui::Style,
    children: Vec<usize>,
}

fn flatten_tree(node: &StyleNode, out: &mut Vec<FlatNode>) -> Result<usize> {
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

/// Build a gpui `Style` from the fixture's documented subset. Unknown keys
/// fail the run instead of being ignored.
fn build_style(map: &BTreeMap<String, String>) -> Result<gpui::Style> {
    let mut style = gpui::Style::default();
    for (key, value) in map {
        match key.as_str() {
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
            other => bail!("unsupported style key {other:?} in layout-effects fixture"),
        }
    }
    Ok(style)
}

fn build_layout(
    flat: &[FlatNode],
    idx: usize,
    window: &mut Window,
    cx: &mut App,
    out: &mut Vec<(String, Option<gpui::LayoutId>)>,
) -> gpui::LayoutId {
    let slot = out.len();
    out.push((flat[idx].label.clone(), None));
    let child_ids: Vec<gpui::LayoutId> = flat[idx]
        .children
        .iter()
        .map(|&child| build_layout(flat, child, window, cx, out))
        .collect();
    let layout_id = window.request_layout(flat[idx].style.clone(), child_ids, cx);
    out[slot].1 = Some(layout_id);
    layout_id
}

impl Element for StyleTreeProbe {
    type RequestLayoutState = Vec<(String, gpui::LayoutId)>;
    type PrepaintState = ();

    fn request_layout(
        &mut self,
        _id: Option<&gpui::GlobalElementId>,
        _inspector_id: Option<&gpui::InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (gpui::LayoutId, Self::RequestLayoutState) {
        let mut flat = Vec::new();
        let root = flatten_tree(&self.tree, &mut flat)
            .expect("style tree parsed during construction");
        let mut laid_out: Vec<(String, Option<gpui::LayoutId>)> = Vec::new();
        build_layout(&flat, root, window, cx, &mut laid_out);
        self.recorder
            .event("layout-requested", Some(&flat[root].label));
        let state: Vec<(String, gpui::LayoutId)> = laid_out
            .into_iter()
            .map(|(label, id)| (label, id.expect("layout id filled")))
            .collect();
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

impl gpui::IntoElement for StyleTreeProbe {
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

pub fn run(envelope: &Envelope) -> Vec<crate::trace::TraceEvent> {
    let recorder = TraceRecorder::new();
    let mut cx = TestAppContext::single();

    // Effects section: run the script op by op. Op boundaries are recorded so
    // deliveries can be attributed to their triggering update.
    let mut fixture = EffectFixture {
        counters: BTreeMap::new(),
        observers: BTreeMap::new(),
    };
    for op in &envelope.inputs.effect_script {
        recorder.event_with(
            "op-begin",
            None,
            [("op".into(), FieldValue::str(op_name(op)))],
        );
        fixture
            .run_op(&mut cx, op, &recorder)
            .unwrap_or_else(|e| panic!("fixture op failed: {e:#}"));
        recorder.event("op-end", None);
    }

    // Layout section: open a window and lay the style tree out as the root
    // element under the envelope's definite available space.
    let window_size = &envelope.inputs.available_space;
    let probe = StyleTreeProbe {
        tree: envelope.inputs.style_tree.clone(),
        recorder: recorder.clone(),
    };
    let (_, mut vcx) = cx.add_window_view(|_, _| gpui::Empty);
    vcx.draw(
        Point::default(),
        Size {
            width: AvailableSpace::Definite(Pixels::from(window_size.width as f32)),
            height: AvailableSpace::Definite(Pixels::from(window_size.height as f32)),
        },
        |_window, _cx| probe,
    );

    recorder.into_events()
}

fn op_name(op: &EffectOp) -> &'static str {
    match op {
        EffectOp::CreateEntity { .. } => "create-entity",
        EffectOp::DropEntity { .. } => "drop-entity",
        EffectOp::ObserveRelease { .. } => "observe-release",
        EffectOp::Subscribe { .. } => "subscribe",
        EffectOp::Notify { .. } => "notify",
        EffectOp::Emit { .. } => "emit",
        EffectOp::Defer { .. } => "defer",
        EffectOp::SpawnTask { .. } => "spawn-task",
        EffectOp::AdvanceClock { .. } => "advance-clock",
        EffectOp::RunTasks => "run-tasks",
        EffectOp::RunEffects => "run-effects",
    }
}
