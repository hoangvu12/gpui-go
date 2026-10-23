# 03 - Missing cursors and frames for hidden windows

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: none
Triage: ready-for-agent

**What to build:** The grab, grabbing, copy and context-menu cursor styles show the right Windows cursor, and minimized or occluded windows stop drawing frames.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [ ] Each of the four cursor styles maps to a loaded system cursor (test of the cursor table).
- [ ] A minimized or occluded window schedules no frames until it is visible again; an animation resumes correctly afterwards.
- [ ] Existing frame-loop tests still pass.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
