# 17 - Context-menu event

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 09
Triage: ready-for-agent

**What to build:** Elements get OnContextMenu from right-button up, the Menu key (on key-up) and Shift+F10 (on key-down), with a keyboard-invoked position at the focused element.

**Blocked by:** 09 (Default actions run after key listeners)

**Status:** ready-for-agent

- [ ] Event-log fixtures for each trigger match Chrome's order on Windows.
- [ ] PreventDefault on contextmenu suppresses nothing else in the core.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
