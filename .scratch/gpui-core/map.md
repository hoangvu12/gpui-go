# Plan Windows GPUI core parity in Go

Labels: wayfinder:map
Status: resolved

## Destination

Settle the contracts and architecture needed to write an implementation-ready specification for a Go port with 1:1 GPUI-CE core parity on Windows. The map is complete when no unresolved design decision prevents that specification; implementation and proof of parity follow later.

## Notes

- [Project brief](../../PROJECT.md) is the source of truth for the user's confirmed scope, pinned upstream reference, Windows-first sequencing, native-dependency permission, and current implementation boundary.
- [Tracker conventions](../../docs/agents/issue-tracker.md) define local child tickets, claims, blocking and frontier queries.
- Read [CONTEXT](../../CONTEXT.md) for terminology. Use the Wayfinder, grilling, and domain-modeling skills when working decisions. Invoke research for a concrete external evidence gap; consult codebase-design when designing interfaces and boundaries.
- [Research index](../../README.md) and [core contracts](../../research/01-gpui-core-contracts.md) supply existing evidence. Read the report linked by the selected ticket rather than redoing discovery.
- The accepted destination is full core parity. Development may proceed in stages; a stage does not redefine completion as a smaller feature subset.
- Initial tickets use the existing research and remain human-in-the-loop decisions. No fresh external research tickets are required merely to repeat the completed investigation.
- The user subsequently delegated routine technical choices to source-backed research and main-chat review. Preserve explicit user preferences and bring back only genuine human tradeoffs; use the selected Roboco `pi` / `iroha/dashscope/glm-5.3` side chat for research. Design resolution and executable verification are recorded separately.
- After the map clears, use to-spec and then to-tickets. The user separately authorized the bounded fluent custom-component authoring slice; its implementation and validation are recorded in the active Go authoring ticket. Broader runtime, renderer and benchmark work remains future work.
- On 2026-10-05 the user installed Go 1.27.1 and explicitly authorized the complete signature-verification, conformance, specification and ticket sequence with "ok do them". That sequence is complete: [specification](spec.md) and [implementation plan](../gpui-core-implementation/plan.md). This resolves the planning destination without claiming runtime parity.

## Decisions so far

- [Define the Windows core parity contract](issues/01-define-windows-parity.md) — full core behavior, approximately 98% visual closeness, documented bug exceptions, and the user's current PC as the first verification target.
- [Set development and runtime success criteria](issues/02-set-development-and-runtime-budgets.md) — Go-only consumer tooling with prebuilt native libraries, no upfront benchmark gate, and modest extra RAM permitted while retaining responsiveness.
- [Choose the Go language and authoring contract](issues/03-choose-go-authoring-contract.md) — Go 1.27, fluent typed authoring, explicit event/action and view/element adapters, and checked child construction; the subsequent bounded signature verification passed.

- [Define entity ownership, effects, and scheduling](issues/04-define-runtime-ownership.md) — explicit scoped leases, deterministic logical release and effect delivery, scoped cancellation, retained frame/cache ownership, and completion-based native retirement; implementation and conformance remain future work.
- [Choose the layout implementation and parity method](issues/05-choose-layout-strategy.md) — locked Taffy behind a prebuilt Windows DLL, Go-owned GPUI layout adaptation, and independent full-tree/measurement differential gates; native artifacts and execution proof remain future work.

- [Choose Windows platform, text, and input integration](issues/06-choose-windows-platform-and-text.md) — Go-owned Win32 hosting with pinned native text/AccessKit/COM services, explicit input/DPI/lifetime contracts and a renderer seam; bridge and platform execution gates remain unverified.

- [Choose the scene renderer and GPU backend](issues/07-choose-scene-renderer.md) — pinned D3D11/DirectComposition and scene-kernel extraction, prebuilt shaders, explicit GPU retirement and capture ownership; native/driver proof remains future work.

- [Define module boundaries and native distribution](issues/08-define-module-and-native-distribution.md) — one local Go module and exact embedded native DLL, offline/bundle loading, private ABI/CRT/DPI requirements and process-lifetime residency; artifacts and clean-consumer proof remain unexecuted.

- [Validate the selected Go authoring signatures](issues/10-validate-authoring-signatures.md) — Go 1.27.1 positive compilation, root/fixture tests and vet, and 29 negative/control pairs passed; corrected the Div builder name and isolated source snapshots from root builds, without implementing runtime behavior.

- [Define conformance gates and upstream update policy](issues/09-define-conformance-and-update-policy.md) — independent pinned oracles, exact/behavior/visual/delivery gates, complete feature accounting, calibrated visual acceptance and deliberate upstream updates; the specification and 33 implementation tickets now have explicit evidence owners.

## Not yet specified

No unresolved planning decision prevents the specification. [Define conformance gates and upstream update policy](issues/09-define-conformance-and-update-policy.md) selects independent oracles, exact/behavior/delivery checks, calibrated visual acceptance, full profile accounting and fixed-pin updates. All ten child tickets are resolved.

Execution remains: capability enumeration, native feasibility and artifact size, the caret-coordinate oracle, real Windows/driver/IME/UIA behavior and numerical visual calibration. These have explicit owners in the implementation backlog. Reopen a contradicted decision with evidence; pending implementation proof is not a completed parity gate.

## Out of scope

- GPUI Base and other component-library ports: separate work after the core.
- Making macOS or Linux usable before the Windows milestone: deferred by the user.
- Broad Windows version and architecture support before the current-PC milestone: deferred by the user.
- Reducing the target to an application-specific core subset or component showcase.
- Writing framework/prototype code beyond the explicitly authorized authoring slice, running implementation benchmarks, or shipping a release during this planning map.
- Creating a remote repository, publishing packages, and branding decisions unrelated to the core specification.
