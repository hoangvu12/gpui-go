package base

// Port of U:text_selection.rs (U: = upstream-base/ce/crates/base/src @
// c0820693): window-scoped text selection across elements — the
// participant handle, the per-window selection state with its frame
// registration and sweep, projection onto laid-out runs, copy, scopes,
// drag auto-scroll, word and line multi-click, the touch selection
// lifecycle, and the TextSelectionLayer element.
//
// Go adaptations (recorded deviations):
//
//   - Ownership. Entities need an owner scope in the Go core
//     (gpui.NewEntity). NewTextSelectionHandle takes one; the window
//     selection state is owned by the scope that acquires it: the
//     TextSelectionLayer retains it through a keyed holder entity
//     (Window.UseKeyedState), whose dependent scope holds the lease, so
//     the state is released when the layer stops drawing or its window
//     closes, as Rust drops the retained Entity. State acquired outside the
//     layer (ObserveTouchSelection, the tests' ensure) is owned by the
//     window's scope.
//   - Hitbox's content mask is unexported in gpui. A registration records
//     the window's content mask when it is registered (the mask its
//     participant inserted the hitbox under); one registered directly on
//     the state (tests) falls back to the hitbox bounds.
//   - TextLayout exposes no line layouts: text_rows_extent's last row top
//     is the caret position after the last character
//     (TextLayout.PositionForIndex(len)), which places the trailing empty
//     line the same way. The extent stays covering, as upstream requires.
//   - Subscriptions. The core has no App-level subscribe/observe:
//     TextSelectionHandle.Subscribe registers a self-subscription on the
//     participant (its callbacks run while the participant is updated, so
//     they must not read it back through the handle);
//     TextSelectionObserveTouchSelection observes through a small observer
//     entity in the window's scope.
//   - std::panic::catch_unwind in with_text_selection_scope is a deferred
//     pop: the stack is restored when a subtree panics, and the panic
//     propagates.
//   - Rust's Option values are gpui.Optional; enum events are a struct with
//     a kind.

