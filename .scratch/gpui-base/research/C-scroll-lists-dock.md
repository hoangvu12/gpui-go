# C — Scroll, lists, dock: GPUI Base port feasibility

Upstream: `upstream-base/ce/crates/base/src` @ c0820693 (read only). Go core: `gpui-go/gpui` (pin 254b5dbd). Reference core: `reference/ce-source/crates/gpui/src`.
Status key: **PRESENT** (Go symbol cited), **MISSING-GO** (needs a Go addition to core, or a base-local shim), **MISSING-NATIVE** (needs a change to the frozen DLL).

## Headline

- **The custom-element answer for VirtualList is YES.** An external package can build items during prepaint and drive `layout_as_root`, `prepaint_at` and `paint` under a content mask using only exported Go API. Details are in §1.
- **The real blocker is Go-side scroll plumbing in core.** In the Go port, `Div` has no `TrackScroll` and no `OverflowScroll` wheel handling. `ScrollHandle` is not wired to any element, and its bounds, max offset and child bounds are never populated. `UniformList` and `List` do not consume wheel events: `list.go:5-18` and `listelement.go:20-36` record this as deferred to "the input ticket". Every module in this slice either sits on top of a scrolling div/list or reads `ScrollHandle.bounds()/max_offset()`.
- **Second blocker: element-author paint and interaction surface.** There is no public quad painting (`window.paint_quad`), no `paint_layer`, no hitbox-scoped cursor and no `notify(EntityId)`. There is no `on_drag` / `on_drag_move` / `DragMoveEvent`, and the drag preview is never painted. There is also no Win32 pointer capture (G13). In-app element drag is therefore **partially present**: `App.StartDrag/ActiveDrag/StopDrag` and `OnDrop[T]` exist (filedrop.go:189-229, div.go:1107), but drag *sources* (`OnDrag`, which needs a mouse-down listener on Div), drag-move and preview are absent. The dock needs them both for tab dragging and, through base `resizable`, for split resizing.
- **No MISSING-NATIVE items.** Everything above is Go-level. The native DLL already has quads with corner radii (`Scene.PaintQuad`, scene.go:521), layers (`Scene.PushLayer`, scene.go:346), the cursor (`App.SetCursorStyle`, app.go:993) and the layout subtree compute that UniformList already uses mid-prepaint (list.go:174, 308).

---

## 1. virtual_list.rs

| | |
|---|---|
| LOC | 905 total (767 production, 138 test) |
| Public | `VirtualListScrollHandle` (`new`, `base_handle`, `scroll_to_item`, `scroll_to_bottom`, `Deref`/`AsRef<ScrollHandle>`, `From<ScrollHandle>`, `impl ScrollbarHandle`); `v_virtual_list`, `h_virtual_list`, `virtual_list` (doc-hidden); `VirtualList` (`Styled`, `track_scroll`, `with_sizing_behavior`, `with_item_to_measure_index`, `with_scroll_handle` doc-hidden); `VirtualListFrameState`; `ItemSizeLayout` |
| Tests | 7. Four pure tests of the `visible_range` binary search (normal, overdraw past end, uneven sizes, empty). Three `gpui::test`: vertical and horizontal visible range plus deferred `scroll_to_item(12)` moving the offset negative (`exercise_axis`, :802-835), and an empty list rendering no items (:889) |
| Base deps | `AxisExt` (geometry.rs), `InteractiveElementExt::lock_scroll_axis` (event.rs:29 → core `restrict_scroll_to_axis`), `ScrollbarHandle` (scrollbar.rs:68) |
| 3rd-party | `smallvec` → slices |

### Key question: can an external Go package build, lay out, prepaint and paint items itself?

Yes. Every step has an exported Go counterpart:

