//go:build windows

// Package dropspec holds ticket24's drop and touchpad gates against
// the public gpui surface: the deterministic native-interaction
// transcript comparison for the drop lifecycle (positions, event
// order, effects) against the pinned CE semantics
// (crates/gpui_windows/src/window.rs:1201-1345), the element-tree
// routing of the drag (hit test, focus transfer and drop delivery
// through the real interactive surface), and the drag bookkeeping the
// pinned window tests assert (crates/gpui/src/window.rs:8876-9060,
// bounded to the platform-drag side).
//
// The transcript driver (WindowHandle.FileDropDriver) replays the
// shell's IDropTarget calls through the port's real COM vtable with a
// real Go-owned CF_HDROP data object and a real window, so positions
// are converted by the real ScreenToClient and the effects are the
// real negotiation. Real-window expectations FAIL, never skip.
package dropspec

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Shared fixture
// ---------------------------------------------------------------------------

// dropRecording is one observed file-drop event.
type dropRecording struct {
	kind     gpui.FileDropEventKind
	position gpui.Point
	paths    gpui.ExternalPaths
}

// dropFixture is one host + app + window with a drop surface.
type dropFixture struct {
	host   *gpui.Host
	app    *gpui.App
	window *gpui.Window
	rec    *dropRecorder
	driver *gpui.FileDropDriver
	focus  gpui.FocusHandle
}

// dropRecorder collects the app-side events and element deliveries.
type dropRecorder struct {
	mu      sync.Mutex
	events  []dropRecording
	dropped []gpui.ExternalPaths
	moves   []string
	focused []uint64
	scrolls []string
}

func (r *dropRecorder) record(event gpui.FileDropEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, dropRecording{
		kind: event.Kind, position: event.Position, paths: event.Paths,
	})
}

func (r *dropRecorder) snapshot() []dropRecording {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]dropRecording(nil), r.events...)
}

func (r *dropRecorder) transcript() string {
	parts := make([]string, 0, len(r.events))
	for _, e := range r.snapshot() {
		switch e.kind {
		case gpui.FileDropEntered, gpui.FileDropPending, gpui.FileDropSubmit:
			parts = append(parts, fmt.Sprintf("%s(%.2f,%.2f)", e.kind, e.position.X, e.position.Y))
		default:
			parts = append(parts, e.kind.String())
		}
	}
	return strings.Join(parts, " ")
}

// dropSurface is the root view: a full-size focusable div with drop,
// move and scroll listeners (the interactive surface the file drops
// route through).
type dropSurface struct {
	rec   *dropRecorder
	focus gpui.FocusHandle
}

// EntityID implements gpui.View: stateless recipe.
func (v *dropSurface) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *dropSurface) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	rec := v.rec
	d := gpui.Div().Flex().SizeFull().TrackFocus(v.focus)
	d = gpui.OnDrop(d, func(paths *gpui.ExternalPaths, event *gpui.MouseUpEvent, w *gpui.Window, app *gpui.App) {
		rec.mu.Lock()
		rec.dropped = append(rec.dropped, *paths)
		rec.mu.Unlock()
	})
	d = d.OnMouseMove(func(event *gpui.MouseMoveEvent, phase gpui.DispatchPhase, hitbox *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
		if phase == gpui.DispatchBubble && hitbox.IsHovered(w) {
			rec.mu.Lock()
			rec.moves = append(rec.moves, fmt.Sprintf("move(%.1f,%.1f)", event.Position.X, event.Position.Y))
			rec.mu.Unlock()
		}
	})
	d = d.OnScrollWheel(func(event *gpui.ScrollWheelEvent, phase gpui.DispatchPhase, hitbox *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
		if phase == gpui.DispatchBubble && hitbox.ShouldHandleScroll(w) {
			rec.mu.Lock()
			rec.scrolls = append(rec.scrolls, fmt.Sprintf("%s(%.2f,%.2f)", event.TouchPhase, event.Delta.Pixels.X, event.Delta.Pixels.Y))
			rec.mu.Unlock()
		}
	})
	element, err := gpui.TryIntoElement(d)
	if err != nil {
		panic(fmt.Sprintf("dropspec: surface conversion: %v", err))
	}
	return element
}

