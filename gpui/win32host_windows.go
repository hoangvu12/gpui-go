//go:build windows

package gpui

// The Win32 foreground host (ticket05), ported from the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/platform.rs — WindowsPlatform::new
//     (OleInitialize, platform message window, dispatcher),
//     WindowsPlatform::run (GetMessageW loop), quit (PostQuitMessage),
//     WindowsDispatcher::dispatch_on_main_thread (wake post with the
//     wake_posted flag) and run_foreground_task (queue drain protocol).
//   - crates/gpui_windows/src/window.rs — window class registration,
//     CreateWindowExW styles, bounds/title/activation/close, and Drop
//     (destroy on the foreground thread).
//   - crates/gpui_windows/src/events.rs — the message policies:
//     WM_CLOSE veto, WM_DESTROY close + registry removal post,
//     WM_DPICHANGED suggested rectangle, WM_SIZE minimized handling,
//     WM_PAINT validation.
//
// Pure Go: every Win32 call goes through the stdlib syscall package
// (NewLazyDLL + NewProc, resolved with Proc.Find; CGO_ENABLED=0, no new
// module dependencies). The message-loop thread is locked
// (runtime.LockOSThread) and owns the OLE apartment.
//
// Threading model: syscall.NewCallback callbacks (the window
// procedures) are invoked on the OS thread that dispatched the message.
// The Go runtime runs them on the goroutine that made the dispatching
// syscall, after exitsyscall, pinned to that M (runtime/cgocall.go
// cgocallbackg: lockOSThread before exitsyscall), so blocking inside a
// callback is allowed. The wake drain may therefore drive scheduler
// tasks that park on channel reads, exactly like the reference blocking
// its main thread while running foreground runnables.

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Win32 bindings
// ---------------------------------------------------------------------------

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modOle32    = syscall.NewLazyDLL("ole32.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")
	modShcore   = syscall.NewLazyDLL("shcore.dll")

	// user32
	procRegisterClassW       = modUser32.NewProc("RegisterClassW")
	procCreateWindowExW      = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW       = modUser32.NewProc("DefWindowProcW")
	procGetMessageW          = modUser32.NewProc("GetMessageW")
	procPeekMessageW         = modUser32.NewProc("PeekMessageW")
	procTranslateMessage     = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW     = modUser32.NewProc("DispatchMessageW")
	procPostMessageW         = modUser32.NewProc("PostMessageW")
	procPostQuitMessage      = modUser32.NewProc("PostQuitMessage")
	procDestroyWindow        = modUser32.NewProc("DestroyWindow")
	procSetWindowTextW       = modUser32.NewProc("SetWindowTextW")
	procGetWindowTextW       = modUser32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = modUser32.NewProc("GetWindowTextLengthW")
	procShowWindow           = modUser32.NewProc("ShowWindow")
	procSetForegroundWindow  = modUser32.NewProc("SetForegroundWindow")
	procSetActiveWindow      = modUser32.NewProc("SetActiveWindow")
	procSetFocus             = modUser32.NewProc("SetFocus")
	procSetWindowPos         = modUser32.NewProc("SetWindowPos")
	procGetWindowRect        = modUser32.NewProc("GetWindowRect")
	procGetClientRect        = modUser32.NewProc("GetClientRect")
	procGetDpiForWindow      = modUser32.NewProc("GetDpiForWindow")
	procIsWindow             = modUser32.NewProc("IsWindow")
	procIsWindowVisible      = modUser32.NewProc("IsWindowVisible")
	procIsIconic             = modUser32.NewProc("IsIconic")
	procIsZoomed             = modUser32.NewProc("IsZoomed")
	procValidateRect         = modUser32.NewProc("ValidateRect")
	procMonitorFromWindow    = modUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW      = modUser32.NewProc("GetMonitorInfoW")
	procGetCurrentThreadId   = modKernel32.NewProc("GetCurrentThreadId")
	procGetModuleHandleW     = modKernel32.NewProc("GetModuleHandleW")
	procOleInitialize        = modOle32.NewProc("OleInitialize")
	procOleUninitialize      = modOle32.NewProc("OleUninitialize")
	procCreateSolidBrush     = modGdi32.NewProc("CreateSolidBrush")

	// Optional (Windows 10 1703+): probed, not required.
	procSetProcessDpiAwarenessContext = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procGetThreadDpiAwarenessContext  = modUser32.NewProc("GetThreadDpiAwarenessContext")
	procAreDpiAwarenessContextsEqual  = modUser32.NewProc("AreDpiAwarenessContextsEqual")
	procSetProcessDpiAwareness        = modShcore.NewProc("SetProcessDpiAwareness")
)

var (
	hostProcsOnce sync.Once
	hostProcsErr  error
)

// resolveHostProcs resolves every required procedure once; the optional
// DPI procedures are probed leniently at their use sites.
func resolveHostProcs() error {
	hostProcsOnce.Do(func() {
		required := []*syscall.LazyProc{
			procRegisterClassW, procCreateWindowExW, procDefWindowProcW,
			procGetMessageW, procPeekMessageW, procTranslateMessage, procDispatchMessageW,
			procPostMessageW, procPostQuitMessage, procDestroyWindow,
			procSetWindowTextW, procGetWindowTextW, procGetWindowTextLengthW,
			procShowWindow, procSetForegroundWindow, procSetActiveWindow,
			procSetFocus, procSetWindowPos, procGetWindowRect, procGetClientRect,
			procGetDpiForWindow, procIsWindow, procIsWindowVisible,
			procIsIconic, procIsZoomed, procValidateRect, procMonitorFromWindow,
			procGetMonitorInfoW, procGetCurrentThreadId, procGetModuleHandleW,
			procOleInitialize, procOleUninitialize, procCreateSolidBrush,
		}
		for _, p := range required {
			if err := p.Find(); err != nil {
				hostProcsErr = fmt.Errorf("gpui: host syscall %s unavailable: %w", p.Name, err)
				return
			}
		}
	})
	return hostProcsErr
}

// Win32 constants (winuser.h, wingdi.h, ole2.h). Comments name the SDK
// symbol.
const (
	wmCreate     = 0x0001
	wmDestroy    = 0x0002
	wmMove       = 0x0003
	wmSize       = 0x0005
	wmActivate   = 0x0006
	wmPaint      = 0x000F
	wmClose      = 0x0010
	wmQuit       = 0x0012
	wmEraseBkgnd = 0x0014
	wmShowWindow = 0x0018
	wmNCCreate   = 0x0081
	wmNCDestroy  = 0x0082
	wmDPICHanged = 0x02E0
	wmUser       = 0x0400

	wmGPUICloseOneWindow = wmUser + 2 // reference WM_GPUI_CLOSE_ONE_WINDOW
	wmGPUIWake           = wmUser + 3 // reference WM_GPUI_TASK_DISPATCHED_ON_MAIN_THREAD

	sizeRestored  = 0
	sizeMinimized = 1
	sizeMaximized = 2

	waInactive = 0

	pmRemove = 0x0001

	swHide           = 0
	swShowNormal     = 1
	swShowMaximized  = 3
	swShowNoActivate = 4
	swShow           = 5
	swMinimize       = 6
	swRestore        = 9

	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000
	wsThickFrame  = 0x00040000
	wsCaption     = 0x00C00000

	wsExAppWindow = 0x00040000

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	monitorDefaultToNearest = 0x00000002

	userDefaultScreenDPI = 96

	hwndMessage = ^uintptr(2) // HWND_MESSAGE ((HWND)-3)

	cwUseDefault = uintptr(0x80000000)

	// DPI_AWARENESS_CONTEXT handle values (winuser.h).
	dpiContextUnaware      = ^uintptr(0) // ((HANDLE)-1)
	dpiContextSystemAware  = ^uintptr(1) // ((HANDLE)-2)
	dpiContextPerMonitor   = ^uintptr(2) // ((HANDLE)-3)
	dpiContextPerMonitorV2 = ^uintptr(3) // ((HANDLE)-4)

	processPerMonitorDPIAware = 2 // shcore SetProcessDpiAwareness

	sOK    = 0
	sFalse = 1
)

