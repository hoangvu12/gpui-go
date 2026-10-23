# 09 - Default actions run after key listeners

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** Core default actions (Tab, Escape, keyboard scrolling, activation) run after element key listeners and are skipped when a listener calls PreventDefault. Key bindings still run before listeners (GPUI shape). A per-window flag gives the full browser order (D5).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Harness event-log fixtures show listener-then-default order and PreventDefault suppression.
- [ ] Key bindings still win over listeners by default; the per-window flag reverses that and matches Chrome's order.
- [ ] Existing keybinding tests pass.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
