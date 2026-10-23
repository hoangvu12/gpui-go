# ADR 0002: Luau for UI scripting and hot reload

- Status: Superseded in part by [ADR 0003](0003-tsx-ui-scripting.md) (2026-10-23): the language is TSX (esbuild + goja), not Luau. Its decisions on plugins, shipping, types and dev reload carry over, adapted, into ADR 0003.
- Date: 2026-10-23

## Context

The user considers hot reload "a really important factor". zgui's `zgui-script` is the reference: UI in Luau, reloaded in place with state kept. Research from source compared it with gpuix and with options for gpui-go ([08](../../research/browser-parity/08-hot-reload-and-ui-scripting.md)):

- **gpuix** (React or Solid in Bun or Node) needs a separate JS runtime. On desktop, its reload remounts the tree and loses state.
- **yaegi** (a Go interpreter) can't call or interpret gpui-go's Go 1.27 generic methods, so the fluent API crashes. It was probed and ruled out.
- A **Go-only dev reload** (rebuild, restart, restore) keeps one language, but each edit costs about 3-5 s (measured).
- **Luau** (zgui) measured close to native Rust in zgui's own benchmarks. It is typed through luau-lsp and sandboxed per script.

## Decision

The user, 2026-10-23: "yk lets just go with luau honestly".

gpui-go gets a Luau UI scripting layer modelled on zgui-script:
- The script describes the tree once.
- Go lowers the description to the core's browser-like elements and styles (research/browser-parity).
- State is owned by the host and keyed, so a reload keeps it.
- Animations run in the engine.
- The script runs only for event handlers, component mounts and computed properties.

Builds use `CGO_ENABLED=0`, so the Luau VM lives in the native DLL. That needs a native rebuild, which can be bundled with a planned one.

## Consequences

- UI can be written in Luau with reload in about a second (estimated; not yet measured in gpui-go). Go stays the language of the framework and of app logic.
- The script surface and the Go core share one element and style vocabulary: the browser-like set from research/browser-parity. That vocabulary must be settled before the binding surface is designed.
- Decided with the user, 2026-10-23, all "like zgui":
  - **Plugins: yes, like zgui.** Each plugin runs in its own sandboxed VM (memory limit and a time budget per call). The app declares slots that plugins contribute to. A plugin reaches only what the app grants: specific actions, shared state, fetch, the clipboard, files, and so on. A plugin that fails is left out, and the app keeps running.
  - **Shipping: like zgui.** Development reads `.luau` files from disk and watches them. Release compiles them to Luau bytecode and embeds them in the binary; a syntax error fails the build with file:line. A shipped app reads nothing from disk except user plugins.
  - **Types: like zgui.** A generated definition file for luau-lsp covers the whole element, style and event surface. A test fails when the engine accepts something the types lack. There is a `check` command, and debug builds check after every reload and show errors over the window.
  - **Go-side dev reload: yes, like `tauri dev`.** A dev command watches the Go files, rebuilds and restarts the app automatically. Keyed Luau state (and the window's position) is saved before the restart and restored after, so a Go change comes back to the same UI state. Luau edits reload in place, as the frontend does in Tauri.
