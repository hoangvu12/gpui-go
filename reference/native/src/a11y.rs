//! gpui-go native accessibility provider service (ticket22, reserved
//! slot 9).
//!
//! The pinned Windows accessibility surface of the gpui-CE reference:
//! the `accesskit` 0.24.1 / `accesskit_windows` 0.33.1 adapter stack
//! (the CE workspace lock resolution — `accesskit` is already in the
//! graph via the gpui-ce path dependency; `accesskit_windows` resolves
//! against the same `windows` 0.62.2 crate family this artifact uses),
//! driven exactly through the paths the pin uses:
//!
//! * `accesskit_windows::Adapter::new(hwnd, is_focused, action_handler)`
//!   — the pin's `a11y_init` (window.rs 1145-1157): the adapter is
//!   created on the window's message thread (the OLE STA foreground
//!   host), with `init_uia()` running inside (must not race a
//!   WM_GETOBJECT; this service's create entry is called at window
//!   setup, never from the WM_GETOBJECT path).
//! * `Adapter::update_if_active(factory)` — the pin's
//!   `a11y_tree_update` (window.rs 1161-1176): the factory runs only
//!   when the tree is active (an assistive technology has connected);
//!   the returned `QueuedEvents` must be raised AFTER the state lock
//!   is dropped (the pin's comment: `events.raise()` may deliver a
//!   nested WM_GETOBJECT). This service mirrors both: the publish
//!   entry decodes the caller's serialized tree ONLY when active, and
//!   raises events outside the service mutex.
//! * `Adapter::handle_wm_getobject(wparam, lparam, activation_handler)`
//!   — the pin's `handle_wm_getobject` (events.rs 855-871): the
//!   `Option<impl Into<LRESULT>>` conversion (UiaReturnRawElementProvider,
//!   which may send a NESTED WM_GETOBJECT) happens with the state
//!   lock dropped, exactly like the pin drops its `RefCell` borrow
//!   before `.into()`.
//! * The activation handler — the pin's window-creation closure
//!   (window.rs 1573-1591): returns the INITIAL minimal tree (a
//!   single `Role::Window` root with the window title as its label,
//!   focus on the root) and flips the active flag; the complete tree
//!   arrives on the next forced frame's publish ("activation before
//!   the complete tree"). This service stores the pending initial
//!   tree at adapter creation (the serialized root label) and returns
//!   it from `request_initial_tree`, so activation never needs a Go
//!   trampoline. The Go side publishes every frame; before activation
//!   the publish is a no-op (the pin's `update_if_active` returning
//!   `None` without running the factory), and on activation the Go
//!   side observes `is_active()` and forces a frame.
//! * The action handler — the pin's `A11yActionHandler` closure:
//!   `do_action` only queues the request (`action_sender
//!   .send_blocking(request)` — an async_channel handoff, never a
//!   synchronous callback into app state). This service's handler
//!   pushes into a native queue drained by the polling entry
//!   (`a11y_poll_actions`), preserving the queue-then-dispatch
//!   behavior without a Go callback.
//!
//! ABI shape (the established conventions; see ATLAS_ABI.md):
//! fixed-width `#[repr(C)]` records, no Rust slices/strings/enums
//! crossing the boundary, caller-owned buffers, every export
//! containing panics with `catch_unwind`, and an adapter slot
//! registry mirroring the text/image/svg services (handle =
//! `(slot index << 20) | slot generation`, stale after destroy or
//! slot reuse, capacity [`MAX_A11Y_ADAPTERS`]).

use std::collections::VecDeque;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::{Arc, Mutex, MutexGuard};

use accesskit::{
    Action, ActionData, ActionRequest, Node, NodeId, Rect, Role, TextPosition, TextSelection,
    Tree, TreeId, TreeUpdate,
};
use accesskit_windows::{Adapter, LPARAM, WPARAM};
use windows::Win32::Foundation::HWND;

// ---------------------------------------------------------------------------
// Constants and status codes
// ---------------------------------------------------------------------------

/// Live adapter slot bound (the text/image/svg services' slot bound,
/// mirrored).
pub const MAX_A11Y_ADAPTERS: u32 = 64;

/// Maximum accepted serialized node count in one publish.
pub const MAX_A11Y_NODES: u32 = 1 << 16;

/// Maximum accepted children-array entries in one publish.
pub const MAX_A11Y_CHILDREN: u32 = 1 << 20;

/// Maximum accepted string-pool bytes in one publish.
pub const MAX_A11Y_STRINGS: u32 = 1 << 22;

/// Maximum initial root label bytes.
pub const MAX_A11Y_LABEL: u32 = 1 << 16;

/// A11y service status codes. 0 is success; negative values are
/// caller/argument errors; 100+ are internal failures.
pub mod a11y_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The adapter handle encoded a past generation (destroyed or
    /// slot reused since).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed (out-of-range slot).
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// An invalid tag, count, offset or record-size value.
    pub const ERR_BAD_VALUE: i32 = -4;
    /// A serialized string was not valid UTF-8.
    pub const ERR_BAD_STRING: i32 = -5;
    /// A record-size self-check mismatched this service's layout.
    pub const ERR_RECORD_SIZE: i32 = -6;
    /// The poll's string buffer was too small; `out_strings_needed`
    /// carries the required size (retry with a larger buffer; the
    /// actions were NOT drained).
    pub const ERR_CAPACITY: i32 = -7;
    /// A panic was contained by `catch_unwind` before returning.
    pub const ERR_PANIC: i32 = 100;
}

/// Role tags (the port's `Role` enumeration mapped onto the pinned
/// `accesskit::Role` variants the ticket-21 published slice uses; the
/// full accesskit Role enum is far larger — unmapped tags are
/// `ERR_BAD_VALUE`, never silently coerced).
pub mod a11y_role {
    /// The window root role (`Role::Window`).
    pub const WINDOW: u32 = 1;
    /// A plain grouping node (`Role::GenericContainer`).
    pub const GENERIC_CONTAINER: u32 = 2;
    /// An interactive button (`Role::Button`).
    pub const BUTTON: u32 = 3;
    /// An editable text field (`Role::TextInput`).
    pub const TEXT_INPUT: u32 = 4;
    /// One run of text inside a text input (`Role::TextRun`).
    pub const TEXT_RUN: u32 = 5;
    /// Static, non-editable text (`Role::StaticText`).
    pub const STATIC_TEXT: u32 = 6;
    /// A toggleable check box (`Role::CheckBox`).
    pub const CHECK_BOX: u32 = 7;
    /// An adjustable slider (`Role::Slider`).
    pub const SLIDER: u32 = 8;
}

