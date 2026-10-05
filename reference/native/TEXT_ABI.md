# gpui-go native text geometry service ABI (ticket09, reserved slot 4)

Status: implemented by `reference/native/src/text.rs`, installed in
reserved slot 4 of the bootstrap `GpuiGoAbiTable` (see
`reference/native/src/lib.rs`) and advertised with capability bit 4
(`text-parley-0-11-1`). The Go mirror lives in `internal/native/text.go`.

The semantics are the pinned GPUI-CE text stack at commit
`254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`: shaping, run/fragment/glyph
production, caret, selection and hit-test geometry come from
`gpui_ce_parley`'s `ParleyTextSystem` (Parley 0.11.1 / Fontique 0.11.1 /
HarfRust 0.12.0 / Skrifa 0.44.0 / Swash 0.2.10), exactly as the pinned
Windows platform constructs it — including the DirectWrite rasterizer
since ticket10 (see the construction note below). The ENTRIES of this
service are CPU **geometry** only; glyph rasterization is the separate
glyph service (reserved slot 5, GLYPH_ABI.md), which calls the same
shared stack's public `rasterize_glyph`.

**Clarifications discovered during implementation are marked `CLARIFIED`
below; each restates what the pinned public API actually exposes.**

## Construction path (recorded per the ticket)