import (
	"sort"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Identities and snapshots (U:text_selection.rs:22-204)
// ---------------------------------------------------------------------------

// TextSelectionScopeID is an opaque selection layer identifier
// (U:text_selection.rs:22-45). The zero value is the default scope.
type TextSelectionScopeID uint64

var textSelectionNextScopeID atomic.Uint64

// NewTextSelectionScopeID allocates a process-unique scope identifier
// (U:text_selection.rs:27-39). Keep it for the semantic lifetime of the
// scope; do not allocate a new one every frame.
func NewTextSelectionScopeID() TextSelectionScopeID {
	value := textSelectionNextScopeID.Add(1)
	if value == 0 {
		panic("text selection scope identifiers exhausted")
	}
	return TextSelectionScopeID(value)
}

// TextSelectionContentKey is a stable participant-defined identity for
// virtualized participant content (U:text_selection.rs:47-61).
type TextSelectionContentKey uint64

// NewTextSelectionContentKey creates a key from a participant-defined
// stable content identity (U:text_selection.rs:52-55).
func NewTextSelectionContentKey(value uint64) TextSelectionContentKey {
	return TextSelectionContentKey(value)
}

// Value is the participant-defined value (U:text_selection.rs:57-60).
func (k TextSelectionContentKey) Value() uint64 { return uint64(k) }

// TextSelectionEndpoint is a selection endpoint anchored to a
// participant's content coordinates (U:text_selection.rs:63-101).
type TextSelectionEndpoint struct {
	entityID   gpui.Optional[gpui.EntityID]
	point      gpui.Point
	contentKey gpui.Optional[TextSelectionContentKey]
}

// newTextSelectionEndpoint is TextSelectionEndpoint::new
// (U:text_selection.rs:72-79).
func newTextSelectionEndpoint(entityID gpui.Optional[gpui.EntityID], point gpui.Point) TextSelectionEndpoint {
	return TextSelectionEndpoint{entityID: entityID, point: point}
}

// withContentKey is U:text_selection.rs:81-85.
func (e TextSelectionEndpoint) withContentKey(key TextSelectionContentKey) TextSelectionEndpoint {
	e.contentKey = gpui.Some(key)
	return e
}

// EntityID is the participant owning this endpoint, when it hit one
// (U:text_selection.rs:87-90).
func (e TextSelectionEndpoint) EntityID() gpui.Optional[gpui.EntityID] { return e.entityID }

// ContentPoint is the participant-relative content point
// (U:text_selection.rs:92-95).
func (e TextSelectionEndpoint) ContentPoint() gpui.Point { return e.point }

// ContentKey is the participant-defined endpoint metadata captured when
// it hit a participant (U:text_selection.rs:97-100).
func (e TextSelectionEndpoint) ContentKey() gpui.Optional[TextSelectionContentKey] {
	return e.contentKey
}

// TextSelectionWindowPoints are the window-coordinate anchor and cursor
// points for painting a selection (U:text_selection.rs:103-120).
type TextSelectionWindowPoints struct {
	anchor, cursor gpui.Point
}

// Anchor is the stable anchor in window coordinates.
func (p TextSelectionWindowPoints) Anchor() gpui.Point { return p.anchor }

// Cursor is the moving cursor in window coordinates.
func (p TextSelectionWindowPoints) Cursor() gpui.Point { return p.cursor }

// TextSelectionCoverage is how much of one participant participates in a
// window selection (U:text_selection.rs:132-144).
type TextSelectionCoverage uint8

const (
	// TextSelectionCoverageBounded selects only the interval between this
	// participant's two endpoints (the default).
	TextSelectionCoverageBounded TextSelectionCoverage = iota
	// TextSelectionCoverageFromStart selects the participant from its
	// beginning through its endpoint.
	TextSelectionCoverageFromStart
	// TextSelectionCoverageToEnd selects the participant from its endpoint
	// through its end.
	TextSelectionCoverageToEnd
	// TextSelectionCoverageFull: the entire participant lies between
	// endpoints in other participants.
	TextSelectionCoverageFull
)

// TextSelectionSnapshot is participant-relative selection endpoints with
// an optional rendering projection (U:text_selection.rs:122-204).
type TextSelectionSnapshot struct {
	anchor, cursor TextSelectionEndpoint
	isSelecting    bool
	windowPoints   gpui.Optional[TextSelectionWindowPoints]
	coverage       TextSelectionCoverage
}

// newTextSelectionSnapshot is TextSelectionSnapshot::new
// (U:text_selection.rs:147-156).
func newTextSelectionSnapshot(anchor, cursor TextSelectionEndpoint) TextSelectionSnapshot {
	return TextSelectionSnapshot{anchor: anchor, cursor: cursor}
}

// withSelecting is U:text_selection.rs:158-162.
func (s TextSelectionSnapshot) withSelecting(selecting bool) TextSelectionSnapshot {
	s.isSelecting = selecting
	return s
}

// withWindowPoints is U:text_selection.rs:164-171.
func (s TextSelectionSnapshot) withWindowPoints(points gpui.Optional[TextSelectionWindowPoints]) TextSelectionSnapshot {
	s.windowPoints = points
	return s
}

// withCoverage is U:text_selection.rs:173-178 (test-only upstream).
func (s TextSelectionSnapshot) withCoverage(coverage TextSelectionCoverage) TextSelectionSnapshot {
	s.coverage = coverage
	return s
}

// Anchor is the stable anchor endpoint.
func (s TextSelectionSnapshot) Anchor() TextSelectionEndpoint { return s.anchor }

// Cursor is the moving cursor endpoint.
func (s TextSelectionSnapshot) Cursor() TextSelectionEndpoint { return s.cursor }

// IsSelecting reports whether the pointer gesture is still active.
func (s TextSelectionSnapshot) IsSelecting() bool { return s.isSelecting }

// WindowPoints is the window-coordinate endpoints for participants that
// need them.
func (s TextSelectionSnapshot) WindowPoints() gpui.Optional[TextSelectionWindowPoints] {
	return s.windowPoints
}

// Coverage is the portion of the receiving participant covered by this
// selection.
func (s TextSelectionSnapshot) Coverage() TextSelectionCoverage { return s.coverage }

// ---------------------------------------------------------------------------
// Registration (U:text_selection.rs:206-354)
// ---------------------------------------------------------------------------

// textSelectionRenderedMarker is RenderedMarker (U:text_selection.rs:
// 206-213): retained element state proving a participant is still in the
// window.
type textSelectionRenderedMarker struct{}

// TextSelectionRegistration is the per-frame geometry reported by a
// TextSelectionHandle participant (U:text_selection.rs:215-226).
type TextSelectionRegistration struct {
	hitbox         gpui.Hitbox
	bounds         gpui.Bounds
	scrollOffset   gpui.Point
	scope          TextSelectionScopeID
	documentOrder  uint64
	textBounds     []gpui.Bounds
	selfScroll     bool
	selectionEdges gpui.Optional[[2]gpui.Bounds]
	rendered       gpui.Optional[gpui.WeakEntity[textSelectionRenderedMarker]]
	// contentMask is the content mask the participant registered under
	// (Hitbox::content_mask; see the file comment).
	contentMask gpui.Optional[gpui.Bounds]
}

// NewTextSelectionRegistration creates a registration with the default
// scope, order and scroll offset (U:text_selection.rs:229-242).
func NewTextSelectionRegistration(hitbox gpui.Hitbox, bounds gpui.Bounds) TextSelectionRegistration {
	return TextSelectionRegistration{hitbox: hitbox, bounds: bounds}
}

// WithRenderedElement ties this registration to the retained state of the
// element reporting it, so a frame replayed from that element's cached
// view keeps it (U:text_selection.rs:244-269). Call it from the reporting
// element's prepaint or paint.
func (r TextSelectionRegistration) WithRenderedElement(participant TextSelectionHandle, w *gpui.Window, app *gpui.App) TextSelectionRegistration {
	marker := w.UseKeyedState(
		gpui.NamedIntegerElementID("text-selection-participant", uint64(participant.EntityID())),
		app,
		func(*gpui.Window, *gpui.Context[textSelectionRenderedMarker]) textSelectionRenderedMarker {
			return textSelectionRenderedMarker{}
		},
	)
	r.rendered = gpui.Some(marker.Downgrade())
	return r
}

// isRendered reports whether the element that reported this registration
// is still part of the window (U:text_selection.rs:271-277).
func (r *TextSelectionRegistration) isRendered() bool {
	marker, ok := r.rendered.Get()
	return ok && marker.IsUpgradable()
}

// withSelfScroll marks a participant that scrolls its own content in
// response to the AutoScroll event (U:text_selection.rs:279-286).
func (r TextSelectionRegistration) withSelfScroll(selfScroll bool) TextSelectionRegistration {
	r.selfScroll = selfScroll
	return r
}

// WithScrollOffset sets the participant's content scroll offset
// (U:text_selection.rs:288-292).
func (r TextSelectionRegistration) WithScrollOffset(offset gpui.Point) TextSelectionRegistration {
	r.scrollOffset = offset
	return r
}

// WithScope sets the opaque selection scope (U:text_selection.rs:294-298).
func (r TextSelectionRegistration) WithScope(scope TextSelectionScopeID) TextSelectionRegistration {
	r.scope = scope
	return r
}

// WithDocumentOrder sets the stable logical document order
// (U:text_selection.rs:300-304).
func (r TextSelectionRegistration) WithDocumentOrder(order uint64) TextSelectionRegistration {
	r.documentOrder = order
	return r
}

// WithTextBounds sets the glyph-bearing bounds used to reject blank-only
// gestures (U:text_selection.rs:306-310).
func (r TextSelectionRegistration) WithTextBounds(bounds []gpui.Bounds) TextSelectionRegistration {
	r.textBounds = bounds
	return r
}

// WithSelectionEdges sets where the participant painted the two ends of
// its selection, in window coordinates; the touch handles are drawn there
// (U:text_selection.rs:312-318).
func (r TextSelectionRegistration) WithSelectionEdges(start, end gpui.Bounds) TextSelectionRegistration {
	r.selectionEdges = gpui.Some([2]gpui.Bounds{start, end})
	return r
}

// Hitbox is the participant hitbox (U:text_selection.rs:320-323).
func (r TextSelectionRegistration) Hitbox() gpui.Hitbox { return r.hitbox }

// Bounds is the participant's window-coordinate bounds.
func (r TextSelectionRegistration) Bounds() gpui.Bounds { return r.bounds }

// ScrollOffset is the participant's content scroll offset.
func (r TextSelectionRegistration) ScrollOffset() gpui.Point { return r.scrollOffset }

// Scope is the opaque selection scope.
func (r TextSelectionRegistration) Scope() TextSelectionScopeID { return r.scope }

// DocumentOrder is the stable logical document order.
func (r TextSelectionRegistration) DocumentOrder() uint64 { return r.documentOrder }

// TextBounds is the glyph-bearing bounds used to reject blank-only
// gestures.
func (r TextSelectionRegistration) TextBounds() []gpui.Bounds { return r.textBounds }

// SelectionEdges is the caret line boxes at the participant's selection
// ends (U:text_selection.rs:350-353).
func (r TextSelectionRegistration) SelectionEdges() (start, end gpui.Bounds, ok bool) {
	edges, ok := r.selectionEdges.Get()
	return edges[0], edges[1], ok
}

// maskBounds is hitbox.content_mask.bounds (see the file comment).
func (r *TextSelectionRegistration) maskBounds() gpui.Bounds {
	if mask, ok := r.contentMask.Get(); ok {
		return mask
	}
	return r.hitbox.Bounds
}

// ---------------------------------------------------------------------------
// Runs and projection (U:text_selection.rs:356-594)
// ---------------------------------------------------------------------------

// TextSelectionRun is laid-out text reported by a plain selection
// participant during paint (U:text_selection.rs:356-405).
type TextSelectionRun struct {
	documentOrder uint64
	text          string
	layout        *gpui.TextLayout
	bounds        gpui.Bounds
}

// NewTextSelectionRun creates a laid-out text run; layout must have been
// produced from exactly text (U:text_selection.rs:370-378).
func NewTextSelectionRun(text string, layout *gpui.TextLayout, bounds gpui.Bounds) TextSelectionRun {
	return TextSelectionRun{text: text, layout: layout, bounds: bounds}
}

// WithDocumentOrder sets the run's logical order within the participant.
func (r TextSelectionRun) WithDocumentOrder(order uint64) TextSelectionRun {
	r.documentOrder = order
	return r
}

// DocumentOrder is the run's logical order within its participant.
func (r TextSelectionRun) DocumentOrder() uint64 { return r.documentOrder }

// Text is the exact text used to produce the layout.
func (r TextSelectionRun) Text() string { return r.text }

// Layout is the laid-out glyph geometry.
func (r TextSelectionRun) Layout() *gpui.TextLayout { return r.layout }

// Bounds is the run's window-coordinate paint bounds.
func (r TextSelectionRun) Bounds() gpui.Bounds { return r.bounds }

// TextSelectionProjection is a selection projected onto a participant's
// laid-out text runs (U:text_selection.rs:407-426).
type TextSelectionProjection struct {
	ranges   []gpui.Optional[gpui.TextRange]
	isActive bool
}

// Ranges is the selected UTF-8 byte ranges, paired with the input runs.
func (p TextSelectionProjection) Ranges() []gpui.Optional[gpui.TextRange] { return p.ranges }

// IsActive reports whether the participant participates in the selection.
func (p TextSelectionProjection) IsActive() bool { return p.isActive }

// projectTextSelectionRanges projects a participant snapshot onto
// laid-out plain-text runs, keeping the input order
// (U:text_selection.rs:428-457).
func projectTextSelectionRanges(snapshot gpui.Optional[TextSelectionSnapshot], runs []TextSelectionRun) TextSelectionProjection {
	snap, ok := snapshot.Get()
	if !ok {
		return TextSelectionProjection{ranges: make([]gpui.Optional[gpui.TextRange], len(runs))}
	}
	points, ok := snap.windowPoints.Get()
	if !ok {
		return TextSelectionProjection{ranges: make([]gpui.Optional[gpui.TextRange], len(runs)), isActive: true}
	}
	ranges := make([]gpui.Optional[gpui.TextRange], len(runs))
	for i := range runs {
		ranges[i] = textSelectionRangeForRun(&runs[i], points.anchor, points.cursor)
	}
	return TextSelectionProjection{ranges: ranges, isActive: true}
}

// textSelectionRangeForRun is selection_range_for_run
// (U:text_selection.rs:459-511).
func textSelectionRangeForRun(run *TextSelectionRun, start, end gpui.Point) gpui.Optional[gpui.TextRange] {
	none := gpui.Optional[gpui.TextRange]{}
	if run.layout == nil || len(run.text) != run.layout.Len() || run.text == "" {
		return none
	}
	lineHeight := run.layout.LineHeight()
	// Rows that all miss the band, or all lie strictly inside it with no
	// endpoint on any row, answer the same for every character.
	rowsTop, rowsBottom := textRowsExtent(run.layout, lineHeight)
	bandTop, bandBottom := min(start.Y, end.Y), max(start.Y, end.Y)
	if rowsBottom <= bandTop || rowsTop > bandBottom {
		return none
	}
	if bandTop < rowsTop && bandBottom >= rowsBottom {
		return gpui.Some(gpui.TextRange{Start: 0, End: len(run.text)})
	}
	var selected *gpui.TextRange
	for offset, c := range run.text {
		next := offset + utf8.RuneLen(c)
		position, ok := run.layout.PositionForIndex(offset)
		if !ok {
			continue
		}
		charWidth := lineHeight / 2
		if nextPos, ok := run.layout.PositionForIndex(next); ok && nextPos.Y == position.Y {
			charWidth = nextPos.X - position.X
		}
		if textSelectionPointInBand(position, charWidth, start, end, lineHeight) {
			if selected == nil {
				selected = &gpui.TextRange{Start: offset, End: offset}
			}
			selected.End = next
		}
	}
	if selected == nil {
		return none
	}
	return gpui.Some(*selected)
}

// textRowsExtent is text_rows_extent (U:text_selection.rs:513-533): the
// top of the layout's first row and the bottom of its last, each row
// lineHeight tall. The last row's top is where the caret after the last
// character sits (see the file comment).
func textRowsExtent(layout *gpui.TextLayout, lineHeight float32) (float32, float32) {
	top := layout.Bounds().Top()
	lastRowTop := top
	if position, ok := layout.PositionForIndex(layout.Len()); ok && position.Y > top {
		lastRowTop = position.Y
	}
	return top, lastRowTop + lineHeight
}

// textSelectionPointsForMultiClick is points_for_multi_click
// (U:text_selection.rs:535-557): the window points of the word (two
// clicks) or logical line (three or more) under position.
func textSelectionPointsForMultiClick(runs []TextSelectionRun, position gpui.Point, clickCount int) (gpui.Point, gpui.Point, bool) {
	var run *TextSelectionRun
	for i := range runs {
		if runs[i].bounds.Contains(position) {
			run = &runs[i]
			break
		}
	}
	if run == nil || run.layout == nil || len(run.text) != run.layout.Len() {
		return gpui.Point{}, gpui.Point{}, false
	}
	offset, ok := run.layout.IndexForPosition(position)
	if !ok {
		return gpui.Point{}, gpui.Point{}, false
	}
	var r gpui.TextRange
	switch {
	case clickCount == 2:
		if r, ok = textBoundaryWordRangeAt(run.text, offset); !ok {
			return gpui.Point{}, gpui.Point{}, false
		}
	case clickCount >= 3:
		r = textBoundaryLineRangeAt(run.text, offset)
	default:
		return gpui.Point{}, gpui.Point{}, false
	}
	if r.Start >= r.End {
		return gpui.Point{}, gpui.Point{}, false
	}
	anchor, ok := run.layout.PositionForIndex(r.Start)
	if !ok {
		return gpui.Point{}, gpui.Point{}, false
	}
	cursor, ok := run.layout.PositionForIndex(r.End)
	if !ok {
		return gpui.Point{}, gpui.Point{}, false
	}
	return anchor, cursor, true
}

// textSelectionPointInBand is point_in_selection_band
// (U:text_selection.rs:559-594); U:text/inline.rs:1321-1362 carries the
// same test, ported as pointInTextSelection (text_inline.go).
func textSelectionPointInBand(position gpui.Point, charWidth float32, start, end gpui.Point, lineHeight float32) bool {
	return pointInTextSelection(position, charWidth, start, end, lineHeight)
}

// ---------------------------------------------------------------------------
// Events and the participant state (U:text_selection.rs:596-779)
// ---------------------------------------------------------------------------

// TextSelectionContentKeyResolver looks up the stable virtualized content
// key at a participant content point (U:text_selection.rs:599).
type TextSelectionContentKeyResolver func(point gpui.Point, app *gpui.App) (TextSelectionContentKey, bool)

type (
	textSelectionFocusCallback func(w *gpui.Window, app *gpui.App)
	textSelectionClearHandler  func(app *gpui.App)
	textSelectionCopyCallback  func(app *gpui.App) string
)

// TextSelectionEventKind is the variant of a TextSelectionEvent
// (U:text_selection.rs:601-615).
type TextSelectionEventKind uint8

const (
	// TextSelectionEventSelectionChanged: the participant's window-selection
	// projection changed; Snapshot carries it.
	TextSelectionEventSelectionChanged TextSelectionEventKind = iota
	// TextSelectionEventAutoScroll: the active drag requests vertical
	// auto-scroll by Delta, or stops it when Delta is absent.
	TextSelectionEventAutoScroll
	// TextSelectionEventCleared: window selection cleared the participant's
	// participant-local state.
	TextSelectionEventCleared
	// TextSelectionEventTouchSelectionChanged: the touch selection the
	// participant takes part in changed; it renders once more.
	TextSelectionEventTouchSelectionChanged
)

// TextSelectionEvent is a notification emitted by a text-selection
// participant (U:text_selection.rs:601-615).
type TextSelectionEvent struct {
	Kind     TextSelectionEventKind
	Snapshot gpui.Optional[TextSelectionSnapshot]
	Delta    gpui.Optional[float32]
}

// textSelectionEvents is EventEmitter<TextSelectionEvent> for the
// participant state (U:text_selection.rs:655).
var textSelectionEvents = gpui.DefineEvent[selectableTextState, TextSelectionEvent]()

// textSelectionCopyItem is CopyItem (U:text_selection.rs:617-621).
type textSelectionCopyItem struct {
	documentOrder uint64
	callback      textSelectionCopyCallback
	fallback      string
}

// resolveTextSelectionCopyItems is resolve_copy_items
// (U:text_selection.rs:623-635): copies in document order, blank ones
// dropped, joined by newlines. Callbacks run outside every state lease.
func resolveTextSelectionCopyItems(items []textSelectionCopyItem, app *gpui.App) string {
	sort.SliceStable(items, func(i, j int) bool { return items[i].documentOrder < items[j].documentOrder })
	var texts []string
	for _, item := range items {
		text := item.fallback
		if item.callback != nil {
			text = item.callback(app)
		}
		if strings.TrimSpace(text) != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

// dispatchTextSelectionClearHandlers is U:text_selection.rs:637-641.
func dispatchTextSelectionClearHandlers(handlers []textSelectionClearHandler, app *gpui.App) {
	for _, handler := range handlers {
		handler(app)
	}
}

// selectableTextState is SelectableTextState (U:text_selection.rs:
// 643-779), the state behind a TextSelectionHandle.
type selectableTextState struct {
	fallbackCopyText   string
	projectedCopyText  gpui.Optional[string]
	runs               []TextSelectionRun
	localSelection     bool
	snapshot           gpui.Optional[TextSelectionSnapshot]
	onFocus            textSelectionFocusCallback
	clear              textSelectionClearHandler
	copy               textSelectionCopyCallback
	contentKeyResolver TextSelectionContentKeyResolver
}

// updateRuns projects the snapshot onto runs and caches their selected
// substrings for copying (U:text_selection.rs:688-717).
func (s *selectableTextState) updateRuns(runs []TextSelectionRun) TextSelectionProjection {
	s.runs = append([]TextSelectionRun(nil), runs...)
	states := projectTextSelectionRanges(s.snapshot, runs)
	type selectedRun struct {
		order uint64
		index int
		text  string
	}
	var selected []selectedRun
	for i, run := range runs {
		if r, ok := states.ranges[i].Get(); ok {
			selected = append(selected, selectedRun{run.documentOrder, i, run.text[r.Start:r.End]})
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].order != selected[j].order {
			return selected[i].order < selected[j].order
		}
		return selected[i].index < selected[j].index
	})
	var b strings.Builder
	for _, run := range selected {
		b.WriteString(run.text)
	}
	s.projectedCopyText = gpui.Some(b.String())
	return states
}

// setSnapshot is U:text_selection.rs:741-748.
func (s *selectableTextState) setSnapshot(snapshot gpui.Optional[TextSelectionSnapshot], cx *gpui.Context[selectableTextState]) {
	if s.snapshot == snapshot {
		return
	}
	s.snapshot = snapshot
	s.projectedCopyText = gpui.Optional[string]{}
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventSelectionChanged, Snapshot: snapshot})
}

