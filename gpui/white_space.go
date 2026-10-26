package gpui

// CSS white-space processing, browser-core ticket 38
// (.scratch/browser-core/issues/38-white-space-and-tracking.md, spec
// story 22: "white-space collapsing (normal, nowrap, pre-line), so that
// source formatting doesn't leak into rendered text", story 28: "tab
// stops of 8 spaces under pre").
//
// The port collapses Go-side, before shaping, keeping an offset map so
// carets, selection and hit-testing still land on the right SOURCE byte
// offsets (the ticket's "caret byte map"). No native change.
//
// The rules are Chromium's (the research checkout,
// %TEMP%\research-src\chromium), whose per-item state machine this file
// re-expresses over whole documents:
//
//   - The mode matrix is core/css/white_space.h:47-108 —
//     EWhiteSpace kNormal/kNowrap/kPre/kPreLine/kPreWrap over the
//     WhiteSpaceCollapse (collapse / preserve / preserve-breaks) and
//     TextWrapMode (wrap / nowrap) longhands. ShouldCollapseWhiteSpaces
//     (white_space.h:59-61) picks normal/nowrap/pre-line;
//     ShouldPreserveBreaks (white_space.h:69-71) picks pre/pre-line/
//     pre-wrap; ShouldWrapLine (white_space.h:84-86) picks normal/
//     pre-line/pre-wrap.
//   - Collapsing itself is AppendCollapseWhitespace
//     (core/layout/inline/inline_items_builder.cc:798-965): the input is
//     segmented into runs of collapsible spaces (space, tab, line feed,
//     carriage return — platform/text/character.h:179-182) and runs of
//     other text; a space run collapses to ONE space, except when the
//     run is the leading run of an item and the previous content ends
//     with a collapsible item — a kept space or a forced break
//     (AppendForcedBreak's "pretend" rule, inline_items_builder.cc:
//     1190-1196) — or the paragraph itself is still empty: then the run
//     is removed entirely (inline_items_builder.cc:812-896, the leading
//     branch).
//   - pre-line splits the text at newlines and collapses the segments
//     between them (AppendPreserveNewline, inline_items_builder.cc:
//     1150-1175); a preserved newline removes the collapsible space
//     before it (AppendForcedBreakCollapseWhitespace, 1212-1218) and
//     leaves the line's leading spaces collapsible.
//   - The paragraph's final trailing space is removed (ExitBlock →
//     RemoveTrailingCollapsibleSpaceIfExists, inline_items_builder.cc:
//     1624-1638); a forced break never is (RemoveTrailingCollapsible
//     Space's kControl guard, 1379-1410).
//   - Atomic inline boxes are opaque separators in the text stream, but
//     they are NOT opaque to the collapse state: an atomic inline item
//     ends kNotCollapsible (AppendAtomicInline appends it through Append,
//     not AppendOpaque — only open/close tags, bidi controls, empty text
//     items and out-of-flow boxes are kOpaqueToCollapsing,
//     inline_items_builder.cc:218-228), so it IS the last item to collapse
//     with. A leading space run after a box therefore keeps its one space
//     (AppendCollapseWhitespace's kNotCollapsible arm, :835-840), a space
//     before a trailing box survives ExitBlock's final removal
//     (LastItemToCollapseWith stops at the box), and
//     AppendAtomicInline even restores a space removed before it
//     (:1281-1289). Verified against Chrome 155: `a <box> b` renders one
//     space on each side of the box.
//   - Carriage returns and form feeds are ignored in preserve modes
//     (ShouldIgnore, inline_items_builder.cc:163-168, and the line
//     breaker's kCarriageReturn/kFormFeed arms, line_breaker.cc:2998-
//     3005); in collapsing modes a carriage return is just a
//     collapsible space (character.h:179-182) and a form feed is a
//     regular character — Blink's recorded reading of
//     https://drafts.csswg.org/css-text-3/#white-space-processing.
//   - A paragraph whose text collapses away entirely produces no line
//     box: the block is self-collapsing (inline_layout_algorithm.cc:
//     1488-1492, IsEmptyLine → SetIsSelfCollapsing).
//
// The offset map mirrors Chromium's OffsetMapping
// (core/layout/inline/offset_mapping.h:37-105): identity units map a
// source range one-to-one onto the display text, collapsed units map a
// source range onto a single display offset (removed). A space run
// collapses to one space identity-mapped to the run's FIRST character
// (AppendCollapseWhitespace's AppendIdentityMapping(1), 885-892 and
// 982-986); the rest is one collapsed unit. Offset conversion follows
// OffsetMappingUnit::ConvertDOMOffsetToTextContent /
// ConvertTextContentToFirstDOMOffset / ConvertTextContentToLastDOMOffset
// (offset_mapping.cc:156-198), with GetFirstMappingUnit /
// GetLastMappingUnit's boundary choices (offset_mapping.cc:506-560).
//
// Deliberately not ported (recorded limitations): the segment-break
// removal rules (ShouldRemoveNewline — with the East-Asian width
// transformation compiled out, inline_items_builder.cc:66-108, only
// zero-width space adjacency can remove a run, which this port treats
// as never); the generated break opportunity when a nowrap item is
// followed by a wrapping one (inline_items_builder.cc:860-872 — a
// collapsed space is removed but the break opportunity it carries is
// regenerated; this port drops it, which matters only when adjacent
// inline segments disagree about wrapping); text-transform: full-width's
// ideographic collapsed space (GetCollapsedSpaceChar, 210-215); and
// RestoreTrailingCollapsibleSpaceIfRemoved (1425-1442 — the restore is
// unreachable once forced breaks append their control item, and this
// port has no generated content).

