package gpui

// The top layer and click-based light dismiss (browser-core ticket 21,
// spec story 68): overlays — popovers, hints and modals — live in one
// ordered top layer per window, each entry with a backdrop slot, and a
// click outside closes the popovers above the clicked one, treating the
// invoker and nested popovers as inside, as HTML popovers do.
//
// Chromium rules this ports (third_party/blink/renderer, the research
// checkout; see docs/research-index.md):
//
//   - The popover kinds: popover=auto light-dismisses and joins the
//     popover stack; popover=manual never does; popover=hint joins the
//     hint stack (html_element.cc's PopoverValueType paths). This port
//     keeps one ordered top layer and gives each entry a kind; the
//     light-dismiss walk and the hide walk skip OverlayModal and
//     OverlayPopoverManual entries, which Chromium keeps out of the
//     popover stacks.
//   - ShowPopoverInternal closes popovers until the topmost popover
//     ancestor of the one being shown, then pushes it onto the stack
//     (core/html/html_element.cc:1950-2144; the ancestor comes from DOM
//     nesting, the implicit anchor or the invoker link —
//     FindTopmostPopoverAncestor html_element.cc:2999-3135). The port
//     resolves that ancestor from the element-path tree and the invoker
//     link at the entry's first registration after Show (the frame is
//     where the port knows the tree), not synchronously inside Show.
//   - The top layer is an ordered list: Document::AddToTopLayer appends
//     (core/dom/document.cc:8703-8748) and inserts the ::backdrop
//     pseudo directly below its element (:8746-8748), so an entry's
//     backdrop paints above everything below the entry and below the
//     entry's own content. The port's entries defer their draw at
//     overlayPriorityBase + stack position, above every non-overlay
//     deferred draw (Base's popups paint at PopupPriority 100), and the
//     backdrop paints first inside the entry's own draw.
//   - Light dismiss is click-based (LightDismissFromClick is stable,
//     platform/runtime_enabled_features.json5:4153-4154):
//     HandlePopoverLightDismissForClick (html_element.cc:3215-3227)
//     runs right before the click event is dispatched
//     (core/input/mouse_event_manager.cc:262-267) and hides everything
//     above the popover related to BOTH the pointerdown target and the
//     pointerup target — only when they are the same popover, so a drag
//     that starts inside a popover and ends outside never dismisses.
//   - "Related" (FindTopmostRelatedPopover, html_element.cc:3141-3172):
//     the nearest open popover ancestor of the clicked node
//     (NearestOpenPopover :2924-2931) or the popover invoked by the
//     nearest invoker ancestor (NearestTargetPopoverForInvoker
//     :2933-2996), whichever is higher on the stack — so clicking the
//     trigger of an open popover does not close it, and a click inside a
//     nested child popover keeps the parent.
//   - HideAllPopoversUntil (html_element.cc:2321-2440) hides from the
//     top of the stack down to (not including) the endpoint; a null
//     endpoint hides the entire stack.
//   - The toggle event is queued as a task (the popover show/hide steps
//     queue a popover toggle event task), so it lands in the transcript
//     AFTER the whole input dispatch's listeners. The port queues the
//     layer's toggle notifications and drains them at the end of each
//     input dispatch — the DOM's task boundary — and at the end of a
//     frame, so stragglers (a Show driven by app code outside input)
//     still fire.
//
// The DOM↔port identity mapping: the popover stack's entries are DOM
// nodes; the port's entries are OverlayHandles (the FocusHandle
// pattern: a per-window identity created outside the view closure).
// The "DOM ancestor" test becomes an element-path prefix test: an entry
// owns every element whose GlobalElementID key extends its content path
// (the TopLayer element pushes its own id, so sibling overlays under
// one parent own disjoint subtrees), and the invoker link is resolved
// from a per-frame name→path registry filled by the invoker element's
// prepaint — the ticket-25 anchor registry's pattern.

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// OverlayKind
// ---------------------------------------------------------------------------

// OverlayKind is one top-layer entry's kind: which of Chromium's
// overlay families it belongs to (the popover attribute's values, plus
// the modal slot a dialog occupies).
type OverlayKind uint8

const (
	// OverlayPopover is popover=auto: it joins the ordered layer, has
	// click-based light dismiss, and showing one closes the
	// non-ancestor popovers above it (the default).
	OverlayPopover OverlayKind = iota
	// OverlayPopoverManual is popover=manual: it never light-dismisses,
	// never closes other popovers, and only closes through its own API.
	OverlayPopoverManual
	// OverlayHint is popover=hint (the tooltip family, ticket 22's
	// follow-up): light-dismissable like an auto popover; showing one
	// closes other hints above it, not autos (Chromium keeps a separate
	// hint stack).
	OverlayHint
	// OverlayModal is the modal dialog slot (Window.ShowModal, later
	// tickets): it sits in the ordered top layer and paints its
	// backdrop, but the popover light dismiss never closes it.
	OverlayModal
)

// String renders the kind like the HTML keyword.
func (k OverlayKind) String() string {
	switch k {
	case OverlayPopover:
		return "popover"
	case OverlayPopoverManual:
		return "popover=manual"
	case OverlayHint:
		return "popover=hint"
	case OverlayModal:
		return "modal"
	}
	return fmt.Sprintf("OverlayKind(%d)", uint8(k))
}

// lightDismissable reports whether the click-based light dismiss may
// close this kind: Chromium's popover stacks hold the auto and hint
// popovers only (manual never joins, dialogs are not popovers).
func (k OverlayKind) lightDismissable() bool {
	return k == OverlayPopover || k == OverlayHint
}

// ---------------------------------------------------------------------------
// OverlayHandle and the per-window layer state
// ---------------------------------------------------------------------------

