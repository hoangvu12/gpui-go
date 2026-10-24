package base

// The unstyled modal dialog host and its parts (U:dialog.rs).
//
// Go adaptations, recorded:
//
//   - DialogHandle shares its open cell and handler between clones
//     (Rc<Cell>, Rc<RefCell>); the port shares one *DialogHandle.
//   - Dialog::new allocates a fresh focus handle from &mut App
//     (U:dialog.rs:427), which Rust drops with the element after the
//     frame. Go has no drop, so a handle allocated per render would never
//     be released: NewDialog takes no context, and a dialog without
//     FocusHandle uses a handle kept in keyed state for as long as the
//     dialog draws (key ("dialog-focus", layer)), released when it stops
//     drawing. The handle persists across frames where upstream's is
//     replaced every render; callers owning focus pass FocusHandle, as
//     upstream callers do.
//   - The parts (DialogBackdrop, DialogPopup, DialogTitle,
//     DialogDescription, DialogClose) and Dialog are gpui.RenderOnce
//     recipes with their own style and children (U:dialog.rs:186-237).
//   - The host's focus-trap container keeps the host's own id one level
//     below the trap id (focus_trap.go).
//   - ClickEvent::default() is the zero gpui.ClickEvent.
//   - Display (map.md gap 2b): every div this file builds is a plain
//     div upstream (DialogTrigger's wrapper U:dialog.rs:174, the closed
//     placeholder :532, the parts :227 and :287/:329, DialogClose :408,
//     the backdrop wrapper :607, the host :550), display Block under
//     crates.io gpui 0.2.2 (the pin defaults to Flex,
//     R:style.rs:1024-1027), so each is built with Block() before the
//     caller's style refines over it; a caller's Flex() still wins. The
//     host then sets flex itself (U:dialog.rs:560), as upstream does.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// dialogContext is the dialog key context (U:dialog.rs:18).
const dialogContext = "Dialog"

// DialogDecision decides whether a confirmation or cancellation
// proceeds (U:dialog.rs:19 Decision).
type DialogDecision = func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) bool

// DialogClosed is notified after the dialog closed (U:dialog.rs:20
// Closed).
type DialogClosed = func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// DialogOpenChange is notified when the dialog's open state is requested
// to change (U:dialog.rs:23 OpenChange).
type DialogOpenChange = func(open bool, reason DialogChangeReason, w *gpui.Window, app *gpui.App)

// DialogChangeReason is why a dialog's open state changed
// (U:dialog.rs:25-32).
type DialogChangeReason uint8

const (
	// DialogChangeReasonTriggerPress is a press on a dialog trigger.
	DialogChangeReasonTriggerPress DialogChangeReason = iota
	// DialogChangeReasonBackdropPress is a press on the backdrop.
	DialogChangeReasonBackdropPress
	// DialogChangeReasonCancel is the Cancel action (Escape).
	DialogChangeReasonCancel
	// DialogChangeReasonConfirm is the Confirm action (Enter).
	DialogChangeReasonConfirm
	// DialogChangeReasonImperative is DialogHandle.Open/Close.
	DialogChangeReasonImperative
)

// String renders the reason like the derived Debug.
func (r DialogChangeReason) String() string {
	switch r {
	case DialogChangeReasonTriggerPress:
		return "TriggerPress"
	case DialogChangeReasonBackdropPress:
		return "BackdropPress"
	case DialogChangeReasonCancel:
		return "Cancel"
	case DialogChangeReasonConfirm:
		return "Confirm"
	}
	return "Imperative"
}

// DialogHandle is a shared open state for a dialog and its triggers
// (U:dialog.rs:34-38). Go adaptation (browser-core ticket 72): the
// handle also carries a sequence number, the app-side identity the
// dialog's top-layer entry keys its overlay handle on — sibling
// dialogs render through the stateless-view adapter, whose element id
// path isolates by type name alone, so the layer number cannot tell
// two dialogs apart.
type DialogHandle struct {
	open         bool
	onOpenChange DialogOpenChange
	seq          uint64
}

// dialogHandleSeq assigns each DialogHandle its sequence number.
var dialogHandleSeq uint64

// NewDialogHandle creates a handle (U:dialog.rs:41-46).
func NewDialogHandle(open bool) *DialogHandle {
	dialogHandleSeq++
	return &DialogHandle{open: open, seq: dialogHandleSeq}
}

