# B1 — GPUI Base controls slice: port research

Scope: `crates/base/src/{button,checkbox,radio,radio_group,switch,toggle,toggle_group,toolbar,link,avatar,progress,slider,accordion,collapsible,tabs,table}.rs` at upstream `c0820693`, checked against the Go port (`gpui/`, core pin 254b5dbd). Read-only research; nothing was executed.
Path shorthand: `U:` = `upstream-base/ce/crates/base/src/`, `P:` = `gpui-go/reference/ce-source/crates/gpui/src/` (the core pin), `G:` = `gpui-go/gpui/`.

## 0. Headline findings

1. **None of the controls can be ported on the current Go `DivElement`.** Every control is "a `div` plus semantic state, a11y and click wiring." The Go div is missing most of that.
   - **No click delivery.** `OnClick` stores `clickListeners` (G:div.go:622, 944), but nothing reads them. There is no mouse-down→up click synthesis, no Enter/Space keyboard click, and `ClickEvent` is `{Button uint8}` with no `Mouse`/`Keyboard` variants (G:div.go:542). Ten of the 16 modules' tests depend on click.
   - **No a11y from div.** `divElement.A11yRole()` returns `(0,false)` (G:div.go:1289). Div has no `Role`/`Aria*` builders.
   - **Very little style vocabulary.** `StyleRefinement` = `authoring.StyleRefinement`, which has only 5 fields: Display, PaddingL/R, Gap, Opacity (authoring/style.go:47-53). The Go `Style` has no background, border-color, corner-radius or shadow fields (G:style.go:774-853). The div paints only a background quad, with zero radii and zero borders (G:div.go:1655-1675).
   - **Missing listeners and state styles.** No `on_mouse_down`, `on_mouse_up_out`, `on_drag`/`on_drag_move`, hover/active/focus_visible styles, or `use_keyed_state`.
2. **The authoring mechanism works but is not connected.** `authoring.Styled[Self]` + `BindStyled` gives concrete-typed fluent chains (authoring/style.go:64-147). `gpui.DivElement` does **not** use it: div has its own hand-written methods, and the two share only the 5-field refinement via `Refine` (G:div.go:896-920). There is no "Interactive" equivalent at all.
3. **One real native blocker: the AccessKit role and property ABI.** The frozen native decoder accepts only 8 role tags and a fixed node record with no toggled/selected/expanded/numeric/orientation/level/set/row/col fields (evidence in §4). Go-side snapshots and tests can carry these; exposing them to UIA cannot. Also, publishing any unknown role currently **fails the whole tree publish** (G:a11yprovider.go:545-566, 673-676).

## 1. Per-module table

Tests: `#[gpui::test]` + `#[test]` counts (from grep). Size covers Go code + ported tests.

