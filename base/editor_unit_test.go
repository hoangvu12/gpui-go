package base

// Ported unit tests of U:input/editor/decorations.rs (6/6),
// U:input/editor/search.rs (5/5) and U:input/editor/indent.rs (2/2).

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

func rangesOf[T trackedDecoration[T]](ds []T) []InputSelection {
	out := make([]InputSelection, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.trackedRange())
	}
	return out
}

func wantRanges(t *testing.T, got, want []InputSelection) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("ranges %v, want %v", got, want)
	}
}

// U:input/editor/decorations.rs:561-590
func TestEditorDecorationsGeometricCollectionsShareUtf8NormalizationAndEditAffinity(t *testing.T) {
	var c decorationCollections[RangeDecoration]
	text := rope.FromStr("héllo world")
	first := c.create(normalizeDecorations(&text, []RangeDecoration{
		NewRangeDecoration(sel(2, 4)), NewRangeDecoration(InputSelection{Start: 2, End: 1}), NewRangeDecoration(sel(100, 200)),
	}))
	second := c.create(normalizeDecorations(&text, []RangeDecoration{NewRangeDecoration(sel(7, 12))}))
	get := func(id decorationCollectionID) []InputSelection { ds, _ := c.get(id); return rangesOf(ds) }
	wantRanges(t, get(first), []InputSelection{sel(1, 4)})
	c.adjustForEdit(sel(1, 1), 2)
	wantRanges(t, get(first), []InputSelection{sel(3, 6)})
	c.adjustForEdit(sel(6, 6), 1)
	wantRanges(t, get(first), []InputSelection{sel(3, 6)})
	c.adjustForEdit(sel(4, 4), 2)
	wantRanges(t, get(first), []InputSelection{sel(3, 8)})
	c.adjustForEdit(sel(3, 8), 0)
	if len(get(first)) != 0 || len(get(second)) == 0 {
		t.Fatalf("first %v second %v", get(first), get(second))
	}
	c.remove(first)
	third := c.create(nil)
	if third == first {
		t.Fatal("a new collection must not reuse a removed id")
	}
	if c.set(first, []RangeDecoration{NewRangeDecoration(sel(0, 1))}) {
		t.Fatal("setting a removed collection must fail")
	}
	if _, ok := c.get(second); !ok {
		t.Fatal("the second collection survives")
	}
}

// U:input/editor/decorations.rs:592-614
func TestEditorDecorationsVisibleQueryPreservesLayersAndSkipsFoldedSpans(t *testing.T) {
	var c decorationCollections[RangeDecoration]
	first := c.create([]RangeDecoration{
		NewRangeDecoration(sel(90, 100)), NewRangeDecoration(sel(0, 100)),
		NewRangeDecoration(sel(40, 50)), // hidden in a fold
		NewRangeDecoration(sel(0, 5)),
	})
	c.create([]RangeDecoration{NewRangeDecoration(sel(2, 4))})
	ranges := func() []InputSelection { return rangesOf(c.intersecting([]InputSelection{sel(0, 5), sel(90, 100)})) }
	wantRanges(t, ranges(), []InputSelection{sel(90, 100), sel(0, 100), sel(0, 5), sel(2, 4)})
	c.adjustForEdit(sel(0, 0), 1)
	wantRanges(t, ranges(), []InputSelection{sel(91, 101), sel(1, 101), sel(1, 6), sel(3, 5)})
	c.set(first, []RangeDecoration{NewRangeDecoration(sel(50, 60))})
	wantRanges(t, ranges(), []InputSelection{sel(3, 5)})
}

// U:input/editor/decorations.rs:616-642
func TestEditorDecorationsIntervalIndexCullsLargeCollectionsEvenWithASpanningRange(t *testing.T) {
	ds := make([]RangeDecoration, 0, 100_001)
	for ix := range 100_000 {
		ds = append(ds, NewRangeDecoration(sel(ix*10, ix*10+5)))
	}
	ds = append(ds, NewRangeDecoration(sel(0, 1_000_000)))
	index := newDecorationIndex(ds)
	for _, query := range []InputSelection{sel(0, 1), sel(500_000, 500_020), sel(999_990, 1_000_001), sel(1_000_000, 1_000_010)} {
		var matches []int
		visited := decorationIndexQuery(&index, ds, 0, len(ds), query, &matches)
		slices.Sort(matches)
		var expected []int
		for ix, d := range ds {
			if d.rng.Start < query.End && d.rng.End > query.Start {
				expected = append(expected, ix)
			}
		}
		if !slices.Equal(matches, expected) {
			t.Fatalf("query %v: %v, want %v", query, matches, expected)
		}
		if visited >= 100 {
			t.Fatalf("visited %d nodes for %v", visited, query)
		}
	}
}

