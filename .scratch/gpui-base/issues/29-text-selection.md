# Select and copy across text elements

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
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
- 2026-10-23 — Claimed and delegated. Owns new base/{text_selection*,selectable_text*,touch_selection*,text_boundary*}.go. May wire 28's hooks in base/text_{view,state,inline,inline_flow,node_render}.go and move 28's 16 deferred tests into place. Ticket 30 waits for this ticket so the two don't both edit those files.
- 2026-10-23 — The worker hit the usage limit, was resumed after the reset, and finished. The orchestrator verified it on a snapshot.

## Answer

Text selection is ported in base/{text_selection,text_selection_adapter,text_selection_word,touch_selection,selectable_text,text_boundary}.go and wired into ticket 28's TextView hooks. It covers:
- cross-element selection: the layer, participants, scopes and handles
- SelectableText
- touch selection handles
- text boundaries, as wrappers over base/segment
- the word lookup

Tests:
- Ported: all 53 upstream tests (text_selection 45, selectable_text 3, touch_selection 3, text/selection 2).
- 28's 16 deferred tests: 13 ported, 3 deviated. One works around the test text system never wrapping, one uses `ScrollTo` in place of `scroll_to_end`, and one uses a plain-div hover card.

Follow-ups, handed to the next batch:
- mount `TextSelectionLayer` in Root and add Root's Copy action
- `ElementExt.TextSelectionScope`
- a core bug: gpui/element.go:612 recomputes paint bounds at paint time instead of using the prepaint bounds (R:element.rs:537-600)

Orchestrator re-run on a snapshot of HEAD plus 29's files: vet, gofmt and build are clean, `go test ./base` has 1012 pass and 3 skip, and `-count=3` is ok. Evidence: [ticket29-text-selection.json](../../../evidence/base/ticket29-text-selection.json).
