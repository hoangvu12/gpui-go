# Text inside drag preview views paints at twice the drag offset (outside the preview card)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

A drag preview view (the `OnDrag` constructor's returned view) paints
its background, border and rounded corners, but none of its
`Text`/string children paint inside the preview: the preview renders
as an empty card, and a surface readback of the preview's bounds finds
zero glyph pixels. The main window's own text paints fine in the same
frame (mordbard's frame 3d — the lifted song row with its number,
name and length — is blocked on this; the drag itself works).

Verified with a deterministic probe (this repo, interact fixture):
a `Div().OnDrag(value, constructor)` whose preview view renders
`Div().W(200).H(20).Bg(...).Child("Row 0")` follows the cursor with
correct bounds (`PrepaintAsRoot` → debug-selector bounds 200x20 at the
drag offset), the background and border paint, and the text element's
committed paint bounds land at **twice the drag offset** — with the
mouse at (120,100) and the cursor offset (20,20) the preview card
paints at (100,80) while the text's prepaint-committed bounds are
(200,160): far outside the preview (usually outside the window), so
the readback finds no glyph pixels and the preview shows no text.

The cause is `LayoutEngine.ParentRelativeLayoutBounds`
(gpui/layout.go): it computes the parent's bounds through the
offset-aware `LayoutBounds(ctx, parentID)` — which already adds the
pixel-snapped element offset — and then adds the snapped element
offset **again** at the tail. The pinned
`taffy.rs parent_relative_layout_bounds` reads the parent's *raw*
engine bounds and `window.rs` adds the offset exactly once. The drag
preview is the first prepaint-under-offset root (the main root
prepaints at offset zero, so only the preview exposes it; a scrolled
container's text under a non-zero scroll offset is the same bug's
other face).

## What to build

`ParentRelativeLayoutBounds` must derive the parent's origin from the
raw, offset-free engine bounds (the pinned computation) so the window
adds the element offset exactly once. The text element's
parent-relative placement then matches the reference for every
prepaint-under-offset path: drag previews, scrolled containers and
absolutely-positioned content.

## Acceptance criteria

- [ ] A drag preview whose view renders text children shows the text
      inside the preview card, following the cursor (deterministic:
      the text element's committed bounds sit inside the preview's
      debug-selector bounds; real-window: glyph pixels inside the
      preview's bounds in a surface readback).
- [ ] The preview's text re-renders when its view entity is notified
      mid-drag (a live-updating preview label).
- [ ] Text under a non-zero scroll offset paints at the scrolled
      position (single offset), and the main window's text placement
      is unchanged (offset zero).
- [ ] Non-text children of previews keep painting exactly as today.
