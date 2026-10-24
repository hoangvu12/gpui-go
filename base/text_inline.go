package base

// Port of U:text/inline.rs (ticket 28): InlineHighlight and its sweeps,
// the text runs of a highlighted text, the retained-layout table and the
// Inline element that renders every piece of TextView text.
//
// Go adaptations (recorded deviations):
//
//   - TextLayout geometry. The pin's TextLayout (gpui/styled_text.go)
//     exposes no line_layouts / wrap_boundaries / glyph runs, so the
//     row walks of text_line_bounds and glyph_boxes are re-expressed over
//     TextLayout.PositionForIndex: rows told apart by the y of their
//     characters, each row's end found by a search over its characters
//     (inlineTextRows, ticket 37: the calls scale with the rows, not the
//     characters), and a row's characters placed by the row's text shaped
//     alone (WindowTextSystem.ShapeLine), which also gives the row's
//     right end.
//   - The text-view state stack lives in this package's textViewStateStack
//     global (text_state.go), not in GlobalState (global_state.go is not
//     this ticket's file).
//   - Window-level drag selection (selection_points, the selection
//     adapter's registrations, touch handles, TextSelection::end) is ticket
//     29's; until it installs the hooks in text_state.go only select-all,
//     multi-click and preserved selections paint. Word ranges for a double
//     click come from U:text/selection.rs::word_range_at, also 29's
//     (textWordRangeAt).
//   - Reveal (U:text/range_highlight.rs RevealAt) is ticket 30's
//     (withReveal/requestReveal in text_range_highlight_state.go); range
//     backgrounds are carried and painted.

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
	"weak"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Font weights of R:text_system.rs FontWeight (BOLD 700, SEMIBOLD 600,
// MEDIUM 500, NORMAL 400); the core exports no constants.
const (
	textFontWeightNormal   float32 = 400
	textFontWeightMedium   float32 = 500
	textFontWeightSemibold float32 = 600
	textFontWeightBold     float32 = 700
)

// inlineHighlight is the style applied to one range of inline text
// (U:text/inline.rs:31-41): a HighlightStyle plus the font family of an
// inline code span and its font size scale.
type inlineHighlight struct {
	style         gpui.HighlightStyle
	fontFamily    *string
	fontSizeScale *float32
}

// inlineHighlightOf is From<HighlightStyle> (U:text/inline.rs:57-65).
func inlineHighlightOf(style gpui.HighlightStyle) inlineHighlight {
	return inlineHighlight{style: style}
}

// highlight layers other over h, as HighlightStyle::highlight does
// (U:text/inline.rs:43-55).
func (h inlineHighlight) highlight(other inlineHighlight) inlineHighlight {
	h.style = h.style.Highlight(other.style)
	if other.fontFamily != nil {
		h.fontFamily = other.fontFamily
	}
	if other.fontSizeScale != nil {
		h.fontSizeScale = other.fontSizeScale
	}
	return h
}

func (h inlineHighlight) equal(o inlineHighlight) bool {
	return h.style.Equal(o.style) && optStringEqual(h.fontFamily, o.fontFamily) && optEqual(h.fontSizeScale, o.fontSizeScale)
}

// inlineHighlightRange is one (Range<usize>, InlineHighlight) entry.
type inlineHighlightRange struct {
	rng gpui.TextRange
	h   inlineHighlight
}

func inlineHighlightsEqual(a, b []inlineHighlightRange) bool {
	return slices.EqualFunc(a, b, func(x, y inlineHighlightRange) bool { return x.rng == y.rng && x.h.equal(y.h) })
}

// textLinkRange is one (Range<usize>, LinkMark) entry.
type textLinkRange struct {
	rng  gpui.TextRange
	link LinkMark
}

func textLinksEqual(a, b []textLinkRange) bool {
	return slices.EqualFunc(a, b, func(x, y textLinkRange) bool { return x.rng == y.rng && x.link.equal(&y.link) })
}

// textColorRange is one (Range<usize>, Hsla) range background.
type textColorRange struct {
	rng   gpui.TextRange
	color gpui.Hsla
}

// textFadeRange is one (Range<usize>, f32) streamed fade (ticket 30
// produces them).
type textFadeRange struct {
	rng     gpui.TextRange
	fadeOut float32
}

// combineInlineHighlights merges two highlight lists over one text into
// non-overlapping ranges, cutting at every endpoint of every input range
// (U:text/inline.rs:67-104).
func combineInlineHighlights(a, b []inlineHighlightRange) []inlineHighlightRange {
	type endpoint struct {
		position int
		id       int
		isStart  bool
	}
	var endpoints []endpoint
	var highlights []inlineHighlight
	for _, r := range slices.Concat(a, b) {
		if r.rng.Start < r.rng.End {
			id := len(highlights)
			endpoints = append(endpoints, endpoint{r.rng.Start, id, true}, endpoint{r.rng.End, id, false})
			highlights = append(highlights, r.h)
		}
	}
	// sort_unstable_by_key on the position; ties keep an order that does
	// not matter for the output (an empty range is never emitted).
	sort.SliceStable(endpoints, func(i, j int) bool { return endpoints[i].position < endpoints[j].position })

	var combined []inlineHighlightRange
	var active []int
	ix := 0
	for _, e := range endpoints {
		if e.position > ix && len(active) > 0 {
			var style inlineHighlight
			for _, id := range active {
				style = style.highlight(highlights[id])
			}
			combined = append(combined, inlineHighlightRange{gpui.TextRange{Start: ix, End: e.position}, style})
		}
		ix = e.position
		if e.isStart {
			active = append(active, e.id)
		} else {
			active = slices.DeleteFunc(active, func(id int) bool { return id == e.id })
		}
	}
	return combined
}

// fadeHighlights layers a streamed fade-in over highlights: text inside a
// fade range loses that share of its color, and a highlight background
// fades with it (U:text/inline.rs:106-134).
func fadeHighlights(highlights []inlineHighlightRange, fades []textFadeRange) []inlineHighlightRange {
	if len(fades) == 0 {
		return highlights
	}
	fadeRanges := make([]inlineHighlightRange, len(fades))
	for i, f := range fades {
		fadeOut := f.fadeOut
		fadeRanges[i] = inlineHighlightRange{f.rng, inlineHighlightOf(gpui.HighlightStyle{FadeOut: &fadeOut})}
	}
	combined := combineInlineHighlights(highlights, fadeRanges)
	for i := range combined {
		h := &combined[i].h
		if h.style.FadeOut != nil && h.style.BackgroundColor != nil {
			bg := *h.style.BackgroundColor
			bg.FadeOut(*h.style.FadeOut)
			h.style.BackgroundColor = &bg
		}
	}
	return combined
}

// textRuns builds the runs for textLen bytes of inline text: each
// highlight refines defaultStyle over its range, and a highlight that
// names a font family shapes its run in that family
// (U:text/inline.rs:136-164).
func textRuns(textLen int, defaultStyle gpui.TextStyle, highlights []inlineHighlightRange) []gpui.TextRun {
	runs := make([]gpui.TextRun, 0, len(highlights)*2+1)
	ix := 0
	for _, r := range highlights {
		if ix < r.rng.Start {
			runs = append(runs, defaultStyle.ToRun(r.rng.Start-ix))
		}
		run := defaultStyle.Highlight(r.h.style).ToRun(r.rng.End - r.rng.Start)
		if r.h.fontFamily != nil {
			run.Font.Family = *r.h.fontFamily
		}
		runs = append(runs, run)
		ix = r.rng.End
	}
	if ix < textLen {
		runs = append(runs, defaultStyle.ToRun(textLen-ix))
	}
	return runs
}

// textSizeRange is one (Range<usize>, f32) range sharing a font size.
type textSizeRange struct {
	rng   gpui.TextRange
	scale float32
}

// textSizeRanges splits text into contiguous ranges sharing one font size;
// runs can vary the font but not its size, so each range is its own shaped
// line (U:text/inline.rs:166-194).
func textSizeRanges(textLen int, highlights []inlineHighlightRange) []textSizeRange {
	var ranges []textSizeRange
	push := func(r gpui.TextRange, scale float32) {
		if r.Start >= r.End {
			return
		}
		if n := len(ranges); n > 0 && ranges[n-1].scale == scale && ranges[n-1].rng.End == r.Start {
			ranges[n-1].rng.End = r.End
			return
		}
		ranges = append(ranges, textSizeRange{r, scale})
	}
	cursor := 0
	for _, h := range highlights {
		push(gpui.TextRange{Start: cursor, End: h.rng.Start}, 1)
		scale := float32(1)
		if h.h.fontSizeScale != nil {
			scale = *h.h.fontSizeScale
		}
		push(h.rng, scale)
		cursor = h.rng.End
	}
	push(gpui.TextRange{Start: cursor, End: textLen}, 1)
	return ranges
}

// ---------------------------------------------------------------------------
// Retained layouts (U:text/inline.rs:232-298)
// ---------------------------------------------------------------------------

// retainedLayout is one frame's StyledText, kept for the next frame's
// Inline of the same inlineState (U:text/inline.rs:232-256). Go keys the
// table on a weak pointer to the state, which tells a live state from a
// collected one the way Rust's Weak does.
type retainedLayout struct {
	state      weak.Pointer[inlineState]
	styledText *gpui.StyledText
	text       string
	runs       []gpui.TextRun
	textStyle  gpui.TextStyle
}

// retainedSweepAt is U:text/inline.rs:262-264 RETAINED_SWEEP_AT.
const retainedSweepAt = 4096

// retainedLayouts is the RETAINED_LAYOUTS table (U:text/inline.rs:258-260;
// thread-local in Rust, one table guarded by a mutex here).
var retainedLayouts = struct {
	sync.Mutex
	m map[weak.Pointer[inlineState]]*retainedLayout
}{m: map[weak.Pointer[inlineState]]*retainedLayout{}}

// takeRetainedLayout takes the layout the previous frame left for state
// (U:text/inline.rs:270-281).
func takeRetainedLayout(state *inlineState) *retainedLayout {
	retainedLayouts.Lock()
	defer retainedLayouts.Unlock()
	key := weak.Make(state)
	r, ok := retainedLayouts.m[key]
	if !ok {
		return nil
	}
	delete(retainedLayouts.m, key)
	if r.state.Value() != state {
		return nil
	}
	return r
}

// hasRetainedLayout reports whether another element of the same state
// already handed one over this frame (U:text/inline.rs:283-288).
func hasRetainedLayout(state *inlineState) bool {
	retainedLayouts.Lock()
	defer retainedLayouts.Unlock()
	_, ok := retainedLayouts.m[weak.Make(state)]
	return ok
}

// retainLayout keeps a layout, sweeping dead entries once the table is
// large (U:text/inline.rs:290-298).
func retainLayout(state *inlineState, r *retainedLayout) {
	retainedLayouts.Lock()
	defer retainedLayouts.Unlock()
	if len(retainedLayouts.m) >= retainedSweepAt {
		for k, v := range retainedLayouts.m {
			if v.state.Value() == nil {
				delete(retainedLayouts.m, k)
			}
		}
	}
	retainedLayouts.m[weak.Make(state)] = r
}

// ---------------------------------------------------------------------------
// Inline (U:text/inline.rs:196-1169)
// ---------------------------------------------------------------------------

// inlineSelectionSource is the (state, range) a flow fragment reports its
// selection into (U:text/inline.rs:208).
type inlineSelectionSource struct {
	state *inlineState
	rng   gpui.TextRange
}

// inlineText is the Inline element, which renders all TextView text
// (U:text/inline.rs:196-222). The name avoids gpui's Inline display.
type inlineText struct {
	text             string
	links            []textLinkRange
	highlights       []inlineHighlightRange
	styledText       *gpui.StyledText
	textStyle        *gpui.TextStyle
	paintOrigin      *gpui.Point
	selectionBounds  *gpui.Bounds
	selectionSource  *inlineSelectionSource
	rangeBackgrounds []textColorRange
	// reveal is the start of a pending reveal in this text (ticket 30,
	// text_range_highlight_state.go).
	reveal           *revealAt
	linkClickHandler LinkClickHandlerFn
	retainedRuns     []gpui.TextRun
	retainedStyle    *gpui.TextStyle
	fontSize         float32
	handedOver       bool
	state            *inlineState
}

// newInlineText is Inline::new (U:text/inline.rs:352-379).
func newInlineText(state *inlineState, links []textLinkRange, highlights []inlineHighlightRange, linkClickHandler LinkClickHandlerFn) *inlineText {
	text, _ := state.snapshot()
	return &inlineText{
		text:             text,
		links:            links,
		highlights:       highlights,
		styledText:       gpui.NewStyledText(text),
		linkClickHandler: linkClickHandler,
		state:            state,
	}
}

// withTextStyle uses the style resolved by a deferred parent layout
// (U:text/inline.rs:381-385).
func (e *inlineText) withTextStyle(style gpui.TextStyle) *inlineText { e.textStyle = &style; return e }

// withPaintOrigin keeps the flow's baseline (U:text/inline.rs:387-391):
// the hitbox, line span and selection geometry use the flow's unsnapped
// fragment origin over the layout's bounds (U:text/inline.rs:904, 942).
// The text itself is placed by StyledText from its own layout bounds
// (R:elements/text.rs:1002-1018), as upstream.
func (e *inlineText) withPaintOrigin(origin gpui.Point) *inlineText {
	e.paintOrigin = &origin
	return e
}

// withSelectionBounds is Inline::selection_bounds (U:text/inline.rs:393-396).
func (e *inlineText) withSelectionBounds(bounds gpui.Bounds) *inlineText {
	e.selectionBounds = &bounds
	return e
}

// withSelectionSource is Inline::selection_source (U:text/inline.rs:398-405).
func (e *inlineText) withSelectionSource(state *inlineState, rng gpui.TextRange) *inlineText {
	e.selectionSource = &inlineSelectionSource{state: state, rng: rng}
	return e
}

// withRangeBackgrounds paints backgrounds behind the text; they are not
// part of the runs, so changing them does not reshape
// (U:text/inline.rs:407-412).
func (e *inlineText) withRangeBackgrounds(backgrounds []textColorRange) *inlineText {
	e.rangeBackgrounds = backgrounds
	return e
}

// retainStyledText hands the shaped text to the next frame
// (U:text/inline.rs:307-335).
func (e *inlineText) retainStyledText() {
	if e.handedOver || e.retainedStyle == nil || hasRetainedLayout(e.state) {
		return
	}
	retainLayout(e.state, &retainedLayout{
		state:      weak.Make(e.state),
		styledText: e.styledText,
		text:       e.text,
		runs:       e.retainedRuns,
		textStyle:  *e.retainedStyle,
	})
	e.retainedRuns, e.retainedStyle = nil, nil
	e.styledText = gpui.NewStyledText("")
	e.handedOver = true
}

// reclaimStyledText takes the shaped text back for painting; false when it
// is gone (U:text/inline.rs:337-350).
func (e *inlineText) reclaimStyledText() bool {
	if !e.handedOver {
		return true
	}
	r := takeRetainedLayout(e.state)
	if r == nil {
		return false
	}
	e.styledText = r.styledText
	e.retainedRuns, e.retainedStyle = r.runs, &r.textStyle
	e.handedOver = false
	return true
}

// linkForPosition is the link at a window position
// (U:text/inline.rs:469-483).
func inlineLinkForPosition(layout *gpui.TextLayout, links []textLinkRange, position gpui.Point) (LinkMark, bool) {
	offset, ok := layout.IndexForPosition(position)
	if !ok {
		return LinkMark{}, false
	}
	for _, l := range links {
		if offset >= l.rng.Start && offset < l.rng.End {
			return l.link, true
		}
	}
	return LinkMark{}, false
}

// inlineLinkRangeForPosition is the range of the link at a window position
// (U:text/inline.rs:485-496).
func inlineLinkRangeForPosition(layout *gpui.TextLayout, links []textLinkRange, position gpui.Point) (gpui.TextRange, bool) {
	offset, ok := layout.IndexForPosition(position)
	if !ok {
		return gpui.TextRange{}, false
	}
	for _, l := range links {
		if offset >= l.rng.Start && offset < l.rng.End {
			return l.rng, true
		}
	}
	return gpui.TextRange{}, false
}

// layoutSelections decides whether the inline is selectable, whether a
// selection is active, and which bytes it covers
// (U:text/inline.rs:511-669).
func (e *inlineText) layoutSelections(layout *gpui.TextLayout, bounds gpui.Bounds, w *gpui.Window, app *gpui.App) (bool, bool, *InputSelection) {
	view, ok := textViewStateCurrent(app)
	if !ok {
		return false, false, nil
	}
	var (
		selectable, allSelected, preserve bool
		multi                             *textViewMultiClickSelection
		start, end                        gpui.Point
		hasPoints                         bool
	)
	view.Read(app, func(s *TextViewState, app *gpui.App) struct{} {
		selectable = s.isSelectable()
		allSelected = s.isAllSelected()
		preserve = s.preserveInlineSelection
		multi = s.multiClickSelection()
		start, end, hasPoints = s.selectionPoints(app)
		return struct{}{}
	})
	if !selectable {
		return false, false, nil
	}
	if allSelected {
		return selectable, true, &InputSelection{Start: 0, End: len(e.text)}
	}
	if preserve {
		var selection *InputSelection
		if src := e.selectionSource; src != nil {
			if _, sel := src.state.snapshot(); sel != nil {
				s, en := max(sel.Start, src.rng.Start), min(sel.End, src.rng.End)
				if s < en {
					selection = &InputSelection{Start: s - src.rng.Start, End: en - src.rng.Start}
				}
			}
		} else {
			_, selection = e.state.snapshot()
		}
		return true, selection != nil, selection
	}
	if multi != nil {
		if multi.kind == textViewMultiClickLine {
			if multi.lineBounds != nil && multi.lineBounds.Contains(bounds.Center()) {
				return true, true, &InputSelection{Start: 0, End: len(e.text)}
			}
			return true, true, nil
		}
		if r, ok := selectionForMultiClick(e.text, layout, bounds, multi.pos, multi.kind); ok {
			return selectable, true, &InputSelection{Start: r.Start, End: r.End}
		}
		return selectable, true, nil
	}
	if !hasPoints {
		return selectable, false, nil
	}
	lineHeight := w.LineHeight()
	if e.text == "" {
		return true, true, nil
	}
	var rowsTop, rowsBottom float32
	if e.selectionBounds != nil {
		rowsTop, rowsBottom = e.selectionBounds.Top(), e.selectionBounds.Bottom()
	} else {
		rowsTop, rowsBottom = inlineRowsExtent(e.text, layout, lineHeight)
	}
	bandTop, bandBottom := min(start.Y, end.Y), max(start.Y, end.Y)
	if rowsBottom <= bandTop || rowsTop > bandBottom {
		return true, true, nil
	}
	if bandTop < rowsTop && bandBottom >= rowsBottom && layout.Len() >= len(e.text) {
		return true, true, &InputSelection{Start: 0, End: len(e.text)}
	}

	var selection *InputSelection
	offset := 0
	for _, c := range e.text {
		next := offset + utf8.RuneLen(c)
		pos, ok := layout.PositionForIndex(offset)
		if !ok {
			offset = next
			continue
		}
		charWidth := lineHeight / 2
		if nextPos, ok := layout.PositionForIndex(next); ok && nextPos.Y == pos.Y {
			charWidth = nextPos.X - pos.X
		}
		selectionPos, selectionHeight := pos, lineHeight
		if e.selectionBounds != nil {
			selectionPos = gpui.Point{X: pos.X, Y: e.selectionBounds.Top()}
			selectionHeight = e.selectionBounds.Size.Height
		}
		if pointInTextSelection(selectionPos, charWidth, start, end, selectionHeight) {
			if selection == nil {
				selection = &InputSelection{Start: offset, End: offset}
			}
			selection.End = next
		}
		offset = next
	}
	return true, true, selection
}

// inlineRowsExtent is the vertical extent of the rows a layout's
// characters are on: the top of the first row and the bottom of the last.
// U:text_selection.rs text_rows_extent (ticket 29's module) answers this
// for layout_selections from the pin's line layouts; this re-expresses it
// over the row walk (inlineTextRows).
func inlineRowsExtent(text string, layout *gpui.TextLayout, lineHeight float32) (float32, float32) {
	rows := inlineTextRows(text, layout)
	if len(rows) == 0 {
		b := layout.Bounds()
		return b.Top(), b.Bottom()
	}
	return rows[0].y, rows[len(rows)-1].y + lineHeight
}

// inlineTextLineBounds is one box per laid-out row, from the row's start
// to its last character, clipped to mask; a row followed by another row is
// extended by half a line height (U:text/inline.rs:671-722). Like
// upstream it walks rows, not characters (inlineTextRows): upstream notes
// the per-character walk cost O(chars x glyphs) for every selectable
// inline on every frame.
func inlineTextLineBounds(text string, layout *gpui.TextLayout, lineHeight float32, mask gpui.Bounds) []gpui.Bounds {
	origin := layout.Bounds().Origin
	type row struct {
		y     float32
		right float32
	}
	var rows []row
	for _, r := range inlineTextRows(text, layout) {
		// The row reaches its last character's start; the text's last row
		// reaches the end of the text when that is on it.
		right := max(r.firstX, r.lastX)
		if r.end == len(text) {
			if p, ok := layout.PositionForIndex(len(text)); ok && p.Y == r.y {
				right = max(right, p.X)
			}
		}
		rows = append(rows, row{y: r.y, right: right})
	}
	if len(rows) == 0 {
		rows = append(rows, row{y: origin.Y, right: origin.X})
	}
	var out []gpui.Bounds
	for i, r := range rows {
		width := r.right - origin.X
		// A trailing newline ends the last row before an empty one, which
		// lays out no character (ticket 30, found by row_walk_matches_the_
		// character_walk: U:text/inline.rs:698-711 walks that line too).
		if i+1 < len(rows) || (i == len(rows)-1 && text != "" && text[len(text)-1] == '\n') {
			width += lineHeight / 2
		}
		b := textBoundsIntersect(gpui.Bounds{Origin: gpui.Point{X: origin.X, Y: r.y}, Size: gpui.Size{Width: width, Height: lineHeight}}, mask)
		if b.Size.Width > 0 && b.Size.Height > 0 {
			out = append(out, b)
		}
	}
	return out
}

// inlineTextRow is one laid-out row of a text: the byte range [start, end)
// of the characters on it, its y, and the x of its first and last
// character (window coordinates).
type inlineTextRow struct {
	start, end    int
	y             float32
	firstX, lastX float32
}

// inlineTextRows is the row walk of U:text/inline.rs:687-721 and
// 1182-1261 over the port's TextLayout, which exposes no line_layouts or
// wrap_boundaries (see the file comment). Rows are told apart by the y of
// their characters' positions, as the character walk did, but each row's
// end is found by a galloping, then binary, search over its characters,
// so the PositionForIndex calls scale with the rows (rows x log of the
// row length), not with the characters. Like the character walk it
// relies on a row's characters being contiguous in the text: a
// character's y never decreases with its offset, as rows are laid out in
// text order.
func inlineTextRows(text string, layout *gpui.TextLayout) []inlineTextRow {
	offsets := make([]int, 0, len(text))
	for offset := range text {
		offsets = append(offsets, offset)
	}
	runeEnd := func(i int) int {
		if i+1 < len(offsets) {
			return offsets[i+1]
		}
		return len(text)
	}
	var rows []inlineTextRow
	for i := 0; i < len(offsets); {
		first, ok := layout.PositionForIndex(offsets[i])
		if !ok {
			i++
			continue
		}
		// lo is the last character known to be on the row; hi the first
		// known not to be.
		lo, lastX, hi := i, first.X, len(offsets)
		onRow := func(k int) bool {
			p, ok := layout.PositionForIndex(offsets[k])
			if ok && p.Y == first.Y {
				lo, lastX = k, p.X
				return true
			}
			return false
		}
		for step := 1; lo+step < len(offsets); step *= 2 {
			if !onRow(lo + step) {
				hi = lo + step
				break
			}
		}
		for hi-lo > 1 {
			if mid := lo + (hi-lo)/2; !onRow(mid) {
				hi = mid
			}
		}
		rows = append(rows, inlineTextRow{start: offsets[i], end: runeEnd(lo), y: first.Y, firstX: first.X, lastX: lastX})
		i = lo + 1
	}
	return rows
}

// paintInlineSelection paints the selection background
// (U:text/inline.rs:747-819).
func paintInlineSelection(selection InputSelection, layout *gpui.TextLayout, bounds gpui.Bounds, w *gpui.Window, color gpui.Hsla) {
	start, end := min(selection.Start, selection.End), max(selection.Start, selection.End)
	startPos, ok := layout.PositionForIndex(start)
	if !ok {
		return
	}
	endPos, ok := layout.PositionForIndex(end)
	if !ok {
		return
	}
	lineHeight := layout.LineHeight()
	fill := func(b gpui.Bounds) { w.PaintQuad(gpui.Fill(b, gpui.SolidBackground(color))) }
	if startPos.Y == endPos.Y {
		fill(textBoundsFromCorners(startPos, gpui.Point{X: endPos.X, Y: endPos.Y + lineHeight}))
		return
	}
	fill(textBoundsFromCorners(startPos, gpui.Point{X: bounds.Right(), Y: startPos.Y + lineHeight}))
	if endPos.Y > startPos.Y+lineHeight {
		fill(textBoundsFromCorners(gpui.Point{X: bounds.Left(), Y: startPos.Y + lineHeight}, gpui.Point{X: bounds.Right(), Y: endPos.Y}))
	}
	fill(textBoundsFromCorners(gpui.Point{X: bounds.Left(), Y: endPos.Y}, gpui.Point{X: endPos.X, Y: endPos.Y + lineHeight}))
}

// paintRangeHighlights paints each range background behind its text
// (U:text/inline.rs:821-841).
func (e *inlineText) paintRangeHighlights(layout *gpui.TextLayout, w *gpui.Window) {
	bounds := layout.Bounds()
	glyphs := glyphBoxes(e.text, e.retainedRuns, e.fontSize, layout, gpui.WindowTextStyle(w).TextAlign, bounds.Size.Width, w)
	lineHeight := layout.LineHeight()
	for _, bg := range e.rangeBackgrounds {
		for _, b := range rangeBoxes(glyphs, bg.rng) {
			w.PaintQuad(gpui.Fill(textBoundsFromCorners(
				gpui.Point{X: bounds.Origin.X + b.left, Y: bounds.Origin.Y + lineHeight*float32(b.row)},
				gpui.Point{X: bounds.Origin.X + b.right, Y: bounds.Origin.Y + lineHeight*float32(b.row+1)},
			), gpui.SolidBackground(bg.color)))
		}
	}
}

// IntoElement implements gpui.IntoElement.
func (e *inlineText) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, gpui.Hitbox](e)
}

