package base

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/range_highlight.rs tests (683-815): 6 of 6.

func rhParse(t *testing.T, source string) *parsedDocument {
	t.Helper()
	doc, err := markdownParse(source, &nodeContext{})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func rhColor() gpui.Hsla { return gpui.Hsla{H: 0.15, S: 1, L: 0.5, A: 0.4} }

func rhRowEnd(r *leafRemap, key textLeafKey) *int {
	end, ok := r.rowSourceEnd(key)
	if !ok {
		return nil
	}
	return &end
}

func intPtrEqual(a, b *int) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// U:text/range_highlight.rs:694-709
func TestTableRowIndexKeepsEmptyCellsInTheirRow(t *testing.T) {
	source := "| a | b |\n|---|---|\n| é |   |\n|   |   |\n| c | d |\n"
	doc := rhParse(t, source)
	remap := newLeafRemap(doc, doc, false)
	if len(remap.tableRows) != 4 {
		t.Fatalf("%d rows", len(remap.tableRows))
	}
	for row, text := range []string{"b", "é", "", "d"} {
		var want *int
		if text != "" {
			e := strings.Index(source, text) + len(text)
			want = &e
		}
		for column := range 2 {
			key := *textLeafTableCell(0, row*2+column)
			if got := rhRowEnd(remap, key); !intPtrEqual(got, want) {
				t.Errorf("%+v: %v, want %v", key, got, want)
			}
		}
	}
	if rhRowEnd(remap, *textLeafTableCell(0, 8)) != nil {
		t.Error("a cell past the table has a row")
	}
}

// U:text/range_highlight.rs:711-739
func TestTableRowIndexFindsNestedTablesWithoutCrossingBetweenThem(t *testing.T) {
	source := "| a |\n|---|\n| b |\n\n" + "> | c |\n> |---|\n> | d |\n\n" + "- | e |\n  |---|\n  | f |\n"
	doc := rhParse(t, source)
	remap := newLeafRemap(doc, doc, false)
	if len(remap.tableRows) != 6 {
		t.Fatalf("%d rows", len(remap.tableRows))
	}
	for _, pair := range [][2]string{{"a", "b"}, {"c", "d"}, {"e", "f"}} {
		start := strings.Index(source, "| "+pair[0]+" |")
		for cell, text := range pair {
			want := strings.Index(source, text) + len(text)
			if got := rhRowEnd(remap, *textLeafTableCell(start, cell)); !intPtrEqual(got, &want) {
				t.Errorf("%s: %v, want %d", text, got, want)
			}
		}
		if rhRowEnd(remap, *textLeafTableCell(start, 2)) != nil || rhRowEnd(remap, *textLeafTableCell(start+1, 0)) != nil {
			t.Errorf("table %s crosses into another", pair[0])
		}
	}
}

// U:text/range_highlight.rs:741-773
func TestLongAndWideTablesRemapHighlightsByWholeRows(t *testing.T) {
	for _, dims := range [][2]int{{4096, 1}, {2, 1024}, {64, 16}} {
		rows, columns := dims[0], dims[1]
		row := "|" + strings.Repeat(" x |", columns) + "\n"
		separator := "|" + strings.Repeat("---|", columns) + "\n"
		source := row + separator + strings.Repeat(row, rows)
		old := rhParse(t, source)
		// Even unchanged cells earlier in the edited row must lose their
		// highlights, as must the unchanged rows that follow it.
		edit := len(row) + len(separator) + strings.LastIndex(row, "x")
		changed := source[:edit] + "y" + source[edit+1:]
		next := rhParse(t, changed)
		remap := newLeafRemap(old, next, false)
		if len(remap.tableRows) != rows+1 {
			t.Fatalf("%v: %d rows", dims, len(remap.tableRows))
		}
		frame := &rangeHighlightFrame{}
		for _, l := range remap.oldLeaves {
			frame.leaves = append(frame.leaves, leafBackgrounds{key: l.key, backgrounds: []textColorRange{{rng: mdRange(0, 1), color: rhColor()}}})
		}
		if len(frame.leaves) != (rows+1)*columns {
			t.Fatalf("%v: %d leaves", dims, len(frame.leaves))
		}
		kept := frame.remap(remap)
		if kept == nil || len(kept.leaves) != columns {
			t.Fatalf("%v: kept %+v", dims, kept)
		}
		for column := range columns {
			got := kept.backgrounds(*textLeafTableCell(0, column))
			if len(got) != 1 || got[0].rng != mdRange(0, 1) || got[0].color != rhColor() {
				t.Errorf("%v column %d: %v", dims, column, got)
			}
		}
	}
}

// U:text/range_highlight.rs:775-789
func TestAppendRemappingDoesNotBuildATableRowIndex(t *testing.T) {
	source := "| a |\n|---|\n| b |\n\n| c |\n|---|\n| d |\n"
	old := rhParse(t, source)
	next := rhParse(t, source+"| e |\n")
	remap := newLeafRemap(old, next, true)
	if len(remap.tableRows) != 0 {
		t.Fatal("an append built a row index")
	}
	first := *textLeafTableCell(0, 0)
	if key, unchanged, ok := remap.leaf(first); !ok || key != first || unchanged != unchangedAll {
		t.Errorf("first: %v %d %v", key, unchanged, ok)
	}
	lastStart := strings.Index(source, "| c |")
	for cell := range 2 {
		k := *textLeafTableCell(lastStart, cell)
		if key, unchanged, ok := remap.leaf(k); !ok || key != k || unchanged != 1 {
			t.Errorf("cell %d: %v %d %v", cell, key, unchanged, ok)
		}
	}
}

// U:text/range_highlight.rs:791-806
func TestAPositionInAnInlineObjectMovesOntoText(t *testing.T) {
	// "ab" then two objects of 2 bytes each, then "cd".
	leaf := func(n int) *leafSpan {
		return &leafSpan{rng: mdRange(10, 10+n), key: *textLeafBlock(0), objects: []gpui.TextRange{mdRange(2, 4), mdRange(4, 6)}}
	}
	for _, c := range []struct{ n, offset, want int }{{8, 1, 1}, {8, 3, 6}, {8, 5, 6}, {6, 5, 1}} {
		if got, ok := leaf(c.n).textOffsetNear(c.offset); !ok || got != c.want {
			t.Errorf("%s: %d %v, want %d", fmt.Sprint(c), got, ok, c.want)
		}
	}
}

// U:text/range_highlight.rs:808-814
func TestRangeHighlightRequiresABackground(t *testing.T) {
	h := NewRangeHighlight(mdRange(2, 5), rhColor())
	if h.Range() != mdRange(2, 5) || h.Background() != rhColor() {
		t.Fatalf("%+v", h)
	}
}
