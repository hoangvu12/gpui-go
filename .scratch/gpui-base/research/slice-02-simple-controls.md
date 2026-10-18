# Slice 02: Base simple controls, port inventory

Research date: 2026-10-19. Read-only. Upstream `gpui-ce/gpui-component` `c0820693`, crate `crates/base`.
Shorthand: `U:` = `upstream-base/ce/crates/base/src/`, `P:` = `gpui-go/reference/ce-source/crates/gpui/src/` (the core Rust pin), `G:` = `gpui-go/gpui/`, `N:` = `gpui-go/reference/native/src/` (the frozen DLL source).
This overlaps B1-controls.md (same date). The claims here were re-checked against the current tree, and this slice adds otp_input, number_input, time_field and pagination.

## Headline

- **None of the 19 components ports as pure Go on today's `gpui.DivElement`.** Every control is a `div` plus click, focus, a11y and refinable style. The Go div lacks:
  - click delivery. `OnClick` appends to `clickListeners` (G:div.go:944), but nothing reads that slice (grep: only div.go:620-622, 944).
  - a `ClickEvent::Keyboard` variant. G:div.go:542 is `{Button uint8}`.
  - the role and `aria_*` builders.
  - `on_mouse_down`, `on_mouse_up_out`, `on_drag` and `on_drag_move`.
  - `use_keyed_state`.
  - a complete `StyleRefinement`. It has only 5 fields (authoring/style.go:47-53).
- **Every gap but one is a Go-side core addition.** The native primitives already exist: quads with border and radii, shadows, polychrome sprites, hitboxes, focus, keymaps, entities, events, timers and the image atlas.
- **The one native gap is the a11y node ABI.** It carries 8 role tags, plus label, description, value, author id, hidden, bounds, actions and children. It has no toggled, selected, expanded, numeric, orientation, level or set-position properties (details under "Native gaps").

## Summary table

Lines = full file and tests = `#[test]`+`#[gpui::test]` (grep). "prod" = lines before `#[cfg(test)]`.
Class key:
- **CORE** = needs a Go-side core addition.
- **NAT-A11Y** = also hits the a11y ABI gap. Behavior is portable; UIA exposure is lossy.
- **PURE** = pure Go over the existing core.