// ID implements gpui.Element: no identity (U:text/inline.rs:856-858).
func (e *inlineText) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements gpui.Element.
func (e *inlineText) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout reuses the previous frame's shaped text when the text,
// runs and style are unchanged (U:text/inline.rs:864-893).
func (e *inlineText) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.WindowTextStyle(w)
	if e.textStyle != nil {
		style = *e.textStyle
	}
	// The white-space mode arrives from the enclosing text block's
	// pre-wrap pin (textBlockDiv) — the U: pin's inline text renders
	// verbatim with soft wrap, which is pre-wrap exactly; the flow's
	// pre-wrapped fragments pass their own mode (base/text_inline_flow.
	// go).
	runs := textRuns(len(e.text), style, e.highlights)
	e.fontSize = style.FontSizePixels(gpui.RemSize(w))
	// Deviation (ticket 37): the previous frame's layout is taken but not
	// reused. gpui's TextLayout answers an unchanged measure from its own
	// cached size without asking the window's line cache for its line
	// again (gpui/styled_text.go measure), and that two-frame cache
	// disposes a line no frame asked for (gpui/text_cache.go), so a layout
	// carried over a second frame holds a stale native handle and every
	// position query on it fails. Upstream's retained layout holds its line
	// alive (an Arc). Laying the text out afresh asks the cache each frame,
	// which hands back the same shaped line while the text, runs and style
	// are unchanged, so the shaping is still not repeated.
	takeRetainedLayout(e.state)
	e.styledText = gpui.NewStyledText(e.text).WithRuns(runs)
	e.retainedRuns, e.retainedStyle = runs, &style
	id, _ := e.styledText.RequestLayout(global, inspector, w, app)
	return id, struct{}{}
}

