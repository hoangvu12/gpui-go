# Spec: a browser-like gpui-go core, TSX UI scripting, and Base in its own repo

Parent: map.md
Labels: spec
Triage: ready-for-agent
Status: open
Sources: [the proposal](../../research/browser-parity/README.md) (decisions D0-D16 all decided 2026-10-23), reports [01](../../research/browser-parity/01-visual-primitives.md)-[06](../../research/browser-parity/06-platform-features.md), [08](../../research/browser-parity/08-hot-reload-and-ui-scripting.md), [09](../../research/browser-parity/09-tsx-goja-probe.md), [app-gaps](../../research/app-gaps/README.md), [ADR 0001](../../docs/adr/0001-tailwind-authoring.md), [ADR 0003](../../docs/adr/0003-tsx-ui-scripting.md) (supersedes the language in [ADR 0002](../../docs/adr/0002-luau-ui-scripting.md)).

## Problem Statement

I build desktop apps on gpui-go. Much of that UI code is written by LLM coding agents, and so will most future app code. Those agents know HTML, CSS, the DOM, Tailwind and React deeply, and they have never seen gpui-go.

Today gpui-go follows GPUI-CE 1:1, and GPUI behaves differently from a browser in ways an agent (or I) can't predict:
- Text in a flex row won't shrink, so everything needs `min-w-0`.
- `absolute` ignores positioned ancestors.
- Rounded corners don't clip their children.
- Opacity is applied per child instead of to the element as a whole.
- Blur sizes don't match CSS.
- The default line height is 1.618.
- Tab order runs backwards.
- Hover sticks at the window edge and vanishes when I type.
- Ctrl+Shift+] never fires.
- Escape and click-outside only work if each component wires them up itself.
- Bold text is faked differently from Chrome.

So code that "looks right" by web knowledge comes out subtly wrong, and every component has to fake browser behaviour on its own.

Second, the framework repo ships a whole component library (Base) inside it, which feels wrong for a UI framework.

Third, iterating on UI is slow. An edit costs a 3-5 s rebuild and restart, and loses all UI state. zgui shows that scripted UI with state-keeping hot reload is far better.

Fourth, the renderer redraws the whole window every frame, has no transforms, gradients, masks or rounded clipping, and re-renders a whole view for a one-element animation. zgui and gpui-fast are measurably ahead.

## Solution

- **Browser behaviour by default.** gpui-go's core behaves like a browser wherever GPUI is silent or differs. The W3C/WHATWG specs are the rule, and Chromium on Windows decides where the spec leaves room. When the browser and the GPUI-CE pin differ, the browser wins, and each divergence is recorded with its fixture updated (D0).
- **The core owns behaviour without look:** focus and Tab, the Escape (close-request) stack, a top layer with light dismiss, modal/inert, text selection, style-driven transitions, reduced motion, system preferences, browser-correct input events, CSS layout rules, CSS text rules and CSS-correct painting. Like a browser's UA stylesheet, it ships a minimal default look (focus ring, plain tooltip, scrollbars, selection colours), but no themes or widgets.
- **Base moves out** to a new public repository, `gpui-go-base`. It becomes the look-and-composition layer built on core primitives. zterm and the gallery move with it as local test apps and are never pushed.
- **TSX UI scripting.** I can write UI in TSX, using JSX elements with CSS-named `style` objects and Tailwind `className`. esbuild compiles it and goja runs it, both pure Go. It follows zgui's model: the script describes the tree once, the host keeps state by key, and updates change bound values. Saving a `.tsx` file reloads in place in milliseconds with state kept. Changing Go code triggers an automatic rebuild and restart that restores the same UI state, like `tauri dev`. Release builds embed the compiled bundle. A generated `.d.ts` gives editors and agents type checking. Sandboxed plugins can contribute UI to slots the app declares.
- **Native work in two rebuilds:**
  - **R15:** the corrected Windows bold rule, gradients, shadow clip-out, colour filters, backdrop cropping, a pixelated sampler, and a wider text and layout interface.
  - **R16:** the compositor (group targets, group opacity, transforms, rounded clipping, masks, blend modes, clip-path, damage-limited redraw and partial present, property-only frames).

  Each needs its own approval at build time; R15 is already approved.

## User Stories

### Direction and authoring

