# Slice 01: Infra + Overlays inventory (gpui-base @ c0820693 → gpui-go)

Read-only research, 2026-10-19. This builds on and re-verifies [A-infra-overlays.md](A-infra-overlays.md). I spot-checked its Go claims against the current tree. It also adds the files that pass skipped: test_support.rs, macos_accessibility.rs, and the a11y native-role finding.

Path prefixes:
- **U:** `upstream-base/ce/crates/base/src`
- **G:** `gpui-go/gpui`
- **R:** `gpui-go/reference/ce-source/crates/gpui/src`
- **N:** `gpui-go/reference/native/src` (the frozen DLL source)

Classes:
- **PURE-GO** means it can be written today in a base package over the exported core API.
- **CORE+** means it needs a Go-side addition or export inside package `gpui`.
- **NATIVE** means the frozen DLL would have to change.

Totals: 41 files, **12,221 lines, 147 tests** (`#[test]` + `#[gpui::test]`).

## 1. Summary table

| Component | Lines | Tests | Class | Missing core APIs (see section 2 ids) | 3rd-party |
|---|---:|---:|---|---|---|
| lib.rs | 248 | 0 | PURE-GO | (aggregates `init`) | none |
| root.rs | 355 | 1 | CORE+ | C10 (TryGlobal, View→Entity downcast `window.root::<Root>()`), C9 (Relative, Inset0, full Refine) | none |
| theme.rs | 157 | 1 | CORE+ (small) | C10 (TryGlobal/HasGlobal) | serde, schemars |
| theme_tokens.rs | 274 | 1 | PURE-GO | (FontWeight named type: trivial, base-local OK) | palette (WithAlpha), serde, schemars |
| styled.rs | 285 | 1 | CORE+ | C9 (full Styled vocab: borders, radii, shadow, flex_col, font_weight…; div paint of radii/borders) | gpui_macros (inspector reflection, debug-only) |
| element_ext.rs | 31 | 0 | CORE+ | C5 (Canvas) | none |
| state_style.rs | 103 | 4 | CORE+ | C9 (StyleRefinement layering over all fields) | none |
| actions.rs | 39 | 1 | PURE-GO | none (`DefineAction`/`DefineUnitAction` G:action.go:465/483) | serde (Deserialize derive) |
| event.rs | 60 | 0 | CORE+ | C8 (OngoingScroll + restrict_scroll_to_axis; ClickEvent.click_count) | none |
| global_state.rs | 200 | 2 | CORE+ (small) | C10 (TryGlobal/HasGlobal) | web_time |
| observe.rs | 52 | 1 | PURE-GO | none in prod (identity alias U:observe.rs:9) | none |
| geometry.rs | 151 | 0 | PURE-GO | (Axis: trivial, base-local) | serde, schemars |
| measure.rs | 48 | 0 | PURE-GO | none | tracing (1 call, U:measure.rs:46) |
| async_util.rs | 7 | 0 | PURE-GO (drop) | none | smol / async_channel |
| component_traits.rs | 114 | 2 | PURE-GO | none (Go interfaces + helpers) | none |
| focus_trap.rs | 228 | 0 | PURE-GO (ergonomics: C9 mixins) | none hard: CustomElement G:element.go:615, WeakFocusHandle/ContainsFocused G:focus.go, TrackFocus G:div.go:685 | none |
| positioner.rs | 792 | 15 | CORE+ (exports) | C4 (`LayoutBounds`, `ViewportSize`, `WindowDecorations`/`ClientInset`), C10 (Anchor, Bounds helpers, Half) | none |
| popup.rs | 325 | 3 | CORE+ | C3 Deferred, C5 Canvas, C7 UseKeyedState, C1 frame loop | none |
| popover.rs | 589 | 6 | CORE+ | C7, C8 (OnMouseDown, OnMouseDownOut, Role), C10 (SubscribeIn, App.Notify), C1 | none |
| hover_card.rs | 406 | 3 | CORE+ | C2 timers, C7, C8 (OnHover, OnMouseUpOut) | web_time |
| tooltip.rs | 347 | 3 | CORE+ | C2 timers, C3 Deferred (priority), C8 Role | none |
| dialog.rs | 910 | 6 | CORE+ | C3 Deferred, C4 Anchored + ViewportSize, C7, C8 (OnAnyMouseDown, Role, ClickEvent fields), C9 | smallvec |
| alert_dialog.rs | 352 | 2 | CORE+ | via Dialog; C8 Role | smallvec |
| sheet.rs | 270 | 4 | CORE+ | C4 Anchored + ViewportSize, C8 OnAnyMouseDown, C9 | none |
| toast.rs | 1014 | 11 | CORE+ | C5 Canvas, C1 (refresh_windows actually redraws), C9 (Invisible, Relative), C8 Role | web_time |
| reduce_motion.rs | 244 | 5 | PURE-GO (+C10 DefaultGlobal) | `ReduceMotion`/`SetReduceMotion` present (G:app.go:282/287) | windows (SystemParametersInfoW), objc2-app-kit (mac), ashpd + futures (Linux) |
| animation.rs | 397 | 4 | CORE+ | C6 AnimationElement/`with_animation` | smallvec |
| motion.rs | 1873 | 43 | CORE+ | C7 UseKeyedState, C1 rAF loop, C2 clock | web_time |
| motion/easing.rs | 234 | 1 | PURE-GO | none | none |
| motion/keyframes.rs | 140 | 0 | PURE-GO | none | none |
| motion/presence.rs | 151 | 0 | CORE+ | C7, C1 | none |
| motion/reveal.rs | 120 | 0 | PURE-GO (+C1 at runtime) | none: WithElementState G:element.go:1379, RequestElementLayout G:element.go:1530, LayoutAsRoot G:element.go:654, WithContentMask G:element.go:1165 | none |
| motion/sequence.rs | 256 | 0 | CORE+ | C7, C1 | none |
| motion/stagger.rs | 37 | 0 | PURE-GO | none | none |
| motion/timing.rs | 174 | 0 | PURE-GO | none | none |
| index_path.rs | 102 | 3 | PURE-GO | (composite ElementId: string concat workaround) | none |
| list_settings.rs | 17 | 0 | PURE-GO | none | serde, schemars |
| macos_accessibility.rs | 84 | 0 | N/A (skip) | macOS-only (`cfg(target_os="macos")`, U:lib.rs:36) | objc2, objc2-app-kit, objc2-foundation, raw-window-handle |
| test_support.rs | 421 | 0 | CORE+ (+NATIVE-adjacent, see section 3) | C11 (a11y Node state fields, `Interactivity.compute_style`, ContentMask/ViewportSize exports) | none |
| history.rs | 285 | 10 | PURE-GO | none | none |
| undo_history.rs | 329 | 14 | PURE-GO | none (`Instant` → `time.Time`) | web_time |

