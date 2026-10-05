# Windows GPUI core parity in Go

Triage: ready-for-agent
Status: specified
Reference: GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
Date: 2026-10-05

## Problem Statement

The user already gets the desired UI behavior from GPUI-CE, but Rust development builds and disk use make application iteration costly. They want Go's development workflow while retaining the complete GPUI core programming model, extensibility, behavior and closely matching rendered output. A similar-looking widget API or a counter demo would not support future ports of real GPUI applications and component libraries.

## Solution

Provide a Windows-first Go framework with typed entities, views and fluent custom components; the pinned core's layout, text, interaction, rendering and platform capabilities; and deterministic resource ownership adapted to Go. Ordinary app development uses Go and exact prebuilt native assets with cgo disabled. Maintainers build the pinned native services and shaders. The first acceptance environment is the user's Windows AMD64 PC.

Core behavior remains the complete destination. Approximately 98% visual closeness permits small differences but is not a pixel-match fraction or SSIM threshold. Independent reference fixtures, exact deterministic checks, actual Windows interactions, calibrated captures and a complete capability ledger establish acceptance. The [implementation plan](../gpui-core-implementation/plan.md) supplies 33 bounded, independently verifiable slices and their prerequisites.

## User Stories

1. As an app developer, I want to build with Go and prebuilt assets, so that I do not install Rust, C, resource or shader compilers for ordinary development.
2. As an app developer, I want offline startup and an explicit artifact bundle option, so that installed apps do not download native dependencies on first launch.
3. As an app developer, I want actionable artifact/ABI/architecture errors, so that deployment problems fail clearly before unsafe calls.
4. As a component author, I want fluent styling to return my concrete component type, so that common style calls compose with my own methods without mandatory code generation.
5. As a component author, I want plain Go fields for properties and distinct recipe/view/element roles, so that components do not need unnecessary entities.
6. As a component author, I want convenient string and mixed children plus checked conversion, so that composition is concise and invalid content produces useful diagnostics.
7. As a component author, I want explicit recipe attachment ownership, so that accidental reuse and failed batch construction have defined behavior.
8. As a view author, I want typed entities and context access, so that invalid state and callback types fail at compilation where possible.
9. As a view author, I want shared model state with independent view identities, so that two windows can display the same data without sharing placement state.
10. As an app developer, I want scoped leases and weak handles, so that callbacks and resources have deterministic logical lifetimes despite Go garbage collection.
11. As an app developer, I want typed event declarations keyed by source and payload, so that subscriptions do not confuse unrelated event streams.
12. As an app developer, I want notifications, events and deferred work to preserve their separate ordering rules, so that updates behave like the reference.
13. As an app developer, I want scope-owned subscriptions and cancellable tasks, so that ignoring a returned Go handle does not accidentally cancel useful work or keep dead targets alive.
14. As an app developer, I want owned async results and safe late-result discard, so that closing a view does not leak data or update expired state.
15. As an app developer, I want canonical typed actions with optional JSON/schema support, so that programmatic dispatch and configurable bindings agree.
16. As an app user, I want focus, tab navigation and key contexts/chords to behave consistently, so that keyboard workflows remain usable.
17. As an app user, I want correct dead-key, AltGr, surrogate and layout-change handling, so that physical shortcuts and text entry remain distinct.
18. As an app user, I want native IME composition and candidate positioning, so that multilingual editing works after scrolling, moving windows or changing DPI.
19. As a component author, I want the complete typed custom-element phases and accessibility hooks, so that editors and custom layouts can extend the framework.
20. As an app developer, I want reference-compatible flex, grid and inline layout, so that translating a GPUI UI does not change geometry or intrinsic measurement.
21. As an app user, I want correct shaping, fallback, wrapping, selection and caret geometry, so that complex text remains readable and editable.
22. As an app user, I want the reference glyph raster styles and color handling, so that text appearance stays close across scales.
23. As an app developer, I want all scene primitives, clipping, paths and nested filters, so that the core rendering surface is complete.
24. As an app developer, I want retained state, scene replay and cached input behavior, so that caching does not alter interaction semantics.
25. As an app user, I want scrolling, large lists, tooltips and overlays to stay responsive, so that common application interactions feel reliable.
26. As an app developer, I want image/SVG assets with explicit loading, errors and animation, so that rich content does not require a separate UI stack.
27. As an app developer, I want HTTP behavior to be explicitly injected with the reference Null default, so that loading cannot silently make unintended network requests.
28. As an app developer, I want SVG font loading to resume safely, so that native decoding does not block or reenter foreground state incorrectly.
29. As an app user, I want clipboard, file drops and touchpad gestures, so that the application participates in normal Windows workflows.
30. As an app user, I want native dialogs, window controls, menus and desktop integration, so that the port behaves like a complete Windows app.
31. As an app developer, I want credential operations distinct from clipboard data, so that platform storage retains its own errors and ownership rules.
32. As an assistive-technology user, I want stable semantic trees and responsive UIA queries/actions, so that a changing or closing window does not break accessibility.
33. As an app developer, I want capture frames with explicit ownership, so that producer buffer reuse does not corrupt displayed or cached content.
34. As an app developer, I want GPU completion separated from submission, so that resources survive asynchronous use, failure and shutdown.
35. As an app user, I want multiwindow, resize, minimize/restore, modal and device-loss behavior to be reliable, so that normal desktop activity does not hang or corrupt the app.
36. As a component developer, I want an inspector and useful debug/lifetime diagnostics, so that I can understand my live element tree and ownership mistakes.
37. As a maintainer, I want a source/profile capability ledger, so that missing or optional operations cannot disappear from a core-parity claim.
38. As a maintainer, I want independent pinned oracles and reproducible traces, so that a port regression cannot validate itself.
39. As a maintainer, I want calibrated per-feature visual comparisons, so that tolerated noise does not conceal missing glyphs, shifted geometry or broken filters.
40. As a maintainer, I want deliberate upstream/artifact updates with provenance and regression evidence, so that the compatibility target never changes silently.
41. As an app developer, I want documented Go adaptations and source-supported Windows limitations, so that guarantees are honest rather than inferred from Rust syntax.
42. As the project owner, I want full core completion before Base or platform expansion, so that implementation sequencing does not shrink or redirect the agreed scope.
43. As a test/tool author, I want owned scene readback and explicit headless availability, so that image tooling has a supported and reproducible capture path.

