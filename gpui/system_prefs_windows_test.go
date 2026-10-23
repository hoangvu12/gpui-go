//go:build windows

package gpui

import (
	"testing"
	"time"
	"unsafe"
)

// utf16FromString appends the NUL terminator, as the Win32 string
// parameters need.
func utf16FromString(s string) []uint16 {
	u := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		u = append(u, uint16(r))
	}
	return append(u, 0)
}

// ---------------------------------------------------------------------------
// The derivation (pure, injectable source)
// ---------------------------------------------------------------------------

// TestContrastPreferenceFollowsChromiumRuleForHighContrastPalettes checks
// the acceptance rule: prefers-contrast is derived from the forced
// palette's contrast ratio between the Window and WindowText system
// colours — 7:1 or more matches "more", 2.5:1 or less "less", anything
// between "custom", and without forced colours there is no preference
// (Chromium ui/native_theme/os_settings_provider.cc:179-220, the colours
// from os_settings_provider_win.cc:270-286).
func TestContrastPreferenceFollowsChromiumRuleForHighContrastPalettes(t *testing.T) {
	highContrast := defaultSystemSettingsSnapshot()
	highContrast.highContrast = true

	// Black on white: ratio 21:1 -> more.
	more := highContrast
	if prefs := deriveSystemPreferences(more); prefs.Contrast != ContrastMore {
		t.Errorf("black-on-white forced palette contrast = %v, want ContrastMore", prefs.Contrast)
	}

	// White on white: ratio 1:1 -> less.
	less := highContrast
	less.sysColors.WindowText = 0xFFFFFF
	if prefs := deriveSystemPreferences(less); prefs.Contrast != ContrastLess {
		t.Errorf("white-on-white forced palette contrast = %v, want ContrastLess", prefs.Contrast)
	}

	// 0x777777 on white: ratio about 4.5:1 -> custom.
	custom := highContrast
	custom.sysColors.WindowText = 0x777777
	if prefs := deriveSystemPreferences(custom); prefs.Contrast != ContrastCustom {
		t.Errorf("mid-grey forced palette contrast = %v, want ContrastCustom", prefs.Contrast)
	}

	// No forced colours: no preference (os_settings_provider.cc:219-220).
	plain := defaultSystemSettingsSnapshot()
	if prefs := deriveSystemPreferences(plain); prefs.Contrast != ContrastNoPreference {
		t.Errorf("unforced contrast = %v, want ContrastNoPreference", prefs.Contrast)
	}
}

// TestCaretBlinkIntervalMapsGetCaretBlinkTime checks the caret blink
// mapping: GetCaretBlinkTime's 0 means "use the default" (500 ms),
// INFINITE means a steady caret (zero interval), anything else is the
// blink period (Chromium ui/native_theme/os_settings_provider_win.cc
// CaretBlinkInterval:147-160; the default is
// os_settings_provider.h:57).
func TestCaretBlinkIntervalMapsGetCaretBlinkTime(t *testing.T) {
	cases := []struct {
		ms   uint32
		want time.Duration
	}{
		{0, 500 * time.Millisecond},
		{0xFFFFFFFF, 0},
		{530, 530 * time.Millisecond},
		{1, time.Millisecond},
	}
	for _, c := range cases {
		if got := caretBlinkInterval(c.ms); got != c.want {
			t.Errorf("caretBlinkInterval(%d) = %v, want %v", c.ms, got, c.want)
		}
	}
}

// TestReducedMotionFollowsClientAreaAnimation checks the reduced-motion
// derivation: SPI_GETCLIENTAREAANIMATION off asks for reduced motion, and
// a failed query assumes animations are enabled (Chromium
// ui/gfx/animation/animation_win.cc UpdatePrefersReducedMotion:36-47 —
// "we default to assuming that animations are enabled").
func TestReducedMotionFollowsClientAreaAnimation(t *testing.T) {
	on := defaultSystemSettingsSnapshot()
	if prefs := deriveSystemPreferences(on); prefs.ReducedMotion {
		t.Error("animations on derived a reduced-motion preference")
	}

	off := defaultSystemSettingsSnapshot()
	off.animationsEnabled = false
	off.animationsKnown = true
	if prefs := deriveSystemPreferences(off); !prefs.ReducedMotion {
		t.Error("animations off did not derive reduced motion")
	}

	// A failed query (unknown) leaves the default: animations enabled,
	// whatever the out-parameter held.
	unknown := defaultSystemSettingsSnapshot()
	unknown.animationsEnabled = false
	unknown.animationsKnown = false
	if prefs := deriveSystemPreferences(unknown); prefs.ReducedMotion {
		t.Error("an unknown query must assume animations are enabled")
	}
}

