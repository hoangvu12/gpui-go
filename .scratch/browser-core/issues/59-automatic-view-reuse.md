# 59 - Automatic view reuse

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 12 (Claude Code)
Blocked by: 58
Triage: ready-for-agent

**What to build:** Views whose tracked inputs didn't change are reused automatically, as gpui-fast does, and reused views keep their accessibility nodes.

**Blocked by:** 58 (Damage tracking and paint flashing)

**Status:** ready-for-agent

- [ ] A view that didn't change is not re-rendered (observable through render counts in a test).
- [ ] The accessibility tree is identical with and without reuse.
- [ ] Frame cost measured against the baseline.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
