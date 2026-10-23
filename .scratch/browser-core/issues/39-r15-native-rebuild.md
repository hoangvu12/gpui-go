# 39 - R15 native rebuild

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat 9044df02
Blocked by: none
Triage: ready-for-agent

**What to build:** One native rebuild (approved as rev 15) delivers: the Windows bold rule (weight >= 600 over a face < 600), drop shadows clipped out of the border box, a gradient record (two stops, CSS interpolation), colour-matrix filters, the backdrop mirror crop, a point sampler, new text-run fields (word-break, overflow-wrap, text-wrap, locale, variations, width, optical size, justify) and new layout fields (baseline in measure, variable grid tracks, justify-items/self, flow-root). Built and measured like rev 13 and rev 14.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] The bold weight-matrix test passes with Chrome's Windows rule (the 600/400, 700/500 and 900/600 cells updated).
- [x] Native spec packages cover each new record and field; unchanged bit-exact fixtures stay unchanged.
- [x] Revision and artifact pins, measurements and evidence are updated in every place rev 14 touched.
- [x] Headless runs only; no visible windows (the GPU gates open non-activating windows, the established scenespec practice).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified on core/39-r15 before merge. The embedded DLL hashes to the pinned 8b944eae…. I rebuilt it from the committed source: same size (19,104,256 bytes), and only 23 bytes differ (PE and debug timestamps plus the PDB id), so the code is identical. `go test -p 1` passes for fontspec, glyphspec, scenespec, textspec, layoutspec, consumerspec, native, gpui and base. The drop-shadow GPU gate opened an on-screen window; I parked it at 20000,20000 (it still reads 127/127 at the centre, 3 runs out of 3). The Chromium bold-rule citations were checked by the worker and match ticket 13's method. GAP: the text-run fields (word-break, overflow-wrap, text-wrap, locale, variations, width, optical size, justify) and the measure baseline cross the ABI but do not drive shaping or alignment (the pinned Parley/Taffy APIs give no slot). Tickets 42 and 43 need a follow-up native change for them; raised with the user.

## Answer

Done as **native revision 15**: SHA-256 `8b944eae962a75e693ce3fdc8c97c274ad5b8e80c5e1487fd2fd038ebc10c40a`, 19,104,256 bytes, static CRT, mask `0xFFF`. Evidence: [`evidence/browser-core/ticket39-native-rebuild.json`](../../../evidence/browser-core/ticket39-native-rebuild.json). Five commits on `core/39-r15` (bold rule, scene records, text run fields, layout fields, pins/evidence).

### 1. The Windows bold rule (done)

`effective_weight` keeps Fontique's synthesis only when the request is **at least 600 AND the matched static face is below 600** — `font_cache_skia_win.cc:484-486` (Chromium 0d4dcd4f), the file Windows actually compiles (`platform/BUILD.gn:1810-1813`), with `kBoldThreshold=600` (`font_selection_types.h:183`) and `SkTypeface::isBold()` = weight ≥ 600 (skia `src/core/SkTypeface.cpp:491-493`, `kSemiBold_Weight` at `include/core/SkFontStyle.h:25`). The `>200` rule rev 14 followed sits inside `#if !BUILDFLAG(IS_WIN)` (`font_cache_skia.cc:312`) and never ran on Windows. Changed cells: 600/400 and 700/500 synthesize, 900/600 does not, 700/600 (zterm) stays plain. The weight-matrix test gained the Wt450 (400/500) family for the 700/500 cell.

### 2. The scene records (done)

