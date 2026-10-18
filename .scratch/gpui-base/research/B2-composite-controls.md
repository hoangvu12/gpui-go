# B2 — Composite controls (pagination, select, combobox, tree, nav_stack, resizable, calendar, date_picker, time_field, color_picker, number_input, otp_input, questionnaire)

Upstream root: `upstream-base/ce/crates/base/src` (commit c0820693). Go root: `gpui-go/gpui`. Native ref: `gpui-go/reference/native/src`.
Slice total: 10,959 upstream LOC (about 3,000 of that is tests).

## 0. Cross-cutting findings (these apply to most modules)

| # | Finding | Status | Evidence |
|---|---|---|---|
| X1 | `DivElement` has no accessibility builders (`role`, `aria_label`, `aria_value`, `aria_expanded`, `aria_selected`, `aria_toggled`, `aria_numeric_value`, `aria_position_in_set`/`size_of_set`, `on_a11y_action`). In Go, a11y comes only from custom elements implementing `A11yCompanion`/`A11yActions[P]` that are wrapped by `Accessibility[L,P]`. | MISSING-GO | `gpui/accessibility.go:305-331` (`A11ySpec`: Role/Label/Description/Value only), `:358,377,441`. `gpui/div.go` has 66 methods and none of them is a11y. |
| X2 | Roles outside the native 8 cannot be published: Navigation, ComboBox, Tree, TreeItem, RadioButton, SpinButton, TimeInput. Publishing such a role aborts the whole publish. | MISSING-NATIVE | `reference/native/src/a11y.rs:598-612` `role_from_tag` accepts tags 1-8 only (else `ERR_BAD_VALUE`). `internal/native/a11y.go:33-47`. `gpui/a11yprovider.go:545-566` `a11yRoleTag` errors "has no native tag", and `:673-675` fails publish. |
| X3 | States cannot be published: expanded, selected, toggled/checked, numeric value, position-in-set and size-of-set. | MISSING-NATIVE | The `A11yNodeRecord` flags are only `0x1` hidden and `0x2` has-bounds (`internal/native/a11y.go:84-87,106-127`). The decoder (`reference/native/src/a11y.rs:816-870`) sets only label, description, value, author_id, hidden, bounds, actions and children. Even `CheckBox` cannot carry a toggled state. |
| X3b | The Click action IS advertisable. The native decoder reads the mask as tag-presence, so all 9 tags work (`a11y.rs:845-856`), and Go encodes `1<<(tag-1)` (`gpui/a11yprovider.go:601-606`). The header comment at `a11yprovider.go:62-70` is stale: it says only Focus, Increment and SetTextSelection are representable. Select/ColorPicker `on_a11y_action(Click)` therefore needs only Go work. | doc hazard | as cited |
| X4 | `NewFocusHandle(w *Window)` needs a window, while upstream `cx.focus_handle()` is App-level. State constructors (`TreeState::new`, `QuestionnaireState::new`, `CalendarState::new`, …) must take `*Window`, or core adds an App allocator. | API-shape hazard / optional MISSING-GO | `gpui/window.go:339` |
| X5 | `DivElement.OnMouseDown` is missing. `OnMouseUp`/`OnMouseMove` exist, and `MouseDownEvent` exists. | MISSING-GO (small) | `gpui/div.go:1064,1073`, `gpui/mouseevent.go:114` |
| X6 | Thin fluent style API. `authoring.StyleRefinement` has only Display, PaddingL/R, Gap and Opacity. There are no Div setters for flex direction, grow/shrink/basis, min/max size, per-side padding, `w_full`/`h_full`, `relative`, gap_x/y or font weight. The `Style` fields and the layout path already carry all of these. | MISSING-GO (shared with the styled researcher) | `gpui/authoring/style.go:47-53`, `gpui/div.go:896` `Refine`, `gpui/style.go:829` `FlexDirection`, `gpui/layout.go:331-338` |
| X7 | `ElementID` is limited to string, integer and view ids. There are no tuple ids (`("name", ix)`/`(name, entity_id)`), and `Div.ID` takes only a string. Workaround: `fmt.Sprintf`. | minor MISSING-GO | `gpui/element.go:45-85`, `gpui/div.go:678` |
| X8 | There is no exported Interactivity or InteractiveElement passthrough. Base wrappers (CalendarItem, TimeFieldSegment, DatePicker, Pagination) must embed or forward to `*DivElement`. | design / MISSING-GO | — |
| X9 | Test harness. PRESENT: `NewTestApp`, `AdvanceClock`, `RunUntilParked` (`gpui/testapp.go:25,50,54`), `NewTestWindow` (`testwindow.go:45`), `DrawWindowFrame` (`render.go:58`), `Window.DispatchKeyEvent` (`key_dispatch.go:728`), `Window.DispatchInput` (`filedrop.go:379`), `ActivateWindowA11y` (`accessibility.go:659`). MISSING-GO sugar: `AddWindowView`, `SimulateKeystrokes`, `SimulateMouse*`, and a per-element `write_a11y_info` (use the snapshot instead). | test-only MISSING-GO | — |