1. As an app developer, I want gpui-go elements to behave like the HTML/CSS I already know, so that UI code written from web knowledge works the first time.
2. As an LLM coding agent, I want CSS property names, Tailwind classes and DOM event semantics to mean what they mean in a browser, so that I don't need gpui-go-specific knowledge to write correct UI.
3. As a maintainer, I want every deliberate divergence from the GPUI-CE pin recorded next to its fixture, so that I can tell intended browser behaviour from regressions.
4. As a maintainer, I want the GPUI shape (elements, views, actions, entities, the fluent styling methods) kept wherever it doesn't change what the user sees or feels, so that existing gpui-go code keeps compiling.
5. As an app developer, I want a browser default look (focus ring, plain tooltip, scrollbars, selection colours) out of the box, so that a bare app is usable and accessible without a component library.
6. As an app developer, I want the framework to ship no themed widgets, so that the core stays small and I choose my own component layer.

### Layout

7. As an app developer, I want text inside a flex row to shrink and wrap like in a browser (min-content wraps at every break opportunity), so that I don't need `min-w-0` everywhere.
8. As an app developer, I want `position: static` to exist and `absolute` to resolve against the nearest positioned ancestor, so that HTML/Tailwind layouts port unchanged. The default is introduced opt-in, then flipped when Base migrates, with a lint for `absolute` under a non-positioned parent.
9. As an app developer, I want `position: sticky`, so that list headers and toolbars stick while scrolling.
10. As an app developer, I want `position: fixed`, so that an element can be placed relative to the window regardless of its ancestors.
11. As an app developer, I want `display: block` to be the default, as in browsers, so that a plain div stacks its children. A codemod adds `.Flex()` where existing code relied on the old default.
12. As an app developer, I want `display: contents`, `inline-block`, `inline-grid` and `flow-root`, so that common CSS layouts are expressible.
13. As an app developer, I want a scroll container's range to include all descendant overflow and trailing margins, so that the end of the content is always reachable.
14. As an app developer, I want `overflow: auto` and an opt-in `scrollbar-gutter: stable`, so that scrollbars appear only when needed without shifting layout when I don't want them to.
15. As an app developer, I want full grid track lists (`240px 1fr`, auto rows, areas) and `justify-items`/`justify-self`, so that real grid layouts are expressible.
16. As an app developer, I want `items-baseline` to align text baselines, so that mixed-size labels line up.
17. As an app developer, I want border widths floored to device pixels and sub-pixel hairlines kept at one device pixel, so that 1px borders look like Chrome's at any scale.
18. As an app developer, I want percentage padding to resolve against the containing block's width everywhere, so that layout matches CSS.

### Text and fonts

