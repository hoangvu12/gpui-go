package gpui

import (
	"errors"
	"fmt"
)

// This file owns the window slice of the runtime (ticket05): the logical
// window identity and registry on the App, the window options that cross
// the platform seam, and the real-application boot path that attaches a
// Win32 host as the foreground thread.
//
// The platform seam itself lives in win32host_windows.go (the real host:
// locked OLE-STA thread, PerMonitorV2 DPI, window classes, message loop,
// wake mechanism) and win32host_other.go (a typed-error stub). Only the
// host's public surface is referenced here, so this file stays
// platform-neutral.
//
// The reference is CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
// crates/gpui_windows/src/platform.rs (WindowsPlatform: run/quit,
// open_window, the dispatcher wake), crates/gpui_windows/src/window.rs
// (WindowsWindow: bounds, title, activation, close) and
// crates/gpui_windows/src/events.rs (message policies). Deviations are
// cited at each site.

// ---------------------------------------------------------------------------
// Window geometry (logical pixels)
// ---------------------------------------------------------------------------

// Point is a position in window-local logical pixels. Logical pixels are
// device pixels divided by the window's current scale factor; conversion
// happens exactly once at the platform boundary (windows platform
// contract, "Keyboard, focus, IME and coordinates").
type Point struct {
	// X is the horizontal offset in logical pixels.
	X float32
	// Y is the vertical offset in logical pixels.
	Y float32
}

// Size is an extent in logical pixels.
type Size struct {
	// Width is the horizontal extent in logical pixels.
	Width float32
	// Height is the vertical extent in logical pixels.
	Height float32
}

// Bounds is a window origin plus client size in logical pixels. The
// origin is the window's screen position; the size is the client (draw)
// area, mirroring the reference WindowsWindowState::bounds (origin from
// WM_MOVE, logical_size from the client WM_SIZE).
type Bounds struct {
	// Origin is the window position in logical screen pixels.
	Origin Point
	// Size is the client size in logical pixels.
	Size Size
}

// ---------------------------------------------------------------------------
// Window options
// ---------------------------------------------------------------------------

// WindowOptions describes one window to open. The geometry is logical
// pixels and is converted with the creating monitor's DPI at the host
// boundary.
//
// The callbacks run on the foreground (host) thread. The host calls them
// directly; the application's OpenWindow wraps them in App.Update so app
// effects land at defined update boundaries (windows platform contract:
// "At a safe foreground entry, key/input/close callbacks execute
// synchronously when their return value controls Windows handling").
type WindowOptions struct {
	// Title is the window title. Empty is allowed.
	Title string
	// Bounds is the requested window bounds in logical pixels. A zero
	// size lets Windows place a default-sized window.
	Bounds Bounds
	// Resizable adds the thick frame and maximize box (reference
	// WS_THICKFRAME|WS_MAXIMIZEBOX in WindowsWindow::new).
	Resizable bool
	// MinimizeBox adds WS_MINIMIZEBOX.
	MinimizeBox bool
	// Show shows the window once created.
	Show bool
	// Activate brings the window to the foreground when shown.
	Activate bool

	// ShouldClose is the close veto: it is called synchronously when a
	// close is requested (WM_CLOSE); returning false vetoes the close
	// and the window stays open. A nil callback lets the close proceed
	// (reference handle_close_msg: no callback -> DefWindowProc ->
	// DestroyWindow).
	ShouldClose func() bool
	// OnClose runs on the foreground thread after the OS window is
	// destroyed (WM_DESTROY), before the host retires the window
	// registry entry.
	OnClose func()
	// OnActivate reports activation changes (WM_ACTIVATE), deferred to
	// the foreground queue like the reference's handle_activate_msg
	// (which spawns the callback on the foreground executor).
	OnActivate func(active bool)
	// OnResize reports client size and scale changes (WM_SIZE and
	// WM_DPICHANGED), delivered synchronously on the foreground thread.
	OnResize func(size Size, scale float32)
	// OnMoved reports window moves (WM_MOVE), synchronously.
	OnMoved func()
	// OnRedraw is the paint hook: WM_PAINT records the window dirty and
	// calls this hook instead of drawing (the renderer arrives with
	// ticket07; reference draw_window -> request_frame).
	OnRedraw func()
	// OnKey receives keyboard platform events (ticket13: *KeyDownEvent,
	// *KeyUpEvent, *ModifiersChangedEvent from the Win32 translation).
	// Returning true consumes the event: the host's pump then skips
	// TranslateMessage, so a handled shortcut does not also insert text
	// (the accelerator pre-dispatch contract).
	OnKey func(event any) bool
	// OnText receives assembled WM_CHAR text (ticket13: surrogate pairs
	// combined, control characters dropped) for the focused input
	// handler.
	OnText func(text string)
	// OnKeyboardLayoutChange receives WM_INPUTLANGCHANGE reports with
	// the active layout's identity and display name (ticket13).
	OnKeyboardLayoutChange func(layout KeyboardLayoutInfo)
}