// TestTextScaleMapsTheAccessibilityRegistryPercent checks the text-scale
// mapping: the TextScaleFactor registry value is a percent (100 = 1.0),
// and a missing or zero value stays 1.0.
func TestTextScaleMapsTheAccessibilityRegistryPercent(t *testing.T) {
	cases := []struct {
		percent uint32
		want    float32
	}{
		{100, 1.0},
		{125, 1.25},
		{225, 2.25},
		{0, 1.0},
	}
	for _, c := range cases {
		snap := defaultSystemSettingsSnapshot()
		snap.textScalePercent = c.percent
		if got := deriveSystemPreferences(snap).TextScale; got != c.want {
			t.Errorf("text scale from %d%% = %v, want %v", c.percent, got, c.want)
		}
	}
}

// TestSystemColorsUseTheOSPaletteWhenForcedOrLight checks the CSS system
// colour mapping: with forced colours or a light scheme the CSS names map
// onto the OS palette (Chromium ui/color/win/css_system_color_mixer_win.cc
// MapNativeColorsToCssSystemColors:19-34 — Window/WindowText/BtnFace/
// BtnText/GrayText/Highlight/HighlightText/Hotlight, Field=Window,
// FieldText=WindowText, and the link colours = Hotlight; ButtonBorder
// follows ButtonText per blink's SystemColorFromColorProvider
// third_party/blink/renderer/core/layout/layout_theme.cc:800-808).
func TestSystemColorsUseTheOSPaletteWhenForcedOrLight(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	snap.sysColors = systemColorSet{
		BtnFace: 0xF0F0F0, BtnHighlight: 0xFFFFFF, BtnShadow: 0xA0A0A0, BtnText: 0x101010,
		GrayText: 0x6D6D6D, Highlight: 0x1B69D8, HighlightText: 0x0FFEE1,
		Hotlight: 0x0066CC, MenuHilight: 0x3399FF, Scrollbar: 0xC8C8C8,
		Window: 0x112233, WindowText: 0x443322,
	}
	for name, forced := range map[string]bool{"light": false, "forced": true} {
		s := snap
		s.highContrast = forced
		colors := deriveSystemColors(s)
		checks := []struct {
			name string
			got  Hsla
			want Hsla
		}{
			{"Canvas", colors.Canvas, Rgb(0x112233)},
			{"CanvasText", colors.CanvasText, Rgb(0x443322)},
			{"ButtonFace", colors.ButtonFace, Rgb(0xF0F0F0)},
			{"ButtonText", colors.ButtonText, Rgb(0x101010)},
			{"ButtonBorder", colors.ButtonBorder, Rgb(0x101010)},
			{"Field", colors.Field, Rgb(0x112233)},
			{"FieldText", colors.FieldText, Rgb(0x443322)},
			{"GrayText", colors.GrayText, Rgb(0x6D6D6D)},
			{"Highlight", colors.Highlight, Rgb(0x1B69D8)},
			{"HighlightText", colors.HighlightText, Rgb(0x0FFEE1)},
			{"LinkText", colors.LinkText, Rgb(0x0066CC)},
			{"VisitedText", colors.VisitedText, Rgb(0x0066CC)},
			{"ActiveText", colors.ActiveText, Rgb(0x0066CC)},
			{"SelectedItem", colors.SelectedItem, Rgb(0x1B69D8)},
			{"SelectedItemText", colors.SelectedItemText, Rgb(0x0FFEE1)},
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("%s: %s = %v, want the OS palette colour %v", name, c.name, c.got, c.want)
			}
		}
	}

	// Mark and MarkText are constants in every mode
	// (layout_theme.cc:733-737).
	if colors := deriveSystemColors(snap); colors.Mark != Rgb(0xFFFF00) || colors.MarkText != Rgb(0x000000) {
		t.Errorf("Mark/MarkText = %v/%v, want yellow on black", colors.Mark, colors.MarkText)
	}
}

