# 41 - Colour filters, object-position and pixelated images

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code)
Blocked by: 39
Triage: ready-for-agent

**What to build:** CSS colour filters (grayscale, sepia, brightness, contrast, saturate, invert, hue-rotate), object-position and image-rendering: pixelated work from styles.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [x] Fixtures for each filter and for object-position match Chrome. (Recorded and checked in; object-position and pixelated match Chrome end-to-end. The three colour-filter fixtures' Tier C pixel comparisons are skipped behind a documented native blocker — rev 15 never applies the composed colour matrix on GPU, see the GAP below; their Chrome records, views and Tier A box comparisons are in and waiting for the native fix.)
- [x] A pixel-art image upscaled with pixelated shows hard edges like Chrome. (filters-pixelated: the point-sampled cell measures 0.0000 mean / 0.0000 hot against Chrome — pixel-exact.)

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, worker (core/41-filters): **GAP — native rev 15 never applies the composed colour-matrix payload on GPU, so the three colour-filter fixtures cannot match Chrome yet.** Root cause found and verified on this branch: `reference/native/src/renderer/draw.rs:2327` (and `:2330`, the backdrop-crop uniform) calls `update_buffer(ctx, &buffer, &[*uniforms])` where `*uniforms` is a `&&CompositeExtensionsUniforms`, so the slice's element type is the *reference* (8 bytes) — only the pointer value is copied into the 112-byte `COLOR_MATRIX_LOCALS` cbuffer, the shader's `enabled` (offset 88) reads zero, and every colour function is a GPU no-op. Diagnosis (nothing committed — the native tree and the DLL 8b944eae are untouched, hash re-verified): the scene records carry the matrix and the plan compiles one isolated group per chain (gpui-side tests green); the vendored composite shader's DXBC in the DLL disassembles with the matrix math and the `movc` on `cb5[5].z` present; a local instrumented rebuild proved the shader runs and cb5 binds (an opacity probe through cb2 works; a marker colour written through a correctly-typed `&[**uniforms]` update appears on the real GPU, and grayscale(1) on a red panel then reads (54,54,54)). The fix is native — two dereferences plus an artifact rebuild — and needs the native-rebuild approval, so this ticket ships the complete Go styling side, the recorded fixtures and the skip flags; **removing `SkipPixels` from the three filter fixtures (and unskipping their discriminator tests) is the acceptance for that rebuild**. The same bug disables rev 15's backdrop mirror-crop uniform.
- 2026-10-24, orchestrator 7: verified and merged (2d616a4). The native bug is confirmed in the source: `match &extras` binds `uniforms: &&T`, so `&[*uniforms]` is `[&T]` and update_buffer (draw.rs:1254-1267, copy_nonoverlapping of data.len() elements) copies 8 pointer bytes. Both the colour-matrix (draw.rs:2327) and the backdrop-crop (:2330) uploads are hit; on the rev 15.1 list. The DLL is untouched (sha256 8b944eae…). Merge with 04 and 40: div.go uses this ticket's one-group-per-chain record (WithFilterLayerChain, PaintBackdropFilterChain) with 04's ink-dilated group bounds and σ notes; class.go and tailwind.md keep both the gradient (40) and filter (41) stages. `go test ./gpui/ ./internal/parity/... ./internal/scenespec ./internal/capturespec` passes on the merged tree, and so does the full suite.

## Answer

**Implemented on `core/41-filters` (headless only; five new fixtures recorded with Chrome 155.0.8059.85; object-position and pixelated pass all three tiers; the three colour-filter fixtures pass Tier A and skip Tier C behind the documented native rev-15 blocker above).**

### The CSS colour filter functions (Go styling + scene payload)

- **Filter values**: `Filter` grew a CSS function discriminator and amount (the struct is `gpui/style.go:1076-1090`, the zero value stays `FilterFunctionBlur` so `Filter{BlurRadius: r}` literals and `Blur(r)` keep meaning blur; `IsIdentity` covers each function's no-op amount, `gpui/style.go:1098-1110`). Constructors: `Grayscale`, `Sepia`, `Saturate`, `HueRotate`, `Invert`, `OpacityFilter`, `Brightness`, `Contrast` (`gpui/filter_colors.go:78-138`), with Chromium's amount rules (grayscale/sepia/invert/opacity clamp to [0,1], saturate/brightness/contrast stay ≥0, hue-rotate is degrees — `filter_operation_resolver.cc:133-170`).
- **The matrices are Chromium's** (which follow the Filter Effects spec): grayscale/sepia from `GrayscaleMatrix`/`SepiaMatrix` (luminance weights 0.2126/0.7152/0.0722, `core/paint/filter_effect_builder.cc:66-127`); saturate/hue-rotate from `SaturateMatrix`/`HueRotateMatrix` (`platform/graphics/filters/fe_color_matrix.cc:58-96`, the spec's rounded 0.213/0.715/0.072); invert/opacity/brightness/contrast as the component transfers of `filter_effect_builder.cc:233-305` — invert's table [x, 1−x] is C' = (1−2x)·C + x (`fe_component_transfer.cc:49-62`), opacity's alpha table [0, x] is A' = x·A, brightness is linear slope x intercept 0, contrast linear slope x intercept −0.5x+0.5 (`fe_component_transfer.cc:64-73`). Colour space: sRGB — Chromium forces `kInterpolationSpaceSRGB` for the CSS shorthand functions (`filter_effect_builder.cc:361-366`), matching the native composite's non-premultiplied-sRGB application. Unit-tested value-for-value in `gpui/filter_colors_test.go` (`TestFilterColorMatrixValues`).
- **Chain order**: Chromium's `FilterEffectBuilder` chains each effect's input to the previous one, so the first listed function applies first (`filter_effect_builder.cc:326-373`); `composeColorMatrix` (`gpui/filter_colors.go:260-279`) composes in that order (first-first, out = A2·A1 with offset A2·b1+b2), and `composeFilterChain` (`:316-333`) collapses a whole chain into one `FilterChain` (`:293-313`): blurs composed by quadrature (sequential gaussians: σ² adds, exact for any radius/σ convention) and all matrix filters into one 4×5 payload — the rev-15 record's shape (SCENE_ABI.md, GpuiGoSceneFilterRecord). Matrix-vs-blur interleaving collapses the same way because a per-pixel linear map commutes with the uniform-per-channel gaussian (the record's recorded reasoning).
- **One group per chain** (was: one nested group per filter, which the depth cap of 2 silently degraded at three filters): `Style.paint` composes the chain and opens a single group (`gpui/div.go:1980-2007`); the backdrop chain likewise paints one record (`gpui/div.go:1899-1904`). The scene seam: `FilterBoundary`/`BackdropFilter` carry the matrix payload (`gpui/scene.go:124-175`), `Scene.BeginFilterGroupChain/EndFilterGroupChain/WithFilterLayerChain/PaintBackdropFilterChain` (`gpui/scene.go:642-683`, `gpui/filters.go:50-84`), `filterRecordOf` packs the tags/count/matrix (`gpui/scene_kernel.go:580-617`; blur tag first, one matrix tag, `color_matrix_count` 1). Round-trip and plan tests: `gpui/filter_scene_test.go` (the pair carries the composed matrix, the plan compiles one isolated group; a matrix-only chain still opens a group; an identity chain opens none). `Styled` methods appending like `Blur`: `gpui/styled_filters.go:17-73` (`.Grayscale(1).Blur(4)` = CSS `grayscale(1) blur(4)`).
- **The GAP above blocks the GPU application** (not the Go side): the fixtures carry `SkipPixels` with the blocker's file:line until the native rebuild.

