# Visual primitives: how the browser behaves, and how gpui-go differs

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: claimed
Assignee: orchestrator 5 (research worker, batch 1)
Blocked by: none

## Question

Scope: CSS transforms (2D/3D, transform-origin), gradients (linear/radial/conic, repeating), mask-image and mask modes, clip-path, border-radius clipping of descendants (overflow + rounded corners), filter and backdrop-filter, box-shadow (outer and inset), opacity and isolation groups, mix-blend-mode, outline and focus rings, image rendering and object-fit.

For each behaviour, report:
- what the spec says (spec section link)
- what Chromium actually does (file:line in the sparse checkout, especially where the spec leaves room)
- what gpui-go does today (gpui/ and reference/native/src file:line)
- the gap, and whether fixing it needs Go or native work, with a rough size

Output: research/browser-parity/01-visual-primitives.md, with the top gaps ranked first. Mark claims [V] (verified in source) or [I] (inferred).

## Comments

## Answer
