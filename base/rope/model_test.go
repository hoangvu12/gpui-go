package rope

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

// Brute-force string model of every metric, used by the property tests.

func modelCharIdx(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return utf8.RuneCountInString(s[:i])
}

func modelUTF16Idx(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	n := 0
	for _, r := range s[:i] {
		n += utf16Len(r)
	}
	return n
}

func utf16Len(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

func modelCharToByte(s string, c int) int {
	for i := range s {
		if c == 0 {
			return i
		}
		c--
	}
	return len(s)
}

func modelUTF16ToByte(s string, u int) int {
	seen := 0
	for i, r := range s {
		if u < seen+utf16Len(r) {
			return i
		}
		seen += utf16Len(r)
	}
	return len(s)
}

func modelLineToByte(s string, line int) int {
	if line == 0 {
		return 0
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line--
			if line == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

// checkMetrics compares every query of t against the model text.
func checkMetrics(t *testing.T, sl RopeSlice, text string) {
	t.Helper()
	if !sl.EqualString(text) || sl.String() != text {
		t.Fatalf("text mismatch: got %q want %q", sl.String(), text)
	}
	if sl.Len() != len(text) || sl.LenChars() != utf8.RuneCountInString(text) ||
		sl.LenUTF16() != modelUTF16Idx(text, len(text)) || sl.LenLines(LF) != strings.Count(text, "\n")+1 {
		t.Fatalf("len metrics mismatch for %q: %d %d %d %d", text, sl.Len(), sl.LenChars(), sl.LenUTF16(), sl.LenLines(LF))
	}
	for i := 0; i <= len(text); i++ {
		if got, want := sl.IsCharBoundary(i), i == len(text) || utf8.RuneStart(text[i]); got != want {
			t.Fatalf("IsCharBoundary(%d)=%v", i, got)
		}
		if got, want := sl.ByteToCharIdx(i), modelCharIdx(text, i); got != want {
			t.Fatalf("ByteToCharIdx(%d)=%d want %d (%q)", i, got, want, text)
		}
		if got, want := sl.ByteToUTF16Idx(i), modelUTF16Idx(text, i); got != want {
			t.Fatalf("ByteToUTF16Idx(%d)=%d want %d", i, got, want)
		}
		if got, want := sl.ByteToLineIdx(i, LF), strings.Count(text[:i], "\n"); got != want {
			t.Fatalf("ByteToLineIdx(%d)=%d want %d", i, got, want)
		}
	}
	for c := 0; c <= utf8.RuneCountInString(text); c++ {
		if got, want := sl.CharToByteIdx(c), modelCharToByte(text, c); got != want {
			t.Fatalf("CharToByteIdx(%d)=%d want %d", c, got, want)
		}
	}
	for u := 0; u <= sl.LenUTF16(); u++ {
		if got, want := sl.UTF16ToByteIdx(u), modelUTF16ToByte(text, u); got != want {
			t.Fatalf("UTF16ToByteIdx(%d)=%d want %d (%q)", u, got, want, text)
		}
	}
	for l := 0; l <= sl.LenLines(LF); l++ {
		if got, want := sl.LineToByteIdx(l, LF), modelLineToByte(text, l); got != want {
			t.Fatalf("LineToByteIdx(%d)=%d want %d", l, got, want)
		}
	}
}

// checkInvariants mirrors Rope::assert_invariants (ropey/src/rope.rs:418-425)
// for the parts that apply: equal leaf depth, accurate summaries, no empty
// internal node, no empty non-root leaf, bounded fan-out, char-aligned leaves.
func checkInvariants(t *testing.T, r Rope) {
	t.Helper()
	root := r.tree()
	if !root.leaf && len(root.children) == 1 {
		t.Fatalf("root has a single child")
	}
	var depth = -1
	var walk func(n *node, d int, isRoot bool) textInfo
	walk = func(n *node, d int, isRoot bool) textInfo {
		if n.leaf {
			if depth < 0 {
				depth = d
			} else if depth != d {
				t.Fatalf("unequal leaf depth %d vs %d", depth, d)
			}
			if !isRoot && n.text == "" {
				t.Fatalf("empty non-root leaf")
			}
			if len(n.text) > maxText {
				t.Fatalf("leaf of %d bytes > %d", len(n.text), maxText)
			}
			if !utf8.ValidString(n.text) {
				t.Fatalf("leaf splits a char: %q", n.text)
			}
			if infoOf(n.text) != n.info {
				t.Fatalf("leaf info stale")
			}
			return n.info
		}
		if len(n.children) == 0 || len(n.children) > maxChildren {
			t.Fatalf("internal node with %d children", len(n.children))
		}
		var sum textInfo
		for _, c := range n.children {
			sum = sum.add(walk(c, d+1, false))
		}
		if sum != n.info {
			t.Fatalf("internal info stale: %+v vs %+v", sum, n.info)
		}
		return sum
	}
	walk(root, 0, true)
}

// alphabet mixes ASCII, CR/LF, 2-, 3- and 4-byte chars.
var alphabet = []string{"a", "b", "Z", " ", "\n", "\r", "\r\n", "é", "ж", "中", "文", "🎉", "😀", "́", "\t"}

func randText(r *rand.Rand, max int) string {
	n := r.Intn(max + 1)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(alphabet[r.Intn(len(alphabet))])
	}
	return b.String()
}

func boundaryIn(r *rand.Rand, s string) int {
	i := r.Intn(len(s) + 1)
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
}

func seedName(seed int64) string { return fmt.Sprintf("seed=%d", seed) }
