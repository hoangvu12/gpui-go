# 66 - Dev command: TSX reload and Go restart with state

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 62
Triage: ready-for-agent

**What to build:** A dev command watches .tsx files (reload in place) and Go files (rebuild and restart), saving keyed script state and the window position before a restart and restoring them after, like tauri dev.

**Blocked by:** 62 (TSX host tracer)

**Status:** ready-for-agent

- [ ] Editing a Go file restarts the app with the same keyed state and window position.
- [ ] Editing a .tsx file reloads in place without a restart.
- [ ] Edit-to-screen times recorded for both paths.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
