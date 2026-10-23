# 27 - SystemPreferences, system colours and live reduced motion

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Apps read a SystemPreferences record (reduced motion, contrast, transparency, accent colour, text scale, caret blink) and SystemColors, with change events when Windows settings change; reduced motion is now core and live.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Simulated WM_SETTINGCHANGE updates the record and fires a change event.
- [x] Contrast preference follows Chromium's rule for Windows high-contrast palettes.
- [x] Base's own reduced-motion read can be dropped later (ticket 74).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Implemented on `core/27-system-prefs` (commit: see the branch history). All Go; no native rebuild. No visible windows at test time; `GPUI_REAL_INPUT_TESTS` was never set; the user's settings were never written (every registry/SPI access is a read).

### API

- `App.SystemPreferences() SystemPreferences` — the record: `ColorScheme` (light/dark), `ReducedMotion`, `ForcedColors`, `Contrast` (a `ContrastPreference`: no-preference/more/less/custom), `ReducedTransparency`, `InvertedColors`, `OverlayScrollbars`, `AccentColor Hsla`, `CaretBlinkInterval time.Duration` (0 = steady caret), `TextScale float32` (1.0 = 100%). Test apps (no host) answer the documented default record (light, animations on, 500 ms blink, 1.0 scale).
- `App.SystemColors() SystemColors` — the CSS Color 4 set (Canvas, CanvasText, LinkText, VisitedText, ActiveText, ButtonFace, ButtonText, ButtonBorder, Field, FieldText, Highlight, HighlightText, SelectedItem, SelectedItemText, Mark, MarkText, GrayText, AccentColor, AccentColorText).
- `App.OnSystemPreferencesChanged(func(prefs, colors))` — the change event: an application update with the new record and colours, fired whenever a settings message changed them. Host-level twins: `Host.SystemPreferences/SystemColors/OnSystemPreferencesChanged`.
- The reads: `systemSettingsSnapshot` (one raw OS reading) through the injectable `systemSettingsSnapshotProvider`; `deriveSystemPreferences`/`deriveSystemColors` are pure.

### What changed

- `gpui/system_prefs.go` (new) — the types and the pure derivation: `ContrastPreference` (`:38`), `SystemPreferences` (`:73`), `SystemColors` (`:115`), the snapshot (`:185`), the default snapshot (`:223`), `caretBlinkInterval` (`:260`), the WCAG luminance/ratio helpers (`:286-303`), `contrastPreference` (`:309`), `deriveSystemPreferences` (`:321`), `deriveSystemColors` (`:356`), the App surface (`:430-476`).
- `gpui/system_prefs_windows.go` (new) — the real reads and the host plumbing: constants (`:57-92`), the seam `systemSettingsSnapshotProvider` (`:113`), `readSystemSettingsSnapshot` (`:118` — registry + SystemParametersInfoW + GetSysColor + GetCaretBlinkTime, pure-stdlib syscalls), `readRegistryDword` (`:213`), the host seed/refresh (`:249-277`), the Host accessors (`:279-293`), `Host.OnSystemPreferencesChanged` (`:296`), `handleSystemPrefsChangeMsg` (`:312`).
- `gpui/system_prefs_other.go` (new) — the non-Windows stubs.
- `gpui/desktop_windows.go` — the desktop callback cell `systemPreferencesChanged` (`:252-256`), the host-thread cache `systemPrefs/systemColors` (`:310-317`), the seed at host boot (`:375`), the `isFuncZero` case (`:436-437`), and every `WM_SETTINGCHANGE` refreshes the record (`:1825-1833`, both the parameter-action and the area-string spellings).
- `gpui/win32host_windows.go` — `WM_SYSCOLORCHANGE`, `WM_THEMECHANGED` and `WM_DWMCOLORIZATIONCOLORCHANGED` routed to the refresh (`:826-836`); `NewHost` seeds the default record (`:1489-1492`).

Base is untouched. How Base reads reduced motion today (for ticket 74): `base/reduce_motion_windows.go:20-38` reads `SPI_GETCLIENTAREAANIMATION` once (failed call = "unknown, leave the flag alone"), `base/reduce_motion.go` applies it at Init and `platformFollowReduceMotion` is a no-op ("Windows posts no notification Base can follow without a window", `base/reduce_motion_windows.go:41-43`). The core replaces both halves: `App.SystemPreferences().ReducedMotion` (same SPI, and a failed query derives "not reduced" exactly like Chromium's `prefers_reduced_motion_ = !win_anim_enabled` with the BOOL pre-set to true) and the live follow via `App.OnSystemPreferencesChanged` (the host's windows receive `WM_SETTINGCHANGE`). Base can drop its reader in ticket 74 without behavioural change beyond becoming live.

