//go:build windows

package gpui

import (
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/hoangvu12/gpui-go/internal/realinput"
)

var (
	procGetWindowLongPtrTitlebar = modUser32.NewProc("GetWindowLongPtrW")
	procGetDCTitlebar            = modUser32.NewProc("GetDC")
	procReleaseDCTitlebar        = modUser32.NewProc("ReleaseDC")
	procGetPixelTitlebar         = modGdi32.NewProc("GetPixel")
)

const gwlExStyle = ^uintptr(19) // GWL_EXSTYLE (-20)

// TestWin32WindowStylesKeepTheDefault checks the option plumbing into
// CreateWindowExW: without the new options the styles are exactly the
// pre-titlebar ones, the titlebar option changes no style (WM_NCCALCSIZE
// hides the caption), and DirectComposition adds only
// WS_EX_NOREDIRECTIONBITMAP.
func TestWin32WindowStylesKeepTheDefault(t *testing.T) {
	style, ex := win32WindowStyles(WindowOptions{})
	if style != wsSysMenu|wsCaption || ex != wsExAppWindow {
		t.Fatalf("default styles = %#x, %#x; want %#x, %#x", style, ex, wsSysMenu|wsCaption, wsExAppWindow)
	}
	full := WindowOptions{Resizable: true, MinimizeBox: true}
	style, ex = win32WindowStyles(full)
	wantStyle := uintptr(wsSysMenu | wsCaption | wsThickFrame | wsMaximizeBox | wsMinimizeBox)
	if style != wantStyle || ex != wsExAppWindow {
		t.Fatalf("resizable styles = %#x, %#x", style, ex)
	}
	full.Titlebar = &TitlebarOptions{AppearsTransparent: true}
	full.Immovable = true
	if s, e := win32WindowStyles(full); s != wantStyle || e != wsExAppWindow {
		t.Fatalf("hidden-titlebar styles = %#x, %#x; want the same as the default", s, e)
	}
	full.DirectComposition = true
	if s, e := win32WindowStyles(full); s != wantStyle || e != wsExAppWindow|wsExNoRedirectionBitmap {
		t.Fatalf("DirectComposition styles = %#x, %#x", s, e)
	}
}

// TestTitlebarTopResizeHitTest is the top-border half of events.rs
// handle_hit_test_msg: the top frame band of a resizable, restored
// window answers HTTOPLEFT / HTTOP / HTTOPRIGHT.
func TestTitlebarTopResizeHitTest(t *testing.T) {
	const frameX, frameY, width = 8, 8, 816 // window rect width
	cases := []struct {
		x, y                 int32
		resizable, maximized bool
		want                 uintptr
		ok                   bool
	}{
		{0, 0, true, false, htTopLeft, true},
		{-3, 4, true, false, htTopLeft, true},
		{400, 0, true, false, htTop, true},
		{400, 8, true, false, htTop, true},
		{width - 1 - 2*frameX, 2, true, false, htTopRight, true},
		{width - 2 - 2*frameX, 2, true, false, htTop, true},
		{400, 9, true, false, 0, false},
		{400, -1, true, false, 0, false},
		{400, 2, false, false, 0, false},
		{400, 2, true, true, 0, false},
	}
	for _, c := range cases {
		got, ok := titlebarTopResizeHitTest(c.x, c.y, frameX, frameY, width, c.resizable, c.maximized)
		if ok != c.ok || got != c.want {
			t.Errorf("(%d,%d resizable=%v maximized=%v) = %d, %v; want %d, %v",
				c.x, c.y, c.resizable, c.maximized, got, ok, c.want, c.ok)
		}
	}
}

// TestMakePointsLParam checks the POINTS packing StartWindowMove posts.
func TestMakePointsLParam(t *testing.T) {
	lp := makePointsLParam(-5, 1234)
	if signedLoword(lp) != -5 || signedHiword(lp) != 1234 {
		t.Fatalf("makePointsLParam(-5, 1234) unpacks to %d, %d", signedLoword(lp), signedHiword(lp))
	}
}

// titlebarClientTop returns the client area's top and the window rect's
// top in screen device pixels.
func titlebarClientTop(t *testing.T, h *Host, hwnd uintptr) (clientTop, windowTop int32) {
	t.Helper()
	if !h.runForegroundSync(func() {
		var origin point
		procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&origin)))
		var wr rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
		clientTop, windowTop = origin.y, wr.top
	}) {
		t.Fatal("the host stopped")
	}
	return clientTop, windowTop
}

// sendTitlebar sends one message to the window (cross-thread SendMessage
// runs it on the host thread).
func sendTitlebar(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}

