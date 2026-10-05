# Rasterize and draw text in all required glyph formats

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 08, 09

## Question

Text shaped by the selected service appears in a window using the reference raster and atlas paths.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [renderer contract](../../../docs/renderer-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Use exact-face DirectWrite rasterization and the pinned fallback/color behavior, preserving glyph IDs, bounds, offsets and scale.
- [x] Upload monochrome R8, subpixel BGRA and polychrome BGRA through their distinct atlas, blend and gamma paths; never reinterpret one format as another.
- [x] Compare deterministic raster/geometry fixtures and record controlled text captures at representative scales, fractional origins and composition modes.
- [x] Exercise font fallback, missing glyphs and supported color-font cases without claiming all COLR variants from the presence of a raster library.
- [x] Retain CPU raster/atlas allocations across cached draws and release after independent scene/submission owners retire; stress atlas growth/reuse and close with draws in flight.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render styled boxes through the scene kernel](08-paint-boxes.md)
- [Shape text and expose paragraph, caret and hit-test geometry](09-text-geometry.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by a background subagent and independently re-verified. Complete record: [evidence/ticket10-draw-text.json](../../../evidence/ticket10-draw-text.json).

- The pinned WindowsGlyphRasterizer (pub(crate) in the pin) is ported into the native artifact with the exact pinned platform construction (faces from the Parley/Fontique stack via IDWriteInMemoryFontFileLoader; TextSystem::rasterize_glyph entry): AlphaMask grayscale, ClearType BgraSubpixelMask, COLRv0 BgraColor with gamma/contrast-corrected coverage, typed unsupported-color distinctions routed to the pinned Swash fallback. Atlas service ported from directx_atlas.rs (etagere =0.2.15, three pools, device-loss generations, bound to the renderer device). Scene kernel version 2 adds sprite primitives with fx-0003 gates unchanged. DLL 7,723,008 bytes static CRT (revision 6, mask 0x7F).
- THE GATE: fx-0005-glyph-raster (39 events, deterministic) reproduced bit-exactly including all 26 raster pixel SHA-256 hashes through DirectWrite. A font-weight default discovery (Segoe UI Light vs regular) was found by the gate and fixed.
- Real-window text draw: shape -> rasterize -> atlas -> sprite scene -> present -> retirement on GPU completion with the ledger proving the poll-before-retire invariant.
- The pre-existing renderer pacing shutdown race (flagged by this ticket's window tests) was fixed by the orchestrator: 6/6 consecutive full-suite runs green.

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

