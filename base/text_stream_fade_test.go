package base

import (
	"slices"
	"testing"
	"time"
)

// Ports of the U:text/stream_fade.rs tests (562-642): 6 of 6.

// U:text/stream_fade.rs:566-581
func TestCommonPrefixSpansChunkBoundaries(t *testing.T) {
	cases := []struct {
		a, b []string
		want int
	}{
		{[]string{"ab", "cd"}, []string{"abc", "d"}, 4},
		{[]string{"ab", "cd"}, []string{"abc", "x"}, 3},
		{[]string{"", "ab"}, []string{"a", "", "b", "c"}, 2},
		{[]string{"ab"}, nil, 0},
	}
	for _, c := range cases {
		if got := commonPrefixLen(c.a, c.b); got != c.want {
			t.Errorf("commonPrefixLen(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// U:text/stream_fade.rs:583-596
func TestFadeUnitsAreWordsWithTheirTrailingSpace(t *testing.T) {
	if got := fadeUnits([]string{"hello", " one two", "  three"}, 5, 20); !slices.Equal(got, [][2]int{{5, 10}, {10, 15}, {15, 20}}) {
		t.Errorf("units = %v", got)
	}
	// A unit that is only whitespace joins the word after it.
	if got := fadeUnits([]string{"a  b"}, 1, 4); !slices.Equal(got, [][2]int{{1, 4}}) {
		t.Errorf("units = %v", got)
	}
	if got := fadeUnits([]string{"abc"}, 3, 3); len(got) != 0 {
		t.Errorf("units = %v", got)
	}
}

// U:text/stream_fade.rs:598-606
func TestFadeUnitsSplitCJKByCharacter(t *testing.T) {
	if got := fadeUnits([]string{"你好，世界 ok"}, 0, 18); !slices.Equal(got, [][2]int{{0, 3}, {3, 6}, {6, 9}, {9, 12}, {12, 16}, {16, 18}}) {
		t.Errorf("units = %v", got)
	}
	// Latin before CJK starts a unit at the script change.
	if got := fadeUnits([]string{"ab中"}, 0, 5); !slices.Equal(got, [][2]int{{0, 2}, {2, 5}}) {
		t.Errorf("units = %v", got)
	}
}

// U:text/stream_fade.rs:608-617
func TestStaggerHoldsWhileTheUpdateLightsUpWithinOneFade(t *testing.T) {
	motion := DefaultTextViewMotion().WithStreamFade(600 * time.Millisecond).WithStreamFadeStagger(100 * time.Millisecond)
	for _, c := range []struct {
		words int
		want  time.Duration
	}{{1, 0}, {3, 100 * time.Millisecond}, {7, 100 * time.Millisecond}} {
		if got := motion.staggerStep(c.words); got != c.want {
			t.Errorf("staggerStep(%d) = %v, want %v", c.words, got, c.want)
		}
	}
}

// U:text/stream_fade.rs:619-630
func TestAnUpdateTooLargeToLightUpInOneFadeFadesAsOneChunk(t *testing.T) {
	motion := DefaultTextViewMotion().WithStreamFade(600 * time.Millisecond).WithStreamFadeStagger(100 * time.Millisecond)
	if motion.staggerStep(8) != 0 || motion.staggerStep(200) != 0 {
		t.Error("an update past one fade kept its stagger")
	}
	plain := DefaultTextViewMotion().WithStreamFade(600 * time.Millisecond)
	if plain.staggerStep(200) != 0 {
		t.Error("an unstaggered motion staggered")
	}
}

// U:text/stream_fade.rs:632-641
func TestPrefixNeverSplitsACharacter(t *testing.T) {
	// "中" and "串" share their first UTF-8 byte.
	next, old := "a中", "a串"
	prefix := commonPrefixLen([]string{next}, []string{old})
	if !(prefix > 1 && !isCharBoundary(next, prefix)) {
		t.Fatalf("prefix = %d", prefix)
	}
	if got := floorCharBoundary([]string{next}, prefix); got != 1 {
		t.Errorf("floor = %d", got)
	}
	if got := floorCharBoundary([]string{"a", "中"}, 4); got != 4 {
		t.Errorf("floor at the end = %d", got)
	}
}