| component | lines (prod) | tests | class | missing core APIs | Base deps | 3rd-party |
|---|---|---|---|---|---|---|
| button | 558 (260) | 8 | CORE (role Button maps natively) | click synthesis + `ClickEvent::{Mouse,Keyboard}`, `on_mouse_down`, `use_keyed_state`, `role`/`aria_label`, full StyleRefinement (`line_height`, flex align), `canvas` (tests) | styled (`RoleOverride`, `StyledExt::refine_style`), state_style, component_traits (`Selectable`), test_support, element_ext (tests) | smallvec |
| checkbox | 705 (401) | 8 | CORE + NAT-A11Y (`aria_toggled`) | as button + `aria_toggled`/`Toggled` | styled, state_style, test_support | smallvec |
| radio | 414 (240) | 5 | CORE + NAT-A11Y (RadioButton, toggled, selected, pos/size-in-set) | as button + `aria_selected`, `aria_position_in_set`, `aria_size_of_set` | state_style, styled, test_support | smallvec |
| radio_group | 67 | 0 | CORE (thin) + NAT-A11Y (RadioGroup, orientation) | `role`, `aria_orientation`, `Axis`, `Orientation` | styled | smallvec |
| switch | 739 (398) | 9 | CORE + NAT-A11Y (Switch, toggled) | as button + `ElementId::NamedChild` (`(id,"track")`, U:switch.rs:459) | state_style, styled, test_support | smallvec |
| toggle | 459 (214) | 7 | CORE + NAT-A11Y (toggled on a Button) | as button + `aria_toggled` | state_style, styled, test_support | smallvec |
| toggle_group | 67 | 0 | CORE (thin) + NAT-A11Y (Toolbar role) | `role`, `aria_orientation` | styled | smallvec |
| link | 466 (205) | 8 | CORE + NAT-A11Y (Link role) | as button (`href`/`open_with` is pure, no `open_url`) | state_style, styled | smallvec |
| avatar | 187 (143) | 2 | CORE | `img()`/`Img`/`ImageSource` element (no Go image element exists) | styled | smallvec |
| progress | 182 (130) | 3 | CORE (thin) + NAT-A11Y (ProgressIndicator, numeric) | `role`, `aria_numeric_value`/`min`/`max`, `aria_label` | styled | smallvec |
| slider | 834 (793) | 4 | CORE + NAT-A11Y (numeric, step, orientation; Slider role maps) | `on_drag`, `on_drag_move`/`DragMoveEvent`, `on_mouse_down`, `on_mouse_up(_out)`, `Window::listener_for`, `on_a11y_action`, numeric aria | geometry (`AxisExt`, `Along`), element_ext (`on_prepaint`), test_support | none |
| collapsible | 112 (83) | 1 | PURE (via motion::MotionReveal) | none: `WithElementState`, `RequestAnimationFrame`, `WithContentMask`, `LayoutAsRoot`, `PrepainAt` and `RequestElementLayout` all exist | motion/reveal.rs | none |
| accordion | 507 (362) | 3 | CORE + NAT-A11Y (Group, Heading, Region; expanded, level) | click, `role`, `aria_expanded`, `aria_level` | styled, test_support | smallvec |
| tabs | 427 (236) | 5 | CORE + NAT-A11Y (Tab, TabList; selected, pos/size) | click, `on_mouse_down`, `role`, `aria_selected`, set position | state_style, styled, test_support | smallvec |
| toolbar | 460 (267) | 7 | CORE + NAT-A11Y (Toolbar, Group; orientation) | `use_keyed_state`, `role`, `aria_orientation` (`FocusNext`/`FocusPrev`/`Focused`/`ContainsFocused`/`OnKeyDown`/`StopPropagation` exist) | styled | smallvec |
| pagination | 281 (203) | 4 | CORE (thin) + NAT-A11Y (Navigation) | `role`, `aria_label` (`PaginationState` is pure logic) | styled | none |
| otp_input | 237 (210) | 2 | CORE | `cx.on_focus`, `cx.on_blur`, `cx.observe_window_activation`, `listener_for` (`PreventDefault`, `IsActive`, entities, events, `Observe` and `TrackFocus` exist) | input/base/blink_cursor.rs (spawn + timer: G `App.Spawn` + `TaskRun.Sleep` exist), styled | (web_time via BlinkCursor) |
| number_input | 489 (374) | 2 | CORE + NAT-A11Y (SpinButton, numeric) | `role`, `aria_numeric_value`, click (via Button) (actions, `BindKeys`, `KeyContext` and `OnAction` exist) | input (`InputState`, `InputBase`), button, styled | none |
| time_field | 775 (634) | 10 | CORE + NAT-A11Y (TimeInput; `aria_value` maps to Value) | `on_mouse_down`, `listener_for`, `role`, `aria_value`; `Focusable` trait (pattern only) | number_input (`Increment`/`Decrement`), actions.rs (`SelectLeft`/`Right`/`NextColumn`/`PrevColumn`), input (`Delete`), styled, test_support | chrono (`NaiveTime`, `Timelike`) |

Totals: 7,966 lines, 88 tests.

## Per-component notes (a: what it is / public API)

