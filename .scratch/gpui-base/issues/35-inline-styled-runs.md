# Paint StyledText runs inside inline paragraphs

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 10

## Question

A `StyledText` inside a Block/Inline div keeps its per-run colors, backgrounds and decorations, as it does standalone.

## Scope

- Ticket 10's finding: the paragraph's piece painting (`gpui/inline.go`, `gpui/inline_real.go`) paints each piece in one ambient color. Route it through `paintStyledDocument` with the published runs cut to each piece's byte range.

## Acceptance criteria

- [ ] An inline paragraph with a multi-run `StyledText` paints each run in its own color with its decorations (deterministic transcript + real window).
- [ ] Every ported behavior cites the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] No native artifact change (rev 11 stays frozen).
- [ ] `CGO_ENABLED=0 go test ./gpui ./internal/inlinespec ./internal/richtextspec` green, `go vet` + gofmt clean; evidence in `evidence/base/ticket35-*.json`.

## Comments

- 2026-10-22 — Opened by the base-port-2 orchestrator from ticket 10's report.
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated.

## Answer

Resolved 2026-10-22. Paragraph pieces paint through the shared per-line painter (`paintStyledVisualLine`, extracted from `paintStyledDocument`) with the runs cut to each piece's byte range, in two passes (backgrounds, then glyphs and decorations; `div/inline.rs:475-499`, `text_system/line.rs:195-225`). StyledText marks its runs paint-styled; the orchestrator also made the plain Text element publish `TextStyle.ToRun` (`elements/text.rs:903-915`). Deterministic glyph-color and decoration transcripts plus a real-window test; orchestrator snapshot rerun green across gpui and 8 spec packages. Deviation: decoration offsets use each piece's ascent/descent. [Evidence](../../../evidence/base/ticket35-inline-styled-runs.json).
