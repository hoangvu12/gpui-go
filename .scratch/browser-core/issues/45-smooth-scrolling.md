# 45 - Smooth scrolling

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 9 (Claude Code); verified by orchestrator 10
Blocked by: 15, 44
Triage: ready-for-agent

**What to build:** Wheel, keyboard and programmatic scrolls animate with Chrome's curve by default, and jump instantly under reduced motion (D13).

**Blocked by:** 15 (Wheel latching, overscroll contain and keyboard scrolling), 44 (Transition style property and the core motion engine)

**Status:** ready-for-agent

- [x] Timed scroll-offset samples match Chrome's curve within tolerance.
- [x] Reduced motion gives instant scrolls.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-25, orchestrator 10: verified and merged (f773a9f, merge e52bfd0). The merge needed one fix: `pointSub` was declared twice (19's text_selection_band.go and 45's scroll_smooth.go); the scroll_smooth.go copy was dropped. On the merged tree `go test -p 1 -count=1 ./gpui/ ./internal/parity/... ./base ./internal/layoutspec ./examples/zterm`: all 43 packages ok. Spot-check: the duration constants (kConstantDuration 9, kDurationDivisor 60, the inverse-delta ramp 120-480 px over 12-6 sixtieths) match cc/animation/scroll_offset_animation_curve.cc:17-47 at 0d4dcd4f. Limitation accepted: Chrome can't be sampled mid-animation (impl-side cc animations), so the records pin end states and the curve is pinned by unit tests against Chromium's formulas.

## Answer

**API**

