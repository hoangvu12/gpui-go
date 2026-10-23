# 23 - ShowModal

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 14, 21, 22
Triage: ready-for-agent

**What to build:** Window.ShowModal opens an overlay as modal: everything outside becomes inert, focus moves by the dialog focusing steps, focus returns to the previous element on close, and closedby controls light dismiss and Escape.

**Blocked by:** 14 (Core Tab navigation, focus fixup and Inert), 21 (Top layer and light dismiss), 22 (Close watchers and Escape)

**Status:** ready-for-agent

- [ ] Fixtures for focusing steps and focus restore match Chrome's dialog.showModal().
- [ ] Outside content can't be focused, clicked or reached by the accessibility tree while modal.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
