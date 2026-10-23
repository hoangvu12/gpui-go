# 27 - SystemPreferences, system colours and live reduced motion

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** Apps read a SystemPreferences record (reduced motion, contrast, transparency, accent colour, text scale, caret blink) and SystemColors, with change events when Windows settings change; reduced motion is now core and live.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Simulated WM_SETTINGCHANGE updates the record and fires a change event.
- [ ] Contrast preference follows Chromium's rule for Windows high-contrast palettes.
- [ ] Base's own reduced-motion read can be dropped later (ticket 74).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
