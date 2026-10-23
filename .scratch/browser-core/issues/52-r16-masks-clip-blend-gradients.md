# 52 - R16: masks, clip-path, blend modes, drop-shadow and gradients

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: mask composite (edge fades, gradient and image masks, alpha and luminance), clip-path coverage, blend modes including plus-lighter, the drop-shadow filter, and n-stop, radial, conic and repeating gradients.

**Blocked by:** 49 (R16 compositor design)

**Status:** ready-for-agent

- [ ] Native spec tests for each primitive.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