These building blocks are PRESENT and used everywhere:

| Building block | Go symbol |
|---|---|
| Actions | `DefineAction`/`RegisterActions` (`action.go:465,804`), `DivElement.OnAction[A]` (`div.go:950`) |
| Key bindings | `NewKeyBinding` (`keymap.go:64`), `App.BindKeys` (`input.go:151`), `Div.KeyContext` (`div.go:692`) |
| Focus | `Div.TrackFocus` (`div.go:685`), `FocusHandle.TabStop/TabIndex/Focus/IsFocused` (`focus.go:169,181,237,245`) |
| Propagation | `App.Propagate`/`StopPropagation` (`key_dispatch.go:1174-1186`), `Window.PreventDefault`/`DefaultPrevented` (`mouseevent.go:511,520`) |
| Entities | `Entity.Read/Update/UpdateWith/UpdateIn/UpdateInWith/EntityID` (`entity.go:259-404`) |
| Events | `DefineEvent`/`Emit`/`Subscribe`/`Subscription.Detach` (`event.go:24,36,111,190`) |
| Context | `Context.Notify/Observe/Listener/Defer` (`context.go:65,77,144,129`), `App.Defer` (`app.go:326`), `App.ReduceMotion` (`app.go:282`) |
| Elements | `Empty()` (`element.go:806`), `ViewOf` (`view.go:135`), `Div.OnClick/OnKeyDown/CaptureKeyDown/Occlude/DebugSelector` (`div.go:940,982,992,1141,933`) |

## 1. Per-module tables

### pagination.rs: 281 LOC, 4 tests (2 `#[test]`, 2 `#[gpui::test]`)

- **Public items:** `PaginationItem{Page,Ellipsis(Range)}`; `PaginationState` (visible_pages, on_change, previous/next/request_page, items, …); `Pagination` (new, accessibility_label, state; Styled, Parent, Interactive, RenderOnce).
- **Tests:** page clamping and the ellipsis algorithm; `request_page` guards for same page, out of range and disabled; an a11y Navigation node with a label.

| API | Upstream | Go |
|---|---|---|
| `div().id`, children, refine | :115,133,159 | PRESENT `Div()` `div.go:671`, `ID`, `Children`, `Refine` |
| `interactivity()` passthrough | :145-151 | X8 |
| `role(Role::Navigation)` | :156 | X1 + X2 (MISSING-NATIVE) |
| `aria_label` | :157 | X1 (`A11ySpec.Label` exists) |
| `write_a11y_info` (test) | :275 | X9 |

- **Third-party:** none. **Base deps:** styled.
- **Size:** S (about 180 lines). **Hazard:** only the a11y role.

### select.rs: 507 LOC, 6 `#[gpui::test]`

- **Public items:** `init`; `Select` (open, disabled, focus_handle, content_focus_handle, accessibility_label/value, on_open_change, on_dismiss, on_confirm).
- **Tests:**
  - Down opens the select and focuses the content; Enter opens it.
  - Escape closes it and restores trigger focus; dismiss runs before close.
  - A disabled select ignores keys.
  - The a11y projection exposes label, value, expanded and Click.

| API | Upstream | Go |
|---|---|---|
| `KeyBinding` + `bind_keys` (context "Select") | :17-27 | PRESENT |
| `role(ComboBox)` / `aria_expanded` | :196-203 | X1 + X2/X3 MISSING-NATIVE |
| `aria_label` / `aria_value` | :197-203 | X1 (native carries label and value) |
| `track_focus(tab_stop)`, `key_context` | :204-207 | PRESENT |
| `on_a11y_action(Click)` | :216 | X1. `A11yActions`/`A11yActionClick` exist, and native supports Click |
| `on_action` SelectUp/Down/Confirm/Cancel; propagate/stop | :230-302 | PRESENT |
| `focus`/`is_focused` | :190,226 | PRESENT |

- **Third-party:** none. **Base deps:** actions, styled, observe/test_support.
- **Size:** S-M (about 280 lines plus tests).
- **Hazards:**
  - Confirm calls `propagate()` before the callback.
  - Confirm has a `secondary-enter` binding. The Go parser supports `secondary-` (`keystroke.go:8,192`), so this is PRESENT.
  - The order dismiss, then close, then focus is pinned by tests.