/// Action tags (the port's `A11yAction` set mapped onto the pinned
/// `accesskit::Action` variants; the port's `A11yActionShowMenu`
/// maps to the pin's `Action::ShowContextMenu`).
pub mod a11y_action {
    /// Activate the node (`Action::Click`).
    pub const CLICK: u32 = 1;
    /// Move keyboard focus (`Action::Focus`).
    pub const FOCUS: u32 = 2;
    /// Clear window focus (`Action::Blur`).
    pub const BLUR: u32 = 3;
    /// Increase the value (`Action::Increment`).
    pub const INCREMENT: u32 = 4;
    /// Decrease the value (`Action::Decrement`).
    pub const DECREMENT: u32 = 5;
    /// Assign the value (`Action::SetValue`, `ActionData::Value`).
    pub const SET_VALUE: u32 = 6;
    /// Replace the selected text (`Action::ReplaceSelectedText`,
    /// `ActionData::Value`).
    pub const REPLACE_SELECTED_TEXT: u32 = 7;
    /// Change the text selection (`Action::SetTextSelection`,
    /// `ActionData::SetTextSelection`).
    pub const SET_TEXT_SELECTION: u32 = 8;
    /// Open the context menu (`Action::ShowContextMenu`).
    pub const SHOW_CONTEXT_MENU: u32 = 9;
}

/// Action payload tags for [`GpuiGoA11yActionRecord::data_tag`].
pub mod a11y_action_data {
    /// No payload.
    pub const NONE: u32 = 0;
    /// A string value (SetValue/ReplaceSelectedText).
    pub const VALUE: u32 = 1;
    /// A text selection (SetTextSelection).
    pub const TEXT_SELECTION: u32 = 2;
}

// ---------------------------------------------------------------------------
// ABI records
// ---------------------------------------------------------------------------

/// One serialized accessibility node (the port's published slice of
/// the pinned `accesskit::Node` construction in element.rs
/// `prepaint`: `Node::new(role)` + `set_bounds` (physical pixels,
/// f64) + `write_a11y_info` (label/description/value/author id/
/// actions) + `set_hidden`, children via the builder's `push_child`).
///
/// Layout (x86-64, `#[repr(C)]`): `id` @0 (u64), `role` @8, `flags`
/// @12, `actions_mask` @16, `children_count` @20, `children_offset`
/// @24 (index into the publish's u64 children array), `label_offset`
/// @28, `label_len` @32, `description_offset` @36,
/// `description_len` @40, `value_offset` @44, `value_len` @48,
/// `author_id_offset` @52, `author_id_len` @56, `bounds_x0_bits` @64
/// (u64, f64 bits), `bounds_y0_bits` @72, `bounds_x1_bits` @80,
/// `bounds_y1_bits` @88, `record_size` @96; size 104, alignment 8.
#[repr(C)]
pub struct GpuiGoA11yNodeRecord {
    /// The node id (the port's `A11yNodeID`; 0 is every tree's root).
    pub id: u64,
    /// Role tag; see [`a11y_role`].
    pub role: u32,
    /// Flags: bit 0 = hidden, bit 1 = has bounds.
    pub flags: u32,
    /// OR of [`a11y_action`] tags the node advertises.
    pub actions_mask: u32,
    /// Number of child ids starting at `children_offset`.
    pub children_count: u32,
    /// Index into the publish call's children array.
    pub children_offset: u32,
    /// Byte offset of the label in the string pool.
    pub label_offset: u32,
    /// Label byte length (0 = no label).
    pub label_len: u32,
    /// Byte offset of the description in the string pool.
    pub description_offset: u32,
    /// Description byte length (0 = none).
    pub description_len: u32,
    /// Byte offset of the value in the string pool.
    pub value_offset: u32,
    /// Value byte length (0 = none).
    pub value_len: u32,
    /// Byte offset of the author id in the string pool.
    pub author_id_offset: u32,
    /// Author id byte length (0 = none).
    pub author_id_len: u32,
    /// Bounds x0 as f64 bits (physical pixels).
    pub bounds_x0_bits: u64,
    /// Bounds y0 as f64 bits.
    pub bounds_y0_bits: u64,
    /// Bounds x1 as f64 bits.
    pub bounds_x1_bits: u64,
    /// Bounds y1 as f64 bits.
    pub bounds_y1_bits: u64,
    /// Self-check: `size_of::<GpuiGoA11yNodeRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoA11yNodeRecord {
    fn default() -> Self {
        GpuiGoA11yNodeRecord {
            id: 0,
            role: a11y_role::WINDOW,
            flags: 0,
            actions_mask: 0,
            children_count: 0,
            children_offset: 0,
            label_offset: 0,
            label_len: 0,
            description_offset: 0,
            description_len: 0,
            value_offset: 0,
            value_len: 0,
            author_id_offset: 0,
            author_id_len: 0,
            bounds_x0_bits: 0,
            bounds_y0_bits: 0,
            bounds_x1_bits: 0,
            bounds_y1_bits: 0,
            record_size: core::mem::size_of::<GpuiGoA11yNodeRecord>() as u32,
        }
    }
}

// Compile-time layout pins (the Go loader mirrors these exactly).
const _: () = assert!(core::mem::size_of::<GpuiGoA11yNodeRecord>() == 104);
const _: () = assert!(core::mem::align_of::<GpuiGoA11yNodeRecord>() == 8);

/// Publish result record. Layout: `status` @0, `active` @4,
/// `applied` @8, `events_raised` @12, `record_size` @16; size 20,
/// alignment 4.
#[repr(C)]
pub struct GpuiGoA11yPublishRecord {
    /// Status of the call.
    pub status: i32,
    /// Whether an assistive technology has activated the adapter (the
    /// pin's active flag). When false the serialized update was NOT
    /// decoded (the pin's factory-not-called semantics).
    pub active: u32,
    /// Whether the decoded update was applied to the live tree.
    pub applied: u32,
    /// Whether change events were raised to the connected technology.
    pub events_raised: u32,
    /// Self-check: `size_of::<GpuiGoA11yPublishRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoA11yPublishRecord {
    fn default() -> Self {
        GpuiGoA11yPublishRecord {
            status: a11y_status::OK,
            active: 0,
            applied: 0,
            events_raised: 0,
            record_size: core::mem::size_of::<GpuiGoA11yPublishRecord>() as u32,
        }
    }
}