// point is Win32 POINT.
type point struct {
	x, y int32
}

// rect is Win32 RECT.
type rect struct {
	left, top, right, bottom int32
}

// msg is Win32 MSG (48 bytes on amd64: Go's trailing padding matches
// the SDK layout).
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	_pad    uint32
}

// wndClass is Win32 WNDCLASSW (72 bytes on amd64).
type wndClass struct {
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
}

// createStruct is Win32 CREATESTRUCTW (80 bytes on amd64). Only
// lpCreateParams is read: at WM_NCCREATE it carries the creating *Host,
// the way the reference passes its WindowCreateContext pointer. The
// field is unsafe.Pointer-typed so the message parameter crosses the
// trampoline without integer-to-pointer conversions.
type createStruct struct {
	lpCreateParams unsafe.Pointer
	hInstance      uintptr
	hMenu          uintptr
	hwndParent     uintptr
	cy, cx, y, x   int32
	style          int32
	lpszName       *uint16
	lpszClass      *uint16
	dwExStyle      uint32
	_pad           uint32
}

// monitorInfo is Win32 MONITORINFO (40 bytes).
type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

// Message-word helpers.
func loword(v uintptr) uint16      { return uint16(v) }
func hiword(v uintptr) uint16      { return uint16(v >> 16) }
func signedLoword(v uintptr) int32 { return int32(int16(uint16(v))) }
func signedHiword(v uintptr) int32 { return int32(int16(uint16(v >> 16))) }

// ---------------------------------------------------------------------------
// Window classes and procedures
// ---------------------------------------------------------------------------

// Window class names derived from the reference: "Zed::PlatformWindow"
// (platform.rs PLATFORM_WINDOW_CLASS_NAME, the message-only dispatcher
// window) and "Zed::Window" (window.rs WINDOW_CLASS_NAME).
const (
	platformClassName = "gpui-go::PlatformWindow"
	windowClassName   = "gpui-go::Window"
)

// The class-name buffers must outlive RegisterClassW, so they live at
// package level.
var (
	platformClassNamePtr *uint16
	windowClassNamePtr   *uint16

	platformWndProcPtr uintptr
	windowWndProcPtr   uintptr

	classOnce sync.Once
	classErr  error
)

// registerHostClasses creates the trampolines and registers both window
// classes once per process; multiple hosts share the classes.
//
// Deviation from the reference app window: the reference removes the OS
// title bar by default (hide_title_bar plus WM_NCCALCSIZE adjustments);
// this slice keeps the native frame (WS_CAPTION) because the custom
// title bar's geometry and hit testing arrive with the renderer/input
// tickets.
func registerHostClasses(hInstance uintptr) error {
	classOnce.Do(func() {
		var err error
		if platformClassNamePtr, err = syscall.UTF16PtrFromString(platformClassName); err != nil {
			classErr = err
			return
		}
		if windowClassNamePtr, err = syscall.UTF16PtrFromString(windowClassName); err != nil {
			classErr = err
			return
		}
		// Callbacks are created here, once per process, before the
		// message loop runs (syscall.NewCallback is limited to 2000 per
		// process and must not race class registration).
		platformWndProcPtr = syscall.NewCallback(platformWndProc)
		windowWndProcPtr = syscall.NewCallback(windowWndProc)

		// Platform class: default style, no background — a message-only
		// window (reference register_platform_window_class).
		pc := wndClass{
			lpfnWndProc:   platformWndProcPtr,
			hInstance:     hInstance,
			lpszClassName: platformClassNamePtr,
		}
		if a, _, _ := procRegisterClassW.Call(uintptr(unsafe.Pointer(&pc))); a == 0 {
			classErr = fmt.Errorf("gpui: RegisterClassW(%s) failed", platformClassName)
			return
		}
		// App window class: CS_HREDRAW|CS_VREDRAW with a solid black
		// brush (reference register_window_class).
		brush, _, _ := procCreateSolidBrush.Call(0)
		wc := wndClass{
			style:         csHRedraw | csVRedraw,
			lpfnWndProc:   windowWndProcPtr,
			hInstance:     hInstance,
			hbrBackground: brush,
			lpszClassName: windowClassNamePtr,
		}
		if a, _, _ := procRegisterClassW.Call(uintptr(unsafe.Pointer(&wc))); a == 0 {
			classErr = fmt.Errorf("gpui: RegisterClassW(%s) failed", windowClassName)
			return
		}
	})
	return classErr
}

// Global hwnd bindings. The reference stores a Weak pointer in
// GWLP_USERDATA and upgrades it per message (window.rs
// window_procedure); the Go port keeps the same binding in a
// mutex-guarded table instead, because converting a GWLP result (an
// integer register value) back to a Go pointer is not vet-provable.
// Bindings are written at WM_NCCREATE/WM_NCDESTROY on the host thread
// and read from the window procedures; the table lock is never held
// across application callbacks (platform contract: a nested window
// procedure must not hold a registry mutex across callbacks).
var (
	wndBindMu  sync.Mutex
	wndByHWND  = map[uintptr]*hostWindow{}
	platByHWND = map[uintptr]*Host{}
)

func bindWindow(hwnd uintptr, w *hostWindow) {
	wndBindMu.Lock()
	wndByHWND[hwnd] = w
	wndBindMu.Unlock()
}

func unbindWindow(hwnd uintptr) {
	wndBindMu.Lock()
	delete(wndByHWND, hwnd)
	wndBindMu.Unlock()
}

func windowFor(hwnd uintptr) *hostWindow {
	wndBindMu.Lock()
	w := wndByHWND[hwnd]
	wndBindMu.Unlock()
	return w
}

func bindPlatformWindow(hwnd uintptr, h *Host) {
	wndBindMu.Lock()
	platByHWND[hwnd] = h
	wndBindMu.Unlock()
}

func unbindPlatformWindow(hwnd uintptr) {
	wndBindMu.Lock()
	delete(platByHWND, hwnd)
	wndBindMu.Unlock()
}

func platformHostFor(hwnd uintptr) *Host {
	wndBindMu.Lock()
	h := platByHWND[hwnd]
	wndBindMu.Unlock()
	return h
}

