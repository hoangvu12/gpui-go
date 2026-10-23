# 08 · Text rasterization and italic probes against Chrome (T6 + T7)

Ticket: `.scratch/browser-core/issues/08-rasterization-and-italic-probes.md` (spec T6, T7;
research [04 §2](../04-text-and-fonts.md) ranked this "the biggest visible difference but
needs a measured A/B before anyone touches the shader").
Measured 2026-10-24 on this PC: Chrome `Chrome/155.0.8059.85` (headless, recorded through
the harness), gpui-go native rev 14, ClearType **on** in the OS, DirectWrite rendering
params gamma **1.8** / grayscale enhanced contrast **0.5** (both read back through
`gpui.TextSystem.RasterBackend()`), and the ClearType-tuner registry key
`HKCU\SOFTWARE\Microsoft\Avalon.Graphics` present **without per-display subkeys**, so
Chrome keeps Skia's compile-time gamma/contrast (see §3).

Everything here is reproducible: `go test ./research/browser-parity/08-raster-probes/ -v`
replays the checked-in fixtures, re-measures and re-writes the captures. The regression
gates live in `internal/parity` (`TestFixtures`) as three new fixtures:
`textpaint-raster`, `textpaint-raster-gray`, `textpaint-italic`.

## The two answers

| Question | Answer | Key numbers |
|---|---|---|
| **R17: does the text gamma/contrast shader need porting to Skia's preblend?** | **NO — skip R17** | Mean ink (total text darkness) matches Chrome within **0.95–1.005** across dark/light/grey × 12/16/24/32 px in the like-with-like (ClearType) comparison, and within 0.90–1.00 in every other pairing; per-pixel text differences stay under the harness's "hot pixel" bar (whole-image hot fraction ≤ 0.0076); coverage histograms agree per bucket to ~0.003 of the cell. |
| **Does synthetic italic slant need a fix?** | **YES** | Chrome shears a family without an italic face by exactly **−0.250** (−14.03°, Skia's `setSkewX(-1/4)`); gpui-go's DirectWrite oblique simulation shears by **−0.340** (−18.77°) — **34% steeper**, 16.3 px vs 12.0 px of lean over the same stems. Root cause: `reference/native/src/glyph.rs` maps Fontique's italic synthesis onto `DWRITE_FONT_SIMULATIONS_OBLIQUE` instead of applying the shear Fontique computed (14°, which would match Chrome). |

---

## 1. What each side actually does (source trail)

**Chrome on Windows** (Chromium `0d4dcd4ffc1d`, sparse checkout
`%TEMP%\research-src\chromium`; Skia `e71ecd07`/`51c1eacc`, `%TEMP%\research-src\skia`):

- Gamma/contrast: `ui/gfx/font_render_params_win.cc:97-109` calls
  `FontUtilWin::GetGammaFromRegistry/GetContrastFromRegistry`;
  `ui/gfx/font_util_win.cc:23-49` (`GetTextParameters`) keeps **Skia's compile-time
  defaults** (`SK_GAMMA_EXPONENT`, `SK_GAMMA_CONTRAST`) unless the per-display key
  `HKCU\SOFTWARE\Microsoft\Avalon.Graphics\<primary display>` exists
  (`font_util_win.cc:54-71`), in which case DirectWrite's default rendering params are
  read. Skia's header defaults are gamma 0.0 (= sRGB) and contrast 0.5
  (`skia/include/core/SkTypes.h:86-94`); Chromium's Windows build overrides the contrast
  to **1.0** (report 04's [V-web] read of chromium's skia build config, and confirmed
  empirically here — §4.3). The values reach Skia through
  `skia::LegacyDisplayGlobals::SetCachedParams` (`font_render_params_win.cc:106-109`).
- The correction is Skia's **mask preblend**, baked into the glyph mask at
  rasterization: `SkScalerContext.cpp:100-102` takes `fPreBlend` from the rec,
  `:167-185` (`GetMaskPreBlend`/`CachedMaskGamma`) builds it from
  `SkMaskGamma::preBlend(luminanceColor)`, and the LUT itself is
  `SkMaskGamma.cpp:82-136` (`SkTMaskGamma_build_correcting_lut`): per text-color
  luminance, `srca = a + (1−a)·contrast·linDst·a`, composite in linear light, then
  "undo what the blit blend will do" so the sRGB framebuffer gets the linear-light
  result. The DirectWrite scaler applies the LUT per channel:
  `SkScalerContext_win_dw.cpp:2155-2190` dispatches `GrayscaleToA8` (`:1978-1994`) and
  `RGBToLcd16` (`:2016-2044`) with `fPreBlend.fR/fG/fB`.
- Rendering mode: Skia's own table (`SkScalerContext_win_dw.cpp:313-410`): gasp v1
  ranges, else >20 device px or unhinted → `NATURAL_SYMMETRIC`; grid fit
  **enabled** by default (`:395-398`) — Chromium asks for medium hinting
  (`ui/gfx/font_render_params_win.cc:83`) which Skia maps to normal
  (`SkTypeface_win_dw.cpp:740-745`). Grayscale text is `DWRITE_TEXT_ANTIALIAS_MODE_
  GRAYSCALE` + `DWRITE_TEXTURE_ALIASED_1x1` (`:410-418`).
- LCD is dropped for large text: `SkScalerContext.cpp:1173-1187` defines
  `SK_MAX_SIZE_FOR_LCDTEXT 48`, and `:1256-1260` turns the LCD mask into A8 when
  `det(post2x2)·textSize² > 48²` — i.e. **text above 48 device px renders grayscale**.
- Synthetic italic on a face without one: `setSkewX(-SK_Scalar1/4)`
  (`platform/fonts/win/font_platform_data_win.cc:37-38`) — a −0.25 x-shear (−14.04°),
  applied by Skia's raster (`rec->fPreSkewX = font.getSkewX()`,
  `SkScalerContext.cpp:1207`). Chromium builds with `SK_WIN_FONTMGR_NO_SIMULATIONS`
  so DirectWrite never synthesizes; Blink does (report 04 §1).

**gpui-go (rev 14)**:

- Coverage: DirectWrite `CreateGlyphRunAnalysis` + `CreateAlphaTexture`, raw DWrite
  coverage (no gamma/contrast at raster time): `reference/native/src/glyph.rs:830-905`
  (`create_glyph_analysis`), mode table `GetRecommendedRenderingMode` with the **system**
  `IDWriteRenderingParams` (`:859-875`), subpixel → `CLEARTYPE_3x1` + ClearType
  antialias, grayscale → `GRAYSCALE` + `ALIASED_1x1` (`:876-890`).
- Correction: the pinned **shader**, at composite: `renderer/draw.rs:210-237`
  (`font_info()`) reads `GetGamma()` → the Windows-Terminal gamma table
  (`ce:crates/gpui/src/platform.rs:1782-1811`), `GetGrayscaleEnhancedContrast()` (0.5
  here) and `GetEnhancedContrast()` as uniforms; `ce:crates/gpui_render/src/shaders/
  sprites.rs` `fragment_monochrome_sprite` and the subpixel fragment apply
  `apply_contrast_and_gamma_correction` (`shaders/common.rs:307-365`):
  `enhance_contrast` gated by `light_on_dark_contrast` = `ec·saturate(4·(0.75−luma))`
  (constants `common.rs:27,39-40`) — i.e. contrast applies to **dark text only** — then
  the Terminal gamma-ratio `apply_alpha_correction`.
- Synthetic italic: Fontique resolves Tahoma-italic to the upright face with
  `synth.skew = 14` (fontique-0.11.1 `src/font.rs:108-118`), and the native face
  creation maps that to `DWRITE_FONT_SIMULATIONS_OBLIQUE`
  (`reference/native/src/glyph.rs:1301-1309`) — the angle is DirectWrite's, not 14°.
- gpui's AA mode is the platform recommendation alone (ClearType on → subpixel at every
  size; `gpui/draw_text.go:567-585`, `glyph.rs:1266-1272`): there is no per-window or
  size-based fallback to grayscale today (the pin's window-transparency check is
  unported — spec Q21 owns that half).

