package base

// Port of U:text/range_highlight.rs (ticket 30): application-supplied
// highlights over the text a TextViewState renders, and scrolling one of
// its ranges into view.
//
// Ranges address the rendered text, the string plain copy produces: an
// application searches TextViewState.RenderedText and hands the ranges it
// found back. Each range is split into the text leaves it covers (a
// paragraph, a heading, a code block, a table cell), which paint it as a
// background behind their glyphs, so a highlight never changes layout.
// Text outside every leaf (the separators between blocks and cells,
// custom blocks, HTML blocks, inline objects) is left unpainted.
//
// Go adaptations (recorded deviations):
//   - Ranges are gpui.TextRange; RangeHighlightError is a comparable value
//     returned as error.
//   - The reveal clock is the application clock (App.Now, the scheduler's
//     virtual clock under TestApp), as upstream reads the background
//     executor's now().

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

// RenderedText is a snapshot of the text a TextViewState renders, as of
// one parse of its content (U:text/range_highlight.rs:33-111). Offsets
// into it are UTF-8 byte offsets. Two snapshots are equal (Equal) when
// they come from the same view and the same parse. The text is built when
// first read.
type RenderedText struct {
	owner    gpui.EntityID
	revision int
	document *parsedDocument
	index    *renderedIndexCell
}

// renderedIndexCell is the Arc<OnceLock<RenderedIndex>> a view shares
// with its snapshots.
type renderedIndexCell struct {
	once  sync.Once
	index *renderedIndex
}

func (c *renderedIndexCell) get(document *parsedDocument) *renderedIndex {
	c.once.Do(func() { c.index = newRenderedIndex(document) })
	return c.index
}

// String is the rendered text (as_str).
func (t RenderedText) String() string { return t.renderedIndex().text }

// Len is the length of the rendered text, in bytes.
func (t RenderedText) Len() int { return len(t.renderedIndex().text) }

// IsEmpty reports whether the view renders no text.
func (t RenderedText) IsEmpty() bool { return t.Len() == 0 }

// Equal is RenderedText's PartialEq: the same view and the same parse.
func (t RenderedText) Equal(o RenderedText) bool {
	return t.owner == o.owner && t.revision == o.revision
}

func (t RenderedText) renderedIndex() *renderedIndex {
	if t.index == nil {
		return &renderedIndex{}
	}
	return t.index.get(t.document)
}

// RangeHighlight is a background painted behind one range of a
// RenderedText; later highlights paint over earlier ones
// (U:text/range_highlight.rs:113-139).
type RangeHighlight struct {
	rng        gpui.TextRange
	background gpui.Hsla
}

// NewRangeHighlight is a highlight over rng, in byte offsets of a
// RenderedText.
func NewRangeHighlight(rng gpui.TextRange, background gpui.Hsla) RangeHighlight {
	return RangeHighlight{rng: rng, background: background}
}

// Range is the highlighted range.
func (h RangeHighlight) Range() gpui.TextRange { return h.rng }

// Background is the highlight color.
func (h RangeHighlight) Background() gpui.Hsla { return h.background }

// RangeHighlightErrorKind is why setting range highlights or revealing a
// range was rejected (U:text/range_highlight.rs:141-152).
type RangeHighlightErrorKind int

const (
	// RangeHighlightUnsupported: the view renders HTML, which records no
	// source positions to address its text by.
	RangeHighlightUnsupported RangeHighlightErrorKind = iota + 1
	// RangeHighlightInvalidRange: the range at Index is reversed, out of
	// bounds, or not on a character boundary.
	RangeHighlightInvalidRange
)

// RangeHighlightError is the rejection; existing highlights and reveals
// stay unchanged.
type RangeHighlightError struct {
	Kind  RangeHighlightErrorKind
	Index int
}

// Error is U:text/range_highlight.rs:154-161.
func (e RangeHighlightError) Error() string {
	if e.Kind == RangeHighlightUnsupported {
		return "HTML views do not support ranges of their text"
	}
	return fmt.Sprintf("range %d is not a range of the text", e.Index)
}

func rangeHighlightInvalid(ix int) error {
	return RangeHighlightError{Kind: RangeHighlightInvalidRange, Index: ix}
}

