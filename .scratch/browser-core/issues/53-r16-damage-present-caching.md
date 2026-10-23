# 53 - R16: retained frame, damage redraw, dirty-rect present and caching

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: a retained frame target, scissored redraw of damaged regions, dirty-rect present, blur damage expansion, redundant GPU-state bind removal and a path tessellation cache.

**Blocked by:** 49 (R16 compositor design)

**Status:** ready-for-agent

- [ ] Native tests show unchanged pixels outside damage and correct present regions.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
