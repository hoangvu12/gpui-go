# 42 - Grid tracks, justify, baselines and flow-root

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 11 (Claude Code); worker chat 02be753f; R16 integration branch native/r16
Blocked by: 39
Triage: ready-for-agent

**What to build:** Grid accepts full track lists (fixed and fr mixes, auto rows, areas) and justify-items/self; items-baseline aligns text baselines; flow-root is available.

**Blocked by:** 39 (R15 native rebuild)

**Status:** ready-for-agent

- [ ] Fixtures with 240px 1fr grids, areas and baseline-aligned mixed font sizes match Chrome's boxes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-27, orchestrator 11: verified on `native/r16` (36a329e..ae84f31); it reaches main with ticket 54. Clean tree, no artifacts committed. The test DLL `2fbaa85a…` matches the shared target's build output. With the manifest swapped in: `go vet ./...` clean; the broad batch set plus the native spec packages gives 74 packages ok, failing only portfixture TestFx0004 (known), the 7 loader tests that fail by construction under the swap, and two known flakes that pass alone (clipspec contention, gpui/script TestFrameOverheadIsUnder1msAtTheProbeSize under load). Spot-check: flex_layout_algorithm.cc:2018-2026 at 0d4dcd4f (baseline offset = max ascent - item ascent) matches. Gaps accepted: text paints at the raw-metric baseline while Chrome paints at the strut's (up to ~0.6 px); a first line mixing font sizes reports the base font's strut baseline.