// renderedIndex is the rendered text of one parsed document, and where
// each text leaf sits in it (U:text/range_highlight.rs:165-174).
type renderedIndex struct {
	text string
	// leaves are in document order, so by their position in text.
	leaves []leafSpan
	// blocks is where the text of each top-level block sits.
	blocks []gpui.TextRange
}

// leafSpan is U:text/range_highlight.rs:176-184.
type leafSpan struct {
	rng gpui.TextRange
	key textLeafKey
	// objects are the inline objects in the leaf's text, in leaf offsets.
	objects []gpui.TextRange
}

func rangeContains(r gpui.TextRange, offset int) bool { return offset >= r.Start && offset < r.End }

// textOffsetNear is offset in the leaf's text, moved out of an inline
// object onto the text after it, or before it at the end of the leaf;
// false when the leaf has no text outside its objects
// (U:text/range_highlight.rs:186-204).
func (l *leafSpan) textOffsetNear(offset int) (int, bool) {
	objectAt := func(offset int) *gpui.TextRange {
		for i := range l.objects {
			if rangeContains(l.objects[i], offset) {
				return &l.objects[i]
			}
		}
		return nil
	}
	after := offset
	for o := objectAt(after); o != nil; o = objectAt(after) {
		after = o.End
	}
	if after < l.rng.End-l.rng.Start {
		return after, true
	}
	before := offset
	for o := objectAt(before); o != nil; o = objectAt(before) {
		if o.Start == 0 {
			return 0, false
		}
		before = o.Start - 1
	}
	return before, true
}

// newRenderedIndex is RenderedIndex::new (U:text/range_highlight.rs:208-223).
func newRenderedIndex(document *parsedDocument) *renderedIndex {
	var b indexBuilder
	var blocks []gpui.TextRange
	if document != nil {
		blocks = make([]gpui.TextRange, 0, len(document.blocks))
		for _, block := range document.blocks {
			start := len(b.text)
			b.pushBlock(block)
			blocks = append(blocks, gpui.TextRange{Start: start, End: len(b.text)})
		}
	}
	return &renderedIndex{text: string(b.text), leaves: b.leaves, blocks: blocks}
}

func isCharBoundary(s string, i int) bool {
	return i == 0 || i == len(s) || (i < len(s) && utf8.RuneStart(s[i]))
}

// leafPiece is one (TextLeafKey, Range) piece of a resolved range.
type leafPiece struct {
	key textLeafKey
	rng gpui.TextRange
}

// resolve is the leaf ranges rng paints over, none when it covers no leaf
// text, or false when it is not a range of the text
// (U:text/range_highlight.rs:225-263).
func (x *renderedIndex) resolve(rng gpui.TextRange) ([]leafPiece, bool) {
	if rng.Start > rng.End || rng.End > len(x.text) || !isCharBoundary(x.text, rng.Start) || !isCharBoundary(x.text, rng.End) {
		return nil, false
	}
	first := sort.Search(len(x.leaves), func(i int) bool { return x.leaves[i].rng.End > rng.Start })
	pieces := []leafPiece{}
	for i := first; i < len(x.leaves); i++ {
		leaf := &x.leaves[i]
		if leaf.rng.Start >= rng.End {
			break
		}
		end := min(rng.End, leaf.rng.End) - leaf.rng.Start
		cursor := max(rng.Start, leaf.rng.Start) - leaf.rng.Start
		for _, object := range leaf.objects {
			if object.Start >= end {
				break
			}
			if object.End <= cursor {
				continue
			}
			if object.Start > cursor {
				pieces = append(pieces, leafPiece{leaf.key, gpui.TextRange{Start: cursor, End: object.Start}})
			}
			cursor = object.End
		}
		if cursor < end {
			pieces = append(pieces, leafPiece{leaf.key, gpui.TextRange{Start: cursor, End: end}})
		}
	}
	return pieces, true
}

