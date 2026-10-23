//go:build windows

package gpui

// The Windows host half of the client-drawn titlebar (titlebar.go) and
// the DirectComposition window style. Ported from the pinned CE
// reference crates/gpui_windows/src:
//
//   - window.rs:456-493 WindowsWindow::new: hide_title_bar from
//     TitlebarOptions::appears_transparent, the window styles and
//     WS_EX_NOREDIRECTIONBITMAP.
//   - events.rs:783-805 handle_calc_client_size (WM_NCCALCSIZE).
//   - events.rs:966-1032 handle_hit_test_msg (WM_NCHITTEST): the window
//     control area mapping and the top resize border.
//   - events.rs:1034-1166 the non-client mouse handlers.
//   - events.rs:309-321 handle_size_move_loop_exit's dragging part and
//     window.rs:1014-1039 start_window_move.
//   - events.rs:873-880 handle_create_msg and :1745-1778 the frame
//     thickness helpers and notify_frame_changed.
//
// Deviations, all deliberate:
//
//   - The non-client mouse messages reach element dispatch only for a
//     window whose titlebar is hidden. The pin routes them for every
//     window; doing so here would deliver presses on the system titlebar
//     to the app and change existing windows.
//   - start_window_move posts WM_NCLBUTTONDOWN with the cursor position
//     packed into lparam (the pin passes a pointer to a stack value
//     there), and that synthetic press goes straight to DefWindowProc
//     instead of being dispatched to elements first.
//   - There is no fullscreen state in the port, so the pin's fullscreen
//     early-outs are absent.

import "unsafe"

// Win32 messages, styles and metrics of the titlebar (winuser.h).
const (
	wmNCCalcSize      = 0x0083 // WM_NCCALCSIZE
	wmNCHitTest       = 0x0084 // WM_NCHITTEST
	wmNcLButtonDown   = 0x00A1 // WM_NCLBUTTONDOWN
	wmNcLButtonUp     = 0x00A2 // WM_NCLBUTTONUP
	wmNcLButtonDblClk = 0x00A3 // WM_NCLBUTTONDBLCLK
	wmNcRButtonDown   = 0x00A4 // WM_NCRBUTTONDOWN
	wmNcRButtonUp     = 0x00A5 // WM_NCRBUTTONUP
	wmNcMButtonDown   = 0x00A7 // WM_NCMBUTTONDOWN
	wmNcMButtonUp     = 0x00A8 // WM_NCMBUTTONUP
	wmExitSizeMove    = 0x0232 // WM_EXITSIZEMOVE

	wsExNoRedirectionBitmap = 0x00200000 // WS_EX_NOREDIRECTIONBITMAP

	smCxSizeFrame    = 32 // SM_CXSIZEFRAME
	smCySizeFrame    = 33 // SM_CYSIZEFRAME
	smCxPaddedBorder = 92 // SM_CXPADDEDBORDER

	swpNoCopyBits     = 0x0100 // SWP_NOCOPYBITS
	swpNoOwnerZOrder  = 0x0200 // SWP_NOOWNERZORDER (= SWP_NOREPOSITION)
	swpNoSendChanging = 0x0400 // SWP_NOSENDCHANGING
)

var procGetSystemMetricsForDpi = modUser32.NewProc("GetSystemMetricsForDpi")

// win32WindowStyles returns the CreateWindowExW style and extended style
// for a normal window (window.rs WindowsWindow::new): WS_SYSMENU, plus
// WS_THICKFRAME|WS_MAXIMIZEBOX when resizable and WS_MINIMIZEBOX when
// minimizable, with WS_EX_APPWINDOW. Deviations: WS_CAPTION is explicit
// (the pin gets it implicitly as an overlapped window; a hidden titlebar
// removes it through WM_NCCALCSIZE, which keeps the frame, snap and the
// Windows 11 rounded corners), and WS_EX_NOREDIRECTIONBITMAP is opt-in
// through WindowOptions.DirectComposition rather than the default.
func win32WindowStyles(opts WindowOptions) (style, exStyle uintptr) {
	style = wsSysMenu | wsCaption
	if opts.Resizable {
		style |= wsThickFrame | wsMaximizeBox
	}
	if opts.MinimizeBox {
		style |= wsMinimizeBox
	}
	exStyle = wsExAppWindow
	if opts.DirectComposition {
		exStyle |= wsExNoRedirectionBitmap
	}
	return style, exStyle
}

