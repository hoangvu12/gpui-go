# 57 - Masks, clip-path, blend modes and edge fades from styles

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 11 (Claude Code)
Blocked by: 54
Triage: ready-for-agent

**What to build:** Styles expose mask-image, edge fades (including the existing OverflowFade setters, which now paint), clip-path, mix-blend-mode and the drop-shadow filter.

**Blocked by:** 54 (R16: integrate, build rev 16 and measure)

**Status:** ready-for-agent

- [ ] Fixtures for each match Chrome within tolerance.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-27, orchestrator 12: verified and merged on top of 56, 58 and 61. Conflicts: gpui/div.go paintInFlow (kept 56's transform group inside 55's opacity scope and switched the opacity to 57's `styleElementOpacity(&style)`; 57's mask/clip-path/blend/filter groups live in `Style.paintBox`, so they nest inside the transform group); path.go/scene_kernel.go (kept 58's damage-log record and 57's `pathCount`); TSX node.go/surface.go/.d.ts (union of 56's transform keys and 57's keys); class_generated.go regenerated (`go generate ./gpui/`). Every native insert 57 uses (gradient, coverage, filter-op chain) is recorded in 58's damage log. On the merged tree: build and vet clean, both codemod checks 0; the broad batch set plus portfixture, scenespec, rendererspec, buildcostspec and native/...: 92 packages ok, failing only portfixture TestFx0004 and gpui TestWin32TranslateKeyDown (known) and focusspec TestRealWindowKeyboardTranslation (timed out once, passed alone: a new flake while the user typed). gpui-go-base: vet clean, all ok. Spot-check: paint_property_tree_builder.cc at 0d4dcd4f: one `EffectPaintPropertyNode::State` takes `state.opacity` (2265) and `state.blend_mode` (2289-2291), so opacity and blend are one composite; matches (the cited 2285-2289 is a few lines off). FOLLOW-UP (found in review): native `replay` (reference/native/src/scene.rs:2789) re-inserts gradient/coverage/op-chain table entries at NEW indices, but replayed quads/effects/boundaries keep the baked old indices, and `pathCount` doesn't count replayed paths. A cached view containing an n-stop gradient, clip-path or drop-shadow can therefore reference the wrong table entry when the content before it changes. Handed to ticket 59 (automatic reuse), which must refuse replay or fix the indices. The native drop-shadow lease bug (Gap 1) is a native follow-up.

## Answer

Landed on `core/57-masks-clip-blend-fades-styles` (Go-only, 9 commits, riding 55's group plumbing and 52's native scene API):

**What landed**

