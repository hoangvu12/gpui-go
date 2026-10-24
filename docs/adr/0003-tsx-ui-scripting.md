# ADR 0003: TSX (esbuild + goja) for UI scripting, replacing Luau

- Status: Accepted (direction). The design and spec are still to come.
  Plugin memory isolation is decided (2026-11, ticket 67): time budget
  only, in process — see the addendum below.
- Date: 2026-10-23
- Supersedes: the language choice in [ADR 0002](0002-luau-ui-scripting.md). Its other decisions carry over, adapted below.

## Context

ADR 0002 chose a zgui-style script layer in Luau. The user then set the priority: "i want the Ui dev experience to be as similar to browser as possible since LLMs were trained on them a lot". LLMs know TSX/JSX, CSS and Tailwind far better than Luau or zgui's own Luau API.

A probe measured TSX on this PC ([09](../../research/browser-parity/09-tsx-goja-probe.md), prototype `research/tsx-probe/`; the orchestrator re-ran it). esbuild compiles the TSX and goja runs it, both pure Go, on a zterm-like view of about 1,050 elements:

| Measure | Result |
|---|---|
| reload with state kept | ~4 ms |
| first load | ~4-5 ms |
| a bound-value update | 0.001 ms |
| frame overhead against plain Go | within noise |
| memory | ~0.6 MB |

A React-style re-run of the whole tree per update took 2.3 ms.

Costs measured or found:
- A release binary grows by about 12 MB (goja). esbuild is needed only in development.
- goja has a time budget but no per-VM memory cap.
- Type checking needs `tsc` (Node) on the developer's machine.

## Decision

The user, 2026-10-23: "ok lets try that".

UI scripting is TSX, run by goja and compiled by esbuild, on zgui's model:
- The script describes the tree once.
- The host owns keyed state, so it survives reload.
- Updates change bound values. The whole tree is not re-run on every state change.
- Animations run in the engine.

The authoring surface is browser-shaped: JSX elements with CSS-named `style` objects and Tailwind `className` (the ADR 0001 layer). It lowers to the core's browser-like elements (research/browser-parity).

Carried over from ADR 0002, adapted:
- **Plugins, like zgui:** slots, contributions, and capability grants. Each plugin runs in its own goja runtime with a time budget. goja can't cap memory, so the spec must choose: a memory cap through a separate plugin process, or plugins with a time budget only.
- **Shipping, like zgui:** development reads and watches `.tsx` files and reloads in place. Release bundles them with esbuild at build time and embeds the bundle; a compile error fails the build with file:line.
- **Types, like zgui:** a generated `.d.ts` covers the element, style and event surface. A test fails when the engine accepts something the types lack. A `check` command runs `tsc`, and debug builds check after every reload when `tsc` is installed.
- **Go-side dev reload, like `tauri dev`:** the dev command rebuilds and restarts the app on Go changes, restoring keyed script state.

## Consequences

- No native rebuild is needed for scripting. Luau no longer rides on R15.
- Release apps that use scripting are about 12 MB larger.
- Host APIs that goja lacks (`setTimeout`, `fetch`, `Intl`) must be provided by the host. ES2017 is the lowering floor, and esbuild bundles modules because goja has no ES modules.
- The API looks like React or Solid, but runs zgui's update model underneath. The spec must make bound values the natural way to write updates.

## Addendum: plugin memory isolation (ticket 67, 2026-11)

The carried-over decision above left one choice open: "goja can't cap
memory, so the spec must choose: a memory cap through a separate
plugin process, or plugins with a time budget only." Ticket 67 (the
plugin layer) decides: **time budget only, in process.**

- Each plugin runs in its own goja runtime (its own `script.Host`:
  VM, keyed state, bindings, timers, grants) inside the app's process,
  on the thread that renders. Every script turn — a load, a handler, a
  timer, a frame callback, a promise settlement — runs under a
  per-call time budget (default 50 ms, zgui's number); a turn that
  exceeds it is interrupted through goja's `Interrupt` (an uncatchable
  exception, so a plugin cannot swallow its budget) and the plugin is
  left out: its slots show an error placeholder with the .tsx file and
  line, its pending work is cancelled, and the app and the other
  plugins keep running.
- A plugin's memory is bounded by its lifetime, not by a cap: the VM,
  its state and its pending work live exactly as long as the plugin's
  slot registration, so unloading (or a failed-plugin park) hands them
  to the Go collector. Within its budget a plugin can still allocate
  freely — goja has no per-VM heap cap — so a plugin that allocates
  aggressively can grow the process until it is unloaded. That is the
  accepted trade-off.
- A separate plugin process would add a real heap cap, at the cost of
  an IPC boundary per contribution (every described tree crossing
  processes, grants proxied, events marshalled back) and a per-plugin
  process lifecycle to supervise. That machinery is future work for a
  plugin *ecosystem* of untrusted third parties; the in-process model
  with budgets, grants and lifetime-bound VMs is what the app-shaped
  use needs now, and nothing in it blocks moving the same registration
  API behind a process boundary later.
- One threading exception is documented and deliberate: `Interrupt`
  is called from the watchdog's timer goroutine. It is goja's one
  documented thread-safe entry point (it only stores a flag under the
  runtime's interrupt lock); the host clears it on the host thread,
  serialized with the timer, so a late watchdog cannot leave a stray
  flag armed on the next turn. Everything else about a VM stays on the
  host thread.
