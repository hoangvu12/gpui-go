# Browser parity: a browser-like core, components in their own repo

Ticket: [.scratch/browser-parity/issues/07-synthesis.md](../../.scratch/browser-parity/issues/07-synthesis.md). Written 2026-10-23 from reports [01](01-visual-primitives.md)–[06](06-platform-features.md), [08](08-hot-reload-and-ui-scripting.md), [app-gaps](../app-gaps/README.md), gpuix `a53b294` and the bold correction in [zterm 13](../../.scratch/zterm/issues/13-browser-like-bold-synthesis.md). This is a proposal: nothing here is accepted until the user says so. Every gpui-go file:line below was rechecked on this branch; no code under `gpui/`, `base/` or `reference/native/src` changed after the reports' commit `8f5ad4d`.

Sizes: S ≤ 2 days, M ≤ 1 week, L ≤ 3 weeks, XL more. "§n" means a section of the linked report.

## 1. Summary

- **Direction.** gpui-go's core behaves like a browser wherever GPUI is silent or differs. The spec is the rule, and Chromium on Windows decides where the spec leaves room. The core ships no components.
- **What the core becomes.** It owns behaviour without look: focus and Tab, the Escape stack, the top layer and light dismiss, inert/modal, text selection, transitions and reduced motion, system preferences, and CSS-correct painting (group opacity, transforms, rounded clips, gradients, CSS blur sizes). Where a browser ships a UA default look, the core ships one too: the focus ring, a plain tooltip, scrollbars and selection colours.
- **What leaves.** Base, with every styled widget, theme, motion preset and keymap, moves to its own repo and becomes a thin presenter over the core. gpuix independently landed on the same split: headless primitives in the binding, styled copies in userland.
- **The work.** First 23 Go quick wins, then five Go themes, then **three native rebuilds**: R15 (ABI growth, the bold rule and small shader fixes), R16 (the compositor: group targets, transforms, rounded clips, masks, damage and partial present) and R17+ (measured or optional items).
- **Decisions first.** §5 lists 16 questions. The first, D0, decides whether "browser wins over the GPUI pin" becomes the conformance rule.

## 2. Principles

