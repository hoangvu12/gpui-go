package gpui

import (
	"fmt"
	"testing"
	"time"
)

// fakeTypeAheadClock is the injectable clock of the TypeAhead tests
// (ticket 31: the helper reads time through a clock func, so tests drive
// it deterministically instead of sleeping). tick advances it.
type fakeTypeAheadClock struct {
	now time.Time
}

func (c *fakeTypeAheadClock) Now() time.Time       { return c.now }
func (c *fakeTypeAheadClock) tick(d time.Duration) { c.now = c.now.Add(d) }

// typeAheadItems is the test data source (the shape of Chromium's
// TestTypeAheadDataSource, type_ahead_test.cc:20-43): the caller owns
// the selection and moves it to every returned index, exactly like
// HTMLSelectElement does after TypeAhead::HandleEvent.
type typeAheadItems struct {
	options  []string
	selected int
}

func (s *typeAheadItems) IndexOfSelectedOption() int { return s.selected }
func (s *typeAheadItems) OptionCount() int           { return len(s.options) }
func (s *typeAheadItems) OptionAtIndex(index int) string {
	return s.options[index]
}

// newTypeAheadFixture builds a helper over options with a fake clock
// reading zero.
func newTypeAheadFixture(options ...string) (*TypeAhead, *typeAheadItems, *fakeTypeAheadClock) {
	src := &typeAheadItems{options: options, selected: -1}
	clock := &fakeTypeAheadClock{}
	return NewTypeAheadWithClock(src, clock.Now), src, clock
}

// typeKey presses one character through the usual select mode
// (kMatchPrefix | kCycleFirstChar) and applies the match to the
// selection, the caller's half of the contract.
func typeKey(t *TypeAhead, src *typeAheadItems, ch rune) int {
	index := t.Key(ch, TypeAheadMatchPrefix|TypeAheadCycleFirstChar)
	if index >= 0 {
		src.selected = index
	}
	return index
}

// TestTypeAheadPrefixMatches pins the prefix-matching rules of
// type_ahead.cc:105-115: the search starts at the item AFTER the
// selected one (search_start_offset 1, type_ahead.cc:89, :108), wraps
// around the list (:108-114), and a multi-character buffer starts at
// the selected item itself (offset 0, type_ahead.cc:100).
func TestTypeAheadPrefixMatches(t *testing.T) {
	fresh := func() (*TypeAhead, *typeAheadItems) {
		ta, src, _ := newTypeAheadFixture("aa", "ab", "ba", "bb")
		return ta, src
	}

	// Nothing selected: the search starts after item 0, so "a" lands on
	// "ab" (index 1), not "aa" (type_ahead.cc:108, selected<0 -> 0, +1).
	ta, src := fresh()
	if got := typeKey(ta, src, 'a'); got != 1 {
		t.Errorf("typed 'a' with nothing selected = %d, want 1 (\"ab\")", got)
	}
	ta, src = fresh()
	if got := typeKey(ta, src, 'b'); got != 2 {
		t.Errorf("typed 'b' with nothing selected = %d, want 2 (\"ba\")", got)
	}
	ta, src = fresh()
	if got := typeKey(ta, src, 'c'); got != -1 {
		t.Errorf("typed 'c' = %d, want -1 (no item starts with c)", got)
	}

	// A multi-character prefix anchors the search at the selected item
	// (type_ahead.cc:98-101: length>1 -> offset 0), so "ap" keeps
	// "Apricot" selected instead of wrapping to "Apple".
	ta, src, _ = newTypeAheadFixture("Apple", "Apricot")
	if got := typeKey(ta, src, 'a'); got != 1 {
		t.Fatalf("typed 'a' = %d, want 1 (\"Apricot\")", got)
	}
	if got := typeKey(ta, src, 'p'); got != 1 {
		t.Errorf("typed \"ap\" = %d, want 1 (multi-char prefixes search from the selected item)", got)
	}

	// A multi-character prefix that matches nothing reports no match.
	ta, src = fresh()
	typeKey(ta, src, 'a')
	if got := typeKey(ta, src, 'x'); got != -1 {
		t.Errorf("typed \"ax\" = %d, want -1", got)
	}

	// The search wraps past the end of the list (type_ahead.cc:114).
	ta, src = fresh()
	src.selected = 3
	if got := typeKey(ta, src, 'a'); got != 0 {
		t.Errorf("typed 'a' with the last item selected = %d, want 0 (wrapped to \"aa\")", got)
	}
}