- **The CSS gradient arm** (`gpui/gradient_styles.go`): `Background` grows `BackgroundKindCssGradient` (a `*CssGradient`, so Background stays comparable) - n stops, radial (circle/ellipse x closest/farthest-side/corner), conic (from-angle, clockwise from up) and the repeating tile, resolved at PAINT time against the element's device bounds into one scene gradient table entry (`Scene.PaintQuad`; the border strips share it; the element opacity folds into the stops before the ramp bakes). The geometry and the stop fixup are Chromium's rules: EndPointsFromAngle's gradient line (css_gradient_value.cc:1284-1350), RadiusToSide/RadiusToCorner/EllipseRadius (:1848-1990), AddStops + NormalizeAndAddStops's fixup and span normalization with the domain contraction (:508-830). The two-stop linear gradient keeps the R15 record bit-exact.
- **Edge fades paint** - `Style.OverflowMask` fills `ClipMask.Fade` from `OverflowFade` on each clipped axis (the pin's style.rs:836-843 rule); the paint context carries `MaskFade` with the pin's `ContentMask::intersect` rule and every record kind whose pipeline evaluates the ramps carries them (quads, shadows, underlines, paths, all three sprite kinds) through the ABI mask's `FadeOut` words. Zero fades stay bit-exact.
- **mask-image, clip-path, mix-blend-mode and isolation from styles** (`gpui/mask_styles.go`, `styled_masks.go`): `Style.MaskImage` (a gradient mask, alpha or luminance - `GradientMaskImage`), `Style.ClipPath` (circle/ellipse/inset/polygon + a path form, in element-local coordinates), `Style.MixBlendMode`, `Style.Isolation`. `Style.paintBox` brackets the element's box in rev-16 effect groups in Chromium's effect-tree order - blend(opacity(mask(clip_path(filter(content))))) - with the blend group carrying the element opacity on the SAME group (the effect node holds state.opacity and state.blend_mode together, paint_property_tree_builder.cc:2285-2289; div/img skip their opacity scope for a blended element). The clip-path shape tessellates in device coordinates, inserts a scene path record (a transparent fill, a full-window bound so the post-sort path index equals the insertion index) plus a coverage entry, and composites dst_in against the rasterized coverage. **The hit test answers point-in-shape** - the hitbox record carries the element's ClipPath and `contains` tests the shape (css-masking-1), so a click inside the bounds but outside the shape falls through to whatever is behind.
- **filter: drop-shadow()** - `Filter` grows the drop-shadow function (offset, the CSS blur radius, the color), `composeFilterChain` collects the ops, and `Scene.WithFilterLayerChain` inserts them scene-level (scaled to device) with the boundary pair carrying the op-chain index/count (rev 16's appended fields; 0/0 stays the R15 legacy chain, bit-exact). The filter/blend group's bounds (`Style.InkBounds`) dilate by the shadow's ink (3-sigma of sigma = radius/2 + the offset, ShadowData::RectOutsets' shape).
- **The Tailwind classes**: `via-*` un-blocks (the n-stop arm; ticket 40's `ErrGradientViaUnsupported` retired), `bg-radial`, `bg-conic` and their `bg-repeating-*` forms, `mix-blend-*` (the 17 zero-arg methods - styled_masks.go joined tailwindgen's source list), `drop-shadow-<preset>/none/[0_4px_6px_#00000040]` and `isolate`. `docs/tailwind.md` gains the rows.
- **The TSX style keys** (`gpui/script/node.go` + the parsers `gpui/css_parse.go`): `overflowFade` (one/two/four lengths), `mixBlendMode` (the 17 modes), `isolation`, `clipPath`, `maskImage` and `dropShadow` (CSS value strings parsed by `ParseClipPath`/`ParseMaskImage`/`ParseDropShadowValues`); the tsxgen extractor learned the three parser-call shapes, the Class type gained the new arms, and the .d.ts + surface fixture were regenerated.

**Fixtures** (recorded in headless Chrome 155.0.8059.85, all registered; all 138 fixture packages pass):

- `maskimage` - a box masked by `linear-gradient(to bottom, black, transparent)`; the wrong view (no mask) fails Tier C.
- `overflowfade` - the clipped child fading out over the bottom 12px; the wrong view (the hard clip, the pre-57 behaviour) fails.
- `clippath` - circle(40px) + the bottom triangle, with FOUR clicks: two inside the shapes (box then root), two inside the bounds but OUTSIDE (the root alone - the point-in-shape hit test). The wrong view (full squares) fails Tier C AND Tier B; `TestClicksOutsideTheShapesMissTheBoxes` pins the transcript.
- `blendmode` - multiply over red and plus-lighter over dark slate; the wrong view (normal compositing) fails.
- `dropshadow` - a rounded translucent box with `drop-shadow(0 6px 8px rgba(0,0,0,0.4))`; the wrong view (no shadow) fails (measured 0.284/1.27/0.0001 - the gates at 0.6/3.0/0.002, the headroom rule).
- `gradientstops` - a three-stop linear with NO stop positions (CSS's fixup distributes the middle to the halfway point - the via form) + the default radial; the wrong view (both collapsed to two-stop linears) fails.

**Test results** - `go test -p 1 -count=1` over ./gpui/, ./internal/parity/..., ./internal/scenespec, ./internal/rendererspec, ./internal/buildcostspec, ./internal/portfixture, ./gpui/script/..., ./internal/tsxgen/, ./examples/... and ./internal/native/...: all ok except the KNOWN pre-existing failures (portfixture TestFx0004, gpui TestWin32TranslateKeyDownCarriesCodeAndLocation). `go vet ./...` clean; `displaycodemod -check` and `positioncodemod -check` both 0 (755 files). New gates: `gpui/gradient_styles_test.go` (the fixup, the geometry, the paint seam), `gpui/group_styles_test.go`'s fade tests, `gpui/mask_styles_test.go` (the group emissions + the shapes' point-in-shape), `gpui/filter_scene_test.go`'s drop-shadow chain, `gpui/css_parse_test.go`, `gpui/class_test.go`'s new utilities, the six fixture packages' discrimination tests.

**Gaps**

1. **A NATIVE BUG, reported not fixed** (no native changes in this ticket): the group-pool lease's `needs_half` is computed from the record's CONTENT blur only (`reference/native/src/renderer/draw.rs:3506`), so a filter group whose content chain carries no blur but whose drop-shadow ops do takes a bucket WITHOUT the half-resolution scratch, and `composite_filter_group` fails with **ERR_DEVICE** at `lease.half_a_rtv.is_none()` (`draw.rs:5301-5316`; the window-sized bucket path takes the window pair, which is why 52's r52 drop-shadow readback - a full-window bucket - passed). The dropshadow fixture carries a 0.1px content blur on BOTH sides as the documented workaround; the fix is the lease accounting for the ops' blur, then dropping the blur from the fixture.
2. **Image masks** (`mask-image: url(...)`) are not at the style layer: the native atlas mask stretches the WHOLE polychrome texture over the group bounds (52's recorded gap) - a dedicated mask texture per image is the fix. The scene seam (EffectGroup with `MaskKindAtlas` + the texture index) is available.
3. The clip-path coverage entry's path index is the INSERTION index (a full-window-bounded path record's post-sort index equals it); a path inserted inside an older layer after the coverage path can reorder the array - the scene-level `InsertCoverage` callers are robust (out-of-range hides the element, never a failed frame), and the style path is the only user.
4. The coverage rasterization is window-frame (52's recorded limit - no transforms in this ticket's style path, so unhit); a clip-path on a transformed group (ticket 56's) samples the window frame.
5. The `ClipPathOfPath` form scales a tessellated path's vertices (the S/T curve parameters desynchronize - the coverage's edge AA can mis-shape); the shape forms (circle/ellipse/inset/polygon) tessellate at device scale and are exact.
6. Replayed (cached-view) ranges re-insert the gradient/coverage/op-chain table entries (52's replay arms) but the REPLAYED records keep their baked table indices - a range replayed into a scene that already carries other gradients could shift them; no fixture or port path hits it (the frame loop rebuilds scenes).
