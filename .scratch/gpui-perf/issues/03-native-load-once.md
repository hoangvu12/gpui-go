# Load and verify the native DLL once per process

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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
