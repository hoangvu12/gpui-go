package sumtree

import (
	"cmp"
	"math/rand"
	"testing"
)

// Property tests of the two Base trees against brute-force slice models,
// using the exact summary/dimension definitions and cursor call patterns of
// input/editor/display_map/text_wrapper.rs:59-241,500-512 and
// input/editor/diagnostics.rs:137-252.

// --- text_wrapper.rs:29-139 -------------------------------------------------

type lineItem struct {
	len, wraps int // len, wrapped_lines.len()
}

type lineSummary struct {
	bufferRows, wrapRows, bytes, maxLineLen, longestRow int
}

// text_wrapper.rs:87-96
func (s lineSummary) AddSummary(o lineSummary) lineSummary {
	if o.maxLineLen > s.maxLineLen {
		s.longestRow = s.bufferRows + o.longestRow
		s.maxLineLen = o.maxLineLen
	}
	s.bufferRows += o.bufferRows
	s.wrapRows += o.wrapRows
	s.bytes += o.bytes
	return s
}

// text_wrapper.rs:102-110
func (l lineItem) Summary() lineSummary {
	return lineSummary{bufferRows: 1, wrapRows: l.wraps, bytes: l.len, maxLineLen: l.len}
}

type bufferRows int

func (b bufferRows) AddSummary(s lineSummary) bufferRows { return b + bufferRows(s.bufferRows) }
func (b bufferRows) Cmp(o bufferRows) int                { return cmp.Compare(b, o) }

type wrapRows int

func (w wrapRows) AddSummary(s lineSummary) wrapRows { return w + wrapRows(s.wrapRows) }
func (w wrapRows) Cmp(o wrapRows) int                { return cmp.Compare(w, o) }

type lines = SumTree[lineItem, lineSummary]
type bufWrap = Dimensions[lineSummary, bufferRows, wrapRows]
type wrapBuf = Dimensions[lineSummary, wrapRows, bufferRows]

// text_wrapper.rs:203-208
func line(t lines, row int) (lineItem, bool) {
	c := NewCursor[bufferRows](t)
	c.Seek(bufferRows(row), Right)
	return c.Item()
}

// text_wrapper.rs:218-222
func bufferLineToFirstWrapRow(t lines, row int) int {
	c := NewCursor[bufWrap](t)
	c.Seek(FirstDimension[lineSummary, bufferRows, wrapRows](bufferRows(row)), Right)
	return int(c.Start().V1)
}

// text_wrapper.rs:234-241
func wrapRowToBufferLine(t lines, wrapRow int) int {
	c := NewCursor[wrapBuf](t)
	c.Seek(FirstDimension[lineSummary, wrapRows, bufferRows](wrapRows(wrapRow)), Right)
	if _, ok := c.Item(); ok {
		return int(c.Start().V1)
	}
	return max(t.Summary().bufferRows-1, 0)
}

// text_wrapper.rs:500-512: replace rows [start, end] with newLines.
func splice(t lines, start, end int, newLines []lineItem) lines {
	if t.IsEmpty() {
		return FromIter[lineItem](newLines)
	}
	c := NewCursor[bufferRows](t)
	nt := c.Slice(bufferRows(start), Right)
	c.SeekForward(bufferRows(end+1), Right)
	nt.Extend(newLines)
	nt.Append(c.Suffix())
	return nt
}