// Prepaint lays the text out, reports its line span to a clamping view and
// inserts the hitbox (U:text/inline.rs:895-930).
func (e *inlineText) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) gpui.Hitbox {
	if e.paintOrigin != nil {
		bounds.Origin = *e.paintOrigin
	}
	e.styledText.Prepaint(global, inspector, bounds, &struct{}{}, w, app)
	textViewReportLineSpan(app, textLineSpan{top: bounds.Top(), bottom: bounds.Bottom(), lineHeight: w.LineHeight()})
	e.requestReveal(w)
	hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
	e.retainStyledText()
	return hitbox
}

// Paint paints the range backgrounds, the text, the selection, and
// registers the link and multi-click listeners (U:text/inline.rs:932-1168).
func (e *inlineText) Paint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, hitbox *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
	if e.paintOrigin != nil {
		bounds.Origin = *e.paintOrigin
	}
	viewID := currentView(w)
	if !e.reclaimStyledText() {
		return
	}
	layout := e.styledText.Layout()
	if len(e.rangeBackgrounds) > 0 {
		e.paintRangeHighlights(layout, w)
	}
	e.styledText.Paint(global, nil, bounds, &struct{}{}, &struct{}{}, w, app)

	isSelectable, isSelection, selection := e.layoutSelections(layout, bounds, w, app)
	e.state.setSelection(selection)
	if src := e.selectionSource; src != nil && selection != nil {
		s, en := src.rng.Start+selection.Start, src.rng.Start+selection.End
		_, prev := src.state.snapshot()
		if prev != nil {
			src.state.setSelection(&InputSelection{Start: min(prev.Start, s), End: max(prev.End, en)})
		} else {
			src.state.setSelection(&InputSelection{Start: s, End: en})
		}
	}

	if isSelection || isSelectable {
		w.SetCursorStyle(gpui.CursorIBeam, hitbox)
	}
	hoveredLink, hovered := inlineLinkRangeForPosition(layout, e.links, w.TrackedMousePosition())
	if hovered {
		w.SetCursorStyle(gpui.CursorPointingHand, hitbox)
	}

	view, hasView := textViewStateCurrent(app)
	if selection != nil {
		color := ThemeGlobal(app).Tokens.Colors.Selection
		if hasView {
			color = view.Read(app, func(s *TextViewState, _ *gpui.App) gpui.Hsla { return s.textViewStyle.Selection() })
		}
		paintInlineSelection(*selection, layout, bounds, w, color)
		if hasView {
			// The touch handles' ends (U:text/inline.rs:996-1001, ticket 29).
			textViewRegisterSelectionEdges(view, *selection, layout, app)
		}
	}

	if isSelectable {
		// Browser-core 20: a user-select: none ancestor opts the whole
		// rich-text document out — the inline registration joins plain
		// text's opt-out (the walk resolves auto from the ancestors,
		// Chromium's Node::CanStartSelection, node.cc:2017-2041).
		if !gpui.UserSelectAllowsSelection(w) {
			isSelectable = false
		}
	}
	if isSelectable {
		if hasView && textSelectionRegisterInline != nil {
			textSelectionRegisterInline(view, inlineTextLineBounds(e.text, layout, layout.LineHeight(), gpui.ContentMask(w)), e.text, layout, hitbox.Bounds, app)
		}
		box := *hitbox
		inline := e.state
		text := e.text
		lineBounds := e.selectionBounds
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.MouseDownEvent)
			if !ok || phase != gpui.DispatchBubble || !box.IsHovered(w) || ev.Button != gpui.MouseButtonLeft {
				return
			}
			if ev.ClickCount == 3 && lineBounds != nil {
				GlobalStateSuppressTextSelection(app)
				if hasView {
					view.Update(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
						s.setMultiClickLine(*lineBounds, cx.App())
					})
				}
				app.Notify(viewID)
				return
			}
			if ev.ClickCount == 2 && GlobalStateIsTouchPress(app) {
				GlobalStateSuppressTextSelection(app)
				return
			}
			var kind textViewMultiClickKind
			switch ev.ClickCount {
			case 2:
				kind = textViewMultiClickWord
			case 3:
				kind = textViewMultiClickParagraph
			default:
				return
			}
			r, ok := selectionForMultiClick(text, layout, box.Bounds, ev.Position, kind)
			if !ok {
				return
			}
			selected := text[r.Start:r.End]
			GlobalStateSuppressTextSelection(app)
			inline.setSelection(&InputSelection{Start: r.Start, End: r.End})
			if hasView {
				view.Update(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
					s.setMultiClickSelection(ev.Position, kind, selected, cx.App())
				})
			}
			app.Notify(viewID)
		})
	}

	if len(e.links) > 0 {
		box := *hitbox
		links := e.links
		current, currentOK := hoveredLink, hovered
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.MouseMoveEvent)
			if !ok || phase != gpui.DispatchBubble || !box.IsHovered(w) {
				return
			}
			updated, updatedOK := inlineLinkRangeForPosition(layout, links, ev.Position)
			if updated != current || updatedOK != currentOK {
				current, currentOK = updated, updatedOK
				app.Notify(viewID)
			}
		})
	}

	if !isSelection {
		box := *hitbox
		links := e.links
		handler := e.linkClickHandler
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.MouseUpEvent)
			if !ok || phase != gpui.DispatchBubble || !box.IsHovered(w) {
				return
			}
			if hasView && view.Read(app, func(s *TextViewState, app *gpui.App) bool { return s.hasSelection(app) }) {
				return
			}
			link, ok := inlineLinkForPosition(layout, links, ev.Position)
			if !ok {
				return
			}
			textSelectionEndHook(w, app)
			app.StopPropagation()
			click := gpui.ClickEvent{Kind: gpui.ClickEventMouse, Mouse: gpui.MouseClickEvent{
				Down: gpui.MouseDownEvent{Button: ev.Button, Position: ev.Position, Modifiers: ev.Modifiers, ClickCount: ev.ClickCount},
				Up:   *ev,
			}}
			handleLinkClick(handler, link.URL, click, w, app)
		})
	}
	e.retainStyledText()
}

