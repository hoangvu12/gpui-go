# 06 - Text style quick fixes

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Several text styles behave like CSS: text-transform (uppercase, lowercase, capitalize) is applied, non-zero letter-spacing turns off ligatures, underlines use Chrome's thickness and offset, line-clamp ends with an ellipsis, tabular-nums and friends work as classes, the generic families sans-serif/serif/monospace resolve, and text on non-opaque windows uses grayscale antialiasing.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures for each item match Chrome (boxes exact, pixels within tolerance).
- [ ] text-transform matches the pin's behaviour (it was a port gap, not a divergence).
- [ ] Grayscale AA on a transparent window is covered by a scene/glyph spec test.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
