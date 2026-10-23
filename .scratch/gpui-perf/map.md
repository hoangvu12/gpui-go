# gpui-perf: runtime costs found in the zgui / GPUI Fast research

Labels: wayfinder:map

## Destination

On 2026-10-23 the user accepted every proposal in [the research](../../research/zgui-vs-gpui-fast/README.md): "alright try to fix all of them, im not using pc so its all urs".

The goal is to cut gpui-go's per-frame CPU, native-call, allocation and startup costs, plus the presentation latency, without changing rendered output. The weight-500 fake-bold fix is in scope (a native rebuild).

## Notes

- Integration branch: `base-port-2`. The workers use worktrees `gpui-go-wt-perf-go-a`, `gpui-go-wt-perf-viewcache` and `gpui-go-wt-perf-native`, all branched from 45dc7e7.
- The user isn't at the PC, so real-window tests are allowed.
- The native rebuild is user-approved for this effort only, and is one rebuild (ticket 05).
- Bit-exact fixtures (textspec, glyphspec, scenespec and similar) must stay unchanged unless a change is intended and explained. Intended changes are the weight-500 face choice and nothing else.

## Decisions so far

- [Frame-cost benchmark](issues/01-frame-cost-benchmark.md): `gpui/perf_bench_test.go` + `native.CallCount`. Baseline: 1,000 divs take 16.5k allocs and 8k native calls per frame; the 300-line text page takes 82k allocs, 129k native calls and about 200 ms per frame; an unchanged refresh costs the same as a rebuild.
- [Native load once](issues/03-native-load-once.md): `native.Load` shares one verified load per source while a handle is open, and re-verifies after the last close. The first frame dropped from 391 to 248 ms (median).
- [Glyph raster cache](issues/02-glyph-raster-cache.md): the pin's raster_metadata cache and atlas lookup-before-raster are ported Go-side, plus pooled single-call rasters. The text page dropped from 129k to 23.5k native calls, 82k to 17k allocs and 200 to 83 ms per frame; sprites are identical.
- [Frame allocations](issues/06-frame-allocations.md): per-element style boxes, a by-value layout context, allocation-free strips, the a11y field and cached line paint dumps (so parked lines no longer re-shape). Divs dropped from 16.5k to 5.4k allocs per frame; the text page from 17.4k to 8.4k allocs and 83 to 31 ms.
- [Scene leak](issues/07-scene-leak.md): a replaced, abandoned or closed-window scene is disposed (after the cached-view replay). Live scenes stay at 1 per window, shown by `LiveSceneCount`.
- [View caching](issues/04-view-caching.md): `gpui.CachedView` ports the pin's `cached()` with dirty views, `reuse_prepaint`/`reuse_paint` and scene replay, checked by a cached-vs-uncached differential test. With panes cached, a root-only redraw drops from 9.8 to 3.7 ms on the 1,000-div page and from 236 to 20 ms on the text page. `Refresh` (scene (c)) still bypasses caching, as in the pin.
- [Native rebuild rev 13](issues/05-native-rev-perf-and-weight500.md): DLL `0b6e9265…`. Batched scene inserts cut 1,000-div frames from 8,037 to 3,048 native calls. Waitable swap chains start the first frame after idle immediately (0.5 ms notify-to-present against about 4.5 ms). Weight 500 now uses the real Regular face (no fake bold). Present1 dirty rects are deferred until ticket 04 provides damage information.

## Not yet specified

- Present1 dirty rects: ticket 05 investigates them, and they're implemented only if correct under the flip model together with ticket 04's damage information.

## Out of scope

- Inactive-window throttling: not raised by the research.
