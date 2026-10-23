package base

// Port of U:input/base/inline_tokens.rs: document annotations for atomic
// inline objects. Coordinates are always source UTF-8 bytes.
//
// Go adaptations:
//
//   - SharedString fields are strings; Result<_, InlineTokenError> is an
//     error whose concrete type is InlineTokenError.
//   - `impl From<text> for InputContent` (U:input/base/inline_tokens.rs:
//     138-159) has no Go counterpart: SetValue keeps taking a string and
//     SetContent is set_value's InputContent arm.
//   - The token API exists on InputMode and TextareaMode only
//     (token_api!, U:input/base/inline_tokens.rs:389-425); like the other
//     mode-specific methods the Go methods exist on every mode and panic on
//     EditorMode (input_kind.go).
//   - grapheme_indices(true) is segment.GraphemeIndices.

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/hoangvu12/gpui-go/base/segment"
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// InlineToken is an application-defined reference rendered as one inline
// editing unit. The ID names the referenced resource; the same ID may
// occur more than once (U:input/base/inline_tokens.rs:9-16).
type InlineToken struct {
	id    string
	text  string
	label string
}

// NewInlineToken is InlineToken::new: the label defaults to the text
// (U:input/base/inline_tokens.rs:19-26).
func NewInlineToken(id, text string) InlineToken {
	return InlineToken{id: id, text: text, label: text}
}

// WithLabel is U:input/base/inline_tokens.rs:27-30.
func (t InlineToken) WithLabel(label string) InlineToken {
	t.label = label
	return t
}

// ID is U:input/base/inline_tokens.rs:31-33.
func (t InlineToken) ID() string { return t.id }

// Text is U:input/base/inline_tokens.rs:34-36.
func (t InlineToken) Text() string { return t.text }

// Label is U:input/base/inline_tokens.rs:37-39.
func (t InlineToken) Label() string { return t.label }

// validate rejects an empty ID and empty or control-character text and
// label (U:input/base/inline_tokens.rs:41-52).
func (t InlineToken) validate() error {
	bad := func(s string) bool {
		return s == "" || strings.ContainsFunc(s, func(c rune) bool {
			return unicode.IsControl(c) || c == ' ' || c == ' '
		})
	}
	if strings.TrimSpace(t.id) == "" || bad(t.text) || bad(t.label) {
		return InlineTokenErrorInvalidToken
	}
	return nil
}

// InlineTokenSpan is a token and its current half-open UTF-8 byte range
// (U:input/base/inline_tokens.rs:55-60).
type InlineTokenSpan struct {
	rng   InputSelection
	token InlineToken
}

// Range is U:input/base/inline_tokens.rs:62-64.
func (s InlineTokenSpan) Range() InputSelection { return s.rng }

// Token is U:input/base/inline_tokens.rs:65-67.
func (s InlineTokenSpan) Token() InlineToken { return s.token }

// shifted is U:input/base/inline_tokens.rs:68-82.
func (s InlineTokenSpan) shifted(delta int) InlineTokenSpan {
	if s.rng.Start+delta < 0 || s.rng.End+delta < 0 {
		panic("base: valid token range")
	}
	return InlineTokenSpan{rng: NewInputSelection(s.rng.Start+delta, s.rng.End+delta), token: s.token}
}

// InputContent is an owned, coherent text-and-token snapshot: what
// Content returns and SetContent accepts. Every token is validated against
// the text as it is attached (U:input/base/inline_tokens.rs:85-93).
type InputContent struct {
	text   string
	tokens []InlineTokenSpan
}

// NewInputContent is InputContent::new (U:input/base/inline_tokens.rs:95-100).
func NewInputContent(text string) InputContent { return InputContent{text: text} }