| Module | LOC | Public items | Tests | Base deps | Size |
|---|---|---|---|---|---|
| button | 558 | `Button` (new, disabled, selected, styles, accessibility_label, role, track_focus, on_click, tab_index, tab_stop, focusable), `ButtonStyles{selected,disabled}`; impl Styled/ParentElement/InteractiveElement/StatefulInteractiveElement/RenderOnce/`Selectable` | 8 | styled (`RoleOverride`, `StyledExt::refine_style`), state_style, observe/test_support (`ObservedElement`, `TestSupportExt`), component_traits (`Selectable`), element_ext (`on_prepaint`, test only) | M |
| checkbox | 705 | `CheckboxState{Unchecked,Checked,Indeterminate}`, `Checkbox` (state/checked/indeterminate/disabled/styles/label/on_change(next_state)/tab_*/track_focus/role), `CheckboxStyles`, `CheckboxIndicator` + `CheckboxIndicatorStyles` | 8 | styled, state_style, test_support | M |
| radio | 414 | `Radio` (id override, checked, disabled, styles, label, on_change, track_focus, set_position, tab_*), `RadioStyles` | 5 | state_style, styled, test_support | M |
| radio_group | 67 | `RadioGroup` (axis) | 0 | styled | S |
| switch | 739 | `Switch`, `SwitchStyles`, `SwitchTrack(+Styles)` (Stateful, id tuple), `SwitchThumb(+Styles)` | 9 | state_style, styled, test_support | M |
| toggle | 459 | `Toggle` (pressed, …), `ToggleStyles{pressed,disabled}` | 7 | state_style, styled, test_support | M |
| toggle_group | 67 | `ToggleGroup` (axis; Role::Toolbar) | 0 | styled | S |
| toolbar | 460 | `Toolbar` (disabled; arrow-key roving focus), `ToolbarGroup` (label) | 7 | styled | M |
| link | 466 | `Link` (href, open_with, on_activate, disabled, styles, label, tab_*), `LinkStyles{disabled}` | 8 | state_style, styled | M |
| avatar | 187 | `Avatar` (image, fallback), `AvatarImage(ImageSource)`, `AvatarFallback` | 2 | styled | S (+core `img`) |
| progress | 182 | `Progress` (value clamped 0..100, indeterminate, label), `ProgressTrack`, `ProgressIndicator` (macro) | 3 | styled | S |
| slider | 834 | `SliderEvent{Change,Release}`, `SliderValue{Single,Range}`, `SliderScale{Linear,Logarithmic}`, `SliderState` (entity: min/max/step/scale/default_value/set_value/value/…/update_value_by_position/handle_release/percentage/bounds/set_bounds), `Slider`, `SliderTrack`, `SliderIndicator`, `SliderThumb` | 4 | geometry (`AxisExt`, `Along`), element_ext (`on_prepaint`), test_support | M (~900) |
| accordion | 507 | `Accordion`, `AccordionItem` (open, disabled, header, panel), `AccordionHeader` (level, id), `AccordionPanel` (open, keep_mounted, id), `AccordionTrigger` (open, disabled, on_change) | 3 | test_support, styled | M |
| collapsible | 112 | `Collapsible` (open, content, reveal(id, progress)) | 1 | motion (`MotionReveal`) | S |
| tabs | 427 | `Tab` (id, selected, disabled, label, set_position, on_click, styles), `TabStyles`, `Tabs` | 5 | state_style, styled, test_support | M |
| table | 424 | `Table` (row_count, column_count, label), `TableHeader`/`TableBody` (RowGroup), `TableRow(row_index)`, `TableHead`/`TableCell(column_index)`, `TableCaption` | 3 | test_support, styled | M (one generic part type replaces the macros) |

Third-party crates: `smallvec` everywhere (use a Go slice); `std::rc::Rc` handler storage (use a Go func value). Nothing else. Slider uses `std::ops::Range` (use a struct). No crates are needed from the base Cargo.toml (markdown/regex/etc. belong to other modules).

Whole-slice estimate: about 5.5–7k Go lines including tests. This assumes the core additions in §5 land first. Base-side infrastructure (state_style, styled `RoleOverride`/`refine_style`, observe/test_support, component_traits, geometry, element_ext, motion reveal) is counted by the other researcher.

## 2. What the tests exercise (behavioural policies to preserve)

- **Button** (U:button.rs:336-557)
  - Pointer click fires once.
  - Enter and Space each fire exactly one `ClickEvent::Keyboard` after focus.
  - **Disabled Button blocks parent activation.** It gets no focus and adds `on_mouse_down(Left)` with `stop_propagation` (U:button.rs:242-246). Parent click count stays 0.
  - A fixed-height Button centers its child (flex + items_center + justify_center + `line_height(relative(1.))`, U:button.rs:219-228). Uses `on_prepaint` via canvas.
  - Style priority is **instance < selected < disabled** (state_style::resolve_style). Disabled wins even over a later `.bg()`.
  - `hover`/`active`/`focus_visible` builders must exist.
  - A11y: role Button, label, Click action only when enabled; `is_disabled()` stays false (upstream gap, U:button.rs:553-556).
- **Checkbox** (U:checkbox.rs:416-705)
  - Indicator projects state styles over the instance style.
  - Click emits `activated()` (Indeterminate→Checked, Checked→Unchecked); Enter and Space each emit once.
  - **Disabled Checkbox is inert but lets pointer events bubble**: no mouse-down stopper, parent click count == 1 (U:checkbox.rs:551-558). This contrasts with Button/Switch/Toggle/Link.
  - Priority instance < checked|indeterminate < disabled.
  - A11y: CheckBox role, label, `Toggled` False/True/Mixed.
- **Radio** (U:radio.rs:255-414)
  - Pointer and keyboard each fire once.
  - **Checked radios are inert as well as disabled ones**: `on_click` is attached only when `!disabled && !checked` (U:radio.rs:227-234). There is no mouse-down stopper, so a disabled radio bubbles like Checkbox.
  - A11y: RadioButton role, toggled, selected, position/size in set.
