# Hard tabs render the way GPUI's users see them

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 41

## Question

A hard tab paints as the missing-glyph box under the pin (Parley), while Base's 0.2.2 line (DirectWrite) drew blank space at a tab stop (ticket 41). The user asked to follow how GPUI and its users handle it. Research the pin, the 0.2.2 line, Zed's editor (tab map) and the gpui-component input, then implement the closest faithful behaviour.

## Comments

- 2026-10-23 — Created and claimed by the orchestrator at the user's request ("see how gpui or sth do it").

## Answer

Tabs now draw as blank space instead of the missing-glyph box, with no native change.

- **General text in gpui:** follows the default of DirectWrite, which Base's 0.2.2 line used. A tab runs to the next stop, every 4 font-sizes (4 em) from the line start. This was measured headlessly against dwrite.dll.
- **Base Input, Textarea and Editor:** follow the editor tab-map rule. A tab spans tab_size - column % tab_size columns of space width. The cursor, selection and columns are unchanged.

Headless tests fail without the fix and pass with it. Evidence: [ticket44-tab-rendering.json](../../../evidence/base/ticket44-tab-rendering.json).
