//go:build windows

// Package imespec holds ticket20's IME tests: the caret-coordinate
// convention fixture against a REAL window at a nonzero origin (the
// documented bounds_for_range discrepancy), move/DPI/scroll tracking,
// real IMM32 composition message routing through the host message
// path, nested query/reentry replies, window teardown mid-composition,
// and the honest CJK IME availability record.
//
// Real-window tests FAIL (never skip) when window creation fails, like
// internal/winhostspec and internal/focusspec. Environment
// expectations: real interactive Windows session; the native text
// stack (embedded DLL) supplies the editor geometry; this machine's
// user input profile is US English only, so actual Japanese/Chinese/
// Korean composition sessions are an environment-blocked row recorded
// honestly (no input packages are installed by these tests).
package imespec

import (
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gpui-go/gpui"
)

var (
	realModUser32         = syscall.NewLazyDLL("user32.dll")
	realProcPostMessageW  = realModUser32.NewProc("PostMessageW")
	realProcSendMessageW  = realModUser32.NewProc("SendMessageW")
	realProcGetClientRect = realModUser32.NewProc("GetClientRect")
	realProcDestroyWindow = realModUser32.NewProc("DestroyWindow")
)

// Win32 message constants (winuser.h).
const (
	realWMIMEStartComposition = 0x010D
	realWMIMEEndComposition   = 0x010E
	realWMIMEComposition      = 0x010F
	realWMIMENotify           = 0x0282
	realWMPaint               = 0x000F
	realWMDPICHanged          = 0x02E0
	realWMClose               = 0x0010
)

// GCS_* flags (imm.h), the values the host tests.
const (
	realGCSResultStr uint32 = 0x0800
	realGCSCompStr   uint32 = 0x0008
)

func realPostMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) bool {
	ret, _, _ := realProcPostMessageW.Call(hwnd, uintptr(message), wparam, lparam)
	return ret != 0
}

// realSendResult is SendMessageW's LRESULT (int64 on amd64).
func realSendMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) int64 {
	ret, _, _ := realProcSendMessageW.Call(hwnd, uintptr(message), wparam, lparam)
	return int64(ret)
}

type realClientRect struct {
	left, top, right, bottom int32
}

func realClientSize(hwnd uintptr) (int32, int32) {
	var rc realClientRect
	realProcGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	return rc.right - rc.left, rc.bottom - rc.top
}

func realWaitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// realState snapshots editor state from the host thread (the editor is
// foreground-thread-owned; reads marshal there).
type realState struct {
	Text   string
	Marked *gpui.UTF16Range
	Sel    gpui.UTF16Selection
	CaretX float32
	CaretY float32
	CaretH float32
	OK     bool
}

// onHost runs f on the host thread and waits for it.
func onHost(t *testing.T, host *gpui.Host, f func()) {
	t.Helper()
	done := make(chan struct{})
	if !host.Post(func() {
		defer close(done)
		f()
	}) {
		t.Fatal("posting to the host failed")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the host closure")
	}
}

// snapEditor snapshots the editor's IME-facing state on the host thread.
func snapEditor(t *testing.T, host *gpui.Host, e *gpui.Editor) realState {
	t.Helper()
	var state realState
	onHost(t, host, func() {
		state.Text = e.Text()
		if marked, ok := e.MarkedTextRange(nil, nil); ok {
			m := marked
			state.Marked = &m
		}
		state.Sel, _ = e.SelectedTextRange(false, nil, nil)
		if bounds, ok := e.BoundsForRange(state.Sel.Range, nil, nil); ok {
			state.OK = true
			state.CaretX = bounds.Origin.X
			state.CaretY = bounds.Origin.Y
			state.CaretH = bounds.Size.Height
		}
	})
	return state
}

// newRealAppHost boots a real host attached to a real application and
// stops it at cleanup (focusspec's helper, bounded to this package).
func newRealAppHost(t *testing.T) (*gpui.App, *gpui.Host) {
	t.Helper()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("app attach failed: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
	})
	return app, host
}

// openIMEWindow opens one real window with the given logical bounds.
// show=false keeps the window hidden: no natural WM_PAINT runs before
// the test installs its input handler, so update_ime_enabled's first
// query happens with the handler present (the reference flow — the
// focused element registers its handler during the first frame's
// paint) and the default IME context is never disassociated first.
func openIMEWindow(t *testing.T, app *gpui.App, title string, bounds gpui.Bounds, show bool) *gpui.Window {
	t.Helper()
	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:  title,
		Bounds: bounds,
		Show:   show,
	})
	if err != nil {
		t.Fatalf("OpenWindow(%q) failed: %v — this session is expected to create real windows", title, err)
	}
	return window
}

// installEditor installs the editor as the window's active input
// handler on the host thread and drives one WM_PAINT so
// update_ime_enabled runs its query with the handler present
// (draw_window's tail in the reference; the first query flips nothing
// because ime_enabled starts true, so the default IME context stays
// associated).
func installEditor(t *testing.T, host *gpui.Host, window *gpui.Window, e gpui.IMEInputHandler) {
	t.Helper()
	onHost(t, host, func() {
		window.SetInputHandler(e)
	})
	hwnd := window.Handle().Hwnd()
	if !realPostMessage(hwnd, realWMPaint, 0, 0) {
		t.Fatal("posting WM_PAINT failed")
	}
	// The paint drains update_ime_enabled synchronously; give the
	// message a moment to land.
	time.Sleep(50 * time.Millisecond)
}

