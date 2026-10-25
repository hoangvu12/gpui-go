package gpui

// Port of U:text_selection.rs (U: = upstream-base/ce/crates/base/src @
// c0820693): window-scoped text selection across elements — the
// participant handle, the per-window selection state with its frame
// registration and sweep, projection onto laid-out runs, copy, scopes,
// drag auto-scroll, word and line multi-click, the touch selection
// lifecycle, and the TextSelectionLayer element. Moved from base into
// the core by browser-core ticket 19 (static text selection is a core
// behaviour: the model, the platform-highlight painting and the copy
// action); Base re-exports the public surface as aliases
// (base/text_selection.go) and installs its upstream word/line
// boundaries over the seam (base/text_boundary.go). Browser-core
// ticket 20 (spec D7) made the selection default-on: the window
// installs the state and gestures itself (user_select.go), plain Text
// elements register participants, and the layer is a compat no-op.
//
// The ticket's own additions on top of the upstream port, with their
// Chromium citations: the multi-click boundaries go through a seam whose
// default is the browser pair (gpui/text_boundary.go — the UAX #29 word
// segments and the paragraph-with-break line), a drag after a
// multi-click extends by the same granularity from the selection's
// stored range (windowSelectionState.updateGranularityEndpoints —
// HandleMouseDragged's ExtendSelectionAsDirectional,
// third_party/blink/renderer/core/editing/selection_controller.cc:
// 316-375, 693-699), and the copy command falls to the window selection
// when nothing editable is focused (gpui/text_selection_copy.go).
//
// Go adaptations (recorded deviations):
//
//   - Ownership. Entities need an owner scope in the Go core
//     (NewEntity). NewTextSelectionHandle takes one; the window
//     selection state is owned by the window's scope — browser-core 20
//     installs it for every window at the head of the frame draw
//     (user_select.go installWindowTextSelection), so the state lives
//     as long as the window, where ticket 19's TextSelectionLayer
//     retained it through a keyed holder while the layer drew.
//   - Hitbox's content mask is unexported here. A registration records
//     the window's content mask when it is registered (the mask its
//     participant inserted the hitbox under); one registered directly on
//     the state (tests) falls back to the hitbox bounds.
//   - TextLayout exposes no line layouts: textRowsExtent's last row top
//     is the caret position after the last character
//     (TextLayout.PositionForIndex(len)), which places the trailing empty
//     line the same way. The extent stays covering, as upstream requires.
//   - Subscriptions. The core has no App-level subscribe/observe:
//     TextSelectionHandle.Subscribe registers a self-subscription on the
//     participant (its callbacks run while the participant is updated, so
//     they must not read it back through the handle);
//     TextSelectionObserveTouchSelection observes through a small observer
//     entity in the window's scope.
//   - std::panic::catch_unwind in withTextSelectionScope is a deferred
//     pop: the stack is restored when a subtree panics, and the panic
//     propagates.
//   - Rust's Option values are Optional; enum events are a struct with
//     a kind.

import (
	"sort"
	"strings"
	"sync/atomic"
	"unicode/utf8"
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
	entityID   Optional[EntityID]
	point      Point
	contentKey Optional[TextSelectionContentKey]
}

// newTextSelectionEndpoint is TextSelectionEndpoint::new
// (U:text_selection.rs:72-79).
func newTextSelectionEndpoint(entityID Optional[EntityID], point Point) TextSelectionEndpoint {
	return TextSelectionEndpoint{entityID: entityID, point: point}
}

// withContentKey is U:text_selection.rs:81-85.
func (e TextSelectionEndpoint) withContentKey(key TextSelectionContentKey) TextSelectionEndpoint {
	e.contentKey = Some(key)
	return e
}

// EntityID is the participant owning this endpoint, when it hit one
// (U:text_selection.rs:87-90).
func (e TextSelectionEndpoint) EntityID() Optional[EntityID] { return e.entityID }

// ContentPoint is the participant-relative content point
// (U:text_selection.rs:92-95).
func (e TextSelectionEndpoint) ContentPoint() Point { return e.point }

// ContentKey is the participant-defined endpoint metadata captured when
// it hit a participant (U:text_selection.rs:97-100).
func (e TextSelectionEndpoint) ContentKey() Optional[TextSelectionContentKey] {
	return e.contentKey
}

// TextSelectionWindowPoints are the window-coordinate anchor and cursor
// points for painting a selection (U:text_selection.rs:103-120).
type TextSelectionWindowPoints struct {
	anchor, cursor Point
}

// Anchor is the stable anchor in window coordinates.
func (p TextSelectionWindowPoints) Anchor() Point { return p.anchor }

// Cursor is the moving cursor in window coordinates.
func (p TextSelectionWindowPoints) Cursor() Point { return p.cursor }

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
	windowPoints   Optional[TextSelectionWindowPoints]
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
func (s TextSelectionSnapshot) withWindowPoints(points Optional[TextSelectionWindowPoints]) TextSelectionSnapshot {
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
func (s TextSelectionSnapshot) WindowPoints() Optional[TextSelectionWindowPoints] {
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
	hitbox         Hitbox
	bounds         Bounds
	scrollOffset   Point
	scope          TextSelectionScopeID
	documentOrder  uint64
	textBounds     []Bounds
	selfScroll     bool
	selectionEdges Optional[[2]Bounds]
	rendered       Optional[WeakEntity[textSelectionRenderedMarker]]
	// runs are the laid-out runs the participant paints this frame
	// (browser-core 20): the automatic registration carries them here —
	// the window state already holds the registration, so the gesture
	// hit-testing and the copy projection read them without an entity
	// update per text per frame. Nil for participants that report their
	// runs through the handle (SelectableText's paint, U:842-845).
	runs []TextSelectionRun
	// contentMask is the content mask the participant registered under
	// (Hitbox::content_mask; see the file comment).
	contentMask Optional[Bounds]
}

// NewTextSelectionRegistration creates a registration with the default
// scope, order and scroll offset (U:text_selection.rs:229-242).
func NewTextSelectionRegistration(hitbox Hitbox, bounds Bounds) TextSelectionRegistration {
	return TextSelectionRegistration{hitbox: hitbox, bounds: bounds}
}

// WithRenderedElement ties this registration to the retained state of the
// element reporting it, so a frame replayed from that element's cached
// view keeps it (U:text_selection.rs:244-269). Call it from the reporting
// element's prepaint or paint.
func (r TextSelectionRegistration) WithRenderedElement(participant TextSelectionHandle, w *Window, app *App) TextSelectionRegistration {
	marker := w.UseKeyedState(
		NamedIntegerElementID("text-selection-participant", uint64(participant.EntityID())),
		app,
		func(*Window, *Context[textSelectionRenderedMarker]) textSelectionRenderedMarker {
			return textSelectionRenderedMarker{}
		},
	)
	r.rendered = Some(marker.Downgrade())
	return r
}

// isRendered reports whether the element that reported this registration
// is still part of the window (U:text_selection.rs:271-277).
func (r *TextSelectionRegistration) isRendered() bool {
	marker, ok := r.rendered.Get()
	return ok && marker.IsUpgradable()
}

// WithSelfScroll marks a participant that scrolls its own content in
// response to the AutoScroll event (U:text_selection.rs:279-286;
// exported when the model moved into the core — Base's TextView adapter
// calls it, browser-core 19).
func (r TextSelectionRegistration) WithSelfScroll(selfScroll bool) TextSelectionRegistration {
	r.selfScroll = selfScroll
	return r
}

// WithScrollOffset sets the participant's content scroll offset
// (U:text_selection.rs:288-292).
func (r TextSelectionRegistration) WithScrollOffset(offset Point) TextSelectionRegistration {
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
func (r TextSelectionRegistration) WithTextBounds(bounds []Bounds) TextSelectionRegistration {
	r.textBounds = bounds
	return r
}

// WithRuns sets the laid-out runs this participant paints this frame
// (browser-core 20): the registration carries them, so the model's
// gesture hit-testing and the copy projection read the geometry without
// the handle's per-frame entity updates — the automatic registration's
// cost shape. A participant that reports runs through the handle
// (SelectableText's UpdateRuns) leaves them nil and the reads fall back
// to the handle's retained runs.
func (r TextSelectionRegistration) WithRuns(runs []TextSelectionRun) TextSelectionRegistration {
	r.runs = runs
	return r
}

// Runs is the laid-out runs this participant registered for the frame
// (nil when the participant reports them through the handle).
func (r TextSelectionRegistration) Runs() []TextSelectionRun { return r.runs }

// WithSelectionEdges sets where the participant painted the two ends of
// its selection, in window coordinates; the touch handles are drawn there
// (U:text_selection.rs:312-318).
func (r TextSelectionRegistration) WithSelectionEdges(start, end Bounds) TextSelectionRegistration {
	r.selectionEdges = Some([2]Bounds{start, end})
	return r
}

// Hitbox is the participant hitbox (U:text_selection.rs:320-323).
func (r TextSelectionRegistration) Hitbox() Hitbox { return r.hitbox }

// Bounds is the participant's window-coordinate bounds.
func (r TextSelectionRegistration) Bounds() Bounds { return r.bounds }

// ScrollOffset is the participant's content scroll offset.
func (r TextSelectionRegistration) ScrollOffset() Point { return r.scrollOffset }

// Scope is the opaque selection scope.
func (r TextSelectionRegistration) Scope() TextSelectionScopeID { return r.scope }

// DocumentOrder is the stable logical document order.
func (r TextSelectionRegistration) DocumentOrder() uint64 { return r.documentOrder }

// TextBounds is the glyph-bearing bounds used to reject blank-only
// gestures.
func (r TextSelectionRegistration) TextBounds() []Bounds { return r.textBounds }

// SelectionEdges is the caret line boxes at the participant's selection
// ends (U:text_selection.rs:350-353).
func (r TextSelectionRegistration) SelectionEdges() (start, end Bounds, ok bool) {
	edges, ok := r.selectionEdges.Get()
	return edges[0], edges[1], ok
}

// maskBounds is hitbox.content_mask.bounds (see the file comment).
func (r *TextSelectionRegistration) maskBounds() Bounds {
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
	layout        *TextLayout
	bounds        Bounds
}

// NewTextSelectionRun creates a laid-out text run; layout must have been
// produced from exactly text (U:text_selection.rs:370-378).
func NewTextSelectionRun(text string, layout *TextLayout, bounds Bounds) TextSelectionRun {
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
func (r TextSelectionRun) Layout() *TextLayout { return r.layout }

// Bounds is the run's window-coordinate paint bounds.
func (r TextSelectionRun) Bounds() Bounds { return r.bounds }

// TextSelectionProjection is a selection projected onto a participant's
// laid-out text runs (U:text_selection.rs:407-426).
type TextSelectionProjection struct {
	ranges   []Optional[TextRange]
	isActive bool
}

// Ranges is the selected UTF-8 byte ranges, paired with the input runs.
func (p TextSelectionProjection) Ranges() []Optional[TextRange] { return p.ranges }

// IsActive reports whether the participant participates in the selection.
func (p TextSelectionProjection) IsActive() bool { return p.isActive }

// projectTextSelectionRanges projects a participant snapshot onto
// laid-out plain-text runs, keeping the input order
// (U:text_selection.rs:428-457).
func projectTextSelectionRanges(snapshot Optional[TextSelectionSnapshot], runs []TextSelectionRun) TextSelectionProjection {
	snap, ok := snapshot.Get()
	if !ok {
		return TextSelectionProjection{ranges: make([]Optional[TextRange], len(runs))}
	}
	points, ok := snap.windowPoints.Get()
	if !ok {
		return TextSelectionProjection{ranges: make([]Optional[TextRange], len(runs)), isActive: true}
	}
	ranges := make([]Optional[TextRange], len(runs))
	for i := range runs {
		ranges[i] = textSelectionRangeForRun(&runs[i], points.anchor, points.cursor)
	}
	return TextSelectionProjection{ranges: ranges, isActive: true}
}

// textSelectionRangeForRun is selection_range_for_run
// (U:text_selection.rs:459-511).
func textSelectionRangeForRun(run *TextSelectionRun, start, end Point) Optional[TextRange] {
	none := Optional[TextRange]{}
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
		return Some(TextRange{Start: 0, End: len(run.text)})
	}
	var selected *TextRange
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
				selected = &TextRange{Start: offset, End: offset}
			}
			selected.End = next
		}
	}
	if selected == nil {
		return none
	}
	return Some(*selected)
}

