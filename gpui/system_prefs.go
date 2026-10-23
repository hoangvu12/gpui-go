package gpui

import (
	"fmt"
	"math"
	"time"
)

// System preferences (browser-core ticket 27).
//
// The browser treats the OS accessibility and theme settings as platform
// data apps read through one record, refreshed when the OS changes them
// (the spec's Platform module; research/browser-parity/06 §4). Chromium's
// Windows rules are the reference:
//   - forced colours: SPI_GETHIGHCONTRAST & HCF_HIGHCONTRASTON, refreshed
//     on WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST)
//     (ui/native_theme/os_settings_provider_win.cc:30-37, 293-298);
//   - prefers-contrast derived from the forced palette's Window vs
//     WindowText ratio (ui/native_theme/os_settings_provider.cc:179-220);
//   - reduced transparency, inverted colours and overlay scrollbars from
//     the registry (os_settings_provider_win.cc:205-260);
//   - the accent colour (ui/color/win/accent_color_observer.cc);
//   - the caret blink period (os_settings_provider_win.cc:147-160);
//   - reduced motion from SPI_GETCLIENTAREAANIMATION, refreshed on
//     WM_SETTINGCHANGE (ui/gfx/animation/animation_win.cc:36-47).
//
// The Windows reads live in system_prefs_windows.go; everything here is
// the platform-neutral record, the derivation from one raw reading, and
// the App surface. The derivation is pure so tests inject the reading
// instead of this machine's settings.

// ---------------------------------------------------------------------------
// The preference record
// ---------------------------------------------------------------------------

// ContrastPreference is the prefers-contrast value the browser derives
// (CSS Media Queries 5; Chromium's NativeTheme::PreferredContrast).
type ContrastPreference uint8

const (
	// ContrastNoPreference is the default: no forced colours, or the
	// forced palette's contrast could not be determined.
	ContrastNoPreference ContrastPreference = iota
	// ContrastMore matches prefers-contrast: more — a forced palette at
	// 7:1 or higher contrast.
	ContrastMore
	// ContrastLess matches prefers-contrast: less — 2.5:1 or lower.
	ContrastLess
	// ContrastCustom matches prefers-contrast: custom — a forced palette
	// between the two thresholds.
	ContrastCustom
)

// String returns the CSS media-feature spelling.
func (c ContrastPreference) String() string {
	switch c {
	case ContrastMore:
		return "more"
	case ContrastLess:
		return "less"
	case ContrastCustom:
		return "custom"
	default:
		return "no-preference"
	}
}

// SystemPreferences is the OS settings record apps read. It is derived
// from one raw OS reading (systemSettingsSnapshot) by
// deriveSystemPreferences, cached on the host, and refreshed on Windows
// settings messages; every change fires the system-preferences observer
// with the new record.
type SystemPreferences struct {
	// ColorScheme is the light/dark scheme (the registry's
	// AppsUseLightTheme, the same source as WindowAppearance). Under
	// forced colours Chromium additionally derives the preferred scheme
	// from the Window colour's LAB lightness
	// (os_settings_provider.cc:120-152); that refinement belongs to the
	// forced-colours ticket.
	ColorScheme WindowAppearance
	// ReducedMotion reports the Windows "Animation effects" switch off
	// state (SPI_GETCLIENTAREAANIMATION): animations disabled asks for
	// reduced motion. A failed query assumes animations are enabled.
	ReducedMotion bool
	// ForcedColors reports an active Windows high-contrast theme
	// (SPI_GETHIGHCONTRAST & HCF_HIGHCONTRASTON).
	ForcedColors bool
	// Contrast is the derived prefers-contrast value.
	Contrast ContrastPreference
	// ReducedTransparency reports the "Transparency effects" switch off
	// state (Personalize!EnableTransparency == 0).
	ReducedTransparency bool
	// InvertedColors reports the colour filter set to "inverted"
	// (ColorFiltering!Active == 1 && FilterType == 1).
	InvertedColors bool
	// OverlayScrollbars reports the "Automatically hide scrollbars"
	// preference on (Control Panel\Accessibility!DynamicScrollbars != 0).
	OverlayScrollbars bool
	// AccentColor is the Windows accent colour, opaque. Without an OS
	// accent it is Chromium's default 0x0075FF
	// (blink layout_theme.cc:961-972).
	AccentColor Hsla
	// CaretBlinkInterval is the caret blink period; zero means a steady
	// (non-blinking) caret. The OS "use the default" spelling (a
	// GetCaretBlinkTime of zero) maps to the 500 ms default
	// (os_settings_provider.h:57).
	CaretBlinkInterval time.Duration
	// TextScale is the Windows text-size setting (the
	// Accessibility!TextScaleFactor percent over 100; 1.0 = 100%).
	TextScale float32
}

