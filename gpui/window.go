package gpui

import (
	"errors"
	"fmt"
	"time"
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
	// Kind selects the window flavor. A native AnchoredPopup is
	// rejected on Windows with ErrPopupNotSupported so callers fall
	// back to in-window popovers (ticket26, WindowsWindow::new).
	Kind WindowKind
	// Bounds is the requested window bounds in logical pixels. A zero
	// size lets Windows place a default-sized window.
	Bounds Bounds
	// Resizable adds the thick frame and maximize box (reference
	// WS_THICKFRAME|WS_MAXIMIZEBOX in WindowsWindow::new).
	Resizable bool
	// MinSize is the window's minimum client size in logical pixels
	// (reference WindowOptions::window_min_size,
	// platform.rs:2595). Zero dimensions are unconstrained; the zero
	// value keeps the system default minimum. The Win32 host enforces
	// it on interactive resizing (WM_GETMINMAXINFO raises
	// MINMAXINFO.ptMinTrackSize to the scaled size plus the frame
	// offset, reference events.rs handle_get_min_max_info_msg) and on
	// programmatic sizing (SetBounds and the initial placement clamp
	// the client area). The device-pixel floor is re-derived at the
	// current scale factor, so a per-monitor DPI change re-clamps.
	MinSize Size
	// MinimizeBox adds WS_MINIMIZEBOX.
	MinimizeBox bool
	// Titlebar configures the titlebar (platform.rs WindowOptions::
	// titlebar). nil keeps the system titlebar (the pin hides it on
	// Windows when titlebar is None; the port keeps the old default).
	// AppearsTransparent hides it for a client-drawn titlebar built from
	// WindowControlArea elements (titlebar.go).
	Titlebar *TitlebarOptions
	// Immovable is the pin's !is_movable (platform.rs WindowOptions::
	// is_movable, default true): window control areas then never answer
	// HTCAPTION, so the user cannot drag the window by them. The zero
	// value is movable.
	Immovable bool
	// DirectComposition creates the window for a DirectComposition
	// surface: WS_EX_NOREDIRECTIONBITMAP, which the pin sets on every
	// window unless GPUI_DISABLE_DIRECT_COMPOSITION is set
	// (window.rs:491-493). Without a GDI redirection surface the DWM
	// backdrop (SetBackgroundAppearance Transparent, Blurred, Mica) shows
	// through the transparent parts of a BackgroundTransparent frame.
	// Only a DirectComposition swap chain (Renderer.GetOrCreateSurface's
	// default mode) shows content in such a window; a
	// SurfaceModeHwndAlphaIgnore surface does not. The zero value keeps
	// the redirection surface, as before.
	DirectComposition bool
	// Show shows the window once created.
	Show bool
	// Activate brings the window to the foreground when shown.
	Activate bool
	// PreferredFrameInterval asks the frame source for a lower frame rate
	// for this window (browser-core ticket 47; the port of viz's
	// ExternalBeginFrameSourceWin::SetPreferredInterval,
	// components/viz/service/frame_sinks/external_begin_frame_source_win.cc:
	// 96-110). Zero (the default) is the display rate — every vblank.
	// A value greater than twice the observed vblank interval minus half
	// a millisecond (about 33 ms on a 60 Hz display) selects HALF RATE:
	// the frame source skips every other vblank for this window
	// (external_begin_frame_source_win.cc:53-62), vsync-aligned, so its
	// animations draw every other vblank. Values in between keep the
	// full rate — like Chromium, only full and half rate exist, and the
	// effective interval is always a whole number of vblanks. Change it
	// live with Window.SetPreferredFrameInterval. A negative value is
	// rejected at open. Test windows ignore it (their frames arrive
	// through SimulateFrameRequest).
	PreferredFrameInterval time.Duration
	// KeyDispatchOrder selects when key bindings dispatch relative to
	// the element key listeners (browser-core ticket 09, spec D5). The
	// zero value is BindingsBeforeListeners, the GPUI shape: bindings
	// dispatch before listeners, and an action that stops propagation
	// hides the key from every listener; core default actions still run
	// after the listeners and honour Window.PreventDefault. Setting it
	// to ListenersBeforeBindings gives the browser order: key listeners
	// run first, then bindings and the core default actions in the
	// default-action position, all skipped when a listener prevents the
	// default. Change it live with Window.SetKeyDispatchOrder.
	KeyDispatchOrder KeyDispatchOrder

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
	// (which spawns the callback on the foreground executor). The window's
	// own activation handling (the cached active flag, the
	// ObserveWindowActivation observers and the refresh) runs first,
	// whether or not this hook is set.
	OnActivate func(active bool)
	// OnResize reports client size and scale changes (WM_SIZE and
	// WM_DPICHANGED), delivered synchronously on the foreground thread.
	OnResize func(size Size, scale float32)
	// OnMoved reports window moves (WM_MOVE), synchronously.
	OnMoved func()
	// OnRedraw is the window's present hook (Base ticket 03; the
	// reference Window::present, window.rs:3325, which hands the
	// rendered scene to the platform window). Every window is driven by
	// the frame loop (Base ticket 33): notify of an entity its last frame
	// accessed, Refresh, RefreshWindows and animation frames wake it, and
	// WM_PAINT delivers the window's frame request (draw_window ->
	// request_frame, window.rs:1715-1843): pending next-frame callbacks
	// run, a dirty window with a root view is drawn, and then this hook
	// runs whenever a drawn frame awaits presentation — present
	// LastDrawnScene(w) through the window's Surface. Without the hook the
	// window is still drawn; presenting the scene is up to the
	// application (the port's renderer is application-owned).
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
	// OnAppearanceChanged receives the system appearance change report
	// (ticket26): WM_SETTINGCHANGE with the ImmersiveColorSet area.
	// Delivered synchronously on the foreground thread after the
	// window's cached appearance changed.
	OnAppearanceChanged func()
	// OnHoverStatusChange receives hover enter/leave reports (ticket26,
	// the cursor slice's WM_MOUSEMOVE tracking and WM_MOUSELEAVE
	// clearing).
	OnHoverStatusChange func(hovered bool)
	// OnFileDrop receives the OLE drop target's file-drop events
	// (ticket24: the Entered/Pending/Submit/Exited/Ended events with
	// window-relative logical positions — the pin's input callback
	// FileDrop arm, delivered synchronously on the host thread from
	// the IDropTarget calls, including inside OLE's modal drag loop).
	// App.OpenWindow always installs its own dispatch wrapper (the
	// events route through Window.DispatchInput), so this hook is an
	// observer that runs first.
	OnFileDrop func(event FileDropEvent)
	// fileDropAnswer is the installed dispatch wrapper's host-side
	// form (browser-core ticket 16): it returns the drop effect the
	// drag session negotiated for the event, the answer the OLE
	// IDropTarget callbacks report to the drag loop. Set by
	// App.OpenWindow; never set by user code (unexported).
	fileDropAnswer func(event FileDropEvent) uint32
	// OnGesture receives the Direct Manipulation events (ticket24:
	// *ScrollWheelEvent and *PinchEvent with gesture phases, drained at
	// the window's paint entry).
	OnGesture func(event any)
	// OnMouseInput receives the client-area mouse events of the window
	// (Base ticket 34; gpui_windows events.rs:114, 138-151, 371-398,
	// 489-651): *MouseMoveEvent, *MouseDownEvent, *MouseUpEvent and
	// *ScrollWheelEvent, translated from WM_MOUSEMOVE, the eight
	// WM_[LRMX]BUTTON(DOWN|UP) messages, WM_MOUSEWHEEL and WM_MOUSEHWHEEL.
	// Returning true reports the event handled (the message result 0).
	// App.OpenWindow always routes these events through the window's
	// element dispatch (Window.DispatchInput); an optional user hook runs
	// first and consumes the event by returning true, like OnKey.
	OnMouseInput func(event any) bool
	// OnHitTestWindowControl answers the host's window-control hit test
	// (the pin's on_hit_test_window_control, window.rs:1928): the window
	// control area at a window-local logical position, if any. The host
	// asks it on WM_NCHITTEST. App.OpenWindow installs one that consults
	// the window's rendered WindowControlArea elements; a user hook runs
	// first and wins when it reports an area.
	OnHitTestWindowControl func(position Point) (WindowControlArea, bool)
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

	// input is the window's mouse-family input state (ticket24;
	// mouseevent.go's windowInputState: the tracked mouse position,
	// hit test, prevent-default flag, input modality and hitbox id
	// counter). Created on first use; touched only on the foreground
	// thread.
	input *windowInputState

	// overlays is the window's top layer (browser-core ticket 21,
	// toplayer.go's windowOverlayState: the overlay handle registry,
	// the ordered layer and the queued toggle notifications). Created
	// on first use; touched only on the foreground thread.
	overlays *windowOverlayState

	// frames is the window's frame-loop state (Base ticket 03,
	// frameloop.go: the invalidator's dirty flag, next-frame callbacks,
	// needs-present and the tracked rendered views). Touched only on the
	// foreground thread.
	frames *windowFrameState

	// autoscroll is the pending autoscroll request (window.rs
	// requested_autoscroll, R:window.rs:1276) and autoscrollFrame the frame
	// it was made in. The pin clears the request at every draw start
	// (R:window.rs:3165); the port drops a request made in another frame
	// when it is taken, which answers the same (Base ticket 37).
	autoscroll      *Bounds
	autoscrollFrame *frameDrawState
}