// textRowsExtent is text_rows_extent (U:text_selection.rs:513-533): the
// top of the layout's first row and the bottom of its last, each row
// lineHeight tall. The last row's top is where the caret after the last
// character sits (see the file comment).
func textRowsExtent(layout *TextLayout, lineHeight float32) (float32, float32) {
	top := layout.Bounds().Top()
	lastRowTop := top
	if position, ok := layout.PositionForIndex(layout.Len()); ok && position.Y > top {
		lastRowTop = position.Y
	}
	return top, lastRowTop + lineHeight
}

// textSelectionPointsForMultiClick is points_for_multi_click
// (U:text_selection.rs:535-557): the window points of the word (two
// clicks) or logical line (three or more) under position, plus the range
// the points came from — the RAW granularity range (before the
// double-click's trailing-whitespace extension), which the drag
// extension keeps as its anchor word (browser-core 19: Chromium's
// HandleMouseDragged extends from the selection's own positions,
// selection_controller.cc:693-699, not from a re-derived hit). The word
// range goes through the boundary seam: the default is the browser pair,
// whose double-click additionally extends over the word's trailing
// whitespace (Chromium's Windows SelectTrailingWhitespace,
// selection_controller.cc:187-209 — SelectClosestWordFromMouseEvent's
// kShouldAppend, selection_controller.cc:842-849).
func textSelectionPointsForMultiClick(runs []TextSelectionRun, position Point, clickCount int) (Point, Point, TextRange, bool) {
	var run *TextSelectionRun
	for i := range runs {
		if runs[i].bounds.Contains(position) {
			run = &runs[i]
			break
		}
	}
	if run == nil || run.layout == nil || len(run.text) != run.layout.Len() {
		return Point{}, Point{}, TextRange{}, false
	}
	offset, ok := run.layout.IndexForPosition(position)
	if !ok {
		return Point{}, Point{}, TextRange{}, false
	}
	var r TextRange
	switch {
	case clickCount == 2:
		if r, ok = textSelectionWordRangeAt(run.text, offset); !ok {
			return Point{}, Point{}, TextRange{}, false
		}
	case clickCount >= 3:
		r = textSelectionLineRangeAt(run.text, offset)
	default:
		return Point{}, Point{}, TextRange{}, false
	}
	if r.Start >= r.End {
		return Point{}, Point{}, TextRange{}, false
	}
	// The raw range is the granularity anchor; the painted/clicked
	// selection takes the trailing whitespace.
	sel := r
	if clickCount == 2 && textSelectionExtendsWordTrailingWhitespace {
		sel.End = extendWordOverTrailingWhitespace(run.text, r.End)
	}
	anchor, ok := run.layout.PositionForIndex(sel.Start)
	if !ok {
		return Point{}, Point{}, TextRange{}, false
	}
	cursor, ok := run.layout.PositionForIndex(sel.End)
	if !ok {
		return Point{}, Point{}, TextRange{}, false
	}
	return anchor, cursor, r, true
}

// extendWordOverTrailingWhitespace extends a word range's end over the
// whitespace run that follows it, stopping at a line break
// (AdjustSelectionWithTrailingWhitespace's SkipWhitespace,
// selection_controller.cc:187-209): a double-clicked word takes its
// trailing spaces and tabs (the Windows rule), never the paragraph break.
func extendWordOverTrailingWhitespace(text string, end int) int {
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if r == ' ' || r == '\t' {
			end += size
			continue
		}
		break
	}
	return end
}

// textSelectionPointInBand is point_in_selection_band
// (U:text_selection.rs:559-594); U:text/inline.rs:1321-1362 carries the
// same test, ported as pointInTextSelection (text_inline.go).
func textSelectionPointInBand(position Point, charWidth float32, start, end Point, lineHeight float32) bool {
	return pointInTextSelection(position, charWidth, start, end, lineHeight)
}

// ---------------------------------------------------------------------------
// Events and the participant state (U:text_selection.rs:596-779)
// ---------------------------------------------------------------------------

// TextSelectionContentKeyResolver looks up the stable virtualized content
// key at a participant content point (U:text_selection.rs:599).
type TextSelectionContentKeyResolver func(point Point, app *App) (TextSelectionContentKey, bool)

type (
	textSelectionFocusCallback func(w *Window, app *App)
	textSelectionClearHandler  func(app *App)
	textSelectionCopyCallback  func(app *App) string
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
	Snapshot Optional[TextSelectionSnapshot]
	Delta    Optional[float32]
}

// textSelectionEvents is EventEmitter<TextSelectionEvent> for the
// participant state (U:text_selection.rs:655).
var textSelectionEvents = DefineEvent[selectableTextState, TextSelectionEvent]()

// textSelectionCopyItem is CopyItem (U:text_selection.rs:617-621).
type textSelectionCopyItem struct {
	documentOrder uint64
	callback      textSelectionCopyCallback
	fallback      string
}

// resolveTextSelectionCopyItems is resolve_copy_items
// (U:text_selection.rs:623-635): copies in document order, blank ones
// dropped, joined by newlines. Callbacks run outside every state lease.
func resolveTextSelectionCopyItems(items []textSelectionCopyItem, app *App) string {
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
func dispatchTextSelectionClearHandlers(handlers []textSelectionClearHandler, app *App) {
	for _, handler := range handlers {
		handler(app)
	}
}

// selectableTextState is SelectableTextState (U:text_selection.rs:
// 643-779), the state behind a TextSelectionHandle.
type selectableTextState struct {
	fallbackCopyText   string
	projectedCopyText  Optional[string]
	runs               []TextSelectionRun
	localSelection     bool
	snapshot           Optional[TextSelectionSnapshot]
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
	s.projectedCopyText = Some(b.String())
	return states
}

// setSnapshot is U:text_selection.rs:741-748.
func (s *selectableTextState) setSnapshot(snapshot Optional[TextSelectionSnapshot], cx *Context[selectableTextState]) {
	if s.snapshot == snapshot {
		return
	}
	s.snapshot = snapshot
	s.projectedCopyText = Optional[string]{}
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventSelectionChanged, Snapshot: snapshot})
}

// clearState is U:text_selection.rs:750-757.
func (s *selectableTextState) clearState(cx *Context[selectableTextState]) textSelectionClearHandler {
	s.snapshot = Optional[TextSelectionSnapshot]{}
	s.projectedCopyText = Optional[string]{}
	s.localSelection = false
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventCleared})
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventSelectionChanged})
	return s.clear
}

// setAutoScroll is U:text_selection.rs:759-761.
func (s *selectableTextState) setAutoScroll(delta Optional[float32], cx *Context[selectableTextState]) {
	textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventAutoScroll, Delta: delta})
}

// focus defers the focus callback to after the current dispatch
// (U:text_selection.rs:763-767).
func (s *selectableTextState) focus(w *Window, app *App) {
	if callback := s.onFocus; callback != nil && w != nil {
		w.Defer(app, func(w *Window, app *App) { callback(w, app) })
	}
}

// copyItem is U:text_selection.rs:769-778. Browser-core 20: an absent
// projection cache (the snapshot changed since the last paint — the
// copy command can run mid-gesture, between frames) re-projects over
// the passed runs instead of falling back to the whole text, so the
// window's selected text reads the selection the gestures actually
// made. runs is the participant's current geometry (the registration's
// runs, else the handle's — windowSelectionState.runsOf).
func (s *selectableTextState) copyItem(documentOrder uint64, runs []TextSelectionRun, fromRegistration bool) (textSelectionCopyItem, bool) {
	if !s.snapshot.IsSome() && !s.localSelection {
		return textSelectionCopyItem{}, false
	}
	fallback, ok := s.projectedCopyText.Get()
	if !ok {
		// The automatic registration's runs live on the registration
		// itself, refreshed every frame — project on demand, so a copy
		// mid-gesture, between frames, reads the selection the gestures
		// made. The handle-reported runs (SelectableText) keep the pin's
		// staleness: their projection cache is refreshed by the paint,
		// and an invalidated one falls back to the fallback copy text.
		if fromRegistration && len(runs) > 0 {
			fallback = projectedTextOfRuns(s.snapshot, runs)
		} else {
			fallback = s.fallbackCopyText
		}
	}
	return textSelectionCopyItem{documentOrder: documentOrder, callback: s.copy, fallback: fallback}, true
}

// projectedTextOf assembles the selected text of the participant's
// CURRENT snapshot over its retained runs (the paint path's updateRuns
// cache, computed on demand — ProjectedText's body).
func projectedTextOf(s *selectableTextState) string {
	return projectedTextOfRuns(s.snapshot, s.runs)
}

// projectedTextOfRuns assembles the selected text of a snapshot over
// runs (the copy path's on-demand projection, browser-core 20).
func projectedTextOfRuns(snapshot Optional[TextSelectionSnapshot], runs []TextSelectionRun) string {
	states := projectTextSelectionRanges(snapshot, runs)
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
	return b.String()
}

// ---------------------------------------------------------------------------
// The participant handle (U:text_selection.rs:781-974)
// ---------------------------------------------------------------------------

// touchHandleHitbox is one laid-out touch handle with its hitbox.
type touchHandleHitbox struct {
	edge   SelectionEdge
	hitbox Hitbox
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
	entity Entity[selectableTextState]
}

