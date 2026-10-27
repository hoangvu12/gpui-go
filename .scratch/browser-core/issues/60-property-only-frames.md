# 60 - Property-only animation frames

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 12 (Claude Code)
Blocked by: 44, 56, 58
Triage: ready-for-agent

**What to build:** Transform and opacity transitions update scene property nodes and skip render, layout and paint; animation frames stop notifying the whole view.

**Blocked by:** 44 (Transition style property and the core motion engine), 56 (Transforms with hit testing), 58 (Damage tracking and paint flashing)

**Status:** ready-for-agent

- [x] A transform transition runs with zero view renders per frame (observable count).
- [x] Frame cost measured and recorded.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: under the approved R16 design (docs/design/r16-compositor.md §3.10), this is the Go half of property-only frames. It needs the native `scene_update_property` (51) and `renderer_surface_draw_scene_damage` (53), shipped in rev 16 (54). Detection is automatic from an active transform or opacity transition, with no opt-in style (user, 2026-10-24).

## Answer

Landed on `core/60-property-only-frames` (Go-only — rev 16's `scene_update_property` and `draw_scene_damage` consumed unchanged; no native edits, no cargo).

**Design** (`gpui/propertyframes.go`, the new file; the model is Chromium's compositor-only frame — `compositor_animations.cc:398-426` the compositable-property switch, `element_animations.cc:487-499,520-536` the `SetElement{Opacity,Transform}Mutated` node writes from `keyframe_effect.cc:120-139`'s tick, `layer_tree_host_impl.cc:924-949` `SetNeedsRedraw(animation_only)`):

- **Detection is automatic** from an active transform or opacity transition run (no opt-in style, the user's 2026-10-24 decision): `resolveStyleTransitions` (all three style resolutions) records a per-element *draft* in the frame when those slots sample active, assigns the property-node ids ONCE into the retained `transitionElementState` (per-window monotonic counter — an evicted element never reuses an id), and requests the next frame through the NEW `requestPropertyAnimationFrame` (no notify) when the element's active slots are ALL compositable — any other slot (width/height/background/border) keeps ticket 44's notify-and-rebuild animation.
- **The paint brackets the nodes**: `withElementTransformPaint` marks the transform group `PropertyNode`+`NodeID`; `withElementOpacityNode` (the internal variant of `WithElementOpacity`) does the same for the opacity group — the native never FOLDS a property-node group (the fold bakes the opacity into the primitive at plan time, which would make an in-place update invisible), so an animating single-primitive opacity element takes the isolated composite (the design's recorded 8-bit-quantization divergence, within every fixture tolerance). An ACTIVE transform transition also FORCES the matrix scope for translation-only samples (even identity) — the offset fast path emits no group to tick; the native composites translation-only groups through the matrix (a lossless blit at integral offsets, 51), and a settled transform returns to the fast path. `completeFrame` publishes the drafts as `ds.lastPropertyAnimations` (the registry).
- **The tick** (`tickPropertyAnimations`, served by `requestFrame` when the window is clean and by `SimulateNextFrame` for tests): samples the retained runs at the app clock READ-ONLY, re-resolves the sampled transform list against the recorded geometry (border box/origin/rem/scale — exactly what a rebuild would emit), calls `Scene.UpdateEffectMatrix/UpdateEffectOpacity` on the last drawn scene, publishes the returned old ∪ new mapped bounds (dilated 2 px, the r53/r54 rule) as the frame's damage through the existing `LastFrameDamage`/`PresentFrame` seam (58's atlas/appearance plumbing reused verbatim), and re-requests while any run is active. A run that finished between ticks applies its final sample once (the animation settles on its target). Nothing renders, lays out or paints; `WindowDrawCount` stands.

**What falls back to a full frame** (correctness first — the fallback is a notify plus rebuild, the pre-60 behavior): a notified view (any app change), a refresh request, a view notified during the last draw, an abandoned build's invalid damage state, paint flashing, a singular mid-flight matrix, a run whose frame bracketed no group (an element in a parent paragraph, opacity at exactly 1 — one frame, a backdrop-filter element), the element state evicted (a CachedView freeze — the motion engine's recorded limitation), and **a rebuilt frame with an emitted transform node presents FULL damage** (damage.go): the transform group's visible pixels live at the MAPPED bounds, not the ops' local bounds the damage rule reads. **The fallback-equals-property contract** (design §5 risk 4 / Q4): a rebuilt frame re-samples the same retained runs at its clock — pinned by test at the same virtual clock (the rebuilt group's matrix equals the tick's).

**Hit testing follows the animated transform without a rebuild**: `pushTransformScope` records each open scope's OWN matrix + node id; a hitbox inserted under an animated scope snapshots the chain (`Hitbox.transformChain`), and the tick recomposes each affected hitbox's matrix by replacing the node's own matrix and folding left — exactly the composed matrix a rebuild would capture (Chromium's accumulated-transform hit test, `hit_testing_transform_state.cc:36-49`). The tick also re-records the debug-selector bounds (the mapped AABB), so Tier A probes and the parity fixtures follow the animation. The anchor registry, the inspector tree and the cursor resolution still read the last rebuilt frame's positions (56's recorded transform gaps, unchanged).

