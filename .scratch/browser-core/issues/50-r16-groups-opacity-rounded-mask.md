# 50 - R16: group targets, opacity groups and rounded content mask

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 11 (Claude Code); R16 integration branch native/r16
Blocked by: 39, 49
Triage: ready-for-agent

**What to build:** On the R16 integration branch: a pool of offscreen group targets with no nesting cap, group opacity, and a rounded content mask that clips descendants to rounded corners.

**Blocked by:** 39 (R15 native rebuild; R16 records extend it), 49 (R16 compositor design)

**Status:** ready-for-agent

**Design ([docs/design/r16-compositor.md](../../../docs/design/r16-compositor.md) §2, §3.1-3.3, §7, §8.1; approved 2026-10-24):** One effect-group record (`GpuiGoSceneEffectRecord`, plan command kinds 3 and 4) carries opacity, transform, blend, mask, rounded clip and property node. Filter boundaries stay for R15 chains, and both kinds share one group stack. `MAX_FILTER_GROUP_DEPTH` is removed and replaced by a bounds-sized, bucketed target pool that is kept across frames. Over the pool budget a group renders inline, and this must be **observable**: a debug-stats counter and a warning in debug builds, never silent and never a crash (user, 2026-10-24). A single-primitive opacity group folds into a per-primitive multiply. The content mask grows to 52 bytes (bounds, `fade_out`, radii, smoothing). Cut the integration branch only after R15 (39) merges.

- [ ] Native spec tests for nested groups beyond the old depth of 2, group opacity and rounded clipping.
- [ ] Pool overflow increments a debug counter and warns in debug builds; a test covers it.
- [ ] R15's gradient record and its 2-stop path stay bit-exact.
- [ ] Work lands on the shared R16 integration branch; green is promised at ticket 54.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Landed on `native/r16` (rev 16, scene service v6 / renderer service v5, native revision left at 151 for ticket 54).

**What landed**

- **The effect-group record and plan pairing**: `GpuiGoSceneEffectRecord` (primitive class 10, 116 bytes) with `insert_effect`/`effect_dump` (table slots 27/28) and the `insert_batch` op kinds 12/13; the plan compiles matched pairs into command kinds **3/4** (begin/end effect group) through the **same group stack** as the R15 filter boundaries (one shared render stack, kind-checked matching, one `MAX_SCENE_GROUPS = 1024` sanity bound counting start markers of both kinds -> `ERR_LIMIT`). The effect record carries and validates the full A.1 payload (transform with an invertibility check, blend tag, mask kinds - 0-only this revision - property node, radii, smoothing); tickets 51/52 wire transform/blend/mask/property-node, 55 emits them from Go styling.
- **`MAX_FILTER_GROUP_DEPTH` removed**: every matched group of either kind gets an isolated pool target slot (nesting-based, so <=2-deep plans are byte-identical to R15); `requirements.isolated_target_count` is the pool high-water mark and `isolated_effect_count` was appended to the requirements record.
- **The group target pool**: bounds-bucketed acquire/release, retained across frames (frame-used pruning per viz's UpdateRenderPassTextures rule), 64 targets / 256 MB per surface; over the budget the group renders **inline, observably** - the cumulative overflow counter plus a debug-build `eprintln` warning (never a crash; a 70-deep scene renders through with the surviving composites). Exposed through the new `renderer_surface_debug_stats` entry (renderer v5, `GpuiGoDebugStatsRecord`: pool targets/bytes/overflow).
- **Group opacity**: the explicit no-blur `composite_group` path (the R15 `dx_blur_and_composite` early-out kept for filter records, the effect composite always draws), and the **single-primitive fold** - a pure-opacity effect group over exactly one primitive with no nested markers emits inline begin/end commands and the opacity multiplies that primitive's colour at snapshot time (Chromium's "at least two layers draw content" rule; the stored records stay as inserted). The fold reads the START marker's record (the pair's shared snapshot).
- **The 52-byte content mask** (bounds + `fade_out` LTRB + radii + smoothing) in every record that has one, plus one layout-pad word (56 bytes total): the pin's shader-side mask finally carried. The innermost rounded clip applies **inline** in the vendored quad/shadow pipelines (`ContentMask::alpha` multiplies in the rrect SDF); ancestor rounded clips become `rounded_clip_only`/`clip_to_bounds` effect groups. Fades ride the pinned underline/sprite/surface pipelines too (the pinned `ContentMask` already had `fade_out`; the ABI finally carries it). The quad/shadow records now use the PINNED field order + trailing layout pads because they ARE the vendored pipelines' instance layouts (HLSL aligns vec2f to 8 bytes; the pin's field order is the alignment-safe one).
- **Ticket 77's partial-region backdrop crop zoom folded in**: the backdrop crop carries its own crop-sized half-resolution blur pair (the rev-15 code ran the gaussian on the window-sized ping/pong at crop-relative coordinates, zooming a partial-region backdrop's blurred input; a full-window backdrop keeps identical sizes and pixels). Verified by `TestR16BackdropPartialRegionBlurIsUnzoomed`.