const _: () = assert!(core::mem::size_of::<GpuiGoA11yPublishRecord>() == 20);
const _: () = assert!(core::mem::align_of::<GpuiGoA11yPublishRecord>() == 4);

/// One drained action request. Layout: `target_node` @0 (u64),
/// `action` @8, `data_tag` @12, `value_offset` @16, `value_len` @20,
/// `anchor_node` @24 (u64), `anchor_char` @32, `focus_node` @36 (u64),
/// `focus_char` @44, `record_size` @48; size 56, alignment 8.
#[repr(C)]
pub struct GpuiGoA11yActionRecord {
    /// The node the action targets.
    pub target_node: u64,
    /// Action tag; see [`a11y_action`].
    pub action: u32,
    /// Payload tag; see [`a11y_action_data`].
    pub data_tag: u32,
    /// Byte offset of the value string in the poll's string buffer.
    pub value_offset: u32,
    /// Value byte length.
    pub value_len: u32,
    /// Selection anchor node (SetTextSelection).
    pub anchor_node: u64,
    /// Selection anchor character index.
    pub anchor_char: u32,
    /// Selection focus node.
    pub focus_node: u64,
    /// Selection focus character index.
    pub focus_char: u32,
    /// Self-check: `size_of::<GpuiGoA11yActionRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoA11yActionRecord {
    fn default() -> Self {
        GpuiGoA11yActionRecord {
            target_node: 0,
            action: 0,
            data_tag: a11y_action_data::NONE,
            value_offset: 0,
            value_len: 0,
            anchor_node: 0,
            anchor_char: 0,
            focus_node: 0,
            focus_char: 0,
            record_size: core::mem::size_of::<GpuiGoA11yActionRecord>() as u32,
        }
    }
}

const _: () = assert!(core::mem::size_of::<GpuiGoA11yActionRecord>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoA11yActionRecord>() == 8);

/// The a11y service table (reserved slot 9 of the bootstrap table).
#[repr(C)]
pub struct GpuiGoA11yTable {
    /// `a11y_adapter_create`.
    pub adapter_create: Option<
        unsafe extern "system" fn(
            hwnd: u64,
            is_focused: u32,
            label_ptr: *const u8,
            label_len: u32,
            out_handle: *mut u64,
        ) -> i32,
    >,
    /// `a11y_adapter_destroy`.
    pub adapter_destroy: Option<unsafe extern "system" fn(handle: u64) -> i32>,
    /// `a11y_publish`.
    pub publish: Option<
        unsafe extern "system" fn(
            handle: u64,
            nodes_ptr: *const GpuiGoA11yNodeRecord,
            node_count: u32,
            children_ptr: *const u64,
            children_len: u32,
            strings_ptr: *const u8,
            strings_len: u32,
            root_id: u64,
            focus_id: u64,
            out: *mut GpuiGoA11yPublishRecord,
        ) -> i32,
    >,
    /// `a11y_handle_wm_getobject`.
    pub handle_wm_getobject: Option<
        unsafe extern "system" fn(
            handle: u64,
            wparam: u64,
            lparam: i64,
            out_handled: *mut u32,
            out_lresult: *mut i64,
        ) -> i32,
    >,
    /// `a11y_poll_actions`.
    pub poll_actions: Option<
        unsafe extern "system" fn(
            handle: u64,
            out_records: *mut GpuiGoA11yActionRecord,
            record_capacity: u32,
            strings_buf: *mut u8,
            strings_capacity: u32,
            out_count: *mut u32,
            out_strings_needed: *mut u32,
        ) -> i32,
    >,
    /// `a11y_is_active`.
    pub is_active: Option<unsafe extern "system" fn(handle: u64, out_active: *mut u32) -> i32>,
    /// `a11y_pending_actions` (observability: the undrained queue
    /// depth).
    pub pending_actions: Option<unsafe extern "system" fn(handle: u64, out_count: *mut u32) -> i32>,
    /// `a11y_panic_probe` (test-only; panics inside catch_unwind).
    pub panic_probe: Option<unsafe extern "system" fn() -> i32>,
    /// A11y ABI version.
    pub service_version: u32,
    /// `size_of::<GpuiGoA11yTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoA11yTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoA11yNodeRecord>()` self-check.
    pub size_of_node_record: u32,
    /// `size_of::<GpuiGoA11yPublishRecord>()` self-check.
    pub size_of_publish_record: u32,
    /// `size_of::<GpuiGoA11yActionRecord>()` self-check.
    pub size_of_action_record: u32,
    /// [`MAX_A11Y_ADAPTERS`].
    pub max_adapters: u32,
    /// [`MAX_A11Y_NODES`].
    pub max_nodes: u32,
    /// [`MAX_A11Y_STRINGS`].
    pub max_strings: u32,
}

/// A11y service ABI version.
pub const GPUI_GO_A11Y_SERVICE_VERSION: u32 = 1;

pub(crate) static A11Y_TABLE: GpuiGoA11yTable = GpuiGoA11yTable {
    adapter_create: Some(a11y_adapter_create),
    adapter_destroy: Some(a11y_adapter_destroy),
    publish: Some(a11y_publish),
    handle_wm_getobject: Some(a11y_handle_wm_getobject),
    poll_actions: Some(a11y_poll_actions),
    is_active: Some(a11y_is_active),
    pending_actions: Some(a11y_pending_actions),
    panic_probe: Some(a11y_panic_probe),
    service_version: GPUI_GO_A11Y_SERVICE_VERSION,
    size_of_table: core::mem::size_of::<GpuiGoA11yTable>() as u32,
    align_of_table: core::mem::align_of::<GpuiGoA11yTable>() as u32,
    size_of_node_record: core::mem::size_of::<GpuiGoA11yNodeRecord>() as u32,
    size_of_publish_record: core::mem::size_of::<GpuiGoA11yPublishRecord>() as u32,
    size_of_action_record: core::mem::size_of::<GpuiGoA11yActionRecord>() as u32,
    max_adapters: MAX_A11Y_ADAPTERS,
    max_nodes: MAX_A11Y_NODES,
    max_strings: MAX_A11Y_STRINGS,
};