import (
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// The mode matrix (white_space.h)
// ---------------------------------------------------------------------------

// WrapsAtWidth reports whether text in this mode soft-wraps at the wrap
// width (the public face of whiteSpaceWrapsAtWidth: Base's inline flow
// gates its own wrapping on it, base/text_inline_flow.go).
func (w WhiteSpace) WrapsAtWidth() bool { return w.whiteSpaceWrapsAtWidth() }

// whiteSpaceTabSize returns the browser tab-size request of a mode:
// the space-preserving modes keep tabs, so their tabs shape at the
// CSS default 8-space stops (browser-core 38); every other mode folds
// tabs into the collapse (or is the pin's path) and answers nil.
func whiteSpaceTabSize(mode WhiteSpace) *uint32 {
	if mode.whiteSpaceTabStops() {
		spaces := CSSDefaultTabSizeSpaces
		return &spaces
	}
	return nil
}

// whiteSpaceCollapsesSpaces reports whether the mode collapses runs of
// spaces and tabs (ShouldCollapseWhiteSpaces, white_space.h:59-61:
// normal, nowrap and pre-line collapse; pre and pre-wrap preserve).
func (w WhiteSpace) whiteSpaceCollapsesSpaces() bool {
	return w == WhiteSpaceNormal || w == WhiteSpaceNowrap || w == WhiteSpacePreLine
}

// whiteSpacePreservesSpaces reports whether the mode preserves spaces
// and tabs (ShouldPreserveWhiteSpaces, white_space.h:52-56: pre and
// pre-wrap; only here do tabs survive to the tab-stop shaping).
func (w WhiteSpace) whiteSpacePreservesSpaces() bool {
	return w == WhiteSpacePre || w == WhiteSpacePreWrap
}

// whiteSpacePreservesBreaks reports whether the mode preserves newlines
// as forced line breaks (ShouldPreserveBreaks, white_space.h:69-71: pre,
// pre-line and pre-wrap).
func (w WhiteSpace) whiteSpacePreservesBreaks() bool {
	return w == WhiteSpacePre || w == WhiteSpacePreLine || w == WhiteSpacePreWrap
}

// whiteSpaceWrapsAtWidth reports whether the mode soft-wraps at the wrap
// width (ShouldWrapLine, white_space.h:84-86: normal, pre-line and
// pre-wrap).
func (w WhiteSpace) whiteSpaceWrapsAtWidth() bool {
	return w == WhiteSpaceNormal || w == WhiteSpacePreLine || w == WhiteSpacePreWrap
}

// whiteSpaceTabStops reports whether a tab in this mode reaches the
// tab-stop machinery: only the space-preserving modes keep tabs
// (AppendPreserveWhitespace's kTab control items, inline_items_builder.
// cc:1108-1119); the collapsing modes fold them into the space run.
func (w WhiteSpace) whiteSpaceTabStops() bool {
	return w == WhiteSpacePre || w == WhiteSpacePreWrap
}

// ---------------------------------------------------------------------------
// The byte map (offset_mapping.h)
// ---------------------------------------------------------------------------

// collapseUnit is one span of the source↔display byte map (the pinned
// shape of OffsetMappingUnit, offset_mapping.h:37-105, reduced to the
// two kinds this collapse produces):
//
//   - identity: [domStart, domEnd) maps one-to-one onto
//     [textStart, textEnd) (equal lengths);
//   - collapsed: [domStart, domEnd) maps onto the single offset
//     textStart == textEnd (the source range was removed).
//
// A space run collapsed to one space is an identity unit over the run's
// first character followed by a collapsed unit over the rest — the
// source shape Chromium's AppendIdentityMapping(1) +
// AppendCollapsedMapping produce.
type collapseUnit struct {
	domStart, domEnd   int
	textStart, textEnd int
	// keptSpace marks the ONE space a collapsing run folded to (the
	// AppendIdentityMapping(1) unit): it is what RemoveTrailing
	// CollapsibleSpace removes at a paragraph's end or a preserved
	// newline — a PRESERVED space (the pre modes' identity text) is
	// never removed (the pre item is kNotCollapsible).
	keptSpace bool
}

// collapsed reports the removed kind.
func (u collapseUnit) collapsed() bool { return u.textStart == u.textEnd }

// collapseMap converts byte offsets between the source text and its
// collapsed display text (OffsetMapping). Units are sorted by domStart
// and cover the source exactly; a nil *collapseMap is the identity map.
type collapseMap struct {
	units []collapseUnit
}

// domToText maps a source byte offset onto the display text
// (OffsetMappingUnit::ConvertDOMOffsetToTextContent, offset_mapping.cc:
// 156-172): a unit's dom end maps to its text end, an interior offset of
// an identity unit maps linearly, and every interior offset of a
// collapsed unit maps to its single text offset.
func (m *collapseMap) domToText(dom int) int {
	if m == nil {
		return dom
	}
	units := m.units
	if len(units) == 0 {
		return dom
	}
	if dom < 0 {
		return 0
	}
	if dom >= units[len(units)-1].domEnd {
		return units[len(units)-1].textEnd
	}
	// The first unit whose domEnd > dom contains the offset.
	i := firstUnitPast(units, dom, func(u collapseUnit) int { return u.domEnd })
	u := units[i]
	switch {
	case dom <= u.domStart:
		return u.textStart
	case dom >= u.domEnd:
		return u.textEnd
	case u.collapsed():
		return u.textStart
	default:
		return u.textStart + (dom - u.domStart)
	}
}

// textToDomFirst maps a display byte offset to the FIRST source offset
// it can stand for (ConvertTextContentToFirstDOMOffset with
// GetFirstMappingUnit's unit choice, offset_mapping.cc:175-188 and
// 506-531: at a shared boundary the later unit wins, so an offset right
// after kept content prefers the removed range's start).
func (m *collapseMap) textToDomFirst(text int) int {
	return m.textToDom(text, true)
}

// textToDomLast maps a display byte offset to the LAST source offset it
// can stand for (ConvertTextContentToLastDOMOffset with
// GetLastMappingUnit's unit choice, offset_mapping.cc:190-198 and
// 533-560).
func (m *collapseMap) textToDomLast(text int) int {
	return m.textToDom(text, false)
}

// textToDom implements the first/last conversions: both find a unit
// whose [textStart, textEnd] contains the offset — first prefers the
// earliest unit at or after a boundary, last the latest at or before it
// — then collapse to domStart/domEnd or map linearly.
func (m *collapseMap) textToDom(text int, first bool) int {
	if m == nil {
		return text
	}
	units := m.units
	if len(units) == 0 {
		return text
	}
	if text < 0 {
		return 0
	}
	if text >= units[len(units)-1].textEnd {
		return units[len(units)-1].domEnd
	}
	// The first unit whose textEnd >= text contains the offset or ends
	// at it. GetFirstMappingUnit prefers a unit that STARTS at the
	// offset (its next_unit->TextContentStart() == offset branch,
	// offset_mapping.cc:518-531); GetLastMappingUnit prefers the LAST
	// unit that starts at or before it (its upper_bound over
	// TextContentStart, offset_mapping.cc:533-554).
	i := firstUnitPastOrAt(units, text)
	if first {
		if i+1 < len(units) && units[i+1].textStart == text && units[i].textEnd == text {
			i++
		}
	} else {
		for i+1 < len(units) && units[i+1].textStart <= text {
			i++
		}
	}
	u := units[i]
	if u.textStart > text {
		// The offset falls before the unit (only possible for the first
		// unit): clamp to the source start.
		return 0
	}
	if u.collapsed() {
		if first {
			return u.domStart
		}
		return u.domEnd
	}
	dom := u.domStart + (text - u.textStart)
	if dom > u.domEnd {
		return u.domEnd
	}
	return dom
}

// firstUnitPast returns the index of the first unit whose end (selected
// by endOf) is strictly greater than offset.
func firstUnitPast(units []collapseUnit, offset int, endOf func(collapseUnit) int) int {
	lo, hi := 0, len(units)
	for lo < hi {
		mid := (lo + hi) / 2
		if endOf(units[mid]) <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// firstUnitPastOrAt returns the index of the first unit whose textEnd
// is >= offset (lower_bound, the lookup both text→dom conversions
// start from).
func firstUnitPastOrAt(units []collapseUnit, text int) int {
	lo, hi := 0, len(units)
	for lo < hi {
		mid := (lo + hi) / 2
		if units[mid].textEnd < text {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(units) {
		lo = len(units) - 1
	}
	return lo
}

// ---------------------------------------------------------------------------
// The collapser (inline_items_builder.cc)
// ---------------------------------------------------------------------------

// whiteSpaceSegment is one maximal source range sharing a white-space
// mode (the per-item styles of AppendText's dispatch,
// inline_items_builder.cc:683-689: preserve modes →
// AppendPreserveWhitespace, preserve-breaks → AppendPreserveNewline,
// else AppendCollapseWhitespace). The paragraph collector produces one
// segment per inline element; the single-element paths produce one.
type whiteSpaceSegment struct {
	start, end int
	mode       WhiteSpace
}

// whitespaceCollapser folds source text into display text under the
// white-space modes of its segments (the InlineItemsBuilder state
// machine, one output document).
type whitespaceCollapser struct {
	sb    strings.Builder
	units []collapseUnit
	// endsKeptSpace is the builder's kCollapsible end state: the output
	// so far ends with a kept collapsed space or a forced break, so a
	// following leading space run folds into it and is removed. It is
	// initialized true: an empty paragraph is the "remove leading
	// spaces" state (inline_items_builder.cc:887-889).
	endsKeptSpace bool
	// boxes are the atomic inline insertion positions of the document
	// being folded (nil for the single-element paths, which never
	// contain a box). A box is the builder's kNotCollapsible last item
	// once it is passed, so the trailing-space removals consult it.
	boxes []int
	// pos is the source offset the collapser has folded up to.
	pos int
}

func newWhitespaceCollapser() *whitespaceCollapser {
	return &whitespaceCollapser{endsKeptSpace: true}
}

// emitIdentity appends a source range verbatim (an identity unit).
func (c *whitespaceCollapser) emitIdentity(text string, start, end int) {
	if end <= start {
		return
	}
	c.sb.WriteString(text[start:end])
	c.units = append(c.units, collapseUnit{
		domStart:  start,
		domEnd:    end,
		textStart: c.sb.Len() - (end - start),
		textEnd:   c.sb.Len(),
	})
}

// emitSpace emits the single collapsed space for a run's first source
// character (an identity unit of one byte, whatever that byte was —
// GetCollapsedSpaceChar always emits U+0020, inline_items_builder.cc:
// 210-215).
func (c *whitespaceCollapser) emitSpace(dom int) {
	c.sb.WriteByte(' ')
	c.units = append(c.units, collapseUnit{
		domStart:  dom,
		domEnd:    dom + 1,
		textStart: c.sb.Len() - 1,
		textEnd:   c.sb.Len(),
		keptSpace: true,
	})
}

// emitCollapsed records a removed source range (a collapsed unit).
func (c *whitespaceCollapser) emitCollapsed(start, end int) {
	if end <= start {
		return
	}
	at := c.sb.Len()
	c.units = append(c.units, collapseUnit{
		domStart:  start,
		domEnd:    end,
		textStart: at,
		textEnd:   at,
	})
}

// emitNewline emits a preserved newline as a forced break (an identity
// unit; the kCollapsible "pretend" state follows, so leading spaces of
// the next line collapse away — AppendForcedBreak,
// inline_items_builder.cc:1190-1196).
func (c *whitespaceCollapser) emitNewline(dom int) {
	c.sb.WriteByte('\n')
	c.units = append(c.units, collapseUnit{
		domStart:  dom,
		domEnd:    dom + 1,
		textStart: c.sb.Len() - 1,
		textEnd:   c.sb.Len(),
	})
	c.endsKeptSpace = true
}

// removeTrailingKeptSpaceBefore removes the output's trailing kept
// space (the unit collapse of RemoveTrailingCollapsibleSpace,
// 1379-1410: only a kept TEXT space, never the forced break the
// kControl guard protects). upto is the source offset the removal
// happens at: a forced break's own offset in pre-line, or the end of
// the document at finish. An atomic box sitting after the space's
// source byte and at or before upto is the builder's last item to
// collapse with (kNotCollapsible), so the space is the item before
// the box and survives (LastItemToCollapseWith stops at the box).
func (c *whitespaceCollapser) removeTrailingKeptSpaceBefore(upto int) {
	// The output's last character is the last unit that produces one;
	// trailing collapsed units (the removed tail of the final space run)
	// sit after it and must be walked past.
	text := c.sb.String()
	for i := len(c.units) - 1; i >= 0; i-- {
		u := c.units[i]
		if u.collapsed() {
			continue
		}
		if u.keptSpace && u.textEnd == len(text) &&
			!c.boxAfter(u.domStart, upto) {
			c.units[i] = collapseUnit{
				domStart:  u.domStart,
				domEnd:    u.domEnd,
				textStart: u.textStart,
				textEnd:   u.textStart,
			}
			// The units after the removed space (the removed tail of the
			// same run) shift down onto the truncated text.
			for j := i + 1; j < len(c.units); j++ {
				c.units[j].textStart--
				c.units[j].textEnd--
			}
			c.sb.Reset()
			c.sb.WriteString(text[:u.textStart])
			c.endsKeptSpace = false
		}
		return
	}
}

// append folds one segment (inline_items_builder.cc's AppendText
// dispatch): preserve modes copy the text (dropping carriage returns and
// form feeds), pre-line splits at newlines, and the collapsing modes run
// the space-run state machine. boxes are the source positions of atomic
// inline boxes — separators between the records; a sub-run or trailing
// removal that would cross one stops at the box instead (the box is the
// kNotCollapsible last item to collapse with, inline_items_builder.cc:
// 218-228 and 1281-1289).
func (c *whitespaceCollapser) append(text string, seg whiteSpaceSegment, boxes []int) {
	defer func() { c.pos = seg.end }()
	switch {
	case seg.mode.whiteSpacePreservesSpaces():
		c.appendPreserved(text, seg)
	case seg.mode == WhiteSpacePreLine:
		c.appendPreLine(text, seg, boxes)
	default:
		c.appendCollapsed(text, seg, true, boxes)
	}
}

// appendPreserved copies a pre/pre-wrap segment verbatim, dropping
// carriage returns and form feeds (ShouldIgnore, inline_items_builder.
// cc:163-168, dropped by the line breaker's kCarriageReturn/kFormFeed
// arms, line_breaker.cc:2998-3005). The segment ends in the
// not-collapsible state (AppendPreserveWhitespace items never set an
// end collapse type).
func (c *whitespaceCollapser) appendPreserved(text string, seg whiteSpaceSegment) {
	i := seg.start
	for i < seg.end {
		if text[i] == '\r' || text[i] == 0x0c {
			c.emitCollapsed(i, i+1)
			i++
			continue
		}
		j := i
		for j < seg.end && text[j] != '\r' && text[j] != 0x0c {
			j++
		}
		c.emitIdentity(text, i, j)
		i = j
	}
	c.endsKeptSpace = false
}

// appendPreLine folds a pre-line segment (AppendPreserveNewline,
// inline_items_builder.cc:1150-1175): the text splits at newlines, each
// newline removes the trailing collapsible space before it and becomes
// a forced break, and the pieces between newlines collapse their
// spaces.
func (c *whitespaceCollapser) appendPreLine(text string, seg whiteSpaceSegment, boxes []int) {
	i := seg.start
	for i < seg.end {
		if text[i] == '\n' {
			c.removeTrailingKeptSpaceBefore(i)
			c.emitNewline(i)
			i++
			continue
		}
		j := i
		for j < seg.end && text[j] != '\n' {
			j++
		}
		c.appendCollapsed(text, whiteSpaceSegment{start: i, end: j, mode: WhiteSpacePreLine}, true, boxes)
		i = j
	}
}

// appendCollapsed folds a collapsing-mode segment (AppendCollapse
// Whitespace, inline_items_builder.cc:798-965): non-space text is
// identity, and each maximal run of collapsible spaces (space, tab,
// line feed, carriage return — character.h:179-182) collapses to one
// space or is removed. leading selects the item's leading-run logic for
// the segment's first run; a run that starts at an atomic box always
// consults the builder's state (the box is opaque, so the run is the
// next item's leading run).
func (c *whitespaceCollapser) appendCollapsed(text string, seg whiteSpaceSegment, leading bool, boxes []int) {
	i := seg.start
	for i < seg.end {
		if isCollapsibleSpaceByte(text[i]) {
			j := i
			for j < seg.end && isCollapsibleSpaceByte(text[j]) {
				j++
			}
			c.processSpaceRun(i, j, leading && i == seg.start, boxes)
			i = j
			continue
		}
		j := i
		for j < seg.end && !isCollapsibleSpaceByte(text[j]) {
			j++
		}
		c.emitIdentity(text, i, j)
		c.endsKeptSpace = false
		i = j
	}
}

// processSpaceRun folds one maximal collapsible run
// (AppendCollapseWhitespace's space-run branches): the run splits at
// atomic box positions (a box sits between two source bytes, so a run
// spanning the box position is two records' runs); a sub-run that
// starts at an atomic box is the next record's leading run AFTER the
// box — the box is the builder's last item to collapse with and ends
// kNotCollapsible (an atomic inline is appended through Append, not
// AppendOpaque), so the sub-run does NOT consult the state and keeps
// its one space — while the segment's own leading run (no box before
// it) does consult it, and is removed when the output ends with a kept
// space or forced break (or the paragraph is still empty).
func (c *whitespaceCollapser) processSpaceRun(start, end int, consultsState bool, boxes []int) {
	p := start
	for p < end {
		q := end
		for _, bx := range boxes {
			if bx > p && bx < q {
				q = bx
			}
		}
		// A sub-run starting at a box position follows the atomic box:
		// the box is the last item (kNotCollapsible), so this sub-run
		// never folds into the previous kept space.
		consults := consultsState && !boxAt(boxes, p)
		if consults && c.endsKeptSpace {
			c.emitCollapsed(p, q)
			// The state is unchanged: the output still ends with the
			// space this run folded into (or the paragraph is still
			// empty).
		} else {
			c.emitSpace(p)
			c.emitCollapsed(p+1, q)
			c.endsKeptSpace = true
		}
		consultsState = false
		p = q
	}
}

// boxAt reports whether an atomic box is inserted at the source
// position (between text[pos-1] and text[pos]).
func boxAt(boxes []int, pos int) bool {
	return slices.Contains(boxes, pos)
}

// boxAfter reports whether an atomic box sits after the source byte
// spacePos and at or before upto: the box is then between the kept
// space and the offset the trailing-space removal happens at, so it is
// the last item to collapse with and the space is not trailing
// (LastItemToCollapseWith stops at a kNotCollapsible atomic inline;
// AppendAtomicInline even restores a removed space before a box,
// inline_items_builder.cc:1281-1289).
func (c *whitespaceCollapser) boxAfter(spacePos, upto int) bool {
	for _, bx := range c.boxes {
		if bx > spacePos && bx <= upto {
			return true
		}
	}
	return false
}

// isCollapsibleSpaceByte reports the collapsible white space of the
// collapsing modes (unicode::IsCollapsibleSpace over ASCII,
// platform/text/character.h:179-182): space, line feed, tab and
// carriage return. A form feed is NOT collapsible (it stays a regular
// character, Blink's reading of the white-space processing rules).
func isCollapsibleSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

// finish closes the document (ExitBlock, inline_items_builder.cc:
// 1624-1638): the final trailing space is removed — never a forced
// break.
func (c *whitespaceCollapser) finish() {
	c.removeTrailingKeptSpaceBefore(c.pos)
}

// result returns the display text and its byte map; the map is nil when
// the collapse changed nothing (the identity fast path).
func (c *whitespaceCollapser) result(sourceLen int) (string, *collapseMap) {
	text := c.sb.String()
	identity := len(text) == sourceLen
	if identity {
		for _, u := range c.units {
			if u.collapsed() || u.domStart != u.textStart || u.domEnd != u.textEnd {
				identity = false
				break
			}
		}
	}
	if identity {
		return text, nil
	}
	return text, &collapseMap{units: c.units}
}

// ---------------------------------------------------------------------------
// The entry points
// ---------------------------------------------------------------------------

// collapseStyledRuns folds one StyledText document and its covering
// runs under a white-space mode, returning the display text, the
// remapped runs and the byte map the TextLayout queries map through
// (browser-core 38).
func collapseStyledRuns(text string, runs []TextRun, mode WhiteSpace) (string, []TextRun, *collapseMap) {
	if !mode.whiteSpaceCollapsesSpaces() && !mode.whiteSpacePreservesSpaces() {
		return text, runs, nil
	}
	display, m := collapseWhiteSpace(text, mode)
	if m == nil {
		return text, runs, nil
	}
	return display, remapRunsThroughCollapse(runs, m, len(display)), m
}

// collapseElementText folds one element's display text and its
// covering runs under a white-space mode (the Text leaf and
// StyledText's element paths, browser-core 38): the mode's collapse
// over the whole text, then the runs remapped onto the display text.
// collapseElementTextWithMap is the same fold returning the byte map
// (browser-core 20: the Text leaf's selection participant answers its
// geometry queries in SOURCE offsets through it).
func collapseElementText(text string, runs []TextRun, mode WhiteSpace) (string, []TextRun) {
	display, runs, _ := collapseElementTextWithMap(text, runs, mode)
	return display, runs
}

func collapseElementTextWithMap(text string, runs []TextRun, mode WhiteSpace) (string, []TextRun, *collapseMap) {
	return collapseStyledRuns(text, runs, mode)
}

// collapseParagraphDocument folds a collected inline document in
// place (browser-core 38): the text under the per-record white-space
// modes (the textStyles' ranges, in collection order — one record per
// inline element, like Blink's per-LayoutText items), the atomic boxes
// as opaque separators, then the remapped covering runs, style ranges,
// box indices and span byte ranges. A record whose text folds away
// entirely keeps its (empty) style range: splitInlineSegments skips
// empty ranges, and an empty document answers no rows.
func collapseParagraphDocument(document *inlineDocument, ambient WhiteSpace) {
	if len(document.text) == 0 {
		return
	}
	segments := make([]whiteSpaceSegment, 0, len(document.textStyles))
	for _, style := range document.textStyles {
		if style.Range.End <= style.Range.Start {
			continue
		}
		segments = append(segments, whiteSpaceSegment{
			start: style.Range.Start,
			end:   style.Range.End,
			mode:  style.WhiteSpace,
		})
	}
	boxes := make([]int, len(document.boxes))
	for i, box := range document.boxes {
		boxes[i] = box.Index
	}
	// The boxes keep the document's order (the collapse's remap answers
	// per input position); duplicates at one position are two atomic
	// elements sharing an insertion point, and both map to the same
	// display offset.
	display, newBoxes, m := collapseSegments(document.text, segments, boxes, ambient)
	if m == nil {
		return
	}
	document.runs = remapRunsThroughCollapse(document.runs, m, len(display))
	for i := range document.textStyles {
		document.textStyles[i].Range = TextRange{
			Start: m.domToText(document.textStyles[i].Range.Start),
			End:   m.domToText(document.textStyles[i].Range.End),
		}
	}
	for i, bx := range newBoxes {
		document.boxes[i].Index = bx
	}
	for i := range document.spans {
		document.spans[i].textRange = TextRange{
			Start: m.domToText(document.spans[i].textRange.Start),
			End:   m.domToText(document.spans[i].textRange.End),
		}
	}
	document.text = display
}

// collapseWhiteSpace folds one text under one mode — the element paths
// (the Text leaf's measured cell and StyledText's TextLayout, which hold
// exactly one segment). It returns the display text and its byte map
// (nil for the identity).
func collapseWhiteSpace(text string, mode WhiteSpace) (string, *collapseMap) {
	c := newWhitespaceCollapser()
	c.append(text, whiteSpaceSegment{start: 0, end: len(text), mode: mode}, nil)
	c.finish()
	return c.result(len(text))
}

// collapseSegments folds a multi-segment document (the inline
// paragraph's collected records) and remaps the atomic boxes' insertion
// indices through the map (a box at source position p sits at the
// display offset domToText(p), whatever collapsed around it). It
// returns the display text, the boxes' new indices and the map.
func collapseSegments(text string, segments []whiteSpaceSegment, boxes []int, ambient WhiteSpace) (string, []int, *collapseMap) {
	sorted := slices.Clone(segments)
	slices.SortStableFunc(sorted, func(a, b whiteSpaceSegment) int {
		return a.start - b.start
	})
	c := newWhitespaceCollapser()
	c.boxes = boxes
	covered := 0
	fold := func(seg whiteSpaceSegment) {
		c.append(text, seg, boxes)
	}
	for _, seg := range sorted {
		if seg.start > covered {
			// A gap between the records' ranges (a document with boxes
			// but no covering textStyles) inherits the ambient mode.
			fold(whiteSpaceSegment{start: covered, end: seg.start, mode: ambient})
		}
		fold(seg)
		covered = max(covered, seg.end)
	}
	if covered < len(text) {
		fold(whiteSpaceSegment{start: covered, end: len(text), mode: ambient})
	}
	c.finish()
	display, m := c.result(len(text))
	if m == nil {
		return display, boxes, nil
	}
	remapped := make([]int, len(boxes))
	for i, bx := range boxes {
		remapped[i] = m.domToText(bx)
	}
	return display, remapped, m
}

// remapRunsThroughCollapse rebuilds the covering runs of the display
// text from the source-covering runs: each identity unit's source range
// slices its runs onto the unit's display range (identity units map
// byte-for-byte, so the lengths match), and collapsed units contribute
// nothing. Adjacent pieces of the same style merge, so the remap cannot
// grow the run list past the source's.
func remapRunsThroughCollapse(runs []TextRun, m *collapseMap, displayLen int) []TextRun {
	if m == nil {
		return runs
	}
	var out []TextRun
	for _, u := range m.units {
		if u.collapsed() {
			continue
		}
		pieces := sliceRunsByRange(runs, u.domStart, u.domEnd)
		for _, piece := range pieces {
			// An identity unit maps byte-for-byte, so each piece's
			// source slice length is already its display length; pieces
			// of one unit may straddle run boundaries, and each keeps
			// its own length (their sum is the unit's).
			if n := len(out); n > 0 && sameShapingRun(out[n-1], piece) {
				out[n-1].Len += piece.Len
				continue
			}
			out = append(out, piece)
		}
	}
	if len(out) == 0 && len(runs) > 0 {
		// A display text that collapsed away entirely (or runs that
		// failed to cover the source) still shape as one run of the
		// first font — a zero-length run keeps ShapeText's "at least
		// one run" contract (defensive, like sliceInlineRuns' fallback).
		out = []TextRun{{Len: displayLen, Font: runs[0].Font}}
	}
	return out
}

// sameShapingRun reports whether two runs are interchangeable in a
// covering sequence (every field but Len).
func sameShapingRun(a, b TextRun) bool {
	if !sameFontDescriptor(a.Font, b.Font) || a.Color != b.Color {
		return false
	}
	if !equalPtr(a.BackgroundColor, b.BackgroundColor) ||
		!equalUnderline(a.Underline, b.Underline) ||
		!equalStrikethrough(a.Strikethrough, b.Strikethrough) {
		return false
	}
	if (a.LetterSpacing == nil) != (b.LetterSpacing == nil) {
		return false
	}
	if a.LetterSpacing != nil && *a.LetterSpacing != *b.LetterSpacing {
		return false
	}
	// The browser text fields (browser-core 43): a run with different
	// word-breaking tags shapes differently.
	return a.WordBreak == b.WordBreak && a.OverflowWrap == b.OverflowWrap && a.TextWrap == b.TextWrap
}

// sameFontDescriptor reports whether two descriptors select the same
// font (the slice fields compare by content).
func sameFontDescriptor(a, b FontDescriptor) bool {
	if a.Family != b.Family || a.Weight != b.Weight || a.Style != b.Style {
		return false
	}
	return slices.Equal(a.Features, b.Features) && slices.Equal(a.Fallbacks, b.Fallbacks)
}

// sliceRunsByRange slices covering runs to a source byte range,
// rebasing lengths (inline_real.go sliceInlineRuns' shape, without the
// defensive fallback: the collapse only slices ranges the runs cover).
func sliceRunsByRange(runs []TextRun, start, end int) []TextRun {
	var sliced []TextRun
	cursor := 0
	for _, run := range runs {
		runStart, runEnd := cursor, cursor+run.Len
		cursor = runEnd
		if runEnd <= start {
			continue
		}
		if runStart >= end {
			break
		}
		from := max(start-runStart, 0)
		to := min(end-runStart, run.Len)
		if to <= from {
			continue
		}
		sliced = append(sliced, TextRun{
			Len:             to - from,
			Font:            run.Font,
			Color:           run.Color,
			BackgroundColor: run.BackgroundColor,
			Underline:       run.Underline,
			Strikethrough:   run.Strikethrough,
			LetterSpacing:   run.LetterSpacing,
			// The browser text fields ride the slice (browser-core 43).
			WordBreak:    run.WordBreak,
			OverflowWrap: run.OverflowWrap,
			TextWrap:     run.TextWrap,
		})
	}
	return sliced
}
