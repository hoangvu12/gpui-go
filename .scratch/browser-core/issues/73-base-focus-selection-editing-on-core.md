# 73 - Base focus, selection and editing on core primitives

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 9 (Claude Code)
Blocked by: 14, 20, 26, 28
Triage: ready-for-agent

**What to build:** Base drops its Tab binding, selection model, copy/cut/paste/select-all actions and hard-coded caret blink and selection colours, using the core's versions.

**Blocked by:** 14 (Core Tab navigation, focus fixup and Inert), 20 (Text selectable by default, with UserSelect), 26 (Edit actions and the HTML clipboard), 28 (Text scale to rem, caret blink and highlight colours)

**Status:** ready-for-agent

- [x] Base tests pass with the core versions; the removed code is gone.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

**What Base now uses per area**

- **Tab (14/72):** nothing in Base binds the root Tab anymore (72 deleted it); the survey of what was left found two sites, both kept deliberately: (1) the input/editor's `tab`/`shift-tab` → IndentInline/OutdentInline rows — only a multi-line editor registers the handlers, so the editor inserts an indent and consumes the key (the textarea in tab-capture mode, the rule a code editor follows), while a single-line input's unhandled action propagates and the core's Tab default action moves focus out (the plain textarea's rule — new test `TestInputTabMovesFocusOutOfASingleLineInput` / `TestEditorTabInsertsAnIndentAndKeepsFocus`); (2) the TimeField's segment rows (Chrome's time input keeps Tab walking its focusable subfields, out of the control after the last). Dock, lists, tabs and the other widgets bind no Tab — their TabStop focus handles ride the core's `TabStopMap` default action.
- **Selection (19/20):** already the core's — `base/text_selection.go` / `selectable_text.go` / `auto_scroll.go` / `touch_selection.go` are the alias files 19 left, `text_boundary.go` installs Base's upstream word/line pair over the core's seam, and the TextView bridges through the adapter. No duplicated Base selection model remains; the aliases stay because they are Base's public API.
- **Edit actions (26):** `inputElementHandler` (the input element's paint-time handler, `base/input_state_edit.go`) now implements `gpui.EditActionHandler`, so Copy/Cut/Paste/SelectAll arrive from the core's default-action arm on Chrome's Windows key table. The handler runs the upstream bodies (the multi-selection join, the atomic-undo cut, the line-per-selection paste, the masked gate) and returns prevented so the core's default write/mutation is skipped — the key stays consumed either way. The `input::Copy/Cut/Paste/SelectAll` action types and descriptors are gone: `base.Copy/Cut/Paste/SelectAll` and `CopyAction/CutAction/PasteAction/SelectAllAction` alias `gpui`'s `edit::*` (source-compatible; the registered name changes, e.g. `input::Copy` → `edit::Copy`). Base's OnAction listeners stay on the aliased descriptors for programmatic dispatch. New tests: `base/input_edit_actions_test.go`.
- **Caret blink (28):** already the core's — `base/input_blink_cursor.go` reads `SystemPreferences().CaretBlinkInterval` each tick (0 = steady, no timer); nothing hard-coded remains (the 500 ms constant is the hostless default's mirror of the record).
- **Selection colours (28):** already the core's — unset input selections and SelectableText resolve from `App.SelectionColors` (`base/system_selection.go`, `input_state_style.go`); no hard-coded wash remains.
- **Focus ring (48/12):** Base paints no ring of its own, so every Base control now shows the core's default `:focus-visible` ring (keyboard focus only, the focus-origin rule of 12) with no double ring. `FocusableExt`'s unpainted `FocusRing(false)` maps onto the core's opt-out through the new `base.FocusRingOutline(element, enabled)` helper (pins `OutlineNone` when disabled, leaves the default auto kind when enabled) — the seam a future component with its own ring uses; new test in `values_test.go`.

**Deleted code**