One process-global text stack, created lazily behind the service mutex
(the renderer service's device pattern). The construction mirrors
`crates/gpui_windows/src/platform.rs::WindowsPlatform::new` (lines
117-126 at the pin):

```text
ParleyTextSystem::new_with_rasterizer(SystemFonts::Load, "Segoe UI",
                                      WindowsGlyphRasterizer::new())
    .with_fallback_families(["Lilex", "IBM Plex Sans", "Arial"])
→ gpui::TextSystem::new(Arc::new(parley_text_system))
```

(The rasterizer is the ticket10 port in `crate::glyph`; ticket09
temporarily passed `SwashGlyphRasterizer::default()` — see the note
below.)

* `SystemFonts::Load` performs Fontique's real DirectWrite-backed system
  font enumeration (the ticket's "REAL Parley/DirectWrite backend" for
  discovery and shaping).
* **The rasterizer argument is the pinned `WindowsGlyphRasterizer`**
  since ticket10: `crate::glyph` ports the pinned DirectWrite
  rasterizer (`crates/gpui_windows/src/font_rasterizer.rs`) and the
  stack is constructed with it, exactly like the pinned Windows
  platform. Ticket09 had recorded a temporary deviation here (the
  portable `SwashGlyphRasterizer`, because the pinned type is
  `pub(crate)` in `gpui_windows` and rasterization was out of that
  ticket's scope; "the glyph-pixels ticket must revisit this"). The
  glyph service (reserved slot 5, ticket10) rasterizes through the
  SAME store that shapes the text — the pinned face-acquisition
  mechanism. No geometry entry observes the rasterizer (the fx-0004
  trace records no `is_emoji` flags and no raster output), so the
  text-geometry gate stays green while FontIds and metrics stay
  identical (same store, same interning). See GLYPH_ABI.md.
* `gpui::TextSystem` (the public wrapper) is constructed around the same
  instance; `text_font_names` is served through it. The other entries
  call the same `ParleyTextSystem` through its public
  `PlatformTextSystem` methods so failures are typed statuses rather
  than the wrapper's panics (`resolve_font` panics where
  `PlatformTextSystem::font_id` returns `Result`).

## Shaping entry point (the cache decision)

`WindowTextSystem::shape_text` delegates to `LineLayoutCache::
layout_wrapped_line`, which calls **`PlatformTextSystem::layout_text`**
directly for every wrapped, clamped or newline-containing document (and
`layout_line` makes the identical call on cache miss for unwrapped
single lines). This service calls `layout_text` directly with the same
inputs: the frame-scoped `LineLayoutCache` belongs to the Go port per
the windows platform contract ("Go retains GPUI style resolution, text
cache keys, measurement requests…"), and a native cache without
`finish_frame` lifecycle would grow unboundedly. The observable shaping
behavior is identical because it is the same call.

## General rules

Same conventions as `LAYOUT_ABI.md` / `SCENE_ABI.md`: C ABI,
`extern "system"`, `#[repr(C)]` fixed-width records, f32 values as u32
IEEE-754 bits, every export wrapped in `catch_unwind` (a contained panic
returns `-101`; the state mutex recovers, and a failed entry publishes
nothing — there is no latched failure state), no Go pointers retained
(caller text/runs/features/strings are copied into service-owned state
before any shaping; output records and dump buffers are borrowed for the
call only).

**Byte offsets are UTF-8 byte indices** — the pin's layout coordinate
system. UTF-16 (surrogate pairs, IME ranges) is the Go adapter's
concern and never crosses this ABI.

## Capacity bounds

- 256 live shaping handles (`MAX_SHAPING_SLOTS`; exceeding fails
  `text_shape` with -6 before any shaping work),
- 1 MiB text length, 1024 runs, 4096 feature records, 64 KiB packed
  strings per shape request,
- 1 MiB packed font-name dump.

## Handles

- **Shaping handle**: `(slot index << 20) | slot generation` (the layout
  engine's encoding). Stale after `text_dispose` or slot reuse; malformed
  (out-of-range slot, never-issued generation) is -2. `text_layout`
  re-shapes the SAME text under a new wrap/clamp constraint.
- **Font handle**: the pinned canonical `FontId` (bit 63 set, FontStore
  index below). Fonts are process-stable (the store only interns) and
  have no dispose. The service validates ids against its registry of
  resolved and shaping-discovered ids; unknown ids are -2 typed errors,
  never the store's `expect` panic.

## Records

All records `#[repr(C)]`; f32 fields as u32 bits; every service-written
record ends in a `record_size` self-check the Go side verifies.

| Record | Size / align | Fields (offsets) |
|---|---|---|
| `GpuiGoTextFeature` | 8 / 4 | tag @0 (4 ASCII chars, big-endian), value @4 (0/1, ≤ u16::MAX) |
| `GpuiGoTextFontRequest` | 28 / 4 | weight_bits @0 (f32), style @4 (0 normal/1 italic/2 oblique), feature_count @8, fallback_count @12, reserved[3] @16 (must be 0) |
| `GpuiGoTextFontRecord` | 40 / 8 | font_id @0 (u64 canonical), generation @8 (u64 catalog generation), weight_bits @12, style @16, feature_count @20, fallback_count @24, reserved @28, record_size @36 |
| `GpuiGoTextFontMetricsRecord` | 52 / 4 | units_per_em @0, ascent/descent/line_gap/underline_position/underline_thickness/cap_height/x_height @4..32, bbox x/y/w/h @32..48, record_size @48 (font units, exactly `gpui::FontMetrics`) |
| `GpuiGoTextRunRecord` | 48 / 4 | len @0, family_offset @4, family_len @8, fallbacks_offset @12, fallbacks_len @16, weight_bits @20, style @24, feature_offset @28, feature_count @32, letter_spacing_present @36 (0/1), letter_spacing_bits @40, reserved @44 (must be 0) |
| `GpuiGoTextLayoutRecord` | 56 / 4 | text_len @0, font_size_bits @4, wrap_present @8, wrap_bits @12, clamp_present @16, line_clamp @20, line_count @24, fragment_count @28, glyph_count @32, width_bits @36, ascent_bits @40, descent_bits @44, font_generation @48, record_size @52 |
| `GpuiGoTextLineRecord` | 36 / 4 | line_index @0, text_start @4, text_end @8, fragment_start @12, fragment_end @16, advance_width_bits @20, line_height_bits @24 (echo), baseline_bits @28, record_size @32 |
| `GpuiGoTextCaretRecord` | 56 / 4 | index @0, affinity @4 (0 downstream/1 upstream), present @8, x/y/w/h_bits @12..28, cluster_before_present/start/end @28..40, cluster_after_present/start/end @40..52, record_size @52 |
| `GpuiGoTextHitRecord` | 16 / 4 | index @0, affinity @4, inside @8 (1 = Ok, 0 = the pinned Err edge caret), record_size @12 |
| `GpuiGoTextSelectionRecord` | 20 / 4 | x/y/w/h_bits @0..16, record_size @16 |
| `GpuiGoTextClusterRecord` | 16 / 4 | present @0, start @4, end @8, record_size @12 |
| `GpuiGoTextFragmentRecord` | 32 / 8 | font_id @0 (u64 canonical), font_size_bits @8, x_start_bits @12, x_end_bits @16, glyph_start @20, glyph_count @24, record_size @28 |
| `GpuiGoTextGlyphRecord` | 20 / 4 | glyph_id @0, x_bits @4 (line-local), y_bits @8 (baseline-relative), is_emoji @12, record_size @16 |

## Table

`GpuiGoTextTable`: 15 function pointers then 20 self-check/capacity
scalars; size 200, alignment 8. All entries return an i32 status.

| Slot | Entry | Signature |
|---|---|---|
| 0 | `font_names` | `(buf: *mut u8, capacity: u32, out_count: *mut u32, out_needed: *mut u32) -> i32` |
| 1 | `font_resolve` | `(req: *const FontRequest, family: *const u8, family_len: u32, features: *const Feature, feature_count: u32, fallbacks: *const u8, fallbacks_len: u32, out: *mut FontRecord) -> i32` |
| 2 | `font_metrics` | `(font_id: u64, out: *mut MetricsRecord) -> i32` |
| 3 | `shape` | `(text: *const u8, text_len: u32, runs: *const RunRecord, run_count: u32, features: *const Feature, feature_count: u32, strings: *const u8, strings_len: u32, font_size_bits: u32, wrap_bits: u32, wrap_present: u32, line_clamp: u32, clamp_present: u32, out_handle: *mut u64) -> i32` |
| 4 | `layout` | `(handle: u64, wrap_bits: u32, wrap_present: u32, line_clamp: u32, clamp_present: u32) -> i32` |
| 5 | `dispose` | `(handle: u64) -> i32` |
| 6 | `layout_info` | `(handle: u64, out: *mut LayoutRecord) -> i32` |
| 7 | `line_count` | `(handle: u64, out: *mut u32) -> i32` |
| 8 | `line` | `(handle: u64, index: u32, line_height_bits: u32, out: *mut LineRecord) -> i32` |
| 9 | `caret` | `(handle: u64, index: u32, affinity: u32, line_height_bits: u32, out: *mut CaretRecord) -> i32` |
| 10 | `hit_test` | `(handle: u64, x_bits: u32, y_bits: u32, line_height_bits: u32, out: *mut HitRecord) -> i32` |
| 11 | `selection_rects` | `(handle: u64, start: u32, end: u32, line_height_bits: u32, rects: *mut SelectionRecord, capacity: u32, out_needed: *mut u32) -> i32` |
| 12 | `cluster` | `(handle: u64, index: u32, affinity: u32, side: u32, out: *mut ClusterRecord) -> i32` |
| 13 | `fragments` | `(handle: u64, records: *mut FragmentRecord, capacity: u32, out_needed: *mut u32) -> i32` |
| 14 | `glyphs` | `(handle: u64, records: *mut GlyphRecord, capacity: u32, out_needed: *mut u32) -> i32` |

Self-check scalars: `service_version` (1), `size_of_table` (200),
`align_of_table` (8), `size_of_font_request` (28), `size_of_font_record`
(40), `align_of_font_record` (8), `size_of_metrics_record` (52),
`size_of_run_record` (48), `size_of_feature_record` (8),
`size_of_layout_record` (56), `size_of_line_record` (36),
`size_of_caret_record` (56), `size_of_hit_record` (16),
`size_of_selection_record` (20), `size_of_cluster_record` (16),
`size_of_fragment_record` (32), `align_of_fragment_record` (8),
`size_of_glyph_record` (20), `max_shaping_handles` (256),
`max_text_bytes` (1 MiB).

## Status codes

0 ok; -1 stale handle; -2 bad handle (malformed shaping handle, or a
font id outside the canonical-bit/registry rules); -3 null argument; -4
bad value (run coverage, byte boundary, feature tag/value, enumerant,
non-finite float, bound, reserved field); -5 font unresolved (the whole
pinned fallback chain failed — with the pinned service fallbacks ending
in Arial this is effectively unreachable on a normal Windows install;
see the tests for the fallback-resolution behavior); -6 shaping-handle
limit; -7 capacity (dump too small; `out_needed` carries the required
count/byte count); 101 panic contained.

## Entry semantics

- `font_names`: the pinned `all_font_names` (catalog families +
  `.SystemUIFont`, sorted, deduplicated) as a packed dump of
  NUL-terminated UTF-8 names. Capacity probe: call with a null buffer /
  zero capacity → -7 with `out_count` (families) and `out_needed`
  (bytes) written, nothing copied.
- `font_resolve`: builds the pinned `Font` descriptor (family, weight,
  style, features, fallbacks) and resolves it through
  `PlatformTextSystem::font_id` (`resolve_canonical_font`: family list,
  descriptor fallbacks, then the service fallbacks). Output: the
  canonical FontId, catalog generation, descriptor echo. **`CLARIFIED`**
  (identity record): the pinned public surface resolves a descriptor to
  the canonical `FontId`; face index, data identity and variation axes
  are `pub(crate)` in `gpui_ce_parley` and not observable. The
  descriptor bytes are the caller's own request and are not echoed.
- `font_metrics`: the full `gpui::FontMetrics` (font units) for any id
  the service registered — resolved ids AND every font id Parley
  selected during shaping (fragment font ids are registered at shape
  time, since character-level fallback can pick faces the caller never
  resolved).
- `shape`: validates and copies the text, run table, feature records
  and packed strings; pre-resolves every run's font (so an unresolvable
  family is -5 rather than the pinned `expect` panic); calls
  `layout_text`; registers fragment font ids; publishes the shaping
  handle only on success. **`CLARIFIED`** (validation): the pin assumes
  gpui-validated input; the ABI turns the assumptions into typed errors
  (runs must exactly cover the text and end on UTF-8 boundaries,
  feature tags are 4 printable ASCII chars, values fit u16 — the
  adapter's `Tag::parse`/narrowing; floats must be finite; degenerate
  but finite inputs the pin accepts, like font size 0, stay accepted).
- `layout`: re-shapes the stored text/runs/font-size under the new wrap
  width / line clamp (the ticket's `shape_handle.layout(wrap)`). Wrap
  widths must be finite and >= 0; clamps >= 1. **`CLARIFIED`**
  (alignment): the pin has no alignment in shaping — `TextAlign` shifts
  rows at paint time (`aligned_visual_origin_x`), which is Go-owned
  paint; the re-layout entry takes wrap/clamp only.
- `layout_info`: `LineLayout` facts — text length, font size, wrap/clamp
  echo, line/fragment/glyph counts, `width` (the widest line's advance),
  `ascent`/`descent` (the document maxima), catalog generation.
- `line`: `VisualLine { text_range, fragment_range, advance_width }`
  plus the row's placement facts derived exactly as the pinned
  `paint_visual_text` does: row i occupies
  `[i*line_height, (i+1)*line_height)` and
  `baseline = (line_height - ascent - descent)/2 + ascent` from the row
  top. **Note**: in multi-paragraph documents the pinned merge keeps the
  paragraph separator bytes in the paragraph's last visual row
  ("Keep separators in document indices without adding native trailing
  rows"), so a row's `text_end` can include its trailing `\n`.
- `caret`: `PlatformTextLayout::caret_bounds` (full bounds; `present` 0
  when the pin returns `None`) plus the two adjacent logical clusters
  (`logical_cluster_before`/`after`). **`CLARIFIED`** (clusters): in
  this pin logical clusters are **graphemes** (the `paragraphs.rs`
  grapheme indices), i.e. one backend-defined caret step.
- `hit_test`: `caret_from_pixel_point` — `inside` 1 for the pinned `Ok`
  (point inside a visual row), 0 for the pinned `Err` (the edge caret).
- `selection_rects`: `selection_bounds(byte_range, line_height)` — the
  pinned visual-order rectangles (empty range → none; height exactly
  `line_height`).
- `cluster`: `logical_cluster_before` (side 0) / `after` (side 1).
- `fragments` / `glyphs`: bulk dumps with the capacity protocol
  (`out_needed` always written; too-small capacity copies nothing and
  returns -7). Fragment records carry the resolved canonical FontId,
  font size, x range and the glyph dump slice; glyph records carry
  glyph id, line-local x, baseline-relative y and the color-artwork
  flag.

## Output-surface clarifications (the ticket sketch vs the pin)

- **Per-glyph advances**: `ShapedGlyph` carries `id`, `position`,
  `is_emoji` — no advance field. The pen advance between consecutive
  glyphs is the difference of dumped positions; each fragment's
  `x_range` carries the run extent. The service exposes exactly what
  the pin exposes.
- **Run direction**: `LineLayout`/`PaintFragment` carry no direction.
  Bidi behavior is observable through the exposed caret/hit-test
  geometry (affinities, positions), which is what the pin's consumers
  use.
- **Cluster glyph ranges**: the pin's clusters are caret stops (byte
  ranges), not glyph mappings; no cluster→glyph correspondence is part
  of the public output.

## Fallback behavior (observed on this machine)

The pinned construction appends the service fallback families (Lilex,
IBM Plex Sans, Arial) to every resolve chain, so an unknown family name
resolves through them (Arial on a default Windows install). The Rust
tests cover the fallback-resolution path; `-5` fires only when the whole
chain fails.
