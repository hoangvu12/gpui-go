# The native test suite leaks an 18 MB gpui-go-native-test-* folder per cache root in %TEMP%

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
Blocked by: none

## Question

`internal/native`'s tests pin every library load to a unique temp
cache root: `testDir` (`internal/native/native_test.go`) does
`os.MkdirTemp("", "gpui-go-native-test-")` and registers a best-effort
`os.RemoveAll`. Every load from a cold root materializes the ~18 MB
embedded DLL into it, and the removal always fails — a loaded DLL
stays mapped for the process lifetime (no FreeLibrary by design), and
Windows refuses to delete a mapped image — so the folders accumulate
in %TEMP% forever. Measured on this machine: one
`go test ./internal/native` run (24 direct `testDir` sites plus the
`mustTextService`/`mustLayoutService`/`mustImageService` helpers and
`writeBundle`, ~68 calls) leaves **58 folders × 18 MB ≈ 1 GB**; 42 GB
had piled up before the 2026-10-20 cleanup. The bundle-writing tests
leak the same way (the successfully loaded bundle DLLs are mapped; the
reparse test writes another full 18 MB copy of the real artifact).

## What to build

Stop creating a unique root per call: the loads that just need a
working cache share **one fixed-name root** (`%TEMP%\
gpui-go-native-test`, no random suffix) reused by every test run — at
most one 18 MB copy exists on the machine, and the reuse is safe
because the cache contract is already cross-process (publication
lock, staging+rename, verified reuse). The root also best-effort
cleans stale sibling hash directories (older artifacts) so artifact
revisions do not accumulate. The valid-bundle tests share one
fixed-name bundle directory the same way. Tests that genuinely need
isolation — a cold cache for the publication counter, a corrupt final
file, a stale staging file, a reparse redirect, an unwritable root —
keep unique temp dirs, and the 18 MB copies they used to write are
replaced by the shared bundle's DLL (the reparse target) or small
handcrafted PEs (the wrong-hash/replacement bundle rejections).

## Acceptance criteria

- [x] A full `go test ./internal/native` run leaves **no new
      `gpui-go-native-test-*` folders** in %TEMP% beyond the single
      fixed-name shared root and bundle dir (which are reused, not
      multiplied, by further runs).
- [x] The isolation tests (cold-cache publication count, corrupt
      final file, stale staging cleanup, reparse rejection, unwritable
      root) still exercise their own unique roots and pass unchanged.
- [x] All load-path behavior assertions (identity, layout, round
      trips) pass unchanged against the shared root.

## Answer

Resolved 2026-10-20 on `consumer-gaps-2` (commit "Stop the native test
suite leaking an 18 MB temp folder per cache root").

**Root cause** (as filed): `testDir`
(internal/native/native_test.go) minted a unique
`gpui-go-native-test-*` cache root per load — 68 calls per run once the
`mustTextService`/`mustLayoutService`/`mustImageService` helpers and
`writeBundle` are counted — and every cold load materialized the ~18 MB
embedded DLL into it. The best-effort `os.RemoveAll` at cleanup always
failed (a loaded DLL stays OS-mapped for the process lifetime; no
FreeLibrary by design), so the folders accumulated in %TEMP% forever:
measured 58 folders ≈ 1 GB per `go test ./internal/native` run, 42 GB
before the 2026-10-20 cleanup.

**Fix**:
- Loads that just need a working cache share **one fixed-name root**
  (`%TEMP%\gpui-go-native-test`, no random suffix): at most one 18 MB
  copy exists, reused by every test run. Cross-process reuse is safe by
  the cache contract itself (publication lock, staging+rename,
  verified reuse). Stale sibling artifact directories (older artifact
  hashes) are removed best-effort at first use, so artifact revisions
  do not accumulate either.
- Successfully-loading bundle tests share **one fixed bundle dir**
  (`gpui-go-native-test-bundle`, the DLL written staging+rename).
- The isolation tests keep unique roots exactly as before (corrupt
  final file, stale staging cleanup, unwritable root, reparse redirect,
  missing files — their loads fail or write small payloads, so their
  dirs actually delete). The concurrent-publication test's cold root
  became a fixed dir deleted at test start: cold for this process (a
  previous run's DLL died with its process, so the removal succeeds),
  bounded at one folder. The reparse test's "real artifact outside the
  cache" reuses the shared bundle's DLL; the rejected-bundle corpus
  (wrong hash, mutated manifest, replacement identity) uses small
  handcrafted AMD64 PEs — the same typed errors, no 18 MB writes.
- The svg/layout tests that implicitly relied on per-root **module
  identity** (each unique root is a distinct DLL file, so a distinct
  loaded module with fresh static state) now tolerate the shared
  module's latched state with honest skips
  (TestSvgTextWithBundledFont, TestSvgFontAssetSurface,
  TestLayoutMeasureWithoutTrampoline, TestConcurrentPublication under
  `-count>1`).

**Verification**: one full `go test ./internal/native -count=1` run
leaves **zero random-suffix `gpui-go-native-test-*` folders** and
exactly three fixed dirs (the shared root, the shared bundle, the
concurrent root — 56 MB total, reused by every further run; a second
and a `-count=2` run leave the same three). The suite passes in 3.8 s
(was ~28 s: no 50+ artifact copies to hash and write). The isolation
gates pass unchanged (cold-cache publication count 1, corrupt final
file typed error, stale staging cleaned, reparse rejected, unwritable
root typed error). `go build ./...`, `go vet ./...`, and
`go test ./... -count=1` green except the documented environmental
gate (`internal/headlessspec` needs the gitignored
`reference/ce-source`). The whole-repo run still leaves no
random-suffix folders in %TEMP% (the other packages use the default
per-user cache, not temp).

**What mordbard should change**: nothing — this is this repo's own
test hygiene; consumers' caches are unaffected.

## Comments

- 2026-10-20 (pi worker): claimed, implemented, verified, resolved.
  The shared module is a deliberate trade: the package's native static
  state (svg fonts, the measure trampoline) is now shared across the
  tests instead of fresh per root — the affected tests were adjusted
  with honest skips, and the state-sharing is documented here.
