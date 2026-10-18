//go:build windows

package gpui

// Ticket26's Win32 desktop operations, ported from the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/platform.rs — WindowsPlatformState
//     (current_cursor, cursor_visible, jump_list, menus),
//     set_cursor_style / hide_cursor_until_mouse_moves /
//     is_cursor_visible, displays / primary_display, on_system_wake +
//     RegisterSuspendResumeNotification, on_quit / end-session,
//     set_app_identity, the nominal thermal state, the empty
//     activate/hide and unimplemented hide_other_apps outcomes, and
//     WM_POWERBROADCAST / WM_GPUI_DOCK_MENU_ACTION /
//     WM_GPUI_END_SESSION in WindowsPlatformInner::handle_msg.
//   - crates/gpui_windows/src/window.rs — minimize (ShowWindowAsync
//     SW_MINIMIZE), zoom (SW_MAXIMIZE/SW_RESTORE), set_title,
//     background_appearance/set_background_appearance (DWM accent /
//     backdrop attributes), request_attention (FlashWindowEx),
//     is_active, mouse_position.
//   - crates/gpui_windows/src/events.rs — the message policies for
//     WM_SETCURSOR (handle_set_cursor), WM_SETTINGCHANGE
//     (handle_system_settings_changed / handle_system_theme_changed),
//     WM_DISPLAYCHANGE, WM_ACTIVATE's cursor/modifier reset,
//     WM_MOUSEMOVE/WM_NCMOUSEMOVE restore_cursor_after_hide,
//     WM_MOUSELEAVE, WM_SHOWWINDOW, WM_MOUSEACTIVATE and
//     WM_GPUI_CURSOR_STYLE_CHANGED.
//   - crates/gpui_windows/src/display.rs — monitor enumeration, the
//     primary monitor, per-monitor scale and the v5 monitor UUID.
//   - crates/gpui_windows/src/system_settings.rs — the mouse-wheel
//     settings read through SystemParametersInfoW.
//   - crates/gpui_windows/src/util.rs — load_cursor,
//     configure_dwm_dark_mode.
//
// Pure Go: every Win32 call goes through the stdlib syscall package
// (CGO_ENABLED=0). State that app callbacks observe is marshaled to the
// host's foreground thread exactly like the reference's foreground
// executor; OS queries (display enumeration, package identity) run on
// the calling goroutine like the reference's direct calls.
//
// Deviations (each cited at its site):
//   - system_appearance reads the Personalize registry value instead
//     of the WinRT UISettings foreground color; the WinRT activation
//     surface is not ported. The provider is a seam so the observer
//     routing is testable without touching user settings.
//   - ShellExecuteW-based launches run on the host thread (STA
//     apartment) instead of a background thread.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Win32 bindings (ticket26's own set; names are distinct from the other
// platform slices' bindings so concurrent tickets cannot collide)
// ---------------------------------------------------------------------------

var (
	modDwmapi = syscall.NewLazyDLL("dwmapi.dll")
	modNtdll  = syscall.NewLazyDLL("ntdll.dll")

	procSetCursor                               = modUser32.NewProc("SetCursor")
	procLoadImageW                              = modUser32.NewProc("LoadImageW")
	procShowWindowAsync                         = modUser32.NewProc("ShowWindowAsync")
	procGetCursorPos                            = modUser32.NewProc("GetCursorPos")
	procScreenToClient                          = modUser32.NewProc("ScreenToClient")
	procMonitorFromPoint                        = modUser32.NewProc("MonitorFromPoint")
	procEnumDisplayMonitors                     = modUser32.NewProc("EnumDisplayMonitors")
	procSystemParametersInfoW                   = modUser32.NewProc("SystemParametersInfoW")
	procFlashWindowEx                           = modUser32.NewProc("FlashWindowEx")
	procRegisterSuspendResumeNotification       = modUser32.NewProc("RegisterSuspendResumeNotification")
	procUnregisterSuspendResumeNotification     = modUser32.NewProc("UnregisterSuspendResumeNotification")
	procMessageBeep                             = modUser32.NewProc("MessageBeep")
	procTrackMouseEvent                         = modUser32.NewProc("TrackMouseEvent")
	procDwmSetWindowAttribute                   = modDwmapi.NewProc("DwmSetWindowAttribute")
	procDwmGetWindowAttribute                   = modDwmapi.NewProc("DwmGetWindowAttribute")
	procRtlGetVersion                           = modNtdll.NewProc("RtlGetVersion")
	procGetCurrentPackageFullName               = modKernel32.NewProc("GetCurrentPackageFullName")
	procGetDpiForMonitor                        = modShcore.NewProc("GetDpiForMonitor")
	procSetCurrentProcessExplicitAppUserModelID = modShell32.NewProc("SetCurrentProcessExplicitAppUserModelID")

	// Optional/undocumented: probed with Find, not required.
	procSetWindowCompositionAttribute = modUser32.NewProc("SetWindowCompositionAttribute")
)

// This slice's own bindings whose names stay distinct from sibling
// platform slices: GetActiveWindow/IsWindowEnabled/advapi32 registry.
var (
	procGetActiveWindowDesktop = modUser32.NewProc("GetActiveWindow")
	procIsWindowEnabledDesktop = modUser32.NewProc("IsWindowEnabled")

	procRegOpenKeyExWDesktop    = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExWDesktop = modAdvapi32.NewProc("RegQueryValueExW")
	procRegCloseKeyDesktop      = modAdvapi32.NewProc("RegCloseKey")
)

// Win32 message and constant values (winuser.h, dwmapi.h, shcore.h,
// appmodel.h). Comments name the SDK symbol.
const (
	wmEndSession      = 0x0016
	wmSettingChange   = 0x001A
	wmSetCursor       = 0x0020
	wmMouseActivate   = 0x0021
	wmNcMouseMove     = 0x00A0
	wmDisplayChange   = 0x007E
	wmMouseMove       = 0x0200
	wmMouseLeave      = 0x02A3
	wmNcMouseLeave    = 0x02A2
	wmPowerBroadcast  = 0x0218
	wmGPUICursorStyle = wmUser + 1 // reference WM_GPUI_CURSOR_STYLE_CHANGED
	wmGPUIDockAction  = wmUser + 4 // reference WM_GPUI_DOCK_MENU_ACTION
	wmGPUIEndSession  = wmUser + 9 // reference WM_GPUI_END_SESSION

	pbtApmResumeAutomatic = 0x12 // PBT_APMRESUMEAUTOMATIC

	spiGetWheelScrollLines = 0x68 // SPI_GETWHEELSCROLLLINES
	spiGetWheelScrollChars = 0x6C // SPI_GETWHEELSCROLLCHARS

	maActivate = 1 // MA_ACTIVATE

	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17

	swMaximize    = 3  // SW_MAXIMIZE (same value as SW_SHOWMAXIMIZED)
	swShowDefault = 10 // SW_SHOWDEFAULT

	idcArrow    = 32512
	idcIBeam    = 32513
	idcCross    = 32515
	idcSizeNWSE = 32642
	idcSizeNESW = 32643
	idcSizeWE   = 32644
	idcSizeNS   = 32645
	idcNo       = 32648
	idcHand     = 32649

	imageCursor              = 2
	lrDefaultSize            = 0x00000040
	lrShared                 = 0x00008000
	tmeLeave                 = 0x00000002
	tmeNonClient             = 0x00000010
	hoverDefault             = 0xFFFFFFFF
	flashwAll                = 0x00000003
	mbOK                     = 0x00000000
	monitorDefaultToNull     = 0x00000000
	monitorDefaultToPrimary  = 0x00000001
	mdtEffectiveDPI          = 0
	deviceNotifyWindowHandle = 0x00000000

	dwmwaUseImmersiveDarkMode = 20 // DWMWA_USE_IMMERSIVE_DARK_MODE
	dwmwaSystemBackdropType   = 38 // DWMWA_SYSTEMBACKDROP_TYPE

	dwmsbtMainWindow   = 2 // DWMSBT_MAINWINDOW (Mica)
	dwmsbtTabbedWindow = 4 // DWMSBT_TABBEDWINDOW (Mica Alt)

	wcaAccentPolicy = 0x13

	errorInsufficientBuffer  = 122
	appModelErrorNoPackage   = 15700
	hresultFileNotFoundWin32 = uintptr(0x80070002) // HRESULT_FROM_WIN32(ERROR_FILE_NOT_FOUND)

	createNoWindow = 0x08000000

	cchDeviceName = 32
)

// trackMouseEventStruct is Win32 TRACKMOUSEEVENT.
type trackMouseEventStruct struct {
	cbSize      uint32
	dwFlags     uint32
	hwndTrack   uintptr
	dwHoverTime uint32
}

// flashwInfo is Win32 FLASHWINFO.
type flashwInfo struct {
	cbSize    uint32
	hwnd      uintptr
	dwFlags   uint32
	uCount    uint32
	dwTimeout uint32
}

// osVersionInfo is Win32 RTL_OSVERSIONINFOW (276 bytes).
type osVersionInfo struct {
	dwOSVersionInfoSize uint32
	dwMajorVersion      uint32
	dwMinorVersion      uint32
	dwBuildNumber       uint32
	dwPlatformId        uint32
	szCSDVersion        [128]uint16
}

