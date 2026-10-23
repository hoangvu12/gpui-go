# Render rich text: inline flow, nodes and TextView

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
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
- 2026-10-23 — Claimed by the base-port-2 successor orchestrator and delegated. Also owns text/inline_virtual_tests.rs and the 7 node.rs tests ticket 27 deferred; renders inline plugins from 27's registry.
- 2026-10-23 — Worker finished. The orchestrator wired `textInit` into `Init`. The orchestrator also fixed a core atlas leak that the `-count=3` re-run exposed: `App.Shutdown` now disposes the app image codec, and the base test helpers shut their app down at cleanup.

## Answer

Rich text rendering is ported in base/text_{style,utils,inline,inline_element,inline_flow,inline_object,node_render,state,view}.go. It covers:
- inline wrapping, re-expressed over ticket 10's LineWrapper
- InlineFlow with embedded objects
- the render half of node.rs, with its caches in weak-keyed side tables
- TextViewState, with a background parse queue that coalesces 64 updates per batch
- TextView and the Text/Markdown/HTML constructors

Tests:
- Ported or deviated: style 6, utils 2, inline 15, inline_flow 10 (5 deviated: the test text system gives every family the same width, so a real-window test covers the property), inline_object 3, inline_virtual 1, the 7 node.rs tests 27 deferred, state 23, text_view 18.
- Deferred to 29: 16 tests that need selection.
- Deferred to 30: 65 tests (stream fade, range highlight, reveal, HTML).
- Left out: 1 retained-layout test, plus 3 text_view tests (`h_resizable` isn't ported, TestApp has no `opened_url`).

Main deviation: boxes that hold text are flex columns, and a TextView must sit inside a Block parent to wrap.

Core fix found here: each App's image codec held one of the 8 native atlas slots for the rest of the process. `App.Shutdown` now disposes it.

Orchestrator re-run on a snapshot of HEAD plus 28's and 22's files and the fix: vet, gofmt and build are clean, `go test ./base` has 747 pass and 3 skip, and `-count=3` is ok. gpui, shutdownspec, assetspec and imagespec are ok. Evidence: [ticket28-text-view.json](../../../evidence/base/ticket28-text-view.json).
