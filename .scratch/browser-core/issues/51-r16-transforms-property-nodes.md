# 51 - R16: transformed composite and property nodes

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 39, 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: subtrees rendered offscreen and composited under a transform matrix, and scene property nodes for transform and opacity that can change without rebuilding the scene.

**Blocked by:** 39 (R15 native rebuild; R16 records extend it), 49 (R16 compositor design)

**Status:** ready-for-agent

**Design ([docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) §3.4, §3.10, §7, §8.1; approved 2026-10-24):** Render the subtree offscreen in local space, then composite it under a 2×3 matrix. Translation-only transforms skip the group. Add `scene_update_property(scene, node_id, kind, values)`: it mutates a finished scene's effect records in place and returns the old and new mapped bounds, so the same scene can be re-presented with damage. Property-only frames are **automatic**, detected from an active transform or opacity transition with no opt-in style. The Go half is ticket 60.

- [ ] Native spec tests for translate, scale, rotate and skew composites and for property-node updates.
- [ ] `scene_update_property` tests: records mutated, old and new bounds returned, the plan unchanged, a stale id rejected.
- [ ] Integration branch only; green at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Landed on `native/r16` (scene service v7, renderer v5, native revision 151 â€” 54 sets 16).

**What landed**

- **50's handed-off gap: the group target pool is bounds-sized.** The root cause of 50's sprite gap is a coordinate-invariant break, not a viewport bug per se: the sketched offset-viewport scheme (children at window coordinates, the viewport shifted by the bucket origin) makes every fragment's `SV_Position` target-frame while the records stay window-space, and every pipeline whose per-pixel geometry measures against the records then drops the whole primitive â€” the polychrome sprite's rounded-rect SDF (and its content-mask fades), the vendored quads'/shadows' border/radius geometry (50's "vendored pipelines work" held only for borderless, radii-less quads â€” the geometry early-out; a rounded quad in an offset bucket also draws nothing), the underline wave. The fix: each bucket renders at its **frame's origin** with the **globals' viewport size set to the bucket** (the pin's own `pre_draw` invariant, established for the window and now per target), so the children keep their true coordinates and every fragment's `SV_Position` stays in the records' frame. The bucket spans the frame's `[0, extent max]`, quantized to the 256-px granularity and clamped to the window â€” the LOCAL extent for a transform group's local-authored subtree (true bounds-sized), the content extent's far corner for window-frame groups (the region's distance from the origin inflates it; the price of the invariant), and the window for FILTER groups (R15's exact blur textures and scratch â€” a bounds-sized filter bucket would wrap the gaussian at the region edge; ticket 53 owns the region-limited blur). The composites draw the group's **content REGION** (the un-quantized extent) with the sampling offset the translation only, so the wrap-addressed sampling edge stays at the content's own edge and the quantization margin never reaches the target. The backdrop crop clamps to the current target (the bucket), not the window.
- **The transformed group composite (design Â§3.4):** an effect group with `has_transform` renders its subtree into the bucket in the group's **LOCAL frame** â€” the subtree's records and the group's own bounds/mask/radii are authored there, and the record's row-major 2x3 `[m00,m01,m10,m11,tx,ty]` maps local to parent frame (transform-origin and the group's position resolved by the caller into the matrix, CSS-style). The new vendored `transformed_blur_composite` pipeline maps the content region's corners through the matrix (the matrix rides the composite-extension cbuffer b5, appended after the rev-15.1 colour-matrix prefix as two `vec4` rows â€” a nested matrix struct violates the uniform layout rules and the prefix stays byte-identical), interpolates the local position, samples the bucket at `local / bucket_size` with **linear filtering**, and clips to the record's content mask (the rrect coverage at the LOCAL position with the record's radii â€” CSS clips inside the element, then the transform applies). A translation-only matrix composites through 50's axis-aligned path with its rects translated (keeping the smoothed-corner composite). The render walk re-derives everything from the CURRENT records, so a stale plan cannot mis-draw an updated node.
- **`scene_update_property` (design Â§3.10 / appendix A.7):** `(scene, node_id, kind, values, count, out_old, out_new)` mutates every FINISHED effect record carrying the node id in place â€” kind 0 the matrix (six f32 words; the records must carry `has_transform`), kind 1 the opacity (one word in [0,1]; `has_opacity`) â€” with **no re-sort, no re-plan** (the commands and orders never move; the render walk consults the mutated records), returning the old and new mapped device bounds (the record bounds mapped through the old/new matrix â€” the damage union the caller re-presents). `ERR_NOT_FINISHED` before finish, `ERR_BAD_VALUE` for a stale id, a zero id, an unknown kind, a wrong count, a missing flag, or a value the insert validation rejects; null out-pointers are allowed (no report).
- **The Go half:** `internal/native` v7 client (`Scene.UpdateProperty`, the table mirror and validation) and the gpui scene encoding â€” `EffectGroup` grows `HasTransform`/`PropertyNode`/`NodeID`/`Transform` (the pinned `TransformationMatrix` shape), `Scene.UpdateEffectMatrix`/`UpdateEffectOpacity` wrap the kernel seam, and the effect-group dump round-trips the new fields. NOT the Go styling/hit-testing (56) or the frame-loop fast path (60).

**Record/ABI changes:** scene service **v7** (table slot 29 `update_property`; 30 pointers, 312â†’320 bytes; **no records or scalars changed**); renderer v5 (no table change). The vendored composite-extension cbuffer (b5) appends `target_origin`/`padding2` (layout words, always zero) and `transform_row0/1` (the 2x3) â€” 704 bytes; the pinned prefix is byte-identical, and every path that draws into the window or the offscene target computes exactly what it did before (the added b5 vertex binding applies only where the shader declares it).

**Tests and results** (with `GPUI_GO_NATIVE_BUNDLE` on the interim bundle, DLL SHA-256 `6dea0dc44d55b03493ac2aebb000362feeca286058a777e44b4255422cdb16bd`; the embedded artifact untouched â€” the manifest swap reverted before the commit):
- `go test -p 1 -count=1` over internal/native/â€¦, scenespec, rendererspec, conformance/â€¦, portfixture, parity/â€¦, gpui, glyphspec, textspec, layoutspec, fontspec, textframespec: **all ok** except the 7 internal/native loader tests that check the embedded DLL against the swapped manifest (expected until 54) and portfixture fx-0004 (the KNOWN pre-existing font count, 94 vs 95).
- New rendererspec gates: `TestR16PolychromeSpriteInSubWindowBucket`/`TestR16QuadInSubWindowBucket` (the 50-gap regression: the pinned sprite pipeline and a rounded quad inside a 256-px bucket at a nonzero window offset), `TestR16{Translate,Scale,Rotate,Skew}Composite` (each readback compared against a CPU reference computed at the same bilinear sampling: the untransformed subtree read back once, inverse-mapped per output pixel, wrap-addressed), `TestR16RotatedClipComposite` (the rotated rounded clip), `TestR16PropertyUpdateRedrawsOnlyInDamage` (render â†’ update â†’ render through the same handle; pixels outside oldâˆªnew byte-identical, inside changed).
- New scenespec gates (`r16_property_test.go`): the matrix update mutates both markers with the exact bits, returns the old/new mapped bounds, leaves the plan byte-identical; the opacity update; the stale id, pre-finish, and every validation rejection as typed errors that leave the scene untouched.
- Everything 50 made green stays green: the R16 group suite, all 42 parity fixture packages, fx-0003/fx-0007 (the accepted R16 divergences unchanged), conformance.
- `cargo test --release -p gpui-go-native --lib`: **117 passed, 2 failed** â€” both pre-existing (`layout::style_translation`, `text::font_add`; a third font test flakes under parallel contention and passes in isolation).

**Deviations from the design (Â§3.4/Â§3.10)**

1. **The translation-only "skip the group" fast path is the GO paint level, not a native walk mechanism.** The design's "the existing element offset" is the paint context's own offset: a translated element's children paint at their translated positions with no group at all. Rendering the children at an offset through the viewport would break the fragment invariant above (the same root cause as 50's sprite gap). The native composites translation-only groups through the matrix (a lossless blit for integral offsets) so property-node translations still update.
2. **Filter groups keep window-sized buckets** (exactly R15's blur textures); a bounds-sized filter bucket would wrap the gaussian at the region edge and shift the R15 blur fixtures' edge pixels. Ticket 53's region-limited blur owns that change.
3. **The transform composite's rounded clip is the plain rrect SDF over the record's CONTENT MASK** (the group's own clip rect, local frame), not corner smoothing â€” 52's smoothed-corner SDF owns it; a window-frame group's bucket is inflated by its region's distance from the origin (the invariant's price; a group at (0,0) is truly bounds-sized).
4. The transform composite draws the CONTENT REGION, not the quantized bucket, and the group's opacity field is the composite's multiplier ALWAYS (set 1 for groups that do not fade â€” documented in the ABI).

**For 52/53:** 52's blend/mask work rides the same composite extension cbuffer (the b5 prefix is stable); the mask table entries remain validated-rejected. 53's damage entry can consume `out_old`/`out_new` directly (dilated by the bilinear half-texel support and any enclosing blur â€” the design's expansion rule); the state-bind counter and the tess cache are untouched. 55 authors the transform groups from Go styling (the local-frame contract is in SCENE_ABI.md); 56's hit testing maps through the inverse 2x3 (the ABI record carries the matrix); 60 is unblocked for the property-only frame path.