// accentPolicy is the undocumented AccentPolicy the reference passes
// through SetWindowCompositionAttribute (window.rs AccentPolicy).
type accentPolicy struct {
	accentState   uint32
	accentFlags   uint32
	gradientColor uint32
	animationID   uint32
}

// windowCompositionAttribData is Win32 WINDOWCOMPOSITIONATTRIBDATA
// (window.rs WINDOWCOMPOSITIONATTRIBDATA).
type windowCompositionAttribData struct {
	attrib uint32
	pvData *accentPolicy
	cbData uintptr
}

// monitorInfoEx is Win32 MONITORINFOEXW (104 bytes): the shared
// MONITORINFO header plus the device name the display UUID derives
// from.
type monitorInfoEx struct {
	info   monitorInfo
	device [cchDeviceName]uint16
}

// ---------------------------------------------------------------------------
// Desktop state on the Host
// ---------------------------------------------------------------------------

// desktopCallbacks are the app-level platform callbacks
// (gpui_windows/src/platform.rs PlatformCallbacks). They are set from
// any goroutine and invoked only on the host (foreground) thread; the
// mutex guards the cells, never the callbacks themselves (the
// with-callback pattern takes the callback out, invokes it and puts it
// back, so a reentrant platform call never deadlocks).
type desktopCallbacks struct {
	openURLs               func([]string)
	quit                   func() bool
	reopen                 func()
	appMenuAction          func(BoxedAction)
	willOpenAppMenu        func()
	validateAppMenuCommand func(BoxedAction) bool
	systemWake             func()
}

// jumpListState is the in-process jump list (destination_list.rs
// JumpList): the dock (taskbar) menu items and the recent workspace
// entries.
type jumpListState struct {
	dockMenus        []dockMenuItem
	recentWorkspaces [][]string
}

// dockMenuItem is one dock menu action (destination_list.rs
// DockMenuItem: name, description, action).
type dockMenuItem struct {
	name        string
	description string
	action      BoxedAction
}

// systemNotificationState is the notification bookkeeping
// (system_notifications.rs SystemNotificationState) bounded to the
// parts that do not require WinRT activation: the active-toast tag
// set, the response channel and the callback.
type systemNotificationState struct {
	mu           sync.Mutex
	activeTags   map[string]struct{}
	responseCh   chan SystemNotificationResponse
	responseCB   func(SystemNotificationResponse)
	drainStarted bool
	drainClosed  bool
}

func newSystemNotificationState() *systemNotificationState {
	return &systemNotificationState{
		activeTags: make(map[string]struct{}),
		responseCh: make(chan SystemNotificationResponse),
	}
}

// desktopHostState holds every Host field this ticket adds. Host-thread
// fields are documented as such; everything set from other goroutines
// is mutex-guarded.
type desktopHostState struct {
	// currentCursor is the platform-level HCURSOR (0 = none) and
	// cursorStyle its source value (WindowsPlatformState::current_cursor
	// + the Go query mirror). Host thread.
	currentCursor uintptr
	cursorStyle   CursorStyle
	// cursorVisible is the hide-until-moves flag shared with every
	// window's WM_SETCURSOR handler (cursor_visible: Arc<AtomicBool>).
	cursorVisible atomicBool
	// systemSettings holds the mouse-wheel parameters
	// (system_settings.rs WindowsSystemSettings). Host thread.
	systemSettings MouseWheelSettings
	// hasPackageIdentity records GetCurrentPackageFullName's answer.
	hasPackageIdentity bool
	// appIdentity is SetAppIdentity's record (identifier, name).
	appIdentifier string
	appName       string
	// suspendResumeNotify is the RegisterSuspendResumeNotification
	// handle; host thread.
	suspendResumeNotify uintptr
	// menus is the recorded application menus (set_menus/get_menus).
	// Host thread (mutations marshal to the host thread).
	menus []Menu
	// jumpList is the in-process jump-list state. Guarded by mu.
	jumpList jumpListState
	// notif is the notification state.
	notif *systemNotificationState
	// callbacks are the app-level callbacks. Guarded by mu.
	callbacks desktopCallbacks
	mu        sync.Mutex
}

// atomicBool is a tiny bool atomics helper (the Go analogue of the
// reference's Arc<AtomicBool>).
type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (b *atomicBool) Load() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.v
}

// Swap stores next and reports the previous value.
func (b *atomicBool) Swap(next bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	prev := b.v
	b.v = next
	return prev
}

func (b *atomicBool) Store(next bool) {
	b.mu.Lock()
	b.v = next
	b.mu.Unlock()
}

// initDesktopStateOnHostThread initializes the desktop state during
// host boot (WindowsPlatformState::new loads the arrow cursor; the
// package identity and appearance are read once). Runs on the host
// thread before the first window can exist.
func (h *Host) initDesktopStateOnHostThread() {
	h.desktop.currentCursor = loadCursorHandle(CursorArrow)
	h.desktop.cursorStyle = CursorArrow
	h.desktop.cursorVisible.Store(true)
	h.desktop.hasPackageIdentity = probeHasPackageIdentity()
	h.refreshMouseWheelSettingsOnHostThread()
}

// shutdownDesktopStateOnHostThread retires the desktop state at host
// shutdown: unregister the suspend/resume notification and stop the
// notification response drain (WindowsPlatform::drop unregisters the
// power notification before destroying the platform window).
func (h *Host) shutdownDesktopStateOnHostThread() {
	if h.desktop.suspendResumeNotify != 0 {
		procUnregisterSuspendResumeNotification.Call(h.desktop.suspendResumeNotify)
		h.desktop.suspendResumeNotify = 0
	}
	if n := h.desktop.notif; n != nil {
		n.mu.Lock()
		if !n.drainClosed {
			n.drainClosed = true
			close(n.responseCh)
		}
		n.mu.Unlock()
	}
}

// withDesktopCallback implements the reference's with_callback: take
// the callback out (so a reentrant registration or invocation replaces
// a dead value), invoke it with the lock released, put it back.
func (h *Host) withDesktopCallback[T any](project func(*desktopCallbacks) *T, invoke func(T)) {
	h.desktop.mu.Lock()
	cell := project(&h.desktop.callbacks)
	var cb T
	if cell != nil {
		cb = *cell
		var zero T
		*cell = zero
	}
	h.desktop.mu.Unlock()
	if isFuncZero(cb) {
		return
	}
	invoke(cb)
	h.desktop.mu.Lock()
	*project(&h.desktop.callbacks) = cb
	h.desktop.mu.Unlock()
}

// isFuncZero reports whether a func-typed value is nil. The generic
// withDesktopCallback works on any T; only nil-able values skip the
// invocation.
func isFuncZero(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case func():
		return t == nil
	case func() bool:
		return t == nil
	case func([]string):
		return t == nil
	case func(BoxedAction):
		return t == nil
	case func(BoxedAction) bool:
		return t == nil
	default:
		return false
	}
}

