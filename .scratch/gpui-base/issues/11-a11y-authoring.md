# Author accessibility roles and states on divs

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port orchestrator (wave 1c)
Blocked by: 02

## Question

Base components declare roles, labels, values, toggled/selected/expanded, numeric ranges, set positions, table indices and a11y actions on divs, visible in the Go snapshot; publication follows ticket 02's decision.

## Scope

- Div reports no role (`gpui/div.go:1289`); `A11ySpec` has role/label/description/value only (`gpui/accessibility.go:305-331`); Go roles stop at Slider.
- Port the div a11y builders (R:elements/div.rs:1496-1728, write path :4065; R:element.rs:131) and the accesskit Role/Toggled/Orientation surface Base uses.

## Acceptance criteria

- [x] Snapshot golden traces for every new property; publishing a tree with an unsupported role no longer aborts (per decision).
- [x] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [x] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [x] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [x] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-02-simple-controls.md §5](../research/slice-02-simple-controls.md)
- [slice-03-composite-controls.md C1, X2-X3](../research/slice-03-composite-controls.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).

## Answer

Resolved 2026-10-19 (wave 1c worker, verified by the orchestrator on the staged snapshot: TestA11y + accessspec/a11yspec/inspectorspec/shutdownspec). The full pinned div a11y builder set and write path, 27 roles, the snapshot attributes and the decision-02 publication (nearest-role table in the evidence; states dropped at native serialization, no publish abort). The `div.go` hook into the tree lands with the 12+34 batch. Upstream: 11 ported, 3 deviated, 6 n/a. Evidence: [evidence/base/ticket11-a11y-authoring.json](../../../evidence/base/ticket11-a11y-authoring.json).
