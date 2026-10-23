package rope

import (
	"errors"
	"testing"
)

// Doc examples from ropey 2.0.0-beta.1, restricted to the LF metric set.
func TestRopeyDocExamples(t *testing.T) {
	// ropey/src/rope.rs:36-44
	r := FromStr("Hello みんなさん!")
	r.Remove(6, 21)
	r.Insert(6, "world")
	if !r.EqualString("Hello world!") {
		t.Fatalf("edit example: %q", r.String())
	}
	// ropey/src/rope.rs:72-79
	r = FromStr("Hello みんなさん!")
	if got := r.Slice(3, 12).String(); got != "lo みん" {
		t.Fatalf("slice example: %q", got)
	}
	// ropey/src/rope.rs:258-263
	r = FromStr("Hello!")
	r.Insert(5, " world")
	if r.String() != "Hello world!" {
		t.Fatalf("insert example: %q", r.String())
	}
	// ropey/src/rope.rs:313-317 (`remove(5..)`)
	r = FromStr("Hello world!")
	r.Remove(5, r.Len())
	if r.String() != "Hello" {
		t.Fatalf("remove example: %q", r.String())
	}
	// ropey/src/rope.rs:365-370 (`slice(..5)`)
	if got := FromStr("Hello world!").Slice(0, 5).String(); got != "Hello" {
		t.Fatalf("slice(..5): %q", got)
	}
	// ropey/src/lib.rs:199-207: LF sees two lines; CR is ordinary text.
	r = FromStr("Line 1\nLine 2\rLine 3")
	if r.Line(0, LF).String() != "Line 1\n" || r.Line(1, LF).String() != "Line 2\rLine 3" || r.LenLines(LF) != 2 {
		t.Fatalf("LineType::LF example: %q %q", r.Line(0, LF), r.Line(1, LF))
	}
}

func TestEmptyRope(t *testing.T) {
	for _, r := range []Rope{New(), {}, FromStr("")} {
		if r.Len() != 0 || r.LenChars() != 0 || r.LenUTF16() != 0 || r.LenLines(LF) != 1 || r.String() != "" {
			t.Fatalf("empty rope metrics")
		}
		if r.LineToByteIdx(1, LF) != 0 || r.ByteToLineIdx(0, LF) != 0 || !r.IsCharBoundary(0) {
			t.Fatal("empty rope conversions")
		}
		if _, err := r.GetChar(0); !errors.Is(err, ErrOutOfBounds) {
			t.Fatal("GetChar on empty")
		}
		if c, _ := r.Chunk(0); c != "" {
			t.Fatal("chunk of empty")
		}
	}
	var r Rope
	r.Insert(0, "x")
	if r.String() != "x" {
		t.Fatal("insert into zero Rope")
	}
}

func TestErrors(t *testing.T) {
	r := FromStr("中文")
	if err := r.TryInsert(1, "x"); !errors.Is(err, ErrNonCharBoundary) {
		t.Fatalf("insert mid-char: %v", err)
	}
	if err := r.TryInsert(1, ""); !errors.Is(err, ErrNonCharBoundary) {
		t.Fatalf("empty insert mid-char: %v (ropey/src/rope.rs:535-543)", err)
	}
	if err := r.TryInsert(7, "x"); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("insert oob: %v", err)
	}
	if err := r.TryRemove(3, 0); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("inverted remove: %v", err)
	}
	if err := r.TryRemove(0, 7); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("remove oob: %v", err)
	}
	if err := r.TryRemove(0, 1); !errors.Is(err, ErrNonCharBoundary) {
		t.Fatalf("remove mid-char: %v", err)
	}
	if _, err := r.TrySlice(1, 3); !errors.Is(err, ErrNonCharBoundary) {
		t.Fatalf("slice mid-char: %v", err)
	}
	if _, err := r.GetChar(1); !errors.Is(err, ErrNonCharBoundary) {
		t.Fatalf("get_char mid-char: %v", err)
	}
	if _, ok := r.GetLine(1, LF); ok {
		t.Fatal("get_line oob")
	}
	if r.String() != "中文" {
		t.Fatalf("failed edits must leave the rope untouched: %q", r.String())
	}
	mustPanic(t, "Char mid-char", func() { r.Char(1) })
	mustPanic(t, "Line oob", func() { r.Line(5, LF) })
	mustPanic(t, "IsCharBoundary oob", func() { r.IsCharBoundary(7) })
	mustPanic(t, "CharsAt mid-char", func() { r.CharsAt(2) })
	mustPanic(t, "Insert oob", func() { r.Insert(9, "x") })
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s: expected panic", name)
		}
	}()
	f()
}

