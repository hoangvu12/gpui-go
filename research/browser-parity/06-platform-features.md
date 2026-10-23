# 06 · Platform features: how the browser behaves, and how gpui-go differs

Ticket: `.scratch/browser-parity/issues/06-platform-features.md`. Researched 2026-10-23. Read only, nothing built.

Sources and pins:
- **Chromium** `0d4dcd4f` (sparse checkout, `%TEMP%\research-src\chromium`). Paths are relative to `third_party/blink/renderer/` unless they start with `ui/`. The `ui/views` tooltip and the `content/` prefs aren't in the checkout, so claims about them are [I].
- **gpui-go**, this worktree (`research/browser-parity` @ `8f5ad4d`): `gpui/`, `reference/native/src`, and `base/` where Base supplies a behaviour the core lacks. Nothing in this report touches `reference/native/src` except where noted. The renderer already draws wavy underlines (`gpui/scene.go:105-106`) and the a11y tree already carries a `Modal` flag (`gpui/accessibility.go:398-399`) [V].
- **Specs.** HTML (popover, dialog, close watchers, inert, select, title, spellcheck), CSS Position 4 (top layer), CSS Color 4 and Color Adjust 1, Media Queries 5, CSS Anchor Positioning 1, Clipboard API. A web-fetch helper read the spec pages; see the "Spec check" section at the end.
- **No overlap with 03.** Focus order, inert as a focus/hit-test flag, keyboard dispatch order and OS drag-and-drop are in [03](03-input-and-events.md) §2-§4. This report covers what sits on top of them. Reduced motion is in [02](02-motion-and-compositing.md) §7.

Markers: **[V]** means I read it in source. **[I]** means it's inferred and not traced end to end. Sizes: S ≤ 2 days, M ≤ 1 week, L ≤ 3 weeks, XL more than that. "Go" work is in `gpui/`. Nothing here needs a native (Rust) rebuild.

---

## Ranked gaps

| # | Gap | Core or component? | Work | Size |
|---|-----|-----|------|------|
| 1 | No top layer and no overlay stack. Each Base popup dismisses itself on *mousedown outside its own box*. The core has no auto-popover stack, no "close everything above the one you clicked", and doesn't treat the invoker as inside. | **Core**: top layer, popover stack, light dismiss | Go | M-L |
| 2 | No close-request stack. Escape is a key binding in each component's key context, so it only works when focus is inside that overlay, and stacked overlays don't close top-down. | **Core**: close watcher stack, Escape as a default action | Go | S-M |
| 3 | No modal state in the core. Modal dialogs are Base focus traps plus a backdrop hitbox. Nothing makes the rest of the window inert, and focus restore is per component. | **Core**: `ShowModal` (inert, focusing steps, restore). **Component**: the dialog's look | Go | M (after 03 §2) |
| 4 | High contrast, system colours, the accent colour, `prefers-contrast` and `prefers-reduced-transparency` aren't read. Only light/dark is, and only from the registry. | **Core**: a system-preferences and system-colour API with change events. **Component**: what the theme does with them | Go | S-M |
| 5 | No `title`-style tooltip. GPUI's `div.tooltip` wasn't ported. Base's tooltip uses 500 ms and has no keyboard trigger. | **Core**: a tooltip hook on elements, with hover and focus triggers and platform delays. **Component**: the tooltip's look | Go | S-M |
| 6 | No `contextmenu` event. Right-click is only a click with a button, and the Menu key and Shift+F10 do nothing. | **Core**: the event (mouse, Menu key, Shift+F10) with a position. **Component**: the menu | Go | S |
| 7 | No spellcheck service. | **Core**: an `ISpellChecker` service and marker painting. **Component**: when to check, suggestions UI | Go (COM) | M |
| 8 | Anchoring: the core's `Anchored` only switches corners or snaps. Side placement, flip and alignment live in Base's `Positioner`. Nothing hides a popup whose anchor scrolls out of view. | **Core**: anchor-to-element with flip/shift fallbacks | Go | S-M |
| 9 | Clipboard: text, images and files only. No HTML/RTF, no change notification. Copy/cut/paste keys are bound by Base, not the core. | **Core**: formats, change events, standard edit actions and their keys. **Component**: what an input does on paste | Go | S-M |
| 10 | Text size: rem is fixed at 16 px. The Windows "Text size" setting is ignored. | **Core**: read it and scale rem | Go | S |
| 11 | Caret blink and selection colours are hardcoded in Base (500 ms; accent at 40 % alpha). Windows' caret blink time (which can be "never") and the inactive-window selection colour aren't used. | **Core**: expose the platform values. **Component**: the caret and selection painting | Go | S |
| 12 | Select and menu keyboard rules (typeahead, Alt+Down/F4, arrows that change the value while closed) aren't anywhere. | **Component**, with a small core `TypeAhead` helper | Go | S |

