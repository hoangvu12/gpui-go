# Load assets asynchronously and animate image elements

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: implement-spec wave-3 worker (background subagent, model dashscope/glm-5.3; evidence: ../../evidence/ticket18-asset-loading.json)
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


## Answer

Resolved 2026-10-10. Implemented by the wave-3 asset implementer subagent (session impl-18-assets, clean completion) and verified by the orchestrator (gpui 150 tests, assetspec 46 tests re-run green; vet/gofmt clean). The Go port of the pinned asset machinery, all in package gpui mirroring the pinned crate layout (a subpackage would import-cycle with the App wiring — documented in the file headers): gpui/assets.go (AssetSource load/list, AssetEntry PreLoaded/OnDemand, the AssetRegistry with the exact DuplicateAssetPath diagnostic, extend/iter_preloaded/iter_ondemand; Resource Uri/Path/Embedded; the (loader-tag, source-hash)-keyed application fetch cache with remove/has), gpui/httpclient.go (Get(url, followRedirects) over HttpResponse; NullHttpClient as the app default — zero network attempts; BlockedHttpClient with the permission-denied error; the fake fixed-status clients; the net/http adapter as explicit app configuration, never http.DefaultClient), gpui/imagecache.go (the three-state cache: nil=loading/result/error; RetainAllImageCache with shared-task dedup so two consumers share one load, clear/remove invalidation, the release-driven atlas drop; the ImageAssetLoader: Path=fs read, Uri=GET(url, true) with non-2xx → BadStatus keeping the first body line, Embedded=registry lookup; ErrImageFormatUnknown routed to an explicit SVG-deferred error; the ImageCacheError variants with the pinned strings; the 200ms LOADING_DELAY state; the rational frame-delay advance with backdating/wraparound, the 100ms out-of-range fallback, reduce-motion and active-window policies). Cancellation and discard ride ticket04 tasks (window close cancels the awaiter, entity release discards the late result); determinism rides the TestApp virtual clock. Adaptations and deferrals (the interactive img element's sprite painting, the window image-cache stack, the static-WebP ImageDecoder path, the SVG renderer = ticket19) are recorded in the evidence file. Dependent unblocked: 19 (SVG — still deferred natively). Executed evidence: [evidence/ticket18-asset-loading.json](../../../evidence/ticket18-asset-loading.json).