| Upstream step | Go API | Status |
|---|---|---|
| `impl Element` with typed states | `Element[L,P]` (element.go:272), `CustomElement` (element.go:615) | PRESENT |
| `measure_item` → `item.layout_as_root(MinContent…)` during **request_layout** (:325-335) | `AnyElement.LayoutAsRoot` (element.go:654), `MinContentAvailableSpace` / `DefiniteAvailableSpace` (layout_context.go:118-128). Precedent: UniformList measures inside its RequestLayout (list.go:171-178) | PRESENT |
| `window.with_element_state(global_id)` (:466) | `WithElementState` (element.go:1379); the global id comes from `ID()` via drawable (element.go:448-453) | PRESENT |
| `request_measured_layout` with known/available dims (:530-575) | `RequestElementMeasuredLayout` (element.go:1536), `MeasureRequest` (layout_context.go:167, `KnownWidthPresent`, `AvailWidth.Kind`…) | PRESENT |
| `request_layout(style)` (Auto sizing) | `RequestElementLayout` (element.go:1530) | PRESENT |
| `with_text_style`, `text_style().font_size`, `rem_size` | `WithTextStyleVoid` (element.go:1243), `WindowTextStyle` (1154), `RemSize` (1226) | PRESENT |
| `with_content_mask(bounds)` around items (:705) | `WithContentMask` / `WithContentMaskVoid` (element.go:1165/1251) | PRESENT |
| `item.layout_as_root(Definite…)` + `item.prepaint_at(origin)` (:729-730) | `AnyElement.LayoutAsRoot` + `AnyElement.PrepainAt` (element.go:663; note the misspelled name) or `WithAbsoluteElementOffsetVoid` + `Prepaint` | PRESENT |
| `item.paint` (:760) | `AnyElement.Paint` (element.go:645) | PRESENT, with hazard H1 |
| `ListSizingBehavior` | `ListInfer` / `ListAuto` (list.go:27-35) | PRESENT |
| `ScrollStrategy` | scroll.go:15 (Top/Center/Bottom/Nearest) | PRESENT |
| `DeferredScrollToItem` (pub in core uniform_list.rs:103) | none | MISSING-GO, trivial; define it in base |
| `self.base.interactivity().request_layout/prepaint/paint/compute_style` (:460, 609, 660, 751): the Stateful<Div> machinery that provides style, hitbox, **overflow_scroll wheel handling, track_scroll bounds/max_offset write-back**, background and border painting | No exported `Interactivity`. `DivElement` internals are unexported (div.go:1371-1650) and Div has no scroll at all (fields div.go:601-668) | **MISSING-GO, major** (see G1, G2, G3) |
| `scroll_handle.offset()/set_offset()` | `ScrollHandle.Offset/SetOffset` (scroll.go:68/81) | PRESENT |
| `ScrollHandle.bounds()/max_offset()` updated by the tracked div | Exists (scroll.go:74/87), but fields are unexported (scroll.go:33-47) and **no element writes them**. An external VirtualList cannot populate them | MISSING-GO (G2) |
| `lock_scroll_axis` → `restrict_scroll_to_axis` + `OngoingScroll` | none (no `OngoingScroll`, no restrict) | MISSING-GO (G4) |

**Hazards**

- **H1 — paint offset.** Go `drawable.paint` re-resolves bounds with `layoutBoundsOf` at the current element offset (element.go:560; `LayoutBounds` adds the offset, layout.go:786-790). Rust instead reuses the prepaint bounds. So the Go VirtualList must wrap each `item.Paint` in the same `WithAbsoluteElementOffsetVoid(origin)` and content mask used at prepaint, as UniformList does (list.go:325-331). It must store each item's origin.
- **H2 — listener order.** The Go Div registers hitboxes and mouse listeners in **prepaint** (div.go:1491-1498); Rust registers them in paint. Any base element that registers in Paint ends up ordered after every core div. This matters for capture/bubble ordering in ScrollableMask and ScrollBounce.
- **H3 — reentrancy (checked: OK).** `render_items` calls `view.update` during prepaint. The Go `viewElement` runs `RenderOnce` only inside RequestLayout (view.go:249-262). Prepaint and Paint only push the entity id on the rendered-view stack (view.go:267-292, 306-311) and hold no entity lease, so `Entity.UpdateIn(cx, w, ..)` (entity.go:394) from inside the VirtualList prepaint is not reentrant.
- **H4 — Div paints background only.** `paintDivElementBox` draws only the background quad, with no border and no corner radii (div.go:1655-1675). Upstream VirtualList inherits border, padding and rounded corners from `Stateful<Div>`. A Go VirtualList that wraps or reimplements the base div gets no border paint until G6 lands, or until Div gains border and radius paint.

**Size:** M (~600 Go lines plus tests) once G1–G3 exist. Without them it can only paint statically positioned items with no wheel scrolling.

---

## 2. scrollbar.rs