// IsOpen reports the open state (U:dialog.rs:47-49).
func (h *DialogHandle) IsOpen() bool { return h.open }

// Open opens the dialog (U:dialog.rs:50-52).
func (h *DialogHandle) Open(w *gpui.Window, app *gpui.App) {
	h.setOpen(true, DialogChangeReasonImperative, w, app)
}

// Close closes the dialog (U:dialog.rs:53-55).
func (h *DialogHandle) Close(w *gpui.Window, app *gpui.App) {
	h.setOpen(false, DialogChangeReasonImperative, w, app)
}

// setOpen changes the state, reports it and refreshes the window
// (U:dialog.rs:56-71).
func (h *DialogHandle) setOpen(open bool, reason DialogChangeReason, w *gpui.Window, app *gpui.App) {
	if h.open == open {
		return
	}
	h.open = open
	if callback := h.onOpenChange; callback != nil {
		callback(open, reason, w, app)
	}
	w.Refresh()
}

// dialogRequestOpenChange routes a change through the handle, or to the
// handler of an uncontrolled dialog (U:dialog.rs:74-87).
func dialogRequestOpenChange(handle *DialogHandle, callback DialogOpenChange, open bool, reason DialogChangeReason, w *gpui.Window, app *gpui.App) {
	if handle != nil {
		handle.setOpen(open, reason, w, app)
	} else if callback != nil {
		callback(open, reason, w, app)
	}
}

// dialogInit binds the dialog keys (U:dialog.rs:89-94 init). Escape is
// not bound: the modal entry's close watcher owns it (browser-core
// ticket 22 — the dialog registers an always-cancelable watcher,
// html_dialog_element.cc:536-545, whose cancel forwards to the dialog's
// cancel listener, ticket 23's OverlayHandle.OnCancel).
func dialogInit(app *gpui.App) {
	ctx := dialogContext
	app.BindKeys(
		gpui.NewKeyBinding("enter", ConfirmAction.Box(Confirm{Secondary: false}), &ctx),
	)
}

// Dialog is the unstyled modal host owning focus, keyboard actions,
// dismissal and callback ordering (U:dialog.rs:122-143). Its Styled
// methods refine the host, which centers a popup in normal flow by
// default; its Parent methods add host children.
type Dialog struct {
	gpui.Styled[*Dialog]
	gpui.Parent[*Dialog]

	focus           *gpui.FocusHandle
	role            gpui.Role
	layer           int
	keyboard        bool
	overlayClosable bool
	topmost         bool
	backdrop        *gpui.AnyElement
	popup           *gpui.AnyElement
	onOK            DialogDecision
	onCancel        DialogDecision
	onClose         DialogClosed
	requestClose    func(confirmed bool, w *gpui.Window, app *gpui.App)
	handle          *DialogHandle
	open            bool
	onOpenChange    DialogOpenChange
}

// NewDialog creates an open dialog host (U:dialog.rs:424-445
// Dialog::new; see the file comment for its focus handle).
func NewDialog() *Dialog {
	d := &Dialog{
		role:            gpui.RoleDialog,
		keyboard:        true,
		overlayClosable: true,
		topmost:         true,
		onOK:            func(*gpui.ClickEvent, *gpui.Window, *gpui.App) bool { return true },
		onCancel:        func(*gpui.ClickEvent, *gpui.Window, *gpui.App) bool { return true },
		onClose:         func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {},
		requestClose:    func(bool, *gpui.Window, *gpui.App) {},
		open:            true,
	}
	gpui.BindStyled(d)
	gpui.BindParent(d)
	return d
}

// OnOK sets the confirm decision (U:dialog.rs:97-103).
func (d *Dialog) OnOK(handler DialogDecision) *Dialog {
	d.onOK = handler
	return d
}

// OnCancel sets the cancel decision; returning false vetoes the
// dismissal (U:dialog.rs:105-111).
func (d *Dialog) OnCancel(handler DialogDecision) *Dialog {
	d.onCancel = handler
	return d
}

// OnClose sets the handler notified after closing (U:dialog.rs:113-119).
func (d *Dialog) OnClose(handler DialogClosed) *Dialog {
	d.onClose = handler
	return d
}