// SystemColors is the CSS Color 4 system colour set (Canvas, CanvasText,
// …), as the browser resolves them on Windows.
type SystemColors struct {
	// Canvas is CSS Canvas (Chromium's kColorCssSystemWindow).
	Canvas Hsla
	// CanvasText is CSS CanvasText (kColorCssSystemWindowText).
	CanvasText Hsla
	// LinkText is CSS LinkText (kColorCssSystemHotlight).
	LinkText Hsla
	// VisitedText is CSS VisitedText (Hotlight).
	VisitedText Hsla
	// ActiveText is CSS ActiveText (Hotlight).
	ActiveText Hsla
	// ButtonFace is CSS ButtonFace (COLOR_BTNFACE).
	ButtonFace Hsla
	// ButtonText is CSS ButtonText (COLOR_BTNTEXT).
	ButtonText Hsla
	// ButtonBorder is CSS ButtonBorder; Blink maps it onto the
	// ButtonText colour (layout_theme.cc:800-808 groups the two).
	ButtonBorder Hsla
	// Field is CSS Field (the Window colour).
	Field Hsla
	// FieldText is CSS FieldText (the WindowText colour).
	FieldText Hsla
	// Highlight is CSS Highlight (COLOR_HIGHLIGHT).
	Highlight Hsla
	// HighlightText is CSS HighlightText (COLOR_HIGHLIGHTTEXT).
	HighlightText Hsla
	// SelectedItem is CSS SelectedItem (the Highlight colour).
	SelectedItem Hsla
	// SelectedItemText is CSS SelectedItemText (the HighlightText
	// colour).
	SelectedItemText Hsla
	// Mark is CSS Mark (yellow).
	Mark Hsla
	// MarkText is CSS MarkText (black).
	MarkText Hsla
	// GrayText is CSS GrayText (COLOR_GRAYTEXT).
	GrayText Hsla
	// AccentColor is the resolved accent colour.
	AccentColor Hsla
	// AccentColorText is the accent colour's readable text colour:
	// white on a dark accent, black on a light one (the Firefox rule
	// blink documents, layout_theme.cc:973-980).
	AccentColorText Hsla
}

// ---------------------------------------------------------------------------
// One raw OS reading (the injectable source)
// ---------------------------------------------------------------------------

// systemColorSet is the set of Win32 system colours Chromium reads
// (ui/color/win/native_color_mixers_win.cc GetCurrentSysColors): the
// values are 0xRRGGBBAA, already converted from COLORREF's 0x00BBGGRR.
type systemColorSet struct {
	BtnFace       uint32
	BtnHighlight  uint32
	BtnShadow     uint32
	BtnText       uint32
	GrayText      uint32
	Highlight     uint32
	HighlightText uint32
	Hotlight      uint32
	MenuHilight   uint32
	Scrollbar     uint32
	Window        uint32
	WindowText    uint32
}

