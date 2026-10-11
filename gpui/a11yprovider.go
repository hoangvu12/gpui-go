package gpui

// This file is the port's native UIA provider half (ticket22): the
// A11yProvider model over App that bridges the ticket-21 semantic
// snapshots to the native AccessKit adapter service (internal/native,
// reserved slot 9 of the rev-10 artifact).
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/window.rs 1145-1176 (a11y_init /
//     a11y_tree_update): the adapter is created on the window's message
//     thread at window setup with a minimal initial tree (a Role::Window
//     root labeled with the window title); every completed frame sends
//     the tree through update_if_active — the update is only decoded
//     when an assistive technology has connected, and the queued events
//     are raised after the state lock drops
//     (UiaRaiseAutomationPropertyChangedEvent may deliver a nested
//     WM_GETOBJECT back into the window procedure).
//   - crates/gpui/src/window.rs 1573-1653 (the window-creation a11y
//     closure): the activation closure flips the active flag and hands
//     the stored initial tree to the adapter; the receiving foreground
//     task forces a redraw so the next frame publishes the complete
//     tree ("A11y can be activated at any time ... we force a new
//     frame, which will then send a correct TreeUpdate").
//   - crates/gpui_windows/src/events.rs 855-871 (handle_wm_getobject):
//     the adapter answers UIA root/client object-id queries (the
//     LRESULT conversion runs outside every lock); everything else
//     falls to DefWindowProc.
//   - crates/gpui/src/window.rs 7130-7200 (handle_a11y_action): the
//     queued actions are drained on the foreground thread — listeners
//     first, then the built-ins (Click synthesizes MouseDown/MouseUp at
//     the node bounds center, Focus focuses the bound handle, Blur
//     blurs the window).
//   - crates/gpui/src/element.rs 400-470 (node construction): bounds
//     are reported in physical pixels (logical x scale, f64), the
//     properties through write_a11y_info, the children through the
//     builder's push_child.
//
// Bounded deviations, recorded honestly:
//
//   - The pin creates the adapter unconditionally in WindowsWindow::new;
//     the port creates it at the ticket-21 activation entry
//     (ActivateWindowA11y), because the port's accessibility runtime is
//     inert until activation and hostless test windows never carry a
//     provider. The initial tree label is the window title exactly like
//     the pin's initial_window_title.
//   - The pin drains the action channel through a foreground task that
//     receives every queued request as it arrives; the native service
//     queues the same way (do_action only queues), and the Go side
//     POLLS: the publication seam drains once per completed frame and
//     PollWindowA11yActions drains on demand.
//   - is_focused at creation reads the host's is_active answer
//     (GetActiveWindow == hwnd, the ticket26 port) instead of
//     GetForegroundWindow == hwnd: both answer "is this window the
//     foreground window" on the message thread, and binding
//     GetForegroundWindow would touch files this slice does not own.
//   - The serialized node's advertised actions are the ticket-21
//     snapshot's listener-registered actions plus Focus for
//     focus-bound nodes (div.rs 4153-4161). The reference additionally
//     advertises Click for plain div click listeners, which the port's
//     element layer does not surface into the snapshot yet.
//   - The FROZEN native actions-mask decode (a11y.rs decode_update)
//     reads the bit at index i as the action whose TAG equals the
//     bit's value, so only Focus (2), Increment (4) and
//     SetTextSelection (8) are representable in the mask; Click, Blur,
//     Decrement, SetValue, ReplaceSelectedText and ShowContextMenu
//     cannot be advertised to the AT through the frozen ABI (any other
//     bit fails the whole publish). The serializer encodes the
//     representable bits and omits the rest; the REQUEST path (the
//     drained actions) routes all nine actions.
//   - The native service flips its active flag on ANY routed
//     WM_GETOBJECT (including object ids the adapter declines); the
//     pin flips it only inside the activation closure. The Go side
//     mirrors the native observable (the flag the service reports) and
//     forces the activation redraw on every false-to-true edge.

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"gpui-go/internal/native"
)

// wmGetObject is WM_GETOBJECT (winuser.h 0x003D): the assistive
// technology's window-query message. Declared here (platform-neutral)
// because the window procedure's routing case lives in
// win32host_windows.go while the provider model is neutral.
const wmGetObject = 0x003D

// ---------------------------------------------------------------------------
// The application's native a11y service
// ---------------------------------------------------------------------------

// ErrA11yService reports a failure to resolve the native a11y provider
// service (the artifact missing the capability, or a failed load).
var ErrA11yService = fmt.Errorf("gpui: native a11y service unavailable")

