package gpui

// The user-select style and the automatic selection, browser-core
// ticket 20 (spec D7: text is selectable by default). The file owns:
// the UserSelect value and its ancestor-chain resolution; the window's
// automatic selection install (the state, the gestures, the
// registration flush — the TextSelectionLayer's role, installed by the
// window itself); and the Text element's participant registration with
// its slot identity, batched registration and wash. CSS UI 4
// §user-select, as Chromium computes it:
//
//   - The property is inherited (third_party/blink/renderer/core/css/
//     css_properties.json5:8169-8175, `inherited: true`, keywords
//     auto/none/text/all/contain), so a user-select: none ancestor
//     makes every descendant none unless the descendant sets its own
//     value (html.css:850's input[type=button]/submit/reset and the
//     button rule's html.css:904 branch are the UA stylesheet's
//     opt-outs for buttons and form controls).
//   - The used value of `auto` resolves from the ANCESTOR CHAIN:
//     Node::CanStartSelection (dom/node.cc:2017-2041) walks the parent
//     on auto — a none ancestor answers false, a text/all ancestor
//     true, the root true — so an explicit auto deep under a none
//     subtree does not re-enable selection.
//   - UsedUserSelect is none for inert elements (style/computed_style.
//     h:1881-1886); the port's inert suppression rides the hit test
//     (an inert subtree inserts inert hitboxes, which the selection
//     endpoint's IsHovered read skips), not this style.
//
// The port models auto/none/text (all and contain are not modelled;
// select-all is not generated). The value cascades through the text
// style stack (WithTextStyle), the same inheritance the text style
// properties use, and the resolution walks that stack — the ancestor
// chain — exactly like CanStartSelection.
//
// With the style, plain text becomes selectable by default (D7): the
// Text element registers a window-selection participant and requests
// the automatic I-beam while user-select allows selection
// (ShouldShowIBeamForNode, input/event_handler.cc:563-573 — a text node
// with CanStartSelection shows the I-beam; event_handler_test.cc:489
// pins the none case), and a mousedown on non-selectable content starts
// no selection (CanMouseDownStartSelect, editing/selection_controller.
// cc:122-127, feeding mouse_down_may_start_select_ at :1170).

// UserSelect is the value of the user-select property (CSS UI 4
// §user-select; browser-core ticket 20).
type UserSelect uint8

const (
	// UserSelectAuto is the initial value: selection resolves from the
	// ancestor chain (Chromium's CanStartSelection walk, node.cc:
	// 2017-2041) — selectable unless a user-select: none ancestor
	// forbids it.
	UserSelectAuto UserSelect = iota
	// UserSelectNone forbids selection on this element and its
	// descendants (unless a descendant sets text).
	UserSelectNone
	// UserSelectText allows selection explicitly, overriding an
	// inherited none (Chromium's plain inheritance; event_handler_
	// test.cc:557's UserSelectTextCanOverrideUserSelectNone).
	UserSelectText
)

// String renders the value like CSS.
func (u UserSelect) String() string {
	switch u {
	case UserSelectAuto:
		return "auto"
	case UserSelectNone:
		return "none"
	case UserSelectText:
		return "text"
	}
	return "unknown"
}

// userSelectAllowsSelection resolves whether text under the window's
// current text style stack may start a selection (Chromium's
// Node::CanStartSelection, node.cc:2017-2041): walk the refinement
// stack innermost first — the ancestor chain, each explicit value the
// element at that depth set; none answers false, text answers true,
// and auto keeps walking to the parent exactly like CanStartSelection's
// parent recursion. The chain's end — the root default — is text.
func userSelectAllowsSelection(w *Window) bool {
	if w == nil {
		return false
	}
	frame := currentFrame(w)
	if frame.userSelectSuppressed > 0 {
		return false
	}
	for i := len(frame.textStyles) - 1; i >= 0; i-- {
		if value, ok := frame.textStyles[i].UserSelect.Get(); ok {
			switch value {
			case UserSelectNone:
				return false
			case UserSelectText:
				return true
			}
		}
	}
	return true
}

// UserSelectAllowsSelection reports whether text at the window's current
// text style position may start a selection — the ancestor-chain walk
// above (browser-core 20). Base's inline registration paths consult it
// so a user-select: none subtree opts a rich-text document out the same
// way it opts plain text out.
func UserSelectAllowsSelection(w *Window) bool {
	return userSelectAllowsSelection(w)
}

// WithUserSelectSuppressed runs f with the automatic text-selection
// registration suppressed for this window (browser-core 20): the
// subtree f draws is not part of the window's plain-text document —
// the drag preview's snapshot, the inspector overlay, an inline flow's
// object fragments (whose paragraph's selection another participant
// owns) — so the texts inside it never join the window selection, and
// no automatic I-beam is requested for them. Nesting is allowed.
func WithUserSelectSuppressed[R any](w *Window, f func(*Window) R) R {
	frame := currentFrame(w)
	frame.userSelectSuppressed++
	defer func() { frame.userSelectSuppressed-- }()
	return f(w)
}