Start with #2 and #6. Each is small and unblocks Base's overlays and menus. #1 and #3 are the core of "a browser-like core without components". They build on 03's inert flag and focus fixup (03 §2). #4, #10 and #11 share one new piece: a `SystemPreferences` record refreshed on `WM_SETTINGCHANGE`/`WM_SYSCOLORCHANGE`.

---

## 1. The top layer, the popover stack and light dismiss

**Spec.**
- [HTML §popover](https://html.spec.whatwg.org/multipage/popover.html): `popover=auto` joins a stack and closes the other autos that aren't its ancestors. `manual` never light-dismisses. `hint` has its own stack.
- [HTML §popover-light-dismiss](https://html.spec.whatwg.org/multipage/popover.html#popover-light-dismiss): clicking outside closes popovers *above* the nearest popover related to the click target. That's either a popover ancestor or the popover's invoker.
- [CSS Position 4 §top-layer](https://drafts.csswg.org/css-position-4/#top-layer): top-layer elements paint above everything, in insertion order. Their containing block is the viewport.

**Chromium.**
- **Stacks.** `ShowPopoverInternal` closes popovers until it reaches the new one's topmost popover ancestor (DOM parent *or* invoker), then pushes onto the auto or hint stack (`core/html/html_element.cc:2004-2144`) [V].
- **Top layer.** The popover joins the top layer (`:2180`). `Document::AddToTopLayer` appends to an ordered list, and a `::backdrop` sits just below its element (`core/dom/document.cc:8703-8748`) [V].
- **Focus.** It records the previously focused element *only for the first popover in the stack* (and for each `<menulist>` level), so closing a nested popover doesn't bounce focus (`html_element.cc:2135-2140, 2219-2222`) [V].
- **Escape.** Each popover gets a close watcher (`:2146-2157`) [V]; see §2.
- **Light dismiss is on click, not pointerdown.** `LightDismissFromClick` is `stable` (`platform/runtime_enabled_features.json5:4153-4154`) [V]. `HandlePopoverLightDismissForClick` finds the related popover for the pointerdown target and for the pointerup target. Only if they're the same does it hide everything above it (`html_element.cc:3215-3227`) [V].
  - "Related" means the nearest open popover ancestor *or* the popover the node invokes, whichever is higher on the stack (`:3140-3170`) [V].
  - So clicking the trigger of an open popover doesn't close and reopen it, and a drag that starts inside and ends outside doesn't dismiss.
- **Surprise: menus are becoming platform elements.** This checkout has `<menulist>`, `<menuitem>`, `<menubar>` and submenus (`core/html/html_menu_*.cc`) behind `MenuElements: experimental` (`runtime_enabled_features.json5:4464-4465`) [V]. Opening a submenu from a menu item creates a **safe triangle** with 10 px padding (`core/html/menu_safe_triangle.cc:28, 44`; called from `html_element.cc:2207-2212`) [V]. The safe triangle is the hover corridor that stops a submenu closing while the pointer moves diagonally towards it.

**gpui-go.**
- **Paint order only.** The core has `Deferred` with integer priorities (`gpui/deferred.go:62-67, 230-297`) and `Anchored` (`gpui/anchored.go:74-92`) [V]. That covers the top layer's *paint* order but nothing else. There's no list of open overlays, no ordering by when they opened, and no backdrop concept.
- **Dismissal is per component.** Base's popover closes on `OnMouseDownOut` (`base/popover.go:397-402`) [V], a capture-phase **mousedown** whose hitbox doesn't geometrically contain the pointer (`gpui/interact.go:881-890`) [V]. Consequences [I, from the code, not run]:
  - A press inside a *child* popover (rendered deferred, outside the parent's box) also dismisses the parent.
  - A press on the trigger of an open popover closes it on mousedown. The trigger's click may then reopen it.
  - A drag from inside to outside dismisses on the press.
  - Every open popover decides independently; nothing closes "everything above".
- Base keeps a liveness list of open popups (`base/global_state.go:148-165`) [V], but it's only used to answer "is any popup open".
- `hasActivePrompt` is a stub returning false (`gpui/interact.go:530`) [V].

**Gap.** Add a core **top layer**: an ordered per-window list of open overlays. Each entry has an owner (the invoker's element id), a kind (`auto`, `manual`, `hint`, `modal`), an optional backdrop, a close watcher (§2) and a restore-focus target.
- Light dismiss runs once per click in the core, using Chromium's rule: compare the related overlay at down and at up, and close everything above it.
- "Related" uses the element tree plus the invoker link, so nested popovers and their triggers work.
- Paint the list above the deferred draws, in list order.

Go, M-L. Base's popover, select, hover-card and tooltip then become thin presenters.

---

## 2. Close requests (Escape) as a core default action

**Spec.** [HTML §close-requests-and-close-watchers](https://html.spec.whatwg.org/multipage/interaction.html#close-requests-and-close-watchers): on desktop, Esc is a close request. It goes to the newest close watcher (group). That watcher's `cancel` may be prevented, then it closes.

**Chromium.**
- Esc is a **default action**: `DefaultEscapeEventHandler` runs only if the page didn't handle keydown (`core/input/keyboard_event_manager.cc:455-456, 618-631`) [V]. It also drops all "interest" (hover-card) state (`:628`).
- `EscapeKeyHandler` requires a trusted, not-default-handled Esc (`core/html/closewatcher/close_watcher.cc:159-164`) [V]. `Signal` closes the newest group's watchers, newest first, stopping at the first one whose cancel is prevented (`:166-180`) [V].
- Grouping (`:83-150`) is anti-abuse, so a page can't trap the Back button. An app framework doesn't need it.

**gpui-go.**
- Escape is bound per component: popover (`base/popover.go:36`), dialog (`base/dialog.go:136`), select (`base/select.go:18`), combobox (`base/combobox.go:19`) [V].
- Key bindings resolve on the *focus* path, so:
  - an overlay that doesn't hold focus (a tooltip, a hover card, a popover whose content isn't focused) can't be closed by Escape;
  - with two overlays open, the focused one closes, not the newest [I].

**Gap.** Add a core `CloseWatcher` per top-layer entry (§1), with `OnCancel` (preventable) and `OnClose`. Unhandled Escape (after listeners and bindings, 03 §4) signals the newest watcher. Drop the grouping. Go, S-M.

---

## 3. Modal dialogs: inertness, focusing steps and focus restore

**Spec.**
- [HTML §the-dialog-element](https://html.spec.whatwg.org/multipage/interactive-elements.html#the-dialog-element): `showModal()` puts the dialog in the top layer and makes the rest of the document inert ([§inert-subtrees](https://html.spec.whatwg.org/multipage/interaction.html#inert-subtrees), "blocked by a modal dialog").
- The dialog focusing steps pick a focus delegate. Closing restores focus to the previously focused element.
- `closedby` controls whether Esc and light dismiss close it.

**Chromium.**
- `showModal` (`core/html/html_dialog_element.cc:553-617`) [V]:
  - fires a cancelable `beforetoggle`;
  - adds the dialog to the top layer and sets modal;
  - recomputes inert subtrees;
  - enables its close watcher;
  - closes popovers that aren't its ancestors;
  - runs the focusing steps.
- **Inertness is part of style.** If there's an active modal dialog (the newest in the top layer, `core/dom/document.cc:8841-8854`), the root is inert and the dialog escapes it (`core/css/resolver/style_resolver.cc:563-583`) [V]. Fullscreen elements do the same.
- **Focusing steps.** Focus the delegate, or the dialog itself. If nothing is focusable and it's modal, clear focus (`html_dialog_element.cc:673-688`) [V].
- **Close.**
  - It removes the dialog from the top layer, recomputes inertness and queues `close` (`:148-204`) [V].
  - It refocuses the previous element with `preventScroll`, but only if the dialog was modal or focus was inside it (`:206-223`) [V].
- **`closedby`.** It defaults to `closerequest` when modal and `none` otherwise (`:240-255`) [V]. Light dismiss applies only to `closedby=any`, and a click on the backdrop counts as outside (`:274-302, 349-375`) [V].

**gpui-go.**
- Base's `Dialog` renders a deferred, viewport-sized focus trap over a backdrop. Its options are `CloseOnEscape`, `CloseOnBackdropPress` and `DismissBelowY` (`base/dialog.go:247-264, 296-383`) [V].
- The backdrop blocks the mouse by covering the window. Keyboard containment is the focus trap (`base/focus_trap.go`).
- The core has no inert state (03 §2), so:
  - accessibility clients can still reach the background (the core sets `Modal` on the node, `gpui/accessibility.go:398-399`, but nothing hides siblings) [I];
  - any code that moves focus programmatically escapes the trap [I].

**Gap.** Add `Window.ShowModal(id)` and `Close(id)` on the top layer:
- inert everything outside the topmost modal (reuses 03 §2's `Inert`, applied to the root path, not per div);
- run Chromium's focusing steps;
- record and restore focus;
- `closedby` maps onto the close watcher and light dismiss.

Go, M, after 03 §2.

---

## 4. System colours, colour scheme, forced colours and contrast

**Spec.**
- [CSS Color 4 §css-system-colors](https://drafts.csswg.org/css-color-4/#css-system-colors): Canvas, CanvasText, LinkText, VisitedText, ActiveText, ButtonFace, ButtonText, ButtonBorder, Field, FieldText, Highlight, HighlightText, SelectedItem, SelectedItemText, Mark, MarkText, GrayText, AccentColor, AccentColorText.
- [CSS Color Adjust 1](https://drafts.csswg.org/css-color-adjust-1/): `color-scheme` picks the light or dark palette for UA controls and system colours. In **forced colors mode** the UA replaces author colours with system colours unless `forced-color-adjust: none` is set.
- [Media Queries 5](https://drafts.csswg.org/mediaqueries-5/): `forced-colors`, `prefers-contrast` (`more`, `less`, `custom`), `prefers-reduced-transparency`, `inverted-colors`.

**Chromium (Windows).** `ui/native_theme/os_settings_provider_win.cc` [V]:
- **Forced colors** = `SPI_GETHIGHCONTRAST & HCF_HIGHCONTRASTON` (`:30-35`). It's refreshed on `WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST)`. The colours are refreshed on `WM_SYSCOLORCHANGE` (`:288-300`).
- **Dark mode** = `AppsUseLightTheme`. **Reduced transparency** = `EnableTransparency == 0`. Both come from the same `Personalize` key, watched with `RegNotifyChangeKeyValue`, not a window message (`:46-53, 162-215`).
- **Inverted colours** come from `ColorFiltering\FilterType == 1` (`:217-232`).
- **Overlay scrollbars** come from `Accessibility\DynamicScrollbars` (`:245-260`).
- **Accent colour** comes from `AccentColorObserver` (`:137, 263-268`).
- **Caret blink** comes from `GetCaretBlinkTime` (`:147-160`, see §11).

Elsewhere:
- **`prefers-contrast` is derived from the forced palette.** It's `more` if the window/text contrast is ≥ 7:1, `less` if ≤ 2.5:1, and `custom` otherwise. With no forced colours it's no-preference (`ui/native_theme/os_settings_provider.cc:179-218`) [V]. Windows has no separate "prefer more contrast" switch, so Chromium infers it.
- **System colour values.**
  - With forced colours they come from the OS palette (`core/layout/layout_theme.cc:633-639, 783+`). Without, they're defaults per light/dark scheme (`:641-740`) [V].
  - `AccentColor` and `AccentColorText` are stable on Windows (`runtime_enabled_features.json5:1585-1595`) [V].
- **Forced colours as a style pass.** A UA sheet (`core/html/resources/forced_colors.css`) is matched only in forced colours mode (`core/css/resolver/style_resolver.cc:1177-1181`) [V]. The media features are evaluated in `core/css/media_query_evaluator.cc:1044, 1145, 1168, 1199, 1527` [V].

**gpui-go.**
- Light/dark only. It reads `AppsUseLightTheme` (`gpui/desktop_windows.go:1133-1162`) [V]. It re-reads only on `WM_SETTINGCHANGE` with lparam `ImmersiveColorSet` (`:1781-1800`) and reports `Window.Appearance()` (`gpui/window.go:539-541`) [V].
- `updateSystemSettings` ignores every SPI except the wheel ones (`gpui/desktop_windows.go:1217-1224`) [V].
- No `SPI_GETHIGHCONTRAST`, no `WM_SYSCOLORCHANGE`, no `GetSysColor`, no accent colour, no transparency setting (grep for each in `gpui/`: no hits) [V].
- Base's theme is a hand-made light/dark palette (`base/theme.go`, `base/theme_tokens.go`) [V].

**Gap.** The browser treats these as **platform data** plus a **cascade rule**. gpui-go has no cascade, so:
1. **Core:** add `Window.SystemPreferences()`. It returns `{ColorScheme, ForcedColors, Contrast, ReducedTransparency, InvertedColors, OverlayScrollbars, AccentColor, CaretBlink, TextScale}` plus a `SystemColors` record (the CSS Color 4 list, filled from `GetSysColor` when forced, from scheme defaults otherwise). Refresh it on `WM_SETTINGCHANGE` (high contrast and `ImmersiveColorSet`) and `WM_SYSCOLORCHANGE`, plus a registry watch for `EnableTransparency`. Add one observer event. Go, S-M.
2. **Core, optional:** a forced-colours hook in style resolution. When forced colours are on, an element without `ForcedColorAdjustNone()` gets its text, background and border colours mapped to CanvasText, Canvas and ButtonBorder, and shadows dropped. That's what makes apps built without thought for high contrast still usable. Go, M.
3. **Component:** the theme picks palettes from these values.

---

## 5. Tooltips (the `title` attribute) and hover interest

**Spec.**
- [HTML §the-title-attribute](https://html.spec.whatwg.org/multipage/dom.html#the-title-attribute) is advisory information, typically a tooltip. The spec warns it's not reachable by keyboard-only or touch users [I, helper check below].
- `interestfor` (hover/focus-triggered popovers with `interest-delay`) is an Open UI proposal that Chromium has implemented [I].

**Chromium.**
- **`title`.** Blink only sends the text up. `UpdateTooltipUnderCursor` handles the mouse and `UpdateTooltipFromKeyboard` handles keyboard focus, with the element bounds (`core/page/chrome_client_impl.cc:754-781`) [V]. The browser process draws it with OS-like delays (`ui/views`, not checked out) [I]. So the platform owns the tooltip, including a **keyboard trigger**.
- **Interest invokers.** Hover or focus schedules "interest" after `interest-delay-start` (default **0.25 s**) and loses it after `interest-delay-end` (default **0.15 s**) (`core/dom/element.h:307-308`; `core/dom/element.cc:13280-13312`) [V]. Escape drops all interest (`core/input/keyboard_event_manager.cc:628`) [V].

**gpui-go.**
- The core says tooltips "are not ported" (`gpui/interact.go:1028-1031, 1225-1228`) [V]. GPUI's `div.tooltip()` is a core API upstream; gpui-go moved it to Base.
- Base's tooltip shows after **500 ms**, with a 300 ms grace period so the next one shows at once (`base/tooltip.go:35-40`) [V]. It's mouse-only: there's a touch gate, and no focus trigger (`:125-200`) [V].

**Gap.**
- **Core:** `Tooltip(text)` / `TooltipView(builder)` on interactive elements.
  - Hover and keyboard-focus triggers, with show and hide delays and the grace window.
  - The tooltip goes in the top layer as a `hint` (§1). Escape dismisses it (§2). Accessibility gets `HelpText`/`Description` for free.
  - A default plain renderer, so apps without Base still get tooltips.
- **Component:** styled tooltip views and hover cards.

Go, S-M.

---

## 6. Context menus

**Spec.** The `contextmenu` event ([UI Events](https://w3c.github.io/uievents/#event-type-contextmenu), [HTML](https://html.spec.whatwg.org/multipage/indices.html#event-contextmenu)) is cancelable. Its default action is the UA menu [I].

**Chromium.**
- **Keyboard.** The unmodified **Menu key fires on key-up on Windows**, and **Shift+F10 on key-down** (`core/frame/web_frame_widget_impl.cc:1116-1138`) [V].
  - `ShowNonLocatedContextMenu` places it at the selection, or at the focused element's corner (`core/input/event_handler.cc:2245-2330`) [V].
- **Mouse.**
  - Right-button mousedown sends `contextmenu` unless the `ShowContextMenuOnMouseUp` setting is on (`web_frame_widget_impl.cc:1191-1201`); the mouse-up path is `event_handler.cc:2345` [V]. Windows Chrome sets that setting [I].
  - `SendContextMenuEvent` releases the mouse press first, so no drag starts while the menu is up (`event_handler.cc:2183-2193`) [V].

**gpui-go.**
- `VK_APPS` maps to the key name `"menu"` (`gpui/keyboard_win32.go:357-358`) [V], but nothing binds it.
- Right-click arrives as a click with `IsRightClick` (`gpui/interact.go:171-188`) [V].
- There's a `context-menu` cursor (`gpui/styled_generated.go:28288-28291`) and an a11y `ShowContextMenu` action (`gpui/a11yprovider.go:652, 690`) [V], but no event they feed.
- There's no menu component in Base either (app-gaps #4).

**Gap.**
- **Core:** an `OnContextMenu(func(*ContextMenuEvent))` that bubbles from the target.
  - Mouse: right-button up (the Windows convention). Keyboard: Menu key-up and Shift+F10 key-down, at the focused element. A11y: the `ShowContextMenu` action.
  - It carries a position and the input source, and cancels any pending drag.
- **Component:** the menu itself, including submenus and the safe triangle.

Go, S.

---

## 7. Spellcheck

**Spec.** [HTML §spelling-and-grammar-checking](https://html.spec.whatwg.org/multipage/interaction.html#spelling-and-grammar-checking): `spellcheck` is true, false or default. The default is UA-defined, and inherits [I].

**Chromium.**
- The attribute is read in `core/dom/element.cc:12230-12239` [V].
- **Idle checker, two modes** (`core/editing/spellcheck/idle_spell_check_controller.cc`) [V]:
  - **hot mode** checks around the caret and each new undo step, in 1024-char chunks (`:272-323`; `hot_mode_spell_check_requester.cc:27`);
  - **cold mode** sweeps the rest of the focused editable on a 1 s timer, then every 200 ms (`:40-42`).
- **Markers.** They're drawn as underlines in red, with grammar in grey (`core/layout/layout_theme.cc:412-422`) [V].
- On Windows the dictionary is the OS `ISpellChecker`, via the browser process [I].

**gpui-go.**
- Nothing in `gpui/` or `base/` (grep for `spell`: no hits) [V].
- The renderer can already draw wavy underlines (`gpui/scene.go:105-106`, `gpui/text_run.go:317`) [V].
- zgui uses `ISpellChecker` (app-gaps #8).

**Gap.**
- **Core:** a `SpellChecker` service (COM `ISpellCheckerFactory`) offering `Check(text, lang) []Misspelling`, `Suggest(word)`, `Add`/`Ignore`. It runs off the UI thread and returns through a task. Also a `Spellcheck(bool)` element flag that inherits like the attribute.
- **Component:** the editor or input decides when to check (hot/cold like Blink) and draws its markers. The suggestion menu builds on §6.

Go (COM, no cgo), M.

---

## 8. Anchor positioning

**Spec.** [CSS Anchor Positioning 1](https://drafts.csswg.org/css-anchor-position-1/):
- `position-anchor`, `position-area`;
- `position-try-fallbacks` (`flip-block`, `flip-inline`, `flip-start`, `@position-try`), tried in order until one fits;
- `position-visibility: anchors-visible`, which hides the element when its anchor is scrolled out. The keyword's spelling is unconfirmed; see "Spec check".

**Chromium.**
- `out_of_flow_layout_part.cc:109-181, 257` iterates the base style and then each fallback until one fits [V].
- `anchor_position_scroll_data.cc` tracks the anchor's scroll offset so the popup follows it without relayout (`:65, 290-346`) [V].
- `anchor_position_visibility_observer.cc:91-172` hides the element when its anchor is clipped [V].

**gpui-go.**
- The core's `Anchored` takes a window position and a corner. It fits by switching corners or snapping to the window (`gpui/anchored.go:20-34, 95-127`) [V].
- Side placement, alignment, flip, offset and the client-inset margin live in Base's `Positioner` (`base/positioner.go:101-152, 265-300`) [V].
- The trigger bounds come from the previous frame (Base `Popup`'s first-frame sync, `base/popup.go:146-186`) [V].
- Because every frame redraws, the popup does follow a scrolled anchor. But nothing hides it when the anchor scrolls out of its clip [I].

**Gap.**
- **Core:** move the `Positioner` logic into `Anchored`: anchor to an element id rather than a point, side + align + offset, an ordered list of fallbacks (flip, then shift/clamp), and `HideWhenAnchorClipped`.
- **Component:** none; the components just choose a side.

Go, S-M.

---

## 9. Clipboard events and formats

**Spec.** [Clipboard API §clipboard-event-copy](https://w3c.github.io/clipboard-apis/#clipboard-event-copy): copy, cut and paste fire a cancelable `ClipboardEvent` at the focused element (or body) *before* the default action. If cancelled, the page's `DataTransfer` contents are written instead.

**Chromium.**
- `DispatchClipboardEvent` targets the focused or selected element. A cancelled copy/cut writes the event's data object to the system clipboard (`core/editing/commands/clipboard_commands.cc:256-295`) [V].
- There's no copy event inside password fields (`:297-311`).
- `ExecuteCopy` runs the event first, then the default copy (`:390-395`) [V].
- These are *editing commands*, so the keyboard, the context menu and `execCommand` all reach the same path.

**gpui-go.**
- `App.ReadClipboard`/`WriteClipboard` (`gpui/clipboard.go:418-440`) [V]. The supported formats (`gpui/clipboard_windows.go:90-92, 127-132`) [V]:
  - `CF_UNICODETEXT` with GPUI's hash and metadata;
  - PNG/GIF/JFIF/SVG and `CF_DIB`;
  - `CF_HDROP`.
- **No `HTML Format` or RTF**, so pasting from a browser or Word yields plain text only.
- No clipboard-change notification (`AddClipboardFormatListener`) [V].
- Copy, cut, paste and select-all are Base actions bound in Base (`base/input_state.go:327-333`, `base/root.go:42-49`) [V]. GPUI's action system already gives the "dispatch to focused element, any ancestor can intercept" shape, so the browser's event model maps onto it.

**Gap.**
- **Core:** standard edit actions (`Copy`, `Cut`, `Paste`, `SelectAll`, `Undo`, `Redo`) and their platform keys, so every app and every component agrees. Plus `HTML Format` read and write, and an `OnClipboardChange` observer. Go, S-M.
- **Component:** what each input does.
- OS file drops are 03 §3.

---

## 10. Text size and zoom

**Spec.** None for apps. Browsers offer page zoom (scales CSS px) and minimum or text-only font size.

**Chromium.**
- Blink has an OS text-scale hook, `SetAccessibilityFontScaleFactor` ("TODO: Rename this OSTextScaleFactor", `core/exported/web_settings_impl.cc:158-160`) [V].
- Who feeds it on Windows is in `content/`, which isn't checked out [I].
- Page zoom is a browser feature.

**gpui-go.**
- `DefaultRemSize = 16` (`gpui/layout_context.go:17-19`) is set per window (`gpui/element.go:1193-1199`) [V]. It can be overridden per subtree with `WithRemSize` (`gpui/element.go:1405`) [V].
- The Windows **Settings → Accessibility → Text size** value (`HKCU\Software\Microsoft\Accessibility\TextScaleFactor`, also `UISettings.TextScaleFactor`) isn't read [V by grep, registry path I].

**Gap.**
- **Core:** read the text scale (100-225 %) into `SystemPreferences` (§4), watch it, and set the window's rem to `16 × scale` by default, with an opt-out.
- **Component or app:** Ctrl+= zoom is the app's choice, built on the same rem.

Go, S.

---

## 11. Caret blink and selection colours

**Spec.** None for blink. Selection colours are `::selection` and the Highlight/HighlightText system colours ([CSS Pseudo 4 §highlight-pseudos](https://drafts.csswg.org/css-pseudo-4/#highlight-pseudos)) [I].

**Chromium.**
- **Caret blink** = `GetCaretBlinkTime()`. `INFINITE` means no blinking, and 0 means use the default (`ui/native_theme/os_settings_provider_win.cc:147-160`) [V]. Blink then reads it from `LayoutTheme::CaretBlinkInterval` (`core/layout/layout_theme.cc:626-631`) [V].
- **Selection background** is the platform's active or inactive colour, blended with white. The inactive-window colour is separate (`layout_theme.cc:365-390, 425-440`) [V].

**gpui-go.**
- Base hardcodes a 500 ms blink, a 300 ms pause after typing and a 2 px caret (`base/input_blink_cursor.go:19-26`) [V]. So users who turned blinking off in Windows still see it blink.
- The selection is the accent colour at alpha 0.4 (`base/input_state_style.go:76-80`) [V]. There's no inactive-window variant.

**Gap.**
- **Core:** expose `CaretBlinkInterval` (0 = steady), and `Highlight`/`HighlightText` plus their inactive variants (via §4's system colours), and refresh them on settings change.
- **Component:** use them by default.

Go, S.

---

## 12. Select and menu keyboard behaviour

**Spec.** [HTML §the-select-element](https://html.spec.whatwg.org/multipage/form-elements.html#the-select-element) leaves the keyboard to the platform [I].

**Chromium (Windows behaviour).** `core/html/forms/select_type.cc` [V]:
- **Opening the popup.** Alt+Down/Up and F4 open it (`:539-567`); so do Space (when no typeahead is in progress) and Enter, if the theme says so (`:569-575`).
- **While closed**, arrows, PageUp/PageDown (±3), Home and End *change the value* without opening (`:405-427`).
- **Typeahead** (`core/html/forms/type_ahead.cc`) [V]:
  - the buffer resets after 1 s (`:42, 69-74`);
  - repeating one character cycles through the items starting with it;
  - otherwise it prefix-matches, case- and accent-insensitive, starting after the current item (`:89-117`).

**gpui-go.** Base's select binds only Up, Down, Enter and Escape (`base/select.go:11-19`) [V]. There's no typeahead anywhere (grep: none), and no Alt+Down or F4.

**Gap.**
- **Component:** select and menu keyboard rules.
- **Core helper:** a small `TypeAhead` (buffer, timeout, cycling, accent-insensitive prefix match) that lists, trees and menus can share.

Go, S.

---

## Core versus component: the summary

The test I used: if Chromium implements it in `core/dom`, `core/input`, `core/html` element behaviour or `ui/native_theme` **independent of how it looks**, it's a platform primitive.

| Belongs in the core (behaviour, no look) | Belongs in components (look and composition) |
|---|---|
| Top layer: ordered overlays, backdrop slot, paint above everything | Popover, dialog, sheet, select, menu, toast visuals |
| Popover stack and click-based light dismiss (invoker-aware) | Which popover kind a component uses |
| Close watchers; Escape as a default action | Confirm/cancel buttons |
| `ShowModal`: inert outside, focusing steps, focus restore | Dialog layout, animations |
| Tooltip hook (hover and focus, delays, a11y description, plain default) | Rich tooltip and hover-card views |
| `contextmenu` event (right-click, Menu key, Shift+F10, a11y) | The menu, submenus, safe triangle, checked items |
| Anchor-to-element with fallbacks and hide-when-clipped | Choosing side and offset |
| System preferences: scheme, forced colours, contrast, transparency, accent, text scale, caret blink, system colours, with change events | Theme palettes built from them |
| Optional forced-colours override in style resolution | — |
| Spellcheck service and the `Spellcheck` flag | When to check, suggestion UI |
| Standard edit actions and keys; clipboard HTML format and change events | Per-input paste handling |
| `TypeAhead` helper | Select and menu key maps |

Base already does most of the right-hand column. What's missing is the left-hand column, which Base currently fakes per component.

## Where gpui-go already matches

- Overlay paint order via `Deferred` priorities. It's the right primitive for the top layer's painting.
- Light/dark follows the system live, including the DWM title bar (`gpui/desktop_windows.go:1781-1800`).
- Wavy underlines exist for spelling markers.
- Rich clipboard images, and files out to the OS.
- Base's focus restore on popover close, and its tooltip grace period, match browser and OS behaviour.

## Spec check

A web-fetch helper read the pages on 2026-10-23. Most pages were cut off at about 39k characters, so long sections weren't reached. Every spec link above that isn't listed here as verified stays [I].

**Quoted verbatim [V]:**
- **Popover states** ([#the-popover-attribute](https://html.spec.whatwg.org/multipage/popover.html#the-popover-attribute)):
  - auto: "Closes other popovers when opened; has light dismiss and responds to close requests."
  - manual: "Does not close other popovers; does not light dismiss or respond to close requests."
  - hint: "Closes other hint popovers when opened, but not other auto popovers; has light dismiss and responds to close requests."
- **Hiding a popover** ([#hide-popover-algorithm](https://html.spec.whatwg.org/multipage/popover.html#hide-popover-algorithm)): "If focusPreviousElement is true and … run the focusing steps for previouslyFocusedElement".
- **Modal inertness** ([#modal-dialogs-and-inert-subtrees](https://html.spec.whatwg.org/multipage/interaction.html#modal-dialogs-and-inert-subtrees)): "every node that is connected to document, with the exception of the subject element and its flat tree descendants, must become inert."
- **Top layer** ([CSS Position 4](https://drafts.csswg.org/css-position-4/#top-layer)): "the last element in the top layer is rendered on top of everything else." Each element's `::backdrop` paints just before it. A fixed-position top-layer element's containing block is the viewport; otherwise it's the initial containing block.
- **Forced colours** ([#forced-colors-properties](https://drafts.csswg.org/css-color-adjust-1/#forced-colors-properties)):
  - "the <color> components of all properties on the element are force-adjusted". That covers color, background-color, border-color, outline-color, fill, stroke, caret-color and accent-color.
  - background-color keeps its alpha "so that transparent backgrounds remain transparent". `url()` images are kept.
  - box-shadow and text-shadow become none.
  - This is what §4's optional core hook would copy.
- **color-scheme** ([#color-scheme-effect](https://drafts.csswg.org/css-color-adjust-1/#color-scheme-effect)): it must change "the default colors of scrollbars and other interaction UI", "the default colors of form controls", and "the value of system colors".
- **Clipboard** ([#override-copy](https://w3c.github.io/clipboard-apis/#override-copy)): "If the event is not canceled, the currently selected data will be copied to the system clipboard." "The paste action has no effect in a non-editable context, but the paste event fires regardless."

**The helper's summaries only [I]:**
- Dialog `closedby`: the auto state acts as closerequest when modal and none otherwise. This matches Chromium (§3).
- Dialog light dismiss: pointerdown, then pointerup on the same non-topmost dialog, requests close of the topmost dialog when it's `any`.
- The `prefers-contrast` values (`no-preference`, `less`, `more`, `custom`) and the rule that the forced palette's contrast picks more or less.
- The system colour list in §4.
- Anchor fallbacks `flip-block`, `flip-inline` and `flip-start`.

**Not reached:**
- close watchers;
- `title`;
- spellcheck;
- the popover light-dismiss steps;
- "fire a clipboard event".

**Open points:**
- **Light-dismiss trigger.** The helper's summary says the spec's popover light dismiss acts on pointerdown/pointerup. Chromium ships a click-based variant (`LightDismissFromClick: stable`) that compares the down and up targets. Both reject drags that start inside, and the proposal follows Chromium's.
- **`position-visibility` keyword.** The helper reported `anchor-visible` rather than `anchors-visible`. Check the exact spelling before using it in an API name.