- **button** (U:button.rs:20-258): `Button::new(id)`. Builders: `disabled`, `selected`, `styles(ButtonStyles{selected,disabled})`, `accessibility_label`, `role(RoleOverride)`, `track_focus`, `on_click`, `tab_index`, `tab_stop` and `focusable`. Implements Styled, ParentElement, InteractiveElement, StatefulInteractiveElement, `Selectable`. The focus handle is kept in `window.use_keyed_state(id, ...)` (U:button.rs:132-136). A disabled button swallows mouse-down with `stop_propagation` (U:243-245). Tests check for `ClickEvent::Keyboard` (U:299).
- **checkbox** (U:checkbox.rs:17-401): `CheckboxState{Unchecked,Checked,Indeterminate}` and `Checkbox` (`state`/`checked`/`indeterminate`/`on_change(next)`/...). Also `CheckboxIndicator(+Styles)`. Uses `aria_toggled(state.toggled())` (U:377).
- **radio / radio_group** (U:radio.rs:21-240, U:radio_group.rs:12-67): `Radio` (`checked`, `on_change`, `set_position(pos,size)`) and `RadioGroup` (`axis`), which sets `Role::RadioGroup` plus orientation (U:radio_group.rs:59-60).
- **switch** (U:switch.rs:22-398): `Switch`, plus `SwitchTrack` (Stateful, with a tuple id) and `SwitchThumb`, each with a `*Styles` type. Uses `Role::Switch` plus toggled (U:364-365).
- **toggle / toggle_group**: `Toggle` (`pressed`, `ToggleStyles`) is `Role::Button` plus toggled (U:toggle.rs:173-181). `ToggleGroup(axis)` is `Role::Toolbar` (U:toggle_group.rs:59).
- **link** (U:link.rs:24-205): `Link` (`href`, `open_with(handler)`, `on_activate`, `disabled`, `LinkStyles`). It never launches a browser itself (U:18-21).
- **avatar** (U:avatar.rs:11-143): `Avatar` (`image`, `fallback`; shows the image, else the fallback), `AvatarImage::new(impl Into<ImageSource>)` wrapping `gpui::img` (U:69-78), and `AvatarFallback`.
- **progress** (U:progress.rs:12-130): `Progress` (value clamped to 0..100, indeterminate, label), `ProgressTrack` and `ProgressIndicator`.
- **slider** (U:slider.rs:15-793): `SliderEvent`, `SliderValue{Single,Range}`, `SliderScale{Linear,Logarithmic}`, entity `SliderState` (min/max/step/scale/value/percentage/bounds), plus `Slider`, `SliderTrack`, `SliderIndicator` and `SliderThumb`. Drag goes through `on_drag(DragSlider)` + `on_drag_move` (U:612-616, 751-755). A11y increment/decrement go through `on_a11y_action` (U:487, 497).
- **collapsible** (U:collapsible.rs:14-83): `Collapsible` (`open`, `content`, `reveal(id, progress)`), which wraps content in `MotionReveal` (U:motion/reveal.rs:36-120, a custom Element).
- **accordion** (U:accordion.rs:17-362): `Accordion` (Group), `AccordionItem`, `AccordionHeader` (Heading + `aria_level`), `AccordionPanel` (Region, `keep_mounted`) and `AccordionTrigger` (Button + `aria_expanded`, `on_change`).
- **tabs** (U:tabs.rs:23-236): `Tab` (`selected`, `disabled`, `set_position`, `on_click`, `TabStyles`) and `Tabs` (TabList).
- **toolbar** (U:toolbar.rs:45-267): `Toolbar` (`disabled`; left/right roving focus that steps window tab stops until focus re-enters the container, U:87-135) and `ToolbarGroup` (Group + label).
- **pagination** (U:pagination.rs:14-203): `PaginationItem`, `PaginationState` (pure page-window math) and `Pagination` (Navigation, `on_change`).
- **otp_input** (U:otp_input.rs:10-210): entity `OtpState` (Focusable, emits `OtpEvent`, owns a `BlinkCursor`) and `OtpInput`. Subscribes to focus, blur and window activation (U:32-41).
- **number_input** (U:number_input.rs:13-374): `actions!(number_input,[Increment,Decrement])` with up/down bindings (U:13-20). Also `StepAction`, `NumberStep`, `NumberInputEvent` (implemented on `InputState`, U:60), `NumberInput`, `NumberInputText` and `step_value()`. Composed from `InputBase` plus two `Button`s (U:238-243, 299-306).
- **time_field** (U:time_field.rs:22-634): 8 key bindings (U:22-29). Also `TimePrecision`, `HourCycle`, `TimeSegment`, `TimeFieldEvent`, entity `TimeFieldState` (Focusable), `TimeFieldSegment(+State)` and `TimeField` (`Role::TimeInput`, `aria_value` set to the formatted time, U:587-591).