**Record sizes / versions**: scene v6 (the design's "v5" - ticket 77 had taken 5; recorded in the design doc, SCENE_ABI.md, scene.rs and the Go mirror), renderer v5, native revision 151. Records: quad 236->280, shadow 172->216, underline 72->112, filter 780->820, surface 56->96, sprite 124->164, polychrome sprite 104->144, path 116->156, content mask 16->56; effect record 116 (new); table sizes: scene 288->312 (2 pointers + 3 scalars), renderer 184->200 (1 pointer + 1 scalar).

**Tests and results** (all with `GPUI_GO_NATIVE_BUNDLE` pointing at the interim bundle, DLL SHA-256 `588cd19d9efcd57aa0c8bd2eb40a1c7de87c296b0fd91c7b1b46173419230215`; the embedded artifact was NOT touched - the manifest swap during testing was reverted before the commit):

- `go test ./internal/scenespec/ ./internal/rendererspec/ ./conformance/` - ok. New gates: `r16_groups_test.go` (plan pairing at depth 0-5, both kinds on one stack, the fold vs the group, the clip-blocking-the-fold, MAX_SCENE_GROUPS at 1024 via the flush, effect validation, mask validation/round-trip, dump/replay/sort) and `r16_groups_gpu_test.go` (group-free pixel identity, 4-deep nested opacity (3 isolated - beyond the old 2), group-opacity-vs-fold pixel identity, rounded clip incl. the nested case, pool overflow counter + cap, partial-region backdrop, clean stats).
- `go test ./internal/parity/...` - all 40+ fixture packages ok (the R15 filters/shadow/gradient fixtures bit-exact).
- `go test ./internal/portfixture/` - fx-0007 and fx-0003 pass with the documented, scoped R16 divergences (the depth-3 group now isolates: 6 accepted fields per fixture, asserted exactly); fx-0004 fails on the KNOWN pre-existing font-count (94 vs 95).
- `go test ./gpui/` - ok.
- `cargo test -p gpui-go-native --lib` - 117 passed, 2 failed: both PRE-EXISTING failures in modules this ticket did not touch (`layout::tests::style_translation_rejects_invalid_values`, `text::tests::font_add_rejects_bad_data_and_arguments`). The 5 pre-existing a11y/capture test compile errors were fixed (small, test-only: a missing trait import, unsafe probe calls, a null-texture stand-in; windows-core was added as a dev-dependency for the `implement` macro).

**Deviations from the design**

1. **The pool's buckets are window-sized this revision, not bounds-sized.** The bounds-sized bucketing landed structurally (the pool, the offset-viewport plumbing, the region-relative composite all speak it, and `GROUP_TARGET_BUCKET_GRANULARITY` is defined), but the small-bucket path has an unresolved pinned-sprite-pipeline issue: a polychrome sprite batch draws nothing into a pool target smaller than the window (the vendored quad/shadow pipelines work; the pinned sprite pipeline does not - verified by direct bucket dumps, with the RTV and offset viewport confirmed correct). Rather than ship a silently-wrong sprite path, every bucket = the window: exactly the R15 group-target shape, so all R15 fixtures stay pixel-identical **by construction** (verified). Handed to **51/52** (the transform work needs local-space targets anyway) with the debugging trail in the ticket's git history.
2. The design's A.2 mask sizes assumed 16-byte masks + 36-byte growth; the HLSL structured-buffer alignment rules (vec2f = 8 bytes, struct sizes round up) made the real numbers 56/40 with one pad word, and forced the quad record onto the pinned field order. The mask record is therefore 56 bytes with `mask_padding`, and the quad/shadow records carry `layout_padding`.
3. `scene_update_property` (A.7) is NOT implemented - it is ticket 51's work, as the design's section 4 order says.

**Handed to 51-53**

- **51**: the bounds-sized bucket finishing (the sprite-in-small-bucket bug above), `scene_update_property`, the matrix composite for `has_transform` groups.
- **52**: the mask table (`mask_kind` 1-3 are validated-rejected this revision), blend-mode compositing, the smoothed-corner SDF for mask radii (the plain rrect SDF is used this revision), and the pinned underline/sprite/path pipelines' rounded-mask gap (they clip rect+fade only - `shaders/mod.rs` divergence 4 records it).
- **53**: the retained-frame/damage/present work; the state-bind and tess-cache counters (the debug-stats record is extensible for them).
- The Go styling (WithElementOpacity's group path, Style.OverflowMask radii, the mask-intersection rules) is ticket 55 as planned; the Go mirror encodes zero-radii/zero-fade masks, so existing output is unchanged (verified: group-free scenes and all parity fixtures pixel-identical).

**Pre-existing failures seen during this run** (not investigated, per instructions): portfixture fx-0004 (font count), the two cargo-test layout/text failures above.
