//go:build windows

package gpui

// Ticket24's Direct Manipulation gates: the real COM setup and
// lifecycle against the OS, the simulated gesture dispatch (a
// SIMULATED gesture validates the gesture-state machine and event
// dispatch only — device interoperability requires a physical
// precision touchpad, which Windows alone generates pointer input
// for), and the machine's ACTUAL precision-touchpad availability
// record.
//
// The simulated gestures drive the real event handler's
// IDirectManipulationViewportEventHandler vtable (the exact entry
// points the OS viewport calls) with Go-owned fake content/viewport
// objects, so the transform read, classification, phase tracking and
// event synthesis all run for real; only the gesture input itself is
// synthetic.

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// dmGestureRecorder collects the gesture events the window dispatch
// observed (the onGesture callback).
type dmGestureRecorder struct {
	scrolls []string
	pinches []string
}

// openDMTestWindow opens a shown real window whose gesture callback
// records the dispatched events (through the same dispatchInput path
// WM_PAINT's drain uses).
func openDMTestWindow(t *testing.T, h *Host, app *App, recorder *dmGestureRecorder) *Window {
	t.Helper()
	window, err := app.OpenWindow(WindowOptions{
		Title:     "gpui-go direct manipulation",
		Bounds:    Bounds{Origin: Point{X: 40, Y: 40}, Size: Size{Width: 240, Height: 160}},
		Show:      true,
		Resizable: true,
		OnGesture: func(event any) {
			switch e := event.(type) {
			case *ScrollWheelEvent:
				recorder.scrolls = append(recorder.scrolls,
					fmt.Sprintf("scroll(%s,%.2f,%.2f)", e.TouchPhase, e.Delta.Pixels.X, e.Delta.Pixels.Y))
			case *PinchEvent:
				recorder.pinches = append(recorder.pinches,
					fmt.Sprintf("pinch(%s,%.3f)", e.Phase, e.Delta))
			}
		},
	})
	if err != nil {
		t.Fatalf("OpenWindow failed: %v — this session is expected to create real windows", err)
	}
	return window
}

// dmHandlerOf resolves the window's DM handler through the host lease.
func dmHandlerOf(t *testing.T, w *Window) *directManipulationHandler {
	t.Helper()
	h := w.handle.host
	if h == nil {
		t.Fatalf("the window has no host")
	}
	var handler *directManipulationHandler
	if !h.runForegroundSync(func() {
		record := w.handle.record()
		if record == nil || record.dm == nil {
			return
		}
		handler = record.dm
	}) {
		t.Fatalf("the host stopped before the query")
	}
	if handler == nil {
		t.Fatalf("the window has no Direct Manipulation handler")
	}
	return handler
}

// dmDrain drains the queued gesture events through the window's
// WM_PAINT drain path (update + drain + the onGesture delivery), so
// the recorder observes exactly what a paint-driven frame delivers.
func dmDrain(t *testing.T, w *Window) {
	t.Helper()
	h := w.handle.host
	if !h.runForegroundSync(func() {
		record := w.handle.record()
		if record == nil {
			return
		}
		record.drainDirectManipulation()
	}) {
		t.Fatalf("the host stopped before the drain")
	}
}

// dmDriveContentUpdated invokes the event handler's OnContentUpdated
// (vtable slot 5) with the fake content, the exact call the OS
// viewport makes. The drive runs on the host (STA) thread — the thread
// the OS delivers the callbacks on — so the drive, the queue and the
// pump's own drains stay serialized.
func dmDriveContentUpdated(t *testing.T, handler *directManipulationHandler, viewport, content uintptr) {
	t.Helper()
	if !handler.eventHandler.host.runForegroundSync(func() {
		eventHandler := handler.eventHandler
		vtbl := (*[7]uintptr)(unsafe.Pointer(eventHandler.vtbl))
		hr, _, _ := syscall.SyscallN(vtbl[5], comSelf(eventHandler), viewport, content)
		if int32(hr) < 0 {
			t.Errorf("OnContentUpdated returned %s", hresultString(hr))
		}
	}) {
		t.Fatalf("the host stopped before the drive")
	}
}

