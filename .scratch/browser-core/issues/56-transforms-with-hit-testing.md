# 56 - Transforms with hit testing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: worker 56 (transforms + hit testing)
Blocked by: 54
Triage: ready-for-agent

**What to build:** Elements accept CSS transforms (translate, scale, rotate, skew, transform-origin), and hit testing follows the transform.

**Blocked by:** 54 (R16: integrate, build rev 16 and measure)

**Status:** ready-for-agent

- [x] Box and pixel fixtures for each transform match Chrome.
- [x] Event-log fixtures: clicks on rotated and scaled elements hit the right targets (spec risk tested first).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Landed on `core/56-transforms-with-hit-testing` (branched from main at 43d22e6; Go-only — no native changes, ticket 51's rev-16 effect-group record carries everything).

**What landed**

- **The transform model (`gpui/transform.go`):** a `Style.Transform []TransformFunction` + `Style.TransformOrigin *TransformOrigin` pair. The functions — translate (px/rem/%-of-own-border-box lengths), scale, rotate, skew — compose left to right about the origin (the CSS initial 50% 50% of the border box) into ONE window-to-window 2x3 `TransformationMatrix`, resolved at PREPAINT against the laid-out border box: transforms never touch layout. Percentages of the border box resolve at matrix time; scale(0) (a singular 2x2) collapses (paints nothing, hit-tests nothing, like CSS's collapsed transform).
- **Paint (`gpui/transform_paint.go` + `div.go`):** the matrix brackets the element's whole painted box — background, borders, shadows, outline and children — in one scene effect group (`HasTransform`, opacity 1, bounds = the border box, content mask = the ambient device mask). The subtree's records stay at their untransformed window coordinates: the group's LOCAL frame is the untransformed window frame, the contract ticket 51 recorded in SCENE_ABI ("the matrix maps LOCAL (untransformed window-space) points to device points"); the device matrix is the 2x2 unchanged with the translation scaled. A translation-only transform skips the group entirely and shifts the element's own bounds plus the children's element offset — the sticky-shift mechanism, design §3.4's fast path (the known price: a window-frame group's bucket is inflated by its distance from the origin, ticket 54's recorded leftover).
- **Hit testing (`mouseevent.go`, `pointercapture.go`):** each hitbox captures the composed matrix from the frame's transform scope at insertion (entry = parent·own, so NESTED transforms compose); `Hitbox.Contains` maps the pointer through the inverse before the bounds test — Chromium accumulates the transform chain with origins applied and maps the location through the accumulated inverse (`hit_testing_transform_state.cc:37-49`) — while the content mask tests the RAW pointer (an ancestor's overflow clip applies to the transformed result). `LocalPosition` maps through the same inverse (offsetX/offsetY are the pre-image). Hover, click, capture and drag targets all ride `renderedFrame.hitTestAt`, so all four follow the transform; the debug bounds (the Tier A probe) report the mapped AABB, `getBoundingClientRect`'s geometry.
- **The transition engine (`transition.go`):** `TransitionTransform`, the sixth interpolable slot. Two lists of the same length and function kinds interpolate per function (CSS's list-interpolation rule: numbers, lengths, angles); anything else applies discretely — the matrix-decomposition fallback is not ported (recorded below). This is the seam tickets 60/61 build on.
- **The style API:** `Styled` methods, all with the `[_ lazy]` marker (buildcostspec passes): `Transform(...)` (replaces the list), the list-appending `Translate/TranslatePx/TranslateX/TranslateY`, `Scale/ScaleUniform/ScaleX/ScaleY`, `Rotate`, `Skew/SkewX/SkewY`, `TransformOrigin(x, y)`, and the nine zero-arg `Origin*` keyword methods.
- **Tailwind classes (`class.go` transformClass):** `translate-x/y-*` (the 0.25rem spacing scale, px, full=100%, fractions, arbitrary lengths, negatives), `scale/scale-x/scale-y-*` (a percentage like scale-95, or a raw factor scale-[1.7]), `rotate-*` and `skew-x/y-*` (degrees or arbitrary [45deg]) — consecutive transform classes append in class order like the filter classes; `origin-*` through the generated method switch; the bare `transform` class and the 3D families get rejection reasons. docs/tailwind.md updated.
- **TSX style keys:** `transform` ("translate(8px, 4px) rotate(45deg) scale(1.5)") and `transformOrigin` ("top left", "50% 50%", "10px 20px") in parseStyle; tsxgen taught the string value shape; `go generate ./gpui/script/types` regenerated (the drift tests and the surface fixture cover both keys).

**Fixtures** (recorded in headless Chrome 155.0.8059.85; all three tiers green; the hit-test fixture written and recorded FIRST — the spec's risk):

- `transform-hit` — 27 recorded events. Clicks that hit a rotated square, a scaled square and a rotated child inside a translated parent through the inverse matrix, and clicks that MISS through the layout boxes (both directions of the mapping), with the pointerenter/leave boundary sweeps in Chromium's order. The replay's transcript matches Chrome's entry for entry; Tier A pins the translated parent's box.
- `transform-translate`, `transform-scale`, `transform-rotate`, `transform-skew` — one box+pixel fixture per kind, each with Tier-A-exact mapped geometry (integer translation, dyadic 1.75 scale, quarter-turn, tan(45deg)=1). translate and rotate measure 0.0000 mean channel diff (the offset path and the quarter-turn composite are pixel-exact); scale's bilinear edge measures 0.117 whole-image mean (its gates sit at 2x).
- `transform-origin` — the same quarter-turned box about the default centre and about `transform-origin: 0 0` (both Tier-A exact).
- `transform-transition` — the 200ms mid-flight sample of a 1 -> 1.75 scale (Tier A: exactly 1.375, dyadic) and a 0 -> 90deg rotation (Tier C), through the recorder's paused-animation seek.
- Each fixture has a wrong view that must fail (`transform_test.go`): the no-transform views (Tier A/B/C), the wrong-origin view (Tier A on `corner`), and the 800ms transition (Tier A at the sample).

**Unit gates** (`gpui/transform_test.go`, `gpui/transform_class_test.go`): matrix composition and origin resolution (including the translate-then-scale order), the inverse round trip, the mapped AABB, hit tests through rotation/scale/translation/nesting, the scene's group emission (one start/end pair with the device matrix; a translation emits none), scale(0) hiding the element, the transition's exact midpoint, and the Tailwind class table with rejections.

**Tests:** `go test -p 1 -count=1 ./gpui/ ./internal/parity/... ./internal/scenespec ./internal/rendererspec ./internal/buildcostspec ./internal/focusspec ./internal/dropspec ./internal/inspectorspec ./gpui/script/... ./internal/tsxgen/ ./examples/...` — 57 packages ok, 0 failures (TestFrameOverheadIsUnder1msAtTheProbeSize passed this run; it is the known load-sensitive flake). `go vet` clean; `displaycodemod -check ./...` and `positioncodemod -check ./...` at 0; `tsxdtsgen -check` current.

**Known gaps (also in docs/browser-parity-harness.md and transform_paint.go):**

- An element's transform composes ABOVE an ancestor's overflow clip, not inside it: inside a scroller, the subtree's records clip pre-transform (insert-time culling) and the hit test applies the ancestor's mask to the raw pointer, so a rotated element can paint and hit-test past the scroller's edge where Chrome clips the transformed result (the ABI has no post-transform clip on the effect record). Fixtures avoid scroller+transform.
- Transforms on elements participating in a parent paragraph (inline content) are inert: CSS applies `transform` to transformable elements only, and the fragments path has no matrix seam (atomic inlines would transform in CSS — a fragments-path follow-up).
- Anchored()/tooltip-hook anchors and the inspector tree still read a transformed element's layout position; the group carries no `PropertyNode` (NodeID 0) — ticket 60 wires the property node + `UpdateEffectMatrix` for property-only frames, and 61 (FLIP) can build directly on the transform list, the transition slot and the mapped-bounds helper.
- No 3D, no matrix() spelling, no percentage rotation, and non-matching transform lists transition discretely (CSS's matrix-decomposition fallback not ported).
