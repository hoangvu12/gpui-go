# Choose the scene renderer and GPU backend

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex /root (Wayfinder continuation, 2026-10-04)
Blocked by: 01, 02, 04, 06

## Later status — 2026-10-05

This ticket preserves its original decision and chronological comments. Subsequent [signature verification](10-validate-authoring-signatures.md) passed on user-installed Go 1.27.1, all ten Wayfinder children are resolved, and the [specification](../spec.md) plus [implementation backlog](../../gpui-core-implementation/plan.md) are complete. Earlier open-task/compiler-blocker statements below describe their recorded date; broader runtime/native behavior remains unimplemented.

## Question

Which rendering approach preserves the pinned GPUI scene contract on the selected Windows platform without undermining the development goals?

Evaluate scene ordering, clipping, shadows, paths, text atlases, opacity/filter groups, backdrop effects, imported surfaces, GPU ABI layout, device loss, and resource retirement. Native dependencies are permitted, but no library has been selected. Distinguish source/API capability evidence from actual driver validation that remains future work.

## Existing evidence

The [Windows platform contract](../../../docs/windows-platform-contract.md) defines the host/renderer seam: a leased HWND and window generation, device-pixel extent, scale, visibility, resize generations and foreground completion messages. The text service supplies AlphaMask, BgraSubpixelMask and BgraColor CPU glyph data from pinned Parley/DirectWrite/Swash behavior. Select upload ownership, pixel interpretation/blending, atlas invalidation, surface/device loss, pacing and actual completion fencing here. A Go-owned Win32 host must not acquire a second Rust app runtime or implicitly choose the existing CE Windows constructor's GPU backend.

Honor the [runtime ownership contract](../../../docs/runtime-ownership-contract.md): frame/cache reuse carries resource dependencies, unpublished construction is separately owned, and submitted native work retains resources until confirmed completion or safe abort. Choose backend fencing, device-loss handling and teardown mechanics here; frame counters and GC are not completion evidence.

- [gpui core contracts](../../../research/01-gpui-core-contracts.md)
- [backend text layout options](../../../research/03-backend-text-layout-options.md)
- [build footprint and validation](../../../research/05-build-footprint-and-validation.md)

## Resolution boundary

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Comments

### Claim and focused research, 2026-10-04

The user instructed continuing Wayfinder after the Windows platform resolution. Main claimed the renderer decision and will guide/review the required Roboco `pi` / `iroha/dashscope/glm-5.3` side chat. This work selects the renderer boundary and verification obligations; no runtime/native implementation, installation, compiler probe or benchmark is authorized.

### Research and review, 2026-10-04

The required side chat completed [renderer research](../../../research/17-scene-renderer-strategy.md), then corrected specific source/ABI/completion claims under main guidance. Main saved 12 pinned renderer/shader/capture snapshots, inspected the scene/plan/ABI and lockfile, and checked Microsoft query, resize and capture APIs. A final read-only side-chat review found the composition-disabled capture-device trap: query `IDXGIDevice` from `ID3D11Device` rather than using a DComp-gated optional member. The canonical contract includes that fix. Main tightened source-versus-proof language and preserved all execution limits.

## Answer

Resolved at documentation level on 2026-10-04 under the user's instruction to keep going and delegated routine engineering choices. Native dependencies and Go plus prebuilt artifacts are explicit user preferences; the backend/extraction choices below are reviewed engineering selections, not direct user quotations or authorization to implement the runtime.

Select a **maintainer-built native service extracting the pinned CE Direct3D 11 / DirectComposition renderer, finite scene kernel, atlas and shader tooling**, consumed by Go with `CGO_ENABLED=0`. Go owns GPUI application state, element/layout/paint traversal, caches and HWND lifecycle. Native code owns scene insertion/order/replay/finish/planning and GPU packing/execution; it contains no second Rust GPUI app or entity runtime. Full details live in the [renderer contract](../../../docs/renderer-contract.md).

Preserve the native Windows reference: adapter enumeration with 11.1/11.0 BGRA device requests, triple-buffer BGRA8 flip-sequential presentation, composition-premultiplied versus HWND-ignore alpha, all primitive/text/path/filter pipelines, two levels of isolated content-filter groups with deeper inline behavior, and separately paired surface opacity. Preserve font raster style and tagged alpha/subpixel/color pixel meaning. Cache/scene resources remain owned through replay, separately from submitted GPU use.

Use a private versioned semantic paint-command protocol with generation-checked resources and explicit copied staging. Native scene insertion retains BoundsTree/layer/order-floor semantics; Go does not attempt to reconstruct them by sorting primitive arrays. The `repr(C)` source types are evidence for GPU packing, not automatically a stable wire ABI. Maintainers generate and validate WGSL/HLSL/DXBC artifacts using pinned wgsl-rs, Naga 29.0.4/legacy 24.0.0 and Windows shader-model-5 compilation; consumer builds and startup do not run shader compilers.

Add explicit D3D11 event-query retirement for accepted submissions. Acceptance is distinct from completion. Neither Present, Flush, timeout nor a device-generation change frees resource leases. A terminal acknowledgment requires completed GPU use or native-confirmed safe abort; otherwise retain/quarantine dependencies through teardown. Serialize immediate-context operations on the foreground at safe app boundaries; host pacing remains a separate DWM wait worker. Keep resize, modal reentry, device recovery and HWND destruction within the existing ownership contract.

The reference native surface path attempts direct SRV import of WindowsCapture textures; general cross-device or optional wgpu surfaces are not assumed supported. For framework-owned capture, select a labeled same-device native producer adapter, obtaining the WinRT device from the renderer's actual D3D11 device independently of DComp mode. Retain actual checked-out frame ownership through a bounded foreground copy and GPU copy completion; app-visible captures own immutable destination storage. Preserve capture semantics/settings while verifying producer synchronization, content bounds and device loss. This is a new ownership adaptation, not an assertion that the pin's raw-texture clone already provides it.

### Rationale and alternatives

- Native D3D11 reuse follows what the pinned Windows host constructs and retains its rendering algorithms. It concentrates new work in extraction, ABI, ownership and host integration; it does not prove parity by construction.
- The pinned wgpu renderer (locked 29.0.4) and wgpu-native remain alternate approaches with different backend/interop behavior and another equivalence burden. Optional custom-GPU/surface feature gates are distinguished from backend availability. No unmeasured binary-size or speed argument is used.
- A pure-Go renderer can use precompiled shaders, but still requires porting and validating the pipelines, GPU packing, atlas and device management. That extra compatibility work is not selected for the first Windows target.
- Preserving a small native scene kernel is permitted by the accepted native-dependency policy and avoids duplicating subtle insertion/replay behavior. Application authoring and state remain Go.

### Limits and next decision

No renderer, native bridge, shader artifact or conformance test was built/run. Source hashes and documentation/dependency checks are not driver proof. Required gates include exact scene-plan/replay comparisons against an independent pinned oracle; GPU ABI checks; all pipeline and alpha/text/filter cases; capture device/producer lifetime; resize/occlusion/modal/multiwindow behavior; delayed queries, partial-submit errors and safe-abort/quarantine; and clean Go-only consumer loading. Complete optional-feature/source capability coverage remains an explicit distribution/conformance inventory, not permission to silently shrink the full Windows core target.

The blocked Go upgrade was not retried, and no older-toolchain probe, installation or benchmark occurred. No ready-made extraction/bridge has been verified. The next decision is [Define module boundaries and native distribution](08-define-module-and-native-distribution.md); conformance and signature verification remain open.
