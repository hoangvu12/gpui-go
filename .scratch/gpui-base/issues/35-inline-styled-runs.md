# Paint StyledText runs inside inline paragraphs

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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
