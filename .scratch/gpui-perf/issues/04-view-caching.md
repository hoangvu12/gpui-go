# Reuse unchanged views across frames (GPUI view caching)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: gpui-perf W2
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

Ported on branch `perf-viewcache` (commits 97a9936, cb3a25d; ticket 01's d1046f8 merged in for the harness). R: below is `reference/ce-source/crates/gpui/src` at 254b5dbd.

### What was ported

- **`gpui.CachedView(view, style)`** (`gpui/view_cache.go`) is `AnyView::cached` / `Entity::cached` (R:view.rs:34-41, 277-289, 316-328). It panics for views without an entity, like the pin's crate-private `ViewElement::cached`. Usage: `gpui.CachedView(gpui.ViewOf[Panel](panel), gpui.Styles().SizeFull())`.
- **The cached view element phases** (`gpui/view.go`) follow R:view.rs:368-537:
  - request_layout asks for one node at the cached style and doesn't render;
  - prepaint calls `set_view_id`, then checks the cache key (bounds, content mask, text style), `dirty_views` and `refreshing`. On a hit it reuses the prepaint range and extends the accessed entities. On a miss it renders, lays out as a root at the bounds and prepaints at the origin, all under `refreshing` and `detect_accessed_entities`;
  - paint either paints the fresh element under `refreshing` or replays the paint range;
  - caching is off while the inspector is picking.
- **Prepaint and paint indices** follow R:window.rs:1123-1142 and 3712-3822:
  - `prepaintStateIndex` (`gpui/window.go`) gains the accessed-element-states and line-layout indices.
  - `paintIndex`, `reusePrepaint` and `reusePaint` are new in `gpui/view_cache.go`.
  - Prepaint reuse carries hitboxes, accessed element states, line layouts, the dispatch subtree, tab stops and deferred draws.
  - `reuseDispatchSubtree` / `moveDispatchNode` port R:key_dispatch.rs:246-308. Deferred-draw parents are renumbered with `refresh_node_id`.
  - Paint reuse carries cursor requests, input handlers, mouse listeners, accessed element states, line layouts, inline facts, and `Scene.Replay` of the previous scene's operation range.
  - Deferred draws now record and replay their own ranges (R:window.rs:3580-3703, `gpui/deferred.go`).
- **Invalidation** follows the pin:
  - `invalidateViewWindows` records the notified entity even while the window draws (R:window.rs:189-214).
  - The draw start walks it through the rendered dispatch tree's `viewPathReversed` into `dirtyViews` (R:window.rs:3316-3322, 2210-2222; R:key_dispatch.rs:588-596).
  - `dirtyViews` is cleared after drawRoots (R:window.rs:3197).
  - `Window.Refresh`, `requestRefresh` and the app refresh effect set `refreshing` (R:window.rs:2287-2292; R:app.rs:1896-1903). It is cleared when a frame completes (R:window.rs:3278).
- **Line-layout cache** (`gpui/text_cache.go`): `layoutIndex`, `reuseLayouts` and `truncateLayouts` port R:text_system/line_layout.rs:957-1006. `transact` now also truncates the element-state log and the line uses, as R:window.rs:3968-3989 does.

### Deviations (recorded in the code)

- **Tab stops** are registered during prepaint in the port (`div.go`), so they belong to the prepaint range rather than the pin's paint index. There are no tooltip requests to carry, because the port has none.
- **Line uses:** the port also records hits in the current frame. Otherwise a reused view could lose a `*WrappedLine` that was first shaped elsewhere, because Go has no Arc refcount. This only extends line lifetimes.
- **Accessed element states:** the frame now keeps an ordered log beside the set.
- **Abandoned frames:** a failed or panicking frame leaves the window `refreshing`, because the pin's draw cannot fail. Its rewritten ranges are then never trusted.
- **Same gaps as the pin:** debug bounds and a11y nodes are not carried by reuse (R:window.rs:3723-3772 doesn't carry them either). A click with no prior mouse move leaves a reused view's hover paint stale, exactly as in the pin.

### Wiring

- Base upstream (`upstream-base/ce/crates/base/src`) uses `.cached()` only in two input tests, U:input/base/state.rs:4957-5019 and 5123-5192. Both are now ported and passing in `base/input_cached_view_test.go`.
- The Base dock (`dock/tab_group.rs`) does not cache, so the Go dock is unchanged.
- gpui-component's `TabPanel` / `Dock` (`.cached()` at tab_panel.rs:1033 and dock.rs:415), and the kit/shell crates, have no Go port.
- The pin's own `.cached()` test, R:elements/deferred.rs:193-264, now runs cached in `TestDeferredNestedDrawsWithReusedViews`.

### Tests

- **`TestViewCacheDifferential`** (`gpui/view_cache_test.go`) drives a cached and an uncached window through 15 identical steps. It runs on both the deterministic and the real text stacks. The steps are:
  - root re-render
  - partial update
  - hover, clicks and hover-out
  - focus and FocusNext
  - reuse with focus inside a reused view
  - Refresh
- After every step it requires identical output:
  - quads, glyph sprites and plan with draw orders;
  - hitboxes and hit tests on a sample grid;
  - the dispatch tree, mouse listeners, tab stops and cursor;
  - the focused handle and its context path;
  - delivered clicks and deferred draws.
- It also asserts that the reuse actually happens: 6 of 6 panels on a root re-render, and 5 reused plus 1 rendered on a partial update.
- Other tests cover the refresh and dirty-view rules, a change of cache key, and that a cache needs an entity.

### Numbers

Full figures are in `evidence/perf/ticket04-viewcache.json`. Before and after are the best of 3 runs each, back to back on the same machine.

| Scene | Before (ms / allocs / native calls) | After (ms / allocs / native calls) |
|---|---|---|
| 1,000 divs in 10 cached panes, root notified only | 9.84 / 16,526 / 8,067 | 3.71 / 611 / 97 |
| Same, partial update (1 of 10 panes changes) | 8.75 / 16,532 / 8,067 | 3.86 / 2,265 / 900 |
| 300 lines in 15 cached panes, root notified only | 235.9 / 86,239 / 137,365 | 19.6 / 882 / 142 |
| Same, partial update | 195.6 / 86,243 / 137,365 | 30.9 / 6,578 / 9,313 |
| Ticket 01 scene (c), `Refresh`, divs | 9.06 | 8.27 (unchanged by design) |
| Ticket 01 scene (c), `Refresh`, text | 212.2 | 191.0 (unchanged by design) |

- Scene (c) uses `Refresh`, which bypasses every view cache in the pin (R:view.rs:435). It is therefore unchanged, within noise.
- On a refresh, cached panes cost about 2% more allocations (16,891 vs 16,526) for the bookkeeping on a miss.
- Primitive counts match on both sides of every row.

### Not done

- No damage information was exported for ticket 05's Present1 dirty rects. The bounds of a reused view are the natural source for it.
- The ms figures are wall time, so they are noisy. The allocation and native-call counts are deterministic.
