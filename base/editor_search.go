package base

// Port of U:input/editor/search.rs: the presentation-independent search
// engine (SearchMatcher), the search session and the state's search API.
//
// Go adaptations:
//
//   - The matcher compiles its query with aho-corasick upstream
//     (ascii_case_insensitive, one pattern, non-overlapping leftmost
//     matches). One pattern needs no automaton: matching ASCII-folded
//     bytes with strings.Index gives the same non-overlapping matches
//     (ASCII folding keeps byte lengths), so the pub `query:
//     Option<AhoCorasick>` field is the unexported compiled pattern.
//   - Iterator::next / DoubleEndedIterator::next_back are Next / NextBack
//     returning (range, ok).
//   - anchor_offset (Option<usize>) is *int.

import (
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// searchQuery is the compiled single-pattern query.
type searchQuery struct {
	pattern         string
	caseInsensitive bool
}

// SearchMatcher is the stateful search engine used by text inputs
// (U:input/editor/search.rs:11-19).
type SearchMatcher struct {
	text           rope.Rope
	query          *searchQuery
	matchedRanges  []InputSelection
	currentMatchIx int
	replacing      bool
}

// NewSearchMatcher is SearchMatcher::new (U:input/editor/search.rs:314-323).
func NewSearchMatcher() SearchMatcher { return SearchMatcher{text: rope.FromStr("")} }

// SearchSession is one search over an input: the query, how the built-in
// panel shows it, and its matches; written only through the state's
// search methods (U:input/editor/search.rs:21-39).
type SearchSession struct {
	// Open reports that the built-in search panel is showing.
	Open            bool
	ReplaceMode     bool
	CaseInsensitive bool
	Query           string
	Replacement     string
	AnchorOffset    *int
	Matcher         SearchMatcher
	// active: the panel is open, or a query was set without it and not
	// closed since.
	active bool
}

// DefaultSearchSession is `impl Default for SearchSession`
// (U:input/editor/search.rs:41-54).
func DefaultSearchSession() SearchSession {
	return SearchSession{CaseInsensitive: true, Matcher: NewSearchMatcher()}
}

// open is U:input/editor/search.rs:57-61.
func (s *SearchSession) open(replaceMode, replaceable bool) {
	s.Open = true
	s.active = true
	s.ReplaceMode = replaceMode && replaceable
}

// activate starts a search without the built-in panel
// (U:input/editor/search.rs:63-68).
func (s *SearchSession) activate() { s.active = true }

// close is U:input/editor/search.rs:70-73.
func (s *SearchSession) close() {
	s.Open = false
	s.active = false
}

// IsActive reports a search in progress: matches are highlighted while it
// holds (U:input/editor/search.rs:75-80).
func (s *SearchSession) IsActive() bool { return s.active }

// updateQuery is U:input/editor/search.rs:82-91: an identical query keeps
// the current match.
func (s *SearchSession) updateQuery(query string, caseInsensitive bool) {
	if s.Query == query && s.CaseInsensitive == caseInsensitive {
		return
	}
	s.Query = query
	s.CaseInsensitive = caseInsensitive
	s.Matcher.UpdateQuery(s.Query, caseInsensitive)
}

// OpenSearch opens the search session or re-invokes it; every call
// advances SearchActivationRevision, so call it from a user gesture, never
// from a render pass (U:input/editor/search.rs:95-134).
func (s *InputBaseState[M]) OpenSearch(replaceMode bool, cx *gpui.Context[InputBaseState[M]]) {
	if !s.searchable {
		return
	}
	s.searchActivationRevision++
	s.searchSession.open(replaceMode, s.IsReplaceable())
	query := s.SelectedText().String()
	if query == "" {
		query = s.searchSession.Query
	}
	queryChanged := query != s.searchSession.Query
	// A retained query resumes its previous occurrence; only a new query
	// is anchored to the current viewport.
	s.searchSession.AnchorOffset = nil
	if queryChanged && s.lastLayout != nil {
		anchor := s.lastLayout.visibleRangeOffset.Start
		s.searchSession.AnchorOffset = &anchor
	}
	s.searchSession.updateQuery(query, s.searchSession.CaseInsensitive)
	s.searchSession.Matcher.Update(s.text)
	if queryChanged && s.searchSession.AnchorOffset != nil {
		s.searchSession.Matcher.UpdateCursorByOffset(*s.searchSession.AnchorOffset)
	}
	cx.Notify()
}

// SearchSession is U:input/editor/search.rs:136-138.
func (s *InputBaseState[M]) SearchSession() *SearchSession { return &s.searchSession }

// SearchActivationRevision advances on every OpenSearch, including while
// the session is open (U:input/editor/search.rs:140-148).
func (s *InputBaseState[M]) SearchActivationRevision() uint64 { return s.searchActivationRevision }

// SetSearchReplaceMode is U:input/editor/search.rs:150-154 (doc(hidden)).
func (s *InputBaseState[M]) SetSearchReplaceMode(replaceMode bool, cx *gpui.Context[InputBaseState[M]]) {
	s.searchSession.ReplaceMode = replaceMode && s.IsReplaceable()
	cx.Notify()
}

// IsReplaceable reports whether the search panel can replace matches:
// replaceable, and neither disabled nor read-only
// (U:input/editor/search.rs:156-162).
func (s *InputBaseState[M]) IsReplaceable() bool { return s.replaceable && s.IsEditable() }

// SetSearchQuery sets the query and highlights its matches without the
// built-in panel, the entry point of a custom search UI
// (U:input/editor/search.rs:164-182).
func (s *InputBaseState[M]) SetSearchQuery(query string, caseInsensitive bool, cx *gpui.Context[InputBaseState[M]]) {
	s.searchSession.activate()
	s.searchSession.updateQuery(query, caseInsensitive)
	s.searchSession.Matcher.Update(s.text)
	cx.Notify()
}

// CloseSearch hides the panel and the highlights, keeping the query
// (U:input/editor/search.rs:184-189).
func (s *InputBaseState[M]) CloseSearch(cx *gpui.Context[InputBaseState[M]]) {
	s.searchSession.close()
	cx.Notify()
}

// NextSearchMatch moves to the next match and reveals it with padding
// (U:input/editor/search.rs:191-198).
func (s *InputBaseState[M]) NextSearchMatch(cx *gpui.Context[InputBaseState[M]]) (InputSelection, bool) {
	s.syncSearchMatcher()
	rng, ok := s.searchSession.Matcher.Next()
	if !ok {
		return InputSelection{}, false
	}
	s.scrollToWithPadding(rng.End, nil, scrollPaddingSurroundingLines, cx)
	return rng, true
}

// PreviousSearchMatch is U:input/editor/search.rs:200-207.
func (s *InputBaseState[M]) PreviousSearchMatch(cx *gpui.Context[InputBaseState[M]]) (InputSelection, bool) {
	s.syncSearchMatcher()
	rng, ok := s.searchSession.Matcher.NextBack()
	if !ok {
		return InputSelection{}, false
	}
	s.scrollToWithPadding(rng.Start, nil, scrollPaddingSurroundingLines, cx)
	return rng, true
}

// ReplaceCurrentSearchMatch replaces the current match and moves on;
// reports whether there was one (U:input/editor/search.rs:209-241).
func (s *InputBaseState[M]) ReplaceCurrentSearchMatch(replacement string, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) bool {
	if !s.IsReplaceable() {
		return false
	}
	s.syncSearchMatcher()
	matcher := &s.searchSession.Matcher
	ix := matcher.CurrentMatchIndex()
	if ix >= len(matcher.matchedRanges) {
		return false
	}
	rng := matcher.matchedRanges[ix]
	next, ok := matcher.peek()
	if !ok {
		next = rng
	}
	var direction *moveDirection
	if matcher.hasNextWithoutWrap() {
		down := moveDirectionDown
		direction = &down
	} else {
		matcher.setCurrentMatchIndex(0)
	}
	matcher.beginReplacement()
	rngUTF16 := s.rangeToUTF16(rng)
	s.scrollTo(next.End, direction, cx)
	s.replaceTextInRangeSilent(&rngUTF16, replacement, w, cx)
	return true
}

// ReplaceAllSearchMatches replaces every match and returns the count
// (U:input/editor/search.rs:243-267).
func (s *InputBaseState[M]) ReplaceAllSearchMatches(replacement string, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) int {
	if !s.IsReplaceable() {
		return 0
	}
	s.syncSearchMatcher()
	ranges := s.searchSession.Matcher.matchedRanges
	if len(ranges) == 0 {
		return 0
	}
	text := s.text
	ext := RopeExtOf(&text)
	for i := len(ranges) - 1; i >= 0; i-- {
		ext.Replace(ranges[i].Start, ranges[i].End, replacement)
	}
	s.searchSession.Matcher.beginReplacement()
	count := len(ranges)
	all := gpui.UTF16Range{Start: 0, End: s.text.LenUTF16()}
	s.replaceTextInRangeSilent(&all, text.String(), w, cx)
	down := moveDirectionDown
	s.scrollTo(0, &down, cx)
	return count
}

// updateSearch keeps the matches in step with an edit; a closed search
// skips the scan, and every reader syncs first
// (U:input/editor/search.rs:269-277).
func (s *InputBaseState[M]) updateSearch(*gpui.App) {
	if !s.searchSession.IsActive() {
		return
	}
	s.syncSearchMatcher()
}

// syncSearchMatcher recomputes the matches if the text changed
// (U:input/editor/search.rs:279-282).
func (s *InputBaseState[M]) syncSearchMatcher() { s.searchSession.Matcher.Update(s.text) }

// onActionSearch leaves the shortcut to the ancestors of an input that is
// not searchable, so a custom search UI can take it
// (U:input/editor/search.rs:284-292).
func (s *InputBaseState[M]) onActionSearch(_ *Search, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if !s.searchable {
		cx.Propagate()
		return
	}
	s.OpenSearch(false, cx)
}

// onActionReplace is U:input/editor/search.rs:294-305.
func (s *InputBaseState[M]) onActionReplace(_ *Replace, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if !s.searchable {
		cx.Propagate()
		return
	}
	s.OpenSearch(true, cx)
}

// Update sets the source text and recomputes the matches
// (U:input/editor/search.rs:325-333).
func (m *SearchMatcher) Update(text rope.Rope) {
	if m.text.Equal(text) {
		m.replacing = false
		return
	}
	m.text = text
	m.updateMatches()
}

// UpdateQuery compiles the query; empty is none
// (U:input/editor/search.rs:335-343).
func (m *SearchMatcher) UpdateQuery(query string, caseInsensitive bool) {
	m.query = nil
	if query != "" {
		m.query = &searchQuery{pattern: query, caseInsensitive: caseInsensitive}
	}
	m.updateMatches()
}

// MatchedRanges is U:input/editor/search.rs:345-347.
func (m *SearchMatcher) MatchedRanges() []InputSelection { return m.matchedRanges }

// CurrentMatchIndex is U:input/editor/search.rs:349-351.
func (m *SearchMatcher) CurrentMatchIndex() int { return m.currentMatchIx }

// Current is the current match's index, false without matches
// (U:input/editor/search.rs:353-357).
func (m *SearchMatcher) Current() (int, bool) {
	if m.IsEmpty() {
		return 0, false
	}
	return m.currentMatchIx, true
}

// Len is U:input/editor/search.rs:359-361.
func (m *SearchMatcher) Len() int { return len(m.matchedRanges) }

// IsEmpty is U:input/editor/search.rs:363-365.
func (m *SearchMatcher) IsEmpty() bool { return len(m.matchedRanges) == 0 }

// Label is `2/5`, or `0/0` without matches (U:input/editor/search.rs:367-373).
func (m *SearchMatcher) Label() string {
	ix, ok := m.Current()
	if !ok {
		return "0/0"
	}
	return strconv.Itoa(ix+1) + "/" + strconv.Itoa(m.Len())
}

// peek is U:input/editor/search.rs:375-378.
func (m *SearchMatcher) peek() (InputSelection, bool) {
	ix, ok := m.nextIndex()
	if !ok || ix >= len(m.matchedRanges) {
		return InputSelection{}, false
	}
	return m.matchedRanges[ix], true
}

// hasNextWithoutWrap is U:input/editor/search.rs:380-382.
func (m *SearchMatcher) hasNextWithoutWrap() bool {
	return m.currentMatchIx < max(len(m.matchedRanges)-1, 0)
}

// UpdateCursorByOffset makes the first match at or after offset current
// (U:input/editor/search.rs:384-391).
func (m *SearchMatcher) UpdateCursorByOffset(offset int) {
	for ix, r := range m.matchedRanges {
		m.currentMatchIx = ix
		if r.Contains(offset) || r.End >= offset {
			return
		}
	}
}

// beginReplacement preserves the current logical match while a
// replacement mutates the text (U:input/editor/search.rs:393-396).
func (m *SearchMatcher) beginReplacement() { m.replacing = true }

// setCurrentMatchIndex is U:input/editor/search.rs:398-400.
func (m *SearchMatcher) setCurrentMatchIndex(index int) {
	m.currentMatchIx = min(index, max(len(m.matchedRanges)-1, 0))
}

// nextIndex is U:input/editor/search.rs:402-410.
func (m *SearchMatcher) nextIndex() (int, bool) {
	switch {
	case m.IsEmpty():
		return 0, false
	case m.hasNextWithoutWrap():
		return m.currentMatchIx + 1, true
	}
	return 0, true
}

// updateMatches is U:input/editor/search.rs:412-429.
func (m *SearchMatcher) updateMatches() {
	var ranges []InputSelection
	if m.query != nil {
		ranges = findAllMatches(m.text.String(), m.query.pattern, m.query.caseInsensitive)
	}
	m.matchedRanges = ranges
	if !m.replacing || m.IsEmpty() {
		m.currentMatchIx = 0
	} else {
		m.currentMatchIx = min(m.currentMatchIx, m.Len()-1)
	}
	m.replacing = false
}

// findAllMatches is the non-overlapping leftmost matches of one pattern,
// ASCII case-insensitively when asked (aho-corasick's
// ascii_case_insensitive find_iter).
func findAllMatches(text, pattern string, caseInsensitive bool) []InputSelection {
	if caseInsensitive {
		text, pattern = asciiLower(text), asciiLower(pattern)
	}
	var out []InputSelection
	for from := 0; from <= len(text); {
		i := strings.Index(text[from:], pattern)
		if i < 0 {
			break
		}
		start := from + i
		out = append(out, NewInputSelection(start, start+len(pattern)))
		from = start + len(pattern)
	}
	return out
}

// asciiLower lower-cases ASCII letters only, keeping byte offsets.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// Next is Iterator::next: the next match, wrapping to the first
// (U:input/editor/search.rs:432-440).
func (m *SearchMatcher) Next() (InputSelection, bool) {
	ix, ok := m.nextIndex()
	if !ok {
		return InputSelection{}, false
	}
	m.currentMatchIx = ix
	return m.matchedRanges[ix], true
}

// NextBack is DoubleEndedIterator::next_back: the previous match,
// wrapping to the last (U:input/editor/search.rs:442-453).
func (m *SearchMatcher) NextBack() (InputSelection, bool) {
	if m.IsEmpty() {
		return InputSelection{}, false
	}
	if m.currentMatchIx == 0 {
		m.currentMatchIx = m.Len()
	}
	m.currentMatchIx--
	return m.matchedRanges[m.currentMatchIx], true
}