func TestTextWrapperTreeAgainstModel(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	randLines := func(n int) []lineItem {
		out := make([]lineItem, n)
		for i := range out {
			out[i] = lineItem{len: rng.Intn(80), wraps: 1 + rng.Intn(3)}
		}
		return out
	}
	for seq := 0; seq < 60; seq++ {
		model := randLines(1 + rng.Intn(30))
		tr := FromIter[lineItem](model)
		for step := 0; step < 15; step++ {
			start := rng.Intn(len(model))
			end := start + rng.Intn(len(model)-start)
			repl := randLines(1 + rng.Intn(6))
			model = append(append(append([]lineItem{}, model[:start]...), repl...), model[end+1:]...)
			tr = splice(tr, start, end, repl)

			// summary(): len / lines_count / longest_row (text_wrapper.rs:184-200)
			s := tr.Summary()
			wantWrap, wantLongest, wantMax := 0, 0, 0
			for i, l := range model {
				wantWrap += l.wraps
				if l.len > wantMax {
					wantMax, wantLongest = l.len, i
				}
			}
			if s.bufferRows != len(model) || s.wrapRows != wantWrap || s.longestRow != wantLongest || s.maxLineLen != wantMax {
				t.Fatalf("summary %+v; want rows=%d wraps=%d longest=%d", s, len(model), wantWrap, wantLongest)
			}
			first := 0
			for row, l := range model {
				if got, ok := line(tr, row); !ok || got != l {
					t.Fatalf("line(%d)=%+v want %+v", row, got, l)
				}
				if got := bufferLineToFirstWrapRow(tr, row); got != first {
					t.Fatalf("first wrap row of %d = %d want %d", row, got, first)
				}
				for w := first; w < first+l.wraps; w++ {
					if got := wrapRowToBufferLine(tr, w); got != row {
						t.Fatalf("wrap row %d -> %d want %d", w, got, row)
					}
				}
				first += l.wraps
			}
			if _, ok := line(tr, len(model)); ok {
				t.Fatal("line past the end")
			}
			if got := bufferLineToFirstWrapRow(tr, len(model)); got != first {
				t.Fatalf("out-of-range row gives %d want total %d", got, first)
			}
			if got := wrapRowToBufferLine(tr, first+3); got != len(model)-1 {
				t.Fatalf("wrap row past end clamps to %d", got)
			}
		}
	}
}

// --- diagnostics.rs:122-183 -------------------------------------------------

type diagEntry struct{ start, end int }

type diagSummary struct{ count, start, end int }

// diagnostics.rs:165-169
func (s diagSummary) AddSummary(o diagSummary) diagSummary {
	return diagSummary{count: s.count + o.count, start: o.start, end: o.end}
}

// diagnostics.rs:146-152
func (e diagEntry) Summary() diagSummary { return diagSummary{1, e.start, e.end} }

// offset is `impl SeekTarget<DiagnosticSummary, DiagnosticSummary> for usize`
// (diagnostics.rs:173-183).
type offset int

func (o offset) Cmp(s diagSummary) int {
	switch {
	case int(o) < s.start:
		return -1
	case int(o) > s.end:
		return 1
	}
	return 0
}

// diagnostics.rs:240-252
func diagRange(t SumTree[diagEntry, diagSummary], start, end int) []diagEntry {
	c := NewCursor[diagSummary](t)
	c.Seek(offset(start), Left)
	var out []diagEntry
	for {
		e, ok := c.Item()
		if !ok || e.start >= end {
			return out
		}
		c.Next()
		out = append(out, e)
	}
}

func TestDiagnosticsTreeAgainstModel(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	for seq := 0; seq < 80; seq++ {
		// DiagnosticSet::push appends entries in order (diagnostics.rs:204-216);
		// the summary seek assumes ascending, non-overlapping ranges.
		var model []diagEntry
		var tr SumTree[diagEntry, diagSummary]
		pos := 0
		for i := rng.Intn(25); i > 0; i-- {
			pos += rng.Intn(6)
			e := diagEntry{pos, pos + rng.Intn(5)}
			pos = e.end + 1
			model = append(model, e)
			tr.Push(e)
		}
		if tr.Summary().count != len(model) || tr.IsEmpty() != (len(model) == 0) {
			t.Fatal("len / is_empty")
		}
		for q := 0; q < 30; q++ {
			a := rng.Intn(pos + 3)
			b := a + rng.Intn(10)
			// Model: the first entry whose end >= a (the Left-biased seek
			// stops at the first entry the target does not pass), then every
			// entry starting before b.
			var want []diagEntry
			for _, e := range model {
				if e.end >= a && e.start < b {
					want = append(want, e)
				} else if e.start >= b {
					break
				}
			}
			got := diagRange(tr, a, b)
			if len(got) != len(want) {
				t.Fatalf("range(%d..%d)=%v want %v (model %v)", a, b, got, want, model)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("range(%d..%d)=%v want %v", a, b, got, want)
				}
			}
		}
	}
}
