package segment

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

// text_boundary.rs has no upstream tests; these pin its behaviour by hand
// and against a brute-force rune-slice model.

func TestWordRangeAt(t *testing.T) {
	text := "foo_bar  baz, Привет café\nnext"
	for _, c := range []struct {
		off, start, end int
		ok              bool
	}{
		{0, 0, 7, true},    // word with underscore
		{5, 0, 7, true},    // inside the word
		{7, 7, 9, true},    // whitespace run
		{12, 12, 13, true}, // "," is Other: just itself
		{14, 14, 26, true}, // Cyrillic word (2-byte chars)
		{16, 14, 26, true}, // mid-char offset clips left
		{27, 27, 32, true}, // "café" (é in U+00C0..U+024F)
		{len("foo_bar  baz, Привет café"), 32, 33, true}, // "\n" is Newline: never joins
		{len(text), 0, 0, false},
		{len(text) + 5, 0, 0, false},
	} {
		s, e, ok := WordRangeAt(text, c.off)
		if ok != c.ok || ok && (s != c.start || e != c.end) {
			t.Errorf("WordRangeAt(%d) = %d..%d,%v want %d..%d,%v (%q)", c.off, s, e, ok, c.start, c.end, c.ok, text[s:e])
		}
	}
	// CJK is Other, so each char stands alone.
	if s, e, _ := WordRangeAt("中文", 3); s != 3 || e != 6 {
		t.Errorf("CJK range %d..%d", s, e)
	}
}

func TestLineRangeAt(t *testing.T) {
	text := "one\ntwo\n\nfour"
	for _, c := range [][3]int{{0, 0, 3}, {3, 0, 3}, {4, 4, 7}, {8, 8, 8}, {9, 9, 13}, {13, 9, 13}, {99, 9, 13}} {
		if s, e := LineRangeAt(text, c[0]); s != c[1] || e != c[2] {
			t.Errorf("LineRangeAt(%d)=%d..%d want %d..%d", c[0], s, e, c[1], c[2])
		}
	}
}

func TestWordRangeFrom128CharCap(t *testing.T) {
	text := strings.Repeat("a", 300)
	s, e, _ := WordRangeAt(text, 150)
	if s != 150-128 || e != 151+128 {
		t.Fatalf("cap: %d..%d", s, e)
	}
}

// Brute-force model over runes for WordRangeAt.
func modelWordRange(text string, off int) (int, int, bool) {
	for off > 0 && off < len(text) && !utf8.RuneStart(text[off]) {
		off--
	}
	if off >= len(text) {
		return 0, 0, false
	}
	type rc struct {
		start int
		r     rune
	}
	var rs []rc
	for i, r := range text {
		rs = append(rs, rc{i, r})
	}
	k := 0
	for rs[k].start != off {
		k++
	}
	kind := kindOf(rs[k].r)
	join := func(r rune) bool {
		o := kindOf(r)
		return (kind == kindWord || kind == kindWhitespace) && o == kind
	}
	lo, hi := k, k
	for lo > 0 && k-lo < 128 && join(rs[lo-1].r) {
		lo--
	}
	for hi+1 < len(rs) && hi-k < 128 && join(rs[hi+1].r) {
		hi++
	}
	return rs[lo].start, rs[hi].start + utf8.RuneLen(rs[hi].r), true
}

func TestWordRangeAtAgainstModel(t *testing.T) {
	pool := []string{"a", "Z", "9", "_", " ", "\t", "　", "\n", "\r", ",", "é", "Ж", "́", "中", "🎉", "ß", "ẞ"}
	rng := rand.New(rand.NewSource(21))
	for i := 0; i < 300; i++ {
		var b strings.Builder
		for n := rng.Intn(40); n > 0; n-- {
			b.WriteString(pool[rng.Intn(len(pool))])
		}
		text := b.String()
		for off := 0; off <= len(text); off++ {
			s, e, ok := WordRangeAt(text, off)
			ms, me, mok := modelWordRange(text, off)
			if ok != mok || s != ms || e != me {
				t.Fatalf("%q@%d: got %d..%d,%v model %d..%d,%v", text, off, s, e, ok, ms, me, mok)
			}
		}
	}
}
