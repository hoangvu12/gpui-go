# 72 - Base overlays rebuilt on core primitives

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 9 (Claude Code)
Blocked by: 21, 22, 23, 24, 25
Triage: ready-for-agent

**What to build:** Base's popover, dialog, sheet, select, combobox and tooltip use the core top layer, close watchers, ShowModal, Anchored and the tooltip hook instead of their own dismissal, Escape bindings, focus trap, liveness list and positioner.

**Blocked by:** 21 (Top layer and light dismiss), 22 (Close watchers and Escape), 23 (ShowModal), 24 (Tooltip hook), 25 (Anchored to an element, with fallbacks)

**Status:** ready-for-agent

- [ ] Base's overlay tests pass on the core primitives.
- [ ] The per-component Escape bindings, focus trap and popup liveness list are deleted.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-25, orchestrator 9 (verification): merged f76327b as 6c4bed1. One conflict in base/select.go: I kept 72's picker close watcher together with 20's `SelectNone()` on the trigger. The full broad batch set passed on the merged tree except:
  - spellspec `TestSpellcheckCheckNeverBlocksTheUIThread` (a load-timing gap); it passed 3/3 on re-run and as a whole package.
  - clipspec `TestClipboardPowerShellNativeInterop`: the OS clipboard was held by another process, and a plain `Set-Clipboard` from PowerShell failed the same way, so it's environmental.
  The diff also fixes a core bug: `finishOverlayFrame` never reset `registered`, so unmounted overlays stayed in the top layer forever. Behaviour changes (backdrop press now reports Cancel, as in the DOM; the focusing steps only see focusables with element IDs) are listed in the Answer. Resolved.

## Answer

**What each component now uses**

- **Popover** — already on the TopLayer (ticket 21); its Escape dismissal now also comes from the entry's close watcher (ticket 22) instead of the `escape`→`CancelAction` binding: the watcher's close hides the entry, whose toggle runs the state's own close (the focus restore and the open-change announcement). `popoverInit` keeps only Enter/Space; the dismissal-event subscription and the focus lifecycle stay (U:popover.rs, not core-ruled yet). New test: `TestPopoverEscapeClosesThroughTheCloseWatcher`.
- **Dialog (and AlertDialog)** — a core modal: a pass-through `TopLayer` entry opened with `Window.ShowModal` (ticket 23), `ClosedBy(any|closerequest|none)` mapped from `CloseOnBackdropPress`/`CloseOnEscape`, `DialogFocus` on a zero-size focus-tracked node inside the host, Escape through the entry's watcher (`OverlayHandle.OnCancel` = the dialog's cancel decision, the veto), a backdrop press through the `closedby=any` light dismiss, and the close steps funnelled through the entry's toggle. The host keeps Base's own `Deferred(Anchored(0,0))` window pinning so a dialog still mounts anywhere in the tree. The `TopLayer` entry replaces the focus trap (Tab follows the inert document), the focus restore and the focusing steps.
- **Sheet** — the same modal shape: a pass-through TopLayer entry + `ShowModal` (no deferred wrapper; it still paints in place per ticket 42, and the gallery's `gpui.Deferred` wrapper is gone), Escape through the watcher, an overlay press through the light dismiss, both running `sheetClose` from the entry's toggle; `OverlayClosable` picks closedby any/closerequest.
- **Select / Combobox** — the picker's Escape dismissal is a core close watcher held in keyed state while open (created at the first open render, destroyed on close, released with the element): an unhandled Escape signals the newest watcher, which runs the exact old close steps (dismiss → open change → trigger refocus). The `escape` bindings are gone from `selectInit`/`comboboxInit`, and the `OnAction(CancelAction)` handler is gone with them. The app-owned picker surface gets light dismiss by rendering it through Base's Popover, as Chrome's picker does.
- **Tooltip** — the per-window `TooltipOverlay` provider is deleted; the lifecycle is the core hook (ticket 24): `DivElement.TooltipView` carries a `Tooltip` surface as the hint-layer content, placed with `Anchored().AnchorTo(<the invoker's id>)`. The gallery's tooltip page moved onto it. `TooltipPositioner` (ticket 25's core-Anchored wrapper) stays.
- **Popup / positioning** — `base/positioner.go` is deleted: the popup host places its content through the core's element-anchored `gpui.Anchored` (`AnchorBounds` over the measured trigger cell, the corner anchors mapped onto side/alignment, the offset onto the gap, `AnchorShift` as the only fallback — the corner rule clamps without flipping — and the window margin), so every anchored surface in Base resolves through the same rule as CSS anchor positioning. `ResolvedPosition`/`edgesAll` moved to popup.go; `PopupResolvedCorner` and `overlayIntoElement` (ex-focus_trap.go) stay.
- **Root** — the Tab/Shift-Tab bindings and `cycleFocus` are gone (with the focus trap): Tab is the core's default action (ticket 14), and a modal dialog's inert outside is the trap's replacement.