Counts: PURE-GO 21 (incl. N/A), CORE+ 20, NATIVE 0 hard (1 soft, a11y publication).

## 2. Core additions needed (consolidated; all in package `gpui`, all Go-side)

I verified each item against the current tree (grep plus read). Go-side evidence is G:, the Rust source is R:.

**C1. Real frame loop / invalidation.** Blocks every overlay and animation at runtime.
- `RequestAnimationFrame` only sets `refreshRequested` (G:imagecache.go:958 → G:focus.go:525). The flag is consumed only by the explicit `DrawWindowFrame` (cleared at G:render.go:153).
- The `effRefresh` handler calls `drawHook.DirtyWindows()` and discards the result (G:app.go:501-506).
- `Context.Notify` never dirties a window.
- No production `InvalidateRect` exists; the only one is in a test file.
- Needed: notify→dirty-window tracking, consuming the refresh flag, `Window.OnNextFrame`, a public `Window.Refresh`, and a test-side auto-draw on `RunUntilParked`/`AdvanceClock`.
- Rust: `Window::refresh` R:window.rs:2287; `on_next_frame` R:window.rs:2613; `request_animation_frame` R:window.rs:2633; `simulate_next_frame` R:window.rs:2643; `Context::on_next_frame` R:app/context.rs:292.

**C2. Production wall-clock timers.**
- The scheduler clock (`clockMs`, G:executor.go:44) advances only through `advanceClock` (G:executor.go:475). Its sole caller is `TestApp.AdvanceClock` (G:testapp.go:54). The production `drive()` only runs `sched.run()` (G:win32host_windows.go:2377-2384).
- So `TaskRun.Sleep` (G:executor.go:625) and `App.Now` (G:imagecache.go:852) never move in a real app.
- Needed by tooltip (U:tooltip.rs:163-185), hover_card (U:hover_card.rs:138-153), toast timeouts, undo grouping, and every motion sample.
- Rust: `BackgroundExecutor::timer` / `advance_clock` R:executor.rs:212. A `Context.SpawnIn` helper is optional (R:app/context.rs:676).

