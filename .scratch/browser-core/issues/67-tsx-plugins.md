# 67 - TSX plugins

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 63, 64
Triage: ready-for-agent

**What to build:** Apps declare slots, and plugins (each in its own goja runtime with a per-call time budget) contribute components to them; plugins reach only granted actions, shared state and capabilities; a missing grant is a named error; a failing or runaway plugin is left out while the app keeps running. The ticket decides plugin memory isolation (separate process or time budget only) and records the decision.

**Blocked by:** 63 (TSX surface, generated .d.ts and check command), 64 (TSX host APIs and the Go bridge)

**Status:** ready-for-agent

- [ ] Tests for slot contributions, a denied grant, a runaway loop stopped by the budget and a crashing plugin left out.
- [ ] The memory-isolation decision is recorded in ADR 0003.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
