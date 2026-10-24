# 72 - Base overlays rebuilt on core primitives

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 9 (Claude Code)
Blocked by: 21, 22, 23, 24, 25
Triage: ready-for-agent

**What to build:** Base's popover, dialog, sheet, select, combobox and tooltip use the core top layer, close watchers, ShowModal, Anchored and the tooltip hook instead of their own dismissal, Escape bindings, focus trap, liveness list and positioner.

**Blocked by:** 21 (Top layer and light dismiss), 22 (Close watchers and Escape), 23 (ShowModal), 24 (Tooltip hook), 25 (Anchored to an element, with fallbacks)

**Status:** ready-for-agent

- [ ] Base's overlay tests pass on the core primitives.
- [ ] The per-component Escape bindings, focus trap and popup liveness list are deleted.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
