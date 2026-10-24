package gpui

import "testing"

// The core selection boundaries' own tests (browser-core ticket 19): the
// UAX #29 Latin-subset word segments the default seam computes and the
// paragraph rule of the triple click. The recorded browser fixtures
// (internal/parity/fixtures/selection) verify the same rules against
// headless Chrome on sample text; these tables pin the rules directly,
// including the shapes the fixtures do not spell out.

func TestChromeWordRangeAtSegmentsLatinText(t *testing.T) {
	cases := []struct {
		text   string
		offset int
		want   TextRange
	}{
		// WB5: letter runs are one word.
		{"Alpha beta gamma delta", 0, TextRange{Start: 0, End: 5}},
		{"Alpha beta gamma delta", 3, TextRange{Start: 0, End: 5}},
		{"Alpha beta gamma delta", 6, TextRange{Start: 6, End: 10}},
		// Accented letters are ALetter.
		{"one café, three", 6, TextRange{Start: 4, End: 9}},
		// WB9/WB10: letters and digits join.
		{"sqlite3 rocks", 4, TextRange{Start: 0, End: 7}},
		{"v2 release", 1, TextRange{Start: 0, End: 2}},
		// WB8 with WB11/WB12: digits around a full stop or comma.
		{"pi = 3.14", 8, TextRange{Start: 5, End: 9}},
		{"1,000 items", 2, TextRange{Start: 0, End: 5}},
		// WB6/WB7: an apostrophe between letters joins (Single_Quote).
		{"don't stop", 3, TextRange{Start: 0, End: 5}},
		// WB6/WB7: a full stop between letters joins (MidNumLet).
		{"example.com", 4, TextRange{Start: 0, End: 11}},
		// WB13a/WB13b: the underscore joins (ExtendNumLet).
		{"hello_world", 5, TextRange{Start: 0, End: 11}},
		// Punctuation without a join on either side is its own segment:
		// the comma after "café" (MidNum, no digits around it).
		{"one café, three", 9, TextRange{Start: 9, End: 10}},
		// A caret exactly on a boundary takes the segment after it
		// (kNextWordIfOnBoundary, position_units_word.cc:269-281).
		{"word, more", 4, TextRange{Start: 4, End: 5}},
		{"word, more", 5, TextRange{Start: 5, End: 6}},
		// WB3d: a run of spaces is one segment.
		{"a  b", 1, TextRange{Start: 1, End: 3}},
		// The caret past the last character takes the last segment
		// (the clipped caret).
		{"last word", 9, TextRange{Start: 5, End: 9}},
		// Each character of a punctuation run is its own segment (no
		// rule joins consecutive mid punctuation).
		{"foo...", 4, TextRange{Start: 4, End: 5}},
		{"foo...", 3, TextRange{Start: 3, End: 4}},
		// A mid punctuation that does not sit between joinable
		// characters breaks on both sides: "a..b" joins neither dot (the
		// second dot's lookbehind is a dot, not a letter).
		{"a..b", 1, TextRange{Start: 1, End: 2}},
	}
	for _, c := range cases {
		if got, ok := chromeWordRangeAt(c.text, c.offset); !ok || got != c.want {
			t.Errorf("chromeWordRangeAt(%q, %d) = %v, %v; want %v", c.text, c.offset, got, ok, c.want)
		}
	}
}

func TestChromeWordRangeAtPunctuationBetweenJoinableClasses(t *testing.T) {
	// The lookahead/lookbehard rules: "example.com" keeps the stop
	// (letters around it), but "a.2" breaks on both sides of the stop
	// (a letter before and a digit after join nothing), and "1.x" breaks
	// too (a digit before and a letter after join nothing — WB9/WB10
	// only join DIRECT neighbours).
	cases := []struct {
		text   string
		offset int
		want   TextRange
	}{
		{"a.2", 1, TextRange{Start: 1, End: 2}},
		{"1.x", 1, TextRange{Start: 1, End: 2}},
		{"3.14", 1, TextRange{Start: 0, End: 4}},
		{"end.", 3, TextRange{Start: 3, End: 4}},
	}
	for _, c := range cases {
		if got, ok := chromeWordRangeAt(c.text, c.offset); !ok || got != c.want {
			t.Errorf("chromeWordRangeAt(%q, %d) = %v, %v; want %v", c.text, c.offset, got, ok, c.want)
		}
	}
}

func TestChromeWordRangeAtDocumentsCJKAndEmojiAsLimitations(t *testing.T) {
	// The documented limitation: without ICU's dictionary tailoring, each
	// Han character is its own segment (the un-tailored UAX #29 result —
	// no rule joins them), and an emoji is Other.
	if got, ok := chromeWordRangeAt("你好", 0); !ok || got != (TextRange{Start: 0, End: 3}) {
		t.Errorf("chromeWordRangeAt(\"你好\", 0) = %v, %v", got, ok)
	}
	if got, ok := chromeWordRangeAt("a🙂b", 3); !ok || got != (TextRange{Start: 1, End: 5}) {
		t.Errorf("chromeWordRangeAt(\"a🙂b\", 3) = %v, %v", got, ok)
	}
}

func TestChromeParagraphRangeAtIncludesTheBreak(t *testing.T) {
	cases := []struct {
		text   string
		offset int
		want   TextRange
	}{
		// The line plus its break when another line follows
		// (selection_adjuster.cc:318-334).
		{"first line\nsecond line\nthird", 15, TextRange{Start: 11, End: 23}},
		// The final line has no break to take.
		{"first line\nsecond line", 15, TextRange{Start: 11, End: 22}},
		{"only line", 4, TextRange{Start: 0, End: 9}},
		// \r\n is one break.
		{"first\r\nsecond", 4, TextRange{Start: 0, End: 7}},
	}
	for _, c := range cases {
		if got := chromeParagraphRangeAt(c.text, c.offset); got != c.want {
			t.Errorf("chromeParagraphRangeAt(%q, %d) = %v, want %v", c.text, c.offset, got, c.want)
		}
	}
}

func TestExtendWordOverTrailingWhitespace(t *testing.T) {
	cases := []struct {
		text string
		end  int
		want int
	}{
		// SkipWhitespace: the trailing spaces and tabs join, the line
		// break does not (selection_controller.cc:187-209).
		{"beta gamma", 4, 5},
		{"beta\t\tgamma", 4, 6},
		{"beta \n gamma", 4, 5},
		{"beta", 4, 4},
		{"beta,", 4, 4},
	}
	for _, c := range cases {
		if got := extendWordOverTrailingWhitespace(c.text, c.end); got != c.want {
			t.Errorf("extendWordOverTrailingWhitespace(%q, %d) = %d, want %d", c.text, c.end, got, c.want)
		}
	}
}
