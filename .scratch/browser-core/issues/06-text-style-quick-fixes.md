# 06 - Text style quick fixes

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Several text styles behave like CSS: text-transform (uppercase, lowercase, capitalize) is applied, non-zero letter-spacing turns off ligatures, underlines use Chrome's thickness and offset, line-clamp ends with an ellipsis, tabular-nums and friends work as classes, the generic families sans-serif/serif/monospace resolve, and text on non-opaque windows uses grayscale antialiasing.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures for each item match Chrome (boxes exact, pixels within tolerance).
- [x] text-transform matches the pin's behaviour (it was a port gap, not a divergence).
- [x] Grayscale AA on a transparent window is covered by a scene/glyph spec test.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments
- 2026-10-24, orchestrator 7: verified and merged (f2cc78c). No item needed native support, so nothing from 06 joins the rev 15.1 list. Merge conflicts were only shared lists (fixture imports, class tests) plus a duplicated recorder comment. The textstyle fixtures and the gpui/glyphspec tests pass in the full suite on the merged tree.

## Answer

All seven items landed Go-side; none needed the R15 native fields (the
DLL is untouched). The fixtures live in the new package
`internal/parity/fixtures/textstyle` (one fixture per item, recorded in
headless Chrome 155.0.8059.85, registered in `TestFixtures` and
`cmd/parityrecord`), each with a deliberately-wrong view reproducing the
port's pre-fix behaviour that must FAIL Tier C while Tier A/B keep
passing (`fixtures/textstyle/textstyle_test.go`). The shared fixture
shape and the baseline-alignment arithmetic (Chrome floors each line
box's baseline to a whole CSS pixel with Win32-rounded metrics,
`platform/fonts/font_metrics.cc:113-114`, verified by probing Chrome's
rendered baselines; fonts whose VDMX metrics diverge from the shaper's
hhea carry a documented per-row line-height compensation) are commented
in `fixtures/textstyle/textstyle.go`.

### 1. text-transform (port gap, matches the pin)

- Ported the pin's `apply_text_transform_preserving_byte_len`
  (ce:crates/gpui/src/elements/text.rs:636-691, incl.
  `push_case_mapped_character` and the `split_word_bounds` capitalize)
  as `gpui/text_transform.go:44` (`applyTextTransformPreservingByteLen`,
  `pushCaseMappedCharacter`, `wordBoundPieces` over
  clipperhouse/uax29/v2/words): uppercase/lowercase per character
  keeping only length-preserving mappings, capitalize uppercasing the
  first Alphabetic of every UAX #29 word.
- Applied at the pin's call site (text.rs:895, inside
  TextLayout::layout, before shaping and inline publication): the text
  element `gpui/div.go:602` and `TextLayout.layout`
  `gpui/styled_text.go:282`.
- Fixture `textstyle-transform`: measured Tier C whole 0.023 / worst
  row 0.189 / hot 0.0001 under tolerance 0.06/0.4/0.002; the wrong view
  (raw untransformed text) measures 1.880 and fails. Unit tests port
  the pin's text_transform_tests (gpui/text_transform_test.go).

### 2. Non-zero letter-spacing turns off ligatures

- `gpui/text.go:786` `letterSpacingLigatureFeatures` appends liga/clig/
  calt = 0 to a run's features when its LetterSpacing is non-zero (a tag
  the caller set is left alone — Chrome appends font-feature-settings
  after the spacing-derived features and the later setting wins in
  HarfBuzz); used in `ShapeText`'s run-spec build at `gpui/text.go:726`.
  Chromium citation: `platform/fonts/shaping/font_features.cc:59-86`
  (`letter_spacing` pushes no_liga, no_clig, no_calt, and keeps
  dlig/hlig from being enabled).
- Fixture `textstyle-tracking-ligatures` (Constantia — whose fi/ffi
  substitutions change the advance; Cambria's only swap near-identical
  glyphs): measured whole 0.019 / row 0.170 / hot 0.0008 under tolerance
  0.05/0.4/0.002; the wrong view (liga forced back on under spacing)
  measures 2.080 and fails. Shaping-level test:
  gpui/letter_spacing_test.go (glyph ids under spacing equal the
  explicit liga/clig/calt=0 shaping and differ from the unspaced one).

### 3. Underlines use Chrome's thickness and offset

  (A recorded divergence toward CSS over the pin's 1px at baseline +
  descent*0.618.)

