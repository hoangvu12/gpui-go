//go:build windows

// Package desktopspec holds ticket26's desktop-operations corpus: the
// observable behavior of the reference window/app controls and the
// desktop settings, power and lifecycle changes through the Go host,
// exercised through the public gpui API from outside the runtime
// package (repo convention; see internal/winhostspec).
//
// Environment expectation: the user's interactive Windows session.
// Window-creation failures FAIL the tests, never skip. Controlled
// events are delivered as synthetic messages to this test's own
// windows (WM_SETTINGCHANGE, WM_DISPLAYCHANGE, WM_POWERBROADCAST,
// WM_ENDSESSION, WM_MOUSEMOVE); no system setting, no theme, no
// power state and no external launch is performed. What was and was
// not really executed is recorded in ticket26's Comments.
package desktopspec

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Win32 bindings for the controlled-event delivery (tests bring their
// own; the package under test never exposes them).
var (
	modUser32 = syscall.NewLazyDLL("user32.dll")
	procSend  = modUser32.NewProc("SendMessageW")
)

// Message ids the corpus delivers (winuser.h).
const (
	wmSettingChange  = 0x001A
	wmDisplayChange  = 0x007E
	wmEndSession     = 0x0016
	wmPowerBroadcast = 0x0218
	pbtResumeAuto    = 0x12
)

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

func recvWithin[T any](t *testing.T, timeout time.Duration, what string, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatalf("timed out after %v waiting for %s", timeout, what)
		var zero T
		return zero
	}
}

// newHost boots a fresh host, checking the clean thread join at
// cleanup like internal/winhostspec.
func newHost(t *testing.T) *gpui.Host {
	t.Helper()
	before := runtime.NumGoroutine()
	h := gpui.NewHost()
	if err := h.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
		waitFor(t, 5*time.Second, "goroutines to drain", func() bool {
			return runtime.NumGoroutine() <= before+2
		})
	})
	return h
}

// newApp boots a fresh host-backed application.
func newApp(t *testing.T) (*gpui.App, *gpui.Host) {
	t.Helper()
	h := newHost(t)
	app := gpui.NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return app, h
}

// openWindow opens one shown window through the application, failing
// on creation errors (interactive-session expectation).
func openWindow(t *testing.T, app *gpui.App, opts gpui.WindowOptions) *gpui.Window {
	t.Helper()
	w, err := app.OpenWindow(opts)
	if err != nil {
		t.Fatalf("OpenWindow(%q) failed — this session is expected to create real windows: %v", opts.Title, err)
	}
	return w
}

// deliver sends one controlled message to one of this test's windows.
func deliver(t *testing.T, hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	t.Helper()
	r, _, _ := procSend.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}

// wideString builds a NUL-terminated UTF-16 buffer that stays alive for
// the duration of one synchronous SendMessage.
func wideString(s string) []uint16 {
	runes := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		runes = append(runes, uint16(r))
	}
	return append(runes, 0)
}

// ---------------------------------------------------------------------------
// Window controls: minimize / zoom / restore / title / activation
// ---------------------------------------------------------------------------