// platformWndProc is the platform (dispatcher) window procedure. It
// resolves the host through the hwnd binding (set at WM_NCCREATE from
// the create parameters, mirroring the reference's context pointer) and
// handles the private wake and window-registry messages; everything
// else falls to DefWindowProcW.
//
// The recover trampoline keeps a Go panic from unwinding into the Win32
// dispatch frame (native ABI rule: panics are caught at trampolines).
// lparam is unsafe.Pointer-typed because it is a pointer for the
// messages this procedure dereferences (WM_NCCREATE's CREATESTRUCTW);
// integer reinterpretations use uintptr(lparam).
func platformWndProc(hwnd uintptr, message uint32, wparam uintptr, lparam unsafe.Pointer) (result uintptr) {
	lp := uintptr(lparam)
	var h *Host
	if message == wmNCCreate {
		h = (*Host)((*createStruct)(lparam).lpCreateParams)
	} else {
		h = platformHostFor(hwnd)
	}
	defer func() {
		if r := recover(); r != nil {
			if h != nil {
				h.recordPanic(r)
			}
			result = 0
		}
	}()
	if h == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		return r
	}
	switch message {
	case wmNCCreate:
		bindPlatformWindow(hwnd, h)
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		return r
	case wmNCDestroy:
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		unbindPlatformWindow(hwnd)
		return r
	case wmGPUIWake:
		// Private wake: validate the generation token like the
		// reference's validation_number check, then drain the
		// foreground queue.
		if wparam != uintptr(h.validationNumber) {
			h.recordFault("wake message with wrong validation token")
			return 0
		}
		tracef("wake platformhwnd=%x tid=%d", hwnd, currentThreadID())
		h.runForegroundWork()
		h.mu.Lock()
		queued := len(h.queue)
		h.mu.Unlock()
		tracef("wake drained platformhwnd=%x queued=%d", hwnd, queued)
		return 0
	case wmGPUICloseOneWindow:
		// One window finished destroying; its registry entry is already
		// gone (removed at WM_NCDESTROY). When this was the last window
		// the host quits (reference close_one_window reports the empty
		// registry; gpui quits on the last window).
		if len(h.windows) == 0 && h.quitOnLastWindow {
			tracef("quit-on-last platformhwnd=%x windows=%d", hwnd, len(h.windows))
			procPostQuitMessage.Call(0)
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
	return r
}

// windowWndProc is the application window procedure, ported from
// WindowsWindowInner::handle_msg (events.rs). Message policy in this
// slice:
//
//   - WM_NCCREATE creates the host window record (reference
//     WindowsWindowInner::new) and binds it to the hwnd.
//   - WM_CLOSE runs the veto synchronously (its return controls
//     destruction): returning 0 vetoes; DefWindowProcW destroys.
//   - WM_DESTROY runs the close callback synchronously and posts
//     WM_GPUI_CLOSE_ONE_WINDOW to the platform window.
//   - WM_NCDESTROY retires the record and its hwnd binding.
//   - WM_DPICHANGED records the new scale and applies the suggested
//     rectangle (SetWindowPos; WM_SIZE/WM_MOVE update the rest).
//   - WM_SIZE skips the resize report while minimized.
//   - WM_ACTIVATE defers the activation callback to the foreground
//     queue (reference spawns it on the foreground executor).
//   - WM_ERASEBKGND returns 1 (the renderer owns painting; ticket07).
//   - WM_PAINT records dirty, calls the redraw hook, validates.
//
// Input, IME, accessibility and dialog messages arrive with their
// tickets; they fall through to DefWindowProcW here. lparam is
// unsafe.Pointer-typed because it is a pointer for the messages this
// procedure dereferences (WM_NCCREATE's CREATESTRUCTW and
// WM_DPICHANGED's suggested RECT); integer reinterpretations use
// uintptr(lparam).
func windowWndProc(hwnd uintptr, message uint32, wparam uintptr, lparam unsafe.Pointer) (result uintptr) {
	lp := uintptr(lparam)
	tracef("window msg hwnd=%x id=0x%x w=%x", hwnd, message, wparam)
	if message == wmNCCreate {
		h := (*Host)((*createStruct)(lparam).lpCreateParams)
		defer func() {
			if r := recover(); r != nil {
				h.recordPanic(r)
				result = 0
			}
		}()
		w := h.newHostWindow(hwnd)
		bindWindow(hwnd, w)
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		return r
	}
	w := windowFor(hwnd)
	defer func() {
		if r := recover(); r != nil {
			if w != nil {
				w.host.recordPanic(r)
			}
			result = 0
		}
	}()
	if w == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		return r
	}
	switch message {
	case wmActivate:
		activated := loword(wparam) != waInactive
		// Deferred like the reference (handle_activate_msg spawns the
		// callback through the foreground executor).
		if w.onActivate != nil {
			cb := w.onActivate
			w.host.post(func() { cb(activated) })
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		return r

	case wmMove:
		w.originX = float32(signedLoword(lp))
		w.originY = float32(signedHiword(lp))
		if w.onMoved != nil {
			w.onMoved()
		}
		return 0

	case wmSize:
		if wparam == sizeMinimized {
			// Don't report a resize while minimized (reference
			// handle_size_msg); the restore reports the size again.
			w.minimized = true
			return 0
		}
		w.minimized = false
		width := int32(loword(lp))
		height := int32(hiword(lp))
		if width < 1 {
			width = 1
		}
		if height < 1 {
			height = 1
		}
		w.applySizeChange(width, height)
		return 0

	case wmDPICHanged:
		newDPI := float32(loword(wparam))
		w.scale = newDPI / userDefaultScreenDPI
		w.updateBorderOffset()
		if zoomed, _, _ := procIsZoomed.Call(hwnd); zoomed != 0 {
			// Maximized: resize to the monitor's work area at the new
			// DPI (reference handle_dpi_changed_msg maximized path).
			if mon, _, _ := procMonitorFromWindow.Call(hwnd, uintptr(monitorDefaultToNearest)); mon != 0 {
				var mi monitorInfo
				mi.cbSize = uint32(unsafe.Sizeof(mi))
				if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
					work := mi.rcWork
					procSetWindowPos.Call(hwnd, 0,
						uintptr(int64(work.left)), uintptr(int64(work.top)),
						uintptr(int64(work.right-work.left)), uintptr(int64(work.bottom-work.top)),
						uintptr(swpNoZOrder|swpNoActivate|swpFrameChanged))
					// SetWindowPos may not send WM_SIZE for maximized
					// windows; apply the size directly (reference
					// comment).
					w.applySizeChange(work.right-work.left, work.bottom-work.top)
				}
			}
		} else {
			// Apply the system's suggested rectangle; WM_SIZE and
			// WM_MOVE follow synchronously and update the state.
			suggested := (*rect)(lparam)
			procSetWindowPos.Call(hwnd, 0,
				uintptr(int64(suggested.left)), uintptr(int64(suggested.top)),
				uintptr(int64(suggested.right-suggested.left)), uintptr(int64(suggested.bottom-suggested.top)),
				uintptr(swpNoZOrder|swpNoActivate))
		}
		return 0

	case wmPaint:
		// Paint stub: record dirty, run the redraw hook (the renderer
		// arrives with ticket07), then validate so the invalid region
		// stops reposting (reference draw_window validates at the end).
		w.dirty = true
		if w.onRedraw != nil {
			w.onRedraw()
		}
		procValidateRect.Call(hwnd, 0)
		return 0

	case wmEraseBkgnd:
		// The renderer owns painting; report the background as erased.
		return 1

	case wmClose:
		if w.shouldClose != nil {
			if !w.shouldClose() {
				// Vetoed: consume the message so DefWindowProcW does
				// not destroy the window (reference handle_close_msg
				// returns Some(0) on veto).
				return 0
			}
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		return r

	case wmDestroy:
		// Synchronous close callback, then the generation-checked
		// registry removal through the platform window (reference
		// handle_destroy_msg).
		if w.onClose != nil {
			w.onClose()
		}
		procPostMessageW.Call(w.host.platformHWND, uintptr(wmGPUICloseOneWindow),
			uintptr(w.host.validationNumber), hwnd)
		return 0

	case wmNCDestroy:
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
		// Retire the record: drop the hwnd binding and the registry
		// entry so late messages see a dead lease (the reference clears
		// its Weak box at WM_NCDESTROY).
		unbindWindow(hwnd)
		delete(w.host.windows, hwnd)
		return r
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lp)
	return r
}

// ---------------------------------------------------------------------------
// Host window state (host thread only)
// ---------------------------------------------------------------------------

// hostWindow is the per-window record. Every field is written and read
// on the host thread only: creation, queries and mutations all marshal
// there and the window procedures run there. The lease id is the
// generation stamp: a handle whose id no longer matches the registry
// entry is stale.
type hostWindow struct {
	host  *Host
	hwnd  uintptr
	id    uint64
	scale float32
	// origin is the client origin in screen device pixels (WM_MOVE);
	// clientW/H are the client size in device pixels (WM_SIZE).
	originX, originY float32
	clientW, clientH int32
	// borderW/H is the window-minus-client frame extent (reference
	// WindowBorderOffset), updated after creation and DPI changes.
	borderW, borderH int32
	minimized        bool
	dirty            bool
	title            string

	// Callbacks from WindowOptions (nil keeps the default policy).
	shouldClose func() bool
	onClose     func()
	onActivate  func(active bool)
	onResize    func(size Size, scale float32)
	onMoved     func()
	onRedraw    func()
}

// newHostWindow builds the record for a window being created. Runs at
// WM_NCCREATE on the host thread.
func (h *Host) newHostWindow(hwnd uintptr) *hostWindow {
	w := &hostWindow{
		host:  h,
		hwnd:  hwnd,
		scale: 1,
	}
	if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi != 0 {
		w.scale = float32(uint32(dpi)) / userDefaultScreenDPI
	}
	// The creation closure fills id, callbacks and initial geometry
	// when CreateWindowExW returns.
	h.windows[hwnd] = w
	return w
}

// updateBorderOffset records the frame extent (GetWindowRect minus
// GetClientRect), mirroring WindowBorderOffset::update.
func (w *hostWindow) updateBorderOffset() {
	var wr, cr rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&wr)))
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	w.borderW = (wr.right - wr.left) - (cr.right - cr.left)
	w.borderH = (wr.bottom - wr.top) - (cr.bottom - cr.top)
}