// A11yService returns the application's native a11y provider service
// (App::a11y wiring through the process native library): lazily
// resolved once; the first failure sticks, exactly like SvgRenderer's
// accessor. Test applications may call it too — the native library is
// process-wide — but the provider model only uses it for real
// (host-leased) windows.
func (a *App) A11yService() (*native.A11yService, error) {
	if s := a.a11yAdapterService; s != nil {
		return s, nil
	}
	if err := a.a11yServiceErr; err != nil {
		return nil, err
	}
	lib, err := native.Load(native.Options{})
	if err != nil {
		a.a11yServiceErr = fmt.Errorf("%w: loading the native artifact: %w", ErrA11yService, err)
		return nil, a.a11yServiceErr
	}
	svc, err := lib.A11y()
	if err != nil {
		a.a11yServiceErr = fmt.Errorf("%w: %w", ErrA11yService, err)
		return nil, a.a11yServiceErr
	}
	a.a11yAdapterService = svc
	return svc, nil
}

// The native a11y client's typed errors, aliased so callers can
// errors.Is them without importing internal/native (svg.go's
// ErrSvgParse pattern).
var (
	// ErrA11yNativeStaleHandle reports a native adapter handle that
	// encoded a past generation (destroyed or slot reused).
	ErrA11yNativeStaleHandle = native.ErrA11yStaleHandle
	// ErrA11yNativeBadValue reports the native argument rejection.
	ErrA11yNativeBadValue = native.ErrA11yBadValue
	// ErrA11yNativeCapacity reports a too-small action poll buffer.
	ErrA11yNativeCapacity = native.ErrA11yCapacity
)

// ErrA11yNoProvider reports a window with no native provider (a test
// window, an unactivated runtime, or a provider that failed to create).
var ErrA11yNoProvider = fmt.Errorf("gpui: the window has no native a11y provider")

// ProbeNativeA11yAdapter reports whether one native adapter handle
// value still answers (the slot-generation staleness probe: a handle
// whose slot was destroyed or reused reports the stale-handle error).
// Diagnostics surface — the provider model never needs to probe its
// own live handle.
func ProbeNativeA11yAdapter(a *App, handle uint64) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("gpui: ProbeNativeA11yAdapter requires an application")
	}
	svc, err := a.A11yService()
	if err != nil {
		return false, err
	}
	return svc.IsActive(native.A11yAdapterHandle(handle))
}

// ---------------------------------------------------------------------------
// The per-window provider
// ---------------------------------------------------------------------------

// a11yProvider is one window's native UIA provider half (the pin's
// A11yState): the adapter handle plus the observable provider state.
// The native adapter calls run on the window's message thread (the
// host thread) — creation at window setup (a11y_init), publication per
// completed frame (a11y_tree_update), the WM_GETOBJECT answer and the
// queued-action drain. The mutable diagnostics are mutex-guarded
// because the WM_GETOBJECT route runs inside window-procedure
// reentrancy while A11yProviderState may be read from any goroutine;
// no lock is ever held across a native call (the nested-message
// discipline the pin documents at events.rs 866-869).
type a11yProvider struct {
	app     *App
	service *native.A11yService
	window  *Window
	hwnd    uintptr
	title   string
	handle  native.A11yAdapterHandle

	mu            sync.Mutex
	active        bool
	activated     bool
	wmGetObjects  uint64
	publishes     uint64
	lastRecord    native.A11yPublishRecord
	actionsRouted uint64
	destroyed     bool
	lastErr       error
}

// a11yProviders is the hwnd -> provider registry the WM_GETOBJECT
// routing consults (the pin resolves the adapter through the window's
// state). The lock is never held across a native call: a nested
// WM_GETOBJECT delivered inside the adapter's own LRESULT conversion
// would re-enter the lookup on the same thread.
var (
	a11yProvidersMu sync.Mutex
	a11yProviders   = map[uintptr]*a11yProvider{}
)

// a11yProviderForHWND resolves the provider serving one window handle.
func a11yProviderForHWND(hwnd uintptr) *a11yProvider {
	a11yProvidersMu.Lock()
	p := a11yProviders[hwnd]
	a11yProvidersMu.Unlock()
	return p
}

// registerA11yProvider installs the provider for its hwnd.
func registerA11yProvider(p *a11yProvider) {
	a11yProvidersMu.Lock()
	a11yProviders[p.hwnd] = p
	a11yProvidersMu.Unlock()
}

// unregisterA11yProvider removes the provider BEFORE the adapter drops
// (late or nested WM_GETOBJECT queries then fall to DefWindowProc
// instead of touching a slot that is being destroyed).
func unregisterA11yProvider(p *a11yProvider) {
	a11yProvidersMu.Lock()
	if a11yProviders[p.hwnd] == p {
		delete(a11yProviders, p.hwnd)
	}
	a11yProvidersMu.Unlock()
}