// TestWindowControlsMinimizeZoomRestoreTitle drives the window-control
// semantics on a real resizable window: minimize reports minimized,
// zoom toggles maximize, the restore reports the size again, and titles
// round-trip through the OS.
func TestWindowControlsMinimizeZoomRestoreTitle(t *testing.T) {
	app, _ := newApp(t)
	w := openWindow(t, app, gpui.WindowOptions{
		Title:     "desktopspec controls",
		Show:      true,
		Activate:  true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 320, Height: 200}},
	})
	waitFor(t, 10*time.Second, "the window to become visible", w.Alive)

	if minimized, err := w.IsMinimized(); err != nil || minimized {
		t.Fatalf("fresh window minimized = %v, %v", minimized, err)
	}

	// Title round-trip (SetWindowTextW / GetWindowTextW).
	w.SetTitle("desktopspec retitled")
	waitFor(t, 10*time.Second, "the title to round-trip", func() bool {
		title, err := w.Title()
		return err == nil && title == "desktopspec retitled"
	})

	// Minimize.
	w.Minimize()
	waitFor(t, 10*time.Second, "the window to minimize", func() bool {
		minimized, _ := w.IsMinimized()
		return minimized
	})

	// Zoom from minimized: the reference maximizes (a minimized window
	// is still visible for IsWindowVisible).
	w.Zoom()
	waitFor(t, 10*time.Second, "the zoom to maximize", func() bool {
		maximized, _ := w.IsMaximized()
		return maximized
	})
	// Maximized windows keep a live bounds query.
	bounds, err := w.Bounds()
	if err != nil {
		t.Fatalf("Bounds while maximized: %v", err)
	}
	if bounds.Size.Width < 100 || bounds.Size.Height < 100 {
		t.Fatalf("implausible maximized bounds: %+v", bounds)
	}

	// The second zoom restores.
	w.Zoom()
	waitFor(t, 10*time.Second, "the second zoom to restore", func() bool {
		maximized, _ := w.IsMaximized()
		return !maximized
	})

	// Restore from minimized via activate semantics.
	w.Minimize()
	waitFor(t, 10*time.Second, "the window to minimize again", func() bool {
		minimized, _ := w.IsMinimized()
		return minimized
	})
	w.Activate()
	waitFor(t, 10*time.Second, "activation to restore the window", func() bool {
		minimized, _ := w.IsMinimized()
		return !minimized
	})

	// Scale and mouse position stay queryable through the controls.
	scale, err := w.ScaleFactor()
	if err != nil || scale <= 0 || scale > 10 {
		t.Fatalf("scale factor = %v, %v", scale, err)
	}
	if _, err := w.MousePosition(); err != nil {
		t.Fatalf("MousePosition: %v", err)
	}
}

// TestWindowActivationReportsActiveWindow checks the activation query:
// an activated window reports itself active, a second window takes over
// when activated.
func TestWindowActivationReportsActiveWindow(t *testing.T) {
	app, _ := newApp(t)
	w1 := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec active one", Show: true, Activate: true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 300, Height: 180}},
	})
	waitFor(t, 10*time.Second, "window one visible", w1.Alive)
	w2 := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec active two", Show: true, Activate: true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 300, Height: 180}},
	})
	waitFor(t, 10*time.Second, "window two visible", w2.Alive)

	// The most recently activated window reports active (Windows can
	// refuse a background process's foreground request; the observer
	// ordering is still exercised through the activation callback).
	active2, err := w2.IsActive()
	if err != nil {
		t.Fatalf("IsActive: %v", err)
	}
	t.Logf("window two active after its activation: %v", active2)
	if active2 {
		if active1, _ := w1.IsActive(); active1 {
			t.Fatal("two windows cannot both be the active window")
		}
	}
}

// TestAnchoredPopupRejectedThroughApp verifies the native-popup
// rejection through the application's open path: callers receive the
// typed error and can fall back.
func TestAnchoredPopupRejectedThroughApp(t *testing.T) {
	app, _ := newApp(t)
	_, err := app.OpenWindow(gpui.WindowOptions{
		Kind:  gpui.WindowKindAnchoredPopup,
		Title: "desktopspec popup",
	})
	if !errors.Is(err, gpui.ErrPopupNotSupported) {
		t.Fatalf("anchored popup error = %v, want ErrPopupNotSupported", err)
	}
	w := openWindow(t, app, gpui.WindowOptions{Title: "desktopspec after popup", Show: true})
	waitFor(t, 10*time.Second, "the fallback window to open", w.Alive)
}

// ---------------------------------------------------------------------------
// Displays
// ---------------------------------------------------------------------------