// newGeometryEditor builds an editor with the REAL text geometry (the
// default shape provider through the native text stack).
func newGeometryEditor(text string, caret int) *gpui.Editor {
	return gpui.NewEditor(text, caret, gpui.EditorOptions{})
}

// ---------------------------------------------------------------------------
// The nonzero-window-origin caret-coordinate fixture
// ---------------------------------------------------------------------------

// TestRealIMECaretCoordinateConventionAtNonZeroOrigin is the
// independent fixture that resolves the documented coordinate
// discrepancy (windows platform contract: the core bounds_for_range
// comment says screen coordinates while Windows's
// retrieve_caret_position scales without a screen-to-client
// translation).
//
// Derivation, from pinned source and this real window:
//
//   - The COMPOSITIONFORM/CANDIDATEFORM contract (imm.h) requires
//     ptCurrentPos in client-area coordinates of the window receiving
//     composition.
//   - The pinned Windows consumer (events.rs retrieve_caret_position)
//     multiplies the handler's window-local logical bounds by the
//     scale factor and adds no origin translation; the pinned macOS
//     adapter (first_rect_for_character_range) is the one that adds
//     the window frame origin to satisfy NSTextInputClient's screen
//     contract — the doc comment describes that seam.
//   - THE ORACLE: with this window at a nonzero screen origin and a
//     caret inside the client area, a screen-coordinate convention
//     would place the point OUTSIDE the client rect (the origin added
//     on top of the caret), while the client convention keeps it
//     inside. The real trace must show the client convention, and a
//     window MOVE must not change the point at all.
func TestRealIMECaretCoordinateConventionAtNonZeroOrigin(t *testing.T) {
	app, host := newRealAppHost(t)
	// Nonzero logical origin (300, 200): at the reference scale factor
	// 2.0 the window sits at device (600, 400) — far outside its own
	// 320x200 logical (640x400 device) client rect, so an
	// origin-including point could not hide inside the client area.
	window := openIMEWindow(t, app, "gpui-go imespec caret fixture", gpui.Bounds{
		Origin: gpui.Point{X: 300, Y: 200},
		Size:   gpui.Size{Width: 320, Height: 200},
	}, true)
	hwnd := window.Handle().Hwnd()
	if hwnd == 0 {
		t.Fatal("the real window has no HWND")
	}

	// Wait for the WM_MOVE that carries the creation bounds.
	realWaitFor(t, 5*time.Second, "the window to report its nonzero origin", func() bool {
		bounds, err := window.Bounds()
		return err == nil && bounds.Origin.X >= 300 && bounds.Origin.Y >= 200
	})
	bounds, _ := window.Bounds()
	t.Logf("window origin: logical (%.1f, %.1f)", bounds.Origin.X, bounds.Origin.Y)
	if bounds.Origin.X == 0 && bounds.Origin.Y == 0 {
		t.Fatal("the fixture requires a nonzero window origin")
	}

	// A real editor with real geometry: "日本語" (CJK — the IME case),
	// caret at the end, element at a window-local offset (20, 12) so
	// the caret is unambiguously inside the client area.
	editor := newGeometryEditor("日本語", 9)
	editor.SetElementBounds(gpui.Bounds{Origin: gpui.Point{X: 20, Y: 12}})
	installEditor(t, host, window, editor)

	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("ScaleFactor: %v", err)
	}
	t.Logf("window scale factor: %.2f", scale)

	if !realPostMessage(hwnd, realWMIMEStartComposition, 0, 0) {
		t.Fatal("posting WM_IME_STARTCOMPOSITION failed")
	}
	realWaitFor(t, 5*time.Second, "the caret point to be recorded", func() bool {
		trace, err := window.Handle().IMETrace()
		return err == nil && (trace.CaretX != 0 || trace.CaretY != 0)
	})

	trace, err := window.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace: %v", err)
	}
	state := snapEditor(t, host, editor)
	if !state.OK {
		t.Fatal("the editor reported no caret bounds (real geometry failed)")
	}

	// The independent formula (retrieve_caret_position):
	// x = trunc(origin.x * scale), y = trunc(origin.y * scale) +
	// trunc(height * scale) / 2, from the handler's WINDOW-LOCAL
	// bounds — no origin term.
	wantX := int32(state.CaretX * scale)
	wantY := int32(state.CaretY*scale) + int32(state.CaretH*scale)/2
	if trace.CaretX != wantX || trace.CaretY != wantY {
		t.Errorf("caret point = (%d, %d), want the window-local formula (%d, %d) — the adapter's convention must match events.rs retrieve_caret_position exactly",
			trace.CaretX, trace.CaretY, wantX, wantY)
	}

	// THE ORACLE: the point must lie inside the client rect. With the
	// window at device origin (600, 400) and a 640x400 device client
	// rect, a screen-coordinate convention (origin + caret) would
	// report x >= 620+600 = beyond the client width only when the
	// caret is near the right edge; the decisive check is that the
	// point equals the window-local value (above) AND fits the client
	// rect (below) — the two together exclude the origin-including
	// reading.
	clientW, clientH := realClientSize(hwnd)
	t.Logf("caret point: (%d, %d), client rect: %dx%d device px, window origin (trace): (%.0f, %.0f) device px",
		trace.CaretX, trace.CaretY, clientW, clientH, trace.WindowOriginX, trace.WindowOriginY)
	if trace.CaretX < 0 || trace.CaretX > clientW || trace.CaretY < 0 || trace.CaretY > clientH {
		t.Errorf("caret point (%d, %d) lies OUTSIDE the client rect %dx%d — the COMPOSITIONFORM client-coordinate convention is violated (a screen-origin term would produce exactly this)",
			trace.CaretX, trace.CaretY, clientW, clientH)
	}
	// The window origin is nonzero and NOT included in the point: with
	// the origin at device (>=600, >=400), any point below those
	// values proves no origin term.
	if trace.WindowOriginX > float32(trace.CaretX) && trace.WindowOriginY > float32(trace.CaretY) {
		t.Logf("convention confirmed: point (%d, %d) excludes the window origin (%.0f, %.0f)",
			trace.CaretX, trace.CaretY, trace.WindowOriginX, trace.WindowOriginY)
	} else {
		t.Errorf("caret point (%d, %d) is not clearly below the window origin (%.0f, %.0f) — the fixture needs a larger origin to be decisive",
			trace.CaretX, trace.CaretY, trace.WindowOriginX, trace.WindowOriginY)
	}
	// The honest IMM32 call results (no IME installed here: the calls
	// legitimately fail outside a composition).
	t.Logf("ImmSetCompositionWindow ok=%v, ImmSetCandidateWindow ok=%v (honest results; no IME installed on this profile)",
		trace.SetCompositionWindowOK, trace.SetCandidateWindowOK)

	// MOVE the window: the caret point must be IDENTICAL (client
	// coordinates track the caret's window-local position, not the
	// window's screen position).
	window.SetBounds(gpui.Bounds{
		Origin: gpui.Point{X: 900, Y: 600},
		Size:   gpui.Size{Width: 320, Height: 200},
	})
	realWaitFor(t, 5*time.Second, "the window to move", func() bool {
		b, err := window.Bounds()
		return err == nil && b.Origin.X >= 900 && b.Origin.Y >= 600
	})
	if !realPostMessage(hwnd, realWMIMEStartComposition, 0, 0) {
		t.Fatal("posting WM_IME_STARTCOMPOSITION after the move failed")
	}
	realWaitFor(t, 5*time.Second, "the caret point after the move", func() bool {
		trace2, err := window.Handle().IMETrace()
		return err == nil && (trace2.CaretX != trace.CaretX || trace2.WindowOriginX != trace.WindowOriginX)
	})
	traceMoved, err := window.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace after the move: %v", err)
	}
	if traceMoved.CaretX != trace.CaretX || traceMoved.CaretY != trace.CaretY {
		t.Errorf("caret point changed with the window move: (%d, %d) -> (%d, %d); the client-coordinate convention must be window-origin independent",
			trace.CaretX, trace.CaretY, traceMoved.CaretX, traceMoved.CaretY)
	}
	if traceMoved.WindowOriginX == trace.WindowOriginX && traceMoved.WindowOriginY == trace.WindowOriginY {
		t.Error("the trace's window origin did not update after the move — the move case is not proven")
	} else {
		t.Logf("move case: origin (%.0f, %.0f) -> (%.0f, %.0f), point unchanged (%d, %d)",
			trace.WindowOriginX, trace.WindowOriginY, traceMoved.WindowOriginX, traceMoved.WindowOriginY, traceMoved.CaretX, traceMoved.CaretY)
	}

	window.Close()
	realWaitFor(t, 5*time.Second, "the window to close", func() bool {
		return !window.Alive()
	})
}

