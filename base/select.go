package base

// Port of U:select.rs: the unstyled controlled select root.
//
// browser-core ticket 72: the picker's Escape dismissal is the core's
// close watcher (ticket 22) — Chrome's customizable <select> picker is a
// popover=auto element (select_type.cc:611: popover_->setAttribute(
// kPopoverAttr, "auto")) shown with the select as its invoker
// (select_type.cc:774: popover_->ShowPopoverInternal(select_, ...)), so
// Escape signals the picker's close watcher wherever focus sits inside
// it, and the hide refocuses the select (PopoverElementForAppearanceBase
// ::HidePopoverInternal's kFocusPreviousElement arm, select_type.cc:
// 182-196 — closeSelect's trigger refocus). The app-owned picker
// surface gets the popover's light dismiss by rendering it through
// Base's Popover (the top layer); a plain Popup is the app's own. The
// Select's escape key binding is gone with the watcher
// (U:select.rs:15-28).

import "github.com/hoangvu12/gpui-go/gpui"

// selectContext is the Select key context (U:select.rs:13).
const selectContext = "Select"

// selectInit binds the select keys (U:select.rs:15-28). Escape is not
// bound: the open state's close watcher owns it (see the file comment).
func selectInit(app *gpui.App) {
	ctx := selectContext
	app.BindKeys(
		gpui.NewKeyBinding("up", SelectUpAction.Box(SelectUp{}), &ctx),
		gpui.NewKeyBinding("down", SelectDownAction.Box(SelectDown{}), &ctx),
		gpui.NewKeyBinding("enter", ConfirmAction.Box(Confirm{Secondary: false}), &ctx),
		gpui.NewKeyBinding("secondary-enter", ConfirmAction.Box(Confirm{Secondary: true}), &ctx),
	)
}

// The open-change handler (U:select.rs:30) is popover.go's
// OpenChangeHandler, the same signature.

// ActionHandler handles a semantic action (U:select.rs:31).
type ActionHandler = func(w *gpui.Window, app *gpui.App)

// Select is an unstyled controlled select root (U:select.rs:33-58).
//
// Applications own the trigger and popup presentation, the option
// collection, and the selected value. This root owns combobox
// accessibility semantics, keyboard opening and dismissal, and focus
// transfer between the trigger and popup content. The application marks
// its highlighted option with AriaActiveDescendant.
type Select struct {
	gpui.Styled[*Select]
	gpui.Parent[*Select]

	base               *gpui.DivElement
	id                 gpui.ElementID
	open               bool
	disabled           bool
	focusHandle        *gpui.FocusHandle
	contentFocusHandle *gpui.FocusHandle
	accessibilityLabel *string
	accessibilityValue *string
	onOpenChange       OpenChangeHandler
	keyContext         string
	onDismiss          ActionHandler
	onConfirm          ActionHandler
}

// NewSelect is Select::new (U:select.rs:61-77).
func NewSelect[I gpui.IntoElementID](id I) *Select {
	// Gap 2b: the plain upstream div() is a block.
	eid := gpui.ElementIDOf(id)
	return gpui.BindParent(gpui.BindStyled(&Select{
		base:       TestSupport(gpui.Div().ID(eid).Block()),
		id:         eid,
		keyContext: selectContext,
	}))
}

// Open sets the application-controlled open state (U:select.rs:79-83).
func (s *Select) Open(open bool) *Select {
	s.open = open
	return s
}

// Disabled prevents keyboard and accessible activation and removes the
// trigger from tab traversal (U:select.rs:85-89).
func (s *Select) Disabled(disabled bool) *Select {
	s.disabled = disabled
	return s
}

// FocusHandle supplies the focus handle for the select trigger
// (U:select.rs:91-95).
func (s *Select) FocusHandle(handle gpui.FocusHandle) *Select {
	s.focusHandle = &handle
	return s
}

// ContentFocusHandle supplies the focus handle that receives keyboard
// navigation while open (U:select.rs:97-101).
func (s *Select) ContentFocusHandle(handle gpui.FocusHandle) *Select {
	s.contentFocusHandle = &handle
	return s
}

// AccessibilityLabel sets the accessible name exposed by the controlled
// root (U:select.rs:103-107).
func (s *Select) AccessibilityLabel(label string) *Select {
	s.accessibilityLabel = &label
	return s
}

// AccessibilityValue sets the committed value exposed by the controlled
// root: a readable selection title, not the current search query or
// cursor (U:select.rs:109-115).
func (s *Select) AccessibilityValue(value string) *Select {
	s.accessibilityValue = &value
	return s
}

// OnOpenChange handles requests to update the controlled open state
// (U:select.rs:117-124).
func (s *Select) OnOpenChange(handler OpenChangeHandler) *Select {
	s.onOpenChange = handler
	return s
}

// KeyContext overrides the key context (U:select.rs:126-130,
// #[doc(hidden)]; Combobox uses it).
func (s *Select) KeyContext(keyContext string) *Select {
	s.keyContext = keyContext
	return s
}

// OnDismiss handles a dismissal, however it was requested: the Escape
// key (the open state's close watcher, browser-core ticket 72), or the
// accessible activation that closes an open control (U:select.rs:132-141).
// It runs before the controlled open state is asked to close, so a
// caller that commits its pending value on dismissal can still read
// that value here.
func (s *Select) OnDismiss(handler ActionHandler) *Select {
	s.onDismiss = handler
	return s
}

