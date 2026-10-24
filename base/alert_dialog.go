package base

// The alert-dialog specialization of the Base modal host and its parts
// (U:alert_dialog.rs). The parts are gpui.RenderOnce recipes with their
// own style and children, rendered like the dialog parts (dialog.go
// dialogPartElement). AlertDialogTrigger shares DialogTrigger's wrapper.
// Display (map.md gap 2b): the parts, the trigger wrapper and the
// action wrappers are plain divs upstream (U:alert_dialog.rs:47, 88,
// 132, 182), display Block under crates.io gpui 0.2.2, so each is built
// with Block() before the caller's style; a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// AlertDialogBackdrop is the unstyled backdrop part
// (U:alert_dialog.rs:58).
type AlertDialogBackdrop struct {
	gpui.Styled[*AlertDialogBackdrop]
	gpui.Parent[*AlertDialogBackdrop]
}

// NewAlertDialogBackdrop creates the part (U:alert_dialog.rs:22-29).
func NewAlertDialogBackdrop() *AlertDialogBackdrop {
	b := &AlertDialogBackdrop{}
	gpui.BindStyled(b)
	return gpui.BindParent(b)
}

// RenderOnce implements gpui.RenderOnce (U:alert_dialog.rs:45-54).
func (b *AlertDialogBackdrop) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("alert-dialog-backdrop", false, b.Style(), b.TakeChildren())
}

// AlertDialogPopup is the unstyled popup part; presses on it stay off
// the backdrop behind it (U:alert_dialog.rs:59).
type AlertDialogPopup struct {
	gpui.Styled[*AlertDialogPopup]
	gpui.Parent[*AlertDialogPopup]
}

// NewAlertDialogPopup creates the part.
func NewAlertDialogPopup() *AlertDialogPopup {
	p := &AlertDialogPopup{}
	gpui.BindStyled(p)
	return gpui.BindParent(p)
}

// RenderOnce implements gpui.RenderOnce.
func (p *AlertDialogPopup) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("alert-dialog-popup", true, p.Style(), p.TakeChildren())
}

// AlertDialogTitle is the unstyled title part (U:alert_dialog.rs:60).
type AlertDialogTitle struct {
	gpui.Styled[*AlertDialogTitle]
	gpui.Parent[*AlertDialogTitle]
}

// NewAlertDialogTitle creates the part.
func NewAlertDialogTitle() *AlertDialogTitle {
	t := &AlertDialogTitle{}
	gpui.BindStyled(t)
	return gpui.BindParent(t)
}

// RenderOnce implements gpui.RenderOnce.
func (t *AlertDialogTitle) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("alert-dialog-title", false, t.Style(), t.TakeChildren())
}

// AlertDialogDescription is the unstyled description part
// (U:alert_dialog.rs:61).
type AlertDialogDescription struct {
	gpui.Styled[*AlertDialogDescription]
	gpui.Parent[*AlertDialogDescription]
}

// NewAlertDialogDescription creates the part.
func NewAlertDialogDescription() *AlertDialogDescription {
	d := &AlertDialogDescription{}
	gpui.BindStyled(d)
	return gpui.BindParent(d)
}

// RenderOnce implements gpui.RenderOnce.
func (d *AlertDialogDescription) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogPartElement("alert-dialog-description", false, d.Style(), d.TakeChildren())
}

// AlertDialogTrigger is an unstyled trigger opening an alert dialog
// (U:alert_dialog.rs:63-98).
type AlertDialogTrigger struct {
	trigger gpui.AnyElement
	open    func(w *gpui.Window, app *gpui.App)
	handle  *DialogHandle
}

// NewAlertDialogTrigger wraps trigger (U:alert_dialog.rs:70-76): any
// child value.
func NewAlertDialogTrigger(trigger any) *AlertDialogTrigger {
	return &AlertDialogTrigger{
		trigger: overlayIntoElement("AlertDialogTrigger", trigger),
		open:    func(*gpui.Window, *gpui.App) {},
	}
}

// OnOpen sets the handler run on press (U:alert_dialog.rs:77-80).
func (t *AlertDialogTrigger) OnOpen(open func(w *gpui.Window, app *gpui.App)) *AlertDialogTrigger {
	t.open = open
	return t
}