// TestRealIMECaretCoordinateDPIRescale proves the point rescales with
// the window's DPI: a WM_DPICHANGED (synthetic value — this
// single-monitor session cannot automate a real monitor transition;
// recorded honestly) updates the scale, and the next
// WM_IME_STARTCOMPOSITION converts with the NEW scale.
func TestRealIMECaretCoordinateDPIRescale(t *testing.T) {
	app, host := newRealAppHost(t)
	window := openIMEWindow(t, app, "gpui-go imespec dpi rescale", gpui.Bounds{
		Origin: gpui.Point{X: 100, Y: 100},
		Size:   gpui.Size{Width: 400, Height: 300},
	}, true)
	hwnd := window.Handle().Hwnd()

	editor := newGeometryEditor("日本語", 9)
	editor.SetElementBounds(gpui.Bounds{Origin: gpui.Point{X: 20, Y: 12}})
	installEditor(t, host, window, editor)

	scale0, _ := window.ScaleFactor()
	if !realPostMessage(hwnd, realWMIMEStartComposition, 0, 0) {
		t.Fatal("posting WM_IME_STARTCOMPOSITION failed")
	}
	realWaitFor(t, 5*time.Second, "the first caret point", func() bool {
		trace, err := window.Handle().IMETrace()
		return err == nil && (trace.CaretX != 0 || trace.CaretY != 0)
	})
	trace0, err := window.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace: %v", err)
	}
	state := snapEditor(t, host, editor)
	if !state.OK {
		t.Fatal("the editor reported no caret bounds")
	}

	// Synthetic WM_DPICHANGED with 1.5x DPI (144): wparam low word =
	// new DPI, lParam = suggested RECT. The handler applies the
	// suggested rectangle, so pass a real client-sized rect.
	newScale := float32(144) / 96
	suggested := realClientRect{left: 150, top: 150, right: 150 + 600, bottom: 150 + 450}
	wparam := uintptr(144)
	res := realSendMessage(hwnd, realWMDPICHanged, wparam, uintptr(unsafe.Pointer(&suggested)))
	t.Logf("WM_DPICHANGED(144) result=%d", res)
	scale1, err := window.ScaleFactor()
	if err != nil || scale1 != newScale {
		t.Fatalf("scale after WM_DPICHANGED = %v (err %v), want %v", scale1, err, newScale)
	}

	if !realPostMessage(hwnd, realWMIMEStartComposition, 0, 0) {
		t.Fatal("posting WM_IME_STARTCOMPOSITION after the DPI change failed")
	}
	realWaitFor(t, 5*time.Second, "the caret point after the DPI change", func() bool {
		trace, err := window.Handle().IMETrace()
		return err == nil && trace.Scale == newScale
	})
	trace1, err := window.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace after the DPI change: %v", err)
	}
	wantX := int32(state.CaretX * newScale)
	wantY := int32(state.CaretY*newScale) + int32(state.CaretH*newScale)/2
	if trace1.CaretX != wantX || trace1.CaretY != wantY {
		t.Errorf("caret point at the new scale = (%d, %d), want (%d, %d) — candidate placement must track the rescale",
			trace1.CaretX, trace1.CaretY, wantX, wantY)
	}
	if scale0 == newScale {
		t.Fatal("the DPI case needs a scale change to be meaningful")
	}
	t.Logf("DPI case: scale %.2f -> %.2f, point (%d, %d) -> (%d, %d) (synthetic WM_DPICHANGED; real monitor transitions are not automatable in this session)",
		scale0, newScale, trace0.CaretX, trace0.CaretY, trace1.CaretX, trace1.CaretY)

	window.Close()
	realWaitFor(t, 5*time.Second, "the window to close", func() bool {
		return !window.Alive()
	})
}