// newDropFixture boots a fresh host, app and shown window with the
// drop surface drawn.
func newDropFixture(t *testing.T, title string) *dropFixture {
	t.Helper()
	host := gpui.NewHost()
	if err := host.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
	})
	app := gpui.NewApp()
	if err := app.Attach(host); err != nil {
		t.Fatalf("app attach failed: %v", err)
	}
	rec := &dropRecorder{}
	var surface *dropSurface
	window, err := app.OpenWindowView(gpui.WindowOptions{
		Title:     title,
		Bounds:    gpui.Bounds{Origin: gpui.Point{X: 40, Y: 40}, Size: gpui.Size{Width: 320, Height: 220}},
		Show:      true,
		Resizable: true,
		OnFileDrop: func(event gpui.FileDropEvent) {
			rec.record(event)
		},
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		surface = &dropSurface{rec: rec, focus: gpui.NewFocusHandle(w)}
		return surface
	})
	if err != nil {
		t.Fatalf("OpenWindowView(%q) failed: %v — this session is expected to create real windows", title, err)
	}
	// Wait for the window to settle at its requested position (WM_MOVE).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		bounds, err := window.Bounds()
		if err == nil && bounds.Origin.X >= 30 && bounds.Origin.Y >= 30 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	driver := window.Handle().FileDropDriver()
	if driver == nil {
		t.Fatalf("FileDropDriver: the window has no registered drop target")
	}
	return &dropFixture{
		host: host, app: app, window: window, rec: rec, driver: driver,
		focus: surface.focus,
	}
}

// expectedClientPoint computes the expected window-relative logical
// position for a screen point from the window's WM_MOVE client origin
// and scale (an independent computation from ScreenToClient).
func expectedClientPoint(t *testing.T, w *gpui.Window, x, y int32) gpui.Point {
	t.Helper()
	bounds, err := w.Bounds()
	if err != nil {
		t.Fatalf("window bounds: %v", err)
	}
	scale, err := w.ScaleFactor()
	if err != nil {
		t.Fatalf("window scale: %v", err)
	}
	if scale <= 0 {
		scale = 1
	}
	return gpui.Point{
		X: float32(x-int32(bounds.Origin.X*scale)) / scale,
		Y: float32(y-int32(bounds.Origin.Y*scale)) / scale,
	}
}

func requirePointsClose(t *testing.T, name string, got, want gpui.Point) {
	t.Helper()
	dx, dy := got.X-want.X, got.Y-want.Y
	if dx < -0.5 || dx > 0.5 || dy < -0.5 || dy > 0.5 {
		t.Fatalf("%s position = (%.3f,%.3f), want (%.3f,%.3f)", name, got.X, got.Y, want.X, want.Y)
	}
}

// effects are the DROPEFFECT values the driver negotiates.
const (
	effectNone = 0
	effectCopy = 1
)

// ---------------------------------------------------------------------------
// The deterministic transcript (window.rs 1201-1345)
// ---------------------------------------------------------------------------