// ---------------------------------------------------------------------------
// The automatic window install (browser-core 20, spec D7)
// ---------------------------------------------------------------------------

// installWindowTextSelection gives the window its selection gestures
// unconditionally (browser-core 20): the document-order counter, the
// window's retained selection state and the frame's mouse listeners,
// at the head of the frame draw — the TextSelectionLayer's role
// (text_selection.go), installed by the window itself so plain text
// selects with no opt-in. The listeners register before any element
// can, so they sit outermost: the capture arm runs before every
// element's capture listener and the bubble arms after every
// element's, like the DOM's window-level handlers. The install also
// records the live selection's window band (the texts it can touch
// read their snapshot and paint the wash) and schedules the frame's
// registration flush, which applies every pending automatic
// registration in one window-state update — before the frame sweep,
// and before any input dispatch reads the participants.
func installWindowTextSelection(w *Window, app *App) {
	frame := currentFrame(w)
	beginSelectionFrame(app)
	// The state resolves lazily (userSelectStateOf): a window with no
	// selectable text never creates it, and the listeners installed here
	// resolve it per event, activating the moment the first selectable
	// text paints.
	state := userSelectStateOf(w, app)
	if state.app == nil {
		// No selectable text has created the state yet: the listeners
		// resolve it per event, so they activate on the first frame a
		// text paints (nothing to begin, sweep or band yet).
		paintTextSelection(state, w, app)
		return
	}
	_ = state.TryUpdate(app, func(s *windowSelectionState, _ *Context[windowSelectionState]) {
		s.touch.beginFrame()
		// The wash gate: while a selection exists, its band is the whole
		// viewport — the band math can select beyond the endpoints'
		// rectangle (a middle row selects fully), so every selectable text
		// checks its snapshot; while nothing is selected there is no band
		// and the texts cost nothing.
		if s.snapshot().IsSome() {
			frame.userSelectBand = Some(Bounds{Origin: Point{}, Size: frame.viewport})
		} else {
			frame.userSelectBand = Optional[Bounds]{}
		}
	})
	paintTextSelection(state, w, app)
}

// flushUserSelectionRegistrations applies the frame's collected
// automatic registrations in one window-state update (browser-core 20):
// drawRoots calls it after the element phases complete — the pending
// list is full, and the update runs inside the draw's own application
// update, before the frame completes and long before any input dispatch
// reads the participants. A window with no pending registrations (no
// selectable text this frame) queues no effect and touches no entity.
func flushUserSelectionRegistrations(w *Window, app *App) {
	frame := currentFrame(w)
	pending := frame.userSelectPending
	frame.userSelectPending = nil
	if len(pending) == 0 {
		return
	}
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return
	}
	_, _ = state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) struct{} {
		for i := range pending {
			s.registerParticipant(pending[i].handle, &pending[i].registration, cx.App())
		}
		return struct{}{}
	})
}

// userSelectRegistration is one pending automatic registration of the
// frame (browser-core 20): the participant handle and its registration,
// collected in paint order and applied by the flush.
type userSelectRegistration struct {
	handle       TextSelectionHandle
	registration TextSelectionRegistration
}

// userSelectStateOf resolves the window's selection state without
// creating it (browser-core 20): the draw state caches the acquisition,
// and the first selectable text creates the state (ensureUserSelectState)
// — a window that never shows selectable text owns no selection entity.
func userSelectStateOf(w *Window, app *App) WeakEntity[windowSelectionState] {
	return drawState(w).userSelectionState
}

// ensureUserSelectState resolves the window's selection state, creating
// it once on the first selectable text: the acquisition's lease lives in
// the window's scope until the window closes (UpgradeInto adds a lease
// per call, so the acquisition is cached in the draw state).
func ensureUserSelectState(w *Window, app *App) WeakEntity[windowSelectionState] {
	ds := drawState(w)
	if state := ds.userSelectionState; state.IsUpgradable() {
		return state
	}
	state := textSelectionStateAcquire(w.ID(), app, w.Scope())
	ds.userSelectionState = state
	return state
}

// ---------------------------------------------------------------------------
// The Text element's participation (browser-core 20)
// ---------------------------------------------------------------------------