**C3. `Deferred` element + `Window.DeferDraw(priority)`.**
- Grep finds zero `Deferred` in G:.
- The scene order floor already exists (`Scene.RaiseOrderFloor` G:scene.go:354, `SceneKernel.RaiseOrderFloor` G:scene_kernel.go:137). It is just never called from a deferred pass.
- Used at U:popup.rs:190, U:tooltip.rs:238, U:dialog.rs:548.
- Rust: R:elements/deferred.rs:7 (`deferred`), :89 (`priority`), 265 LOC; `Window::defer_draw` R:window.rs:4310.

**C4. `Anchored` element + window geometry exports.**
- `Anchored` is absent in G:.
- `layoutBoundsOf` is unexported (G:element.go:1324). The viewport is only in the unexported `windowDrawState.viewport` (G:element.go:949). No decorations or client-inset exist.
- Used at U:positioner.rs:386,393-394,405, U:dialog.rs:549, U:sheet.rs:123.
- Rust: R:elements/anchored.rs:27 (398 LOC; `snap_to_window` :68); `viewport_size` R:window.rs:2779; `client_inset` R:window.rs:2828; `window_decorations` R:window.rs:2833; `Decorations` R:platform.rs:581; `layout_bounds` R:window.rs:5303.
- On Windows, decorations can return `Server` (no inset).

**C5. `Canvas` element + public paint primitives.**
- `Canvas` is absent in G:.
- Custom elements cannot paint, because the frame scene is reachable only through the unexported `currentFrame(w)` (G:div.go:362).
- Used at U:popup.rs:147, U:element_ext.rs:21, U:toast.rs:557.
- Rust: R:elements/canvas.rs:10 (95 LOC).

**C6. `AnimationElement` / `AnimationExt.with_animation` + `Animation{duration, easing, repeat}`.**
- Absent in G:.
- Used at U:animation.rs:219-224.
- Rust: R:elements/animation.rs:15 (`Animation`), :71 (`AnimationExt`), :150 (`AnimationElement`).

**C7. `UseKeyedState` / `UseState` + public current-global-id + eviction release.**
- `WithElementState` exists (G:element.go:1379), but `pushElementID` is unexported (G:element.go:1111).
- `retainElementStates` drops unaccessed state with no release hook (G:element.go:1451).
- No auto-notify of the current view on state change.
- Used at U:popup.rs:133, U:popover.rs:303, U:hover_card.rs:216, U:dialog.rs:403, U:motion.rs:290/384/636, U:motion/presence.rs:52, U:motion/sequence.rs:144.
- Rust: `use_keyed_state` R:window.rs:4093; `use_state` R:window.rs:4122; `with_global_id` R:window.rs:2935; `with_element_state` R:window.rs:4140.

**C8. Div interactivity + a11y props.**
- `DivElement` has OnClick/OnMouseMove/OnMouseUp/OnScrollWheel/OnAction/KeyContext/TrackFocus/Occlude/TabGroup only (method list from G:div.go).
- `divElement.A11yRole` always returns none (G:div.go:1289).
- `ClickEvent` has only `Button` (G:div.go:542).
- `OngoingScroll` is absent.
- Rust:
  - mouse handlers: `on_mouse_down` R:elements/div.rs:996; `on_any_mouse_down` :1039; `on_mouse_down_out` :1101; `on_mouse_up_out` :1128
  - `on_hover` :1899; `role` :1496; `restrict_scroll_to_axis` :1768
  - `ClickEvent` R:interactive.rs:290; `OngoingScroll` R:gestures.rs:49
- Go `Role` constants stop at Slider (G:accessibility.go:77-96). Base needs Dialog, AlertDialog, Tooltip and Alert: U:popover.rs:356, U:alert_dialog.rs:199, U:tooltip.rs:31, U:toast.rs:658. See section 3 for publication.