### combobox.rs: 246 LOC, 2 `#[gpui::test]`

- Thin wrapper over `crate::Select`. Its own bindings (`:17-27`) are PRESENT.
- **Tests:** Escape order; Enter confirms without dismissing.
- **Third-party:** none. **Base deps:** select, actions, styled.
- **Size:** S (about 100 lines). **Hazard:** inherits select's a11y gaps.

### tree.rs: 715 LOC, 6 tests (1 `#[test]`, 5 `#[gpui::test]`, state-only)

- **Public items:** `init`, `key_context`, `TreeItem`, `TreeEntry`, `TreeEvent{Expanded,Collapsed}`, `TreeEntryState`, `TreeState` (set_items, selection, scroll_to_item, reveal_item, focus; Render, EventEmitter), `Tree` (item, list_style).
- **Tests:**
  - Shared state between clones.
  - Flattening.
  - Selecting a hidden item expands its ancestors.
  - `reveal_item`.
  - A folder toggle rebuilds the entries.
  - Events fire, and `set_items` is silent.

| API | Upstream | Go |
|---|---|---|
| `cx.focus_handle()` on `&mut App` | :206 | X4 |
| `UniformListScrollHandle`, `scroll_to_item`, `ScrollStrategy::Top/Bottom` | :194,268,424,437 | PRESENT `list.go:59,78`, `scroll.go:19` |
| `uniform_list(id,count,processor)` + `track_scroll` | :451,489 | PRESENT in another shape: `UniformList(handle,count,render)` `list.go:124` |
| `refine_style` on uniform_list | :490 | MISSING-GO: `UniformListElement` has only W/H/SizeFull/WithSizingBehavior (`list.go:134-155`) |
| uniform-list wheel scroll | implicit | MISSING-GO (confirmed). `list.go:16-18` says wheel plumbing is pending, and `list.go`/`listelement.go`/`scroll.go` contain no ScrollWheel, OnMouseEvent or InsertHitbox. Shared with the virtual_list researcher |
| `div().id(ix)` | :461 | X7 |
| `Role::Tree/TreeItem`, `aria_selected/expanded` | :463-467,548 | X1 + X2/X3 MISSING-NATIVE |
| `on_mouse_down(Left/Right)` | :471-483 | X5 (`MouseButtonRight` exists, `mouseevent.go:59`) |
| `cx.listener`/`listener_for`, emit/subscribe, `.child(entity)` | :473,551,575 | PRESENT (`Context.Listener`, `event.go`, `ViewOf`) |

- **Third-party:** none. **Base deps:** actions, styled, observe.
- **Size:** M (about 450 lines plus about 250 lines of tests).
- **Hazards:**
  - The render path mutates state while the list callback reads the entity. Avoid an Update inside a Read.
  - The list measures item 0 every frame.

### nav_stack.rs: 833 LOC, 7 `#[gpui::test]`

- **Public items:** `NavOperation`, `NavMotion`, `NavStackEvent{Pushed,Popped,Forwarded,Replaced,Cleared}`, `NavStackState` (push, pop, pop_to_root, forward, replace, clear, views, …), `NavStack` (transition, item), `NavPage`.
- **Tests:**
  - Stack semantics, and forward entries surviving until the next push.
  - An immediate change superseding the running one.
  - The outgoing view being dropped after its exit transition (`advance_clock` + draw).

| API | Upstream | Go |
|---|---|---|
| `AnyView`, `entity_id`, view `==` | :56,89,453 | PRESENT `View` (`view.go:53`). Compare by EntityID |
| emit/notify/subscribe | :98,251,279 | PRESENT |
| `cx.reduce_motion()` | :349 | PRESENT `App.ReduceMotion` `app.go:282` |
| `relative/absolute/inset_0` | :437,526 | PRESENT (`Absolute`, `Left`/`Top`/… in `div.go:756-781`). `Relative`/`Inset0` fall under X6 |
| `.occlude()` | :447 | PRESENT `div.go:1141` |
| `advance_clock`, draw (tests) | :799-805 | PRESENT |

- **Third-party:** none. **Base deps:** history (`History<T>`), motion (`Presence`, `PresencePhase`, `Transition`), styled.
- **Size:** M (about 400 lines plus about 350 lines of tests).
- **Hazards:**
  - It depends on motion `Presence` being keyed by ElementID and driven by the test clock.
  - The render path mutates state (`transit = None`).

### resizable/*: mod 786, panel 489, resize_handle 772 (2,047 LOC); 12 `#[gpui::test]` + 3 `#[test]`

