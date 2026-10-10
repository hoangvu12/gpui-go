//go:build windows

package gpui

// Ticket24's Direct Manipulation integration, ported from the pinned
// CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a,
// crates/gpui_windows/src/direct_manipulation.rs (the whole module):
//
//   - DirectManipulationHandler::new — CoCreateInstance the manager,
//     GetUpdateManager, CreateViewport, ActivateConfiguration
//     (interaction + translation X/Y + inertia + rails + scaling),
//     SetViewportOptions (manual update, pixel snapping disabled),
//     SetViewportRect (the fixed 1000x1000 recognition viewport),
//     manager.Activate, viewport.Enable, AddEventHandler and the
//     initial Update.
//   - DirectManipulationHandler::set_scale_factor /
//     on_pointer_hit_test / update / drain_events — the shared scale
//     cell and the pending-event queue the gesture callbacks fill.
//   - Drop — viewport.Stop, viewport.Abandon, manager.Deactivate.
//   - DirectManipulationEventHandler (#[implement(
//     IDirectManipulationViewportEventHandler)]) — the gesture state
//     (gesture_kind, last_scale, last_x/y_offset, scroll_phase), the
//     mouse-position/modifier readout (GetCursorPos -> ScreenToClient
//     -> logical_point, current_modifiers), end_gesture's Ended
//     events, OnViewportStatusChanged's inertia-interrupt and
//     READY-cycle reset (ZoomToRect plus the infinite-loop guard), and
//     OnContentUpdated's scroll-versus-pinch classification with the
//     float_equals epsilon.
//   - crates/gpui_windows/src/events.rs — DM_POINTERHITTEST ->
//     on_pointer_hit_test (GetPointerType == PT_TOUCHPAD ->
//     SetContact), the draw_window drain (direct_manipulation.update
//     + drain_events through the input callback), and
//     handle_dpi_changed_msg's set_scale_factor.
//
// The Go COM implementation follows dragdrop_windows.go's registry
// pattern (once-per-process trampolines, the self-value registry, the
// generation-checked window resolution).
//
// HONEST DEVICE-AVAILABILITY NOTE: a simulated gesture (the fake
// IDirectManipulationContent/viewport objects and the direct vtable
// calls the tests use) validates the gesture-state machine and event
// DISPATCH only — it does not prove interoperability with a physical
// precision touchpad, whose pointer input Windows alone generates.
// The real-handler smoke test creates the real COM manager, viewport
// and event-handler registration against the OS and exercises
// Update/Stop/Abandon/Deactivate; gesture content itself requires the
// device. Host.PrecisionTouchpadAvailability() records the machine's
// ACTUAL precision-touchpad presence through GetPointerDevices.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Win32 bindings and constants (ticket24's own set)
// ---------------------------------------------------------------------------

var (
	procGetPointerType    = modUser32.NewProc("GetPointerType")
	procGetPointerDevices = modUser32.NewProc("GetPointerDevices")
)

// Direct Manipulation constants (directmanipulation.h, winuser.h).
const (
	dmPointerHitTest = 0x0250 // DM_POINTERHITTEST
	ptTouchpad       = 5      // PT_TOUCHPAD

	// DIRECTMANIPULATION_STATUS.
	dmStatusBuilding  = 0
	dmStatusEnabled   = 1
	dmStatusDisabled  = 2
	dmStatusRunning   = 3
	dmStatusInertia   = 4
	dmStatusReady     = 5
	dmStatusSuspended = 6

	// DIRECTMANIPULATION_CONFIGURATION (the pin's exact OR).
	dmConfigInteraction        = 0x00000001
	dmConfigTranslationX       = 0x00000002
	dmConfigTranslationY       = 0x00000004
	dmConfigScaling            = 0x00000010
	dmConfigTranslationInertia = 0x00000020
	dmConfigRailsX             = 0x00000100
	dmConfigRailsY             = 0x00000200

	// DIRECTMANIPULATION_VIEWPORT_OPTIONS (the pin's exact OR).
	dmViewportOptionsManualUpdate         = 0x00000002
	dmViewportOptionsDisablePixelSnapping = 0x00000010

	// dmDefaultViewportSize is the pin's DEFAULT_VIEWPORT_SIZE: the
	// actual content size doesn't matter because the viewport is only
	// used for gesture recognition, not visual output.
	dmDefaultViewportSize = int32(1000)
)

