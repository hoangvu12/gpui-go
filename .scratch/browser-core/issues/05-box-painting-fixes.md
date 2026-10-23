# 05 - Box painting fixes: clip inset, radius scaling, object-fit

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat e1c615c2
Blocked by: 01
Triage: ready-for-agent

**What to build:** Overflow clipping always insets by the border widths, overflowing unequal corner radii are scaled by one common factor as in CSS, and images with object-fit: none are centred and fitted to the content box.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures: a bordered overflow-hidden box with a child at the edge, a box with radii that overflow, an image with object-fit none/contain/cover inside padding.
- [x] All match Chrome; changed fixtures carry divergence notes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified on core/05-box-painting before merge. Re-ran vet, `go build ./...` and `go test ./internal/parity/... ./gpui/... ./base ./internal/capturespec` (ok). All 4 fixtures and the 6 deliberate-failure tests pass. Re-recording boxpaint-radii gave an identical screenshot. The Chromium citations (FloatRoundedRect::ConstrainRadii, LayoutBox::OverflowClipRect) match the source. Known residual: the pinned shader can't draw a radius past half a side, which R16's rounded mask fixes.

## Answer

**Implemented on `core/05-box-painting` (headless only; three new fixtures recorded with Chrome 155.0.8059.85, test-first: every fixture failed Tier C before the fixes).**

### What changed (with file:line)

1. **Overflow clipping always insets by the border widths** (CSS clips at the padding box — Chromium `LayoutBox::OverflowClipRect` contracts `PhysicalBorderBoxRect` by `BorderOutsets` unconditionally, `core/layout/layout_box.cc:2288-2302` in the research checkout):
   - `gpui/style.go` `OverflowMask` (`gpui/style.go:1218-1250`): the border-widths inset is no longer conditioned on a visible border color. Recorded divergence toward CSS: the pin insets only when the border color is set and opaque (`style.rs:812-820`).
   - The pinned mixed-axis mask selection (x visible, y clipped) is unchanged (the CSS overflow-value computation belongs to spec ticket V2).
2. **Overflowing unequal corner radii scale by one common factor** (CSS Backgrounds 3 §corner-overlap, f = min(Li/Si) over the four sides — Chromium `FloatRoundedRect::ConstrainRadii`, `platform/geometry/float_rounded_rect.cc:289-305`):
   - `gpui/style.go` `Corners.ConstrainRadiiForQuadSize` (`gpui/style.go:1037-1070`) replaces the per-corner `ClampRadiiForQuadSize` (removed): all four radii multiply by one factor when adjacent sums overflow a side. Recorded divergence toward CSS: the pin clamps each corner independently to half the shortest side (`geometry.rs:2511-2523`).
   - Call sites updated: `gpui/div.go:1873-1876` (paintBox) and `gpui/paintapi.go:289-292` (image corner radii).
3. **object-fit: none is centred; images fit and clip in the content box** (Chromium `LayoutReplaced::ReplacedContentRect` from `PhysicalContentBoxRect`, `core/layout/layout_replaced.cc:474-477` + `layout_box.cc:3848-3856`; the default object-position 50% 50% centres every mode, `layout_replaced.cc:453-471`; and css-images-3 §the-object-fit: “replaced elements always clip their contents to the content box” — verified against the recorded Chrome pixels):
   - `gpui/capture.go` `ObjectFit.GetBounds` (`gpui/capture.go:612-679`): the `none` case centres instead of sitting at the origin. Recorded divergence toward CSS: the pin pins `none` at the origin (`style.rs:134-137`). (The centring is the shared math; the Surface element also gets it, deliberately — it has no CSS twin.)
   - `gpui/img.go` `imgDrawable.Paint` (`gpui/img.go:562-608`): the object-fit base rect and the image's clip region are the element's **content box** (border box minus border widths minus padding) instead of the pin's border box (`img.rs:492-493`). Recorded divergence toward CSS. The new `Style.ContentBox` helper lives at `gpui/style.go:1252-1280` (padding fractions resolve against the element's own width — a paint-time approximation, exact for px/rem).

