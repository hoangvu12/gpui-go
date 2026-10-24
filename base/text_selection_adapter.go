package base

// Port of U:text/selection_adapter.rs: TextView's renderer-specific bridge
// to the window TextSelection (text_selection.go), plus the TextViewState
// and TextView pieces that drive it (U:text/state.rs:743-767 block_ix_at,
// 880-885 set_auto_scroll, 1066-1101 the render prepaint;
// U:text/text_view.rs:764-775, 791-794, 832-856 the element's
// registration and touch handles; U:text/inline.rs:996-1001 the painted
// selection edges).
//
// It plugs into ticket 28's hooks (text_state.go) in init.
//
// Go adaptations: the adapter is shared by pointer (Rust clones it out of
// the state before registering; the clone only snapshots what this frame
// collected, which the pointer still holds when the element registers).
// The selection participant is owned by the TextViewState's dependent
// scope.

import "github.com/hoangvu12/gpui-go/gpui"

func init() {
	newTextViewSelectionAdapter = func(view gpui.WeakEntity[TextViewState], owner *gpui.Scope, app *gpui.App) textViewSelectionAdapter {
		return newTextViewSelectionBridge(view, owner, app)
	}
	textSelectionRegisterInline = func(view gpui.Entity[TextViewState], lineBounds []gpui.Bounds, text string, layout *gpui.TextLayout, bounds gpui.Bounds, app *gpui.App) {
		adapter, ok := textViewBridgeOf(view, app)
		if !ok {
			return
		}
		adapter.registerInline(lineBounds)
		if layout != nil {
			adapter.registerTextRun(NewTextSelectionRun(text, layout, bounds))
		}
	}
	textSelectionEnd = TextSelectionEnd
	textSelectionSelectedText = TextSelectionSelectedText
}

// cachedBlockEndpoint is CachedBlockEndpoint
// (U:text/selection_adapter.rs:11-15).
type cachedBlockEndpoint struct {
	endpoint TextSelectionEndpoint
	blockIx  gpui.Optional[int]
}

// virtualBlockSelection is VirtualBlockSelection
// (U:text/selection_adapter.rs:17-69): the top-level blocks a virtualized
// view's selection endpoints were in when they were taken.
type virtualBlockSelection struct {
	anchor, cursor gpui.Optional[cachedBlockEndpoint]
	coverage       TextSelectionCoverage
}

// update is U:text/selection_adapter.rs:25-35.
func (v *virtualBlockSelection) update(snapshot gpui.Optional[TextSelectionSnapshot], entityID gpui.EntityID) {
	snap, ok := snapshot.Get()
	if !ok {
		*v = virtualBlockSelection{}
		return
	}
	v.coverage = snap.Coverage()
	updateCachedBlockEndpoint(&v.anchor, snap.Anchor(), entityID)
	updateCachedBlockEndpoint(&v.cursor, snap.Cursor(), entityID)
}

// updateCachedBlockEndpoint is U:text/selection_adapter.rs:37-49.
func updateCachedBlockEndpoint(cached *gpui.Optional[cachedBlockEndpoint], endpoint TextSelectionEndpoint, entityID gpui.EntityID) {
	if current, ok := cached.Get(); ok && current.endpoint == endpoint {
		return
	}
	var blockIx gpui.Optional[int]
	if endpoint.EntityID() == gpui.Some(entityID) {
		if key, ok := endpoint.ContentKey().Get(); ok {
			blockIx = gpui.Some(int(key.Value()))
		}
	}
	*cached = gpui.Some(cachedBlockEndpoint{endpoint: endpoint, blockIx: blockIx})
}

// blockRange is the inclusive block range the selection covers in this
// view (U:text/selection_adapter.rs:51-68).
func (v *virtualBlockSelection) blockRange(entityID gpui.EntityID, last int) *[2]int {
	anchor, ok := v.anchor.Get()
	if !ok {
		return nil
	}
	cursor, ok := v.cursor.Get()
	if !ok {
		return nil
	}
	either := func() (int, bool) {
		if ix, ok := anchor.blockIx.Get(); ok {
			return ix, true
		}
		return cursor.blockIx.Get()
	}
	switch v.coverage {
	case TextSelectionCoverageFull:
		return &[2]int{0, last}
	case TextSelectionCoverageFromStart:
		ix, ok := either()
		if !ok {
			return nil
		}
		return &[2]int{0, ix}
	case TextSelectionCoverageToEnd:
		ix, ok := either()
		if !ok {
			return nil
		}
		return &[2]int{ix, last}
	}
	if anchor.endpoint.EntityID() != gpui.Some(entityID) || cursor.endpoint.EntityID() != gpui.Some(entityID) {
		return nil
	}
	a, ok := anchor.blockIx.Get()
	if !ok {
		return nil
	}
	c, ok := cursor.blockIx.Get()
	if !ok {
		return nil
	}
	return &[2]int{min(a, c), max(a, c)}
}

