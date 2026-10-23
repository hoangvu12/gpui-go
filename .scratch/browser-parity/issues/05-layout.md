# Layout: how the browser behaves, and how gpui-go differs

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: claimed
Assignee: orchestrator 5 (research worker, batch 2)
Blocked by: none

## Question

Scope: Flexbox, grid, block and inline flow, positioning (absolute, fixed, sticky), overflow and scroll containers, min/max-content sizing, aspect-ratio, gap, intrinsic sizing of text, and rounding to device pixels. gpui-go uses Taffy 0.13: find where Taffy or gpui-go's adaptation differs from Blink LayoutNG. This is the lowest priority.

For each behaviour, report:
- what the spec says (spec section link)
- what Chromium actually does (file:line in the sparse checkout, especially where the spec leaves room)
- what gpui-go does today (gpui/ and reference/native/src file:line)
- the gap, and whether fixing it needs Go or native work, with a rough size

Output: research/browser-parity/05-layout.md, with the top gaps ranked first. Mark claims [V] (verified in source) or [I] (inferred).

## Comments

## Answer
