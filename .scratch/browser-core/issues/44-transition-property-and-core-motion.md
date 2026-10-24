# 44 - Transition style property and the core motion engine

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 27
Triage: ready-for-agent

**What to build:** A Transition style property animates style changes like CSS transitions, driven by a per-element style diff; the timing, easing and keyframe-interpolation engine (with CSS's reversing rule) moves from Base into the core; reduced motion is honoured.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion)

**Status:** ready-for-agent

- [ ] Timed fixtures (test clock) for transition start, interruption and reversal match the CSS spec's values.
- [ ] Reduced motion makes transitions complete immediately.
- [ ] Base compiles against the moved engine (Base migration itself is ticket 74).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
