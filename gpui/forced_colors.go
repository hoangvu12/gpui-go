package gpui

import "fmt"

// The forced-colours pass of browser-core ticket 29: in Windows
// high-contrast mode, an optional pass maps every authored colour onto
// the system palette during style resolution, as Chromium does in
// forced-colors mode. The port follows Chromium's shape:
//
//   - The switch is preference-driven: the pass is active exactly when
//     App.SystemPreferences().ForcedColors reports a high-contrast
//     session (Windows SPI_GETHIGHCONTRAST & HCF_HIGHCONTRASTON, the
//     preference ticket 27 reads). Forced colours is an OS state, not an
//     app decision — an app opting in would stay wrong the moment the
//     user turns high contrast off — so there is no opt-in flag; apps
//     that need a deterministic pass (tests, a "preview high contrast"
//     developer toggle) pin the state with App.SetForcedColors, which
//     wins until cleared, and pinning Active false turns the pass off
//     for the whole app (the spec's optional pass).
//   - The mapping is Chromium's AdjustForForcedColorsMode
//     (third_party/blink/renderer/core/css/resolver/style_adjuster.cc:
//     1034-1058 @ 0d4dcd4f) plus the ShouldForceColor accessors
//     (core/style/computed_style.h:2917-2922): the text colour becomes
//     the forced colour (CanvasText by default — the inherited
//     -internal-forced-color's initial value, css_properties.json5:
//     10807-10823), the background becomes Canvas with the authored
//     ALPHA kept (the unforced background's alpha is copied onto the
//     forced colour, longhands_custom.cc:6140-6156), the border, ring
//     (outline), underline and strikethrough (text-decoration) colours
//     become the forced colour (the -internal-forced-*-color
//     properties' currentColor defaults resolved against the forced
//     colour, css_properties.json5:10793-10805, 10825-10837 and
//     longhands_custom.cc:10036-10047), box shadows are removed
//     (style_adjuster.cc:1039-1040), and gradient backgrounds are
//     cleared like background images (style_adjuster.cc:1050-1053).
//   - The per-element opt-out is forced-color-adjust: none
//     (ForcedColorAdjustNone), which skips the whole mapping exactly
//     like Chromium's adjuster (style_adjuster.cc:1036-1038): authored
//     colours, shadows included, pass through.
//
// The pass runs at the END of style resolution — the div's
// computeStyleInternal (interact.go) and the image's computeStyle —
// so it sees the fully refined style, like the adjuster that runs after
// the cascade. The style-resolution path reads the pass state through
// a cached derivation (styleForcedColors): the host versions its
// system-preference record with a stamp it bumps on every refresh
// (WM_SETTINGCHANGE and family), so the style path re-derives at most
// once per record version — no host accessor round trip per element —
// and a preference flip is visible on the very next style resolution
// (the next frame), with no invalidation machinery of its own. The
// application-level opt-out is SetForcedColors(&ForcedColorsState{
// Active: false}): the pass is off for the whole app, whatever the OS
// reports.
//
// Not mapped (no Style-layer equivalent in the port): caret-color (the
// editor draws its own caret), column-rule-color (no multi-column
// layout), fill/stroke (no SVG paint), text-shadow (no text shadows),
// scrollbar-color and accent-color (no widgets). Chromium's escape
// hatch of authoring a system-colour keyword (ShouldForceColor's
// IsSystemColorIncludingDeprecated check, computed_style.h:2917-2922)
// has no port spelling — colours are plain Hsla values with no keyword
// identity — so an element that must keep a specific colour under
// forced colours opts out with ForcedColorAdjustNone and sets it.

// ForcedColorAdjust is the forced-color-adjust property (CSS Color
// Adjustment 1 §forced-color-adjust: auto is the default, none opts the
// element out of the forced-colours pass). The spec's third value
// (preserve-parent-color, a Blink-internal spelling that re-inherits
// the parent's used colour) is not ported: the port has no separate
// authored/forced cascade to bridge, and an element that wants the
// parent's colour sets it and opts out.
type ForcedColorAdjust uint8

const (
	// ForcedColorAdjustAuto maps the element's colours onto the system
	// palette while forced colours are active (the default).
	ForcedColorAdjustAuto ForcedColorAdjust = iota
	// ForcedColorAdjustNone keeps the element's authored colours and
	// shadows under forced colours.
	ForcedColorAdjustNone
)

