package base

// The data half of U:text/node.rs: the parsed block and inline node tree,
// text marks, source segments and the pure text/selection/Markdown methods
// (ticket 27). The rendering half (NodeContext's render fields, the
// render impls and render caches: Paragraph's ParagraphRenderCache,
// CodeBlock's highlight cache, Table's TableDataCache, ImageNode's decoded
// data-URL image) is ticket 28's, in its own files.
//
// Naming (doc.go): pub items keep their names (Span, LinkMark, TextMark,
// ImageNode, CodeBlock, TableData); pub(crate) items stay unexported.
// Table/TableRow/TableCell collide with table.go's components and take the
// text prefix (textTable, textTableRow, textTableCell). The Rust enum
// BlockNode becomes the blockNode interface over one Go type per variant;
// its methods are package functions switching on the variant
// (blockNodeSpan, blockNodeText, ...), mirroring the Rust match arms.
// Option<T> fields are pointers; Range<usize> is gpui.TextRange.
//
// Two items owned by other upstream modules are defined here because the
// parser and the selection mapping need them before ticket 28 lands:
// inlineState (U:text/inline.rs:226-230,300-305, text plus selection) and
// orderedListOrdinal (U:text/utils.rs:11-16); SelectionFormat
// (U:text/state.rs:70-84) likewise.

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/hoangvu12/gpui-go/gpui"
)

// SelectionFormat is the format of selected text and of what copy writes
// (U:text/state.rs:70-84).
type SelectionFormat int

const (
	// SelectionFormatPlain is the rendered text, without any markup.
	SelectionFormatPlain SelectionFormat = iota
	// SelectionFormatSource is the Markdown source of the selection.
	SelectionFormatSource
)

// inlineState is the text a leaf actually rendered and the selection in
// it (U:text/inline.rs:226-230). It is shared between clones of a node, as
// Base shares Arc<Mutex<InlineState>>.
type inlineState struct {
	mu        sync.Mutex
	text      string
	selection *InputSelection
}

// setText saves the rendered text the selection indexes
// (U:text/inline.rs:300-305).
func (s *inlineState) setText(text string) {
	s.mu.Lock()
	s.text = text
	s.mu.Unlock()
}

func (s *inlineState) setSelection(selection *InputSelection) {
	s.mu.Lock()
	s.selection = selection
	s.mu.Unlock()
}

// snapshot returns the text and selection under the lock.
func (s *inlineState) snapshot() (string, *InputSelection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selection == nil {
		return s.text, nil
	}
	sel := *s.selection
	return s.text, &sel
}

// sharedFlag is an Arc<Mutex<bool>> (InlineNode::custom_selection).
type sharedFlag struct {
	mu    sync.Mutex
	value bool
}

func (f *sharedFlag) get() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value
}

func (f *sharedFlag) set(v bool) {
	f.mu.Lock()
	f.value = v
	f.mu.Unlock()
}

// orderedListOrdinal is an ordered-list item's ordinal, defaulting an
// omitted start to one (U:text/utils.rs:11-16).
func orderedListOrdinal(start *uint32, ix int) uint32 {
	s := uint32(1)
	if start != nil {
		s = *start
	}
	add := uint64(ix)
	if ix < 0 || add > 0xFFFFFFFF {
		add = 0xFFFFFFFF
	}
	sum := uint64(s) + add
	if sum > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(sum)
}

// Span is a node's UTF-8 byte range in the document source
// (U:text/node.rs:472-477).
type Span struct {
	Start int
	End   int
}

// LinkMark is a link's target (U:text/node.rs:397-404).
type LinkMark struct {
	URL string
	// Identifier is the reference or footnote identifier, if any.
	Identifier *string
	Title      *string
}

func (l *LinkMark) equal(o *LinkMark) bool {
	if l == nil || o == nil {
		return l == o
	}
	return l.URL == o.URL && optStringEqual(l.Identifier, o.Identifier) && optStringEqual(l.Title, o.Title)
}

func optStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// TextMark is the style of a text run (U:text/node.rs:406-418).
type TextMark struct {
	Bold          bool
	Italic        bool
	Strikethrough bool
	Underline     bool
	Code          bool
	// Highlight is the `<mark>` background; nil means not highlighted.
	Highlight *gpui.Hsla
	Link      *LinkMark
}

// WithBold is TextMark::bold (U:text/node.rs:421-424); builder methods
// sharing a field's name take the With prefix (doc.go).
func (m TextMark) WithBold() TextMark { m.Bold = true; return m }

// WithItalic is TextMark::italic (U:text/node.rs:426-429).
func (m TextMark) WithItalic() TextMark { m.Italic = true; return m }

// WithStrikethrough is TextMark::strikethrough (U:text/node.rs:431-434).
func (m TextMark) WithStrikethrough() TextMark { m.Strikethrough = true; return m }

// WithUnderline is TextMark::underline (U:text/node.rs:436-439).
func (m TextMark) WithUnderline() TextMark { m.Underline = true; return m }

// WithCode is TextMark::code (U:text/node.rs:441-444).
func (m TextMark) WithCode() TextMark { m.Code = true; return m }

// WithHighlight marks the text highlighted with color
// (U:text/node.rs:446-450).
func (m TextMark) WithHighlight(color gpui.Hsla) TextMark { m.Highlight = &color; return m }

// WithLink is TextMark::link (U:text/node.rs:452-455).
func (m TextMark) WithLink(link LinkMark) TextMark { m.Link = &link; return m }

// Merge ORs the flags in and lets other's highlight and link win
// (U:text/node.rs:457-469).
func (m *TextMark) Merge(other TextMark) {
	m.Bold = m.Bold || other.Bold
	m.Italic = m.Italic || other.Italic
	m.Strikethrough = m.Strikethrough || other.Strikethrough
	m.Underline = m.Underline || other.Underline
	m.Code = m.Code || other.Code
	if other.Highlight != nil {
		m.Highlight = other.Highlight
	}
	if other.Link != nil {
		m.Link = other.Link
	}
}

func (m TextMark) equal(o TextMark) bool {
	if m.Bold != o.Bold || m.Italic != o.Italic || m.Strikethrough != o.Strikethrough ||
		m.Underline != o.Underline || m.Code != o.Code {
		return false
	}
	if (m.Highlight == nil) != (o.Highlight == nil) || (m.Highlight != nil && *m.Highlight != *o.Highlight) {
		return false
	}
	return m.Link.equal(o.Link)
}

// textMarkRange is one (Range<usize>, TextMark) entry of InlineNode::marks.
type textMarkRange struct {
	rng  gpui.TextRange
	mark TextMark
}

// ImageNode is an image in the document (U:text/node.rs:479-492). The
// decoded data-URL cache (`embedded`) belongs to the renderer (ticket 28).
type ImageNode struct {
	URL    string
	Link   *LinkMark
	Title  *string
	Alt    *string
	Width  *gpui.DefiniteLength
	Height *gpui.DefiniteLength
	span   *Span
}

