# 04 - CSS shadow and blur geometry

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 01
Triage: ready-for-agent

**What to build:** Box shadows and blur filters look like Chrome's: shadow blur uses sigma = blur/2, filter and backdrop-filter blur use sigma = radius, spread grows the corner radii, the first shadow in the list paints on top, and shadows and the ring paint inside the filter group (D8; pin values verified in the spec).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Harness fixtures for: a shadow with blur, a shadow with spread on a rounded box, a multi-shadow list, filter: blur, backdrop-filter: blur.
- [ ] Each fixture matches Chrome within the pixel tolerance.
- [ ] Changed bit-exact fixtures carry a 'divergence toward CSS' note.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
