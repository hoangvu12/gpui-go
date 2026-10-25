package gpui

// Browser-core ticket 29's tests for the forced-colours pass: in
// Windows high-contrast mode the pass maps every authored colour onto
// the system palette during style resolution, as Chromium does
// (StyleAdjuster::AdjustForForcedColorsMode,
// third_party/blink/renderer/core/css/resolver/style_adjuster.cc:1034-
// 1058 @ 0d4dcd4f, and the ShouldForceColor accessors,
// core/style/computed_style.h:2917-2922), the per-element opt-out is
// forced-color-adjust: none, and the pass is off whenever the system
// reports no forced colours. The style-level checks drive the div's
// computeStyleInternal exactly like the interactivity tests
// (interact_test.go); the host-driven preference check lives in
// forced_colors_windows_test.go.

import (
	"reflect"
	"testing"
)

// forcedColorsTestPalette is a distinctive palette for the mapping
// tests: a black canvas with white text, so a pass that wrongly
// hardcodes the classic white-on-black theme fails.
func forcedColorsTestPalette() SystemColors {
	return SystemColors{
		Canvas:           Rgb(0x000000),
		CanvasText:       Rgb(0xffffff),
		LinkText:         Rgb(0x00ffff),
		VisitedText:      Rgb(0x00ffff),
		ActiveText:       Rgb(0xff00ff),
		ButtonFace:       Rgb(0x336633),
		ButtonText:       Rgb(0xffffff),
		ButtonBorder:     Rgb(0xffffff),
		Field:            Rgb(0x000000),
		FieldText:        Rgb(0xffffff),
		Highlight:        Rgb(0xffff00),
		HighlightText:    Rgb(0x000000),
		SelectedItem:     Rgb(0xffff00),
		SelectedItemText: Rgb(0x000000),
		Mark:             Rgb(0xffff00),
		MarkText:         Rgb(0x000000),
		GrayText:         Rgb(0x808080),
		AccentColor:      Rgb(0x00ff00),
		AccentColorText:  Rgb(0x000000),
	}
}

// newForcedColorsFixture builds a hostless test app and window: the
// system-preference record is the documented default (no forced
// colours), so the pass is off until a test pins an override.
func newForcedColorsFixture(t *testing.T) (*App, *Window) {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	t.Cleanup(func() {
		CloseTestWindow(window)
		_ = app.Shutdown()
	})
	return app, window
}

// forceForcedColors pins the pass on with the test palette.
func forceForcedColors(app *App) {
	state := ForcedColorsState{Active: true, Colors: forcedColorsTestPalette()}
	app.SetForcedColors(&state)
}

// styleOf computes one div's resolved style in the fixture's window.
func styleOf(app *App, window *Window, d *DivElement) Style {
	var style Style
	app.Update(func(*App) {
		style = d.computeStyleInternal(nil, nil, window, app)
	})
	return style
}

// TestForcedColorsPassIsOffWithoutForcedColors checks the acceptance
// rule: a hostless test app answers the default preference record (no
// forced colours), and the pass leaves authored colours untouched.
func TestForcedColorsPassIsOffWithoutForcedColors(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	if state := app.ForcedColors(); state.Active {
		t.Fatalf("App.ForcedColors() = %+v, want inactive without forced colours", state)
	}
	d := Div().Flex().
		TextColor(Rgb(0xdc2626)).
		Bg(Rgb(0x2563eb).Alpha(0.5)).
		Border(Px(2)).
		BorderColor(Rgb(0x16a34a)).
		Shadow([]BoxShadow{NewBoxShadow(0, 4, SolidBackground(Rgb(0x000000).Alpha(0.25)))})
	style := styleOf(app, window, d)
	if color, ok := style.Text.Color.Get(); !ok || color != Rgb(0xdc2626) {
		t.Fatalf("text color = %v (present %v), want the authored red", color, ok)
	}
	if bg := style.Background; bg == nil || bg.Solid != Rgb(0x2563eb).Alpha(0.5) {
		t.Fatalf("background = %+v, want the authored half-transparent blue", bg)
	}
	if border := style.BorderColor; border == nil || border.Solid != Rgb(0x16a34a) {
		t.Fatalf("border color = %+v, want the authored green", border)
	}
	if len(style.BoxShadow) != 1 {
		t.Fatalf("box shadows = %d, want the authored shadow", len(style.BoxShadow))
	}
}