// NewTextSelectionHandle creates a participant with fallback text for
// copying (U:text_selection.rs:792-795), owned by owner (see the file
// comment).
func NewTextSelectionHandle(fallbackCopyText string, cx AppContext, owner *Scope) TextSelectionHandle {
	return TextSelectionHandle{entity: NewEntity(cx, owner, func(s *selectableTextState, _ *Context[selectableTextState]) {
		*s = selectableTextState{fallbackCopyText: fallbackCopyText}
	})}
}

// EntityID is this participant's stable identity
// (U:text_selection.rs:797-800).
func (h TextSelectionHandle) EntityID() EntityID { return h.entity.EntityID() }

// Snapshot is the participant's current geometry selection snapshot
// (U:text_selection.rs:802-805).
func (h TextSelectionHandle) Snapshot(app *App) Optional[TextSelectionSnapshot] {
	return h.entity.Read(app, func(s *selectableTextState, _ *App) Optional[TextSelectionSnapshot] {
		return s.snapshot
	})
}

// SetFallbackCopyText sets the text copied while this participant
// participates (U:text_selection.rs:807-811).
func (h TextSelectionHandle) SetFallbackCopyText(text string, app *App) {
	h.entity.Update(app, func(s *selectableTextState, _ *Context[selectableTextState]) {
		s.fallbackCopyText = text
		s.projectedCopyText = Optional[string]{}
	})
}

// SetLocalSelection marks participant-local selection, such as
// select-all, as active (U:text_selection.rs:813-817).
func (h TextSelectionHandle) SetLocalSelection(active bool, app *App) {
	h.entity.Update(app, func(s *selectableTextState, _ *Context[selectableTextState]) {
		s.localSelection = active
	})
}

// HasLocalSelection reports whether participant-local selection is active
// (U:text_selection.rs:819-822).
func (h TextSelectionHandle) HasLocalSelection(app *App) bool {
	return h.entity.Read(app, func(s *selectableTextState, _ *App) bool {
		return s.localSelection
	})
}

// Register registers this participant and its geometry for the current
// frame (U:text_selection.rs:824-840). It is a no-op until the window
// renders a TextSelectionLayer.
func (h TextSelectionHandle) Register(registration TextSelectionRegistration, w *Window, app *App) {
	if scope, ok := currentTextSelectionScope(w.ID(), app); ok {
		registration.scope = scope
	}
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return
	}
	if !registration.contentMask.IsSome() {
		if mask, ok := textSelectionWindowContentMask(w); ok {
			registration.contentMask = Some(mask)
		}
	}
	_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
		s.registerParticipant(h, &registration, cx.App())
	})
}

// textSelectionWindowContentMask is the content mask of the frame being
// drawn, false outside a draw (ContentMask panics there).
func textSelectionWindowContentMask(w *Window) (mask Bounds, ok bool) {
	defer func() {
		if recover() != nil {
			mask, ok = Bounds{}, false
		}
	}()
	return ContentMask(w), true
}

// UpdateRuns projects the current snapshot onto plain-text runs and
// caches their copy text (U:text_selection.rs:842-845).
func (h TextSelectionHandle) UpdateRuns(runs []TextSelectionRun, app *App) TextSelectionProjection {
	return h.entity.UpdateWith(app, func(s *selectableTextState, _ *Context[selectableTextState]) TextSelectionProjection {
		return s.updateRuns(runs)
	})
}

// setHitTestRuns retains geometry for word hit testing; rich text renders
// its own selection (U:text_selection.rs:847-850).
// ProjectedRanges projects the participant's current snapshot onto the
// runs it last reported (the paint path's UpdateRuns geometry) and
// returns the selected UTF-8 SOURCE byte ranges — the browser-parity
// harness's selection read (browser-core 19: the recorded page's
// window.getSelection() offsets against the model's projection). The
// ranges pair with the runs in report order.
func (h TextSelectionHandle) ProjectedRanges(app *App) []Optional[TextRange] {
	ranges, err := h.entity.Downgrade().TryRead(app, func(s *selectableTextState, _ *App) []Optional[TextRange] {
		return append([]Optional[TextRange](nil), projectTextSelectionRanges(s.snapshot, s.runs).ranges...)
	})
	if err != nil {
		return nil
	}
	return ranges
}

// ProjectedText returns the participant's selected text as the copy
// path assembles it (updateRuns' projection over the runs it last
// reported), without touching the cached copy — the browser-parity
// harness's selection read (browser-core 19), which samples the
// projection directly rather than through the paint-refreshed cache.
func (h TextSelectionHandle) ProjectedText(app *App) string {
	out, err := h.entity.Downgrade().TryRead(app, func(s *selectableTextState, _ *App) string {
		return projectedTextOf(s)
	})
	if err != nil {
		return ""
	}
	return out
}

// SetHitTestRuns retains geometry for word hit testing; rich text
// renders its own selection (U:text_selection.rs:847-850) — exported
// for Base's TextView adapter when the model moved into the core
// (browser-core 19).
func (h TextSelectionHandle) SetHitTestRuns(runs []TextSelectionRun, app *App) {
	h.setHitTestRuns(runs, app)
}

func (h TextSelectionHandle) setHitTestRuns(runs []TextSelectionRun, app *App) {
	h.entity.Update(app, func(s *selectableTextState, _ *Context[selectableTextState]) {
		s.runs = append([]TextSelectionRun(nil), runs...)
	})
}

// Subscribe subscribes to participant selection notifications
// (U:text_selection.rs:852-859). See the file comment.
func (h TextSelectionHandle) Subscribe(callback func(event *TextSelectionEvent, app *App), app *App) Subscription {
	return h.entity.UpdateWith(app, func(_ *selectableTextState, cx *Context[selectableTextState]) Subscription {
		return textSelectionEvents.SubscribeSelf(cx, func(_ *selectableTextState, event *TextSelectionEvent, cx *Context[selectableTextState]) {
			callback(event, cx.App())
		})
	})
}