## Implementation Decisions

The following are selected engineering contracts under the user's delegated technical workflow. Canonical linked contracts contain the complete details and supersede historical research proposals. They are implementation requirements, not claims of executed runtime parity.

1. **Language and surface.** Use Go 1.27 or later; the installed Go 1.27.1 has compiled the bounded API. Root package gpui and the independent authoring helper belong to one local module, with backend adapters private. Public branding/module publication remains undecided. Keep constructor-bound generic Styled embedding, the Div constructor with DivElement as concrete type, plain property fields, typed context access and generic methods. [Authoring contract](../../docs/authoring-decision-round.md).
2. **Events, actions and conversion.** A zero-valid comparable event descriptor carries an unexported source/payload pair; runtime identity uses entity identity and the declared payload type, including interface payloads. Canonical action definitions are separate from named registration; rich actions require explicit Clone/Equal, boxed bindings preserve cloned bound payloads, and optional JSON/schema has distinct failure categories. Children follow the selected conversion precedence and diagnostics, with explicit ownership tokens and destination-only reservation/commit atomicity. Go cannot enforce Rust coherence, borrow checking or universal raw-alias detection. [Authoring contract](../../docs/authoring-decision-round.md).
3. **View and element extension.** Keep persistent entities separate from one-placement recipes, explicit ViewOf adapters and managed-view capabilities. Typed layout/prepaint states, global and inspector IDs, accessibility companion interfaces and constrained parents remain available to external packages. The verified counter/two-window corpus becomes a real runtime acceptance path. [Counter and custom-element contract](../../docs/counter-view-api.md).
4. **Logical ownership.** Scope-owned independent leases determine liveness; assignment aliases a lease, retaining creates an independent owner, transfer moves ownership and release is idempotent. Weak upgrade fails irrevocably at logical zero. Borrowed handles expire with access; temporary callback pins protect physical state. Entity-dependent scopes follow entity lifetime rather than the first window. Strong cycles require explicit weak backedges/disposal and diagnostics. [Ownership contract](../../docs/runtime-ownership-contract.md).
5. **Effects and asynchronous work.** Preserve release-before-next-effect, FIFO events/defers, pending-only notification coalescing, deferred registration activation and cancel-next semantics. Scope subscriptions even when the handle is ignored. Detached callbacks use weak endpoints; detached tasks have an app registry. Task execution resources persist until worker acknowledgment, with owned result-message handoff through delivery or discard. Explicit payload ownership/freezing and arena-tail release progress are Go adaptations. [Ownership contract](../../docs/runtime-ownership-contract.md).
6. **Layout boundary.** Use pinned Taffy 0.13.0 in the maintainer artifact with its effective features and rounding disabled. Go owns GPUI refinement, units/rem/DPI, f32 rounding, absolute geometry/cache rules, measurement and inline orchestration. Explicit dimension/available-space tags, generation handles and a fixed synchronous callback trampoline govern the seam. Reject same-engine reentry before native entry while preserving independent-engine nesting. [Layout contract](../../docs/layout-contract.md).
7. **Windows and text boundary.** Go owns Win32 hosting on a locked foreground OLE STA thread, window/app state, message policies and dispatch. Pinned Parley/Fontique/HarfRust/Skrifa/Swash services plus exact-face DirectWrite rasterization provide text algorithms. Preserve byte/cluster geometry, UTF-16 input ranges, font/fallback identities, physical-key/text separation and IMM32 behavior. PerMonitorV2 applies to the consumer executable. The caret coordinate discrepancy is resolved by a mandatory independent nonzero-origin fixture before adapter acceptance. [Windows contract](../../docs/windows-platform-contract.md).
8. **Accessibility and host services.** Go publishes immutable semantic trees; the pinned native AccessKit provider reads them independently and posts actions to generation-checked foreground dispatch. Do not block provider queries on a live app update or raise UIA events while holding reentered locks. Narrow COM adapters preserve apartment ownership. Clipboard, drag/drop, touchpad, dialogs, credentials and the complete applicable desktop/window operations remain core work. [Windows contract](../../docs/windows-platform-contract.md).
9. **Scene and renderer boundary.** Extract the pinned native D3D11/DirectComposition renderer, scene insertion/replay/planning kernel, atlas and shaders. Go produces semantic paint commands. Preserve all primitive formats, order floors/layers, opacity pairing, filter depth rules, blend/gamma behavior and DComp/HWND modes. Shader compilation is maintainer work; consumers receive embedded DXBC. Wire records and GPU shader layouts are separately validated. [Renderer contract](../../docs/renderer-contract.md).
10. **GPU ownership and capture.** Keep scene/cache resource ownership separate from accepted submission ownership. Add event-query retirement after final GPU use, ensured marker submission and bounded nonblocking polling. Present, Flush, cancellation, timeouts and generation changes are not completion. Safe abort requires proof of no further access; uncertainty quarantines dependencies. Capture uses the actual renderer device, retains source frame ownership through copy completion and immutable destination storage through later draws. [Renderer contract](../../docs/renderer-contract.md).
11. **Frames, caches and recovery.** Keyed state persists by identity/type in committed frames; removal/reinsertion starts fresh. Failed construction preserves published artifacts, not arbitrary app mutation rollback. Cached hitboxes, input handlers, callbacks, focus and model/resource dependencies survive alongside paint. Device loss invalidates GPU caches and triggers reconstruction without reviving released logical state. [Ownership](../../docs/runtime-ownership-contract.md) and [renderer](../../docs/renderer-contract.md) contracts.
12. **Images, SVG and assets.** Keep asset registry/cache/task/HTTP policy in Go and pinned codecs/resvg/usvg in native services. Explicit image entry modes preserve clipboard/resource WebP differences and GIF animation. Preserve EXIF, rational delays, alpha arithmetic, SVG sizing/masks and font snapshot lifetime. Owned NeedsFontAssets request/resume avoids unsafe synchronous native-worker callbacks. HTTP defaults to Null; network adapters are explicitly injected. [Distribution contract](../../docs/distribution-contract.md).
13. **Artifact and consumer boundary.** Package one exact combined Windows AMD64 MSVC DLL with private versioned service tables, embedded by default and available through an explicit verified bundle. Guard per-user full-hash publication with locks/read handles; use restricted absolute loading, identity/ABI/capability validation and no first-run download. Measure module-format limits on the first build, verify actual CRT/import closure and final executable DPI resources, and keep the DLL resident for process lifetime while still retiring all logical resources. [Distribution contract](../../docs/distribution-contract.md).
14. **Reference and applicability.** Freeze the CE commit and record native Windows default/capture and separate debug/test profiles. Every public operation gets a per-symbol/profile ledger row. Trace alternate wgpu/custom-GPU, hot-patching, stack guards and profiling rather than assuming either support or exemption. A newly discovered applicable capability must have an implementation owner; unresolved rows block full-core acceptance. [Coverage inventory](../../docs/conformance-inventory.md).
15. **Implementation order.** Start with independent oracle/schema/capability enumeration. Prove real native loading and artifact feasibility early; complete GPU retirement with the first presented frame. Add each observable authoring/platform/rendering path with its own evidence, then run cross-service shutdown, complete visual calibration and final clean-consumer acceptance. Preserve a fixed pin; contradicted design assumptions reopen their decisions with a reproducer. [Implementation tickets](../gpui-core-implementation/plan.md).