// TestForcedColorsPassIsOffWhenTheOverrideReportsNoForcedColors checks
// that an override set with Active false keeps the pass off (the
// "preference-driven, off when high contrast is off" rule extends to
// pinned preview states).
func TestForcedColorsPassIsOffWhenTheOverrideReportsNoForcedColors(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	app.SetForcedColors(&ForcedColorsState{Active: false, Colors: forcedColorsTestPalette()})
	d := Div().Flex().TextColor(Rgb(0xdc2626)).Bg(Rgb(0x2563eb))
	style := styleOf(app, window, d)
	if color, ok := style.Text.Color.Get(); !ok || color != Rgb(0xdc2626) {
		t.Fatalf("text color = %v (present %v), want the authored red", color, ok)
	}
	if bg := style.Background; bg == nil || bg.Solid != Rgb(0x2563eb) {
		t.Fatalf("background = %+v, want the authored blue", bg)
	}
}

// TestForcedColorsMapsStyleColorsToSystemColors checks the mapping: the
// text color becomes CanvasText, the background becomes Canvas with the
// authored alpha kept, the border and ring colors become CanvasText
// (currentColor), the underline and strikethrough colors become
// CanvasText, and box shadows disappear.
func TestForcedColorsMapsStyleColorsToSystemColors(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	forceForcedColors(app)
	underlineRed := Rgb(0xdc2626)
	strikeGreen := Rgb(0x16a34a)
	d := Div().Flex().
		TextColor(Rgb(0xdc2626)).
		Bg(Rgb(0x2563eb).Alpha(0.5)).
		Border(Px(2)).
		BorderColor(Rgb(0x16a34a)).
		Ring(1).
		RingColor(Rgb(0x9333ea)).
		Shadow([]BoxShadow{NewBoxShadow(0, 4, SolidBackground(Rgb(0x000000).Alpha(0.25)))})
	d.TextStyleMut().Underline = Some(UnderlineStyle{Thickness: 1, Color: &underlineRed})
	d.TextStyleMut().Strikethrough = Some(StrikethroughStyle{Thickness: 1, Color: &strikeGreen})
	style := styleOf(app, window, d)

	if color, ok := style.Text.Color.Get(); !ok || color != Rgb(0xffffff) {
		t.Fatalf("text color = %v (present %v), want CanvasText (white)", color, ok)
	}
	if bg := style.Background; bg == nil || bg.Kind != BackgroundKindSolid {
		t.Fatalf("background = %+v, want a solid", bg)
	} else if bg.Solid != Rgb(0x000000).Alpha(0.5) {
		t.Fatalf("background color = %+v, want Canvas (black) with the authored alpha 0.5 kept", bg.Solid)
	}
	if border := style.BorderColor; border == nil || border.Solid != Rgb(0xffffff) {
		t.Fatalf("border color = %+v, want CanvasText (white)", border)
	}
	if !style.Ring.Color.Explicit || style.Ring.Color.Color.Solid != Rgb(0xffffff) {
		t.Fatalf("ring color = %+v, want CanvasText (white)", style.Ring.Color)
	}
	if underline, ok := style.Text.Underline.Get(); !ok || underline.Color == nil || *underline.Color != Rgb(0xffffff) {
		t.Fatalf("underline color = %+v, want CanvasText (white)", underline.Color)
	}
	if strike, ok := style.Text.Strikethrough.Get(); !ok || strike.Color == nil || *strike.Color != Rgb(0xffffff) {
		t.Fatalf("strikethrough color = %+v, want CanvasText (white)", strike.Color)
	}
	if len(style.BoxShadow) != 0 {
		t.Fatalf("box shadows = %d, want none under forced colours", len(style.BoxShadow))
	}
}