// RefreshWindowOnChange subscribes w to refresh whenever this
// participant's selection changes (U:text_selection.rs:861-873). Retain
// the subscription or detach it.
func (h TextSelectionHandle) RefreshWindowOnChange(w *Window, app *App) Subscription {
	windowID := w.ID()
	return h.Subscribe(func(event *TextSelectionEvent, app *App) {
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
func (h TextSelectionHandle) FocusWith(callback func(w *Window, app *App), app *App) {
	h.entity.Update(app, func(s *selectableTextState, _ *Context[selectableTextState]) { s.onFocus = callback })
}

// ClearWith sets the synchronous participant cleanup command used by
// window clear (U:text_selection.rs:881-884).
func (h TextSelectionHandle) ClearWith(callback func(app *App), app *App) {
	h.entity.Update(app, func(s *selectableTextState, _ *Context[selectableTextState]) { s.clear = callback })
}

// CopyWith sets a participant-specific copy projection
// (U:text_selection.rs:886-889).
func (h TextSelectionHandle) CopyWith(callback func(app *App) string, app *App) {
	h.entity.Update(app, func(s *selectableTextState, _ *Context[selectableTextState]) { s.copy = callback })
}

// ResolveContentKeyWith sets a participant-specific lookup for stable
// virtualized content keys (U:text_selection.rs:961-969).
func (h TextSelectionHandle) ResolveContentKeyWith(callback TextSelectionContentKeyResolver, app *App) {
	h.entity.Update(app, func(s *selectableTextState, _ *Context[selectableTextState]) {
		s.contentKeyResolver = callback
	})
}

// PrepaintTouchHandles lays out the touch handles at the ends of this
// participant's selection, as painted last frame, each with a hitbox
// where the finger takes it. Call during the participant's prepaint and
// hand the result to PaintTouchHandles (U:text_selection.rs:891-912).
func (h TextSelectionHandle) PrepaintTouchHandles(w *Window, app *App) TouchHandleLayout {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return TouchHandleLayout{}
	}
	handles, _ := state.TryRead(app, func(s *windowSelectionState, _ *App) []touchHandleCaret {
		return s.touchHandlesOf(h.EntityID())
	})
	var layout TouchHandleLayout
	for _, handle := range handles {
		hitbox := w.InsertHitbox(TouchHandleHitBounds(handle.edge, handle.caret), HitboxBlockMouse)
		layout.hitboxes = append(layout.hitboxes, touchHandleHitbox{edge: handle.edge, hitbox: hitbox})
	}
	return layout
}

// PaintTouchHandles paints the touch handles at the ends of this
// participant's selection in color, where the participant is in the paint
// order, and takes the finger's drag from them. Call at the end of the
// participant's paint, after Register (U:text_selection.rs:914-959).
func (h TextSelectionHandle) PaintTouchHandles(layout *TouchHandleLayout, color Hsla, w *Window, app *App) {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return
	}
	handles, _ := state.TryRead(app, func(s *windowSelectionState, _ *App) []touchHandleCaret {
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
		_ = state.TryUpdate(app, func(s *windowSelectionState, _ *Context[windowSelectionState]) {
			s.registerTouchUI(hitbox.Bounds)
		})
		// Mouse: the drag for a pointer.
		w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
			ev, ok := event.(*MouseDownEvent)
			if !ok || phase != DispatchBubble || ev.Button != MouseButtonLeft || !hitbox.IsHovered(w) {
				return
			}
			if !state.IsUpgradable() {
				return
			}
			app.StopPropagation()
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
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
	participant  WeakEntity[selectableTextState]
	registration *TextSelectionRegistration
	generation   uint64
}

// selectionEndpoint is SelectionEndpoint (U:text_selection.rs:983-1022).
type selectionEndpoint struct {
	participant   Optional[WeakEntity[selectableTextState]]
	point         Point
	inside        bool
	insideText    bool
	contentKey    Optional[TextSelectionContentKey]
	resolver      TextSelectionContentKeyResolver
	resolverPoint Point
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
func (e *selectionEndpoint) resolve(participants map[EntityID]participantRegistration) (Point, bool) {
	participant, ok := e.participant.Get()
	if !ok {
		return Point{}, false
	}
	registration, ok := participants[participant.EntityID()]
	if !ok || !participant.IsUpgradable() {
		return Point{}, false
	}
	return pointAdd(pointAdd(e.point, registration.registration.scrollOffset), registration.registration.bounds.Origin), true
}

// entityID is U:text_selection.rs:1017-1021.
func (e *selectionEndpoint) entityID() Optional[EntityID] {
	if participant, ok := e.participant.Get(); ok {
		return Some(participant.EntityID())
	}
	return Optional[EntityID]{}
}

// touchEdges is the ends as last laid out (U:text_selection.rs:1040-1044).
type touchEdges struct {
	start, end               Bounds
	startVisible, endVisible bool
}

// touchSelection is what a long press left behind: the handles and the
// edit menu (U:text_selection.rs:1024-1071).
type touchSelection struct {
	// active: the current selection was made by touch and carries handles.
	active   bool
	menuOpen bool
	drag     Optional[edgeDrag]
	// uiBounds and previousUIBounds are where the handles and the menu
	// were painted this frame and the frame before.
	uiBounds, previousUIBounds []Bounds
	lastEdges                  Optional[touchEdges]
}

// covers is U:text_selection.rs:1048-1055.
func (t *touchSelection) covers(position Point) bool {
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
	t.drag = Optional[edgeDrag]{}
	t.lastEdges = Optional[touchEdges]{}
	return had
}

// autoScrollStallGeometry is the anchor geometry auto_scroll_stall keeps
// (U:text_selection.rs:1088-1092).
type autoScrollStallGeometry struct {
	bounds Bounds
	scroll Point
}

// windowSelectionState is WindowSelectionState (U:text_selection.rs:
// 1073-1097), the window-local generic text-selection state. Its zero
// value is the Rust Default.
// selectionGranularity is the expansion granularity a multi-click set
// and a still-held drag extends by (browser-core 19, Chromium's
// TextGranularity carried by the selection —
// selection_controller.cc:693-699: HandleMouseDragged extends by
// Selection().Granularity(), the value the double/triple click's
// SetSelectionOptions carried).
type selectionGranularity uint8

const (
	// selectionGranularityCharacter is a plain press-drag: the endpoints
	// follow the pointer exactly.
	selectionGranularityCharacter selectionGranularity = iota
	// selectionGranularityWord is a double-click's word granularity.
	selectionGranularityWord
	// selectionGranularityParagraph is a triple-click's paragraph
	// granularity.
	selectionGranularityParagraph
)

type windowSelectionState struct {
	participants           map[EntityID]participantRegistration
	activeScope            TextSelectionScopeID
	anchor                 *selectionEndpoint
	cursor                 *selectionEndpoint
	pendingExtensionAnchor *selectionEndpoint
	isSelecting            bool
	didHitText             bool
	// granularity is the multi-click's expansion granularity: while the
	// press that set it is still held, a drag extends by it (the moving
	// endpoint snaps to the granularity boundary, the anchor word kept
	// whole — Chromium's ExtendSelectionAsDirectional,
	// selection_controller.cc:316-375).
	granularity selectionGranularity
	// granularityAnchor is the multi-click's own range, from the
	// selection's stored positions (selection_controller.cc:693-699): the
	// anchor word a granularity drag keeps whole.
	granularityAnchor    *granularityAnchorRange
	frameGeneration      uint64
	finishFrameScheduled bool
	refreshHeldCursor    bool
	mouseDownPrepared    bool
	autoScroll           AutoScroll
	autoScrollStall      Optional[autoScrollStallGeometry]
	autoScrollStalled    uint8
	touch                touchSelection
	// entityID is this entity, so touch changes can notify observers from
	// paths that only hold an App.
	entityID Optional[EntityID]
}

// granularityAnchorRange is the stored multi-click range of one
// participant's run: the anchor word a granularity drag keeps whole.
type granularityAnchorRange struct {
	participant EntityID
	run         TextSelectionRun
	rng         TextRange
}

// textSelectionResolveContentKeys runs the endpoints' content key
// resolvers outside the state lease and stores their keys
// (U:text_selection.rs:1100-1126).
func textSelectionResolveContentKeys(state WeakEntity[windowSelectionState], app *App) {
	type pending struct {
		resolver TextSelectionContentKeyResolver
		point    Point
	}
	pendingKeys, err := state.TryUpdateWith(app, func(s *windowSelectionState, _ *Context[windowSelectionState]) [2]pending {
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
	var resolved [2]Optional[TextSelectionContentKey]
	for i, p := range pendingKeys {
		if p.resolver != nil {
			if key, ok := p.resolver(p.point, app); ok {
				resolved[i] = Some(key)
			}
		}
	}
	_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
		for i, endpoint := range []*selectionEndpoint{s.anchor, s.cursor} {
			if key, ok := resolved[i].Get(); ok && endpoint != nil {
				endpoint.contentKey = Some(key)
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
	states map[WindowID]WeakEntity[windowSelectionState]
}

func textSelectionRegistry(app *App) *selectionStateRegistry {
	if r, ok := TryGlobal[*selectionStateRegistry](app); ok {
		return r
	}
	r := &selectionStateRegistry{states: map[WindowID]WeakEntity[windowSelectionState]{}}
	app.SetGlobal(r)
	return r
}

// pendingTextSelectionScopes is PendingTextSelectionScopes
// (U:text_selection.rs:2222-2225).
type pendingTextSelectionScopes struct {
	scopes map[WindowID]TextSelectionScopeID
}

// textSelectionStateAcquire is WindowSelectionState::acquire
// (U:text_selection.rs:1127-1177): the window's live state, retained into
// owner, or a new one owned by owner.
func textSelectionStateAcquire(windowID WindowID, app *App, owner *Scope) WeakEntity[windowSelectionState] {
	registry := textSelectionRegistry(app)
	if state, ok := registry.states[windowID]; ok && state.IsUpgradable() {
		state.UpgradeInto(owner)
		return state
	}
	var activeScope TextSelectionScopeID
	if pending, ok := TryGlobal[*pendingTextSelectionScopes](app); ok {
		activeScope = pending.scopes[windowID]
		delete(pending.scopes, windowID)
	}
	state := NewEntity(app, owner, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
		entityID := cx.EntityID()
		cx.OnRelease(func(s *windowSelectionState, app *App) {
			handlers := s.clearState(app)
			if registry, ok := TryGlobal[*selectionStateRegistry](app); ok {
				if current, ok := registry.states[windowID]; ok && current.EntityID() == entityID {
					delete(registry.states, windowID)
				}
			}
			if len(handlers) > 0 {
				app.Defer(func(app *App) { dispatchTextSelectionClearHandlers(handlers, app) })
			}
		}).Detach()
		*s = windowSelectionState{activeScope: activeScope, entityID: Some(entityID)}
	})
	weak := state.Downgrade()
	textSelectionRegistry(app).states[windowID] = weak
	return weak
}

// textSelectionStateEnsure is WindowSelectionState::ensure
// (U:text_selection.rs:1179-1182, test-only upstream), owned by the
// window's scope.
func textSelectionStateEnsure(w *Window, app *App) WeakEntity[windowSelectionState] {
	return textSelectionStateAcquire(w.ID(), app, w.Scope())
}

// textSelectionStateExisting is WindowSelectionState::existing
// (U:text_selection.rs:1184-1192).
func textSelectionStateExisting(w *Window, app *App) (WeakEntity[windowSelectionState], bool) {
	registry, ok := TryGlobal[*selectionStateRegistry](app)
	if !ok {
		return WeakEntity[windowSelectionState]{}, false
	}
	state, ok := registry.states[w.ID()]
	if !ok || !state.IsUpgradable() {
		return WeakEntity[windowSelectionState]{}, false
	}
	return state, true
}

// setActiveScope updates the active scope (U:text_selection.rs:1194-1199,
// test-only upstream).
func (s *windowSelectionState) setActiveScope(scope TextSelectionScopeID, app *App) {
	dispatchTextSelectionClearHandlers(s.setActiveScopeState(scope, app), app)
}

// setActiveScopeState is U:text_selection.rs:1201-1213.
func (s *windowSelectionState) setActiveScopeState(scope TextSelectionScopeID, app *App) []textSelectionClearHandler {
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
func (s *windowSelectionState) finishFrame(app *App) []textSelectionClearHandler {
	s.finishFrameScheduled = false
	type stale struct {
		id          EntityID
		participant WeakEntity[selectableTextState]
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
		handler, err := p.participant.TryUpdateWith(app, func(state *selectableTextState, cx *Context[selectableTextState]) textSelectionClearHandler {
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
func (s *windowSelectionState) registerParticipant(selection TextSelectionHandle, registration *TextSelectionRegistration, app *App) {
	// The idle guard (browser-core 20): while nothing is selected and no
	// gesture runs, the prune/publish walk — O(participants) per
	// registration — is a no-op (every snapshot is already none); skip
	// it so a frame of n plain texts costs n registrations, not n².
	if s.idle() {
		if s.participants == nil {
			s.participants = map[EntityID]participantRegistration{}
		}
		s.participants[selection.EntityID()] = participantRegistration{
			participant:  selection.entity.Downgrade(),
			registration: registration,
			generation:   s.frameGeneration,
		}
		return
	}
	s.pruneDeadParticipants()
	id := selection.EntityID()
	previous, hadPrevious := s.participants[id]
	if s.isSelecting && registration.selfScroll && s.anchor != nil &&
		s.anchor.entityID() == Some(id) && hadPrevious &&
		(previous.registration.scrollOffset != registration.scrollOffset || previous.registration.bounds != registration.bounds) {
		s.refreshHeldCursor = true
	}
	// The handles sit on the painted ends; when those moved, whoever draws
	// the handles needs to know.
	edgesMoved := s.touch.active && (!hadPrevious || previous.registration.selectionEdges != registration.selectionEdges)
	if s.participants == nil {
		s.participants = map[EntityID]participantRegistration{}
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
func (s *windowSelectionState) begin(position Point, extend bool, app *App) {
	s.beginImpl(position, extend, false, nil, app)
}

// update moves the gesture using bounds hit testing
// (U:text_selection.rs:1310-1314, test-only upstream).
func (s *windowSelectionState) update(position Point, app *App) {
	s.updateImpl(position, nil, app)
}

// end ends the current gesture and keeps its selection visible
// (U:text_selection.rs:1316-1329).
func (s *windowSelectionState) end(app *App) {
	s.pendingExtensionAnchor = nil
	s.granularity = selectionGranularityCharacter
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
func (s *windowSelectionState) clear(app *App) {
	dispatchTextSelectionClearHandlers(s.clearState(app), app)
}

// clearState is U:text_selection.rs:1337-1353.
func (s *windowSelectionState) clearState(app *App) []textSelectionClearHandler {
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
func (s *windowSelectionState) clearParticipants(app *App) []textSelectionClearHandler {
	var handlers []textSelectionClearHandler
	for _, registration := range s.participants {
		handler, err := registration.participant.TryUpdateWith(app, func(state *selectableTextState, cx *Context[selectableTextState]) textSelectionClearHandler {
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
func (s *windowSelectionState) touchChanged(app *App) {
	var participants []WeakEntity[selectableTextState]
	for _, registration := range s.participants {
		selected, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *App) bool {
			return p.snapshot.IsSome()
		})
		if err == nil && selected {
			participants = append(participants, registration.participant)
		}
	}
	entityID := s.entityID
	app.Defer(func(app *App) {
		for _, participant := range participants {
			_ = participant.TryUpdate(app, func(_ *selectableTextState, cx *Context[selectableTextState]) {
				textSelectionEvents.Emit(cx, TextSelectionEvent{Kind: TextSelectionEventTouchSelectionChanged})
			})
		}
		if id, ok := entityID.Get(); ok {
			app.Notify(id)
		}
	})
}

// copyItems is U:text_selection.rs:1387-1397. Browser-core 20: the
// item's fallback projection runs over the participant's current runs
// (the registration's when it carries them, else the handle's —
// runsOf), so a copy mid-gesture, between frames, reads the selection
// the gestures made.
func (s *windowSelectionState) copyItems(app *App) []textSelectionCopyItem {
	var items []textSelectionCopyItem
	for _, registration := range s.participants {
		order := registration.registration.documentOrder
		runs, fromRegistration := s.runsOf(registration.participant, app)
		item, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *App) Optional[textSelectionCopyItem] {
			if item, ok := p.copyItem(order, runs, fromRegistration); ok {
				return Some(item)
			}
			return Optional[textSelectionCopyItem]{}
		})
		if value, ok := item.Get(); err == nil && ok {
			items = append(items, value)
		}
	}
	return items
}

// selectedText is U:text_selection.rs:1399-1402 (test-only upstream).
func (s *windowSelectionState) selectedText(app *App) string {
	return resolveTextSelectionCopyItems(s.copyItems(app), app)
}

// runsOf resolves a participant's current runs: the registration's
// runs (the automatic registration carries them — browser-core 20), or,
// when the registration carries none, the handle's retained runs
// (SelectableText's UpdateRuns geometry, U:842-845). Call OUTSIDE the
// participant's own entity update: the entity read falls back to the
// handle, and re-entering it from inside its closure would trip the
// reentry guard.
func (s *windowSelectionState) runsOf(participant WeakEntity[selectableTextState], app *App) ([]TextSelectionRun, bool) {
	if registration, ok := s.participants[participant.EntityID()]; ok {
		if len(registration.registration.runs) > 0 {
			return registration.registration.runs, true
		}
	}
	runs, _ := participant.TryRead(app, func(p *selectableTextState, _ *App) []TextSelectionRun {
		return p.runs
	})
	return runs, false
}

// selectsText reports whether the endpoints take in at least one
// character of some participant's painted text
// (U:text_selection.rs:1404-1421).
func (s *windowSelectionState) selectsText(app *App) bool {
	if !s.snapshot().IsSome() {
		return false
	}
	for _, registration := range s.participants {
		runs, _ := s.runsOf(registration.participant, app)
		selects, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *App) bool {
			for _, r := range projectTextSelectionRanges(p.snapshot, runs).ranges {
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
func (s *windowSelectionState) hasSelection(app *App) bool {
	if s.snapshot().IsSome() {
		return true
	}
	for _, registration := range s.participants {
		local, err := registration.participant.TryRead(app, func(p *selectableTextState, _ *App) bool {
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
func (s *windowSelectionState) snapshot() Optional[TextSelectionSnapshot] {
	none := Optional[TextSelectionSnapshot]{}
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
	return Some(newTextSelectionSnapshot(s.anchor.snapshot(), s.cursor.snapshot()).
		withSelecting(s.isSelecting).
		withWindowPoints(Some(TextSelectionWindowPoints{anchor: anchor, cursor: cursor})))
}

// prepareForMouseDown is U:text_selection.rs:1456-1476.
func (s *windowSelectionState) prepareForMouseDown(extend bool, app *App) []textSelectionClearHandler {
	var pending *selectionEndpoint
	if extend {
		pending = s.anchor.clone()
	}
	s.stopAnchorAutoScroll(app)
	s.anchor = nil
	s.cursor = nil
	s.pendingExtensionAnchor = nil
	s.isSelecting = false
	s.granularity = selectionGranularityCharacter
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
	caret Bounds
}

// touchSelection is the touch selection laid out for its handles and edit
// menu (U:text_selection.rs:1478-1526).
func (s *windowSelectionState) touchSelection(app *App) Optional[TouchSelectionSnapshot] {
	none := Optional[TouchSelectionSnapshot]{}
	if !s.touch.active {
		return none
	}
	type end struct {
		order   uint64
		caret   Bounds
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
		s.touch.lastEdges = Some(edges)
	case s.touch.drag.IsSome():
		var ok bool
		if edges, ok = s.touch.lastEdges.Get(); !ok {
			return none
		}
	default:
		return none
	}
	var dragging Optional[SelectionEdge]
	if drag, ok := s.touch.drag.Get(); ok {
		dragging = Some(drag.edge)
	}
	return Some(newTouchSelectionSnapshot(edges.start, edges.end).
		withEdgeVisible(SelectionEdgeStart, edges.startVisible).
		withEdgeVisible(SelectionEdgeEnd, edges.endVisible).
		withMenuOpen(s.touch.menuOpen).
		withDragging(dragging))
}

// touchHandlesOf is the handles participant paints: the start when no
// participant earlier in document order has a selection end, the end when
// none later has (U:text_selection.rs:1528-1567).
func (s *windowSelectionState) touchHandlesOf(participant EntityID) []touchHandleCaret {
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
func (s *windowSelectionState) keepTouchSelection(app *App) {
	s.touch.active = s.snapshot().IsSome()
	s.touch.menuOpen = s.touch.active
	s.touch.drag = Optional[edgeDrag]{}
	s.touchChanged(app)
}

// registerTouchUI records where the handles and the menu are painted this
// frame (U:text_selection.rs:1577-1580).
func (s *windowSelectionState) registerTouchUI(bounds Bounds) {
	s.touch.uiBounds = append(s.touch.uiBounds, bounds)
}

// closeEditMenu is U:text_selection.rs:1582-1588.
func (s *windowSelectionState) closeEditMenu(app *App) {
	if !s.touch.menuOpen {
		return
	}
	s.touch.menuOpen = false
	s.touchChanged(app)
}

// editMenuOnScroll steps the menu aside while a finger scrolls and brings
// it back once the finger lifts (U:text_selection.rs:1590-1606).
func (s *windowSelectionState) editMenuOnScroll(phase TouchPhase, app *App) {
	if !s.touch.active || s.touch.drag.IsSome() {
		return
	}
	switch phase {
	case TouchPhaseEnded, TouchPhaseCancelled:
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
func endpointAt(participant WeakEntity[selectableTextState], registration *TextSelectionRegistration, resolver TextSelectionContentKeyResolver, windowPoint Point) *selectionEndpoint {
	contentPoint := pointSub(pointSub(windowPoint, registration.bounds.Origin), registration.scrollOffset)
	return &selectionEndpoint{
		participant:   Some(participant),
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
func (s *windowSelectionState) selectAllTouched(app *App) {
	if !s.touch.active {
		return
	}
	participant, registration, ok := s.anchorRegistration()
	if !ok {
		return
	}
	type ends struct {
		anchor, cursor Point
		resolver       TextSelectionContentKeyResolver
		ok             bool
	}
	// From the participant's text runs, not its painted line bounds: those
	// are clipped to the viewport.
	runs, _ := s.runsOf(participant, app)
	found, err := participant.TryRead(app, func(p *selectableTextState, _ *App) ends {
		if len(runs) == 0 {
			return ends{}
		}
		first, last := runs[0], runs[0]
		for _, run := range runs[1:] {
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
			anchor:   Point{X: start.X + inset, Y: start.Y + first.layout.LineHeight()/2},
			cursor:   Point{X: end.X - inset, Y: end.Y + last.layout.LineHeight()/2},
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
func (s *windowSelectionState) beginEdgeDrag(edge SelectionEdge, finger Point, w *Window, app *App) {
	snapshot, ok := s.touchSelection(app).Get()
	if !ok {
		return
	}
	dispatchTextSelectionClearHandlers(s.prepareForMouseDown(false, app), app)
	held := snapshot.Edge(edge.Opposite())
	// Just inside the held end, so the anchor lands on the character it
	// marks rather than on the boundary between two participants.
	const nudge = 1
	anchorPoint := Point{X: held.Left() + nudge, Y: held.Center().Y}
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
	s.touch.drag = Some(drag)
	s.publishSnapshots(app)
	s.touchChanged(app)
}

// updateEdgeDrag moves the dragged end; a handle never collapses the
// selection, and dragging one end past the other swaps them
// (U:text_selection.rs:1709-1740).
func (s *windowSelectionState) updateEdgeDrag(finger Point, w *Window, cx *Context[windowSelectionState]) {
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
				s.touch.drag = Some(d)
			}
		}
	}
	s.touchChanged(app)
}

// endEdgeDrag ends the handle drag and reopens the edit menu
// (U:text_selection.rs:1742-1748).
func (s *windowSelectionState) endEdgeDrag(app *App) {
	if !s.touch.drag.IsSome() {
		return
	}
	s.touch.drag = Optional[edgeDrag]{}
	s.end(app)
	s.keepTouchSelection(app)
}

// beginInWindow is U:text_selection.rs:1750-1758.
func (s *windowSelectionState) beginInWindow(position Point, extend bool, w *Window, app *App) {
	s.beginImpl(position, extend, true, w, app)
}

// updateInWindow moves the cursor unless a drag and drop is active
// (U:text_selection.rs:1760-1770).
func (s *windowSelectionState) updateInWindow(position Point, w *Window, cx *Context[windowSelectionState]) {
	if !cx.App().HasActiveDrag() {
		s.updateImpl(position, w, cx.App())
		s.updateAutoScroll(position, w, cx)
	}
}

// selectAt selects the word (two clicks) or line (three or more) under
// position (U:text_selection.rs:1772-1822).
func (s *windowSelectionState) selectAt(position Point, clickCount int, w *Window, app *App) {

	if IsTextSelectionSuppressed(app) {
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
	runs, _ := s.runsOf(participant, app)
	type found struct {
		anchor, cursor Point
		rng            TextRange
		run            TextSelectionRun
		hasRun         bool
		resolver       TextSelectionContentKeyResolver
		ok             bool
	}
	points, err := participant.TryRead(app, func(p *selectableTextState, _ *App) found {
		anchor, cursor, rng, ok := textSelectionPointsForMultiClick(runs, position, clickCount)
		out := found{anchor: anchor, cursor: cursor, rng: rng, resolver: p.contentKeyResolver, ok: ok}
		for i := range runs {
			if runs[i].bounds.Contains(position) {
				out.run, out.hasRun = runs[i], true
				break
			}
		}
		return out
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
	// The multi-click's granularity arms the drag (browser-core 19,
	// Chromium's selection_state_ = kExtendedSelection with the
	// granularity the SetSelectionOptions carried,
	// selection_controller.cc:1131-1149 and 693-699): a still-held drag
	// extends by the same granularity, the moving endpoint snapping to
	// the boundary while the anchor word stays whole — from the
	// selection's own positions (the raw granularity range stored here),
	// exactly like ExtendSelectionAsDirectional reads the selection's
	// stored endpoints rather than re-deriving them from the pointer
	// (selection_controller.cc:316-375).
	s.isSelecting = true
	s.granularity = selectionGranularityCharacter
	if clickCount == 2 {
		s.granularity = selectionGranularityWord
	} else if clickCount >= 3 {
		s.granularity = selectionGranularityParagraph
	}
	if points.hasRun {
		s.granularityAnchor = &granularityAnchorRange{
			participant: participant.EntityID(),
			run:         points.run,
			rng:         points.rng,
		}
	} else {
		s.granularityAnchor = nil
	}
	_, _ = participant.TryRead(app, func(p *selectableTextState, app *App) struct{} {
		p.focus(w, app)
		return struct{}{}
	})
	s.publishSnapshots(app)
}

// updateInWindowWithActiveDrag is U:text_selection.rs:1824-1835
// (test-only upstream).
func (s *windowSelectionState) updateInWindowWithActiveDrag(position Point, activeDrag bool, w *Window, app *App) {
	if !activeDrag {
		s.updateImpl(position, w, app)
	}
}

// beginImpl is U:text_selection.rs:1837-1877.
func (s *windowSelectionState) beginImpl(position Point, extend, alreadyPrepared bool, w *Window, app *App) {

	if IsTextSelectionSuppressed(app) {
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
	var focusParticipant Optional[WeakEntity[selectableTextState]]
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
	s.granularity = selectionGranularityCharacter
	if participant, ok := focusParticipant.Get(); ok && w != nil {
		_, _ = participant.TryRead(app, func(p *selectableTextState, app *App) struct{} {
			p.focus(w, app)
			return struct{}{}
		})
	}
	s.publishSnapshots(app)
}

// updateImpl is U:text_selection.rs:1879-1890, plus the
// granularity-drag extension of browser-core 19: when the press that set
// the granularity is still held, the endpoints snap to the granularity's
// boundaries (ExtendSelectionAsDirectional,
// selection_controller.cc:316-375).
func (s *windowSelectionState) updateImpl(position Point, w *Window, app *App) {
	if !s.isSelecting {
		return
	}
	endpoint := s.endpoint(position, w, app)
	if s.granularity != selectionGranularityCharacter {
		s.updateGranularityEndpoints(endpoint, position, w, app)
	} else {
		s.cursor = endpoint
	}
	s.didHitText = s.didHitText || endpoint.insideText
	if w == nil {
		s.updateParticipantAutoScroll(position, app)
	}
	s.publishSnapshots(app)
}

// updateGranularityEndpoints snaps the selection to the granularity's
// boundaries for a drag extending a multi-click's selection (browser-core
// 19, Chromium's ExtendSelectionAsDirectional,
// selection_controller.cc:316-375): extending forward keeps the anchor's
// word start and takes the word END under the pointer; extending backward
// swaps the roles — the anchor becomes the word START under the pointer
// and the cursor the anchor word's END (SetBaseAndExtent(new_end,
// new_start)) — so the selection always covers whole words. The anchor
// word is the stored multi-click range (the selection's own positions,
// selection_controller.cc:693-699 — not a re-derived hit), and the hit
// must land in a laid-out run of the same participant; anything else
// keeps the plain character endpoint (the drag leaves the text).
func (s *windowSelectionState) updateGranularityEndpoints(hit *selectionEndpoint, position Point, w *Window, app *App) {
	s.cursor = hit
	anchor := s.granularityAnchor
	if anchor == nil || s.anchor == nil {
		return
	}
	hitParticipant, ok := hit.participant.Get()
	if !ok || hitParticipant.EntityID() != anchor.participant {
		return
	}
	anchorParticipant := hitParticipant
	registration, ok := s.participants[anchor.participant]
	if !ok {
		return
	}
	expand := func(text string, offset int) (TextRange, bool) {
		if s.granularity == selectionGranularityParagraph {
			r := textSelectionLineRangeAt(text, offset)
			if r.Start >= r.End {
				return TextRange{}, false
			}
			return r, true
		}
		return textSelectionWordRangeAt(text, offset)
	}
	// hitSegment is the granularity range under the pointer, in the
	// participant's own runs.
	type segment struct {
		run     *TextSelectionRun
		rangeAt TextRange
		ok      bool
	}
	runs, _ := s.runsOf(anchorParticipant, app)
	segmentAt := func(point Point) segment {
		run, err := anchorParticipant.TryRead(app, func(p *selectableTextState, _ *App) *TextSelectionRun {
			for i := range runs {
				if runs[i].bounds.Contains(point) {
					return &runs[i]
				}
			}
			return nil
		})
		if err != nil || run == nil || run.layout == nil || len(run.text) != run.layout.Len() {
			return segment{}
		}
		offset, ok := run.layout.IndexForPosition(point)
		if !ok {
			return segment{}
		}
		r, ok := expand(run.text, offset)
		if !ok || r.Start >= r.End {
			return segment{}
		}
		return segment{run: run, rangeAt: r, ok: true}
	}
	// The anchor word is the stored range; its RUN resolves against the
	// participant's CURRENT runs (browser-core 20): a frame painted while
	// the press is held — the drag's own repaints — replaces the
	// participant's runs, and the stored run copy's layout pointer then
	// identifies nothing; the anchor word's positions live in the
	// participant's current geometry, exactly like Chromium's
	// ExtendSelectionAsDirectional reads the selection's stored positions
	// against the current layout (selection_controller.cc:316-375).
	anchorRun, err := anchorParticipant.TryRead(app, func(p *selectableTextState, _ *App) *TextSelectionRun {
		for i := range runs {
			run := &runs[i]
			if run.documentOrder == anchor.run.documentOrder &&
				anchor.rng.End <= len(run.text) && len(run.text) == run.layout.Len() {
				return run
			}
		}
		return nil
	})
	if err != nil || anchorRun == nil || anchorRun.layout == nil || anchor.rng.Start >= anchor.rng.End {
		return
	}
	hitSegment := segmentAt(position)
	if !hitSegment.ok {
		return
	}
	// Forward or backward: the run order first (the document order of the
	// two runs), then the ranges inside a shared run. Both runs come from
	// the participant's current run slice, so sameness is identity of the
	// run itself.
	sameRun := hitSegment.run == anchorRun ||
		(hitSegment.run.layout == anchorRun.layout &&
			hitSegment.run.bounds == anchorRun.bounds &&
			hitSegment.run.documentOrder == anchorRun.documentOrder)
	forward := true
	if sameRun {
		forward = hitSegment.rangeAt.Start >= anchor.rng.End
	} else if hitSegment.run.documentOrder < anchorRun.documentOrder ||
		(hitSegment.run.documentOrder == anchorRun.documentOrder && hitSegment.run.bounds.Top() < anchorRun.bounds.Top()) {
		forward = false
	}
	var anchorPoint, cursorPoint Point
	if forward {
		anchorPoint, ok = anchorRun.layout.PositionForIndex(anchor.rng.Start)
		if !ok {
			return
		}
		cursorPoint, ok = hitSegment.run.layout.PositionForIndex(hitSegment.rangeAt.End)
		if !ok {
			return
		}
	} else {
		anchorPoint, ok = hitSegment.run.layout.PositionForIndex(hitSegment.rangeAt.Start)
		if !ok {
			return
		}
		cursorPoint, ok = anchorRun.layout.PositionForIndex(anchor.rng.End)
		if !ok {
			return
		}
	}
	resolver := hit.resolver
	s.anchor = endpointAt(anchorParticipant, registration.registration, resolver, anchorPoint)
	s.cursor = endpointAt(anchorParticipant, registration.registration, resolver, cursorPoint)
	s.didHitText = true
}

// endpoint hit-tests the participants at position: the smallest hovered
// one (ties by document order), else the last one starting above it,
// else the first (U:text_selection.rs:1892-2002). Without a window,
// participant bounds stand in for the hover test.
func (s *windowSelectionState) endpoint(position Point, w *Window, app *App) *selectionEndpoint {
	s.pruneDeadParticipants()
	type candidate struct {
		participant  WeakEntity[selectableTextState]
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
	point := pointSub(pointSub(position, selection.registration.bounds.Origin), selection.registration.scrollOffset)
	resolver, _ := selection.participant.TryRead(app, func(p *selectableTextState, _ *App) TextSelectionContentKeyResolver {
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
		participant:   Some(selection.participant),
		point:         point,
		inside:        inside,
		insideText:    insideText,
		resolver:      resolver,
		resolverPoint: point,
	}
}

// publishSnapshots hands every participant its share of the selection
// (U:text_selection.rs:2004-2023).
func (s *windowSelectionState) publishSnapshots(app *App) {
	s.pruneDeadParticipants()
	snapshot := s.snapshot()
	single, hasSingle := s.singleParticipant()
	for id, registration := range s.participants {
		if !registration.participant.IsUpgradable() {
			continue
		}
		var participantSnapshot Optional[TextSelectionSnapshot]
		if registration.registration.scope == s.activeScope && s.participates(id, registration) && (!hasSingle || single == id) {
			if snap, ok := snapshot.Get(); ok {
				snap.coverage = s.coverageFor(id)
				participantSnapshot = Some(snap)
			}
		}
		_ = registration.participant.TryUpdate(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
			p.setSnapshot(participantSnapshot, cx)
		})
	}
}

// idle reports whether no selection exists and no gesture is running
// (browser-core 20's cost guard): with plain text registering by
// default, a frame with many texts registers many participants, and
// the per-registration publish/prune would walk them all — O(n²) per
// idle frame. While idle the publishes are no-ops anyway (every
// participant's snapshot is already none), so registration and the
// frame sweep skip them; the first gesture mutation un-idles the state
// and every mutation path publishes as before.
func (s *windowSelectionState) idle() bool {
	return s.anchor == nil && s.cursor == nil && !s.touch.active
}

// coverageFor is U:text_selection.rs:2025-2044.
func (s *windowSelectionState) coverageFor(id EntityID) TextSelectionCoverage {
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
func (s *windowSelectionState) singleParticipant() (EntityID, bool) {
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
func (s *windowSelectionState) participates(id EntityID, registration participantRegistration) bool {
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
func (s *windowSelectionState) updateAutoScroll(position Point, w *Window, cx *Context[windowSelectionState]) {
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
	eventPosition := Point{
		X: min(max(position.X, visible.Left()+textSelectionHitTestInset), visible.Right()-textSelectionHitTestInset),
		Y: min(max(position.Y, visible.Top()+textSelectionHitTestInset), visible.Bottom()-textSelectionHitTestInset),
	}
	s.autoScroll.LastDragPosition = Some(eventPosition)
	s.autoScrollStall = Optional[autoScrollStallGeometry]{}
	s.autoScrollStalled = 0
	windowID := w.ID()
	s.autoScroll.Set(delta, cx, func(delta float32, state *windowSelectionState, cx *Context[windowSelectionState]) {
		position, ok := state.autoScroll.LastDragPosition.Get()
		if !ok {
			return
		}
		// Hold off once the container has shown, over a few ticks, that
		// it has nowhere left to go; the next move of the finger asks
		// again.
		var geometry Optional[autoScrollStallGeometry]
		if _, registration, ok := state.anchorRegistration(); ok {
			geometry = Some(autoScrollStallGeometry{registration.bounds, registration.scrollOffset})
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
		cx.App().Defer(func(app *App) {
			if w, ok := app.Window(windowID); ok {
				w.DispatchInput(&ScrollWheelEvent{
					Position:   position,
					Delta:      ScrollDeltaPixels(0, -delta),
					TouchPhase: TouchPhaseMoved,
				}, app)
			}
		})
	})
}

// updateParticipantAutoScroll drives the anchor participant's own
// scrolling, measured against the visible part of its bounds
// (U:text_selection.rs:2165-2180).
func (s *windowSelectionState) updateParticipantAutoScroll(position Point, app *App) {
	participant, registration, ok := s.anchorRegistration()
	if !ok {
		return
	}
	visible := intersectBounds(registration.bounds, registration.maskBounds())
	var delta Optional[float32]
	if visible.Size.Width > 0 && visible.Size.Height > 0 {
		delta = AutoScrollComputeDelta(position.Y, visible)
	}
	_ = participant.TryUpdate(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
		p.setAutoScroll(delta, cx)
	})
}

// stopAnchorAutoScroll is U:text_selection.rs:2182-2188.
func (s *windowSelectionState) stopAnchorAutoScroll(app *App) {
	s.autoScroll.Stop()
	participant, ok := s.anchorParticipant()
	if !ok {
		return
	}
	_ = participant.TryUpdate(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
		p.setAutoScroll(Optional[float32]{}, cx)
	})
}

// anchorParticipant is the live participant owning the anchor of the
// current gesture (U:text_selection.rs:2190-2198).
func (s *windowSelectionState) anchorParticipant() (WeakEntity[selectableTextState], bool) {
	if s.anchor == nil || !s.anchor.inside {
		return WeakEntity[selectableTextState]{}, false
	}
	participant, ok := s.anchor.participant.Get()
	if !ok || !participant.IsUpgradable() {
		return WeakEntity[selectableTextState]{}, false
	}
	return participant, true
}

// anchorRegistration is the anchor participant with its current frame
// registration (U:text_selection.rs:2200-2207).
func (s *windowSelectionState) anchorRegistration() (WeakEntity[selectableTextState], *TextSelectionRegistration, bool) {
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
	stacks map[WindowID][]TextSelectionScopeID
}

// pushTextSelectionScope is U:text_selection.rs:2232-2241.
func pushTextSelectionScope(windowID WindowID, scope TextSelectionScopeID, app *App) {
	stacks, ok := TryGlobal[*textSelectionScopeStacks](app)
	if !ok {
		stacks = &textSelectionScopeStacks{stacks: map[WindowID][]TextSelectionScopeID{}}
		app.SetGlobal(stacks)
	}
	stacks.stacks[windowID] = append(stacks.stacks[windowID], scope)
}

// popTextSelectionScope is U:text_selection.rs:2243-2252.
func popTextSelectionScope(windowID WindowID, app *App) {
	stacks, ok := TryGlobal[*textSelectionScopeStacks](app)
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
func currentTextSelectionScope(windowID WindowID, app *App) (TextSelectionScopeID, bool) {
	stacks, ok := TryGlobal[*textSelectionScopeStacks](app)
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
func withTextSelectionScope[T any](windowID WindowID, scope TextSelectionScopeID, app *App, f func(*App) T) T {
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
func TextSelectionSelectedText(w *Window, app *App) string {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return ""
	}
	items, err := state.TryRead(app, func(s *windowSelectionState, app *App) []textSelectionCopyItem {
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
func TextSelectionHasSelection(w *Window, app *App) bool {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return false
	}
	has, err := state.TryRead(app, func(s *windowSelectionState, app *App) bool { return s.hasSelection(app) })
	return err == nil && has
}

// TextSelectionClear clears window selection and every participant's
// local selection (TextSelection::clear, U:text_selection.rs:2302-2308).
func TextSelectionClear(w *Window, app *App) {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return
	}
	handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) []textSelectionClearHandler {
		return s.clearState(cx.App())
	})
	if err == nil {
		dispatchTextSelectionClearHandlers(handlers, app)
	}
}

// TextSelectionStateExists reports whether the window runs a selection
// layer's state — the Root-mounted TextSelectionLayer — (test support
// for the model's Base-side tests, browser-core 19).
func TextSelectionStateExists(w *Window, app *App) bool {
	_, ok := textSelectionStateExisting(w, app)
	return ok
}

type participantScopeQuery struct {
	scope TextSelectionScopeID
	ok    bool
}

// TextSelectionParticipantScope is the selection scope a participant
// last registered under (test support for the scope tests).
func TextSelectionParticipantScope(w *Window, app *App, participant EntityID) (TextSelectionScopeID, bool) {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return 0, false
	}
	query, err := state.TryRead(app, func(s *windowSelectionState, _ *App) participantScopeQuery {
		registration, ok := s.participants[participant]
		if !ok {
			return participantScopeQuery{}
		}
		return participantScopeQuery{scope: registration.registration.scope, ok: true}
	})
	if err != nil || !query.ok {
		return 0, false
	}
	return query.scope, true
}

// TextSelectionClearForWindow clears selection for a known window
// identifier; prefer TextSelectionClear with a window
// (TextSelection::clear_for_window, U:text_selection.rs:2310-2316).
func TextSelectionClearForWindow(windowID WindowID, app *App) {
	clearWindowTextSelection(windowID, app)
}

// TextSelectionEnd ends the current drag while leaving its selection
// visible (TextSelection::end, U:text_selection.rs:2318-2323).
func TextSelectionEnd(w *Window, app *App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) { s.end(cx.App()) })
	}
}

// textSelectionTouchObserver is the observer entity behind
// TextSelectionObserveTouchSelection (see the file comment).
type textSelectionTouchObserver struct{}

// TextSelectionObserveTouchSelection calls callback whenever the touch
// selection changes: it appears, its handles move, its menu opens or
// closes, or it goes away (TextSelection::observe_touch_selection,
// U:text_selection.rs:2325-2335).
func TextSelectionObserveTouchSelection(w *Window, app *App, callback func(app *App)) Subscription {
	weak := textSelectionStateEnsure(w, app)
	borrow := w.Scope().Child()
	defer borrow.Close()
	state, _ := weak.UpgradeInto(borrow)
	observer := NewEntity(app, w.Scope(), func(*textSelectionTouchObserver, *Context[textSelectionTouchObserver]) {})
	return observer.UpdateWith(app, func(_ *textSelectionTouchObserver, cx *Context[textSelectionTouchObserver]) Subscription {
		return cx.Observe(state, func(_ *textSelectionTouchObserver, _ Entity[windowSelectionState], cx *Context[textSelectionTouchObserver]) {
			callback(cx.App())
		})
	})
}

// TextSelectionTouchSelection is the selection a long press made, laid out
// for its handles and edit menu; absent for a pointer selection
// (TextSelection::touch_selection, U:text_selection.rs:2337-2343).
func TextSelectionTouchSelection(w *Window, app *App) Optional[TouchSelectionSnapshot] {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		return Optional[TouchSelectionSnapshot]{}
	}
	snapshot, _ := state.TryRead(app, func(s *windowSelectionState, app *App) Optional[TouchSelectionSnapshot] {
		return s.touchSelection(app)
	})
	return snapshot
}

// TextSelectionRegisterTouchUI records where a touch handle or the edit
// menu is painted this frame, so pressing it does not clear its selection.
// Call from paint every frame the surface is shown
// (TextSelection::register_touch_ui, U:text_selection.rs:2345-2352).
func TextSelectionRegisterTouchUI(bounds Bounds, w *Window, app *App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, _ *Context[windowSelectionState]) { s.registerTouchUI(bounds) })
	}
}

// TextSelectionCloseEditMenu closes the edit menu and keeps the selection
// with its handles (TextSelection::close_edit_menu,
// U:text_selection.rs:2354-2359).
func TextSelectionCloseEditMenu(w *Window, app *App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) { s.closeEditMenu(cx.App()) })
	}
}

// TextSelectionSelectAll selects all of the text the touch selection
// started in (TextSelection::select_all, U:text_selection.rs:2361-2367).
func TextSelectionSelectAll(w *Window, app *App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) { s.selectAllTouched(cx.App()) })
	}
}

// TextSelectionBeginEdgeDrag starts dragging one end of the touch
// selection from finger (TextSelection::begin_edge_drag,
// U:text_selection.rs:2369-2383).
func TextSelectionBeginEdgeDrag(edge SelectionEdge, finger Point, w *Window, app *App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			s.beginEdgeDrag(edge, finger, w, cx.App())
		})
		textSelectionResolveContentKeys(state, app)
	}
}

// TextSelectionUpdateEdgeDrag moves the dragged end to the text under
// finger (TextSelection::update_edge_drag, U:text_selection.rs:2385-2391).
func TextSelectionUpdateEdgeDrag(finger Point, w *Window, app *App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			s.updateEdgeDrag(finger, w, cx)
		})
		textSelectionResolveContentKeys(state, app)
	}
}

// TextSelectionEndEdgeDrag ends the handle drag and reopens the edit menu
// (TextSelection::end_edge_drag, U:text_selection.rs:2393-2398).
func TextSelectionEndEdgeDrag(w *Window, app *App) {
	if state, ok := textSelectionStateExisting(w, app); ok {
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) { s.endEdgeDrag(cx.App()) })
	}
}

// TextSelectionActivateScope activates the opaque selection scope for the
// window (TextSelection::activate_scope, U:text_selection.rs:2400-2413).
func TextSelectionActivateScope(scope TextSelectionScopeID, w *Window, app *App) {
	state, ok := textSelectionStateExisting(w, app)
	if !ok {
		pending, ok := TryGlobal[*pendingTextSelectionScopes](app)
		if !ok {
			pending = &pendingTextSelectionScopes{scopes: map[WindowID]TextSelectionScopeID{}}
			app.SetGlobal(pending)
		}
		pending.scopes[w.ID()] = scope
		return
	}
	handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) []textSelectionClearHandler {
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
	element AnyElement
}

// TextSelectionScope places element in scope (text_selection_scope,
// U:text_selection.rs:2422-2430; ElementExt::text_selection_scope) —
// exported for Base's ElementExt when the model moved into the core
// (browser-core 19).
func TextSelectionScope(scope TextSelectionScopeID, element AnyElement) AnyElement {
	return textSelectionScope(scope, element)
}

// textSelectionScope places element in scope (text_selection_scope,
// U:text_selection.rs:2422-2430; ElementExt::text_selection_scope).
func textSelectionScope(scope TextSelectionScopeID, element AnyElement) AnyElement {
	return CustomElement[struct{}, struct{}](&textSelectionScopeMarker{scope: scope, element: element})
}

// ID implements Element.
func (m *textSelectionScopeMarker) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (m *textSelectionScopeMarker) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (U:text_selection.rs:2457-2468).
func (m *textSelectionScopeMarker) RequestLayout(_ *GlobalElementID, _ *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	id := withTextSelectionScope(w.ID(), m.scope, app, func(app *App) LayoutID {
		return m.element.RequestLayout(w, app)
	})
	return id, struct{}{}
}

// Prepaint implements Element (U:text_selection.rs:2470-2484).
func (m *textSelectionScopeMarker) Prepaint(_ *GlobalElementID, _ *InspectorElementID, _ Bounds, _ *struct{}, w *Window, app *App) struct{} {
	return withTextSelectionScope(w.ID(), m.scope, app, func(app *App) struct{} {
		m.element.Prepaint(w, app)
		return struct{}{}
	})
}

// Paint implements Element (U:text_selection.rs:2486-2508).
func (m *textSelectionScopeMarker) Paint(_ *GlobalElementID, _ *InspectorElementID, _ Bounds, _ *struct{}, _ *struct{}, w *Window, app *App) {
	withTextSelectionScope(w.ID(), m.scope, app, func(app *App) struct{} {
		m.element.Paint(w, app)
		return struct{}{}
	})
}

// TextSelectionLayer is a zero-sized root layer that enabled text
// selection for a window (U:text_selection.rs:2416-2420). Browser-core
// ticket 20 (spec D7) made it redundant: installWindowTextSelection
// (user_select.go) gives every window the selection state and gestures
// at the head of its frame draw, so text selects with no opt-in. The
// element remains in the API for compatibility — it still reserves
// its (empty) layout node so existing trees lay out identically — but
// its phases no longer install anything: the layer's own install
// would duplicate the window's listeners.
type TextSelectionLayer struct{}

// IntoElement implements IntoElement.
func (l TextSelectionLayer) IntoElement() AnyElement {
	return CustomElement[struct{}, WeakEntity[windowSelectionState]](&l)
}

// ID implements Element (U:text_selection.rs:2526-2528).
func (l *TextSelectionLayer) ID() (ElementID, bool) {
	return NameElementID("window-text-selection"), true
}

// SourceLocation implements Element.
func (l *TextSelectionLayer) SourceLocation() *SourceLocation { return nil }

// RequestLayout requests an empty node (U:text_selection.rs:2534-2542)
// at the pin's default display (Flex; browser-core 71, spec D6 — a
// childless node's display cannot change its layout, but the node
// stays exactly at the pin's value).
func (l *TextSelectionLayer) RequestLayout(_ *GlobalElementID, _ *InspectorElementID, w *Window, _ *App) (LayoutID, struct{}) {
	style := DefaultStyle()
	style.Display = DisplayFlex
	id, err := RequestElementLayout(w, style)
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint is inert as an installer (browser-core 20): the window
// installs the selection gestures itself at the head of its frame draw
// (installWindowTextSelection). The layer still ensures the state
// exists — a tree that mounts it expects the window to carry selection
// state the way it did before the automatic install (the state is
// created lazily otherwise, by the first participant).
func (l *TextSelectionLayer) Prepaint(_ *GlobalElementID, _ *InspectorElementID, _ Bounds, _ *struct{}, w *Window, app *App) WeakEntity[windowSelectionState] {
	return ensureUserSelectState(w, app)
}

// Paint is inert (browser-core 20): the window installs the selection
// gestures itself (installWindowTextSelection), before any element
// registers its own listeners — the layer's own install would
// duplicate them.
func (l *TextSelectionLayer) Paint(_ *GlobalElementID, _ *InspectorElementID, _ Bounds, _ *struct{}, state *WeakEntity[windowSelectionState], w *Window, app *App) {
	_ = state
}

// textSelectionSweepPlan is paintTextSelection's scheduling answer:
// whether the window has anything to sweep this frame and whether the
// sweep's deferred effect was newly scheduled (browser-core 20).
type textSelectionSweepPlan struct {
	needs     bool
	scheduled bool
}

// textSelectionPressPrepare is the mousedown capture arm's answer: the
// clear handlers the prepare collected and whether a selection existed
// to clear (browser-core 20's gated refresh).
type textSelectionPressPrepare struct {
	handlers []textSelectionClearHandler
	had      bool
}

// paintTextSelection schedules the post-frame sweep and installs the
// window's mouse listeners (U:text_selection.rs:2602-2767). Browser-
// core 20: the sweep's deferred effect runs only when the window has
// something to sweep — participants or a live gesture; a window with
// no selectable text queues no per-frame effect, so its flushes draw
// no extra frames (the automatic install otherwise hits every
// window).
func paintTextSelection(state WeakEntity[windowSelectionState], w *Window, app *App) {
	if state.app != nil {
		paintTextSelectionSweep(state, w, app)
	}
	paintTextSelectionListeners(w, app)
}

// paintTextSelectionSweep schedules the post-frame sweep (the head of
// paintTextSelection, skipped for a window whose state does not exist
// yet — browser-core 20).
func paintTextSelectionSweep(state WeakEntity[windowSelectionState], w *Window, app *App) {
	sweep, _ := state.TryUpdateWith(app, func(s *windowSelectionState, _ *Context[windowSelectionState]) textSelectionSweepPlan {
		if needs := len(s.participants) > 0 || !s.idle(); !needs {
			return textSelectionSweepPlan{}
		}
		return textSelectionSweepPlan{needs: true, scheduled: s.scheduleFinishFrame()}
	})
	if sweep.needs && sweep.scheduled {
		w.Defer(app, func(w *Window, app *App) {
			handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) []textSelectionClearHandler {
				return s.finishFrame(cx.App())
			})
			if err != nil {
				return
			}
			dispatchTextSelectionClearHandlers(handlers, app)
			// Direct participant scrolling produces no wheel event. Refresh
			// the held cursor after paint registers the new scroll geometry.
			refresh, _ := state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) bool {
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
}

// paintTextSelectionListeners installs the window's selection mouse
// listeners (U:text_selection.rs:2602-2767's listener half): each
// resolves the window's state per event, so a window whose state does
// not exist yet (no selectable text has painted) stays inert and the
// listeners activate the moment the first text creates it.
func paintTextSelectionListeners(w *Window, app *App) {
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		ev, ok := event.(*MouseDownEvent)
		if !ok || ev.Button != MouseButtonLeft {
			return
		}
		// Browser-core 20: the state is resolved per event — a window
		// whose selectable text has not created it yet (the lazy
		// acquisition, user_select.go) stays inert, and the listeners
		// activate the moment it exists.
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			return
		}
		// A press on a handle or on the edit menu acts on the selection; it
		// must not clear it.
		covers, _ := state.TryRead(app, func(s *windowSelectionState, _ *App) bool { return s.touch.covers(ev.Position) })
		if covers {
			return
		}
		switch {
		case phase == DispatchCapture:

			ResetTextSelectionSuppression(app)
			prepared, _ := state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) textSelectionPressPrepare {
				if s.mouseDownPrepared {
					return textSelectionPressPrepare{}
				}
				s.mouseDownPrepared = true
				return textSelectionPressPrepare{
					handlers: s.prepareForMouseDown(ev.ClickCount == 1 && ev.Modifiers.Shift, cx.App()),
					had:      !s.idle(),
				}
			})
			handlers, hadSelection := prepared.handlers, prepared.had
			dispatchTextSelectionClearHandlers(handlers, app)
			// A press that cleared a selection repaints it away (browser-
			// core 20: the auto path has no subscriber to refresh the
			// window — Base's TextView refreshes through
			// RefreshWindowOnChange — so the gesture arms refresh
			// themselves; Chrome repaints on selection change). A press
			// over nothing selectable clears nothing and stays quiet.
			if hadSelection || len(handlers) > 0 {
				w.requestRefresh()
			}
		case ev.ClickCount == 1:
			if IsTextSelectionSuppressed(app) {
				_ = state.TryUpdate(app, func(s *windowSelectionState, _ *Context[windowSelectionState]) { s.pendingExtensionAnchor = nil })
				return
			}
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
				if !s.isSelecting {
					s.beginInWindow(ev.Position, ev.Modifiers.Shift, w, cx.App())
				}
			})
			textSelectionResolveContentKeys(state, app)
			w.requestRefresh()
		case ev.ClickCount >= 2:
			if IsTextSelectionSuppressed(app) {
				return
			}
			touch := IsTouchPress(app)
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
				s.selectAt(ev.Position, ev.ClickCount, w, cx.App())
				// A double tap is touch's other way to select a word, and it
				// gets the handles and the menu like a long press does.
				if touch {
					s.keepTouchSelection(cx.App())
				}
			})
			textSelectionResolveContentKeys(state, app)
			w.requestRefresh()
		}
	})

	// A handle drag in progress follows the finger or the pointer wherever
	// it goes, whether or not the handle it took is laid out this frame.
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		ev, ok := event.(*MouseMoveEvent)
		if !ok || phase != DispatchBubble || ev.PressedButton == nil || *ev.PressedButton != MouseButtonLeft {
			return
		}
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			return
		}
		dragging, err := state.TryRead(app, func(s *windowSelectionState, _ *App) bool { return s.touch.drag.IsSome() })
		if err != nil || !dragging {
			return
		}
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			s.updateEdgeDrag(ev.Position, w, cx)
		})
		textSelectionResolveContentKeys(state, app)
	})
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		ev, ok := event.(*MouseUpEvent)
		if !ok || phase != DispatchBubble || ev.Button != MouseButtonLeft {
			return
		}
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			return
		}
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) { s.endEdgeDrag(cx.App()) })
	})

	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		ev, ok := event.(*MouseMoveEvent)
		if !ok || phase != DispatchBubble {
			return
		}
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			return
		}
		selecting, _ := state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) bool {
			// A handle drag maps the pointer through the handle's offset;
			// the raw pointer must not fight it.
			if s.touch.drag.IsSome() {
				return true
			}
			s.updateInWindow(ev.Position, w, cx)
			return s.isSelecting
		})
		textSelectionResolveContentKeys(state, app)
		// A drag in progress repaints its moving wash (browser-core 20,
		// the auto path's own refresh; Chrome repaints the selection as
		// it extends).
		if selecting {
			w.requestRefresh()
		}
	})

	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		if _, ok := event.(*MouseUpEvent); !ok || phase != DispatchBubble {
			return
		}
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			return
		}
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			s.mouseDownPrepared = false
			s.end(cx.App())
		})
	})

	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		ev, ok := event.(*ScrollWheelEvent)
		if !ok {
			return
		}
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			return
		}
		// On capture, before a scroll container can stop the event: one
		// stretched past its end swallows the whole stream, the lift
		// included, and the menu would never come back.
		if phase == DispatchCapture {
			_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
				s.editMenuOnScroll(ev.TouchPhase, cx.App())
			})
			return
		}
		position := w.TrackedMousePosition()
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
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
func clearWindowTextSelection(windowID WindowID, app *App) {
	registry, ok := TryGlobal[*selectionStateRegistry](app)
	if !ok {
		return
	}
	state, ok := registry.states[windowID]
	if !ok || !state.IsUpgradable() {
		return
	}
	handlers, err := state.TryUpdateWith(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) []textSelectionClearHandler {
		return s.clearState(cx.App())
	})
	if err == nil {
		dispatchTextSelectionClearHandlers(handlers, app)
	}
}