// dmDriveStatusChanged invokes OnViewportStatusChanged (vtable slot
// 3) with the fake viewport, on the host thread.
func dmDriveStatusChanged(t *testing.T, handler *directManipulationHandler, viewport uintptr, current, previous uint32) {
	t.Helper()
	if !handler.eventHandler.host.runForegroundSync(func() {
		eventHandler := handler.eventHandler
		vtbl := (*[7]uintptr)(unsafe.Pointer(eventHandler.vtbl))
		hr, _, _ := syscall.SyscallN(vtbl[3], comSelf(eventHandler), viewport, uintptr(current), uintptr(previous))
		if int32(hr) < 0 {
			t.Errorf("OnViewportStatusChanged returned %s", hresultString(hr))
		}
	}) {
		t.Fatalf("the host stopped before the drive")
	}
}

// TestDirectManipulationRealSetupAndLifecycle verifies the real COM
// setup against the OS: every creation call succeeded (the window
// creation would otherwise have failed — the pin's fatal `?`), Update
// runs without error, and the teardown (Stop/Abandon/Deactivate plus
// the interface releases) happens on the owning apartment at window
// destruction with the host stopping cleanly afterwards.
func TestDirectManipulationRealSetupAndLifecycle(t *testing.T) {
	h, app := newDropTestApp(t)
	recorder := &dmGestureRecorder{}
	window := openDMTestWindow(t, h, app, recorder)
	handler := dmHandlerOf(t, window)

	// The real update manager accepts the manual update.
	handler.update()

	// Close the window: the DM teardown runs at WM_DESTROY (the pin's
	// Drop: Stop/Abandon/Deactivate) on the host apartment.
	window.Close()
	desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })

	// The host stops cleanly (the balanced OleUninitialize is the
	// apartment-release evidence after the COM work).
	if err := h.Stop(); err != nil {
		t.Fatalf("host stop after Direct Manipulation: %v", err)
	}
}

// TestDirectManipulationSimulatedScrollGesture drives the gesture
// state machine through the event handler's real vtable with a fake
// content object: a translation-only transform classifies as a scroll
// (Started on the first update, Moved afterwards), the pixel deltas
// are the offset differences scaled by the window's scale factor, and
// the RUNNING -> READY status change ends the gesture (the Ended
// event) and resets the transform (one ZoomToRect on the fake
// viewport).
func TestDirectManipulationSimulatedScrollGesture(t *testing.T) {
	h, app := newDropTestApp(t)
	recorder := &dmGestureRecorder{}
	window := openDMTestWindow(t, h, app, recorder)
	handler := dmHandlerOf(t, window)

	viewport := newDmFakeViewport(h)
	defer dropFakeViewport(viewport)
	content := newDmFakeContent(h)
	defer dropFakeContent(content)

	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("window scale: %v", err)
	}
	// Window-relative translation of 10 device pixels.
	txDevice := float32(10)
	txLogical := txDevice / scale

	content.SetTransform(1, txDevice, 0)
	dmDriveContentUpdated(t, handler, viewport.comPointer(), content.comPointer())
	dmDrain(t, window)
	content.SetTransform(1, 2*txDevice, 0)
	dmDriveContentUpdated(t, handler, viewport.comPointer(), content.comPointer())
	dmDrain(t, window)

	events := append([]string(nil), recorder.scrolls...)
	if len(events) < 2 {
		t.Fatalf("simulated scroll events = %v, want at least the Started and Moved updates", events)
	}
	wantFirst := fmt.Sprintf("scroll(%s,%.2f,0.00)", TouchPhaseStarted, txLogical)
	if events[0] != wantFirst {
		t.Fatalf("first scroll event = %q, want %q", events[0], wantFirst)
	}
	wantSecond := fmt.Sprintf("scroll(%s,%.2f,0.00)", TouchPhaseMoved, txLogical)
	if events[1] != wantSecond {
		t.Fatalf("second scroll event = %q, want %q", events[1], wantSecond)
	}

	// The READY cycle ends the gesture and resets the content transform
	// (ZoomToRect through the viewport).
	dmDriveStatusChanged(t, handler, viewport.comPointer(), dmStatusReady, dmStatusRunning)
	dmDrain(t, window)
	events = append([]string(nil), recorder.scrolls...)
	if events[len(events)-1] != "scroll(Ended,0.00,0.00)" {
		t.Fatalf("last scroll event = %q, want the Ended event", events[len(events)-1])
	}
	if got := viewport.ZoomToRectCount(); got != 1 {
		t.Fatalf("ZoomToRect calls = %d, want 1 (the reset, no infinite loop)", got)
	}

	// A subsequent scroll starts fresh (Started, not Moved): the reset
	// cleared the classification and the last offsets, so the delta is
	// the whole offset from the reset baseline.
	content.SetTransform(1, 3*txDevice, 0)
	dmDriveContentUpdated(t, handler, viewport.comPointer(), content.comPointer())
	dmDrain(t, window)
	events = append([]string(nil), recorder.scrolls...)
	last := events[len(events)-1]
	if want := fmt.Sprintf("scroll(%s,%.2f,0.00)", TouchPhaseStarted, 3*txLogical); last != want {
		t.Fatalf("post-reset scroll event = %q, want a fresh %q", last, want)
	}
}

