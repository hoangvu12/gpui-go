# Give views a strong self-handle (Context.Entity() borrow expires before listeners run)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
Blocked by: none

## Question

The natural authoring pattern — capture `cx.Entity()` inside a view's
`Render` and call `Update` on it from an `OnClick`/`OnAction` listener
— panics when the event fires, because `Context.Entity()` returns a
callback-borrowed lease that expires when `Render` returns ("gpui:
callback-borrowed entity handle expired: handles issued inside a
callback are valid only during that callback; use RetainInto to keep
one"). Listeners run after the render callback returns, so every
listener captured this way is a delayed panic (mordbard's spike hit it
on its first action dispatch).

The reference's `cx.entity()` inside a view's render returns a strong
`Entity<T>`; every GPUI Base component captures entity handles in
render for use in listeners, so ecosystem portability (the accepted
ecosystem-portability goal) needs an equivalent pattern.

## What to build

Either `Context.Entity()` retains an independent lease (matching the
reference's strong handle; release flows through the existing lease
ownership), or an equally ergonomic documented self-handle pattern for
views (e.g. the window/view construction installs a strong self-handle
the view can read in `Render`).

## Acceptance criteria

- [x] A view whose `OnClick` listener updates the view through the
      handle it captured in `Render` works when the click fires after
      `Render` returns, in a real window.
- [x] The ownership contract's release semantics stay intact (no
      leaked leases: the leak detector stays green in the existing
      suites).
- [x] The pattern is documented in the README's authoring example
      (the counter example uses a foreign entity today).

## Comments

- 2026-10-19 — Claimed by the pi worker (consumer-gaps/05 branch).

## Answer

Resolved 2026-10-19 on `consumer-gaps/05-strong-self-entity-handle`.

**Design choice.** The ticket's first option — `Context.Entity()`
retaining an independent lease — conflicts with the recorded runtime
ownership decision ([04-define-runtime-ownership](../../gpui-core/issues/04-define-runtime-ownership.md),
[contract](../../../docs/runtime-ownership-contract.md) "Borrowing and
retained callbacks": `Context.Entity()` is a callback-borrowed facade,
saving an owned handle requires `RetainInto`, and the framework must
"not leak one owned lease per render"). The implemented pattern is the
ticket's second alternative, the closest conforming option:

- **`func (cx *Context[S]) StrongSelf() Entity[S]`** — the view's strong
  self-handle, read inside `Render` (R:app/context.rs:50-56
  `Context::entity`, strong in Rust because clones own ref-counts and
  the element tree owns the listener closures). Valid only while the
  view is the one being rendered in a window frame; misuse panics with
  a diagnostic. The returned handle never expires, works from listener
  closures after `Render` returns, `Downgrade`s and `RetainInto`s, but
  is not releasable or transferable (new `leaseSelfHandle` facade kind:
  "the window rendering the view owns its lease").
- **One lease per (window, view), owned by the window's scope**
  (`installViewSelfHandle`, cached in the window's draw state): the
  first read retains it, every later read shares it, so repeated
  renders never accumulate leases. This is the deterministic Go
  adaptation of the reference's closure-owned strong clones (Go cannot
  release a lease when a closure becomes unreachable); the recorded
  alternative for strong cycles applies — the window is the external
  graph-owner scope. Closing the window releases the lease; the view
  releases with its last window (each window renders it with its own
  placement + self-handle lease).
- **`OpenWindowView` builds the root view with the window as the active
  frame-construction boundary** (R:app.rs:1346-1350
  `window_update_stack` around `build_root_view`; app.rs:1360
  `window.root.replace` — the window owns its root view): the root
  placement's `ViewOf` lease now lands in the window's scope, so a
  closed window releases its root view instead of leaking it to the
  app root scope until shutdown.

**Verification** (evidence:
[evidence/consumer-gaps/ticket05-strong-self-handle.json](../../../evidence/consumer-gaps/ticket05-strong-self-handle.json)).
Deterministic corpus
[internal/portfixture/self_handle_test.go](../../../internal/portfixture/self_handle_test.go)
(7 tests): the click-after-render acceptance, the borrowed-facade
contrast (captured `cx.Entity()` still panics expired while
`StrongSelf()` works), one shared lease across renders (lease count
stable at 3 over 6 renders), release with the window (release hook +
`AssertEntityReleased` + `AssertNoNewLeaks` clean), per-window leases
(closing one window keeps the view alive through the other, which
still serves clicks), and the misuse diagnostics. REAL-window
acceptance
[internal/authorspec/self_handle_window_test.go](../../../internal/authorspec/self_handle_window_test.go)
(2 tests): a synthetic `WM_LBUTTONDOWN`/`WM_LBUTTONUP` pair through
`SendMessageW` on the host thread drives the whole platform path —
window-procedure translation, the mouse input callback, the element
listener and the frame-loop redraw — updating the view through the
render-captured handle; the release test proves the leak detector green
across the window close. The README's authoring example documents the
pattern ("Updating the view from a listener"), and the glossary records
the term ("View self-handle").

Full corpus: 33 packages ok; the three failures are the documented
environmental classes, re-verified (the interactive-session cursor
gate fails identically on the pre-change commit; the capture pixel
gate passes isolated `-p 1`; headlessspec needs the gitignored
`reference/ce-source`, absent in a fresh worktree).

**Known follow-up (out of scope here):** `ViewOf` called during a render
retains one placement lease per call, so a nested view rebuilt every
frame accumulates leases in the window's scope until it closes — the
same per-render-lease shape the self-handle avoids; a per-(window,
entity) placement-lease cache would mirror this ticket's fix for
adapters.

**Operational note:** during verification a cross-worktree `git stash`
race swapped WIP between this worktree and the ticket-02 worker's
(`refs/stash` is shared repo-wide across worktrees); both sides
recovered their trees from the stash objects. Workers in this repo
should avoid `git stash` while sibling worktrees are active.
