# 55 - Group opacity, rounded clipping and border order from styles

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: core/55 implementer (Claude Code; worktree core-55, branch core/55-group-opacity-rounded-clip-styles)
Blocked by: 54
Triage: ready-for-agent

**What to build:** Styles use the new compositor: opacity applies to the group (D9), rounded corners clip descendants, and borders paint before children as in CSS.

**Blocked by:** 54 (R16: integrate, build rev 16 and measure)

**Status:** ready-for-agent

- [x] Fixtures: translucent element with overlapping children, rounded card with an image, bordered box with an overflowing child, all match Chrome.
- [x] Changed fixtures carry divergence notes. (No existing fixture changed - every pre-existing record passes pixel-identical; the Answer records why.)

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-27, implementer: the pre-existing ticket text carried a stray "Status: ready-for-agent" line below the blockers; the tracker's Status field (top) is the lifecycle of record and is now `claimed` with the Answer below. Native revision 16 (the embedded artifact from 54) drives everything; no native or cargo changes were made.

## Answer

Landed on `core/55-group-opacity-rounded-clip-styles` (Go-only; the native rev 16 artifact from ticket 54 drives it).

**What landed**

- **D9 group opacity** (`gpui/element.go` WithElementOpacity, `gpui/group_styles.go`): `WithElementOpacity` gained the group path - a non-identity element opacity brackets the element's whole style paint in an effect group (HasOpacity, the border-box bounds, the intersected mask) and the paint context is NOT multiplied: the group's composite applies the opacity once, so overlapping children blend inside the group before the group blends with the page (Chromium's "at least two layers in subtree draw content" surface rule, cc/trees/property_tree_builder.cc:395-403). The single-primitive subtree folds natively (the plan's single-primitive opacity fold multiplies the record's colour at snapshot time), so the transition fixture's mid-flight frames are pixel-identical to the multiply - no re-record. The div and img call sites pass the element bounds and the group condition; one documented exception keeps the per-primitive multiply: an element with a backdrop-filter chain (the native backdrop pass samples the CURRENT target - inside a group's bucket it would blur the empty bucket; the native gap is recorded, `opacityCompositesAsGroup`).
- **The rounded overflow clip** (`gpui/style.go` OverflowMask, `gpui/group_styles.go` withRoundedClipScope, `gpui/element.go` WithContentMask): `Style.OverflowMask` returns the new `ClipMask` (the rect - the padding box, the border box inset by the border widths, always - plus the INNER border radii: the outer radii shrunk by the border widths per corner, clamped at zero, and the corner smoothing; Chromium's UpdateInnerBorderRadiusClip reading ContouredBorderGeometry::ContouredInnerBorder, paint_property_tree_builder.cc:3257-3293, contoured_border_geometry.cc:209-219). One axis visible: no radii (css-overflow §corner-clipping, NeedsInnerBorderRadiusClip paint_property_tree_builder.cc:3108-3112). The new `WithRoundedContentMask` (element.go) takes the ClipMask - the pin's `WithContentMask(*Bounds)` is untouched, so gpui-go-base's four call sites keep compiling - and a rounded mask brackets the scope in a `rounded_clip_only`/`ClipToBounds` effect group (the paint phase only - `frameDrawState.paintPhase`, set by drawRoots; a prepaint-inserted pair would bracket nothing and drop the clip). The div, img, list and sticky call sites hand `Style.OverflowMask`'s result to the rounded form; `scrollMaskOf` copies the list style's radii along. Nested rounded clips nest their groups (each composite clips its subtree; the outer group's composite clips the inner's) - `TestNestedRoundedClipsNestGroups`.
- **The scene encoding carries real radii** (`gpui/scene.go`, `gpui/scene_kernel.go`, `internal/native/scene.go`): `Quad` and `Shadow` grew `MaskRadii`/`MaskCornerSmoothing` (zero values = the pre-R16 rect-only mask bit-exact; every existing record site compiles unchanged), the kernel seam packs them into the 56-byte ABI mask (`SceneContentMaskRounded`; the fade edges stay zero - fades are ticket 57) and the dumps round-trip them.
- **Borders paint before children** (`gpui/div.go` paintUnit): the border quad moved from after the continuation to the inset shadows' side - CSS's background phase (paints backgrounds AND borders before the foreground, paint_phase.h:33-35; box_fragment_painter.cc's PaintBoxDecorationBackgroundWithRectImpl paints the border as the decoration phase's last step, after the drop shadows, background and inset shadows). A child poking over the border box now paints above the border (the pin painted the border after the children, covering the child) - `TestBorderPaintsBeforeTheOverflowingChild`.
- **52's leftover closed**: the pixel gate for a smoothed content mask (`internal/rendererspec/r55_smoothed_mask_gpu_test.go`): a quad authored through the Go record seam with a mask carrying radii clips INLINE (no effect group - the vendored quad pipeline's mask SDF) and the mask's own corner smoothing reshapes the corner band measurably against a smoothing-0 control.

**Fixtures** (recorded in headless Chrome 155.0.8059.85, registered like the others)

- `opacitygroup` - a 0.5-opacity 120x120 parent with two overlapping absolute children (30px overlap): the record's overlap band reads the group's single 0.5 blend; the wrong view (the pre-D9 per-primitive multiply - the children each at 0.5, the parent's colour pre-multiplied) fails Tier C in the parent's ROI while Tier A holds.
- `roundedclip` - a 120x90 card with a 36px radius, overflow hidden, a covering image child (the embedded quadrant PNG, the boxpaint object-fit pattern): the corners cut; the wrong view (the rectangular clip - the pre-R16 rect-only mask, the image's corners escaping the arc) fails Tier C. The large radius against the small card makes the corner arcs a big enough ROI fraction to clear the image-resampling tolerance.
- `borderorder` - a 200x120 12px-bordered box with a 200x200 in-flow child covering the right and bottom border bands (overflow: visible, the port's default block display): the child paints over the border; the wrong view reproduces the pin's order exactly (a border-only overlay absolutely positioned over the box, painted last) and fails Tier C while Tier A holds.

**Changed fixtures**: NONE. All 40+ existing fixture packages pass pixel-identical (the serial run and the full `./internal/parity/...` pass): every existing translucent element in the fixtures is single-primitive (the fold keeps its pixels), no fixture clips a rounded overflow (boxpaint's radii boxes do not clip; its clip boxes have no radii; lineclamp's clip is rectangular), and no fixture's child pokes over a border. No re-recorded tolerances, no divergence notes needed - the D9 and border-order changes are observable only through the new fixtures and the unit tests.

**Test results**

- `go test -p 1 -count=1 ./gpui/ ./internal/parity/... ./internal/scenespec ./internal/rendererspec ./internal/portfixture ./internal/textframespec ./internal/inlinespec ./internal/native/... ./gpui/script/... ./examples/...` - all ok except portfixture `TestFx0004TextGeometryMatchesRecordedReference` (the KNOWN pre-existing font-count failure).
- `go vet ./...` clean; `go run ./cmd/displaycodemod -check ./...` and `go run ./cmd/positioncodemod -check ./...` both 0 (736 files scanned).
- New gates: `gpui/group_styles_test.go` (the mask's inner radii and axis rules, the rounded-clip group emission + the rect-clip non-emission, nested groups, the opacity group over overlapping children, the backdrop-filter multiply exception), `TestBorderPaintsBeforeTheOverflowingChild` + the reworked `TestDivPaintsTheBoxInPinnedOrder` (styled_test.go, the roles identified in the sorted dump; the CSS order), `TestDivPaintRadiiClampAndOpacity` and `TestTransitionStyleInterpolatesOpacityAndBackground` re-pointed at the effect-group encoding, `TestOverflowMaskInsetsByBorderWidths` finding the child by role, and the rendererspec `r55_smoothed_mask_gpu_test.go` pair.

**Gaps / deviations**

1. **Every style-authored rounded overflow clip becomes a `rounded_clip_only` group, not just nested ones** (the design's "the innermost rounded clip is free, inline" is not ridden by the style path): the pinned underline/sprite/path pipelines clip mask rect+fade only (the recorded native divergence 4, reference/native/shaders/mod.rs) - an image or text child under an inline-only rounded mask would escape the corners (the roundedclip fixture would fail). The group composite clips every primitive kind with one anti-aliased rrect (carrying the radii on the records too would double the corner coverage: the composite's clip times the inline clip, halving the corner AA band). The inline mask radii remain the record-level encoding (Quad/Shadow MaskRadii, gated by the rendererspec tests) for the day the pipelines are vendored. Cost: one pooled target per rounded clipping element (reused across frames, the R15 filter-group cost class; 58/60 own the damage side).
2. **Backdrop-filter + opacity keeps the per-primitive multiply** (the native gap above): CSS composites the blurred backdrop inside the opacity group; the native backdrop pass samples the current target, so a group's bucket would blur nothing. Recorded as the follow-up for whoever vendors/extends the backdrop pass.
3. A child fully DISJOINT from the clip group's bounds (no intersection at all) can sort outside the group's bracket (the pin's bounds-tree semantics, the same shape the R15 filter groups have always had) - it would paint unclipped where CSS clips it invisible. Edge case, not hit by any fixture or test.
4. The sprite/underline/path records keep rect-only masks in the Go encoding (divergence 4: the pinned pipelines consume bounds+fade only); the ABI fields exist, the Go fields arrive with the pipeline vendoring.
5. 56's transform groups and 57's fades/masks/blend styles are untouched (this ticket's scope was D9, the rounded clip and the border order).