// Typed window errors.
var (
	// ErrNoHost reports an operation that needs a real platform host but
	// the application has none attached (test applications).
	ErrNoHost = errors.New("gpui: no platform host is attached to this application (real windows require Attach/Run with a started host)")
	// ErrHostNotStarted reports use of a host that never started.
	ErrHostNotStarted = errors.New("gpui: the host has not been started")
	// ErrHostStopped reports a host whose message loop already exited.
	ErrHostStopped = errors.New("gpui: the host message loop has exited")
	// ErrHostUnsupportedPlatform reports the Win32 host on a non-windows
	// build.
	ErrHostUnsupportedPlatform = errors.New("gpui: the Win32 host requires a windows build")
)

// WindowCreateError reports a window that could not be created. It wraps
// the Win32 failure from CreateWindowExW (reference returns the anyhow
// context from WindowsWindow::new; the Go port carries it as a typed
// error).
type WindowCreateError struct {
	// Title is the requested window title for diagnostics.
	Title string
	// Err is the underlying syscall error.
	Err error
}

func (e *WindowCreateError) Error() string {
	return fmt.Sprintf("gpui: could not create window %q: %v", e.Title, e.Err)
}

// Unwrap exposes the underlying Win32 error.
func (e *WindowCreateError) Unwrap() error { return e.Err }

// ---------------------------------------------------------------------------
// The real Window
// ---------------------------------------------------------------------------

// Window is the ambient window context threaded through window-aware
// access (UpdateIn and friends): a logical window identity in the
// application's registry, a leased platform window handle and the
// window-owned root scope.
//
// "Roots belong to the app and its windows" (runtime ownership
// contract): each window's scope is a child of the app's root scope and
// closes when the OS window is destroyed, retiring window-scoped
// registrations and tasks without touching independent windows or
// shared entities (reference: destroying one window retires its scope
// and sends a generation-checked removal, "without destroying
// independent windows or shared entities").
//
// The handle is a lease: the host owns the HWND and its destruction.
// Window methods marshal to the foreground (host) thread; the typed
// action dispatch entry points (Dispatch/DispatchBoxed in action.go)
// stay routing panics until ticket13 wires the focus tree, but now
// carry this window's real identity.
type Window struct {
	app    *App
	id     WindowID
	handle WindowHandle
	scope  *Scope
	title  string

	// focus is the window's focus/keyboard-dispatch runtime (ticket13;
	// focus.go's windowFocusState). Created on first use; touched only
	// on the foreground thread.
	focus *windowFocusState
}

// ID returns the window's logical identity. Identities are assigned in
// opening order on the foreground thread and never reused.
func (w *Window) ID() WindowID { return w.id }

// Handle returns the leased platform window handle. The host owns the
// underlying HWND; the handle value is for lease validation and
// diagnostics.
func (w *Window) Handle() WindowHandle { return w.handle }

// Scope returns the window-owned root scope for this window's
// registrations, retained elements and tasks. It closes when the window
// is destroyed.
func (w *Window) Scope() *Scope { return w.scope }

// Title returns the current window title as the platform reports it.
func (w *Window) Title() (string, error) { return w.handle.Title() }

// SetTitle sets the window title (SetWindowTextW through the lease).
func (w *Window) SetTitle(title string) { w.handle.SetTitle(title) }

// Show shows the window without activating it.
func (w *Window) Show() { w.handle.Show() }

// Hide hides the window.
func (w *Window) Hide() { w.handle.Hide() }

// Activate brings the window to the foreground.
func (w *Window) Activate() { w.handle.Activate() }

// Close requests a close through the veto path: the host delivers
// WM_CLOSE, the ShouldClose callback decides, and the window is
// destroyed only when the close is allowed.
func (w *Window) Close() { w.handle.Close() }

