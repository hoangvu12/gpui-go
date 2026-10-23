package base

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/sumtree"
)

// The 15 tests of U:input/base/rope_ext.rs:458-718, plus the trait's doc
// examples (U:input/base/rope_ext.rs:84-274).

const inpSample = "Hello\nWorld\r\nThis is a test 中文\nRope"

func inpExt(s string) RopeExt {
	r := rope.FromStr(s)
	return RopeExtOf(&r)
}

func inpEqStr(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func inpEqInt(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func inpLines(e RopeExt) []string {
	var out []string
	it := e.IterLines()
	for line, ok := it.Next(); ok; line, ok = it.Next() {
		out = append(out, line.String())
	}
	return out
}

// U:input/base/rope_ext.rs:458-473 test_slice_line, plus the slice_line and
// slice_lines doc examples (:109-131).
func TestRopeExtSliceLine(t *testing.T) {
	r := inpExt(inpSample)
	inpEqStr(t, r.SliceLine(0).String(), "Hello")
	inpEqStr(t, r.SliceLine(1).String(), "World\r")
	inpEqStr(t, r.SliceLine(2).String(), "This is a test 中文")
	inpEqStr(t, r.SliceLine(3).String(), "Rope")
	inpEqStr(t, r.SliceLine(6).String(), "")
	r = inpExt("Hello\r")
	inpEqStr(t, r.SliceLine(0).String(), "Hello\r")
	inpEqStr(t, r.SliceLine(1).String(), "")

	r = inpExt(inpSample)
	inpEqStr(t, r.SliceLines(0, 2).String(), "Hello\nWorld\r")
	inpEqStr(t, r.SliceLines(1, 3).String(), "World\r\nThis is a test 中文")
	inpEqStr(t, r.SliceLines(2, 5).String(), "This is a test 中文\nRope")
	inpEqStr(t, r.SliceLines(3, 10).String(), "Rope")
	inpEqStr(t, r.SliceLines(5, 10).String(), "")
}

// U:input/base/rope_ext.rs:475-487 test_lines_len.
func TestRopeExtLinesLen(t *testing.T) {
	inpEqInt(t, inpExt(inpSample).LinesLen(), 4)
	inpEqInt(t, inpExt("").LinesLen(), 1)
	inpEqInt(t, inpExt("Single line").LinesLen(), 1)
	inpEqInt(t, inpExt("Hello\r").LinesLen(), 1)
}

// U:input/base/rope_ext.rs:489-497 test_lines, plus the line_len doc
// example (:155-167).
func TestRopeExtLines(t *testing.T) {
	got := inpLines(inpExt("Hello\nWorld\r\nThis is a test 中文\nRope\r"))
	inpEqStr(t, strings.Join(got, "|"), "Hello|World\r|This is a test 中文|Rope\r")

	r := inpExt(inpSample)
	for row, want := range []int{5, 6, 21, 4, 0} {
		inpEqInt(t, r.LineLen(row), want)
	}
	if n := r.IterLines().Len(); n != 4 {
		t.Fatalf("Len()=%d", n)
	}
}

// U:input/base/rope_ext.rs:499-507 test_eq.
func TestRopeExtEq(t *testing.T) {
	r := rope.FromStr(inpSample)
	if !r.Equal(rope.FromStr(inpSample)) || r.Equal(rope.FromStr("Hello\nWorld")) {
		t.Fatal("eq")
	}
	r1 := r
	if !r.Equal(r1) {
		t.Fatal("clone eq")
	}
}

// U:input/base/rope_ext.rs:509-519 test_iter_lines (skip(1).take(2)),
// using RopeLines::nth.
func TestRopeExtIterLines(t *testing.T) {
	it := inpExt(inpSample).IterLines()
	a, ok1 := it.Nth(1)
	b, ok2 := it.Next()
	if !ok1 || !ok2 {
		t.Fatal("iterator ended early")
	}
	inpEqStr(t, a.String()+"|"+b.String(), "World\r|This is a test 中文")
	inpEqInt(t, it.Len(), 1)
}

// U:input/base/rope_ext.rs:521-538 test_line_start_end_offset.
func TestRopeExtLineStartEndOffset(t *testing.T) {
	r := inpExt(inpSample)
	for _, c := range [][3]int{{0, 0, 5}, {1, 6, 12}, {2, 13, 34}, {3, 35, 39}, {4, 39, 39}} {
		inpEqInt(t, r.LineStartOffset(c[0]), c[1])
		inpEqInt(t, r.LineEndOffset(c[0]), c[2])
	}
}

// U:input/base/rope_ext.rs:540-565 test_line_column.
func TestRopeExtLineColumn(t *testing.T) {
	r := inpExt("a 中文🎉 test\nRope")
	inpEqInt(t, r.PositionToOffset(NewInputPosition(0, 3)), len("a 中"))
	inpEqInt(t, r.PositionToOffset(NewInputPosition(0, 5)), len("a 中文🎉"))
	inpEqInt(t, r.PositionToOffset(NewInputPosition(1, 1)), len("a 中文🎉 test\nR"))
	inpEqInt(t, r.PositionToOffset(NewInputPosition(0, 1<<32-1)), len("a 中文🎉 test"))
	if p := r.OffsetToPosition(len("a 中文🎉 test\nR")); p != NewInputPosition(1, 1) {
		t.Fatalf("OffsetToPosition %+v", p)
	}
	if p := r.OffsetToPosition(len("a 中文🎉")); p != NewInputPosition(0, 5) {
		t.Fatalf("OffsetToPosition %+v", p)
	}
}

// U:input/base/rope_ext.rs:567-578 test_offset_to_point.
func TestRopeExtOffsetToPoint(t *testing.T) {
	r := inpExt("a 中文🎉 test\nRope")
	for _, c := range []struct {
		off  int
		want InputPoint
	}{{0, NewInputPoint(0, 0)}, {1, NewInputPoint(0, 1)}, {len("a 中"), NewInputPoint(0, 5)}, {len("a 中文🎉"), NewInputPoint(0, 12)}, {len("a 中文🎉 test\nR"), NewInputPoint(1, 1)}} {
		if got := r.OffsetToPoint(c.off); got != c.want {
			t.Fatalf("OffsetToPoint(%d)=%+v want %+v", c.off, got, c.want)
		}
	}
}

// U:input/base/rope_ext.rs:580-591 test_point_to_offset.
func TestRopeExtPointToOffset(t *testing.T) {
	r := inpExt("a 中文🎉 test\nRope")
	inpEqInt(t, r.PointToOffset(NewInputPoint(0, 0)), 0)
	inpEqInt(t, r.PointToOffset(NewInputPoint(0, 1)), 1)
	inpEqInt(t, r.PointToOffset(NewInputPoint(0, 5)), len("a 中"))
	inpEqInt(t, r.PointToOffset(NewInputPoint(0, 12)), len("a 中文🎉"))
	inpEqInt(t, r.PointToOffset(NewInputPoint(1, 1)), len("a 中文🎉 test\nR"))
}

// U:input/base/rope_ext.rs:593-603 test_char_at.
func TestRopeExtCharAt(t *testing.T) {
	r := inpExt("Hello\nWorld\r\nThis is a test 中文🎉\nRope")
	for off, want := range map[int]rune{0: 'H', 5: '\n', 13: 'T', 28: '中', 34: '🎉', 38: '\n'} {
		if got, ok := r.CharAt(off); !ok || got != want {
			t.Fatalf("CharAt(%d)=%q,%v want %q", off, got, ok, want)
		}
	}
	if _, ok := r.CharAt(50); ok {
		t.Fatal("CharAt(50) should be None")
	}
}

// U:input/base/rope_ext.rs:605-623 test_word_at.
func TestRopeExtWordAt(t *testing.T) {
	r := inpExt("Hello\nWorld\r\nThis is a test 中文 世界\nRope")
	for _, c := range []struct {
		off        int
		word       string
		start, end int
		ok         bool
	}{
		{0, "Hello", 0, 5, true}, {8, "World", 6, 11, true}, {12, "", 0, 0, false},
		{13, "This", 13, 17, true}, {31, "中文", 28, 34, true}, {38, "世界", 35, 41, true},
		{44, "Rope", 42, 46, true}, {45, "Rope", 42, 46, true},
	} {
		inpEqStr(t, r.WordAt(c.off), c.word)
		s, e, ok := r.WordRange(c.off)
		if ok != c.ok || (ok && (s != c.start || e != c.end)) {
			t.Fatalf("WordRange(%d)=%d..%d,%v", c.off, s, e, ok)
		}
	}
}

// U:input/base/rope_ext.rs:625-639 test_offset_utf16_conversion.
func TestRopeExtOffsetUTF16Conversion(t *testing.T) {
	r := inpExt("hello 中文🎉 test\nRope")
	inpEqInt(t, r.OffsetToOffsetUTF16(len("hello")), 5)
	inpEqInt(t, r.OffsetToOffsetUTF16(len("hello 中")), 7)
	inpEqInt(t, r.OffsetToOffsetUTF16(len("hello 中文")), 8)
	inpEqInt(t, r.OffsetToOffsetUTF16(len("hello 中文🎉")), 10)
	inpEqInt(t, r.OffsetToOffsetUTF16(100), 20)
	inpEqInt(t, r.OffsetUTF16ToOffset(5), len("hello"))
	inpEqInt(t, r.OffsetUTF16ToOffset(7), len("hello 中"))
	inpEqInt(t, r.OffsetUTF16ToOffset(8), len("hello 中文"))
	inpEqInt(t, r.OffsetUTF16ToOffset(10), len("hello 中文🎉"))
	inpEqInt(t, r.OffsetUTF16ToOffset(100), len("hello 中文🎉 test\nRope"))
}

// U:input/base/rope_ext.rs:641-677 test_replace.
func TestRopeExtReplace(t *testing.T) {
	r := rope.FromStr(inpSample)
	e := RopeExtOf(&r)
	e.Replace(6, 11, "Universe")
	inpEqStr(t, r.String(), "Hello\nUniverse\r\nThis is a test 中文\nRope")
	e.Replace(0, 5, "Hi")
	inpEqStr(t, r.String(), "Hi\nUniverse\r\nThis is a test 中文\nRope")
	e.Replace(r.Len()-4, r.Len(), "String")
	inpEqStr(t, r.String(), "Hi\nUniverse\r\nThis is a test 中文\nString")
	for _, c := range []struct {
		s, e int
		want string
	}{{0, 1, "New文"}, {0, 2, "New文"}, {0, 3, "New文"}, {1, 4, "New"}} {
		r := rope.FromStr("中文")
		RopeExtOf(&r).Replace(c.s, c.e, "New")
		inpEqStr(t, r.String(), c.want)
	}
}

// U:input/base/rope_ext.rs:679-700 test_clip_offset.
func TestRopeExtClipOffset(t *testing.T) {
	r := inpExt("Hello 中文🎉 test\nRope")
	L, R := sumtree.Left, sumtree.Right
	for _, c := range []struct {
		off  int
		bias sumtree.Bias
		want int
	}{{5, L, 5}, {7, L, 6}, {7, R, 9}, {9, L, 9}, {13, L, 12}, {13, R, 16}, {16, L, 16}, {5, L, 5}, {5, R, 5}, {26, L, 26}, {100, L, 26}} {
		inpEqInt(t, r.ClipOffset(c.off, c.bias), c.want)
	}
}

// U:input/base/rope_ext.rs:702-718 test_char_index_to_offset.
func TestRopeExtCharIndexToOffset(t *testing.T) {
	r := inpExt("a 中文🎉 test\nRope")
	inpEqInt(t, r.CharIndexToOffset(0), 0)
	inpEqInt(t, r.CharIndexToOffset(1), 1)
	inpEqInt(t, r.CharIndexToOffset(3), len("a 中"))
	inpEqInt(t, r.CharIndexToOffset(5), len("a 中文🎉"))
	inpEqInt(t, r.CharIndexToOffset(6), len("a 中文🎉 "))
	for _, c := range [][2]int{{0, 0}, {1, 1}, {3, 3}, {4, 3}, {5, 3}, {6, 4}, {10, 5}} {
		inpEqInt(t, r.OffsetToCharIndex(c[0]), c[1])
	}
}