// OverlayHandle is one overlay's identity in its window's top layer
// (the FocusHandle pattern: created outside the view closure, stable
// across re-renders). The handle owns the overlay's open state the way
// the DOM owns the popover attribute's open state: Show, Hide and
// Toggle flip it, the TopLayer element renders the overlay while it is
// open, and the layer's light dismiss closes it through Hide. Every
// transition fires the handle's OnToggle listener (queued and drained
// at the input task boundary, like the DOM's toggle event).
type OverlayHandle struct {
	id     uint64
	window *Window
}

// ID returns the overlay identity within its window.
func (h OverlayHandle) ID() uint64 { return h.id }

// NewOverlayHandle creates a closed overlay handle in w's top layer.
func NewOverlayHandle(w *Window) OverlayHandle {
	if w == nil {
		panic("gpui: NewOverlayHandle requires a live window")
	}
	os := overlayState(w)
	os.seq++
	os.handles[os.seq] = &overlayRecord{id: os.seq, window: w}
	return OverlayHandle{id: os.seq, window: w}
}

// Show opens the overlay (showPopover): it joins the ordered top layer
// above the entries already open, and the entry's first registration
// runs the show rules (closing the non-ancestor popovers above its
// topmost popover ancestor, html_element.cc:2043-2087). It reports
// whether the overlay opened (false when it was already open). The
// toggle notification is queued, not delivered inline.
func (h OverlayHandle) Show() bool {
	rec := h.record()
	if rec == nil || rec.open {
		return rec != nil && rec.open
	}
	rec.open = true
	rec.pendingShowRules = true
	// browser-core ticket 23: a plain Show opens the entry as the
	// popover family (a dialog shown with show() is not modal — the
	// is_modal_ mark belongs to showModal alone, html_dialog_element.
	// cc:579-580 vs 460-486), so a re-shown entry drops the modal
	// bookkeeping (the blocked document, the focusing steps, the
	// restore) until ShowModal marks it again.
	rec.modal = false
	rec.pendingModalFocus = false
	rec.closedBy = ClosedByAuto
	rec.dialogFocus = FocusHandle{}
	rec.prevFocus = FocusHandle{}
	rec.modalPath = ""
	os := overlayState(h.window)
	os.entries = append(os.entries, rec)
	// browser-core ticket 22: the show steps create the entry's close
	// watcher (html_element.cc:2146-2157, the stack-append branch), so
	// an unhandled Escape signals this overlay (closewatcher.go).
	rec.ensureCloseWatcher()
	queueOverlayToggle(rec, true)
	return true
}

// Hide closes the overlay (hidePopover): it leaves the top layer and
// queues the toggle notification. It reports whether the overlay closed
// (false when it was already closed).
func (h OverlayHandle) Hide() bool {
	rec := h.record()
	if rec == nil || !rec.open {
		return false
	}
	rec.hide()
	return true
}

// OnCancel sets a MODAL entry's cancel listener (browser-core ticket
// 23): the dialog's own cancel event (the DOM dialog element's
// oncancel — DialogCloseWatcherFiredCancel forwards the watcher's
// cancel onto the dialog, html_dialog_element.cc:497-510), fired when
// a close request (Escape or the closedby=any light dismiss) reaches
// the dialog. The listener can call the event's PreventDefault to keep
// the dialog open. Non-modal entries never signal it (the HTML
// popover rule leaves cancel alone). Setting it again replaces the
// listener. It reports the handle for chaining.
func (h OverlayHandle) OnCancel(f func(e *CloseWatcherEvent, w *Window, app *App)) OverlayHandle {
	if rec := h.record(); rec != nil {
		rec.onCancel = f
	}
	return h
}

// Toggle toggles the overlay and returns the resulting open state.
func (h OverlayHandle) Toggle() bool {
	if rec := h.record(); rec != nil && rec.open {
		h.Hide()
		return false
	}
	h.Show()
	return true
}

// IsOpen reports whether the overlay is in the top layer.
func (h OverlayHandle) IsOpen() bool {
	rec := h.record()
	return rec != nil && rec.open
}

// OnToggle sets the overlay's toggle listener, notified on every
// open/close transition (the DOM toggle event; delivered at the input
// task boundary). Setting it again replaces the listener.
func (h OverlayHandle) OnToggle(f func(open bool, w *Window, app *App)) {
	if rec := h.record(); rec != nil {
		rec.onToggle = f
	}
}

// record returns the handle's window record, or nil when the handle is
// zero or its window is gone.
func (h OverlayHandle) record() *overlayRecord {
	if h.window == nil || h.window.overlays == nil {
		return nil
	}
	return h.window.overlays.handles[h.id]
}

// hide is the record's close path (the handle's Hide and the layer's
// light dismiss and show rules): leave the ordered layer and queue the
// toggle notification. browser-core ticket 22: the hide steps destroy
// the entry's close watcher (html_element.cc:2834-2836). browser-core
// ticket 23: a modal dialog's close restores focus to the previously
// focused element (close, html_dialog_element.cc:206-223 —
// previously_focused_element->Focus(preventScroll) when the dialog was
// modal), after the entry has left the layer.
func (rec *overlayRecord) hide() {
	if rec.window == nil || rec.window.overlays == nil || !rec.open {
		return
	}
	os := overlayState(rec.window)
	for i, candidate := range os.entries {
		if candidate == rec {
			os.entries = append(os.entries[:i], os.entries[i+1:]...)
			break
		}
	}
	wasModal := rec.modal
	rec.open = false
	rec.registered = false
	rec.pendingShowRules = false
	rec.pendingModalFocus = false
	rec.modalPath = ""
	rec.destroyCloseWatcher()
	queueOverlayToggle(rec, false)
	if wasModal {
		rec.restoreModalFocus(rec.window)
	}
}