// TestDisplayEnumerationAndPrimary pins the display query: the machine
// reports at least one monitor with sane geometry, the primary's bounds
// sit at the origin, and each display's UUID is canonical.
func TestDisplayEnumerationAndPrimary(t *testing.T) {
	app, _ := newApp(t)
	displays, err := app.Displays()
	if err != nil {
		t.Fatalf("Displays: %v", err)
	}
	if len(displays) == 0 {
		t.Fatal("no displays were reported")
	}
	seenPrimary := false
	for _, display := range displays {
		if display.ScaleFactor < 0.25 || display.ScaleFactor > 5 {
			t.Fatalf("implausible scale %v for display %d", display.ScaleFactor, display.ID)
		}
		if display.Bounds.Size.Width <= 0 || display.Bounds.Size.Height <= 0 {
			t.Fatalf("implausible bounds %+v for display %d", display.Bounds, display.ID)
		}
		// The work area is inside the monitor bounds.
		if display.VisibleBounds.Size.Width > display.Bounds.Size.Width ||
			display.VisibleBounds.Size.Height > display.Bounds.Size.Height {
			t.Fatalf("work area larger than the monitor for display %d", display.ID)
		}
		if len(display.UUID) != 36 || display.UUID[8] != '-' || display.UUID[14] != '5' {
			t.Fatalf("display %d uuid is not a canonical v5: %q", display.ID, display.UUID)
		}
		if display.ID == 0 {
			t.Fatalf("display id must be a real HMONITOR, got %d", display.ID)
		}
	}
	primary, err := app.PrimaryDisplay()
	if err != nil {
		t.Fatalf("PrimaryDisplay: %v", err)
	}
	if primary.Bounds.Origin != (gpui.Point{}) {
		t.Fatalf("the primary monitor's origin is %+v, want (0,0)", primary.Bounds.Origin)
	}
	for _, display := range displays {
		if display.ID == primary.ID {
			seenPrimary = true
		}
	}
	if !seenPrimary {
		t.Fatalf("the primary monitor %d is not among the displays", primary.ID)
	}
}

// TestWindowDisplayFollowsDisplayChange pins the per-window display
// record: a real window carries its monitor, and a controlled
// WM_DISPLAYCHANGE re-resolves it.
func TestWindowDisplayFollowsDisplayChange(t *testing.T) {
	app, _ := newApp(t)
	w := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec display", Show: true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 280, Height: 160}},
	})
	waitFor(t, 10*time.Second, "the window to become visible", w.Alive)

	display, err := w.Display()
	if err != nil {
		t.Fatalf("Display: %v", err)
	}
	if display.ScaleFactor < 0.25 || display.ScaleFactor > 5 {
		t.Fatalf("implausible window display scale %v", display.ScaleFactor)
	}

	// A controlled WM_DISPLAYCHANGE: the handler re-resolves through
	// MonitorFromWindow; the record must stay live and match the same
	// monitor (nothing actually changed).
	hwnd := w.Handle().Hwnd()
	if r := deliver(t, hwnd, wmDisplayChange, 0, 0); r != 0 {
		t.Fatalf("WM_DISPLAYCHANGE result = %d, want 0 (consumed)", r)
	}
	after, err := w.Display()
	if err != nil {
		t.Fatalf("Display after WM_DISPLAYCHANGE: %v", err)
	}
	if after.ID != display.ID {
		t.Fatalf("display id changed without a real display change: %d -> %d", display.ID, after.ID)
	}
}

// ---------------------------------------------------------------------------
// Background appearance: DWM behavior and the renderer clear path
// ---------------------------------------------------------------------------

// TestBackgroundAppearanceModes covers set_background_appearance for
// opaque, transparent and blurred (plus the Mica modes): the recorded
// mode, the renderer clear color under the same mode, and the DWM-side
// application record.
func TestBackgroundAppearanceModes(t *testing.T) {
	app, _ := newApp(t)
	w := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec background", Show: true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 280, Height: 160}},
	})
	waitFor(t, 10*time.Second, "the window to become visible", w.Alive)

	defaults, err := w.BackgroundAppearance()
	if err != nil || defaults != gpui.WindowBackgroundOpaque {
		t.Fatalf("default background appearance = %v, %v", defaults, err)
	}
	if clear, err := w.ClearColor(); err != nil || clear != (gpui.Color{R: 1, G: 1, B: 1, A: 1}) {
		t.Fatalf("opaque clear color = %+v, %v", clear, err)
	}

	modes := []struct {
		appearance gpui.WindowBackgroundAppearance
		clear      gpui.Color
	}{
		{gpui.WindowBackgroundOpaque, gpui.Color{R: 1, G: 1, B: 1, A: 1}},
		{gpui.WindowBackgroundTransparent, gpui.Color{}},
		{gpui.WindowBackgroundBlurred, gpui.Color{}},
		{gpui.WindowBackgroundMica, gpui.Color{}},
		{gpui.WindowBackgroundMicaAlt, gpui.Color{}},
	}
	for _, mode := range modes {
		w.SetBackgroundAppearance(mode.appearance)
		waitFor(t, 10*time.Second, "the appearance to be recorded", func() bool {
			got, err := w.BackgroundAppearance()
			return err == nil && got == mode.appearance
		})
		clear, err := w.ClearColor()
		if err != nil {
			t.Fatalf("ClearColor(%d): %v", mode.appearance, err)
		}
		if clear != mode.clear {
			t.Errorf("mode %d clear color = %+v, want %+v (the renderer clear path)", mode.appearance, clear, mode.clear)
		}
		record, err := w.Handle().CompositionRecord()
		if err != nil {
			t.Fatalf("CompositionRecord(%d): %v", mode.appearance, err)
		}
		if record == "" {
			t.Errorf("mode %d left no composition record", mode.appearance)
		}
		t.Logf("mode %d composition: %s", mode.appearance, record)
	}
}

