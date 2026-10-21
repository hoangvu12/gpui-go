# A — Infra + Overlays slice (gpui-base @ c0820693) → gpui-go readiness

Status: DONE (read-only research, 2026-10-19).

Upstream root: `upstream-base/ce/crates/base/src` (U:). Go root: `gpui-go/gpui` (G:). Reference core: `gpui-go/reference/ce-source/crates/gpui/src` (R:).
Status words: **PRESENT** = exported Go symbol found by grep and read. **MISSING-GO** = core (package `gpui`) must add or export something; it cannot be done from outside the package. **BASE-LOCAL** = can be written in the base package with the exported core API. **MISSING-NATIVE** = would need the frozen DLL to change.

## 0. Global findings (read first)

### 0.1 There is no automatic frame loop (MISSING-GO, blocks every overlay and animation)
- Drawing happens only when the app calls `DrawWindowFrame` (G:render.go:58) from the host's WM_PAINT hook `WindowOptions.OnRedraw` (G:window.go:112-115). See the explicit loop in `examples/hello/main.go:3-6,50-63`. `OpenWindowView` draws one frame (G:view.go:364-385).
- `RequestAnimationFrame(w)` (G:imagecache.go:958) and every internal `requestRefresh` (G:focus.go:520) only set `ds.refreshRequested = true`. Nothing reads the flag except the test accessor `RefreshRequested` (G:focus.go:531). It is cleared at G:render.go:153.
- `App.RefreshWindows` (G:app.go:336) queues `effRefresh`. Its handler calls `drawHook.DirtyWindows()` and discards the result (G:app.go:501-506). `DrawHook` (G:app.go:58) is a stub seam.
- `Context.Notify` → `applyNotify` (G:app.go:521) only runs entity observers. It never marks a window dirty. A view that notifies is not redrawn.
- The Win32 host is pure Go syscalls. `InvalidateRect` is bound only in a test file (G:directmanipulation_windows_test.go:390). `SystemParametersInfoW` and others are bound in G:desktop_windows.go:77. So a real loop is **Go work, not native**. It needs: (a) tracking which views each window rendered, so a notify marks the window dirty; (b) consuming `refreshRequested` and the dirty set by posting an InvalidateRect or a foreground redraw; (c) pacing `request_animation_frame` at the display rate (`on_next_frame`); (d) a test-side driver so `TestApp.RunUntilParked`/`AdvanceClock` redraw dirty windows. Upstream motion tests assume (d), for example `requested_animation_frames_resample_without_manual_refresh` (U:motion.rs:1586).

### 0.2 Timers never fire in production (MISSING-GO)
- The scheduler clock is virtual. `clockMs` is advanced only by `advanceClock` (G:executor.go:475-487). Its only caller is `TestApp.AdvanceClock` (G:testapp.go:54). The production dispatcher only calls `sched.run()` (G:win32host_windows.go:2377-2384).
- So `TaskRun.Sleep` (G:executor.go:625) parks forever in a real app, and `App.Now()` (G:imagecache.go:852) / `BackgroundExecutor.NowMs` never move. Tooltip show and grace delays, hover-card delays, toast timeouts, undo grouping, and every motion `now()` sample depend on this. Fix: a wall-clock timer driver in production (for example `time.AfterFunc` → `platformWake`), with the virtual clock kept for tests.

### 0.3 Fluent typing for custom components today
- `authoring.Styled[Self]` is embedded by value and bound with `BindStyled` (authoring/style.go, README). It has only 6 methods: `Flex`, `PaddingX`, `Px3`, `Gap`, `Gap2`, `Opacity`. `StyleRefinement` (authoring/style.go:47, aliased at G:div.go:46) has 5 fields: Display, PaddingL/R, Gap, Opacity.
- `DivElement` has its own concrete fluent methods returning `*DivElement` (G:div.go:678-1179): ID, TrackFocus, KeyContext, Flex/Block/Grid, Absolute, Left/Top/Right/Bottom, W/H/Size/SizeFull, Gap2, P, Justify, AlignItems, Bg, Opacity, TextSize/TextColor, OnClick, OnAction/CaptureAction, OnKeyDown/Up, OnMouseMove/Up, OnScrollWheel, Occlude, TabGroup/TabIndex, DebugSelector, Child/Children, Refine. `Refine` (G:div.go:896-918) maps only the 5 authoring fields.
- No ParentElement or InteractiveElement mixin exists. `ParentElementTyped` (G:element.go:363) is only an interface. Children go through `Child(any)`/`TryIntoElement` (G:element.go:697), which accepts View, RenderOnce, string and AnyElement. There are no `When`/`WhenSome`/`Map` (FluentBuilder).
- `Style` (G:style.go:778-852) is layout-only. It has no background, border color, corner radii, box shadow, cursor, visibility or text props. The div paints its background with a hard-coded `CornerRadii: Corners{}` and draws no border or shadow (G:div.go:1655-1672). The scene and native DLL **do** support radii, borders and shadows (`PaintQuad` G:paint.go:237-244, `BoxShadow` G:paint.go:268, native `SceneShadowRecord` internal/native/scene.go:344). So this gap is Go-only.
- Custom elements can implement `Element[L,P]` (G:element.go:272) and wrap with `CustomElement` (G:element.go:615). But they cannot paint: the frame scene and paint context are reachable only through the unexported `currentFrame(w)`. There is no `Window.PaintQuad`/`PaintShadows` (MISSING-GO).
- Every base component (Popup, Popover, Dialog, Toast, Tooltip, FocusTrapContainer, Root) is "Styled + InteractiveElement + ParentElement delegating to an inner Div". In Go that needs either a full `Styled[Self]` vocabulary plus `Interactive[Self]` and `Parent[Self]` mixins that record into a Div, or forwarding methods per component. This is the single largest porting hazard. Recommended core addition: grow `authoring.Styled`/`StyleRefinement` to the full Styled set, give `DivElement.Refine` full coverage, and add `Interactivity[Self]` and `Children[Self]` mixins.

