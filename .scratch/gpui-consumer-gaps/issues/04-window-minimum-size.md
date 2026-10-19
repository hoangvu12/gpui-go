# Enforce a window minimum size (WM_GETMINMAXINFO)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

`WindowOptions` has no minimum size and the Win32 host never handles
`WM_GETMINMAXINFO`, so nothing bounds a window: `SetBounds(200x200)` on
a 640x820 window produces exactly 200x200 (verified), and a user
dragging the resize frame can shrink a window to any size. The
reference (GPUI-CE) has `window_min_size` in `WindowOptions`; consumer
apps need it (mordbard's design fixes a 600x640 minimum).

## What to build

`WindowOptions.MinSize` (logical pixels; zero = unbounded as today),
enforced in the Win32 host by clamping `MINMAXINFO.ptMinTrackSize`
(client-area adjusted) for `WM_GETMINMAXINFO`, for both interactive
resizing and `SetBounds`. Per-monitor DPI changes should re-clamp.

## Acceptance criteria

- [ ] A resizable window with `MinSize(600, 640)` cannot be resized
      below 600x640 logical pixels (drag test) and `SetBounds` clamps
      to the same floor.
- [ ] A window without `MinSize` behaves exactly as today.
- [ ] The clamp tracks the scale factor (DPI change re-derives the
      device-pixel track size).