// TestDirectManipulationSimulatedPinchGesture drives the pinch
// classification: a scale-changing transform ends any scroll gesture,
// emits the pinch Started event, then Moved deltas of scale/lastScale
// - 1; the inertia-interrupt status change (RUNNING after INERTIA)
// ends the active gesture.
func TestDirectManipulationSimulatedPinchGesture(t *testing.T) {
	h, app := newDropTestApp(t)
	recorder := &dmGestureRecorder{}
	window := openDMTestWindow(t, h, app, recorder)
	handler := dmHandlerOf(t, window)

	viewport := newDmFakeViewport(h)
	defer dropFakeViewport(viewport)
	content := newDmFakeContent(h)
	defer dropFakeContent(content)

	// Start a scroll, then pinch: the scroll gesture ends first (the
	// Scroll -> Pinch direction is allowed).
	content.SetTransform(1, 5, 0)
	dmDriveContentUpdated(t, handler, viewport.comPointer(), content.comPointer())
	content.SetTransform(1.5, 5, 0)
	dmDriveContentUpdated(t, handler, viewport.comPointer(), content.comPointer())
	dmDrain(t, window)

	scrolls := append([]string(nil), recorder.scrolls...)
	// The pin's classification ends the scroll gesture before the pinch
	// starts (OnContentUpdated: end_gesture then Pinch Started), so the
	// scroll's Ended event lands between them.
	if len(scrolls) != 2 ||
		!hasPrefix(scrolls[0], "scroll(Started,") ||
		scrolls[1] != "scroll(Ended,0.00,0.00)" {
		t.Fatalf("pinch-start scroll events = %v, want the Started scroll then its Ended event", scrolls)
	}
	pinches := append([]string(nil), recorder.pinches...)
	if len(pinches) != 2 {
		t.Fatalf("pinch events = %v, want Started then Moved", pinches)
	}
	if pinches[0] != "pinch(Started,0.000)" {
		t.Fatalf("first pinch event = %q, want the Started zero-delta", pinches[0])
	}
	if pinches[1] != "pinch(Moved,0.500)" {
		t.Fatalf("second pinch event = %q, want Moved 1.5/1.0-1 = 0.5", pinches[1])
	}

	// RUNNING after INERTIA: the interrupted gesture ends.
	dmDriveStatusChanged(t, handler, viewport.comPointer(), dmStatusRunning, dmStatusInertia)
	dmDrain(t, window)
	pinches = append([]string(nil), recorder.pinches...)
	if pinches[len(pinches)-1] != "pinch(Ended,0.000)" {
		t.Fatalf("last pinch event = %q, want the Ended event", pinches[len(pinches)-1])
	}
}