// TestWin32TitlebarHiddenWindowFrame opens never-shown windows and checks
// WM_NCCALCSIZE and the host's hit test: a hidden titlebar puts the
// client top at the window top, the default window keeps its caption,
// the top band answers HTTOP, and the control-area hook answers its
// codes for both. Hidden windows only; nothing is shown.
func TestWin32TitlebarHiddenWindowFrame(t *testing.T) {
	h := desktopNewHost(t)
	closeArea := func(position Point) (WindowControlArea, bool) {
		if position.Y >= 40 && position.Y < 60 && position.X >= 100 && position.X < 140 {
			return WindowControlAreaClose, true
		}
		if position.Y >= 40 && position.Y < 60 && position.X >= 140 && position.X < 180 {
			return WindowControlAreaMax, true
		}
		return 0, false
	}
	bounds := Bounds{Origin: Point{X: 200, Y: 200}, Size: Size{Width: 400, Height: 300}}
	hidden, err := h.OpenWindow(WindowOptions{
		Title: "titlebar hidden", Bounds: bounds, Resizable: true, MinimizeBox: true,
		Titlebar:               &TitlebarOptions{AppearsTransparent: true},
		DirectComposition:      true,
		OnHitTestWindowControl: closeArea,
	})
	if err != nil {
		t.Fatalf("OpenWindow(hidden titlebar): %v", err)
	}
	defer hidden.Close()
	plain, err := h.OpenWindow(WindowOptions{
		Title: "titlebar default", Bounds: bounds, Resizable: true,
		OnHitTestWindowControl: closeArea,
	})
	if err != nil {
		t.Fatalf("OpenWindow(default): %v", err)
	}
	defer plain.Close()
	if hidden.Visible() || plain.Visible() {
		t.Fatal("the test windows must stay hidden")
	}

	clientTop, windowTop := titlebarClientTop(t, h, hidden.Hwnd())
	if clientTop != windowTop {
		t.Errorf("hidden titlebar: client top %d, window top %d; want equal (WM_NCCALCSIZE)", clientTop, windowTop)
	}
	plainClientTop, plainWindowTop := titlebarClientTop(t, h, plain.Hwnd())
	if plainClientTop <= plainWindowTop {
		t.Errorf("default window: client top %d, window top %d; want the caption above the client", plainClientTop, plainWindowTop)
	}
	got, err := hidden.Bounds()
	if size := got.Size; err != nil || size != bounds.Size {
		scale, _ := hidden.ScaleFactor()
		if scale == 1 {
			t.Errorf("hidden titlebar client size = %+v, %v; want %+v", got.Size, err, bounds.Size)
		} else {
			t.Logf("hidden titlebar client size = %+v at scale %v", got.Size, scale)
		}
	}

	var hiddenEx, plainEx uintptr
	h.runForegroundSync(func() {
		hiddenEx, _, _ = procGetWindowLongPtrTitlebar.Call(hidden.Hwnd(), gwlExStyle)
		plainEx, _, _ = procGetWindowLongPtrTitlebar.Call(plain.Hwnd(), gwlExStyle)
	})
	if hiddenEx&wsExNoRedirectionBitmap == 0 {
		t.Errorf("DirectComposition window ex-style %#x lacks WS_EX_NOREDIRECTIONBITMAP", hiddenEx)
	}
	if plainEx&wsExNoRedirectionBitmap != 0 {
		t.Errorf("default window ex-style %#x has WS_EX_NOREDIRECTIONBITMAP", plainEx)
	}

	screen := func(handle WindowHandle, x, y float32) uintptr {
		var p point
		h.runForegroundSync(func() {
			scale := handle.record().scale
			p = point{x: int32(x * scale), y: int32(y * scale)}
			procClientToScreen.Call(handle.Hwnd(), uintptr(unsafe.Pointer(&p)))
		})
		return makePointsLParam(p.x, p.y)
	}
	checks := []struct {
		name   string
		handle WindowHandle
		x, y   float32
		want   uintptr
	}{
		{"hidden top band", hidden, 200, 1, htTop},
		{"hidden close area", hidden, 110, 50, htClose},
		{"hidden max area", hidden, 150, 50, htMaxButton},
		{"hidden client", hidden, 200, 150, htClient},
		{"default close area", plain, 110, 50, htClose},
		{"default client", plain, 200, 150, htClient},
	}
	for _, c := range checks {
		if got := sendTitlebar(c.handle.Hwnd(), wmNCHitTest, 0, screen(c.handle, c.x, c.y)); got != c.want {
			t.Errorf("%s: WM_NCHITTEST = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestWin32TitlebarElementsDriveHitTestAndCaptionButtons opens a
// never-shown app window whose root view draws a client titlebar, then
// drives the window procedure: WM_NCHITTEST answers the elements'
// codes, a non-client move reaches element hover, and a non-client
// press and release on the close button posts WM_CLOSE (vetoed here).
func TestWin32TitlebarElementsDriveHitTestAndCaptionButtons(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	var closeRequests, hovered atomic.Int32
	window, err := app.OpenWindowView(WindowOptions{
		Title:       "titlebar elements",
		Bounds:      Bounds{Origin: Point{X: 120, Y: 120}, Size: Size{Width: 400, Height: 240}},
		Resizable:   true,
		MinimizeBox: true,
		Titlebar:    &TitlebarOptions{AppearsTransparent: true},
		ShouldClose: func() bool { closeRequests.Add(1); return false },
	}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(v *interactView, _ *Context[interactView]) {
			v.build = func(*interactView, *Window, *Context[interactView]) AnyElement {
				caption := func(id string, left float32, area WindowControlArea) AnyElement {
					return Div().ID(id).Absolute().Left(Px(left)).Top0().W(Px(40)).H(Px(32)).
						Occlude().WindowControlArea(area).IntoElement()
				}
				return Div().Relative().SizeFull().Child(
					Div().ID("titlebar").Absolute().Left0().Top0().W(Px(400)).H(Px(32)).
						WindowControlArea(WindowControlAreaDrag).
						OnHover(func(h *bool, _ *Window, _ *App) {
							if *h {
								hovered.Add(1)
							}
						}).
						Child(caption("min", 280, WindowControlAreaMin)).
						Child(caption("max", 320, WindowControlAreaMax)).
						Child(caption("close", 360, WindowControlAreaClose)),
				).IntoElement()
			}
		}))
	})
	if err != nil {
		t.Fatalf("OpenWindowView: %v", err)
	}
	t.Cleanup(func() {
		h.runForegroundSync(func() { procDestroyWindow.Call(window.Handle().Hwnd()) })
	})
	if window.Handle().Visible() {
		t.Fatal("the test window must stay hidden")
	}
	hwnd := window.Handle().Hwnd()
	screen := func(x, y float32) uintptr {
		var p point
		h.runForegroundSync(func() {
			scale := window.Handle().record().scale
			p = point{x: int32(x * scale), y: int32(y * scale)}
			procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
		})
		return makePointsLParam(p.x, p.y)
	}
	checks := []struct {
		x, y float32
		want uintptr
	}{
		{100, 20, htCaption},
		{290, 20, htMinButton},
		{330, 20, htMaxButton},
		{370, 20, htClose},
		{100, 100, htClient},
	}
	for _, c := range checks {
		if got := sendTitlebar(hwnd, wmNCHitTest, 0, screen(c.x, c.y)); got != c.want {
			t.Errorf("WM_NCHITTEST at (%v,%v) = %d, want %d", c.x, c.y, got, c.want)
		}
	}

	sendTitlebar(hwnd, wmNcMouseMove, htCaption, screen(100, 20))
	desktopWaitFor(t, "the non-client move to hover the titlebar", func() bool { return hovered.Load() > 0 })

	sendTitlebar(hwnd, wmNcLButtonDown, htClose, screen(370, 20))
	sendTitlebar(hwnd, wmNcLButtonUp, htClose, screen(370, 20))
	desktopWaitFor(t, "the close button to request a close", func() bool { return closeRequests.Load() == 1 })

	// A release on another button than the press does nothing.
	sendTitlebar(hwnd, wmNcLButtonDown, htClose, screen(370, 20))
	sendTitlebar(hwnd, wmNcLButtonUp, htCaption, screen(100, 20))
	time.Sleep(50 * time.Millisecond)
	if n := closeRequests.Load(); n != 1 {
		t.Fatalf("close requests after a mismatched release = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Real-window tests (visible windows; GPUI_REAL_INPUT_TESTS=1)
// ---------------------------------------------------------------------------

// TestTitlebarRealHiddenTitlebarMaximize shows a hidden-titlebar window
// and maximizes it: restored, the client starts at the window top;
// maximized, it starts one frame thickness lower, which is the monitor
// work area's top (events.rs handle_calc_client_size's maximized arm).
func TestTitlebarRealHiddenTitlebarMaximize(t *testing.T) {
	realinput.Require(t) // shows, maximizes and restores a window
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{
		Title:       "titlebar real maximize",
		Bounds:      Bounds{Origin: Point{X: 120, Y: 120}, Size: Size{Width: 420, Height: 260}},
		Resizable:   true,
		MinimizeBox: true,
		Show:        true,
		Titlebar:    &TitlebarOptions{AppearsTransparent: true},
	})
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	defer handle.Close()
	desktopWaitFor(t, "the window to be visible", handle.Visible)
	clientTop, windowTop := titlebarClientTop(t, h, handle.Hwnd())
	if clientTop != windowTop {
		t.Errorf("restored: client top %d, window top %d; want equal", clientTop, windowTop)
	}
	handle.Zoom()
	desktopWaitFor(t, "the window to maximize", func() bool { m, _ := handle.IsMaximized(); return m })
	time.Sleep(100 * time.Millisecond)
	clientTop, windowTop = titlebarClientTop(t, h, handle.Hwnd())
	var frame int32
	h.runForegroundSync(func() { frame = frameThicknessX(handle.record().windowDPI()) })
	if clientTop != windowTop+frame {
		t.Errorf("maximized: client top %d, window top %d, frame %d; want client top = window top + frame", clientTop, windowTop, frame)
	}
	handle.Zoom()
	desktopWaitFor(t, "the window to restore", func() bool { m, _ := handle.IsMaximized(); return !m })
}

// screenPixel reads one composed screen pixel (GetPixel on the screen
// DC) as 8-bit RGB.
func screenPixel(x, y int32) (r, g, b uint8) {
	dc, _, _ := procGetDCTitlebar.Call(0)
	defer procReleaseDCTitlebar.Call(0, dc)
	c, _, _ := procGetPixelTitlebar.Call(dc, uintptr(x), uintptr(y))
	return uint8(c), uint8(c >> 8), uint8(c >> 16)
}

// TestTitlebarRealBackdropShowsThrough checks that the desktop behind a
// DirectComposition window shows through a transparent frame. A white
// opaque window sits behind; on top, a hidden-titlebar window presents
// a fully transparent clear with WindowBackgroundTransparent and then
// WindowBackgroundBlurred. With WS_EX_NOREDIRECTIONBITMAP the composed
// pixel at its center is light (the white window, plain or blurred).
// The same window without DirectComposition is logged for comparison:
// the audit's risk was that its redirection surface shows black.
func TestTitlebarRealBackdropShowsThrough(t *testing.T) {
	realinput.Require(t) // shows windows and reads the screen
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	// Manual: WaitForCompletion polls the submission itself, which the
	// paced renderer's worker would retire first (stale handle).
	renderer := newManualRenderer(t)
	bounds := Bounds{Origin: Point{X: 160, Y: 160}, Size: Size{Width: 360, Height: 240}}
	open := func(title string, opts WindowOptions) *Window {
		opts.Title = title
		opts.Bounds = bounds
		opts.Show = true
		w, err := app.OpenWindow(opts)
		if err != nil {
			t.Fatalf("OpenWindow(%s): %v", title, err)
		}
		t.Cleanup(w.Close)
		desktopWaitFor(t, title+" to be visible", w.Alive)
		return w
	}
	present := func(w *Window, color Color) {
		sub, err := renderer.PresentClear(w, color)
		if err != nil {
			t.Fatalf("PresentClear: %v", err)
		}
		if _, err := sub.WaitForCompletion(5 * time.Second); err != nil {
			t.Fatalf("WaitForCompletion: %v", err)
		}
		if _, err := sub.Retire(); err != nil {
			t.Fatalf("Retire: %v", err)
		}
	}
	center := func(w *Window) (int32, int32) {
		var p point
		h.runForegroundSync(func() {
			rec := w.Handle().record()
			p = point{x: rec.clientW / 2, y: rec.clientH / 2}
			procClientToScreen.Call(w.Handle().Hwnd(), uintptr(unsafe.Pointer(&p)))
		})
		return p.x, p.y
	}
	settle := func() { time.Sleep(400 * time.Millisecond) }

	backdrop := open("titlebar backdrop (white)", WindowOptions{})
	present(backdrop, Color{R: 1, G: 1, B: 1, A: 1})
	settle()

	for _, direct := range []bool{false, true} {
		glass := open("titlebar glass", WindowOptions{
			Resizable:         true,
			Titlebar:          &TitlebarOptions{AppearsTransparent: true},
			DirectComposition: direct,
			Activate:          true,
		})
		for _, mode := range []WindowBackgroundAppearance{WindowBackgroundTransparent, WindowBackgroundBlurred} {
			glass.SetBackgroundAppearance(mode)
			present(glass, Color{})
			settle()
			x, y := center(glass)
			r, g, b := screenPixel(x, y)
			t.Logf("DirectComposition=%v mode=%d: composed pixel at center = (%d,%d,%d)", direct, mode, r, g, b)
			if direct && (int(r)+int(g)+int(b))/3 < 128 {
				t.Errorf("DirectComposition mode %d: center pixel (%d,%d,%d) is dark; the white window behind should show through", mode, r, g, b)
			}
		}
		glass.Close()
		desktopWaitFor(t, "the glass window to close", func() bool { return !glass.Alive() })
	}
}
