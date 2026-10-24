# 29 - Forced-colours pass

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 27
Triage: ready-for-agent

**What to build:** In Windows high-contrast mode an optional forced-colours pass maps colours to system colours during style resolution, as Chromium does.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion)

**Status:** ready-for-agent

- [x] Fixtures rendered with forced-colors active in Chrome match gpui-go with the pass on.
- [x] The pass is off when high contrast is off.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-26, worker (core/29-forced-colors): the pass, the opt-out, the Tailwind class, the harness's emulated-media + computed-probe + app-configure support and the `forcedcolors` fixture are implemented. Acceptance: `TestFixtures/forcedcolors` (all three tiers) passes; the pass-off discrimination and unit tests pin the off state. Details in the Answer below.

## Answer

Implemented on `core/29-forced-colors`. All Go; no native code, no DLL rebuild.

### The switch: preference-driven, with a pin

The pass is **ON exactly when the system reports forced colours** — `App.ForcedColors()` derives it live from `App.SystemPreferences().ForcedColors` (ticket 27's record, `SPI_GETHIGHCONTRAST & HCF_HIGHCONTRASTON`, refreshed on the settings messages) and the palette from `App.SystemColors()`. Why preference-driven and not an opt-in: forced colours is an OS state, not an app decision — an app that opted in would stay wrong the moment the user turns high contrast off, and Chromium never makes it a page decision (the media feature and the document flag come from the OS/DevTools). The deterministic spelling for tests and previews is `App.SetForcedColors(&ForcedColorsState{Active, Colors})` (nil clears): it pins the state until cleared and queues `RefreshWindows` on a change (the `SetReduceMotion` shape). A hostless test app answers the default record, so the pass is off unless pinned — which is the second acceptance's unit-tested state. The derivation is per-frame (style resolution re-derives every frame), so a `WM_SETTINGCHANGE` flip shows on the next redraw with no extra invalidation machinery.

### API

- `App.ForcedColors() ForcedColorsState` — `{Active bool, Colors SystemColors}`: the pinned state, or the derived one.
- `App.SetForcedColors(state *ForcedColorsState)` — pin/clear (tests, a high-contrast preview toggle).
- `Styled.ForcedColorAdjustNone()` — the per-element opt-out (CSS `forced-color-adjust: none`), plus the Tailwind class `forced-color-adjust-none` (generated into `applyMethodClass`, so the class and the method cannot disagree).
- `Style.ForcedColorAdjust` / `StyleRefinement.ForcedColorAdjust Optional[ForcedColorAdjust]` — `ForcedColorAdjustAuto` (default) / `ForcedColorAdjustNone`.

### What changed

- `gpui/forced_colors.go` (new) — the pass: `ForcedColorAdjust` (:65), `ForcedColorsState` (:92), `App.ForcedColors` (:108), `App.SetForcedColors` (:130), `applyForcedColorsToStyle` (:145, the mapping), `forcedColorsActive` (:235, the cheap gate). The mapping: text colour → CanvasText (:162 — sets the refinement's colour so it also cascades, the port's spelling of the inherited `-internal-forced-color`); background → Canvas with the authored **alpha kept** (:179, gradients cleared like background images); border colour → CanvasText (:191); ring/inset-ring (outline) explicit colours → CanvasText (:200); underline/strikethrough (text-decoration) colours → CanvasText; box shadows → none (:224).
- `gpui/interact.go:1186-1192` — the seam: `applyForcedColorsToStyle` at the end of `DivElement.computeStyleInternal` (all three phases flow through it), exactly where Chromium's adjuster runs after the cascade.
- `gpui/img.go:385-394` — the image's `computeStyle(w, app)` gains the same pass (the other style-resolution site), applied at its three call sites (:465, :541, :567).
- `gpui/app.go:113-117` — the `forcedColorsOverride` field.
- `gpui/style.go:941-946`, `gpui/styled_refinement.go:168-172` (+ refine/merge) — the `ForcedColorAdjust` style field and refinement.
- `gpui/styled.go:277-296` — `ForcedColorAdjustNone()` (hand-written like `Static`; the pinned styles.rs has no such variant).
- `gpui/class_generated.go:386-387` — the `forced-color-adjust-none` class (regenerated with `cmd/tailwindgen`; the only generated diff).
- Harness: `internal/parity/fixture.go` — `EmulatedMedia []EmulatedMediaFeature` (:216, record-time media-feature emulation), `ComputedScript` (:225, page-computed values into the record), `Configure func(app) error` (:234, app-level state before the first frame); `internal/parity/recorder.go` — `Emulation.setEmulatedMedia` before the page loads (:131-136, :452-462) and the `ComputedScript` evaluation (:191-201); `internal/parity/record.go:41-45` — `Record.Computed`; `internal/parity/replay.go:89-97` — `Configure` runs right after the test app is created. Documented in `docs/browser-parity-harness.md` (the fixture-writing guide's new paragraphs) and `docs/tailwind.md` (the class).

### The fixture (internal/parity/fixtures/forcedcolors, registered in replay_test.go and cmd/parityrecord)

The page records under emulated `forced-colors: active`; the recorded palette (Chrome 155.0.8059.85's emulated forced-colours palette, read off the page's 19 hidden `[data-color-probe]` elements through the new `ComputedScript` → `Record.Computed`) is `Canvas #000000`, `CanvasText #ffffff`, `LinkText/VisitedText/ActiveText #ffff00`, `Highlight #1aebff`, `GrayText #3ff23f`, `Mark #ffff00`, `SelectedItem #1967d2` — a black-canvas high-contrast theme, not this PC's palette, which is exactly why the replay injects the recorded values (`Configure` → `App.SetForcedColors`) instead of reading the machine. Coverage: a red-text line (→ CanvasText), an `rgba(37,99,235,0.5)` background with a green border (→ Canvas α=0.5, border CanvasText), a shadowed blue box (shadow removed, background → Canvas), and a `forced-color-adjust: none` box (purple background, yellow text, orange border, shadow all kept). Boxes: root 0,0,640×400; the four 240×64 boxes at (200,42), (200,126), (200,210), (200,294) — Tier A exact, Tier B empty (a pure painting fixture).

Tier C measured (correct view, stable across runs): whole mean 0.1416 / hot 0.00172; text-line 1.6748 / 0.02295 (white ClearType-vs-Chrome text ink, the textpaint fixtures' known noise); keep 0.4107 / 0.00575; **alpha and shadow 0.0000** — the mapped fills and the dropped shadow are pixel-exact. Gates at 2×: `MaxMeanChannelDiff 0.3`, `MaxROIMeanChannelDiff 3.6`, `MaxHotPixelFraction 0.05`. The deliberately-wrong replays fail by ~2 orders of magnitude (pass-off: whole mean 72.17; swapped Canvas/CanvasText palette: 78.42).

### Chromium citations (pinned checkout `%TEMP%\research-src\chromium` @ 0d4dcd4f)

- The adjuster: `third_party/blink/renderer/core/css/resolver/style_adjuster.cc:1034-1058` `AdjustForForcedColorsMode` — the gate (`InForcedColorsMode && ForcedColorAdjust == kAuto`, :1036-1038), text/box shadows reset to initial (:1039-1040), background images cleared unless URL images (:1050-1053); wired from `AdjustComputedStyle` at :1326-1327 (the end of style resolution).
- The forced-colour properties and their defaults: `third_party/blink/renderer/core/css/css_properties.json5:10773-10791` (`-internal-forced-background-color`, default `StyleColor(CSSValueID::kCanvas)`), :10793-10805 (`-internal-forced-border-color`, default CurrentColor), :10807-10823 (`-internal-forced-color`, default `StyleColor(CSSValueID::kCanvastext)`, **inherited**), :10825-10837 (`-internal-forced-outline-color`, CurrentColor), :10839-10855 (`-internal-forced-visited-color`, Canvastext).
- The accessors that map each CSS property onto those: `third_party/blink/renderer/core/css/properties/longhands/longhands_custom.cc:2470-2478` (`color`), :1168-1186 (`background-color`), :1431-1446/:1707/:1794/:1881 (border-*-color), :6130-6156 (`InternalForcedBackgroundColor::ColorIncludingFallback` — the unforced alpha is read at :6140/:6147 and set onto the forced colour at :6156, the alpha-kept rule), :6175-6190 (border current color), :6248-6260 (outline), :2252-2270 (caret → `GetInternalForcedCurrentColor`), :10036-10047 (text-decoration → the forced current colour), :2763-2770 (column-rule), :4252/:9742 (fill/stroke).
- The gate predicate: `third_party/blink/renderer/core/style/computed_style.h:2917-2922` `ShouldForceColor` (in forced colours && adjust auto && `!IsSystemColorIncludingDeprecated`).
- The UA element rules (why links are LinkText, inputs Canvas/ButtonText, mark yellow/black): `third_party/blink/renderer/core/html/resources/forced_colors.css` (`a:link → -internal-forced-color: LinkText`, `input → Canvas/ButtonText`, `button → ButtonFace/ButtonText/ButtonBorder`, `mark → yellow/black`), matched only in forced-colours sessions from `style_resolver.cc:1177-1181`.
- The document flag: `core/dom/document.cc:9940-9943` (`in_forced_colors_mode_` from settings, seeded by `web_view_impl.cc:1931` `SetInForcedColors(prefs.in_forced_colors)` — the WebPreferences the CDP emulation writes).

### Tests

- `gpui/forced_colors_test.go` (written first, red): `TestForcedColorsPassIsOffWithoutForcedColors` (:81) and `TestForcedColorsPassIsOffWhenTheOverrideReportsNoForcedColors` (:111) — the off acceptance; `TestForcedColorsMapsStyleColorsToSystemColors` (:129), `TestForcedColorsForcesUnauthoredColorsToo` (:175, the inherited case), `TestForcedColorsKeepsTransparentBackgroundsInvisible` (:192), `TestForcedColorsClearsGradientBackgrounds` (:205), `TestForcedColorAdjustNoneOptsOutOfThePass` (:224), `TestForcedColorAdjustNoneClass` (:255), `TestSetForcedColorsPinsAndClearsTheOverride` (:270), `TestForcedColorsMapsTheImageStyle` (:289).
- `gpui/forced_colors_windows_test.go:23` `TestForcedColorsFollowsSystemPreferences` — a host app with the injected snapshot source: pass off, then `WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST)` flips the record into forced colours and `App.ForcedColors()` turns active with the injected palette (ticket 27's message machinery).
- `internal/parity/fixtures/forcedcolors/forcedcolors_test.go` — `TestPassOffFailsOnlyThePixelComparison` (:54, the no-op configure: authored colours against the forced record fail Tier C at whole mean 72.17, boxes unchanged) and `TestWrongPaletteFailsOnlyThePixelComparison` (:74, Canvas/CanvasText swapped: 78.42 — the mapping reads the palette, so a hardcoded white/black pass cannot pass).
- `internal/parity` — `TestFixtures/forcedcolors` (all three tiers) and `TestPixelTolerancesKeepHeadroom/forcedcolors` (2× headroom).

### Limitations

- Chromium's per-element UA rules (link → LinkText, input/button → ButtonFace/Canvas, mark → yellow/black, `forced_colors.css`) have no port equivalent: gpui-go has no element-type UA layer, so an element that must render LinkText under forced colours opts out (`ForcedColorAdjustNone`) and sets the colour — the app is the UA. Link-styled elements are future work with the editor/input components.
- The system-colour escape hatch (authoring `color: LinkText` — an explicit system colour is never forced, `IsSystemColorIncludingDeprecated`) has no spelling: gpui-go colours are plain `Hsla` with no keyword identity. The opt-out is the port's replacement.
- `forced-color-adjust: preserve-parent-color` (Blink's third value) is not ported — the port has no separate authored/forced cascade to bridge.
- caret-color (the editor's own caret), column-rule, fill/stroke, text-shadow, scrollbar-colour and accent-colour have no Style-layer equivalents; mapped the day those exist. A WM_SETTINGCHANGE flip becomes visible on the next redraw (apps wanting an immediate repaint call `RefreshWindows` from `OnSystemPreferencesChanged`; Chromium repaints on the message itself).
- The emulated forced-colours palette Chrome answers on this machine (black Canvas) is itself a fixture-level pin: the record's palette, not a live reading.