// RequestAutoscroll asks the containing elements to scroll so that bounds
// (window coordinates) become visible (window.rs request_autoscroll,
// R:window.rs:3991-3999). Call it during prepaint: a containing List
// takes the request after prepainting the item that made it and, when it
// decides to scroll, prepaints its items again at the new position (see
// ListElement.Prepaint). As in the pin, a later request in the same frame
// replaces an earlier one. It panics outside a window draw (the pin's
// debug_assert_prepaint).
func (w *Window) RequestAutoscroll(bounds Bounds) {
	frame := currentFrame(w)
	w.autoscroll, w.autoscrollFrame = &bounds, frame
}

// TakeAutoscroll takes the pending autoscroll request, if any (window.rs
// take_autoscroll, R:window.rs:4001-4006): the containing-element half of
// RequestAutoscroll. A request is taken at most once; one made in an
// earlier frame is gone (R:window.rs:3165). It panics outside a window
// draw.
func (w *Window) TakeAutoscroll() (Bounds, bool) {
	frame := currentFrame(w)
	bounds, madeIn := w.autoscroll, w.autoscrollFrame
	w.autoscroll, w.autoscrollFrame = nil, nil
	if bounds == nil || madeIn != frame {
		return Bounds{}, false
	}
	return *bounds, true
}

