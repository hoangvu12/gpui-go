# Enforce a window minimum size (WM_GETMINMAXINFO)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
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

- [x] A resizable window with `MinSize(600, 640)` cannot be resized
      below 600x640 logical pixels (drag test) and `SetBounds` clamps
      to the same floor.
- [x] A window without `MinSize` behaves exactly as today.
- [x] The clamp tracks the scale factor (DPI change re-derives the
      device-pixel track size).

## Comments

- 2026-10-19 — Claimed and resolved by the pi worker on branch
  `consumer-gaps/04-window-minimum-size` (one ticket, one worktree).

## Answer

Resolved 2026-10-19 (pi worker on `consumer-gaps/04-window-minimum-size`).
`WindowOptions.MinSize Size` (logical client pixels, per-dimension,
zero = unconstrained) mirrors the reference `WindowOptions::window_min_size`
(platform.rs:2595); the Win32 host ports `handle_get_min_max_info_msg`
(events.rs:224-237): WM_GETMINMAXINFO raises `MINMAXINFO.ptMinTrackSize`
to the logical minimum scaled at the current scale factor plus the
frame offset (client-area adjusted), re-derived on every message so a
DPI change re-clamps; a zero minimum falls to DefWindowProcW (today's
path exactly). `SetBounds` and the initial placement also clamp the
client size explicitly (verified: Windows clamps raw SetWindowPos to
ptMinTrackSize too, so the two floors agree by construction). Proven
with real-window tests in `internal/winhostspec/minsize_test.go`
(drag floor through the real window procedure, SetBounds/initial
clamp, no-MinSize regression including the ticket's verified
200x200 fact, synthetic-WM_DPICHANGED re-derivation 1.0→1.5) and
unit tests in `gpui/win32minsize_windows_test.go` (floor math at five
scales, fall-through, per-dimension policy). Evidence:
[evidence/consumer-gaps/ticket04-window-minimum-size.json](../../../evidence/consumer-gaps/ticket04-window-minimum-size.json).
