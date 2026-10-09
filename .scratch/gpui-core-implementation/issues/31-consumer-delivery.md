# Build an offline consumer with prebuilt assets and executable DPI resources

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: wave-2 worker (implementation) + orchestrator (test gate and verification; evidence: ../../evidence/ticket31-consumer-delivery.json)
Blocked by: 05

## Question

A clean Windows consumer builds and opens an app using local Go and the shipped native artifact alone.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Provide the pure-Go consumer resource command producing verified PerMonitorV2 executable resources while preserving supported manifest customizations and rejecting duplicates.
- [x] Build with CGO_ENABLED=0, no Rust/C/resource/shader compiler and no first-run network; run default embedded and explicit verified-bundle modes.
- [x] Test clean-machine normal/delay/dynamic imports and actual static-CRT outcome; do not infer absence of a redistributable requirement from a build flag alone.
- [x] Verify module zip compressed/expanded limits, exact artifact/manifest hashes, notices and maintainer provenance; root research snapshots must remain outside consumer packages.
- [x] Exercise cache corruption/concurrency/read-only conditions and early DPI fallback diagnostics in a real app. Final acceptance reruns this on the complete artifact; no public publishing is authorized.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.


- 2026-10-09 — The wave-2 consumer worker ran 2h14m (cmd/gpuiresource + internal/consumerspec's coff/imports/manifest/pe/resource/evidence sources landed; no tests yet) and was stopped by the orchestrator after ignoring the wrap-up steer; it was mid-fix of a real PE bug it found (the delay-import data directory is index 13, not 12 — 12 is the IAT). The session was resumed with a corrective bounded prompt (finish the fix, complete the tests, wrap within 30 minutes).

## Answer

Resolved 2026-10-09. The wave-2 worker delivered the implementation across two sessions (the original 2h14m run wrote the full pure-Go surface and identified a real PE bug mid-stop — the delay-import data directory is index 13, not 12 — which the resumed session applied) and the orchestrator wrote the test gate after the second stall: internal/consumerspec 8 tests (the artifact-pair identity against the recorded evidence with the provenance record check; the real DLL's import surface with static CRT and the delay table; the manifest build/validate round trip with typed DPI rejections; the resource section round trip; duplicate rejection; the COFF syso container; the module zip limits; and the end-to-end embed into a real Go-built executable that still runs with a verified manifest and a rejected second embed) plus the re-run of ticket 02's 16 loader tests (corrupt/unwritable/concurrent cache, bundle override, staging cleanup) and the executed verify-imports command. Executed evidence: [evidence/ticket31-consumer-delivery.json](../../../evidence/ticket31-consumer-delivery.json).
