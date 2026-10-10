package gpui

// This file is ticket24's file-drop and drag bookkeeping surface,
// ported from the pinned CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/interactive.rs:735-758 — FileDropEvent
//     (Entered{position, paths}/Pending{position}/Submit{position}/
//     Exited/Ended).
//   - crates/gpui/src/interactive.rs:694-733 — ExternalPaths (the
//     drag payload of a platform file drag) and
//     ExternalDragPayload::Files(FileDragPaths) with its
//     path/is-directory pairs.
//   - crates/gpui/src/window.rs:5804-5855 — dispatch_event's
//     FileDrop translation: the external-file drag becomes internal
//     drag state plus synthetic mouse events (Entered/Pending ->
//     MouseMove with the left button pressed; Submit -> MouseUp;
//     Exited/Ended stay FileDrop events and ride the mouse-event
//     path), with the platform-owned drag handoff
//     (restore_platform_drag / hand_restored_drag_to_platform /
//     end_platform_drag) and the refresh on Exited/Ended.
//   - crates/gpui/src/window.rs:5909-5936 —
//     promote_external_drag_to_platform: an internal drag whose mouse
//     move leaves the viewport hands the drag to the platform's
//     external drag seam when it can start one.
//   - crates/gpui/src/app.rs:2612-2672 — the drag bookkeeping:
//     active_drag, AnyDrag, start_drag/stop_drag/stop_active_drag,
//     and the platform-owned drag states
//     (hand_active_drag_to_platform / restore_platform_drag /
//     hand_restored_drag_to_platform / end_platform_drag).
//   - crates/gpui/src/platform.rs:985-992 — the PlatformWindow
//     external-drag seam: can_start_external_drag /
//     start_external_drag, whose Windows platform does NOT override
//     the trait defaults (both report false), so the pinned Windows
//     behavior never promotes an internal drag to a native one. The
//     port keeps the same seam and the same false answers.
//
// Go adaptation: the Rust enum becomes a Kind-tagged struct (this
// package's established enum convention, see ClipboardEntry), and
// Option<MouseButton> becomes a *MouseButton.

import "reflect"

// ---------------------------------------------------------------------------
// FileDropEvent (interactive.rs:735)
// ---------------------------------------------------------------------------

// FileDropEventKind selects the file-drop event variant.
type FileDropEventKind uint8

// File-drop event kinds.
const (
	// FileDropEntered: the files have entered the window.
	FileDropEntered FileDropEventKind = iota
	// FileDropPending: the files are being dragged over the window.
	FileDropPending
	// FileDropSubmit: the files have been dropped onto the window.
	FileDropSubmit
	// FileDropExited: the user has stopped dragging the files over the
	// window (a window-local exit, not notification that the platform
	// drag session ended).
	FileDropExited
	// FileDropEnded: the platform-owned drag session has ended.
	FileDropEnded
)

// String renders the kind for diagnostics and transcripts.
func (k FileDropEventKind) String() string {
	switch k {
	case FileDropEntered:
		return "Entered"
	case FileDropPending:
		return "Pending"
	case FileDropSubmit:
		return "Submit"
	case FileDropExited:
		return "Exited"
	case FileDropEnded:
		return "Ended"
	}
	return "Unknown"
}

// FileDropEvent is a file drop event from the platform, generated
// when files are dragged and dropped onto the window
// (interactive.rs:735). Position is meaningful for Entered, Pending
// and Submit (the mouse position relative to the window); Paths is
// meaningful for Entered.
type FileDropEvent struct {
	// Kind selects the variant.
	Kind FileDropEventKind
	// Position is the mouse position relative to the window.
	Position Point
	// Paths are the dragged files' paths (Entered only).
	Paths ExternalPaths
}

// NewFileDropEntered builds the Entered event.
func NewFileDropEntered(position Point, paths ExternalPaths) FileDropEvent {
	return FileDropEvent{Kind: FileDropEntered, Position: position, Paths: paths}
}

// NewFileDropPending builds the Pending event.
func NewFileDropPending(position Point) FileDropEvent {
	return FileDropEvent{Kind: FileDropPending, Position: position}
}

