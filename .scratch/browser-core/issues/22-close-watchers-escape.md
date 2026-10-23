# 22 - Close watchers and Escape

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 09, 21
Triage: ready-for-agent

**What to build:** Escape not handled by a listener signals the newest close watcher, so the newest overlay closes regardless of where focus is.

**Blocked by:** 09 (Default actions run after key listeners), 21 (Top layer and light dismiss)

**Status:** ready-for-agent

- [ ] Fixtures: stacked overlays close newest-first on Escape; a tooltip-like hint closes with Escape.
- [ ] PreventDefault in a key listener stops the close.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