- **Public items:** `PANEL_MIN_SIZE`, `h_resizable`/`v_resizable`/`resizable_panel`, `ResizableState` (sizes, resize/insert/remove/reset_panel, clear), `ResizablePanelGroup`, `ResizablePanel`, `ResizablePanelEvent`, `ResizeHandle*` (renderer, context, state, `HandleEdge`). `resize_handle(...).inside(edge)` and `adopt_sizes` are used by dock.
- **Tests:**
  - A mixed fixed/flex layout is stable on the follow-up frame.
  - Caller-owned state settles on the same frame.
  - Insert, reset, remove and clear.
  - A drag gives a 220/180 split, with `on_resize` called once.
  - The handle stays idle when occluded.
  - The Idle→Hovered→Pressed→Dragging state machine.
  - The handle paints under an earlier `deferred` popover.
  - Theme colour fallbacks.

| API | Upstream | Go |
|---|---|---|
| `Axis` / `Size::along` | mod.rs:3,207 | MISSING-GO: no `Axis` in `gpui/` (trivial; could live in base) |
| `Bounds.left/top`, Pixels math | panel.rs:457 | PRESENT `paint.go:616-619` (float32) |
| `window.use_keyed_state` | panel.rs:140 | MISSING-GO: no `UseKeyedState` anywhere in `gpui/` |
| `window.defer` | panel.rs:195 | PRESENT `App.Defer`/`Context.Defer` |
| `window.refresh()` | resize_handle.rs:396,419,441 | MISSING-GO: only the unexported `requestRefresh` exists |
| `window.on_mouse_event` (Move/Up/Down + phase) | panel.rs:441-487 | PRESENT `Window.OnMouseEvent` `mouseevent.go:459` |
| `insert_hitbox`, `HitboxBehavior::Normal`, `is_hovered` | resize_handle.rs:364,393 | PRESENT `mouseevent.go:477,199,249` |
| custom `Element` + `request_layout`/`with_element_state` | panel.rs:397,416; rh:210,241 | PRESENT `element.go:272,615,1379,1530` |
| div `on_prepaint(bounds)` | panel.rs:175,354 | MISSING-GO: no `OnPrepaint` (base `element_ext`; needs a core hook or a custom element) |
| `cursor_col_resize`/`cursor_row_resize` per hitbox | rh:264-304 | MISSING-GO: `CursorResizeColumn`/`Row` exist (`desktop.go:60-69`), but only as the global `App.SetCursorStyle` (`app.go:993`). There is no per-hitbox cursor resolution. Not native: `SetCursorStyle` is pure-Go Win32 (`desktop_windows.go:71,479`) |
| `group`/`group_hover` | rh:251,334 | MISSING-GO (can be dropped; it restates the same colour) |
| div `on_drag` + drag view | panel.rs:370; rh:253 | MISSING-GO: drag bookkeeping exists (`filedrop.go:166-219`), but there is no `Div.OnDrag` and no preview painting. Avoidable with a window MouseDown listener plus `IsHovered` |
| flex/min/max/padding setters, `Styled` refine | panel.rs:317-353 | X6 |
| `deferred(...)` (test only) | rh:475 | MISSING-GO (popover infrastructure) |

- **Third-party:** none. **Base deps:** styled (`AxisExt`, `StyledExt`, `h_flex`/`v_flex`), element_ext (`on_prepaint`), theme (`resizable.handle`/`active_handle`, `border`/`ring` tokens). Reverse dependency: dock.
- **Size:** M (about 800 lines plus about 600 lines of tests).
- **Hazards:**
  - The settle-frame protocol depends on when prepaint feedback, deferred notify and Go frame scheduling run relative to each other.
  - Hitbox order and occlusion.
  - The drag is captured at paint time, so it takes effect one frame late.
  - Exact float32 arithmetic (the 220/180 split).
  - Without per-hitbox cursors the resize cursor silently disappears.

### calendar.rs: 1,078 LOC, 8 `#[gpui::test]` + 1 `#[test]`

- **Public items:**
  - `Date{Single,Range}` (format(strftime), is_in_range, …).
  - Matchers: `Matcher{DayOfWeek,Interval,Range,Custom}`, `IntervalMatcher`, `RangeMatcher`.
  - `CalendarView`, `CalendarEvent::Selected`.
  - `CalendarState` (about 35 methods: set/select/activate date, month and year paging, views, today), `CalendarItemKind`/`ItemState`/`Item`.
  - `Calendar` (number_of_months, first_day_of_week, item and label slots).
