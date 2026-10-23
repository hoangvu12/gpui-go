# 10 - Hover: pointer enter/leave, window leave, survives typing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** Elements get OnPointerEnter and OnPointerLeave from a hover diff, leaving the window clears hover on every element, and key presses no longer clear hover (D12).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Event-log fixtures for enter/leave across nested elements match Chrome's order.
- [ ] Moving the pointer out of the window clears hover styles (simulated WM_MOUSELEAVE).
- [ ] Typing while hovering keeps hover styles.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