// frameThicknessX is events.rs get_frame_thicknessx: the resize handle
// plus the padded border, in device pixels at dpi.
func frameThicknessX(dpi uint32) int32 {
	resize, _, _ := procGetSystemMetricsForDpi.Call(smCxSizeFrame, uintptr(dpi))
	padding, _, _ := procGetSystemMetricsForDpi.Call(smCxPaddedBorder, uintptr(dpi))
	return int32(resize) + int32(padding)
}

// frameThicknessY is events.rs get_frame_thicknessy.
func frameThicknessY(dpi uint32) int32 {
	resize, _, _ := procGetSystemMetricsForDpi.Call(smCySizeFrame, uintptr(dpi))
	padding, _, _ := procGetSystemMetricsForDpi.Call(smCxPaddedBorder, uintptr(dpi))
	return int32(resize) + int32(padding)
}

// notifyFrameChanged is events.rs notify_frame_changed: SWP_FRAMECHANGED
// without moving, sizing, activating or reordering, so Windows sends
// WM_NCCALCSIZE again.
func notifyFrameChanged(hwnd uintptr) {
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		uintptr(swpFrameChanged|swpNoActivate|swpNoCopyBits|swpNoMove|
			swpNoOwnerZOrder|swpNoSendChanging|swpNoSize|swpNoZOrder))
}

// windowDPI returns the window's DPI (GetDpiForWindow).
func (w *hostWindow) windowDPI() uint32 {
	dpi, _, _ := procGetDpiForWindow.Call(w.hwnd)
	if dpi == 0 {
		return userDefaultScreenDPI
	}
	return uint32(dpi)
}

// isZoomed reports whether the window is maximized (IsZoomed).
func (w *hostWindow) isZoomed() bool {
	zoomed, _, _ := procIsZoomed.Call(w.hwnd)
	return zoomed != 0
}

