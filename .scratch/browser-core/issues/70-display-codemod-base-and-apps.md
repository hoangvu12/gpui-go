# 70 - Codemod applied in Base, zterm and the gallery

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: 68
Triage: ready-for-agent

**What to build:** All elements in Base, zterm and the gallery that relied on the default display state Flex explicitly.

**Blocked by:** 68 (Codemod: explicit Flex where the default was relied on)

**Status:** ready-for-agent

- [ ] Base tests and zterm tests pass; offscreen captures of zterm and the gallery are identical.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
