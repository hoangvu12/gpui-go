# Port the rope, sum tree, segmentation and small utilities

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 01

## Question

Pure-Go building blocks behave like ropey 2 (byte-indexed, LF lines, UTF-16 metrics), zed-sum-tree, unicode-segmentation (per ticket 01), data-url and a chrono strftime subset.

## Scope

- Rope API surface: slice-05 §Third-party (ropey); SumTree: text_wrapper.rs:59-236, diagnostics.rs:144-246; word/grapheme: state.rs:1460,1477, inline_tokens.rs:198, text_boundary.rs; data-url (2 sites); `Date::format` strftime (calendar.rs, time_field.rs).

## Acceptance criteria

- [ ] Property tests against brute-force string models; ported rope_ext-adjacent unit tests.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-05-input-editor.md §Third-party](../research/slice-05-input-editor.md)
- [deps-recommendation.md](../research/deps-recommendation.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
