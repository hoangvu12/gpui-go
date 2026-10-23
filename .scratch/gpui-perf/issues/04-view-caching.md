# Reuse unchanged views across frames (GPUI view caching)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

Every frame rebuilds the whole tree:

- a fresh scene
- layout reset
- layout, prepaint and paint for every element (`gpui/render.go:90-104`, `:245-321`)

Views only ever paint fresh (`gpui/view.go:328-360`), and `Scene.Replay` (`gpui/scene.go:398`) is unused.

Port the pinned GPUI's view caching faithfully:

- `AnyView::cached(style)` and the view cache keyed by bounds, content mask and text style.
- The `dirty_views` / `notify` invalidation.
- Reuse of the previous frame's layout ids, prepaint state, paint ranges, hitboxes, tooltips, cursor styles and input handlers through the `reuse_prepaint` / `reuse_paint` ranges of the pin's window.rs.
- Replay of the previous scene's primitive ranges into the new scene.

Wire it where the pin's components do: gpui-component and Base use `.cached()` on list items and panes.

This is a parity port, not an invention. Cite the pin's lines. Report ticket 01's scene (c) and a partial-update scene before and after. Rendered output, hit-testing and focus must match the uncached path (add a differential test).

## Comments

## Answer
