//go:build !windows

package gpui

// The non-Windows desktop-operation stubs (ticket26): the same public
// surface as the Win32 desktop driver with typed errors instead of OS
// behavior, so the platform-neutral window layer (window.go, app.go)
// compiles everywhere. The target is windows/amd64 (see
// docs/windows-platform-contract.md); every real desktop operation
// exists only there.

import "errors"

// ---------------------------------------------------------------------------
// Host desktop operations
// ---------------------------------------------------------------------------

// SetCursorStyle reports the unsupported platform.
func (h *Host) SetCursorStyle(style CursorStyle) {}

// CursorStyle reports the unsupported platform.
func (h *Host) CursorStyle() (CursorStyle, error) { return CursorArrow, ErrHostUnsupportedPlatform }

// HideCursorUntilMouseMoves is a no-op.
func (h *Host) HideCursorUntilMouseMoves() {}

// IsCursorVisible reports the unsupported platform.
func (h *Host) IsCursorVisible() bool { return false }

// Displays reports the unsupported platform.
func (h *Host) Displays() ([]DisplayInfo, error) { return nil, ErrHostUnsupportedPlatform }

// PrimaryDisplay reports the unsupported platform.
func (h *Host) PrimaryDisplay() (*DisplayInfo, error) { return nil, ErrHostUnsupportedPlatform }

// OpenURL is a no-op.
func (h *Host) OpenURL(url string) {}

// OpenWithSystem is a no-op.
func (h *Host) OpenWithSystem(path string) {}

// RevealPath is a no-op.
func (h *Host) RevealPath(path string) {}

// OnOpenUrls reports the unsupported platform.
func (h *Host) OnOpenUrls(cb func(urls []string)) error { return ErrHostUnsupportedPlatform }

// OnSystemWake reports the unsupported platform.
func (h *Host) OnSystemWake(cb func()) error { return ErrHostUnsupportedPlatform }

// OnQuit reports the unsupported platform.
func (h *Host) OnQuit(cb func() bool) error { return ErrHostUnsupportedPlatform }

// OnReopen reports the unsupported platform.
func (h *Host) OnReopen(cb func()) error { return ErrHostUnsupportedPlatform }

// ThermalState reports the nominal constant (no platform exists).
func (h *Host) ThermalState() ThermalState { return ThermalStateNominal }

// OnThermalStateChange is a no-op.
func (h *Host) OnThermalStateChange(cb func()) error { return nil }

// ActivateApp is a no-op.
func (h *Host) ActivateApp(ignoringOtherApps bool) {}

// HideApp is a no-op.
func (h *Host) HideApp() {}

// HideOtherApps reports the unsupported platform (the Windows build
// reproduces the reference panic instead).
func (h *Host) HideOtherApps() error { return ErrHostUnsupportedPlatform }

// UnhideOtherApps reports the unsupported platform.
func (h *Host) UnhideOtherApps() error { return ErrHostUnsupportedPlatform }

// PathForAuxiliaryExecutable reports the unsupported platform.
func (h *Host) PathForAuxiliaryExecutable(name string) (string, error) {
	return "", ErrHostUnsupportedPlatform
}

// RegisterURLScheme reports the unsupported platform.
func (h *Host) RegisterURLScheme(scheme string) error { return ErrHostUnsupportedPlatform }

// AppPath reports the unsupported platform.
func (h *Host) AppPath() (string, error) { return "", ErrHostUnsupportedPlatform }

// Restart reports the unsupported platform.
func (h *Host) Restart(binaryPath string, arguments []string) error {
	return ErrHostUnsupportedPlatform
}

// SetAppIdentity reports the unsupported platform.
func (h *Host) SetAppIdentity(identifier, name string) error {
	return ErrHostUnsupportedPlatform
}

// ShowSystemNotification reports the unsupported platform.
func (h *Host) ShowSystemNotification(notification SystemNotification) error {
	return ErrHostUnsupportedPlatform
}

// DismissSystemNotification is a no-op.
func (h *Host) DismissSystemNotification(tag string) {}

// OnSystemNotificationResponse reports the unsupported platform.
func (h *Host) OnSystemNotificationResponse(cb func(SystemNotificationResponse)) error {
	return ErrHostUnsupportedPlatform
}

// SetMenus is a no-op.
func (h *Host) SetMenus(menus []Menu) {}