// applySizeChange records a new client size and reports it (WM_SIZE and
// the maximized DPI path).
func (w *hostWindow) applySizeChange(width, height int32) {
	w.clientW = width
	w.clientH = height
	if w.onResize != nil {
		w.onResize(Size{
			Width:  float32(width) / w.scale,
			Height: float32(height) / w.scale,
		}, w.scale)
	}
}

// clientSize returns the logical client size.
func (w *hostWindow) clientSize() Size {
	return Size{
		Width:  float32(w.clientW) / w.scale,
		Height: float32(w.clientH) / w.scale,
	}
}

// windowRectFromClient computes the outer window rect for a desired
// client rect, distributing the frame offset like the reference's
// calculate_window_rect (which avoids AdjustWindowRectEx deliberately:
// it reports incorrect sizes).
func (w *hostWindow) windowRectFromClient(x, y, cx, cy int32) rect {
	left := w.borderW / 2
	top := w.borderH / 2
	right := w.borderW - left
	bottom := w.borderH - top
	return rect{
		left:   x - left,
		top:    y - top,
		right:  x + cx + right,
		bottom: y + cy + bottom,
	}
}

// ---------------------------------------------------------------------------
// Host
// ---------------------------------------------------------------------------

// Host is the Win32 foreground host: a dedicated locked OS thread that
// owns the OLE apartment, PerMonitorV2 DPI awareness, both window
// classes, the message-only platform (dispatcher) window, the message
// loop and the foreground runnable queue.
//
// The exported methods are safe from any goroutine; everything that
// touches thread state marshals through the wake mechanism (Post +
// WM_GPUIWake), running inline when called from the host thread itself.
// The host owns HWND destruction: handles handed out by OpenWindow are
// leases.
//
// One host per application (Attach); hosts are single-use (Start once).
// The reference maps this to WindowsPlatform + WindowsDispatcher.
type Host struct {
	// hInstance is the module handle used for class registration.
	hInstance uintptr
	// validationNumber stamps private messages (reference
	// WindowsDispatcher validation_number).
	validationNumber uint64

	startedOnce bool
	started     chan struct{} // closed when initialization finished
	done        chan struct{} // closed when the message loop exited

	mu         sync.Mutex
	queue      []func()
	wakePosted bool
	stopped    bool
	panicValue any
	faults     []string
	// initErr/loopErr are written under mu and read after the matching
	// channel closes.
	initErr error
	loopErr error

	// Host-thread state: threadID and platformHWND are written before
	// `started` closes; everything else is host-thread-only.
	// loopDone invalidates the thread identity the moment the loop
	// exits: the Go runtime returns the unlocked thread to its pool,
	// where any later goroutine (or the next host's loop) can land on
	// it. A stale identity would make onHostThread() report true for
	// callers that are NOT the foreground thread anymore — the exact
	// bug that let a second Stop() post a WM_QUIT into a thread nobody
	// pumps, killing the next host that reused that thread.
	threadID         uint32
	loopDone         atomic.Bool
	platformHWND     uintptr
	windowSeq        uint64
	windows          map[uintptr]*hostWindow
	oleInit          bool
	dpiRecord        string
	quitOnLastWindow bool

	// Application wiring.
	app        *App
	dispatcher *platformDispatcher
}

// NewHost creates an unstarted Win32 host. Start it (or App.Run) before
// posting work; the host quits when the last window closes or Stop is
// called.
func NewHost() *Host {
	return &Host{
		started:          make(chan struct{}),
		done:             make(chan struct{}),
		windows:          make(map[uintptr]*hostWindow),
		quitOnLastWindow: true,
		validationNumber: newValidationNumber(),
	}
}

// newValidationNumber stamps the private messages so foreign posts with
// guessed tokens are ignored (reference: rand::random::<usize>).
func newValidationNumber() uint64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err == nil {
		return binary.LittleEndian.Uint64(b[:])
	}
	return uint64(time.Now().UnixNano()) ^ uint64(os.Getpid())
}

// Start spawns the host thread (initializing the apartment, DPI
// awareness, classes and the platform window) and returns when
// initialization completed. The message loop keeps running on the host
// thread afterwards. Starting again reports the first result.
func (h *Host) Start() error {
	h.mu.Lock()
	if !h.startedOnce && h.stopped {
		h.mu.Unlock()
		return ErrHostStopped
	}
	first := !h.startedOnce
	h.startedOnce = true
	h.mu.Unlock()
	if first {
		go h.loop()
	}
	<-h.started
	h.mu.Lock()
	err := h.initErr
	h.mu.Unlock()
	return err
}

// loop is the host thread body: lock the thread, initialize the
// apartment and DPI awareness, register the classes, create the
// platform window, run the message loop, then drain owned resources.
func (h *Host) loop() {
	// The host thread is the foreground thread: messages, callbacks and
	// application updates all run here (runtime ownership contract:
	// "Run app mutation ... on one foreground goroutine locked to the
	// owning OS thread").
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	fail := func(err error) {
		h.mu.Lock()
		if h.initErr == nil {
			h.initErr = err
		}
		h.mu.Unlock()
		h.shutdownOnHostThread()
		close(h.started)
		close(h.done)
	}

	if err := resolveHostProcs(); err != nil {
		fail(err)
		return
	}
	h.threadID = currentThreadID()
	if inst, _, _ := procGetModuleHandleW.Call(0); inst != 0 {
		h.hInstance = inst
	}
	h.initDPIAwareness()
	h.initOLE()
	if err := h.initErrLocked(); err != nil {
		fail(err)
		return
	}
	if err := registerHostClasses(h.hInstance); err != nil {
		fail(err)
		return
	}

	// The message-only platform window (reference:
	// CreateWindowExW(PLATFORM_WINDOW_CLASS_NAME, HWND_MESSAGE)).
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(platformClassNamePtr)),
		0, 0,
		0, 0, 0, 0,
		hwndMessage,
		0,
		h.hInstance,
		uintptr(unsafe.Pointer(h)),
	)
	if hwnd == 0 {
		fail(fmt.Errorf("gpui: CreateWindowExW for the platform window failed"))
		return
	}
	h.platformHWND = hwnd
	tracef("loop ready thread=%d platformHWND=%x", h.threadID, hwnd)

	// Flush any stale state this pooled thread carried over. The Go
	// runtime recycles OS threads: a previous user of this thread can
	// have left a WM_QUIT flag (or messages) behind, and GetMessageW
	// would end this host's loop immediately. PeekMessageW with
	// PM_REMOVE drains the queue and consumes a pending quit flag
	// (verified: a following GetMessageW blocks).
	for {
		var m msg
		r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 {
			break
		}
		tracef("flushed stale message id=0x%x", m.message)
	}

	close(h.started)

	// Message loop (reference WindowsPlatform::run). The accelerator
	// pre-dispatch (translate_accelerator -> WM_GPUI_KEYDOWN) arrives
	// with the keyboard ticket; TranslateMessage/DispatchMessageW alone
	// are correct for this slice.
	for {
		var m msg
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 {
			// WM_QUIT: the last window closed or Stop was called.
			tracef("loop exit WM_QUIT thread=%d", h.threadID)
			break
		}
		if r == ^uintptr(0) {
			tracef("loop exit GetMessageW-error thread=%d", h.threadID)
			h.setLoopErr(fmt.Errorf("gpui: GetMessageW failed"))
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	h.shutdownOnHostThread()
	close(h.done)
}

// initErrLocked returns the recorded initialization error.
func (h *Host) initErrLocked() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.initErr
}