// TestSystemColorsDarkModeUsesTheBrowserDefaults checks the dark-scheme
// palette: Windows' native colours are only used in light and forced
// modes, so a dark scheme keeps the browser defaults (Chromium
// ui/color/color_provider_utils.cc CompleteDefaultCssSystemColorDefinition
// and blink layout_theme.cc DefaultSystemColor:649-754).
func TestSystemColorsDarkModeUsesTheBrowserDefaults(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	snap.appearance = WindowAppearanceDark
	colors := deriveSystemColors(snap)
	checks := []struct {
		name string
		got  Hsla
		want Hsla
	}{
		{"Canvas", colors.Canvas, Rgb(0x121212)},
		{"CanvasText", colors.CanvasText, Rgb(0xFFFFFF)},
		{"ButtonFace", colors.ButtonFace, Rgb(0x6B6B6B)},
		{"ButtonText", colors.ButtonText, Rgb(0xFFFFFF)},
		{"ButtonBorder", colors.ButtonBorder, Rgb(0xFFFFFF)},
		{"Field", colors.Field, Rgb(0x3B3B3B)},
		{"FieldText", colors.FieldText, Rgb(0xFFFFFF)},
		{"GrayText", colors.GrayText, Rgb(0x808080)},
		{"LinkText", colors.LinkText, Rgb(0x9E9EFF)},
		{"VisitedText", colors.VisitedText, Rgb(0xD0ADF0)},
		{"ActiveText", colors.ActiveText, Rgb(0xFF0000)},
		{"Highlight", colors.Highlight, Rgb(0x1967D2)},
		{"HighlightText", colors.HighlightText, Rgb(0xFFFFFF)},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("dark %s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestAccentColorDefaultAndTextContrast checks the accent colour
// derivation: a missing OS accent keeps Chromium's default 0x0075FF, and
// AccentColorText is white on a dark accent and black on a light one (the
// Firefox rule blink documents at layout_theme.cc:973-980).
func TestAccentColorDefaultAndTextContrast(t *testing.T) {
	absent := defaultSystemSettingsSnapshot()
	if prefs := deriveSystemPreferences(absent); prefs.AccentColor != Rgb(0x0075FF) {
		t.Errorf("default accent = %v, want Chromium's 0x0075FF", prefs.AccentColor)
	}

	dark := defaultSystemSettingsSnapshot()
	dark.accentKnown = true
	dark.accentColor = 0x0A2E52
	prefs := deriveSystemPreferences(dark)
	if prefs.AccentColor != Rgb(0x0A2E52) {
		t.Errorf("accent = %v, want the OS colour", prefs.AccentColor)
	}
	if colors := deriveSystemColors(dark); colors.AccentColorText != White() {
		t.Error("a dark accent must pair with white text")
	}

	light := dark
	light.accentColor = 0xFFFFFF
	if colors := deriveSystemColors(light); colors.AccentColorText != Rgb(0x000000) {
		t.Error("a light accent must pair with black text")
	}
}

// ---------------------------------------------------------------------------
// The message routing (real offscreen window, injected source)
// ---------------------------------------------------------------------------

// TestSystemPreferencesRefreshOnSettingChangeMessages is the ticket's
// main acceptance: simulated WM_SETTINGCHANGE (both the parameter-action
// and the area-string spellings), WM_SYSCOLORCHANGE, WM_THEMECHANGED and
// the DWM colourization change each refresh the record through the real
// window's message path and fire the change event, while an unchanged
// reading fires nothing.
func TestSystemPreferencesRefreshOnSettingChangeMessages(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	snap.animationsEnabled = true
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	events := make(chan SystemPreferences, 8)
	h.OnSystemPreferencesChanged(func(p SystemPreferences, _ SystemColors) { events <- p })

	handle, err := h.OpenWindow(WindowOptions{
		Title:    "gpui system prefs observer",
		Bounds:   Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 240, Height: 160}},
		Show:     true,
		Activate: false,
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}

	if initial := h.SystemPreferences(); initial.ReducedMotion || initial.TextScale != 1.0 {
		t.Fatalf("the seeded record = %+v, want the injected default", initial)
	}

	// WM_SETTINGCHANGE with the high-contrast action (Chromium's trigger,
	// os_settings_provider_win.cc:293-298): the record follows the source.
	snap.animationsEnabled = false
	snap.animationsKnown = true
	snap.textScalePercent = 150
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), uintptr(spiSetHighContrast), 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	select {
	case p := <-events:
		if !p.ReducedMotion || p.TextScale != 1.5 {
			t.Fatalf("record after WM_SETTINGCHANGE = %+v, want reduced motion at 1.5x", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the system-preferences observer did not fire on WM_SETTINGCHANGE")
	}
	if current := h.SystemPreferences(); !current.ReducedMotion || current.TextScale != 1.5 {
		t.Fatalf("cached record = %+v, want the refreshed reading", current)
	}

	// The area-string spelling (the ImmersiveColorSet theme report): the
	// record's colour scheme follows the source even when the window's
	// own appearance does not change.
	snap.appearance = WindowAppearanceDark
	immersive := utf16FromString("ImmersiveColorSet")
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), 0,
		uintptr(unsafe.Pointer(&immersive[0]))); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE(ImmersiveColorSet) result = %d, want 0", r)
	}
	select {
	case p := <-events:
		if p.ColorScheme != WindowAppearanceDark {
			t.Fatalf("colour scheme after ImmersiveColorSet = %v, want dark", p.ColorScheme)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the observer did not fire on the ImmersiveColorSet report")
	}

	// WM_SYSCOLORCHANGE refreshes the palette
	// (os_settings_provider_win.cc:288-292). Back to a light scheme first
	// so the palette actually feeds the record.
	snap.appearance = WindowAppearanceLight
	immersive = utf16FromString("ImmersiveColorSet")
	procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), 0,
		uintptr(unsafe.Pointer(&immersive[0])))
	select {
	case <-events:
	case <-time.After(10 * time.Second):
		t.Fatal("the observer did not fire on the return to light")
	}
	snap.sysColors.Window = 0x123456
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSysColorChange), 0, 0); r != 0 {
		t.Fatalf("WM_SYSCOLORCHANGE result = %d, want 0 (consumed)", r)
	}
	select {
	case <-events:
	case <-time.After(10 * time.Second):
		t.Fatal("the observer did not fire on WM_SYSCOLORCHANGE")
	}
	if colors := h.SystemColors(); colors.Canvas != Rgb(0x123456) {
		t.Errorf("Canvas after WM_SYSCOLORCHANGE = %v, want the new window colour", colors.Canvas)
	}

	// WM_THEMECHANGED and the DWM colourization change refresh too.
	snap.accentKnown = true
	snap.accentColor = 0x0A2E52
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmThemeChanged), 0, 0); r != 0 {
		t.Fatalf("WM_THEMECHANGED result = %d, want 0 (consumed)", r)
	}
	select {
	case p := <-events:
		if p.AccentColor != Rgb(0x0A2E52) {
			t.Fatalf("accent after WM_THEMECHANGED = %v, want the new colour", p.AccentColor)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the observer did not fire on WM_THEMECHANGED")
	}
	snap.accentColor = 0x1B69D8
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmDwmColorizationColorChanged), 0, 0); r != 0 {
		t.Fatalf("WM_DWMCOLORIZATIONCOLORCHANGED result = %d, want 0 (consumed)", r)
	}
	select {
	case p := <-events:
		if p.AccentColor != Rgb(0x1B69D8) {
			t.Fatalf("accent after the DWM colour change = %v, want the new colour", p.AccentColor)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the observer did not fire on the DWM colourization change")
	}

	// An unchanged reading notifies nobody
	// (os_settings_provider_win.cc:293-300 compares before notifying).
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), uintptr(spiSetHighContrast), 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE (no change) result = %d, want 0", r)
	}
	select {
	case p := <-events:
		t.Fatalf("an unchanged reading must not notify, got %+v", p)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestSystemPreferencesSysColorChangeForcesNotifyInForcedColors checks
// Chromium's forced-notification rule: WM_SYSCOLORCHANGE in a
// high-contrast session notifies even when the derived record did not
// change (os_settings_provider_win.cc:288-292, the force_notify flag).
func TestSystemPreferencesSysColorChangeForcesNotifyInForcedColors(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	snap.highContrast = true
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	events := make(chan SystemPreferences, 4)
	h.OnSystemPreferencesChanged(func(p SystemPreferences, _ SystemColors) { events <- p })

	handle, err := h.OpenWindow(WindowOptions{
		Title:    "gpui system prefs forced notify",
		Bounds:   Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 240, Height: 160}},
		Show:     true,
		Activate: false,
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}

	// The first WM_SYSCOLORCHANGE reports the (unchanged) forced record.
	force := func() {
		if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSysColorChange), 0, 0); r != 0 {
			t.Fatalf("WM_SYSCOLORCHANGE result = %d, want 0 (consumed)", r)
		}
	}
	force()
	select {
	case <-events:
	case <-time.After(10 * time.Second):
		t.Fatal("WM_SYSCOLORCHANGE did not notify in forced colours")
	}
	// The second one, with nothing changed, still notifies.
	force()
	select {
	case <-events:
	case <-time.After(10 * time.Second):
		t.Fatal("WM_SYSCOLORCHANGE must force the notification in forced colours")
	}
}

