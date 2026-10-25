# 71 - Flip the default display to block

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: 69, 70
Triage: ready-for-agent

**What to build:** The default display becomes block, as in browsers (D6, contract step).

**Blocked by:** 69 (Codemod applied in gpui-go), 70 (Codemod applied in Base, zterm and the gallery)

**Status:** ready-for-agent

- [ ] Fixtures with plain divs match Chrome's block layout.
- [ ] All suites still pass because callers state Flex explicitly; the divergence is noted.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
