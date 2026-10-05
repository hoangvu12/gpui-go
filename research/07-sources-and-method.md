# Sources, revisions, and research limits

Research date: 2026-10-04. Purpose: assess a faithful Go port of GPUI-CE, including the user's linked Base ecosystem, before choosing an implementation architecture.

## Method

1. Web browsing located current official documentation and repository material, including [GPUI Base](https://gpui-kit.com/base/) and the current Go language documentation.
2. Exa semantic search broadened discovery. Queries and returned source metadata/highlights are saved in [the Exa evidence](../evidence/exa-port-discovery.json). Search summaries were leads, not sufficient evidence for API or implementation claims.
3. GitHub CLI (`gh api` and repository search) resolved revisions, read manifests and repository trees, and retrieved source files. Reports trace concrete control flow and data types rather than relying only on README descriptions. Selected files were preserved locally; this was not a full repository checkout or line-by-line audit of every package.
4. Installed Go documentation and version commands established the local toolchain boundary. No toolchain was upgraded. No application, framework, or renderer was implemented or built.
5. Findings were separated into source observations, inferred port requirements, candidate designs, and unresolved decisions. Upstream test names describe inspected tests; they do not imply those tests were executed locally.

## Main references

The links below pin source snapshots. Scope describes what was investigated; it does not certify repository maturity, release status, or interoperability.

| Repository and revision | Inspection scope |
|---|---|
| [GPUI-CE `254b5dbd`](https://github.com/gpui-ce/gpui-ce/tree/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a) | Deep core audit: entities, elements, styles, events, window, scene, Taffy, text, platform contracts, scheduler, build manifests |
| [Original Zed GPUI `a8468907`](https://github.com/zed-industries/zed/tree/a84689073d296dfd39987bc7dd478e43ef76d83a/crates/gpui) | User-requested follow-up comparison: counter example, element/view/style contracts, conversion derive, typed events/listeners, actions and key-dispatch docs; comparison only, not a new parity pin |
| [GPUI Kit `4c7f1350`](https://github.com/longbridge/gpui-kit/tree/4c7f1350331562436df868c55ac33bebc4c6406c) | Base and styled component architecture; representative controls, overlays, input, virtual list, dock, themes, manifests and notices |
| [CE component fork `c0820693`](https://github.com/gpui-ce/gpui-component/tree/c08206932417f863062d2ab70cc2854c6ca04238) | Manifest plus bounded button/popover/virtual-list comparison |
| [Gio `3397eb8f`](https://github.com/gioui/gio/tree/3397eb8f4df4d59eab121f64975173ea0d3fdb38) | Public renderer and paint operations, window/custom-renderer seams, Windows platform, IME and text |
| [go-gui `58bae296`](https://github.com/go-gui-org/go-gui/tree/58bae296bcc1112f9e118829e25225420c6712ba) | Rendering commands and Metal rendering/text implementation; framework coupling |
| [go-text/typesetting `64922d2b`](https://github.com/go-text/typesetting/tree/64922d2b48df3ea2ebae4733781b7c0d0c00d91b) | Shaping capabilities and distinction from complete native text/input integration |
| [gogpu/wgpu `052a7e52`](https://github.com/gogpu/wgpu/tree/052a7e526b23454334761537410a5e50406bdfe4) | Native implementation, module and Metal/Vulkan foreign-call boundaries |
| [go-webgpu/webgpu `907b82e3`](https://github.com/go-webgpu/webgpu/tree/907b82e37b752ea9a2735e5beec4c9e9925f6315) | Loader and native runtime dependency; zero-cgo does not imply no native runtime |
| [SCKelemen/layout `6f734941`](https://github.com/SCKelemen/layout/tree/6f7349411616cbb31fc83c4b5d8baf48ccccebcb) | Units, documented compliance/limitations, and suitability for Taffy parity |
| [Yororen UI `346502ac`](https://github.com/MeowLynxSea/yororen-ui/tree/346502ac654b77fdaff3be2d7444fca8783acfc9) | Independent CE ecosystem probe: headless button, renderer registry and default renderer |

Supplementary candidate inspection appears in the backend report and historical discovery:

| Repository and revision | Limited purpose |
|---|---|
| [gogpu/ui `4d78f0d0`](https://github.com/gogpu/ui/tree/4d78f0d03a87c5c48bb66d3dc34f625de43067d0) | Existing Go UI candidate, distinct authoring model |
| [Fyne `31221918`](https://github.com/fyne-io/fyne/tree/312219185e6aebb7ac7728abc7044089b657ef27) | Existing toolkit comparison; not GPUI compatibility evidence |
| [go-gl/glfw `d41da22a`](https://github.com/go-gl/glfw/tree/d41da22a9587f777098f96d37014f6cdd35d1afb) | Windowing/dependency option |
| [cogentcore/webgpu `834052c4`](https://github.com/cogentcore/webgpu/tree/834052c4692498a8afff69fce01bdb8450ab4381) | Older binding lineage and successor pointer |
| [oliverbestmann/webgpu `1e5d2824`](https://github.com/oliverbestmann/webgpu/tree/1e5d2824fac0657917c4801ad28137e555612441) | cgo/prebuilt-native route and upstream library-size statement; no local size measurement |
| [energye/gpui `eebfbcd0`](https://github.com/energye/gpui/tree/eebfbcd036d30ab179492694e982b1b4122e59f1) | Name-collision check: README, go.mod and basic rendering example only |

The last candidate's module depends on `energye/lcl` and includes a local replacement; its inspected basic example uses `TGPUControl.SetOnRender` with drawing operations. This does not establish the Zed/CE entity/element/Base contract. Its name alone should not be treated as an already completed port. [Module](https://github.com/energye/gpui/blob/eebfbcd036d30ab179492694e982b1b4122e59f1/go.mod), [example](https://github.com/energye/gpui/blob/eebfbcd036d30ab179492694e982b1b4122e59f1/examples/basic/main.go).

[GitHub search evidence](../evidence/discovery/github-gpui-go-search.json) records the bounded `gpui language:Go` query. Results include unrelated projects; search ranking, indexing and query coverage prevent a universal claim that no port exists. The conclusion is that this investigation did not verify a ready-made port meeting the requested scope.

## Language and toolchain sources

- [Go 1.27 release notes](https://go.dev/doc/go1.27), [generic-method explanation](https://go.dev/blog/generic-methods), and [method declarations](https://go.dev/ref/spec#Method_declarations): current language capabilities and interface restrictions.
- [Installed Go evidence](../evidence/go-language/): Go 1.26.5 method grammar, `runtime.AddCleanup`, `weak.Pointer`, and `runtime.LockOSThread`. The older installed grammar and current website differ intentionally; do not infer the local compiler supports the newest syntax.
- [Environment record](../evidence/research-environment.json): installed Go, Rust and GitHub CLI versions. Having both compilers available is not a benchmark result.

Other primary sources are cited beside the claims in individual reports. Repository license and documentation notices were inspected where discussed; this is a record of those notices, not a project-wide licensing decision. Assets and dependencies need their own accounting if later incorporated.

## Evidence integrity and limits

[Evidence README](../evidence/README.md) identifies retrieval formats and manifests. Local SHA-256 values detect changes to saved files. Core text snapshots can have normalized line endings; API/discovery files were decoded directly from Base64. Backend snapshots were retrieved using HEAD with the contemporaneous revision recorded, as their manifest states; a local hash alone does not prove identity with the pinned remote blob. Source citations use the recorded commit to keep claims reviewable.

Some sources were read remotely without being preserved locally. The evidence directory is not sufficient to build the upstream projects, and the investigation does not establish that all recorded revisions run together. Only the explicitly named component slices were compared across forks.

The later [original GPUI comparison](10-original-gpui-authoring.md) resolved Zed's `main` to `a84689073d296dfd39987bc7dd478e43ef76d83a` (commit timestamp `2026-10-03T08:53:38Z`) and then fetched selected files at that exact revision. [Authoring](../evidence/original-gpui-authoring/manifest.json) and [events/actions](../evidence/original-gpui-events/manifest.json) manifests record exact Base64-decoded source bytes, Git blob IDs and local SHA-256 hashes. This check does not move the accepted GPUI-CE target and does not claim the repositories are equivalent. Original example tests were inspected, not run.

The following remain untested: compile speed and disk savings, GPU backend conformance and reliability, text/pixel parity, platform accessibility, real IME behavior, allocation/GC costs, and the proposed Go API shapes. The [validation protocol](05-build-footprint-and-validation.md) describes how to investigate them after implementation work is authorized. Research completeness here means a reviewable decision packet, not proof of a production framework.