// Direct Manipulation GUIDs (directmanipulation.h, verified against
// the installed SDK 10.0.26100.0).
var (
	clsidDirectManipulationManager             = guid{0x54E211B6, 0x3650, 0x4F75, [8]byte{0x83, 0x34, 0xFA, 0x35, 0x95, 0x98, 0xE1, 0xC5}}
	iidIDirectManipulationManager              = guid{0xFBF5D3B4, 0x70C7, 0x4163, [8]byte{0x93, 0x22, 0x5A, 0x6F, 0x66, 0x0D, 0x6F, 0xBC}}
	iidIDirectManipulationViewport             = guid{0x28B85A3D, 0x60A0, 0x48BD, [8]byte{0x9B, 0xA1, 0x5C, 0xE8, 0xD9, 0xEA, 0x3A, 0x6D}}
	iidIDirectManipulationUpdateManager        = guid{0xB0AE62FD, 0xBE34, 0x46E7, [8]byte{0x9C, 0xAA, 0xD3, 0x61, 0xFA, 0xCB, 0xB9, 0xCC}}
	iidIDirectManipulationViewportEventHandler = guid{0x952121DA, 0xD69F, 0x45F9, [8]byte{0xB0, 0xF9, 0xF2, 0x39, 0x44, 0x32, 0x1A, 0x6D}}
	iidIDirectManipulationContent              = guid{0xB89962CB, 0x3D89, 0x442B, [8]byte{0xBB, 0x58, 0x50, 0x98, 0xFA, 0x0F, 0x9F, 0x16}}
)

// ---------------------------------------------------------------------------
// The shared gesture state (the pin's Rc<Cell>/Rc<RefCell> sharing)
// ---------------------------------------------------------------------------

// dmGestureKind is the pin's GestureKind.
type dmGestureKind uint8

const (
	dmGestureNone dmGestureKind = iota
	dmGestureScroll
	dmGesturePinch
)

// dmSharedState is the state the handler and its event handler share
// (the pin's Rc<Cell<f32>> scale factor and Rc<RefCell<Vec>>
// pending events). Guarded by a mutex: Direct Manipulation can
// deliver events during SetContact/Update on the host thread, and a
// defensive lock keeps that correct regardless of the delivery
// thread.
type dmSharedState struct {
	mu sync.Mutex
	// scale is the current window scale factor (set_scale_factor).
	scale float32
	// pending are the gesture PlatformInputs awaiting the next drain.
	pending []any
}

// dmGestureTracking is the event handler's per-gesture tracking (the
// pin's Cells on DirectManipulationEventHandler).
type dmGestureTracking struct {
	// gestureKind classifies the active gesture.
	gestureKind dmGestureKind
	// lastScale, lastXOffset, lastYOffset are the last content
	// transform facts.
	lastScale   float32
	lastXOffset float32
	lastYOffset float32
	// scrollPhase is the scroll gesture's next phase.
	scrollPhase TouchPhase
}

// ---------------------------------------------------------------------------
// The event handler COM object (IDirectManipulationViewportEventHandler)
// ---------------------------------------------------------------------------

// dmEventHandlerObj is the Go object implementing
// IDirectManipulationViewportEventHandler for one viewport
// (directmanipulation.rs DirectManipulationEventHandler).
type dmEventHandlerObj struct {
	vtbl *dmEventHandlerVtable

	host     *Host
	hwnd     uintptr
	windowID uint64
	refs     int32
	// shared is the handler's shared state (scale + pending events).
	shared *dmSharedState
	// tracking is the gesture classification state (host-thread
	// discipline: DM delivers on the STA; the mutex in shared covers
	// the queue, the tracking is only touched from the callbacks).
	tracking dmGestureTracking
}

// dmEventHandlerVtable is the IDirectManipulationViewportEventHandler
// vtable (directmanipulation.h: QueryInterface, AddRef, Release,
// OnViewportStatusChanged, OnViewportUpdated, OnContentUpdated).
type dmEventHandlerVtable struct {
	queryInterface          uintptr
	addRef                  uintptr
	release                 uintptr
	onViewportStatusChanged uintptr
	onViewportUpdated       uintptr
	onContentUpdated        uintptr
}

var (
	dmEventHandlerVtableOnce sync.Once
	dmEventHandlerVtableVal  dmEventHandlerVtable
)

func dmEventHandlerVtableGet() *dmEventHandlerVtable {
	dmEventHandlerVtableOnce.Do(func() {
		dmEventHandlerVtableVal = dmEventHandlerVtable{
			queryInterface:          syscall.NewCallback(dmEventHandlerQueryInterface),
			addRef:                  syscall.NewCallback(dmEventHandlerAddRef),
			release:                 syscall.NewCallback(dmEventHandlerRelease),
			onViewportStatusChanged: syscall.NewCallback(dmOnViewportStatusChanged),
			onViewportUpdated:       syscall.NewCallback(dmOnViewportUpdated),
			onContentUpdated:        syscall.NewCallback(dmOnContentUpdated),
		}
	})
	return &dmEventHandlerVtableVal
}