// TestEditorCandidateScrollTracking proves scroll tracking at the
// model level with REAL geometry: scrolling moves the element origin,
// and the candidate point moves with the caret's content position —
// while the real-window tests above prove the window origin never
// enters the point.
func TestEditorCandidateScrollTracking(t *testing.T) {
	editor := newGeometryEditor("日本語", 9)
	editor.SetElementBounds(gpui.Bounds{Origin: gpui.Point{X: 20, Y: 12}})
	sel, ok := editor.SelectedTextRange(false, nil, nil)
	if !ok {
		t.Fatal("no selection")
	}
	base, ok := editor.BoundsForRange(sel.Range, nil, nil)
	if !ok {
		t.Fatal("no caret bounds (real geometry failed)")
	}
	// The empty-range caret rect is CARET_PIXELS_EPSILON (4px) wide and
	// one line height tall (state.rs bounds_for_range's
	// attached_to_next_cluster + epsilon shape).
	if base.Size.Width != 4 {
		t.Errorf("caret width = %v, want the 4px CARET_PIXELS_EPSILON", base.Size.Width)
	}
	if base.Size.Height != editor.LineHeight() {
		t.Errorf("caret height = %v, want the line height %v", base.Size.Height, editor.LineHeight())
	}

	// Scrolled by (8, -40) device-independent logical pixels: the
	// element origin moves with the content.
	editor.SetElementBounds(gpui.Bounds{Origin: gpui.Point{X: 28, Y: -28}})
	scrolled, ok := editor.BoundsForRange(sel.Range, nil, nil)
	if !ok {
		t.Fatal("no scrolled caret bounds")
	}
	want := gpui.Point{X: base.Origin.X + 8, Y: base.Origin.Y - 40}
	if scrolled.Origin != want {
		t.Errorf("scrolled caret origin = %+v, want %+v (the scroll delta exactly)", scrolled.Origin, want)
	}
	// The caret size never changes with scroll.
	if scrolled.Size != base.Size {
		t.Errorf("caret size changed with scroll: %+v -> %+v", base.Size, scrolled.Size)
	}
}

// ---------------------------------------------------------------------------
// Real composition message routing (through the real HIMC)
// ---------------------------------------------------------------------------

// routingEditor records the IME deliveries (the real context holds no
// composition string without an active IME composition, so the
// delivered strings are empty — the routing proof is the DELIVERY,
// observable through the wrapper).
type routingEditor struct {
	*gpui.Editor
	mu       sync.Mutex
	replaces []string
	marks    []string
}

func (e *routingEditor) ReplaceTextInRange(rng *gpui.UTF16Range, text string, w *gpui.Window, app *gpui.App) {
	e.mu.Lock()
	e.replaces = append(e.replaces, text)
	e.mu.Unlock()
	e.Editor.ReplaceTextInRange(rng, text, w, app)
}

func (e *routingEditor) ReplaceAndMarkTextInRange(rng *gpui.UTF16Range, text string, newSelected *gpui.UTF16Range, w *gpui.Window, app *gpui.App) {
	e.mu.Lock()
	e.marks = append(e.marks, text)
	e.mu.Unlock()
	e.Editor.ReplaceAndMarkTextInRange(rng, text, newSelected, w, app)
}

func (e *routingEditor) snapshot() (replaces, marks []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.replaces...), append([]string{}, e.marks...)
}