// screenToClient converts a non-client message's screen position
// (signed words of lparam) to client device pixels. A failed conversion
// keeps the screen point, like the pin's log_err.
func (w *hostWindow) screenToClient(lp uintptr) point {
	p := point{x: signedLoword(lp), y: signedHiword(lp)}
	procScreenToClient.Call(w.hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

// handleTitlebarMsg routes the titlebar messages. handled false sends
// the message to DefWindowProcW.
func (w *hostWindow) handleTitlebarMsg(message uint32, wparam uintptr, lparam unsafe.Pointer) (uintptr, bool) {
	lp := uintptr(lparam)
	switch message {
	case wmNCCalcSize:
		return w.handleCalcClientSize(wparam, lparam)
	case wmNCHitTest:
		return w.handleHitTestMsg(lp)
	case wmExitSizeMove:
		w.handleSizeMoveLoopExit()
		return 0, false
	}
	if !w.hideTitleBar {
		return 0, false
	}
	switch message {
	case wmNcLButtonDown, wmNcLButtonDblClk:
		// A double click is a second single click: the click count is
		// tracked here, and an unhandled double click on HTCAPTION falls
		// to DefWindowProc, which toggles maximize (events.rs:117-122).
		return w.handleNcMouseDownMsg(MouseButtonLeft, wparam, lp)
	case wmNcRButtonDown:
		return w.handleNcMouseDownMsg(MouseButtonRight, wparam, lp)
	case wmNcMButtonDown:
		return w.handleNcMouseDownMsg(MouseButtonMiddle, wparam, lp)
	case wmNcLButtonUp:
		return w.handleNcMouseUpMsg(MouseButtonLeft, wparam, lp)
	case wmNcRButtonUp:
		return w.handleNcMouseUpMsg(MouseButtonRight, wparam, lp)
	case wmNcMButtonUp:
		return w.handleNcMouseUpMsg(MouseButtonMiddle, wparam, lp)
	}
	return 0, false
}

// handleCalcClientSize is events.rs:783-805 handle_calc_client_size: with
// the titlebar hidden, let DefWindowProc compute the client rect, then
// put its top back at the window's top so the client covers the caption.
// A maximized window extends past the monitor by the frame, so its top
// moves down by the frame thickness.
func (w *hostWindow) handleCalcClientSize(wparam uintptr, lparam unsafe.Pointer) (uintptr, bool) {
	if !w.hideTitleBar || wparam == 0 {
		return 0, false
	}
	// NCCALCSIZE_PARAMS starts with rgrc[0], the proposed window rect.
	proposed := (*rect)(lparam)
	savedTop := proposed.top
	result, _, _ := procDefWindowProcW.Call(w.hwnd, wmNCCalcSize, wparam, uintptr(lparam))
	proposed.top = savedTop
	if w.isZoomed() {
		proposed.top += frameThicknessX(w.windowDPI())
	}
	return result, true
}

// titlebarTopResizeHitTest is the top-border half of events.rs:996-1029:
// a hidden titlebar leaves DefWindowProc a 1px top edge, so the top
// frame-thickness band of a resizable, non-maximized window answers
// HTTOPLEFT, HTTOP or HTTOPRIGHT. x and y are client device pixels;
// frameX/frameY are the pin's frame_x/frame_y (note the pin's crossed
// helper names); windowWidth is the window rect's width.
func titlebarTopResizeHitTest(x, y, frameX, frameY, windowWidth int32, resizable, maximized bool) (uintptr, bool) {
	if !resizable || maximized || y < 0 || y > frameY {
		return 0, false
	}
	// The x axis actually goes from -frame_x to 0.
	if x <= 0 {
		return htTopLeft, true
	}
	// The right bound of a RECT is exclusive, and the bounds include the
	// padding frames on both sides.
	right := windowWidth - 1
	if right-2*frameX <= x {
		return htTopRight, true
	}
	return htTop, true
}

// handleHitTestMsg is events.rs:966-1032 handle_hit_test_msg: the window
// control area under the cursor maps to its hit-test code; a hidden
// titlebar also hit-tests the top resize border first. handled false
// leaves the answer to DefWindowProc.
func (w *hostWindow) handleHitTestMsg(lp uintptr) (uintptr, bool) {
	var (
		code uintptr
		have bool
	)
	if w.hitTestWindowControl != nil {
		p := w.screenToClient(lp)
		if area, ok := w.hitTestWindowControl(win32LogicalPoint(p.x, p.y, w.scale)); ok {
			code, have = windowControlHitTestCode(area, w.movable, w.resizable, w.minimizable)
		}
	}
	if !w.hideTitleBar {
		// The system draws the titlebar: only the control areas answer.
		return code, have
	}
	dpi := w.windowDPI()
	frameY := frameThicknessX(dpi)
	frameX := frameThicknessY(dpi)
	p := w.screenToClient(lp)
	var wr rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&wr)))
	if top, ok := titlebarTopResizeHitTest(p.x, p.y, frameX, frameY, wr.right-wr.left, w.resizable, w.isZoomed()); ok {
		return top, true
	}
	return code, have
}

// handleNcMouseMoveMsg is events.rs:1034-1055 handle_nc_mouse_move_msg's
// delivery: the move goes to element dispatch at its client position.
// It reports whether the application handled it.
func (w *hostWindow) handleNcMouseMoveMsg(lp uintptr) bool {
	input := w.onMouseInput
	if input == nil {
		return false
	}
	p := w.screenToClient(lp)
	return input(&MouseMoveEvent{
		Position:  win32LogicalPoint(p.x, p.y, w.scale),
		Modifiers: win32CurrentModifiers(),
	})
}