// ensureA11yProvider creates the window's native provider (the pin's
// a11y_init at window setup): the adapter is created on the window's
// message thread — never inside a WM_GETOBJECT handler — with the
// initial minimal tree labeled by the window title, and a window-scope
// sentinel entity whose release destroys the adapter at real window
// closure (the pin drops A11yState with the window; the UIA provider
// references release apartment-affine on the message thread).
//
// Hostless test windows and failed service resolution leave no
// provider: the ticket-21 Go snapshot model is then the whole
// observable, with the failure recorded (surfaced through
// A11yProviderState).
func (st *windowA11yState) ensureA11yProvider(w *Window) {
	if st.provider != nil || st.providerErr != nil {
		return
	}
	app := w.app
	h := app.Host()
	if h == nil {
		// Test application: no message thread, no HWND, no provider.
		return
	}
	hwnd := w.handle.Hwnd()
	if hwnd == 0 {
		// Hostless lease (test window): no provider.
		return
	}
	svc, err := app.A11yService()
	if err != nil {
		st.providerErr = err
		return
	}
	// a11y_init's is_focused: GetForegroundWindow() == hwnd in the pin;
	// the port reads the host's is_active answer on the message thread.
	isFocused := false
	if active, err := w.handle.IsActive(); err == nil {
		isFocused = active
	}
	title := st.title
	created := false
	ok := h.runForegroundSync(func() {
		handle, err := svc.CreateAdapter(hwnd, isFocused, title)
		if err != nil {
			st.providerErr = fmt.Errorf("gpui: a11y adapter create: %w", err)
			return
		}
		p := &a11yProvider{
			app:     app,
			service: svc,
			window:  w,
			hwnd:    hwnd,
			title:   title,
			handle:  handle,
		}
		st.provider = p
		registerA11yProvider(p)
		created = true
	})
	if !ok {
		st.providerErr = fmt.Errorf("gpui: a11y adapter create: %w", ErrHostStopped)
		return
	}
	if !created {
		return
	}
	// The close sentinel: an entity leased by the window's scope whose
	// release callback destroys the adapter when the OS window closes
	// (WM_DESTROY -> removeWindow -> scope close -> lease release ->
	// flush boundary), keeping the destroy on the message thread inside
	// the window's own destruction. NewEntity wraps its own update.
	NewEntity(app, w.Scope(), func(s *a11yCloseSentinel, cx *Context[a11yCloseSentinel]) {
		s.window = w
		cx.OnRelease(func(s *a11yCloseSentinel, a *App) {
			if st := windowA11yStateOf(s.window); st != nil {
				st.destroyA11yProvider()
			}
		})
	})
}

// a11yCloseSentinel is the window-scope release carrier.
type a11yCloseSentinel struct {
	window *Window
}

// destroyA11yProvider retires the window's native provider (the pin's
// A11yState drop at window closure). The adapter handle is
// deregistered first so late WM_GETOBJECT queries fall to
// DefWindowProc; DestroyAdapter then runs on the message thread. The
// provider pointer stays as a tombstone: its record reports Destroyed
// and every later provider operation is a no-op.
func (st *windowA11yState) destroyA11yProvider() {
	p := st.provider
	if p == nil {
		return
	}
	p.destroy()
}

// destroy drops the native adapter. Called from CloseWindowA11y (the
// explicit a11y teardown) and the window-scope release (the real
// window close). Idempotent.
func (p *a11yProvider) destroy() {
	p.mu.Lock()
	if p.destroyed {
		p.mu.Unlock()
		return
	}
	p.destroyed = true
	p.mu.Unlock()
	h := p.app.Host()
	if h == nil {
		return
	}
	ok := h.runForegroundSync(func() {
		unregisterA11yProvider(p)
		if err := p.service.DestroyAdapter(p.handle); err != nil {
			p.recordErr(fmt.Errorf("gpui: a11y adapter destroy: %w", err))
		}
	})
	if !ok {
		// The host loop is gone: the adapter could not be destroyed on
		// the message thread. The native slot stays occupied until the
		// process exits; recorded honestly.
		p.recordErr(fmt.Errorf("gpui: a11y adapter destroy: %w", ErrHostStopped))
	}
}

// recordErr stores the last provider error (diagnostics only; the
// provider never panics the window procedure).
func (p *a11yProvider) recordErr(err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	p.lastErr = err
	p.mu.Unlock()
}

// syncActiveFlag refreshes the observed native active flag (the pin's
// sync_active_flag at frame start/end) and reports the pre-sync state.
// It returns whether the flag transitioned false-to-true (the
// activation edge).
func (p *a11yProvider) syncActiveFlag() (wasActive bool, activatedNow bool) {
	active, err := p.service.IsActive(p.handle)
	if err != nil {
		p.recordErr(fmt.Errorf("gpui: a11y is_active: %w", err))
		return false, false
	}
	p.mu.Lock()
	wasActive = p.active
	p.active = active
	if active {
		p.activated = true
	}
	p.mu.Unlock()
	return wasActive, active && !wasActive
}