- **Switch** (U:switch.rs:413-739)
  - Track/thumb style priority.
  - **Track identity is a tuple id `(switch_id, "track")`** (U:switch.rs:457-477).
  - Pointer reports `!checked`; Enter/Space give one keyboard click each.
  - **Disabled blocks parent activation** (mouse-down stopper, U:switch.rs:380-384).
  - A11y: Switch role, toggled.
- **Toggle** (U:toggle.rs:279-459)
  - Pointer requests `!pressed`; Enter/Space give a keyboard click each.
  - Disabled is inert and has the mouse-down stopper.
  - Fixed-height centering; priority instance < pressed < disabled.
  - A11y: **Button role + toggled** (not ToggleButton).
- **Toolbar** (U:toolbar.rs:272-460)
  - Builder defaults.
  - **Left/Right rove focus** across items: `focus_next`/`focus_prev`, retried until focus is inside the container (`contains_focused`), max 100 attempts, restored on failure (U:toolbar.rs:73-112). The container's focus handle is `tab_stop(false)` from `use_keyed_state`.
  - A single item keeps focus; disabled ignores arrows.
  - Group has the Group role + aria label. Toolbar has the Toolbar role + Horizontal orientation.
- **Link** (U:link.rs:299-466)
  - The injected `open_with(href)` runs **before** `on_activate`, once per pointer click and once per Enter/Space.
  - **An href without a strategy never opens externally** (`cx.opened_url()==None`). The control never calls the platform.
  - Disabled blocks parent activation.
  - Priority instance < disabled. A11y: Link role, label, action surface.
- **Avatar** (U:avatar.rs:173-187): the image slot takes precedence over the fallback; the fallback renders without an image (structure only).
- **Progress** (U:progress.rs:136-182)
  - `value(120)` clamps to 100. numeric/min/max are projected via `write_a11y_info`.
  - Indeterminate omits the numeric value. The label projects to the name.
- **Slider** (U:slider.rs:798-834): pure unit tests only (value conversions/clamp, linear percentage, log mapping, log validation `should_panic`). The drag/a11y paths (on_a11y_action Increment/Decrement, drag_move, mouse_up_out release) are **untested upstream**.
- **Accordion** (U:accordion.rs:373-507)
  - The trigger exposes the expanded state.
  - Pointer requests `!open`; disabled stops this (by omitting `on_click`, with no stopper, so it bubbles).
  - Header gets Heading + level only when it has an id; Panel gets Region only when it has an id.
  - A closed panel renders `Empty` unless `keep_mounted`.
- **Collapsible** (U:collapsible.rs:101-112): `content()` children render only while open; plain children always render. `reveal()` delegates to `MotionReveal`.
- **Tabs** (U:tabs.rs:280-427)
  - Pointer activation is gated by disabled.
  - **Tab always stops mouse-down propagation, even when enabled** (U:tabs.rs:173).
  - Tab is not focusable (no track_focus).
  - Fixed-height centering; selected/position/size; Tabs has the TabList role.
- **Table** (U:table.rs:340-424): label; row/column indices; and debug_bounds for size/children. Both clicks route to the Table's own `on_click` (InteractiveElement forwarding).

Summary of the disabled pointer policy:

| Policy on disabled | Controls |
|---|---|
| Stop mouse-down propagation (blocks parent) | Button, Switch, Toggle, Link |
| Let events bubble | Checkbox, Radio, AccordionTrigger |
| Always stops mouse-down | Tab |

## 3. Core API usage → Go status

Legend:
- **PRESENT** — with a G: cite.
- **MISSING-GO** — a Go core addition is needed.
- **MISSING-NATIVE** — the frozen DLL would need to change.