// locate is where rng starts: the line of the first leaf text it covers,
// or, when it covers none, of the leaf text at its start or last before it
// in its top-level block, or else that whole block; false when it is not a
// range of the text, or there is none (U:text/range_highlight.rs:265-309).
func (x *renderedIndex) locate(rng gpui.TextRange) (revealTarget, bool) {
	pieces, ok := x.resolve(rng)
	if !ok {
		return revealTarget{}, false
	}
	if len(pieces) > 0 {
		return revealTarget{key: pieces[0].key, offset: pieces[0].rng.Start}, true
	}
	if len(x.blocks) == 0 {
		return revealTarget{}, false
	}
	blockIx := min(sort.Search(len(x.blocks), func(i int) bool { return x.blocks[i].End > rng.Start }), len(x.blocks)-1)
	blockStart := x.blocks[blockIx].Start
	ix := sort.Search(len(x.leaves), func(i int) bool { return x.leaves[i].rng.End > rng.Start })
	var leaf *leafSpan
	offset := 0
	if ix < len(x.leaves) && rangeContains(x.leaves[ix].rng, rng.Start) {
		leaf, offset = &x.leaves[ix], rng.Start-x.leaves[ix].rng.Start
	} else if ix > 0 && x.leaves[ix-1].rng.Start >= blockStart {
		// A position after the text of a leaf, on the separators after it
		// or at the end of the text, is on the line of the last character
		// before it in its block.
		l := &x.leaves[ix-1]
		if text := x.text[l.rng.Start:l.rng.End]; text != "" {
			_, size := utf8.DecodeLastRuneInString(text)
			leaf, offset = l, len(text)-size
		}
	}
	if leaf != nil {
		if near, ok := leaf.textOffsetNear(offset); ok {
			return revealTarget{key: leaf.key, offset: near}, true
		}
	}
	return revealTarget{block: true, ix: blockIx}, true
}

// indexBuilder builds the rendered text the way BlockNode::text does,
// recording each leaf as it goes (U:text/range_highlight.rs:312-405).
type indexBuilder struct {
	text   []byte
	leaves []leafSpan
}

func (b *indexBuilder) pushBlock(block blockNode) {
	start := len(b.text)
	switch v := block.(type) {
	case *blockRoot:
		for _, c := range v.children {
			b.pushBlock(c)
		}
	case *blockBlockquote:
		for _, c := range v.children {
			b.pushBlock(c)
		}
	case *blockList:
		for _, c := range v.children {
			b.pushBlock(c)
		}
		return
	case *blockListItem:
		for _, c := range v.children {
			b.pushBlock(c)
		}
		return
	case *paragraph:
		b.pushParagraph(v, spanLeafKey(v.span))
	case *blockHeading:
		b.pushParagraph(v.children, spanLeafKey(v.span))
	case *textTable:
		ordinal := 0
		for _, row := range v.children {
			if len(row.children) == 0 {
				continue
			}
			for ix, cell := range row.children {
				if ix > 0 {
					b.text = append(b.text, ' ')
				}
				var key *textLeafKey
				if v.span != nil {
					key = textLeafTableCell(v.span.Start, ordinal)
				}
				b.pushParagraph(cell.children, key)
				ordinal++
			}
			b.text = append(b.text, '\n')
		}
	case *CodeBlock:
		b.pushLeaf(v.Code(), spanLeafKey(v.Span), nil)
	case *blockCustom:
		b.text = append(b.text, v.node.AsText()...)
	}
	if len(b.text) > start {
		b.text = append(b.text, '\n')
	}
}

func (b *indexBuilder) pushParagraph(p *paragraph, key *textLeafKey) {
	var text []byte
	var objects []gpui.TextRange
	for i := range p.children {
		child := &p.children[i]
		if child.custom != nil {
			objects = append(objects, gpui.TextRange{Start: len(text), End: len(text) + len(child.text)})
		}
		text = append(text, child.text...)
	}
	b.pushLeaf(string(text), key, objects)
}

func (b *indexBuilder) pushLeaf(text string, key *textLeafKey, objects []gpui.TextRange) {
	start := len(b.text)
	b.text = append(b.text, text...)
	if key != nil && text != "" {
		b.leaves = append(b.leaves, leafSpan{rng: gpui.TextRange{Start: start, End: len(b.text)}, key: *key, objects: objects})
	}
}

// tableRowEnd is the last cell and source end of one table row.
type tableRowEnd struct {
	last   textLeafKey
	end    int
	hasEnd bool
}

