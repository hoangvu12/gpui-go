# 58 - Damage tracking and paint flashing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: unassigned
Blocked by: 54
Triage: ready-for-agent

**What to build:** Frames redraw and present only damaged regions: reused view ranges at the same bounds are undamaged, everything else damages old and new bounds. A paint-flashing overlay shows what each frame repaints.

**Blocked by:** 54 (R16: integrate, build rev 16 and measure)

**Status:** ready-for-agent

- [x] Tests show an idle hover change damages only the hovered element's bounds.
- [x] Measured GPU/CPU savings recorded against the baseline.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Landed on `core/58-damage-and-paint-flashing` (Go only — the native rev-16 `renderer_surface_draw_scene_damage` from ticket 53 consumes the rects unchanged).

**What landed**

- **The damage rule** (`gpui/damage.go`): a frame's damage is computed at the end of its build, before the scene's finish, from the scene-op ranges `reusePaint` replayed from the previous scene. The reused view ranges at the same bounds and mask are undamaged — Chrome's chunk rule for a cached subsequence (`raster_invalidator.cc:242-249`; the cached view IS the cached subsequence, and the cache key already demands the same bounds, content mask and text style). Everything else damages its old AND new bounds (`raster_invalidator.cc:268-275`): the fresh ops' clipped bounds (new) plus the previous scene's ops outside the replayed ranges (old — the changed, moved and disappeared content, the trailing sweep of `raster_invalidator.cc:308-325`). The rects are merged and capped at 16 (zgui's `MAX_REGIONS`, the native cap) before the present.
- **The op-index seam**: the scene kernel's Go side mirrors the native paint-op log's index space — every insert appends the op's clipped device bounds (the native insertion core's empty-clip drop rule replicated field-for-field), `Replay` copies the source range's entries, and every read of a range validates the log against the native `Len()` (drift → a full redraw, never a missed one). Capture-backed surfaces (`PaintCaptureSurface`) are marked in the log: their bounds ALWAYS damage, even inside a replayed range (their frames update outside the scene's knowledge — the port's contrast to Chromium's self-invalidating foreign layers, `raster_invalidator.cc:168-171`).
- **The present seam**: `Window.LastFrameDamage()` returns the last completed frame's merged device rects and its full-redraw flag, and `gpui.PresentFrame(w, surface, appearance)` is the OnRedraw hook's damage-aware spelling (the window's own glyph atlas, `Surface.DrawSceneDamage`, forceFull for full frames, and NOTHING for a frame whose every op replayed unchanged — an empty rect list is the native entry's full-redraw fallback, so skipping the submission is both correct and the saving). `examples/hello` presents through it. The port's renderer is application-owned, so the frame loop's present step (the hook) reads the window's computation — the same handoff the reference's `Window::present` makes internally.
- **The paint-flashing overlay** (`WindowOptions.PaintFlashing` / `Window.SetPaintFlashing`): one translucent quad per merged damage rect, alternating red and green frame over frame, one viewport-sized quad on a full redraw — inserted after the damage was computed and marked in the op log so a tint never damages its own bounds (it persists until its region repaints, Chrome's paint-rects behavior).

**Full-redraw fallbacks** (correctness first — any frame the rule cannot prove): the first frame after open or an abandoned build; a viewport, scale or clear-appearance change; damage-log drift; replays out of order, overlapping or out of range (the in-order rule of `MatchNewChunkToOldChunk`, `raster_invalidator.cc:51-66` — an absolutely-positioned cached view whose tree position moved backward is the test's reproduction). Recovery and surface resize fall back natively (the retained frame target's validity, ticket 53).

**Tests**

- `gpui/damage_test.go` (deterministic test windows): the first frame is full; an idle hover change (the pin's `cx.notify(current_view)` through the real mouse-move path) damages exactly the hovered panel's device rect, and so does the hover-out; a notified panel damages its own bounds; a root notification with every panel cached replays everything and damages nothing; a removed panel damages its old bounds; a swapped pair damages both cells (the moved-chunk answer); an out-of-order replay falls back to full; an uncached window damages all painted content; a viewport change falls back to full; a scroll damages only the scroller's region; capture-backed surfaces damage inside replayed ranges; the paint-flashing overlay tints the damage rects, alternates colors and stays out of its own damage; and the kernel's damage log matches the native op count across every op kind (quads with empty-clip drops, shadows, underlines, backdrops, boundaries, effects, surfaces, the three sprite kinds, paths, layer pushes/pops, gradients, coverage, filter-op chains, replay copies).
- `internal/rendererspec/r58_damage_frame_test.go` (one real off-screen window driven through its frame loop, the r16Surface pattern with a damage-aware OnRedraw): `TestR58HoverScrollAndAnimationDamageMatchesFullRedraw` — for every step of the hover, scroll and animation scenarios, the damaged present's retained-frame readback is BYTE-IDENTICAL to a full redraw of the same scene (a missed damage region would leave stale pixels and fail the compare), with the hover's damage exactly the hovered pane's device rect, the scroll's inside the scroller, the animation's exactly the pulse pane's.
- The measurement harness (below) doubles as the present-integration gate.

**Measurements** (`evidence/browser-core/ticket58-damage.json`, env-gated `R58_MEASURE_OUT`, the r54 pattern): one 1280x800 window per surface mode, a page of 192 cached panes + a 40-row scroller + a pulse pane (~470 primitives), every scenario measured as interleaved damage/force-full pairs so both sides see the same machine load.

| Scenario | Damage fraction (native, per-mille) | Paired frame savings | Present submit (damage vs full) |
|---|---|---|---|
| Hover change | 3 (one 80x40 pane) | +0.5 to +0.8 ms mean (5-8%) | ~1.0 vs ~0.6 ms CPU |
| Scroll | 188 (the scroller region) | ~0 (±0.5 ms) | ~0.8 vs ~0.6 ms CPU |
| Animation | 20 (the pulse pane) | ~0 (±0.3 ms) | ~0.7 vs ~0.5 ms CPU |
| Full-redraw control | 1000 (the whole window) | ~0 (the control pins the ceiling) | ~0.9 vs ~0.5 ms CPU |
| Hover, hwnd mode | 3 | — | 30/30 PARTIAL presents (Present1 at the window level; 0 full) |

The honest reading: the GPU work the redraw does is the damage fraction — 3-per-mille for a hover (a 300x reduction of repainted area), 188 for a scroll — and the hwnd mode converts that into dirty-rect presents (30/30). The damage path pays ~0.3 ms more present CPU (the merge, the retained-target copy) and wins it back in GPU completion time, so the wall-clock saving is real for small damage (hover) and about neutral at 20-190-per-mille at this scene size (~470 primitives; the r54 ABI-level numbers — the property tick 1.58 ms vs the full redraw 2.17 ms at 1000+ quads — are the pure-present ceiling). This machine is heavily shared during the runs, so the absolute wall times are several times an idle machine's; the paired mean is the comparable statistic and the fractions/present counts are deterministic. On this machine the DEFAULT (DComp) surface presents full after ticket 53's pinned readback fallback — the scissored redraw still skips the undamaged shading — while the hwnd surface accepts Present1.

**Gaps and follow-ups**

- The rule's granularity is the cached view range: a window whose content is not wrapped in `CachedView`s damages everything it paints every frame (the conservative rule — the savings are ticket 04's caching paying off here). Chrome additionally matches uncached chunks by client id; the port would need element-level content comparison to do that (not in this ticket's scope).
- The draw (rebuild) dominates the frame at the window level; ticket 60's property-only frame path (UpdateEffectMatrix/Opacity → old/new bounds → DrawSceneDamage, no rebuild at all) owns the rest of the saving — the r54 measurements are its ceiling and `PresentFrame`/`LastFrameDamage` are its ready-made seam.
- The DComp partial present remains disabled on this machine by ticket 53's readback fallback (native-side, verified per surface once).
- A no-change frame (every op replayed) presents nothing through `PresentFrame`; an app that must keep presenting (e.g. to feed a capture) can call `Surface.DrawSceneDamage` itself.

**For 59/60**: 59 (automatic view reuse) widens the undamaged coverage — the rule needs no change, it already reads whatever `reusePaint` replays; 60 should drive `Scene.UpdateEffectMatrix/Opacity` → the returned old/new bounds (dilated ~2px for the composite's bilinear support, as the r53/r54 tests do) → `Surface.DrawSceneDamage` directly, and can reuse `PresentFrame`'s atlas/appearance plumbing. The damage log and the validation gate (`damageLogDrifted`) are the debugging seam if 59's reuse changes the range bookkeeping.
