package rope

import (
	"strings"
	"testing"
	"unicode"

	"github.com/hoangvu12/gpui-go/base/sumtree"
)

// The 15 unit tests of upstream-base/ce/crates/base/src/input/base/rope_ext.rs
// (lines 458-718) run here against a test-only transcription of RopeExt
// (rope_ext.rs:277-448). RopeExt itself is Base input logic and belongs to the
// input data-layer ticket; transcribing it here proves the rope exposes every
// ropey call it makes, with the semantics its tests expect.

type point struct{ row, column int }
type position struct{ line, character int }

type extRope struct{ Rope }

func ext(s string) *extRope { return &extRope{FromStr(s)} }

// rope_ext.rs:278-293
func (r *extRope) sliceLine(row int) RopeSlice {
	if row >= r.linesLen() {
		return r.Slice(0, 0)
	}
	line := r.Line(row, LF)
	if line.Len() > 0 {
		end := line.Len() - 1
		if line.IsCharBoundary(end) && line.Char(end) == '\n' {
			return line.Slice(0, end)
		}
	}
	return line
}

// rope_ext.rs:295-299
func (r *extRope) sliceLines(start, end int) RopeSlice {
	s := r.lineStartOffset(start)
	e := r.lineEndOffset(max(end-1, 0))
	return r.Slice(s, e)
}

// rope_ext.rs:32-75 (RopeLines) collected eagerly.
func (r *extRope) iterLines() []string {
	var out []string
	for row := 0; row < r.linesLen(); row++ {
		out = append(out, r.sliceLine(row).String())
	}
	return out
}

func (r *extRope) lineLen(row int) int         { return r.sliceLine(row).Len() }         // :305-307
func (r *extRope) lineStartOffset(row int) int { return r.pointToOffset(point{row, 0}) } // :309-311
func (r *extRope) linesLen() int               { return r.LenLines(LF) }                 // :353-355

// rope_ext.rs:313-319
func (r *extRope) offsetToPoint(offset int) point {
	offset = r.clipOffset(offset, sumtree.Left)
	row := r.ByteToLineIdx(offset, LF)
	start := r.LineToByteIdx(row, LF)
	return point{row, max(offset-start, 0)}
}

// rope_ext.rs:321-328
func (r *extRope) pointToOffset(p point) int {
	if p.row >= r.linesLen() {
		return r.Len()
	}
	return r.LineToByteIdx(p.row, LF) + p.column
}

// rope_ext.rs:330-335
func (r *extRope) positionToOffset(p position) int {
	line := r.sliceLine(p.line)
	return r.lineStartOffset(p.line) + line.CharToByteIdx(min(p.character, line.LenChars()))
}

// rope_ext.rs:337-343
func (r *extRope) offsetToPosition(offset int) position {
	p := r.offsetToPoint(offset)
	line := r.sliceLine(p.row)
	off := line.UTF16ToByteIdx(line.ByteToUTF16Idx(p.column))
	return position{p.row, line.Slice(0, off).LenChars()}
}

// rope_ext.rs:345-351
func (r *extRope) lineEndOffset(row int) int {
	if row > r.linesLen() {
		return r.Len()
	}
	return r.lineStartOffset(row) + r.lineLen(row)
}

// rope_ext.rs:357-363
func (r *extRope) charAt(offset int) (rune, bool) {
	if offset > r.Len() {
		return 0, false
	}
	c, err := r.GetChar(offset)
	return c, err == nil
}

func isWordChar(c rune) bool { return unicode.IsLetter(c) || unicode.IsNumber(c) || c == '_' }

// rope_ext.rs:365-389
func (r *extRope) wordRange(offset int) (int, int, bool) {
	if offset >= r.Len() {
		return 0, 0, false
	}
	offset = r.clipOffset(offset, sumtree.Left)
	left := 0
	back := r.CharsAt(offset).Reversed()
	for c, ok := back.Next(); ok && isWordChar(c); c, ok = back.Next() {
		left += len(string(c))
	}
	right := 0
	fwd := r.CharsAt(offset)
	for c, ok := fwd.Next(); ok && isWordChar(c); c, ok = fwd.Next() {
		right += len(string(c))
	}
	start, end := max(offset-left, 0), offset+right
	return start, end, start != end
}

