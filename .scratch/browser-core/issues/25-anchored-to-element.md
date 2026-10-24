# 25 - Anchored to an element, with fallbacks

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 7 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Anchored can position relative to another element, with side, alignment and offset, flip/shift fallbacks when it doesn't fit, and hiding when the anchor is clipped, as in CSS anchor positioning.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Box fixtures for each side and for flip and shift near window edges match Chrome's anchor positioning.
- [ ] Base's positioner can be replaced in one component (proof, not the full migration).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

**API (gpui/anchored.go — the existing Anchored extended, no parallel element):**

- `AnchorTo(id string)` — position-anchor: the anchor names itself with `.ID(name)`; the div prepaint records its window-space bounds and its content mask (the clipping scroller's visible rect) into a per-frame registry (gpui/div.go:1643-1653, gated on the window having an Anchored consumer — the flag is set during request layout, gpui/anchored.go:451-456, so plain frames pay nothing; the lookup reads the current frame first, then the last completed frame — gpui/element.go:1332-1347).
- `AnchorBounds(cell *Bounds)` — the tracked-anchor form (Base's popup measure pattern); the cell is read at every prepaint.
- `Side(AnchorSideTop/Bottom/Left/Right)` + `Align(AnchorAlignStart/Center/End)` + `Gap(px)` — the position-area / anchor() inset geometry: bottom is `top: calc(anchor(bottom)+gap)`, end is `left: calc(anchor(right)-width)`, center is `anchor(center)-size/2` (gpui/anchored.go:700-725, the mirror of Chromium's ResolveAnchorValue, anchor_evaluator_impl.cc:93-186 — the end-side inset conversion `available_size - value` at :184).
- `TryFallbacks(AnchorFlipBlock, AnchorFlipInline, AnchorShift)` — position-try-fallbacks, an ordered list; the base placement is candidate 0 and wins outright when it fits (Chromium's OOFCandidateStyleIterator, out_of_flow_layout_part.cc:113-332 and the try loop :2140-2260; "none of the fallbacks worked out" falls back to the base style at :2232). Flip tactics mirror across the anchor's midlines (try_tactic_transform.h:72-190): on a vertical side flip-block flips the side and flip-inline flips the alignment, the other way round on a horizontal side. Shift is the port's clamp (Base's positioner clamp; CSS expresses it with min()/max() over the anchor() inset — anchor() is a math node, css_math_expression_node.cc:3926-3941). CSS never snaps implicitly, so the corner modes' snap does not apply in side mode unless AnchorShift is listed.
- `Margin(Edges)` — the viewport distance for the fit test and the shift (Base's Positioner::margin; zero by default = CSS-exact).
- `HideWhenAnchorClipped()` — position-visibility: anchors-visible: the element paints nothing while the anchor does not intersect the clipping scroller it lives in (Chromium hides the paint layer when the anchor's intersection with its nearest clipping ancestor is empty — anchor_position_visibility_observer.cc:21-83, the clipping-ancestor walk at :38-52 and the observer at :57-70; OnIntersectionVisibilityChanged :172-179). The layout bounds still exist, like the browser's. The keyword's spelling is `anchors-visible` (the singular `anchor-visible` is its non-deprecated alias; both parse — longhands_custom.cc:278-345, css_identifier_value_mappings.h:2304-2322).

The popup follows a scrolled anchor exactly (the anchor's prepaint bounds are post-scroll) — Chromium's equivalent is anchor_position_scroll_data.cc (position-anchor's anchor at :22, the scroll adjustment class at :63+). Flip-block/flip-inline are `TryTactic::kFlipBlock/kFlipInline` (computed_style_constants.h:627-634; the ordered tactic list, position_try_fallbacks.h:21-38). All cited files are cross-platform blink core compiled on Windows (core/layout/build.gni:28-35 pulled in by core/BUILD.gn:321; no OS guards in any of them).

**What changed:**

- gpui/anchored.go — the whole element-anchored mode: the types and builders (:195-395), the anchor resolution and the side-mode prepaint with the fallback iteration and the hidden flag (:574-671), the placement geometry and the shift clamp (:673-746), paint skipping when hidden (:751-770).
- gpui/element.go — the per-frame anchor registry: the window/frame state fields (:1026-1041, :1144-1147) and anchorRecord, recordAnchorBounds/lookupAnchorBounds/requestAnchorTracking (:1305-1361).
- gpui/render.go:227 — completeFrame publishes the frame's registry.
- gpui/div.go:1643-1653 — a named element records its bounds and content mask at prepaint (additive, next to the debug-selector recording — shared element code; ticket 33 note: this is the only div.go touch, and it is additive, not layout positioning).
- internal/parity/fixtures/anchor/anchor.go — four fixtures + wrong views, registered in internal/parity/replay_test.go and cmd/parityrecord/main.go.
- internal/parity/recorder.go:148-163 — the record now settles (two animation frames) before reading the transcript/boxes/screenshot; docs/browser-parity-harness.md documents it. The whole existing suite still passes against the unchanged records.
- base/tooltip.go — TooltipPositioner switched from PositionerSide (Base's own positioner) to the core's `gpui.Anchored().AnchorBounds(cell)` with `Side/Align/TryFallbacks/Margin` (:276-320, the caller at :263); the fallback tactic is picked per side (flip-block on vertical, flip-inline on horizontal) at :302 and :322-345. base/tooltip_test.go adds two position tests pinning the migrated behaviour (:138-215).

**Fixtures (Chrome 155.0.8059.85, recorded headless; every Tier A box exact and every Tier C statistic 0.0 mean / 0.0 hot — the fixtures are text-free solid boxes at integral logical coordinates):**

- anchor-sides — one anchor (240,180,160x40) with four 60x40 popups, 8px away: top/align-end (340,132), bottom/align-start (240,228), left/align-center (172,180), right/align-center (408,180). Chrome's anchor() insets produce exactly these boxes.
- anchor-flip — anchor (40,360,120x40) whose bottom edge is the window's: the base placement (40,408) overflows, flip-block lands (40,292,100x60); the control anchor (200,100) keeps its side (200,148,100x60).
- anchor-shift — anchor (560,200,60x40): base (560,248) overflows right, shift clamps to (540,248,100x60); anchor (100,360,60x40) on a left-side popup: base y=360 overflows bottom, shift clamps to (12,350,80x50). Chrome's twin is min() over the anchor() inset and produces the same boxes.
- anchor-hidden — a 200x300 scroller at (20,40) over 800px content, the anchor 200px in; the input sequence clicks the anchor (focus) and seven ArrowDowns (280px). Only the scroller's box (20,40,200x300) is compared: the scrolled boxes and the popup's box are Chrome-side stale by design (a scroller containing an anchor defers its main-thread scroll commit — anchor_position_scroll_data.cc exists exactly to follow the scroll without relayout — so getBoundingClientRect lags the compositor while the port follows exactly every frame). The popup's hiding is proven by Tier C: the record's final pixels show no popup, and the always-visible wrong view fails at 1.471 mean image-wide / 1.830 in the scroller's ROI / 0.0195 hot. The 45-event transcript (click bubbling + 7 keydown/keyup chains with focus=anchor) matches exactly.

**Discrimination tests** (internal/parity/fixtures/anchor/anchor_test.go): the wrong-side view fails Tier A naming p-top/p-left/p-right; the no-fallback view fails Tier A naming p-flip; the always-visible view fails only Tier C while its boxes and transcript still pass. A gpui unit test pins the base-fits rule (fallbacks never tried when the base placement fits — the bug the tooltip migration caught).

**The Base component switched:** the tooltip (base/tooltip.go's TooltipPositioner — the only PositionerSide consumer), now a thin wrapper over the core Anchored; its existing 4 tests pass plus 2 new position tests (side placement, flip near the top edge, shift 4px from the right edge, each requested side). PositionerCorner and its popover/select/dialog users are untouched (the full migration is out of scope, as the ticket says).

**Commands and results:** `go vet ./gpui/ ./base/ ./internal/parity/... ./cmd/parityrecord/` clean; `gofmt -l` clean on touched files; `go build ./...` clean; `go test ./internal/parity/... ./base/ -count=1` pass (all 39 fixtures + tolerance headroom + discrimination); `go test ./gpui/ -count=1` passes except TestWin32TranslateKeyDownCarriesCodeAndLocation, which reads the live keyboard state (the machine's Shift is held by the user during the run — `win32TranslateKeyDown(BracketRight) = shift-}` — and the same suite passed earlier in the session; unrelated to this diff, which touches no input code).

**Limitations:** the cell anchor (AnchorBounds) carries no clipping rect, so HideWhenAnchorClipped does not apply to it (documented on the method; use AnchorTo when the clipped case matters); a hidden element still inserts hitboxes at prepaint (Chromium's invisible layer excludes hit testing — unobservable here, no fixture clicks a hidden popup); cached views that skip request layout fall back to the last completed frame's anchor bounds (one-frame lag, like Base's popup first-frame sync); the Shift fallback clamps the base placement, so a flip-and-shift combination (flip near one edge, shift near another) clamps the base side rather than the flipped one — the parity fixture exercises Shift alone, matching Chrome's min()/max() twin exactly.

