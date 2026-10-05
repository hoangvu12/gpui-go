# Choose the layout implementation and parity method

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex /root (Wayfinder continuation, 2026-10-04)
Blocked by: 01, 02, 04

## Later status — 2026-10-05

This ticket preserves its original decision and chronological comments. Subsequent [signature verification](10-validate-authoring-signatures.md) passed on user-installed Go 1.27.1, all ten Wayfinder children are resolved, and the [specification](../spec.md) plus [implementation backlog](../../gpui-core-implementation/plan.md) are complete. Earlier open-task/compiler-blocker statements below describe their recorded date; broader runtime/native behavior remains unimplemented.

## Question

Which layout implementation can reproduce the pinned GPUI layout contract, including intrinsic text measurement and device-pixel rounding?

Compare an exact-version native Taffy bridge and a Go implementation against the accepted parity and development criteria. Define sizing units, refinement mapping, measurement callbacks, root layout, prepaint-time layout, and differential fixtures. A feature subset cannot replace the accepted parity target. The research did not verify an existing ready-made bridge.

## Existing evidence

Honor the [runtime ownership contract](../../../docs/runtime-ownership-contract.md): layout/measurement callbacks use foreground-scoped access, phase resources belong to construction scopes, nested builds cannot clear outer storage, and abandoned construction does not roll back application state. Native layout ownership and callback bridging must fit these constraints.

- [gpui core contracts](../../../research/01-gpui-core-contracts.md)
- [backend text layout options](../../../research/03-backend-text-layout-options.md)

## Resolution boundary

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Comments

### Claim and focused research, 2026-10-04

The user instructed continuing Wayfinder after the runtime ownership resolution. Claimed the layout decision and reused the required Roboco `pi` / `iroha/dashscope/glm-5.3` research chat. Main owns review, integration and tracker changes. Scope is source-backed layout strategy and parity planning; no bridge implementation, toolchain operation or benchmark is authorized by this claim.

### Research review, 2026-10-04

The side chat produced [layout strategy research](../../../research/15-layout-strategy.md), revised it after concrete main-chat corrections, and reviewed the [contract synthesis](../../../docs/layout-contract.md) against primary sources. Main checked the saved Taffy adapter (including its manifest hash), style/window entry points, the CE manifests/lock, Taffy default features, GPUI rounding helpers and official Go DLL/callback documentation. Review corrected overly broad absence claims, incomplete callback records, an incorrect compute-deduplication claim, missing pointer ownership, and same-engine versus cross-engine reentrancy. The final review found no substantive unsafe or unresolved contract gap; it is source review, not execution proof.

## Answer

Resolved at documentation level on 2026-10-04 under the user's research-first delegation and instruction to continue Wayfinder. The user explicitly allowed native dependencies and requires Go-only consumer tooling with prebuilt artifacts. Selecting the engine and interface below is a reviewed engineering decision, not a direct user quotation or new implementation authorization.

Select **Taffy 0.13.0 from the accepted CE lockfile behind a maintainer-built Windows DLL**, loaded through `golang.org/x/sys/windows` with no consumer cgo dependency. Preserve the default feature set and record the actual target dependency/feature graph, bridge ABI, compiler/target and artifact provenance at build time. The exact registry checksum is recorded in the [layout contract](../../../docs/layout-contract.md). No suitable ready-made binding has been verified; this bridge remains to be built and tested.

Keep GPUI style refinement, tagged units/constraints, scale/rem conversion, its two-stage rounding, absolute-position caches and inline/text orchestration in the Go compatibility adapter. Preserve the source's proportional padding and parent-relative text exceptions. Use native Taffy for tree/style/algorithm operations, with rounding disabled. Full parity means the pinned GPUI-exposed behavior plus its native defaults, not every possible public property in Taffy or CSS.

Select a private versioned C-compatible interface with explicit field widths/tags/presence, independent per-axis constraints, copied buffers, generation-checked engine/node handles and a fixed measurement callback trampoline. Keep callbacks synchronous on the foreground thread, reject access to the same busy engine, preserve separate engines during legitimate nesting, and defer disposal until active calls return. Go references remain in a scoped registry, never stored as native object pointers. Contain panics on their respective sides, latch failure and discard partial results; Rust recovery requires the bridge's own unwind-enabled profile. These interface details are documented designs, not compiled declarations.

Use an independent Rust oracle around the pinned GPUI adapter and locked Taffy, with identical semantic trees and deterministic measurement functions. Compare every node and the callback requests/results under identical cache histories. Preserve exact deterministic geometry and investigate discrepancies rather than substituting the approximate visual-closeness target. Add actual Windows-font/text fixtures separately. Validate ABI layouts, callback GC/lifetimes, failure/reset, and a clean `CGO_ENABLED=0` consumer build before claiming the route works.

### Rationale and alternatives

- A new Go algorithm port would add flex/grid/intrinsic-sizing compatibility work to the already necessary GPUI adapter. The investigated Go alternatives are not verified equivalents and have documented blockers; this is not a claim that no other implementation exists.
- The locked native engine reuses the reference algorithms while concentrating new work in the bridge and GPUI adapter. It does not prove parity by construction; differential verification remains required.
- Consumer cgo wrappers still require a C compiler even when linking a prebuilt library, conflicting with the accepted development criteria. A Windows DLL and Go loader provide the selected compiler-free consumer architecture; maintainers retain the native build responsibility.
- Adopting another toolkit's layout model or accepting a feature subset would change the established core-parity goal. Neither is selected. A later pure-Go replacement must pass the same corpus; there is no silent algorithm fallback when the DLL is unavailable.

### Consequences and limits

The [Windows/text decision](06-choose-windows-platform-and-text.md) now depends on this contract and owns real text metrics, wrapping/fallback, inline integration and native input. [Distribution](08-define-module-and-native-distribution.md) owns artifact production/loading and clean consumer evidence. [Conformance](09-define-conformance-and-update-policy.md) owns full layout/ABI gates. Updated the authoring/counter references, glossary, research index, project brief and continuation note.

All native code, signatures, artifacts and differential tests remain unimplemented/unexecuted. ABI verification is distinct from generic-authoring signature validation; it does not authorize probing with the older installed compiler or bypassing the blocked upgrade. No new compiler, runtime, bridge, installation or benchmark work occurred. No human preference remains necessary to settle this layout strategy.

Documentation validation passed: 21 documents, 201 local links, balanced code fences and an acyclic dependency graph with all blockers present. Five decisions are resolved; four decisions and one signature-verification task remain open.

Next decision: [Choose Windows platform, text, and input integration](06-choose-windows-platform-and-text.md).
