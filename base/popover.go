package base

// An unstyled popover owning trigger interaction, open state, dismissal
// and focus (U:popover.rs).
//
// Go adaptations, recorded:
//
//   - PopoverState lives in keyed element state, which is released when
//     its element stops drawing. Rust drops the state with the element;
//     the port releases nothing on collection (browser-core ticket 72:
//     the deferred-popover liveness list is gone with the top layer —
//     the overlay entry's own registration is the liveness).
//   - Window::subscribe (R:window.rs:2549) has no app-level form in the
//     core; the dismissal subscription is the state's own window-bound
//     subscription to its DismissEvent (gpui.Event.SubscribeIn), cancelled
//     when the popover closes (Rust drops it).
//   - Display (map.md gap 2b): the content surface and the empty and
//     state placeholders (U:popover.rs:160, 317, 351) are plain divs
//     upstream, display Block under crates.io gpui 0.2.2 (the pin
//     defaults to Flex, R:style.rs:1024-1027), so they are built with
//     Block() before the caller's content; the trigger container is the
//     Popup root (popup.go).
//   - Popover is a gpui.RenderOnce recipe; trigger and content builders
//     return any child value (an element, IntoElement, string, view or
//     recipe).
//   - browser-core ticket 72: the popover's Escape dismissal is the
//     entry's close watcher (ticket 22 — popover=auto entries create one
//     per show, html_element.cc:2146-2157): an unhandled Escape signals
//     the newest watcher, which hides the entry, whose toggle runs the
//     state's own close (the focus restore and the open-change
//     announcement). The popover's escape key binding is gone with it;
//     Enter and Space still dispatch Confirm (U:popover.rs:17-23).

import "github.com/hoangvu12/gpui-go/gpui"

// popoverContext is the popover key context (U:popover.rs:15).
const popoverContext = "Popover"

// popoverInit binds the popover keys (U:popover.rs:17-23). Escape is not
// bound: the top-layer entry's close watcher owns it (browser-core
// ticket 22, the popover show steps' watcher).
func popoverInit(app *gpui.App) {
	ctx := popoverContext
	app.BindKeys(
		gpui.NewKeyBinding("enter", ConfirmAction.Box(Confirm{Secondary: false}), &ctx),
		gpui.NewKeyBinding("space", ConfirmAction.Box(Confirm{Secondary: false}), &ctx),
	)
}

// OpenChangeHandler is notified when an overlay opens or closes
// (U:popover.rs:25 OpenChangeHandler, U:hover_card.rs:19).
type OpenChangeHandler = func(open bool, w *gpui.Window, app *gpui.App)

// popoverDismissEvent is the PopoverState DismissEvent emitter
// (U:popover.rs:164 EventEmitter<DismissEvent>).
var popoverDismissEvent = gpui.DefineEvent[PopoverState, gpui.DismissEvent]()

// PopoverState is the state and focus lifecycle of an unstyled popover
// (U:popover.rs:27-42). Applications own trigger/content construction,
// positioning, appearance and animation; the state owns the controlled
// open lifecycle, dismissal, focus capture and restoration, and the
// deferred-popup registration.
type PopoverState struct {
	focusHandle         gpui.FocusHandle
	trackedFocusHandle  *gpui.FocusHandle
	previousFocusHandle *gpui.FocusHandle
	open                bool
	onOpenChange        OpenChangeHandler
	dismissSubscription *gpui.Subscription
	// overlay is the popover's entry in the window's top layer
	// (browser-core ticket 21): created lazily at the first render
	// (the layer is per window), kept in step with open, and closed by
	// the core's click-based light dismiss and Escape close watcher
	// (ticket 22).
	overlay gpui.OverlayHandle
}

// NewPopoverState builds the state of a popover entity
// (U:popover.rs:44-55 PopoverState::new). Call it from the entity's
// build:
//
//	gpui.NewEntity(app, scope, func(s *base.PopoverState, cx *gpui.Context[base.PopoverState]) {
//		*s = base.NewPopoverState(false, cx)
//	})
func NewPopoverState(defaultOpen bool, cx *gpui.Context[PopoverState]) PopoverState {
	return PopoverState{focusHandle: cx.App().FocusHandle(), open: defaultOpen}
}

// IsOpen reports whether the popover is open (U:popover.rs:57-59).
func (s *PopoverState) IsOpen() bool { return s.open }

// Dismiss closes an open popover (U:popover.rs:61-65).
func (s *PopoverState) Dismiss(w *gpui.Window, cx *gpui.Context[PopoverState]) {
	if s.open {
		s.ToggleOpen(w, cx)
	}
}

