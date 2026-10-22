package base

// Port of U:input/editor/language_config.rs: language-specific bracket
// pairing and indentation rules.
//
// Go adaptations:
//
//   - The rules are pub fields upstream and also builders of the same name
//     (`brackets`, `auto_closing_pairs`, ...). Go cannot give a field and a
//     method one name, so the builders are With* (the repo's convention,
//     e.g. ScrollbarMotion.WithIdle); likewise AutoClosingPair.not_in is
//     WithNotIn next to the NotIn field.
//   - Option<Vec<AutoClosingPair>> is a slice: nil is None, a non-nil
//     (possibly empty) slice is Some (ticket 19's convention,
//     input_undo_manager.go).
//   - Patterns use the regex crate upstream; Go's regexp is the same RE2
//     family. Patterns are compiled by the caller, so options such as
//     case-insensitivity travel inside the compiled value.

import (
	"regexp"
	"strings"
	"unicode"
)

// BracketPair is a structural pair, used for indentation and splitting
// Enter between delimiters (U:input/editor/language_config.rs:9-24).
type BracketPair struct {
	Open  string
	Close string
}

// NewBracketPair is BracketPair::new (U:input/editor/language_config.rs:17-23).
func NewBracketPair(open, close string) BracketPair { return BracketPair{Open: open, Close: close} }

// AutoClosingPair is an automatic closing pair and the syntax contexts in
// which insertion is disabled. Empty delimiters are ignored; multi-char
// delimiters such as `/*` and `*/` are supported
// (U:input/editor/language_config.rs:26-34).
type AutoClosingPair struct {
	Open  string
	Close string
	NotIn []SyntaxContext
}

// NewAutoClosingPair is AutoClosingPair::new
// (U:input/editor/language_config.rs:36-43).
func NewAutoClosingPair(open, close string) AutoClosingPair {
	return AutoClosingPair{Open: open, Close: close}
}

// WithNotIn is AutoClosingPair::not_in: the contexts in which the pair is
// not inserted (U:input/editor/language_config.rs:45-48).
func (p AutoClosingPair) WithNotIn(contexts ...SyntaxContext) AutoClosingPair {
	p.NotIn = append([]SyntaxContext(nil), contexts...)
	return p
}

// IndentationRules are a language's indentation patterns, applied on
// Enter: increase matches the text before the caret, decrease the text
// after it (U:input/editor/language_config.rs:51-60).
type IndentationRules struct {
	IncreaseIndentPattern *regexp.Regexp
	DecreaseIndentPattern *regexp.Regexp
}

// NewIndentationRules is IndentationRules::new
// (U:input/editor/language_config.rs:62-69).
func NewIndentationRules(increase, decrease *regexp.Regexp) IndentationRules {
	return IndentationRules{IncreaseIndentPattern: increase, DecreaseIndentPattern: decrease}
}

// LanguageConfig is a language's declarative editing rules, independent
// of editor preferences and parsers. A nil AutoClosingPairs uses Brackets;
// an empty non-nil one disables every automatic pair
// (U:input/editor/language_config.rs:71-86).
type LanguageConfig struct {
	Brackets         []BracketPair
	AutoClosingPairs []AutoClosingPair
	// AutoCloseBefore lists the characters automatic insertion is allowed
	// before, besides whitespace and the end of the text.
	AutoCloseBefore  string
	IndentationRules *IndentationRules
}

// DefaultLanguageConfig is `impl Default for LanguageConfig`: (), [], {}
// and both quotes, none paired inside strings or comments
// (U:input/editor/language_config.rs:88-117).
func DefaultLanguageConfig() LanguageConfig {
	brackets := []BracketPair{NewBracketPair("(", ")"), NewBracketPair("[", "]"), NewBracketPair("{", "}")}
	pairs := make([]AutoClosingPair, 0, 5)
	for _, p := range brackets {
		pairs = append(pairs, NewAutoClosingPair(p.Open, p.Close).WithNotIn(SyntaxContextString, SyntaxContextComment))
	}
	for _, p := range []AutoClosingPair{NewAutoClosingPair(`"`, `"`), NewAutoClosingPair("'", "'")} {
		pairs = append(pairs, p.WithNotIn(SyntaxContextString, SyntaxContextComment))
	}
	return LanguageConfig{
		Brackets:         brackets,
		AutoClosingPairs: pairs,
		AutoCloseBefore:  ";:.,=}])>",
	}
}

// WithBrackets is LanguageConfig::brackets
// (U:input/editor/language_config.rs:120-123).
func (c LanguageConfig) WithBrackets(pairs ...BracketPair) LanguageConfig {
	c.Brackets = append([]BracketPair(nil), pairs...)
	return c
}

// WithAutoClosingPairs is LanguageConfig::auto_closing_pairs; no pairs
// disables automatic closing (U:input/editor/language_config.rs:124-127).
func (c LanguageConfig) WithAutoClosingPairs(pairs ...AutoClosingPair) LanguageConfig {
	c.AutoClosingPairs = append([]AutoClosingPair{}, pairs...)
	return c
}

// WithAutoCloseBefore is LanguageConfig::auto_close_before
// (U:input/editor/language_config.rs:128-131).
func (c LanguageConfig) WithAutoCloseBefore(characters string) LanguageConfig {
	c.AutoCloseBefore = characters
	return c
}

// WithIndentationRules is LanguageConfig::indentation_rules
// (U:input/editor/language_config.rs:132-135).
func (c LanguageConfig) WithIndentationRules(rules IndentationRules) LanguageConfig {
	c.IndentationRules = &rules
	return c
}

// closingPair is one (open, close, not_in) of closing_pairs.
type closingPair struct {
	open, close string
	notIn       []SyntaxContext
}

// closingPairs is the configured pairs, or the brackets when none are
// configured, skipping empty delimiters
// (U:input/editor/language_config.rs:137-152).
func (c *LanguageConfig) closingPairs() []closingPair {
	var out []closingPair
	add := func(p closingPair) {
		if p.open != "" && p.close != "" {
			out = append(out, p)
		}
	}
	for _, p := range c.AutoClosingPairs {
		add(closingPair{p.Open, p.Close, p.NotIn})
	}
	if c.AutoClosingPairs == nil {
		for _, p := range c.Brackets {
			add(closingPair{p.Open, p.Close, nil})
		}
	}
	return out
}

// opensIndent reports text before the caret that opens an indent level:
// the increase pattern, else a trailing bracket opener
// (U:input/editor/language_config.rs:154-166).
func (c *LanguageConfig) opensIndent(text string) bool {
	if c.IndentationRules != nil && c.IndentationRules.IncreaseIndentPattern != nil {
		return c.IndentationRules.IncreaseIndentPattern.MatchString(text)
	}
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	for _, p := range c.Brackets {
		if p.Open != "" && strings.HasSuffix(trimmed, p.Open) {
			return true
		}
	}
	return false
}

// closesIndent reports text after the caret matching the decrease pattern
// (U:input/editor/language_config.rs:168-173).
func (c *LanguageConfig) closesIndent(text string) bool {
	return c.IndentationRules != nil && c.IndentationRules.DecreaseIndentPattern != nil &&
		c.IndentationRules.DecreaseIndentPattern.MatchString(text)
}

func containsSyntaxContext(list []SyntaxContext, c SyntaxContext) bool {
	for _, v := range list {
		if v == c {
			return true
		}
	}
	return false
}
