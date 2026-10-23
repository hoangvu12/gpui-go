# Port range highlights, stream fade and HTML

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 01, 28, 36

## Question

Range highlights, streaming fade-in and HTML documents (with the minifier) render as upstream.

## Scope

- Base files: text/range_highlight.rs, text/stream_fade.rs, text/format/html.rs, text/format/html5minify/* (HTML parser per ticket 01).

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-06-text-selection.md T6-T7](../research/slice-06-text-selection.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Note from the base-port-2 orchestrator: ticket 28 leaves hooks for this ticket: nodeRenderContext.streamFades/rangeBackgrounds and textLeafKey. This ticket also owns 28's 65 deferred tests: 59 from state.rs, 5 from inline.rs and 1 inline HTML test from text_view.rs. It also owns the 3 skipped markdown HTML tests from ticket 27.
- 2026-10-23 — Claimed and delegated. Owns new base/text_{range_highlight,stream_fade,html*}*.go and the html5minify port. May wire 28's hooks in base/text_*.go and un-skip the 3 markdown HTML tests. x/net/html enters go.mod (ticket 01), with notices.
- 2026-10-23 — Worker finished. The orchestrator verified it on a snapshot.

## Answer

Streaming fade-in, range highlights with reveal, HTML documents and the html5 minifier are ported. `golang.org/x/net` enters go.mod per ticket 01, with notices. The 3 markdown HTML tests ticket 27 skipped now run.

Tests: everything upstream in scope is ported except 11 reveal tests.
- Those 11 need a block laid out on many rows, which the test text system can't produce. 8 of them also need gpui's `request_autoscroll` (follow-up [37](37-request-autoscroll.md)).
- 4 tests are deviated.
- 28's deferred count was 59; the true count is 63.

28's `inlineTextLineBounds` missed the trailing newline row. That is fixed.

Orchestrator re-run on a snapshot: `go mod verify`, vet, gofmt and build are clean, `go test ./base` has 1268 pass with no skips left, and `-count=2` is ok. Evidence: [ticket30-text-extras.json](../../../evidence/base/ticket30-text-extras.json).
