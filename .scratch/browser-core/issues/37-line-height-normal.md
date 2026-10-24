# 37 - Line height normal by default

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: implementer on core/37-line-height (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** The default line height is CSS normal (from the font's ascent, descent and gap); 1.618 stays available as an opt-in (D1).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures with default line height in Segoe UI and Geist match Chrome's boxes exactly.
- [x] Changed fixtures carry divergence notes; examples are checked for regressions in offscreen captures.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **The rule (D1) and where it lives:** the default text style's line height is the CSS `normal` keyword, not the reference's golden-ratio fraction — `gpui.DefaultTextStyle` sets `LineHeight: NormalLineHeight()` (`gpui/div.go:157`), the new `DefiniteLengthNormal` kind (`gpui/style.go:96-108`). Resolution computes the resolved font's line spacing `lroundf(ascent)+lroundf(descent)+lroundf(line gap)` scaled to the font size: `FontMetrics.LineSpacingPx` (`gpui/text.go`), `TextSystem.NormalLineHeight`, `WindowTextSystem.NormalLineHeight` (`gpui/shaped_line.go`), and the window-aware `gpui.ResolveLineHeight` (`gpui/div.go`) used by the layout entry points (`div.go` textElement, `inline.go` finishParagraph, `styled_text.go` layout) and `Window.LineHeight` (`gpui/helpers_window.go`). A `normal` inline row bounds itself from the font's ROUNDED metrics with Chrome's leading split (ascent side floored): `roundedInlineLineBounds` + the per-piece branch in `realWindowTextSystem.layoutInline` (`gpui/inline.go`, `gpui/inline_real.go`), threaded through `inlineTextContent`/`InlineTextStyle`/`inlineLayoutRequest`/`inlinePaintPiece.lineHeightNormal`. The test window's modeled face answers the same rule deterministically (16px → 20).
- **The opt-in:** `gpui.PhiLineHeight()` (the reference's exact f32 golden-ratio fraction, `gpui/text.go`; `gpui.DefaultLineHeight(fontSize)` is its pixel form — the pin's derivation, now the opt-in); the Tailwind arbitrary class `leading-[normal]` (`gpui/class.go` typographyClass, `internal/tsxgen/classes.go`, d.ts regenerated) opts into the keyword; Tailwind's own `leading-normal` stays 1.5 (Tailwind semantics; `LeadingNormal` documented). No other new class needed.
- **Key Chromium citations:** `ComputedStyle::ComputedLineHeight`'s IsAuto branch returns `FontMetrics().LineSpacing()` — "For 'normal' line-height use the font's built-in spacing" (core/style/computed_style.cc:2425-2436); `SetLineSpacing(lroundf(ascent)+lroundf(descent)+lroundf(line_gap))` with Windows ascent/descent the rounded scaled metrics of `AscentDescentWithHacks` (platform/fonts/simple_font_data.cc:158-165, font_metrics.cc:113-114); half-leading for `normal` splits with the ascent side FLOORED — `CalculateLeadingSpace` (core/layout/inline/line_utils.cc:34-43) fed the rounded `FixedAscent`/`FixedDescent` (inline_box_state.cc:113-158).
- **Behaviour changes (all updated with their tests):**
  - Default-styled text rows are the font's line spacing (Segoe UI 16px 21, was phi→26): `TestHelperLineHeightAndModifiers` (26→20 on the test face), `TestTextElementUnderlineRecordFollowsChrome` (baseline 17/43), inlinespec `TestRealInlineMixedFontSizes` (mixed row 32/baseline 26) and `TestInlineTopAndBottomAlignmentGrowsBothExtents` (20/20.4), richtextspec `TestStyledTextLayoutGeometry`/`MeasurementFollowsWrapWidth`/`StyledTextRunDecorationsTranscript`/`InlineStyledTextRunDecorationsTranscript` (line height 20, baseline 20.4), portfixture Fx0006 (the reference-mirroring counter pins phi explicitly — the pin's default is unchanged).
  - `TextStyle.LineHeightPixels` PANICS on the `normal` kind (it cannot resolve without the font; `DefiniteLength.Absolute` panics for it too); Base's three call sites resolve through `gpui.ResolveLineHeight` (`base/text_view.go`, `base/text_inline_flow.go` incl. `plainLineHeight`, `base/text_inline_object.go`).
  - Base components follow the ambient rule (browser-wins D0): inputs/lists/labels get normal rows; the components upstream pins explicitly (button/tabs/toggle `Fraction(1)`) keep their pins. The editor's own caret default stays the pin's phi derivation (`gpui/editor.go` doc; reference-shaped document model).
  - Fixtures whose pages pin the reference's 1.618 kept their records by pinning the opt-in on their views: tracer, tsxslots, tsxtracer (TSX class `leading-[1.618]`), activation ×3, contextmenu, dnd, editkeys ×2, imecompose ×2, keycode ×2, keyorder ×4, outline ×3, tabnav, textpaint (cells + italic rows). tooltip-look DROPPED its page pin (a divergence-toward-CSS note) and was re-recorded: Tier C measured mean fell 2.47→0.59 — the closer match D1 predicts for a page at the initial line-height value.
- **New fixtures:** `internal/parity/fixtures/lineheight` — `lh-normal-segoe` (12/13/16/24/48px, a 2-line wrap, a 16px+24px inline span, an explicit 1.5 ratio row) and `lh-normal-geist` (the embedded face on both sides: the page's @font-face data URL and the replay's `AddFont`); Chrome 155's recorded boxes match the replay EXACTLY (Tier A: 16/17/21/32/64/42/32/24 and 21/31/62/31). One deliberately wrong view (`WrongPhiView`, the pre-D1 golden-ratio default) with the discrimination test in `lineheight_test.go` (fails Tier A on every row and Tier C).
- **Capture check:** zterm (19 pages) and base-gallery (69 pages) captured before/after into %TEMP%: diffs are confined to text bands (re-baselined rows at the shorter line height; zero-diff bands elsewhere), ink coverage unchanged (no clipping or loss), the palette/tabs diffs are the re-flowed text rows. Eyeballed via numeric band/cross-correlation analysis (no vision model was available at run time; alignment verified pixel-exact at dy=0).
- **Files:** `gpui/{style,text,div,inline,inline_real,testwindow,styled_text,helpers_window,shaped_line,styled,styled_tailwind,class,editor}.go`, `gpui/script/types/gpui-script.d.ts` (regenerated), `internal/tsxgen/classes.go`, `base/{text_view,text_inline_flow,text_inline_object}.go`, the fixture packages above, `internal/parity/{replay_test.go,testdata/lh-normal-*,testdata/tooltip-look}` (re-recorded), `cmd/parityrecord/main.go`, `docs/browser-parity-harness.md` (ticket 37 section).
- **Limitations:** a definite (non-normal) line height keeps the pin's symmetric fractional half-leading baseline, so an explicit non-integer ratio can sit a subpixel from Chrome's floored ascent leading (the `span-ratio` Tier C drift, ~1.3 mean; documented in the tolerance comment — the same recorded divergence the textstyle fixtures carry); the standalone text-element paint path (no parent paragraph) still derives its baseline natively (`(L-a-d)/2+a`), which quantizes onto Chrome's row at DPR 2 for gap-less faces but can land one device row off elsewhere; the editor's caret default stays phi (documented); Tier C tolerances are provisional (research 19's calibration pending).

