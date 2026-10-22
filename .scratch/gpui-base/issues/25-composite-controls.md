# Port Tree, NavStack, Resizable and Questionnaire

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 17, 18, 22

## Question

Tree, NavStack, Resizable panels and Questionnaire behave as upstream (including resizable's exact feedback-frame split).

## Scope

- Base files: tree.rs, nav_stack.rs, resizable/*, questionnaire/*.

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-03-composite-controls.md](../research/slice-03-composite-controls.md)
- [B2-composite-controls.md](../research/B2-composite-controls.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Claimed and delegated. Owns new base/{tree,nav_stack,resizable*,questionnaire*}.go and the tree Init line in base/lib.go.
- 2026-10-23 — The worker hit the usage limit, was resumed after the reset, and finished. The orchestrator verified it on a snapshot.

## Answer

Tree, NavStack, Resizable panels and Questionnaire are ported in base/{tree,nav_stack,resizable*,questionnaire_*}.go. `treeInit` joins `Init`.

Tests: all 35 upstream tests are ported, with none deviated. The resizable drag split gives exactly [220, 180], with one `on_resize` call.

Deviations:
- `ResizableState.drawNotify` drops notifies raised while drawing. The pinned window drops them too; the Go core still delivers them, and that made the panels redraw forever.
- Tree `ListStyle` applies only width and height.
- Go shapes follow ticket 22's convention.

Orchestrator re-run on a snapshot of HEAD plus 25's files: vet, gofmt and build are clean, `go test ./base` has 862 pass and 3 skip, and `-count=2` is ok. Evidence: [ticket25-composite-controls.json](../../../evidence/base/ticket25-composite-controls.json).
