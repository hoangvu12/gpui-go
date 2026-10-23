# 40 - Gradient backgrounds and Tailwind classes

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 39
Triage: ready-for-agent

**What to build:** Elements can have linear gradient backgrounds, including the Tailwind bg-linear/from/via/to classes, interpolated like CSS.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [ ] Fixtures for angles, transparent stops and diagonal boxes match Chrome within tolerance (the diagonal normalization risk is tested first).
- [ ] The class mapper no longer rejects these classes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
