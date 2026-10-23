# 38 - White-space collapse, tab size and em-based letter spacing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** white-space normal, nowrap and pre-line collapse whitespace like CSS with a correct caret byte map, tabs are 8 spaces under pre, and em letter spacing works.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures for each white-space value match Chrome's boxes and line breaks.
- [ ] Caret positions map correctly into collapsed text (text geometry test).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
