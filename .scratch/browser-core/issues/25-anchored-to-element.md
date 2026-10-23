# 25 - Anchored to an element, with fallbacks

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 7 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Anchored can position relative to another element, with side, alignment and offset, flip/shift fallbacks when it doesn't fit, and hiding when the anchor is clipped, as in CSS anchor positioning.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Box fixtures for each side and for flip and shift near window edges match Chrome's anchor positioning.
- [ ] Base's positioner can be replaced in one component (proof, not the full migration).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
