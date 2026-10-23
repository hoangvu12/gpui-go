# 50 - R16: group targets, opacity groups and rounded content mask

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 39, 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: a pool of offscreen group targets with no nesting cap, group opacity, and a rounded content mask that clips descendants to rounded corners.

**Blocked by:** 39 (R15 native rebuild; R16 records extend it), 49 (R16 compositor design)

**Status:** ready-for-agent

**Design ([docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) §2, §3.1-3.3, §7, §8.1; approved 2026-10-24):** One effect-group record (`GpuiGoSceneEffectRecord`, plan command kinds 3 and 4) carries opacity, transform, blend, mask, rounded clip and property node. Filter boundaries stay for R15 chains, and both kinds share one group stack. `MAX_FILTER_GROUP_DEPTH` is removed and replaced by a bounds-sized, bucketed target pool that is kept across frames. Over the pool budget a group renders inline, and this must be **observable**: a debug-stats counter and a warning in debug builds, never silent and never a crash (user, 2026-10-24). A single-primitive opacity group folds into a per-primitive multiply. The content mask grows to 52 bytes (bounds, `fade_out`, radii, smoothing). Cut the integration branch only after R15 (39) merges.

- [ ] Native spec tests for nested groups beyond the old depth of 2, group opacity and rounded clipping.
- [ ] Pool overflow increments a debug counter and warns in debug builds; a test covers it.
- [ ] R15's gradient record and its 2-stop path stay bit-exact.
- [ ] Work lands on the shared R16 integration branch; green is promised at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
