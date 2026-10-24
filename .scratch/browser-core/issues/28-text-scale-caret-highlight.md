# 28 - Text scale to rem, caret blink and highlight colours

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 8 (Claude Code)
Blocked by: 27
Triage: ready-for-agent

**What to build:** rem follows the Windows text size by default with a per-app opt-out (D11), caret blink uses the system rate (including never), and selection highlight colours come from the system.

**Blocked by:** 27 (SystemPreferences, system colours and live reduced motion)

**Status:** ready-for-agent

- [x] rem changes live when the simulated text scale changes; the opt-out holds it at 16.
- [x] Caret blink and highlight colours read from SystemPreferences.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 8 (verification): merged eb52d42 into main with no conflicts. Build, vet and gofmt are clean. With GOFLAGS=-p=4, `go test -p 1 -count=1` passes for ./gpui/, ./base, ./internal/parity/..., richtextspec, authorspec, inspectorspec, winhostspec and examples/zterm. Checked: `ds.rem` is otherwise only set at test-window creation, so `frameRemSize` overrides no app setting. Hostless test apps get scale 1, so existing fixtures are unchanged. The scale is cached per preference version (no host call per frame). A changed preference record now triggers RefreshWindows even without an app observer. Citation spot-checked: frame_caret.cc:186-190 (a zero blink interval starts no timer; the caret stays visible). Behaviour note: apps on PCs with a Windows text size other than 100% now get a scaled rem by default (D11); `SetRemFollowsSystemTextScale(false)` opts out.

## Answer

Implemented on `core/28-text-scale-caret`. All Go; no native change. Every test uses injected or default records (the hostless default, the pin, or the windows snapshot-provider seam) — never this PC's settings.

### API

- `App.TextScale() float32` — the effective scale rem resolution multiplies into the base 16 (D11): the Windows text size from the system-preference record, or 1.0 while opted out.
- `App.SetTextScale(*float32)` — the deterministic pin (nil clears; wins over the system and the opt-out; a change reflows).
- `App.SetRemFollowsSystemTextScale(bool)` — **the per-app opt-out**: false holds rem at 16 whatever Windows reports (default: on).
- `App.SelectionColors() / App.SelectionColorsForPaint() SelectionColors{Background, Foreground}` — the default selection colours from the record: Highlight in its blend-with-white wash + HighlightText; the paint spelling is the cached per-record-version read (ticket 29's `systemPrefsStamp` pattern).
- Base: the input/editor caret blinks at `SystemPreferences().CaretBlinkInterval` (0 = "never" → steady, visible, no timer); `InputEditorStyle.Resolved(app, tokens)` and `SelectableText` default their selection to the system wash (`SelectionColor` / a projected style still wins — the app's `::selection`).

### What changed

- `gpui/text_scale.go` (new) — the API, the policy, and `frameTextScale` (the stamp-keyed frame cache; one derivation per record version, plus a policy-version compare so the pin/opt-out re-derives without a record move) + `frameRemSize`.
- `gpui/selection_colors.go` (new) — `SelectionColors`, the fresh and cached reads, and `blendWithWhite` (Color::BlendWithWhite's ladder, 153..204 step 17, float32 division — Chromium's own int/float truncation quirks included).
- `gpui/render.go` — every frame recomputes the window's rem as `16 × frameTextScale()` before building the frame (`:96-103`); `WithRemSize` overrides and `RemSize` follow.
- `gpui/system_prefs_windows.go` — a changed record also repaints (`:280-293`: `attachedApp().Update → RefreshWindows`), so a text-scale change reflows live (the layout tree is rebuilt every frame) with no app observer required.
- `gpui/win32host_windows.go` — `Host.attachedApp()` (`:1925-1934`).
- `gpui/app.go` — the pin/policy/cache fields (`:129-163`); `gpui/text_scale.go` — `TextScale` (`:33`), `SetTextScale` (`:50`), `SetRemFollowsSystemTextScale` (`:71`), `frameTextScale` (`:104`), `frameRemSize` (`:129`); `gpui/selection_colors.go` — `SelectionColors` (`:31`), the cached read (`:62`), `blendWithWhite` (`:97`).
- `base/input_blink_cursor.go` — `blink` reads the system rate each tick (Chromium re-reads on timer start; Windows posts no change notification), and a 0/"never" rate keeps the caret visible with no timer.
- `base/system_selection.go` (new) — the base seam over `App.SelectionColorsForPaint`.
- `base/input_state_style.go` + `input_state_render.go` — unset `Selection` resolves from the system (was palette accent @ 0.4 — D0 divergence, noted at the site).
- `base/selectable_text.go` — the default wash is the system's; `SelectionColor` stays the override.

### Tests

- `gpui/text_scale_test.go` — default-on, pin reflow (probe 32→40 px), opt-out holds 16, pin/clear, hostless no-host-call property, `SelectionColors` on the default record, `blendWithWhite` arithmetic.
- `gpui/text_scale_windows_test.go` — the acceptance, live: injected snapshot + `WM_SETTINGCHANGE` flips the scale, the parked off-screen window reflows 32→40 px and back to 32 with the opt-out; 100 frames = 1 derivation, the flip = +1 (the counter seam); the selection colours flip with `WM_SYSCOLORCHANGE`, one derivation per record version.
- `base/input_blink_cursor_test.go` — the system rate (250/900 ms), 'never' (steady, 0 notifications), rate turning 'never' mid-blink (injected through the seam).
- `base/system_selection_test.go` + `input_state_style_test.go` — the SelectableText quad paints the system wash (injected and default), the explicit override wins, the resolved input selection is the system wash and stays translucent.

### Chromium citations (pinned checkout @ 0d4dcd4f)

- Text scale: `core/exported/web_settings_impl.cc:158-161` (`SetAccessibilityFontScaleFactor`, "TODO: Rename this OSTextScaleFactor"); the Windows feeder is the `Accessibility!TextScaleFactor` registry value ticket 27 reads.
- Caret blink: `ui/native_theme/os_settings_provider_win.cc:147-160` (GetCaretBlinkTime: 0 → default, INFINITE → steady) with the 500 ms default at `os_settings_provider.h:57`; the consumer `core/editing/frame_caret.cc:176-193` (StartBlinkCaret: zero interval → no repeating timer, rate read on start; "Windows does not seem to have any way to monitor changes" at `os_settings_provider_win.cc:148-153`).
- Selection: `core/highlight/highlight_style_utils.cc:188-201` (the ::selection UA default background = ActiveSelectionBackgroundColor), `core/layout/layout_theme.cc:365-371` (BlendWithWhite) and `:725-728` (kHighlight/kHighlighttext), `platform/graphics/color.cc:69-80, 1303-1330` (the blend), `public/common/renderer_preferences/renderer_preferences.h:26-27` (Chrome's default 0xFF1967D2/white).
- The repaint-on-message: `core/layout/layout_theme.cc:615-618`.

### Limitations

- Rich-text `TextView` selections keep the theme-derived wash (`TextViewStyle.Selection`): TextViewStyle is the app-authored `::selection` rule; the system default applies where nothing was authored (inputs, SelectableText). Flipping the TextView default belongs with the core selection model (19/20).
- Selected text keeps its own colour under the translucent wash; painting HighlightText over an opaque selection (Blink's `SupportsSelectionForegroundColors` arm) has no spelling yet — also 19/20.
- A caret stopped by 'never' restarts on the next focus/caret move (Chromium's timer-restart rule), not on a rate change alone.
- `TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank` (ticket 47) fails under this machine's load both with and without this branch's render.go change (vblank pacing, 5/40 gaps) — pre-existing, not investigated per instructions.
