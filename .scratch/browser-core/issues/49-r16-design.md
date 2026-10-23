# 49 - R16 compositor design

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: none
Triage: ready-for-human

**What to build:** A short design for the R16 rebuild: group target pool, opacity groups, transformed composite, rounded content mask, masks, clip-path, blend modes, n-stop gradients, drop-shadow, scene property nodes, retained frame target with damage redraw and dirty-rect present, plus GPU-state and path caching. It names the native records, the Go-side scene plan changes and the tests.

**Blocked by:** None - can start immediately

**Status:** ready-for-human

- [ ] A design document reviewed and approved by the user.
- [ ] Tickets 50-53 are updated with any design changes before work starts.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- **2026-10-24 (orchestrator 6, ticket 49 work):** Design written to [docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) on branch `core/49-r16-design`. One effect-group record + a group target pool (no nesting cap) carries opacity, transforms, rounded clipping, masks, clip-path and blend modes; the content mask grows to 52 bytes (the pin's `fade_out` finally carried); a gradient table covers n-stop/radial/conic/repeating (R15's 2-stop path stays bit-exact); a filter op chain adds drop-shadow (R15-append); `scene_update_property` + `draw_scene_damage` + `Present1` dirty rects give property-only frames and partial present. Order of work, risks, measurements, proposed ticket 50-53 changes and six open questions for the user are in the doc. Awaiting user review; Status stays claimed until the doc is approved.

## Answer
