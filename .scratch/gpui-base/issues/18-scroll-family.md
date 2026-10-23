# Port scrollbars, scroll bounce, scrollable masks and the virtual list

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 07, 08, 13, 14

## Question

Scrollbar (over ScrollHandle, UniformList and List), ScrollBounce, ScrollableMask, AutoScroll and VirtualList behave as upstream.

## Scope

- Base files: scrollbar.rs (35 tests), scroll_bounce.rs, scrollable_mask.rs, auto_scroll.rs, virtual_list.rs.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-04-scroll-lists-dock.md](../research/slice-04-scroll-lists-dock.md)
- [C-scroll-lists-dock.md](../research/C-scroll-lists-dock.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated.

## Answer

Resolved 2026-10-22. Scrollbar, ScrollBounce, ScrollableMask, AutoScroll and VirtualList in the root `base` package (the scrollbar value types moved into scrollbar.go). All 83 upstream tests ported + 4 Go-only; 379 base tests pass on the orchestrator's snapshot rerun. Main deviation: VirtualList drives scrolling through an inner div with one content slot because the core exposes no external scroll-metrics setter (sizing agrees with upstream whenever the list size is definite). Optional core seams that would remove the deviations are listed in the [evidence](../../../evidence/base/ticket18-scroll-family.json).