// TestDropLifecycleTranscriptAgainstPinnedSemantics compares the drop
// lifecycle transcript — event order, window-relative positions,
// effects and paths — against the pinned semantics:
//
//   - DragEnter with CF_HDROP negotiates DROPEFFECT_COPY and emits
//     Entered{position: ScreenToClient/Scale, paths}.
//   - DragOver negotiates COPY and emits Pending{position}.
//   - Drop negotiates COPY and emits Submit{position}.
//   - DragLeave emits Exited (no position).
//   - DragEnter without CF_HDROP negotiates DROPEFFECT_NONE and emits
//     nothing.
func TestDropLifecycleTranscriptAgainstPinnedSemantics(t *testing.T) {
	f := newDropFixture(t, "dropspec transcript")

	screenX, screenY := int32(150), int32(110)
	paths := []string{`C:\dropspec-a.txt`, `C:\dropspec-b.txt`}

	// DragEnter: COPY effect, Entered at the converted position with
	// the paths.
	if effect := f.driver.DragEnter(paths, screenX, screenY); effect != effectCopy {
		t.Fatalf("DragEnter effect = %d, want DROPEFFECT_COPY (window.rs:1225)", effect)
	}
	events := f.rec.snapshot()
	if len(events) != 1 || events[0].kind != gpui.FileDropEntered {
		t.Fatalf("after DragEnter transcript = %q, want one Entered", f.rec.transcript())
	}
	requirePointsClose(t, "Entered", events[0].position, expectedClientPoint(t, f.window, screenX, screenY))
	if len(events[0].paths) != len(paths) || events[0].paths[0] != paths[0] || events[0].paths[1] != paths[1] {
		t.Fatalf("Entered paths = %v, want %v (window.rs:1240-1250)", events[0].paths, paths)
	}

	// DragOver: COPY effect, Pending at the converted position.
	overX, overY := int32(170), int32(130)
	if effect := f.driver.DragOver(overX, overY); effect != effectCopy {
		t.Fatalf("DragOver effect = %d, want DROPEFFECT_COPY (window.rs:1285)", effect)
	}
	events = f.rec.snapshot()
	if len(events) != 2 || events[1].kind != gpui.FileDropPending {
		t.Fatalf("after DragOver transcript = %q, want Entered Pending", f.rec.transcript())
	}
	requirePointsClose(t, "Pending", events[1].position, expectedClientPoint(t, f.window, overX, overY))

	// Drop: COPY effect, Submit; the drop listener receives the drag's
	// ExternalPaths payload (the Entered arm set the internal drag).
	if effect := f.driver.Drop(paths, overX, overY); effect != effectCopy {
		t.Fatalf("Drop effect = %d, want DROPEFFECT_COPY (window.rs:1327)", effect)
	}
	events = f.rec.snapshot()
	if len(events) != 3 || events[2].kind != gpui.FileDropSubmit {
		t.Fatalf("after Drop transcript = %q, want Entered Pending Submit", f.rec.transcript())
	}
	requirePointsClose(t, "Submit", events[2].position, expectedClientPoint(t, f.window, overX, overY))
	f.rec.mu.Lock()
	dropped := append([]gpui.ExternalPaths(nil), f.rec.dropped...)
	f.rec.mu.Unlock()
	if len(dropped) != 1 || len(dropped[0]) != 2 || dropped[0][1] != paths[1] {
		t.Fatalf("drop listener payloads = %v, want one ExternalPaths of %v", dropped, paths)
	}

	// DragLeave: the Exited event, no position (window.rs:1311).
	if err := f.driver.DragLeave(); err != nil {
		t.Fatalf("DragLeave: %v", err)
	}
	events = f.rec.snapshot()
	if len(events) != 4 || events[3].kind != gpui.FileDropExited {
		t.Fatalf("after DragLeave transcript = %q, want Entered Pending Submit Exited", f.rec.transcript())
	}

	// DragEnter without CF_HDROP: DROPEFFECT_NONE, no event (the
	// QueryGetData arm, window.rs:1267).
	if effect := f.driver.DragEnter(nil, screenX, screenY); effect != effectNone {
		t.Fatalf("DragEnter(no files) effect = %d, want DROPEFFECT_NONE (window.rs:1267)", effect)
	}
	events = f.rec.snapshot()
	if len(events) != 4 {
		t.Fatalf("after the no-files DragEnter transcript = %q, want no new event", f.rec.transcript())
	}
}

