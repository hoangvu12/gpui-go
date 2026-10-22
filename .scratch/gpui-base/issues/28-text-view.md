# Render rich text: inline flow, nodes and TextView

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 10, 18, 27

## Question

TextView renders markdown documents (inline flow with embedded objects, code blocks via a user highlighter callback, images, lists, tables) as upstream.

## Scope

- Base files: text/{inline,inline_flow,inline_object,inline_element,node,state,text_view,style,utils,mod}.rs. Inline wrapping re-expressed over the Go inline layout (no LineWrapper in the pin).

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-06-text-selection.md T3-T4](../research/slice-06-text-selection.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Note from the base-port-2 orchestrator: ticket 27 ports the node.rs data model into base/text_node.go; this ticket adds the rendering half (NodeContext, Paragraph/BlockNode render impls, caches) in separate files.
