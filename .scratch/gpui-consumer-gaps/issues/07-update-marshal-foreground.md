# App.Update from a non-foreground goroutine races the scheduler drive and panics in the window trampoline

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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

- [ ] A real app whose background goroutine updates an entity while
      the host thread wakes into a scheduler drive no longer records
      the trampoline panic: `Host.Wait` returns nil after
      `window.Close()` (the harness's settle-then-close workaround
      becomes unnecessary).
- [ ] The marshaled update runs on the host thread (an observer sees
      the foreground thread identity), and a panic inside the
      marshaled callback propagates to the calling goroutine.
- [ ] Updates on the foreground thread, test apps without a host, and
      updates after the loop exits behave exactly as today.
- [ ] A task body that updates through `AsyncApp.Update` still runs
      inline in the drive (no deadlock, no behavior change).