// prepaintStateIndex marks how far the frame under construction's
// prepaint registrations reach (window.rs PrepaintStateIndex,
// R:window.rs:1124-1131, taken by prepaint_index, R:window.rs:3712-3721),
// so a discarded prepaint can be cut back to it (transact) and a cached
// view's prepaint range can be reused by the next frame (reuse_prepaint,
// view_cache.go).
//
// Mapping onto the port: hitboxes, deferred draws, the dispatch tree, the
// accessed element states (the frame's ordered log) and the line-layout
// index are the pin's fields. The port also registers tab stops and their
// groups during prepaint (gpui/div.go registerDivElementDispatch; the pin
// does so at paint, outside a transaction) and allows mouse listeners
// then, so both are marked as well (mouse listeners for transact only:
// reuse carries them with the paint range, as the pin). The pin's tooltip
// requests have no port counterpart. A cut-back accessed element state
// leaves the frame's log but stays in its retention set (retained one more
// frame).
type prepaintStateIndex struct {
	hitboxes              int
	mouseListeners        int
	deferredDraws         int
	dispatchTree          int
	tabStops              int
	tabStopPath           tabStopPath
	accessedElementStates int
	lineLayout            int
}

// prepaintIndex is window.rs prepaint_index (R:window.rs:3712-3721).
func (w *Window) prepaintIndex() prepaintStateIndex {
	next := focusState(w).next
	if next == nil {
		panic("gpui: no frame is being built (prepaint runs inside a window draw)")
	}
	frame := currentFrame(w)
	return prepaintStateIndex{
		hitboxes:              len(next.hitboxes),
		mouseListeners:        len(next.mouseListeners),
		deferredDraws:         len(frame.deferredDraws),
		dispatchTree:          len(next.dispatchTree.nodes),
		tabStops:              len(next.tabStops.insertionHistory),
		tabStopPath:           append(tabStopPath{}, next.tabStops.currentPath...),
		accessedElementStates: len(frame.accessedElementStateLog),
		lineLayout:            frame.text.layoutIndex(),
	}
}