// liveWindow resolves the owning window generation-checked.
func (o *dmEventHandlerObj) liveWindow() *hostWindow {
	w := windowFor(o.hwnd)
	if w == nil || w.host != o.host || w.id != o.windowID {
		return nil
	}
	return w
}

// mousePosition is the pin's mouse_position: the cursor in
// window-local logical pixels (GetCursorPos -> ScreenToClient ->
// logical_point with the shared scale factor).
func (o *dmEventHandlerObj) mousePosition() Point {
	o.shared.mu.Lock()
	scale := o.shared.scale
	o.shared.mu.Unlock()
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	clientX, clientY, _ := screenToClientPoint(o.hwnd, pt.x, pt.y)
	return Point{X: float32(clientX) / scale, Y: float32(clientY) / scale}
}

// push queues one gesture event (pending_events.borrow_mut().push).
func (o *dmEventHandlerObj) push(event any) {
	o.shared.mu.Lock()
	o.shared.pending = append(o.shared.pending, event)
	o.shared.mu.Unlock()
}

// endGesture is the pin's end_gesture: emit the Ended event for the
// active gesture kind and reset the classification.
func (o *dmEventHandlerObj) endGesture() {
	position := o.mousePosition()
	modifiers := win32CurrentModifiers()
	switch o.tracking.gestureKind {
	case dmGestureScroll:
		o.push(&ScrollWheelEvent{
			Position:   position,
			Delta:      ScrollDeltaPixels(0, 0),
			Modifiers:  modifiers,
			TouchPhase: TouchPhaseEnded,
		})
	case dmGesturePinch:
		o.push(&PinchEvent{
			Position:  position,
			Delta:     0,
			Modifiers: modifiers,
			Phase:     TouchPhaseEnded,
		})
	case dmGestureNone:
	}
	o.tracking.gestureKind = dmGestureNone
}

// dmOnViewportStatusChanged is OnViewportStatusChanged
// (directmanipulation.rs): a RUNNING-after-INERTIA status ends the
// interrupted gesture; READY ends the gesture and resets the content
// transform through ZoomToRect (guarded against the second
// RUNNING->READY cycle it triggers).
func dmOnViewportStatusChanged(self uintptr, viewport unsafe.Pointer, current, previous uint32) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	handler, _ := comResolve(self).(*dmEventHandlerObj)
	if handler == nil {
		return eUnexpected
	}
	if current == previous {
		return sOK
	}

	// A new gesture interrupted inertia, so end the old sequence.
	if current == dmStatusRunning && previous == dmStatusInertia {
		handler.endGesture()
	}

	if current == dmStatusReady {
		handler.endGesture()

		// Reset the content transform so the viewport is ready for the
		// next gesture. ZoomToRect triggers a second RUNNING -> READY
		// cycle, so prevent an infinite loop here.
		if handler.tracking.lastScale != 1.0 ||
			handler.tracking.lastXOffset != 0.0 ||
			handler.tracking.lastYOffset != 0.0 {
			if viewport != nil {
				vp := (*comIFace)(viewport)
				syscall.SyscallN(vp.vtbl[13], /* ZoomToRect */
					uintptr(unsafe.Pointer(vp)),
					0, 0, uintptr(dmDefaultViewportSize), uintptr(dmDefaultViewportSize), 0)
			}
		}

		handler.tracking.lastScale = 1.0
		handler.tracking.lastXOffset = 0.0
		handler.tracking.lastYOffset = 0.0
	}

	return sOK
}

// dmOnViewportUpdated is OnViewportUpdated: the pin's no-op.
func dmOnViewportUpdated(self uintptr, viewport unsafe.Pointer) (result uintptr) {
	return sOK
}

