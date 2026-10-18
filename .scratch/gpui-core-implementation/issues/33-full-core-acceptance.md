# Calibrate visual gates and audit complete Windows core conformance

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: wave-4 orchestrator chat (Roboco)
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

## Answer

RESOLVED (2026-10-11, wave 4, batch 12 — the final ticket). The full-core audit ran on the final rev-11 artifact (sha256 37ee6877…):

- **The reconciliation is COMPLETE**: all 6911 capability-ledger rows (schema @2) map through their owning tickets to executed resolution evidence (3927 exact + 2846 behavior + 23 both + 115 structure; zero unreconciled) — executed by the committed audit tool (conformance/cmd/auditreconcile). The blocked environments and the narrowly documented exceptions are recorded, never passed as behavior.
- **The visual calibration executed** (conformance/cmd/visualcalib): the repeat-run noise is measured ZERO (10/10 byte-identical captures per scene), the material injected defects (a one-pixel color change, a 1-pixel displacement, an extent error) are all detected by the exact gates, and the thresholds are frozen accordingly — the exact gates at threshold 0, with the ~98% intent staying intent (never an SSIM/pixel percentage), exactly as the ticket requires.
- **The complete corpus on the final exact artifact**: 34 packages ok (build/vet/gofmt clean); the only failures are the documented environmental classes (the post-fullscreen shell corruption for the cursor gate; the parallel-contention class for the live-capture pixel gate — both proven baseline-pre-existing, the capture gate green serially). fx-0001..fx-0008 bit-exact on the final artifact (the property held through all three rotations); the consumer delivery gates green.
- **The provenance recorded**: the fixed pin (254b5dbd…), the artifact family history (rev 1→11, every rotation additive with the measure manifest), the adaptation/exception provenance per ticket, and the upstream/dependency update procedure (a new pin reopens the conformance decision; the rebuild follows the bounded resource rules with the additive assertion checklist).

Evidence: [evidence/ticket33-full-core-audit.json](../../../evidence/ticket33-full-core-audit.json) (the corpus table, the reconciliation counts, the calibration measurements, the remaining-work record — the honest statement of what publication-status work remains).

