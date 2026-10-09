package gpui

// This file owns ticket26's platform-neutral desktop-operation types:
// the enum/record surface of the reference Platform trait that the
// Win32 desktop slice consumes. The Win32 behavior lives in
// desktop_windows.go / desktop_shell_windows.go (windows) and
// desktop_other.go (typed-error stubs elsewhere); only the data shapes
// and the pure mappings live here so they compile and are testable on
// every platform.
//
// The reference is CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a,
// crates/gpui/src/platform.rs:
//   - CursorStyle (platform.rs:3189) — bounded to the variants the
//     pinned Windows loader distinguishes; the reference styles that
//     load_cursor's `_` arm maps to the arrow fold to Arrow here too.
//   - WindowAppearance (platform.rs:2849).
//   - WindowBackgroundAppearance (platform.rs:2879) and is_opaque /
//     is_transparent.
//   - ThermalState (platform.rs:457).
//   - SystemNotification / SystemNotificationAction /
//     SystemNotificationResponse (platform.rs near line 430).
//   - the renderer clear color for one background appearance
//     (crates/gpui_windows/src/directx_renderer.rs render: opaque
//     clears to [1,1,1,1], everything else to [0,0,0,0]).

import "errors"

// ---------------------------------------------------------------------------
// Cursor
// ---------------------------------------------------------------------------

// CursorStyle selects the pointer cursor. The set is the one the pinned
// Windows loader (gpui_windows/src/util.rs load_cursor) maps to distinct
// system cursors; unmapped reference styles load the arrow exactly like
// the reference's catch-all arm.
type CursorStyle uint8

// Cursor styles (reference platform.rs CursorStyle, bounded to the
// Windows loader's distinct cases).
const (
	// CursorArrow is the default pointer (IDC_ARROW).
	CursorArrow CursorStyle = iota
	// CursorIBeam is the text cursor (IDC_IBEAM).
	CursorIBeam
	// CursorIBeamVertical is the vertical text cursor (IDC_IBEAM; the
	// reference shares the IBeam mapping).
	CursorIBeamVertical
	// CursorCrosshair is the crosshair (IDC_CROSS).
	CursorCrosshair
	// CursorPointingHand is the pointer hand (IDC_HAND).
	CursorPointingHand
	// CursorDragLink shares the pointing-hand cursor (IDC_HAND).
	CursorDragLink
	// CursorResizeLeft is the left resize arrow (IDC_SIZEWE).
	CursorResizeLeft
	// CursorResizeRight is the right resize arrow (IDC_SIZEWE).
	CursorResizeRight
	// CursorResizeLeftRight is the horizontal double arrow (IDC_SIZEWE).
	CursorResizeLeftRight
	// CursorResizeColumn is the column resize arrow (IDC_SIZEWE).
	CursorResizeColumn
	// CursorResizeUp is the up resize arrow (IDC_SIZENS).
	CursorResizeUp
	// CursorResizeDown is the down resize arrow (IDC_SIZENS).
	CursorResizeDown
	// CursorResizeUpDown is the vertical double arrow (IDC_SIZENS).
	CursorResizeUpDown
	// CursorResizeRow is the row resize arrow (IDC_SIZENS).
	CursorResizeRow
	// CursorResizeUpLeftDownRight is the \ diagonal arrow (IDC_SIZENWSE).
	CursorResizeUpLeftDownRight
	// CursorResizeUpRightDownLeft is the / diagonal arrow (IDC_SIZENESW).
	CursorResizeUpRightDownLeft
	// CursorOperationNotAllowed is the "no" cursor (IDC_NO).
	CursorOperationNotAllowed
)

// ---------------------------------------------------------------------------
// Appearance
// ---------------------------------------------------------------------------

// WindowAppearance is the OS appearance of a window (reference
// platform.rs WindowAppearance; Windows maps only Light/Dark and keeps
// the vibrant spelling for parity).
type WindowAppearance uint8

