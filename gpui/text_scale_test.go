package gpui

import "testing"

// browser-core 28 (D11): rem follows the Windows text size by default
// with a per-app opt-out; the caret-blink and selection reads are in
// system_prefs_test-shaped files and the base package. These are the
// hostless spellings — a test app answers the default record (1.0, the
// 500 ms blink, the classic light palette), so nothing here depends on
// this machine's settings; the live host behaviour is pinned in
// text_scale_windows_test.go.

func TestTextScaleDefaultsToTheSystemRecord(t *testing.T) {
	ta := NewTestApp()
	defer ta.App().Shutdown()
	if scale := ta.App().TextScale(); scale != 1.0 {
		t.Fatalf("default text scale = %v, want 1.0 (the default record)", scale)
	}
	if rem := ta.App().frameRemSize(); rem != DefaultRemSize {
		t.Fatalf("default frame rem = %v, want the base %v", rem, DefaultRemSize)
	}
}

func TestRemFollowsThePinnedTextScale(t *testing.T) {
	ta := NewTestApp()
	defer ta.App().Shutdown()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	setRootForTest(t, w, app)

	// The default: rems resolve against 16.
	DrawWindowFrame(w)
	if rem := testWindowRemOf(t, w); rem != 16 {
		t.Fatalf("rem at the default scale = %v, want 16", rem)
	}
	if bounds, ok := WindowDebugBound(w, "probe"); !ok || bounds.Size.Width != 32 {
		t.Fatalf("2rem probe at the default scale = %+v (ok %v), want width 32", bounds, ok)
	}

	// A 1.25x text scale reflows the same view: the pin is the
	// deterministic spelling of the Windows text size.
	scale := float32(1.25)
	app.Update(func(*App) { app.SetTextScale(&scale) })
	DrawWindowFrame(w)
	if rem := testWindowRemOf(t, w); rem != 20 {
		t.Fatalf("rem at 1.25x = %v, want 20", rem)
	}
	if bounds, ok := WindowDebugBound(w, "probe"); !ok || bounds.Size.Width != 40 {
		t.Fatalf("2rem probe at 1.25x = %+v (ok %v), want width 40", bounds, ok)
	}

	// Clearing the pin returns to the system record.
	app.Update(func(*App) { app.SetTextScale(nil) })
	DrawWindowFrame(w)
	if rem := testWindowRemOf(t, w); rem != 16 {
		t.Fatalf("rem after clearing the pin = %v, want 16", rem)
	}
}

func TestRemOptOutHoldsTheBaseRem(t *testing.T) {
	ta := NewTestApp()
	defer ta.App().Shutdown()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	setRootForTest(t, w, app)

	scale := float32(1.5)
	app.Update(func(a *App) {
		a.SetTextScale(&scale)
		a.SetRemFollowsSystemTextScale(false)
	})
	DrawWindowFrame(w)
	if scale := app.TextScale(); scale != 1.5 {
		t.Fatalf("effective scale with the pin set = %v, want the pinned 1.5 (the pin wins)", scale)
	}
	if rem := testWindowRemOf(t, w); rem != 24 {
		t.Fatalf("rem with the pin at 1.5 = %v, want 24", rem)
	}

	// The opt-out on its own: the system record is ignored, rem holds
	// the base 16.
	app.Update(func(a *App) { a.SetTextScale(nil) })
	if scale := app.TextScale(); scale != 1.0 {
		t.Fatalf("effective scale with the opt-out = %v, want 1.0", scale)
	}
	DrawWindowFrame(w)
	if rem := testWindowRemOf(t, w); rem != 16 {
		t.Fatalf("rem with the opt-out = %v, want 16 (held at the base)", rem)
	}

	app.Update(func(a *App) { a.SetRemFollowsSystemTextScale(true) })
	DrawWindowFrame(w)
	if rem := testWindowRemOf(t, w); rem != 16 {
		t.Fatalf("rem after re-enabling = %v, want 16 (the hostless record is 1.0)", rem)
	}
}

// testWindowRemOf reads the test window's rem through the diagnostics
// accessor.
func testWindowRemOf(t *testing.T, w *Window) float32 {
	t.Helper()
	_, _, rem, err := TestWindowDrawGeometry(w)
	if err != nil {
		t.Fatalf("TestWindowDrawGeometry: %v", err)
	}
	return rem
}

// setRootForTest installs a view whose probe div is 2 rem wide.
func setRootForTest(t *testing.T, w *Window, app *App) {
	t.Helper()
	host := NewEntity(app, w.Scope(), func(*remProbeView, *Context[remProbeView]) {})
	w.SetRootView(ViewOf(host))
}

// remProbeView renders a 2-rem-wide probe.
type remProbeView struct{}

