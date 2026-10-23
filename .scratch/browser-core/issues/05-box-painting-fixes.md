# 05 - Box painting fixes: clip inset, radius scaling, object-fit

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Overflow clipping always insets by the border widths, overflowing unequal corner radii are scaled by one common factor as in CSS, and images with object-fit: none are centred and fitted to the content box.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures: a bordered overflow-hidden box with a child at the edge, a box with radii that overflow, an image with object-fit none/contain/cover inside padding.
- [ ] All match Chrome; changed fixtures carry divergence notes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
