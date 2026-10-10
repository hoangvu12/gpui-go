# Render SVGs with lazy fonts and correct alpha conversion

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: implement-spec wave-3 worker (background subagent, model dashscope/glm-5.3; evidence: ../../evidence/ticket19-svg-fonts.json; the native service + approved rebuild done in-session by the orchestrator)
Blocked by: 09, 18

## Question

An SVG element requests missing font assets, resumes decoding once they arrive and renders the reference pixels.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md), [windows platform contract](../../../docs/windows-platform-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Integrate pinned resvg/usvg parsing/rasterization, sizing/scaling, 8192 limit, alpha masks and smooth-scale behavior through explicit native result ownership.
- [ ] Preserve fallback/emoji selection and the renderer font snapshot/missing-cache lifetime; native NeedsFontAssets requests return owned data for Go task orchestration.
- [ ] Resume exactly once when requested bytes arrive, handling cancellation/failure and concurrent consumers without a synchronous foreground callback into a native worker.
- [ ] Verify premultiplied RGBA to straight BGRA swap/unpremultiply arithmetic, transparent pixels and truncating alpha conversion using oracle fixtures.
- [ ] Draw actual SVG/image elements with text, emoji, masks and malformed inputs; prove asset/font/pixel resources retire correctly after caching and shutdown.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Shape text and expose paragraph, caret and hit-test geometry](09-text-geometry.md)
- [Load assets asynchronously and animate image elements](18-asset-loading.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
- 2026-10-06 — DEFERRED (successor chat): pinned resvg/usvg SVG decoding lives in native services; blocked by the no-cargo/DLL-rebuild constraint (and by ticket17). No pure-Go resvg equivalent exists at reference quality. No work started.


## Answer

Resolved 2026-10-10/11 under the user's DLL approval ("u can do the dll work, im not home rn") — the second bounded native rebuild of the implementation phase.

**Native (the orchestrator, in-session):** reference/native/src/svg.rs — the SVG service in reserved slot 8, capability bit 9, native revision 9: the pinned resvg/usvg 0.48.1 stack driven through the CE's public `SvgRenderer` paths (the constructor's system+bundled font resolution with the emoji-presentation fallback, the three sizing modes with `SMOOTH_SVG_SCALE_FACTOR=2`, the 8192 clamp, the premultiplied-RGBA→BGRA output, the alpha-mask entry re-implemented against the public surface since the pin's is pub(crate)) plus the owned font-asset surface (`svg_font_asset_count/path/has/add` — the NeedsFontAssets seam; a font added after a first parse is seen by the next parse, the port's font-snapshot lifetime). The reserved slot array grew from 8 to 16 entries (an additive ABI record growth, 152→216 bytes, mirrored in lockstep by the Go loader; the size self-checks enforce the match), and two latent Rust-side self-check bugs from ticket 17's window were fixed (the stale `reserved[7]`-is-null and missing image-bit mask assertions — invisible because full-workspace `cargo test` is forbidden by the resource rules). resvg/usvg were already in the compiled graph via the gpui-ce path dependency: no new crate, and the rebuild was incremental (~10-14s, `-j 4`, crt-static). Artifact after the rebuild + measure: **revision 9, capability mask 0x3FF, 18,216,448 bytes, sha256 f10c79737a4f68f629efa70533b016ccdcfb03b919e0d8a2058b696d062fde8c, static CRT, OS-only imports**; the measure tool records the resvg 0.48.1 / usvg 0.48.1 lock resolution and the feature graph. The Go client (internal/native/svg.go + svg_windows.go: the typed error surface, the capacity protocol, the mode tags) gates through internal/native/svg_test.go (6 tests: the font surface, the parse/render lifecycle with the BGRA oracle, the alpha mask, the typed errors and handle lifecycle, the 8192 clamp, the bundled-font text render) with the CE corpus fonts committed to testdata.

**Go side (the wave-3 worker, session impl-19-svg-go, clean 70m completion; verified by the orchestrator):** gpui/svg.go — the SvgRenderer model (the process service, the three modes, the font orchestration: missing pinned assets → one shared asset load per path through ticket18's registry → AddFont inside the worker → each suspended parse resumes exactly once; concurrent consumers/windows share the load (counting-registry evidence); scope closure and App.Shutdown discard; a missing path is a typed SvgFontAssetMissingError, never a hang; no synchronous foreground callback into the native worker); gpui/svgelement.go — the Svg element (path/data, the transformation matrix with the pinned compose chain, PaintSvg: snap → RenderSvgParams bounds×2 ceil → the mask cache hit → the centering math → the tinted monochrome sprite; FetchSvgAsset/UseSvgAsset) and the SVG mask atlas identity (InsertSvgMask/RemoveSvgMask — the glyph-key AlphaMask ride with a bit-62 identity disjoint, documented: the frozen ABI has no AtlasKey::Svg variant, a recorded next-revision follow-up). internal/svgspec: 14 tests (the orchestration, the element tint/centering/transformation/registry/external/malformed/closure, the emoji presentation through a real render with the availability record, the three modes, the truncating-unpremultiply pixel oracle 254/255/128, the alpha mask, the resource retirement, the transformation matrix, the text model). All 14 + the 6 native gates + the rotated-artifact regression set (layoutspec/scenespec/glyphspec/textspec/imagespec/native) green; the full-suite gate carries only the documented environmental cursor class plus the provenance check this evidence file resolves. Deferred, recorded: the distinct AtlasKey::Svg variant (next approved native touchpoint, alongside fx-0008), full-color SVG painting through the img element (ticket17's ErrSVGRendererDeferred seam — the Svg element is alpha-mask-only as in the pin), the element's full interactivity (the input ticket). Executed evidence: [evidence/ticket19-svg-fonts.json](../../../evidence/ticket19-svg-fonts.json).