// isActiveFlag reports the last observed native active flag.
func (p *a11yProvider) isActiveFlag() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// ---------------------------------------------------------------------------
// WM_GETOBJECT routing (events.rs 855-871)
// ---------------------------------------------------------------------------

// routeWindowWmGetObject answers one WM_GETOBJECT for a window of this
// host (the window procedure's routing case; runs on the window's
// message thread). handled reports the adapter's answer: true means the
// caller must return result as the LRESULT; false means DefWindowProc
// owns the message (no provider, or the adapter declining the object
// id), exactly like the pin's None arm.
//
// The path touches NO application state: the registry lookup, then the
// native adapter (which answers from its own tree — the last published
// snapshot), so a delivered query never waits on a Go update. The
// registry lock is dropped before the native call because the
// adapter's LRESULT conversion may deliver a nested WM_GETOBJECT back
// into this same procedure on this same thread.
func routeWindowWmGetObject(h *Host, hwnd uintptr, wparam uintptr, lparam uintptr) (result uintptr, handled bool) {
	p := a11yProviderForHWND(hwnd)
	if p == nil {
		return 0, false
	}
	answer, err := p.service.HandleWmGetObject(p.handle, wparam, lparam)
	p.mu.Lock()
	p.wmGetObjects++
	p.mu.Unlock()
	if err != nil {
		p.recordErr(fmt.Errorf("gpui: a11y wm_getobject: %w", err))
		return 0, false
	}
	// Sync the activation flag after the query (the native service
	// flips it during the call — the pin's activation closure). The
	// false-to-true edge forces a redraw so the next frame publishes
	// the complete tree over the initial one (window.rs 1617-1628).
	if _, activatedNow := p.syncActiveFlag(); activatedNow {
		p.window.requestRefresh()
	}
	if answer.Handled == 0 {
		return 0, false
	}
	return uintptr(answer.LResult), true
}

// ---------------------------------------------------------------------------
// Per-frame publication (window.rs 1161-1176 a11y_tree_update)
// ---------------------------------------------------------------------------

// publishA11yProviderNative sends one completed frame's snapshot to the
// native adapter with the pin's update_if_active semantics: when no
// assistive technology has connected the serialized update is never
// decoded (nothing applied — the Go side skips the serialization
// entirely, the pin's factory-not-called behavior); when active the
// update reaches the live tree and the native side raises the change
// events outside its lock. The publication runs on the message thread;
// the activation edge observed here forces the redraw.
func (st *windowA11yState) publishA11yProviderNative(w *Window, snapshot *A11ySnapshot) {
	p := st.provider
	if p == nil {
		return
	}
	p.mu.Lock()
	destroyed := p.destroyed
	p.mu.Unlock()
	if destroyed {
		// The adapter is retired (window closed or the explicit
		// teardown): no publication reaches it.
		return
	}
	h := p.app.Host()
	if h == nil {
		return
	}
	activatedHere := false
	ok := h.runForegroundSync(func() {
		// sync_active_flag at the frame boundary gates the platform
		// call (the pin's should_send_a11y_update).
		if _, activatedNow := p.syncActiveFlag(); activatedNow {
			activatedHere = true
		}
		if !p.isActiveFlag() {
			return
		}
		scale := a11ySerializationScale(w)
		update, err := serializeA11ySnapshot(snapshot, scale)
		if err != nil {
			p.recordErr(err)
			return
		}
		record, err := p.service.Publish(p.handle, update)
		if err != nil {
			p.recordErr(fmt.Errorf("gpui: a11y publish: %w", err))
			return
		}
		p.mu.Lock()
		p.publishes++
		p.lastRecord = record
		p.mu.Unlock()
	})
	if !ok {
		p.recordErr(fmt.Errorf("gpui: a11y publish: %w", ErrHostStopped))
		return
	}
	if activatedHere {
		// The activation edge observed at publication: force a frame so
		// the complete tree reaches the client (the pin's refresh).
		w.requestRefresh()
	}
	// Drain the queued actions on the foreground thread (the pin's
	// action channel task receiving requests as they arrive; the port's
	// per-frame drain).
	p.drainActions(w)
}

// a11ySerializationScale resolves the scale factor the snapshot's
// logical bounds convert to physical pixels with (element.rs 414-423:
// bounds x scale, f64). Real windows read the live lease; the draw
// state's scale is the fallback; 1 is the last resort.
func a11ySerializationScale(w *Window) float32 {
	if w == nil {
		return 1
	}
	if scale, err := w.handle.ScaleFactor(); err == nil && scale > 0 {
		return scale
	}
	if ds, ok := windowDrawStates[w]; ok && ds.scale > 0 {
		return ds.scale
	}
	return 1
}

// ---------------------------------------------------------------------------
// Serialization (the ticket-21 snapshot -> the native tree update)
// ---------------------------------------------------------------------------

