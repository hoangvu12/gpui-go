# A fresh window's hover shows no cursor: hostWindow.cursor is never seeded, so WM_SETCURSOR answers SetCursor(NULL)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: pi worker
Blocked by: none

## Question

The user report (mordbard): the mouse cursor disappears whenever it
hovers the app window.

Root cause (verified in this worktree against the pinned reference
under `evidence/windows-platform/`): the per-window cursor handle is
never initialized. `newHostWindow` (`gpui/win32host_windows.go`)
builds the `hostWindow` record at WM_NCCREATE without touching
`cursor`, so it stays 0. `handleSetCursorMsg` (`gpui/desktop_windows.go`,
events.rs handle_set_cursor) then runs on every WM_SETCURSOR over the
client area:

```go
var cursor uintptr
if w.host.desktop.cursorVisible.Load() {
    cursor = w.cursor        // 0 for every fresh window
}
procSetCursor.Call(cursor)   // SetCursor(NULL) -> invisible cursor
```

`Host.SetCursorStyle` (platform.rs set_cursor_style) only posts
`WM_GPUI_CURSOR_STYLE_CHANGED` when the newly loaded handle differs
from the platform's `currentCursor`, and host boot
(`initDesktopStateOnHostThread`) already sets that to the Arrow handle
(WindowsPlatformState::new). So an application that never requests a
different style — mordbard's case — never delivers the message, and
every window keeps `cursor` = 0 forever: hovering the window calls
`SetCursor(NULL)` on every WM_SETCURSOR and the cursor vanishes. The
hide-until-moves restore path is poisoned the same way
(`restoreCursorAfterHide` re-runs `SetCursor(w.cursor)` = NULL).

The reference does not have this bug:
`WindowsPlatform::generate_creation_info` (platform.rs:232) passes the
platform's *current* cursor into `WindowCreationInfo.current_cursor`,
`WindowsWindow::new` copies it into the `WindowCreateContext`, and
`WindowsWindowState::new` (window.rs:176) seeds every window's
`current_cursor` cell from it. So a window born while the platform
style is, say, IBeam starts with the IBeam handle; a window born under
the boot default starts with Arrow. `SetCursorStyle(Arrow)` explicitly
(say, from an app's style resolution) still early-returns — and that
is correct, because every window already carries the platform's
current handle.

## What to build

Seed the per-window cursor at creation, exactly like the reference:
`newHostWindow` initializes `cursor: h.desktop.currentCursor` (the
host-thread platform state; boot already guarantees it is the Arrow
handle, and a style set before the window opens is the window's
birth style). The message policies stay pinned: WM_SETCURSOR keeps
answering with the window's handle, WM_GPUI_CURSOR_STYLE_CHANGED keeps
replacing it, hide-until-moves keeps restoring it.

Also harden the two cursor-gate tests the orchestrator reported as
~10 s timeouts on `base-port` (`TestSetCursorMessagePolicy` in gpui,
`TestCursorStyleAndVisibilityAppLevel` in internal/desktopspec):
verified NOT caused by this bug — their assertions never observe the
window's cursor handle, and both pass on base-port in a healthy shell
session. The timeouts are the documented environmental one-shot-gate
class (wave 4 / ticket26): both tests show a default-position window
without activation and wait one-shot for the shared
cursor-visibility flag to clear after `HideCursorUntilMouseMoves`; a
real WM_MOUSELEAVE delivered to that window (the live mouse crossing
the shown window — interactive machine use, the user gaming) runs
`handleMouseLeaveMsg`, which stores `cursorVisible = true` again, and
the one-shot wait then times out at its 10 s budget. Treatment is the
established bounded re-drive pattern (`desktopDriveAndWaitFor`): re-hide
before every poll so the driven state is re-established; message-level
assertions stay one-shot and deterministic. The tests keep touching
neither the real mouse nor the foreground (SW_SHOWNOACTIVATE windows
and synthetic SendMessageW stay ungated per `internal/realinput`).

## Acceptance criteria

- [ ] A freshly created window, with no SetCursorStyle call, answers a
      synthetic WM_SETCURSOR over its client area with the Arrow
      cursor: the message is consumed (0), the window record's cursor
      is the Arrow handle (non-zero), and the global cursor handle the
      handler installed (GetCursor) is the Arrow.
- [ ] A window created while the platform cursor style is IBeam takes
      the IBeam handle at creation (the reference's
      generate_creation_info semantics).
- [ ] `TestSetCursorMessagePolicy` and
      `TestCursorStyleAndVisibilityAppLevel` pass, with their
      hide/restore gates re-driven so a real WM_MOUSELEAVE cannot
      poison them; they still move no real input and take no focus.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` green except
      the documented environmental gate (`internal/headlessspec` needs
      the gitignored `reference/ce-source`).

## Answer

## Comments