// rope_ext.rs:391-397
func (r *extRope) wordAt(offset int) string {
	if s, e, ok := r.wordRange(offset); ok {
		return r.Slice(s, e).String()
	}
	return ""
}

// rope_ext.rs:399-415
func (r *extRope) offsetUTF16ToOffset(u int) int {
	if u > r.LenUTF16() {
		return r.Len()
	}
	return r.UTF16ToByteIdx(u)
}

func (r *extRope) offsetToOffsetUTF16(offset int) int {
	if offset > r.Len() {
		return r.LenUTF16()
	}
	return r.ByteToUTF16Idx(offset)
}

// rope_ext.rs:417-422
func (r *extRope) replace(start, end int, text string) {
	start, end = r.clipOffset(start, sumtree.Left), r.clipOffset(end, sumtree.Right)
	r.Remove(start, end)
	r.Insert(start, text)
}

// rope_ext.rs:424-438
func (r *extRope) clipOffset(offset int, bias sumtree.Bias) int {
	if offset > r.Len() {
		return r.Len()
	}
	if r.IsCharBoundary(offset) {
		return offset
	}
	if bias == sumtree.Left {
		return r.FloorCharBoundary(offset)
	}
	return r.CeilCharBoundary(offset)
}

// rope_ext.rs:440-447
func (r *extRope) charIndexToOffset(c int) int {
	n := 0
	it := r.Chars()
	for ch, ok := it.Next(); ok && c > 0; ch, ok = it.Next() {
		n += len(string(ch))
		c--
	}
	return n
}

func (r *extRope) offsetToCharIndex(offset int) int {
	offset = r.clipOffset(offset, sumtree.Right)
	return r.Slice(0, offset).LenChars()
}

const sample = "Hello\nWorld\r\nThis is a test 中文\nRope"