// Open sets the open state of a dialog without a handle
// (U:dialog.rs:446-449).
func (d *Dialog) Open(open bool) *Dialog {
	d.open = open
	return d
}

// Handle controls the dialog through a shared handle
// (U:dialog.rs:450-456).
func (d *Dialog) Handle(handle *DialogHandle) *Dialog {
	if d.onOpenChange != nil {
		handle.onOpenChange = d.onOpenChange
	}
	d.handle = handle
	return d
}

// OnOpenChange sets the open-change handler, also on the handle
// (U:dialog.rs:457-467).
func (d *Dialog) OnOpenChange(handler DialogOpenChange) *Dialog {
	if d.handle != nil {
		d.handle.onOpenChange = handler
	}
	d.onOpenChange = handler
	return d
}

// Backdrop sets the backdrop rendered behind the popup
// (U:dialog.rs:469-472): any child value.
func (d *Dialog) Backdrop(element any) *Dialog {
	e := overlayIntoElement("Dialog backdrop", element)
	d.backdrop = &e
	return d
}

// Popup sets the popup (U:dialog.rs:473-476): any child value.
func (d *Dialog) Popup(element any) *Dialog {
	e := overlayIntoElement("Dialog popup", element)
	d.popup = &e
	return d
}

// CloseOnEscape sets whether the keyboard actions apply
// (U:dialog.rs:477-480). With the core modal (browser-core ticket 72)
// this is the closedby policy's keyboard half: false maps onto
// ClosedByNone — neither Escape nor a backdrop press may close, which
// is the DOM's closedby=none (the old combination “no Escape, backdrop
// still dismisses” is not expressible in the attribute).
func (d *Dialog) CloseOnEscape(value bool) *Dialog {
	d.keyboard = value
	return d
}

// CloseOnBackdropPress sets whether a backdrop press dismisses
// (U:dialog.rs:481-484): the closedby policy's light-dismiss half —
// true (with CloseOnEscape) is closedby=any, otherwise closedby=
// closerequest (Escape closes, a press outside is inert).
func (d *Dialog) CloseOnBackdropPress(value bool) *Dialog {
	d.overlayClosable = value
	return d
}

// withRole sets the host role (U:dialog.rs:489-492, pub(crate)).
func (d *Dialog) withRole(role gpui.Role) *Dialog {
	d.role = role
	return d
}

// Layer sets the stacking layer and whether it is topmost
// (U:dialog.rs:493-498, doc(hidden)).
func (d *Dialog) Layer(index int, topmost bool) *Dialog {
	d.layer = index
	d.topmost = topmost
	return d
}

// FocusHandle replaces the host's focus handle (U:dialog.rs:499-503,
// doc(hidden)).
func (d *Dialog) FocusHandle(value gpui.FocusHandle) *Dialog {
	d.focus = &value
	return d
}

// RequestClose sets the close request handler; confirmed reports a
// confirmation (U:dialog.rs:504-511, doc(hidden)).
func (d *Dialog) RequestClose(handler func(confirmed bool, w *gpui.Window, app *gpui.App)) *Dialog {
	d.requestClose = handler
	return d
}

// dialogOverlayState is the dialog's keyed top-layer identity
// (browser-core ticket 72): the overlay handle. The app-controlled
// open flag is not mirrored here — the entry's own open state is the
// sync target (the popover's overlay sync pattern).
type dialogOverlayState struct {
	overlay gpui.OverlayHandle
}

// dialogOverlayPath is the per-render close-path cell: which gesture
// closed the entry, set by the close gestures and read by the entry's
// toggle listener (the same render's closures), reset to the core
// default after it runs.
type dialogOverlayPath struct {
	path dialogClosePath
}

func (p *dialogOverlayPath) get() dialogClosePath  { return p.path }
func (p *dialogOverlayPath) set(v dialogClosePath) { p.path = v }

// dialogClosePath is which gesture closed the entry, read by the
// toggle listener that runs the close steps (the reason reported to
// the app and the confirmed flag for RequestClose).
type dialogClosePath uint8