func TestInvalidUTF8IsReplaced(t *testing.T) {
	r := FromStr("a\xffb")
	if r.String() != "a�b" {
		t.Fatalf("got %q", r.String())
	}
	r.Insert(1, "\xc3")
	if r.String() != "a��b" {
		t.Fatalf("insert got %q", r.String())
	}
}

func TestMidCharConversions(t *testing.T) {
	// ropey/src/shared_impl.rs:294-302 and 358-369: a byte inside a char maps
	// to that char's index; a UTF-16 index inside a surrogate pair maps to the
	// char's start byte (shared_impl.rs:392-402).
	r := FromStr("a🎉b")
	for i, want := range []int{0, 1, 1, 1, 1, 2, 3} {
		if got := r.ByteToCharIdx(i); got != want {
			t.Fatalf("ByteToCharIdx(%d)=%d want %d", i, got, want)
		}
	}
	for i, want := range []int{0, 1, 1, 1, 1, 3, 4} {
		if got := r.ByteToUTF16Idx(i); got != want {
			t.Fatalf("ByteToUTF16Idx(%d)=%d want %d", i, got, want)
		}
	}
	for u, want := range []int{0, 1, 1, 5, 6} {
		if got := r.UTF16ToByteIdx(u); got != want {
			t.Fatalf("UTF16ToByteIdx(%d)=%d want %d", u, got, want)
		}
	}
	if r.FloorCharBoundary(3) != 1 || r.CeilCharBoundary(3) != 5 || r.FloorCharBoundary(6) != 6 {
		t.Fatal("floor/ceil char boundary")
	}
}

func TestSliceOfSliceAndLineToByteClamp(t *testing.T) {
	r := FromStr("ab\ncd\nef\ngh")
	s := r.Slice(4, 10) // "d\nef\ng"
	if s.String() != "d\nef\ng" || s.LenLines(LF) != 3 {
		t.Fatalf("slice %q lines %d", s.String(), s.LenLines(LF))
	}
	// ropey/src/shared_impl.rs:515-522: line starts are clamped to the slice.
	for l, want := range []int{0, 2, 5, 6} {
		if got := s.LineToByteIdx(l, LF); got != want {
			t.Fatalf("LineToByteIdx(%d)=%d want %d", l, got, want)
		}
	}
	if got := s.Slice(1, 5).String(); got != "\nef\n" {
		t.Fatalf("nested slice %q", got)
	}
	if got := s.Line(1, LF).String(); got != "ef\n" {
		t.Fatalf("slice line %q", got)
	}
	if str, ok := FromStr("short").AsStr(); !ok || str != "short" {
		t.Fatal("AsStr of single chunk")
	}
}

func TestEqualAcrossChunkings(t *testing.T) {
	a := FromStr("The quick brown fox jumps over the lazy dog, twice over.")
	b := New()
	for _, w := range []string{"The quick ", "brown fox ", "jumps over ", "the lazy dog, ", "twice over."} {
		b.Insert(b.Len(), w)
	}
	if !a.Equal(b) || !a.RopeSlice.Equal(b.Slice(0, b.Len())) {
		t.Fatal("ropes with different chunking must compare equal")
	}
	b.Insert(0, "x")
	if a.Equal(b) {
		t.Fatal("different ropes compared equal")
	}
	if !FromSlice(a.Slice(4, 9)).EqualString("quick") {
		t.Fatal("FromSlice")
	}
	if !SliceFromStr("  abc").EqualString("  abc") {
		t.Fatal("SliceFromStr")
	}
}
