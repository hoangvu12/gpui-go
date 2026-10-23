# 21 - Top layer and light dismiss

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 14
Triage: ready-for-agent

**What to build:** Overlays live in an ordered top layer (popover, hint and modal kinds, with a backdrop slot). A click outside closes popovers above the clicked one, treating the invoker and nested popovers as inside, as HTML popovers do.

**Blocked by:** 14 (Core Tab navigation, focus fixup and Inert)

**Status:** ready-for-agent

- [ ] A failing test first reproduces Base's nested-popover dismissal fault (spec risk).
- [ ] Fixtures with nested popovers and invokers match Chrome's light-dismiss results.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