## Testing Decisions

The main seam is a common, versioned conformance fixture envelope consumed by an independent Rust reference and the Go public/API or service entry point under test. Use the highest observable boundary that can expose the requirement. Native wire validation and GPU resource ownership additionally require their explicit boundary seams; do not test private helper calls merely to mirror implementation.

- Reuse the existing fluent-authoring tests and the bounded external-package signature fixture as prior art. Go 1.27.1 root/fixture tests and vet plus all 29 negative/control pairs have passed. The wider fixture uses compile-only stubs; rerun its cases against the real API as implementation replaces them. [Recorded evidence](../../evidence/authoring-signature-validation.json).
- Build oracles directly from pinned CE, outside the extracted bridge. Record source patches, dependency/features, toolchain and environment; retain raw and normalized traces with each normalization justified. Exact deterministic gates compare identities, ordered effects, full layout/callback query history, scene plans, text geometry, CPU outputs and ABI records. Start finite f32 comparisons at bit equality.
- Control worker schedules and virtual time for lifetimes/effects, then exercise real Windows reentry and callbacks. Test two independent window owners, cancellation and late results, cache replay, abandoned frames, UIA queries during teardown and GPU work that completes after app delivery scopes close. Forced GC and panic/error injection validate the native ownership boundary.
- Use actual font/IME/layout/driver identities for text, input, capture and UIA sessions. Record unavailable environments explicitly. The caret coordinate fixture, same-device capture and real import/CRT/DPI checks are required execution evidence, not facts inferred from declarations.
- Compare images only after exact geometry/scene checks. Measure reference-repeat noise, inject material defects, calibrate on one set and validate on held-out scenes, then freeze per-feature/region thresholds and a review rubric. Do not equate 98% with a numeric metric or mask whole categories to pass. [Complete conformance procedure](../../docs/conformance-contract.md).
- Build and run a clean offline consumer with cgo disabled and prebuilt assets only. Test concurrent/interrupted publication, incompatible/corrupt artifacts, read-only storage, restricted loading and correct executable DPI resources. Repeat on the final complete artifact.
- Require all applicable capability rows and gates for full-core acceptance, with narrow approved exceptions or blocked-environment results reported explicitly. A demo or percentage cannot hide missing rows. Preserve responsiveness and investigate observable stutter/input lag; comparative Rust/Go benchmarks and new hard performance budgets are not prerequisites.