// TitleText is ImageNode::title: the title, else the alt text, else empty
// (U:text/node.rs:494-500). Renamed: Go cannot share the Title field's name.
func (i *ImageNode) TitleText() string {
	if i.Title != nil {
		return *i.Title
	}
	if i.Alt != nil {
		return *i.Alt
	}
	return ""
}

// sourceSegment pairs a rendered UTF-8 byte span with its exact Markdown
// source span (U:text/node.rs:543-547).
type sourceSegment struct {
	rendered gpui.TextRange
	source   gpui.TextRange
}

func rangeLen(r gpui.TextRange) int { return max(r.End-r.Start, 0) }

// sourceRangeSelection is how a selection maps to the source
// (U:text/node.rs:549-575).
type sourceRangeSelection struct {
	kind sourceRangeKind
	rng  gpui.TextRange
}

type sourceRangeKind int

const (
	sourceRangeUnselected sourceRangeKind = iota
	sourceRangeMapped
	sourceRangeUnmapped
)

func sourceRangeMappedOf(r gpui.TextRange) sourceRangeSelection {
	return sourceRangeSelection{kind: sourceRangeMapped, rng: r}
}

func (s *sourceRangeSelection) merge(other sourceRangeSelection) {
	switch {
	case other.kind == sourceRangeUnselected:
	case other.kind == sourceRangeUnmapped:
		*s = sourceRangeSelection{kind: sourceRangeUnmapped}
	case s.kind == sourceRangeUnselected:
		*s = other
	case s.kind == sourceRangeMapped:
		s.rng.Start = min(s.rng.Start, other.rng.Start)
		s.rng.End = max(s.rng.End, other.rng.End)
	}
}

func (s sourceRangeSelection) intoRange() (gpui.TextRange, bool) {
	if s.kind == sourceRangeMapped {
		return s.rng, true
	}
	return gpui.TextRange{}, false
}

// sourceRangeForSegments maps a rendered selection through segments; nil
// when any selected byte has no source (U:text/node.rs:577-622).
func sourceRangeForSegments(segments []sourceSegment, selection gpui.TextRange) (gpui.TextRange, bool) {
	mappedStart := func(seg sourceSegment, renderedStart int) int {
		if rangeLen(seg.rendered) == rangeLen(seg.source) {
			return seg.source.Start + max(renderedStart-seg.rendered.Start, 0)
		}
		return seg.source.Start
	}
	mappedEnd := func(seg sourceSegment, renderedEnd int) int {
		if rangeLen(seg.rendered) == rangeLen(seg.source) {
			return seg.source.Start + max(min(renderedEnd, seg.rendered.End)-seg.rendered.Start, 0)
		}
		return seg.source.End
	}
	if selection.Start >= selection.End {
		return gpui.TextRange{}, false
	}
	first := -1
	var renderedEnd, sourceStart, sourceEnd int
	for i, seg := range segments {
		if !(seg.rendered.Start < selection.End && seg.rendered.End > selection.Start) {
			continue
		}
		if first < 0 {
			first = i
			if seg.rendered.Start > selection.Start {
				return gpui.TextRange{}, false
			}
			renderedEnd = seg.rendered.End
			sourceStart = mappedStart(seg, selection.Start)
			sourceEnd = mappedEnd(seg, selection.End)
			continue
		}
		if seg.rendered.Start > renderedEnd {
			return gpui.TextRange{}, false
		}
		renderedEnd = max(renderedEnd, seg.rendered.End)
		sourceEnd = mappedEnd(seg, selection.End)
	}
	if first < 0 || renderedEnd < selection.End {
		return gpui.TextRange{}, false
	}
	return gpui.TextRange{Start: sourceStart, End: sourceEnd}, true
}

// inlineNode is one inline run of a paragraph: text, an image or a custom
// node (U:text/node.rs:624-637).
type inlineNode struct {
	text            string
	image           *ImageNode
	custom          *MarkdownNode
	customSelection *sharedFlag
	marks           []textMarkRange
	sourceSegments  []sourceSegment
	state           *inlineState
}

// newInlineNode is InlineNode::new (U:text/node.rs:1052-1062).
func newInlineNode(text string) inlineNode {
	return inlineNode{text: text, customSelection: &sharedFlag{}, state: &inlineState{}}
}

// inlineNodeCustom is InlineNode::custom (U:text/node.rs:1064-1068).
func inlineNodeCustom(node MarkdownNode) inlineNode {
	n := newInlineNode(node.AsText())
	n.custom = &node
	return n
}

// inlineNodeImage is InlineNode::image (U:text/node.rs:1070-1074).
func inlineNodeImage(image ImageNode) inlineNode {
	n := newInlineNode("")
	n.image = &image
	return n
}

// withMarks is InlineNode::marks (U:text/node.rs:1076-1079).
func (n inlineNode) withMarks(marks []textMarkRange) inlineNode {
	n.marks = marks
	return n
}

// withSourceSegments is InlineNode::source_segments
// (U:text/node.rs:1081-1084).
func (n inlineNode) withSourceSegments(segments []sourceSegment) inlineNode {
	n.sourceSegments = segments
	return n
}

func (n *inlineNode) selectedSourceRange(selection gpui.TextRange) (gpui.TextRange, bool) {
	return sourceRangeForSegments(n.sourceSegments, selection)
}

// wrapWithMark wraps text in the Markdown syntax mark implies, innermost
// first (U:text/node.rs:649-688).
func wrapWithMark(text string, mark TextMark) string {
	if text == "" {
		return ""
	}
	out := text
	if mark.Code {
		out = "`" + out + "`"
	}
	if mark.Italic {
		out = "*" + out + "*"
	}
	if mark.Bold {
		out = "**" + out + "**"
	}
	if mark.Strikethrough {
		out = "~~" + out + "~~"
	}
	if mark.Underline {
		out = "<u>" + out + "</u>"
	}
	if mark.Highlight != nil {
		out = "==" + out + "=="
	}
	if mark.Link != nil {
		if mark.Link.Title != nil {
			out = "[" + out + "](" + mark.Link.URL + " \"" + *mark.Link.Title + "\")"
		} else {
			out = "[" + out + "](" + mark.Link.URL + ")"
		}
	}
	return out
}

// markdownSource collects selected pieces with their marks and serializes
// them once, so neighbouring runs share delimiters
// (U:text/node.rs:690-820).
type markdownSource struct {
	pieces []markdownPiece
}

type markdownPiece struct {
	source string
	layers []TextMark
}

func (s *markdownSource) pushStr(source string) { s.pushMarked(source, TextMark{}) }

func (s *markdownSource) pushMarked(source string, mark TextMark) {
	if source == "" {
		return
	}
	var layers []TextMark
	if mark.Link != nil {
		layers = append(layers, TextMark{Link: mark.Link})
	}
	if mark.Highlight != nil {
		layers = append(layers, TextMark{Highlight: mark.Highlight})
	}
	if mark.Underline {
		layers = append(layers, TextMark{}.WithUnderline())
	}
	if mark.Strikethrough {
		layers = append(layers, TextMark{}.WithStrikethrough())
	}
	if mark.Bold {
		layers = append(layers, TextMark{}.WithBold())
	}
	if mark.Italic {
		layers = append(layers, TextMark{}.WithItalic())
	}
	if mark.Code {
		layers = append(layers, TextMark{}.WithCode())
	}
	s.pieces = append(s.pieces, markdownPiece{source: source, layers: layers})
}