// clearState is U:text_selection.rs:750-757.
func (s *selectableTextState) clearState(cx *gpui.Context[selectableTextState]) textSelectionClearHandler {
	s.snapshot = gpui.Optional[TextSelectionSnapshot]{}
	s.projectedCopyText = gpui.Optional[string]{}
	s.localSelection = false
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventCleared})
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventSelectionChanged})
	return s.clear
}

// setAutoScroll is U:text_selection.rs:759-761.
func (s *selectableTextState) setAutoScroll(delta gpui.Optional[float32], cx *gpui.Context[selectableTextState]) {
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventAutoScroll, Delta: delta})
}

// focus defers the focus callback to after the current dispatch
// (U:text_selection.rs:763-767).
func (s *selectableTextState) focus(w *gpui.Window, app *gpui.App) {
	if callback := s.onFocus; callback != nil && w != nil {
		w.Defer(app, func(w *gpui.Window, app *gpui.App) { callback(w, app) })
	}
}

// copyItem is U:text_selection.rs:769-778.
func (s *selectableTextState) copyItem(documentOrder uint64) (textSelectionCopyItem, bool) {
	if !s.snapshot.IsSome() && !s.localSelection {
		return textSelectionCopyItem{}, false
	}
	fallback, ok := s.projectedCopyText.Get()
	if !ok {
		fallback = s.fallbackCopyText
	}
	return textSelectionCopyItem{documentOrder: documentOrder, callback: s.copy, fallback: fallback}, true
}

// ---------------------------------------------------------------------------
// The participant handle (U:text_selection.rs:781-974)
// ---------------------------------------------------------------------------

// touchHandleHitbox is one laid-out touch handle with its hitbox.
type touchHandleHitbox struct {
	edge   SelectionEdge
	hitbox gpui.Hitbox
}

// TouchHandleLayout is the touch handles a participant laid out for a
// frame, with their hitboxes (U:text_selection.rs:781-785).
type TouchHandleLayout struct {
	hitboxes []touchHandleHitbox
}

// TextSelectionHandle is a stable, participant-neutral handle for text
// that participates in window selection (U:text_selection.rs:787-789).
// Copies share the participant.
type TextSelectionHandle struct {
	entity gpui.Entity[selectableTextState]
}

// NewTextSelectionHandle creates a participant with fallback text for
// copying (U:text_selection.rs:792-795), owned by owner (see the file
// comment).
func NewTextSelectionHandle(fallbackCopyText string, cx gpui.AppContext, owner *gpui.Scope) TextSelectionHandle {
	return TextSelectionHandle{entity: gpui.NewEntity(cx, owner, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) {
		*s = selectableTextState{fallbackCopyText: fallbackCopyText}
	})}
}

// EntityID is this participant's stable identity
// (U:text_selection.rs:797-800).
func (h TextSelectionHandle) EntityID() gpui.EntityID { return h.entity.EntityID() }

// Snapshot is the participant's current geometry selection snapshot
// (U:text_selection.rs:802-805).
func (h TextSelectionHandle) Snapshot(app *gpui.App) gpui.Optional[TextSelectionSnapshot] {
	return h.entity.Read(app, func(s *selectableTextState, _ *gpui.App) gpui.Optional[TextSelectionSnapshot] {
		return s.snapshot
	})
}

// SetFallbackCopyText sets the text copied while this participant
// participates (U:text_selection.rs:807-811).
func (h TextSelectionHandle) SetFallbackCopyText(text string, app *gpui.App) {
	h.entity.Update(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) {
		s.fallbackCopyText = text
		s.projectedCopyText = gpui.Optional[string]{}
	})
}

// SetLocalSelection marks participant-local selection, such as
// select-all, as active (U:text_selection.rs:813-817).
func (h TextSelectionHandle) SetLocalSelection(active bool, app *gpui.App) {
	h.entity.Update(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) {
		s.localSelection = active
	})
}

// HasLocalSelection reports whether participant-local selection is active
// (U:text_selection.rs:819-822).
func (h TextSelectionHandle) HasLocalSelection(app *gpui.App) bool {
	return h.entity.Read(app, func(s *selectableTextState, _ *gpui.App) bool {
		return s.localSelection
	})
}

// Register registers this participant and its geometry for the current
// frame (U:text_selection.rs:824-840). It is a no-op until the window
// renders a TextSelectionLayer.
func (h TextSelectionHandle) Register(registration TextSelectionRegistration, w *gpui.Window, app *gpui.App) {
	if scope, ok := currentTextSelectionScope(w.ID(), app); ok {
		registration.scope = scope
	}
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return
	}
	if !registration.contentMask.IsSome() {
		if mask, ok := textSelectionWindowContentMask(w); ok {
			registration.contentMask = gpui.Some(mask)
		}
	}
	_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
		s.registerParticipant(h, &registration, cx.App())
	})
}

// textSelectionWindowContentMask is the content mask of the frame being
// drawn, false outside a draw (gpui.ContentMask panics there).
func textSelectionWindowContentMask(w *gpui.Window) (mask gpui.Bounds, ok bool) {
	defer func() {
		if recover() != nil {
			mask, ok = gpui.Bounds{}, false
		}
	}()
	return gpui.ContentMask(w), true
}

// UpdateRuns projects the current snapshot onto plain-text runs and
// caches their copy text (U:text_selection.rs:842-845).
func (h TextSelectionHandle) UpdateRuns(runs []TextSelectionRun, app *gpui.App) TextSelectionProjection {
	return h.entity.UpdateWith(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) TextSelectionProjection {
		return s.updateRuns(runs)
	})
}

// setHitTestRuns retains geometry for word hit testing; rich text renders
// its own selection (U:text_selection.rs:847-850).
func (h TextSelectionHandle) setHitTestRuns(runs []TextSelectionRun, app *gpui.App) {
	h.entity.Update(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) {
		s.runs = append([]TextSelectionRun(nil), runs...)
	})
}

// Subscribe subscribes to participant selection notifications
// (U:text_selection.rs:852-859). See the file comment.
func (h TextSelectionHandle) Subscribe(callback func(event *TextSelectionEvent, app *gpui.App), app *gpui.App) gpui.Subscription {
	return h.entity.UpdateWith(app, func(_ *selectableTextState, cx *gpui.Context[selectableTextState]) gpui.Subscription {
		return textSelectionEvents.SubscribeSelf(cx, func(_ *selectableTextState, event *TextSelectionEvent, cx *gpui.Context[selectableTextState]) {
			callback(event, cx.App())
		})
	})
}

// RefreshWindowOnChange subscribes w to refresh whenever this
// participant's selection changes (U:text_selection.rs:861-873). Retain
// the subscription or detach it.
func (h TextSelectionHandle) RefreshWindowOnChange(w *gpui.Window, app *gpui.App) gpui.Subscription {
	windowID := w.ID()
	return h.Subscribe(func(event *TextSelectionEvent, app *gpui.App) {
		if event.Kind != TextSelectionEventSelectionChanged {
			return
		}
		if w, ok := app.Window(windowID); ok {
			w.Refresh()
		}
	}, app)
}

// FocusWith sets the callback that focuses the participant when a drag
// begins in it (U:text_selection.rs:875-879).
func (h TextSelectionHandle) FocusWith(callback func(w *gpui.Window, app *gpui.App), app *gpui.App) {
	h.entity.Update(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) { s.onFocus = callback })
}

// ClearWith sets the synchronous participant cleanup command used by
// window clear (U:text_selection.rs:881-884).
func (h TextSelectionHandle) ClearWith(callback func(app *gpui.App), app *gpui.App) {
	h.entity.Update(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) { s.clear = callback })
}

// CopyWith sets a participant-specific copy projection
// (U:text_selection.rs:886-889).
func (h TextSelectionHandle) CopyWith(callback func(app *gpui.App) string, app *gpui.App) {
	h.entity.Update(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) { s.copy = callback })
}

// ResolveContentKeyWith sets a participant-specific lookup for stable
// virtualized content keys (U:text_selection.rs:961-969).
func (h TextSelectionHandle) ResolveContentKeyWith(callback TextSelectionContentKeyResolver, app *gpui.App) {
	h.entity.Update(app, func(s *selectableTextState, _ *gpui.Context[selectableTextState]) {
		s.contentKeyResolver = callback
	})
}

// PrepaintTouchHandles lays out the touch handles at the ends of this
// participant's selection, as painted last frame, each with a hitbox
// where the finger takes it. Call during the participant's prepaint and
// hand the result to PaintTouchHandles (U:text_selection.rs:891-912).
func (h TextSelectionHandle) PrepaintTouchHandles(w *gpui.Window, app *gpui.App) TouchHandleLayout {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return TouchHandleLayout{}
	}
	handles, _ := state.TryRead(app, func(s *windowSelectionState, _ *gpui.App) []touchHandleCaret {
		return s.touchHandlesOf(h.EntityID())
	})
	var layout TouchHandleLayout
	for _, handle := range handles {
		hitbox := w.InsertHitbox(TouchHandleHitBounds(handle.edge, handle.caret), gpui.HitboxBlockMouse)
		layout.hitboxes = append(layout.hitboxes, touchHandleHitbox{edge: handle.edge, hitbox: hitbox})
	}
	return layout
}

// PaintTouchHandles paints the touch handles at the ends of this
// participant's selection in color, where the participant is in the paint
// order, and takes the finger's drag from them. Call at the end of the
// participant's paint, after Register (U:text_selection.rs:914-959).
func (h TextSelectionHandle) PaintTouchHandles(layout *TouchHandleLayout, color gpui.Hsla, w *gpui.Window, app *gpui.App) {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return
	}
	handles, _ := state.TryRead(app, func(s *windowSelectionState, _ *gpui.App) []touchHandleCaret {
		return s.touchHandlesOf(h.EntityID())
	})
	for _, handle := range handles {
		TouchHandlePaint(handle.edge, handle.caret, color, w)
	}
	if layout == nil {
		return
	}
	for _, laid := range layout.hitboxes {
		edge, hitbox := laid.edge, laid.hitbox
		_ = state.TryUpdate(app, func(s *windowSelectionState, _ *gpui.Context[windowSelectionState]) {
			s.registerTouchUI(hitbox.Bounds)
		})
		// Mouse: the drag for a pointer.
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.MouseDownEvent)
			if !ok || phase != gpui.DispatchBubble || ev.Button != gpui.MouseButtonLeft || !hitbox.IsHovered(w) {
				return
			}
			if !state.IsUpgradable() {
				return
			}
			app.StopPropagation()
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
				s.beginEdgeDrag(edge, ev.Position, w, cx.App())
			})
			textSelectionResolveContentKeys(state, app)
		})
	}
}

// ---------------------------------------------------------------------------
// The window selection state (U:text_selection.rs:976-2213)
// ---------------------------------------------------------------------------

// participantRegistration is ParticipantRegistration
// (U:text_selection.rs:976-981).
type participantRegistration struct {
	participant  gpui.WeakEntity[selectableTextState]
	registration *TextSelectionRegistration
	generation   uint64
}

// selectionEndpoint is SelectionEndpoint (U:text_selection.rs:983-1022).
type selectionEndpoint struct {
	participant   gpui.Optional[gpui.WeakEntity[selectableTextState]]
	point         gpui.Point
	inside        bool
	insideText    bool
	contentKey    gpui.Optional[TextSelectionContentKey]
	resolver      TextSelectionContentKeyResolver
	resolverPoint gpui.Point
}

// clone copies the endpoint (Rust's Clone).
func (e *selectionEndpoint) clone() *selectionEndpoint {
	if e == nil {
		return nil
	}
	c := *e
	return &c
}

// snapshot is U:text_selection.rs:994-1001.
func (e *selectionEndpoint) snapshot() TextSelectionEndpoint {
	snapshot := newTextSelectionEndpoint(e.entityID(), e.point)
	if key, ok := e.contentKey.Get(); ok {
		return snapshot.withContentKey(key)
	}
	return snapshot
}

// resolve is the endpoint in window coordinates under the participant's
// current registration (U:text_selection.rs:1003-1015).
func (e *selectionEndpoint) resolve(participants map[gpui.EntityID]participantRegistration) (gpui.Point, bool) {
	participant, ok := e.participant.Get()
	if !ok {
		return gpui.Point{}, false
	}
	registration, ok := participants[participant.EntityID()]
	if !ok || !participant.IsUpgradable() {
		return gpui.Point{}, false
	}
	return textPointAdd(textPointAdd(e.point, registration.registration.scrollOffset), registration.registration.bounds.Origin), true
}