// dmOnContentUpdated is OnContentUpdated (direct_manipulation.rs):
// read the content transform, classify scroll-versus-pinch (Scroll ->
// Pinch allowed, not the reverse), and emit the pixel-delta scroll or
// the pinch-zoom events with the mouse position and modifiers.
func dmOnContentUpdated(self uintptr, viewport, content unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	handler, _ := comResolve(self).(*dmEventHandlerObj)
	if handler == nil || content == nil {
		return ePointer
	}
	contentIFace := (*comIFace)(content)

	// Get the 6-element content transform: [scale, 0, 0, scale, tx, ty].
	var xform [6]float32
	hr, _, _ := syscall.SyscallN(contentIFace.vtbl[9], /* GetContentTransform */
		uintptr(unsafe.Pointer(contentIFace)), uintptr(unsafe.Pointer(&xform[0])))
	if int32(hr) < 0 {
		return hr
	}

	scale := xform[0]
	handler.shared.mu.Lock()
	scaleFactor := handler.shared.scale
	handler.shared.mu.Unlock()
	xOffset := xform[4] / scaleFactor
	yOffset := xform[5] / scaleFactor

	if scale == 0.0 {
		return sOK
	}

	lastScale := handler.tracking.lastScale
	lastX := handler.tracking.lastXOffset
	lastY := handler.tracking.lastYOffset

	if dmFloatEquals(scale, lastScale) && dmFloatEquals(xOffset, lastX) && dmFloatEquals(yOffset, lastY) {
		return sOK
	}

	position := handler.mousePosition()
	modifiers := win32CurrentModifiers()

	// Direct Manipulation reports both translation and scale in every
	// content update. Translation values can shift during a pinch due
	// to the zoom center shifting. We classify each gesture as either
	// scroll or pinch and only emit one type of event. We allow Scroll
	// -> Pinch (a pinch can start with a small pan) but not the
	// reverse.
	if !dmFloatEquals(scale, 1.0) {
		if handler.tracking.gestureKind != dmGesturePinch {
			handler.endGesture()
			handler.tracking.gestureKind = dmGesturePinch
			handler.push(&PinchEvent{
				Position:  position,
				Delta:     0,
				Modifiers: modifiers,
				Phase:     TouchPhaseStarted,
			})
		}
	} else if handler.tracking.gestureKind == dmGestureNone {
		handler.tracking.gestureKind = dmGestureScroll
		handler.tracking.scrollPhase = TouchPhaseStarted
	}

	switch handler.tracking.gestureKind {
	case dmGestureScroll:
		dx := xOffset - lastX
		dy := yOffset - lastY
		touchPhase := handler.tracking.scrollPhase
		handler.tracking.scrollPhase = TouchPhaseMoved
		handler.push(&ScrollWheelEvent{
			Position:   position,
			Delta:      ScrollDeltaPixels(dx, dy),
			Modifiers:  modifiers,
			TouchPhase: touchPhase,
		})
	case dmGesturePinch:
		scaleDelta := scale / lastScale
		handler.push(&PinchEvent{
			Position:  position,
			Delta:     scaleDelta - 1.0,
			Modifiers: modifiers,
			Phase:     TouchPhaseMoved,
		})
	case dmGestureNone:
	}

	handler.tracking.lastScale = scale
	handler.tracking.lastXOffset = xOffset
	handler.tracking.lastYOffset = yOffset

	return sOK
}

// dmFloatEquals is the pin's float_equals: the difference must stay
// under EPSILON_SCALE * max(|f1|, |f2|, EPSILON_SCALE).
func dmFloatEquals(f1, f2 float32) bool {
	const epsilonScale = 0.00001
	return abs32(f1-f2) < epsilonScale*dmMaxOf(abs32(f1), abs32(f2), epsilonScale)
}

// dmMaxOf of three values (max32 in paint.go is two-argument).
func dmMaxOf(a, b, c float32) float32 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}

// The IUnknown trampolines for the event handler.
func dmEventHandlerQueryInterface(self uintptr, riid, ppv unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmEventHandlerObj)
	if obj == nil || ppv == nil {
		return ePointer
	}
	out := (*uintptr)(ppv)
	*out = 0
	iid := (*guid)(riid)
	if *iid == iidIUnknown || *iid == iidIDirectManipulationViewportEventHandler {
		*out = self
		atomic.AddInt32(&obj.refs, 1)
		return sOK
	}
	return eNoInterface
}

func dmEventHandlerAddRef(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmEventHandlerObj)
	if obj == nil {
		return 0
	}
	return uintptr(uint32(atomic.AddInt32(&obj.refs, 1)))
}

func dmEventHandlerRelease(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmEventHandlerObj)
	if obj == nil {
		return 0
	}
	refs := atomic.AddInt32(&obj.refs, -1)
	if refs <= 0 {
		comUnregister(self)
		return 0
	}
	return uintptr(uint32(refs))
}

// ---------------------------------------------------------------------------
// The handler (DirectManipulationHandler)
// ---------------------------------------------------------------------------

// directManipulationHandler is the port of DirectManipulationHandler:
// the per-window manager/viewport wiring, the shared scale/events
// state and the lifecycle (Drop = Stop/Abandon/Deactivate).
type directManipulationHandler struct {
	manager       *comIFace // IDirectManipulationManager
	updateManager *comIFace // IDirectManipulationUpdateManager
	viewport      *comIFace // IDirectManipulationViewport
	handlerCookie uint32
	window        uintptr

	// eventHandler is the registered COM event handler object (the
	// Go allocation the registry keeps alive while the viewport holds
	// the COM reference).
	eventHandler *dmEventHandlerObj
	// shared is the state the handler and event handler share.
	shared *dmSharedState
}

