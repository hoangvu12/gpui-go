# 23 - ShowModal

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 9 (Claude Code)
Blocked by: 14, 21, 22
Triage: ready-for-agent

**What to build:** Window.ShowModal opens an overlay as modal: everything outside becomes inert, focus moves by the dialog focusing steps, focus returns to the previous element on close, and closedby controls light dismiss and Escape.

**Blocked by:** 14 (Core Tab navigation, focus fixup and Inert), 21 (Top layer and light dismiss), 22 (Close watchers and Escape)

**Status:** ready-for-agent

- [ ] Fixtures for focusing steps and focus restore match Chrome's dialog.showModal().
- [ ] Outside content can't be focused, clicked or reached by the accessibility tree while modal.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **API:** `Window.ShowModal(OverlayHandle) bool` opens a top-layer entry as a modal dialog; `TopLayer.ClosedBy(ClosedBy)` (any / closerequest / none; the auto default = closerequest for a modal) and `TopLayer.DialogFocus(FocusHandle)` name the policy and the dialog element's own focus slot; `DivElement.AutoFocus()` marks the autofocus delegate; `OverlayHandle.OnCancel` is the dialog's preventable cancel listener.
- **Files:** the modal core (the blocked document's inertness, the focusing steps, the focus restore, closedby, the light dismiss, the watcher wiring) in `gpui/modal.go`; the record/registration fields and the modal-kind forcing in `gpui/toplayer.go` (the entry at `overlayRecord`, `finishOverlayFrame`, `runShowRules`, `topLayerDrawable.RequestLayout` registering the content path at request time); the div's inert half in `gpui/div.go` (`inert := d.inert || inertPrepaintDepth(w) > 0 || modalInertnessBlocks(...)`, `registerDivElementDispatch`'s modal focus entries, `AutoFocus`); the a11y half in `gpui/accessibility.go` (`beginElementA11yNode`); the always-cancelable dialog watcher in `gpui/closewatcher.go`; the frame's `modalFocusEntries` in `gpui/focus.go`; the light-dismiss call after the popover walk in `gpui/mouseevent.go` (`dispatchMouseClick`).
- **Fixtures:** `internal/parity/fixtures/modal` — `modal-focus` (the first focusable, the modal Tab scope, the pass-out blur, the Escape restore), `modal-autofocus`, `modal-dialog-focus` (the dialog-itself fallback), `modal-dismiss` (closedby=any light dismiss + restore), `modal-closedby-none` (neither gesture closes; the settle stays open with the backdrop), plus the wrong view (`DismissWithoutModal`, the plain-Show overlay) and its two discrimination tests. Unit tests in `gpui/modal_test.go` (the focusing steps, the inert outside, Escape, closedby variants, the prevented cancel, Hide's restore, `TestA11yModalBlocksOutsideSubtree`).
- **Key citations:** `html_dialog_element.cc:553-616` (showModal: the top layer, the modal mark, the inert recomputation, the watcher, the popover hide-until, the focusing steps); `style_resolver.cc:563-583` + `document.cc:8841-8854` (the root's inherited html_inert with the modal element's escape, ActiveModalDialog); `html_dialog_element.cc:660-682` (SetFocusForDialog: the autofocus delegate, the first keyboard focusable, the dialog itself, the cleared focus); `:206-223` (the close's focus restore); `:240-254` (closedby's auto default); `:330-360` + `mouse_event_manager.cc:262-267` (the closedby=any light dismiss beside the popover walk); `:531-542` + `close_watcher.cc:246-249` (the disabled watcher, AllowCancel::kAlways); `html_dialog_element.cc:497-531` (the watcher's cancel forwarding onto the dialog).
- **Limitations:** the dialog cancel is a synchronous Go callback (`OnCancel`), not a script-visible DOM event — the fixtures prove a prevented cancel through the missing `toggle:closed` entry, not a `cancel` transcript kind (the harness's per-dispatch `defaultPrevented` marking cannot carry the cancel event's own state). The focusing steps run at the entry's first frame (the port's tree is frame-resolved), so the frame's focus listeners see one move, not Chromium's inline showModal dispatch. A click on the port's plain backdrop div falls through to the inert page (empty hit chain, mouse-focus blur) where Chrome's `::backdrop` propagates its pointer events onto the dialog element (its mouse focus lands on the dialog) — unobservable in the fixtures (the light dismiss's close+restore overwrites the intermediate focus before any key listener runs), recorded here as a divergence; the fixtures narrow the dialog's `data-events` to keydown/keyup/toggle accordingly.

