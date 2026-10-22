package base

// An unstyled modal sheet host (U:sheet.rs). Applications provide the
// overlay and the surface; the host owns focus trapping, Escape
// handling, overlay dismissal and close callback ordering.
//
// Go adaptations: Sheet::new allocates a fresh focus handle from &mut
// App (U:sheet.rs:50); like NewDialog (dialog.go), NewSheet takes no
// context and a sheet without FocusHandle uses a keyed-state handle
// (key "sheet-focus") released when the sheet stops drawing. The host's focus-trap container keeps the
// host's own id one level below the trap id (focus_trap.go).
//
// Display (map.md gap 2b): the host (U:sheet.rs:48, 124, no explicit
// display) and the overlay layer (:145) are plain divs upstream, display
// Block under crates.io gpui 0.2.2 (the pin defaults to Flex,
// R:style.rs:1024-1027), so each is built with Block() before the
// caller's style refines over it; a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// sheetContext is the sheet key context (U:sheet.rs:12).
const sheetContext = "Sheet"

// sheetClose requests the close, then notifies (U:sheet.rs:17-21 close).
func sheetClose(request func(*gpui.Window, *gpui.App), notify func(*gpui.ClickEvent, *gpui.Window, *gpui.App), w *gpui.Window, app *gpui.App) {
	event := gpui.ClickEvent{}
	request(w, app)
	notify(&event, w, app)
}

// sheetInit binds the sheet keys (U:sheet.rs:23-25 init).
func sheetInit(app *gpui.App) {
	ctx := sheetContext
	app.BindKeys(gpui.NewKeyBinding("escape", CancelAction.Box(Cancel{}), &ctx))
}

// Sheet is an unstyled modal sheet host (U:sheet.rs:27-43). Its Styled
// methods refine the viewport-sized host.
type Sheet struct {
	gpui.Styled[*Sheet]

	base               *gpui.DivElement
	focus              *gpui.FocusHandle
	overlayInteractive bool
	overlayClosable    bool
	dismissBeforeY     *float32
	overlay            *gpui.AnyElement
	surface            *gpui.AnyElement
	requestClose       func(w *gpui.Window, app *gpui.App)
	onClose            func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)
}

// NewSheet creates a sheet host (U:sheet.rs:45-59 Sheet::new).
func NewSheet() *Sheet {
	return gpui.BindStyled(&Sheet{
		base:               gpui.Div().Block(),
		overlayInteractive: true,
		overlayClosable:    true,
		requestClose:       func(*gpui.Window, *gpui.App) {},
		onClose:            func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {},
	})
}

// Overlay sets the overlay (U:sheet.rs:61-64): any child value.
func (s *Sheet) Overlay(overlay any) *Sheet {
	e := overlayIntoElement("Sheet overlay", overlay)
	s.overlay = &e
	return s
}

// Surface sets the surface (U:sheet.rs:66-69): any child value.
func (s *Sheet) Surface(surface any) *Sheet {
	e := overlayIntoElement("Sheet surface", surface)
	s.surface = &e
	return s
}

// OverlayClosable sets whether an overlay press closes the sheet
// (U:sheet.rs:71-74).
func (s *Sheet) OverlayClosable(closable bool) *Sheet {
	s.overlayClosable = closable
	return s
}

// OverlayInteractive sets whether the overlay takes presses at all
// (U:sheet.rs:76-80, doc(hidden)).
func (s *Sheet) OverlayInteractive(interactive bool) *Sheet {
	s.overlayInteractive = interactive
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

// DismissBeforeY ignores overlay presses above y (U:sheet.rs:96-100,
// doc(hidden)).
func (s *Sheet) DismissBeforeY(y float32) *Sheet {
	s.dismissBeforeY = &y
	return s
}

// RequestClose sets the close request handler (U:sheet.rs:102-106,
// doc(hidden)).
func (s *Sheet) RequestClose(handler func(w *gpui.Window, app *gpui.App)) *Sheet {
	s.requestClose = handler
	return s
}

// RenderOnce implements gpui.RenderOnce (U:sheet.rs:115-167): a
// viewport-sized focus-trap host anchored at the window origin, closing
// on Cancel and on an overlay press.
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

	host := TestSupport(s.base.ID("sheet-host")).
		Absolute().
		Top0().
		Left0().
		W(gpui.Px(viewport.Width)).
		H(gpui.Px(viewport.Height)).
		KeyContext(sheetContext).
		TrackFocus(focus).
		OnAction(CancelAction, func(_ *Cancel, w *gpui.Window, app *gpui.App) {
			app.Propagate()
			sheetClose(requestClose, onClose, w, app)
		})
	if s.overlay != nil {
		dismissBeforeY := s.dismissBeforeY
		overlayInteractive := s.overlayInteractive
		overlayClosable := s.overlayClosable
		layer := gpui.Div().Block().Absolute().Inset0()
		if overlayInteractive {
			layer.OnAnyMouseDown(func(event *gpui.MouseDownEvent, w *gpui.Window, app *gpui.App) {
				if !overlayInteractive {
					return
				}
				if dismissBeforeY != nil && event.Position.Y < *dismissBeforeY {
					return
				}
				app.StopPropagation()
				if overlayClosable && event.Button == gpui.MouseButtonLeft {
					sheetClose(requestClose, onClose, w, app)
				}
			})
		}
		host.Child(*s.overlay).Child(layer)
	}
	if s.surface != nil {
		host.Child(*s.surface)
	}
	style := s.Style()
	host = StyledExtOf(host).RefineStyle(&style)
	trap := FocusTrapElementOf(host).FocusTrap("sheet", focus)
	return gpui.Anchored().Position(gpui.Point{}).Child(trap).IntoElement()
}