- **Tests:**
  - a11y labels in all 3 views.
  - Range restart and complete.
  - Disabled dates.
  - Year wrap.
  - Six-week months.
  - View transitions.
  - Grid constants.
  - Year page bounds.
  - Emit only when the value is complete.

| API | Upstream | Go |
|---|---|---|
| Entity, emit, subscribe, notify, Empty | 264-830 | PRESENT |
| `cx.focus_handle()` | 186 | X4 |
| `div().id`, `track_focus`, `on_click`, children | 503-824 | PRESENT |
| Interactivity passthrough | 535 | X8 |
| `grid()`/`grid_cols` | 734 | PRESENT `Grid`, `GridTemplateCols` (`div.go:727`). The shorthand is trivial |
| `gap_x`, `font_medium`, `flex_col` (v_flex) | 700-734 | X6 (`TextStyleRefinement.FontWeight` exists, `div.go:123`) |
| `aria_label` | 512,775 | X1 (label is publishable once a role exists) |
| `write_a11y_info` + `accesskit::Node::label` (test) | 874 | X9 |

- **Third-party:** chrono (`NaiveDate`, `Weekday`, `Datelike`, `Local::now().date_naive()`, `Duration::days`, `.format(strftime)`). Go: stdlib `time` plus a hand-written `NaiveDate{Y, M, D}` value type (about 80 lines). Do not use `cloud.google.com/go/civil`.
- **Third-party question:** the public `Date::format(&str)` takes a user strftime string. Either hand-write a subset translator (about 60 lines) or switch the API to Go layouts.
- **Base deps:** styled, observe/test_support.
- **Size:** M (about 750 lines plus about 300 lines of tests).
- **Hazards:**
  - `Matcher::Custom(Fn)` means `==` is lost.
  - Calendar render updates state (`set_number_of_months`).

### date_picker.rs: 112 LOC, 0 tests

- `DatePicker` (new(id, focus), open, disabled, on_open_change). It is a trigger shell.

| API | Upstream | Go |
|---|---|---|
| `role(ComboBox)`, `aria_expanded` | 85-86 | X1 + X2/X3 MISSING-NATIVE |
| `tab_stop`, `track_focus`, `on_action` Confirm/Cancel, `propagate` | 87-108 | PRESENT |

- **Third-party:** none. **Base deps:** actions, styled.
- **Size:** S (about 120 lines). Its only semantic payload is the a11y role and expanded state.

### time_field.rs: 775 LOC, 10 `#[test]` (pure `SegmentEditor`)

- **Public items:** `TimePrecision` (format returns a chrono format string; truncate), `HourCycle{H23,H12}`, `TimeSegment`, `TimeFieldEvent::Change(NaiveTime)`, `TimeFieldState`, `TimeFieldSegment(State)`, `TimeField`. A crate-private `init` binds 8 keys.
- **Tests:**
  - Digit typing and auto-advance.
  - Out-of-range digit restarts.
  - Wrap without carry.
  - Precision truncation.
  - Clearing.
  - 12h labels, typing, period and stepping.

| API | Upstream | Go |
|---|---|---|
| `KeyBinding`, `key_context`, `on_action` ×7, `on_key_down` | 21-30,591-599 | PRESENT |
| `window.listener_for` | 592 | MISSING-GO convenience (work around with `Entity.UpdateIn`/`Context.Listener`) |
| `Keystroke.modifiers.modified()`, `key` | 418 | PRESENT `keystroke.go:55,132,512` |
| `prevent_default`, `stop_propagation`/`propagate` | 405-432 | PRESENT |
| `on_mouse_down(Left)` | 622 | X5 |
| `role(TimeInput)`, `aria_value` | 587-588 | X1 + X2 MISSING-NATIVE |

- **Third-party:** chrono `NaiveTime`/`Timelike` plus 4 fixed formats. Go: a hand-written `NaiveTime{H, M, S}` (about 40 lines) plus `fmt.Sprintf`. The `TimePrecision::format` API must change.
- **Base deps:** number_input (`Increment`/`Decrement`), actions (SelectLeft/Right/Next/PrevColumn), input (`Delete`), styled, observe.
- **Size:** M (about 550 lines plus about 250 lines of tests).
- **Hazard:** Tab/Shift-Tab actions `propagate()` at the ends. They must fall through to Go's focus traversal.

### color_picker.rs: 999 LOC, 3 `#[test]` + 6 `#[gpui::test]`