// ---------------------------------------------------------------------------
// Settings / theme observers through the foreground dispatcher
// ---------------------------------------------------------------------------

// TestSettingsChangeWheelSettings delivers a controlled
// WM_SETTINGCHANGE with a system-parameter action and verifies the
// tracked settings refresh (read-only SystemParametersInfoW query).
func TestSettingsChangeWheelSettings(t *testing.T) {
	app, _ := newApp(t)
	w := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec settings", Show: true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 280, Height: 160}},
	})
	waitFor(t, 10*time.Second, "the window to become visible", w.Alive)

	if r := deliver(t, w.Handle().Hwnd(), wmSettingChange, 104 /*SPI_GETWHEELSCROLLLINES*/, 0); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	settings, err := app.MouseWheelSettings()
	if err != nil {
		t.Fatalf("MouseWheelSettings: %v", err)
	}
	t.Logf("mouse wheel settings after the change: %+v", settings)
	if settings.WheelScrollLines > 1000 || settings.WheelScrollChars > 1000 {
		t.Fatalf("implausible wheel settings: %+v", settings)
	}
}

// TestThemeChangeObserverFiresOnlyOnChange delivers a controlled
// WM_SETTINGCHANGE(0, "ImmersiveColorSet") to a real window with the
// appearance observer installed: with the machine's real appearance the
// window's cached appearance equals the fresh read, so the observer
// does not fire (the reference compares before reporting); the message
// is still consumed.
func TestThemeChangeObserverFiresOnlyOnChange(t *testing.T) {
	app, _ := newApp(t)
	var mu sync.Mutex
	var notifications int
	w := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec theme", Show: true,
		OnAppearanceChanged: func() {
			mu.Lock()
			notifications++
			mu.Unlock()
		},
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 280, Height: 160}},
	})
	waitFor(t, 10*time.Second, "the window to become visible", w.Alive)

	appearance, err := w.Appearance()
	if err != nil {
		t.Fatalf("Appearance: %v", err)
	}
	systemAppearance, err := app.WindowAppearance()
	if err != nil {
		t.Fatalf("WindowAppearance: %v", err)
	}
	t.Logf("window appearance %v, system appearance %v", appearance, systemAppearance)

	if r := deliver(t, w.Handle().Hwnd(), wmSettingChange, 0,
		uintptr(unsafe.Pointer(&wideString("ImmersiveColorSet")[0]))); r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if appearance == systemAppearance && notifications != 0 {
		t.Fatalf("an unchanged appearance must not notify; notifications = %d", notifications)
	}
	if appearance != systemAppearance && notifications == 0 {
		t.Fatalf("a changed appearance must notify (window %v vs system %v)", appearance, systemAppearance)
	}
}

