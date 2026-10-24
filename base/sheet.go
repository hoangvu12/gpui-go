package base

// An unstyled modal sheet host (U:sheet.rs). Applications provide the
// overlay and the surface; the host owns Escape handling, overlay
// dismissal and close callback ordering.
//
// Go adaptations: Sheet::new allocates a fresh focus handle from &mut
// App (U:sheet.rs:50); like NewDialog (dialog.go), NewSheet takes no
// context and a sheet without FocusHandle uses a keyed-state handle
// (key "sheet-focus") released when the sheet stops drawing.
//
// Display (map.md gap 2b): the host (U:sheet.rs:48, 124, no explicit
// display) and the overlay layer (:145) are plain divs upstream, display
// Block under crates.io gpui 0.2.2 (the pin defaults to Flex,
// R:style.rs:1024-1027), so each is built with Block() before the
// caller's style refines over it; a caller's Flex() still wins.
//
// browser-core ticket 72: the sheet is a core modal (Window.ShowModal,
// gpui/modal.go — ticket 23) rendered through a pass-through TopLayer
// entry: Escape is the entry's close watcher (ticket 22), an overlay
// press is the closedby=any light dismiss — both arrive as the sheet's
// close steps through the entry's toggle — and the deleted focus trap's
// Tab scope is the modal's inert outside. The host keeps Base's own
// Anchored window pinning and paints in place (U:sheet.rs:115-167): a
// caller embedding it under a clipping ancestor still wraps it in
// gpui.Deferred, like the pinned sheet (ticket 42).

import "github.com/hoangvu12/gpui-go/gpui"

// sheetContext is the sheet key context (U:sheet.rs:12).
const sheetContext = "Sheet"

// sheetClose requests the close, then notifies (U:sheet.rs:17-21 close).
func sheetClose(request func(*gpui.Window, *gpui.App), notify func(*gpui.ClickEvent, *gpui.Window, *gpui.App), w *gpui.Window, app *gpui.App) {
	event := gpui.ClickEvent{}
	request(w, app)
	notify(&event, w, app)
}

// Sheet is an unstyled modal sheet host (U:sheet.rs:27-43). Its Styled
// methods refine the viewport-sized host.
type Sheet struct {
	gpui.Styled[*Sheet]

	base            *gpui.DivElement
	focus           *gpui.FocusHandle
	overlayClosable bool
	overlay         *gpui.AnyElement
	surface         *gpui.AnyElement
	requestClose    func(w *gpui.Window, app *gpui.App)
	onClose         func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)
}

// NewSheet creates a sheet host (U:sheet.rs:45-59 Sheet::new).
func NewSheet() *Sheet {
	return gpui.BindStyled(&Sheet{
		base:            gpui.Div().Block(),
		overlayClosable: true,
		requestClose:    func(*gpui.Window, *gpui.App) {},
		onClose:         func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {},
	})
}

// Overlay sets the overlay (U:sheet.rs:61-64): any child value. The
// element should carry no listeners of its own: a press on it falls
// through to the inert page, where the core's light dismiss (closedby
// =any while OverlayClosable) requests the sheet's close — the
// application renders interactive, non-dismissing chrome inside the
// surface instead.
func (s *Sheet) Overlay(overlay any) *Sheet {
	e := overlayIntoElement("Sheet overlay", overlay)
	s.overlay = &e
	return s
}

// Surface sets the surface (U:sheet.rs:66-69): any child value. Give it
// an occluding hitbox (Occlude) so presses on it resolve inside the
// modal and never light-dismiss the sheet.
func (s *Sheet) Surface(surface any) *Sheet {
	e := overlayIntoElement("Sheet surface", surface)
	s.surface = &e
	return s
}

// OverlayClosable sets whether an overlay press closes the sheet
// (U:sheet.rs:71-74): the closedby policy's light-dismiss half — true
// is closedby=any (a press outside the surface closes), false is
// closedby=closerequest (Escape still closes; a press outside is
// inert).
func (s *Sheet) OverlayClosable(closable bool) *Sheet {
	s.overlayClosable = closable
	return s
}

// OnClose sets the handler notified after a close request
// (U:sheet.rs:82-88).
func (s *Sheet) OnClose(handler func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)) *Sheet {
	s.onClose = handler
	return s
}

// FocusHandle replaces the host's focus handle (U:sheet.rs:90-94,
// doc(hidden)).
func (s *Sheet) FocusHandle(focus gpui.FocusHandle) *Sheet {
	s.focus = &focus
	return s
}

