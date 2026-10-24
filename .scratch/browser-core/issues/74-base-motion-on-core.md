# 74 - Base motion on the core engine

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 27, 44
Triage: ready-for-agent

**What to build:** Base's motion presets (presence, stagger, sequence, reveal, springs) run on the core timing, easing and keyframe engine, and Base reads reduced motion from SystemPreferences.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion), 44 (Transition style property and the core motion engine)

**Status:** ready-for-agent

- [ ] Base motion tests pass; Base's own timing, easing, keyframe and reduced-motion code is removed.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