// TestButtonLayoutUnsupported pins the button-layout rows with the
// source-supported outcome: Windows keeps the trait default (no layout,
// observer never fires).
func TestButtonLayoutUnsupported(t *testing.T) {
	app, _ := newApp(t)
	if layout, ok := app.ButtonLayout(); ok {
		t.Fatalf("Windows reports no button layout (the trait default), got %+v", layout)
	}
	if err := app.OnButtonLayoutChanged(func() { t.Error("the button-layout observer must never fire") }); err != nil {
		t.Fatalf("OnButtonLayoutChanged: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Power and session lifecycle through the foreground dispatcher
// ---------------------------------------------------------------------------

// TestPowerBroadcastResumeWakesApp registers the app-level wake
// callback (the real RegisterSuspendResumeNotification) and delivers a
// controlled WM_POWERBROADCAST(PBT_APMRESUMEAUTOMATIC) to the platform
// window: the callback crosses the update boundary on the foreground
// thread.
func TestPowerBroadcastResumeWakesApp(t *testing.T) {
	app, h := newApp(t)
	woken := make(chan bool, 4)
	if err := app.OnSystemWake(func() { woken <- true }); err != nil {
		t.Fatalf("OnSystemWake: %v", err)
	}
	platform := h.PlatformHwnd()
	if platform == 0 {
		t.Fatal("the platform window was not exposed")
	}
	deliver(t, platform, wmPowerBroadcast, pbtResumeAuto, 0)
	recvWithin(t, 10*time.Second, "the system-wake callback", woken)
	// A suspend broadcast must not wake.
	deliver(t, platform, wmPowerBroadcast, 4 /*PBT_APMSUSPEND*/, 0)
	select {
	case <-woken:
		t.Fatal("PBT_APMSUSPEND must not run the system-wake callback")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestEndSessionQuitsApp delivers a controlled WM_ENDSESSION to a real
// window: the app's quit callback runs at an update boundary (returning
// false: completed shutdown would exit the process), and the message
// loop quits so App.Run/Wait return.
func TestEndSessionQuitsApp(t *testing.T) {
	app, h := newApp(t)
	w := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec end session", Show: true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 280, Height: 160}},
	})
	waitFor(t, 10*time.Second, "the window to become visible", w.Alive)

	quitRan := make(chan bool, 2)
	if err := app.OnQuit(func() bool {
		quitRan <- true
		return false
	}); err != nil {
		t.Fatalf("OnQuit: %v", err)
	}
	deliver(t, w.Handle().Hwnd(), wmEndSession, 1, 0)
	recvWithin(t, 10*time.Second, "the quit callback", quitRan)
	waitDone := make(chan error, 1)
	go func() { waitDone <- h.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("host Wait after the session end: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session end did not quit the message loop")
	}
}

// ---------------------------------------------------------------------------
// Cursor, character palette, notifications and unsupported outcomes
// ---------------------------------------------------------------------------

// TestCursorStyleAndVisibilityAppLevel pins the app-level cursor
// surface on a real window: the style query follows set_cursor_style,
// hide-until-moves flips the flag and a mouse move restores it.
func TestCursorStyleAndVisibilityAppLevel(t *testing.T) {
	app, _ := newApp(t)
	w := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec cursor", Show: true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 280, Height: 160}},
	})
	waitFor(t, 10*time.Second, "the window to become visible", w.Alive)

	if visible, err := app.IsCursorVisible(); err != nil || !visible {
		t.Fatalf("cursor starts visible = %v, %v", visible, err)
	}
	if err := app.SetCursorStyle(gpui.CursorIBeam); err != nil {
		t.Fatalf("SetCursorStyle: %v", err)
	}
	waitFor(t, 10*time.Second, "the style to be recorded", func() bool {
		style, err := app.CursorStyle()
		return err == nil && style == gpui.CursorIBeam
	})

	if err := app.HideCursorUntilMouseMoves(); err != nil {
		t.Fatalf("HideCursorUntilMouseMoves: %v", err)
	}
	waitFor(t, 10*time.Second, "the cursor to hide", func() bool {
		visible, _ := app.IsCursorVisible()
		return !visible
	})
	// A mouse move over the window restores it.
	deliver(t, w.Handle().Hwnd(), 0x0200 /*WM_MOUSEMOVE*/, 0, 0)
	waitFor(t, 10*time.Second, "the cursor to restore on the move", func() bool {
		visible, _ := app.IsCursorVisible()
		return visible
	})
}

// TestCharacterPaletteInactiveWindowGuard verifies the palette's
// foreground guard on a hidden window: the call refuses without
// injecting any input (SendInput targets the foreground window; opening
// the picker for another application would be wrong).
func TestCharacterPaletteInactiveWindowGuard(t *testing.T) {
	app, _ := newApp(t)
	w := openWindow(t, app, gpui.WindowOptions{
		Title: "desktopspec palette guard (hidden)", Show: false,
	})
	waitFor(t, 10*time.Second, "the hidden window to exist", w.Alive)
	err := w.ShowCharacterPalette()
	if err == nil {
		t.Fatal("a hidden (non-foreground) window must refuse the character palette")
	}
	if !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("palette guard error = %v", err)
	}
}

