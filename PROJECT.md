# Project brief

## Confirmed intent

Build a Go port with full 1:1 GPUI-CE core behavior, familiar typed/fluent authoring and approximately 98% visual closeness. The user already likes GPUI-CE; Rust development build time and disk consumption motivate the port. The visual intent is not a pixel-match or SSIM threshold. Base and other component libraries come after core; implementation stages do not reduce the destination to a showcase or app-specific subset.

The permanent workspace is `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go`. The local module is `gpui-go`, with root package name `gpui` and Go floor 1.27.0. Public branding, module publication and project licensing remain future publication work.

## Current phase

**Implementation is authorized and in progress.** On 2026-10-05 the user explicitly authorized full implementation of the [33-ticket backlog](.scratch/gpui-core-implementation/plan.md) in a dedicated implementation chat (Roboco, harness `pi`, model `iroha/dashscope/glm-5.3`): "ok now use roboco mcp, create a new chat that use /implement skill to do all those tickets, use iroha dashscope glm 5.3 with pi harness". This supersedes earlier planning-only restrictions in repository documents. Routine engineering decisions are delegated; implementation and verification of the whole local backlog, including the runtime/native work its acceptance criteria require, is authorized. Work proceeds from [ticket01](.scratch/gpui-core-implementation/issues/01-reference-harness.md) through the dependency frontier until the backlog completes or a concrete external blocker stops it. No git repository exists in this workspace; the implement skill's commit step is recorded as blocked, and work continues without commits or publication.

Verified maintainer toolchains on this PC: Go 1.27.1 (`C:\Program Files\Go\bin\go.exe`), Rust 1.97.0 MSVC (`cargo 1.97.0`, host `x86_64-pc-windows-msvc`), GitHub CLI, and network access to the pinned CE repository.

**Planning history (superseded for authorization, retained as evidence):** all ten [decision/verification tickets](.scratch/gpui-core/map.md) are resolved. The [implementation specification](.scratch/gpui-core/spec.md), [conformance contract](docs/conformance-contract.md), [capability inventory](docs/conformance-inventory.md) and [33-ticket implementation plan](.scratch/gpui-core-implementation/plan.md) were produced under the earlier bounded authorization.

The user installed Go 1.27.1 themselves after the earlier assistant MSI command was rejected before execution. Main verified the installed compiler and, under the user's explicit "ok do them" authorization, completed the proposed signature verification, conformance plan, specification and local tickets together. Root and isolated-fixture tests/vet pass, along with 29 negative/control pairs. [Toolchain record](evidence/go-toolchain-installed.json), [signature results](evidence/authoring-signature-validation.json).

The implemented code remains the bounded [fluent-authoring helper](authoring/README.md). The wider [API fixture](.scratch/signature-validation/README.md) uses compile-only stubs and does not implement a runtime. The compiler checks corrected the constructor/type collision to `Div()`/`DivElement`; an evidence-module boundary keeps upstream reading snapshots out of root builds without changing their bytes. No native DLL, independent oracle, GUI, benchmark or full parity result has been produced.

For continuation, read [the resume note](handoff/resume.md). The next implementation path begins with [independent reference fixtures and source capability enumeration](.scratch/gpui-core-implementation/issues/01-reference-harness.md). Planning completion is not evidence of framework completion.

## Accepted scope and workflow

