# 35 - position: fixed

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 33, 21
Triage: ready-for-agent

**What to build:** Fixed elements are placed relative to the window and painted above normal flow, regardless of their ancestors.

**Blocked by:** 33 (position: static (opt-in), containing block and lint), 21 (Top layer and light dismiss)

**Status:** ready-for-agent

- [ ] Fixtures with fixed inside scrolled and transformed-free ancestors match Chrome.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
