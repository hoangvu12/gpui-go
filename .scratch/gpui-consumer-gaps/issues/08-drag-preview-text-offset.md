# Text inside drag preview views paints at twice the drag offset (outside the preview card)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
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

## Answer

Resolved 2026-10-20 on `consumer-gaps-2` (commit "Fix drag preview
text: parent-relative bounds add the element offset once").

**Root cause** (as filed): `LayoutEngine.ParentRelativeLayoutBounds`
(gpui/layout.go) resolved the parent's bounds through the offset-aware
`LayoutBounds(ctx, parentID)` — which already adds the pixel-snapped
element offset — and then added the snapped element offset AGAIN at
the tail. The pinned `taffy.rs parent_relative_layout_bounds` reads
the parent's **raw** engine bounds and `window.rs` adds
`pixel_snap_point(element_offset())` exactly once, after the engine
call. Every prepaint-under-offset root was affected: the drag preview
(the only separately-prepainted root) placed its text at 2× the drag
offset — (200,160) with the card at (100,80), usually outside the
window — which read as "the preview's text never paints". The main
root prepaints at offset zero, so only the preview exposed it; text
under a non-zero scroll offset is the same bug's other face (fixed by
the same one-line change).

**Fix**: one line — the parent's bounds come from the raw, offset-free
`e.layoutBounds(parentID, ctx.ScaleFactor())`; the tail still adds the
snapped offset once. No other path double-adds (the drawable's own
bounds, the paragraph paint origin and the inline fragment placement
were already offset-once).

**Verification**:
- `internal/layoutspec TestParentRelativeLayoutBoundsOffsetsOnce` — the
  engine-level pin: a child under a (100,80) element offset reports
  (104,86), never (204,166); the parentless fallback stays raw+offset.
- `gpui TestInteractDragPreviewText*` (3 tests, deterministic) — the
  text element's committed bounds sit inside the preview card, follow
  the cursor across frames, and re-commit inside the card after a
  mid-drag entity notify (a live-updating preview label).
- `internal/authorspec TestDragPreviewTextPaintsInsideCardOnRealWindow`
  — a real window, a synthetic drag (SendMessageW: hover, press,
  threshold-crossing start move, follow-up move), the frame drawn by
  the real frame loop, rendered through the consumer present path
  (RenderScene + ReadPixels with the window's glyph atlas): >= 30 text
  ink pixels INSIDE the preview card and zero deviating pixels in the
  band below it. No real mouse or focus is touched (the window shows
  without activating; all input is in-process messages).
- All three gates were re-run red on the unfixed line (the
  parent-relative query temporarily reverted to the offset-aware form)
  and green after.
- `go build ./...`, `go vet ./...`, and `go test ./... -count=1` green
  except `internal/headlessspec` (the documented environmental gate:
  the gitignored `reference/ce-source` is absent in this worktree).
  The real-input/foreground corpus now skips by default behind
  `GPUI_REAL_INPUT_TESTS=1`, so the full suite runs without touching
  the mouse or focus.

**What mordbard should change**: nothing for the preview — once frame
3d's drag is live again, the lifted song row's number, name and length
paint inside the preview card. (Their drag cursor style, reorder and
drop-slot logic are unaffected.)

## Comments

- 2026-10-20 (pi worker): claimed, implemented, verified, resolved.
