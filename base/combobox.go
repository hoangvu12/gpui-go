package base

// Port of U:combobox.rs: the unstyled controlled combobox root, a Select
// with its own key context.

import "github.com/hoangvu12/gpui-go/gpui"

// comboboxContext is the Combobox key context (U:combobox.rs:14).
const comboboxContext = "Combobox"

// comboboxInit binds the combobox keys (U:combobox.rs:16-28). Escape
// is not bound: the open state's close watcher owns it (browser-core
// ticket 72 — the Select's picker watcher, see select.go).
func comboboxInit(app *gpui.App) {
	ctx := comboboxContext
	app.BindKeys(
		gpui.NewKeyBinding("up", SelectUpAction.Box(SelectUp{}), &ctx),
		gpui.NewKeyBinding("down", SelectDownAction.Box(SelectDown{}), &ctx),
		gpui.NewKeyBinding("enter", ConfirmAction.Box(Confirm{Secondary: false}), &ctx),
		gpui.NewKeyBinding("secondary-enter", ConfirmAction.Box(Confirm{Secondary: true}), &ctx),
	)
}

// Combobox is an unstyled controlled combobox root
// (U:combobox.rs:33-49). Applications own the trigger, popup, searchable
// collection, selection and appearance; this root owns combobox
// semantics and keyboard focus transfer.
type Combobox struct {
	gpui.Styled[*Combobox]
	gpui.Parent[*Combobox]

	id                 gpui.ElementID
	open               bool
	disabled           bool
	focusHandle        *gpui.FocusHandle
	contentFocusHandle *gpui.FocusHandle
	onOpenChange       OpenChangeHandler
	onConfirm          ActionHandler
	onDismiss          ActionHandler
}

// NewCombobox is Combobox::new (U:combobox.rs:52-65).
func NewCombobox[I gpui.IntoElementID](id I) *Combobox {
	return gpui.BindParent(gpui.BindStyled(&Combobox{id: gpui.ElementIDOf(id)}))
}

// Open is U:combobox.rs:67-70.
func (c *Combobox) Open(open bool) *Combobox {
	c.open = open
	return c
}

// Disabled is U:combobox.rs:72-75.
func (c *Combobox) Disabled(disabled bool) *Combobox {
	c.disabled = disabled
	return c
}

// FocusHandle is U:combobox.rs:77-80.
func (c *Combobox) FocusHandle(handle gpui.FocusHandle) *Combobox {
	c.focusHandle = &handle
	return c
}

// ContentFocusHandle is U:combobox.rs:82-85.
func (c *Combobox) ContentFocusHandle(handle gpui.FocusHandle) *Combobox {
	c.contentFocusHandle = &handle
	return c
}

// OnOpenChange is U:combobox.rs:87-93.
func (c *Combobox) OnOpenChange(handler OpenChangeHandler) *Combobox {
	c.onOpenChange = handler
	return c
}

// OnConfirm handles the Confirm action while the combobox is open
// (U:combobox.rs:95-99).
func (c *Combobox) OnConfirm(handler ActionHandler) *Combobox {
	c.onConfirm = handler
	return c
}

// OnDismiss handles a dismissal requested through the Cancel action; it
// runs before the controlled open state is asked to close
// (U:combobox.rs:101-109).
func (c *Combobox) OnDismiss(handler ActionHandler) *Combobox {
	c.onDismiss = handler
	return c
}

// RenderOnce implements gpui.RenderOnce: it renders a Select with the
// Combobox key context (U:combobox.rs:124-146).
func (c *Combobox) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	s := NewSelect(c.id).
		Open(c.open).
		Disabled(c.disabled).
		KeyContext(comboboxContext)
	if c.focusHandle != nil {
		s.FocusHandle(*c.focusHandle)
	}
	if c.contentFocusHandle != nil {
		s.ContentFocusHandle(*c.contentFocusHandle)
	}
	if c.onOpenChange != nil {
		s.OnOpenChange(c.onOpenChange)
	}
	if c.onConfirm != nil {
		s.OnConfirm(c.onConfirm)
	}
	if c.onDismiss != nil {
		s.OnDismiss(c.onDismiss)
	}
	s.Extend(c.TakeChildren())
	style := c.Style()
	StyledExtOf(s).RefineStyle(&style)
	return s.RenderOnce(w, app)
}
