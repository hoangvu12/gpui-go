# Decide how Base handles the frozen-artifact gaps

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Triage: ready-for-human
Status: resolved
Assignee: user (via the Base port chat)
Blocked by: none

## Question

Three capabilities Base needs are limited by the frozen rev-11 DLL. Accept the Go-side fallbacks (with recorded deviations), or schedule a native revision?

## Scope

- **A11y (hard ABI limit):** the native decoder accepts 8 role tags (`reference/native/src/a11y.rs:598-612`) and its node record has no toggled/selected/expanded/numeric/level/set/row/col state (`internal/native/a11y.go:84-127`); Base needs ~25 more roles. Proposed fallback: full semantics in the Go snapshot (what upstream tests check), publish unsupported roles as the nearest supported role/GenericContainer instead of failing the whole publish (`gpui/a11yprovider.go:545-566,673-676`).
- **Per-run text paint styles:** the native run/fragment/glyph records carry no color/decoration/byte index (`reference/native/src/text.rs:327,518-553,1099-1109`). Proposed fallback: shape once, paint per run clipped by `SelectionRects`; a ligature spanning a color boundary takes one color; strikethrough offset derived (no native strikeout metric).
- **Gradient/pattern backgrounds:** the scene color record accepts tag 0 (solid) only (`reference/native/src/scene.rs:164-181,2506`). Base itself only mentions `linear_gradient` in a doc comment (`plot/shape/bar.rs:145`). Proposed: keep `Background` solid-only, documented.

## Acceptance criteria

- [x] The user's choice per gap is recorded; tickets 10 and 11 follow it.

## Research

- [slice-02-simple-controls.md](../research/slice-02-simple-controls.md)
- [slice-03-composite-controls.md](../research/slice-03-composite-controls.md)
- [slice-06-text-selection.md](../research/slice-06-text-selection.md)
- [slice-05-input-editor.md](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).

## Answer

Resolved 2026-10-19 — the user accepted the presented plan ("sounds good then"), which proposed the Go-side fallbacks below; no native revision.

- **A11y:** full roles/states live in the Go snapshot (what upstream tests read); publication maps roles the rev-11 ABI lacks to the nearest supported role (else GenericContainer) and drops unrepresentable states instead of failing the publish. Recorded as a screen-reader fidelity deviation per affected component.
- **Per-run text paint:** shape once, paint each run clipped to its `SelectionRects`; a ligature spanning a color boundary takes one color; strikethrough offset derived from font metrics. Recorded deviation.
- **Gradients/patterns:** `Background` stays solid-only; documented.