// textViewSelectionBridge is TextViewSelectionAdapter
// (U:text/selection_adapter.rs:71-81).
type textViewSelectionBridge struct {
	selection  TextSelectionHandle
	textBounds []gpui.Bounds
	textRuns   []TextSelectionRun
	// selectionEdges is the caret boxes at the first and last selected
	// character painted this frame, where the touch handles go.
	selectionEdges gpui.Optional[[2]gpui.Bounds]
	layoutRevision gpui.Optional[int]
}

// newTextViewSelectionBridge is TextViewSelectionAdapter::new
// (U:text/selection_adapter.rs:84-183).
func newTextViewSelectionBridge(view gpui.WeakEntity[TextViewState], owner *gpui.Scope, app *gpui.App) *textViewSelectionBridge {
	selection := NewTextSelectionHandle("", app, owner)
	selectionID := selection.EntityID()
	blocks := &virtualBlockSelection{}

	selection.Subscribe(func(event *TextSelectionEvent, app *gpui.App) {
		switch event.Kind {
		case TextSelectionEventSelectionChanged:
			snapshot := event.Snapshot
			_ = view.TryUpdate(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
				s.preserveInlineSelection = false
				blocks.update(snapshot, selectionID)
				snap, ok := snapshot.Get()
				s.isSelecting = ok && snap.IsSelecting()
				cx.Notify()
			})
		case TextSelectionEventAutoScroll:
			delta := event.Delta
			_ = view.TryUpdate(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
				if s.scrollable {
					s.setAutoScroll(delta, cx)
				} else if !delta.IsSome() {
					s.autoScroll.Stop()
				}
			})
		case TextSelectionEventTouchSelectionChanged:
			_ = view.TryUpdate(app, func(_ *TextViewState, cx *gpui.Context[TextViewState]) { cx.Notify() })
		}
	}, app).Detach()

	selection.ClearWith(func(app *gpui.App) {
		*blocks = virtualBlockSelection{}
		_ = view.TryUpdate(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
			s.resetSelection()
			cx.Notify()
		})
	}, app)

	selection.CopyWith(func(app *gpui.App) string {
		text, err := view.TryRead(app, func(s *TextViewState, _ *gpui.App) string {
			last := max(len(s.parsedContent.document.blocks)-1, 0)
			return s.selectedTextIn(blocks.blockRange(selectionID, last))
		})
		if err != nil {
			return ""
		}
		return text
	}, app)

	selection.ResolveContentKeyWith(func(point gpui.Point, app *gpui.App) (TextSelectionContentKey, bool) {
		type found struct {
			ix int
			ok bool
		}
		block, err := view.TryRead(app, func(s *TextViewState, _ *gpui.App) found {
			ix, ok := s.blockIxAt(point.Y)
			return found{ix, ok}
		})
		if err != nil || !block.ok {
			return 0, false
		}
		return NewTextSelectionContentKey(uint64(block.ix)), true
	}, app)

	selection.FocusWith(func(w *gpui.Window, app *gpui.App) {
		focus, err := view.TryUpdateWith(app, func(s *TextViewState, _ *gpui.Context[TextViewState]) gpui.FocusHandle {
			s.isSelecting = true
			return s.focusHandle
		})
		if err != nil {
			return
		}
		focus.Focus(w)
	}, app)

	return &textViewSelectionBridge{selection: selection}
}

// textViewBridgeOf is the TextView's adapter, when it has one.
func textViewBridgeOf(view gpui.Entity[TextViewState], app *gpui.App) (*textViewSelectionBridge, bool) {
	adapter := view.Read(app, func(s *TextViewState, _ *gpui.App) textViewSelectionAdapter { return s.selectionAdapter })
	bridge, ok := adapter.(*textViewSelectionBridge)
	return bridge, ok && bridge != nil
}

// updateLayoutRevision reports a layout revision change outside a drag
// (U:text/selection_adapter.rs:185-193).
func (a *textViewSelectionBridge) updateLayoutRevision(revision int, isSelecting bool) bool {
	previous, ok := a.layoutRevision.Get()
	changed := ok && previous != revision
	if !changed || !isSelecting {
		a.layoutRevision = gpui.Some(revision)
	}
	return changed && !isSelecting
}

// beginFrame is U:text/selection_adapter.rs:195-199.
func (a *textViewSelectionBridge) beginFrame() {
	a.textBounds = nil
	a.textRuns = nil
	a.selectionEdges = gpui.Optional[[2]gpui.Bounds]{}
}

