package gpui

// The modal dialog slot (browser-core ticket 23, spec story 70):
// Window.ShowModal opens a top-layer overlay as a modal dialog
// (HTMLDialogElement::showModal, core/html/html_dialog_element.cc:
// 553-616): everything outside the dialog becomes inert, focus moves
// by the dialog focusing steps, focus returns to the previously
// focused element on close, and closedby controls light dismiss and
// Escape.
//
// The Chromium rules this ports (0d4dcd4f, %TEMP%\research-src\
// chromium, third_party/blink/renderer below):
//
//   - showModal (html_dialog_element.cc:553-616): beforetoggle, add to
//     the top layer, mark modal, recompute the inert subtrees, create
//     the close watcher, close the popovers that are not its ancestors
//     (HideAllPopoversUntil, :586-593), then run the focusing steps.
//   - The inert outside is part of style: with an active modal dialog
//     (the newest in the top layer, Document::ActiveModalDialog,
//     core/dom/document.cc:8841-8854), the document element is inert
//     and the modal element escapes it (core/css/resolver/
//     style_resolver.cc:563-583: html_inert true at the root, false at
//     the modal; the property inherits, so every subtree but the
//     modal's own is inert — Chromium's "blocked by a modal dialog").
//     The port reuses ticket 14's Inert machinery for the outside
//     (hitboxes, tab stops, key dispatch and the accessibility tree
//     all suppress), gated on the active modal's content path instead
//     of a per-element attribute.
//   - The dialog focusing steps (SetFocusForDialog, html_dialog_
//     element.cc:660-682): the autofocus delegate
//     (GetAutofocusDelegate, core/dom/container_node.cc's first
//     autofocusable focusable descendant), else the first keyboard
//     focusable descendant in tree order (GetFocusDelegate's walk —
//     IsFocusable with AdjustedTabIndex >= 0, a real tab stop), else
//     the dialog itself (a dialog is always focusable,
//     html_dialog_element.h:114-116 SupportsFocus), else — nothing
//     focusable and modal — a cleared focus.
//   - Focus restore on close (close, html_dialog_element.cc:206-223):
//     the previously focused element is refocused (preventScroll) when
//     the dialog was modal. previously_focused_element_ is the
//     document's focused element at showModal time (:674).
//   - closedby (html_dialog_element.cc:240-254): any / none /
//     closerequest, with the auto default acting as closerequest when
//     modal. Light dismiss applies only to closedby=any
//     (HandleDialogLightDismissForClick, :330-360), which runs beside
//     the popover light dismiss right before the click event
//     (core/input/mouse_event_manager.cc:262-267); a click on the
//     ::backdrop counts as outside (FindNearestDialog's rect check,
//     :274-302). Escape signals the dialog's close watcher, which is
//     disabled by closedby=none (SetCloseWatcherEnabledState,
//     :531-542) and whose requestClose is always cancelable
//     (RequestCloseInternal, :529-539, CloseWatcher::RequestClose with
//     AllowCancel::kAlways, core/html/closewatcher/close_watcher.cc:
//     246-249); the watcher's cancel forwards to the dialog's own
//     cancel event (DialogCloseWatcherEventListener →
//     CloseWatcherFiredCancel, html_dialog_element.cc:497-525), whose
//     preventDefault keeps the dialog open, and its close closes it
//     (CloseWatcherFiredClose → close, :527-531).
//
// The DOM↔port identity mapping: the dialog element is the TopLayer
// element's subtree, so the "modal element's subtree" is the entry's
// content path (the element-path prefix test, toplayer.go containsPath)
// and the dialog's own focus slot is named by the TopLayer builder's
// DialogFocus (the app tracks it on the dialog box div). The focusing
// steps and the show rules run at the entry's first registration after
// ShowModal — the frame is where the port knows the tree, the same
// deferral ticket 21 uses for the popover show rules.

// ClosedBy is a modal dialog's closedby policy (the HTML attribute):
// which close gestures — light dismiss and the Escape close request —
// may close the dialog.
type ClosedBy uint8

const (
	// ClosedByAuto is the attribute's missing-value default: it acts
	// as ClosedByCloseRequest for a modal dialog (and none for
	// anything else; every entry this governs is modal).
	ClosedByAuto ClosedBy = iota
	// ClosedByAny allows the light-dismiss click AND the close request
	// (Escape) to close the dialog.
	ClosedByAny
	// ClosedByCloseRequest allows only the close request (Escape); a
	// click outside the dialog is inert and does nothing.
	ClosedByCloseRequest
	// ClosedByNone allows neither: the dialog closes only through its
	// own API.
	ClosedByNone
)

