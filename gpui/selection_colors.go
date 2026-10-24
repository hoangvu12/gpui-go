package gpui

// The colours text selection paints with by default, from the system
// (browser-core ticket 28's third clause: "selection highlight colours
// come from the system").
//
// Blink's ::selection UA default (highlight_style_utils.cc
// DefaultBackgroundColor, third_party/blink/renderer/core/highlight/
// highlight_style_utils.cc:188-201) is the theme's active selection
// background (or the inactive one while the frame is unfocused); the
// CSS system colours agree (layout_theme.cc:725-728: kHighlight →
// ActiveSelectionBackgroundColor, kHighlighttext →
// ActiveSelectionForegroundColor), and ActiveSelectionBackgroundColor
// converts the platform colour to "an equivalent color that looks the
// same when blended with white" (layout_theme.cc:365-371, Color::
// BlendWithWhite) — the translucent wash that keeps the page's own
// glyphs readable. Chrome's embedder seeds the platform colour from the
// renderer preferences (kDefaultActiveSelectionBgColor 0xFF1967D2 /
// FgColor 0xFFFFFFFF, third_party/blink/public/common/renderer_
// preferences/renderer_preferences.h:26-27); on Windows the CSS system
// colours come from the OS palette in light and forced-colours modes
// (ticket 27's SystemColors derivation, the same record this reads).
//
// The port: Background = the record's Highlight converted with
// BlendWithWhite; Foreground = HighlightText. The record is live
// (WM_SYSCOLORCHANGE refreshes the palette, system_prefs_windows.go).

// SelectionColors are the default colours of selected text: the
// background wash painted behind the glyphs and the highlight's
// foreground (HighlightText).
type SelectionColors struct {
	// Background is the selection wash: the system Highlight colour in
	// its blend-with-white equivalent, so it stays translucent over
	// light surfaces and the selected text keeps its own colour (what
	// Chrome paints for an unstyled ::selection).
	Background Hsla
	// Foreground is HighlightText, the colour the selected text would
	// paint in when a selection style needs a foreground (the port's
	// selection painting keeps the text colour while the background is
	// translucent, like Blink's translucent-background path).
	Foreground Hsla
}

// SetSelectionColors pins the selection colours the paint path uses
// (SetForcedColors' pattern: a test or a preview pins the colours instead
// of the derived system record; nil restores the derivation).
func (a *App) SetSelectionColors(colors *SelectionColors) {
	a.selectionColorsOverride = colors
}

// SelectionColors returns the selection colours the system record
// resolves (always fresh, any thread). The paint/render path — once per
// frame, not per element — uses SelectionColorsForPaint, the cached
// spelling.
func (a *App) SelectionColors() SelectionColors {
	if a.selectionColorsOverride != nil {
		return *a.selectionColorsOverride
	}
	colors := a.systemColorsForRecord()
	return SelectionColors{
		Background: blendWithWhite(colors.Highlight),
		Foreground: colors.HighlightText,
	}
}

// SelectionColorsForPaint is the paint and render paths' read of the
// selection colours: the cached derivation, re-derived only when the
// host's system-preference record version moved (ticket 29's pattern —
// the host bumps its stamp on every settings refresh). Foreground-
// thread state, like the rest of the frame path; App.SelectionColors is
// the any-thread spelling.
func (a *App) SelectionColorsForPaint() SelectionColors {
	if a.host == nil || a.selectionColorsOverride != nil {
		return a.SelectionColors()
	}
	if stamp := a.host.systemPreferencesStamp(); stamp != a.selectionColorsStamp {
		a.selectionColorsStamp = stamp
		a.selectionColorsDerivations++
		a.selectionColorsCache = a.SelectionColors()
	}
	return a.selectionColorsCache
}

// systemColorsForRecord reads the current system colours (the App
// surface's own spelling, kept in one place for the two reads above).
func (a *App) systemColorsForRecord() SystemColors {
	if a.host == nil {
		return defaultSystemColors()
	}
	return a.host.SystemColors()
}

// blendWithWhite converts an opaque colour into the translucent colour
// that looks the same when blended over white (Color::BlendWithWhite,
// third_party/blink/renderer/platform/graphics/color.cc:1303-1330: an
// already-translucent colour is returned unchanged; an opaque one walks
// the alpha ladder 153..204 step 17 (color.cc:69-71) and stops at the
// first alpha whose components stay non-negative, "try using less
// transparency if the numbers end up being negative"). The component
// arithmetic (color.cc:73-80 BlendComponent: (c - (255 - a)) / (a/255))
// is the pinned integer path.
//
// Divergence, recorded: Chromium keeps the last ladder step's possibly
// negative components for colours no step can represent (a saturated
// blue's red channel) and relies on later 8-bit quantization; the port
// clamps the components into [0, 255] here — the same painted result.
func blendWithWhite(color Hsla) Hsla {
	if color.A < 1 {
		// "If the color contains alpha already, we leave it alone."
		return color
	}
	rgba := hslaToRgba(color)
	r0, g0, b0 := int(roundHalfTowardZero(rgba.R*255)), int(roundHalfTowardZero(rgba.G*255)), int(roundHalfTowardZero(rgba.B*255))
	const (
		startAlpha = 153
		endAlpha   = 204
		step       = 17
	)
	r, g, b, alpha := 0, 0, 0, endAlpha
	for a := startAlpha; a <= endAlpha; a += step {
		whiteBlend := 255 - a
		af := float32(a) / 255
		cr := int(float32(r0-whiteBlend) / af)
		cg := int(float32(g0-whiteBlend) / af)
		cb := int(float32(b0-whiteBlend) / af)
		r, g, b, alpha = cr, cg, cb, a
		if cr >= 0 && cg >= 0 && cb >= 0 {
			break
		}
	}
	clamp8 := func(v int) uint32 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint32(v)
	}
	return RgbaToHsla(clamp8(r)<<24 | clamp8(g)<<16 | clamp8(b)<<8 | uint32(alpha))
}