// Show opens a closed popover (U:popover.rs:67-71).
func (s *PopoverState) Show(w *gpui.Window, cx *gpui.Context[PopoverState]) {
	if !s.open {
		s.ToggleOpen(w, cx)
	}
}

// SetOpen sets the open state without the focus lifecycle
// (U:popover.rs:73-77, doc(hidden)).
func (s *PopoverState) SetOpen(open bool, cx *gpui.Context[PopoverState]) {
	s.open = open
}

// SyncOpen follows a controlled open value without announcing it
// (U:popover.rs:79-82, doc(hidden)).
func (s *PopoverState) SyncOpen(open bool, w *gpui.Window, cx *gpui.Context[PopoverState]) {
	s.transitionTo(open, false, w, cx)
}

// ToggleOpen toggles the open state and announces it
// (U:popover.rs:84-87, doc(hidden)).
func (s *PopoverState) ToggleOpen(w *gpui.Window, cx *gpui.Context[PopoverState]) {
	s.transitionTo(!s.open, true, w, cx)
}

// transitionTo is the open/close lifecycle (U:popover.rs:89-134): opening
// remembers the focused handle, focuses the tracked (or own) handle and
// subscribes to dismissal; closing drops the subscription and restores
// the remembered focus when focus is still inside the popover.
func (s *PopoverState) transitionTo(opening, announce bool, w *gpui.Window, cx *gpui.Context[PopoverState]) {
	if s.open == opening {
		return
	}
	if opening {
		s.previousFocusHandle = nil
		if focused := w.Focused(); focused.Ok {
			handle := focused.Handle
			s.previousFocusHandle = &handle
		}
	}
	s.SetOpen(opening, cx)
	// browser-core ticket 21: the top-layer entry follows the state's
	// open (the handle exists once the popover has rendered). The
	// layer's toggle notification fires to the OnToggle listener, which
	// ignores opens (the state is already open) and only acts on the
	// layer's own closes.
	if s.overlay != (gpui.OverlayHandle{}) {
		if opening {
			s.overlay.Show()
		} else {
			s.overlay.Hide()
		}
	}

	if s.open {
		target := s.focusHandle
		if s.trackedFocusHandle != nil {
			target = *s.trackedFocusHandle
		}
		target.Focus(w)
		// The state's own subscription: it goes with the state, so it
		// cannot keep the state (and its deferred-popover registration)
		// alive after its trigger is gone.
		subscription := popoverDismissEvent.SubscribeIn(cx, cx.Entity(), w, func(s *PopoverState, _ gpui.Entity[PopoverState], _ *gpui.DismissEvent, w *gpui.Window, cx *gpui.Context[PopoverState]) {
			s.Dismiss(w, cx)
			w.Refresh()
		})
		s.dismissSubscription = &subscription
	} else {
		if s.dismissSubscription != nil {
			s.dismissSubscription.Cancel()
			s.dismissSubscription = nil
		}
		if previous := s.previousFocusHandle; previous != nil {
			s.previousFocusHandle = nil
			if s.focusHandle.ContainsFocused(w) {
				previous.Focus(w)
			}
		}
	}

	if announce && s.onOpenChange != nil {
		s.onOpenChange(opening, w, cx.App())
	}
	cx.Notify()
}

// TrackFocus sets the handle focused on open instead of the state's own
// (U:popover.rs:136-139, doc(hidden)); nil clears it.
func (s *PopoverState) TrackFocus(focusHandle *gpui.FocusHandle) { s.trackedFocusHandle = focusHandle }

// SetOnOpenChange sets the open-change handler (U:popover.rs:141-144,
// doc(hidden)).
func (s *PopoverState) SetOnOpenChange(handler OpenChangeHandler) { s.onOpenChange = handler }

// OnActionCancel dismisses the popover (U:popover.rs:146-149,
// doc(hidden)).
func (s *PopoverState) OnActionCancel(_ *Cancel, w *gpui.Window, cx *gpui.Context[PopoverState]) {
	s.Dismiss(w, cx)
}

// FocusHandle returns the state's focus handle (U:popover.rs:152-156
// Focusable).
func (s *PopoverState) FocusHandle(*gpui.App) gpui.FocusHandle { return s.focusHandle }

// DismissEvent returns the state's dismissal event descriptor
// (U:popover.rs:164 EventEmitter<DismissEvent>): content emits it to
// close the popover.
func (s *PopoverState) DismissEvent() gpui.Event[PopoverState, gpui.DismissEvent] {
	return popoverDismissEvent
}

// Render renders nothing of its own (U:popover.rs:158-162).
func (s *PopoverState) Render(*gpui.Window, *gpui.Context[PopoverState]) gpui.AnyElement {
	return gpui.Div().Block().IntoElement()
}

