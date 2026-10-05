# Route keyboard input through focus, actions and key contexts

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 05, 11, 12

## Question

A focused counter/editor reacts to physical keys, chords and actions using the same propagation rules as the reference.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [authoring decision round](../../../docs/authoring-decision-round.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement focus handles/paths/tab traversal/release and dispatch capture/bubble behavior, including default stop/propagate rules and typed listeners.
- [ ] Integrate key contexts, matchers, chord timeout/mismatch/replay and keymap JSON with canonical action services.
- [ ] Run accelerator handling before TranslateMessage and keep physical keyboard events separate from WM_CHAR text; test repeats, surrogate assembly, dead keys, AltGr and WM_INPUTLANGCHANGE/keyboard-layout-change callbacks with actual layouts where available.
- [ ] Exercise focus transfer across two windows, focus release during dispatch and listener cancellation without stale callbacks. Cover public keystroke and pending-input observers with the same scope/order rules.
- [ ] Record deterministic dispatch transcripts plus real host input evidence; unavailable layouts are blocked-environment rows rather than invented passes.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)
- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)
- [Register, serialize and bind typed actions](12-action-registry.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