## Core additions needed (Go-side; consolidated)

Existing Go pieces each addition builds on are noted. None needs a new DLL export.

1. **Full `StyleRefinement` / `Styled` vocabulary.** P:style.rs:233-362 (`Style` derives `Refineable`: `background` 339, `border_color` 342, `corner_radii` 355, `box_shadow` 362) and P:style.rs:591-610 (`TextStyle.line_height`).
   - The Go refinement has 5 fields (authoring/style.go:47-53). G `Style` has no background, border-color, radius or shadow (G:style.go `type Style`).
   - The div paints only the bg quad, with `CornerRadii{}` (G:div.go:1655-1675).
   - The native `InsertQuad`/`InsertShadow` already exist (G:scene.go:359-362).
   - Every component in the slice depends on this through `StyledExt::refine_style` (U:styled.rs:79-82) and `StateStyle` (U:state_style.rs).
2. **Click synthesis and the `ClickEvent` enum.** P:interactive.rs:220 (`MouseClickEvent`), 264 (`KeyboardClickEvent`), 290 (`enum ClickEvent`). Also P:elements/div.rs:1814 (`on_click`), 3253 + 3452-3483 (mouse-down to up pairing via `pending_mouse_down`) and 3527-3560 (Enter/Space keyboard click). The Go listeners are stored but never dispatched (G:div.go:542, 620-622, 944). Used by 12 of the 19 components.
3. **Mouse and drag listeners on div.**
   - P:elements/div.rs:996 `on_mouse_down`, 1128 `on_mouse_up_out`, 1194 `on_drag_move` + `DragMoveEvent`, 1844 `on_drag`.
   - Go building blocks exist: `Window.OnMouseEvent` (G:mouseevent.go:459), `InsertHitbox` (477), `App.StartDrag`/`StopDrag`/`ActiveDrag` (G:filedrop.go:192-207) and `OnDrop` (G:div.go:1107). The Go div builders today are only OnMouseMove/OnMouseUp/OnScrollWheel/OnPinch.
4. **`Window::use_keyed_state`.** P:window.rs:4093 (+ `use_state` 4122). Go has only `WithElementState` (G:element.go:1379), so this can be layered on it plus `NewEntity` (G:entity.go:271). Used by button, checkbox, radio, switch, toggle, link and toolbar.
5. **A11y builders on div plus the `A11ySpec` fields.**
   - Rust builders: P:elements/div.rs:1496 `role`, 1518 `aria_label`, 1589 `aria_selected`, 1595 `aria_expanded`, 1601 `aria_toggled`, 1608 `aria_disabled`, 1614-1653 numeric value/step/min/max and orientation, 1628 `aria_value`, 1659 `aria_level`, 1665/1671 position/size of set, 1728 `on_a11y_action`.
   - Rust types: P:gpui.rs:96 re-exports `accesskit::{Orientation, Role, Toggled}`.
   - Go today: `divElement.A11yRole()` returns `(0,false)` (G:div.go:1289). `Role` has 9 values (G:accessibility.go:76-97). `A11ySpec` has role, label, description, value, hidden, author id and focus only (G:accessibility.go:305-330). `A11ySnapshotNode` matches (880-905).
   - The Go-side snapshot can carry every field for tests. Native exposure is limited (see below).
