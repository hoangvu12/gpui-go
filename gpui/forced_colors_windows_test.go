//go:build windows

package gpui

// The host-driven half of ticket 29's acceptance: the forced-colours
// pass is preference-driven, so it follows the live system-preference
// record the way ticket 27's machinery refreshes it (an injected
// snapshot source, a real window parked off screen, and the
// WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST) message Chromium treats as the
// forced-colours trigger). The style-level mapping tests live in
// forced_colors_test.go.

import (
	"testing"
	"time"
)

// TestForcedColorsFollowsSystemPreferences checks the pass's switch on
// a host: with the injected record reporting no high contrast the pass
// is off, and after the message flips the record into forced colours
// (the same flip TestSystemPreferencesAppObserverRouting drives) the
// app answers an active pass with the injected palette.
func TestForcedColorsFollowsSystemPreferences(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}

	handle, err := h.OpenWindow(WindowOptions{
		Title:    "gpui forced colors",
		Bounds:   Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 240, Height: 160}},
		Show:     true,
		Activate: false,
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}

	// High contrast off: the pass is off (the acceptance rule).
	if state := app.ForcedColors(); state.Active {
		t.Fatalf("App.ForcedColors() = %+v, want inactive while the system reports no forced colours", state)
	}

	// WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST) flips the record into a
	// forced-colours session (os_settings_provider_win.cc:293-298): the
	// pass turns on with the injected palette, with no override set.
	snap.highContrast = true
	snap.sysColors.Window = 0x000000
	snap.sysColors.WindowText = 0xFFFFFF
	if r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), uintptr(spiSetHighContrast), 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if state := app.ForcedColors(); state.Active {
			if state.Colors.Canvas != Rgb(0x000000) || state.Colors.CanvasText != Rgb(0xFFFFFF) {
				t.Fatalf("the active pass's palette = Canvas %v CanvasText %v, want the injected forced palette",
					state.Colors.Canvas, state.Colors.CanvasText)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the pass did not follow the record into forced colours")
}

// TestForcedColorsStylePathDerivesOncePerPreferenceRecord checks the
// style-resolution path's cache (the perf half of the pass): with a host
// reporting no forced colours, hundreds of style computations cost ONE
// record derivation — the forcedColorsDerivations counter is the seam
// that proves no per-element host accessor calls — and after a
// WM_SETTINGCHANGE flips the record into forced colours, the next style
// resolution re-derives exactly once and the styles flip with it (the
// liveness half), staying cached afterwards.
func TestForcedColorsStylePathDerivesOncePerPreferenceRecord(t *testing.T) {
	snap := defaultSystemSettingsSnapshot()
	origProvider := systemSettingsSnapshotProvider
	systemSettingsSnapshotProvider = func() systemSettingsSnapshot { return snap }
	t.Cleanup(func() { systemSettingsSnapshotProvider = origProvider })

	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	window, err := app.OpenWindow(WindowOptions{
		Title:    "gpui forced colors cache",
		Bounds:   Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 240, Height: 160}},
		Show:     true,
		Activate: false,
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}

	div := func() *DivElement {
		return Div().TextColor(Rgb(0xdc2626)).Bg(Rgb(0x2563eb))
	}
	styleOf := func() Style {
		var style Style
		app.Update(func(*App) {
			style = div().computeStyleInternal(nil, nil, window, app)
		})
		return style
	}

	// Hundreds of unforced styles: one derivation (the first read seeds
	// the cache at the host's seeded record version), the rest read the
	// cache with no host accessor calls.
	const batch = 200
	app.Update(func(*App) {
		for i := 0; i < batch; i++ {
			div().computeStyleInternal(nil, nil, window, app)
		}
	})
	if got := app.forcedColorsDerivations; got != 1 {
		t.Fatalf("derivations after %d unforced styles = %d, want 1 (the cache serves the rest without host reads)", batch, got)
	}
	if style := styleOf(); !style.Text.Color.IsSome() || style.Text.Color != Some(Rgb(0xdc2626)) {
		t.Fatalf("the unforced style's text colour = %v, want the authored red", style.Text.Color)
	}

	// WM_SETTINGCHANGE(SPI_SETHIGHCONTRAST) flips the record into a
	// forced-colours session: the public read flips, and the next style
	// resolution re-derives exactly once (the record version moved).
	snap.highContrast = true
	snap.sysColors.Window = 0x000000
	snap.sysColors.WindowText = 0xFFFFFF
	if r, _, _ := procSendMessageW.Call(window.Handle().Hwnd(), uintptr(wmSettingChange), uintptr(spiSetHighContrast), 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if app.ForcedColors().Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !app.ForcedColors().Active {
		t.Fatal("the public record did not flip into forced colours")
	}
	style := styleOf()
	if color, ok := style.Text.Color.Get(); !ok || color != Rgb(0xFFFFFF) {
		t.Fatalf("the style after the flip: text colour = %v (present %v), want the forced CanvasText (white)", color, ok)
	}
	if bg := style.Background; bg == nil || bg.Solid != Rgb(0x000000) {
		t.Fatalf("the style after the flip: background = %+v, want the forced Canvas (black)", bg)
	}
	if got := app.forcedColorsDerivations; got != 2 {
		t.Fatalf("derivations after the flip = %d, want 2 (one re-derivation per record version)", got)
	}

	// The flipped record's styles stay cached too.
	app.Update(func(*App) {
		for i := 0; i < batch; i++ {
			div().computeStyleInternal(nil, nil, window, app)
		}
	})
	if got := app.forcedColorsDerivations; got != 2 {
		t.Fatalf("derivations after %d more forced styles = %d, want 2 (the cache serves them)", batch, got)
	}
}