// WithToken attaches a token to a half-open UTF-8 byte range of the text.
// The range must be nonempty, sit on grapheme boundaries, contain exactly
// the token's text and not overlap another token
// (U:input/base/inline_tokens.rs:101-129).
func (c InputContent) WithToken(rng InputSelection, token InlineToken) (InputContent, error) {
	if err := token.validate(); err != nil {
		return c, err
	}
	if err := validateTokenRange(c.text, rng); err != nil {
		return c, err
	}
	if rng.IsEmpty() {
		return c, InlineTokenErrorInvalidRange
	}
	if c.text[rng.Start:rng.End] != token.text {
		return c, InlineTokenErrorTextMismatch
	}
	ix := sort.Search(len(c.tokens), func(i int) bool { return c.tokens[i].rng.End > rng.Start })
	if ix < len(c.tokens) && c.tokens[ix].rng.Start < rng.End {
		return c, InlineTokenErrorOverlappingTokens
	}
	c.tokens = slices.Insert(slices.Clone(c.tokens), ix, InlineTokenSpan{rng: rng, token: token})
	return c, nil
}

// Text is U:input/base/inline_tokens.rs:130-132.
func (c InputContent) Text() string { return c.text }

// Tokens is U:input/base/inline_tokens.rs:133-135.
func (c InputContent) Tokens() []InlineTokenSpan { return c.tokens }

// InlineTokenError is why a token operation was rejected; a rejected
// operation never partially changes the document
// (U:input/base/inline_tokens.rs:161-173).
type InlineTokenError uint8

const (
	InlineTokenErrorInvalidRange InlineTokenError = iota
	InlineTokenErrorInvalidBoundary
	InlineTokenErrorInvalidToken
	InlineTokenErrorOverlappingTokens
	InlineTokenErrorTextMismatch
	InlineTokenErrorUnsupportedMode
	InlineTokenErrorValidationRejected
	InlineTokenErrorCompositionActive
)

// Error is U:input/base/inline_tokens.rs:175-191.
func (e InlineTokenError) Error() string {
	switch e {
	case InlineTokenErrorInvalidRange:
		return "token range is outside the document or empty"
	case InlineTokenErrorInvalidBoundary:
		return "token range splits a Unicode grapheme"
	case InlineTokenErrorInvalidToken:
		return "token requires a nonempty ID, text and label without control characters"
	case InlineTokenErrorOverlappingTokens:
		return "token ranges overlap"
	case InlineTokenErrorTextMismatch:
		return "token text does not match its range or input normalization"
	case InlineTokenErrorUnsupportedMode:
		return "tokens are not supported by this input mode"
	case InlineTokenErrorValidationRejected:
		return "input validation rejected the content"
	case InlineTokenErrorCompositionActive:
		return "finish the active IME composition before editing tokens"
	}
	return "unknown inline token error"
}

// validateTokenRange checks the range is inside text and on grapheme
// boundaries (U:input/base/inline_tokens.rs:193-203).
func validateTokenRange(text string, rng InputSelection) error {
	if rng.Start > rng.End || rng.End > len(text) || rng.Start < 0 {
		return InlineTokenErrorInvalidRange
	}
	boundary := func(offset int) bool {
		if offset == len(text) {
			return true
		}
		for ix := range segment.GraphemeIndices(text) {
			if ix == offset {
				return true
			}
			if ix > offset {
				break
			}
		}
		return false
	}
	if !boundary(rng.Start) || !boundary(rng.End) {
		return InlineTokenErrorInvalidBoundary
	}
	return nil
}

// inlineTokenStore is InlineTokenStore (U:input/base/inline_tokens.rs:205-208).
type inlineTokenStore struct {
	spans []InlineTokenSpan
}

// tokenDelta is the affected records an edit removed and inserted, with
// ranges relative to the edit start; only these are retained in history
// (U:input/base/inline_tokens.rs:210-215).
type tokenDelta struct {
	removed  []InlineTokenSpan
	inserted []InlineTokenSpan
}

