# 79 - App-level Tab wrap switch

Parent: ../map.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: none
Triage: ready-for-agent

**What to build:** An app-level switch that makes Tab past the last focus stop wrap to the first (and Shift+Tab before the first wrap to the last). It is OFF by default, so the default stays Chrome's: focus passes out and the window blurs (ticket 14's boundary rule, `Window.FocusNext/FocusPrev`).

- [ ] With the switch off, Tab past the last stop blurs as today (the existing focusspec/parity tests are unchanged).
- [ ] With it on, Tab past the last stop focuses the first and Shift+Tab before the first focuses the last, respecting inert subtrees, `TabGroup`s and modal focus traps.
- [ ] Documented next to the Tab navigation docs.

User decision (2026-10-27): add the switch, off by default (asked as open question 1 since 2026-10-26; the user accepted the recommendation).

## Comments

## Answer