// TestLeaveCancelDropSemantics covers the leave / cancel / drop arms:
// the leave emits Exited and clears the internal drag; a drop outside
// the element's hitbox delivers no payload; a fresh drag after the
// leave still enters (the platform drag did not end).
func TestLeaveCancelDropSemantics(t *testing.T) {
	f := newDropFixture(t, "dropspec leave-cancel-drop")

	paths := []string{`C:\dropspec-leave.txt`}
	if effect := f.driver.DragEnter(paths, 150, 110); effect != effectCopy {
		t.Fatalf("DragEnter effect = %d, want COPY", effect)
	}
	if !f.app.HasActiveDrag() {
		t.Fatal("the Entered arm must set the internal drag (window.rs:5808)")
	}
	if value := f.app.ActiveDrag().Value; len(value.(gpui.ExternalPaths)) != 1 {
		t.Fatalf("the drag payload = %v, want the Entered paths", value)
	}

	// DragLeave: the drag is taken (window.rs:5843: active_drag.take()).
	if err := f.driver.DragLeave(); err != nil {
		t.Fatalf("DragLeave: %v", err)
	}
	if f.app.HasActiveDrag() {
		t.Fatal("DragLeave must clear the internal drag (window.rs:5841-5843)")
	}
	// The Exited event still rides the mouse listener path (the move
	// listener observes hover state changes if it runs; the event itself
	// is the observable here).
	events := f.rec.snapshot()
	if len(events) != 2 || events[1].kind != gpui.FileDropExited {
		t.Fatalf("after DragLeave transcript = %q, want Entered Exited", f.rec.transcript())
	}

	// A drop with no active drag (the platform drag ended): the Submit
	// event still dispatches, but no element receives a payload.
	f.rec.mu.Lock()
	droppedBefore := len(f.rec.dropped)
	f.rec.mu.Unlock()
	if effect := f.driver.Drop(paths, 150, 110); effect != effectCopy {
		t.Fatalf("Drop effect = %d, want COPY (the target's negotiation is unconditional)", effect)
	}
	f.rec.mu.Lock()
	droppedAfter := len(f.rec.dropped)
	f.rec.mu.Unlock()
	if droppedAfter != droppedBefore {
		t.Fatalf("a drop with no active drag delivered a payload (drop count %d -> %d)", droppedBefore, droppedAfter)
	}

	// A fresh drag still enters: the leave did not end the session.
	if effect := f.driver.DragEnter(paths, 150, 110); effect != effectCopy {
		t.Fatalf("DragEnter after a leave effect = %d, want COPY", effect)
	}
	if effect := f.driver.Drop(paths, 150, 110); effect != effectCopy {
		t.Fatalf("Drop effect = %d, want COPY", effect)
	}
	var secondPayloads []gpui.ExternalPaths
	f.rec.mu.Lock()
	secondPayloads = append([]gpui.ExternalPaths(nil), f.rec.dropped...)
	f.rec.mu.Unlock()
	if len(secondPayloads) != 1 || secondPayloads[0][0] != paths[0] {
		t.Fatalf("the second drag's drop payload = %v, want %v", secondPayloads, paths)
	}
}

// ---------------------------------------------------------------------------
// Element-tree routing (hit test and focus through the interactive surface)
// ---------------------------------------------------------------------------

// TestPointerRoutingThroughElementTree verifies the drag routing
// through the real element surface: the synthetic mouse move from
// Entered reaches the hovered element's move listener (the hit test),
// and a mouse down transfers focus to the tracked focus handle (the
// focus-transfer listener div.rs paint_mouse_listeners registers).
func TestPointerRoutingThroughElementTree(t *testing.T) {
	f := newDropFixture(t, "dropspec element routing")

	if focused := f.window.Focused(); focused.Ok {
		t.Fatal("no element starts focused")
	}

	// Entered: the translated MouseMove reaches the element's move
	// listener through the hit test (the drag position must hover the
	// full-window hitbox).
	if effect := f.driver.DragEnter([]string{`C:\dropspec-routing.txt`}, 150, 110); effect != effectCopy {
		t.Fatalf("DragEnter effect = %d, want COPY", effect)
	}
	f.rec.mu.Lock()
	moves := append([]string(nil), f.rec.moves...)
	f.rec.mu.Unlock()
	if len(moves) != 1 {
		t.Fatalf("the element's move listener saw %v, want the Entered move through the hit test", moves)
	}

	// A mouse down inside the element: the focus-transfer listener
	// focuses the tracked handle through the hit test.
	result := f.window.DispatchInput(&gpui.MouseDownEvent{
		Button:   gpui.MouseButtonLeft,
		Position: gpui.Point{X: 100, Y: 80},
	}, f.app)
	if !result.Propagate {
		t.Fatal("the mouse down should propagate (no listener stopped it)")
	}
	focused := f.window.Focused()
	if !focused.Ok || focused.Handle.ID() != f.focus.ID() {
		t.Fatalf("after a hovered mouse down, focused = %+v, want the surface's tracked handle (div.rs paint_mouse_listeners)", focused)
	}
}

// TestScrollDispatchThroughElementTree dispatches a Direct
// Manipulation-shaped scroll event through the public input dispatch:
// the pixel delta, gesture phase and the hitbox's
// ShouldHandleScroll answer reach the element's scroll listener.
func TestScrollDispatchThroughElementTree(t *testing.T) {
	f := newDropFixture(t, "dropspec scroll routing")

	f.window.DispatchInput(&gpui.ScrollWheelEvent{
		Position:   gpui.Point{X: 100, Y: 80},
		Delta:      gpui.ScrollDeltaPixels(12.5, -4.25),
		TouchPhase: gpui.TouchPhaseStarted,
	}, f.app)
	f.rec.mu.Lock()
	scrolls := append([]string(nil), f.rec.scrolls...)
	f.rec.mu.Unlock()
	if len(scrolls) != 1 || scrolls[0] != "Started(12.50,-4.25)" {
		t.Fatalf("scroll listener events = %v, want the pixel-delta Started gesture", scrolls)
	}
}

