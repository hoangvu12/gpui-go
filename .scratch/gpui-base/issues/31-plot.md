# Port the plot library

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 08, 10, 14

## Question

Scales, shapes (line/area/bar/arc/pie/radial/stack/sankey), axes, grid, labels, hover and appear animation render as upstream.

## Scope

- Base files: plot/**. Pure parts (scales, stack, pie, sankey, arc geometry: 44 tests) can start immediately; rendering needs the paint API and the shape-line facade.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-07-plot.md](../research/slice-07-plot.md)
- [E2-plot.md](../research/E2-plot.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated.
- 2026-10-23 — The first worker died at the usage-limit stop with 56 of 67 tests ported. A successor orchestrator's fresh worker finished the ticket, and the ticket is resolved.

## Answer

The plot library is in the root `base` package (base/plot*.go), with the Plot theme field in base/theme.go. It covers:
- scales: band, linear, ordinal, point
- shapes: arc, area, bar, line, pie, radial line, stack, sankey
- axis, grid, label, hover readers, the path cache and the plot element
- the appear animation, with PlotAppearScope

All 67 upstream tests are ported, with none deviated. 8 of the 11 appear tests run windows on the virtual clock.

Deviations:
- solid colours only (decision 02)
- no rust_decimal
- maphash for ShapeKey
- the appear-scope stack is per window
- appear memory is keyed by `GlobalElementID.String()`

Orchestrator re-run on a snapshot of HEAD plus 31's files: vet, gofmt and `go build ./...` are clean, `go test ./base` has 533 pass and 3 skip (the ticket-30 deferrals). Evidence: [ticket31-plot.json](../../../evidence/base/ticket31-plot.json).
