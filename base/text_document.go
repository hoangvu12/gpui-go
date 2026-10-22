package base

// Port of U:text/document.rs: the parsed document and its text,
// selection and Markdown traversals. render_root (U:text/document.rs:
// 189-245) renders blocks and is ticket 28's; nodeRenderOptions, its
// pure data, is here.

import (
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// parsedDocument is the parsed document AST (U:text/document.rs:15-20).
type parsedDocument struct {
	source string
	blocks []blockNode
}

// nodeRenderOptions is the per-block render state
// (U:text/document.rs:22-31).
type nodeRenderOptions struct {
	ix        int
	inList    bool
	todo      bool
	ordered   bool
	listStart *uint32
	depth     int
	isLast    bool
}

// withIsLast is NodeRenderOptions::is_last (U:text/document.rs:33-38);
// the builder takes the With prefix beside the isLast field (doc.go).
func (o nodeRenderOptions) withIsLast(isLast bool) nodeRenderOptions {
	o.isLast = isLast
	return o
}

// text is the plain text of every block (U:text/document.rs:41-47).
func (d *parsedDocument) text() string {
	var b strings.Builder
	for _, block := range d.blocks {
		b.WriteString(blockNodeText(block))
	}
	return b.String()
}

// selectedText is the selected text across all blocks in format
// (U:text/document.rs:49-138). blocks, when non-nil, is the inclusive
// [first, last] block span the selection endpoints recorded; blocks it
// spans that never painted are emitted whole. In source mode top-level
// blocks are joined with a blank line, and blocks strictly inside the
// span (or without a selection of their own) are copied from the source.
func (d *parsedDocument) selectedText(format SelectionFormat, blocks *[2]int) string {
	painted := make([]bool, len(d.blocks))
	paintedFirst, paintedLast := -1, -1
	for ix, block := range d.blocks {
		painted[ix] = blockNodeHasSelection(block)
		if painted[ix] {
			if paintedFirst < 0 {
				paintedFirst = ix
			}
			paintedLast = ix
		}
	}
	if paintedFirst < 0 {
		return ""
	}
	lastIx := max(len(d.blocks)-1, 0)
	first, last := paintedFirst, paintedLast
	if blocks != nil {
		first, last = min(blocks[0], lastIx), min(blocks[1], lastIx)
	}

	if format == SelectionFormatPlain {
		var text strings.Builder
		for ix := first; ix <= last && ix < len(d.blocks); ix++ {
			block := d.blocks[ix]
			selected := blockNodeSelectedText(block, format)
			isVirtualEndpoint := blocks != nil && (ix == blocks[0] || ix == blocks[1])
			switch {
			case blocks != nil && !isVirtualEndpoint:
				text.WriteString(blockNodeText(block))
			case selected != "":
				text.WriteString(selected)
			case !painted[ix]:
				// Never painted, so it cannot report a selection of its own
				// even though the span covers it.
				text.WriteString(blockNodeText(block))
			}
		}
		return text.String()
	}

	var out []string
	for ix := first; ix <= last && ix < len(d.blocks); ix++ {
		block := d.blocks[ix]
		// Only the blocks the selection starts and ends in can be partly
		// selected; everything between is covered whole.
		var source string
		if (ix == first || ix == last) && painted[ix] {
			source = blockNodeSelectedText(block, format)
		} else {
			source = d.wholeSource(block)
		}
		if trimmed := strings.TrimRight(source, "\n"); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n\n")
}

// wholeSource is the source of a block the selection covers, copied from
// the original text when the parser located it
// (U:text/document.rs:140-155).
func (d *parsedDocument) wholeSource(block blockNode) string {
	if span := blockNodeSpan(block); span != nil {
		if source, ok := markdownStrGet(d.source, span.Start, span.End); ok {
			return source
		}
	}
	return blockNodeSelectedText(block, SelectionFormatSource)
}

// equal is ParsedDocument's derived PartialEq: the source and every block
// (U:text/document.rs:16-20).
func (d *parsedDocument) equal(o *parsedDocument) bool {
	if d == nil || o == nil {
		return d == o
	}
	return d.source == o.source && blocksEqual(d.blocks, o.blocks)
}

// selectedSourceRange is the exact source range of the selection, or
// false when any selected byte cannot be mapped
// (U:text/document.rs:157-163).
func (d *parsedDocument) selectedSourceRange() (gpui.TextRange, bool) {
	var selected sourceRangeSelection
	for _, block := range d.blocks {
		selected.merge(blockNodeSelectedSourceRange(block))
	}
	return selected.intoRange()
}

// clearSelection clears every stored selection synchronously
// (U:text/document.rs:165-175).
func (d *parsedDocument) clearSelection() {
	for _, block := range d.blocks {
		blockNodeClearSelection(block)
	}
}

// toMarkdown converts the document to Markdown, used by tests and round
// trips (U:text/document.rs:177-187).
func (d *parsedDocument) toMarkdown() string {
	return blocksToMarkdown(d.blocks, "\n\n")
}
