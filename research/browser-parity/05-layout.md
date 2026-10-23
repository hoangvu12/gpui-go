# 05 · Layout: how the browser behaves, and how gpui-go differs

Ticket: `.scratch/browser-parity/issues/05-layout.md`. Researched 2026-10-23. Read only, nothing built. This is the lowest-priority area, so the report is short.

Sources and pins:
- **Chromium** `0d4dcd4f` (sparse checkout, `%TEMP%\research-src\chromium`). Paths are relative to `third_party/blink/renderer/`.
- **Taffy 0.13.0**, from the cargo registry (`~/.cargo/registry/src/index.crates.io-*/taffy-0.13.0/src`). This is the version GPUI-CE locks.
- **gpui-go**, this worktree (`research/browser-parity` @ `8f5ad4d`): `gpui/`, `internal/native/`, `reference/native/src/layout.rs`.
- **Specs**, linked by section. I did not fetch the spec text separately. Where Chromium or Taffy quotes the spec in a source comment, I say so.

Markers: **[V]** means I read it in source. **[I]** means it's inferred and not traced end to end. Sizes: S ≤ 2 days, M ≤ 1 week, L ≤ 3 weeks, XL more than that.

---

## Ranked gaps

| # | Gap | Who notices | Work | Size |
|---|-----|-------------|------|------|
| 1 | Text reports its unwrapped width as its min-content width, so flex items holding text won't shrink and wrap | Every sidebar, toolbar or card row with a long label | Go | S–M |
| 2 | The scroll range covers only the direct children's border boxes. Descendant overflow and trailing margins are ignored, and Taffy's `content_size` is computed but thrown away | Scroll views whose content is nested or has a bottom margin | Go | S |
| 3 | No `position: static`, so an absolutely positioned element always uses its direct parent as its containing block | Ported HTML/Tailwind with `absolute` under a non-`relative` wrapper | Go + native | M |
| 4 | No `sticky` | Sticky table headers and section headers in lists | Go (prepaint) | M |
| 5 | No `fixed` (Deferred and Anchored partly cover it) | Toasts, FABs, full-window overlays authored as CSS | Go | S–M |
| 6 | Baseline alignment ignores text baselines: measured leaves report none, so Taffy aligns their bottom edges | `items-baseline` rows that mix font sizes | native | M |
| 7 | The grid surface is only `repeat(N, minmax(x, 1fr))`. No explicit track lists, auto rows, areas, `justify-items` or `justify-self` | App shells such as `grid-cols-[240px_1fr]` | Go + native ABI | M |
| 8 | Display modes: the default is `flex` (a browser `div` is `block`). `contents`, `flow-root`, `inline-block` and `inline-grid` are missing | Ported markup; wrapper components that need `contents` | Go (+ native for flow-root) | S–M each |
| 9 | Device-pixel rounding happens before layout, and borders round where Blink floors | Hairlines (`w-[0.5px]` vanishes at 1×); 1px borders at 175% become 2 device px | Go | S |
| 10 | No `overflow: auto` and no scrollbar gutter, so scrollbars always overlay the content | Content hidden under a scrollbar; layouts ported from Chrome on Windows, where scrollbars take space | Go + native | S–M |