// NewFileDropSubmit builds the Submit event.
func NewFileDropSubmit(position Point) FileDropEvent {
	return FileDropEvent{Kind: FileDropSubmit, Position: position}
}

// NewFileDropExited builds the Exited event.
func NewFileDropExited() FileDropEvent { return FileDropEvent{Kind: FileDropExited} }

// NewFileDropEnded builds the Ended event.
func NewFileDropEnded() FileDropEvent { return FileDropEvent{Kind: FileDropEnded} }

// ExternalDragPayload is the outbound payload handed to the platform
// for a native file drag (interactive.rs:706): real on-disk paths
// paired with directory flags so the platform does not query them.
type ExternalDragPayload struct {
	// Entries are the (path, isDirectory) pairs (FileDragPaths).
	Entries [][2]any
}

// FileDragPaths builds the payload entries (FileDragPaths::new).
func FileDragPaths(paths ...struct {
	Path  string
	IsDir bool
}) ExternalDragPayload {
	payload := ExternalDragPayload{}
	for _, p := range paths {
		payload.Entries = append(payload.Entries, [2]any{p.Path, p.IsDir})
	}
	return payload
}

// FileDragPath is one (path, isDirectory) pair of an external drag
// payload, the typed form of ExternalDragPayload.Entries.
type FileDragPath struct {
	// Path is the on-disk path.
	Path string
	// IsDir reports whether the path is a directory.
	IsDir bool
}

// Paths returns the payload's paths with directory flags
// (FileDragPaths::entries).
func (p ExternalDragPayload) Paths() []FileDragPath {
	out := make([]FileDragPath, 0, len(p.Entries))
	for _, entry := range p.Entries {
		path, _ := entry[0].(string)
		isDir, _ := entry[1].(bool)
		out = append(out, FileDragPath{Path: path, IsDir: isDir})
	}
	return out
}

// ---------------------------------------------------------------------------
// The drag bookkeeping (app.rs:2600-2720)
// ---------------------------------------------------------------------------

// AnyDrag is an in-progress drag operation (app.rs AnyDrag): the
// payload value, the drag preview view, the cursor offset the preview
// should paint at, and the cursor style.
type AnyDrag struct {
	// Value is the drag payload (an ExternalPaths for a platform file
	// drag; any application value for an internal drag).
	Value any
	// View renders the drag preview. This slice leaves it nil: drag
	// preview painting belongs to the draw path (window.rs
	// draw_window's active-drag element), and the Windows platform
	// renders the file drag image through the OLE drag helper instead.
	View View
	// CursorOffset is the offset from the mouse position where the
	// preview anchors.
	CursorOffset Point
	// CursorStyle is the drag cursor style when set.
	CursorStyle *CursorStyle
	// externalPayloadSource produces the platform drag payload when an
	// internal drag is promoted to a native one (AnyDrag's
	// external_payload_source; nil means the drag cannot leave the
	// window through the platform).
	externalPayloadSource func(w *Window, app *App) *ExternalDragPayload
}

// HasActiveDrag reports whether a drag is in progress
// (App::has_active_drag).
func (a *App) HasActiveDrag() bool { return a.activeDrag != nil }

// ActiveDrag returns the current drag, when any (App::active_drag).
func (a *App) ActiveDrag() *AnyDrag { return a.activeDrag }

// StartDrag sets the current drag payload (App::start_drag). The
// window is expected to be refreshed around this call.
func (a *App) StartDrag(drag AnyDrag) {
	if a.activeDrag != nil {
		panic("gpui: StartDrag while a drag is already active (the reference's debug_assert)")
	}
	a.activeDrag = &drag
}

// StopDrag takes the current drag payload (App::stop_drag): if a
// platform drag interop is active for the given window it is
// canceled. The value is reported with ok=false when no drag was
// active.
func (a *App) StopDrag(w *Window) (value any, ok bool) {
	drag := a.activeDrag
	a.activeDrag = nil
	if drag != nil && a.platformOwnedDrag != nil &&
		a.platformOwnedDrag.sourceWindow == w.ID() &&
		a.platformOwnedDrag.state == platformDragRestored {
		a.platformOwnedDrag = nil
	}
	if drag == nil {
		return nil, false
	}
	return drag.Value, true
}