19. As an app developer, I want bold synthesized exactly when Chrome on Windows does (requested weight at least 600 over a face below 600), so that bold text looks the same as in the browser.
20. As an app developer, I want the default line height to be CSS `normal` (from the font's metrics), with 1.618 available as an opt-in, so that text rhythm matches the web.
21. As an app developer, I want `uppercase`, `lowercase` and `capitalize` to actually transform text, so that the style does what it says.
22. As an app developer, I want white-space collapsing (`normal`, `nowrap`, `pre-line`), so that source formatting doesn't leak into rendered text.
23. As an app developer, I want `word-break`, `overflow-wrap` and `text-wrap: balance`, so that long words and headings wrap like on the web.
24. As an app developer, I want `line-clamp-N` to end with an ellipsis, so that clamped text looks clamped.
25. As an app developer, I want non-zero letter-spacing to turn off ligatures, so that tracked text matches the browser.
26. As an app developer, I want underlines at Chrome's offset and thickness, so that links look right.
27. As an app developer, I want the generic families `sans-serif`, `serif` and `monospace`, and `tabular-nums` as a class, so that common font CSS works.
28. As an app developer, I want tab stops of 8 spaces under `pre`, so that preformatted text matches CSS.
29. As an app developer, I want grayscale antialiasing on non-opaque windows, so that text on glass doesn't show colour fringes.
30. As an app developer, I want `justify`, `start` and `end` text alignment, so that paragraphs align as in CSS.
31. As a maintainer, I want a measured A/B of text rasterization against Chrome (dark, light and grey text; grayscale and ClearType), so that gamma/contrast shader work happens only if a visible gap exists.

### Visual primitives

32. As an app developer, I want rounded corners to clip descendants, so that images and children don't poke out of rounded cards.
33. As an app developer, I want `opacity` to apply to the element as a whole (group opacity), so that overlapping children don't show through each other.
34. As an app developer, I want CSS transforms (translate, scale, rotate, skew, transform-origin) on any element, with hit testing that follows the transform, so that press effects, pop-ins and spinners work like on the web.
35. As an app developer, I want linear gradients now and radial, conic, repeating and n-stop gradients later, interpolated like CSS, so that gradient backgrounds and Tailwind `bg-linear`/`from`/`to` work.
36. As an app developer, I want `mask-image`, edge fades, `clip-path` and `mix-blend-mode`, so that scroll-edge fades and composited effects are possible.
37. As an app developer, I want shadow blur, `filter: blur` and `backdrop-filter` blur at CSS sizes, so that ported values look the same as in Chrome.
38. As an app developer, I want box shadows that grow their corner radii with spread, paint first-on-top, clip out from under the element, and sit inside the filter group, so that shadows match CSS.
39. As an app developer, I want colour filters (grayscale, sepia, brightness, contrast, …) and `drop-shadow()`, so that CSS filter values work.
40. As an app developer, I want backdrop blur cropped at the element edge, so that content just outside the box doesn't bleed in.
41. As an app developer, I want unlimited nesting of filter and opacity groups, so that a third nested filter no longer silently loses its blur.
42. As an app developer, I want `outline` with offset, painted above the content, so that focus rings render like Chrome's.
43. As an app developer, I want the border painted before the children, so that the stacking order matches CSS.
44. As an app developer, I want `object-fit: none` centred, `object-position`, images fitted to the content box, and `image-rendering: pixelated`, so that images behave like `<img>`.
45. As an app developer, I want unequal corner radii scaled together when they overflow, so that pill shapes match CSS.

### Input and events

46. As an app developer, I want per-element pointer enter and leave events, and hover cleared when the pointer leaves the window, so that hover styles never stick.
47. As an app developer, I want hover to survive key presses, so that typing doesn't drop hover styles.
48. As an app developer, I want pointer capture to send events to the captured element, with got/lost callbacks, and capture lost to another window to cancel cleanly, so that drags never leave a button stuck.
49. As an app developer, I want an element-local pointer position, so that sliders and dividers don't compute it from hitbox bounds.
50. As an app developer, I want click to go to the common ancestor when press and release land on different children, so that clicks behave like the DOM.
51. As an app developer, I want Enter to activate on key-down and Space to keep `:active` while held, so that keyboard activation feels like a browser.
52. As an app developer, I want `:focus-visible` to follow the focus origin (keyboard vs pointer), with modified keys like Ctrl+S not counting, so that focus rings appear when expected.
53. As an app developer, I want key events to carry the physical `code` and `location` while keeping Shift, and keymaps to match either form, so that Ctrl+Shift+] fires.
54. As an app developer, I want core default actions (Tab, Escape, keyboard scrolling, activation) to run after element key listeners and honour `PreventDefault`, while key bindings still run before listeners, so that I can override defaults like in the DOM without breaking GPUI's action model. A per-window flag gives full browser order.
55. As an app developer, I want the full drag and drop sequence (drag start, enter, over with an effect, leave, drop, end), with Escape and window deactivation cancelling, so that drag interactions need no workarounds.
56. As an app developer, I want files dragged in from other apps to show "none" over elements that won't accept them, so that the OS cursor tells the truth.
57. As an app developer, I want wheel latching to the innermost scroller and `overscroll-behavior: contain`, so that nested scroll areas don't scroll together.
58. As an app developer, I want Chrome's wheel step (100 px per notch at 3 lines) and the "one screen per notch" setting handled, so that scrolling feels right and never breaks.
59. As an app developer, I want keyboard scrolling (arrows, Page Up/Down, Home/End, Space) as a default action, so that scroll containers work from the keyboard.
60. As an app developer, I want an `OnContextMenu` event from right-button up, the Menu key and Shift+F10, so that context menus work from mouse and keyboard.
61. As an app developer, I want IME composition events and an `IsComposing` flag, and later a TSF text store, so that text input matches Chrome's Windows IME behaviour.
62. As an app developer, I want pen and touch pointer events, so that pointer types are distinguishable (low priority).
63. As an app developer, I want the grab, grabbing, copy and context-menu cursors to load, so that cursor styles are honoured.
64. As an app developer, I want text selectable by default (`user-select: auto`) with an automatic I-beam, and `UserSelect(None)` to opt out, so that text behaves as in a browser.