// overlayRecord is one overlay's persistent record: the handle's open
// state, its layer position and the frame-resolved entry data.
type overlayRecord struct {
	id     uint64
	window *Window
	open   bool
	// kind, invokerName and invokerPath come from the TopLayer
	// element's registration; contentPath is the element-path key the
	// entry owns (the DOM popover element's stand-in).
	kind        OverlayKind
	invokerName string
	invokerPath string
	contentPath string
	// registered reports whether the entry's TopLayer element rendered
	// in the last completed frame; an entry left unregistered for a
	// frame leaves the layer (the element stopped rendering: the app
	// closed its own state, or the tree dropped it).
	registered bool
	// pendingShowRules marks an entry shown but not yet registered: it
	// survives the reconciliation that would otherwise drop it, and its
	// first registration runs the show rules.
	pendingShowRules bool
	// onToggle is the handle's toggle listener.
	onToggle func(open bool, w *Window, app *App)
	// onCancel is a MODAL entry's dialog cancel listener (browser-core
	// ticket 23, the DOM dialog's oncancel event — the watcher's cancel
	// forwards onto the dialog, html_dialog_element.cc:497-510).
	onCancel func(e *CloseWatcherEvent, w *Window, app *App)
	// watcher is the entry's close watcher (browser-core ticket 22,
	// closewatcher.go): created at show, destroyed at hide — the
	// popover show/hide steps' watcher (html_element.cc:2146-2157 and
	// :2834-2836). Zero while the entry carries none (manual popovers).
	watcher CloseWatcher
	// modal marks a dialog entry opened by Window.ShowModal
	// (browser-core ticket 23): the entry registers with the modal
	// kind whatever the builder says, everything outside its content
	// path is inert, and close restores focus (modal.go).
	modal bool
	// closedBy is the modal's closedby policy (the TopLayer element's
	// ClosedAt registration; the auto default resolves to closerequest
	// for a modal). Zero (unset) means auto.
	closedBy ClosedBy
	// dialogFocus is the dialog element's own focus slot (the TopLayer
	// element's DialogFocus): the focusing steps' last resort and the
	// app's key-dispatch target for a dialog with nothing focusable
	// inside.
	dialogFocus FocusHandle
	// prevFocus is the previously focused element at ShowModal time
	// (previously_focused_element_, html_dialog_element.cc:674),
	// restored on close.
	prevFocus FocusHandle
	// pendingModalFocus marks a shown modal whose focusing steps have
	// not run yet (SetFocusForDialog runs at showModal; the port runs
	// them at the entry's first registration, when the frame knows the
	// tree — the show rules' pattern).
	pendingModalFocus bool
	// modalPath is the modal's content path as registered by the
	// TopLayer element's request phase this frame (the blocked
	// document's escape hatch, style_resolver.cc:563-583: everything
	// outside it is inert while the entry is the active modal).
	modalPath string
}

// ensureCloseWatcher creates the entry's close watcher when its kind
// registers one (browser-core ticket 22): every kind but manual —
// popover=auto and popover=hint create a watcher per show
// (html_element.cc:2146-2157, inside the stack-append branch both
// kinds take) and the modal dialog slot registers one
// (html_dialog_element.cc:536-545); popover=manual never joins the
// popover stacks and creates none. The watcher's close listener hides
// the entry (the PopoverCloseWatcherEventListener's close arm,
// html_element.cc:240-242), and its cancel listener stays unset (the
// HTML popover rule: "Don't do anything in response to cancel events",
// html_element.cc:243-245) — except a MODAL entry, whose cancel
// forwards to the dialog's own cancel listener (browser-core ticket
// 23, DialogCloseWatcherEventListener → CloseWatcherFiredCancel,
// html_dialog_element.cc:497-510: the dialog's preventDefault stops
// the close) and whose requestClose is always cancelable (the dialog
// path's CloseWatcher::RequestClose with AllowCancel::kAlways,
// html_dialog_element.cc:536 — RequestCloseInternal).
func (rec *overlayRecord) ensureCloseWatcher() {
	if rec == nil || rec.watcher != (CloseWatcher{}) || rec.kind == OverlayPopoverManual && !rec.modal {
		return
	}
	watcher := NewCloseWatcher(rec.window)
	watcher.OnClose(func(_ *Window, _ *App) { rec.hide() })
	if rec.modal {
		// The modal's watcher: always-cancelable requests (the dialog's
		// requestClose form, html_dialog_element.cc:536) and the dialog
		// cancel forwarding.
		if wrec := watcher.record(); wrec != nil {
			wrec.alwaysCancelable = true
		}
		watcher.OnCancel(func(e *CloseWatcherEvent, w *Window, app *App) {
			if rec.onCancel != nil {
				rec.onCancel(e, w, app)
			}
		})
	}
	rec.watcher = watcher
}

// destroyCloseWatcher destroys the entry's close watcher (the hide
// steps' close_watcher_->destroy(), html_element.cc:2834-2836).
func (rec *overlayRecord) destroyCloseWatcher() {
	if rec == nil || rec.watcher == (CloseWatcher{}) {
		return
	}
	rec.watcher.Destroy()
	rec.watcher = CloseWatcher{}
}

// overlayToggle is one queued toggle notification (the DOM's toggle
// event task).
type overlayToggle struct {
	rec  *overlayRecord
	open bool
}

// windowOverlayState is a window's top layer: the handle registry, the
// ordered layer (bottom first, the Chromium top layer's push order) and
// the queued toggle notifications.
type windowOverlayState struct {
	seq     uint64
	handles map[uint64]*overlayRecord
	entries []*overlayRecord
	// pendingToggles are the queued notifications, in transition order.
	pendingToggles []overlayToggle
}

// overlayState returns (creating when absent) the window's top-layer
// state. Foreground thread only.
func overlayState(w *Window) *windowOverlayState {
	if w == nil {
		panic("gpui: the top layer requires a live window")
	}
	if w.overlays == nil {
		w.overlays = &windowOverlayState{handles: make(map[uint64]*overlayRecord)}
	}
	return w.overlays
}