// systemSettingsSnapshot is one raw reading of every OS source the
// record derives from. The Windows reader fills it with the real Win32
// answers; tests replace the provider and answer snapshots instead.
type systemSettingsSnapshot struct {
	// appearance is the light/dark registry answer.
	appearance WindowAppearance
	// animationsEnabled is SPI_GETCLIENTAREAANIMATION's BOOL; it only
	// counts when animationsKnown (a failed query defaults to enabled,
	// animation_win.cc:41-47).
	animationsEnabled bool
	animationsKnown   bool
	// highContrast is SPI_GETHIGHCONTRAST & HCF_HIGHCONTRASTON.
	highContrast bool
	// sysColors are the GetSysColor values.
	sysColors systemColorSet
	// transparencyEnabled is Personalize!EnableTransparency != 0
	// (missing counts as enabled, os_settings_provider_win.cc:205-214).
	transparencyEnabled bool
	// colorFilterInverted is ColorFiltering Active == 1 && FilterType ==
	// 1 (os_settings_provider_win.cc:217-231).
	colorFilterInverted bool
	// dynamicScrollbars is Control Panel\Accessibility!DynamicScrollbars
	// != 0 (missing counts as on, os_settings_provider_win.cc:254-260).
	dynamicScrollbars bool
	// accentColor is the DWM AccentColor value as 0xRRGGBB, valid only
	// when accentKnown (Chromium's registry fallback,
	// accent_color_observer.cc:126-131).
	accentColor uint32
	accentKnown bool
	// caretBlinkMs is GetCaretBlinkTime: 0 = use the default, INFINITE
	// = a steady caret (os_settings_provider_win.cc:147-160).
	caretBlinkMs uint32
	// textScalePercent is Accessibility!TextScaleFactor (100 = 1.0).
	textScalePercent uint32
}

// defaultSystemSettingsSnapshot is the documented default reading: a
// light, un-customized Windows with animations, transparency and
// overlay scrollbars on, the 500 ms caret blink and 100% text scale,
// and the classic light system palette. Test applications (no host)
// answer records derived from it.
func defaultSystemSettingsSnapshot() systemSettingsSnapshot {
	return systemSettingsSnapshot{
		appearance:          WindowAppearanceLight,
		animationsEnabled:   true,
		transparencyEnabled: true,
		dynamicScrollbars:   true,
		caretBlinkMs:        0,
		textScalePercent:    100,
		sysColors: systemColorSet{
			// The classic light theme values (the Win10/11 defaults).
			BtnFace: 0xF0F0F0, BtnHighlight: 0xFFFFFF, BtnShadow: 0xA0A0A0, BtnText: 0x000000,
			GrayText: 0x6D6D6D, Highlight: 0x0078D7, HighlightText: 0xFFFFFF,
			Hotlight: 0x0066CC, MenuHilight: 0x3399FF, Scrollbar: 0xC8C8C8,
			Window: 0xFFFFFF, WindowText: 0x000000,
		},
	}
}

// defaultSystemPreferences is the record of the default snapshot.
func defaultSystemPreferences() SystemPreferences {
	return deriveSystemPreferences(defaultSystemSettingsSnapshot())
}

// defaultSystemColors is the colour record of the default snapshot.
func defaultSystemColors() SystemColors {
	return deriveSystemColors(defaultSystemSettingsSnapshot())
}

// ---------------------------------------------------------------------------
// The derivation (pure)
// ---------------------------------------------------------------------------

// caretBlinkInterval maps GetCaretBlinkTime's answer: 0 uses the 500 ms
// default, INFINITE (0xFFFFFFFF) means a steady caret (zero interval),
// anything else is the period in milliseconds
// (os_settings_provider_win.cc:147-160; the default at
// os_settings_provider.h:57).
func caretBlinkInterval(ms uint32) time.Duration {
	const infinite = uint32(0xFFFFFFFF)
	switch ms {
	case 0:
		return 500 * time.Millisecond
	case infinite:
		return 0
	default:
		return time.Duration(ms) * time.Millisecond
	}
}