// TestForcedColorsForcesUnauthoredColorsToo checks the inherited case:
// an element that authored no colour at all is still forced (Chromium's
// -internal-forced-color is inherited with the CanvasText default, so
// plain text turns CanvasText, css_properties.json5:10807-10823).
func TestForcedColorsForcesUnauthoredColorsToo(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	forceForcedColors(app)
	style := styleOf(app, window, Div().Flex().Bg(Rgb(0xffffff)))
	if color, ok := style.Text.Color.Get(); !ok || color != Rgb(0xffffff) {
		t.Fatalf("text color = %v (present %v), want CanvasText forced on a colorless element", color, ok)
	}
	if bg := style.Background; bg == nil || bg.Solid != Rgb(0x000000) {
		t.Fatalf("background = %+v, want the opaque Canvas (black)", bg)
	}
}

// TestForcedColorsKeepsTransparentBackgroundsInvisible checks the
// alpha rule's boundary: a fully transparent background maps to Canvas
// with alpha 0, which paints nothing exactly like the authored value
// (Chromium sets the unforced alpha onto the forced colour,
// longhands_custom.cc:6140-6156).
func TestForcedColorsKeepsTransparentBackgroundsInvisible(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	forceForcedColors(app)
	style := styleOf(app, window, Div().Flex().Bg(Rgb(0x2563eb).Alpha(0)))
	if bg := style.Background; bg == nil || !bg.IsTransparent() {
		t.Fatalf("background = %+v, want a fully transparent Canvas", bg)
	}
}

// TestForcedColorsClearsGradientBackgrounds checks the background-image
// rule: a gradient is a background image, and forced colours clears
// background images (style_adjuster.cc:1050-1053), so the gradient
// background disappears rather than being mapped.
func TestForcedColorsClearsGradientBackgrounds(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	forceForcedColors(app)
	gradient := Background{Kind: BackgroundKindLinearGradient, Gradient: LinearGradient{
		Direction: GradientDirectionAngle,
		Angle:     90,
		From:      GradientStop{Color: Rgb(0xdc2626), Position: 0},
		To:        GradientStop{Color: Rgb(0x2563eb), Position: 1},
	}}
	style := styleOf(app, window, Div().Flex().Bg(gradient))
	if bg := style.Background; bg != nil {
		t.Fatalf("background = %+v, want the cleared gradient (no background)", bg)
	}
}

// TestForcedColorAdjustNoneOptsOutOfThePass checks the per-element
// opt-out: forced-color-adjust: none leaves every colour authored,
// shadows included (Chromium's adjuster returns before the mapping,
// style_adjuster.cc:1036-1038).
func TestForcedColorAdjustNoneOptsOutOfThePass(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	forceForcedColors(app)
	d := Div().Flex().
		ForcedColorAdjustNone().
		TextColor(Rgb(0xdc2626)).
		Bg(Rgb(0x2563eb).Alpha(0.5)).
		Border(Px(2)).
		BorderColor(Rgb(0x16a34a)).
		Shadow([]BoxShadow{NewBoxShadow(0, 4, SolidBackground(Rgb(0x000000).Alpha(0.25)))})
	style := styleOf(app, window, d)
	if style.ForcedColorAdjust != ForcedColorAdjustNone {
		t.Fatalf("forced-color-adjust = %v, want none", style.ForcedColorAdjust)
	}
	if color, ok := style.Text.Color.Get(); !ok || color != Rgb(0xdc2626) {
		t.Fatalf("text color = %v (present %v), want the authored red", color, ok)
	}
	if bg := style.Background; bg == nil || bg.Solid != Rgb(0x2563eb).Alpha(0.5) {
		t.Fatalf("background = %+v, want the authored blue", bg)
	}
	if border := style.BorderColor; border == nil || border.Solid != Rgb(0x16a34a) {
		t.Fatalf("border color = %+v, want the authored green", border)
	}
	if len(style.BoxShadow) != 1 {
		t.Fatalf("box shadows = %d, want the authored shadow kept by the opt-out", len(style.BoxShadow))
	}
}

