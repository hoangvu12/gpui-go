# Port the code-editor mode

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 21

## Question

`Editor` mode adds the line-number gutter, folding, indentation and indent guides, language config/auto-close, highlighting hooks, decorations and search.

## Scope

- Base files: input/editor/{mod,indent,language,language_config,auto_close,highlighting,decorations,search}.rs, display_map/fold_map.rs. syntect is not used by Base itself (only the showcase).

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-05-input-editor.md T4](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Claimed and delegated. Owns the input engine files (base/input*.go except the ticket-19 data layer, plus textarea*.go and number_input_step.go) and new base/editor*.go. Also owns the 61 editor-only state.rs tests that 21 marked not applicable, and the 9 element.rs editor tests 20 marked not applicable (gutter, decorations, editor scrollbar).
- 2026-10-23 — The worker hit the usage limit, was resumed after the reset, and finished. The orchestrator verified it on a snapshot.

## Answer

Editor mode is ported in base/editor*.go plus changes to the input engine. It covers:
- `NewEditorState` and the Editor component
- the language provider and language config
- auto-close, smart indent and the bracket split on Enter
- folding, the line-number gutter and the editor scrollbar
- ghost text painting
- highlighting hooks, decorations and search

Tests:
- Ported: 80 of 83 upstream tests in scope, 1 of them deviated: the blur test runs only its decoration half, because the hover half needs LSP (ticket 24).
- Not applicable: 3. The diagnostics scroll test is ticket 24's, one test needs `AnyView::cached`, and one needs the native context menu.
- Ticket 21's 16 "editor mode opened by hand" deviations are retired.
- 23 also fixed a blink-cursor task that kept respawning after `App.Shutdown`.

Orchestrator re-run on a snapshot of HEAD plus 23's files: vet, gofmt and build are clean, `go test ./base` has 943 pass and 3 skip, and `-count=2` is ok. Evidence: [ticket23-editor-basics.json](../../../evidence/base/ticket23-editor-basics.json).