- `inputInit`'s `!mac` letter rows (ctrl-a/c/x/v → input::SelectAll/Copy/Cut/Paste, U:input/base/state.rs:243-250). The macOS cmd rows stay (the core's table carries only the !Mac rows).
- The `input::SelectAll/Copy/Cut/Paste` payload types and descriptors (`base/input_state.go`) — replaced by the core aliases.
- `rootInit`'s !mac ctrl-c binding and its `copyKeys` plumbing (`base/root.go`) — the core's copy command owns the keystroke off macOS. `onActionCopy` and the Root's OnAction listener stay for programmatic dispatch.
- `textInit`'s !mac ctrl-c row (`base/text_state.go`) — the window-selection stand-in covers the TextView's copy. Select-all stays (see below).

**Kept on purpose (per-site decisions)**

- TextView select-all (cmd-a/ctrl-a + OnAction): the core's select-all is editable-only — the window-selection stand-in wires Copy only (19's recorded limitation) — so deleting it would kill the feature.
- The editor's Tab (tab-capture), the TimeField's Tab (segment walk), the input's macOS cmd rows, Shift+Delete → Delete (26's recorded Base divergence; the binding wins over the core's Shift+Delete → Cut).

**Behaviour changes to the core/Chrome rule (tests updated)**

- The window-selection copy (Root/TextView, nothing editable focused) is now **untrimmed** — the core copies the selection exactly; the pinned U:root.rs:264-273 trimmed it. `TestRootCopyActionPropagatesOnlyWithoutSelectedText` → split: on darwin the old action path stays (unchanged test, skipped off darwin); off darwin `TestRootCopyKeystrokeRunsTheCoreEditCommand` (no action dispatches, selection or not — the command fires regardless, only the write gates) and `TestRootCopyKeystrokeStaysUnboundWithoutSelectionState`.
- Base inputs gain the Windows alternates the upstream list never bound: **Ctrl+Insert = copy, Shift+Insert = paste** (26's core table; pinned by `TestInputEditCommandsRunThroughTheCoreDefaultAction`).
- A copy/cut/paste key pressed **during an IME composition** now runs nothing on Base inputs (the core's IsComposing gate, 26/18), where the old binding path dispatched the action.

**Files**: base/{input_state,input_state_edit,root,text_state,text_view,time_field,component_traits,lib,text_selection}.go, gpui/text_selection_copy.go (the stale "Base keeps its binding" comment), tests base/{root_copy,input_edit_actions,values}_test.go. No new parity fixture: every Base behaviour here is covered by the core fixtures (editkeys, outline-*, tabnav-*, userselect-*, selection-*) or by the Base/clip tests; no deliberately-wrong view was needed.

**Chromium cited (only rules the core tickets had not)**

- The textarea Tab rule: `editor_key_bindings.cc:67-76` — the kRawKeyDown pass returns false for text-insertion commands ("Tab that changes focus" falls through to the default handler; a tab-inserting Tab belongs to the keypress arm), so a plain textarea's Tab moves focus and the frame's keydown chain routes it to `DefaultTabEventHandler` (`keyboard_event_manager.cc:453-455`); the tab-capture insertion is the editor case the Base editor follows.
- The time input's segment Tab: `date_time_field_element.cc:232-238` (DateTimeFieldElement::SupportsFocus — every subfield is a focusable shadow element, so the sequential focus order the default Tab handler follows runs segment to segment and out of the control after the last) and `:77-110` (DefaultKeyboardEventHandler — the arrows are the field's own segment moves).

**Limitations**

- The Root's copy command flow has no direct test: a hostless TestApp cannot observe the write (the command's dispatch through the default-action arm is pinned by gpui's `TestEditActions*` on a fake handler, and the write itself by clipspec); the Base test pins the binding's deletion and the no-dispatch.
- The seam prevents the core's own mutations, so the DOM's beforeinput/deleteByCut preview never fires on Base inputs (Base has no beforeinput surface; the mutations run with their upstream undo intents instead).
- The action-name change (`input::*` → `edit::*`) is observable to an app matching actions by name string; the typed surface is source-compatible.
- The macOS cmd rows and the TextView's trimmed OnAction copy keep the old behaviour on darwin / on programmatic dispatch (the keystroke path off darwin is the core's).
