# 42 - Grid tracks, justify, baselines and flow-root

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 39
Triage: ready-for-agent

**What to build:** Grid accepts full track lists (fixed and fr mixes, auto rows, areas) and justify-items/self; items-baseline aligns text baselines; flow-root is available.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [ ] Fixtures with 240px 1fr grids, areas and baseline-aligned mixed font sizes match Chrome's boxes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-26, orchestrator 11 (the approach): fold into the rev 16 rebuild on `native/r16`. Feed the measured first baseline (rev 15's measure response) into Taffy's algorithms by driving them through our own implementation of Taffy 0.13's low-level tree traits (LayoutPartialTree and friends), so a measured leaf returns `first_baselines` instead of the hardcoded `Point::NONE`; keep every existing layout fixture byte-identical. Grid track lists, justify-items/self and flow-root already work since rev 15; check grid-template-areas.

- 2026-10-24, orchestrator 6: rev 15 (39) carries the text-run fields and the measure baseline through the ABI, but they do not drive shaping or alignment yet. The pinned Parley TextLayoutRequest and Taffy leaf compute have no slots for them; see TEXT_ABI.md and LAYOUT_ABI.md. This ticket needs a follow-up native change, and so a rebuild that needs the user's approval, before its behaviour can match Chrome. Grid track lists, justify-items/self and flow-root do work in rev 15.

## Answer
