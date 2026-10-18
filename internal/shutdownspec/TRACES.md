# Shutdown-integration trace record (ticket 30)

This file records, per corpus case, where the port's observable
**matches a pinned reference trace** and where it is a **documented Go
adaptation**, with the pin (GPUI-CE `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`
under `reference/`) cited by file and line. Failed seeds and minimized
reproducers found by this corpus are retained below. The suites this
corpus deliberately does not duplicate (and cites instead):
`internal/taskspec` (task-layer delivery gates and the drain
accounting), `internal/portfixture/ownership_test.go` (entity/lease and
flush-loop traces), `internal/a11yspec` (provider routing/staleness),
`internal/dialogspec` (modal-pump basics), `internal/devicelossspec`
(confirmed-loss recovery), `internal/rendererspec` (renderer ledger).

## Reference-preserved traces

| Case (test) | Pinned trace | Pin |
|---|---|---|
| Flush-loop order: releases before effects, each iteration | `release_dropped_entities()` at the top of the `flush_effects` loop, before the effect `pop_front` | `crates/gpui/src/app.rs:1752-1757` (loop head at 1743) |
| Window removal drops the window's state, not shared entities | the removal trail: `window_handles.remove`, `windows.remove`, tracked-entity invalidator cleanup, window-closed observers, quit-on-empty; no entity teardown | `crates/gpui/src/app.rs:1978-2010` |
| WM_CLOSE veto → DefWindowProc destroys; WM_DESTROY runs the close callback then posts WM_GPUI_CLOSE_ONE_WINDOW | `handle_close_msg` / `handle_destroy_msg` | `crates/gpui_windows/src/events.rs:339-366` |
| Quit on last window closed (non-macOS default) | `quit_on_empty` in the removal trail | `crates/gpui/src/app.rs:1994-2004` |
| Late WM_GETOBJECT with no provider falls to DefWindowProc (the `None` arm) | `handle_wm_getobject` | `crates/gpui_windows/src/events.rs:855-871` |
| Adapter destroyed at window close; the handle is deregistered before the native destroy so late queries fall through | A11yState drop with the window | `crates/gpui_windows/src/window.rs:1145-1176` (a11y_init/drop) plus the port's sentinel release |
| Modal dialog pumps its own loop; posted foreground closures defer until it ends; the late result after owner closure discards | the reference dispatcher during dialog pumping; the dropped oneshot receiver | `crates/gpui_windows/src/platform.rs:1296-1389` (file dialogs) |
| Capture frame handles stay resolvable only while published; drawing a released frame is an explicit error, never a blank surface | `frame_texture_for_draw`'s slot/published checks returning the typed capture status codes through the draw | `reference/native/src/renderer/draw.rs:1940-1960`, `reference/native/src/capture.rs:1338-1357` |
| Acceptance is not completion; retirement only after a completed poll; destroy refuses while un-retired submissions remain | submission retire/destroy bodies | `reference/native/src/renderer.rs:1842-1905` (retire), `1897+` (destroy's pending guard) |
| Focus release of the focused handle clears focus and notifies focus-lost exactly once, idempotently | `notify_focus_lost` through `FocusHandle` release | `crates/gpui/src/window.rs:2295` (remove_window) neighborhood; `focus.go`'s port cites window.rs |
| Abandoned build keeps the previously published artifacts | `rendered_frame` swaps only on a successful draw | `crates/gpui/src/window.rs` (draw's `mem::swap` on success) via `gpui/render.go`'s `completed` gate |

## Documented Go adaptations (with the replaced guarantee)

| Case | Adaptation | Source of the guarantee |
|---|---|---|
| Entity release at flush boundaries (no drop-driven release) | explicit leases/scopes; hooks run at the next flush, scope already closed | `docs/runtime-ownership-contract.md` "Logical ownership" |
| Arena-tail retirement schedules a subsequent flush | the pin clears `event_arena` at the loop break and leaves those releases to a later boundary; the port performs one extra flush (`arenaWake`) | contract "Effects and payloads"; pin `app.rs:1825-1828` |
| Focus handles are window-scoped and logically released (no refcount) | release is idempotent logical state | `docs/runtime-ownership-contract.md`; `gpui/focus.go` header |
| Unresolved executions stay tracked through GC/time; the late ack releases them | the deterministic scheduler accepts terminal completions after `App.Shutdown` rejects new work | contract "Subscriptions and tasks" |
| Terminal retirement records ride the host's foreground queue, not `App.Spawn` | `App.Spawn` tasks are cancelled by the shutdown drain (see the retained reproducer below); posted host closures survive shutdown and the host's final drain runs them | contract "Foreground, frames, and native resources" ("drains framework completion/retirement work before destroying native owners") |
| App.Shutdown drains the task layer only | scope/entity/window teardown happens through window close (WM_DESTROY) and explicit owner calls, not a single orchestrated app teardown; effects queued without an update boundary flush at the next `App.Update` | `gpui/task_ownership.go` (Shutdown doc); `gpui/window.go` (NewApp) |
| Real-mode task timers park on the virtual clock | the host does not advance the scheduler's virtual clock yet; a worker parked on `Sleep` stays unresolved (the honest record this corpus asserts) | `gpui/window.go` (NewApp comment: "real-time timers arrive with the shutdown integration ticket") |
| The renderer's own gate is `Renderer.Close`, not `App.Shutdown` | a submission presented after app shutdown began is still accepted and retires (recorded in `TestRealAppShutdownAcceptsRendererRetirementAfterShutdown`); the app-shutdown ↔ renderer-close ordering is the application's job | `docs/renderer-contract.md`; TRACES note, no orchestrated fix in this slice |
| The modal-pump test runs on the dialog-driver seam, not the real IFileDialog | the real COM dialog mixed with this package's a11y/capture churn crosses the native heap-corruption threshold (seed #2 below); dialogspec pins the real-dialog evidence | the driver seam: `App.SetDialogDriver` (dialogs.go); TRACES seed #2 |

## Honest blocked/unverified rows

- **Native ERR_QUARANTINE** (retirement refused: failed without
  confirmed loss, `renderer.rs:1864-1870`): not forceable on healthy
  hardware with the shipped artifact — real driver removal only; the
  service has no fake-failure injection. The surrounding guards
  (pending retire refusal, ERR_PENDING destroy refusal, the DROPPED
  abandonment record) are pinned by
  `TestRendererCloseGuardsAndAbandonmentRecord`; the confirmed-loss
  release path is pinned by `internal/devicelossspec`.
- **Abandoned-build scene disposal**: `gpui/render.go`'s error path
  returns without disposing the abandoned frame's native scene handle,
  and the window's last drawn scene is not disposed at window close
  (`dropDrawState` disposes the layout engine only). The native scene
  registry grows for the process lifetime (no slot cap,
  `reference/native/src/scene.rs:2662-2681`). Observed from source; not
  fixed in this slice (the scene handle is shared with the
  `DrawWindowFrame` caller, so disposing it at close would invalidate a
  live caller-owned reference — an ownership decision beyond this
  ticket's bounded scope).
- **Real-mode late worker completion**: a delivery worker parked on the
  virtual-clock `Sleep` never completes in real mode (the clock is not
  host-driven), so the "result handoff racing window close" discard is
  pinned deterministically (`internal/taskspec` plus
  `TestScopeCancellationRacingResultHandoffThroughWindowClose`); in real
  apps only the registration-rejection flavor is currently reachable
  (spawning into a closed window scope panics with the scope gate).

## Retained failed seed and minimized reproducer #1 (fixed in this slice)

**Seed: retirement record lost across `App.Shutdown`.** Symptom: a
pacing-renderer submission accepted before `App.Shutdown` never
retires — `Submission.WaitForRetirement` times out and the ledger entry
sticks in `retiring` (marked by `markRetiring` after the native retire
succeeded, but the terminal record never landed).

Minimized reproducer (before the fix):

```go
// app/host running; pacing renderer; one shown window, one kept open.
sub, _ := renderer.PresentClear(wa, color)   // in flight
busy := make(chan struct{})
host.Post(func() { time.Sleep(10 * time.Millisecond); close(busy) }) // keeps the host thread busy
host.Post(func() { app.Shutdown() })          // queued behind the busy closure
<-busy
// the pacing worker retires during the busy window; its terminal record
// was dispatched as an App.Spawn TASK (queued, not yet driven)
// → App.Shutdown's cancelAllTasksForShutdown cancels the queued
//   retire-record task before its body runs
// → the record is lost; sub.WaitForRetirement(15s) times out.
```

Root cause: the terminal retirement record was marshaled through
`App.Spawn`, whose tasks the shutdown drain cancels — the exact
contract sentence "Shutdown continues accepting terminal
completion/retirement messages after rejecting new application work"
was violated by the dispatch path, not the accept path. Two adjacent
crash/loss modes existed:

1. the pacing worker's `App.Spawn` after shutdown began panicked on the
   rejection (an unrecovered panic on the pacing goroutine would abort
   the process);
2. the pre-shutdown dispatch's task was cancelled by the drain (the
   lost record above).

Fix (this slice, `gpui/renderer.go`): terminal retirement records ride
the host's foreground queue (`Host.Post` + `App.Update`) when the
application has a host — posted closures survive `App.Shutdown`, and
the host's final drain runs them even after the message loop exits;
hostless applications keep the `App.Spawn` path behind a shutdown
pre-check plus a recover backstop, recording directly when rejected.
`TestRealAppShutdownAcceptsRendererRetirementAfterShutdown` pins both
sides (the post-shutdown presentation retires; the pre-shutdown one
retires across the boundary without a panic).

## Retained failed seed #2 (pre-existing native defect, NOT fixed — frozen DLL)

**Seed: native heap corruption under mixed COM/service churn.** Symptom:
sporadic process death (exit status 0xC0000374 — STATUS_HEAP_CORRUPTION,
and access violations such as a fault on 0xffffffffffffffff inside
Win32 message translation, `win32host_windows.go`'s TranslateMessage/
DispatchMessage site) once one process hosts cumulative apartment churn:
native a11y adapters + a real IFileDialog + a Windows Graphics Capture
stream across several hosts.

Minimized reproducer (this machine, unmodified tree):

```
CGO_ENABLED=0 go test ./internal/a11yspec -count=2
# → the second round exhausts the native atlas slot budget (8 process-
#   wide; typed failures — a separate budget row below)
CGO_ENABLED=0 go test ./internal/shutdownspec -run \
  'TestA11y|TestCapture|TestModalDialog' -count=3   # (the real-dialog
#   variant of the modal test) → sporadic 0xC0000374 / 0xC0000005
```

Each service alone is stable (a11yspec, dialogspec, capturespec at
`-count=1`; dialogspec even at `-count=2`); the MIX corrupts. The
faulting write is native-side (Rust/DLL) — not fixable in this slice
(no cargo, frozen artifact). Recorded honestly instead of silently
worked around:

- the modal-dialog test runs on the application's dialog-driver seam
  (`App.SetDialogDriver` with a pumping driver — the dialogspec
  pumpingDriver model) so this package's shutdown integration (the
  full two-window close cascade during the pump, the discard, the
  quit-on-last, `App.Run`'s clean return, the post-cascade drain) is
  covered without adding the real IFileDialog's COM churn to the mix;
  the REAL dialog's modal-pump evidence stays pinned by
  `internal/dialogspec` (real IFileOpenDialog/SaveDialog, programmatic
  dismissal, the owner-close discard);
- the package stays at four real hosts (a11y, capture, modal,
  renderer-cascade) — measured stable at 0/24 consecutive runs.

## Process-wide budgets this corpus respects (recorded, like the
## a11yspec Narrator note)

- **Atlas slots: 8 per process.** Every real window's first draw
  creates an atlas (`windowTextSystemOf`); nothing disposes it at
  window close (`dropDrawState` disposes the layout engine only), so
  repeated in-process runs of the drawing tests (a11y corpus windows)
  eventually fail with the typed slot-limit error — the same budget
  the a11yspec Narrator note documents. Run heavy repetitions in
  separate `go test` invocations.
- **Wake coalescing during a modal pump.** A host-marshaled query
  (`Window.Alive`, `runForegroundSync`) issued while a nested modal
  loop pumps inside a posted foreground closure deadlocks: the
  enclosing drain still holds `wakePosted`, so `post()` skips the wake
  message and the query's closure never runs (the dialogspec corpus
  keeps to channel evidence during the pump for this reason; this
  package's modal test does the same and records the discipline
  here).