- **Public items:** `ColorPickerEvent::Change(Option<Hsla>)`, `HslaSliders`, `ColorPickerState` (value, preview, hex input, commit_hex, select/update color, open, tabs), `ColorPicker`, `ColorSwatch`. Crate-private: `init`, `parse_hex`.
- **Tests:**
  - Hex parsing at widths 3/4/6/8; alpha appears in hex only when translucent.
  - The default value is synced to the hex field and the sliders.
  - A preview does not change the committed value; invalid hex is a no-op.
  - A palette pick closes the picker, but a slider update keeps it open.
  - Open state and tab are controlled.
  - Keystroke toggle and dismiss.

| API | Upstream | Go |
|---|---|---|
| `Hsla`, `Rgba` | :4-8 | PRESENT `paint.go:43` (`H` is a 0..1 fraction), `glyph.go:89` |
| Rgba→Hsla (`into_color`) | :58-66 | PRESENT `RgbaToHsla(hex uint32)` `paint.go:109` (bit-exact palette port). A float-input variant is MISSING-GO (small) |
| Hsla→Rgba | :71 | MISSING-GO: export the unexported `hslaToRgba` (`draw_text.go:513`) |
| `hsla()` clamping ctor | :151 | MISSING-GO (small) |
| `cx.subscribe_in` (window-carrying) | :200,232 | MISSING-GO: only `Subscribe`/`SubscribeSelf` exist (`event.go`) |
| `on_hover(bool)` | :771 | MISSING-GO: there is no `Div.OnHover` |
| `window.use_keyed_state` | :744 | MISSING-GO (same as resizable) |
| `role(Button/RadioButton)`, `aria_expanded`/`selected`/`toggled` | :571-760 | X1. RadioButton and the states are MISSING-NATIVE (X2/X3). Button is publishable |
| bind_keys, on_action, propagate, on_click, tab_index | various | PRESENT |

- **Third-party:**
  - palette: only Srgb↔Hsl and `RgbHue::into_degrees`, used through the core re-exports. It is already hand-ported in Go, so it only needs exporting.
  - regex `^#[0-9a-fA-F]{0,8}$` maps to stdlib `regexp`.
  - smallvec maps to a slice.
- **Base deps:** input (`InputState`, `.pattern(Regex)`), slider, actions, styled.
- **Size:** M (about 700 lines).
- **Hazards:**
  - `into_degrees()` is signed (-180,180], so upstream writes a negative hue into a slider with min 0. Port it as `deg := H*360; if deg > 180 { deg -= 360 }`.
  - The `(v*255) as u32` cast saturates in Rust. Go needs an explicit clamp.
  - The `suppress_input_change` guard relies on the input emitting synchronously.

### number_input.rs: 489 LOC, 1 `#[gpui::test]` + 1 `#[test]`

- **Public items:** actions `Increment`/`Decrement`, `StepAction`, `NumberStep{Fixed,ByValue}`, `NumberInputEvent::Step`, `NumberInput`, `NumberInputText`, `step_value()`. It also adds `apply_number_step` on `InputState` and reaches into input internals (`input/base/state.rs:401-403,1247,3512`).
- **Tests:**
  - Mouse down/up on the decrement button keeps focus in the editor.
  - `step_value` precision ("0.1"+0.2 gives "0.3"), clamping and None.

| API | Upstream | Go |
|---|---|---|
| `actions!`, `bind_keys`, `on_action`, `key_context`, `track_focus`, `propagate`, emit | :13-323 | PRESENT |
| `role(SpinButton)`, `aria_numeric_value` | :304-305 | X1 + X2/X3 MISSING-NATIVE |
| `flex_col`, `flex_1`, `min_w_0`, `h_full`, `flex_none` | :150-296 | X6 |
| simulated mouse events (test) | tests | X9 (`DispatchInput` present) |

- **Third-party:** none. `rust_decimal` and `num-traits` are not used here; they appear only in `plot/scale*`. Go: `strconv.ParseFloat`, and `FormatFloat(v,'f',-1|N,64)`.
- **Base deps:** input (InputState internals), button (`focusable(false)`), styled, theme.
- **Size:** S-M (about 350 lines).
- **Hazards:**
  - `ParseFloat` accepts hex floats and Go "Inf" spellings, which Rust rejects. Pre-validate.
  - `apply_number_step` needs a seam in the Go input package.

### otp_input.rs: 237 LOC, 2 `#[test]`

- **Public items:** `OtpEvent{Change,Complete,Focus,Blur}`, `OtpState` (length, value, masked, cursor_visible, focus), `OtpInput`.
- **Tests:** `edit_value` handles backspace, full-width digit mapping, the length cap, and ignoring non-digit keys.

