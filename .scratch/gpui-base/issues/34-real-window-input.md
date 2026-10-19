# Route real-window mouse input into element dispatch

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port orchestrator (wave 1c)
Blocked by: 06, 33

## Question

On a real Win32 window, clicking, hovering and dragging over divs dispatch through the element tree exactly as they do in test windows.

## Scope

- Finding (ticket 06, confirmed by the orchestrator): `gpui/win32host_windows.go` routes no `WM_*BUTTONDOWN/UP` messages into element dispatch; the core's input evidence drove dispatch synthetically.
- Ticket 06's wiring: route the eight button messages to `w.handleMouseButtonMsg(...)`, add `hostWindow.onMouseInput` / `WindowOptions.OnMouseInput`, `forgetWin32ClickState` at WM_NCDESTROY, `clickState().systemUpdate` at WM_SETTINGCHANGE (gpui_windows events.rs:138-149,489-559,1220); port the WM_MOUSEMOVE translation (events.rs:371-398) and wheel routing if missing; `window.go` OpenWindow dispatches through `window.dispatchInput` inside `a.Update`.
- (moved to ticket 08, which owns render.go) `render.go`: drag-preview prepaint/paint in `drawRoots` (window.rs:3451-3456,3474-3475); `w.resetCursorStyle(app)` at frame completion (window.rs:3277).
- `key_dispatch.go`: `w.beginPlatformInputDispatch(event, app)` at the start of key dispatch (window.rs:5746-5760).
- Ticket 06 deviation to close: the pinned hover-gated `on_mouse_move` and button-filtered `on_mouse_up` names (move existing receive-everything callers to the `...All` names).

- Harden `TestFrameLoopRealWindowNotifyRepaints`: under the full `./gpui` run it once saw one extra frame inside its 250ms idle window (10/10 isolated passes) — attribute repaints from neighbouring real-window tests or OS-initiated WM_PAINT instead of assuming exactness.

- From ticket 07: WM_MOUSEWHEEL/WM_MOUSEHWHEEL routing (gpui_windows events.rs:150-151,562-651) with a real-window wheel test over a tracked scroll div; expose the scroll builders through the Interactive mixin.

## Acceptance criteria

- [ ] Real-window tests with synthetic Win32 messages (never the real mouse) prove click, hover-style, drag-with-preview and cursor changes end to end; they FAIL not skip.
- [ ] Full corpus green apart from the documented environmental classes; evidence in `evidence/base/ticket34-*.json`.

## Comments

- 2026-10-19 — Opened by the orchestrator from ticket 06's report.