func (s *markdownSource) pushText(text string, marks []textMarkRange, selection gpui.TextRange) {
	start := min(selection.Start, len(text))
	end := min(selection.End, len(text))
	if start >= end {
		return
	}
	cursor := start
	for _, m := range marks {
		lo := max(m.rng.Start, start)
		hi := min(m.rng.End, end)
		if lo >= hi {
			continue
		}
		if cursor < lo {
			s.pushStr(text[cursor:lo])
		}
		s.pushMarked(text[lo:hi], m.mark)
		cursor = hi
	}
	if cursor < end {
		s.pushStr(text[cursor:end])
	}
}

func (s *markdownSource) pushObject(node *inlineNode) {
	var mark TextMark
	for _, layer := range node.marks {
		mark.Merge(layer.mark)
	}
	s.pushMarked(node.custom.toMarkdown(), mark)
}

func (s *markdownSource) isEmpty() bool { return len(s.pieces) == 0 }

func (s *markdownSource) finish() string {
	var write func(pieces []markdownPiece) string
	contains := func(layers []TextMark, mark TextMark) bool {
		for _, l := range layers {
			if l.equal(mark) {
				return true
			}
		}
		return false
	}
	write = func(pieces []markdownPiece) string {
		var out strings.Builder
		offset := 0
		for offset < len(pieces) {
			if len(pieces[offset].layers) == 0 {
				out.WriteString(pieces[offset].source)
				offset++
				continue
			}
			best, bestCount := TextMark{}, -1
			for _, mark := range pieces[offset].layers {
				count := 0
				for _, p := range pieces[offset:] {
					if !contains(p.layers, mark) {
						break
					}
					count++
				}
				if count > bestCount {
					best, bestCount = mark, count
				}
			}
			group := pieces[offset : offset+bestCount]
			for i := range group {
				kept := group[i].layers[:0:0]
				for _, l := range group[i].layers {
					if !l.equal(best) {
						kept = append(kept, l)
					}
				}
				group[i].layers = kept
			}
			out.WriteString(wrapWithMark(write(group), best))
			offset += bestCount
		}
		return out.String()
	}
	return write(s.pieces)
}

// runSelection is how a selection covers one rendered run
// (U:text/node.rs:822-829).
type runSelection struct {
	emitted bool
	atStart bool
	atEnd   bool
}

// runChild is one (offset in the run's text, child) entry of a run.
type runChild struct {
	offset int
	child  *inlineNode
}

// emitRun emits the selected part of one rendered run, preceded by the
// images the selection ran into (U:text/node.rs:831-874).
func emitRun(state *inlineState, run []runChild, pendingImages *[]string, out *markdownSource) runSelection {
	var selected runSelection
	text, selection := state.snapshot()
	if selection == nil || selection.Start >= selection.End {
		return selected
	}
	selected.atStart = selection.Start == 0
	selected.atEnd = selection.End >= len(text)
	for _, rc := range run {
		end := rc.offset + len(rc.child.text)
		lo := max(selection.Start, rc.offset)
		hi := min(selection.End, end)
		if lo >= hi {
			continue
		}
		if !selected.emitted {
			if selected.atStart {
				out.pushStr(strings.Join(*pendingImages, ""))
			}
			*pendingImages = (*pendingImages)[:0]
		}
		selected.emitted = true
		out.pushText(rc.child.text, rc.child.marks, gpui.TextRange{Start: lo - rc.offset, End: hi - rc.offset})
	}
	return selected
}

// imageMarkdown is an inline image's Markdown, `![alt](url "title")`
// (U:text/node.rs:876-884).
func imageMarkdown(image *ImageNode) string {
	alt := ""
	if image.Alt != nil {
		alt = *image.Alt
	}
	title := ""
	if image.Title != nil {
		title = " \"" + *image.Title + "\""
	}
	return "![" + alt + "](" + image.URL + title + ")"
}

// paragraph is a run of inline nodes; it keeps the selection state of the
// run after its last image (U:text/node.rs:1091-1108). The render cache
// is ticket 28's.
type paragraph struct {
	span     *Span
	children []inlineNode
	// linkRefs maps a reference identifier to its URL.
	linkRefs map[string]string
	state    *inlineState
}

// newParagraph is Paragraph::new (U:text/node.rs:1160-1168).
func newParagraph(text string) *paragraph {
	return &paragraph{children: []inlineNode{newInlineNode(text)}, state: &inlineState{}}
}

// defaultParagraph is Paragraph::default().
func defaultParagraph() *paragraph {
	return &paragraph{state: &inlineState{}}
}

// selectedText is the rendered text of the selection
// (U:text/node.rs:1231-1255).
func (p *paragraph) selectedText() string {
	var text strings.Builder
	for i := range p.children {
		c := &p.children[i]
		t, sel := c.state.snapshot()
		if sel != nil {
			text.WriteString(t[sel.Start:sel.End])
		}
		if c.custom != nil && c.customSelection.get() {
			text.WriteString(c.custom.AsText())
		}
	}
	if t, sel := p.state.snapshot(); sel != nil {
		text.WriteString(t[sel.Start:sel.End])
	}
	return text.String()
}