// setDesktopCallback installs one callback from any goroutine.
func (h *Host) setDesktopCallback[T any](project func(*desktopCallbacks) *T, cb T) {
	h.desktop.mu.Lock()
	*project(&h.desktop.callbacks) = cb
	h.desktop.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Cursor style, visibility and hit-testing
// ---------------------------------------------------------------------------

// cursorResourceID maps a cursor style to its system cursor resource
// (util.rs load_cursor's match arms). Styles that the reference's
// catch-all arm maps to the arrow (every not-listed style, including
// ClosedHand/OpenHand) load IDC_ARROW here too.
func cursorResourceID(style CursorStyle) uintptr {
	switch style {
	case CursorIBeam, CursorIBeamVertical:
		return idcIBeam
	case CursorCrosshair:
		return idcCross
	case CursorPointingHand, CursorDragLink:
		return idcHand
	case CursorResizeLeft, CursorResizeRight, CursorResizeLeftRight, CursorResizeColumn:
		return idcSizeWE
	case CursorResizeUp, CursorResizeDown, CursorResizeUpDown, CursorResizeRow:
		return idcSizeNS
	case CursorResizeUpLeftDownRight:
		return idcSizeNWSE
	case CursorResizeUpRightDownLeft:
		return idcSizeNESW
	case CursorOperationNotAllowed:
		return idcNo
	default:
		return idcArrow
	}
}

// loadCursorHandle loads one shared system cursor (LoadImageW with
// LR_DEFAULTSIZE|LR_SHARED; the reference caches per style and shares
// cursors, which the LR_SHARED flag provides).
func loadCursorHandle(style CursorStyle) uintptr {
	handle, _, _ := procLoadImageW.Call(
		0, cursorResourceID(style), imageCursor, 0, 0,
		uintptr(lrDefaultSize|lrShared))
	return handle
}

// SetCursorStyle sets the platform cursor (platform.rs
// set_cursor_style): load the system cursor, and when it changed, post
// WM_GPUI_CURSOR_STYLE_CHANGED to every window with the new handle and
// record it for later WM_SETCURSOR handling.
func (h *Host) SetCursorStyle(style CursorStyle) {
	h.runForegroundAsync(func() {
		hcursor := loadCursorHandle(style)
		if h.desktop.currentCursor == hcursor {
			return
		}
		for _, hwnd := range h.orderedWindowHwnds() {
			procPostMessageW.Call(hwnd, uintptr(wmGPUICursorStyle), 0, hcursor)
		}
		h.desktop.currentCursor = hcursor
		h.desktop.cursorStyle = style
	})
}

// CursorStyle returns the platform's recorded cursor style.
func (h *Host) CursorStyle() (CursorStyle, error) {
	return queryForeground(h, func() (CursorStyle, error) {
		return h.desktop.cursorStyle, nil
	})
}

// HideCursorUntilMouseMoves hides the cursor until the next mouse move
// (platform.rs hide_cursor_until_mouse_moves): flip the shared flag
// once, and for the first hovered window SetCursor(None) — the next
// WM_MOUSEMOVE/WM_NCMOUSEMOVE restores it.
func (h *Host) HideCursorUntilMouseMoves() {
	h.runForegroundAsync(func() {
		if !h.desktop.cursorVisible.Swap(false) {
			return
		}
		for _, hwnd := range h.orderedWindowHwnds() {
			w := h.windows[hwnd]
			if w == nil {
				continue
			}
			if w.hovered {
				procSetCursor.Call(0)
				break
			}
		}
	})
}

// IsCursorVisible reports the shared cursor-visibility flag.
func (h *Host) IsCursorVisible() bool {
	return h.desktop.cursorVisible.Load()
}

// orderedWindowHwnds lists the live window handles in creation order
// (deterministic records, never map iteration order).
func (h *Host) orderedWindowHwnds() []uintptr {
	type windowPair struct {
		hwnd uintptr
		id   uint64
	}
	pairs := make([]windowPair, 0, len(h.windows))
	for hwnd, w := range h.windows {
		if w != nil {
			pairs = append(pairs, windowPair{hwnd: hwnd, id: w.id})
		}
	}
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j-1].id > pairs[j].id; j-- {
			pairs[j-1], pairs[j] = pairs[j], pairs[j-1]
		}
	}
	out := make([]uintptr, len(pairs))
	for i, pair := range pairs {
		out[i] = pair.hwnd
	}
	return out
}

// ---------------------------------------------------------------------------
// Window controls (minimize / zoom / restore / activation)
// ---------------------------------------------------------------------------

// Minimize minimizes the window asynchronously (window.rs minimize:
// ShowWindowAsync(hwnd, SW_MINIMIZE)). The restore path reports the
// client size again through WM_SIZE (the existing minimized handling).
func (wh WindowHandle) Minimize() {
	wh.host.runForegroundAsync(func() {
		if wh.record() == nil {
			return
		}
		procShowWindowAsync.Call(wh.hwnd, swMinimize)
	})
}

// Zoom toggles the maximized state (window.rs zoom): a hidden window
// records the pending maximized state (applied when shown); a visible
// window maximizes or restores.
func (wh WindowHandle) Zoom() {
	wh.host.runForegroundAsync(func() {
		w := wh.record()
		if w == nil {
			return
		}
		visible, _, _ := procIsWindowVisible.Call(wh.hwnd)
		if visible == 0 {
			w.pendingMaximized = true
			return
		}
		operation := uintptr(swMaximize)
		if zoomed, _, _ := procIsZoomed.Call(wh.hwnd); zoomed != 0 {
			operation = swRestore
		}
		procShowWindowAsync.Call(wh.hwnd, operation)
	})
}

// IsMaximized reports whether the window is maximized.
func (wh WindowHandle) IsMaximized() (bool, error) {
	if wh.host == nil {
		return false, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (bool, error) {
		if wh.record() == nil {
			return false, ErrWindowClosed
		}
		zoomed, _, _ := procIsZoomed.Call(wh.hwnd)
		return zoomed != 0, nil
	})
}

// IsMinimized reports whether the window is minimized.
func (wh WindowHandle) IsMinimized() (bool, error) {
	if wh.host == nil {
		return false, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (bool, error) {
		if wh.record() == nil {
			return false, ErrWindowClosed
		}
		iconic, _, _ := procIsIconic.Call(wh.hwnd)
		return iconic != 0, nil
	})
}

// IsActive reports whether this window is the thread's active window
// (window.rs is_active: hwnd == GetActiveWindow()).
func (wh WindowHandle) IsActive() (bool, error) {
	if wh.host == nil {
		return false, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (bool, error) {
		if wh.record() == nil {
			return false, ErrWindowClosed
		}
		active, _, _ := procGetActiveWindowDesktop.Call()
		return active == wh.hwnd, nil
	})
}

// RequestAttention flashes the window's taskbar button once when it is
// not active (window.rs request_attention: FlashWindowEx FLASHW_ALL,
// uCount 1).
func (wh WindowHandle) RequestAttention() {
	wh.host.runForegroundAsync(func() {
		w := wh.record()
		if w == nil {
			return
		}
		if active, _, _ := procGetActiveWindowDesktop.Call(); active == wh.hwnd {
			return
		}
		info := flashwInfo{
			cbSize:  uint32(unsafe.Sizeof(flashwInfo{})),
			hwnd:    wh.hwnd,
			dwFlags: flashwAll,
			uCount:  1,
		}
		procFlashWindowEx.Call(uintptr(unsafe.Pointer(&info)))
	})
}

// MousePosition returns the cursor position in window-local logical
// pixels (window.rs mouse_position: GetCursorPos + ScreenToClient,
// converted with the window scale).
func (wh WindowHandle) MousePosition() (Point, error) {
	if wh.host == nil {
		return Point{}, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (Point, error) {
		w := wh.record()
		if w == nil {
			return Point{}, ErrWindowClosed
		}
		var cursor point
		if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor))); ok == 0 {
			return Point{}, fmt.Errorf("gpui: GetCursorPos failed")
		}
		if ok, _, _ := procScreenToClient.Call(wh.hwnd, uintptr(unsafe.Pointer(&cursor))); ok == 0 {
			return Point{}, fmt.Errorf("gpui: ScreenToClient failed")
		}
		return Point{X: float32(cursor.x) / w.scale, Y: float32(cursor.y) / w.scale}, nil
	})
}

// PlaySystemBell plays the Windows default beep (window.rs
// play_system_bell: MessageBeep(MB_OK)). It is user-audible; tests do
// not invoke it.
func (wh WindowHandle) PlaySystemBell() {
	wh.host.runForegroundAsync(func() {
		if wh.record() == nil {
			return
		}
		procMessageBeep.Call(mbOK)
	})
}

// IsHovered reports whether the cursor is over this window (window.rs
// is_hovered).
func (wh WindowHandle) IsHovered() (bool, error) {
	if wh.host == nil {
		return false, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (bool, error) {
		w := wh.record()
		if w == nil {
			return false, ErrWindowClosed
		}
		return w.hovered, nil
	})
}

// Appearance returns the window's cached appearance (window.rs
// appearance: the per-window record updated by ImmersiveColorSet).
func (wh WindowHandle) Appearance() (WindowAppearance, error) {
	if wh.host == nil {
		return WindowAppearanceLight, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (WindowAppearance, error) {
		w := wh.record()
		if w == nil {
			return WindowAppearanceLight, ErrWindowClosed
		}
		return w.appearance, nil
	})
}

// Display returns the window's current monitor record (the
// PlatformWindow display query).
func (wh WindowHandle) Display() (*DisplayInfo, error) {
	if wh.host == nil {
		return nil, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (*DisplayInfo, error) {
		w := wh.record()
		if w == nil {
			return nil, ErrWindowClosed
		}
		if w.display == nil {
			return nil, fmt.Errorf("gpui: the window has no monitor record")
		}
		display := *w.display
		return &display, nil
	})
}

// ---------------------------------------------------------------------------
// Background appearance (DWM behavior + the renderer clear path)
// ---------------------------------------------------------------------------

// SetBackgroundAppearance sets the window's background compositing mode
// (window.rs set_background_appearance): record the mode, then apply
// the DWM-side behavior — opaque/transparent/blurred through the
// undocumented SetWindowCompositionAttribute accent policy, the Mica
// modes through DwmSetWindowAttribute(DWMWA_SYSTEMBACKDROP_TYPE).
// The renderer-side behavior is ClearColorForBackground: the next
// present clears opaque modes to opaque white and every other mode to
// transparent, so the content behind the window survives the clear.
func (wh WindowHandle) SetBackgroundAppearance(appearance WindowBackgroundAppearance) {
	wh.host.runForegroundAsync(func() {
		w := wh.record()
		if w == nil {
			return
		}
		w.backgroundAppearance = appearance
		w.compositionRecord = applyBackgroundAppearance(wh.hwnd, appearance)
	})
}

// BackgroundAppearance returns the window's recorded background
// appearance (window.rs background_appearance).
func (wh WindowHandle) BackgroundAppearance() (WindowBackgroundAppearance, error) {
	if wh.host == nil {
		return WindowBackgroundOpaque, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (WindowBackgroundAppearance, error) {
		w := wh.record()
		if w == nil {
			return WindowBackgroundOpaque, ErrWindowClosed
		}
		return w.backgroundAppearance, nil
	})
}

