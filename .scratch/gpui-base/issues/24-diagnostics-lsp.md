# Port editor diagnostics and LSP integration

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 23

## Question

Diagnostics, completions, hover, code actions, definitions, document colors, semantic tokens and inline-completion ghost text work through Base's provider traits.

## Scope

- Base files: input/editor/diagnostics.rs, input/editor/lsp/*. lsp-types → hand-written Go structs (per ticket 01).

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-05-input-editor.md T5](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Claimed and delegated. Owns new base/editor_{diagnostics,lsp*}*.go and their tests. It may edit the input/editor engine files (base/input*.go, base/editor*.go) to hook diagnostics and LSP in. It also owns the 2 state.rs tests 23 deferred: the diagnostics scroll test and the hover half of test_blur_keeps_decorations.
- 2026-10-23 — The worker hit the usage limit, was resumed after the reset, and finished. The orchestrator verified it on a snapshot.

## Answer

Diagnostics and the LSP layer are ported and hooked into the editor. They cover:
- diagnostics: `DiagnosticSet` over base/sumtree, wavy underlines and the popover
- completions, plus the inline completion behind ghost text
- code actions, hover, and definitions with the hover-definition hitbox
- document colors, semantic tokens and the overlay

The lsp-types are hand-written structs, per ticket 01.

Tests: all upstream tests in scope are ported (diagnostics 1, document_colors 2, semantic_tokens 4, and 23's 2 deferred state.rs tests). One more state.rs test is ported in full.

Deviations:
- LSP methods are generic methods on `InputBaseState[M]`.
- `resolve_completions` isn't ported: its item type is an empty marker and Base never calls it.
- The LSP type shapes are simplified.

Orchestrator re-run on a snapshot of HEAD plus 24's files: vet, gofmt and build are clean, `go test ./base` has 1188 pass and 3 skip, and `-count=2` is ok. Evidence: [ticket24-diagnostics-lsp.json](../../../evidence/base/ticket24-diagnostics-lsp.json).