// newDirectManipulationHandler is DirectManipulationHandler::new. Runs
// on the host (STA) thread; every COM call must succeed or creation
// fails (the pin propagates with `?`).
func newDirectManipulationHandler(w *hostWindow) (*directManipulationHandler, error) {
	var manager *comIFace
	hr, _, callErr := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidDirectManipulationManager)),
		0,
		uintptr(1), // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIDirectManipulationManager)),
		uintptr(unsafe.Pointer(&manager)),
	)
	if int32(hr) < 0 || manager == nil {
		return nil, dmCreationError("CoCreateInstance(DirectManipulationManager)", hr, callErr)
	}

	// manager.GetUpdateManager (slot 7).
	var updateManager *comIFace
	if hr, err := comCall(manager, 7, uintptr(unsafe.Pointer(&iidIDirectManipulationUpdateManager)),
		uintptr(unsafe.Pointer(&updateManager))); err != nil {
		return nil, dmWrapError("GetUpdateManager", hr, err)
	}

	// manager.CreateViewport(None, window) (slot 8).
	var viewport *comIFace
	if hr, err := comCall(manager, 8, 0, w.hwnd,
		uintptr(unsafe.Pointer(&iidIDirectManipulationViewport)),
		uintptr(unsafe.Pointer(&viewport))); err != nil {
		return nil, dmWrapError("CreateViewport", hr, err)
	}

	// viewport.ActivateConfiguration (slot 22).
	configuration := uintptr(dmConfigInteraction | dmConfigTranslationX | dmConfigTranslationY |
		dmConfigTranslationInertia | dmConfigRailsX | dmConfigRailsY | dmConfigScaling)
	if hr, err := comCall(viewport, 22, configuration); err != nil {
		return nil, dmWrapError("ActivateConfiguration", hr, err)
	}

	// viewport.SetViewportOptions (slot 19).
	if hr, err := comCall(viewport, 19, uintptr(dmViewportOptionsManualUpdate|dmViewportOptionsDisablePixelSnapping)); err != nil {
		return nil, dmWrapError("SetViewportOptions", hr, err)
	}

	// viewport.SetViewportRect (slot 12).
	rect := rect{left: 0, top: 0, right: dmDefaultViewportSize, bottom: dmDefaultViewportSize}
	if hr, err := comCall(viewport, 12, uintptr(unsafe.Pointer(&rect))); err != nil {
		return nil, dmWrapError("SetViewportRect", hr, err)
	}

	// manager.Activate(window) (slot 3).
	if hr, err := comCall(manager, 3, w.hwnd); err != nil {
		return nil, dmWrapError("Activate", hr, err)
	}

	// viewport.Enable (slot 3).
	if hr, err := comCall(viewport, 3); err != nil {
		return nil, dmWrapError("Enable", hr, err)
	}

	scaleFactor := w.scale
	shared := &dmSharedState{scale: scaleFactor}

	eventHandler := &dmEventHandlerObj{
		vtbl:     dmEventHandlerVtableGet(),
		host:     w.host,
		hwnd:     w.hwnd,
		windowID: w.id,
		shared:   shared,
		tracking: dmGestureTracking{lastScale: 1.0},
	}
	comRegister(comSelf(eventHandler), eventHandler)

	// viewport.AddEventHandler(Some(window), handler) (slot 25).
	var cookie uint32
	if hr, err := comCall(viewport, 25, w.hwnd, comSelf(eventHandler),
		uintptr(unsafe.Pointer(&cookie))); err != nil {
		comUnregister(comSelf(eventHandler))
		return nil, dmWrapError("AddEventHandler", hr, err)
	}

	handler := &directManipulationHandler{
		manager:       manager,
		updateManager: updateManager,
		viewport:      viewport,
		handlerCookie: cookie,
		window:        w.hwnd,
		eventHandler:  eventHandler,
		shared:        shared,
	}

	// update_manager.Update(None) (slot 3).
	handler.update()
	return handler, nil
}

// setScaleFactor records a new window scale factor (WM_DPICHANGED,
// events.rs handle_dpi_changed_msg).
func (h *directManipulationHandler) setScaleFactor(scale float32) {
	h.shared.mu.Lock()
	h.shared.scale = scale
	h.shared.mu.Unlock()
}

// onPointerHitTest is DM_POINTERHITTEST (events.rs
// handle_dm_pointer_hit_test): a touchpad pointer claims contact with
// the viewport (SetContact, viewport slot 5).
func (h *directManipulationHandler) onPointerHitTest(wparam uintptr) {
	pointerID := uint32(loword(wparam))
	var pointerType uint32
	hr, _, _ := procGetPointerType.Call(uintptr(pointerID), uintptr(unsafe.Pointer(&pointerType)))
	if hrOK(hr) && pointerType == ptTouchpad {
		if _, err := comCall(h.viewport, 5 /* SetContact */, uintptr(pointerID)); err != nil {
			h.eventHandler.host.recordFault(fmt.Sprintf("DirectManipulation SetContact failed: %v", err))
		}
	}
}