6. **`canvas` element.** P:elements/canvas.rs:10. Not in Go (no `func Canvas`). It is needed by Base `ElementExt::on_prepaint` (U:element_ext.rs: `canvas(...)` child), which slider uses in production (U:slider.rs:678). Every a11y test also uses it. A custom Go `Element` is trivial.
7. **`img` element / `Img` / `ImageSource`.** P:elements/img.rs:42 (`ImageSource`) and 201 (`img`), plus P:window.rs:5057 (`paint_image`) and 5078. Go has the image cache (`UseImageAsset`, G:imagecache.go:751), the atlas and native `InsertPolychromeSprite` (G:draw_text.go:176), but no element and no `Window.PaintImage`. Needed by avatar.
8. **Focus-change and activation observers.**
   - Rust: P:app/context.rs:547 `on_focus`, 596 `on_blur`, 573/642 focus in/out, 444 `observe_window_activation`. The window side is P:window.rs:1290 (`focus_listeners`) and 2784 (`is_window_active`).
   - Go has only `Window.OnFocusLost` (G:focus.go:486) and `Window.IsActive()` (G:window.go:288).
   - Needed by otp_input. The input slice probably needs it too.
9. **`Window::listener_for(entity, f)`.** P:window.rs:6868. Go has `Context.Listener` (G:context.go:144) but no entity-handle variant. It is a small helper over `Entity.UpdateIn` (G:entity.go:394). Used by slider, otp_input and time_field.
10. **`ElementId::NamedChild` / `NamedInteger`.** P:window.rs:7667, 7673, 7770-7782. Go has Name/View/Integer only (G:element.go:73-83). Base could fake these with formatted names, but identity-path parity argues for the core variant. Used by switch (U:switch.rs:459) and number_input (`("number-input", entity_id)`, U:number_input.rs:299).
11. **`Axis` and `Orientation` types.** P:geometry.rs:26. Go `Orientation` (G:path.go:97) is a path type, not the a11y one. Used by radio_group, toggle_group, slider and toolbar. Trivial.
12. **`Focusable` trait.** P:window.rs:724. No Go interface. It is a pattern (`FocusHandle(app)`) that Base can define locally. Low priority.

Already present (verified):
- Focus: `FocusHandle.TabIndex`/`TabStop`/`IsFocused`/`ContainsFocused` (G:focus.go:169, 181, 245, 255), `Window.Focused`/`Focus`/`FocusNext`/`FocusPrev` (G:focus.go:373, 348, 412, 435), `TrackFocus` and `KeyContext` (G:div.go).
- Events and dispatch: `PreventDefault` (G:mouseevent.go:511), `StopPropagation`/`Propagate` (G:key_dispatch.go:1174-1186), `OnKeyDown` and `OnAction`.
- Keymaps and actions: `NewKeyBinding` (G:keymap.go:64), `App.BindKeys` (G:input.go:151), `Action[A]` (G:action.go:60).
- Entities and tasks: `Event[S,P].Emit`/`Subscribe` (G:event.go:36, 111), `Context.Observe`/`Notify`, `App.Spawn` + `TaskRun.Sleep` (G:executor.go:742, 625).
- Rendering: `RequestAnimationFrame` (G:imagecache.go:958), `WithContentMask` (G:element.go:1165), `RequestElementLayout` (G:element.go:1530), the debug-selector bounds (G:render.go:289).

## Native gaps

There is **one**: the a11y node ABI. Hard evidence:

- **The native decoder accepts only 8 role tags.** These are Window, GenericContainer, Button, TextInput, TextRun, StaticText (mapped to `Label`), CheckBox and Slider. Any other tag returns `ERR_BAD_VALUE` (N:a11y.rs:598-612).
- **The record has no property fields.** The fixed `GpuiGoA11yNodeRecord` mirror holds only id, role, flags, actions mask, children, label, description, value, author id and bounds (internal/native/a11y.go:106-127). The decoder sets only those fields plus hidden (flag 0x1) and bounds (flag 0x2) (N:a11y.rs:813-872). Toggled, selected, expanded, disabled, numeric value/min/max/step, orientation, level and position/size-in-set have no wire representation.
- **The Go publisher fails the whole publish on an unmapped role.** It errors rather than coercing (G:a11yprovider.go:545-566, 673-676).

