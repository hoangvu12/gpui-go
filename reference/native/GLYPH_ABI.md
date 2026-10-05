# gpui-go native glyph raster service ABI (ticket10, reserved slot 5)

Status: implemented by `reference/native/src/glyph.rs`, installed in
reserved slot 5 of the bootstrap `GpuiGoAbiTable` (see
`reference/native/src/lib.rs`) and advertised with capability bit 5
(`glyph-raster-dwrite`). The Go mirror lives in
`internal/native/glyph.go`; the typed gpui seam in `gpui/glyph.go`.

The semantics are the pinned Windows glyph rasterizer at commit
`254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`
(`crates/gpui_windows/src/font_rasterizer.rs`), **ported line-by-line**
into this artifact (the pinned type is `pub(crate)` in `gpui_windows`
and cannot be linked). The independent oracle is the reference harness
(`reference/harness/src/fixtures/glyph_raster.rs`, fixture family
`glyph-raster-v1`, recorded trace `fx-0005`), which contains its own
port of the same pinned source and exercises the pinned public raster
API.

## Face acquisition (the recorded port decision)

The pinned `WindowsGlyphRasterizer` implements
`gpui_ce_parley::GlyphRasterizer::rasterize(face: RasterFace, params)`:
the face (exact font bytes, face index, variation coordinates and
Fontique synthesis) is supplied by the Parley text stack itself
(`PlatformTextSystem::rasterize_glyph` →
`fonts.get(font_id).raster_face(font_id)`), and DirectWrite faces are
created from those exact bytes through `IDWriteInMemoryFontFileLoader`
— never through a system enumeration that could select a different
face. The service therefore constructs the process-global text stack
(the one `reference/native/src/text.rs` owns) WITH this rasterizer —
the exact pinned Windows platform construction — and the rasterize
entry calls the **public** `gpui::TextSystem::rasterize_glyph` on that
stack, the same call the pinned window paint path makes (including
`validate()` and the raster-metadata consistency check).

Ticket09 recorded the Swash-rasterizer substitution as a temporary
deviation ("the glyph-pixels ticket must revisit this"); this service
is that revisit. No text-geometry output observes the rasterizer (the
fx-0004 trace records no `is_emoji` flags and no raster output), so the
text-geometry gate stays green while FontIds and metrics stay identical
(same store, same interning).

## Formats supported (the pin's honest surface)

* `AlphaMask` (tag 0) — one coverage byte per pixel; grayscale mode
  through DirectWrite's `DWRITE_TEXTURE_ALIASED_1x1` alpha textures.
* `BgraSubpixelMask` (tag 1) — four bytes per pixel in blue, green,
  red, unused order; subpixel mode through DirectWrite's ClearType
  `DWRITE_TEXTURE_CLEARTYPE_3x1` textures expanded to BGRA with the
  pin's reversed RGB order.
* `BgraColor` (tag 2) — four bytes per pixel in blue, green, red,
  straight-alpha order; color mode through DirectWrite COLRv0 layer
  rasterization with gamma/contrast-corrected coverage, the tinted
  monochrome currentColor path, or the Swash fallback (below).

## Unsupported color behavior (preserved honestly)

`supports_color_glyph` reports exactly the pin's predicate
(`ColrV0 | Bitmap`). Per-glyph, the DirectWrite path returns typed
errors for **bitmap color glyphs** (CBDT/sbix), **COLRv1 paint
graphs** and **variable axes on legacy DirectWrite**, and the outer
rasterizer routes those to the retained `SwashGlyphRasterizer`
fallback (which still produces pixels). The capability predicate is
NOT a claim of COLRv1 support; the `backend_info` record reports the
actual selection. Other raster failures remain errors
(`ERR_RASTER`), never silent fallbacks.

## General rules

Same conventions as `TEXT_ABI.md`: C ABI, `extern "system"`,
`#[repr(C)]` fixed-width records, f32 values as u32 IEEE-754 bits,
every export wrapped in `catch_unwind` (a contained panic returns
`101`), no Go pointers retained (the style record is borrowed for the
call; the pixel dump buffer is borrowed for the call only).

Font ids are the pinned canonical `FontId` values and are validated
against the text stack's registry (unknown ids are `-2`).

## Capacity bounds

- Font size ≤ 4096 px, scale ≤ 128 (the pin accepts any finite size
  with scale > 0; the ABI adds input bounds so a caller cannot request
  unbounded native work).
- Raster pixel bytes ≤ 64 MiB (`MAX_RASTER_BYTES`); a larger raster
  returns `-5`.

## Records

All records `#[repr(C)]`, alignment 4, every f32 as u32 bits, every
service-written record ending in a `record_size` self-check.