// selectedSourceRange maps the rendered selection to its exact Markdown
// source range (U:text/node.rs:1257-1387).
func (p *paragraph) selectedSourceRange() sourceRangeSelection {
	var selected sourceRangeSelection
	var run []runChild
	offset := 0
	var pendingImages []*Span
	entersImage := true

	includeRun := func(state *inlineState, run []runChild) (sourceRangeSelection, runSelection) {
		text, selection := state.snapshot()
		if selection == nil || selection.Start >= selection.End {
			return sourceRangeSelection{}, runSelection{}
		}
		rs := runSelection{atStart: selection.Start == 0, atEnd: selection.End >= len(text)}
		var mapped sourceRangeSelection
		renderedEnd := selection.Start
		for _, rc := range run {
			end := rc.offset + len(rc.child.text)
			lo := max(selection.Start, rc.offset)
			hi := min(selection.End, end)
			if lo >= hi {
				continue
			}
			rs.emitted = true
			if lo > renderedEnd {
				return sourceRangeSelection{kind: sourceRangeUnmapped}, rs
			}
			r, ok := rc.child.selectedSourceRange(gpui.TextRange{Start: lo - rc.offset, End: hi - rc.offset})
			if !ok {
				return sourceRangeSelection{kind: sourceRangeUnmapped}, rs
			}
			mapped.merge(sourceRangeMappedOf(r))
			renderedEnd = max(renderedEnd, hi)
		}
		if renderedEnd < selection.End {
			return sourceRangeSelection{kind: sourceRangeUnmapped}, rs
		}
		return mapped, rs
	}
	mergeImages := func(selected *sourceRangeSelection, images *[]*Span) {
		for _, span := range *images {
			if span != nil {
				selected.merge(sourceRangeMappedOf(gpui.TextRange{Start: span.Start, End: span.End}))
			} else {
				selected.merge(sourceRangeSelection{kind: sourceRangeUnmapped})
			}
		}
		*images = (*images)[:0]
	}

	for i := range p.children {
		child := &p.children[i]
		if child.custom != nil {
			runRange, rs := includeRun(child.state, run)
			if rs.emitted && rs.atStart {
				mergeImages(&selected, &pendingImages)
			}
			selected.merge(runRange)
			if child.customSelection.get() {
				if len(run) == 0 || (rs.emitted && rs.atEnd) {
					mergeImages(&selected, &pendingImages)
				}
				if r, ok := child.custom.SourceRange(); ok {
					selected.merge(sourceRangeMappedOf(r))
				} else {
					selected.merge(sourceRangeSelection{kind: sourceRangeUnmapped})
				}
				entersImage = true
			} else {
				entersImage = false
			}
			pendingImages = pendingImages[:0]
			run = run[:0]
			offset = 0
			continue
		}
		if child.image != nil {
			runBefore := len(run) > 0
			runRange, rs := includeRun(child.state, run)
			if rs.emitted && rs.atStart {
				mergeImages(&selected, &pendingImages)
			}
			selected.merge(runRange)
			if runBefore {
				entersImage = rs.emitted && rs.atEnd
			}
			if entersImage {
				pendingImages = append(pendingImages, child.image.span)
			} else {
				pendingImages = pendingImages[:0]
			}
			run = run[:0]
			offset = 0
			continue
		}
		run = append(run, runChild{offset: offset, child: child})
		offset += len(child.text)
	}

	trailingRange, trailing := includeRun(p.state, run)
	if trailing.emitted && trailing.atStart {
		mergeImages(&selected, &pendingImages)
	}
	selected.merge(trailingRange)
	if !trailing.emitted && entersImage && selected.kind != sourceRangeUnselected {
		mergeImages(&selected, &pendingImages)
	}
	return selected
}

// selectedSource reconstructs the Markdown source of the selection, walking
// the children in the runs the renderer stores selections for
// (U:text/node.rs:1389-1463).
func (p *paragraph) selectedSource() string {
	var source markdownSource
	var pendingImages []string
	var run []runChild
	offset := 0
	entersImage := true

	for i := range p.children {
		child := &p.children[i]
		if child.custom != nil {
			selected := emitRun(child.state, run, &pendingImages, &source)
			objectSelected := child.customSelection.get()
			if objectSelected {
				if len(run) == 0 || (selected.emitted && selected.atEnd) {
					source.pushStr(strings.Join(pendingImages, ""))
				}
				source.pushObject(child)
			}
			pendingImages = pendingImages[:0]
			entersImage = objectSelected
			run = run[:0]
			offset = 0
			continue
		}
		if child.image == nil {
			run = append(run, runChild{offset: offset, child: child})
			offset += len(child.text)
			continue
		}
		// The run before an image is stored in that image's own state.
		runBefore := len(run) > 0
		selected := emitRun(child.state, run, &pendingImages, &source)
		if runBefore {
			entersImage = selected.emitted && selected.atEnd
		}
		if entersImage {
			pendingImages = append(pendingImages, imageMarkdown(child.image))
		} else {
			pendingImages = pendingImages[:0]
		}
		run = run[:0]
		offset = 0
	}

	trailing := emitRun(p.state, run, &pendingImages, &source)
	// Trailing images have no run after them to flush them.
	if !trailing.emitted && entersImage && !source.isEmpty() {
		source.pushStr(strings.Join(pendingImages, ""))
	}
	return source.finish()
}

// text is the concatenated text of the children (U:text/node.rs:1465-1471).
func (p *paragraph) text() string {
	var b strings.Builder
	for i := range p.children {
		b.WriteString(p.children[i].text)
	}
	return b.String()
}

// hasSelection reports a selection, even an empty one, in any state
// (U:text/node.rs:1473-1484).
func (p *paragraph) hasSelection() bool {
	for i := range p.children {
		c := &p.children[i]
		if _, sel := c.state.snapshot(); sel != nil {
			return true
		}
		if c.customSelection.get() {
			return true
		}
	}
	_, sel := p.state.snapshot()
	return sel != nil
}

// clearSelection clears every stored selection synchronously
// (U:text/node.rs:1486-1499).
func (p *paragraph) clearSelection() {
	for i := range p.children {
		p.children[i].customSelection.set(false)
		p.children[i].state.setSelection(nil)
	}
	p.state.setSelection(nil)
}

// take moves the paragraph out, leaving an empty one
// (U:text/node.rs:1677-1688).
func (p *paragraph) take() *paragraph {
	out := *p
	*p = paragraph{state: &inlineState{}}
	return &out
}

// isImage is always false (U:text/node.rs:1690-1692).
func (p *paragraph) isImage() bool { return false }

// setSpan is Paragraph::set_span (U:text/node.rs:1694-1696).
func (p *paragraph) setSpan(span Span) { p.span = &span }

// pushStr appends an unstyled run carrying a default mark
// (U:text/node.rs:1698-1703).
func (p *paragraph) pushStr(text string) {
	p.children = append(p.children, newInlineNode(text).withMarks([]textMarkRange{{rng: gpui.TextRange{Start: 0, End: len(text)}}}))
}

// push is Paragraph::push (U:text/node.rs:1705-1708).
func (p *paragraph) push(node inlineNode) { p.children = append(p.children, node) }

// pushImage is Paragraph::push_image (U:text/node.rs:1710-1713).
func (p *paragraph) pushImage(image ImageNode) {
	p.children = append(p.children, inlineNodeImage(image))
}

// isEmpty reports no text and no image (U:text/node.rs:1720-1726).
func (p *paragraph) isEmpty() bool {
	for i := range p.children {
		if p.children[i].text != "" || p.children[i].image != nil {
			return false
		}
	}
	return true
}

// textLen is the byte length of the children's text
// (U:text/node.rs:1728-1734).
func (p *paragraph) textLen() int {
	n := 0
	for i := range p.children {
		n += len(p.children[i].text)
	}
	return n
}

// merge appends other's children (U:text/node.rs:1736-1739).
func (p *paragraph) merge(other *paragraph) { p.children = append(p.children, other.children...) }

// toMarkdown serializes the paragraph (U:text/node.rs:2634-2695).
func (p *paragraph) toMarkdown() string {
	hasCustom := false
	for i := range p.children {
		if p.children[i].custom != nil {
			hasCustom = true
			break
		}
	}
	if hasCustom {
		var source markdownSource
		for i := range p.children {
			node := &p.children[i]
			if node.custom != nil {
				source.pushObject(node)
				continue
			}
			source.pushText(node.text, node.marks, gpui.TextRange{Start: 0, End: len(node.text)})
			if node.image != nil {
				source.pushStr(imageMarkdown(node.image))
			}
		}
		return source.finish() + "\n\n"
	}
	var b strings.Builder
	for i := range p.children {
		node := &p.children[i]
		text := node.text
		for _, m := range node.marks {
			slice := node.text[m.rng.Start:m.rng.End]
			if m.mark.Bold {
				text = "**" + slice + "**"
			}
			if m.mark.Italic {
				text = "*" + slice + "*"
			}
			if m.mark.Strikethrough {
				text = "~~" + slice + "~~"
			}
			if m.mark.Code {
				text = "`" + slice + "`"
			}
			if m.mark.Highlight != nil {
				text = "==" + slice + "=="
			}
			if m.mark.Link != nil {
				text = "[" + slice + "](" + m.mark.Link.URL + ")"
			}
		}
		if node.image != nil {
			text += imageMarkdown(node.image)
		}
		b.WriteString(text)
	}
	return b.String() + "\n\n"
}

