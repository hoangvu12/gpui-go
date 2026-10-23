package base

import "testing"

// Ports of the U:text/selection.rs tests (112-172): 2 tests, 2 ported.

// U:text/selection.rs:116-140
func TestTextCharTypeFromChar(t *testing.T) {
	cases := map[rune]textCharType{
		'a': textCharWord, 'Z': textCharWord, '0': textCharWord, '_': textCharWord,
		'.': textCharOther, ',': textCharOther, ';': textCharOther, '!': textCharOther,
		'?': textCharOther, '[': textCharOther, '{': textCharOther,
		' ': textCharWhitespace, '\t': textCharWhitespace, ' ': textCharWhitespace,
		'\n': textCharNewline, '\r': textCharNewline,
		'汉': textCharOther,
		'é': textCharWord, 'ä': textCharWord, 'ö': textCharWord, 'ü': textCharWord, 'д': textCharWord,
	}
	for c, want := range cases {
		if got := textCharTypeOf(c); got != want {
			t.Errorf("%q: %v, want %v", c, got, want)
		}
	}
}

// U:text/selection.rs:142-171
func TestTextSelectionWordRangeAt(t *testing.T) {
	text := "test text\nabcde 中文🎉 test\nhello[()]\ntest_connector ____\nRope\nrök\ngrande île"
	tests := []struct {
		offset int
		want   string
	}{
		{0, "test"}, {4, " "}, {10, "abcde"}, {15, " "}, {16, "中"}, {19, "文"},
		{22, "🎉"}, {27, "test"}, {37, "["}, {38, "("}, {39, ")"}, {40, "]"},
		{42, "test_connector"}, {56, " "}, {57, "____"}, {62, "Rope"}, {67, "rök"}, {79, "île"},
	}
	for _, tc := range tests {
		r, ok := textSelectionWordRangeAt(text, tc.offset)
		if !ok || text[r.Start:r.End] != tc.want {
			t.Errorf("offset %d: %q (%v), want %q", tc.offset, text[r.Start:r.End], ok, tc.want)
		}
	}
}