- **Gradient record**: `GpuiGoSceneBackground` (72 bytes — tag, color space, solid, angle, two stops with CSS positions) replaces the color in quad background/border, shadow and path records; the kernel builds the pinned `Background` through `linear_gradient(...).color_space(Srgb)`.
- **Colour-matrix filters**: the filter record grows per-filter tags plus one composed row-major 4×5 matrix (188 bytes); the composite applies it to non-premultiplied RGBA (a per-pixel linear map commutes with the uniform-per-channel gaussian, so blur-then-matrix preserves the CSS chain order).
- **Point sampler**: a `pixelated` flag on the sprite records; the plan splits sprite batches on it and the draw binds a `MIN_MAG_MIP_POINT` sampler.
- **Drop-shadow clip-out** and **the CSS gradient geometry** (the line length `|W·sinθ|+|H·cosθ|`, premultiplied sRGB mixing) are shader behaviors the pinned blobs cannot express, so this rebuild vendors a PATCHED copy of the pinned shader modules (`native/shaders/`, compiled by `native/build.rs` through the same wgsl-rs → Naga 24 → HLSL sm 5.0 → D3DCompile chain; the frozen `ce-source` is untouched — the recorded divergences live in `shaders/mod.rs`). **GPU gate**: a translucent panel over a hard red drop shadow reads 127/127 at its center on rev 15 (rev 14 read the shadow's red through the background).
- **Backdrop mirror crop**: the backdrop-filter input is cropped to the element's pixel-covered bounds with mirrored margins (Chromium `SkTileMode::kMirror`, `viz skia_renderer.cc:3254`) through a vendored `backdrop_crop` pass; the blur then reads the crop, not the whole target.

### 3. The text run fields (done — interface side)

The run record grew to 92 bytes (word-break, overflow-wrap, text-wrap, locale, font width, variation settings, optical size, justify); `text_shape` gained the variation array; a new `text_run_info` slot echoes the stored records (table 224 bytes, 17 pointers). **Recorded limit**: the pinned `TextLayoutRequest`/`TextRun` has no slots for these fields and `ParleyTextSystem` is fully encapsulated (its Parley state is private), so shaping behavior is unchanged for them — the fields validate and round-trip, which is the interface the Go styling tickets consume (see TEXT_ABI.md).

### 4. The layout fields (done)

The measure response carries a first baseline (20 bytes), stashed per measured node and echoed through the layout record (80 bytes). **Recorded limit**: Taffy 0.13's stored `Layout` drops baselines and its leaf compute hardcodes `Point::NONE` (`compute/leaf.rs:157`), and the `TaffyView` is `pub(crate)`, so the algorithms keep the pinned bottom-edge fallback (LAYOUT_ABI.md). The layout table gained `layout_node_set_grid_tracks` (variable-length track lists, each track a minmax pair: length/min-content/max-content/auto/fr; 168 bytes with 15 pointers); the style record grew justify-items/justify-self (352 bytes) and display tag 4 = `flow-root` (Taffy's `Display::FlowRoot`).

### Verification

- `go vet ./...` clean; `gofmt` clean on `gpui/` + `internal/`.
- `go test -p 1 ./... -count=1`: **51 packages pass**; the one failure is `internal/portfixture` `TestFx0004TextGeometryMatchesRecordedReference`, which reports `fontFamilyCount` 94 (the 2026-10-05 recording) vs 95 (this machine now) — **proven pre-existing**: the same failure reproduces on the pristine base commit `d1f2e72` with the rev-14 DLL (a font family was installed on this PC since the trace was recorded; nothing in this rebuild touches catalog enumeration). Re-record fx-0004 with the oracle harness to refresh it (a maintainer action, not done here).
- `cargo test -p gpui-go-native` still fails to compile the lib test target with the same **5 pre-existing** a11y/capture errors (`a11y.rs:1361/1367/1385`, `capture.rs:1510/1531`) and nothing new — the rev-15 record changes' test constructors were fixed.
- No bit-exact fixture changed: fx-0001/fx-0002 (layout), fx-0005 (the raster oracle), the scene dumps and the glyphspec bit-exact gates pass unchanged.
- The new gates: fontspec `TestBoldSynthesisFollowsBrowserRule` (rev 15 green, rev 14 red), scenespec `TestGradientBackground*`, `TestColorMatrixFilter*`, `TestPixelatedSpriteFlagSplitsBatches`, `TestDropShadowClippedOutOfBorderBoxOnRealGPU`, textspec `TestBrowserRunFields*`, layoutspec `TestFlowRootDisplayLaysOut`, `TestGridTrackListLaysOut`, `TestJustifyItemsAndSelfAreAccepted`, `TestMeasureResponseBaselineEchoes`.

### Divergences (recorded)

All toward CSS, per D0: the Windows bold rule (fontspec matrix); the drop-shadow border-box clip-out; the CSS gradient line length and premultiplied sRGB mixing; the backdrop mirror crop. Each is documented at its site (`shaders/mod.rs`, TEXT_ABI.md, LAYOUT_ABI.md, SCENE_ABI.md) with the Chromium citations. The vendored shader tree itself is the one structural divergence: the pinned `ce-source` stays unmodified, and the patched pipelines are byte-copies plus the three recorded patches.

### Not done / open

- The gradient and colour-matrix GPU output has record-level and pipeline-level coverage but no pixel-by-pixel Chrome comparison (the Go styling tickets that author gradients/filters will add browser-parity harness fixtures).
- The text run fields and the measure baseline cross the ABI but do not yet drive behavior (the exact upstream blockers are recorded in the ABI docs).
