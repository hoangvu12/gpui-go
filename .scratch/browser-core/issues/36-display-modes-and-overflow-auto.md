# 36 - More display modes, overflow: auto and scrollbar gutter

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** display: contents, inline-block and inline-grid work; overflow: auto shows scrollbars only when needed; scrollbar-gutter: stable is available as an opt-in.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures for each mode match Chrome's boxes.
- [x] overflow: auto shows no scrollbar when content fits and one when it overflows.

## Comments

## Answer

- **API**: `Display` gained `DisplayContents`, `DisplayInlineBlock`, `DisplayInlineGrid` and `DisplayFlowRoot` (gpui/style.go) with the `Contents()` / `InlineBlock()` / `InlineGrid()` / `FlowRoot()` builders; `Overflow` gained `OverflowAuto` with the stateful `OverflowAuto()` / `OverflowXAuto()` / `OverflowYAuto()` div builders (gpui/scroll_element.go, also on the Interactive mixin); `ScrollbarGutter` (`ScrollbarGutterStable()` / `scrollbar-gutter-stable`) opts a scroll container into reserving its gutter while nothing shows. Classes: `contents`, `inline-block`, `inline-grid`, `flow-root`, `scrollbar-gutter-stable`; `overflow-auto` now sets the real `OverflowAuto` (it previously pinned to `OverflowScroll` — a recorded divergence toward Chrome, noted in class_test.go).
- **Record translation** (gpui/layout.go displayToRecord/overflowToRecord): InlineBlock→Block and InlineGrid→Grid like the pin's InlineFlex fold, FlowRoot→the rev-15 tag, Contents→Block fallback (a contents element requests no node), OverflowAuto→Taffy Hidden (contain, zero auto-min, no reservation) — the div's request-time copy (gpui/scrollbar.go autoOverflowRecordStyle) maps a shown or gutter-stable axis onto Scroll WITH the scrollbar width so Taffy reserves the gutter (taffy compute/flexbox.rs:459-470, the CSS classic-scrollbar arithmetic).
- **display: contents** (gpui/contents.go): the div requests no node; its children splice into the enclosing node request through a frame-scoped continuation register — `requestLayoutNode` expands it for every attaching container (gpui/containingblock.go), and the div also expands before the inline collector and the published inline content (gpui/div.go requestLayout). Prepaint/paint walk the children under the parent's scopes with no box, no hitbox, no scroll, no sticky (prepaintContents/paintContents, gpui/div.go); key listeners keep their dispatch node. Chromium: no LayoutObject for contents (layout_object.cc:430-432) and the builder splices the children (layout_tree_builder_traversal.cc:604-634).
- **inline-block / inline-grid**: atomic inline-level boxes in the paragraph collector (gpui/inline.go isAtomicInlineDisplay) — the pin's InlineFlex pattern with block/grid contents (Chromium: kInlineBlock→LayoutBlockFlow, layout_object.cc:437-442; kInlineGrid→LayoutGrid, :470-472).
- **overflow: auto** (gpui/scrollbar.go): the prepaint decides per axis from the clamp's max offset (Chromium's ComputeScrollbarExistence kAuto arm, paint_layer_scrollable_area.cc:1852-1861) and notifies a redraw on a change, so the reservation settles one frame later — Chromium's post-layout relayout (:1480-1500) becomes a frame-end dirty marking (gpui/render.go completeFrame). The scrollbar paints after the children inside the overflow mask (gpui/div.go paintInFlow): a #fcfcfc track over the scrollbar rect (RectForVerticalScrollbar, :1625-1650), #8b8b8b arrow glyphs in the WxW buttons and a rounded thumb in the slot between them — the Windows Chrome headless measurements (15px `ClassicScrollbarWidth`). `WindowAutoScrollbarFacts` is the test observable. The wheel listener and the keyboard-scroll default action treat an auto axis like a scroll one; the clamp keeps a fitting scroller's range zero.
- **scrollbar-gutter: stable** (gpui/scrollbar.go autoAxisReserves): reserves the gutter at request time even while no scrollbar shows (LayoutBox::HasScrollbarGutters/ComputeScrollbarsInternal, layout_box.cc:1477-1560); nothing paints in the empty gutter.
- **Sticky under hidden/auto overflow** (gpui/sticky.go, gpui/div.go prepaintInFlow): overflow hidden and auto are sticky scroll containers per CSS (ComputedStyle::IsScrollContainer, computed_style.h:1988-1993; PaintLayer::ContainingScrollContainerLayer, paint_layer.cc:275-292), closing ticket 34's recorded limitation — the div pushes the sticky scroller scope for every scroll container (a hidden one contributes a zero offset and its content extent), and the constraint axes follow ScrollableAxes (paint_layer_scrollable_area.cc:2520-2542).
- **Fixtures**: `internal/parity/fixtures/display` — `display-modes` (the splice, inert box styles, nested contents, an empty contents, the atomic inline boxes and the inline-grid columns) and `overflow-auto` (fits / gutter-stable / overflows vertically / overflows horizontally), both recorded in headless Chrome 155, registered in replay_test.go and cmd/parityrecord; one deliberately-wrong view (contents as the pin's flex) fails Tier A (display_test.go). Unit tests: gpui/display_overflow_test.go. The TSX class types were regenerated after the merge (gpui/script/types/gpui-script.d.ts).
- **Limitations**: a contents element at a window root (or under a container that never requests a node over its children) lays out only its first child; the reservation settles one frame after content changes; `scrollbar-width: none` is not expressible for auto (ScrollbarWidth 0 reads as unset → the classic 15px); `overflow: scroll` still draws no scrollbar (the pin's behaviour — painting for it would change existing pinned expectations, e.g. the sticky fixtures' `scrollbar-width: none`); the scrollbar look is this machine's headless-Chrome measurement, not a themeable API; the sticky constraining rect under a shown auto scrollbar is not contracted by the scrollbar width (Chromium's OverflowClipRect contracts by it); the CSS rule that a `visible` axis computes to auto when the other axis clips is not implemented (the fixtures pin both axes).