// The native service's accepted bounds (reference/native/src/a11y.rs
// MAX_A11Y_*), enforced Go-side so a malformed publication fails fast.
const (
	a11yMaxSerializedNodes    = 1 << 16
	a11yMaxSerializedChildren = 1 << 20
	a11yMaxSerializedStrings  = 1 << 22
)

// a11yNodeRecordSize is the serialized node record size (validated at
// service fetch; reasserted here so a mismatch is a Go-side error).
var a11yNodeRecordSize = uint32(unsafe.Sizeof(native.A11yNodeRecord{}))

// a11yRoleTag maps the port's Role onto the native role tag. The
// published slice maps one-to-one (Window=1 ... Slider=8); anything
// else is an error, never a silent coercion.
func a11yRoleTag(role Role) (uint32, error) {
	switch role {
	case RoleWindow:
		return native.A11yRoleWindow, nil
	case RoleGenericContainer:
		return native.A11yRoleGenericContainer, nil
	case RoleButton:
		return native.A11yRoleButton, nil
	case RoleTextInput:
		return native.A11yRoleTextInput, nil
	case RoleTextRun:
		return native.A11yRoleTextRun, nil
	case RoleStaticText:
		return native.A11yRoleStaticText, nil
	case RoleCheckBox:
		return native.A11yRoleCheckBox, nil
	case RoleSlider:
		return native.A11yRoleSlider, nil
	default:
		return 0, fmt.Errorf("gpui: a11y role %s has no native tag", role)
	}
}

// a11yActionTag maps the port's A11yAction onto the native action tag
// (one-to-one, Click=1 ... ShowContextMenu=9).
func a11yActionTag(action A11yAction) (uint32, error) {
	switch action {
	case A11yActionClick:
		return native.A11yActionClick, nil
	case A11yActionFocus:
		return native.A11yActionFocus, nil
	case A11yActionBlur:
		return native.A11yActionBlur, nil
	case A11yActionIncrement:
		return native.A11yActionIncrement, nil
	case A11yActionDecrement:
		return native.A11yActionDecrement, nil
	case A11yActionSetValue:
		return native.A11yActionSetValue, nil
	case A11yActionReplaceSelectedText:
		return native.A11yActionReplaceSelectedText, nil
	case A11yActionSetTextSelection:
		return native.A11yActionSetTextSelection, nil
	case A11yActionShowMenu:
		return native.A11yActionShowContextMenu, nil
	default:
		return 0, fmt.Errorf("gpui: a11y action %s has no native tag", action)
	}
}

// a11yMaskBit encodes one action tag into the serialized actions mask
// under the FROZEN native decode (a11y.rs decode_update: the bit at
// index i is read as the action whose tag equals the bit's value, so a
// tag is representable exactly when it is a power of two >= 2 — the
// bit itself IS the tag). Non-representable tags (Click, Blur,
// Decrement, SetValue, ReplaceSelectedText, ShowContextMenu) return
// zero: the frozen decoder cannot advertise them at all, so the
// serializer omits them rather than fail the publication.
func a11yMaskBit(tag uint32) uint32 {
	switch tag {
	case 2, 4, 8: // Focus, Increment, SetTextSelection
		return tag
	default:
		return 0
	}
}

// a11yActionOfTag maps a native action tag back to the port's action.
func a11yActionOfTag(tag uint32) (A11yAction, error) {
	switch tag {
	case native.A11yActionClick:
		return A11yActionClick, nil
	case native.A11yActionFocus:
		return A11yActionFocus, nil
	case native.A11yActionBlur:
		return A11yActionBlur, nil
	case native.A11yActionIncrement:
		return A11yActionIncrement, nil
	case native.A11yActionDecrement:
		return A11yActionDecrement, nil
	case native.A11yActionSetValue:
		return A11yActionSetValue, nil
	case native.A11yActionReplaceSelectedText:
		return A11yActionReplaceSelectedText, nil
	case native.A11yActionSetTextSelection:
		return A11yActionSetTextSelection, nil
	case native.A11yActionShowContextMenu:
		return A11yActionShowMenu, nil
	default:
		return 0, fmt.Errorf("gpui: native a11y action tag %d is not a ported action", tag)
	}
}