// setLoopErr records the terminal loop error.
func (h *Host) setLoopErr(err error) {
	h.mu.Lock()
	h.loopErr = err
	h.mu.Unlock()
}

// initDPIAwareness establishes PerMonitorV2 before any window exists
// and records the outcome honestly: a process whose DPI context was
// already fixed (manifest or an earlier host in this process) fails the
// set call while already carrying the context; that is recorded, not
// hidden (the pinned reference ships awareness through its application
// manifest; this host sets it programmatically before creating
// windows, as the ticket requires).
func (h *Host) initDPIAwareness() {
	var parts []string
	set := "not-attempted"
	if err := procSetProcessDpiAwarenessContext.Find(); err == nil {
		if ok, _, callErr := procSetProcessDpiAwarenessContext.Call(dpiContextPerMonitorV2); ok != 0 {
			set = "ok"
		} else {
			// Failure semantics: an already-fixed process fails with
			// ERROR_ACCESS_DENIED while the context is already set.
			set = fmt.Sprintf("failed(%v)", callErr)
		}
	} else if err := procSetProcessDpiAwareness.Find(); err == nil {
		// Pre-1703 fallback: shcore SetProcessDpiAwareness.
		r, _, _ := procSetProcessDpiAwareness.Call(uintptr(processPerMonitorDPIAware))
		if uint32(r) == uint32(sOK) || uint32(r) == uint32(sFalse) {
			set = "ok(shcore-fallback)"
		} else {
			set = fmt.Sprintf("failed-shcore(0x%x)", uint32(r))
		}
	}
	parts = append(parts, "set="+set)
	// Verify the final executable mode on this thread. The returned
	// DPI_AWARENESS_CONTEXT is an opaque handle on current Windows (not
	// the documented -1..-4 constants), so it is compared through
	// AreDpiAwarenessContextsEqual. GetProcessDpiAwarenessContext does
	// not exist in user32.dll; the thread context after the set is the
	// honest record here (the host thread is the window-creating
	// thread).
	if err := procGetThreadDpiAwarenessContext.Find(); err == nil {
		if ctx, _, _ := procGetThreadDpiAwarenessContext.Call(); ctx != 0 {
			parts = append(parts, "effective="+describeDPIContext(ctx))
		}
	}
	h.dpiRecord = strings.Join(parts, " ")
}

// describeDPIContext names a DPI_AWARENESS_CONTEXT handle through
// AreDpiAwarenessContextsEqual, falling back to the documented constant
// values when the comparison API is unavailable.
func describeDPIContext(ctx uintptr) string {
	if err := procAreDpiAwarenessContextsEqual.Find(); err == nil {
		for _, known := range []struct {
			ctx  uintptr
			name string
		}{
			{dpiContextPerMonitorV2, "per-monitor-v2"},
			{dpiContextPerMonitor, "per-monitor"},
			{dpiContextSystemAware, "system-aware"},
			{dpiContextUnaware, "unaware"},
		} {
			if ok, _, _ := procAreDpiAwarenessContextsEqual.Call(ctx, known.ctx); ok != 0 {
				return known.name
			}
		}
		return fmt.Sprintf("unknown(%d)", int64(ctx))
	}
	switch ctx {
	case dpiContextUnaware:
		return "unaware"
	case dpiContextSystemAware:
		return "system-aware"
	case dpiContextPerMonitor:
		return "per-monitor"
	case dpiContextPerMonitorV2:
		return "per-monitor-v2"
	}
	return fmt.Sprintf("unknown(%d)", int64(ctx))
}

// initOLE initializes the OLE apartment on the host thread.
// OleInitialize is CoInitializeEx(NULL, COINIT_APARTMENTTHREADED) plus
// OLE services: it is the pinned reference's call (platform.rs
// OleInitialize(None)) and the platform contract's ("This follows
// OleInitialize"). S_OK and S_FALSE are both balanced with
// OleUninitialize at shutdown; an incompatible existing apartment
// (RPC_E_CHANGED_MODE) is an initialization error, per contract.
func (h *Host) initOLE() {
	r, _, _ := procOleInitialize.Call(0)
	switch uint32(r) {
	case uint32(sOK), uint32(sFalse):
		h.oleInit = true
	default:
		h.mu.Lock()
		h.initErr = fmt.Errorf("gpui: OleInitialize failed: 0x%08x (an existing incompatible COM apartment is an initialization error)", uint32(r))
		h.mu.Unlock()
	}
}

// shutdownOnHostThread is the orderly shutdown after the message loop
// ends: destroy the remaining windows (the host owns HWND destruction)
// in window-id order, then the platform window, then balance OLE.
// Destroying windows here runs their WM_DESTROY/WM_NCDESTROY handlers
// synchronously on this thread.
func (h *Host) shutdownOnHostThread() {
	// Deterministic order, not map iteration (runtime ownership
	// contract: ordered records).
	hwnds := make([]uintptr, 0, len(h.windows))
	for hwnd := range h.windows {
		hwnds = append(hwnds, hwnd)
	}
	sort.Slice(hwnds, func(i, j int) bool {
		return h.windows[hwnds[i]].id < h.windows[hwnds[j]].id
	})
	for _, hwnd := range hwnds {
		if h.windows[hwnd] != nil {
			procDestroyWindow.Call(hwnd)
		}
	}
	if h.platformHWND != 0 {
		procDestroyWindow.Call(h.platformHWND)
		h.platformHWND = 0
	}
	if h.oleInit {
		procOleUninitialize.Call(0)
		h.oleInit = false
	}

	// Reject new posts, invalidate the thread identity, then drain what
	// was already queued so blocked callers unblock instead of hanging
	// on results that will never come (the reference forgets runnables
	// whose receiver is gone). The thread identity must drop BEFORE the
	// final drain: a drained closure calling Stop or a query must not
	// take the "I am the host thread" inline path on a dead loop.
	h.mu.Lock()
	h.stopped = true
	panicValue := h.panicValue
	h.mu.Unlock()
	h.loopDone.Store(true)
	h.runForegroundWork()

	if panicValue != nil && h.loopErr == nil && h.initErr == nil {
		h.setLoopErr(fmt.Errorf("gpui: host recorded a panic in the window trampoline: %v", panicValue))
	}
}