## Out of Scope

- GPUI Base, Kit or other component-library ports before core completion.
- macOS/Linux and broad Windows version/architecture guarantees before the current-PC milestone.
- Replacing full core parity with an app-specific subset or declaring an alternate backend unsupported without source applicability review.
- Public module branding/licensing decisions, remote issue publication, package/native artifact publication or a release during this planning work.
- An upfront build-time/disk benchmark campaign or invented latency/SSIM thresholds.
- Claiming Rust destructor/borrow/coherence semantics are provided automatically by Go.

## Further Notes

The [Wayfinder map](map.md), [project brief](../../PROJECT.md), canonical contracts and local ticket answers record the selected decisions. The user authorized finishing signature verification, conformance planning, this specification and the implementation backlog together. The only runtime code completed so far is the bounded fluent-authoring helper; the wider authoring fixture establishes compile behavior only. No native artifact, reference run, live GUI or parity result exists yet.

The remaining execution questions have explicit owners: source/profile enumeration and oracle recordings first; artifact size and native ABI feasibility at bootstrap; actual font/driver/IME/UIA behavior in their slices; schema and node-ID details at their public capability seams; the caret-coordinate discrepancy in IME; visual tolerances in final calibrated acceptance. These are required future proof, not hidden passes or reasons to leave the planning map indefinitely open. If execution contradicts a selected architecture, reopen that decision instead of lowering the target.
