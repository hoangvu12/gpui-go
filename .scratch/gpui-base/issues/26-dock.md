# Port the dock

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
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
- 2026-10-23 — Claimed and delegated. Owns new base/dock*.go and its tests, plus the fixtures copied under base/testdata/dock/.
- 2026-10-23 — The worker hit the usage limit, was resumed, re-ran against ticket 36 and finished. The orchestrator verified it on a snapshot.

## Answer

The dock is ported in base/dock*.go, with the 6 frozen fixtures in base/testdata/dock/. It covers:
- the layout tree, edit, normalize and builder
- active tracking
- state and state_convert, with JSON persistence through custom `encoding/json` marshalers that keep the frozen tags
- placement and drag
- panels and the registry
- TabGroup and DockArea

Tests: all 152 upstream tests are ported, with none deviated. One assertion compares numbers numerically, because Go writes 1.0 as `1`.

Deviations:
- Data enums are sealed interfaces.
- Panel handles don't own the panel entity.
- `PanelViewsEqual` compares panel ids.
- The docks are walked in a fixed order.

Orchestrator re-run on a snapshot of HEAD plus 26's files: vet, gofmt and build are clean, `go test ./base` has 1169 pass and 3 skip, and `-count=2` is ok. Evidence: [ticket26-dock.json](../../../evidence/base/ticket26-dock.json).