// replace drops the spans an edit overlaps, shifts the spans after it and
// adds inserted (relative to the edit start)
// (U:input/base/inline_tokens.rs:218-248).
func (s *inlineTokenStore) replace(rng InputSelection, newLen int, inserted []InlineTokenSpan) *tokenDelta {
	var removed []InlineTokenSpan
	shift := newLen - rng.Len()
	kept := s.spans[:0:0]
	for _, span := range s.spans {
		if span.rng.Start < rng.End && rng.Start < span.rng.End {
			removed = append(removed, span.shifted(-rng.Start))
			continue
		}
		if span.rng.Start >= rng.End {
			span = span.shifted(shift)
		}
		kept = append(kept, span)
	}
	for _, span := range inserted {
		kept = append(kept, span.shifted(rng.Start))
	}
	if len(inserted) > 0 {
		slices.SortStableFunc(kept, func(a, b InlineTokenSpan) int { return a.rng.Start - b.rng.Start })
	}
	s.spans = kept
	if len(removed) == 0 && len(inserted) == 0 {
		return nil
	}
	return &tokenDelta{removed: removed, inserted: slices.Clone(inserted)}
}

// tokenSpans is U:input/base/inline_tokens.rs:252-256.
func (s *InputBaseState[M]) tokenSpans() []InlineTokenSpan {
	if s.inlineTokens == nil {
		return nil
	}
	return s.inlineTokens.spans
}

// tokenBoundary snaps an offset inside a token to its start (Left) or end
// (Right) (U:input/base/inline_tokens.rs:257-269).
func (s *InputBaseState[M]) tokenBoundary(offset int, bias sumtree.Bias) int {
	spans := s.tokenSpans()
	ix := sort.Search(len(spans), func(i int) bool { return spans[i].rng.End > offset })
	if ix < len(spans) && spans[ix].rng.Start < offset {
		if bias == sumtree.Left {
			return spans[ix].rng.Start
		}
		return spans[ix].rng.End
	}
	return offset
}

// normalizeTokenRange widens a range over whole tokens; history replay
// keeps its exact ranges (U:input/base/inline_tokens.rs:270-281).
func (s *InputBaseState[M]) normalizeTokenRange(rng InputSelection) InputSelection {
	if s.replayingHistory {
		return rng
	}
	if rng.IsEmpty() {
		offset := s.tokenBoundary(rng.Start, sumtree.Right)
		return NewInputSelection(offset, offset)
	}
	return NewInputSelection(s.tokenBoundary(rng.Start, sumtree.Left), s.tokenBoundary(rng.End, sumtree.Right))
}

// editTokens shifts the spans over an edit, inserting the pending token,
// and returns the delta for history; nil is None
// (U:input/base/inline_tokens.rs:282-300).
func (s *InputBaseState[M]) editTokens(rng InputSelection, newLen int) *tokenDelta {
	if s.replayingHistory {
		return nil
	}
	var inserted []InlineTokenSpan
	if s.pendingToken != nil {
		inserted = []InlineTokenSpan{{rng: NewInputSelection(0, newLen), token: *s.pendingToken}}
		s.pendingToken = nil
		if s.inlineTokens == nil {
			s.inlineTokens = &inlineTokenStore{}
		}
	}
	if s.inlineTokens == nil {
		return nil
	}
	return s.inlineTokens.replace(rng, newLen, inserted)
}

// replayTokens restores a change's token records on undo (removed) or redo
// (inserted) (U:input/base/inline_tokens.rs:301-323).
func (s *InputBaseState[M]) replayTokens(rng InputSelection, newLen int, delta *tokenDelta, undo bool) {
	var inserted []InlineTokenSpan
	if delta != nil {
		if undo {
			inserted = delta.removed
		} else {
			inserted = delta.inserted
		}
	}
	if len(inserted) > 0 && s.inlineTokens == nil {
		s.inlineTokens = &inlineTokenStore{}
	}
	if s.inlineTokens != nil {
		s.inlineTokens.replace(rng, newLen, inserted)
	}
}