**Core seams this added (gpui)**

- `gpui/anchored.go`: `AnchoredElement.Occlude()` (a `HitboxBlockMouse` over the placed bounds, ahead of the children's own hitboxes — the DOM popover's hit-test coverage) and `OnResolved(func(Bounds))` (the placement observation Base's `OnPosition` reads, before the children prepaint).
- `gpui/tooltip.go`: the tooltip invoker records its bounds into the frame's anchor registry (turning the window's tracking on), so custom `TooltipView` content can `AnchorTo(<the invoker's id>)` on the first frame it renders.
- `gpui/toplayer.go`: `finishOverlayFrame` now resets every entry's `registered` mark at the start of the reconciliation — without it, a once-registered entry whose element stopped rendering (a page switch) lingered in the layer forever with its close watcher (the migration surfaced this: the lingering entry's show-rule hide fired a toggle into a released keyed state).

**Deleted code**

- `base/focus_trap.go` + `base/focus_trap_test.go` (the manager, the container element, `ActiveFocusTrap`), root.go's `cycleFocus`/`onActionTab`/`onActionTabPrev`/`rootTabAction`/`rootTabPrevAction` and the `tab`/`shift-tab` bindings, `lib.go`'s `focusTrapInit`.
- `base/positioner.go` + `base/positioner_test.go` (the side/corner strategies, `windowDecorations`/`frameInsets`/`tiling`, `Align`).
- The popup liveness list: `global_state.go`'s `deferredPopovers`/`deferredToken`/`DeferredPopover`/`GlobalStateRegisterDeferredPopover`/`GlobalStateIsInDeferredContext`, `PopoverState.deferredContext` and its release hook (the TopLayer entry's own registration is the liveness).
- The per-component Escape bindings: `popoverInit`, `dialogInit`, `sheetInit` (the whole initializer), `selectInit`, `comboboxInit`.
- The sheet's overlay press listener (`OverlayInteractive`, `DismissBeforeY`), the dialog's backdrop `OnAnyMouseDown` (`DismissBelowY` on both hosts), the sheet's focus trap and `Anchored` wrapper, `base/tooltip.go`'s provider (`TooltipOverlay`, `TooltipRequest`, `TooltipTransition`, `TooltipBuilder`, `TooltipRenderer`, the 500/300 ms delays — `overlayAfter`/`overlayDelay` moved to hover_card.go, their only remaining user).

**Behaviour changes to Chrome's (tests updated)**