// tableRowSourceEnds collects the last cell and source end of each table
// row (U:text/range_highlight.rs:407-437).
func tableRowSourceEnds(blocks []blockNode, rows []tableRowEnd) []tableRowEnd {
	for _, block := range blocks {
		switch v := block.(type) {
		case *textTable:
			if v.span == nil {
				continue
			}
			cellCount := 0
			for _, row := range v.children {
				if len(row.children) == 0 {
					continue
				}
				cellCount += len(row.children)
				r := tableRowEnd{last: *textLeafTableCell(v.span.Start, cellCount-1)}
				for _, cell := range row.children {
					if end, ok := paragraphSourceEnd(cell.children); ok && (!r.hasEnd || end > r.end) {
						r.end, r.hasEnd = end, true
					}
				}
				rows = append(rows, r)
			}
		case *blockRoot:
			rows = tableRowSourceEnds(v.children, rows)
		case *blockBlockquote:
			rows = tableRowSourceEnds(v.children, rows)
		case *blockList:
			rows = tableRowSourceEnds(v.children, rows)
		case *blockListItem:
			rows = tableRowSourceEnds(v.children, rows)
		}
	}
	return rows
}

// paragraphSourceEnd is where the source of p's text ends, when the parser
// recorded it (U:text/range_highlight.rs:439-456).
func paragraphSourceEnd(p *paragraph) (int, bool) {
	end, found := 0, false
	keep := func(e int) {
		if !found || e > end {
			end, found = e, true
		}
	}
	for i := range p.children {
		node := &p.children[i]
		for _, seg := range node.sourceSegments {
			keep(seg.source.End)
		}
		if node.custom != nil {
			if r, ok := node.custom.SourceRange(); ok {
				keep(r.End)
			}
		}
	}
	return end, found
}

// leafBackgrounds is one leaf's backgrounds, in the application's order.
type leafBackgrounds struct {
	key         textLeafKey
	backgrounds []textColorRange
}

// rangeHighlightFrame is the highlights each leaf paints, resolved once
// when they change so rendering only looks up its leaf
// (U:text/range_highlight.rs:458-465). leaves are sorted by key.
type rangeHighlightFrame struct {
	leaves []leafBackgrounds
}

// newRangeHighlightFrame validates highlights against text and resolves
// them to leaves; nil when they paint nothing
// (U:text/range_highlight.rs:467-496).
func newRangeHighlightFrame(text RenderedText, highlights []RangeHighlight) (*rangeHighlightFrame, error) {
	type piece struct {
		key textLeafKey
		bg  textColorRange
	}
	var pieces []piece
	index := text.renderedIndex()
	for ix, h := range highlights {
		ranges, ok := index.resolve(h.rng)
		if !ok {
			return nil, rangeHighlightInvalid(ix)
		}
		for _, r := range ranges {
			pieces = append(pieces, piece{r.key, textColorRange{rng: r.rng, color: h.background}})
		}
	}
	// Stable, so each leaf keeps the application's order.
	slices.SortStableFunc(pieces, func(a, b piece) int { return a.key.compare(b.key) })
	var leaves []leafBackgrounds
	for _, p := range pieces {
		if n := len(leaves); n > 0 && leaves[n-1].key == p.key {
			leaves[n-1].backgrounds = append(leaves[n-1].backgrounds, p.bg)
			continue
		}
		leaves = append(leaves, leafBackgrounds{key: p.key, backgrounds: []textColorRange{p.bg}})
	}
	if len(leaves) == 0 {
		return nil, nil
	}
	return &rangeHighlightFrame{leaves: leaves}, nil
}

// backgrounds is leaf key's backgrounds, in its rendered byte space
// (U:text/range_highlight.rs:498-503).
func (f *rangeHighlightFrame) backgrounds(key textLeafKey) []textColorRange {
	if f == nil {
		return nil
	}
	ix, ok := slices.BinarySearchFunc(f.leaves, key, func(l leafBackgrounds, k textLeafKey) int { return l.key.compare(k) })
	if !ok {
		return nil
	}
	return f.leaves[ix].backgrounds
}

