# Route keyboard input through focus, actions and key contexts

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer 386673cc (successor chat; orchestrator after worker aborts)
Blocked by: 05, 11, 12

## Question

A focused counter/editor reacts to physical keys, chords and actions using the same propagation rules as the reference.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [authoring decision round](../../../docs/authoring-decision-round.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Implement focus handles/paths/tab traversal/release and dispatch capture/bubble behavior, including default stop/propagate rules and typed listeners.
- [x] Integrate key contexts, matchers, chord timeout/mismatch/replay and keymap JSON with canonical action services.
- [x] Run accelerator handling before TranslateMessage and keep physical keyboard events separate from WM_CHAR text; test repeats, surrogate assembly, dead keys, AltGr and WM_INPUTLANGCHANGE/keyboard-layout-change callbacks with actual layouts where available.
- [x] Exercise focus transfer across two windows, focus release during dispatch and listener cancellation without stale callbacks. Cover public keystroke and pending-input observers with the same scope/order rules.
- [x] Record deterministic dispatch transcripts plus real host input evidence; unavailable layouts are blocked-environment rows rather than invented passes.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)
- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)
- [Register, serialize and bind typed actions](12-action-registry.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

## Answer

Resolved 2026-10-06 by the successor chat (worker aborted twice on connection errors; the orchestrator finished it). Keystroke vocabulary, key contexts/predicates, the keymap with the full precedence algorithm and JSON loading, the dispatch tree with chord/replay/timeout semantics, the focus runtime with tab traversal and logical release, the input-handler seam, div interactivity wiring (context/focus/listeners/tab groups), deferred action dispatch through the focus tree (replacing the ticket12 stub), and the pinned gpui_windows keyboard path (process_key dead-key/AltGr logic, accelerator pre-dispatch before TranslateMessage, WM_CHAR surrogate assembly, WM_INPUTLANGCHANGE layout reports, the WindowsKeyboardMapper). Deterministic transcripts in internal/focusspec (10 tests incl. two-window isolation, in-dispatch focus release, tab traversal), real-host input evidence in focus_real_test.go (real windows, posted keys, surrogate text, real layout report), and the reference keymap/context unit tests ported. Dead-key/AltGr real-interaction verification is a blocked-environment row (PostMessage cannot fake keyboard state; details in the evidence). Executed evidence: [evidence/ticket13-focus-keyboard.json](../../../evidence/ticket13-focus-keyboard.json).