// registerSelectionEdges records one inline's painted selection ends.
// Inlines paint in document order, so the first start and the last end are
// the view's (U:text/selection_adapter.rs:201-206).
func (a *textViewSelectionBridge) registerSelectionEdges(start, end gpui.Bounds) {
	first := start
	if edges, ok := a.selectionEdges.Get(); ok {
		first = edges[0]
	}
	a.selectionEdges = gpui.Some([2]gpui.Bounds{first, end})
}

// registerTextRun is U:text/selection_adapter.rs:208-210.
func (a *textViewSelectionBridge) registerTextRun(run TextSelectionRun) {
	a.textRuns = append(a.textRuns, run)
}

// registerInline is U:text/selection_adapter.rs:212-214.
func (a *textViewSelectionBridge) registerInline(bounds []gpui.Bounds) {
	a.textBounds = append(a.textBounds, bounds...)
}

// register reports this frame's geometry to the window selection
// (U:text/selection_adapter.rs:216-239).
func (a *textViewSelectionBridge) register(hitbox gpui.Hitbox, bounds gpui.Bounds, scrollOffset gpui.Point, documentOrder uint64, selfScroll bool, w *gpui.Window, app *gpui.App) {
	a.selection.SetHitTestRuns(a.textRuns, app)
	registration := NewTextSelectionRegistration(hitbox, bounds).
		WithScrollOffset(scrollOffset).
		WithDocumentOrder(documentOrder).
		WithTextBounds(append([]gpui.Bounds(nil), a.textBounds...)).
		WithSelfScroll(selfScroll).
		WithRenderedElement(a.selection, w, app)
	if edges, ok := a.selectionEdges.Get(); ok {
		registration = registration.WithSelectionEdges(edges[0], edges[1])
	}
	a.selection.Register(registration, w, app)
}

// prepaintTouchHandles is U:text/selection_adapter.rs:241-249.
func (a *textViewSelectionBridge) prepaintTouchHandles(w *gpui.Window, app *gpui.App) TouchHandleLayout {
	return a.selection.PrepaintTouchHandles(w, app)
}

// paintTouchHandles is U:text/selection_adapter.rs:251-262.
func (a *textViewSelectionBridge) paintTouchHandles(layout *TouchHandleLayout, color gpui.Hsla, w *gpui.Window, app *gpui.App) {
	a.selection.PaintTouchHandles(layout, color, w, app)
}

// selectionPoints is U:text/selection_adapter.rs:264-267.
func (a *textViewSelectionBridge) selectionPoints(app *gpui.App) (gpui.Point, gpui.Point, bool) {
	snapshot, ok := a.selection.Snapshot(app).Get()
	if !ok {
		return gpui.Point{}, gpui.Point{}, false
	}
	points, ok := snapshot.WindowPoints().Get()
	if !ok {
		return gpui.Point{}, gpui.Point{}, false
	}
	return points.Anchor(), points.Cursor(), true
}

// setLocalSelection is U:text/selection_adapter.rs:269-271.
func (a *textViewSelectionBridge) setLocalSelection(active bool, app *gpui.App) {
	a.selection.SetLocalSelection(active, app)
}

// isPartOfWindowSelection is U:text/selection_adapter.rs:273-278.
func (a *textViewSelectionBridge) isPartOfWindowSelection(app *gpui.App) bool {
	id := gpui.Some(a.selection.EntityID())
	snapshot, ok := a.selection.Snapshot(app).Get()
	return ok && (snapshot.Anchor().EntityID() == id || snapshot.Cursor().EntityID() == id)
}

// hasSelectionSnapshot is U:text/selection_adapter.rs:280-282.
func (a *textViewSelectionBridge) hasSelectionSnapshot(app *gpui.App) bool {
	return a.selection.Snapshot(app).IsSome()
}

// textBoundsForTest is U:text/selection_adapter.rs:284-287 (test-only).
func (a *textViewSelectionBridge) textBoundsForTest() []gpui.Bounds {
	return append([]gpui.Bounds(nil), a.textBounds...)
}

// ---------------------------------------------------------------------------
// TextViewState and TextView wiring
// ---------------------------------------------------------------------------

// blockIxAt is the index of the top-level block at contentY, in the view's
// content coordinates; false for a view that is not virtualized
// (U:text/state.rs:743-767).
func (s *TextViewState) blockIxAt(contentY float32) (int, bool) {
	if !s.scrollable {
		return 0, false
	}
	origin := s.bounds.Origin.Y + s.scrollOffset().Y
	count := s.listState.ItemCount()
	for ix := s.listState.LogicalScrollTop().ItemIx; ix < count; ix++ {
		bounds, ok := s.listState.BoundsForItem(ix)
		if !ok {
			return 0, false
		}
		if contentY < bounds.Bottom()-origin {
			return ix, true
		}
	}
	if count == 0 {
		return 0, false
	}
	return count - 1, true
}