Where gpui-go already matches the browser: the `min-width: auto` algorithm itself, `overflow: hidden` versus `clip`, percentage padding and margins, the absolute-position inset basis, `aspect-ratio`, `gap`, and block margin collapsing (Taffy's block algorithm). See [the end of this report](#where-gpui-go-already-matches).

#1 is the one users hit most. It is also the reason GPUI apps sprinkle `min_w_0` and `overflow_hidden` everywhere. #2 is a few lines of Go. #3 to #5 are positioning work that should be designed together.

---

## 1. Text min-content equals its max-content

**Spec.** [css-sizing-3 §min-content](https://drafts.csswg.org/css-sizing-3/#min-content): the min-content inline size is the width the content takes when it wraps at every soft wrap opportunity, which for text is roughly the longest word. [css-flexbox-1 §4.5](https://drafts.csswg.org/css-flexbox-1/#min-size-auto): a flex item's automatic minimum is its content size suggestion, which is that min-content size.

**Chromium.** `core/layout/inline/inline_node.cc:2308-2342`: `ComputeMinMaxSizes` computes the min size by line-breaking at "0 available inline size", in its own words "the width where lines wrap at every break opportunity" (`LineBreakerMode::kMinContent`). Max-content is a separate pass. [V]

**gpui-go.** The text measure functions wrap only at a known width or a definite available width. Min-content and max-content queries both shape unwrapped: `gpui/div.go:574-586` ("min/max-content content-based sizing measures unwrapped"), and the same at `gpui/styled_text.go:280-290`, `gpui/inline.go:694-704` and `gpui/text_truncate.go:61`. [V] Taffy asks for exactly this query when it computes the automatic minimum: `Size::MIN_CONTENT` at `compute/flexbox.rs:833-846`. [V]

**Gap.** A flex item containing text gets the full single-line width as its automatic minimum, so in a `flex-row` it won't shrink and its text won't wrap. It overflows instead. The browser shrinks it to the longest word and wraps. Grid `minmax(min-content, …)` tracks are too wide for the same reason. [I]

There is also a related risk [I]. The cached-layout shortcut returns the cached size for any unconstrained request (`div.go:592-594`: `!hasWrap || …`). So a max-content query that arrives after a definite-width measurement gets the *wrapped* size. Whether Taffy ever asks in that order within one frame is untraced.

**Fix.** Go only. When `AvailWidth` is `AvailMinContent`, shape with a zero wrap width (or take the widest unbreakable segment), and key the cache by mode. Size S–M. The cost is one extra shape per text item per layout, so cache it per frame. This changes existing layouts: rows that relied on text holding them open will start wrapping. That needs a conformance-fixture update, because the pinned GPUI behaves the same way.

## 2. The scrollable-overflow region

**Spec.** [css-overflow-3 §2.2 scrollable overflow](https://drafts.csswg.org/css-overflow-3/#scrollable): the region includes the descendants' scrollable overflow (unless they clip it), the end-side padding, and the margin boxes of in-flow children for flex and grid.

**Chromium.** `core/layout/scrollable_overflow_calculator.cc`. It starts from the padding rect (`:113`). Its result unites the in-flow bounds expanded by the padding (`:116-137`). `ScrollableOverflowForPropagation` (`:262-320`) folds each child's own scrollable overflow in, clipped per axis only where the child clips. [V] Whether Blink adds margins for block children is [I].

**gpui-go.** `DivElement.scrollContentSize` (`gpui/scroll_element.go:338-363`) takes the union of the direct children's layout bounds, which are border boxes, measured from their minimum origin. `clamp` (`:96-131`) adds padding. [V] Taffy's `content_size` already crosses the ABI (`reference/native/src/layout.rs:1462-1463`, `internal/native/layout.go:458` `ContentW`), but nothing in `gpui/` reads it. [V] `clamp` also resolves percentage padding against the scroller's *own* height for top and bottom (`:110-113`). CSS, and Taffy (`compute/flexbox.rs:178`), resolve all padding percentages against the containing block's *width*. [V]

**Gap.** Overflow from descendants doesn't extend the scroll range: for example, a fixed-height child with taller content, or an absolutely positioned grandchild. A last child's bottom margin is cut off. [I] Percentage vertical padding on a scroller is resolved inconsistently with how layout resolved it.

**Fix.** Go: use Taffy's `content_size` (it already includes visible descendant overflow, `compute/common/content_size.rs:16-20` [V]) plus the padding. Resolve the padding the way Taffy did, or read the resolved padding from the layout record. S.

## 3. The absolute containing block (no `static`)

**Spec.** [css-position-3 §2.1](https://drafts.csswg.org/css-position-3/#def-cb): an absolutely positioned box's containing block is formed by the nearest *positioned* ancestor. Elements are `static` by default.

**Chromium.** `core/layout/layout_object.cc:2072` `ComputeIsAbsoluteContainer` decides containership from the position value plus transforms, filters and containment. [V]

**Taffy and gpui-go.** Taffy's `Position` has only `Relative` (the default) and `Absolute` (`style/mod.rs:291-302`). [V] Absolute children are laid out by their *direct parent* (`compute/flexbox.rs:2231-2239`, with the inset basis being the parent's padding box). [V] gpui-go mirrors this (`gpui/style.go:350-358`) and rejects the `static` class with "elements are relative unless absolute" (`gpui/class.go:1112`). [V]

**Gap.** Tailwind and HTML code relies on `absolute` escaping intermediate static wrappers up to the nearest `relative`. In gpui-go it always pins to the immediate parent.

**Fix.** Add `PositionStatic` and keep it the Go default only if the team accepts the behaviour change. Otherwise make it opt-in. Implementation: lay the absolute subtree out against the right ancestor, either by reparenting the node in the Taffy tree (Go), or with a custom absolute pass in `layout.rs` (native). M.

## 4. Sticky positioning

**Spec.** [css-position-3 §3.4 sticky](https://drafts.csswg.org/css-position-3/#stickypos-insets): the box is laid out in flow, then shifted by its scroll container's offset within its insets, and clamped to its containing block.

**Chromium.** This is not a layout algorithm. Constraints are computed after layout (`core/layout/layout_box_model_object.cc:664-730`, `SetStickyConstraints`) and applied as a paint and scroll offset (`core/paint/paint_property_tree_builder.cc:916-932`, which rounds the sticky offset). [V]

**gpui-go.** There is none; the `sticky` class is rejected (`gpui/class.go:1111`). [V] Taffy has no sticky either. [V]

**Fix.** Go only, at prepaint: after layout, for each sticky element, compute the offset from the nearest scroll handle's offset and viewport and the parent's bounds, and add it to the element offset. Hit-testing follows automatically, since it uses prepaint bounds. [I] M. Lists (`gpui/list.go`, `listelement.go`) need their own sticky-header hook.

## 5. Fixed positioning

**Spec.** [css-position-3 §2.1](https://drafts.csswg.org/css-position-3/#fixed-cb): the containing block is the viewport, unless an ancestor has a transform, filter or containment.

**Chromium.** `core/layout/layout_object.cc:2009-2030` `ComputeIsFixedContainer`: filter, backdrop-filter and others make an ancestor capture fixed descendants. [V]

**gpui-go.** The `fixed` class is rejected, and users are told to use `Deferred` or `Anchored` (`gpui/class.go:1110`; `gpui/deferred.go:274`, `gpui/anchored.go:91`). [V]

**Fix.** Go: `PositionFixed` lays out as absolute against the window root and paints deferred. Note that gpui-go supports backdrop blur, so it should capture fixed descendants like Blink does, or document that it doesn't. S–M. This shares a design with #3 and z-ordering, which is covered in report 02.

## 6. Baseline alignment of text

**Spec.** [css-flexbox-1 §8.3](https://drafts.csswg.org/css-flexbox-1/#baseline-participation): baseline alignment uses each item's first baseline, which for text is its first line's baseline.

**Chromium.** Every inline formatting context reports its first-line baseline to flex and grid. [I] (The obvious design; I didn't trace the file.)

**Taffy and gpui-go.** A measured leaf returns `first_baselines: Point::NONE` (`compute/leaf.rs:102,157`). Flex then falls back to the item's height (`compute/flexbox.rs:2037,2047`: `unwrap_or(size.height)`). [V] gpui-go's `MeasureFunc` returns only a `Size` (`gpui/layout_context.go:196`). [V]

**Gap.** `items-baseline` aligns the bottom edges of text boxes, not their baselines. With mixed font sizes or multi-line text, the result is visibly wrong.

**Fix.** Native: let the measure response carry a baseline, and implement Taffy's tree trait in `layout.rs` so measured leaves report it, instead of using `compute_layout_with_measure`. Also extend the ABI record and Go's `MeasureFunc`. M.

## 7. The grid surface

**Spec.** [css-grid-2](https://drafts.csswg.org/css-grid-2/): explicit track lists, `grid-auto-rows/columns`, `grid-auto-flow`, named areas and lines, and `justify-items` / `justify-self`.

**gpui-go.** `GridTemplate` is only `repeat(Repeat, minmax(MinSize, 1fr))` (`gpui/style.go:659-665`; the native side builds exactly that, per the comment at `gpui/layout.go:229-232`). [V] `justify-items`, `justify-self` and the `place-*` utilities are rejected (`gpui/class.go:1126`). [V] `order` is rejected (`:1124`). [V] Taffy 0.13 implements the grid properties, with the `grid` feature on by default [V, per research/15 §1]. I didn't check whether it has `order`.

**Gap.** Common app shells such as `grid-cols-[240px_1fr]`, `auto-rows-min` or `grid-flow-dense` can't be expressed.

**Fix.** Go style fields plus a variable-length track list in the native ABI record. M. Taffy does the algorithm work.

## 8. Display modes

**Spec.** [css-display-3](https://drafts.csswg.org/css-display-3/). HTML's UA sheet makes `div` `display: block` (Chromium `core/html/resources/html.css:74-75` [V]).

**gpui-go.** The display modes are `Block`, `Flex` (the default), `Grid`, `Inline`, `InlineFlex` and `None` (`gpui/style.go:290-307`, `:929`). [V] Inline folds to Taffy `Block` and InlineFlex to `Flex`, and inline flow is GPUI-owned (`gpui/layout.go:256-269`, `gpui/inline.go`). [V] `contents`, `flow-root`, `inline-block`, `inline-grid`, `table` and `list-item` are rejected (`gpui/class.go:1113`). [V] Taffy 0.13 has `Block`, `FlowRoot`, `Flex`, `Grid` and `None`, but no `Contents` (`style/mod.rs:188-203`). [V] The native ABI maps only Flex, Block, Grid and None (`reference/native/src/layout.rs:492-495`). [V]

**Gaps and fixes.**
- **Default `flex` (row).** A plain `Div` with two children puts them side by side, where a browser stacks them. Changing the default would break every existing view, so keep it and document it. [I] The newer GPUI-CE checkout in the cargo git cache (`beb5a6d`, `crates/gpui/src/style.rs:971`) defaults to `Display::Block`. gpui-go's pinned port says the reference default is Flex (`gpui/style.go:919-929`). This is worth a check against the pin `254b5dbd`, because upstream may have changed it.
- **`contents`** is the useful missing one for a component-less core: wrapper elements that add behaviour without a box. Go: splice the children into the parent's child list at request-layout time, while keeping the element for events and accessibility. S–M.
- **`flow-root`** is a one-line native mapping, because Taffy has it. S. **`inline-block` and `inline-grid`**: gpui-go already has InlineFlex as an atomic inline. Doing the same with Block or Grid inside is S each [I].

## 9. Rounding to device pixels

**Spec.** No normative rule. Browsers lay out in fractional units and snap at paint.

**Chromium.** `LayoutUnit` is fixed point with 6 fractional bits, so 1/64 px (`platform/geometry/layout_unit.h:473`). [V] `Round()` rounds half up (`:294-297`). [V] Paint snapping uses `SnapSizeToPixel` (`:805-813`): it rounds the far edge with the location's fraction, and forces at least 1px when the size is above 4/64 px. [V] Border widths are *floored* in zoomed pixels, and any width strictly between 0 and 1 becomes 1 (`core/css/resolver/style_builder_converter.cc:1970-1977` `ClampLineWidth`). [V]

**gpui-go.** Every absolute length is rounded to whole device pixels *before* layout, with ties toward zero (`gpui/layout.go:149-153`, `:48-53`). Borders use the round-with-min-1 stroke rule (`:79-92`). After layout, the near and far edges round independently (`:831-842`). Measured text sizes are ceiled (`:112-114`). Taffy's own rounding is off. [V] All of this is ported bit for bit from the GPUI pin.

**Gap.** Small, and visible only in edge cases:
- `w-[0.5px]` or `h-[0.5px]` at 1× rounds to 0, so the element vanishes. Blink keeps it at 1 device px.
- A 1px border at 175% is 1.75 device px. gpui-go rounds that to 2 and Blink floors it to 1, so borders look heavier at 175% [V by arithmetic].
- The edge-rounding scheme itself matches Blink's in spirit (independent edges, so adjacent boxes don't gap or overlap).

**Fix.** Go: floor border widths (keep the minimum of 1), and give sizes in (0, 1) device px a minimum of 1 device px. S. Both diverge from the GPUI pin, so the conformance fixtures need updating.

## 10. `overflow: auto` and scrollbar space

**Spec.** [css-overflow-3 §3](https://drafts.csswg.org/css-overflow-3/#overflow-properties) and [§scrollbar-gutter](https://drafts.csswg.org/css-overflow-3/#scrollbar-gutter-property): `auto` shows scrollbars only when the content overflows. Classic scrollbars take layout space; overlay scrollbars don't.

**gpui-go.** The overflow values are `Visible`, `Clip`, `Hidden` and `Scroll` (`gpui/style.go:384-396`). There is no `Auto`. `ScrollbarWidth` defaults to 0 (`:933`), so Taffy reserves no gutter, and every scrollbar overlays the content. The `scroll-*`, `snap-*` and `overscroll-*` utilities are rejected (`gpui/class.go:1134`). [V]

**Gap.** Overlay-only scrollbars are a defensible desktop choice. Parity would mean an opt-in `scrollbar-gutter: stable`, which is easy because Taffy's `scrollbar_width` is already forwarded. Scroll snapping is a separate feature, and belongs with the input and scroll work in report 03. S–M.

---

## Where gpui-go already matches

- **`min-width: auto` on flex items.** Taffy computes min(specified size suggestion, content size suggestion), clamped by the max size and by padding plus border, and zero for scroll containers (`compute/flexbox.rs:830-853`, citing css-flexbox-1 §4.5). Blink does the same (`core/layout/flex/flex_layout_algorithm.cc:1061-1166`). The visible difference comes from the min-content values fed into it (#1), not from the algorithm. [V]
- **`overflow: hidden` versus `clip`.** In Taffy, `Hidden` and `Scroll` are scroll containers (automatic minimum 0) and `Clip` is not (`style/mod.rs:375-390`). Blink keys on the "scrollable" overflow values the same way (`core/style/computed_style.h:1990-1993`; `flex_layout_algorithm.cc:423-436`). gpui-go's `overflow-hidden` and `overflow-clip` classes map to these (`gpui/class_generated.go:497`, `gpui/class.go:611-620`). [V]
- **Percentages.** Taffy resolves padding and margin against the parent's width (`compute/flexbox.rs:178,446-447`; block `compute/block.rs:348`), and the absolute insets against the padding box (`flexbox.rs:2238`), as CSS does. gpui-go forwards fractions as percentages and doesn't pre-resolve them (`gpui/layout.go:154-155`). [V] (Exception: the scroll clamp, see #2.)
- **`box-sizing`.** It is always border-box (`class.go:1127`; Taffy's default). That matches Tailwind's preflight, which every Tailwind-authored app assumes. [V]
- **Block margin collapsing** comes from Taffy's block algorithm (`compute/block.rs:499-671`). [V] I didn't diff edge cases against Blink.
- **`aspect-ratio`, `gap`, `flex-wrap`, `grow/shrink/basis`** are forwarded field for field (`gpui/layout.go:298-341`). [V]
