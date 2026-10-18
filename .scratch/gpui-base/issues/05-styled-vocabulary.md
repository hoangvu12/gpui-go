# Author the full Styled vocabulary and paint every div style

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port orchestrator (wave 1a)
Blocked by: none

## Question

A Go div (and any custom component) can use the whole pinned `Styled` surface — backgrounds, borders, radii, shadows, text style, cursor, flex/grid/size/position/overflow helpers — and the div paints them.

## Scope

- `authoring.Styled` has 6 builders and `StyleRefinement` 5 fields (`authoring/style.go:47-53,123-144`); `Style` is layout-only (`gpui/style.go:778-852`); div paint hard-codes zero radii/borders and no shadow (`gpui/div.go:1655-1675`).
- Port `Style`'s paint fields + `Refineable` (R:style.rs:233-362, 591-610), `Styled` (R:styled.rs, 1,086 lines) and the macro helpers (`gpui_macros/src/styles.rs`), `FluentBuilder` When/WhenSome/Map, and component mixins (`Interactive[Self]`, `Parent[Self]`) so Base components delegate to an inner div without per-component forwarding.
- Div paint: background/border/radii/shadow via the existing `Scene.PaintQuad`/`BoxShadow` (native already supports them).

## Acceptance criteria

- [x] Layout + scene transcripts for the new properties match pinned semantics; a custom component type gets the whole vocabulary from one embedded mixin.
- [x] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [x] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [x] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [x] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md §C9](../research/slice-01-infra-overlays.md)
- [slice-02-simple-controls.md §1](../research/slice-02-simple-controls.md)
- [slice-03-composite-controls.md C13](../research/slice-03-composite-controls.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).

## Answer

Resolved 2026-10-19 (wave 1a worker, resumed once after a session restart; verified by the orchestrator: 22 tests, layout/scene/inline/glyph/text gates, byte-identical regeneration). The whole pinned Styled surface (140 hand-ported + 3,124 generated methods via `cmd/stylegen`; all 118 names Base calls), flattened refinements, FluentBuilder and the Parent/Interactive/Component mixins; divs paint background/border/radii/shadows/rings/filters in pinned order. Stored-but-unapplied fields are handed to tickets 06 (cursor, hidden listeners), 07 (scroll flags, overflow fade) and 10 (text fields). Evidence: [evidence/base/ticket05-styled-vocabulary.json](../../../evidence/base/ticket05-styled-vocabulary.json).