// Bounds returns the window bounds in logical pixels.
func (w *Window) Bounds() (Bounds, error) { return w.handle.Bounds() }

// SetBounds resizes and moves the window to the requested logical
// bounds.
func (w *Window) SetBounds(bounds Bounds) { w.handle.SetBounds(bounds) }

// ScaleFactor returns the window's current scale factor (device pixels
// per logical pixel). Per-monitor DPI changes update it (WM_DPICHANGED).
func (w *Window) ScaleFactor() (float32, error) { return w.handle.ScaleFactor() }

// Alive reports whether the leased window still exists.
func (w *Window) Alive() bool { return w.handle.Alive() }

// NewFocusHandle allocates a focus identity inside this window
// (window.rs FocusHandle::new: the window's focus registry inserts a
// slot with the default tab properties).
func NewFocusHandle(w *Window) FocusHandle {
	if w == nil {
		panic("gpui: NewFocusHandle requires a live window")
	}
	fs := focusState(w)
	fs.seq++
	id := fs.seq
	fs.handles[id] = &focusRef{tabIndex: 0, tabStop: false}
	return FocusHandle{id: id, window: w}
}

// FocusHandleByID looks up a live focus identity in this window (test
// support; the identity must not be released).
func (w *Window) FocusHandleByID(id uint64) (FocusHandle, bool) {
	if w == nil || w.focusRefOf(id) == nil {
		return FocusHandle{}, false
	}
	return FocusHandle{id: id, window: w}, true
}

// ---------------------------------------------------------------------------
// The application window registry
// ---------------------------------------------------------------------------

// Windows returns the open window identities in opening order. It must
// be read on the foreground thread; the registry is only touched there.
func (a *App) Windows() []WindowID {
	ids := make([]WindowID, 0, len(a.windows))
	for id := range a.windows {
		ids = append(ids, id)
	}
	// Opening order = identity order; map iteration is never the
	// delivery order (runtime ownership contract).
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
	return ids
}

// Window returns the open window with the given identity. It must be
// read on the foreground thread.
func (a *App) Window(id WindowID) (*Window, bool) {
	w, ok := a.windows[id]
	return w, ok
}

// removeWindow retires a destroyed window: the registry entry is
// removed and the window's scope closes, cancelling its registrations
// and tasks while independent windows and shared entities are
// untouched. The caller must hold the update (the host's WM_DESTROY
// path wraps this in App.Update).
func (a *App) removeWindow(id WindowID) {
	w, ok := a.windows[id]
	if !ok {
		return
	}
	delete(a.windows, id)
	// Close runs inline: we are already inside the update boundary.
	w.scope.Close()
}

// ---------------------------------------------------------------------------
// Real-application boot path
// ---------------------------------------------------------------------------

// NewApp creates the real application core. The returned app has no
// platform host yet: attach one with Attach (then start the host) or
// block on Run. Deterministic tests keep using NewTestApp.
//
// The executors are the existing scheduler-backed ones; in real mode the
// host's wake mechanism drives the scheduler on the foreground thread
// (see Host.Attach). The scheduler's virtual clock is not advanced by
// the host in this slice: real-time timers arrive with the shutdown
// integration ticket, and task bodies that park on Sleep stay parked.
func NewApp() *App {
	app := newApp(newScheduler())
	return app
}

// Attach wires the platform host as this application's foreground
// dispatcher: work queued on the scheduler wakes the host, and the host
// thread drains it as the foreground thread (reference:
// WindowsDispatcher::dispatch_on_main_thread posts
// WM_GPUI_TASK_DISPATCHED_ON_MAIN_THREAD; the drain is
// run_foreground_task).
//
// Attach must complete before the application is used from other
// goroutines (the wake hook is installed here).
func (a *App) Attach(h *Host) error {
	if h == nil {
		return fmt.Errorf("gpui: Attach requires a non-nil host")
	}
	if a.host != nil {
		if a.host == h {
			return nil
		}
		return fmt.Errorf("gpui: application already attached to another host")
	}
	if err := h.attach(a); err != nil {
		return err
	}
	a.host = h
	return nil
}

// Host returns the attached platform host, or nil for test apps.
func (a *App) Host() *Host { return a.host }

