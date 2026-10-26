# 43 - Text wrapping options and alignment

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 39
Triage: ready-for-agent

**What to build:** word-break, overflow-wrap and text-wrap: balance work, and text aligns with justify, start and end.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [ ] Fixtures for long URLs, CJK text and balanced headings match Chrome's line breaks.
- [ ] Justified paragraphs match Chrome within tolerance.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-26, orchestrator 11 (the approach, chosen by the orchestrator as the handoff asked): fold into the rev 16 rebuild on `native/r16`. Vendor the pinned adapter's paragraph layout (`gpui_ce_parley` `parley_paragraph_layout`) into reference/native the way R15 vendored the shaders, keeping the ONE FontContext (FontId provenance check), with a side-channel request entry so the pinned gpui types stay untouched; drive word-break, overflow-wrap and justify/start/end through it. text-wrap: balance stays Go-side bisection (the rev-15 record's contract). Locale and font width/variations are out of this ticket's acceptance.

- 2026-10-24, orchestrator 6: rev 15 (39) carries the text-run fields and the measure baseline through the ABI, but they do not drive shaping or alignment yet. The pinned Parley TextLayoutRequest and Taffy leaf compute have no slots for them; see TEXT_ABI.md and LAYOUT_ABI.md. This ticket needs a follow-up native change, and so a rebuild that needs the user's approval, before its behaviour can match Chrome. Grid track lists, justify-items/self and flow-root do work in rev 15.

## Answer