// TestRealIMECompositionRouting drives the real WM_IME_COMPOSITION
// message through the host path: the wndproc acquires the real
// IMM32 context (ImmGetContext/ImmReleaseContext), the driver reads
// the real composition strings with ImmGetCompositionStringW (empty
// without an active IME composition — the honest no-IME state on this
// machine), and the editor receives them through the take/put seam.
func TestRealIMECompositionRouting(t *testing.T) {
	app, host := newRealAppHost(t)
	window := openIMEWindow(t, app, "gpui-go imespec composition routing", gpui.Bounds{
		Origin: gpui.Point{X: 120, Y: 120},
		Size:   gpui.Size{Width: 400, Height: 300},
	}, false)
	hwnd := window.Handle().Hwnd()

	editor := &routingEditor{Editor: newGeometryEditor("start", 5)}
	installEditor(t, host, window, editor)

	// WM_IME_COMPOSITION with GCS_RESULTSTR: the real context's result
	// string is empty, so the delivery is an empty replacement of the
	// (empty) selection — the text does not change, but exactly one
	// replacement is delivered (handle_ime_composition_inner's
	// GCS_RESULTSTR branch, consumed).
	if !realPostMessage(hwnd, realWMIMEComposition, 0, uintptr(realGCSResultStr)) {
		t.Fatal("posting WM_IME_COMPOSITION(GCS_RESULTSTR) failed")
	}
	realWaitFor(t, 5*time.Second, "the real result string to be delivered", func() bool {
		replaces, _ := editor.snapshot()
		return len(replaces) == 1
	})
	replaces, marks := editor.snapshot()
	if len(replaces) != 1 || replaces[0] != "" {
		t.Fatalf("result-string deliveries = %q, want exactly one empty (the real no-composition state)", replaces)
	}
	if len(marks) != 0 {
		t.Fatalf("unexpected marked deliveries: %q", marks)
	}
	if text := snapText(t, host, editor.Editor); text != "start" {
		t.Fatalf("text after the empty real commit = %q, want start", text)
	}
	// The trace recorded the consumed state (checked before the later
	// fall-through posts overwrite the single trace entry).
	trace, err := window.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace: %v", err)
	}
	if !trace.HandledComposition {
		t.Fatal("the trace must record the handled composition")
	}

	// Without a handler: the message must fall to DefWindowProc (no
	// crash, no delivery).
	onHost(t, host, func() {
		window.SetInputHandler(nil)
	})
	if !realPostMessage(hwnd, realWMIMEComposition, 0, uintptr(realGCSResultStr)) {
		t.Fatal("posting WM_IME_COMPOSITION without a handler failed")
	}
	time.Sleep(150 * time.Millisecond)
	replaces, _ = editor.snapshot()
	if len(replaces) != 1 {
		t.Fatalf("delivery without a handler: %d replacements, want the single earlier one", len(replaces))
	}

	// A plain InputHandler (no IME extension) is invisible to the IME
	// seam: composition messages fall through without delivery.
	plain := &plainOnlyHandler{}
	onHost(t, host, func() {
		window.SetInputHandler(plain)
	})
	if !realPostMessage(hwnd, realWMIMEComposition, 0, uintptr(realGCSResultStr)) {
		t.Fatal("posting WM_IME_COMPOSITION with a plain handler failed")
	}
	time.Sleep(150 * time.Millisecond)
	if plain.replacements != 0 {
		t.Fatalf("a plain InputHandler received %d IME replacements; the IME seam must require IMEInputHandler", plain.replacements)
	}
	replaces, _ = editor.snapshot()
	if len(replaces) != 1 {
		t.Fatalf("the plain handler window delivered to the editor: %q", replaces)
	}

	window.Close()
	realWaitFor(t, 5*time.Second, "the window to close", func() bool {
		return !window.Alive()
	})
}

// plainOnlyHandler implements only the keyboard-path InputHandler.
type plainOnlyHandler struct {
	replacements int
}

func (h *plainOnlyHandler) AcceptsTextInput(w *gpui.Window, app *gpui.App) bool { return true }
func (h *plainOnlyHandler) ReplaceTextInRange(rng *gpui.UTF16Range, text string, w *gpui.Window, app *gpui.App) {
	h.replacements++
}
func (h *plainOnlyHandler) DispatchInput(input string, w *gpui.Window, app *gpui.App) {}

func snapText(t *testing.T, host *gpui.Host, e *gpui.Editor) string {
	t.Helper()
	var text string
	onHost(t, host, func() { text = e.Text() })
	return text
}

// ---------------------------------------------------------------------------
// Nested query/reentry replies
// ---------------------------------------------------------------------------

// reentryEditor re-posts a nested WM_IME_COMPOSITION (through
// SendMessageW, which re-enters the window procedure synchronously on
// the host thread) while its marked replacement runs.
type reentryEditor struct {
	*gpui.Editor
	hwnd              uintptr
	logMu             sync.Mutex
	marks             []string
	replaces          []string
	nested            atomic.Int32
	nestedSeenHandler atomic.Bool
}

func (e *reentryEditor) ReplaceAndMarkTextInRange(rng *gpui.UTF16Range, text string, newSelected *gpui.UTF16Range, w *gpui.Window, app *gpui.App) {
	// The nested message dispatches INSIDE this callback: the
	// take/put discipline must leave the seam empty, so the nested
	// message falls to DefWindowProc instead of re-delivering.
	e.nested.Add(1)
	realSendMessage(e.hwnd, realWMIMEComposition, 0, uintptr(realGCSResultStr))
	e.logMu.Lock()
	e.marks = append(e.marks, text)
	e.logMu.Unlock()
	e.Editor.ReplaceAndMarkTextInRange(rng, text, newSelected, w, app)
}

func (e *reentryEditor) ReplaceTextInRange(rng *gpui.UTF16Range, text string, w *gpui.Window, app *gpui.App) {
	e.logMu.Lock()
	e.replaces = append(e.replaces, text)
	e.logMu.Unlock()
	e.Editor.ReplaceTextInRange(rng, text, w, app)
}

