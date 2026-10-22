# Select and copy across text elements

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 28

## Question

Cross-element selection, SelectableText and touch selection handles work as upstream.

## Scope

- Base files: text_selection.rs (4.4k lines), selectable_text.rs, touch_selection.rs, text_boundary.rs, text/selection*.rs.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-06-text-selection.md T5](../research/slice-06-text-selection.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Note from the base-port-2 orchestrator: ticket 28 leaves hooks for this ticket: newTextViewSelectionAdapter, textSelectionRegisterInline, textSelectionEnd, textSelectionSelectedText and textWordRangeAt (which defaults to segment.WordRangeAt). This ticket also owns 28's 16 deferred selection tests: 15 from text_view.rs and 1 from inline_virtual_tests.rs. See evidence/base/ticket28-text-view.json.
