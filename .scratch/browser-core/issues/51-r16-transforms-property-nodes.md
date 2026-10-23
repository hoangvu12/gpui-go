# 51 - R16: transformed composite and property nodes

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: subtrees rendered offscreen and composited under a transform matrix, and scene property nodes for transform and opacity that can change without rebuilding the scene.

**Blocked by:** 49 (R16 compositor design)

**Status:** ready-for-agent

- [ ] Native spec tests for translate, scale, rotate and skew composites and for property-node updates.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
