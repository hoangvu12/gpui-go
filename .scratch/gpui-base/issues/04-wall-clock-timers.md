# Fire timers and advance App.Now in production

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

`TaskRun.Sleep`, background timers and `App.Now()` move with wall-clock time in a real app, while tests keep the deterministic virtual clock.

## Scope

- The scheduler clock (`gpui/executor.go:44,475-491`) advances only via `TestApp.AdvanceClock`; the production `drive()` only runs `sched.run()` (`gpui/win32host_windows.go:2377-2384`).
- Port the `BackgroundExecutor::timer` semantics (R:executor.rs:212) with a production timer driver waking the foreground dispatcher.

## Acceptance criteria

- [ ] A real-host test: a 50ms sleep wakes within tolerance; `App.Now` advances; tests under `TestApp` remain bit-deterministic.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md §C2](../research/slice-01-infra-overlays.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