// hasPrefix is a local strings.HasPrefix (avoid the import in this
// file).
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// dropFakeViewport / dropFakeContent release the fake objects'
// construction references.
func dropFakeViewport(v *dmFakeViewport) { dmFakeViewportRelease(comSelf(v)) }

func dropFakeContent(c *dmFakeContent) { dmFakeContentRelease(comSelf(c)) }

// TestDirectManipulationDrainDispatchesThroughInputPath drives a
// gesture and drains it through the WM_PAINT drain path
// (drainDirectManipulation: update + drain + the onGesture delivery),
// verifying the events reach the app's input dispatch rather than a
// side channel.
func TestDirectManipulationDrainDispatchesThroughInputPath(t *testing.T) {
	h, app := newDropTestApp(t)
	recorder := &dmGestureRecorder{}
	window := openDMTestWindow(t, h, app, recorder)
	handler := dmHandlerOf(t, window)

	// Queue events without the drain, then drain through the paint-path
	// method: the recorder sees them only after the drain.
	viewport := newDmFakeViewport(h)
	defer dropFakeViewport(viewport)
	content := newDmFakeContent(h)
	defer dropFakeContent(content)
	content.SetTransform(1, 20, 0)
	dmDriveContentUpdated(t, handler, viewport.comPointer(), content.comPointer())

	if len(recorder.scrolls) != 0 {
		t.Fatalf("events reached the recorder before the drain: %v", recorder.scrolls)
	}
	dmDrain(t, window)
	if len(recorder.scrolls) != 1 {
		t.Fatalf("after the drain scroll events = %v, want the queued gesture", recorder.scrolls)
	}
}

// TestPrecisionTouchpadAvailabilityRecord records the machine's ACTUAL
// precision-touchpad availability through GetPointerDevices. This is a
// record, not a capability assertion: absence is a legitimate machine
// state (the simulated-gesture tests validate dispatch; device
// interoperability requires the physical hardware and is NOT proven
// here).
func TestPrecisionTouchpadAvailabilityRecord(t *testing.T) {
	h := desktopNewHost(t)
	info := h.PrecisionTouchpadAvailability()
	if info.ProbeError != nil {
		t.Fatalf("the pointer-device probe failed: %v", info.ProbeError)
	}
	t.Logf("precision touchpad availability: devices=%d touchpad=%v (an honest machine record; dispatch was validated by simulated gestures, device interoperability is NOT proven)",
		info.DeviceCount, info.Touchpad)
}

// TestDMPaintPathDrainsOnRealWindow paints a real window after
// queueing a gesture: the WM_PAINT drain delivers the events.
func TestDMPaintPathDrainsOnRealWindow(t *testing.T) {
	h, app := newDropTestApp(t)
	recorder := &dmGestureRecorder{}
	window := openDMTestWindow(t, h, app, recorder)
	handler := dmHandlerOf(t, window)

	viewport := newDmFakeViewport(h)
	defer dropFakeViewport(viewport)
	content := newDmFakeContent(h)
	defer dropFakeContent(content)
	content.SetTransform(1, 12, 0)
	dmDriveContentUpdated(t, handler, viewport.comPointer(), content.comPointer())

	// Invalidate so the next pump paints (the drain runs at WM_PAINT).
	h.runForegroundSync(func() {
		record := window.handle.record()
		if record == nil {
			return
		}
		invalidateWindow(record.hwnd)
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(recorder.scrolls) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(recorder.scrolls) != 1 {
		t.Fatalf("the WM_PAINT drain did not deliver the queued gesture (scroll events = %v)", recorder.scrolls)
	}
}

// invalidateWindow invalidates the whole client area so WM_PAINT
// arrives (the host's paint stub).
func invalidateWindow(hwnd uintptr) {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if r.right > r.left && r.bottom > r.top {
		procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(&r)), 0)
	}
}

// procInvalidateRect is the paint-invalidations binding (user32).
var procInvalidateRect = modUser32.NewProc("InvalidateRect")

// unused guard for atomic (the fake viewport's counter).
var _ = atomic.AddInt32