// ClearColor returns the renderer clear color for the window's current
// background appearance: the color the present path clears to (the
// renderer half of set_background_appearance).
func (wh WindowHandle) ClearColor() (Color, error) {
	appearance, err := wh.BackgroundAppearance()
	if err != nil {
		return Color{}, err
	}
	return ClearColorForBackground(appearance), nil
}

// CompositionRecord returns the DWM application record for the current
// background appearance (diagnostics: which composition path ran and
// the build gate's answer).
func (wh WindowHandle) CompositionRecord() (string, error) {
	if wh.host == nil {
		return "", ErrWindowClosed
	}
	return queryForeground(wh.host, func() (string, error) {
		w := wh.record()
		if w == nil {
			return "", ErrWindowClosed
		}
		return w.compositionRecord, nil
	})
}

// applyBackgroundAppearance applies the DWM half of one background
// appearance and returns a diagnostic record. window.rs
// set_background_appearance:
//   - Opaque    -> set_window_composition_attribute(hwnd, None, 0)
//   - Transparent -> set_window_composition_attribute(hwnd, None, 2)
//   - Blurred   -> set_window_composition_attribute(hwnd, Some((0,0,0,0)), 4)
//   - Mica      -> dwm_set_window_composition_attribute(hwnd, 2)
//   - MicaAlt   -> dwm_set_window_composition_attribute(hwnd, 4)
func applyBackgroundAppearance(hwnd uintptr, appearance WindowBackgroundAppearance) string {
	switch appearance {
	case WindowBackgroundOpaque:
		return setWindowCompositionAttribute(hwnd, accentPolicy{accentState: 0, accentFlags: 2})
	case WindowBackgroundTransparent:
		return setWindowCompositionAttribute(hwnd, accentPolicy{accentState: 2, accentFlags: 2})
	case WindowBackgroundBlurred:
		return setWindowCompositionAttribute(hwnd, accentPolicy{
			// Acrylic: alpha 0 is promoted to 1 and the flags clear
			// (window.rs is_acrylic path).
			accentState:   4,
			accentFlags:   0,
			gradientColor: 1 << 24,
		})
	case WindowBackgroundMica:
		return dwmSetWindowBackdrop(hwnd, dwmsbtMainWindow)
	case WindowBackgroundMicaAlt:
		return dwmSetWindowBackdrop(hwnd, dwmsbtTabbedWindow)
	}
	return "unsupported-appearance"
}

// osBuildNumber reads the OS build through RtlGetVersion (the reference
// gates the undocumented composition calls on the build number).
func osBuildNumber() (uint32, error) {
	var info osVersionInfo
	info.dwOSVersionInfoSize = uint32(unsafe.Sizeof(info))
	r, _, callErr := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&info)))
	if int32(r) != 0 {
		return 0, fmt.Errorf("gpui: RtlGetVersion failed: %v", callErr)
	}
	return info.dwBuildNumber, nil
}

// setWindowCompositionAttribute applies an accent policy through
// user32's undocumented SetWindowCompositionAttribute (window.rs
// set_window_composition_attribute), gated on build >= 17763 and the
// export's presence. Returns the diagnostic record; failures are part
// of the record, never a silent no-op.
func setWindowCompositionAttribute(hwnd uintptr, policy accentPolicy) string {
	if err := procSetWindowCompositionAttribute.Find(); err != nil {
		return "accent: export missing (SetWindowCompositionAttribute)"
	}
	build, err := osBuildNumber()
	if err != nil {
		return "accent: " + err.Error()
	}
	if build < 17763 {
		return fmt.Sprintf("accent: skipped (build %d < 17763)", build)
	}
	data := windowCompositionAttribData{
		attrib: wcaAccentPolicy,
		pvData: &policy,
		cbData: unsafe.Sizeof(policy),
	}
	ok, _, callErr := procSetWindowCompositionAttribute.Call(
		hwnd, uintptr(unsafe.Pointer(&data)))
	if ok == 0 {
		return fmt.Sprintf("accent state=%d: failed: %v", policy.accentState, callErr)
	}
	return fmt.Sprintf("accent state=%d flags=%d color=0x%08x applied (build %d)",
		policy.accentState, policy.accentFlags, policy.gradientColor, build)
}

// dwmSetWindowBackdrop applies a system backdrop through
// DwmSetWindowAttribute (window.rs dwm_set_window_composition_attribute),
// gated on build >= 22621.
func dwmSetWindowBackdrop(hwnd uintptr, backdrop uint32) string {
	build, err := osBuildNumber()
	if err != nil {
		return "backdrop: " + err.Error()
	}
	if build < 22621 {
		return fmt.Sprintf("backdrop %d: skipped (build %d < 22621)", backdrop, build)
	}
	r, _, callErr := procDwmSetWindowAttribute.Call(
		hwnd, uintptr(dwmwaSystemBackdropType),
		uintptr(unsafe.Pointer(&backdrop)), unsafe.Sizeof(backdrop))
	if int32(r) < 0 {
		return fmt.Sprintf("backdrop %d: DwmSetWindowAttribute failed: 0x%08x %v", backdrop, uint32(r), callErr)
	}
	return fmt.Sprintf("backdrop %d applied (build %d)", backdrop, build)
}

// configureDWMDarkMode sets the immersive dark mode attribute for the
// window's built-in title bar (util.rs configure_dwm_dark_mode). The
// attribute is window-scoped; it is readable back through
// DwmGetWindowAttribute, which the tests use as the DWM-side
// verification.
func configureDWMDarkMode(hwnd uintptr, appearance WindowAppearance) error {
	dark := uint32(0)
	if appearance.IsDark() {
		dark = 1
	}
	r, _, callErr := procDwmSetWindowAttribute.Call(
		hwnd, uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	if int32(r) < 0 {
		return fmt.Errorf("gpui: DwmSetWindowAttribute(DWMWA_USE_IMMERSIVE_DARK_MODE) failed: 0x%08x %v", uint32(r), callErr)
	}
	return nil
}

// readDWMDarkMode reads the immersive dark mode attribute back.
func readDWMDarkMode(hwnd uintptr) (uint32, error) {
	var dark uint32
	r, _, callErr := procDwmGetWindowAttribute.Call(
		hwnd, uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	if int32(r) < 0 {
		return 0, fmt.Errorf("gpui: DwmGetWindowAttribute failed: 0x%08x %v", uint32(r), callErr)
	}
	return dark, nil
}

// ---------------------------------------------------------------------------
// Displays
// ---------------------------------------------------------------------------

// Displays enumerates the machine's monitors (platform.rs displays ->
// WindowsDisplay::displays). Like the reference this is a direct OS
// query on the calling goroutine; monitors whose info cannot be read
// are skipped with a recorded fault (the reference's filter_map).
func (h *Host) Displays() ([]DisplayInfo, error) {
	return collectDisplays(func(fault string) { h.recordFault(fault) })
}

// PrimaryDisplay returns the primary monitor (display.rs
// primary_monitor: MonitorFromPoint over the origin with
// MONITOR_DEFAULTTOPRIMARY).
func (h *Host) PrimaryDisplay() (*DisplayInfo, error) {
	monitor, _, _ := procMonitorFromPoint.Call(
		uintptr(unsafe.Pointer(&point{})), uintptr(monitorDefaultToPrimary))
	if monitor == 0 {
		return nil, fmt.Errorf("gpui: MonitorFromPoint found no primary monitor")
	}
	display, err := displayFromMonitor(monitor)
	if err != nil {
		return nil, err
	}
	return &display, nil
}

// collectDisplays enumerates monitors through EnumDisplayMonitors. The
// enumeration callback is created once per process (syscall.NewCallback
// is limited to 2000).
var (
	monitorEnumOnce sync.Once
	monitorEnumProc uintptr
)

func collectDisplays(recordFault func(string)) ([]DisplayInfo, error) {
	monitorEnumOnce.Do(func() {
		monitorEnumProc = syscall.NewCallback(func(hmonitor, hdc, place uintptr, data unsafe.Pointer) uintptr {
			monitors := (*[]uintptr)(data)
			*monitors = append(*monitors, hmonitor)
			return 1
		})
	})
	monitors := make([]uintptr, 0, 4)
	if ok, _, callErr := procEnumDisplayMonitors.Call(
		0, 0, monitorEnumProc, uintptr(unsafe.Pointer(&monitors))); ok == 0 {
		return nil, fmt.Errorf("gpui: EnumDisplayMonitors failed: %v", callErr)
	}
	displays := make([]DisplayInfo, 0, len(monitors))
	for _, monitor := range monitors {
		display, err := displayFromMonitor(monitor)
		if err != nil {
			recordFault(fmt.Sprintf("display 0x%x unavailable: %v", monitor, err))
			continue
		}
		displays = append(displays, display)
	}
	return displays, nil
}

// displayFromMonitor builds one Display record (display.rs
// WindowsDisplay::new): monitor and work rectangles, effective DPI and
// the v5 device-name UUID.
func displayFromMonitor(monitor uintptr) (DisplayInfo, error) {
	var info monitorInfoEx
	info.info.cbSize = uint32(unsafe.Sizeof(info))
	if ok, _, callErr := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return DisplayInfo{}, fmt.Errorf("gpui: GetMonitorInfoW failed: %v", callErr)
	}
	var dpiX, dpiY uint32
	if r, _, callErr := procGetDpiForMonitor.Call(
		monitor, uintptr(mdtEffectiveDPI),
		uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY))); int32(r) < 0 {
		return DisplayInfo{}, fmt.Errorf("gpui: GetDpiForMonitor failed: 0x%08x %v", uint32(r), callErr)
	}
	if dpiX != dpiY {
		// display.rs asserts dpi_x == dpi_y; a divergent monitor is a
		// real anomaly, not a value to average.
		return DisplayInfo{}, fmt.Errorf("gpui: monitor 0x%x reports DPI %dx%d", monitor, dpiX, dpiY)
	}
	scale := float32(dpiX) / float32(userDefaultScreenDPI)
	mon := info.info.rcMonitor
	work := info.info.rcWork
	return DisplayInfo{
		ID:   DisplayID(monitor),
		UUID: displayUUID(info.device[:]),
		Bounds: Bounds{
			Origin: Point{X: float32(mon.left) / scale, Y: float32(mon.top) / scale},
			Size: Size{
				Width:  float32(mon.right-mon.left) / scale,
				Height: float32(mon.bottom-mon.top) / scale,
			},
		},
		VisibleBounds: Bounds{
			Origin: Point{X: float32(work.left) / scale, Y: float32(work.top) / scale},
			Size: Size{
				Width:  float32(work.right-work.left) / scale,
				Height: float32(work.bottom-work.top) / scale,
			},
		},
		ScaleFactor: scale,
	}, nil
}