// Window appearances.
const (
	// WindowAppearanceLight is the light appearance (the default).
	WindowAppearanceLight WindowAppearance = iota
	// WindowAppearanceVibrantLight is light with vibrant colors
	// (macOS-only spelling; Windows treats it as light).
	WindowAppearanceVibrantLight
	// WindowAppearanceDark is the dark appearance.
	WindowAppearanceDark
	// WindowAppearanceVibrantDark is dark with vibrant colors
	// (macOS-only spelling; Windows treats it as dark).
	WindowAppearanceVibrantDark
)

// IsDark reports whether the appearance is a dark spelling, the test
// configure_dwm_dark_mode uses to pick the immersive dark mode value.
func (a WindowAppearance) IsDark() bool {
	return a == WindowAppearanceDark || a == WindowAppearanceVibrantDark
}

// ---------------------------------------------------------------------------
// Window background appearance
// ---------------------------------------------------------------------------

// WindowBackgroundAppearance selects how the window's background
// composites with the content behind it (reference platform.rs
// WindowBackgroundAppearance). The Mica materials are Windows 11 only.
type WindowBackgroundAppearance uint8

// Background appearances.
const (
	// WindowBackgroundOpaque composites nothing behind the window.
	WindowBackgroundOpaque WindowBackgroundAppearance = iota
	// WindowBackgroundTransparent lets the content behind show through.
	WindowBackgroundTransparent
	// WindowBackgroundBlurred blurs the content behind the window.
	WindowBackgroundBlurred
	// WindowBackgroundMica is the Mica backdrop (Windows 11).
	WindowBackgroundMica
	// WindowBackgroundMicaAlt is the Mica Alt backdrop (Windows 11).
	WindowBackgroundMicaAlt
)

// IsOpaque reports whether the background hides everything behind the
// window (reference is_opaque).
func (a WindowBackgroundAppearance) IsOpaque() bool { return a == WindowBackgroundOpaque }

// IsTransparent reports whether content behind the window shows
// through, directly, blurred, or via a system backdrop material
// (reference is_transparent).
func (a WindowBackgroundAppearance) IsTransparent() bool { return !a.IsOpaque() }

// ClearColorForBackground returns the renderer clear color for one
// background appearance: opaque clears to opaque white and every other
// mode clears to fully transparent, because the content behind the
// window must survive the clear (gpui_windows/src/directx_renderer.rs
// render: `pre_draw(&match background_appearance { appearance if
// appearance.is_opaque() => [1.0f32; 4], _ => [0.0f32; 4] })`). This is
// the renderer-side half of set_background_appearance: the DWM half
// lives in the Win32 desktop driver.
func ClearColorForBackground(a WindowBackgroundAppearance) Color {
	if a.IsOpaque() {
		return Color{R: 1, G: 1, B: 1, A: 1}
	}
	return Color{}
}

// ---------------------------------------------------------------------------
// Thermal state
// ---------------------------------------------------------------------------

// ThermalState is the system's thermal constraint level (reference
// platform.rs ThermalState). The Windows platform always reports
// Nominal and never fires the change callback (gpui_windows/src
// platform.rs: `fn thermal_state -> ThermalState::Nominal` and
// `on_thermal_state_change` is an empty body).
type ThermalState uint8

// Thermal states.
const (
	// ThermalStateNominal: no thermal constraints (the Windows value).
	ThermalStateNominal ThermalState = iota
	// ThermalStateFair: slightly constrained.
	ThermalStateFair
	// ThermalStateSerious: moderately constrained.
	ThermalStateSerious
	// ThermalStateCritical: critically constrained.
	ThermalStateCritical
)

// ---------------------------------------------------------------------------
// Displays
// ---------------------------------------------------------------------------

// DisplayID identifies a monitor (the Win32 HMONITOR value; reference
// DisplayId::new(monitor.0 as u64)).
type DisplayID uint64

