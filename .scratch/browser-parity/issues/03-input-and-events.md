# Input and events: how the browser behaves, and how gpui-go differs

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: claimed
Assignee: orchestrator 5 (research worker, batch 1)
Blocked by: none

## Question

Scope: Pointer events (pointerenter/leave/over/out, capture, coalesced events), mouse event order, the full HTML drag and drop sequence (dragstart, drag, dragenter, dragover with dropEffect, dragleave, drop, dragend), click and dblclick rules, focus (tab order, focus-visible, focus delegation, inert), keyboard events (key vs code, the Ctrl+Shift+] case on Windows), IME composition events, text selection, wheel and touchpad, and cursors. Chromium: blink core/input, core/events, core/page/drag_controller, ui/events.

For each behaviour, report:
- what the spec says (spec section link)
- what Chromium actually does (file:line in the sparse checkout, especially where the spec leaves room)
- what gpui-go does today (gpui/ and reference/native/src file:line)
- the gap, and whether fixing it needs Go or native work, with a rough size

Output: research/browser-parity/03-input-and-events.md, with the top gaps ranked first. Mark claims [V] (verified in source) or [I] (inferred).

## Comments

## Answer
