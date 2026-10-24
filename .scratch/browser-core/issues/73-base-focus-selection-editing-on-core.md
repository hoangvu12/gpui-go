# 73 - Base focus, selection and editing on core primitives

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 9 (Claude Code)
Blocked by: 14, 20, 26, 28
Triage: ready-for-agent

**What to build:** Base drops its Tab binding, selection model, copy/cut/paste/select-all actions and hard-coded caret blink and selection colours, using the core's versions.

**Blocked by:** 14 (Core Tab navigation, focus fixup and Inert), 20 (Text selectable by default, with UserSelect), 26 (Edit actions and the HTML clipboard), 28 (Text scale to rem, caret blink and highlight colours)

**Status:** ready-for-agent

- [ ] Base tests pass with the core versions; the removed code is gone.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
