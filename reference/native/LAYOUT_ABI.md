# gpui-go native layout service ABI (v1)

Design contract for ticket06. This document is the authoritative interface
between the Rust native layout service (reference/native) and the Go layout
adapter (gpui). Implementations must follow it exactly; deviations require
recording in the ticket evidence. Source of truth for semantics:
[layout contract](../../docs/layout-contract.md) and the pinned CE sources
(`crates/gpui/src/taffy.rs`, `crates/gpui/src/style.rs`,
`crates/gpui/src/util.rs` at `254b5dbd…`).

**Clarified during implementation (ticket06): the enumerant and record
details below were tightened where the original sketch was looser than the
pinned sources; every clarification is marked `CLARIFIED`.** No semantics
were invented: each change only restates what the pinned gpui adapter
forwards.

## General rules

- C ABI, `extern "system"`, `#[repr(C)]`, fixed-width types only (u32/u64/i32/f32 bits). No Rust structs by value across the boundary, no Rust Vec/String/Option/closures, no bools (u32 0/1), no Go pointers retained by native code.
- All floating-point values cross as **u32 IEEE-754 bit patterns** (`.to_bits()`/`f32::from_bits`), except inside pointer-addressed records where they are plain `f32` fields in fixed-layout structs (Go reads them with unsafe at explicit offsets; both sides assert `size_of`/`align_of` self-check fields).
- Every entry returns a `Status` (i32): 0 ok; negative = caller/argument error (distinct codes); 100+ = internal failure (panic contained, engine marked for reset). Never a silent wrong result.
- Native IDs (`EngineHandle`, `NodeHandle`, u64) are validated opaque handles. Engine handles encode `(slot index << 20) | slot generation`: they stay valid across engine **reset** and become stale after **dispose** or slot reuse. Node handles encode `(node generation << 44) | node sequence`: they become stale after engine reset, subtree removal or node disposal. Use of a stale handle returns `GPUI_GO_LAYOUT_ERR_STALE_HANDLE`; a malformed handle (out-of-range slot, never-issued sequence) returns `GPUI_GO_LAYOUT_ERR_BAD_HANDLE`. `CLARIFIED`: the handle-generation split (engine handles survive reset because reset must return a usable engine; node handles do not, matching "Layout IDs include engine identity and generation and become unusable on engine reset").
- Rust panics are contained with `catch_unwind` (unwinding enabled in the crate profile); a panicking export marks the engine failed and returns the panic status.
- The service occupies **reserved slot 1** of the existing `GpuiGoAbiTable` (the bootstrap buffer round trip keeps its dedicated first function slot; reserved slot 0 stays null). Slot 1 holds the address of the static `GpuiGoLayoutTable` (see below). Capability bit 1 = `layout-taffy-0-13-0`. The new artifact is a new artifact identity (per the distribution contract) and must be re-measured and re-embedded.
- `CLARIFIED`: an engine is **busy** from `layout_compute` entry until return. Every other engine operation (node ops, geometry queries, dump, reset, dispose) checks the busy flag *before* taking the engine state lock, so same-engine re-entry — including from inside a measure trampoline — is rejected with `GPUI_GO_LAYOUT_ERR_ENGINE_BUSY` instead of deadlocking. The Go adapter additionally rejects same-engine re-entry in its own per-engine busy guard before native entry (the contract's "reject before native entry"); the native flag is defense in depth.

## Records

```c
// Length value: tag + value bits. Tags:
// 0 = Auto (bits ignored), 1 = definite device pixels (Go already converted
// logical px -> device px with the pinned rounding rules), 2 = percent
// (fraction of parent, f32 bits), 3 = not-present (field omitted; native uses
// taffy defaults). Absent (3) vs explicit zero (1 with bits 0) are distinct.
// CLARIFIED: tags 4 (min-content) and 5 (max-content) are reserved and
// REJECTED as caller errors — the pinned gpui Length/DefiniteLength enums
// have no such keywords, so accepting them would invent broader CSS.
typedef struct { u32 tag; u32 bits; } GpuiLen;

// Edges: four GpuiLen in order top, right, bottom, left.
typedef struct { GpuiLen top, right, bottom, left; } GpuiEdges;

// Size: two GpuiLen (width, height).
typedef struct { GpuiLen width, height; } GpuiSizeL;
```

`GpuiGoLayoutStyleRecord` carries exactly the GPUI-forwarded style set (see
`crates/gpui/src/taffy.rs::into_taffy_style`). It is a flat record of u32
fields (336 bytes, alignment 4, trailing `record_size` self-check); no
pointers. Field groups, in order:

- display (u32: 0 flex, 1 block, 2 grid, 3 none — `Inline`→block, `InlineFlex`→flex mapped by the Go adapter before the record);
- position (0 relative, 1 absolute) + inset `GpuiEdges` (`Length`: auto/definite/percent/absent);
- overflow x/y (u32, `CLARIFIED`: 0 visible, 1 clip, 2 hidden, 3 scroll — the pinned gpui `Overflow` source order; the original sketch omitted Clip) + scrollbar_width `GpuiLen` (tag 1 definite device px or tag 3 absent; tags 0/2 are errors because gpui's `AbsoluteLength` has no auto/percent);
- size/min/max `GpuiSizeL` (`Length`) + aspect_ratio (u32 present flag + f32 bits);
- margin `GpuiEdges` (`Length`); padding `GpuiEdges` (`DefiniteLength`: definite device px, percent or absent; auto is a caller error); border_widths (top/right/bottom/left, u32 f32 bits each, device px, already stroke-snapped by the Go adapter with the pinned `round_stroke_to_device_pixel`);
- align_items/align_self (u32 present + u32 value; values 0 start, 1 end, 2 flex-start, 3 flex-end, 4 center, 5 baseline, 6 stretch — gpui `AlignItems` source order); align_content/justify_content (u32 present + u32 value; 0 start, 1 end, 2 flex-start, 3 flex-end, 4 center, 5 stretch, 6 space-between, 7 space-evenly, 8 space-around — gpui `AlignContent` source order);
- gap `GpuiSizeL` (`DefiniteLength`);
- flex_direction (0 row, 1 column, 2 row-reverse, 3 column-reverse); flex_wrap (0 no-wrap, 1 wrap, 2 wrap-reverse); flex_basis `GpuiLen` (`Length`); flex_grow/flex_shrink (u32 present + f32 bits);
- grid: `CLARIFIED` — each of grid rows/columns is gpui's `Option<GridTemplate>` encoded inline as (u32 present, u32 repeat 0..=65535, u32 min_size: 0 zero, 1 min-content, 2 max-content). The native side builds exactly `repeat(<repeat>, minmax(<min>, 1fr))` with `min` = `length(0.0)` / `min_content()` / `max_content()`, as the pinned `to_grid_repeat` does. (The original sketch's "count + pointer to a template record array" could not express `repeat(N, minmax(min, 1fr))` with its five scalar kinds, and gpui's forwarded set has no grid auto rows/columns — those stay at Taffy defaults.)
- grid placement: `CLARIFIED` — one u32 present flag for gpui's `Option<GridLocation>`, then four placements (row start/end, column start/end), each (u32 kind: 0 auto, 1 line, 2 span + u32 value; line values are an i16 bit pattern, span values 0..=65535). The native side maps them through `GridPlacement::from_line_index`/`from_span`.
- trailing `u32 record_size` self-check.

The Rust side validates every tag, count and bound before use; unknown values
are caller errors (`GPUI_GO_LAYOUT_ERR_BAD_VALUE`).

```c
// Available space per axis: tag 0 = definite (bits), 1 = min-content, 2 = max-content.
typedef struct { u32 tag; u32 bits; } GpuiAvail;
typedef struct { GpuiAvail width, height; } GpuiAvailSize;

// Measurement request/response (native-owned records, Go writes the response
// in place during the trampoline call). CLARIFIED: every definite value in
// both directions is DEVICE PIXELS (Taffy's units): the native side has no
// scale factor, so the Go adapter converts logical <-> device around the
// trampoline and applies the pinned measurement snapping (clamp logical
// >= 0, multiply by scale, ceil) before writing the response.
typedef struct {
    u64 engine; u64 node; u64 callback_token; u64 compute_token;
    u32 known_width_present, known_height_present;   // 0/1
    u32 avail_width_tag, avail_height_tag;           // GpuiAvail tags
    u32 known_width_bits, known_height_bits;
    u32 avail_width_bits, avail_height_bits;
    u32 reserved[2];
} GpuiMeasureRequest;   // size 72, align 8
typedef struct {
    i32 status;            // 0 ok; 1 callback failed (panic recovered, latched); 2 invalid token
    u32 width_bits, height_bits;  // device pixels
} GpuiMeasureResponse;  // size 12, align 4
```

## Service table

`CLARIFIED` (structure; the function list was already authoritative): slot 1
of `GpuiGoAbiTable` holds the address of a static
`#[repr(C)] GpuiGoLayoutTable` (160 bytes, alignment 8): 14 function
pointers (all `extern "system"`, each returning `i32`), then 11 self-check
scalars — `service_version` (1), `size_of_table`, `align_of_table`,
`size_of_style_record`, `size_of_layout_record`, `size_of_avail`,
`size_of_avail_size`, `size_of_measure_request`,
`align_of_measure_request`, `size_of_measure_response`,
`align_of_measure_response`. The Go loader copies the table out of DLL
memory, verifies `service_version` and every record size/alignment against
its mirrors, and rejects null function slots, before any call.

```c
// engine lifecycle
i32 layout_engine_create(u64* out_engine);            // fresh empty tree, generation 1
i32 layout_engine_reset(u64 engine);                  // clears tree, bumps node generation (node handles stale; engine handle stays valid)
i32 layout_engine_dispose(u64 engine);                // frees; pending callbacks cannot run after (destruction is deferred until in-flight calls return)

// nodes (styles arrive in device pixels; percentages as fractions)
i32 layout_node_create(u64 engine, const GpuiGoLayoutStyleRecord* style,
                       const u64* children, u32 child_count, u64* out_node);
i32 layout_node_set_style(u64 engine, u64 node, const GpuiGoLayoutStyleRecord* style);
i32 layout_node_set_measure(u64 engine, u64 node, u64 callback_token); // measured leaf
i32 layout_node_remove_subtree(u64 engine, u64 node);

// compute
i32 layout_compute(u64 engine, u64 root, const GpuiAvailSize* available,
                   u64 compute_token, u32* out_flags);
// out_flags bit 0: measured callbacks were invoked (diagnostics).
// The engine is busy from entry until return; a same-engine nested
// layout_compute returns GPUI_GO_LAYOUT_ERR_ENGINE_BUSY.

// geometry (post-compute; LayoutRecord fields in device pixels)
i32 layout_node_layout(u64 engine, u64 node, GpuiGoLayoutRecord* out);
i32 layout_node_parent(u64 engine, u64 node, u64* out_parent);      // 0 = root
i32 layout_node_child_count(u64 engine, u64 node, u32* out_count);
i32 layout_node_child(u64 engine, u64 node, u32 index, u64* out_child);

// bulk dump for oracles: arrays copied into caller-provided buffers
i32 layout_dump(u64 engine, u64 root, u64* node_ids, GpuiGoLayoutRecord* records,
                u32 capacity, u32* out_count);  // preorder walk incl. root; too-small
                                                    // capacity writes the needed count and
                                                    // returns GPUI_GO_LAYOUT_ERR_CAPACITY

// trampoline: registered once per process before any measured compute
i32 layout_set_trampoline(Trampoline trampoline);
// where Trampoline = i32 (*)(GpuiMeasureRequest* req, GpuiMeasureResponse* resp)
```

`GpuiGoLayoutRecord` mirrors taffy's `Layout` for the fields the GPUI adapter
consumes (72 bytes, alignment 4): order (i32), location x/y, size w/h,
content_size w/h, scrollbar_size w/h, border top/right/bottom/left, padding
top/right/bottom/left (all f32, unrounded device pixels — Taffy rounding is
disabled; the pinned Go-side snapping consumes them), plus a trailing
`u32 record_size`.

## Status codes

`CLARIFIED` (names and values; the sign convention was already
authoritative):

| Value | Name | Meaning |
|---|---|---|
| 0 | `GPUI_GO_LAYOUT_OK` | success |
| -1 | `GPUI_GO_LAYOUT_ERR_STALE_HANDLE` | handle encoded a past generation |
| -2 | `GPUI_GO_LAYOUT_ERR_BAD_HANDLE` | handle malformed or never issued |
| -3 | `GPUI_GO_LAYOUT_ERR_NULL_ARG` | required pointer argument was null |
| -4 | `GPUI_GO_LAYOUT_ERR_BAD_VALUE` | invalid tag/enumerant/bound/record-size in a record |
| -5 | `GPUI_GO_LAYOUT_ERR_NODE_ATTACHED` | node creation given an already-attached (or duplicated) child |
| -6 | `GPUI_GO_LAYOUT_ERR_ENGINE_BUSY` | engine is computing; rejected before the engine state lock |
| -7 | `GPUI_GO_LAYOUT_ERR_NO_TRAMPOLINE` | measured node reached with no registered trampoline |
| -8 | `GPUI_GO_LAYOUT_ERR_CAPACITY` | dump capacity too small; `out_count` carries the need |
| 100 | `GPUI_GO_LAYOUT_ERR_ENGINE_FAILED` | engine is marked failed; reset or dispose required |
| 101 | `GPUI_GO_LAYOUT_ERR_PANIC` | native panic contained during this call; engine now failed |
| 102 | `GPUI_GO_LAYOUT_ERR_CALLBACK` | measure trampoline failed during this compute; latched; engine now failed |

## Measurement trampoline semantics

- The Go side registers ONE fixed callback (`syscall.NewCallback`) whose Go
  function recovers panics (defer/recover → response status 1, latched),
  looks up the `callback_token` in its registry, and writes the response. It
  never blocks and never calls back into the busy engine.
  `CLARIFIED`: the Go measure function signals failure by panicking (the
  trampoline recovers it and reports status 1); there is no error return on
  the happy path. The trampoline-level return value AND the response's
  `status` field must both be 0 for success; non-finite returned sizes are
  treated as callback failures. `syscall.NewCallback` only accepts
  word-sized results, so the Go callback is declared with a `uintptr` result
  that carries the status in the low 32 bits — the Win64 ABI reads an i32
  return from EAX either way.
- The native measure closure calls the trampoline synchronously during
  `layout_compute`, on the same thread, with native-owned records; the
  native side validates the response, latches callback failure for the rest
  of that compute, suppresses further user callbacks in it, and returns
  `GPUI_GO_LAYOUT_ERR_CALLBACK` from `layout_compute` (a sentinel zero size
  returned to taffy is internal plumbing, never a successful layout). The
  engine is marked failed after a callback failure, matching "Mark failed
  engine state for reset/disposal after return".
- Same-engine reentry is rejected BEFORE native entry by the Go adapter's
  per-engine busy guard; the native engine busy flag is defense in depth.
  Independent-engine nested compute is allowed: the Go adapter saves/restores
  the active compute context around nested calls.
- Callback tokens are generation-checked (the Go registry binds each token to
  the engine identity — slot and slot generation — it was registered for;
  requests for a different engine fail with status 2). Registry entries are
  removed only after the owning native call has finished (Go Reset/Dispose,
  which are rejected while the engine is busy, purge them). Forced GC must
  never observe a dangling native reference (no Go pointers cross the
  boundary at all).
- `CLARIFIED` (Go-side hazard): a trampoline callback runs Go code and can
  trigger a goroutine stack copy (growth/shrink, e.g. under forced GC). Any
  Go stack slot the native side writes AFTER callbacks have run would
  silently go stale; the Go adapter must therefore place such out-records
  (`out_flags`) in non-moving memory (a pooled heap allocation). This was
  observed and fixed in the ticket06 Go layer; record it here so future
  service tables with callback-visible out-parameters keep the rule.

## Taffy integration rules

- `taffy = "=0.13.0"` from crates.io, default features retained (std,
  taffy_tree, flexbox, grid, block_layout, float_layout, calc, content_size,
  detailed_layout_info as the 0.13.0 default set resolves them); record the
  resolved feature graph in the manifest/evidence. **Rounding is disabled**
  following the pinned `crates/gpui/src/taffy.rs` configuration
  (`TaffyTree::disable_rounding`; `layout()` then reports the unrounded
  layout).
- The style translation follows the pinned `into_taffy_style` exactly; fields
  GPUI omits keep taffy defaults; do not invent broader CSS.
- Compute happens in device pixels; `GpuiAvail` definite values are device
  px from the Go adapter (Go multiplies logical by scale with the pinned
  rounding before the call).
- `CLARIFIED`: the measured-leaf mechanism is taffy 0.13's node-context
  store (`new_leaf_with_context` / `set_node_context`); the per-node context
  is the "measure closure map". The pinned gpui measure closure returns the
  taffy default size for nodes without a context, and the native bridge
  returns a zero size while a failure is latched.
- Panics inside taffy (`expect`) are contained by the export boundary and
  mark the engine failed, matching the pinned source's expect-on-error
  semantics without crashing the caller.