const _: () = assert!(core::mem::size_of::<GpuiGoA11yTable>() == 104);
const _: () = assert!(core::mem::align_of::<GpuiGoA11yTable>() == 8);

// ---------------------------------------------------------------------------
// Internal state
// ---------------------------------------------------------------------------

/// The pin's initial minimal tree (window.rs 1573-1582): a single
/// `Role::Window` root whose label is the window title, focus on the
/// root. Stored at adapter creation and returned by the activation
/// handler; the complete tree arrives on the next published frame.
fn initial_tree(label: &str) -> TreeUpdate {
    let mut root = Node::new(Role::Window);
    if !label.is_empty() {
        root.set_label(label.to_string());
    }
    TreeUpdate {
        nodes: vec![(NodeId(0), root)],
        tree: Some(Tree::new(NodeId(0))),
        tree_id: TreeId::ROOT,
        focus: NodeId(0),
    }
}

/// The queued action (the native mirror of the pin's async_channel
/// handoff — `do_action` only queues).
#[derive(Clone)]
struct QueuedAction {
    target_node: u64,
    action: u32,
    data_tag: u32,
    value: String,
    selection: Option<TextSelection>,
}

/// The native `ActionHandler` (the pin's A11yActionHandler closure:
/// queue, never dispatch synchronously from the UIA thread).
struct QueueingActionHandler {
    queue: Arc<Mutex<VecDeque<QueuedAction>>>,
}

impl accesskit::ActionHandler for QueueingActionHandler {
    fn do_action(&mut self, request: ActionRequest) {
        let (data_tag, value, selection) = match request.data.as_ref() {
            None => (a11y_action_data::NONE, String::new(), None),
            Some(ActionData::Value(value)) => {
                (a11y_action_data::VALUE, value.to_string(), None)
            }
            Some(ActionData::SetTextSelection(selection)) => {
                (a11y_action_data::TEXT_SELECTION, String::new(), Some(selection.clone()))
            }
            Some(other) => {
                // The port's action-data slice covers Value and
                // SetTextSelection; other payload kinds still queue
                // with their action tag but without the unmodeled
                // payload (recorded honestly at the drain site).
                let _ = other;
                (a11y_action_data::NONE, String::new(), None)
            }
        };
        let action = action_to_tag(request.action);
        if let Ok(mut queue) = self.queue.lock() {
            queue.push_back(QueuedAction {
                target_node: request.target_node.0,
                action,
                data_tag,
                value,
                selection,
            });
        }
    }
}

/// One adapter slot.
struct A11yAdapterState {
    /// The owning window handle (diagnostic identity; not read after
    /// creation).
    #[expect(dead_code)]
    hwnd: u64,
    adapter: Adapter,
    /// The active flag (the pin's `a11y_active_flag`, set inside the
    /// activation handler when the assistive technology connects).
    active: bool,
    /// The undrained action queue (shared with the native handler).
    action_queue: Arc<Mutex<VecDeque<QueuedAction>>>,
    /// The pending initial tree (the pin's initial_tree clone held by
    /// the activation closure; consumed on WM_GETOBJECT activation).
    pending_initial: Option<TreeUpdate>,
}

struct A11ySlot {
    slot_generation: u32,
    occupied: bool,
    adapter: Option<A11yAdapterState>,
}

impl A11ySlot {
    fn vacant() -> Self {
        A11ySlot { slot_generation: 0, occupied: false, adapter: None }
    }
}

struct A11yServiceState {
    slots: Vec<A11ySlot>,
}

static GLOBAL: Mutex<A11yServiceState> =
    Mutex::new(A11yServiceState { slots: Vec::new() });

// SAFETY: the state holds WinRT/COM-backed adapter objects which the
// windows crate conservatively treats as !Send. The adapters are
// created on the window's STA thread and used only from the window's
// message thread (create/publish/getobject/poll are foreground-thread
// entries in the Go host's discipline, exactly like the pin's
// a11y_init/a11y_tree_update/handle_wm_getobject), and the action
// queue is a plain mutex-protected VecDeque; the state merely resides
// behind a lock, so no object is ever used concurrently.
unsafe impl Send for A11yServiceState {}

fn lock_global() -> MutexGuard<'static, A11yServiceState> {
    GLOBAL.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Adapter handle: `(slot index << 20) | slot generation` (the
/// scene/layout/atlas encoding).
fn adapter_handle_parts(handle: u64) -> (usize, u32) {
    ((handle >> 20) as usize, (handle & 0xFFFFF) as u32)
}

fn make_adapter_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << 20) | (generation as u64 & 0xFFFFF)
}

fn resolve_slot_mut(
    state: &mut A11yServiceState,
    slot_index: usize,
    slot_generation: u32,
) -> Result<&mut A11yAdapterState, i32> {
    if slot_index >= state.slots.len() {
        return Err(a11y_status::ERR_BAD_HANDLE);
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(a11y_status::ERR_STALE_HANDLE);
    }
    slot.adapter.as_mut().ok_or(a11y_status::ERR_STALE_HANDLE)
}

/// Maps a role tag onto the pinned `Role`.
fn role_from_tag(tag: u32) -> Result<Role, i32> {
    match tag {
        a11y_role::WINDOW => Ok(Role::Window),
        a11y_role::GENERIC_CONTAINER => Ok(Role::GenericContainer),
        a11y_role::BUTTON => Ok(Role::Button),
        a11y_role::TEXT_INPUT => Ok(Role::TextInput),
        a11y_role::TEXT_RUN => Ok(Role::TextRun),
        // The pin's static text role is `Role::Label` (elements/
        // text.rs 189-191: a text element with an id is a Label);
        // accesskit 0.24.1 has no StaticText variant.
        a11y_role::STATIC_TEXT => Ok(Role::Label),
        a11y_role::CHECK_BOX => Ok(Role::CheckBox),
        a11y_role::SLIDER => Ok(Role::Slider),
        _ => Err(a11y_status::ERR_BAD_VALUE),
    }
}

