//go:build windows

package gpui

import (
	"syscall"
	"unsafe"
)

// System preferences: the Windows half of browser-core ticket 27. The
// record types, the pure derivation and the App surface live in
// system_prefs.go. This file owns:
//   - the real Win32 reads (one systemSettingsSnapshot): the registry,
//     SystemParametersInfoW, GetSysColor and GetCaretBlinkTime, through
//     the pure-stdlib syscall style (CGO_ENABLED=0) the host uses;
//   - the injectable seam (systemSettingsSnapshotProvider) so tests
//     never depend on this machine's settings;
//   - the host's cached record, refreshed by the settings messages and
//     delivered to the system-preferences observer.
//
// Chromium's rules, cited at each read:
//   - light/dark, transparency: HKCU\...\Themes\Personalize
//     (ui/native_theme/os_settings_provider_win.cc:205-214 UpdateForThemesRegkey);
//   - inverted colours: HKCU\Software\Microsoft\ColorFiltering
//     (os_settings_provider_win.cc:217-231 UpdateForColorFilteringRegkey);
//   - overlay scrollbars: HKCU\Control Panel\Accessibility!DynamicScrollbars
//     (os_settings_provider_win.cc:254-260 UpdateForAccessibilityRegkey);
//   - the system colours: GetSysColor, the set of
//     ui/color/win/native_color_mixers_win.cc GetCurrentSysColors:32-46;
//   - the accent colour: HKCU\SOFTWARE\Microsoft\Windows\DWM!AccentColor,
//     the registry fallback of ui/color/win/accent_color_observer.cc
//     (the WinRT UISettings read it prefers is not ported to the
//     pure-stdlib host, exactly like the appearance read before it);
//   - forced colours: SPI_GETHIGHCONTRAST (os_settings_provider_win.cc:30-37);
//   - reduced motion: SPI_GETCLIENTAREAANIMATION
//     (ui/gfx/animation/animation_win.cc:36-47);
//   - the caret blink: GetCaretBlinkTime
//     (os_settings_provider_win.cc:147-160);
//   - the text scale: HKCU\Software\Microsoft\Accessibility!TextScaleFactor,
//     the registry value the WinRT UISettings.TextScaleFactor reads
//     (research/browser-parity/06 §10; blink's hook is
//     web_settings_impl.cc:158-161).
//
// Change observation: Chromium watches some of these keys with
// RegNotifyChangeKeyValue and the rest through the messages below. The
// port refreshes the whole record on the messages every window already
// receives — WM_SETTINGCHANGE (both spellings), WM_SYSCOLORCHANGE,
// WM_THEMECHANGED and WM_DWMCOLORIZATIONCOLORCHANGED — because the
// reads are cheap and the refresh is compare-gated; the registry-watch
// path for Personalize/ColorFiltering changes that broadcast nothing is
// a documented gap (see the ticket).

// ---------------------------------------------------------------------------
// Win32 constants and bindings
// ---------------------------------------------------------------------------

const (
	wmSysColorChange              = 0x0015 // WM_SYSCOLORCHANGE (winuser.h)
	wmThemeChanged                = 0x031A // WM_THEMECHANGED (winuser.h)
	wmDwmColorizationColorChanged = 0x0320 // WM_DWMCOLORIZATIONCOLORCHANGED (dwmapi.h)

	spiGetHighContrast   = 0x0042 // SPI_GETHIGHCONTRAST (winuser.h)
	spiSetHighContrast   = 0x0043 // SPI_SETHIGHCONTRAST (winuser.h)
	spiGetClientAreaAnim = 0x1042 // SPI_GETCLIENTAREAANIMATION (winuser.h)
	hcfHighContrastOn    = 0x00000001

	sysColorScrollbar   = 0  // COLOR_SCROLLBAR
	sysColorWindow      = 5  // COLOR_WINDOW
	sysColorWindowText  = 8  // COLOR_WINDOWTEXT
	sysColorGrayText    = 17 // COLOR_GRAYTEXT
	sysColorBtnText     = 18 // COLOR_BTNTEXT
	sysColorBtnFace     = 15 // COLOR_BTNFACE
	sysColorBtnShadow   = 16 // COLOR_BTNSHADOW
	sysColorBtnHigh     = 20 // COLOR_BTNHIGHLIGHT
	sysColorHighlight   = 13 // COLOR_HIGHLIGHT
	sysColorHighlightTx = 14 // COLOR_HIGHLIGHTTEXT
	sysColorHotlight    = 26 // COLOR_HOTLIGHT
	sysColorMenuHilite  = 29 // COLOR_MENUHILIGHT
)

