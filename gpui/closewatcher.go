package gpui

// The close-watcher stack (browser-core ticket 22, spec story 69):
// Escape not handled by a key listener signals the newest close
// watcher, so the newest overlay closes regardless of where focus is.
//
// The Chromium reference (0d4dcd4f, %TEMP%\research-src\chromium):
//
//   - The default-action routing: the keydown default handler runs
//     Escape after Tab and before the navigation keys
//     (core/input/keyboard_event_manager.cc:454-464) through
//     DefaultEscapeEventHandler (:618-630), which signals the frame's
//     close-watcher stack: frame_->DomWindow()->closewatcher_stack()
//     ->EscapeKeyHandler(event) (:630).
//   - EscapeKeyHandler (core/html/closewatcher/close_watcher.cc:
//     195-202): AnyEnabledWatchers() && !event->DefaultHandled() &&
//     event->isTrusted() && keyCode == VKEY_ESCAPE → Signal(). The
//     port runs from the default-action phase (browser-core 09), so
//     the not-default-prevented gate is the phase's own, and a prior
//     default arm that consumed the key (the edit commands,
//     keyboard_event_manager.cc:442-445) returned before this arm.
//   - Signal (close_watcher.cc:204-215): the NEWEST group
//     (watcher_groups_.back()), walked in REVERSE (newest watcher
//     first), each watcher's RequestClose(kWithUserActivation)
//     stopping at the first prevented cancel; each signal spends one
//     allowed group (--allowed_groups_ when > 1).
//   - RequestClose (close_watcher.cc:280-312): a cancelable "cancel"
//     event when CancelEventCanBeCancelable (:151-155 — a free group
//     slot AND the history user activation active), else a
//     non-cancelable one; a default-prevented cancel consumes the
//     history user activation (:296-299, ConsumeHistoryUserActivation)
//     and returns false, stopping the walk; otherwise close() —
//     destroy() then the "close" event.
//   - The user-activation grouping (close_watcher.cc:92-149): each
//     user interaction gives the page ONE opportunity to create a
//     group (SetHadUserInteraction's flag — consecutive interactions
//     do not bank, close_watcher.cc:131-144's comment), and Add
//     (:92-107) opens a new group while the group count is below the
//     allowed count, else the watcher joins the newest group. The
//     port notifies the stack at the head of every real input dispatch
//     (beginPlatformInputDispatch) — Chromium notifies from
//     Frame::NotifyUserActivationInFrame (frame.cc:1041-1055).
//   - The popover link (ticket 21's TopLayer): every stack popover
//     show creates a close watcher and every hide destroys it
//     (core/html/html_element.cc:2146-2157 and :2834-2836); the
//     watcher's close event hides the popover (the
//     PopoverCloseWatcherEventListener, html_element.cc:228-247) and
//     its cancel event is left alone (the HTML popover rule — "Don't
//     do anything in response to cancel events"). The dialog slot
//     registers one too (html_dialog_element.cc:536-545).
//     PopoverHint entries create watchers like autos (the creation
//     site sits inside the stack-append branch that both kinds take);
//     popover=manual never joins the stacks and creates none. The
//     port's overlay records carry that lifecycle (toplayer.go).
//
// Divergences toward the browser, recorded per D0: the stack is the
// port's own (the pin had no close watchers — every component wired
// its own Escape listener, which only worked while focus sat inside
// the overlay); the "trusted event" gate is implicit (every platform
// dispatch is real input in the port); the port has no navigation to
// spend activation on, so the history bit is only consumed by a
// prevented cancel; and the cancel/close listeners are synchronous
// Go callbacks instead of DOM events on a script-visible target.

import "fmt"

// ---------------------------------------------------------------------------
// The window's watcher stack
// ---------------------------------------------------------------------------

// closeWatcherRecord is one watcher's persistent record: its listeners
// and its closed state (the DOM CloseWatcher object; State::kActive /
// kClosed, close_watcher.cc:84-86).
type closeWatcherRecord struct {
	id     uint64
	window *Window
	// closed is State::kClosed: a closed watcher is out of the stack
	// and no longer signals.
	closed bool
	// dispatchingCancel guards RequestClose against reentrancy through
	// its own cancel listener (the scoped_committing AutoReset,
	// close_watcher.cc:290-292).
	dispatchingCancel bool
	// onCancel is the cancel listener (the oncancel event listener).
	onCancel func(*CloseWatcherEvent, *Window, *App)
	// onClose is the close listener (the onclose event listener).
	onClose func(*Window, *App)
}

// windowCloseWatcherState is a window's close-watcher stack
// (CloseWatcher::WatcherStack): the groups of active watchers, the
// allowed group count and the user-interaction bookkeeping.
type windowCloseWatcherState struct {
	seq      uint64
	watchers map[uint64]*closeWatcherRecord
	// groups holds the active groups, bottom group first; the newest
	// group is the last (watcher_groups_).
	groups [][]*closeWatcherRecord
	// allowedGroups is the allowed group count (allowed_groups_).
	allowedGroups int
	// nextInteractionCreatesGroup is SetHadUserInteraction's
	// opportunity flag: one user interaction buys one new group, and
	// consecutive interactions do not bank
	// (next_user_interaction_creates_a_new_allowed_group_).
	nextInteractionCreatesGroup bool
	// historyActivation is the history user activation bit
	// (IsHistoryUserActivationActive's source): set by every user
	// interaction, consumed by a prevented cancel.
	historyActivation bool
}