| API | Upstream | Go |
|---|---|---|
| `cx.observe(&blink)` | :34 | PRESENT `Context.Observe` |
| `cx.observe_window_activation`, `is_window_active` | :35-36 | `Window.IsActive` PRESENT (`window.go:288`). The observer is MISSING-GO; the host already delivers `WindowOptions.OnActivate` (`window.go:103`, `win32host_windows.go:588`) |
| `cx.on_focus`/`on_blur(handle)` | :40-41 | MISSING-GO: only `Window.OnFocusLost` exists (`focus.go:486`) |
| `on_key_down`, `key`/`key_char`, `prevent_default`, `stop_propagation` | :124-203 | PRESENT `keystroke.go:141-143` |
| `listener_for` | :203 | MISSING-GO convenience |

- **Third-party:** none (`unicode/utf8`; use rune counts).
- **Base deps:** input::blink_cursor, styled.
- **Size:** S (about 200 lines).

### questionnaire/*: mod 14, control 121, keyboard 104, state 1,703, types 698 (2,640 LOC); 1 `#[test]` + 5 `#[gpui::test]`

- A headless state machine: answers, validation, navigation, skip and submit, focus, shortcuts and events.
- **Public items:** `QuestionnaireChoiceControl`, `handle_key_down`, `QuestionnaireState` (about 50 methods), and about 20 types/builders/events/errors in types.rs.
- **Tests:**
  - Schema errors.
  - Validate, navigate, skip and the Completed→Submit order.
  - The input draft is kept separate, silent setters and reset (`run_until_parked`).
  - Submit jumps to the first invalid item.
  - Disabled items and choices.
  - Deterministic shortcuts.

| API | Upstream | Go |
|---|---|---|
| Entity read/update(+return), notify, emit, subscribe | throughout | PRESENT (`UpdateWith`/`UpdateInWith` `entity.go:382,404`) |
| `cx.focus_handle()` without a window | state.rs:99-115 | X4 |
| `focus`/`is_focused`, `prevent_default`/`default_prevented` | various | PRESENT |
| `KeyDownEvent{is_held, prefer_character_input}`, `is_ime_in_progress`, `number_of_modifiers`, `secondary` | keyboard.rs:15-33 | PRESENT `keystroke.go:351,512-521,62,65` |
| `capture_key_down`, `track_focus` (through Checkbox/Radio) | control.rs:96-119 | PRESENT on Div (`div.go:992,685`) |
| `aria_description` | control.rs:91 | PRESENT as `A11ySpec.Description` |
| `aria_position_in_set`/`size_of_set` | control.rs:94,111 | MISSING-GO + MISSING-NATIVE (X3) |

- **Third-party:** none.
- **Base deps:** input (InputState/InputEvent), checkbox, radio, init (tests).
- **Size:** L (about 1,500 lines plus about 600 lines of tests).
- **Hazards:**
  - Reentrant InputState updates inside state updates.
  - Go `Subscribe` activates through a queued defer (`event.go:106-110`), which can shift the event counts the silent-setter test checks.
  - The Go `Keystroke.Key` spellings must match the pin's names.

## 2. Size summary

