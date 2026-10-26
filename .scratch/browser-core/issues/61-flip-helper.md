# 61 - FLIP helper

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code)
Blocked by: 44, 56
Triage: ready-for-agent

**What to build:** A FLIP helper animates layout changes by diffing geometry and running a transform transition, replacing hand-written slide code like zterm's.

**Blocked by:** 44 (Transition style property and the core motion engine), 56 (Transforms with hit testing)

**Status:** ready-for-agent

- [ ] A reorder animation test (test clock) shows elements moving from old to new positions.
- [ ] zterm's Flip helper can be replaced (follow-up note).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