// TestTypeAheadCaseAndAccentFold pins the case- and accent-insensitive
// prefix match (StartsWithIgnoringCaseAndAccents, string_impl.cc:1164,
// backed by ICU primary-strength search): typed ASCII matches accented
// items and typed accents match plain items.
func TestTypeAheadCaseAndAccentFold(t *testing.T) {
	ta, src, _ := newTypeAheadFixture("Éclair", "eels")
	if got := typeKey(ta, src, 'e'); got != 1 {
		t.Errorf("typed 'e' = %d, want 1 (accent-insensitive: \"eels\")", got)
	}
	// A second, different character grows the buffer to a two-character
	// prefix, which anchors at the selected item: "ec" matches
	// "Éclair" through the fold.
	if got := typeKey(ta, src, 'c'); got != 0 {
		t.Errorf("typed \"ec\" = %d, want 0 (\"Éclair\" folds to \"eclair\")", got)
	}

	ta, src, _ = newTypeAheadFixture("Éclair", "eels")
	if got := typeKey(ta, src, 'é'); got != 1 {
		t.Errorf("typed 'é' = %d, want 1 (typing an accent matches the plain prefix of \"eels\")", got)
	}
	if got := typeKey(ta, src, 'c'); got != 0 {
		t.Errorf("typed \"éc\" = %d, want 0 (both sides fold to \"eclair\")", got)
	}

	// Repeating the same letter cycles (the kCycleFirstChar rule), and
	// the cycling search is accent-insensitive too: pressing 'e' steps
	// from \"eels\" to \"Éclair\".
	ta, src, _ = newTypeAheadFixture("Éclair", "eels")
	typeKey(ta, src, 'e')
	if got := typeKey(ta, src, 'e'); got != 0 {
		t.Errorf("pressing 'e' twice = %d on the second press, want 0 (cycling steps to \"Éclair\")", got)
	}

	// The ß full case fold of wtf_string_test.cc:720-723: "stras" is a
	// prefix of "Straße" under primary strength.
	ta, src, _ = newTypeAheadFixture("Straße", "Strand")
	if got := typeKey(ta, src, 's'); got != 1 {
		t.Fatalf("typed 's' = %d, want 1 (\"Strand\")", got)
	}
	for _, ch := range "tra" {
		if got := typeKey(ta, src, ch); got != 1 {
			t.Fatalf("typed \"stra\" = %d at %q, want 1 (\"Strand\")", got, ch)
		}
	}
	if got := typeKey(ta, src, 's'); got != 0 {
		t.Errorf("typed \"stras\" = %d, want 0 (\"Straße\" folds to \"strasse\")", got)
	}

	// Case folding: an uppercase item and a lowercase key agree
	// (wtf_string_test.cc:716-718, "PuPpY"/"pup").
	ta, src, _ = newTypeAheadFixture("PuPpY", "zebra")
	if got := typeKey(ta, src, 'p'); got != 0 {
		t.Errorf("typed 'p' = %d, want 0 (\"PuPpY\")", got)
	}
}

// TestTypeAheadLeadingWhiteSpaceStripped pins StripLeadingWhiteSpace
// (type_ahead.cc:44-57): spaces and no-break spaces before an option's
// text do not block the prefix match.
func TestTypeAheadLeadingWhiteSpaceStripped(t *testing.T) {
	ta, src, _ := newTypeAheadFixture("  spaced", "\u00A0nbsp", "plain")
	if got := typeKey(ta, src, 's'); got != 0 {
		t.Errorf("typed 's' = %d, want 0 (leading spaces stripped)", got)
	}
	ta, src, _ = newTypeAheadFixture("  spaced", "\u00A0nbsp", "plain")
	if got := typeKey(ta, src, 'n'); got != 1 {
		t.Errorf("typed 'n' = %d, want 1 (leading no-break space stripped)", got)
	}
}