// update drives the update manager (update_manager.Update(None)).
func (h *directManipulationHandler) update() {
	if _, err := comCall(h.updateManager, 3, 0); err != nil {
		h.eventHandler.host.recordFault(fmt.Sprintf("DirectManipulation Update failed: %v", err))
	}
}

// drainEvents takes the pending gesture events (drain_events; the
// std::mem::take).
func (h *directManipulationHandler) drainEvents() []any {
	h.shared.mu.Lock()
	events := h.shared.pending
	h.shared.pending = nil
	h.shared.mu.Unlock()
	return events
}

// deactivate is the pin's Drop: viewport.Stop, viewport.Abandon,
// manager.Deactivate(window), then the interface releases. Runs on
// the host (STA) thread.
func (h *directManipulationHandler) deactivate() {
	if _, err := comCall(h.viewport, 29 /* Stop */); err != nil {
		h.eventHandler.host.recordFault(fmt.Sprintf("DirectManipulation Stop failed: %v", err))
	}
	if _, err := comCall(h.viewport, 30 /* Abandon */); err != nil {
		h.eventHandler.host.recordFault(fmt.Sprintf("DirectManipulation Abandon failed: %v", err))
	}
	if _, err := comCall(h.manager, 4 /* Deactivate */, h.window); err != nil {
		h.eventHandler.host.recordFault(fmt.Sprintf("DirectManipulation Deactivate failed: %v", err))
	}
	// Release the COM interfaces on the owning apartment (the pin's
	// Drop drops them here).
	comRelease(h.viewport)
	comRelease(h.updateManager)
	comRelease(h.manager)
	h.viewport, h.updateManager, h.manager = nil, nil, nil
	// The event handler object stays valid for any references the
	// viewport still holds; the registry entry drops at refcount zero
	// (its Release ran through the viewport's RemoveEventHandler-less
	// Abandon path — Abandon releases the handler references).
}

// dmCreationError / dmWrapError build the creation errors.
func dmCreationError(site string, hr uintptr, callErr error) error {
	if callErr == nil {
		callErr = fmt.Errorf("HRESULT %s", hresultString(hr))
	}
	return fmt.Errorf("gpui: initializing Direct Manipulation: %s: %w", site, callErr)
}

func dmWrapError(site string, hr uintptr, err error) error {
	return fmt.Errorf("gpui: initializing Direct Manipulation: %s: %w", site, err)
}

// ---------------------------------------------------------------------------
// Precision touchpad availability (the honest machine record)
// ---------------------------------------------------------------------------

// pointerDeviceInfo is Win32 POINTER_DEVICE_INFO (winuser.h; 520
// bytes with the 520-WCHAR product string).
type pointerDeviceInfo struct {
	displayOrientation uint32
	device             uintptr
	pointerDeviceType  uint32
	monitor            uintptr
	startingCursorId   uint32
	maxActiveContacts  uint16
	productString      [520]uint16
}

// PrecisionTouchpadInfo is the machine's actual precision-touchpad
// availability record: how many pointer devices the OS enumerates and
// whether one of them is a touchpad (GetPointerDevices with
// POINTER_DEVICE_TYPE_TOUCH_PAD). A touchpad enumeration means the OS
// exposes precision-touchpad pointer input to Direct Manipulation.
type PrecisionTouchpadInfo struct {
	// DeviceCount is the number of enumerated pointer devices.
	DeviceCount int
	// Touchpad reports whether a touchpad is among them.
	Touchpad bool
	// ProbeError reports an unavailable pointer-device enumeration.
	ProbeError error
}

// PrecisionTouchpadAvailability queries the OS's actual pointer
// devices on the host thread. This is a device record, not a dispatch
// proof: gesture dispatch is validated through simulated content
// updates; device interoperability requires the physical hardware.
func (h *Host) PrecisionTouchpadAvailability() PrecisionTouchpadInfo {
	info, err := queryForeground(h, func() (PrecisionTouchpadInfo, error) {
		if err := procGetPointerDevices.Find(); err != nil {
			return PrecisionTouchpadInfo{ProbeError: fmt.Errorf("GetPointerDevices unavailable: %w", err)}, nil
		}
		var count uint32
		procGetPointerDevices.Call(uintptr(unsafe.Pointer(&count)), 0)
		if count == 0 {
			return PrecisionTouchpadInfo{DeviceCount: 0}, nil
		}
		devices := make([]pointerDeviceInfo, count)
		filled, _, _ := procGetPointerDevices.Call(uintptr(unsafe.Pointer(&count)),
			uintptr(unsafe.Pointer(&devices[0])))
		info := PrecisionTouchpadInfo{DeviceCount: int(uint32(filled))}
		for i := uint32(0); i < uint32(filled); i++ {
			if devices[i].pointerDeviceType == 0x00000004 { // POINTER_DEVICE_TYPE_TOUCH_PAD
				info.Touchpad = true
			}
		}
		return info, nil
	})
	if err != nil {
		return PrecisionTouchpadInfo{ProbeError: err}
	}
	return info
}

