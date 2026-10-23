# Reveal a line inside a long block: core request_autoscroll

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 30

## Question

A TextView inside a scrollable list reveals a line inside a long block, as upstream does through `Window::request_autoscroll`. The inline character walk is also fast enough for long lines.

## Scope

- gpui: add `Window.RequestAutoscroll(bounds)` (R:window.rs request_autoscroll), and have `List` take the request in prepaint and scroll it into view (R:elements/list.rs autoscroll path). gpui/listelement.go's header records this path as omitted.
- base/text_inline.go `inlineText.requestReveal`: add the call at the marked spot, then port the 8 reveal_range tests that need it (see evidence/base/ticket30-text-extras.json). Where the test text system's single-line layout blocks a test, add a real-window test.
- Performance: `inlineCharStops`/`inlineTextLineBounds` call PositionForIndex once per character, so a 400-word line spends about 2s painting. Port upstream's row walk.

## Acceptance criteria

- [ ] Every change cites the pinned sources; no invented APIs.
- [ ] gpui and the core list/scroll spec packages stay green, and `go test ./base` is green.
- [ ] Evidence in `evidence/base/ticket37-*.json`.

## Comments

- 2026-10-23 — Created by the base-port-2 orchestrator from ticket 30's report.
- 2026-10-23 — Claimed and delegated. Owns gpui/window*.go and gpui/list*.go for the autoscroll path, plus base/text_inline*.go and the reveal tests.
- 2026-10-23 — Worker finished. The orchestrator verified it on a snapshot.

## Answer

The core now has `Window.RequestAutoscroll` and `TakeAutoscroll`, and `List` handles them during prepaint (window.rs:3962-4006, list.rs:1250-1361). It takes each item's request, computes the scroll with the pinned walks, discards the first pass through a transact rollback, and prepaints again. The TextView reveal calls it, so a line inside a long block scrolls into view.

The inline row walk replaces the per-character position queries. A 400-word line on the test text system now paints in about 0.06s instead of about 2s.

Tests:
- Of the 11 reveal tests, 10 are ported as real-window tests and 1 is deviated.
- The uncovered linked-image click test is ported.

Recorded environmental case: the gpui wheel test fails while Shift is physically held on the machine.

Orchestrator re-run on a snapshot: `go test ./base` has 1280 pass and 0 skip, `-count=2` is ok, and gpui plus 6 list/scene/text spec packages are ok. Evidence: [ticket37-request-autoscroll.json](../../../evidence/base/ticket37-request-autoscroll.json).
