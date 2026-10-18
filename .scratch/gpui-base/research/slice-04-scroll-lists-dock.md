# Slice 04: scroll, lists, dock (GPUI Base port inventory)

Upstream: `upstream-base/ce/crates/base/src` @ c0820693 (abbreviated `B/`). Go core: `gpui-go/gpui/` (abbreviated `G/`).
Rust core pin: `gpui-go/reference/ce-source/crates/gpui/src` (abbreviated `R/`).
An earlier pass, `research/C-scroll-lists-dock.md`, covered this slice. I re-checked every gap in it against the current Go tree (91 files, 59,053 lines). **Every one of its MISSING-GO findings still holds.** None was closed by `implement-core-remaining`.

Class key:
- **PURE-GO**: can be built today on exported Go core API.
- **CORE-ADD**: needs a Go-side addition to `gpui/`. Ids G1..G13 are defined in the consolidated list below.
- **NATIVE**: needs a change to the frozen DLL.

## Summary table

| Component | Lines | Tests | Class | Missing core APIs | Base deps | 3rd-party |
|---|---|---|---|---|---|---|
| `auto_scroll.rs` | 155 | 2 (pure) | PURE-GO | none. `Context.Spawn` (entity-bound) is a convenience only (G12) | none | none. `Arc<Mutex>` is stdlib |
| `virtual_list.rs` | 905 | 7 (3 window) | CORE-ADD | G1, G2, G4, G9 (`DeferredScrollToItem`, `Axis`), G6 (border/bg via interactivity) | `AxisExt`, `InteractiveElementExt::lock_scroll_axis`, `ScrollbarHandle` | `smallvec` |
| `scrollbar.rs` | 2597 | 35 (13 window) | CORE-ADD | G2, G5, G6 (`paint_quad`, `paint_layer`), G7, G8, G9 (`Axis`, `Anchor`/`from_anchor_and_size`, `contains`, `dragging`), G12 (`use_state`), G13 (thumb drag) | `theme` (`theme.scrollbar.*`, radius token), `animation` (`ease_in_cubic`/`ease_out_cubic`), `AxisExt` | `serde` + `schemars` (`ScrollbarMode` settings), `web_time::Instant` |
| `scroll_bounce.rs` | 1058 | 23 (13 window) | CORE-ADD | G4 (`OngoingScroll`), G5. Tests need G1 + G3 | `ScrollbarHandle`, `OngoingScrollExt` | `web_time::Instant` |
| `scrollable_mask.rs` | 910 | 16 (16 window) | CORE-ADD | G1/G2 (`horizontal_scroll_area` = `div().overflow_hidden().track_scroll`), G4, G5, G9 (line height, `pixel_delta`, `precise`, composite `ElementId`), G6 (debug border only) | `AxisExt`, `OngoingScrollExt`, `ScrollbarHandle`, `StyledExt::refine_style` | none |
| `dock/layout/*` (builder, edit, node, normalize, tree, mod) | 1896 | 30 (4 window) | PURE-GO | none. Needs `Axis` (G9) or a local enum | `geometry::Placement`, `dock::panel` | `smallvec` (`NodePath`) |
| `dock/active.rs` | 222 | 7 | PURE-GO | none | `layout::PanelId` | none |
| `dock/state.rs` | 267 | 3 | PURE-GO | none | none | `serde`, `serde_json::Value` |
| `dock/state_convert.rs` | 587 | 15 | PURE-GO | none | layout, state | `serde_json` (tests, `Value::Null`) |
| `dock/dock_placement.rs` | 263 | 7 | PURE-GO | none | `resizable::PANEL_MIN_SIZE`, state | none |
| `dock/drag.rs` | 380 | 4 | PURE-GO (types and geometry) | none (`DragPanel` is a plain `Render` used as a drag value) | `Placement`, layout | none |
| `dock/panel.rs` | 310 | 2 (window) | PURE-GO with a shim | `Focusable` trait absent in Go (define in Base); `AnyView::downcast` (R/view.rs:53) absent → keep a typed handle | layout, state, state_convert, tab_group | `serde_json` (tests) |
| `dock/registry.rs` | 119 | 0 | PURE-GO | none (`App.SetGlobal`/`Global[T]`) | dock_area, state, panel | none |
| `dock/tab_group.rs` | 1815 | 27 (27 window) | CORE-ADD | G10 (`on_drag`, `on_drag_move`/`DragMoveEvent`), G11 (preview), G13. G12 convenience: `spawn_in` | active, drag, layout, panel, `Placement`, `TestSupportExt` | none |
| `dock/dock_area.rs` | 4148 | 57 (54 window) | CORE-ADD (transitive) | G12 `canvas` (via `ElementExt::on_prepaint`), `subscribe_in`. Through `resizable`: G10 + G7 | `resizable` (`h_/v_resizable`, `resizable_panel`, `ResizableState`, `ResizablePanelEvent`, `ResizeHandleContext`, `PANEL_MIN_SIZE`), `ElementExt`, `Placement`, all dock modules | `anyhow`, `serde_json` (fixtures) |
| `dock/mod.rs` | 202 | 1 | PURE-GO | none | re-exports | none |
| `dock/test_support.rs` | 196 | 0 | PURE-GO (test-only) | Go has no `VisualTestContext` simulate helpers; use `Window.DispatchInput` (G/filedrop.go:379) | panel, tab_group | none |
| `dock/fixtures/*.json` | 6 files | — | data | — | — | — |