// glyphBox is where one character of laid-out text paints: its row, and
// its horizontal extent past the text's origin, alignment applied
// (U:text/inline.rs:1171-1180).
type glyphBox struct {
	text        gpui.TextRange
	row         int
	left, right float32
}

// glyphBoxes is U:text/inline.rs:1182-1261 over the row walk (see the
// file comment and inlineTextRows): characters sorted by the text they
// draw, each placed on its row and reaching to the next character on the
// row or to the row's end; every row aligned in alignWidth by align. A
// row's characters are placed by its text shaped alone, from the row's
// first character on, where upstream reads the glyph positions of the
// line layout; so the cost scales with the rows, not with one
// PositionForIndex call per character.
func glyphBoxes(text string, runs []gpui.TextRun, fontSize float32, layout *gpui.TextLayout, align gpui.TextAlign, alignWidth float32, w *gpui.Window) []glyphBox {
	origin := layout.Bounds().Origin
	lineHeight := layout.LineHeight()
	var boxes []glyphBox
	for _, r := range inlineTextRows(text, layout) {
		row := int((r.y - origin.Y) / lineHeight)
		rowText := text[r.start:r.end]
		rowStart := r.firstX - origin.X
		// A hard line break ends its row and draws nothing: the row's text
		// is shaped up to it, and the break sits at the row's end.
		shaped := rowText
		if ix := strings.IndexByte(shaped, '\n'); ix >= 0 {
			shaped = shaped[:ix]
		}
		line := inlineShapedRow(shaped, runs, r.start, fontSize, w)
		endX := rowStart
		if line != nil {
			endX += line.Width()
		}
		shift := alignedRowLeft(align, alignWidth, endX-rowStart) - rowStart
		xOf := func(offset int) float32 {
			if line == nil || offset >= r.start+len(shaped) {
				return endX
			}
			return rowStart + line.XForIndex(offset-r.start)
		}
		for offset, c := range rowText {
			start := r.start + offset
			end := start + utf8.RuneLen(c)
			boxes = append(boxes, glyphBox{
				text:  gpui.TextRange{Start: start, End: end},
				row:   row,
				left:  shift + xOf(start),
				right: shift + xOf(end),
			})
		}
	}
	sort.SliceStable(boxes, func(a, b int) bool { return boxes[a].text.Start < boxes[b].text.Start })
	return boxes
}

