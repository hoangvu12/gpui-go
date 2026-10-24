# 19 - Core text selection on static text

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 9 (Claude Code)
Blocked by: 10
Triage: ready-for-agent

**What to build:** Static text can be selected with the mouse (press-drag, double-click word, triple-click line), the selection is painted with the platform highlight, and copying works through a core action. The selection model moves from Base into the core.

**Blocked by:** 10 (Hover: pointer enter/leave, window leave, survives typing)

**Status:** ready-for-agent

- [ ] Selection range fixtures for drag, double- and triple-click match Chrome's ranges on sample text.
- [ ] Base's text views keep working on top of the core model.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 9 (verification): merged 19687df as 40a1260. The conflicts were the fixture step validation (kept 19's click-count checks and 15's StepWheel) and the harness guide. Spot check: `HandleTripleClick` expands with `TextGranularity::kParagraph` (selection_controller.cc:1110-1155), which matches. The broad batch set passed with no failures on the merged tree (with 15, 22 and 35): ./gpui/, ./internal/parity/..., ./base, every internal/*spec package in the batch list, ./internal/tsxgen/, ./gpui/script/..., ./examples/... and ./cmd/.... Resolved.

## Answer

- **API (core, gpui):** the window selection model lives in `gpui/text_selection.go` (the whole U:text_selection.rs port: `TextSelectionHandle`, `TextSelectionLayer`, `TextSelectionRegistration`, snapshots/projection, scopes, touch, auto-scroll) plus `gpui.SelectableText` (`gpui/selectable_text.go:32`, paints the platform wash from `App.SelectionColors`, selection end at a line break paints to the last inked glyph); multi-click boundaries through a seam `gpui.SetTextSelectionBoundaries` / `SetTextSelectionWordTrailingWhitespace` (`gpui/text_boundary.go:90,109`) whose default is the browser pair — `chromeWordRangeAt` (UAX #29 Latin subset, `:293`) and `chromeParagraphRangeAt` (line + its break, `:337`); a drag after a multi-click extends by the stored granularity range (`updateGranularityEndpoints`, `gpui/text_selection.go:2066`); read-back for the harness `TextSelectionHandle.ProjectedRanges/ProjectedText` (`:838,853`); copy falls to the window selection when nothing editable is focused — the model's `EditActionHandler` stand-in wired into `defaultEditActions` (`gpui/edit_action.go:238`, `gpui/text_selection_copy.go:69,41`, through ticket 26's action path); `App.SetSelectionColors` pins the wash (`gpui/selection_colors.go:47`).
- **Moved from Base (Base now aliases/calls the core):** `base/text_selection.go` (2701-line model → re-export aliases), `base/auto_scroll.go`, `base/touch_selection.go`, `base/selectable_text.go` (→ `gpui/selectable_text.go`), `base/text_boundary.go` (now installs Base's upstream word/line pair + opts out of trailing whitespace over the seam), the selection-gesture fields of `base/global_state.go` (→ `gpui/text_selection_globals.go`; suppression/touch/document-order delegate); the model's tests moved with it (`gpui/text_selection_test.go` 45/45, `gpui/selectable_text_test.go`, `gpui/touch_selection_test.go`, `gpui/auto_scroll_test.go`); Base's TextView adapter now uses accessor methods + `WithSelfScroll`/`SetHitTestRuns`/`NextSelectionDocumentOrder`.
- **Fixtures:** `internal/parity/fixtures/selection` — `selection-drag` ([9,16) "a gamma"), `selection-word` ([4,9) "latte"), `selection-line` ([11,23) "second line\n"), `selection-word-drag` ([6,16) "beta gamma"), all matching Chrome's `window.getSelection()` recorded via `ComputedScript`; ranges compared Tier B-like by the package test plus the harness's Tier A/B/C; the replay's wash pinned to the record's screenshot-sampled colour (`Configure`); one deliberately-wrong view (no `TextSelectionLayer`: the range comparison and Tier C fail, Tier A/B pass). Harness: `Step.Clicks`/`MultiClick`/`DownWithClicks`/`UpWithClicks` (CDP `clickCount` ↔ event `ClickCount`, `internal/parity/fixture.go:124,176`, recorder/replay wired).
- **Key citations:** word granularity = ICU `createWordInstance` segments (text_break_iterator_icu.cc:637-692) with the boundary-side rule (position_units_word.cc:67-79, 269-281 — a caret on a boundary takes the next segment) and Windows' trailing whitespace (selection_controller.cc:187-209, 842-849); triple click = `HandleTripleClick` ExpandWithGranularity(kParagraph) (selection_controller.cc:1110-1155) including the paragraph break (selection_adjuster.cc:318-334); drag after multi-click extends by `Selection().Granularity()` (selection_controller.cc:316-375, 693-699); copy with nothing editable focused = ExecuteCopy on the frame selection (clipboard_commands.cc:321-371).
- **Limitations:** CJK/emoji use the un-tailored UAX #29 result (each Han/kana character and each emoji its own segment — no ICU dictionary tailoring; Go has no UAX #29 segmenter, so the Latin subset is hand-rolled); the backward granularity drag keeps the stored anchor range only within one participant; select-all on static text is not wired (copy only, per the ticket); the fixture pixels inherit ticket 28's recorded divergence (selected text keeps its own colour over the translucent wash where Chrome paints HighlightText) and headless Chrome's wash is the embedder default (#1967D2's blend, renderer_preferences.h:26), not the OS palette — the fixture pins the recorded wash.