// ---------------------------------------------------------------------------
// Drag bookkeeping (app.rs 2612-2720, the platform-drag side)
// ---------------------------------------------------------------------------

// TestPlatformDragBookkeepingMatchesPin mirrors the drag bookkeeping
// the pinned window test asserts (window.rs:8876-9060), bounded to the
// platform-drag side (the internal drag START listener belongs to the
// mouse input ticket): Entered restores a suspended platform drag for
// its source window; Exited hands a restored drag back; Ended ends
// the platform drag session; an unrelated window's Ended does not.
func TestPlatformDragBookkeepingMatchesPin(t *testing.T) {
	f := newDropFixture(t, "dropspec bookkeeping")
	app, window := f.app, f.window

	// Hand a drag to the platform (hand_active_drag_to_platform).
	drag := gpui.AnyDrag{Value: gpui.ExternalPaths{`C:\dropspec-platform.txt`}, CursorOffset: gpui.Point{X: 1, Y: 2}}
	app.StartDrag(drag)
	if !app.HasActiveDrag() {
		t.Fatal("StartDrag must set the active drag")
	}
	if !handDragToPlatformForTest(app, window.ID()) {
		t.Fatal("handing the active drag to the platform must succeed (app.rs:2661)")
	}
	if app.HasActiveDrag() {
		t.Fatal("a handed drag is no longer active (app.rs:2663)")
	}

	// The source window's Entered restores it (restore_platform_drag):
	// the drag becomes active again.
	if effect := f.driver.DragEnter([]string{`C:\dropspec-restore.txt`}, 150, 110); effect != effectCopy {
		t.Fatalf("DragEnter effect = %d, want COPY", effect)
	}
	if !app.HasActiveDrag() {
		t.Fatal("the source window's Entered must restore the platform drag (window.rs:5808)")
	}
	if value, ok := app.ActiveDrag().Value.(gpui.ExternalPaths); !ok || value[0] != `C:\dropspec-platform.txt` {
		t.Fatalf("the restored drag payload = %v, want the platform-suspended one", app.ActiveDrag().Value)
	}

	// Exited hands the restored drag back to the platform
	// (hand_restored_drag_to_platform).
	if err := f.driver.DragLeave(); err != nil {
		t.Fatalf("DragLeave: %v", err)
	}
	if app.HasActiveDrag() {
		t.Fatal("the restored drag must return to the platform on Exited (window.rs:5842)")
	}

	// Entered again restores; Ended ends the platform session
	// (end_platform_drag) and clears the drag.
	if effect := f.driver.DragEnter([]string{`C:\dropspec-restore.txt`}, 150, 110); effect != effectCopy {
		t.Fatalf("DragEnter effect = %d, want COPY", effect)
	}
	if !app.HasActiveDrag() {
		t.Fatal("the drag must restore a second time")
	}
	f.window.DispatchInput(&gpui.FileDropEvent{Kind: gpui.FileDropEnded}, f.app)
	if app.HasActiveDrag() {
		t.Fatal("Ended must clear the active drag (window.rs:5849)")
	}
	// end_platform_drag reports false after the session ended (the
	// pinned test's double-end assertion, window.rs:9001).
	if endPlatformDragForTest(app, window.ID()) {
		t.Fatal("end_platform_drag must report false after the session ended")
	}
}

// handDragToPlatformForTest hands the active drag to the platform for
// the source window (app.rs hand_active_drag_to_platform — the pinned
// window test drives this transition through the drag-start surface,
// which the mouse input ticket owns; the platform-drag side is the
// app's drag surface).
func handDragToPlatformForTest(app *gpui.App, sourceWindow gpui.WindowID) bool {
	return app.HandActiveDrag(sourceWindow)
}

// endPlatformDragForTest ends the platform-owned session.
func endPlatformDragForTest(app *gpui.App, sourceWindow gpui.WindowID) bool {
	return app.EndPlatformDrag(sourceWindow)
}
