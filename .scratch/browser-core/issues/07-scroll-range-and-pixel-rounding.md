# 07 - Scroll range and pixel rounding

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** A scroll container's range covers all descendant overflow and trailing margins (from the layout engine's content size), vertical padding percentages resolve against the containing block's width, border widths are floored to device pixels, and sub-device-pixel lengths keep one device pixel.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures: nested overflowing content with a bottom margin scrolled to the end; padding-top in percent; 1px border at 175% scale; a 0.5px hairline at 100%.
- [ ] Scroll extents and boxes match Chrome exactly.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer
