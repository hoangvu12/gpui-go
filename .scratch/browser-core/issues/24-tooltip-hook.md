# 24 - Tooltip hook

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 9 (Claude Code)
Blocked by: 21, 22
Triage: ready-for-agent

**What to build:** Any element can have a tooltip that opens on hover and on keyboard focus, with Chrome's delays and grace period, in the hint layer, with a plain default look.

**Blocked by:** 21 (Top layer and light dismiss), 22 (Close watchers and Escape)

**Status:** ready-for-agent

- [ ] Timed event-log fixtures for hover and focus triggers, using the test clock.
- [ ] Escape closes it (via close watchers).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **API:** `DivElement.Tooltip(text)` (the plain default look; the text also becomes the element's a11y description), `DivElement.TooltipView(builder)` (custom content), `DivElement.OnTooltipToggle(listener)`; the constants `interestDelayStart` / `interestDelayEnd` (250ms / 150ms).
- **Files:** the whole hook in `gpui/tooltip.go` — the per-window tooltip records (the interest invokers, keyed by the element's id path), the show/hide timers (`SpawnDelivering` + `TaskRun.Sleep`, the virtual clock in tests), the hover source (the hover boundary sweeps), the focus source (a paint-time focus diff), the grace period's upstream arm (`paintTooltipSurfaceListener` on the tooltip's own surface), the Escape interest loss (`dropAllTooltipInterest`, before the close-watcher signal in `gpui/key_default_action.go`), the frame-end reconciliation (`finishTooltipFrame`), and the window's own rendering of the tooltip's top-layer entry (`buildTooltipLayer`/`tooltipContent`, injected in `gpui/render.go` drawRoots — the inspector element's pattern). The div plumbing: `gpui/div.go` (the `tooltip` field, the prepaint hook), `gpui/interact.go` (the paint hover listener), `gpui/toplayer.go` (the `tooltipRec` surface arm in `topLayerContent`), `gpui/focus.go` (the frame's `tooltipSeen`), and `gpui/interact.go`'s `refreshPaintHitTestAfterDeferred` (the deferred round's hitboxes join the paint-time hit test — a bug the tooltip's surface listener found: deferred-registered boundary checks read a hit test that lacked the deferred hitboxes).
- **Fixtures:** `internal/parity/fixtures/tooltip` — `tooltip-hover` (the show delay, the grace period moving between the element and the tooltip, the hide delay; key markers between the long waits pin the toggle positions), `tooltip-focus` (the keyboard trigger), `tooltip-look` (the default look's box + pixels, the page pinning the port's measured box because the two text systems shape "Tip" 0.33px apart), `tooltip-escape`, plus the wrong view (`WrongLookView`, the wrong text) and its two discrimination tests. Unit tests in `gpui/tooltip_test.go` (the delays at the virtual clock, the grace, the focus trigger, Escape, the default look's placement, the element-gone reconciliation).
- **Key citations:** `element.h:306-308` (kDefaultInterestDelayStartSeconds 0.25 / End 0.15); `element.cc:13280-13335` (ScheduleInterestGained/LostTask), `:13467-13511` (the hover/focus schedule arms and the upstream cancel — the grace), `:2125-2158` (the pointerover/out and focusin/focusout sources), `:2047-2063`/`:2068-2123` (InterestGained/InterestLost → the popover show/hide), `:9170-9183` (LoseInterestInAllElements) + `keyboard_event_manager.cc:616-628` (the Escape order: the interest loss before the close-watcher signal); `html_element.cc:2187` (the implicit anchor). The NATIVE title tooltip is browser UI (ui/views, not in the checkout, not recordable headless) — the interest invoker is the recordable renderer-side mechanism whose delay constants the port adopts.
- **Limitations:** Chrome's headless renderer coalesces its interest timers (the hidden-page wake-up alignment), so the recorded toggles land up to ~1s late — the fixtures use 2s waits and key markers, pinning the SHAPE (nothing before the marker, the toggle between the markers) while the exact 250/150ms constants are pinned by the unit tests; a tighter record would need the timer throttling disabled, which no tried flag does. The default look's box is text-driven, so the look fixture pins the page's geometry to the port's measured box (the 0.33px glyph-advance drift is the text fixtures' known noise). The tooltip needs an element ID (the record keys on the id path) and the invoker link needs the element's ID name; an id-less element gets no tooltip. The interest/loseinterest events (cancelable, preventable) have no port-side listeners — the port's tooltip has no author-visible interest seam, only the toggle.

