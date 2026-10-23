package base

// Port of U:input/editor/auto_close.rs (the delimiters the editor
// inserted, retained across edits and history replay) and the automatic
// closing decisions of U:input/base/state.rs:1685-1795
// (auto_close_target, text_before_matches, text_after_matches,
// is_escaped_at, skip_over_target).
//
// The undo manager keeps Option<AutoClosedPairs> as an `any`
// (input_undo_manager.go): an autoClosedPairs value is Some, nil is None.

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// autoClosedPair is one (open range, close range) entry.
type autoClosedPair struct{ open, close InputSelection }

// autoClosedPairs are the delimiters inserted by the editor
// (U:input/editor/auto_close.rs:3-7).
type autoClosedPairs struct{ pairs []autoClosedPair }

// clone copies the pairs (`#[derive(Clone)]`).
func (p autoClosedPairs) clone() autoClosedPairs {
	return autoClosedPairs{pairs: append([]autoClosedPair(nil), p.pairs...)}
}

// record is U:input/editor/auto_close.rs:15-17.
func (p *autoClosedPairs) record(open, close InputSelection) {
	p.pairs = append(p.pairs, autoClosedPair{open, close})
}

// contains is U:input/editor/auto_close.rs:19-23.
func (p *autoClosedPairs) contains(open, close InputSelection) bool {
	for _, pair := range p.pairs {
		if pair.open == open && pair.close == close {
			return true
		}
	}
	return false
}

// containsCloser is U:input/editor/auto_close.rs:25-29.
func (p *autoClosedPairs) containsCloser(cursor, index, length int) bool {
	for _, pair := range p.pairs {
		if pair.close.Start+index == cursor && pair.close.Len() == length {
			return true
		}
	}
	return false
}

// adjust slides the pairs along with an edit and drops a pair the edit
// touches (U:input/editor/auto_close.rs:31-48).
func (p *autoClosedPairs) adjust(edit InputSelection, newLen int) {
	delta := newLen - edit.Len()
	shift := func(r *InputSelection) {
		r.Start = max(r.Start+delta, 0)
		r.End = max(r.End+delta, 0)
	}
	kept := p.pairs[:0]
	for _, pair := range p.pairs {
		switch {
		case edit.End <= pair.open.Start:
			shift(&pair.open)
			shift(&pair.close)
		case edit.Start >= pair.open.End && edit.End <= pair.close.Start:
			shift(&pair.close)
		case edit.Start < pair.close.End:
			continue
		}
		kept = append(kept, pair)
	}
	p.pairs = kept
}

// autoClosedPairsFromReplay is `replay.auto_closed_pairs.unwrap_or_default()`.
func autoClosedPairsFromReplay(v any) autoClosedPairs {
	if p, ok := v.(autoClosedPairs); ok {
		return p.clone()
	}
	return autoClosedPairs{}
}

// autoCloseTarget decides pairing against the pre-edit text, before an
// opening quote can change its own syntax context; ordinary characters
// never query syntax. It returns the opener's length and the closer
// (U:input/base/state.rs:1685-1733).
func (s *InputBaseState[M]) autoCloseTarget(rng InputSelection, text string) (int, string, bool) {
	if s.silentReplaceText || s.imeMarkedRange != nil || !s.selections.isSingle() ||
		!s.activeSelection().isEmpty() || !rng.IsEmpty() || rng.Start != s.Cursor() ||
		utf8.RuneCountInString(text) != 1 || !s.mode.isAutoClose() {
		return 0, "", false
	}
	rules, ok := s.mode.languageConfig()
	if !ok {
		return 0, "", false
	}
	if c, ok := s.ext().CharAt(rng.Start); ok && !unicode.IsSpace(c) && !strings.ContainsRune(rules.AutoCloseBefore, c) {
		return 0, "", false
	}
	for _, pair := range rules.closingPairs() {
		prefix, found := strings.CutSuffix(pair.open, text)
		if !found {
			continue
		}
		if !s.textBeforeMatches(rng.Start, prefix) {
			continue
		}
		start := rng.Start - len(prefix)
		if pair.open == pair.close {
			if s.isEscapedAt(start) {
				continue
			}
			it := s.text.CharsAt(start)
			if c, ok := it.Prev(); ok && (unicode.IsLetter(c) || unicode.IsNumber(c) || c == '_') {
				continue
			}
		}
		if containsSyntaxContext(pair.notIn, s.hooks.editingSyntaxContext(s, start)) {
			continue
		}
		return len(pair.open), pair.close, true
	}
	return 0, "", false
}

