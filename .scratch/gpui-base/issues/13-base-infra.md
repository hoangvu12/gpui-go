# Stand up the base package: theme, styled extensions, root and globals

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 05, 06, 09, 11

## Question

`github.com/hoangvu12/gpui-go/base` exists with `Init`, Theme/tokens, StyledExt (`HFlex`/`VFlex`, refine_style), StateStyle, ElementExt/event extensions, observe/test_support, measure, component traits, actions, geometry, IndexPath, ListSettings, GlobalState, reduce_motion (SystemParametersInfoW via syscall), Root/RootPlugin, History and UndoHistory.

## Scope

- Base files: lib.rs, theme.rs, theme_tokens.rs, styled.rs, state_style.rs, element_ext.rs, event.rs, observe.rs, test_support.rs, measure.rs, component_traits.rs, actions.rs, geometry.rs, index_path.rs, list_settings.rs, global_state.rs, reduce_motion.rs, root.rs, history.rs, undo_history.rs, async_util.rs. macos_accessibility.rs is not applicable on Windows (recorded).

## Acceptance criteria

- [ ] Package layout decision recorded (one `base` package mirroring the crate, pure data structures in subpackages).
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md](../research/slice-01-infra-overlays.md)
- [A-infra-overlays.md](../research/A-infra-overlays.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Reopened: the 2026-10-19 worker died with its session and left nothing on disk (status set back to open). Re-claimed the same day by the base-port-2 orchestrator (worktree gpui-go-wt-base, branch base-port-2) and delegated.

## Answer

Resolved 2026-10-22 on `base-port-2`. Package `github.com/hoangvu12/gpui-go/base` (one package mirroring the crate; data structures in subpackages per ticket 15; naming rules in `base/doc.go`: `T::f` → `TF`, `new` → `NewT`, extension traits → wrappers such as `StyledExtOf(gpui.Div()).HFlex()`). Covers lib, theme, theme_tokens (upstream shadcn token names), styled, state_style, element_ext, event, observe, measure, component_traits, actions, geometry, index_path, list_settings, global_state, reduce_motion (SystemParametersInfoW via syscall), root + plugins, history, undo_history.

Tests: 59 (46 upstream ported, 4 of them adapted; 13 Go-only), orchestrator rerun on an index snapshot: all pass, vet/gofmt clean. Not applicable: macos_accessibility.rs, async_util.rs (Go chan), test_support.rs (Kit-only consumer).

Deviations and hand-offs: see [evidence](../../../evidence/base/ticket13-base-infra.json). Main one: Base's crates.io gpui-ce 0.2.2 defaults divs to `display: Block`, our core pin to Flex (map gap 2b); the styled cross-axis test is adapted. Hand-offs: Theme.plot → 14/31; Root Copy + TextSelectionLayer → 20/29; activeFocusTrap → 16; scrollbar values → 18; text_view_state_stack → 28; a gpui `IsTestScheduler` helper would replace the reduce-motion test-scheduler guess.
