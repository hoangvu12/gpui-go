# Simulate input in tests the way Base's tests do

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 03, 06

## Question

Ported Base tests can call `AddWindowView`, `SimulateKeystrokes("cmd-a backspace")`, `SimulateMouseDown/Move/Up/Click` and read per-element a11y info and computed styles.

## Scope

- Port R:app/test_context.rs:313,520,816,827,872,910,947 and R:app/test_app.rs:399-443 over `Window.DispatchInput` + `DrawWindowFrame`; `Interactivity.compute_style` (R:elements/div.rs:3882).

## Acceptance criteria

- [ ] The helpers drive a div click, a keystroke chord and a drag deterministically.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md §C11](../research/slice-01-infra-overlays.md)
- [slice-03-composite-controls.md C19](../research/slice-03-composite-controls.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