// columnumnAlign is a table column's alignment; the name keeps upstream's
// spelling (U:text/node.rs:1646-1663).
type columnumnAlign int

const (
	columnAlignLeft columnumnAlign = iota
	columnAlignCenter
	columnAlignRight
)

// columnAlignFromMdast is From<mdast::AlignKind> (U:text/node.rs:1654-1663).
func columnAlignFromMdast(a MdastAlignKind) columnumnAlign {
	switch a {
	case MdastAlignCenter:
		return columnAlignCenter
	case MdastAlignRight:
		return columnAlignRight
	default:
		return columnAlignLeft
	}
}

func (a columnumnAlign) delimiter() string {
	switch a {
	case columnAlignCenter:
		return ":-:"
	case columnAlignRight:
		return "--:"
	default:
		return ":--"
	}
}

// textTable is a parsed table (U:text/node.rs:1502-1510); its TableData
// cache is ticket 28's.
type textTable struct {
	children     []textTableRow
	columnAligns []columnumnAlign
	span         *Span
}

// textTableRow is node::TableRow (U:text/node.rs:1665-1668).
type textTableRow struct {
	children []textTableCell
}

// textTableCell is node::TableCell (U:text/node.rs:1670-1674).
type textTableCell struct {
	children *paragraph
	width    *gpui.DefiniteLength
}

// TableData is the plain snapshot of a table handed to the table actions
// hook (U:text/node.rs:1541-1559).
type TableData struct {
	// Headers are the first row's cells as plain text.
	Headers []string
	// Rows are the rows after the header, possibly ragged while streaming.
	Rows [][]string
	// Markdown is the table serialized back to GFM pipe-table Markdown.
	Markdown string
	// Span is the table's byte range in the Markdown source.
	Span *gpui.TextRange
}

// columnAlign is Table::column_align (U:text/node.rs:1562-1564).
func (t *textTable) columnAlign(index int) columnumnAlign {
	if index < len(t.columnAligns) {
		return t.columnAligns[index]
	}
	return columnAlignLeft
}

func (t *textTable) delimiterRow(cells int) string {
	aligns := make([]string, cells)
	for ix := range aligns {
		aligns[ix] = t.columnAlign(ix).delimiter()
	}
	return "| " + strings.Join(aligns, " | ") + " |"
}

