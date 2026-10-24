# 24 - Tooltip hook

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 9 (Claude Code)
Blocked by: 21, 22
Triage: ready-for-agent

**What to build:** Any element can have a tooltip that opens on hover and on keyboard focus, with Chrome's delays and grace period, in the hint layer, with a plain default look.

**Blocked by:** 21 (Top layer and light dismiss), 22 (Close watchers and Escape)

**Status:** ready-for-agent

- [ ] Timed event-log fixtures for hover and focus triggers, using the test clock.
- [ ] Escape closes it (via close watchers).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