// Handle opens the handle on press (U:alert_dialog.rs:81-84).
func (t *AlertDialogTrigger) Handle(handle *DialogHandle) *AlertDialogTrigger {
	t.handle = handle
	return t
}

// RenderOnce implements gpui.RenderOnce (U:alert_dialog.rs:86-98).
func (t *AlertDialogTrigger) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return dialogTriggerElement(t.trigger, t.handle, t.open)
}

// alertDispatchElement renders a wrapper dispatching action on click
// (the alert_close_part! render, U:alert_dialog.rs:130-139, and
// AlertDialogAction, U:alert_dialog.rs:180-190). browser-core ticket
// 72: the wrapper dispatches from its own tracked focus node
// (DialogClose's anchor pattern, dialog.go dialogCloseAnchor) rather
// than the app's focused node — the modal host no longer takes the
// click's mouse focus (a viewport-sized tracking hitbox would block
// the closedby=any light dismiss), so the app's focused element
// cannot be assumed to sit on the dialog's dispatch path. The anchor
// is zero-size and never hovered: it takes no focus of its own and
// does not enter the Tab order, but it sits inside this dialog's
// dispatch path.
func alertDispatchElement(id string, action gpui.BoxedAction, style gpui.StyleRefinement, children []gpui.AnyElement, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	anchor := overlayKeyedFocusHandle(gpui.NameElementID(id), w, app)
	d := gpui.Div().Block().
		ID(id).
		Child(gpui.Div().Block().Absolute().Size0().TrackFocus(anchor)).
		OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			anchor.DispatchAction(action.Clone(), w, app)
		})
	d.Extend(children)
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}

// AlertDialogClose is a wrapper dispatching Cancel when clicked
// (U:alert_dialog.rs:143).
type AlertDialogClose struct {
	gpui.Styled[*AlertDialogClose]
	gpui.Parent[*AlertDialogClose]
}

// NewAlertDialogClose creates the wrapper (U:alert_dialog.rs:107-114).
func NewAlertDialogClose() *AlertDialogClose {
	c := &AlertDialogClose{}
	gpui.BindStyled(c)
	return gpui.BindParent(c)
}

// RenderOnce implements gpui.RenderOnce.
func (c *AlertDialogClose) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return alertDispatchElement("alert-dialog-close", CancelAction.Box(Cancel{}), c.Style(), c.TakeChildren(), w, app)
}

// AlertDialogCancel is a wrapper dispatching Cancel when clicked
// (U:alert_dialog.rs:144).
type AlertDialogCancel struct {
	gpui.Styled[*AlertDialogCancel]
	gpui.Parent[*AlertDialogCancel]
}

// NewAlertDialogCancel creates the wrapper.
func NewAlertDialogCancel() *AlertDialogCancel {
	c := &AlertDialogCancel{}
	gpui.BindStyled(c)
	return gpui.BindParent(c)
}

// RenderOnce implements gpui.RenderOnce.
func (c *AlertDialogCancel) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return alertDispatchElement("alert-dialog-cancel", CancelAction.Box(Cancel{}), c.Style(), c.TakeChildren(), w, app)
}

// AlertDialogAction is a wrapper dispatching the alert dialog's Confirm
// action when clicked (U:alert_dialog.rs:146-190).
type AlertDialogAction struct {
	gpui.Styled[*AlertDialogAction]
	gpui.Parent[*AlertDialogAction]
}

// NewAlertDialogAction creates the wrapper (U:alert_dialog.rs:153-160).
func NewAlertDialogAction() *AlertDialogAction {
	a := &AlertDialogAction{}
	gpui.BindStyled(a)
	return gpui.BindParent(a)
}

// RenderOnce implements gpui.RenderOnce (U:alert_dialog.rs:180-190).
func (a *AlertDialogAction) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return alertDispatchElement("alert-dialog-action", ConfirmAction.Box(Confirm{Secondary: false}), a.Style(), a.TakeChildren(), w, app)
}

