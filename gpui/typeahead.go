package gpui

import (
	"strings"
	"time"
	"unicode"
)

// TypeAhead is the shared type-to-select helper for lists, trees and
// menus (browser-core ticket 31, spec story 78): it accumulates typed
// characters into a buffer that resets after a timeout and finds the
// option the keys select, matching prefixes case- and
// accent-insensitively and cycling when the same letter repeats.
//
// The port is Blink's TypeAhead
// (third_party/blink/renderer/core/html/forms/type_ahead.cc, and its
// data-source contract at type_ahead.h:47-49):
//
//   - the buffer resets when more than one second passes between keys
//     (kTypeAheadTimeout, type_ahead.cc:42, checked at :69-74 — a gap of
//     exactly one second keeps the session);
//   - a key stamped before the previous one is ignored outright
//     (type_ahead.cc:70-71);
//   - the search starts at the option after the selected one and wraps
//     (type_ahead.cc:107-114), except a multi-character buffer starts
//     at the selected option itself (type_ahead.cc:98-101);
//   - repeating the same character cycles through the options starting
//     with it (kCycleFirstChar, type_ahead.cc:91-95);
//   - matching is a prefix match ignoring case and accents
//     (StartsWithIgnoringCaseAndAccents, string_impl.cc:1164-1179, the
//     ICU primary-strength search; "PuPpY" starts with "pup" and "Straße"
//     with "STRASS" per wtf_string_test.cc:711-723);
//   - leading whitespace — including no-break space — is stripped from
//     an option before matching (type_ahead.cc:44-57);
//   - with TypeAheadMatchIndex, a buffer of digits selects the option
//     at that 1-based index when no prefix matched (type_ahead.cc:118-122).
//
// The folding port approximates ICU primary strength over the
// precomposed Latin ranges (typeahead_fold_generated.go): simple case
// folding, the Latin accent and contraction table, and ignorable
// combining marks. Full Unicode collation equivalence (every script,
// contractions outside the table) is out of scope and documented on
// typeAheadFold.
//
// The caller owns the selection: after Key returns an index, the
// caller moves its selection there and the next key reports it through
// the data source — exactly the HTMLSelectElement contract.
//
// Helper state is not synchronized: like the rest of the foreground
// object graph it is meant to be used from one thread.
type TypeAhead struct {
	source TypeAheadDataSource
	// buffer is the typed characters of the current session.
	buffer []rune
	// repeatingChar is the last single character typed (0 = none), the
	// cycling trigger of type_ahead.cc:91.
	repeatingChar rune
	// lastTypeAt is when the last key arrived; set only while a session
	// runs (the std::optional<TimeTicks> last_type_time_ of
	// type_ahead.h:71, modeled as a flag so a clock that reads the zero
	// time still distinguishes "no session" from a session at time
	// zero).
	lastTypeAt time.Time
	// hasSession reports whether lastTypeAt is set.
	hasSession bool
	// clock reads the time of each key; injectable for tests
	// (NewTypeAheadWithClock), time.Now otherwise.
	clock func() time.Time
}

// TypeAheadDataSource supplies the options a TypeAhead searches
// (blink's TypeAheadDataSource, type_ahead.h:43-50).
type TypeAheadDataSource interface {
	// IndexOfSelectedOption is the index of the currently selected
	// option, or -1 when nothing is selected. The search starts after
	// it (type_ahead.cc:107-108).
	IndexOfSelectedOption() int
	// OptionCount is the number of searchable options.
	OptionCount() int
	// OptionAtIndex returns the searchable text of the option at index,
	// with leading whitespace stripped before matching.
	OptionAtIndex(index int) string
}

// TypeAheadMode selects what Key matches (blink's ModeFlag,
// type_ahead.h:58-62). Modes combine with |.
type TypeAheadMode uint

const (
	// TypeAheadMatchPrefix matches the typed buffer as a prefix,
	// case- and accent-insensitively (kMatchPrefix).
	TypeAheadMatchPrefix TypeAheadMode = 1 << iota
	// TypeAheadCycleFirstChar repeats of the same character cycle
	// through the options starting with it (kCycleFirstChar).
	TypeAheadCycleFirstChar
	// TypeAheadMatchIndex selects the option at the buffer's 1-based
	// index when no prefix matched (kMatchIndex).
	TypeAheadMatchIndex
)

// typeAheadTimeout is kTypeAheadTimeout (type_ahead.cc:42): the buffer
// of an unfinished session resets once this much time passes between
// keys.
const typeAheadTimeout = time.Second

// NewTypeAhead returns a TypeAhead over source, reading the time of
// each key from time.Now.
func NewTypeAhead(source TypeAheadDataSource) *TypeAhead {
	return NewTypeAheadWithClock(source, time.Now)
}

// NewTypeAheadWithClock returns a TypeAhead over source reading the
// time of each key from clock. Tests inject a fake clock; production
// code uses NewTypeAhead.
func NewTypeAheadWithClock(source TypeAheadDataSource, clock func() time.Time) *TypeAhead {
	if source == nil {
		panic("gpui: NewTypeAhead requires a data source")
	}
	if clock == nil {
		panic("gpui: NewTypeAheadWithClock requires a clock")
	}
	return &TypeAhead{source: source, clock: clock}
}

