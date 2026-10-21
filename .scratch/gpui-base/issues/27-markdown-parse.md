# Parse markdown into Base's node tree with source positions

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 01, 13

## Question

Markdown (GFM + math + frontmatter; MDX per ticket 01) parses into Base's node tree with the byte spans the 57 format tests check.

## Scope

- Base files: text/format/markdown.rs, text/markdown_ext.rs (the public `MarkdownPlugin` mdast surface), text/document.rs.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-06-text-selection.md](../research/slice-06-text-selection.md)
- [E1-text-selection.md](../research/E1-text-selection.md)
- [deps-recommendation.md](../research/deps-recommendation.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