// entityID is U:text_selection.rs:1017-1021.
func (e *selectionEndpoint) entityID() gpui.Optional[gpui.EntityID] {
	if participant, ok := e.participant.Get(); ok {
		return gpui.Some(participant.EntityID())
	}
	return gpui.Optional[gpui.EntityID]{}
}

// touchEdges is the ends as last laid out (U:text_selection.rs:1040-1044).
type touchEdges struct {
	start, end               gpui.Bounds
	startVisible, endVisible bool
}

// touchSelection is what a long press left behind: the handles and the
// edit menu (U:text_selection.rs:1024-1071).
type touchSelection struct {
	// active: the current selection was made by touch and carries handles.
	active   bool
	menuOpen bool
	drag     gpui.Optional[edgeDrag]
	// uiBounds and previousUIBounds are where the handles and the menu
	// were painted this frame and the frame before.
	uiBounds, previousUIBounds []gpui.Bounds
	lastEdges                  gpui.Optional[touchEdges]
}

// covers is U:text_selection.rs:1048-1055.
func (t *touchSelection) covers(position gpui.Point) bool {
	if !t.active {
		return false
	}
	for _, b := range t.uiBounds {
		if b.Contains(position) {
			return true
		}
	}
	for _, b := range t.previousUIBounds {
		if b.Contains(position) {
			return true
		}
	}
	return false
}

// beginFrame keeps what was painted last frame one frame more
// (U:text_selection.rs:1057-1060).
func (t *touchSelection) beginFrame() {
	t.previousUIBounds = t.uiBounds
	t.uiBounds = nil
}

// reset drops the handles and the menu, reporting whether there were any
// (U:text_selection.rs:1062-1070).
func (t *touchSelection) reset() bool {
	had := t.active
	t.active = false
	t.menuOpen = false
	t.drag = gpui.Optional[edgeDrag]{}
	t.lastEdges = gpui.Optional[touchEdges]{}
	return had
}

// autoScrollStallGeometry is the anchor geometry auto_scroll_stall keeps
// (U:text_selection.rs:1088-1092).
type autoScrollStallGeometry struct {
	bounds gpui.Bounds
	scroll gpui.Point
}

// windowSelectionState is WindowSelectionState (U:text_selection.rs:
// 1073-1097), the window-local generic text-selection state. Its zero
// value is the Rust Default.
type windowSelectionState struct {
	participants           map[gpui.EntityID]participantRegistration
	activeScope            TextSelectionScopeID
	anchor                 *selectionEndpoint
	cursor                 *selectionEndpoint
	pendingExtensionAnchor *selectionEndpoint
	isSelecting            bool
	didHitText             bool
	frameGeneration        uint64
	finishFrameScheduled   bool
	refreshHeldCursor      bool
	mouseDownPrepared      bool
	autoScroll             AutoScroll
	autoScrollStall        gpui.Optional[autoScrollStallGeometry]
	autoScrollStalled      uint8
	touch                  touchSelection
	// entityID is this entity, so touch changes can notify observers from
	// paths that only hold an App.
	entityID gpui.Optional[gpui.EntityID]
}

// textSelectionResolveContentKeys runs the endpoints' content key
// resolvers outside the state lease and stores their keys
// (U:text_selection.rs:1100-1126).
func textSelectionResolveContentKeys(state gpui.WeakEntity[windowSelectionState], app *gpui.App) {
	type pending struct {
		resolver TextSelectionContentKeyResolver
		point    gpui.Point
	}
	pendingKeys, err := state.TryUpdateWith(app, func(s *windowSelectionState, _ *gpui.Context[windowSelectionState]) [2]pending {
		var out [2]pending
		for i, endpoint := range []*selectionEndpoint{s.anchor, s.cursor} {
			if endpoint != nil && endpoint.resolver != nil {
				out[i] = pending{endpoint.resolver, endpoint.resolverPoint}
			}
		}
		return out
	})
	if err != nil {
		return
	}
	var resolved [2]gpui.Optional[TextSelectionContentKey]
	for i, p := range pendingKeys {
		if p.resolver != nil {
			if key, ok := p.resolver(p.point, app); ok {
				resolved[i] = gpui.Some(key)
			}
		}
	}
	_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
		for i, endpoint := range []*selectionEndpoint{s.anchor, s.cursor} {
			if key, ok := resolved[i].Get(); ok && endpoint != nil {
				endpoint.contentKey = gpui.Some(key)
				endpoint.resolver = nil
			}
		}
		s.publishSnapshots(cx.App())
	})
}

// selectionStateRegistry is SelectionStateRegistry
// (U:text_selection.rs:2215-2220): a non-owning window locator; the
// retained TextSelectionLayer state owns each live selection entity.
type selectionStateRegistry struct {
	states map[gpui.WindowID]gpui.WeakEntity[windowSelectionState]
}

func textSelectionRegistry(app *gpui.App) *selectionStateRegistry {
	if r, ok := gpui.TryGlobal[*selectionStateRegistry](app); ok {
		return r
	}
	r := &selectionStateRegistry{states: map[gpui.WindowID]gpui.WeakEntity[windowSelectionState]{}}
	app.SetGlobal(r)
	return r
}

// pendingTextSelectionScopes is PendingTextSelectionScopes
// (U:text_selection.rs:2222-2225).
type pendingTextSelectionScopes struct {
	scopes map[gpui.WindowID]TextSelectionScopeID
}

// textSelectionStateAcquire is WindowSelectionState::acquire
// (U:text_selection.rs:1127-1177): the window's live state, retained into
// owner, or a new one owned by owner.
func textSelectionStateAcquire(windowID gpui.WindowID, app *gpui.App, owner *gpui.Scope) gpui.WeakEntity[windowSelectionState] {
	registry := textSelectionRegistry(app)
	if state, ok := registry.states[windowID]; ok && state.IsUpgradable() {
		state.UpgradeInto(owner)
		return state
	}
	var activeScope TextSelectionScopeID
	if pending, ok := gpui.TryGlobal[*pendingTextSelectionScopes](app); ok {
		activeScope = pending.scopes[windowID]
		delete(pending.scopes, windowID)
	}
	state := gpui.NewEntity(app, owner, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
		entityID := cx.EntityID()
		cx.OnRelease(func(s *windowSelectionState, app *gpui.App) {
			handlers := s.clearState(app)
			if registry, ok := gpui.TryGlobal[*selectionStateRegistry](app); ok {
				if current, ok := registry.states[windowID]; ok && current.EntityID() == entityID {
					delete(registry.states, windowID)
				}
			}
			if len(handlers) > 0 {
				app.Defer(func(app *gpui.App) { dispatchTextSelectionClearHandlers(handlers, app) })
			}
		}).Detach()
		*s = windowSelectionState{activeScope: activeScope, entityID: gpui.Some(entityID)}
	})
	weak := state.Downgrade()
	textSelectionRegistry(app).states[windowID] = weak
	return weak
}

// textSelectionStateEnsure is WindowSelectionState::ensure
// (U:text_selection.rs:1179-1182, test-only upstream), owned by the
// window's scope.
func textSelectionStateEnsure(w *gpui.Window, app *gpui.App) gpui.WeakEntity[windowSelectionState] {
	return textSelectionStateAcquire(w.ID(), app, w.Scope())
}

// textSelectionStateExisting is WindowSelectionState::existing
// (U:text_selection.rs:1184-1192).
func textSelectionStateExisting(w *gpui.Window, app *gpui.App) (gpui.WeakEntity[windowSelectionState], bool) {
	registry, ok := gpui.TryGlobal[*selectionStateRegistry](app)
	if !ok {
		return gpui.WeakEntity[windowSelectionState]{}, false
	}
	state, ok := registry.states[w.ID()]
	if !ok || !state.IsUpgradable() {
		return gpui.WeakEntity[windowSelectionState]{}, false
	}
	return state, true
}

// setActiveScope updates the active scope (U:text_selection.rs:1194-1199,
// test-only upstream).
func (s *windowSelectionState) setActiveScope(scope TextSelectionScopeID, app *gpui.App) {
	dispatchTextSelectionClearHandlers(s.setActiveScopeState(scope, app), app)
}

// setActiveScopeState is U:text_selection.rs:1201-1213.
func (s *windowSelectionState) setActiveScopeState(scope TextSelectionScopeID, app *gpui.App) []textSelectionClearHandler {
	if s.activeScope == scope {
		return nil
	}
	handlers := s.clearState(app)
	s.activeScope = scope
	s.publishSnapshots(app)
	return handlers
}

// finishFrame sweeps participants after a rendered frame completed
// (U:text_selection.rs:1215-1249). A participant that missed the
// generation is swept only when its element left the window.
func (s *windowSelectionState) finishFrame(app *gpui.App) []textSelectionClearHandler {
	s.finishFrameScheduled = false
	type stale struct {
		id          gpui.EntityID
		participant gpui.WeakEntity[selectableTextState]
	}
	var swept []stale
	for id, registration := range s.participants {
		if registration.generation != s.frameGeneration && !registration.registration.isRendered() {
			swept = append(swept, stale{id, registration.participant})
		}
	}
	var handlers []textSelectionClearHandler
	for _, p := range swept {
		delete(s.participants, p.id)
		handler, err := p.participant.TryUpdateWith(app, func(state *selectableTextState, cx *gpui.Context[selectableTextState]) textSelectionClearHandler {
			return state.clearState(cx)
		})
		if err == nil && handler != nil {
			handlers = append(handlers, handler)
		}
	}
	s.publishSnapshots(app)
	s.frameGeneration++
	return handlers
}

// scheduleFinishFrame is U:text_selection.rs:1251-1257.
func (s *windowSelectionState) scheduleFinishFrame() bool {
	if s.finishFrameScheduled {
		return false
	}
	s.finishFrameScheduled = true
	return true
}

// registerParticipant registers this frame's geometry for a participant
// (U:text_selection.rs:1259-1302).
func (s *windowSelectionState) registerParticipant(selection TextSelectionHandle, registration *TextSelectionRegistration, app *gpui.App) {
	s.pruneDeadParticipants()
	id := selection.EntityID()
	previous, hadPrevious := s.participants[id]
	if s.isSelecting && registration.selfScroll && s.anchor != nil &&
		s.anchor.entityID() == gpui.Some(id) && hadPrevious &&
		(previous.registration.scrollOffset != registration.scrollOffset || previous.registration.bounds != registration.bounds) {
		s.refreshHeldCursor = true
	}
	// The handles sit on the painted ends; when those moved, whoever draws
	// the handles needs to know.
	edgesMoved := s.touch.active && (!hadPrevious || previous.registration.selectionEdges != registration.selectionEdges)
	if s.participants == nil {
		s.participants = map[gpui.EntityID]participantRegistration{}
	}
	s.participants[id] = participantRegistration{
		participant:  selection.entity.Downgrade(),
		registration: registration,
		generation:   s.frameGeneration,
	}
	s.publishSnapshots(app)
	if edgesMoved {
		s.touchChanged(app)
	}
}

// begin starts a gesture using bounds hit testing
// (U:text_selection.rs:1304-1308, test-only upstream).
func (s *windowSelectionState) begin(position gpui.Point, extend bool, app *gpui.App) {
	s.beginImpl(position, extend, false, nil, app)
}

// update moves the gesture using bounds hit testing
// (U:text_selection.rs:1310-1314, test-only upstream).
func (s *windowSelectionState) update(position gpui.Point, app *gpui.App) {
	s.updateImpl(position, nil, app)
}

// end ends the current gesture and keeps its selection visible
// (U:text_selection.rs:1316-1329).
func (s *windowSelectionState) end(app *gpui.App) {
	s.pendingExtensionAnchor = nil
	if !s.isSelecting {
		return
	}
	s.isSelecting = false
	if !s.didHitText {
		s.anchor = nil
		s.cursor = nil
	}
	s.stopAnchorAutoScroll(app)
	s.publishSnapshots(app)
}

// clear clears both window selection and every participant's local
// selection (U:text_selection.rs:1331-1335).
func (s *windowSelectionState) clear(app *gpui.App) {
	dispatchTextSelectionClearHandlers(s.clearState(app), app)
}

// clearState is U:text_selection.rs:1337-1353.
func (s *windowSelectionState) clearState(app *gpui.App) []textSelectionClearHandler {
	s.stopAnchorAutoScroll(app)
	s.anchor = nil
	s.cursor = nil
	s.pendingExtensionAnchor = nil
	s.isSelecting = false
	s.didHitText = false
	if s.touch.reset() {
		s.touchChanged(app)
	}
	s.pruneDeadParticipants()
	return s.clearParticipants(app)
}