// truncatePrepaint cuts the frame under construction's prepaint
// registrations back to index (the Err arm of window.rs transact,
// R:window.rs:3970-3987).
func (w *Window) truncatePrepaint(index prepaintStateIndex) {
	next := focusState(w).next
	next.hitboxes = next.hitboxes[:index.hitboxes]
	next.mouseListeners = next.mouseListeners[:index.mouseListeners]
	frame := currentFrame(w)
	frame.deferredDraws = frame.deferredDraws[:index.deferredDraws]
	frame.accessedElementStateLog = frame.accessedElementStateLog[:index.accessedElementStates]
	frame.text.truncateLayouts(index.lineLayout)
	truncateDispatchTree(next.dispatchTree, index.dispatchTree)
	truncateTabStops(next.tabStops, index.tabStops, index.tabStopPath)
}

// truncateDispatchTree is DispatchTree::truncate (R:key_dispatch.rs:
// 310-321): drop the nodes from index on, with their focus and view
// entries.
func truncateDispatchTree(t *DispatchTree, index int) {
	for _, node := range t.nodes[index:] {
		if node.hasFocusID {
			delete(t.focusableNodeIDs, node.focusID)
		}
		if node.hasViewID {
			delete(t.viewNodeIDs, node.viewID)
		}
	}
	t.nodes = t.nodes[:index]
}

// truncateTabStops drops the tab-stop operations recorded from history
// index on: their stops leave the order, an id whose latest insertion is
// dropped falls back to its earlier one, and the group path returns to
// path (the port's counterpart of cutting back the pin's paint-time tab
// stops; see prepaintStateIndex).
func truncateTabStops(t *TabStopMap, history int, path tabStopPath) {
	t.currentPath = path
	if history >= len(t.insertionHistory) {
		return
	}
	t.insertionHistory = t.insertionHistory[:history]
	kept := t.order[:0]
	dropped := map[uint64]struct{}{}
	for _, node := range t.order {
		if node.index < history {
			kept = append(kept, node)
		} else {
			dropped[node.id] = struct{}{}
		}
	}
	t.order = kept
	for id := range dropped {
		delete(t.byID, id)
	}
	for _, node := range t.order {
		if _, ok := dropped[node.id]; ok {
			if prev, ok := t.byID[node.id]; !ok || node.index > prev.index {
				t.byID[node.id] = node
			}
		}
	}
}

