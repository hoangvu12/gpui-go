# StyledText fails with an infinite wrap width inside a flex row

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: none

## Question

A non-scrollable TextView holding a fenced code block, inside a plain flex-row div, fails the whole frame: "StyledText shaping failed ... wrap width must be finite and >= 0". Found by ticket 41, and present before its changes. Under a flex row the available width is unconstrained (MinContent/MaxContent). The pin's StyledText measure passes no wrap width in that case (R:elements/text.rs measure). The port appears to pass +Inf.

## Acceptance criteria

- [ ] Pinned behaviour cited; a headless test reproduces the failure, then passes.
- [ ] gpui and `go test ./base -skip Real` green.

## Comments

- 2026-10-23 — Created by the orchestrator from ticket 41's report.