// inlineShapedRow is a row's text shaped alone with the runs cut to it
// (offset is the row's start in the runs' text), or nil when it is empty
// or does not shape.
func inlineShapedRow(rowText string, runs []gpui.TextRun, offset int, fontSize float32, w *gpui.Window) *gpui.ShapedLine {
	if rowText == "" {
		return nil
	}
	cut := cutTextRuns(runs, offset, offset+len(rowText))
	line, err := w.TextSystem().ShapeLine(rowText, fontSize, cut, nil)
	if err != nil {
		return nil
	}
	return line
}

// cutTextRuns cuts runs covering a text to the byte range [start, end).
func cutTextRuns(runs []gpui.TextRun, start, end int) []gpui.TextRun {
	var out []gpui.TextRun
	offset := 0
	for _, run := range runs {
		lo, hi := max(offset, start), min(offset+run.Len, end)
		if lo < hi {
			r := run
			r.Len = hi - lo
			out = append(out, r)
		}
		offset += run.Len
	}
	return out
}

// alignedRowLeft is U:text/inline.rs:1263-1269.
func alignedRowLeft(align gpui.TextAlign, alignWidth, width float32) float32 {
	switch align {
	case gpui.TextAlignCenter:
		return (alignWidth - width) / 2
	case gpui.TextAlignRight:
		return alignWidth - width
	}
	return 0
}

