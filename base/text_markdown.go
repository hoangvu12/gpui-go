package base

// Port of U:text/format/markdown.rs: Markdown source -> mdast
// (text_mdast.go) -> Base's block/inline node tree with exact source
// segments for selection mapping.
//
// Naming: the module's private functions take the markdown prefix
// (markdownParse, markdownParseParagraph, ...): U:text/format/html.rs
// (ticket 30) has same-named helpers (parse, parse_paragraph,
// push_merged, merge_children_with_mark, parse_table_row/cell) that will
// share this package.
//
// Raw HTML goes through U:text/format/html.rs::parse, which ticket 30
// installs as textHTMLParse (text_html.go). On a parse error both HTML
// arms take Base's own error path: a block becomes a plain paragraph of
// its source (U:text/format/markdown.rs:985-990) and an inline tag
// contributes its source to the returned text only
// (U:text/format/markdown.rs:826-832).

import (
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/yuin/goldmark/util"
)

// textHTMLParse is U:text/format/html.rs::parse, installed by ticket 30
// (text_html.go init).
var textHTMLParse func(source string, cx *nodeContext) (*parsedDocument, error)

var errMarkdownNoHTMLParser = errors.New("html: parser not available (U:text/format/html.rs is ticket 30)")

func markdownHTMLParse(source string, cx *nodeContext) (*parsedDocument, error) {
	if textHTMLParse == nil {
		return nil, errMarkdownNoHTMLParser
	}
	return textHTMLParse(source, cx)
}

// markdownParse parses Markdown into a tree of nodes
// (U:text/format/markdown.rs:19-29).
func markdownParse(source string, cx *nodeContext) (*parsedDocument, error) {
	options := cx.extensions().parseOptions()
	root, err := mdastParse(source, options)
	if err != nil {
		return nil, err
	}
	prose := options
	prose.mathText = false
	prose.mathFlow = false
	markdownFlattenUnclaimedMath(root, source, prose, cx)
	return markdownASTToDocument(source, root, cx), nil
}

// markdownFlattenUnclaimedMath re-parses the source of every inline math
// node no plugin claims without the math constructs and, when it holds
// markup, splices the result into the parent, positions shifted back into
// the document. Plain prose keeps the literal node
// (U:text/format/markdown.rs:31-70).
func markdownFlattenUnclaimedMath(node *MdastNode, source string, options mdastParseOptions, cx *nodeContext) {
	children := node.Children
	for ix := 0; ix < len(children); {
		var prose []*MdastNode
		if children[ix].Kind == MdastInlineMath {
			parseCx := newMarkdownParseContext(source, cx.offset)
			if cx.extensions().parseInline(children[ix], parseCx) == nil {
				prose = markdownReparseAsProse(children[ix], source, options)
			}
		}
		if prose != nil {
			children = slices.Replace(children, ix, ix+1, prose...)
			ix += len(prose)
			continue
		}
		markdownFlattenUnclaimedMath(children[ix], source, options, cx)
		ix++
	}
	node.Children = children
}

// markdownReparseAsProse is reparse_as_prose (U:text/format/markdown.rs:72-96).
func markdownReparseAsProse(node *MdastNode, source string, options mdastParseOptions) []*MdastNode {
	if node.Position == nil {
		return nil
	}
	position := *node.Position
	literal, ok := markdownStrGet(source, position.Start.Offset, position.End.Offset)
	if !ok || !markdownMayHoldInlineMarkup(literal) {
		return nil
	}
	root, err := mdastParse(literal, options)
	if err != nil || len(root.Children) != 1 || root.Children[0].Kind != MdastParagraph {
		return nil
	}
	paragraph := root.Children[0]
	allText := true
	for _, child := range paragraph.Children {
		if child.Kind != MdastText {
			allText = false
			break
		}
	}
	if allText {
		return nil
	}
	children := paragraph.Children
	for _, child := range children {
		markdownShiftPositions(child, position.Start)
	}
	return children
}