// displayUUID computes the monitor's v5 UUID over its device name
// (display.rs generate_uuid: Uuid::new_v5(NAMESPACE_DNS, name) where
// name is the device string's UTF-16 units in big-endian order).
func displayUUID(device []uint16) string {
	name := make([]byte, 0, len(device)*2)
	for _, unit := range device {
		if unit == 0 {
			break
		}
		name = append(name, byte(unit>>8), byte(unit))
	}
	var dnsNamespace = [16]byte{
		0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1,
		0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
	}
	sum := sha1.Sum(append(dnsNamespace[:], name...))
	// RFC 4122 v5: version nibble 5, variant 10.
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	var canonical [36]byte
	hex.Encode(canonical[0:8], sum[0:4])
	canonical[8] = '-'
	hex.Encode(canonical[9:13], sum[4:6])
	canonical[13] = '-'
	hex.Encode(canonical[14:18], sum[6:8])
	canonical[18] = '-'
	hex.Encode(canonical[19:23], sum[8:10])
	canonical[23] = '-'
	hex.Encode(canonical[24:36], sum[10:16])
	return string(canonical[:])
}

// updateWindowDisplay refreshes a window's monitor record (events.rs
// handle_display_change_msg: MonitorFromWindow with
// MONITOR_DEFAULTTONULL; no monitor means default processing, not an
// error crash).
func (w *hostWindow) updateWindowDisplay() bool {
	monitor, _, _ := procMonitorFromWindow.Call(w.hwnd, uintptr(monitorDefaultToNull))
	if monitor == 0 {
		w.host.recordFault("WM_DISPLAYCHANGE: no monitor detected")
		return false
	}
	display, err := displayFromMonitor(monitor)
	if err != nil {
		w.host.recordFault(fmt.Sprintf("WM_DISPLAYCHANGE: %v", err))
		return false
	}
	w.display = &display
	return true
}

// maybeUpdateDisplayOnMove re-associates the window with its monitor
// after a move (events.rs handle_move_msg: when the window center left
// the current display's bounds, re-resolve through
// MonitorFromWindow(MONITOR_DEFAULTTONULL) and update when the monitor
// really changed). Minimized windows have no monitor; they are skipped
// like the reference.
func (w *hostWindow) maybeUpdateDisplayOnMove() {
	if w.display == nil {
		return
	}
	// WM_MOVE reports the client origin in device pixels; the display
	// bounds are logical. Convert once with the window's scale (the
	// reference computes the center from the logical origin and size).
	size := w.clientSize()
	centerX := w.originX/w.scale + size.Width/2
	centerY := w.originY/w.scale + size.Height/2
	bounds := w.display.Bounds
	if centerX >= bounds.Origin.X && centerX <= bounds.Origin.X+bounds.Size.Width &&
		centerY >= bounds.Origin.Y && centerY <= bounds.Origin.Y+bounds.Size.Height {
		return
	}
	monitor, _, _ := procMonitorFromWindow.Call(w.hwnd, uintptr(monitorDefaultToNull))
	if monitor == 0 {
		return
	}
	if DisplayID(monitor) == w.display.ID {
		return
	}
	if display, err := displayFromMonitor(monitor); err == nil {
		w.display = &display
	} else {
		w.host.recordFault(fmt.Sprintf("display update on move: %v", err))
	}
}

// ---------------------------------------------------------------------------
// System appearance (the ImmersiveColorSet observer input)
// ---------------------------------------------------------------------------

// systemAppearanceProvider reads the system appearance. The reference
// (util.rs system_appearance) asks the WinRT UISettings for the
// foreground color and applies is_color_light; this port deviates by
// reading the Personalize registry value (AppsUseLightTheme) because
// the WinRT activation surface is not ported to the pure-stdlib host.
// The mapping stays the reference's: light foreground means Dark mode.
// The provider is a package seam so tests inject appearances without
// touching the user's theme.
var systemAppearanceProvider = registrySystemAppearance

// registrySystemAppearance reads HKCU\...\Themes\Personalize!
// AppsUseLightTheme (1/missing -> Light, 0 -> Dark).
func registrySystemAppearance() (WindowAppearance, error) {
	var value uint32
	valueSize := uint32(unsafe.Sizeof(value))
	var typ uint32
	var key uintptr
	r, _, _ := procRegOpenKeyExWDesktop.Call(
		hkcuRoot,
		uintptr(unsafe.Pointer(themePersonalizePathPtr)),
		0, uintptr(desktopKeyRead), uintptr(unsafe.Pointer(&key)))
	if int32(r) != 0 {
		// A missing key is a real read outcome, not a fake default: the
		// reference's unwrap_or_default() reports Light only on error,
		// and callers record the failure through this error.
		return WindowAppearanceLight, fmt.Errorf("gpui: opening the Personalize key failed with %d", int32(r))
	}
	defer procRegCloseKeyDesktop.Call(key)
	qr, _, _ := procRegQueryValueExWDesktop.Call(
		key,
		uintptr(unsafe.Pointer(appsUseLightThemePtr)),
		0, uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&valueSize)))
	if int32(qr) != 0 {
		return WindowAppearanceLight, fmt.Errorf("gpui: reading AppsUseLightTheme failed with %d", int32(qr))
	}
	if value == 0 {
		return WindowAppearanceDark, nil
	}
	return WindowAppearanceLight, nil
}

var (
	themePersonalizePathPtr, _ = syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	appsUseLightThemePtr, _    = syscall.UTF16PtrFromString("AppsUseLightTheme")

	// hkcuRoot is HKEY_CURRENT_USER's predefined-handle value
	// (0x80000001). RegOpenKeyExW accepts predefined handles directly.
	hkcuRoot = uintptr(0x80000001)

	// desktopKeyRead is KEY_READ (0x20019) under this slice's naming.
	desktopKeyRead = 0x20019
)

// WindowAppearance returns the system appearance (platform.rs
// window_appearance: a fresh system_appearance() read, Light on error).
func (h *Host) WindowAppearance() (WindowAppearance, error) {
	appearance, err := systemAppearanceProvider()
	if err != nil {
		h.recordFault(fmt.Sprintf("window appearance: %v", err))
		return WindowAppearanceLight, nil
	}
	return appearance, nil
}

// ---------------------------------------------------------------------------
// System settings (WM_SETTINGCHANGE's wparam path)
// ---------------------------------------------------------------------------

// MouseWheelSettings returns the recorded wheel parameters
// (system_settings.rs MouseWheelSettings), copied on the foreground
// thread.
func (h *Host) MouseWheelSettings() MouseWheelSettings {
	var settings MouseWheelSettings
	h.runForegroundSync(func() {
		settings = h.desktop.systemSettings
	})
	return settings
}

// refreshMouseWheelSettingsOnHostThread re-reads the wheel parameters
// through SystemParametersInfoW (SPI_GETWHEELSCROLLCHARS /
// SPI_GETWHEELSCROLLLINES).
func (h *Host) refreshMouseWheelSettingsOnHostThread() {
	chars, charsErr := querySystemParameter(spiGetWheelScrollChars)
	lines, linesErr := querySystemParameter(spiGetWheelScrollLines)
	if charsErr == nil {
		h.desktop.systemSettings.WheelScrollChars = chars
	}
	if linesErr == nil {
		h.desktop.systemSettings.WheelScrollLines = lines
	}
}

