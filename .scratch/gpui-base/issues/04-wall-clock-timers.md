# Fire timers and advance App.Now in production

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port orchestrator (wave 1a)
Blocked by: none

## Question

`TaskRun.Sleep`, background timers and `App.Now()` move with wall-clock time in a real app, while tests keep the deterministic virtual clock.

## Scope

- The scheduler clock (`gpui/executor.go:44,475-491`) advances only via `TestApp.AdvanceClock`; the production `drive()` only runs `sched.run()` (`gpui/win32host_windows.go:2377-2384`).
- Port the `BackgroundExecutor::timer` semantics (R:executor.rs:212) with a production timer driver waking the foreground dispatcher.

## Acceptance criteria

- [x] A real-host test: a 50ms sleep wakes within tolerance; `App.Now` advances; tests under `TestApp` remain bit-deterministic.
- [x] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [x] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [x] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [x] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md §C2](../research/slice-01-infra-overlays.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).

## Answer

Resolved 2026-10-19 (wave 1a worker, verified by the orchestrator). The real host switches the scheduler to monotonic wall time at attach; one `time.AfterFunc` for the earliest deadline wakes the existing dispatcher, whose drive loop catches the clock up, fires due timers and re-arms; `App.Shutdown` stops the driver; TestApp keeps the virtual clock bit-deterministically. 7 tests (2 upstream ports) + taskspec/shutdownspec green. Deviations: whole-millisecond deadlines, AfterFunc instead of the threadpool timer. Evidence: [evidence/base/ticket04-wall-clock-timers.json](../../../evidence/base/ticket04-wall-clock-timers.json).
