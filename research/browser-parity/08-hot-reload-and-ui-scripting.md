# Hot reload and UI scripting: zgui-script vs gpuix, and what gpui-go should use

Researched 2026-10-23 by orchestrator 5, from source:
- zgui `ade8f27` (`crates/zgui-script`, `docs/scripting.md`)
- gpuix `a53b294` (`packages/react`, `packages/native`, `README.md`)

Clones are in `%TEMP%\research-src\`. [V] means read in source or measured; [I] means inferred. These are proposals until the user accepts them.

## Recommendation

1. **Now, Go only: a state-keeping dev reload.**
   - A dev runner watches the files, rebuilds, and restarts the app on the same window position.
   - Apps declare keyed state (like zgui's `ui.state("key", initial)`), which is saved before the restart and restored after it.
   - It costs about 3-5 s per edit (measured below), but nothing about the architecture changes, and it helps every app.
2. **Strategic: a script layer modelled on zgui-script, not gpuix.**
   - The script describes the tree once. Go lowers it to a retained tree and owns the state, keyed so that reload keeps it. Animations run in the engine.
   - The script runs only for event handlers, component mounts and computed properties.
   - The host elements and style properties the script uses are the same browser-like primitive set that the browser-parity research is defining. One surface serves both Go apps and scripts.
   - Language: **Luau inside the native DLL** is the default. zgui proves it: fast, typed through luau-lsp, sandboxed. A pure-Go JS engine (goja) is the alternative if TypeScript familiarity matters more than speed. That's the user's call.
3. **Don't copy gpuix's model for gpui-go.** It puts a whole JS runtime (Bun or Node) beside the app. On desktop its hot reload remounts the tree, so state is lost. And the per-update batch boundary only pays off when the engine is in another language.

## zgui-script [V]

- **Model** (`src/lib.rs:1-23`):
  - "A script runs once per (re)load and returns a description of components, state bindings and animations. Rust lowers that description to ordinary retained views."
  - The script is called again only for event handlers, mounting components, and computed properties.
  - Text templates (`"Count: {count}"`) and all animation run without calling the script.
- **State survives reload:**
  - State is Rust-owned and keyed by string (`ui.state(key, initial)`, `lib.rs:632`), like React Fast Refresh. Changing an initial value needs a new key or a restart (`docs/scripting.md:24`).
  - Each module's keys are its own. `ui.shared` is global, and `ui.use_state` is per component instance (`:278`, `:291`).
  - `ui.persist` also saves state across launches, with batched writes on a 250 ms timer and atomic renames (`:310`).
  - Terminals and web views are kept by id and outlive reloads (`lib.rs:705-707`).
- **Dev loop:**
  - `host.watch(250 ms)` reloads on file change, including any `require`d module (`docs/scripting.md:17-18`, `:278`).
  - A failed reload keeps the last good UI running and shows file:line in a banner (`:24`, `:99`).
  - luau-lsp type checks every reload, in a separate process, at about 0.1 s for 20 modules (`:69`). A built-in linter also runs (`:93`).
  - Theme files hot-reload without re-running scripts (`:399`).
  - `zgui-run app.luau` runs a script with no Rust host at all (`:22`).
- **Safety:** each script gets its own sandboxed VM, with read-only globals, a 32 MiB heap and a 50 ms CPU budget per call. That makes plugins safe to load (`:24`, `:316`).
- **Shipping:** in release, scripts are bundled into the binary by a build script (`:358`).
- **Cost:**
  - Streaming: Luau vs Rust is within noise (2.6% CPU each), with about 2% more instructions and +0.9 MiB memory (`:477+`).
  - Animating 1,000 elements in Luau plus the anime engine takes about 90 µs/frame, against about 240 µs with Rust signals (the engine skips unchanged values) (`### Cost`).
  - Guidance: keep hot paths out of the script. `ScriptHost::script_calls()` lets tests hold them at zero.
- **Size of the binding surface:**
  - `types/zgui.d.luau` is 8,520 lines.
  - The lowering and node code: `src/lower.rs` 3,799, `src/node.rs` 3,699, `src/lib.rs` 2,002, the `anime` engine about 5,400.
  - The crate totals about 65k lines, including tests and examples.
  - It depends on `mlua` with `luau` (`Cargo.toml`).
- **Scope:** layout animation, drag and drop, context menus, responsive layout, terminals, web views and theming are all exposed to scripts (`docs/scripting.md` headings). The zterm we recreated is a zgui-script app.