| | |
|---|---|
| LOC | 2597 (1794 production, 803 test) |
| Public | `ScrollbarMode` (serde + JsonSchema), `trait ScrollbarHandle` (`viewport_bounds`, `offset`, `set_offset`, `content_size`, `start_drag`/`end_drag`) with impls for `ScrollHandle`, `UniformListScrollHandle`, `ListState`; `ScrollbarEntrance`; `ScrollbarMotion` (builders/getters); `ScrollbarAxis`; `ScrollbarTrackStyle`, `ScrollbarThumbStyle`, `ScrollbarStyles` (fluent); `Scrollbar` (`new`/`horizontal`/`vertical`/`id`/`mode`/`scroll_size`/`viewport_bounds`/`viewport_from_layout`/`axis`/`styles`/`max_fps`/`width`); `PrepaintState`, `AxisPrepaintState` |
| Tests | 36: 23 pure, 13 `gpui::test`. Pure tests cover visibility/width animation curves, entrances, reduced-motion snapping, idle-hold logic, thumb geometry and radius clamp, style precedence (instance > theme > default). Window tests cover track click (vertical/horizontal) setting the offset, no-overflow meaning no track, hidden hover-mode ignoring click/drag, hover reveal, drag notify throttling at max_fps, full-track thumb, and `start_drag`/`end_drag` calls |
| Base deps | `theme` (`ActiveTheme`, `theme.scrollbar.{mode,motion,styles}`, `tokens.radius.md`), `animation` (`ease_in_cubic`/`ease_out_cubic`), `AxisExt` |
| 3rd-party | `web_time::Instant` → `App.Now()`, which gives deterministic tests in Go; `serde` → encoding/json; `schemars` → drop or hand-write; `palette` (tests only) |

| Core API (upstream line) | Go status |
|---|---|
| `Element` impl, `request_layout(Style abs 100%)` (:1265-1280) | PRESENT: `CustomElement`, `RequestElementLayout`, `Style.Position/Size` (style.go:778+) |
| `insert_hitbox` under content mask (:1292, 1535) | PRESENT: `Window.InsertHitbox` (mouseevent.go:477) |
| `window.use_state` (:1297) | MISSING-GO. Emulate with `WithElementState` holding a pointer (element.go:1379) |
| `request_animation_frame` (:1346, 1477) | PRESENT: `RequestAnimationFrame(w)` (imagecache.go:958) |
| `window.spawn` + `background_executor().timer` (:1357-1363) | PRESENT: `App.Spawn` (executor.go:742) + background `Spawn` with `TaskRun.Sleep` (executor.go:625) + `AsyncApp.Await` (executor.go:723) |
| `window.current_view()` | PRESENT: `CurrentView(w)` (view.go:325) |
| `cx.notify(EntityId)` (:1361, 1653, 1695, 1765, 1784) | MISSING-GO: `applyNotify` is unexported (app.go:521). Needs `App.Notify(EntityID)` (G5) |
| `cx.reduce_motion()` | PRESENT: `App.ReduceMotion` (app.go:282) |
| `window.paint_layer` (:1611) | MISSING-GO. Native has it (`Scene.PushLayer`, scene.go:346); the frame scene is unexported (G6) |
| `paint_quad(fill(..).corner_radii(r))`, border quad (:1612-1639) | MISSING-GO for element authors. `Scene.PaintQuad` (scene.go:521) and `PaintQuad` (paint.go:234) exist, but `currentFrame(w).scene` / `frame.paint` are unexported (G6) |
| `window.set_cursor_style(style, &hitbox)` (:1609) | MISSING-GO: only the global `App.SetCursorStyle` (app.go:993) exists; no per-frame hitbox cursor resolution (G7) |
| `on_mouse_event` for ScrollWheel/MouseDown/MouseMove/MouseUp (:1642-1787) | PRESENT: `Window.OnMouseEvent` (mouseevent.go:459) plus event types (mouseevent.go:102-172) |
| `MouseMoveEvent.dragging()` | Derivable from `PressedButton` (mouseevent.go:107); trivial |
| `stop_propagation` | PRESENT: `App.StopPropagation` |
| `ScrollHandle.bounds/max_offset` for `content_size` (:83-99) | Getters PRESENT (scroll.go:74/87), but values are always zero because no element populates them (G2) |
| `UniformListScrollHandle` base_handle bounds/max_offset (:101-118) | MISSING-GO: Go handle has `Offset`/`SetOffset`/`LastContentSize` (list.go:69-87) but no viewport `Bounds` (G2) |
| `ListState::{viewport_bounds, scroll_px_offset_for_scrollbar, set_offset_from_scrollbar, max_offset_for_scrollbar, scrollbar_drag_started/ended}` (:120-144) | `ViewportBounds` (listelement.go:337) and `MaxOffsetForScrollbar` (:348) PRESENT. The other four are MISSING-GO; listelement.go:23 records the "scrollbar drag plumbing" as omitted (G8) |
| `Bounds::from_anchor_and_size`, `Anchor`, `Bounds::contains`, `Hsla.opacity`, `Axis` | `OpacityHsla` PRESENT (paint.go:164). No `Axis` type exists in Go gpui; `boundsContain` is unexported (mouseevent.go:380). Define these locally in base or export them (G9) |

