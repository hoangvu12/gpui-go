# Stop Text elements leaking native shaping handles on every frame

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

`textElement.RequestLayout` shapes a fresh `WrappedLine` (a native
shaping handle) every frame and never disposes or reuses it
(gpui/div.go: the measure closure calls `frameTextSystem(w).shape(...)`
into a new `shapedCell` per frame; `ShapeText` allocates a new native
handle each call, and only the failure path disposes). The native
service's live-handle bound is 256 (`MaxShapingHandles`). Any window
whose frames redraw with text — hover, notify, animation — exhausts the
bound and the frame loop dies in layout with
"gpui: text element shaping failed: ErrTextShapingLimit".

Verified from a consumer program (mordbard's readiness spike,
2026-10-19): 17 forced redraws of a 7-`Text` view (~119 new handles)
were enough — after that every further frame fails and a fresh
`ShapeText` itself returns the limit error. The reference (GPUI-CE)
caches shaped lines in the TextSystem (`shape_text` with a
shaped-line cache keyed by layout parameters) so redraws do not
re-shape.

## What to build

A cross-frame shaped-line cache in `TextSystem` (the reference's
shaped-line LRU semantics: same text, font, size, wrap width and line
clamp reuse the shaped line), or failing that, disposal of the replaced
line in the element path. The element path must reuse shaped results
when the inputs are unchanged across frames.

## Acceptance criteria

- [ ] A real window with a `Text` element inside a continuous
      `WithAnimation` loop runs for at least 10,000 frames without
      `ErrTextShapingLimit` and without unbounded handle growth.
- [ ] Scrolling a 300-row `UniformList` of text rows (rows entering and
      leaving the visible range) for thousands of frames stays under
      the live-handle bound.
- [ ] Resizing a window with text (wrap widths changing) does not leak
      the superseded lines.
