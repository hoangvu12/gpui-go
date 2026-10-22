package base

// Application-wide defaults for Base behavior modules (U:theme.rs).

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ThemeAppearance is the light/dark appearance (U:theme.rs:7-14), lower
// case in JSON (serde rename_all = "lowercase").
type ThemeAppearance uint8

const (
	// ThemeAppearanceLight is the light appearance (the default).
	ThemeAppearanceLight ThemeAppearance = iota
	// ThemeAppearanceDark is the dark appearance.
	ThemeAppearanceDark
)

// String renders the appearance like the derived Debug.
func (a ThemeAppearance) String() string {
	if a == ThemeAppearanceDark {
		return "Dark"
	}
	return "Light"
}

// MarshalText encodes the serde lower-case name.
func (a ThemeAppearance) MarshalText() ([]byte, error) {
	switch a {
	case ThemeAppearanceLight:
		return []byte("light"), nil
	case ThemeAppearanceDark:
		return []byte("dark"), nil
	}
	return nil, fmt.Errorf("base: invalid ThemeAppearance %d", uint8(a))
}

// UnmarshalText decodes the serde lower-case name.
func (a *ThemeAppearance) UnmarshalText(text []byte) error {
	switch string(text) {
	case "light":
		*a = ThemeAppearanceLight
	case "dark":
		*a = ThemeAppearanceDark
	default:
		return fmt.Errorf("base: unknown variant %q, expected `light` or `dark`", string(text))
	}
	return nil
}

// Theme is the Base theme global (U:theme.rs:16-25).
type Theme struct {
	Appearance ThemeAppearance
	Tokens     SemanticThemeTokens
	Scrollbar  ScrollbarTheme
	Resizable  ResizableTheme
	Plot       PlotTheme
}

// DefaultTheme returns the default theme (U:theme.rs:16 derive(Default)).
func DefaultTheme() Theme {
	return Theme{
		Appearance: ThemeAppearanceLight,
		Tokens:     DefaultSemanticThemeTokens(),
		Scrollbar:  NewScrollbarTheme(),
		Plot:       NewPlotTheme(),
	}
}

// Clone returns an independent copy (the derived Clone).
func (t Theme) Clone() Theme {
	t.Tokens = t.Tokens.Clone()
	return t
}

// ThemeGlobal returns a copy of the theme global, or the default theme
// when none is set (U:theme.rs:28-30 Theme::global). The global is stored
// as *Theme.
func ThemeGlobal(app *gpui.App) Theme {
	if t, ok := gpui.TryGlobal[*Theme](app); ok {
		return t.Clone()
	}
	return DefaultTheme()
}

// ThemeGlobalMut returns the mutable theme global, assigning the default
// first when absent (U:theme.rs:32-37 Theme::global_mut). Like
// App::global_mut (R:app.rs:2128-2135) it queues a global notification.
func ThemeGlobalMut(app *gpui.App) *Theme {
	if !gpui.HasGlobal[*Theme](app) {
		t := DefaultTheme()
		app.SetGlobal(&t)
	}
	app.NotifyGlobal[*Theme]()
	return gpui.Global[*Theme](app)
}

// activeTheme is the crate-private ActiveTheme for App
// (U:theme.rs:40-50).
func activeTheme(app *gpui.App) Theme { return ThemeGlobal(app) }

// ScrollbarTheme holds the global defaults used by the scrollbar
// (U:theme.rs:52-94). Its motion defaults to motionless: styled layers
// project their own timing; Base never installs a fade or slide.
type ScrollbarTheme struct {
	mode   ScrollbarMode
	motion ScrollbarMotion
	styles ScrollbarStyles
}

// NewScrollbarTheme returns the default scrollbar theme
// (U:theme.rs:64-66).
func NewScrollbarTheme() ScrollbarTheme {
	return ScrollbarTheme{motion: DefaultScrollbarMotion()}
}

// WithMode sets the show mode (U:theme.rs:68-71).
func (t ScrollbarTheme) WithMode(mode ScrollbarMode) ScrollbarTheme { t.mode = mode; return t }

// WithMotion sets the motion tokens (U:theme.rs:73-76).
func (t ScrollbarTheme) WithMotion(motion ScrollbarMotion) ScrollbarTheme {
	t.motion = motion
	return t
}

// WithStyles sets the paint styles (U:theme.rs:78-81).
func (t ScrollbarTheme) WithStyles(styles ScrollbarStyles) ScrollbarTheme {
	t.styles = styles
	return t
}

// Mode returns the show mode (U:theme.rs:83-85).
func (t ScrollbarTheme) Mode() ScrollbarMode { return t.mode }

// Motion returns the motion tokens (U:theme.rs:87-89).
func (t ScrollbarTheme) Motion() ScrollbarMotion { return t.motion }

// Styles returns the paint styles (U:theme.rs:91-93).
func (t *ScrollbarTheme) Styles() *ScrollbarStyles { return &t.styles }

// ResizableTheme holds the global visual defaults of resizable panel
// handles (U:theme.rs:120-137). An absent value means unset, not
// invisible: an unprojected handle resolves from the active tokens —
// border at rest, ring while dragging.
type ResizableTheme struct {
	Handle       gpui.Optional[gpui.Hsla]
	ActiveHandle gpui.Optional[gpui.Hsla]
}

// PlotTheme holds the global defaults used by the plot (U:theme.rs:96-119).
// Its motion defaults to motionless: styled layers project their own
// timing; Base never installs a fade or glide.
type PlotTheme struct {
	motion PlotMotion
}

// NewPlotTheme returns the default plot theme (U:theme.rs:106-108).
func NewPlotTheme() PlotTheme {
	return PlotTheme{motion: DefaultPlotMotion()}
}

// WithMotion sets the motion (U:theme.rs:110-113).
func (t PlotTheme) WithMotion(motion PlotMotion) PlotTheme {
	t.motion = motion
	return t
}

// Motion returns the motion (U:theme.rs:115-117).
func (t PlotTheme) Motion() PlotMotion { return t.motion }