## 1. Per-module inventory

LOC = total lines, including tests. T = `#[test]`+`#[gpui::test]`. Slice total 10,604 LOC (motion/* +1,112), 147 tests.

| Module | LOC | T | Public items | Tests exercise |
|---|---|---|---|---|
| root.rs | 355 | 1 | `Root`, `RootPlugin`, `Root::{new,register_plugin,plugin,read,update,view}`; actions Tab/TabPrev | plugin registration idempotent, per-window plugin entities |
| global_state.rs | 200 | 2 | `GlobalState` (note_touch, is_touch_press, suppress_text_selection, app_menus, register_deferred_popover, is_in_deferred_context), `DeferredPopover` | init idempotence; Rc-drop closes the deferred context |
| focus_trap.rs | 228 | 0 | `FocusTrapElement::focus_trap`, `FocusTrapContainer`, `active_focus_trap` | (covered by sheet/dialog tests) |
| popup.rs | 325 | 3 | `Popup` (anchor, offset, on_position, content), `POPUP_PRIORITY` | anchor math; content renders after the trigger bounds are captured; occlusion blocks hover underneath |
| positioner.rs | 792 | 15 | `Positioner` (side/corner, placement, align, offset, margin, occlude, on_position), `Align`, `ResolvedPosition` | pure placement math: flip, clamp, align, offset, corner, frame insets, tooltip placement |
| popover.rs | 589 | 6 | `Popover`, `PopoverState` (show/dismiss/is_open), keymap | deferred-context register/unregister, pointer open, outside dismiss, default_open, trigger open state, keyboard Enter |
| dialog.rs | 910 | 6 | `Dialog`, `DialogHandle`, `DialogTrigger/Backdrop/Popup/Title/Description/Close`, `DialogChangeReason` | close button a11y, cancel veto, shared handle, backdrop fills host, popup press doesn't dismiss, centering |
| alert_dialog.rs | 352 | 2 | `AlertDialog*` (wraps Dialog) | backdrop not closable; host style positions popup |
| sheet.rs | 270 | 4 | `Sheet` | overlay close order, non-closable overlay, Escape plus focus-trap registration, dismiss cutoff |
| tooltip.rs | 347 | 3 | `Tooltip`, `TooltipOverlay` (request_show/hide), `TooltipRequest`, `TooltipPositioner`, `TooltipTransition` | grace/switch/dismiss with advance_clock; priority > popup; disabled provider |
| hover_card.rs | 406 | 3 | `HoverCard`, `HoverCardState` | delayed open/close (clock); open-change callbacks; tap mode |
| toast.rs | 1014 | 11 | `Toast`, `ToastStack`, `ToastManager<I,T>`, `ToastOptions/Motion/Advance/StackState/TransitionStatus` | 6 pure manager/geometry tests; 5 windowed (alert role, measurement bootstrap, hover/focus expand, keyed reflow) |
| styled.rs | 285 | 1 | `StyledExt` (refine_style, h_flex, v_flex, paddings, margins, corner_radii, debug_*, font-weight macros), `h_flex()`, `v_flex()`, `box_shadow()`, `RoleOverride` | h_flex/v_flex layout via debug_bounds |
| element_ext.rs | 31 | 0 | `ElementExt::{on_prepaint, text_selection_scope}` | — |
| event.rs | 60 | 0 | `InteractiveElementExt::{lock_scroll_axis,on_double_click}`, `OngoingScrollExt` | — |
| observe.rs | 52 | 1 | `TestSupportExt::test_support`, `ObservedElement` | identity preserved |
| measure.rs | 48 | 0 | `measure`, `measure_if`, `Measure`, `measurement_enabled` | — |
| state_style.rs | 103 | 4 | `StateStyle`; `resolve_style` (crate) | refinement layering order |
| theme.rs | 157 | 1 | `Theme` (global), `ThemeAppearance`, `ScrollbarTheme`, `PlotTheme`, `ResizableTheme` | builder construction |
| theme_tokens.rs | 274 | 1 | `SemanticThemeTokens`, `Color/Radius/Spacing/Typography/Shadow Tokens`, `TextStyleToken` | default light/dark palettes |
| component_traits.rs | 114 | 2 | `Selectable`, `Disableable`, `FocusableExt`, `Collapsible` | default open→selected fallback |
| actions.rs | 39 | 1 | `Confirm{secondary}`, Cancel, Select* actions | payload kept |
| reduce_motion.rs | 244 | 5 | `apply_system_reduce_motion`; `init` | preference application rules; test scheduler skip |
| animation.rs | 397 | 4 | `cubic_bezier`, `ease_*_cubic`, `Lerp`, `EffectTransition` (with_animation) | bezier numerics only |
| motion.rs | 1873 | 43 | `transition`, `transition_with_status`, `animate_keyframes`, `spring`, `Spring`, `Transition`, `TransitionId`, `MotionValue`, `MotionStatus`, `MotionTransform`, `Interpolate` | 12 pure, 31 windowed with advance_clock: status, reversal, keyframes, presence, sequence, spring physics, reduced motion, "stops requesting frames" |
| motion/{easing,keyframes,presence,reveal,sequence,stagger,timing}.rs | 1112 | 1 | `Easing`, `Keyframes`, `Presence`, `MotionReveal` (custom Element), `Sequence`, `Stagger`, `Timing`, `IterationCount`… | easing (1) |
| geometry.rs | 151 | 0 | `Placement`, `Side`, `AxisExt`, `LengthExt`, `Edges<T>` | — |
| index_path.rs | 102 | 3 | `IndexPath` | pure |
| history.rs | 285 | 10 | `History<T>` | pure |
| undo_history.rs | 329 | 14 | `UndoHistory<T>` (uses `Instant` grouping) | pure (time-injected) |
| list_settings.rs | 17 | 0 | `ListSettings` | — |
| async_util.rs | 7 | 0 | re-export smol channel | — |
| lib.rs | 248 | 0 | re-exports; `init(cx)` (Theme, GlobalState, reduce_motion, dialog, focus_trap, popover, sheet, combobox, color_picker, select, number_input, time_field, input, tree, text, root) | — |

## 2. gpui API usage → Go status (per module)

### root.rs
| Upstream use (U:line) | Go status |
|---|---|
| `actions!(root,[Tab,TabPrev])` (11) | PRESENT `DefineUnitAction` G:action.go:483 |
| `KeyBinding::new(..,Some(ctx))`, `cx.bind_keys` (13-20) | PRESENT `NewKeyBinding` G:keymap.go:64, `App.BindKeys` G:input.go:151 |
| `has_global/try_global/global_mut/set_global` (105,151) | `SetGlobal` G:app.go:561, `Global[T]` G:app.go:575 (panics if absent). **MISSING-GO** `HasGlobal[T]`/`TryGlobal[T]`/`DefaultGlobal[T]` (small). global_mut works by storing `*T`. |
| `cx.new`, `cx.weak_entity`, `cx.defer`, `cx.observe`, `entity.update` | PRESENT `NewEntity` G:entity.go:271, `Context.Entity().Downgrade()` G:context.go:34/G:entity.go:507, `Context.Defer` G:context.go:129, `Context.Observe` G:context.go:77, `Entity.Update` G:entity.go:375 |
| `AnyView` + `downcast::<V>()`; `window.root::<Root>()` (178,189) | `View` G:view.go:53, `Window.RootView` G:view.go:351. **MISSING-GO** View→`Entity[T]` downcast (`Window.Root[T]`) |
| `window.focus_next/prev/focused`, `FocusHandle::contains_focused` (201-226) | PRESENT G:focus.go:412/435/373/255 |
| `cx.write_to_clipboard(ClipboardItem::new_string)` (272), `cx.propagate` | PRESENT `App.WriteClipboard` G:clipboard.go:434, `NewClipboardString`; `Context.Propagate` G:key_dispatch.go:1182 |
| `div().id().key_context().on_action(cx.listener(..)).relative().size_full().child(view).absolute().inset_0().children(views).refine_style()` | ID/KeyContext/OnAction/`Context.Listener` (G:context.go:144)/SizeFull/Absolute/Children PRESENT. **MISSING-GO** `Relative`, `Inset0`, full `Refine` (§0.3) |
| `Render`, `Styled for Root` | `Render[S]` G:view.go:35; Styled → §0.3 |
Deps: `crate::input::Copy`, `TextSelectionLayer`/`TextSelection` (E1 slice), `StyledExt`, `focus_trap`. Size **S**. Hazards: plugin factory closures capture entities, so Go needs explicit Release for plugin entities when the window closes; `TypeId` registry → `reflect.Type`.

### global_state.rs
| Use | Go |
|---|---|
| Global set/has/try/global_mut | as above (HasGlobal/TryGlobal MISSING-GO, small) |
| `OwnedMenu` | PRESENT `Menu`, `App.SetMenus` G:app.go:1287 |
| `Rc<()>`/`Weak` strong_count for the deferred-popover token | BASE-LOCAL: explicit `Release()` token or refcount. Hazard: Go has no drop, so PopoverState must release on close and on entity release (`Context.OnRelease` G:context.go:110) |
| `Instant::now` (touch) | Go `time.Now` |
Deps: `text::TextViewState` (E slice). Size **S**.

### focus_trap.rs
| Use | Go |
|---|---|
| custom `Element` wrapping an inner Element (request_layout/prepaint/paint delegation with global id) | PRESENT `CustomElement` G:element.go:615 + `AnyElement.RequestLayout/Prepaint/Paint` G:element.go:629-651 (wrap the Div as AnyElement). Note: DivElement's phase methods are unexported, so delegation must go via AnyElement. |
| `GlobalElementId` as HashMap key | PRESENT `GlobalElementID.String()` G:element.go:126 (key() unexported; String is fine) |
| `FocusHandle.downgrade/WeakFocusHandle.upgrade/contains_focused`, `track_focus` | PRESENT G:focus.go:312, 323, 255; `Div.TrackFocus` G:div.go:685 |
| Interactive/Parent/Styled forwarding | §0.3 mixins (MISSING-GO for ergonomics) |
Size **S**. Hazard: registration happens in the request_layout phase and the global must exist (init).

### popup.rs
| Use (U:line) | Go |
|---|---|
| `window.use_keyed_state((id,"anchor"),..)` (133) | **MISSING-GO** `UseKeyedState`/`UseState`. `WithElementState` G:element.go:1379 exists, but there is no public "current global id" (`pushElementID`/`currentElementIDs` are unexported, G:element.go:1111-1126), no eviction hook to Release the entity (`retainElementStates` G:element.go:1451 just drops the map), and no auto-notify of the current view. |
| tuple ElementId `(id,"anchor")` | **MISSING-GO** (minor) composite ids; `ElementID` has Name/View/Integer only (G:element.go:44-66). Workaround: string concatenation |
| `canvas(prepaint_cb, paint_cb)` (147) | BASE-LOCAL possible (CustomElement + `RequestElementLayout` G:element.go:1530 + bounds in Prepaint). Better as a core `Canvas` (R:elements/canvas.rs, 95 LOC) because the paint closure needs public paint primitives (MISSING-GO) |
| `window.request_animation_frame()` (162) | PRESENT `RequestAnimationFrame` G:imagecache.go:958, but flag-only (§0.1) |
| `deferred(..).priority(100)` (190-198) | **MISSING-GO** core `Deferred` + `Window.DeferDraw` (R:window.rs:4310; deferred prepaint/paint passes R:window.rs:3580-3710 with dispatch-tree parent, element-id/text-style stacks, priority sort). Must live in package gpui (frame internals unexported) |
| `.occlude()` | PRESENT `Div.Occlude` G:div.go:1141 |
| `Bounds::top_center/bottom_left/...`, `Anchor` enum | **MISSING-GO** (trivial) geometry helpers + `Anchor`; Go `Bounds` has only Left/Top/Right/Bottom (G:paint.go:616-619). Could be BASE-LOCAL |
| `debug_selector` (tests) | PRESENT G:div.go:933 |
Deps: positioner, styled. Size **S**. Hazard: two-frame bootstrap (capture bounds, then rAF) needs a working frame loop.

### positioner.rs
| Use (U:line) | Go |
|---|---|
| custom `Element` with child `AnyElement`s | PRESENT (CustomElement, AnyElement phases) |
| `window.request_layout(Style{absolute,flex}, children)` | PRESENT `RequestElementLayout` G:element.go:1530 |
| `window.layout_bounds(id)` (386) | **MISSING-GO** export (`layoutBoundsOf` unexported G:element.go:1324) |
| `window.viewport_size()` (405) | **MISSING-GO** export (`frameDrawState.viewport` unexported G:element.go:949/990) |
| `window.window_decorations()`, `client_inset()` (393-394) | **MISSING-GO** (trivial). On Windows the reference defaults to `Decorations::Server` (R:platform.rs:996), so Go can return Server/none. Could be BASE-LOCAL constant |
| `insert_hitbox(bounds, BlockMouse)` | PRESENT `Window.InsertHitbox` G:mouseevent.go:477, `HitboxBlockMouse` G:mouseevent.go:203 |
| `with_element_offset` | PRESENT `WithElementOffset` G:element.go:1197 |
| `Bounds::from_anchor_and_size`, `Half`, Point min/max | MISSING-GO trivial / BASE-LOCAL |
Deps: geometry (Placement). Size **M** (15 pure tests port directly). Low hazard.

### popover.rs
| Use (U:line) | Go |
|---|---|
| `use_keyed_state` (303) | MISSING-GO (see popup) |
| `cx.focus_handle()` in `PopoverState::new(cx: &mut App)` (47) | `NewFocusHandle(w)` G:window.go:339 needs a window (available at the call site). Hazard: explicit `FocusHandle.Release` G:focus.go:204 |
| `EventEmitter<DismissEvent>`, `window.subscribe(&entity, ..)` with a window in the callback (116) | `DefineEvent` G:event.go:24, `Event.Subscribe` G:event.go:111 (no window param), `DismissEvent` G:view.go:65. **MISSING-GO** (small) `SubscribeIn` (window-bound subscription). Subscription drop → explicit Cancel |
| `window.refresh()` (118) | PRESENT-equivalent `RequestAnimationFrame` (same flag) → §0.1 |
| `window.current_view()` | PRESENT `CurrentView` G:view.go:325 |
| `cx.notify(entity_id)` on App (332,344,370) | **MISSING-GO** `App.Notify(EntityID)` (unexported `App.notify` G:app.go:515) |
| `window.listener_for(&entity, f)` (361) | BASE-LOCAL via `Entity.UpdateIn` G:entity.go:394 closure |
| `.on_mouse_down(button,..)`, `.on_mouse_down_out(..)` (366) | **MISSING-GO** Div interactivity: OnMouseDown, OnAnyMouseDown, OnMouseDownOut, OnMouseUpOut, OnHover. Div has only OnMouseMove/OnMouseUp/OnClick (G:div.go:601-641). The building blocks `Window.OnMouseEvent` G:mouseevent.go:459 and `Hitbox.IsHovered` G:mouseevent.go:249 exist |
| `.role(Role::Dialog)` | **MISSING-GO** `Div.Role`/a11y props builder (divElement reports no role, G:div.go:1289; `A11ySpec` G:accessibility.go:305 not wired to Div) |
| `.tab_group()`, `.track_focus`, `.key_context`, `.on_action` | PRESENT G:div.go (TabGroup, TrackFocus 685, KeyContext 692, OnAction 950) |
| `Focusable` trait, `FocusHandle.focus` | PRESENT `FocusHandle.Focus` G:focus.go:237; ManagedView G:view.go:70 |
| `when/when_some` | MISSING-GO (FluentBuilder) or BASE-LOCAL generic funcs |
Deps: popup, global_state, component_traits(Selectable), actions, styled. Size **M**. Hazards: state entity lifetime (keyed state), subscription/focus handle release, `Option<Subscription>=None` is implicit cancel in Rust → explicit in Go.

### dialog.rs / alert_dialog.rs / sheet.rs
| Use (U:line) | Go |
|---|---|
| `deferred(anchored().position(0,0).child(..)).priority(10+layer)` (dialog 548-643) | **MISSING-GO** `Deferred` + `Anchored` (R:elements/anchored.rs, 398 LOC: request_layout/layout_bounds/viewport/client_inset/with_element_offset). Sheet uses `anchored` without deferred (sheet:123) |
| `window.viewport_size()` (dialog 546, sheet 117) | MISSING-GO export |
| `.on_any_mouse_down` with `event.position/button` (dialog 612, sheet 148) | MISSING-GO Div interactivity |
| `.on_click` + `ClickEvent::default()` | `Div.OnClick` PRESENT; `ClickEvent` has only `Button` (G:div.go:542). **MISSING-GO** ClickEvent fields (position, click_count, modifiers, keyboard-vs-mouse) |
| `window.dispatch_action(Box::new(Cancel))` (dialog 359, alert 135/185) | PRESENT `Window.DispatchBoxed` G:action.go:997 |
| `use_keyed_state("dialog-close-anchor", .. cx.focus_handle())` (dialog 403) | MISSING-GO use_keyed_state |
| `focus_trap`, `test_support`, `Role`, `key_context`, `track_focus` | see above |
| `w(viewport.width).h(..).flex().items_center().justify_center()` | W/H/Flex/AlignItems/Justify PRESENT; `Size0`, `Inset0` MISSING-GO |
| `crate::Button` (DialogClose::trigger) | other slice (B1) |
| `SmallVec`, `Rc<Cell>/RefCell` handles | Go slices / pointer structs |
Sizes: dialog **M**, alert_dialog **S**, sheet **S**. Hazard: `DialogHandle` shared `Rc<Cell<bool>>` semantics translate fine to pointers. The tests need simulated mouse input plus redraw (input dispatch PRESENT `Window.DispatchInput`; the test redraw driver is MISSING-GO, §0.1).

### tooltip.rs
| Use | Go |
|---|---|
| `cx.spawn_in(window, async ..)` + `background_executor().timer(d).await` + `this.update_in` (163-185) | `App.Spawn` G:executor.go:742 + `BackgroundExecutor.Spawn`/`TaskRun.Sleep` G:executor.go:663/625 + `AsyncApp.Await/Update` + `WeakEntity.TryUpdateIn` G:entity.go:616. PRESENT as building blocks, but timers don't fire in production (§0.2). A `Context.SpawnIn` convenience would be nice (optional) |
| `Option<Task>` replace = cancel | `Task.Cancel` G:executor.go:535; hazard: must cancel explicitly |
| `deferred(...).priority(200)` | MISSING-GO Deferred |
| `AnyView` builder, `Role::Tooltip` | View PRESENT; Div.Role MISSING-GO |
| Positioner side placement | positioner |
Size **S**.

### hover_card.rs
| Use | Go |
|---|---|
| spawn_in + timer (138-153) | as tooltip (§0.2) |
| `.on_hover(listener(bool))` (239,263), `.on_mouse_up_out` (261) | MISSING-GO Div interactivity |
| use_keyed_state, listener_for, on_click | as above |
Deps: popup. Size **S**.

### toast.rs
| Use (U:line) | Go |
|---|---|
| `canvas` for measuring item bounds (557) | Canvas (see popup) |
| `cx.refresh_windows()` (543,561,573) | PRESENT `App.RefreshWindows` G:app.go:336 (no-op redraw, §0.1) |
| `window.on_mouse_event(MouseMoveEvent)` (570) | PRESENT `Window.OnMouseEvent` G:mouseevent.go:459 (untyped `event any`) |
| `spring(...)` motion (414-459) | motion.rs |
| `.invisible()`, `.opacity`, `.relative/.absolute/.top/.left/.right_0` | Opacity/Absolute/Top/Left/Right PRESENT; `Invisible` (visibility), `Relative` MISSING-GO |
| `Instant`-based `ToastManager` | Go `time.Time` (pure, injected `now`) |
| Interactive/Styled/Parent impls on Toast/ToastStack | §0.3 |
Size **M** (manager is pure; the stack render is the hard part). Hazard: measurement happens in prepaint and then a refresh; that needs the loop.

### styled.rs / state_style.rs
| Use | Go |
|---|---|
| `Styled` + `StyleRefinement.refine()` (refine_style), `Refineable` | **MISSING-GO** full StyleRefinement + `Refine` (§0.3); `merge` exists only for 5 fields (authoring/style.go) |
| `border_1/border_color/rounded_*/corner_radii/font_weight/flex_col/flex_row/h_full/items_center` | MISSING-GO on Div/Styled; paint for radii and borders is MISSING-GO in div paint (G:div.go:1655-1672) even though `PaintQuad` supports it |
| `BoxShadow`, `Corners`, `Edges`, `hsla` | PRESENT types G:paint.go:268/198/207; Div `Shadow` setter MISSING-GO |
| `FocusHandle` (debug_focused) | PRESENT |
| `Role` (RoleOverride) | `Role` type G:element.go:292 PRESENT |
| inspector reflection macro | skip (debug only) |
Size styled **S** (once core Styled exists), state_style **S**. Hazard: Go generic methods on an embedded mixin must return `Self`. That works with the authoring pattern (Go 1.27 generic methods are already used in core, e.g. G:app.go:595).

### element_ext.rs / event.rs / observe.rs / measure.rs
- element_ext: `canvas` + `ParentElement.child` → Canvas (core or base-local). **S**.
- event.rs: `restrict_scroll_to_axis`/`OngoingScroll::filter` → **MISSING-GO** (no OngoingScroll in Go); `ClickEvent.click_count` → MISSING-GO field (`MouseDownEvent.ClickCount` exists, G:mouseevent.go:122, but it is not carried on ClickEvent). **S**.
- observe.rs: test-only wrapper over `Element<PrepaintState=Option<Hitbox>>`; in normal builds it is the identity. **S** (can be a no-op).
- measure.rs: env var + `tracing` → `log/slog`. **S**.

### theme.rs / theme_tokens.rs / geometry.rs / list_settings.rs
- Global `Theme` (try/has/global_mut) → HasGlobal/TryGlobal MISSING-GO (small). serde/schemars → `encoding/json`; JSON Schema needs a third-party lib or is dropped.
- theme_tokens: `rgb_to_hsla` → `RgbaToHsla` G:paint.go:109; `palette::WithAlpha` → set `.A`; `FontWeight` → Go uses `float32` (G:div.go:84), so a named type is needed (MISSING-GO trivial, or base-local).
- geometry: `Axis` MISSING-GO (trivial; base-local fine), `AbsoluteLength/Length` PRESENT G:style.go:51-155.
- All **S**; pure data.

### component_traits.rs / actions.rs
- Traits → Go interfaces. Default methods (`open` → `selected`) need helper funcs. **S**.
- `#[derive(Action)] Confirm{secondary}` → `DefineAction[Confirm]` G:action.go:465; unit actions → `DefineUnitAction`. PRESENT. **S**.

### reduce_motion.rs
| Use | Go |
|---|---|
| `cx.reduce_motion()/set_reduce_motion` | PRESENT G:app.go:282/287 |
| `default_global/try_global` | MISSING-GO small (as above) |
| Windows `SystemParametersInfoW(SPI_GETCLIENTAREAANIMATION)` | BASE-LOCAL via Go syscall (user32). Core already uses the same proc privately (G:desktop_windows.go:77). **Not native.** |
| test scheduler detection | BASE-LOCAL heuristic (`App.Host()==nil`, G:window.go:455) or a MISSING-GO small `App.IsTest()` |
| macOS objc2 / Linux ashpd portal | out of scope (Windows-only port); stub `None` |
Size **S**.

### animation.rs
| Use | Go |
|---|---|
| `Animation::new(d).with_easing`, `element.with_animation(id, anim, |el,delta|..)` (219-224) | **MISSING-GO** core `AnimationElement`/AnimationExt (R:elements/animation.rs: with_element_state + request_animation_frame + current_view). The building blocks exist (`WithElementState`, `RequestAnimationFrame`, `CurrentView`). But "restyle an arbitrary element per frame" needs the Styled mixin in Go, so it fits better in core (or base can use a closure `func(delta) AnyElement` form) |
| `cubic_bezier`, `Lerp` for f32/Pixels/Point/Hsla | pure Go |
Size **S** (math) plus the core element.

### motion.rs + motion/*
| Use (U:line) | Go |
|---|---|
| `window.use_keyed_state(id, ..)` (290,384,636; presence 52; sequence 144) | MISSING-GO |
| `cx.background_executor().now()` (289,383,634) | PRESENT `App.Now` G:imagecache.go:852 / `NowMs` G:executor.go:678, but virtual clock (§0.2) |
| `cx.reduce_motion()` | PRESENT |
| `window.request_animation_frame()` (355,401,693) | PRESENT flag; loop MISSING-GO (§0.1) |
| `MotionReveal` custom Element: `with_element_state`, `request_layout(style, None)`, `layout_as_root`, `with_content_mask` (reveal 55-116) | PRESENT `WithElementState` G:element.go:1379, `RequestElementLayout` G:element.go:1530, `AnyElement.LayoutAsRoot` G:element.go:654, `WithContentMask` G:element.go:1165 |
| `ElementId`, `SharedString`, `Bounds/Size/Point` Interpolate | PRESENT types (composite ids minor) |
| tests: `TestAppContext` + `advance_clock` + `window.update(.. refresh)` + "requests frame" assertions | `TestApp.AdvanceClock` G:testapp.go:54, `NewTestWindow` G:testwindow.go:45, `DrawWindowFrame`, `Window.RefreshRequested` G:focus.go:531 PRESENT; auto-redraw on park MISSING-GO (or the tests call DrawWindowFrame manually) |
Size **L** (about 3,000 Rust lines → roughly 2,000-2,500 Go lines; 44 tests). Hazards: generic `Interpolate` over many types (Go generics fine), `Rc<dyn Fn>` easing closures → Go func values, keyed-state lifetime.

### index_path.rs / history.rs / undo_history.rs / async_util.rs / list_settings.rs
Pure. `ElementId` from IndexPath → `NameElementID(string)` (composite id minor). undo_history uses `web_time::Instant` → `time.Time`. async_util → Go channels (drop). All **S**; 27 pure tests port directly.

### lib.rs init
`init(cx)`: Theme global, GlobalState, reduce_motion, dialog/focus_trap/popover/sheet keymaps, plus other slices (combobox, color_picker, select, number_input, time_field, input, tree, text, root). Go: one `base.Init(app)`, **S**.

## 3. Third-party crates used by this slice
smallvec (→ slices), web_time (→ `time`), palette (WithAlpha only → trivial), serde/serde_json (→ `encoding/json`), schemars (JSON Schema: drop or add a lib), smol/async_channel (→ chan), tracing (→ `log/slog`), windows (SystemParametersInfoW → `syscall`/`x/sys`), objc2/ashpd (mac/Linux only → skip). None of them needs native code.

## 4. Port size summary
S: root, global_state, focus_trap, popup, alert_dialog, sheet, tooltip, hover_card, styled, element_ext, event, observe, measure, state_style, theme, theme_tokens, component_traits, actions, reduce_motion, animation, geometry, index_path, history, undo_history, list_settings, async_util, lib.
M: positioner, popover, dialog, toast.
L: motion + motion/*.
Slice estimate: about 7-9k Go lines, plus the core additions below (the deferred/anchored/canvas/animation elements, use_keyed_state, the frame loop and the Styled/Interactivity vocabulary are probably 3-5k Go lines in package gpui).

## 5. MISSING-GO core additions (deduplicated)
1. **Frame loop**: notify → dirty-window tracking; consume `refreshRequested`/`RefreshWindows` (`DrawHook` stub) → InvalidateRect/redraw; display-rate `request_animation_frame` plus `Window.OnNextFrame`; public `Window.Refresh`; test-side auto-draw on `RunUntilParked`/`AdvanceClock`.
2. **Production wall-clock timers** for `TaskRun.Sleep`/`App.Now` (the virtual clock only advances in tests).
3. **Deferred element + `Window.DeferDraw(priority)`** with deferred prepaint/paint passes (in-package only).
4. **Anchored element**, and exports `Window.ViewportSize()`, `LayoutBounds(w,id)`, `WindowDecorations()/ClientInset()` (Server/none on Windows).
5. **Canvas element** (prepaint/paint closures) and **public paint primitives for custom elements** (`PaintQuad`, `PaintShadows`, …; today only the unexported `currentFrame`).
6. **AnimationElement / AnimationExt** (`with_animation`, `Animation{duration,easing,repeat}`).
7. **`UseKeyedState`/`UseState`** (+ a public current-global-id `WithGlobalID`), plus an element-state eviction hook that Releases keyed entities and an auto-notify of the current view.
8. **Div interactivity**: OnMouseDown(button), OnAnyMouseDown, OnMouseDownOut, OnMouseUpOut, OnHover, hover/active/focus style refinements, Role/a11y props on Div; **ClickEvent** fields (position, click_count, modifiers, keyboard flag); `OngoingScroll`/`RestrictScrollToAxis`.
9. **Full Styled vocabulary**: `StyleRefinement` covering all Style fields plus background/border color/corner radii/box shadow/visibility/text props; `DivElement.Refine` full coverage; div paint of radii/borders/shadows; missing setters (Relative, Inset0, Size0, FlexCol/Row, Border*, Rounded*, Shadow, Overflow*, Invisible, FontWeight, W/HFull, Min/Max sizes, margins/paddings). Plus **component mixins**: grow `authoring.Styled[Self]` to the full set, add `Interactivity[Self]`/`Children[Self]` mixins and FluentBuilder (`When`/`WhenSome`/`Map`).
10. **Small API gaps**: `HasGlobal/TryGlobal/DefaultGlobal[T]`; `App.Notify(EntityID)`; View→`Entity[T]` downcast / `Window.Root[T]`; `Event.SubscribeIn` (window-bound callback); composite ElementIDs; `Anchor` enum + Bounds helpers (top_center, from_anchor_and_size…); `Axis`, `FontWeight` named type; optional `Context.SpawnIn`, `App.IsTest()`.

## 6. MISSING-NATIVE
**None found for this slice.** Evidence:
- The Win32 host (windows, messages, invalidation, SystemParametersInfoW) is Go `syscall` code (G:win32host_windows.go, G:desktop_windows.go:77), not the DLL. So the frame loop, timers and reduce-motion are Go work.
- The scene the DLL renders already carries rounded/bordered quads and shadows (G:scene.go:39-59; internal/native/scene.go:344 `SceneShadowRecord`). Element opacity is applied Go-side. So the styling gaps are Go-only.
- Hit-testing, focus, actions, keymaps and the layout requests used by Positioner/Anchored are Go-side and exported (or need only exports).
- The only DLL-adjacent risk: deferred draws need z-ordered painting after the main tree. The Go `Scene` already orders by insertion/`Order` (G:scene.go:41), so no native change is expected. Verify when implementing (1) and (3).
- Second-pass check: the native scene **already exports** the deferred-draw order floor: `native.Scene.RaiseOrderFloor` (internal/native/scene.go:864-867), wrapped by `SceneKernel.RaiseOrderFloor` (G:scene_kernel.go:137) and `Scene.RaiseOrderFloor` (G:scene.go:354). It is just never called from a deferred pass (no `defer_draw` in G:render.go `drawRoots` 184-232). So item 3 is purely Go.
- Frame pacing: the reference `on_next_frame` → `platform_window.schedule_frame()` + `invalidator.wake_platform()` (R:window.rs:2613-2619), and `request_animation_frame` = `on_next_frame(notify current_view)` (R:window.rs:2633-2636). On Windows the host side of that (WM_PAINT, timers, DwmFlush pacing) is all Go syscalls in this port, so it is MISSING-GO, not native.
- `use_keyed_state` semantics to replicate (R:window.rs:4093-4114): with_global_id + with_element_state holding an `Entity<S>`, `cx.observe(new_state → notify(current_view)).detach()` on creation, and the entity drops when element state is not re-accessed in the next frame. Go's `retainElementStates` (G:element.go:1451-1459) silently drops unaccessed state, with no release hook. That is the eviction hook in item 7.