// Wait blocks until the message loop exits and reports its terminal
// error.
func (h *Host) Wait() error {
	h.mu.Lock()
	started := h.startedOnce
	h.mu.Unlock()
	if !started {
		return ErrHostNotStarted
	}
	<-h.done
	h.mu.Lock()
	err := h.loopErr
	h.mu.Unlock()
	return err
}

// Stop requests the message loop to quit (PostQuitMessage through the
// wake mechanism, mirroring the reference Platform::quit which spawns
// PostQuitMessage on the foreground executor) and waits for the loop to
// exit. Stop is idempotent: on an already-exited host it only reports
// the terminal state. Called on the live host thread itself it posts
// the quit directly (the loop consumes it on its next pump).
func (h *Host) Stop() error {
	h.mu.Lock()
	started := h.startedOnce
	stopped := h.stopped
	h.mu.Unlock()
	if !started {
		return ErrHostNotStarted
	}
	if stopped {
		// Already exited: never touch the thread's queue from here. A
		// second quit posted from a goroutine that happens to run on the
		// recycled host thread would poison that pooled thread for its
		// next user (observed as the next host dying instantly).
		h.mu.Lock()
		err := h.loopErr
		h.mu.Unlock()
		return err
	}
	if h.onHostThread() {
		tracef("stop-quit (inline on host thread) tid=%d", currentThreadID())
		procPostQuitMessage.Call(0)
		return nil
	}
	if h.post(func() {
		// Guard: this closure can be drained by the final shutdown
		// drain AFTER the loop already exited (the last window's quit
		// raced this Stop). Posting a quit then would poison the pooled
		// thread for its next user, so the request is dropped.
		if h.loopDone.Load() {
			tracef("stop-quit skipped: loop already exited tid=%d", currentThreadID())
			return
		}
		tracef("stop-quit closure tid=%d", currentThreadID())
		procPostQuitMessage.Call(0)
	}) {
		<-h.done
	}
	h.mu.Lock()
	err := h.loopErr
	h.mu.Unlock()
	return err
}

// ThreadID returns the host (foreground) thread's OS id, or 0 before
// the host started.
func (h *Host) ThreadID() uint32 { return h.threadID }

// IsForegroundThread reports whether the caller runs on the host
// (foreground) thread, decided through GetCurrentThreadId.
func (h *Host) IsForegroundThread() bool { return h.onHostThread() }

// DPIAwareness returns the recorded process DPI awareness after host
// initialization: what SetProcessDpiAwarenessContext returned and the
// effective context Windows reports (for example
// "set=ok effective=per-monitor-v2").
func (h *Host) DPIAwareness() string { return h.dpiRecord }

// onHostThread reports whether the current OS thread is the live
// foreground (host) thread. Once the loop exited the identity is
// invalid even if the caller happens to run on the recycled thread.
func (h *Host) onHostThread() bool {
	if h.loopDone.Load() {
		return false
	}
	return h.threadID != 0 && currentThreadID() == h.threadID
}

// currentThreadID reads the OS thread id of the caller.
func currentThreadID() uint32 {
	r, _, _ := procGetCurrentThreadId.Call()
	return uint32(r)
}

// ---------------------------------------------------------------------------
// The wake mechanism (foreground runnable queue)
// ---------------------------------------------------------------------------

// Post schedules f to run on the foreground (host) thread; ordering is
// FIFO. It reports whether the closure was accepted: false means the
// host failed to start or its loop already exited and f was dropped
// (waiters must not block on its effects). Posting before Start panics.
// Callers that fire and forget can ignore the result.
//
// This is the mechanism behind the platform dispatcher and every
// window-lease operation: the Go port of
// WindowsDispatcher::dispatch_on_main_thread.
func (h *Host) Post(f func()) bool {
	if f == nil {
		panic("gpui: Post called with a nil closure")
	}
	return h.post(f)
}

// post enqueues f and wakes the message loop once, reporting whether
// the closure was accepted (false: the host failed to start or the loop
// already exited). It panics on an unstarted host. While the loop
// goroutine is still initializing (the platform window does not exist
// yet), post waits for initialization instead of stranding the closure
// on a queue nobody drains yet.
func (h *Host) post(f func()) bool {
	h.mu.Lock()
	if !h.startedOnce {
		h.mu.Unlock()
		panic("gpui: Post before the host started (start it, or run the application through App.Run)")
	}
	for h.platformHWND == 0 && h.initErr == nil && !h.stopped {
		// Initialization is in flight; wait for its outcome.
		h.mu.Unlock()
		<-h.started
		h.mu.Lock()
	}
	if h.stopped || h.initErr != nil {
		h.mu.Unlock()
		return false
	}
	h.queue = append(h.queue, f)
	wake := !h.wakePosted
	h.wakePosted = true
	platformHWND := h.platformHWND
	h.mu.Unlock()
	if wake {
		// The reference guards duplicate posts with its wake_posted
		// flag exactly like this; a failed post clears it so a later
		// post retries instead of sticking.
		if ok, _, callErr := procPostMessageW.Call(platformHWND,
			uintptr(wmGPUIWake), uintptr(h.validationNumber), 0); ok == 0 {
			tracef("PostMessageW(wake) FAILED: %v (platformHWND=%x)", callErr, platformHWND)
			h.mu.Lock()
			h.wakePosted = false
			h.mu.Unlock()
		}
	}
	return true
}

// runForegroundWork drains the foreground queue on the host thread. It
// is the port of run_foreground_task's protocol: drain until quiet,
// clear the wake flag, re-check once (a closure enqueued between the
// drain and the flag clear would otherwise rely on a lost post), and
// only then return.
//
// Deviation: the reference bounds this drain to a 10ms budget and then
// re-posts itself to stay responsive; this slice drains to quiet on
// each wake. Closures posted while draining run in the same pass.
func (h *Host) runForegroundWork() {
	for {
		for {
			h.mu.Lock()
			if len(h.queue) == 0 {
				h.mu.Unlock()
				break
			}
			f := h.queue[0]
			h.queue = h.queue[1:]
			h.mu.Unlock()
			h.safeRun(f)
		}
		h.mu.Lock()
		h.wakePosted = false
		var again func()
		if len(h.queue) > 0 {
			again = h.queue[0]
			h.queue = h.queue[1:]
			h.wakePosted = true
		}
		h.mu.Unlock()
		if again == nil {
			return
		}
		h.safeRun(again)
	}
}

// safeRun runs one posted closure, containing a panic at the
// trampoline so the message loop survives bad foreground work; the
// first panic is recorded and surfaces through Wait/Stop.
func (h *Host) safeRun(f func()) {
	defer func() {
		if r := recover(); r != nil {
			h.recordPanic(r)
		}
	}()
	f()
}

// recordPanic stores the first panic value seen in the host.
func (h *Host) recordPanic(r any) {
	h.mu.Lock()
	if h.panicValue == nil {
		h.panicValue = r
	}
	h.mu.Unlock()
}

// traceEnabled gates diagnostic tracing of the host (message flow and
// queue drains) behind GPUI_WINHOST_TRACE=1; it exists to debug the
// host on real machines and stays off by default.
var traceEnabled = os.Getenv("GPUI_WINHOST_TRACE") == "1"

func tracef(format string, args ...any) {
	if traceEnabled {
		fmt.Fprintf(os.Stderr, "[winhost] "+format+"\n", args...)
	}
}

// recordFault records a soft fault (for example a wake message with a
// wrong validation token) for diagnostics.
func (h *Host) recordFault(msg string) {
	h.mu.Lock()
	if len(h.faults) < 16 {
		h.faults = append(h.faults, msg)
	}
	h.mu.Unlock()
	tracef("fault: %s", msg)
}

