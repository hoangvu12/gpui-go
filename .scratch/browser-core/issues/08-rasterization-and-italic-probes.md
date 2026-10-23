# 08 - Text rasterization and italic probes against Chrome

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** A measured comparison of gpui-go text against Chrome: dark, light and grey text, grayscale and ClearType, plus synthetic italic slant. The result decides whether gamma/contrast shader work (R17) is needed.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] A probe report with captures and numbers is committed under research.
- [ ] The report states a clear yes/no on R17 text shader work and on italic slant, with the evidence.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