// setAutoScroll scrolls the view's list while a drag holds past its edge
// (U:text/state.rs:880-885).
func (s *TextViewState) setAutoScroll(delta gpui.Optional[float32], cx *gpui.Context[TextViewState]) {
	s.autoScroll.Set(delta, cx, func(delta float32, state *TextViewState, cx *gpui.Context[TextViewState]) {
		state.listState.ScrollBy(delta, nil, cx.App())
		cx.Notify()
	})
}

// textViewSelectionPrepaint is the render's on_prepaint
// (U:text/state.rs:1066-1101): it records the bounds, and clears the
// window selection when this view's layout changed under it outside a
// drag.
func textViewSelectionPrepaint(state gpui.WeakEntity[TextViewState], bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	type facts struct {
		sizeChanged, involvesView, hasSnapshot, isSelecting, compatible bool
		adapter                                                         *textViewSelectionBridge
	}
	f, err := state.TryRead(app, func(s *TextViewState, app *gpui.App) facts {
		bridge, _ := s.selectionAdapter.(*textViewSelectionBridge)
		out := facts{
			sizeChanged: s.bounds.Size != bounds.Size,
			isSelecting: s.isSelecting,
			compatible:  s.compatibleLayoutUpdate,
			adapter:     bridge,
		}
		if bridge != nil {
			out.involvesView = bridge.isPartOfWindowSelection(app)
			out.hasSnapshot = bridge.hasSelectionSnapshot(app)
		}
		return out
	})
	if err != nil {
		return
	}
	revisionChanged := false
	_ = state.TryUpdate(app, func(s *TextViewState, _ *gpui.Context[TextViewState]) {
		if f.adapter != nil {
			revisionChanged = f.adapter.updateLayoutRevision(s.selectionRevision, s.isSelecting)
		}
		s.updateBounds(bounds)
		s.compatibleLayoutUpdate = false
	})
	if !f.isSelecting && ((f.sizeChanged && f.involvesView && !f.compatible) || (revisionChanged && f.hasSnapshot)) {
		TextSelectionClear(w, app)
	}
}

// textViewPrepaintTouchHandles lays out the view's touch handles, over the
// text, so after its hitbox (U:text/text_view.rs:764-772).
func textViewPrepaintTouchHandles(state gpui.Entity[TextViewState], w *gpui.Window, app *gpui.App) TouchHandleLayout {
	adapter, ok := textViewBridgeOf(state, app)
	if !ok {
		return TouchHandleLayout{}
	}
	return adapter.prepaintTouchHandles(w, app)
}

// textViewSelectionBeginFrame clears the geometry collected last frame
// (U:text/text_view.rs:791-794).
func textViewSelectionBeginFrame(state gpui.Entity[TextViewState], app *gpui.App) {
	if adapter, ok := textViewBridgeOf(state, app); ok {
		adapter.beginFrame()
	}
}

// textViewSelectionPaint registers the view with the window selection and
// paints its touch handles over the text (U:text/text_view.rs:832-856).
func textViewSelectionPaint(state gpui.Entity[TextViewState], hitbox gpui.Hitbox, touchHandles *TouchHandleLayout, w *gpui.Window, app *gpui.App) {
	adapter, ok := textViewBridgeOf(state, app)
	if !ok {
		return
	}
	type facts struct {
		scrollOffset gpui.Point
		bounds       gpui.Bounds
		selfScroll   bool
		color        gpui.Hsla
	}
	f := state.Read(app, func(s *TextViewState, _ *gpui.App) facts {
		return facts{s.scrollOffset(), s.bounds, s.scrollable, s.textViewStyle.Selection().Opacity(1)}
	})
	GlobalStateInit(app)
	documentOrder := gpui.NextSelectionDocumentOrder(app)
	adapter.register(hitbox, f.bounds, f.scrollOffset, documentOrder, f.selfScroll, w, app)
	// The handles of a touch selection go over the text, and under
	// whatever is painted over the text after it.
	adapter.paintTouchHandles(touchHandles, f.color, w, app)
}

// textViewRegisterSelectionEdges records an inline's painted selection
// ends with its view (U:text/inline.rs:724-746, 996-1001).
func textViewRegisterSelectionEdges(view gpui.Entity[TextViewState], selection InputSelection, layout *gpui.TextLayout, app *gpui.App) {
	adapter, ok := textViewBridgeOf(view, app)
	if !ok {
		return
	}
	start, end := min(selection.Start, selection.End), max(selection.Start, selection.End)
	lineHeight := layout.LineHeight()
	startPos, ok := layout.PositionForIndex(start)
	if !ok {
		return
	}
	endPos, ok := layout.PositionForIndex(end)
	if !ok {
		return
	}
	adapter.registerSelectionEdges(caretLineBox(startPos, lineHeight), caretLineBox(endPos, lineHeight))
}