// StopActiveDrag cancels the current drag and refreshes the window
// (App::stop_active_drag). It reports whether a drag was active.
func (a *App) StopActiveDrag(w *Window) bool {
	if _, ok := a.StopDrag(w); !ok {
		return false
	}
	w.requestRefresh()
	return true
}

// cancelActiveDrag is the dispatch-path drag cancel (the pin's
// dispatch_mouse_event sets active_drag = None on an unhandled mouse
// up without touching the platform-owned drag state).
func (a *App) cancelActiveDrag() { a.activeDrag = nil }

// platformOwnedDragState is the platform drag handoff state
// (app.rs PlatformOwnedDragState).
type platformOwnedDragState uint8

const (
	// platformDragSuspended: the platform owns the drag.
	platformDragSuspended platformOwnedDragState = iota
	// platformDragRestored: the drag was restored into its source
	// window.
	platformDragRestored
)

// platformOwnedDrag tracks a drag handed to the platform
// (app.rs PlatformOwnedDrag).
type platformOwnedDrag struct {
	sourceWindow WindowID
	state        platformOwnedDragState
	drag         *AnyDrag
}

// handActiveDragToPlatform moves the active drag into the
// platform-owned state (app.rs hand_active_drag_to_platform).
func (a *App) handActiveDragToPlatform(sourceWindow WindowID) bool {
	drag := a.activeDrag
	if drag == nil {
		return false
	}
	a.activeDrag = nil
	a.platformOwnedDrag = &platformOwnedDrag{
		sourceWindow: sourceWindow,
		state:        platformDragSuspended,
		drag:         drag,
	}
	return true
}

// restorePlatformDrag restores a suspended platform drag into its
// source window's active drag (app.rs restore_platform_drag).
func (a *App) restorePlatformDrag(sourceWindow WindowID) bool {
	platformDrag := a.platformOwnedDrag
	if platformDrag == nil || platformDrag.sourceWindow != sourceWindow {
		return false
	}
	if platformDrag.state != platformDragSuspended {
		return false
	}
	a.activeDrag = platformDrag.drag
	platformDrag.state = platformDragRestored
	platformDrag.drag = nil
	return true
}

// handRestoredDragToPlatform hands a restored drag back to the
// platform when the files leave the window again (app.rs
// hand_restored_drag_to_platform).
func (a *App) handRestoredDragToPlatform(sourceWindow WindowID) bool {
	platformDrag := a.platformOwnedDrag
	if platformDrag == nil || platformDrag.sourceWindow != sourceWindow ||
		platformDrag.state != platformDragRestored {
		return false
	}
	drag := a.activeDrag
	if drag == nil {
		return false
	}
	a.activeDrag = nil
	platformDrag.state = platformDragSuspended
	platformDrag.drag = drag
	return true
}

// endPlatformDrag ends the platform-owned drag session for the
// window (app.rs end_platform_drag).
func (a *App) endPlatformDrag(sourceWindow WindowID) bool {
	if a.platformOwnedDrag == nil || a.platformOwnedDrag.sourceWindow != sourceWindow {
		return false
	}
	a.platformOwnedDrag = nil
	a.activeDrag = nil
	return true
}

// HandActiveDrag moves the active drag into the platform-owned state
// for the source window (app.rs hand_active_drag_to_platform). The
// dispatch path uses this when the platform accepts an external drag
// (the Windows seam answers CanStartExternalDrag() == false, so the
// promotion never fires there); applications and tests drive the
// state directly for the drag interop surface.
func (a *App) HandActiveDrag(sourceWindow WindowID) bool {
	return a.handActiveDragToPlatform(sourceWindow)
}

// EndPlatformDrag ends the platform-owned drag session for the
// window (app.rs end_platform_drag). It reports whether a session
// was active for that window.
func (a *App) EndPlatformDrag(sourceWindow WindowID) bool {
	return a.endPlatformDrag(sourceWindow)
}