Slice impact:

| | Items |
|---|---|
| Roles with **no** native tag | RadioButton, RadioGroup, Switch, Link, Tab, TabList, ProgressIndicator, Toolbar, Group, Heading, Region, Navigation, SpinButton, TimeInput |
| Roles that map | Button (button, toggle, accordion trigger), CheckBox, Slider |
| Properties that map | label; `aria_value` (time_field) |
| Properties that cannot map | everything else listed above |

Mitigation (proposal; needs a user decision):
- Keep the full semantics in the Go `A11ySnapshot`, so ported Base a11y tests (which assert on `accesskit::Node` built through `canvas`) assert on the snapshot instead.
- At publish time, map unmapped roles onto the nearest native tag (for example Switch, RadioButton and Toggle to CheckBox or Button; containers to GenericContainer) instead of failing.
- Optionally encode state into Value or Description. This is lossy for UIA clients but never blocks the port.

Nothing else in the slice needs native work:
- Styles: quads with radii and borders, and shadows (G:scene.go:359-362).
- Images: polychrome sprites (G:draw_text.go:176) and the image atlas.
- Input: hitboxes, drag state (Go-side) and timers (Go scheduler).

## Third-party usage

| Crate | Where | Go replacement |
|---|---|---|
| `smallvec::SmallVec` | 13 of 19 files hold children in it (not slider, collapsible, pagination, otp_input, number_input, time_field) | a Go slice |
| `std::rc::Rc` | handler storage | func values |
| `std::ops::Range` | slider, pagination | a small struct |
| `chrono::{NaiveTime, Timelike}` | only time_field (U:time_field.rs:4) | `time.Time`/`time.Duration` or a local `{h,m,s,ns}` type (no module needed). Format strings must be re-expressed (U:time_field.rs:44). |
| `web_time::Duration` | transitive via BlinkCursor (U:input/base/blink_cursor.rs:2) | `time.Duration` |
| `serde::Deserialize` | transitive via actions.rs `Confirm` (U:actions.rs) | skip (Go actions have no JSON) |
| `accesskit` | only through the gpui re-exports (and directly in tests) | the Go Role/A11ySpec |

No new Go modules are required.

## Internal dependency order

0. **Core (Go-side, items 1-11 above).** Do these first: StyleRefinement, then click/mouse/drag, then `use_keyed_state`, then the a11y builders and spec, then canvas, then `listener_for`, NamedChild and Axis. Img and the focus/activation observers can follow later.
1. **Base infrastructure.**
   - `styled.rs` (`RoleOverride`, `StyledExt`, `h_flex`/`v_flex`)
   - `state_style.rs` (pure refine)
   - `component_traits.rs` (`Selectable`)
   - `observe.rs` + `test_support.rs` (the element snapshot registry; key it by window in Go)
   - `element_ext.rs` (`on_prepaint`, which needs canvas)
   - `geometry.rs` (`AxisExt`, `Along`)
   - `actions.rs`
   - `motion/reveal.rs`
2. **Leaves:** radio_group, toggle_group, progress, pagination, collapsible, avatar (after img).
3. **button.** Number_input reuses it.
4. **Button-pattern siblings** (copies of the same focus/click/state pattern): toggle, checkbox, radio, switch, link, tabs.
5. **accordion** (click + expanded) and **toolbar** (keyed focus handle + roving focus).
6. **slider** (drag listeners, `on_prepaint`, `listener_for`, `on_a11y_action`).
7. **otp_input.** It needs `input/base/blink_cursor.rs` from the input slice and the focus/blur/activation observers.
8. **number_input.** It needs the input slice's `InputState` and `InputBase`, plus button. Note that `input/mod.rs:81` re-exports `number_input::{NumberInputEvent, NumberStep}`, so the two land together.
9. **time_field.** It needs number_input (`Increment`/`Decrement`, via U:lib.rs:137-138), actions.rs and `input::Delete`.
