# Port the overlay components

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 08, 12, 14

## Question

FocusTrap, Positioner, Popup, Popover, Tooltip, HoverCard, Dialog, AlertDialog, Sheet and Toast work in real windows with their upstream timing, dismissal and focus behavior.

## Scope

- Base files: focus_trap.rs, positioner.rs (15 pure tests), popup.rs, popover.rs, tooltip.rs, hover_card.rs, dialog.rs, alert_dialog.rs, sheet.rs, toast.rs.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md](../research/slice-01-infra-overlays.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated.

## Answer

Resolved 2026-10-22. Focus trap, positioner, popup, popover, tooltip, hover card, dialog, alert dialog, sheet and toast in the root `base` package; Root's focus-trap placeholder replaced by the real lookup; `base.Init` runs the dialog/focus_trap/popover/sheet initializers. All 53 upstream tests ported (0 deviated) + 4 Go-only; 292 base tests pass on the orchestrator's snapshot rerun. Gap 2b: the Block() convention applied to every plain upstream div. Deviations (window decorations assumed server-side, keyed-state focus handles instead of per-render handles, registration-order trap lookup): [evidence](../../../evidence/base/ticket16-overlays.json).
