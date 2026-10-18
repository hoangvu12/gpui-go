# Port the single-line Input

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 10, 12, 18, 19

## Question

A focused `Input` edits, selects, scrolls, validates (pattern/mask/number), copies/pastes and composes through IMM32 like upstream.

## Scope

- Base files: input/base/{kind,mode,blink_cursor,movement,state,element,layout,mod}.rs, input/input/mod.rs, input/mod.rs. `kind.rs` lands with its full hook surface so editor features plug in later. IME through the existing `IMEInputHandler` (`gpui/ime.go:135`).

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-05-input-editor.md T2](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