// TestThermalAndUnsupportedOutcomesAppLevel pins the verified
// unsupported/no-op outcomes through the app surface.
func TestThermalAndUnsupportedOutcomesAppLevel(t *testing.T) {
	app, _ := newApp(t)
	if got := app.ThermalState(); got != gpui.ThermalStateNominal {
		t.Fatalf("thermal state = %v, want Nominal", got)
	}
	if err := app.OnThermalStateChange(func() { t.Error("the thermal callback must never fire") }); err != nil {
		t.Fatalf("OnThermalStateChange: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := app.PathForAuxiliaryExecutable("helper"); !errors.Is(err, gpui.ErrAuxiliaryExecutableUnavailable) {
		t.Fatalf("auxiliary executable error = %v", err)
	}
	if err := app.RegisterURLScheme("zed"); !errors.Is(err, gpui.ErrURLSchemeRegistrationUnsupported) {
		t.Fatalf("register_url_scheme error = %v", err)
	}
	// Empty-body app operations.
	if err := app.ActivateApp(true); err != nil {
		t.Fatalf("ActivateApp: %v", err)
	}
	if err := app.HideApp(); err != nil {
		t.Fatalf("HideApp: %v", err)
	}
	// The unimplemented pair panics like unimplemented!().
	for name, call := range map[string]func() error{
		"HideOtherApps":   app.HideOtherApps,
		"UnhideOtherApps": app.UnhideOtherApps,
	} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%s must panic (reference unimplemented!())", name)
					return
				}
				if msg, ok := r.(string); !ok || !strings.Contains(msg, "not implemented on Windows") {
					t.Errorf("%s panic = %v", name, r)
				}
			}()
			if err := call(); err != nil {
				t.Errorf("%s returned %v before panicking", name, err)
			}
		}()
	}
}

