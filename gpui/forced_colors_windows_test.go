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
