# Define conformance gates and upstream update policy

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex/root
Blocked by: 01, 02, 03, 04, 05, 06, 07, 08, 10

## Question

What evidence will establish Windows core parity, and how will later upstream changes be accepted without making the current target move?

Integrate the chosen contracts into behavioral, API, layout, visual, IME/accessibility, scheduler, resource-lifetime, and performance validation plans. Name reference workloads and tolerances; distinguish source correspondence from executed results. Define baseline updates and reporting of unsupported behavior. The output is a plan for the specification, not implemented tests or a claim that parity has already passed.

## Existing evidence

The [remaining conformance research](../../../research/19-conformance-update-policy.md) supplies a preparatory evidence matrix and fixed-pin update procedure. It is research, not this ticket's answer. Keep exact behavioral/layout/scene/ABI checks separate from controlled visual captures, reference-repeat noise, per-feature regions and deliberately introduced defect calibration. The user's approximate 98% intent is not a selected matching-pixel percentage or SSIM threshold. No numerical visual gate is frozen without calibration; do not mask all text or transparent pixels to obtain a passing aggregate.

The [distribution contract](../../../docs/distribution-contract.md) adds one exact embedded native artifact, an explicit verified bundle path, Windows publication/locking and hash checks, private service negotiation, static CRT/import inspection, executable DPI resources and process-lifetime module residency. Test clean Go-only/offline operation, concurrent or interrupted publication, read-only caches, corrupt or wrong-target binaries, required compiled capabilities versus OS runtime availability, callback/panic boundaries and shutdown obligations. Complete the source capability inventory, including assets/image/SVG/HTTP injection and platform credentials; packaging never reduces core scope.

The [renderer contract](../../../docs/renderer-contract.md) requires an independent scene oracle covering insertion/order-floor/layers, retained replay, surface-opacity pairing, sorting/batching and filter depths beyond two before pixel comparison. Exercise all primitive and glyph formats, BGRA/premultiplication, shader ABI layouts, composition modes, capture device compatibility and producer-buffer lifetime, resize/modal/multiwindow/occlusion, delayed event queries, partial-submit failures, device loss and shutdown. Native acceptance and GPU completion are separate acknowledgments. Neither Present/Flush nor a generation change permits premature lease retirement; failure cases must prove safe abort or retain quarantined dependencies. The same-device capture adapter is a Go-bridge adaptation, not executed source parity. Validate exact target feature capabilities and clean Go-only consumer loading.

The [Windows platform contract](../../../docs/windows-platform-contract.md) adds source-pinned text/font/geometry cases, physical-key versus text traces, IMM32 composition and UTF-16 ranges, nonzero-origin candidate positioning, mixed DPI, clipboard/drop/touchpad, multiwindow and modal reentry, and AccessKit provider queries/actions during window teardown. Exercise actual system fonts/IMEs and record their versions. Resolve the core bounds comment versus Windows caret-consumer coordinate discrepancy with an independent CE fixture. Verify per-message synchronous/deferred return policies, COM apartment release, provider snapshot safety and a clean Go-only consumer load. The [current-PC inspection](../../../evidence/windows-target-inspection.json) is inventory only, not executed platform validation.

The prerequisite [Validate the selected Go authoring signatures](10-validate-authoring-signatures.md) passed on 2026-10-05. Its dependency records the distinction between design selection and compiler proof. Runtime behavioral checks remain part of this conformance decision and future implementation acceptance.

Apply the [accepted development criteria](02-set-development-and-runtime-budgets.md): comparative Rust/Go performance baselines and hard build/disk budgets are not prerequisites. Keep functional and visual checks, and address noticeable stuttering or input lag. Modest additional runtime RAM is allowed. Benchmark suggestions in the historical research remain optional unless later requested.

Apply the [accepted parity policy](01-define-windows-parity.md): the user wants approximately 98% visual closeness with small differences permitted, alongside full core functionality. Define a defensible visual comparison method and local tolerances without assuming that this means 98% matching pixels or SSIM 0.98. Compare against the Rust reference on the user's current PC, with controlled fonts, scale and rendering conditions; report material local differences even if an aggregate score is high.

- [gpui core contracts](../../../research/01-gpui-core-contracts.md)
- [build footprint and validation](../../../research/05-build-footprint-and-validation.md)
- [upstream version check](../../../research/08-upstream-version-check.md)

## Resolution boundary