// markdownMayHoldInlineMarkup is the cheap gate before a re-parse: every
// inline construct starts with one of these bytes or is a GFM autolink
// literal (U:text/format/markdown.rs:98-109).
func markdownMayHoldInlineMarkup(literal string) bool {
	return strings.ContainsAny(literal, "<*_[`~\\!&") ||
		strings.Contains(literal, "://") || strings.Contains(literal, "www.")
}

// markdownShiftPositions moves a re-parsed subtree to origin
// (U:text/format/markdown.rs:111-127).
func markdownShiftPositions(node *MdastNode, origin MdastPoint) {
	if node.Position != nil {
		for _, point := range []*MdastPoint{&node.Position.Start, &node.Position.End} {
			firstLine := point.Line == 1
			point.Offset += origin.Offset
			point.Line += origin.Line - 1
			if firstLine {
				point.Column += origin.Column - 1
			}
		}
	}
	for _, child := range node.Children {
		markdownShiftPositions(child, origin)
	}
}

// markdownInlineGroup is either one node or a run of siblings between a
// paired formatting tag (U:text/format/markdown.rs:129-132).
type markdownInlineGroup struct {
	node  *MdastNode
	mark  TextMark
	nodes []*MdastNode
}

// markdownInlineGroups pairs the raw inline formatting tags this renderer
// knows with their closing tag among the siblings, so `<strong>x</strong>`
// marks its content like `**x**` (U:text/format/markdown.rs:134-155).
func markdownInlineGroups(children []*MdastNode) []markdownInlineGroup {
	groups := make([]markdownInlineGroup, 0, len(children))
	for ix := 0; ix < len(children); {
		if closing, name, ok := markdownInlineHTMLTag(children[ix]); ok && !closing {
			if mark, ok := markdownInlineHTMLMark(name); ok {
				if closeIx, ok := markdownMatchingCloseTag(children, ix, name); ok {
					groups = append(groups, markdownInlineGroup{mark: mark, nodes: children[ix+1 : closeIx]})
					ix = closeIx + 1
					continue
				}
			}
		}
		groups = append(groups, markdownInlineGroup{node: children[ix]})
		ix++
	}
	return groups
}

// markdownInlineHTMLTag reads an inline Html node as a tag: whether it
// closes and its lowercase name (U:text/format/markdown.rs:157-175).
func markdownInlineHTMLTag(node *MdastNode) (closing bool, name string, ok bool) {
	if node.Kind != MdastHTML {
		return false, "", false
	}
	inner := strings.TrimSpace(node.Value)
	if !strings.HasPrefix(inner, "<") || !strings.HasSuffix(inner, ">") || len(inner) < 2 {
		return false, "", false
	}
	inner = inner[1 : len(inner)-1]
	if strings.HasSuffix(inner, "/") {
		return false, "", false
	}
	rest := inner
	if strings.HasPrefix(inner, "/") {
		closing, rest = true, inner[1:]
	}
	end := 0
	for end < len(rest) && util.IsAlphaNumeric(rest[end]) {
		end++
	}
	if end == 0 {
		return false, "", false
	}
	return closing, strings.ToLower(rest[:end]), true
}

// markdownInlineHTMLMark is the mark a formatting tag applies
// (U:text/format/markdown.rs:177-185).
func markdownInlineHTMLMark(name string) (TextMark, bool) {
	switch name {
	case "strong", "b":
		return TextMark{}.WithBold(), true
	case "em", "i":
		return TextMark{}.WithItalic(), true
	case "u":
		return TextMark{}.WithUnderline(), true
	case "s", "del", "strike":
		return TextMark{}.WithStrikethrough(), true
	}
	return TextMark{}, false
}

// markdownMatchingCloseTag finds the sibling closing the tag opened at
// open, counting nested opens (U:text/format/markdown.rs:187-202).
func markdownMatchingCloseTag(children []*MdastNode, open int, name string) (int, bool) {
	depth := 0
	for ix := open + 1; ix < len(children); ix++ {
		closing, tag, ok := markdownInlineHTMLTag(children[ix])
		if !ok || tag != name {
			continue
		}
		if !closing {
			depth++
			continue
		}
		if depth == 0 {
			return ix, true
		}
		depth--
	}
	return 0, false
}

