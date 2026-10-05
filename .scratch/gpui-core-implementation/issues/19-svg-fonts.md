# Render SVGs with lazy fonts and correct alpha conversion

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 09, 18

## Question

An SVG element requests missing font assets, resumes decoding once they arrive and renders the reference pixels.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md), [windows platform contract](../../../docs/windows-platform-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Integrate pinned resvg/usvg parsing/rasterization, sizing/scaling, 8192 limit, alpha masks and smooth-scale behavior through explicit native result ownership.
- [ ] Preserve fallback/emoji selection and the renderer font snapshot/missing-cache lifetime; native NeedsFontAssets requests return owned data for Go task orchestration.
- [ ] Resume exactly once when requested bytes arrive, handling cancellation/failure and concurrent consumers without a synchronous foreground callback into a native worker.
- [ ] Verify premultiplied RGBA to straight BGRA swap/unpremultiply arithmetic, transparent pixels and truncating alpha conversion using oracle fixtures.
- [ ] Draw actual SVG/image elements with text, emoji, masks and malformed inputs; prove asset/font/pixel resources retire correctly after caching and shutdown.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Shape text and expose paragraph, caret and hit-test geometry](09-text-geometry.md)
- [Load assets asynchronously and animate image elements](18-asset-loading.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
- 2026-10-06 — DEFERRED (successor chat): pinned resvg/usvg SVG decoding lives in native services; blocked by the no-cargo/DLL-rebuild constraint (and by ticket17). No pure-Go resvg equivalent exists at reference quality. No work started.

