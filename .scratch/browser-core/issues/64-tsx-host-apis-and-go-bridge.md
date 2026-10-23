# 64 - TSX host APIs and the Go bridge

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 62
Triage: ready-for-agent

**What to build:** Scripts get host-provided timers, fetch and the other browser-ish globals goja lacks, and can invoke Go actions and read and write Go shared state.

**Blocked by:** 62 (TSX host tracer)

**Status:** ready-for-agent

- [ ] Tests for setTimeout/clearTimeout, fetch (against a local server) and action/shared-state round trips.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
