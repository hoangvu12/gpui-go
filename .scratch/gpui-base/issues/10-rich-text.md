# Paint styled text runs and provide the text APIs Base was written against

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 02, 08

## Question

Text with per-run color, background, underline and strikethrough paints correctly, and Base's text calls (`shape_line`, `x_for_index`, `LineWrapper`, `StyledText`/`TextLayout`) have Go equivalents over the existing Parley-backed `ShapeText`.

## Scope

- **Retargeting fact:** Base builds against crates.io `gpui-ce 0.2.2` (`ce/Cargo.lock:3057-3060`), which keeps the legacy Zed text API that our pin 254b5dbd removed in its Parley rewrite (same version string). This corrects [the baseline](../research/00-baseline.md) point 1 for the text system.
- Port: `TextRun` paint fields, `HighlightStyle` + `combine_highlights` (R:style.rs:751,1088,1101,1236); styled paint per ticket 02's decision; `StyledText` + `TextLayout` (R:elements/text.rs:391-1150); a legacy facade — `ShapedLine` x↔index/paint/paint_background and `LineWrapper`/`LineFragment`/`Boundary`/`layout_width` (semantics from the older gpui-ce checkout `~/.cargo/git/checkouts/gpui-ce-*/beb5a6d`, text_system/line_layout.rs:58-105, line_wrapper.rs) built on single-char shaping with a cache; window text-system access (R:window.rs:2373).

## Acceptance criteria

- [ ] Multi-run paint transcripts; facade geometry agrees with `WrappedLine.Caret/HitTest`; recorded deviation for cross-boundary ligatures and strikethrough offset.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-05-input-editor.md (headline + §1-3)](../research/slice-05-input-editor.md)
- [slice-06-text-selection.md §0.1,2.2-2.4](../research/slice-06-text-selection.md)
- [D-input-editor.md](../research/D-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Reopened: the 2026-10-19 worker died with its session and left nothing on disk (status set back to open). Re-claimed the same day by the base-port-2 orchestrator (worktree gpui-go-wt-base, branch base-port-2) and delegated.

## Answer

Resolved 2026-10-22 on `base-port-2`. `TextRun` gains `Color`/`BackgroundColor`/`Underline`/`Strikethrough` (kept out of the layout cache key; geometry unchanged); `HighlightStyle` + `CombineHighlights`; per-run paint clipped to each run's selection rects (decision 02); `StyledText` + `TextLayout`; the legacy facade `Window.TextSystem()` → `ShapeLine` / `LayoutWidth` / `LineWrapper`, `ShapedLine` x↔index + paint/paint_background, `LineFragment`/`Boundary`.

Tests: 18 added (4 upstream ported, 4 adapted, 24 not applicable with reasons); the real-window transcript checks per-run sprite colors and clips, decorations, a Calibri ligature across a color boundary, and `ShapedLine` vs `WrappedLine.Caret/HitTest` for every character. Orchestrator snapshot rerun: richtextspec, gpui, textspec, glyphspec, inlinespec, scenespec, authorspec, layoutspec all green.

Deviations ([evidence](../../../evidence/base/ticket10-rich-text.json)): a ligature across a color boundary shows both colors split inside the glyph, not one color as decision 02 expected; strikethrough/underline use the core's fallback offsets (`line.rs:437,448`); `ShapeLine` rejects a force width (tickets 21/23 re-express the line-number measurement); truncation and text transform not ported. Follow-up: [35](35-inline-styled-runs.md) for per-run colors inside inline paragraphs.
