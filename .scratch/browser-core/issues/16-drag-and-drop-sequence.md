# 16 - The full drag and drop sequence

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 09, 10, 11
Triage: ready-for-agent

**What to build:** Drag and drop follows the HTML sequence: drag start, drag, enter, over (with an effect), leave, drop and end. Escape and window deactivation cancel a drag, and files dragged from other apps show 'none' over elements that won't accept them.

**Blocked by:** 09 (Default actions run after key listeners), 10 (Hover: pointer enter/leave, window leave, survives typing), 11 (Pointer capture retargeting and local position)

**Status:** ready-for-agent

- [ ] Event-log fixtures for an internal drag across three targets match Chrome's sequence.
- [ ] Escape mid-drag fires leave and end with no drop.
- [ ] OLE drag-over reports none over a non-accepting target (dropspec test).
- [ ] zterm's drag workarounds can be removed (a follow-up note, not required here).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