// ---------------------------------------------------------------------------
// The platform external-drag seam (platform.rs:985-992)
// ---------------------------------------------------------------------------

// CanStartExternalDrag reports whether the window's platform supports
// starting a native drag (PlatformWindow::can_start_external_drag).
// The pinned Windows platform does not override the trait default, so
// the answer is false on Windows — an internal drag never leaves the
// window as a native drag there (the Windows source-side OLE
// machinery is deliberately absent from the pin; dragdrop_windows.go
// documents the adapter this port provides for driving and verifying
// the native lifecycle).
func (w *Window) CanStartExternalDrag() bool { return false }

// StartExternalDrag starts a native drag with the payload
// (PlatformWindow::start_external_drag). The pinned Windows platform
// does not override the trait default: it reports failure, and the
// internal drag continues unchanged.
func (w *Window) StartExternalDrag(payload ExternalDragPayload) bool { return false }

// ---------------------------------------------------------------------------
// Input dispatch (window.rs dispatch_event, bounded to the
// mouse/file-drop/gesture family)
// ---------------------------------------------------------------------------

// DispatchEventResult is the dispatch outcome (window.rs
// DispatchEventResult).
type DispatchEventResult struct {
	// Propagate reports whether the event should continue to the
	// platform's default processing.
	Propagate bool
	// DefaultPrevented reports whether a listener prevented the
	// default action.
	DefaultPrevented bool
}

// DispatchInput dispatches one platform input event of the mouse
// family through this window (window.rs Window::dispatch_event):
// *MouseMoveEvent, *MouseDownEvent, *MouseUpEvent,
// *ScrollWheelEvent, *PinchEvent and *FileDropEvent. Keyboard events
// keep their own entry point (DispatchKeyEvent, ticket13).
//
// The event runs inside one application update; the returned result
// carries the propagation and prevent-default answers.
func (w *Window) DispatchInput(event any, cx AppContext) DispatchEventResult {
	if w == nil {
		panic("gpui: DispatchInput requires a live window")
	}
	if event == nil {
		panic("gpui: DispatchInput requires a non-nil event")
	}
	app := appFrom(cx)
	var result DispatchEventResult
	app.Update(func(a *App) {
		result = w.dispatchInput(event, a)
	})
	return result
}

// dispatchInput is dispatch_event's body, bounded to the mouse family.
// Runs inside an update on the foreground thread.
func (w *Window) dispatchInput(event any, app *App) DispatchEventResult {
	is := inputState(w)

	// Track the input modality for hover suppression (window.rs
	// last_input_modality).
	oldModality := is.lastInputModality
	switch event.(type) {
	case *MouseMoveEvent, *MouseDownEvent:
		is.lastInputModality = inputModalityMouse
		is.lastInputWasKeyboard = false
	case *ScrollWheelEvent, *PinchEvent:
		is.lastInputModality = inputModalityMouse
		is.lastInputWasKeyboard = false
	}
	if is.lastInputModality != oldModality {
		w.requestRefresh()
	}

	// Handlers may stop propagation; default actions start enabled.
	app.propagateEvent = true
	is.defaultPrevented = false

	// The translation arm (window.rs:5764-5855): track the mouse
	// position and modifiers from the event, and translate the
	// external-file drag into internal drag state plus synthetic mouse
	// events.
	var translated any = event
	switch e := event.(type) {
	case *MouseMoveEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *MouseDownEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *MouseUpEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *ScrollWheelEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *PinchEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *FileDropEvent:
		translated = w.translateFileDrop(e, app)
	}

	// Dispatch the (possibly translated) event through the mouse
	// listener path. The pin routes by mouse_event()/keyboard_event();
	// every kind this entry accepts is a mouse family event, and the
	// untranslated FileDrop Exited/Ended events ride the same path
	// (FileDropEvent implements MouseEvent in the pin).
	w.dispatchMouseEvent(translated, app)

	// Must run after the move is dispatched: the platform owns the
	// gesture afterwards, so this is the last chance for drag
	// listeners to see the pointer leave and reset their state
	// (window.rs promote_external_drag_to_platform).
	w.promoteExternalDragToPlatform(translated, app)

	return DispatchEventResult{
		Propagate:        app.propagateEvent,
		DefaultPrevented: is.defaultPrevented,
	}
}