// GetMenus reports no menus.
func (h *Host) GetMenus() ([]Menu, bool) { return nil, false }

// SetDockMenu reports the unsupported platform.
func (h *Host) SetDockMenu(items []MenuItem) error { return ErrHostUnsupportedPlatform }

// UpdateJumpList reports the unsupported platform.
func (h *Host) UpdateJumpList(menus []MenuItem, entries [][]string) ([][]string, error) {
	return nil, ErrHostUnsupportedPlatform
}

// PerformDockMenuAction is a no-op.
func (h *Host) PerformDockMenuAction(index int) {}

// OnAppMenuAction reports the unsupported platform.
func (h *Host) OnAppMenuAction(cb func(action BoxedAction)) error {
	return ErrHostUnsupportedPlatform
}

// OnWillOpenAppMenu reports the unsupported platform.
func (h *Host) OnWillOpenAppMenu(cb func()) error { return ErrHostUnsupportedPlatform }

// OnValidateAppMenuCommand reports the unsupported platform.
func (h *Host) OnValidateAppMenuCommand(cb func(action BoxedAction) bool) error {
	return ErrHostUnsupportedPlatform
}

// WindowAppearance reports the unsupported platform.
func (h *Host) WindowAppearance() (WindowAppearance, error) {
	return WindowAppearanceLight, ErrHostUnsupportedPlatform
}

// MouseWheelSettings reports the unsupported platform.
func (h *Host) MouseWheelSettings() MouseWheelSettings { return MouseWheelSettings{} }

// ButtonLayout reports the unsupported platform.
func (h *Host) ButtonLayout() (WindowButtonLayout, bool) {
	return WindowButtonLayout{}, false
}

// OnButtonLayoutChanged reports the unsupported platform.
func (h *Host) OnButtonLayoutChanged(cb func()) error { return ErrHostUnsupportedPlatform }

// Faults reports no faults (there is no host loop to record them).
func (h *Host) Faults() []string { return nil }

// ---------------------------------------------------------------------------
// WindowHandle desktop operations
// ---------------------------------------------------------------------------

// Minimize is a no-op.
func (wh WindowHandle) Minimize() {}

// Zoom is a no-op.
func (wh WindowHandle) Zoom() {}

// IsMaximized reports the unsupported platform.
func (wh WindowHandle) IsMaximized() (bool, error) { return false, ErrHostUnsupportedPlatform }

// IsMinimized reports the unsupported platform.
func (wh WindowHandle) IsMinimized() (bool, error) { return false, ErrHostUnsupportedPlatform }

// IsActive reports the unsupported platform.
func (wh WindowHandle) IsActive() (bool, error) { return false, ErrHostUnsupportedPlatform }

// RequestAttention is a no-op.
func (wh WindowHandle) RequestAttention() {}

// MousePosition reports the unsupported platform.
func (wh WindowHandle) MousePosition() (Point, error) { return Point{}, ErrHostUnsupportedPlatform }

// IsHovered reports the unsupported platform.
func (wh WindowHandle) IsHovered() (bool, error) { return false, ErrHostUnsupportedPlatform }

// ShowCharacterPalette reports the unsupported platform.
func (wh WindowHandle) ShowCharacterPalette() error { return ErrHostUnsupportedPlatform }

// PlaySystemBell is a no-op.
func (wh WindowHandle) PlaySystemBell() {}

// Appearance reports the unsupported platform.
func (wh WindowHandle) Appearance() (WindowAppearance, error) {
	return WindowAppearanceLight, ErrHostUnsupportedPlatform
}

// SetBackgroundAppearance is a no-op.
func (wh WindowHandle) SetBackgroundAppearance(appearance WindowBackgroundAppearance) {}

// BackgroundAppearance reports the unsupported platform.
func (wh WindowHandle) BackgroundAppearance() (WindowBackgroundAppearance, error) {
	return WindowBackgroundOpaque, ErrHostUnsupportedPlatform
}

// ClearColor reports the unsupported platform.
func (wh WindowHandle) ClearColor() (Color, error) { return Color{}, ErrHostUnsupportedPlatform }

// CompositionRecord reports the unsupported platform.
func (wh WindowHandle) CompositionRecord() (string, error) { return "", ErrHostUnsupportedPlatform }

// Display reports the unsupported platform.
func (wh WindowHandle) Display() (*DisplayInfo, error) { return nil, ErrHostUnsupportedPlatform }
