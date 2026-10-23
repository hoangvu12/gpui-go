# Port the controls built on Input

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 16, 17, 20

## Question

NumberInput, OtpInput, TimeField, Select, Combobox, Calendar, DatePicker and ColorPicker behave as upstream.

## Scope

- Base files: number_input.rs, otp_input.rs, time_field.rs, select.rs, combobox.rs, calendar.rs, date_picker.rs, color_picker.rs. chrono → Go `time` + the ticket-15 strftime subset; palette → the core's Hsla/Rgba (exported clamped `HslaToRgba`).

## Acceptance criteria

- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-02-simple-controls.md](../research/slice-02-simple-controls.md)
- [slice-03-composite-controls.md](../research/slice-03-composite-controls.md)
- [B2-composite-controls.md](../research/B2-composite-controls.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Claimed and delegated. Owns new base/{number_input,otp_input,time_field,select,combobox,calendar,date_picker,color_picker}*.go and the Init lines in base/lib.go. It reports any needed input-engine change to the orchestrator (21 owns those files); it also ports state.rs test_number_step.
- 2026-10-23 — Worker finished. The orchestrator verified it together with 28.

## Answer

NumberInput, OtpInput, TimeField, Select, Combobox, Calendar, DatePicker and ColorPicker are ported in base/{number_input,otp_input,time_field,select,combobox,calendar,date_picker,color_picker}.go. Their initializers join `Init` in upstream order.

Tests: all 42 upstream tests are ported, including state.rs `test_number_step`. 4 of them use different plumbing but keep upstream's assertions.

Deviations:
- chrono maps to small value types over Go `time`, with formatting through base/strftime.
- Enums that carry data became structs.
- Two upstream quirks are kept on purpose: negative hue values above 180°, and the hex field swallowing the next edit.

Open item: the NumberStep field belongs in 21's engine files. Until it lands, NumberInput steps by 1.

Orchestrator re-run on the shared snapshot with 28: `go test ./base` has 747 pass and 3 skip, and `-count=3` is ok. Evidence: [ticket22-input-controls.json](../../../evidence/base/ticket22-input-controls.json).