// queueOverlayToggle queues one toggle notification.
func queueOverlayToggle(rec *overlayRecord, open bool) {
	os := overlayState(rec.window)
	os.pendingToggles = append(os.pendingToggles, overlayToggle{rec: rec, open: open})
}

// drainOverlayToggles delivers the queued toggle notifications (the
// DOM's popover toggle event tasks after an input task): each listener
// sees the transition's open state.
func (w *Window) drainOverlayToggles(app *App) {
	if w == nil || w.overlays == nil {
		return
	}
	os := w.overlays
	if len(os.pendingToggles) == 0 {
		return
	}
	pending := os.pendingToggles
	os.pendingToggles = nil
	for _, t := range pending {
		if t.rec.onToggle != nil {
			t.rec.onToggle(t.open, w, app)
		}
	}
}

// overlayPriorityBase is the deferred-draw priority floor of top-layer
// entries: every entry paints above non-overlay deferred draws (Base's
// popup surfaces sit at 100), like the DOM top layer painting above
// every stacking context.
const overlayPriorityBase = 1 << 20

// overlayPriorityOf returns the entry's deferred-draw priority: the
// base plus its position in the ordered layer, so later entries paint
// above earlier ones and the entries sit above every other deferred
// draw.
func overlayPriorityOf(handle OverlayHandle) int {
	if handle.window == nil || handle.window.overlays == nil {
		return overlayPriorityBase
	}
	os := handle.window.overlays
	for i, rec := range os.entries {
		if rec.id == handle.id {
			return overlayPriorityBase + i
		}
	}
	// Not in the layer (closed, or shown but not yet registered): the
	// next free position keeps the draw above everything.
	return overlayPriorityBase + len(os.entries)
}

// ---------------------------------------------------------------------------
// The frame registration
// ---------------------------------------------------------------------------

// overlayRegistration is one entry's registration on a frame under
// construction (the TopLayer element's paint): the handle, its kind and
// invoker name from the builder, and the content path the entry owns.
// Registered during paint, consumed by the frame-end reconciliation
// (finishOverlayFrame).
type overlayRegistration struct {
	id          uint64
	kind        OverlayKind
	invokerName string
	contentPath string
	// closedBy is a MODAL entry's closedby policy (browser-core ticket
	// 23, the TopLayer element's ClosedBy).
	closedBy ClosedBy
	// dialogFocus is a MODAL entry's dialog focus slot (the TopLayer
	// element's DialogFocus).
	dialogFocus FocusHandle
}

// appendOverlayRegistration registers one entry on the frame under
// construction (the paint-phase pattern of the click and contextmenu
// registrations).
func appendOverlayRegistration(w *Window, reg overlayRegistration) {
	fs := focusState(w)
	if fs.next == nil {
		return
	}
	fs.next.overlayRegistrations = append(fs.next.overlayRegistrations, reg)
}

// finishOverlayFrame reconciles the top layer against the completed
// frame (browser-core 21, called from finishFrame after the frame
// swaps in): every entry's paths refresh from its registration, the
// newly shown entries run the show rules, entries whose element did
// not render leave the layer, and the queued toggle notifications
// drain (the frame is also a task boundary for app-driven
// transitions). It reports whether the layer changed anything an app
// listener could have dirtied state with (a delivered toggle or a
// dropped entry), so the caller refreshes after the frame is
// published — the focus listeners' pattern.
func (w *Window) finishOverlayFrame(app *App) bool {
	os := w.overlays
	if os == nil {
		return false
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return false
	}
	changed := false
	regs := fs.rendered.overlayRegistrations
	var shown []*overlayRecord
	for i := range regs {
		rec := os.handles[regs[i].id]
		if rec == nil || !rec.open {
			continue
		}
		rec.registered = true
		rec.kind = regs[i].kind
		// browser-core ticket 23: the modal mark (Window.ShowModal)
		// forces the dialog kind — the modal skips the popover light
		// dismiss walk and paints its own backdrop — and the entry's
		// closedby policy and dialog focus slot arrive here (the TopLayer
		// element's ClosedBy/DialogFocus).
		if rec.modal {
			rec.kind = OverlayModal
			rec.closedBy = regs[i].closedBy
			rec.dialogFocus = regs[i].dialogFocus
		}
		// browser-core ticket 22: the watcher follows the registered
		// kind — Show created one on the default kind before the entry
		// ever rendered, so a manual entry's first registration takes it
		// back out (manual popovers never join the close-watcher family;
		// every other kind ensures one exists, the show steps' rule).
		if rec.kind == OverlayPopoverManual && !rec.modal {
			rec.destroyCloseWatcher()
		} else {
			rec.ensureCloseWatcher()
		}
		if rec.modal && rec.closedBy.effective() == ClosedByNone {
			// SetCloseWatcherEnabledState (html_dialog_element.cc:531-542):
			// closedby=none disables the dialog's watcher — it no longer
			// receives the Escape signal. The port drops it from the stack
			// (the observable: Escape closes nothing); the drop comes after
			// the ensure above so it is not immediately re-created, and the
			// app's own close still works through Hide (a disabled
			// watcher's requestClose reports true without closing —
			// Chromium's enabled_ gate).
			rec.destroyCloseWatcher()
		}
		rec.invokerName = regs[i].invokerName
		rec.contentPath = regs[i].contentPath
		if regs[i].invokerName != "" {
			if path, ok := lookupInvokerPath(w, regs[i].invokerName); ok {
				rec.invokerPath = path
			} else {
				rec.invokerPath = ""
			}
		} else {
			rec.invokerPath = ""
		}
		if rec.pendingShowRules {
			rec.pendingShowRules = false
			shown = append(shown, rec)
		}
		// browser-core ticket 23: the dialog focusing steps run at the
		// modal entry's first registration (SetFocusForDialog runs inside
		// showModal, html_dialog_element.cc:611-616; the port's frame is
		// where the tree — the autofocus marking and the focusable
		// descendants — is known, the show rules' deferral). The steps
		// move focus, so the frame end refreshes.
		if rec.pendingModalFocus {
			rec.pendingModalFocus = false
			if w.runModalFocusingSteps(rec, fs.rendered) {
				changed = true
			}
		}
	}
	// The show rules run after every entry's paths are current, so the
	// ancestor walk sees this frame's nesting (ShowPopoverInternal
	// closes the popovers above the new one's topmost popover ancestor,
	// html_element.cc:2043-2087).
	for _, rec := range shown {
		os.runShowRules(rec)
	}
	// Entries whose element did not render this frame leave the layer:
	// the app stopped rendering the overlay (its own state closed it,
	// or the tree dropped it). A just-shown entry that has not rendered
	// yet survives one frame.
	kept := os.entries[:0]
	for _, rec := range os.entries {
		if !rec.registered && !rec.pendingShowRules {
			rec.open = false
			rec.modalPath = ""
			rec.destroyCloseWatcher()
			changed = true
			continue
		}
		kept = append(kept, rec)
	}
	os.entries = kept
	drained := len(os.pendingToggles)
	w.drainOverlayToggles(app)
	return changed || drained > 0
}

