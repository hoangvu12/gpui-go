# 69 - Codemod applied in gpui-go

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: 68
Triage: ready-for-agent

**What to build:** All elements in gpui-go's own packages, examples and tests that relied on the default display state Flex explicitly.

**Blocked by:** 68 (Codemod: explicit Flex where the default was relied on)

**Status:** ready-for-agent

- [ ] The full headless test suite passes with identical fixtures.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
