# Frame-cost benchmark: allocations, native calls, time per frame

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: gpui-perf W1
Blocked by: none

## Question

The "~60k allocations/frame at 1,000 elements" figure is unmeasured. Add repeatable headless Go benchmarks (`testing.B` + `ReportAllocs`) that drive full frames. Each should report allocs/op, bytes/op, ns/op and native calls per frame (a counter in `internal/native` is fine). Cover these scenes:

- (a) 1,000 styled div elements
- (b) a text-heavy page: at least 300 distinct lines and repeated glyphs
- (c) an unchanged-frame redraw, where `Refresh` is called with no state change

Record the baseline at 45dc7e7 in `evidence/perf/ticket01-baseline.json`. Tickets 02-05 report against it.

## Comments

## Answer
Benchmarks are in `gpui/perf_bench_test.go` (`BenchmarkFrameDivs`, `BenchmarkFrameText`, `BenchmarkFrameRefreshDivs`, `BenchmarkFrameRefreshText`). The native call counter is `native.CallCount()` (`internal/native/callcount.go`). Every artifact call goes through `callStatus` once, and that function increments the counter (`layout_windows.go`). The Win32 calls in `win.go` aren't counted.

The scenes run in a hostless test window (2400x1800, scale 2) that draws through the real text system: native shaping, glyph rasterization and the window atlas. Each frame goes through `SimulateFrameRequest` or the test flush, and each step checks that exactly one frame was drawn.

- (a) `FrameDivs`: 10 child views with 100 styled leaf divs each. Each div has a size, margin, background, border and rounded corners. Every frame notifies the root and all panes.
- (b) `FrameText`: 300 distinct lines in 3 columns at 12px, made of repeated glyphs. Every frame notifies the root.
- (c) `FrameRefreshDivs` and `FrameRefreshText`: `Window.Refresh` with no state change, then one frame request.

`TestPerfScenesDraw` keeps the scenes honest in the normal test run. It checks that the div scene draws at least 1,000 quads and the text scene at least 6,000 sprites. `TestPerfFrameReport` writes the evidence JSON when `GPUI_PERF_OUT` is set (`GPUI_PERF_RUNS` sets the number of runs, default 3). It keeps the best run by ns/frame.

Baseline (`evidence/perf/ticket01-baseline.json`): the code at 9bec2ec is identical to 45dc7e7, plus the counter. It's the best of 5 runs on an i5-14400F.

| scene | ms/frame | allocs/frame | B/frame | native calls/frame | quads | sprites |
|---|---|---|---|---|---|---|
| (a) FrameDivs | 11.3 | 16,476 | 5.02 MB | 8,037 | 5,000 | 0 |
| (b) FrameText | 200.1 | 81,583 | 18.7 MB | 129,292 | 0 | 18,252 |
| (c) FrameRefreshDivs | 10.1 | 16,418 | 5.02 MB | 8,037 | 5,000 | 0 |
| (c) FrameRefreshText | 200.8 | 81,574 | 18.7 MB | 129,292 | 0 | 18,252 |

The allocation, byte and native-call counts are deterministic. ns/frame is noisy when other work shares the machine: a concurrent run measured 2x higher. Findings:

- The "~60k allocations/frame at 1,000 elements" figure is wrong for divs: the real count is about 16.5k. The text page is the expensive case, at about 82k.
- About 7 native calls per glyph (129k for 18k sprites) confirm ticket 02's target.
- An unchanged refresh costs the same as a full rebuild, which is ticket 04's target.
