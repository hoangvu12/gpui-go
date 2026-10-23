# 60 - Property-only animation frames

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 44, 56, 58
Triage: ready-for-agent

**What to build:** Transform and opacity transitions update scene property nodes and skip render, layout and paint; animation frames stop notifying the whole view.

**Blocked by:** 44 (Transition style property and the core motion engine), 56 (Transforms with hit testing), 58 (Damage tracking and paint flashing)

**Status:** ready-for-agent

- [ ] A transform transition runs with zero view renders per frame (observable count).
- [ ] Frame cost measured and recorded.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: under the approved R16 design (docs/design/r16-compositor.md §3.10), this is the Go half of property-only frames. It needs the native `scene_update_property` (51) and `renderer_surface_draw_scene_damage` (53), shipped in rev 16 (54). Detection is automatic from an active transform or opacity transition, with no opt-in style (user, 2026-10-24).

## Answer