Totals: scroll and lists 5,625 lines with 83 tests; dock 10,920 lines with 153 tests.

## Per-component notes (core usage → Go status)

### auto_scroll.rs (B/auto_scroll.rs)

- **API:** `AutoScroll{delta, compute_delta, set, is_active, stop, last_drag_position}`. Its only consumer is `input/base/element.rs:465,564`.
- **Core:** `cx.spawn(async |weak, cx| loop { timer(16ms); weak.update })` (:79, :91-108).
- **Go equivalents:** `App.Spawn` (G/executor.go:742), `TaskRun.Sleep` (G/executor.go:625), `AsyncApp.Await` (G/executor.go:723), `WeakEntity.TryUpdate` (G/entity.go:597).
- **Hazard:** Rust cancels by dropping the `Task`. Go must call `Task.Cancel` (G/executor.go:535) in both `stop` and `set(None)`.

### virtual_list.rs

- **API:** `VirtualListScrollHandle` (`From<ScrollHandle>`, `Deref`, `scroll_to_item`, `scroll_to_bottom`, `impl ScrollbarHandle` :61); `v_virtual_list`/`h_virtual_list` (:139/:160); `VirtualList` (`Styled`, `track_scroll` :236, `with_sizing_behavior`); `VirtualListFrameState`; `ItemSizeLayout`.

| Core use (B/virtual_list.rs) | Go status |
|---|---|
| `layout_as_root` at request_layout (:335) and at prepaint (:729); `prepaint_at` (:730) | PRESENT: `AnyElement.LayoutAsRoot` (G/element.go:654), `PrepainAt` [sic] (G/element.go:663) |
| `with_element_state` (:466), `request_measured_layout` (:530), `with_content_mask` (:705) | PRESENT: G/element.go:1379, 1536, 1251 |
| `self.base.interactivity().request_layout/prepaint/paint` on a `Stateful<Div>` set to `overflow_scroll().lock_scroll_axis().track_scroll()` (:202-204, 460, 660, 751) | **MISSING**. Go Div has no scroll: no `TrackScroll`, no wheel-driven offset, and only the `overflowMask` clip (G/div.go:1347). `Interactivity` is not exported. → G1, G2, G4 |
| `DeferredScrollToItem` (:114) | MISSING (R/elements/uniform_list.rs:103). Trivial; can be defined in Base |
| `ListSizingBehavior`, `ScrollStrategy` | PRESENT: G/list.go:27, G/scroll.go:15 |

- **Hazard:** Go `drawable.paint` re-resolves bounds at paint time. Items must be repainted under the same absolute offset that prepaint used (`WithAbsoluteElementOffsetVoid`, G/element.go:1268), following the UniformList precedent (G/list.go:322-331).

### scrollbar.rs

- **API:**
  - `ScrollbarMode` (serde + JsonSchema, :47).
  - `trait ScrollbarHandle` (:68), with impls for `ScrollHandle` :83, `UniformListScrollHandle` :101 and `ListState` :120.
  - Style and motion types: `ScrollbarEntrance`, `ScrollbarMotion`, the style structs.
  - `Scrollbar` builder: `new`/`horizontal`/`vertical`/`axis`/`mode`/`max_fps`/`width`.

