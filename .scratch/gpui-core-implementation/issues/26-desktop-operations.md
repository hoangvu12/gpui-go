# Implement remaining Windows app and desktop operations

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 05, 13

## Question

A Go app uses the reference window/app controls and receives desktop settings, power and lifecycle changes.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement the per-symbol ledger's remaining menus/jump lists, cursor/window controls, displays, URL/path/reveal, notifications, restart and related app callbacks where applicable.
- [ ] Match settings/theme/power/display changes, suspend/resume, minimize/restore and window/app activation semantics through the foreground dispatcher. Test restart's deferred launch/reentry path, cursor hide-until-move and character-palette modifier release/restore with partial-send cleanup.
- [ ] Reproduce verified Windows unsupported/no-op outcomes including native-popup rejection/fallback, auxiliary-executable lookup, hide/unhide-other-apps and nominal thermal-state behavior, rather than returning fake success for missing work.
- [ ] Cover bounds/activation/appearance/button-layout observers and set_background_appearance for opaque, blurred and transparent modes; verify both DWM behavior and the renderer clear path under the same mode.
- [ ] Trace every remaining Platform/PlatformWindow row to a test or a source-supported unavailable outcome; route any unexpectedly large capability to an explicit bounded follow-up before resolving this ticket.
- [ ] Exercise real operations using temporary local resources and controlled events; external launches, restart or user-visible system changes run only when authorized for implementation validation.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)
- [Route keyboard input through focus, actions and key contexts](13-focus-keyboard.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
