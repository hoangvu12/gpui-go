package base

// Port of U:text/stream_fade.rs (ticket 30): fades streamed text in. The
// rendered characters an update adds start transparent and reach full
// color over TextViewMotion.StreamFade, as one chunk, or word by word
// (character by character for CJK) when StreamFadeStagger separates them.
// An update too large to light up within one fade goes back to fading as
// one chunk (stagger_step).
//
// The tracker compares rendered text, not source, so `**bo` completing
// into bold `bold` fades the changed glyphs rather than mapping source
// bytes.
//
// Go adaptations (recorded deviations):
//   - Instant is time.Time (the wall clock Instant::now() reads, as
//     upstream; the repaint tick waits on the scheduler clock).
//   - TextLeafKey is ticket 28's textLeafKey (text_node_render.go); its
//     ElementId conversion is textLeafElementID (text_inline_flow.go).

import (
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TextViewMotion is the motion policy of a text view. Base plays it; every
// duration defaults to zero, so an unstyled view adopts new content at
// once (U:text/stream_fade.rs:25-42).
type TextViewMotion struct {
	streamFade        time.Duration
	streamFadeStagger time.Duration
	streamFadeEasing  Easing
}

// DefaultTextViewMotion is TextViewMotion::default
// (U:text/stream_fade.rs:34-42).
func DefaultTextViewMotion() TextViewMotion {
	return TextViewMotion{streamFadeEasing: DefaultEasing()}
}

// WithStreamFade sets how long the text an update appends takes to reach
// full color (U:text/stream_fade.rs:45-49).
func (m TextViewMotion) WithStreamFade(duration time.Duration) TextViewMotion {
	m.streamFade = duration
	return m
}

// WithStreamFadeStagger sets how much later each further word of one
// update starts fading than the word before it; zero fades the update as
// one chunk (U:text/stream_fade.rs:51-58).
func (m TextViewMotion) WithStreamFadeStagger(stagger time.Duration) TextViewMotion {
	m.streamFadeStagger = stagger
	return m
}

// WithStreamFadeEasing sets the curve the appended text fades in along
// (U:text/stream_fade.rs:60-63).
func (m TextViewMotion) WithStreamFadeEasing(easing Easing) TextViewMotion {
	m.streamFadeEasing = easing
	return m
}

// StreamFade is U:text/stream_fade.rs:65-67.
func (m TextViewMotion) StreamFade() time.Duration { return m.streamFade }

// StreamFadeStagger is U:text/stream_fade.rs:69-71.
func (m TextViewMotion) StreamFadeStagger() time.Duration { return m.streamFadeStagger }

// StreamFadeEasing is U:text/stream_fade.rs:73-75.
func (m TextViewMotion) StreamFadeEasing() Easing { return m.streamFadeEasing }

// staggerStep is the start offset between consecutive words of an update
// words long: the stagger as asked, or nothing once the last word would
// start later than one fade (U:text/stream_fade.rs:77-95).
func (m TextViewMotion) staggerStep(words int) time.Duration {
	if words < 2 {
		return 0
	}
	span := int64(m.streamFadeStagger) * int64(words-1)
	if span > int64(m.streamFade) {
		return 0
	}
	return m.streamFadeStagger
}

// textLeafKey methods (U:text/stream_fade.rs:120-152); the type and its
// block/table_cell constructors are ticket 28's (text_node_render.go).

// cellIx is the index of the table cell the leaf is among all the cells of
// its table, or false when it is not a cell.
func (k textLeafKey) cellIx() (int, bool) {
	if k.ordinal == 0 {
		return 0, false
	}
	return k.ordinal - 1, true
}

// movedTo is the same leaf in its block moved to start at blockStart.
func (k textLeafKey) movedTo(blockStart int) textLeafKey {
	k.blockStart = blockStart
	return k
}

// compare orders keys as their leaves appear in the document (the derived
// Ord, U:text/stream_fade.rs:102).
func (k textLeafKey) compare(o textLeafKey) int {
	switch {
	case k.blockStart != o.blockStart:
		if k.blockStart < o.blockStart {
			return -1
		}
		return 1
	case k.ordinal < o.ordinal:
		return -1
	case k.ordinal > o.ordinal:
		return 1
	}
	return 0
}

// streamFadeLeaf is one leaf's fade ranges in a frame.
type streamFadeLeaf struct {
	key   textLeafKey
	fades []textFadeRange
}

// streamFadeFrame is one frame's fade factors, resolved once per render so
// node rendering only looks up its leaf (U:text/stream_fade.rs:154-172).
type streamFadeFrame struct {
	leaves []streamFadeLeaf
}

// fades is the fade ranges of leaf key; false when it fades nothing.
func (f *streamFadeFrame) fades(key textLeafKey) ([]textFadeRange, bool) {
	if f == nil {
		return nil, false
	}
	for _, leaf := range f.leaves {
		if leaf.key == key {
			return leaf.fades, true
		}
	}
	return nil, false
}

// fadeSegment is U:text/stream_fade.rs:174-177.
type fadeSegment struct {
	start, end int
	startedAt  time.Time
}

// pendingUpdateKind is U:text/stream_fade.rs:179-189.
type pendingUpdateKind int

const (
	pendingUpdateNone pendingUpdateKind = iota
	// pendingUpdateExtend: every update since the last parse extended the
	// text, which was origin bytes long before the first of them.
	pendingUpdateExtend
	pendingUpdateReplace
)

type pendingUpdate struct {
	kind   pendingUpdateKind
	origin int
}

// leafSegments is one leaf's fading segments.
type leafSegments struct {
	key      textLeafKey
	segments []fadeSegment
}

// streamFadeTracker tracks which rendered text arrived recently and
// samples its fade (U:text/stream_fade.rs:191-197).
type streamFadeTracker struct {
	motion   TextViewMotion
	pending  pendingUpdate
	segments []leafSegments
}

func newStreamFadeTracker() streamFadeTracker {
	return streamFadeTracker{motion: DefaultTextViewMotion()}
}

// setMotion is U:text/stream_fade.rs:200-206.
func (t *streamFadeTracker) setMotion(motion TextViewMotion) {
	if motion.streamFade == 0 {
		t.segments = nil
		t.pending = pendingUpdate{}
	}
	t.motion = motion
}

// isEnabled is U:text/stream_fade.rs:208-210.
func (t *streamFadeTracker) isEnabled() bool { return t.motion.streamFade != 0 }

// noteExtend records that the next update extends the current text, which
// is n bytes long (U:text/stream_fade.rs:212-224).
func (t *streamFadeTracker) noteExtend(n int) {
	if !t.isEnabled() {
		return
	}
	switch t.pending.kind {
	case pendingUpdateNone:
		t.pending = pendingUpdate{kind: pendingUpdateExtend, origin: n}
	case pendingUpdateExtend:
		t.pending.origin = min(t.pending.origin, n)
	}
}

// noteReplace records that the next update replaces the current text
// (U:text/stream_fade.rs:226-231).
func (t *streamFadeTracker) noteReplace() {
	if t.isEnabled() {
		t.pending = pendingUpdate{kind: pendingUpdateReplace}
	}
}

// discardPending forgets the noted update after its parse failed
// (U:text/stream_fade.rs:233-236).
func (t *streamFadeTracker) discardPending() { t.pending = pendingUpdate{} }

// record records what next renders that old did not, for the updates
// noted since the last call, as segments that start fading at now. Only
// blocks the extension reaches are compared, walking both documents from
// the tail (U:text/stream_fade.rs:238-317).
func (t *streamFadeTracker) record(old, next *parsedDocument, now time.Time) {
	pending := t.pending
	t.pending = pendingUpdate{}
	if !t.isEnabled() {
		t.segments = nil
		return
	}
	switch pending.kind {
	case pendingUpdateNone:
		return
	case pendingUpdateReplace:
		t.segments = nil
		return
	}
	origin := pending.origin

	var affected []keyedTextLeaf
	for i := len(next.blocks) - 1; i >= 0; i-- {
		if span := blockNodeSpan(next.blocks[i]); span != nil && span.End <= origin {
			break
		}
		affected = textLeaves(next.blocks[i], affected)
	}
	if len(affected) == 0 {
		return
	}
	firstStart := affected[0].key.blockStart
	for _, a := range affected {
		firstStart = min(firstStart, a.key.blockStart)
	}

	var previous []keyedTextLeaf
	for i := len(old.blocks) - 1; i >= 0; i-- {
		if span := blockNodeSpan(old.blocks[i]); span != nil && span.End < firstStart {
			break
		}
		previous = textLeaves(old.blocks[i], previous)
	}

	for _, a := range affected {
		leaf := a.leaf
		n := leaf.len()
		prefix := 0
		for _, p := range previous {
			if p.key == a.key {
				prefix = leaf.commonPrefixLen(p.leaf)
				break
			}
		}
		ix := -1
		for i := range t.segments {
			if t.segments[i].key == a.key {
				ix = i
				break
			}
		}
		if ix < 0 {
			t.segments = append(t.segments, leafSegments{key: a.key})
			ix = len(t.segments) - 1
		}
		// Text past the divergence is repainted, so its earlier fade no
		// longer describes what is on screen.
		kept := t.segments[ix].segments[:0]
		for _, s := range t.segments[ix].segments {
			s.end = min(s.end, prefix)
			if s.start < s.end {
				kept = append(kept, s)
			}
		}
		t.segments[ix].segments = kept
		if prefix >= n {
			continue
		}
		if t.motion.streamFadeStagger == 0 {
			t.segments[ix].segments = append(t.segments[ix].segments, fadeSegment{start: prefix, end: n, startedAt: now})
			continue
		}
		words := fadeUnits(leaf.chunks(), prefix, n)
		step := t.motion.staggerStep(len(words))
		for wix, r := range words {
			t.segments[ix].segments = append(t.segments[ix].segments, fadeSegment{start: r[0], end: r[1], startedAt: now.Add(step * time.Duration(wix))})
		}
	}
	kept := t.segments[:0]
	for _, s := range t.segments {
		if len(s.segments) > 0 {
			kept = append(kept, s)
		}
	}
	t.segments = kept
}

// frame samples every unfinished segment at now, dropping the finished
// ones; nil means nothing is fading, so no frame needs to follow
// (U:text/stream_fade.rs:319-354).
func (t *streamFadeTracker) frame(now time.Time, reduceMotion bool) *streamFadeFrame {
	if len(t.segments) == 0 {
		return nil
	}
	if reduceMotion || !t.isEnabled() {
		t.segments = nil
		return nil
	}
	timing := NewTiming(t.motion.streamFade).Ease(t.motion.streamFadeEasing)
	var leaves []streamFadeLeaf
	keptLeaves := t.segments[:0]
	for _, ls := range t.segments {
		var fades []textFadeRange
		kept := ls.segments[:0]
		for _, s := range ls.segments {
			elapsed := max(now.Sub(s.startedAt), 0)
			sample := timing.Sample(elapsed)
			if sample.Finished {
				continue
			}
			fadeOut := min(max(1-sample.DirectedProgress, 0), 1)
			fades = append(fades, textFadeRange{rng: gpui.TextRange{Start: s.start, End: s.end}, fadeOut: fadeOut})
			kept = append(kept, s)
		}
		ls.segments = kept
		if len(fades) == 0 {
			continue
		}
		leaves = append(leaves, streamFadeLeaf{key: ls.key, fades: fades})
		keptLeaves = append(keptLeaves, ls)
	}
	t.segments = keptLeaves
	if len(leaves) == 0 {
		return nil
	}
	return &streamFadeFrame{leaves: leaves}
}

// textLeaf is a block's rendered text, in the byte space its highlights
// use: a paragraph's children or a code block's code
// (U:text/stream_fade.rs:357-361).
type textLeaf struct {
	paragraph *paragraph
	code      string
}

// keyedTextLeaf is one (TextLeafKey, TextLeaf) pair.
type keyedTextLeaf struct {
	key  textLeafKey
	leaf textLeaf
}

// chunks is the leaf's text in its chunks (U:text/stream_fade.rs:363-385).
func (l textLeaf) chunks() []string {
	if l.paragraph == nil {
		return []string{l.code}
	}
	out := make([]string, len(l.paragraph.children))
	for i := range l.paragraph.children {
		out[i] = l.paragraph.children[i].text
	}
	return out
}

// len is U:text/stream_fade.rs:387-389.
func (l textLeaf) len() int {
	n := 0
	for _, c := range l.chunks() {
		n += len(c)
	}
	return n
}

// commonPrefixLen is the length of the rendered text l shares with old, on
// a char boundary of l (U:text/stream_fade.rs:391-396).
func (l textLeaf) commonPrefixLen(old textLeaf) int {
	chunks := l.chunks()
	return floorCharBoundary(chunks, commonPrefixLen(chunks, old.chunks()))
}

// textLeaves appends block's leaves to out (U:text/stream_fade.rs:399-446).
func textLeaves(block blockNode, out []keyedTextLeaf) []keyedTextLeaf {
	switch v := block.(type) {
	case *paragraph:
		if v.span != nil {
			out = append(out, keyedTextLeaf{*textLeafBlock(v.span.Start), textLeaf{paragraph: v}})
		}
	case *blockHeading:
		if v.span != nil {
			out = append(out, keyedTextLeaf{*textLeafBlock(v.span.Start), textLeaf{paragraph: v.children}})
		}
	case *CodeBlock:
		if v.Span != nil {
			out = append(out, keyedTextLeaf{*textLeafBlock(v.Span.Start), textLeaf{code: v.Code()}})
		}
	case *textTable:
		if v.span != nil {
			ordinal := 0
			for _, row := range v.children {
				for _, cell := range row.children {
					out = append(out, keyedTextLeaf{*textLeafTableCell(v.span.Start, ordinal), textLeaf{paragraph: cell.children}})
					ordinal++
				}
			}
		}
	case *blockRoot:
		for _, c := range v.children {
			out = textLeaves(c, out)
		}
	case *blockBlockquote:
		for _, c := range v.children {
			out = textLeaves(c, out)
		}
	case *blockList:
		for _, c := range v.children {
			out = textLeaves(c, out)
		}
	case *blockListItem:
		for _, c := range v.children {
			out = textLeaves(c, out)
		}
	}
	return out
}

// commonPrefixLen compares chunk by chunk, descending to bytes only at the
// first chunk pair that differs (U:text/stream_fade.rs:448-484).
func commonPrefixLen(a, b []string) int {
	var aRest, bRest string
	ai, bi, n := 0, 0, 0
	for {
		if aRest == "" {
			if ai == len(a) {
				return n
			}
			aRest = a[ai]
			ai++
			continue
		}
		if bRest == "" {
			if bi == len(b) {
				return n
			}
			bRest = b[bi]
			bi++
			continue
		}
		step := min(len(aRest), len(bRest))
		if aRest[:step] != bRest[:step] {
			i := 0
			for i < step && aRest[i] == bRest[i] {
				i++
			}
			return n + i
		}
		n += step
		aRest, bRest = aRest[step:], bRest[step:]
	}
}

// fadeUnits splits start..end of the rendered text into the units that
// fade one after another: a word together with the whitespace after it, or
// one CJK character (U:text/stream_fade.rs:486-532).
func fadeUnits(chunks []string, start, end int) [][2]int {
	var units [][2]int
	unitStart := start
	unitHasGlyph := false
	var previous rune
	hasPrevious := false
	offset := 0
	for _, chunk := range chunks {
		if offset+len(chunk) <= start {
			offset += len(chunk)
			if chunk != "" {
				previous, _ = utf8.DecodeLastRuneInString(chunk)
				hasPrevious = true
			} else {
				hasPrevious = false
			}
			continue
		}
		for ix, c := range chunk {
			position := offset + ix
			if position >= end {
				break
			}
			if position >= start {
				startsUnit := unitHasGlyph && !unicode.IsSpace(c) &&
					(isCJK(c) || (hasPrevious && (unicode.IsSpace(previous) || isCJK(previous))))
				if startsUnit && position > unitStart {
					units = append(units, [2]int{unitStart, position})
					unitStart = position
					unitHasGlyph = false
				}
				unitHasGlyph = unitHasGlyph || !unicode.IsSpace(c)
			}
			previous, hasPrevious = c, true
		}
		offset += len(chunk)
		if offset >= end {
			break
		}
	}
	if unitStart < end {
		units = append(units, [2]int{unitStart, end})
	}
	return units
}

// isCJK is U:text/stream_fade.rs:534-544.
func isCJK(c rune) bool {
	switch {
	case c >= 0x3040 && c <= 0x30FF, // Hiragana, Katakana
		c >= 0x3400 && c <= 0x4DBF,   // CJK Unified Ideographs Extension A
		c >= 0x4E00 && c <= 0x9FFF,   // CJK Unified Ideographs
		c >= 0xAC00 && c <= 0xD7AF,   // Hangul syllables
		c >= 0xF900 && c <= 0xFAFF,   // CJK Compatibility Ideographs
		c >= 0x20000 && c <= 0x2FA1F: // CJK Unified Ideographs Extensions B and later
		return true
	}
	return false
}

// floorCharBoundary is U:text/stream_fade.rs:546-560.
func floorCharBoundary(chunks []string, offset int) int {
	start := 0
	for _, chunk := range chunks {
		end := start + len(chunk)
		if offset < end {
			local := offset - start
			for local > 0 && !utf8.RuneStart(chunk[local]) {
				local--
			}
			return start + local
		}
		start = end
	}
	return offset
}
