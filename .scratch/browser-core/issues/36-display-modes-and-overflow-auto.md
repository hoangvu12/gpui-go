# 36 - More display modes, overflow: auto and scrollbar gutter

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** display: contents, inline-block and inline-grid work; overflow: auto shows scrollbars only when needed; scrollbar-gutter: stable is available as an opt-in.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures for each mode match Chrome's boxes.
- [ ] overflow: auto shows no scrollbar when content fits and one when it overflows.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