- `UnderlineStyle.AutoThickness` (`gpui/paint.go:344`) — the CSS
  `text-decoration-thickness: auto` — and `Styled.Underline()` now sets
  it (`gpui/styled.go:689`); resolved at paint as max(1, font size/10)
  (`gpui/rich_paint.go:279` `resolvedDecorationThickness`) and the
  underline top at baseline + max(1, ceil(thickness/2))
  (`gpui/rich_paint.go:292` `chromeUnderlineOffset`). Chromium
  citations: `core/style/text_decoration_thickness.cc:29-31`, the
  1-CSS-px floor in `core/paint/text_decoration_info.cc:715-719`, the
  offset in `core/layout/text_decoration_offset.cc:16-35` (gap =
  max(1, ceil(thickness/2)) below the alphabetic baseline; the rect
  from local_origin + line_offset with height = thickness,
  text_decoration_info.cc:496-503).
- The font size reaches the decoration painter through the new
  `shapedText.fontSize()` (gpui/div.go:455, gpui/testwindow.go:441).
- Prerequisite port gap fixed: a plain text child never painted its
  runs, so `.Underline()` on a Div was a no-op (the pin's
  TextLayout::layout shapes and paints the ambient style's run,
  text.rs:902-915/1021-1060). The text element now shapes through
  `shapeStyledRuns` with `TextStyle.ToRun` and paints through
  `paintStyledDocument` (background + foreground, aligned within the
  bounds — the pin's paint), carrying the (possibly truncated) runs in
  `shapedCell.runs` (gpui/div.go:534-545, 620-660, 699-730). This also
  restores the pin's text-align on plain text children.
- Fixture `textstyle-underline` (16px and 40px rows; the words have no
  descenders so Chrome's unported skip-ink never fires): measured whole
  0.116 / 40px row 0.813 mean, 0.0118 hot (large-size LCD noise; the
  16px row is 0.220/0.0035) under tolerance 0.24/1.7/0.024; the wrong
  view (the pin's fixed 1px) measures 2.384 and fails. Geometry unit
  test: gpui/underline_geometry_test.go (thickness/offset formulas plus
  the scene record's snapped geometry at 16 and 40px, computed from the
  same shaped summary the painter reads).

### 4. line-clamp ends with an ellipsis

- `EvaluateTextOverflow` (gpui/text_truncate.go:61) synthesizes an
  end-ellipsis truncation (same width rule as an explicit
  TextOverflow) when only `LineClamp` is set — CSS Overflow 4
  line-clamp; the class needs no change beyond the clamp it already
  sets. The marker resolves through the primary font: `cssEllipsisAffix`
  / `resolveTruncationAffix` (gpui/text_truncate.go:112,126) use the new
  `windowTextSystem.glyphForChar` (gpui/div.go:311-319; real system
  gpui/inline_real.go:85 over ResolveFont/GlyphForChar, test stub
  gpui/testwindow.go) — U+2026 when the font has it, else "...".
  Chromium citation: `core/layout/inline/line_truncator.cc:89-107`
  (custom strings ignored under line-clamp; U+2026 when the primary
  font carries the glyph, else "...").
- Fixture `textstyle-line-clamp` (a 260x68 row, 2 clamped lines): the
  rows are flex COLUMNS like Chrome's block rows so the text child
  receives the row width (a flex row would size the text at its
  max-content width — the port's D2 min-content gap, documented in the
  fixture). Measured whole 0.126 / row 0.579 / hot 0.0045 under
  tolerance 0.26/1.2/0.01; the wrong view (clamp 1 instead of 2)
  measures 1.769 and fails. The correct view's floor is the truncation
  point: the port's grapheme bisection (the pin's
  truncate_to_shaped_layout) keeps about two glyphs more than Chrome's
  line truncator on the clamped line — a pin behaviour, not this
  ticket's change. Unit tests: gpui/text_truncate_test.go
  (TestLineClampAloneEndsInEllipsis,
  TestEllipsisFallsBackToThreeDotsWhenTheFontLacksU2026).

### 5. tabular-nums and friends as classes

- `gpui/class.go:905-940` `isNumericVariantClass`/`numericVariantClass`/
  `numericVariantFeatures` map the nine utilities to their OpenType
  features (tnum, pnum, lnum, onum, zero, ordn, frac, afrc; normal-nums
  clears), applied in `typographyClass` (gpui/class.go:890) and removed
  from `unsupportedClasses`. Chromium citation: the font-variant-numeric
  arms of `platform/fonts/shaping/font_features.cc:157-196`.
- Fixture `textstyle-tabular-nums` (Arial — Segoe UI's figures are
  already tabular so the class would be a no-op; Arial's are
  proportional: 111 at 24.3px vs 888 at 26.7px per 16px): measured
  whole 0.015 / row 0.132 / hot 0.0012 under tolerance 0.05/0.3/0.003;
  the wrong view (the tnum row without the class) measures 1.167 and
  fails the ROI gate. Shaping test: gpui/font_features_test.go
  (TestTabularNumsReachTheShaper, Arial at weight 400). Class tests:
  gpui/class_test.go TestClassTextStyleQuickFixes (the old
  font-sans/numeric rejection expectations replaced).

### 6. The generic families resolve

- `gpui/text.go:232-275` `genericFontFamilyAliases` +
  `resolveGenericFontFamily`/`resolveGenericFontFamilies` map
  sans-serif/serif/monospace (case-insensitive) to Arial/Times New
  Roman/Consolas at the native boundary — both `FontDescriptor.
  toNative` (ResolveFont) and `ShapeText`'s run spec (gpui/text.go:216,
  722-730). Citations: Blink resolves the keywords through settings
  (`platform/fonts/font_selector.cc:74-83`), whose Windows defaults
  match Fontique's Windows generic table
  (fontique-0.11.1/src/backend/dwrite.rs:31-39).
- The classes: `font-sans`/`font-serif`/`font-mono` map to the families
  Chrome resolves Tailwind's default stacks to on Windows (the system
  UI font via system-ui, Georgia, Consolas) — gpui/class.go:883-896;
  docs/tailwind.md updated both ways.