// markdownParseInlineChildren is parse_inline_children
// (U:text/format/markdown.rs:204-224).
func markdownParseInlineChildren(source string, p *paragraph, children []*MdastNode, cx *nodeContext) string {
	var text strings.Builder
	for _, group := range markdownInlineGroups(children) {
		if group.node != nil {
			text.WriteString(markdownParseParagraph(source, p, group.node, cx))
		} else {
			text.WriteString(markdownMergeChildrenWithMark(source, p, group.nodes, group.mark, cx))
		}
	}
	return text.String()
}

// markdownParseTableRow is parse_table_row (U:text/format/markdown.rs:226-237).
func markdownParseTableRow(source string, table *textTable, node *MdastNode, cx *nodeContext) {
	var row textTableRow
	for _, c := range node.Children {
		if c.Kind == MdastTableCell {
			markdownParseTableCell(source, &row, c, cx)
		}
	}
	table.children = append(table.children, row)
}

// markdownParseTableCell is parse_table_cell (U:text/format/markdown.rs:239-252).
func markdownParseTableCell(source string, row *textTableRow, node *MdastNode, cx *nodeContext) {
	p := defaultParagraph()
	markdownParseInlineChildren(source, p, node.Children, cx)
	row.children = append(row.children, textTableCell{children: p})
}

// markdownPushMerged pushes a text run with its marks plus newMark across
// the whole run, merging into a last mark that already covers it; empty
// runs are skipped (U:text/format/markdown.rs:254-283).
func markdownPushMerged(p *paragraph, text string, marks []textMarkRange, segments []sourceSegment, newMark TextMark) {
	if text == "" {
		return
	}
	node := newInlineNode(text).withMarks(marks).withSourceSegments(segments)
	n := len(node.text)
	if last := len(node.marks) - 1; last >= 0 && node.marks[last].rng.Start == 0 && node.marks[last].rng.End == n {
		node.marks[last].mark.Merge(newMark)
	} else {
		node.marks = append(node.marks, textMarkRange{rng: gpui.TextRange{Start: 0, End: n}, mark: newMark})
	}
	p.push(node)
}

// markdownMergeChildrenWithMark parses children and applies mark across
// each emitted run, keeping nested marks shifted into the run; images and
// custom nodes split the run (U:text/format/markdown.rs:285-374).
func markdownMergeChildrenWithMark(source string, p *paragraph, children []*MdastNode, mark TextMark, cx *nodeContext) string {
	var text, mergedText strings.Builder
	var mergedMarks []textMarkRange
	var mergedSegments []sourceSegment
	flush := func() {
		markdownPushMerged(p, mergedText.String(), mergedMarks, mergedSegments, mark)
		mergedText.Reset()
		mergedMarks, mergedSegments = nil, nil
	}

	for _, group := range markdownInlineGroups(children) {
		child := defaultParagraph()
		var childText string
		if group.node != nil {
			childText = markdownParseParagraph(source, child, group.node, cx)
		} else {
			childText = markdownMergeChildrenWithMark(source, child, group.nodes, group.mark, cx)
		}
		text.WriteString(childText)

		for _, node := range child.children {
			if node.custom != nil {
				flush()
				if len(node.marks) > 0 {
					node.marks[0].mark.Merge(mark)
				} else {
					node.marks = append(node.marks, textMarkRange{rng: gpui.TextRange{Start: 0, End: len(node.text)}, mark: mark})
				}
				p.push(node)
				continue
			}
			offset := mergedText.Len()
			mergedText.WriteString(node.text)
			for _, seg := range node.sourceSegments {
				seg.rendered.Start += offset
				seg.rendered.End += offset
				mergedSegments = append(mergedSegments, seg)
			}
			for _, m := range node.marks {
				mergedMarks = append(mergedMarks, textMarkRange{
					rng:  gpui.TextRange{Start: m.rng.Start + offset, End: m.rng.End + offset},
					mark: m.mark,
				})
			}
			if node.image != nil {
				image := *node.image
				if mark.Link != nil {
					image.Link = mark.Link
				}
				// Inline images split the run: flush it and emit the image as
				// its own sibling.
				flush()
				p.push(inlineNodeImage(image))
			}
		}
	}
	flush()
	return text.String()
}

