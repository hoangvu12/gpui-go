# 50 - R16: group targets, opacity groups and rounded content mask

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: a pool of offscreen group targets with no nesting cap, group opacity, and a rounded content mask that clips descendants to rounded corners.

**Blocked by:** 49 (R16 compositor design)

**Status:** ready-for-agent

- [ ] Native spec tests for nested groups beyond the old depth of 2, group opacity and rounded clipping.
- [ ] Work lands on the shared R16 integration branch; green is promised at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