// Run attaches the host (if needed), starts it and blocks until the
// message loop exits: the last window closed (or an explicit host stop)
// quits the loop, mirroring the reference Platform::run returning from
// GetMessageW. All application code runs on the foreground (host)
// thread through window events, posted work and spawned tasks.
//
// Run returns the loop's terminal error. Full application shutdown
// integration (draining scopes, executors and native services) belongs
// to the shutdown ticket; this slice returns when the OS loop ends.
func (a *App) Run(h *Host) error {
	if a.host == nil {
		if err := a.Attach(h); err != nil {
			return err
		}
	} else if a.host != h {
		return fmt.Errorf("gpui: application already attached to another host")
	}
	if err := h.Start(); err != nil {
		return err
	}
	return h.Wait()
}

// OpenWindow opens a real window through the attached host. Creation
// and registration are marshaled to the foreground thread in one unit:
// the platform window is created with the application-wired callbacks,
// and the logical window joins the registry inside one App.Update.
//
// Calling OpenWindow on a test application (no host) fails with
// ErrNoHost.
func (a *App) OpenWindow(opts WindowOptions) (*Window, error) {
	h := a.host
	if h == nil {
		return nil, ErrNoHost
	}

	type openResult struct {
		window *Window
		err    error
	}
	res := make(chan openResult, 1)

	// The creation closure runs on the foreground (host) thread. The
	// user callbacks are wrapped so each event enters the application
	// at an update boundary (safe-boundary rule above); the logical
	// window is registered in the same update that finishes creation.
	accepted := h.runForegroundSync(func() {
		var window *Window
		hostOpts := opts
		if opts.ShouldClose != nil {
			user := opts.ShouldClose
			hostOpts.ShouldClose = func() bool {
				var allow bool
				a.Update(func(*App) { allow = user() })
				return allow
			}
		}
		if opts.OnClose != nil {
			user := opts.OnClose
			hostOpts.OnClose = func() {
				a.Update(func(a *App) { a.removeWindow(window.id) })
				a.Update(func(*App) { user() })
			}
		} else {
			hostOpts.OnClose = func() {
				a.Update(func(a *App) { a.removeWindow(window.id) })
			}
		}
		wrap := func(user func()) func() {
			if user == nil {
				return nil
			}
			return func() { a.Update(func(*App) { user() }) }
		}
		hostOpts.OnMoved = wrap(opts.OnMoved)
		hostOpts.OnRedraw = wrap(opts.OnRedraw)
		// The keyboard input callback routes every event through the
		// logical window's dispatch tree (the reference installs the
		// platform input callback unconditionally in Window::new; an
		// optional user OnKey hook runs first and can consume the event
		// like an interceptor).
		userKey := opts.OnKey
		hostOpts.OnKey = func(event any) bool {
			var handled bool
			a.Update(func(a *App) {
				if userKey != nil && userKey(event) {
					handled = true
					return
				}
				handled = !window.dispatchKeyEvent(event, a)
			})
			return handled
		}
		if opts.OnText != nil {
			user := opts.OnText
			hostOpts.OnText = func(text string) {
				a.Update(func(*App) { user(text) })
			}
		} else {
			// No user hook: WM_CHAR text still routes to the focused
			// input handler (the reference delivers handle_char_msg
			// through the platform input handler).
			hostOpts.OnText = func(text string) {
				a.Update(func(a *App) { window.deliverTextInput(text, a) })
			}
		}
		if opts.OnKeyboardLayoutChange != nil {
			user := opts.OnKeyboardLayoutChange
			hostOpts.OnKeyboardLayoutChange = func(layout KeyboardLayoutInfo) {
				a.Update(func(*App) { user(layout) })
			}
		}
		if opts.OnResize != nil {
			user := opts.OnResize
			hostOpts.OnResize = func(size Size, scale float32) {
				a.Update(func(*App) { user(size, scale) })
			}
		}
		if opts.OnActivate != nil {
			user := opts.OnActivate
			hostOpts.OnActivate = func(active bool) {
				a.Update(func(*App) { user(active) })
			}
		}

		handle, err := h.OpenWindow(hostOpts)
		if err != nil {
			res <- openResult{err: err}
			return
		}
		a.Update(func(a *App) {
			a.windowSeq++
			window = &Window{
				app:    a,
				id:     WindowID(a.windowSeq),
				handle: handle,
				scope:  a.rootScope.Child(),
				title:  opts.Title,
			}
			a.windows[window.id] = window
			res <- openResult{window: window}
		})
	})
	if !accepted {
		res <- openResult{err: ErrHostStopped}
	}

	r := <-res
	if r.err != nil {
		return nil, r.err
	}
	return r.window, nil
}
