# Hot reload and UI scripting: zgui-script vs gpuix, and what gpui-go should use

Parent: ../map.md
Type: research
Labels: wayfinder:research
Status: open
Assignee: unassigned
Blocked by: none

## Question

The user (2026-10-23): "research zgui-script as well to see how much it different from the gpuix, and see what we should use, cuz i think hot reloading is also a really important factor".

Read the actual source of:
- zgui's `crates/zgui-script`: Luau embedding, the binding surface, how hot reload keeps state, what lives in script vs Rust (motion, components), the dev loop, and the cost at runtime and in binary size
- gpuix: the React/Solid reconciler to `applyBatch`, Fast Refresh and what state survives, the JS runtime cost, and the dev loop

Then for gpui-go:
- Measure today's edit-to-screen loop (incremental build, restart, lost state).
- Compare the options: a state-keeping dev reload in pure Go; an embedded script language (Luau in the native DLL, because CGO_ENABLED=0, or a pure-Go engine such as gopher-lua or goja); declarative UI files with live preview; or something else the sources suggest.
- Recommend one, with its costs: two languages, typing, the binding surface, performance, and shipping.

Output: research/browser-parity/08-hot-reload-and-ui-scripting.md, with the recommendation first.

## Comments

## Answer
