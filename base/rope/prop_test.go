package rope

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

// Ports of ropey/tests/proptest_tests.rs for the LF/char/utf16 metric set,
// driven by seeded math/rand instead of proptest. Case counts are modest
// (ropey uses 512) to keep the run cheap.
const propCases = 150

// pt_from_str (proptest_tests.rs:124-134), plus every slice of the result.
func TestPropFromStr(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < propCases; i++ {
		text := randText(r, 120)
		rp := FromStr(text)
		checkInvariants(t, rp)
		checkMetrics(t, rp.RopeSlice, text)
		a, b := boundaryIn(r, text), boundaryIn(r, text)
		a, b = min(a, b), max(a, b)
		checkMetrics(t, rp.Slice(a, b), text[a:b])
	}
}

// pt_insert (proptest_tests.rs:148-164).
func TestPropInsert(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	base := randText(r, 200)
	for i := 0; i < propCases; i++ {
		rp := FromStr(base)
		idx := boundaryIn(r, base)
		ins := randText(r, 50)
		rp.Insert(idx, ins)
		want := base[:idx] + ins + base[idx:]
		checkInvariants(t, rp)
		checkMetrics(t, rp.RopeSlice, want)
	}
}

// pt_remove (proptest_tests.rs:186-206).
func TestPropRemove(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	base := randText(r, 200)
	for i := 0; i < propCases; i++ {
		rp := FromStr(base)
		a, b := boundaryIn(r, base), boundaryIn(r, base)
		a, b = min(a, b), max(a, b)
		rp.Remove(a, b)
		checkInvariants(t, rp)
		checkMetrics(t, rp.RopeSlice, base[:a]+base[b:])
	}
}

// pt_chunks_iter_next and pt_slice_chunks_iter_next (proptest_tests.rs:293-329),
// plus the reversed direction.
func TestPropChunks(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	for i := 0; i < propCases; i++ {
		text := randText(r, 150)
		rp := FromStr(text)
		a, b := boundaryIn(r, text), boundaryIn(r, text)
		a, b = min(a, b), max(a, b)
		for _, c := range []struct {
			s    RopeSlice
			text string
		}{{rp.RopeSlice, text}, {rp.Slice(a, b), text[a:b]}} {
			idx := 0
			it := c.s.Chunks()
			for ch, ok := it.Next(); ok; ch, ok = it.Next() {
				if ch == "" || ch != c.text[idx:idx+len(ch)] {
					t.Fatalf("chunk %q at %d", ch, idx)
				}
				idx += len(ch)
			}
			if idx != len(c.text) {
				t.Fatalf("chunks covered %d of %d", idx, len(c.text))
			}
			back := it.Reversed()
			for ch, ok := back.Next(); ok; ch, ok = back.Next() {
				idx -= len(ch)
				if ch != c.text[idx:idx+len(ch)] {
					t.Fatalf("reversed chunk %q at %d", ch, idx)
				}
			}
			if idx != 0 {
				t.Fatalf("reversed chunks ended at %d", idx)
			}
		}
	}
}

// pt_lines_iter_next/prev and the slice variants (proptest_tests.rs:332-454),
// for LineType::LF.
func TestPropLines(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for i := 0; i < propCases; i++ {
		text := strings.Repeat("\n", r.Intn(4)) + randText(r, 60) + strings.Repeat("\n", r.Intn(4)) + randText(r, 60)
		rp := FromStr(text)
		a, b := boundaryIn(r, text), boundaryIn(r, text)
		a, b = min(a, b), max(a, b)
		for _, c := range []struct {
			s    RopeSlice
			text string
		}{{rp.RopeSlice, text}, {rp.Slice(a, b), text[a:b]}} {
			want := strings.SplitAfter(c.text, "\n") // last element is the (possibly empty) last line
			it := c.s.Lines(LF)
			var got []string
			for l, ok := it.Next(); ok; l, ok = it.Next() {
				got = append(got, l.String())
			}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("lines %q want %q", got, want)
			}
			back := c.s.LinesAt(c.s.LenLines(LF), LF).Reversed()
			k := len(want)
			for l, ok := back.Next(); ok; l, ok = back.Next() {
				k--
				if l.String() != want[k] {
					t.Fatalf("reversed line %d = %q want %q", k, l.String(), want[k])
				}
			}
			if k != 0 {
				t.Fatalf("reversed lines stopped at %d", k)
			}
		}
	}
}

