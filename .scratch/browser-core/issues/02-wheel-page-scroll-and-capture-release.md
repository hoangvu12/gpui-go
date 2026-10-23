# 02 - Wheel page-scroll setting and capture release

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: none
Triage: ready-for-agent

**What to build:** Scrolling works when Windows is set to scroll one screen per notch, and the window's mouse capture is released only once no button is held, so a second held button no longer loses the drag.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [ ] The WHEEL_PAGESCROLL value (0xFFFFFFFF lines) scrolls one page per notch instead of an enormous distance; covered by a unit test of the wheel translation.
- [ ] With two buttons pressed, releasing one keeps capture; releasing the last releases it (simulated-message test).
- [ ] No visible windows; GPUI_REAL_INPUT_TESTS is not used.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