### Focus, overlays and platform

65. As an app developer, I want Tab and Shift+Tab focus navigation built into the core, in HTML tabindex order, so that keyboard navigation works without a component library.
66. As an app developer, I want a press on empty space to blur, and focus to move off an element that disappears (end-of-frame fixup), so that focus never points at nothing.
67. As an app developer, I want an `Inert()` style, so that a subtree can't be focused, clicked or read by screen readers.
68. As an app developer, I want a top layer with an ordered popover stack and click-based light dismiss that knows the invoker and nested popovers, so that popovers close the way HTML popovers do.
69. As an app developer, I want close watchers, where an unhandled Escape closes the newest overlay, so that every overlay closes with Escape regardless of focus.
70. As an app developer, I want `Window.ShowModal`, which makes everything outside inert, runs the focusing steps and restores focus on close, so that modal dialogs are correct for keyboard and screen-reader users.
71. As an app developer, I want a tooltip hook with hover and focus triggers, Chrome's delays and grace period, and a plain default tooltip, so that tooltips behave like `title`.
72. As an app developer, I want `Anchored` positioning relative to an element, with side, alignment, offset, flip/shift fallbacks and hide-when-clipped, so that menus and popovers place themselves like CSS anchor positioning.
73. As an app developer, I want standard edit actions and keys (copy, cut, paste, select all), the HTML clipboard format, and clipboard change events, so that editing works the same everywhere.
74. As an app developer, I want a `SystemPreferences` record (reduced motion, contrast, transparency, accent colour, text scale, caret blink) and `SystemColors`, with change events, so that apps follow Windows settings live.
75. As an app developer, I want rem to follow the Windows text size by default, with an opt-out, so that apps respect the user's accessibility setting.
76. As an app developer, I want an optional forced-colours pass in high-contrast mode, so that apps stay legible for high-contrast users.
77. As an app developer, I want a spellcheck service (Windows spell checker, off the UI thread) and a `Spellcheck` flag, so that text inputs can show misspellings.
78. As an app developer, I want a `TypeAhead` helper, so that lists, trees and menus share type-to-select behaviour.

### Motion and rendering

79. As an app developer, I want a `Transition` style property that animates when a style value changes, like CSS transitions, so that motion is declarative.
80. As an app developer, I want the timing, easing and keyframe interpolation engine in the core, so that components and scripts share one motion engine.
81. As an app developer, I want reduced motion read live from Windows and honoured by transitions and smooth scrolling, so that motion-sensitive users are respected.
82. As an app developer, I want smooth scrolling for wheel, keyboard and programmatic scrolls with Chrome's curve, off under reduced motion, so that scrolling feels like a browser.
83. As an app developer, I want scroll anchoring, so that content inserted above doesn't jump the visible content.
84. As an app developer, I want a FLIP helper on top of transforms and transitions, so that layout changes can animate.
85. As an app developer, I want View Transitions eventually, so that shared-element transitions are possible (last).
86. As an app user, I want only the changed parts of the window redrawn and presented, so that the app uses less GPU and power.
87. As an app user, I want transform and opacity animations to skip layout and repaint, so that animations stay smooth and cheap.
88. As an app user, I want hidden or minimized windows to skip frames, so that background apps cost nothing.
89. As an app developer, I want unchanged views reused automatically, and layout kept between frames, so that large UIs stay fast without hand-marking cached views.
90. As a maintainer, I want a paint-flashing debug overlay, so that I can see what each frame repaints.

### TSX scripting and hot reload