### Fixtures (new package `internal/parity/fixtures/boxpaint`, registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`; records in `internal/parity/testdata/`)

- **`boxpaint-clip`** — a 160×120 overflow-hidden box with a 5px **transparent** border and a 160×120 child at the content origin (the observable case: a transparent border still insets the clip), paired with a visible-border control box. Measured pre-fix (pin): whole 0.367, clip ROI 4.900, hot 0.069 → post-fix **0.0000 everywhere (pixel-exact)**.
- **`boxpaint-radii`** — a 100×40 box with `border-radius: 30px 0 0 0` (CSS keeps 30, the pin clamps to 20) and an 80×40 box with `border-radius: 42px 21px 0 21px` (the left pair 42+21 overflows the 40px height; CSS scales every radius by 40/63 → 26.67/13.33/0/13.33, the pin clamps to 20/20/0/20). Measured pre-fix: whole 0.119, factor ROI 7.79, uneven ROI 1.38 → post-fix: whole 0.0047, factor ROI 0.2076, uneven ROI 0.1303.
- **`boxpaint-objectfit`** — three 128×108 images (4px border, 12px padding → a 96×76 content box) with object-fit none/contain/cover around an embedded 60×40 quadrant PNG (`image.png`, injected into the page as a `data:` URI; the gpui side builds it with `gpui.NewImage`). Measured pre-fix: whole 1.800, ROIs 10.2–12.7 → post-fix: whole 0.1084, none 0.2659, contain 0.6213, cover 1.1199 (hot ≤ 0.0178).

Tier C tolerances (per-fixture, provisional, set at ~2× the measured correct-view statistics; the deliberately-wrong views in `boxpaint_test.go` must fail them — unclipped 4.90/ROI, per-corner clamp 4.16/ROI, fill 12.48/ROI):

| fixture | mean | ROI mean | hot |
|---|---|---|---|
| boxpaint-clip | 0.02 | 0.1 | 0.002 |
| boxpaint-radii | 0.02 | 0.5 | 0.004 |
| boxpaint-objectfit | 0.25 | 2.5 | 0.04 |

### Intended divergences (toward CSS, noted in code and tests)

- The four changes above each carry a “recorded divergence toward CSS” note at the changed site.
- `gpui/styled_test.go` `TestOverflowMaskInsetsByBorderWidths` (was `…InsetsByVisibleBorder`) pins the new always-inset rule including the transparent-border case, with the divergence note in the comment.
- `internal/capturespec/capture_element_test.go` `TestObjectFitGetBounds`'s “none” case now expects the centred bounds, with the divergence note.
- Harness extension, documented in `docs/browser-parity-harness.md`: fixtures may embed a PNG and inject it as a `data:` URI (image support); a fixture with no instrumented events (a painting fixture) is now valid (`internal/parity/recorder.go` `instrumentScript` normalizes nil `EventKinds`); and the flex-defaults note (the page pins `display: flex` / `flex-shrink: 0` where the pin's defaults differ from the browser's).

### Known limitations (measured, not silently absorbed)

- **Pinned shader confines each corner's arc to its quadrant.** A CSS-constrained radius larger than half the box's side (the common corner-overlap outcome) renders with a small band where Chrome's arc crosses the midline and the pin cuts straight (the `radii-uneven` ROI 0.1303 and `radii-factor` ROI 0.2076 residuals). Full CSS rendering needs the native rounded-coverage mask (R16's rounded content mask), not a Go change.
- **Image resampling** differs (the atlas's edge-texel blends at image boundaries and the linear sampler vs Chrome's upsampling): the object-fit fixture's measured stats are that difference — the image rectangles themselves match to a pixel (verified by quadrant-edge measurements).
- `internal/portfixture` `TestFx0004TextGeometryMatchesRecordedReference` still fails on main (system font count 94 vs 95) — pre-existing, unrelated, untouched.