// Display is one monitor's geometry (reference PlatformDisplay /
// gpui_windows/src/display.rs WindowsDisplay, bounded to the public
// PlatformDisplay surface: id, uuid, bounds, visible_bounds plus the
// scale factor the conversions used).
type DisplayInfo struct {
	// ID is the monitor identity (HMONITOR).
	ID DisplayID
	// UUID is the stable monitor identity: a v5 UUID over the monitor's
	// device name (display.rs generate_uuid), formatted canonically.
	UUID string
	// Bounds is the full monitor bounds in logical pixels.
	Bounds Bounds
	// VisibleBounds is the work area in logical pixels.
	VisibleBounds Bounds
	// ScaleFactor is the monitor's own DPI scale (GetDpiForMonitor
	// MDT_EFFECTIVE_DPI / 96), independent of any window.
	ScaleFactor float32
}

// ---------------------------------------------------------------------------
// System notifications
// ---------------------------------------------------------------------------

// SystemNotificationAction is one button of a system notification
// (reference SystemNotificationAction: id + label).
type SystemNotificationAction struct {
	// ID identifies the action in the response.
	ID string
	// Label is the user-visible button text.
	Label string
}

// SystemNotification describes a system notification (reference
// SystemNotification: title, body, tag, actions).
type SystemNotification struct {
	// Title is the notification's heading.
	Title string
	// Body is the notification's detail text.
	Body string
	// Tag identifies the notification for later dismissal.
	Tag string
	// Actions are the notification's buttons.
	Actions []SystemNotificationAction
}

// SystemNotificationResponse reports a user activation of a
// notification (reference SystemNotificationResponse). Go adaptation:
// the reference's Option<SharedString> action id becomes ActionID plus
// ActionActivated, because an empty action id is representable in the
// source and must stay distinguishable from body activation.
type SystemNotificationResponse struct {
	// Tag is the activated notification's tag.
	Tag string
	// ActionID is the pressed action button's id.
	ActionID string
	// ActionActivated reports whether an action button (rather than the
	// notification body) was activated.
	ActionActivated bool
}

// ---------------------------------------------------------------------------
// Menus and jump lists
// ---------------------------------------------------------------------------

// MenuItemKind mirrors the reference MenuItem enum's variants bounded
// to the identity the Windows dock menu needs: only Action items are
// accepted there (gpui_windows/src/destination_list.rs
// DockMenuItem::new bails on every other variant).
type MenuItemKind uint8

// Menu item variants.
const (
	// MenuItemSeparator is a separator between items.
	MenuItemSeparator MenuItemKind = iota
	// MenuItemSubmenu is a nested menu (not dock-menu representable on
	// Windows).
	MenuItemSubmenu
	// MenuItemSystemMenu is a system-managed menu (macOS concept).
	MenuItemSystemMenu
	// MenuItemAction is an action item.
	MenuItemAction
)

// MenuItem is one menu entry (reference gpui::MenuItem bounded to the
// fields the Windows platform consumes: name and action). Submenu and
// system-menu items keep their kind only; the dock-menu builder drops
// them exactly like the reference's DockMenuItem::new error path.
type MenuItem struct {
	// Kind selects the variant.
	Kind MenuItemKind
	// Name is the item's user-visible label.
	Name string
	// Action is the action to perform (valid when Kind is
	// MenuItemAction).
	Action BoxedAction
}

// Menu is one named menu (reference gpui::Menu). The Windows platform
// records menus and returns them on query; it does not install a native
// menu bar (set_menus/get_menus in gpui_windows/src/platform.rs).
type Menu struct {
	// Name is the menu's title.
	Name string
	// Items are the menu's entries.
	Items []MenuItem
}

// ---------------------------------------------------------------------------
// Window kind
// ---------------------------------------------------------------------------

// WindowKind selects the window flavor for creation (reference
// WindowKind). AnchoredPopup exists in the Go surface because the
// Windows platform must reject it with the source's typed error so
// callers fall back to in-window popovers (gpui_windows/src/window.rs
// new: `Native popups are not implemented on Windows yet. Rejecting
// lets callers fall back to gpui's in-window popups.`).
type WindowKind uint8