// ---------------------------------------------------------------------------
// Simulated gesture driver (test-support)
// ---------------------------------------------------------------------------

// dmFakeContent is a Go-owned IDirectManipulationContent whose
// GetContentTransform returns a scripted transform: the simulated
// gesture source. A simulated gesture validates the gesture state
// machine and event dispatch only — it cannot prove device
// interoperability (Windows generates real touchpad pointer input).
type dmFakeContent struct {
	vtbl *dmFakeContentVtable

	host *Host
	refs int32
	// transform is the scripted content transform result.
	transform [6]float32
}

// dmFakeContentVtable is the IDirectManipulationContent vtable
// (directmanipulation.h: QueryInterface, AddRef, Release,
// GetContentRect, SetContentRect, GetViewport, GetTag, SetTag,
// GetOutputTransform, GetContentTransform, SyncContentTransform).
type dmFakeContentVtable struct {
	queryInterface       uintptr
	addRef               uintptr
	release              uintptr
	getContentRect       uintptr
	setContentRect       uintptr
	getViewport          uintptr
	getTag               uintptr
	setTag               uintptr
	getOutputTransform   uintptr
	getContentTransform  uintptr
	syncContentTransform uintptr
}

var (
	dmFakeContentVtableOnce sync.Once
	dmFakeContentVtableVal  dmFakeContentVtable
)

func dmFakeContentVtableGet() *dmFakeContentVtable {
	dmFakeContentVtableOnce.Do(func() {
		dmFakeContentVtableVal = dmFakeContentVtable{
			queryInterface:       syscall.NewCallback(dmFakeContentQueryInterface),
			addRef:               syscall.NewCallback(dmFakeContentAddRef),
			release:              syscall.NewCallback(dmFakeContentRelease),
			getContentRect:       syscall.NewCallback(dmFakeContentNotImpl2),
			setContentRect:       syscall.NewCallback(dmFakeContentNotImpl2),
			getViewport:          syscall.NewCallback(dmFakeContentNotImpl3),
			getTag:               syscall.NewCallback(dmFakeContentNotImpl4),
			setTag:               syscall.NewCallback(dmFakeContentNotImpl3),
			getOutputTransform:   syscall.NewCallback(dmFakeContentNotImpl2),
			getContentTransform:  syscall.NewCallback(dmFakeGetContentTransform),
			syncContentTransform: syscall.NewCallback(dmFakeContentNotImpl3),
		}
	})
	return &dmFakeContentVtableVal
}

// newDmFakeContent creates a registered fake content object.
func newDmFakeContent(h *Host) *dmFakeContent {
	obj := &dmFakeContent{
		vtbl: dmFakeContentVtableGet(),
		host: h,
	}
	obj.transform[0] = 1
	comRegister(comSelf(obj), obj)
	return obj
}

// SetTransform scripts the content transform ([scale, 0, 0, scale,
// tx, ty]).
func (c *dmFakeContent) SetTransform(scale, tx, ty float32) {
	c.transform[0] = scale
	c.transform[3] = scale
	c.transform[4] = tx
	c.transform[5] = ty
}

// comPointer is the interface pointer for the callback argument.
func (c *dmFakeContent) comPointer() uintptr { return comSelf(c) }

func dmFakeContentQueryInterface(self uintptr, riid, ppv unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmFakeContent)
	if obj == nil || ppv == nil {
		return ePointer
	}
	out := (*uintptr)(ppv)
	*out = 0
	iid := (*guid)(riid)
	if *iid == iidIUnknown || *iid == iidIDirectManipulationContent {
		*out = self
		atomic.AddInt32(&obj.refs, 1)
		return sOK
	}
	return eNoInterface
}

func dmFakeContentAddRef(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmFakeContent)
	if obj == nil {
		return 0
	}
	return uintptr(uint32(atomic.AddInt32(&obj.refs, 1)))
}

func dmFakeContentRelease(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmFakeContent)
	if obj == nil {
		return 0
	}
	refs := atomic.AddInt32(&obj.refs, -1)
	if refs <= 0 {
		comUnregister(self)
		return 0
	}
	return uintptr(uint32(refs))
}

// dmFakeGetContentTransform is IDirectManipulationContent::
// GetContentTransform (slot 9): the scripted transform.
func dmFakeGetContentTransform(self uintptr, transform unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmFakeContent)
	if obj == nil || transform == nil {
		return ePointer
	}
	copy((*[6]float32)(transform)[:], obj.transform[:])
	return sOK
}