| Core API (pin cite) | Used by | Go status |
|---|---|---|
| `div()`, `.id()` → `Stateful<Div>` | all | PRESENT `Div()`, `(*DivElement).ID(string)` G:div.go:671,678. **But** ID is string-only. Tuple ids (`(ElementId, "track")`, `("slider", entity_id)`, `("slider-thumb", u32)`) need composite `ElementID` kinds (NamedChild/NamedInteger). Only Name/View/Integer exist (G:element.go:45-85) → MISSING-GO |
| `Styled` + full `StyleRefinement` + `Refineable::refine` | all (refine_style, state styles) | MISSING-GO. 5-field `authoring.StyleRefinement` (authoring/style.go:47-53); `DivElement.Refine` merges only those (G:div.go:896-920) |
| Styled fluent used by controls: `flex, items_center, justify_center, size_full, absolute, line_height(relative(1.))` | button, toggle, tabs, toolbar group, element_ext | Flex/AlignItems/Justify/SizeFull/Absolute PRESENT (G:div.go:698,862,856,850,756). `line_height` has no fluent method (field `TextStyleRefinement.LineHeight` exists G:div.go:129). `relative()` ≈ `Fraction` G:style.go:119 → MISSING-GO (small) |
| Visual styling used by tests/apps: `bg, opacity, border_1, rounded_sm, w, h, size` | tests | Bg/Opacity/W/H/Size PRESENT. Border widths, corner radii, border color and box shadow have no fluent methods and **are not painted by div** (G:div.go:1655-1675 hard-codes `Corners{}`, `Edges{}`). Scene support is PRESENT: `PaintQuad` with radii/borders G:scene.go:521, `PaintShadow` G:scene.go:541 → MISSING-GO (div paint + style fields) |
| `ParentElement::extend/child/children` | all | PRESENT `Child/Children/TryChildren` G:div.go:1171-1215 |
| `RenderOnce` + `#[derive(IntoElement)]` | all | PRESENT `RenderOnce` interface + `OwnRecipe` (docs/authoring-decision-round.md), `TryIntoElement` precedence |
| `InteractiveElement::interactivity()` forwarding to a base div | all Stateful controls, avatar, table, slider | MISSING-GO. Div fields are private (G:div.go:601-666). There is no `Interactivity` record or generic helper to forward into |
| `track_focus(&FocusHandle)` | button…tabs | PRESENT `TrackFocus` G:div.go:685 |
| `FocusHandle::tab_index/tab_stop` | button…link, toolbar | PRESENT G:focus.go:169,181 |
| `cx.focus_handle()` | via use_keyed_state | PRESENT `NewFocusHandle(w)` G:window.go:339 |
| `window.use_keyed_state(id, cx, init) -> Entity<T>` (P:window.rs:4093) | button, checkbox, radio, switch, toggle, link, toolbar | MISSING-GO. `WithElementState` exists (G:element.go:1379) but needs a `GlobalElementID`; no render-time keyed-entity API |
| `tab_group()` | button test harness | PRESENT `TabGroup` G:div.go:1160 |
| `window.focus_next/focus_prev/focused/focus`, `FocusHandle::contains_focused` | toolbar | PRESENT G:focus.go:412,435,373,348,255 |
| `on_key_down` + `KeyDownEvent.keystroke.key` | toolbar | PRESENT `OnKeyDown` G:div.go:982; `Keystroke.Key` G:keystroke.go:132 |
| `on_click(ClickEvent)` with mouse **and** Enter/Space keyboard synthesis (P:elements/div.rs:674, 3527-3560) | button, checkbox, radio, switch, toggle, link, tabs, accordion trigger, table | **MISSING-GO**. Listener stored, never dispatched (only refs: G:div.go:622,944). `ClickEvent` lacks `Mouse/Keyboard` variants (G:div.go:542) |
| `on_mouse_down(MouseButton, …)` (P:div.rs:996) | button, switch, toggle, link, tabs, slider | MISSING-GO. No `OnMouseDown`. Building blocks are PRESENT: `w.OnMouseEvent` G:mouseevent.go:459, `InsertHitbox` :477, `MouseDownEvent{Button,Position}` :114 |
| `on_mouse_up(button)`, `on_mouse_up_out` (P:div.rs:1128) | slider | `OnMouseUp` raw (no button filter) PRESENT G:div.go:1073; `on_mouse_up_out` MISSING-GO |
| `cx.stop_propagation()` in mouse listeners | button…tabs, slider | PRESENT `(*App).StopPropagation` G:key_dispatch.go:1178 |
| `on_drag(value, ctor)`, `on_drag_move<T>` + `DragMoveEvent<T>` (P:div.rs:703,1194) | slider | MISSING-GO. Only `StartDrag/ActiveDrag/StopDrag` + `OnDrop` exist (G:filedrop.go:189-207, G:div.go:1107) |
| `hover/active/focus_visible` style builders (P:div.rs:970,1481) | button test (all controls via InteractiveElement) | MISSING-GO |
| `role(Role)` + `aria_label` (P:div.rs:1496,1518) | all except avatar/collapsible | MISSING-GO on div (G:div.go:1289 returns no role). The A11y runtime is PRESENT (`A11ySpec{Role,Label,…}` G:accessibility.go:305; `Accessibility()` wrapper :441). The Go `Role` enum has only 9 roles (G:accessibility.go:73-97). New roles needed: RadioButton, Switch, Link, Tab, TabList, Table, Row, Cell, ColumnHeader, RowGroup, Heading, Region, Group, Toolbar, RadioGroup, ProgressIndicator. Exposing them to the AT is **MISSING-NATIVE** (§4) |
| `aria_toggled(Toggled)`, `aria_selected`, `aria_expanded`, `aria_level`, `aria_position_in_set/size_of_set`, `aria_orientation`, `aria_numeric_value/min/max/step`, `aria_row_count/column_count/row_index/column_index` (P:div.rs:1589-1695) | checkbox, radio, switch, toggle, accordion, tabs, progress, slider, table, groups, toolbar | Snapshot/test side: MISSING-GO (`A11ySpec`/`Node` lack these fields; G:element.go:297-304). Native exposure: **MISSING-NATIVE** (§4) |
| `on_a11y_action(Increment/Decrement)` (P:div.rs, window.rs:7102) | slider | Runtime PRESENT (`A11yActionRegistration`, `A11yActions[P]` G:accessibility.go:198,377; action tags G:accessibility.go:131-149). There is no div fluent `OnA11yAction` → MISSING-GO (small). Advertising Click for clickable divs is not surfaced (G:a11yprovider.go:52-56) → MISSING-GO |
| `write_a11y_info` (test probe) | button, progress tests | PRESENT interface `A11yProperties.WriteA11yInfo(*Node)` G:element.go:346. `Node` only has Role/Value/Hidden → MISSING-GO fields |
| `img(ImageSource)` → `Img` (Styled + Interactive) | avatar | MISSING-GO element. Pipeline PRESENT: `UseImageAsset` G:imagecache.go:751, `Atlas.InsertImageFrame` G:image.go:280, `InsertPolychromeSprite` G:draw_text.go:176 |
| `canvas(prepaint, paint)` (used by `ElementExt::on_prepaint`) | button test, slider indicator | MISSING-GO (or base-local via `CustomElement[L,P]`, which is PRESENT) |
| `gpui::Empty` | accordion panel, slider drag views | PRESENT `Empty()` G:element.go:806 |
| `Entity<T>`, `cx.new`, `EventEmitter` + `cx.emit`, `cx.notify`, `entity.read/update/entity_id` | slider | PRESENT `NewEntity` G:entity.go:271, `Read/Update` :352,375, `DefineEvent/Emit` G:event.go:24,36, `Notify` G:context.go:65 |
| `window.listener_for(&entity, f)` (P:window.rs:6868) | slider | MISSING-GO (small; `Context.Listener` G:context.go:144 is the context-bound analogue) |
| `Bounds/Point/Pixels/px`, `Axis`, `Along`, `accesskit::Orientation` | slider, groups | Bounds/Point PRESENT. `Axis`/`Orientation` have no Go type → MISSING-GO (trivial; could live in base geometry) |
| `prelude::FluentBuilder` (`when`, `when_some`, `when_none`) | all, StateStyle | MISSING-GO. Go 1.27 generic methods make `When`/`WhenSome[T]` possible (precedent: `OnAction[A]` G:div.go:950) |
| Test harness: `add_window_view`, `simulate_click`, `simulate_event(KeyDown/Up)`, `simulate_keystrokes`, `debug_bounds`, `opened_url` | 13 modules' tests | Partly PRESENT: `NewTestApp` G:testapp.go:25, `NewTestWindow` G:testwindow.go:45, `DispatchInput` G:filedrop.go:379, `DispatchKeyEvent` G:key_dispatch.go:728, `DebugSelector` G:div.go:933. Click/keystroke-string simulators → MISSING-GO (convenience) |

