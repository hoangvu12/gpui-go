# 37 - Line height normal by default

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** The default line height is CSS normal (from the font's ascent, descent and gap); 1.618 stays available as an opt-in (D1).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures with default line height in Segoe UI and Geist match Chrome's boxes exactly.
- [ ] Changed fixtures carry divergence notes; examples are checked for regressions in offscreen captures.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