// markdownSourceSegments aligns a rendered text with its node's source,
// optionally taking the escape before the span
// (U:text/format/markdown.rs:376-402).
func markdownSourceSegments(source, rendered string, span *Span, sourceOffset int, includePrecedingEscape bool) []sourceSegment {
	if span == nil {
		return nil
	}
	localStart := max(span.Start-sourceOffset, 0)
	localEnd := max(span.End-sourceOffset, 0)
	raw, ok := markdownStrGet(source, localStart, localEnd)
	if !ok {
		return nil
	}
	segments := markdownAlignedSourceSegments(raw, rendered, span.Start, true)
	if includePrecedingEscape && localStart >= 1 && source[localStart-1] == '\\' && len(segments) > 0 {
		segments[0].source.Start--
	}
	return segments
}

// markdownCharPositions is source_char_offset's Option<BTreeMap>: built
// after the first missed search so later misses do not rescan.
type markdownCharPositions struct {
	built bool
	index map[rune][]int
}

// markdownAlignedSourceSegments maps each rendered character to the source
// bytes that produced it: escapes, entities, soft breaks and container
// prefixes stay atomic, contiguous one-to-one runs are compacted
// (U:text/format/markdown.rs:404-505).
func markdownAlignedSourceSegments(raw, rendered string, sourceOffset int, decodeEntities bool) []sourceSegment {
	var segments []sourceSegment
	rawCursor, renderedStart, whitespaceEnd := 0, 0, 0
	var positions markdownCharPositions
	entityCursor := -1
	var entity string
	entityLen := 0
	// Only the final LF may absorb source-only text before it.
	finalLineStart := 0
	if prefix, ok := strings.CutSuffix(raw, "\n"); ok {
		if nl := strings.LastIndexByte(prefix, '\n'); nl >= 0 {
			finalLineStart = nl + 1
		}
	}

	for renderedStart < len(rendered) {
		if decodeEntities && entityCursor != rawCursor {
			entity, entityLen = markdownDecodedEntity(raw[rawCursor:])
			entityCursor = rawCursor
		}
		if entityLen > 0 && strings.HasPrefix(rendered[renderedStart:], entity) {
			renderedEnd := renderedStart + len(entity)
			segments = markdownPushSourceSegment(segments, sourceSegment{
				rendered: gpui.TextRange{Start: renderedStart, End: renderedEnd},
				source:   gpui.TextRange{Start: sourceOffset + rawCursor, End: sourceOffset + rawCursor + entityLen},
			})
			renderedStart = renderedEnd
			rawCursor += entityLen
			continue
		}

		renderedChar, size := utf8.DecodeRuneInString(rendered[renderedStart:])
		charStr := rendered[renderedStart : renderedStart+size]
		renderedEnd := renderedStart + size
		remainder := raw[rawCursor:]
		if renderedChar == ' ' && rawCursor >= whitespaceEnd {
			// Reuse this boundary while consuming a run of literal spaces.
			n := 0
			for n < len(remainder) && (remainder[n] == ' ' || remainder[n] == '\t') {
				n++
			}
			whitespaceEnd = rawCursor + n
		}
		var relativeStart, sourceLen int
		switch {
		case renderedChar == ' ' && whitespaceEnd < len(raw) && (raw[whitespaceEnd] == '\n' || raw[whitespaceEnd] == '\r'):
			newlineLen := 1
			if strings.HasPrefix(raw[whitespaceEnd:], "\r\n") {
				newlineLen = 2
			}
			relativeStart, sourceLen = whitespaceEnd-rawCursor, newlineLen
		case renderedChar == '\n' && strings.HasSuffix(remainder, "\n") && rawCursor >= finalLineStart:
			relativeStart, sourceLen = 0, len(remainder)
		case strings.HasPrefix(remainder, "\\") && strings.HasPrefix(remainder[1:], charStr):
			relativeStart, sourceLen = 0, 1+size
		case strings.HasPrefix(remainder, charStr):
			relativeStart, sourceLen = 0, size
		default:
			sourceStart, ok := markdownSourceCharOffset(raw, rawCursor, renderedChar, &positions)
			if !ok {
				// Decoded entities and other source-only syntax have no exact
				// mapping: leave a rendered gap and keep aligning.
				renderedStart = renderedEnd
				continue
			}
			relativeStart, sourceLen = sourceStart-rawCursor, size
		}
		sourceStart := rawCursor + relativeStart
		sourceEnd := sourceStart + sourceLen
		segments = markdownPushSourceSegment(segments, sourceSegment{
			rendered: gpui.TextRange{Start: renderedStart, End: renderedEnd},
			source:   gpui.TextRange{Start: sourceOffset + sourceStart, End: sourceOffset + sourceEnd},
		})
		rawCursor = sourceEnd
		renderedStart = renderedEnd
	}
	return segments
}

