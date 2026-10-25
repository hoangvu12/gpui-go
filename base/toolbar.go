package base

// An unstyled toolbar with roving arrow-key focus (U:toolbar.rs), and
// its semantic ToolbarGroup.
//
// Display: Toolbar's base is a plain `div().id(id)` (U:toolbar.rs:57),
// display Block under Base's gpui-ce 0.2.2 but Flex under the core pin
// (R:style.rs:1024-1027, map.md gap 2b); NewToolbar sets Block before
// the caller's style refines over it. ToolbarGroup sets flex itself
// (U:toolbar.rs:258).

import "github.com/hoangvu12/gpui-go/gpui"

// toolbarMaxFocusAttempts bounds the tab-stop hops when wrapping focus
// back into the toolbar (U:toolbar.rs:11-14 MAX_FOCUS_ATTEMPTS).
const toolbarMaxFocusAttempts = 100

// Toolbar is an unstyled container that groups controls and owns roving
// keyboard focus among them (U:toolbar.rs:16-51). Left and Right move
// focus to the previous or next focusable descendant, wrapping at either
// end; Disabled suppresses only the toolbar's own navigation. The
// container is not itself a tab stop. It exposes RoleToolbar with a
// horizontal orientation.
type Toolbar struct {
	gpui.Component[*Toolbar]

	id       gpui.ElementID
	base     *gpui.DivElement
	disabled bool
}

// NewToolbar creates a toolbar with the given element id
// (U:toolbar.rs:53-63 Toolbar::new).
func NewToolbar[I gpui.IntoElementID](id I) *Toolbar {
	eid := gpui.ElementIDOf(id)
	// Block: the 0.2.2 plain-div default (see the file comment).
	base := gpui.Div().ID(eid).Block()
	return gpui.BindComponent(&Toolbar{id: eid, base: base}, base)
}

// Disabled disables the toolbar's own keyboard navigation; hosted
// controls are not disabled (U:toolbar.rs:65-70).
func (t *Toolbar) Disabled(disabled bool) *Toolbar {
	t.disabled = disabled
	return t
}

// toolbarStepFocus steps window focus once (U:toolbar.rs:73-79).
func toolbarStepFocus(w *gpui.Window, forward bool) {
	if forward {
		w.FocusNext()
	} else {
		w.FocusPrev()
	}
}

// toolbarMoveFocus moves focus to the next (or previous) focusable
// element inside container by walking the window's tab stops until focus
// re-enters the container or returns to where it started
// (U:toolbar.rs:81-112).
func toolbarMoveFocus(container gpui.FocusHandle, forward bool, w *gpui.Window) {
	start := w.Focused()
	if !start.Ok {
		return
	}
	movedInside := func() bool {
		f := w.Focused()
		return container.ContainsFocused(w) && f.Ok && f.Handle.ID() != start.Handle.ID()
	}

	toolbarStepFocus(w, forward)
	if movedInside() {
		return
	}
	for range toolbarMaxFocusAttempts {
		toolbarStepFocus(w, forward)
		if movedInside() {
			return
		}
		if f := w.Focused(); f.Ok && f.Handle.ID() == start.Handle.ID() {
			break
		}
	}
	w.Focus(start.Handle)
}

// toolbarHandleKeyDown maps left/right to roving focus and stops
// propagation (U:toolbar.rs:114-135).
func toolbarHandleKeyDown(disabled bool, container gpui.FocusHandle, event *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
	if disabled {
		return
	}
	var forward bool
	switch event.Keystroke.Key {
	case "left":
		forward = false
	case "right":
		forward = true
	default:
		return
	}
	toolbarMoveFocus(container, forward, w)
	app.StopPropagation()
}

// RenderOnce implements gpui.RenderOnce (U:toolbar.rs:157-190).
func (t *Toolbar) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

// render builds the toolbar's root div (U:toolbar.rs:158-189). The focus
// handle survives across frames in keyed state so containment checks and
// the key handler refer to the same node; it is not a tab stop.
func (t *Toolbar) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	style := t.Style()
	disabled := t.disabled
	state := w.UseKeyedState(t.id, app, func(_ *gpui.Window, cx *gpui.Context[gpui.FocusHandle]) gpui.FocusHandle {
		return cx.App().FocusHandle().TabStop(false)
	})
	focusHandle := state.Read(app, func(h *gpui.FocusHandle, _ *gpui.App) gpui.FocusHandle { return *h })

	d := t.base.
		TrackFocus(focusHandle).
		Role(gpui.RoleToolbar).
		AriaOrientation(gpui.A11yOrientationHorizontal).
		// A toolbar's items are controls, like the UA stylesheet's
		// button rule (user-select: none, html.css:850/:904; browser-
		// core 20's default-on selection opts them back out).
		SelectNone().
		OnKeyDown(func(event *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
			toolbarHandleKeyDown(disabled, focusHandle, event, w, app)
		})
	d.Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}

// ToolbarGroup is a semantic subgroup of items within a Toolbar
// (U:toolbar.rs:192-212). It carries no behavior: the toolbar's roving
// focus traverses its items like direct children. It cannot disable its
// children; that is the group owner's job.
type ToolbarGroup struct {
	gpui.Component[*ToolbarGroup]

	base  *gpui.DivElement
	label *string
}

// NewToolbarGroup creates a group with the given element id
// (U:toolbar.rs:214-222).
func NewToolbarGroup[I gpui.IntoElementID](id I) *ToolbarGroup {
	base := gpui.Div().Flex().ID(id)
	return gpui.BindComponent(&ToolbarGroup{base: base}, base)
}

// Label sets the accessible name announced for the group, e.g. "History"
// (U:toolbar.rs:224-228).
func (g *ToolbarGroup) Label(label string) *ToolbarGroup {
	g.label = &label
	return g
}

// RenderOnce implements gpui.RenderOnce (U:toolbar.rs:251-265).
func (g *ToolbarGroup) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return g.render(w, app).IntoElement()
}

// render builds the group's root div: one inline row segment centered on
// the bar's cross axis (U:toolbar.rs:252-264).
func (g *ToolbarGroup) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	style := g.Style()
	d := g.base.Flex().ItemsCenter().Role(gpui.RoleGroup)
	if g.label != nil {
		d.AriaLabel(*g.label)
	}
	d.Extend(g.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
