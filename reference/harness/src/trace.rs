//! Versioned conformance trace format shared by the reference harness and the
//! Go conformance runner.
//!
//! Trace schema: `gpui-go/conformance/trace@1`.
//!
//! Determinism rules:
//! - Events are recorded in the exact order they occur and numbered by `seq`.
//! - `fields` uses a sorted map so JSON key order is stable.
//! - All IEEE-754 `f32` values that participate in comparisons are encoded as
//!   exact bit patterns: `"f32:XXXXXXXX"` with eight uppercase hex digits
//!   (big-endian bit order, i.e. `value.to_bits()` formatted `{:08X}`).
//! - No timestamps, addresses, allocation ids or thread ids appear in traces.
//!   Run metadata (toolchain, environment, timestamps) is recorded by the
//!   runner in a separate `run-meta` file, never inside the trace itself.

use serde::Serialize;
use std::collections::BTreeMap;

pub const TRACE_SCHEMA: &str = "gpui-go/conformance/trace@1";

/// The pinned GPUI-CE source this harness was built against.
pub const GPUI_CE_COMMIT: &str = "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a";
/// Version of the gpui-ce crate at the pin.
pub const GPUI_CE_CRATE: &str = "gpui-ce 0.2.2";
/// Reference profile used by this harness run (see docs/conformance-inventory.md).
pub const HARNESS_PROFILE: &str = "test";

#[derive(Serialize, Debug, Clone, PartialEq)]
pub struct Trace {
    pub schema: String,
    pub fixture_id: String,
    pub fixture_kind: String,
    pub envelope_sha256: String,
    pub harness: HarnessInfo,
    pub events: Vec<TraceEvent>,
}

#[derive(Serialize, Debug, Clone, PartialEq)]
pub struct HarnessInfo {
    pub harness_version: String,
    pub harness_crate: String,
    pub gpui_crate: String,
    pub gpui_commit: String,
    pub profile: String,
    pub features: Vec<String>,
}

#[derive(Serialize, Debug, Clone, PartialEq)]
pub struct TraceEvent {
    pub seq: u64,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub label: Option<String>,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub fields: BTreeMap<String, FieldValue>,
}

/// A single field value. f32 values that participate in exact comparisons are
/// pre-rendered into `FieldValue::F32` (`"f32:XXXXXXXX"`).
#[derive(Serialize, Debug, Clone, PartialEq)]
#[serde(untagged)]
pub enum FieldValue {
    F32(String),
    Int(i64),
    Uint(u64),
    Str(String),
    Bool(bool),
}

/// Format an f32 as the exact bit-pattern string used by the trace schema.
pub fn f32_bits(value: f32) -> String {
    format!("f32:{:08X}", value.to_bits())
}

impl FieldValue {
    pub fn f32(value: f32) -> Self {
        FieldValue::F32(f32_bits(value))
    }
    pub fn f32_raw_pixels(value: gpui::Pixels) -> Self {
        FieldValue::F32(f32_bits(f32::from(value)))
    }
    pub fn str(value: impl Into<String>) -> Self {
        FieldValue::Str(value.into())
    }
}

/// Thread-safe ordered event recorder. Background tasks record events
/// concurrently with foreground code, so events are appended under a lock and
/// the resulting order is the actual completion order. Clone shares the same
/// underlying event list.
#[derive(Debug, Clone, Default)]
pub struct TraceRecorder {
    events: std::sync::Arc<std::sync::Mutex<Vec<TraceEvent>>>,
}

impl TraceRecorder {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn event(&self, name: &str, label: Option<&str>) {
        self.event_with(name, label, []);
    }

    pub fn event_with(
        &self,
        name: &str,
        label: Option<&str>,
        fields: impl IntoIterator<Item = (String, FieldValue)>,
    ) {
        let mut events = self.events.lock().unwrap();
        let seq = events.len() as u64 + 1;
        events.push(TraceEvent {
            seq,
            name: name.to_string(),
            label: label.map(str::to_string),
            fields: fields.into_iter().collect(),
        });
    }

    pub fn into_events(self) -> Vec<TraceEvent> {
        match std::sync::Arc::try_unwrap(self.events) {
            Ok(mutex) => mutex.into_inner().unwrap(),
            Err(arc) => arc.lock().unwrap().clone(),
        }
    }

    pub fn snapshot(&self) -> Vec<TraceEvent> {
        self.events.lock().unwrap().clone()
    }
}