- Full core first, Base later. Windows AMD64 on the user's current PC is the first usable/verified target; broader Windows versions/architectures and other OSes follow later.
- CE reference is fixed at `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` (researched main, 2026-10-03), distinct from published crate 0.2.2. Zed `a84689073d296dfd39987bc7dd478e43ef76d83a` is comparison evidence only. Upstream changes do not move the target silently.
- Native dependencies are permitted. App developers use Go plus exact prebuilt assets, with cgo disabled and no local Rust/C/resource/shader compiler or first-run download. Maintainers may build native services.
- No upfront comparative benchmark or hard build-time/disk/runtime-memory budget. Modest extra RAM is acceptable while preserving responsiveness; investigate noticeable stuttering or input lag. Module-format limits remain real delivery constraints.
- Local Markdown maps/specs/issues under `.scratch/`, default triage labels, shared AGENTS/CLAUDE instructions, one root domain glossary and lazily created ADRs. No remote repository or external issue publication has been established.
- Routine engineering choices are delegated to source-backed research and main review. Ask only about genuine human tradeoffs. Research uses Roboco side chat with harness `pi`, model `iroha/dashscope/glm-5.3`, reasoning `high`; main guides, reviews and integrates.
- User authorization now covers full implementation and verification of the complete local 33-ticket backlog, including the runtime/native work its acceptance criteria require, in the dedicated implementation chat (2026-10-05). Base, benchmark campaigns and public releases remain future work.

## Selected contracts

| Area | Selected boundary |
|---|---|
| [Authoring](docs/authoring-decision-round.md) and [counter examples](docs/counter-view-api.md) | Go 1.27 typed/generic methods, constructor-bound Styled, declared typed events, canonical actions, checked dynamic children, explicit ViewOf and complete custom Element phases. The bounded declarations compile; runtime guarantees remain unimplemented. |
| [Ownership](docs/runtime-ownership-contract.md) | Independent scoped leases, borrowed access, weak callbacks, deterministic logical release/effects, scoped subscriptions/tasks, owned result handoff and retained frame/cache resources. Assignment aliases a lease; RetainInto creates independent ownership. |
| [Layout](docs/layout-contract.md) | Locked Taffy 0.13.0 in the native artifact; Go owns GPUI adaptation, refinement, rem/DPI/rounding, measurement and inline orchestration. Independent full-tree/callback oracles are required. |
| [Windows, text and input](docs/windows-platform-contract.md) | Go Win32 host on the foreground OLE STA, pinned native Parley/DirectWrite/Swash services and AccessKit/COM; IMM32, UTF-16 ranges, PerMonitorV2 and native integration retained. |
| [Renderer](docs/renderer-contract.md) | Pinned D3D11/DirectComposition and scene kernel, prebuilt shaders, actual GPU completion, generation-safe resources and same-device capture with explicit frame/copy ownership. |
| [Distribution](docs/distribution-contract.md) | One exact combined Windows AMD64 DLL embedded by default; verified bundle alternative, offline loader, private ABI/capabilities, actual import/CRT checks, consumer DPI resources and process-lifetime code residency. Go owns assets/tasks/HTTP; native services supply pinned image/SVG algorithms. |
| [Conformance](docs/conformance-contract.md) and [inventory](docs/conformance-inventory.md) | Independent pinned reference; exact, behavior, visual and delivery gates; full per-symbol/profile accounting; controlled visual calibration and deliberate fixed-pin updates. |

These are reviewed selections under the user's delegation, not verbatim user choices of every native mechanism. Canonical contracts supersede proposal details in historical research. Unavailable/unsupported native operations need source evidence; missing port features and absent optional flags are not automatic exceptions.

## Remaining execution evidence

The implementation backlog owns reference recordings, per-symbol feature applicability, first-artifact size/ABI feasibility, font/driver/IME/UIA behavior, the nonzero-origin caret-coordinate discrepancy and numerical visual calibration. Full-core acceptance requires all applicable rows and required gates, with every blocked environment and narrow approved exception reported. A working counter or successful native build is only an intermediate milestone.

No numerical visual threshold is frozen before measuring reference repeatability and detecting deliberately injected material defects. No live behavior has passed merely because its API compiles. If execution contradicts a contract, reopen the affected decision with the reproducer rather than changing the target silently.

## References

[Research index](README.md), [glossary](CONTEXT.md), [Wayfinder map](.scratch/gpui-core/map.md), [specification](.scratch/gpui-core/spec.md), [implementation plan](.scratch/gpui-core-implementation/plan.md), and [validation evidence](evidence/README.md).