// clearParticipants clears every live participant, collecting their clear
// handlers.
func (s *windowSelectionState) clearParticipants(app *gpui.App) []textSelectionClearHandler {
	var handlers []textSelectionClearHandler
	for _, registration := range s.participants {
		handler, err := registration.participant.TryUpdateWith(app, func(state *selectableTextState, cx *gpui.Context[selectableTextState]) textSelectionClearHandler {
			return state.clearState(cx)
		})
		if err == nil && handler != nil {
			handlers = append(handlers, handler)
		}
	}
	return handlers
}

// touchChanged tells whoever draws the handles and the menu that they
// changed, deferred so a change raised inside a draw starts a frame; the
// participants in the selection hear it too (U:text_selection.rs:
// 1355-1385).
func (s *windowSelectionState) touchChanged(app *gpui.App) {
	var participants []gpui.WeakEntity[selectableTextState]
	for _, registration := range s.participants {
		selected, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *gpui.App) bool {
			return p.snapshot.IsSome()
		})
		if err == nil && selected {
			participants = append(participants, registration.participant)
		}
	}
	entityID := s.entityID
	app.Defer(func(app *gpui.App) {
		for _, participant := range participants {
			_ = participant.TryUpdate(app, func(_ *selectableTextState, cx *gpui.Context[selectableTextState]) {
				textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventTouchSelectionChanged})
			})
		}
		if id, ok := entityID.Get(); ok {
			app.Notify(id)
		}
	})
}

// copyItems is U:text_selection.rs:1387-1397.
func (s *windowSelectionState) copyItems(app *gpui.App) []textSelectionCopyItem {
	var items []textSelectionCopyItem
	for _, registration := range s.participants {
		order := registration.registration.documentOrder
		item, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *gpui.App) gpui.Optional[textSelectionCopyItem] {
			if item, ok := p.copyItem(order); ok {
				return gpui.Some(item)
			}
			return gpui.Optional[textSelectionCopyItem]{}
		})
		if value, ok := item.Get(); err == nil && ok {
			items = append(items, value)
		}
	}
	return items
}

// selectedText is U:text_selection.rs:1399-1402 (test-only upstream).
func (s *windowSelectionState) selectedText(app *gpui.App) string {
	return resolveTextSelectionCopyItems(s.copyItems(app), app)
}

// selectsText reports whether the endpoints take in at least one
// character of some participant's painted text
// (U:text_selection.rs:1404-1421).
func (s *windowSelectionState) selectsText(app *gpui.App) bool {
	if !s.snapshot().IsSome() {
		return false
	}
	for _, registration := range s.participants {
		selects, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *gpui.App) bool {
			for _, r := range projectTextSelectionRanges(p.snapshot, p.runs).ranges {
				if value, ok := r.Get(); ok && value.Start < value.End {
					return true
				}
			}
			return false
		})
		if err == nil && selects {
			return true
		}
	}
	return false
}

// hasSelection reports whether a drag or a participant-local selection is
// active (U:text_selection.rs:1423-1432).
func (s *windowSelectionState) hasSelection(app *gpui.App) bool {
	if s.snapshot().IsSome() {
		return true
	}
	for _, registration := range s.participants {
		local, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *gpui.App) bool {
			return p.localSelection
		})
		if err == nil && local {
			return true
		}
	}
	return false
}

// snapshot is the current resolved selection (U:text_selection.rs:
// 1434-1448).
func (s *windowSelectionState) snapshot() gpui.Optional[TextSelectionSnapshot] {
	none := gpui.Optional[TextSelectionSnapshot]{}
	if !s.didHitText || s.anchor == nil || s.cursor == nil {
		return none
	}
	anchor, ok := s.anchor.resolve(s.participants)
	if !ok {
		return none
	}
	cursor, ok := s.cursor.resolve(s.participants)
	if !ok || anchor == cursor {
		return none
	}
	return gpui.Some(newTextSelectionSnapshot(s.anchor.snapshot(), s.cursor.snapshot()).
		withSelecting(s.isSelecting).
		withWindowPoints(gpui.Some(TextSelectionWindowPoints{anchor: anchor, cursor: cursor})))
}

// prepareForMouseDown is U:text_selection.rs:1456-1476.
func (s *windowSelectionState) prepareForMouseDown(extend bool, app *gpui.App) []textSelectionClearHandler {
	var pending *selectionEndpoint
	if extend {
		pending = s.anchor.clone()
	}
	s.stopAnchorAutoScroll(app)
	s.anchor = nil
	s.cursor = nil
	s.pendingExtensionAnchor = nil
	s.isSelecting = false
	s.didHitText = false
	if s.touch.reset() {
		s.touchChanged(app)
	}
	s.pruneDeadParticipants()
	handlers := s.clearParticipants(app)
	s.pendingExtensionAnchor = pending
	return handlers
}

// touchHandleCaret is one handle a participant paints, at its caret.
type touchHandleCaret struct {
	edge  SelectionEdge
	caret gpui.Bounds
}

// touchSelection is the touch selection laid out for its handles and edit
// menu (U:text_selection.rs:1478-1526).
func (s *windowSelectionState) touchSelection(app *gpui.App) gpui.Optional[TouchSelectionSnapshot] {
	none := gpui.Optional[TouchSelectionSnapshot]{}
	if !s.touch.active {
		return none
	}
	type end struct {
		order   uint64
		caret   gpui.Bounds
		visible bool
		ok      bool
	}
	var start, last end
	for _, registration := range s.participants {
		geometry := registration.registration
		if geometry.scope != s.activeScope || !registration.participant.IsUpgradable() {
			continue
		}
		edges, ok := geometry.selectionEdges.Get()
		if !ok {
			continue
		}
		order := geometry.documentOrder
		viewport := geometry.hitbox.Bounds
		if !start.ok || order < start.order {
			start = end{order, edges[0], caretInView(edges[0], viewport), true}
		}
		if !last.ok || order >= last.order {
			last = end{order, edges[1], caretInView(edges[1], viewport), true}
		}
	}
	var edges touchEdges
	switch {
	case start.ok && last.ok:
		edges = touchEdges{start.caret, last.caret, start.visible, last.visible}
		s.touch.lastEdges = gpui.Some(edges)
	case s.touch.drag.IsSome():
		var ok bool
		if edges, ok = s.touch.lastEdges.Get(); !ok {
			return none
		}
	default:
		return none
	}
	var dragging gpui.Optional[SelectionEdge]
	if drag, ok := s.touch.drag.Get(); ok {
		dragging = gpui.Some(drag.edge)
	}
	return gpui.Some(newTouchSelectionSnapshot(edges.start, edges.end).
		withEdgeVisible(SelectionEdgeStart, edges.startVisible).
		withEdgeVisible(SelectionEdgeEnd, edges.endVisible).
		withMenuOpen(s.touch.menuOpen).
		withDragging(dragging))
}

// touchHandlesOf is the handles participant paints: the start when no
// participant earlier in document order has a selection end, the end when
// none later has (U:text_selection.rs:1528-1567).
func (s *windowSelectionState) touchHandlesOf(participant gpui.EntityID) []touchHandleCaret {
	if !s.touch.active {
		return nil
	}
	own, ok := s.participants[participant]
	if !ok {
		return nil
	}
	edges, ok := own.registration.selectionEdges.Get()
	if !ok || edges[0] == edges[1] {
		return nil
	}
	order := own.registration.documentOrder
	earlier, later := false, false
	for id, registration := range s.participants {
		if id == participant || registration.registration.scope != s.activeScope ||
			!registration.registration.selectionEdges.IsSome() || !registration.participant.IsUpgradable() {
			continue
		}
		other := registration.registration.documentOrder
		earlier = earlier || other < order
		later = later || other > order
	}
	viewport := own.registration.hitbox.Bounds
	var handles []touchHandleCaret
	if !earlier && caretInView(edges[0], viewport) {
		handles = append(handles, touchHandleCaret{SelectionEdgeStart, edges[0]})
	}
	if !later && caretInView(edges[1], viewport) {
		handles = append(handles, touchHandleCaret{SelectionEdgeEnd, edges[1]})
	}
	return handles
}

// keepTouchSelection keeps the selection a long press made and opens the
// edit menu over it (U:text_selection.rs:1569-1575).
func (s *windowSelectionState) keepTouchSelection(app *gpui.App) {
	s.touch.active = s.snapshot().IsSome()
	s.touch.menuOpen = s.touch.active
	s.touch.drag = gpui.Optional[edgeDrag]{}
	s.touchChanged(app)
}

// registerTouchUI records where the handles and the menu are painted this
// frame (U:text_selection.rs:1577-1580).
func (s *windowSelectionState) registerTouchUI(bounds gpui.Bounds) {
	s.touch.uiBounds = append(s.touch.uiBounds, bounds)
}

// closeEditMenu is U:text_selection.rs:1582-1588.
func (s *windowSelectionState) closeEditMenu(app *gpui.App) {
	if !s.touch.menuOpen {
		return
	}
	s.touch.menuOpen = false
	s.touchChanged(app)
}

// editMenuOnScroll steps the menu aside while a finger scrolls and brings
// it back once the finger lifts (U:text_selection.rs:1590-1606).
func (s *windowSelectionState) editMenuOnScroll(phase gpui.TouchPhase, app *gpui.App) {
	if !s.touch.active || s.touch.drag.IsSome() {
		return
	}
	switch phase {
	case gpui.TouchPhaseEnded, gpui.TouchPhaseCancelled:
		if !s.touch.menuOpen {
			s.touch.menuOpen = true
			s.touchChanged(app)
		}
	default:
		s.closeEditMenu(app)
	}
}

// endpointAt builds an inside-text endpoint of participant at a window
// point (the to_endpoint closures of U:text_selection.rs:1647-1660,
// 1801-1815).
func endpointAt(participant gpui.WeakEntity[selectableTextState], registration *TextSelectionRegistration, resolver TextSelectionContentKeyResolver, windowPoint gpui.Point) *selectionEndpoint {
	contentPoint := textPointSub(textPointSub(windowPoint, registration.bounds.Origin), registration.scrollOffset)
	return &selectionEndpoint{
		participant:   gpui.Some(participant),
		point:         contentPoint,
		inside:        true,
		insideText:    true,
		resolver:      resolver,
		resolverPoint: contentPoint,
	}
}

// selectAllTouched selects all of the participant the touch selection
// started in, as a point selection from its first line to its last
// (U:text_selection.rs:1608-1668).
func (s *windowSelectionState) selectAllTouched(app *gpui.App) {
	if !s.touch.active {
		return
	}
	participant, registration, ok := s.anchorRegistration()
	if !ok {
		return
	}
	type ends struct {
		anchor, cursor gpui.Point
		resolver       TextSelectionContentKeyResolver
		ok             bool
	}
	// From the participant's text runs, not its painted line bounds: those
	// are clipped to the viewport.
	found, err := participant.TryRead(app, func(p *selectableTextState, _ *gpui.App) ends {
		if len(p.runs) == 0 {
			return ends{}
		}
		first, last := p.runs[0], p.runs[0]
		for _, run := range p.runs[1:] {
			if run.documentOrder < first.documentOrder {
				first = run
			}
			if run.documentOrder >= last.documentOrder {
				last = run
			}
		}
		start, ok := first.layout.PositionForIndex(0)
		if !ok {
			return ends{}
		}
		end, ok := last.layout.PositionForIndex(len(last.text))
		if !ok {
			return ends{}
		}
		// Just inside the first and the last glyph, mid-line, so both land
		// on text.
		const inset = 1
		return ends{
			anchor:   gpui.Point{X: start.X + inset, Y: start.Y + first.layout.LineHeight()/2},
			cursor:   gpui.Point{X: end.X - inset, Y: end.Y + last.layout.LineHeight()/2},
			resolver: p.contentKeyResolver,
			ok:       true,
		}
	})
	if err != nil || !found.ok {
		return
	}
	s.anchor = endpointAt(participant, registration, found.resolver, found.anchor)
	s.cursor = endpointAt(participant, registration, found.resolver, found.cursor)
	s.didHitText = true
	s.isSelecting = false
	s.touch.menuOpen = true
	s.publishSnapshots(app)
	s.touchChanged(app)
}

