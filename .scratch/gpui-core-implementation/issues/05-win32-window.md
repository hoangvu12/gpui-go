# Open and close a DPI-aware window on the foreground thread

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 02, 03

## Question

A minimal Go app opens two native windows, processes events and closes each without blocking the other.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Initialize the locked foreground OS thread and OLE STA, dispatcher window, wakeups and Win32 pump; deliver app effects at defined safe boundaries.
- [x] Create leased HWND/window identities and implement close veto, focus/activation, resize, scale and display lifecycle with typed messages.
- [x] Encode per-message synchronous query versus deferred mutation rules and demonstrate progress during a nested modal loop without illegal app reentry.
- [x] Establish PerMonitorV2 before window creation and verify the final executable mode; test mixed-scale moves, zero/minimized size and close during a queued callback.
- [x] Record thread/apartment and release traces for two windows sharing an entity. A test host is allowed for deterministic traces but does not replace actual Win32 execution.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Load one exact native artifact from a Go-only consumer](02-native-bootstrap.md)
- [Run scoped entities, typed access and ordered effects headlessly](03-scoped-entities-effects.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by parallel background subagents and independently re-verified by the orchestrator. Complete record: [evidence/ticket05-win32-window.json](../../../evidence/ticket05-win32-window.json).

- Real Win32 foreground host (gpui/win32host_windows.go): locked OLE-STA thread (OleInitialize balanced), PerMonitorV2 verified effective before any window, message loop with wake marshaling mirroring the reference dispatch_on_main_thread, close veto, WM_DPICHANGED, leased generation-stamped HWNDs, quit-on-last-close. Two windows open/process/close without blocking each other (7/7 real-window tests, 10/10 consecutive runs after fixing a pooled-thread WM_QUIT poisoning deadlock found by the orchestrator; 1000-post stress regression test with zero lost wakes). App integration: foreground scheduler wakes through the host; window events route through App.Update on the host thread. Observed machine scale 1.0 (96 DPI) recorded.

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

