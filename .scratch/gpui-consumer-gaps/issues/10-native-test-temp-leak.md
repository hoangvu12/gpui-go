# The native test suite leaks an 18 MB gpui-go-native-test-* folder per cache root in %TEMP%

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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

- [ ] A full `go test ./internal/native` run leaves **no new
      `gpui-go-native-test-*` folders** in %TEMP% beyond the single
      fixed-name shared root and bundle dir (which are reused, not
      multiplied, by further runs).
- [ ] The isolation tests (cold-cache publication count, corrupt
      final file, stale staging cleanup, reparse rejection, unwritable
      root) still exercise their own unique roots and pass unchanged.
- [ ] All load-path behavior assertions (identity, layout, round
      trips) pass unchanged against the shared root.