// beginEdgeDrag starts dragging one end of the touch selection from
// finger, anchored at the end that stays (U:text_selection.rs:1670-1707).
func (s *windowSelectionState) beginEdgeDrag(edge SelectionEdge, finger gpui.Point, w *gpui.Window, app *gpui.App) {
	snapshot, ok := s.touchSelection(app).Get()
	if !ok {
		return
	}
	dispatchTextSelectionClearHandlers(s.prepareForMouseDown(false, app), app)
	held := snapshot.Edge(edge.Opposite())
	// Just inside the held end, so the anchor lands on the character it
	// marks rather than on the boundary between two participants.
	const nudge = 1
	anchorPoint := gpui.Point{X: held.Left() + nudge, Y: held.Center().Y}
	if edge == SelectionEdgeStart {
		anchorPoint.X = held.Left() - nudge
	}
	anchor := s.endpoint(anchorPoint, nil, app)
	drag := newEdgeDrag(edge, snapshot.Edge(edge), finger)
	cursor := s.endpoint(drag.textPosition(finger), w, app)
	s.didHitText = anchor.insideText || cursor.insideText
	s.anchor = anchor
	s.cursor = cursor
	s.isSelecting = true
	s.touch.active = true
	s.touch.menuOpen = false
	s.touch.drag = gpui.Some(drag)
	s.publishSnapshots(app)
	s.touchChanged(app)
}

// updateEdgeDrag moves the dragged end; a handle never collapses the
// selection, and dragging one end past the other swaps them
// (U:text_selection.rs:1709-1740).
func (s *windowSelectionState) updateEdgeDrag(finger gpui.Point, w *gpui.Window, cx *gpui.Context[windowSelectionState]) {
	drag, ok := s.touch.drag.Get()
	if !ok {
		return
	}
	app := cx.App()
	before := s.cursor.clone()
	s.updateInWindow(drag.textPosition(finger), w, cx)
	if !s.selectsText(app) {
		s.cursor = before
		s.publishSnapshots(app)
		return
	}
	if snapshot, ok := s.snapshot().Get(); ok {
		if points, ok := snapshot.windowPoints.Get(); ok {
			anchor, cursor := points.anchor, points.cursor
			cursorFirst := cursor.Y < anchor.Y || (cursor.Y == anchor.Y && cursor.X < anchor.X)
			edge := SelectionEdgeEnd
			if cursorFirst {
				edge = SelectionEdgeStart
			}
			if d, ok := s.touch.drag.Get(); ok {
				d.setEdge(edge)
				s.touch.drag = gpui.Some(d)
			}
		}
	}
	s.touchChanged(app)
}

// endEdgeDrag ends the handle drag and reopens the edit menu
// (U:text_selection.rs:1742-1748).
func (s *windowSelectionState) endEdgeDrag(app *gpui.App) {
	if !s.touch.drag.IsSome() {
		return
	}
	s.touch.drag = gpui.Optional[edgeDrag]{}
	s.end(app)
	s.keepTouchSelection(app)
}

// beginInWindow is U:text_selection.rs:1750-1758.
func (s *windowSelectionState) beginInWindow(position gpui.Point, extend bool, w *gpui.Window, app *gpui.App) {
	s.beginImpl(position, extend, true, w, app)
}

// updateInWindow moves the cursor unless a drag and drop is active
// (U:text_selection.rs:1760-1770).
func (s *windowSelectionState) updateInWindow(position gpui.Point, w *gpui.Window, cx *gpui.Context[windowSelectionState]) {
	if !cx.App().HasActiveDrag() {
		s.updateImpl(position, w, cx.App())
		s.updateAutoScroll(position, w, cx)
	}
}

// selectAt selects the word (two clicks) or line (three or more) under
// position (U:text_selection.rs:1772-1822).
func (s *windowSelectionState) selectAt(position gpui.Point, clickCount int, w *gpui.Window, app *gpui.App) {
	GlobalStateInit(app)
	if GlobalStateIsTextSelectionSuppressed(app) {
		return
	}
	hit := s.endpoint(position, w, app)
	if !hit.insideText {
		return
	}
	participant, ok := hit.participant.Get()
	if !ok || !participant.IsUpgradable() {
		return
	}
	type found struct {
		anchor, cursor gpui.Point
		resolver       TextSelectionContentKeyResolver
		ok             bool
	}
	points, err := participant.TryRead(app, func(p *selectableTextState, _ *gpui.App) found {
		anchor, cursor, ok := textSelectionPointsForMultiClick(p.runs, position, clickCount)
		return found{anchor, cursor, p.contentKeyResolver, ok}
	})
	if err != nil || !points.ok {
		return
	}
	registration, ok := s.participants[participant.EntityID()]
	if !ok {
		return
	}
	s.anchor = endpointAt(participant, registration.registration, points.resolver, points.anchor)
	s.cursor = endpointAt(participant, registration.registration, points.resolver, points.cursor)
	s.didHitText = true
	s.isSelecting = false
	_, _ = participant.TryRead(app, func(p *selectableTextState, app *gpui.App) struct{} {
		p.focus(w, app)
		return struct{}{}
	})
	s.publishSnapshots(app)
}

// updateInWindowWithActiveDrag is U:text_selection.rs:1824-1835
// (test-only upstream).
func (s *windowSelectionState) updateInWindowWithActiveDrag(position gpui.Point, activeDrag bool, w *gpui.Window, app *gpui.App) {
	if !activeDrag {
		s.updateImpl(position, w, app)
	}
}

// beginImpl is U:text_selection.rs:1837-1877.
func (s *windowSelectionState) beginImpl(position gpui.Point, extend, alreadyPrepared bool, w *gpui.Window, app *gpui.App) {
	GlobalStateInit(app)
	if GlobalStateIsTextSelectionSuppressed(app) {
		s.pendingExtensionAnchor = nil
		return
	}
	var previousAnchor *selectionEndpoint
	if extend {
		previousAnchor = s.pendingExtensionAnchor
		s.pendingExtensionAnchor = nil
		if previousAnchor == nil {
			previousAnchor = s.anchor.clone()
		}
		if previousAnchor != nil {
			if _, ok := previousAnchor.resolve(s.participants); !ok {
				previousAnchor = nil
			}
		}
	}
	if !extend && !alreadyPrepared {
		s.clear(app)
	}
	endpoint := s.endpoint(position, w, app)
	var focusParticipant gpui.Optional[gpui.WeakEntity[selectableTextState]]
	if endpoint.inside {
		focusParticipant = endpoint.participant
	}
	anchor := previousAnchor
	if anchor == nil {
		anchor = endpoint.clone()
	}
	s.anchor = anchor.clone()
	s.cursor = endpoint.clone()
	s.didHitText = anchor.insideText || endpoint.insideText
	s.isSelecting = true
	if participant, ok := focusParticipant.Get(); ok && w != nil {
		_, _ = participant.TryRead(app, func(p *selectableTextState, app *gpui.App) struct{} {
			p.focus(w, app)
			return struct{}{}
		})
	}
	s.publishSnapshots(app)
}

// updateImpl is U:text_selection.rs:1879-1890.
func (s *windowSelectionState) updateImpl(position gpui.Point, w *gpui.Window, app *gpui.App) {
	if !s.isSelecting {
		return
	}
	endpoint := s.endpoint(position, w, app)
	s.didHitText = s.didHitText || endpoint.insideText
	s.cursor = endpoint
	if w == nil {
		s.updateParticipantAutoScroll(position, app)
	}
	s.publishSnapshots(app)
}

// endpoint hit-tests the participants at position: the smallest hovered
// one (ties by document order), else the last one starting above it,
// else the first (U:text_selection.rs:1892-2002). Without a window,
// participant bounds stand in for the hover test.
func (s *windowSelectionState) endpoint(position gpui.Point, w *gpui.Window, app *gpui.App) *selectionEndpoint {
	s.pruneDeadParticipants()
	type candidate struct {
		participant  gpui.WeakEntity[selectableTextState]
		registration *TextSelectionRegistration
		area         float32
		ok           bool
	}
	var hit, predecessor, first candidate
	for _, registration := range s.participants {
		if registration.registration.scope != s.activeScope || !registration.participant.IsUpgradable() {
			continue
		}
		geometry := registration.registration
		var hovered bool
		if w == nil {
			hovered = geometry.bounds.Contains(position)
		} else {
			hovered = geometry.hitbox.IsHovered(w)
		}
		if hovered {
			area := geometry.bounds.Size.Width * geometry.bounds.Size.Height
			if !hit.ok || area < hit.area || (area == hit.area && geometry.documentOrder < hit.registration.documentOrder) {
				hit = candidate{registration.participant, geometry, area, true}
			}
		}
		if geometry.bounds.Top() <= position.Y && (!predecessor.ok ||
			geometry.bounds.Top() > predecessor.registration.bounds.Top() ||
			(geometry.bounds.Top() == predecessor.registration.bounds.Top() && geometry.documentOrder < predecessor.registration.documentOrder)) {
			predecessor = candidate{registration.participant, geometry, 0, true}
		}
		if !first.ok || geometry.bounds.Top() < first.registration.bounds.Top() ||
			(geometry.bounds.Top() == first.registration.bounds.Top() && geometry.documentOrder < first.registration.documentOrder) {
			first = candidate{registration.participant, geometry, 0, true}
		}
	}
	selection, inside := hit, true
	if !selection.ok {
		inside = false
		selection = predecessor
		if !selection.ok {
			selection = first
		}
	}
	if !selection.ok {
		return &selectionEndpoint{point: position}
	}
	point := textPointSub(textPointSub(position, selection.registration.bounds.Origin), selection.registration.scrollOffset)
	resolver, _ := selection.participant.TryRead(app, func(p *selectableTextState, _ *gpui.App) TextSelectionContentKeyResolver {
		return p.contentKeyResolver
	})
	insideText := false
	if inside {
		for _, b := range selection.registration.textBounds {
			if b.Contains(position) {
				insideText = true
				break
			}
		}
	}
	return &selectionEndpoint{
		participant:   gpui.Some(selection.participant),
		point:         point,
		inside:        inside,
		insideText:    insideText,
		resolver:      resolver,
		resolverPoint: point,
	}
}

// publishSnapshots hands every participant its share of the selection
// (U:text_selection.rs:2004-2023).
func (s *windowSelectionState) publishSnapshots(app *gpui.App) {
	s.pruneDeadParticipants()
	snapshot := s.snapshot()
	single, hasSingle := s.singleParticipant()
	for id, registration := range s.participants {
		if !registration.participant.IsUpgradable() {
			continue
		}
		var participantSnapshot gpui.Optional[TextSelectionSnapshot]
		if registration.registration.scope == s.activeScope && s.participates(id, registration) && (!hasSingle || single == id) {
			if snap, ok := snapshot.Get(); ok {
				snap.coverage = s.coverageFor(id)
				participantSnapshot = gpui.Some(snap)
			}
		}
		_ = registration.participant.TryUpdate(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
			p.setSnapshot(participantSnapshot, cx)
		})
	}
}

// coverageFor is U:text_selection.rs:2025-2044.
func (s *windowSelectionState) coverageFor(id gpui.EntityID) TextSelectionCoverage {
	if s.anchor == nil || s.cursor == nil {
		return TextSelectionCoverageBounded
	}
	anchor, ok := s.anchor.entityID().Get()
	if !ok {
		return TextSelectionCoverageBounded
	}
	cursor, ok := s.cursor.entityID().Get()
	if !ok || anchor == cursor {
		return TextSelectionCoverageBounded
	}
	anchorOrder := s.participants[anchor].registration.documentOrder
	cursorOrder := s.participants[cursor].registration.documentOrder
	switch {
	case id != anchor && id != cursor:
		return TextSelectionCoverageFull
	case (id == anchor) == (anchorOrder < cursorOrder):
		return TextSelectionCoverageToEnd
	}
	return TextSelectionCoverageFromStart
}

// singleParticipant is U:text_selection.rs:2046-2050.
func (s *windowSelectionState) singleParticipant() (gpui.EntityID, bool) {
	if s.anchor == nil || s.cursor == nil {
		return 0, false
	}
	anchor, ok := s.anchor.entityID().Get()
	if !ok {
		return 0, false
	}
	cursor, ok := s.cursor.entityID().Get()
	if !ok || anchor != cursor {
		return 0, false
	}
	return anchor, true
}

// participates reports whether a participant lies in the document interval
// between the endpoints' participants (U:text_selection.rs:2052-2076).
func (s *windowSelectionState) participates(id gpui.EntityID, registration participantRegistration) bool {
	if s.anchor == nil || s.cursor == nil {
		return false
	}
	anchor, ok := s.anchor.entityID().Get()
	if !ok {
		return false
	}
	cursor, ok := s.cursor.entityID().Get()
	if !ok {
		return false
	}
	anchorRegistration, ok := s.participants[anchor]
	if !ok {
		return false
	}
	cursorRegistration, ok := s.participants[cursor]
	if !ok {
		return false
	}
	start := min(anchorRegistration.registration.documentOrder, cursorRegistration.registration.documentOrder)
	end := max(anchorRegistration.registration.documentOrder, cursorRegistration.registration.documentOrder)
	order := registration.registration.documentOrder
	return (order >= start && order <= end) || id == anchor || id == cursor
}

