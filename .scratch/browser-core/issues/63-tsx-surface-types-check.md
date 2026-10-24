# 63 - TSX surface, generated .d.ts and check command

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 62
Triage: ready-for-agent

**What to build:** Every core element, style property, Tailwind class and event is available to TSX, a .d.ts is generated from the engine's surface, a test fails when the engine accepts something the types lack, and a check command runs tsc (debug builds also check after each reload when tsc is present).

**Blocked by:** 62 (TSX host tracer)

**Status:** ready-for-agent

- [ ] The parity test between engine surface and .d.ts passes and fails on a planted gap.
- [ ] tsc catches a misspelled style key, a bad value and an unknown prop in a sample.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
