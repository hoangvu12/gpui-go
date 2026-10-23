# 76 - Flip the default position to static

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 33, 75
Triage: ready-for-agent

**What to build:** The default position becomes static, so absolute resolves against the nearest positioned ancestor everywhere (D3, second step), with the lint guarding remaining code.

**Blocked by:** 33 (position: static (opt-in), containing block and lint), 75 (Split out gpui-go-base (local))

**Status:** ready-for-agent

- [ ] Fixtures with default positioning match Chrome.
- [ ] gpui-go and gpui-go-base suites pass; the lint reports nothing in either repo.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
