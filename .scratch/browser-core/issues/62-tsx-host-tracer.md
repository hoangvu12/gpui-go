# 62 - TSX host tracer

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat a1fbc74c
Blocked by: 01
Triage: ready-for-agent

**What to build:** A .tsx view (JSX elements, CSS-named style objects, Tailwind className, onClick) loads into a window through esbuild and goja. State declared with a key survives reload, state changes update bound values without re-running the tree, saving the file reloads in place, and a failed reload keeps the last good UI with an error banner showing the .tsx file and line (ADR 0003, productionizing the probe).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] A .tsx harness fixture matches the same Chrome page as its Go twin.
- [x] Reload keeps keyed state; a syntax error keeps the last tree and shows file:line.
- [x] The probe's thresholds hold: reload under 300 ms, a bound update under 2 ms, frame overhead under 1 ms at the probe's size.
- [x] Re-describing on useState stays available but documented as the slow path.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified after merging onto main (rev 15). goja and esbuild match the probe's pins. `go list -deps` shows `gpui/script` has no esbuild and `gpui` has neither library. No test opens a real window. `go test ./gpui/... ./internal/parity/... ./base` is ok, and the TSX twin passes all three tiers against the existing tracer record. Timing over 3 runs, with 4 other workers loading the PC: reload 3.7-5.4 ms (limit 300), bound update 0.0016-0.0027 ms (limit 2), frame overhead 0.06-0.66 ms (limit 1). That last margin is thin on a busy machine; watch it for flakiness.

## Answer

Productionized the probe as **`gpui/script`** (the host: goja, keyed state, bindings, reload, error banner, lowering; `doc.go` is the API guide) plus **`gpui/script/compile`** (the esbuild half). Placement: under `gpui/` because the spec owns it as a core seam (script elements lower to the core's elements) and later tickets (63-67: .d.ts, dev command, plugins) extend the same namespace, while the `gpui` package itself gains no dependency — goja and esbuild enter only through `gpui/script`, so apps that don't script pay nothing (measured: 0).

**API** (zgui's model, the probe's shape): the entry exports `App()`; JSX `div`/`text`/`Fragment` with props `key` (identity), `id` (id + debug selector), `className` (the Tailwind layer, validated at describe time so a bad class is a banner error, not stderr), `style` (CSS-named object; the probe's subset + `border`/`borderColor`), `onClick`/`onMouseDown` (DOM names, DOM-shaped event objects). Globals: `useState(key, init)` (re-describes — the slow path, documented in doc.go and kept off hot paths by tests), `state(key, init)` + `bind(state, fn)` (host-owned keyed state; a set recomputes only that key's bindings). Go side: `New(opts...)` with `WithSource`/`WithoutBanner`/`WithErrorHandler`; `Load(Bundle)`, `Reload()`, `SetGlobal`, `Element()`, `View` (mount via `NewEntity`+`SetRootView`, wires `Host.Notify` to `Window.Refresh`); test support `Text/Click/Get/DescribeCount/NodeCount/Err` (zgui's TestApp surface). Reload builds a fresh VM; state survives; a failed load keeps the last good tree, records `Err`, and mounts an error banner (debug selector `script-error`) with the .tsx file:line — esbuild positions for compile errors, goja + inline source map for runtime errors, and goja `CaptureCallStack` mapped through the source map for describe-time errors (the probe's known gap: an unsupported style key now reports `view.tsx:7:3`).

**esbuild out of release builds** (ADR 0003): the ADR allows a build tag or a separate package; I chose a **separate package** — `gpui/script/compile` is the only esbuild import, a release app compiles at build time (build step / go:generate), embeds the `Bundle` (`go:embed`) and calls `Load`; the compile error then fails the build with file:line. Import-graph exclusion beats a build tag: the default `go build` simply cannot reach esbuild, tests always run the real compile path, and `gpui/script` stays importable everywhere. Measured on a small example: gpui-only 24.1 MB, +host (goja, release shape) +11.5 MB, +compile (goja+esbuild, dev) +15.3 MB stripped — the probe's predicted 11.7/15.9.

**Tests** (test-first, all through public API): `gpui/script` — describe-once, bound update without re-describe (`DescribeCount` guard), useState re-describes, keyed state across 30 reloads, syntax error keeps the last tree + file:line, runtime error maps to the .tsx line, style/prop/element/class errors carry positions, DOM-shaped events, error handler, `SetGlobal` across reloads, release-embed path, App-returning-null; `gpui/script/window_test.go` — save→`Reload()` (the injected notification, no timing) reloads the window in place with keyed state kept (bounds change, counter continues), failed reload keeps the last good tree and mounts the banner at the window's bottom which a good save clears, fragments splice (gap observable); `compile` — ES2017 IIFE bundle on the `gpui_script` global, inline source map, syntax error file:line, imported-module bundling and error positions, `Dir` change detection (content hashes: Windows updates a same-size rewrite's mtime lazily) and `Watch` (injected polls). **Seam 3 fixture:** `internal/parity/fixtures/tsxtracer` — the tracer's page as `view.tsx` (className sizes/pads the box, style colors it, both handlers log through a `SetGlobal` bridge), replayed against the **existing tracer record** (not re-recorded, not registered): all three tiers pass (pixel stats whole 0.0144 / box ROI 0.2309 — the Go twin's own numbers), plus a discrimination test (wrong width fails Tier A).

**Measured timings** (QueryPerformanceCounter, `gpui/script/timing_test.go`, probe's exact 1,053-node sidebar — `NodeCount` asserts the size premise; scenes must be identical, 9 quads / 456 sprites both sides):

| Threshold | Measured (3 runs) |
|---|---|
| reload with state kept < 300 ms | median 3.67-3.95 ms (first 3.6-4.3) |
| bound update < 2 ms | median 0.0016 ms (first 0.006-0.011) |
| frame overhead < 1 ms (paired medians, 200 pairs) | +0.085 / +0.135 / +0.307 ms (script frame ~10.4-10.5 ms, native ~10.4-10.5 ms) |

`go vet`, `gofmt -l`, `go build ./...` clean; `go test ./gpui/... ./internal/parity/... -count=1` all ok. Not built (later tickets): host `setTimeout`/`fetch`/`Intl` globals, the generated `.d.ts` + check command, the dev command (Go rebuild/restart with state restore), plugin slots and budgets; no per-VM time budget or memory cap yet (goja can't cap memory — the plugin ticket's design choice).