## gpuix [V]

- **Model:**
  - React or Solid runs in Bun or Node. The reconciler queues host mutations, and each commit is one `applyBatch` call over napi.
  - Rust keeps a retained tree and rebuilds GPUI elements from it every frame (`README.md:431-487`; `packages/native/src/retained_tree.rs`).
- **Hot reload on desktop:**
  - `bun --hot` re-runs the entry file, and `render()` unmounts the previous tree and mounts the new one on the same window (`packages/react/src/reconciler/renderer.ts:364-425`: "remount: unmount previous tree").
  - **The window is kept, but React state is not.**
  - True Fast Refresh, where `useState` survives, works only in the browser/Wasm build through Bun's dev server (`README.md:393-418`).
- **Runtime cost:**
  - It needs a JS runtime (Bun or Node) next to the native addon.
  - Updates pay for reconciliation plus batch encoding. Their tuned figure is 30 ms to parse and apply a 75k-node batch, at about 592 B per element (`docs/serialization-benchmark.md`).
  - Animations run in Rust without calling back into JS (README "Native animations").
- **Advantages:** the React and Solid ecosystems, TypeScript, and a familiar CSS-like `style` object.

## How they differ

| | zgui-script | gpuix |
|---|---|---|
| Language | Luau, embedded in the app | TS/JS in Bun or Node, a separate runtime |
| Who owns state | the host, keyed, so it survives reload | React (JS); desktop reload loses it |
| When script code runs | once per load, then only for events, mounts and computed props | every React render and commit |
| Animation | the engine (anime.js API, in Rust) | the engine (Rust), with limited options |
| Desktop reload | in place, state kept, last good UI kept on error | remount on the same window, state lost |
| Type checking | luau-lsp on every reload | TypeScript |
| Sandbox and plugins | per-script VM, memory and time limits | none |
| Shipping | scripts bundled into the binary | `bun build --compile` binary or a Node app |
| Cost | ~0 against native (measured) | JS runtime memory plus a batch per update |

## gpui-go today [V, measured 2026-10-23 on this PC]

Incremental `go build` of `examples/zterm` after a one-line edit:
- An app file (`examples/zterm/palette_view.go`): 2.8, 2.9 and 5.2 s.
- A core file (`gpui/div.go`): 7.8 and 4.4 s.
- The binary is 36.7 MB, mostly the embedded 18.9 MB native DLL, so linking is a large part of the time [I].

On top of the build you pay the app restart, and **all UI state is lost**. zterm restores only its layout, through its own `layout.json`.

Constraints:
- Builds use `CGO_ENABLED=0`, so a C-based script engine must live in the native DLL. Adding Luau would need a native rebuild.
- Go's `plugin` package doesn't work on Windows.
- yaegi, which interprets Go itself, would avoid a second language. But gpui-go relies on generics (`Styled[Self]`, `Component[Self]`, the `[_ lazy]` marker), and yaegi's generics and symbol-extraction support is limited [I]. It's worth one small probe before ruling it out.

## Options for gpui-go

| Option | Edit to screen | State kept | Second language | Cost to build | Notes |
|---|---|---|---|---|---|
| A. Dev reload in Go: watch, rebuild, restart, restore keyed state and window | ~3-5 s | yes, for declared keys | no | S-M (Go) | Works for all Go code. Could shrink the link by loading the DLL from disk in dev builds instead of embedding it [I]. |
| B. Luau in the DLL, zgui-style | ~0.1-0.3 s [I] | yes | Luau | L-XL (native plus Go lowering and bindings) | Proven by zgui. Sandboxed plugins come with it. |
| C. goja (JS in Go), zgui-style model | ~0.1-0.5 s [I] | yes | JS (TypeScript via a bundler) | L-XL (Go) | No native rebuild. Slower VM [I]. Using the React ecosystem would be a bigger step. |
| D. yaegi (Go interpreter) | ~0.5-1 s [I] | possible | no | M to probe, unknown after | Generics limits may rule it out. |
| E. gpuix-style external JS runtime | ~0.5 s | no on desktop | TS | XL | Adds Bun or Node. Its batch boundary isn't needed in Go. |

## Open questions for the user

- Is a second language for UI acceptable (Luau or TS), or must app UI stay in Go (A, maybe D)?
- If scripting: Luau (zgui's proven path, needs one native rebuild) or JS/TS through goja (familiar, pure Go)?
- Should plugins (sandboxed third-party scripts) be a goal? They come almost for free with B.
