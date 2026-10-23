# TSX via esbuild + goja: a probe against the Luau decision

Probed 2026-10-23 on branch `research/tsx-probe`. The prototype is in [`research/tsx-probe/`](../tsx-probe/), a nested Go module (its own `go.mod`, with a replace to the repo root), so the root `go.mod` gets no new dependencies. Pins: `github.com/evanw/esbuild v0.28.2` (`pkg/api`) and `github.com/dop251/goja v0.0.0-20261023133238-1622cea953df`, built with go1.27.1, `CGO_ENABLED=0`, on this PC (16 logical CPUs, Windows 11). [V] means measured or read in source; [I] means inferred. This is a proposal: [ADR 0002](../../docs/adr/0002-luau-ui-scripting.md) (Luau) stands until the user decides otherwise.

## Verdict: worth it

TSX on goja meets every threshold, as long as the layer keeps zgui-script's model ([08](08-hot-reload-and-ui-scripting.md)): describe once, host-owned keyed state, the script runs only for events and bound values. The numbers are for a zterm-like sidebar of 1,053 elements [V]:

- **Reload: 4.1 ms** (threshold 300 ms), from source text to the new described tree, with state kept. That is esbuild 0.7 + a new VM 0.05 + compile 0.5 + run 0.2 + describe 2.1 ms. A failed reload keeps the last good tree.
- **Per event, bound (zgui-style): 0.001 ms** (threshold 2 ms) from the handler call to the updated element text. A React-style re-describe of the whole tree costs **2.3 ms**, just over the threshold. So `useState` must not re-run the whole tree on each set.
- **Per frame: +0.03 to +0.12 ms over plain Go** (threshold 1 ms). Building gpui elements from the described tree costs the same as the hand-written Go view (0.64 vs 0.66 ms; within noise). Both draw the same scene: 10 quads and 459 glyph sprites.
- **Costs that stay:** the binary grows by **+11.7 MB** (goja, for a release build that embeds JS transformed at build time) or **+15.9 MB** (goja and esbuild, for a dev build). goja has **no per-VM memory limit**, which ADR 0002's plugin sandbox needs. Type checking needs `tsc`, which means Node.

The performance case for Luau over goja doesn't hold at this size: the script isn't on the frame path in either model. What remains are trade-offs: TSX and CSS familiarity (to LLMs and to people) on one side, and binary size, the plugin memory limit and a Node-based checker on the other. That's the user's call. If TSX wins, the change to ADR 0002 is the language only. The model (keyed state, describe once, bindings, engine-run animation) and the plan for plugins, shipping and types carry over.

## Measurements

From `results/measurements.json`, `results/e2e.json` and `results/sizes.json`, the final run, all headless: hostless test windows on the real text system (`gpui.NewTestWindow` + `UseRealTextSystem`). No visible window was opened, and `GPUI_REAL_INPUT_TESTS` was not set. Times come from QueryPerformanceCounter, because `time.Now` is quantized to about 1 ms on this PC. The PC was shared with other work; across five runs, frame medians ranged from 10 to 26 ms. Script and native frames are therefore drawn alternately, and the overhead is the median of the paired differences.

