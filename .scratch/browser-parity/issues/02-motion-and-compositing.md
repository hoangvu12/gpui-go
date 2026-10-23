# Motion, compositing and frame scheduling: how the browser behaves, and how gpui-go differs

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: claimed
Assignee: orchestrator 5 (research worker, batch 1)
Blocked by: none

## Question

Scope: CSS transitions and animations, Web Animations, the View Transitions API (the browser's layout and shared-element animation), animations that run on the compositor without relayout (transform/opacity), paint invalidation and damage rects, raster tiles and layers, scrolling (scroll-by-copy or tiles, smooth scroll, overscroll, scroll anchoring), BeginFrame/vsync scheduling and present on Windows, and prefers-reduced-motion. Chromium: blink core/animation and paint, cc, components/viz.

For each behaviour, report:
- what the spec says (spec section link)
- what Chromium actually does (file:line in the sparse checkout, especially where the spec leaves room)
- what gpui-go does today (gpui/ and reference/native/src file:line)
- the gap, and whether fixing it needs Go or native work, with a rough size

Output: research/browser-parity/02-motion-and-compositing.md, with the top gaps ranked first. Mark claims [V] (verified in source) or [I] (inferred).

## Comments

## Answer