The [layout contract](../../../docs/layout-contract.md) requires an independent pinned Rust GPUI/Taffy oracle, identical semantic trees and deterministic measurement functions, full-node geometry and callback-query comparison, and fixtures for refinement/units, rem/DPI/negative rounding, proportional padding, text parent-relative placement, exposed grid/flex behavior, inline fragments, recompute/reset, cache reuse and failed/nested builds. Keep actual Windows text/font fixtures separate from fake metrics. Exact deterministic layout checks are not replaced by the visual-closeness target. Also validate the native ABI, fixed callback trampoline, ownership under GC, panic containment, per-engine reentry rejection and Go-only consumer loading; these are distinct from the passing generic-signature checks and remain unexecuted future gates.

The [runtime ownership decision](04-define-runtime-ownership.md) supplies mandatory behavioral cases: independent leases across two windows; alias release and borrowed-handle expiry; weak versus strong callbacks; explicit cycle diagnostics; notification coalescing/re-notify and per-occurrence FIFO events; deferred registration activation and cancel-next during delivery; event payload ownership and post-arena retirement; cooperative cancellation and late-result gates; focus cleanup; keyed-state removal/reinsertion; cache dependency retention; abandoned-frame cleanup; and shutdown with native work still in flight. Exercise them with controlled worker completions, virtual time and recorded external inputs. Compare source-preserved behavior separately from the documented Go lifetime/cleanup adaptations. These are planned checks, not executed results.

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Answer

Resolved on 2026-10-05 under the user's explicit full-sequence authorization and delegated technical choices. The selected [conformance contract](../../../docs/conformance-contract.md) and [family inventory](../../../docs/conformance-inventory.md) integrate the canonical authoring/runtime/layout/Windows/renderer/distribution requirements. Main reviewed the required DashScope GLM-5.3 [coverage investigation](../../../research/20-conformance-spec-coverage-review.md) and a further read-only audit; concrete Windows corpus omissions were assigned before closure.

Use one independent pinned CE reference and versioned fixtures, exact deterministic comparisons, scheduled behavioral traces, actual Windows IME/UIA/input sessions, calibrated per-feature visuals and clean Go-only consumer delivery. Preserve source-supported native unsupported/no-op outcomes and trace every public/profile operation in the first implementation slice. Missing optional flags do not exempt applicable behavior; all pending/failed/blocked rows remain visible. The selected D3D11 profile does not silently authorize dropping custom-GPU or dev/debug capability surfaces.

Freeze the visual calibration procedure now, with numerical thresholds selected only after reference-repeat noise, material defect injection and held-out validation. The user's approximately 98% intent is not SSIM 0.98 or a matching-pixel percentage. Reject broad masks and aggregate scores that hide local missing content. Deliberate pin updates require semantic/dependency review, new identities and affected/full integration evidence; exceptions need narrow provenance and approval.

Alternatives rejected: screenshots as sole evidence, the extracted native bridge as its own oracle, automatic golden rebasing, a reduced first-app capability target, upfront benchmark budgets, and leaving planning indefinitely open because runtime proof requires implementation. The [signature prerequisite](10-validate-authoring-signatures.md) now passes on Go 1.27.1. Caret coordinates, actual artifact size/driver behavior, reference recordings and numerical visual thresholds remain explicit implementation gates, with no guessed results.

The resulting [specification](../spec.md) and [33-ticket implementation plan](../../gpui-core-implementation/plan.md) complete the Wayfinder destination. All framework/native parity gates remain unexecuted. The existing helper and compiler fixture are the full implemented/verified scope of this planning sequence.

## Comments

- 2026-10-05 — Resolved after the Go 1.27.1 signature fixture and side-chat coverage review. The selected conformance contract, family inventory, specification and 33 local implementation tickets are complete. No framework/native/oracle/visual result is claimed.

- 2026-10-05 — Codex/root claimed the conformance decision after signature validation passed on user-installed Go 1.27.1. The user's "ok do them" authorizes completing this decision and the specification/tickets in the same sequence. Main integrates the side-chat coverage audit, preserving full-core scope and distinguishing planning readiness from unexecuted parity gates.

- 2026-10-04 — User requested research of the remaining work. DashScope GLM-5.3 prepared the conformance report alongside distribution research; main requested corrections to invented visual thresholds and broadened source capability coverage. This ticket remains open/unassigned because authoring-signature verification is still a prerequisite. No tests, calibration images or oracle runs were produced.
- 2026-10-04 — Focused asset follow-up closed the HTTP source gap: injected GET with redirect flag and status/body response, default Null failure, explicit blocked-client permission error, resource URI requests with redirects. Include those exact defaults, clipboard/resource WebP differences (GIF animation in both), SVG unpremultiply arithmetic and lazy font request/resume adaptation in the inventory and oracle corpus.
