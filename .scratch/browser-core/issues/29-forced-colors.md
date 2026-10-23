# 29 - Forced-colours pass

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 7 (Claude Code)
Blocked by: 27
Triage: ready-for-agent

**What to build:** In Windows high-contrast mode an optional forced-colours pass maps colours to system colours during style resolution, as Chromium does.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion)

**Status:** ready-for-agent

- [ ] Fixtures rendered with forced-colors active in Chrome match gpui-go with the pass on.
- [ ] The pass is off when high contrast is off.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
