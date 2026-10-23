# 04 · Text and fonts: how the browser behaves, and how gpui-go differs

Ticket: `.scratch/browser-parity/issues/04-text-and-fonts.md`. Researched 2026-10-23. Read only, nothing built.

Sources and pins:
- **Chromium** `0d4dcd4f` (sparse checkout, `%TEMP%\research-src\chromium`). Paths are relative to `third_party/blink/renderer/` unless they start with `cc/`, `ui/` or `skia/`.
- **Chromium `skia/BUILD.gn` and `skia/config/SkUserConfig.h`** are not in the sparse checkout. A web-fetch helper read them at the same commit on chromium.googlesource.com. It could not give line numbers, so those claims are marked **[V-web]**.
- **Skia** `e71ecd07` (`%TEMP%\research-src\skia`).
- **gpui-go**, this worktree: `gpui/` and `reference/native/src/` (native rev 14).
- **The pinned GPUI-CE oracle**, `254b5dbd`. This worktree has no `reference/ce-source`, so I read the main checkout's copy (`gpui-go/reference/ce-source`, HEAD `254b5dbd47cb…`, the exact pin), read only. Paths are shown as `ce:crates/…`.
- **Crates from the cargo registry:** Parley 0.11.1, Fontique 0.11.1.
- **Specs:** [CSS Fonts 4](https://drafts.csswg.org/css-fonts-4/), [CSS Text 3](https://drafts.csswg.org/css-text-3/), [CSS Text Decoration 4](https://drafts.csswg.org/css-text-decor-4/), [CSS Inline 3](https://drafts.csswg.org/css-inline-3/), [UAX #14](https://www.unicode.org/reports/tr14/). They are linked by section; I did not fetch the spec text again.

Markers: **[V]** means I read it in source. **[V-web]** means it was read through the fetch helper, without line numbers. **[I]** means it's inferred and not traced end to end. Sizes: S ≤ 2 days, M ≤ 1 week, L ≤ 3 weeks, XL more than that.

---

## Ranked gaps

| # | Gap | Who notices | Work | Size |
|---|-----|-------------|------|------|
| 1 | **Rev 14's bold rule is Blink's non-Windows rule.** On Windows, Blink synthesizes bold when the request is ≥ 600 and the face is < 600, for local and web fonts alike | Any app whose family lacks a Bold face (600 over 400; 700 over 500; 900 over 600) | native, 1 condition + rebuild | S |
| 2 | Glyph contrast and gamma come from DirectWrite's system values through a D2D-style shader; Chrome uses Skia's sRGB preblend with contrast 1.0. Subpixel AA isn't limited to opaque backgrounds | Every pixel of text: stroke darkness and weight; colour fringes on translucent surfaces | native (+ Go for gating) | M, measure first |
| 3 | Default line height is 1.618 × size, not CSS `normal` (the font's ascent + descent + line gap, each rounded to whole pixels) | Every ported layout's vertical rhythm | Go | S |
| 4 | Wrapping controls aren't plumbed (Parley has them): `overflow-wrap`, `word-break`, `white-space` collapse / pre / pre-line, `text-wrap: balance / pretty` | Long URLs and IDs overflow; HTML-style whitespace renders verbatim | native ABI + Go | M |
| 5 | Fallback picks a font for a script sample, not for the actual character, and passes no locale; Arial sits ahead of system fallback | Symbols and emoji-adjacent characters, CJK (Japanese text in a Chinese font), RTL in custom families | native | M–L |
| 6 | `text-transform` is resolved but never applied, so `.Uppercase()` is a silent no-op | Every uppercase label | Go | S |
| 7 | Letter-spacing doesn't turn off `liga` / `clig` / `calt` | Tracked headings in fonts with ligatures | Go | S |
| 8 | Underline: different offset and thickness formula, and no skip-ink | Every link | Go (offset, thickness); native (skip-ink) | S; M |
| 9 | `line-clamp` shows no ellipsis; no `...` fallback when the font lacks U+2026 | Card descriptions, previews | Go | S |
| 10 | No variation axes: `font-variation-settings`, `font-stretch`, and `opsz` isn't set automatically | Variable web fonts (Inter, Geist variable, Roboto Flex) | native ABI + Go | M |
| 11 | COLRv1 emoji fall back to Swash; Chrome renders COLRv1 through DirectWrite's paint reader | Emoji look on Windows 11 | native | M–L |
| 12 | Smaller items: tabs (4 em vs CSS `tab-size: 8`), no generic families (`sans-serif` / `serif` / `monospace`), em-based tracking, `text-align: justify / start / end`, fractional ascent and descent | Assorted | Go | S each |

#1 is the cheapest high-value fix and also answers ticket 13's open question. #2 is the biggest visible difference but needs a measured A/B before anyone touches the shader. #3, #6, #7 and #9 are small Go changes that make ported CSS look right.

---

## 1. Bold and italic synthesis (font-synthesis)

**Spec.** [CSS Fonts 4 §5.2 step 4 / font-synthesis-weight](https://drafts.csswg.org/css-fonts-4/#font-synthesis-weight): the UA may synthesize bold when the family lacks a bold face. The spec doesn't fix a threshold.

**Chromium on Windows. The surprise: two different rules exist, and Windows compiles only one of them.**
- `platform/fonts/skia/font_cache_skia.cc:312` opens `#if !BUILDFLAG(IS_WIN)`, which closes at `:371` [V]. The ">200 above the face" rule at `:347-353`, the one ticket 13 cited and rev 14 adopted, is **not compiled on Windows**.
- Windows uses `platform/fonts/win/font_cache_skia_win.cc:484-497` instead [V]: `synthetic_bold_requested = (Weight() >= kBoldThreshold && !typeface->isBold()) || IsSyntheticBold()`. Here:
  - `kBoldThreshold` is 600 (`font_selection_types.h:183`) [V];
  - `SkTypeface::isBold()` is `weight >= kSemiBold_Weight (600)` (skia `src/core/SkTypeface.cpp:491-493`) [V].
- So on Windows the local-font rule is **request ≥ 600 and face < 600**. It is the same rule as the web-font path, `core/css/css_segmented_font_face.cc:118-121` (`capabilities.weight.maximum < 600 && request >= 600`) [V]. That is exactly what ticket 13's Chrome probe saw, so the "open finding" there is not a web-font quirk. It is Chrome's only rule on Windows.
- Simulations don't hide the face weight. Chromium defines `SK_WIN_FONTMGR_NO_SIMULATIONS` unconditionally (skia/BUILD.gn `skia_config`) [V-web]. `SkFontMgr_win_dw.cpp:54-97` then retries `GetFirstMatchingFont` until DirectWrite applies no bold or oblique simulation [V], so `isBold()` sees the real face.
- Fallback with a bold request first retries the family at weight 400 and synthesizes whenever the request is ≥ 600 (`font_cache_skia.cc:120-141`, compiled for Windows) [V].
- Rendering: `font_platform_data_win.cc:47-48` calls `setEmbolden` and `setSkewX(-1/4)` (about 14.04°) [V]. Skia drops the DirectWrite embolden flag and strokes the outline instead (`SkTypeface_win_dw.cpp:727-738`, `useStrokeForFakeBold`, which removes `kEmbolden_Flag`) [V].

**gpui-go.**
- Fontique asks for embolden whenever the request is heavier than a static face, and for a 14° skew for italic (`fontique-0.11.1/src/font.rs:90-133`) [V].
- Rev 14 rewrites that in `reference/native/src/text.rs:1311-1358`. Synthesis stays only when `weight >= 600 && weight > face + 200` (`:1347`); otherwise the request becomes the face's own weight [V].
- Rasterization uses DirectWrite simulations: `DWRITE_FONT_SIMULATIONS_BOLD` and `_OBLIQUE` (`glyph.rs:1301-1309`) [V]. Ticket 13 measured 700-over-400 ink at 1.521 against Chrome's 1.517, so the strength matches.

**Gap.** For static faces the Windows rule differs in these cells:

| family faces | request | Chrome (Windows) | gpui rev 14 |
|---|---|---|---|
| 400 | 600 | synth | plain 400 |
| 500 (as heaviest) | 600 / 700 | synth | plain 500 |
| 600 | 700–900 | plain 600 | 700, 800 plain; **900 synth** |
| 400/700 | 800, 900 | plain 700 | plain 700 |

- **Fix (native, S):** change `text.rs:1347` to `weight.0 >= 600.0 && face_weight < 600.0`, rebuild, and update `TestBoldSynthesisFollowsBrowserRule`'s expected cells. The rev 13 rule (no synthesis below 600) is already the Windows rule.
- **Italic** [I]: DirectWrite's oblique simulation angle is not documented in source here. Compare its ink slant against Chrome's −0.25 skew in the same probe page. Size S.

## 2. Rasterization: gamma, contrast, AA mode, hinting

**Spec.** None. CSS leaves this to the UA (`-webkit-font-smoothing` is non-standard and ignored on Windows).

**Chrome on Windows.**
- **Contrast and gamma.** `ui/gfx/font_render_params_win.cc:97-109` [V] calls `FontUtilWin::GetContrastFromRegistry` and `GetGammaFromRegistry` (`kUseGammaContrastRegistrySettings` is on by default, `ui/base/ui_base_features.cc:416-417` [V]).
  - Those read DirectWrite's values **only if** `HKCU\SOFTWARE\Microsoft\Avalon.Graphics\<display>` exists, i.e. the user ran the ClearType tuner (`ui/gfx/font_util_win.cc:25-51`) [V].
  - Otherwise Chrome uses Skia's compile-time values. For Windows, `skia/BUILD.gn` sets `SK_GAMMA_SRGB` and `SK_GAMMA_CONTRAST=1.0` [V-web]. The comment there says they were "chosen to approximate the system browser".
  - The correction is applied as Skia's mask preblend, per glyph, from the paint luminance (`SkScalerContext_win_dw.cpp:2159-2195`, `GrayscaleToA8` / `RGBToLcd16` with `fPreBlend`) [V].
- **Rendering and AA mode.** Chosen by Skia's own table, not by `GetRecommendedRenderingMode` (`SkScalerContext_win_dw.cpp:269-428`) [V]:
  - gasp v1 picks symmetric or natural;
  - above 20 px, or for an unhinted font, NATURAL_SYMMETRIC;
  - a hinted font at 20 px or below without gasp v1 gets NATURAL;
  - grid fit is forced ENABLED (Chrome asks for medium hinting, `font_render_params_win.cc:83`, and Skia maps every hinting level except none to normal, `SkTypeface_win_dw.cpp:740-745`) [V];
  - grayscale is `DWRITE_TEXT_ANTIALIAS_MODE_GRAYSCALE` + `DWRITE_TEXTURE_ALIASED_1x1` (`:395-404`) [V].
- **Subpixel (LCD) text** needs ClearType on (`font_render_params_win.cc:86-95`) **and** an opaque, pixel-aligned layer. `cc/raster/lcd_text_disallowed_reason.h:17-30` lists `kBackgroundColorNotOpaque`, `kContentsNotOpaque`, non-integral offsets, will-change and so on [V]; it is computed in `cc/layers/picture_layer_impl.cc:695-704` [V]. Otherwise Chrome uses grayscale.
- **Subpixel positioning** is on whenever font smoothing is on (`font_render_params_win.cc:86-88`) [V].

**gpui-go.**
- **Rendering mode** (`glyph.rs:856-900`) [V]: `GetRecommendedRenderingMode` with the system `IDWriteRenderingParams`; OUTLINE is promoted to NATURAL_SYMMETRIC; grid fit is whatever DirectWrite recommends. Grayscale uses GRAYSCALE + ALIASED_1x1, the same as Chrome; subpixel uses CLEARTYPE_3x1.
- **Gamma and contrast** come from the system params (`reference/native/src/renderer/draw.rs:214-230`) [V]. The pinned shader (`ce:crates/gpui_render/src/shaders/common.rs:307-365`, used at `sprites.rs:151-155` and `:428-432`) [V] does three things:
  - Windows Terminal's "gamma-incorrect target" alpha correction (`ce:crates/gpui/src/platform.rs:1782-1811` table);
  - enhanced contrast, applied **only to dark text** (`light_on_dark_contrast`: `saturate(4 × (0.75 − luma))`);
  - DirectWrite's defaults for the contrast factor: grayscale enhanced contrast, typically 1.0, and ClearType enhanced contrast, typically 0.5 [I].
- **Subpixel AA is chosen by the system ClearType flag alone** (`gpui/draw_text.go:38-45` records that the pin's window-transparency check isn't ported; `glyph.rs:716-722`) [V].

**Gap.**
- (a) The correction curves differ: DirectWrite's gamma (often 1.8) plus a D2D-style contrast, against Skia's sRGB preblend with contrast 1.0. Expect gpui text to look slightly heavier or lighter than Chrome depending on colour [I]. **Measure first**, the same way as ticket 13: same font, size and colours, black on white, white on black, and mid-grey, in grayscale and in ClearType. Then decide whether to port Skia's preblend (`SkMaskGamma`) into the shader or the atlas upload. Native M.
- (b) Rendering mode: Chrome's table forces grid fit at every size; DirectWrite's recommendation may turn it off at large sizes [I]. This can shift glyph heights by a pixel at display sizes. Native S, after (a).
- (c) Use grayscale when the window or layer isn't opaque, as the pin and Chrome do. Go S, because the host knows when the backdrop is transparent.

## 3. Line height and vertical metrics

**Spec.** [CSS Inline 3 / CSS 2 §10.8](https://www.w3.org/TR/CSS2/visudet.html#line-height): the initial `line-height` is `normal`, which the UA derives from the font's metrics. Half-leading is split above and below.

**Chromium.**
- `normal` = round(ascent) + round(descent) + round(lineGap) (`platform/fonts/simple_font_data.cc:170-178`) [V].
- Ascent and descent are rounded to whole pixels unless the font is tiny, and VDMX is used when present (`font_metrics.cc:105-121`) [V].
- For Segoe UI that is about 1.33 × size [I].

**gpui-go.**
- The default is the pinned golden ratio: `DefaultTextStyle` `LineHeight: Fraction(goldenRatio)` (`gpui/div.go:130`, `gpui/text.go:84-92`) [V].
- The baseline is `(lh − ascent − descent)/2 + ascent` (`reference/native/src/text.rs:1232-1240`) [V]. **That matches CSS half-leading.**
- Ascent and descent stay fractional.

**Gap.** Add a `normal` line-height kind, resolved from the primary font's `FontMetrics` (already exposed: `gpui/text.go:261-303`) as rounded ascent + descent + line gap. Optionally round ascent and descent the same way. Go S. Making it the **default** changes every app's layout; that's the user's call (a proposal, as with Base).

## 4. Line breaking, white-space and wrapping modes

**Spec.**
- [CSS Text 3 §5](https://drafts.csswg.org/css-text-3/#line-breaking) with [UAX #14](https://www.unicode.org/reports/tr14/): `word-break`, `line-break`, `overflow-wrap` (§5.5).
- [§3 white-space processing](https://drafts.csswg.org/css-text-3/#white-space-processing): `normal` collapses spaces and tabs and turns newlines into spaces.
- [CSS Text 4 `text-wrap`](https://drafts.csswg.org/css-text-4/#text-wrap): balance, pretty.

**Chromium.**
- ICU line breaking with locale and strictness (`platform/text/layout_locale.cc:138-150`, `@lb=`; the default has no keyword) [V].
- A fast ASCII path, and the "break after a space run" rule (`text_break_iterator.cc`, per Parley's citation) [I].
- `text-wrap: balance` and `pretty` use `ScoreLineBreaker` and bisection (`core/layout/inline/inline_layout_algorithm.cc:58-80, 154-180`) [V].

**gpui-go.**
- Parley with ICU4X and `complex-scripts` (dictionary breaking for Thai and similar; `ce:crates/gpui_ce_parley/Cargo.toml:22`) [V].
- It applies `CHROMIUM_LINE_BREAK_OVERRIDE` (`ce:…/gpui_ce_parley/src/text_system.rs:992`) [V]. That is Chromium's ASCII pair table plus the space-run and minus-before-digit rules (`parley-0.11.1/src/break_overrides.rs:55-91`) [V]. **This already matches Chrome for Latin text.**
- The adapter pushes only family, weight, style, features, letter spacing, brush and decorations (`text_system.rs:1010-1068`) [V]. Parley's `WordBreak`, `OverflowWrap`, `TextWrapMode`, `WordSpacing`, `Locale`, `FontWidth` and `FontVariations` (`parley-0.11.1/src/style/mod.rs:84-120`) [V] **never reach it**.
- The Go `WhiteSpace` has only Normal and Nowrap (`gpui/div.go:62-71`) [V]. Spaces are never collapsed, and `\n` always breaks [I, there is no collapse code in `div.go` or `styled_text.go`].
- Tailwind `break-words`, `break-all`, `text-balance`, `whitespace-pre` and similar are rejected (`gpui/class.go:1128-1129`) [V].
- Base's legacy `LineWrapper` (`gpui/line_wrapper.go:145-303`) is a separate greedy wrapper with its own word-character table, not UAX #14 [V]. It moves out with Base.

**Gap.**
- Native M: extend the run record with word-break, overflow-wrap, text-wrap mode and locale, and push them in the native service (the oracle can't be edited, so pre- and post-process in `shape_document`).
- Go S: white-space collapse (`normal`, `nowrap`, `pre-line` collapse in Go before shaping, with a byte map back to the source for carets).
- Go M: `text-wrap: balance` by bisecting the wrap width, which Chrome also falls back to.

## 5. Font fallback

**Spec.** [CSS Fonts 4 §5 cluster matching and system fallback](https://drafts.csswg.org/css-fonts-4/#cluster-matching): per character, try each family in the list, then the UA's system fallback.

**Chromium on Windows.**
- Order (`platform/fonts/font_fallback_iterator.h:72-80`, `font_fallback_list.cc:165-192`) [V]: the font-family list, then the user's "standard" font, then per-character system fallback.
- System fallback (`win/font_cache_skia_win.cc:225-295`) [V]:
  - if the request was bold or italic, first retry the family at normal weight and style;
  - then hard-coded per-script lists (`win/font_fallback_win.cc`: Arabic → Tahoma or Segoe UI, `:135-139`; emoji → Segoe UI Emoji, `:342-365`; Han chosen by the content locale, then the UI locale, `:542-600`) [V];
  - then DirectWrite fallback through `matchFamilyStyleCharacter` **with the actual codepoint and a BCP 47 locale** (`:225-256`) [V].

**gpui-go.**
- Families: primary, then descriptor fallbacks, then the service fallbacks "Lilex", "IBM Plex Sans", "Arial" (`text.rs:919-928`; `ce:…/text_system.rs:943-967`) [V]. Then Fontique's script fallback.
- On Windows, Fontique maps **a sample string for the script**, not the character, through `IDWriteFontFallback::MapCharacters`, with no locale because gpui never sets `Locale` (`fontique-0.11.1/src/backend/dwrite.rs:115-122, 200-252`) [V].
- `.SystemUIFont` → `GenericFamily::SystemUi` → "Segoe UI" (`dwrite.rs:37`; `ce:…/text_system.rs:1400-1413`) [V]. This matches Chrome's `system-ui` (the menu font, `font_cache_skia_win.cc:130-131`) [V; that the menu font is Segoe UI is I].

**Gap.**
- (a) No locale, so Japanese Han text gets the default (Chinese) Han face [I].
- (b) A character the script-sample font lacks (symbols, arrows, rare Han) can draw as tofu where Chrome finds Segoe UI Symbol [I; worth a probe string].
- (c) Arial is always ahead of system fallback. That roughly matches Chrome's "standard font" step for `sans-serif` pages, but Chrome's default standard font is Times New Roman.
- Native S for locale plumbing (the same ABI growth as #4). Per-codepoint fallback means a Fontique change or a native retry pass over `.notdef` clusters: M–L.

## 6. text-transform

**Spec.** [CSS Text 3 §2.1](https://drafts.csswg.org/css-text-3/#text-transform-property).

**Pin.** `ce:crates/gpui/src/elements/text.rs:636-680` `apply_text_transform_preserving_byte_len` [V].

**gpui-go.** `TextStyle.TextTransform` is cascaded (`gpui/div.go:115-116, 213`) and set by `.Uppercase()`, `.Lowercase()` and `.Capitalize()` (`gpui/styled_tailwind.go:46-55`) [V]. **Nothing applies it**: no `ToUpper` or `ToTitle` anywhere in `gpui/` text paths [V].

**Gap.** Port the pinned function. It preserves byte length and skips characters whose case maps change length. Go S.

## 7. letter-spacing and ligatures

**Spec.** [CSS Text 3 §8.2](https://drafts.csswg.org/css-text-3/#letter-spacing-property): when letter-spacing is non-zero, UAs should not apply optional ligatures.

**Chromium.** If letter-spacing ≠ 0, it adds `liga=0`, `clig=0` and `calt=0`, and skips `dlig` and `hlig` (`platform/fonts/shaping/font_features.cc:52-86`) [V].

**gpui-go.** Parley adds the spacing to every cluster's advance, including the last one (`parley-0.11.1/src/layout/data.rs:525-545`) [V], and leaves ligatures on. Letter spacing is px only, and Tailwind's em-based `tracking-*` names are rejected (`gpui/class.go:857-866`) [V].

**Gap.** In `ShapeText`, add `liga`, `clig` and `calt` = 0 to a run whose `LetterSpacing` is non-zero, unless the caller set them. Also add em tracking resolved at the font size. Go S.

## 8. Text decoration

**Spec.** [CSS Text Decoration 4](https://drafts.csswg.org/css-text-decor-4/): `text-decoration-thickness: auto`, `text-underline-position: auto`, and `text-decoration-skip-ink: auto`, which is on by default.

**Chromium.**
- Auto thickness = max(1, font-size / 10) (`core/style/text_decoration_thickness.cc:26-40`; `core/paint/text_decoration_info.cc:694-719`) [V].
- Auto underline offset = ascent + max(1, ceil(thickness / 2)) + offset, in whole pixels from the line top (`core/layout/text_decoration_offset.cc:16-35`) [V]. That puts the underline just under the baseline.
- `from-font` uses the font's `post` values (`:37-46`) [V].
- Skip-ink is on.

**gpui-go.**
- `.Underline()` sets a 1 px thickness (`gpui/styled.go:686-693`) [V].
- The underline sits at baseline + descent × 0.618 (the pin's fallback when Parley's offset doesn't cross the ABI, `gpui/rich_paint.go:47-55, 237-249`) [V].
- There is no skip-ink.

**Gap.**
- Go S: auto thickness max(1, size / 10) and Chrome's offset. Both need only metrics the Go side already has.
- Native M: skip-ink, which needs glyph outline intersection with the underline band.

## 9. text-overflow and line-clamp

**Spec.** [CSS Overflow 3 §text-overflow](https://drafts.csswg.org/css-overflow-3/#text-overflow) and [§line-clamp](https://drafts.csswg.org/css-overflow-4/#line-clamp): the last line of a clamped box ends with an ellipsis.

**Chromium.**
- The ellipsis is U+2026 if the primary font has it, otherwise `"..."`. A custom string is ignored when the ellipsis comes from line-clamp (`core/layout/inline/line_truncator.cc:97-107`) [V].
- Middle truncation exists only as an internal mode (`:288`) [V].

**gpui-go.**
- Shaping-based grapheme bisection (`gpui/text_truncate.go:96-125`), with start, end and middle modes and a custom affix [V].
- Truncation needs `TextOverflow` set (`:53-55`). Tailwind `line-clamp-N` sets only `LineClamp` (`gpui/class.go:867-872`) [V], so clamped text is cut with no ellipsis.

**Gap.** Go S: make `line-clamp` imply end truncation with "…", and fall back to "..." when the primary font lacks U+2026.

## 10. Variable fonts: variations, stretch, optical size

**Spec.** [CSS Fonts 4 §font-variation-settings, font-stretch, font-optical-sizing](https://drafts.csswg.org/css-fonts-4/#font-optical-sizing-def).

**Chromium.** For web fonts with `font-optical-sizing: auto`, `opsz` = font size unless it's set explicitly (`platform/fonts/font_custom_platform_data.cc:228-250`) [V].

**gpui-go.**
- `FontDescriptor` has no stretch or variations (`gpui/text.go:146-164`) [V].
- Fontique sets `wght` (and `wdth`, `slnt`, `ital`) from attributes only (`font.rs:90-133`) [V]. It detects `opsz` but never sets it (`font.rs:188, 232`) [V].

**Gap.** Native M: grow the run record with `FontVariations` and `FontWidth`, then set opsz = size for registered fonts. Go S on top.

## 11. Colour emoji

**Chromium / Skia.** Renders COLRv1 through `IDWriteFontFace7::CreatePaintReader(…COLR_PAINT_TREE, …COLR_V1)` (`SkScalerContext_win_dw.cpp:1213-1283`) [V].

**gpui-go.** COLRv0 layers through DirectWrite; COLRv1 glyphs return `NativeRasterUnsupported` and fall back to Swash (`glyph.rs:65-79, 1203-1248`) [V]. Whether Segoe UI Emoji on this OS draws differently is [I]; compare screenshots.

**Gap.** Native M–L: a COLRv1 paint-tree renderer, or DirectWrite's `TranslateColorGlyphRun` with the paint-tree format into a D2D target.

## 12. Smaller differences (all Go, S)

- **Tabs.**
  - gpui-go: 4 × font-size stops, as DirectWrite does (`gpui/text_tabs.go:16-21, 57`) [V].
  - CSS: `tab-size: 8` space advances, and only under `pre` or `pre-wrap`; under `normal`, tabs collapse to spaces ([CSS Text 3 §tab-size](https://drafts.csswg.org/css-text-3/#tab-size-property)).
- **Generic families.**
  - gpui-go rejects `font-sans`, `font-serif` and `font-mono` (`gpui/class.go:873-874`) [V].
  - Parley has generics, but the adapter maps only `.SystemUIFont` (`ce:…/text_system.rs:1400-1413`) [V]. A Go alias table would cover it: `sans-serif` → Arial, `serif` → Times New Roman, `monospace` → Consolas, as Chrome's Windows defaults are [I].
- **Alignment.**
  - gpui-go: Left, Center and Right only (`gpui/div.go:49-60`) [V].
  - CSS also has `justify`, and `start` / `end` (bidi-aware) as the default.
  - `justify` needs native Parley `Alignment::Justify`: native S.
- **Tabular numbers.** They already work through `.FontFeatures(tnum)` [V]. Tailwind `tabular-nums` and the other numeric classes are rejected with a hint (`gpui/class.go:1131`) [V]; mapping them to features is trivial.

---

## Where gpui-go already matches

- **Font matching.** Fontique implements CSS Fonts 4 §5.2: width, then style, then weight (`fontique-0.11.1/src/matching.rs:12-200`) [V]. That is the same order as Blink's `FontSelectionAlgorithm::IsBetterMatchForRequest` (`platform/fonts/font_selection_algorithm.cc:137-161`) [V].
- **No synthesis below 600, and 700 over 400.** Both match Windows Chrome.
- **Synthetic bold strength.** Measured equal in ticket 13 (1.521 vs 1.517).
- **Latin line breaking.** Chromium's ASCII break table, space-run rule and minus rule through `CHROMIUM_LINE_BREAK_OVERRIDE`, on top of UAX #14 with dictionary segmentation [V].
- **Half-leading baseline placement** [V].
- **System UI font.** Segoe UI, and Segoe UI Emoji for emoji [V].
- **Grayscale AA** uses the same DirectWrite mode and texture as Skia (GRAYSCALE + ALIASED_1x1) [V].
- **Subpixel positioning.** Horizontal subpixel positioning is on in both; gpui uses 4 x-variants [V for gpui, I for Skia's count].
- **Features and truncation.** OpenType features pass through, so `tnum`, `ss01` and friends work [V]. Grapheme-safe ellipsis truncation is done by shaping [V].

## Suggested order

1. #1 (one native condition, folded into the next rebuild).
2. #6, #7, #9 and the #8 offset and thickness (Go, about a day each).
3. #3, as an opt-in `normal` line height (the default is the user's call).
4. #2's measured A/B, which decides whether shader work is worth it.
5. One native ABI growth for run records covering #4, #5's locale and #10 together, so the DLL is rebuilt once.
