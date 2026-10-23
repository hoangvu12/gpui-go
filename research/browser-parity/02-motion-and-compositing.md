# Motion, compositing and frame scheduling: the browser compared with gpui-go

Ticket: [.scratch/browser-parity/issues/02-motion-and-compositing.md](../../.scratch/browser-parity/issues/02-motion-and-compositing.md). Researched on 2026-10-23. These are proposals until the user accepts them.

Sources:
- Chromium `0d4dcd4f` (sparse checkout; paths below are relative to its root). `gpu/` and `ui/gl/` are not in the checkout, so claims about them are marked [I].
- Skia `e71ecd07`.
- The W3C/WHATWG specs.
- Microsoft's DXGI docs, read through a fetch helper.
- gpui-go at `8f5ad4d` (`research/browser-parity`).

Every claim is marked: **[V]** means read in source at the cited line, **[I]** means inferred.

## Ranked gaps

| # | Gap | Fix lives in | Size |
|---|---|---|---|
| 1 | No damage: every frame clears and redraws the whole window, then presents it in full | Go (damage from the cached-view diff) + native (retained target, scissor, Present1) | L-XL |
| 2 | No property-only frames: each animation or scroll tick re-renders the view, resets layout, rebuilds the scene and redraws everything | Go (fast path) + native (property nodes in the scene) | XL |
| 3 | `opacity` is per-primitive alpha, not group opacity, so overlapping children render differently from the browser | native (reuse isolation targets) + Go | M |
| 4 | No element transforms (translate/scale/rotate). Only sprites carry a matrix | native + Go style | L |
| 5 | No declarative transitions in the core: CSS starts transitions from computed-style changes, but gpui-go has only Base's explicit, id-keyed calls | Go | M-L |
| 6 | Scrolling: instant jumps, no smooth wheel or `scroll-behavior`, no scroll anchoring, and each scroll re-renders the view | Go (smooth, anchoring) + native (scroll layers or scroll-by-copy) | M + L |
| 7 | Reduced motion lives in Base, is read only once, and is applied as a global kill switch | Go | S |
| 8 | Frame scheduling is already close to viz. Missing: occluded/minimized skip, a half-rate option, frame deadlines | Go | S |
| 9 | No View Transitions (snapshot plus animated pseudo-tree) | Go + native, after 2 and 4 | XL |

Suggested order:
1. **One native revision for 3, 4 and the native half of 1.** Group opacity, element transforms, and a retained frame target with scissored redraw and Present1 dirty rects. Make it one rebuild, like gpui-perf ticket 05.
2. **The Go half of 1.** Compute damage from cached-view reuse, and add a paint-flashing debug overlay to check it.
3. **Item 7, then item 8.** Both are small and independent, so they can go in parallel with the first two steps.
4. **Item 2.** This is the property-only frame fast path. It is the big win for animation, and it needs items 1, 3 and 4.
5. **Item 6 (Go part), then item 5.**
6. **Item 6 (native scroll layers), then item 9.**

---

## 1. Paint invalidation and damage