// closeWatcherState returns (creating when absent) the window's
// watcher stack. Foreground thread only.
func closeWatcherState(w *Window) *windowCloseWatcherState {
	if w == nil {
		panic("gpui: the close-watcher stack requires a live window")
	}
	if w.closeWatchers == nil {
		w.closeWatchers = &windowCloseWatcherState{
			watchers:                    make(map[uint64]*closeWatcherRecord),
			allowedGroups:               1,
			nextInteractionCreatesGroup: true,
		}
	}
	return w.closeWatchers
}

// notifyCloseWatcherUserInteraction records one user interaction
// (SetHadUserInteraction(true), close_watcher.cc:129-137, called from
// Frame::NotifyUserActivationInFrame): the interaction buys one new
// allowed group when the last one was spent on a watcher, and the
// history user activation turns on. Windows that never created a
// watcher pay nothing.
func (w *Window) notifyCloseWatcherUserInteraction() {
	if w == nil || w.closeWatchers == nil {
		return
	}
	cs := w.closeWatchers
	if cs.nextInteractionCreatesGroup {
		cs.allowedGroups++
	}
	cs.nextInteractionCreatesGroup = false
	cs.historyActivation = true
}

// consumeCloseWatcherActivation consumes the history user activation
// (ConsumeHistoryUserActivation, close_watcher.cc:296-299's call after
// a prevented cancel).
func (cs *windowCloseWatcherState) consumeCloseWatcherActivation() {
	cs.historyActivation = false
}

// signalCloseWatchers is the Escape signal (WatcherStack::Signal,
// close_watcher.cc:204-215): the newest group, walked newest-watcher
// first, each watcher's requestClose stopping at the first prevented
// cancel; each signal spends one allowed group. It reports whether
// anything closed.
func (w *Window) signalCloseWatchers(app *App) bool {
	if w == nil || w.closeWatchers == nil || len(w.closeWatchers.groups) == 0 {
		return false
	}
	cs := w.closeWatchers
	group := append([]*closeWatcherRecord(nil), cs.groups[len(cs.groups)-1]...)
	closed := false
	for i := len(group) - 1; i >= 0; i-- {
		if !group[i].requestClose(true, app) {
			break
		}
		closed = true
	}
	if cs.allowedGroups > 1 {
		cs.allowedGroups--
	}
	return closed
}

// removeWatcher removes one watcher from its group (WatcherStack::
// Remove, close_watcher.cc:111-126): the group dies with its last
// watcher.
func (cs *windowCloseWatcherState) removeWatcher(rec *closeWatcherRecord) {
	for gi, group := range cs.groups {
		for wi, candidate := range group {
			if candidate != rec {
				continue
			}
			group = append(group[:wi], group[wi+1:]...)
			if len(group) == 0 {
				cs.groups = append(cs.groups[:gi], cs.groups[gi+1:]...)
			} else {
				cs.groups[gi] = group
			}
			return
		}
	}
}

// ---------------------------------------------------------------------------
// The public API
// ---------------------------------------------------------------------------

// CloseWatcher is a window's close-watcher identity (the DOM
// CloseWatcher object, the OverlayHandle pattern: created outside the
// view closure, stable across re-renders). A watcher joins the
// window's stack at creation; the Escape default action signals the
// newest group, and RequestClose/Close/Destroy drive the watcher
// directly. The cancel and close listeners fire synchronously from
// whatever dispatch reached them.
type CloseWatcher struct {
	id     uint64
	window *Window
}

// ID returns the watcher identity within its window.
func (h CloseWatcher) ID() uint64 { return h.id }

// NewCloseWatcher creates an active watcher in w's stack
// (new CloseWatcher(), the constructor's CreateInternal path): a new
// group while the group count is below the allowed count (the
// user-activation grouping), else the newest group.
func NewCloseWatcher(w *Window) CloseWatcher {
	if w == nil {
		panic("gpui: NewCloseWatcher requires a live window")
	}
	cs := closeWatcherState(w)
	cs.seq++
	rec := &closeWatcherRecord{id: cs.seq, window: w}
	cs.watchers[cs.seq] = rec
	// WatcherStack::Add (close_watcher.cc:92-107): a new group while
	// groups < allowed, else the newest group (allowed is at least 1,
	// so the else branch always has a group).
	if len(cs.groups) < cs.allowedGroups {
		cs.groups = append(cs.groups, []*closeWatcherRecord{rec})
	} else {
		last := cs.groups[len(cs.groups)-1]
		cs.groups[len(cs.groups)-1] = append(last, rec)
	}
	cs.nextInteractionCreatesGroup = true
	return CloseWatcher{id: cs.seq, window: w}
}

