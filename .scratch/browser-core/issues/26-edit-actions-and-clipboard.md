# 26 - Edit actions and the HTML clipboard

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 09
Triage: ready-for-agent

**What to build:** Copy, cut, paste and select-all are core actions with standard keys; the clipboard reads and writes the HTML format; apps can observe clipboard changes.

**Blocked by:** 09 (Default actions run after key listeners)

**Status:** ready-for-agent

- [ ] Clipboard round trip of HTML format (clipspec, rerun under contention).
- [ ] Key-to-action fixtures match Chrome's editing commands for an input.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