// String renders the policy like the HTML keyword.
func (c ClosedBy) String() string {
	switch c {
	case ClosedByAuto:
		return "auto"
	case ClosedByAny:
		return "any"
	case ClosedByCloseRequest:
		return "closerequest"
	case ClosedByNone:
		return "none"
	}
	return "ClosedBy(?)"
}

// effective resolves the auto state (html_dialog_element.cc:250-253:
// the invalid-value and missing-value defaults are the auto state,
// which matches closerequest when the element is modal — every entry
// carrying this policy is modal).
func (c ClosedBy) effective() ClosedBy {
	if c == ClosedByAuto {
		return ClosedByCloseRequest
	}
	return c
}

// lightDismissable reports whether a click outside the dialog may
// close it (HandleDialogLightDismissForClick's ClosedByState::kAny
// gate, html_dialog_element.cc:349-360).
func (c ClosedBy) lightDismissable() bool {
	return c.effective() == ClosedByAny
}

// ShowModal opens the overlay as a modal dialog (HTMLDialogElement::
// showModal, html_dialog_element.cc:553-616): the entry joins the top
// layer with the modal kind, everything outside its content becomes
// inert, the dialog focusing steps run at the entry's first
// registration (the frame where the port knows the tree), and focus
// returns to the previously focused element when it closes. The closed
// by policy comes from the TopLayer element's ClosedBy (the default is
// auto — closerequest for a modal); the dialog's own focus slot from
// DialogFocus. It reports whether the dialog opened (false when it was
// already open; an already-open modal entry is a no-op, and a plain
// popover entry shown earlier is promoted).
func (w *Window) ShowModal(handle OverlayHandle) bool {
	if w == nil {
		panic("gpui: ShowModal requires a live window")
	}
	rec := handle.record()
	if rec == nil || rec.open {
		return rec != nil && rec.open
	}
	// showModal's steps in order (html_dialog_element.cc:576-614): the
	// open flag and the modal mark, the inert recomputation (the
	// frame applies it — the port's style is frame-resolved), the close
	// watcher, the popover hide-until (the show rules run at the entry's
	// first registration like every TopLayer entry), then the focusing
	// steps.
	rec.open = true
	rec.modal = true
	rec.pendingShowRules = true
	rec.pendingModalFocus = true
	rec.closedBy = ClosedByAuto
	// previously_focused_element_ = document.FocusedElement()
	// (SetFocusForDialog, html_dialog_element.cc:674): the focused
	// element at showModal time, restored on close.
	if focused := w.Focused(); focused.Ok {
		rec.prevFocus = focused.Handle
	} else {
		rec.prevFocus = FocusHandle{}
	}
	os := overlayState(w)
	os.entries = append(os.entries, rec)
	// The close watcher the open attribute already created
	// (html_dialog_element.cc:584-585); the modal's cancel forwarding
	// is wired at the entry's registration, once the closedby policy
	// arrives (ensureModalCloseWatcher).
	rec.ensureCloseWatcher()
	queueOverlayToggle(rec, true)
	return true
}

// ---------------------------------------------------------------------------
// The inert outside
// ---------------------------------------------------------------------------

// activeModal returns the window's active modal dialog
// (Document::ActiveModalDialog, document.cc:8841-8854): the highest
// open modal entry in the ordered layer. Everything outside its
// content path is inert (style_resolver.cc:563-583's escape applies to
// the modal element alone, so nested modals leave the older dialog's
// content inert too).
func (os *windowOverlayState) activeModal() *overlayRecord {
	if os == nil {
		return nil
	}
	for i := len(os.entries) - 1; i >= 0; i-- {
		if os.entries[i].modal && os.entries[i].open {
			return os.entries[i]
		}
	}
	return nil
}

// registerModalContentPath records the active modal's content path for
// this frame (called from the TopLayer element's request phase, before
// any prepaint — the request walk covers the whole tree first, so the
// path is known when the outside's inertness resolves). The path is
// the element-id stack key at the element's request: the same key the
// entry's paint registration and every path-prefix test use.
func (os *windowOverlayState) registerModalContentPath(rec *overlayRecord, path string) {
	rec.modalPath = path
}