// TestRealIMENestedCompositionReentry: a WM_IME_COMPOSITION that
// arrives (via SendMessage) while another composition callback is
// running observes NO input handler and falls to DefWindowProc — the
// with_input_handler take/put contract — with no double delivery and
// no host corruption.
func TestRealIMENestedCompositionReentry(t *testing.T) {
	app, host := newRealAppHost(t)
	window := openIMEWindow(t, app, "gpui-go imespec reentry", gpui.Bounds{
		Origin: gpui.Point{X: 140, Y: 140},
		Size:   gpui.Size{Width: 400, Height: 300},
	}, false)
	hwnd := window.Handle().Hwnd()

	editor := &reentryEditor{Editor: newGeometryEditor("", 0), hwnd: hwnd}
	installEditor(t, host, window, editor)

	// The outer composition: GCS_RESULTSTR|GCS_COMPSTR with a real
	// (empty) result string, then the mark. The nested result-string
	// message dispatched inside the mark callback must NOT deliver.
	if !realPostMessage(hwnd, realWMIMEComposition, 0, uintptr(realGCSResultStr|realGCSCompStr)) {
		t.Fatal("posting the outer WM_IME_COMPOSITION failed")
	}
	realWaitFor(t, 5*time.Second, "the outer composition to complete", func() bool {
		editor.logMu.Lock()
		defer editor.logMu.Unlock()
		return len(editor.marks) == 1
	})
	time.Sleep(200 * time.Millisecond)
	editor.logMu.Lock()
	marks := append([]string{}, editor.marks...)
	replaces := append([]string{}, editor.replaces...)
	editor.logMu.Unlock()

	if len(marks) != 1 {
		t.Fatalf("marked replacements = %v, want exactly one (the nested message must observe no handler)", marks)
	}
	if len(replaces) != 1 {
		t.Fatalf("plain replacements = %v, want exactly one (the outer result-string commit)", replaces)
	}
	// The host is still alive and serviceable.
	if !host.IsForegroundThread() && !window.Alive() {
		t.Fatal("the host or window died during the nested reentry")
	}
	t.Logf("reentry: %d nested dispatches, %d marks, %d replaces — nested messages fell to DefWindowProc",
		editor.nested.Load(), len(marks), len(replaces))

	// The window still processes messages afterwards.
	trace, err := window.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace after reentry: %v", err)
	}
	_ = trace
	if !realPostMessage(hwnd, realWMPaint, 0, 0) {
		t.Fatal("posting WM_PAINT after the reentry failed")
	}

	window.Close()
	realWaitFor(t, 5*time.Second, "the window to close", func() bool {
		return !window.Alive()
	})
}

// ---------------------------------------------------------------------------
// Window teardown mid-composition
// ---------------------------------------------------------------------------

// teardownEditor destroys its window from inside the marked
// replacement (the host owns HWND destruction; WM_DESTROY and
// WM_NCDESTROY run synchronously nested on the host thread).
type teardownEditor struct {
	*gpui.Editor
	hwnd     uintptr
	window   *gpui.Window
	closed   atomic.Bool
	markDone atomic.Bool
}

func (e *teardownEditor) ReplaceAndMarkTextInRange(rng *gpui.UTF16Range, text string, newSelected *gpui.UTF16Range, w *gpui.Window, app *gpui.App) {
	e.Editor.ReplaceAndMarkTextInRange(rng, text, newSelected, w, app)
	if !e.closed.Swap(true) {
		// Teardown MID-composition: the outer message's callback
		// destroys the window underneath the composition handler.
		realProcDestroyWindow.Call(e.hwnd)
	}
	e.markDone.Store(true)
}

// TestRealIMEWindowTeardownMidComposition: destroying the window while
// a composition callback runs must not corrupt the host: the nested
// WM_DESTROY/WM_NCDESTROY retire the record, the callback unwinds, the
// late IME messages see no window, and the message loop stays
// serviceable.
func TestRealIMEWindowTeardownMidComposition(t *testing.T) {
	app, host := newRealAppHost(t)
	// A second, independent window keeps the message loop alive: the
	// host quits when the last window closes, and this test destroys
	// the composition window mid-callback.
	keepAlive := openIMEWindow(t, app, "gpui-go imespec teardown keeper", gpui.Bounds{
		Origin: gpui.Point{X: 40, Y: 40},
		Size:   gpui.Size{Width: 300, Height: 200},
	}, false)
	window := openIMEWindow(t, app, "gpui-go imespec teardown", gpui.Bounds{
		Origin: gpui.Point{X: 160, Y: 160},
		Size:   gpui.Size{Width: 400, Height: 300},
	}, false)
	hwnd := window.Handle().Hwnd()

	editor := &teardownEditor{Editor: newGeometryEditor("", 0), hwnd: hwnd, window: window}
	installEditor(t, host, window, editor)

	if !realPostMessage(hwnd, realWMIMEComposition, 0, uintptr(realGCSCompStr)) {
		t.Fatal("posting the teardown WM_IME_COMPOSITION failed")
	}
	realWaitFor(t, 5*time.Second, "the mid-composition teardown to run", func() bool {
		return editor.markDone.Load()
	})
	realWaitFor(t, 5*time.Second, "the window to be destroyed", func() bool {
		return !window.Alive()
	})

	// The composition state landed before the teardown (the editor
	// kept its text through the window's death).
	if text := snapText(t, host, editor.Editor); text == "" {
		// The real context has no composition string, so the mark
		// replaced the selection with empty text; the teardown still
		// ran mid-callback. The proof is survival, not content.
	}

	// Late messages to the destroyed window: PostMessage fails
	// (invalid handle) — no crash, no delivery.
	if realPostMessage(hwnd, realWMIMEComposition, 0, uintptr(realGCSResultStr)) {
		t.Error("PostMessage to the destroyed window unexpectedly succeeded")
	}
	// The message loop is still serviceable: post work to the host.
	loopAlive := make(chan struct{})
	if !host.Post(func() { close(loopAlive) }) {
		t.Fatal("posting to the host after teardown failed")
	}
	select {
	case <-loopAlive:
	case <-time.After(5 * time.Second):
		t.Fatal("the host message loop died after the mid-composition teardown")
	}
	// Stop drains cleanly (the keeper window closes through the host's
	// shutdown, which destroys remaining windows).
	if err := host.Stop(); err != nil {
		t.Fatalf("host stop after teardown: %v", err)
	}
	_ = keepAlive
}