### Chromium citations

Pinned checkout (`%TEMP%\research-src\chromium`, `0d4dcd4f`):

- Forced colours = `SPI_GETHIGHCONTRAST & HCF_HIGHCONTRASTON`, refreshed on `WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST)`: `ui/native_theme/os_settings_provider_win.cc:30-37, 293-298`.
- `prefers-contrast` from the forced palette: ratio of the Window and WindowText system colours, `>= 7` → more, `<= 2.5` → less, else custom, no forced colours → no preference: `ui/native_theme/os_settings_provider.cc:179-220`. Luminance/ratio: `ui/gfx/color_utils.cc:411-418, 424-438` (Linearize, GetRelativeLuminance4f, GetContrastRatio).
- The five OS colours the provider exposes (Window, WindowText, ButtonFace, …): `ui/native_theme/os_settings_provider_win.cc:270-286` (UpdateColors).
- `WM_SYSCOLORCHANGE` re-reads them and force-notifies in forced colours: `ui/native_theme/os_settings_provider_win.cc:288-292`.
- Transparency: `Personalize!EnableTransparency` (`:205-214`); inverted colours: `ColorFiltering!Active/FilterType == 1` (`:217-231`); overlay scrollbars: `Control Panel\Accessibility!DynamicScrollbars` (`:254-260`); defaults from `os_settings_provider_win.h:77-86`.
- Caret blink: `GetCaretBlinkTime`, 0 = default, `INFINITE` = steady, else ms: `ui/native_theme/os_settings_provider_win.cc:147-160`; the 500 ms default: `os_settings_provider.h:57`.
- Reduced motion: `SPI_GETCLIENTAREAANIMATION`, failed call assumes animations enabled, refreshed when `WM_SETTINGCHANGE` is received: `ui/gfx/animation/animation_win.cc:19-24, 36-47`; the caching quirk: `animation.h:160-170`.
- The dark/default CSS system colours: `third_party/blink/renderer/core/layout/layout_theme.cc:633-754` (DefaultSystemColor), the CSS id choices `:787-860` (SystemColorFromColorProvider — Canvas→kColorCssSystemWindow, ButtonBorder grouped with ButtonText at `:800-808`), Mark/MarkText `:733-737`, the accent default `0x0075FF` `:961-972`, AccentColorText `:973-980`.
- The `CSSAccentColorKeyword`/`CSSActiveCaptionMapsToCanvas` statuses: `third_party/blink/renderer/platform/runtime_enabled_features.json5:1585-1610`.
- Text scale: Blink's hook is `SetAccessibilityFontScaleFactor` ("TODO: Rename this OSTextScaleFactor"), `core/exported/web_settings_impl.cc:158-161`; the Windows feeder lives in `content/` (not in the checkout — [I]).

Fetched from `chromium.googlesource.com/chromium/src` main on 2026-10-25 (`ui/color` is not in the sparse checkout):

- `ui/color/win/css_system_color_mixer_win.cc:19-34` — native colours map onto the CSS system colours only for forced colours or light mode ("Windows' native colors don't get updated for dark mode"); the mapping (BtnFace/BtnText/GrayText/Highlight/HighlightText/Hotlight/MenuHilight/Scrollbar/Window/WindowText; Field=Window; FieldText=WindowText; ActiveText/LinkText/VisitedText=Hotlight).
- `ui/color/color_provider_utils.cc` `CompleteDefaultCssSystemColorDefinition` — the dark-mode default palette (values agree with the pinned blink `DefaultSystemColor`).
- `ui/color/win/native_color_mixers_win.cc:32-46` `GetCurrentSysColors` — the exact set of `GetSysColor` indices read; the high-contrast selection recipes (Highlight/HighlightText) at `:214-224`.
- `ui/color/win/accent_color_observer.cc:126-131` — the accent colour's registry fallback `HKCU\SOFTWARE\Microsoft\Windows\DWM!AccentColor` (COLORREF); the WinRT `IUISettings3::GetColorValue(UIColorType_Accent)` read it prefers (`:15-45`) is not portable to the pure-stdlib host, so the port uses the fallback registry read — the same deviation the appearance read already makes.
- `internal/native` cross-build for linux is broken on main (`a11y.go`/`capture.go` lack a windows build tag); Windows is the target per the platform contract.