const (
	// dialogCloseCore is the core's own close: Escape (the entry's
	// close watcher) or a backdrop press (the closedby=any light
	// dismiss) — both arrive as the dialog's cancel event, the DOM's
	// single cancel, so both report DialogChangeReasonCancel.
	dialogCloseCore dialogClosePath = iota
	// dialogCloseConfirm is the Confirm action's close.
	dialogCloseConfirm
	// dialogCloseCancel is a dispatched Cancel action's close
	// (DialogClose, AlertDialogCancel).
	dialogCloseCancel
	// dialogCloseApp is the app's own state closing (a handle Close or
	// an open flag flip): the app already knows.
	dialogCloseApp
)

// RenderOnce implements gpui.RenderOnce (U:dialog.rs:525-646): an open
// dialog renders as a core modal (Window.ShowModal, ticket 23) through a
// pass-through top-layer entry — a viewport-sized host centering its
// popup over a backdrop, handling Cancel and Confirm — with the dialog
// focusing steps, the inert outside, the focus restore and the Escape
// and light-dismiss closes owned by the core.
func (d *Dialog) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	open := d.open
	if d.handle != nil {
		open = d.handle.IsOpen()
	}
	requestClose := d.requestClose
	cancel := d.onCancel
	confirm := d.onOK
	closed := d.onClose
	overlayClosable := d.overlayClosable && d.topmost
	handle := d.handle
	change := d.onOpenChange
	viewport := w.ViewportSize()
	var focus gpui.FocusHandle
	if d.focus != nil {
		focus = *d.focus
	} else {
		focus = overlayKeyedFocusHandle(gpui.NamedIntegerElementID("dialog-focus", uint64(d.layer)), w, app)
	}

	// The close-path cell: which gesture closed the entry, read by the
	// toggle listener below. A per-render cell: the close gestures and
	// the listener all come from the same render's closures, and the
	// queued toggle drains before the next render replaces them.
	path := &dialogOverlayPath{}
	// The modal entry (browser-core ticket 72): one overlay identity per
	// dialog, keyed on the app-side identity — the shared handle's
	// sequence number, else the caller's focus handle, else the stacking
	// layer (the stateless-view adapter's id path isolates by type name
	// alone, so the layer number cannot tell two sibling dialogs apart;
	// two handle-less, focus-less dialogs on one layer keep the pin's
	// keyed-state approximation and share the entry — pass a Handle or a
	// FocusHandle to keep them apart).
	overlayKey := gpui.NamedIntegerElementID("dialog-overlay", uint64(d.layer))
	if d.handle != nil {
		overlayKey = gpui.NamedIntegerElementID("dialog-handle-overlay", d.handle.seq)
	} else if d.focus != nil {
		overlayKey = gpui.NamedIntegerElementID("dialog-focus-overlay", d.focus.ID())
	}
	overlayState := w.UseKeyedState(overlayKey, app, func(w *gpui.Window, cx *gpui.Context[dialogOverlayState]) dialogOverlayState {
		return dialogOverlayState{overlay: gpui.NewOverlayHandle(w)}
	})
	overlay := overlayState.Read(app, func(s *dialogOverlayState, _ *gpui.App) gpui.OverlayHandle { return s.overlay })
	// The app's open state is the entry's source of truth (the popover's
	// overlay sync): show on the open transition (the focusing steps and
	// the inert outside follow at the entry's first frame), hide on the
	// close transition so the core's close path — the restore and the
	// toggle — runs for app-driven closes too, and re-show when the app
	// keeps rendering a core-closed dialog open.
	if open && !overlay.IsOpen() {
		w.ShowModal(overlay)
	}
	if !open && overlay.IsOpen() {
		path.set(dialogCloseApp)
		overlay.Hide()
	}
	// The cancel decision is the dialog's cancel event (the DOM dialog's
	// oncancel — ticket 23's OnCancel forwarding): both the watcher's
	// close request (Escape, the light dismiss) and the dispatched Cancel
	// action run it, and a veto (PreventDefault) keeps the dialog open.
	overlay.OnCancel(func(e *gpui.CloseWatcherEvent, w *gpui.Window, app *gpui.App) {
		event := gpui.ClickEvent{}
		if !cancel(&event, w, app) {
			e.PreventDefault()
		}
	})
	// The close steps run once, from the entry's toggle (the DOM's
	// toggle task): the path says which gesture closed it — the core
	// (Escape or the light dismiss) reports Cancel, Confirm reports
	// Confirm, and the app's own close reports nothing (the app knows).
	overlay.OnToggle(func(isOpen bool, w *gpui.Window, app *gpui.App) {
		if isOpen {
			return
		}
		defer path.set(dialogCloseCore)
		event := gpui.ClickEvent{}
		switch path.get() {
		case dialogCloseApp:
			return
		case dialogCloseConfirm:
			dialogRequestOpenChange(handle, change, false, DialogChangeReasonConfirm, w, app)
			requestClose(true, w, app)
			closed(&event, w, app)
			return
		case dialogCloseCancel:
			dialogRequestOpenChange(handle, change, false, DialogChangeReasonCancel, w, app)
			requestClose(false, w, app)
			closed(&event, w, app)
			return
		}
		// dialogCloseCore: Escape and the backdrop press are the same
		// cancel event in the DOM, so the reason is Cancel
		// (DialogChangeReasonBackdropPress no longer occurs).
		dialogRequestOpenChange(handle, change, false, DialogChangeReasonCancel, w, app)
		requestClose(false, w, app)
		closed(&event, w, app)
	})
	if !open {
		return gpui.Div().Block().IntoElement()
	}

	host := TestSupport(gpui.Div().Block().ID(gpui.NamedIntegerElementID("dialog-host", uint64(d.layer)))).
		Absolute().
		Top0().
		Left0().
		W(gpui.Px(viewport.Width)).
		H(gpui.Px(viewport.Height)).
		// A popup in normal flow lands centered; the refined style below
		// lets the caller lay the host out differently.
		Flex().
		ItemsCenter().
		JustifyCenter().
		Role(d.role).
		// The dialog's own focus slot (the DOM dialog element, always
		// focusable — ticket 23's DialogFocus): tracked on a zero-size
		// out-of-flow node, never hovered and not a tab stop, so the
		// focusing steps' dialog-itself fallback reaches it without a
		// viewport-sized hitbox — a click on the backdrop must fall
		// through to the inert page for the light dismiss to see it.
		Child(gpui.Div().Block().Absolute().Size0().TrackFocus(focus))
	if d.keyboard {
		host.KeyContext(dialogContext)
	}
	host.OnAction(CancelAction, func(_ *Cancel, w *gpui.Window, app *gpui.App) {
		event := gpui.ClickEvent{}
		if cancel(&event, w, app) {
			path.set(dialogCloseCancel)
			overlay.Hide()
		}
	}).OnAction(ConfirmAction, func(_ *Confirm, w *gpui.Window, app *gpui.App) {
		event := gpui.ClickEvent{}
		if confirm(&event, w, app) {
			path.set(dialogCloseConfirm)
			overlay.Hide()
		}
	})
	if d.backdrop != nil {
		host.Child(gpui.Div().Block().
			// The backdrop covers the host, so a caller's absolute
			// surface has a box to fill. It carries no listeners of its
			// own: a press on it falls through to the inert page, where
			// the core's light dismiss (closedby=any) requests the
			// dialog's close through its cancel event.
			Absolute().
			Inset0().
			Child(*d.backdrop))
	}
	if d.popup != nil {
		host.Child(*d.popup)
	}
	host.Extend(d.TakeChildren())
	style := d.Style()
	host = StyledExtOf(host).RefineStyle(&style)

	// The closedby policy (ticket 23): any allows the light dismiss and
	// Escape; closerequest (the modal's auto default) keeps Escape;
	// none — CloseOnEscape(false) — allows neither.
	policy := gpui.ClosedByCloseRequest
	if overlayClosable {
		policy = gpui.ClosedByAny
	}
	if !d.keyboard {
		policy = gpui.ClosedByNone
	}
	layer := gpui.TopLayer(overlay).
		PassThrough().
		DialogFocus(focus).
		ClosedBy(policy)
	// The host keeps Base's own window pinning (the pinned Deferred
	// host, U:dialog.rs:548-643): the Anchored at the origin cancels the
	// tree position, so the dialog mounts anywhere and still fills the
	// window; the deferred draw sits above the page like the old host.
	return gpui.Deferred(gpui.Anchored().Position(gpui.Point{}).Child(layer.Child(host))).
		Priority(10 + d.layer).
		IntoElement()
}

