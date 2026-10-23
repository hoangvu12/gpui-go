# 53 - R16: retained frame, damage redraw, dirty-rect present and caching

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 39, 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: a retained frame target, scissored redraw of damaged regions, dirty-rect present, blur damage expansion, redundant GPU-state bind removal and a path tessellation cache.

**Blocked by:** 39 (R15 native rebuild; R16 records extend it), 49 (R16 compositor design)

**Status:** ready-for-agent

**Design ([docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) §3.11-3.12, §7, §8.1; approved 2026-10-24):**
- A retained frame target per surface, plus a new `renderer_surface_draw_scene_damage` entry.
- Damage rects are merged and capped at **16 regions**, zgui's rule (`MAX_REGIONS`, zgui-gpu `lib.rs:6806-6916`), with a full redraw on a new surface or a resize. There is no percentage-of-window threshold.
- Damage is copied from the retained target to the back buffer, then presented with `Present1` dirty rects. In DirectComposition mode, partial present is used only if the readback test passes; otherwise it presents full.
- Damage expands by 3σ across blur groups, and blur passes are limited to the damaged region.
- The state-bind tracker and the path tessellation cache are independent and can land first.

- [ ] Native tests show unchanged pixels outside damage and correct present regions.
- [ ] A full-damage draw matches the legacy path pixel for pixel; a damaged redraw matches a full redraw bit-exactly over a scripted sequence.
- [ ] `Present1` is verified in both surface modes, and the full-present fallback fires when it fails.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