| Module | Upstream LOC | Tests | Go size | Main gaps |
|---|---|---|---|---|
| pagination | 281 | 4 | S | X1/X2 |
| select | 507 | 6 | S-M | X1/X2/X3 |
| combobox | 246 | 2 | S | via select |
| tree | 715 | 6 | M | X4, X5, list refine, X1-X3 |
| nav_stack | 833 | 7 | M | motion/history deps |
| resizable/* | 2,047 | 15 | M | keyed state, on_prepaint, cursor, refresh, X6 |
| calendar | 1,078 | 9 | M | chrono, strftime API, X6 |
| date_picker | 112 | 0 | S | a11y only |
| time_field | 775 | 10 | M | chrono, X5 |
| color_picker | 999 | 9 | M | Hsla→Rgba export, SubscribeIn, OnHover, keyed state |
| number_input | 489 | 2 | S-M | input internals seam |
| otp_input | 237 | 2 | S | OnFocus/OnBlur, activation observer |
| questionnaire/* | 2,640 | 6 | L | reentrancy, X4 |

Estimated total: about 6,500-7,500 Go lines including tests.

## 3. Deduplicated MISSING-GO core additions

1. Div a11y authoring API: `Role`, `AriaLabel`, `AriaValue`, `AriaDescription`, `AriaExpanded`, `AriaSelected`, `AriaToggled`, `AriaNumericValue`, `AriaPositionInSet`/`SizeOfSet`, `OnA11yAction`. This needs `A11ySpec`/snapshot fields and new Role constants (Navigation, ComboBox, Tree, TreeItem, RadioButton, SpinButton, TimeInput) for snapshot and tests only. See the native list for publishing.
2. `DivElement.OnMouseDown(button, …)`, plus the capture/out variants (copy `OnMouseUp`, `div.go:1073`).
3. `DivElement.OnHover(func(bool, *Window, *App))`.
4. `Window.UseKeyedState` (window-scoped keyed entity store).
5. An exported `Window.Refresh()`.
6. A `DivElement.OnPrepaint(bounds)` hook, or a base custom-element wrapper.
7. Per-hitbox cursor: `Div.Cursor(CursorStyle)` with frame-time resolution feeding the existing `App.SetCursorStyle`.
8. Fluent style setters plus a full `StyleRefinement`: flex direction, grow/shrink/basis/none, min/max w/h, per-side padding, w/h full, relative/inset, gap_x/y, font weight, `GridCols(n)` (X6; shared with styled).
9. `UniformListElement.Refine(StyleRefinement)`, plus mouse-wheel scrolling for `UniformList`/`List`. Wheel scrolling is confirmed absent (`list.go:16-18`).
10. Events and focus:
    - `SubscribeIn` (callback receives `*Window`).
    - Per-handle `OnFocus`/`OnBlur`.
    - `ObserveWindowActivation`.
    - A `ListenerFor(entity, f)` helper usable outside a `Context`.
11. Colour:
    - Export `HslaToRgba` (`draw_text.go:513`).
    - Add `RgbToHsla(Rgba)` for float input.
    - Add a clamping `HslaOf(h, s, l, a)`.
12. Optional:
    - An App-level focus-handle allocator (otherwise base constructors take `*Window`).
    - Tuple or name+int `ElementID` and `Div.ID(ElementID)`.
    - An `Axis` type with `Size.Along`.
    - `Div.OnDrag` with preview painting (resizable can avoid it).
    - `Group`/`GroupHover`.
    - A `Deferred` element (test only; the popover researcher owns it).
13. Test sugar: `AddWindowView`, `SimulateKeystrokes`, `SimulateMouseMove/Down/Up`, and a per-element a11y info read.

## 4. Third-party dependency questions

- **chrono** (calendar, time_field): replace with stdlib `time` plus hand-written `NaiveDate`/`NaiveTime` value types (about 120 lines). Open API decisions:
  - `Date::format(strftime)`: write a subset strftime translator, or take Go layouts?
  - `TimePrecision::format()`: return a Go layout instead of a chrono string.
- **palette** (color_picker, via core `Hsla`/`Rgba`): already hand-ported bit-exact in Go. It only needs exporting, so no module is needed.
- **regex**: stdlib `regexp`.
- **smallvec**: a slice.
- **rust_decimal / num-traits**: not used in this slice (only in plot/scale*). Defer to the plot researcher.
- Result: this whole slice can stay at **zero third-party Go modules**.

## 5. MISSING-NATIVE (frozen DLL)

1. **Roles beyond the 8 native tags.** Navigation (pagination), ComboBox (select, combobox, date_picker), Tree/TreeItem (tree), RadioButton (color swatch), SpinButton (number_input) and TimeInput (time_field) cannot be published.
   - Evidence: `reference/native/src/a11y.rs:598-612` `role_from_tag` returns `ERR_BAD_VALUE` for tags outside 1-8, and the tags are listed at `internal/native/a11y.go:33-47`.
   - Go side: `gpui/a11yprovider.go:545-566` errors, and `:673-675` aborts the whole publish.
2. **Node states.** expanded, selected, toggled/checked, numeric value, and position-in-set/size-of-set cannot be published.
   - Evidence: `A11yNodeRecord` (`internal/native/a11y.go:106-127`; 104 bytes, size-checked) has flags only for Hidden=0x1 and HasBounds=0x2. The native decoder `a11y.rs:816-870` applies only label, description, value, author_id, hidden, bounds, actions and children.
   - This also means native CheckBox and Slider expose no toggled or numeric value.
3. **Not native** (checked):
   - Resize cursors go through pure-Go Win32 `LoadImageW`/`SetCursor` (`desktop_windows.go:71,479`).
   - Window activation already reaches Go (`win32host_windows.go:588`).
   - In-window drag needs no OLE.
   - The native scene colour has a solid tag only (`internal/native/scene.go:180-184`), so there are no gradients. This slice is unstyled and paints none: color_picker leaves the hue and alpha tracks to the app. A styled picker would hit this limit, but Base does not.
   - Every behavioural feature in this slice is reachable without changing the DLL.

**Decision needed:** publish the unsupported roles as GenericContainer, or the nearest supported role such as Button, and drop the states. That is a documented deviation from upstream. The alternative is to treat a11y fidelity for these controls as blocked on a DLL revision. Go-side snapshots can still carry the full semantics for tests either way.