**C9. Full Styled vocabulary + component mixins.**
- `authoring.Styled[Self]` has 6 builders: Flex, PaddingX, Px3, Gap, Gap2, Opacity (authoring/style.go:123-144).
- `StyleRefinement` has 5 fields (authoring/style.go:47-53).
- Div paint hard-codes `CornerRadii: Corners{}`, `BorderWidths: Edges{}` (G:div.go:1660-1666), although `PaintQuad` supports both.
- No `When`/`WhenSome` (FluentBuilder) and no Interactive/Parent mixins.
- Rust: `Styled` R:styled.rs:25 (1,086 LOC) plus the macro-generated helpers in `gpui_macros/src/styles.rs` (1,967 LOC; e.g. `invisible` :466, `relative` :514).
- This is the largest single porting hazard: every overlay is "Styled + Interactive + Parent delegating to an inner Div".

**C10. Small API gaps.**
- Globals: `HasGlobal`/`TryGlobal`/`DefaultGlobal[T]`. Only `SetGlobal` and the panicking `Global[T]` exist (G:app.go:575). Rust: R:app.rs:2106/2120/2139.
- `App.Notify(EntityID)` is unexported (G:app.go:515). Rust: R:app.rs:2920.
- View→`Entity[T]` downcast / `Window.Root[T]`. Rust: R:view.rs:53, R:window.rs:2272.
- Window-bound `SubscribeIn`. Go has `Event.Subscribe`, which has no window. Rust: R:app/context.rs:355, R:window.rs:2549.
- `listener_for` (R:window.rs:6868) is base-local via `Entity.UpdateIn`.
- `Anchor` enum + `Bounds::from_anchor_and_size`/`top_center`…: R:geometry.rs:2209, :868. Base-local is OK.
- Composite (tuple) ElementIds.

**C11. Test-harness surface (for porting tests; not runtime).**
- `simulate_mouse_move`/`simulate_click`/`simulate_keystrokes`/`add_window_view` have no Go equivalents. Rust: R:app/test_context.rs:313/827/872/816/947.
- They can be built base-local over `Window.DispatchInput` (G:filedrop.go:379) + `DrawWindowFrame` + `WindowDebugBounds` (G:render.go:284).
- test_support.rs also needs:
  - Go `Node` with state fields: toggled, selected, expanded, disabled, label, supported actions. Go `Node` has only Role/Value/Hidden (G:element.go:297-304). Used at U:test_support.rs:302-318.
  - an exported `Interactivity.compute_style` (R:elements/div.rs:3882; used at U:test_support.rs:355-358).
  - `ContentMask`/`ViewportSize` exports.

## 3. Native gaps

**Hard gaps: none for runtime behavior.** Evidence:
- The Win32 host, invalidation, timers and SystemParametersInfoW are Go syscalls (G:win32host_windows.go, G:desktop_windows.go:77). C1 and C2 are Go work.
- The scene already carries radii, borders and shadows (`PaintQuad` G:paint.go:237-244, `BoxShadow` G:paint.go:268). The deferred-draw order floor is exported (G:scene.go:354). So C3, C5 and C9 are Go work.
- Hit-testing, focus, actions, keymaps and layout requests are all Go-side.

**Soft gap: accessibility publication to UIA (screen readers only).**
- The DLL's `role_from_tag` accepts only tags 1-8: Window, GenericContainer, Button, TextInput, TextRun, StaticText (published as accesskit Label), CheckBox, Slider. Anything else gets `ERR_BAD_VALUE` (N:a11y.rs:598-612).
- The Go mirror errors on unmapped roles rather than coercing them (G:a11yprovider.go:542-564; tags at internal/native/a11y.go:29-48).
- The node record carries no checked/selected/expanded/disabled/modal state. `flags` uses only bit 0 = hidden and bit 1 = has bounds (N:a11y.rs:181-200).
- So Base's `Role::Dialog`/`AlertDialog`/`Tooltip`/`Alert`, and the toggled/selected/expanded state that test_support reads, **cannot reach UIA without a DLL change**.
- Mitigation without native work:
  - Extend the Go `Role` enum and the Go `Node`/`A11ySpec` with these roles and states for the Go semantic tree and the tests. Snapshots are Go-side.
  - Change `a11yRoleTag` to publish unknown roles as GenericContainer. This is a policy change in Go only.
- Functional overlays are unaffected.