91. As an app developer, I want to write UI in TSX with JSX elements, CSS-named `style` objects and Tailwind `className`, so that I (and agents) write UI in the most familiar language.
92. As an app developer, I want saving a `.tsx` file to update the running window in place within milliseconds, so that I iterate on UI instantly.
93. As an app developer, I want state declared with a key to survive every reload, so that I keep my place while editing.
94. As an app developer, I want a failed reload to keep the last good UI and show the error with the `.tsx` file and line over the window, so that a typo never kills my session.
95. As an app developer, I want updates to change only bound values instead of re-running the whole tree, so that interactions stay well under a frame.
96. As an app developer, I want a React-like authoring feel (components, props, children), so that agents write idiomatic code.
97. As an app developer, I want host-provided `setTimeout`, `fetch` and similar APIs, so that common JS code runs in the script runtime.
98. As an app developer, I want a generated `.d.ts` for every element, style property, class and event, plus a `check` command, so that editors and agents catch misspelled props and bad values before running.
99. As an app developer, I want debug builds to type-check after each reload when `tsc` is available and show errors in the window, so that type errors surface as I work.
100. As an app developer, I want release builds to embed the esbuild bundle and read no scripts from disk, with a compile error failing the build at file:line, so that shipping is a single binary.
101. As an app developer, I want a dev command that rebuilds and restarts the app when Go files change, restoring keyed script state and the window position, so that Go edits don't lose my UI state either.
102. As an app developer, I want script elements to be the same browser-like elements the Go core provides, so that one vocabulary serves Go and TSX.
103. As an app developer, I want to call Go-side actions and read Go-side shared state from scripts, so that app logic stays in Go.
104. As an app developer, I want to declare plugin slots, and plugins to contribute components into them, so that my app is extensible.
105. As an app developer, I want each plugin to reach only what I grant it (named actions, shared state, fetch, clipboard, files, …), with a missing grant reported by name, so that third-party code can't overreach.
106. As an app user, I want a failing or runaway plugin to be stopped and left out while the app keeps running, so that plugins can't break the app.

### Base repository

107. As a component author, I want Base in its own public repo, `gpui-go-base`, depending only on gpui-go's public API, so that the framework and the component library evolve separately.
108. As a component author, I want Base's dialogs, popovers, selects, menus and tooltips rebuilt on core primitives (top layer, close watchers, `ShowModal`, `Anchored`, tooltip hook, focus and selection), so that they stop faking browser behaviour per component.
109. As a component author, I want Base to keep the look: themes, palettes, motion presets (presence, stagger, sequence, reveal, springs), overscroll bounce, widget keymaps and every widget, so that nothing styled lives in the core.
110. As a maintainer, I want zterm and the gallery to live beside Base locally as test apps and never be pushed, so that they keep exercising Base without being published.
111. As a maintainer, I want the core repo to keep one core-only example, so that the framework stays demonstrable without Base.

## Implementation Decisions

### Rule and reference
- When the browser and the GPUI-CE pin differ, the browser wins (D0). The reference is the W3C/WHATWG specs, plus Chromium on Windows where the spec leaves room. Chromium's Windows-specific code wins over its generic code: the bold rule is the precedent.
- GPUI's API shape stays: elements, views, entities, actions, the fluent Styled vocabulary with its lazy generic-method marker, the Tailwind class layer (ADR 0001) and the StyleBuilder. Behaviour changes underneath. New capabilities are added as new style properties and events, named after CSS and the DOM.
- Every deliberate divergence from the pin carries a short "divergence toward CSS" note beside the fixture it changes, as the inset-shadow rule already does.

### Decided behaviour changes (D1-D14, all accepted as recommended)
- **D1:** default line height is CSS `normal`, from the font's metrics; 1.618 is an opt-in.
- **D2:** text min-content wraps at every break opportunity. The text measure cache is keyed by sizing mode.
- **D3:** `position: static` is added opt-in first. The default flips when Base migrates, with a lint for `absolute` under a non-positioned parent.
- **D4:** HTML tabindex order. Positive values come first in ascending order, then 0 in tree order; negative values are never reached by Tab.
- **D5:** key bindings run before listeners (GPUI shape). Core default actions run after listeners and honour `PreventDefault`. A per-window flag selects full browser order.
- **D6:** the default display becomes `block` (the pin is `flex`). It flips together with the Base move, with a codemod adding `.Flex()`.
- **D7:** text is selectable by default, and Base's selection model moves into the core.
- **D8:** blur sizes follow CSS. Shadows use σ = blur/2 and filters use σ = radius; the pin uses σ = blur and σ = radius/2 (verified at the pin).
- **D9:** opacity applies to the group, in R16.
- **D10:** Chrome's wheel step, 100/3 px per line.
- **D11:** rem = 16 × the Windows text scale, with a per-app opt-out.
- **D12:** the input bundle, as one change: hover survives typing, pointer-leave clears hover, Enter activates on key-down, click goes to the common ancestor, capture retargets.
- **D13:** smooth scrolling on by default, off under reduced motion.
- **D14:** two native rebuilds, R15 now and R16 after a short design.