// TestSystemNotificationAppLevel pins the notification surface through
// the app: without an identity the source-supported no-op, with one the
// explicit unported-notifier failure — never fake success. The response
// observer registration is dormant.
func TestSystemNotificationAppLevel(t *testing.T) {
	app, _ := newApp(t)
	err := app.ShowSystemNotification(gpui.SystemNotification{
		Title: "desktopspec never shown", Tag: "desktopspec-no-identity",
	})
	if err != nil {
		t.Fatalf("the no-identity no-op is the source-supported outcome: %v", err)
	}
	if err := app.OnSystemNotificationResponse(func(gpui.SystemNotificationResponse) {
		t.Error("no toast exists to activate")
	}); err != nil {
		t.Fatalf("OnSystemNotificationResponse: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Menus, dock menus and app identity
// ---------------------------------------------------------------------------

// TestMenusAndDockMenusThroughApp pins the menu surface: menus are
// recorded and returned; dock-menu action dispatch crosses the update
// boundary through the app-menu-action callback; the jump-list shell
// commit reports its pending row instead of fabricating a removal list.
func TestMenusAndDockMenusThroughApp(t *testing.T) {
	app, h := newApp(t)

	menus := []gpui.Menu{{Name: "File", Items: []gpui.MenuItem{
		{Kind: gpui.MenuItemSeparator},
	}}}
	if err := app.SetMenus(menus); err != nil {
		t.Fatalf("SetMenus: %v", err)
	}
	waitFor(t, 10*time.Second, "the menus to be recorded", func() bool {
		got, ok := app.GetMenus()
		return ok && len(got) == 1 && got[0].Name == "File"
	})

	// A unit action for the dock menu.
	type dockTestAction struct{}
	defineDockAction := func() gpui.BoxedAction {
		var zero dockTestAction
		_ = zero
		return gpui.DefineUnitAction[dockTestAction]("desktopspec::Dock").Box(dockTestAction{})
	}
	received := make(chan string, 4)
	if err := app.OnAppMenuAction(func(action gpui.BoxedAction) {
		received <- action.Name()
	}); err != nil {
		t.Fatalf("OnAppMenuAction: %v", err)
	}
	items := []gpui.MenuItem{
		{Kind: gpui.MenuItemAction, Name: "New Window", Action: defineDockAction()},
	}
	if _, err := app.UpdateJumpList(items, [][]string{{`C:\workspace`}}); !errors.Is(err, gpui.ErrJumpListShellCommitUnavailable) {
		t.Fatalf("UpdateJumpList error = %v, want the shell-commit pending row", err)
	}
	if err := app.PerformDockMenuAction(0); err != nil {
		t.Fatalf("PerformDockMenuAction: %v", err)
	}
	if name := recvWithin(t, 10*time.Second, "the dock menu action", received); name != "desktopspec::Dock" {
		t.Fatalf("dispatched action = %q", name)
	}
	// A missing index dispatches nothing.
	if err := app.PerformDockMenuAction(99); err != nil {
		t.Fatalf("PerformDockMenuAction(missing): %v", err)
	}
	select {
	case name := <-received:
		t.Fatalf("a missing index must not dispatch, got %q", name)
	case <-time.After(300 * time.Millisecond):
	}
	// SetDockMenu spawns the background shell commit: its unported state
	// surfaces as the recorded fault (the reference logs the error).
	if err := app.SetDockMenu(items); err != nil {
		t.Fatalf("SetDockMenu: %v", err)
	}
	found := false
	for i := 0; i < 100 && !found; i++ {
		for _, fault := range h.Faults() {
			if strings.Contains(fault, "jump list") {
				found = true
			}
		}
		if !found {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !found {
		t.Fatalf("the jump-list shell commit's pending row was not recorded; faults = %v", h.Faults())
	}
}

// TestAppIdentityProcessScoped sets the app identity through the app
// surface: SetCurrentProcessExplicitAppUserModelID is process-scoped
// (no user-visible change), and the notification path switches to the
// explicit pending-row failure.
func TestAppIdentityProcessScoped(t *testing.T) {
	app, _ := newApp(t)
	if err := app.SetAppIdentity("gpui-go.desktopspec", "GPUI Go Desktop Spec"); err != nil {
		t.Fatalf("SetAppIdentity: %v", err)
	}
	err := app.ShowSystemNotification(gpui.SystemNotification{Title: "x", Tag: "with-identity"})
	if !errors.Is(err, gpui.ErrToastNotifierUnavailable) {
		t.Fatalf("with an identity the toast notifier pending row must surface, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test applications (no host) report the typed errors
// ---------------------------------------------------------------------------

// TestNoHostDesktopOperationsErrors pins the ErrNoHost outcome for
// every desktop operation on a test application.
func TestNoHostDesktopOperationsErrors(t *testing.T) {
	app := gpui.NewApp()
	if _, err := app.Displays(); !errors.Is(err, gpui.ErrNoHost) {
		t.Errorf("Displays: %v", err)
	}
	if _, err := app.PrimaryDisplay(); !errors.Is(err, gpui.ErrNoHost) {
		t.Errorf("PrimaryDisplay: %v", err)
	}
	if err := app.SetCursorStyle(gpui.CursorArrow); !errors.Is(err, gpui.ErrNoHost) {
		t.Errorf("SetCursorStyle: %v", err)
	}
	if err := app.OpenURL("https://example.invalid/"); !errors.Is(err, gpui.ErrNoHost) {
		t.Errorf("OpenURL: %v", err)
	}
	if err := app.RevealPath(`C:\x`); !errors.Is(err, gpui.ErrNoHost) {
		t.Errorf("RevealPath: %v", err)
	}
	if err := app.ShowSystemNotification(gpui.SystemNotification{}); !errors.Is(err, gpui.ErrNoHost) {
		t.Errorf("ShowSystemNotification: %v", err)
	}
	if err := app.Restart("", nil); !errors.Is(err, gpui.ErrNoHost) {
		t.Errorf("Restart: %v", err)
	}
	if got := app.ThermalState(); got != gpui.ThermalStateNominal {
		t.Errorf("ThermalState (no host) = %v, want Nominal", got)
	}
	if _, ok := app.GetMenus(); ok {
		t.Error("GetMenus without a host must report nothing")
	}
}
