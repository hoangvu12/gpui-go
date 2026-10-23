# Reveal a line inside a long block: core request_autoscroll

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
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
