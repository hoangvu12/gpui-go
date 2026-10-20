# App.Update from a non-foreground goroutine races the scheduler drive and panics in the window trampoline

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
Blocked by: none

## Question

Closing a window while an update is in flight ends the app with

    gpui: host recorded a panic in the window trampoline:
    gpui: cannot run the scheduler while an application update is active

and `Host.Wait` returns that as the terminal error (exit code 1).
mordbard's screenshot harness hit it roughly one run in ten, and now
every run on this machine.

The panic comes from `scheduler.guardNotUpdating`
(gpui/executor.go): the foreground wake drain
(`platformDispatcher.drive` → `runRealtime` → `guardNotUpdating`)
panics whenever `app.updateDepth > 0`. The in-flight update is
started by a **different goroutine**: `App.Update` (and every
`Entity.Update` through it) runs its callback on the *caller's*
goroutine, so a consumer that drives its view from a background
goroutine (mordbard's harness goroutine calls `a.view.Update(a.gp,
…)` directly, and its background workers marshal through `host.Post`)
increments `updateDepth` while the host thread wakes into a scheduler
drive — the overlap that the guard turns into a terminal panic.
The reference prevents the pattern by construction (its `App` context
is not `Send`; background work returns through
`dispatch_on_main_thread`), but the Go port's public API offers no
marshaling and no diagnostic, only the race.

The race window is not close-specific — any cross-goroutine update
overlapping any wake drain can panic — but close makes it wide: the
harness's last entity update and capture post foreground work right
before `window.Close()`, and the shutdown drain
(`shutdownOnHostThread` → `runForegroundWork`) then drives the
scheduler into the still-open update.

## What to build

Serialize updates with the foreground: `App.Update` called from a
non-foreground goroutine of an attached, started host marshals the
update onto the host thread (post-and-block, panics propagating back
to the caller) instead of running it inline, so `updateDepth` is only
ever touched on the foreground thread and a wake drain can never
overlap an update. The foreground thread itself, test applications
(no host) and a host that already exited keep today's inline
semantics; task bodies keep their sanctioned `AsyncApp.Update`
request path (a raw `App.Update` from a task body must not deadlock
the drive, so the scheduler-mid-drive case falls back to inline and
is documented on `App.Update`).

## Acceptance criteria

- [x] A real app whose background goroutine updates an entity while
      the host thread wakes into a scheduler drive no longer records
      the trampoline panic: `Host.Wait` returns nil after
      `window.Close()` (the harness's settle-then-close workaround
      becomes unnecessary).
- [x] The marshaled update runs on the host thread (an observer sees
      the foreground thread identity), and a panic inside the
      marshaled callback propagates to the calling goroutine.
- [x] Updates on the foreground thread, test apps without a host, and
      updates after the loop exits behave exactly as today.
- [x] A task body that updates through `AsyncApp.Update` still runs
      inline in the drive (no deadlock, no behavior change).

## Answer

Resolved 2026-10-20 on `consumer-gaps-2` (commit "Marshal background
App.Update onto the foreground thread").

**Root cause** (as filed): `App.Update` — and every `Entity.Update`
through it — ran its callback on the *caller's* goroutine. A consumer
driving view state from a background goroutine (mordbard's harness
goroutine, its `drive` helper) held `updateDepth > 0` off-thread while
the host thread's wake drain (`platformDispatcher.drive` →
`runRealtime` → `guardNotUpdating`) drove the scheduler into the open
update; the guard's panic was recovered into the host's recorded
`panicValue` and became `Host.Wait`'s terminal error ("gpui: host
recorded a panic in the window trampoline: gpui: cannot run the
scheduler while an application update is active") — exit code 1 with
every screenshot already written. The reference prevents the pattern
by construction (its `App` context is not `Send`); the Go port now
prevents it by marshaling.

**Fix**: `App.Update` called from a non-foreground goroutine of an
attached, *started* host runs the update on the host's foreground
thread — post-and-block through `runForegroundSync`, with a panic
inside the marshaled update re-raised on the calling goroutine — so
`updateDepth` is only ever touched on the foreground thread and a wake
drain can never overlap an update. Inline fallbacks (today's
semantics, each documented on `Update`): the foreground thread itself;
test applications (no host); before `Start` (pre-Run construction);
**while the scheduler is mid-drive** (`scheduler.driving`, a new
counter around `drive` — a task body's *raw* `App.Update` must not
block on a foreground the drive is already waiting on; `AsyncApp.
Update` remains the sanctioned task-body path and still runs inline
inside the drive); and after the loop exited (best-effort — there is
no foreground left to serialize with).

**Verification** (winhostspec, real hosts, no real input — windows
open without activating):
- `TestAppUpdateFromBackgroundSurvivesSchedulerDrive` — the ticket's
  panic made deterministic: a background update held open across a
  scheduler drive. Red on the unmarshaled code with the EXACT draft
  symptom (the trampoline error through `host.Stop`, plus the drive
  dying so the spawned task never ran); green with the marshal (the
  drive runs after the update completes, the app exits nil).
- `TestAppUpdateMarshalsOntoTheForegroundThread` — the marshaled
  update observes `IsForegroundThread()` true; a panicking marshaled
  update re-raises on the caller; the host keeps serving posts after
  the contained panic.
- `TestAsyncAppUpdateInsideTaskBodiesStillRunsInline` — the sanctioned
  task-body path is unchanged.
- `TestAppUpdateAfterLoopExitDoesNotBlock` — the exited-loop fallback.
- `TestAppUpdateMarshalOrderWithConcurrentBackgrounds` — 8 concurrent
  background updates serialize (max concurrent 1).
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`: 38
  packages ok, the only failure the documented environmental gate
  (`internal/headlessspec` needs the gitignored `reference/ce-source`).
  No deadlocks: every real-app suite (fontspec, authorspec,
  scenespec, rendererspec, devicelossspec, dialogspec, imespec,
  textframespec, …) drives `app.Update` from test goroutines against
  pumping hosts and all completed.

**What mordbard should change**: drop the settle-before-close delay
(app/harness.go's `waitNextFrame` + `time.Sleep(250ms)` before
`window.Close` — the comment there names this ticket). The harness's
`drive` from its goroutine is now safe as written; closing while an
update is in flight can no longer panic the trampoline.

## Comments

- 2026-10-20 (pi worker): claimed, implemented, verified, resolved.
  The mid-drive inline fallback is recorded honestly: a raw
  `App.Update` inside a task body keeps today's racy-inline semantics
  (deadlock-free) instead of marshaling; `AsyncApp.Update` is the
  documented path there.
