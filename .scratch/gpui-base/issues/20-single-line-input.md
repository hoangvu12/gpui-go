# Port the single-line Input

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
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
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated.
- 2026-10-23 — The first worker died at the usage-limit stop. The successor orchestrator gated its 5 non-compiling files behind `//go:build basewip20`. A fresh worker finished the ticket and removed the tags, and the orchestrator wired `inputInit` into `Init`.

## Answer

The single-line Input is ported. Its parts:
- the shared `InputBaseState` engine:
  - kind hooks
  - masked, pattern and number modes
  - the blink cursor and movement
  - editing, history, the clipboard and IME (through `Window.HandleInput`)
  - mouse selection, layout and render
  - `InputEditorStyle`
- the input element and the `Input` component
- `Init` now calls `inputInit` (U:lib.rs:241), which installs the key bindings.

Tests: 98 upstream tests are in scope.
- 84 are ported, 7 of them with deviations. 5 clipboard tests feed their text through the paste path because TestApp has no clipboard.
- 14 are not applicable: the editor gutter, decorations and editor scrollbar, inline tokens, the number-step test (ticket 22), and the cached-parent test (no view caching in the Go core).

Deviations:
- the display map is a one-row-per-line placeholder, which ticket 21 replaces
- the editor-only paths are stand-ins that do nothing
- no native context menu
- mode-specific methods panic on the wrong mode

Orchestrator re-run on a snapshot of HEAD plus 20's files and lib.go: vet, gofmt and `go build ./...` are clean, `go test ./base` has 618 pass and 3 skip. Evidence: [ticket20-single-line-input.json](../../../evidence/base/ticket20-single-line-input.json).
