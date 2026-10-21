# Port the dock

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 25

## Question

DockArea, TabGroup, panels, the registry, layout editing/normalization and JSON persistence (the frozen fixtures) work as upstream.

## Scope

- Base files: dock/** + dock/fixtures/*.json. The pure model (layout/*, active, state, state_convert, dock_placement, drag) can start once ticket 13 lands. serde → `encoding/json` with custom marshalers, value-compared.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-04-scroll-lists-dock.md](../research/slice-04-scroll-lists-dock.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