// TestForcedColorAdjustNoneClass checks the Tailwind class of the
// opt-out: forced-color-adjust-none applies what
// ForcedColorAdjustNone() applies.
func TestForcedColorAdjustNoneClass(t *testing.T) {
	if err := CheckClasses("forced-color-adjust-none"); err != nil {
		t.Fatalf("the forced-color-adjust-none class must apply: %v", err)
	}
	d := Div().Flex().Class("forced-color-adjust-none")
	value, ok := d.StyleMut().ForcedColorAdjust.Get()
	if !ok || value != ForcedColorAdjustNone {
		t.Fatalf("the class set ForcedColorAdjust = %+v (present %v), want none", value, ok)
	}
}

// TestSetForcedColorsPinsAndClearsTheOverride checks the override's
// lifecycle: SetForcedColors pins the state until it is cleared, and
// clearing returns the pass to the derived preference (off for a
// hostless app).
func TestSetForcedColorsPinsAndClearsTheOverride(t *testing.T) {
	app, _ := newForcedColorsFixture(t)
	if state := app.ForcedColors(); state.Active {
		t.Fatalf("the pass must start off for a hostless app, got %+v", state)
	}
	forceForcedColors(app)
	want := ForcedColorsState{Active: true, Colors: forcedColorsTestPalette()}
	if got := app.ForcedColors(); !reflect.DeepEqual(got, want) {
		t.Fatalf("App.ForcedColors() = %+v, want the pinned %+v", got, want)
	}
	app.SetForcedColors(nil)
	if state := app.ForcedColors(); state.Active {
		t.Fatalf("clearing the override must return the pass to off, got %+v", state)
	}
}

// TestForcedColorsMapsTheImageStyle checks the img element's style
// resolution: the replaced element's background and border map like the
// div's (the pass runs wherever a style is resolved).
func TestForcedColorsMapsTheImageStyle(t *testing.T) {
	app, window := newForcedColorsFixture(t)
	forceForcedColors(app)
	img := Img(NewImage(ImageFormatPNG, nil)).Flex().
		Bg(Rgb(0x2563eb).Alpha(0.25)).
		Border(Px(1)).
		BorderColor(Rgb(0x16a34a))
	var style Style
	app.Update(func(*App) {
		style = img.computeStyle(window, app)
	})
	if bg := style.Background; bg == nil || bg.Solid != Rgb(0x000000).Alpha(0.25) {
		t.Fatalf("image background = %+v, want Canvas with the authored alpha kept", bg)
	}
	if border := style.BorderColor; border == nil || border.Solid != Rgb(0xffffff) {
		t.Fatalf("image border color = %+v, want CanvasText", border)
	}
}

// BenchmarkForcedColorsInactiveGate measures the pass's inactive gate on
// the style-resolution path: the opt-out check, the cached state read
// (override check, host check, one atomic stamp load) and the early
// return — no host accessor calls, no allocation. The paired active
// benchmark shows the mapping's own cost for contrast.
func BenchmarkForcedColorsInactiveGate(b *testing.B) {
	ta := NewTestApp()
	app := ta.App()
	style := DefaultStyle()
	style.Text.Color = Some(Rgb(0xdc2626))
	background := SolidBackground(Rgb(0x2563eb))
	style.Background = &background
	app.Update(func(*App) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			applyForcedColorsToStyle(&style, app)
		}
	})
}

// BenchmarkForcedColorsActiveMapping measures the mapping with the pass
// pinned on: the colour writes (the background and decoration colours
// box fresh values) per style.
func BenchmarkForcedColorsActiveMapping(b *testing.B) {
	ta := NewTestApp()
	app := ta.App()
	forceForcedColors(app)
	style := DefaultStyle()
	style.Text.Color = Some(Rgb(0xdc2626))
	background := SolidBackground(Rgb(0x2563eb))
	style.Background = &background
	app.Update(func(*App) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			applyForcedColorsToStyle(&style, app)
		}
	})
}