// textSelectionHitTestInset keeps the synthesized wheel event hit-testing
// inside the mask (U:text_selection.rs:2104-2105).
const textSelectionHitTestInset float32 = 1

// textSelectionStalledTicks is how many ticks a container may sit at its
// end before the synthetic wheel holds off (U:text_selection.rs:2134).
const textSelectionStalledTicks = 3

// updateAutoScroll drives drag auto-scroll: a participant that scrolls
// its own content is notified directly; anything else gets a synthetic
// wheel event for the nearest scrollable ancestor
// (U:text_selection.rs:2078-2163).
func (s *windowSelectionState) updateAutoScroll(position gpui.Point, w *gpui.Window, cx *gpui.Context[windowSelectionState]) {
	// A finished gesture keeps its anchor for shift-click extension; only
	// a live drag may scroll.
	if !s.isSelecting {
		return
	}
	_, registration, ok := s.anchorRegistration()
	if !ok {
		return
	}
	app := cx.App()
	if registration.selfScroll {
		s.autoScroll.Stop()
		s.updateParticipantAutoScroll(position, app)
		return
	}
	// The content mask is the nearest clipping viewport established by a
	// scrollable ancestor; it stays stable as the participant moves.
	visible := registration.maskBounds()
	if visible.Size.Width < textSelectionHitTestInset*2 || visible.Size.Height < textSelectionHitTestInset*2 {
		s.stopAnchorAutoScroll(app)
		return
	}
	delta := AutoScrollComputeDelta(position.Y, visible)
	eventPosition := gpui.Point{
		X: min(max(position.X, visible.Left()+textSelectionHitTestInset), visible.Right()-textSelectionHitTestInset),
		Y: min(max(position.Y, visible.Top()+textSelectionHitTestInset), visible.Bottom()-textSelectionHitTestInset),
	}
	s.autoScroll.LastDragPosition = gpui.Some(eventPosition)
	s.autoScrollStall = gpui.Optional[autoScrollStallGeometry]{}
	s.autoScrollStalled = 0
	windowID := w.ID()
	s.autoScroll.Set(delta, cx, func(delta float32, state *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
		position, ok := state.autoScroll.LastDragPosition.Get()
		if !ok {
			return
		}
		// Hold off once the container has shown, over a few ticks, that
		// it has nowhere left to go; the next move of the finger asks
		// again.
		var geometry gpui.Optional[autoScrollStallGeometry]
		if _, registration, ok := state.anchorRegistration(); ok {
			geometry = gpui.Some(autoScrollStallGeometry{registration.bounds, registration.scrollOffset})
		}
		if state.autoScrollStall == geometry {
			if state.autoScrollStalled < 255 {
				state.autoScrollStalled++
			}
			if state.autoScrollStalled >= textSelectionStalledTicks {
				return
			}
		} else {
			state.autoScrollStall = geometry
			state.autoScrollStalled = 0
		}
		cx.App().Defer(func(app *gpui.App) {
			if w, ok := app.Window(windowID); ok {
				w.DispatchInput(&gpui.ScrollWheelEvent{
					Position:   position,
					Delta:      gpui.ScrollDeltaPixels(0, -delta),
					TouchPhase: gpui.TouchPhaseMoved,
				}, app)
			}
		})
	})
}

// updateParticipantAutoScroll drives the anchor participant's own
// scrolling, measured against the visible part of its bounds
// (U:text_selection.rs:2165-2180).
func (s *windowSelectionState) updateParticipantAutoScroll(position gpui.Point, app *gpui.App) {
	participant, registration, ok := s.anchorRegistration()
	if !ok {
		return
	}
	visible := textBoundsIntersect(registration.bounds, registration.maskBounds())
	var delta gpui.Optional[float32]
	if visible.Size.Width > 0 && visible.Size.Height > 0 {
		delta = AutoScrollComputeDelta(position.Y, visible)
	}
	_ = participant.TryUpdate(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
		p.setAutoScroll(delta, cx)
	})
}

// stopAnchorAutoScroll is U:text_selection.rs:2182-2188.
func (s *windowSelectionState) stopAnchorAutoScroll(app *gpui.App) {
	s.autoScroll.Stop()
	participant, ok := s.anchorParticipant()
	if !ok {
		return
	}
	_ = participant.TryUpdate(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
		p.setAutoScroll(gpui.Optional[float32]{}, cx)
	})
}

// anchorParticipant is the live participant owning the anchor of the
// current gesture (U:text_selection.rs:2190-2198).
func (s *windowSelectionState) anchorParticipant() (gpui.WeakEntity[selectableTextState], bool) {
	if s.anchor == nil || !s.anchor.inside {
		return gpui.WeakEntity[selectableTextState]{}, false
	}
	participant, ok := s.anchor.participant.Get()
	if !ok || !participant.IsUpgradable() {
		return gpui.WeakEntity[selectableTextState]{}, false
	}
	return participant, true
}

// anchorRegistration is the anchor participant with its current frame
// registration (U:text_selection.rs:2200-2207).
func (s *windowSelectionState) anchorRegistration() (gpui.WeakEntity[selectableTextState], *TextSelectionRegistration, bool) {
	participant, ok := s.anchorParticipant()
	if !ok {
		return participant, nil, false
	}
	registration, ok := s.participants[participant.EntityID()]
	if !ok {
		return participant, nil, false
	}
	return participant, registration.registration, true
}

// pruneDeadParticipants is U:text_selection.rs:2209-2212.
func (s *windowSelectionState) pruneDeadParticipants() {
	for id, registration := range s.participants {
		if !registration.participant.IsUpgradable() {
			delete(s.participants, id)
		}
	}
}

// ---------------------------------------------------------------------------
// Scopes (U:text_selection.rs:2227-2281)
// ---------------------------------------------------------------------------

// textSelectionScopeStacks is TextSelectionScopeStacks
// (U:text_selection.rs:2227-2230).
type textSelectionScopeStacks struct {
	stacks map[gpui.WindowID][]TextSelectionScopeID
}

// pushTextSelectionScope is U:text_selection.rs:2232-2241.
func pushTextSelectionScope(windowID gpui.WindowID, scope TextSelectionScopeID, app *gpui.App) {
	stacks, ok := gpui.TryGlobal[*textSelectionScopeStacks](app)
	if !ok {
		stacks = &textSelectionScopeStacks{stacks: map[gpui.WindowID][]TextSelectionScopeID{}}
		app.SetGlobal(stacks)
	}
	stacks.stacks[windowID] = append(stacks.stacks[windowID], scope)
}

// popTextSelectionScope is U:text_selection.rs:2243-2252.
func popTextSelectionScope(windowID gpui.WindowID, app *gpui.App) {
	stacks, ok := gpui.TryGlobal[*textSelectionScopeStacks](app)
	if !ok {
		return
	}
	stack, ok := stacks.stacks[windowID]
	if !ok {
		return
	}
	if len(stack) > 0 {
		stack = stack[:len(stack)-1]
	}
	if len(stack) == 0 {
		delete(stacks.stacks, windowID)
		return
	}
	stacks.stacks[windowID] = stack
}

// currentTextSelectionScope is U:text_selection.rs:2254-2266.
func currentTextSelectionScope(windowID gpui.WindowID, app *gpui.App) (TextSelectionScopeID, bool) {
	stacks, ok := gpui.TryGlobal[*textSelectionScopeStacks](app)
	if !ok {
		return 0, false
	}
	stack := stacks.stacks[windowID]
	if len(stack) == 0 {
		return 0, false
	}
	return stack[len(stack)-1], true
}

// withTextSelectionScope runs f with scope pushed for the window, popping
// it even when f panics (U:text_selection.rs:2268-2281).
func withTextSelectionScope[T any](windowID gpui.WindowID, scope TextSelectionScopeID, app *gpui.App, f func(*gpui.App) T) T {
	pushTextSelectionScope(windowID, scope, app)
	defer popTextSelectionScope(windowID, app)
	return f(app)
}

// ---------------------------------------------------------------------------
// The TextSelection namespace (U:text_selection.rs:2283-2414)
// ---------------------------------------------------------------------------

// TextSelectionSelectedText is the currently selected text in logical
// document order (TextSelection::selected_text,
// U:text_selection.rs:2287-2294).
func TextSelectionSelectedText(w *gpui.Window, app *gpui.App) string {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return ""
	}
	items, err := state.TryRead(app, func(s *windowSelectionState, app *gpui.App) []textSelectionCopyItem {
		return s.copyItems(app)
	})
	if err != nil {
		return ""
	}
	return resolveTextSelectionCopyItems(items, app)
}

// TextSelectionHasSelection reports whether the window has a geometry
// selection or any participant has a participant-local selection
// (TextSelection::has_selection, U:text_selection.rs:2296-2300).
func TextSelectionHasSelection(w *gpui.Window, app *gpui.App) bool {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return false
	}
	has, err := state.TryRead(app, func(s *windowSelectionState, app *gpui.App) bool { return s.hasSelection(app) })
	return err == nil && has
}

// TextSelectionClear clears window selection and every participant's
// local selection (TextSelection::clear, U:text_selection.rs:2302-2308).
func TextSelectionClear(w *gpui.Window, app *gpui.App) {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return
	}
	handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) []textSelectionClearHandler {
		return s.clearState(cx.App())
	})
	if err == nil {
		dispatchTextSelectionClearHandlers(handlers, app)
	}
}

// TextSelectionClearForWindow clears selection for a known window
// identifier; prefer TextSelectionClear with a window
// (TextSelection::clear_for_window, U:text_selection.rs:2310-2316).
func TextSelectionClearForWindow(windowID gpui.WindowID, app *gpui.App) {
	clearWindowTextSelection(windowID, app)
}

// TextSelectionEnd ends the current drag while leaving its selection
// visible (TextSelection::end, U:text_selection.rs:2318-2323).
func TextSelectionEnd(w *gpui.Window, app *gpui.App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) { s.end(cx.App()) })
	}
}

// textSelectionTouchObserver is the observer entity behind
// TextSelectionObserveTouchSelection (see the file comment).
type textSelectionTouchObserver struct{}

// TextSelectionObserveTouchSelection calls callback whenever the touch
// selection changes: it appears, its handles move, its menu opens or
// closes, or it goes away (TextSelection::observe_touch_selection,
// U:text_selection.rs:2325-2335).
func TextSelectionObserveTouchSelection(w *gpui.Window, app *gpui.App, callback func(app *gpui.App)) gpui.Subscription {
	weak := textSelectionStateEnsure(w, app)
	borrow := w.Scope().Child()
	defer borrow.Close()
	state, _ := weak.UpgradeInto(borrow)
	observer := gpui.NewEntity(app, w.Scope(), func(*textSelectionTouchObserver, *gpui.Context[textSelectionTouchObserver]) {})
	return observer.UpdateWith(app, func(_ *textSelectionTouchObserver, cx *gpui.Context[textSelectionTouchObserver]) gpui.Subscription {
		return cx.Observe(state, func(_ *textSelectionTouchObserver, _ gpui.Entity[windowSelectionState], cx *gpui.Context[textSelectionTouchObserver]) {
			callback(cx.App())
		})
	})
}

// TextSelectionTouchSelection is the selection a long press made, laid out
// for its handles and edit menu; absent for a pointer selection
// (TextSelection::touch_selection, U:text_selection.rs:2337-2343).
func TextSelectionTouchSelection(w *gpui.Window, app *gpui.App) gpui.Optional[TouchSelectionSnapshot] {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return gpui.Optional[TouchSelectionSnapshot]{}
	}
	snapshot, _ := state.TryRead(app, func(s *windowSelectionState, app *gpui.App) gpui.Optional[TouchSelectionSnapshot] {
		return s.touchSelection(app)
	})
	return snapshot
}

// TextSelectionRegisterTouchUI records where a touch handle or the edit
// menu is painted this frame, so pressing it does not clear its selection.
// Call from paint every frame the surface is shown
// (TextSelection::register_touch_ui, U:text_selection.rs:2345-2352).
func TextSelectionRegisterTouchUI(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, _ *gpui.Context[windowSelectionState]) { s.registerTouchUI(bounds) })
	}
}

// TextSelectionCloseEditMenu closes the edit menu and keeps the selection
// with its handles (TextSelection::close_edit_menu,
// U:text_selection.rs:2354-2359).
func TextSelectionCloseEditMenu(w *gpui.Window, app *gpui.App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) { s.closeEditMenu(cx.App()) })
	}
}

// TextSelectionSelectAll selects all of the text the touch selection
// started in (TextSelection::select_all, U:text_selection.rs:2361-2367).
func TextSelectionSelectAll(w *gpui.Window, app *gpui.App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) { s.selectAllTouched(cx.App()) })
	}
}

