# Run the typed counter and custom-component authoring path

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 03, 10

## Question

An external Go package builds a fluent custom component and a two-window counter with shared state and independently owned views.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [authoring decision round](../../../docs/authoring-decision-round.md), [counter view api](../../../docs/counter-view-api.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Replace compile-only authoring declarations with real ViewOf/managed-view/RenderOnce/Element adapters, typed listeners and access; retain the verified constructor Div and concrete DivElement spelling.
- [x] Execute the documented counter/label/subscriber flow in two windows. Closing one preserves the independently retained model and other view; closing the last owner releases dependent resources.
- [x] Implement complete typed Element phases with global/inspector IDs, plain struct props, accessibility companions and restricted parent child types; preserve the existing Styled helper without requiring generation.
- [x] Implement conversion precedence and nil diagnostics, checked/unchecked child insertion, typed slices, OwnRecipe attachment tokens and atomic destination reservation/commit under converter reentry; do not promise external side-effect rollback.
- [x] Port the external positive/29 negative-control fixture to the real API and rerun tests/vet; distinguish signature conformance from executed event/child/runtime behavior.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run scoped entities, typed access and ordered effects headlessly](03-scoped-entities-effects.md)
- [Rasterize and draw text in all required glyph formats](10-draw-text.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by a background subagent and independently re-verified. Complete record: [evidence/ticket11-authoring-counter.json](../../../evidence/ticket11-authoring-counter.json).

- The element/view runtime: Element[L,P] phases with the pinned id/state model (retained element-state arena keyed by identity path + type, reentry-guarded), Div()/DivElement with the verified API surface and the checked child conversion gate (reserveAndCommitRecipes), Render/View/ViewOf/ManagedViewOf adapters, DrawWindowFrame (request -> stretch -> compute -> prepaint -> paint -> finish inside one update), deterministic test windows (scale 2.0, the reference TestTextSystem port), plus a11y companion structure for ticket21.
- THE GATE: fx-0006-authoring-counter (108 events, deterministic) reproduced bit-exactly — two windows sharing a model, increment script, per-render primitive counts/quads/debug bounds/shaped text facts through the public window-draw boundary (the reference test-support auto-redraw semantics mirrored).
- Real two-window counter acceptance: two Win32 windows share one counter model; increments notify both views (coalesced), both redraw (bounds grow 96->108, real subpixel sprites), each redraw presents + retires through the GPU ledger (4/4), closing one window preserves the model while the other keeps working, last close releases it. The Styled fluent API composes via authoring.BindStyled from an external package. Full suite green (15 packages).

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