## 2. Getting each AA mode in headless Chrome (and in gpui-go)

Measured, not guessed — this is the question the ticket called out:

- **Default headless Chrome (`--headless=new`, the recorder's flags): LCD ClearType
  text.** The dark cells' mean channel spread (max−min channel coverage over ink
  pixels) is 0.2048 at 12/16/24 px — classic ClearType fringes (`0/0/33 115/211/255
  255/255/214 115/36/0` on a stem edge). So headless does **not** force grayscale.
- **`--disable-lcd-text`: pure grayscale** — channel spread exactly 0.0000 on every
  cell. The two records differ in 8 769 of 1 024 000 pixels (mean 0.248), so the flag
  does exactly this and nothing else.
- The harness now supports this per fixture: `Fixture.ChromeFlags` (new) is appended to
  the recorder's Chrome command line; `textpaint-raster` records with the default flags,
  `textpaint-raster-gray` with `--disable-lcd-text`. `--force-color-profile=srgb` was
  considered and left off: both sides are sRGB-native here and every probe colour
  (#000/#fff/#808080) is color-management-invariant.
- **gpui-go**: the mode is the OS ClearType flag (subpixel here). There is no runtime
  knob at the window level (the pin had a per-window `TextRenderingMode` override; the
  port's element path passes `PaintTextOptions{}` = platform default,
  `gpui/div.go:480`). The **raster** half of both modes is reachable through the public
  `TextSystem.PrepareRasterStyle`/`RasterizeGlyph` API, which the probe uses for the
  raw-coverage measurement (§4.3); a composite grayscale comparison would need the
  unported override, so like-with-like grayscale is compared at the raster level and
  the mode difference is measured as its own finding (§5).

## 3. Environment facts (all read by the probe, `TestEnvironment`)

| Fact | Value | Read through |
|---|---|---|
| gpui raster backend | DirectWrite + Swash fallback (0), variable factory present | `gpui.TextSystem.RasterBackend()` |
| DirectWrite gamma | **1.8** | same |
| DirectWrite grayscale enhanced contrast | **0.5** | same |
| OS font smoothing | ClearType on → gpui recommended mode = subpixel | `RecommendedRenderingMode()` |
| Chrome's gamma/contrast source | Skia compile-time (sRGB gamma, contrast 1.0): `Avalon.Graphics` has **no per-display subkeys**, so `FontUtilWin::GetTextParameters` never reads DirectWrite's 1.8/0.5 | registry check + `font_util_win.cc:23-49` |

So the two engines run **different correction mechanisms with different parameters on
this PC** — Chrome never sees DirectWrite's 1.8/0.5, gpui never sees Skia's sRGB/1.0.
That is the worst case for a visible gap, which makes the small measured difference
(§4) the stronger result.

## 4. The rasterization A/B (T6)

Fixtures: `internal/parity/fixtures/textpaint` — "Hamburg" in Segoe UI, three colour
columns (dark #000 on #fff, light #fff on #000, grey #808080 on #fff) × four sizes
(12/16/24/32 logical px, 24–64 device px at the harness's 2× scale), fixed 200×56 cells,
line height pinned to the pin's 1.618. Ink is derived per channel as
`(pixel − bg)/(fg − bg)` (`parity.MeasureInk`, new harness helper).

### 4.1 Mean ink (total text darkness), Chrome vs gpui-go

The like-with-like columns (Chrome LCD vs gpui ClearType — the mode both choose
by default; Chrome's record is the LCD one):

| cell | Chrome mean ink | gpui mean ink | ratio | cell | Chrome | gpui | ratio |
|---|---|---|---|---|---|---|---|
| dark-12 | 0.0104 | 0.0100 | **0.957** | dark-24 | 0.0427 | 0.0418 | **0.979** |
| light-12 | 0.0125 | 0.0119 | **0.952** | light-24 | 0.0472 | 0.0459 | **0.972** |
| grey-12 | 0.0112 | 0.0112 | **1.001** | grey-24 | 0.0444 | 0.0445 | **1.002** |
| dark-16 | 0.0183 | 0.0178 | **0.969** | dark-32 | 0.0757 | 0.0746 | **0.986** |
| light-16 | 0.0213 | 0.0204 | **0.959** | light-32 | 0.0832 | 0.0800 | **0.961** |
| grey-16 | 0.0194 | 0.0195 | **1.005** | grey-32 | 0.0784 | 0.0781 | **0.997** |

Against Chrome's grayscale record (`--disable-lcd-text`; the gpui side is still
ClearType, so this is mode-mismatched): dark 0.960–0.986, grey 0.991–0.997,
light 0.904–0.961 — the light-on-dark small sizes are the widest gap, and §4.3 shows
it is the correction curve, not the mode.

Reading: **total ink is within 5% everywhere in the like-with-like comparison**
(dark 1.4–4.3% lighter, light 2.8–4.8% lighter, grey ≤ 0.5%). For scale: report 04's
measured synthetic-bold strength difference (ticket 13) was 1.521 vs 1.517 — 0.3%;
the pin's own D1 line-height change is 30%+. A 2–5% ink difference on AA edges is at
or below the level the Tier C tolerance already absorbs for text (the tracer fixture's
text-bearing box ROI measures 0.231 mean channel diff against a 0.5 gate).

### 4.2 Coverage histograms and edge profiles

Per-bucket coverage histograms of the 16 px cells agree to ~0.001–0.003 of the cell
(e.g. dark-16, bucket fractions `0.9745 0.0018 0.0006 … 0.0105` Chrome vs
`0.9749 0.0016 0.0010 … 0.0096` gpui; the full table is in the probe log). The
edge-darkness (mean coverage of partial pixels) is where the mechanisms show:

| cell (LCD record) | Chrome partial-ink | gpui partial-ink | cell (grayscale record) | Chrome | gpui |
|---|---|---|---|---|---|
| dark-16 | 0.5278 | 0.5228 | dark-16 | 0.5110 | 0.5228* |
| light-16 | 0.5658 | 0.5327 | light-16 | 0.6117 | 0.5327* |
| grey-16 | 0.4925 | 0.5013 | grey-16 | 0.4994 | 0.5013* |

*gpui side unchanged between records (it always renders ClearType here) — the
grayscale-record column is Chrome-only evidence.

Dark and grey edge darkness match within ~2%; **light-on-dark edges are ~6% lighter in
gpui (grayscale record: ~13%)**. That is the one systematic curve difference, and
§4.3's analytic comparison explains it exactly.

Captures (checked in, `captures/`): `textpaint-raster-{dark,light,grey}.png` and
`textpaint-raster-gray-*.png` (each cell column, Chrome above gpui-go) and
`textpaint-{raster,raster-gray}-dark-16-4x.png` (the 16 px dark cell at 4×).

### 4.3 The effective correction curves (why they match)

gpui's raw A8 coverage (public `RasterizeGlyph`, no shader): the AA edge values are
DWrite's — e.g. `H` at 16 px logical/32 device px: stems 255 with single partial pixels
of 191 (0.75); at 32 px: edges 64/128 (0.25/0.5). The ClearType raw ramps are the
classic quintets `43 128 213 255` — the same ramp Chrome's composite shows
(`33 115 211 214 255`), confirming both sides rasterize through the same DWrite
coverage and differ only in the correction applied on top.

Chrome's effective curve (black on white, sRGB gamma, contrast 1.0), from
`SkTMaskGamma_build_correcting_lut`: `srca = a·(2−a)` (contrast 1.0, linDst 1),
`mask = 1 − sRGB(1 − srca)`. gpui's (pin shader, gs contrast 0.5, Terminal table at
gamma 1.8, brightness 0): `enhance_contrast(a, 0.5) = 1.5a/(0.5a+1)` then
`a + a(1−a)·(−0.895a − 0.325)`:

| raw edge coverage | 0.25 | 0.50 | 0.75 |
|---|---|---|---|
| Chrome effective | 0.226 | 0.463 | 0.721 |
| gpui effective | 0.211 | 0.415 | 0.670 |
| difference | −6.6% | −10.4% | −7.1% |

| raw (white on black) | 0.25 | 0.50 | 0.75 |
|---|---|---|---|
| Chrome effective (sRGB(a), contrast 0) | 0.537 | 0.735 | 0.880 |
| gpui effective (no contrast, gamma table at brightness 1) | 0.431 | 0.695 | 0.861 |
| difference | −20% | −5% | −2% |

The black-text curves land within ~10% of each other and the measured composites
confirm them: Chrome's grayscale 32 px dark edge pixel measures 119/255 = 0.466
against the analytic 0.463 for a raw-0.5 edge (and rules out contrast 0.5, which gives
0.356, and 0, which gives 0.265 — this is the empirical confirmation that Chromium's
Windows build contrast is 1.0). The light-text curves diverge most at the faint end
(gpui −20% at raw 0.25), which is exactly the measured light-on-dark edge lightness;
because those are the faintest AA pixels of the least numerous edges, the total-ink
cost stays at ~4–5%.

**Net: on this machine the two completely different mechanisms land on nearly the same
effective curves.** And on a machine where the ClearType tuner has run (per-display
`Avalon.Graphics` key present), Chrome switches to DirectWrite's gamma/contrast — the
same source gpui reads — so the two track each other even more closely there.

### 4.4 Tier C and the regression gates

Correct-view statistics (the numbers the fixture tolerances are set from, at 2×
headroom per the guide): `textpaint-raster` whole mean 0.4470 / hot 0.00554, worst ROI
(light-32) 1.5235 / 0.02205; `textpaint-raster-gray` whole 0.5858 / 0.00758, worst ROI
(light-24) 1.7428 / 0.02502; `textpaint-italic` whole 0.3460 / 0.00494, worst ROI
(tahoma-italic) 1.7671 / 0.02340. The deliberately-wrong views (1.5× font size in the
same fixed cells; dense M stems in the italic rows) measure raster whole
4.0068–4.0197 / worst ROI 16.1440 and italic whole 1.5690 / worst ROI 6.5492 / hot
0.08277 — past every gate with real margin (`textpaint_test.go`). One honest limit: a
defect that only *removes* the synthetic slant (rendering Tahoma-italic upright)
measures whole 0.721 — just under the 0.75 gate — because the italic rows' ink is
sparse and the correct-view LCD noise is high; under the harness's 2× headroom rule
the pixel gate cannot both admit the current view and catch that. The slant probe
(§6), not Tier C, is the detector for it.

The whole-image means (0.35–0.59) are dominated by ClearType fringe *phase* — when two
LCD rasters land one subpixel apart, edge pixels differ by up to ~150 per channel while
total ink barely moves (§4.1). That is why the probe measures ink, not just the diff.

## 5. The two real divergences (mode selection, not gamma)

Both are AA-**mode** selection, owned by report 04 §2(b)'s "rendering mode" native item
and Q21 — not by R17's gamma/contrast shader work:

1. **Chrome stops LCD at 48 device px; gpui never does.** Measured: Chrome's 32 px
   logical (= 64 device px) cells have channel spread exactly 0 (grayscale), while
   gpui's are 0.163 (ClearType fringes); 12/16/24 px (≤48 device px) fringe on both.
   Source: `SkScalerContext.cpp:1173-1187` (`SK_MAX_SIZE_FOR_LCDTEXT 48`) +
   `:1256-1260` (LCD → A8 when `det(post2x2)·size² > 48²`); at 2× scale the boundary
   sits exactly between the 24 px (48², kept) and 32 px (64², dropped) samples — the
   measurement pins it to the pixel. gpui: `glyph.rs` picks the mode once (subpixel
   whenever ClearType is on), no size rule. Ink still matches (0.96–0.99) because the
   total coverage is mode-independent; the visible difference is fringes on large
   text. A "grayscale above 48 device px" rule would be a small native change in the
   next rebuild, and report 04 §2(b) already tracks the neighbouring grid-fit
   difference.
2. **gpui has no grayscale window mode when ClearType is on** (the pin's
   window-transparency check is unported; spec Q21 "grayscale AA on non-opaque
   windows"). Chrome falls back to grayscale whenever the layer isn't opaque
   (`cc/raster/lcd_text_disallowed_reason.h:17-30`) — on glass, Chrome's text is
   fringe-free where gpui's would fringe.

## 6. The synthetic-italic probe (T7)

Fixture `textpaint-italic`: Tahoma (Regular + Bold only, no italic face) upright and
`font-style: italic`, plus a Segoe UI italic control (a real italic face), ten
lowercase l's at 32 px. Measurement: the per-row ink centroid's least-squares shear
(49 ink rows per row, RMS residual ≤ 0.02 px on the synthetic rows):

| row | Chrome shear | gpui shear | dx over the stems |
|---|---|---|---|
| tahoma-upright (control) | **−0.0000** (−0.00°) | **0.0000** (0.00°) | 0.0 / 0.0 px |
| tahoma-italic (synthetic) | **−0.2500** (−14.03°) | **−0.3399** (−18.77°) | 12.0 vs 16.3 px |
| segoe-italic (real face, control) | **−0.1684** (−9.56°) | **−0.1685** (−9.56°) | 7.7 vs 7.8 px |

Chrome's measured −0.2500 is Skia's `setSkewX(−SK_Scalar1/4)` to four decimals — the
measurement method is validated by the two controls (upright 0, real-italic identical
on both sides). gpui's −0.3399 is DirectWrite's oblique simulation angle: the native
face creation turns Fontique's italic synthesis into
`DWRITE_FONT_SIMULATIONS_OBLIQUE` (`reference/native/src/glyph.rs:1301-1309`) and
discards the angle Fontique computed — 14° (`fontique-0.11.1/src/font.rs:108-118`),
which would have matched Chrome. So gpui's faux italics lean **34% more** than every
browser's, visible at any size (captures: `captures/textpaint-italic.png`, and
`captures/textpaint-italic-4x.png` at 4×).

**Fix shape (for the owning ticket, not done here):** in the native raster path, apply
the shear Fontique reports (`face.synthesis.skew_degrees`) as a transform on the glyph
run / outline — as Skia does — instead of (or in addition to) the DirectWrite oblique
simulation; the same rebuild can carry the 48-device-px grayscale rule. It is a native
S change (one rebuild), recorded as a divergence toward CSS per D0, with this fixture
becoming the gate (its tolerance will need re-recording when the fix lands).

## 7. Why R17 is a NO

1. **Total ink** (the "does the text look as heavy as Chrome's" measure) is within
   5% for dark and grey text at every probed size and within 5% for light text in the
   like-with-like mode — the level at which report 04's own bold-synthesis comparison
   (0.3%) was called equal, and far below anything a user reads as weight.
2. **Edge coverage curves** differ by ≤ ~10% (dark) and up to ~20% at the faintest
   light-on-dark AA pixels — differences on partial pixels that make up ~2% of a text
   cell, under the hot-pixel bar of the harness's Tier C.
3. **The mechanisms converge by construction on this PC** (Chrome sRGB+1.0 vs
   DWrite-1.8-derived Terminal table + 0.5 contrast) and converge more on
   tuner-configured PCs (both read DirectWrite). The worst-case machine is the one
   measured.
4. What the A/B did surface — the 48-device-px LCD cap and the missing grayscale
   window mode — is mode selection, which R17's "gamma/contrast preblend + grid-fit
   table" was never going to fix. Those belong to report 04 §2(b)/Q21 and the next
   native rebuild, and are recorded here with measurements.

Per the spec's R17+ list ("Skia-style gamma/contrast preblend and grid-fit table,
**only if** T6 shows a visible gap"), no visible gap was shown: **skip R17**.

## 8. What landed

- `internal/parity` harness extensions (documented in
  [docs/browser-parity-harness.md](../../../docs/browser-parity-harness.md)):
  - `Fixture.ChromeFlags` — per-fixture recorder flags (the AA-mode lever).
  - `parity.MeasureInk` / `InkStats` / `ROIOfDevice` (`textmeasure.go`) — the
    text-ink statistics (mean ink, ink/full/partial fractions, partial-edge darkness,
    coverage histogram, LCD channel spread) a text fixture's tolerance is calibrated
    from.
- Fixtures `internal/parity/fixtures/textpaint` (+ records in
  `internal/parity/testdata/textpaint-*`, recorded with Chrome 155.0.8059.85) with
  discrimination tests; registered in `TestFixtures` and `cmd/parityrecord`.
- This probe: `research/browser-parity/08-raster-probes/` (analysis + tests; rerun with
  `go test ./research/browser-parity/08-raster-probes/ -v`), captures under
  `captures/`.
- No gpui or native behaviour was changed; this ticket measures.

## 9. Limitations

- One PC, one Chrome version, one font (Segoe UI) for the raster A/B, four sizes; the
  italic probe covers Tahoma/Segoe UI only. The analytic curves in §4.3 generalize
  (they depend only on the gamma/contrast inputs), but the ink ratios include
  subpixel-phase and hinting effects that are font- and size-specific.
- gpui's composite grayscale mode cannot be produced today (the per-window override is
  unported), so the grayscale like-with-like is raster-level on the gpui side; the
  composite grayscale comparison is Chrome-only. When Q21 lands, re-record
  `textpaint-raster-gray` with a grayscale gpui view for the composite numbers.
- Mean-ink ratios conflate coverage-curve differences with subpixel-phase differences
  at the ~1–2% level; §4.3's raw-coverage measurement is the clean isolation.
- Chrome's contrast=1.0 build value is empirically confirmed here (§4.3) and was web-
  verified by report 04 [V-web]; the sparse Chromium checkout cannot show the build
  define itself.
- The light-on-dark gap (−4 to −5% mean ink) is the largest systematic difference
  found. If a future calibration (research 19) freezes a Tier C gate that this
  violates, the first suspect is the pin's `light_on_dark_contrast` (contrast 0 for
  light text) vs Skia's gamma-only light-text curve — that would be an R17-scope
  shader tweak, decided then, with these fixtures already in place.
