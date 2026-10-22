# Port the input engine's data layer

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 15

## Question

RopeExt, cursor/selection/change, the undo manager and mask patterns work headlessly.

## Scope

- Base files: input/base/{rope_ext,cursor,selection,change,undo_manager,mask_pattern}.rs (~3k lines, ~45 tests).

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-05-input-editor.md T1](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated.

## Answer

Resolved 2026-10-22. Root `base` package input_*.go: `RopeExtOf(*rope.Rope)` (all 20 RopeExt methods), InputEdit, RopeLines, `InputPoint`, `InputPosition`, `InputSelection`, cursor/selections and text selectors, change, the undo manager, MaskToken/MaskPattern. All 42 upstream tests ported (4 adapted) + 1 Go-only; ticket 15's test-only RopeExt copy retired. Typed placeholders (`any`) remain for the token delta and auto-closed pairs until those modules are ported. [Evidence](../../../evidence/base/ticket19-input-data-layer.json).
