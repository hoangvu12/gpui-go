# 51 - R16: transformed composite and property nodes

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 39, 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: subtrees rendered offscreen and composited under a transform matrix, and scene property nodes for transform and opacity that can change without rebuilding the scene.

**Blocked by:** 39 (R15 native rebuild; R16 records extend it), 49 (R16 compositor design)

**Status:** ready-for-agent

**Design ([docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) §3.4, §3.10, §7, §8.1; approved 2026-10-24):** Render the subtree offscreen in local space, then composite it under a 2×3 matrix. Translation-only transforms skip the group. Add `scene_update_property(scene, node_id, kind, values)`: it mutates a finished scene's effect records in place and returns the old and new mapped bounds, so the same scene can be re-presented with damage. Property-only frames are **automatic**, detected from an active transform or opacity transition with no opt-in style. The Go half is ticket 60.

- [ ] Native spec tests for translate, scale, rotate and skew composites and for property-node updates.
- [ ] `scene_update_property` tests: records mutated, old and new bounds returned, the plan unchanged, a stale id rejected.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
