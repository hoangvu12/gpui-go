# StyledText fails with an infinite wrap width inside a flex row

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: none

## Question

A non-scrollable TextView holding a fenced code block, inside a plain flex-row div, fails the whole frame: "StyledText shaping failed ... wrap width must be finite and >= 0". Found by ticket 41, and present before its changes. Under a flex row the available width is unconstrained (MinContent/MaxContent). The pin's StyledText measure passes no wrap width in that case (R:elements/text.rs measure). The port appears to pass +Inf.

## Acceptance criteria

- [ ] Pinned behaviour cited; a headless test reproduces the failure, then passes.
- [ ] gpui and `go test ./base -skip Real` green.

## Comments

- 2026-10-23 — Created by the orchestrator from ticket 41's report.
- 2026-10-23 — Delegated and resolved. The cause was a negative width, not an infinite one.

## Answer

The wrap width was -24, not +Inf. In a flex row's min-content pass, taffy gives a padded box's text a width of 0 minus the padding, without clamping (flexbox.rs:474). The pin passes that straight through to parley. The port's native service rejected it, which failed the frame.

`gpui/text.go` now raises a negative width to 0 before the native call, which behaves identically in parley. NaN and Inf are still rejected.

Two headless tests reproduce the failure and pass with the fix. Evidence: [ticket43-styledtext-negative-wrap.json](../../../evidence/base/ticket43-styledtext-negative-wrap.json).
