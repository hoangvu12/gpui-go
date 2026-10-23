# Components show the pinned cursors on real windows

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: none

## Question

Hovering a Base component on a real window shows the cursor upstream shows: the pointer on buttons and links, the I-beam on inputs, the resize cursors on resizable handles and dock splitters, and so on. The user reports that cursors are missing.

## Scope

- Base: upstream sets a cursor in 16 places (cursor_pointer, cursor_text, cursor_col_resize, cursor_row_resize, CursorStyle::...). The port sets one in 9. Port each missing site.
- Core: confirm the div cursor style (ticket 06) reaches the Win32 host's WM_SETCURSOR on a real window (gpui/desktop_windows.go, gpui/app.go SetCursorStyle). The pinned path is window.rs set_cursor_style / the frame's cursor_styles plus platform set_cursor_style. Fix any gap.

## Acceptance criteria

- [ ] Every site cites upstream or the pin. A deterministic test checks the cursor style requested per hovered element, without visible windows. The user is gaming on the main monitor.
- [ ] gpui and base green, with evidence in `evidence/base/ticket39-*.json`.

## Comments

- 2026-10-23 — Created by the base-port-2 orchestrator from the user's report.