// record returns the watcher's record, or nil when the handle is zero
// or its window is gone.
func (h CloseWatcher) record() *closeWatcherRecord {
	if h.window == nil || h.window.closeWatchers == nil {
		return nil
	}
	return h.window.closeWatchers.watchers[h.id]
}

// IsClosed reports whether the watcher is out of the stack
// (CloseWatcher::IsClosed).
func (h CloseWatcher) IsClosed() bool {
	rec := h.record()
	return rec == nil || rec.closed
}

// OnCancel sets the watcher's cancel listener, fired by RequestClose
// (the oncancel event). The listener can call the event's
// PreventDefault to keep the watcher open, which also stops the
// group walk of an Escape signal. Setting it again replaces the
// listener. It reports the watcher for chaining.
func (h CloseWatcher) OnCancel(f func(e *CloseWatcherEvent, w *Window, app *App)) CloseWatcher {
	if rec := h.record(); rec != nil {
		rec.onCancel = f
	}
	return h
}

// OnClose sets the watcher's close listener, fired after the watcher
// leaves the stack (the onclose event). Setting it again replaces the
// listener. It reports the watcher for chaining.
func (h CloseWatcher) OnClose(f func(w *Window, app *App)) CloseWatcher {
	if rec := h.record(); rec != nil {
		rec.onClose = f
	}
	return h
}

// RequestClose asks the watcher to close (CloseWatcher::requestClose,
// the requestCloseForBinding path with AllowCancel::kAlways): the
// cancel listener runs with a cancelable event, and the watcher closes
// unless the listener prevented the default. It reports whether the
// watcher closed.
func (h CloseWatcher) RequestClose() bool {
	rec := h.record()
	if rec == nil {
		return false
	}
	return rec.requestClose(false, h.window.app)
}

// Close closes the watcher without a cancel event
// (CloseWatcher::close): it leaves the stack and fires the close
// listener.
func (h CloseWatcher) Close() {
	rec := h.record()
	if rec == nil {
		return
	}
	rec.close(h.window.app)
}

// Destroy removes the watcher silently (CloseWatcher::destroy): no
// cancel, no close.
func (h CloseWatcher) Destroy() {
	rec := h.record()
	if rec == nil {
		return
	}
	rec.destroy()
}

// CloseWatcherEvent is the cancel event handed to a watcher's OnCancel
// listener: the listener may call PreventDefault to keep the watcher
// open (the DOM cancel event's preventDefault; a non-cancelable event
// ignores it, close_watcher.cc:151-155's CancelEventCanBeCancelable
// gate).
type CloseWatcherEvent struct {
	// cancelable reports whether PreventDefault has an effect.
	cancelable bool
	// prevented is the listener's answer.
	prevented bool
}

// PreventDefault keeps the watcher open when the event is cancelable
// (the DOM event's preventDefault; a non-cancelable cancel event
// ignores the call).
func (e *CloseWatcherEvent) PreventDefault() {
	if e.cancelable {
		e.prevented = true
	}
}

// DefaultPrevented reports whether a listener prevented the event.
func (e *CloseWatcherEvent) DefaultPrevented() bool { return e.prevented }

// Cancelable reports whether the event can be prevented.
func (e *CloseWatcherEvent) Cancelable() bool { return e.cancelable }

// requestClose is RequestClose's engine (close_watcher.cc:280-312).
// withUserActivation selects the Escape signal's form: the cancel
// event is cancelable only when a group slot is free AND the history
// user activation is active (CancelEventCanBeCancelable); the public
// RequestClose always runs cancelable. A prevented cancel consumes
// the activation and reports false (the signal's group walk stops).
func (rec *closeWatcherRecord) requestClose(withUserActivation bool, app *App) bool {
	if rec.closed || rec.dispatchingCancel {
		return true
	}
	cs := closeWatcherState(rec.window)
	cancelable := !withUserActivation ||
		(len(cs.groups) < cs.allowedGroups && cs.historyActivation)
	event := &CloseWatcherEvent{cancelable: cancelable}
	if rec.onCancel != nil {
		rec.dispatchingCancel = true
		rec.onCancel(event, rec.window, app)
		rec.dispatchingCancel = false
	}
	if event.prevented {
		cs.consumeCloseWatcherActivation()
		return false
	}
	rec.close(app)
	return true
}

// close is CloseWatcher::close (close_watcher.cc:260-275): destroy
// (leave the stack), then fire the close listener.
func (rec *closeWatcherRecord) close(app *App) {
	if rec.closed {
		return
	}
	rec.destroy()
	if rec.onClose != nil {
		rec.onClose(rec.window, app)
	}
}

// destroy is CloseWatcher::destroy (close_watcher.cc:277-286): leave
// the stack, fire nothing.
func (rec *closeWatcherRecord) destroy() {
	if rec.closed {
		return
	}
	if cs := rec.window.closeWatchers; cs != nil {
		cs.removeWatcher(rec)
	}
	rec.closed = true
}

// String renders the handle for diagnostics.
func (h CloseWatcher) String() string {
	if h.window == nil {
		return "CloseWatcher(zero)"
	}
	return fmt.Sprintf("CloseWatcher(%d)", h.id)
}