// runForegroundSync runs f on the foreground thread, inline when called
// from it. Otherwise it posts f and blocks until it ran; it reports
// false when the host no longer accepts work (f was not run).
func (h *Host) runForegroundSync(f func()) bool {
	if h.onHostThread() {
		tracef("runSync INLINE tid=%d hostTid=%d", currentThreadID(), h.threadID)
		h.safeRun(f)
		return true
	}
	done := make(chan struct{})
	if h.post(func() {
		defer close(done)
		h.safeRun(f)
	}) {
		<-done
		return true
	}
	return false
}

// queryForeground runs f on the foreground thread and returns its
// value and error, ErrHostStopped when the host no longer accepts work.
func queryForeground[T any](h *Host, f func() (T, error)) (T, error) {
	if h.onHostThread() {
		return f()
	}
	type result struct {
		value T
		err   error
	}
	res := make(chan result, 1)
	if h.post(func() {
		defer func() {
			if r := recover(); r != nil {
				h.recordPanic(r)
				res <- result{err: fmt.Errorf("gpui: panic while running a foreground query: %v", r)}
			}
		}()
		v, err := f()
		res <- result{value: v, err: err}
	}) {
		r := <-res
		return r.value, r.err
	}
	var zero T
	return zero, ErrHostStopped
}

// runForegroundAsync schedules f on the foreground thread without
// waiting (fire-and-forget window operations).
func (h *Host) runForegroundAsync(f func()) {
	if h.onHostThread() {
		h.safeRun(f)
		return
	}
	h.Post(f)
}

// ---------------------------------------------------------------------------
// Window lifecycle
// ---------------------------------------------------------------------------

// OpenWindow creates a window on the host thread. Creation is marshaled
// through the wake mechanism (inline when the caller already runs on
// the host thread): CreateWindowExW runs synchronously with the
// callback trampolines re-entered on the same thread, like the
// reference creating windows from foreground-executor code.
//
// The returned handle is a lease: the host owns the HWND and destroys
// it on close (through the veto path) or shutdown.
func (h *Host) OpenWindow(opts WindowOptions) (WindowHandle, error) {
	type openResult struct {
		handle WindowHandle
		err    error
	}
	res := make(chan openResult, 1)
	accepted := h.runForegroundSync(func() {
		handle, err := h.openWindowOnHostThread(opts)
		res <- openResult{handle: handle, err: err}
	})
	if !accepted {
		res <- openResult{err: ErrHostStopped}
	}
	r := <-res
	return r.handle, r.err
}

// openWindowOnHostThread creates one window. Runs on the host thread.
func (h *Host) openWindowOnHostThread(opts WindowOptions) (WindowHandle, error) {
	// Styles follow WindowsWindow::new for a normal window:
	// WS_SYSMENU (+WS_THICKFRAME|WS_MAXIMIZEBOX when resizable,
	// +WS_MINIMIZEBOX when minimizable) and WS_EX_APPWINDOW.
	// Deviation: WS_CAPTION keeps the native title bar in this slice
	// (see registerHostClasses); WS_EX_NOREDIRECTIONBITMAP arrives with
	// the DirectComposition renderer (ticket07).
	style := uintptr(wsSysMenu | wsCaption)
	if opts.Resizable {
		style |= wsThickFrame | wsMaximizeBox
	}
	if opts.MinimizeBox {
		style |= wsMinimizeBox
	}

	var title16 *uint16
	if opts.Title != "" {
		// A title with an interior NUL truncates at it: that is Win32
		// title semantics (NUL terminates the window text).
		if t, err := syscall.UTF16PtrFromString(opts.Title); err == nil {
			title16 = t
		}
	}

	h.windowSeq++
	id := h.windowSeq

	// lpCreateParams carries the host to WM_NCCREATE (the reference
	// passes its WindowCreateContext pointer the same way). The unsafe
	// pointer conversions sit in the call's argument list so the
	// compiler keeps the buffers alive through the syscall.
	hwnd, _, callErr := procCreateWindowExW.Call(
		wsExAppWindow,
		uintptr(unsafe.Pointer(windowClassNamePtr)),
		uintptr(unsafe.Pointer(title16)),
		style,
		cwUseDefault, cwUseDefault, cwUseDefault, cwUseDefault,
		0, 0,
		h.hInstance,
		uintptr(unsafe.Pointer(h)),
	)
	if hwnd == 0 {
		err := callErr
		if err == nil {
			err = errors.New("CreateWindowExW returned NULL")
		}
		return WindowHandle{}, &WindowCreateError{Title: opts.Title, Err: err}
	}

	// The record was created at WM_NCCREATE; wire identity, callbacks
	// and initial geometry now (same thread: no message can interleave
	// between CreateWindowExW returning and this code).
	w := h.windows[hwnd]
	if w == nil {
		// The window exists but its record does not: creation raced a
		// destroy (not possible in this flow); destroy defensively.
		procDestroyWindow.Call(hwnd)
		return WindowHandle{}, &WindowCreateError{
			Title: opts.Title,
			Err:   errors.New("window procedure did not attach the host window record"),
		}
	}
	w.id = id
	w.title = opts.Title
	w.shouldClose = opts.ShouldClose
	w.onClose = opts.OnClose
	w.onActivate = opts.OnActivate
	w.onResize = opts.OnResize
	w.onMoved = opts.OnMoved
	w.onRedraw = opts.OnRedraw
	w.updateBorderOffset()

	// Initial bounds: logical pixels convert to device pixels with the
	// window's own monitor DPI (simplified from retrieve_window_placement,
	// which additionally validates the bounds against the chosen
	// display before applying them).
	if opts.Bounds.Size.Width > 0 || opts.Bounds.Size.Height > 0 ||
		opts.Bounds.Origin != (Point{}) {
		x := int32(opts.Bounds.Origin.X * w.scale)
		y := int32(opts.Bounds.Origin.Y * w.scale)
		cx := int32(opts.Bounds.Size.Width * w.scale)
		cy := int32(opts.Bounds.Size.Height * w.scale)
		if cx < 1 {
			cx = 200
		}
		if cy < 1 {
			cy = 120
		}
		r := w.windowRectFromClient(x, y, cx, cy)
		procSetWindowPos.Call(hwnd, 0,
			uintptr(int64(r.left)), uintptr(int64(r.top)),
			uintptr(int64(r.right-r.left)), uintptr(int64(r.bottom-r.top)),
			uintptr(swpNoZOrder|swpNoActivate))
	}

	if opts.Show {
		cmd := uintptr(swShow)
		if !opts.Activate {
			cmd = swShowNoActivate
		}
		procShowWindow.Call(hwnd, cmd)
	}

	return WindowHandle{host: h, id: id, hwnd: hwnd}, nil
}

// ---------------------------------------------------------------------------
// The window lease
// ---------------------------------------------------------------------------

// WindowHandle is a leased reference to a host window. The host owns
// the HWND: closing goes through the WM_CLOSE veto path, destruction
// happens on the host thread, and a handle whose window is already
// destroyed is stale (ErrWindowClosed on value queries, no-op on
// fire-and-forget operations).
//
// The handle is comparable and carries the host window id as its
// generation stamp.
type WindowHandle struct {
	host *Host
	id   uint64
	hwnd uintptr
}

// Hwnd returns the leased HWND value for diagnostics and the renderer
// seam (the renderer receives the leased HWND, device extent and scale
// per the platform contract).
func (wh WindowHandle) Hwnd() uintptr { return wh.hwnd }

// ID returns the host window id (the generation stamp of this lease).
func (wh WindowHandle) ID() uint64 { return wh.id }