// runShowRules closes the popovers above the entry's topmost popover
// ancestor when the entry was shown (html_element.cc:2043-2087,
// ShowPopoverInternal's HideAllPopoversUntil(popoverToOpenParent, ...)):
// the ancestor is the highest entry on the layer whose content contains
// the new entry's content or its invoker (Chromium's DOM-nesting,
// anchor and invoker ancestor paths, FindTopmostPopoverAncestor). An
// entry with no ancestor closes everything of its family: an auto
// popover closes the autos and hints above it; a hint closes only
// hints (the hint stack, html_element.cc:2085-2118). browser-core
// ticket 23: a MODAL entry runs the same hide-until (showModal closes
// the popovers that are not its ancestors, html_dialog_element.cc:
// 586-593 — TopLayerElementPopoverAncestor then HideAllPopoversUntil),
// while the popover stacks hold no dialogs so nothing modal is hidden.
func (os *windowOverlayState) runShowRules(rec *overlayRecord) {
	if rec.kind == OverlayPopoverManual {
		return
	}
	var ancestor *overlayRecord
	for _, candidate := range os.entries {
		if candidate == rec || !candidate.kind.lightDismissable() {
			continue
		}
		if containsPath(candidate.contentPath, rec.contentPath) ||
			(rec.invokerPath != "" && containsPath(candidate.contentPath, rec.invokerPath)) {
			if ancestor == nil || os.positionOf(candidate) > os.positionOf(ancestor) {
				ancestor = candidate
			}
		}
	}
	// Hide the light-dismissable entries above the ancestor (or all of
	// the new entry's family when there is no ancestor), from the top
	// down, skipping the new entry itself.
	var hide []*overlayRecord
	for i := len(os.entries) - 1; i >= 0; i-- {
		candidate := os.entries[i]
		if candidate == rec {
			continue
		}
		if ancestor != nil && candidate == ancestor {
			break
		}
		if !candidate.kind.lightDismissable() {
			continue
		}
		if rec.kind == OverlayHint && candidate.kind != OverlayHint {
			continue
		}
		hide = append(hide, candidate)
	}
	for _, candidate := range hide {
		candidate.hide()
	}
}