## 4. MISSING-NATIVE evidence (strict)

**N1 — the AccessKit role set is fixed at 8.**
- Native `role_from_tag` maps only Window, GenericContainer, Button, TextInput, TextRun, StaticText(→Label), CheckBox and Slider. Everything else returns `ERR_BAD_VALUE` (gpui-go/reference/native/src/a11y.rs:598-613). The Go mirror is `internal/native/a11y.go:32-47`.
- The Go serializer errors on any other role and aborts the whole publish (G:a11yprovider.go:545-566, 673-676).
- Base needs 16 more roles (§3). They cannot reach UIA without a DLL change.

**N2 — the node record has no state properties.**
- `GpuiGoA11yNodeRecord` (internal/native/a11y.go:106-127) carries id, role, flags (hidden, has-bounds only, :84-88), actions mask, children, label, description, value, author id and bounds.
- The native decode sets exactly those (reference/native/src/a11y.rs:815-856).
- So toggled, selected, expanded, level, position/size-in-set, orientation, numeric value/min/max/step, row/column count/index and disabled cannot be exposed natively.

**Not a blocker — actions.** The header comment in G:a11yprovider.go:57-66 says Click and Decrement are unrepresentable, but it is stale. The encoder uses presence bits `1<<(tag-1)` (G:a11yprovider.go:601-606), which matches the native decoder (reference/native/src/a11y.rs:845-856: "all nine tags representable"). Advertising Click/Increment/Decrement is fine natively. Only the Go element layer needs to surface them.