**Hazards**

- Upstream reads wall-clock `Instant::now()` everywhere. In Go, use `App.Now()` so `TestApp.AdvanceClock` drives the animations.
- Listeners are registered in Paint (see H2).
- Theme coupling: `theme.scrollbar` must exist in the theme slice first.

**Size:** L (~1600 Go production lines plus ~800 test lines).

---

## 3. scroll_bounce.rs

| | |
|---|---|
| LOC | 1058 (497 production) |
| Public | `ScrollBounceMotion` (`with_tracking`, `with_response`, getters), `ScrollBounce` (`new(id, &H: ScrollbarHandle, child)`, `enabled`, `motion`, `on_scroll`), `ScrollBouncePrepaintState` (doc-hidden) |
| Tests | 23: 13 window, 10 pure. Window tests send `ScrollWheelEvent` sequences with `TouchPhase` Started/Moved/Ended through a `div().overflow_y_scroll().track_scroll()` viewport and through `gpui::list(ListState…measure_all())`. They cover stretch past edge, reverse drag consuming stretch, momentum suppression, phaseless wheel pauses, catch slop, reduced-motion/disabled, diagonal wobble, and two packets dispatched in one update via `window.dispatch_event(PlatformInput::ScrollWheel)`. Pure tests cover the spring physics: frame-rate independence, settling, extent re-grab |
| Base deps | `ScrollbarHandle`, `OngoingScrollExt::lock_axis` (event.rs:11-23) |
| 3rd-party | `web_time::Instant` |

| Core API | Go status |
|---|---|
| Wrapper element delegating to `child.request_layout/prepaint/paint` (:214, 252, 409) | PRESENT: `AnyElement.RequestLayout/Prepaint/Paint` (element.go:629-650). H1 applies: apply the same y offset at paint |
| `with_element_state(Rc<RefCell<State>>)` | PRESENT (element.go:1379) |
| `with_element_offset(point(0, offset))` | PRESENT: `WithElementOffset` (element.go:1197; generic only, no Void form) |
| `with_content_mask`, `insert_hitbox`, `hitbox.should_handle_scroll` | PRESENT: element.go:1251, mouseevent.go:477, `Hitbox.ShouldHandleScroll` (mouseevent.go:280) |
| `on_mouse_event(ScrollWheelEvent)` in both Capture and Bubble phases; `ScrollDelta::Pixels`; `TouchPhase` | PRESENT: mouseevent.go:142-172. Windows precision-touchpad phases come from directmanipulation_windows.go:245/373 |
| `cx.background_executor().now()` | PRESENT: `App.Now` (imagecache.go:852) |
| `request_animation_frame`, `notify(view)`, `reduce_motion` | rAF and reduce_motion PRESENT; notify-by-id MISSING-GO (G5) |
| `OngoingScroll::filter` (core gestures.rs:49-110) | MISSING-GO (G4): about 80 lines, a pure function of delta, phase and time |
| Tests need a scrolling `div` with `track_scroll` and a wheel-scrolling `list` that clamps eagerly | MISSING-GO (G1, G3) |

**Hazards**

- The algorithm depends on core wheel-handling order. Core div applies the delta in its listener, unclamped, and clamps at prepaint (:361-363 comment). Core list clamps eagerly. The Go G1/G3 implementations must reproduce these semantics exactly or the residual math will break.
- The listener-order assumption in H2 also applies here.

**Size:** M (~550 Go production lines plus ~600 test lines).

---

## 4. scrollable_mask.rs