// linearize is the sRGB transfer function Chromium's Linearize applies
// before the relative luminance (ui/gfx/color_utils.cc:434-438; the
// 0.03928/12.92 threshold discussion is in its comment). The port uses
// float64 where Chromium uses float32; the contrast thresholds (7 and
// 2.5) are far from the precision boundary.
func linearize(component float64) float64 {
	if component <= 0.04045 {
		return component / 12.92
	}
	return math.Pow((component+0.055)/1.055, 2.4)
}

// relativeLuminance is WCAG's relative luminance of an 0xRRGGBB colour
// (ui/gfx/color_utils.cc GetRelativeLuminance4f:424-427).
func relativeLuminance(hex uint32) float64 {
	r := float64((hex>>16)&0xFF) / 255.0
	g := float64((hex>>8)&0xFF) / 255.0
	b := float64(hex&0xFF) / 255.0
	return 0.2126*linearize(r) + 0.7152*linearize(g) + 0.0722*linearize(b)
}

// contrastRatio is (L1+0.05)/(L2+0.05), the lighter over the darker
// (ui/gfx/color_utils.cc GetContrastRatio:411-418).
func contrastRatio(a, b uint32) float64 {
	la := relativeLuminance(a) + 0.05
	lb := relativeLuminance(b) + 0.05
	if la > lb {
		return la / lb
	}
	return lb / la
}

// contrastPreference derives prefers-contrast from one forced palette:
// 7:1 or more matches "more", 2.5:1 or less "less", anything between
// "custom" (Chromium ui/native_theme/os_settings_provider.cc
// PreferredContrast:179-220 — the ratios are WCAG's, the colours are the
// Window and WindowText system colours).
func contrastPreference(window, windowText uint32) ContrastPreference {
	ratio := contrastRatio(window, windowText)
	if ratio >= 7 {
		return ContrastMore
	}
	if ratio <= 2.5 {
		return ContrastLess
	}
	return ContrastCustom
}

// deriveSystemPreferences derives the record from one raw reading.
func deriveSystemPreferences(s systemSettingsSnapshot) SystemPreferences {
	accent := uint32(0x0075FF) // kDefaultAccentColor, layout_theme.cc:961
	if s.accentKnown {
		accent = s.accentColor
	}
	textScale := float32(1.0)
	if s.textScalePercent > 0 {
		textScale = float32(s.textScalePercent) / 100.0
	}
	prefs := SystemPreferences{
		ColorScheme:         s.appearance,
		ReducedMotion:       s.animationsKnown && !s.animationsEnabled,
		ForcedColors:        s.highContrast,
		Contrast:            ContrastNoPreference,
		ReducedTransparency: !s.transparencyEnabled,
		InvertedColors:      s.colorFilterInverted,
		OverlayScrollbars:   s.dynamicScrollbars,
		AccentColor:         Rgb(accent),
		CaretBlinkInterval:  caretBlinkInterval(s.caretBlinkMs),
		TextScale:           textScale,
	}
	if s.highContrast {
		prefs.Contrast = contrastPreference(s.sysColors.Window, s.sysColors.WindowText)
	}
	return prefs
}

