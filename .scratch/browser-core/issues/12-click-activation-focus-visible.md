# 12 - Click on the common ancestor, activation and focus-visible

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** A click whose press and release land on different children goes to their common ancestor; Enter activates on key-down; Space keeps :active while held; :focus-visible follows the focus origin, and modified keys like Ctrl+S don't count as keyboard focus (D12).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Event-log fixtures for each rule match Chrome.
- [ ] focus-visible fixtures: click focus shows no ring, Tab focus shows it, Ctrl+S after a click doesn't.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
