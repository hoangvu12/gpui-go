# Scroll divs and lists with the wheel and expose scroll metrics

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port orchestrator (wave 1b)
Blocked by: 05

## Question

`div().OverflowYScroll().TrackScroll(h)` and both list elements scroll with the wheel, and `ScrollHandle` reports bounds/max offset/child bounds for scrollbars.

## Scope

- Go Div has no scroll state or wheel handling; `ScrollHandle` fields are never written (`gpui/scroll.go:33-47`); lists ignore the wheel (`gpui/list.go`, `gpui/listelement.go:20-36`).
- Port: overflow-scroll + `track_scroll` + default wheel listener (unclamped apply, clamp at prepaint) + `restrict_scroll_to_axis` (R:elements/div.rs:1747-1774,2426-2460,2646,2737,3816); `OngoingScroll` (R:gestures.rs:49-110); list wheel (eager clamp, R:elements/list.rs:1626) + scrollbar plumbing (R:elements/list.rs:336,743-799); `UniformListScrollHandle` bounds/max offset (R:elements/uniform_list.rs:80).

## Acceptance criteria

- [ ] Wheel transcripts (pixel + line deltas, touchpad phases) match pinned clamping for div, uniform list and list.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-04-scroll-lists-dock.md G1-G4,G8](../research/slice-04-scroll-lists-dock.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
