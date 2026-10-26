# 58 - Damage tracking and paint flashing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code)
Blocked by: 54
Triage: ready-for-agent

**What to build:** Frames redraw and present only damaged regions: reused view ranges at the same bounds are undamaged, everything else damages old and new bounds. A paint-flashing overlay shows what each frame repaints.

**Blocked by:** 54 (R16: integrate, build rev 16 and measure)

**Status:** ready-for-agent

- [ ] Tests show an idle hover change damages only the hovered element's bounds.
- [ ] Measured GPU/CPU savings recorded against the baseline.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