// translateFileDrop is the FileDrop arm of dispatch_event's
// translation (window.rs:5804-5855): the external file drag becomes
// internal drag state plus synthetic mouse events; Exited/Ended keep
// their FileDrop shape (and refresh the window, matching the pin).
func (w *Window) translateFileDrop(event *FileDropEvent, app *App) any {
	is := inputState(w)
	switch event.Kind {
	case FileDropEntered:
		is.mousePosition = event.Position
		sourceWindow := w.ID()
		if !app.restorePlatformDrag(sourceWindow) && app.activeDrag == nil {
			app.activeDrag = &AnyDrag{
				Value:        event.Paths,
				View:         nil, // drag preview painting is the draw path's
				CursorOffset: event.Position,
			}
		}
		left := MouseButtonLeft
		return &MouseMoveEvent{
			Position:      event.Position,
			PressedButton: &left,
			Modifiers:     Modifiers{},
		}

	case FileDropPending:
		is.mousePosition = event.Position
		left := MouseButtonLeft
		return &MouseMoveEvent{
			Position:      event.Position,
			PressedButton: &left,
			Modifiers:     Modifiers{},
		}

	case FileDropSubmit:
		// cx.activate(true): bring the window forward on the drop (the
		// host Activate path marshals to the foreground thread, which
		// is where dispatch runs; hostless test windows skip it).
		if w.handle.host != nil {
			w.handle.Activate()
		}
		is.mousePosition = event.Position
		return &MouseUpEvent{
			Button:     MouseButtonLeft,
			Position:   event.Position,
			Modifiers:  Modifiers{},
			ClickCount: 1,
		}

	case FileDropExited:
		if !app.handRestoredDragToPlatform(w.ID()) {
			app.activeDrag = nil
		}
		w.requestRefresh()
		return event

	case FileDropEnded:
		app.endPlatformDrag(w.ID())
		w.requestRefresh()
		return event
	}
	return event
}

// promoteExternalDragToPlatform hands an internal drag to the
// platform when its move leaves the viewport (window.rs
// promote_external_drag_to_platform). Dormant in this slice's element
// surface: it needs a drag whose externalPayloadSource was set (the
// drag-start listener surface, div.rs drag_listener), and the Windows
// platform seam answers CanStartExternalDrag() == false like the
// pinned trait default, so the promotion never fires on Windows.
func (w *Window) promoteExternalDragToPlatform(event any, app *App) {
	move, ok := event.(*MouseMoveEvent)
	if !ok {
		return
	}
	if move.PressedButton == nil || *move.PressedButton != MouseButtonLeft {
		return
	}
	viewport := w.dispatchViewport()
	if boundsContain(Bounds{Origin: Point{}, Size: viewport}, move.Position) {
		return
	}
	if !w.CanStartExternalDrag() {
		return
	}
	if app.activeDrag == nil || app.activeDrag.externalPayloadSource == nil {
		return
	}
	source := app.activeDrag.externalPayloadSource
	payload := source(w, app)
	if payload == nil {
		return
	}
	if w.StartExternalDrag(*payload) && app.handActiveDragToPlatform(w.ID()) {
		w.requestRefresh()
	}
}

// dispatchViewport returns the window's last drawn viewport size (the
// pin's self.viewport_size; 0-sized when the window has not drawn,
// which leaves every position outside the viewport).
func (w *Window) dispatchViewport() Size {
	ds, ok := windowDrawStates[w]
	if !ok {
		return Size{}
	}
	return ds.viewport
}

// dropListenerPayloadMatches reports whether a drag value matches the
// listener's payload type T (the pin's TypeId equality; the file-drop
// drag payload is an ExternalPaths).
func dropListenerPayloadMatches(dragValue any, payloadType reflect.Type) bool {
	if dragValue == nil {
		return false
	}
	return reflect.TypeOf(dragValue) == payloadType
}