// popoverTriggerBuilder builds the trigger for the current open state
// (U:popover.rs:166 TriggerBuilder).
type popoverTriggerBuilder = func(open bool, w *gpui.Window, app *gpui.App) gpui.AnyElement

// PopoverContentBuilder builds the popover content (U:popover.rs:167-168
// ContentBuilder).
type PopoverContentBuilder = func(state *PopoverState, w *gpui.Window, cx *gpui.Context[PopoverState]) any

// Popover is an unstyled popover owning trigger interaction, open state,
// dismissal and focus (U:popover.rs:170-186). Its Styled methods style
// the trigger container: the element that takes part in the parent
// layout and whose bounds the popup is anchored to
// (U:popover.rs:293-299).
type Popover struct {
	gpui.Styled[*Popover]

	id                 gpui.ElementID
	anchor             gpui.Anchor
	offset             float32
	onPosition         func(ResolvedPosition, gpui.Bounds)
	defaultOpen        bool
	open               *bool
	trackedFocusHandle *gpui.FocusHandle
	trigger            popoverTriggerBuilder
	content            PopoverContentBuilder
	mouseButton        gpui.MouseButton
	overlayClosable    bool
	onOpenChange       OpenChangeHandler
}

// NewPopover creates a popover (U:popover.rs:188-205 Popover::new).
func NewPopover[I gpui.IntoElementID](id I) *Popover {
	return gpui.BindStyled(&Popover{
		id:              gpui.ElementIDOf(id),
		anchor:          gpui.AnchorTopLeft,
		mouseButton:     gpui.MouseButtonLeft,
		overlayClosable: true,
	})
}

// Anchor sets the popup corner attached to the trigger
// (U:popover.rs:207-210).
func (p *Popover) Anchor(anchor gpui.Anchor) *Popover {
	p.anchor = anchor
	return p
}

// Offset sets the gap from the trigger along the anchor's outward
// direction, zero by default (U:popover.rs:212-216).
func (p *Popover) Offset(offset float32) *Popover {
	p.offset = offset
	return p
}

// OnPosition observes the geometry to supply presentation such as a
// pointer arrow (U:popover.rs:218-225).
func (p *Popover) OnPosition(callback func(position ResolvedPosition, triggerBounds gpui.Bounds)) *Popover {
	p.onPosition = callback
	return p
}

// MouseButton sets the button that toggles the popover
// (U:popover.rs:227-230).
func (p *Popover) MouseButton(button gpui.MouseButton) *Popover {
	p.mouseButton = button
	return p
}

// Trigger sets a Selectable trigger, which is marked open while the
// popover is open (U:popover.rs:232-241): the open state goes through
// SelectableOpen, so a trigger keeping open and selected apart keeps its
// selection.
func (p *Popover) Trigger[T Selectable[T]](trigger T) *Popover {
	p.trigger = func(isOpen bool, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
		open := SelectableIsOpen(trigger)
		return overlayIntoElement("Popover trigger", SelectableOpen(trigger, open || isOpen))
	}
	return p
}

// TriggerWith supplies a trigger builder for higher-level presentation
// facades (U:popover.rs:243-251, doc(hidden)).
func (p *Popover) TriggerWith(trigger func(open bool, w *gpui.Window, app *gpui.App) gpui.AnyElement) *Popover {
	p.trigger = trigger
	return p
}

// DefaultOpen sets the initial open state of an uncontrolled popover
// (U:popover.rs:253-256).
func (p *Popover) DefaultOpen(open bool) *Popover {
	p.defaultOpen = open
	return p
}

// Open controls the open state (U:popover.rs:258-261).
func (p *Popover) Open(open bool) *Popover {
	p.open = &open
	return p
}

// TrackFocus focuses handle on open instead of the popover's own handle
// (U:popover.rs:263-266).
func (p *Popover) TrackFocus(handle gpui.FocusHandle) *Popover {
	p.trackedFocusHandle = &handle
	return p
}

// OverlayClosable sets whether a press outside the content dismisses it
// (U:popover.rs:268-271).
func (p *Popover) OverlayClosable(closable bool) *Popover {
	p.overlayClosable = closable
	return p
}

// OnOpenChange sets the handler notified when the user opens or closes
// the popover (U:popover.rs:273-279).
func (p *Popover) OnOpenChange(callback OpenChangeHandler) *Popover {
	p.onOpenChange = callback
	return p
}

// Content sets the content builder (U:popover.rs:281-290).
func (p *Popover) Content(content PopoverContentBuilder) *Popover {
	p.content = content
	return p
}

