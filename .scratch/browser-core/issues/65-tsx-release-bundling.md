# 65 - TSX release bundling

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 62
Triage: ready-for-agent

**What to build:** Release builds compile the .tsx files with esbuild at build time and embed the bundle; a compile error fails the build with file:line; the shipped app reads no scripts from disk and doesn't include esbuild.

**Blocked by:** 62 (TSX host tracer)

**Status:** ready-for-agent

- [ ] A release build of a sample app runs from the embedded bundle with the sources removed.
- [ ] A planted error fails the build with file:line.
- [ ] Binary size is recorded.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
