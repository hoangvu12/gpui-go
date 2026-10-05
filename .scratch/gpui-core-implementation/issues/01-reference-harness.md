# Record independent reference fixtures and the core capability ledger

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer 065e52f5 (gpui-go — implement full core backlog)
Blocked by: none

## Question

A maintainer can run one pinned reference fixture and inspect a complete source coverage ledger before port behavior is accepted.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Build a reference harness directly from the fixed CE source; record instrumentation patches, full resolved dependencies/features, compiler identity and environment. Do not use the extracted bridge as its own oracle.
- [x] Version the common fixture envelope and implement runner, raw trace retention, exact f32 serialization and an intentional mismatch demonstration using a small deterministic layout/effect fixture.
- [x] Enumerate public core exports and platform/window/text/element operations, including feature-gated APIs and debug/test/inspector behavior, into rows with source/profile, applicability, owner ticket, fixture and evidence state.
- [x] Trace custom-GPU/wgpu, hot-patching, diagnostics and native unsupported/no-op operations from call sites. Any applicable uncovered operation gets a concrete ticket before implementation proceeds; selecting D3D11 is not an exemption.
- [x] Record all applicable rows as pending until executed. An unavailable Rust toolchain is a blocked environment result, not a pass; do not install new prerequisites without applicable authorization.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

None. This is the initial implementation frontier once implementation is requested.

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3). Complete record: [evidence/ticket01-reference-harness.json](../../../evidence/ticket01-reference-harness.json).

- **Independent reference harness**: `gpui-reference-harness` built directly from the pinned CE source (`reference/ce-source`, commit `254b5dbd…`, zero instrumentation patches, `default + test-support` on x86_64-pc-windows-msvc, rustc 1.97.0/VS2022 BuildTools). Recorded envelope `gpui-go/conformance/envelope@1`, trace `gpui-go/conformance/trace@1` with exact f32 bit patterns, raw-output retention and run metadata. Fixture `fx-0001-layout-effects` recorded 54 deterministic events (byte-identical across three runs): notify coalescing, FIFO emit, nested defers, virtual-clock task delivery, release-before-effects, exact flexbox bounds.
- **Intentional mismatch demonstration**: `TestRecordedReferenceTraceMismatchDemonstration` mutates the *real recorded reference trace* five ways (one-bit f32 flip, event swap, event removal, integer change, fixture-id change); every mutation fails with a diff naming event index/seq/field; self-comparison is equal.
- **Capability ledger**: `gpui-go/capability-ledger@1` with 6911 rows (all `planned`), 5 resolved profiles (native-default/test-support/inspector-capture/custom-gpu-wgpu/hotpatch-profiler-stacker from recorded cargo-tree graphs), owners assigned via 110 family rules from docs/conformance-inventory.md. 154 unimplemented/no-op rows and 1001 feature-gated rows traced from call sites (api-scan `feature_trace`). 351 supporting-crate rows without a confident family owner were given concrete follow-up work: [ticket 34](34-core-support-triage.md), so no applicable row is silently uncovered.
- **Executed results**: `CGO_ENABLED=0 go vet ./...`, `go test ./...`, `gofmt` all pass; oracle and ledger regeneration commands recorded in [reference/README.md](../../../reference/README.md).
- **Preserved limitations**: dev-profile trace recorded (release-profile reference build pending), supporting-crate triage pending (ticket 34), native windows profile fixtures out of scope for this slice.

Frontier after this ticket: [02](02-native-bootstrap.md) and [03](03-scoped-entities-effects.md).

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
- 2026-10-05 — Claimed by the implementation chat (Roboco `065e52f5-9c7f-4d65-8566-f4bb485196a1`, harness `pi`, model `iroha/dashscope/glm-5.3`) under the user's full-implementation authorization. Toolchain inspection recorded in [handoff/resume.md](../../../handoff/resume.md): Go 1.27.1, Rust 1.97.0 MSVC, gh CLI, network to the pinned CE repository confirmed; no git repository in the workspace (commit step environment-blocked).

