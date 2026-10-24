# 19 - Core text selection on static text

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 10
Triage: ready-for-agent

**What to build:** Static text can be selected with the mouse (press-drag, double-click word, triple-click line), the selection is painted with the platform highlight, and copying works through a core action. The selection model moves from Base into the core.

**Blocked by:** 10 (Hover: pointer enter/leave, window leave, survives typing)

**Status:** ready-for-agent

- [ ] Selection range fixtures for drag, double- and triple-click match Chrome's ranges on sample text.
- [ ] Base's text views keep working on top of the core model.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
