# 43 - Text wrapping options and alignment

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 11 (Claude Code); worker chat 56dea239; R16 integration branch native/r16
Blocked by: 39
Triage: ready-for-agent

**What to build:** word-break, overflow-wrap and text-wrap: balance work, and text aligns with justify, start and end.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [ ] Fixtures for long URLs, CJK text and balanced headings match Chrome's line breaks.
- [ ] Justified paragraphs match Chrome within tolerance.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-26, orchestrator 11: verified on `native/r16` (8402b74..c288e90); it reaches main with ticket 54. Clean tree, no artifacts committed. The test DLL `cb8abb65…` matches the shared target's build output. With the manifest swapped in for the test run: `go vet ./...` clean; the broad batch set plus the native spec packages gives 74 packages ok, failing only portfixture TestFx0004 (known), the 7 internal/native loader tests that fail by construction under the swap, and clipspec (clipboard contention; ok when re-run alone). Spot-check: text_break_iterator.cc:125-149 at 0d4dcd4f (`ShouldBreakAfterBreakAll` uses the break-all class table) matches. Accepted deviations: a break-all document drops the Chromium line-break override; the inline paragraph assembler doesn't balance; Parley's keep-all breaks after U+3005.

- 2026-10-26, orchestrator 11 (the approach, chosen by the orchestrator as the handoff asked): fold into the rev 16 rebuild on `native/r16`. Vendor the pinned adapter's paragraph layout (`gpui_ce_parley` `parley_paragraph_layout`) into reference/native the way R15 vendored the shaders, keeping the ONE FontContext (FontId provenance check), with a side-channel request entry so the pinned gpui types stay untouched; drive word-break, overflow-wrap and justify/start/end through it. text-wrap: balance stays Go-side bisection (the rev-15 record's contract). Locale and font width/variations are out of this ticket's acceptance.

- 2026-10-24, orchestrator 6: rev 15 (39) carries the text-run fields and the measure baseline through the ABI, but they do not drive shaping or alignment yet. The pinned Parley TextLayoutRequest and Taffy leaf compute have no slots for them; see TEXT_ABI.md and LAYOUT_ABI.md. This ticket needs a follow-up native change, and so a rebuild that needs the user's approval, before its behaviour can match Chrome. Grid track lists, justify-items/self and flow-root do work in rev 15.

## Answer

Landed on `native/r16` in three commits (the vendored adapter + the rev-16
fields, the Go styling layer, the fixtures) plus this answer. **Text
service v3** (run record 92 → 96 bytes — the `text_align` tag; layout
record 56 → 60 bytes — `is_rtl`); native revision stays **151** (ticket 54
sets 16). Interim-bundle DLL SHA-256
`cb8abb653c88445e04115258a36fdff06e8df168bab8011a1df41822b9513b71`,
19,472,896 bytes, static CRT, mask `0xFFF`; the embedded artifact stays
at the rev-15.1 pins until 54 (the manifest swap ran for the tests only,
reverted before every commit).

### What landed

**NATIVE — the vendored Parley adapter.**
`reference/native/src/text_vendored/` is a byte-copy of the pinned
`gpui_ce_parley` implementation (`text_system.rs` — the
`ParleyTextSystem`, the paragraph layout, the `PlatformTextLayout`
impls; `store.rs` — the `FontStore` interning; `catalog.rs` — the
`FontCatalog` resolve; `paragraphs.rs` — the paragraph splitting), with
the import paths rewritten and the divergences recorded in
`text_vendored/mod.rs` (the pinned crate stays the dependency that owns
the public types — `SystemFonts`, the `GlyphRasterizer` trait
`crate::glyph` implements, `RasterFace` — so the DirectWrite rasterizer
plugs in unchanged). The service constructs the VENDORED system with
the pinned construction path, and the glyph service rasterizes through
the SAME stack instance, so the process keeps ONE FontContext/FontStore
— the FontIds the text service reports are the FontIds the glyph
service rasterizes.
`text::tests::vendored_font_ids_match_the_pinned_adapter` proves it
further: a fresh pinned system and a fresh vendored system resolve the
same descriptor sequence to the same FontIds, and the same document
(including fallback-discovering non-Latin text) shapes to the same
fragment FontIds and the same glyph stream.

