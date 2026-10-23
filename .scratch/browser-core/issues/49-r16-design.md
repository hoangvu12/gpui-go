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

## Answer
