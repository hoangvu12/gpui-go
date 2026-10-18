# Paint styled text runs and provide the text APIs Base was written against

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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