// transact runs a prepaint whose registrations are discarded when it
// fails (window.rs transact, R:window.rs:3962-3989): on ok false the
// frame is cut back to where it stood before f. The List's autoscroll
// path prepaints its items, learns the requested bounds, and prepaints
// them again at the new scroll position. (The pin's transact is public;
// the port keeps it internal, as no caller outside gpui needs it yet.)
func transact[T any](w *Window, f func(w *Window) (T, bool)) (T, bool) {
	index := w.prepaintIndex()
	result, ok := f(w)
	if !ok {
		w.truncatePrepaint(index)
	}
	return result, ok
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
// bounds; Bounds and SetBounds round-trip exactly (to device rounding),
// so a saved position restores without drift.
func (w *Window) SetBounds(bounds Bounds) { w.handle.SetBounds(bounds) }

// ScaleFactor returns the window's current scale factor (device pixels
// per logical pixel). Per-monitor DPI changes update it (WM_DPICHANGED).
func (w *Window) ScaleFactor() (float32, error) { return w.handle.ScaleFactor() }

// Alive reports whether the leased window still exists.
func (w *Window) Alive() bool { return w.handle.Alive() }

// Minimize minimizes the window (ticket26; ShowWindowAsync
// SW_MINIMIZE). The restore reports the client size again through the
// WM_SIZE policy.
func (w *Window) Minimize() { w.handle.Minimize() }

// Zoom toggles the maximized state (ticket26; a hidden window records
// the pending state and applies it when shown).
func (w *Window) Zoom() { w.handle.Zoom() }

// IsMaximized reports whether the window is maximized (ticket26).
func (w *Window) IsMaximized() (bool, error) { return w.handle.IsMaximized() }

// IsMinimized reports whether the window is minimized (ticket26).
func (w *Window) IsMinimized() (bool, error) { return w.handle.IsMinimized() }

// IsActive reports whether this window is the active window (ticket26).
func (w *Window) IsActive() (bool, error) { return w.handle.IsActive() }

// RequestAttention flashes the window's taskbar entry once when it is
// not active (ticket26).
func (w *Window) RequestAttention() { w.handle.RequestAttention() }

// MousePosition returns the cursor position in window-local logical
// pixels (ticket26).
func (w *Window) MousePosition() (Point, error) { return w.handle.MousePosition() }

// IsHovered reports whether the cursor is over this window (ticket26).
func (w *Window) IsHovered() (bool, error) { return w.handle.IsHovered() }

// ShowCharacterPalette opens the Windows character palette for this
// window when it is the foreground window (ticket26: modifier
// release/restore with partial-send cleanup).
func (w *Window) ShowCharacterPalette() error { return w.handle.ShowCharacterPalette() }

// PlaySystemBell plays the Windows default beep (ticket26). It is
// user-audible; tests do not invoke it.
func (w *Window) PlaySystemBell() { w.handle.PlaySystemBell() }

// Appearance returns the window's cached system appearance, updated by
// the ImmersiveColorSet observer (ticket26).
func (w *Window) Appearance() (WindowAppearance, error) { return w.handle.Appearance() }

// SetBackgroundAppearance sets the window's background compositing
// mode: the DWM behavior plus the renderer clear path (ticket26).
func (w *Window) SetBackgroundAppearance(appearance WindowBackgroundAppearance) {
	w.handle.SetBackgroundAppearance(appearance)
}

// BackgroundAppearance returns the window's background compositing
// mode (ticket26).
func (w *Window) BackgroundAppearance() (WindowBackgroundAppearance, error) {
	return w.handle.BackgroundAppearance()
}

// ClearColor returns the renderer clear color for the window's current
// background appearance (the renderer half of SetBackgroundAppearance,
// ticket26: opaque modes clear to opaque white, the rest to fully
// transparent).
func (w *Window) ClearColor() (Color, error) { return w.handle.ClearColor() }

// Display returns the window's current monitor record (ticket26,
// updated by WM_DISPLAYCHANGE and window moves).
func (w *Window) Display() (*DisplayInfo, error) { return w.handle.Display() }

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
// removed, the window's scope closes, cancelling its registrations
// and tasks while independent windows and shared entities are
// untouched, and the window's element runtime is dropped (render.go's
// RemoveWindowView path — the reference window removal drops the
// window's frame state with the registry entry, app.rs 1978-1985's
// trail). The caller must hold the update (the host's WM_DESTROY
// path wraps this in App.Update).
func (a *App) removeWindow(id WindowID) {
	w, ok := a.windows[id]
	if !ok {
		return
	}
	delete(a.windows, id)
	// Close runs inline: we are already inside the update boundary.
	w.scope.Close()
	// The element runtime retires with the window (idempotent; the
	// test-window close path calls it again as a no-op). Lease
	// releases queued by the scope close flush at this update's
	// boundary, after removeWindow returned.
	dropDrawState(w)
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
	if opts.PreferredFrameInterval < 0 {
		return nil, fmt.Errorf("gpui: WindowOptions.PreferredFrameInterval must be non-negative, got %v", opts.PreferredFrameInterval)
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
		// WM_PAINT delivers the window's frame request (Base ticket 03,
		// frameloop.go; events.rs draw_window -> request_frame). The user
		// OnRedraw hook is the present step the request runs.
		hostOpts.OnRedraw = func() {
			if window == nil {
				return
			}
			window.requestFrame()
		}
		hostOpts.OnAppearanceChanged = wrap(opts.OnAppearanceChanged)
		if opts.OnHoverStatusChange != nil {
			user := opts.OnHoverStatusChange
			hostOpts.OnHoverStatusChange = func(hovered bool) {
				a.Update(func(*App) { user(hovered) })
			}
		}
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
		// A size or scale change refreshes the window (Window::bounds_changed
		// calls refresh, window.rs:2694-2705) before the user hook runs.
		userResize := opts.OnResize
		hostOpts.OnResize = func(size Size, scale float32) {
			if window == nil && userResize == nil {
				// WM_SIZE during creation, before registration: nothing to
				// refresh yet (the window starts dirty) and no user hook.
				return
			}
			a.Update(func(*App) {
				if window != nil {
					window.Refresh()
				}
				if userResize != nil {
					userResize(size, scale)
				}
			})
		}
		// Activation changes reach the window's own handling before the
		// user hook (the platform on_active_status_change callback,
		// window.rs:1887-1902: the active flag, the activation observers,
		// then a refresh).
		userActivate := opts.OnActivate
		hostOpts.OnActivate = func(active bool) {
			a.Update(func(a *App) {
				if window != nil {
					window.activationChanged(active, a)
				}
				if userActivate != nil {
					userActivate(active)
				}
			})
		}
		// The file-drop callback routes the OLE drop target's events
		// through the window's input dispatch (ticket24: the pin's
		// input callback delivers PlatformInput::FileDrop straight into
		// dispatch_event's translation arm — Entered/Pending become
		// synthetic mouse moves, Submit a mouse up, Exited/Ended ride the
		// mouse listener path). An optional user hook observes first.
		// Browser-core ticket 16: the wrapper returns the drop effect the
		// drag session negotiated for the event (the dragenter/dragover
		// dispatch under the position), which the OLE IDropTarget
		// callbacks report to the drag loop — COPY only over a target
		// that accepts the drag, NONE otherwise.
		userFileDrop := opts.OnFileDrop
		hostOpts.fileDropAnswer = func(event FileDropEvent) uint32 {
			var answer uint32
			a.Update(func(a *App) {
				if userFileDrop != nil {
					userFileDrop(event)
				}
				// The pointer form matches the mouse-family pointer events
				// dispatchInput routes.
				window.dispatchInput(&event, a)
				answer = window.oleDropEffectAnswer()
			})
			return answer
		}
		hostOpts.OnFileDrop = nil
		// The gesture callback routes Direct Manipulation events through
		// the same dispatch (the pin's input callback ScrollWheel/Pinch
		// arms).
		userGesture := opts.OnGesture
		hostOpts.OnGesture = func(event any) {
			a.Update(func(a *App) {
				if userGesture != nil {
					userGesture(event)
				}
				window.dispatchInput(event, a)
			})
		}
		// The mouse input callback routes the client-area mouse events
		// through the window's dispatch_event (Base ticket 34; the pin's
		// platform input callback, window.rs:5739, whose answer the host
		// turns into the message result: handled = !propagate,
		// events.rs:394). An optional user OnMouseInput hook runs first
		// and can consume the event like OnKey's interceptor. A message
		// delivered before the window joined the registry is not handled.
		userMouse := opts.OnMouseInput
		hostOpts.OnMouseInput = func(event any) bool {
			if window == nil {
				return false
			}
			var handled bool
			a.Update(func(a *App) {
				if userMouse != nil && userMouse(event) {
					handled = true
					return
				}
				if _, open := a.windows[window.id]; !open {
					return
				}
				handled = !window.dispatchInput(event, a).Propagate
			})
			return handled
		}
		// The window-control hit test reads the rendered frame's
		// WindowControlArea hitboxes (window.rs:1928-1945,
		// on_hit_test_window_control). It only reads state, so it runs
		// outside an update; the host asks it on the foreground thread.
		userHitTest := opts.OnHitTestWindowControl
		hostOpts.OnHitTestWindowControl = func(position Point) (WindowControlArea, bool) {
			if userHitTest != nil {
				if area, ok := userHitTest(position); ok {
					return area, true
				}
			}
			if window == nil {
				return 0, false
			}
			if _, open := a.windows[window.id]; !open {
				return 0, false
			}
			return window.windowControlAreaAt(position)
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
				// The frame loop drives every window; OnRedraw is its present
				// step. The window starts dirty (window.rs:178).
				frames: newWindowFrameState(opts.OnRedraw, opts.PreferredFrameInterval),
			}
			if opts.KeyDispatchOrder != BindingsBeforeListeners {
				// browser-core 09: the key dispatch order option, stored in
				// the focus runtime (SetKeyDispatchOrder validates it).
				window.SetKeyDispatchOrder(opts.KeyDispatchOrder)
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