func (v *remProbeView) Render(w *Window, cx *Context[remProbeView]) AnyElement {
	return Div().ID("probe").DebugSelector("probe").W(DefiniteRem(2)).H(Px(20)).IntoElement()
}

func TestFrameRemSizeNeverCallsTheHostlessRecordPerElement(t *testing.T) {
	// The hostless path answers the default record directly (no host,
	// no stamp): repeated frame reads stay at the same value and the
	// cache fields are untouched.
	ta := NewTestApp()
	defer ta.App().Shutdown()
	app := ta.App()
	for i := 0; i < 100; i++ {
		if rem := app.frameRemSize(); rem != DefaultRemSize {
			t.Fatalf("frame rem read %d = %v, want the base", i, rem)
		}
	}
	if app.textScaleDerivations != 0 {
		t.Fatalf("hostless derivations = %d, want 0 (no host, no stamp path)", app.textScaleDerivations)
	}
}

func TestSetTextScalePinAndClear(t *testing.T) {
	ta := NewTestApp()
	defer ta.App().Shutdown()
	app := ta.App()

	scale := float32(1.1)
	app.Update(func(a *App) {
		a.SetTextScale(&scale)
	})
	if got := app.TextScale(); got != 1.1 {
		t.Fatalf("pinned scale = %v, want 1.1", got)
	}
	app.Update(func(a *App) {
		a.SetTextScale(nil)
	})
	if got := app.TextScale(); got != 1.0 {
		t.Fatalf("cleared scale = %v, want the system 1.0", got)
	}
}

// The selection colours (ticket 28's third clause) on the default
// record: the Highlight system colour in its blend-with-white wash with
// HighlightText as the foreground.

func TestSelectionColorsUseTheSystemHighlight(t *testing.T) {
	ta := NewTestApp()
	defer ta.App().Shutdown()
	app := ta.App()
	colors := app.SelectionColors()
	highlight := app.SystemColors().Highlight
	if colors.Background != blendWithWhite(highlight) {
		t.Fatalf("background = %v, want the Highlight wash %v", colors.Background, blendWithWhite(highlight))
	}
	if colors.Foreground != app.SystemColors().HighlightText {
		t.Fatalf("foreground = %v, want HighlightText", colors.Foreground)
	}
	// The default record's Highlight (0x0078D7, opaque) resolves to a
	// translucent wash the text reads through (Chromium's
	// BlendWithWhite intent, color.cc:1296-1298).
	if !(colors.Background.A > 0 && colors.Background.A < 1) {
		t.Fatalf("background alpha = %v, want a translucent wash", colors.Background.A)
	}
	// The paint-path read agrees with the fresh one without a host.
	if paint := app.SelectionColorsForPaint(); paint != colors {
		t.Fatalf("paint-path colours = %+v, want %+v", paint, colors)
	}
}

func TestBlendWithWhiteMatchesThePinnedArithmetic(t *testing.T) {
	// An already-translucent colour is returned unchanged (color.cc:
	// 1304-1307).
	wash := Rgb(0x55a0fc).Alpha(0.3)
	if got := blendWithWhite(wash); got != wash {
		t.Fatalf("translucent input = %v, want it unchanged", got)
	}

	// White: every component stays non-negative at the first step
	// (alpha 153), so the ladder stops there. The components come out
	// 254, not 255: BlendComponent's int/float32 division truncates
	// (153/0.6f = 254.9999…), and the port uses the same float32
	// arithmetic as color.cc:73-80.
	if got, want := blendWithWhite(Rgb(0xFFFFFF)), RgbaToHsla(0xFEFEFE99); got != want {
		t.Fatalf("white = %v, want the first-step blend at alpha 153 (%v)", got, want)
	}
	rgba := hslaToRgba(blendWithWhite(Rgb(0xFFFFFF)))
	if abs32(rgba.A*255-153) > 0.5 {
		t.Fatalf("white blend alpha = %v, want 153 (the ladder starts there)", rgba.A*255)
	}

	// The classic Highlight 0x0078D7: every step's red goes negative
	// (0 - (255 - a)), so the ladder runs to its last step and the
	// clamped port paints a dark-enough wash at alpha 204.
	got := hslaToRgba(blendWithWhite(Rgb(0x0078D7)))
	if abs32(got.A*255-204) > 0.5 {
		t.Fatalf("0x0078D7 blend alpha = %v, want 204 (the ladder's end)", got.A*255)
	}
	if got.R != 0 {
		t.Fatalf("0x0078D7 blend red = %v, want the clamped 0", got.R)
	}
	// Green 120: (120-51)/0.8 -> 86; blue 215: (215-51)/0.8 -> 205.
	if abs32(got.G*255-86) > 1 || abs32(got.B*255-205) > 1 {
		t.Fatalf("0x0078D7 blend = (%v, %v), want about (86, 205)", got.G*255, got.B*255)
	}
}