### object-position and image-rendering (styles)

- `ObjectPosition`/`ObjectPositionAxis` (`gpui/object_position.go:22-52`): each axis is a fraction of the FREE space (content box minus the mode's fitted size) plus a pixel offset — exactly Chromium's `MinimumValueForLength(ObjectPosition().X(), base − object_size)` (`core/layout/layout_replaced.cc:469-472`, `ComputeObjectFitAndPositionRect` `:410-477`; the CSS default 50% 50% is `css_properties.json5:4693`). Keywords (`ObjectLeft/Center/Right/Top/Bottom`), percentages (`ObjectPercent`) and lengths (`ObjectPx`) plus the `right 10px` form (Fraction 1, OffsetPx −10). `ObjectFit.GetPositionedBounds` (`gpui/object_position.go:104-155`) places every mode's fitted size (the sizes are ticket 05's; `GetBounds` at `:157-160` stays the centered default); fill's percentage positions are no-ops with zero free space while lengths still shift — Chromium's non-default-position behaviour. Unit tests: `gpui/object_position_test.go` (all modes × keywords/percent/px, the fill quirk, degenerate inputs).
- **Style properties**: `Style.ObjectFit/ObjectPosition/ImageRendering` (`gpui/style.go:907-917`) with refinement fields (`gpui/styled_refinement.go:162-164`, refine/merge at `:305-307`/`:395-397`); CSS applies these to replaced elements only, so they are inert elsewhere. The img paint resolves style-over-element-option: fit from `style.ObjectFit` else the element's pinned option, position from `style.ObjectPosition` else the CSS default, sampler from `style.ImageRendering` (`gpui/img.go:585-611`). `Styled.ObjectPosition(x, y)` and `.ImageRendering(...)` (`gpui/styled_filters.go:77-104`); object-fit's whole vocabulary is the five class methods (below).
- **pixelated**: `ImageRendering` (`gpui/object_position.go:163-192`; pixelated and crisp-edges both point-sample — Chromium `GetInterpolationQuality`, `core/style/computed_style.cc:1326-1337`), `PolychromeSprite.Pixelated` (`gpui/draw_text.go:134`), inserted/dumped through the record (`gpui/draw_text.go:267-284, 340-365`), and `PaintImageWithCornerSmoothing(..., pixelated)` (`gpui/paintapi.go:234-237`).

### Tailwind classes

- **Colour filters** (a hand-written stage beside blur's, `gpui/class.go:1119-1213` `filterClass`, in `classStages` before `unsupportedClass`): `grayscale`, `grayscale-0`, `grayscale-[.5]/[50%]`; `sepia(…)`, `invert(…)`, `saturate-0…200` and `[1.25]`, `brightness-0…200` and `[1.75]`, `contrast-…`, `hue-rotate-0/15/30/60/90/180`, `-hue-rotate-90`, `hue-rotate-[90deg]`. Consecutive filter classes compose in class order — the CSS spelling (`.Grayscale(1).Blur(4)`), a superset of Tailwind's fixed CSS-variable order for the single-function cases. `backdrop-*` colour filters stay rejected with the existing reason (blurClass intercepts the prefix). The old "only blur filters are supported" entry now covers only drop-shadow/mix-blend/the bare `filter` class (`gpui/class.go:1122-1124`).
- **object-\* and rendering-\*** (generated method classes, regenerated `gpui/class_generated.go` — 415 in the switch now): `object-fill/contain/cover/none/scale-down` and the nine keyword positions `object-left-top` … `object-bottom` (methods in `gpui/styled_tailwind.go:182-269`), `rendering-pixelated`/`rendering-auto` (`:272-281`). The stale "object-fit is an image option" rejection is replaced with a pointer to the supported set (`gpui/class.go:1126`). Tests: `gpui/class_test.go` `TestClassFiltersAndObjectOptions`, `TestClassFiltersComposeInOrder`, `TestClassFilterErrors`.

### Fixtures (package `internal/parity/fixtures/filters`, registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`; records in `internal/parity/testdata/`, Chrome 155.0.8059.85, text-free pages; embedded PNGs — the 60×40 quadrant and the 8×8 pixel art — injected as `data:` URIs)

| fixture | what it pins | Tier C |
|---|---|---|
| filters-colors | one swatch per function (grayscale(1), sepia(1), invert(1), brightness(1.5), contrast(1.5), saturate(2), hue-rotate(90deg)) + control, five saturated stripes each | **skipped** (SkipPixels, the native blocker); Tier A green |
| filters-chain | sepia(1) hue-rotate(90deg) beside hue-rotate(90deg) sepia(1) (order-sensitive composition) + control | **skipped** (same); Tier A green |
| filters-image | grayscale(1) and hue-rotate(90deg) on img elements (the sprite path) + control | **skipped** (same); Tier A green |
| filters-objectposition | 12 frames: contain/none × (left top, right bottom, 25% 75%, 12px 8px) + cover×2 (style-side object-fit, the object-cover class path) + scale-down + fill×px | **passes** — measured whole 0.4882 / hot 0.0068, per frame ≤ 1.4933 mean / 0.01769 hot (cover-left-top); tolerance 1.0 / 3.0 / 0.04 (~2×); the centered wrong view measures 6.6664 whole, ROIs 2.80–17.29 — fails it |
| filters-pixelated | the 8×8 pixel art upscaled 12.5× (100×100, non-integer so bilinear blends): pixelated beside smooth | **passes** — the pixelated cell measures **0.0000 mean / 0.0000 hot (pixel-exact vs Chrome)**; the smooth control 6.4025 / 0.08687 (the pinned bilinear sampler vs Chrome's upsampling on the 1px diagonal pattern); tolerance 0.6 / 15.0 / 0.2; the smooth-instead-of-pixelated wrong view measures **41.7909 / 0.67450** in the pixelated cell — fails it |

Discriminator tests (`internal/parity/fixtures/filters/filters_test.go`): object-position-centered and pixelated-smooth fail Tier C with readable diffs while Tier A stays green; the three filter discriminators skip with the blocker reason (with the matrix a GPU no-op, wrong and correct views render identically — unskipping them is part of the native fix).

### Verification (commands and results)

- `go vet ./...` clean; `gofmt -l` clean on every touched file; `go build ./...` clean.
- `go test ./internal/parity/...` — ok (10 fixtures: 5 new; the 3 filter fixtures log the Tier C skip).
- `go test ./gpui/... ./base` — ok. `go test ./gpui/ -run 'TestClass|TestFilter|TestObject|TestImageRendering|TestCompose'` — the new unit suites pass.
- `go test ./internal/...` — all ok except `internal/portfixture TestFx0004TextGeometryMatchesRecordedReference` (font count 94 vs 95) — **pre-existing on main, unrelated, untouched** (the task noted it).
- The native tree, DLL and manifest are untouched (artifact hash re-verified `8b944eae…` after the diagnosis; the instrumented rebuilds lived only in a scratch target dir, since removed).

### Limitations

- **The native GAP above**: colour filters compose, record and plan correctly Go-side but paint as no-ops until the rev-15.1 cbuffer fix + rebuild; the three filter fixtures' Tier C comparisons and discriminators wait behind `SkipPixels` (their Chrome records are checked in).
- Blur radii keep the pin's σ convention (radius→σ = 0.5·radius); the CSS blur size fix is ticket 04's, untouched here — the chain composition (quadrature) is convention-independent either way.
- `drop-shadow()`, `mix-blend-mode` and backdrop colour filters remain unsupported (backdrop-* stays blur-only, the class mapper's recorded rejection).
- The pixelated flag is plumbed for polychrome sprites (raster images, the img path); SVG's monochrome sprites keep bilinear (out of this ticket's scope).
- Known residuals measured, not silently absorbed: image resampling (the linear sampler vs Chrome's upsampling) drives the object-position fixture's 0.49/1.49 stats and the pixelated fixture's smooth control 6.40 — the same class boxpaint-objectfit recorded; the image rectangles match to a pixel (the pixel-exact pixelated cell proves the geometry).