// paintUserSelectText registers a plain Text element's run with the
// window selection and paints its wash under the glyphs (the paint runs
// before the glyphs). The participant's identity is the window's
// paint-order slot (userSelectHandle — the slot is document order for a
// stable tree, so the participant, and a selection anchored on it,
// survives across frames while the text keeps drawing); the run's
// layout answers in SOURCE byte offsets through the cell's collapse
// map; the registration rides the frame's batch (flushed with one
// entity update at the frame's effect flush, before any input
// dispatch), and its runs ride the registration itself, so an idle
// frame costs no per-text entity update; and the automatic I-beam is
// requested for the hitbox — the resolution prefers any explicit cursor
// (interact.go frameCursorStyle, Chromium's SelectCursor at
// event_handler.cc:574-764).
func paintUserSelectText(t *textElement, layout *stringLayoutState, hitbox *Hitbox, w *Window, app *App) {
	frame := currentFrame(w)
	source := layout.cell.source
	slot := frame.userSelectSlot
	frame.userSelectSlot++
	handle := userSelectHandle(w, app, slot)
	selectionLayout := &TextLayout{state: &textLayoutState{inner: &textLayoutInner{
		len:        len(source),
		text:       source,
		document:   layout.cell.shaped,
		runs:       layout.cell.runs,
		lineHeight: layout.lineHeight,
		wrapWidth:  layout.cell.wrapWidth,
		collapse:   layout.cell.collapse,
		size:       &layout.cell.size,
		bounds:     layout.bounds,
	}}}
	run := NewTextSelectionRun(source, selectionLayout, *layout.bounds).
		WithDocumentOrder(nextSelectionDocumentOrder(app))
	registration := NewTextSelectionRegistration(*hitbox, *layout.bounds).
		WithDocumentOrder(run.DocumentOrder()).
		WithTextBounds([]Bounds{*layout.bounds}).
		WithRuns([]TextSelectionRun{run})
	frame.userSelectPending = append(frame.userSelectPending, userSelectRegistration{
		handle:       handle,
		registration: registration,
	})
	// The cursor: auto resolution — the I-beam over the selectable text
	// (ShouldShowIBeamForNode, event_handler.cc:563-573) while no
	// explicit cursor request covers the pointer.
	w.setAutomaticCursorStyle(CursorIBeam, hitbox)
	// The wash: only while a selection exists, and only for the texts
	// the selection's window band can touch (the band recorded at the
	// frame's head). The projection runs locally over this frame's run.
	if band, ok := frame.userSelectBand.Get(); ok && boundsOverlap(band, *layout.bounds) {
		paintUserSelectWash(handle, selectionLayout, run, app, w)
	}
}

// userSelectHandle resolves slot's participant handle, creating it once
// (browser-core 20): the handles live in the window's draw state,
// indexed by the frame's paint-order slot — the identity survives
// across frames for a stable tree, and the per-frame lookup is an array
// index. The entities are owned by the window's scope, so they live as
// long as the window; a slot whose text stopped drawing is swept from
// the participants map by the model's frame sweep (the registration
// generation), and its entity is reused when a text paints under the
// slot again.
func userSelectHandle(w *Window, app *App, slot uint64) TextSelectionHandle {
	ds := drawState(w)
	for uint64(len(ds.userSelectParticipants)) <= slot {
		ds.userSelectParticipants = append(ds.userSelectParticipants, TextSelectionHandle{})
	}
	if handle := ds.userSelectParticipants[slot]; handle.entity.EntityID() != 0 {
		return handle
	}
	handle := TextSelectionHandle{entity: NewEntity(app, w.Scope(), func(*selectableTextState, *Context[selectableTextState]) {})}
	ds.userSelectParticipants[slot] = handle
	return handle
}

// paintUserSelectWash paints the selection wash under the glyphs of a
// live selection (browser-core 20): the participant's snapshot — the
// window points the gestures published — projected onto this frame's
// run, painted with the system's selection colour by default
// (::selection, browser-core 28).
func paintUserSelectWash(handle TextSelectionHandle, layout *TextLayout, run TextSelectionRun, app *App, w *Window) {
	snapshot, ok := handle.Snapshot(app).Get()
	if !ok {
		return
	}
	projection := projectTextSelectionRanges(Some(snapshot), []TextSelectionRun{run})
	color := app.SelectionColorsForPaint().Background
	for _, r := range projection.Ranges() {
		if r, ok := r.Get(); ok {
			paintSelectableTextSelection(layout, r, color, w)
		}
	}
}

// WithUserSelectSuppressedVoid is the void form of
// WithUserSelectSuppressed.
func WithUserSelectSuppressedVoid(w *Window, f func(*Window)) {
	WithUserSelectSuppressed(w, func(w *Window) struct{} {
		f(w)
		return struct{}{}
	})
}

// boundsOverlap reports whether the two bounds share any area
// (boundsContain over the corners: two boxes overlap when either
// contains a corner of the other).
func boundsOverlap(a, b Bounds) bool {
	return boundsContain(a, b.Origin) || boundsContain(a, boundsCorner(b)) ||
		boundsContain(b, a.Origin) || boundsContain(b, boundsCorner(a))
}

// boundsCorner is the far corner of the bounds.
func boundsCorner(b Bounds) Point {
	return Point{X: b.Origin.X + b.Size.Width, Y: b.Origin.Y + b.Size.Height}
}
