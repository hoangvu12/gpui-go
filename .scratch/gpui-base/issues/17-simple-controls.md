# Port the button family and simple controls

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-22)
Blocked by: 08, 12, 13, 14

## Question

Button, Toggle, Checkbox, Radio, RadioGroup, Switch, ToggleGroup, Link, Tabs, Toolbar, Accordion, Collapsible, Avatar, Progress, Slider, Pagination and Table render, respond and expose a11y as upstream.

## Scope

- Base files: button.rs, toggle.rs, checkbox.rs, radio.rs, radio_group.rs, switch.rs, toggle_group.rs, link.rs, tabs.rs, toolbar.rs, accordion.rs, collapsible.rs, avatar.rs, progress.rs, slider.rs, pagination.rs, table.rs.

## Acceptance criteria

- [ ] Button lands first as the pattern the rest copy.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-02-simple-controls.md](../research/slice-02-simple-controls.md)
- [B1-controls.md](../research/B1-controls.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-22 — Claimed by the base-port-2 orchestrator and delegated.
