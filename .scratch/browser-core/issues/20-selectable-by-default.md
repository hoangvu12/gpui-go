# 20 - Text selectable by default, with UserSelect

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 19
Triage: ready-for-agent

**What to build:** Text is selectable by default with an automatic I-beam over it, and UserSelect(None) opts out (D7).

**Blocked by:** 19 (Core text selection on static text)

**Status:** ready-for-agent

- [ ] Fixtures: plain text shows the I-beam and selects; a UserSelect(None) subtree doesn't.
- [ ] Buttons and other interactive elements in examples opt out where Chrome's UA style would (documented).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
