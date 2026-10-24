# 21 - Top layer and light dismiss

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 8 (Claude Code)
Blocked by: 14
Triage: ready-for-agent

**What to build:** Overlays live in an ordered top layer (popover, hint and modal kinds, with a backdrop slot). A click outside closes popovers above the clicked one, treating the invoker and nested popovers as inside, as HTML popovers do.

**Blocked by:** 14 (Core Tab navigation, focus fixup and Inert)

**Status:** ready-for-agent

- [x] A failing test first reproduces Base's nested-popover dismissal fault (spec risk).
- [x] Fixtures with nested popovers and invokers match Chrome's light-dismiss results.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 8 (verification): merged db01f36 into main (batch with 07+46 and 26). Harness conflicts with 18's composition fields were resolved by keeping both: the `state` field in the EventRecord, the transcript script and the readable diff. With GOFLAGS=-p=4, `go test -p 1 -count=1` passes for ./gpui/, ./internal/parity/..., layoutspec, clipspec, ./base, focusspec, imespec, actionspec, winhostspec, authorspec, inspectorspec, dropspec, richtextspec, spellspec, tsxgen, ./gpui/script/..., ./examples/... and ./cmd/.... Build, vet and gofmt are clean, and the TSX types are unchanged. Citation spot-checked: mouse_event_manager.cc:262-267 (light dismiss for a connected click pair). The Base nested-popover fault test (`TestPopoverClickInsideNestedChildKeepsTheParentOpen`) failed first and passes now. 33's Anchored containing-block gap is closed (Anchored uses RequestContainerLayout).

## Answer

- **API:** `gpui.OverlayHandle` (`NewOverlayHandle(w)`, `Show/Hide/Toggle/IsOpen`, `OnToggle` — the DOM's popover open state, one ordered top layer per window) and `gpui.TopLayer(handle)` (`Kind(OverlayPopover|PopoverManual|Hint|Modal)`, `Invoker(id)`, `Backdrop(child)`, `OnToggle`, `Child`, `PassThrough` for overlays the app already defers, e.g. Base's popup surfaces). Toggle notifications are queued like the DOM's toggle task and drained at the end of each input dispatch and each frame (`drainOverlayToggles`), so they land after the click's own listeners in the transcript.
- **Files:** `gpui/toplayer.go` (the layer, the element, the registration/reconciliation, the light-dismiss walk and show rules), `gpui/mouseevent.go` `windowInputState.pressHitPaths` and `dispatchMouseClick` (the light dismiss runs right before the click listeners, for every click including empty-space body clicks), `gpui/key_dispatch.go` `dispatchKeyEvent` + `gpui/helpers_frame.go` `finishFrame` (drain points), `gpui/element.go` (the per-frame invoker registry, the ticket-25 anchor registry's pattern), `gpui/div.go` (named elements record their paths), `gpui/window.go` (`w.overlays`). `gpui/anchored.go` `RequestLayout` now goes through `RequestContainerLayout` (the ticket-33 note: an Anchored is a containing block for absolute children of Static divs — `gpui/toplayer_test.go` `TestAnchoredIsAContainingBlockForAbsoluteChildrenOfStaticDivs`).
- **The Base fault and its fix:** Base dismissed every popover independently on a mousedown outside its own hitbox, so a press inside a nested child popover (rendered deferred, outside the parent's box) closed the parent — reproduced failing first by `base/popover_test.go` `TestPopoverClickInsideNestedChildKeepsTheParentOpen`. Minimal fix in `base/popover.go`: the content registers through `gpui.TopLayer(handle).PassThrough().Kind(OverlayPopover).Invoker(p.id.Name)` instead of `OnMouseDownOut`, with the state's overlay handle synced in `transitionTo` and closed through `Dismiss` on the layer's toggle. All existing Base tests pass.
- **Fixtures:** `internal/parity/fixtures/popover` (`popover-nested`, `popover-dismiss`, `popover-backdrop`, Chrome 155 records in `internal/parity/testdata/popover-*`): nested popovers, clicks inside the child (both stay open — the fault case), inside the parent (child closes), on the invoker (light dismiss keeps it, the activation toggles) and outside (everything closes, top down); the backdrop fixture also pins the popover `::backdrop`'s `pointer-events: none` (a scrim click falls through and light-dismisses the whole layer) and the scrim's paint order. The harness gained the `toggle` transcript kind with the entry's `state` field (`log.Toggle`, the recorder instrumentation, `docs/browser-parity-harness.md`). The discrimination view (`DismissNoInvokerLink`, the parent's invoker link dropped) fails Tier B and Tier C with readable diffs.
- **Key citations (third_party/blink/renderer, the research checkout):** `core/input/mouse_event_manager.cc:262-267` (the light dismiss runs while creating the click event, comparing the pointerdown and pointerup targets); `core/html/html_element.cc:3215-3227` (`HandlePopoverLightDismissForClick` — hide everything above the related popover only when press and release relate to the same one, the drag rule); `html_element.cc:3141-3172` (`FindTopmostRelatedPopover` — the nearest open popover ancestor vs the nearest invoker's popover, the higher on the stack wins), `:2924-2996` (the two walks), `:2321-2440` (`HideAllPopoversUntil`), `:1950-2144` (`ShowPopoverInternal` closes the non-ancestors above the topmost popover ancestor), `:2999-3135` (`FindTopmostPopoverAncestor`); `core/dom/document.cc:8703-8748` (`AddToTopLayer` appends, `::backdrop` directly below its element); `platform/runtime_enabled_features.json5:4153-4154` (`LightDismissFromClick` stable); `core/html/resources/html.css:1966-1977` (the popover UA style the pages reset) and `:2012-2015` (the popover `::backdrop` is `pointer-events: none`).
- **Limitations:** the show rules run at the entry's first registration (a frame after Show — the port only knows the tree per frame), not synchronously inside Show; nested `TopLayer` elements resolve their containing block against the parent popover's box until `position: fixed` lands (ticket 35), so nested overlays render at the root and link through `Invoker` (the DOM shape the fixtures use); the hint kind is one ordered layer (a hint show closes hints above it, autos are not auto-closed, but Chromium's separate hint-stack positioning is not modeled); Escape, close watchers and modal behaviour stay with tickets 16/22 as instructed; focus restore on popover close stays Base's own (ticket 14's fixup covers removals).