// U:input/editor/decorations.rs:644-674
func TestEditorDecorationsIntervalIndexMatchesLinearReferenceForOverlapsAndMutations(t *testing.T) {
	var c decorationCollections[RangeDecoration]
	ds := make([]RangeDecoration, 0, 512)
	for ix := range 512 {
		start := (ix * 37) % 997
		ds = append(ds, NewRangeDecoration(sel(start, start+ix%61+1)))
	}
	id := c.create(ds)
	for _, edit := range []InputSelection{sel(0, 0), sel(300, 450), sel(900, 1100)} {
		c.adjustForEdit(edit, 3)
		for start := 0; start < 1100; start += 13 {
			query := sel(start, start+17)
			current, _ := c.get(id)
			var expected []InputSelection
			for _, d := range current {
				if d.rng.Start < query.End && d.rng.End > query.Start {
					expected = append(expected, d.rng)
				}
			}
			wantRanges(t, rangesOf(c.intersecting([]InputSelection{query})), expected)
		}
	}
}

// U:input/editor/decorations.rs:676-725
func TestEditorDecorationsCollectionsAreIndependentAndRangesAreClipped(t *testing.T) {
	text := rope.FromStr("héllo")
	bold := float32(700)
	red := gpui.Red()
	firstStyle := gpui.HighlightStyle{FontWeight: &bold}
	secondStyle := gpui.HighlightStyle{BackgroundColor: &red}
	var c decorationCollections[TextDecoration]
	first := c.create(normalizeDecorations(&text, []TextDecoration{NewTextDecoration(sel(2, 4), firstStyle)}))
	second := c.create(normalizeDecorations(&text, []TextDecoration{NewTextDecoration(sel(5, 100), secondStyle)}))
	if first == second {
		t.Fatal("collections have distinct ids")
	}
	check := func(id decorationCollectionID, want []TextDecoration) {
		t.Helper()
		got, ok := c.get(id)
		if !ok || len(got) != len(want) {
			t.Fatalf("collection %d = %v, want %v", id, got, want)
		}
		for i := range got {
			if got[i].Range != want[i].Range || got[i].Style.FontWeight != want[i].Style.FontWeight || got[i].Style.BackgroundColor != want[i].Style.BackgroundColor {
				t.Fatalf("collection %d = %v, want %v", id, got, want)
			}
		}
	}
	check(first, []TextDecoration{NewTextDecoration(sel(1, 4), firstStyle)})
	check(second, []TextDecoration{NewTextDecoration(sel(5, 6), secondStyle)})
	if !c.append(first, []TextDecoration{NewTextDecoration(sel(4, 5), secondStyle)}) {
		t.Fatal("append to a live collection")
	}
	check(first, []TextDecoration{NewTextDecoration(sel(1, 4), firstStyle), NewTextDecoration(sel(4, 5), secondStyle)})
	if !c.set(first, nil) {
		t.Fatal("set on a live collection")
	}
	check(first, nil)
	check(second, []TextDecoration{NewTextDecoration(sel(5, 6), secondStyle)})
}

// U:input/editor/decorations.rs:727-754
func TestEditorDecorationsDecorationRangesFollowTextEdits(t *testing.T) {
	var c decorationCollections[TextDecoration]
	id := c.create([]TextDecoration{NewTextDecoration(sel(2, 6), gpui.HighlightStyle{})})
	get := func() []InputSelection { ds, _ := c.get(id); return rangesOf(ds) }
	c.adjustForEdit(sel(0, 0), 2)
	wantRanges(t, get(), []InputSelection{sel(4, 8)})
	c.adjustForEdit(sel(6, 6), 2)
	wantRanges(t, get(), []InputSelection{sel(4, 10)})
	c.adjustForEdit(sel(4, 10), 3)
	wantRanges(t, get(), []InputSelection{sel(4, 7)})
	for _, step := range []struct{ rng, edit, want InputSelection }{
		{sel(2, 6), sel(2, 2), sel(4, 8)},
		{sel(2, 6), sel(6, 6), sel(2, 6)},
		{sel(2, 6), sel(2, 6), sel(2, 5)},
	} {
		newLen := 2
		if step.edit.Len() > 0 {
			newLen = 3
		}
		if got := adjustRangeForEdit(step.rng, step.edit, newLen); got != step.want {
			t.Fatalf("adjust %v by %v = %v, want %v", step.rng, step.edit, got, step.want)
		}
	}
}

// U:input/editor/search.rs:459-472
func TestEditorSearchFindsNavigatesAndPreservesReplacementPosition(t *testing.T) {
	m := NewSearchMatcher()
	m.Update(rope.FromStr("foo FOO foo"))
	m.UpdateQuery("foo", true)
	wantRanges(t, m.MatchedRanges(), []InputSelection{sel(0, 3), sel(4, 7), sel(8, 11)})
	if r, _ := m.Next(); r != sel(4, 7) {
		t.Fatalf("next = %v", r)
	}
	if r, _ := m.NextBack(); r != sel(0, 3) {
		t.Fatalf("next back = %v", r)
	}
	m.setCurrentMatchIndex(2)
	m.beginReplacement()
	m.Update(rope.FromStr("foo FOO bar"))
	if m.CurrentMatchIndex() != 1 {
		t.Fatalf("current = %d, want 1", m.CurrentMatchIndex())
	}
}

