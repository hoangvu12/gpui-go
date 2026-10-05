# Choose Windows platform, text, and input integration

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex /root (Wayfinder continuation, 2026-10-04)
Blocked by: 01, 02, 04, 05

## Later status — 2026-10-05

This ticket preserves its original decision and chronological comments. Subsequent [signature verification](10-validate-authoring-signatures.md) passed on user-installed Go 1.27.1, all ten Wayfinder children are resolved, and the [specification](../spec.md) plus [implementation backlog](../../gpui-core-implementation/plan.md) are complete. Earlier open-task/compiler-blocker statements below describe their recorded date; broader runtime/native behavior remains unimplemented.

## Question

Which Windows host and text stack will preserve GPUI windowing, text geometry, native input, and accessibility behavior?

Cover window lifecycle and multiple windows, event-loop ownership, focus and action routing, text shaping/rasterization/fallback, UTF-16 composition and selection, candidate bounds, clipboard, drag/drop, DPI, and accessibility. Distinguish reusable platform code from adopting another toolkit's UI model. Define the native window/surface boundary the renderer needs.

## Existing evidence

Honor the [runtime ownership contract](../../../docs/runtime-ownership-contract.md): one foreground OS thread, native callbacks entering the dispatcher, independent window scopes, deterministic logical focus release, and terminal completion/retirement messages during shutdown. Select the Windows loop/COM/native-handle mechanisms here without replacing logical lifetime with GC.

The [layout contract](../../../docs/layout-contract.md) keeps Taffy computation separate from GPUI's text/inline orchestration. The text choice must provide logical-pixel measurement under optional known dimensions and per-axis definite/min/max-content constraints, preserving font/run/wrap/clamp inputs, inline atomic boxes, vertical alignment, fragments, and font-generation invalidation. Actual Windows-font fixtures complement the deterministic layout oracle; a native layout DLL alone does not establish text parity.

Apply the [accepted parity policy](01-define-windows-parity.md): target the user's current PC first. A broad Windows version/architecture support matrix is deferred. Source facts about the current hardware and drivers should be inspected when needed, not turned into user questions.

- [gpui core contracts](../../../research/01-gpui-core-contracts.md)
- [backend text layout options](../../../research/03-backend-text-layout-options.md)

## Resolution boundary

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Comments

### Claim and focused research, 2026-10-04

The user instructed continuing Wayfinder after the layout resolution. Claimed this decision and reused the required Roboco `pi` / `iroha/dashscope/glm-5.3` research chat. Main owns synthesis, validation and tracker changes. This is Windows host/text/input/accessibility planning; renderer selection stays in its next decision. No implementation, installation or compiler probe is part of this work.

### Research review, 2026-10-04

Main inspected the current PC and saved 16 selected CE snapshots with a local hash manifest. The required side chat researched the source and audited the draft. After a broad pass, main sent a fresh bounded report-writing assignment while idle; a transient DashScope 502 retried without changing model/provider. [Reviewed research](../../../research/16-windows-platform-text-input.md) supports the host/service split. Main additionally verified the lockfile's HarfRust dependency and accelerator pre-dispatch, and retained source-versus-execution limits. The coordinate discrepancy remains an explicit fixture requirement, not an invented source conclusion.

## Answer

Resolved at documentation level on 2026-10-04 under the user's instruction to continue Wayfinder and delegation of routine technical choices. Windows on the current PC, full core parity, native dependencies, and Go-only consumers are explicit user preferences. The architecture below is a reviewed engineering selection under those preferences, not a direct user quotation or broader implementation authorization.

Select a **Go-owned Win32 host and GPUI runtime with maintainer-built native text, AccessKit and narrow COM services**, loaded from prebuilt DLLs without consumer cgo. Keep app/entity/effect state, focus/actions, element/layout/inline orchestration and window scopes in Go. Native services do not introduce a second Rust GPUI runtime. Full responsibilities, callback rules and planned evidence live in the [Windows platform contract](../../../docs/windows-platform-contract.md).

Use the pinned Parley/Fontique 0.11.1 text stack, HarfRust 0.12.0, Skrifa 0.44.0 and Swash 0.2.10, retaining the GPUI adapter and its full geometry interface. DirectWrite rasterizes the selected face; it does not replace Parley shaping. Preserve exact fallback rules, font generations, paragraph/caret/selection geometry and typed alpha/subpixel/color pixels. GPU atlas/blending stays with the renderer.

Use one locked foreground OS thread and OLE STA, explicit apartment-owned teardown, a Win32 loop with native key pre-dispatch, safe modal-loop progress and separate window/quit lifetimes. Queries that must answer synchronously use native/published state; app mutations enter the foreground dispatcher under the ownership rules. Preserve IMM32 composition and UTF-16 input ranges separately from byte-indexed text layout. Preserve clipboard formats/metadata, OLE drop behavior, precision touchpad input and PerMonitorV2 scaling. Use AccessKit 0.24.1 / accesskit_windows 0.33.1 with published native trees and posted generation-checked actions; provider queries must not wait for mutable Go state.

The renderer receives a leased HWND, window/resize generations, device-pixel extent, scale and visibility, plus typed CPU glyph data. It owns GPU surfaces, upload, composition, pacing and completion evidence; the host owns HWND destruction. Explicit native buffer/handle/COM release, fixed trampolines, generation checks and contained panics apply throughout.

### Rationale and alternatives

- A native text service retains the pinned shaping, fallback, paragraph and raster algorithms while concentrating compatibility work in a new bridge and Go adapter. No ready-made binding or working artifact has been verified.
- A pure-Go text replacement would add unproved algorithmic equivalence work; inspected alternatives do not establish full parity. This is not a universal claim that no suitable library exists.
- Gio hosting would add a second window/input ownership model while leaving the selected text/accessibility integrations to build. It is not selected as the host.
- Bridging the CE Windows platform unchanged entangles its GPUI types and renderer/device construction with the new Go runtime. Extract narrowly scoped services instead; linking shared Rust value code does not itself establish a second runtime.
- Prebuilt services permit the accepted Go-only consumer workflow in the selected design. Their actual compiler-free loading, callbacks and distribution still require execution evidence.

### Remaining evidence and next decision

No Windows host, text bridge, accessibility provider or renderer was built or run. Required gates include complete platform-trait coverage; clean `CGO_ENABLED=0` consumer loading; per-message consumed/default behavior and modal reentry; font/run/paragraph/inline geometry; real IME single-delivery traces; mixed DPI and nonzero-origin candidate placement; clipboard/drop/touchpad; concurrent accessibility queries and COM lifetime; and native failure/shutdown. In particular, the core bounds comment says screen coordinates while the Windows caret consumer scales without translating; the inspected editor example returns `None` and cannot resolve it. Test the actual convention before freezing that adapter. These gates are carried by the existing conformance/distribution decisions.

The [PC inspection](../../../evidence/windows-target-inspection.json) records Windows 11 Pro 10.0.26200, AMD64, i5-14400F, RTX 5050 and driver 32.0.16.1074; it proves inventory only. Source hashes and document/link/tracker checks do not prove runtime parity. No installation, compiler probe, benchmark or retry of the blocked Go upgrade occurred.

Next: [Choose the scene renderer and GPU backend](07-choose-scene-renderer.md). Packaging and conformance remain open; no new human preference is required to record this selection.