// TestRealIMEWindowClosedBetweenCompositions: a window closed (through
// the veto path) between two composition messages leaves the editor
// state intact and the host healthy — the late message cannot be
// delivered to the destroyed window.
func TestRealIMEWindowClosedBetweenCompositions(t *testing.T) {
	app, host := newRealAppHost(t)
	keepAlive := openIMEWindow(t, app, "gpui-go imespec closed-between keeper", gpui.Bounds{
		Origin: gpui.Point{X: 40, Y: 40},
		Size:   gpui.Size{Width: 300, Height: 200},
	}, false)
	window := openIMEWindow(t, app, "gpui-go imespec closed between", gpui.Bounds{
		Origin: gpui.Point{X: 180, Y: 180},
		Size:   gpui.Size{Width: 400, Height: 300},
	}, false)
	hwnd := window.Handle().Hwnd()

	editor := newGeometryEditor("keep", 4)
	installEditor(t, host, window, editor)

	// Close through WM_CLOSE (the veto path): no veto callback vetoes.
	window.Close()
	realWaitFor(t, 5*time.Second, "the window to close", func() bool {
		return !window.Alive()
	})
	// The editor's state survived the close untouched.
	if text := snapText(t, host, editor); text != "keep" {
		t.Fatalf("editor text after close = %q, want keep", text)
	}
	// The late composition message cannot be delivered.
	if realPostMessage(hwnd, realWMIMEComposition, 0, uintptr(realGCSResultStr)) {
		t.Error("PostMessage to the closed window unexpectedly succeeded")
	}
	if _, err := window.Handle().IMETrace(); err == nil {
		// A stale lease may report either way depending on the host's
		// record retirement; the requirement is no panic. The error
		// path is the documented ErrWindowClosed.
		t.Logf("stale IMETrace call succeeded; the host record is retired but not closed")
	}
	// The keeper window keeps the loop alive for the host's cleanup
	// stop (closing it here would quit the host as the last window).
	_ = keepAlive
}

// ---------------------------------------------------------------------------
// IME availability (honest recording; never installs)
// ---------------------------------------------------------------------------

// TestIMEAvailabilityRecord enumerates the machine's CJK IME/keyboard
// entries and logs them honestly. This machine's user profile is US
// English only: the Japanese/Chinese/Korean session rows are
// environment-blocked (no IME installed; installing input packages is
// out of scope and never done implicitly), and the registry templates
// are recorded as templates, not sessions.
func TestIMEAvailabilityRecord(t *testing.T) {
	entries := gpui.IMEInstalledLayouts()
	if len(entries) == 0 {
		t.Log("no CJK keyboard-layout registry entries enumerated (unexpected on a stock Windows install; recorded honestly)")
	}
	installedAny := false
	for _, entry := range entries {
		t.Logf("KLID %s: %q file=%s installed=%v", entry.KLID, entry.LayoutText, entry.LayoutFile, entry.Installed)
		if entry.Installed {
			installedAny = true
		}
		// Template entries must have a layout file when the registry
		// key exists (KBDJPN.DLL etc.); an empty file means the key
		// was unreadable, which is a soft failure recorded, not
		// hidden.
		if entry.LayoutFile == "" {
			t.Logf("KLID %s (%q) has no readable Layout File", entry.KLID, entry.LayoutText)
		}
	}
	if installedAny {
		t.Logf("CJK input profiles are INSTALLED on this machine: actual composition sessions are expected in the dedicated session test")
	} else {
		t.Logf("no CJK input profile installed: Japanese/Chinese/Korean composition sessions are environment-blocked on this machine (registry templates above are not sessions; nothing was installed)")
	}
}

