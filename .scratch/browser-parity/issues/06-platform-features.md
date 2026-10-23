# Platform features: how the browser behaves, and how gpui-go differs

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: claimed
Assignee: orchestrator 5 (research worker, batch 2)
Blocked by: none

## Question

Scope: The popover API, the dialog element and the top layer, select and menu behaviour (positioning, keyboard, typeahead), tooltips (the title attribute), spellcheck, system colours and color-scheme, forced-colors and prefers-contrast, text size and zoom, context menus, clipboard events, file drop from the OS, and native form-control behaviours that apps rely on (caret blink, selection colours).

For each behaviour, report:
- what the spec says (spec section link)
- what Chromium actually does (file:line in the sparse checkout, especially where the spec leaves room)
- what gpui-go does today (gpui/ and reference/native/src file:line)
- the gap, and whether fixing it needs Go or native work, with a rough size

Output: research/browser-parity/06-platform-features.md, with the top gaps ranked first. Mark claims [V] (verified in source) or [I] (inferred).

## Comments

## Answer