// serializeA11ySnapshot converts one immutable published snapshot into
// the native serialized tree update: the node records (role, flags,
// actions mask, string offsets, physical-pixel bounds as f64 bits),
// the concatenated child-id array and the UTF-8 string pool. The
// snapshot is read-only here — no element, entity or builder state is
// touched, so a concurrent reader of the snapshot is safe and the
// native tree serves queries from the copy without waiting on a Go
// update.
func serializeA11ySnapshot(snapshot *A11ySnapshot, scale float32) (*native.A11yTreeUpdate, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("gpui: a11y publish: no snapshot")
	}
	nodes := snapshot.Nodes()
	if len(nodes) == 0 {
		return nil, fmt.Errorf("gpui: a11y publish: the snapshot has no nodes")
	}
	if len(nodes) > a11yMaxSerializedNodes {
		return nil, fmt.Errorf("gpui: a11y publish: %d nodes exceed the native maximum %d", len(nodes), a11yMaxSerializedNodes)
	}
	update := &native.A11yTreeUpdate{
		RootID:  uint64(snapshot.Root()),
		FocusID: uint64(snapshot.Focus()),
	}
	records := make([]native.A11yNodeRecord, 0, len(nodes))
	var children []uint64
	var pool []byte
	poolAppend := func(s string) (uint32, uint32) {
		if s == "" {
			return 0, 0
		}
		offset := uint32(len(pool))
		pool = append(pool, s...)
		return offset, uint32(len(s))
	}
	for _, node := range nodes {
		record := native.A11yNodeRecord{
			ID:         uint64(node.ID),
			RecordSize: a11yNodeRecordSize,
		}
		role, err := a11yRoleTag(node.Role)
		if err != nil {
			return nil, fmt.Errorf("gpui: a11y publish: node %s: %w", node.ID, err)
		}
		record.Role = role
		if node.Hidden {
			record.Flags |= native.A11yNodeFlagHidden
		}
		// write_a11y_info: label, description, value, author id.
		record.LabelOffset, record.LabelLen = poolAppend(node.Label)
		record.DescOffset, record.DescLen = poolAppend(node.Description)
		record.ValueOffset, record.ValueLen = poolAppend(node.Value)
		record.AuthorOff, record.AuthorLen = poolAppend(node.AuthorID)
		// push_child: the child-id array.
		if len(node.Children) > 0 {
			record.ChildrenOffset = uint32(len(children))
			record.ChildrenCount = uint32(len(node.Children))
			for _, child := range node.Children {
				children = append(children, uint64(child))
			}
		}
		// Bounds in physical pixels (element.rs 414-423: logical x
		// scale, f64). A zero-extent node omits the bounds.
		if node.Bounds.Size.Width > 0 || node.Bounds.Size.Height > 0 {
			record.Flags |= native.A11yNodeFlagHasBounds
			record.BoundsX0Bits = math.Float64bits(float64(node.Bounds.Origin.X * scale))
			record.BoundsY0Bits = math.Float64bits(float64(node.Bounds.Origin.Y * scale))
			record.BoundsX1Bits = math.Float64bits(float64((node.Bounds.Origin.X + node.Bounds.Size.Width) * scale))
			record.BoundsY1Bits = math.Float64bits(float64((node.Bounds.Origin.Y + node.Bounds.Size.Height) * scale))
		}
		// The advertised actions (div.rs 4153-4161). The FROZEN native
		// decode reads the mask as: the bit at index i adds the action
		// whose tag equals the bit's VALUE (1<<i is passed to
		// action_from_tag), so only the power-of-two tags are
		// representable (Focus=2, Increment=4, SetTextSelection=8); a bit
		// at index 0 or >= 4 fails the whole publish. The serializer
		// encodes exactly those bits — mask |= tag for representable
		// tags — and skips the non-representable ones (recorded in the
		// file header's deviation list; the REQUEST path routes all nine
		// actions).
		var mask uint32
		for _, action := range node.Actions {
			tag, err := a11yActionTag(action)
			if err != nil {
				return nil, fmt.Errorf("gpui: a11y publish: node %s: %w", node.ID, err)
			}
			mask |= a11yMaskBit(tag)
		}
		if node.FocusHandleID != 0 {
			mask |= a11yMaskBit(native.A11yActionFocus)
		}
		record.ActionsMask = mask
		records = append(records, record)
	}
	if len(children) > a11yMaxSerializedChildren {
		return nil, fmt.Errorf("gpui: a11y publish: %d children exceed the native maximum %d", len(children), a11yMaxSerializedChildren)
	}
	if len(pool) > a11yMaxSerializedStrings {
		return nil, fmt.Errorf("gpui: a11y publish: %d string bytes exceed the native maximum %d", len(pool), a11yMaxSerializedStrings)
	}
	update.Nodes = records
	update.Children = children
	update.Strings = pool
	return update, nil
}

// ---------------------------------------------------------------------------
// The queued-action drain (window.rs 7130-7200 handle_a11y_action)
// ---------------------------------------------------------------------------

