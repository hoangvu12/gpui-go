# 78 - Spec regressions on main outside the batch set

Parent: ../map.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: none
Triage: ready-for-agent

**What to build:** Four tests fail on main (d7a5b80) in packages that the per-batch set doesn't run. Find the commit that broke each, then fix the cause, or update the test when the new behaviour is the intended browser rule.

- internal/inlinespec `TestInlineDivPlacesElementChildrenInTextFlow`: paragraph text = "prefix suffix", want "prefix  suffix" (likely 38's white-space collapse).
- internal/listspec `TestUniformListRetainsFocusedItemDispatch`: key deliveries after the item returns = 2, want 3.
- internal/textframespec `TestTextUnderAnimationRedrawsBounded`: LiveShapingHandles after the first frame = 21, want <= 14 (two per text).
- internal/textframespec `TestResizingWindowDoesNotLeakSupersededLines`: 9, want <= 6.

- [ ] Each failure is bisected to a commit, and the cause is fixed or the test updated with the reason.
- [ ] inlinespec, listspec and textframespec pass; the batch set gains them.

Found by orchestrator 10 while merging 68 (the 37 and 68 workers saw them on their baselines too).

## Comments

## Answer
