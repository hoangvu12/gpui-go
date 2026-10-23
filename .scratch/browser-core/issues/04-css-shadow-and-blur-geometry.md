# 04 - CSS shadow and blur geometry

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Box shadows and blur filters look like Chrome's: shadow blur uses sigma = blur/2, filter and backdrop-filter blur use sigma = radius, spread grows the corner radii, the first shadow in the list paints on top, and shadows and the ring paint inside the filter group (D8; pin values verified in the spec).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Harness fixtures for: a shadow with blur, a shadow with spread on a rounded box, a multi-shadow list, filter: blur, backdrop-filter: blur.
- [ ] Each fixture matches Chrome within the pixel tolerance. (4 of 5: shadow-blur, shadow-spread, shadow-order and shadow-filter all pass; shadow-backdrop is blocked on a native rev-15 backdrop-blur bug this ticket reports — see the Answer and the Comments.)
- [x] Changed bit-exact fixtures carry a 'divergence toward CSS' note.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, worker (ticket 04): **spec premise correction, measured.** D8's note "the pin uses σ = blur and σ = radius/2" is half right. The shadow half is right (the pinned shadow shader uses the record's blur_radius as σ directly in device pixels, `shadows.rs` gaussian(sample_position, blur_radius)). The filter half is wrong in effective device pixels: the pin's `RADIUS_TO_STANDARD_DEVIATION = 0.5` (blur.rs:17) computes σ in TEXELS of the HALF-RESOLUTION blur texture (the blur passes run on the downsampled ping/pong textures, draw.rs), so the effective device-pixel σ IS the record radius = the CSS radius × scale — the pin's filter and backdrop-filter blur were already at the CSS σ. The shadow-filter fixture measured the pin value at 0.42 mean channel diff against Chrome vs 9.73 for the doubled radius. Consequence: NO divergence or Go change was needed for filter/backdrop blur radii; the doubling the report's Q2 suggested is a measurement-verified no-op-to-wrong. The record-level tests pin the values.
- 2026-10-24, worker (ticket 04): **native blocker reported (rev 15).** The rev-15 backdrop blur (ticket 39's mirror crop) is broken natively: (a) a partial-region backdrop renders its blurred input ZOOMED — `dx_blur_and_composite` builds `BlurUniforms::composite(..., blur_size, source_size)` with source_size = the crop's total, but the blurred result lives in the WINDOW-SIZED ping texture, so the composite's `uv = (position − sample_offset)/crop.total` samples the window-sized texture with crop-relative coordinates; the content-filter path passes the full window dims and works. (b) even a full-viewport backdrop (crop region = window, margin 0) shows an un-blurred, neighbor-bar-averaged input (measured: every tested radius except one produces a constant 50/50 neighbor average across each bar). Native code and the DLL are out of this ticket's scope; the shadow-backdrop fixture, its Chrome record and its view are checked in, its pixel comparison is skipped with this reason (shadow_test.go), and it must be re-registered in shadow.go's init once the native fix lands.

## Answer

**Implemented on `core/04-shadow-blur` (headless only; the four pixel fixtures recorded with Chrome 155.0.8059.85, test-first: each failed before the fixes below).**

### What changed (with file:line)

1. **Shadow blur σ = CSS blur / 2** (css-backgrounds-3 §shadow-blur, "the blur is a Gaussian whose σ is half the blur radius"; Chromium `ShadowData::BlurRadiusToStdDev`/`BlurAsSigma`, `core/style/shadow_data.h:76-88` @ 0d4dcd4f — "Per spec, sigma is exactly half the blur radius" — fed to Skia's blur mask filter whose sigma IS the standard deviation, `core/paint/box_painter_base.cc:339` + `cc/paint/draw_looper.cc:28-30` + skia `include/core/SkMaskFilter.h:33-37`; the pinned shadow shader uses the record's blur_radius as σ in device pixels). Recorded divergence toward CSS (the pin passed the blur through as σ, twice as blurry):
   - `gpui/scene.go` `Scene.PaintBoxShadow` (new, `gpui/scene.go:593-611`): the Style paint path's shadow seam — σ = blur/2 (the record's blur is the shader's σ, so the CSS radius is halved at the record boundary) plus the spread-grown radii (below). The pin's `Scene.PaintShadow` (unchanged, `gpui/scene.go:553-568`) keeps the pinned record for the pin-conformance fixtures (fx-0003 stays bit-exact).
   - `gpui/paint.go` `dropShadowFor` (`gpui/paint.go:578-591`): grows a `shapeRadii` parameter (the pinned caller passes the element radii; PaintBoxShadow passes the CSS spread-adjusted radii).
   - `gpui/div.go` paintBox: the drop loop (`gpui/div.go:1944-1959`), the inset ring (`gpui/div.go:1995`) and the inset loop (`gpui/div.go:1997-2004`) call `PaintBoxShadow`.
   - Measured: the shadow-blur fixture's correct view compares at **0.029 mean channel diff image-wide / 0.104 in the ink-covering ROI / 0.0000 hot** against Chrome; the deliberately-wrong σ = blur view measures 1.073 / 3.616.

2. **Spread grows the shadow shape's corner radii** (css-backgrounds-3 §corner-shaping, the outset-adjusted border radius, including the ratio rule for small radii; Chromium `AdjustedRadiusDimension` + `ComputeOutsetAdjustedBorderRadius`, `platform/geometry/float_rounded_rect.cc:131-186`, `FloatRoundedRect::OutsetWithCornerCorrection` `:209-219`, `ConstrainRadii` `:289-305`, applied by `ApplySpreadToShadowShape`, `core/paint/box_painter_base.cc:90-95`; both files are in the blink core build.gni source lists, compiled on Windows). Recorded divergence toward CSS (the pin kept the element's radii under spread, so a `0 0 0 4px` halo on an r=8 box had 8px corners where CSS gives 12):
   - `gpui/style.go` `Corners.OutsetWithCornerCorrection` (new, `gpui/style.go:1081-1108`): each radius grows by the outset; when the radius is smaller than the outset it grows by the cubic ratio rule `r + s·(1 − (1−r/s)³·(1−coverage³))` with coverage = 2·min(r/w, r/h); zero radii stay zero; a negative outset (inset shadows) shrinks, clamped at zero.
   - `gpui/scene.go` `PaintBoxShadow` applies it plus `ConstrainRadiiForQuadSize` on the dilated box.
   - Measured: the shadow-spread fixture (a 12px-radius box with an 8px halo → CSS 20px corners, and a 4px-radius box with a 10px halo → the ratio rule's ≈11.84px) compares at **0.002 / 0.011-0.017 / 0.0000**; the pin-radii wrong view (painted through `Window.PaintDropShadows`) measures 0.089 / 0.632-0.655 / 0.0101.

3. **The first shadow in the list paints on top** (css-backgrounds-3 §box-shadow; Chromium shadow_list.h:46-47 "the first-specified shadow is painted on top", `box_painter_base.cc:295` and `:416` iterate the list backwards). Recorded divergence toward CSS (the pin painted in list order, so its last shadow was on top):
   - `gpui/div.go` paintBox: the drop loop (`gpui/div.go:1944-1959`) and the inset loop (`gpui/div.go:1997-2004`) iterate in reverse.
   - Measured: the shadow-order fixture (a red halo first, a blue offset shadow second, overlapping at the bottom-right) compares **pixel-exactly (0.0000)**; the reversed-list wrong view measures ROI 2.611 / hot 0.0746.

4. **Shadows and the focus ring paint inside the filter group** (filter-effects-1 §FilterProperty: the filter applies to the whole rendered element, including its box-shadow; Chromium paints the decorations inside the filter effect node — the filter's reference box is the layer's visual overflow, `PaintLayer::LocalBoundingBox` = `VisualOverflowRect`, `core/paint/paint_layer.cc:2199-2205`, from `ComputedStyle::BoxDecorationOutsets`, `core/style/computed_style.cc:2697-2709`, i.e. `ShadowList::RectOutsetsIncludingOriginal`, `core/style/shadow_list.cc:38-47`; the pin painted the shadows and the ring above `with_filter_layer`, style.rs:918-930). Recorded divergence toward CSS:
   - `gpui/div.go` paintBox: the drop shadows and the outer ring moved inside `paintUnit` (the filter group), `gpui/div.go:1917-1967`; the backdrop-filter loop stays before the group (Chromium: "the backdrop-filter node comes first", `core/paint/paint_layer.cc:2629-2638`).
   - The group's bounds are the element's INK bounds (the border box dilated by the shadows' and the ring's extent — Chromium's filter reference box), so the composite covers the whole blurred shadow instead of cutting it at the element bounds + 3σ: `gpui/paint.go` `BoxShadow.inkOutsets` (new, `gpui/paint.go:311-332`, Chromium `ShadowData::RectOutsets`, `core/style/shadow_data.cc:33-43`: ceil(3σ) + spread ∓ offset per side) and the dilation at `gpui/div.go:2058-2080`.
   - Measured: the shadow-filter fixture (a bordered card with a swatch, a shadow and `filter: blur(3px)`) compares at **0.063 / 0.422 (box) / 0.285 (card) / 0.0000 hot**; the shadow-outside-group wrong view (the pin's structure, the shadow on an unfiltered wrapper) measures 0.165 / 1.384 / hot 0.0064.

5. **Filter and backdrop-filter blur σ = the CSS radius — NO change needed** (filter-effects-1 §funcdef-filter-blur: the length is the standard deviation; Chromium `FEGaussianBlur(std_deviation = the raw length)`, `core/paint/filter_effect_builder.cc:284-291`, and the compositor path passes the amount to Skia as the sigma, `cc/paint/render_surface_filters.cc:193-212`; the software path `BlurPaintFilter(std_x, std_y)`, `platform/graphics/filters/fe_gaussian_blur.cc:79-88`). The pin's kernel computes σ = 0.5 × the record radius in half-resolution TEXELS (`gpui_render/src/blur.rs:17`, the blur passes' viewport and texture sizes are `downsampled_dimension`), so the effective device-pixel σ IS the record radius = the CSS radius × scale. Measured against Chrome: the pin value 0.42 mean channel diff, the doubled radius 9.73 — the report's Q2 suggestion ("pass 2× radius") is wrong, and the spec's D8 pin-note ("the pin uses σ = radius/2") read the texel conversion without the half-resolution factor. The Go side passes the CSS radius through unchanged (`gpui/div.go:1886-1910` backdrop, `gpui/div.go:2086-2104` filter chain) and the record-level tests pin it: `gpui/css_shadow_test.go` `TestFilterBlurSigmaIsTheCSSRadius` and `TestBackdropFilterBlurSigmaIsTheCSSRadius`.

### Fixtures (new package `internal/parity/fixtures/shadow`, registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`; records in `internal/parity/testdata/shadow-*`, recorded with Chrome 155.0.8059.85 headless)

| fixture | what it pins | measured correct view (whole / worst ROI / hot) | tolerance | wrong view (fails it) |
|---|---|---|---|---|
| shadow-blur | σ = blur/2 | 0.029 / 0.104 / 0.0000 | 0.06 / 0.25 / 0.002 | σ = blur: 1.073 / 3.616 |
| shadow-spread | spread-grown radii, the ratio rule | 0.002 / 0.017 / 0.0000 | 0.01 / 0.05 / 0.002 | pin radii: 0.089 / 0.655 / 0.0101 |
| shadow-order | first shadow on top | 0.000 / 0.000 / 0.0000 | 0.02 / 0.05 / 0.002 | reversed: 2.611 ROI / 0.0746 hot |
| shadow-filter | the group covers the shadow; the CSS σ | 0.063 / 0.422 / 0.0000 | 0.15 / 0.9 / 0.004 | σ = radius/2: 0.698 / 6.930 / 0.0502; shadow outside the group: 0.165 / 1.384 / 0.0064 |
| shadow-backdrop | backdrop-filter blur σ | BLOCKED on native (see the Comments) | placeholders | — |

All five pages are text-free (no font rasterization noise); the ROIs are wrapper elements sized to cover each shadow's ink extent (shadows paint outside the element box). The discrimination tests (`internal/parity/fixtures/shadow/shadow_test.go`) assert each wrong view FAILS Tier C with a readable diff while Tier A (boxes) still passes.

### Changed existing tests (divergence notes)

- `gpui/styled_test.go` `TestDivPaintsTheBoxInPinnedOrder` (`gpui/styled_test.go:326-336`): the drop shadow's record blur expectation changed from `4*testScale` (the pin: σ = the blur radius) to `4*testScale/2` (CSS: σ = blur/2), with the divergence note in the comment. The paint order the test pins is unchanged.
- `gpui/styled_test.go` `TestDebugOutlineAndFilters`: the filter/backdrop blur expectations stay at the pin values (radius × scale) — corrected the comment to the half-res-kernel analysis (no divergence; the pin value is the CSS σ).
- No bit-exact RECORD changed: fx-0003 (scene painting, the portfixture oracle) still passes bit-exactly — the CSS conversions live at the Style paint path, and the pinned `Scene.PaintShadow`/`WithFilterLayer`/`PaintBackdrop` record construction is untouched. scenespec, capturespec and the portfixture suite are green (fx-0004's font-count failure is pre-existing on main, unrelated).

### Citations (Chromium 0d4dcd4f, `%TEMP%\research-src\chromium`; all files are in the blink core build.gni source lists, i.e. compiled on Windows)

- shadow σ: `core/style/shadow_data.h:76-88` ("Per spec, sigma is exactly half the blur radius"), `core/paint/box_painter_base.cc:339`, `cc/paint/draw_looper.cc:28-30`, skia `include/core/SkMaskFilter.h:33-37`.
- spread radii: `platform/geometry/float_rounded_rect.cc:131-186, 209-219, 289-305`, `core/paint/box_painter_base.cc:90-95, 366`.
- shadow order: `core/style/shadow_list.h:46-47`, `core/paint/box_painter_base.cc:295, 416`.
- filter in the group: `core/paint/paint_layer.cc:2199-2205, 2273-2278` (the filter reference box = the visual overflow), `core/style/computed_style.cc:2697-2709`, `core/style/shadow_list.cc:38-47`, `core/style/shadow_data.cc:33-43`.
- backdrop before the group: `core/paint/paint_layer.cc:2629-2638`.
- filter σ: `core/paint/filter_effect_builder.cc:284-291, 473-477`, `cc/paint/render_surface_filters.cc:193-212`, `platform/graphics/filters/fe_gaussian_blur.cc:79-88`.
- The pin's half-res blur: `reference/ce-source/crates/gpui_render/src/blur.rs:17, 50-65` (σ = 0.5 × radius in texels) + the renderer's downsampled blur passes (`reference/native/src/renderer/draw.rs:2471-2590`, `reference/ce-source/crates/gpui_windows/src/directx_renderer.rs`).

### Not done / open

- **shadow-backdrop's pixel comparison is blocked on the native rev-15 backdrop blur** (the Comments describe the two measured failures precisely, with the one-line fix location for a follow-up native change: the composite's target size for the crop path). The fixture, its Chrome record and its view are checked in; `shadow_test.go` skips its Tier C with that reason and its Tier A (boxes) passes. The σ conversion for backdrops is pinned at the record level instead.
- The pin's blur quality (a 2× downsample before every blur, and the shadow shader's 4-sample analytic integral vs Skia's three-box-blur) is inside the fixtures' tolerances; the report's σ-related "related blur-quality gaps" (full-res blurring up to σ=4, the 32-sample kernel cap) remain native concerns, unchanged by this ticket.
- The group's ink bounds cover the drop shadows and the outer ring only; a filtered element whose CHILDREN overflow its bounds still composites at the pin's element-bounds-plus-3σ (Chromium's filter reference box includes descendant ink) — a residual the R16 compositor work can revisit.