// markdownSourceCharOffset finds character at or after rawCursor; after
// the first miss it indexes the rest of raw so misses stay cheap
// (U:text/format/markdown.rs:507-535).
func markdownSourceCharOffset(raw string, rawCursor int, character rune, positions *markdownCharPositions) (int, bool) {
	if positions.built {
		offsets := positions.index[character]
		i := sort.SearchInts(offsets, rawCursor)
		if i < len(offsets) {
			return offsets[i], true
		}
		return 0, false
	}
	if offset := strings.IndexRune(raw[rawCursor:], character); offset >= 0 {
		return rawCursor + offset, true
	}
	positions.built = true
	positions.index = map[rune][]int{}
	for offset, c := range raw[rawCursor:] {
		positions.index[c] = append(positions.index[c], rawCursor+offset)
	}
	return 0, false
}

// markdownPushSourceSegment appends segment, extending the previous one
// when both are contiguous one-to-one mappings
// (U:text/format/markdown.rs:537-549).
func markdownPushSourceSegment(segments []sourceSegment, segment sourceSegment) []sourceSegment {
	if n := len(segments); n > 0 {
		prev := &segments[n-1]
		if prev.rendered.End == segment.rendered.Start && prev.source.End == segment.source.Start &&
			rangeLen(prev.rendered) == rangeLen(prev.source) && rangeLen(segment.rendered) == rangeLen(segment.source) {
			prev.rendered.End = segment.rendered.End
			prev.source.End = segment.source.End
			return segments
		}
	}
	return append(segments, segment)
}

// markdownDecodedEntity decodes a `&name;`, `&#n;` or `&#xh;` reference at
// the start of source, returning the text and the bytes it spans
// (0 when none) (U:text/format/markdown.rs:551-575).
func markdownDecodedEntity(source string) (string, int) {
	candidate, ok := strings.CutPrefix(source, "&")
	if !ok {
		return "", 0
	}
	end := -1
	for i := 0; i < len(candidate); i++ {
		b := candidate[i]
		if b == ';' || !(util.IsAlphaNumeric(b) || b == '#') {
			end = i
			break
		}
	}
	if end < 0 || candidate[end] != ';' {
		return "", 0
	}
	semicolon := end + 1
	name := source[1 : semicolon+1]
	var decoded string
	switch {
	case strings.HasPrefix(name, "#x") || strings.HasPrefix(name, "#X"):
		r, ok := markdownCharFromNumber(strings.TrimSuffix(name[2:], ";"), 16)
		if !ok {
			return "", 0
		}
		decoded = string(r)
	case strings.HasPrefix(name, "#"):
		r, ok := markdownCharFromNumber(strings.TrimSuffix(name[1:], ";"), 10)
		if !ok {
			return "", 0
		}
		decoded = string(r)
	default:
		e, ok := util.LookUpHTML5EntityByName(strings.TrimSuffix(name, ";"))
		if !ok {
			return "", 0
		}
		decoded = string(e.Characters)
	}
	return decoded, semicolon + 1
}