**macos_accessibility.rs:** macOS-only, so out of scope for this Windows port. It is gated by `cfg(target_os="macos")` (U:lib.rs:36,122).

## 4. Third-party usage (this slice)

| Crate | Where | What for | Go replacement |
|---|---|---|---|
| serde / serde_json | theme, theme_tokens, actions, geometry, list_settings | (De)serialize theme, actions, settings | `encoding/json` + struct tags |
| schemars | theme, theme_tokens, geometry, list_settings | JsonSchema derive (settings schema) | drop, or a JSON Schema lib (nothing in go.mod today) |
| palette | theme_tokens (`WithAlpha`, ~1 use) | alpha on colors | set `Hsla.A` |
| smallvec | dialog, alert_dialog, animation | small inline vecs | Go slices |
| web_time | global_state, hover_card, toast, motion, undo_history | `Instant`/`Duration` (wasm-portable) | `time` (but it must read the C2 clock in tests) |
| smol / async_channel | async_util (7 lines) | channel re-export | Go `chan` (drop file) |
| tracing | measure (1 `trace!`) | timing log | `log/slog` |
| windows | reduce_motion | `SystemParametersInfoW(SPI_GETCLIENTAREAANIMATION)` | `syscall`/user32 (core already binds SPI privately, G:desktop_windows.go:77) |
| objc2*, raw-window-handle | reduce_motion (mac), macos_accessibility | macOS | skip |
| ashpd + futures | reduce_motion (Linux) | XDG portal | skip |
| gpui_macros | styled.rs:76 | inspector reflection (debug only) | skip |

None of these requires native code.

## 5. Internal dependency order (what to port first)

Edges come from the `crate::` refs in each file. External-slice deps are marked [slice].

1. **Leaves (pure, no deps):** geometry (Placement/Side/Axis), actions, component_traits, index_path, history, undo_history, list_settings, measure, motion/timing, motion/stagger, motion/keyframes, animation (cubic_bezier/Lerp math part).
2. **motion/easing** (→ animation::cubic_bezier U:motion/easing.rs) → **motion.rs core** (Transition/Spring/Interpolate; → animation, easing, timing, keyframes) → motion/presence, motion/sequence, motion/reveal. Then the animation.rs `EffectTransition`, which needs C6.
3. **theme_tokens → theme.** theme.rs embeds `ScrollbarStyles/ScrollbarMode/ScrollbarMotion` [slice C] and `PlotMotion` [slice E2] (U:theme.rs, the `use crate::{PlotMotion, ScrollbarMode…}` line). Either port those value types first or stub them.
4. **styled (StyledExt, h_flex/v_flex)** and **state_style**. They need C9. Nearly every overlay imports `StyledExt`.
5. **observe / test_support**: needed by the dialog/sheet/toast tests (`TestSupportExt`). **element_ext** (needs C5; also `text_selection_scope` [slice E1]).
6. **global_state** (→ `text::TextViewState` [slice E], U:global_state.rs; `DeferredPopover` token) and **focus_trap**.
7. **reduce_motion**. It is independent but should land before motion runtime tests.
8. **positioner** (→ geometry; C4 exports) → **popup** (→ positioner, styled; C3/C5/C7).
9. **tooltip** (→ positioner, POPUP_PRIORITY), **hover_card** (→ popup), **popover** (→ popup, global_state, component_traits::Selectable, actions; tests use Button [slice B1]).
10. **dialog** (→ focus_trap, styled, actions, observe; `DialogClose::trigger` takes `crate::Button` [slice B1], U:dialog.rs:362) → **alert_dialog** (→ dialog). **sheet** (→ focus_trap, actions, styled).
11. **toast** (→ motion::spring, element_ext::on_prepaint, observe).
12. **root** (→ focus_trap::active_focus_trap, StyledExt, `input::Copy` [slice D], `TextSelectionLayer` [slice E1]) and **lib.rs init** last. `init` also calls combobox/color_picker/select/number_input/time_field/input/tree/text init from other slices (U:lib.rs:228-245).

Core work order that unblocks the most: **C9 + C8**, which every overlay compiles against. Then **C7 + C1 + C2**, which motion, popup and tooltip need at runtime and for tests. Then **C3/C4/C5**, which overlays need for layering. C6, C10 and C11 can land as they come up.