/// Maps the pinned `Action` onto the port's tag (0 when unmapped —
/// drained with the action lost, never silently coerced; the
/// published action set is the mapped one).
fn action_to_tag(action: Action) -> u32 {
    match action {
        Action::Click => a11y_action::CLICK,
        Action::Focus => a11y_action::FOCUS,
        Action::Blur => a11y_action::BLUR,
        Action::Increment => a11y_action::INCREMENT,
        Action::Decrement => a11y_action::DECREMENT,
        Action::SetValue => a11y_action::SET_VALUE,
        Action::ReplaceSelectedText => a11y_action::REPLACE_SELECTED_TEXT,
        Action::SetTextSelection => a11y_action::SET_TEXT_SELECTION,
        Action::ShowContextMenu => a11y_action::SHOW_CONTEXT_MENU,
        _ => 0,
    }
}

/// Maps the port's action tag onto the pinned `Action`.
fn action_from_tag(tag: u32) -> Result<Action, i32> {
    match tag {
        a11y_action::CLICK => Ok(Action::Click),
        a11y_action::FOCUS => Ok(Action::Focus),
        a11y_action::BLUR => Ok(Action::Blur),
        a11y_action::INCREMENT => Ok(Action::Increment),
        a11y_action::DECREMENT => Ok(Action::Decrement),
        a11y_action::SET_VALUE => Ok(Action::SetValue),
        a11y_action::REPLACE_SELECTED_TEXT => Ok(Action::ReplaceSelectedText),
        a11y_action::SET_TEXT_SELECTION => Ok(Action::SetTextSelection),
        a11y_action::SHOW_CONTEXT_MENU => Ok(Action::ShowContextMenu),
        _ => Err(a11y_status::ERR_BAD_VALUE),
    }
}

/// The activation handler: returns the stored pending initial tree
/// and flips the active flag (the pin's activation closure,
/// window.rs 1573-1591 — the minimal tree now, the complete tree on
/// the next forced frame).
struct PendingInitialTreeHandler {
    initial: TreeUpdate,
}

impl accesskit::ActivationHandler for PendingInitialTreeHandler {
    fn request_initial_tree(&mut self) -> Option<TreeUpdate> {
        Some(self.initial.clone())
    }
}

// ---------------------------------------------------------------------------
// Body implementations (caller holds no locks unless stated)
// ---------------------------------------------------------------------------

fn adapter_create_body(
    hwnd: u64,
    is_focused: u32,
    label_ptr: *const u8,
    label_len: u32,
    out_handle: *mut u64,
) -> i32 {
    unsafe {
        if out_handle.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        if hwnd == 0 {
            return a11y_status::ERR_BAD_VALUE;
        }
        if label_len > MAX_A11Y_LABEL {
            return a11y_status::ERR_BAD_VALUE;
        }
        if label_len > 0 && label_ptr.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        let label_bytes: &[u8] = if label_len == 0 {
            &[]
        } else {
            std::slice::from_raw_parts(label_ptr, label_len as usize)
        };
        let label = match std::str::from_utf8(label_bytes) {
            Ok(label) => label,
            Err(_) => return a11y_status::ERR_BAD_STRING,
        };

        // The pin creates the adapter on the window's message thread
        // (a11y_init); the Go caller's contract is the same thread
        // discipline (the foreground host thread that owns the HWND).
        let queue = Arc::new(Mutex::new(VecDeque::<QueuedAction>::new()));
        let action_handler = QueueingActionHandler { queue: queue.clone() };
        let adapter = Adapter::new(HWND(hwnd as _), is_focused != 0, action_handler);
        let initial = initial_tree(label);

        let mut state = lock_global();
        let mut take_vacant = false;
        let index = if state.slots.len() >= MAX_A11Y_ADAPTERS as usize {
            match state.slots.iter().position(|slot| !slot.occupied) {
                Some(index) => {
                    take_vacant = true;
                    index
                }
                None => return a11y_status::ERR_BAD_VALUE,
            }
        } else {
            state.slots.push(A11ySlot::vacant());
            state.slots.len() - 1
        };
        let generation = if take_vacant {
            state.slots[index].slot_generation.wrapping_add(1) & 0xFFFFF
        } else {
            1
        };
        state.slots[index] = A11ySlot {
            slot_generation: generation,
            occupied: true,
            adapter: Some(A11yAdapterState {
                hwnd,
                adapter,
                active: false,
                action_queue: queue,
                pending_initial: Some(initial),
            }),
        };
        *out_handle = make_adapter_handle(index, generation);
        a11y_status::OK
    }
}

fn adapter_destroy_body(handle: u64) -> i32 {
    let (slot_index, slot_generation) = adapter_handle_parts(handle);
    let mut state = lock_global();
if slot_index >= state.slots.len() {
        return a11y_status::ERR_BAD_HANDLE;
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return a11y_status::ERR_STALE_HANDLE;
    }
    // Drop the adapter (the pin's window closure drops A11yState;
    // the UIA provider references release apartment-affine).
    slot.adapter.take();
    slot.occupied = false;
    a11y_status::OK
}