// remap is the highlights that still describe the document remap maps the
// old one to: each follows its leaf as far as the leaf's text is
// unchanged, and is dropped with a leaf that is gone
// (U:text/range_highlight.rs:505-525).
func (f *rangeHighlightFrame) remap(remap *leafRemap) *rangeHighlightFrame {
	var leaves []leafBackgrounds
	for _, l := range f.leaves {
		newKey, unchanged, ok := remap.leaf(l.key)
		if !ok {
			continue
		}
		var clipped []textColorRange
		for _, bg := range l.backgrounds {
			if bg.rng.Start < unchanged {
				clipped = append(clipped, textColorRange{rng: gpui.TextRange{Start: bg.rng.Start, End: min(bg.rng.End, unchanged)}, color: bg.color})
			}
		}
		if len(clipped) > 0 {
			leaves = append(leaves, leafBackgrounds{key: newKey, backgrounds: clipped})
		}
	}
	// Moving keys keeps their order, but stay safe for the binary search.
	slices.SortFunc(leaves, func(a, b leafBackgrounds) int { return a.key.compare(b.key) })
	if len(leaves) == 0 {
		return nil
	}
	return &rangeHighlightFrame{leaves: leaves}
}

// unchangedAll is usize::MAX: a leaf whose whole text is unchanged.
const unchangedAll = int(^uint(0) >> 1)

// leafRemap is where the text leaves of one parsed document are found in
// the document parsed after it (U:text/range_highlight.rs:528-547).
type leafRemap struct {
	oldLen, newLen int
	// tailStart is, with an append, where the block it parsed again starts.
	tailStart       *int
	unchangedPrefix int
	unchangedSuffix int
	oldLeaves       []keyedTextLeaf
	newLeaves       []keyedTextLeaf
	// tableRows is sorted by each row's last cell; unneeded for appends.
	tableRows []tableRowEnd
}

// newLeafRemap is LeafRemap::new: with tailOnly, next was parsed by
// appending to old (U:text/range_highlight.rs:550-624).
func newLeafRemap(old, next *parsedDocument, tailOnly bool) *leafRemap {
	oldLen, newLen := len(old.source), len(next.source)
	r := &leafRemap{oldLen: oldLen, newLen: newLen}
	if tailOnly {
		tail := oldLen
		if n := len(old.blocks); n > 0 {
			if span := blockNodeSpan(old.blocks[n-1]); span != nil {
				tail = span.Start
			}
		}
		r.tailStart = &tail
		r.unchangedPrefix = oldLen
	} else {
		shorter := min(oldLen, newLen)
		prefix := 0
		for prefix < shorter && old.source[prefix] == next.source[prefix] {
			prefix++
		}
		if prefix == shorter {
			// One source extends the other, as when text is appended.
			r.unchangedPrefix = prefix
		} else {
			// Where the two overlap, the end wins: the blocks after a
			// change keep following their text rather than their offset.
			suffix := 0
			for suffix < shorter && old.source[oldLen-1-suffix] == next.source[newLen-1-suffix] {
				suffix++
			}
			r.unchangedPrefix, r.unchangedSuffix = min(prefix, shorter-suffix), suffix
		}
	}
	leavesFrom := func(document *parsedDocument) []keyedTextLeaf {
		var leaves []keyedTextLeaf
		for i := len(document.blocks) - 1; i >= 0; i-- {
			if r.tailStart != nil {
				if span := blockNodeSpan(document.blocks[i]); span == nil || span.Start < *r.tailStart {
					break
				}
			}
			leaves = textLeaves(document.blocks[i], leaves)
		}
		slices.SortStableFunc(leaves, func(a, b keyedTextLeaf) int { return a.key.compare(b.key) })
		return leaves
	}
	if !tailOnly {
		r.tableRows = tableRowSourceEnds(old.blocks, nil)
		slices.SortStableFunc(r.tableRows, func(a, b tableRowEnd) int { return a.last.compare(b.last) })
	}
	r.oldLeaves = leavesFrom(old)
	r.newLeaves = leavesFrom(next)
	return r
}