| What | Result | Threshold | Notes |
|---|---|---|---|
| esbuild transform of `view.tsx` (99 lines, TSX → ES2017 IIFE, inline source map) | 0.76 ms median, 0.92 ms first | | 0.7–1.9 ms across runs |
| goja VM startup (`goja.New` + host globals) | 0.012 ms median, 0.09 ms first | | 8 KiB of heap per bare VM |
| First load (transform + VM + compile + run + describe) | 3.8 ms | | describe is 2.6 ms of it: 1,053 `h()` calls into Go |
| **Reload after a file change, state kept** | **4.1 ms** median (3.7 first) | 300 ms | 3.8–7.0 ms across runs. Doesn't include a file watcher's debounce or the next frame |
| **Event, bound value (zgui)**: counter click → `state.set` → one `bind` recomputed | **0.001 ms** (0.017 first) | 2 ms | |
| Event, re-describe (React `useState`): row click → `App` runs again, 1,053 nodes | 2.3 ms (2.4–4.4 across runs) | 2 ms | Over the threshold: keep re-describe off the common path |
| Build gpui elements per frame: script tree vs hand-written Go | 0.638 vs 0.663 ms | | Paired difference −0.04 to +0.07 ms across runs |
| **Whole changed frame**: build + layout + prepaint + paint | script 10.33, native 10.26 ms | | **overhead +0.12 ms** (+0.03 to +0.6 on a loaded PC) |
| Click → frame, end to end (simulated press and release through gpui hit testing) | 20.1 ms bound, 22.1 ms re-describe | | 2 frames per click (press and release each redraw); frame-bound, like native |
| Retained heap: VM + described tree | 504 KiB; 518 KiB after 30 reloads | | Old VMs are collected; no growth |
| Binary, stripped (`-s -w`) | gpui only 25.6 MB; +goja 37.3 MB; +goja+esbuild 41.4 MB | | **+11.7 MB release, +15.9 MB dev**. goja alone adds 9.6 MB to an empty program |
| `tsc` on `gpui.d.ts` + `view.tsx` | 1.1 s cold, 0.15 s warm | | Node 24, TypeScript 5.9.3 |

The frame cost is gpui-go's own, and the same for script and Go: about 10 ms for this 1,053-element tree, almost all of it layout and paint. That's a gpui-go performance question, not a scripting one.

## What the prototype is

- `host.go` (about 600 lines) is the whole host:
  - **The JSX factory.** esbuild compiles `<div …>` to `h("div", props, …children)`. Function components run in JS; host elements are built in Go (`__el`) as `*Node` values. A style object is parsed once, at describe time, into a flag-set struct. `className` goes to `Styled.Class` (the Tailwind layer) and is applied before `style`, so inline style wins, as in CSS. `onClick` keeps the JS function.
  - **Host-owned keyed state.** `useState(key, init)` returns `[value, set]`; setting it describes the tree again. `state(key, init)` with `bind(state, fn)` is zgui's `ui.state` plus a computed text: setting it recomputes only the bindings on that key. Values live in a Go map. A reload makes a fresh VM and reads them back, so `init` matters only for new keys, as in zgui.
  - **Lowering.** `Lower()` walks the Go tree once per frame and calls the same fluent gpui methods the Go view uses. The script doesn't run during a frame.
- `view.tsx` is the sample: a header, 6 agent rows, a counter and 200 session rows (1,053 nodes), modelled on `examples/zterm/sidebar.go`. `native.go` is the same view in plain gpui-go.
- `gpui.d.ts` types the surface: `JSX.IntrinsicElements`, a `CSSProperties` subset with literal unions and `` `#${string}` `` colours, and the state functions.
- Run it with `CGO_ENABLED=0 go test -count=1 .` in `research/tsx-probe`. The tests also check correctness: state survives 30 reloads, bound text updates, a re-describe highlights the right row, both views draw the same scene, and a syntax error keeps the last good tree.

## DX observations

**Language support.** Each snippet in `dx_test.go` was run in goja as written and after esbuild lowering to ES2020, ES2017 and ES2015 (`results/dx.json`) [V]:
- **Works as written:** goja runs modern JS with no lowering: let/const, arrows, destructuring, spread, classes with ES2022 fields and `#private`, `?.`, `??`, `??=`, generators, async/await, BigInt, `flat`/`at`/`fromEntries`/`replaceAll`, and regex named groups and lookbehind. ES2017, the target the host uses, is just a safe floor.
- **Lowering to ES2015 breaks BigInt `**`.** esbuild rewrites it to `Math.pow`. Don't go below ES2017.
- **Missing APIs:** `Intl`, `structuredClone`, `setTimeout`/`fetch` and other web or host APIs. The host would grant these (zgui grants fetch and the clipboard the same way). `goja_nodejs` has an event loop and `console` [I].
- **No ES modules or top-level await in goja.** esbuild bundles imports into one IIFE. A real layer would use `api.Build` with bundling, so `import` across files works and reload follows the dependency graph, like zgui's `require` watch.

