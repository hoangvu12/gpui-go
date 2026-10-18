# Verify cross-service close, cancellation and shutdown

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: wave-4 orchestrator chat (Roboco)
Blocked by: 19, 20, 22, 24, 25, 26, 27, 29

## Question

A multiwindow app closes while tasks, modal operations, UIA, capture and rendering are active, and every owner reaches its specified terminal state.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [runtime ownership contract](../../../docs/runtime-ownership-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Run deterministic adversarial schedules for shared entities, scope cancellation, result handoff, effect flushing, focus release and abandoned frames across completed services.
- [ ] Run real multiwindow close/reopen/modal/provider/capture cases with delayed GPU completion and callbacks arriving after window removal.
- [ ] Verify exactly-once cleanup and explicit unresolved quarantine records; neither GC, elapsed time nor process-resident modules substitutes for logical release.
- [ ] Compare reference-preserved traces separately from documented Go adaptations; retain failed seeds and minimized reproducers.
- [ ] Demonstrate representative app responsiveness and investigate visible regressions. This integration ticket does not introduce a comparative benchmark requirement.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render SVGs with lazy fonts and correct alpha conversion](19-svg-fonts.md)
- [Compose and edit text through IMM32](20-ime-editor.md)
- [Expose the published tree to Windows UI Automation](22-uia-provider.md)
- [Handle OLE drag/drop and precision touchpad gestures](24-drop-touchpad.md)
- [Complete native dialogs and credential storage operations](25-dialogs-credentials.md)
- [Implement remaining Windows app and desktop operations](26-desktop-operations.md)
- [Inspect live element state and expose debug diagnostics](27-inspector.md)
- [Recover device loss and rebuild retained GPU resources](29-device-loss.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

## Answer

RESOLVED (2026-10-18, wave 4). The cross-service close/cancellation/shutdown corpus is live:

- **Corpus**: internal/shutdownspec — 11 tests over the deterministic test app, real Win32 windows and the real GPU/capture/UIA/modal services (FAIL not skip, synthetic messages only). Deterministic: the window-close cascade (scope close → lease release → focus release → element-runtime teardown, at the flush boundary), the two-window shared-entity close with exactly-once release, the window-scope delivery racing the window close, the shutdown drain (effect flushing at update boundaries, the unresolved-execution record that survives GC/time, new-work rejection, the late terminal completion, idempotence), the abandoned frame build. Real: the multiwindow close/reopen with delayed GPU completion (submissions retiring after window removal — the ledger's post-close exactly-once terminal record, the stale-lease generation refusals, the surface-detach ordering, the double-close guards, the ledger reconciliation), the capture stream stopping/destroying under a live scene reference (stop is not release; the destroy is the typed bad-handle record; GC/time is not release), both a11y teardowns (the adapter destroyed exactly once at window close; the explicit teardown's late WM_GETOBJECT falling to DefWindowProc; the generation-checked dispatch refusals), the modal prompt pumping during a full close cascade (both scopes closed during the pump, the outcome discarded, the quit-on-last during the pump, App.Run's clean return), and the honest quarantine record (the native ERR_QUARANTINE not forceable — the surrounding guards pinned).
- **Fixed in this slice**: the terminal retirement records were lost across App.Shutdown (a queued App.Spawn task cancelled by the drain — `Submission.WaitForRetirement` stuck in `retiring`; minimized reproducer retained) and a post-shutdown dispatch panicked the pacing worker; the records now ride the host's foreground queue, which survives app shutdown. The window-close element-runtime teardown (render.go's RemoveWindowView path) was unwired for real windows — now hooked in removeWindow.
- **Recorded, not fixed (frozen DLL)**: the pre-existing native heap corruption under mixed COM churn (a real IFileDialog + a11y adapters + WGC in one process; minimized reproducers in TRACES.md), the 8-slot atlas budget, the undisposed abandoned/last-scene handles, the real-mode virtual-clock timer limitation. Trace notes with pin citations: internal/shutdownspec/TRACES.md.

Evidence: [evidence/ticket30-shutdown-integration.json](../../../evidence/ticket30-shutdown-integration.json) (executed results; the honest blocked rows; the machine's mouse discipline).

