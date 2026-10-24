# 17 - Context-menu event

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 8 (Claude Code)
Blocked by: 09
Triage: ready-for-agent

**What to build:** Elements get OnContextMenu from right-button up, the Menu key (on key-up) and Shift+F10 (on key-down), with a keyboard-invoked position at the focused element.

**Blocked by:** 09 (Default actions run after key listeners)

**Status:** ready-for-agent

- [x] Event-log fixtures for each trigger match Chrome's order on Windows.
- [x] PreventDefault on contextmenu suppresses nothing else in the core.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 8 (verification): merged 976cd25+0b21604 into main. Conflicts with 34/36 (fixture import lists, the harness guide, and gpui/div.go's child prepaint) were resolved: div.go keeps 14's inert scope wrapping 34's `stickyChildrenLast` order. With GOFLAGS=-p=4, `go test -p 1 -count=1` passes for ./gpui/, ./internal/parity/... (tabnav, contextmenu and every existing fixture), ./base, authorspec, inspectorspec, dropspec, winhostspec, richtextspec, spellspec, tsxgen and ./examples/...; build, vet and gofmt are clean. Citation spot-checked: web_frame_widget_impl.cc:1112-1124 (on Windows the Menu key triggers on key-up). The recorder now sends rawKeyDown for text-less keys; the existing key transcripts are unchanged.

## Answer

- **API:** `DivElement.OnContextMenu(f func(*ContextMenuEvent, *Window, *App))` (an element ID required, like OnClick) and `ContextMenuEvent{Kind ContextMenuEventKind (mouse/keyboard), Position, Modifiers}` (`gpui/contextmenu.go`). The fluent surface is untouched (OnContextMenu is a plain InteractiveElement method like OnClick).
- **Right-button up:** after the whole up tail — listeners, capture release, click/auxclick, boundary settling — and *regardless of the click having been swallowed* (no default-prevented gate), the event targets the element under the release position and bubbles to the root (Chromium `WebFrameWidgetImpl::HandleMouseUp` → `MouseContextMenu` → `SendContextMenuEvent`, web_frame_widget_impl.cc:1253-1260/:1215-1245, the Windows `context_menu_on_mouse_up` default, web_preferences.h:64; the hit test at the release, event_handler.cc:2183-2228).
- **Menu key (key-up) / Shift+F10 (key-down):** in the default-action position — after the key listeners, suppressed by preventDefault — the unmodified Menu key fires on KEY UP and Shift+F10 (Shift alone held) on KEY DOWN (Windows' `is_context_menu_key_triggering_event`/`is_shift_f10`, web_frame_widget_impl.cc:1112-1141); the key is consumed (kHandledSystem). The event targets the focused element's chain (focused-first, bubbling) with the position at the focused element's centre (`ShowNonLocatedContextMenu`/`GetFocusedElementRectForNonLocatedContextMenu`, event_handler.cc:2245-2316) or the viewport corner + 1px with nothing focused (:2320-2323).
- **Registrations:** the keyboard path reads the dispatch node's registration (prepaint, like the keyboard click — `nodeContextMenu` with bounds), the mouse path the frame's path-keyed registrations (paint, like the click listeners); a reused cached view carries both (view_cache.go `moveDispatchNode`).
- **PreventDefault:** sets the window's default-prevented flag only — nothing in the core reads it after the dispatch (there is no core menu), the acceptance's check. The synthesized follow-up shares the triggering platform dispatch's prevented flag (the activation click's documented coarseness), so the prevented fixture binds only the `contextmenu` kind.
- **Recorder fix that fell out:** a key step without text now sends the CDP `rawKeyDown` type — Chrome's Shift+F10 arm requires kRawKeyDown, and the untextured `keyDown` type never triggered it; the existing key fixtures' transcripts are unchanged (verified by re-recording tabnav-order).
- **Files:** `gpui/contextmenu.go` (the event, both registrations, both dispatches), `gpui/div.go` (the field, the prepaint/paint registrations), `gpui/key_dispatch.go` (the node member + SetContextMenu), `gpui/focus.go` (the frame's registrations), `gpui/interact.go` (shouldInsertHitbox), `gpui/key_default_action.go` (the keyboard arm), `gpui/view_cache.go`, `internal/parity/fixture.go` (the `contextmenu` kind, `log.ContextMenu`/`ContextMenuPreventDefault`, the menu/f10 DOM key names), `internal/parity/recorder.go` (the rawKeyDown type and VKEY_APPS).
- **Fixtures:** `internal/parity/fixtures/contextmenu` — `contextmenu-mouse` (the order after the whole up tail), `contextmenu-menu-key` (after the keyup), `contextmenu-shift-f10` (between the halves), `contextmenu-prevented` (the prevented state + the following Tab still moving focus); the wrong-view test `TestPreventedFailsWhenTheListenerDoesNotPrevent`.
- **Key citations:** `web_frame_widget_impl.cc:1253-1260` (the up-tail dispatch, "regardless of if the click was swallowed"), `:1215-1245` (MouseContextMenu), `:1112-1141` (the Windows menu-key arms), `event_handler.cc:2183-2228` (SendContextMenuEvent), `:2245-2316` (the keyboard position at the focused element), `:2320-2323` (the corner fallback), `web_preferences.h:64` (context_menu_on_mouse_up on Windows).
- **Limitations:** the keyboard trigger's position is not transcript-observable (the harness's entries carry kinds, targets, key identity and focus only), so the focused-element-centre rule is pinned by the code, not a fixture; the port has no menu to show, so the event's only default is the app's own; Mac's mousedown trigger arm (web_frame_widget_impl.cc:1192-1204) is not ported — this port is Windows-only, where the release is the trigger.
