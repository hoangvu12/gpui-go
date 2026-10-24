package base

// An unstyled popup with delayed hover behavior on desktop; on iOS and
// Android tapping toggles it and tapping outside dismisses it
// (U:hover_card.rs).
//
// Go adaptations: the delays are overlayAfter tasks cancelled
// explicitly (tooltip.go); HoverCard is a gpui.RenderOnce recipe; the
// content builder returns the stateful div (Rust Stateful<Div>) the card
// adds its hover or dismissal listener to. Display (map.md gap 2b): the
// trigger wrapper (U:hover_card.rs:227) and the default trigger (:224)
// are plain divs upstream, display Block under crates.io gpui 0.2.2 (the
// pin defaults to Flex, R:style.rs:1024-1027), so they are built with
// Block(); a caller's Flex() still wins.

import (
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// overlayDelay is a pending delayed update of an overlay state: the
// background timer and the foreground continuation awaiting it (the
// pinned `cx.spawn_in(window, async move |this, cx| {
// cx.background_executor().timer(delay).await; this.update_in(...) })`,
// U:hover_card.rs:138-160). It lived in tooltip.go until browser-core
// ticket 72 deleted the tooltip provider; the hover card is its only
// user.
type overlayDelay struct {
	timer gpui.Task[struct{}]
	body  gpui.Task[struct{}]
}

// overlayAfter runs f with the entity and the window once delay has
// elapsed on the scheduler clock (the virtual clock under TestApp, wall
// time in a real application). The update is skipped when the entity
// released or the window closed (the pinned `let _ = this.update_in`).
func overlayAfter[S any](cx *gpui.Context[S], w *gpui.Window, delay time.Duration, f func(*S, *gpui.Window, *gpui.Context[S])) *overlayDelay {
	d := &overlayDelay{}
	d.timer = cx.App().Background().Spawn(func(run *gpui.TaskRun) struct{} {
		run.Sleep(delay)
		return struct{}{}
	})
	timer := d.timer
	d.body = cx.SpawnIn(w, func(this gpui.WeakEntity[S], acx *gpui.AsyncWindowContext) struct{} {
		acx.AsyncApp.Await(timer)
		if timer.Cancelled() || d.body.Cancelled() {
			return struct{}{}
		}
		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = this.TryUpdateIn(app, w, f)
		})
		return struct{}{}
	})
	return d
}

// cancel drops the pending update (the pinned Task drop). It is a no-op
// on nil.
func (d *overlayDelay) cancel() {
	if d == nil {
		return
	}
	d.timer.Cancel()
	d.body.Cancel()
}

// HoverCardContentBuilder builds the card content: a div with an id,
// which receives the card's hover (or tap dismissal) listener
// (U:hover_card.rs:12-18 ContentBuilder).
type HoverCardContentBuilder = func(state *HoverCardState, w *gpui.Window, cx *gpui.Context[HoverCardState]) *gpui.DivElement

// HoverCard is an unstyled popup with delayed hover behavior
// (U:hover_card.rs:21-33).
type HoverCard struct {
	id           gpui.ElementID
	anchor       gpui.Anchor
	tapToOpen    bool
	trigger      *gpui.AnyElement
	content      HoverCardContentBuilder
	openDelay    time.Duration
	closeDelay   time.Duration
	onOpenChange OpenChangeHandler
}

// NewHoverCard creates a hover card (U:hover_card.rs:35-47
// HoverCard::new): top-center anchor, 0.6 s open and 0.3 s close delays,
// tap-to-open on mobile.
func NewHoverCard[I gpui.IntoElementID](id I) *HoverCard {
	return &HoverCard{
		id:         gpui.ElementIDOf(id),
		anchor:     gpui.AnchorTopCenter,
		tapToOpen:  IsMobile(),
		openDelay:  600 * time.Millisecond,
		closeDelay: 300 * time.Millisecond,
	}
}

// Anchor sets the popup corner attached to the trigger
// (U:hover_card.rs:49-52).
func (h *HoverCard) Anchor(anchor gpui.Anchor) *HoverCard {
	h.anchor = anchor
	return h
}

// Trigger sets the trigger (U:hover_card.rs:54-57): any child value.
func (h *HoverCard) Trigger(trigger any) *HoverCard {
	element := overlayIntoElement("HoverCard trigger", trigger)
	h.trigger = &element
	return h
}

// Content sets the content builder (U:hover_card.rs:59-70).
func (h *HoverCard) Content(content HoverCardContentBuilder) *HoverCard {
	h.content = content
	return h
}

// OpenDelay sets the hover delay before opening (U:hover_card.rs:72-75).
func (h *HoverCard) OpenDelay(duration time.Duration) *HoverCard {
	h.openDelay = duration
	return h
}

// CloseDelay sets the delay before closing after the pointer leaves
// (U:hover_card.rs:77-80).
func (h *HoverCard) CloseDelay(duration time.Duration) *HoverCard {
	h.closeDelay = duration
	return h
}

// OnOpenChange sets the handler notified on each open change
// (U:hover_card.rs:82-88).
func (h *HoverCard) OnOpenChange(callback OpenChangeHandler) *HoverCard {
	h.onOpenChange = callback
	return h
}

// HoverCardState is the state exposed to a HoverCard content builder
// (U:hover_card.rs:91-102).
type HoverCardState struct {
	open              bool
	openDelay         time.Duration
	closeDelay        time.Duration
	onOpenChange      OpenChangeHandler
	openTask          *overlayDelay
	closeTask         *overlayDelay
	epoch             int
	isHoveringTrigger bool
	isHoveringContent bool
}

