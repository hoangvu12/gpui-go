# 11 - Pointer capture retargeting and local position

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** While an element holds pointer capture, pointer events go to it alone, with got/lost-capture callbacks; losing capture to another window cancels cleanly; events carry an element-local position (D12).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Event-log fixtures with setPointerCapture match Chrome.
- [ ] A simulated capture change fires lost-capture and leaves no button stuck.
- [ ] A local-position test replaces the hitbox-bounds workaround in one example.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
