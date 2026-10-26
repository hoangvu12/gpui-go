# 56 - Transforms with hit testing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code)
Blocked by: 54
Triage: ready-for-agent

**What to build:** Elements accept CSS transforms (translate, scale, rotate, skew, transform-origin), and hit testing follows the transform.

**Blocked by:** 54 (R16: integrate, build rev 16 and measure)

**Status:** ready-for-agent

- [ ] Box and pixel fixtures for each transform match Chrome.
- [ ] Event-log fixtures: clicks on rotated and scaled elements hit the right targets (spec risk tested first).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