**NATIVE — the side-channel.** `layout_text_with_browser(TextLayoutRequest,
BrowserLayoutParams)` reaches the paragraph layout without changing the
pinned gpui types: per-run `word_break`/`overflow_wrap` become Parley
range styles (`StyleProperty::WordBreak`/`OverflowWrap`, pushed only
when they differ from the default, so a field-free request is
byte-identical to the pinned `layout_text` — the provenance test
compares the glyph streams), and the document-level `text_align` tag
(rev 15.1's any-run rule, like text-wrap: nowrap) drives Parley's
`Alignment::Start`/`End`/`Justify`. `is_rtl` (the `paragraphs[0]` base
direction) returns in the extended result and is reported by
`text_layout_info` so the Go paint layer resolves start/end against it.
One recorded behaviour divergence (mod.rs divergence 6): a document
carrying `WordBreak::BreakAll` DROPS the `CHROMIUM_LINE_BREAK_OVERRIDE`
— parley's override is un-scoped and applied after the segmenter, and
its ASCII table sets every printable-ASCII pair to no-break, so
break-all could never break ASCII text under it; Chrome's own break-all
overrides its fast pair table with the break-all class table, which
allows AL×AL breaks (`ShouldBreakAfterBreakAll`,
`platform/text/text_break_iterator.cc:125-149,328-336` at 0d4dcd4f).
The recorded cost: a mixed normal+break-all document loses the
override's punctuation rules for its normal runs.

**GO — the styling layer.** `TextStyle.WordBreak`/`OverflowWrap`/
`TextWrap` ride the runs (`ToRunAt`) so every shaping path carries them
(the text leaf, StyledText, the inline paragraph's segments — through
the collector's `paintRuns`, the collapse remap and the segment
slicing, all fixed to keep the fields), and `TextLayoutRequest.Align`
stamps the native tag on every run. The paint layer resolves start/end
against the shaped summary's new `IsRTL` (`resolvedTextAlign`) and
paints justify from the line origin (the stretch is native; the paint
layer's bounds widen to the alignment width). The inline paragraph: a
pure-text single-segment paragraph carries the native align (justify
stretches each row's piece — the ticket's "justified paragraph" case);
mixed documents keep the Go row geometry, now resolving start/end
against the first piece's direction. `text-wrap: balance` is the
Go-side bisection over the shaping handle's relayouts
(`gpui/text_balance.go`) — Chromium's
`ParagraphLineBreaker::AttemptParagraphBalancing`
(`paragraph_line_breaker.cc` at 0d4dcd4f): the bisection for the
narrowest width producing `num_lines` (`BisectAvailableWidth:119-145,
220-230`), the single-line guard (:188-190), `kMaxLinesForBalance = 6`
(`score_line_break_context.h:17`), the 1px epsilon (:199-202) and the
0.8 × average lower bound (:226-228). Tailwind: `break-all`,
`break-keep`, `break-words`, `wrap-break-word`, `wrap-anywhere`,
`text-justify`, `text-start`, `text-end`, `text-balance`
(`class_generated.go` and the TS surface regenerated; the
unsupported-class table narrowed to break-normal/hyphens/text-pretty).
The shaping cache key gains the align + balance modes and the runs'
browser fields.

### Fixtures and results

`internal/parity/fixtures/textwrap` (registered in `replay_test.go` and
`cmd/parityrecord`), recorded in headless Chrome 155.0.8059.85:

| fixture | gates | Tier A | Tier C (measured / tolerance) |
|---|---|---|---|
| textwrap-url | the long URL under overflow-wrap anywhere/break-word and word-break break-all | the line counts (row heights): normal 2 rows, anywhere/break-word 3, break-all 2 | whole 0.198/0.4, worst ROI span-url 0.701/1.5, hot 0.0036/0.008 |
| textwrap-cjk | 25 ideographs, normal vs keep-all (Microsoft YaHei both sides) | keep-all stays 1 overflow row, normal wraps to 2 | whole 0.068/0.2, worst ROI span-cjk-keepall 0.866/1.8, hot ≤ 0.0002/0.006 |
| textwrap-balance | the balanced heading (the text-leaf path) vs the greedy control | same line count — Tier C only | whole 0.053/0.2, worst ROI span-balanced 0.423/0.9, hot 0.0007/0.006 |
| textwrap-justify | the justified paragraph (the paragraph path AND the leaf path) plus a left control | same line count — Tier C only | whole 0.119/0.25, worst ROI span-left 0.612/1.3, hot 0.0014/0.006 |

Every fixture has a wrong-view test that must fail (the pre-ticket
views: the classes dropped, balance dropped, justify dropped —
`textwrap_test.go`), and `TestPixelTolerancesKeepHeadroom` pins each
tolerance at ≥ 2× the measured correct-view statistics. The existing
fixtures are unchanged (all 42 previous parity packages green).

### Verification

- `cargo test --release -p gpui-go-native --target x86_64-pc-windows-msvc
  --lib`: **127 passed**, 2 failed — both the known pre-existing
  (`layout::style_translation`, `text::font_add`).
- The full required set with the bundle + manifest swap
  (`go test -p 1 -count=1` over internal/native/…, scenespec,
  rendererspec, conformance/…, portfixture, parity/…, gpui,
  gpui/script/…, glyphspec, textspec, layoutspec, fontspec,
  textframespec, inlinespec, richtextspec, tsxgen): **59 packages ok**,
  failing only the 7 internal/native loader tests that fail by
  construction under the swap and the known portfixture TestFx0004.
- `go vet ./...` clean; `gofmt` clean on the touched files;
  `go build ./...` clean.

### Deviations / recorded gaps

- **The break-all override drop** (mod.rs divergence 6, cited above) —
  required to make break-all work at all under parley's un-scoped
  Chromium override.
- **Parley's keep-all breaks after U+3005** (the ideographic iteration
  mark) where Chrome keeps the run unbroken — the CJK fixture's text
  avoids the character; the divergence is recorded in the fixture's
  comment and the harness doc.
- **The port always bisects for balance** — Chrome's primary path is the
  newer ScoreLineBreaker (inline_layout_algorithm.cc's LineBreakStrategy)
  with the bisection as its fallback; the rev-15 record's contract is the
  bisection, and the balanced fixture records whichever Chrome chose (it
  matched).
- **The inline paragraph assembler does not balance** — its row model
  re-shapes suffixes per row, so a paragraph-level bisection needs a
  restructure; the balanced-heading fixture uses the text-leaf path and
  the gap is recorded (the paragraph path carries word-break,
  overflow-wrap and justify).
- **The CJK text's sub-pixel raster drift** measures ~2× the Latin
  fixtures' (denser glyphs, accumulated sub-pixel pen drift) — the
  tolerances are calibrated at 2× the measured values, like every
  fixture's.

### Still blocked (recorded, not fixed)

- **locale** (an upstream parley language feature first — unchanged from
  the rev-15.1 record).
- **font width / variation settings / optical size**: the vendored store
  is reachable now, but the resolve path still hardcodes `FontWidth::NORMAL`
  and passes no variation coordinates; the smallest plan (a vendored
  resolve entry interning the requested instance through the same store)
  is 6–10 h, folded into a later ticket. TEXT_ABI.md carries the record.
- **Grid baselines** (ticket 42) and the measure baseline (Taffy's
  `Point::NONE` — LAYOUT_ABI.md, revisit with the R17 retained-layout ABI).
