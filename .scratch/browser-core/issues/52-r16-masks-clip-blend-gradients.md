# 52 - R16: masks, clip-path, blend modes, drop-shadow and gradients

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 39, 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: mask composite (edge fades, gradient and image masks, alpha and luminance), clip-path coverage, blend modes including plus-lighter, the drop-shadow filter, and n-stop, radial, conic and repeating gradients.

**Blocked by:** 39 (R15 native rebuild; R16 records extend it), 49 (R16 compositor design)

**Status:** ready-for-agent

**Design ([docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) §3.5-3.9, §7, §8.1; approved 2026-10-24):**
- Masks composite `dst_in` from a gradient, atlas-image or A8 coverage source, in alpha or luminance mode.
- Clip-path is rendered as a coverage mask built from the tessellated path.
- Blend groups always take the offscreen route and blend against a backdrop copy in the shader. Plus-lighter is premultiplied clamp(s+d). Blend groups have **no size cap** for now; they are measured at 54, and any later cap must not change pixels.
- Gradients use a scene-level gradient table plus premultiplied sRGB ramp rows. Allow **up to 64 stops**; above that, Go resamples to 64 and warns in debug builds. R15's 2-stop path is untouched.
- Drop-shadow is a filter-op chain appended to the R15 filter record. The new fields are append-only, and 0/0 means R15 semantics.

- [ ] Native spec tests for each primitive.
- [ ] R15 filter and gradient records keep their exact outputs; the 64-stop limit and the resampling above it are tested.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
Landed on `native/r16` (rev 16, scene service v8 / renderer v5, native revision 151 — 54 sets 16), in four commits (3ba4e83, 8f1a5b4, 78de4d7, dccfdbc).

**What landed**

