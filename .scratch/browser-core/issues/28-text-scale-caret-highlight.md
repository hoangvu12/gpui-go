# 28 - Text scale to rem, caret blink and highlight colours

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 27
Triage: ready-for-agent

**What to build:** rem follows the Windows text size by default with a per-app opt-out (D11), caret blink uses the system rate (including never), and selection highlight colours come from the system.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion)

**Status:** ready-for-agent

- [ ] rem changes live when the simulated text scale changes; the opt-out holds it at 16.
- [ ] Caret blink and highlight colours read from SystemPreferences.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