// modalInertnessBlocks reports whether the modal blocks the element at
// path (the style resolver's inherited html_inert, style_resolver.cc:
// 563-583): a modal dialog is open and the path is not inside its
// content path. The element's own Inert() and an inert ancestor are
// checked separately (div.go's inert computation); this is the
// document-blocked half.
func modalInertnessBlocks(w *Window, path string) bool {
	if w == nil || w.overlays == nil || path == "" {
		return false
	}
	rec := w.overlays.activeModal()
	if rec == nil || rec.modalPath == "" {
		return false
	}
	return !containsPath(rec.modalPath, path)
}

// modalA11yBlocked is the a11y-tree half of the blocked document: the
// node at path publishes hidden while a modal blocks it
// (ax_node_object.cc:1368 ShouldIgnoreForHiddenOrInert, the same rule
// ticket 14's Inert uses).
func modalA11yBlocked(w *Window, path string) bool {
	return modalInertnessBlocks(w, path)
}

// ---------------------------------------------------------------------------
// The dialog focusing steps
// ---------------------------------------------------------------------------

// modalFocusEntry is one focusable element of the frame inside the
// modal's content: the element's path key, its focus handle and the
// element's autofocus marking. Registered at prepaint while a modal is
// open (registerDivElementDispatch), read by the focusing steps at the
// entry's first registration.
type modalFocusEntry struct {
	path      string
	handle    FocusHandle
	autoFocus bool
}

// appendModalFocusEntry records one focusable element of the frame
// under construction, gated on an open modal (the plain-frame cost is
// one nil check).
func appendModalFocusEntry(w *Window, path string, handle FocusHandle, autoFocus bool) {
	fs := focusState(w)
	if fs.next == nil {
		return
	}
	if w.overlays == nil || w.overlays.activeModal() == nil {
		return
	}
	fs.next.modalFocusEntries = append(fs.next.modalFocusEntries, modalFocusEntry{
		path:      path,
		handle:    handle,
		autoFocus: autoFocus,
	})
}

// runModalFocusingSteps is the dialog focusing steps
// (SetFocusForDialog, html_dialog_element.cc:660-682, over
// GetFocusDelegate, core/dom/element.cc's walk): the autofocus
// delegate — the first autofocus-marked focusable element in the
// content — else the first keyboard focusable element in tree order,
// else the dialog itself (DialogFocus), else a cleared focus (a modal
// dialog with nothing focusable blurs). It runs at the modal entry's
// first registration, when the frame's registrations are complete. It
// reports whether focus moved, so the frame end refreshes.
func (w *Window) runModalFocusingSteps(rec *overlayRecord, fs *renderedFrame) bool {
	// The autofocus delegate (GetAutofocusDelegate: the first
	// autofocusable focusable descendant, tree order). The dialog's own
	// slot (DialogFocus — the app tracks it on the dialog box div, the
	// DOM dialog element's stand-in) is not a descendant: the search
	// skips it, the fallback below uses it (ElementTraversal's
	// DescendantsOf never visits the dialog itself).
	for _, entry := range fs.modalFocusEntries {
		if entry.autoFocus && modalEntryContains(rec, entry) && entry.handle != rec.dialogFocus {
			w.Focus(entry.handle)
			return true
		}
	}
	// The first keyboard focusable descendant in tree order
	// (GetFocusDelegate's DescendantsOf walk with the dialog's
	// AdjustedTabIndex >= 0 gate — a real tab stop; a mouse-focusable
	// only element is skipped).
	for _, entry := range fs.modalFocusEntries {
		if modalEntryContains(rec, entry) && entry.handle != rec.dialogFocus && isKeyboardFocusable(entry.handle) {
			w.Focus(entry.handle)
			return true
		}
	}
	// The dialog itself (control = this): a dialog element is always
	// focusable (html_dialog_element.h:114-116), so the port focuses
	// the dialog's own slot when the app named one.
	if rec.dialogFocus != (FocusHandle{}) {
		w.Focus(rec.dialogFocus)
		return true
	}
	// Nothing focusable and modal: clear the focused element
	// (html_dialog_element.cc:677-679). Blur is a no-op when nothing
	// was focused.
	w.Blur()
	return w.Focused().Ok
}

