# Stand up the base package: theme, styled extensions, root and globals

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port orchestrator (wave 1d/2a)
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