// TestTypeAheadTimeoutResetsBuffer pins kTypeAheadTimeout
// (type_ahead.cc:42, checked at :69-74): the buffer keeps growing while
// keys arrive within one second, is kept when the gap is exactly the
// timeout (the check is strictly greater), and is cleared once the gap
// passes it.
func TestTypeAheadTimeoutResetsBuffer(t *testing.T) {
	ta, src, clock := newTypeAheadFixture("aa", "ab", "ba", "bb")
	clock.now = clock.now.Add(time.Second)
	if got := typeKey(ta, src, 'a'); got != 1 {
		t.Fatalf("first 'a' = %d, want 1", got)
	}

	// Exactly the timeout later: still the same session, so the buffer
	// grows to "ab" and matches item 1 (a fresh buffer would have made
	// the single "b" land on "ba" at index 2).
	clock.tick(time.Second)
	if got := typeKey(ta, src, 'b'); got != 1 {
		t.Fatalf("typed \"ab\" at exactly the timeout = %d, want 1 (session kept)", got)
	}

	// One nanosecond past the timeout: the buffer resets, so the typed
	// "b" is a fresh single character and lands on "ba".
	clock.tick(time.Second)
	clock.now = clock.now.Add(time.Nanosecond)
	if got := typeKey(ta, src, 'b'); got != 2 {
		t.Fatalf("typed 'b' past the timeout = %d, want 2 (buffer reset)", got)
	}
}

// TestTypeAheadRepeatedLetterCycles pins the kCycleFirstChar rule
// (type_ahead.cc:91-95): typing the same letter again searches on that
// single character starting after the selected item, so pressing it
// repeatedly cycles through every item beginning with it.
func TestTypeAheadRepeatedLetterCycles(t *testing.T) {
	ta, src, _ := newTypeAheadFixture("aa", "ab", "ba", "bb")
	var got []int
	for range 3 {
		got = append(got, typeKey(ta, src, 'a'))
	}
	if fmt.Sprint(got) != "[1 0 1]" {
		t.Errorf("pressing 'a' three times selected %v, want [1 0 1] (cycling \"ab\" -> \"aa\" -> \"ab\")", got)
	}

	ta, src, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	got = nil
	for range 3 {
		got = append(got, typeKey(ta, src, 'b'))
	}
	if fmt.Sprint(got) != "[2 3 2]" {
		t.Errorf("pressing 'b' three times selected %v, want [2 3 2] (cycling \"ba\" -> \"bb\" -> \"ba\")", got)
	}

	// A different letter breaks the cycle: the buffer grows to "aab",
	// which is a prefix of nothing.
	ta, src, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	typeKey(ta, src, 'a')
	typeKey(ta, src, 'a')
	if got := typeKey(ta, src, 'b'); got != -1 {
		t.Errorf("typed \"aab\" = %d, want -1 (a different letter ends the cycling)", got)
	}
}

// TestTypeAheadActiveSession ports Chromium's HasActiveSession tests
// (type_ahead_test.cc:54-75, type_ahead.cc:128-133): no session before
// the first key, the session stays alive up to and including exactly
// one second, dies past it, and Reset ends it immediately.
func TestTypeAheadActiveSession(t *testing.T) {
	ta, src, clock := newTypeAheadFixture("aa", "ab")
	if ta.Active() {
		t.Fatal("a fresh helper has no session")
	}
	clock.now = clock.now.Add(500 * time.Millisecond)
	typeKey(ta, src, 'a')
	if !ta.Active() {
		t.Fatal("a key press starts a session")
	}
	clock.tick(time.Second)
	if !ta.Active() {
		t.Fatal("the session is still active exactly one second later (delta <= timeout)")
	}
	clock.now = clock.now.Add(time.Nanosecond)
	if ta.Active() {
		t.Fatal("the session is inactive past one second")
	}
}