// updateSystemSettings applies one WM_SETTINGCHANGE action
// (system_settings.rs update): the wheel actions refresh the wheel
// parameters; other actions are ignored.
func (h *Host) updateSystemSettings(action uint32) {
	switch action {
	case spiGetWheelScrollLines, spiGetWheelScrollChars:
		h.refreshMouseWheelSettingsOnHostThread()
	}
}

// querySystemParameter reads one uint32 system parameter.
func querySystemParameter(action uint32) (uint32, error) {
	var value uint32
	r, _, callErr := procSystemParametersInfoW.Call(
		uintptr(action), 0, uintptr(unsafe.Pointer(&value)), 0)
	if r == 0 {
		return 0, fmt.Errorf("gpui: SystemParametersInfoW(%d) failed: %v", action, callErr)
	}
	return value, nil
}

// ---------------------------------------------------------------------------
// Power broadcast and session end (platform window messages)
// ---------------------------------------------------------------------------

// OnSystemWake registers the system wake callback and registers the
// suspend/resume notification once (platform.rs on_system_wake +
// RegisterSuspendResumeNotification on the platform window,
// DEVICE_NOTIFY_WINDOW_HANDLE).
func (h *Host) OnSystemWake(cb func()) error {
	if h.platformHWND == 0 {
		return ErrHostNotStarted
	}
	h.setDesktopCallback(func(c *desktopCallbacks) *func() { return &c.systemWake }, cb)
	if !h.runForegroundSync(func() {
		if h.desktop.suspendResumeNotify == 0 {
			notify, _, _ := procRegisterSuspendResumeNotification.Call(
				h.platformHWND, uintptr(deviceNotifyWindowHandle))
			h.desktop.suspendResumeNotify = notify
		}
	}) {
		return ErrHostStopped
	}
	return nil
}

// handlePowerBroadcastMsg is the platform window's WM_POWERBROADCAST
// policy (platform.rs handle_power_broadcast): PBT_APMRESUMEAUTOMATIC
// runs the system-wake callback; every power broadcast returns TRUE.
func (h *Host) handlePowerBroadcastMsg(wparam uintptr) uintptr {
	if uint32(wparam) == pbtApmResumeAutomatic {
		h.withDesktopCallback(
			func(c *desktopCallbacks) *func() { return &c.systemWake },
			func(cb func()) { cb() })
	}
	return 1
}

// OnQuit registers the quit callback (platform.rs on_quit). It runs
// after the message loop exits (run's tail) and on session end
// (handle_end_session); its return reports whether shutdown completed
// synchronously.
func (h *Host) OnQuit(cb func() bool) error {
	h.setDesktopCallback(func(c *desktopCallbacks) *func() bool { return &c.quit }, cb)
	return nil
}

// runQuitCallbackAfterLoop invokes the quit callback once after the
// message loop exits (platform.rs run: with_callback(quit) after
// GetMessageW returns false).
func (h *Host) runQuitCallbackAfterLoop() {
	h.withDesktopCallback(
		func(c *desktopCallbacks) *func() bool { return &c.quit },
		func(cb func() bool) { cb() })
}

// handleEndSessionMsg is the platform window's WM_GPUI_END_SESSION
// policy (platform.rs handle_end_session): run the quit callback; when
// it reports completed shutdown the process exits immediately like the
// reference's std::process::exit(0) (Windows may terminate the app as
// soon as the handler returns); otherwise post WM_QUIT and hope the
// orderly shutdown finishes first.
func (h *Host) handleEndSessionMsg() uintptr {
	completed := false
	h.withDesktopCallback(
		func(c *desktopCallbacks) *func() bool { return &c.quit },
		func(cb func() bool) { completed = cb() })
	if completed {
		os.Exit(0)
	}
	procPostQuitMessage.Call(0)
	return 0
}

// OnOpenUrls registers the URL-open callback (platform.rs on_open_urls).
// Windows registers it but nothing delivers to it: protocol
// activation does not feed an unpackaged process (the callback stays
// dormant, exactly like the reference's stored Cell).
func (h *Host) OnOpenUrls(cb func(urls []string)) error {
	h.setDesktopCallback(func(c *desktopCallbacks) *func([]string) { return &c.openURLs }, cb)
	return nil
}

// OnReopen registers the reopen callback (platform.rs on_reopen); the
// macOS reopen event has no Windows source, so the registration stays
// dormant (the reference registers it and never invokes it).
func (h *Host) OnReopen(cb func()) error {
	h.setDesktopCallback(func(c *desktopCallbacks) *func() { return &c.reopen }, cb)
	return nil
}

// ---------------------------------------------------------------------------
// Menus, dock menus and jump lists
// ---------------------------------------------------------------------------

// SetMenus records the application menus (platform.rs set_menus: the
// menus are stored owned; Windows draws no native menu bar). Keymap
// input is unused on Windows (the reference's _keymap).
func (h *Host) SetMenus(menus []Menu) {
	h.runForegroundAsync(func() {
		h.desktop.menus = append([]Menu(nil), menus...)
	})
}

// GetMenus returns the recorded menus (platform.rs get_menus: always
// Some on Windows).
func (h *Host) GetMenus() ([]Menu, bool) {
	var menus []Menu
	var ok bool
	done := h.runForegroundSync(func() {
		menus = append([]Menu(nil), h.desktop.menus...)
		ok = true
	})
	if !done {
		return nil, false
	}
	return menus, ok
}

// SetDockMenu builds the dock (taskbar) menu items and updates the jump
// list (platform.rs set_dock_menu -> set_dock_menus). Only action items
// are representable (destination_list.rs DockMenuItem::new); other
// items are dropped with a recorded fault like the reference's
// log_err. The shell commit runs in the background like the reference's
// background_executor spawn; its unported state is recorded, not
// faked.
func (h *Host) SetDockMenu(items []MenuItem) {
	built := h.buildDockMenuItems(items)
	h.desktop.mu.Lock()
	h.desktop.jumpList.dockMenus = built
	h.desktop.mu.Unlock()
	go func() {
		if _, err := h.commitJumpList(); err != nil {
			h.recordFault(fmt.Sprintf("jump list: %v", err))
		}
	}()
}

// UpdateJumpList updates the jump-list state and returns the entries
// the user removed from the list (platform.rs update_jump_list). The
// shell commit (ICustomDestinationList) is not ported, so the removed
// set is unavailable: the typed error is the honest outcome, while the
// in-process state update itself ran (the reference updates
// jump_list.borrow_mut() before spawning the commit).
func (h *Host) UpdateJumpList(menus []MenuItem, entries [][]string) ([][]string, error) {
	built := h.buildDockMenuItems(menus)
	h.desktop.mu.Lock()
	h.desktop.jumpList.dockMenus = built
	h.desktop.jumpList.recentWorkspaces = append([][]string(nil), entries...)
	h.desktop.mu.Unlock()
	if _, err := h.commitJumpList(); err != nil {
		return nil, err
	}
	return nil, nil
}

// commitJumpList is the shell destination-list commit
// (destination_list.rs update_jump_list: BeginList, recent folders,
// dock tasks, CommitList). The COM commit is not ported to the
// pure-stdlib host in this slice; the typed error records the pending
// row instead of reporting a fabricated removal list.
func (h *Host) commitJumpList() ([][]string, error) {
	return nil, ErrJumpListShellCommitUnavailable
}

// buildDockMenuItems maps menu items to dock menu records
// (destination_list.rs DockMenuItem::new): action items keep their name
// (and gain the "New Window" description the reference hard-codes),
// every other variant is rejected — recorded as a fault, not an error,
// because the reference logs and drops them.
func (h *Host) buildDockMenuItems(items []MenuItem) []dockMenuItem {
	built := make([]dockMenuItem, 0, len(items))
	for _, item := range items {
		if item.Kind != MenuItemAction {
			h.recordFault(fmt.Sprintf("dock menu: only action items are supported on Windows (dropped %q)", item.Name))
			continue
		}
		description := item.Name
		if item.Name == "New Window" {
			description = "Opens a new window"
		}
		built = append(built, dockMenuItem{
			name:        item.Name,
			description: description,
			action:      item.Action,
		})
	}
	return built
}

// PerformDockMenuAction dispatches one dock menu action by index
// (platform.rs perform_dock_menu_action: post WM_GPUI_DOCK_MENU_ACTION
// with the validation number to the platform window).
func (h *Host) PerformDockMenuAction(index int) {
	procPostMessageW.Call(h.platformHWND,
		uintptr(wmGPUIDockAction), uintptr(h.validationNumber), uintptr(index))
}

