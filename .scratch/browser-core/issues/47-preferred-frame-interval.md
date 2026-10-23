# 47 - Preferred frame interval per window

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: none
Triage: ready-for-agent

**What to build:** A window can ask for a lower frame rate (for example half rate) for its animations.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [ ] Frame-loop test: a half-rate window draws every other vblank during an animation.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
