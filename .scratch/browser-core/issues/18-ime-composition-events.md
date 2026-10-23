# 18 - IME composition events

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** Input handlers get composition start, update and end callbacks and an IsComposing flag on key events, so apps can ignore keys during composition.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] A failing test first shows keydowns dropped during composition (spec risk), then passes.
- [ ] A Tier B transcript of a scripted composition matches the recorded Chrome event order.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