**Consequence and decision needed.** Go-side behavior and tests (which inspect a Go snapshot/`Node`) can be complete with Go-only additions. Native UIA exposure for Base roles/states is blocked.
- Until the DLL changes, the publish path must not crash. Today a Base control rendered while an AT is attached would fail every publish.
- Options: (a) degrade unmapped roles in the serializer, e.g. to GenericContainer, and drop the unsupported properties; or (b) keep them out of the native tree.
- Either is a Go-side policy change that needs the user's decision. It is not a native workaround.

## 5. Deduplicated MISSING-GO core additions

1. **Full `StyleRefinement` + shared `Styled[Self]` vocabulary** in `gpui` with refine semantics.
   - Fields: background, border widths + color, corner radii, box shadows, text refinement (color/size/line-height/weight), opacity, overflow, flex direction/grow/shrink, margins, padding, inset, cursor, etc.
   - Move `Styled[Self]` from `authoring` into `gpui` (authoring can't import gpui; G:div.go:46 aliases the other way). Make `DivElement` itself use it.
2. **Div box painting** of borders, corner radii and shadows (scene already supports it: G:scene.go:521,541).
3. **Exported `Interactivity` record + generic `Interactive[Self]` helper**, so a component can forward listeners, focus and a11y into its base div (Rust `InteractiveElement::interactivity`).
4. **Click synthesis**: mouse down+up on the same hovered hitbox, and Enter/Space on the focused element (P:div.rs:3527-3560). `ClickEvent` becomes `Mouse|Keyboard` (with `Touch` optional).
5. **Mouse listeners**: `OnMouseDown(button, …)`, button-filtered `OnMouseUp`, `OnMouseUpOut` (and `OnMouseDownOut` for later modules).
6. **Drag**: `OnDrag(value, makeView)` with a threshold drag start; `OnDragMove[T]` with `DragMoveEvent[T]`.
7. **State styles**: `Hover`, `Active`, `FocusVisible` (needs per-element hover/active element state + hitbox hover).
8. **Div a11y builders**: `Role`, `AriaLabel`, `AriaToggled(Toggled)`, `AriaSelected`, `AriaExpanded`, `AriaLevel`, `AriaPositionInSet`, `AriaSizeOfSet`, `AriaOrientation`, `AriaNumericValue/Min/Max/Step`, `AriaRow/ColumnCount`, `AriaRow/ColumnIndex`, `OnA11yAction`.
   - Extend `Role` (16 roles), `A11ySpec` and `Node` with these fields.
   - Advertise Click for clickable divs.
   - The serializer policy for natively unmapped roles/properties is a §4 decision.
9. **`Window.UseKeyedState[T](key ElementID, cx, init) Entity[T]`**.
10. **Composite `ElementID`** (child-of-id + name, name + integer).
11. **`Img(ImageSource)` element** (Styled + Interactive).
12. **`Canvas(prepaint, paint)` element** (or a base-local `CustomElement`).
13. **Small helpers**:
    - `LineHeight` fluent + `Relative()` length.
    - `FlexRow/FlexCol`/`ItemsCenter` aliases.
    - `When/WhenSome/WhenNone` generic fluent helpers.
    - `Axis`/`Orientation` types.
    - `Window.ListenerFor`.
14. **Test conveniences**: `SimulateClick(point)`, `SimulateKeystrokes("enter space")`, add-window-with-view, `OpenedURL` recording.

## 6. Go authoring proposal for `Button`

**Today:**
- `authoring.Styled[*Button]` embedded by value + `authoring.BindStyled(&Button{})` keeps `*Button` through style chains. Its method set is only 6 methods (authoring/README.md, authoring/style.go:64-147).
- `examples/hello/main.go` uses only `gpui.Div()` fluent calls.
- `DivElement` is a separate hand-written builder (G:div.go:601-1270) and does not embed the helper.
- Go 1.27 generic methods are already in use (G:div.go:950), so `When`/`WhenSome[T]`/`OnAction[A]` can live on the generic helpers.

**Proposal: three value-embedded generic helpers in `gpui`, bound once in the constructor.** `DivElement` is rebuilt on the same helpers, so there is one vocabulary.

```go
// gpui (core additions #1, #3)
type Styled[Self any] struct{ /* self, anchor, refinement StyleRefinement */ }      // all style methods return Self
type Interactive[Self any] struct{ /* self, anchor, it *Interactivity */ }          // OnClick/OnMouseDown/Hover/Role/Aria*/TrackFocus/TabIndex/... return Self
type Parent[Self any] struct{ /* self, anchor, children []AnyElement, gate */ }     // Child/Children/TryChildren return Self
func Bind[V any, S interface{ *V; StyleBase() *Styled[S] }](s S) S                  // binds whichever helpers S embeds
func (i *Interactive[Self]) BindTo(it *Interactivity)                                // forward into the base div's record
func (d *DivElement) Interactivity() *Interactivity

// base
type Button struct {
    gpui.Styled[*Button]
    gpui.Interactive[*Button]
    gpui.Parent[*Button]
    id       gpui.ElementID
    base     *gpui.DivElement
    states   ButtonStyles
    disabled, selected, focusable bool
    label    *string; role RoleOverride; focus *gpui.FocusHandle
    tabIndex int64; tabStop bool
    onClick  func(*gpui.ClickEvent, *gpui.Window, *gpui.App)
}
func NewButton(id string) *Button {
    b := &Button{id: gpui.NameElementID(id), base: gpui.Div().ID(id), focusable: true, tabStop: true}
    gpui.Bind(b)
    b.Interactive.BindTo(b.base.Interactivity()) // Rust: interactivity() -> base.interactivity()
    return b
}
func (b *Button) Disabled(v bool) *Button { b.disabled = v; return b }
func (b *Button) OnClick(f func(*gpui.ClickEvent, *gpui.Window, *gpui.App)) *Button { b.onClick = f; return b } // shadows promoted Interactive.OnClick (outer depth wins)
func (b *Button) Styles(f func(*ButtonStyles)) *Button { f(&b.states); return b }
func (b *Button) RenderOnce(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
    style := ResolveStyle(b.Style(), b.selected, &b.states.selected, b.disabled, &b.states.disabled)
    d := b.base.Flex().ItemsCenter().Justify(gpui.AlignContentCenter).LineHeight(gpui.Relative(1))
    if r, ok := b.role.Resolve(gpui.RoleButton); ok { d = d.Role(r) }
    // ...track focus via w.UseKeyedState(b.id, cx, NewFocusHandle), mouse-down stopper when disabled, OnClick when enabled
    return d.Children(b.TakeChildren()...).Refine(style).IntoElement()
}
func (b *Button) IntoElement() gpui.AnyElement { return gpui.OwnRecipe(b) }
// Usage: NewButton("save").Px3().Rounded(gpui.Rem(0.25)).Hover(func(s *gpui.StyleRefinement){...}).Disabled(true).Child("Save")  // stays *Button
```

Rules and hazards:
- **No method-name overlap between helpers.** Two embedded helpers exposing the same method at equal depth makes the selector ambiguous (a compile error at the call site). The Styled/Interactive/Parent method sets must be disjoint. `Refine` and `Style` belong to Styled only.
- **Control-specific overrides are deliberate.** `OnClick` (Button's handler semantics), `ID` (Radio/Tab rebind the base id) and `TrackFocus` (Accordion/Table forward) are defined on the outer type and shadow the promoted method. Document this, because Go has no `override` keyword and a typo silently adds a new method.
- **Copies.** The copy check (`anchor`) must cover all three helpers. Base components have to be pointer-only, like the existing contract.
- **Two style layers stay separate.** Interactive state styles (`Hover`/`Active`/`FocusVisible`) live in the Interactivity record, so they forward to the base div. The instance `Styled` refinement is stored on the component and applied last via `Refine(resolved)`. This mirrors Rust, where `Button.style` is separate from `base.interactivity().base_style`.
- **StateStyle** = `struct{ gpui.Styled[*StateStyle] }`. Rust's `styles(|s| s.disabled(|st| st.opacity(0.5)))` becomes the callback-mutation form `Styles(func(*ButtonStyles))` / `Disabled(func(*StateStyle))`, which avoids value-copy hazards with bound helpers.
- **Order of operations differs.** Rust consumes `self` in render. In Go, `RenderOnce` runs on the stored pointer, and `OwnRecipe` single-use semantics apply (docs/authoring-decision-round.md "Child conversion").

## 7. Port hazards (slice-specific)

- **Click and keyboard semantics are the core of 10 test suites.** Exactly one keyboard click per Enter/Space, with KeyDown+KeyUp pairing as in P:div.rs:3527-3560. Port the pin's logic rather than reinventing it.
- **Disabled-policy divergence must be preserved per control** (§2 table). Do not unify it.
- **Slider**:
  - Drag needs `OnDrag`/`OnDragMove` plus `on_mouse_up_out` release.
  - Bounds come from a prepaint canvas child (`SliderIndicator`). This needs a canvas and absolute positioning.
  - `handle_release` emits only after `dragging` was set by `update_value_by_position` (U:slider.rs:351, 384-390).
  - Logarithmic validation panics. Keep these as Go panics, matching the `should_panic` test.
- **Toolbar roving focus** relies on `focus_next/prev` order across tab groups. Go's tab-stop map exists (G:div.go:1488-1494), but the toolbar handle is `tab_stop(false)` + `track_focus`, so check that `ContainsFocused` sees descendants.
- **Tuple element ids** (switch track, slider thumb/bar) affect a11y node identity and element-state keys. Without composite ids, use stable string encodings and record the deviation.
- **Avatar is blocked on an `Img` element.** It needs image loading (async asset + loading/fallback states), which is the largest single core item for this slice.
- **A11y tests in Go** must assert against a Go `Node`/snapshot. `write_a11y_info` into a role-preset `Node` (button/progress tests) needs `Node` to carry the new properties.
- **The upstream known gap stays a gap.** Button has no `aria_disabled` (U:button.rs:553-556). Do not "fix" it in the port.
- **Keyed focus handles need a release path.** Go `FocusHandle` has an explicit `Release()` (G:focus.go), while Rust drops it when keyed state is evicted. `UseKeyedState` (MISSING-GO #9) has to release or dispose its entity, and the handle inside it, when the key is not rendered in a frame. Otherwise every Button/Checkbox/Radio/Switch/Toggle/Link/Toolbar instance leaks a handle.
- **Second-pass verification (independent re-read).** The following were re-checked and hold:
  - click listeners are never dispatched (no `ClickEvent{` literal anywhere in gpui/);
  - native `role_from_tag` covers only 8 roles (reference/native/src/a11y.rs:598-612), and the Go `a11yRoleTag` errors on the rest (G:a11yprovider.go:545-566);
  - the actions mask uses presence bits, so all 9 actions are representable (reference/native/src/a11y.rs:845-856; G:a11yprovider.go:601-606);
  - the node record has no state fields (internal/native/a11y.go:106-127; flags are Hidden and HasBounds only, :84-88).

  One caveat: this evidence is the in-repo native *source*. Confirm that the shipped frozen artifact was built from the fixed (presence-bit) decoder revision before relying on Click advertisement.
- **Base-module dependencies (other researcher):** state_style (`StateStyle`, `resolve_style`), styled (`RoleOverride`, `StyledExt::refine_style`/`h_flex`), observe/test_support (`ObservedElement`, `TestSupportExt`), component_traits (`Selectable`), element_ext (`on_prepaint`), geometry (`AxisExt`), motion (`MotionReveal`).