// pt_char_indices_iter and pt_char_indices_iter_reversed
// (proptest_tests.rs:456-518).
func TestPropCharIndices(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	for i := 0; i < propCases; i++ {
		text := randText(r, 120)
		s := FromStr(text).RopeSlice
		off := 0
		it := s.CharIndices()
		for idx, ch, ok := it.Next(); ok; idx, ch, ok = it.Next() {
			if idx != off {
				t.Fatalf("forward idx %d want %d", idx, off)
			}
			if want, _ := utf8.DecodeRuneInString(text[off:]); want != ch {
				t.Fatalf("forward char %q want %q", ch, want)
			}
			off += utf8.RuneLen(ch)
		}
		if off != len(text) {
			t.Fatalf("forward ended at %d", off)
		}
		back := s.CharIndicesAt(s.Len())
		for idx, ch, ok := back.Prev(); ok; idx, ch, ok = back.Prev() {
			off -= utf8.RuneLen(ch)
			if idx != off {
				t.Fatalf("backward idx %d want %d", idx, off)
			}
		}
		if off != 0 {
			t.Fatalf("backward ended at %d", off)
		}
		rev := s.CharIndicesAt(s.Len()).Reversed()
		off = len(text)
		for idx, ch, ok := rev.Next(); ok; idx, ch, ok = rev.Next() {
			off -= utf8.RuneLen(ch)
			if idx != off {
				t.Fatalf("reversed idx %d want %d", idx, off)
			}
		}
		fwd := s.CharIndices().Reversed()
		for idx, ch, ok := fwd.Prev(); ok; idx, ch, ok = fwd.Prev() {
			if idx != off {
				t.Fatalf("reversed-prev idx %d want %d", idx, off)
			}
			off += utf8.RuneLen(ch)
		}
		if off != len(text) {
			t.Fatalf("reversed-prev ended at %d", off)
		}
	}
}

// Random edit sequences against a string model, with O(1) clones taken along
// the way that must stay unchanged (persistence; Base clones the rope for undo
// and snapshots, deps-03-rope-lsp-misc.md §1).
func TestPropEditSequenceAndClones(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for seq := 0; seq < 40; seq++ {
		text := randText(r, 80)
		rp := FromStr(text)
		type snap struct {
			r    Rope
			text string
		}
		var snaps []snap
		for step := 0; step < 30; step++ {
			if r.Intn(4) == 0 {
				snaps = append(snaps, snap{rp, text})
			}
			if r.Intn(2) == 0 {
				idx := boundaryIn(r, text)
				ins := randText(r, 40)
				rp.Insert(idx, ins)
				text = text[:idx] + ins + text[idx:]
			} else {
				a, b := boundaryIn(r, text), boundaryIn(r, text)
				a, b = min(a, b), max(a, b)
				rp.Remove(a, b)
				text = text[:a] + text[b:]
			}
			checkInvariants(t, rp)
			if rp.String() != text {
				t.Fatalf("seq %d step %d: %q want %q", seq, step, rp.String(), text)
			}
		}
		checkMetrics(t, rp.RopeSlice, text)
		for _, s := range snaps {
			if s.r.String() != s.text {
				t.Fatalf("clone changed after edits: %q want %q", s.r.String(), s.text)
			}
		}
	}
}

// Char iteration from an arbitrary position, both directions, against the
// model (the chars_at(..).reversed() pattern of rope_ext.rs:372 and
// selection.rs:86).
func TestPropCharsAt(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	for i := 0; i < propCases; i++ {
		text := randText(r, 100)
		s := FromStr(text).RopeSlice
		at := boundaryIn(r, text)
		var fwd []rune
		it := s.CharsAt(at)
		for c, ok := it.Next(); ok; c, ok = it.Next() {
			fwd = append(fwd, c)
		}
		if string(fwd) != text[at:] {
			t.Fatalf("chars_at(%d) forward %q want %q", at, string(fwd), text[at:])
		}
		var back []rune
		rev := s.CharsAt(at).Reversed()
		for c, ok := rev.Next(); ok; c, ok = rev.Next() {
			back = append([]rune{c}, back...)
		}
		if string(back) != text[:at] {
			t.Fatalf("chars_at(%d).reversed() %q want %q", at, string(back), text[:at])
		}
	}
}

func TestLargeTextBuildsDeepTree(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	text := randText(r, 4000)
	rp := FromStr(text)
	checkInvariants(t, rp)
	checkMetrics(t, rp.RopeSlice, text)
	depth := 0
	for n := rp.tree(); !n.leaf; n = n.children[0] {
		depth++
	}
	if depth < 3 {
		t.Fatalf("expected a deep tree with test constants, depth %d", depth)
	}
	// One big insert in the middle, then remove most of it again.
	big := randText(r, 3000)
	mid := boundaryIn(r, text)
	rp.Insert(mid, big)
	checkInvariants(t, rp)
	want := text[:mid] + big + text[mid:]
	if rp.String() != want {
		t.Fatal("big insert mismatch")
	}
	a := boundaryIn(r, want[:len(want)/4])
	b := len(want) - len(want)/4
	for b < len(want) && !utf8.RuneStart(want[b]) {
		b++
	}
	rp.Remove(a, b)
	checkInvariants(t, rp)
	checkMetrics(t, rp.RopeSlice, want[:a]+want[b:])
}
