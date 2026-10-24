# 22 - Close watchers and Escape

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 9 (Claude Code)
Blocked by: 09, 21
Triage: ready-for-agent

**What to build:** Escape not handled by a listener signals the newest close watcher, so the newest overlay closes regardless of where focus is.

**Blocked by:** 09 (Default actions run after key listeners), 21 (Top layer and light dismiss)

**Status:** ready-for-agent

- [x] Fixtures: stacked overlays close newest-first on Escape; a tooltip-like hint closes with Escape.
- [x] PreventDefault in a key listener stops the close.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-25, worker (core/22-35-escape-fixed): implemented test-first, headless only, one commit. The fixture records surfaced two harness facts (both now documented in docs/browser-parity-harness.md): the recorder's CDP key commands do not pump the renderer's task loop (mouse commands do), so key-driven popover toggles stay queued in Chrome and coalesce — the closewatcher fixtures insert listener-less pointer moves as task-loop pumps so both sides deliver the toggles at the same positions; and the port's occluding popover hitbox cuts the mouse propagation chain at the popover, so the fixtures narrow the root to `data-events="keydown"` (the DOM's body would see the bubbled mouse events — recorded as a divergence for ticket 12's follow-ups). Status left as claimed for the orchestrator's verification.

- 2026-10-24, orchestrator 9 (verification): merged cf2b1ac+175b083 as e845d10, together with 35. The conflicts were only in the harness guide and class_generated.go (kept both sides, then regenerated). Spot check: `WatcherStack::EscapeKeyHandler` → `Signal()` (only on an undefaulted, trusted Escape with an enabled watcher) and `RequestClose`/`CancelEventCanBeCancelable` match close_watcher.cc. In this checkout they sit at :154-173 and :243-254, about 40 lines before the worker's cited numbers. On the merged tree, closewatcher-hint and closewatcher-prevented failed Tier C because the branch predates 48's default focus ring; 9c6aef8 adds `.OutlineNone()` to the invokers. The touched packages and ./internal/parity/... then pass. Resolved.

## Answer

- **API:** `gpui.NewCloseWatcher(w)` — the DOM CloseWatcher (`CloseWatcher`, `RequestClose() bool`, `Close()`, `Destroy()`, `IsClosed()`, `OnCancel(func(*CloseWatcherEvent, *Window, *App))` with `CloseWatcherEvent.PreventDefault/DefaultPrevented/Cancelable`, `OnClose(func(*Window, *App))`, `gpui/closewatcher.go`). The window's watcher stack signals the NEWEST group newest-first, stopping at a prevented cancel; the user-activation grouping gives a watcher created after an interaction its own group (nested popovers close newest-first on repeated Escape) while a programmatic one joins the newest group.
- **Files:** `gpui/closewatcher.go` (the stack, the signal, the grouping, the public API); `gpui/key_default_action.go:200-215` + `:219-232` (`defaultEscapeCloseWatchers` — the Escape arm in `runKeyDefaultActions`' keydown chain, after Tab and before the navigation keys, never consuming the key); `gpui/interact.go:484-493` (`beginPlatformInputDispatch` notifies the stack on every key down / mouse press — `Frame::NotifyUserActivationInFrame` frame.cc:1041-1055); `gpui/toplayer.go` (`overlayRecord.watcher`: `Show` creates it, `hide` destroys it, the first registration corrects for the manual kind, the reconciliation drop destroys it — the watcher's close listener hides the entry); `gpui/window.go:319-325` (`w.closeWatchers`); `gpui/closewatcher_test.go` (10 unit tests); `internal/parity/fixtures/closewatcher/` + records in `internal/parity/testdata/closewatcher-*`; registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`; `docs/browser-parity-harness.md` (the fixture notes and the two harness facts).
- **Fixtures:** `closewatcher-escape` (nested popovers: the first Escape closes the newest with focus inside the parent but not inside the newest overlay; the second closes the last — `toggle` entries plus `keydown` entries carrying the focus field), `closewatcher-hint` (popover=hint closes with one Escape), `closewatcher-prevented` (a `preventDefault` keydown listener closes nothing). The wrong view (`EscapePerOverlay`, manual entries + per-overlay Escape listeners — the pre-ticket shape) fails Tier B on the wrongly closed parent's toggle entry and Tier C on the still-painted child, while Tier A still passes.
- **Key citations:** `core/input/keyboard_event_manager.cc:454-464` (the keydown default chain routes Escape after Tab), `:618-630` (`DefaultEscapeEventHandler` → `closewatcher_stack()->EscapeKeyHandler`), `core/html/closewatcher/close_watcher.cc:195-202` (`EscapeKeyHandler`'s gates), `:204-215` (`Signal` — the newest group, reverse walk, the first prevented cancel stops it, one allowed group spent), `:280-312` (`RequestClose` — the cancelable cancel event, the prevented branch consuming the history activation, close = destroy + the close event), `:92-149` (`Add`/`SetHadUserInteraction` — the grouping and the no-banking flag), `:151-155` (`CancelEventCanBeCancelable`); `core/html/html_element.cc:2146-2157` (the popover show creates a watcher per show), `:228-247` (the popover listener: close hides, cancel does nothing), `:2834-2836` (the hide destroys it); `core/html/html_dialog_element.cc:536-545` (the dialog registers one); `core/frame/frame.cc:1041-1055` (the activation notification).
- **Limitations:** the modal entry's watcher keeps the popover's cancel semantics (the dialog's cancel-forwarding to its own `cancel` event is ticket 23's `ShowModal`); the "trusted event" gate is implicit (every platform dispatch is real input in the port); the port has no navigation to spend activation on, so only a prevented cancel consumes the history bit; Escape with a listener's `StopPropagation` still signals (the DOM clears the stop flags before the default phase — ticket 09's rule); the watcher's cancel/close listeners are synchronous Go callbacks, not script-visible DOM events; key events during IME composition are not gated off the Escape arm (the port has no `kVKeyProcessKey`); and two harness-recorded divergences live with the fixtures' design notes (the key-command task-loop pump and the occluding hitbox's cut of the mouse propagation chain).