/// Decodes the serialized publish into a `TreeUpdate` (the decode
/// runs only when the adapter is active, mirroring the pin's
/// factory-only-when-active).
unsafe fn decode_update(
    nodes_ptr: *const GpuiGoA11yNodeRecord,
    node_count: u32,
    children_ptr: *const u64,
    children_len: u32,
    strings_ptr: *const u8,
    strings_len: u32,
    root_id: u64,
    focus_id: u64,
) -> Result<TreeUpdate, i32> {
    unsafe {
        if node_count == 0 || nodes_ptr.is_null() {
            return Err(a11y_status::ERR_NULL_ARG);
        }
        if node_count > MAX_A11Y_NODES {
            return Err(a11y_status::ERR_BAD_VALUE);
        }
        if children_len > MAX_A11Y_CHILDREN {
            return Err(a11y_status::ERR_BAD_VALUE);
        }
        if strings_len > MAX_A11Y_STRINGS {
            return Err(a11y_status::ERR_BAD_VALUE);
        }
        if children_len > 0 && children_ptr.is_null() {
            return Err(a11y_status::ERR_NULL_ARG);
        }
        if strings_len > 0 && strings_ptr.is_null() {
            return Err(a11y_status::ERR_NULL_ARG);
        }
        let nodes: &[GpuiGoA11yNodeRecord] =
            std::slice::from_raw_parts(nodes_ptr, node_count as usize);
        let children: &[u64] = if children_len == 0 {
            &[]
        } else {
            std::slice::from_raw_parts(children_ptr, children_len as usize)
        };
        let strings: &[u8] = if strings_len == 0 {
            &[]
        } else {
            std::slice::from_raw_parts(strings_ptr, strings_len as usize)
        };
        let pool = |offset: u32, len: u32| -> Result<&str, i32> {
            let start = offset as usize;
            let end = start.checked_add(len as usize).ok_or(a11y_status::ERR_BAD_VALUE)?;
            if end > strings.len() {
                return Err(a11y_status::ERR_BAD_VALUE);
            }
            std::str::from_utf8(&strings[start..end]).map_err(|_| a11y_status::ERR_BAD_STRING)
        };

        let mut decoded: Vec<(NodeId, Node)> = Vec::with_capacity(node_count as usize);
        let mut has_root = false;
        for record in nodes {
            if record.record_size as usize != core::mem::size_of::<GpuiGoA11yNodeRecord>() {
                return Err(a11y_status::ERR_RECORD_SIZE);
            }
            let role = role_from_tag(record.role)?;
            let mut node = Node::new(role);
            if record.label_len > 0 {
                node.set_label(pool(record.label_offset, record.label_len)?.to_string());
            }
            if record.description_len > 0 {
                node.set_description(
                    pool(record.description_offset, record.description_len)?.to_string(),
                );
            }
            if record.value_len > 0 {
                node.set_value(pool(record.value_offset, record.value_len)?.to_string());
            }
            if record.author_id_len > 0 {
                node.set_author_id(
                    pool(record.author_id_offset, record.author_id_len)?.to_string(),
                );
            }
            if record.flags & 0x1 != 0 {
                node.set_hidden();
            }
            if record.flags & 0x2 != 0 {
                node.set_bounds(Rect {
                    x0: f64::from_bits(record.bounds_x0_bits),
                    y0: f64::from_bits(record.bounds_y0_bits),
                    x1: f64::from_bits(record.bounds_x1_bits),
                    y1: f64::from_bits(record.bounds_y1_bits),
                });
            }
            // The actions mask is a tag-PRESENCE set: bit i set means
            // the action with tag i+1 is advertised (all nine tags
            // representable; the rev-10 decoder misread the bit VALUE
            // as the tag, which only worked for power-of-two tags —
            // the a11y Go worker's finding).
            let mut mask = record.actions_mask;
            while mask != 0 {
                let index = mask.trailing_zeros();
                mask &= !(1 << index);
                let tag = index + 1;
                node.add_action(action_from_tag(tag)?);
            }
            if record.children_count > 0 {
                let start = record.children_offset as usize;
                let end = start
                    .checked_add(record.children_count as usize)
                    .ok_or(a11y_status::ERR_BAD_VALUE)?;
                if end > children.len() {
                    return Err(a11y_status::ERR_BAD_VALUE);
                }
                node.set_children(
                    children[start..end].iter().map(|id| NodeId(*id)).collect::<Vec<_>>(),
                );
            }
            if record.id == root_id {
                has_root = true;
            }
            decoded.push((NodeId(record.id), node));
        }
        if !has_root {
            return Err(a11y_status::ERR_BAD_VALUE);
        }
        Ok(TreeUpdate {
            nodes: decoded,
            tree: Some(Tree::new(NodeId(root_id))),
            tree_id: TreeId::ROOT,
            focus: NodeId(focus_id),
        })
    }
}

fn publish_body(
    handle: u64,
    nodes_ptr: *const GpuiGoA11yNodeRecord,
    node_count: u32,
    children_ptr: *const u64,
    children_len: u32,
    strings_ptr: *const u8,
    strings_len: u32,
    root_id: u64,
    focus_id: u64,
    out: *mut GpuiGoA11yPublishRecord,
) -> i32 {
    unsafe {
        if out.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        *out = GpuiGoA11yPublishRecord {
            status: a11y_status::OK,
            active: 0,
            applied: 0,
            events_raised: 0,
            record_size: core::mem::size_of::<GpuiGoA11yPublishRecord>() as u32,
        };
        let (slot_index, slot_generation) = adapter_handle_parts(handle);
        let mut state = lock_global();
        let adapter_state = match resolve_slot_mut(&mut state, slot_index, slot_generation) {
            Ok(adapter_state) => adapter_state,
            Err(code) => return code,
        };
        if !adapter_state.active {
            // The pin's update_if_active when inactive: the factory
            // never runs, no update applied. The serialized buffer is
            // not decoded either (pure Go-side cost, identical
            // observable behavior).
            return a11y_status::OK;
        }
        let update = match decode_update(
            nodes_ptr,
            node_count,
            children_ptr,
            children_len,
            strings_ptr,
            strings_len,
            root_id,
            focus_id,
        ) {
            Ok(update) => update,
            Err(code) => return code,
        };
        // update_if_active with an active tree always applies and
        // returns queued events; the events must be raised AFTER the
        // state lock drops (the pin's reentrancy rule).
        let events = adapter_state.adapter.update_if_active(|| update);
        drop(state);
        (*out).active = 1;
        (*out).applied = 1;
        if let Some(events) = events {
            events.raise();
            (*out).events_raised = 1;
        }
        a11y_status::OK
    }
}

fn handle_wm_getobject_body(
    handle: u64,
    wparam: u64,
    lparam: i64,
    out_handled: *mut u32,
    out_lresult: *mut i64,
) -> i32 {
    unsafe {
        if out_handled.is_null() || out_lresult.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        *out_handled = 0;
        *out_lresult = 0;
        let (slot_index, slot_generation) = adapter_handle_parts(handle);
        let mut state = lock_global();
        let adapter_state = match resolve_slot_mut(&mut state, slot_index, slot_generation) {
            Ok(adapter_state) => adapter_state,
            Err(code) => return code,
        };
        let mut activation = PendingInitialTreeHandler {
            initial: adapter_state
                .pending_initial
                .clone()
                .unwrap_or_else(|| initial_tree("")),
        };
        let result = adapter_state
            .adapter
            .handle_wm_getobject(WPARAM(wparam as usize), LPARAM(lparam as isize), &mut activation);
        // The activation handler ran: the assistive technology
        // connected (the pin's active flag flips inside the
        // activation closure).
        adapter_state.active = true;
        // Drop the state lock BEFORE converting the LRESULT: the
        // conversion calls UiaReturnRawElementProvider, which may send
        // a NESTED WM_GETOBJECT back into the window procedure (the
        // pin's comment, events.rs 866-869) and re-enter this entry.
        drop(state);
        match result {
            Some(lresult) => {
                let lresult: windows::Win32::Foundation::LRESULT = lresult.into();
                *out_handled = 1;
                *out_lresult = lresult.0 as i64;
            }
            None => *out_handled = 0,
        }
        a11y_status::OK
    }
}

