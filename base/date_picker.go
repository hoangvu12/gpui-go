package base

// Port of U:date_picker.rs: the unstyled controlled date-picker root.

import "github.com/hoangvu12/gpui-go/gpui"

// DatePicker is the unstyled controlled date-picker root
// (U:date_picker.rs:17-29). Applications own its trigger, calendar,
// positioning and visual presentation; Base owns focus and open/dismiss
// keyboard behavior. Its interactivity is the inner div's
// (U:date_picker.rs:66-76).
type DatePicker struct {
	gpui.Component[*DatePicker]

	base         *gpui.DivElement
	open         bool
	disabled     bool
	focusHandle  gpui.FocusHandle
	onOpenChange OpenChangeHandler
}

// NewDatePicker is DatePicker::new (U:date_picker.rs:31-42).
func NewDatePicker[I gpui.IntoElementID](id I, focusHandle gpui.FocusHandle) *DatePicker {
	// Gap 2b: the plain upstream div() is a block.
	base := TestSupport(gpui.Div().ID(gpui.ElementIDOf(id)).Block())
	return gpui.BindComponent(&DatePicker{base: base, focusHandle: focusHandle}, base)
}

// Open is U:date_picker.rs:43-46.
func (d *DatePicker) Open(open bool) *DatePicker {
	d.open = open
	return d
}

// Disabled is U:date_picker.rs:47-50.
func (d *DatePicker) Disabled(disabled bool) *DatePicker {
	d.disabled = disabled
	return d
}

// OnOpenChange is U:date_picker.rs:51-57.
func (d *DatePicker) OnOpenChange(handler OpenChangeHandler) *DatePicker {
	d.onOpenChange = handler
	return d
}

// RenderOnce implements gpui.RenderOnce (U:date_picker.rs:78-111).
func (d *DatePicker) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return d.render(w, app).IntoElement()
}

func (d *DatePicker) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	open := d.open
	disabled := d.disabled
	handler := d.onOpenChange
	root := d.base.
		Role(gpui.RoleComboBox).
		AriaExpanded(open).
		TrackFocus(d.focusHandle.TabStop(!disabled)).
		OnAction(ConfirmAction, func(_ *Confirm, w *gpui.Window, app *gpui.App) {
			// Enter opens the picker, and closes it again once the value
			// shown in the popup is the one the user wants.
			if disabled {
				app.Propagate()
			} else if handler != nil {
				handler(!open, w, app)
			}
		}).
		OnAction(CancelAction, func(_ *Cancel, w *gpui.Window, app *gpui.App) {
			if open {
				if handler != nil {
					handler(false, w, app)
				}
			} else {
				app.Propagate()
			}
		})
	style := d.Style()
	StyledExtOf(root).RefineStyle(&style)
	root.Extend(d.TakeChildren())
	return root
}
