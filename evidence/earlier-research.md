# Go foundations for a GPUI-like framework

Research date: 2026-10-04. Read-only web research and GitHub CLI `gh api` source inspection; no implementation or performance measurements. Source HEADs below are snapshots, not necessarily released versions.

## Conclusion

A GPU-drawn native Go UI is already demonstrated by Gio. For a GPUI-style Go API, the lowest-risk starting point is a layer over Gio's window/input/drawing/text facilities, while designing retained identity, state, invalidation, declarative styling, and component composition above it. This is an architectural recommendation, not evidence that Gio is API-compatible with GPUI or automatically reproduces its layout/performance. Gio's immediate operation model and weighted-axis Flex layout differ from a full CSS-like layout system.

## Source inspections

| Repository | Inspected HEAD | Important inspected paths |
|---|---|---|
| gioui/gio | 3397eb8f4df4d59eab121f64975173ea0d3fdb38 | go.mod; gpu/api.go; layout/flex.go; app/ime.go; app/os_windows.go; app/os_macos.m; app/os_js.go; io/semantic/semantic.go |
| go-text/typesetting | 64922d2b48df3ea2ebae4733781b7c0d0c00d91b | README.md; shaping/shaping.go; recursive tree |
| cogentcore/webgpu | 834052c4692498a8afff69fce01bdb8450ab4381 | README.md; recursive tree |
| oliverbestmann/webgpu | 1e5d2824fac0657917c4801ad28137e555612441 | README.md; go.mod; wgpu/wgpu.go |
| fyne-io/fyne | 312219185e6aebb7ac7728abc7044089b657ef27 | go.mod; accessibility.go; internal/driver/glfw/accessibility_notdarwin.go; recursive tree |
| go-gl/glfw | d41da22a9587f777098f96d37014f6cdd35d1afb | README.md; recursive tree |

## Gio: strongest established foundation for this particular goal

The renderer is not wholly internal: public `gpu.New(API)` returns a `GPU` with `Frame(*op.Ops, RenderTarget, image.Point)`, `Clear`, and `Release`. However, `NewWithDevice` accepts an internal driver type and is explicitly marked internal-use-only; do not plan to depend on Gio's internal driver interfaces as a stable general GPU abstraction. The straightforward reusable boundary is normal app/window plus public operations, or the public GPU API with correctly managed native targets. [Renderer entry points](https://github.com/gioui/gio/blob/3397eb8f4df4d59eab121f64975173ea0d3fdb38/gpu/gpu.go).