fn poll_actions_body(
    handle: u64,
    out_records: *mut GpuiGoA11yActionRecord,
    record_capacity: u32,
    strings_buf: *mut u8,
    strings_capacity: u32,
    out_count: *mut u32,
    out_strings_needed: *mut u32,
) -> i32 {
    unsafe {
        if out_count.is_null() || out_strings_needed.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        *out_count = 0;
        *out_strings_needed = 0;
        let (slot_index, slot_generation) = adapter_handle_parts(handle);
        let state = lock_global();
        if slot_index >= state.slots.len() {
            return a11y_status::ERR_BAD_HANDLE;
        }
        let slot = &state.slots[slot_index];
        if !slot.occupied || slot.slot_generation != slot_generation {
            return a11y_status::ERR_STALE_HANDLE;
        }
        let Some(adapter_state) = slot.adapter.as_ref() else {
            return a11y_status::ERR_STALE_HANDLE;
        };
        let mut queue = adapter_state
            .action_queue
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let total = queue.len();
        if total == 0 {
            return a11y_status::OK;
        }
        let drainable = total.min(record_capacity as usize);
        if record_capacity > 0 && out_records.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        // First pass: compute the string bytes needed.
        let mut needed_strings = 0usize;
        for action in queue.iter().take(drainable) {
            match needed_strings.checked_add(action.value.len()) {
                Some(total) => needed_strings = total,
                None => return a11y_status::ERR_BAD_VALUE,
            }
        }
        if needed_strings > strings_capacity as usize
            || (needed_strings > 0 && strings_buf.is_null())
        {
            *out_strings_needed = needed_strings as u32;
            return a11y_status::ERR_CAPACITY;
        }
        let records: &mut [GpuiGoA11yActionRecord] =
            std::slice::from_raw_parts_mut(out_records, drainable);
        let mut string_cursor = 0usize;
        for (index, action) in queue.drain(..drainable).enumerate() {
            let (data_tag, value_offset, value_len, anchor, anchor_char, focus, focus_char) =
                match action.selection.as_ref() {
                    Some(selection) => {
                        let (a_node, a_char) = text_position_parts(&selection.anchor);
                        let (f_node, f_char) = text_position_parts(&selection.focus);
                        (
                            a11y_action_data::TEXT_SELECTION,
                            0,
                            0,
                            a_node,
                            a_char,
                            f_node,
                            f_char,
                        )
                    }
                    None => {
                        let len = action.value.len();
                        if len > 0 {
                            std::ptr::copy_nonoverlapping(
                                action.value.as_ptr(),
                                strings_buf.add(string_cursor),
                                len,
                            );
                        }
                        let offset = string_cursor;
                        string_cursor += len;
                        (
                            if action.data_tag == a11y_action_data::VALUE {
                                a11y_action_data::VALUE
                            } else {
                                a11y_action_data::NONE
                            },
                            offset as u32,
                            len as u32,
                            0,
                            0,
                            0,
                            0,
                        )
                    }
                };
            records[index] = GpuiGoA11yActionRecord {
                target_node: action.target_node,
                action: action.action,
                data_tag,
                value_offset,
                value_len,
                anchor_node: anchor,
                anchor_char,
                focus_node: focus,
                focus_char,
                record_size: core::mem::size_of::<GpuiGoA11yActionRecord>() as u32,
            };
        }
        *out_count = drainable as u32;
        *out_strings_needed = needed_strings as u32;
        a11y_status::OK
    }
}

fn text_position_parts(position: &TextPosition) -> (u64, u32) {
    (position.node.0, position.character_index as u32)
}

fn is_active_body(handle: u64, out_active: *mut u32) -> i32 {
    unsafe {
    if out_active.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        *out_active = 0;
        let (slot_index, slot_generation) = adapter_handle_parts(handle);
        let state = lock_global();
        if slot_index >= state.slots.len() {
            return a11y_status::ERR_BAD_HANDLE;
        }
        let slot = &state.slots[slot_index];
        if !slot.occupied || slot.slot_generation != slot_generation {
            return a11y_status::ERR_STALE_HANDLE;
        }
        match slot.adapter.as_ref() {
            Some(adapter_state) => {
                *out_active = u32::from(adapter_state.active);
                a11y_status::OK
            }
            None => a11y_status::ERR_STALE_HANDLE,
        }
    }
}

fn pending_actions_body(handle: u64, out_count: *mut u32) -> i32 {
    unsafe {
    if out_count.is_null() {
            return a11y_status::ERR_NULL_ARG;
        }
        *out_count = 0;
        let (slot_index, slot_generation) = adapter_handle_parts(handle);
        let state = lock_global();
        if slot_index >= state.slots.len() {
            return a11y_status::ERR_BAD_HANDLE;
        }
        let slot = &state.slots[slot_index];
        if !slot.occupied || slot.slot_generation != slot_generation {
            return a11y_status::ERR_STALE_HANDLE;
        }
        match slot.adapter.as_ref() {
            Some(adapter_state) => {
                let queue = adapter_state
                    .action_queue
                    .lock()
                    .unwrap_or_else(|poisoned| poisoned.into_inner());
                *out_count = queue.len() as u32;
                a11y_status::OK
            }
            None => a11y_status::ERR_STALE_HANDLE,
        }
    }
}

// ---------------------------------------------------------------------------
// Panic containment and exports
// ---------------------------------------------------------------------------

macro_rules! a11y_entry {
    ($body:expr) => {{
        let outcome = catch_unwind(AssertUnwindSafe(|| $body));
        match outcome {
            Ok(code) => code,
            Err(payload) => {
                drop(payload);
                a11y_status::ERR_PANIC
            }
        }
    }};
}

unsafe extern "system" fn a11y_adapter_create(
    hwnd: u64,
    is_focused: u32,
    label_ptr: *const u8,
    label_len: u32,
    out_handle: *mut u64,
) -> i32 {
    a11y_entry!(adapter_create_body(
        hwnd,
        is_focused,
        label_ptr,
        label_len,
        out_handle
    ))
}