// TestSystemPreferencesAppObserverRouting checks the app-level surface:
// the observer wraps the host report in an application update, and
// App.SystemPreferences answers the refreshed record.
func TestSystemPreferencesAppObserverRouting(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	events := make(chan SystemPreferences, 4)
	if err := app.OnSystemPreferencesChanged(func(p SystemPreferences, _ SystemColors) { events <- p }); err != nil {
		t.Fatalf("OnSystemPreferencesChanged: %v", err)
	}

	handle, err := h.OpenWindow(WindowOptions{
		Title:    "gpui system prefs app",
		Bounds:   Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 240, Height: 160}},
		Show:     true,
		Activate: false,
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	if initial := app.SystemPreferences(); initial.ForcedColors {
		t.Fatalf("initial record = %+v, want the injected default", initial)
	}

	snap.highContrast = true
	snap.sysColors.Window = 0x000000
	snap.sysColors.WindowText = 0xFFFFFF
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), uintptr(spiSetHighContrast), 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0", r)
	}
	select {
	case p := <-events:
		if !p.ForcedColors || p.Contrast != ContrastMore {
			t.Fatalf("app record after the change = %+v, want forced colours at more contrast", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the app observer did not fire")
	}
	if current := app.SystemPreferences(); !current.ForcedColors {
		t.Fatalf("App.SystemPreferences after the change = %+v, want forced colours", current)
	}
	if colors := app.SystemColors(); colors.Canvas != Rgb(0x000000) || colors.CanvasText != Rgb(0xFFFFFF) {
		t.Errorf("App.SystemColors = Canvas %v CanvasText %v, want the forced palette", colors.Canvas, colors.CanvasText)
	}
}

// TestAppWithoutHostAnswersDefaultSystemPreferences checks the test-app
// path: without a host the record is the documented default (light,
// animations on, no forced colours, the 500 ms caret blink, 1.0 text
// scale).
func TestAppWithoutHostAnswersDefaultSystemPreferences(t *testing.T) {
	app := NewApp()
	prefs := app.SystemPreferences()
	if prefs.ColorScheme != WindowAppearanceLight || prefs.ReducedMotion || prefs.ForcedColors ||
		prefs.Contrast != ContrastNoPreference || prefs.ReducedTransparency || prefs.InvertedColors ||
		prefs.TextScale != 1.0 || prefs.CaretBlinkInterval != 500*time.Millisecond {
		t.Fatalf("default record = %+v", prefs)
	}
	colors := app.SystemColors()
	if colors.Canvas != Rgb(0xFFFFFF) || colors.CanvasText != Rgb(0x000000) {
		t.Errorf("default colours = Canvas %v CanvasText %v, want the classic light palette", colors.Canvas, colors.CanvasText)
	}
}

// ---------------------------------------------------------------------------
// The real Windows reads (read-only)
// ---------------------------------------------------------------------------

// TestReadSystemSettingsSnapshotReadsThisMachine exercises the real
// provider read-only (like TestRegistrySystemAppearance): every value
// answers and derives without touching the user's settings.
func TestReadSystemSettingsSnapshotReadsThisMachine(t *testing.T) {
	snap := readSystemSettingsSnapshot()
	prefs := deriveSystemPreferences(snap)
	colors := deriveSystemColors(snap)
	t.Logf("snapshot: %+v", snap)
	t.Logf("preferences: %+v", prefs)
	t.Logf("colors: %+v", colors)
	if prefs.ColorScheme != WindowAppearanceLight && prefs.ColorScheme != WindowAppearanceDark {
		t.Errorf("colour scheme = %v", prefs.ColorScheme)
	}
	if prefs.TextScale < 1.0 {
		t.Errorf("text scale = %v, want at least 1.0", prefs.TextScale)
	}
	if prefs.CaretBlinkInterval < 0 {
		t.Errorf("caret blink interval = %v", prefs.CaretBlinkInterval)
	}
	if prefs.Contrast != ContrastNoPreference && prefs.Contrast != ContrastMore &&
		prefs.Contrast != ContrastLess && prefs.Contrast != ContrastCustom {
		t.Errorf("contrast = %v", prefs.Contrast)
	}
}
