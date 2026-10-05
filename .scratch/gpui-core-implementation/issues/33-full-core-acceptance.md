# Calibrate visual gates and audit complete Windows core conformance

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 15, 30, 31, 32

## Question

A maintainer can produce an honest full-core coverage report tied to one exact artifact, independent reference and controlled environment.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [conformance contract](../../../docs/conformance-contract.md), [conformance inventory](../../../docs/conformance-inventory.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Reconcile every public/profile capability row from the first slice with a passed fixture, evidenced native unsupported outcome, blocked environment or narrowly approved exception. No applicable missing row or unowned optional surface can hide behind the selected backend.
- [ ] Complete reference captures across every primitive/text/image/SVG/filter/inline/composition feature; measure repeat-run noise and validate candidate metrics with material injected defects and held-out scenes.
- [ ] Freeze per-region thresholds, environment and review rules before judging the release corpus. Do not interpret 98% as SSIM/pixel percentage or mask whole text/transparent areas.
- [ ] Run the complete exact/behavior/IME/UIA/native corpus and the clean consumer delivery gates on the final exact artifact; report all failures, unavailable cases, exceptions and visual local differences.
- [ ] Record fixed-pin baseline, adaptation/exception provenance and upstream/dependency update procedure. Reopen contradicted decisions; no full-parity claim or release publication while required gates remain pending.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render scenes to owned images and account for headless APIs](32-render-to-image.md)

- [Lay out inline text and embedded elements across fragments](15-inline-layout.md)
- [Verify cross-service close, cancellation and shutdown](30-shutdown-integration.md)
- [Build an offline consumer with prebuilt assets and executable DPI resources](31-consumer-delivery.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