// drainActions polls the native action queue on the message thread and
// routes every drained request through the generation-checked
// foreground dispatch (listeners first, then the built-ins). Stale
// requests (closed runtime, removed or re-targeted nodes) are recorded
// as provider diagnostics, never dispatched into application state.
func (p *a11yProvider) drainActions(w *Window) int {
	p.mu.Lock()
	destroyed := p.destroyed
	p.mu.Unlock()
	if destroyed {
		return 0
	}
	h := p.app.Host()
	if h == nil {
		return 0
	}
	routed := 0
	ok := h.runForegroundSync(func() {
		for round := 0; round < 32; round++ {
			pending, err := p.service.PendingActions(p.handle)
			if err != nil {
				p.recordErr(fmt.Errorf("gpui: a11y pending actions: %w", err))
				return
			}
			if pending == 0 {
				return
			}
			capacity := pending
			if capacity > 64 {
				capacity = 64
			}
			records := make([]native.A11yActionRecord, capacity)
			stringsBuf := make([]byte, 1024)
			requests, count, needed, err := p.service.PollActions(p.handle, records, stringsBuf)
			if errors.Is(err, native.ErrA11yCapacity) && needed > 0 {
				// The capacity protocol: nothing was drained; retry with
				// the reported string need.
				stringsBuf = make([]byte, needed)
				requests, count, _, err = p.service.PollActions(p.handle, records, stringsBuf)
			}
			if err != nil {
				p.recordErr(fmt.Errorf("gpui: a11y poll actions: %w", err))
				return
			}
			for _, request := range requests {
				routed++
				if err := routeA11yActionRequest(p.app, w, request); err != nil {
					// Stale or malformed requests are diagnostics: the
					// queue keeps draining (the pin's foreground task
					// logs unhandled actions).
					p.recordErr(fmt.Errorf("gpui: a11y action %s on %d: %w", a11yRequestActionName(request.Action), request.TargetNode, err))
				}
			}
			if count == 0 || count < uint32(capacity) {
				return
			}
		}
	})
	if !ok {
		p.recordErr(fmt.Errorf("gpui: a11y poll actions: %w", ErrHostStopped))
	}
	p.mu.Lock()
	p.actionsRouted += uint64(routed)
	p.mu.Unlock()
	return routed
}

// a11yRequestActionName renders a native action tag for diagnostics.
func a11yRequestActionName(tag uint32) string {
	if action, err := a11yActionOfTag(tag); err == nil {
		return action.String()
	}
	return fmt.Sprintf("Action(%d)", tag)
}

// routeA11yActionRequest routes one drained native action request
// through the foreground update boundary into the ticket-21
// generation-checked dispatch (the pin's handle_a11y_action).
func routeA11yActionRequest(app *App, w *Window, request native.A11yActionRequest) error {
	action, err := a11yActionOfTag(request.Action)
	if err != nil {
		return err
	}
	var data *A11yActionData
	if request.Value != "" {
		data = &A11yActionData{Value: request.Value}
	}
	if action == A11yActionSetTextSelection {
		selection := &A11yTextSelection{
			AnchorNode: A11yNodeID(request.SelectionAnchorNode),
			AnchorChar: request.SelectionAnchorChar,
			FocusNode:  A11yNodeID(request.SelectionFocusNode),
			FocusChar:  request.SelectionFocusChar,
		}
		if data == nil {
			data = &A11yActionData{}
		}
		data.TextSelection = selection
	}
	var result error
	app.Update(func(app *App) {
		result = dispatchWindowA11yRequest(app, w, A11yNodeID(request.TargetNode), action, data)
	})
	return result
}

// dispatchWindowA11yRequest is the drain path's foreground body: mint
// the generation-checked token from the CURRENT published snapshot,
// resolve it against the live node identity, then dispatch through the
// ticket-21 listener/built-in machinery. A request whose node was
// removed (or whose window closed) is rejected before any application
// state is touched.
func dispatchWindowA11yRequest(app *App, w *Window, node A11yNodeID, action A11yAction, data *A11yActionData) error {
	st := windowA11yStateOf(w)
	if st == nil || !st.active || st.published == nil {
		return ErrA11yWindowGone
	}
	if !w.Alive() {
		// The OS window is gone (the pin's window drop): no request runs
		// against the dead lease, no listener runs.
		return ErrA11yWindowGone
	}
	if st.provider != nil {
		st.provider.mu.Lock()
		destroyed := st.provider.destroyed
		st.provider.mu.Unlock()
		if destroyed {
			return ErrA11yWindowGone
		}
	}
	token, err := st.published.Token(node)
	if err != nil {
		// The node is not in the current published snapshot: it was
		// removed (or never published) — the typed removed error.
		return ErrA11yNodeRemoved
	}
	resolved, err := ResolveA11yToken(app, token)
	if err != nil {
		return err
	}
	return dispatchA11yActionForeground(app, resolved, action, data)
}

// ---------------------------------------------------------------------------
// Public surface
// ---------------------------------------------------------------------------

// A11yTextSelection is the SetTextSelection action payload (the
// accesskit ActionData::SetTextSelection the native provider decodes).
type A11yTextSelection struct {
	// AnchorNode is the selection anchor's node.
	AnchorNode A11yNodeID
	// AnchorChar is the anchor's character index.
	AnchorChar uint32
	// FocusNode is the selection focus's node.
	FocusNode A11yNodeID
	// FocusChar is the focus's character index.
	FocusChar uint32
}