// Window kinds.
const (
	// WindowKindNormal is a regular top-level window.
	WindowKindNormal WindowKind = iota
	// WindowKindAnchoredPopup is a native popup; rejected on Windows.
	WindowKindAnchoredPopup
)

// ---------------------------------------------------------------------------
// Typed desktop errors
// ---------------------------------------------------------------------------

var (
	// ErrPopupNotSupported reports a native anchored popup request.
	// Reproducing the reference's PopupNotSupportedError lets callers
	// take the in-window popover fallback instead of failing silently.
	ErrPopupNotSupported = errors.New("gpui: native anchored popups are not supported on Windows (the reference rejects WindowKind::AnchoredPopup so callers fall back to in-window popovers)")
	// ErrAuxiliaryExecutableUnavailable reproduces the reference's
	// path_for_auxiliary_executable bail ("not yet implemented").
	ErrAuxiliaryExecutableUnavailable = errors.New("gpui: path_for_auxiliary_executable is not yet implemented on Windows (reference: bail!(\"not yet implemented\"))")
	// ErrURLSchemeRegistrationUnsupported reproduces the reference's
	// register_url_scheme task error.
	ErrURLSchemeRegistrationUnsupported = errors.New("gpui: register_url_scheme is unimplemented on Windows")
	// ErrJumpListShellCommitUnavailable reports the one unported half
	// of the Windows jump list: the shell destination-list commit
	// (ICustomDestinationList BeginList/AppendCategory/AddUserTasks/
	// CommitList). The in-process jump-list state and the dock-menu
	// action dispatch are implemented; writing the taskbar's list is a
	// pending row, not a fake success.
	ErrJumpListShellCommitUnavailable = errors.New("gpui: the shell destination-list commit (ICustomDestinationList) is not ported to the pure-stdlib host; the jump-list state update ran, the taskbar list was not written")
	// ErrToastNotifierUnavailable reports the unported WinRT toast
	// notifier (ToastNotificationManager::CreateToastNotifier /
	// CreateToastNotifierWithId). Showing a toast is a pending row; the
	// source-supported no-identity outcome (a recorded warning and no
	// notification) is returned as success exactly like the reference.
	ErrToastNotifierUnavailable = errors.New("gpui: the WinRT toast notifier is not ported to the pure-stdlib host; no notification was shown")
	// ErrPowerShellUnavailable reproduces the reference restart path's
	// missing-PowerShell outcome.
	ErrPowerShellUnavailable = errors.New("gpui: PowerShell was not found for the restart script")
)

// MouseWheelSettings holds the system parameters the Windows platform
// tracks from WM_SETTINGCHANGE (gpui_windows/src/system_settings.rs):
// the wheel scroll line and character counts read through
// SystemParametersInfoW.
type MouseWheelSettings struct {
	// WheelScrollChars is SPI_GETWHEELSCROLLCHARS.
	WheelScrollChars uint32
	// WheelScrollLines is SPI_GETWHEELSCROLLLINES.
	WheelScrollLines uint32
}

// WindowButton identifies one window-control button (reference
// WindowButton, bounded to the buttons the layouts describe).
type WindowButton uint8

// Window buttons (reference platform.rs WindowButton, bounded).
const (
	// WindowButtonClose is the close button.
	WindowButtonClose WindowButton = iota
	// WindowButtonMinimize is the minimize button.
	WindowButtonMinimize
	// WindowButtonZoom is the maximize/zoom button.
	WindowButtonZoom
)

// WindowButtonLayout is the window-control button arrangement
// (reference WindowButtonLayout: which buttons sit on which side).
// Windows does not override the platform trait's button_layout, so the
// query always reports the unsupported default there.
type WindowButtonLayout struct {
	// Left lists the buttons on the window's left side.
	Left []WindowButton
	// Right lists the buttons on the window's right side.
	Right []WindowButton
}
