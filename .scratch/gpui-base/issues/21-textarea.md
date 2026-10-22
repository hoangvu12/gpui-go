# Port the Textarea and the display map

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
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
- 2026-10-23 — From ticket 22: add numberStep *NumberStep to InputBaseState, defaulting to a fixed step of 1, plus the Step/StepBy/SetStep builders (U:input/base/state.rs:401,732,9743-9835). Then switch the inputNumberStep stub in base/number_input_step.go to read it. That stub is the one line outside 21's files that this change touches.
- 2026-10-23 — Worker finished, including 22's NumberStep request. The orchestrator verified it on a snapshot.

## Answer

The multi-line input is ported. Its parts:
- the display map, which replaces ticket 20's placeholder: a wrap map over a re-expressed text wrapper, the fold map, folding and inline lines
- inline tokens and token presentation
- the Textarea component and `NewTextareaState`
- the multi-line builders: SoftWrap, WrappingIndent, Rows, AutoGrow and Searchable
- incremental map and fold updates on edit
- `Input.Token` and `OnTokenClick`
- 22's NumberStep field and the Step/StepBy/SetStep builders

Tests:
- text_wrapper.rs: 13 of 13.
- fold_map.rs: 1 of 1.
- state.rs: of the 122 tests left after 20 and 22, 61 are ported (18 deviated, mostly opening the editor mode by hand) and 61 are not applicable. Those 61 are auto-close, smart indent, indent, search, diagnostics and similar editor features, left for 23 and 24.

Deviations:
- re-wrapping takes the window
- the map's font is set at prepaint
- token renderers return `AnyElement`
- wrong-mode methods panic

Orchestrator re-run on a snapshot of HEAD plus 21's files: vet, gofmt and build are clean, `go test ./base` has 824 pass and 3 skip, and `-count=2` is ok. Evidence: [ticket21-textarea.json](../../../evidence/base/ticket21-textarea.json). Follow-up: narrow `change.tokenDelta` to `*tokenDelta` in input_change.go.
