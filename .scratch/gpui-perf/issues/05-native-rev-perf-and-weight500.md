# Native rebuild: bulk scene insert, low-latency present, weight-500 face

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: gpui-perf W3
Blocked by: none

## Question

This is one user-approved native rebuild (the next revision after the embedded one). It has four parts.

1. **Weight 450-599 without fake bold.**
   - Today 500 maps to Regular plus DirectWrite bold simulation. The chain is fontique 0.11.1 font.rs:97-103 and matching.rs:386, the gpui_ce_parley catalog.rs:188 and text_system.rs:713-717/1211-1216 carrying the flag, then native glyph.rs:1192-1199.
   - Stop the synthesis for requested weights below 600 in the native font-resolution layer, on both the resolve and the per-run shaping paths.
   - Do not edit the oracle `ce-source`; patch in the native crate or a vendored copy, and document it as a deviation.
   - Evidence and probes are in `%TEMP%\boldprobe\out\`.
2. **Bulk scene insert.**
   - Today there's one `SyscallN` per primitive (`internal/native/scene_windows.go:47-110`).
   - Add an entry that takes a packed buffer of primitives in one call. On the Go side, buffer the primitives per frame and flush once before `DrawScene`.
   - Ordering and the scene output must be identical (scenespec).
3. **Low-latency present.**
   - Today the swapchain is flip-sequential with 3 buffers, `Present(0,0)`, no waitable object and no `SetMaximumFrameLatency` (`reference/native/src/renderer.rs:1070-1085`, `draw.rs:2436`).
   - Evaluate and implement `DXGI_SWAP_CHAIN_FLAG_FRAME_LATENCY_WAITABLE_OBJECT` with maximum latency 1. The frame loop waits on the waitable object before building a frame, alongside the existing DwmFlush waker in `gpui/frameloop_windows.go`.
   - Don't add any idle cost: still no work when nothing is dirty.
4. **Present1 dirty rects.** Optional: implement only if correct under the flip model, otherwise record why not.

Rebuild per `reference/README.md` "Build and measure": crt-static, then `tools/measure` to rotate the embedded DLL and manifest, then the revision, mask and SHA assertions. The CE source is at `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go\reference\ce-source`; copy it in, never modify the original.

## Comments

## Answer

All four parts are settled: three are implemented in one rebuild, and Present1 dirty rects were evaluated and not implemented. The embedded DLL was revision 12 (`lib.rs`, the manifest and `reference/README.md` agree; the handoff's "rev 11" was stale), so this is **revision 13**: SHA-256 `0b6e9265e0d519ecaf56aba6f6905efab0bdc7be936d6085ea42cab28e234450`, 18,877,952 bytes, static CRT, mask `0xFFF`. Evidence: [`evidence/perf/ticket05-native-rebuild.json`](../../../evidence/perf/ticket05-native-rebuild.json), with images under `evidence/perf/ticket05/`.

### 1. Weight 450-599 without fake bold (done)

- The fix lives in the native text service (`reference/native/src/text.rs` `effective_weight`, `glyph.rs` `probe_face`). `ce-source` and fontique are untouched.
  - A workspace `[patch]` of fontique would also have changed the oracle harness, which links fontique too.
- For a requested weight below 600, the service resolves the font and checks whether Fontique selected bold synthesis.
  - `gpui_ce_parley`'s font store is private, so the check is a probe: it asks the injected rasterizer for glyph 0 with a thread-local flag set. The rasterizer records the `RasterFace` and returns without drawing.
- If synthesis was selected, the request becomes the face's own OS/2 weight. That selects the same face with no synthesis, which is verified by face hash and index, then cached per family, fallbacks, weight and style. The cache is cleared by `font_add`.
- The rewrite runs on both paths: `resolve_font_descriptor` and every shaping run in `shape_document`.
- Faces with a `wght` axis and requests from 600 up are unchanged.
- Recorded deviation from the pin. Fontique would emit the same synthesis for requests from 600 up; this only follows CSS's "no synthesis below 600".
- Result at 14px: 500 now uses the Regular FontId with ink 298.6, the same as 400. Before, it was synthesized bold with ink 473.9, heavier than the real Semibold at 399.2.
  - Against browser-like DirectWrite, the ink ratio for 500 drops from 1.84 to 1.16, the same as 400.
  - 300-450 and 550-900 are unchanged.

### 2. Bulk scene insert (done)

- Scene service v4 adds `insert_batch`, a packed `[kind, words, record…]` stream applied under one lock with the single-op validation and state code.
- `internal/native/scene_batch.go` buffers inserts, layer push/pop and floor raises in a fixed 64 KiB buffer from a free list. It flushes before any read, finish, replay or path insert.
- Stateless record checks run at the insert call, so malformed records still fail immediately.
  - Deviation: state errors (limit, stale or failed scene) surface at the next flush, and the ops after the failing one are dropped.
- `TestSceneBatchMatchesSingleOps` compares the batched scene with a scene built through the single-op entries: identical len, meta, dumps and plan.
- Results:

| scene | native calls/frame | ms/frame | allocs/frame |
|---|---|---|---|
| 1,000 divs | 8,037 → 3,048 | 10.2 → 8.5 | unchanged |
| text page | 129,292 → 110,914 | 190 → 183 (noisy) | unchanged |

The remaining text-page calls are per-glyph text and glyph calls (ticket 02).

### 3. Low-latency present (done)

- Every swap chain uses `FRAME_LATENCY_WAITABLE_OBJECT` with `SetMaximumFrameLatency(1)`; `ResizeBuffers` passes the same flag, and the device-loss rebuild takes a new waitable.
- The new `surface_frame_latency_handle` returns a duplicated handle that Go waits on outside every lock.
- The frame pacer (`gpui/frameloop_windows.go`) waits on the waitable of a pending window's surface, which is registered by `Renderer.GetOrCreateSurface`.
  - The first frame after idle starts immediately, instead of waiting for DwmFlush first.
  - Continuous frames still wait for the vblank. With `Present(0,0)` the waitable bounds only the GPU queue: alone, it ran an animation unpaced at 0.23 ms/frame.
- The pacer now exits before waiting when nothing is pending, so idle costs nothing.
- Real-window results (`TestFrameLatencyWaitableRealWindow`, 239 Hz display):
  - Notify to present from idle: median 0.54 ms, against 4.4-4.8 ms with DwmFlush (p90 up to 17.5 ms).
  - Animation: 4.18 ms/frame.
  - Idle: 0 presents in 300 ms.

### 4. Present1 dirty rects (not implemented)

- Under the flip model, the back buffer DXGI hands out holds content from three presents ago. A dirty-rect hint is only correct when everything outside the rects matches the previous frame.
- This draw path redraws the whole target every frame, and through an offscreen it blits the whole target (`draw.rs` `dx_blit`).
- No damage source exists yet: ticket 04 is open and not on this branch.
- Without damage information, any rect except the full surface could leave DWM showing stale pixels, and the full surface gains nothing. It should be revisited once ticket 04 provides per-frame damage. Even then it only saves DWM composition work.

### Fixtures and tests

- No bit-exact fixture changed. No fixture renders weight 500, so the intended change had nothing to update.
- Revision and SHA pins moved to 13 and `0b6e9265…`: `abi.go`, `consumerspec/evidence.go`, the glyphspec and scenespec revision checks, and the measure tool.
- `cargo test -p gpui-go-native` still fails with the 5 pre-existing a11y and capture test errors and nothing new.
- `ce-source` was copied from the main checkout. It is gitignored and not committed.