// deriveSystemColors derives the CSS system colours from one raw
// reading. Windows' native colours are only used with forced colours or
// a light scheme (ui/color/win/css_system_color_mixer_win.cc:19-21:
// "Windows' native colors don't get updated for dark mode"); a dark
// scheme keeps the browser's default dark palette
// (ui/color/color_provider_utils.cc
// CompleteDefaultCssSystemColorDefinition, matching blink
// layout_theme.cc DefaultSystemColor:649-754).
func deriveSystemColors(s systemSettingsSnapshot) SystemColors {
	accent := uint32(0x0075FF)
	if s.accentKnown {
		accent = s.accentColor
	}
	// AccentColorText: white on a dark accent, black on a light one.
	// Blink documents the Firefox rule (layout_theme.cc:973-980); its
	// literal comparison of the [0,1] luminance against 128 would always
	// answer white, so the port implements the documented intent with the
	// 0.5 midpoint.
	accentText := uint32(0x000000)
	if relativeLuminance(accent) <= 0.5 {
		accentText = 0xFFFFFF
	}

	if s.highContrast || !s.appearance.IsDark() {
		// The OS palette (css_system_color_mixer_win.cc:22-34, blink
		// layout_theme.cc:787-860 for the id choices; SelectedItem
		// follows Highlight per ui/color/win/native_color_mixers_win.cc's
		// high-contrast selection recipes).
		c := s.sysColors
		return SystemColors{
			Canvas:           Rgb(c.Window),
			CanvasText:       Rgb(c.WindowText),
			LinkText:         Rgb(c.Hotlight),
			VisitedText:      Rgb(c.Hotlight),
			ActiveText:       Rgb(c.Hotlight),
			ButtonFace:       Rgb(c.BtnFace),
			ButtonText:       Rgb(c.BtnText),
			ButtonBorder:     Rgb(c.BtnText),
			Field:            Rgb(c.Window),
			FieldText:        Rgb(c.WindowText),
			Highlight:        Rgb(c.Highlight),
			HighlightText:    Rgb(c.HighlightText),
			SelectedItem:     Rgb(c.Highlight),
			SelectedItemText: Rgb(c.HighlightText),
			Mark:             Rgb(0xFFFF00),
			MarkText:         Rgb(0x000000),
			GrayText:         Rgb(c.GrayText),
			AccentColor:      Rgb(accent),
			AccentColorText:  Rgb(accentText),
		}
	}
	return SystemColors{
		Canvas:           Rgb(0x121212),
		CanvasText:       Rgb(0xFFFFFF),
		LinkText:         Rgb(0x9E9EFF),
		VisitedText:      Rgb(0xD0ADF0),
		ActiveText:       Rgb(0xFF0000),
		ButtonFace:       Rgb(0x6B6B6B),
		ButtonText:       Rgb(0xFFFFFF),
		ButtonBorder:     Rgb(0xFFFFFF),
		Field:            Rgb(0x3B3B3B),
		FieldText:        Rgb(0xFFFFFF),
		Highlight:        Rgb(0x1967D2),
		HighlightText:    Rgb(0xFFFFFF),
		SelectedItem:     Rgb(0x1967D2),
		SelectedItemText: Rgb(0xFFFFFF),
		Mark:             Rgb(0xFFFF00),
		MarkText:         Rgb(0x000000),
		GrayText:         Rgb(0x808080),
		AccentColor:      Rgb(accent),
		AccentColorText:  Rgb(accentText),
	}
}

// ---------------------------------------------------------------------------
// The App surface
// ---------------------------------------------------------------------------

// SystemPreferences returns the current system-preference record. A real
// application answers the host's cached record, refreshed on Windows
// settings messages; a test application (no host) answers the documented
// default. Follow changes with OnSystemPreferencesChanged.
func (a *App) SystemPreferences() SystemPreferences {
	if a.host == nil {
		return defaultSystemPreferences()
	}
	return a.host.SystemPreferences()
}

// SystemColors returns the current system colours (the CSS Color 4 set
// as the browser resolves them on Windows).
func (a *App) SystemColors() SystemColors {
	if a.host == nil {
		return defaultSystemColors()
	}
	return a.host.SystemColors()
}

// OnSystemPreferencesChanged registers the system-preferences observer:
// it runs as an application update whenever a Windows settings message
// (WM_SETTINGCHANGE, WM_SYSCOLORCHANGE, WM_THEMECHANGED, the DWM
// colourization change) changed the record, with the new preference
// record and system colours. The registration replaces a previous one;
// it requires an attached host. This is the surface Base's one-shot
// reduced-motion read moves onto (browser-core ticket 74).
func (a *App) OnSystemPreferencesChanged(cb func(prefs SystemPreferences, colors SystemColors)) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	if cb == nil {
		return fmt.Errorf("gpui: OnSystemPreferencesChanged requires a non-nil callback")
	}
	h.OnSystemPreferencesChanged(func(prefs SystemPreferences, colors SystemColors) {
		a.Update(func(*App) { cb(prefs, colors) })
	})
	return nil
}
