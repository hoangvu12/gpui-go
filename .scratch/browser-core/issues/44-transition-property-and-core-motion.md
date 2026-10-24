# 44 - Transition style property and the core motion engine

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 27
Triage: ready-for-agent

**What to build:** A Transition style property animates style changes like CSS transitions, driven by a per-element style diff; the timing, easing and keyframe-interpolation engine (with CSS's reversing rule) moves from Base into the core; reduced motion is honoured.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion)

**Status:** ready-for-agent

- [x] Timed fixtures (test clock) for transition start, interruption and reversal match the CSS spec's values.
- [x] Reduced motion makes transitions complete immediately.
- [x] Base compiles against the moved engine (Base migration itself is ticket 74).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

**API**

- `Style.Transition []TransitionRule` (gpui/style.go:958) + `Styled.Transition(rules...)` (gpui/styled.go:843) + the refinement entry (gpui/styled_refinement.go:181, replaces wholesale, not inherited) — CSS `transition: <property> <duration> <timing-function> <delay>`.
- `TransitionRule{Property, Duration, Delay, Easing}` with `NewTransitionRule` (the CSS shorthand defaults: `ease`, no delay), `WithDelay` (SignedDuration — negative starts partway) and `WithEasing` (gpui/transition.go:125-157). The interpolable set: `TransitionAll/Opacity/Background/Width/Height/BorderColor`; the last matching rule wins; combined delay+duration ≤ 0 starts nothing.
- The per-element style diff (gpui/transition.go:396 resolveStyleTransitions) runs at the div's three style resolutions (gpui/div.go:1565, 1670, 1879): the first resolution of a frame (draw-count guard) diffs, the later ones sample; animated width/height override the resolved style as px lengths (feeding the layout node), opacity/backgrounds/border colours for paint. Requires an element ID (panics otherwise, like the pinned stateful styles); elements without a Transition pay one length check (unit-tested: no frames requested). One `RequestAnimationFrame` per frame while delayed or running.
- Reduced motion: `App.ReduceMotion() || SystemPreferences().ReducedMotion` (gpui/motion.go:reduceMotionActive — 27's live record) completes transitions immediately.

**Moved from Base** (no behaviour change; base/motion.go now aliases): the timing/easing/keyframe engine — `Easing` + cubic-bezier solver + steps (gpui/motion_easing.go), `Timing`/`SignedDuration` (gpui/motion_timing.go; the pin's `MotionPhase` renamed `TimingPhase` — gpui's own `MotionPhase` is the pinned duration-motion's), `Keyframes`/`Discrete` (gpui/motion_keyframes.go), and the value-transition machinery with the CSS reversing rule: `Transition`, `MotionTransition`/`TransitionWithStatus`, `AnimateKeyframes`, `Interpolate`/`Lerp`/`InterpolateValue`, `CubicBezier`, `EaseOutCubic` (gpui/motion.go). Springs and `MotionTransform` stay in Base (the research split's component half). Base keeps its whole public surface through aliases; its motion/easing/timing/keyframes tests pass unchanged.

**Chromium citations** (sparse checkout 0d4dcd4f)

- Transition start from the before/after-change style diff: core/animation/css/css_animations.cc:3105 (`CalculateTransitionUpdate`, diff at :3062-3075).
- Last rule wins / combined duration+delay ≤ 0 starts nothing: css_animations.cc:2870-2885.
- The reversing rule (adjusted start value, shortening factor, duration × factor, negative delay × factor): css_animations.cc:2890-2912, eased progress from timing.cc:226-235 (`progress` = the transformed progress).
- cubic-bezier: ui/gfx/geometry/cubic_bezier.cc:181-236 (Newton ≤ 4 + bisection) wrapped at platform/animation/timing_function.cc:132-137.
- steps: ui/gfx/animation/keyframe/timing_function.cc:98-133 (floor(steps·t+offset)/jumps) + :136-150.
- Which properties interpolate: core/css/properties/css_property.h:87 (`IsInterpolable`, flag at :217); transition keyframes built from interpolable values at css_animations.cc:2918-2940.

**Colour interpolation**: sRGB channelwise (the rgb() midpoint — red→blue 0.5 = rgb(128,0,128)), via the Hsla→Rgba round trip, not HSL (gpui/transition.go:animatedValue.lerp; unit-tested).

**Fixtures** (internal/parity/fixtures/transition, registered in replay_test.go and parityrecord)

- `transition` (Chrome 155 record): click + 200ms sample of width linear (150px — Tier A exact), width steps(4) 800ms (125px — Tier A exact), width ease (180px, Tier C — the float32 bezier lands within one device pixel of Chrome's 180.16), opacity 1→0 (0.5 midpoint) and background red→blue (sRGB midpoint) — Tier C. Measured 0.026 mean/0 hot; tolerance 0.1/1.0/0.004.
- `transition-reverse`: click, 200ms, click, 100ms — the reversing-shortened return reads 125px, matching Chromium's own rule (Tier A + Tier C + Tier B).
- Wrong-view test: an 800ms-linear box fails Tier A with "recorded 150x30, replayed 125x30" (transition_test.go).
- Recorder time control: the virtual-time budget does NOT drive the CSS transition timeline in `--headless=new` (measured: transitions sit unstarted at currentTime 0 during a budget — no frames are produced), so a wait step pauses and seeks the page's animations (`currentTime += ms`, the Web Animations clock; recorder.go, docs/browser-parity-harness.md). The replay's twin: `TestApp.AdvanceClock` + one delivered animation frame (replay.go).

**Tests**

- gpui/transition_test.go: linear/steps/ease samples, delay and negative delay, the reversal shortening (150 → reverse at 200 → 150 again at +100 → 100 at +200), retarget-from-current, reduced motion, no-ID panic, zero cost without rules, the sRGB colour midpoint, rule matching/gating, painted opacity+background at the midpoint.
- Base's motion/easing/timing/keyframes test files pass unchanged against the aliases.

**Limitations**

- No property-only frames: a transition re-renders its view every frame (the fast path is research 02 gap 2); an element inside a `CachedView` freezes mid-flight while its view is cached.
- Width/height interpolate definite px/rem lengths only (percentages and auto are discrete — CSS interpolates them as calc() mixes the scene cannot represent); gradients do not transition; no element transforms yet (gap 4, so "transform-like" is the size pair).
- Reduced motion is a deliberate app-policy kill switch (the browser only exposes `prefers-reduced-motion` to authors — Chromium does not stop CSS transitions for it), so it is unit-tested, not a parity fixture.
- The reversing rule's `delay × factor` applies only to negative delays, per css-transitions-1.
