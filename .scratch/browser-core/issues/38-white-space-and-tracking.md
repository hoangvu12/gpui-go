# 38 - White-space collapse, tab size and em-based letter spacing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** white-space normal, nowrap and pre-line collapse whitespace like CSS with a correct caret byte map, tabs are 8 spaces under pre, and em letter spacing works.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures for each white-space value match Chrome's boxes and line breaks.
- [x] Caret positions map correctly into collapsed text (text geometry test).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **Behaviour:** `white-space` gained `pre`, `pre-line` and `pre-wrap` next to `normal`/`nowrap`, and the element layer now folds white space like CSS before shaping, keeping a source↔display byte map: normal/nowrap/pre-line collapse runs of spaces, tabs, newlines and carriage returns to one space (removed at a paragraph's start, after a preserved newline, and at its end), across inline element boundaries; pre-line preserves newlines as forced breaks; pre/pre-wrap preserve everything, with tabs at the browser's 8-space stops. An em-based letter spacing (`LetterSpacingEm`, Tailwind's named `tracking-*`) resolves against the element's own font size. All measured paths collapse (the Text leaf, the inline paragraph, StyledText); the editor and Base's line wrapper never do.
- **Files:**
	- `gpui/white_space.go` — the collapse engine (the mode matrix, the collapser state machine, the `collapseMap` byte map with Chromium's OffsetMapping conversion semantics, the run remap, `collapseParagraphDocument`).
	- `gpui/div.go:62-80` (the WhiteSpace values), `gpui/div.go:617-719` (the Text leaf: the wrap predicate, the collapse before truncation, the tab-size request, the empty no-line-box case), `gpui/div.go:130-140,300-312` (`LetterSpacingEm`, `resolveLetterSpacing`).
	- `gpui/styled_text.go:262-345` (the same fold + the byte map in `textLayoutInner`), `gpui/styled_text.go:452-533` (`PositionForIndex`/`IndexForPosition`/`Len`/`Text`/`WrappedText` answer in source offsets), `gpui/text_run.go:217-244` (`ToRunAt`).
	- `gpui/inline.go:209-226` (the per-record mode on `InlineTextStyle`), `gpui/inline.go:706-712` (`finishParagraph` collapses the collected document, remapping runs, style ranges, boxes and spans), `gpui/inline_real.go:174-207,467-486` (per-segment tab size).
	- `gpui/text.go:667-687` (`TextLayoutRequest.TabSizeSpaces`), `gpui/text_tabs.go:44-92,183-262` (the browser tab stops: 8 × the tab's own shaped advance, `Font::TabWidth`'s half-space minimum), `gpui/text_cache.go:144-160` (the tab mode in the shape-cache key).
	- `gpui/styled.go:382-403,436-446` (`WhitespacePre/PreLine/PreWrap`, `LetterSpacingEm`), `gpui/class.go:899-921` (the named `tracking-*` classes), `gpui/class_generated.go` (the `whitespace-*` method classes, regenerated).
	- `base/text_node_render.go:63-76` (TextView pins pre-wrap — its verbatim rich text), `base/text_inline_flow.go:370-378` (a pre-wrapped fragment is `pre`: verbatim, no soft wrap), `base/text_inline_flow.go:284-286` (the flow's wrap gate).
	- `gpui/white_space_test.go` — the collapser tables for every mode, the cross-record and box cases, the byte-map conversions, the StyledText caret-byte-map geometry test, the empty paragraph, the real-system 8-space tab-stop test, the em resolution.
- **Fixtures:** `internal/parity/fixtures/whitespace/` — `whitespace-collapse` (the paragraph/text-leaf/StyledText paths under normal, the cross-boundary collapse of two inline spans, pre-line, the emptied paragraph) and `whitespace-pre` (tabs under pre, pre-wrap's wrapped rows, 0.1em tracking), recorded in Chrome 155.0.8059.85, registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`; one wrong-view test (`whitespace_test.go`, the everything-`pre` view = the pre-ticket pin, fails Tier A and C). `docs/browser-parity-harness.md` carries the ticket entry.
- **Key citations:** `core/css/white_space.h:47-108` (the mode matrix), `core/layout/inline/inline_items_builder.cc:798-965` (AppendCollapseWhitespace), `:1150-1175` (AppendPreserveNewline), `:1624-1638` (ExitBlock's trailing removal), `:218-228` (opaque atomic boxes), `core/layout/inline/offset_mapping.cc:156-198,506-560` (the offset conversions), `core/layout/inline/inline_layout_algorithm.cc:1488-1492` (the empty line's self-collapsing block), `platform/text/tab_size.h:34-38` + `platform/fonts/font.cc:319-337` (the 8-space stops and the half-space minimum), `core/css/css_properties.json5:6207-6210` (tab-size's default `TabSize(8)`), `core/css/resolver/style_builder_converter.cc:2821-2830` (the em letter-spacing conversion).
- **Limitations:** the segment-break removal rules (ZWSP-adjacent / East-Asian) are not ported (a run containing a newline keeps its one space); the generated break opportunity at a nowrap→wrap boundary is dropped; a mixed-mode inline paragraph can still soft-wrap inside a `pre` span's text; Base's flow measures its fragments with the pin's DirectWrite tab stops while they render at the browser's 8-space stops (tabs inside Base rich text can overrun their measured box — no test pins it); a lone preserved newline keeps the shaper's two rows where Chrome drops the trailing empty line; `StyledText.Text()` answers the source text where the pin answered the truncated display text (the truncation is pinned by the painted text now — see the divergence notes in `gpui/text_truncate_test.go`); form feeds are regular characters in the collapsing modes (Blink's reading).