// DialogTrigger is an unstyled trigger owning pointer activation for
// opening a dialog (U:dialog.rs:145-151).
type DialogTrigger struct {
	trigger gpui.AnyElement
	open    func(w *gpui.Window, app *gpui.App)
	handle  *DialogHandle
}

// NewDialogTrigger wraps trigger (U:dialog.rs:153-160): any child value.
func NewDialogTrigger(trigger any) *DialogTrigger {
	return &DialogTrigger{
		trigger: overlayIntoElement("DialogTrigger", trigger),
		open:    func(*gpui.Window, *gpui.App) {},
	}
}

// Handle opens the handle on press (U:dialog.rs:161-164).
func (t *DialogTrigger) Handle(handle *DialogHandle) *DialogTrigger {
	t.handle = handle
	return t
}

// OnOpen sets the handler run on press (U:dialog.rs:166-169).
func (t *DialogTrigger) OnOpen(open func(w *gpui.Window, app *gpui.App)) *DialogTrigger {
	t.open = open
	return t
}

// RenderOnce implements gpui.RenderOnce (U:dialog.rs:172-184).
func (t *DialogTrigger) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogTriggerElement(t.trigger, t.handle, t.open)
}

// dialogTriggerElement is the shared trigger wrapper of DialogTrigger
// and AlertDialogTrigger (U:dialog.rs:173-183, U:alert_dialog.rs:87-97).
func dialogTriggerElement(trigger gpui.AnyElement, handle *DialogHandle, open func(*gpui.Window, *gpui.App)) gpui.AnyElement {
	return gpui.Div().Block().
		OnMouseDown(gpui.MouseButtonLeft, func(_ *gpui.MouseDownEvent, w *gpui.Window, app *gpui.App) {
			if handle != nil {
				handle.setOpen(true, DialogChangeReasonTriggerPress, w, app)
			}
			open(w, app)
			app.StopPropagation()
		}).
		Child(trigger).
		IntoElement()
}