| Core use (B/scrollbar.rs) | Go status |
|---|---|
| `insert_hitbox` under a content mask (:1292, :1535) | PRESENT: G/mouseevent.go:477, G/element.go:1251 |
| `window.use_state` (:1297) | MISSING (R/window.rs:4122). Workaround: `WithElementState` holding a pointer (G12) |
| `request_animation_frame` (:1346, :1477), `current_view` (:1355), `reduce_motion` (:1307) | PRESENT: G/imagecache.go:958, G/view.go:325, G/app.go:282 |
| `spawn` + `background_executor().timer` (:540, :1358) | PRESENT, using the same pattern as auto_scroll |
| `cx.notify(EntityId)` (:531, :550, :1361, plus the mouse listeners) | **MISSING**. `applyNotify` is unexported (G/app.go:521). Rust: R/app.rs:2920 → G5 |
| `window.paint_layer` (:1611), `paint_quad(fill(..).corner_radii)` (:1612+) | **MISSING for element authors**. `Scene.PaintQuad` (G/scene.go:521), `Scene.PushLayer` (G/scene.go:346) and `Fill` (G/paint.go:256) exist, but the frame scene and paint context are reachable only through the unexported `currentFrame(w)`. Rust: R/window.rs:4339, 4634 → G6 |
| `set_cursor_style(style, &hitbox)` (:1609) | **MISSING**. Only the global `App.SetCursorStyle` (G/app.go:993) exists. Rust: R/window.rs:3844 → G7 |
| `on_mouse_event` for MouseDown/MouseMove/MouseUp/ScrollWheel (:1642-1787), `stop_propagation` | PRESENT: G/mouseevent.go:459, G/key_dispatch.go:1178 |
| `MouseMoveEvent::dragging()` | MISSING (R/interactive.rs:515). Trivial → G9 |
| `ScrollHandle.bounds()`/`max_offset()` (:85, :97) | Getters exist (G/scroll.go:74, :87), but no element ever writes them. The fields are unexported (G/scroll.go:33) → G2 |
| `UniformListScrollHandle.base_handle.bounds()`/`max_offset()` (:103, :116) | MISSING. The Go handle has only `Offset`, `SetOffset`, `LastItemSize` and `LastContentSize` (G/list.go:69-87) → G2 |
| `ListState::{scroll_px_offset_for_scrollbar, set_offset_from_scrollbar, scrollbar_drag_started/ended}` | MISSING (R/elements/list.rs:743-781). `ViewportBounds` and `MaxOffsetForScrollbar` are PRESENT (G/listelement.go:337, :348); the rest is recorded as omitted at G/listelement.go:20-36 → G8 |
| `Axis`, `Anchor`, `Bounds::from_anchor_and_size`, `Bounds::contains` | MISSING. There is no `type Axis` in G/, and `Bounds` has only Left/Top/Right/Bottom (G/paint.go:616-619). Rust: R/geometry.rs:26, 868, 1465 → G9 |

- **Hazard:** upstream uses wall-clock `Instant::now()` (:171, :548, :1301). The Go port should use `App.Now()` (G/imagecache.go:852) so `TestApp.AdvanceClock` (G/testapp.go:54) can drive the tests.

### scroll_bounce.rs

- **API:** `ScrollBounceMotion` (:21), `ScrollBounce::new(id, &H, child)` (:95) with `enabled`/`motion`/`on_scroll`, and `ScrollBouncePrepaintState`.
- **Core:** child delegation; `with_element_state` (:226); `insert_hitbox` (:249); `with_content_mask` plus `with_element_offset` (:250-251); capture/bubble `on_mouse_event(ScrollWheelEvent)` with `TouchPhase` (:276); `background_executor().now()` (:317); `notify(view)` (:401); `OngoingScroll` (:145).
- **Go status:** all PRESENT except two:
  - `OngoingScroll` (R/gestures.rs:49-110): about 80 pure lines → G4.
  - Notify by id → G5.
- **Test dependencies:** the tests drive `div().overflow_y_scroll().track_scroll()` (:520) and a wheel-scrolling `gpui::list`. Both are missing in Go → G1, G3.
- **Hazard:** the residual math assumes two Rust behaviours:
  - The div applies the wheel delta unclamped and clamps at prepaint (R/elements/div.rs:3816 `paint_scroll_listener`).
  - The list clamps eagerly (R/elements/list.rs:1626).

  G1 and G3 must reproduce both exactly.

### scrollable_mask.rs