// TestTypeAheadReset pins ResetSession (type_ahead.cc:135-138): the
// buffer and the session both clear, so the next key starts over.
func TestTypeAheadReset(t *testing.T) {
	ta, src, _ := newTypeAheadFixture("ab", "aa")
	if got := typeKey(ta, src, 'a'); got != 1 {
		t.Fatalf("first 'a' = %d, want 1 (\"aa\")", got)
	}
	ta.Reset()
	if ta.Active() {
		t.Fatal("Reset must end the session")
	}
	// With a cleared buffer the next 'a' is a single character again:
	// the search starts after the (still) selected item 1 and wraps to
	// "ab". Without the reset the buffer would be "aa" and keep "aa".
	if got := typeKey(ta, src, 'a'); got != 0 {
		t.Errorf("'a' after Reset = %d, want 0 (buffer cleared)", got)
	}
}

// TestTypeAheadOutOfOrderKeyRejected pins the early return of
// type_ahead.cc:70-71: a key stamped before the last one is ignored
// entirely — no buffer growth, no session refresh.
func TestTypeAheadOutOfOrderKeyRejected(t *testing.T) {
	ta, src, clock := newTypeAheadFixture("aa", "ab", "ba", "bb")
	clock.now = clock.now.Add(time.Second)
	if got := typeKey(ta, src, 'a'); got != 1 {
		t.Fatalf("first 'a' = %d, want 1", got)
	}
	clock.now = clock.now.Add(-100 * time.Millisecond)
	if got := typeKey(ta, src, 'b'); got != -1 {
		t.Fatalf("an out-of-order key returned %d, want -1", got)
	}
	// The session still runs from the first key: a later 'b' extends the
	// buffer to "ab" (a swallowed 'b' would have made it "abb").
	clock.now = clock.now.Add(600 * time.Millisecond)
	if got := typeKey(ta, src, 'b'); got != 1 {
		t.Errorf("typed \"ab\" = %d, want 1 (the rejected key never reached the buffer)", got)
	}
}

// TestTypeAheadMatchIndex pins the kMatchIndex fallback
// (type_ahead.cc:118-122 with StringToIntLoose,
// string_to_number.cc:353-355): when the prefix search finds nothing,
// a numeric buffer selects the option at that 1-based index.
func TestTypeAheadMatchIndex(t *testing.T) {
	ta, _, _ := newTypeAheadFixture("aa", "ab", "ba", "bb")
	if got := ta.Key('2', TypeAheadMatchIndex); got != 1 {
		t.Errorf("typed '2' = %d, want 1 (1-based index)", got)
	}
	// A fresh helper per key: the buffer accumulates, so typing '4' after
	// '2' would parse the buffer "24" instead of 4.
	ta, _, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	if got := ta.Key('4', TypeAheadMatchIndex); got != 3 {
		t.Errorf("typed '4' = %d, want 3", got)
	}
	// The accumulating buffer: "24" parses as 24, past the count.
	ta, _, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	ta.Key('2', TypeAheadMatchIndex)
	if got := ta.Key('4', TypeAheadMatchIndex); got != -1 {
		t.Errorf("typed \"24\" = %d, want -1 (the buffer accumulates)", got)
	}
	ta, _, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	if got := ta.Key('5', TypeAheadMatchIndex); got != -1 {
		t.Errorf("typed '5' = %d, want -1 (past the option count)", got)
	}
	ta, _, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	if got := ta.Key('0', TypeAheadMatchIndex); got != -1 {
		t.Errorf("typed '0' = %d, want -1 (index 0 is not a selection)", got)
	}
	ta, _, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	if got := ta.Key('x', TypeAheadMatchIndex); got != -1 {
		t.Errorf("typed 'x' = %d, want -1 (not a number)", got)
	}

	// Prefix and index together: "3" matches no prefix, so the index
	// fallback selects item 2.
	ta, _, _ = newTypeAheadFixture("aa", "ab", "ba", "bb")
	if got := ta.Key('3', TypeAheadMatchPrefix|TypeAheadMatchIndex); got != 2 {
		t.Errorf("typed '3' with both modes = %d, want 2 (index fallback)", got)
	}
}

