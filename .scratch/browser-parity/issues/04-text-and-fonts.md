# Text and fonts: how the browser behaves, and how gpui-go differs

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: resolved
Assignee: orchestrator 5 (research worker, batch 2)
Blocked by: none

## Question

Scope: Font matching (CSS Fonts 4), weight and style synthesis (font-synthesis), the fallback chain, shaping, line breaking (UAX 14 and CJK rules), text-overflow ellipsis, white-space, letter-spacing, font-feature-settings and tabular numbers, text decoration, and how Chrome rasterizes text on Windows (Skia + DirectWrite: gamma, contrast, subpixel AA, hinting) against gpui-go's native DirectWrite path. Chromium: blink platform/fonts, platform/text, core/layout/inline; Skia src/ports (SkScalerContext_DW, SkFontMgr_win_dw).

For each behaviour, report:
- what the spec says (spec section link)
- what Chromium actually does (file:line in the sparse checkout, especially where the spec leaves room)
- what gpui-go does today (gpui/ and reference/native/src file:line)
- the gap, and whether fixing it needs Go or native work, with a rough size

Output: research/browser-parity/04-text-and-fonts.md, with the top gaps ranked first. Mark claims [V] (verified in source) or [I] (inferred).

## Comments

## Answer

Answer: research/browser-parity/04-text-and-fonts.md (merged 2026-10-23).