**Tests** (`gpui/propertyframes_test.go`): the transform transition ticks with ZERO view renders and zero draws per frame (the render counter and `WindowDrawCount` stand; the node's matrix, debug bounds and partial damage advance; the settled rebuild agrees with the last tick); the opacity transition likewise (including its one unemitted fallback frame at opacity 1); a translate transition (the forced group from the identity from-value, the settled return to the offset path); the hit test at 200ms hits the visual span and misses outside it; the notified-view fallback; the mixed transform+width slot keeps notify-driven rebuilds; the rebuild-with-active-transform full damage; the same-clock rebuild re-inserts the current value. `internal/rendererspec/r60_property_frame_test.go` (one real shown-never-activated off-screen window through its frame loop): every property tick presents damage that is BYTE-IDENTICAL to a force-full redraw of the same scene (the r58 equivalence gate, 6 steps, both transitions), the boxes render nothing across the ticks, and the scene carries both node kinds.

**Measurements** (`evidence/browser-core/ticket60-property-frames.json`, env-gated `R60_MEASURE_OUT`, the r54/r58 pattern; 640x300 DComp window, 200 cached panes + two animated boxes, 30 interleaved pairs): the property tick 3.8 ms mean / 1.6 ms min (present submit 0.33 ms) vs the full animation frame 11.5 ms mean / 6.6 ms min — **paired savings 7.8 ms (67%)**, damage 189 per-mille, 76/76 tick presents partial in this process (this DComp surface accepted Present1; r58's same-mode run hit the pinned readback fallback — the per-process verification decides, design Q5). The r54 ABI-level numbers (1.58 ms vs 2.17 ms at 1000+ quads) remain the pure-present ceiling.

**Gaps**: (1) FLIP (61) still re-renders each frame — re-pointing it is a real second driver, not a re-pointing: the move's value comes from the keyed `valueTransition` entity (the tick would need a read-only sampler beside `TransitionFromWithStatus`, which requests frames through the notify), and the placement's `visual`/`moveGen` bookkeeping (the interruption geometry) must be ticked too; the seam is otherwise ready (the transform group + node machinery and the registry are property-agnostic — a flip draft would record the keyed-state key, the Flip's policy pointer and the geometry). (2) An element inside a `CachedView` still freezes mid-flight (the state sweep evicts the run; the frozen value stays — ticket 44's recorded limitation). (3) The anchor/inspector/cursor gaps above. (4) A singular mid-flight scale (a transform crossing exactly zero) falls back per tick.

**For 59**: the tick reads only `ds.lastScene` (mutated in place), the element-state arena and the published registry — it never touches the view cache or the reuse ranges, so automatic view reuse composes without change (a reused range replays the group markers WITH the node id and the current mutated matrix, so a rebuild during a transition re-inserts the ticked value by construction). The one seam we share is `completeFrame` (one publish line added after the damage publish) and `requestFrame` (the tick block sits between the callbacks and the draw); `SimulateNextFrame`'s tick serving is additive. The damage-log and `damageLogDrifted` are untouched.