### Tests

`gpui/system_prefs_windows_test.go` (new, written first — red, then green):

- `:32` `TestContrastPreferenceFollowsChromiumRuleForHighContrastPalettes` — the acceptance rule: 21:1 → more, 1:1 → less, ~4.5:1 → custom, unforced → no preference.
- `:69` `TestCaretBlinkIntervalMapsGetCaretBlinkTime` — 0 → 500 ms, `INFINITE` → steady, 530 → 530 ms.
- `:91` `TestReducedMotionFollowsClientAreaAnimation` — off → reduced; unknown → assume enabled.
- `:117` `TestTextScaleMapsTheAccessibilityRegistryPercent` — 100/125/225/0 %.
- `:144` `TestSystemColorsUseTheOSPaletteWhenForcedOrLight` — every field against injected sys colours, both modes; Mark/MarkText constants.
- `:196` `TestSystemColorsDarkModeUsesTheBrowserDefaults` — the dark palette.
- `:230` `TestAccentColorDefaultAndTextContrast` — the 0x0075FF default, white/black text.
- `:264` `TestSystemPreferencesRefreshOnSettingChangeMessages` — the main acceptance on a real window parked at origin 20000,20000 with an injected snapshot source: `WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST)` (reduced motion + text scale flip), `WM_SETTINGCHANGE(0, "ImmersiveColorSet")` (colour scheme flip), `WM_SYSCOLORCHANGE` (palette flip), `WM_THEMECHANGED` and `WM_DWMCOLORIZATIONCOLORCHANGED` (accent flips) each update the record and fire the event; an unchanged reading fires nothing.
- `:395` `TestSystemPreferencesSysColorChangeForcesNotifyInForcedColors` — Chromium's force_notify rule.
- `:440` `TestSystemPreferencesAppObserverRouting` — the App-level event and accessors, forced-colours flip with Contrast=more.
- `:495` `TestAppWithoutHostAnswersDefaultSystemPreferences` — the test-app default.
- `:516` `TestReadSystemSettingsSnapshotReadsThisMachine` — the real provider, read-only.

Results (`go test ./gpui -count=1` ×8 and `go test ./gpui/... ./base -count=1` ×4, all green; `go vet ./gpui/... ./base`, `gofmt -l` on touched files, `go build ./...` clean): 1777 gpui tests + base pass. The pre-existing `internal/portfixture` TestFx0004 failure (font count 94 vs 95) and the `internal/fontspec/weight_synthesis_test.go` gofmt flag are unchanged and unrelated. One transient full-suite failure was observed once under parallel package load (not reproduced in 11 subsequent runs, not one of the new tests — the new tests are synchronous: `SendMessageW` blocks until the host thread handles the message, so the observer has fired before the call returns).

### Limitations

- Registry changes that broadcast no message (the `EnableTransparency`/`ColorFiltering`/`DynamicScrollbars` keys, which Chromium watches with `RegNotifyChangeKeyValue`, `os_settings_provider_win.cc:162-247`) refresh only when one of the four messages arrives; a dedicated registry watch is future work. The `ImmersiveColorSet` report covers the Personalize page in practice.
- A host running with zero windows receives no broadcasts (the platform window is message-only), so the record stops refreshing until a window exists; apps read the seeded record. Chromium keeps a hidden singleton window for this (`gfx::SingletonHwnd`); adding one would interfere with quit-on-last-window and is left out deliberately.
- Under forced colours, Chromium derives the preferred colour scheme from the Window colour's LAB lightness (`os_settings_provider.cc:120-152`); this record reports the registry scheme — that refinement belongs with the forced-colours pass (ticket 29).
- Blink's AccentColorText literal (`layout_theme.cc:973-980`) compares the [0,1] luminance against 128 and so always answers white; the port implements the Firefox rule the comment documents (white on a dark accent), noted at the code site.
- TextScaleFactor's registry path is the undocumented backing store of the WinRT `UISettings.TextScaleFactor` (research 06 §10, [I] for the path); the blink hook's Windows feeder is in `content/` (not in the checkout).