// checkTokenMode is U:input/base/inline_tokens.rs:324-332.
func (s *InputBaseState[M]) checkTokenMode() error {
	if s.imeMarkedRange != nil {
		return InlineTokenErrorCompositionActive
	}
	if s.IsCodeEditor() || s.masked || s.tokenIsSecret() || !s.maskPattern.IsNone() {
		return InlineTokenErrorUnsupportedMode
	}
	return nil
}

// replaceToken replaces a range with a token as one atomic, validated edit
// (U:input/base/inline_tokens.rs:333-373).
func (s *InputBaseState[M]) replaceToken(rng InputSelection, token InlineToken, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) error {
	if err := s.checkTokenMode(); err != nil {
		return err
	}
	if err := token.validate(); err != nil {
		return err
	}
	text := s.text.String()
	if err := validateTokenRange(text, rng); err != nil {
		return err
	}
	rng = s.normalizeTokenRange(rng)
	next := text[:rng.Start] + token.text + text[rng.End:]
	if err := validateTokenRange(next, NewInputSelection(rng.Start, rng.Start+len(token.text))); err != nil {
		return err
	}
	if s.normalizeInput(next) != next {
		return InlineTokenErrorTextMismatch
	}
	if !s.isValidInput(next, cx) {
		return InlineTokenErrorValidationRejected
	}
	for _, span := range s.tokenSpans() {
		if span.rng == rng && span.token == token {
			return nil
		}
	}
	pending := token
	s.pendingToken = &pending
	s.undoManager.breakTransactionCoalescing()
	s.undoManager.setPendingIntent(editIntentAtomic)
	rngUTF16 := s.rangeToUTF16(rng)
	s.validatedTokenEdit = true
	s.withEditsAllowed(func() { s.ReplaceTextInRange(&rngUTF16, token.text, w, cx) })
	s.validatedTokenEdit = false
	s.pendingToken = nil
	return nil
}

// installTokens adopts the tokens of content whose text set_value just
// installed. They are dropped when this mode cannot show them or when
// normalization changed the text (U:input/base/inline_tokens.rs:374-386).
func (s *InputBaseState[M]) installTokens(content InputContent) {
	supported := !s.IsCodeEditor() && s.maskPattern.IsNone()
	textKept := s.text.EqualString(content.text)
	s.inlineTokens = nil
	if supported && textKept && len(content.tokens) > 0 {
		s.inlineTokens = &inlineTokenStore{spans: slices.Clone(content.tokens)}
	}
}

// requireTokenMode panics on EditorMode, where token_api! is not
// implemented (U:input/base/inline_tokens.rs:424-425).
func (s *InputBaseState[M]) requireTokenMode(method string) {
	if s.IsCodeEditor() {
		panic("base: " + method + " is only available on InputState and TextareaState")
	}
}

// ReplaceWithToken replaces the selection with an atomic token; no
// delimiter is added (U:input/base/inline_tokens.rs:392-400).
func (s *InputBaseState[M]) ReplaceWithToken(token InlineToken, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) error {
	s.requireTokenMode("ReplaceWithToken")
	return s.replaceToken(s.SelectedRange(), token, w, cx)
}

// ReplaceRangeWithToken replaces a UTF-8 byte range, expanding overlaps to
// whole tokens (U:input/base/inline_tokens.rs:401-410).
func (s *InputBaseState[M]) ReplaceRangeWithToken(rng InputSelection, token InlineToken, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) error {
	s.requireTokenMode("ReplaceRangeWithToken")
	return s.replaceToken(rng, token, w, cx)
}

// Tokens is U:input/base/inline_tokens.rs:411-413.
func (s *InputBaseState[M]) Tokens() []InlineTokenSpan {
	s.requireTokenMode("Tokens")
	return s.tokenSpans()
}

// Content is the text with its tokens, as SetContent accepts it
// (U:input/base/inline_tokens.rs:414-420).
func (s *InputBaseState[M]) Content() InputContent {
	s.requireTokenMode("Content")
	return InputContent{text: s.Value(), tokens: slices.Clone(s.tokenSpans())}
}