// String returns the CSS value name.
func (a ForcedColorAdjust) String() string {
	switch a {
	case ForcedColorAdjustAuto:
		return "auto"
	case ForcedColorAdjustNone:
		return "none"
	}
	return fmt.Sprintf("ForcedColorAdjust(%d)", uint8(a))
}

// ForcedColorsState is the forced-colours pass's configuration: whether
// the pass maps colours, and the system palette it maps onto. The
// palette is the CSS Color 4 system colour set (App.SystemColors) —
// the host's palette when the OS drives the pass, or the palette a
// test or a preview pinned with SetForcedColors.
type ForcedColorsState struct {
	// Active reports whether style resolution maps colours onto the
	// palette.
	Active bool
	// Colors is the palette the pass maps onto (Canvas for
	// backgrounds, CanvasText for text, borders and decorations).
	Colors SystemColors
}

// ForcedColors returns the forced-colours pass's current state: the
// state pinned with SetForcedColors when there is one, otherwise the
// state derived from the system preferences (active exactly when the
// system reports forced colours, the palette the system reports). A
// hostless test application answers the documented default record: the
// pass is off. This is the always-fresh, any-thread spelling: every
// call derives through the host accessors (SystemPreferences and
// SystemColors marshal to the foreground), so a preference change is
// visible on the next call. The style-resolution path uses the cached
// styleForcedColors spelling instead (no host round trip per element).
func (a *App) ForcedColors() ForcedColorsState {
	if a.forcedColorsOverride != nil {
		return *a.forcedColorsOverride
	}
	if a.host == nil {
		return ForcedColorsState{}
	}
	return a.deriveForcedColors()
}

// deriveForcedColors derives the pass state from the live records.
func (a *App) deriveForcedColors() ForcedColorsState {
	return ForcedColorsState{
		Active: a.SystemPreferences().ForcedColors,
		Colors: a.SystemColors(),
	}
}

// styleForcedColors is the style-resolution path's read of the pass
// state: the application's cached derivation, re-derived only when the
// host's system-preference record version moved (the host bumps its
// stamp on every WM_SETTINGCHANGE-family refresh, system_prefs_
// windows.go). One derivation per record version — two foreground
// accessor calls — instead of two per element per frame; an inactive
// read is an override check, a host check and one atomic load. The
// cache is foreground-thread state, like the rest of the frame path
// that reaches here (element request/prepaint/paint run on the
// application's foreground thread); App.ForcedColors is the any-thread
// spelling.
func (a *App) styleForcedColors() ForcedColorsState {
	if a.forcedColorsOverride != nil {
		return *a.forcedColorsOverride
	}
	if a.host == nil {
		return ForcedColorsState{}
	}
	if stamp := a.host.systemPreferencesStamp(); stamp != a.forcedColorsStamp {
		a.forcedColorsStamp = stamp
		a.forcedColorsDerivations++
		a.forcedColorsCache = a.deriveForcedColors()
	}
	return a.forcedColorsCache
}

// SetForcedColors pins the forced-colours pass's state (the
// preference-driven derivation stops while the override is set); nil
// clears the pin and returns to the derived state. Pinning
// &ForcedColorsState{Active: false} is the application-level opt-out —
// the pass is off for the whole app, whatever the OS reports (the
// spec's "optional" pass: an app whose own palette already meets
// high-contrast needs can refuse the mapping). This is also the
// deterministic spelling for tests and for apps that preview the pass
// (a high-contrast preview toggle); an app that wants the real
// behaviour never calls it — the pass follows the OS. Call it inside
// App.Update; a change queues the RefreshWindows effect so every
// window redraws with the new state (the SetReduceMotion shape,
// app.rs:1123-1129). Clearing the pin re-derives on the next style
// read if the host record moved while it was set (the stamp compare
// catches up), so no extra invalidation is needed.
func (a *App) SetForcedColors(state *ForcedColorsState) {
	if a.forcedColorsOverride == nil && state == nil {
		return
	}
	if a.forcedColorsOverride != nil && state != nil && *a.forcedColorsOverride == *state {
		return
	}
	a.forcedColorsOverride = state
	a.RefreshWindows()
}

// applyForcedColorsToStyle maps s's colours onto the system palette at
// the end of style resolution (the port of AdjustForForcedColorsMode
// plus the ShouldForceColor accessors; see the file comment). Inactive
// passes and opted-out elements return unchanged — the opt-out check
// first (it needs no app state), then the cached pass state, so the
// inactive path makes no host accessor calls and allocates nothing.
func applyForcedColorsToStyle(s *Style, app *App) {
	if s.ForcedColorAdjust == ForcedColorAdjustNone {
		return
	}
	state := app.styleForcedColors()
	if !state.Active {
		return
	}
	applyForcedColors(s, state.Colors)
}