// TestTypeAheadModeWithoutPrefix documents the mode flags: with neither
// kMatchPrefix nor a cycle in progress no prefix is ever built
// (type_ahead.cc:89-103), so nothing matches.
func TestTypeAheadModeWithoutPrefix(t *testing.T) {
	ta, _, _ := newTypeAheadFixture("aa", "ab")
	if got := ta.Key('a', 0); got != -1 {
		t.Errorf("mode 0 returned %d, want -1", got)
	}
	if got := ta.Key('a', TypeAheadCycleFirstChar); got != -1 {
		t.Errorf("cycle-only mode returned %d, want -1 (cycling needs a prefix match first)", got)
	}
}

// TestTypeAheadNoOptions pins the option-count guard
// (type_ahead.cc:85-88): an empty source never matches, but the key
// still opens a session (last_type_time_ is set before the guard,
// type_ahead.cc:81).
func TestTypeAheadNoOptions(t *testing.T) {
	ta, src, clock := newTypeAheadFixture()
	if got := typeKey(ta, src, 'a'); got != -1 {
		t.Errorf("typed 'a' with no options = %d, want -1", got)
	}
	if !ta.Active() {
		t.Error("the key press must still start a session")
	}
	clock.tick(2 * time.Second)
	if ta.Active() {
		t.Error("the session must still time out")
	}
}

// TestTypeAheadShouldHandleKey pins ShouldHandleKeyboardEvent
// (type_ahead.cc:59-63): printable characters without ctrl, alt or
// meta feed typeahead; shift (and any other modifier) does not block
// it.
func TestTypeAheadShouldHandleKey(t *testing.T) {
	cases := []struct {
		ch   rune
		mods Modifiers
		want bool
	}{
		{'a', Modifiers{}, true},
		{'A', Modifiers{Shift: true}, true},
		{'z', Modifiers{}, true},
		{'é', Modifiers{}, true},
		{'中', Modifiers{}, true},
		{' ', Modifiers{}, true},
		{'1', Modifiers{}, true},
		{'a', Modifiers{Control: true}, false},
		{'a', Modifiers{Alt: true}, false},
		{'a', Modifiers{Platform: true}, false},
		{'a', Modifiers{Shift: true, Control: true}, false},
		{'\n', Modifiers{}, false},
		{'\t', Modifiers{}, false},
		{'\r', Modifiers{}, false},
		{0, Modifiers{}, false},
	}
	for _, tc := range cases {
		if got := TypeAheadShouldHandleKey(tc.ch, tc.mods); got != tc.want {
			t.Errorf("TypeAheadShouldHandleKey(%q, %+v) = %v, want %v", tc.ch, tc.mods, got, tc.want)
		}
	}
}

// TestTypeAheadFold pins the folding used for matching (the Go port of
// ICU primary strength over the precomposed Latin ranges; the Chromium
// reference is string_impl.cc:1164-1179 and its tests at
// wtf_string_test.cc:711-723).
func TestTypeAheadFold(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"PuPpY", "puppy"},
		{"international", "international"},
		{"Éclair", "eclair"},
		{"naïve", "naive"},
		{"ÀÉÎÕÜ", "aeiou"},
		{"Straße", "strasse"},
		{"ẞ", "ss"},
		{"STRASS", "strass"},
		{"æon", "aeon"},
		{"Œuvre", "oeuvre"},
		{"smørrebrød", "smorrebrod"},
		{"Cộ Hồi", "co hoi"},
		{"e\u0301", "e"}, // a combining mark folds away
	}
	for _, tc := range cases {
		if got := typeAheadFold(tc.in); got != tc.want {
			t.Errorf("typeAheadFold(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// The folded forms of a key and a matching item agree.
	if typeAheadFold("Éclair") != typeAheadFold("eclair") {
		t.Error("accent folding is not symmetric")
	}
}