// handleDockMenuActionMsg is the platform window's
// WM_GPUI_DOCK_MENU_ACTION policy (platform.rs handle_dock_action_event):
// look the action up by index, clone it and deliver through the
// app-menu-action callback; a missing index records the fault and
// returns 1.
func (h *Host) handleDockMenuActionMsg(index uintptr) uintptr {
	h.desktop.mu.Lock()
	var action *BoxedAction
	if int(index) < len(h.desktop.jumpList.dockMenus) {
		boxed := h.desktop.jumpList.dockMenus[index].action
		action = &boxed
	}
	h.desktop.mu.Unlock()
	if action == nil {
		h.recordFault(fmt.Sprintf("dock menu: action for index %d not found", int(index)))
		return 1
	}
	// The clone keeps the callback's copy independent of the retained
	// menu item (boxed_clone in the reference).
	cloned := action.Clone()
	h.withDesktopCallback(
		func(c *desktopCallbacks) *func(BoxedAction) { return &c.appMenuAction },
		func(cb func(BoxedAction)) { cb(cloned) })
	return 0
}

// OnAppMenuAction registers the app menu action callback (platform.rs
// on_app_menu_action).
func (h *Host) OnAppMenuAction(cb func(action BoxedAction)) error {
	h.setDesktopCallback(func(c *desktopCallbacks) *func(BoxedAction) { return &c.appMenuAction }, cb)
	return nil
}

// OnWillOpenAppMenu registers the will-open callback (platform.rs
// on_will_open_app_menu). Windows has no native menu to open, so the
// registration stays dormant like the reference's stored Cell.
func (h *Host) OnWillOpenAppMenu(cb func()) error {
	h.setDesktopCallback(func(c *desktopCallbacks) *func() { return &c.willOpenAppMenu }, cb)
	return nil
}

// OnValidateAppMenuCommand registers the validation callback
// (platform.rs on_validate_app_menu_command); dormant on Windows.
func (h *Host) OnValidateAppMenuCommand(cb func(action BoxedAction) bool) error {
	h.setDesktopCallback(func(c *desktopCallbacks) *func(BoxedAction) bool { return &c.validateAppMenuCommand }, cb)
	return nil
}

// ---------------------------------------------------------------------------
// App identity and system notifications
// ---------------------------------------------------------------------------

// SetAppIdentity records the application identity and sets the
// process's explicit AppUserModelID (platform.rs set_app_identity).
// A packaged process already carries a system AUMID and is skipped
// exactly like the reference. The AUMID is process-scoped, not a
// user-visible system change.
func (h *Host) SetAppIdentity(identifier, name string) error {
	if h.desktop.hasPackageIdentity {
		return nil
	}
	id16, err := syscall.UTF16PtrFromString(identifier)
	if err != nil {
		return fmt.Errorf("gpui: app identifier %q: %v", identifier, err)
	}
	if r, _, callErr := procSetCurrentProcessExplicitAppUserModelID.Call(
		uintptr(unsafe.Pointer(id16))); int32(r) < 0 {
		return fmt.Errorf("gpui: SetCurrentProcessExplicitAppUserModelID failed: 0x%08x %v", uint32(r), callErr)
	}
	h.desktop.mu.Lock()
	h.desktop.appIdentifier = identifier
	h.desktop.appName = name
	h.desktop.mu.Unlock()
	return nil
}

// probeHasPackageIdentity reads the package identity through
// GetCurrentPackageFullName (platform.rs has_package_identity):
// ERROR_INSUFFICIENT_BUFFER means packaged, APPMODEL_ERROR_NO_PACKAGE
// means unpackaged, anything else warns and reports unpackaged.
func probeHasPackageIdentity() bool {
	var length uint32
	r, _, _ := procGetCurrentPackageFullName.Call(
		uintptr(unsafe.Pointer(&length)), 0)
	switch uint32(r) {
	case errorInsufficientBuffer:
		return true
	case appModelErrorNoPackage:
		return false
	default:
		return false
	}
}

// ShowSystemNotification shows one notification (platform.rs
// show_system_notification -> SystemNotificationState::show). Without
// an app identity the reference records a warning and shows nothing —
// a source-supported outcome returned as success. With an identity the
// reference activates the WinRT toast notifier, which this pure-stdlib
// port has not ported: the typed ErrToastNotifierUnavailable is the
// honest pending row, never fake success.
func (h *Host) ShowSystemNotification(notification SystemNotification) error {
	_, err := queryForeground(h, func() (struct{}, error) {
		h.desktop.mu.Lock()
		identifier := h.desktop.appIdentifier
		h.desktop.mu.Unlock()
		if !h.desktop.hasPackageIdentity && identifier == "" {
			h.recordFault("system notification: cannot show without an app identity; call SetAppIdentity during startup (source-supported no-op)")
			return struct{}{}, nil
		}
		return struct{}{}, ErrToastNotifierUnavailable
	})
	return err
}

// DismissSystemNotification dismisses the notification with the given
// tag (platform.rs dismiss_system_notification). With no active toast
// the call is a no-op exactly like the reference's missing-tag branch.
func (h *Host) DismissSystemNotification(tag string) {
	h.desktop.mu.Lock()
	_, active := h.desktop.notif.activeTags[tag]
	delete(h.desktop.notif.activeTags, tag)
	h.desktop.mu.Unlock()
	_ = active // no toast can be active until the notifier is ported
}

// OnSystemNotificationResponse registers the response callback and
// starts the response drain (system_notifications.rs on_response: the
// receiver task runs on the foreground executor and takes the callback
// out for each delivery so it can re-enter the platform). Responses
// arrive from toast activations; until the notifier is ported the drain
// is dormant but fully wired.
func (h *Host) OnSystemNotificationResponse(cb func(SystemNotificationResponse)) error {
	n := h.desktop.notif
	n.mu.Lock()
	n.responseCB = cb
	startDrain := !n.drainStarted && !n.drainClosed
	n.drainStarted = true
	ch := n.responseCh
	n.mu.Unlock()
	if startDrain {
		go h.drainNotificationResponses(ch)
	}
	return nil
}

// drainNotificationResponses delivers queued responses to the callback
// on the foreground thread (the executor.spawn analogue: the callback
// runs on the foreground dispatcher, never on the channel reader).
func (h *Host) drainNotificationResponses(ch chan SystemNotificationResponse) {
	for response := range ch {
		h.Post(func() {
			n := h.desktop.notif
			n.mu.Lock()
			cb := n.responseCB
			n.mu.Unlock()
			if cb != nil {
				cb(response)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Verified unsupported / no-op outcomes
// ---------------------------------------------------------------------------

// ThermalState returns the nominal thermal state (platform.rs
// thermal_state: always ThermalState::Nominal on Windows).
func (h *Host) ThermalState() ThermalState { return ThermalStateNominal }

// OnThermalStateChange registers the thermal-state callback. Windows
// never reports thermal state, so the registration is a no-op body
// exactly like the reference's empty function.
func (h *Host) OnThermalStateChange(cb func()) error {
	_ = cb // source: `fn on_thermal_state_change(&self, _: Box<dyn FnMut()>) {}`
	return nil
}

// ActivateApp activates the application (platform.rs activate). The
// Windows body is empty; the argument is accepted for parity.
func (h *Host) ActivateApp(ignoringOtherApps bool) {
	_ = ignoringOtherApps // source: `fn activate(&self, _ignoring_other_apps: bool) {}`
}

// HideApp hides the application (platform.rs hide). Empty body on
// Windows.
func (h *Host) HideApp() {}

// HideOtherApps reproduces the reference's unimplemented!() panic
// (platform.rs hide_other_apps: `unimplemented!()`). Panicking is the
// source behavior; callers must not reach this path on Windows.
func (h *Host) HideOtherApps() {
	panic("gpui: hide_other_apps is not implemented on Windows (reference: unimplemented!())")
}

// UnhideOtherApps reproduces the reference's unimplemented!() panic
// (platform.rs unhide_other_apps).
func (h *Host) UnhideOtherApps() {
	panic("gpui: unhide_other_apps is not implemented on Windows (reference: unimplemented!())")
}

// PathForAuxiliaryExecutable reproduces the reference's bail
// (platform.rs path_for_auxiliary_executable: "not yet implemented").
func (h *Host) PathForAuxiliaryExecutable(name string) (string, error) {
	_ = name
	return "", ErrAuxiliaryExecutableUnavailable
}

// RegisterURLScheme reproduces the reference's task error
// (platform.rs register_url_scheme: "register_url_scheme unimplemented").
func (h *Host) RegisterURLScheme(scheme string) error {
	_ = scheme
	return ErrURLSchemeRegistrationUnsupported
}

// ButtonLayout reports the window-control button layout when the
// platform supports one. Windows does not override the platform trait's
// button_layout (gpui_windows/src/platform.rs has no arm for it), so the
// query reports the trait default: no layout.
func (h *Host) ButtonLayout() (WindowButtonLayout, bool) {
	return WindowButtonLayout{}, false
}

// OnButtonLayoutChanged registers the button-layout observer. Windows
// does not override the trait's on_button_layout_changed (an empty
// default body), so the registration is stored and never fires.
func (h *Host) OnButtonLayoutChanged(cb func()) error {
	_ = cb // source: trait default `fn on_button_layout_changed(&self, _callback) {}`
	return nil
}

// Faults returns the recorded host faults (diagnostics; wake-token
// mismatches, unavailable desktop outcomes and marshaling failures).
func (h *Host) Faults() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.faults...)
}

// PlatformHwnd returns the platform (dispatcher) window's HWND: the
// window that owns the private messages (wake, dock actions, session
// end) and the suspend/resume registration. It exists for diagnostics
// and for controlled-event tests that deliver those messages directly;
// it is a lease the host destroys at shutdown.
func (h *Host) PlatformHwnd() uintptr { return h.platformHWND }

// ---------------------------------------------------------------------------
// Window-proc message policies (events.rs)
// ---------------------------------------------------------------------------

// handleSetCursorMsg is WM_SETCURSOR (events.rs handle_set_cursor):
// when the window is disabled, or the hit-test area is a resize edge
// (DefWindowProc owns the sizing cursors there), fall to default
// processing; otherwise show the platform cursor — or none while the
// cursor is hidden — and consume. The reference returns Some(0) on the
// consumed path.
func (w *hostWindow) handleSetCursorMsg(lp uintptr) (uintptr, bool) {
	enabled, _, _ := procIsWindowEnabledDesktop.Call(w.hwnd)
	if enabled == 0 {
		return 0, false
	}
	switch loword(lp) {
	case htLeft, htRight, htTop, htTopLeft, htTopRight, htBottom, htBottomLeft, htBottomRight:
		return 0, false
	}
	var cursor uintptr
	if w.host.desktop.cursorVisible.Load() {
		cursor = w.cursor
	}
	procSetCursor.Call(cursor)
	return 0, true
}

// restoreCursorAfterHide clears the hidden flag and restores the
// cursor immediately (events.rs restore_cursor_after_hide); it runs on
// every WM_MOUSEMOVE and WM_NCMOUSEMOVE.
func (w *hostWindow) restoreCursorAfterHide() {
	if !w.host.desktop.cursorVisible.Swap(true) {
		procSetCursor.Call(w.cursor)
	}
}

// startMouseLeaveTracking marks the window hovered and requests
// WM_MOUSELEAVE / WM_NCMOUSELEAVE delivery (events.rs
// start_tracking_mouse). Only the cursor slice's tracking is installed
// here; mouse input routing belongs to the input tickets.
func (w *hostWindow) startMouseLeaveTracking(flags uint32) {
	if w.hovered {
		return
	}
	w.hovered = true
	event := trackMouseEventStruct{
		cbSize:      uint32(unsafe.Sizeof(trackMouseEventStruct{})),
		dwFlags:     flags,
		hwndTrack:   w.hwnd,
		dwHoverTime: hoverDefault,
	}
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&event)))
}