- 2026-10-26, orchestrator 11 (the approach): fold into the rev 16 rebuild on `native/r16`. Feed the measured first baseline (rev 15's measure response) into Taffy's algorithms by driving them through our own implementation of Taffy 0.13's low-level tree traits (LayoutPartialTree and friends), so a measured leaf returns `first_baselines` instead of the hardcoded `Point::NONE`; keep every existing layout fixture byte-identical. Grid track lists, justify-items/self and flow-root already work since rev 15; check grid-template-areas.

- 2026-10-24, orchestrator 6: rev 15 (39) carries the text-run fields and the measure baseline through the ABI, but they do not drive shaping or alignment yet. The pinned Parley TextLayoutRequest and Taffy leaf compute have no slots for them; see TEXT_ABI.md and LAYOUT_ABI.md. This ticket needs a follow-up native change, and so a rebuild that needs the user's approval, before its behaviour can match Chrome. Grid track lists, justify-items/self and flow-root do work in rev 15.

## Answer

Done on `native/r16` in two commits (the native trait-driven compute +
the Go styling/fixtures, then the dts track arms) plus this answer.
**Layout service v2** — the table grew two append-only entries
(`layout_node_set_grid_template_areas`, `layout_node_set_grid_area`;
15 → 17 function pointers, 168 → 184 bytes, every earlier slot offset
unchanged) — and the compute path changed with no record layout
change. Native revision stays **151** (ticket 54 sets 16).
Interim-bundle DLL SHA-256
`2fbaa85a85c96e29377aaab50e6c53b015c804a072098020233cbc713528c6ce`,
19,464,192 bytes, static CRT, mask `0xFFF`; the embedded artifact
stays at the rev-15.1 pins until 54 (the manifest swap ran for the
tests only, reverted before every commit).

### The trait route landed (the decided approach)

`reference/native/src/layout.rs` now drives Taffy 0.13's algorithms
through the service's own `GpuiLayoutView` — an implementation of
`TraversePartialTree`, `LayoutPartialTree`, `CacheTree`,
`LayoutFlexboxContainer`, `LayoutGridContainer` and
`LayoutBlockContainer` over the SAME `TaffyTree` storage (every trait
body mirrors taffy's `pub(crate)` `TaffyView`, delegated through
`TaffyTree`'s public trait impls where its fields are private) —
instead of `compute_layout_with_measure`. The measured leaf arm calls
`compute_leaf_layout` and then patches the measure response's first
baseline into `LayoutOutput.first_baselines.y`, offset by the leaf's
resolved top padding and border (border-box-relative, exactly the
contract the algorithms consume), so `align-items: baseline` /
`align-self: baseline` align text baselines like Chrome
(`flex_layout_algorithm.cc:2019-2027`: `ItemPosition::kBaseline` →
`offset = max_ascent - ascent`; `grid_layout_utils.cc:210-229`
`ComputeBaselineOffset`: "the difference between the grid item's
baseline and its track baseline" — the §11.5.1 shim). The cache
replays patched outputs whole, and a baseline-less measure keeps the
rev-15 bottom-edge fallback everywhere, so every existing fixture is
byte-identical. Two structural consequences of the route, recorded in
LAYOUT_ABI.md: `TaffyTree`'s layout slots are `pub(crate)`, so the
view's `set_unrounded_layout` writes the service's own per-node store
(identical values; rounding is disabled); and `RoundTree`/
`PrintTree` are unimplemented (unneeded — rounding is off).

### The Go side

- `MeasureFunc` returns `MeasuredSize` (size + optional
  `FirstBaseline`, logical px; the adapter converts to the unrounded
  device value, the definite-available-space rule). The text leaf,
  StyledText and the inline paragraph report **Chrome's strut math**:
  the font's FIXED (rounded) ascent/descent with the leading's ascent
  side floored (`inline_box_state.cc:182/202` FixedAscent/
  FixedDescent, `line_utils.cc:35-44` CalculateLeadingSpace's floor)
  — measured at Segoe UI 12/16/24px under line-height 40: baselines
  25/26/30, so baseline rows offset the 12px and 16px items 5px and
  4px and grow to 45.
- The style layer grows the full track lists (`GridColsList`/
  `GridRowsList` of minmax pairs, forwarded through rev 15's
  `SetGridTracks`; `240px 1fr` = the fixed minmax + `minmax(auto,
  1fr)` per §7.2.3), `GridTemplateAreas` (the CSS template parsed
  into 1-based line rectangles) and `GridArea` (the item's named
  placement), with the div builders, the refinement fields, the
  Tailwind classes (`grid-cols-[240px_1fr]`, `grid-rows-[...]`,
  `grid-areas-["..."_"..."]`, `grid-area-[name]`) and the
  regenerated dts (numeric template arms for the track lists; the
  full track grammar and the area classes stay load-checked, the
  composed forms' rule — the drift tests' string-hole sampler fills
  hex-like strings, which a structured value rejects).
- A track length now requires its `px` unit (a bare number is not a
  CSS track size — the candidates probe caught it).

### Fixtures (the acceptance)

`internal/parity/fixtures/gridlayout`, recorded against headless
Chrome 155 (`record.json` + `chrome.png` + `page.html` checked in):

- **gridtracks** — `grid-template-columns: 240px 1fr` at 460px (the
  fr resolves 220) and 360px (120) with auto rows (the row is its
  tallest item); every box byte-exact. The wrong view (the pinned
  `repeat(2, minmax(0, 1fr))`) fails Tier A on the second columns.
- **gridareas** — `grid-template-areas: "head head" "side main"` over
  `120px 1fr`: head spans both columns, side/main split row 2;
  byte-exact. The wrong view (auto placement) fails Tier A on all
  three cells.
- **gridbaseline** — a flex row and a grid row of 12/16/24px spans
  (line-height 40, width 60) under `align-items: baseline`: the items
  offset 5/4/0 and both rows are 45 tall, byte-exact against Chrome
  (Tier A). The wrong view (items-start, the pre-ticket pin's
  effective behaviour for equal-height items) fails Tier A on the
  offsets and the row heights.

`grid-template-areas` did NOT exist in the record — that is why the
  two new ABI entries landed (append-only). Justify-items/self and
flow-root were already rev-15 surface (the ticket's check item);
`ItemsBaseline()`/`SelfBaseline()` and the `items-baseline`/
`self-baseline` classes already existed.

### Verification

- `cargo test -p gpui-go-native --lib layout::` — 13 passed, 1 failed:
  `style_translation_rejects_invalid_values`, the KNOWN pre-existing
  rev-15 failure (display tag 4 = FlowRoot is valid now; not
  investigated per the standing list).
- The full final run under the bundle swap
  (`GPUI_GO_NATIVE_BUNDLE=gpui-r16-bundle`, `go test -p 1 -count=1
  ./internal/native/... ./internal/scenespec
  ./internal/rendererspec ./conformance/... ./internal/portfixture
  ./internal/parity/... ./gpui/ ./gpui/script/...
  ./internal/glyphspec ./internal/textspec ./internal/layoutspec
  ./internal/fontspec ./internal/textframespec ./internal/inlinespec
  ./internal/listspec ./internal/tsxgen/`): every package passes
  except the known pre-existing set — the 7 by-construction loader
  tests (the manifest swap), `internal/portfixture`
  `TestFx0004TextGeometryMatchesRecordedReference` (the font count).
- `go vet ./...` clean; gofmt clean on every touched file.
- No existing fixture changed: the whole parity suite (including
  fx-0001/fx-0002's layoutspec and every browser-parity fixture)
  passes byte-identical before the new fixtures were added.

### Recorded deviations / gaps

- The ink-paint drift: the port paints text at the pin's raw-metric
  line baseline while Chrome paints at the strut's fixed-metric one
  (up to ~0.6px at the leading-split floor). gridbaseline's Tier C
  tolerance is calibrated at 2× the measured drift (mean 5.0, hot
  0.07) and its Tier A boxes pin the alignment byte-exactly; a
  follow-up could move the paint layer to the strut baseline.
- A paragraph whose FIRST line mixes font sizes reports the base
  font's strut baseline (Chrome's line baseline would follow a taller
  run); the fixtures keep one font size per item.
- The dts does not advertise the structured class values beyond the
  numeric track-list arms (see above); the engine accepts the full
  grammar at load.