// handleNcMouseDownMsg is events.rs:1057-1104 handle_nc_mouse_down_msg:
// the press goes to element dispatch first; an unhandled left press on a
// caption button is remembered and consumed, because the button acts on
// release (handleNcMouseUpMsg) and DefWindowProc would otherwise run its
// own button tracking.
func (w *hostWindow) handleNcMouseDownMsg(button MouseButton, wparam, lp uintptr) (uintptr, bool) {
	if button == MouseButtonLeft && w.dragging && wparam == htCaption {
		// StartWindowMove's synthetic press: straight to the move loop.
		return 0, false
	}
	if input := w.onMouseInput; input != nil {
		p := w.screenToClient(lp)
		clickCount := w.clickState().update(button, p.x, p.y)
		handled := input(&MouseDownEvent{
			Button:     button,
			Position:   win32LogicalPoint(p.x, p.y, w.scale),
			Modifiers:  win32CurrentModifiers(),
			ClickCount: clickCount,
		})
		if handled {
			return 0, true
		}
	}
	if button == MouseButtonLeft {
		switch wparam {
		case htMinButton, htMaxButton, htClose:
			w.ncButtonPressed = wparam
			return 0, true
		}
	}
	return 0, false
}

// handleNcMouseUpMsg is events.rs:1106-1166 handle_nc_mouse_up_msg: the
// release goes to element dispatch first; otherwise a left release on
// the caption button the press went down on performs it.
func (w *hostWindow) handleNcMouseUpMsg(button MouseButton, wparam, lp uintptr) (uintptr, bool) {
	if input := w.onMouseInput; input != nil {
		p := w.screenToClient(lp)
		handled := input(&MouseUpEvent{
			Button:     button,
			Position:   win32LogicalPoint(p.x, p.y, w.scale),
			Modifiers:  win32CurrentModifiers(),
			ClickCount: 1,
		})
		if handled {
			return 0, true
		}
	}
	lastPressed := w.ncButtonPressed
	w.ncButtonPressed = 0
	if button != MouseButtonLeft || lastPressed == 0 || wparam != lastPressed {
		return 0, false
	}
	switch wparam {
	case htMinButton:
		if w.minimizable {
			procShowWindowAsync.Call(w.hwnd, swMinimize)
		}
	case htMaxButton:
		if w.resizable {
			if w.isZoomed() {
				procShowWindowAsync.Call(w.hwnd, swShowNormal)
			} else {
				procShowWindowAsync.Call(w.hwnd, swMaximize)
			}
		}
	case htClose:
		procPostMessageW.Call(w.hwnd, wmClose, 0, 0)
	default:
		return 0, false
	}
	return 0, true
}

// handleSizeMoveLoopExit is the dragging half of events.rs:309-321
// handle_size_move_loop_exit (emulating winit): the move loop that
// StartWindowMove entered swallowed the left release, so post one.
func (w *hostWindow) handleSizeMoveLoopExit() {
	if w.dragging {
		w.dragging = false
		procPostMessageW.Call(w.hwnd, wmLButtonUp, 0, 0)
	}
}

// StartWindowMove starts the system move loop for the window
// (window.rs:1014-1039 start_window_move): release the mouse capture and
// post a left press on the caption at the cursor.
func (wh WindowHandle) StartWindowMove() {
	if wh.host == nil {
		return
	}
	wh.host.runForegroundAsync(func() {
		w := wh.record()
		if w == nil {
			return
		}
		w.dragging = true
		var cursor point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
		procReleaseCapture.Call()
		procPostMessageW.Call(wh.hwnd, wmNcLButtonDown, htCaption, makePointsLParam(cursor.x, cursor.y))
	})
}

// makePointsLParam packs a screen point like MAKELPARAM(x, y) (POINTS).
func makePointsLParam(x, y int32) uintptr {
	return uintptr(uint32(uint16(int16(x))) | uint32(uint16(int16(y)))<<16)
}