| Record | Size / align | Fields (offsets) |
|---|---|---|
| `GpuiGoRasterStyleRecord` | 36 / 4 | mode @0 (0 gray/1 sub/2 color), color_effect_tag @4 (0 independent/1 preblend/2 dilation-rejected), color_r/g/b/a @8..24 (preblend bytes), dilation @24 (must be 0), reserved @28, record_size @32 |
| `GpuiGoRasterRecord` | 36 / 4 | format @0, bounds_x @4, bounds_y @8, bounds_w @12, bounds_h @16, width @20, height @24, pixel_count @28, record_size @32 |
| `GpuiGoRasterBackendRecord` | 24 / 4 | backend @0 (0 DirectWrite/1 Swash), variable_factory @4, gamma_bits @8, contrast_bits @12, system_subpixel @16, record_size @20 |

`bounds` is the pinned placement relative to the glyph's baseline
origin: origin `(left, top)` in device pixels (y negative above the
baseline). **The pinned `RasterizedGlyph` carries no advance field**
(the `CLARIFIED` deviation of TEXT_ABI.md applies here too: pen
advances are the shaping positions of the text-geometry service), and
**no stride field either**: the pin's pixel buffers are tightly packed
(one byte per pixel for `AlphaMask`, four for the BGRA formats), so
`pixel_count` fixes the byte count exactly and the row pitch is
`width × bytes-per-pixel`.

## Table

`GpuiGoGlyphTable`: 6 function pointers then 9 self-check scalars;
size 120 (116 bytes of fields rounded to the 8-byte alignment),
alignment 8. All entries return an i32 status.

| Slot | Entry | Signature |
|---|---|---|
| 0 | `prepare_style` | `(color_r/g/b/a_bits: u32 ×4, requested_mode: u32, out: *mut RasterStyleRecord) -> i32` — the pinned `PlatformTextSystem::prepare_raster_style` (the window paint path's style preparation; color mode quantizes the scene color into a Preblend effect) |
| 1 | `glyph_for_char` | `(font_id: u64, char_code: u32, out_glyph_id: *mut u32) -> i32` — the pinned glyph lookup; `0xFFFFFFFF` = the pinned `None` (missing glyph) |
| 2 | `rasterize` | `(font_id: u64, glyph_id: u32, font_size_bits: u32, subpixel_x: u32, subpixel_y: u32, scale_bits: u32, style: *const RasterStyleRecord, out: *mut RasterRecord, pixels: *mut u8, pixel_capacity: u32, out_needed: *mut u32) -> i32` |
| 3 | `recommended_mode` | `(out_mode: *mut u32) -> i32` — the pinned `recommended_rendering_mode` (Subpixel when the OS uses ClearType, else Grayscale; never PlatformDefault) |
| 4 | `backend_info` | `(out: *mut RasterBackendRecord) -> i32` — the rasterizer's construction facts (constructs the shared stack first so the reported facts are real) |
| 5 | `panic_probe` | `() -> i32` (test-only; panics inside catch_unwind and reports `101`) |

Self-check scalars: `service_version` (1), `size_of_table` (120),
`align_of_table` (8), `size_of_style_record` (36),
`size_of_raster_record` (36), `size_of_backend_record` (24),
`subpixel_variants_x` (4), `subpixel_variants_y` (1),
`max_raster_bytes` (64 MiB).

## Status codes

0 ok; -2 bad handle (font id not canonical/registered); -3 null
argument; -4 bad value (mode/effect enumerant, non-finite float,
subpixel variant, scale ≤ 0 or > 128, glyph id over 16 bits, record
size, reserved field); -5 raster failure (the pinned rasterization
returned an error — font missing from the store, DirectWrite/Swash
failure, or the byte-count bound); -7 capacity (pixel dump too small;
`out_needed` carries the byte count); 101 panic contained.

## The pixel dump protocol

`rasterize` performs the raster work either way and always writes the
metadata record; a too-small pixel capacity (`-7`) copies no pixel
bytes and reports the needed byte count — the caller retries with the
exact buffer (the Go mirror retries internally, so callers always
receive the full pixel buffer on success).

## Output-surface clarifications

- **No advance field**: the pinned `RasterizedGlyph` is bounds, size,
  format and pixels (the ticket sketch's "advance" is a shaping
  concept).
- **The style record is round-trippable**: `prepare_style` output
  decodes back into the pinned `PreparedRasterStyle` losslessly
  (except CoreGraphics `Dilation`, which this backend never produces
  and the ABI rejects).
- **`backend_info` before any raster** constructs the shared text
  stack, so its facts always describe the real construction (backend,
  gamma, enhanced contrast, the OS subpixel setting).
