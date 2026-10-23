# 62 - TSX host tracer

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** A .tsx view (JSX elements, CSS-named style objects, Tailwind className, onClick) loads into a window through esbuild and goja. State declared with a key survives reload, state changes update bound values without re-running the tree, saving the file reloads in place, and a failed reload keeps the last good UI with an error banner showing the .tsx file and line (ADR 0003, productionizing the probe).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] A .tsx harness fixture matches the same Chrome page as its Go twin.
- [ ] Reload keeps keyed state; a syntax error keeps the last tree and shows file:line.
- [ ] The probe's thresholds hold: reload under 300 ms, a bound update under 2 ms, frame overhead under 1 ms at the probe's size.
- [ ] Re-describing on useState stays available but documented as the slow path.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
