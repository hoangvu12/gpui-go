# 32 - Text min-content wraps

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Text min-content wraps at every break opportunity, so text in a flex row shrinks and wraps instead of overflowing; the text measure cache is keyed by sizing mode (D2).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures: flex rows with long text match Chrome's wrapping and boxes exactly.
- [ ] A test proves the cache doesn't return a wrapped size for a max-content query (spec risk).
- [ ] Changed fixtures carry divergence notes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