// handleMouseLeaveMsg is WM_MOUSELEAVE / WM_NCMOUSELEAVE (events.rs
// handle_mouse_leave_msg): the window stops being hovered, the shared
// cursor-visibility flag clears for tight is_cursor_visible semantics,
// and the hover observer reports false.
func (w *hostWindow) handleMouseLeaveMsg() uintptr {
	w.hovered = false
	w.host.desktop.cursorVisible.Store(true)
	if w.onHoverStatusChange != nil {
		w.onHoverStatusChange(false)
	}
	return 0
}

// handleCursorStyleChangedMsg is WM_GPUI_CURSOR_STYLE_CHANGED (events.rs
// handle_cursor_changed): record the window's cursor handle (0 = none)
// and re-show it when the some/none presence flipped.
func (w *hostWindow) handleCursorStyleChangedMsg(lp uintptr) uintptr {
	hadCursor := w.cursor != 0
	w.cursor = lp
	if hadCursor != (w.cursor != 0) {
		procSetCursor.Call(w.cursor)
	}
	return 0
}

// handleSettingChangeMsg is WM_SETTINGCHANGE (events.rs
// handle_system_settings_changed): a parameter action refreshes the
// tracked settings and the frame border; a zero action is the theme
// change path.
func (w *hostWindow) handleSettingChangeMsg(wparam uintptr, lparam unsafe.Pointer) (uintptr, bool) {
	if wparam != 0 {
		w.updateBorderOffset()
		w.host.updateSystemSettings(uint32(wparam))
		return 0, true
	}
	return w.handleSystemThemeChanged(lparam)
}

// handleSystemThemeChanged is the ImmersiveColorSet path (events.rs
// handle_system_theme_changed): read the area name; on
// "ImmersiveColorSet" re-read the system appearance and, when it
// changed, update the record, notify the observer and re-apply the DWM
// dark mode. The `?`-shaped short-circuits of the reference map to
// (result, handled=false) so default processing runs.
func (w *hostWindow) handleSystemThemeChanged(lparam unsafe.Pointer) (uintptr, bool) {
	parameter := lparamString(lparam)
	if parameter == "" {
		return 0, true
	}
	if parameter != "ImmersiveColorSet" {
		return 0, true
	}
	appearance, err := systemAppearanceProvider()
	if err != nil {
		w.host.recordFault(fmt.Sprintf("system appearance: %v", err))
		return 0, false
	}
	if appearance == w.appearance {
		return 0, true
	}
	w.appearance = appearance
	if w.onAppearanceChanged == nil {
		// The reference's callback.take()? — no observer, default
		// processing.
		return 0, false
	}
	w.onAppearanceChanged()
	if err := configureDWMDarkMode(w.hwnd, appearance); err != nil {
		w.host.recordFault(fmt.Sprintf("dwm dark mode: %v", err))
	}
	return 0, true
}

// lparamString reads a Win32 PCWSTR from a message's lparam. The value
// is only valid for the duration of the synchronous dispatch, so it is
// read immediately (never retained).
func lparamString(ptr unsafe.Pointer) string {
	if ptr == nil {
		return ""
	}
	// Bounded scan for the terminator (a system-provided string).
	runes := make([]uint16, 0, 64)
	for i := uintptr(0); ; i += 2 {
		if i > 4096 {
			return ""
		}
		unit := *(*uint16)(unsafe.Add(ptr, i))
		if unit == 0 {
			break
		}
		runes = append(runes, unit)
	}
	return syscall.UTF16ToString(runes)
}

// handleActivateDesktopState is the WM_ACTIVATE part that runs
// synchronously (events.rs handle_activate_msg): deactivation restores
// the shared cursor flag; activation resets the modifier dedup state.
// The synthesized ModifiersChanged event is delivered at the next safe
// foreground entry: the reference invokes the input callback directly
// in the message handler, but a WM_ACTIVATE that arrives inside
// CreateWindowExW/ShowWindow would re-enter App.OpenWindow before the
// logical window exists, so the port posts it (platform contract:
// "Reentrant mutating input is copied and delivered at the next safe
// entry") — the app still sees one ModifiersChanged per activation,
// after the reset, on the foreground thread.
func (w *hostWindow) handleActivateDesktopState(wparam uintptr) {
	activated := loword(wparam) != waInactive
	if !activated {
		w.host.desktop.cursorVisible.Store(true)
		return
	}
	// Windows does not always deliver key-ups to unfocused windows;
	// activation resets the dedup state so Alt-Tab back cannot leave a
	// stale modifier report (last_reported_modifiers/capslock).
	w.lastReportedModifiers = nil
	w.lastReportedCapslock = nil
	if w.onKey != nil {
		event := &ModifiersChangedEvent{
			Modifiers: win32CurrentModifiers(),
			Capslock:  win32CurrentCapslock(),
		}
		cb := w.onKey
		w.host.post(func() { cb(event) })
	}
}

// handleWindowVisibilityChanged is WM_SHOWWINDOW (events.rs
// handle_window_visibility_changed): a shown window draws; default
// processing always continues (the reference returns None).
func (w *hostWindow) handleWindowVisibilityChanged(wparam uintptr) {
	if wparam == 1 && w.onRedraw != nil {
		w.onRedraw()
	}
}

// handleEndSessionWindowMsg is WM_ENDSESSION on an app window (events.rs
// handle_end_session_msg): the session really ending forwards to the
// platform window's WM_GPUI_END_SESSION handler and consumes; a vetoed
// end passes through.
func (w *hostWindow) handleEndSessionWindowMsg(wparam uintptr) (uintptr, bool) {
	if wparam == 0 {
		return 0, false
	}
	procSendMessageW.Call(w.host.platformHWND,
		uintptr(wmGPUIEndSession), uintptr(w.host.validationNumber), 0)
	return 0, true
}

// handleMouseActivateMsg is WM_MOUSEACTIVATE (events.rs WM_MOUSEACTIVATE
// arm): eagerly activate, so a click handler that consumes the press
// cannot leave the window un-activated (DefWindowProc only activates
// when it sees the WM_NCLBUTTONDOWN).
func (w *hostWindow) handleMouseActivateMsg() uintptr {
	return uintptr(maActivate)
}

// handleDisplayChangeMsg is WM_DISPLAYCHANGE (events.rs
// handle_display_change_msg): re-resolve the window's monitor; no
// monitor means default processing.
func (w *hostWindow) handleDisplayChangeMsg() (uintptr, bool) {
	if !w.updateWindowDisplay() {
		return 0, false
	}
	return 0, true
}

// filepathParent computes a path's parent, reporting false for paths
// without one (Path::parent's None: drive roots).
func filepathParent(path string) (string, bool) {
	dir := filepath.Dir(path)
	if dir == path {
		return "", false
	}
	return dir, true
}
