# Intermittent "host loop exited" in an inset-shadow real-GPU test

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: needs-info
Status: resolved
Assignee: gpui-perf W4
Blocked by: none

## Question

`TestInsetShadowBlurPaintsFromPaddingEdgeOnRealGPU` (scenespec) sometimes fails in cleanup with "host loop exited". The host loop had already exited before `window.Close` was posted.

| Code | Failures |
|---|---|
| perf-go-a (W1's Go changes only) | 3 in early runs, then 0 of ~70 |
| combined gpui-perf tree | 1 of 60 |
| pre-effort 9bec2ec | 0 of 40 (plus 0 of 24 earlier) |

The rates are too low to attribute. W1's first failures happened before W3's frame-pacer changes were merged, so the pacer alone is not the cause.

Find out whether the test's host-loop teardown races, or a gpui-perf change shortened the host loop's lifetime. Capture the log of a failing run before changing anything.

## Comments

- 2026-10-23: created by the orchestrator when integrating gpui-perf; the integration was not blocked on it. Note: the full suite passed on the combined tree.

- 2026-10-23: claimed by gpui-perf W4 (worktree `gpui-go-wt-flake`, branch `fix-inset-flake`).

## Answer

**This is a test-teardown race, not a runtime bug. The gpui-perf changes didn't shorten the host loop's lifetime; they only shift the timing.** Evidence: [ticket08-inset-flake.json](../../../evidence/perf/ticket08-inset-flake.json).

**Failing log, captured before any change.** Captured with `GPUI_WINHOST_TRACE=1` and `-test.count=150` over the three `TestInsetShadow*` tests; the first failure came at iteration 150. The trace:

1. The cleanup's sync posts `window.Close`, which only posts `WM_CLOSE`.
2. The host processes `WM_CLOSE`, `WM_DESTROY` and `WM_NCDESTROY`.
3. The host logs `quit-on-last` and then `loop exit WM_QUIT`.
4. Only then does the test's first `Alive` poll call `host.Post`. Post correctly returns false, and the sync helper calls `t.Fatalf("foreground post was dropped")`.

In passing runs the poll's wake message is queued before the close-one-window message. The poll then reads `alive=false` while the loop is still up.

**Root cause.** `renderInsetPanel` polls `window.Alive()` through a sync helper that fails the test when a post is dropped. The poll runs after closing the host's only window, and closing the last window quits the host loop by design (`quitOnLastWindow`, as in the reference). The test therefore loses whenever its goroutine is slower than the destroy-and-quit sequence. All three inset tests share the helper, so all three were exposed, not only the blur one.

**Same pattern elsewhere.** `internal/authorspec/drag_preview_text_window_test.go` has the identical cleanup, so it had the same latent race. Every other real-window cleanup polls `!window.Alive()` directly. That is safe because `Alive` reports false on a stopped host: `queryForeground` returns `ErrHostStopped`.

**Fix.** Both cleanups now poll `!window.Alive()` directly. There is no runtime change.

**Regression test.** `winhostspec.TestAlivePollSurvivesQuitOnLastWindow` forces the losing side of the race: the last window is closed and the loop has already exited before the first poll. It asserts that `Post` reports the drop and that `Alive` reads false.

**Rates.** Each side ran `-test.count=400` over the three tests in one binary, with real windows, sequentially on the same machine.

| Code | Failures |
|---|---|
| before (61d7ec0) | 6 of 1,200 (blur 3/400, blur-zero 2/400, borderless 1/400), all "post was dropped" |
| after | 0 of 1,200 (the target test: 0 of 400) |
