//go:build windows

package gpui

// browser-core 28, D11: rem follows the Windows text size by default
// (the per-app opt-out and the hostless spellings are pinned in
// text_scale_test.go). These pin the live host behaviour with the
// injected snapshot source — nothing here reads this machine's real
// settings: a WM_SETTINGCHANGE flip reflows the window's layout at the
// new rem, and the frame path derives the scale at most once per
// system-preference record version (ticket 29's stamp cache).

import "testing"

// hostRemProbe renders a 2-rem-wide probe.
type hostRemProbe struct{}

func (v *hostRemProbe) Render(w *Window, cx *Context[hostRemProbe]) AnyElement {
	return Div().ID("probe").DebugSelector("probe").W(DefiniteRem(2)).H(Px(20)).IntoElement()
}

// openHostRemWindow opens a real (parked off-screen) window rendering
// the 2-rem probe on the host app.
func openHostRemWindow(t *testing.T, app *App) *Window {
	t.Helper()
	window, err := app.OpenWindowView(WindowOptions{
		Title:    "gpui rem text scale",
		Bounds:   Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 240, Height: 160}},
		Show:     true,
		Activate: false,
	}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(*hostRemProbe, *Context[hostRemProbe]) {}))
	})
	if err != nil {
		t.Fatalf("OpenWindowView failed (real window expected): %v", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return window
}

// TestRemFollowsTheWindowsTextScaleLive is the acceptance: a simulated
// text-scale change reflows a live window's rem-based layout. The
// record change repaints (system_prefs_windows.go's refresh), and the
// next frame's rebuilt layout tree lays the 2-rem probe out at the new
// size — 32 px at 100%, 40 px at 125%.
func TestRemFollowsTheWindowsTextScaleLive(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	window := openHostRemWindow(t, app)

	desktopWaitFor(t, "the opening frame", func() bool { return WindowDrawCount(window) >= 1 })
	desktopWaitFor(t, "the probe bounds", func() bool {
		_, ok := WindowDebugBound(window, "probe")
		return ok
	})
	if bounds, ok := WindowDebugBound(window, "probe"); !ok || bounds.Size.Width != 32 {
		t.Fatalf("2rem probe at 100%% = %+v (ok %v), want width 32", bounds, ok)
	}
	if scale := app.TextScale(); scale != 1.0 {
		t.Fatalf("text scale = %v, want the injected 1.0", scale)
	}

	// The Windows text size flips to 125%: the WM_SETTINGCHANGE refresh
	// updates the record, repaints, and the reflowed frame lays the
	// probe out 25% wider.
	snap.textScalePercent = 125
	if r, _, _ := procSendMessageW.Call(window.Handle().Hwnd(), uintptr(wmSettingChange), uintptr(spiSetHighContrast), 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	desktopWaitFor(t, "the reflow at 1.25x", func() bool {
		bounds, ok := WindowDebugBound(window, "probe")
		return ok && bounds.Size.Width == 40
	})
	if scale := app.TextScale(); scale != 1.25 {
		t.Fatalf("text scale after the flip = %v, want 1.25", scale)
	}

	// The per-app opt-out holds the layout at the base 16 while the
	// record still reports 1.25.
	h.runForegroundSync(func() {
		app.Update(func(a *App) { a.SetRemFollowsSystemTextScale(false) })
	})
	desktopWaitFor(t, "the opt-out reflow", func() bool {
		bounds, ok := WindowDebugBound(window, "probe")
		return ok && bounds.Size.Width == 32
	})
	if scale := app.TextScale(); scale != 1.0 {
		t.Fatalf("text scale with the opt-out = %v, want 1.0", scale)
	}
}

// TestFrameTextScaleDerivesOncePerPreferenceRecord pins the cache: with
// a host, hundreds of frame draws cost ONE record derivation — the
// textScaleDerivations counter is the seam that proves no per-frame
// host accessor calls — and after a WM_SETTINGCHANGE flips the record's
// text scale, the next frame re-derives exactly once and the drawn rem
// moves with it, staying cached afterwards.
func TestFrameTextScaleDerivesOncePerPreferenceRecord(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	window := openHostRemWindow(t, app)
	desktopWaitFor(t, "the opening frame", func() bool { return WindowDrawCount(window) >= 1 })
	if got := app.textScaleDerivations; got == 0 {
		t.Fatal("the opening frame did not derive the text scale")
	}
	openingDerivations := app.textScaleDerivations

	// Hundreds of frames: no further derivation, no host accessor calls.
	const batch = 100
	for i := 0; i < batch; i++ {
		h.runForegroundSync(func() {
			if _, err := DrawWindowFrame(window); err != nil {
				t.Errorf("DrawWindowFrame: %v", err)
			}
		})
	}
	if got := app.textScaleDerivations; got != openingDerivations {
		t.Fatalf("derivations after %d frames = %d, want %d (the cache serves the frames without host reads)",
			batch, got, openingDerivations)
	}

	// The record's text scale flips: the next frame re-derives exactly
	// once (the record version moved) and the rem moves with it.
	snap.textScalePercent = 150
	if r, _, _ := procSendMessageW.Call(window.Handle().Hwnd(), uintptr(wmSettingChange), uintptr(spiSetHighContrast), 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	var rem float32
	h.runForegroundSync(func() {
		if _, err := DrawWindowFrame(window); err != nil {
			t.Errorf("DrawWindowFrame after the flip: %v", err)
		}
		ds, ok := windowDrawStates[window]
		if !ok {
			t.Fatal("the window lost its draw state")
		}
		rem = ds.rem
	})
	if rem != 24 {
		t.Fatalf("rem after the 1.5x flip = %v, want 24", rem)
	}
	if got := app.textScaleDerivations; got != openingDerivations+1 {
		t.Fatalf("derivations after the flip = %d, want %d (one re-derivation per record version)",
			got, openingDerivations+1)
	}

	// The flipped record's frames stay cached too.
	for i := 0; i < batch; i++ {
		h.runForegroundSync(func() {
			if _, err := DrawWindowFrame(window); err != nil {
				t.Errorf("DrawWindowFrame: %v", err)
			}
		})
	}
	if got := app.textScaleDerivations; got != openingDerivations+1 {
		t.Fatalf("derivations after %d more frames = %d, want %d", batch, got, openingDerivations+1)
	}
}

// TestSelectionColorsFollowTheSystemPalette pins the third clause live:
// the selection colours read the system Highlight/HighlightText, and a
// WM_SYSCOLORCHANGE palette flip moves them through the paint-path
// cache with one derivation per record version.
func TestSelectionColorsFollowTheSystemPalette(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	window := openHostRemWindow(t, app)
	desktopWaitFor(t, "the opening frame", func() bool { return WindowDrawCount(window) >= 1 })

	colors := app.SelectionColorsForPaint()
	if colors.Background != blendWithWhite(Rgb(0x0078D7)) {
		t.Fatalf("selection background = %v, want the Highlight wash of the injected palette", colors.Background)
	}
	if colors.Foreground != Rgb(0xFFFFFF) {
		t.Fatalf("selection foreground = %v, want HighlightText", colors.Foreground)
	}
	firstDerivations := app.selectionColorsDerivations
	if firstDerivations == 0 {
		t.Fatal("the first paint-path read did not derive")
	}

	// The palette flips: WM_SYSCOLORCHANGE re-reads it
	// (os_settings_provider_win.cc:288-292) and the paint-path read
	// re-derives exactly once.
	snap.sysColors.Highlight = 0x0A6E3C
	snap.sysColors.HighlightText = 0x112233
	if r, _, _ := procSendMessageW.Call(window.Handle().Hwnd(), uintptr(wmSysColorChange), 0, 0); r != 0 {
		t.Fatalf("WM_SYSCOLORCHANGE result = %d, want 0 (consumed)", r)
	}
	colors = app.SelectionColorsForPaint()
	if colors.Background != blendWithWhite(Rgb(0x0A6E3C)) {
		t.Fatalf("selection background after the flip = %v, want the new Highlight wash", colors.Background)
	}
	if colors.Foreground != Rgb(0x112233) {
		t.Fatalf("selection foreground after the flip = %v, want the new HighlightText", colors.Foreground)
	}
	if got := app.selectionColorsDerivations; got != firstDerivations+1 {
		t.Fatalf("derivations after the palette flip = %d, want %d", got, firstDerivations+1)
	}
	// The read stays cached at this record version.
	if _ = app.SelectionColorsForPaint(); app.selectionColorsDerivations != firstDerivations+1 {
		t.Fatalf("the flipped record's reads did not stay cached")
	}
}
