# Development cost, runtime cost, and validation

Research date: 2026-10-04. No compilation benchmarks or application performance measurements were run. This report defines what to measure and records source evidence relevant to the user's build-time and disk-space motivation.

## What is established

The user reports that GPUI-CE works well but Rust development takes too much time and disk space. The actual application, target directory sizes, selected features, machine performance, and measured build stages have not been audited. These are user-reported pain points, not a benchmark dataset.

At the pinned GPUI-CE revision, `gpui_render` has build dependencies on `gpui`, `wgsl-rs`, Naga 29.0.4 and Naga 24.0.0. The shader build pipeline generates/validates multiple dialects and produces native shader artifacts. The core styling interface also expands large groups of helper methods using Rust macros. These are concrete sources of build work, not measured explanations for this user's entire delay. [Renderer manifest](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_render/Cargo.toml), [shader pipeline](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_render/build.rs), [style interface](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/styled.rs).

Cargo's normal development profile carries debug information and incremental state. Incremental compilation trades extra disk storage for reuse on rebuild. Changing those settings is a baseline experiment, not evidence that the user's motivation is invalid. [Cargo profiles](https://doc.rust-lang.org/cargo/reference/profiles.html).

Go caches compilation artifacts and downloaded modules. Choosing Go does not remove caches, platform SDKs, native libraries, fonts, assets, or shader compilation. Automatic toolchain selection can also cache multiple Go versions. [Go build cache](https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching), [Go toolchains](https://go.dev/doc/toolchain).

The installed research-host versions were Go 1.26.5 windows/amd64 and rustc 1.97.0. [Environment record](../evidence/research-environment.json). These versions were inspected only; toolchains and global settings were left as found.

## Four cost categories to keep separate

| Cost | Included | Why it matters |
|---|---|---|
| Framework maintainer setup | Compilers, generators, native SDKs, shader toolchains | A port can move expensive work into releases but maintainers still pay it |
| Application developer setup | Required toolchain, modules, native dependencies, caches | Closest to the user's stated pain |
| Edit-to-visible-result latency | App edit, compile/link, restart or patch, restore test state | A warm widget edit can matter more than clean CI builds |
| Shipped application cost | Binary/package size, runtime RAM, CPU/GPU latency | Does not follow automatically from build-directory size |

Measure both absolute totals and the extra footprint attributable to one project. A shared Go cache can make a project directory look tiny while substantial storage lives elsewhere. A shared Cargo target directory can have the same accounting effect. Download compression and logical filesystem file lengths are different quantities; state which one is reported.

## Architecture choices that affect the outcome

### Native or shader libraries distributed prebuilt

Shipping artifacts can remove compilation from an ordinary app edit. It exchanges some local compiler work for release engineering, downloads, binary compatibility, and storage. Packaging one platform's artifacts separately is relevant. The maintained Go WebGPU binding inspected in the backend report uses prebuilt wgpu-native libraries; its combined library set exceeds 512 MB according to its README. That figure is not the size of a typical linked application. [Pinned WebGPU README](https://github.com/oliverbestmann/webgpu/blob/1e5d2824fac0657917c4801ad28137e555612441/README.md).

If shader generation is moved to framework release time, keep shader source and a reproducible regeneration path for framework maintainers. Validate layouts, struct alignment, resource bindings, and blend conventions against the Go scene representation. Generated WGSL/HLSL/MSL is not interchangeable solely because the same primitive names are used.

### Keep ordinary app edits away from expensive tools

Proposed developer-experience objective: consumers of a published framework use normal Go compilation; generation runs only when maintainers change the relevant definitions. Checked-in generated style forwarders could preserve GPUI chains while avoiding a generator invocation on every app build. Generated code still has parse/type-check/compile costs; benchmark it rather than equating generation with free work.

### Control dependency direction

The conceptual dependency direction should be components -> Base -> core, with platform/rendering adapters supplying core services. An application using just core should not need editor syntax parsers, every icon pack, all fonts, or all platform binary bundles. Separate packages can reduce compiled imports, while separate modules or release artifacts may be needed to avoid downloading bulky optional assets. Do not assume package separation guarantees small downloads. The final package/module layout is undecided. [Go dependency management](https://go.dev/doc/modules/managing-dependencies).

### Treat hot reload as a separate feature

GPUI-CE's current branch documents experimental Subsecond-based hot patching, with platform/build restrictions. This is a relevant existing capability if 'everything' includes the development loop. A Go port's shorter rebuild time does not imply equivalent live state preservation. Compare restart latency and state restoration explicitly. [GPUI-CE hot-patching documentation](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/docs/pages/hot-patching.typ).

## Future comparison protocol

This is an experiment specification to execute after prototype work is authorized. It is not a claim that the candidate can currently run these workloads.

### Fix the workload and environment

- Pin framework/source revisions, compiler versions, OS, target architecture, graphics driver, GPU, display scale/refresh rate, build options, and fonts.
- Choose a representative app slice from the user's real requirements: one multi-panel screen with an editable input, a virtual list, keyboard navigation, and a popup. Add a separate rendering stress scene.
- Compare the same visible content, interaction behavior, asset set, viewport, and number of actually built list rows.
- Keep caches isolated per experiment. Do not clear the user's shared caches to manufacture a cold run.
- Separate network acquisition, dependency compilation, app compilation/linking, process launch, and first presented frame. Repeat runs and report median plus spread.

### Build scenarios

| Scenario | Question answered |
|---|---|
| Empty cache plus dependency download | What does a new developer wait for and download? |
| Downloaded dependencies, empty build cache | What is cold compilation cost without network noise? |
| Warm no-change build | Is the build tool doing avoidable work? |
| One literal/style edit in an app view | What is the ordinary UI iteration cost? |
| One shared component edit | How much recompiles when reusable UI changes? |
| Core/interface edit | What is framework maintainer iteration cost? |
| Shader edit | Is generation confined to renderer work? |
| Release build | What is packaging cost and produced binary size? |

For Rust, `cargo build --timings` exposes compiler units, feature sets, dependency scheduling, and custom build work. It should be captured for the baseline instead of blaming a dependency from its name alone. [Cargo timings](https://doc.rust-lang.org/cargo/reference/timings.html).

### Storage accounting

Record toolchain footprint, module/registry/source downloads, project artifacts, shared caches, incremental state, debug symbols/PDBs, native archives, generated shaders, and final packages separately. Record the immediate post-build total and a realistic repeated-edit total. If a compiler or module download is reused across projects, show both amortized and first-project cost.

### Runtime scenarios

| Scenario | Metrics and correctness conditions |
|---|---|
| Idle window | Event-driven sleep, wakeups, CPU/GPU activity; no continuous redraw without need |
| Fast list scrolling | Frame-time distribution, constructed row count, allocations/frame, missed frames; stable focus and scroll anchoring |
| Text editing | Input-to-presentation latency; correct caret/selection and composition; undo/redo |
| Layout churn | CPU layout time, cache invalidation correctness, image/text cache behavior |
| Blur/shadow/clip scene | GPU and CPU time; ordering, alpha, clipping and visual reference comparisons |
| Async updates | Queue delay, cancellation behavior, stale-result suppression, race-free UI mutation |
| Multiwindow/DPI changes | Resource lifecycle, redraw correctness, per-window scale and focus |

At 60 Hz the frame interval is about 16.67 ms; at 120 Hz it is about 8.33 ms. These arithmetic intervals are not accepted budgets for the framework alone. The user must choose target hardware and acceptable latency/percentiles. A fast average can hide visible stalls.

Use allocation/heap profiles, CPU profiles and execution traces to distinguish GC, scheduler, layout and renderer costs. Include live heap and retained cache size; reducing allocation by retaining unlimited memory is not a satisfactory storage/performance result. [Go diagnostics](https://go.dev/doc/diagnostics).

## Conformance before performance claims

Every optimized scenario should have a behavior reference. A list that builds fewer rows by losing keyboard focus is not a successful optimization. A text renderer that omits complex shaping is not a comparable workload. Include Latin, Vietnamese combining marks, Thai, Arabic/Hebrew bidi, CJK composition, emoji sequences, and fallback fonts according to actual product scope; record unsupported cases explicitly.

For visual comparisons, use the same font files where licensing permits and distinguish layout geometry mismatches from rasterizer differences. Exact cross-platform screenshot equality is not an automatic requirement; define tolerances and approved platform differences first.

## Evidence gates, not implementation tickets

1. **Contract gate:** choose upstream snapshots, syntax adaptations, and lifecycle semantics with the user.
2. **Interface gate:** representative Base/custom components can be expressed without unstable internal access.
3. **Backend gate:** the chosen approach can support required clip/effect/text/input/accessibility cases.
4. **Developer-cost gate:** measured ordinary edits and storage meet agreed targets against a fair baseline.
5. **Ecosystem gate:** selected Base controls share behavior and can be independently styled.
6. **Distribution gate:** supported-platform builds and dependency acquisition are reproducible.

The work is large because multiple contracts interact, not because a specific line count predicts duration. Calendar estimates would be speculation without platform scope, team capacity and fidelity decisions. The next flow should resolve those choices before producing implementation tickets.