// markdownCharFromNumber is char::from_u32(u32::from_str_radix(n, base)).
func markdownCharFromNumber(n string, base int) (rune, bool) {
	v, err := strconv.ParseUint(n, base, 32)
	if err != nil || !utf8.ValidRune(rune(v)) {
		return 0, false
	}
	return rune(v), true
}

// markdownCodeSourceSegments aligns code with the body of its node's
// source, without the fences (U:text/format/markdown.rs:577-621).
func markdownCodeSourceSegments(source, code string, span *Span, sourceOffset int) []sourceSegment {
	if span == nil {
		return nil
	}
	raw, ok := markdownStrGet(source, max(span.Start-sourceOffset, 0), max(span.End-sourceOffset, 0))
	if !ok {
		return nil
	}
	trimmed := strings.TrimLeftFunc(raw, isRustWhitespace)
	fenceChar, fenceLen := byte(0), 0
	if trimmed != "" && (trimmed[0] == '`' || trimmed[0] == '~') {
		n := 0
		for n < len(trimmed) && trimmed[n] == trimmed[0] {
			n++
		}
		if n >= 3 {
			fenceChar, fenceLen = trimmed[0], n
		}
	}
	bodyStart, bodyEnd := 0, len(raw)
	if fenceLen > 0 {
		bodyStart = len(raw)
		if nl := strings.IndexByte(raw, '\n'); nl >= 0 {
			bodyStart = nl + 1
		}
		lastLine := bodyStart
		if nl := strings.LastIndexByte(raw, '\n'); nl >= 0 {
			lastLine = nl + 1
		}
		closing := strings.TrimFunc(raw[lastLine:], isRustWhitespace)
		isClosing := utf8.RuneCountInString(closing) >= fenceLen &&
			strings.Trim(closing, string(fenceChar)) == ""
		if isClosing {
			bodyEnd = lastLine
		}
	}
	if bodyStart > bodyEnd {
		bodyStart = bodyEnd
	}
	return markdownAlignedSourceSegments(raw[bodyStart:bodyEnd], code, span.Start+bodyStart, false)
}

// isRustWhitespace is char::is_whitespace (Unicode White_Space).
func isRustWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x85, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// markdownStrGet is str::get(a..b): the slice when in bounds and on
// character boundaries.
func markdownStrGet(s string, a, b int) (string, bool) {
	if a < 0 || b > len(s) || a > b {
		return "", false
	}
	if (a < len(s) && !utf8.RuneStart(s[a])) || (b < len(s) && !utf8.RuneStart(s[b])) {
		return "", false
	}
	return s[a:b], true
}

// markdownNewSpan is new_span: the node's position shifted by the context
// offset (U:text/format/markdown.rs:889-896).
func markdownNewSpan(node *MdastNode, cx *nodeContext) *Span {
	if node.Position == nil {
		return nil
	}
	return &Span{Start: cx.offset + node.Position.Start.Offset, End: cx.offset + node.Position.End.Offset}
}

// markdownMappedInline is mapped_inline (U:text/format/markdown.rs:623-642).
func markdownMappedInline(source, text string, node *MdastNode, cx *nodeContext) inlineNode {
	span := markdownNewSpan(node, cx)
	segments := markdownSourceSegments(source, text, span, cx.offset, node.Kind == MdastText)
	return newInlineNode(text).withSourceSegments(segments)
}

// markdownAppendInlineHTMLBlocks flattens the blocks an inline HTML tag
// parsed into into the paragraph; false when one is not inline
// (U:text/format/markdown.rs:644-667).
func markdownAppendInlineHTMLBlocks(p *paragraph, blocks []blockNode) (string, bool) {
	var text strings.Builder
	for _, block := range blocks {
		switch v := block.(type) {
		case *blockRoot:
			t, ok := markdownAppendInlineHTMLBlocks(p, v.children)
			if !ok {
				return "", false
			}
			text.WriteString(t)
		case *paragraph:
			text.WriteString(v.text())
			for _, child := range v.children {
				p.push(child)
			}
		case *blockBreak:
			text.WriteByte('\n')
			p.push(newInlineNode("\n"))
		default:
			return "", false
		}
	}
	return text.String(), true
}