| | |
|---|---|
| LOC | 910 (299 production) |
| Public | `ScrollableMask<H>` (`new(axis, &H)`, `id`, `debug`); crate-level `horizontal_scroll_area` (used by table) |
| Tests | 16 window tests of wheel routing. Cover horizontal-area vertical-wheel passthrough, behaviour inside `gpui::list`, edge chaining (vertical hands off, horizontal traps), occlusion, per-gesture axis lock and unlock on a strong turn, transient-overscroll clamp |
| Base deps | `AxisExt`, `OngoingScrollExt`, `ScrollbarHandle`, `StyledExt::refine_style` |
| 3rd-party | none |

| Core API | Go status |
|---|---|
| `request_layout` (abs 100%), `insert_hitbox`, `with_content_mask`, `with_element_state` | PRESENT (see above) |
| `ElementId::CodeLocation` / `(id, "axis")` tuple ids (:19, :132) | MISSING-GO, minor: Go `ElementID` is only Name/View/Integer (element.go:45-70). Use a `Name` built from caller file:line plus the axis suffix |
| `window.line_height()`, `ScrollDelta::pixel_delta(lh)`, `.precise()` | MISSING-GO, trivial. Derive from `WindowTextStyle(w).LineHeightPixels` (div.go:173) and `ScrollDelta.IsPixels/Lines` (mouseevent.go:142-150) |
| `paint_quad` (debug border) | MISSING-GO (G6), debug only |
| Capture-phase `on_mouse_event(ScrollWheelEvent)`, `should_handle_scroll`, `stop_propagation`, `notify(view_id)` | PRESENT, except notify-by-id (G5) |
| `div().overflow_hidden().track_scroll(h)` (`horizontal_scroll_area`, :41-53): a div that applies the ScrollHandle offset to its children and reports bounds/max_offset | MISSING-GO (G1/G2): Go Div only clips via `overflowMask` (div.go:1347) and ignores ScrollHandle |
| `OngoingScroll` | MISSING-GO (G4) |

**Hazard.** The design explicitly relies on the order of wheel listeners relative to `gpui::list` (doc comment :62-65, :219-224). Go registers div and list listeners at prepaint (H2), so revalidate every test.

**Size:** S/M (~300 Go production lines plus ~550 test lines).

---

## 5. auto_scroll.rs

| | |
|---|---|
| LOC | 155 (126 production) |
| Public | `AutoScroll` (`delta`, `compute_delta`, `set`, `is_active`, `stop`, pub field `last_drag_position`) |
| Tests | 2 pure tests: dead-zone and ramp math; `stop` clears state |
| 3rd-party | `Arc<Mutex>` → `sync.Mutex` or a plain field (foreground only) |

| Core API | Go status |
|---|---|
| `cx.spawn(async |weak, cx| loop { timer(16ms); weak.update(..) })` | PRESENT: `App.Spawn` + `AsyncApp.Await(Background.Spawn(Sleep))` + `Entity.Downgrade` / `WeakEntity.TryUpdate` (entity.go:507/597). `Context.Spawn` (entity-bound) is not present; that is a convenience only |
| `Task` dropped means cancelled | Go needs an explicit `Task.Cancel` (executor.go:535) in `stop`/`set(None)`. Hazard: easy to leak |

**Size:** S (~120 Go lines).

---

## 6. dock/* (with layout/, fixtures/, registry, persistence)

