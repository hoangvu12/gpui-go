# Build an offline consumer with prebuilt assets and executable DPI resources

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned (worker aborted on connection errors before writing files; orchestrator takes it next)
Blocked by: 05

## Question

A clean Windows consumer builds and opens an app using local Go and the shipped native artifact alone.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Provide the pure-Go consumer resource command producing verified PerMonitorV2 executable resources while preserving supported manifest customizations and rejecting duplicates.
- [ ] Build with CGO_ENABLED=0, no Rust/C/resource/shader compiler and no first-run network; run default embedded and explicit verified-bundle modes.
- [ ] Test clean-machine normal/delay/dynamic imports and actual static-CRT outcome; do not infer absence of a redistributable requirement from a build flag alone.
- [ ] Verify module zip compressed/expanded limits, exact artifact/manifest hashes, notices and maintainer provenance; root research snapshots must remain outside consumer packages.
- [ ] Exercise cache corruption/concurrency/read-only conditions and early DPI fallback diagnostics in a real app. Final acceptance reruns this on the complete artifact; no public publishing is authorized.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