// markdownParseParagraph converts one inline mdast node into the
// paragraph's inline nodes and returns its plain text
// (U:text/format/markdown.rs:669-870).
func markdownParseParagraph(source string, p *paragraph, node *MdastNode, cx *nodeContext) string {
	span := markdownNewSpan(node, cx)
	if span != nil {
		p.setSpan(*span)
	}

	parseCx := newMarkdownParseContext(source, cx.offset)
	if custom := cx.extensions().parseInline(node, parseCx); custom != nil {
		c := *custom
		c.setSpan(span)
		nodeSource, _ := parseCx.NodeSource(node)
		c = c.withInlineSource(nodeSource)
		p.push(inlineNodeCustom(c))
		return c.AsText()
	}

	var text string
	switch node.Kind {
	case MdastParagraph:
		text = markdownParseInlineChildren(source, p, node.Children, cx)
	case MdastText:
		// Soft breaks reflow to spaces (a CRLF takes its CR along); hard
		// breaks arrive as their own Break node.
		text = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(node.Value)
		p.push(markdownMappedInline(source, text, node, cx))
	case MdastEmphasis:
		text = markdownMergeChildrenWithMark(source, p, node.Children, TextMark{}.WithItalic(), cx)
	case MdastStrong:
		text = markdownMergeChildrenWithMark(source, p, node.Children, TextMark{}.WithBold(), cx)
	case MdastDelete:
		text = markdownMergeChildrenWithMark(source, p, node.Children, TextMark{}.WithStrikethrough(), cx)
	case MdastInlineCode:
		text = node.Value
		p.push(newInlineNode(text).
			withSourceSegments(markdownCodeSourceSegments(source, text, span, cx.offset)).
			withMarks([]textMarkRange{{rng: gpui.TextRange{Start: 0, End: len(text)}, mark: TextMark{}.WithCode()}}))
	case MdastLink:
		link := LinkMark{URL: node.URL, Title: node.Title}
		text = markdownMergeChildrenWithMark(source, p, node.Children, TextMark{Link: &link}, cx)
	case MdastImage:
		alt := node.Alt
		p.pushImage(ImageNode{URL: node.URL, Title: node.Title, Alt: &alt, span: span})
	case MdastBreak:
		// Hard line break: a newline node, like the inline-HTML <br> path.
		text = "\n"
		p.push(markdownMappedInline(source, "\n", node, cx))
	case MdastInlineMath:
		// Unclaimed math stays literal: fall back to the original source.
		if s, ok := parseCx.NodeSource(node); ok {
			text = s
		} else {
			text = node.Value
		}
		p.push(markdownMappedInline(source, text, node, cx))
	case MdastMdxTextExpression:
		text = node.Value
		p.push(newInlineNode(text).
			withSourceSegments(markdownCodeSourceSegments(source, text, span, cx.offset)).
			withMarks([]textMarkRange{{rng: gpui.TextRange{Start: 0, End: len(text)}}}))
	case MdastHTML:
		doc, err := markdownHTMLParse(node.Value, cx)
		if err != nil {
			text = node.Value
			break
		}
		if inlineText, ok := markdownAppendInlineHTMLBlocks(p, doc.blocks); ok {
			text = inlineText
		}
	case MdastFootnoteReference:
		prefix := "[" + node.Identifier + "]"
		p.push(markdownMappedInline(source, prefix, node, cx).
			withMarks([]textMarkRange{{rng: gpui.TextRange{Start: 0, End: len(prefix)}, mark: TextMark{Italic: true}}}))
	case MdastLinkReference:
		identifier := node.Identifier
		link := LinkMark{Title: node.Label, Identifier: &identifier}
		text = markdownMergeChildrenWithMark(source, p, node.Children, TextMark{Link: &link}, cx)
	}
	return text
}