// OnConfirm handles the Confirm action while the select is open
// (U:select.rs:143-150). Confirming a closed select opens it instead.
func (s *Select) OnConfirm(handler ActionHandler) *Select {
	s.onConfirm = handler
	return s
}

// selectOverlayState is the open select's close-watcher runtime
// (browser-core ticket 72): the watcher exists while the select is
// open — created at the first open render, destroyed when it closes —
// and its close listener runs the select's close steps. Keyed state
// keeps it across the app's re-renders and releases it with the
// element.
type selectOverlayState struct {
	watcher gpui.CloseWatcher
}

// RenderOnce implements gpui.RenderOnce (U:select.rs:165-306).
func (s *Select) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return s.render(w, app).IntoElement()
}

// render builds the select's root div (U:select.rs:166-305).
func (s *Select) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	open := s.open
	disabled := s.disabled
	focusHandle := s.focusHandle
	contentFocusHandle := s.contentFocusHandle
	onOpenChange := s.onOpenChange
	onDismiss := s.onDismiss
	onConfirm := s.onConfirm

	// Every way of closing runs the same steps. A caller that tracks
	// dismissal has to see one however the popup was closed, and the
	// accessible activation closes exactly what Escape closes
	// (U:select.rs:175-193).
	closeSelect := func(w *gpui.Window, app *gpui.App) {
		if onDismiss != nil {
			onDismiss(w, app)
		}
		if onOpenChange != nil {
			onOpenChange(false, w, app)
		}
		if focusHandle != nil {
			focusHandle.Focus(w)
		}
	}
	openAndFocusContent := func(w *gpui.Window, app *gpui.App) {
		if !open && onOpenChange != nil {
			onOpenChange(true, w, app)
		}
		if contentFocusHandle != nil {
			contentFocusHandle.Focus(w)
		}
	}

	// The open state's close watcher (browser-core ticket 72 — the
	// picker's popover watcher, see the file comment): Escape not handled
	// by a listener signals the newest watcher, which runs the same
	// close steps the Cancel action used to.
	runtime := w.UseKeyedState(gpui.NamedChildElementID(s.id, "closewatcher"), app, func(w *gpui.Window, cx *gpui.Context[selectOverlayState]) selectOverlayState {
		cx.OnRelease(func(st *selectOverlayState, _ *gpui.App) { st.watcher.Destroy() })
		return selectOverlayState{}
	})
	watcher := runtime.UpdateWith(app, func(st *selectOverlayState, cx *gpui.Context[selectOverlayState]) gpui.CloseWatcher {
		if open && st.watcher == (gpui.CloseWatcher{}) {
			st.watcher = gpui.NewCloseWatcher(w)
		}
		if !open && st.watcher != (gpui.CloseWatcher{}) {
			st.watcher.Destroy()
			st.watcher = gpui.CloseWatcher{}
		}
		return st.watcher
	})
	if watcher != (gpui.CloseWatcher{}) {
		// A fresh closure every render keeps the listener current with
		// the app's handlers (the listener is replaced, not accumulated).
		watcher.OnClose(func(w *gpui.Window, app *gpui.App) { closeSelect(w, app) })
	}

	// The closed select's trigger is a control, like the UA
	// stylesheet's select rule (user-select: none, html.css:2191; a
	// browser-core 20 default-on selection opts it back out). The open
	// popup's items opt out the same way (the list below).
	d := s.base.SelectNone().Role(gpui.RoleComboBox).AriaExpanded(open)
	if s.accessibilityLabel != nil {
		d.AriaLabel(*s.accessibilityLabel)
	}
	if s.accessibilityValue != nil {
		d.AriaValue(*s.accessibilityValue)
	}
	d.KeyContext(s.keyContext)
	if focusHandle != nil && !disabled {
		d.TrackFocus(focusHandle.TabStop(true))
	}
	if !disabled {
		// Platform adapters may flatten the trigger child. Expose
		// activation on the semantic root itself (U:select.rs:214-228).
		d.OnA11yAction(gpui.A11yActionClick, func(_ *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			if open {
				closeSelect(w, app)
				return
			}
			if onOpenChange != nil {
				onOpenChange(true, w, app)
			}
			if contentFocusHandle != nil {
				contentFocusHandle.Focus(w)
			}
		})
	}
	d.OnAction(SelectUpAction, func(_ *SelectUp, w *gpui.Window, app *gpui.App) {
		if disabled {
			app.Propagate()
			return
		}
		openAndFocusContent(w, app)
		app.Propagate()
	}).OnAction(SelectDownAction, func(_ *SelectDown, w *gpui.Window, app *gpui.App) {
		if disabled {
			app.Propagate()
			return
		}
		openAndFocusContent(w, app)
		app.Propagate()
	}).OnAction(ConfirmAction, func(_ *Confirm, w *gpui.Window, app *gpui.App) {
		if disabled {
			app.Propagate()
			return
		}
		app.Propagate()
		if open {
			if onConfirm != nil {
				onConfirm(w, app)
			}
		} else if onOpenChange != nil {
			onOpenChange(true, w, app)
		}
		if contentFocusHandle != nil {
			contentFocusHandle.Focus(w)
		}
	})
	d.Extend(s.TakeChildren())
	style := s.Style()
	return StyledExtOf(d).RefineStyle(&style)
}