- Fixture `textstyle-generic-families` (three generic-keyword rows and
  three Tailwind-stack rows): measured whole 0.028 / worst row 0.089 /
  hot 0.0004 under tolerance 0.07/0.2/0.001; the wrong view (every row
  in the default family) measures 1.225 and fails. Unit tests:
  gpui/font_generic_test.go (the alias table, resolution and shaping
  equivalence with the concrete families).

### 7. Grayscale antialiasing on non-opaque windows

- `PaintTextOptions.Background` (gpui/draw_text.go:582) and the pin's
  first `should_use_subpixel_rendering` check (gpui/draw_text.go:590):
  a transparent window background returns false — grayscale — before
  the override or the platform recommendation; fed from the frame's
  `PaintContext.Background` (gpui/scene.go:532) which `DrawWindowFrame`
  fills per frame from the live host window record
  (gpui/render.go:129, 391 `windowDrawBackground`; test windows are
  opaque like the reference test window). The inline piece path derives
  it too (gpui/draw_text.go PaintShapedTextLine's glyph loop). Pin
  citation: ce:crates/gpui/src/window.rs:4926-4931 (the
  background-appearance check precedes the mode);
  is_subpixel_rendering_supported is not ported because the Windows
  platform always answers true (gpui_windows window.rs:913) — nothing
  is missing on Windows. Chrome's equivalent gate: cc's LCD-text
  disallow list (cc/raster/lcd_text_disallowed_reason.h:17-30,
  kBackgroundColorNotOpaque).
- Scene/glyph spec test (as the ticket asks): internal/glyphspec/
  text_aa_spec_test.go `TestTextPaintModeFollowsWindowBackground` — a
  shaped line painted per background appearance; the sprite class is
  the observable (grayscale rasters insert MonochromeSprite records,
  subpixel insert SubpixelSprite): transparent/blurred/Mica/MicaAlt
  force monochrome-only, the opaque default follows the recorded
  platform recommendation (Subpixel on this machine, pinned by the
  fx-0005 trace), and an explicit subpixel override cannot beat a
  transparent background (the check precedes the override). End-to-end
  window half: gpui/text_aa_window_windows_test.go — a hidden,
  never-shown app window draws text opaque (subpixel sprites) and then
  with `SetBackgroundAppearance(Transparent)` (monochrome only).

### Harness fixes and recorded divergences

- `internal/parity/recorder.go`: the page instrumentation marshals a
  nil EventKinds list as `[]` not `null` (a fixture with no events
  previously failed recording with "null is not iterable").
- Recorded divergences toward CSS (each documented at its code site):
  the underline geometry (item 3), the line-clamp ellipsis synthesis
  (item 4), the class/family mappings (items 5-6), and
  `Underline()`/`UnderlineStyle` gaining the auto-thickness semantics.
  Restored pin behaviours (port gaps, not divergences): the
  text-transform application, the text element's runs-shaped/painted
  path (decorations and text-align on plain text children).
- Existing tests updated for the intended changes (not silently):
  gpui/text_truncate_test.go's recorder also hooks the paintLine path
  the text element now paints through; gpui/class_test.go's
  font-sans/numeric-variant rejection rows replaced by the new
  supported-class assertions.

### Not done (out of this ticket)

- Skip-ink (native, R17+; the fixture avoids descenders so it never
  fires), the strikethrough geometry (still the pin's half-ascent
  fallback), the em-based `tracking-*` classes and generic
  `cursive`/`fantasy`/`math` families (ticket T4's scope), and the
  Skia-gamma/contrast rasterization A/B (research 19 / T6).
