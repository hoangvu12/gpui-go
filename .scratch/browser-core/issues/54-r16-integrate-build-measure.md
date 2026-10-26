# 54 - R16: integrate, build rev 16 and measure

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 50, 51, 52, 53
Triage: ready-for-agent

**What to build:** The R16 branch is integrated, rebuilt as native rev 16 and measured, with pins and evidence updated. Needs the user's approval before the rebuild.

**Blocked by:** 50 (R16: group targets, opacity groups and rounded content mask), 51 (R16: transformed composite and property nodes), 52 (R16: masks, clip-path, blend modes, drop-shadow and gradients), 53 (R16: retained frame, damage redraw, dirty-rect present and caching)

**Status:** ready-for-agent

- [ ] The user approved the rebuild (recorded in the ticket).
- [ ] All native spec packages pass; unchanged bit-exact fixtures stay unchanged.
- [ ] Perf measured against the gpui-perf baseline and recorded.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-26, orchestrator 11: the rebuild is approved by the user's decision of 2026-10-25 ("feel free to do native tickets after every go only is done, but make sure to make it fast somehow without making my pc to freeze"). Conditions: one cargo build at a time, `-j 4`, below-normal priority. R16 runs on the integration branch `native/r16` (50 → 51 → 52 → 53, then 42/43), with interim DLLs tested through `GPUI_GO_NATIVE_BUNDLE` and not committed; this ticket refreshes the embedded artifact.

## Answer
