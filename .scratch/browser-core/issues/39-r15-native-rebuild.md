# 39 - R15 native rebuild

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: none
Triage: ready-for-agent

**What to build:** One native rebuild (approved as rev 15) delivers: the Windows bold rule (weight >= 600 over a face < 600), drop shadows clipped out of the border box, a gradient record (two stops, CSS interpolation), colour-matrix filters, the backdrop mirror crop, a point sampler, new text-run fields (word-break, overflow-wrap, text-wrap, locale, variations, width, optical size, justify) and new layout fields (baseline in measure, variable grid tracks, justify-items/self, flow-root). Built and measured like rev 13 and rev 14.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [ ] The bold weight-matrix test passes with Chrome's Windows rule (the 600/400, 700/500 and 900/600 cells updated).
- [ ] Native spec packages cover each new record and field; unchanged bit-exact fixtures stay unchanged.
- [ ] Revision and artifact pins, measurements and evidence are updated in every place rev 14 touched.
- [ ] Headless runs only; no visible windows.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
