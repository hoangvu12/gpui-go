# Give views a strong self-handle (Context.Entity() borrow expires before listeners run)

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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

- [ ] A view whose `OnClick` listener updates the view through the
      handle it captured in `Render` works when the click fires after
      `Render` returns, in a real window.
- [ ] The ownership contract's release semantics stay intact (no
      leaked leases: the leak detector stays green in the existing
      suites).
- [ ] The pattern is documented in the README's authoring example
      (the counter example uses a foreign entity today).
