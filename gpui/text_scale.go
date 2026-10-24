package gpui

// D11 (browser-parity spec): rem = 16 × the Windows text size, with a
// per-app opt-out. The reference seeds every window's rem at 16 logical
// pixels (window.rs `rem_size: px(16.)`); the browser instead resolves
// rems against the OS text size — Blink's font scale factor hook
// (SetAccessibilityFontScaleFactor, "TODO: Rename this OSTextScaleFactor",
// third_party/blink/renderer/core/exported/web_settings_impl.cc:158-161),
// fed on Windows from the WinRT UISettings.TextScaleFactor registry
// backing store (ticket 27's snapshot, HKCU\Software\Microsoft\
// Accessibility!TextScaleFactor).
//
// The port keeps DefaultRemSize (16) as the base and scales it by the
// effective text scale at every frame draw (render.go): a preference
// change repaints the windows (system_prefs_windows.go) and the next
// frame's rebuilt layout tree reflows at the new rem, which is the
// "text scale change must reflow live" acceptance.
//
// Cost model (ticket 29's pattern): the frame path never calls into the
// host per element or per window. The host versions its
// system-preference record with systemPrefsStamp, and the frame path's
// read (frameTextScale) re-derives — one foreground accessor call — at
// most once per record version, not once per frame.

// TextScale returns the effective text scale rem resolution multiplies
// into the base rem: the pinned value (SetTextScale) while one is set,
// otherwise the Windows text size the system-preferences record reports
// — or 1.0 while the app has opted out (SetRemFollowsSystemTextScale
// (false), the per-app opt-out that holds rem at 16). Always fresh and
// safe from any thread; the frame path uses the cached frameTextScale
// spelling instead. A test application (no host) answers the documented
// default record, 1.0.
func (a *App) TextScale() float32 {
	if a.textScaleOverride != nil {
		return *a.textScaleOverride
	}
	if a.host == nil {
		return a.textScalePolicy(defaultSystemPreferences().TextScale)
	}
	return a.textScalePolicy(a.SystemPreferences().TextScale)
}

// SetTextScale pins the text scale (nil clears the pin and returns to
// the system-derived value). The pin wins over the system record and
// over the opt-out — it is the deterministic spelling for tests and for
// apps that preview a text size; an app that wants the real behaviour
// never calls it. Call it inside App.Update; a change queues the
// RefreshWindows effect so every window reflows (the SetForcedColors
// shape, app.rs:1123-1129).
func (a *App) SetTextScale(scale *float32) {
	if a.textScaleOverride == nil && scale == nil {
		return
	}
	if a.textScaleOverride != nil && scale != nil && *a.textScaleOverride == *scale {
		return
	}
	a.textScaleOverride = scale
	// The frame cache must re-derive even though the host record did
	// not move (the policy stamp below), so the next frame reflows.
	a.textScalePolicyVersion++
	a.RefreshWindows()
}

// SetRemFollowsSystemTextScale selects whether rem follows the Windows
// text size. The default (enabled) is D11; passing false is the
// per-app opt-out — the app's rems resolve against 16 logical pixels
// whatever the OS reports (an app whose own layout cannot reflow at
// other text sizes can refuse). The pinned scale (SetTextScale) still
// wins. Call it inside App.Update; a change queues the RefreshWindows
// effect.
func (a *App) SetRemFollowsSystemTextScale(follow bool) {
	disabled := !follow
	if a.systemTextScaleDisabled == disabled {
		return
	}
	a.systemTextScaleDisabled = disabled
	// The frame cache must re-derive even though the host record did
	// not move (the policy stamp below), so the next frame reflows.
	a.textScalePolicyVersion++
	a.RefreshWindows()
}

// textScalePolicy applies the opt-out to a system-reported scale.
func (a *App) textScalePolicy(system float32) float32 {
	if a.systemTextScaleDisabled {
		return 1
	}
	if system <= 0 {
		// A missing or zero reading stays 1.0 (the snapshot's mapping).
		return 1
	}
	return system
}

// frameTextScale is the frame path's read of the text scale: the cached
// derivation, re-derived only when the host's system-preference record
// version moved (the host bumps its stamp on every WM_SETTINGCHANGE-
// family refresh, system_prefs_windows.go). One derivation per record
// version instead of one per frame; a pinned or opt-out state short-
// circuits without touching the host at all. Foreground-thread state,
// like the rest of the frame path that reads it; App.TextScale is the
// any-thread spelling. textScaleDerivations counts the derivations (the
// no-per-frame-host-reads test's seam).
func (a *App) frameTextScale() float32 {
	if a.textScaleOverride != nil {
		return *a.textScaleOverride
	}
	if a.host == nil {
		return a.textScalePolicy(defaultSystemPreferences().TextScale)
	}
	stamp := a.host.systemPreferencesStamp()
	if stamp != a.textScaleStamp || a.textScalePolicyStamp != a.textScalePolicyVersion {
		// The record version OR the app's own policy moved (the pin and
		// the opt-out bump textScalePolicyVersion, app.go): re-derive.
		a.textScaleStamp = stamp
		a.textScalePolicyStamp = a.textScalePolicyVersion
		a.textScaleDerivations++
		a.textScaleCache = a.textScalePolicy(a.SystemPreferences().TextScale)
	}
	return a.textScaleCache
}

// frameRemSize returns the rem size a window's frames resolve rems
// against: the base rem (DefaultRemSize, the reference window.rs
// `rem_size: px(16.)`) scaled by the frame path's cached text scale.
// The frame draw stores it on the window's draw state (render.go), so
// RemSize, WithRemSize overrides and the between-draw LineHeight read
// all agree with the frames.
func (a *App) frameRemSize() float32 {
	return DefaultRemSize * a.frameTextScale()
}
