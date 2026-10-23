# 45 - Smooth scrolling

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 15, 44
Triage: ready-for-agent

**What to build:** Wheel, keyboard and programmatic scrolls animate with Chrome's curve by default, and jump instantly under reduced motion (D13).

**Blocked by:** 15 (Wheel latching, overscroll contain and keyboard scrolling), 44 (Transition style property and the core motion engine)

**Status:** ready-for-agent

- [ ] Timed scroll-offset samples match Chrome's curve within tolerance.
- [ ] Reduced motion gives instant scrolls.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