// rowBox is one (row, left, right) highlight box.
type rowBox struct {
	row         int
	left, right float32
}

// rangeBoxes is the boxes behind the text of r: every glyph drawing some
// of it, joined where they touch on a row (U:text/inline.rs:1271-1295).
func rangeBoxes(glyphs []glyphBox, r gpui.TextRange) []rowBox {
	first := sort.Search(len(glyphs), func(i int) bool { return glyphs[i].text.End > r.Start })
	var hits []rowBox
	for _, g := range glyphs[first:] {
		if g.text.Start >= r.End {
			break
		}
		hits = append(hits, rowBox{g.row, g.left, g.right})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].row != hits[j].row {
			return hits[i].row < hits[j].row
		}
		return hits[i].left < hits[j].left
	})
	var boxes []rowBox
	for _, h := range hits {
		if n := len(boxes); n > 0 && boxes[n-1].row == h.row && h.left <= boxes[n-1].right {
			boxes[n-1].right = max(boxes[n-1].right, h.right)
			continue
		}
		boxes = append(boxes, h)
	}
	return boxes
}

// selectionForMultiClick is the range a double or triple click selects
// (U:text/inline.rs:1297-1319). A word comes from
// U:text/selection.rs::word_range_at (ticket 29, textWordRangeAt).
func selectionForMultiClick(text string, layout *gpui.TextLayout, bounds gpui.Bounds, pos gpui.Point, kind textViewMultiClickKind) (gpui.TextRange, bool) {
	if !bounds.Contains(pos) {
		return gpui.TextRange{}, false
	}
	offset, ok := layout.IndexForPosition(pos)
	if !ok {
		return gpui.TextRange{}, false
	}
	switch kind {
	case textViewMultiClickWord:
		if textWordRangeAt == nil {
			return gpui.TextRange{}, false
		}
		return textWordRangeAt(text, offset)
	default:
		if text == "" {
			return gpui.TextRange{}, false
		}
		return gpui.TextRange{Start: 0, End: len(text)}, true
	}
}

// pointInTextSelection reports whether pos is inside the selection band,
// considering multi-line selections (U:text/inline.rs:1321-1362).
func pointInTextSelection(pos gpui.Point, charWidth float32, selectionStart, selectionEnd gpui.Point, lineHeight float32) bool {
	pointInLine := func(p gpui.Point) bool { return p.Y >= pos.Y && p.Y < pos.Y+lineHeight }
	top, bottom := min(selectionStart.Y, selectionEnd.Y), max(selectionStart.Y, selectionEnd.Y)
	x := pos.X + charWidth/2
	if pos.Y+lineHeight <= top || pos.Y > bottom {
		return false
	}
	if pointInLine(selectionStart) && pointInLine(selectionEnd) {
		left, right := min(selectionStart.X, selectionEnd.X), max(selectionStart.X, selectionEnd.X)
		return x >= left && x <= right
	}
	topPoint, bottomPoint := selectionEnd, selectionStart
	if selectionStart.Y < selectionEnd.Y {
		topPoint, bottomPoint = selectionStart, selectionEnd
	}
	switch {
	case pointInLine(topPoint):
		return x >= topPoint.X
	case pointInLine(bottomPoint):
		return x <= bottomPoint.X
	}
	return true
}