// modalEntryContains reports whether the focus entry sits inside the
// modal's content path (the modal element's subtree — GetFocusDelegate
// walks the dialog's descendants).
func modalEntryContains(rec *overlayRecord, entry modalFocusEntry) bool {
	return containsPath(rec.contentPath, entry.path)
}

// isKeyboardFocusable reports whether the handle's element is a real
// tab stop (AdjustedTabIndex >= 0: focusable AND not excluded by a
// negative tabindex — the dialog focusing steps skip mouse-focusable
// only elements, element.cc's GetFocusDelegate dialog gate).
func isKeyboardFocusable(handle FocusHandle) bool {
	ref := handle.windowRef()
	if ref == nil {
		return false
	}
	return ref.tabStop && ref.tabIndex >= 0
}

// restoreModalFocus is close()'s focus restore (html_dialog_element.
// cc:206-223): the previously focused element is refocused (a script
// focus, preventScroll) when the dialog was modal. A released or
// vanished previously focused element leaves focus alone (the
// WeakMember failure — the end-of-frame fixup answers the rest).
func (rec *overlayRecord) restoreModalFocus(w *Window) {
	if rec == nil || w == nil || rec.prevFocus == (FocusHandle{}) {
		return
	}
	if w.focusRefOf(rec.prevFocus.id) == nil {
		return
	}
	w.Focus(rec.prevFocus)
}

// ---------------------------------------------------------------------------
// Light dismiss (closedby=any)
// ---------------------------------------------------------------------------

// lightDismissModalFromClick is the dialog light dismiss
// (HandleDialogLightDismissForClick, html_dialog_element.cc:330-360),
// run beside the popover light dismiss right before the click event
// (mouse_event_manager.cc:262-267): resolve the dialog related to the
// press target and the release target (FindNearestDialog's ancestor
// walk — a click on the backdrop or the inert page relates to nothing),
// and only when they are the same dialog does the TOPMOST open
// dialog — when it is not the clicked one — request close, and only
// with closedby=any. The request runs the watcher's cancel (the
// dialog's preventable cancel), not a direct close.
func (w *Window) lightDismissModalFromClick(app *App) {
	if w == nil || w.overlays == nil {
		return
	}
	os := w.overlays
	topmost := os.activeModal()
	if topmost == nil {
		return
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return
	}
	is := inputState(w)
	downDialog := os.nearestModalContaining(is.pressHitPaths)
	upDialog := os.nearestModalContaining(chainPathsOf(fs.rendered, is.mouseHitTest))
	if downDialog != upDialog {
		// The press and the release relate to different dialogs: the
		// drag rule (html_dialog_element.cc:345-352's same-target
		// check, the popover pair rule's twin).
		return
	}
	if upDialog == topmost {
		// The click is inside the topmost dialog: nothing dismisses
		// (html_dialog_element.cc:353-355).
		return
	}
	if !topmost.closedBy.lightDismissable() {
		return
	}
	topmost.requestModalClose()
}

// nearestModalContaining resolves the nearest modal dialog containing
// any path of the click's hit chain (FindNearestDialog's ancestor
// walk, html_dialog_element.cc:274-302): the open modal entry whose
// content path contains the chain path — the innermost match wins,
// like the popover walk's NearestOpenPopover. A chain that hit nothing
// (the inert outside, the backdrop's fall-through) relates to no
// dialog.
func (os *windowOverlayState) nearestModalContaining(paths []string) *overlayRecord {
	var best *overlayRecord
	for _, path := range paths {
		for _, rec := range os.entries {
			if !rec.modal || !rec.open || rec.contentPath == "" {
				continue
			}
			if containsPath(rec.contentPath, path) {
				if best == nil || len(rec.contentPath) > len(best.contentPath) {
					best = rec
				}
			}
		}
	}
	return best
}

// requestModalClose is the dialog's requestClose
// (RequestCloseInternal, html_dialog_element.cc:529-539): the close
// watcher is enabled for the request and asked with an always
// cancelable cancel — the dialog's own cancel listener can keep it
// open. Without a watcher (closedby=none dropped it) the request
// closes nothing, matching Chromium's disabled watcher.
func (rec *overlayRecord) requestModalClose() {
	if rec == nil || rec.watcher == (CloseWatcher{}) {
		return
	}
	rec.watcher.RequestClose()
}
