package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
)

// The 2 tests of U:input/base/selection.rs:92-165.

// U:input/base/selection.rs:97-144 test_word_range. The indoc! literal
// strips the common indentation and the leading newline.
func TestTextSelectorWordRange(t *testing.T) {
	r := rope.FromStr("test text:\nabcde 中文🎉 test\nhello[()]\ntest_connector ____\nRope\nrök\ngrande île\n")
	ext := RopeExtOf(&r)
	for _, c := range []struct {
		line, column int
		want         string
	}{
		{0, 0, "test"}, {0, 4, " "}, {1, 0, "abcde"}, {1, 4, "abcde"}, {1, 5, " "},
		{1, 6, "中"}, {1, 9, "文"}, {1, 13, "🎉"}, {1, 20, "test"},
		{2, 5, "["}, {2, 6, "("}, {2, 7, ")"}, {2, 8, "]"},
		{3, 5, "test_connector"}, {3, 14, " "}, {3, 16, "____"},
		{4, 0, "Rope"}, {5, 0, "rök"}, {6, 8, "île"},
	} {
		offset := ext.LineStartOffset(c.line) + c.column
		s, e, ok := textSelectorWordRange(&r, offset)
		if !ok || r.Slice(s, e).String() != c.want {
			t.Fatalf("line %d, column %d: got %q (ok=%v) want %q", c.line, c.column, r.Slice(s, e).String(), ok, c.want)
		}
	}
}

// U:input/base/selection.rs:146-164 test_line_range.
func TestTextSelectorLineRange(t *testing.T) {
	r := rope.FromStr("first line\nsecond line\nthird")
	ext := RopeExtOf(&r)
	for _, c := range []struct {
		line, column int
		want         string
	}{{0, 0, "first line"}, {0, 5, "first line"}, {1, 3, "second line"}, {2, 1, "third"}} {
		s, e := textSelectorLineRange(&r, ext.LineStartOffset(c.line)+c.column)
		inpEqStr(t, r.Slice(s, e).String(), c.want)
	}
}