// Key handles one typed character and returns the index of the option
// it selects, or -1 when nothing matches (TypeAhead::HandleEvent,
// type_ahead.cc:66-126). Use TypeAheadShouldHandleKey to decide which
// keys reach it.
func (t *TypeAhead) Key(ch rune, mode TypeAheadMode) int {
	now := t.clock()
	if t.hasSession {
		// An out-of-order timestamp is rejected before anything else
		// changes (type_ahead.cc:70-71).
		if now.Before(t.lastTypeAt) {
			return -1
		}
		if now.Sub(t.lastTypeAt) > typeAheadTimeout {
			t.buffer = t.buffer[:0]
		}
	}
	t.hasSession = true
	t.lastTypeAt = now
	t.buffer = append(t.buffer, ch)

	count := t.source.OptionCount()
	if count < 1 {
		return -1
	}

	// searchStart is where the wrap-around search begins; a
	// single-character prefix starts after the selection so repeats
	// cycle, a multi-character buffer starts at it (type_ahead.cc:89,
	// :98-101, :108).
	searchStart := 1
	var prefix []rune
	switch {
	case mode&TypeAheadCycleFirstChar != 0 && ch == t.repeatingChar:
		// The user is repeating one character: search on it alone
		// (type_ahead.cc:91-95).
		prefix = []rune{ch}
		t.repeatingChar = ch
	case mode&TypeAheadMatchPrefix != 0:
		prefix = t.buffer
		if len(t.buffer) > 1 {
			t.repeatingChar = 0
			searchStart = 0
		} else {
			t.repeatingChar = ch
		}
	}

	if len(prefix) > 0 {
		selected := t.source.IndexOfSelectedOption()
		if selected < 0 {
			selected = 0
		}
		index := (selected + searchStart) % count
		folded := typeAheadFold(string(prefix))
		for i := 0; i < count; i, index = i+1, (index+1)%count {
			text := typeAheadStripLeadingSpace(t.source.OptionAtIndex(index))
			if strings.HasPrefix(typeAheadFold(text), folded) {
				return index
			}
		}
	}

	if mode&TypeAheadMatchIndex != 0 {
		if index, ok := typeAheadLooseInt(string(t.buffer)); ok && index > 0 && index <= count {
			return index - 1
		}
	}
	return -1
}

// Active reports whether a type-ahead session is running: a key was
// typed and no more than one second has passed since
// (TypeAhead::HasActiveSession, type_ahead.cc:128-133 — the boundary
// itself, exactly one second, still counts).
func (t *TypeAhead) Active() bool {
	if !t.hasSession {
		return false
	}
	return t.clock().Sub(t.lastTypeAt) <= typeAheadTimeout
}

// Reset drops the buffer and ends the session
// (TypeAhead::ResetSession, type_ahead.cc:135-138).
func (t *TypeAhead) Reset() {
	t.hasSession = false
	t.lastTypeAt = time.Time{}
	t.buffer = t.buffer[:0]
	t.repeatingChar = 0
}

// TypeAheadShouldHandleKey reports whether a key press should feed a
// TypeAhead (TypeAhead::ShouldHandleKeyboardEvent, type_ahead.cc:59-63):
// a printable character with no ctrl, alt or meta modifier. Shift does
// not block it — shift+a is a typed "A" — and neither does any other
// modifier the reference does not check.
func TypeAheadShouldHandleKey(ch rune, mods Modifiers) bool {
	if mods.Control || mods.Alt || mods.Platform {
		return false
	}
	return unicode.IsPrint(ch)
}

// typeAheadStripLeadingSpace is StripLeadingWhiteSpace
// (type_ahead.cc:44-57): spaces and no-break spaces at the start of an
// option's text are ignored. Go's unicode.IsSpace covers the space and
// newline family the reference checks (unicode::IsSpaceOrNewline) plus
// the no-break space it lists separately.
func typeAheadStripLeadingSpace(s string) string {
	cut := 0
	for _, r := range s {
		if r == '\u00A0' || unicode.IsSpace(r) {
			cut += len(string(r))
			continue
		}
		break
	}
	return s[cut:]
}

// typeAheadLooseInt parses a leading run of digits, like
// StringToIntLoose (string_to_number.cc:353-355): "12" and "12x" are
// 12, anything that does not start with a digit is not a number.
func typeAheadLooseInt(s string) (int, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n := 0
	for _, digit := range []byte(s[:i]) {
		n = n*10 + int(digit-'0')
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, true
}

// typeAheadFold folds text for matching, the port of the
// case-and-accent-insensitive comparison behind
// StringImpl::StartsWithIgnoringCaseAndAccents (string_impl.cc:1164,
// backed by ICU's primary-strength search): characters are lowercased,
// precomposed Latin accents drop their marks, the contractions ß, æ, œ
// and ø expand, and combining marks are ignorable. Both the option
// text and the typed prefix pass through it.
//
// The approximation covers the precomposed Latin ranges
// (typeahead_fold_generated.go); outside them text only case-folds.
// Full Unicode collation equivalence (other scripts' accents, all
// contractions) would need the collation tables the reference gets
// from ICU.
func typeAheadFold(s string) string {
	folded := make([]rune, 0, len(s))
	for _, r := range s {
		r = unicode.ToLower(r)
		if base, ok := typeAheadFoldTable[r]; ok {
			folded = append(folded, []rune(base)...)
			continue
		}
		// Combining marks are ignorable at primary strength.
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		folded = append(folded, r)
	}
	return string(folded)
}
