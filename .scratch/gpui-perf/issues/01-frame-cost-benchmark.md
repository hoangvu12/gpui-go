# Frame-cost benchmark: allocations, native calls, time per frame

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

The "~60k allocations/frame at 1,000 elements" figure is unmeasured. Add repeatable headless Go benchmarks (`testing.B` + `ReportAllocs`) that drive full frames. Each should report allocs/op, bytes/op, ns/op and native calls per frame (a counter in `internal/native` is fine). Cover these scenes:

- (a) 1,000 styled div elements
- (b) a text-heavy page: at least 300 distinct lines and repeated glyphs
- (c) an unchanged-frame redraw, where `Refresh` is called with no state change

Record the baseline at 45dc7e7 in `evidence/perf/ticket01-baseline.json`. Tickets 02-05 report against it.

## Comments

## Answer
