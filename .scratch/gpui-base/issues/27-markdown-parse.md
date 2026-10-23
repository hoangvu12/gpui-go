# Parse markdown into Base's node tree with source positions

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
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
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated. Ownership split with 28: the parser needs text/node.rs data types (BlockNode, InlineNode, TextMark, LinkMark, Span, ImageNode, Paragraph/Table/CodeBlock data, MarkdownSource), so 27 ports the node DATA model and its pure methods into base/text_node.go; 28 adds the rendering half of node.rs in separate files. goldmark enters go.mod here (ticket 01).
- 2026-10-23 — The first worker died at the usage-limit stop. A successor orchestrator's fresh worker finished the ticket from the files on disk, and the ticket is resolved.

## Answer

Markdown parses through goldmark (v1.8.6, MIT, enters go.mod per ticket 01). The pipeline runs goldmark, then an mdast adapter (base/text_mdast.go), then Base's node tree with source byte spans (base/text_markdown.go). It covers GFM, math and frontmatter. The other parts:
- the public `MarkdownPlugin` surface: block and inline registries, compared by count and name as in markdown_ext.rs:252,331-362 (base/text_markdown_ext.go)
- `TextDocument` (base/text_document.go)
- the node.rs data model with its pure methods and upstream equality (base/text_node.go)

Test counts:
- markdown.rs, 57: 52 ported, 2 deviated, 3 deferred to 30. The 2 deviated tests assert that MDX is unsupported, because ticket 01 deferred MDX. The 3 deferred inline HTML tests skip until html.rs lands.
- node.rs, 37: 30 ported, 7 deferred to 28. Those 7 cover rendering only: the inline flow, table metrics, the highlight cache, the table data cache and the image source.

Orchestrator re-run on a snapshot of HEAD plus 27's files: vet, gofmt, `go build ./...` and `go mod verify` are clean, `go test ./base` has 466 pass and 3 skip. Evidence: [ticket27-markdown-parse.json](../../../evidence/base/ticket27-markdown-parse.json). Follow-ups: 28 renders inline plugins and ports the 7 deferred tests; 30 un-skips the 3 HTML tests.
