# Port editor diagnostics and LSP integration

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
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
