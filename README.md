# gpui-go

Building toward **1:1 parity with the Rust GPUI core first**. Base and other component libraries are later work. Current status: **Wayfinder complete; specification and 33 implementation tickets ready; no runnable GUI or native artifact**.

The user authorized researching and implementing the custom-component authoring mechanism. [Usage and validation](authoring/README.md) show the selected generic embedding approach; custom component authors do not need code generation. The rest of the framework remains unimplemented.

The [selected authoring contract](docs/authoring-decision-round.md) and [counter example](docs/counter-view-api.md) connect views, recipes, typed access, events/actions and custom elements. The bounded declarations compile under user-installed Go 1.27.1: root/fixture tests and vet plus all 29 negative/control pairs passed. [Validation evidence](evidence/authoring-signature-validation.json). The wider API fixture contains compile-only stubs; runtime and native implementation remain future work.

The [selected runtime ownership contract](docs/runtime-ownership-contract.md) defines scoped leases, borrowed access, effects, cancellation, frame/cache lifetimes and native retirement. The counter example includes independent owners in two windows. The [layout contract](docs/layout-contract.md) defines the pinned Taffy bridge, GPUI rounding/measurement/inline behavior and differential verification obligations. These are design contracts, not implemented runtime behavior.

The [Windows platform contract](docs/windows-platform-contract.md) selects Go-owned Win32 hosting with prebuilt native Parley/DirectWrite/Swash text, AccessKit and COM services. The [renderer contract](docs/renderer-contract.md) selects a native D3D11/DirectComposition and scene-kernel extraction, prebuilt shaders and explicit GPU/capture ownership. The [distribution contract](docs/distribution-contract.md) packages logical native services in one exact embedded Windows AMD64 DLL, with offline/bundle delivery, explicit ABI checks and process-lifetime residency. Execution and artifacts remain unverified.

The research below also covers the future component ecosystem. Its proposals for a Base-led first release predate the user's clarified core-first scope; [PROJECT.md](PROJECT.md) records the current destination.

The motivation is faster development and lower disk use while retaining GPUI's authoring experience. The investigation used web research, Exa discovery, GitHub CLI source reads, and local toolchain documentation on 2026-10-04.

## Read first

The [implementation specification](.scratch/gpui-core/spec.md) and [33-ticket implementation plan](.scratch/gpui-core-implementation/plan.md) are the next-work entry points. The completed [Wayfinder map](.scratch/gpui-core/map.md) records all ten resolved decisions/verification tasks. The [conformance contract](docs/conformance-contract.md) and [coverage inventory](docs/conformance-inventory.md) define the future parity evidence without claiming it has passed.

1. [Synthesis](research/00-synthesis.md): feasibility, consequential findings, and architectural recommendation.
2. [Project brief](PROJECT.md) and [glossary](CONTEXT.md): confirmed intent, current phase, and shared terminology.
3. [Decision brief](research/06-decision-brief.md): questions to resolve before a specification.
4. [Ask Matt handoff](handoff/ask-matt.md): proposed next flow and setup status.

## Technical reports

| Report | Contents |
|---|---|
| [Core contracts](research/01-gpui-core-contracts.md) | Entities, effects, element phases, caching, input, scene, scheduler, and proposed conformance cases |
| [Base and ecosystem](research/02-base-and-ecosystem.md) | Original Kit versus CE fork; controls, overlays, text input, virtualization, docking, and extension traits |
| [Backend, text, and layout](research/03-backend-text-layout-options.md) | Gio, go-gui, GPU libraries, shaping, Taffy, platform integration, and candidate qualification |
| [Go API and lifetimes](research/04-go-api-and-lifetimes.md) | Fluent chains, custom components, generics, element adapters, explicit disposal, and async |
| [Selected fluent-authoring method](research/09-fluent-authoring-methods.md) | Generic embedding versus generation; constructor binding, copy rules, and third-party authoring |
| [Original GPUI authoring comparison](research/10-original-gpui-authoring.md) | Zed's original source and counter example compared with the pinned CE contracts; consumption, child typing, custom element states, events and actions |
| [Go event declaration alternatives](research/11-go-event-declarations.md) | Short emission syntax versus typed pair declarations, source/payload marker limits, shared events, and context facade costs |
| [Authoring contract completion research](research/12-authoring-contract-completion.md) | Descriptor identity, action services, child conversion, and public view/custom-element candidates; reviewed selections and remaining gaps live in the current authoring round |
| [Authoring closure contracts](research/13-authoring-closure-contracts.md) | Controlled access/listeners, canonical action services and boxed dispatch, child staging and explicit ownership-adapter limits; source-backed selections with compiler proof deferred |
| [Runtime ownership research](research/14-runtime-ownership-contracts.md) | Pinned lifetime/effect behavior, Go lease/scope adaptations, subscription/task ownership, payload retirement and frame/cache stress cases; final details live in the selected runtime contract |
| [Layout strategy research](research/15-layout-strategy.md) | Native Taffy versus Go alternatives, exact dependency and style/measurement behavior, Windows DLL/callback design, and differential parity gates |
| [Windows platform, text and input research](research/16-windows-platform-text-input.md) | Pinned engines and native messages, text/IME/accessibility boundaries, host alternatives, draft audit and unexecuted platform gates |
| [Scene renderer strategy research](research/17-scene-renderer-strategy.md) | D3D11 versus wgpu/Go routes, semantic paint and native scene boundary, shader/atlas ABI, capture interoperability and GPU completion gates |
| [Module and native distribution research](research/18-module-native-distribution.md) | Go package boundaries, combined native services, embedded/offline artifacts, loader/ABI/CRT/DPI requirements, image/SVG ownership and source review |
| [Conformance and update research](research/19-conformance-update-policy.md) | Exact versus visual evidence, controlled captures/calibration, real IME/UIA sessions and fixed-pin updates; integrated into the selected conformance contract |
| [Conformance/specification coverage review](research/20-conformance-spec-coverage-review.md) | Side-chat source/profile audit, remaining execution gates and proposed slices; canonical inventory/specification supersede intermediate status and grouping |
| [Build footprint and validation](research/05-build-footprint-and-validation.md) | Sources of build cost, dependency boundaries, and an unexecuted measurement protocol |
| [Sources and method](research/07-sources-and-method.md) | Repository revisions, discovery scope, provenance, and limits |

[Evidence](evidence/README.md) contains selected upstream files, manifests with local SHA-256 hashes, Exa results, and installed Go documentation. Upstream source files are reference material, not project dependencies. The copied earlier investigation is historical and predates the clarified faithful-port requirement.

Historical research recommendations remain proposals unless accepted or superseded in the [project brief](PROJECT.md) and Wayfinder tickets. All planning decisions and bounded authoring-signature verification are now resolved. Framework implementation and exact/behavior/visual/delivery acceptance remain pending; the approximate visual target has no calibrated numerical metric yet.