// leaf is where leaf key is in the new document, and how much of its text
// is unchanged; false when it is gone (U:text/range_highlight.rs:626-651).
func (r *leafRemap) leaf(key textLeafKey) (textLeafKey, int, bool) {
	if r.tailStart != nil && key.blockStart < *r.tailStart {
		return key, unchangedAll, true
	}
	moved, ok := r.moved(key.blockStart)
	if !ok {
		return textLeafKey{}, 0, false
	}
	newKey := key.movedTo(moved)
	oldLeaf, ok := findLeaf(r.oldLeaves, key)
	if !ok {
		return textLeafKey{}, 0, false
	}
	// A table's cells are only known by their place in it, so after a
	// change inside the table a cell is the same one only when the source
	// of its whole row ends before that change.
	if _, isCell := key.cellIx(); isCell && key.blockStart < r.unchangedPrefix && r.tailStart == nil {
		if end, ok := r.rowSourceEnd(key); !ok || end > r.unchangedPrefix {
			return textLeafKey{}, 0, false
		}
	}
	newLeaf, ok := findLeaf(r.newLeaves, newKey)
	if !ok {
		return textLeafKey{}, 0, false
	}
	return newKey, newLeaf.commonPrefixLen(oldLeaf), true
}

// rowSourceEnd is U:text/range_highlight.rs:653-660.
func (r *leafRemap) rowSourceEnd(key textLeafKey) (int, bool) {
	ix := sort.Search(len(r.tableRows), func(i int) bool { return r.tableRows[i].last.compare(key) >= 0 })
	if ix >= len(r.tableRows) {
		return 0, false
	}
	row := r.tableRows[ix]
	if row.last.blockStart != key.blockStart {
		return 0, false
	}
	return row.end, row.hasEnd
}

// moved is where the block starting at start in the old document starts
// in the new one (U:text/range_highlight.rs:662-672).
func (r *leafRemap) moved(start int) (int, bool) {
	switch {
	case start < r.unchangedPrefix:
		return start, true
	case start >= r.oldLen-r.unchangedSuffix:
		return start + r.newLen - r.oldLen, true
	}
	return 0, false
}

func findLeaf(leaves []keyedTextLeaf, key textLeafKey) (textLeaf, bool) {
	ix, ok := slices.BinarySearchFunc(leaves, key, func(l keyedTextLeaf, k textLeafKey) int { return l.key.compare(k) })
	if !ok {
		return textLeaf{}, false
	}
	return leaves[ix].leaf, true
}

// ---------------------------------------------------------------------------
// Reveal (U:text/range_highlight.rs:817-1019)
// ---------------------------------------------------------------------------

// revealTarget is where a range to reveal starts: a line of a text leaf
// (key, offset in its text), or a whole top-level block
// (U:text/range_highlight.rs:817-824).
type revealTarget struct {
	block  bool
	key    textLeafKey
	offset int
	ix     int
}

// revealTimeout is how long a reveal keeps trying
// (U:text/range_highlight.rs:826-829).
const revealTimeout = time.Second

// revealAttempts is how many frames a reveal whose line was laid out but
// not visible keeps trying (U:text/range_highlight.rs:831-833).
const revealAttempts = 8

// revealReport is where the line a reveal starts on was laid out in one
// frame, in window coordinates, and whether it was visible
// (U:text/range_highlight.rs:835-841).
type revealReport struct {
	line    gpui.Bounds
	visible bool
}

// revealReportCell is the shared Arc<Mutex<Option<RevealReport>>>.
type revealReportCell struct {
	mu     sync.Mutex
	report *revealReport
}

func (c *revealReportCell) set(r *revealReport) {
	c.mu.Lock()
	c.report = r
	c.mu.Unlock()
}

func (c *revealReportCell) get() *revealReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.report
}

// pendingReveal is a range TextViewState.RevealRange is scrolling into
// view (U:text/range_highlight.rs:843-858).
type pendingReveal struct {
	target      revealTarget
	requestedAt time.Duration
	report      *revealReportCell
	attempts    int
}

// revealProgressKind is U:text/range_highlight.rs:860-868.
type revealProgressKind int

const (
	revealShown revealProgressKind = iota
	revealHidden
	revealNotLaidOut
)