// A11yProviderRecord is the observable provider diagnostics of one
// window (the a11y half of the runtime's state; the ticket-21 snapshot
// accessors carry the tree).
type A11yProviderRecord struct {
	// Available reports whether the native provider exists.
	Available bool
	// Hwnd is the provider's window handle.
	Hwnd uintptr
	// Title is the initial-tree label baked at adapter creation.
	Title string
	// NativeHandle is the native adapter handle value (diagnostics;
	// stale after the provider is destroyed).
	NativeHandle uint64
	// Active is the last observed native is_active flag.
	Active bool
	// Activated reports the activation edge observed (a WM_GETOBJECT
	// was routed to the adapter).
	Activated bool
	// WMGetObjects counts the routed WM_GETOBJECT queries.
	WMGetObjects uint64
	// Publishes counts the native tree publications made.
	Publishes uint64
	// LastActive/LastApplied/LastEventsRaised mirror the last
	// publication's native record (the activation record).
	LastActive       bool
	LastApplied      bool
	LastEventsRaised bool
	// ActionsRouted counts the dispatched native action requests.
	ActionsRouted uint64
	// Destroyed reports the adapter destroyed (window close or the
	// explicit a11y teardown).
	Destroyed bool
	// Err is the last provider error, if any (rendered).
	Err string
}

// A11yProviderState returns the window's native provider diagnostics.
// The second return reports whether the window's accessibility runtime
// exists at all (activated, even without a provider).
func A11yProviderState(w *Window) (A11yProviderRecord, bool) {
	if w == nil {
		return A11yProviderRecord{}, false
	}
	st := windowA11yStateOf(w)
	if st == nil {
		return A11yProviderRecord{}, false
	}
	record := A11yProviderRecord{}
	if st.providerErr != nil {
		record.Err = st.providerErr.Error()
	}
	if p := st.provider; p != nil {
		p.mu.Lock()
		record.Available = true
		record.Hwnd = p.hwnd
		record.Title = p.title
		record.NativeHandle = uint64(p.handle)
		record.Active = p.active
		record.Activated = p.activated
		record.WMGetObjects = p.wmGetObjects
		record.Publishes = p.publishes
		record.LastActive = p.lastRecord.Active != 0
		record.LastApplied = p.lastRecord.Applied != 0
		record.LastEventsRaised = p.lastRecord.EventsRaised != 0
		record.ActionsRouted = p.actionsRouted
		record.Destroyed = p.destroyed
		if p.lastErr != nil {
			record.Err = p.lastErr.Error()
		}
		p.mu.Unlock()
	}
	return record, true
}

// PollWindowA11yActions drains the window's queued native action
// requests and routes each through the foreground dispatch (listeners
// first, then the built-ins). It is the on-demand drain behind the
// per-frame publication drain; the routed count is returned. A window
// without a provider (test window, unactivated runtime, failed
// provider) reports ErrA11yNoProvider.
func PollWindowA11yActions(w *Window) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("gpui: PollWindowA11yActions requires a window")
	}
	st := windowA11yStateOf(w)
	if st == nil {
		return 0, ErrA11yWindowGone
	}
	if st.provider == nil {
		if st.providerErr != nil {
			return 0, st.providerErr
		}
		return 0, ErrA11yNoProvider
	}
	st.provider.mu.Lock()
	destroyed := st.provider.destroyed
	st.provider.mu.Unlock()
	if destroyed {
		return 0, ErrA11yNoProvider
	}
	return st.provider.drainActions(w), nil
}

// DispatchWindowA11yRequest routes ONE synthetic action request through
// the exact path the native drain uses (the tag mapping, the payload
// decode, the generation-checked foreground dispatch). It is the
// deterministic drive entry for tests and app-level simulation: the
// request does NOT pass through the native queue (the queue is fed by
// real UIA invocations).
func DispatchWindowA11yRequest(w *Window, target A11yNodeID, action A11yAction, data *A11yActionData) error {
	if w == nil {
		return fmt.Errorf("gpui: DispatchWindowA11yRequest requires a window")
	}
	tag, err := a11yActionTag(action)
	if err != nil {
		return err
	}
	request := native.A11yActionRequest{
		TargetNode: uint64(target),
		Action:     tag,
	}
	if data != nil {
		request.Value = data.Value
		if data.TextSelection != nil {
			request.Action = native.A11yActionSetTextSelection
			request.SelectionAnchorNode = uint64(data.TextSelection.AnchorNode)
			request.SelectionAnchorChar = data.TextSelection.AnchorChar
			request.SelectionFocusNode = uint64(data.TextSelection.FocusNode)
			request.SelectionFocusChar = data.TextSelection.FocusChar
		}
	}
	return routeA11yActionRequest(w.app, w, request)
}