**Spec.** No spec covers this; it is an implementation concern. The HTML "update the rendering" steps only define *when* rendering happens (https://html.spec.whatwg.org/multipage/webappapis.html#update-the-rendering).

**Chromium.** Damage is computed at three levels:
- **Blink raster invalidation diffs paint chunks.** It does not track "dirty elements". `RasterInvalidator::GenerateRasterInvalidations` matches each new chunk to an old one by id [V `third_party/blink/renderer/platform/graphics/paint/raster_invalidator.cc:196-320`]:
  - A chunk moved from a cached subsequence with unchanged properties produces **no** damage (:242-249).
  - A property change invalidates the old and new bounds (:268-279). Transform changes have tolerances, 1e-2 px for translation and 1e-4 otherwise (:206-207). Sub-tolerance changes accumulate (:282-287).
  - An incremental change invalidates the XOR of old and new bounds (:322-333).
  - Chunks that appeared or disappeared invalidate their bounds (:216-224, :310-319).
- **cc** unions each layer's `update_rect` into its render surface. A layer whose property changed damages its old and new visible rects [V `cc/trees/damage_tracker.cc:425-461`].
- **viz** adds damage the back buffer is still missing from earlier frames (`GetCurrentFramebufferDamage`), then expands damage across any pixel-moving filter (blur) it touches [V `components/viz/service/display/direct_renderer.cc:1013-1062`, `:964-1001`]. Partial swap is used only when the output surface supports it [V `direct_renderer.cc:104`].
- **Present on Windows.**
  - The GPU path presents through DirectComposition. Its `update_rect` is ignored "because the SharedImage backing already knows the area to be swapped" [V `components/viz/service/display_embedder/skia_output_device_dcomp.cc:213-219`].
  - That backing is a DComp surface drawn with an update rect, which is in `gpu/` [I].
  - The software path keeps a full staging copy, copies the damage into the swap-chain buffer, and calls `Present1` with one dirty rect [V `components/viz/service/display_embedder/software_output_device_win_swapchain.cc:241-273`, flip-sequential with 2 buffers at :154-167].
- **DXGI rule** (Microsoft docs, via the fetch helper): "An application must update every single pixel in each rectangle … cannot assume that the pixels are saved from the previous frame" and "must not update any pixel outside of the dirty rectangles". The app owns every pixel the dirty rects don't cover. DXGI copies forward only the scroll rect. Sources: https://learn.microsoft.com/en-us/windows/win32/api/dxgi1_2/ns-dxgi1_2-dxgi_present_parameters and https://learn.microsoft.com/en-us/windows/win32/direct3ddxgi/dxgi-1-2-presentation-improvements.

**gpui-go.**
- Every frame builds a fresh scene [V `gpui/render.go:96`].
- The native renderer clears the whole target [V `reference/native/src/renderer/draw.rs:1289`], or blits a whole offscreen target when filters need one [V `draw.rs:1591-1600`].
- It presents with `Present(0, 0)` [V `draw.rs:2436`] on a flip-sequential swap chain [V `reference/native/src/renderer.rs:1132`, `:1157`].
- Ticket 05 rejected Present1 because no damage source existed. The DXGI docs above confirm that reasoning: dirty rects need either a retained copy or buffer-age tracking.

**Gap and proposal.**
- **Go (M).** gpui-go already has Chrome's chunk-matching input. `CachedView` reuse (`reusePaint`, [V `gpui/view_cache.go:168`]) grafts the previous frame's scene ranges. Mark a reused range at the same bounds and content mask as undamaged (Chrome's :242-249 rule). Everything else damages the union of its old and new primitive bounds.
- **Native (L).**
  - Keep one persistent frame texture per surface. The offscreen scene target already exists for filters (`draw.rs:1321-1370`).
  - Draw only the damage rects, with scissor and a clear limited to those rects.
  - Copy the damage plus the previous frame's damage into the back buffer (the docs' buffer-age pattern), then call `Present1` with the dirty rects.
  - Expand damage across blur groups as viz does.
  - Alternative: a DComp virtual surface (`IDCompositionVirtualSurface::BeginDraw(updateRect)`) keeps its contents, which makes the copy unnecessary. That is Chrome's GPU path [I]. It needs the composition (mode 0) surface, which `renderer.rs:558-613` already builds.
- **Debug.** Add paint flashing (Chrome's `damage_frame_annotator.cc` exists [V in the directory listing]) to check the damage by eye.

## 2. Which animations can skip main-thread work (compositor animations)

**Spec.** Web Animations defines the model (https://www.w3.org/TR/web-animations-1/#animations). Nothing in the spec requires threading. `will-change` (https://www.w3.org/TR/css-will-change-1/) is only a hint.

**Chromium.** The rules are in `third_party/blink/renderer/core/animation/compositor_animations.cc`:
- **Allowed properties.** Only `opacity`, `transform`/`translate`/`rotate`/`scale`, `filter` and `backdrop-filter` are compositable. `background-color` and `clip-path` qualify only through native paint worklets, and custom properties only when a paint worklet uses them. Anything else is `kUnsupportedCSSProperty` [V :396-452, :312-359].
- **Disqualifiers.**
  - A `filter` that moves pixels [V :406-414].
  - Non-replace composite modes [V :389-393].
  - `iterationComposite` [V :265-268].
  - `will-change: contents` in the subtree [V :279].
  - Unsupported timing [V :500-506].
  - Incompatible animations already running on the same property [V :495-498].
- **The target needs a paint-property node with a direct compositing reason.** Examples are an active transform or opacity animation, or `will-change` [V `core/paint/compositing/compositing_reason_finder.cc:119`, `:594`, `:614`]. Without one the result is `kTargetHasInvalidCompositingState`. The same applies when the target is fragmented [V :621-655].
- **Forced reduced motion disables compositor animations** [V :606-607].
- **Transitions start from style diffs.** `CSSAnimations::CalculateTransitionUpdate` [V `core/animation/css/css_animations.cc:3105`] diffs the before-change and after-change styles. Once started, a compositable animation ticks in cc with no main-thread style, layout or paint.

**gpui-go.**
- **The tick.** An animation tick is `RequestAnimationFrame`, which notifies the *whole current view* on the next frame [V `gpui/imagecache.go:1036-1048`]. `AnimationElement` re-runs its animator and lays out the child again every frame [V `gpui/animation.go:571-646`]. Base motion does the same through `gpui.RequestAnimationFrame` [V `base/motion.go:390`, `:432`, `:618`].
- **The frame.** The frame then resets the layout engine [V `gpui/render.go:108`], re-renders the view and its uncached descendants, rebuilds the scene and redraws the full window.
- **Measured costs** (gpui-perf map): 8.5 ms per frame for 1,000 divs and 31 ms for the text page, so a heavy page can't hold 60 Hz while anything animates.

**Gap and proposal (XL, Go + native).** Add a "property-only frame":
1. A core style can mark an element's `transform` and `opacity` as animated (the `will-change` analogue). Painting then wraps its subtree in a scene property node, meaning a group with a matrix and an alpha.
2. A transition or animation on that property updates the node's value in the retained scene. The frame skips render, layout and paint, re-submits the scene with the new value, and damages the node's old and new bounds (section 1).
3. Any other change falls back to the full frame, the same way Chrome falls back to main-thread animation.

This needs gap 4 (transforms) and gap 3 (group opacity) in the scene first. Threading is not needed: the win comes from skipping the pipeline, not from running on another thread. The zgui research points the same way (app-gaps README, performance item 4).

## 3. Opacity semantics

**Spec.** `opacity` < 1 creates a stacking context, and the element is composited as one group (https://www.w3.org/TR/css-color-4/#transparency, https://www.w3.org/TR/compositing-1/#csscompositingrules_CSS).

**Chromium.** Opacity is an effect node. Its subtree is drawn into a render surface when needed, then blended once (`cc` effect tree; `CompositorElementNamespaceForProperty` maps opacity to `kPrimaryEffect` [V `compositor_animations.cc:221-228`]).

**gpui-go.** Element opacity multiplies down to every primitive [V `gpui/scene.go:596`]. The filter group documents that its group opacity is 1.0 "because the group's children already carry it" [V `gpui/filters.go:12-13`]. This is GPUI's behaviour, and it differs visibly from the browser: overlapping children of a half-transparent parent show their overlap, and borders darken over backgrounds.

**Gap.**
- **Native (M).** Route opacity < 1 on an element that has more than one overlapping child through the isolated group targets the filter chain already has (`draw.rs:794`, `dx_blur_and_composite` with `opacity` at :1572). The depth limit (2) needs lifting, or an inline fallback.
- **Go (S).** Emit a group instead of multiplying opacity into each primitive.

This also gives gap 2 its opacity node.

## 4. Transforms

**Spec.** CSS Transforms 1/2 (https://www.w3.org/TR/css-transforms-1/, https://www.w3.org/TR/css-transforms-2/#individual-transforms).

**Chromium.** Separate transform, translate, rotate and scale nodes in the property trees [V `compositor_animations.cc:225-232`].

**gpui-go.** The native scene carries a 2x3 `transformation` only on sprite records [V `reference/native/src/scene.rs:519-560`] and path commands [V `scene.rs:644-646`]. Go styles have no element `transform`; the only "transform" is `TextTransform` [V `gpui/style.go:1134-1146`]. `base/motion.go`'s `MotionTransform` [V :61-125] is a value bundle that nothing in the core can paint.

**Gap (native L + Go S).** Add a transform stack to the scene: a push/pop node applied to quads, shadows, text, sprites and paths, plus hit-testing through the inverse matrix. It is needed for gaps 2 and 9 and for FLIP.

## 5. Transitions and animations as a core contract

**Spec.**
- css-transitions-1 §3 defines starting, the reversing-adjusted start value and the shortening factor (https://www.w3.org/TR/css-transitions-1/#starting).
- css-animations-1 (https://www.w3.org/TR/css-animations-1/).
- Web Animations covers timing, events and the `finished` promise (https://www.w3.org/TR/web-animations-1/).

**Chromium.** Transitions are computed during style recalc. An interrupted transition back to its start shortens by the eased progress [V `css_animations.cc:2892-2911`].

**gpui-go.**
- `gpui/animation.go` is GPUI's `with_animation`: an explicit wrapper, durations only, no spring [V :10-15].
- Base's `TransitionWithStatus` implements the CSS reversing rule faithfully (eased progress × old factor + (1 − old factor)) [V `base/motion.go:364-373`].
- Everything in Base is id-keyed and called imperatively in render [V `base/motion.go:326-399`]. A style change never starts anything.

**Gap (Go, M-L).** For browser-like behaviour, the core needs a `Transition` style property (property set, duration, easing, delay, `transition-behavior`). It also needs a per-element (global element id) diff of resolved style between frames that starts, retargets or reverses transitions using Base's existing math. Keyframes and springs can stay in components. This matches the map's "core behaves like the browser; components elsewhere" split: the timing engine and easing (`base/motion_easing.go`, `motion_timing.go`, `motion_keyframes.go`) belong in the core, while presence, stagger, sequence and reveal stay in components.

## 6. Scrolling

**Specs.**
- CSSOM View smooth scrolling (https://drafts.csswg.org/cssom-view/#smooth-scrolling).
- `scroll-behavior` and `overscroll-behavior` (https://www.w3.org/TR/css-overscroll-1/).
- CSS Scroll Anchoring (https://drafts.csswg.org/css-scroll-anchoring/).

**Chromium.**
- **Smooth scroll uses an ease-in-out curve whose duration depends on the input** [V `cc/animation/scroll_offset_animation_curve_factory.cc:15-21`, `scroll_offset_animation_curve.cc:17-54`, `:148-170`]:

  | input | duration rule |
  |---|---|
  | mouse wheel | inverse-delta: 12 down to 6 frames at 60 Hz as the delta grows from 120 to 480 px |
  | keyboard | constant 9 frames |
  | programmatic | delta-based |

- Retargeting mid-flight keeps the motion continuous (:385-391).
- **Scroll anchoring** picks an anchor node before layout and adjusts the offset after it [V `third_party/blink/renderer/core/layout/scroll_anchor.cc:613`, `:653-760`].
- **Composited scrolling** moves a transform node with no repaint.
- **Non-composited scroll re-rasters.** "Raster-inducing scroll" resets the chunk info [V `raster_invalidator.cc:396-402`].
- **Tiles.** Tiles are 256-512 px for software raster [V `third_party/blink/renderer/platform/widget/compositing/layer_tree_settings.cc:261-292`]. GPU raster uses viewport-width tiles rounded to 32 px [V `cc/layers/tile_size_calculator.cc:22-83`]. Raster prepaints a 3,000 px interest area [V `cc/base/features.h:38`].
- **Paint recording.** Skia's `SkPicture`/`SkRecorder` (e.g. `skia/src/core/SkPicture.cpp`) is the recorded-paint format that tiles replay [I: role; files present].

**gpui-go.**
- A wheel event adds its delta to the offset immediately and notifies the view [V `gpui/scroll_element.go:146-193`]. There is no animation, no `scroll-behavior`, and no anchoring.
- `ScrollHandle` does have scroll-to-item [V `gpui/scroll.go:198-260`].
- Precision touchpads go through DirectManipulation with OS inertia [V `gpui/directmanipulation_windows.go:11-25`].
- Base adds a touch overscroll bounce [V `base/scroll_bounce.go:1-40`].
- Every scroll re-renders and repaints.

**Gaps.**
- **Smooth scrolling (Go, S-M).** Animate wheel steps, keyboard scrolls and programmatic `ScrollTo(smooth)` with Chrome's curve and durations, honouring reduced motion. This belongs in the core: it is browser default behaviour, not a component.
- **Scroll anchoring (Go, M).** Anchor on the first visible child of `ScrollHandle` (the core already tracks child bounds and the top item [V `gpui/scroll.go:143-182`]).
- **Scroll layers (native L).**
  - A scrolled subtree becomes a translated property node (gap 2), so a scroll tick is a property-only frame.
  - With section 1's retained target, use the DXGI scroll rect (flip model only, per the Microsoft docs) or a GPU copy, and repaint only the exposed strip.
  - Tiles are not needed at app scale; zgui's scroll-by-copy is the closer model (app-gaps README, performance item 5).

## 7. prefers-reduced-motion

**Spec.** Media Queries 5 `prefers-reduced-motion` (https://www.w3.org/TR/mediaqueries-5/#prefers-reduced-motion). It exposes a preference; authors decide what to do with it.

**Chromium.**
- On Windows it reads `SPI_GETCLIENTAREAANIMATION` [V `ui/gfx/animation/animation_win.cc:44-47`].
- It re-reads on `WM_SETTINGCHANGE` with that `wParam` [V `ui/gfx/font_render_params_win.cc:129-133`].
- A forced reduce-motion setting also turns off compositor animations [V `compositor_animations.cc:606-607`].

**gpui-go.**
- Base reads the same SPI once at `Init` [V `base/reduce_motion_windows.go:20-33`], with no live follow ("no notification Base can subscribe to without a window") [V :35-38].
- Every Base motion then jumps to its target [V `base/reduce_motion.go:6-10`].
- The core owns only the `App.ReduceMotion` flag, and `AnimationElement` jumps to the resting value [V `gpui/animation.go:588-592`].

**Gap (Go, S).**
- Move the platform read into the core.
- Follow `WM_SETTINGCHANGE` in the host's WndProc; the core does have windows.
- Expose it as a preference query, like the media query.
- Leave the "what to do" to components. The core's own defaults (smooth scroll, core transitions) should collapse to instant.

## 8. BeginFrame scheduling and present

**Spec.** HTML "update the rendering" runs rAF callbacks once per rendering opportunity (https://html.spec.whatwg.org/multipage/webappapis.html#run-the-animation-frame-callbacks).

**Chromium.**
- **Demand-driven.** The Windows begin-frame source subscribes to the vsync thread only while frames are needed [V `components/viz/service/frame_sinks/external_begin_frame_source_win.cc:109-118`].
- **No wait after idle.** A newly added observer gets a "missed" begin frame at `now`, so it doesn't wait for vsync [V :70-92].
- **Half rate.** A preferred interval can halve the rate by skipping every other vsync [V :53-62, :94-106].
- **Draw conditions.** The display draws only if it needs a draw, is visible, and has a root frame [V `components/viz/service/display/display_scheduler.cc:602-607`].
- **Deadlines.** The deadline is immediate when all surfaces are ready, and late when swaps are throttled (`pending_swaps >= max`) [V :685-760].

**gpui-go.** This is already close:
- It is demand driven [V `gpui/frameloop.go:184-203`].
- The pacer exits when idle [V `gpui/frameloop_windows.go:99-106`].
- The first frame after idle skips the vblank wait [V `frameloop_windows.go:166-177`], which is the same idea as viz's missed begin frame.
- It uses a waitable swap chain at latency 1 [V `reference/native/src/renderer.rs:641-643`].
- It invalidates through `InvalidateRect`/`WM_PAINT` [V `frameloop_windows.go:131`].

**Gaps (Go, S).**
- Skip frames for minimized or fully occluded windows (`DXGI_STATUS_OCCLUDED` / `IsIconic`). The frame loop deliberately doesn't port throttling [V `gpui/frameloop.go:71-73`].
- Add an optional per-window preferred interval: half rate for low-priority animation, the analogue of `AnimationElement`'s `MaxFPS` timer [V `gpui/animation.go:621-640`], but vsync-aligned.
- Frame deadlines don't matter in a single-process renderer.

## 9. View Transitions

**Spec.**
- css-view-transitions-1: capture the old state, update the DOM, capture the new state, then animate a `::view-transition` pseudo-tree (https://www.w3.org/TR/css-view-transitions-1/).
- Level 2 adds element-scoped transitions (https://www.w3.org/TR/css-view-transitions-2/).

**Chromium.**
- The UA stylesheet is generated per transition. Each group animates `transform` plus `width`/`height` from old to new geometry [V `third_party/blink/renderer/core/view_transition/view_transition_style_builder.cc:148-191`].
- The old and new images cross-fade with `plus-lighter` blending, `ease` timing and 0 s delay [V :62-112].
- Damage from the live content surface propagates through the shared-element layer [V `cc/trees/damage_tracker.cc:415-423`, `:596-630`].

**gpui-go.**
- There is nothing in the core.
- `capture.rs` and render-to-image exist [V `reference/native/src/capture.rs`; `draw.rs:2502`], as do the filter isolation targets.
- The app-gaps README lists FLIP and shared-element animation as a Go gap (feature item 3).

**Gap (XL).** The pieces are:
1. Snapshot keyed elements into textures (native, reuse capture).
2. Diff keyed geometry across the state change (Go).
3. Animate group transform and size plus an old/new cross-fade with `plus-lighter` (needs gap 4 and a `plus-lighter` blend in native).

Do this last. A plain FLIP helper (geometry diff plus transform transition) is the cheap subset and needs only gaps 4 and 5.

## Notes for the proposal (ticket 07)

- **Core:** the frame loop, damage, property nodes (transform and opacity), the timing and easing engine, style-driven transitions, smooth scroll, anchoring, and the reduced-motion preference.
- **Components:** presence, stagger, sequence, reveal, springs as design tokens, overscroll bounce, and any opinionated motion.
- **Surprises:**
  1. Chrome's damage is a diff of old and new paint chunks. gpui-go's `CachedView` reuse already produces the matching Chrome computes, so Go-side damage is cheaper than expected.
  2. gpui-go's opacity is not CSS group opacity. This is a visible correctness gap inherited from GPUI.
  3. A one-element animation re-renders its whole enclosing view, because `RequestAnimationFrame` notifies the view.
  4. Ticket 05's Present1 caution is correct per the DXGI docs. Partial present needs a retained target, not just rects.