// dialogPartElement renders one unstyled part: a div with the part id
// that optionally keeps presses on itself (the popup over a dismissing
// backdrop), with the children and the part's style (the dialog_part!
// and alert_part! render, U:dialog.rs:225-235, U:alert_dialog.rs:45-54).
func dialogPartElement(id string, occlude bool, style gpui.StyleRefinement, children []gpui.AnyElement) gpui.AnyElement {
	d := gpui.Div().Block().ID(id)
	if occlude {
		d.Occlude()
	}
	d.Extend(children)
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}

// DialogBackdrop is the unstyled backdrop part rendered behind a dialog
// popup (U:dialog.rs:239-243).
type DialogBackdrop struct {
	gpui.Styled[*DialogBackdrop]
	gpui.Parent[*DialogBackdrop]
}

// NewDialogBackdrop creates the part.
func NewDialogBackdrop() *DialogBackdrop {
	b := &DialogBackdrop{}
	gpui.BindStyled(b)
	return gpui.BindParent(b)
}

// RenderOnce implements gpui.RenderOnce.
func (b *DialogBackdrop) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("dialog-backdrop", false, b.Style(), b.TakeChildren())
}

// DialogPopup is the unstyled popup part containing dialog content
// (U:dialog.rs:245-250). It keeps presses on itself from falling through
// to the dismissing backdrop.
type DialogPopup struct {
	gpui.Styled[*DialogPopup]
	gpui.Parent[*DialogPopup]
}

// NewDialogPopup creates the part.
func NewDialogPopup() *DialogPopup {
	p := &DialogPopup{}
	gpui.BindStyled(p)
	return gpui.BindParent(p)
}

// RenderOnce implements gpui.RenderOnce.
func (p *DialogPopup) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("dialog-popup", true, p.Style(), p.TakeChildren())
}

// DialogTitle is the unstyled title slot of a dialog surface
// (U:dialog.rs:252-292).
type DialogTitle struct {
	gpui.Styled[*DialogTitle]
	gpui.Parent[*DialogTitle]
}

// NewDialogTitle creates the part.
func NewDialogTitle() *DialogTitle {
	t := &DialogTitle{}
	gpui.BindStyled(t)
	return gpui.BindParent(t)
}

// RenderOnce implements gpui.RenderOnce.
func (t *DialogTitle) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("dialog-title", false, t.Style(), t.TakeChildren())
}

