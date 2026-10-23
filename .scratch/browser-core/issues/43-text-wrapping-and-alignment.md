# 43 - Text wrapping options and alignment

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 39
Triage: ready-for-agent

**What to build:** word-break, overflow-wrap and text-wrap: balance work, and text aligns with justify, start and end.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [ ] Fixtures for long URLs, CJK text and balanced headings match Chrome's line breaks.
- [ ] Justified paragraphs match Chrome within tolerance.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: rev 15 (39) carries the text-run fields and the measure baseline through the ABI, but they do not drive shaping or alignment yet. The pinned Parley TextLayoutRequest and Taffy leaf compute have no slots for them; see TEXT_ABI.md and LAYOUT_ABI.md. This ticket needs a follow-up native change, and so a rebuild that needs the user's approval, before its behaviour can match Chrome. Grid track lists, justify-items/self and flow-root do work in rev 15.

## Answer