- **The scene-level gradient table (§3.8):** `GpuiGoSceneGradientRecord` (56 bytes, the design's A.3: kind/tile/color-space + 8 geometry words + stop count) with the bulk stop array (position + straight RGBA8, 8 bytes per stop) through `scene_insert_gradient`/`scene_gradient_dump` (table slots 30/31, the batch op 14). The service bakes each gradient into a 1024×1 **premultiplied-sRGB ramp row** (deterministic f32 mixing of the fixed stops; the Go CPU references mirror the arithmetic exactly) and assigns `stop_row` = the gradient's own index. The stop cap is **64** (the user's 2026-10-24 decision; the design text said 32); above it **Go resamples to 64 evenly-spaced stops** (`gpui.ResampleTableGradientStops`, evaluating the original's interpolation) with a debug-build warning. The shader: `BackgroundTag::GradientTable = 4` (the table index in the record's ANGLE field) evaluated per pixel — linear (the projection onto the caller-resolved line), radial (the ellipse-normalized radius with the rx/ry aspect), conic (the clockwise-from-up angle swept over `rotation` radians), the tile wrap (pad/repeat/reflect), sampled from the ramp with linear filtering. Quads and shadows reference the table (the **R15 2-stop record and shader stay bit-exact**; tag 4 is rejected on paths — the pinned path-sprite pipeline). The gradient bindings (t7 table / t8 ramp / s9 clamp sampler) sit above every vendored module's own group-1 slots.
- **Blend modes (§3.7):** the effect group's `has_blend` composites with the **W3C compositing-1 §10 formulas** in the shader (all 17 modes: the separable set, hue/saturation/color/luminosity through Lum/ClipColor/SetLum/Sat/SetSat, plus-lighter = the **premultiplied clamp(source + backdrop)**, Skia kPlus). D3D11 has no programmable blending, so the composite samples a **backdrop copy** (a shader-pass 1:1 copy of the parent target into a per-surface backdrop texture) and writes with **no fixed blending** — outside the group's coverage the formula degenerates to exactly the backdrop, so untouched pixels round-trip. A blend group forces `uses_offscreen_target` (the plan flag) so the base target is a readable texture; there is **no size cap** (the design's §8.1 answer 6). One pipeline handles the axis-aligned and matrix cases (the transformed vertex shape, the identity matrix for the former).
- **The mask composite (§3.5):** `has_mask` composites `dst_in` — the b5 mask fields (kind/reference/mode appended after the matrix rows, the rev-15.1 prefix byte-identical) plus the mask source: **kind 1 the gradient ramp** (the table entry's parameter at the pixel, wrapped by the tile mode), **kind 2 the atlas texture** (the whole polychrome texture stretched over the group's bounds — the mask-image contract), **kind 3 the coverage target**. Mode 0 reads the sample's alpha, mode 1 its REC-601 luminance. The mask multiplies the premultiplied group colour in the composite fragments (the axis-aligned, smoothed, transformed and blend paths all evaluate it — the transformed/blend paths at the LOCAL position, CSS masks inside the element's space). **An unresolvable mask hides the group** (the kind-4 sentinel: the references resolve at render, so replayed ranges stay robust and nothing fails a frame).
- **Clip-path (§3.6):** `GpuiGoSceneCoverageRecord` (24 bytes, A.4) through `scene_insert_coverage` (slot 32, the batch op 15). The renderer rasterizes each referenced path (the tessellated triangles, a solid white fill through the pinned path-rasterization pipeline) into a per-surface **A8 coverage target** (the MSAA resolve the pin's path machinery already runs), once per frame before the command walk; the coverage masks sample it.
- **Drop-shadow (§3.9):** `GpuiGoSceneFilterOpRecord` (40 bytes, A.5: offset, the CSS blur radius, the color) through `scene_insert_filter_op_chain` (slot 33, the batch op 16); the R15 filter record appends `op_chain_index`/`op_chain_count` after `record_size` (820→828 bytes; **0/0 = R15 semantics, bit-exact** — the R15 blur/matrix payloads untouched). The renderer draws each op BENEATH the content: the group's blurred **alpha silhouette** (the op's own gaussian chain at σ = radius/2, Chromium's BlurAsSigma rule; a zero radius samples the raw bucket), tinted with the op's color, offset, composited premultiplied. The R15 early-out (nothing to draw) now fires only without ops.
- **51's handed-off gap closed:** the transformed composite's rounded clip carries the record's **corner smoothing** (the prepared corner reaches/factors as flat varyings, the smoothed SDF when smoothing > 0 — the plain rrect stays bit-exact at 0; the blend composite carries it too). The content mask's inline rounded clip (`ContentMask::alpha`) also gained the smoothed SDF from the mask's own smoothing field.

**Record/ABI changes:** scene service **v8** (table slots 30-33, 34 pointers, 320→376 bytes, five new self-check scalars); renderer v5 (no table change). Records: filter 820→828 (the appended op-chain words), meta 60→72 (gradient/coverage/chain counts appended after record_size); new: gradient 56, stop 8, coverage 24, filter op 40. The `insert_batch` ops 14/15/16. The composite extension cbuffer b5 appends the mask/blend/tint/backdrop fields (704→752 bytes, the prefix byte-identical); the vendored pipelines gain the gradient (t7/t8/s9) and mask/backdrop (t10/t11) bindings above every module's own slots.

**Tests and results** (with `GPUI_GO_NATIVE_BUNDLE` on the interim bundle, DLL SHA-256 `a9c0fa25ab3ee1a956a38ab316d21319c0b61557c02254a64615d6ab292757ae`; the embedded artifact untouched — the manifest swap reverted before every commit):
- scenespec `r52_gradients_test.go`: the insert/dump round-trip (stop_row 1:1, the meta counts), the 64-stop cap (64 ok, 65 rejected), the validation (kind/tile/Oklab/degenerate geometry/stop ordering/position range), the coverage and op-chain inserts + counts, the effect mask kinds, the blend group's forced offscreen route, the fold skip for table-gradient quads, the tag-4 background validation.
- rendererspec `r52_gpu_test.go` + `r52_masks_gpu_test.go`, every pixel gate against a CPU reference that mirrors the native bake and the hardware's linear filtering: **each gradient kind** (a 3-stop linear, a 2-stop radial, a 4-stop conic, repeating and reflect tiles), **the >64-stop resampling**, **every blend mode** (all 17 against the W3C formula; the backdrop untouched outside the group), **alpha vs luminance masks** (the gradient ramp and the atlas image), **coverage** (the rasterized path clip), **the invalid-mask hide**, **drop-shadow** (the zero-blur silhouette exact; the blurred falloff monotone with a full-strength interior), and **the rotated smoothed clip**.
- The R15 gates: the 2-stop gradient and filter records keep their exact outputs.
- The final full suite with the bundle + manifest swap, `go test -p 1 -count=1` over internal/native/…, scenespec, rendererspec, conformance/…, portfixture, parity/…, gpui, glyphspec, textspec, layoutspec, fontspec, textframespec: **all ok except** the 7 internal/native loader tests that fail by construction under the swap (expected until 54) and portfixture TestFx0004 (the KNOWN pre-existing font count) — every other package green, including all 42 parity fixture packages (the R15 filters/shadow/gradient fixtures bit-exact), conformance, gpui and the text/font/layout spec packages.
- `cargo test --release -p gpui-go-native --lib`: **117 passed, 2 failed** — both pre-existing (`layout::style_translation`, `text::font_add`).

**Deviations from the design**

1. **The atlas mask's reference is the polychrome texture index, not the "atlas tile id"** (A.1's wording). The atlas service has no tile-id → texture lookup (its queries are by content key, and etagere tile ids collide across textures), and the effect record carries one u32. The landed semantics: `mask_reference` = the polychrome texture index; the whole texture stretches over the group's bounds (the mask-image contract). 55's styling can allocate dedicated mask textures or 54/55 can extend the atlas with a tile lookup; the render path and the ABI shape support both.
2. **The gradient-table arm's stop limit is 64, not the design text's 32** — the user's §8.1 answer 4 ("raise the ramp-path cap to 64; above that, resample down to 64 stops in Go, warning in debug"), which this ticket's instructions confirmed.
3. **No pixel-level gate for a SMOOTHED content mask** (the mask's own corner smoothing): the ABI record and the shader carry it and the smoothing-0 path is pixel-identical (every fixture), but the Go paint surface can't author a quad mask with radii yet (gpui.Quad's ContentMask is a plain Bounds; the styling is 55). The transform composite's smoothed clip — 51's gap — has its gate.
4. **The gradient table is not dithered** (the R15 two-stop gradient is): the 1024-texel ramp's quantization is far below the two-stop path's banding (the dither's reason), and the spec tests compare the exact filtered bytes. The R15 path's dither is untouched.
5. **A coverage mask on a TRANSFORM group samples the window-frame rasterization**: the coverage rasterizes once per frame in the base frame (the common case; a transform group's local-frame mask would need per-frame rasterization in the group's bucket — recorded for 54).

**Handed to 53/54/55**

- **53**: the damage entry can consume the blend/mask groups' dilated bounds (a mask group's composite region = its bounds; blend groups force the offscreen route — the damage computation should mirror both); the region-limited blur and the state-bind/tess-cache items are untouched.
- **54**: the build measure/pins; the remaining 50 hand-off (the pinned underline/sprite/path pipelines' rounded-mask gap — they clip rect+fade only, vendoring those pipelines is the fix; recorded in shaders/mod.rs divergence 4); the smoothed-content-mask pixel gate once 55 authors it.
- **55**: the Go styling — `mask-image` (the gradient/atlas mask kinds), `clip-path` (the coverage entries), `mix-blend-mode`/`isolation` (the blend groups), the gradient classes (bg-linear/radial/conic with >2 stops through `InsertGradient` + `TableGradientBackground`), and `filter: drop-shadow()` (the op chain + the FilterChain fields). The CSS stop fixup and the endpoint/shape math (EndPointsFromAngle, the ellipse radii, the conic angles) are the caller's — the geometry words are the resolved device-pixel values.

**Pre-existing failures seen during this run** (not investigated, per instructions): portfixture fx-0004 (font count), the two cargo-test layout/text failures, the 7 internal/native loader tests that fail by construction under the bundle/manifest swap.
