# 61 - FLIP helper

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: open
Assignee: unassigned
Blocked by: 44, 56
Triage: ready-for-agent

**What to build:** A FLIP helper animates layout changes by diffing geometry and running a transform transition, replacing hand-written slide code like zterm's.

**Blocked by:** 44 (Transition style property and the core motion engine), 56 (Transforms with hit testing)

**Status:** ready-for-agent

- [x] A reorder animation test (test clock) shows elements moving from old to new positions.
- [x] zterm's Flip helper can be replaced (follow-up note).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Landed on `core/61-flip-helper` (Go-only, no native changes; built on the merged 56).

**The API (`gpui/flip.go`)**

- `gpui.NewFlip(duration, easing) *Flip` — the group: one per animated arrangement, kept in the view's state like a ScrollHandle. `WithScale()` opts into size changes (a scale about the element's transform-origin); the default is translate-only. `Arm()` marks the group's next placements as moves — call it in the update that changes the arrangement (a reorder, an insert, a drop) or the render that detects it; unarmed changes follow the layout at once, so scrolls, resizes and other views' changes never slide.
- `Div().ID("row").Flip(f)` — the div hook (`DivElement.Flip`, a plain builder method beside `ID`/`DebugSelector`, not a `Styled` method, so the `[_ lazy]` marker rule and buildcostspec are untouched). The element's ID IS the FLIP key: the bookkeeping is `WithElementState` under the element's global id path — the transition diff's own mechanism — so zterm's string keys disappear, and an element that stops drawing for a frame loses its state and snaps when it returns (fresh insertion never animates, like CSS).
- The mechanism, one call site in the div's PREPAINT (div.go, after `resolveStyleTransitions`, before 56's `elementTransform`): First = the border box the element last showed (mapped through its whole transform list, `mapBoundsThrough` — 56's mapped-bounds helper); Last = the newly laid-out border box; Invert = the inverse translate (plus the inverse size ratio with `WithScale`, origin-resolved) written as `Style.Transform` PREPENDED to the element's own functions — a pure translate rides 56's translation-only fast path (no scene group; the hitbox and debug bounds shift with it, so mid-flight clicks hit the visual position), a scale rides 56's effect group; Play = `TransitionFromWithStatus` — ticket 44's motion engine, the seeding seam Base's presence enters through — easing a `flipOffset{X, Y, SX, SY}` (a `Lerp[T]` value) to the identity over the group's policy, requesting animation frames while it runs and completing at once under `App.MotionReduced()` (the App flag plus the live OS preference). Each frame's prepaint writes the sampled inverse back onto the style; a finished move writes nothing, so a settled element carries a zero transform.
- Interruption: a second `Arm` mid-flight starts a FRESH keyed transition from the current visual position (a move's key = the group's identity + the armed generation, so the superseded move's keyed state is released by the frame-end element-state sweep) — what FLIP libraries and Chrome's transition retargeting do; the reversing-shortening rule is deliberately not applied (a move back toward the old resting box plays at full duration, like a retarget, not shortened like a reversal).

**Why the keyed value transition and not 56's `TransitionTransform` style slot:** the style diff's before-change value is the frame's first style resolution, at request_layout, which runs BEFORE the new layout exists — the inverse is only computable at prepaint, against the laid-out border box. FLIP's diff is geometric, not stylistic, and needs a transition whose start value the caller owns; `TransitionFromWithStatus`'s seeding is exactly that seam. Recorded in flip.go's file comment.

**Tests (`gpui/flip_test.go`, the test clock):** `TestFlipReorderSlidesRowsFromOldToNewPositions` — the acceptance reorder: the change frame shows the First box (a still at 0 after the swap), the exact midpoint at half the duration (20 between 0 and 40), the end state at the new layout with a zero transform and no frames requested; `TestFlipMidFlightClicksHitTheVisualPosition` — a click at a point in row b's visual span but row a's layout span hits b, and a translate-only move emits no transform group (the fast path); `TestFlipReducedMotionCompletesImmediately` — lands at the new layout in the change frame, 0 frames; `TestFlipInterruptionStartsFromTheCurrentVisualPosition` — the second reorder's first frame shows the mid-flight position exactly (20), then a fresh full duration eases to the newest layout; `TestFlipScaleAnimatesTheBoxAboutTheOrigin` — the box interpolates linearly (60x30 at half), a transform group mid-flight, none and the new layout after; `TestFlipUnarmedChangeFollowsAtOnce`; `TestFlipRequiresAnElementID`. No parity fixture (optional): Chrome has no FLIP primitive — a browser FLIP is page-authored CSS the recorder's transcript cannot express — recorded in docs/browser-parity-harness.md.

**The zterm follow-up (gpui-go-base/examples/zterm, read-only here; the core helper replaces the translation cases):**

- `NewFlip(d, ease)` → `gpui.NewFlip(d, gpui.EasingCustom(ease))` (chromeEase → `gpui.EasingCubicBezier(.22, 1, .36, 1)`; `gpui.EaseOutQuint()` → `gpui.EasingCustom(gpui.EaseOutQuint)`).
- `f.Arrange(shape, group, now)` → the group dimension disappears (keying is by element-id path: another tab's rows are another subtree, and an un-armed tab switch snaps anyway); the shape comparison stays in zterm's render as `if shape != last { f.Arm() }`.
- `f.Wrap(key, child)` → `Div().ID(key).Flip(f).Child(child)` — the flipElement/`WithElementOffset` adapter is deleted; the core writes the inverse as a CSS transform, so there is no one-frame gap and the row's own subtree (hover, listeners, a11y) rides the move.
- `f.Place(key, to, now)` + paint-at-rect (the glass) → the glass becomes an `Absolute` div filling its parent with `Flip(f)`; the Canvas/record plumbing goes away (the div's own laid-out box is the target), and zterm's press-scale composes by prepending (FLIP writes before the element's own functions).
- `if f.Moving(now) { gpui.RequestAnimationFrame(w) }` → deleted (the engine requests frames while a move runs). `f.Shown(key)` in the tests → `cx.DebugBounds(selector)`. The `f.Reduce = app.ReduceMotion()` plumbing → deleted (the engine's reduced-motion gate covers the App flag and the live OS preference).
- `f.FadeIn` → not ported: pair the arrival with the opacity style transition (ticket 44) or keep `base.MotionTransition`; zterm's pane fade (220ms) and the agent-glass fade (160ms) are the two users.
- zterm's pane SPLIT easing (`Edges` as margins — an inset/layout resize) does not map: the core FLIP is transform-only, and `WithScale` approximates it by scaling content; the panes keep their inset easing or accept the scale visual. The rows, the tab strip and the two glasses map 1:1.

**Gaps:** no fade-in for arrivals; a move inside a `CachedView` freezes mid-flight (the motion engine's recorded limitation, the one ticket 44's style transitions carry); elements participating in a parent paragraph get no placement (56's recorded transform limitation); an element's own transform combined with Flip is approximate (the inverse is measured against the untransformed border box; the visual-bounds record self-corrects the next move); a zero-sized axis does not scale; an unarmed resize mid-move keeps the eased offset over the live target (zterm's documented semantics — the visual jumps by the layout delta); no TSX/class surface (a group is state, not a style value — the TSX host would need a group registry).

**What 60's property-only frames can reuse:** as built, a FLIP move re-renders its view every frame (the engine's `RequestAnimationFrame` → a full draw), so 60's style-diff-driven fast path will not accelerate it. What lands for 60: the prepaint-time transform write is exactly where a property-node update would go (`withElementTransformPaint`'s group and `transformMatrixOf` are the seam `UpdateEffectMatrix` would drive), and the placement's per-element geometry — the layout box, the mapped visual box, the move generation — is the state a property-node FLIP needs: re-pointing `writeFlipTransform` at the property node would drop a move's per-frame cost to the matrix update without touching the placement.