// U:input/editor/search.rs:474-481
func TestEditorSearchNextWrapsToStart(t *testing.T) {
	m := NewSearchMatcher()
	m.Update(rope.FromStr(".....aaaaa.....aaaaa.....aaaaa"))
	m.UpdateQuery("aaaaa", false)
	m.setCurrentMatchIndex(2)
	if r, _ := m.Next(); r != sel(5, 10) {
		t.Fatalf("next = %v, want 5..10", r)
	}
}

// U:input/editor/search.rs:483-500
func TestEditorSearchAQuerySetWithoutThePanelKeepsTheSessionActiveUntilClosed(t *testing.T) {
	session := DefaultSearchSession()
	if session.IsActive() {
		t.Fatal("a new session is inactive")
	}
	session.open(false, true)
	if !session.IsActive() {
		t.Fatal("open activates")
	}
	session.close()
	if session.IsActive() {
		t.Fatal("close deactivates")
	}
	session.activate()
	if !session.IsActive() || session.Open {
		t.Fatal("activate highlights without the panel")
	}
	session.close()
	if session.IsActive() {
		t.Fatal("close deactivates")
	}
}

// U:input/editor/search.rs:502-517
func TestEditorSearchIdenticalQueryKeepsTheCurrentMatch(t *testing.T) {
	session := DefaultSearchSession()
	session.updateQuery("foo", true)
	session.Matcher.Update(rope.FromStr("foo bar foo baz foo"))
	session.Matcher.UpdateCursorByOffset(12)
	if session.Matcher.CurrentMatchIndex() != 2 {
		t.Fatalf("current = %d", session.Matcher.CurrentMatchIndex())
	}
	session.updateQuery("foo", true)
	if session.Matcher.CurrentMatchIndex() != 2 || session.Matcher.Label() != "3/3" {
		t.Fatalf("current %d label %q", session.Matcher.CurrentMatchIndex(), session.Matcher.Label())
	}
}

// U:input/editor/search.rs:519-541
func TestEditorSearchReplacementKeepsCurrentMatchIndexOnNextMatch(t *testing.T) {
	m := NewSearchMatcher()
	m.Update(rope.FromStr("foo foo foo"))
	m.UpdateQuery("foo", true)
	if m.Label() != "1/3" || !m.hasNextWithoutWrap() {
		t.Fatalf("label %q", m.Label())
	}
	m.beginReplacement()
	m.Update(rope.FromStr("bar foo foo"))
	if m.CurrentMatchIndex() != 0 || m.MatchedRanges()[0] != sel(4, 7) || m.Label() != "1/2" {
		t.Fatalf("current %d first %v label %q", m.CurrentMatchIndex(), m.MatchedRanges()[0], m.Label())
	}
	m.setCurrentMatchIndex(1)
	if m.hasNextWithoutWrap() {
		t.Fatal("the last match has no next without wrapping")
	}
	m.setCurrentMatchIndex(0)
	m.beginReplacement()
	m.Update(rope.FromStr("bar foo bar"))
	if m.CurrentMatchIndex() != 0 || m.MatchedRanges()[0] != sel(4, 7) || m.Label() != "1/1" {
		t.Fatalf("current %d first %v label %q", m.CurrentMatchIndex(), m.MatchedRanges()[0], m.Label())
	}
}

// U:input/editor/search.rs:543-556
func TestEditorSearchUpdateMatchesClampsCurrentMatchIndexWhileReplacing(t *testing.T) {
	m := NewSearchMatcher()
	m.Update(rope.FromStr("foo foo foo"))
	m.UpdateQuery("foo", true)
	m.setCurrentMatchIndex(2)
	m.beginReplacement()
	m.Update(rope.FromStr("foo xoo foo"))
	if m.Len() != 2 || m.CurrentMatchIndex() != 1 || m.Label() != "2/2" {
		t.Fatalf("len %d current %d label %q", m.Len(), m.CurrentMatchIndex(), m.Label())
	}
}

// U:input/editor/indent.rs:451-474
func TestEditorIndentTabSize(t *testing.T) {
	for _, step := range []struct {
		tab  TabSize
		want string
	}{
		{TabSize{TabSize: 2}, "  "},
		{TabSize{TabSize: 4}, "    "},
		{TabSize{TabSize: 2, HardTabs: true}, "\t"},
		{TabSize{TabSize: 4, HardTabs: true}, "\t"},
	} {
		if got := step.tab.String(); got != step.want {
			t.Fatalf("%+v = %q, want %q", step.tab, got, step.want)
		}
	}
}

// U:input/editor/indent.rs:476-489
func TestEditorIndentTabSizeIndentCount(t *testing.T) {
	tab := TabSize{TabSize: 4}
	for _, step := range []struct {
		line string
		want int
	}{{"abc", 0}, {"  abc", 2}, {"    abc", 4}, {"\tabc", 4}, {"  \tabc", 6}, {" \t abc  ", 6}, {"abc", 0}} {
		line := rope.FromStr(step.line)
		if got := tab.IndentCount(line.RopeSlice); got != step.want {
			t.Fatalf("%q = %d, want %d", step.line, got, step.want)
		}
	}
}