### Core modules (new or changed behaviour)
- **Input dispatch:** pointer enter/leave from a hover diff, pointer-left-window, capture retargeting with got/lost and a cancel on capture change, element-local positions, click on the common ancestor, key `code`/`location` with Shift kept (keymaps match both forms), the default-action phase after listeners, the drag and drop sequence with OLE effects driven by can-drop, wheel latching and overscroll containment, keyboard scrolling, the context-menu event, IME composition callbacks, a pointer-type family, and extra cursors.
- **Focus:** core Tab/Shift+Tab as a default action, HTML order, blur on empty press, end-of-frame focus fixup, `Inert`, focus-visible from the focus origin, and optional scroll-into-view.
- **Overlays:** a top layer of ordered overlays (popover, hint, modal) with a backdrop slot and click-based light dismiss that knows the invoker and nested popovers. Close watchers make unhandled Escape signal the newest overlay. `Window.ShowModal`/`Close` makes the outside inert, runs the focusing steps, restores focus and supports `closedby`. A tooltip hook adds hover and focus triggers with Chrome's delays and grace. `Anchored` can target an element, with flip/shift fallbacks and hide-when-clipped.
- **Platform:** a `SystemPreferences` record and `SystemColors`, refreshed on Windows settings, colour and registry changes. Text scale drives rem. An optional forced-colours pass in style resolution. A spellcheck service off the UI thread. Core edit actions and keys, the HTML clipboard format and clipboard change events. A `TypeAhead` helper. Caret blink and highlight colours come from the platform.
- **Layout:** the min-content rule; static positioning with the nearest-positioned containing block (reparented on the Go side); sticky applied at prepaint; fixed (absolute to the window root, painted deferred); `contents`, `inline-block`, `inline-grid` and `flow-root`; `overflow: auto` and scrollbar gutter; the scroll range from the layout engine's content size; grid track lists and justify properties; baselines from measure; border flooring and hairline preservation.
- **Text:** the `normal` line-height kind, white-space collapse with a caret byte map, balance by bisection, em-based tracking, generic families, tab size 8 under `pre`, alignment, text-transform, ligatures off under letter-spacing, line-clamp ellipsis, Chrome's underline geometry, and grayscale AA on non-opaque windows.
- **Visual (Go side):** `Outline` painted after the children, with the default `auto` focus ring. The border paints before the children. CSS overflow value computation. Gradient backgrounds and their Tailwind classes. Object position and the pixelated flag. Styles emit opacity groups, transforms, masks, blend modes and clip-path to the scene plan.
- **Motion:** reduced motion in the core with live updates. A `Transition` style property driven by a per-element style diff. The timing, easing and keyframe-interpolation engine moves from Base, including CSS's reversing rule, which Base already matches. Smooth scroll with Chrome's curve. Scroll anchoring on the scroll handle. A per-window preferred interval. Transforms with a transform stack, origin and inverse hit testing. A FLIP helper. View Transitions last.
- **Moves out of the core:** the restored line wrapper, which served only Base's editor, goes with Base. The Tailwind palette and class mapper stay for now.

### Native artifact
- **R15** (approved as "rev 15"; one rebuild, built and measured like rev 13 and rev 14):
  - the Windows bold rule (weight ≥ 600 over a face < 600)
  - drop shadows clipped out of the border box
  - a gradient record (tag, angle, two stops, sRGB default, premultiplied mix, CSS line length)
  - colour-matrix filters
  - the backdrop mirror crop
  - a point sampler with a per-sprite flag
  - text run fields (word-break, overflow-wrap, text-wrap mode, locale, font variations and width, optical size, justify)
  - layout fields (a baseline in the measure response, variable-length grid tracks, justify-items/self, flow-root)
- **R16** (needs a short design ticket and its own approval):
  - a group target pool with no nesting cap
  - opacity groups
  - transformed group composite (the subtree is rendered offscreen, then composited under the matrix)
  - a rounded content mask (an rrect in the shader plus a coverage-mask stack)
  - mask composite (edge fades, gradient and image masks, alpha and luminance)
  - clip-path coverage
  - blend modes, including plus-lighter
  - n-stop, radial, conic and repeating gradients
  - the drop-shadow filter
  - scene property nodes for transform and opacity
  - a retained frame target with scissored damage redraw, dirty-rect present and blur damage expansion
  - redundant state-bind removal and a path tessellation cache