// highContrastInfo is WinUser.h's HIGHCONTRAST.
type highContrastInfo struct {
	cbSize            uint32
	dwFlags           uint32
	lpszDefaultScheme uintptr
}

var (
	procGetSysColor       = modUser32.NewProc("GetSysColor")
	procGetCaretBlinkTime = modUser32.NewProc("GetCaretBlinkTime")

	sysPrefsPersonalizePath, _     = syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	sysPrefsColorFilteringPath, _  = syscall.UTF16PtrFromString(`Software\Microsoft\ColorFiltering`)
	sysPrefsAccessibilityPath, _   = syscall.UTF16PtrFromString(`Control Panel\Accessibility`)
	sysPrefsMsAccessibilityPath, _ = syscall.UTF16PtrFromString(`Software\Microsoft\Accessibility`)
	sysPrefsDwmPath, _             = syscall.UTF16PtrFromString(`Software\Microsoft\Windows\DWM`)

	sysPrefsEnableTransparency, _ = syscall.UTF16PtrFromString("EnableTransparency")
	sysPrefsActive, _             = syscall.UTF16PtrFromString("Active")
	sysPrefsFilterType, _         = syscall.UTF16PtrFromString("FilterType")
	sysPrefsDynamicScrollbars, _  = syscall.UTF16PtrFromString("DynamicScrollbars")
	sysPrefsTextScaleFactor, _    = syscall.UTF16PtrFromString("TextScaleFactor")
	sysPrefsAccentColor, _        = syscall.UTF16PtrFromString("AccentColor")
)

// ---------------------------------------------------------------------------
// The snapshot read (the real source)
// ---------------------------------------------------------------------------

// systemSettingsSnapshotProvider is the snapshot seam: the host thread
// reads the OS through it, tests replace it. Swapping it while the host
// is idle (between messages) keeps every read single-threaded.
var systemSettingsSnapshotProvider = readSystemSettingsSnapshot

