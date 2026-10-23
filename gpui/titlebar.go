package gpui

// Client-drawn titlebars: the window option that hides the system
// titlebar, the element-level window control areas, and the mapping from
// a control area to the Win32 hit-test code. Ported from the pinned CE
// reference:
//
//   - crates/gpui/src/platform.rs:2799-2811 TitlebarOptions
//     (appears_transparent) and WindowOptions::is_movable.
//   - crates/gpui/src/window.rs:852-863 WindowControlArea,
//     :1109 Frame::window_control_hitboxes, :1928-1945 the
//     on_hit_test_window_control callback, :2817 start_window_move and
//     :5460 insert_window_control_hitbox.
//   - crates/gpui/src/elements/div.rs:1443 window_control_area and
//     :3068-3073 its paint-time hitbox registration.
//   - crates/gpui_windows/src/events.rs:966-1032 handle_hit_test_msg
//     (the Windows host half lives in titlebar_windows.go).
//
// Deviations, all deliberate:
//
//   - The pin hides the Windows titlebar when WindowOptions.titlebar is
//     None. Here a nil Titlebar keeps the system titlebar, so a window
//     opened without the option behaves exactly as before.
//   - The pin reads the window's last mouse hit test when Windows asks
//     WM_NCHITTEST, which lags one mouse move behind the cursor. The port
//     hit-tests the rendered frame at the WM_NCHITTEST point itself, so
//     the first move over a caption button already reports it.
//   - Like the pin, a cached view (CachedView) that is reused rather than
//     repainted does not carry its window control areas into the new
//     frame (the pin's reuse_paint does not replay
//     window_control_hitboxes). Keep a custom titlebar out of cached
//     views.

// WindowControlArea is a kind of window control area on a client-drawn
// titlebar (window.rs:852 WindowControlArea).
type WindowControlArea uint8

const (
	// WindowControlAreaDrag is an area that drags the platform window
	// (and maximizes it on double-click).
	WindowControlAreaDrag WindowControlArea = iota
	// WindowControlAreaClose is an area that closes the platform window.
	WindowControlAreaClose
	// WindowControlAreaMax is an area that maximizes or restores the
	// platform window. On Windows 11 hovering it shows the snap layouts.
	WindowControlAreaMax
	// WindowControlAreaMin is an area that minimizes the platform window.
	WindowControlAreaMin
)

// String names the area like the pin's Debug output.
func (a WindowControlArea) String() string {
	switch a {
	case WindowControlAreaDrag:
		return "Drag"
	case WindowControlAreaClose:
		return "Close"
	case WindowControlAreaMax:
		return "Max"
	case WindowControlAreaMin:
		return "Min"
	}
	return "WindowControlArea(?)"
}

// TitlebarOptions configures a window's titlebar (platform.rs:2799
// TitlebarOptions). The title itself is WindowOptions.Title; the macOS
// traffic-light position is not ported.
type TitlebarOptions struct {
	// AppearsTransparent hides the system titlebar so the application
	// draws its own (the pin's appears_transparent; on Windows its
	// hide_title_bar). The client area then extends to the top of the
	// window; the side and bottom resize borders stay, the top resize
	// border is hit-tested by the host, and elements marked with
	// WindowControlArea act as the caption and its buttons.
	AppearsTransparent bool
}

// hidesTitleBar reports whether the options hide the system titlebar
// (window.rs:456-460 hide_title_bar, except that nil keeps it; see the
// file comment).
func (o *TitlebarOptions) hidesTitleBar() bool {
	return o != nil && o.AppearsTransparent
}

// windowControlHitbox is one Frame::window_control_hitboxes entry.
type windowControlHitbox struct {
	area   WindowControlArea
	hitbox HitboxID
}

// InsertWindowControlHitbox registers a hitbox as a control area of the
// platform window for the frame under construction
// (window.rs:5460 insert_window_control_hitbox). Call it from paint.
func (w *Window) InsertWindowControlHitbox(area WindowControlArea, hitbox Hitbox) {
	if w == nil {
		panic("gpui: InsertWindowControlHitbox requires a live window")
	}
	fs := focusState(w)
	if fs.next == nil {
		panic("gpui: InsertWindowControlHitbox requires a frame under construction (paint)")
	}
	fs.next.windowControlHitboxes = append(fs.next.windowControlHitboxes,
		windowControlHitbox{area: area, hitbox: hitbox.ID()})
}

// windowControlAreaAt is the on_hit_test_window_control callback
// (window.rs:1928-1945): the first window control area, in insertion
// order, whose hitbox is in the rendered frame's hit test at position
// (window-local logical pixels). Like the pin it uses the scrollable
// ids, so a control area under an element that blocks the mouse but not
// scrolling still counts; an occluding element above it hides it.
func (w *Window) windowControlAreaAt(position Point) (WindowControlArea, bool) {
	if w == nil {
		return 0, false
	}
	frame := focusState(w).rendered
	if frame == nil || len(frame.windowControlHitboxes) == 0 {
		return 0, false
	}
	hit := frame.hitTestAt(position)
	for _, control := range frame.windowControlHitboxes {
		if hit.containsScrollable(uint64(control.hitbox)) {
			return control.area, true
		}
	}
	return 0, false
}

// StartWindowMove starts an interactive move of the window, as if the
// user pressed on the titlebar (window.rs:2817 start_window_move). Call
// it from a mouse-down listener. On Windows the move runs in the
// system's modal move loop; when it ends the window receives a left
// button release.
func (w *Window) StartWindowMove() { w.handle.StartWindowMove() }

// Win32 hit-test results used by the control-area mapping (winuser.h).
const (
	htNowhere   = 0  // HTNOWHERE
	htClient    = 1  // HTCLIENT
	htCaption   = 2  // HTCAPTION
	htMinButton = 8  // HTMINBUTTON
	htMaxButton = 9  // HTMAXBUTTON
	htClose     = 20 // HTCLOSE
)

// windowControlHitTestCode maps a window control area to the Win32
// hit-test code (events.rs:978-988). ok false means no answer: the host
// lets default processing decide (an immovable window's drag area).
func windowControlHitTestCode(area WindowControlArea, movable, resizable, minimizable bool) (code uintptr, ok bool) {
	switch area {
	case WindowControlAreaDrag:
		if movable {
			return htCaption, true
		}
		return 0, false
	case WindowControlAreaClose:
		return htClose, true
	case WindowControlAreaMax:
		switch {
		case resizable:
			return htMaxButton, true
		case movable:
			return htCaption, true
		}
		return htNowhere, true
	case WindowControlAreaMin:
		switch {
		case minimizable:
			return htMinButton, true
		case movable:
			return htCaption, true
		}
		return htNowhere, true
	}
	return 0, false
}