// textBeforeMatches is U:input/base/state.rs:1735-1739.
func (s *InputBaseState[M]) textBeforeMatches(offset int, text string) bool {
	return offset >= len(text) && s.text.IsCharBoundary(offset-len(text)) &&
		s.text.Slice(offset-len(text), offset).EqualString(text)
}

// textAfterMatches is U:input/base/state.rs:1741-1745.
func (s *InputBaseState[M]) textAfterMatches(offset int, text string) bool {
	return offset+len(text) <= s.text.Len() && s.text.IsCharBoundary(offset+len(text)) &&
		s.text.Slice(offset, offset+len(text)).EqualString(text)
}

// isEscapedAt reports an odd run of backslashes before offset
// (U:input/base/state.rs:1747-1755).
func (s *InputBaseState[M]) isEscapedAt(offset int) bool {
	it := s.text.CharsAt(offset)
	n := 0
	for c, ok := it.Prev(); ok && c == '\\'; c, ok = it.Prev() {
		n++
	}
	return n%2 == 1
}

// skipOverTarget is the cursor target when a typed closer should skip an
// existing, unescaped one that already follows the collapsed cursor
// (U:input/base/state.rs:1757-1795).
func (s *InputBaseState[M]) skipOverTarget(newText string) (int, bool) {
	if !s.mode.isAutoClose() || utf8.RuneCountInString(newText) != 1 {
		return 0, false
	}
	rules, ok := s.mode.languageConfig()
	if !ok {
		return 0, false
	}
	cursor := s.Cursor()
	for _, pair := range rules.closingPairs() {
		for index := range pair.close {
			if !strings.HasPrefix(pair.close[index:], newText) ||
				!s.textBeforeMatches(cursor, pair.close[:index]) ||
				!s.textAfterMatches(cursor, pair.close[index:]) {
				continue
			}
			context := s.hooks.editingSyntaxContext(s, cursor)
			generated := s.mode.autoClosed.containsCloser(cursor, index, len(pair.close))
			if !generated && containsSyntaxContext(pair.notIn, context) &&
				!(context == SyntaxContextString && pair.open == pair.close) {
				continue
			}
			if s.isEscapedAt(cursor - index) {
				continue
			}
			return cursor + len(newText), true
		}
	}
	return 0, false
}

// autoClosePairDeletion is the pair a lone backspace between an opener and
// its closer deletes at once (U:input/base/state.rs:1808-1839).
func (s *InputBaseState[M]) autoClosePairDeletion() (InputSelection, bool) {
	if !s.mode.isAutoClose() || !s.selections.isSingle() || !s.activeSelection().isEmpty() {
		return InputSelection{}, false
	}
	rules, ok := s.mode.languageConfig()
	if !ok {
		return InputSelection{}, false
	}
	off := s.Cursor()
	for _, pair := range rules.closingPairs() {
		if !s.textBeforeMatches(off, pair.open) || !s.textAfterMatches(off, pair.close) {
			continue
		}
		start := off - len(pair.open)
		generated := s.mode.autoClosed.contains(NewInputSelection(start, off), NewInputSelection(off, off+len(pair.close)))
		if s.isEscapedAt(start) || (!generated && containsSyntaxContext(pair.notIn, s.hooks.editingSyntaxContext(s, start))) {
			continue
		}
		return NewInputSelection(start, off+len(pair.close)), true
	}
	return InputSelection{}, false
}