// readSystemSettingsSnapshot takes one raw reading of every source the
// record derives from. Missing registry values answer the documented
// defaults; failing SPI queries leave the "known" flags unset.
func readSystemSettingsSnapshot() systemSettingsSnapshot {
	snap := defaultSystemSettingsSnapshot()

	// The light/dark registry answer (the same provider the appearance
	// observer uses; a read failure keeps the default light scheme).
	if appearance, err := systemAppearanceProvider(); err == nil {
		snap.appearance = appearance
	} else {
		snap.appearance = WindowAppearanceLight
	}

	// Reduced motion: SPI_GETCLIENTAREAANIMATION writes one BOOL
	// (animation_win.cc:41-47); a failed call means the platform could
	// not say, and the derivation then assumes animations are enabled.
	var animations int32
	r, _, _ := procSystemParametersInfoW.Call(
		uintptr(spiGetClientAreaAnim), 0, uintptr(unsafe.Pointer(&animations)), 0)
	if r != 0 {
		snap.animationsEnabled = animations != 0
		snap.animationsKnown = true
	}

	// Forced colours: SPI_GETHIGHCONTRAST with the HIGHCONTRAST record,
	// HCF_HIGHCONTRASTON decides (os_settings_provider_win.cc:30-37).
	hc := highContrastInfo{cbSize: uint32(unsafe.Sizeof(highContrastInfo{}))}
	r, _, _ = procSystemParametersInfoW.Call(
		uintptr(spiGetHighContrast), uintptr(hc.cbSize), uintptr(unsafe.Pointer(&hc)), 0)
	if r != 0 {
		snap.highContrast = hc.dwFlags&hcfHighContrastOn != 0
	}

	// The system colours: GetCurrentSysColors' set, converted from
	// COLORREF's 0x00BBGGRR to 0xRRGGBB.
	snap.sysColors = systemColorSet{
		BtnFace:       colorrefToRgb(sysColorValue(sysColorBtnFace)),
		BtnHighlight:  colorrefToRgb(sysColorValue(sysColorBtnHigh)),
		BtnShadow:     colorrefToRgb(sysColorValue(sysColorBtnShadow)),
		BtnText:       colorrefToRgb(sysColorValue(sysColorBtnText)),
		GrayText:      colorrefToRgb(sysColorValue(sysColorGrayText)),
		Highlight:     colorrefToRgb(sysColorValue(sysColorHighlight)),
		HighlightText: colorrefToRgb(sysColorValue(sysColorHighlightTx)),
		Hotlight:      colorrefToRgb(sysColorValue(sysColorHotlight)),
		MenuHilight:   colorrefToRgb(sysColorValue(sysColorMenuHilite)),
		Scrollbar:     colorrefToRgb(sysColorValue(sysColorScrollbar)),
		Window:        colorrefToRgb(sysColorValue(sysColorWindow)),
		WindowText:    colorrefToRgb(sysColorValue(sysColorWindowText)),
	}

	// Reduced transparency (os_settings_provider_win.cc:205-214).
	snap.transparencyEnabled = readRegistryDwordDefault(sysPrefsPersonalizePath, sysPrefsEnableTransparency, 1) != 0

	// Inverted colours (os_settings_provider_win.cc:217-231): only the
	// "Invert" filter type counts.
	if readRegistryDwordDefault(sysPrefsColorFilteringPath, sysPrefsActive, 0) == 1 {
		snap.colorFilterInverted = readRegistryDwordDefault(sysPrefsColorFilteringPath, sysPrefsFilterType, 0) == 1
	}

	// Overlay scrollbars (os_settings_provider_win.cc:254-260).
	snap.dynamicScrollbars = readRegistryDwordDefault(sysPrefsAccessibilityPath, sysPrefsDynamicScrollbars, 1) != 0

	// The accent colour (accent_color_observer.cc:126-131): the DWM
	// AccentColor registry value, a COLORREF.
	if accent, ok := readRegistryDword(sysPrefsDwmPath, sysPrefsAccentColor); ok {
		snap.accentColor = colorrefToRgb(accent)
		snap.accentKnown = true
	}

	// The text scale (research/browser-parity/06 §10).
	snap.textScalePercent = readRegistryDwordDefault(sysPrefsMsAccessibilityPath, sysPrefsTextScaleFactor, 100)

	// The caret blink period (os_settings_provider_win.cc:147-160).
	blink, _, _ := procGetCaretBlinkTime.Call()
	snap.caretBlinkMs = uint32(blink)

	return snap
}

// sysColorValue reads one GetSysColor answer (a COLORREF).
func sysColorValue(index uintptr) uint32 {
	v, _, _ := procGetSysColor.Call(index)
	return uint32(v)
}

// colorrefToRgb converts a COLORREF (0x00BBGGRR) to the 0xRRGGBB the
// derivation uses (skia::COLORREFToSkColor's byte swap; the high byte
// some DWM values carry is not part of the colour).
func colorrefToRgb(colorref uint32) uint32 {
	r := colorref & 0xFF
	g := (colorref >> 8) & 0xFF
	b := (colorref >> 16) & 0xFF
	return r<<16 | g<<8 | b
}

// readRegistryDword reads one HKCU DWORD value; ok reports whether the
// key and value exist.
func readRegistryDword(path, name *uint16) (value uint32, ok bool) {
	var key uintptr
	r, _, _ := procRegOpenKeyExWDesktop.Call(
		hkcuRoot, uintptr(unsafe.Pointer(path)), 0,
		uintptr(desktopKeyRead), uintptr(unsafe.Pointer(&key)))
	if int32(r) != 0 {
		return 0, false
	}
	defer procRegCloseKeyDesktop.Call(key)
	var typ uint32
	size := uint32(unsafe.Sizeof(value))
	qr, _, _ := procRegQueryValueExWDesktop.Call(
		key, uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&value)),
		uintptr(unsafe.Pointer(&size)))
	if int32(qr) != 0 || typ != 4 { // REG_DWORD
		return 0, false
	}
	return value, true
}