// AlertDialog is the alert-dialog specialization of the Base modal host
// (U:alert_dialog.rs:192-290): the AlertDialog role, and a backdrop press
// does not dismiss by default. Its Styled and Parent methods reach the
// wrapped dialog (U:alert_dialog.rs:275-284).
type AlertDialog struct {
	gpui.Styled[*AlertDialog]

	dialog *Dialog
}

// NewAlertDialog creates an open alert dialog (U:alert_dialog.rs:195-202
// AlertDialog::new; its focus handle follows NewDialog's).
func NewAlertDialog() *AlertDialog {
	dialog := NewDialog().withRole(gpui.RoleAlertDialog).CloseOnBackdropPress(false)
	return gpui.BindStyledTo(&AlertDialog{dialog: dialog}, dialog.StyleMut())
}

// Open sets the open state (U:alert_dialog.rs:203-206).
func (a *AlertDialog) Open(open bool) *AlertDialog {
	a.dialog.Open(open)
	return a
}

// Handle controls the dialog through a shared handle
// (U:alert_dialog.rs:207-210).
func (a *AlertDialog) Handle(handle *DialogHandle) *AlertDialog {
	a.dialog.Handle(handle)
	return a
}

// OnOpenChange sets the open-change handler (U:alert_dialog.rs:211-217).
func (a *AlertDialog) OnOpenChange(handler DialogOpenChange) *AlertDialog {
	a.dialog.OnOpenChange(handler)
	return a
}

// Backdrop sets the backdrop (U:alert_dialog.rs:218-221).
func (a *AlertDialog) Backdrop(element any) *AlertDialog {
	a.dialog.Backdrop(element)
	return a
}

// Popup sets the popup (U:alert_dialog.rs:222-225).
func (a *AlertDialog) Popup(element any) *AlertDialog {
	a.dialog.Popup(element)
	return a
}

// CloseOnEscape sets whether the keyboard actions apply
// (U:alert_dialog.rs:226-229): the closedby policy's keyboard half —
// false is closedby=none (neither Escape nor a press outside closes).
func (a *AlertDialog) CloseOnEscape(value bool) *AlertDialog {
	a.dialog.CloseOnEscape(value)
	return a
}

// OnOK sets the confirm decision (U:alert_dialog.rs:234-240).
func (a *AlertDialog) OnOK(handler DialogDecision) *AlertDialog {
	a.dialog.OnOK(handler)
	return a
}

// OnCancel sets the cancel decision (U:alert_dialog.rs:241-247).
func (a *AlertDialog) OnCancel(handler DialogDecision) *AlertDialog {
	a.dialog.OnCancel(handler)
	return a
}

// OnClose sets the handler notified after closing
// (U:alert_dialog.rs:248-254).
func (a *AlertDialog) OnClose(handler DialogClosed) *AlertDialog {
	a.dialog.OnClose(handler)
	return a
}

// Layer sets the stacking layer (U:alert_dialog.rs:255-259,
// doc(hidden)).
func (a *AlertDialog) Layer(index int, topmost bool) *AlertDialog {
	a.dialog.Layer(index, topmost)
	return a
}

// FocusHandle replaces the host's focus handle
// (U:alert_dialog.rs:260-264, doc(hidden)).
func (a *AlertDialog) FocusHandle(value gpui.FocusHandle) *AlertDialog {
	a.dialog.FocusHandle(value)
	return a
}

// RequestClose sets the close request handler
// (U:alert_dialog.rs:265-272, doc(hidden)).
func (a *AlertDialog) RequestClose(handler func(confirmed bool, w *gpui.Window, app *gpui.App)) *AlertDialog {
	a.dialog.RequestClose(handler)
	return a
}

// Extend appends host children (U:alert_dialog.rs:280-284).
func (a *AlertDialog) Extend(elements []gpui.AnyElement) { a.dialog.Extend(elements) }

// Child adds one host child (ParentElement::child).
func (a *AlertDialog) Child(value any) *AlertDialog {
	a.dialog.Child(value)
	return a
}

// Children adds host children in order (ParentElement::children).
func (a *AlertDialog) Children(values ...any) *AlertDialog {
	a.dialog.Children(values...)
	return a
}

// RenderOnce renders the wrapped dialog (U:alert_dialog.rs:285-290).
func (a *AlertDialog) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return a.dialog.RenderOnce(w, app)
}