- Escape now closes the newest overlay regardless of focus (ticket 22's rule) — it no longer needs focus inside the component's key context, and it no longer propagates past the sheet/select. Select/combobox Escape works from anywhere in the window (previously only from inside the select's context; the gallery's combobox search input gained working Escape).
- Escape no longer closes a popover with `OverlayClosable(false)` (no entry, no watcher — Chrome's popover=manual rule; it previously closed through the binding).
- Dialog `DialogChangeReasonBackdropPress` no longer occurs: the light dismiss and Escape are the DOM's single cancel event, so both report `Cancel`. `DismissBelowY`/`DismissBeforeY` are deleted (Chrome's light dismiss has no positional rule; a non-dismissing strip belongs inside the dialog): `TestSheetPointerAboveTheDismissCutoffIsIgnored` → `TestSheetEveryOverlayPressClosesWhereverItLands`.
- `CloseOnEscape(false)` maps to `closedby=none`: neither Escape nor a backdrop press closes (the old “no Escape, backdrop still dismisses” combination is not expressible in the attribute).
- A backdrop/overlay press dismisses through the light dismiss on ANY button (previously left-only) and the cancel veto still applies; the scrim click no longer focuses the host (it falls through to the inert page — ticket 23's recorded backdrop divergence), and a click on the dialog's own non-focusable popup area no longer focuses the host either.
- The dialog focusing steps now move focus on open (the first keyboard-focusable with an ID, else the dialog's own slot) and the modal close restores the previously focused element — Chrome's `showModal`/`close` behavior, new to Base. `TestDialogHostRegistersItsFocusTrap` → `TestDialogTabStaysInsideAndEscapeClosesAndRestores`; the sheet test's trap section became the Escape-key test (`SimulateKeystrokes("escape")` instead of `DispatchAction(CancelAction)`).
- Tooltip delays are Chrome's interest delays (250 ms in / 150 ms out) with the grace period, Escape and keyboard-focus triggers — the provider's 500/300 ms and its app-driven visibility are gone (the gallery's “pinned open” demo with it).
- Dialog/sheet paint order: the TopLayer entry paints above every non-overlay deferred draw (popovers opened later still paint above; plain deferred popups at priority 100 now paint below a modal — the DOM's top-layer order, previously dialogs painted below all popups).

**Files**: base/{dialog,alert_dialog,sheet,select,combobox,popover,popup,tooltip,hover_card,root,lib,global_state}.go, deleted base/{focus_trap,positioner}.go(+tests), tests base/{dialog,sheet,alert_dialog,popover,popup,tooltip,select,combobox,app,window}_test.go; gpui/{anchored,tooltip,toplayer}.go; examples/base-gallery/{pages_overlays,capture_test,gallery_test}.go. No new parity fixture: every Base overlay behaviour here is covered by the core fixtures (modal, popover, closewatcher, tooltip, anchor).

**Chromium cited (only where the core tickets had not)**: the customizable-select picker is a popover=auto shown with the select as its invoker — `select_type.cc:611` (`popover_->setAttribute(kPopoverAttr, "auto")`), `:774` (`ShowPopoverInternal(select_)`), so Escape goes through the picker's popover close watcher and the hide refocuses the select — `select_type.cc:852-860` (HidePopup's `kFocusPreviousElement`) and `:182-196` (PopoverElementForAppearanceBase::HidePopoverInternal's “Focus the select when the popover is hidden”); the picker's light-dismiss coverage (`select_type.cc:445-455`).

**Limitations**

- The dialog focusing steps only see focusable elements WITH an element ID (`modalFocusEntries` key on the element-id path — the port's identity model; id-less focusables are invisible to the first-keyboard-focusable walk, the same shape as the tooltip's id requirement). The dialog-itself fallback still works.
- The entry's per-frame registration reset makes an unmounted overlay's entry drop SILENTLY (no toggle, no focus restore) — an app that unmounts an open modal without closing it skips the modal's focus restore; closing through the state (the closed render's Hide, Escape, the light dismiss) runs the full path.
- Two handle-less, focus-less dialogs on the same layer share one overlay entry (the stateless-view adapter's id path isolates by type name alone — the pin's own keyed-state approximation; pass a `DialogHandle` or `FocusHandle` to keep them apart; the same applies to two sheets without `FocusHandle`).
- The app's open state wins: a dialog still rendered open after a core close re-shows (the popover's overlay-sync semantics), re-running the focusing steps.
- The select's watcher is created at the first open RENDER (Chrome creates it at the picker's show, inside the user activation): an Escape before that render closes nothing (unobservable in practice — the picker cannot be interacted with before it renders).
- A plain deferred popup (not rendered through a popover/TopLayer entry) has no light dismiss — the app's picker surface must use the Popover (as Chrome's picker does) or wire its own dismissal.