// readRegistryDwordDefault reads one HKCU DWORD value with the default a
// missing key or value answers (Chromium's ReadValueDW semantics).
func readRegistryDwordDefault(path, name *uint16, def uint32) uint32 {
	if value, ok := readRegistryDword(path, name); ok {
		return value
	}
	return def
}

// ---------------------------------------------------------------------------
// The host record and its refresh
// ---------------------------------------------------------------------------

// seedSystemPreferencesOnHostThread fills the cached record once during
// host boot (the provider's constructor seeding; no event).
func (h *Host) seedSystemPreferencesOnHostThread() {
	snap := systemSettingsSnapshotProvider()
	h.desktop.systemPrefs = deriveSystemPreferences(snap)
	h.desktop.systemColors = deriveSystemColors(snap)
	h.systemPrefsStamp.Add(1)
}

// refreshSystemPreferencesOnHostThread re-reads the snapshot, updates the
// cached record and notifies the observer when something changed. With
// forceInForcedColors, a WM_SYSCOLORCHANGE in a forced-colours session
// notifies even without a change — Chromium's force_notify rule
// (os_settings_provider_win.cc:288-292). Runs on the host thread.
func (h *Host) refreshSystemPreferencesOnHostThread(forceInForcedColors bool) {
	snap := systemSettingsSnapshotProvider()
	prefs := deriveSystemPreferences(snap)
	colors := deriveSystemColors(snap)
	changed := prefs != h.desktop.systemPrefs || colors != h.desktop.systemColors
	h.desktop.systemPrefs = prefs
	h.desktop.systemColors = colors
	// The stamp moves with the record, before any observer notification:
	// a frame the notification triggers re-derives the forced-colours
	// state exactly once (forced_colors.go's style-path cache).
	h.systemPrefsStamp.Add(1)
	if changed || (forceInForcedColors && prefs.ForcedColors) {
		h.withDesktopCallback(
			func(c *desktopCallbacks) *func(SystemPreferences, SystemColors) {
				return &c.systemPreferencesChanged
			},
			func(cb func(SystemPreferences, SystemColors)) { cb(prefs, colors) })
	}
}

// SystemPreferences returns the host's cached system-preference record
// (foreground-thread copy, like MouseWheelSettings). Never-started or
// stopped hosts answer the default record.
func (h *Host) SystemPreferences() SystemPreferences {
	prefs := defaultSystemPreferences()
	h.runForegroundSync(func() { prefs = h.desktop.systemPrefs })
	return prefs
}

// SystemColors returns the host's cached system colours.
func (h *Host) SystemColors() SystemColors {
	colors := defaultSystemColors()
	h.runForegroundSync(func() { colors = h.desktop.systemColors })
	return colors
}

// OnSystemPreferencesChanged registers the host-level observer for
// system-preference changes. It replaces a previous registration and
// never fails (no OS registration is involved); the app-level surface
// (App.OnSystemPreferencesChanged) wraps it in an application update.
func (h *Host) OnSystemPreferencesChanged(cb func(SystemPreferences, SystemColors)) {
	h.setDesktopCallback(
		func(c *desktopCallbacks) *func(SystemPreferences, SystemColors) {
			return &c.systemPreferencesChanged
		}, cb)
}

// handleSystemPrefsChangeMsg routes the colour- and theme-change
// messages to the record refresh. WM_SETTINGCHANGE (both spellings) is
// routed from handleSettingChangeMsg; these three are the broadcast
// companions: WM_SYSCOLORCHANGE re-reads the palette and, in a
// forced-colours session, force-notifies
// (os_settings_provider_win.cc:288-292); WM_THEMECHANGED and the DWM
// colourization change re-read the whole record (the DWM key Chromium
// watches with RegNotifyChangeKeyValue, accent_color_observer.cc:108-113;
// its broadcast spelling arrives here).
func (w *hostWindow) handleSystemPrefsChangeMsg(message uint32) (uintptr, bool) {
	switch message {
	case wmSysColorChange:
		w.host.refreshSystemPreferencesOnHostThread(true)
		return 0, true
	case wmThemeChanged, wmDwmColorizationColorChanged:
		w.host.refreshSystemPreferencesOnHostThread(false)
		return 0, true
	}
	return 0, false
}
