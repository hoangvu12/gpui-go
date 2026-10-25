# 75 - Split out gpui-go-base (local)

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: 71, 72, 73, 74
Triage: ready-for-agent

**What to build:** Base, zterm and the gallery move into a new local gpui-go-base repository that depends only on gpui-go's public API; the line wrapper moves with Base; gpui-go keeps one core-only example. zterm and the gallery are test apps and are never pushed. Creating the public GitHub repo and any push need the user's go-ahead at that step.

**Blocked by:** 71 (Flip the default display to block), 72 (Base overlays rebuilt on core primitives), 73 (Base focus, selection and editing on core primitives), 74 (Base motion on the core engine)

**Status:** ready-for-agent

- [ ] gpui-go builds and passes its suites with no Base inside.
- [ ] gpui-go-base builds against gpui-go and its tests pass; zterm and the gallery run offscreen.
- [ ] The user's go-ahead for creating the GitHub repo is recorded before any remote step.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