// markdownASTToDocument is ast_to_document (U:text/format/markdown.rs:872-887).
func markdownASTToDocument(source string, root *MdastNode, cx *nodeContext) *parsedDocument {
	blocks := make([]blockNode, 0, len(root.Children))
	for _, c := range root.Children {
		blocks = append(blocks, markdownASTToNode(source, c, cx))
	}
	return &parsedDocument{source: source, blocks: blocks}
}

func markdownASTToNodes(source string, children []*MdastNode, cx *nodeContext) []blockNode {
	out := make([]blockNode, 0, len(children))
	for _, c := range children {
		out = append(out, markdownASTToNode(source, c, cx))
	}
	return out
}

// markdownASTToNode converts one block mdast node
// (U:text/format/markdown.rs:898-1082).
func markdownASTToNode(source string, value *MdastNode, cx *nodeContext) blockNode {
	span := markdownNewSpan(value, cx)
	parseCx := newMarkdownParseContext(source, cx.offset)
	if custom := cx.extensions().parseBlock(value, parseCx); custom != nil {
		c := *custom
		c.setSpan(span)
		return &blockCustom{node: c}
	}

	switch value.Kind {
	case MdastParagraph, MdastMdxJsxTextElement, MdastMdxJsxFlowElement:
		p := defaultParagraph()
		markdownParseInlineChildren(source, p, value.Children, cx)
		p.span = span
		return p
	case MdastBlockquote:
		return &blockBlockquote{children: markdownASTToNodes(source, value.Children, cx), span: span}
	case MdastList:
		return &blockList{children: markdownASTToNodes(source, value.Children, cx), ordered: value.Ordered, start: value.Start, span: span}
	case MdastListItem:
		return &blockListItem{children: markdownASTToNodes(source, value.Children, cx), spread: value.Spread, checked: value.Checked, span: span}
	case MdastBreak:
		return &blockBreak{span: span}
	case MdastCode:
		segments := markdownCodeSourceSegments(source, value.Value, span, cx.offset)
		return newCodeBlock(value.Value, value.Lang, span).withSourceSegments(segments)
	case MdastHeading:
		p := defaultParagraph()
		markdownParseInlineChildren(source, p, value.Children, cx)
		return &blockHeading{level: value.Depth, children: p, span: span}
	case MdastMath:
		segments := markdownCodeSourceSegments(source, value.Value, span, cx.offset)
		return newCodeBlock(value.Value, nil, span).withSourceSegments(segments)
	case MdastHTML:
		doc, err := markdownHTMLParse(value.Value, cx)
		if err != nil {
			return newParagraph(value.Value)
		}
		return &blockRoot{children: doc.blocks, span: span}
	case MdastMdxFlowExpression:
		segments := markdownCodeSourceSegments(source, value.Value, span, cx.offset)
		lang := "mdx"
		return newCodeBlock(value.Value, &lang, span).withSourceSegments(segments)
	case MdastYaml:
		lang := "yml"
		return newCodeBlock(value.Value, &lang, span)
	case MdastToml:
		lang := "toml"
		return newCodeBlock(value.Value, &lang, span)
	case MdastThematicBreak:
		return &blockHorizontalRule{span: span}
	case MdastTable:
		table := &textTable{}
		for _, align := range value.Align {
			table.columnAligns = append(table.columnAligns, columnAlignFromMdast(align))
		}
		for _, c := range value.Children {
			if c.Kind == MdastTableRow {
				markdownParseTableRow(source, table, c, cx)
			}
		}
		table.span = span
		return table
	case MdastFootnoteDefinition:
		p := defaultParagraph()
		prefix := "[" + value.Identifier + "]: "
		p.push(newInlineNode(prefix).withMarks([]textMarkRange{{rng: gpui.TextRange{Start: 0, End: len(prefix)}, mark: TextMark{Italic: true}}}))
		markdownParseInlineChildren(source, p, value.Children, cx)
		p.span = span
		return p
	case MdastDefinition:
		identifier := value.Identifier
		cx.addRef(identifier, LinkMark{URL: value.URL, Identifier: &identifier, Title: value.Title})
		return &blockDefinition{identifier: identifier, url: value.URL, title: value.Title, span: span}
	}
	return blockUnknown{}
}