// RequestClose sets the close request handler (U:sheet.rs:102-106,
// doc(hidden)).
func (s *Sheet) RequestClose(handler func(w *gpui.Window, app *gpui.App)) *Sheet {
	s.requestClose = handler
	return s
}

// sheetOverlayState is the sheet's keyed top-layer identity
// (browser-core ticket 72): the overlay handle. The sheet renders while
// mounted, so every render re-opens a core-closed entry (the app's
// unmount is the close), and the toggle listener runs the close steps.
type sheetOverlayState struct {
	overlay gpui.OverlayHandle
}

// RenderOnce implements gpui.RenderOnce (U:sheet.rs:115-167): a
// viewport-sized host anchored at the window origin carrying the
// overlay and the surface, shown as a core modal whose Escape and
// overlay-press closes run the sheet's close steps.
func (s *Sheet) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	viewport := w.ViewportSize()
	var focus gpui.FocusHandle
	if s.focus != nil {
		focus = *s.focus
	} else {
		focus = overlayKeyedFocusHandle(gpui.NameElementID("sheet-focus"), w, app)
	}
	requestClose := s.requestClose
	onClose := s.onClose
	overlayClosable := s.overlayClosable

	// The modal entry (browser-core ticket 72): one overlay identity per
	// mounted sheet, keyed on the caller's focus handle when supplied
	// (the stateless-view adapter's id path isolates by type name alone,
	// so a fixed key cannot tell two sibling sheets apart), shown as a
	// modal on the first render — the focusing steps and the inert
	// outside follow at the entry's first frame. A core close (Escape,
	// the light dismiss) hides the entry and the sheet re-opens it on its
	// next render — the sheet's open state is its presence in the tree,
	// as upstream's (U:sheet.rs:115), so the application's unmount after
	// RequestClose is the real close.
	overlayKey := gpui.NameElementID("sheet-overlay")
	if s.focus != nil {
		overlayKey = gpui.NamedIntegerElementID("sheet-focus-overlay", focus.ID())
	}
	overlayState := w.UseKeyedState(overlayKey, app, func(w *gpui.Window, cx *gpui.Context[sheetOverlayState]) sheetOverlayState {
		return sheetOverlayState{overlay: gpui.NewOverlayHandle(w)}
	})
	overlay := overlayState.Read(app, func(st *sheetOverlayState, _ *gpui.App) gpui.OverlayHandle { return st.overlay })
	if !overlay.IsOpen() {
		w.ShowModal(overlay)
	}
	overlay.OnToggle(func(isOpen bool, w *gpui.Window, app *gpui.App) {
		if isOpen {
			return
		}
		// The core closed the entry (Escape through the close watcher, or
		// an overlay press through the light dismiss): run the sheet's
		// close steps — the request, then the notification
		// (U:sheet.rs:17-21).
		sheetClose(requestClose, onClose, w, app)
	})

	host := TestSupport(s.base.ID("sheet-host")).
		Absolute().
		Top0().
		Left0().
		W(gpui.Px(viewport.Width)).
		H(gpui.Px(viewport.Height)).
		KeyContext(sheetContext).
		// The sheet's own focus slot (the DOM dialog element, always
		// focusable — ticket 23's DialogFocus): tracked on a zero-size
		// out-of-flow node, never hovered and not a tab stop, so a press
		// on the overlay falls through to the inert page for the light
		// dismiss to see it.
		Child(gpui.Div().Block().Absolute().Size0().TrackFocus(focus))
	if s.overlay != nil {
		// The overlay carries no listeners of its own (see Overlay): the
		// light dismiss owns the press. It renders below the surface.
		host.Child(*s.overlay)
	}
	if s.surface != nil {
		host.Child(*s.surface)
	}
	style := s.Style()
	host = StyledExtOf(host).RefineStyle(&style)

	policy := gpui.ClosedByCloseRequest
	if overlayClosable {
		policy = gpui.ClosedByAny
	}
	layer := gpui.TopLayer(overlay).
		PassThrough().
		DialogFocus(focus).
		ClosedBy(policy)
	// The host keeps the pinned anchored host (U:sheet.rs:123): the
	// Anchored at the origin cancels the tree position, so the sheet
	// mounts anywhere and still fills the window — in place, so a
	// clipping ancestor still requires the caller's gpui.Deferred
	// (ticket 42).
	return gpui.Anchored().Position(gpui.Point{}).Child(layer.Child(host)).IntoElement()
}
