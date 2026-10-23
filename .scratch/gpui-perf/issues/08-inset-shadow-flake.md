# Intermittent "host loop exited" in an inset-shadow real-GPU test

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: needs-info
Status: open
Assignee: unassigned
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

## Answer