// record resolves the live host window record for this lease on the
// host thread; nil means the lease is stale.
func (wh WindowHandle) record() *hostWindow {
	if wh.host == nil {
		return nil
	}
	w := wh.host.windows[wh.hwnd]
	if w == nil || w.id != wh.id {
		return nil
	}
	return w
}

// Alive reports whether the leased window still exists.
func (wh WindowHandle) Alive() bool {
	if wh.host == nil {
		return false
	}
	alive, err := queryForeground(wh.host, func() (bool, error) {
		ok, _, _ := procIsWindow.Call(wh.hwnd)
		return ok != 0 && wh.record() != nil, nil
	})
	return err == nil && alive
}

// Visible reports whether the window is currently shown.
func (wh WindowHandle) Visible() bool {
	if wh.host == nil {
		return false
	}
	visible, err := queryForeground(wh.host, func() (bool, error) {
		if wh.record() == nil {
			return false, nil
		}
		ok, _, _ := procIsWindowVisible.Call(wh.hwnd)
		return ok != 0, nil
	})
	return err == nil && visible
}

// Title returns the window title as Windows reports it (GetWindowTextW).
func (wh WindowHandle) Title() (string, error) {
	if wh.host == nil {
		return "", ErrWindowClosed
	}
	return queryForeground(wh.host, func() (string, error) {
		if wh.record() == nil {
			return "", ErrWindowClosed
		}
		n, _, _ := procGetWindowTextLengthW.Call(wh.hwnd)
		if n == 0 {
			return "", nil
		}
		buf := make([]uint16, int(n)+1)
		got, _, _ := procGetWindowTextW.Call(wh.hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
		return syscall.UTF16ToString(buf[:got]), nil
	})
}

// SetTitle sets the window title (SetWindowTextW). A stale lease is a
// no-op, like a post to a dead window.
func (wh WindowHandle) SetTitle(title string) {
	wh.host.runForegroundAsync(func() {
		w := wh.record()
		if w == nil {
			return
		}
		if t, err := syscall.UTF16PtrFromString(title); err == nil {
			procSetWindowTextW.Call(wh.hwnd, uintptr(unsafe.Pointer(t)))
			w.title = title
		}
	})
}

// Show shows the window without activating it.
func (wh WindowHandle) Show() {
	wh.host.runForegroundAsync(func() {
		if wh.record() == nil {
			return
		}
		procShowWindow.Call(wh.hwnd, swShow)
	})
}

// Hide hides the window.
func (wh WindowHandle) Hide() {
	wh.host.runForegroundAsync(func() {
		if wh.record() == nil {
			return
		}
		procShowWindow.Call(wh.hwnd, swHide)
	})
}

// Activate brings the window to the foreground: restore it when
// minimized, then SetActiveWindow/SetFocus/SetForegroundWindow.
//
// Deviation: the reference additionally synthesizes an Alt keypress
// through SendInput to work around Windows' foreground-lock rule (the
// window must have received input to raise itself). Injecting synthetic
// keyboard input is avoided here, so activation can be denied by the OS
// in exactly the situations the reference works around (cite:
// window.rs activate).
func (wh WindowHandle) Activate() {
	wh.host.runForegroundAsync(func() {
		if wh.record() == nil {
			return
		}
		if iconic, _, _ := procIsIconic.Call(wh.hwnd); iconic != 0 {
			procShowWindow.Call(wh.hwnd, swRestore)
		}
		procSetActiveWindow.Call(wh.hwnd)
		procSetFocus.Call(wh.hwnd)
		procSetForegroundWindow.Call(wh.hwnd)
	})
}

// Close requests a close: WM_CLOSE is delivered, the ShouldClose veto
// decides, and the window is destroyed only when allowed. Close is
// asynchronous (the veto and destruction happen on the host thread).
func (wh WindowHandle) Close() {
	wh.host.runForegroundAsync(func() {
		if wh.record() == nil {
			return
		}
		procPostMessageW.Call(wh.hwnd, uintptr(wmClose), 0, 0)
	})
}

// Bounds returns the window bounds in logical pixels: the screen origin
// from WM_MOVE and the client size from WM_SIZE, converted with the
// current scale (reference WindowsWindowState::bounds).
func (wh WindowHandle) Bounds() (Bounds, error) {
	if wh.host == nil {
		return Bounds{}, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (Bounds, error) {
		w := wh.record()
		if w == nil {
			return Bounds{}, ErrWindowClosed
		}
		return Bounds{
			Origin: Point{X: w.originX / w.scale, Y: w.originY / w.scale},
			Size:   w.clientSize(),
		}, nil
	})
}

// SetBounds moves and resizes the window to the requested logical
// bounds. The client area becomes the requested size (the frame offset
// is added like the reference's calculate_window_rect); the position is
// the requested origin.
func (wh WindowHandle) SetBounds(bounds Bounds) {
	wh.host.runForegroundAsync(func() {
		w := wh.record()
		if w == nil {
			return
		}
		x := int32(bounds.Origin.X * w.scale)
		y := int32(bounds.Origin.Y * w.scale)
		cx := int32(bounds.Size.Width * w.scale)
		cy := int32(bounds.Size.Height * w.scale)
		if cx < 1 {
			cx = w.clientW
		}
		if cy < 1 {
			cy = w.clientH
		}
		r := w.windowRectFromClient(x, y, cx, cy)
		procSetWindowPos.Call(wh.hwnd, 0,
			uintptr(int64(r.left)), uintptr(int64(r.top)),
			uintptr(int64(r.right-r.left)), uintptr(int64(r.bottom-r.top)),
			uintptr(swpNoZOrder|swpNoActivate))
	})
}

// ScaleFactor returns the window's current scale factor (device pixels
// per logical pixel), updated by WM_DPICHANGED.
func (wh WindowHandle) ScaleFactor() (float32, error) {
	if wh.host == nil {
		return 0, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (float32, error) {
		if w := wh.record(); w != nil {
			return w.scale, nil
		}
		return 0, ErrWindowClosed
	})
}

// ---------------------------------------------------------------------------
// The platform dispatcher (real-app foreground executor backing)
// ---------------------------------------------------------------------------

// platformDispatcher backs the application's foreground executor on the
// host's wake mechanism: when the scheduler gains runnable work the
// host thread is woken (the Go analogue of
// WindowsDispatcher::dispatch_on_main_thread posting
// WM_GPUI_TASK_DISPATCHED_ON_MAIN_THREAD); the wake drain then drives
// the scheduler on the host thread, so task bodies' App.Update
// requests execute on the foreground thread.
type platformDispatcher struct {
	app  *App
	host *Host
	// pending coalesces wakes: only one drive closure is queued at a
	// time; a wake arriving while driving queues the next one.
	pending atomic.Bool
}

// wake is the hook the scheduler calls when work becomes runnable.
func (d *platformDispatcher) wake() {
	if d.pending.CompareAndSwap(false, true) {
		d.host.Post(func() {
			d.pending.Store(false)
			d.drive()
		})
	}
}

// drive drains the scheduler on the host thread. Blocking here mirrors
// the reference: its main thread runs foreground runnables to
// completion; task bodies park on their own goroutines and their update
// requests execute inline here.
func (d *platformDispatcher) drive() {
	defer func() {
		if r := recover(); r != nil {
			d.host.recordPanic(r)
		}
	}()
	d.app.sched.run()
}

// attach wires the host into the application as its foreground
// dispatcher.
func (h *Host) attach(a *App) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.app != nil && h.app != a {
		return fmt.Errorf("gpui: host already attached to another application")
	}
	if h.app == a {
		return nil
	}
	h.app = a
	h.dispatcher = &platformDispatcher{app: a, host: h}
	a.platformWake = h.dispatcher.wake
	return nil
}