// applyForcedColors is the mapping body: colors is the system palette
// the pass resolved (styleForcedColors).
func applyForcedColors(s *Style, colors SystemColors) {
	forcedColor := colors.CanvasText

	// color: every element's used colour is the forced colour
	// (Color::ColorIncludingFallback, longhands_custom.cc:2470-2478:
	// ShouldForceColor hands the value to the forced-colour property,
	// whose inherited default is CanvasText, css_properties.json5:
	// 10807-10823). Setting the refinement's colour also cascades it to
	// the children through the text style stack, the port's spelling of
	// the inherited -internal-forced-color.
	s.Text.Color = Some(forcedColor)

	// background-color: Canvas with the authored alpha kept
	// (InternalForcedBackgroundColor::ColorIncludingFallback reads the
	// unforced background's alpha and sets it on the forced colour,
	// longhands_custom.cc:6140-6156). A background image (the port's
	// gradient arm) is cleared instead — forced colours drops
	// background images that are not URL images, style_adjuster.cc:
	// 1050-1053 — leaving nothing painted, the CSS result of a cleared
	// image over a transparent background colour.
	switch {
	case s.Background == nil:
	case s.Background.Kind == BackgroundKindLinearGradient:
		s.Background = nil
	default:
		background := *s.Background
		background.Solid = withAlpha(colors.Canvas, s.Background.Solid.A)
		s.Background = &background
	}

	// border-*-color: the forced colour (InternalForcedBorderColor::
	// ColorIncludingFallback resolves the property's currentColor
	// default against the forced colour, longhands_custom.cc:6175-6190;
	// css_properties.json5:10793-10805). No alpha is kept.
	if s.BorderColor != nil {
		border := *s.BorderColor
		border.Solid = withAlpha(forcedColor, 1)
		border.Kind = BackgroundKindSolid
		border.Gradient = LinearGradient{}
		s.BorderColor = &border
	}

	// outline-color: the ring is the port's outline, and its colour maps
	// to the forced colour (InternalForcedOutlineColor::
	// ColorIncludingFallback, longhands_custom.cc:6248-6260; a
	// currentColor ring already resolves against the forced text colour
	// above).
	if s.Ring.Color.Explicit {
		s.Ring.Color = RingColorOf(SolidBackground(forcedColor))
	}
	if s.InsetRing.Color.Explicit {
		s.InsetRing.Color = RingColorOf(SolidBackground(forcedColor))
	}

	// outline-color: the port's outline is a real outline now
	// (browser-core ticket 48). An author's explicit colour maps to the
	// forced colour (InternalForcedOutlineColor::ColorIncludingFallback,
	// longhands_custom.cc:6248-6260); the default focus ring's unset
	// colour is the forced Highlight (the UA forced-colours rule
	// `:focus-visible { -internal-forced-outline-color: Highlight }`,
	// forced_colors.css:18-19) — a currentColor outline already
	// resolves against the forced text colour.
	if s.Outline.Color.Explicit {
		s.Outline.Color = RingColorOf(SolidBackground(forcedColor))
	} else if s.Outline.Kind == OutlineFocusRing {
		s.Outline.Color = RingColorOf(SolidBackground(colors.Highlight))
	}

	// text-decoration-color: the underline and strikethrough colours map
	// to the forced colour (TextDecorationColor::ColorIncludingFallback
	// returns the forced current colour, longhands_custom.cc:10036-
	// 10047). A nil colour is the pin's own default (transparent), not
	// an authored colour, and stays untouched.
	if underline, ok := s.Text.Underline.Get(); ok && underline.Color != nil {
		color := forcedColor
		underline.Color = &color
		s.Text.Underline = Some(underline)
	}
	if strike, ok := s.Text.Strikethrough.Get(); ok && strike.Color != nil {
		color := forcedColor
		strike.Color = &color
		s.Text.Strikethrough = Some(strike)
	}

	// box-shadow / text-shadow: none (style_adjuster.cc:1039-1040 resets
	// both to their initial values; the port has no text shadows).
	s.BoxShadow = nil
}

// withAlpha returns color with its alpha set to a.
func withAlpha(color Hsla, a float32) Hsla {
	color.A = clamp01(a)
	return color
}
