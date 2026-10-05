# gpui-go native texture atlas service ABI (ticket10, reserved slot 6)

Status: implemented by `reference/native/src/atlas.rs`, installed in
reserved slot 6 of the bootstrap `GpuiGoAbiTable` (see
`reference/native/src/lib.rs`) and advertised with capability bit 6
(`glyph-atlas-d3d11`). The Go mirror lives in
`internal/native/atlas.go`; the typed gpui seam in `gpui/atlas.go`.

The semantics are the pinned Windows sprite atlas at commit
`254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`
(`crates/gpui_windows/src/directx_atlas.rs`), ported into this
artifact. The atlas owns no draw pipelines (the sprite draw pipeline
is a later ticket); this service is the CPU/GPU tile management the
paint path depends on.

## The design decisions (recorded per the ticket)

* **Allocation algorithm** — `etagere::BucketedAtlasAllocator`, pinned
  exactly at 0.2.15 (the renderer contract's lock pin). Tile ids are
  the serialized `etagere::AllocId` values, exactly as the pin's
  `TileId`.
* **Texture pools** — three separate pools (the pin's
  `DirectXAtlasState`): monochrome (`DXGI_FORMAT_R8_UNORM`, 1 byte per
  pixel), polychrome and subpixel (`DXGI_FORMAT_B8G8R8A8_UNORM`, 4
  bytes per pixel, different pixel meanings). New textures are
  1024x1024 by default, clamped to the D3D11 16384 maximum; freed
  texture slots go on the free list and are reused.
* **Device sharing** — the atlas is created from the RENDERER
  service's shared per-process device and immediate context
  (mirroring the pin where `DirectXAtlas::new` receives the renderer's
  device and context, directx_renderer.rs:349). The context is the
  multithread-protected immediate context the renderer service
  created; the atlas serializes every entry behind its own state
  mutex, and no lock is held across callbacks (the service has none).
  The lock order is atlas → renderer: the renderer's own entries never
  take the atlas lock, so the order is acyclic.
* **Upload validation** — the pin's `AtlasTextureKind::validate_upload`
  (positive dimensions, the exact channel count) runs before
  allocation or publication; dimensions above 16384 are rejected;
  uploads go through `UpdateSubresource` with the
  `width × bytes_per_pixel` row pitch.
* **Tile removal** — the pin's `remove`: deallocate the etagere slot,
  decrement the texture's live-key count, free-list the texture when
  unreferenced.
* **Device loss** — the pin's `handle_device_lost` (clear all pools
  and the tile map, rebind to the current device) plus an ABI
  generation counter: previously returned tile records become
  observably stale (queries miss) — the renderer contract's "device
  generation changes invalidate all physical tiles, even if a logical
  font/image resource remains alive".

## Deviations from the pin (each mechanical, none silent)

* The pin's `get_or_insert_with(key, &mut build)` laziness cannot
  cross an ABI (the renderer contract requires exactly that: "Go
  obtains raster bytes before calling the atlas insertion seam"). The
  ABI splits the operation: `atlas_insert(key, size, bytes)` performs
  the same map check first (a cached hit returns the existing tile
  with NO upload and NO new allocation) and the caller (Go) rasterizes
  before inserting.
* `atlas_query`, `atlas_texture_count`, `atlas_tile_count` and
  `atlas_generation` are ABI observability entries; the pin exposes
  `contains` only under test support. They read state.
* `get_texture_view` (the renderer's SRV lookup) is not exposed yet;
  it lands with the sprite draw pipeline ticket.
* The mutex is a `std::sync::Mutex` (the service's own), not the pin's
  `parking_lot::Mutex` — the semantics are identical here (no
  reentrancy, no callbacks under the lock).

## General rules

Same conventions as `RENDERER_ABI.md`: C ABI, `extern "system"`,
`#[repr(C)]` fixed-width records with self-checks, every export
wrapped in `catch_unwind` (a contained panic returns `101`). Atlas
handles encode `(slot index << 20) | slot generation` (the scene
encoding); stale after dispose or slot reuse; malformed handles are
typed errors. The upload byte buffer is borrowed for the call only and
consumed synchronously; no Go pointer is retained.

## Capacity bounds

- 8 live atlas instances (`MAX_ATLAS_SLOTS`).
- Tile dimensions ≤ 16384 (the D3D11 texture limit; the pin's own
  check).

## Records

All records `#[repr(C)]`; the tile record ends in a `record_size`
self-check.

| Record | Size / align | Fields (offsets) |
|---|---|---|
| `GpuiGoAtlasKeyRecord` | 88 / 8 | kind @0 (0 glyph; svg/image later tickets, rejected), padding @4, font_id @8, glyph_id @16, font_size_bits @20, subpixel_x @24, subpixel_y @28, scale_bits @32, style @36 (the 36-byte raster style record), format @72 (raster format tag), reserved[2] @76, record_size @84 |
| `GpuiGoAtlasTileRecord` | 40 / 4 | texture_index @0, texture_kind @4 (0 mono/1 poly/2 sub), tile_id @8, padding @12, bounds x/y/w/h @16..32, generation @32, record_size @36 |

The glyph key mirrors the pinned `AtlasKey::Glyph { params, format }`:
the full `RenderGlyphParams` content plus the rasterized format — the
cache identity of one glyph raster.

## Table

`GpuiGoAtlasTable`: 10 function pointers then 9 self-check scalars;
size 120 (116 bytes of fields rounded to the 8-byte alignment),
alignment 8. All entries return an i32 status.

| Slot | Entry | Signature |
|---|---|---|
| 0 | `create` | `(out_handle: *mut u64) -> i32` — binds to the renderer service's shared device |
| 1 | `dispose` | `(handle: u64) -> i32` |
| 2 | `insert` | `(handle, key: *const AtlasKeyRecord, width: i32, height: i32, bytes: *const u8, byte_count: u32, out_tile: *mut AtlasTileRecord) -> i32` — validates, allocates, uploads, publishes; a cached key returns its tile with no upload |
| 3 | `remove` | `(handle, key) -> i32` |
| 4 | `query` | `(handle, key, out_tile) -> i32` — 0 found, `-8` not found |
| 5 | `texture_count` | `(handle, kind: u32, out_count: *mut u32) -> i32` |
| 6 | `tile_count` | `(handle, out_count) -> i32` |
| 7 | `generation` | `(handle, out_generation: *mut u32) -> i32` |
| 8 | `notify_device_lost` | `(handle) -> i32` — clears pools + tiles, rebinds to the current device, bumps the generation |
| 9 | `panic_probe` | `() -> i32` (test-only) |

Self-check scalars: `service_version` (1), `size_of_table` (120),
`align_of_table` (8), `size_of_key_record` (88),
`align_of_key_record` (8), `size_of_tile_record` (40),
`default_atlas_size` (1024), `max_atlas_size` (16384),
`max_atlas_slots` (8).

## Status codes

0 ok; -1 stale handle; -2 bad handle (malformed, or the 8-slot limit);
-3 null argument; -4 bad value (key kind/tag/record, dimensions,
byte-count mismatch against the texture kind, reserved fields); -5
device failure (texture creation failed — the pin's `push_texture`
returning `None`); -8 not found (query miss); 101 panic contained.
