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
- [View caching](issues/04-view-caching.md): `gpui.CachedView` ports the pin's `cached()` with dirty views, `reuse_prepaint`/`reuse_paint` and scene replay, checked by a cached-vs-uncached differential test. With panes cached, a root-only redraw drops from 9.8 to 3.7 ms on the 1,000-div page and from 236 to 20 ms on the text page. `Refresh` (scene (c)) still bypasses caching, as in the pin.

## Not yet specified

- Present1 dirty rects: ticket 05 investigates them, and they're implemented only if correct under the flip model together with ticket 04's damage information.

## Out of scope

- Inactive-window throttling: not raised by the research.