// newHoverCardState builds the state (U:hover_card.rs:105-117).
func newHoverCardState(openDelay, closeDelay time.Duration) HoverCardState {
	return HoverCardState{openDelay: openDelay, closeDelay: closeDelay}
}

// IsOpen reports whether the card is open (U:hover_card.rs:119-121).
func (s *HoverCardState) IsOpen() bool { return s.open }

// sync adopts the element's current delays and handler
// (U:hover_card.rs:123-132).
func (s *HoverCardState) sync(openDelay, closeDelay time.Duration, onOpenChange OpenChangeHandler) {
	s.openDelay = openDelay
	s.closeDelay = closeDelay
	s.onOpenChange = onOpenChange
}

// scheduleOpen opens after the open delay (U:hover_card.rs:134-146).
func (s *HoverCardState) scheduleOpen(w *gpui.Window, cx *gpui.Context[HoverCardState]) {
	s.cancelTasks()
	epoch := s.nextEpoch()
	s.openTask = overlayAfter(cx, w, s.openDelay, func(s *HoverCardState, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
		if s.epoch == epoch {
			s.setOpen(true, w, cx)
		}
	})
}

// scheduleClose closes after the close delay unless the pointer came
// back (U:hover_card.rs:148-161).
func (s *HoverCardState) scheduleClose(w *gpui.Window, cx *gpui.Context[HoverCardState]) {
	s.cancelTasks()
	epoch := s.nextEpoch()
	s.closeTask = overlayAfter(cx, w, s.closeDelay, func(s *HoverCardState, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
		if s.epoch == epoch && !s.isHoveringTrigger && !s.isHoveringContent {
			s.setOpen(false, w, cx)
		}
	})
}

// cancelTasks drops both pending tasks (U:hover_card.rs:163-167).
func (s *HoverCardState) cancelTasks() {
	s.epoch++
	s.openTask.cancel()
	s.openTask = nil
	s.closeTask.cancel()
	s.closeTask = nil
}

// nextEpoch advances the epoch (U:hover_card.rs:169-172).
func (s *HoverCardState) nextEpoch() int {
	s.epoch++
	return s.epoch
}

// setOpen changes the open state and announces it from the state, since
// the delay timers outlive the HoverCard that carried the handler
// (U:hover_card.rs:174-187).
func (s *HoverCardState) setOpen(open bool, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
	if s.open == open {
		return
	}
	s.open = open
	cx.Notify()
	if onOpenChange := s.onOpenChange; onOpenChange != nil {
		onOpenChange(open, w, cx.App())
	}
}

// onTriggerHover tracks the trigger hover (U:hover_card.rs:189-196).
func (s *HoverCardState) onTriggerHover(hovering bool, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
	s.isHoveringTrigger = hovering
	if hovering {
		s.scheduleOpen(w, cx)
	} else if !s.isHoveringContent {
		s.scheduleClose(w, cx)
	}
}

// onContentHover tracks the content hover (U:hover_card.rs:198-205).
func (s *HoverCardState) onContentHover(hovering bool, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
	s.isHoveringContent = hovering
	if hovering {
		s.cancelTasks()
	} else if !s.isHoveringTrigger {
		s.scheduleClose(w, cx)
	}
}

// Render renders nothing of its own (U:hover_card.rs:208-212).
func (s *HoverCardState) Render(*gpui.Window, *gpui.Context[HoverCardState]) gpui.AnyElement {
	return gpui.Div().Block().IntoElement()
}

// RenderOnce implements gpui.RenderOnce (U:hover_card.rs:214-267).
func (h *HoverCard) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	openDelay, closeDelay := h.openDelay, h.closeDelay
	state := w.UseKeyedState(h.id, app, func(*gpui.Window, *gpui.Context[HoverCardState]) HoverCardState {
		return newHoverCardState(openDelay, closeDelay)
	})
	state.Update(app, func(s *HoverCardState, _ *gpui.Context[HoverCardState]) {
		s.sync(h.openDelay, h.closeDelay, h.onOpenChange)
	})
	open := state.Read(app, func(s *HoverCardState, _ *gpui.App) bool { return s.IsOpen() })

	var trigger any = gpui.Div().Block()
	if h.trigger != nil {
		trigger = *h.trigger
	}
	triggerDiv := gpui.Div().Block().ID("trigger").Child(trigger)
	if h.tapToOpen {
		triggerDiv.OnClick(w.ListenerFor(state, func(s *HoverCardState, _ *gpui.ClickEvent, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
			s.cancelTasks()
			// Toggle the state rendered by this trigger even if outside
			// dismissal handles the same release first.
			s.setOpen(!open, w, cx)
		}))
	} else {
		triggerDiv.OnHover(w.ListenerFor(state, func(s *HoverCardState, hovered *bool, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
			s.onTriggerHover(*hovered, w, cx)
		}))
	}
	popup := NewPopup(h.id, triggerDiv).Anchor(h.anchor)

	if !open || h.content == nil {
		return gpui.OwnRecipe(popup)
	}
	build := h.content
	hover := w.ListenerFor(state, func(s *HoverCardState, hovered *bool, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
		s.onContentHover(*hovered, w, cx)
	})
	dismiss := w.ListenerFor(state, func(s *HoverCardState, _ *gpui.MouseUpEvent, w *gpui.Window, cx *gpui.Context[HoverCardState]) {
		s.cancelTasks()
		s.setOpen(false, w, cx)
	})
	content := state.UpdateWith(app, func(s *HoverCardState, cx *gpui.Context[HoverCardState]) *gpui.DivElement {
		return build(s, w, cx)
	})
	if h.tapToOpen {
		content.OnMouseUpOut(gpui.MouseButtonLeft, dismiss)
	} else {
		content.OnHover(hover)
	}
	return gpui.OwnRecipe(popup.Content(content))
}
