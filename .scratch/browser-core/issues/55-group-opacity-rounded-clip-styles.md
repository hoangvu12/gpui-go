# 55 - Group opacity, rounded clipping and border order from styles

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 54
Triage: ready-for-agent

**What to build:** Styles use the new compositor: opacity applies to the group (D9), rounded corners clip descendants, and borders paint before children as in CSS.

**Blocked by:** 54 (R16: integrate, build rev 16 and measure)

**Status:** ready-for-agent

- [ ] Fixtures: translucent element with overlapping children, rounded card with an image, bordered box with an overflowing child, all match Chrome.
- [ ] Changed fixtures carry divergence notes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