// TextSelectionBeginEdgeDrag starts dragging one end of the touch
// selection from finger (TextSelection::begin_edge_drag,
// U:text_selection.rs:2369-2383).
func TextSelectionBeginEdgeDrag(edge SelectionEdge, finger gpui.Point, w *gpui.Window, app *gpui.App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			s.beginEdgeDrag(edge, finger, w, cx.App())
		})
		textSelectionResolveContentKeys(state, app)
	}
}

// TextSelectionUpdateEdgeDrag moves the dragged end to the text under
// finger (TextSelection::update_edge_drag, U:text_selection.rs:2385-2391).
func TextSelectionUpdateEdgeDrag(finger gpui.Point, w *gpui.Window, app *gpui.App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			s.updateEdgeDrag(finger, w, cx)
		})
		textSelectionResolveContentKeys(state, app)
	}
}

// TextSelectionEndEdgeDrag ends the handle drag and reopens the edit menu
// (TextSelection::end_edge_drag, U:text_selection.rs:2393-2398).
func TextSelectionEndEdgeDrag(w *gpui.Window, app *gpui.App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) { s.endEdgeDrag(cx.App()) })
	}
}

// TextSelectionActivateScope activates the opaque selection scope for the
// window (TextSelection::activate_scope, U:text_selection.rs:2400-2413).
func TextSelectionActivateScope(scope TextSelectionScopeID, w *gpui.Window, app *gpui.App) {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		pending, ok := gpui.TryGlobal[*pendingTextSelectionScopes](app)
		if !ok {
			pending = &pendingTextSelectionScopes{scopes: map[gpui.WindowID]TextSelectionScopeID{}}
			app.SetGlobal(pending)
		}
		pending.scopes[w.ID()] = scope
		return
	}
	handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) []textSelectionClearHandler {
		return s.setActiveScopeState(scope, cx.App())
	})
	if err == nil {
		dispatchTextSelectionClearHandlers(handlers, app)
	}
}

// ---------------------------------------------------------------------------
// The scope marker and the selection layer (U:text_selection.rs:2416-2790)
// ---------------------------------------------------------------------------

// textSelectionScopeMarker is TextSelectionScopeMarker
// (U:text_selection.rs:2432-2509): it runs every phase of its element with
// the scope pushed. It carries no id of its own; the wrapped element keeps
// its identity.
type textSelectionScopeMarker struct {
	scope   TextSelectionScopeID
	element gpui.AnyElement
}

// textSelectionScope places element in scope (text_selection_scope,
// U:text_selection.rs:2422-2430; ElementExt::text_selection_scope).
func textSelectionScope(scope TextSelectionScopeID, element gpui.AnyElement) gpui.AnyElement {
	return gpui.CustomElement[struct{}, struct{}](&textSelectionScopeMarker{scope: scope, element: element})
}

// ID implements gpui.Element.
func (m *textSelectionScopeMarker) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements gpui.Element.
func (m *textSelectionScopeMarker) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements gpui.Element (U:text_selection.rs:2457-2468).
func (m *textSelectionScopeMarker) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	id := withTextSelectionScope(w.ID(), m.scope, app, func(app *gpui.App) gpui.LayoutID {
		return m.element.RequestLayout(w, app)
	})
	return id, struct{}{}
}

// Prepaint implements gpui.Element (U:text_selection.rs:2470-2484).
func (m *textSelectionScopeMarker) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return withTextSelectionScope(w.ID(), m.scope, app, func(app *gpui.App) struct{} {
		m.element.Prepaint(w, app)
		return struct{}{}
	})
}

// Paint implements gpui.Element (U:text_selection.rs:2486-2508).
func (m *textSelectionScopeMarker) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, _ *struct{}, w *gpui.Window, app *gpui.App) {
	withTextSelectionScope(w.ID(), m.scope, app, func(app *gpui.App) struct{} {
		m.element.Paint(w, app)
		return struct{}{}
	})
}

// TextSelectionLayer is a zero-sized root layer that enables text
// selection for a window (U:text_selection.rs:2416-2420). Mount one as the
// root's first child; its stable "window-text-selection" identity retains
// the window-local selection state across frames.
type TextSelectionLayer struct{}

// textSelectionRetainer is the keyed holder whose dependent scope owns the
// window selection state for the layer (see the file comment).
type textSelectionRetainer struct {
	state gpui.WeakEntity[windowSelectionState]
}

// IntoElement implements gpui.IntoElement.
func (l TextSelectionLayer) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, gpui.WeakEntity[windowSelectionState]](&l)
}

// ID implements gpui.Element (U:text_selection.rs:2526-2528).
func (l *TextSelectionLayer) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("window-text-selection"), true
}

// SourceLocation implements gpui.Element.
func (l *TextSelectionLayer) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout requests an empty node (U:text_selection.rs:2534-2542).
func (l *TextSelectionLayer) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint begins the selection frame and retains the window state
// (U:text_selection.rs:2544-2564). Automatic participant order is paint
// order within this frame.
func (l *TextSelectionLayer) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) gpui.WeakEntity[windowSelectionState] {
	GlobalStateInit(app)
	GlobalStateGlobalMut(app).BeginSelectionFrame()
	state := retainTextSelectionState(w, app)
	// The handles and the menu register again as they paint this frame.
	_ = state.TryUpdate(app, func(s *windowSelectionState, _ *gpui.Context[windowSelectionState]) { s.touch.beginFrame() })
	return state
}

// Paint installs the window's selection gestures
// (U:text_selection.rs:2566-2577).
func (l *TextSelectionLayer) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, state *gpui.WeakEntity[windowSelectionState], w *gpui.Window, app *gpui.App) {
	paintTextSelection(*state, w, app)
}

// retainTextSelectionState is U:text_selection.rs:2580-2600.
func retainTextSelectionState(w *gpui.Window, app *gpui.App) gpui.WeakEntity[windowSelectionState] {
	windowID := w.ID()
	holder := w.UseKeyedState(gpui.NameElementID("state"), app, func(_ *gpui.Window, cx *gpui.Context[textSelectionRetainer]) textSelectionRetainer {
		return textSelectionRetainer{state: textSelectionStateAcquire(windowID, app, cx.Scope())}
	})
	state := holder.Read(app, func(r *textSelectionRetainer, _ *gpui.App) gpui.WeakEntity[windowSelectionState] {
		return r.state
	})
	textSelectionRegistry(app).states[windowID] = state
	return state
}

// paintTextSelection schedules the post-frame sweep and installs the
// window's mouse listeners (U:text_selection.rs:2602-2767).
func paintTextSelection(state gpui.WeakEntity[windowSelectionState], w *gpui.Window, app *gpui.App) {
	schedule, _ := state.TryUpdateWith(app, func(s *windowSelectionState, _ *gpui.Context[windowSelectionState]) bool {
		return s.scheduleFinishFrame()
	})
	if schedule {
		w.Defer(app, func(w *gpui.Window, app *gpui.App) {
			handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) []textSelectionClearHandler {
				return s.finishFrame(cx.App())
			})
			if err != nil {
				return
			}
			dispatchTextSelectionClearHandlers(handlers, app)
			// Direct participant scrolling produces no wheel event. Refresh
			// the held cursor after paint registers the new scroll geometry.
			refresh, _ := state.TryUpdateWith(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) bool {
				if !s.refreshHeldCursor || !s.isSelecting {
					s.refreshHeldCursor = false
					return false
				}
				s.refreshHeldCursor = false
				// A handle drag holds the finger off the text; keep the
				// same offset, or the cursor would hop between the two.
				position := w.TrackedMousePosition()
				if drag, ok := s.touch.drag.Get(); ok {
					position = drag.textPosition(position)
				}
				s.updateInWindow(position, w, cx)
				return true
			})
			if refresh {
				textSelectionResolveContentKeys(state, app)
			}
		})
	}

	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		ev, ok := event.(*gpui.MouseDownEvent)
		if !ok || ev.Button != gpui.MouseButtonLeft || !state.IsUpgradable() {
			return
		}
		// A press on a handle or on the edit menu acts on the selection; it
		// must not clear it.
		covers, _ := state.TryRead(app, func(s *windowSelectionState, _ *gpui.App) bool { return s.touch.covers(ev.Position) })
		if covers {
			return
		}
		switch {
		case phase == gpui.DispatchCapture:
			GlobalStateInit(app)
			GlobalStateResetTextSelectionSuppression(app)
			handlers, _ := state.TryUpdateWith(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) []textSelectionClearHandler {
				if s.mouseDownPrepared {
					return nil
				}
				s.mouseDownPrepared = true
				return s.prepareForMouseDown(ev.ClickCount == 1 && ev.Modifiers.Shift, cx.App())
			})
			dispatchTextSelectionClearHandlers(handlers, app)
		case ev.ClickCount == 1:
			if GlobalStateIsTextSelectionSuppressed(app) {
				_ = state.TryUpdate(app, func(s *windowSelectionState, _ *gpui.Context[windowSelectionState]) { s.pendingExtensionAnchor = nil })
				return
			}
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
				if !s.isSelecting {
					s.beginInWindow(ev.Position, ev.Modifiers.Shift, w, cx.App())
				}
			})
			textSelectionResolveContentKeys(state, app)
		case ev.ClickCount >= 2:
			if GlobalStateIsTextSelectionSuppressed(app) {
				return
			}
			touch := GlobalStateIsTouchPress(app)
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
				s.selectAt(ev.Position, ev.ClickCount, w, cx.App())
				// A double tap is touch's other way to select a word, and it
				// gets the handles and the menu like a long press does.
				if touch {
					s.keepTouchSelection(cx.App())
				}
			})
			textSelectionResolveContentKeys(state, app)
		}
	})

	// A handle drag in progress follows the finger or the pointer wherever
	// it goes, whether or not the handle it took is laid out this frame.
	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		ev, ok := event.(*gpui.MouseMoveEvent)
		if !ok || phase != gpui.DispatchBubble || ev.PressedButton == nil || *ev.PressedButton != gpui.MouseButtonLeft {
			return
		}
		dragging, err := state.TryRead(app, func(s *windowSelectionState, _ *gpui.App) bool { return s.touch.drag.IsSome() })
		if err != nil || !dragging {
			return
		}
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			s.updateEdgeDrag(ev.Position, w, cx)
		})
		textSelectionResolveContentKeys(state, app)
	})
	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, _ *gpui.Window, app *gpui.App) {
		ev, ok := event.(*gpui.MouseUpEvent)
		if !ok || phase != gpui.DispatchBubble || ev.Button != gpui.MouseButtonLeft {
			return
		}
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) { s.endEdgeDrag(cx.App()) })
	})

	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		ev, ok := event.(*gpui.MouseMoveEvent)
		if !ok || phase != gpui.DispatchBubble || !state.IsUpgradable() {
			return
		}
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			// A handle drag maps the pointer through the handle's offset;
			// the raw pointer must not fight it.
			if s.touch.drag.IsSome() {
				return
			}
			s.updateInWindow(ev.Position, w, cx)
		})
		textSelectionResolveContentKeys(state, app)
	})

	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, _ *gpui.Window, app *gpui.App) {
		if _, ok := event.(*gpui.MouseUpEvent); !ok || phase != gpui.DispatchBubble {
			return
		}
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			s.mouseDownPrepared = false
			s.end(cx.App())
		})
	})

	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		ev, ok := event.(*gpui.ScrollWheelEvent)
		if !ok || !state.IsUpgradable() {
			return
		}
		// On capture, before a scroll container can stop the event: one
		// stretched past its end swallows the whole stream, the lift
		// included, and the menu would never come back.
		if phase == gpui.DispatchCapture {
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
				s.editMenuOnScroll(ev.TouchPhase, cx.App())
			})
			return
		}
		position := w.TrackedMousePosition()
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			// A handle drag holds the finger off the text; keep its offset.
			if drag, ok := s.touch.drag.Get(); ok {
				position = drag.textPosition(position)
			}
			s.updateInWindow(position, w, cx)
		})
		textSelectionResolveContentKeys(state, app)
	})
}

// clearWindowTextSelection is U:text_selection.rs:2776-2790.
func clearWindowTextSelection(windowID gpui.WindowID, app *gpui.App) {
	registry, ok := gpui.TryGlobal[*selectionStateRegistry](app)
	if !ok {
		return
	}
	state, ok := registry.states[windowID]
	if !ok || !state.IsUpgradable() {
		return
	}
	handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) []textSelectionClearHandler {
		return s.clearState(cx.App())
	})
	if err == nil {
		dispatchTextSelectionClearHandlers(handlers, app)
	}
}
