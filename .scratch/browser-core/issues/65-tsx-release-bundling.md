# 65 - TSX release bundling

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code)
Blocked by: 62
Triage: ready-for-agent

**What to build:** Release builds compile the .tsx files with esbuild at build time and embed the bundle; a compile error fails the build with file:line; the shipped app reads no scripts from disk and doesn't include esbuild.

**Blocked by:** 62 (TSX host tracer)

**Status:** ready-for-agent

- [x] A release build of a sample app runs from the embedded bundle with the sources removed.
- [x] A planted error fails the build with file:line.
- [x] Binary size is recorded.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments
- 2026-10-24, orchestrator 7: verified and merged (7b1c0f1). `go list -deps ./examples/tsx-hello` links goja but no esbuild and no gpui/script/compile. A planted unclosed JSX in a copy of the sample makes `go generate` fail with `app.tsx:4:1: Unexpected end of file before a closing "div" tag` and exit 1. `-selftest` draws into gpui.NewTestWindow, which has no platform host, so no OS window is created. `go test ./gpui/script/... ./cmd/tsxbundle ./examples/tsx-hello` passes.

## Answer

**The build step: `go generate` + `cmd/tsxbundle`.** Go has no build.rs and `go build` never runs code, so the idiomatic pre-build hook is a go:generate directive that `go run`s the module's own tool (versioned with it, nothing installed) — chosen over a Makefile/CI-only step because it is declarative, hermetic and discoverable in the app's source, and over a build tag because ticket 62's import-graph exclusion already keeps esbuild out of release builds without one. `cmd/tsxbundle` compiles the entry with esbuild **through `gpui/script/compile`** (one place owns the ADR's compile contract: ES2017, IIFE on `gpui_script`, inline source map) and writes three files beside the app's main package, named after the var:

```
//go:generate go run github.com/hoangvu12/gpui-go/cmd/tsxbundle -var UI -entry ui/app.tsx
ui.tsxbundle.js       the compiled bundle (inline source map)
ui.tsxbundle.go       the script.EmbeddedBundle var //go:embed picks up
ui.tsxbundle_test.go  the staleness guard
```

`go:embed` cannot cross the package directory, and `go generate` runs in the directive's, so the files land there and `-entry` is package-relative — the generated files are committed (like `gpui/styled_generated.go`), regeneration is byte-idempotent (`writeIfChanged`), and a compile error fails the step (and any build running it) with the esbuild `file:line:column` — zgui's `build::bundle` failing the Cargo build is the same shape. New in `compile`: `Build(entry) (Bundle, []string, error)` — Files' compile plus the metafile's input list (entry and every imported module), which is what the tool hashes.

**The loader API: `script.EmbeddedBundle`** (`gpui/script/embed.go`) — `{Name, JS, Tool, Sources []EmbeddedSource{Path, Hash}}` with `Bundle()` (the `Host.Load` argument) and `CheckFresh(root)`. The generated Go file carries the `//go:embed` var (`UIJS`) and `UI = script.EmbeddedBundle{...}` with every source's SHA-256 (`script.HashSource`, shared by generator and checker so they cannot drift); the app does `host.Load(UI.Bundle())`. The host package gains no esbuild/goja surface change — `Bundle` was already the load input, so release embedding is data, not code. The shipped app imports neither the tool nor `compile`: **esbuild stays out of the release import graph** (proved below), and the binary reads no scripts from disk.

**Stale-bundle handling.** `go build` does not rerun `go generate`, so an edited `.tsx` alone cannot fail a build — the guard is the generated **test** (`TestUIIsFresh`), the Go-idiomatic place where the build fails: it calls `UI.CheckFresh(".")`, which re-hashes the recorded sources and fails naming the changed/gone file plus `go generate ./...` as the fix. New imports are caught too (the importing file's hash changed); an unrecorded file beside the sources is not. The generated test is part of the bundle family — regenerating replaces it, deleting it is a choice an app makes deliberately. zgui gets this for free from cargo's rerun-if-changed; a test is Go's equivalent, and it runs in CI's `go test ./...` before any release.

**The sample app: `examples/tsx-hello`** — the release shape, in full: `ui/app.tsx` (JSX, className, style object, keyed state + bound label + onClick), the go:generate directive, the committed generated files, and a `-selftest` mode that loads the embedded bundle, renders one frame in an **offscreen test window** (deterministic, no HWND, no platform host — nothing visible, no DLL needed), checks the card's laid-out bounds, clicks once through the host and asserts the bound label updated — proving the embedded script *runs*, not merely parses — then exits 0. The windowed mode is a normal app (`WithoutBanner` + `WithErrorHandler`, per ADR 0003's "release errors go to an app hook"); ticket 66's dev command reuses this app shape with `compile.Open` + reload instead of the embed.

**Tests** (test-first; all through public APIs, headless, no visible windows, no `GPUI_REAL_INPUT_TESTS`):

- `cmd/tsxbundle/main_test.go` — the three generated files exist, parse (`go/parser`) and carry the embed directive, the EmbeddedBundle var, the hashed sources and the CheckFresh test; the recorded hash equals `script.HashSource`; regeneration is byte-identical; **a planted unclosed-JSX error fails the step with `view.tsx:3:` and writes nothing**; a missing entry and bad flags fail; and the real command (`go run .`, what go:generate drives) exits 1 with the `file:line` on stderr.
- `gpui/script/embed_test.go` — an EmbeddedBundle loads through `Load(Bundle())`; CheckFresh passes while sources match, flags an edit and a removal (naming the file + the fix), ignores unrelated files, passes with no recorded sources; HashSource's shape and stability.
- `gpui/script/compile/compile_test.go` — Build reports the entry and its imports (sorted, entry-dir-relative) and fails like Files on a syntax error.
- `examples/tsx-hello/main_test.go` — **the release build to a temp dir runs `-selftest` with `ui/` renamed away, from an empty working directory** (exit 0); **`go list -deps` of the app contains no esbuild and no `gpui/script/compile`, while goja (the host) is present**; the staleness guard passes on the committed bundle and fires on a planted late edit; the generated `TestUIIsFresh` runs on every `go test`.

`go vet ./...`, `gofmt -l` on every touched file, `go build ./...` clean; `go test ./gpui/script/... ./internal/parity/... ./cmd/tsxbundle/ ./examples/tsx-hello/ -count=1` all ok (portfixture's pre-existing TestFx0004 font-count failure is untouched).

**Measured release binary sizes** (`go build -trimpath`, Windows):

| Binary | Plain | Stripped (`-ldflags "-s -w"`) |
|---|---|---|
| `examples/hello` (no script host) | 29.1 MB | 26.1 MB |
| `examples/tsx-hello` (script host + embedded bundle) | 46.1 MB | 38.4 MB |
| the host (goja) | +17.0 MB | +12.4 MB |
| embedded bundle JS (`ui.tsxbundle.js`) | 3.7 KB | — |
| `cmd/tsxbundle` (esbuild+goja; dev tool, never in the app) | — | 37.1 MB |

The +12.4 MB stripped matches ticket 62's measured +11.5 (a plain-Go app vs a script app; the delta is goja, ADR 0003's ~12 MB prediction).

Not built (later tickets): the dev command (66 — watches .tsx, reloads in place; Go rebuild/restart with state restore), the `.d.ts` + `check` (63), plugins (67). No per-app config of the compile contract yet — the ADR's floor is the only shape `compile.Build` emits.