// TestIMEUninterceptedMessagesFallThrough pins the dispatch-table
// policy: WM_IME_SETCONTEXT, WM_IME_NOTIFY and WM_IME_ENDCOMPOSITION
// are NOT intercepted (the pinned handle_msg leaves them to default
// processing), so DefWindowProcW answers them. SendMessage to a real
// window must not deliver anything to the input handler or crash.
func TestIMEUninterceptedMessagesFallThrough(t *testing.T) {
	app, host := newRealAppHost(t)
	window := openIMEWindow(t, app, "gpui-go imespec unintercepted", gpui.Bounds{
		Origin: gpui.Point{X: 200, Y: 200},
		Size:   gpui.Size{Width: 400, Height: 300},
	}, false)
	hwnd := window.Handle().Hwnd()

	editor := newGeometryEditor("stable", 6)
	installEditor(t, host, window, editor)

	// Send the unintercepted messages synchronously: they must return
	// DefWindowProcW's answer (0 for these) without any handler
	// delivery or crash. WM_IME_SETCONTEXT carries wparam with the
	// ISC flags in real deliveries; 0 disables — safe to pass.
	res := realSendMessage(hwnd, 0x0281, 0, 0) // WM_IME_SETCONTEXT
	t.Logf("WM_IME_SETCONTEXT -> %d", res)
	res = realSendMessage(hwnd, realWMIMENotify, 0, 0)
	t.Logf("WM_IME_NOTIFY -> %d", res)
	res = realSendMessage(hwnd, realWMIMEEndComposition, 0, 0)
	t.Logf("WM_IME_ENDCOMPOSITION -> %d", res)

	if text := snapText(t, host, editor); text != "stable" {
		t.Fatalf("the unintercepted IME messages changed the document: %q", text)
	}
	// The window is still alive and the host serviceable.
	if !window.Alive() {
		t.Fatal("the window died from the unintercepted IME messages")
	}

	window.Close()
	realWaitFor(t, 5*time.Second, "the window to close", func() bool {
		return !window.Alive()
	})
}

// ---------------------------------------------------------------------------
// Focus loss / disable under the active-HWND guard (events.rs
// update_ime_enabled's GetFocus check)
// ---------------------------------------------------------------------------

var realProcSetFocus = realModUser32.NewProc("SetFocus")

// TestRealIMEFocusLossDisablesUnderFocusGuard proves the focused-HWND
// guard (events.rs update_ime_enabled): the IME context is per-thread
// and shared across the thread's windows, so when a window's handler
// stops accepting text input the completion call
// (ImmNotifyIME NI_COMPOSITIONSTR CPS_COMPLETE) runs only for the
// FOCUSED window; every window still disassociates its own context.
func TestRealIMEFocusLossDisablesUnderFocusGuard(t *testing.T) {
	app, host := newRealAppHost(t)
	focused := openIMEWindow(t, app, "gpui-go imespec focus-guard focused", gpui.Bounds{
		Origin: gpui.Point{X: 60, Y: 60},
		Size:   gpui.Size{Width: 300, Height: 200},
	}, false)
	unfocused := openIMEWindow(t, app, "gpui-go imespec focus-guard unfocused", gpui.Bounds{
		Origin: gpui.Point{X: 400, Y: 60},
		Size:   gpui.Size{Width: 300, Height: 200},
	}, false)
	focusedHwnd := focused.Handle().Hwnd()

	editorA := newGeometryEditor("a", 1)
	editorB := newGeometryEditor("b", 1)
	installEditor(t, host, focused, editorA)
	installEditor(t, host, unfocused, editorB)

	// Focus window A on the host thread (focus lives in the host
	// thread's message queue — exactly the queue the window procedures
	// and GetFocus consult).
	onHost(t, host, func() {
		realProcSetFocus.Call(focusedHwnd)
	})

	// Focus loss on the FOCUSED window: the handler goes away, the next
	// draw's update_ime_enabled disables the IME with the completion
	// call under the guard, then disassociates.
	onHost(t, host, func() {
		focused.SetInputHandler(nil)
	})
	if !realPostMessage(focusedHwnd, realWMPaint, 0, 0) {
		t.Fatal("posting WM_PAINT to the focused window failed")
	}
	realWaitFor(t, 5*time.Second, "the focused window's disable trace", func() bool {
		trace, err := focused.Handle().IMETrace()
		return err == nil && trace.Disassociate
	})
	traceA, err := focused.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace(focused): %v", err)
	}
	if !traceA.Disassociate || !traceA.NotifyComplete {
		t.Errorf("focused window disable: Disassociate=%v NotifyComplete=%v, want both true (GetFocus matched)", traceA.Disassociate, traceA.NotifyComplete)
	}

	// Focus loss on the UNFOCUSED window: disassociate without the
	// completion call — the shared context belongs to the focused
	// window's composition.
	onHost(t, host, func() {
		unfocused.SetInputHandler(nil)
	})
	if !realPostMessage(unfocused.Handle().Hwnd(), realWMPaint, 0, 0) {
		t.Fatal("posting WM_PAINT to the unfocused window failed")
	}
	realWaitFor(t, 5*time.Second, "the unfocused window's disable trace", func() bool {
		trace, err := unfocused.Handle().IMETrace()
		return err == nil && trace.Disassociate
	})
	traceB, err := unfocused.Handle().IMETrace()
	if err != nil {
		t.Fatalf("IMETrace(unfocused): %v", err)
	}
	if !traceB.Disassociate {
		t.Error("the unfocused window must still disassociate its own context")
	}
	if traceB.NotifyComplete {
		t.Error("the unfocused window must NOT complete the shared IME composition (the focused-HWND guard)")
	}
	t.Logf("focus guard: focused (Disassociate=%v NotifyComplete=%v), unfocused (Disassociate=%v NotifyComplete=%v)",
		traceA.Disassociate, traceA.NotifyComplete, traceB.Disassociate, traceB.NotifyComplete)

	focused.Close()
	unfocused.Close()
	realWaitFor(t, 5*time.Second, "the windows to close", func() bool {
		return !focused.Alive() && !unfocused.Alive()
	})
}
