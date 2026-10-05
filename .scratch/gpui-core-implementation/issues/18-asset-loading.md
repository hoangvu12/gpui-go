# Load assets asynchronously and animate image elements

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 04, 11, 17

## Question

Image elements load from embedded, on-demand or explicitly injected HTTP sources and redraw safely as frames arrive.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement asset registries, duplicate-path diagnostics, source/caching identities and asynchronous loading/error/delay states in real elements.
- [ ] Default to the Null HTTP client with zero network attempts; preserve Blocked permission errors and explicit injected GET(url, followRedirects) status/body behavior, including non-2xx first-line errors.
- [ ] Test fake 200/404/redirect/cancel flows; resource URI requests pass the required redirect setting. A net/http adapter is explicit app configuration.
- [ ] Schedule animation frames with rational delays and reference reduced-motion/error behavior; retained frame ownership survives loading/close races.
- [ ] Exercise two consumers sharing an asset, cache invalidation, scope cancellation and result discard with deterministic tasks and real image output.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Deliver scoped async results and cancel safely](04-scoped-tasks.md)
- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)
- [Decode CPU images and draw their selected frames](17-image-decode.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