unsafe extern "system" fn a11y_adapter_destroy(handle: u64) -> i32 {
    a11y_entry!(adapter_destroy_body(handle))
}

unsafe extern "system" fn a11y_publish(
    handle: u64,
    nodes_ptr: *const GpuiGoA11yNodeRecord,
    node_count: u32,
    children_ptr: *const u64,
    children_len: u32,
    strings_ptr: *const u8,
    strings_len: u32,
    root_id: u64,
    focus_id: u64,
    out: *mut GpuiGoA11yPublishRecord,
) -> i32 {
    a11y_entry!(publish_body(
        handle,
        nodes_ptr,
        node_count,
        children_ptr,
        children_len,
        strings_ptr,
        strings_len,
        root_id,
        focus_id,
        out
    ))
}

unsafe extern "system" fn a11y_handle_wm_getobject(
    handle: u64,
    wparam: u64,
    lparam: i64,
    out_handled: *mut u32,
    out_lresult: *mut i64,
) -> i32 {
    a11y_entry!(handle_wm_getobject_body(handle, wparam, lparam, out_handled, out_lresult))
}

unsafe extern "system" fn a11y_poll_actions(
    handle: u64,
    out_records: *mut GpuiGoA11yActionRecord,
    record_capacity: u32,
    strings_buf: *mut u8,
    strings_capacity: u32,
    out_count: *mut u32,
    out_strings_needed: *mut u32,
) -> i32 {
    a11y_entry!(poll_actions_body(
        handle,
        out_records,
        record_capacity,
        strings_buf,
        strings_capacity,
        out_count,
        out_strings_needed
    ))
}

unsafe extern "system" fn a11y_is_active(handle: u64, out_active: *mut u32) -> i32 {
    a11y_entry!(is_active_body(handle, out_active))
}

unsafe extern "system" fn a11y_pending_actions(handle: u64, out_count: *mut u32) -> i32 {
    a11y_entry!(pending_actions_body(handle, out_count))
}

unsafe extern "system" fn a11y_panic_probe() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        panic!("a11y panic probe (contained by design)");
    }));
    match outcome {
        Ok(()) => a11y_status::OK,
        Err(payload) => {
            drop(payload);
            a11y_status::ERR_PANIC
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    // The ActionHandler trait must be in scope for the queue tests'
    // do_action calls (rev 16, ticket 50: the pre-existing test-only
    // compile error - the trait moved out of the prelude upstream).
    use accesskit::ActionHandler;

    #[test]
    fn role_and_action_maps_round_trip() {
        for tag in [
            a11y_role::WINDOW,
            a11y_role::GENERIC_CONTAINER,
            a11y_role::BUTTON,
            a11y_role::TEXT_INPUT,
            a11y_role::TEXT_RUN,
            a11y_role::STATIC_TEXT,
            a11y_role::CHECK_BOX,
            a11y_role::SLIDER,
        ] {
            let role = role_from_tag(tag).unwrap();
            assert_eq!(role_from_tag(tag).is_ok(), true);
            let _ = role;
        }
        assert!(role_from_tag(0).is_err());
        assert!(role_from_tag(999).is_err());
        for tag in [
            a11y_action::CLICK,
            a11y_action::FOCUS,
            a11y_action::BLUR,
            a11y_action::INCREMENT,
            a11y_action::DECREMENT,
            a11y_action::SET_VALUE,
            a11y_action::REPLACE_SELECTED_TEXT,
            a11y_action::SET_TEXT_SELECTION,
            a11y_action::SHOW_CONTEXT_MENU,
        ] {
            assert_eq!(action_to_tag(action_from_tag(tag).unwrap()), tag);
        }
        assert!(action_from_tag(0).is_err());
    }

    #[test]
    fn initial_tree_shape() {
        let update = initial_tree("Test Window");
        assert_eq!(update.nodes.len(), 1);
        assert_eq!(update.nodes[0].0, NodeId(0));
        assert_eq!(update.focus, NodeId(0));
        assert!(update.tree.is_some());
        let update_empty = initial_tree("");
        assert_eq!(update_empty.nodes.len(), 1);
    }

    #[test]
    fn handle_encoding_round_trip() {
        for (slot, generation) in [(0u64, 1u32), (5, 7), (63, 0xFFFFF)] {
            let handle = make_adapter_handle(slot as usize, generation);
            assert_eq!(adapter_handle_parts(handle), (slot as usize, generation));
        }
    }

    #[test]
    fn action_record_layout_pins() {
        assert_eq!(
            core::mem::size_of::<GpuiGoA11yActionRecord>(),
            GpuiGoA11yActionRecord::default().record_size as usize
        );
        assert_eq!(
            core::mem::size_of::<GpuiGoA11yNodeRecord>(),
            GpuiGoA11yNodeRecord::default().record_size as usize
        );
    }

    #[test]
    fn queued_action_handler_enqueues() {
        let queue = Arc::new(Mutex::new(VecDeque::new()));
        let mut handler = QueueingActionHandler { queue: queue.clone() };
        handler.do_action(ActionRequest {
            action: Action::SetValue,
            target_tree: TreeId::ROOT,
            target_node: NodeId(42),
            data: Some(ActionData::Value("hello".into())),
        });
        handler.do_action(ActionRequest {
            action: Action::Click,
            target_tree: TreeId::ROOT,
            target_node: NodeId(7),
            data: None,
        });
        let queue = queue.lock().unwrap();
        assert_eq!(queue.len(), 2);
        assert_eq!(queue[0].target_node, 42);
        assert_eq!(queue[0].action, a11y_action::SET_VALUE);
        assert_eq!(queue[0].data_tag, a11y_action_data::VALUE);
        assert_eq!(queue[0].value, "hello");
        assert_eq!(queue[1].action, a11y_action::CLICK);
        assert_eq!(queue[1].data_tag, a11y_action_data::NONE);
    }

    #[test]
    fn panic_probe_reports_containment() {
        // The export is unsafe (an extern "system" fn); the test-only
        // call needed an unsafe block (rev 16, ticket 50: the
        // pre-existing test-only compile error).
        assert_eq!(unsafe { a11y_panic_probe() }, a11y_status::ERR_PANIC);
    }
}