1. **Browser semantics by default.** Where GPUI is silent or differs, use the browser's behaviour. Keep GPUI's shape, such as the element, view and action APIs, wherever it doesn't change what the user sees or feels.
2. **One reference.** The W3C/WHATWG specs, plus Chromium on Windows where the spec leaves room. Chromium's Windows-only code wins over its generic code; the bold rule in [04 §1](04-text-and-fonts.md) is the example of why.
3. **Core = behaviour without look.** The test from [06](06-platform-features.md): if Chromium implements something in `core/dom`, `core/input`, `core/html` element behaviour or `ui/native_theme`, independent of how it looks, it belongs in the core. The core may ship a UA-default look (like Chrome's UA stylesheet) but no themes.
4. **Components = look and composition.** Styled widgets, palettes, motion presets, menus and dialog chrome live in the Base repo. They call core primitives instead of faking them per component, as Base does now.
5. **Every divergence from the pin is recorded.** Each change that moves away from GPUI-CE `254b5dbd` gets a fixture update and a one-line note ("deliberate divergence toward CSS"), as the inset-shadow rule already has (`gpui/div.go:1916-1949`).

**What gpuix confirms.** gpuix is React/Solid over GPUI, with browser-like host elements:
- `div` is block, not flex (README:3382-3384).
- Tab is a default action that runs after `onKeyDown`, can be cancelled with `preventDefault()`, and can be turned off per window with `tabNavigation: false` (README:2106-2147; `packages/native/js/host.ts:776`). GPUI actions still run before listeners (README:2103-2104).
- Escape closes the newest layer on one per-window stack (README:2609-2643).
- All text is selectable, with `userSelect: "none"` to opt out (README:2664-2671; `packages/native/src/renderer.rs:4419-4430`).
- Its controls are unstyled primitives that apps copy into local styled files, the shadcn model (README:2188-2209). That is the model for the Base repo.
- Its desktop hot reload loses state ([08](08-hot-reload-and-ui-scripting.md)), so it isn't a model for the hot reload track.

## 3. Core vs components

### Features

| Feature | Where | Why | Ref |
|---|---|---|---|
| Rounded clipping of children, clip-path, masks and edge fades | Core | CSS painting rule | [01 §1, §8, §9](01-visual-primitives.md) |
| Group opacity, `isolation`, mix-blend-mode | Core | CSS painting rule | [01 §2, §10](01-visual-primitives.md), [02 §3](02-motion-and-compositing.md) |
| Transforms, transform-origin, inverse hit testing | Core | CSS painting rule | [01 §3](01-visual-primitives.md), [02 §4](02-motion-and-compositing.md) |
| CSS blur sizes and box-shadow geometry | Core | Ported CSS numbers must look right | [01 §4, §5](01-visual-primitives.md) |
| Gradients, filters, backdrop-filter | Core | CSS painting rule | [01 §6, §7](01-visual-primitives.md) |
| `outline`, offset, default focus ring (`auto`) | Core (UA default look) | Chrome draws it | [01 §11](01-visual-primitives.md) |
| object-fit, object-position, image-rendering, radius scaling | Core | CSS rule | [01 §12, §13](01-visual-primitives.md) |
| Damage, partial present, property-only frames, scroll layers | Core | Performance, invisible to apps | [02 §1, §2, §6](02-motion-and-compositing.md) |
| Timing and easing engine, keyframe interpolation, `Transition` style property | Core | CSS transitions are style-driven | [02 §5](02-motion-and-compositing.md) |
| Presence, stagger, sequence, reveal, springs as tokens, overscroll bounce | Components | Opinionated motion | [02 §5, §6](02-motion-and-compositing.md) |
| Smooth scroll, scroll anchoring, reduced-motion preference | Core | Browser defaults | [02 §6, §7](02-motion-and-compositing.md) |
| Hover/enter/leave, pointer capture, click synthesis, `:focus-visible` | Core | DOM event model | [03 §1, §5, §7, §10](03-input-and-events.md) |
| Tab order, focus defaults, fixup, `Inert` | Core | Most important core piece | [03 §2](03-input-and-events.md) |
| Drag and drop events and effects | Core | DOM event model | [03 §3](03-input-and-events.md) |
| Key `code`, Shift kept, listener/default order | Core | DOM event model | [03 §4](03-input-and-events.md) |
| Wheel latching, keyboard scrolling | Core | Browser default actions | [03 §6](03-input-and-events.md) |
| Text selection, `user-select`, auto I-beam | Core (see D7) | Text is selectable in a browser | [03 §8](03-input-and-events.md) |
| IME composition events, TSF | Core | Platform | [03 §9](03-input-and-events.md) |
| Pointer types (pen, touch) | Core | Platform | [03 §11](03-input-and-events.md) |
| Cursors | Core | Platform | [03 §12](03-input-and-events.md) |
| Font synthesis, rasterization, line height, wrapping, fallback, decorations, variable fonts, emoji | Core | UA text engine | [04](04-text-and-fonts.md) |
| Min-content, scroll overflow, positioning, sticky, fixed, baselines, grid, display, rounding, `overflow: auto` | Core | CSS layout | [05](05-layout.md) |
| Top layer, popover stack, light dismiss | Core | HTML popover | [06 §1](06-platform-features.md) |
| Close watchers; Escape as a default action | Core | HTML close requests | [06 §2](06-platform-features.md) |
| `ShowModal`: inert outside, focusing steps, focus restore | Core | HTML dialog | [06 §3](06-platform-features.md) |
| Dialog, popover, sheet, select, menu, toast visuals | Components | Look | [06 §1](06-platform-features.md) |
| System preferences and system colours, with change events; optional forced-colours pass | Core | `ui/native_theme` | [06 §4](06-platform-features.md) |
| Theme palettes built from them | Components | Look | [06 §4](06-platform-features.md) |
| Tooltip hook (hover and focus, delays) with a plain default | Core | `title` is platform-owned | [06 §5](06-platform-features.md) |
| Rich tooltips, hover cards | Components | Look | [06 §5](06-platform-features.md) |
| `contextmenu` event | Core | DOM event | [06 §6](06-platform-features.md) |
| Menus, submenus, safe triangle | Components | Look and composition | [06 §6](06-platform-features.md) |
| Spellcheck service and `Spellcheck` flag | Core | Platform service | [06 §7](06-platform-features.md) |
| When to check, suggestion UI | Components | Policy | [06 §7](06-platform-features.md) |
| Anchor-to-element with flip/shift and hide-when-clipped | Core | CSS anchor positioning | [06 §8](06-platform-features.md) |
| Standard edit actions and keys; HTML clipboard format; clipboard change events | Core | Editing commands | [06 §9](06-platform-features.md) |
| Text scale → rem, caret blink, highlight colours | Core | Platform values | [06 §10, §11](06-platform-features.md) |
| Select and menu key maps | Components | Behaviour of a widget | [06 §12](06-platform-features.md) |
| `TypeAhead` helper | Core | Shared by lists, trees, menus | [06 §12](06-platform-features.md) |
| Hot reload runner, keyed state restore | Core tooling | Every app benefits | [08](08-hot-reload-and-ui-scripting.md) |

### Move out of Base into the core

| Base today | Core replacement | Ref |
|---|---|---|
| Tab binding (`base/root.go:47-48, 273-279`) | Tab/Shift+Tab as a default action (I2) | [03 §2](03-input-and-events.md) |
| Per-component Escape bindings (`base/popover.go:36`, `base/dialog.go:136`, `base/select.go:18`, `base/combobox.go:19`) | Close-watcher stack (P2) | [06 §2](06-platform-features.md) |
| Focus trap and backdrop (`base/focus_trap.go`, `base/dialog.go:247-264`) | `Inert` + `ShowModal` (I2, P3) | [06 §3](06-platform-features.md) |
| Popup liveness list (`base/global_state.go:148-165`) and `OnMouseDownOut` dismissal (`base/popover.go:397-402`) | Top layer with light dismiss (P1) | [06 §1](06-platform-features.md) |
| Reduced-motion read (`base/reduce_motion_windows.go:20-38`) | Core preference with live updates (M1) | [02 §7](02-motion-and-compositing.md) |
| Timing, easing and keyframe interpolation (`base/motion_timing.go`, `base/motion_easing.go`, `base/motion_keyframes.go`), and the reversing rule (`base/motion.go:364-373`) | Core transitions (M2) | [02 §5](02-motion-and-compositing.md) |
| `Positioner` (`base/positioner.go:101-152`) | `Anchored` to an element, with fallbacks (P5) | [06 §8](06-platform-features.md) |
| Tooltip triggers and delays (`base/tooltip.go:35-40`) | Core tooltip hook (P4) | [06 §5](06-platform-features.md) |
| Text selection model (`base/text_selection*.go`, `base/text_view.go`) | Core selection (I10, D7) | [03 §8](03-input-and-events.md) |
| Copy, cut, paste, select-all actions and keys (`base/root.go:42-49`) | Core edit actions (P6) | [06 §9](06-platform-features.md) |
| Hard-coded caret blink and selection colour (`base/input_blink_cursor.go:19-26`, `base/input_state_style.go:76-80`) | Platform values via `SystemPreferences` (P7) | [06 §11](06-platform-features.md) |

Base keeps the look: dialog, popover, select, menu and tooltip views, theme palettes, motion presets (presence, stagger, sequence, reveal, springs), overscroll bounce (`base/scroll_bounce.go`), select keymaps, and every widget (editor, tables, trees, docking).

### Move out of the core

- **`gpui/line_wrapper.go`.** Its header says it was restored for Base's editor display map and inline flow; the core pin removed it because Parley wraps. Move it with Base. Check the references in `gpui/shaped_line.go` and `gpui/text_tabs.go` first.
- **The Tailwind palette and class mapper** (`gpui/tailwind_palette_generated.go`, `gpui/class.go`): these are look-adjacent tokens. **Keep them for now.** The mapper only targets core properties, and its "not supported" hints shrink as this work lands. Revisit once Base has moved.
- Nothing else in `gpui/` is a component. `editor.go` is the IME-facing text model, which is core, like an `<input>`.

## 4. The work

"Changes behaviour?" column:
- **No**: additive, or it moves *toward* the pin.
- **Fixture**: a deliberate divergence from the pin. Update the Tier A/C fixtures ([research 19](../19-conformance-update-policy.md)). Covered by D0.
- **D*n***: needs that decision in §5 first.

### (a) Quick Go wins (a few lines each)

| # | Change | Where | Changes behaviour? |
|---|---|---|---|
| Q1 | Shadow blur: pass σ = blur/2 | `gpui/paint.go:556`, `:581` | Fixture (see risk 1 in §8) |
| Q2 | `filter` and `backdrop-filter` blur: pass 2× radius | `gpui/div.go:1898`, `:1997` | Fixture |
| Q3 | Spread grows the shadow's corner radii | `gpui/paint.go:553-565` | Fixture |
| Q4 | Paint the shadow list in reverse, so the first is on top | `gpui/div.go:1885` | Fixture |
| Q5 | Paint shadows and the ring inside the filter group | `gpui/div.go:1885-1900` vs `:1977-1985` | Fixture |
| Q6 | Overflow clip always insets by the border widths | `gpui/style.go:1189` | Fixture |
| Q7 | Scale all corner radii by one factor | `gpui/style.go:1019-1029` | Fixture |
| Q8 | `object-fit: none` centred; fit in the content box | `gpui/capture.go:658-659`, `gpui/img.go:570` | Fixture |
| Q9 | Apply `TextTransform` (port the pin's `ce:crates/gpui/src/elements/text.rs:636-680`) | field at `gpui/div.go:115-116` | No (the pin does it) |
| Q10 | Letter-spacing ≠ 0 sets `liga`/`clig`/`calt` = 0 | `gpui/text.go:659-673` | Fixture |
| Q11 | Underline thickness max(1, size/10) and Chrome's offset | `gpui/styled.go:686-693`, `gpui/rich_paint.go:47-55` | Fixture |
| Q12 | `line-clamp-N` implies an end ellipsis, with "..." when U+2026 is missing | `gpui/class.go:867-872` | No |
| Q13 | Map `tabular-nums` and friends to features; alias `font-sans`/`serif`/`mono` | `gpui/class.go:1131`, `:873-874` | No |
| Q14 | Handle `WHEEL_PAGESCROLL` (0xFFFFFFFF lines) | `gpui/win32input_windows.go:90` | No (bug) |
| Q15 | `ReleaseCapture` only once no button is held | `gpui/desktop_input_windows.go:191-229` | No |
| Q16 | `:focus-visible` follows the focus origin, and modified keys don't count | `gpui/interact.go:1076`, `:466-476` | D12 |
| Q17 | Enter activates on key-down | `gpui/interact.go:1160-1224` | D12 |
| Q18 | A key press no longer clears hover | `gpui/mouseevent.go:277-289` | D12 |
| Q19 | Scroll range from Taffy's `content_size`; vertical padding % resolves against width | `gpui/scroll_element.go:338-363`, `:110-113` | Fixture |
| Q20 | Floor border widths; sizes in (0, 1) device px keep 1 device px | `gpui/layout.go:79-92` | Fixture |
| Q21 | Grayscale text AA when the window isn't opaque | `gpui/draw_text.go:38-45` | No (the pin does it) |
| Q22 | Load the grab, grabbing, copy and context-menu cursors | `gpui/desktop_windows.go:440-463` | No |
| Q23 | Skip frames while minimized or occluded | `gpui/frameloop.go:71-73` | No |

### (b) Go work by theme

**Input and focus** ([03](03-input-and-events.md))

| # | Item | Size | Depends on | Changes behaviour? |
|---|---|---|---|---|
| I1 | Default actions run after key listeners (Tab, Escape, keyboard scroll, activation) | M | — | D5 |
| I2 | Core Tab/Shift+Tab, HTML tabindex order, blur on empty press, end-of-frame focus fixup, `Inert()`, optional scroll-into-view | M | I1 | D4, Fixture |
| I3 | WM_MOUSELEAVE → "pointer left"; `OnPointerEnter`/`OnPointerLeave` from a hover diff | M | — | D12 |
| I4 | Capture retargets the hit test; got/lost callbacks; WM_CAPTURECHANGED cancels; `LocalPosition` | S-M | — | D12 |
| I5 | Drag enter/leave/end, cancel on Escape and deactivation, OLE effect from `CanDrop` | M-L | I1, I3, I4 | No |
| I6 | `Keystroke.Code`/`Location`; keep Shift; keymap matches both forms | M | — | No, if both forms match |
| I7 | Wheel latching and `OverscrollContain`; keyboard scrolling; 100/3 px per line | M | I1 | D10 |
| I8 | Click on the common ancestor; `:active` while Space is held | S | — | D12 |
| I9 | `OnContextMenu` (right-button up, Menu key-up, Shift+F10, a11y) | S | — | No |
| I10 | Core text selection, `UserSelect`, auto I-beam | L | I3 | D7 |
| I11 | IME composition callbacks and `IsComposing` (S-M); TSF text store later (L) | S-M / L | — | No |
| I12 | WM_POINTER `PointerEvent` family, move coalescing | M-L | — | No (low priority) |

**Layout** ([05](05-layout.md))

| # | Item | Size | Depends on | Changes behaviour? |
|---|---|---|---|---|
| L1 | Text min-content wraps at every opportunity; cache key includes the mode | S-M | — | D2 |
| L2 | `PositionStatic` and the nearest-positioned containing block (reparent in Go) | M | — | D3 |
| L3 | Sticky, applied at prepaint; list header hook | M | — | No |
| L4 | `PositionFixed` (absolute to the window root, painted deferred) | S-M | L2, P1 | No |
| L5 | `display: contents`, `inline-block`, `inline-grid`; `flow-root` mapping in R15 | S-M | R15 for flow-root | D6 for the default |
| L6 | `overflow: auto`; opt-in `scrollbar-gutter: stable` | S-M | — | No |
| L7 | Grid track lists, auto rows, areas, `justify-items`/`-self` (Go side) | M | R15 | No |
| L8 | Baseline alignment (Go `MeasureFunc` carries a baseline) | S | R15 | Fixture |

**Text** ([04](04-text-and-fonts.md))

| # | Item | Size | Depends on | Changes behaviour? |
|---|---|---|---|---|
| T1 | `normal` line-height kind (rounded ascent + descent + gap) | S | — | D1 for the default |
| T2 | White-space collapse (`normal`, `nowrap`, `pre-line`) with a caret byte map | S | — | Fixture |
| T3 | `text-wrap: balance` by bisection | M | R15 | No |
| T4 | Em-based tracking; generic families; `tab-size: 8` under `pre` | S | — | Fixture (tabs) |
| T5 | `justify`/`start`/`end` alignment (Go side) | S | R15 | No |
| T6 | Rasterization A/B probe (black/white/grey × grayscale/ClearType vs Chrome) | S | — | No; decides R17 shader work |
| T7 | Italic slant probe (DirectWrite oblique vs Chrome −0.25 skew) | S | — | No |

**Motion** ([02](02-motion-and-compositing.md))

| # | Item | Size | Depends on | Changes behaviour? |
|---|---|---|---|---|
| M1 | Reduced motion in the core: live `WM_SETTINGCHANGE`, preference query | S | P7 shares the record | No |
| M2 | `Transition` style property, per-element style diff, timing and easing moved from Base | M-L | M1 | No (opt-in property) |
| M3 | Smooth scroll for wheel, keyboard and programmatic scrolls, with Chrome's curve | S-M | M1, I7 | D13 |
| M4 | Scroll anchoring on `ScrollHandle` | M | — | Fixture |
| M5 | Half-rate preferred interval per window | S | — | No |
| M6 | Transforms, Go side: style, transform stack, origin, inverse hit testing | L | R16 | No (new property) |
| M7 | FLIP helper (geometry diff plus transform transition) | M | M2, M6 | No |
| M8 | View Transitions | XL | R16, M6, perf track | No; do it last |

**Platform** ([06](06-platform-features.md))

| # | Item | Size | Depends on | Changes behaviour? |
|---|---|---|---|---|
| P1 | Top layer: ordered overlays, kinds, backdrop slot, click-based light dismiss that knows the invoker | M-L | I2 | No (Base migrates) |
| P2 | Close watchers; unhandled Escape signals the newest | S-M | I1, P1 | No |
| P3 | `Window.ShowModal`/`Close`: inert outside, focusing steps, focus restore, `closedby` | M | I2, P1, P2 | No |
| P4 | Tooltip hook with hover and focus triggers, delays and grace, a `hint` layer, a plain default | S-M | P1, P2 | No |
| P5 | `Anchored` to an element: side, align, offset, flip/shift fallbacks, hide when clipped | S-M | — | No |
| P6 | Core edit actions and keys; `HTML Format` read/write; `OnClipboardChange` | S-M | I1 | No |
| P7 | `SystemPreferences` + `SystemColors`, refreshed on settings, colour and registry changes | S-M | — | No |
| P8 | rem = 16 × the Windows text scale | S | P7 | D11 |
| P9 | Optional forced-colours pass in style resolution | M | P7 | No (only in high contrast) |
| P10 | Spellcheck service (`ISpellChecker`, off-thread) and `Spellcheck` flag | M | — | No |
| P11 | `TypeAhead` helper | S | — | No |

**Visual, Go side** ([01](01-visual-primitives.md))

| # | Item | Size | Depends on | Changes behaviour? |
|---|---|---|---|---|
| V1 | `Outline{Width, Offset, Color, Style}` painted after the children; default `auto` focus ring | S | — | Fixture |
| V2 | Border painted before the children (CSS2 Appendix E); CSS overflow value computation | S | R16 (rounded clip) | Fixture |
| V3 | Gradient `Background` and the `bg-linear`/`from`/`to` classes | S | R15 | No |
| V4 | `ObjectPosition`; `image-rendering: pixelated` flag | S | R15 (sampler) | No |
| V5 | Emit opacity groups, transforms, masks, blend and clip-path from styles | M | R16 | D9 |

### (c) Native rebuild groups

Each rebuild costs a user approval, an evidence run and pin updates in about 8 places ([zterm 13](../../.scratch/zterm/issues/13-browser-like-bold-synthesis.md)). So bundle by readiness: R15 is fully specified and can ship soon; R16 needs design. Merging them (D14) would hold the bold fix and gradients back for months.

**R15: ABI growth and small fixes (L in total; ready now).**
| Item | Size | Changes behaviour? | Ref |
|---|---|---|---|
| Bold rule: `weight >= 600 && face < 600` (`reference/native/src/text.rs:1347`, the proposed rev 15); update `TestBoldSynthesisFollowsBrowserRule` | S | Fixture (600/400, 700/500, 900/600 cells) | [04 §1](04-text-and-fonts.md) |
| Drop shadows clip out the border box | S | Fixture | [01 §5](01-visual-primitives.md) |
| Gradient record: tag, angle, 2 stops; sRGB default, premultiplied mix, CSS line length | S | No (new) | [01 §6](01-visual-primitives.md) |
| Colour-matrix filters (grayscale, sepia, …) | S-M | No (new) | [01 §7](01-visual-primitives.md) |
| Backdrop mirror crop at the element edge | S | Fixture | [01 §7](01-visual-primitives.md) |
| Point sampler and a per-sprite flag | S | No | [01 §12](01-visual-primitives.md) |
| Text run record: word-break, overflow-wrap, text-wrap mode, locale, `FontVariations`, `FontWidth`, opsz = size, justify | M | No (new fields) | [04 §4, §5, §10, §12](04-text-and-fonts.md) |
| Layout record: a baseline in the measure response (Taffy tree trait), variable-length grid tracks, `justify-items`/`-self`, `flow-root` | M | Fixture (baselines) | [05 §6, §7, §8](05-layout.md) |

**R16: the compositor (XL; needs a short design first).**
| Item | Size | Changes behaviour? | Ref |
|---|---|---|---|
| Group target pool; remove `MAX_FILTER_GROUP_DEPTH = 2` (`reference/native/src/scene.rs:150`); skip single-primitive groups | M | No | [01 §2](01-visual-primitives.md) |
| Opacity groups | M | D9 | [01 §2](01-visual-primitives.md), [02 §3](02-motion-and-compositing.md) |
| Transformed group composite (option b) | L | No (new) | [01 §3](01-visual-primitives.md) |
| Rounded content mask: an rrect in the shader plus a coverage mask stack for nesting | M-L | Fixture (children clip at corners) | [01 §1](01-visual-primitives.md) |
| Mask composite: edge fades (also gives `OverflowFade`), gradient and image masks, alpha/luminance | M | No | [01 §8](01-visual-primitives.md) |
| clip-path coverage (lyon into the mask) | M | No | [01 §9](01-visual-primitives.md) |
| Blend modes, including `plus-lighter` | M-L | No | [01 §10](01-visual-primitives.md) |
| n-stop, radial, conic and repeating gradients (stop texture) | L | No | [01 §6](01-visual-primitives.md) |
| `drop-shadow()` filter | M | No | [01 §7](01-visual-primitives.md) |
| Scene property nodes (transform and opacity) for property-only frames | M | No | [02 §2](02-motion-and-compositing.md) |
| Retained frame target, scissored damage redraw, `Present1` dirty rects, blur damage expansion | L | No | [02 §1](02-motion-and-compositing.md) |
| Redundant state binds, path tessellation cache | S-M | No | [app-gaps](../app-gaps/README.md) perf 6 |

**R17+: measured or optional (bundle whatever is ready).**
- Skia-style gamma/contrast preblend and grid-fit table, **only if** T6 shows a visible gap (M). [04 §2](04-text-and-fonts.md)
- Skip-ink (M), COLRv1 emoji (M-L), per-codepoint fallback (M-L). [04 §5, §8, §11](04-text-and-fonts.md)
- Retained layout ABI: keep the Taffy tree across frames (L). This could ride R16 if it's designed in time. [app-gaps](../app-gaps/README.md) perf 3
- Scroll layers or scroll-by-copy (L). [02 §6](02-motion-and-compositing.md)
- View Transitions snapshot capture (part of M8), mipmaps (M), elliptical radii (M).
- Luau runtime, if D15 picks it. [08](08-hot-reload-and-ui-scripting.md)

### Order

1. D0–D16 answered.
2. Quick wins Q1–Q23, plus T6/T7 probes, in parallel.
3. R15, then the Go items it unblocks: V3, V4, L5 (flow-root), L7, L8, T3, T5.
4. Input and focus: I1 → I2 → I3/I4 → I5–I9. Then P1 → P2 → P3 → P4, plus P5–P7 in parallel. This is the critical path for moving Base out.
5. Layout L1–L4 and text T1/T2/T4. These are the decision-gated ones, so land each as soon as its decision is in.
6. Motion M1 → M2 → M3/M4.
7. R16 design, then the build. Then V1/V2/V5, M6, and the performance track.
8. R17+ and M7/M8.

## 5. Behaviour changes that need the user's decision

| # | Question | Recommendation |
|---|---|---|
| D0 | When the browser and the GPUI-CE pin differ, does the browser win (with each divergence recorded and its fixtures updated)? This replaces "1:1 pin parity" in PROJECT.md. | **Yes.** It's what "behave like the browser" means. "Fixture" items in §4 then need no separate OK. |
| D1 | Default line height: the pin's 1.618 × size, or CSS `normal` (about 1.33 for Segoe UI)? | **`normal` by default**, with 1.618 as an opt-in. Changes every app's vertical rhythm. |
| D2 | Should text min-content wrap at every opportunity (CSS), so flex rows shrink and wrap? | **Yes.** It removes most `min_w_0` workarounds. Rows that relied on text holding them open will start wrapping. |
| D3 | `position: static` as the default, so `absolute` resolves against the nearest positioned ancestor? | **Yes, in two steps**: add `PositionStatic` as opt-in now, flip the default when Base migrates, with a lint for `absolute` under a non-`relative` parent. |
| D4 | Tabindex order: HTML (positive ascending first, then 0 in tree order, negative never reached by Tab) instead of the pin's −1 < 0 < 1? | **HTML order.** Handles stay opt-in tab stops, since the core has no buttons. |
| D5 | Key-binding vs listener order. | **gpuix's compromise**: bindings stay before listeners (GPUI shape), and core defaults (Tab, Escape, keyboard scroll, activation) run after listeners and honour `PreventDefault`. Full browser order is a per-window flag. |
| D6 | Default display: `flex` (pin) or `block` (browser, gpuix, and newer GPUI-CE `beb5a6d`)? | **`block`**, flipped together with the Base move plus a codemod adding `.Flex()`. First check what the pin `254b5dbd` defaults to ([05 §8](05-layout.md)). |
| D7 | Is text selectable by default (`user-select: auto`), as in browsers and gpuix? | **Yes.** Opt out with `UserSelect(None)`. Base's selection logic moves into the core. |
| D8 | Shadow and filter blur: use CSS sizes (Q1/Q2), diverging from the pin? | **Yes**, after the pinned shader is checked (risk 1). |
| D9 | Group opacity instead of per-primitive alpha (visible on any translucent element with overlapping children)? | **Yes**, in R16. |
| D10 | Wheel step: Chrome's 100/3 px per line, or the window's line height (about 60 px per notch at 20 px)? | **Chrome's** (100 px per notch at 3 lines). |
| D11 | Should rem follow the Windows "Text size" setting? | **Yes, by default**, with a per-app opt-out. |
| D12 | Input bundle: hover survives typing, pointer-leave clears hover, Enter activates on key-down, click goes to the common ancestor, capture retargets? | **Yes, as one bundle.** Each is a browser rule, and each needs only a Tier B transcript update. |
| D13 | Smooth scrolling on by default (off under reduced motion)? | **Yes.** It's the browser default. |
| D14 | Two compositor rebuilds (R15 now, R16 later) or one? | **Two.** R15 is ready and small; R16 needs design. |
| D15 | Hot reload and UI scripting. | **DECIDED 2026-10-23: TSX via esbuild + goja, on zgui's model** ([ADR 0003](../../docs/adr/0003-tsx-ui-scripting.md), which supersedes Luau, [ADR 0002](../../docs/adr/0002-luau-ui-scripting.md); probe [09](09-tsx-goja-probe.md)). Plugins, shipping, types and dev reload follow zgui and `tauri dev`. |
| D16 | The Base repo: name, public or private, and whether zterm and the gallery move with it ([map](../../.scratch/browser-parity/map.md)). | Your call. Moving zterm keeps it a real Base consumer. Keep one core-only example in this repo. |

## 6. Performance track

[02](02-motion-and-compositing.md) and [app-gaps](../app-gaps/README.md) (zgui, gpui-fast) describe the same pipeline. The baseline is 8.5 ms per frame for 1,000 divs and 31 ms for the text page ([02 §2](02-motion-and-compositing.md)). Every frame resets layout (`gpui/render.go:108`), rebuilds the scene and redraws the window.

| Step | What | Go | Native | Needs |
|---|---|---|---|---|
| 1 | **Damage.** A reused `CachedView` range at the same bounds is undamaged (Chrome's chunk rule); everything else damages old ∪ new bounds. Add a paint-flashing overlay. | M | R16 retained target, scissor, `Present1` | — |
| 2 | **View reuse.** Reuse views whose inputs didn't change, as gpui-fast does, so step 1 has reused ranges to find. Fix a11y nodes dropped under caching ([app-gaps](../app-gaps/README.md) feature 10). | XL | — | 1 |
| 3 | **Property-only frames.** Transform and opacity transitions update scene property nodes and skip render, layout and paint. `RequestAnimationFrame` (`gpui/imagecache.go:1036-1048`) stops notifying the whole view. | XL | R16 property nodes | 1, M2, M6 |
| 4 | **Retained layout and text measurement.** Stop resetting the engine each frame and re-measure only dirty nodes. | L | R17 layout ABI | 2 |
| 5 | **Scroll layers.** A scroll tick becomes a property-only frame plus scroll-by-copy of the exposed strip (zgui's model; no tiles). | L | R17 | 1, 3 |

Measure after each step against the gpui-perf baseline; paint flashing is the check for step 1. Threading isn't needed: the win comes from skipping work ([02 §2](02-motion-and-compositing.md)).

## 7. Hot reload track

From [08](08-hot-reload-and-ui-scripting.md):
- **Now (Go, S-M):** a dev runner that watches the files, rebuilds and restarts on the same window position, and restores state the app declared with keys. It takes about 3-5 s per edit (measured: 2.8-5.2 s for an app file). Loading the DLL from disk in dev builds could shorten the link [I].
- **Later (L-XL):** a zgui-style script layer. The script describes the tree once; Go owns keyed state that survives reload; the engine runs animations. Its host elements and styles are exactly this proposal's browser-like set, so one surface serves Go and scripts. The default is Luau in the native DLL (proven by zgui, needs a rebuild); goja is the alternative if TypeScript matters more.
- **Not gpuix's model:** it needs a separate JS runtime, and desktop reload loses state.
- **Open questions (D15):** a second language at all? Luau or goja? Plugins? Is yaegi (a Go interpreter) worth one probe, given that its generics support may rule it out [I]?

## 8. Risks and what was not verified

1. **Q1/Q2 and the shadow clip-out rest on shader reads from a newer CE checkout** (`beb5a6d` in the cargo cache, 7 days past the pin; [01](01-visual-primitives.md) marks them [V\*]). Before changing blur sizes, read `ce:crates/gpui_render/src/shaders/shadows.rs` and `blur.rs` at `254b5dbd`, or capture σ in a fixture.
2. **The default display** may have changed upstream (`beb5a6d` defaults to Block, while gpui-go says the pin is Flex). Check this before D6.
3. **The text gamma/contrast difference is inferred** ([04 §2](04-text-and-fonts.md)). Do no shader work until the T6 A/B measures it.
4. **The font fallback issues are inferred**: tofu for symbols, and Japanese Han drawn in a Chinese face. Probe them with test strings before R17 ([04 §5](04-text-and-fonts.md)).
5. **Behaviour read from code, not run:**
   - nested scrollers moving together ([03 §6](03-input-and-events.md));
   - Base popover dismissal faults ([06 §1](06-platform-features.md));
   - keydowns dropped during IME composition ([03 §9](03-input-and-events.md)).
   Write a failing test first for each.
6. **Layout cache hazard [I]:** a max-content query after a definite-width measure may get the wrapped size (`gpui/div.go:592-594`). L1 must key the cache by mode anyway.
7. **Transforms and hit testing:** that hitboxes are rectangles, and that cc composites transformed layers the way R16 plans, are both [I] ([01 §3](01-visual-primitives.md)).
8. **Gradient diagonal normalization** not reaching the corners is [I], from reading the formula.
9. **Spec text not reached** by the fetch helper: close watchers, `title`, spellcheck, the popover light-dismiss steps, the focus fixup rule, and the `position-visibility` keyword's spelling ([06](06-platform-features.md) "Spec check"). The proposal follows Chromium there.
10. **Chromium's Windows-only pieces outside the sparse checkout:** the context menu on mouse-up, the tooltip delays in `ui/views`, the text-scale feed in `content/`, and WM_POINTER handling. All are [I].
11. **Report inconsistencies this proposal resolves:**
    - [02 §5](02-motion-and-compositing.md) puts keyframes in both the core and components. Here keyframe *interpolation* is core and keyframe *presets* are components.
    - [04 §4](04-text-and-fonts.md) calls `gpui/line_wrapper.go` Base's. It lives in `gpui/` but was restored for Base, so it moves out (§3).
12. **Hot reload estimates.** Only the Go build times were measured. The Luau/goja edit-to-screen times are [I].

## Orchestrator check against the pin (2026-10-23)

- **D8, blur sizes:** confirmed at the pinned CE `254b5db`.
  - Shadows use σ = blur radius (`crates/gpui_render/src/shaders/shadows.rs:67`, `gaussian(sample_position, shadow.blur_radius)`). CSS uses σ = blur/2.
  - Filter blur uses σ = radius × 0.5 (`crates/gpui_render/src/blur.rs:17,52`). CSS uses σ = radius.
- **D6, default display:** the pin defaults to `Display::Flex` (`crates/gpui/src/style.rs:1027`), so gpui-go matches its pin. `Block` is a newer upstream change, and it is also the browser default.
