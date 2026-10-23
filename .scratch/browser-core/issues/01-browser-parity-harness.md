# 01 - Browser-parity harness (tracer)

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: none
Triage: ready-for-agent

**What to build:** A maintainer can write one fixture: a small HTML/CSS page, the equivalent gpui-go view and a scripted input sequence. A recorder captures the page in headless Chrome (element boxes, the event log for the input sequence, a screenshot) as checked-in fixture files. A replay test runs the gpui-go view in the headless test window with the real text system and simulated input, and compares boxes exactly, event logs exactly and pixels approximately. This is the main test seam of the spec.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [ ] The recorder runs headless Chrome (no visible window) and writes boxes, event log and screenshot fixtures, recording the Chrome version.
- [ ] Replay compares boxes as Tier A (exact), event logs as a Tier B transcript (exact) and pixels as Tier C, with a per-fixture tolerance documented as provisional (research 19).
- [ ] One tracer fixture passes end to end: a padded, bordered box with text and a click handler.
- [ ] A deliberately wrong gpui-go view makes each of the three comparisons fail with a readable diff.
- [ ] Writing a new fixture is documented in a short guide.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