// dmFakeContentNotImpl stubs at fixed arities.
func dmFakeContentNotImpl2(self, a uintptr) uintptr       { return eNotImpl }
func dmFakeContentNotImpl3(self, a, b uintptr) uintptr    { return eNotImpl }
func dmFakeContentNotImpl4(self, a, b, c uintptr) uintptr { return eNotImpl }

// dmFakeViewport is a Go-owned IDirectManipulationViewport for
// simulated status changes (ZoomToRect recordable).
type dmFakeViewport struct {
	vtbl *dmViewportVtable
	host *Host
	refs int32
	// zoomToRectCount records ZoomToRect calls.
	zoomToRectCount int32
}

// dmViewportVtable is the IDirectManipulationViewport vtable
// (directmanipulation.h; the slots the port calls).
type dmViewportVtable [31]uintptr

var (
	dmFakeViewportVtableOnce sync.Once
	dmFakeViewportVtableVal  dmViewportVtable
)

func dmFakeViewportVtableGet() *dmViewportVtable {
	dmFakeViewportVtableOnce.Do(func() {
		v := &dmFakeViewportVtableVal
		v[0] = syscall.NewCallback(dmFakeViewportQueryInterface)
		v[1] = syscall.NewCallback(dmFakeViewportAddRef)
		v[2] = syscall.NewCallback(dmFakeViewportRelease)
		v[3] = syscall.NewCallback(dmFakeViewportNotImpl1)
		v[4] = syscall.NewCallback(dmFakeViewportNotImpl1)
		v[5] = syscall.NewCallback(dmFakeViewportNotImpl1)
		v[6] = syscall.NewCallback(dmFakeViewportNotImpl1)
		v[7] = syscall.NewCallback(dmFakeViewportNotImpl2)
		v[8] = syscall.NewCallback(dmFakeViewportNotImpl2)
		v[9] = syscall.NewCallback(dmFakeViewportNotImpl4)
		v[10] = syscall.NewCallback(dmFakeViewportNotImpl3)
		v[11] = syscall.NewCallback(dmFakeViewportNotImpl2)
		v[12] = syscall.NewCallback(dmFakeViewportNotImpl2)
		v[13] = syscall.NewCallback(dmFakeViewportZoomToRect)
		for slot := 14; slot < 31; slot++ {
			v[slot] = syscall.NewCallback(dmFakeViewportNotImpl1)
		}
	})
	return &dmFakeViewportVtableVal
}

// newDmFakeViewport creates a registered fake viewport.
func newDmFakeViewport(h *Host) *dmFakeViewport {
	obj := &dmFakeViewport{
		vtbl: dmFakeViewportVtableGet(),
		host: h,
	}
	comRegister(comSelf(obj), obj)
	return obj
}

// comPointer is the interface pointer.
func (v *dmFakeViewport) comPointer() uintptr { return comSelf(v) }

// ZoomToRectCount reports recorded ZoomToRect calls.
func (v *dmFakeViewport) ZoomToRectCount() int32 { return atomic.LoadInt32(&v.zoomToRectCount) }

func dmFakeViewportQueryInterface(self uintptr, riid, ppv unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmFakeViewport)
	if obj == nil || ppv == nil {
		return ePointer
	}
	out := (*uintptr)(ppv)
	*out = 0
	iid := (*guid)(riid)
	if *iid == iidIUnknown || *iid == iidIDirectManipulationViewport {
		*out = self
		atomic.AddInt32(&obj.refs, 1)
		return sOK
	}
	return eNoInterface
}

func dmFakeViewportAddRef(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmFakeViewport)
	if obj == nil {
		return 0
	}
	return uintptr(uint32(atomic.AddInt32(&obj.refs, 1)))
}

func dmFakeViewportRelease(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*dmFakeViewport)
	if obj == nil {
		return 0
	}
	refs := atomic.AddInt32(&obj.refs, -1)
	if refs <= 0 {
		comUnregister(self)
		return 0
	}
	return uintptr(uint32(refs))
}

// dmFakeViewportZoomToRect records the reset call (slot 13).
func dmFakeViewportZoomToRect(self, left, top, right, bottom uintptr, animate uintptr) uintptr {
	obj, _ := comResolve(self).(*dmFakeViewport)
	if obj == nil {
		return eUnexpected
	}
	atomic.AddInt32(&obj.zoomToRectCount, 1)
	return sOK
}

func dmFakeViewportNotImpl1(self uintptr) uintptr          { return eNotImpl }
func dmFakeViewportNotImpl2(self, a uintptr) uintptr       { return eNotImpl }
func dmFakeViewportNotImpl3(self, a, b uintptr) uintptr    { return eNotImpl }
func dmFakeViewportNotImpl4(self, a, b, c uintptr) uintptr { return eNotImpl }