- **R17+** (measured or optional): a gamma/contrast preblend only if the rasterization A/B shows a gap; skip-ink; COLRv1 emoji; per-codepoint fallback; a retained layout interface (may ride R16 if designed in time); scroll-by-copy; View Transitions snapshots; mipmaps; elliptical radii.
- Every rebuild updates the native revision, the artifact pins and the measurements, and leaves bit-exact fixtures unchanged except for recorded divergences.

### Performance track
In order, measured against the gpui-perf baseline after each step:
1. Damage from reused view ranges (Chrome's chunk rule), with the paint-flashing overlay.
2. Automatic view reuse by tracked inputs (gpui-fast's approach), keeping accessibility nodes on reused views.
3. Property-only frames: transform and opacity transitions update scene property nodes and skip render, layout and paint; animation frames stop notifying the whole view.
4. A retained layout and text measurement.
5. Scroll as a property-only frame plus scroll-by-copy of the exposed strip.

No threading is required.

### TSX scripting (ADR 0003)
- The script host compiles `.tsx` with esbuild (ES2017 floor, bundled, source maps kept) and runs it in a goja runtime. esbuild ships only in development builds. Release builds embed a prebuilt bundle.
- **zgui's model:** the script describes the tree once per load. The host owns state keyed by string, so reload keeps it. A state change updates the bound values without re-running the tree. The script runs again only for event handlers, component mounts and computed properties. Animations run in the core motion engine.
- **Elements and styles:** script elements lower to the core's elements. `style` takes CSS property names; `className` goes through the Tailwind class layer. Events use the DOM names of the core's events.
- **The API shape, from the probe (trimmed; it encodes the two update paths and keyed state):**

  ```ts
  declare function useState<T>(key: string, init: T): [T, (value: T) => void]; // re-describes the tree (avoid on hot paths)
  declare function state<T>(key: string, init: T): State<T>;                  // host-owned, keyed, survives reload
  declare function bind<T>(s: State<T>, fn: (value: T) => string): Binding<string>; // updates only the bound value
  ```

  The probe measured a re-described update at 2.3 ms and a bound update at 0.001 ms. Bound values must be the natural path, and the spec'd API must steer authors and agents toward them.
- **Host-provided globals:** timers, fetch and the other browser-ish APIs goja lacks.
- **Errors:** a failed load or reload keeps the last good tree and shows the message with the `.tsx` file and line in a debug banner. In release, errors go to an app hook.
- **Types:** a `.d.ts` generated from the engine's element, style, class and event surface. A test fails when the engine accepts something the types lack. A `check` command runs `tsc`. Debug builds check after each reload when `tsc` is present.
- **Plugins, like zgui:** the app declares slots and plugins contribute components. Each plugin runs in its own goja runtime with a per-call time budget. Plugins reach only granted actions, shared state and capabilities (fetch, clipboard, files, …). A missing grant is a named error, and a failing plugin is left out. goja can't cap memory, so plugin memory isolation is a design choice in the scripting tickets: a separate plugin process, or a time budget only.
- **The dev command:** it watches `.tsx` files (in-place reload) and Go files (rebuild and restart). Before a restart it saves keyed script state and the window position, and restores them after.

### Base repository (D16)
- A new public repository, `gpui-go-base`. Creating it and pushing need the user's go-ahead at that step.
- It depends only on gpui-go's public API. Its components migrate onto core primitives as those land: the critical path is input and focus, then overlays.
- zterm and the gallery move beside it as local test apps and are never pushed. gpui-go keeps one core-only example.

### Order of work
1. The quick Go wins (23 items), plus the rasterization and italic probes.
2. R15, then the Go items it unblocks (gradients, image flags, flow-root, grid, baselines, balance, alignment).
3. Input and focus, then overlays (the critical path for moving Base out), with platform items in parallel.
4. The decision-gated layout and text items.
5. Motion.
6. The R16 design and build, then the visual Go side, transforms and the performance track.
7. R17+ and FLIP/View Transitions.

TSX scripting can start once the element and style vocabulary from steps 1-4 is stable enough to generate types from, and can grow with it. The Base move follows step 3.

## Testing Decisions

- **A good test checks external behaviour only:** element boxes, event sequences delivered to app code, focus and overlay state visible through the public API, and pixels. It never checks internal state, private helpers or call counts, except the established hot-path guards (for example "script calls during a resize stay at zero").
- **Seam 1, the main one: the browser-parity harness.**
  - Each fixture is a small HTML/CSS page plus the equivalent gpui-go view and a scripted input sequence.
  - Headless Chrome (installed on this PC; ticket 13 used it) records, once, as checked-in fixtures:
    - element boxes, via `getBoundingClientRect`;
    - the event log for the input sequence (event kinds, targets, order, default-prevented state);
    - screenshots.
  - The gpui-go side runs the view in the headless test window with the real text system and simulated input, and compares:
    - boxes exactly, as Tier A;
    - event logs exactly, as a Tier B transcript;
    - pixels approximately, as Tier C (the gate is derived per research 19, not invented).
  - Layout, text, input, focus, overlays, platform behaviour and visual primitives are all tested here. Fixtures record the Chromium version they were captured with.
- **Seam 2, the existing native spec packages** (scene, text, glyph, font, layout, consumer and the others). They cover R15/R16 interface changes, record formats, bit-exactness and revision pins, as rev 13 and rev 14 did; the bold weight-matrix test is the model.
- **Seam 3, the TSX script host, through the same harness.** A `.tsx` fixture loads into the headless window and is compared against the same Chrome page as its Go twin. Reload tests (state kept, last good tree kept on error, file:line in errors) and the timing thresholds from the probe (reload under 300 ms, bound update under 2 ms, frame overhead under 1 ms at the probe's size) run here. The probe's tests are the prior art.
- **Seam 4, the `gpui-go-base` repo,** which tests its components against gpui-go's public API only. Base's existing tests move with it.
- **Prior art:**
  - the headless perf benchmark with the real text system;
  - the simulated-input visual test context used by zterm's drag tests;
  - zterm's offscreen capture test;
  - the ticket 13 Chrome probe and the weight-matrix test;
  - the conformance tiers and update policy in research 19;
  - the TSX probe.
- **Rules:**
  - Tests that drive the real mouse and keyboard (`GPUI_REAL_INPUT_TESTS=1`) never run while the user is on the PC.
  - Headless and offscreen runs are the default; visible windows only when the user allows.
  - A behaviour read from code but not yet run (nested scrollers, popover dismissal faults, keydowns dropped during IME) gets a failing harness test first.

## Out of Scope

- Real terminals, ConPTY, VT emulation, and zterm's logic (zterm stays a placeholder-pane test app).
- macOS and Linux; this spec is Windows only.
- Embedded web views (WebView2). Noted in app-gaps; not part of browser parity.
- A tray icon, and native popup/floating/dialog window kinds beyond what overlays need (app-gaps items, separate work).
- Luau. Superseded by TSX (ADR 0003); yaegi was ruled out by a probe.
- Running React itself in goja. The authoring feel is React-like, but the update model is zgui's.
- Themes, palettes, styled widgets and motion presets in the core; they belong to `gpui-go-base`.
- Pushing anything, creating the GitHub repo, or tagging without the user's go-ahead at that step. zterm and the gallery are never pushed.
- Native rebuilds beyond R15 without a separate approval.

## Further Notes

- **Measured facts this spec relies on:**
  - TSX reload is about 4 ms, a bound update 0.001 ms, frame overhead within noise, about 0.6 MB of memory and about 12 MB of release binary (probe 09).
  - The Go rebuild loop is 2.8-5.2 s for an app edit (report 08).
  - Pin blur σ values and the pin's flex default were verified against the pinned source.
- **Inferred, and must be proven by a failing test or a probe first** (proposal §8):
  - the gradient diagonal normalization;
  - transformed hit testing;
  - font fallback tofu and Han selection;
  - text gamma/contrast;
  - nested scrollers moving together;
  - popover dismissal faults;
  - keydowns dropped during IME.
- **Some HTML spec text wasn't fetched** (close watchers, `title`, spellcheck, the popover light-dismiss steps, focus fixup). Where it wasn't, follow Chromium's behaviour as read in source.
- Each R-rebuild costs an approval, an evidence run and pin updates in about eight places. Bundle by readiness, not by theme.
- Prior research and evidence: the browser-parity reports, app-gaps, the zterm parity report, the gpui-perf effort and the TSX probe prototype.
