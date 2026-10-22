# Port the Textarea and the display map

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 20

## Question

A multi-line `Textarea` soft-wraps, moves vertically, scrolls and shows inline tokens as upstream.

## Scope

- Base files: input/editor/display_map/*, input/base/{inline_tokens,token_presentation}.rs, input/textarea/mod.rs; the bulk of the 169 state.rs tests.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-05-input-editor.md T3](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Claimed and delegated. Owns the ticket-20 input engine files (base/input_{state*,element,layout,kind,movement,base,mode}.go and input.go) in order to replace the placeholder display map and the inline-token stand-ins, plus new base/input_{display_map,wrap_map,fold_map,text_wrapper,inline_line,inline_tokens,token_presentation,textarea}*.go.