**Errors.**
- A syntax error comes from esbuild with file, line and column: `view.tsx:62:27: Expected "}" but found "{"`.
- A runtime error in a handler maps back through the inline source map, which goja reads by itself: `TypeError: Cannot read property 'count' of undefined at onClick (view.tsx:88:59(6))`. That is the right line and column in the `.tsx`. (goja says "undefined" where V8 would say "null".)
- An error raised by the host while describing (an unsupported style key) currently names the Go function, not the `.tsx` line. A real host would add the JS caller's position (goja `CaptureCallStack`).
- esbuild strips types and never checks them.

**Types for editors and LLMs.**
- `gpui.d.ts` plus a `tsconfig.json` (`jsx: react`, `jsxFactory: h`) makes every TS editor check the view.
- `tsc` found a real bug the runtime had accepted (a `string` where a colour literal was required). It also caught a misspelled style key ("Did you mean to write 'color'?"), an unsupported value (`"space-around"`) and an unknown event prop (`results/tsc.txt`).
- As with zgui's `zgui.d.luau`, the `.d.ts` should be generated from the Go lowering tables, with a test that fails when the host accepts something the types lack.
- LLMs know this shape well, because it is React's `style` and `className`. The surface should stay close to `React.CSSProperties` names and values, and say clearly what isn't supported.
- The checker needs Node. Microsoft's Go port of the TypeScript compiler (typescript-go) could give a Node-free `check` command [I, not probed].

## Risks

1. **Binary size.** goja adds 11.7 MB to every shipped app (esbuild could stay out of release builds if scripts are transformed at build time). Luau would add an estimated 1–2 MB inside the DLL [I].
2. **No memory limit per VM.** goja can interrupt a runaway script (`Runtime.Interrupt` on a timer, for a per-call time budget), but it can't cap a VM's heap. ADR 0002's plugins need a memory limit per VM (zgui uses 32 MiB). Possible answers: run plugins in a subprocess, or patch goja's allocation accounting [I]. This is the largest gap.
3. **Re-describe cost grows with the tree.** A full re-describe costs about 2.2 µs per node, so 2.3 ms at 1,053 nodes and more for larger views. The layer must keep zgui's discipline: bindings for values, keyed lists that re-describe only changed items, and component-level re-describe instead of the whole root. None of that is built in this probe.
4. **The JS semantics trap.** Writing TSX invites React habits that don't apply: hooks with rules, effects, re-render on every set, npm UI packages. The model must say clearly "this looks like React, but it isn't React". A real React reconciler (gpuix's model) would bring back the per-update costs and the lost-state reload that [08](08-hot-reload-and-ui-scripting.md) rejected.
5. **goja is one maintainer's project.** The pin is a pseudo-version dated today. It's active and used by k6 and others, but there are no releases [I].
6. **goja's interpreter speed** is below Luau's, an estimated 5–20× on tight loops [I, not measured here]. It doesn't matter for describe-once and event code, but it does for any script on a per-frame path (zgui keeps animation in the engine for this reason).
7. **Toolchain.** Type checking needs Node plus TypeScript (or an unproven typescript-go). Luau's checker, luau-lsp, is a single binary.

## What a Luau comparison would need (not done)

- **A native rebuild.** Builds are `CGO_ENABLED=0`, so the Luau VM must live in `reference/native`'s DLL, with a C ABI for Go: create a VM, load bytecode, call a function, and a host callback table for `h`, `state`, `bind` and `set`. That is the planned-rebuild work in ADR 0002.
- **The same sample** written in Luau (`view.luau`, zgui-script style), with the same host model and lowering, and with nodes crossing the ABI either as batched descriptions or as one callback per `h` call. That ABI choice decides the describe cost.
- **The same measurements:** compile from source to bytecode (`luau-compile`, or the Luau compiler in the DLL), VM startup, first load, reload with state kept, a bound event, a re-describe, per-frame build, DLL growth and memory.
- **Types:** a generated `.d.luau`, checked by luau-lsp, against `gpui.d.ts` and tsc. This includes what a Luau checker reports for the same three broken edits.
- **A memory limit** per VM, as Luau's allocator hook supports, to confirm the plugin sandbox that goja lacks.