- `App.SmoothScrolling()` / `App.SetSmoothScrolling(bool)` (gpui/app.go:366, 372) — the D13 pref, ON by default in a real application; the effective gate is the pref AND `App.MotionReduced()` (gpui/scroll_smooth.go:478 smoothScrollAnimated — ticket 74's one reduced-motion source).
- `ScrollHandle.ScrollTo(offset, behavior, w, app)` + `ScrollBehavior` auto/instant/smooth (gpui/scroll.go:300, 324): smooth runs the programmatic curve (target clamped into the handle's range, a running user animation cancelled first, chromium scrollable_area.cc:519-525); auto/instant are the plain write. `SetOffset` keeps its shape but now adjusts (not cancels) a running animation (crbug.com/1264266).
- Wheel (line/page, non-precise) and keyboard scrolls animate by default; precise (pixel/touchpad) deltas stay instant and cancel a running animation, exactly as in Chrome. The uniform list's wheel path animates too. `ScrollToItem`/`ScrollToBottom`/`ListState.ScrollTo` stay instant (documented limitation).
- `TestApp` runs with smooth scrolling OFF by default — the deterministic twin of the recorder's Chrome (`--disable-smooth-scrolling`), so every existing scroll test and fixture keeps its instant shape — and `TestApp.WithSmoothScrolling(true)` (gpui/testapp.go:141) opts in. The parity replay opts in from a fixture's `SmoothScrolling` field (internal/parity/fixture.go:371; the recorder adds `--enable-smooth-scrolling`, internal/parity/recorder.go:134; the replay opts the app in, internal/parity/replay.go:91).

**Engine** (gpui/scroll_smooth.go, new): the port of cc::ScrollOffsetAnimationCurve plus its animator — one animator per scroller next to the shared offset cell (the handle's state or the div's element state, so wheel/keyboard/programmatic share one animation like Chromium's per-ScrollableArea ScrollAnimator). Durations per source (smoothScrollSegmentDuration, :203): keyboard 150ms constant; wheel the inverse-delta ramp (200ms at ≤120px down to 100ms at ≥480px); programmatic sqrt(|delta|)/60 capped at 1500ms. Curves (:139): ease-in-out (0.42, 0, 0.58, 1) for wheel/keyboard, the tweaked (0.4, 0, 0, 1) for programmatic. Retarget = UpdateTarget (retargetTo, :293): the new segment starts at the current animated position, duration = the source rule bounded by the velocity bound, bezier y1 scaled by the initial slope to preserve velocity. The tick (smoothScrollTick, :450) samples at App.Now, writes the offset cell, notifies the view, and requests the next frame only while the animation runs (44's rule); the reduced-motion/disabled gate adopts the target at once. Anchoring and instant writes shift the curve (adjust, :345) instead of cancelling (ticket 46's requirement — pinned by TestSmoothScrollAnchoringAdjustsARunningAnimation). The bezier solver's slope (needed for velocity) was extracted as cubicBezierParts (gpui/motion_easing.go:351), shared with CubicBezier.

**Files**

- gpui/scroll_smooth.go (new): the engine above; gpui/scroll_smooth_test.go (new): the curve fits.
- gpui/scroll_element.go:314 the wheel listener's smooth branch (create target clamp(current+delta), retarget clamp(target+delta), ≤0.1px instant, precise instant+cancel); :193 the clamp's kClamping arm adjusts a running animation.
- gpui/key_default_action.go:687 applyScroll's smooth branch (blink's target: the running animation's target plus the clamped delta — scroll_animator.cc:88-140 — so two quick presses accumulate like Chrome's).
- gpui/listelement.go:574-655 the list's smooth wheel path (the animator on listStateInner, the tick driving the list's own scroll body so the logical rebasing, eager clamp and scroll handler all run).
- gpui/scroll_anchor.go:366 the anchoring adjust; gpui/app.go, gpui/testapp.go the gates; internal/parity/{fixture,recorder,replay}.go the harness flag; docs/browser-parity-harness.md the guide.

**Fixtures** (internal/parity/fixtures/smoothscroll, registered in replay_test.go and cmd/parityrecord)

- `smoothscroll-keyboard` (Chrome 155, recorded with `--enable-smooth-scrolling`): click-to-focus + two ArrowDown (40px each, the second retargets the running animation to 80px) + settle past the 150ms animation — Tier A exact at the end state, Tier C flat colour. Wrong view (never-scrolling scroller) fails Tier A on exactly the moved rows.
- `smoothscroll-programmatic`: a page `scrollTo({behavior:"smooth"})` against the replay's `ScrollTo(ScrollBehaviorSmooth)` — end state exact.
- `TestKeyboardScrollAnimatesThroughTheMidpoint` (smoothscroll_test.go): the same fixture with Wait(75) reads the offset at exactly the ease-in-out midpoint (-20 of the 40px step) — the harness-level proof the port animates; an instant scroll reads -40.

**Key citations** (sparse checkout 0d4dcd4f)

- Durations and curves: cc/animation/scroll_offset_animation_curve.cc:17-18 (9/60, divisor 60), :28-34 (ease-in-out points), :44-54 (inverse-delta ramp), :148-178 (EaseInOutSegmentDuration); cc/base/features.cc:258-266 (the tweaked programmatic bezier + 1500ms cap on non-Android).
- Retarget: scroll_offset_animation_curve.cc:337-396 (UpdateTarget), :70-82 (slope-preserving y1), :81-99 (velocity bound); the wheel update path cc/animation/scroll_offset_animations_impl.cc:118-135 (target = clamp(curve target + delta)); the create path cc/trees/layer_tree_host_impl.cc:4684-4722 (clamp(current+delta), ≤0.1px instant).
- Precise never animates: cc/input/input_handler.cc:2484-2492 (ShouldAnimateScroll) and :149-161 (ScrollBegin cancels); blink scroll_animator.cc:99-103.
- Reduced motion: content/browser/web_contents/web_contents_impl.cc:4042-4046 (enable_scroll_animator = flag OR (not flag AND ScrollAnimationsEnabledBySystem())) + ui/gfx/animation/animation_win.cc:17-44 (Windows: SPI_GETCLIENTAREAANIMATION off = reduced motion = no smooth scrolling) — D13 matches Chrome on Windows for user AND programmatic scrolls (scrollable_area.cc:519-521), so the port follows the spec and the source.
- The "impulse curve" is not in the pinned commit (AnimationType is {kLinear, kEaseInOut}, scroll_offset_animation_curve.h:114; upstream main today also only has those two) — the sole trace is the UpdateTarget comment at :363 ("Impulse-style animation curves"), a forward reference. The port implements the ease-in-out curve the tree ships.
- Adjust-not-cancel: scrollable_area.cc:567-570 (crbug.com/1264266), ApplyAdjustment at scroll_offset_animation_curve.cc:300-305.

**Limitations**

- No timed mid-animation sample can be RECORDED from Chrome: its scroll animations are impl-only compositor cc animations (not document.getAnimations(), so the Wait step cannot seek them), and CDP-dispatched wheels are kScrollByPrecisePixel (content/browser/devtools/protocol/input_handler.cc:484), which never animates. The records pin the end states; the mid-flight curve is pinned by the port-side midpoint test and the unit tests' Chromium-formula fits (gpui/scroll_smooth_test.go: durations exact, the symmetric midpoint exact, later samples against an independent bezier solver, retarget invariants, precise-instant, reduced motion, frames-only-while-running, anchoring-adjusts, list curve).
- Chromium's one-frame animation start offset (layer_tree_host_impl.cc:4710-4714) is not ported (the port's first sample is the next delivered frame — the same latency shape without the explicit offset); the delayed_by plumbing degenerates to zero (no delayed scroll starts in the port).
- ScrollToItem/ScrollToBottom and ListState's programmatic scrolls stay instant (the CSS scroll-into-view family would be its own follow-up); the keyboard walk covers div scrollers only (unchanged from 15). A dropped scroller's pending tick runs out its bounded duration (≤1.5s) instead of aborting.
- Find-in-page's kUser scroll filtering (scrollable_area.h:754-783) is not ported (no find-in-page).
