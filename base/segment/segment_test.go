package segment

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode"
)

func wordEnds(s string) []int {
	out := []int{}
	for i, seg := range SplitWordBoundIndices(s) {
		out = append(out, i+len(seg))
	}
	return out
}

func graphemeEnds(s string) []int {
	out := []int{}
	for i, g := range GraphemeIndices(s) {
		out = append(out, i+len(g))
	}
	return out
}

// TestRustOracleGolden replays recorded unicode-segmentation 1.13.3 output
// (oracle source: .scratch/gpui-base/research/oracles/segmentation/src/main.rs,
// `split_word_bound_indices` and `grapheme_indices(true)` segment ends).
// testdata holds every UCD 17.0.0 WordBreakTest/GraphemeBreakTest string, the
// 9 Base test strings, a 1-in-40 sample of the 50,000 seeded random strings,
// and the 5 strings where the crates disagree.
func TestRustOracleGolden(t *testing.T) {
	f, err := os.Open("testdata/unicode-segmentation-1.13.3.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	total, deviations := 0, 0
	for sc.Scan() {
		var c struct {
			S string `json:"s"`
			W []int  `json:"w"`
			G []int  `json:"g"`
		}
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		total++
		if got := graphemeEnds(c.S); !slices.Equal(got, c.G) {
			t.Errorf("graphemes(%q) ends %v, rust %v", c.S, got, c.G)
		}
		got := wordEnds(c.S)
		if slices.Equal(got, c.W) {
			continue
		}
		// The recorded deviation: uax29 splits after the letter before
		// MidLetter/MidNumLet + ZWJ + Extended_Pictographic; the Rust crate
		// keeps one segment. Every Rust boundary must still be present.
		if !strings.Contains(c.S, "‍") || len(got) <= len(c.W) || !subset(c.W, got) {
			t.Errorf("word bounds(%q) ends %v, rust %v", c.S, got, c.W)
			continue
		}
		deviations++
	}
	if total < 3900 || deviations != 5 {
		t.Fatalf("checked %d strings with %d known deviations; want >= 3900 and exactly 5", total, deviations)
	}
}

func subset(a, b []int) bool {
	for _, v := range a {
		if !slices.Contains(b, v) {
			return false
		}
	}
	return true
}

// The documented deviation, pinned (deps-02-segmentation-html.md §Results).
func TestKnownWordBoundDeviation(t *testing.T) {
	var segs []string
	for seg := range SplitWordBounds("q.‍👨") {
		segs = append(segs, seg)
	}
	if !slices.Equal(segs, []string{"q", ".‍👨"}) {
		t.Fatalf("got %q", segs)
	}
}

func blank(s string) bool { return strings.TrimLeftFunc(s, unicode.IsSpace) == "" }

// Emulates state.rs:1455-1465 previous_start_of_word_at before token_boundary:
// `split_word_bound_indices(left).rfind(|(_, s)| !s.trim_start().is_empty())`.
func prevStartOfWord(text string, offset int) int {
	type seg struct {
		i int
		s string
	}
	var segs []seg
	for i, s := range SplitWordBoundIndices(text[:offset]) {
		segs = append(segs, seg{i, s})
	}
	for k := len(segs) - 1; k >= 0; k-- {
		if !blank(segs[k].s) {
			return segs[k].i
		}
	}
	return 0
}

// Emulates state.rs:1472-1482 next_end_of_word_at before token_boundary.
func nextEndOfWord(text string, offset int) int {
	for i, s := range SplitWordBoundIndices(text[offset:]) {
		if !blank(s) {
			return offset + i + len(s)
		}
	}
	return len(text)
}

// Expected values are the Rust test assertions named below
// (input/base/state.rs tests); token tests list the raw value before
// token_boundary widens it.
func TestBaseWordMotionCases(t *testing.T) {
	for _, c := range []struct {
		name      string
		text      string
		off, want int
		next      bool
	}{
		{"test_multi_cursor_word_movement next(2)", "one two three", 2, 3, true},
		{"test_multi_cursor_word_movement next(5)", "one two three", 5, 7, true},
		{"test_multi_cursor_word_movement prev(3)", "one two three", 3, 0, false},
		{"test_multi_cursor_word_movement prev(7)", "one two three", 7, 4, false},
		{"test_masked_input_collapses_word_boundaries (unmasked)", "aaa bbb ccc", 11, 8, false},
		{"test_undo_manager_word_and_line_deletes_do_not_coalesce", "one two three\nfour five", 13, 8, false},
		{"test_word_delete_undo_restores_caret", "hello", 5, 0, false},
		{"test_inline_token_edit_history_and_validation next raw", "问 @alice!", 4, 5, true},
		{"test_inline_token_edit_history_and_validation prev raw", "问 @alice!", 10, 5, false},
	} {
		got := prevStartOfWord(c.text, c.off)
		if c.next {
			got = nextEndOfWord(c.text, c.off)
		}
		if got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

// inline_tokens.rs:197-198: an offset is a valid token boundary when it is
// the text end or a grapheme start.
func isGraphemeBoundary(text string, offset int) bool {
	if offset == len(text) {
		return true
	}
	for i := range GraphemeIndices(text) {
		if i == offset {
			return true
		}
	}
	return false
}

func TestBaseGraphemeBoundaryCases(t *testing.T) {
	for _, c := range []struct {
		text string
		off  int
		want bool
	}{
		{"á", 1, false}, // test_inline_token_edit_history_and_validation InvalidBoundary
		{"á", 0, true},
		{"á", 3, true},
		{"@a@a", 2, true},
		{"中@b\n后面", 3, true},
		{"中@b\n后面", 5, true},
		{"👨‍👩‍👧", 4, false},
		{"🇻🇳🇺🇸", 8, true},
		{"🇻🇳🇺🇸", 4, false},
		{"\r\n", 1, false},
	} {
		if got := isGraphemeBoundary(c.text, c.off); got != c.want {
			t.Errorf("%q@%d: got %v", c.text, c.off, got)
		}
	}
}

// split_word_bounds yields every segment, whitespace and punctuation included
// (the shape inline_flow.rs:1483 relies on).
func TestSplitWordBoundsShape(t *testing.T) {
	var got []string
	for s := range SplitWordBounds("foo  bar, baz's 3.14 @x!") {
		got = append(got, s)
	}
	want := []string{"foo", "  ", "bar", ",", " ", "baz's", " ", "3.14", " ", "@", "x", "!"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	var gs []string
	for g := range Graphemes("éx🇻🇳") {
		gs = append(gs, g)
	}
	if !slices.Equal(gs, []string{"é", "x", "🇻🇳"}) {
		t.Fatalf("graphemes %q", gs)
	}
	// Early break is honoured.
	for range SplitWordBoundIndices("a b") {
		break
	}
}