// DialogDescription is the unstyled descriptive-content slot of a dialog
// surface (U:dialog.rs:294-334).
type DialogDescription struct {
	gpui.Styled[*DialogDescription]
	gpui.Parent[*DialogDescription]
}

// NewDialogDescription creates the part.
func NewDialogDescription() *DialogDescription {
	d := &DialogDescription{}
	gpui.BindStyled(d)
	return gpui.BindParent(d)
}

// RenderOnce implements gpui.RenderOnce.
func (d *DialogDescription) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("dialog-description", false, d.Style(), d.TakeChildren())
}

// dialogCloseAnchor is the focus node a DialogClose activation
// dispatches from, filled at render and read when clicked
// (U:dialog.rs:342-344).
type dialogCloseAnchor struct {
	handle *gpui.FocusHandle
}

// DialogClose is a wrapper that dispatches the dialog's Cancel action
// when activated (U:dialog.rs:336-345).
type DialogClose struct {
	gpui.Styled[*DialogClose]
	gpui.Parent[*DialogClose]

	trigger *gpui.AnyElement
	anchor  *dialogCloseAnchor
}

// NewDialogClose creates the wrapper (U:dialog.rs:347-355).
func NewDialogClose() *DialogClose {
	c := &DialogClose{anchor: &dialogCloseAnchor{}}
	gpui.BindStyled(c)
	return gpui.BindParent(c)
}

// Trigger styles a button with the accessible name "Close" and cancel
// activation (U:dialog.rs:357-369). The supplied button already supports
// pointer, keyboard and accessibility activation; build only supplies
// presentation and returns any child value. The wrapper does not also
// handle clicks when a trigger is supplied.
func (c *DialogClose) Trigger(build func(button *Button) any) *DialogClose {
	anchor := c.anchor
	button := NewButton("close").
		AccessibilityLabel("Close").
		OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { dialogCloseActivate(anchor, w, app) })
	element := overlayIntoElement("DialogClose trigger", build(button))
	c.trigger = &element
	return c
}

// dialogCloseActivate dispatches Cancel from the control's own focus
// node, so it reaches the dialog the control sits in whatever holds focus
// at that moment (U:dialog.rs:371-383).
func dialogCloseActivate(anchor *dialogCloseAnchor, w *gpui.Window, app *gpui.App) {
	handle := anchor.handle
	if handle != nil {
		handle.DispatchAction(CancelAction.Box(Cancel{}), w, app)
		return
	}
	w.DispatchBoxed(CancelAction.Box(Cancel{}), app)
}

// RenderOnce implements gpui.RenderOnce (U:dialog.rs:400-421).
func (c *DialogClose) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	anchor := overlayKeyedFocusHandle(gpui.NameElementID("dialog-close-anchor"), w, app)
	c.anchor.handle = &anchor
	cell := c.anchor
	d := gpui.Div().Block().
		ID("dialog-close").
		// A zero-size, out-of-flow node that tracks the anchor: it is
		// never hovered, so it takes no focus on its own and does not
		// enter the Tab order, but it sits inside this dialog's dispatch
		// path.
		Child(gpui.Div().Block().Absolute().Size0().TrackFocus(anchor))
	if c.trigger == nil {
		d.OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { dialogCloseActivate(cell, w, app) })
	} else {
		d.Child(*c.trigger)
	}
	d.Extend(c.TakeChildren())
	style := c.Style()
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}

// overlayKeyedFocusHandle returns a focus handle kept in keyed state
// (Window::use_keyed_state(key, cx, |_, cx| cx.focus_handle()),
// U:dialog.rs:402-405), released with the state when no element with
// the key draws (the pinned Entity<FocusHandle> drop).
func overlayKeyedFocusHandle(key gpui.ElementID, w *gpui.Window, app *gpui.App) gpui.FocusHandle {
	entity := w.UseKeyedState(key, app, func(_ *gpui.Window, cx *gpui.Context[gpui.FocusHandle]) gpui.FocusHandle {
		cx.OnRelease(func(h *gpui.FocusHandle, _ *gpui.App) { h.Release() })
		return cx.App().FocusHandle()
	})
	return entity.Read(app, func(h *gpui.FocusHandle, _ *gpui.App) gpui.FocusHandle { return *h })
}