func eqStr(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func eqInt(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

// rope_ext.rs:458-473 test_slice_line
func TestRopeExtSliceLine(t *testing.T) {
	r := ext(sample)
	eqStr(t, r.sliceLine(0).String(), "Hello")
	eqStr(t, r.sliceLine(1).String(), "World\r")
	eqStr(t, r.sliceLine(2).String(), "This is a test 中文")
	eqStr(t, r.sliceLine(3).String(), "Rope")
	eqStr(t, r.sliceLine(6).String(), "")
	r = ext("Hello\r")
	eqStr(t, r.sliceLine(0).String(), "Hello\r")
	eqStr(t, r.sliceLine(1).String(), "")
	// doc examples rope_ext.rs:123-131 (slice_lines)
	r = ext(sample)
	eqStr(t, r.sliceLines(0, 2).String(), "Hello\nWorld\r")
	eqStr(t, r.sliceLines(1, 3).String(), "World\r\nThis is a test 中文")
	eqStr(t, r.sliceLines(2, 5).String(), "This is a test 中文\nRope")
	eqStr(t, r.sliceLines(3, 10).String(), "Rope")
	eqStr(t, r.sliceLines(5, 10).String(), "")
}

// rope_ext.rs:475-487 test_lines_len
func TestRopeExtLinesLen(t *testing.T) {
	eqInt(t, ext(sample).linesLen(), 4)
	eqInt(t, ext("").linesLen(), 1)
	eqInt(t, ext("Single line").linesLen(), 1)
	eqInt(t, ext("Hello\r").linesLen(), 1)
}

// rope_ext.rs:489-497 test_lines
func TestRopeExtLines(t *testing.T) {
	got := ext("Hello\nWorld\r\nThis is a test 中文\nRope\r").iterLines()
	eqStr(t, strings.Join(got, "|"), "Hello|World\r|This is a test 中文|Rope\r")
}

// rope_ext.rs:499-507 test_eq
func TestRopeExtEq(t *testing.T) {
	r := FromStr(sample)
	if !r.Equal(FromStr(sample)) || r.Equal(FromStr("Hello\nWorld")) {
		t.Fatal("eq")
	}
	r1 := r
	if !r.Equal(r1) {
		t.Fatal("clone eq")
	}
}

// rope_ext.rs:509-519 test_iter_lines (skip(1).take(2))
func TestRopeExtIterLines(t *testing.T) {
	got := ext(sample).iterLines()[1:3]
	eqStr(t, strings.Join(got, "|"), "World\r|This is a test 中文")
}

// rope_ext.rs:521-538 test_line_start_end_offset
func TestRopeExtLineStartEndOffset(t *testing.T) {
	r := ext(sample)
	for _, c := range [][3]int{{0, 0, 5}, {1, 6, 12}, {2, 13, 34}, {3, 35, 39}, {4, 39, 39}} {
		eqInt(t, r.lineStartOffset(c[0]), c[1])
		eqInt(t, r.lineEndOffset(c[0]), c[2])
	}
}

// rope_ext.rs:540-565 test_line_column
func TestRopeExtLineColumn(t *testing.T) {
	r := ext("a 中文🎉 test\nRope")
	eqInt(t, r.positionToOffset(position{0, 3}), len("a 中"))
	eqInt(t, r.positionToOffset(position{0, 5}), len("a 中文🎉"))
	eqInt(t, r.positionToOffset(position{1, 1}), len("a 中文🎉 test\nR"))
	eqInt(t, r.positionToOffset(position{0, 1<<32 - 1}), len("a 中文🎉 test"))
	if p := r.offsetToPosition(len("a 中文🎉 test\nR")); p != (position{1, 1}) {
		t.Fatalf("offsetToPosition %+v", p)
	}
	if p := r.offsetToPosition(len("a 中文🎉")); p != (position{0, 5}) {
		t.Fatalf("offsetToPosition %+v", p)
	}
}

// rope_ext.rs:567-578 test_offset_to_point
func TestRopeExtOffsetToPoint(t *testing.T) {
	r := ext("a 中文🎉 test\nRope")
	for _, c := range []struct {
		off  int
		want point
	}{{0, point{0, 0}}, {1, point{0, 1}}, {len("a 中"), point{0, 5}}, {len("a 中文🎉"), point{0, 12}}, {len("a 中文🎉 test\nR"), point{1, 1}}} {
		if got := r.offsetToPoint(c.off); got != c.want {
			t.Fatalf("offsetToPoint(%d)=%+v want %+v", c.off, got, c.want)
		}
	}
}

// rope_ext.rs:580-591 test_point_to_offset
func TestRopeExtPointToOffset(t *testing.T) {
	r := ext("a 中文🎉 test\nRope")
	eqInt(t, r.pointToOffset(point{0, 0}), 0)
	eqInt(t, r.pointToOffset(point{0, 1}), 1)
	eqInt(t, r.pointToOffset(point{0, 5}), len("a 中"))
	eqInt(t, r.pointToOffset(point{0, 12}), len("a 中文🎉"))
	eqInt(t, r.pointToOffset(point{1, 1}), len("a 中文🎉 test\nR"))
}

// rope_ext.rs:593-603 test_char_at
func TestRopeExtCharAt(t *testing.T) {
	r := ext("Hello\nWorld\r\nThis is a test 中文🎉\nRope")
	for off, want := range map[int]rune{0: 'H', 5: '\n', 13: 'T', 28: '中', 34: '🎉', 38: '\n'} {
		if got, ok := r.charAt(off); !ok || got != want {
			t.Fatalf("charAt(%d)=%q,%v want %q", off, got, ok, want)
		}
	}
	if _, ok := r.charAt(50); ok {
		t.Fatal("charAt(50) should be None")
	}
}

// rope_ext.rs:605-623 test_word_at
func TestRopeExtWordAt(t *testing.T) {
	r := ext("Hello\nWorld\r\nThis is a test 中文 世界\nRope")
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
		eqStr(t, r.wordAt(c.off), c.word)
		s, e, ok := r.wordRange(c.off)
		if ok != c.ok || (ok && (s != c.start || e != c.end)) {
			t.Fatalf("wordRange(%d)=%d..%d,%v", c.off, s, e, ok)
		}
	}
}

// rope_ext.rs:625-639 test_offset_utf16_conversion
func TestRopeExtOffsetUTF16Conversion(t *testing.T) {
	r := ext("hello 中文🎉 test\nRope")
	eqInt(t, r.offsetToOffsetUTF16(len("hello")), 5)
	eqInt(t, r.offsetToOffsetUTF16(len("hello 中")), 7)
	eqInt(t, r.offsetToOffsetUTF16(len("hello 中文")), 8)
	eqInt(t, r.offsetToOffsetUTF16(len("hello 中文🎉")), 10)
	eqInt(t, r.offsetToOffsetUTF16(100), 20)
	eqInt(t, r.offsetUTF16ToOffset(5), len("hello"))
	eqInt(t, r.offsetUTF16ToOffset(7), len("hello 中"))
	eqInt(t, r.offsetUTF16ToOffset(8), len("hello 中文"))
	eqInt(t, r.offsetUTF16ToOffset(10), len("hello 中文🎉"))
	eqInt(t, r.offsetUTF16ToOffset(100), r.Len())
}

// rope_ext.rs:641-677 test_replace
func TestRopeExtReplace(t *testing.T) {
	r := ext(sample)
	r.replace(6, 11, "Universe")
	eqStr(t, r.String(), "Hello\nUniverse\r\nThis is a test 中文\nRope")
	r.replace(0, 5, "Hi")
	eqStr(t, r.String(), "Hi\nUniverse\r\nThis is a test 中文\nRope")
	r.replace(r.Len()-4, r.Len(), "String")
	eqStr(t, r.String(), "Hi\nUniverse\r\nThis is a test 中文\nString")
	for _, c := range []struct {
		s, e int
		want string
	}{{0, 1, "New文"}, {0, 2, "New文"}, {0, 3, "New文"}, {1, 4, "New"}} {
		r := ext("中文")
		r.replace(c.s, c.e, "New")
		eqStr(t, r.String(), c.want)
	}
}

// rope_ext.rs:679-700 test_clip_offset
func TestRopeExtClipOffset(t *testing.T) {
	r := ext("Hello 中文🎉 test\nRope")
	L, R := sumtree.Left, sumtree.Right
	for _, c := range []struct {
		off  int
		bias sumtree.Bias
		want int
	}{{5, L, 5}, {7, L, 6}, {7, R, 9}, {9, L, 9}, {13, L, 12}, {13, R, 16}, {16, L, 16}, {5, L, 5}, {5, R, 5}, {26, L, 26}, {100, L, 26}} {
		eqInt(t, r.clipOffset(c.off, c.bias), c.want)
	}
}

// rope_ext.rs:702-718 test_char_index_to_offset
func TestRopeExtCharIndexToOffset(t *testing.T) {
	r := ext("a 中文🎉 test\nRope")
	eqInt(t, r.charIndexToOffset(0), 0)
	eqInt(t, r.charIndexToOffset(1), 1)
	eqInt(t, r.charIndexToOffset(3), len("a 中"))
	eqInt(t, r.charIndexToOffset(5), len("a 中文🎉"))
	eqInt(t, r.charIndexToOffset(6), len("a 中文🎉 "))
	for off, want := range []int{0, 1, 2, 3, 3, 3, 4} {
		if off == 2 {
			continue // rope_ext.rs:711-716 asserts offsets 0,1,3,4,5,6 only
		}
		eqInt(t, r.offsetToCharIndex(off), want)
	}
	eqInt(t, r.offsetToCharIndex(10), 5)
}
