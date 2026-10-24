# 34 - position: sticky

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Sticky elements stick within their scroll container, applied at prepaint, with a hook for list headers.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures of sticky headers at several scroll offsets match Chrome's boxes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-25, worker (core/34-sticky): implemented, test-first (the unit expectations and the fixture pages were written from Chromium's constraint math before the prepaint pass ran; the Chrome records confirmed the predicted boxes). The offset applies at prepaint only — the scroll offset cell is the only input that changes, never the layout (the record zeroes the sticky insets; sticky maps onto in-flow Relative). One scoped paint-order rule: a sticky child paints (and inserts its hitbox) after its in-flow siblings (css2.1 Appendix E step 8 — a positioned element rides above in-flow content), without which the pinned header is covered by the content scrolling under it.

## Answer

**Implemented on `core/34-sticky` (headless only; recorded with Chrome 155.0.8059.85; 5 fixtures, all Tier A/C exact).**

- **API:** `PositionSticky` (style.go:376-380) + `Sticky()` (styled.go:283-309) + the Tailwind `sticky` class (regenerated; left class.go's unsupported list). The list-header hook: `StickyOffset(w, style, box, containingBlock)` + `WithStickyScrollContainer(w, style, bounds, maxOffset, offset, f)` (gpui/sticky.go:169, 146) — a custom scroller (the uniform-list shape) opens the scope and adds the offset to its headers' `PrepaintAt` origins.
- **Layout:** the record maps sticky to in-flow Relative with the insets zeroed (layout.go:282-294, 325-337 — constraints, not relative offsets); sticky joins the positioned set, so it IS a containing block for absolute children (containingblock.go:78-84).
- **Prepaint pass:** `gpui/sticky.go` — the scroller pushes a context (constraining = its content box, ComputeStickyConstrainingRect layout_box.cc:4312-4322; self-CB = the scrollable overflow for direct children, layout_box_model_object.cc:583-615); divs record flow boxes for sticky descendants; each sticky div's prepaint shifts its bounds and opens the shift as its children's element offset (div.go:1666-1690), re-applied at paint (div.go:1999-2020). The per-axis clamp math is sticky_position_scrolling_constraints.cc:65-144 (top/left over bottom/right; the end inset reduced when over-constrained, :668-680 of the constraints file's caller), in window coordinates (the scroll cancels — sticky.go's header proof). Nested sticky works for free: the element offset carries the ancestor's shift into both the inner box and its containing block.
- **Paint order:** sticky children paint after their in-flow siblings (div.go:1806-1810, 2027-2031 via sticky.go's stickyChildrenLast) — a pinned header rides over the content under it, and its hitbox lands on top (Chromium hit-tests in paint order).
- **Fixtures** (internal/parity/fixtures/sticky, records in testdata/): `sticky-top` (scroll 0: header natural, bottom-sticky pinned 8 above the port bottom — (20,276) 240x36), `sticky-top-scrolled` (3 arrows = 120: header pinned at the 12px inset line (20,32), bar child rides, footer pinned), `sticky-top-past` (End = 516: header pushed out past its section (20,-286), footer released to natural (20,244)), `sticky-left` (horizontal, 3 rights = 120: column pinned at (34,60)), `sticky-sections` (5 arrows = 200: header 1's bottom meets the port top (20,-4) exactly as header 2's top reaches it (20,20) — the replacement handoff). Tier A exact; Tier C 0.0 (headroom passes); wrong-view test: the relative view fails Tier A on exactly header-a/bar/footer.
- **Tests:** gpui/sticky_test.go (the offsets across scroll states, nearest-scroller, horizontal, CB==scroller, nested, margins, insets-not-offsets, the hook over a PrepaintAt list, enum/class), internal/layoutspec/position_sticky_test.go (the record translation), the fixtures + discrimination (internal/parity/fixtures/sticky/sticky_test.go).
- **Limitations:** sticky needs a scroll-container ancestor (no root/document scroller in the port); inline (paragraph-participating) sticky divs don't stick; OverflowHidden is not a sticky container (the port has no hidden-scroll machinery — ticket 36); the paint-order rule covers a sticky vs its in-flow SIBLINGS (a sticky above other subtrees, and sticky vs relative/absolute sibling order, stay tree order — the port has no z-index); Chromium floors the scroll and rounds the translation to whole device px, the port keeps float32 (integer-geometry fixtures agree); the key-dispatch order of a sticky child shifts with the paint order (Chromium dispatches DOM order).
