# 14 - Core Tab navigation, focus fixup and Inert

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 8 (Claude Code)
Blocked by: 09
Triage: ready-for-agent

**What to build:** Tab and Shift+Tab move focus as a core default action in HTML tabindex order; a press on empty space blurs; focus moves off an element that disappears at the end of the frame; an Inert style makes a subtree unfocusable and unclickable and hides it from accessibility (D4).

**Blocked by:** 09 (Default actions run after key listeners)

**Status:** ready-for-agent

- [x] Event-log and focus fixtures for tab order (positive, zero, negative) match Chrome.
- [x] Removing the focused element moves focus to the body equivalent, as Chrome does.
- [x] Inert subtrees are skipped by Tab, hit testing and the accessibility tree.
- [x] Base still works (its own Tab binding may stay until ticket 73).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 8 (verification): merged 976cd25+0b21604 into main. Conflicts with 34/36 (fixture import lists, the harness guide, and gpui/div.go's child prepaint) were resolved: div.go keeps 14's inert scope wrapping 34's `stickyChildrenLast` order. With GOFLAGS=-p=4, `go test -p 1 -count=1` passes for ./gpui/, ./internal/parity/... (tabnav, contextmenu and every existing fixture), ./base, authorspec, inspectorspec, dropspec, winhostspec, richtextspec, spellspec, tsxgen and ./examples/...; build, vet and gofmt are clean. Behaviour changes toward Chrome: `FocusNext`/`FocusPrev` no longer wrap (Tab past the last stop blurs; the next Tab starts from the first stop), and body-focused key events now reach the root element's listeners.

## Answer

- **Tab order (D4):** `TabStopMap` now sorts the HTML sequential order — positive tab indexes ascending, then the zero bucket in tree order; a negative index (or `TabStop(false)`) is never a stop, and Tab from a *focused* excluded element searches in tree order (Chromium `focus_controller.cc:748-752`, `:1255-1283` `ShouldVisit`/`AdjustedTabIndex`, `:1347-1443` `Next/PreviousFocusableElement`).
- **Boundary (no wrap):** Tab past the last stop / Shift+Tab before the first *passes focus out* — `Window.FocusNext/FocusPrev` blur (the body equivalent) instead of wrapping, matching Chrome handing focus to the browser UI at the document scope's end (`focus_controller.cc:2216-2231`, `CanTakeFocus`/`TakeFocus`; headless records the body). The core public `FocusNext`/`FocusPrev` gained this shape (Base's questionnaire tests still pass).
- **Focus fixup (end of frame):** a focused handle that left the completed frame — removed, hidden or inert — blurs at frame completion (`helpers_frame.go` `finishFrame` → `focus.go` `fixupFocusedElement`; Chromium's synchronous node-remove fixup `document.cc:5809-5866` and the deferred `ClearFocusedElementIfNeeded` `:3261-3277`, driven from `local_frame_view.cc:3694`). The fixup only answers *removal* — a handle no frame ever carried (state-only handles) keeps the pin's behaviour. It also records the sequential focus navigation starting point (`document.cc:6308-6319`, `:6382-6395`), so the next Tab continues from the removed element's position.
- **Blur on empty press:** the mousedown's focus default — after the listeners, suppressed by preventDefault — walks the flat-tree ancestors of the press target (`mouseevent.go` `defaultMouseFocus`, hitbox records carry the focus handle; `mouse_event_manager.cc:537-636` `HandleMouseFocus`, `:615-621`'s SetFocusedElement(nullptr) "even with !element"), blurring when nothing focusable is on the chain.
- **Inert:** `DivElement.Inert()` (+ the fluent `Interactive.Inert`) — the subtree's prepaint registrations are suppressed (no tab stops, keyboard clicks, key listeners), its hitboxes are skipped by the hit test (`pointer-events: none`, `computed_style.h:1862-1867`/`2052-2056`), and its a11y nodes are hidden (`ax_node_object.cc:1368` `ShouldIgnoreForHiddenOrInert`). Inertness inherits down the subtree like CSS `interactivity` (`html.css:2143-2145`, `computed_style.h:2047-2049`); it still paints.
- **Body-focused keys (a divergence toward Chrome that fell out):** key events with nothing focused now dispatch through the root element's node (the body equivalent) instead of the pin's synthetic tree root (`key_dispatch.go` `bodyNodeID`) — the pin silently dropped the root element's listeners. Activation stays gated on a real focused element (the body has no activation behaviour).
- **Files:** `gpui/focus.go` (order, pass-out, fixup, starting point, `blurWithOrigin`), `gpui/mouseevent.go` (hitbox inert/hidden/focusHandle records, `hitTestAt` skip, `defaultMouseFocus`), `gpui/div.go` (`Inert`, the inert/hidden prepaint scope, the suppressed focus node), `gpui/interact.go` (`inertPrepaintDepth`/`withInertPrepaint`), `gpui/key_dispatch.go` (`SetFocusSuppressed`, `bodyNodeID`), `gpui/key_default_action.go` (the body-state activation gate), `gpui/helpers_frame.go` (the fixup call), `gpui/accessibility.go`+`gpui/div_a11y.go` (the hidden marking), `gpui/view_cache.go` (the reused node keeps the flag), `gpui/fluent.go`.
- **Fixtures:** `internal/parity/fixtures/tabnav` — `tabnav-order` (the D4 walk + both pass-out boundaries), `tabnav-negative` (the excluded element's tree-order rule), `tabnav-inert` (Tab skips the subtree, the click passes through to the root), `tabnav-removed` (the fixup + the starting point), `tabnav-blur` (the empty press); the wrong-view test `TestOrderFailsWhenTabIndexesAreFlattened` (the pin's DOM order must fail Tier B). A11y: `gpui/div_a11y_test.go` `TestA11yDivInertHidesTheSubtree`.
- **Key citations:** `focus_controller.cc:748-752` (the sequential-order contract), `:1347-1443` (next/prev, the negative arm), `:2216-2231` (the pass-out), `document.cc:5809-5866` (node-remove focus fixup), `:3261-3277` + `local_frame_view.cc:3694` (the deferred unfocusable fixup), `:6308-6319`/`:6382-6395` (the starting point), `mouse_event_manager.cc:537-636` (HandleMouseFocus), `event_handler.cc:998-1001` (the not-handled gate), `computed_style.h:1862-1867`, `:1894-1896`, `:2047-2049`, `:2052-2056` (inert's focusability/hit-testing), `html.css:2143-2145` ([inert] → interactivity), `ax_node_object.cc:1368` (a11y ignore).
- **Limitations:** the fixup's timing is the frame end (Chromium's removal fixup is synchronous inside the mutation; the observable — the next event sees the body — matches). A second forward pass-out from the same last element is nondeterministic in headless Chrome (the DevTools input path returns focus somewhere in between), so `tabnav-order` steps each boundary once. The mouse-focus default keeps the pin's innermost-arm mid-bubble approximation for the common case (an ancestor listener's later preventDefault cannot un-focus it).