// toMarkdown serializes the table back to a GFM pipe table
// (U:text/node.rs:1566-1607).
func (t *textTable) toMarkdown() string {
	var lines []string
	for rowIx, row := range t.children {
		cells := make([]string, len(row.children))
		for i, cell := range row.children {
			c := strings.TrimSpace(cell.children.toMarkdown())
			c = strings.ReplaceAll(c, "\n", " ")
			cells[i] = strings.ReplaceAll(c, "|", "\\|")
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		if rowIx == 0 {
			lines = append(lines, t.delimiterRow(len(row.children)))
		}
	}
	return strings.Join(lines, "\n")
}

// tableData snapshots the table (U:text/node.rs:1609-1625).
func (t *textTable) tableData() TableData {
	rowText := func(row textTableRow) []string {
		out := make([]string, len(row.children))
		for i, cell := range row.children {
			out[i] = strings.TrimSpace(cell.children.text())
		}
		return out
	}
	data := TableData{Headers: []string{}, Rows: [][]string{}, Markdown: t.toMarkdown()}
	if len(t.children) > 0 {
		data.Headers = rowText(t.children[0])
		for _, row := range t.children[1:] {
			data.Rows = append(data.Rows, rowText(row))
		}
	}
	if t.span != nil {
		data.Span = &gpui.TextRange{Start: t.span.Start, End: t.span.End}
	}
	return data
}

// tableSelectedSource is the Markdown source of a table's selected cells
// (U:text/node.rs:905-950).
func tableSelectedSource(t *textTable) string {
	cellSource := func(cell textTableCell) string {
		return strings.ReplaceAll(cell.children.selectedSource(), "\n", " ")
	}
	anySelected := false
	for _, row := range t.children {
		for _, cell := range row.children {
			if strings.TrimSpace(cellSource(cell)) != "" {
				anySelected = true
			}
		}
	}
	if !anySelected {
		return ""
	}
	var lines []string
	for rowIx, row := range t.children {
		cells := make([]string, len(row.children))
		for i, cell := range row.children {
			cells[i] = strings.TrimSpace(cellSource(cell))
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		if rowIx == 0 {
			lines = append(lines, t.delimiterRow(len(row.children)))
		}
	}
	return strings.Join(lines, "\n")
}

// CodeBlock is a fenced, indented or math code block
// (U:text/node.rs:1742-1749). Its highlight cache is ticket 28's.
type CodeBlock struct {
	lang           *string
	state          *inlineState
	sourceSegments []sourceSegment
	// Span is the block's byte range in the source.
	Span *Span
}

// equal is CodeBlock's PartialEq: language, code and span
// (U:text/node.rs:1764-1768).
func (c *CodeBlock) equal(o *CodeBlock) bool {
	spanEq := (c.Span == nil) == (o.Span == nil) && (c.Span == nil || *c.Span == *o.Span)
	return optStringEqual(c.lang, o.lang) && c.Code() == o.Code() && spanEq
}

// Lang is the block's language, if any (U:text/node.rs:1771-1774).
func (c *CodeBlock) Lang() (string, bool) {
	if c.lang == nil {
		return "", false
	}
	return *c.lang, true
}

// Code is the block's code (U:text/node.rs:1776-1782).
func (c *CodeBlock) Code() string {
	text, _ := c.state.snapshot()
	return text
}

// CodeBlockFromCode builds a code block that is not tied to a parsed
// document, e.g. to exercise a highlighter (U:text/node.rs:1784-1791).
func CodeBlockFromCode(code string, lang *string) *CodeBlock {
	return newCodeBlock(code, lang, nil)
}

// newCodeBlock is CodeBlock::new (U:text/node.rs:1793-1810).
func newCodeBlock(code string, lang *string, span *Span) *CodeBlock {
	state := &inlineState{}
	state.setText(code)
	return &CodeBlock{lang: lang, state: state, Span: span}
}

// withSourceSegments is CodeBlock::source_segments
// (U:text/node.rs:1812-1815).
func (c *CodeBlock) withSourceSegments(segments []sourceSegment) *CodeBlock {
	c.sourceSegments = segments
	return c
}

// selectedSourceRange maps the code selection to the source
// (U:text/node.rs:1824-1837).
func (c *CodeBlock) selectedSourceRange() sourceRangeSelection {
	_, sel := c.state.snapshot()
	if sel == nil || sel.Start >= sel.End {
		return sourceRangeSelection{}
	}
	if r, ok := sourceRangeForSegments(c.sourceSegments, gpui.TextRange{Start: sel.Start, End: sel.End}); ok {
		return sourceRangeMappedOf(r)
	}
	return sourceRangeSelection{kind: sourceRangeUnmapped}
}

// selectedText is the selected code (U:text/node.rs:1864-1872).
func (c *CodeBlock) selectedText() string {
	text, sel := c.state.snapshot()
	if sel == nil {
		return ""
	}
	return text[sel.Start:sel.End]
}

// selectedSource wraps the selected code in a fence carrying the language
// (U:text/node.rs:1874-1890).
func (c *CodeBlock) selectedSource() string {
	code := c.selectedText()
	if code == "" {
		return ""
	}
	lang := ""
	if c.lang != nil {
		lang = *c.lang
	}
	return "```" + lang + "\n" + strings.TrimRight(code, "\n") + "\n```"
}

// text is the block's code (U:text/node.rs:1892-1897).
func (c *CodeBlock) text() string { return c.Code() }

// hasSelection reports a stored selection (U:text/node.rs:1899-1906).
func (c *CodeBlock) hasSelection() bool {
	_, sel := c.state.snapshot()
	return sel != nil
}

// clearSelection is CodeBlock::clear_selection (U:text/node.rs:1908-1912).
func (c *CodeBlock) clearSelection() { c.state.setSelection(nil) }

// nodeContext is the parse-side state of U:text/node.rs:1984-2004 NodeContext:
// the document offset, the collected link references and the Markdown
// extensions. Ticket 28 adds the render fields (style, hooks, fades,
// highlights, reveal).
type nodeContext struct {
	// offset is the byte offset of the parsed source in the document.
	offset             int
	linkRefs           map[string]LinkMark
	markdownExtensions *MarkdownExtensions
}

// addRef records a link reference definition (U:text/node.rs:2014-2016).
func (cx *nodeContext) addRef(identifier string, link LinkMark) {
	if cx.linkRefs == nil {
		cx.linkRefs = map[string]LinkMark{}
	}
	cx.linkRefs[identifier] = link
}

func (cx *nodeContext) extensions() *MarkdownExtensions {
	if cx.markdownExtensions == nil {
		cx.markdownExtensions = &MarkdownExtensions{}
	}
	return cx.markdownExtensions
}

// blockNode is node::BlockNode (U:text/node.rs:43-95): one Go type per
// variant. *paragraph, *CodeBlock and *textTable are the Paragraph,
// CodeBlock and Table variants.
type blockNode interface{ isBlockNode() }

type (
	// blockRoot is a container, like an HTML div.
	blockRoot struct {
		children []blockNode
		span     *Span
	}
	blockHeading struct {
		level    uint8
		children *paragraph
		span     *Span
	}
	blockBlockquote struct {
		children []blockNode
		span     *Span
	}
	// blockList holds only blockListItems; start is the first ordinal of an
	// ordered list (unset for HTML lists).
	blockList struct {
		children []blockNode
		ordered  bool
		start    *uint32
		span     *Span
	}
	// blockListItem: checked is nil when the item is not a task.
	blockListItem struct {
		children []blockNode
		spread   bool
		checked  *bool
		span     *Span
	}
	// blockCustom is a node produced by MarkdownExtensions.
	blockCustom struct {
		node MarkdownNode
	}
	blockBreak struct {
		html bool
		span *Span
	}
	blockHorizontalRule struct {
		span *Span
	}
	// blockDefinition keeps a link reference definition for to_markdown.
	blockDefinition struct {
		identifier string
		url        string
		title      *string
		span       *Span
	}
	blockUnknown struct{}
)

func (*blockRoot) isBlockNode()           {}
func (*paragraph) isBlockNode()           {}
func (*blockHeading) isBlockNode()        {}
func (*blockBlockquote) isBlockNode()     {}
func (*blockList) isBlockNode()           {}
func (*blockListItem) isBlockNode()       {}
func (*CodeBlock) isBlockNode()           {}
func (*blockCustom) isBlockNode()         {}
func (*textTable) isBlockNode()           {}
func (*blockBreak) isBlockNode()          {}
func (*blockHorizontalRule) isBlockNode() {}
func (*blockDefinition) isBlockNode()     {}
func (blockUnknown) isBlockNode()         {}

// blockTextKind selects which text a traversal collects
// (U:text/node.rs:97-104).
type blockTextKind int

const (
	blockTextAll blockTextKind = iota
	blockTextSelected
	// blockTextSelectedSource reconstructs the selection's Markdown source.
	blockTextSelectedSource
)

// blockNodeIsListItem is BlockNode::is_list_item (U:text/node.rs:107-109).
func blockNodeIsListItem(b blockNode) bool {
	_, ok := b.(*blockListItem)
	return ok
}

// blockNodeCompact unwraps a Root with one child (U:text/node.rs:111-117).
func blockNodeCompact(b blockNode) blockNode {
	if r, ok := b.(*blockRoot); ok && len(r.children) == 1 {
		return blockNodeCompact(r.children[0])
	}
	return b
}

// blockNodeSpan is BlockNode::span (U:text/node.rs:119-136).
func blockNodeSpan(b blockNode) *Span {
	switch v := b.(type) {
	case *blockRoot:
		return v.span
	case *paragraph:
		return v.span
	case *blockHeading:
		return v.span
	case *blockBlockquote:
		return v.span
	case *blockList:
		return v.span
	case *blockListItem:
		return v.span
	case *CodeBlock:
		return v.Span
	case *blockCustom:
		return v.node.span
	case *textTable:
		return v.span
	case *blockBreak:
		return v.span
	case *blockHorizontalRule:
		return v.span
	case *blockDefinition:
		return v.span
	}
	return nil
}

// blockNodeText is BlockNode::text (U:text/node.rs:138-140).
func blockNodeText(b blockNode) string { return blockNodeTextByKind(b, blockTextAll) }

// blockNodeSelectedText is the selected text in format
// (U:text/node.rs:142-151).
func blockNodeSelectedText(b blockNode, format SelectionFormat) string {
	if format == SelectionFormatSource {
		return blockNodeTextByKind(b, blockTextSelectedSource)
	}
	return blockNodeTextByKind(b, blockTextSelected)
}

// blockNodeSelectedSourceRange is BlockNode::selected_source_range
// (U:text/node.rs:153-181).
func blockNodeSelectedSourceRange(b blockNode) sourceRangeSelection {
	var selected sourceRangeSelection
	switch v := b.(type) {
	case *blockRoot:
		for _, c := range v.children {
			selected.merge(blockNodeSelectedSourceRange(c))
		}
	case *blockBlockquote:
		for _, c := range v.children {
			selected.merge(blockNodeSelectedSourceRange(c))
		}
	case *blockList:
		for _, c := range v.children {
			selected.merge(blockNodeSelectedSourceRange(c))
		}
	case *blockListItem:
		for _, c := range v.children {
			selected.merge(blockNodeSelectedSourceRange(c))
		}
	case *paragraph:
		selected = v.selectedSourceRange()
	case *blockHeading:
		selected = v.children.selectedSourceRange()
	case *textTable:
		for _, row := range v.children {
			for _, cell := range row.children {
				selected.merge(cell.children.selectedSourceRange())
			}
		}
	case *CodeBlock:
		selected = v.selectedSourceRange()
	}
	return selected
}

func paragraphTextByKind(p *paragraph, kind blockTextKind) string {
	switch kind {
	case blockTextSelected:
		return p.selectedText()
	case blockTextSelectedSource:
		return p.selectedSource()
	}
	return p.text()
}

// blockNodeTextByKind is BlockNode::text_by_kind (U:text/node.rs:183-323).
func blockNodeTextByKind(b blockNode, kind blockTextKind) string {
	var text strings.Builder
	push := func(s string) {
		if s != "" {
			text.WriteString(s)
			text.WriteByte('\n')
		}
	}
	switch v := b.(type) {
	case *blockRoot:
		push(blockChildrenText(v.children, kind))
	case *paragraph:
		push(paragraphTextByKind(v, kind))
	case *blockHeading:
		blockText := paragraphTextByKind(v.children, kind)
		if blockText != "" {
			// In source mode the heading marker round-trips (`## Title`).
			if kind == blockTextSelectedSource {
				text.WriteString(strings.Repeat("#", int(v.level)))
				text.WriteByte(' ')
			}
			push(blockText)
		}
	case *blockList:
		if kind == blockTextSelectedSource {
			text.WriteString(listSelectedSource(v.children, v.ordered, v.start, ""))
		} else {
			text.WriteString(blockChildrenText(v.children, kind))
		}
	case *blockListItem:
		text.WriteString(blockChildrenText(v.children, kind))
	case *blockBlockquote:
		blockText := blockChildrenText(v.children, kind)
		if blockText != "" {
			if kind == blockTextSelectedSource {
				lines := rustLines(strings.TrimRight(blockText, "\n"))
				for i, line := range lines {
					if line == "" {
						lines[i] = ">"
					} else {
						lines[i] = "> " + line
					}
				}
				text.WriteString(strings.Join(lines, "\n"))
			} else {
				text.WriteString(blockText)
			}
			text.WriteByte('\n')
		}
	case *textTable:
		if kind == blockTextSelectedSource {
			push(tableSelectedSource(v))
		} else {
			var blockText strings.Builder
			for _, row := range v.children {
				var rowTexts []string
				for _, cell := range row.children {
					if kind == blockTextAll {
						rowTexts = append(rowTexts, cell.children.text())
					} else {
						rowTexts = append(rowTexts, cell.children.selectedText())
					}
				}
				if len(rowTexts) > 0 {
					blockText.WriteString(strings.Join(rowTexts, " "))
					blockText.WriteByte('\n')
				}
			}
			push(blockText.String())
		}
	case *CodeBlock:
		switch kind {
		case blockTextAll:
			push(v.text())
		case blockTextSelected:
			push(v.selectedText())
		default:
			push(v.selectedSource())
		}
	case *blockCustom:
		if kind == blockTextAll {
			push(v.node.AsText())
		}
	}
	return text.String()
}

// blockChildrenText is BlockNode::children_text (U:text/node.rs:325-332).
func blockChildrenText(children []blockNode, kind blockTextKind) string {
	var text strings.Builder
	for _, c := range children {
		text.WriteString(blockNodeTextByKind(c, kind))
	}
	return text.String()
}

// blockNodeHasSelection reports a stored selection, even an empty one
// (U:text/node.rs:334-366).
func blockNodeHasSelection(b blockNode) bool {
	switch v := b.(type) {
	case *blockRoot:
		return blocksHaveSelection(v.children)
	case *blockBlockquote:
		return blocksHaveSelection(v.children)
	case *blockList:
		return blocksHaveSelection(v.children)
	case *blockListItem:
		return blocksHaveSelection(v.children)
	case *paragraph:
		return v.hasSelection()
	case *blockHeading:
		return v.children.hasSelection()
	case *textTable:
		for _, row := range v.children {
			for _, cell := range row.children {
				if cell.children.hasSelection() {
					return true
				}
			}
		}
	case *CodeBlock:
		return v.hasSelection()
	}
	return false
}

func blocksHaveSelection(children []blockNode) bool {
	for _, c := range children {
		if blockNodeHasSelection(c) {
			return true
		}
	}
	return false
}

// blockNodeClearSelection is BlockNode::clear_selection
// (U:text/node.rs:368-394).
func blockNodeClearSelection(b blockNode) {
	switch v := b.(type) {
	case *blockRoot:
		blocksClearSelection(v.children)
	case *blockBlockquote:
		blocksClearSelection(v.children)
	case *blockList:
		blocksClearSelection(v.children)
	case *blockListItem:
		blocksClearSelection(v.children)
	case *paragraph:
		v.clearSelection()
	case *blockHeading:
		v.children.clearSelection()
	case *textTable:
		for _, row := range v.children {
			for _, cell := range row.children {
				cell.children.clearSelection()
			}
		}
	case *CodeBlock:
		v.clearSelection()
	}
}

func blocksClearSelection(children []blockNode) {
	for _, c := range children {
		blockNodeClearSelection(c)
	}
}

// listSelectedSource reconstructs a list's selected items with their
// markers, checkboxes and nesting (U:text/node.rs:952-1049).
func listSelectedSource(children []blockNode, ordered bool, start *uint32, indent string) string {
	var out strings.Builder
	itemIx := 0
	for _, child := range children {
		item, ok := child.(*blockListItem)
		if !ok {
			continue
		}
		marker := "- "
		if ordered {
			marker = strconv.FormatUint(uint64(orderedListOrdinal(start, itemIx)), 10) + ". "
		}
		checkbox := ""
		if item.checked != nil {
			if *item.checked {
				checkbox = "[x] "
			} else {
				checkbox = "[ ] "
			}
		}
		childIndent := indent + strings.Repeat(" ", len(marker))

		var content, nested strings.Builder
		for _, sub := range item.children {
			if list, ok := sub.(*blockList); ok {
				nested.WriteString(listSelectedSource(list.children, list.ordered, list.start, childIndent))
			} else {
				content.WriteString(blockNodeTextByKind(sub, blockTextSelectedSource))
			}
		}
		contentText := strings.TrimRight(content.String(), "\n")
		if contentText == "" && nested.Len() == 0 {
			itemIx++
			continue
		}
		if contentText == "" {
			// An item whose only selected content is a nested list.
			out.WriteString(indent)
			out.WriteString(marker)
			out.WriteString(strings.TrimRight(checkbox, " "))
			out.WriteByte('\n')
		} else {
			lines := rustLines(contentText)
			if len(lines) > 0 {
				out.WriteString(indent)
				out.WriteString(marker)
				out.WriteString(checkbox)
				out.WriteString(lines[0])
				out.WriteByte('\n')
			}
			for _, line := range lines[min(1, len(lines)):] {
				out.WriteString(childIndent)
				out.WriteString(line)
				out.WriteByte('\n')
			}
		}
		out.WriteString(nested.String())
		itemIx++
	}
	return out.String()
}

// rustLines is str::lines: split on '\n', drop a trailing '\r' from each
// line and the empty piece after a final line ending.
func rustLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// blockNodeToMarkdown converts the node to Markdown, used by tests and
// round trips (U:text/node.rs:2697-2799).
func blockNodeToMarkdown(b blockNode) string {
	var out string
	switch v := b.(type) {
	case *blockRoot:
		out = blocksToMarkdown(v.children, "\n\n")
	case *paragraph:
		out = v.toMarkdown()
	case *blockHeading:
		out = strings.Repeat("#", int(v.level)) + " " + v.children.toMarkdown()
	case *blockBlockquote:
		lines := rustLines(blocksToMarkdown(v.children, "\n\n"))
		for i, line := range lines {
			lines[i] = "> " + line
		}
		out = strings.Join(lines, "\n")
	case *blockList:
		parts := make([]string, len(v.children))
		for i, child := range v.children {
			prefix := "- "
			if v.ordered {
				prefix = strconv.FormatUint(uint64(orderedListOrdinal(v.start, i)), 10) + ". "
			}
			parts[i] = prefix + blockNodeToMarkdown(child)
		}
		out = strings.Join(parts, "\n")
	case *blockListItem:
		checkbox := ""
		if v.checked != nil {
			if *v.checked {
				checkbox = "[x] "
			} else {
				checkbox = "[ ] "
			}
		}
		out = checkbox + blocksToMarkdown(v.children, "\n")
	case *CodeBlock:
		lang := ""
		if v.lang != nil {
			lang = *v.lang
		}
		out = "```" + lang + "\n" + v.Code() + "\n```"
	case *textTable:
		out = v.toMarkdown()
	case *blockBreak:
		if v.html {
			out = "<br>"
		} else {
			out = "\n"
		}
	case *blockHorizontalRule:
		out = "---"
	case *blockCustom:
		out = v.node.toMarkdown()
	case *blockDefinition:
		if v.title != nil {
			out = "[" + v.identifier + "]: " + v.url + " \"" + *v.title + "\""
		} else {
			out = "[" + v.identifier + "]: " + v.url
		}
	}
	return strings.TrimSpace(out)
}

// Equality. Rust derives or hand-writes PartialEq on the node tree, and the
// TextViewState tests compare reparsed block lists with it
// (U:text/state.rs:1831,1850). Selection state and render caches are not
// part of equality, as upstream.

// optEqual compares two optional values (Option<T>: PartialEq).
func optEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// equal is ImageNode's PartialEq: every field but the decoded image
// (U:text/node.rs:531-541).
func (i *ImageNode) equal(o *ImageNode) bool {
	if i == nil || o == nil {
		return i == o
	}
	return i.URL == o.URL && i.Link.equal(o.Link) && optStringEqual(i.Title, o.Title) &&
		optStringEqual(i.Alt, o.Alt) && optEqual(i.Width, o.Width) && optEqual(i.Height, o.Height) &&
		optEqual(i.span, o.span)
}

// equal is InlineNode's PartialEq: text, image, custom node, marks and
// source segments (U:text/node.rs:639-647).
func (n *inlineNode) equal(o *inlineNode) bool {
	if n.text != o.text || !n.image.equal(o.image) || !slices.Equal(n.sourceSegments, o.sourceSegments) {
		return false
	}
	if (n.custom == nil) != (o.custom == nil) || (n.custom != nil && !n.custom.equal(*o.custom)) {
		return false
	}
	return slices.EqualFunc(n.marks, o.marks, func(a, b textMarkRange) bool {
		return a.rng == b.rng && a.mark.equal(b.mark)
	})
}

// equal is Paragraph's PartialEq: span, children and link references
// (U:text/node.rs:1151-1157).
func (p *paragraph) equal(o *paragraph) bool {
	if p == nil || o == nil {
		return p == o
	}
	return optEqual(p.span, o.span) && maps.Equal(p.linkRefs, o.linkRefs) &&
		slices.EqualFunc(p.children, o.children, func(a, b inlineNode) bool { return a.equal(&b) })
}

// equal is Table's derived PartialEq; the TableData cache always compares
// equal (U:text/node.rs:1502-1510,1529-1533,1665-1674).
func (t *textTable) equal(o *textTable) bool {
	return optEqual(t.span, o.span) && slices.Equal(t.columnAligns, o.columnAligns) &&
		slices.EqualFunc(t.children, o.children, func(a, b textTableRow) bool {
			return slices.EqualFunc(a.children, b.children, func(x, y textTableCell) bool {
				return optEqual(x.width, y.width) && x.children.equal(y.children)
			})
		})
}

// blockNodeEqual is BlockNode's derived PartialEq (U:text/node.rs:43-95).
func blockNodeEqual(a, b blockNode) bool {
	switch x := a.(type) {
	case *blockRoot:
		y, ok := b.(*blockRoot)
		return ok && optEqual(x.span, y.span) && blocksEqual(x.children, y.children)
	case *paragraph:
		y, ok := b.(*paragraph)
		return ok && x.equal(y)
	case *blockHeading:
		y, ok := b.(*blockHeading)
		return ok && x.level == y.level && optEqual(x.span, y.span) && x.children.equal(y.children)
	case *blockBlockquote:
		y, ok := b.(*blockBlockquote)
		return ok && optEqual(x.span, y.span) && blocksEqual(x.children, y.children)
	case *blockList:
		y, ok := b.(*blockList)
		return ok && x.ordered == y.ordered && optEqual(x.start, y.start) && optEqual(x.span, y.span) &&
			blocksEqual(x.children, y.children)
	case *blockListItem:
		y, ok := b.(*blockListItem)
		return ok && x.spread == y.spread && optEqual(x.checked, y.checked) && optEqual(x.span, y.span) &&
			blocksEqual(x.children, y.children)
	case *CodeBlock:
		y, ok := b.(*CodeBlock)
		return ok && x.equal(y)
	case *blockCustom:
		y, ok := b.(*blockCustom)
		return ok && x.node.equal(y.node)
	case *textTable:
		y, ok := b.(*textTable)
		return ok && x.equal(y)
	case *blockBreak:
		y, ok := b.(*blockBreak)
		return ok && x.html == y.html && optEqual(x.span, y.span)
	case *blockHorizontalRule:
		y, ok := b.(*blockHorizontalRule)
		return ok && optEqual(x.span, y.span)
	case *blockDefinition:
		y, ok := b.(*blockDefinition)
		return ok && x.identifier == y.identifier && x.url == y.url && optStringEqual(x.title, y.title) &&
			optEqual(x.span, y.span)
	case blockUnknown:
		_, ok := b.(blockUnknown)
		return ok
	}
	return false
}

// blocksEqual is Vec<BlockNode>: PartialEq.
func blocksEqual(a, b []blockNode) bool { return slices.EqualFunc(a, b, blockNodeEqual) }

func blocksToMarkdown(children []blockNode, sep string) string {
	parts := make([]string, len(children))
	for i, c := range children {
		parts[i] = blockNodeToMarkdown(c)
	}
	return strings.Join(parts, sep)
}
