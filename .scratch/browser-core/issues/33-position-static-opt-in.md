# 33 - position: static (opt-in), containing block and lint

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** PositionStatic exists, and absolute elements resolve against the nearest positioned ancestor when their parents opt in; a lint reports absolute under a non-positioned parent. The default flips later (ticket 76) (D3).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures with absolute inside static and relative ancestors match Chrome.
- [ ] The lint flags a sample and passes on a corrected one.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