- Actual native graphics APIs are exposed for Direct3D 11, Metal, Vulkan, and OpenGL/OpenGL ES. This is not a WebView and does not require wgpu-native. The module uses go-text/typesetting v0.3.5. [GPU API source](https://github.com/gioui/gio/blob/3397eb8f4df4d59eab121f64975173ea0d3fdb38/gpu/api.go), [dependencies](https://github.com/gioui/gio/blob/3397eb8f4df4d59eab121f64975173ea0d3fdb38/go.mod).
- Its input model routes events by tags and builds an operation list for each frame. A retained GPUI-like component API could generate those operations, but identity, invalidation, and lifecycle must be designed explicitly. [Input architecture](https://gioui.org/doc/architecture/input). Its Flex is a weighted axis layout, not evidence of complete CSS Flexbox/Grid compatibility. [Flex source](https://github.com/gioui/gio/blob/3397eb8f4df4d59eab121f64975173ea0d3fdb38/layout/flex.go).
- There is real composition handling: platform-independent IME ranges/snippets, UTF-8/rune/UTF-16 conversions, surrounding-text updates, Windows IMM preedit/commit separation and candidate-window positioning. These are substantial pieces worth reusing; existence is not a claim of bug-free support for every IME. [IME state source](https://github.com/gioui/gio/blob/3397eb8f4df4d59eab121f64975173ea0d3fdb38/app/ime.go), [Windows source](https://github.com/gioui/gio/blob/3397eb8f4df4d59eab121f64975173ea0d3fdb38/app/os_windows.go).
- It has semantic label/description/class/selected/enabled operations for accessibility. Do not equate those operations with verified complete Windows/macOS/Linux screen-reader support; a platform audit and actual assistive-technology testing remain required. [Semantic source](https://github.com/gioui/gio/blob/3397eb8f4df4d59eab121f64975173ea0d3fdb38/io/semantic/semantic.go).
- Windows default setup officially needs no extra dependencies. Linux requires native development packages and Apple requires Xcode. Thus Gio is especially attractive for this user's Windows workflow, but â€œGo GUI means pure Go on every platformâ€ would be false. [Windows install](https://gioui.org/doc/install/windows), [Linux install](https://gioui.org/doc/install/linux), [macOS install](https://gioui.org/doc/install/macos).

## Text: reuse go-text, do not restart typography

go-text/typesetting supplies pure-Go typesetting and is already shared by Gio, Fyne, and Ebitengine. Its tree contains font parsing, font discovery, bidi, script/language support, segmentation, shaping, and a Go HarfBuzz implementation. `HarfbuzzShaper` caches fonts and reuses a shaping buffer; its source explicitly recommends reuse for time/memory efficiency. This can remove a C HarfBuzz dependency from the shaping layer, but text shaping alone does not provide native IME integration, editor caret/selection behavior, rasterization/GPU rendering, or accessibility. APIs remain v0 and the maintainers warn about breaking minor-version changes. [README](https://github.com/go-text/typesetting/blob/64922d2b48df3ea2ebae4733781b7c0d0c00d91b/README.md), [shaping implementation](https://github.com/go-text/typesetting/blob/64922d2b48df3ea2ebae4733781b7c0d0c00d91b/shaping/shaping.go).

## WebGPU bindings: useful GPU abstraction, wrong assumption about disk footprint

The original cogentcore/webgpu README now explicitly says it will not be updated and directs new users to oliverbestmann/webgpu. Both are wrappers around native wgpu-native, not a new pure-Go GPU implementation. [Archived-direction README](https://github.com/cogentcore/webgpu/blob/834052c4692498a8afff69fce01bdb8450ab4381/README.md).

The maintained fork uses prebuilt native libraries, so normal application development can avoid compiling Rust locally. However, the README says the combined libraries exceed 512 MB and therefore splits them into platform modules/branches. `go.mod` includes Android, Darwin, iOS, Linux, and Windows library modules; `wgpu/wgpu.go` blank-imports all five under `!js` and uses cgo. Consequently this is a concrete disk/download concern, not an automatic solution to Rust's development footprint. Exact downloaded/cache sizes were not measured. The GPU wrapper still needs a window/input layer, text stack, layout, widget/state system and semantics bridge. [Maintained README](https://github.com/oliverbestmann/webgpu/blob/1e5d2824fac0657917c4801ad28137e555612441/README.md), [module](https://github.com/oliverbestmann/webgpu/blob/1e5d2824fac0657917c4801ad28137e555612441/go.mod), [native binding](https://github.com/oliverbestmann/webgpu/blob/1e5d2824fac0657917c4801ad28137e555612441/wgpu/wgpu.go).

## GLFW and Fyne

GLFW provides windows/input/context handling, not a GUI framework. go-gl/glfw includes and compiles GLFW C sources; native SDK/development packages remain prerequisites and calls need main-thread handling (`runtime.LockOSThread` in its example). It is reasonable for a renderer experiment but choosing it still leaves editor-grade input and accessibility integration to audit/build. [GLFW README](https://github.com/go-gl/glfw/blob/d41da22a9587f777098f96d37014f6cdd35d1afb/README.md).

Fyne is an existing full toolkit, and the inspected module uses go-gl/gl, go-gl/glfw, go-text/typesetting, and go-text/render. It can be considered if the goal is shipping an application rather than reproducing GPUI's custom API. Avoid outdated claims that it has no accessibility: inspected HEAD includes `Accessible`/`AccessibleRole` marked Since 2.8 and native platform files. However, its desktop fallback `updateAccessibility` is a no-op under `!accessibility || (!darwin && !windows)`, so availability is build-tag and platform dependent. HEAD functionality is not necessarily in an installed release. [Fyne dependencies](https://github.com/fyne-io/fyne/blob/312219185e6aebb7ac7728abc7044089b657ef27/go.mod), [accessible API](https://github.com/fyne-io/fyne/blob/312219185e6aebb7ac7728abc7044089b657ef27/accessibility.go), [desktop fallback](https://github.com/fyne-io/fyne/blob/312219185e6aebb7ac7728abc7044089b657ef27/internal/driver/glfw/accessibility_notdarwin.go).

## Decision implications

1. If the main goal is faster iteration and smaller development storage, evaluate a representative Go/Gio application first; rebuilding platform layers is unnecessary to evaluate that hypothesis.
2. If GPUI's programming model is the attraction, design a Go-native component/state/style layer over Gio rather than translate Rust ownership machinery line-for-line.
3. If exact GPUI renderer behavior is essential, a custom renderer with native/WebGPU backends is feasible but a much larger project; prebuilt backend libraries exchange compile time for download/disk cost.
4. Measure cold build, warm no-op build, one-widget-edit build, toolchain/module/build-cache sizes, frame CPU/GPU time and allocation/GC behavior on the same target app. None of the inspected sources establish a specific Go-vs-Rust speedup or disk-reduction factor.

## Related source findings from the coordinating researcher

GPUI CE snapshot `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`: element lifecycle separates request_layout, prepaint and paint; AnyElement contains ArenaBox. The arena bulk-resets storage while tracking destructors. The entity map uses SlotMap IDs, reference counts and leases. Taffy supplies flex/grid layout; PlatformTextSystem abstracts platform typography; scenes contain typed, ordered primitives. A Go port should preserve these responsibilities rather than copy reference counting blindly into a garbage-collected language. The `gpui_render` package build-depends on gpui, naga 29.0.4, older naga 24 and wgsl-rs; build.rs generates WGSL, HLSL/DXBC, GLSL and MSL. These establish plausible sources of build complexity, not measured bottlenecks. [GPUI CE repository](https://github.com/gpui-ce/gpui-ce/tree/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a).

`go-gui-org/go-gui` snapshot `58bae296bcc1112f9e118829e25225420c6712ba` deserves evaluation alongside Gio when an existing framework may suffice. Source inspection found object pools, separate layout/render invalidation and a command queue in `gui/window_update.go`, visible-range and overscan handling in `gui/view_virtual_list_build.go`, IBus preedit/commit/caret integration, and an Objective-C accessibility bridge on macOS. Its Linux X11/EGL and Windows WGL backends advertise cgo-free operation; macOS Metal uses cgo. These are implementation findings, not measured equivalence to GPUI. [Update loop](https://github.com/go-gui-org/go-gui/blob/58bae296bcc1112f9e118829e25225420c6712ba/gui/window_update.go), [virtual list](https://github.com/go-gui-org/go-gui/blob/58bae296bcc1112f9e118829e25225420c6712ba/gui/view_virtual_list_build.go), [IBus](https://github.com/go-gui-org/go-gui/blob/58bae296bcc1112f9e118829e25225420c6712ba/gui/backend/ibus/doc.go), [macOS accessibility](https://github.com/go-gui-org/go-gui/blob/58bae296bcc1112f9e118829e25225420c6712ba/gui/backend/metal/a11y_darwin.go).

`gogpu/ui` snapshot `4d78f0d03a87c5c48bb66d3dc34f625de43067d0` remains a candidate to watch, but its accessibility documentation places native adapters in the future and roadmap Phase 8 still includes IME/native adapters. Its text field moves/deletes rune-by-rune, which is not sufficient for all grapheme clusters. Do not infer editor-grade international input or assistive-technology readiness from GPU rendering or abstract semantic types. [Accessibility documentation](https://github.com/gogpu/ui/blob/4d78f0d03a87c5c48bb66d3dc34f625de43067d0/a11y/doc.go), [text field events](https://github.com/gogpu/ui/blob/4d78f0d03a87c5c48bb66d3dc34f625de43067d0/core/textfield/event.go).