| File | LOC (prod/total) | Tests | Notes |
|---|---|---|---|
| mod.rs | 179/202 | 1 | re-exports |
| active.rs | 125/222 | 7 | pure active-state reconciliation |
| dock_placement.rs | 182/263 | 7 | `DockSizing` pointer→size clamp, `Dock` |
| drag.rs | 283/380 | 4 | `DragPanel`, `AnyDrag`, `DropTarget`, `DropIndicator`, `DropPlaceholderBounds`, `split_placement_at` (pure geometry) |
| panel.rs | 247/310 | 2 | `trait Panel: EventEmitter<PanelEvent>+Render+Focusable`, `trait PanelView` (`view() -> AnyView`, downcast) |
| registry.rs | 119/119 | 0 | `PanelRegistry` as a gpui `Global`, `register_panel` |
| state.rs | 193/267 | 3 | serde schema (frozen tags) |
| state_convert.rs | 189/587 | 15 | `PanelSource` / `PanelBuilder`, tree↔state |
| tab_group.rs | 974/1815 | 27 | `TabGroup` entity, `TabGroupContext`, `TabGroupRenderer`, drop handling |
| dock_area.rs | 1937/4148 | 57 | `DockArea`, `DockAreaRenderer`, `DockContext`, `DockEvent`, `dock_frame`, load/dump |
| layout/{builder,edit,node,normalize,tree,mod}.rs | 1126/1896 | 30 | pure `PaneTree` model: insert/move/split/set_sizes/normalize |
| test_support.rs | 196 | 0 | test panels |
| fixtures/*.json | 6 files | — | load/dump round-trip fixtures |

**Totals:** about 5,750 production lines, about 5,000 test lines, 153 tests.

**What the tests cover.**

- Pure: the layout tree edit/normalize algebra, state↔tree conversion, the drop-placement geometry, and active-state reconciliation.
- Entity/window: load→dump round-trips (shipped fixture, unregistered panels, zero-size sentinel); move/remove keeping entities and active notifications; zoom; lock; split share math after drops; the `TabGroupRenderer` compose order; and the drag flow. That flow is `simulate_mouse_down/move/up` with `on_drag` on a tab and the drop on the content frame (tab_group.rs:1636, 1717-1800).
- Most `TabGroup` drop tests call `group.on_drop(...)` directly. Only about 2 need real drag dispatch.

**Base deps:** `resizable` (`h_resizable`, `v_resizable`, `resizable_panel`, `ResizableState`, `ResizablePanelEvent`, `ResizeHandleContext`, `PANEL_MIN_SIZE`), `geometry::Placement`, `element_ext::ElementExt::on_prepaint` (built on core `canvas`), `observe::TestSupportExt`.

**3rd-party:** `serde` / `serde_json` (`PanelInfo::Panel(serde_json::Value)`) → `encoding/json` with `json.RawMessage`; `anyhow`; `smallvec`.

| Core API | Go status |
|---|---|
| Entities, `Context.notify`, `weak_entity` | PRESENT: `Context.Notify` (context.go:65), `Entity.Downgrade` (entity.go:507) |
| `EventEmitter` + `cx.emit`, `cx.subscribe` | PRESENT: `Event[S,P].Emit/Subscribe` (event.go:36/111) |
| `cx.subscribe_in(&e, window, ..)`, `window.subscribe` | MISSING-GO, small: no window-carrying subscribe. Workaround: capture the window handle |
| `cx.spawn_in(window, async ..)` (tab_group.rs:411, 476) | MISSING-GO convenience. `App.Spawn` + `Entity.UpdateIn(w)` (entity.go:394) works |
| `Focusable`, `focus_handle`, `.focus(window)`, `track_focus`, `tab_group()` | PRESENT: `NewFocusHandle(w)` (window.go:339; needs a window, while Rust `cx.focus_handle()` does not — hazard), `Window.Focus` (focus.go:348), `DivElement.TrackFocus` / `TabGroup` (div.go:685/1160) |
| `AnyView`, `into_any_element`, `downcast::<T>()` (panel.rs:200) | `View` interface plus `TryIntoElement(View)` (element.go:729) PRESENT. Downcasting a `View` back to `Entity[T]` is MISSING-GO: `viewAdapter` is unexported (view.go:80). Keep a typed handle in `PanelView` instead |
| `Global` (`set_global`, `global`, `global_mut`) | PRESENT: `App.SetGlobal` (app.go:561), `Global[T]` (app.go:575); store a pointer for mutation |
| `on_drop::<T>` (tab_group.rs:732-736) | PRESENT: `OnDrop[T]` (div.go:1107). No `can_drop` predicate, which the dock does not need |
| `on_drag_move::<T>` with `DragMoveEvent{event, bounds, drag(cx)}` (tab_group.rs:518-559, 731/735) | **MISSING-GO** (G10): no `DragMoveEvent` / `OnDragMove` anywhere in gpui/*.go |
| `on_drag(value, ctor→Entity<W>)` (skins/tests, tab_group.rs:1636) | **MISSING-GO** (G10): `App.StartDrag` (filedrop.go:196) exists, but there is no threshold-based div `OnDrag` and no `OnMouseDown` on Div |
| Drag preview painting (the skin's `on_drag` view, `DragPanel::{drag_offset, preview_size}`) | **MISSING-GO** (G11): `AnyDrag.View` is documented as "left nil… preview painting belongs to the draw path" (filedrop.go:171-175); render.go never paints the active drag. Reference: window.rs:3451-3456 does `prepaint_as_root(mouse_position - cursor_offset, min_size)` on the drag view |
| Drag continuing outside the window, or mouse-up outside it | **MISSING-GO** (G13): the Go host never calls Win32 `SetCapture`/`ReleaseCapture`. mouseevent.go:38-41 says "SetCapture arrives with the mouse input ticket", but the reference does it on button down/up (gpui_windows/src/events.rs:495, 525). Without capture, a tab drag or scrollbar-thumb drag released outside the window leaves `activeDrag` / `dragged_axis` stuck until the next in-window up. This is a user32 call through the Go host's `syscall.NewLazyDLL` (win32host_windows.go:56), **not** the frozen DLL |
| Split-handle and dock-edge resizing via base `resizable` | Transitively needs **G10 `OnDrag` + `OnDragMove` and G7 cursor**: resizable/resize_handle.rs:252-300 (`on_drag`, `cursor_col_resize` / `cursor_row_resize`) and resizable/panel.rs:370 (`on_drag(DragPanel ..)`). Even a dock with no tab dragging is blocked on G10 through `resizable` |
| `cx.has_active_drag()` | PRESENT (filedrop.go:189); unhandled mouse-up cancels (mouseevent.go:591) |
| `on_prepaint` → `canvas(prepaint_cb, paint_cb)` | No `canvas` in Go. Implement it as a small base `CustomElement` (PRESENT building blocks) or add core `Canvas` (G12) |
| Div styling used structurally: `flex_col`, `flex_row`, `flex_1`, `min_h`, `relative`, `size_full`, `overflow_hidden` | Partly present: `Flex`, `SizeFull`, `Refine(StyleRefinement)` (div.go:698/850/896) cover these through Style fields. Styled-ext coverage belongs to another slice |
| Serde persistence: externally tagged `PanelInfo` `{"stack":{sizes,axis:usize}}` / `{"tabs":{active_index}}` / `{"panel":Value}`, renamed `DockPlacement` strings, `Pixels` as f32 numbers, `skip_serializing_if` | encoding/json with custom `MarshalJSON`/`UnmarshalJSON`; `json.RawMessage` for `Value`. Hazard: Go emits `200` where serde emits `200.0`. Semantic equality is fine, byte equality is not; compare as values in tests (upstream tests use `json!` value equality) |

**Hazards**

- The dock is mostly model and entity logic. It is portable today except for (a) drag-move/drag-start/preview and (b) its hard dependency on base `resizable` (other slice).
- Async ordering: `spawn_in` for active-state reconcile "after every same-frame mutation" (tab_group.rs:468-489) requires the Go foreground spawn to run after deferred effects. Check this against Go executor semantics.

**Size:** XL (about 6–7k Go production lines plus about 5k test lines). The `layout/` subtree (~1.1k) and state/persistence (~600) are an independent first milestone with no core gaps.

---

## Deduplicated MISSING-GO core additions

| ID | Addition | Needed by | Evidence |
|---|---|---|---|
| G1 | **Scrolling Div**: `TrackScroll(ScrollHandle)`, `OverflowScroll`/`OverflowXScroll`/`OverflowYScroll` builders; apply the handle offset to child prepaint/paint; default wheel listener (pixel/line delta, unclamped apply, clamp at prepaint, `restrict_scroll_to_axis`) | VirtualList, ScrollableMask (`horizontal_scroll_area`), ScrollBounce tests, Scrollbar on `ScrollHandle` | div.go:601-668 has no scroll state; scroll.go:1-9 says wheel plumbing "arrives with the hitbox/input tickets"; no `ScrollHandle` use outside scroll.go |
| G2 | **ScrollHandle write-back**: the tracked element writes `bounds`, `maxOffset`, `childBounds`, plus an exported setter or element seam so base elements (VirtualList) can publish them; add `Bounds()` / `MaxOffset()` to `UniformListScrollHandle` | Scrollbar `content_size`, ScrollBounce/ScrollableMask `max_scroll_extent`, VirtualList | scroll.go:33-47 (unexported, never assigned); list.go:59-87 |
| G3 | **Wheel scrolling for `UniformList` and `List`** (List: clamp eagerly, fire the scroll handler, notify) | ScrollBounce/ScrollableMask list tests, Scrollbar on `ListState` | list.go:15-17; listelement.go:27-29 |
| G4 | **`OngoingScroll`** (core gestures.rs:49-110: dominant-axis lock with timeout and unlock ratio) | ScrollBounce, ScrollableMask, G1's `restrict_scroll_to_axis` | not present in gpui/*.go |
| G5 | **`App.Notify(EntityID)`** (export of `applyNotify`) | Scrollbar, ScrollBounce, ScrollableMask (notify `current_view` from listeners) | app.go:521 unexported |
| G6 | **Element-author paint API**: `PaintQuad(w, PaintQuad)`, `PaintLayer(w, bounds, f)` (plus shadows/paths for other slices) over the frame scene and paint context | Scrollbar (track/thumb with radius), ScrollableMask debug, VirtualList background/border | scene.go:521 / 346 exist, but `currentFrame(w).scene` and `.paint` are unexported (element.go:984+, div.go:1656-1670) |
| G7 | **Hitbox-scoped cursor**: `Window.SetCursorStyle(style, hitbox)`, resolved per frame against the hovered hitbox, applied via the existing `App.SetCursorStyle` | Scrollbar (default cursor over the bar), resize handles elsewhere | only global app.go:993 |
| G8 | **`ListState` scrollbar plumbing**: `ScrollPxOffsetForScrollbar`, `SetOffsetFromScrollbar`, `ScrollbarDragStarted/Ended` (frozen height), `Offset/SetOffset`, `MeasureAll`, `ListAlignment` | Scrollbar `impl ScrollbarHandle for ListState`, ScrollBounce/ScrollableMask tests | listelement.go:20-36 lists these as omitted |
| G9 | Small geometry/util exports: `Axis`, `Bounds.Contains`, `Bounds.FromAnchorAndSize` / `Anchor`, `DefiniteLength.ToPixels`, `ScrollDelta.PixelDelta/Precise`, `MouseMoveEvent.Dragging`, window line height, composite `ElementID` (named child / code location), `DeferredScrollToItem`, `WithElementOffsetVoid`, rename/alias `PrepainAt`→`PrepaintAt` | all modules | `boundsContain` unexported (mouseevent.go:380); no `type Axis`; ElementID kinds element.go:45-56 |
| G10 | **Drag sources and drag-move**: Div `OnMouseDown`, `OnDrag[T](value, ctor)` with drag threshold → `App.StartDrag`, `OnDragMove[T]` delivering `DragMoveEvent{Event, Bounds, Drag(app)}` | dock TabGroup drop indicator, dock drag tests | no `OnDrag` / `DragMoveEvent` in gpui/*.go; Div listener set div.go:620-649 |
| G11 | **Active-drag preview rendering**: paint `AnyDrag.View` at cursor minus `CursorOffset` on top of the frame | dock skins' drag preview | filedrop.go:171-175; no activeDrag reference in render.go |
| G13 | **Win32 pointer capture**: `SetCapture` on a button down inside the client area and `ReleaseCapture` on up, in the Go host (reference gpui_windows/src/events.rs:495/525); wire it to the existing `capturedHitbox` (mouseevent.go:419-422, 601) | dock tab drag, resizable drag, Scrollbar thumb drag released outside the window | mouseevent.go:38-41 marks it deferred; no SetCapture in gpui/*.go. Go syscall, not the native DLL |
| G12 | Optional conveniences: `Canvas(prepaint, paint)`, `Window.UseState`, `Context.Spawn` / `SpawnIn`, `SubscribeIn` (window-aware), `View` → `Entity[T]` downcast | dock `on_prepaint`, Scrollbar `use_state`, TabGroup async, panel downcast | view.go:80 unexported adapter; context.go has no Spawn |

## MISSING-NATIVE

**None found.** Evidence that the needed capabilities already exist natively or in pure Go:

- Quads with corner radii and borders: `Scene.PaintQuad` → kernel `InsertQuad` (scene.go:359/521).
- Layers: `PushLayer`/`PopLayer` (scene.go:346-349).
- Cursor: `Host.SetCursorStyle` (desktop_windows.go:479).
- Content-masked hitboxes and wheel/touchpad phases: pure Go (mouseevent.go, directmanipulation_windows.go).
- Pointer capture (G13) and in-app drag (G10/G11): pure Go. The Win32 host calls user32 through `syscall.NewLazyDLL` (win32host_windows.go:56-60). In-app drag in the pin never touches the platform on Windows, because `CanStartExternalDrag` is false (filedrop.go:30-35, 339+). OLE file drop is the only native drag path, and it is out of scope here.
- Re-entrant subtree `ComputeLayout` mid-prepaint: already used by UniformList (list.go:174, 308) and List.

Every gap above is a Go-side omission recorded as deferred in the Go source comments.