// positionOf returns the entry's position in the ordered layer, or -1.
func (os *windowOverlayState) positionOf(rec *overlayRecord) int {
	for i, candidate := range os.entries {
		if candidate == rec {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Light dismiss (the click pair rule)
// ---------------------------------------------------------------------------

// lightDismissFromClick is the click-based popover light dismiss
// (browser-core 21): Chromium runs it while creating the click event,
// right before the listeners (mouse_event_manager.cc:262-267), passing
// the pointerdown and pointerup targets (the hit targets of the press
// and the release). HandlePopoverLightDismissForClick
// (html_element.cc:3215-3227) resolves the popover related to each
// target and, only when both resolve to the same popover, hides
// everything above it — so a drag that starts inside a popover and
// ends outside (different related popovers, or an outside press with
// an inside release) never dismisses. The press's hit chain is
// recorded at the down dispatch (pressHitPaths); the release's is the
// up's effective hit test.
func (w *Window) lightDismissFromClick(app *App) {
	if w == nil || w.overlays == nil || len(w.overlays.entries) == 0 {
		return
	}
	os := w.overlays
	fs := focusState(w)
	if fs.rendered == nil {
		return
	}
	is := inputState(w)
	downRelated := os.topmostRelatedPopover(is.pressHitPaths)
	upRelated := os.topmostRelatedPopover(chainPathsOf(fs.rendered, is.mouseHitTest))
	if downRelated != nil && downRelated != upRelated {
		// The press and the release relate to different popovers: the
		// drag case (html_element.cc:3225-3226's same-target check).
		return
	}
	os.hideAllPopoversUntil(upRelated)
}

// topmostRelatedPopover resolves the popover related to a click
// target's hit chain (FindTopmostRelatedPopover, html_element.cc:
// 3141-3172): the nearest open popover containing the clicked element
// (NearestOpenPopover's walk), or the open popover invoked by the
// nearest invoker containing it (NearestTargetPopoverForInvoker's
// walk), whichever is higher on the layer.
func (os *windowOverlayState) topmostRelatedPopover(paths []string) *overlayRecord {
	if len(paths) == 0 {
		return nil
	}
	var direct, invoker *overlayRecord
	for _, path := range paths {
		if rec, ok := os.nearestPopoverContaining(path); ok {
			direct = rec
			break
		}
	}
	for _, path := range paths {
		if rec, ok := os.nearestInvokerPopoverOf(path); ok {
			invoker = rec
			break
		}
	}
	if invoker == nil {
		return direct
	}
	if direct == nil {
		return invoker
	}
	if os.positionOf(direct) > os.positionOf(invoker) {
		return direct
	}
	return invoker
}

// nearestPopoverContaining returns the nearest open popover that
// contains the element at path (NearestOpenPopover: the nearest
// ancestor that is an open popover): the entry with the longest
// content path among the light-dismissable entries whose content path
// is path or a prefix of it — the innermost overlay that owns the
// element. The entry's content path prefixes the path exactly when the
// popover element is an ancestor of the clicked element (the TopLayer
// element pushes its own id, so sibling overlays own disjoint
// subtrees).
func (os *windowOverlayState) nearestPopoverContaining(path string) (*overlayRecord, bool) {
	var best *overlayRecord
	for _, rec := range os.entries {
		if !rec.kind.lightDismissable() || rec.contentPath == "" {
			continue
		}
		if containsPath(rec.contentPath, path) {
			if best == nil || len(rec.contentPath) > len(best.contentPath) {
				best = rec
			}
		}
	}
	return best, best != nil
}

// nearestInvokerPopoverOf returns the open popover invoked by the
// nearest invoker containing the element at path
// (NearestTargetPopoverForInvoker): the entry with the longest invoker
// path among the light-dismissable entries whose invoker path is path
// or a prefix of it.
func (os *windowOverlayState) nearestInvokerPopoverOf(path string) (*overlayRecord, bool) {
	var best *overlayRecord
	for _, rec := range os.entries {
		if !rec.kind.lightDismissable() || rec.invokerPath == "" {
			continue
		}
		if containsPath(rec.invokerPath, path) {
			if best == nil || len(rec.invokerPath) > len(best.invokerPath) {
				best = rec
			}
		}
	}
	return best, best != nil
}

// containsPath reports whether ancestor is path or an ancestor of it
// (the element-id path prefix test: "a" contains "a.b" but not "ab").
func containsPath(ancestor, path string) bool {
	if ancestor == "" || path == "" {
		return false
	}
	if len(ancestor) > len(path) {
		return false
	}
	if path == ancestor {
		return true
	}
	return strings.HasPrefix(path, ancestor) && path[len(ancestor)] == '.'
}

// hideAllPopoversUntil hides the light-dismissable entries from the
// top of the layer down to (not including) the endpoint
// (HideAllPopoversUntil, html_element.cc:2321-2440): a nil endpoint
// hides the whole family. Manual and modal entries are skipped, not
// stopping the walk, like Chromium's popover stacks, which hold autos
// and hints only.
func (os *windowOverlayState) hideAllPopoversUntil(endpoint *overlayRecord) {
	var hide []*overlayRecord
	for i := len(os.entries) - 1; i >= 0; i-- {
		rec := os.entries[i]
		if rec == endpoint {
			break
		}
		if !rec.kind.lightDismissable() {
			continue
		}
		hide = append(hide, rec)
	}
	for _, rec := range hide {
		rec.hide()
	}
}

// ---------------------------------------------------------------------------
// The TopLayer element
// ---------------------------------------------------------------------------

// TopLayerElement is the top-layer rendering of one overlay: while its
// handle is open, it registers the entry on the frame (the kind, the
// invoker and the content path the entry owns) and paints its content
// on top of everything, above every other deferred draw, with the
// optional backdrop below the content and above everything under the
// entry. Build one with TopLayer.
type TopLayerElement struct {
	handle OverlayHandle
	kind   OverlayKind
	// invoker is the element id name of the overlay's invoker: a click
	// on the invoker (or its content) relates to THIS entry, not the
	// popovers it sits inside, so light dismiss keeps this entry (the
	// DOM's popovertarget rule — clicking the trigger of an open
	// popover does not close it).
	invoker string
	// closedBy is a MODAL entry's closedby policy (browser-core ticket
	// 23): ClosedByAny lets a click outside the dialog close it, ClosedByNone
	// disables Escape, and the auto default (the zero value) acts as
	// closerequest for a modal — Escape closes, clicks outside do not.
	closedBy ClosedBy
	// dialogFocus is a MODAL entry's dialog focus slot: the focus handle
	// the dialog focusing steps fall back to when nothing inside is
	// focusable (the dialog element itself, always focusable in the
	// DOM). The app tracks it on the dialog box div so key events with
	// focus on the dialog reach its listeners.
	dialogFocus FocusHandle
	// backdrop is the entry's backdrop slot (::backdrop): painted over
	// everything below the entry and under the entry's content.
	backdrop AnyElement
	// onToggle is forwarded to the handle at build time.
	onToggle func(open bool, w *Window, app *App)
	// child is the overlay's content.
	child AnyElement
	// passThrough keeps the child in the caller's own painting order
	// (an overlay the app already defers, like Base's popup surfaces):
	// the element only registers the entry.
	passThrough bool
}

// TopLayer returns an element that renders handle's overlay in the
// window's top layer while the handle is open (the popover attribute's
// rendering half: an open popover joins the top layer and paints above
// everything). The default kind is OverlayPopover.
func TopLayer(handle OverlayHandle) *TopLayerElement {
	return &TopLayerElement{handle: handle, kind: OverlayPopover}
}

// Kind sets the entry's kind. The default is OverlayPopover.
func (t *TopLayerElement) Kind(kind OverlayKind) *TopLayerElement {
	t.kind = kind
	return t
}

// Invoker names the overlay's invoker element (CSS anchor-positioning's
// invoker link): the invoker element carries .ID(name), and light
// dismiss treats a click on it as a click on this entry — the nearest
// invoker wins over the popovers the invoker sits inside.
func (t *TopLayerElement) Invoker(id string) *TopLayerElement {
	t.invoker = id
	return t
}

// ClosedBy sets a MODAL entry's closedby policy (browser-core ticket
// 23, the dialog element's closedby attribute): ClosedByAny lets a
// click outside the dialog close it, ClosedByNone disables the Escape
// close request, and the default (unset) acts as closerequest — Escape
// closes, clicks outside do not (html_dialog_element.cc:250-253's auto
// default for a modal).
func (t *TopLayerElement) ClosedBy(policy ClosedBy) *TopLayerElement {
	t.closedBy = policy
	return t
}

// DialogFocus names a MODAL entry's dialog focus slot: the handle the
// dialog focusing steps fall back to when nothing inside the dialog
// is focusable (the dialog element itself, always focusable —
// html_dialog_element.h:114-116 SupportsFocus). The app tracks the
// handle on the dialog box div so key events dispatched while the
// dialog holds focus reach the div's listeners (the DOM dialog
// element's own position in the bubbling chain).
func (t *TopLayerElement) DialogFocus(handle FocusHandle) *TopLayerElement {
	t.dialogFocus = handle
	return t
}

// Backdrop sets the entry's backdrop slot (the ::backdrop pseudo): a
// full-window element painted below the entry's content and above
// everything below the entry (a scrim behind a modal). Not available
// with PassThrough.
func (t *TopLayerElement) Backdrop(child any) *TopLayerElement {
	t.backdrop = intoChild("TopLayer backdrop", 0, child)
	return t
}

// OnToggle sets the overlay's toggle listener (forwarded to the
// handle; OverlayHandle.OnToggle). Set it once per build so it is
// live before the first Show.
func (t *TopLayerElement) OnToggle(f func(open bool, w *Window, app *App)) *TopLayerElement {
	t.onToggle = f
	t.handle.OnToggle(f)
	return t
}

// PassThrough keeps the child in the caller's painting order: the
// element registers the entry (kind, invoker, light dismiss) but
// neither defers the content nor paints a backdrop — for overlays the
// app already paints above the tree itself (Base's deferred popup
// surfaces). The child requests, prepaints and paints exactly as it
// would without the wrapper.
func (t *TopLayerElement) PassThrough() *TopLayerElement {
	t.passThrough = true
	return t
}

// Child sets the overlay's content.
func (t *TopLayerElement) Child(child any) *TopLayerElement {
	t.child = intoChild("TopLayer", 1, child)
	return t
}

// IntoElement implements IntoElement.
func (t *TopLayerElement) IntoElement() AnyElement {
	if t.child.obj == nil {
		panic("gpui: TopLayer requires a child (Child)")
	}
	if t.passThrough && t.backdrop.obj != nil {
		panic("gpui: TopLayer PassThrough has no backdrop slot (the caller owns the painting)")
	}
	return CustomElement[topLayerRequest, topLayerPrepaint](&topLayerDrawable{
		topLayer:  t,
		composite: CustomElement[struct{}, struct{}](&topLayerContent{topLayer: t}),
	})
}

// topLayerRequest is the element's request-phase state.
type topLayerRequest struct {
	// childLayout is the composite container's node (the full form) or
	// the child's own node (the pass-through form).
	childLayout LayoutID
}

// topLayerPrepaint is the element's prepaint-phase state (nothing: the
// registration reads the ambient state at paint).
type topLayerPrepaint struct{}

// topLayerDrawable adapts the builder into the Element phases.
type topLayerDrawable struct {
	topLayer *TopLayerElement
	// composite is the full form's deferred element: the backdrop and
	// the content as one draw, so the backdrop paints below the content
	// within the entry's own priority.
	composite AnyElement
}

// elementIDName is the TopLayer element's own id name: derived from the
// handle, so the entry's content path (the element-id stack including
// this id) is distinct per overlay and sibling overlays under one
// parent own disjoint subtrees.
func (t *TopLayerElement) elementIDName() string {
	return fmt.Sprintf("overlay-%d", t.handle.id)
}

// ID implements Element: the auto-derived identity above, so the
// element's subtree extends the entry's content path.
func (d *topLayerDrawable) ID() (ElementID, bool) {
	return ElementID{Kind: ElementIDName, Name: d.topLayer.elementIDName()}, true
}

// SourceLocation implements Element.
func (d *topLayerDrawable) SourceLocation() *SourceLocation { return nil }

// topLayerNodeStyle is the entry's container style: an absolutely
// positioned node pinned to its containing block's origin at full
// size — the DOM top layer's stand-in (the overlay escapes ancestor
// stacking; a top-layer element's box is the viewport's, so the
// composite insets to zero and fills its containing block, which for
// a root-level entry is the window root), and a positioned containing
// block for the overlay's absolutely positioned descendants
// (browser-core 33: RequestContainerLayout opens the scope).
func topLayerNodeStyle() Style {
	style := DefaultStyle()
	style.Position = PositionAbsolute
	style.Display = DisplayFlex
	zero := DefiniteLengthOf(DefinitePx(0))
	style.Inset = LengthEdges{Top: zero, Right: zero, Bottom: zero, Left: zero}
	style.Size = LengthSize{Width: DefiniteLengthOf(Fraction(1)), Height: DefiniteLengthOf(Fraction(1))}
	return style
}

// RequestLayout implements Element. The full form requests the
// composite container (the backdrop and the content as its children);
// the pass-through form forwards the child's own node, like Deferred.
func (d *topLayerDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, topLayerRequest) {
	t := d.topLayer
	if !t.handle.IsOpen() {
		// Closed: a display:none node keeps the tree shape without
		// laying out the overlay (the pin's popover display rule).
		style := topLayerNodeStyle()
		style.Display = DisplayNone
		id, err := requestLayoutOf(w, style)
		if err != nil {
			panic(fmt.Sprintf("gpui: TopLayer request_layout: %v", err))
		}
		return id, topLayerRequest{}
	}
	// The invoker registry fills only while a window's overlays name
	// their invokers (the anchor registry's gate, ticket 25).
	if t.invoker != "" {
		requestInvokerTracking(w)
	}
	// browser-core ticket 23: a MODAL entry's content path registers
	// HERE — the request phase covers the whole tree before any
	// prepaint, and the blocked document's inertness (everything
	// outside this path) resolves during prepaint, so the path must be
	// known before the outside's divs prepaint. The path is this
	// element's id stack key (the drawable pushed overlay-<id>), the
	// same key the paint registration records and containsPath
	// compares (modal.go modalInertnessBlocks).
	if rec := t.handle.record(); rec != nil && rec.modal {
		if os := w.overlays; os != nil {
			os.registerModalContentPath(rec, (&GlobalElementID{Path: currentElementIDs(w)}).key())
		}
	}
	if t.passThrough {
		childLayout := t.child.RequestLayout(w, app)
		return childLayout, topLayerRequest{childLayout: childLayout}
	}
	compositeLayout := d.composite.RequestLayout(w, app)
	return compositeLayout, topLayerRequest{childLayout: compositeLayout}
}

// Prepaint implements Element. The full form defers the composite draw
// (the entry's top-layer paint, ordered by the layer position); the
// pass-through form prepaints the child in place.
func (d *topLayerDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *topLayerRequest, w *Window, app *App) topLayerPrepaint {
	t := d.topLayer
	if !t.handle.IsOpen() {
		return topLayerPrepaint{}
	}
	if t.passThrough {
		t.child.Prepaint(w, app)
		return topLayerPrepaint{}
	}
	// The element is taken and deferred at the current offset with the
	// entry's priority (Deferred's pattern, elements/deferred.rs).
	w.DeferDraw(d.composite, ElementOffset(w), overlayPriorityOf(t.handle), nil)
	return topLayerPrepaint{}
}

// Paint implements Element: the registration (the paint-phase pattern
// of the click registrations) plus, for the pass-through form, the
// child's paint in place. The full form paints nothing here — the
// composite draws in the deferred round.
func (d *topLayerDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *topLayerRequest, prepaint *topLayerPrepaint, w *Window, app *App) {
	t := d.topLayer
	if !t.handle.IsOpen() {
		return
	}
	kind := t.kind
	if rec := t.handle.record(); rec != nil && rec.modal {
		// browser-core ticket 23: ShowModal's entry registers with the
		// modal kind whatever the builder said (the record's modal mark
		// wins, so an app that renders TopLayer(handle) plainly after
		// Window.ShowModal still gets the dialog behaviour).
		kind = OverlayModal
	}
	appendOverlayRegistration(w, overlayRegistration{
		id:          t.handle.id,
		kind:        kind,
		invokerName: t.invoker,
		contentPath: (&GlobalElementID{Path: currentElementIDs(w)}).key(),
		closedBy:    t.closedBy,
		dialogFocus: t.dialogFocus,
	})
	if t.passThrough {
		t.child.Paint(w, app)
	}
}

// LayoutNodeStyle implements the root-stretch report.
func (d *topLayerDrawable) LayoutNodeStyle() (Style, bool) {
	if d.topLayer.passThrough {
		return Style{}, false
	}
	return topLayerNodeStyle(), true
}

// topLayerContent is the full form's deferred element: the backdrop
// and the content prepaint and paint inside one deferred draw, so the
// backdrop lands below the content within the entry's priority
// (AddToTopLayer inserts the ::backdrop directly below its element,
// document.cc:8746-8748).
type topLayerContent struct {
	topLayer *TopLayerElement
}

// ID implements Element: none — the composite must not push an id, so
// the subtree's paths extend the entry's content path exactly.
func (c *topLayerContent) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (c *topLayerContent) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element: the container node (absolute,
// full-size) through RequestContainerLayout, so the overlay's
// absolutely positioned descendants resolve against it (browser-core
// 33's containing-block rule inside the top layer).
func (c *topLayerContent) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	id, err := RequestContainerLayout(w, topLayerNodeStyle(), func(w *Window) []LayoutID {
		children := make([]LayoutID, 0, 2)
		if c.topLayer.backdrop.obj != nil {
			children = append(children, c.topLayer.backdrop.RequestLayout(w, app))
		}
		children = append(children, c.topLayer.child.RequestLayout(w, app))
		return children
	})
	if err != nil {
		panic(fmt.Sprintf("gpui: TopLayer content request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements Element: the backdrop, then the content, then
// the containment hitbox — one plain hitbox over the union of the
// hitboxes the children inserted during this prepaint, whose owner
// path is exactly the entry's content path (the deferred scope
// restored it). A click anywhere on the overlay's content —
// interactive or not — then resolves to this entry, like the DOM's
// popover-element ancestor walk.
func (c *topLayerContent) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	fs := focusState(w)
	hitboxStart := len(fs.next.hitboxes)
	if c.topLayer.backdrop.obj != nil {
		c.topLayer.backdrop.Prepaint(w, app)
	}
	c.topLayer.child.Prepaint(w, app)
	if union, ok := unionOfHitboxesFrom(fs.next, hitboxStart); ok {
		w.insertHitboxRecord(union, HitboxNormal)
	}
	return struct{}{}
}

// Paint implements Element: the backdrop first, then the content —
// the backdrop paints below the content within this entry's draw.
func (c *topLayerContent) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
	if c.topLayer.backdrop.obj != nil {
		c.topLayer.backdrop.Paint(w, app)
	}
	c.topLayer.child.Paint(w, app)
}

// unionOfHitboxesFrom returns the union of the hitboxes from index
// start (the records the overlay's content inserted during its
// prepaint), or false when the content inserted none.
func unionOfHitboxesFrom(frame *renderedFrame, start int) (Bounds, bool) {
	if frame == nil || start >= len(frame.hitboxes) {
		return Bounds{}, false
	}
	union := frame.hitboxes[start].box.Bounds
	for _, record := range frame.hitboxes[start+1:] {
		union = boundsUnion(union, record.box.Bounds)
	}
	return union, true
}