// newPendingReveal is the start of rng in text, asked for at now; false
// when rng is not a range of it (U:text/range_highlight.rs:871-880).
func newPendingReveal(text RenderedText, rng gpui.TextRange, now time.Duration) (*pendingReveal, bool) {
	target, ok := text.renderedIndex().locate(rng)
	if !ok {
		return nil, false
	}
	return &pendingReveal{target: target, requestedAt: now, report: &revealReportCell{}}, true
}

// isExpired is U:text/range_highlight.rs:882-885.
func (p *pendingReveal) isExpired(now time.Duration) bool {
	return max(now-p.requestedAt, 0) > revealTimeout || p.attempts >= revealAttempts
}

// isBlock is U:text/range_highlight.rs:887-890.
func (p *pendingReveal) isBlock() bool { return p.target.block }

// blockIx is the index of the top-level block of document the reveal
// starts in (U:text/range_highlight.rs:892-902).
func (p *pendingReveal) blockIx(document *parsedDocument) (int, bool) {
	if p.target.block {
		return p.target.ix, p.target.ix < len(document.blocks)
	}
	for i := len(document.blocks) - 1; i >= 0; i-- {
		if span := blockNodeSpan(document.blocks[i]); span != nil && span.Start <= p.target.key.blockStart {
			return i, true
		}
	}
	return 0, false
}

// wasLaidOut is U:text/range_highlight.rs:904-907.
func (p *pendingReveal) wasLaidOut() bool { return p.report.get() != nil }

// request starts a frame: forgets the previous report and hands the line
// to rendering; a block has no line (U:text/range_highlight.rs:909-923).
func (p *pendingReveal) request() *revealRequest {
	p.report.set(nil)
	if p.target.block {
		return nil
	}
	return &revealRequest{key: p.target.key, offset: p.target.offset, report: p.report}
}

// progress ends a frame with what the line reported, counting a frame in
// which it was laid out but hidden as an attempt
// (U:text/range_highlight.rs:925-937).
func (p *pendingReveal) progress() (revealProgressKind, gpui.Bounds) {
	report := p.report.get()
	switch {
	case report == nil:
		return revealNotLaidOut, gpui.Bounds{}
	case report.visible:
		return revealShown, gpui.Bounds{}
	}
	p.attempts++
	return revealHidden, report.line
}

// remap is the reveal in the document remap maps the old one to, as long
// as the text it starts at is unchanged (U:text/range_highlight.rs:939-949).
func (p *pendingReveal) remap(remap *leafRemap) *pendingReveal {
	if p.target.block {
		return nil
	}
	key, unchanged, ok := remap.leaf(p.target.key)
	if !ok || p.target.offset >= unchanged {
		return nil
	}
	p.target.key = key
	return p
}

// revealRequest is the start of a pending reveal, as rendering hands it to
// the Inline that lays that text out (U:text/range_highlight.rs:952-979).
type revealRequest struct {
	key    textLeafKey
	offset int
	report *revealReportCell
}

// at is the start of the reveal when it is in key's text between start and
// end, rebased to start.
func (r *revealRequest) at(key *textLeafKey, start, end int) *revealAt {
	if r == nil || key == nil || *key != r.key {
		return nil
	}
	return (&revealAt{offset: r.offset, report: r.report}).rebase(start, end)
}

// revealAt is the start of a pending reveal in the byte space of one run
// of text (U:text/range_highlight.rs:981-1019).
type revealAt struct {
	offset int
	report *revealReportCell
}

// rebase is the reveal in the text between start and end, rebased to
// start, or nil when it starts outside it.
func (r *revealAt) rebase(start, end int) *revealAt {
	if r == nil || r.offset < start || r.offset >= end {
		return nil
	}
	return &revealAt{offset: r.offset - start, report: r.report}
}

// clamp moves the reveal into the text between start and end and rebases
// it to start.
func (r *revealAt) clamp(start, end int) *revealAt {
	return &revealAt{offset: min(max(r.offset, start), end) - start, report: r.report}
}

// reportLine reports where the line the reveal starts on was laid out, in
// window coordinates, and whether it was inside the visible area.
func (r *revealAt) reportLine(line gpui.Bounds, visible bool) {
	r.report.set(&revealReport{line: line, visible: visible})
}
