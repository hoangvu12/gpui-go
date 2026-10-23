# 68 - Codemod: explicit Flex where the default was relied on

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: none
Triage: ready-for-agent

**What to build:** A codemod adds .Flex() to every element that relies on the current default display, so flipping the default later changes nothing for existing code (expand step of the D6 flip).

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [ ] The codemod is idempotent and leaves elements with an explicit display unchanged.
- [ ] Running it on a sample package produces no behaviour change (offscreen captures identical).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
