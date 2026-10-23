# Port the rope, sum tree, segmentation and small utilities

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
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
- 2026-10-22 — Reopened: the 2026-10-19 worker died with its session and left nothing on disk (status set back to open). Re-claimed the same day by the base-port-2 orchestrator (worktree gpui-go-wt-base, branch base-port-2) and delegated.

## Answer

Resolved 2026-10-22 on `base-port-2`. Subpackages `base/rope` (ropey 2 subset: byte-indexed, LF lines, char/UTF-16/line conversions, persistent copies), `base/sumtree` (SumTree + `NewCursor[D]`), `base/segment` (word/grapheme over `github.com/clipperhouse/uax29/v2` v2.7.0, the first go.mod requirement, plus Base's text_boundary.rs), `base/dataurl` (data-url crate port) and `base/strftime` (chrono strftime subset). THIRD_PARTY_NOTICES.md and its generator's GO_SOURCES updated.

Tests: 57 top-level, all pass on the orchestrator's snapshot rerun; rope property tests against a string model, the Rust segmentation oracle replay (graphemes 3,974/3,974, words 3,969/3,974 = the recorded ZWJ-emoji deviation), the data-url WPT suite in full. Counts, deviations and the ticket-19 hand-off (RopeExt/Point/LSP Position): [evidence](../../../evidence/base/ticket15-data-structures.json).
