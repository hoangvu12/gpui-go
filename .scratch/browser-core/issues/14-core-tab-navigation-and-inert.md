# 14 - Core Tab navigation, focus fixup and Inert

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 09
Triage: ready-for-agent

**What to build:** Tab and Shift+Tab move focus as a core default action in HTML tabindex order; a press on empty space blurs; focus moves off an element that disappears at the end of the frame; an Inert style makes a subtree unfocusable and unclickable and hides it from accessibility (D4).

**Blocked by:** 09 (Default actions run after key listeners)

**Status:** ready-for-agent

- [ ] Event-log and focus fixtures for tab order (positive, zero, negative) match Chrome.
- [ ] Removing the focused element moves focus to the body equivalent, as Chrome does.
- [ ] Inert subtrees are skipped by Tab, hit testing and the accessibility tree.
- [ ] Base still works (its own Tab binding may stay until ticket 73).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
