# 15 - Wheel latching, overscroll contain and keyboard scrolling

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 09
Triage: ready-for-agent

**What to build:** A wheel gesture latches to the innermost scroller that can scroll, overscroll-behavior: contain stops chaining, the keyboard scrolls the focused or hovered scroller (arrows, Page Up/Down, Home/End, Space) as a default action, and the wheel step is Chrome's 100/3 px per line (D10).

**Blocked by:** 09 (Default actions run after key listeners)

**Status:** ready-for-agent

- [ ] A failing test first shows nested scrollers moving together today (spec risk), then passes.
- [ ] Scroll-offset fixtures for nested scrollers and keyboard scrolling match Chrome.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