// RenderOnce implements gpui.RenderOnce (U:popover.rs:301-376).
func (p *Popover) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	defaultOpen := p.defaultOpen
	state := w.UseKeyedState(p.id, app, func(_ *gpui.Window, cx *gpui.Context[PopoverState]) PopoverState {
		return NewPopoverState(defaultOpen, cx)
	})
	state.Update(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) {
		s.TrackFocus(p.trackedFocusHandle)
		s.SetOnOpenChange(p.onOpenChange)
		if p.open != nil {
			s.SyncOpen(*p.open, w, cx)
		}
	})

	open := state.Read(app, func(s *PopoverState, _ *gpui.App) bool { return s.IsOpen() })
	focusHandle := state.Read(app, func(s *PopoverState, app *gpui.App) gpui.FocusHandle { return s.FocusHandle(app) })
	if p.trigger == nil {
		return gpui.Div().Block().ID("empty").IntoElement()
	}
	parentView, hasParentView := gpui.CurrentView(w)
	notifyParent := func(app *gpui.App) {
		if hasParentView {
			app.Notify(parentView)
		}
	}
	// The top-layer entry (browser-core ticket 21): created lazily at
	// the first render (the layer is per window), kept in step with the
	// state's open, and listening for the layer's light-dismiss closes.
	overlay := state.UpdateWith(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) gpui.OverlayHandle {
		if s.overlay == (gpui.OverlayHandle{}) {
			s.overlay = gpui.NewOverlayHandle(w)
		}
		if s.open != s.overlay.IsOpen() {
			// A state-only transition (SetOpen without a window, or the
			// keyed state's first read of defaultOpen) syncs the layer.
			if s.open {
				s.overlay.Show()
			} else {
				s.overlay.Hide()
			}
		}
		return s.overlay
	})
	if p.overlayClosable {
		overlay.OnToggle(func(open bool, w *gpui.Window, app *gpui.App) {
			if open {
				return
			}
			// A light-dismiss close: hide through the state so the focus
			// lifecycle and the open-change announcement run (the handle
			// is already closed; Dismiss no-ops on a closed state).
			state.Update(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) { s.Dismiss(w, cx) })
			notifyParent(app)
		})
	}
	style := p.Style()
	popup := StyledExtOf(NewPopup(p.id, p.trigger(open, w, app))).RefineStyle(&style).
		Anchor(p.anchor).
		Offset(p.offset)
	if callback := p.onPosition; callback != nil {
		popup.OnPosition(callback)
	}
	popup.KeyContext(popoverContext).
		OnAction(ConfirmAction, func(_ *Confirm, w *gpui.Window, app *gpui.App) {
			state.Update(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) { s.ToggleOpen(w, cx) })
			notifyParent(app)
		}).
		OnMouseDown(p.mouseButton, func(_ *gpui.MouseDownEvent, w *gpui.Window, app *gpui.App) {
			app.StopPropagation()
			state.Update(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) {
				if s.IsOpen() == open {
					s.ToggleOpen(w, cx)
				}
			})
			notifyParent(app)
		})
	if !open {
		return gpui.OwnRecipe(popup)
	}

	content := gpui.Div().Block().
		ID("content").
		// A popover surface is a non-modal dialog: it takes focus and is
		// dismissed with Escape, which is what this role tells assistive
		// technology to expect.
		Role(gpui.RoleDialog).
		Occlude().
		TabGroup().
		TrackFocus(focusHandle).
		KeyContext(popoverContext).
		OnAction(CancelAction, w.ListenerFor(state, func(s *PopoverState, a *Cancel, w *gpui.Window, cx *gpui.Context[PopoverState]) {
			s.OnActionCancel(a, w, cx)
		}))
	if build := p.content; build != nil {
		child := state.UpdateWith(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) any { return build(s, w, cx) })
		content.Child(child)
	}
	// invoker carries the top-layer registration's invoker name
	// (browser-core ticket 21): the popover's own id when it is a name,
	// which names the trigger container the popup root carries — the
	// element a click on keeps this popover open.
	invoker := ""
	if p.overlayClosable && p.id.Kind == gpui.ElementIDName {
		invoker = p.id.Name
	}
	popupChild := any(content)
	if p.overlayClosable {
		// browser-core ticket 21: the per-popover mousedown-out dismissal
		// is replaced by the core top layer's click-based light dismiss
		// (research/browser-parity 06 §1: the old rule dismissed the
		// parent on a press inside a nested child popover, because the
		// deferred child renders outside the parent's box, and every
		// popover decided independently). The popover registers as an
		// OverlayPopover entry with the trigger container as its invoker;
		// the layer hides the popovers above the clicked one, treating
		// the invoker and nested popovers as inside, like HTML popovers.
		popupChild = gpui.TopLayer(overlay).
			PassThrough().
			Kind(gpui.OverlayPopover).
			Invoker(invoker).
			Child(content)
	}
	return gpui.OwnRecipe(popup.Content(popupChild))
}