- **API:** `ScrollableMask<H>::new(axis, &H)` (:76) with `id` and `debug`, plus the crate-level `horizontal_scroll_area` (:41-53, used by table).
- **Core:**
  - `insert_hitbox` (:175).
  - `window.line_height()` (:189); R/window.rs:2983.
  - `with_element_state<OngoingScroll>` (:193).
  - `paint_quad` debug border (:202).
  - Capture `on_mouse_event` (:212).
  - `delta.pixel_delta(lh)` (:234) and `.precise()` (:239); R/interactive.rs:614, 606.
  - `notify(view_id)` (:276, :290).
- **Go status:**
  - Line height has no window accessor. Derive it from `WindowTextStyle` (G/element.go:1154).
  - `ElementId` tuple and `CodeLocation` forms are absent. Go has only the Name, View and Integer kinds (G/element.go:45-55), so build composite ids as strings.
- **Hazard:** the design depends on listener order relative to `gpui::list` (doc :62-65, :219-224). Go Div registers listeners in prepaint, not paint, so every routing test must be re-validated.

### dock/**

- **Pure model** (layout/*, active, state, state_convert, dock_placement, drag geometry): no core dependency beyond `Pixels`/`Point`/`Bounds`, plus `Axis` (G9 or a local enum). This is about 4.4k lines and 89 tests, and can be ported first.
- **Entity layer** (panel, registry, tab_group, dock_area) uses:
  - `Entity`/`WeakEntity`/`Context.notify` (G/context.go:65).
  - `EventEmitter` + `emit`/`subscribe`: PRESENT as `Event[S,P].Emit/Subscribe` (G/event.go:36, :111).
  - `subscribe_in` (B/dock/dock_area.rs:1055): MISSING (R/app/context.rs:355). Workaround: capture the window handle.
  - `spawn_in(window, ..)` (B/dock/tab_group.rs:411, :476): MISSING (R/app/context.rs:676). Workaround: `App.Spawn` + `Entity.UpdateIn` (G/entity.go:394). Ordering hazard: the spawned task must run after same-frame deferred effects (tab_group.rs:468-489).
  - `Global` (B/dock/registry.rs:70-108): PRESENT (G/app.go:561, :575).
  - `track_focus`: PRESENT (G/div.go:685).
  - `focus_handle`: `NewFocusHandle(w)` needs a window (G/window.go:339). Rust `cx.focus_handle()` does not, so this is a hazard.
  - `has_active_drag` (B/dock/tab_group.rs:288): PRESENT (G/filedrop.go:189).
  - `on_drop::<T>` (tab_group.rs:732, :736): PRESENT as `OnDrop[T]` (G/div.go:1107).
  - `on_drag_move::<T>` + `DragMoveEvent{event, bounds, drag(cx)}` (tab_group.rs:518-559, 731, 735): **MISSING** (R/elements/div.rs:80, 388) → G10.
  - `on_drag(value, ctor)` (tab_group.rs:1636, skin tests; resizable/panel.rs:370; resizable/resize_handle.rs:253): **MISSING** (R/elements/div.rs:703). Go Div has no mouse-down listener at all: its listener fields are click, move, up, wheel, pinch and drop only (G/div.go:620-649). `App.StartDrag` exists (G/filedrop.go:196) → G10.
  - Drag preview: `AnyDrag.View` is documented as left nil (G/filedrop.go:171-175), and no render path reads `activeDrag`. Rust: R/window.rs:3451-3456 → G11.
  - `on_prepaint` (B/dock/dock_area.rs:1483): built on core `canvas` (B/element_ext.rs:21; R/elements/canvas.rs:10). There is no Go `Canvas`; either a Base `CustomElement` or G12.
  - Resize-handle `cursor_col_resize`/`cursor_row_resize` (resize_handle.rs:264, :271): G7, plus a Div cursor builder (absent).
- **Persistence:**
  - Externally tagged `PanelInfo` `{"stack":{sizes,axis}}`/`{"tabs":{active_index}}`/`{"panel":Value}` (B/dock/state.rs:75-83).
  - Renamed `DockPlacement` strings (:162-168).
  - `skip_serializing_if` (:17-21).
  - The frozen-tag test is at :245-256.
  - Go approach: `encoding/json` with custom `MarshalJSON`/`UnmarshalJSON` and `json.RawMessage` for `Value`. Compare by value, not bytes, because Go emits `1` where serde emits `1.0`.

## Core additions needed (consolidated, Go-side only)

| Id | Addition | Needed by | Rust citation | Go evidence |
|---|---|---|---|---|
| G1 | Scrolling Div: `TrackScroll(ScrollHandle)`, `OverflowScroll`/`OverflowXScroll`/`OverflowYScroll` builders, default wheel listener (pixel/line delta, unclamped apply, clamp at prepaint), `RestrictScrollToAxis`, child offset | virtual_list, scrollable_mask, scroll_bounce tests, Scrollbar on ScrollHandle | R/elements/div.rs:1747-1774, 2426-2460, 2646, 2737, 3816 | G/div.go:601-668 has no scroll state; G/scroll.go:1-9 says input plumbing is "arrives with the input tickets" |
| G2 | ScrollHandle write-back (bounds, max offset, child bounds) plus an exported setter or seam for Base elements; `Bounds()`/`MaxOffset()` on `UniformListScrollHandle` | scrollbar, bounce, mask, virtual_list | R/elements/div.rs:2737; R/elements/uniform_list.rs:80 | G/scroll.go:33-47 (never assigned); G/list.go:59-87 |
| G3 | Wheel scrolling for `UniformList` and `List` (List clamps eagerly, fires the handler, notifies) | bounce/mask tests, Scrollbar on ListState | R/elements/list.rs:1626; uniform_list goes through div interactivity | G/listelement.go:27-29; G/list.go (no ScrollWheel) |
| G4 | `OngoingScroll` (`filter`, dominant-axis lock) | bounce, mask, G1 `restrict_scroll_to_axis` | R/gestures.rs:49-110 | absent from G/ |
| G5 | `App.Notify(EntityID)` (export `applyNotify`) | scrollbar, bounce, mask | R/app.rs:2920 | G/app.go:521 is unexported |
| G6 | Element-author paint: `PaintQuad(w, PaintQuad)`, `PaintLayer(w, bounds, f)` over the current frame scene and paint context | scrollbar, mask debug, virtual_list bg/border | R/window.rs:4339, 4634 | G/scene.go:346/521 exist; `currentFrame(w)` is unexported |
| G7 | Hitbox-scoped cursor: `Window.SetCursorStyle(style, hitbox)` resolved per frame, plus Div `Cursor*` builders | scrollbar, resizable handles (dock) | R/window.rs:3844 | only G/app.go:993 (global) |
| G8 | ListState scrollbar plumbing: `ScrollPxOffsetForScrollbar`, `SetOffsetFromScrollbar`, `ScrollbarDragStarted/Ended`, `MeasureAll` | `ScrollbarHandle for ListState`, bounce/mask tests | R/elements/list.rs:336, 743-799 | G/listelement.go:20-36 (omitted) |
| G9 | Small exports: `Axis`, `Anchor` + `BoundsFromAnchorAndSize`, `Bounds.Contains`, `ScrollDelta.PixelDelta/Precise`, `MouseMoveEvent.Dragging`, window line height, composite `ElementID`, `DeferredScrollToItem`; alias `PrepainAt`→`PrepaintAt` | all | R/geometry.rs:26, 868, 1465; R/interactive.rs:515, 606, 614; R/window.rs:2983; R/elements/uniform_list.rs:103 | absent (grep) |
| G10 | Drag sources: Div `OnMouseDown`, `OnDrag[T](value, ctor)` with a threshold → `StartDrag`, `OnDragMove[T]` + `DragMoveEvent{Event, Bounds, Drag}` | tab_group, resizable (so all of dock_area) | R/elements/div.rs:80, 139, 388, 703 | no `OnMouseDown`/`OnDrag`/`DragMoveEvent` in G/ |
| G11 | Active-drag preview painting at `mouse - CursorOffset` | dock drag skins | R/window.rs:3439-3456 | G/filedrop.go:171-175; no render path reads `activeDrag` |
| G12 | Conveniences: `Canvas(prepaint, paint)`, `Window.UseState`, `Context.Spawn`/`SpawnIn`, `SubscribeIn`, View→`Entity[T]` downcast | dock_area `on_prepaint`, scrollbar, tab_group, panel | R/elements/canvas.rs:10; R/window.rs:4122; R/app/context.rs:355, 676; R/view.rs:53 | G/context.go has no Spawn; G/view.go:80 adapter is unexported |
| G13 | Win32 pointer capture (`SetCapture`/`ReleaseCapture` on button down/up) | tab drag, resizable drag, scrollbar thumb drag released outside the window | reference gpui_windows events.rs:495/525 | G/mouseevent.go:38-41 marks it deferred; `capturedHitbox` exists (G/mouseevent.go:419). It is a user32 call through the Go host's `syscall.NewLazyDLL`, **not** the DLL |

Minimum blocking set for this slice: G1, G2, G4, G5, G6, G10. G3, G7, G8 and G11 are needed for parity. G9 and G12 are trivial or have Base-local shims.

## Native gaps

**None.** Every capability the slice needs already exists natively or is pure Go:
- Quads with corner radii and borders: `Scene.PaintQuad` → kernel `InsertQuad` (G/scene.go:359, :521).
- Layers: `PushLayer`/`PopLayer` (G/scene.go:346-349).
- Cursor: `Host.SetCursorStyle` (G/desktop_windows.go:479).
- Hitboxes, content masks and wheel/touchpad phases: pure Go (G/mouseevent.go; G/directmanipulation_windows.go:241, :387 already emit `ScrollWheelEvent` with phases).
- Mid-prepaint subtree layout: already used by UniformList (G/list.go:168-215) and List.
- In-app drag never touches the platform (`CanStartExternalDrag` is false, G/filedrop.go:347).
- Pointer capture is a Go user32 syscall.

Every gap is a Go-side omission that the Go source comments themselves record as deferred.

## Third-party usage

| Crate | Where | What for | Go replacement |
|---|---|---|---|
| `serde` (derive) | B/scrollbar.rs:18 (`ScrollbarMode`); B/dock/state.rs:2 (all state types: rename, default, skip_serializing_if) | settings and theme JSON; dock layout persistence with frozen tags | `encoding/json` + custom `MarshalJSON`/`UnmarshalJSON` |
| `serde_json` | B/dock/state.rs:83 (`PanelInfo::Panel(Value)` as opaque per-panel state); fixtures and tests in dock_area, state_convert, panel | opaque panel payload; fixture round-trips | `json.RawMessage`; value-equality tests |
| `schemars` | B/scrollbar.rs:17 (`JsonSchema` on `ScrollbarMode`) | settings schema generation | drop, or a hand-written schema |
| `smallvec` | B/virtual_list.rs:26 (item sizes/origins); B/dock/layout/tree.rs:18 (`NodePath = SmallVec<[usize; 8]>`) | stack-allocation optimisation only | Go slices |
| `anyhow` | B/dock/dock_area.rs:11 (`Result` for load) | error plumbing | `error` |
| `web_time` | B/scrollbar.rs:3, B/scroll_bounce.rs:13 (`Instant`) | wasm-safe clock for animations | `App.Now()` (deterministic in tests) |
| `sum-tree`, `itertools` | not used in this slice | — | — |

## Internal dependency order

1. **Leaf utilities (other slices):**
   - `geometry` (`AxisExt`, `Placement`) and `event` (`OngoingScrollExt` :11, `InteractiveElementExt::lock_scroll_axis` :26-29, needs G4/G1).
   - `styled::StyledExt` (:78), `element_ext::ElementExt::on_prepaint` (needs Canvas/G12), `animation` eases, `theme` (`theme.scrollbar`, B/theme.rs:20).
   - `observe::TestSupportExt` (tests).
2. **`auto_scroll`:** no dependencies, so it can be ported now. Its consumer is the input slice.
3. **`scrollbar`:** defines `ScrollbarHandle`. Needs theme, animation, AxisExt and G2/G5/G6/G7/G8/G9.
4. **`scroll_bounce`, `scrollable_mask`:** depend on `ScrollbarHandle` and `OngoingScrollExt`, plus G1/G3/G4.
5. **`virtual_list`:** depends on `ScrollbarHandle`, `lock_scroll_axis` and G1/G2. Its consumers are `table` and text.
6. **dock model** (no core gaps, can start now), in this order:
   1. `layout/node` → `layout/tree` → `layout/normalize` → `layout/edit` → `layout/builder` (builder needs `panel`).
   2. `active`, `state` → `state_convert` → `dock_placement` (needs `resizable::PANEL_MIN_SIZE`) → `drag`.
7. **dock entities:** `panel` → `tab_group` (needs G10/G11/G13) → `registry` → `dock_area` (needs the `resizable` slice, which itself needs G10 + G7, plus `ElementExt::on_prepaint`) → `mod`, `test_support`.
