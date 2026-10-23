# Load and verify the native DLL once per process

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: gpui-perf W1
Blocked by: none

## Question

`native.Load` isn't memoized. About 10 services each call it (for example `gpui/scene_kernel.go:37`). Every call re-parses the 1.1 MB manifest, SHA-256s the embedded 18.9 MB DLL (`internal/native/native.go:228`, `verify.go:31`) and re-reads and re-hashes the cached copy (`cache.go:142-143`).

Make it process-once:

- The first call verifies, and later calls reuse the loaded module.
- Keep the error semantics: a failed load must still report the same error.
- Keep the verification guarantees and the existing loader tests.

Measure startup before and after: time to the first service ready, and to the first frame.

## Comments

## Answer
`native.Load` now shares one verified load per source while any handle to it is open (`internal/native/native.go`, `sharedLoad`). A source is a bundle directory, or the embedded artifact under a cache root.

- **First load:** the first `Load` of a source does the full work: it parses the manifest, hashes the embedded bytes, materializes the file, hashes it through the retained deny-write handle, then runs `LoadLibraryExW` and validates the ABI table.
- **Later loads:** later `Load` calls for that source reuse the module, the table and the retained verified handle. They still return a fresh `*Library` with its own release state and generation token, so `Close`, `ErrDoubleRelease` and `ErrClosed` behave exactly as before for each handle.
- **Last close:** the last `Close` releases the shared verified handle and drops the entry. The next `Load` verifies from scratch, so the tamper and staging tests still exercise the full path.
- **Failures:** failed loads are never registered. Every failing call runs the load and reports the same typed error.

A single mutex covers the loads. `materialize` was already serialized by `pubMu`, so this adds no new contention. The gpui services still each call `native.Load` once (they keep `sync.Once`). Only the first of those calls pays for verification now.

New tests in `internal/native/loadonce_test.go` cover the shared module and handle, distinct generations, close-one-keeps-the-other, re-verification after the last close, and a failure reported on every call. All existing loader tests pass.

Startup was measured in fresh processes, 11 per side, with `gpui/perf_startup_test.go` (`TestPerfStartupReport`). Evidence: `evidence/perf/ticket03-startup-{before,after}.json`.

| | before (median / min) | after (median / min) |
|---|---|---|
| first service ready (`NewScene`) | 59.3 / 56.5 ms | 63.9 / 56.9 ms (unchanged: still one full verification) |
| first frame (1,000-div window, real text) | 391.5 / 355.2 ms | 248.3 / 230.1 ms (−37%) |

What remains in the first service is the single verification: hashing the 18.9 MB embedded artifact plus the cached copy. The ticket requires keeping that.
